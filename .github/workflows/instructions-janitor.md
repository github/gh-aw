---
private: true
on:
  schedule: daily
  workflow_dispatch: null
permissions:
  contents: read
  issues: read
  pull-requests: read
  copilot-requests: write
network:
  allowed:
  - defaults
  - github
imports:
- shared/pr-safe-output-defaults.md
- shared/reporting.md
- shared/otlp.md
safe-outputs:
  create-pull-request:
    allowed-files:
    - .github/aw/**
    labels:
    - documentation
    - automation
    - instructions
    protected-files: allowed
    title-prefix: "[instructions] "
description: Reviews and cleans up instruction files to ensure clarity, low duplication, and sub-400-line files for agentic consumption
emoji: 🧹
engine:
  id: claude
  model-provider: github
  model-routing:
    goal: cost
    mode: balanced
name: Instructions Janitor
strict: true
timeout-minutes: 20
tools:
  bash:
  - cat .github/aw/*.md
  - wc -l .github/aw/*.md
  - "git log --since=\"*\" --pretty=format:\"%h %s\" -- docs/ .github/aw/"
  - "git log --since=\"*\" --name-only --pretty=format:\"%h %s\" -- docs/ .github/aw/"
  - "git diff *..HEAD -- docs/ .github/aw/"
  - "git show --stat *"
  - ls .github/aw/
  cache-memory: true
  cli-proxy: false
  edit: null
  github:
    mode: local
    toolsets:
    - default
evals:
  - id: edited-files-within-targets
    question: For a proposed instruction-cleanup PR, do the resulting edited instruction files meet their applicable file-layout line targets? Return UNKNOWN when no PR is proposed.
  - id: duplication-consolidated
    question: When a patch addresses duplicated guidance, does it replace repeated rules with references to a single focused canonical location? Return UNKNOWN when no duplication cleanup is proposed.
---

# Instructions Janitor

Keep instruction files in `.github/aw/` synchronized with the product, compact enough for agentic loading, and free of avoidable duplication.

## File Layout Goals

Use small, topic-focused files.

| File | Role | Target |
|---|---|---|
| `github-agentic-workflows.md` | Compact entry point and reference table | `< 200` lines |
| `syntax.md` | Schema index only | `< 100` lines |
| `syntax-*.md` | Focused schema detail files | `< 400` lines each |
| `safe-outputs.md` | Safe-output index only | `< 120` lines |
| `safe-outputs-*.md` | Focused safe-output detail files | `< 400` lines each |
| `create-agentic-workflow.md` | Creator prompt | `< 400` lines |
| `update-agentic-workflow.md` | Updater prompt | `< 400` lines |
| `debug-agentic-workflow.md` | Debugger prompt | `< 400` lines |
| `create-shared-agentic-workflow.md` | Shared-component prompt | `< 400` lines |
| `charts.md` | Compact charting overview | `< 250` lines |
| any new `.github/aw/*.md` file | Single focused topic | `< 400` lines |

## Mission

1. Keep instructions aligned with current code and documentation.
2. Keep each file compact enough for targeted loading.
3. Remove duplicate guidance by consolidating shared rules into one file and cross-linking.
4. Prefer imperative, high-density wording and minimal examples.

## Required Audits

### Tool and permission boundaries

- Use native GitHub MCP tools for release metadata and remote reads, and native read/search/edit tools for local files.
- Bash is only for the declared audit commands. Invoke each separately from the existing workspace; do not add pipelines, command chains, redirection, substitutions, or helper scripts.
- Use native `get_latest_release` for the latest release tag and publication date. If shallow checkout history cannot resolve the tag, use native GitHub commit/file reads; do not fetch or expand shell permissions.
- Publish edits through the native `create_pull_request` safe-output tool with structured arguments following its discovered schema. Do not invoke the `safeoutputs` CLI, serialize a payload in Bash, or pipe a PR body through `jq`.
- After a denied or unavailable required call, stop that operation and call native `report_incomplete` with the blocked tool, audit phase, and edited paths. Never repeat a denied write in another syntax or claim the PR was created.

### 1. Release and change audit

- find the latest release tag
- run a single bounded history pass:
  ```bash
  git log --since=<release-date> --name-only --pretty=format:"%h %s" -- docs/ .github/aw/
  ```
- if there are relevant changes, run one batched diff to review all affected content at once:
  ```bash
  git diff <release-tag>..HEAD -- docs/ .github/aw/
  ```
- inspect individual commits only when the diff alone is insufficient; cap detail inspection at **5 commits maximum**
- inspect changed code when instruction accuracy depends on implementation details

### 2. Size audit

Run:

```bash
wc -l .github/aw/*.md
```

Rules:

- any file over `400` lines must be split or rewritten before the task is considered complete
- if a file is close to the limit, prefer extracting a dedicated sub-file before adding more content
- keep index files short and route detail to sub-files

### 3. Duplication audit

Look for repeated content across creator, updater, debugger, and reference files.

Common duplication to eliminate:

- file structure and recompilation rules
- single-job architectural constraints
- safe-output security posture
- long trigger-selection tutorials copied into multiple prompts

When duplication is found:

- move the shared rule into one focused file
- replace copies with a short reference

### 4. Accuracy audit

Treat code as the source of truth for schema and safe outputs.

Review at least:

- `pkg/workflow/compiler_types.go`
- `pkg/workflow/safe_outputs_config.go`
- `pkg/parser/schemas/main_workflow_schema.json`

Update the instruction files if behavior changed even when docs commits did not mention it.

## Editing Principles

- make surgical edits
- reduce wording before adding new files
- split by topic, not by arbitrary size alone
- keep examples representative and minimal
- do not duplicate the same concept across multiple files
- prefer references to detailed sub-files instead of restating them

## PR Expectations

If you made changes, open a PR titled:

`[instructions] Sync instruction files with release X.Y.Z`

Call native `create_pull_request` once with the title and body as argument fields after completing the edits and checks. Follow its generated patch/commit instructions; do not push a branch or use `gh pr create`. If no edits are needed after the required audits, call native `noop` with the audit result before finishing.

Include:

- files changed
- documentation commits reviewed
- before/after line counts for modified instruction files
- confirmation that duplicated content was removed or reduced
- confirmation that every edited `.github/aw/*.md` file is under the target limit or explain the exception

## Edge Cases

- if the Release/Change audit finds no relevant docs or `.github/aw/` changes **and** the Size audit confirms all files are under their limits, exit immediately without running the Duplication or Accuracy audits
- if no documentation changed, still run the size, duplication, and safe-output accuracy audits
- if instructions are already current, exit without edits
- if a file needs more than one new topic section to stay compact, create more than one focused sub-file instead of keeping one large catch-all file