---
title: GitHub Actions Compiler Threat Detection Specification
description: Normative requirements for compiler rules that prevent unsafe generated workflows
sidebar:
  order: 1001
---

# GitHub Actions Compiler Threat Detection Specification

**Version**: 1.0.31
**Status**: Candidate Recommendation  
**Latest Version**: https://github.com/github/gh-aw/blob/main/specs/compiler-threat-detection-spec.md  
**Editors**: GitHub Next (GitHub, Inc.)

## Abstract

This specification is the source of truth for compiler-side threat detection in GitHub Agentic Workflows (gh-aw). A conforming implementation detects unsafe generated GitHub Actions behavior before runtime and keeps rule definitions, implementations, tests, and daily review evidence synchronized.

## Status

The gh-aw maintainers revise this Candidate Recommendation using security-review and conformance evidence. Requirement keywords use [RFC 2119](https://www.rfc-editor.org/rfc/rfc2119).

## 1. Scope and Conformance

This specification covers `pkg/workflow/`, related `pkg/parser/` and `actions/setup/` validation, and the daily optimizer. It excludes runtime detection-job internals, external scanner ecosystems, and non-compiler repositories.

A conforming implementation satisfies every MUST in Sections 2–6. Rules are specification-first, secure by default, mapped bidirectionally to implementation, and auditable through version history.

## 2. Spec-to-Implementation Sync

Each version maps to the minimum compatible binary. A version change MUST update this table and any lock-file compatibility change in the same pull request.

| Versions | Minimum gh-aw | Compatibility |
|---|---:|---|
| `1.0.31` | `v0.87.9` | Audit-only; no new CTR rule or lock-file schema change. |
| `1.0.30` | `v0.87.9` | CTR-001 status-function guard mapping update only. |
| `1.0.29` | `v0.87.9` | CTR-004 Playwright renderer mapping update only. |
| `1.0.27`–`1.0.28` | `v0.87.4` | CTR-004 enclave and CTR-006 guard-renderer mapping updates only. |
| `1.0.26` | `v0.87.4` | Adds CTR-026; generated job timeouts are literal positive integers. |
| `1.0.23`–`1.0.25` | `v0.87.1` | Adds CTR-025; mapping-only audit updates otherwise. |
| `1.0.22` | `v0.87.0` | Adds suppression manifest and optimizer safeguards. |
| `1.0.15`–`1.0.21` | `v0.72.1`–`v0.83.6` | Adds CTR-021–023 and editorial/mapping updates. |
| `1.0.8`–`1.0.14` | `v0.72.1` | Establishes CTR-016, CTR-018–020; earlier versions establish CTR-001–015. |

## 3. Rule Model and Requirements

Each rule has a stable `CTR-*` ID, threat class, trigger, compiler action, diagnostic, implementation mapping, and test. The compiler MUST produce deterministic, actionable diagnostics and either reject insecure generation or apply a safe rewrite.

### 3.1 Rule Catalog

| Rule | Required detection and action |
|---|---|
| CTR-001 Privilege Escalation | Reject unauthorized generated-job write permissions. |
| CTR-002 Unpinned Action Integrity | Reject unpinned action references in strict contexts. |
| CTR-003 Unsafe Tool Scope Expansion | Reject or warn on policy-violating wildcard or overbroad tool scope. |
| CTR-004 Sandbox Bypass Configuration | Reject generated configuration that disables required sandboxing. |
| CTR-005 Unsafe Output Route | Reject direct write paths that bypass safe outputs. |
| CTR-006 Template Injection | Reject user-controlled expressions directly embedded in shell commands. |
| CTR-007 Markdown Content Security | Detect unsafe external markdown, including obfuscation, scripts, and social engineering. |
| CTR-008 Pull Request Target Safety | Reject unsafe `pull_request_target` checkout patterns. |
| CTR-009 Shell Expansion in Safe-Outputs | Reject dangerous shell expansion in safe-output scripts. |
| CTR-010 Expression Safety Allowlist | Reject unauthorized or multiline GitHub Actions expressions. |
| CTR-011 Network Firewall Configuration | Reject missing firewall prerequisites and strict-mode wildcard domains. |
| CTR-012 Safe-Outputs Wildcard Push Scope | Warn for unconstrained wildcard PR-branch pushes. |
| CTR-013 Argument Injection via Package/Image Names | Reject hyphen-prefixed package and image names before subprocess use. |
| CTR-014 Supply Chain Attack via Install Scripts | Warn, or reject in strict mode, when Node install scripts are enabled. |
| CTR-015 Allowed Label Glob Scope | Reject bare `*` safe-output allowed-label patterns. |
| CTR-016 Compile-Time Manifest Drift | Reject new restricted secrets or action references absent from an existing manifest. |
| CTR-017 Secret Leakage via Environment Variables | Warn, or reject in strict mode, for uncontrolled secret-expression placement. |
| CTR-018 Version Integrity Bypass | Warn, or reject in strict mode, for `check-for-updates: false`. |
| CTR-019 Cache-Memory Integrity Enforcement | Require cache updates only after successful agent and threat-detection jobs. |
| CTR-020 Conditional Import Security | Reject `imports` entries containing `if`. |
| CTR-021 Workflow Run Trigger Branch Scope | Warn, or reject in strict mode, for unscoped `workflow_run`; always reject missing `workflows`. |
| CTR-022 Git Subprocess Argument Injection | Reject unsafe remote ref/path arguments before invoking Git. |
| CTR-023 Bash Command Allowlist Illusion | Reject explicit bash restrictions for engines that cannot enforce them. |
| CTR-025 Framework Self-Prompt Misattribution | Strip only a leading framework `<system>` block before analysis. |
| CTR-026 Generated Job Timeout Expression Injection | Reject non-positive or expression job timeout values. |

### 3.2 Lifecycle and Deprecation

When a threat is found, maintainers MUST add its mapping and test if covered, or implement detection, tests, and documentation if not. Experimental threats MUST NOT fail production compilation. Candidates need a trigger, action, stable diagnostic, test, deployment evidence, and security-maintainer review before becoming normative.

Removing a rule dependency MUST deprecate—not delete—the catalog and mapping rows in the same change set. The catalog records the version and reason; mapped tests become `[DEPRECATED]`; the mapping implementation cell is cleared; and the changelog records the retirement. `TestFormal_DeprecationPolicy_SpecArtifactsConform` enforces this policy.

## 4. Daily Optimizer Protocol

The daily optimizer MUST review recent compiler changes, related validation paths, open/recent security findings, and this catalog. For each candidate threat, it MUST determine coverage, update the mapping/tests if covered, or implement, test, and document remediation if uncovered. Its output MUST be either a pull request or an explicit noop report.

### 4.1 Suppressions

`threat-detection-suppress` entries MUST provide a rule and non-empty reason; `expires` is optional ISO 8601. Active entries MUST retain rule, reason, and expiry in the lock-file manifest. Expired entries do not suppress a rule.

The optimizer SHOULD resolve false positives affecting non-strict rejection controls within 10 business days. It MUST report older suppressions as `SLA_BREACH` with rule, reason, age, owner, and expiry, and MUST create a follow-up action after 20 business days.

### 4.2 Failure Safeguards

| Failure | Required behavior |
|---|---|
| API unavailable | Retry with bounded exponential backoff (10 seconds to 5 minutes, three attempts); then emit `OPTIMIZER_DEGRADED` with endpoint, error, and UTC time. Do not create artifacts from incomplete data. |
| Timeout | Emit `OPTIMIZER_TIMEOUT` with completed step and unevaluated rules; discard partial artifacts. Use an explicit timeout and request same-day retry. |
| Rate limit | Apply `RATE_LIMIT_RETRY_CONFIG`; after exhaustion emit `OPTIMIZER_RATE_LIMITED` with endpoint and retry metadata. Count neither completion nor noop; retry next window. |
| Missed schedule | Emit `OPTIMIZER_MISSED_CRON` with scheduled and detected times and lookback; do not count completion and create a follow-up action. |

## 5. Implementation Mapping

Every active rule MUST map to implementation and test coverage. References are patterns and MUST be verified against concrete paths whenever changed.

| Rule | Implementation | Tests |
|---|---|---|
| CTR-001 | `pkg/workflow/*permissions*validation*.go`, `compiler_builtin_job_augmentation.go` | `*permissions*_test.go`, `compiler_custom_jobs_test.go` |
| CTR-002–003 | `pkg/workflow/*action*.go`, `tools_validation*.go`, strict-mode validation | `*action*_test.go`, `*tools*_test.go` |
| CTR-004 | sandbox validation, `enclaves.go`, `enclave_github_proxy.go` | sandbox, enclave, and proxy tests |
| CTR-005 | safe-output compiler/validation; setup safe-output and manifest helpers | safe-output and setup helper tests |
| CTR-006 | template/heredoc validation, `mcp_renderer_guard.go` | template-injection and MCP tests |
| CTR-007–008 | `markdown_security_scanner.go`, `pull_request_target_validation.go` | corresponding workflow and sanitizer tests |
| CTR-009–012 | safe-output shell/push validation, expression validation, firewall validation | corresponding workflow tests |
| CTR-013–015 | name, install-script, and allowed-label validation | `argument_injection_test.go`, corresponding validation tests |
| CTR-016–019 | safe-update, strict env/update, cache, and expression builder | corresponding enforcement, secrets, update, and cache tests |
| CTR-020 | `pkg/parser/import_bfs.go` | `pkg/parser/import_bfs_test.go` |
| CTR-021–023 | `agent_validation.go`, `agentic_engine.go`, `pkg/gitutil/gitutil.go` | workflow-run, bash-allowlist, gitutil, and download tests |
| CTR-025 | `actions/setup/js/setup_threat_detection.cjs` | `setup_threat_detection.test.cjs` |
| CTR-026 | custom-job properties and timeout resolution | custom-job and timeout tests |

### 5.1 Latest Mapping Audit (2026-09-09)

CTR-001–026 have implementation and test references with no `TODO` placeholders. The available history began at `bce650c`, with no additional compiler/parser diff in the review window. No critical alert was open. Alert #672 (`go/allocation-size-overflow`) concerns `make(map[string]any, len(tools)+1)` in `pkg/workflow/mcp_setup_generator.go`; its in-process, schema-validated map capacity is not a new compiler threat class. Other reviewed high alerts are outside conformance scope. No live suppression annotation or `SLA_BREACH` was found.

Historical audits through 2026-09-06 confirmed existing coverage or recorded mapping-only updates for CTR-001, CTR-004–007, CTR-009–012, CTR-016–021, CTR-023, and CTR-025; no audit added an uncovered threat class.

## 6. Compliance Testing

Each active rule MUST have at least one deterministic test that covers its primary trigger and stable diagnostic. Rule additions MUST add tests in the same change set; deprecated-rule tests become `[DEPRECATED]`.

| Test IDs | Rules |
|---|---|
| T-CTR-001–015 | CTR-001–015 |
| T-CTR-016–023 | CTR-016–023 |
| T-CTR-039 | CTR-025 |
| T-CTR-041 | CTR-026 |
| T-CTR-024–038, T-CTR-040 | Suppression and optimizer protocol requirements in Section 4 |

The core tests exercise their catalog trigger and assert the expected rejection, warning, rewrite, or runtime-safe output. Optimizer tests cover suppression validation/auditing/SLA/expiration; degraded API, timeout, and rate-limit handling; and missed schedules.

## 7. References

- RFC 2119: Key words for use in RFCs to Indicate Requirement Levels
- GitHub Actions syntax and permissions documentation
- gh-aw security architecture and safe-output specifications

## 8. Change Log

| Version | Change |
|---|---|
| 1.0.31 | Audit-only review; #672 is not a new threat class. |
| 1.0.30–1.0.27 | CTR-001, CTR-004, and CTR-006 mapping synchronization. |
| 1.0.26 | Added CTR-026 timeout-expression rejection. |
| 1.0.25–1.0.23 | Added CTR-025 and mapping-only audit updates. |
| 1.0.22 | Added suppression and optimizer-failure requirements. |
| 1.0.20–1.0.15 | Added CTR-022, CTR-023, and CTR-021. |
| 1.0.14–1.0.8 | Added CTR-020, CTR-019, CTR-018, CTR-017, and CTR-016. |
| 1.0.7–1.0.0 | Established CTR-001–015, conformance model, and daily reconciliation. |
