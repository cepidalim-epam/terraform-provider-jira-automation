package client

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewDataCenter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/api/2/serverInfo" {
			_, _ = fmt.Fprint(w, `{"deploymentType":"Server"}`)
			return
		}
		if r.URL.Path == "/rest/api/2/myself" {
			_, _ = fmt.Fprint(w, `{"key":"jira-user","name":"jira-user"}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	c, err := New(server.URL, "jira-user", "pat", "", "", nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if !c.IsDataCenter {
		t.Fatal("expected Data Center mode")
	}
	if c.BaseURL != server.URL+"/rest/cb-automation/latest" {
		t.Errorf("BaseURL = %q", c.BaseURL)
	}
	if c.UserKey != "jira-user" {
		t.Errorf("UserKey = %q, want jira-user", c.UserKey)
	}
}

func TestNewCloudFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/2/serverInfo":
			http.NotFound(w, r)
		case "/_edge/tenant_info":
			_, _ = fmt.Fprint(w, `{"cloudId":"cloud-123"}`)
		case "/rest/api/3/myself":
			_, _ = fmt.Fprint(w, `{"accountId":"account-123"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	c, err := New(server.URL, "user@example.com", "token", "", "", nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if c.IsDataCenter || c.CloudID != "cloud-123" {
		t.Fatalf("client mode/id = DataCenter:%t CloudID:%q", c.IsDataCenter, c.CloudID)
	}
	if want := "https://api.atlassian.com/automation/public/jira/cloud-123/rest/v1"; c.BaseURL != want {
		t.Errorf("BaseURL = %q, want %q", c.BaseURL, want)
	}
	if c.AccountID != "account-123" {
		t.Errorf("AccountID = %q, want account-123", c.AccountID)
	}
}

func TestListProjectRulesDataCenter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/cb-automation/latest/project/80647/rule" {
			t.Errorf("request path = %q", r.URL.Path)
		}
		_, _ = fmt.Fprint(w, `[{"id":42,"name":"test rule","state":"ENABLED"}]`)
	}))
	defer server.Close()

	c := &Client{
		BaseURL:      server.URL + "/rest/cb-automation/latest",
		IsDataCenter: true,
		HTTPClient:   server.Client(),
	}
	rules, err := c.ListProjectRules("80647")
	if err != nil {
		t.Fatalf("ListProjectRules() error = %v", err)
	}
	if len(rules) != 1 || rules[0].UUID != "42" || !rules[0].Enabled {
		t.Fatalf("ListProjectRules() = %#v", rules)
	}
}

func TestGetRuleDataCenterScope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/cb-automation/latest/project/80647/rule/42/export" {
			t.Errorf("request path = %q, want per-rule export route", r.URL.Path)
		}
		_, _ = fmt.Fprint(w, `{"cloud":false,"rules":[{"id":42,"name":"test rule","state":"ENABLED","projects":[{"projectId":"80647","projectTypeKey":"software"}],"labels":[12345,"managed-by:terraform"],"trigger":{},"components":[]}]}`)
	}))
	defer server.Close()

	c := &Client{
		BaseURL:      server.URL + "/rest/cb-automation/latest",
		ProjectID:    "80647",
		IsDataCenter: true,
		HTTPClient:   server.Client(),
	}
	rule, err := c.GetRule("42")
	if err != nil {
		t.Fatalf("GetRule() error = %v", err)
	}
	if rule.UUID != "42" || len(rule.RuleScopeARIs) != 1 || rule.RuleScopeARIs[0] != "80647" {
		t.Fatalf("GetRule() ID/scope = %q/%v", rule.UUID, rule.RuleScopeARIs)
	}
	if len(rule.Labels) != 2 || rule.Labels[0] != "12345" || rule.Labels[1] != "managed-by:terraform" {
		t.Fatalf("GetRule() labels = %v, want numeric ID and string label", rule.Labels)
	}
}

