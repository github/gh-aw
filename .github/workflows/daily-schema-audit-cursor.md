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
    - sort
    - echo
safe-outputs:
  create-issue:
    expires: 1d
    title-prefix: "[schema-audit] "
    labels: [automation, schema, "backlog:reports"]
    max: 1
    close-older-issues: true
    close-older-key: daily-schema-audit-cursor
  missing-tool:
timeout-minutes: 20
imports:
  - shared/otlp.md
  - shared/reporting.md
evals:
  - id: genuinely-unreferenced-defs
    question: For reported unreachable $defs entries, does the schema evidence show that each is unreachable from the schema body through local $ref links? Return UNKNOWN when no such findings are reported.
  - id: missing-required-accuracy
    question: For reported objects missing required, does each cited object define properties without a required field? Return UNKNOWN when no such findings are reported.
---

# Daily Schema Consistency Audit

Scan the workflow schema and key JSON config files in this repository for consistency drift and
missing required fields.

## Step 1 — Identify schema files

Find all `*.json` schema files under `pkg/parser/schemas/` and list them:

```bash
find pkg/parser/schemas -name "*.json" | sort
```

## Step 2 — Find unreachable `$defs`

For the primary schema (`main_workflow_schema.json`), follow local `$ref` links from the schema
body through referenced definitions. Report definitions not reachable by this closure, including
isolated cycles. References to a nested path count as references to its enclosing definition.

```bash
jq -r '
  def refs:
    [.. | objects | .["$ref"]? | strings | select(startswith("#/$defs/"))
     | ltrimstr("#/$defs/") | split("/")[0] | gsub("~1"; "/") | gsub("~0"; "~")] | unique;
  . as $schema | ($schema["$defs"] // {}) as $defs |
  ($schema | del(.["$defs"]) | refs) as $initial |
  (reduce range(0; $defs | length) as $i
    ($initial; . as $reachable |
     ($reachable + [$reachable[] | $defs[.]? | select(. != null) | refs[]]) | unique)
  ) as $reachable |
  ($defs | keys) - $reachable | .[]
' pkg/parser/schemas/main_workflow_schema.json
```

If the command fails, report the check as unavailable, not as zero findings.

## Step 3 — Check `required` completeness

For each object in the schema that has `properties`, verify whether a `required` array is
present. List objects that define `properties` but have no `required` field — these may allow
optional fields that should be mandatory.

The output is an array of paths, including `[]` for the root object. Its length is the count.
These are informational candidates, not necessarily errors; optional fields and conditional
subschemas may legitimately omit `required`.

```bash
jq '[if (type == "object" and has("properties") and (has("required") | not)) then [] else empty end,
     paths(objects | select(has("properties") and (has("required") | not)))]' \
  pkg/parser/schemas/main_workflow_schema.json
```

If the command fails, report the check as unavailable, not as zero findings.

## Step 4 — Report

Use the `reporting` skill to format the report body. Use `###` (or lower) headers only — never
`#` or `##`. Keep the overall status visible at the top; wrap the full list of unreferenced
`$defs` and objects missing `required` in a
`<details><summary><b>Full Findings</b></summary>` block.

Use the `create_issue` safe-output tool to post the audit:

- **Title**: `[schema-audit] Daily Schema Consistency Report — ${{ github.run_id }}`
- **Body**:
  - `### Summary` with overall status: 🟢 (no issues) / 🟡 (minor) / 🔴 (action needed)
  - `<details><summary><b>Full Findings</b></summary>` wrapping unreachable `$defs` count and
    names, and objects missing `required` arrays, with unavailable checks explicitly identified

Only call `noop` with `"Schema consistency audit passed — no issues found."` if all checks
completed and found no issues. Do not treat missing `required` arrays as issues without evidence
that a field must be mandatory.