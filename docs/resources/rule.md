---
page_title: "jira-automation_rule Resource - jira-automation"
subcategory: ""
description: |-
  Manages a single Jira Automation rule.
---

# jira-automation_rule (Resource)

Manages a single Jira Automation rule.

## Example Usage

### Simple HCL rule

The provider ships typed `trigger` and `components` blocks so you can avoid raw JSON for common patterns.

```terraform
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
```

### Debugging with `add_release_related_work`

Set `debug = "true"` on a component to inject diagnostic log actions that print the webhook URL, request body, and resolved field values. Remove the flag and re-apply to clean up the debug logs.

```terraform
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
```

### Custom component (condition with then/else)

Conditions nest `then` and `else` action blocks. Each sub-block uses the same `type`/`args` structure.

```terraform
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
```

### Raw JSON (fall-back)

When the HCL helpers don't cover your trigger or action type, use `trigger_json` and `components_json` directly. The provider performs semantic JSON comparison so key order and whitespace are ignored during plan.

```terraform
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
```

## Schema

### Required

- `name` (String) - Rule name.

One of `trigger` or `trigger_json` is required; one of `components` or `components_json` is required.

### Optional

- `trigger` (Block) - Typed trigger block with `type` and `args`. Mutually exclusive with `trigger_json`.
- `trigger_json` (String) - Raw JSON trigger configuration. Use `jsonencode()`. Mutually exclusive with `trigger`.
- `components` (Block List) - Typed component blocks with `type`, `args`, and optional `then`/`else` sub-blocks. Mutually exclusive with `components_json`.
- `components_json` (String) - Raw JSON components array. Use `jsonencode()`. Mutually exclusive with `components`.
- `enabled` (Boolean) - Enable or disable the rule. Defaults to `true`.
- `project_id` (String) - Jira project numeric ID for project-scoped event triggers.

### Read-Only

- `id` (String) - Rule UUID, set on create or import.
- `state` (String) - `ENABLED` or `DISABLED`.
- `scope` (List of String) - Scope ARIs assigned by the API.
- `labels` (List of String) - Rule labels. The provider auto-tags rules with `managed-by:terraform`.

## Import

Import existing rules using an `import` block with the rule UUID:

```hcl
import {
  to = jira-automation_rule.my_rule
  id = "01997721-1866-7233-9bb8-cec4a4614919"
}
```

Then generate the resource configuration:

```bash
terraform plan -generate-config-out=generated.tf
```

~> **Labels:** The provider automatically tags managed rules with `managed-by:terraform`. You must create this label in the Jira UI first (Project Settings → Automation → Labels). Labels cannot be set via Terraform config — use the Jira UI to manage labels.

~> The Jira Automation API has no DELETE endpoint. Running `terraform destroy` will **disable** the rule instead of deleting it.
