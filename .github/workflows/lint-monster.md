---
private: true
emoji: "🧌"
name: LintMonster
description: Daily workflow that runs custom linters, groups findings, and launches up to three Copilot agent sessions to fix lint issues
on:
  schedule: daily
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  discussions: read
  pull-requests: read


tracker-id: lint-monster
model: openai/gpt-5.3-codex
engine:
  id: codex
  model-provider: openai
strict: true
timeout-minutes: 45
tools:
  cli-proxy: true
  github:
    mode: local
    toolsets: [default, issues, discussions]
  bash:
    - "*"
steps:
  - name: Run custom lint pre-check
    id: lint_scan
    run: |
      set -euo pipefail
      mkdir -p /tmp/gh-aw/agent
      rm -f /tmp/gh-aw/agent/lint-clean.flag

      if make golint-custom > /tmp/gh-aw/agent/golint-custom.log 2>&1; then
        : > /tmp/gh-aw/agent/lint-diagnostics.txt
        : > /tmp/gh-aw/agent/skill-index.txt
        touch /tmp/gh-aw/agent/lint-clean.flag
        exit 0
      fi

      grep -E '^[^:]+:[0-9]+:[0-9]+:' /tmp/gh-aw/agent/golint-custom.log > /tmp/gh-aw/agent/lint-diagnostics.txt || true
      diag_count=$(wc -l < /tmp/gh-aw/agent/lint-diagnostics.txt | tr -d ' ')
      if [ "${diag_count}" -eq 0 ]; then
        grep -E '^[[:space:]]*[^[:space:]].*$' /tmp/gh-aw/agent/golint-custom.log | head -n 50 > /tmp/gh-aw/agent/lint-diagnostics.txt || true
        diag_count=$(wc -l < /tmp/gh-aw/agent/lint-diagnostics.txt | tr -d ' ')
      fi

      find .github/skills -maxdepth 6 -name 'SKILL.md' | sort > /tmp/gh-aw/agent/skill-index.txt
      echo "Lint diagnostics captured: ${diag_count}"

safe-outputs:
  create-issue:
    expires: 7d
    title-prefix: "[lint-monster] "
    labels: [automation, lint, cookie]
    max: 3
  close-issue:
    max: 10
    required-title-prefix: "[lint-monster] "
    state-reason: duplicate
  update-issue:
    max: 10
    title-prefix: "[lint-monster] "
    target: "*"
  assign-to-agent:
    max: 3
    target: "*"
    allowed: [copilot]
  create-discussion:
    expires: 2d
    category: audits
    title-prefix: "[lint-monster] "
    max: 1
    close-older-discussions: true
  noop:

imports:
  - shared/otlp.md
  - shared/reporting.md
sandbox:
  agent:
    runtime: cloud-hypervisor
---

{{#runtime-import? .github/shared-instructions.md}}

# LintMonster

You are **LintMonster**, a daily custom-linter remediation orchestrator.

## Mission

Use the pre-check lint output from `make golint-custom`. If lint is clean, do nothing. If lint issues exist, group them and launch up to three Copilot agent sessions to resolve the groups.

## Runtime Inputs

Read:
- `/tmp/gh-aw/agent/golint-custom.log`
- `/tmp/gh-aw/agent/lint-diagnostics.txt`
- `/tmp/gh-aw/agent/skill-index.txt`
- `/tmp/gh-aw/agent/lint-clean.flag` (exists only when lint is already clean)

## Skill mining and fusion (required)

1. Read `/tmp/gh-aw/agent/skill-index.txt` and identify the minimum relevant skill material for fixing custom linter findings.
2. Use **skill fusion**: extract only precise fragments instead of loading full skills broadly.

<!-- gh-skill-fusion: .github/skills/go-linters/SKILL.md#build-and-test-linters -->
Use these fused constraints while creating remediation instructions:
- Validate fixes by running `make golint-custom`.
- Keep remediation scoped to findings in the assigned lint group.
- Prefer minimal, targeted code edits.
<!-- End fusion -->

Convert fused guidance into clear, actionable instructions that Copilot can execute for each lint group issue.

## Required flow

1. If `/tmp/gh-aw/agent/lint-clean.flag` exists, call `noop` and stop.
2. Group findings from `/tmp/gh-aw/agent/lint-diagnostics.txt` into **at most three** distinct sets by root cause first, then subsystem/path prefix when helpful.
3. Treat all `largefunc` / long-function / function-length findings in `pkg/workflow` and `pkg/cli` as **one shared tracking topic** named `function-length refactoring`.
   - Do **not** open separate issues for `pkg/workflow`, `pkg/cli`, "part 1 / part 2", or differing count snapshots of the same function-length backlog.
   - Use the current lint run as the single source of truth for the current function-length finding count.
   - Search open and recent closed `lint-monster` issues for matching function-length tracking work before creating anything new.
   - Pick one authoritative issue (prefer an existing open issue if it already tracks the same backlog); otherwise create one new consolidated tracking issue.
   - If an authoritative issue already exists, use `update_issue` (passing its `issue_number` explicitly, since this workflow is schedule-triggered and has no triggering issue context) to refresh it with the current count, affected paths, and a checklist of next slices to refactor.
   - For any older open duplicates that cover the same function-length backlog, close them with `close_issue` using a pointer comment to the authoritative issue.
4. For each selected group:
   - Create or update one issue summarizing findings (paths, representative diagnostics, expected outcome).
   - Include a concise remediation checklist using fused skill guidance.
   - For the `function-length refactoring` group, explicitly mention the current authoritative count and list any duplicate issues that were linked/closed.
   - Only assign a Copilot agent when you created a new issue that needs execution work right now; do not create a fresh assignment just to duplicate an already-open tracking issue.
5. If at least one assignment succeeded **or** you updated/closed an existing function-length tracking issue, create one discussion report containing:
   - Daily lint scan summary
   - Group definitions and finding counts
   - Issues created or updated, plus any duplicate issues closed
   - Agent assignments, if any
   - Any groups skipped and why
6. If no assignments were made and no existing tracking issue was updated, call `noop` with a short reason.

## Output rules

- Launch **no more than three** agent sessions total.
- Never assign the same group twice.
- Keep exactly one authoritative open issue for the shared `function-length refactoring` backlog.
- Always use safe outputs for issue creation, issue updates, comments, assignment, and discussion creation.
- Final action must be `create_discussion` when agents were launched or when existing function-length tracking issues were updated/closed; otherwise `noop`.