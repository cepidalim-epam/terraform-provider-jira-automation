resource "jira-automation_rule" "release_work" {
  name       = "Add release related work"
  project_id = "10001"

  trigger = {
    type = "status_transition"
    args = {
      from_status = "In Progress"
      to_status   = "Done"
    }
  }

  components = [{
    type = "add_release_related_work"
    args = {
      version_field = "release_version" # resolved via field_aliases
      category      = "deployment"
      title         = "Deploy {{issue.key}}"
      url           = "https://ci.example.com/deploy/{{issue.key}}"
      debug         = "true"
    }
  }]
}
