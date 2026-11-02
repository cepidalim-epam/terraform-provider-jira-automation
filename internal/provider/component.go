package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// componentModel is the Terraform model for the "components" block.
type componentModel struct {
	Type types.String       `tfsdk:"type"`
	Args types.Map          `tfsdk:"args"`
	Then []innerActionModel `tfsdk:"then"`
	Else []innerActionModel `tfsdk:"else"`
}

// innerActionModel is the Terraform model for actions inside then/else blocks.
type innerActionModel struct {
	Type types.String `tfsdk:"type"`
	Args types.Map    `tfsdk:"args"`
}

// componentBuilder builds the API JSON for a single action from user args.
type componentBuilder func(args map[string]string, cloudID, webhookUser, webhookToken string) (json.RawMessage, error)

// componentParser extracts user-facing args from the API JSON for a single action.
type componentParser func(raw json.RawMessage) (map[string]string, error)

type componentDef struct {
	apiType string
	build   componentBuilder
	parse   componentParser
}

// debugLogPrefix is the prefix used by auto-generated debug log actions.
const debugLogPrefix = "[DEBUG add_release_related_work] "

// componentRegistry maps user-facing type names to their builder/parser pairs.
// "condition" is special-cased and not in this registry.
var componentRegistry = map[string]componentDef{
	"log": {
		apiType: "codebarrel.action.log",
		build:   buildLog,
		parse:   parseLog,
	},
	"comment": {
		apiType: "jira.issue.comment",
		build:   buildComment,
		parse:   parseComment,
	},
	"add_release_related_work": {
		apiType: "jira.issue.outgoing.webhook",
		build:   buildAddReleaseRelatedWork,
		parse:   parseAddReleaseRelatedWork,
	},
}

// apiTypeToComponentUserType maps API types back to user-facing names.
var apiTypeToComponentUserType = func() map[string]string {
	m := make(map[string]string, len(componentRegistry))
	for userType, def := range componentRegistry {
		m[def.apiType] = userType
	}
	return m
}()

// --- Field alias resolution ---

// resolveAliases replaces alias names with field IDs in arg values.
// - Smart values: {{issue.ALIAS...}} → {{issue.FIELD_ID...}}
// - Bare arg value: if the entire value exactly matches an alias key, replace with field ID.
func resolveAliases(args map[string]string, aliases map[string]string) map[string]string {
	if len(aliases) == 0 {
		return args
	}
	// Sort aliases by length descending to avoid partial matches.
	sorted := sortedKeys(aliases)
	result := make(map[string]string, len(args))
	for k, v := range args {
		// Smart value replacement in any string value.
		resolved := v
		for _, alias := range sorted {
			fieldID := aliases[alias]
			resolved = replaceSmartValueField(resolved, alias, fieldID)
		}
		// Bare value: if the entire resolved value is an alias, replace it.
		if id, ok := aliases[resolved]; ok {
			resolved = id
		}
		result[k] = resolved
	}
	return result
}

// unresolveAliases is the reverse: replaces field IDs with alias names.
func unresolveAliases(args map[string]string, reverse map[string]string) map[string]string {
	if len(reverse) == 0 {
		return args
	}
	sorted := sortedKeys(reverse)
	result := make(map[string]string, len(args))
	for k, v := range args {
		resolved := v
		for _, fieldID := range sorted {
			alias := reverse[fieldID]
			resolved = replaceSmartValueField(resolved, fieldID, alias)
		}
		if alias, ok := reverse[resolved]; ok {
			resolved = alias
		}
		result[k] = resolved
	}
	return result
}

// replaceSmartValueField replaces {{issue.OLD...}} and {{triggerIssue.OLD...}} with the new field name.
func replaceSmartValueField(s, oldField, newField string) string {
	// Match {{issue.OLD}} {{issue.OLD.xxx}} {{issue.OLD}} etc.
	// We look for the pattern and replace just the field name part.
	old1 := "{{issue." + oldField
	new1 := "{{issue." + newField
	old2 := "{{triggerIssue." + oldField
	new2 := "{{triggerIssue." + newField
	s = strings.ReplaceAll(s, old1, new1)
	s = strings.ReplaceAll(s, old2, new2)
	return s
}

// sortedKeys returns map keys sorted by length descending (longest first).
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return len(keys[i]) > len(keys[j])
	})
	return keys
}

