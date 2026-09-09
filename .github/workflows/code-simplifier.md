---
private: true
emoji: "🔧"
name: Code Simplifier
description: Analyzes recently modified code and creates pull requests with simplifications that improve clarity, consistency, and maintainability while preserving functionality
on:
  schedule: daily

max-turns: 50
max-ai-credits: 1000
max-daily-ai-credits: 5000
permissions:
  contents: read
  issues: read
  pull-requests: read

tracker-id: code-simplifier

imports:
  - shared/mcp-pagination.md
  - uses: shared/skip-if-issue-open.md
    with:
      title-prefix: "[code-simplifier]"
      kind: "pr"
  - uses: shared/daily-pr-base.md
    with:
      title-prefix: "[code-simplifier] "
      expires: "1d"
      labels: [refactoring, code-quality, automation]
      reviewers: [copilot]
      protected-files:
        exclude:
          - .github/extensions/

  - shared/otlp.md
  - shared/graders.md
network:
  allowed:
    - go

sandbox:
  agent:
    runtime: cloud-hypervisor
tools:
  cli-proxy: true
  github:
    mode: local
    toolsets: [default]
  bash: ["*"]

steps:
  - name: Prepare recent-change dataset (deterministic)
    env:
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
      EXPR_GITHUB_REPOSITORY: ${{ github.repository }}
    run: |
      set -euo pipefail
      : "${GH_TOKEN:?GH_TOKEN is required for gh CLI queries}"
      mkdir -p /tmp/gh-aw/agent/code-simplifier

      YESTERDAY=$(date -d '1 day ago' '+%Y-%m-%d' 2>/dev/null || date -v-1d '+%Y-%m-%d')
      echo "$YESTERDAY" > /tmp/gh-aw/agent/code-simplifier/yesterday.txt

      git log --since='24 hours ago' --no-merges --pretty=format:'%H%x09%s' \
        | jq -R 'select(length > 0) | split("\t") | {sha: .[0], subject: (.[1] // "")}' \
        > /tmp/gh-aw/agent/code-simplifier/recent-commits.jsonl || true

      git log --since='24 hours ago' --name-only --pretty=format:'' --no-merges \
        | sed '/^$/d' \
        | sort -u \
        > /tmp/gh-aw/agent/code-simplifier/recent-files.txt

      gh pr list \
        --repo "$EXPR_GITHUB_REPOSITORY" \
        --state merged \
        --search "merged:>=$YESTERDAY" \
        --limit 100 \
        --json number,title,mergedAt,url \
        > /tmp/gh-aw/agent/code-simplifier/recent-prs.json || echo '[]' > /tmp/gh-aw/agent/code-simplifier/recent-prs.json

      jq -R -s '
        split("\n")
        | map(select(length > 0))
        | map(select(test("\\.(go|js|cjs|mjs|ts|tsx|py|cs|java|rb|php|kt|swift)$")))
        | map(select(test("(_test\\.|\\.test\\.|\\.spec\\.|package-lock\\.json$|pnpm-lock\\.yaml$|yarn\\.lock$|go\\.sum$|Cargo\\.lock$|\\.generated\\.|/generated/|/dist/|/build/)"; "i") | not))
        | .[0:20]
      ' /tmp/gh-aw/agent/code-simplifier/recent-files.txt > /tmp/gh-aw/agent/code-simplifier/source-files.json

      jq -n \
        --arg date "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
        --arg yesterday "$YESTERDAY" \
        --slurpfile prs /tmp/gh-aw/agent/code-simplifier/recent-prs.json \
        --slurpfile files /tmp/gh-aw/agent/code-simplifier/source-files.json \
        '{generated_at:$date, window_start:$yesterday, candidate_file_cap:20, merged_prs:($prs[0] // []), candidate_files:($files[0] // [])}' \
        > /tmp/gh-aw/agent/code-simplifier/recent-context.json

  - name: Prepare workflow history summary (deterministic)
    env:
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
      EXPR_GITHUB_REPOSITORY: ${{ github.repository }}
    run: |
      set -euo pipefail
      : "${GH_TOKEN:?GH_TOKEN is required for gh CLI queries}"
      mkdir -p /tmp/gh-aw/agent/code-simplifier

      gh api "repos/$EXPR_GITHUB_REPOSITORY/actions/workflows/code-simplifier.lock.yml/runs?per_page=30" \
        > /tmp/gh-aw/agent/code-simplifier/workflow-runs.json || echo '{"workflow_runs":[]}' > /tmp/gh-aw/agent/code-simplifier/workflow-runs.json

      jq '
        .workflow_runs // []
        | {
            sample_size: length,
            success_count: (map(select(.conclusion == "success")) | length),
            failure_count: (map(select(.conclusion == "failure")) | length),
            cancelled_count: (map(select(.conclusion == "cancelled")) | length),
            recent_failures: (map(select(.conclusion == "failure")) | map({id,created_at,html_url}) | .[0:5]),
            deterministic_candidates: [
              {
                name: "recent PR and commit discovery",
                move_to: "steps",
                reason: "This data is fetched every run and can be pre-computed once in deterministic shell steps."
              },
              {
                name: "changed-file filtering",
                move_to: "steps",
                reason: "Extension and test/generated-file filtering is deterministic and should not consume AI tokens."
              },
              {
                name: "workflow run history aggregation",
                move_to: "steps",
                reason: "Historical run metadata can be summarized via jq before entering agent context."
              }
            ]
          }
      ' /tmp/gh-aw/agent/code-simplifier/workflow-runs.json > /tmp/gh-aw/agent/code-simplifier/history-summary.json

