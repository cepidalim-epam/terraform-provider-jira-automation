---
name: jira-automation-api
description: Jira Automation REST API reference — envelope requirements, read-modify-write pattern, component ID handling, IF condition structure, and debugging 400 errors. Use when modifying the API client, adding new endpoints, or debugging API write failures.
disable-model-invocation: false
user-invocable: true
allowed-tools: Read, Grep, Glob, Bash, WebFetch
---

# Jira Automation REST API — Tacit Knowledge

## API Base URL

```
https://api.atlassian.com/automation/public/jira/{cloudId}/rest/v1
```

Cloud ID is resolved from `https://{site}/_edge/tenant_info`.

Auth: Basic auth with `email:api_token`.

## The Envelope Rule

**All write operations (POST, PUT) require the payload wrapped in `{"rule": ...}`.**

```json
// WRONG — returns 400
{"name": "My Rule", "trigger": {...}, "components": [...]}

// CORRECT
{"rule": {"name": "My Rule", "trigger": {...}, "components": [...]}}
```

The GET response also uses this envelope (`{"rule": {...}}`), so a round-trip is: unwrap on read, re-wrap on write.

The error message for a missing envelope is the same generic `400: "The request body could not be parsed"` — it gives no hint about the envelope.

## POST /rule — Required Fields for Rule Creation

**POST /rule requires ALL of these fields.** Omitting any one returns `400: "The request body could not be parsed"`.

```json
{
  "rule": {
    "name": "My Rule",
    "state": "DISABLED",
    "notifyOnError": "FIRSTERROR",
    "canOtherRuleTrigger": false,
    "authorAccountId": "<accountId from /rest/api/3/myself>",
    "actor": {"type": "ACCOUNT_ID", "actor": "<same accountId>"},
    "writeAccessType": "OWNER_ONLY",
    "trigger": { ... },
    "components": [ ... ],
    "ruleScopeARIs": ["ari:cloud:jira:<cloudId>:project/<projectId>"]
  }
}
```

| Field | Values | Notes |
|-------|--------|-------|
| `state` | `"ENABLED"` / `"DISABLED"` | Always create disabled, enable via PUT /rule/{uuid}/state after |
| `notifyOnError` | `"FIRSTERROR"` / `"EVERYERROR"` | How to notify on rule execution errors |
| `canOtherRuleTrigger` | `true` / `false` | Whether other rules can trigger this one |
| `authorAccountId` | string | Jira account ID of the author. Resolve via `GET /rest/api/3/myself` → `accountId` |
| `actor` | `{"type":"ACCOUNT_ID","actor":"<id>"}` | Who the rule runs as (same accountId works) |
| `writeAccessType` | `"OWNER_ONLY"` / `"COLLABORATORS"` | Edit permissions |
| `ruleScopeARIs` | array of ARI strings | Project: `ari:cloud:jira:<cloudId>:project/<projectId>`, Global: `ari:cloud:jira::site/<cloudId>` |

**Optional on create:** `description`, `labels`, `collaborators`.

The response returns `{"ruleUuid": "..."}` (note: `ruleUuid`, not `uuid`).

## PUT Requires the Full Rule Object

A PUT that only sends the fields you want to change will fail with 400. The API expects **all** fields from the GET response (minus `uuid`, `created`, `updated`).

The correct pattern is **read-modify-write**:
1. GET the rule (full JSON)
2. Remove `uuid`, `created`, `updated`
3. Merge in your changes (name, trigger, components, scope, labels)
4. Wrap in `{"rule": ...}` and PUT

Fields that must be preserved from GET: `state`, `description`, `canOtherRuleTrigger`, `notifyOnError`, `authorAccountId`, `actor`, `writeAccessType`, `collaborators`.

## Component IDs

Components have API-assigned `id`, `parentId`, and `conditionParentId` fields. Key rules:

