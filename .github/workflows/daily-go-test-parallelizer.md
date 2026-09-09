---
private: true
name: Daily Go Test Parallelizer
description: Adds t.Parallel to safe Go tests using daily round-robin analysis
on:
  schedule: every 2h
  workflow_dispatch:
  skip-if-match: 'is:pr is:open in:title "[test-parallel]"'
permissions:
  copilot-requests: write
  contents: read
  issues: read
  pull-requests: read
engine:
  id: codex
  model-provider: github
model: copilot/gpt-5.3-codex
strict: true
timeout-minutes: 30
network:
  allowed:
    - defaults
    - go
    - node
tools:
  cli-proxy: true
  github:
    mode: gh-proxy
  cache-memory:
    retention-days: 30
    allowed-extensions: [".json"]
  edit:
  bash:
    - "*"
safe-outputs:
  steer: true
  create-pull-request:
    title-prefix: "[test-parallel] "
    labels: [automation, testing]
    draft: true
    expires: 3d
    if-no-changes: ignore
    protected-files: blocked
    allowed-files:
      - "**/*_test.go"
    max-patch-files: 25
    max-patch-size: 2048
  noop:
evals:
  - id: tests_analyzed
    question: Did the agent analyze Go tests to identify a safe candidate for t.Parallel?
  - id: pr_created_or_noop
    question: Did the agent create a pull request for a safe test change, or use noop when no safe change was available?
imports:
  - shared/reporting.md
features:
  gh-aw-detection: true
---

# Daily Go Test Parallelizer

Analyze up to twenty-five Go test files per run and add `t.Parallel()` only where parallel execution is demonstrably safe.

## Select a file

1. Use `grep` to list tracked `*_test.go` files containing top-level `Test` functions. Exclude `vendor/` and generated files, then sort paths lexicographically.
2. Read `/tmp/gh-aw/cache-memory/go-test-parallelizer/state.json` when it exists. It has this shape:
   `{"last_file":"path/to/file_test.go"}`.
3. Select up to 25 consecutive paths starting after `last_file`, wrapping to the first path. If the cache is absent, malformed, or names a removed file, start from the first path.
4. Analyze and modify only this selected batch (maximum 25 files).
5. Minimize token usage: avoid re-reading files you already analyzed, and do not paste full file contents into notes or outputs.

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

After editing the selected batch:

1. Run `go test -race` once per unique modified package (deduplicate package paths across edited files).
2. Run `go test ./...` once after all candidate edits.
3. Inspect the diff and confirm it contains only safe `t.Parallel()` additions in selected files.
4. Revert the edit and use `noop` if any test command fails or the diff contains any other change.

## Persist and report

Always create `/tmp/gh-aw/cache-memory/go-test-parallelizer/` and write the last path from the selected batch to `state.json`, even when no edit is safe, so the next daily run advances round-robin.

If validation succeeds with a change, create one draft pull request describing the safety analysis and test results. Otherwise use `noop` with the selected path and a short reason.

Do not parallelize tests that use or may conflict through:

- `t.Setenv`, `os.Setenv`, `os.Chdir`, or other process-wide state
- shared mutable globals, singleton state, ordering assumptions, or package-level mocks
- fixed ports, fixed filesystem paths, shared databases, or shared external services
- unsafe loop-variable capture, timing dependencies, or explicit synchronization between tests
