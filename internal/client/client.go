package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	BaseURL        string
	SiteURL        string
	CloudID        string
	AccountID      string // Current user's Jira account ID, resolved at init.
	Email          string
	APIToken       string
	WebhookUser    string
	WebhookToken   string
	HTTPClient     *http.Client
	FieldAliases   map[string]string // alias → fieldID
	ReverseAliases map[string]string // fieldID → alias
}

// TenantInfo is the response from /_edge/tenant_info.
type TenantInfo struct {
	CloudID string `json:"cloudId"`
}

// RuleSummary is a single entry returned by GET /rule/summary.
type RuleSummary struct {
	UUID    string `json:"uuid"`
	Name    string `json:"name"`
	State   string `json:"state"`
	Enabled bool   `json:"enabled"`
}

// ListRulesResponse is the paginated response from GET /rule/summary.
type ListRulesResponse struct {
	Data   []RuleSummary `json:"data"`
	Cursor *string       `json:"cursor"`
}

// GetRuleResponse is the envelope for GET /rule/{uuid}.
type GetRuleResponse struct {
	Rule json.RawMessage `json:"rule"`
}

// Rule is the full rule object from GET /rule/{uuid}.
type Rule struct {
	UUID          string            `json:"uuid,omitempty"`
	Name          string            `json:"name"`
	State         string            `json:"state,omitempty"`
	RuleScopeARIs []string          `json:"ruleScopeARIs,omitempty"`
	Labels        []string          `json:"labels,omitempty"`
	Trigger       json.RawMessage   `json:"trigger"`
	Components    []json.RawMessage `json:"components"`
}

// GetRuleRaw returns the raw JSON for a rule (without the envelope).
func (c *Client) GetRuleRaw(uuid string) (json.RawMessage, error) {
	url := c.BaseURL + "/rule/" + uuid
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building get rule request: %w", err)
	}

	resp, err := c.do(req)
	if err != nil {
		return nil, fmt.Errorf("getting rule %s: %w", uuid, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get rule returned %d: %s", resp.StatusCode, string(body))
	}

	var envelope GetRuleResponse
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decoding rule: %w", err)
	}

	return envelope.Rule, nil
}

// CreateRuleRequest is the payload for POST /rule.
type CreateRuleRequest struct {
	Name       string
	ProjectID  string // Optional; used to build project-scoped ARIs.
	Trigger    json.RawMessage
	Components []json.RawMessage
}

// CreateRuleResponse is the response from POST /rule.
type CreateRuleResponse struct {
	UUID string `json:"uuid"`
	// The API may also return "ruleUuid" depending on the endpoint.
	RuleUUID string `json:"ruleUuid"`
}

// UpdateRuleRequest is the payload for PUT /rule/{uuid}.
type UpdateRuleRequest struct {
	Name       string            `json:"name"`
	Trigger    json.RawMessage   `json:"trigger"`
	Components []json.RawMessage `json:"components"`
}

// SetRuleStateRequest is the payload for PUT /rule/{uuid}/state.
type SetRuleStateRequest struct {
	Value string `json:"value"`
}

// New creates a new API client. It resolves the Cloud ID from the site URL.
// aliases maps friendly names to Jira field IDs (e.g. "release_version" → "customfield_10709").
// Pass nil for no aliases.
func New(siteURL, email, apiToken, webhookUser, webhookToken string, aliases map[string]string) (*Client, error) {
	httpClient := &http.Client{Timeout: 30 * time.Second}

	// Resolve cloud ID from tenant info.
	tenantURL := siteURL + "/_edge/tenant_info"
	req, err := http.NewRequest(http.MethodGet, tenantURL, nil)
	if err != nil {
		return nil, fmt.Errorf("building tenant info request: %w", err)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching tenant info from %s: %w", tenantURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("tenant info returned %d: %s", resp.StatusCode, string(body))
	}

	var tenant TenantInfo
	if err := json.NewDecoder(resp.Body).Decode(&tenant); err != nil {
		return nil, fmt.Errorf("decoding tenant info: %w", err)
	}
	if tenant.CloudID == "" {
		return nil, fmt.Errorf("empty cloudId from tenant info")
	}

	baseURL := fmt.Sprintf("https://api.atlassian.com/automation/public/jira/%s/rest/v1", tenant.CloudID)

	// Resolve the current user's account ID for rule authorship fields.
	myselfURL := siteURL + "/rest/api/3/myself"
	myselfReq, err := http.NewRequest(http.MethodGet, myselfURL, nil)
	if err != nil {
		return nil, fmt.Errorf("building myself request: %w", err)
	}
	myselfReq.SetBasicAuth(email, apiToken)
	myselfReq.Header.Set("Accept", "application/json")
	myselfResp, err := httpClient.Do(myselfReq)
	if err != nil {
		return nil, fmt.Errorf("fetching current user: %w", err)
	}
	defer myselfResp.Body.Close()

	if myselfResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(myselfResp.Body)
		return nil, fmt.Errorf("myself returned %d: %s", myselfResp.StatusCode, string(body))
	}

	var myself struct {
		AccountID string `json:"accountId"`
	}
	if err := json.NewDecoder(myselfResp.Body).Decode(&myself); err != nil {
		return nil, fmt.Errorf("decoding myself response: %w", err)
	}
	if myself.AccountID == "" {
		return nil, fmt.Errorf("empty accountId from /rest/api/3/myself")
	}

	if aliases == nil {
		aliases = map[string]string{}
	}
	reverse := make(map[string]string, len(aliases))
	for alias, fieldID := range aliases {
		reverse[fieldID] = alias
	}

	return &Client{
		BaseURL:        baseURL,
		SiteURL:        siteURL,
		CloudID:        tenant.CloudID,
		AccountID:      myself.AccountID,
		Email:          email,
		APIToken:       apiToken,
		WebhookUser:    webhookUser,
		WebhookToken:   webhookToken,
		HTTPClient:     httpClient,
		FieldAliases:   aliases,
		ReverseAliases: reverse,
	}, nil
}

