# Screenshot Capture Workflow

After running `make golden` to create acceptance-test rules in Jira, capture
screenshots of each rule's workflow view using Claude Code with Chrome MCP tools.

## Steps

1. Run `make golden` to create/update rules and golden JSON files.
2. Open a Claude Code session with Chrome MCP access.
3. Navigate to the Jira Automation UI for the test project.
4. For each rule (filter by the `tf-acc-test` label):
   - Open the rule's workflow/detail view.
   - Use `mcp__claude-in-chrome__computer` to take a screenshot.
   - Save to `testdata/screenshots/<name>.png`.

## Naming

| Test name               | Screenshot file                  |
|--------------------------|----------------------------------|
| `simple_hcl`            | `simple_hcl.png`                |
| `debug_webhook`         | `debug_webhook.png`             |
| `condition_then_else`   | `condition_then_else.png`       |
| `raw_json`              | `raw_json.png`                  |