// --- Builders ---

func buildLog(args map[string]string, _, _, _ string) (json.RawMessage, error) {
	msg := args["message"]
	if msg == "" {
		return nil, fmt.Errorf("log requires a 'message' arg")
	}
	action := map[string]interface{}{
		"children":      []interface{}{},
		"component":     "ACTION",
		"conditions":    []interface{}{},
		"connectionId":  nil,
		"schemaVersion": 1,
		"type":          "codebarrel.action.log",
		"value":         msg,
	}
	return json.Marshal(action)
}

func buildComment(args map[string]string, _, _, _ string) (json.RawMessage, error) {
	msg := args["message"]
	if msg == "" {
		return nil, fmt.Errorf("comment requires a 'message' arg")
	}
	action := map[string]interface{}{
		"children":      []interface{}{},
		"component":     "ACTION",
		"conditions":    []interface{}{},
		"connectionId":  nil,
		"schemaVersion": 2,
		"type":          "jira.issue.comment",
		"value": map[string]interface{}{
			"comment":           msg,
			"publicComment":     false,
			"commentVisibility": nil,
			"sendNotifications": true,
			"addCommentOnce":    false,
		},
	}
	return json.Marshal(action)
}

func buildAddReleaseRelatedWork(args map[string]string, cloudID, webhookUser, webhookToken string) (json.RawMessage, error) {
	versionField := args["version_field"]
	category := args["category"]
	title := args["title"]
	url := args["url"]
	if versionField == "" || category == "" || title == "" || url == "" {
		return nil, fmt.Errorf("add_release_related_work requires version_field, category, title, and url args")
	}
	if webhookUser == "" || webhookToken == "" {
		return nil, fmt.Errorf("add_release_related_work requires webhook_user and webhook_token in provider config (or JIRA_WEBHOOK_USER / JIRA_WEBHOOK_TOKEN env vars)")
	}

	webhookURL := fmt.Sprintf(
		"https://api.atlassian.com/ex/jira/%s/rest/api/3/version/{{issue.%s.format(\"###\")}}/relatedwork",
		cloudID, versionField,
	)

	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(webhookUser+":"+webhookToken))

	customBody := map[string]string{
		"category": category,
		"title":    title,
		"url":      url,
	}
	customBodyJSON, err := json.Marshal(customBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling custom body: %w", err)
	}

	action := map[string]interface{}{
		"children":      []interface{}{},
		"component":     "ACTION",
		"conditions":    []interface{}{},
		"connectionId":  nil,
		"schemaVersion": 1,
		"type":          "jira.issue.outgoing.webhook",
		"value": map[string]interface{}{
			"contentType":            "custom",
			"continueOnErrorEnabled": false,
			"customBody":             string(customBodyJSON),
			"headers": []map[string]interface{}{
				{
					"headerSecure": true,
					"id":           nil,
					"name":         "Authorization",
					"value":        authHeader,
				},
			},
			"method":          "POST",
			"responseEnabled": false,
			"sendIssue":       false,
			"url":             webhookURL,
		},
	}
	return json.Marshal(action)
}

// buildDebugLogs returns 4 log actions that dump useful runtime info for add_release_related_work.
func buildDebugLogs(args map[string]string, cloudID string) ([]json.RawMessage, error) {
	versionField := args["version_field"]
	category := args["category"]
	title := args["title"]
	url := args["url"]

	webhookURL := fmt.Sprintf(
		"https://api.atlassian.com/ex/jira/%s/rest/api/3/version/{{issue.%s.format(\"###\")}}/relatedwork",
		cloudID, versionField,
	)

	customBody := map[string]string{
		"category": category,
		"title":    title,
		"url":      url,
	}
	customBodyJSON, err := json.Marshal(customBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling debug body: %w", err)
	}

	messages := []string{
		debugLogPrefix + "webhook_url = " + webhookURL,
		debugLogPrefix + "request_body = " + string(customBodyJSON),
		debugLogPrefix + fmt.Sprintf("version_field_value = {{issue.%s}}", versionField),
		debugLogPrefix + fmt.Sprintf("version_id = {{issue.%s.format(\"###\")}}", versionField),
	}

	var logs []json.RawMessage
	for _, msg := range messages {
		raw, err := buildLog(map[string]string{"message": msg}, "", "", "")
		if err != nil {
			return nil, err
		}
		logs = append(logs, raw)
	}
	return logs, nil
}

