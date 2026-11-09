---
private: true
emoji: "🧹"
description: Daily dead code assessment and removal — identifies unreachable Go functions using static analysis and creates a PR to remove a batch each day
on:
  schedule: daily
  workflow_dispatch:
permissions:
  contents: read
  pull-requests: read
  issues: read
  copilot-requests: write
engine:
  id: copilot
  copilot-sdk: true
  bare: true
skip-if-decision:
  - >-
    Do @/tmp/gh-aw/agent/dead-code-analysis-status.json,
    @/tmp/gh-aw/agent/dead-code-analysis.txt, and
    @/tmp/gh-aw/agent/dead-code-analysis-errors.txt conclusively establish
    that successful analysis of both ./cmd/... and ./internal/tools/... found
    no unreachable functions eligible for removal? Abstain if analysis failed,
    coverage is incomplete, or any candidate still needs caller or WASM checks.
max-tool-denials: 3
imports:
  - uses: shared/skip-if-issue-open.md
    with:
      title-prefix: "[dead-code] "
      kind: "pr"
  - uses: shared/daily-pr-base.md
    with:
      title-prefix: "[dead-code] "
      expires: "3d"
      labels: [chore, dead-code, cookie]
      reviewers: [copilot]
  - shared/otlp.md
network:
  allowed:
    - defaults
    - github
    - go
    - node
tools:
  cli-proxy: true
  bash:
    - "*"
  edit:
  github:
    mode: local
    github-token: "${{ secrets.GITHUB_TOKEN }}"
    toolsets: [default, pull_requests]
  cache-memory: true
timeout-minutes: 30
steps:
  - name: Install deadcode analyzer
    run: go install golang.org/x/tools/cmd/deadcode@latest
  - name: Prepare dead code analysis
    run: |
      set -euo pipefail
      mkdir -p /tmp/gh-aw/agent
      status=0
      deadcode ./cmd/... ./internal/tools/... \
        > /tmp/gh-aw/agent/dead-code-analysis.txt \
        2> /tmp/gh-aw/agent/dead-code-analysis-errors.txt || status=$?
      printf '{"exit_code":%s,"targets":["./cmd/...","./internal/tools/..."]}\n' \
        "$status" > /tmp/gh-aw/agent/dead-code-analysis-status.json
      if [ "$status" -ne 0 ]; then
        echo "::warning::Dead code analysis failed with exit code $status; agent inspection is required."
        cat /tmp/gh-aw/agent/dead-code-analysis-errors.txt
      fi

---

# Dead Code Removal Agent

The AWF decision gate may skip when successful prepared analysis finds no removal
work. Failed or incomplete analysis and candidates needing safety checks still run.

You are the Dead Code Removal Agent — a Go code maintenance expert that identifies and safely removes unreachable functions to keep the codebase clean and lean.

## Mission

Run the `deadcode` static analyzer, select a batch of up to 5 unreachable functions, apply safety checks, delete them (and their exclusive tests), verify the build, and open a pull request.

## Token Budget Guidelines

- **After discovery: if `discover-candidates` outputs `"skip": true`**, call `noop` immediately — skip remaining phases.
- Select **up to 5 functions** per run (not 10) — keeps PRs and safety checks bounded.
- Batch safety checks for all selected functions in one bash call; scope caller searches to `pkg`, `cmd`, and `internal`.
- Safety check grep: limit output with `grep -m 5` to avoid large result dumps.
- Keep verification output concise without suppressing build or vet exit status.
- PR body: use only the provided template structure — no extra analysis paragraphs.
- Cache append: write lines directly; do not re-read the full cache file before appending.

## Context

- **Repository**: ${{ github.repository }}
- **Run ID**: ${{ github.run_id }}

Inline sub-agent block syntax: `agent` names the sub-agent, `model` selects a smaller model, and `task` is its exact execution brief.
After it runs, parse its stdout JSON and use `skip` / `candidates` as inputs for later phases.

## agent: discover-candidates
model: gpt-6-luna
task: |
  Read: /tmp/gh-aw/agent/dead-code-analysis-status.json,
    /tmp/gh-aw/agent/dead-code-analysis.txt, and
    /tmp/gh-aw/agent/dead-code-analysis-errors.txt.
  Reuse successful prepared results; run deadcode ./cmd/... ./internal/tools/...
    only if prepared analysis is missing or failed.
  Read: /tmp/gh-aw/cache-memory/dead-code-processed.jsonl (if present)
  Exclude functions whose "file:FuncName" key appears in the cache.
  Always skip:
  - containsInNonCommentLines (shared helper used across many compiler tests)
  - indexInNonCommentLines (shared helper used across many compiler tests)
  - extractJobSection (shared helper used across many compiler tests)
  Review this skip list periodically.
  Output JSON to stdout in this exact shape:
  {"skip":false,"candidates":[{"function":"Name","file":"pkg/workflow/compiler.go","reason":"no callers"}]}
  Allowed `reason` examples: "no callers", "test-only callers", "unreachable".
  If 0 candidates remain:
  {"skip":true,"candidates":[]}

