package provider

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestResolveAliases(t *testing.T) {
	aliases := map[string]string{
		"release_version": "customfield_10709",
		"sprint":          "customfield_10020",
	}

	args := map[string]string{
		"version_field": "release_version",
		"url":           "https://example.com/{{issue.release_version}}/details",
		"other":         "no alias here",
	}

	got := resolveAliases(args, aliases)

	// Bare value replacement.
	if got["version_field"] != "customfield_10709" {
		t.Errorf("version_field: got %q, want %q", got["version_field"], "customfield_10709")
	}
	// Smart value replacement.
	if got["url"] != "https://example.com/{{issue.customfield_10709}}/details" {
		t.Errorf("url: got %q, want %q", got["url"], "https://example.com/{{issue.customfield_10709}}/details")
	}
	// No-op for non-aliases.
	if got["other"] != "no alias here" {
		t.Errorf("other: got %q, want %q", got["other"], "no alias here")
	}
}

func TestResolveAliases_Empty(t *testing.T) {
	args := map[string]string{"key": "value"}

	// nil aliases should be a no-op.
	got := resolveAliases(args, nil)
	if got["key"] != "value" {
		t.Errorf("nil aliases: got %q, want %q", got["key"], "value")
	}

	// Empty aliases should be a no-op.
	got = resolveAliases(args, map[string]string{})
	if got["key"] != "value" {
		t.Errorf("empty aliases: got %q, want %q", got["key"], "value")
	}
}

func TestUnresolveAliases(t *testing.T) {
	reverse := map[string]string{
		"customfield_10709": "release_version",
		"customfield_10020": "sprint",
	}

	args := map[string]string{
		"version_field": "customfield_10709",
		"url":           "https://example.com/{{issue.customfield_10709}}/details",
	}

	got := unresolveAliases(args, reverse)

	if got["version_field"] != "release_version" {
		t.Errorf("version_field: got %q, want %q", got["version_field"], "release_version")
	}
	if got["url"] != "https://example.com/{{issue.release_version}}/details" {
		t.Errorf("url: got %q, want %q", got["url"], "https://example.com/{{issue.release_version}}/details")
	}
}

