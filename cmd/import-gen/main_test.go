package main

import (
	"encoding/json"
	"strings"
	"testing"

	"terraform-provider-jira-automation/internal/client"
)

func TestExtractRuleIDFromURL(t *testing.T) {
	for _, test := range []struct {
		url  string
		want string
	}{
		{url: "https://jira.example.com/jira/settings/automate#/rule/42", want: "42"},
		{url: "https://example.atlassian.net/jira/settings/automate#/rule/01997721-1866-7233-9bb8-cec4a4614919", want: "01997721-1866-7233-9bb8-cec4a4614919"},
		{url: "https://jira.example.com/jira/settings/automate", want: ""},
	} {
		if got := extractRuleIDFromURL(test.url); got != test.want {
			t.Errorf("extractRuleIDFromURL(%q) = %q, want %q", test.url, got, test.want)
		}
	}
}

func TestGenerateDataCenterHCLIncludesProject(t *testing.T) {
	rule := &client.Rule{
		UUID:          "42",
		Name:          "DC rule",
		State:         "ENABLED",
		RuleScopeARIs: []string{"ari:cloud:jira::project/80647"},
		Trigger:       json.RawMessage(`{"type":"test"}`),
		Components:    []json.RawMessage{},
	}
	generated := generateHCL("dc_rule", rule)
	if !strings.Contains(generated, `project_id = "80647"`) {
		t.Fatalf("generated HCL missing project_id: %s", generated)
	}
}
