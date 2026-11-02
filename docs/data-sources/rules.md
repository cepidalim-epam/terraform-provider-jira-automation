---
page_title: "jira-automation_rules Data Source - Jira Automation"
subcategory: ""
description: |-
  Lists all Jira Automation rule summaries.
---

# jira-automation_rules (Data Source)

Lists all automation rule summaries for the configured Jira site.

## Example Usage

```hcl
data "jira-automation_rules" "all" {}

output "rules" {
  value = data.jira-automation_rules.all.rules
}
```

## Schema

### Read-Only

- `rules` (List of Object) - All automation rule summaries. Each entry has:
  - `uuid` (String) - Rule UUID.
  - `name` (String) - Rule name.
  - `state` (String) - `ENABLED` or `DISABLED`.
  - `enabled` (Boolean) - Whether the rule is enabled.
