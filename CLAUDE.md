# Terraform Provider for Jira Automation

## Building

```bash
go build .
```

The binary is `terraform-provider-jira-automation` (gitignored).

## Running Tests

### Unit tests (no credentials needed)

```bash
go test ./... -short -v
# or
make test
```

### Acceptance tests (requires live Jira instance)

```bash
TF_ACC=1 go test ./... -v -timeout 120m
# or
make testacc
```

Run a single acceptance test:

```bash
TF_ACC=1 go test ./internal/provider/ -v -run TestAccRuleResource_basic -timeout 10m
```

### Required environment variables

| Variable | Required by |
|----------|-------------|
| `TF_ACC=1` | All acceptance tests (framework gate) |
| `JIRA_SITE_URL` (or `ATLASSIAN_SITE_URL`) | All acceptance tests |
| `JIRA_EMAIL` (or `ATLASSIAN_USER`) | All acceptance tests |
| `JIRA_API_TOKEN` (or `ATLASSIAN_TOKEN`) | All acceptance tests |
| `JIRA_TEST_PROJECT_ID` | Tests with project-scoped triggers |
| `JIRA_WEBHOOK_USER` | `add_release_related_work` tests |
| `JIRA_WEBHOOK_TOKEN` | `add_release_related_work` tests |

### Test naming conventions

- Unit tests: `Test*` (e.g. `TestBuildLog`, `TestResolveAliases`)
- Acceptance tests: `TestAcc*` (e.g. `TestAccRuleResource_basic`)
- All acceptance test rules are prefixed `tf-acc-` and labeled `tf-acc-test`

### Cleanup

Acceptance test rules accumulate as disabled rules (the API has no DELETE endpoint). Periodically clean via Jira UI by filtering on the `tf-acc-test` label.

## Dev Override

Generate a portable `dev.tfrc` pointing at the local binary:

```bash
make dev.tfrc
TF_CLI_CONFIG_FILE=dev.tfrc terraform plan
```

No `terraform init` needed with dev overrides.

## import-gen

Regenerates all `rule_*.tf` files in a target directory from the live Jira Automation rules:

```bash
go build -o import-gen ./cmd/import-gen && ./import-gen ../beno
```

Requires `ATLASSIAN_SITE_URL`, `ATLASSIAN_USER`, `ATLASSIAN_TOKEN` env vars.

## Doc Examples & Golden Files

The 4 HCL examples in `docs/resources/rule.md` are generated from `examples/resources/jira-automation_rule/*.tf` via `tfplugindocs`. These same example files are the source of truth for the `TestAccDocExample_*` acceptance tests.

### Regenerate docs from examples

```bash
make docs
```

### Run doc-example acceptance tests

```bash
TF_ACC=1 go test ./internal/provider/ -v -run TestAccDocExample -timeout 30m
```

### Update golden files (API response snapshots)

```bash
make golden
# or equivalently:
TF_ACC=1 GOLDEN_UPDATE=1 go test ./internal/provider/ -v -run TestAccDocExample -timeout 30m
```

Golden files live in `testdata/golden/*.json`. Set `GOLDEN_STRICT=1` in CI to fail on drift.

### Screenshot capture

After `make golden`, use Claude Code with Chrome MCP tools to capture Jira UI screenshots. See `testdata/screenshots/README.md`.

## API Knowledge

Jira Automation REST API tacit knowledge (envelope requirements, read-modify-write pattern, component IDs, debugging) is in `.claude/skills/jira-automation-api/SKILL.md`.
