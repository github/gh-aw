---
private: true
name: Daily Go Test Parallelizer
description: Adds t.Parallel to safe Go tests using bounded batch analysis
on:
  schedule: every 2h
  workflow_dispatch:
  skip-if-match: 'is:pr is:open in:title "[test-parallel]"'
permissions:
  copilot-requests: write
  contents: read
  issues: read
  pull-requests: read
model: router/economy
engine:
  id: codex
  env:
    GOCACHE: /tmp/gh-aw/agent/go-build-cache
    GOMODCACHE: /tmp/gh-aw/agent/go-mod-cache
  model-provider: github
steps:
  - name: Provision disposable Go validation caches
    run: mkdir -p /tmp/gh-aw/agent/go-build-cache /tmp/gh-aw/agent/go-mod-cache /tmp/gh-aw/agent/parallelizer-validation
strict: true
timeout-minutes: 60
max-ai-credits: 25
max-turns: 60
runtimes:
  go: {}
network:
  allowed:
    - defaults
    - github
    - go
    - node
tools:
  cli-proxy: true
  github:
    mode: gh-proxy
  cache-memory:
    restore-only: true
    retention-days: 30
    allowed-extensions: [".json"]
  edit:
  bash:
    - "*"
safe-outputs:
  create-pull-request:
    title-prefix: "[test-parallel] "
    labels: [automation, testing]
    draft: true
    expires: 3d
    if-no-changes: ignore
    protected-files: blocked
    allowed-files:
      - "**/*_test.go"
    max-patch-files: 10
    max-patch-size: 2048
  noop:
evals:
  - id: tests_analyzed
    question: Did the agent analyze Go tests to identify a safe candidate for t.Parallel?
  - id: pr_created_or_noop
    question: Did the agent create a pull request for a safe test change, or use noop when no safe change was available?
imports:
  - shared/pr-safe-output-defaults.md
  - shared/reporting.md
features:
  gh-aw-detection: true
---

# Daily Go Test Parallelizer

Analyze up to three Go test files per run and add `t.Parallel()` only where parallel execution is demonstrably safe. Three files keeps analysis and package validation inside the 25-credit, 60-minute budget.

## Select a file

1. Use `grep` to list tracked `*_test.go` files containing top-level `Test` functions. Exclude `vendor/` and generated files, then sort paths lexicographically.
2. Read `/tmp/gh-aw/cache-memory/go-test-parallelizer/state.json` when it exists. It has this shape:
   `{"last_file":"path/to/file_test.go"}`.
3. Rotate the sorted list to start after `last_file`, wrapping to the first path. If the cache is absent, malformed, or names a removed file, start from the first path. Partition the rotated list into non-overlapping batches of at most 3 files; do not wrap within a batch.
4. Select batch `(github.run_number - 1) % number_of_batches`, using run number `${{ github.run_number }}`. This attempted-batch rotation permits zero consecutive retries after a block: the next run selects the next batch regardless of outcome, without changing the completed-analysis cursor. If there are no files, report a completed no-candidate analysis; if there is only one batch, report that rotation cannot select a different batch.
5. Analyze and modify only this selected batch (maximum 3 files). Keep the previous cursor unchanged; selection alone is not completed analysis.
6. Minimize token usage: first `grep -n` the batch for the exclusion patterns below, then read only the remaining candidate test functions (bounded line ranges, never whole files or long `-A` context dumps). Do not re-read analyzed code, load unrelated skills or Makefiles, or paste file contents into notes or outputs.

## Analyze safety

Add `t.Parallel()` at the start of eligible top-level tests. Also add it to eligible table-driven subtests.
For Go 1.22+ semantics, do not add redundant loop-variable rebinding (`tt := tt`, `cmd := cmd`) unless a case truly needs an additional local copy for correctness.
Apply the safety exclusions below before assessing eligibility.

## Batched analysis agent (small context)

1. Analyze the selected path list as one batch.
2. Do not create one sub-agent per file; keep the repeated static rules in this single batch call.
3. Require compact JSON output:
   `{"files":[{"file":"...","safe":true|false,"reasons":["..."],"candidate_tests":["TestName"]}]}`.
4. Use the JSON results to decide which files to edit. Keep aggregation notes short and avoid repeating unchanged rule text.

Do not change assertions, test behavior, production code, dependencies, generated files, or vendored files. When safety is uncertain, make no change.

## Validate

Use only this workflow's disposable `GOCACHE` and `GOMODCACHE`; do not inspect pre-existing caches or enumerate the process environment yourself. The Go toolchain from `go.mod` is preinstalled; do not change `GOTOOLCHAIN`.

Running the assigned `go test` commands is permitted validation, not credential or environment access by you, even when tests under test set or read environment variables, call `os.Environ`, or write dummy tokens to temporary files. Do not pre-screen packages for policy compatibility or skip a command because of what the tests do; run it and classify the actual result.

Stop after the first policy denial or unrecoverable tool failure; do not retry or probe around it. At turn 55 of 60 or 55 minutes of the 60-minute timeout, whichever comes first, checkpoint. Continue only if all remaining analysis and validation can finish with at least five turns and five minutes reserved for diff review and the terminal safe output. Otherwise restore candidate edits and report incomplete.

Print the candidate-only diff before validation; require only safe `t.Parallel()` additions in selected files. Run `go test -race -json` once per unique modified package; this is the complete required validation because test-only edits cannot affect other packages, and CI runs the full suite on the draft PR. Do not run `go test ./...`. Use `set -o pipefail` and `tee` to save each complete output under `/tmp/gh-aw/agent/parallelizer-validation/`; preserve Go's exit status. Read saved JSON failure events and nearby output, not a truncated tool excerpt. Report failing test names and errors; a package-level FAIL alone is insufficient when test evidence exists.

On failed, blocked, or out-of-scope validation, restore only your edits and report incomplete. Never omit tests, substitute static analysis, grant exceptions, or bypass denied operations. Suspected policy incompatibility is not proof of a block or no safe candidate.

Before `report_incomplete`, self-check and report:

- State the batch and exact command, denial, or test error. Classify as environment/policy blocked only for an observed denial or a specific rule that forbids an unrun command; classify a permitted nonzero test as test failure. For a budget stop without command failure, report budget-limited work, not a policy block. Otherwise report agent failure or unknown and state uncertainty; never infer a block from suspected incompatibility.
- Confirm candidate edits were restored without reverting pre-existing work, and the cursor is unchanged; identify the next attempted batch or say why it is unknown.

## Persist and report

Assess every selected file before calling analysis complete. Use `noop` only when no safe edit is available (no tests claimed); create a draft PR only when edits pass all validation and diff checks. For any partial, blocked, failed, or uncertain result, use `report_incomplete`, never `noop`.

The restore-only cache makes `state.json` a completed-analysis cursor, not an attempted-batch cursor. Never write it or its directory; preserve the cursor unchanged on every outcome. Run-number rotation advances attempted batches only; it neither persists progress nor proves analysis succeeded.

Call exactly one terminal safe-output tool. Include the selected batch, short reason, and that the cursor did not advance; include the self-check for incomplete outcomes. Use `###` or lower headings; show outcome and key results first, put long evidence in `<details>`, and format run IDs as `[§12345](https://github.com/${{ github.repository }}/actions/runs/12345)`.

Do not parallelize tests that use or may conflict through:

- `t.Setenv`, `os.Setenv`, `os.Chdir`, or other process-wide state;
- shared mutable globals, singleton state, ordering assumptions, or package mocks;
- fixed ports/paths, shared databases/services, unsafe loop capture, timing dependencies, or explicit synchronization.