// buildActionWithDebug builds one or more actions for the given type and args.
// For add_release_related_work with debug="true", it prepends 4 debug log actions.
func buildActionWithDebug(actionType string, args map[string]string, cloudID, webhookUser, webhookToken string) ([]json.RawMessage, error) {
	if actionType == "add_release_related_work" && args["debug"] == "true" {
		// Strip debug from args before building the real action.
		buildArgs := make(map[string]string, len(args))
		for k, v := range args {
			if k != "debug" {
				buildArgs[k] = v
			}
		}

		debugLogs, err := buildDebugLogs(buildArgs, cloudID)
		if err != nil {
			return nil, err
		}
		webhookRaw, err := buildAddReleaseRelatedWork(buildArgs, cloudID, webhookUser, webhookToken)
		if err != nil {
			return nil, err
		}
		return append(debugLogs, webhookRaw), nil
	}

	def, ok := componentRegistry[actionType]
	if !ok {
		return nil, fmt.Errorf("unknown action type %q", actionType)
	}
	raw, err := def.build(args, cloudID, webhookUser, webhookToken)
	if err != nil {
		return nil, err
	}
	return []json.RawMessage{raw}, nil
}

// --- Parsers ---

func parseLog(raw json.RawMessage) (map[string]string, error) {
	var action struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(raw, &action); err != nil {
		return nil, fmt.Errorf("parsing log action: %w", err)
	}
	return map[string]string{"message": action.Value}, nil
}

func parseComment(raw json.RawMessage) (map[string]string, error) {
	var action struct {
		Value struct {
			Comment string `json:"comment"`
		} `json:"value"`
	}
	if err := json.Unmarshal(raw, &action); err != nil {
		return nil, fmt.Errorf("parsing comment action: %w", err)
	}
	return map[string]string{"message": action.Value.Comment}, nil
}

// relatedworkURLPattern matches the webhook URL pattern for add_release_related_work.
var relatedworkURLPattern = regexp.MustCompile(
	`^https://api\.atlassian\.com/ex/jira/[^/]+/rest/api/3/version/\{\{issue\.([^.]+)\.format\("###"\)\}\}/relatedwork$`,
)

func parseAddReleaseRelatedWork(raw json.RawMessage) (map[string]string, error) {
	var action struct {
		Value struct {
			URL        string `json:"url"`
			CustomBody string `json:"customBody"`
		} `json:"value"`
	}
	if err := json.Unmarshal(raw, &action); err != nil {
		return nil, fmt.Errorf("parsing webhook action: %w", err)
	}

	matches := relatedworkURLPattern.FindStringSubmatch(action.Value.URL)
	if matches == nil {
		return nil, fmt.Errorf("webhook URL %q does not match relatedwork pattern", action.Value.URL)
	}
	versionField := matches[1]

	var body map[string]string
	if err := json.Unmarshal([]byte(action.Value.CustomBody), &body); err != nil {
		return nil, fmt.Errorf("parsing webhook custom body: %w", err)
	}

	return map[string]string{
		"version_field": versionField,
		"category":      body["category"],
		"title":         body["title"],
		"url":           body["url"],
	}, nil
}

// --- Condition builder ---

