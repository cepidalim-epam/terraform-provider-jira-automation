resource "jira-automation_rule" "conditional_comment" {
  name       = "Comment on high-priority issues"
  project_id = "10001"

  trigger = {
    type = "status_transition"
    args = {
      from_status = "To Do"
      to_status   = "In Progress"
    }
  }

  components = [{
    type = "condition"
    args = {
      first    = "{{issue.priority.name}}"
      operator = "equals"
      second   = "High"
    }

    then = [{
      type = "comment"
      args = {
        message = "High-priority issue started — notifying the team."
      }
    }]

    else = [{
      type = "log"
      args = {
        message = "Normal priority — no action needed."
      }
    }]
  }]
}