func (c *Client) do(req *http.Request) (*http.Response, error) {
	req.SetBasicAuth(c.Email, c.APIToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	return c.HTTPClient.Do(req)
}

// ListRules returns all rule summaries, handling cursor pagination.
func (c *Client) ListRules() ([]RuleSummary, error) {
	var all []RuleSummary
	url := c.BaseURL + "/rule/summary"

	for {
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return nil, fmt.Errorf("building list rules request: %w", err)
		}

		resp, err := c.do(req)
		if err != nil {
			return nil, fmt.Errorf("listing rules: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("list rules returned %d: %s", resp.StatusCode, string(body))
		}

		var page ListRulesResponse
		if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
			return nil, fmt.Errorf("decoding list rules response: %w", err)
		}

		all = append(all, page.Data...)

		if page.Cursor == nil || *page.Cursor == "" {
			break
		}
		url = c.BaseURL + "/rule/summary?cursor=" + *page.Cursor
	}

	return all, nil
}

// GetRule returns the full rule config for a given UUID.
func (c *Client) GetRule(uuid string) (*Rule, error) {
	raw, err := c.GetRuleRaw(uuid)
	if err != nil {
		return nil, err
	}

	var rule Rule
	if err := json.Unmarshal(raw, &rule); err != nil {
		return nil, fmt.Errorf("decoding rule: %w", err)
	}

	return &rule, nil
}