// BuildConditionJSON builds the 3-layer condition container JSON.
func BuildConditionJSON(condArgs map[string]string, thenActions, elseActions []json.RawMessage) (json.RawMessage, error) {
	first := condArgs["first"]
	operator := condArgs["operator"]
	second := condArgs["second"]
	if first == "" || operator == "" {
		return nil, fmt.Errorf("condition requires 'first' and 'operator' args")
	}

	// The comparator condition inside the IF block.
	// The API expects uppercase operator values (EQUALS, NOT_EQUALS, etc.).
	comparator := map[string]interface{}{
		"children":      []interface{}{},
		"component":     "CONDITION",
		"conditions":    []interface{}{},
		"connectionId":  nil,
		"schemaVersion": 1,
		"type":          "jira.comparator.condition",
		"value": map[string]interface{}{
			"first":    first,
			"operator": strings.ToUpper(operator),
			"second":   second,
		},
	}

	// Convert thenActions from json.RawMessage to interface{} for nesting.
	thenChildren := make([]interface{}, len(thenActions))
	for i, raw := range thenActions {
		var v interface{}
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, fmt.Errorf("parsing then action %d: %w", i, err)
		}
		thenChildren[i] = v
	}

	elseChildren := make([]interface{}, len(elseActions))
	for i, raw := range elseActions {
		var v interface{}
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, fmt.Errorf("parsing else action %d: %w", i, err)
		}
		elseChildren[i] = v
	}

	// IF block (first CONDITION_BLOCK): has the comparator condition + then children.
	ifBlock := map[string]interface{}{
		"children":  thenChildren,
		"component": "CONDITION_BLOCK",
		"conditions": []interface{}{
			comparator,
		},
		"connectionId":  nil,
		"schemaVersion": 1,
		"type":          "jira.condition.if.block",
		"value": map[string]interface{}{
			"conditionMatchType": "ALL",
		},
	}

	// ELSE block (second CONDITION_BLOCK): empty conditions + else children.
	elseBlock := map[string]interface{}{
		"children":      elseChildren,
		"component":     "CONDITION_BLOCK",
		"conditions":    []interface{}{},
		"connectionId":  nil,
		"schemaVersion": 1,
		"type":          "jira.condition.if.block",
		"value": map[string]interface{}{
			"conditionMatchType": "ALL",
		},
	}

	// Outer container wrapping both IF and ELSE blocks.
	container := map[string]interface{}{
		"children": []interface{}{
			ifBlock,
			elseBlock,
		},
		"component":     "CONDITION",
		"conditions":    []interface{}{},
		"connectionId":  nil,
		"schemaVersion": 1,
		"type":          "jira.condition.container.block",
		"value":         map[string]interface{}{},
	}

	return json.Marshal(container)
}

// --- Condition parser ---

func parseConditionContainer(raw json.RawMessage, ctx context.Context, reverse map[string]string) (*componentModel, error) {
	var container struct {
		Children []json.RawMessage `json:"children"`
	}
	if err := json.Unmarshal(raw, &container); err != nil {
		return nil, fmt.Errorf("parsing condition container: %w", err)
	}
	if len(container.Children) < 1 {
		return nil, fmt.Errorf("condition container has no children")
	}

	// Parse IF block (first child).
	var ifBlock struct {
		Conditions []json.RawMessage `json:"conditions"`
		Children   []json.RawMessage `json:"children"`
	}
	if err := json.Unmarshal(container.Children[0], &ifBlock); err != nil {
		return nil, fmt.Errorf("parsing IF block: %w", err)
	}

	// Extract condition args from the comparator.
	if len(ifBlock.Conditions) < 1 {
		return nil, fmt.Errorf("IF block has no conditions")
	}
	var comparator struct {
		Value struct {
			First    string `json:"first"`
			Operator string `json:"operator"`
			Second   string `json:"second"`
		} `json:"value"`
	}
	if err := json.Unmarshal(ifBlock.Conditions[0], &comparator); err != nil {
		return nil, fmt.Errorf("parsing comparator condition: %w", err)
	}

	condArgs := map[string]string{
		"first":    comparator.Value.First,
		"operator": strings.ToLower(comparator.Value.Operator),
		"second":   comparator.Value.Second,
	}
	condArgs = unresolveAliases(condArgs, reverse)

	// Parse THEN actions from IF block children.
	thenActions, err := parseInnerActions(ifBlock.Children, reverse)
	if err != nil {
		return nil, fmt.Errorf("parsing then actions: %w", err)
	}

	// Parse ELSE actions from second child (if present).
	var elseActions []innerActionModel
	if len(container.Children) >= 2 {
		var elseBlock struct {
			Children []json.RawMessage `json:"children"`
		}
		if err := json.Unmarshal(container.Children[1], &elseBlock); err != nil {
			return nil, fmt.Errorf("parsing ELSE block: %w", err)
		}
		if len(elseBlock.Children) > 0 {
			elseActions, err = parseInnerActions(elseBlock.Children, reverse)
			if err != nil {
				return nil, fmt.Errorf("parsing else actions: %w", err)
			}
		}
	}

	argsMap, err := stringMapToTypesMap(ctx, condArgs)
	if err != nil {
		return nil, err
	}

	model := &componentModel{
		Type: types.StringValue("condition"),
		Args: argsMap,
		Then: thenActions,
	}
	// Only set Else if there are actual actions (avoids null vs empty plan diff).
	if len(elseActions) > 0 {
		model.Else = elseActions
	}

	return model, nil
}