func TestBuildLog(t *testing.T) {
	raw, err := buildLog(map[string]string{"message": "hello world"}, "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var action map[string]interface{}
	if err := json.Unmarshal(raw, &action); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if action["type"] != "codebarrel.action.log" {
		t.Errorf("type: got %q, want %q", action["type"], "codebarrel.action.log")
	}
	if action["value"] != "hello world" {
		t.Errorf("value: got %q, want %q", action["value"], "hello world")
	}
	if action["component"] != "ACTION" {
		t.Errorf("component: got %q, want %q", action["component"], "ACTION")
	}
}

func TestBuildLog_MissingMessage(t *testing.T) {
	_, err := buildLog(map[string]string{}, "", "", "")
	if err == nil {
		t.Fatal("expected error for missing message")
	}
	if !strings.Contains(err.Error(), "message") {
		t.Errorf("error should mention 'message': %v", err)
	}
}

func TestBuildComment(t *testing.T) {
	raw, err := buildComment(map[string]string{"message": "a comment"}, "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var action map[string]interface{}
	if err := json.Unmarshal(raw, &action); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if action["type"] != "jira.issue.comment" {
		t.Errorf("type: got %q, want %q", action["type"], "jira.issue.comment")
	}

	value, ok := action["value"].(map[string]interface{})
	if !ok {
		t.Fatal("value is not a map")
	}
	if value["comment"] != "a comment" {
		t.Errorf("comment: got %q, want %q", value["comment"], "a comment")
	}
}

func TestBuildAddReleaseRelatedWork(t *testing.T) {
	args := map[string]string{
		"version_field": "customfield_10709",
		"category":      "other",
		"title":         "Deploy {{issue.key}}",
		"url":           "https://example.com/{{issue.key}}",
	}

	raw, err := buildAddReleaseRelatedWork(args, "cloud-123", "user@test.com", "token123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var action map[string]interface{}
	if err := json.Unmarshal(raw, &action); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if action["type"] != "jira.issue.outgoing.webhook" {
		t.Errorf("type: got %q, want %q", action["type"], "jira.issue.outgoing.webhook")
	}

	value, ok := action["value"].(map[string]interface{})
	if !ok {
		t.Fatal("value is not a map")
	}

	// Verify webhook URL pattern.
	url := value["url"].(string)
	if !strings.Contains(url, "cloud-123") {
		t.Errorf("url should contain cloudID: %s", url)
	}
	if !strings.Contains(url, "customfield_10709") {
		t.Errorf("url should contain version field: %s", url)
	}
	if !strings.HasSuffix(url, "/relatedwork") {
		t.Errorf("url should end with /relatedwork: %s", url)
	}

	// Verify auth header.
	headers, ok := value["headers"].([]interface{})
	if !ok || len(headers) == 0 {
		t.Fatal("missing headers")
	}
	header := headers[0].(map[string]interface{})
	if header["name"] != "Authorization" {
		t.Errorf("header name: got %q, want %q", header["name"], "Authorization")
	}
	authValue := header["value"].(string)
	if !strings.HasPrefix(authValue, "Basic ") {
		t.Errorf("auth header should start with 'Basic ': %s", authValue)
	}

	// Verify custom body.
	var body map[string]string
	if err := json.Unmarshal([]byte(value["customBody"].(string)), &body); err != nil {
		t.Fatalf("invalid customBody JSON: %v", err)
	}
	if body["category"] != "other" {
		t.Errorf("category: got %q, want %q", body["category"], "other")
	}
	if body["title"] != "Deploy {{issue.key}}" {
		t.Errorf("title: got %q, want %q", body["title"], "Deploy {{issue.key}}")
	}
}

func TestBuildConditionJSON(t *testing.T) {
	thenAction, _ := buildLog(map[string]string{"message": "then"}, "", "", "")
	elseAction, _ := buildLog(map[string]string{"message": "else"}, "", "", "")

	condArgs := map[string]string{
		"first":    "{{issue.status.name}}",
		"operator": "equals",
		"second":   "Done",
	}

	raw, err := BuildConditionJSON(condArgs, []json.RawMessage{thenAction}, []json.RawMessage{elseAction})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var container map[string]interface{}
	if err := json.Unmarshal(raw, &container); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// Outer container.
	if container["type"] != "jira.condition.container.block" {
		t.Errorf("container type: got %q, want %q", container["type"], "jira.condition.container.block")
	}
	if container["component"] != "CONDITION" {
		t.Errorf("container component: got %q, want %q", container["component"], "CONDITION")
	}

	children, ok := container["children"].([]interface{})
	if !ok || len(children) != 2 {
		t.Fatalf("expected 2 children (IF + ELSE), got %d", len(children))
	}

	// IF block.
	ifBlock := children[0].(map[string]interface{})
	if ifBlock["component"] != "CONDITION_BLOCK" {
		t.Errorf("IF block component: got %q, want %q", ifBlock["component"], "CONDITION_BLOCK")
	}
	ifConditions := ifBlock["conditions"].([]interface{})
	if len(ifConditions) != 1 {
		t.Fatalf("IF block should have 1 condition, got %d", len(ifConditions))
	}

	// Comparator.
	comparator := ifConditions[0].(map[string]interface{})
	if comparator["type"] != "jira.comparator.condition" {
		t.Errorf("comparator type: got %q, want %q", comparator["type"], "jira.comparator.condition")
	}
	compValue := comparator["value"].(map[string]interface{})
	if compValue["first"] != "{{issue.status.name}}" {
		t.Errorf("first: got %q, want %q", compValue["first"], "{{issue.status.name}}")
	}

	// THEN children.
	ifChildren := ifBlock["children"].([]interface{})
	if len(ifChildren) != 1 {
		t.Errorf("IF block should have 1 child, got %d", len(ifChildren))
	}

	// ELSE block.
	elseBlock := children[1].(map[string]interface{})
	elseChildren := elseBlock["children"].([]interface{})
	if len(elseChildren) != 1 {
		t.Errorf("ELSE block should have 1 child, got %d", len(elseChildren))
	}
}

func TestParseLog_RoundTrip(t *testing.T) {
	raw, err := buildLog(map[string]string{"message": "test log"}, "", "", "")
	if err != nil {
		t.Fatalf("build error: %v", err)
	}

	args, err := parseLog(raw)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if args["message"] != "test log" {
		t.Errorf("message: got %q, want %q", args["message"], "test log")
	}
}

func TestParseComment_RoundTrip(t *testing.T) {
	raw, err := buildComment(map[string]string{"message": "test comment"}, "", "", "")
	if err != nil {
		t.Fatalf("build error: %v", err)
	}

	args, err := parseComment(raw)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if args["message"] != "test comment" {
		t.Errorf("message: got %q, want %q", args["message"], "test comment")
	}
}

func TestParseAddReleaseRelatedWork_RoundTrip(t *testing.T) {
	args := map[string]string{
		"version_field": "customfield_10709",
		"category":      "other",
		"title":         "Deploy {{issue.key}}",
		"url":           "https://example.com/{{issue.key}}",
	}

	raw, err := buildAddReleaseRelatedWork(args, "cloud-123", "user@test.com", "token123")
	if err != nil {
		t.Fatalf("build error: %v", err)
	}

	parsed, err := parseAddReleaseRelatedWork(raw)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	for _, key := range []string{"version_field", "category", "title", "url"} {
		if parsed[key] != args[key] {
			t.Errorf("%s: got %q, want %q", key, parsed[key], args[key])
		}
	}
}

func TestBuildDebugLogs(t *testing.T) {
	args := map[string]string{
		"version_field": "customfield_10709",
		"category":      "other",
		"title":         "Deploy {{issue.key}}",
		"url":           "https://example.com",
	}

	logs, err := buildDebugLogs(args, "cloud-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(logs) != 4 {
		t.Fatalf("expected 4 debug logs, got %d", len(logs))
	}

	// Verify each log has the debug prefix.
	for i, raw := range logs {
		var action struct {
			Value string `json:"value"`
		}
		if err := json.Unmarshal(raw, &action); err != nil {
			t.Fatalf("log %d: invalid JSON: %v", i, err)
		}
		if !strings.HasPrefix(action.Value, debugLogPrefix) {
			t.Errorf("log %d: missing debug prefix: %s", i, action.Value)
		}
	}
}

func TestBuildActionWithDebug_True(t *testing.T) {
	args := map[string]string{
		"version_field": "customfield_10709",
		"category":      "other",
		"title":         "Deploy",
		"url":           "https://example.com",
		"debug":         "true",
	}

	actions, err := buildActionWithDebug("add_release_related_work", args, "cloud-123", "user@test.com", "token123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 4 debug logs + 1 webhook action = 5.
	if len(actions) != 5 {
		t.Fatalf("expected 5 actions (4 debug + 1 webhook), got %d", len(actions))
	}

	// First 4 should be debug logs.
	for i := 0; i < 4; i++ {
		var action struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(actions[i], &action); err != nil {
			t.Fatalf("action %d: invalid JSON: %v", i, err)
		}
		if action.Type != "codebarrel.action.log" {
			t.Errorf("action %d: type = %q, want %q", i, action.Type, "codebarrel.action.log")
		}
	}

	// Last should be webhook.
	var last struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(actions[4], &last); err != nil {
		t.Fatalf("last action: invalid JSON: %v", err)
	}
	if last.Type != "jira.issue.outgoing.webhook" {
		t.Errorf("last action type: got %q, want %q", last.Type, "jira.issue.outgoing.webhook")
	}
}

func TestBuildActionWithDebug_False(t *testing.T) {
	args := map[string]string{
		"version_field": "customfield_10709",
		"category":      "other",
		"title":         "Deploy",
		"url":           "https://example.com",
	}

	actions, err := buildActionWithDebug("add_release_related_work", args, "cloud-123", "user@test.com", "token123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(actions) != 1 {
		t.Fatalf("expected 1 action (no debug), got %d", len(actions))
	}

	var action struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(actions[0], &action); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if action.Type != "jira.issue.outgoing.webhook" {
		t.Errorf("type: got %q, want %q", action.Type, "jira.issue.outgoing.webhook")
	}
}