- **On update:** strip all component IDs. The API will reassign them. If you keep old IDs but change the component tree structure, you get `400: "Component ids do not match the existing rule or there are duplicate ids"`.
- **The `parentId` / `conditionParentId` fields are redundant** with the `children` / `conditions` nesting. Set them to `null` when writing; the API populates them on read.
- **For Terraform state:** strip these fields during normalization so the config (which doesn't have them) matches the state (which would have them from GET).

## IF Condition Structure

An IF block that gates actions requires a **3-layer nesting**:

```
CONDITION (jira.condition.container.block)     — outer wrapper
  └── CONDITION_BLOCK (jira.condition.if.block)  — the IF branch
        ├── conditions: [CONDITION (jira.issue.condition)]  — the check
        └── children: [ACTION, ACTION, ...]                 — guarded actions
```

For "field is not empty":
```json
{
  "component": "CONDITION",
  "type": "jira.issue.condition",
  "schemaVersion": 3,
  "value": {
    "compareValue": null,
    "comparison": "NOT_EMPTY",
    "selectedField": {"type": "ID", "value": "customfield_XXXXX"},
    "selectedFieldType": "customfield_XXXXX"
  }
}
```

An ELSE branch is another `CONDITION_BLOCK` sibling with `conditions: []`.

## Rule State Endpoint

`PUT /rule/{uuid}/state` uses a **different payload format** than other write endpoints:

```json
// WRONG — returns 400
{"enabled": true}

// CORRECT
{"value": "ENABLED"}   // or "DISABLED"
```

No `{"rule": ...}` envelope needed. The schema is `RuleStateUpdateRequest` with a single required field `value` (enum: `"ENABLED"`, `"DISABLED"`).

An expired token on this endpoint returns `400` (not `401`), same as the main rule endpoints.

## API Token Expiry

The `ATLASSIAN_TOKEN` has a short lifetime. An expired token returns `401: "Unauthorized"` on reads and `400: "The request body could not be parsed"` on writes. Always verify auth with a quick GET before debugging write failures.

## OpenAPI Spec & Component Type Schemas

- **Official OAS:** `atlassian-automation-oas.json` (in this skill directory). Downloaded from `https://developer.atlassian.com/cloud/automation/swagger.v3.json`. Defines endpoints, request/response envelopes, and base schemas (`RuleWriteRequest`, `ComponentConfigDTO`, `TriggerConfigDTO`, etc.). The official spec leaves `ComponentConfigDTO.type` as a free-form string and `value` as `oneOf [string, object]`.

- **OAS Overlay:** `component-types.overlay.json` (in this skill directory). An [OAS Overlay 1.0](https://spec.openapis.org/overlay/v1.0.0.html) that enriches the official spec with discovered component types. Adds `x-known-values` to `ComponentConfigDTO.type` and `TriggerConfigDTO.type` with all known type strings, and adds named value schemas (e.g., `ConditionValue_comparator`, `ActionValue_webhook`) under `components.schemas`. Each has `x-type`, `x-ui-name`, and `x-schema-version` annotations. Apply with any OAS overlay tool to produce a merged spec.

## Internal vs Public API Differences

The UI uses the **internal API** for rule creation, while the public API also works with the correct fields. Key differences:

| Aspect | Internal API (CREATE) | Public API (CREATE/UPDATE) |
|--------|----------------------|---------------------------|
| Envelope key | `ruleConfigBean` | `rule` |
| Actor field | `actor.value` | `actor.actor` |
| Scope field | `ruleScope.resources` | `ruleScopeARIs` |
| Labels format | Numeric IDs (Long) | String names |
| Endpoint | `{siteURL}/gateway/api/automation/internal-api/jira/{cloudId}/pro/rest/{projectId}/rule` | `api.atlassian.com/automation/public/jira/{cloudId}/rest/v1/rule` |

The Terraform provider uses the **public API** for all operations.

## Label Internal API

Labels are managed via the internal API (not the public automation API).

**List labels:** `GET {siteURL}/gateway/api/automation/internal-api/jira/{cloudId}/pro/rest/{projectId}/rule-labels`

**Create label:** `POST` to the same URL with `{"name": "label-name", "color": "B300"}`. **The `color` field is required** — omitting it returns 500 with an empty body. Valid colors: `B300`, `B500`, `G300`, `T300`, `N800`, etc.

**Add label to rule:** `PUT {siteURL}/gateway/api/automation/internal-api/jira/{cloudId}/pro/rest/{projectId}/rules/{ruleUUID}/labels/{labelId}` (idempotent).

## Debugging Checklist

When a write operation returns 400:
1. Verify auth — does GET still work?
2. Check the `{"rule": ...}` envelope is present
3. Check you're sending the full rule object (not just changed fields)
4. Check component IDs are stripped (or consistent with the existing rule)
5. If all else fails, extract the OpenAPI spec and validate your payload against the schema

---

## Component Type Quick Reference

Full schemas are in `component-types.schema.json`. Here are the most commonly needed types:

**Conditions:**
- `jira.issue.condition` (sv3) — issue field check (NOT_EMPTY, EQUALS, etc.). Limited custom field support.
- `jira.comparator.condition` (sv1) — smart values comparison. Better for custom fields. `{"first": "{{...}}", "second": "", "operator": "NOT_EQUALS"}`
- `jira.jql.condition` (sv1) — JQL check. `{"jql": "..."}`
- `jira.condition.container.block` + `jira.condition.if.block` — IF/ELSE structure (see "IF Condition Structure" above)

**Actions:**
- `codebarrel.action.log` (sv1) — value is a plain string
- `jira.issue.edit` (sv12) — uses operations array
- `jira.issue.transition` (sv11) — uses operations array + destinationStatus
- `jira.issue.outgoing.webhook` (sv1) — web request with headers, method, body
- `jira.issue.create` (sv12) — uses operations array

**Triggers:**
- `jira.issue.event.trigger:<event>` — `:transitioned`, `:created`, `:commented`, etc.
- `jira.jql.scheduled` (sv1) — cron/interval schedule
- `jira.incoming.webhook` (sv1) — incoming webhook with token
