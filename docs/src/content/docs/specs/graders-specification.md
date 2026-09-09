---
title: Graders Specification
description: Formal specification for deterministic gh-aw graders and grader-backed experiment metrics
sidebar:
  order: 1360
---

# Graders Specification

**Version**: 0.2.0
**Status**: Draft Specification
**Feature Status**: Experimental
**Latest Version**: [graders-specification](/gh-aw/specs/graders-specification/)
**Editor**: GitHub Agentic Workflows Team

---

## Abstract

This specification defines the `graders` feature in gh-aw: deterministic execution metrics and operational value observations persisted as structured artifacts. It specifies configuration, built-in grader behavior, custom inline grader constraints, operational-value grader behavior, execution ordering, artifact outputs, historical regrading, experiment metric references, and conformance requirements.

## Status of This Document

This section describes the status of this document at the time of publication. This is a draft specification and may be updated, replaced, or made obsolete by other documents at any time.

This feature is experimental and implementations SHOULD expect iteration before final recommendation status.

## Table of Contents

1. [Introduction](#1-introduction)
2. [Conformance](#2-conformance)
3. [Architecture](#3-architecture)
4. [Configuration Model](#4-configuration-model)
5. [Built-in Graders](#5-built-in-graders)
6. [Custom Inline Graders](#6-custom-inline-graders)
7. [Operational Value Grader](#7-operational-value-grader)
8. [Execution and Artifacts](#8-execution-and-artifacts)
9. [Experiment Metric References](#9-experiment-metric-references)
10. [Security and Isolation](#10-security-and-isolation)
11. [Compliance Testing](#11-compliance-testing)
12. [Norms](#12-norms)
13. [References](#13-references)
14. [Change Log](#14-change-log)

---

## 1. Introduction

### 1.1 Purpose

The `graders` feature provides deterministic execution metrics and operational value observations without issuing additional LLM calls.

### 1.2 Scope

This specification covers:

- Frontmatter configuration under `graders`
- Built-in grader identifiers and semantics
- Custom inline grader script requirements
- Operational-value evaluator and replay requirements
- Output artifact contracts
- Experiment metric integration for grader references

This specification does NOT cover:

- Non-deterministic evaluator models
- UI visualization requirements
- External metric backends

### 1.3 Design Goals

A conforming implementation:

1. MUST compute grader values deterministically for the same inputs and evidence cutoff.
2. MUST preserve stable grader IDs for experiment references.
3. MUST keep trace grading isolated from network-dependent behavior.
4. MUST emit machine-readable grader artifacts for downstream tooling.

---

## 2. Conformance

### 2.1 Conformance Classes

- **Conforming implementation**: Satisfies all MUST/SHALL requirements in this document.
- **Partially conforming implementation**: Supports built-in graders but omits custom inline grader execution.

### 2.2 Requirements Notation

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and "OPTIONAL" in this document are to be interpreted as described in [RFC 2119](https://www.ietf.org/rfc/rfc2119.txt).

### 2.3 Compliance Levels

- **Level 1 (Required)**: Built-in graders, manifest/results output.
- **Level 2 (Standard)**: Custom inline graders with validation and isolation.
- **Level 3 (Complete)**: Experiment metric references to graders with validation.

---

## 3. Architecture

`graders` executes as a post-agent step in the existing `agent` job:

1. Parse and validate frontmatter `graders`.
2. Build grader manifest and execution spec.
3. Preprocess trace artifacts once.
4. Execute enabled graders (built-in, custom inline, and operational value).
5. Write normalized outputs to grader artifact files.

The grading step MUST run with `if: always()` semantics and SHOULD continue even when individual graders fail, recording per-grader errors in results.

---

## 4. Configuration Model

### 4.1 Frontmatter Key

The configuration key MUST be `graders`.

### 4.2 Enable/Disable Semantics

- If `graders` is omitted, grading MUST be disabled.
- If `graders: {}` is provided, all built-in graders MUST be enabled with defaults.
- If `graders` is present, at least one grader MUST be enabled; otherwise configuration MUST fail.

### 4.3 Entry Model

`graders` is a map of `<grader-id> -> <definition|null>`.

- Built-in grader entries MAY be `null` to enable defaults.
- Custom grader entries MUST be objects and MUST include `script`.
- The reserved `operational-value` entry MUST be an object and MUST include `run`.

Supported object fields include `enabled`, `name`, `description`, `unit`, `direction`, `threshold`, `min`, `max`, `config`, `script`, and `run`. Only the reserved `operational-value` grader accepts `run`.

---

## 5. Built-in Graders

The implementation MUST recognize the following built-in grader IDs:

- `tool-success-rate`
- `tool-failure-count`
- `retries`
- `loops`
- `trajectory-efficiency`
- `execution-step-count`
- `execution-duration`
- `working-set-rebuild-factor`
- `context-growth`
- `artifact-production`

These IDs are reserved for built-ins. A built-in grader MUST NOT accept a custom `script`.

---

## 6. Custom Inline Graders

A custom grader is any grader ID not in the built-in set.

### 6.1 Required Fields

A custom grader MUST define `script`.

### 6.2 Script Limits

- `script` MUST be non-empty.
- `script` MUST NOT exceed 4096 characters.

### 6.3 Forbidden Patterns

Inline scripts MUST be rejected if they contain any forbidden pattern, including:

- `require(`
- `import(`
- `import `
- `fetch(`
- `eval(`
- `process.exit`
- `child_process`
- `execSync`
- `spawnSync`
- `Function(`

---

## 7. Operational Value Grader

The reserved grader ID MUST be `operational-value`. It MUST NOT accept an inline `script`.

The compiler MUST resolve `run` within the repository, reject symlinks and non-regular files, validate Bash syntax prerequisites, freeze the evaluator bytes, and record their SHA-256 digest in the grader manifest and result implementation.

The evaluator MUST implement `--definition` and `--grade-run`. Its primary `value` MUST be absolute operational attainment in `[0,1]` or `null`. A baseline MAY be frozen separately; gh-aw MUST derive `deltaFromBaseline` and MUST NOT replace the primary value with that delta.

An operational-value observation MUST include:

- the complete workflow run subject and run attempt;
- a stable opportunity key and replayable operational case;
- requested evidence time, effective evidence cutoff, and maturity time;
- accepted evidence provenance for every numeric value.

The effective evidence cutoff MUST NOT follow either the requested evidence time or the maturity time. A replayed observation MUST be identified by `(runId, evaluatorDigest, evidenceAt)`.

Historical regrading MUST reuse the original case, run subject, and archived evaluator. It MUST verify that the archived evaluator matches the digest recorded by both the original manifest and result and the evaluator at the recorded commit in a trusted local checkout before execution. It MUST emit a new observation and MUST NOT mutate the original run artifact.

A historical report MUST apply one current evaluator digest to every included run so observations remain comparable. It MUST discover completed runs no earlier than the contract's adoption time. For runs without grader artifacts, it MUST provide the available workflow run subject and a null case and event, allowing the evaluator to reconstruct assignment from accepted evidence. It MUST preserve unavailable and failed observations and MUST NOT coerce missing evidence to zero.

Historical report caches MUST be partitioned by repository, workflow ID, evaluator digest, and UTC week. Only mature numeric observations MAY be treated as final cache entries. Report output MUST retain every run-level observation. Aggregates MUST NOT treat repeated opportunity keys within one week as independent observations; the latest observation for each key in that week is used for the weekly mean.

---

## 8. Execution and Artifacts

### 8.1 Output Directory

Graders output MUST be written under:

`/tmp/gh-aw/agent/graders`

### 8.2 Required Files

The implementation MUST produce:

- `grader_manifest.json`
- `grader_results.json`
- `operational_value_evaluator.sh` when the `operational-value` grader is enabled

### 8.3 Artifact Inclusion

All applicable files MUST be included in the unified `agent` artifact.

### 8.4 Deterministic Output Contract

`grader_results.json` SHOULD include normalized run/result structures suitable for downstream programmatic reads, including per-grader value/status and run-level pass/fail/error counts.

---

## 9. Experiment Metric References

Experiment metric fields MAY reference grader outputs.

Supported forms include:

- `grader:<id>`
- `graders.<id>.value`

When a grader reference is used, `<id>` MUST resolve to a declared enabled grader. Unknown or empty grader references MUST fail validation.

For each production run, the selected experiment variant is recorded before agent execution. The
post-agent grader result then becomes an observation attributed to that assignment:

```text
production run
→ grader result
→ experiment observation
→ statistical analysis
→ deterministic decision
```

`gh aw experiments analyze` reads the persisted assignment ledger and the run's
`grader_results.json`; this path does not require historical trace replay or an additional evaluator
model invocation. Only valid numeric grader results count toward `min_samples`. Missing artifacts,
missing or failed graders, and invalid values are excluded rather than treated as zero.

Grader `direction` determines whether lower or higher values are favorable. The experiment decision
layer normalizes this direction before applying its absolute `minimum_effect` policy and mandatory
guardrails. A grader measures only the behavior encoded by that grader; not every grader represents
semantic task correctness. The normative readiness, decision, and JSON contracts are defined in the
[Experiments Specification](/gh-aw/experimental/experiments-specification/#116-deterministic-decision-policy).

---

## 10. Security and Isolation

- Grading MUST operate on local run artifacts and MUST NOT require outbound network access for built-ins.
- Custom inline graders MUST execute in a restricted context with blocked dangerous primitives.
- Operational-value graders MAY access declared repository evidence using `GH_TOKEN`; implementations MUST NOT add agent-job permission scopes on behalf of the evaluator, and evaluators MUST NOT receive workflow secrets.
- Historical regrading MUST verify archived evaluator bytes against both digest records and a trusted local checkout at the recorded commit before execution.
- Implementations SHOULD enforce bounded execution time for inline scripts.
- Implementations SHOULD redact grader outputs when custom scripts are enabled to reduce secret leakage risk.

---

## 11. Compliance Testing

### 11.1 Test Suite Requirements

- **T-GRD-001**: Omitted `graders` key disables grading step emission.
- **T-GRD-002**: `graders: {}` enables all built-ins.
- **T-GRD-003**: Unknown custom grader without `script` is rejected.
- **T-GRD-004**: Custom `script` over 4096 chars is rejected.
- **T-GRD-005**: Forbidden script patterns are rejected.
- **T-GRD-006**: Built-in grader with `script` is rejected.
- **T-GRD-007**: `grader_manifest.json` is written to required path.
- **T-GRD-008**: `grader_results.json` is written to required path.
- **T-GRD-009**: Grader files are present in `agent` artifact.
- **T-GRD-010**: `experiments.*.metric` with `grader:<id>` validates declared enabled grader.
- **T-GRD-011**: `experiments.*.metric` with `graders.<id>.value` validates declared enabled grader.
- **T-GRD-012**: `graders.operational-value.run` is frozen and its digest is recorded.
- **T-GRD-013**: Operational-value output, evidence cutoff, maturity, and provenance are validated.
- **T-GRD-014**: Historical regrading rejects evaluator or run identity mismatches.

### 11.2 Compliance Checklist

| Requirement | Test ID | Level | Status |
|---|---|---|---|
| Frontmatter key is `graders` | T-GRD-001 | 1 | Required |
| Empty map enables built-ins | T-GRD-002 | 1 | Required |
| Custom graders require script | T-GRD-003 | 2 | Required |
| Script safety constraints enforced | T-GRD-004, T-GRD-005 | 2 | Required |
| Required artifact files emitted | T-GRD-007, T-GRD-008 | 1 | Required |
| Experiment grader references validate | T-GRD-010, T-GRD-011 | 3 | Required |
| Operational-value evaluators and observations validate | T-GRD-012, T-GRD-013 | 2 | Required |
| Historical regrading preserves identity | T-GRD-014 | 2 | Required |

---

## 12. Norms

- **N-GRD-001**: Implementations MUST treat `graders` as experimental.
- **N-GRD-002**: Implementations MUST preserve built-in grader ID stability across patch releases.
- **N-GRD-003**: Implementations SHOULD preserve deterministic output for identical trace inputs.
- **N-GRD-004**: Implementations MUST fail fast on invalid custom grader scripts.
- **N-GRD-005**: Implementations MUST keep grader artifact paths stable unless a major version change is issued.
- **N-GRD-006**: Implementations MUST keep operational value separate from execution quality metrics.

---

## 13. References

### Normative References

- **[RFC 2119]** Key words for use in RFCs to Indicate Requirement Levels.  
  https://www.ietf.org/rfc/rfc2119.txt

### Informative References

- **[Graders Reference]** [Graders](/gh-aw/experimental/trace-graders/)
- **[Experiments Specification]** [Experiments Specification](/gh-aw/experimental/experiments-specification/)

---

## 14. Change Log

### Version 0.2.0 (Draft Specification)

- Defines the operational value grader and absolute attainment semantics.
- Defines evidence-bounded historical regrading and observation identity.

### Version 0.1.0 (Draft Specification)

- Initial draft for gh-aw graders.
- Defines `graders` configuration semantics and built-in grader set.
- Defines custom inline grader constraints and forbidden patterns.
- Defines grader artifact output contract and experiment metric references.