timeout-minutes: 30
strict: true
evals:
  - id: code_analyzed
    question: Did the agent analyze recently modified code for simplification opportunities?
  - id: pr_created_or_noop
    question: Was a pull request created with simplifications, or was noop used when no improvements were needed?

---

# Code Simplifier Agent

Preserve behavior exactly while simplifying recently changed production code for readability and maintainability.

## Required Inputs (already precomputed)

Use these deterministic files first:

- `/tmp/gh-aw/agent/code-simplifier/recent-context.json`
- `/tmp/gh-aw/agent/code-simplifier/source-files.json`
- `/tmp/gh-aw/agent/code-simplifier/recent-commits.jsonl`
- `/tmp/gh-aw/agent/code-simplifier/recent-prs.json`
- `/tmp/gh-aw/agent/code-simplifier/history-summary.json`

Do **not** re-fetch these datasets with GitHub tools unless a required file is missing, empty, or fails JSON parsing.

## Token Budget Rules

- Keep responses concise and operational.
- Read only files in the candidate list.
- Avoid loading full payloads when filtered data already exists.
- Prefer deterministic shell outputs and compact JSON over repeated tool queries.

## Command Guardrails (Required)

- Do **NOT** use `python3` for JSON parsing; use `jq`, `cat`, or `head` instead.
- Do **NOT** repeatedly retry variations of the same blocked command.
- If a command fails due to permission/policy, stop that approach immediately and use `report_incomplete` with the blocked command and error.
- If you hit repeated permission-denied errors for the same action, short-circuit instead of continuing retries.
- If you encounter 3 or more consecutive `Permission denied` errors for the same type of command, stop immediately and call `report_incomplete`.

## Phase 1 — Determine Scope

1. Read `recent-context.json` and `source-files.json`.
2. If no candidate files exist, call `noop` with this exact message:

```
✅ No code changes detected in the last 24 hours.
Code simplifier has nothing to process today.
```
3. If candidate files hit the deterministic cap (`candidate_file_cap`), process only the provided list and do not discover additional files in this run.

## Phase 2 — Historical Optimization Gate

1. Read `history-summary.json`.
2. Review `deterministic_candidates` and keep those operations in deterministic paths.
3. If a planned action duplicates one of those candidates, use the precomputed file instead of a tool call.

