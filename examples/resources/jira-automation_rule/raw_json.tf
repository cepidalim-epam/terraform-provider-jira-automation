resource "jira-automation_rule" "json_fallback" {
  name       = "My Rule"
  project_id = "10001"
  enabled    = true

  trigger_json = jsonencode({
    component     = "TRIGGER"
    schemaVersion = 1
    type          = "jira.issue.event.trigger:transitioned"
    value = {
      fromStatus = [{ type = "NAME", value = "To Do" }]
      toStatus   = [{ type = "NAME", value = "In Progress" }]
    }
  })

  components_json = jsonencode([
    {
      component     = "ACTION"
      schemaVersion = 1
      type          = "codebarrel.action.log"
      value         = "Hello from Terraform"
    }
  ])
}
