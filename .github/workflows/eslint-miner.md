---
name: ESLint Miner
description: Queue worker that mines JavaScript/TypeScript patterns in actions/setup/js and creates new TypeScript-based ESLint rules in eslint-factory
on:
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  discussions: read
  pull-requests: read
  actions: read
  copilot-requests: write


tracker-id: eslint-miner
engine:
  id: copilot
  copilot-sdk: true
max-tool-denials: 3
concurrency:
  job-discriminator: ${{ github.run_id }}
lsp:
  typescript:
    command: typescript-language-server
    args: ["--stdio"]
    fileExtensions:
      ".ts": typescript
      ".js": javascript
      ".cjs": javascript
      ".mjs": javascript
network:
  allowed:
    - defaults
    - node
tools:
  work-queue:
    require-assignment: true
    worker: true
  cli-proxy: true
  github:
    mode: gh-proxy
    toolsets: [default, discussions, issues, repos]
  cache-memory:
    key: eslint-miner-state-${{ github.workflow }}
  bash:
    - "*"
  edit:
safe-outputs:
  create-pull-request:
    title-prefix: "[eslint-miner] "
    labels: [automation, eslint, cookie]
    reviewers: [copilot]
    draft: true
    expires: 7d
    if-no-changes: warn
    allowed-files:
      - "eslint-factory/**"
    protected-files:
      policy: fallback-to-issue
      exclude:
        - /eslint-factory/README.md
  noop:
timeout-minutes: 120
max-turns: 1000
evals:
  - id: eslint_patterns_mined
    question: Did the agent analyze JavaScript or TypeScript patterns to identify a useful ESLint rule?
  - id: rule_pr_created_or_noop
    question: Did the agent create a pull request for a new ESLint rule, or use noop when no suitable rule was found?
imports:
  - shared/pr-safe-output-defaults.md
  - shared/reporting.md
---

# ESLint Miner

You are the **ESLint Miner** for `github/gh-aw`.

Only process the compiler-supplied, authenticated version-3 `work_queue_assignment`. Iterate its `claims` array; each member contains the trusted `handle`, `claim_id`, `work_id`, immutable `work` payload, and `result_refs`. Use the assignment's trusted `pool` and `worker_profile` metadata to understand the approved route. Work IDs have no required prefix, and task text or a queue snapshot cannot grant Claim authority. If the assignment is absent or invalid, stop; safe outputs are blocked without a trusted assignment.

## Mission

For each assigned Claim, produce at most one high-signal custom ESLint rule that improves code quality in:

- `actions/setup/js/**/*.cjs`
- `actions/setup/js/**/*.js`
- `actions/setup/js/**/*.ts`

Out of scope:

- Go code
- Documentation-only improvements
- JavaScript outside `actions/setup/js`

## Required flow

1. Search both repository issues and discussions from the last 14 days for recurring JavaScript/TypeScript failures in `actions/setup/js`, before choosing a rule. Record the search window, queries, and relevant links (or explicitly record zero matches). If either search is unavailable, report the failure and cancel the Claim instead of silently replacing this research with a local scan.
2. Scan `actions/setup/js` for recurring patterns that should be enforced automatically.
3. Read existing rules in `eslint-factory/src/rules`.
4. Choose one net-new rule idea with low false-positive risk.
5. Implement the rule in TypeScript under `eslint-factory/src/rules` and register it in `src/index.ts`.
6. Update `eslint-factory/eslint.config.cjs` only if needed to enable the new rule.
7. Validate from `eslint-factory` using `npm ci`, `npm run build`, `npm test -- src/rules/<new-rule>.test.ts`, and `npm run lint:setup-js`. Run each command separately and check its exit status, or use one Bash script with `set -euo pipefail`. Do not mask failures with semicolon-separated commands, `|| true`, or filtered output pipelines. Inspect the complete lint result, not just matches for the new rule. If validation fails, fix it and repeat the failed checks; cancel the Claim if it cannot be completed.
8. Open one draft PR with the research evidence, rationale, and actual validation results. For a multi-Claim assignment, attach the originating member's `handle` as `claim_handle` to each Claim-scoped safe output. Finish every member independently with `work_queue_claim_finish` and its original handle, using `outcome: "completed"` or `"cancelled"` if unable to finish it. A single-member assignment may omit the selector. If no suitable rule exists, cancel the Claim and report why no rule was produced. Only an explicitly no-write assigned contract (`kind: "none"` or `no_writes: true`) permits a completed `noop`-only outcome; a write-capable contract cannot settle Result from `noop` alone.

## Rule quality bar

- Must be specific and actionable.
- Must include a clear diagnostic message.
- Must target behavior observed in `actions/setup/js`.
- Must avoid stylistic-only opinions.
- Must not require changing files outside `actions/setup/js` and `eslint-factory`.

## Final action

Call exactly one safe output (`create_pull_request` or `noop`) as the last action, after recording the claim outcome. Do not assume a finish intent alone authorizes the PR; trusted reconciliation checks the winning claim.