// parseInnerActions parses a list of action JSON blobs into innerActionModels.
// It detects debug log actions (prefixed with debugLogPrefix), skips them, and
// sets debug="true" on the following add_release_related_work action.
func parseInnerActions(raws []json.RawMessage, reverse map[string]string) ([]innerActionModel, error) {
	var actions []innerActionModel
	sawDebugLog := false

	for _, raw := range raws {
		var envelope struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return nil, fmt.Errorf("parsing action type: %w", err)
		}

		// Detect and skip debug log actions.
		if envelope.Type == "codebarrel.action.log" {
			var logAction struct {
				Value string `json:"value"`
			}
			if err := json.Unmarshal(raw, &logAction); err != nil {
				return nil, fmt.Errorf("parsing log value: %w", err)
			}
			if strings.HasPrefix(logAction.Value, debugLogPrefix) {
				sawDebugLog = true
				continue
			}
		}

		// For outgoing webhooks, check if it matches our known pattern.
		userType, ok := apiTypeToComponentUserType[envelope.Type]
		if !ok {
			return nil, fmt.Errorf("unrecognized component API type: %q", envelope.Type)
		}

		// For webhooks, we need to disambiguate.
		if envelope.Type == "jira.issue.outgoing.webhook" {
			var webhook struct {
				Value struct {
					URL string `json:"url"`
				} `json:"value"`
			}
			if err := json.Unmarshal(raw, &webhook); err != nil {
				return nil, fmt.Errorf("parsing webhook URL: %w", err)
			}
			if !strings.HasSuffix(webhook.Value.URL, "/relatedwork") {
				return nil, fmt.Errorf("outgoing webhook URL %q does not match any known component type; use components_json escape hatch", webhook.Value.URL)
			}
		}

		def := componentRegistry[userType]
		args, err := def.parse(raw)
		if err != nil {
			return nil, fmt.Errorf("parsing %s action: %w", userType, err)
		}

		// If we saw debug logs before this action, mark it with debug="true".
		if sawDebugLog && userType == "add_release_related_work" {
			args["debug"] = "true"
		}
		sawDebugLog = false

		args = unresolveAliases(args, reverse)
		argsMap, err := stringMapToTypesMapInner(args)
		if err != nil {
			return nil, err
		}

		actions = append(actions, innerActionModel{
			Type: types.StringValue(userType),
			Args: argsMap,
		})
	}
	return actions, nil
}

// --- Top-level orchestrators ---

// BuildComponentsJSON builds the full API components JSON from the structured components.
// aliases maps friendly names → field IDs; pass nil for no alias resolution.
func BuildComponentsJSON(components []componentModel, cloudID, webhookUser, webhookToken string, ctx context.Context, aliases map[string]string) ([]json.RawMessage, error) {
	var result []json.RawMessage

	for i, comp := range components {
		compType := comp.Type.ValueString()

		if compType == "condition" {
			// Build condition with then/else children.
			condArgs, err := typesMapToStringMap(ctx, comp.Args)
			if err != nil {
				return nil, fmt.Errorf("component %d: %w", i, err)
			}
			condArgs = resolveAliases(condArgs, aliases)

			var thenActions []json.RawMessage
			for j, action := range comp.Then {
				raws, err := buildInnerAction(action, cloudID, webhookUser, webhookToken, ctx, aliases)
				if err != nil {
					return nil, fmt.Errorf("component %d then[%d]: %w", i, j, err)
				}
				thenActions = append(thenActions, raws...)
			}

			var elseActions []json.RawMessage
			for j, action := range comp.Else {
				raws, err := buildInnerAction(action, cloudID, webhookUser, webhookToken, ctx, aliases)
				if err != nil {
					return nil, fmt.Errorf("component %d else[%d]: %w", i, j, err)
				}
				elseActions = append(elseActions, raws...)
			}

			raw, err := BuildConditionJSON(condArgs, thenActions, elseActions)
			if err != nil {
				return nil, fmt.Errorf("component %d: %w", i, err)
			}
			result = append(result, raw)
		} else {
			// Top-level action (no then/else).
			args, err := typesMapToStringMap(ctx, comp.Args)
			if err != nil {
				return nil, fmt.Errorf("component %d: %w", i, err)
			}
			args = resolveAliases(args, aliases)
			raws, err := buildActionWithDebug(compType, args, cloudID, webhookUser, webhookToken)
			if err != nil {
				return nil, fmt.Errorf("component %d: %w", i, err)
			}
			result = append(result, raws...)
		}
	}

	return result, nil
}

