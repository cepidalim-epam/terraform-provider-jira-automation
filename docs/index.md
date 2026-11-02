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
      source = "registry.terraform.io/cepicdalim-epam/terraform-provider-jira-automation"
    }
  }
}

provider "jira-automation" {}
```

When all three required values come from environment variables, the provider block can be empty.
You can also set values directly:

```hcl
provider "jira-automation" {
  site_url  = "https://yoursite.atlassian.net"
  email     = "you@example.com"
  api_token = "your-api-token"
}
```

Any attribute set in the block takes priority over environment variables.

## Authentication

The provider authenticates using [Atlassian API tokens](https://id.atlassian.com/manage-profile/security/api-tokens).
Create a `.env` file with `export` prefixes and `source` it before running Terraform:

```bash
export ATLASSIAN_SITE_URL=https://yoursite.atlassian.net
export ATLASSIAN_USER=you@example.com
export ATLASSIAN_TOKEN=your-api-token
```

```bash
source .env
terraform plan
```

## Schema

### Optional

- `site_url` (String) - The Jira site URL (e.g. `https://yoursite.atlassian.net`). Can also be set via `JIRA_SITE_URL` or `ATLASSIAN_SITE_URL` env var.
- `email` (String) - The email for Jira API authentication. Can also be set via `JIRA_EMAIL` or `ATLASSIAN_USER` env var.
- `api_token` (String, Sensitive) - The API token for Jira authentication. Can also be set via `JIRA_API_TOKEN` or `ATLASSIAN_TOKEN` env var.
- `webhook_user` (String) - Email for outgoing webhook Basic auth (service account). Can also be set via `JIRA_WEBHOOK_USER` env var.
- `webhook_token` (String, Sensitive) - API token for outgoing webhook Basic auth. Can also be set via `JIRA_WEBHOOK_TOKEN` env var.
- `field_aliases` (Map of String) - Map of friendly alias names to Jira custom field IDs (e.g. `release_version = "customfield_10709"`). Aliases can be used in smart values and as bare arg values; the provider resolves them to field IDs on write and reverses on read.

All three of `site_url`, `email`, and `api_token` must be provided — either in the provider block, via env vars, or a combination.
