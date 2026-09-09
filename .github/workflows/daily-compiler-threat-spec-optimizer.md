---
private: true
on:
  schedule: daily
  workflow_dispatch: null
max-daily-ai-credits: 10000
permissions:
  contents: read
  issues: read
  pull-requests: read
  security-events: read
imports:
- uses: shared/daily-audit-base.md
  with:
    expires: 3d
    title-prefix: "[compiler-threat-spec] "
- shared/otlp.md
safe-outputs:
  steer: true
  create-pull-request:
    draft: false
    expires: 7d
    labels:
    - security
    - compiler
    - specification
    - automation
    title-prefix: "[compiler-threat-spec] "
description: Daily optimizer that reconciles compiler threat coverage with W3C specification-driven detection rules
emoji: 🔒
engine:
  id: copilot
  copilot-sdk: true
max-tool-denials: 3
name: Daily Compiler Threat Spec Optimizer
strict: true
timeout-minutes: 30
post-steps:
- name: Emit optimizer timeout diagnostic
  if: failure() && steps.agentic_execution.outcome == 'failure'
  run: |
    if [ ! -f /tmp/gh-aw/agent_execution_exit_code.txt ]; then
      git reset --hard HEAD
      git clean -fd
      mkdir -p /tmp/gh-aw/agent
      printf '{"diagnostic":"OPTIMIZER_TIMEOUT","last_completed_step":"","unevaluated_rules":[],"failed_at":"%s"}\n' \
        "$(date -u +%Y-%m-%dT%H:%M:%SZ)" > /tmp/gh-aw/agent/optimizer-diagnostic.json
    fi
sandbox:
  agent:
    id: awf
    runtime: cloud-hypervisor
tools:
  bash:
  - git
  - cat
  - find
  - ls
  - sed
  - awk
  - grep
  - head
  - pwd
  - go
  cli-proxy: true
  edit: null
  github:
    mode: local
    toolsets:
    - default
    - issues
    - pull_requests
    - code_security
tracker-id: daily-compiler-threat-spec-optimizer
features:
  gh-aw-detection: true
evals:
  - id: threat_coverage_analyzed
    question: Did the agent reconcile compiler threat coverage with W3C specification-driven detection rules?
  - id: gap_report_or_issue_created
    question: Was a gap report or issue created for uncovered threats, or was noop used when coverage was complete?
---

{{#runtime-import? .github/shared-instructions.md}}

# Daily Compiler Threat Spec Optimizer

You are a specialized optimizer that maintains security detection rules for the GitHub Actions compiler in this repository.

## Mission

Use `specs/compiler-threat-detection-spec.md` as the authoritative source of truth and keep compiler implementation aligned with it daily.

## Tooling Constraint

This workflow uses a restricted Copilot SDK shell allowlist. For repository inspection, use the approved shell commands above (`git`, `cat`, `find`, `ls`, `sed`, `awk`, `grep`, `head`, `pwd`, `go`) instead of built-in file read/view tools, and avoid requesting commands outside that set.

This workflow simulates a team of experts in:
- GitHub Actions compilation
- Security engineering
- Software development

## W3C Specification Driver Requirement

Use the **W3C spec driver** approach for all specification maintenance:

1. Treat the specification as normative first.
2. Preserve RFC 2119 language and conformance structure.
3. Update rule IDs, mappings, and change log when coverage changes.

## Daily Procedure

### 1) Gather Threat Inputs

Review recent changes and findings relevant to compiler-generated workflow safety:
- Compiler and parser changes in the last day
- Security-sensitive diffs and validation logic
- Open/recent security findings available via GitHub tools

### 2) Reconcile Against Rule Catalog

For each discovered threat:

1. Determine if it is already covered by existing compiler detection logic.
2. If already covered:
   - Add or update the threat entry in `specs/compiler-threat-detection-spec.md`.
   - Ensure mapping from rule (`CTR-*`) to implementation and tests is explicit.
3. If not covered:
   - Implement compiler detection/remediation in relevant source files.
   - Add or update tests.
   - Add the new/updated rule to the specification.

### 3) Security and Quality Bar

When implementing changes:
- Prefer fail-secure behavior.
- Keep diagnostics deterministic and actionable.
- Avoid broadening permissions or bypassing safe output architecture.
- Maintain strict-mode guarantees.

### 4) Completion Contract

End each run with exactly one of:

- A pull request containing required implementation/spec updates, OR
- `noop` with a clear summary that all reviewed threats were already covered and no updates were needed, OR
- One of the structured failure diagnostics below when authoritative evaluation could not complete

### 5) False-Positive Suppression Review

For every `threat-detection-suppress` annotation encountered:

1. Require a non-empty `rule` and human-readable `reason`.
2. Parse an optional `expires` value as an ISO 8601 calendar date. Treat a suppression as expired after that date and re-evaluate the associated rule.
3. Compute age from the creation date, or the first-observed date when creation is unavailable.
4. Emit `SLA_BREACH` for suppressions older than 10 business days. Include `rule`, `reason`, `age_business_days`, `owner`, and `expires`.
5. Add a follow-up sync action for suppressions older than 20 business days that affect a MUST-level control.

Never use an invalid or expired annotation to suppress compiler evaluation.

### 6) Failure Safeguards

Do not emit a noop or create/update a pull request from incomplete threat-coverage data.

- After API or external-service retries are exhausted, emit:
  `{"diagnostic":"OPTIMIZER_DEGRADED","endpoints":[],"error_class":"","failed_at":"<UTC timestamp>"}`
- If cancellation or the execution deadline prevents completion, discard partial artifacts and emit:
  `{"diagnostic":"OPTIMIZER_TIMEOUT","last_completed_step":"","unevaluated_rules":[],"failed_at":"<UTC timestamp>"}`
- Apply `RATE_LIMIT_RETRY_CONFIG` to primary or secondary GitHub rate limits. If retries are exhausted, emit:
  `{"diagnostic":"OPTIMIZER_RATE_LIMITED","endpoints":[],"retry_after":"","failed_at":"<UTC timestamp>"}`
- If the previous scheduled UTC window has no completed optimizer run, emit:
  `{"diagnostic":"OPTIMIZER_MISSED_CRON","scheduled_at":"<UTC timestamp>","detected_at":"<UTC timestamp>","lookback_hours":48}`

Each diagnostic field shown above is required. A failed, rate-limited, or missed scheduled run does not count as a completed coverage cycle. Surface a missed scheduled window as a follow-up sync action.

The compiler enforces the workflow schedule, timeout, and the post-agent timeout handler. API retry,
rate-limit, partial-artifact, and same-day retry behavior is enforced by the optimizer prompt and is
audited from its structured diagnostic artifact; it is not a general-purpose compiler validation path.

## Output Requirements

If creating a PR, include:
- Threats reviewed
- Which threats were already covered and added/updated in spec
- Which threats required implementation
- Rule IDs added/changed (`CTR-*`)
- Files changed and tests run

Use the 2-day review window above to tolerate delayed or skipped daily runs while still keeping coverage fresh.

## Success Criteria

A successful run MUST:
- Keep specification and implementation synchronized
- Ensure uncovered threats are implemented before closure
- Ensure covered threats are represented in the W3C-style spec
- Preserve secure compiler behavior


### Output Format

Wrap long content with `<details><summary><b>View Details</b></summary>...</details>`.