// buildInnerAction builds one or more action JSONs from the innerActionModel.
// For add_release_related_work with debug="true", multiple actions are returned.
func buildInnerAction(action innerActionModel, cloudID, webhookUser, webhookToken string, ctx context.Context, aliases map[string]string) ([]json.RawMessage, error) {
	actionType := action.Type.ValueString()
	args, err := typesMapToStringMap(ctx, action.Args)
	if err != nil {
		return nil, err
	}
	args = resolveAliases(args, aliases)
	return buildActionWithDebug(actionType, args, cloudID, webhookUser, webhookToken)
}

// ParseComponents parses the full API components JSON back into structured componentModels.
// It detects debug log actions at the top level and sets debug="true" on the following action.
// reverse maps field IDs → alias names; pass nil for no alias resolution.
func ParseComponents(raws []json.RawMessage, ctx context.Context, reverse map[string]string) ([]componentModel, error) {
	var result []componentModel
	sawDebugLog := false

	for i, raw := range raws {
		var envelope struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return nil, fmt.Errorf("component %d: parsing type: %w", i, err)
		}

		// Detect and skip debug log actions.
		if envelope.Type == "codebarrel.action.log" {
			var logAction struct {
				Value string `json:"value"`
			}
			if err := json.Unmarshal(raw, &logAction); err != nil {
				return nil, fmt.Errorf("component %d: parsing log value: %w", i, err)
			}
			if strings.HasPrefix(logAction.Value, debugLogPrefix) {
				sawDebugLog = true
				continue
			}
		}

		if envelope.Type == "jira.condition.container.block" {
			sawDebugLog = false
			model, err := parseConditionContainer(raw, ctx, reverse)
			if err != nil {
				return nil, fmt.Errorf("component %d: %w", i, err)
			}
			result = append(result, *model)
		} else {
			userType, ok := apiTypeToComponentUserType[envelope.Type]
			if !ok {
				return nil, fmt.Errorf("component %d: unrecognized API type %q", i, envelope.Type)
			}
			def := componentRegistry[userType]
			args, err := def.parse(raw)
			if err != nil {
				return nil, fmt.Errorf("component %d: %w", i, err)
			}

			if sawDebugLog && userType == "add_release_related_work" {
				args["debug"] = "true"
			}
			sawDebugLog = false

			args = unresolveAliases(args, reverse)
			argsMap, err := stringMapToTypesMap(ctx, args)
			if err != nil {
				return nil, fmt.Errorf("component %d: %w", i, err)
			}
			result = append(result, componentModel{
				Type: types.StringValue(userType),
				Args: argsMap,
			})
		}
	}

	return result, nil
}

// --- Helper functions ---

func typesMapToStringMap(ctx context.Context, m types.Map) (map[string]string, error) {
	if m.IsNull() || m.IsUnknown() {
		return map[string]string{}, nil
	}
	result := make(map[string]string)
	diags := m.ElementsAs(ctx, &result, false)
	if diags.HasError() {
		return nil, fmt.Errorf("converting map: %s", diags.Errors()[0].Detail())
	}
	return result, nil
}

func stringMapToTypesMap(ctx context.Context, m map[string]string) (types.Map, error) {
	result, diags := types.MapValueFrom(ctx, types.StringType, m)
	if diags.HasError() {
		return types.MapNull(types.StringType), fmt.Errorf("converting map: %s", diags.Errors()[0].Detail())
	}
	return result, nil
}

// stringMapToTypesMapInner converts a Go map to a types.Map without needing context.
func stringMapToTypesMapInner(m map[string]string) (types.Map, error) {
	ctx := context.Background()
	return stringMapToTypesMap(ctx, m)
}
