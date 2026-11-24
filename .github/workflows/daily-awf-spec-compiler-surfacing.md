---
private: true
emoji: "🧭"
name: Daily AWF Spec Compiler Surfacing Review
description: Reviews AWF specification and compiler updates (starting with the main JSON schema) to detect new AWF features that should be surfaced in gh-aw.
on:
  schedule: daily
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
checkout:
  fetch-depth: 200
tracker-id: daily-awf-spec-compiler-surfacing
model: copilot/gpt-6-sol
engine:
  id: codex
  model-provider: github
sandbox:
  agent:
    id: awf
    runtime: cloud-hypervisor
tools:
  cli-proxy: true
  github:
    mode: local
    toolsets: [default, issues, pull_requests]
  ledger:
    awf-feature-reviews:
      schema:
        type: object
        required: [record_type, run_id, reviewed_at, reviewed_sha, schema_sha256, open_feature_ids, outcome]
        properties:
          record_type:
            enum: [awf_spec_surfacing_review]
          run_id:
            type: string
          reviewed_at:
            type: string
          reviewed_sha:
            type: string
          schema_sha256:
            type: string
          open_feature_ids:
            type: array
            items:
              type: string
          outcome:
            enum: [issue_created, pull_request_created, noop]
        additionalProperties: false
      max-record-kb: 8
      max-patch-kb: 10
  bash: true
  edit: null
safe-outputs:
  create-pull-request:
    title-prefix: "[awf-feature-surfacing] "
    labels: [automation, awf, compiler, specifications]
    expires: 7d
    max: 1
    allowed-files:
      - ".github/aw/triggers.md"
      - ".github/aw/syntax*.md"
      - "pkg/parser/schemas/*.json"
      - "docs/src/content/docs/**"
    protected-files:
      policy: request-review
timeout-minutes: 30
strict: true
imports:
  - shared/otlp.md
  - shared/reporting.md
features:
  gh-aw-detection: true
evals:
  - id: spec_updates_reviewed
    question: Did the agent review AWF specification and compiler updates for features that should be surfaced in gh-aw?
  - id: pr_created_or_noop
    question: Did the agent create a pull request for actionable surfacing changes, or report that no change was needed?
---

