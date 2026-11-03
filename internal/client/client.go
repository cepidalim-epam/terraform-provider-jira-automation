package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	BaseURL        string
	SiteURL        string
	CloudID        string
	IsDataCenter   bool
	ProjectID      string
	AccountID      string // Current user's Jira account ID, resolved at init.
	UserKey        string
	Email          string
	APIToken       string
	WebhookUser    string
	WebhookToken   string
	HTTPClient     *http.Client
	FieldAliases   map[string]string // alias → fieldID
	ReverseAliases map[string]string // fieldID → alias
}

type APIError struct {
	StatusCode int
	Operation  string
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("%s returned %d: %s", e.Operation, e.StatusCode, e.Body)
}

// IsNotFoundError reports whether an API operation returned HTTP 404.
func IsNotFoundError(err error) bool {
	var apiError *APIError
	return errors.As(err, &apiError) && apiError.StatusCode == http.StatusNotFound
}

// TenantInfo is the response from /_edge/tenant_info.
type TenantInfo struct {
	CloudID string `json:"cloudId"`
}

// RuleSummary is a single entry returned by GET /rule/summary.
type RuleSummary struct {
	UUID    string `json:"uuid"`
	ID      string `json:"id,omitempty"`
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
	UUID          string          `json:"uuid,omitempty"`
	ID            string          `json:"id,omitempty"`
	Name          string          `json:"name"`
	State         string          `json:"state,omitempty"`
	Enabled       *bool           `json:"enabled,omitempty"`
	RuleScope     json.RawMessage `json:"ruleScope,omitempty"`
	RuleScopeARIs []string        `json:"ruleScopeARIs,omitempty"`
	Projects      []struct {
		ProjectID string `json:"projectId"`
	} `json:"projects,omitempty"`
	Labels     []string          `json:"labels,omitempty"`
	Trigger    json.RawMessage   `json:"trigger"`
	Components []json.RawMessage `json:"components"`
}

