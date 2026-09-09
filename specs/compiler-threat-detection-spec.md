---
title: GitHub Actions Compiler Threat Detection Specification
description: Formal W3C-style specification for compiler detection rules that identify and remediate unsafe generated workflow behavior
sidebar:
  order: 1001
---

# GitHub Actions Compiler Threat Detection Specification

**Version**: 1.0.31
**Status**: Candidate Recommendation  
**Latest Version**: https://github.com/github/gh-aw/blob/main/specs/compiler-threat-detection-spec.md  
**Editors**: GitHub Next (GitHub, Inc.)

---

## Abstract

This specification defines the normative requirements for compiler-side threat detection rules in GitHub Agentic Workflows (gh-aw). The rules detect unsafe or non-compliant patterns in generated GitHub Actions workflows and enforce secure-by-default outcomes before runtime.

This specification is the source of truth for detection rule coverage, implementation obligations, and daily maintenance. Implementations MUST keep compiler behavior and this document synchronized.

## Status of This Document

This is a Candidate Recommendation specification. It may be revised based on operational evidence, threat-model updates, and conformance results.

**Publication Date**: May 26, 2026  
**Governance**: This specification is maintained by the gh-aw maintainers and governed by gh-aw security review processes.

## Table of Contents

1. [Introduction](#1-introduction)
2. [Spec-to-Implementation Sync](#2-spec-to-implementation-sync)
3. [Conformance](#3-conformance)
4. [Threat Detection Rule Model](#4-threat-detection-rule-model)
5. [Normative Rule Requirements](#5-normative-rule-requirements)
6. [Daily Optimizer Maintenance Protocol](#6-daily-optimizer-maintenance-protocol)
7. [Implementation Mapping](#7-implementation-mapping)
8. [Compliance Testing](#8-compliance-testing)
9. [References](#9-references)
10. [Change Log](#10-change-log)

---

## 1. Introduction

### 1.1 Purpose

This specification defines how compiler detection rules are authored, implemented, and maintained to prevent unsafe generated workflow behavior.

### 1.2 Scope

This specification covers:

- Rule definitions for generated-code security threats
- Compiler obligations for detection and remediation
- Daily optimizer behavior for threat coverage review
- Rule-to-implementation mapping and conformance expectations

This specification does NOT cover:

- Runtime threat detection job internals
- External scanner rule ecosystems
- Non-compiler repositories

### 1.3 Design Principles

1. **Specification-first**: Rules MUST be defined in this specification.
2. **Security by default**: Unsafe generated behavior MUST be blocked or remediated.
3. **Bidirectional sync**: Implemented rules MUST appear in spec, and specified rules MUST map to implementation.
4. **Auditable evolution**: Rule additions and changes MUST be traceable.

---

## 2. Spec-to-Implementation Sync

This section anchors the specification version to the minimum gh-aw binary version expected to implement it and to the lock-file behavior that must remain compatible.

| Spec version | Minimum gh-aw binary version | Lock-file compatibility notes |
|--------------|------------------------------|-------------------------------|
| `1.0.31` | `v0.87.9` (or newer) | No new CTR rules; daily optimizer cycle 2026-09-09 re-audits the CTR-001 through CTR-026 mapping table against the sole reachable commit history and reviews open high/critical code-scanning alerts; alert #672 (`go/allocation-size-overflow`, `pkg/workflow/mcp_setup_generator.go:146`) is confirmed as within compiler conformance scope (Section 3.1) but is a non-security-relevant `make()` capacity hint (`len(tools)+1`) bounded by in-process frontmatter tool maps, not attacker-controlled unbounded input; not a new threat class. No `.lock.yml` schema changes. |
| `1.0.30` | `v0.87.9` (or newer) | CTR-001 mapping adds the status-function guard for compiler-owned prerequisites; no new rule or `.lock.yml` schema change. |
| `1.0.29` | `v0.87.9` (or newer) | No new CTR rules; extends the CTR-004 Sandbox Bypass Configuration mapping (Section 7.2 audit) to document that the unified MCP config renderer's duplicate emission of the Playwright Chromium `--no-sandbox` entrypoint flag (`pkg/workflow/mcp_renderer_builtin.go`, commit `ce08eba4b`) is the same browser-process-level flag already covered under the 2026-07-26 audit for `pkg/workflow/mcp_config_playwright_renderer.go`; not a workflow sandbox bypass. No `.lock.yml` schema changes. |
| `1.0.28` | `v0.87.4` (or newer) | No new CTR rules; extends the CTR-004 Sandbox Bypass Configuration mapping to document the enclave GitHub proxy trust-surface additions (`pkg/workflow/enclaves.go`, `pkg/workflow/enclave_github_proxy.go`) that were already implemented and tested (`pkg/workflow/enclaves_test.go`, `pkg/workflow/enclave_github_proxy_test.go`) but absent from the Section 7.1 mapping table; `enclaves[].agent.github.cli: issues-read-v1` requires AWF network isolation (`validateEnclavesConfig` rejects when `isAWFNetworkIsolationEnabled` is false), gates on minimum AWF/MCPG versions, uses a dedicated read-only operation allowlist (`issues.comments.list`, `issues.get`, `issues.list`), and derives its upstream GitHub token from a chain that intentionally excludes the default `GITHUB_TOKEN` (`getEffectiveEnclaveGitHubToken`). No `.lock.yml` schema changes. |
| `1.0.27` | `v0.87.4` (or newer) | No new CTR rules; extends the CTR-006 Template Injection mapping to document `pkg/workflow/mcp_renderer_guard.go`, which uses a sentinel-based defer mechanism (`guardExprSentinel`/`renderGuardPoliciesJSON`) to emit MCP gateway guard-policy expressions as unquoted GitHub Actions runtime expressions in generated heredocs; this behavior was already implemented and tested (`pkg/workflow/template_injection_validation_test.go`, `pkg/workflow/copilot_github_mcp_test.go`) but absent from the Section 7.1 mapping table. No `.lock.yml` schema changes. |
| `1.0.26` | `v0.87.4` (or newer) | Adds CTR-026 Generated Job Timeout Expression Injection, documenting the already-implemented rejection of expression/non-positive-integer `jobs.agent.timeout-minutes` and `jobs.detection.timeout-minutes` values (`extractCustomJobTimeoutMinutes` in `pkg/workflow/compiler_custom_job_properties.go`). `.lock.yml` files now always emit a literal integer job-level `timeout-minutes` for the generated `agent` and `detection` jobs instead of a `${{ vars.GH_AW_DEFAULT_*_TIMEOUT_MINUTES || '<n>' }}` expression; recompilation of existing workflows changes these two lines but does not alter secrets or action-reference manifests (no CTR-016 impact). |
| `1.0.25` | `v0.87.1` (or newer) | No new CTR rules; the CTR-025 mapping is re-confirmed against the merged `fix-threat-detection-system-block-false-positive` changeset (`stripFrameworkSystemBlock`/`SYSTEM_BLOCK_REMOVED_MARKER` in `actions/setup/js/setup_threat_detection.cjs`), which matches the existing Section 7.1 mapping with no drift. No `.lock.yml` schema changes. |
| `1.0.24` | `v0.87.1` (or newer) | No new CTR rules; extends existing rule mappings (CTR-005, CTR-006/CTR-009, CTR-007, CTR-012) to cover recently-hardened implementation sites reviewed in this cycle (safe-output field allowlisting, agent-import-path shell escaping, URL-authority userinfo bypass in the markdown/content sanitizer, and generalized wildcard-target validation). No `.lock.yml` schema changes. |
| `1.0.23` | `v0.87.1` (or newer) | Adds normative coverage for framework self-prompt misattribution handling (CTR-025) in threat-detection setup by stripping only the leading framework-generated `<system>...</system>` block before analysis; no `.lock.yml` schema changes (runtime-only detection setup behavior). |
| `1.0.22` | `v0.87.0` (or newer) | Adds validated `threat-detection-suppress` handling and the `threat_detection_suppressions` manifest field, plus optimizer suppression and failure-safeguard conformance coverage. |
| `1.0.21` | `v0.83.6` (or newer) | Editorial correction: the Deprecation Policy subsection is numbered 5.4 to match its parent section; no lock-file compatibility changes. |
| `1.0.20` | `v0.83.6` (or newer) | Threat-detection behavior must remain compatible with current `.lock.yml` compilation semantics, including manifest drift enforcement (`gh-aw-manifest` checks for CTR-016), update-check validation (`check-for-updates` handling for CTR-018), cache-memory integrity enforcement (`update_cache_memory` gating for CTR-019), conditional import rejection (`imports.if` rejection for CTR-020), `workflow_run` trigger branch scope enforcement (CTR-021), git subprocess argument-injection guards for remote import/download ref and path arguments (CTR-022), and bash command allowlist illusion rejection for engines lacking allowlist enforcement (CTR-023). No `.lock.yml` schema changes are introduced by CTR-022 or CTR-023; both are compile-time-only validations. |
| `1.0.15`–`1.0.19` | `v0.72.1` (or newer) | Adds `workflow_run` trigger branch-scope enforcement (CTR-021); runtime-only `docker-sbx`, credential-refresh, and Playwright changes introduce no `.lock.yml` schema constraint. |
| `1.0.8`–`1.0.14` baseline | `v0.72.1` (or newer) | Establishes manifest drift (CTR-016), update-check (CTR-018), cache-memory integrity (CTR-019), and conditional-import (CTR-020) validation. |
Compact changelog: `1.0.8` introduced CTR-016 and CTR-018; `1.0.10`–`1.0.13` added
CTR-019; `1.0.14` added CTR-020; `1.0.15` added CTR-021; `1.0.20` added CTR-022
and CTR-023; `1.0.21` corrects the Deprecation Policy subsection numbering; `1.0.23`
adds CTR-025; `1.0.26` adds CTR-026; and `1.0.27` extends the CTR-006 mapping
with `mcp_renderer_guard.go`. Versions with no distinct lock-file impact are grouped above.

When this specification version changes, maintainers MUST update this table in the same pull request as any lock-file compatibility changes.

---

## 3. Conformance

An implementation conforms to this specification if it satisfies all MUST requirements in Sections 4-8.

### 3.1 Conformance Targets

- Compiler source in `pkg/workflow/`
- Related schema/validation sources in `pkg/parser/` and `actions/setup/` where applicable
- Daily optimizer workflow that enforces ongoing coverage

### 3.2 Requirement Keywords

The key words **MUST**, **MUST NOT**, **SHALL**, **SHOULD**, and **MAY** are to be interpreted as described in RFC 2119.

---

## 4. Threat Detection Rule Model

Each rule SHALL be represented with:

- **Rule ID** (e.g., `CTR-001`)
- **Threat Class** (permissions, sandbox, network, integrity, output safety)
- **Detection Condition**
- **Compiler Action** (reject, rewrite, warn)
- **Evidence** (error code/message and affected source location)
- **Implementation Mapping** (file/function reference)

Rule definitions SHOULD remain implementation-agnostic while preserving testability.

---

## 5. Normative Rule Requirements

### 5.1 Core Rule Catalog

A conforming implementation MUST include detection coverage for at least the following rules:

- **CTR-001 Privilege Escalation**: Detect generated jobs with unauthorized write permissions.
- **CTR-002 Unpinned Action Integrity**: Detect unpinned or weakly pinned action references in strict contexts.
- **CTR-003 Unsafe Tool Scope Expansion**: Detect wildcard or overbroad tool permissions that violate policy.
- **CTR-004 Sandbox Bypass Configuration**: Detect generated configurations that disable required sandboxing.
- **CTR-005 Unsafe Output Route**: Detect direct unsafe write paths that bypass safe-output controls.
- **CTR-006 Template Injection**: Detect GitHub Actions expressions used directly in `run:` shell commands where user-controlled data flows into shell execution context without environment variable indirection.
- **CTR-007 Markdown Content Security**: Detect dangerous or malicious content patterns in externally-sourced markdown workflow files, including unicode abuse, hidden content, obfuscated links, HTML abuse, embedded scripts, and social engineering.
- **CTR-008 Pull Request Target Safety**: Detect unsafe use of the `pull_request_target` trigger, which runs workflows with write permissions and secret access; enforce checkout restrictions to prevent pwn-request attacks.
- **CTR-009 Shell Expansion in Safe-Outputs**: Detect dangerous bash expansion patterns (`${var@op}`, `${!var}`, `$(...)`, backtick substitution) in safe-outputs `run:` scripts that would be blocked by the safe-outputs security harness at runtime.
- **CTR-010 Expression Safety Allowlist**: Enforce an allowlist of approved GitHub Actions expressions; reject unauthorized or multi-line expressions that could enable injection or exfiltration.
- **CTR-011 Network Firewall Configuration**: Validate network firewall configuration dependencies and domain patterns; reject configurations that declare firewall rules without required prerequisites (e.g., `allow-urls` without `ssl-bump`); reject wildcard `*` domains in strict mode.
- **CTR-012 Safe-Outputs Wildcard Push Scope**: Detect misconfiguration patterns when `safe-outputs.push-to-pull-request-branch: target: "*"` is used; warn when no wildcard fetch pattern is present in checkout (suppressed for public repos) and when no access constraints (`title-prefix` or `labels`) are configured.
- **CTR-013 Argument Injection via Package/Image Names**: Detect package or container image names that start with `-` (hyphen) in npm/npx, pip/uv, and Docker frontmatter configurations; reject these names before they are passed to `exec.Command` calls where they would be interpreted as CLI flags, enabling argument injection.
- **CTR-014 Supply Chain Attack via Install Scripts**: Detect when `run-install-scripts: true` is configured under `runtimes.node` in workflow frontmatter; warn in non-strict mode and reject in strict mode to protect against malicious npm pre/post install hooks that can exfiltrate secrets or corrupt the runner environment.
- **CTR-015 Allowed Label Glob Scope**: Detect bare `*` wildcard patterns in `safe-outputs.*.allowed-labels` fields (`create-issue`, `create-discussion`, `update-discussion`, `create-pull-request`, `merge-pull-request`); reject compilation when such a pattern is present because it renders the label restriction ineffective and may allow the agent to apply labels that trigger unintended label-driven automation in the repository.
- **CTR-016 Compile-Time Manifest Drift**: Detect when recompilation of an existing workflow would introduce new secrets or unapproved action references beyond what was previously approved in the lock file manifest; reject compilation when new restricted secrets or previously-absent action references appear, preventing adversarial workflow sources or prompt-injection from silently expanding the workflow's trust surface during routine updates.
- **CTR-017 Secret Leakage via Environment Variables**: Detect secrets expressions (`${{ secrets.* }}`) in the top-level `env:` section, in `engine.env` (excluding allowed engine-required vars), and in custom step fields (`pre-steps`, `steps`, `pre-agent-steps`, `post-steps`) outside controlled `env:` bindings and `with:` inputs for `uses:` action steps; these placements expose secrets to the agent container environment. Warn in non-strict mode; reject in strict mode.
- **CTR-018 Version Integrity Bypass**: Detect `check-for-updates: false` in workflow frontmatter, which disables the compile-agentic version update check that ensures the workflow was compiled with a supported version of gh-aw. Warn in non-strict mode; reject in strict mode.
- **CTR-019 Cache-Memory Integrity Enforcement**: Enforce that `update_cache_memory` job only runs when threat detection succeeds (not when skipped or failed); prevents cache pollution when agent outputs have not been validated or when detection was bypassed, ensuring cache-memory data integrity and preventing persistence of potentially malicious content from unvalidated agent sessions.
- **CTR-020 Conditional Import Security**: Detect and reject `imports:` entries that contain an `if` field; conditional imports can alter workflow setup and security posture at runtime by making security-sensitive import decisions dependent on runtime conditions; reject compilation and direct authors to use `{{#if ...}}{{#runtime-import? ...}}{{/if}}` for experiment-specific prompt imports.
- **CTR-021 Workflow Run Trigger Branch Scope**: Detect `workflow_run` triggers that lack `branches:` restrictions; unrestricted `workflow_run` triggers fire on any branch including attacker-controlled branches, which can expose workflow context data via `github.event.workflow_run.*` and enable unintended cross-workflow trigger chains; warn in non-strict mode and reject in strict mode. Also enforce that `workflow_run` must include a non-empty `workflows:` field (hard error in both modes).
- **CTR-022 Git Subprocess Argument Injection**: Detect and reject `ref` or `path` values derived from workflow import specs or remote-download configuration that begin with `-` (hyphen), contain NUL bytes, contain `..` path-traversal segments, or (for paths) are absolute, before those values are passed as positional arguments to a `git` subprocess (e.g., `git archive --remote=<repo> <ref> <path>`, `git ls-remote`, `git show`). A value beginning with `-` would otherwise be parsed by `git` as a CLI option rather than a literal ref/path, enabling argument injection (CWE-88) against the invoking process. Reject before subprocess invocation in all modes (compile-time, non-strict and strict); this is a hard security boundary, not a strict-mode-only enhancement.
- **CTR-023 Bash Command Allowlist Illusion**: Detect when workflow frontmatter declares an explicit `tools.bash` restriction (`bash: false`, `bash: []`, or a non-wildcard command list such as `bash: ["git", "npm"]`) for a coding agent engine that does not enforce a bash command allowlist. Some engines silently ignore restricted `tools.bash` configurations at runtime, creating the dangerous illusion that bash execution is restricted when in fact all commands remain permitted. Reject compilation with an error identifying the unsupported engine and the ignored restriction; unrestricted (`bash: true` or absent) and wildcard (`bash: ["*"]`, `bash: [":*"]`) configurations are unaffected because they do not depend on engine-side enforcement.
- **CTR-025 Framework Self-Prompt Misattribution**: Detect and neutralize threat-detection false positives caused by gh-aw framework-generated prompt scaffolding by stripping only a leading `<system>...</system>` block from the analyzed prompt artifact before threat analysis. The removal MUST be position-bound to the first block only; `<system>`-lookalike markup that appears later in prompt content MUST remain intact so attacker-supplied injections are still visible to analysis.
- **CTR-026 Generated Job Timeout Expression Injection**: Detect and reject non-positive-integer or expression values (including `${{ ... }}` GitHub Actions expressions, zero, negative numbers, non-integral floats, and out-of-range values) for `jobs.agent.timeout-minutes` and `jobs.detection.timeout-minutes`. Because these generated jobs' step budgets (including setup and teardown) are computed at compile time from the resolved literal, an expression or non-positive value would defer resolution to workflow-run time or produce an unbounded/invalid job budget, undermining the job-level timeout guarantee that prevents a hung or malicious setup/teardown step from consuming runner capacity indefinitely. Reject compilation identifying the field and the required positive-integer-literal form in all modes; this is a hard compile-time boundary, not a strict-mode-only enhancement.

### 5.2 Compiler Response Requirements

For each triggered rule, the compiler MUST:

1. Produce deterministic diagnostics.
2. Prevent insecure generation by failing compilation OR applying a safe rewrite.
3. Emit actionable remediation guidance.
4. Include stable identifiers so tests can assert rule behavior.

### 5.3 Rule Lifecycle Requirements

When a new threat class is identified:

- If implementation already covers the threat, the threat MUST be added to this specification with mapping and tests.
- If implementation does not cover the threat, detection/remediation MUST be implemented and then added to this specification.

#### 5.4 Deprecation Policy

When a compiler feature that a `CTR-*` rule depends on is removed, the rule MUST be formally retired:

- The rule's status MUST be updated to `Deprecated` in this specification in the same change set as the implementation removal.
- The rule catalog entry MUST be retained (not deleted) with a deprecation notice indicating the version in which the rule was retired and the reason.
- All test IDs mapped to the deprecated rule in Section 7 MUST be marked as `[DEPRECATED]` and MUST NOT be required for conformance after the deprecation version.
- The implementation mapping in Section 7.1 for the deprecated rule MUST be cleared; the row MUST remain in the table annotated with `[Deprecated in vX.Y.Z]`. This is enforced mechanically by `TestFormal_DeprecationPolicy_SpecArtifactsConform` in `pkg/workflow/threat_detection_deprecation_policy_formal_test.go`, which fails if a deprecated rule's Section 7.1 implementation cell is non-empty.
- A change-log entry MUST document the deprecation with the rule ID, deprecation version, and rationale.

---

## 6. Daily Optimizer Maintenance Protocol

A daily optimizer process MUST execute threat coverage reconciliation.

### 6.1 Daily Inputs

The optimizer MUST inspect at least:

- Recent compiler changes (`pkg/workflow/**/*.go`)
- Related validation/security code paths
- Open and recent security findings (issues, PRs, and code scanning context where available)
- Current rule catalog in this specification

### 6.2 Daily Decision Procedure

For each discovered or candidate threat:

1. Determine whether an implemented compiler rule already covers the threat.
2. If covered, update the specification (rule catalog/mapping/tests references).
3. If uncovered, implement detection/remediation in compiler code and tests, then update the specification.

### 6.3 Daily Output Requirements

The optimizer MUST produce one of:

- A pull request containing required spec and/or implementation updates, or
- A noop report explicitly stating no new threat coverage actions were required

### 6.4 False-Positive Handling

False positives occur when a CTR rule triggers on a workflow input that is not actually unsafe. This section defines normative norms for suppressing, auditing, and resolving false-positive detections.

1. **Author suppression mechanism**: When a workflow author believes a compiler diagnostic is a false positive, they **MUST** add an inline suppression annotation in the workflow frontmatter using the `threat-detection-suppress` key. The value **MUST** be a list of objects, each with a `rule` field (the `CTR-*` identifier), a `reason` field (human-readable explanation of why the flagged pattern is safe in this context), and an optional `expires` field (ISO 8601 date after which the suppression is no longer valid). A suppression without a `reason` **MUST NOT** be accepted by the compiler; the compiler **MUST** emit a validation error if `reason` is absent or empty. (T-CTR-024)

2. **Audit trail requirement**: Every active suppression annotation **MUST** be recorded in the compiled lock file (`.lock.yml`) manifest section so that reviewers can audit which rules are suppressed and why. The lock file **MUST** include the full `rule`, `reason`, and `expires` values for each suppression. Suppressions absent from the lock file manifest **MUST** be treated by subsequent compilations as unapproved and re-evaluated against the current CTR rule. (T-CTR-025)

3. **SLA for resolution**: Suppressions marked as false positives that affect a `MUST`-level security control (as defined in Section 5.1 — specifically those rules whose compiler action is `reject` in non-strict mode) **SHOULD** be resolved within **10 business days** — either by confirming the suppression is correct and updating the rule's detection logic to eliminate the false positive, or by removing the suppression when the workflow is corrected. (T-CTR-026)
4. **SLA enforcement in daily optimizer output**: The daily optimizer **MUST** compute suppression age from suppression creation date (or first-observed date when unavailable). Suppressions older than 10 business days **MUST** be emitted in daily output as `SLA_BREACH` entries that include `rule`, `reason`, `age_business_days`, `owner`, and `expires`. (T-CTR-027)
5. **Escalation requirement**: Suppressions older than 20 business days for `MUST`-level controls **MUST** create a follow-up sync action in the same daily output (for example, PR task, issue task, or explicit policy exception record) so unresolved suppressions cannot remain silent. (T-CTR-028)
6. **Expiration handling**: A suppression **MUST** be re-evaluated and explicitly renewed if the `expires` date passes; expired suppressions **MUST** be treated by the compiler as if they do not exist. (T-CTR-029)

### 6.5 Threat Category Lifecycle

New threat categories do not immediately become normative rules. This section defines the lifecycle stages a threat category **MUST** pass through before it is added to the CTR rule catalog in Section 5.1.

1. **Experimental stage**: A threat class is identified (via security research, incident analysis, or operational observation) and a tracking issue is opened in `github/gh-aw`. An experimental prototype detection implementation **MAY** be added to the compiler behind a feature flag. The threat class **MUST NOT** appear in the normative CTR catalog while in Experimental stage; it **SHOULD** be documented in a separate scratchpad or issue thread. Experimental detections **MUST NOT** cause compilation failures in production.

2. **Candidate stage**: The threat class has a concrete detection trigger, an agreed compiler action (reject, rewrite, or warn), a stable diagnostic ID reserved in a draft spec update, and at least one test case demonstrating the detection. A Candidate threat **SHOULD** be deployed behind a feature flag for a minimum of one release cycle. During Candidate stage, maintainers **MUST** collect evidence (false-positive reports, affected workflow patterns) and document findings in the tracking issue. A Candidate threat **SHOULD NOT** be promoted to Normative without at least one successful deployment in a non-strict production workflow.

3. **Normative stage**: The threat class is formally added to Section 5.1 and Section 8.1 via a pull request that includes: the CTR rule definition, the implementation mapping in Section 7.1, at least one test ID in Section 8.1, and a change-log entry in Section 10. The pull request **MUST** be reviewed by at least one security-focused maintainer. Once merged, the rule **MUST** be enforced by all conforming implementations. Any feature flag used during Candidate stage **MUST** be removed in the same pull request that adds the Normative definition.

### 6.6 Optimizer Failure Safeguards

The daily optimizer process (Section 6) is itself subject to failure. This section specifies normative behavior for the four principal failure modes: API unavailability, runner timeout, rate-limit or quota exhaustion, and a missed scheduled run. These safeguards mirror the pattern established in the AWF Config Canonical Sources Specification §7.

**Failure Mode 1 — API Unavailability**

When the GitHub API or any external service required by the optimizer (for example, code-scanning results, issue search, or PR listing endpoints) is unavailable during the threat-coverage check:

1. The optimizer **MUST** not emit false noop reports. When authoritative data cannot be retrieved, the optimizer **MUST** emit an `OPTIMIZER_DEGRADED` diagnostic entry in its daily output that records the failing endpoint(s), the HTTP status or error class, and the UTC timestamp of the failure. (T-CTR-030)
2. The optimizer **MUST NOT** open a pull request or update spec artifacts based on incomplete threat-coverage data obtained during a degraded API run. (T-CTR-031)
3. The optimizer **SHOULD** retry failed API calls with an exponential back-off policy (initial delay: 10 seconds; maximum delay: 5 minutes; maximum attempts: 3) before declaring the run degraded. (T-CTR-032)
4. After exhausting that policy, the optimizer **MUST NOT** retry the same request against a different degraded endpoint; it **MUST** declare the run degraded unless a configured, independently authoritative endpoint is available.

**Failure Mode 2 — Runner Timeout**

When the optimizer job is cancelled or exceeds its allotted execution time before completing the threat-coverage check:

1. The optimizer job **MUST** emit a structured `OPTIMIZER_TIMEOUT` output entry before termination, recording the last completed step and the set of CTR rules that had not yet been evaluated at the time of cancellation. (T-CTR-033)
2. The optimizer **MUST NOT** produce a partial noop report or a partial PR when the timeout occurs mid-evaluation; any in-progress artifacts **MUST** be discarded. (T-CTR-034)
3. The optimizer workflow **SHOULD** be configured with an explicit `timeout-minutes` value and **SHOULD** schedule a follow-up retry run within the same calendar day when a timeout is detected. (T-CTR-035)

**Failure Mode 3 — Rate-Limit or Quota Exhaustion**

When the GitHub API returns secondary rate-limit (`403` with `Retry-After` header) or primary rate-limit (`429`) responses during the optimization run:

1. The optimizer **MUST** apply the `RATE_LIMIT_RETRY_CONFIG` retry policy (as defined in `actions/setup/js/error_recovery.cjs`) before emitting a terminal failure. (T-CTR-036)
2. If all retries are exhausted and the rate limit is not recovered, the optimizer **MUST** emit an `OPTIMIZER_RATE_LIMITED` diagnostic entry recording the affected endpoints and the `Retry-After` or `x-ratelimit-reset` value. (T-CTR-037)
3. The optimizer **MUST NOT** count a rate-limited run as a completed threat-coverage cycle; the run **MUST** be retried in the next scheduled window. (T-CTR-038)

**Failure Mode 4 — Missed Scheduled Run**

When the daily optimizer job itself does not run during its scheduled UTC window, the next successful optimizer invocation or external scheduler audit **MUST** emit an `OPTIMIZER_MISSED_CRON` diagnostic entry recording the missed `scheduled_at` window, the `detected_at` timestamp, and the lookback horizon used to detect the gap. A missed scheduled window **MUST NOT** count as a completed threat-coverage cycle and **SHOULD** be surfaced as a follow-up sync action. (T-CTR-040)

---

## 7. Implementation Mapping

This specification maps primarily to:

- `pkg/workflow/` (compiler and validation logic)
- `pkg/parser/` (schema and frontmatter validation where relevant)
- `actions/setup/js/` (runtime validation helpers where required by rule semantics)

Implementations MUST maintain a clear mapping from each active `CTR-*` rule to concrete source locations and test coverage.

### 7.1 Baseline Rule Mapping

| Rule ID | Primary Implementation Areas | Test Coverage Targets |
|---------|-------------------------------|-----------------------|
| CTR-001 Privilege Escalation | `pkg/workflow/*permissions*validation*.go`, `pkg/workflow/strict_mode_permissions_validation.go`, `pkg/workflow/github_app_permissions_validation.go`, `pkg/workflow/compiler_builtin_job_augmentation.go` (status-function guard for compiler-owned prerequisites) | `pkg/workflow/*permissions*_test.go`, `pkg/workflow/*dangerous_permissions*_test.go`, `pkg/workflow/compiler_custom_jobs_test.go` |
| CTR-002 Unpinned Action Integrity | `pkg/workflow/*action*.go`, `pkg/workflow/strict_mode_validation*.go` | `pkg/workflow/*action*_test.go`, `pkg/workflow/*strict_mode*_test.go` |
| CTR-003 Unsafe Tool Scope Expansion | `pkg/workflow/tools_validation*.go`, `pkg/workflow/strict_mode_validation*.go` | `pkg/workflow/*tools*_test.go` |
| CTR-004 Sandbox Bypass Configuration | `pkg/workflow/sandbox_validation*.go`, `pkg/workflow/strict_mode_sandbox_validation*.go`, `pkg/workflow/strict_mode_permissions_validation.go`; enclave GitHub proxy network-isolation and version gating in `pkg/workflow/enclaves.go` (`validateEnclavesConfig`, `validateEnclaveGitHubIssuesVersions`) and dedicated read-only credential/operation scoping in `pkg/workflow/enclave_github_proxy.go` (`operationsForEnclaveGitHubProfile`, `getEffectiveEnclaveGitHubToken`) | `pkg/workflow/*sandbox*_test.go`, `pkg/workflow/enclaves_test.go`, `pkg/workflow/enclave_github_proxy_test.go` |
| CTR-005 Unsafe Output Route | `pkg/workflow/compiler_safe_outputs*.go`, `pkg/workflow/safe_outputs*.go`; runtime harness field allowlisting in `actions/setup/js/safe_output_type_validator.cjs` (declared-field enforcement) and patch/manifest differential-parsing hardening in `actions/setup/js/patch_path_helpers.cjs`, `actions/setup/js/manifest_file_helpers.cjs` (patch-parser vs. `git am` protected-file bypass defense) | `pkg/workflow/*safe_outputs*_test.go`, `actions/setup/js/safe_output_type_validator.test.cjs`, `actions/setup/js/patch_path_helpers.test.cjs`, `actions/setup/js/manifest_file_helpers.test.cjs` |
| CTR-006 Template Injection | `pkg/workflow/template_injection_validation.go`, `pkg/workflow/heredoc_validation.go`, `pkg/workflow/mcp_renderer_guard.go` (MCP gateway guard-policy sentinel-based expression deferral, emitting unquoted GitHub Actions runtime expressions in generated heredocs) | `pkg/workflow/template_injection_validation_test.go`, `pkg/workflow/template_injection_validation_fuzz_test.go`, `pkg/workflow/copilot_github_mcp_test.go` |
| CTR-007 Markdown Content Security | `pkg/workflow/markdown_security_scanner.go`; URL-authority allowlist parity between the stripping and filtering passes (userinfo-prefix bypass, backslash-separator normalization, embedded-whitespace discard) in `actions/setup/js/sanitize_content_core.cjs` | `pkg/workflow/markdown_security_scanner_test.go`, `pkg/workflow/secure_markdown_rendering_test.go`, `actions/setup/js/sanitize_content.test.cjs` |
| CTR-008 Pull Request Target Safety | `pkg/workflow/pull_request_target_validation.go` | `pkg/workflow/pull_request_target_validation_test.go` |
| CTR-009 Shell Expansion in Safe-Outputs | `pkg/workflow/safe_outputs_steps_shell_expansion_validation.go`; agent-import-path allowlist regex and consistent argument escaping in engine command generation (`pkg/workflow/agent_validation.go` path-character allowlist, `pkg/workflow/shell.go` `shellEscapeArg`/`shellJoinArgs`, `pkg/workflow/engine_helpers.go`) | `pkg/workflow/safe_outputs_steps_shell_expansion_validation_test.go`, `pkg/workflow/engine_agent_import_test.go`, `pkg/workflow/inline_imports_test.go` |
| CTR-010 Expression Safety Allowlist | `pkg/workflow/expression_safety_validation.go`, `pkg/workflow/expression_syntax_validation.go`, `pkg/workflow/runtime_import_validation.go` (`validateRuntimeImportFiles`) | `pkg/workflow/expression_extraction_test.go` |
| CTR-011 Network Firewall Configuration | `pkg/workflow/network_firewall_validation.go`, `pkg/workflow/firewall_validation.go`, `pkg/workflow/strict_mode_network_validation.go` | `pkg/workflow/network_firewall_validation_test.go` |
| CTR-012 Safe-Outputs Wildcard Push Scope | `pkg/workflow/push_to_pull_request_branch_validation.go`; generalized wildcard-target validation across other safe-output tools so a missing target identifier fails immediately with a tool-specific compiler error rather than deferring to apply time (`pkg/workflow/safe_outputs_tools_generation.go`, `pkg/workflow/safe_outputs_tools_repo_params.go`) | `pkg/workflow/push_to_pull_request_branch_test.go`, `pkg/workflow/push_to_pull_request_branch_warning_test.go` |
| CTR-013 Argument Injection via Package/Image Names | `pkg/workflow/name_validation.go` (shared helper `rejectHyphenPrefixPackages`), `pkg/workflow/npm_validation.go`, `pkg/workflow/pip_validation.go`, `pkg/workflow/docker_validation.go` | `pkg/workflow/argument_injection_test.go` |
| CTR-014 Supply Chain Attack via Install Scripts | `pkg/workflow/run_install_scripts_validation.go` (`validateRunInstallScripts`, `resolveRunInstallScripts`) | `pkg/workflow/run_install_scripts_validation_test.go` |
| CTR-015 Allowed Label Glob Scope | `pkg/workflow/safe_outputs_allowed_labels_validation.go` (`validateSafeOutputsAllowedLabelsGlobScope`) | `pkg/workflow/safe_outputs_allowed_labels_validation_test.go` |
| CTR-016 Compile-Time Manifest Drift | `pkg/workflow/safe_update_enforcement.go` (`EnforceSafeUpdate`, `collectSecretViolations`, `collectActionViolations`, `collectRedirectViolations`), called from `pkg/workflow/compiler.go` | `pkg/workflow/safe_update_enforcement_test.go` |
| CTR-017 Secret Leakage via Environment Variables | `pkg/workflow/strict_mode_env_validation.go` (`validateEnvSecrets`, `validateEnvSecretsSection`), `pkg/workflow/strict_mode_steps_validation.go` (`validateStepsSecrets`, `validateStepsSectionSecrets`) | `pkg/workflow/env_secrets_validation_test.go`, `pkg/workflow/jobs_secrets_validation_test.go` |
| CTR-018 Version Integrity Bypass | `pkg/workflow/strict_mode_update_check_validation.go` (`validateUpdateCheck`) | `pkg/workflow/strict_mode_update_check_validation_test.go` |
| CTR-019 Cache-Memory Integrity Enforcement | `pkg/workflow/cache.go` (`buildUpdateCacheMemoryJob` using `buildDetectionSuccessCondition`), `pkg/workflow/expression_builder.go` (`buildDetectionSuccessCondition`) | `pkg/workflow/cache_memory_threat_detection_test.go`, `pkg/workflow/threat_detection_job_combinations_integration_test.go` |
| CTR-020 Conditional Import Security | `pkg/parser/import_bfs.go` (`parseImportSpecsFromArray`: rejects import items that contain an `if` field) | `pkg/parser/import_bfs_test.go` (`TestParseImportSpecsFromArray_RejectsIfField`) |
| CTR-021 Workflow Run Trigger Branch Scope | `pkg/workflow/agent_validation.go` (`validateWorkflowRunBranches`, `validateWorkflowRunHasWorkflows`, `emitWorkflowRunMissingBranches`) | `pkg/workflow/workflow_run_validation_test.go` (`TestWorkflowRunBranchValidation`, `TestWorkflowRunBranchValidationEdgeCases`) |
| CTR-022 Git Subprocess Argument Injection | `pkg/gitutil/gitutil.go` (`ValidateGitRef`, `ValidateGitPath`), called from `pkg/cli/download_workflow.go` (`downloadWorkflowContentViaGit`), `pkg/parser/remote_resolve_sha.go`, `pkg/parser/remote_workflow_spec.go`, `pkg/parser/remote_download_file.go`, `pkg/parser/import_remote.go` | `pkg/gitutil/gitutil_test.go` (`TestValidateGitRef`, `TestValidateGitPath`), `pkg/cli/download_workflow_test.go` |
| CTR-023 Bash Command Allowlist Illusion | `pkg/workflow/agent_validation.go` (`validateBashCommandAllowlistSupport`, `hasBashExplicitRestriction`), `pkg/workflow/agentic_engine.go` (`EngineCapabilities.BashCommandAllowlist`), per-engine capability declarations in `pkg/workflow/*_engine.go` | `pkg/workflow/bash_command_allowlist_validation_test.go` (`TestValidateBashCommandAllowlistSupport`, `TestEngineBashCommandAllowlistCapability`) |
| CTR-025 Framework Self-Prompt Misattribution | `actions/setup/js/setup_threat_detection.cjs` (`stripFrameworkSystemBlock`, `SYSTEM_BLOCK_REMOVED_MARKER`) | `actions/setup/js/setup_threat_detection.test.cjs` (`removes the leading framework system block from the analyzed prompt file`, `removes only the first system block and preserves later lookalikes`) |
| CTR-026 Generated Job Timeout Expression Injection | `pkg/workflow/compiler_custom_job_properties.go` (`extractCustomJobTimeoutMinutes`, rejects expressions/non-positive values for the `agent` and `detection` generated jobs), `pkg/workflow/job_timeouts.go` (`resolveAgentJobTimeoutValue`, `resolveDetectionJobTimeoutValue`, `builtinJobTimeoutOverride`) | `pkg/workflow/compiler_custom_jobs_test.go` (`TestApplyBuiltinJobAugmentations_RejectsTimeoutMinutesExpression`, `TestExtractCustomJobTimeoutMinutes`, `TestBuildCustomJob_InvalidTimeoutMinutesError`), `pkg/workflow/job_timeouts_test.go` (`TestResolveAgentJobTimeoutValue`, `TestResolveDetectionJobTimeoutValue`, `TestGeneratedAgentJobTimeoutMinutes`) |

The mappings above are pattern-based references and MUST be validated against concrete file paths whenever this specification is updated.

When mappings change, this table MUST be updated in the same change set as the implementation update.

### 7.2 Mapping Audit (2026-09-09)

Audit result: ✅ all listed `CTR-001` through `CTR-026` rows currently include non-empty implementation references and non-empty test coverage targets; no `TODO` placeholders were found in the mapping table. Review window: daily optimizer cycle 2026-09-09, covering commits merged in the prior 48 hours (repository history reachable in this sandbox begins at `bce650c`, the sole reachable commit; no additional compiler or parser diffs were available to review beyond the existing baseline). Security-relevant items evaluated: (1) Reviewed open code-scanning alerts (severity critical/high) via GitHub MCP: no open `critical`-severity alerts. One open `high`-severity alert, #672 (`go/allocation-size-overflow`), is located in `pkg/workflow/mcp_setup_generator.go:146` (`updated := make(map[string]any, len(tools)+1)` inside `toolsWithEnclaveGitHubIssues`), which is within this specification's conformance targets (Section 3.1: `pkg/workflow/`). Verified the flagged capacity hint is computed from `len(tools)`, the in-process `tools` frontmatter map parsed from the workflow's own YAML/JSON frontmatter (already size-bounded by the compiler's frontmatter parsing and schema validation prior to this call), not from unbounded or externally-controlled runtime input; the `+1` addend cannot cause a practical `int` overflow given realistic tool-map sizes, and even a theoretical overflow would only affect a `make` capacity hint (denial-of-service via panic at worst), not memory-safety-relevant or trust-boundary-crossing behavior. This is a code-quality/robustness finding, not a compiler-generated-workflow threat class; no new `CTR-*` rule required. The alert's four other open high-severity siblings (#674/#669/#668/#667, `go/bad-redirect-check`, in `pkg/cli/add_package_manifest_imports.go`, `pkg/cli/add_package_manifest_includes.go`, `pkg/cli/project_command.go`) and #663 (`js/http-to-file-access`, `scripts/ensure-docs-slide-pdf.js`) remain outside conformance targets as previously noted in the 2026-09-06 audit. (2) No `threat-detection-suppress` annotations were found in any live (non-fixture, non-documentation-example) workflow source in this review window, so no `SLA_BREACH` or expiration findings apply. No new `CTR-*` rule required this cycle.

### 7.2 Mapping Audit (2026-09-06)

Audit result: ✅ all listed `CTR-001` through `CTR-026` rows currently include non-empty implementation references and non-empty test coverage targets; no `TODO` placeholders were found in the mapping table. Review window: daily optimizer cycle 2026-09-06, covering commits merged in the prior 48 hours (repository history begins at `8fb0a67`, the sole reachable commit; no additional compiler or parser diffs were available to review beyond the existing baseline). Security-relevant items evaluated: (1) **Spec-to-implementation sync gap**: the specification header and Section 2 sync table were previously bumped to version `1.0.30` (documenting the CTR-001 status-function guard for compiler-owned prerequisites, `guardIfAgainstStatusFuncBypass`/`ifExpressionContainsStatusFunc` in `pkg/workflow/compiler_builtin_job_augmentation.go`), but this Section 7.2 mapping audit and the Section 10 change log were not updated to match, violating the Section 7.3 sync protocol. Verified the guard is implemented and covered by `TestApplyBuiltinJobNeedsAugmentations_StatusFuncAddsSuccessGuards`, `TestApplyBuiltinJobNeedsAugmentations_StatusFuncFailureAddsSuccessGuards`, `TestApplyBuiltinJobNeedsAugmentations_StatusFuncAlwaysAddsSuccessGuards`, and `TestApplyBuiltinJobNeedsAugmentations_StatusFuncKeepsCustomJobUnguarded` (`pkg/workflow/compiler_custom_jobs_test.go`); this is a documentation-sync fix only, not a new threat class, so no new `CTR-*` rule is required. (2) Reviewed open code-scanning alerts (severity critical/high) via GitHub MCP: alerts #674/#669/#668/#667 (`go/bad-redirect-check`), #672 (`go/allocation-size-overflow`), and #663 (`js/http-to-file-access`) affect non-compiler tooling (`pkg/cli/add_package_manifest_imports.go`, `pkg/cli/add_package_manifest_includes.go`, `pkg/cli/project_command.go`, `scripts/ensure-docs-slide-pdf.js`) and are outside this specification's conformance targets (Section 3.1: `pkg/workflow/`, `pkg/parser/`, `actions/setup/`); alerts #653 (`workflow-out-of-context`) and #651/#652 (`workflow-go-graphql-injection-sprintf`) are findings from the `daily-malicious-code-scan` and `daily-semgrep-scan` workflows' own self-scans and are tracked by those workflows' remediation processes, not compiler-generated-workflow threat detection. No new compiler-side threat class identified. (3) No `threat-detection-suppress` annotations were found in any live (non-fixture, non-documentation-example) workflow source in this review window, so no `SLA_BREACH` or expiration findings apply. No new `CTR-*` rule required this cycle.

### 7.2 Mapping Audit (2026-08-31)

Audit result: ✅ all listed `CTR-001` through `CTR-026` rows currently include non-empty implementation references and non-empty test coverage targets; no `TODO` placeholders were found in the mapping table. Review window: daily optimizer cycle 2026-08-31, covering commits merged in the prior 48 hours (through `ff62cdb`). Security-relevant items evaluated: (1) **Duplicate Playwright `--no-sandbox` entrypoint arg** (`pkg/workflow/mcp_renderer_builtin.go`, commit `ce08eba4b` "Fix Playwright MCP TOML sandbox configuration"): the unified MCP config renderer's `renderPlaywrightTOML` now emits the same `--no-sandbox` Chromium entrypoint flag already validated for `pkg/workflow/mcp_config_playwright_renderer.go` under the 2026-07-26 audit; this is the identical browser-process-level flag (required for headless Chromium to reach `localhost` inside CI containers), not a workflow sandbox bypass; already covered under CTR-004's established rationale; regression test `pkg/workflow/mcp_config_refactor_test.go` covers the new emission path; no new threat class; no new CTR rule required. (2) No other security-sensitive compiler or parser diffs, and no new `threat-detection-suppress` annotations, were found in the review window. Review window: SPDD daily spec review cycle 2026-07-31 (rotation index 5–9 of 18, covering `specs/compiler-threat-detection-spec.md` among others). Security-relevant items evaluated: (1) **CTR-016/018/019/020/021 sync references**: each of these five rules was individually verified against current `pkg/workflow/` source locations — `safe_update_enforcement.go` (CTR-016), `strict_mode_update_check_validation.go` (CTR-018), `cache.go` + `expression_builder.go` (CTR-019), `pkg/parser/import_bfs.go` (CTR-020), `agent_validation.go` (CTR-021); all implementation references and test coverage targets in Section 7.1 are current and accurate; no drift detected. (2) **Section 6 Optimizer Failure Safeguards** (§6.6): the three failure modes (API unavailability, runner timeout, rate-limit exhaustion) are specified normatively but are not currently covered by a dedicated unit or integration test; flagged as a coverage gap for the next implementation cycle — a future PR should add tests in `pkg/workflow/` or an integration harness that exercises the `OPTIMIZER_DEGRADED`, `OPTIMIZER_TIMEOUT`, and `OPTIMIZER_RATE_LIMITED` diagnostic paths. No new threat class; no new CTR rule required this cycle.

Audit result: ✅ all listed `CTR-001` through `CTR-021` rows currently include non-empty implementation references and non-empty test coverage targets; no `TODO` placeholders were found in the mapping table. Review window: commit d4872c2 (fix: disable Chromium sandbox for playwright-cli mode in CI containers), merged 2026-07-26. Security-relevant items evaluated: (1) **Playwright CLI mode** (`pkg/workflow/playwright_cli.go`): compiler adds `tools.playwright.mode: cli` support; in CLI mode, `@playwright/cli` is installed via npm and `playwright-cli install --skills` runs before the agent; the npm install step uses `RunInstallScripts: true` internally, but this is a compiler-controlled invocation for a single trusted package (`@playwright/cli`) rather than a user-controlled `runtimes.node.run-install-scripts: true` frontmatter flag — CTR-014 validates the latter only; no new threat class; no new CTR rule required. (2) **Playwright MCP deprecation warning** (`pkg/workflow/playwright_validation.go`): compiler emits a non-blocking deprecation warning when `tools.playwright` is in MCP mode; no new permissions or trust surface introduced; no new threat class; no new CTR rule required. (3) **Playwright Chromium `--no-sandbox` flag** (MCP mode, `pkg/workflow/mcp_config_playwright_renderer.go`): disabling Chromium's process sandbox is required for Chromium to reach `localhost` inside CI containers; this is a browser-process-level flag, not a workflow sandbox bypass; threat class is distinct from CTR-004 (workflow sandbox bypass) and is an expected and documented operational necessity for containerized Playwright; no new CTR rule required.

Audit result: ✅ all listed `CTR-001` through `CTR-021` rows currently include non-empty implementation references and non-empty test coverage targets; no `TODO` placeholders were found in the mapping table. Review window: commit 7bdc455 (docs: add weekly update blog post for 2026-07-20), merged 2026-07-20. Security-relevant items evaluated: (1) **Weekly update blog post** (`7bdc455`): documentation-only change; no compiler source, parser, or validation logic modified; no new threat class; no new CTR rule required. No compiler changes, security-sensitive diffs, or open security findings were identified in this review cycle.

Audit result: ✅ all listed `CTR-001` through `CTR-021` rows currently include non-empty implementation references and non-empty test coverage targets; no `TODO` placeholders were found in the mapping table. Review window: commit fe21742 (fix: emit sbx credential refresh step before agent execution), merged 2026-07-12. Security-relevant items evaluated: (1) **`docker-sbx` microVM runtime support** (`pkg/workflow/docker_sbx_install.go`, `pkg/workflow/sandbox_validation.go`): compiler emits KVM check, Docker Hub secrets check, sbx install, sbx auth/daemon-setup, sbx pre-flight smoke, and a new `Refresh sbx credentials` step immediately before AWF agent execution; the refresh step ensures Docker Hub OAuth tokens do not expire between the daemon-setup and agent-creation phases; `sandbox_validation.go` enforces that `docker-sbx` requires `sudo: true` (hard error if absent), compatible runner topology (rejects `arc-dind`), and a minimum AWF version; covered by CTR-004 (sandbox bypass configuration); the `sudo: true` deprecation path in `strict_mode_sandbox_validation.go` is correctly exempted for `docker-sbx`; no new threat class; no new CTR rule required. (2) **Docker Hub secrets via environment variable binding** (`DOCKER_PAT_VAL`, `DOCKER_USERNAME_VAL`): secrets are bound to step-scoped env vars and consumed via `$DOCKER_PAT_VAL` inside the `run:` block; fully compliant with CTR-017 safe binding pattern; no new threat class; no new CTR rule required. (3) **Credential refresh timing** (security improvement): refreshing credentials immediately before `sbx create` prevents authentication failures from token expiry without broadening the workflow's trust surface; no new threat class; no new CTR rule required.

### 7.3 Sync Protocol for CTR Rule and Manifest Updates

When adding, removing, or materially changing any `CTR-*` rule, the same pull request **MUST** update all synchronized artifacts:

1. Section 5.1 rule catalog entry.
2. Section 7.1 implementation mapping row.
3. Section 8.1 test ID coverage entry.
4. Lock file manifest schema and compiler emission logic for any new or changed suppression/manifest fields tied to the rule.

If a CTR rule change lands without a matching lock file manifest update, CI policy **MUST** fail the change as out-of-sync. Recompilation of affected workflows **MUST** occur in the same change set when manifest shape changes.

---

## 8. Compliance Testing

A conforming implementation MUST provide tests that validate:

1. Rule detection triggers for malicious or unsafe inputs.
2. Expected compiler action (reject/rewrite/warn) per rule.
3. Stable diagnostics (rule IDs and actionable messages).
4. No regression in secure generation behavior.

Test updates SHOULD be included whenever rules are added or modified.

### 8.1 Test ID Catalog

The following test IDs map one-to-one to the CTR rules in Section 5.1. Each test case MUST exercise the described detection trigger and verify the expected compiler action. Test IDs are allocated from a single sequence shared with the Section 8.2 optimizer protocol catalog, so a test ID number does not necessarily match its rule ID number (for example, CTR-025 maps to `T-CTR-039`).

| Test ID | Rule | Detection Trigger | Expected Compiler Action | Stable Diagnostic ID |
|---------|------|-------------------|--------------------------|----------------------|
| **T-CTR-001** | CTR-001 Privilege Escalation | Workflow frontmatter declares `permissions: contents: write` (or another write permission) in a non-safe-outputs job without `strict: false` override | Compilation failure with error identifying the unauthorized write permission and suggesting `safe-outputs` | `CTR-001` |
| **T-CTR-002** | CTR-002 Unpinned Action Integrity | A `jobs.*.steps[].uses` field references an action by tag (e.g., `actions/checkout@v6`) or branch name (`@main`) in strict mode | Compilation failure with error identifying the unpinned reference and providing SHA pinning instructions | `CTR-002` |
| **T-CTR-003** | CTR-003 Unsafe Tool Scope Expansion | Workflow grants wildcard tool permissions (e.g., `tools: bash: ["*"]`) in a context where policy forbids it, or an MCP server is granted broader than declared tool scope | Compilation failure or warning identifying the overbroad scope and suggesting a restricted permission set | `CTR-003` |
| **T-CTR-004** | CTR-004 Sandbox Bypass Configuration | Workflow configuration sets `sandbox.agent: false` in strict mode, disabling the agent sandbox firewall | Compilation failure with error identifying the disabled sandbox control and referencing the required configuration; note that the formerly supported top-level `sandbox: false` field is removed and now triggers a schema validation error rather than CTR-004 | `CTR-004` |
| **T-CTR-005** | CTR-005 Unsafe Output Route | Workflow uses a direct write path (e.g., `contents: write` with inline shell commands) that bypasses the safe-outputs subsystem | Compilation failure with error identifying the unsafe write route and requiring use of `safe-outputs` | `CTR-005` |
| **T-CTR-006** | CTR-006 Template Injection | A `run:` step embeds a GitHub Actions expression (`${{ github.event.issue.title }}`) directly in the shell command string without environment variable indirection | Compilation failure with error identifying the injected expression, the affected step, and providing the env-var indirection pattern | `CTR-006` |
| **T-CTR-007** | CTR-007 Markdown Content Security | An externally-sourced markdown workflow file contains a known dangerous pattern (e.g., unicode abuse, embedded HTML script tag, obfuscated link) | Compilation failure or error identifying the detected dangerous pattern, its location in the file, and recommending sanitization | `CTR-007` |
| **T-CTR-008** | CTR-008 Pull Request Target Safety | Workflow declares `on: pull_request_target` and a `checkout` step that references the PR head (`ref: ${{ github.event.pull_request.head.sha }}`) without an explicit fork-safety guard | Compilation failure with error identifying the unsafe checkout pattern, the pwn-request risk, and safe alternatives | `CTR-008` |
| **T-CTR-009** | CTR-009 Shell Expansion in Safe-Outputs | A `safe-outputs` `run:` step contains a dangerous bash expansion (e.g., `${var@Q}`, `${!var}`, `` `cmd` ``, `$(cmd)`) that the safe-outputs security harness would block at runtime | Compilation failure or error identifying the dangerous expansion pattern, the affected step, and safe alternatives | `CTR-009` |
| **T-CTR-010** | CTR-010 Expression Safety Allowlist | A workflow prompt or step uses a GitHub Actions expression not on the approved allowlist (e.g., `${{ github.event.comment.body }}`) or a multi-line expression that could enable exfiltration | Compilation failure with error identifying the disallowed expression, its location, and the approved allowlist | `CTR-010` |
| **T-CTR-011** | CTR-011 Network Firewall Configuration | Workflow declares `network: allowed: [some-domain]` with `ssl-bump: false` (or omits `ssl-bump` when required), or uses a wildcard `*` domain in strict mode | Compilation failure with error identifying the missing prerequisite or disallowed wildcard domain and providing the corrective configuration | `CTR-011` |
| **T-CTR-012** | CTR-012 Safe-Outputs Wildcard Push Scope | Workflow uses `safe-outputs.push-to-pull-request-branch: target: "*"` without a wildcard fetch pattern in checkout (for non-public repos) or without `title-prefix` or `labels` access constraints | Compilation warning identifying the unconstrained wildcard scope and the missing checkout fetch pattern or access constraint; suppressed for public repositories | `CTR-012` |
| **T-CTR-013** | CTR-013 Argument Injection via Package/Image Names | A workflow frontmatter declares an npm/npx package, a pip/uv package, or a Docker container image name that starts with `-` (e.g., `--privileged`, `-exploit`) | Compilation failure with error identifying the invalid name, the affected tool kind, and instructing the user to fix the package or image name | `CTR-013` |
| **T-CTR-014** | CTR-014 Supply Chain Attack via Install Scripts | A workflow frontmatter sets `runtimes.node.run-install-scripts: true` | Compilation warning in non-strict mode identifying the supply chain risk and advising removal of `run-install-scripts: true`; compilation failure in strict mode | `CTR-014` |
| **T-CTR-015** | CTR-015 Allowed Label Glob Scope | A workflow frontmatter sets `safe-outputs.*.allowed-labels` to `["*"]` (bare wildcard) for any safe-output type that supports the field (`create-issue`, `create-discussion`, `update-discussion`, `create-pull-request`, `merge-pull-request`) | Compilation failure with error identifying the field name, explaining that `"*"` disables label restrictions and may permit unintended label-driven automation, and recommending specific names or narrower patterns | `CTR-015` |
| **T-CTR-016** | CTR-016 Compile-Time Manifest Drift | An existing workflow lock file has a `gh-aw-manifest` section recording approved secrets and action references; when recompiled, the new workflow body introduces a secret not in the approved manifest (e.g., `MY_NEW_SECRET`) or a new action reference not previously recorded | Compilation failure with error identifying each new restricted secret and each added or removed action reference beyond the previously approved manifest baseline, preventing silent trust-surface expansion | `CTR-016` |
| **T-CTR-017** | CTR-017 Secret Leakage via Environment Variables | A workflow frontmatter declares a secrets expression (e.g., `${{ secrets.MY_SECRET }}`) in the top-level `env:` section, in `engine.env` for a non-engine var, or in a custom step's `run:` field | Compilation warning in non-strict mode identifying the secrets expression and the section where it appears; compilation failure in strict mode | `CTR-017` |
| **T-CTR-018** | CTR-018 Version Integrity Bypass | A workflow frontmatter sets `check-for-updates: false` | Compilation warning in non-strict mode identifying the disabled version check and advising removal; compilation failure in strict mode | `CTR-018` |
| **T-CTR-019** | CTR-019 Cache-Memory Integrity Enforcement | A workflow has `cache-memory` enabled with `threat-detection: true`; the compiled lock file's `update_cache_memory` job condition must require `needs.detection.result == 'success'` (not accept `skipped`) | Compilation emits `update_cache_memory` job with condition `always() && needs.detection.result == 'success' && needs.agent.result == 'success'`; verified by integration tests | `CTR-019` |
| **T-CTR-020** | CTR-020 Conditional Import Security | A workflow frontmatter `imports:` list contains an entry that is a YAML object with an `if` field (e.g., `- path: shared.md\n  if: experiments.variant == 'a'`) | Compilation failure with error `"import 'if' is no longer supported; use {{#if ...}}{{#runtime-import? ...}}{{/if}} for experiment-specific prompt imports"` | `CTR-020` |
| **T-CTR-021** | CTR-021 Workflow Run Trigger Branch Scope | A workflow frontmatter declares `on: workflow_run:` without a `branches:` restriction (e.g., `on:\n  workflow_run:\n    workflows: ["CI"]\n    types: [completed]`) | Compilation warning in non-strict mode identifying the missing branch restriction and suggesting `branches: [$default-branch]`; compilation failure in strict mode. Separately, omitting `workflows:` or providing an empty list is always a hard error in both modes | `CTR-021` |
| **T-CTR-022** | CTR-022 Git Subprocess Argument Injection | A remote import spec or download configuration resolves a `ref` or `path` value beginning with `-` (e.g., `ref: "--upload-pack=evil"`, `path: "-x"`), containing a NUL byte, containing `..`, or (for paths) an absolute path, immediately before it would be passed as a positional argument to a `git` subprocess | `ValidateGitRef`/`ValidateGitPath` reject the value with an error identifying the unsafe ref or path and the argument-injection risk, before any `git` subprocess is invoked | `CTR-022` |
| **T-CTR-023** | CTR-023 Bash Command Allowlist Illusion | A workflow frontmatter declares `tools.bash: false`, `tools.bash: []`, or a non-wildcard command list (e.g., `tools.bash: ["git", "npm"]`) while the configured engine's `EngineCapabilities.BashCommandAllowlist` is `false` (e.g., `codex`) | Compilation failure with error identifying the engine, stating the restriction is silently ignored at runtime, and suggesting `bash: ["*"]`, removing the entry, or switching to an engine that supports allowlist enforcement (copilot, claude, gemini) | `CTR-023` |
| **T-CTR-039** | CTR-025 Framework Self-Prompt Misattribution | The analyzed prompt artifact starts with a framework-generated `<system>...</system>` block and also contains a later `<system>`-lookalike block in user content | Threat-detection setup strips only the leading framework block and emits the marker while preserving later `<system>`-lookalike content for analysis | `CTR-025` |
| **T-CTR-041** | CTR-026 Generated Job Timeout Expression Injection | A workflow frontmatter sets `jobs.agent.timeout-minutes` or `jobs.detection.timeout-minutes` to a GitHub Actions expression (e.g., `${{ inputs.agent-timeout }}`), zero, a negative integer, or a non-integral float | Compilation failure with error identifying the job name and field and stating that `timeout-minutes` must be a positive integer literal | `CTR-026` |

### 8.2 Optimizer Protocol Test ID Catalog

| Test ID | Requirement | Expected behavior |
|---------|-------------|-------------------|
| **T-CTR-024** | §6.4 item 1 | Suppression validation requires `rule` and a non-empty `reason`; optional `expires` is an ISO 8601 date. |
| **T-CTR-025** | §6.4 item 2 | Active suppression records retain `rule`, `reason`, and `expires` in auditable output. |
| **T-CTR-026** | §6.4 item 3 | MUST-level suppressions are identified for resolution at 10 business days. |
| **T-CTR-027** | §6.4 item 4 | Suppressions older than 10 business days produce complete `SLA_BREACH` entries. |
| **T-CTR-028** | §6.4 item 5 | MUST-level suppressions older than 20 business days produce a follow-up sync action. |
| **T-CTR-029** | §6.4 item 6 | Expired suppressions are re-evaluated and no longer suppress a rule. |
| **T-CTR-030** | §6.6 mode 1 item 1 | API failure produces a complete `OPTIMIZER_DEGRADED` diagnostic instead of noop. |
| **T-CTR-031** | §6.6 mode 1 item 2 | Degraded evaluation cannot produce a PR or spec update. |
| **T-CTR-032** | §6.6 mode 1 items 3–4 | API retries use the bounded exponential back-off policy before degradation. |
| **T-CTR-033** | §6.6 mode 2 item 1 | Timeout produces a complete `OPTIMIZER_TIMEOUT` diagnostic. |
| **T-CTR-034** | §6.6 mode 2 item 2 | Timeout discards partial noop and PR artifacts. |
| **T-CTR-035** | §6.6 mode 2 item 3 | The workflow has an explicit timeout and requests same-day retry. |
| **T-CTR-036** | §6.6 mode 3 item 1 | Rate limiting applies `RATE_LIMIT_RETRY_CONFIG`. |
| **T-CTR-037** | §6.6 mode 3 item 2 | Exhausted rate limiting produces a complete `OPTIMIZER_RATE_LIMITED` diagnostic. |
| **T-CTR-038** | §6.6 mode 3 item 3 | Rate-limited evaluation is incomplete and retries in the next scheduled window. |
| **T-CTR-040** | §6.6 mode 4 | A missed scheduled optimizer run emits `OPTIMIZER_MISSED_CRON`, does not count as completed coverage, and creates a follow-up sync action. |

These optimizer-protocol IDs cover Section 6 norms; they do not add or replace the one-to-one core CTR rule mappings in Section 8.1.

### 8.3 Test Coverage Requirements

- Each active CTR rule MUST have at least one test ID in Section 8.1 that covers the primary detection trigger.
- Tests MUST be deterministic: given the same malicious or unsafe input, the compiler MUST always emit the same diagnostic.
- Tests MUST assert the stable diagnostic ID (e.g., `CTR-006`) appears in the compiler error output so that CI can mechanically verify rule coverage.
- When a new rule is added to Section 5.1, at least one new test ID MUST be added to Section 8.1 in the same change set.
- When a rule is deprecated per Section 5.3.1, its test IDs MUST be marked `[DEPRECATED]` and removed from the required compliance gate.

---

## 9. References

- RFC 2119: Key words for use in RFCs to Indicate Requirement Levels
- GitHub Actions syntax and permissions documentation
- gh-aw security architecture and safe outputs specifications

---

## 10. Change Log

### 1.0.31 (2026-09-09)

- Daily optimizer review cycle. Reachable commit history in this sandbox is limited to `bce650c` (no additional compiler or parser diffs to review beyond the existing baseline).
- Reviewed open critical/high code-scanning alerts. No open critical alerts. Evaluated open high-severity alert #672 (`go/allocation-size-overflow`, `pkg/workflow/mcp_setup_generator.go:146`, within `pkg/workflow/` conformance scope): the flagged `make(map[string]any, len(tools)+1)` capacity hint is derived from the already-parsed, schema-validated, in-process frontmatter `tools` map, not attacker-controlled unbounded input, so no practical overflow or new compiler-generated-workflow threat class applies; documented in Section 7.2 as a code-quality finding, no new `CTR-*` rule required.
- No `threat-detection-suppress` annotations found in this review window; no `SLA_BREACH` findings.

### 1.0.30 (2026-09-06)

- Reconciled the existing `1.0.30` CTR-001 status-function mapping with its missing Section 7.2 audit and Section 10 changelog entries, as required by Section 7.3.
- Confirmed the guard implementation and tests; reviewed critical/high code-scanning alerts and live workflow sources. Findings were outside this specification's compiler targets or were self-scan findings; no new `CTR-*` rule or `SLA_BREACH` finding applies.

### 1.0.29 (2026-08-31)

- Daily optimizer review cycle. Reviewed compiler and parser changes merged in the prior 48 hours (through commit `ff62cdb`). The only security-sensitive diff identified was `ce08eba4b` "Fix Playwright MCP TOML sandbox configuration" (#56800), which adds a `--no-sandbox` Chromium entrypoint arg to the unified MCP config renderer's `renderPlaywrightTOML` (`pkg/workflow/mcp_renderer_builtin.go`) so the refactored TOML-emission path matches the existing behavior of `pkg/workflow/mcp_config_playwright_renderer.go`.
- Evaluated this change against the CTR catalog: the `--no-sandbox` flag disables Chromium's own process sandbox (not the workflow/job sandbox), is required for headless Chromium to reach `localhost` inside CI containers, and is already documented and accepted under the CTR-004 rationale recorded in the 2026-07-26 mapping audit (Section 7.2) for the non-unified renderer path. This is a second emission site for an already-reviewed, already-accepted operational necessity, not a new trust surface; no new threat class; no new `CTR-*` rule required. Regression coverage exists in `pkg/workflow/mcp_config_refactor_test.go`.
- No `threat-detection-suppress` annotations were found in any live (non-fixture, non-documentation-example) workflow source in this review window, so no `SLA_BREACH` or expiration findings apply.
- No new threat class was identified requiring a new `CTR-*` rule.
- Added Section 7.2 Mapping Audit (2026-08-31) entry documenting the above review.
- Updated Section 2 spec-to-implementation sync table with version 1.0.29 entry.

### 1.0.28 (2026-08-28)

- Daily optimizer review cycle. Reviewed the most recent merged compiler change: `Add read-only GitHub Issues access to agent enclaves` (#55531), which adds a compiler-owned mcpg `issues-read-v1` proxy for the `enclaves[].agent.github.cli` configuration (`pkg/workflow/enclaves.go`, `pkg/workflow/enclave_github_proxy.go`, `actions/setup/sh/start_enclave_github_proxy.sh`, `actions/setup/sh/stop_enclave_github_proxy.sh`). Evaluated the new trust surface against the existing CTR catalog: (1) **Network isolation precondition** — `validateEnclavesConfig` rejects any `enclaves:` configuration unless AWF network isolation is enabled (`isAWFNetworkIsolationEnabled`), which is the same precondition class as CTR-004 Sandbox Bypass Configuration; (2) **Version gating** — `validateEnclaveGitHubIssuesVersions` hard-rejects compilation when the effective AWF version is below `v0.28.9` or the effective MCPG version is below `v0.4.13`, preventing the proxy from being emitted against runtime components that lack its readiness/policy-enforcement guarantees; (3) **Read-only operation allowlist** — `operationsForEnclaveGitHubProxyProfile`/`enclaveGitHubProfileOperations` restrict the `issues-read-v1` profile to exactly `issues.comments.list`, `issues.get`, and `issues.list`, a fixed compiler-controlled allowlist that the author cannot widen from workflow frontmatter; (4) **Credential scoping** — `getEffectiveEnclaveGitHubToken` intentionally excludes `GITHUB_TOKEN` from its fallback chain (`GH_AW_GITHUB_MCP_SERVER_TOKEN || GH_AW_GITHUB_TOKEN` only), and the ephemeral per-run capability key (`MCP_GATEWAY_ENCLAVE_CAPABILITY_KEY`) generated in `start_enclave_github_proxy.sh` is masked and scoped to the run/attempt/job-hash identity, consistent with CTR-017 Secret Leakage via Environment Variables safe-binding expectations; (5) **Repository sensitivity/count limits** — `validateEnclaveEntry`/`validateEnclaveRepositories` cap the profile to at most one non-public repository and validate repo slugs against a strict pattern, bounding the blast radius of the read-only proxy. All five items are already implemented and covered by existing tests (`pkg/workflow/enclaves_test.go`, `pkg/workflow/enclave_github_proxy_test.go`); none introduce a threat class outside CTR-004/CTR-017 scope, so no new `CTR-*` rule is required this cycle — this is a mapping-only update.
- No `threat-detection-suppress` annotations were found in any live (non-fixture, non-documentation-example) workflow source in this review window, so no `SLA_BREACH` or expiration findings apply.
- No new threat class was identified requiring a new `CTR-*` rule.
- Extended Section 7.1 CTR-004 mapping row with `pkg/workflow/enclaves.go` and `pkg/workflow/enclave_github_proxy.go` and their test coverage (`pkg/workflow/enclaves_test.go`, `pkg/workflow/enclave_github_proxy_test.go`).
- Updated Section 2 spec-to-implementation sync table with version 1.0.28 entry.

### 1.0.27 (2026-08-26)

- Daily optimizer review cycle. Reviewed the pending `.changeset/*.md` inventory and current `pkg/workflow` source since the 1.0.26 audit. All reviewed items were found to be already covered by existing `CTR-*` rules or outside compiler-detectable scope: `patch-escape-mcp-template-expressions` (MCP gateway guard-policy expressions are escaped via the sentinel mechanism in `pkg/workflow/mcp_renderer_guard.go` — this was implemented but had not yet been added to the Section 7.1 CTR-006 mapping table, so it is added here as a mapping-only update, not a new rule); `patch-fix-heredoc-delimiter-injection` (heredoc delimiter randomization/normalization, already covered by CTR-006's `heredoc_validation.go` mapping); `patch-fix-multi-repo-configure-git-credentials-template-injection` (already reconciled in the 1.0.25 cycle, confirmed unchanged in `checkout_step_generator.go`); `patch-sanitize-template-delimiters` (Jinja2/Liquid/ERB/JS/Jekyll delimiter neutralization in `sanitize_content_core.cjs`'s `neutralizeTemplateDelimiters`, already covered by CTR-007's markdown-content-security mapping); `patch-fix-shell-escape-agent-path-injection` (already reconciled in the 1.0.24 cycle, CTR-009 mapping unchanged); `patch-fix-codex-threat-detection-proxy`, `patch-threat-detection-ghe-api-target`, `patch-skip-empty-threat-detection` (CTR-019 cache-memory gating scope, no behavior change to detection-skip gating logic), `patch-inject-difc-proxy-pre-agent-gh`, `patch-inject-git-identity-env-vars` (operational/runtime wiring correctness for detection and sandboxed execution, not new detectable compiler patterns), and `hint-jq-file-injection-safe-outputs-prompt` (documentation-only prompt guidance, no compiler detection logic).
- No `threat-detection-suppress` annotations were found in any live (non-fixture, non-documentation-example) workflow source in this review window, so no `SLA_BREACH` or expiration findings apply.
- No new threat class was identified requiring a new `CTR-*` rule.
- Extended Section 7.1 CTR-006 mapping row with `pkg/workflow/mcp_renderer_guard.go` and its test coverage in `pkg/workflow/copilot_github_mcp_test.go`.
- Updated Section 2 spec-to-implementation sync table with version 1.0.27 entry.

### 1.0.26 (2026-08-23)

- Daily optimizer review cycle. Reviewed compiler and parser changes merged since the 1.0.25 audit (`d2432aca6`..`HEAD` at `11a1dcf84`): `Enforce positive-integer jobs.*.timeout-minutes in compiled lock files` (#54884) rejects GitHub Actions expressions, zero, negative, and non-integral values for `jobs.agent.timeout-minutes` and `jobs.detection.timeout-minutes` in `extractCustomJobTimeoutMinutes` (`pkg/workflow/compiler_custom_job_properties.go`), and removes the previous `${{ vars.GH_AW_DEFAULT_*_TIMEOUT_MINUTES || '<n>' }}` runtime-variable indirection in favor of a compile-time-resolved literal integer (`resolveAgentJobTimeoutValue`/`resolveDetectionJobTimeoutValue` in `pkg/workflow/job_timeouts.go`). This closes a gap where an author- or import-controlled expression could defer a generated job's timeout budget to workflow-run time (or an invalid non-positive value could produce an unbounded/undefined job budget), undermining the job-level timeout guarantee that bounds setup, agentic execution, and teardown steps against a hung or malicious step consuming runner capacity indefinitely. This was implemented (not merely already covered) during the review window, so it is added as new rule **CTR-026 Generated Job Timeout Expression Injection** with test ID **T-CTR-041**, mapped to `TestApplyBuiltinJobAugmentations_RejectsTimeoutMinutesExpression`, `TestExtractCustomJobTimeoutMinutes`, `TestBuildCustomJob_InvalidTimeoutMinutesError` (`pkg/workflow/compiler_custom_jobs_test.go`) and `TestResolveAgentJobTimeoutValue`, `TestResolveDetectionJobTimeoutValue`, `TestGeneratedAgentJobTimeoutMinutes` (`pkg/workflow/job_timeouts_test.go`).
- Other reviewed changesets in the window were evaluated and found to be outside compiler-detectable threat-class scope or already covered: `Add secret-scanning-alerts to workflow permissions schema` (#54904, schema enum extension, covered by existing CTR-001 permissions-validation mapping — no behavior change to detection logic); `Add regression test diffing permissions.go constants against permissions schema enum` (#54928, test-only regression guard for CTR-001 schema/constant drift, no new detectable pattern); `Make generated artifact downloads resilient to missing optional artifacts` (#54900, best-effort `continue-on-error` artifact download step generation with a GHES-compatible fallback path when the download action lacks `pattern`/`merge-multiple` support — operational resilience, not a security control); `Treat non-waiting workflow runs as skipped in approve_workflow_run` (#54895, runtime approval-gate correctness fix, no new trust-surface or detectable compiler pattern); `Refactor WSRF as a builtin grader` (#54934) and `Advertise typed workflow safe-output tools` (#54817) (already-covered CTR-005 safe-outputs surface, refactor/typing only, no behavior change); `revert to v0.28.4` / `Bump default gh-aw-firewall (AWF) version to v0.28.5` (#54894) and associated `filesystem.allowWrite` test fixes (#54930) (firewall version pin churn, covered by existing CTR-004/CTR-011 sandbox and network mappings, no new threat class); `Unify create-* close-older fields via shared CloseOlderConfig embed` (#54656) and samples-replay fixes (#54813, #54814) (developer-tooling and test-infrastructure refactors, no compiler validation logic changed).
- No `threat-detection-suppress` annotations were found in any live (non-fixture, non-documentation-example) workflow source in this review window, so no SLA-breach or expiration findings apply.
- Extended Section 5.1 with CTR-026, Section 7.1 baseline rule mapping with the CTR-026 row, and Section 8.1 with T-CTR-041.
- Updated Section 2 spec-to-implementation sync table with version 1.0.26 mapped to minimum binary `v0.87.4`, noting the `.lock.yml` job-level `timeout-minutes` literal-vs-expression emission change for the generated `agent`/`detection` jobs (no secrets/action-manifest impact).

### 1.0.25 (2026-08-21)

- Daily optimizer review cycle. Reviewed pending changesets and outstanding work since the 1.0.24 audit (`.changeset/*.md` staged for the next release plus `HEAD` at `c9199e1`): `fix-threat-detection-system-block-false-positive` (already-implemented CTR-025 behavior — `stripFrameworkSystemBlock`/`SYSTEM_BLOCK_REMOVED_MARKER` in `actions/setup/js/setup_threat_detection.cjs` strips only the leading framework `<system>` block; re-verified against current source, mapping unchanged), `patch-fix-github-env-high-vulnerability` (in-progress/staged hardening replacing framework-controlled `>> $GITHUB_ENV` writes with step-scoped `$GITHUB_OUTPUT` in select runtime-setup and GHES-host wiring; several `$GITHUB_ENV` writes for non-secret, compiler-controlled runtime paths remain by design — evaluated as CTR-004/CTR-017 scope hardening, not a new threat class), `patch-exclude-secret-env-vars-from-agent-container` (extends CTR-017 secret-leakage mitigation via AWF `--exclude-env`, no new rule), `patch-prevent-mcp-secret-masking` (extends CTR-017 mapping — MCP-safe git auth env separated from Actions secret masking), `patch-fix-multi-repo-configure-git-credentials-template-injection` (extends CTR-006 mapping — `${{ github.event.inputs.* }}` now passed through `GH_AW_SUBREPO_N` env vars instead of being inlined into the generated `git remote set-url` shell command, confirmed implemented in `pkg/workflow/checkout_step_generator.go`), `patch-document-bypasspermissions-security-boundary` and `hint-jq-file-injection-safe-outputs-prompt` (documentation-only, no compiler detection logic), and `patch-cross-repo-private-callee-auth-guidance` (improved diagnostic messaging, no new detectable pattern).
- No `threat-detection-suppress` annotations were found in any live (non-fixture, non-test) workflow source in this review window, so no SLA-breach or expiration findings apply.
- No new threat class was identified requiring a new `CTR-*` rule; all reviewed items either extend existing rule mappings (CTR-004, CTR-006, CTR-017) or fall outside compiler-detectable scope (documentation, diagnostics wording).
- Updated Section 2 spec-to-implementation sync table with version 1.0.25 entry.

### 1.0.24 (2026-08-18)

- Daily optimizer review cycle. Reviewed recent security-relevant changesets: URL-authority userinfo-prefix allowlist bypass in the content sanitizer (`fix-protocol-relative-url-userinfo-bypass`, strengthens CTR-007's `sanitize_content_core.cjs` mapping — camo-proxy exfiltration via `https://allowlisted.com@evil.com/` differential between the stripping and filtering regex passes); patch-parser vs. `git am` protected-file check bypass (`patch-fix-patch-parser-file-protection-bypass`, strengthens CTR-005's file-protection enforcement mapping with `patch_path_helpers.cjs`/`manifest_file_helpers.cjs` diff-header parsing hardening); shell-escaping bypass in engine command generation for crafted agent import paths (`patch-fix-shell-escape-agent-path-injection`, strengthens CTR-009 mapping with `agent_validation.go` path allowlist regex and `shell.go` consistent escaping); generalized wildcard-target safe-outputs validation so missing target identifiers fail at compile time instead of apply time (`patch-generalize-wildcard-target-validation`, strengthens CTR-012 mapping); and hardened safe-output field validation restricting handlers to declared fields plus trusted allowlisted comment-reuse IDs (`patch-harden-safe-output-field-validation`, strengthens CTR-005 mapping). All five items extend existing rule coverage; none introduce a new threat class requiring a new `CTR-*` rule.
- Evaluated additional non-security or already-covered items from the same review window: MCP actor validation runtime flag (not a compiler detection rule), cross-repo allowlist validation hardening (already covered by CTR-005/CTR-012), MCP config schema validation wiring (CTR-003/CTR-011 covered), detection-job/OTLP-OIDC/discussions permission fixes (CTR-001 scope, operational correctness rather than new detectable threat class), git-identity env var injection and DIFC proxy wiring for pre-agent steps (defense-in-depth hardening, no new compiler-detectable pattern), and dependency/documentation-only changes.
- Extended Section 7.1 baseline rule mapping table for CTR-005, CTR-006/CTR-007 (no change to CTR-006 row), CTR-009, and CTR-012 with the concrete implementation and test references identified above.
- Updated Section 2 spec-to-implementation sync table with version 1.0.24 entry.
- No `SLA_BREACH` suppression findings and no `threat-detection-suppress` annotations were present in the reviewed diffs during this cycle.

### 1.0.23 (2026-08-16)

- Added CTR-025 Framework Self-Prompt Misattribution to Section 5.1, documenting the existing runtime behavior that strips only the leading framework-generated `<system>...</system>` block before threat analysis.
- Added T-CTR-039 to Section 8.1 as conformance coverage for CTR-025 without colliding with optimizer-protocol IDs T-CTR-024 through T-CTR-038.
- Extended Section 7.1 baseline rule mapping with CTR-025 implementation references in `actions/setup/js/setup_threat_detection.cjs` and tests in `actions/setup/js/setup_threat_detection.test.cjs`.
- Updated Section 2 spec-to-implementation sync table with version 1.0.23 mapped to minimum binary `v0.87.1` and no `.lock.yml` schema changes.

### 1.0.22 (2026-08-15)

- Added T-CTR-024 through T-CTR-029 conformance coverage for false-positive suppression validation, auditing, SLA, escalation, and expiration handling.
- Added T-CTR-030 through T-CTR-038 conformance coverage for optimizer API, timeout, and rate-limit safeguards.
- Synchronized the daily optimizer workflow with the Section 6 protocol and daily schedule.

### 1.0.21 (2026-08-14)

- Corrected the Deprecation Policy subsection number from 4.3.1 to 5.4 so that it matches Section 5, Normative Rule Requirements.

### 1.0.20 (2026-08-03)

- Added CTR-022 Git Subprocess Argument Injection (already implemented: `ValidateGitRef`/`ValidateGitPath` in `pkg/gitutil/gitutil.go` reject `ref`/`path` values beginning with `-`, containing NUL bytes or `..`, or absolute paths, before they are passed as positional arguments to `git` subprocesses in `pkg/cli/download_workflow.go`, `pkg/parser/remote_resolve_sha.go`, `pkg/parser/remote_workflow_spec.go`, `pkg/parser/remote_download_file.go`, and `pkg/parser/import_remote.go`; fixed upstream in commit 85d757b6 as CWE-88 remediation for the `git archive` fallback path, then confirmed already applied consistently across all other git subprocess call sites during this audit)
- Added T-CTR-022 test ID entry in Section 8.1
- Extended Section 7.1 baseline rule mapping table with CTR-022 implementation references (`pkg/gitutil/gitutil.go`) and test coverage (`pkg/gitutil/gitutil_test.go`, `pkg/cli/download_workflow_test.go`)
- Added CTR-023 Bash Command Allowlist Illusion (already implemented: `validateBashCommandAllowlistSupport`/`hasBashExplicitRestriction` in `pkg/workflow/agent_validation.go` reject explicit `tools.bash` restrictions — `bash: false`, `bash: []`, or non-wildcard command lists — for engines whose `EngineCapabilities.BashCommandAllowlist` is `false`, such as `codex`, preventing the illusion of a bash restriction that is silently ignored at runtime; implemented in commit 34035eee5)
- Added T-CTR-023 test ID entry in Section 8.1
- Extended Section 7.1 baseline rule mapping table with CTR-023 implementation references (`pkg/workflow/agent_validation.go`, `pkg/workflow/agentic_engine.go`) and test coverage (`pkg/workflow/bash_command_allowlist_validation_test.go`)
- Updated Section 2 spec-to-implementation sync table with version 1.0.20 entry
- Reviewed remaining commits since the 1.0.19 audit (2026-07-31 through 2026-08-03, `846bb1732`..`24f6c45b9`): shellcheck linting pipeline changes, comment-only frontmatter parsing fix, `on.needs` emission fix, agent job gating (`jobs.agent.needs`/`jobs.agent.if`), MCP container CVE removals (`mcp/notion`, `mcp/markitdown`, `semgrep/semgrep`, `gh-aw-firewall` stale pins, `cli-proxy` stale pin), `sbx` bounded-query runtime support, and dependency/pricing updates — all evaluated as either non-security-relevant compiler behavior, already-covered CTR scope (container CVE removals reduce attack surface but do not introduce a new compiler-detectable threat class), or pre-existing CTR-004/CTR-011 sandbox and firewall scope; no additional new threat class identified beyond CTR-022 and CTR-023

### 1.0.19 (2026-07-31)

- Updated Section 7.2 mapping audit to 2026-07-31 covering SPDD daily spec review cycle (rotation index 5–9 of 18): re-validated CTR-016/018/019/020/021 sync references against current `pkg/workflow/` implementations — all Section 7.1 references confirmed accurate, no drift detected; identified Section 6.6 Optimizer Failure Safeguards (`OPTIMIZER_DEGRADED`, `OPTIMIZER_TIMEOUT`, `OPTIMIZER_RATE_LIMITED` diagnostic paths) as untested — flagged as a coverage gap for the next implementation cycle; no new threat class; no new CTR rules required
- Updated Section 2 spec-to-implementation sync table with version 1.0.19 entry

### 1.0.18 (2026-07-27)

- Updated Section 7.2 mapping audit to 2026-07-27 covering commit d4872c2 (fix: disable Chromium sandbox for playwright-cli mode in CI containers, 2026-07-26): evaluated Playwright CLI mode (`playwright_cli.go`: compiler-generated npm install for `@playwright/cli`, not user-frontmatter-controlled `run-install-scripts`), Playwright MCP deprecation warning (`playwright_validation.go`: non-blocking deprecation warning, no new trust surface), and Playwright Chromium `--no-sandbox` flag (`mcp_config_playwright_renderer.go`: browser-process-level flag distinct from CTR-004 workflow sandbox bypass); no new threat class; no new CTR rules required
- Updated Section 2 spec-to-implementation sync table with version 1.0.18 entry

### 1.0.17 (2026-07-20)

- Updated Section 7.2 mapping audit to 2026-07-20 covering commit 7bdc455 (docs: add weekly update blog post for 2026-07-20): documentation-only change with no compiler source, parser, or validation logic modifications; no new threat class; no new CTR rules required
- Updated Section 2 spec-to-implementation sync table with version 1.0.17 entry

### 1.0.16 (2026-07-13)

- Updated Section 7.2 mapping audit to 2026-07-13 covering commit fe21742 (fix: emit sbx credential refresh step before agent execution, merged 2026-07-12): evaluated `docker-sbx` microVM runtime support (CTR-004 covered — `sandbox_validation.go` enforces `sudo: true`, topology, and AWF version; `strict_mode_sandbox_validation.go` exempts `docker-sbx` from sudo deprecation), Docker Hub secrets via env var binding (CTR-017 compliant), and credential refresh timing improvement (security improvement, no new threat class); no new CTR rules required
- Updated Section 2 spec-to-implementation sync table with version 1.0.16 entry

### 1.0.15 (2026-07-06)

- Added CTR-021 Workflow Run Trigger Branch Scope (compiler-level detection of `workflow_run` triggers that lack `branches:` restrictions; unrestricted triggers fire on all branches including attacker-controlled branches and expose `github.event.workflow_run.*` context data; warn in non-strict mode, reject in strict mode; missing or empty `workflows:` field is always a hard error; implemented in `pkg/workflow/agent_validation.go` via `validateWorkflowRunBranches`, `validateWorkflowRunHasWorkflows`, `emitWorkflowRunMissingBranches`)
- Added T-CTR-021 test ID entry in Section 8.1
- Extended Section 7.1 baseline rule mapping table with CTR-021 implementation references (`pkg/workflow/agent_validation.go`) and test coverage (`pkg/workflow/workflow_run_validation_test.go`)
- Updated Section 7.2 mapping audit to 2026-07-06 covering commit c9361ed (squash merge, 2026-07-05): evaluated `workflow_run` branch scope enforcement (new CTR-021), OTLP headers env var (compile-time artifact hardening only), `remove-imports-if` (CTR-020 covered), `sandbox.agent: false` (CTR-004 covered), `network-isolation` → `sudo` rename (refactoring), MCP config schema validation (CTR-003/CTR-011 covered), cross-repo allowlist validation (CTR-005/CTR-012 covered), `skip-empty-threat-detection` (CTR-019 covered), `auto-hoist-run-expressions` (CTR-006 related, no new rule), and runtime-only dependency bumps
- Updated Section 2 spec-to-implementation sync table with version 1.0.15 entry

### 1.0.14 (2026-06-29)

- Added CTR-020 Conditional Import Security (compiler-level rejection of `imports:` entries containing an `if` field; conditional imports can alter workflow setup and security posture at runtime; implemented in `pkg/parser/import_bfs.go` `parseImportSpecsFromArray`)
- Added T-CTR-020 test ID entry in Section 8.1
- Extended Section 7.1 baseline rule mapping table with CTR-020 implementation references (`pkg/parser/import_bfs.go`)
- Extended CTR-010 mapping with `runtime_import_validation.go` (`validateRuntimeImportFiles`): runtime-import macro targets are now validated against the expression allowlist
- Updated Section 7.2 mapping audit to 2026-06-29 covering commit d6d9160 (PR #42146): evaluated conditional import rejection (→ CTR-020), sandbox validation consolidation (strengthens CTR-004, already covered by mapping pattern), runtime import expression validation (extends CTR-010), network-isolation → sudo rename (refactoring only), and mcpg/firewall dependency bumps (runtime-only)
- Updated Section 2 spec-to-implementation sync table with version 1.0.14 entry

### 1.0.13 (2026-05-27)

- Updated Section 7.2 mapping audit to 2026-05-27 confirming no new uncovered threats in this review cycle
- Evaluated nine security-relevant items from PRs #35005–#35078: permission-scope validation caching (perf-only, CTR-001 detection unchanged), `ghs_` token redaction regex update (runtime-only, outside compiler scope), Codex structured outputs for threat detection parsing (detection infrastructure, no new rule required), `add_comment` locked-target handling (safe-outputs operational fix), `github-workflow.json` schema `code-quality` key addition (JSON schema only; compiler frontmatter enforcement unaffected), and several documentation/dependency-only PRs with no security impact
- Updated Section 2 spec-to-implementation sync table with version 1.0.13 entry

### 1.0.12 (2026-05-26)

- Updated Section 7.2 mapping audit to 2026-05-26 confirming no new uncovered threats in this review cycle
- Evaluated three security items from PR #34841: heredoc delimiter injection defense (already covered by CTR-006), MCP actor validation runtime flag (not a compiler detection rule), and cross-repo allowlist validation for SEC-005 (strengthens CTR-005/CTR-012 boundaries; no new CTR rule required)
- Updated Section 2 spec-to-implementation sync table with version 1.0.12 entry

### 1.0.11 (2026-05-25)

- Corrected CTR-018 implementation mapping: `pkg/workflow/update_check_validation.go` → `pkg/workflow/strict_mode_update_check_validation.go` (the spec referenced a non-existent filename; the actual implementation and test file are `strict_mode_update_check_validation.go` and `strict_mode_update_check_validation_test.go`)
- Updated Section 7.2 mapping audit to 2026-05-25 noting the CTR-018 filename correction and confirming no new uncovered threats in this review cycle
- Updated Section 2 spec-to-implementation sync table with version 1.0.11 entry

### 1.0.10 (2026-05-22)

- Added CTR-019 Cache-Memory Integrity Enforcement (enforces that `update_cache_memory` job only runs when threat detection succeeds, not when skipped or failed; prevents cache pollution from unvalidated agent outputs; implemented in `cache.go` via `buildDetectionSuccessCondition` instead of `buildDetectionPassedCondition`)
- Added T-CTR-019 test ID entry in Section 8.1
- Extended Section 7.1 baseline rule mapping table with CTR-019 implementation references (`cache.go`, `expression_builder.go`)
- Updated Section 2 spec-to-implementation sync table to include CTR-019 in lock-file compatibility notes for version 1.0.10
- Updated Section 7.2 mapping audit timestamp to 2026-05-22 and notes to reflect CTR-019 addition

### 1.0.9 (2026-05-17)

- Updated T-CTR-004 detection trigger from deprecated `sandbox: false` (removed field) to `sandbox.agent: false` in strict mode; noted that the old top-level `sandbox: false` now triggers a schema validation error rather than CTR-004
- Extended CTR-004 implementation mapping with `strict_mode_permissions_validation.go`, which is the concrete enforcement site for `sandbox.agent: false` rejection in strict mode
- Updated Section 7.2 mapping audit timestamp and notes to reflect the CTR-004 mapping correction

### 1.0.8 (2026-05-16)

- Added CTR-018 Version Integrity Bypass (warn/reject when `check-for-updates: false` disables the compile-agentic version update check; implemented in `update_check_validation.go`)
- Added T-CTR-018 test ID entry in Section 8.1
- Extended Section 7.1 baseline rule mapping table with CTR-018 implementation references (`update_check_validation.go`)

### 1.0.7 (2026-05-16)

- Added CTR-017 Secret Leakage via Environment Variables (warn/reject when secrets expressions appear in top-level `env:`, `engine.env`, or in uncontrolled custom step fields; implemented in `strict_mode_env_validation.go` and `strict_mode_steps_validation.go`)
- Added T-CTR-017 test ID entry in Section 8.1
- Extended Section 7.1 baseline rule mapping table with CTR-017 implementation references (`strict_mode_env_validation.go`, `strict_mode_steps_validation.go`)
- Updated mapping audit note to cover CTR-001 through CTR-018

### 1.0.6 (2026-05-15)

- Added CTR-016 Compile-Time Manifest Drift (compilation rejection when recompilation of an existing workflow would introduce new restricted secrets or unapproved action references beyond the previously approved lock file manifest baseline; detected by `EnforceSafeUpdate` in `safe_update_enforcement.go`, called from `compiler.go`)
- Added T-CTR-016 test ID entry in Section 8.1
- Extended Section 7.1 baseline rule mapping table with CTR-016 implementation references (`safe_update_enforcement.go`, `compiler.go`)

### 1.0.5 (2026-05-14)

- Added CTR-015 Allowed Label Glob Scope (compilation error when `safe-outputs.*.allowed-labels` contains a bare `"*"` wildcard that effectively disables label restrictions and may permit unintended label-driven automation; triggered by the new glob pattern support for `allowed-labels` introduced in gh-aw #32027)
- Added T-CTR-015 test ID entry in Section 8.1
- Extended Section 7.1 baseline rule mapping table with CTR-015 implementation references (`safe_outputs_allowed_labels_validation.go`)

### 1.0.4 (2026-05-13)

- Added CTR-014 Supply Chain Attack via Install Scripts (warn/reject when `run-install-scripts: true` is configured; protects against malicious npm pre/post install hooks)
- Added T-CTR-014 test ID entry in Section 8.1
- Extended Section 7.1 baseline rule mapping table with CTR-014 implementation references (`run_install_scripts_validation.go`)

### 1.0.3 (2026-05-11)

- Added CTR-013 Argument Injection via Package/Image Names (hyphen-prefix package/image name rejection for npm/npx, pip/uv, and Docker to prevent exec.Command argument injection)
- Added T-CTR-013 test ID entry in Section 8.1
- Extended Section 7.1 baseline rule mapping table with CTR-013 implementation references

### 1.0.2 (2026-05-09)

- Added CTR-012 Safe-Outputs Wildcard Push Scope (unconstrained write scope detection in safe-outputs push-to-pull-request-branch subsystem)
- Extended CTR-001 mapping with `github_app_permissions_validation.go` (GitHub App-only permission scope enforcement)
- Extended CTR-006 mapping with `heredoc_validation.go` (heredoc delimiter injection defense)
- Extended CTR-010 mapping with `expression_syntax_validation.go` (structural expression syntax validation)
- Extended CTR-011 rule description and mapping with `strict_mode_network_validation.go` (wildcard domain rejection in strict mode)
- Updated Section 7.1 baseline rule mapping table for CTR-001, CTR-006, CTR-010, CTR-011, and CTR-012

### 1.0.1 (2026-05-08)

- Extended CTR rule catalog from 5 to 11 rules to reflect existing compiler coverage
- Added CTR-006 Template Injection (template injection detection in shell run: steps)
- Added CTR-007 Markdown Content Security (unicode abuse, hidden content, HTML abuse, social engineering)
- Added CTR-008 Pull Request Target Safety (pwn-request prevention for pull_request_target trigger)
- Added CTR-009 Shell Expansion in Safe-Outputs (dangerous bash expansion detection at compile time)
- Added CTR-010 Expression Safety Allowlist (approved expression enforcement, multi-line rejection)
- Added CTR-011 Network Firewall Configuration (firewall dependency and domain pattern validation)
- Updated Section 7.1 baseline rule mapping table with concrete file references for CTR-006 through CTR-011

### 1.0.0 (2026-05-06)

- Initial W3C-style specification for compiler threat detection rule governance
- Defined daily optimizer reconciliation protocol
- Established baseline `CTR-*` rule catalog and conformance model
