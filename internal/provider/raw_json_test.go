package provider

import (
	"context"
	"encoding/json"
	"testing"

	"terraform-provider-jira-automation/internal/client"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
)

func TestReconcileRawJSON(t *testing.T) {
	tests := []struct {
		name, configured, remote string
		equivalent               bool
	}{
		{
			name:       "created trigger defaults and event metadata",
			configured: `{"component":"TRIGGER","id":"__NEW__TRIGGER","type":"jira.issue.event.trigger:created","value":{"eventKey":"jira:issue_created","issueEvent":"issue_created"}}`,
			remote:     `{"component":"TRIGGER","schemaVersion":1,"type":"jira.issue.event.trigger:created","value":{"synchronous":false}}`,
			equivalent: true,
		},
		{
			name:       "omitted value reconciles with defaults",
			configured: `{"component":"TRIGGER","type":"jira.issue.event.trigger:created"}`,
			remote:     `{"component":"TRIGGER","schemaVersion":1,"type":"jira.issue.event.trigger:created","value":{"synchronous":false}}`,
			equivalent: true,
		},
		{
			name:       "omitted value with remote settings is drift",
			configured: `{"component":"TRIGGER","type":"jira.issue.event.trigger:created"}`,
			remote:     `{"component":"TRIGGER","type":"jira.issue.event.trigger:created","value":{"synchronous":true}}`,
		},
		{
			name:       "field changed trigger omitted null event values",
			configured: `{"component":"TRIGGER","schemaVersion":2,"type":"jira.issue.field.changed","value":{"eventKey":null,"issueEvent":null,"actions":["edit","create"],"fields":[{"type":"field","value":"labels"}],"synchronous":false}}`,
			remote:     `{"component":"TRIGGER","schemaVersion":2,"type":"jira.issue.field.changed","value":{"actions":["edit","create"],"fields":[{"type":"field","value":"labels"}],"synchronous":false}}`,
			equivalent: true,
		},
		{
			name:       "condition omitted null compare metadata",
			configured: `[{"component":"CONDITION","schemaVersion":3,"type":"jira.issue.condition","value":{"compareValue":{"modifier":null,"source":null,"multiValue":false,"type":"ID","value":"6"},"comparison":"EQUALS"}}]`,
			remote:     `[{"component":"CONDITION","schemaVersion":3,"type":"jira.issue.condition","value":{"compareValue":{"multiValue":false,"type":"ID","value":"6"},"comparison":"EQUALS"}}]`,
			equivalent: true,
		},
		{
			name:       "non-null compare modifier remains significant",
			configured: `{"component":"CONDITION","type":"jira.issue.condition","value":{"compareValue":{"modifier":null,"value":"6"}}}`,
			remote:     `{"component":"CONDITION","type":"jira.issue.condition","value":{"compareValue":{"modifier":"changed","value":"6"}}}`,
		},
		{
			name:       "non-null event field remains significant",
			configured: `{"component":"TRIGGER","type":"jira.issue.field.changed","value":{"eventKey":null}}`,
			remote:     `{"component":"TRIGGER","type":"jira.issue.field.changed","value":{"eventKey":"other"}}`,
		},
		{
			name:       "arbitrary null field remains significant",
			configured: `{"component":"ACTION","type":"jira.issue.outgoing.webhook","value":{"customBody":null}}`,
			remote:     `{"component":"ACTION","type":"jira.issue.outgoing.webhook","value":{}}`,
		},
		{
			name:       "component defaults and empty metadata",
			configured: `[{"component":"CONDITION","type":"jira.issue.condition","value":{"comparison":"EQUALS"}},{"component":"ACTION","type":"webhook","schemaVersion":2,"children":[],"conditions":[],"parentId":null,"conditionParentId":null,"value":{"url":"test"}}]`,
			remote:     `[{"component":"CONDITION","schemaVersion":3,"type":"jira.issue.condition","value":{"comparison":"EQUALS"}},{"component":"ACTION","schemaVersion":2,"type":"webhook","value":{"url":"test"}}]`,
			equivalent: true,
		},
		{
			name:       "nested component defaults",
			configured: `[{"component":"CONDITION","type":"block","children":[{"component":"ACTION","type":"log","value":"x"}]}]`,
			remote:     `[{"component":"CONDITION","schemaVersion":1,"type":"block","children":[{"component":"ACTION","schemaVersion":1,"type":"log","value":"x"}]}]`,
			equivalent: true,
		},
		{
			name:       "user data with component key remains significant",
			configured: `{"component":"ACTION","type":"webhook","value":{"component":"data","type":"custom","id":"one"}}`,
			remote:     `{"component":"ACTION","type":"webhook","value":{"component":"data","type":"custom","id":"two"}}`,
		},
		{
			name:       "changed action must be drift",
			configured: `[{"component":"ACTION","type":"webhook","value":{"url":"old"}}]`,
			remote:     `[{"component":"ACTION","schemaVersion":2,"type":"webhook","value":{"url":"new"}}]`,
		},
		{
			name:       "explicit schema version change must be drift",
			configured: `{"component":"TRIGGER","schemaVersion":2,"type":"x","value":{}}`,
			remote:     `{"component":"TRIGGER","schemaVersion":1,"type":"x","value":{}}`,
		},
		{
			name:       "nondefault event key not ignored",
			configured: `{"component":"TRIGGER","type":"jira.issue.event.trigger:created","value":{"eventKey":"unexpected"}}`,
			remote:     `{"component":"TRIGGER","type":"jira.issue.event.trigger:created","value":{}}`,
		},
		{
			name:       "transition defaults returned by API",
			configured: `{"component":"TRIGGER","type":"jira.issue.event.trigger:transitioned","value":{"toStatus":[{"value":"Done"}]}}`,
			remote:     `{"component":"TRIGGER","type":"jira.issue.event.trigger:transitioned","value":{"eventKey":"jira:issue_updated","issueEvent":"issue_generic","eventFilters":["ari:cloud:jira::project/123"],"toStatus":[{"value":"Done"}]}}`,
			equivalent: true,
		},
		{
			name:       "remote event changed",
			configured: `{"component":"TRIGGER","type":"jira.issue.event.trigger:created","value":{"eventKey":"jira:issue_created"}}`,
			remote:     `{"component":"TRIGGER","type":"jira.issue.event.trigger:created","value":{"eventKey":"jira:issue_updated"}}`,
		},
		{
			name:       "unknown trigger explicit values preserved",
			configured: `{"component":"TRIGGER","type":"other","value":{"eventKey":"a"}}`,
			remote:     `{"component":"TRIGGER","type":"other","value":{"eventKey":"b"}}`,
		},
		{
			name:       "explicit filter changed",
			configured: `{"component":"TRIGGER","type":"jira.issue.event.trigger:transitioned","value":{"eventFilters":["other"]}}`,
			remote:     `{"component":"TRIGGER","type":"jira.issue.event.trigger:transitioned","value":{"eventFilters":["ari:cloud:jira::project/123"]}}`,
		},
		{
			name:       "component value event fields are not metadata",
			configured: `{"component":"ACTION","type":"x","value":{"eventKey":"a"}}`,
			remote:     `{"component":"ACTION","type":"x","value":{"eventKey":"b"}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := reconcileRawJSON(jsontypes.NewNormalizedValue(tt.configured), tt.remote, &client.Client{IsDataCenter: true, ProjectID: "123"}, "123")
			want := tt.remote
			if tt.equivalent {
				want = tt.configured
			}
			if got != want {
				t.Errorf("got %s, want %s", got, want)
			}
		})
	}
}

func TestReconcileEventDefaultsCloud(t *testing.T) {
	configured := `{"component":"TRIGGER","type":"jira.issue.event.trigger:transitioned","value":{"toStatus":[{"value":"Done"}]}}`
	remote := `{"component":"TRIGGER","type":"jira.issue.event.trigger:transitioned","value":{"eventKey":"jira:issue_updated","issueEvent":"issue_generic","eventFilters":["ari:cloud:jira:cloud-id:project/123"],"toStatus":[{"value":"Done"}]}}`
	got := reconcileRawJSON(jsontypes.NewNormalizedValue(configured), remote, &client.Client{CloudID: "cloud-id"}, "123")
	if got != configured {
		t.Fatalf("cloud default values caused drift: %s", got)
	}
}

func TestNormalizeEmptyComponents(t *testing.T) {
	got, err := normalizeRawJSONArray(nil)
	if err != nil || got != "[]" {
		t.Fatalf("normalize empty components = %q, %v", got, err)
	}
}

func TestResolveEventTriggerDataCenter(t *testing.T) {
	for _, tt := range []struct {
		name, input string
		dc          bool
		key, event  string
	}{
		{"default", `{"component":"TRIGGER","type":"jira.issue.event.trigger:created","value":{"synchronous":false}}`, true, "jira:issue_created", "issue_created"},
		{"explicit", `{"component":"TRIGGER","type":"jira.issue.event.trigger:created","value":{"eventKey":"custom","issueEvent":"custom"}}`, true, "custom", "custom"},
		{"transition defaults", `{"component":"TRIGGER","type":"jira.issue.event.trigger:transitioned","value":{}}`, true, "jira:issue_updated", "issue_generic"},
		{"cloud unchanged", `{"component":"TRIGGER","type":"jira.issue.event.trigger:created","value":{}}`, false, "", ""},
		{"unknown unchanged", `{"component":"TRIGGER","type":"jira.issue.event.trigger:commented","value":{}}`, true, "", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := ruleResource{client: &client.Client{IsDataCenter: tt.dc}}
			model := ruleResourceModel{TriggerJSON: jsontypes.NewNormalizedValue(tt.input)}
			raw, diags := r.resolveTriggerJSON(context.Background(), &model)
			if diags.HasError() {
				t.Fatal(diags)
			}
			var trigger struct {
				Value map[string]interface{} `json:"value"`
			}
			if err := json.Unmarshal(raw, &trigger); err != nil {
				t.Fatal(err)
			}
			if tt.key == "" {
				if _, ok := trigger.Value["eventKey"]; ok {
					t.Fatal("unexpected event key")
				}
				return
			}
			if trigger.Value["eventKey"] != tt.key || trigger.Value["issueEvent"] != tt.event {
				t.Errorf("value = %v", trigger.Value)
			}
		})
	}
}
