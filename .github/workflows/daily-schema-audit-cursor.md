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

## Step 2 — Validate referenced `$defs` are used

For the primary schema (`main_workflow_schema.json`), compute the transitive closure of local
`$defs` references from the schema body and list definitions outside that closure:

```bash
jq -r '
  def refs:
    [.. | objects | .["$ref"]? | strings | select(startswith("#/$defs/"))
     | ltrimstr("#/$defs/") | split("/")[0] | gsub("~1"; "/") | gsub("~0"; "~")] | unique;
  . as $schema |
  def closure($seen):
    ([$seen[], ($seen[] | $schema["$defs"][.] // empty | refs[])] | unique) as $next |
    if $next == $seen then $seen else closure($next) end;
  (del(.["$defs"]) | refs) as $initial |
  closure($initial) as $used |
  $schema["$defs"] | keys[] | select(. as $key | $used | index($key) | not)
' pkg/parser/schemas/main_workflow_schema.json
```

These are candidates only. Before removing one, check for references outside the schema, including
other schemas, code, tests, and documentation; retain it if usage or compatibility is uncertain.

## Step 3 — Check `required` completeness

For each object in the schema that has `properties`, verify whether a `required` array is
present. List objects that define `properties` but have no `required` field — these may allow
optional fields that should be mandatory.

```bash
jq '[ ([], paths(objects)) as $p | getpath($p)
      | select(type == "object" and has("properties") and (has("required") | not))
      | $p ]' pkg/parser/schemas/main_workflow_schema.json
```

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