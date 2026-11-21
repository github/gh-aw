---
on:
  schedule: daily
  workflow_dispatch: null
permissions:
  contents: read
  issues: read
  pull-requests: read
  copilot-requests: write
imports:
- uses: shared/skip-if-issue-open.md
  with:
    title-prefix: "[testify-expert]"
    kind: pr
- shared/activation-app.md
- shared/go-source-analysis.md
- shared/safe-output-app.md
- shared/otlp.md
description: Daily expert that improves one Go test file and proposes validated testify-based edits in a draft PR
emoji: 🧪
engine:
  id: copilot
  copilot-sdk: true
max-tool-denials: 3
name: Daily Testify Uber Super Expert
strict: true
timeout-minutes: 20
network:
  allowed:
  - defaults
  - go
tools:
  edit:
  bash:
  - "*"
  cli-proxy: true
  github:
    mode: gh-proxy
    toolsets:
    - default
  repo-memory:
    branch-name: memory/testify-expert
    description: Tracks processed test files to avoid duplicates
    file-glob:
    - "*.json"
    - "*.txt"
    max-file-size: 51200
safe-outputs:
  threat-detection:
    report-as-issue: false
  report-failure-as-issue: false
  report-failed-jobs: false
  report-missing-tool:
    create-issue: false
  report-missing-data:
    create-issue: false
  create-pull-request:
    title-prefix: "[testify-expert] "
    labels: [testing, code-quality, automated-analysis, cookie]
    draft: true
    max: 1
    expires: 2d
    fallback-as-issue: false
    github-token-for-extra-empty-commit: none
    if-no-changes: error
    protected-files: blocked
    allowed-files:
    - "pkg/**/*_test.go"
    - "cmd/**/*_test.go"
    max-patch-files: 1
  noop:
    report-as-issue: false
  report-incomplete:
    create-issue: false
tracker-id: daily-testify-uber-super-expert
features:
  gh-aw-detection: true
sandbox:
  agent:
    id: awf
---

{{#runtime-import? .github/shared-instructions.md}}

# Daily Testify Uber Super Expert 🧪✨

Improve one tracked Go `*_test.go` file under `pkg/` or `cmd/` per run using testify best practices. Submit at most one draft PR containing actual, validated edits; never create an issue or a proposal-only PR.

For this workflow only, override the shared Go analysis instruction to skip `_test.go`: use Serena to inspect and edit the selected test file, with its paired source read-only. Leave the shared instructions and all other files unchanged.

## Current Context

- **Repository**: ${{ github.repository }}
- **Analysis Date**: $(date +%Y-%m-%d)
- **Workspace**: ${{ github.workspace }}
- **Cache Location**: `/tmp/gh-aw/repo-memory/default/memory/testify-expert/`

## Required Execution Flow

### 1) Load cache and choose one target file

- Read `/tmp/gh-aw/repo-memory/default/memory/testify-expert/processed_files.txt` when present (`path|YYYY-MM-DD`).
- Build a sorted candidate list from `git ls-files 'pkg/*_test.go' 'cmd/*_test.go'`. Exclude generated files.
- Prefer files never processed or processed more than 30 days ago.
- Read open PRs with the `[testify-expert]` title prefix. If one exists, use `noop`; do not create competing work. Also skip a candidate already changed by any open PR.
- If all files were processed recently or no eligible candidate remains, use `noop` with the reason.

### 2) Analyze with Serena + code inspection

For the selected test file:

- Read file structure and identify its paired source file (`*_test.go` → `.go` when it exists).
- Assess testify usage (`assert.*` vs `require.*`), table-driven patterns, naming clarity, isolation, and edge-case coverage.
- Compare exported source functions vs test functions to identify likely coverage gaps.
- Choose a small, concrete improvement: high-value missing cases, appropriate `require` for fatal prerequisites and `assert` for independent checks, or a focused table-driven refactor. Preserve intended behavior and existing conventions.
- Implement the improvement in the selected file. Do not weaken or remove assertions, skip tests, modify production code, add dependencies, or edit workflows, generated files, vendored files, or other tests.
- If no demonstrably useful change is available, use `noop`. If Serena fails, report the error and stop without publishing; do not substitute text-only analysis.

### 3) Validate the edits

- Record the baseline worktree state; never revert pre-existing edits.
- Run `gofmt -w <TARGET_FILE>` and `git diff --check`.
- Run `go test <SELECTED_PACKAGE> -count=1` for the package containing the file, using its repository-relative `./pkg/...` or `./cmd/...` path. Preserve required build tags and repository test conventions. Do not use a narrow test-name filter to avoid failing tests.
- Inspect `git diff` and `git status --short`: require a nonempty patch containing only the selected test file. No source, dependency, or unrelated formatting changes are allowed.
- On failed or blocked validation, restore only your edits, use `report_incomplete` with the exact command/error, and stop without a PR or cache update. Do not describe failure as successful `noop`, weaken tests, or bypass a denial.

### 4) Submit one draft PR

Recheck open PRs for the prefix and selected path immediately before submission; if competing work appeared, restore only your edits and use `noop`. Otherwise stage only the selected test file, commit locally, and submit that branch with `create_pull_request` and the title `Improve Test Quality: <FILE_PATH>`. Include the selected path, concise rationale, assertion/coverage changes, and exact validation commands and results. Use `###`/`####` headers only; put long evidence in `<details>`.

Do not push branches, create PRs with `gh` or raw APIs, request auto-merge, assign an agent, or create fallback issues. The safe output is the only publication path.

### 5) Update cache only after confirmed publication

- Only after confirmed PR publication, append `<TARGET_FILE>|<TODAY>` to `processed_files.txt`.
- Deduplicate by file path, keeping the newest date.
- A recorded safe-output call or `output_id` is not publication confirmation. If publication is pending or failed, leave the cache unchanged and report that state.

## Output Requirements

### If no eligible file exists

Use `noop` with the actual reason (recently processed, duplicate, no eligible file, or no useful change); do not claim tests ran when no edit was validated.

### If analysis was completed

Return a compact summary with selected file, implemented change, validation results, PR submission/publication state, and whether the cache changed. Do not claim a PR exists without publication confirmation.

## Guardrails

- One file per run.
- Prefer a small reviewable patch over broad refactoring or long prose.
- Follow patterns in `scratchpad/testing.md` and nearby `pkg/**/_test.go` files.
- Keep the cache unchanged on no-op, incomplete validation, or unconfirmed publication.