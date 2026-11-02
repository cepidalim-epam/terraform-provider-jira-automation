resource "jira-automation_rule" "log_on_transition" {
  name       = "Log on transition"
  project_id = "10001"

  trigger = {
    type = "status_transition"
    args = {
      from_status = "To Do"
      to_status   = "In Progress"
    }
  }

  components = [{
    type = "log"
    args = {
      message = "Issue {{issue.key}} moved to In Progress"
    }
  }]
}
