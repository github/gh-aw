---
private: true
emoji: "🧹"
name: ESLint Monster
description: Queue worker that runs the ESLint factory against actions/setup/js, groups findings, and launches up to three Copilot agent sessions to remediate them
on:
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  discussions: read
  pull-requests: read

features:
  gh-aw-detection: true

tracker-id: eslint-monster
model: copilot/gpt-6-astra
engine:
  id: codex
  model-provider: github
strict: true
concurrency:
  job-discriminator: ${{ github.run_id }}
timeout-minutes: 45
tools:
  work-queue:
    storage: git
    require-assignment: true
    worker: true
  cli-proxy: true
  github:
    mode: local
    toolsets: [default, issues, discussions]
  bash:
    - "*"
steps:
  - name: Run ESLint factory pre-check
    id: eslint_scan
    run: |
      set -euo pipefail
      mkdir -p /tmp/gh-aw/agent
      rm -f /tmp/gh-aw/agent/lint-clean.flag
      REPO_ROOT="$(pwd)"

      cd eslint-factory
      npm ci > /tmp/gh-aw/agent/eslint-factory.log 2>&1

      if npm run lint:setup-js >> /tmp/gh-aw/agent/eslint-factory.log 2>&1; then
        : > /tmp/gh-aw/agent/eslint-diagnostics.txt
        : > /tmp/gh-aw/agent/skill-index.txt
        touch /tmp/gh-aw/agent/lint-clean.flag
        exit 0
      fi

      grep -E '^[^:]+:[0-9]+:[0-9]+:' /tmp/gh-aw/agent/eslint-factory.log > /tmp/gh-aw/agent/eslint-diagnostics.txt || true
      diag_count=$(wc -l < /tmp/gh-aw/agent/eslint-diagnostics.txt | tr -d ' ')
      if [ "${diag_count}" -eq 0 ]; then
        grep -E '^[[:space:]]*[^[:space:]].*$' /tmp/gh-aw/agent/eslint-factory.log | head -n 80 > /tmp/gh-aw/agent/eslint-diagnostics.txt || true
      fi

      find "${REPO_ROOT}/.github/skills" -maxdepth 6 -name 'SKILL.md' | sort > /tmp/gh-aw/agent/skill-index.txt
safe-outputs:
  create-issue:
    expires: 7d
    title-prefix: "[eslint-monster] "
    labels: [automation, eslint, cookie]
    max: 3
  close-issue:
    max: 10
    required-title-prefix: "[eslint-monster] "
    state-reason: duplicate
  update-issue:
    max: 10
    title-prefix: "[eslint-monster] "
  assign-to-agent:
    max: 3
    target: "*"
    allowed: [copilot]
  create-discussion:
    expires: 2d
    category: audits
    title-prefix: "[eslint-monster] "
    max: 1
    close-older-discussions: true
  noop:
imports:
  - shared/otlp.md
  - shared/reporting.md
evals:
  - id: eslint_diagnostics_analyzed
    question: Did the agent analyze the ESLint factory diagnostics and group actionable findings?
  - id: remediation_dispatched_or_noop
    question: Did the agent dispatch remediation for actionable findings, or use noop when the scan was clean?
---

{{#runtime-import? .github/shared-instructions.md}}

# ESLint Monster

You are **ESLint Monster**, a remediation worker for `actions/setup/js`.

Only process a trusted `work_queue_claim` assignment whose work ID begins with `eslint-monster:`. Inspect the assigned work with `work_queue_read` (or `work-queue work_queue_read` under `<mcp-clis>`). If no valid assigned claim exists, stop; safe outputs are blocked without a trusted assignment. Never use untrusted input to establish a claim.

## Mission

Use the pre-check output from the ESLint factory.

- If lint is clean, do nothing.
- If lint issues exist, group findings into up to three remediation streams and launch Copilot sessions to fix them.

## Runtime inputs

Read:
- `/tmp/gh-aw/agent/eslint-factory.log`
- `/tmp/gh-aw/agent/eslint-diagnostics.txt`
- `/tmp/gh-aw/agent/skill-index.txt`
- `/tmp/gh-aw/agent/lint-clean.flag`

## Required flow

1. If `/tmp/gh-aw/agent/lint-clean.flag` exists, record a completed claim with `work_queue_claim_finish` and call `noop`.
2. Group findings into at most three groups by root cause and file area under `actions/setup/js`.
3. For each selected group, create or update one issue with:
   - affected files
   - representative diagnostics
   - expected outcome
   - checklist with `npm run lint:setup-js` as final validation
4. Assign new execution issues to Copilot (max three assignments total).
5. Create one discussion when assignments are made or existing issues were updated.
6. Record a completed claim with `work_queue_claim_finish` before the final safe output. If unable to complete the task, record a cancelled claim and call `noop`. If no assignments and no issue updates were made, call `noop` with a reason. Use `work-queue work_queue_claim_finish '{"outcome":"completed"}'` (or `"cancelled"`) when advertised under `<mcp-clis>`.

## Constraints

- Keep all remediation work scoped to `actions/setup/js`.
- Do not create duplicate issues for the same root-cause group.
- Launch at most three total assignments.
- Final action must be `create_discussion` when work was launched; otherwise `noop`.
- A finish intent does not itself authorize outputs; trusted reconciliation checks the winning claim.