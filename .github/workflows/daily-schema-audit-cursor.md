---
private: true
emoji: "🖱️"
description: Daily audit and repair of JSON schema consistency across workflow definitions
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
    - go
tools:
  github:
    mode: local
    toolsets: [repos, pull_requests]
  edit:
  bash:
    - cat
    - grep
    - find
    - jq
    - wc
    - sort
    - echo
    - go
    - git
safe-outputs:
  create-pull-request:
    expires: 7d
    title-prefix: "[schema-audit] "
    labels: [automation, schema]
    max: 1
    draft: true
    fallback-as-issue: false
    allowed-files:
      - pkg/parser/schemas/main_workflow_schema.json
  missing-tool:
timeout-minutes: 20
imports:
  - shared/otlp.md
  - shared/reporting.md
evals:
  - id: verified-schema-repairs
    question: When a pull request is proposed, does the output identify evidence for each schema change and successful validation? Return UNKNOWN when no pull request is proposed.
  - id: no-actionable-repair-noop
    question: When no verified actionable schema repair is available or a matching pull request is already open, does the agent request noop? Return UNKNOWN otherwise.
---

# Daily Schema Consistency Audit

Scan the workflow schema for consistency drift and prepare at most one small, verified schema
repair. Treat missing `required` arrays as candidates, not errors: optional fields and open
variants are often intentional. Never change validation semantics without evidence.

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

These are candidates only. Before removing one, check for references outside the schema, including
other schemas, code, tests, and documentation; retain it if usage or compatibility is uncertain.

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

## Step 4 — Repair and validate

Check open pull requests for an overlapping schema repair before editing. If one already covers
the candidate, do not create a competing PR. Only edit `pkg/parser/schemas/main_workflow_schema.json`;
leave uncertain findings and other schema files unchanged. Remove a definition only after verifying
it is unreachable and has no external consumers. Add `required` only when existing implementation
and tests establish that the field is mandatory; do not infer this from its absence alone.

Validate the edited JSON with `jq -e 'type == "object"'` and run `go test ./pkg/parser`.
Review the diff for unintended changes. If a required check or evidence is unavailable or
validation fails, do not create a PR; call `report-incomplete` with the limitation.

## Step 5 — Publish

For a nonempty, validated repair, call `create-pull-request` once with a concise title and a body
containing `### Summary`, `### Evidence`, and `### Validation`. Explain each change, cite the
supporting schema and implementation or test evidence, list checks actually run, and note any
unresolved candidates. Do not create a report-only or empty PR.

If there is no verified actionable repair, or an open PR already covers it, call `noop` with a
brief reason. Do not create issues or write through GitHub APIs or git.
