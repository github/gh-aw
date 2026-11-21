---
title: Safe Update Specification
description: Formal specification of safe update enforcement and manifest baselines in gh-aw compilation
sidebar:
  order: 1366
---

# Safe Update Specification

**Version**: 1.0.0  
**Status**: Working Draft  
**Publication Date**: 2026-07-11  
**Editor**: GitHub Agentic Workflows Team  
**This Version**: [safe-update-specification](/gh-aw/specs/safe-update-specification/)  
**Latest Published Version**: This document

---

## Abstract

This specification defines safe update behavior in GitHub Agentic Workflows compilation. It standardizes when safe update enforcement is active, how baseline manifests are loaded and trusted, which secret/action/redirect/event changes require review, how warnings are surfaced, and how approval and strict-mode settings affect enforcement.

## Status of This Document

This is a working draft and may change. It describes behavior implemented in `pkg/workflow/` and exercised by `pkg/workflow/*safe_update*` and `pkg/cli/compile_safe_update_integration_test.go`.

## Table of Contents

1. [Introduction](#1-introduction)
2. [Conformance](#2-conformance)
3. [Safe Update Activation Model](#3-safe-update-activation-model)
4. [Baseline Manifest Resolution and Trust](#4-baseline-manifest-resolution-and-trust)
5. [Violation Detection Rules](#5-violation-detection-rules)
6. [Compiler Output and Approval Flow](#6-compiler-output-and-approval-flow)
7. [Manifest Format Requirements](#7-manifest-format-requirements)
8. [Compliance Testing](#8-compliance-testing)
9. [References](#9-references)
10. [Change Log](#10-change-log)

---

## 1. Introduction

### 1.1 Purpose

This document defines normative requirements for safe update enforcement during workflow compilation so that newly introduced security-sensitive changes are surfaced for review.

### 1.2 Scope

This specification covers:

- safe update activation and disablement behavior
- baseline manifest lookup precedence and trust guarantees
- restricted secret and action change detection
- redirect and event-trigger escalation detection
- warning/prompt emission and approval workflow
- `gh-aw-manifest` content used for future comparisons

This specification does NOT cover runtime execution policy in GitHub Actions jobs.

### 1.3 Design Goals

Safe update enforcement is designed to preserve secure-by-default compilation while keeping compile output usable. The compiler SHALL produce actionable warnings and SHALL still generate lock files so subsequent compilations can compare against a baseline.

---

## 2. Conformance

### 2.1 Requirements Notation

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and "OPTIONAL" in this document are to be interpreted as described in [RFC 2119](https://www.ietf.org/rfc/rfc2119.txt).

### 2.2 Conformance Classes

- **C1 (Compiler conformance)**: Correctly activates safe update mode, resolves trusted baseline manifests, detects violations, and emits required warnings.
- **C2 (Manifest conformance)**: Emits parseable `gh-aw-manifest` metadata with deterministic normalization required for future safe update comparisons.

---

## 3. Safe Update Activation Model

### 3.1 Effective Safe Update Mode

Safe update mode MUST be enabled whenever effective strict mode is enabled and approval is not granted.

Effective strict mode MUST follow this precedence:

1. CLI strict flag (enabled)
2. Frontmatter `strict` boolean
3. Default `true` when unspecified

Safe update mode MUST be disabled when `--approve` is set, regardless of strict mode.

### 3.2 Strict-Mode Coupling

When frontmatter sets `strict: false`, safe update mode MUST be disabled and enforcement warnings MUST NOT be emitted.

---

## 4. Baseline Manifest Resolution and Trust

### 4.1 Resolution Order

When safe update mode is enabled, baseline manifest lookup MUST use this precedence:

1. Pre-cached prior manifest supplied by caller
2. Existing lock file content from git `HEAD`
3. Existing lock file content from filesystem
4. Empty non-nil manifest when no lock file exists

### 4.2 Legacy Lock Files

If a lock file exists but has no `gh-aw-manifest`, enforcement MUST be skipped for that compilation and MUST NOT produce violation warnings from missing baseline data.

### 4.3 Baseline Cache Stability

When a non-nil baseline manifest is resolved, the compiler SHOULD retain that first trusted baseline for the current compiler instance and SHOULD NOT overwrite it with just-generated local results in subsequent compiles.

---

## 5. Violation Detection Rules

### 5.1 Secret Violations

Secret names MUST be normalized by removing the `secrets.` prefix before comparison.

The following secrets MUST always be allowed even when absent from prior manifest data:

- `GITHUB_TOKEN`
- `GH_AW_GITHUB_TOKEN`
- `GH_AW_GITHUB_MCP_SERVER_TOKEN`
- `GH_AW_AGENT_TOKEN`
- `GH_AW_CI_TRIGGER_TOKEN`
- `GH_AW_PROJECT_GITHUB_TOKEN`
- `COPILOT_GITHUB_TOKEN`

Any other normalized secret name not present in the baseline manifest MUST be reported as a violation.

### 5.2 Action Violations

Action comparison MUST use repository identity (owner/repo) as the key. Pin or version changes for an already approved repository MUST NOT be treated as violations.

Compiler implementations MUST report:

- added unapproved action repositories
- removed previously approved action repositories

The following repositories MUST be treated as trusted and MUST NOT generate add/remove violations:

- `actions/*`
- `github/gh-aw/actions/*`
- `github/gh-aw-actions/*`
- runtime-manager trusted repositories defined by compiler runtime mapping

### 5.3 Redirect Violations

Redirect values MUST be whitespace-trimmed before comparison.

If redirect changes relative to baseline, the compiler MUST report:

- newly added redirect
- removed previously approved redirect
- both signals when one redirect is replaced by another

### 5.4 Event Escalation Violations

The compiler MUST report a security escalation when baseline event presence indicates `pull_request` only, and current event presence indicates `pull_request_target` only.

Adding `pull_request_target` while retaining `pull_request` MUST NOT be treated as an event escalation violation.

---

## 6. Compiler Output and Approval Flow

### 6.1 Warning-Only Enforcement

Safe update violations MUST be emitted as warnings, not hard compilation failures. Compilation MUST continue, and lock output SHOULD still be written.

### 6.2 Warning Content

Violation output MUST include:

- grouped violation details (secrets, added/removed actions, redirect changes, event escalation)
- remediation guidance that includes approval and revert options

A security-review prompt SHOULD instruct calling agents to review the flagged changes and include a review note in pull request descriptions.

### 6.3 Approval Behavior

When `--approve` is provided, safe update enforcement MUST be skipped for that compile invocation.

### 6.4 First-Compile Baseline Establishment

For workflows with no prior lock file, compilers MUST compare against an empty non-nil baseline, surface new restricted changes as warnings, and emit a lock file with `gh-aw-manifest` so future compiles have baseline state.

---

## 7. Manifest Format Requirements

### 7.1 Manifest Header

Compiled lock files MUST embed a single-line JSON manifest comment in header form:

- `# gh-aw-manifest: { ... }`

### 7.2 Required and Optional Fields

Manifest payloads MUST include `version`, `secrets`, and `actions`. Implementations MAY include additional fields such as skills, resolution failures, containers, redirect metadata, and pull request event presence flags.

### 7.3 Normalization and Determinism

Manifest generation MUST normalize and deduplicate secret/action data and SHOULD sort entries for deterministic output.

### 7.4 Transitive Coverage

Manifest content used by safe update comparisons SHOULD reflect secrets and actions contributed by imported and transitively imported workflow files.

---

## 8. Compliance Testing

### 8.1 Required Tests

- **T-SU-001**: Safe update enabled when strict mode is effective and `--approve` is not set
- **T-SU-002**: `strict: false` disables safe update warnings
- **T-SU-003**: `--approve` disables safe update enforcement regardless of strict mode
- **T-SU-004**: Baseline resolution order follows prior-manifest cache → HEAD lock → filesystem lock → empty baseline
- **T-SU-005**: Legacy lock file without `gh-aw-manifest` skips enforcement
- **T-SU-006**: New non-allowlisted secrets are reported; `GITHUB_TOKEN` and internal secrets are exempt
- **T-SU-007**: Action add/remove detection is repo-based; pin changes do not violate
- **T-SU-008**: Trusted action repos are exempt from add/remove violations
- **T-SU-009**: Redirect add/remove/change violations are reported after trim normalization
- **T-SU-010**: `pull_request` → `pull_request_target` conversion is reported as escalation
- **T-SU-011**: Violations are emitted as warnings while compilation still succeeds
- **T-SU-012**: First compile emits warning and writes baseline manifest
- **T-SU-013**: Manifest includes data from imported/transitively imported workflow content

### 8.2 Compliance Checklist

| Requirement | Test ID | Level | Status |
|---|---|---|---|
| Safe update activation/deactivation rules | T-SU-001, T-SU-002, T-SU-003 | C1 | Required |
| Baseline trust and legacy compatibility | T-SU-004, T-SU-005 | C1 | Required |
| Secret and action violation detection | T-SU-006, T-SU-007, T-SU-008 | C1/C2 | Required |
| Redirect and trigger escalation detection | T-SU-009, T-SU-010 | C1 | Required |
| Warning-only enforcement and baseline creation | T-SU-011, T-SU-012 | C1 | Required |
| Manifest completeness for imports | T-SU-013 | C2 | Required |

---

## 9. References

The [TLA+ baseline lifecycle model](https://github.com/github/gh-aw/blob/main/specs/safe-update/SafeUpdate.tla) and [TLC configuration](https://github.com/github/gh-aw/blob/main/specs/safe-update/SafeUpdate.cfg) cover two compilations in one compiler instance. The model checks baseline precedence, retention of the first trusted manifest, legacy-lock handling, and warning-only publication under effective strict mode and explicit approval. It uses symbolic manifests and one representative restricted change; it does not verify policy matching, manifest parsing, Git or filesystem trust, or who supplies `--approve`.

### Normative References

- [RFC 2119](https://www.ietf.org/rfc/rfc2119.txt)
- `pkg/workflow/compiler.go`
- `pkg/workflow/compiler_yaml.go`
- `pkg/workflow/safe_update_enforcement.go`
- `pkg/workflow/safe_update_manifest.go`
- `pkg/workflow/compiler_types.go`

### Informative References

- `pkg/workflow/safe_update_enforcement_test.go`
- `pkg/cli/compile_safe_update_integration_test.go`
- `/gh-aw/setup/cli/`

---

## 10. Change Log

### Version 1.0.0 (Working Draft)

- Initial safe update specification
- Defined strict/approve activation semantics and baseline lookup precedence
- Defined normative violation categories for secrets, actions, redirect changes, and trigger escalation
- Defined warning-only compiler behavior and manifest conformance requirements
