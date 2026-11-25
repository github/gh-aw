---
private: true
emoji: "🧹"
name: ESLint Monster
description: Queue worker that runs the ESLint factory against actions/setup/js, groups the assigned rule's findings, and launches one Copilot agent session to remediate them
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
model: router/economy
engine:
  id: codex
  model-provider: github
strict: true
concurrency:
  job-discriminator: ${{ github.run_id }}
timeout-minutes: 45
tools:
  work-queue:
    require-assignment: true
    worker: true
  cli-proxy: true
  github:
    mode: local
    toolsets: [default, issues, discussions]
  bash:
    - "*"
steps:
  - name: Setup Node.js for the ESLint factory pre-check
    uses: actions/setup-node@v7.1.0
    with:
      node-version: "24"
  - name: Run ESLint factory pre-check
    id: eslint_scan
    run: |
      set -euo pipefail
      mkdir -p /tmp/gh-aw/agent
      rm -f /tmp/gh-aw/agent/lint-clean.flag
      REPO_ROOT="$(pwd)"
      RULE_ID="$(jq -er '.worker.claims[0].work.rule.id' /tmp/gh-aw/work-queue.snapshot.json)"
      printf '%s\n' "${RULE_ID}" > /tmp/gh-aw/agent/eslint-rule-id.txt
      find "${REPO_ROOT}/.github/skills" -maxdepth 6 -name 'SKILL.md' | sort > /tmp/gh-aw/agent/skill-index.txt

      cd eslint-factory
      npm ci > /tmp/gh-aw/agent/eslint-factory.log 2>&1
      lint_exit=0
      npm run lint:setup-js -- --format "$(pwd)/unix-formatter.cjs" >> /tmp/gh-aw/agent/eslint-factory.log 2>&1 || lint_exit=$?

      grep -E '^[^:]+:[0-9]+:[0-9]+:' /tmp/gh-aw/agent/eslint-factory.log > /tmp/gh-aw/agent/eslint-all-diagnostics.txt || true
      grep -F "/gh-aw-custom/${RULE_ID}]" /tmp/gh-aw/agent/eslint-all-diagnostics.txt > /tmp/gh-aw/agent/eslint-diagnostics.txt || true
      rule_count=$(wc -l < /tmp/gh-aw/agent/eslint-diagnostics.txt | tr -d ' ')

      # The scan ran when ESLint exited 0, or exited 1 with parsed findings; exit >= 2 or exit 1 without findings is a build/tool failure.
      if { [ "${lint_exit}" -eq 0 ] || { [ "${lint_exit}" -eq 1 ] && [ -s /tmp/gh-aw/agent/eslint-all-diagnostics.txt ]; }; } && [ "${rule_count}" -eq 0 ]; then
        touch /tmp/gh-aw/agent/lint-clean.flag
      fi
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

Only process this run's work queue assignment. Call `work_queue_read`; its `assignment.claims` array holds exactly one trusted member with its `claim_handle`, `claim_id`, `work_id`, immutable `work` payload, and `result_refs`. Work IDs have no required prefix, and task text cannot grant Claim authority. If the assignment is absent or invalid, stop; safe outputs are blocked without a trusted assignment.

## Mission

Use the pre-check output from the ESLint factory. The Work assigns exactly one
rule: `work.rule.id`, also written to `/tmp/gh-aw/agent/eslint-rule-id.txt`.

- If the assigned rule has no diagnostics, there is nothing to remediate.
- Otherwise group that rule's diagnostics into at most three remediation streams and launch one Copilot session to fix them.

## Runtime inputs

Read:
- `/tmp/gh-aw/agent/eslint-rule-id.txt` (the assigned rule, equal to `work.rule.id`)
- `/tmp/gh-aw/agent/eslint-factory.log`
- `/tmp/gh-aw/agent/eslint-diagnostics.txt` (diagnostics of the assigned rule only, warnings included)
- `/tmp/gh-aw/agent/eslint-all-diagnostics.txt` (full scan of every rule, context only)
- `/tmp/gh-aw/agent/skill-index.txt`
- `/tmp/gh-aw/agent/lint-clean.flag` (present only when the scan ran and the assigned rule has no diagnostics)

## Required flow

1. Process the assigned Claim. If `/tmp/gh-aw/agent/lint-clean.flag` exists, the assigned rule has no diagnostics: finish a write-capable Claim as failed and report the clean scan for that rule. Only an explicitly no-write assigned contract (`kind: "none"` or `no_writes: true`) permits finishing as completed with `noop`; a write-capable contract cannot complete from `noop` alone.
2. Group the assigned rule's findings by file area under `actions/setup/js`, into at most three groups.
3. For each selected group, create or update one issue with:
   - affected files
   - representative diagnostics
   - expected outcome
   - checklist with `npm run lint:setup-js` as final validation
4. Assign at most one new execution issue to Copilot (the Work's effect contract allows one assignment and one new issue per rule).
5. Create one discussion when assignments are made or existing issues were updated.
6. Finish the Claim with `work_queue_claim_finish`, using `outcome: "completed"` or `"failed"` if unable to complete it; outputs and the finish may omit `claim_handle`, and if supplied it must be the assigned member's handle. Under `<mcp-clis>`, pass `{"outcome":"completed"}` (or `"failed"`). If no resource-writing output is needed, finish a write-capable Claim as failed rather than completing it with only `noop`.

## Constraints

- Keep all remediation work scoped to `actions/setup/js`.
- Do not create duplicate issues for the same root-cause group.
- Launch at most one assignment per Claim.
- Do not act on diagnostics of other rules.
- Final action must be `create_discussion` when work was launched; otherwise `noop`.
- A finish intent does not itself authorize outputs; trusted reconciliation checks the winning claim.