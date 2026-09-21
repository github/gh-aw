---
private: true
emoji: "🖱️"
description: Daily audit of JSON schema consistency across workflow definitions
features:
  gh-aw-detection: true
on:
  schedule: daily
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
sandbox:
  agent:
    id: awf
tracker-id: daily-schema-audit-cursor
engine:
  id: copilot
  copilot-sdk: true
strict: true
network:
  allowed:
    - defaults
    - github
tools:
  github:
    mode: local
    toolsets: [repos]
  bash:
    - cat
    - grep
    - find
    - jq
    - wc
safe-outputs:
  create-issue:
    expires: 1d
    title-prefix: "[schema-audit] "
    labels: [automation, schema]
    max: 1
    close-older-issues: true
    close-older-key: daily-schema-audit-cursor
  missing-tool:
timeout-minutes: 20
imports:
  - shared/otlp.md
  - shared/reporting.md
---

# Daily Schema Consistency Audit

Scan the workflow schema and key JSON config files in this repository for consistency drift and
missing required fields.

## Step 1 — Identify schema files

Find all `*.json` schema files under `pkg/parser/schemas/` and list them:

```bash
find pkg/parser/schemas -name "*.json" | sort
```

## Step 2 — Validate referenced `$defs` are used

For the primary schema (`main_workflow_schema.json`), check that every definition declared in
`$defs` is referenced at least once in the schema body:

```bash
jq -r '
  .["$defs"] | keys[] as $k |
  if (tostring | test("\"\\($k)\"") | not) then $k else empty end
' pkg/parser/schemas/main_workflow_schema.json 2>/dev/null || echo "File not found"
```

Report any unreferenced `$defs` entries.

## Step 3 — Check `required` completeness

For each object in the schema that has `properties`, verify whether a `required` array is
present. List objects that define `properties` but have no `required` field — these may allow
optional fields that should be mandatory.

```bash
jq '[.. | objects | select(has("properties") and (has("required") | not)) | path] | length' \
  pkg/parser/schemas/main_workflow_schema.json 2>/dev/null || echo "0"
```

## Step 4 — Report

Use the `reporting` skill to format the report body. Use `###` (or lower) headers only — never
`#` or `##`. Keep the overall status visible at the top; wrap the full list of unreferenced
`$defs` and objects missing `required` in a
`<details><summary><b>Full Findings</b></summary>` block.

Use the `create_issue` safe-output tool to post the audit:

- **Title**: `[schema-audit] Daily Schema Consistency Report — ${{ github.run_id }}`
- **Body**:
  - `### Summary` with overall status: 🟢 (no issues) / 🟡 (minor) / 🔴 (action needed)
  - `<details><summary><b>Full Findings</b></summary>` wrapping unreferenced `$defs` count and
    names, and objects missing `required` arrays

If all checks pass with no issues, call `noop` with `"Schema consistency audit passed — no issues found."`.