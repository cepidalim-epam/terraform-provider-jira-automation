package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestRawJSONDataCenterTerraformLifecycle(t *testing.T) {
	var rule map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/rest/api/2/serverInfo":
			fmt.Fprint(w, `{"deploymentType":"Data Center"}`)
		case r.URL.Path == "/rest/api/2/myself":
			fmt.Fprint(w, `{"key":"test"}`)
		case r.URL.Path == "/rest/api/2/project/123":
			fmt.Fprint(w, `{"projectTypeKey":"software"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/rest/cb-automation/latest/project/123/rule":
			if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			value := rule["trigger"].(map[string]interface{})["value"].(map[string]interface{})
			if value["eventKey"] != "jira:issue_created" || value["issueEvent"] != "issue_created" {
				t.Errorf("incorrect created event: %v", value)
				w.WriteHeader(400)
				return
			}
			rule["id"] = 42
			fmt.Fprint(w, `{"id":42}`)
		case r.Method == http.MethodGet && r.URL.Path == "/rest/cb-automation/latest/project/123/rule/42/export":
			if rule == nil {
				w.WriteHeader(404)
				return
			}
			copyBytes, _ := json.Marshal(rule)
			var exported map[string]interface{}
			json.Unmarshal(copyBytes, &exported)
			trigger := exported["trigger"].(map[string]interface{})
			trigger["schemaVersion"] = 1
			trigger["value"] = map[string]interface{}{"synchronous": false}
			for _, v := range exported["components"].([]interface{}) {
				v.(map[string]interface{})["schemaVersion"] = 3
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"rules": []interface{}{exported}})
		case r.Method == http.MethodPut && r.URL.Path == "/rest/cb-automation/latest/project/123/rule/42":
			if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			fmt.Fprint(w, `{}`)
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/cb-automation/latest/project/123/rule/42":
			rule = nil
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	config := func(name string) string {
		return fmt.Sprintf(`
provider "jira-automation" {
  site_url = %q
  email = "test"
  api_token = "test"
  project_id = "123"
}
resource "jira-automation_rule" "test" {
  name = %q
  trigger_json = jsonencode({
    component = "TRIGGER"
    type = "jira.issue.event.trigger:created"
  })
  components_json = jsonencode([{
    component = "CONDITION"
    type = "jira.issue.condition"
    children = []
    parentId = null
    value = { comparison = "EQUALS" }
  }])
}
`, server.URL, name)
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config("created")},
			{Config: config("updated")},
		},
	})
}