## Phase 3 — Analyze and Simplify

For each file in `source-files.json`:

1. Use `scope-filter` (`model: claude-haiku-4.5`) to confirm the file should be simplified and detect language.
2. Apply only the matching language skill guidance (do not load unrelated language guidance).
3. Use `simplification-scout` (`model: claude-haiku-4.5`) for extractive opportunity scoring.
4. Make targeted edits that preserve behavior.

Priorities:

- remove needless branching or nesting
- remove duplication and dead local abstractions
- improve naming clarity
- avoid clever compression that hurts readability
- keep interfaces and behavior unchanged

## Phase 4 — Validate

Run project checks after edits:

```bash
make test-unit
make lint
make build
```

If validation fails, adjust or revert risky simplifications.

## Phase 5 — PR Decision

Create a PR only if all are true:

- meaningful simplifications were made
- behavior is preserved
- `make test-unit`, `make lint`, and `make build` succeed

If no useful change remains, call `noop` with this exact message:

```
✅ Code analyzed from last 24 hours.
No simplifications needed - code already meets quality standards.
```

If you are close to the run ceiling (turn budget, time budget, or AI-credit budget) before finishing, stop and call `noop` with this exact message:

```
⚠️ Code simplifier run stopped at configured per-run budget.
Processed available deterministic inputs only; no safe write action produced.
```

## PR Body Requirements

Include:

- files simplified
- concise list of simplifications
- source references from `recent-prs.json` and `recent-commits.jsonl`
- validation results
- brief note on deterministic pre-processing and token-efficiency choices

## Output Requirements

You MUST finish by calling exactly one safe-output tool:

1. `noop` with the no-recent-code-changes message above
2. `noop` with the no-beneficial-simplifications message above
3. `noop` with the run-budget-ceiling message above
4. `create_pull_request` when meaningful validated simplifications are ready
5. `missing_tool` when blocked by unavailable tooling
6. `missing_data` when blocked by missing required data

Do not finish with plain text only. The safe-output tool call is required.
## agent: `scope-filter`
---
description: Decides whether a candidate file should be simplified and labels its primary language
model: claude-haiku-4.5
---
Input is a single file path string. Return strict JSON only:
`{"path":"...","include":true|false,"language":"go|typescript|javascript|python|csharp|other","reason":"..."}`

Set `include` to false for tests, generated files, lockfiles, vendored code, or unsupported languages.

## agent: `simplification-scout`
---
description: Extractive scout that proposes low-risk simplification opportunities per file
model: claude-haiku-4.5
---
Given file content, return strict JSON only:
`{"path":"...","opportunities":[{"kind":"clarity|duplication|complexity|naming","summary":"...","risk":"low|medium"}]}`

Report at most 5 opportunities, ordered by expected readability gain, then by lowest risk.

## skill: `js-ts-simplifier-skill`
---
description: JavaScript and TypeScript simplification standards
---
- prefer explicit, readable control flow over nested ternaries
- keep top-level functions and exported APIs clear and typed
- maintain import hygiene and existing module conventions
- avoid behavior-changing refactors while simplifying

## skill: `go-simplifier-skill`
---
description: Go simplification standards
---
- prefer explicit error handling and clear guard clauses
- use idiomatic Go naming and keep functions focused
- preserve package boundaries and exported API behavior
- reduce unnecessary indirection where readability improves

## skill: `python-simplifier-skill`
---
description: Python simplification standards
---
- keep code explicit and readable following PEP 8 principles
- preserve type hints and improve naming clarity
- simplify conditionals and duplication without changing behavior
- prefer straightforward constructs over compact cleverness

## skill: `csharp-simplifier-skill`
---
description: C# simplification standards
---
- keep nullable and async behavior unchanged
- favor clear pattern matching and explicit control flow
- preserve public contracts and existing conventions
- remove incidental complexity while keeping intent obvious