// GetRuleRaw returns the raw JSON for a rule (without the envelope).
func (c *Client) GetRuleRaw(uuid string) (json.RawMessage, error) {
	if err := c.requireProjectID(); err != nil {
		return nil, err
	}
	url := c.ruleURL(uuid)
	if c.IsDataCenter {
		url += "/export"
	}
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
		return nil, &APIError{StatusCode: resp.StatusCode, Operation: "get rule", Body: string(body)}
	}

	if c.IsDataCenter {
		var exported struct {
			Rules []json.RawMessage `json:"rules"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&exported); err != nil {
			return nil, fmt.Errorf("decoding exported rule: %w", err)
		}
		for _, rule := range exported.Rules {
			var fields struct {
				ID json.RawMessage `json:"id"`
			}
			if err := json.Unmarshal(rule, &fields); err != nil {
				return nil, fmt.Errorf("decoding exported rule ID: %w", err)
			}
			id, err := decodeRuleID(fields.ID)
			if err != nil {
				return nil, fmt.Errorf("decoding exported rule ID: %w", err)
			}
			if id == uuid {
				return rule, nil
			}
		}
		return nil, &APIError{StatusCode: http.StatusNotFound, Operation: "get rule", Body: fmt.Sprintf("rule %s not found in export", uuid)}
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
	Enabled    bool
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
	Enabled    bool              `json:"-"`
	Trigger    json.RawMessage   `json:"trigger"`
	Components []json.RawMessage `json:"components"`
}

// SetRuleStateRequest is the payload for PUT /rule/{uuid}/state.
type SetRuleStateRequest struct {
	Value string `json:"value"`
}

// New creates a client for Jira Cloud or Jira Server/Data Center.
// aliases maps friendly names to Jira field IDs (e.g. "release_version" → "customfield_10709").
// Pass nil for no aliases.
func New(siteURL, email, apiToken, webhookUser, webhookToken string, aliases map[string]string) (*Client, error) {
	httpClient := &http.Client{Timeout: 30 * time.Second}
	if !strings.HasPrefix(siteURL, "http://") && !strings.HasPrefix(siteURL, "https://") {
		siteURL = "https://" + siteURL
	}
	siteURL = strings.TrimRight(siteURL, "/")

	// Server/Data Center exposes authenticated serverInfo; Cloud uses tenant_info.
	serverInfoURL := siteURL + "/rest/api/2/serverInfo"
	serverInfoReq, err := http.NewRequest(http.MethodGet, serverInfoURL, nil)
	if err != nil {
		return nil, fmt.Errorf("building server info request: %w", err)
	}
	serverInfoReq.SetBasicAuth(email, apiToken)
	serverInfoResp, err := httpClient.Do(serverInfoReq)
	if err != nil {
		return nil, fmt.Errorf("fetching Jira server info from %s: %w", serverInfoURL, err)
	}
	defer serverInfoResp.Body.Close()

	isDataCenter := false
	cloudID := ""
	baseURL := ""
	if serverInfoResp.StatusCode == http.StatusOK {
		var serverInfo struct {
			DeploymentType string `json:"deploymentType"`
		}
		if err := json.NewDecoder(serverInfoResp.Body).Decode(&serverInfo); err != nil {
			return nil, fmt.Errorf("decoding Jira server info: %w", err)
		}
		isDataCenter = serverInfo.DeploymentType == "Server" || serverInfo.DeploymentType == "Data Center"
	}

	if isDataCenter {
		baseURL = siteURL + "/rest/cb-automation/latest"
	} else {
		// Cloud serverInfo may be unavailable or omit a deployment type, so use the
		// tenant endpoint as the definitive Cloud check.
		tenantURL := siteURL + "/_edge/tenant_info"
		tenantReq, err := http.NewRequest(http.MethodGet, tenantURL, nil)
		if err != nil {
			return nil, fmt.Errorf("building tenant info request: %w", err)
		}
		tenantResp, err := httpClient.Do(tenantReq)
		if err != nil {
			return nil, fmt.Errorf("fetching tenant info from %s: %w", tenantURL, err)
		}
		defer tenantResp.Body.Close()
		if tenantResp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(tenantResp.Body)
			return nil, fmt.Errorf("Jira server info returned %d and tenant info returned %d: %s", serverInfoResp.StatusCode, tenantResp.StatusCode, string(body))
		}
		var tenant TenantInfo
		if err := json.NewDecoder(tenantResp.Body).Decode(&tenant); err != nil {
			return nil, fmt.Errorf("decoding tenant info: %w", err)
		}
		if tenant.CloudID == "" {
			return nil, fmt.Errorf("empty cloudId from tenant info")
		}
		cloudID = tenant.CloudID
		baseURL = fmt.Sprintf("https://api.atlassian.com/automation/public/jira/%s/rest/v1", cloudID)
	}

	// Resolve the current user's account ID (Cloud) or user key/name (Data Center).
	myselfURL := siteURL + "/rest/api/3/myself"
	if isDataCenter {
		myselfURL = siteURL + "/rest/api/2/myself"
	}
	myselfReq, err := http.NewRequest(http.MethodGet, myselfURL, nil)
	if err != nil {
		return nil, fmt.Errorf("building myself request: %w", err)
	}
	if isDataCenter {
		myselfReq.Header.Set("Authorization", "Bearer "+apiToken)
	} else {
		myselfReq.SetBasicAuth(email, apiToken)
	}
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
		Key       string `json:"key"`
		Name      string `json:"name"`
	}
	if err := json.NewDecoder(myselfResp.Body).Decode(&myself); err != nil {
		return nil, fmt.Errorf("decoding myself response: %w", err)
	}
	userKey := myself.AccountID
	if isDataCenter {
		userKey = myself.Key
		if userKey == "" {
			userKey = myself.Name
		}
	}
	if userKey == "" {
		return nil, fmt.Errorf("empty user identifier from %s", myselfURL)
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
		CloudID:        cloudID,
		IsDataCenter:   isDataCenter,
		AccountID:      myself.AccountID,
		UserKey:        userKey,
		Email:          email,
		APIToken:       apiToken,
		WebhookUser:    webhookUser,
		WebhookToken:   webhookToken,
		HTTPClient:     httpClient,
		FieldAliases:   aliases,
		ReverseAliases: reverse,
	}, nil
}

// ParseProjectID validates and returns a Jira project's numeric ID.
func ParseProjectID(projectID string) (string, error) {
	parsed, err := strconv.ParseInt(projectID, 10, 64)
	if err != nil || parsed <= 0 {
		return "", fmt.Errorf("project ID %q must be a positive numeric Jira project ID", projectID)
	}
	return strconv.FormatInt(parsed, 10), nil
}

func (c *Client) do(req *http.Request) (*http.Response, error) {
	if c.IsDataCenter {
		req.Header.Set("Authorization", "Bearer "+c.APIToken)
	} else {
		req.SetBasicAuth(c.Email, c.APIToken)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	return c.HTTPClient.Do(req)
}

// ListRules returns all rules for the configured Data Center project, or all
// Cloud rules for a Cloud tenant.
func (c *Client) ListRules() ([]RuleSummary, error) {
	if c.IsDataCenter {
		if c.ProjectID == "" {
			return nil, fmt.Errorf("jira_id (project ID) is required to list Jira Data Center automation rules")
		}
		return c.ListProjectRules(c.ProjectID)
	}

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
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("list rules returned %d: %s", resp.StatusCode, string(body))
		}

		var page ListRulesResponse
		if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("decoding list rules response: %w", err)
		}
		resp.Body.Close()

		all = append(all, page.Data...)

		if page.Cursor == nil || *page.Cursor == "" {
			break
		}
		url = c.BaseURL + "/rule/summary?cursor=" + *page.Cursor
	}

	return all, nil
}

// ListProjectRules returns rule summaries for a Jira Data Center project.
func (c *Client) ListProjectRules(projectID string) ([]RuleSummary, error) {
	if _, err := ParseProjectID(projectID); err != nil {
		return nil, err
	}
	url := c.projectBaseURL(projectID) + "/rule"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building list rules request: %w", err)
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, fmt.Errorf("listing rules for project %s: %w", projectID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list rules returned %d: %s", resp.StatusCode, string(body))
	}

	var rules []struct {
		ID      json.RawMessage `json:"id"`
		Name    string          `json:"name"`
		State   string          `json:"state"`
		Enabled *bool           `json:"enabled"`
	}
	decoder := json.NewDecoder(resp.Body)
	decoder.UseNumber()
	if err := decoder.Decode(&rules); err != nil {
		return nil, fmt.Errorf("decoding list rules response: %w", err)
	}

	result := make([]RuleSummary, 0, len(rules))
	for _, rule := range rules {
		id, err := decodeRuleID(rule.ID)
		if err != nil {
			return nil, fmt.Errorf("decoding rule ID: %w", err)
		}
		if id == "" {
			return nil, fmt.Errorf("list rules response contained a rule without an ID")
		}
		enabled := rule.State == "ENABLED" || (rule.Enabled != nil && *rule.Enabled)
		state := rule.State
		if state == "" && rule.Enabled != nil {
			state = "DISABLED"
			if enabled {
				state = "ENABLED"
			}
		}
		result = append(result, RuleSummary{
			UUID:    id,
			ID:      id,
			Name:    rule.Name,
			State:   state,
			Enabled: enabled,
		})
	}
	return result, nil
}

// GetRule returns the full rule config for a given UUID.
func (c *Client) GetRule(uuid string) (*Rule, error) {
	raw, err := c.GetRuleRaw(uuid)
	if err != nil {
		return nil, err
	}

	if c.IsDataCenter {
		raw, err = normalizeDataCenterRuleID(raw)
		if err != nil {
			return nil, fmt.Errorf("normalizing Data Center rule ID: %w", err)
		}
	}

	var wireRule struct {
		UUID          string          `json:"uuid,omitempty"`
		ID            string          `json:"id,omitempty"`
		Name          string          `json:"name"`
		State         string          `json:"state,omitempty"`
		Enabled       *bool           `json:"enabled,omitempty"`
		RuleScope     json.RawMessage `json:"ruleScope,omitempty"`
		RuleScopeARIs []string        `json:"ruleScopeARIs,omitempty"`
		Projects      []struct {
			ProjectID string `json:"projectId"`
		} `json:"projects,omitempty"`
		Labels     []json.RawMessage `json:"labels,omitempty"`
		Trigger    json.RawMessage   `json:"trigger"`
		Components []json.RawMessage `json:"components"`
	}
	if err := json.Unmarshal(raw, &wireRule); err != nil {
		return nil, fmt.Errorf("decoding rule: %w", err)
	}
	labels, err := decodeRuleLabels(wireRule.Labels)
	if err != nil {
		return nil, fmt.Errorf("decoding rule labels: %w", err)
	}
	rule := Rule{
		UUID:          wireRule.UUID,
		ID:            wireRule.ID,
		Name:          wireRule.Name,
		State:         wireRule.State,
		Enabled:       wireRule.Enabled,
		RuleScope:     wireRule.RuleScope,
		RuleScopeARIs: wireRule.RuleScopeARIs,
		Projects:      wireRule.Projects,
		Labels:        labels,
		Trigger:       wireRule.Trigger,
		Components:    wireRule.Components,
	}

	if c.IsDataCenter {
		if rule.UUID == "" {
			rule.UUID = rule.ID
		}
		if rule.State == "" && rule.Enabled != nil {
			rule.State = "DISABLED"
			if *rule.Enabled {
				rule.State = "ENABLED"
			}
		}
		if len(rule.RuleScopeARIs) == 0 && len(rule.RuleScope) > 0 {
			var scope struct {
				Resources json.RawMessage `json:"resources"`
			}
			if err := json.Unmarshal(rule.RuleScope, &scope); err == nil {
				var resources []string
				if err := json.Unmarshal(scope.Resources, &resources); err == nil {
					rule.RuleScopeARIs = resources
				} else {
					var resource string
					if err := json.Unmarshal(scope.Resources, &resource); err == nil && resource != "" {
						rule.RuleScopeARIs = []string{resource}
					}
				}
			}
		}
		if len(rule.RuleScopeARIs) == 0 && len(rule.Projects) > 0 {
			for _, project := range rule.Projects {
				if project.ProjectID != "" {
					rule.RuleScopeARIs = append(rule.RuleScopeARIs, project.ProjectID)
				}
			}
		}
	}

	return &rule, nil
}

func decodeRuleLabels(rawLabels []json.RawMessage) ([]string, error) {
	labels := make([]string, 0, len(rawLabels))
	for _, rawLabel := range rawLabels {
		var label string
		if err := json.Unmarshal(rawLabel, &label); err == nil {
			labels = append(labels, label)
			continue
		}
		var number json.Number
		decoder := json.NewDecoder(bytes.NewReader(rawLabel))
		decoder.UseNumber()
		if err := decoder.Decode(&number); err == nil {
			labels = append(labels, number.String())
			continue
		}
		return nil, fmt.Errorf("label %s is neither a string nor a number", rawLabel)
	}
	return labels, nil
}

func normalizeDataCenterRuleID(raw json.RawMessage) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	id, ok := fields["id"]
	if !ok {
		return raw, nil
	}

	var stringID string
	if err := json.Unmarshal(id, &stringID); err == nil {
		return raw, nil
	}

	decoder := json.NewDecoder(bytes.NewReader(id))
	decoder.UseNumber()
	var numberID json.Number
	if err := decoder.Decode(&numberID); err != nil {
		return nil, fmt.Errorf("decoding numeric rule ID: %w", err)
	}
	fields["id"], _ = json.Marshal(numberID.String())
	normalized, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	return normalized, nil
}

// CreateRule creates a new automation rule and returns the UUID.
// The Cloud API requires several fields beyond name/trigger/components:
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
	var dcProjectID string
	if c.IsDataCenter {
		dcProjectID = c.ProjectID
		if _, err := ParseProjectID(dcProjectID); err != nil {
			return "", fmt.Errorf("jira_id is required for Jira Data Center rule operations: %w", err)
		}
		if rule.ProjectID != "" && rule.ProjectID != dcProjectID {
			return "", fmt.Errorf("resource project_id %q must match provider jira_id %q for Jira Data Center", rule.ProjectID, dcProjectID)
		}
	} else if rule.ProjectID != "" {
		scopeARIs = []string{
			fmt.Sprintf("ari:cloud:jira:%s:project/%s", c.CloudID, rule.ProjectID),
		}
	} else {
		scopeARIs = []string{
			fmt.Sprintf("ari:cloud:jira::site/%s", c.CloudID),
		}
	}

	// Build the full rule payload with all required fields.
	state := "DISABLED"
	if rule.Enabled {
		state = "ENABLED"
	}
	payload := map[string]interface{}{
		"name":                rule.Name,
		"state":               state,
		"notifyOnError":       "FIRSTERROR",
		"canOtherRuleTrigger": false,
		"authorAccountId":     c.AccountID,
		"actor":               map[string]string{"type": "ACCOUNT_ID", "actor": c.AccountID},
		"writeAccessType":     "OWNER_ONLY",
		"trigger":             trigger,
		"components":          components,
		"ruleScopeARIs":       scopeARIs,
	}

	var requestPayload interface{} = map[string]interface{}{"rule": payload}
	if c.IsDataCenter {
		payload["authorAccountId"] = c.UserKey
		projectTypeKey := "software"
		if c.SiteURL != "" {
			var err error
			projectTypeKey, err = c.getDataCenterProjectTypeKey(dcProjectID)
			if err != nil {
				return "", err
			}
		}
		now := time.Now().UnixMilli()
		payload["isNewRule"] = true
		payload["created"] = now
		payload["updated"] = now
		payload["projects"] = []map[string]string{{"projectId": dcProjectID, "projectTypeKey": projectTypeKey}}
		delete(payload, "actorAccountId")
		delete(payload, "actor")
		delete(payload, "writeAccessType")
		delete(payload, "ruleScopeARIs")
		requestPayload = payload
	}
	body, err := json.Marshal(requestPayload)
	if err != nil {
		return "", fmt.Errorf("marshaling create rule request: %w", err)
	}

	url := c.BaseURL + "/rule"
	if c.IsDataCenter {
		url = c.projectBaseURL(dcProjectID) + "/rule"
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
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

	if c.IsDataCenter {
		var result struct {
			ID     json.RawMessage `json:"id"`
			RuleID json.RawMessage `json:"ruleId"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return "", fmt.Errorf("decoding create rule response: %w", err)
		}
		id, err := decodeRuleID(result.ID)
		if err != nil {
			return "", fmt.Errorf("decoding create rule ID: %w", err)
		}
		if id != "" {
			return id, nil
		}
		id, err = decodeRuleID(result.RuleID)
		if err != nil {
			return "", fmt.Errorf("decoding create rule ID: %w", err)
		}
		if id == "" {
			return "", fmt.Errorf("create rule response did not include a rule ID")
		}
		return id, nil
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

func (c *Client) getDataCenterProjectTypeKey(projectID string) (string, error) {
	url := c.SiteURL + "/rest/api/2/project/" + projectID
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("building Jira project request: %w", err)
	}
	resp, err := c.do(req)
	if err != nil {
		return "", fmt.Errorf("getting Jira project %s: %w", projectID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("getting Jira project %s returned %d: %s", projectID, resp.StatusCode, string(body))
	}
	var project struct {
		ProjectTypeKey string `json:"projectTypeKey"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&project); err != nil {
		return "", fmt.Errorf("decoding Jira project %s: %w", projectID, err)
	}
	if project.ProjectTypeKey == "" {
		return "", fmt.Errorf("Jira project %s did not include projectTypeKey", projectID)
	}
	return project.ProjectTypeKey, nil
}

func decodeRuleID(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	var id string
	if err := json.Unmarshal(raw, &id); err == nil {
		return id, nil
	}
	var number json.Number
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&number); err != nil {
		return "", err
	}
	return number.String(), nil
}

// UpdateRule updates an existing automation rule.
// It performs a read-modify-write: fetches the current rule to get all API fields,
// merges in the Terraform-managed fields, strips component IDs (so the API recreates
// them), and PUTs the complete rule wrapped in the required {"rule": ...} envelope.
func (c *Client) UpdateRule(uuid string, update UpdateRuleRequest) error {
	if c.IsDataCenter {
		if err := c.requireProjectID(); err != nil {
			return err
		}
		if _, err := parseRuleID(uuid); err != nil {
			return err
		}
	}
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

	// Cloud rejects server-generated fields on write; Data Center's working
	// project API update format includes the exported ID and timestamps.
	delete(ruleMap, "uuid")
	if !c.IsDataCenter {
		delete(ruleMap, "created")
		delete(ruleMap, "updated")
	} else {
		delete(ruleMap, "enabled")
	}

	// 4. Merge Terraform-managed fields.
	ruleMap["name"] = update.Name
	if c.IsDataCenter {
		ruleMap["state"] = "DISABLED"
		if update.Enabled {
			ruleMap["state"] = "ENABLED"
		}
	}

	var trigger interface{}
	if err := json.Unmarshal(update.Trigger, &trigger); err != nil {
		return fmt.Errorf("parsing trigger: %w", err)
	}
	if c.IsDataCenter {
		trigger = mergeDataCenterComponent(ruleMap["trigger"], trigger)
	} else {
		stripComponentIDs(trigger)
	}
	ruleMap["trigger"] = trigger

	var components []interface{}
	for _, comp := range update.Components {
		var component interface{}
		if err := json.Unmarshal(comp, &component); err != nil {
			return fmt.Errorf("parsing component: %w", err)
		}
		if c.IsDataCenter {
			component = mergeDataCenterComponent(existingComponentAt(ruleMap["components"], len(components)), component)
		} else {
			stripComponentIDs(component)
		}
		components = append(components, component)
	}
	ruleMap["components"] = components

	// 5. Wrap Cloud updates in the required {"rule": ...} envelope.
	var requestPayload interface{} = map[string]interface{}{"rule": ruleMap}
	if c.IsDataCenter {
		requestPayload = ruleMap
	}
	body, err := json.Marshal(requestPayload)
	if err != nil {
		return fmt.Errorf("marshaling update rule request: %w", err)
	}

	url := c.ruleURL(uuid)
	if c.IsDataCenter {
		url += "?dryrun=false"
	}
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(body))
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

func existingComponentAt(raw interface{}, index int) interface{} {
	components, ok := raw.([]interface{})
	if !ok || index < 0 || index >= len(components) {
		return nil
	}
	return components[index]
}

func mergeDataCenterComponent(existing, planned interface{}) interface{} {
	existingMap, existingIsMap := existing.(map[string]interface{})
	plannedMap, plannedIsMap := planned.(map[string]interface{})
	if !existingIsMap || !plannedIsMap {
		return planned
	}
	if existingMap["type"] != plannedMap["type"] {
		return planned
	}

	merged := make(map[string]interface{}, len(existingMap)+len(plannedMap))
	for key, value := range existingMap {
		merged[key] = value
	}
	for key, value := range plannedMap {
		merged[key] = mergeDataCenterComponent(existingMap[key], value)
	}
	return merged
}

// --- Internal API for label management ---

// Label is a rule label from the internal API.
type Label struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// internalBaseURL returns the base URL for the internal automation API scoped to a project.
func (c *Client) internalBaseURL(projectID string) string {
	if c.IsDataCenter {
		return c.projectBaseURL(projectID)
	}
	return fmt.Sprintf("%s/gateway/api/automation/internal-api/jira/%s/pro/rest/%s",
		c.SiteURL, c.CloudID, projectID)
}

func (c *Client) projectBaseURL(projectID string) string {
	return fmt.Sprintf("%s/project/%s", c.BaseURL, projectID)
}

func (c *Client) ruleURL(ruleID string) string {
	if c.IsDataCenter {
		if c.ProjectID == "" {
			return ""
		}
		return c.projectBaseURL(c.ProjectID) + "/rule/" + ruleID
	}
	return c.BaseURL + "/rule/" + ruleID
}

func (c *Client) requireProjectID() error {
	if !c.IsDataCenter {
		return nil
	}
	if _, err := ParseProjectID(c.ProjectID); err != nil {
		return fmt.Errorf("jira_id is required for Jira Data Center rule operations: %w", err)
	}
	return nil
}

func parseRuleID(ruleID string) (int64, error) {
	parsed, err := strconv.ParseInt(ruleID, 10, 64)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("rule ID %q must be a positive numeric Jira Data Center automation rule ID", ruleID)
	}
	return parsed, nil
}

// ListLabels returns all rule labels for a project via the internal API.
func (c *Client) ListLabels(projectID string) ([]Label, error) {
	if c.IsDataCenter {
		if _, err := ParseProjectID(projectID); err != nil {
			return nil, err
		}
	}
	url := c.internalBaseURL(projectID) + "/rule-labels"
	if c.IsDataCenter {
		url = c.projectBaseURL(projectID) + "/rule-labels"
	}
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
	if c.IsDataCenter {
		if _, err := ParseProjectID(projectID); err != nil {
			return err
		}
	}
	url := fmt.Sprintf("%s/rules/%s/labels/%d", c.internalBaseURL(projectID), ruleUUID, labelID)
	if c.IsDataCenter {
		url = fmt.Sprintf("%s/rule/%s/label/%d", c.projectBaseURL(projectID), ruleUUID, labelID)
	}
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
	if c.IsDataCenter {
		if _, err := parseRuleID(uuid); err != nil {
			return err
		}
		return c.updateDataCenterRuleState(uuid, enabled)
	}

	stateVal := "DISABLED"
	if enabled {
		stateVal = "ENABLED"
	}
	body, err := json.Marshal(SetRuleStateRequest{Value: stateVal})
	if err != nil {
		return fmt.Errorf("marshaling set rule state request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPut, c.ruleURL(uuid)+"/state", bytes.NewReader(body))
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

func (c *Client) updateDataCenterRuleState(ruleID string, enabled bool) error {
	raw, err := c.GetRuleRaw(ruleID)
	if err != nil {
		return fmt.Errorf("reading rule before changing state: %w", err)
	}
	var rule map[string]interface{}
	if err := json.Unmarshal(raw, &rule); err != nil {
		return fmt.Errorf("parsing rule before changing state: %w", err)
	}
	delete(rule, "uuid")
	delete(rule, "id")
	delete(rule, "created")
	delete(rule, "updated")
	state := "DISABLED"
	if enabled {
		state = "ENABLED"
	}
	rule["state"] = state
	body, err := json.Marshal(rule)
	if err != nil {
		return fmt.Errorf("marshaling rule state update: %w", err)
	}
	req, err := http.NewRequest(http.MethodPut, c.ruleURL(ruleID)+"?dryrun=false", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("building rule state update request: %w", err)
	}
	resp, err := c.do(req)
	if err != nil {
		return fmt.Errorf("updating rule state: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("update rule state returned %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// DeleteRule deletes a Jira Data Center automation rule. Cloud's public API
// doesn't support rule deletion, so provider Delete continues disabling there.
func (c *Client) DeleteRule(ruleID string) error {
	if !c.IsDataCenter {
		return fmt.Errorf("deleting Jira Cloud automation rules is not supported")
	}
	if _, err := parseRuleID(ruleID); err != nil {
		return err
	}
	if err := c.requireProjectID(); err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodDelete, c.ruleURL(ruleID), nil)
	if err != nil {
		return fmt.Errorf("building delete rule request: %w", err)
	}
	resp, err := c.do(req)
	if err != nil {
		return fmt.Errorf("deleting rule %s: %w", ruleID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete rule returned %d: %s", resp.StatusCode, string(body))
	}
	return nil
}