{{#runtime-import? .github/shared-instructions.md}}

# Daily AWF Spec Compiler Surfacing Review

You are the AWF feature surfacing reviewer for `gh-aw`.

Your mission is to review AWF specification and compiler evolution and decide if newly introduced AWF capabilities need to be surfaced in gh-aw UX, docs, commands, templates, or migration guidance.

## Scope

Start with the main AWF schema, then expand to nearby specification/compiler sources:

- `pkg/parser/schemas/main_workflow_schema.json` (primary source)
- `.github/aw/syntax.md`
- `.github/aw/syntax-agentic.md`
- `.github/aw/syntax-core.md`
- `.github/aw/syntax-tools-imports.md`
- `pkg/parser/`
- `pkg/workflow/`

## Persistent Review History

Use the generic records projection at `/tmp/gh-aw/ledgers/awf-feature-reviews/ledger.db` as the authoritative cross-run history. Query recent
review records and use the newest valid record's `reviewed_sha` as the previous review cursor and
its `open_feature_ids` to identify already-tracked gaps. Check whether each tracked gap has an open pull request before suppressing work; an issue without an implementing pull request is not a completed change. If no valid record exists,
use the last 7 days as the initial diff window.

Each record contains the run ID, review timestamp, reviewed commit SHA, schema
SHA-256, stable open feature IDs, and outcome. The legacy `issue_created` value
is retained for existing records; new records use `pull_request_created` or
`noop`. Do
not store full diffs, source excerpts, or raw analysis notes in the ledger.

## Procedure

### 1) Collect current state

1. Get current commit SHA.
2. Compute SHA-256 of `pkg/parser/schemas/main_workflow_schema.json`.
3. Query recent reviews and inspect their stable feature IDs:

   ```sql
   SELECT json_extract(payload, '$.run_id') AS run_id,
          json_extract(payload, '$.reviewed_at') AS reviewed_at,
          json_extract(payload, '$.reviewed_sha') AS reviewed_sha,
          json_extract(payload, '$.schema_sha256') AS schema_sha256,
          json_extract(payload, '$.open_feature_ids') AS open_feature_ids,
          json_extract(payload, '$.outcome') AS outcome
   FROM records
   WHERE json_extract(payload, '$.record_type') = 'awf_spec_surfacing_review'
   ORDER BY ordinal DESC LIMIT 100;
   ```
4. Build the diff window from the newest valid `reviewed_sha` (if present) to `HEAD`; if absent, use the last 7 days.

### 2) Detect candidate AWF feature changes

Focus on additions/semantic changes affecting users:

- new top-level or nested schema properties
- new enums/keywords/validation constraints
- new compiler behavior that enables previously unsupported syntax
- new directives or frontmatter behavior

Use `awf-change-detector` for candidate extraction from schema/compiler diffs.

### 3) Evaluate surfacing gaps

For each candidate feature, determine if it is already surfaced in:

- docs under `.github/aw/`
- user-facing CLI/docs entry points
- upgrade/fix guidance and workflow patterns

Use `surfacing-gap-evaluator` for each candidate.

### 4) Make changes and output

For each actionable surfacing gap, make the smallest direct change to the
relevant user-facing source files (for example, syntax guidance or schema
descriptions). Do not implement underlying compiler features or make unrelated
changes. Check existing open pull requests to avoid duplicating work; if an
open issue tracks the gap but no pull request implements it, make the change
and reference that issue in the pull request.

Create exactly one pull request containing the changes, with a concise summary
of the feature, evidence (files/commits/schema keys), the surfacing gap, and
what changed. Use `###` headings and `<details>` for verbose evidence. If no
actionable surfacing gap exists, return `noop` with a brief explanation.

### 5) Persist the review event

After creating the pull request or using `noop`, query for a record whose
`payload.run_id` matches `${{ github.run_id }}`. If absent, submit one
`ledger_append` record with `record_type: awf_spec_surfacing_review`, the run ID,
UTC timestamp, current commit SHA, schema SHA-256, stable open feature IDs, and
outcome (`pull_request_created` or `noop`). Do not edit ledger branches directly or invoke compaction.

## Output Quality Bar

- Do not report cosmetic refactors as new features.
- Do not create duplicate pull requests for already-tracked feature IDs.
- Prefer fewer high-confidence items over broad speculation.
- Never finish without either `create_pull_request` or `noop`.

## Reporting Guidelines

- Use `###` (h3) or lower for all report headers; never use `#` or `##` inside the report body.
- Wrap long lists, tables, and detailed findings in `<details><summary><b>...</b></summary>...</details>` blocks for progressive disclosure.
- Structure reports as: overview → key metrics/issues → collapsible detail → next actions.
## agent: `awf-change-detector`
---
description: Extracts user-relevant feature candidates from schema/compiler diffs
model: gpt-5
---
Input: schema/compiler diff context.

Return strict JSON only:
`{"candidates":[{"id":"feature-id","title":"...","evidence":["..."],"severity":"high|medium|low"}]}`

Rules:
- include only user-visible behavior changes
- prefer stable IDs based on schema key path + behavior
- max 12 candidates (keeps review sets compact and avoids token-heavy low-signal tails)

## agent: `surfacing-gap-evaluator`
---
description: Determines if a candidate feature is sufficiently surfaced in gh-aw
model: gpt-5
---
Input: one candidate + relevant docs/CLI/context snippets.

Return strict JSON only:
`{"id":"feature-id","already_surfaced":true|false,"gap_summary":"...","recommended_actions":["..."]}`

If surfaced, keep `recommended_actions` empty.

## skill: `awf-surfacing-criteria`
---
description: Criteria for deciding whether AWF features need gh-aw surfacing work
---
- treat schema and compiler behavior as the feature source of truth
- mark as surfaced only when users can discover and apply the feature without reading implementation code
- require at least one concrete user-facing surface: syntax docs, command/docs guidance, or upgrade/fix guidance
- prefer actionable deltas: what changed, where users encounter it, and what must be updated