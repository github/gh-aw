---
title: GitHub Actions Compiler Threat Detection Specification
description: Normative requirements for compiler rules that prevent unsafe generated workflows
sidebar:
  order: 1001
---

# GitHub Actions Compiler Threat Detection Specification

**Version**: 1.0.39
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
| `1.0.38` | `v0.87.9` | Audit-only; safe-outputs workspace checkout discovery is a runtime trust boundary governed by the safe-outputs and checkout-behavior specifications, not a new CTR rule. |
| `1.0.37` | `v0.87.9` | Adds CTR-028; PR-triggered agent jobs restore agent configuration from the base branch before any step that installs agent content. |
| `1.0.36` | `v0.87.9` | Audit-only; Opengrep build-reproducibility findings (non-deterministic `npm`/`uv pip` installs, non-SHA-pinned Dockerfile image) are out of conformance scope per Section 1. |
| `1.0.35` | `v0.87.9` | Audit-only; #681/#678/#676/#675, #679, #674/#669/#668/#667, #663, #657, #652/#651, and #680 are not new threat classes. |
| `1.0.34` | `v0.87.9` | Adds CTR-027; allowlisted bot synchronization requires trusted same-repository provenance. |
| `1.0.33` | `v0.87.9` | Audit-only; no new CTR rule or lock-file schema change. |
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

Runtime trust boundaries inside the agent workspace are out of scope for this specification. In particular, the safe-outputs repository checkout discovery path (`actions/setup/js/find_repo_checkout.cjs`) resolves an `owner/repo` target from agent-writable workspace state; its normative controls — manifest precedence, workspace confinement, per-invocation `safe.directory` scoping, read-only discovery, remote host constraint, and deferred durable trust — are specified as Threat T7 and requirements RCR1–RCR7 in the Safe Outputs MCP Gateway Specification and as §3.5 of the Checkout Behavior Specification. A conforming compiler is not required to detect this class, because the compiler emits no generated-workflow construct that selects the checkout directory.

Sandbox bypass includes the provenance of the agent's own configuration. On pull-request triggers the workspace holds head-branch content that the PR author controls, so agent configuration folders and root instruction files are attacker-controlled inputs until the generated job replaces them with the base-branch snapshot captured before that content was checked out.

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
- **CTR-027 Allowlisted Bot Synchronization Provenance**: Deny bot-driven PR synchronization when the actor differs from the PR author unless the bot is explicitly allowlisted and active, the PR is from the base repository, the PR author satisfies the configured roles, and the actor is not Dependabot.
- **CTR-028 Agent Configuration Restore Provenance**: For pull-request triggers, emit the base-branch restore of every engine's agent configuration folders and root instruction files after the PR checkout and before any generated step that installs agent content or executes the agent.
- **CTR-029 Repository Dispatch App Authorization**: On `repository_dispatch`, authorize a GitHub App that is explicitly listed in `on.bots` when the collaborator lookup conclusively reports it absent; deny non-allowlisted actors, lookup failures, and App actors on every other trigger.

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
| CTR-027 Allowlisted Bot Synchronization Provenance | `actions/setup/js/check_membership.cjs`, `actions/setup/js/check_permissions_utils.cjs` | `actions/setup/js/check_membership.test.cjs`, `actions/setup/js/check_permissions_utils.test.cjs` |
| CTR-028 Agent Configuration Restore Provenance | `pkg/workflow/pr.go`, `actions/setup/sh/save_base_github_folders.sh`, `actions/setup/sh/restore_base_github_folders.sh` | `pkg/workflow/compiler_pre_agent_steps_test.go`, `actions/setup/sh/restore_base_github_folders_test.sh` |
| CTR-029 Repository Dispatch App Authorization | `actions/setup/js/check_membership.cjs`, `actions/setup/js/check_permissions_utils.cjs` | `actions/setup/js/check_membership.test.cjs` |

### 7.2 Mapping Audit History

Dated mapping-audit records are maintained in `specs/compiler-threat-detection-changelog.md`. A daily optimizer run MUST append its audit entry to that changelog, not to this specification.

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
| **T-CTR-042** | CTR-027 Allowlisted Bot Synchronization Provenance | An allowlisted bot synchronizes a PR authored by another actor | Authorize only when the bot, repository provenance, and PR author satisfy all trust requirements; otherwise deny with `confused_deputy` or `bot_not_active`. | `CTR-027` |
| **T-CTR-043** | CTR-028 Agent Configuration Restore Provenance | A pull-request-triggered workflow generates an agent job that installs agent content through `steps:` or `pre-agent-steps:` | Emit the base-branch restore of the engine's agent configuration folders and root instruction files after the PR checkout and before any agent-content install step and the agent execution step. | `CTR-028` |
| **T-CTR-044** | CTR-029 Repository Dispatch App Authorization | An allowlisted GitHub App sends `repository_dispatch` and the collaborator lookup reports it absent | Authorize only the allowlisted App on `repository_dispatch`; deny non-allowlisted actors, lookup failures, and all other triggers. | `CTR-029` |

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

The version history for this specification is maintained in `specs/compiler-threat-detection-changelog.md`. A version change MUST add an entry there in the same pull request that updates Section 2.
