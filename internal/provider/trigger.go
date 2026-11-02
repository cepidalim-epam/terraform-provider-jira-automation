package provider

import (
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// triggerModel is the Terraform model for the "trigger" block.
type triggerModel struct {
	Type types.String `tfsdk:"type"`
	Args types.Map    `tfsdk:"args"`
}

// triggerBuilder builds the full API trigger JSON from user args.
type triggerBuilder func(args map[string]string, cloudID, projectID string) (json.RawMessage, error)

// triggerParser extracts user-facing args from the full API trigger JSON.
type triggerParser func(raw json.RawMessage) (map[string]string, error)

type triggerDef struct {
	apiType string
	build   triggerBuilder
	parse   triggerParser
}

// triggerRegistry maps user-facing type names to their builder/parser pairs.
var triggerRegistry = map[string]triggerDef{
	"status_transition": {
		apiType: "jira.issue.event.trigger:transitioned",
		build:   buildStatusTransition,
		parse:   parseStatusTransition,
	},
}

// apiTypeToUserType maps API trigger types back to user-facing names.
var apiTypeToUserType = func() map[string]string {
	m := make(map[string]string, len(triggerRegistry))
	for userType, def := range triggerRegistry {
		m[def.apiType] = userType
	}
	return m
}()

// BuildTriggerJSON builds the full API trigger JSON for a given user-facing trigger type.
func BuildTriggerJSON(triggerType string, args map[string]string, cloudID, projectID string) (json.RawMessage, error) {
	def, ok := triggerRegistry[triggerType]
	if !ok {
		return nil, fmt.Errorf("unknown trigger type: %q", triggerType)
	}
	return def.build(args, cloudID, projectID)
}

// ParseTrigger extracts the user-facing type and args from API trigger JSON.
func ParseTrigger(raw json.RawMessage) (triggerType string, args map[string]string, err error) {
	var envelope struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return "", nil, fmt.Errorf("parsing trigger type: %w", err)
	}

	userType, ok := apiTypeToUserType[envelope.Type]
	if !ok {
		return "", nil, fmt.Errorf("unrecognized API trigger type: %q", envelope.Type)
	}

	def := triggerRegistry[userType]
	args, err = def.parse(raw)
	if err != nil {
		return "", nil, fmt.Errorf("parsing trigger args for %q: %w", userType, err)
	}

	return userType, args, nil
}

// --- status_transition ---

func buildStatusTransition(args map[string]string, cloudID, projectID string) (json.RawMessage, error) {
	fromStatus := args["from_status"]
	toStatus := args["to_status"]
	if fromStatus == "" || toStatus == "" {
		return nil, fmt.Errorf("status_transition requires from_status and to_status args")
	}

	trigger := map[string]interface{}{
		"component":     "TRIGGER",
		"conditions":    []interface{}{},
		"connectionId":  nil,
		"schemaVersion": 1,
		"type":          "jira.issue.event.trigger:transitioned",
		"value": map[string]interface{}{
			"eventFilters": []string{
				fmt.Sprintf("ari:cloud:jira:%s:project/%s", cloudID, projectID),
			},
			"eventKey":   "jira:issue_updated",
			"issueEvent": "issue_generic",
			"fromStatus": []map[string]string{
				{"type": "NAME", "value": fromStatus},
			},
			"toStatus": []map[string]string{
				{"type": "NAME", "value": toStatus},
			},
		},
	}

	return json.Marshal(trigger)
}

func parseStatusTransition(raw json.RawMessage) (map[string]string, error) {
	var trigger struct {
		Value struct {
			FromStatus []struct {
				Value string `json:"value"`
			} `json:"fromStatus"`
			ToStatus []struct {
				Value string `json:"value"`
			} `json:"toStatus"`
		} `json:"value"`
	}
	if err := json.Unmarshal(raw, &trigger); err != nil {
		return nil, fmt.Errorf("parsing status_transition: %w", err)
	}

	args := map[string]string{}
	if len(trigger.Value.FromStatus) > 0 {
		args["from_status"] = trigger.Value.FromStatus[0].Value
	}
	if len(trigger.Value.ToStatus) > 0 {
		args["to_status"] = trigger.Value.ToStatus[0].Value
	}

	return args, nil
}