func TestCreateRuleDataCenter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/rest/api/2/project/80647" {
			if got := r.Header.Get("Authorization"); got != "Bearer test-pat" {
				t.Errorf("project lookup authorization = %q, want Bearer token", got)
			}
			_, _ = fmt.Fprint(w, `{"projectTypeKey":"software"}`)
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/rest/cb-automation/latest/project/80647/rule" {
			t.Errorf("request = %s %s, want POST to project rule route", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-pat" {
			t.Errorf("create authorization = %q, want Bearer token", got)
		}
		var payload map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decoding request body: %v", err)
		}
		if _, labeled := payload["labels"]; labeled {
			t.Error("Data Center create payload unexpectedly contains provider test labels")
		}
		var authorAccountID string
		if err := json.Unmarshal(payload["authorAccountId"], &authorAccountID); err != nil || authorAccountID != "jira-user" {
			t.Errorf("authorAccountId = %s, want jira-user", payload["authorAccountId"])
		}
		var state string
		if err := json.Unmarshal(payload["state"], &state); err != nil || state != "ENABLED" {
			t.Errorf("state = %s, want ENABLED", payload["state"])
		}
		var projects []struct {
			ProjectID      string `json:"projectId"`
			ProjectTypeKey string `json:"projectTypeKey"`
		}
		if err := json.Unmarshal(payload["projects"], &projects); err != nil || len(projects) != 1 || projects[0].ProjectID != "80647" || projects[0].ProjectTypeKey != "software" {
			t.Errorf("projects = %s, want software project 80647", payload["projects"])
		}
		for _, field := range []string{"isNewRule", "created", "updated"} {
			if _, exists := payload[field]; !exists {
				t.Errorf("Data Center create payload is missing field %q", field)
			}
		}
		for _, unusedField := range []string{"actorAccountId", "rule", "actor", "writeAccessType", "ruleScopeARIs", "ruleScope"} {
			if _, exists := payload[unusedField]; exists {
				t.Errorf("Data Center create payload unexpectedly contains field %q", unusedField)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"id":42}`)
	}))
	defer server.Close()

	c := &Client{
		BaseURL:      server.URL + "/rest/cb-automation/latest",
		ProjectID:    "80647",
		IsDataCenter: true,
		SiteURL:      server.URL,
		APIToken:     "test-pat",
		UserKey:      "jira-user",
		HTTPClient:   server.Client(),
	}
	id, err := c.CreateRule(CreateRuleRequest{
		Name:       "test rule",
		Enabled:    true,
		Trigger:    json.RawMessage(`{"type":"trigger"}`),
		Components: []json.RawMessage{json.RawMessage(`{"type":"action"}`)},
	})
	if err != nil {
		t.Fatalf("CreateRule() error = %v", err)
	}
	if id != "42" {
		t.Errorf("CreateRule() ID = %q, want 42", id)
	}
}

func TestUpdateRuleDataCenterPreservesExportMetadata(t *testing.T) {
	var putPayload map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			if r.URL.Path != "/rest/cb-automation/latest/project/80647/rule/15784/export" {
				t.Errorf("GET path = %q, want per-rule export route", r.URL.Path)
			}
			_, _ = fmt.Fprint(w, `{"cloud":false,"rules":[{"id":15784,"clientKey":"com.codebarrel.tenant.global","name":"test","state":"ENABLED","actorAccountId":"jira-user","authorAccountId":"jira-user","created":1793705609093,"updated":1793705609093,"trigger":{"id":"123353","component":"TRIGGER","schemaVersion":2,"type":"jira.issue.field.changed","value":{"actions":["create","edit"]},"children":[],"conditions":[]},"components":[{"id":"123354","component":"CONDITION","schemaVersion":3,"type":"jira.issue.condition","value":{"comparison":"ONE_OF"},"children":[],"conditions":[]},{"id":"123355","component":"ACTION","schemaVersion":6,"type":"jira.issue.edit","value":{"sendNotifications":true},"children":[],"conditions":[]}],"projects":[{"projectId":"80647","projectTypeKey":"software"}],"labels":[],"tags":[]}]}`)
			return
		}
		if r.Method != http.MethodPut || r.URL.Path != "/rest/cb-automation/latest/project/80647/rule/15784" {
			t.Errorf("request = %s %s, want PUT to project rule route", r.Method, r.URL.Path)
		}
		if r.URL.Query().Get("dryrun") != "false" {
			t.Errorf("PUT dryrun = %q, want false", r.URL.Query().Get("dryrun"))
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-pat" {
			t.Errorf("update authorization = %q, want Bearer token", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&putPayload); err != nil {
			t.Errorf("decoding update request: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	c := &Client{
		BaseURL:      server.URL + "/rest/cb-automation/latest",
		ProjectID:    "80647",
		IsDataCenter: true,
		APIToken:     "test-pat",
		HTTPClient:   server.Client(),
	}
	err := c.UpdateRule("15784", UpdateRuleRequest{
		Name:    "renamed",
		Enabled: false,
		Trigger: json.RawMessage(`{"component":"TRIGGER","type":"jira.issue.field.changed","value":{"actions":["create","edit"]},"children":[],"conditions":[]}`),
		Components: []json.RawMessage{
			json.RawMessage(`{"component":"CONDITION","type":"jira.issue.condition","value":{"comparison":"ONE_OF"},"children":[],"conditions":[]}`),
			json.RawMessage(`{"component":"ACTION","type":"jira.issue.edit","value":{"sendNotifications":true},"children":[],"conditions":[]}`),
		},
	})
	if err != nil {
		t.Fatalf("UpdateRule() error = %v", err)
	}

	for field, want := range map[string]interface{}{
		"id":             float64(15784),
		"clientKey":      "com.codebarrel.tenant.global",
		"created":        float64(1793705609093),
		"updated":        float64(1793705609093),
		"name":           "renamed",
		"state":          "DISABLED",
		"actorAccountId": "jira-user",
	} {
		if got := putPayload[field]; got != want {
			t.Errorf("PUT field %q = %#v, want %#v", field, got, want)
		}
	}
	trigger, _ := putPayload["trigger"].(map[string]interface{})
	if trigger["id"] != "123353" || trigger["schemaVersion"] != float64(2) {
		t.Errorf("PUT trigger metadata = %#v", trigger)
	}
	components, _ := putPayload["components"].([]interface{})
	if len(components) != 2 {
		t.Fatalf("PUT components count = %d, want 2", len(components))
	}
	wantIDs := []string{"123354", "123355"}
	wantSchemas := []float64{3, 6}
	for i, rawComponent := range components {
		component, _ := rawComponent.(map[string]interface{})
		if component["id"] != wantIDs[i] || component["schemaVersion"] != wantSchemas[i] {
			t.Errorf("PUT component %d metadata = %#v", i, component)
		}
	}
}

func TestSetRuleStateDataCenter(t *testing.T) {
	var putSeen bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			if r.URL.Path != "/rest/cb-automation/latest/project/80647/rule/42/export" {
				t.Errorf("GET path = %q, want per-rule export route", r.URL.Path)
			}
			_, _ = fmt.Fprint(w, `{"cloud":false,"rules":[{"id":42,"name":"test rule","state":"ENABLED","trigger":{},"components":[]}]}`)
			return
		}
		if r.Method != http.MethodPut || r.URL.Path != "/rest/cb-automation/latest/project/80647/rule/42" {
			t.Errorf("request = %s %s, want PUT to project rule route", r.Method, r.URL.Path)
		}
		if r.URL.Query().Get("dryrun") != "false" {
			t.Errorf("dryrun query = %q, want false", r.URL.Query().Get("dryrun"))
		}
		var payload struct {
			Name  string `json:"name"`
			State string `json:"state"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decoding state request: %v", err)
		}
		if payload.Name != "test rule" || payload.State != "DISABLED" {
			t.Errorf("state update payload = %#v", payload)
		}
		putSeen = true
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	c := &Client{
		BaseURL:      server.URL + "/rest/cb-automation/latest",
		ProjectID:    "80647",
		IsDataCenter: true,
		HTTPClient:   server.Client(),
	}
	if err := c.SetRuleState("42", false); err != nil {
		t.Fatalf("SetRuleState() error = %v", err)
	}
	if !putSeen {
		t.Fatal("expected state update request")
	}
}

func TestDeleteRuleDataCenter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/rest/cb-automation/latest/project/80647/rule/42" {
			t.Errorf("request = %s %s, want DELETE to project rule route", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	c := &Client{
		BaseURL:      server.URL + "/rest/cb-automation/latest",
		ProjectID:    "80647",
		IsDataCenter: true,
		HTTPClient:   server.Client(),
	}
	if err := c.DeleteRule("42"); err != nil {
		t.Fatalf("DeleteRule() error = %v", err)
	}
	if _, err := parseRuleID("not-numeric"); err == nil {
		t.Fatal("parseRuleID() accepted a nonnumeric ID")
	}
}

func TestParseProjectID(t *testing.T) {
	for _, test := range []struct {
		input string
		valid bool
	}{
		{input: "80647", valid: true},
		{input: "EPMASE", valid: false},
		{input: "0", valid: false},
		{input: "-1", valid: false},
	} {
		_, err := ParseProjectID(test.input)
		if (err == nil) != test.valid {
			t.Errorf("ParseProjectID(%q) error = %v, valid = %t", test.input, err, test.valid)
		}
	}
}