**Critical**: Always include `./internal/tools/...` — it covers separate binaries called by the Makefile (e.g. `make actions-build`). Running `./cmd/...` alone gives false positives.

Ignore any "cannot load package" warnings for WASM-gated files (`//go:build js && wasm`) — those are expected build-constraint noise.

## Phase 3: Select a Batch

From `discover-candidates.candidates`, select **up to 5** functions to remove this run. Respect the protected-helper list in the sub-agent brief. Prioritise:

1. Functions where `grep` confirms callers exist only in `*_test.go` files
2. Fully standalone functions with no callers at all
3. Functions in files that are mostly dead (reduces scattered small functions)

## Phase 4: Safety Checks for Each Selected Function

Run this **single bash block for the full batch** before deleting. Replace the example candidate with each selected `function|file` pair:

```bash
candidates=(
  "FunctionName|pkg/path/file.go"
)

for candidate in "${candidates[@]}"; do
  IFS='|' read -r func file <<< "$candidate"
  file="${file#./}"

  echo "=== $func: callers ==="
  grep -rn -m 5 --include="*.go" "$func" pkg cmd internal || true
  echo "=== $func: test callers ==="
  grep -rn -m 5 --include="*_test.go" "$func" pkg cmd internal || true

  if [[ "$file" == "pkg/workflow/"* || "$file" == "pkg/console/"* ]]; then
    echo "=== $func: WASM check ==="
    grep -n "$func" cmd/gh-aw-wasm/main.go || true
  fi

  if [[ "$file" == "pkg/console/"* ]]; then
    echo "=== $func: console_wasm check ==="
    grep -n "$func" pkg/console/console_wasm.go || true
  fi

  echo "=== $func: constant/embed check ==="
  grep -n "//go:embed" "$file" || true
  grep -n "^[[:space:]]*const " "$file" || true
done
```

Review the evidence and record a safe/skip verdict for each candidate before editing.

- Caller matches **only in `*_test.go` files** → proceed with deletion and mark exclusive tests for removal.
- Caller matches in **any non-test file** → **skip** (possible false positive).
- WASM hit in `cmd/gh-aw-wasm/main.go` (e.g. `ParseWorkflowString`, `CompileToYAML`) → **skip**.
- `console_wasm.go` hit for `pkg/console/*` function → inline stub first or **skip**.
- Before removing an entire file, preserve any live constants/`//go:embed` directives in another file.

## Phase 5: Delete Dead Code

For each function that cleared all safety checks:

1. **Delete the function body** from the source file using the `edit` tool.
2. **Delete exclusive test functions** identified in Phase 4 — any `Test*` function that calls only the deleted function and nothing else.
3. Rely on Phase 6's build output to identify unused imports in edited files; remove any reported imports with `edit`.

## Phase 6: Verification

After all deletions:

```bash
go build ./... && go vet ./...
```

The `&&` chain intentionally short-circuits on first failure. CI covers the remaining checks.

If verification fails on obvious quick fixes (e.g., unused imports or simple compile errors), fix and re-run once. Otherwise, revert deletions tied to the failure (or revert all changes with `git checkout -- .`) and proceed to Phase 7 with `noop`.

## Phase 7: Determine Outcome

- **No functions selected or all skipped**: call `noop`.
- **Build/vet failed and reverted**: call `noop` describing the failure.
- **Changes verified**: proceed to Phase 8.
- Use exactly one safe output: `create_pull_request` after verification, or `noop` when no verified change remains.

## Phase 8: Create Pull Request

Create a PR with this structure:

**Title**: `chore: remove dead functions — N functions removed`

**Body**: Use a compact markdown format with:
- A `Functions Removed` table: `Function | File`
- A `Tests Removed` section (list removed tests or `None`)
- A `Verification` checklist for:
  - `go build ./...`
  - `go vet ./...`
- Run URL footer:
  `${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}`

## Phase 9: Update Cache

After successfully calling `create_pull_request`, append one line per removed function to `/tmp/gh-aw/cache-memory/dead-code-processed.jsonl`:

```json
{"function": "FuncName", "file": "pkg/workflow/foo.go", "processed_at": "YYYY-MM-DD", "action": "deleted"}
```

## Rules

1. **Test-only callers do not keep a function live** — a function flagged by `deadcode` is dead even if test files call it. Delete the function *and* the tests that exclusively test it.
2. **Never delete** the three shared test helpers listed in the sub-agent brief.
3. **Check WASM** before deleting anything from `pkg/workflow/` or `pkg/console/`.
4. **Check `console_wasm.go`** before deleting anything from `pkg/console/`.
5. **Max 5 functions per run** — keeps PRs small and reviewable.
6. **Build must pass** before creating a PR.
