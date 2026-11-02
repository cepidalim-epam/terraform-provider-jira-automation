package provider

import (
	"encoding/json"
	"testing"
)

func TestBuildTriggerJSON_StatusTransition(t *testing.T) {
	args := map[string]string{
		"from_status": "To Do",
		"to_status":   "In Progress",
	}

	raw, err := BuildTriggerJSON("status_transition", args, "cloud-123", "10001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var trigger map[string]interface{}
	if err := json.Unmarshal(raw, &trigger); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// Verify API type.
	if trigger["type"] != "jira.issue.event.trigger:transitioned" {
		t.Errorf("type: got %q, want %q", trigger["type"], "jira.issue.event.trigger:transitioned")
	}

	if trigger["component"] != "TRIGGER" {
		t.Errorf("component: got %q, want %q", trigger["component"], "TRIGGER")
	}

	value, ok := trigger["value"].(map[string]interface{})
	if !ok {
		t.Fatal("value is not a map")
	}

	// Verify eventFilters contain project ARI.
	eventFilters, ok := value["eventFilters"].([]interface{})
	if !ok || len(eventFilters) != 1 {
		t.Fatalf("expected 1 eventFilter, got %v", eventFilters)
	}
	ari := eventFilters[0].(string)
	if ari != "ari:cloud:jira:cloud-123:project/10001" {
		t.Errorf("eventFilter ARI: got %q, want %q", ari, "ari:cloud:jira:cloud-123:project/10001")
	}

	// Verify from/to status.
	fromStatus := value["fromStatus"].([]interface{})
	if len(fromStatus) != 1 {
		t.Fatalf("expected 1 fromStatus, got %d", len(fromStatus))
	}
	from := fromStatus[0].(map[string]interface{})
	if from["value"] != "To Do" {
		t.Errorf("fromStatus value: got %q, want %q", from["value"], "To Do")
	}

	toStatus := value["toStatus"].([]interface{})
	if len(toStatus) != 1 {
		t.Fatalf("expected 1 toStatus, got %d", len(toStatus))
	}
	to := toStatus[0].(map[string]interface{})
	if to["value"] != "In Progress" {
		t.Errorf("toStatus value: got %q, want %q", to["value"], "In Progress")
	}
}

func TestBuildTriggerJSON_UnknownType(t *testing.T) {
	_, err := BuildTriggerJSON("nonexistent_trigger", map[string]string{}, "", "")
	if err == nil {
		t.Fatal("expected error for unknown trigger type")
	}
}

func TestParseTrigger_RoundTrip(t *testing.T) {
	args := map[string]string{
		"from_status": "To Do",
		"to_status":   "In Progress",
	}

	raw, err := BuildTriggerJSON("status_transition", args, "cloud-123", "10001")
	if err != nil {
		t.Fatalf("build error: %v", err)
	}

	gotType, gotArgs, err := ParseTrigger(raw)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if gotType != "status_transition" {
		t.Errorf("type: got %q, want %q", gotType, "status_transition")
	}
	if gotArgs["from_status"] != "To Do" {
		t.Errorf("from_status: got %q, want %q", gotArgs["from_status"], "To Do")
	}
	if gotArgs["to_status"] != "In Progress" {
		t.Errorf("to_status: got %q, want %q", gotArgs["to_status"], "In Progress")
	}
}