// CreateRule creates a new automation rule and returns the UUID.
// The API requires several fields beyond name/trigger/components:
// state, notifyOnError, canOtherRuleTrigger, authorAccountId, actor,
// writeAccessType, and ruleScopeARIs. These are populated automatically.
func (c *Client) CreateRule(rule CreateRuleRequest) (string, error) {
	// Parse trigger and components into generic types for the payload.
	var trigger interface{}
	if err := json.Unmarshal(rule.Trigger, &trigger); err != nil {
		return "", fmt.Errorf("parsing trigger: %w", err)
	}

	var components []interface{}
	for _, comp := range rule.Components {
		var v interface{}
		if err := json.Unmarshal(comp, &v); err != nil {
			return "", fmt.Errorf("parsing component: %w", err)
		}
		components = append(components, v)
	}

	// Build scope ARIs.
	var scopeARIs []string
	if rule.ProjectID != "" {
		scopeARIs = []string{
			fmt.Sprintf("ari:cloud:jira:%s:project/%s", c.CloudID, rule.ProjectID),
		}
	} else {
		scopeARIs = []string{
			fmt.Sprintf("ari:cloud:jira::site/%s", c.CloudID),
		}
	}

	// Build the full rule payload with all required fields.
	payload := map[string]interface{}{
		"name":                rule.Name,
		"state":               "DISABLED", // Create disabled; enable via SetRuleState after.
		"notifyOnError":       "FIRSTERROR",
		"canOtherRuleTrigger": false,
		"authorAccountId":     c.AccountID,
		"actor":               map[string]string{"type": "ACCOUNT_ID", "actor": c.AccountID},
		"writeAccessType":     "OWNER_ONLY",
		"trigger":             trigger,
		"components":          components,
		"ruleScopeARIs":       scopeARIs,
	}

	envelope := map[string]interface{}{"rule": payload}
	body, err := json.Marshal(envelope)
	if err != nil {
		return "", fmt.Errorf("marshaling create rule request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, c.BaseURL+"/rule", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("building create rule request: %w", err)
	}

	resp, err := c.do(req)
	if err != nil {
		return "", fmt.Errorf("creating rule: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("create rule returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result CreateRuleResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decoding create rule response: %w", err)
	}

	// The API returns either "uuid" or "ruleUuid" depending on the endpoint.
	uuid := result.UUID
	if uuid == "" {
		uuid = result.RuleUUID
	}

	return uuid, nil
}

// UpdateRule updates an existing automation rule.
// It performs a read-modify-write: fetches the current rule to get all API fields,
// merges in the Terraform-managed fields, strips component IDs (so the API recreates
// them), and PUTs the complete rule wrapped in the required {"rule": ...} envelope.
func (c *Client) UpdateRule(uuid string, update UpdateRuleRequest) error {
	// 1. Fetch current rule as raw JSON to preserve all API-managed fields.
	raw, err := c.GetRuleRaw(uuid)
	if err != nil {
		return fmt.Errorf("reading current rule for update: %w", err)
	}

	// 2. Unmarshal into a generic map so we can merge fields.
	var ruleMap map[string]interface{}
	if err := json.Unmarshal(raw, &ruleMap); err != nil {
		return fmt.Errorf("parsing current rule: %w", err)
	}

	// 3. Remove read-only fields that the API won't accept on write.
	delete(ruleMap, "uuid")
	delete(ruleMap, "created")
	delete(ruleMap, "updated")

	// 4. Merge Terraform-managed fields.
	ruleMap["name"] = update.Name

	var trigger interface{}
	if err := json.Unmarshal(update.Trigger, &trigger); err != nil {
		return fmt.Errorf("parsing trigger: %w", err)
	}
	// Strip IDs from trigger.
	stripComponentIDs(trigger)
	ruleMap["trigger"] = trigger

	var components []interface{}
	for _, comp := range update.Components {
		var c interface{}
		if err := json.Unmarshal(comp, &c); err != nil {
			return fmt.Errorf("parsing component: %w", err)
		}
		stripComponentIDs(c)
		components = append(components, c)
	}
	ruleMap["components"] = components

	// 5. Wrap in the required {"rule": ...} envelope.
	envelope := map[string]interface{}{"rule": ruleMap}
	body, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("marshaling update rule request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPut, c.BaseURL+"/rule/"+uuid, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("building update rule request: %w", err)
	}

	resp, err := c.do(req)
	if err != nil {
		return fmt.Errorf("updating rule %s: %w", uuid, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("update rule returned %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// --- Internal API for label management ---

// Label is a rule label from the internal API.
type Label struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// internalBaseURL returns the base URL for the internal automation API scoped to a project.
func (c *Client) internalBaseURL(projectID string) string {
	return fmt.Sprintf("%s/gateway/api/automation/internal-api/jira/%s/pro/rest/%s",
		c.SiteURL, c.CloudID, projectID)
}

// ListLabels returns all rule labels for a project via the internal API.
func (c *Client) ListLabels(projectID string) ([]Label, error) {
	url := c.internalBaseURL(projectID) + "/rule-labels"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building list labels request: %w", err)
	}

	resp, err := c.do(req)
	if err != nil {
		return nil, fmt.Errorf("listing labels: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list labels returned %d: %s", resp.StatusCode, string(body))
	}

	var labels []Label
	if err := json.NewDecoder(resp.Body).Decode(&labels); err != nil {
		return nil, fmt.Errorf("decoding labels: %w", err)
	}

	return labels, nil
}

// AddLabelToRule associates a label with a rule via the internal API.
func (c *Client) AddLabelToRule(projectID, ruleUUID string, labelID int) error {
	url := fmt.Sprintf("%s/rules/%s/labels/%d", c.internalBaseURL(projectID), ruleUUID, labelID)
	req, err := http.NewRequest(http.MethodPut, url, nil)
	if err != nil {
		return fmt.Errorf("building add label request: %w", err)
	}

	resp, err := c.do(req)
	if err != nil {
		return fmt.Errorf("adding label to rule: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("add label returned %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// ExtractProjectID extracts the project ID from a scope ARI string.
// Format: ari:cloud:jira:{cloudId}:project/{projectId}
// Returns empty string if the ARI doesn't match the expected format.
func ExtractProjectID(ari string) string {
	const prefix = ":project/"
	idx := strings.LastIndex(ari, prefix)
	if idx == -1 {
		return ""
	}
	return ari[idx+len(prefix):]
}

// stripComponentIDs recursively removes "id" fields from components so the API
// assigns new IDs. Also nulls out parentId and conditionParentId since the
// parent-child relationships are expressed through nesting.
func stripComponentIDs(v interface{}) {
	m, ok := v.(map[string]interface{})
	if !ok {
		return
	}

	delete(m, "id")
	m["parentId"] = nil
	m["conditionParentId"] = nil

	if children, ok := m["children"].([]interface{}); ok {
		for _, child := range children {
			stripComponentIDs(child)
		}
	}
	if conditions, ok := m["conditions"].([]interface{}); ok {
		for _, cond := range conditions {
			stripComponentIDs(cond)
		}
	}
}

// SetRuleState enables or disables a rule.
func (c *Client) SetRuleState(uuid string, enabled bool) error {
	stateVal := "DISABLED"
	if enabled {
		stateVal = "ENABLED"
	}
	body, err := json.Marshal(SetRuleStateRequest{Value: stateVal})
	if err != nil {
		return fmt.Errorf("marshaling set rule state request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPut, c.BaseURL+"/rule/"+uuid+"/state", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("building set rule state request: %w", err)
	}

	resp, err := c.do(req)
	if err != nil {
		return fmt.Errorf("setting rule %s state: %w", uuid, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("set rule state returned %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}
