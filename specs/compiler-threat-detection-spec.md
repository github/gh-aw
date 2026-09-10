---
title: GitHub Actions Compiler Threat Detection Specification
description: Normative requirements for compiler rules that prevent unsafe generated workflows
sidebar:
  order: 1001
---

# GitHub Actions Compiler Threat Detection Specification

**Version**: 1.0.32
**Status**: Candidate Recommendation  
**Latest Version**: https://github.com/github/gh-aw/blob/main/specs/compiler-threat-detection-spec.md  
**Editors**: GitHub Next (GitHub, Inc.)

## Abstract

This specification is the source of truth for compiler-side threat detection in GitHub Agentic Workflows (gh-aw). A conforming implementation detects unsafe generated GitHub Actions behavior before runtime and keeps rule definitions, implementations, tests, and daily review evidence synchronized.

## Status

The gh-aw maintainers revise this Candidate Recommendation using security-review and conformance evidence. Requirement keywords use [RFC 2119](https://www.rfc-editor.org/rfc/rfc2119).

## 1. Scope and Conformance

This specification covers `pkg/workflow/`, related `pkg/parser/` and `actions/setup/` validation, and the daily optimizer. It excludes runtime detection-job internals, external scanner ecosystems, and non-compiler repositories.

A conforming implementation satisfies every MUST in Sections 2–8. Rules are specification-first, secure by default, mapped bidirectionally to implementation, and auditable through version history.

## 2. Spec-to-Implementation Sync

Each version maps to the minimum compatible binary. A version change MUST update this table and any lock-file compatibility change in the same pull request.

| Versions | Minimum gh-aw | Compatibility |
|---|---:|---|
| `1.0.32` | `v0.87.9` | Audit-only; no new CTR rule or lock-file schema change. |
| `1.0.31` | `v0.87.9` | Audit-only; no new CTR rule or lock-file schema change. |
| `1.0.30` | `v0.87.9` | CTR-001 status-function guard mapping update only. |
| `1.0.29` | `v0.87.9` | CTR-004 Playwright renderer mapping update only. |
| `1.0.27`–`1.0.28` | `v0.87.4` | CTR-004 enclave and CTR-006 guard-renderer mapping updates only. |
| `1.0.26` | `v0.87.4` | Adds CTR-026; generated job timeouts are literal positive integers. |
| `1.0.23`–`1.0.25` | `v0.87.1` | Adds CTR-025; mapping-only audit updates otherwise. |
| `1.0.22` | `v0.87.0` | Adds suppression manifest and optimizer safeguards. |
| `1.0.15`–`1.0.21` | `v0.72.1`–`v0.83.6` | Adds CTR-021–023 and editorial/mapping updates. |
| `1.0.8`–`1.0.14` | `v0.72.1` | Establishes CTR-016, CTR-018–020; earlier versions establish CTR-001–015. |

## 3. Threat Model Overview

Generated workflows run with elevated permissions and consume untrusted content (issues, PRs, comments, external tool output). The catalog in Section 5 groups the threats a conforming compiler MUST detect into five classes: unauthorized privilege or scope expansion, unsafe or bypassed sandboxing, injection (template, shell, or subprocess argument), unsafe output and supply-chain routes, and compile-time drift between manifests, mappings, and the rules that reference them. Each class maps to one or more `CTR-*` rules with a stable trigger and compiler action.

## 4. Governance and Responsibilities

Maintainers own the rule catalog, its implementation mapping, and its test coverage; the daily optimizer (Section 6) reviews new candidate threats and reports coverage gaps. Every rule addition, deprecation, or mapping change MUST be reviewed against this specification in the same pull request that changes the implementation, per the Spec-to-Implementation Sync table in Section 2.

## 5. Rule Model and Requirements

Each rule has a stable `CTR-*` ID, threat class, trigger, compiler action, diagnostic, implementation mapping, and test.

### 5.1 Core Rule Catalog

- **CTR-001 Privilege Escalation**: Reject unauthorized generated-job write permissions.
- **CTR-002 Unpinned Action Integrity**: Reject unpinned action references in strict contexts.
- **CTR-003 Unsafe Tool Scope Expansion**: Reject or warn on policy-violating wildcard or overbroad tool scope.
- **CTR-004 Sandbox Bypass Configuration**: Reject generated configuration that disables required sandboxing.
- **CTR-005 Unsafe Output Route**: Reject direct write paths that bypass safe outputs.
- **CTR-006 Template Injection**: Reject user-controlled expressions directly embedded in shell commands.
- **CTR-007 Markdown Content Security**: Detect unsafe external markdown, including obfuscation, scripts, and social engineering.
- **CTR-008 Pull Request Target Safety**: Reject unsafe `pull_request_target` checkout patterns.
- **CTR-009 Shell Expansion in Safe-Outputs**: Reject dangerous shell expansion in safe-output scripts.
- **CTR-010 Expression Safety Allowlist**: Reject unauthorized or multiline GitHub Actions expressions.
- **CTR-011 Network Firewall Configuration**: Reject missing firewall prerequisites and strict-mode wildcard domains.
- **CTR-012 Safe-Outputs Wildcard Push Scope**: Warn for unconstrained wildcard PR-branch pushes.
- **CTR-013 Argument Injection via Package/Image Names**: Reject hyphen-prefixed package and image names before subprocess use.
- **CTR-014 Supply Chain Attack via Install Scripts**: Warn, or reject in strict mode, when Node install scripts are enabled.
- **CTR-015 Allowed Label Glob Scope**: Reject bare `*` safe-output allowed-label patterns.
- **CTR-016 Compile-Time Manifest Drift**: Reject new restricted secrets or action references absent from an existing manifest.
- **CTR-017 Secret Leakage via Environment Variables**: Warn, or reject in strict mode, for uncontrolled secret-expression placement.
- **CTR-018 Version Integrity Bypass**: Warn, or reject in strict mode, for `check-for-updates: false`.
- **CTR-019 Cache-Memory Integrity Enforcement**: Require cache updates only after successful agent and threat-detection jobs.
- **CTR-020 Conditional Import Security**: Reject `imports` entries containing `if`.
- **CTR-021 Workflow Run Trigger Branch Scope**: Warn, or reject in strict mode, for unscoped `workflow_run`; always reject missing `workflows`.
- **CTR-022 Git Subprocess Argument Injection**: Reject unsafe remote ref/path arguments before invoking Git.
- **CTR-023 Bash Command Allowlist Illusion**: Reject explicit bash restrictions for engines that cannot enforce them.
- **CTR-025 Framework Self-Prompt Misattribution**: Strip only a leading framework `<system>` block before analysis.
- **CTR-026 Generated Job Timeout Expression Injection**: Reject non-positive or expression job timeout values.

### 5.2 Compiler Response Requirements

The compiler MUST produce deterministic, actionable diagnostics and either reject insecure generation or apply a safe rewrite for every active rule in Section 5.1.

### 5.3 Candidacy and Lifecycle

When a threat is found, maintainers MUST add its mapping and test if covered, or implement detection, tests, and documentation if not. Experimental threats MUST NOT fail production compilation. Candidates need a trigger, action, stable diagnostic, test, deployment evidence, and security-maintainer review before becoming normative.

### 5.4 Deprecation Policy

Removing a rule dependency MUST deprecate—not delete—the catalog and mapping rows in the same change set. The catalog records the version and reason; mapped tests become `[DEPRECATED]`; the mapping implementation cell is cleared; and the changelog records the retirement. `TestFormal_DeprecationPolicy_SpecArtifactsConform` enforces this policy.

## 6. Daily Optimizer Protocol

The daily optimizer MUST review recent compiler changes, related validation paths, open/recent security findings, and this catalog. For each candidate threat, it MUST determine coverage, update the mapping/tests if covered, or implement, test, and document remediation if uncovered. Its output MUST be either a pull request or an explicit noop report.

### 6.1 Suppressions

`threat-detection-suppress` entries MUST provide a rule and non-empty reason; `expires` is optional ISO 8601. Active entries MUST retain rule, reason, and expiry in the lock-file manifest. Expired entries do not suppress a rule.

The optimizer SHOULD resolve false positives affecting non-strict rejection controls within 10 business days. It MUST report older suppressions as `SLA_BREACH` with rule, reason, age, owner, and expiry, and MUST create a follow-up action after 20 business days. The mechanically-verified norms for suppression handling are cataloged as `T-CTR-024`–`T-CTR-029` in `specs/compiler-threat-detection-compliance/README.md` (Section 6.4 False-Positive Handling Norms).

### 6.2 Failure Safeguards

| Failure | Required behavior |
|---|---|
| API unavailable | Retry with bounded exponential backoff (10 seconds to 5 minutes, three attempts); then emit `OPTIMIZER_DEGRADED` with endpoint, error, and UTC time. Do not create artifacts from incomplete data. |
| Timeout | Emit `OPTIMIZER_TIMEOUT` with completed step and unevaluated rules; discard partial artifacts. Use an explicit timeout and request same-day retry. |
| Rate limit | Apply `RATE_LIMIT_RETRY_CONFIG`; after exhaustion emit `OPTIMIZER_RATE_LIMITED` with endpoint and retry metadata. Count neither completion nor noop; retry next window. |
| Missed schedule | Emit `OPTIMIZER_MISSED_CRON` with scheduled and detected times and lookback; do not count completion and create a follow-up action. |

The mechanically-verified norms for these safeguards are cataloged as `T-CTR-030`–`T-CTR-038`, `T-CTR-040` in `specs/compiler-threat-detection-compliance/README.md` (Section 6.6 Optimizer Failure Safeguards).

## 7. Implementation Mapping

Every active rule MUST map to implementation and test coverage. References are patterns and MUST be verified against concrete paths whenever changed.

### 7.1 Baseline Rule Mapping

| Rule ID | Primary Implementation Areas | Test Coverage Targets |
|---------|------------------------------|-----------------------|
| CTR-001 Privilege Escalation | `pkg/workflow/*permissions*validation*.go`, `compiler_builtin_job_augmentation.go` | `*permissions*_test.go`, `compiler_custom_jobs_test.go` |
| CTR-002 Unpinned Action Integrity | `pkg/workflow/*action*.go`, `tools_validation*.go`, strict-mode validation | `*action*_test.go`, `*tools*_test.go` |
| CTR-003 Unsafe Tool Scope Expansion | `pkg/workflow/*action*.go`, `tools_validation*.go`, strict-mode validation | `*action*_test.go`, `*tools*_test.go` |
| CTR-004 Sandbox Bypass Configuration | sandbox validation, `enclaves.go`, `enclave_github_proxy.go` | sandbox, enclave, and proxy tests |
| CTR-005 Unsafe Output Route | safe-output compiler/validation; setup safe-output and manifest helpers | safe-output and setup helper tests |
| CTR-006 Template Injection | template/heredoc validation, `mcp_renderer_guard.go` | template-injection and MCP tests |
| CTR-007 Markdown Content Security | `markdown_security_scanner.go`, `pull_request_target_validation.go` | corresponding workflow and sanitizer tests |
| CTR-008 Pull Request Target Safety | `markdown_security_scanner.go`, `pull_request_target_validation.go` | corresponding workflow and sanitizer tests |
| CTR-009 Shell Expansion in Safe-Outputs | safe-output shell/push validation, expression validation, firewall validation | corresponding workflow tests |
| CTR-010 Expression Safety Allowlist | safe-output shell/push validation, expression validation, firewall validation | corresponding workflow tests |
| CTR-011 Network Firewall Configuration | safe-output shell/push validation, expression validation, firewall validation | corresponding workflow tests |
| CTR-012 Safe-Outputs Wildcard Push Scope | safe-output shell/push validation, expression validation, firewall validation | corresponding workflow tests |
| CTR-013 Argument Injection via Package/Image Names | name, install-script, and allowed-label validation | `argument_injection_test.go`, corresponding validation tests |
| CTR-014 Supply Chain Attack via Install Scripts | name, install-script, and allowed-label validation | `argument_injection_test.go`, corresponding validation tests |
| CTR-015 Allowed Label Glob Scope | name, install-script, and allowed-label validation | `argument_injection_test.go`, corresponding validation tests |
| CTR-016 Compile-Time Manifest Drift | safe-update, strict env/update, cache, and expression builder | corresponding enforcement, secrets, update, and cache tests |
| CTR-017 Secret Leakage via Environment Variables | safe-update, strict env/update, cache, and expression builder | corresponding enforcement, secrets, update, and cache tests |
| CTR-018 Version Integrity Bypass | safe-update, strict env/update, cache, and expression builder | corresponding enforcement, secrets, update, and cache tests |
| CTR-019 Cache-Memory Integrity Enforcement | safe-update, strict env/update, cache, and expression builder | corresponding enforcement, secrets, update, and cache tests |
| CTR-020 Conditional Import Security | `pkg/parser/import_bfs.go` | `pkg/parser/import_bfs_test.go` |
| CTR-021 Workflow Run Trigger Branch Scope | `agent_validation.go`, `agentic_engine.go`, `pkg/gitutil/gitutil.go` | workflow-run, bash-allowlist, gitutil, and download tests |
| CTR-022 Git Subprocess Argument Injection | `agent_validation.go`, `agentic_engine.go`, `pkg/gitutil/gitutil.go` | workflow-run, bash-allowlist, gitutil, and download tests |
| CTR-023 Bash Command Allowlist Illusion | `agent_validation.go`, `agentic_engine.go`, `pkg/gitutil/gitutil.go` | workflow-run, bash-allowlist, gitutil, and download tests |
| CTR-025 Framework Self-Prompt Misattribution | `actions/setup/js/setup_threat_detection.cjs` | `setup_threat_detection.test.cjs` |
| CTR-026 Generated Job Timeout Expression Injection | custom-job properties and timeout resolution | custom-job and timeout tests |

### 7.2 Mapping Audit (2026-09-10)

CTR-001–026 have implementation and test references with no `TODO` placeholders. The available repository history is a single squashed commit (`099efdd`, dated 2026-09-09 17:19 -0700); no additional compiler/parser diff exists beyond that state, so no new candidate threat surfaced from source changes. No live `threat-detection-suppress` annotation exists in any workflow frontmatter (only illustrative documentation examples in `.github/aw/syntax-agentic.md` and reference docs), so no `SLA_BREACH` applies.

Open high/critical code-scanning alerts were reviewed against conformance scope: alert #677 (`go/allocation-size-overflow` in `pkg/workflow/mcp_setup_generator.go`) was already assessed in the 2026-09-09 audit as an in-process, schema-validated map capacity computation, not a new compiler threat class. Alerts #675–#676 (`go/allocation-size-overflow` in `pkg/workflow/mcp_github_config.go`) are the same class of finding — capacity hints computed from already-bounded, schema-validated slice/map lengths (`dynamicEnclaveGitHubGuardRepos`, `staticEnclaveGitHubGuardPolicies`) — and are not exploitable by untrusted input; no CTR rule applies. Alerts #667–#669 and #674 (`go/bad-redirect-check`) flag `strings.HasPrefix` path-containment guards in `pkg/cli/add_package_manifest_imports.go`, `pkg/cli/add_package_manifest_includes.go`, and `pkg/workflow/graders_config.go`; these are manifest path-traversal guards, not HTTP redirect validators, and already carry in-code rationale comments explaining the CodeQL heuristic mismatch. Other reviewed alerts (`js/http-to-file-access`, `workflow-security-finding-1`, `workflow-out-of-context`, `workflow-go-graphql-injection-sprintf`) are outside `pkg/workflow/`, `pkg/parser/`, and `actions/setup/` conformance scope per Section 1.

Historical audits through 2026-09-09 confirmed existing coverage or recorded mapping-only updates for CTR-001, CTR-004–007, CTR-009–012, CTR-016–021, CTR-023, and CTR-025; no audit added an uncovered threat class.

## 8. Compliance Testing

Each active rule MUST have at least one deterministic test that covers its primary trigger and stable diagnostic. Rule additions MUST add tests in the same change set; deprecated-rule tests become `[DEPRECATED]`.

### 8.1 Test ID Catalog

| Test ID | Rule | Detection Trigger | Expected Compiler Action | Stable Diagnostic ID |
|---------|------|--------------------|---------------------------|----------------------|
| **T-CTR-001** | CTR-001 Privilege Escalation | Reject unauthorized generated-job write permissions | Reject unauthorized generated-job write permissions. | `CTR-001` |
| **T-CTR-002** | CTR-002 Unpinned Action Integrity | Reject unpinned action references in strict contexts | Reject unpinned action references in strict contexts. | `CTR-002` |
| **T-CTR-003** | CTR-003 Unsafe Tool Scope Expansion | Reject or warn on policy-violating wildcard or overbroad tool scope | Reject or warn on policy-violating wildcard or overbroad tool scope. | `CTR-003` |
| **T-CTR-004** | CTR-004 Sandbox Bypass Configuration | Reject generated configuration that disables required sandboxing | Reject generated configuration that disables required sandboxing. | `CTR-004` |
| **T-CTR-005** | CTR-005 Unsafe Output Route | Reject direct write paths that bypass safe outputs | Reject direct write paths that bypass safe outputs. | `CTR-005` |
| **T-CTR-006** | CTR-006 Template Injection | Reject user-controlled expressions directly embedded in shell commands | Reject user-controlled expressions directly embedded in shell commands. | `CTR-006` |
| **T-CTR-007** | CTR-007 Markdown Content Security | Detect unsafe external markdown, including obfuscation, scripts, and social engineering | Detect unsafe external markdown, including obfuscation, scripts, and social engineering. | `CTR-007` |
| **T-CTR-008** | CTR-008 Pull Request Target Safety | Reject unsafe `pull_request_target` checkout patterns | Reject unsafe `pull_request_target` checkout patterns. | `CTR-008` |
| **T-CTR-009** | CTR-009 Shell Expansion in Safe-Outputs | Reject dangerous shell expansion in safe-output scripts | Reject dangerous shell expansion in safe-output scripts. | `CTR-009` |
| **T-CTR-010** | CTR-010 Expression Safety Allowlist | Reject unauthorized or multiline GitHub Actions expressions | Reject unauthorized or multiline GitHub Actions expressions. | `CTR-010` |
| **T-CTR-011** | CTR-011 Network Firewall Configuration | Reject missing firewall prerequisites and strict-mode wildcard domains | Reject missing firewall prerequisites and strict-mode wildcard domains. | `CTR-011` |
| **T-CTR-012** | CTR-012 Safe-Outputs Wildcard Push Scope | Warn for unconstrained wildcard PR-branch pushes | Warn for unconstrained wildcard PR-branch pushes. | `CTR-012` |
| **T-CTR-013** | CTR-013 Argument Injection via Package/Image Names | Reject hyphen-prefixed package and image names before subprocess use | Reject hyphen-prefixed package and image names before subprocess use. | `CTR-013` |
| **T-CTR-014** | CTR-014 Supply Chain Attack via Install Scripts | Warn, or reject in strict mode, when Node install scripts are enabled | Warn, or reject in strict mode, when Node install scripts are enabled. | `CTR-014` |
| **T-CTR-015** | CTR-015 Allowed Label Glob Scope | Reject bare `*` safe-output allowed-label patterns | Reject bare `*` safe-output allowed-label patterns. | `CTR-015` |
| **T-CTR-016** | CTR-016 Compile-Time Manifest Drift | Reject new restricted secrets or action references absent from an existing manifest | Reject new restricted secrets or action references absent from an existing manifest. | `CTR-016` |
| **T-CTR-017** | CTR-017 Secret Leakage via Environment Variables | Warn, or reject in strict mode, for uncontrolled secret-expression placement | Warn, or reject in strict mode, for uncontrolled secret-expression placement. | `CTR-017` |
| **T-CTR-018** | CTR-018 Version Integrity Bypass | Warn, or reject in strict mode, for `check-for-updates: false` | Warn, or reject in strict mode, for `check-for-updates: false`. | `CTR-018` |
| **T-CTR-019** | CTR-019 Cache-Memory Integrity Enforcement | Require cache updates only after successful agent and threat-detection jobs | Require cache updates only after successful agent and threat-detection jobs. | `CTR-019` |
| **T-CTR-020** | CTR-020 Conditional Import Security | Reject `imports` entries containing `if` | Reject `imports` entries containing `if`. | `CTR-020` |
| **T-CTR-021** | CTR-021 Workflow Run Trigger Branch Scope | Warn, or reject in strict mode, for unscoped `workflow_run`; always reject missing `workflows` | Warn, or reject in strict mode, for unscoped `workflow_run`; always reject missing `workflows`. | `CTR-021` |
| **T-CTR-022** | CTR-022 Git Subprocess Argument Injection | Reject unsafe remote ref/path arguments before invoking Git | Reject unsafe remote ref/path arguments before invoking Git. | `CTR-022` |
| **T-CTR-023** | CTR-023 Bash Command Allowlist Illusion | Reject explicit bash restrictions for engines that cannot enforce them | Reject explicit bash restrictions for engines that cannot enforce them. | `CTR-023` |
| **T-CTR-039** | CTR-025 Framework Self-Prompt Misattribution | Strip only a leading framework `<system>` block before analysis | Strip only a leading framework `<system>` block before analysis. | `CTR-025` |
| **T-CTR-041** | CTR-026 Generated Job Timeout Expression Injection | Reject non-positive or expression job timeout values | Reject non-positive or expression job timeout values. | `CTR-026` |

The core tests exercise their catalog trigger and assert the expected rejection, warning, rewrite, or runtime-safe output.

### 8.2 Optimizer Protocol Test ID Catalog

| Test IDs | Rules |
|---|---|
| T-CTR-024–038, T-CTR-040 | Suppression and optimizer protocol requirements in Section 6 |

Optimizer tests cover suppression validation/auditing/SLA/expiration; degraded API, timeout, and rate-limit handling; and missed schedules. The concrete norm-to-test mapping is maintained in `specs/compiler-threat-detection-compliance/README.md` (Section 6.4 and Section 6.6 norm tables) and enforced against `pkg/workflow/compiler_threat_optimizer_protocol_test.go` by `TestFormal_ComplianceReadmeNormTestNamesStaySynced`.

### 8.3 Deprecated Test ID Retirement

A test ID that is deprecated under Section 5.4 MUST remain listed in Section 8.1, marked `[DEPRECATED]`, but MUST leave the required conformance gate: `requiredTestIDs()` excludes any row marked `[DEPRECATED]`, so a deprecated rule's test is retained for audit history without being required to pass.

## 9. References

- RFC 2119: Key words for use in RFCs to Indicate Requirement Levels
- GitHub Actions syntax and permissions documentation
- gh-aw security architecture and safe-output specifications

## 10. Change Log

| Version | Change |
|---|---|
| 1.0.32 | Audit-only review; #675–677 and #667–669/#674 are not new threat classes. |
| 1.0.31 | Audit-only review; #672 is not a new threat class. |
| 1.0.30–1.0.27 | CTR-001, CTR-004, and CTR-006 mapping synchronization. |
| 1.0.26 | Added CTR-026 timeout-expression rejection. |
| 1.0.25–1.0.23 | Added CTR-025 and mapping-only audit updates. |
| 1.0.22 | Added suppression and optimizer-failure requirements. |
| 1.0.20–1.0.15 | Added CTR-022, CTR-023, and CTR-021. |
| 1.0.14–1.0.8 | Added CTR-020, CTR-019, CTR-018, CTR-017, and CTR-016. |
| 1.0.7–1.0.0 | Established CTR-001–015, conformance model, and daily reconciliation. |
