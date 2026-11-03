---
page_title: "Provider: Jira Automation"
description: |-
  The Jira Automation provider manages Jira Automation rules via the Automation Rule Management REST API.
---

# Jira Automation Provider

The Jira Automation provider lets you manage [Jira Automation](https://www.atlassian.com/software/jira/features/automation) rules as Terraform resources.

## Example Usage

```hcl
terraform {
  required_providers {
    jira-automation = {
      source = "registry.terraform.io/cepidalim-epam/jira-automation"
    }
  }
}

provider "jira-automation" {}
```

When the required values come from environment variables, the provider block can be empty.
You can also set values directly:

```hcl
provider "jira-automation" {
  site_url   = "https://jira.example.com"
  email      = "your-username"
  api_token  = "your-personal-access-token"
  project_id = "80647"
}
```

Any attribute set in the block takes priority over environment variables.

## Authentication

The provider supports Jira Cloud and Jira Server/Data Center. For a dedicated instance, use a Jira username and PAT, and set a numeric project ID for Automation rule operations. Create a `.env` file with `export` prefixes and `source` it before running Terraform:

```bash
export jira_url=https://jira.example.com
export jira_username=your-username
export jira_pat_rw=your-personal-access-token
export jira_id=80647
```

```bash
source .env
terraform plan
```

## Schema

### Optional

- `site_url` (String) - The Jira site URL. Can also be set via `jira_url`, `JIRA_URL`, or legacy `JIRA_SITE_URL` / `ATLASSIAN_SITE_URL` env vars.
- `email` (String) - The Jira username for API authentication. Can also be set via `jira_username`, `JIRA_USERNAME`, or legacy `JIRA_EMAIL` / `ATLASSIAN_USER` env vars.
- `api_token` (String, Sensitive) - The Jira PAT. Can also be set via `jira_pat_rw`, `JIRA_PAT_RW`, or legacy `JIRA_API_TOKEN` / `ATLASSIAN_TOKEN` env vars.
- `project_id` (String) - Numeric Jira project ID for Data Center rule operations. Can also be set via `jira_id`, `JIRA_ID`, or `JIRA_TEST_PROJECT_ID`.
- `webhook_user` (String) - Email for outgoing webhook Basic auth (service account). Can also be set via `JIRA_WEBHOOK_USER` env var.
- `webhook_token` (String, Sensitive) - API token for outgoing webhook Basic auth. Can also be set via `JIRA_WEBHOOK_TOKEN` env var.
- `field_aliases` (Map of String) - Map of friendly alias names to Jira custom field IDs (e.g. `release_version = "customfield_10709"`). Aliases can be used in smart values and as bare arg values; the provider resolves them to field IDs on write and reverses on read.

The Jira URL, username, and PAT are required. Jira Data Center also needs a numeric project ID from `project_id`, `jira_id`, `JIRA_ID`, or `JIRA_TEST_PROJECT_ID`.
