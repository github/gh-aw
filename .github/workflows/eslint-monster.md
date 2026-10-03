---
private: true
emoji: "🧹"
name: ESLint Monster
description: Daily ESLint factory dispatcher that queues one task for the miner, refiner, or applier
on:
  schedule: daily
  workflow_dispatch:
concurrency:
  group: eslint-factory-dispatcher
  cancel-in-progress: false
permissions:
  contents: read
  issues: read
  pull-requests: read

features:
  gh-aw-detection: true

tracker-id: eslint-monster
model: copilot/gpt-6-astra
engine:
  id: codex
  model-provider: github
strict: true
timeout-minutes: 45
tools:
  cli-proxy: true
  github:
    mode: gh-proxy
    toolsets: [default, issues, pull_requests]
  bash:
    - "*"
steps:
  - name: Run ESLint factory pre-check
    id: eslint_scan
    run: |
      set -euo pipefail
      mkdir -p /tmp/gh-aw/agent
      rm -f /tmp/gh-aw/agent/lint-clean.flag
      cd eslint-factory
      npm ci > /tmp/gh-aw/agent/eslint-factory.log 2>&1

      if npm run lint:setup-js >> /tmp/gh-aw/agent/eslint-factory.log 2>&1; then
        : > /tmp/gh-aw/agent/eslint-diagnostics.txt
        touch /tmp/gh-aw/agent/lint-clean.flag
        exit 0
      fi

      grep -E '^[^:]+:[0-9]+:[0-9]+:' /tmp/gh-aw/agent/eslint-factory.log > /tmp/gh-aw/agent/eslint-diagnostics.txt || true
      diag_count=$(wc -l < /tmp/gh-aw/agent/eslint-diagnostics.txt | tr -d ' ')
      if [ "${diag_count}" -eq 0 ]; then
        echo "ESLint factory pre-check failed without diagnostics" >&2
        exit 1
      fi
safe-outputs:
  create-issue:
    title-prefix: "[eslint-factory] "
    labels: [automation, eslint]
    max: 1
  noop:
imports:
  - shared/reporting.md
evals:
  - id: worker_selected
    question: Did the dispatcher select the appropriate ESLint factory worker using the available evidence?
  - id: work_queued_or_noop
    question: Did the dispatcher queue one non-duplicate task or use noop when a task was already queued?
---

{{#runtime-import? .github/shared-instructions.md}}

# ESLint Factory Dispatcher

You are the daily ESLint factory dispatcher for `actions/setup/js`. GitHub issues labeled
`eslint` with the `[eslint-factory] ` title prefix are the durable work queue. The three worker types are
`miner`, `refiner`, and `applier`; workers poll this queue independently.

## Mission

Choose one task each day:
- If the pre-check found lint diagnostics, queue an `applier` task to remediate them.
- Otherwise, if recent feedback on `eslint-factory` rules identifies a concrete false positive,
  false negative, unsafe fix, or unclear diagnostic, queue a `refiner` task citing that feedback.
- Otherwise, queue a `miner` task to look for one new high-signal rule in `actions/setup/js`.

## Runtime inputs

Read:
- `/tmp/gh-aw/agent/eslint-factory.log`
- `/tmp/gh-aw/agent/eslint-diagnostics.txt`
- `/tmp/gh-aw/agent/lint-clean.flag`

## Required flow

1. Read the diagnostics and check the clean flag. If diagnostics exist, prefer the applier.
2. If clean, review recent rule feedback (last 14 days) before choosing refiner or miner.
3. Search open issues labeled `eslint` with the `[eslint-factory] ` prefix before creating anything. If a
   task for the selected worker is already open, call `noop` rather than enqueueing
   duplicate work. Check today's already completed queue tasks too, so a manual
   rerun does not enqueue the same work twice. Do not create a second task for
   the same findings or feedback.
4. Create exactly one issue titled `[worker] <specific task>` (the configured
   `[eslint-factory] ` prefix is added automatically). Include the worker name,
   affected paths, evidence or representative diagnostics, expected outcome, and
   validation command. For a miner, specify the target paths and rule quality bar.
5. If the queue already contains equivalent work, call `noop` with a reason.

## Constraints

- Keep application scoped to `actions/setup/js` and rule changes to `eslint-factory`.
- Never add the `cookie` label or assign agents from the dispatcher; the workers
  own execution and close their queue item only after successful processing.
- Do not treat an ESLint command failure without diagnostics as a remediation task.