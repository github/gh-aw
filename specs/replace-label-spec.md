---
title: replace-label Safe-Output Type Specification
description: Formal W3C-style specification for the replace-label safe-output type in GitHub Agentic Workflows
sidebar:
  order: 1005
---

# replace-label Safe-Output Type Specification

**Version**: 1.0.2
**Status**: Candidate Recommendation  
**Latest Version**: https://github.com/github/gh-aw/blob/main/specs/replace-label-spec.md  
**Editors**: GitHub gh-aw Team (GitHub, Inc.)  
**Publication Date**: 2026-09-12

---

## Abstract

This specification defines the `replace-label` safe-output type for GitHub Agentic Workflows (gh-aw), a mechanism that enables AI agents to transition label state on GitHub issues and pull requests in a single REST API call. The `replace-label` type removes one label and adds another in a single `PUT /repos/{owner}/{repo}/issues/{issue_number}/labels` REST call, eliminating the HTTP round-trip between the two operations that would otherwise exist when using `remove-labels` and `add-labels` as separate sequential messages.

The specification covers the configuration schema, the message schema produced by AI agents, the multi-stage validation pipeline, the REST API call executed against the GitHub API, error-handling requirements, security controls, and conformance testing requirements.

## Status of This Document

This is a Candidate Recommendation specification representing the design and implementation of the `replace-label` safe-output type as shipped in gh-aw version 1.0.0. This specification is subject to updates based on implementation feedback, operational experience, and security research. Future versions may introduce additional configuration options or refine validation semantics.

**Governance**: This specification is maintained by the GitHub gh-aw Team and governed by GitHub's research and engineering processes. Feedback and errata should be submitted as issues to the `github/gh-aw` repository.

---

## Table of Contents

1. [Introduction](#1-introduction)
2. [Conformance](#2-conformance)
3. [Concepts and Terminology](#3-concepts-and-terminology)
4. [Data Model](#4-data-model)
5. [Processing Model](#5-processing-model)
6. [REST Interface](#6-rest-interface)
7. [Error Handling](#7-error-handling)
8. [Security Considerations](#8-security-considerations)
9. [Compliance Testing](#9-compliance-testing)
10. [Examples](#10-examples)
11. [References](#references)
12. [Change Log](#change-log)

---

## 1. Introduction

### 1.1 Purpose

Label-based state machines are a common pattern in GitHub issue and pull request workflows. A triage issue may progress from `pending` → `in-review` → `approved`, or a PR review cycle may move from `needs-revision` → `ready-to-merge`. When these transitions are driven by AI agents operating through gh-aw, the canonical implementation using separate `remove-labels` and `add-labels` safe-output messages introduces a race window: between the removal and the addition, the item carries no label — or may be picked up by a concurrent automation that considers the intermediate label-less state valid.

The `replace-label` type solves this by combining the remove and add operations into a single REST API call (`PUT /repos/{owner}/{repo}/issues/{issue_number}/labels`). This endpoint replaces the entire label set of the target item atomically — the new desired state (current labels minus the removed label, plus the added label) is applied in a single request. This eliminates the HTTP round-trip between the two operations and provides true atomicity: either the entire label set update succeeds or it fails, with no intermediate state where neither label is present.

### 1.2 Scope

This specification covers:

- The YAML configuration schema for the `replace-label` key within the `safe-outputs` frontmatter block
- The JSON message schema produced by AI agents when requesting a label replacement
- The multi-stage processing pipeline: schema validation, label allowlist/blocklist enforcement, required-label and title-prefix gate checks, staged-mode preview, label set computation, and REST API call execution
- The exact REST API call used against the GitHub API
- Rate-limit retry semantics and error propagation
- Security controls, including cross-repository access restrictions
- Conformance requirements and test procedures

This specification does NOT cover:

- The `add-labels` safe-output type (separate type with different semantics)
- The `remove-labels` safe-output type (separate type with different semantics)
- Label management outside the context of gh-aw safe outputs (e.g., repository label administration)
- The gh-aw compilation pipeline (defined separately)
- GitHub Actions platform security guarantees
- AI agent prompt engineering or model behavior

### 1.3 Design Goals

The `replace-label` type is designed to satisfy the following goals:

1. **Reduced race window**: Remove and add operations MUST execute in a single GitHub API round-trip to eliminate the observable intermediate state compared to two separate requests.
2. **Idempotency on missing source label**: If the label to be removed is not present on the target item, the operation MUST still add the new label and succeed, rather than failing.
3. **Least privilege**: Allowlist-based configuration MUST constrain which labels agents may add or remove, limiting the blast radius of a misbehaving agent.
4. **Pre-existing labels**: Labels referenced in `label_to_add` MUST already exist in the repository; the REST `setLabels` call will fail with a 422 error if the label is missing.
5. **Safe preview**: Staged mode MUST allow operators to review what the agent would do without applying any changes to the GitHub API.
6. **Consistency with the safe-outputs framework**: Configuration fields (`max`, `target`, `target-repo`, `allowed-repos`, `github-token`, `staged`, `required-labels`, `required-title-prefix`) MUST follow the same semantics as all other safe-output types in gh-aw.

---

## 2. Conformance

### 2.1 Conformance Classes

This specification defines a single conformance class: a **conforming replace-label implementation**. A conforming implementation MUST satisfy all normative requirements (MUST/SHALL) defined in this specification. Optional features (SHOULD/MAY) are not required for conformance but are RECOMMENDED for production use.

### 2.2 Requirements Notation

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and "OPTIONAL" in this document are to be interpreted as described in [RFC 2119](https://www.ietf.org/rfc/rfc2119.txt).

Normative requirements are additionally identified by a short requirement code of the form **RL-NNN** (e.g., **RL-001**). These codes are the stable identifiers used in the compliance testing section.

### 2.3 Compliance Levels

| Level | Description | Requirements |
|-------|-------------|--------------|
| **Level 1 — Core** | Minimum viable replace-label support | All MUST/SHALL requirements |
| **Level 2 — Production** | Production-ready deployment | All Level 1 requirements + all SHOULD requirements |

---

## 3. Concepts and Terminology

**Label replacement**: The operation of removing one named label from a GitHub labelable item and adding a different named label in the same GitHub REST API call (`PUT /repos/{owner}/{repo}/issues/{issue_number}/labels`).

**Labelable**: A GitHub resource that can carry labels. In this specification, labelable items are GitHub Issues and GitHub Pull Requests.

**Label allowlist** (`allowed-add`, `allowed-remove`): Optional glob pattern lists in the workflow configuration that constrain which labels an agent may add or remove. When absent, no label-name restriction applies.

**Label blocklist** (`blocked`): Optional glob pattern list in the workflow configuration that unconditionally prohibits specific labels from being added or removed, applied after allowlist checks.

**Staged mode**: A preview mode in which the handler logs what it would do but makes no GitHub API calls. Activated by setting `staged: true` in the configuration.

**Triggering item**: The GitHub issue or pull request that caused the workflow to execute, identified from the GitHub Actions event context (`github.event.issue.number` or `github.event.pull_request.number`).

**Temporary ID**: A placeholder identifier used in agent messages when the target item number is not yet known (e.g., a newly created issue whose number is resolved by a prior safe-output handler). Temporary ID resolution is governed by the common gh-aw temporary-ID framework.

**Deterministic pastel color**: A six-character hex color code derived from a label name using a deterministic hash function, producing values in the pastel range (128–191 per RGB channel) for visual readability.

**Count gate**: The mechanism that tracks how many `replace-label` operations have been executed during a single workflow run and enforces the configured `max` limit.

---

## 4. Data Model

### 4.1 Configuration Schema

The `replace-label` type is configured under the `safe-outputs` key in a gh-aw workflow frontmatter block. All fields are optional unless otherwise noted.

```yaml
safe-outputs:
  replace-label:
    allowed-add: ["approved", "done"]          # Glob patterns for labels that may be added
    allowed-remove: ["in-review", "pending"]   # Glob patterns for labels that may be removed
    blocked: ["~*", "*[bot]"]                  # Glob patterns blocked for both add and remove
    max: 5                                     # Max replacements per run (default 5)
    target: "triggering"                       # "triggering" | "*" | explicit number
    target-repo: "owner/repo"                  # Cross-repo target repository
    allowed-repos: ["owner/repo"]              # Allowlist for multi-repo targeting
    github-token: "${{ secrets.TOKEN }}"       # Per-type token override
    required-labels: ["triage"]                # ALL must be present on item before operation
    required-title-prefix: "[Bug]"             # Item title must start with this prefix
    staged: false                              # Preview-only mode; no API calls made
```

#### 4.1.1 Field Definitions

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `allowed-add` | `string[]` | `[]` (any) | Glob patterns for labels the agent is permitted to add. When empty or absent, no add restriction applies. |
| `allowed-remove` | `string[]` | `[]` (any) | Glob patterns for labels the agent is permitted to remove. When empty or absent, no remove restriction applies. |
| `blocked` | `string[]` | `[]` (none) | Glob patterns that are unconditionally prohibited for both add and remove operations. Applied after allowlist checks. |
| `max` | `integer` or GHA expression | `5` | Maximum number of `replace-label` operations permitted in a single workflow run. Supports GitHub Actions expressions (e.g., `${{ inputs.max_labels }}`). |
| `target` | `"triggering"` \| `"*"` \| integer | `"triggering"` | Determines which issue/PR may be targeted. `"triggering"` restricts to the event item; `"*"` permits an `item_number` from the message, or falls back to the event item when absent; an integer pins to a specific item number. |
| `target-repo` | `string` | (current repo) | Default target repository in `owner/repo` format for cross-repository operations. |
| `allowed-repos` | `string[]` | `[]` | Additional repositories the agent may target, beyond `target-repo`. |
| `github-token` | `string` | (workflow default) | GitHub token or GitHub Actions expression for authentication. Overrides the workflow-level token for this type only. |
| `required-labels` | `string[]` | `[]` | ALL listed labels must be present on the target item for the operation to proceed. Items that do not satisfy this gate are skipped (not errored). |
| `required-title-prefix` | `string` | `""` | The target item's title must begin with this prefix for the operation to proceed. Items that do not satisfy this gate are skipped (not errored). |
| `staged` | `boolean` | `false` | When `true`, the handler logs a preview of each operation but makes no GitHub API calls. |

#### 4.1.2 Glob Pattern Semantics

**RL-001**: Glob pattern matching for `allowed-add`, `allowed-remove`, and `blocked` MUST follow the semantics of the `gobwas/glob` library (as used elsewhere in gh-aw), where `*` matches any sequence of characters within a label name and `[...]` denotes a character class.

**RL-002**: A label name MUST match at least one pattern in an allowlist (`allowed-add` or `allowed-remove`) when the list is non-empty. An empty or absent allowlist permits any label name.

**RL-003**: A label name MUST NOT match any pattern in the `blocked` list. Blocklist evaluation MUST occur before allowlist evaluation (it is a security boundary). A label matching a blocked pattern MUST be rejected immediately, regardless of any allowlist entry.

### 4.2 Message Schema

AI agents emit `replace_label` messages as part of the safe-outputs protocol. The following JSON schema defines the message structure:

```json
{
  "label_to_remove": "<string, required>",
  "label_to_add":    "<string, required>",
  "item_number":     "<integer | temporary-ID string, optional>",
  "repo":            "<string 'owner/repo', optional>"
}
```

#### 4.2.1 Message Field Definitions

| Field | Type | Required | Max Length | Description |
|-------|------|----------|------------|-------------|
| `label_to_remove` | `string` | Yes | 128 characters | Name of the label to remove from the target item. The label need not currently be present on the item (see §5.3.4). |
| `label_to_add` | `string` | Yes | 128 characters | Name of the label to add to the target item. The label need not pre-exist in the repository (see §5.4). |
| `item_number` | `integer` or temporary-ID `string` | No | — | Issue or pull request number to target when `target: "*"` is configured. When absent, falls back to the triggering item derived from the GitHub Actions event context. May be a temporary-ID string resolved by the gh-aw temporary-ID framework. |
| `repo` | `string` | No | 256 characters | Target repository in `owner/repo` format. Overrides the configured `target-repo` for this message only. Must satisfy the `allowed-repos` configuration constraint. |

**RL-004**: A conforming implementation MUST reject any `replace_label` message in which `label_to_remove` is absent, empty after trimming, or exceeds 128 characters.

**RL-005**: A conforming implementation MUST reject any `replace_label` message in which `label_to_add` is absent, empty after trimming, or exceeds 128 characters.

**RL-006**: A conforming implementation MUST reject any `replace_label` message in which `repo` is present and exceeds 256 characters.

**RL-007**: A conforming implementation MUST sanitize the string values of `label_to_remove` and `label_to_add` against the standard gh-aw safe-output string sanitization rules before use.

#### 4.2.2 Aliased Item Number Fields

With `target: "*"`, the handler MUST also accept the following field names as aliases for `item_number`:

- `issue_number`
- `pr_number`
- `pull_number`

When multiple aliased fields are present in the same message, `item_number` takes precedence, followed by `issue_number`, `pr_number`, and `pull_number` in that order.

---

## 5. Processing Model

A conforming implementation MUST execute the following pipeline for each `replace_label` message. The stages are ordered; a failure at any stage MUST prevent execution of subsequent stages.

```
 Agent Message (JSON)
        │
        ▼
 ┌─────────────────────┐
 │  Stage 1            │
 │  Schema Validation  │  ← RL-004 – RL-007
 └──────────┬──────────┘
            │
            ▼
 ┌─────────────────────┐
 │  Stage 2            │
 │  Count Gate Check   │  ← max enforcement
 └──────────┬──────────┘
            │
            ▼
 ┌─────────────────────┐
 │  Stage 3            │
 │  Target Resolution  │  ← item_number, repo, target config
 └──────────┬──────────┘
            │
            ▼
 ┌─────────────────────┐
 │  Stage 4            │
 │  Label Validation   │  ← allowed-add, allowed-remove, blocked
 └──────────┬──────────┘
            │
            ▼
 ┌─────────────────────┐
 │  Stage 5            │
 │  Gate Checks        │  ← required-labels, required-title-prefix
 └──────────┬──────────┘
            │
            ▼
 ┌─────────────────────┐
 │  Stage 6            │
 │  Staged Mode Check  │  ← log preview; exit if staged: true
 └──────────┬──────────┘
            │
            ▼
 ┌─────────────────────┐
 │  Stage 7            │
 │  Label Set          │  ← compute new label set (current - remove + add)
 │  Computation        │
 └──────────┬──────────┘
            │
            ▼
 ┌─────────────────────┐
 │  Stage 8            │
 │  REST setLabels     │  ← single REST call (PUT /issues/{n}/labels)
 └─────────────────────┘
```

### 5.1 Stage 1: Schema Validation

**RL-008**: The implementation MUST validate each incoming message against the schema defined in §4.2 before any other processing.

**RL-009**: Schema validation MUST be performed by the common gh-aw safe-output validation pipeline (defined in `pkg/workflow/safe_outputs_validation_config.go`) using the `replace_label` entry with `DefaultMax: 5` and the field map:

| Field | Required | Type | Max Length | Notes |
|-------|----------|------|------------|-------|
| `label_to_remove` | Yes | `string` | 128 | Sanitized |
| `label_to_add` | Yes | `string` | 128 | Sanitized |
| `item_number` | No | issue number or temporary ID | — | — |
| `repo` | No | `string` | 256 | — |

Messages that fail schema validation MUST be rejected with a structured error logged to the GitHub Actions step summary. The workflow run MUST NOT be marked as failed solely due to a validation rejection; the message is silently skipped with a warning.

### 5.2 Stage 2: Count Gate Check

**RL-010**: The implementation MUST track the number of `replace-label` operations executed in the current workflow run. When the count would exceed the configured `max` value, the message MUST be rejected with a warning and processing MUST stop for that message.

**RL-011**: The `max` field MUST support GitHub Actions expressions (e.g., `${{ inputs.max }}`) which are resolved at workflow execution time.

**RL-012**: The default value of `max` MUST be `5` when the field is absent from the configuration.

### 5.3 Stage 3: Target Resolution

#### 5.3.1 Repository Resolution

**RL-013**: The target repository is resolved as follows, in priority order:

1. The `repo` field in the agent message, when present.
2. The `target-repo` configuration field, when present.
3. The repository of the triggering workflow run (the default).

**RL-014**: The resolved repository MUST be in `owner/repo` format. The implementation MUST reject messages with a malformed repository identifier.

**RL-015**: When `allowed-repos` is non-empty, the resolved repository MUST match one of the entries in `allowed-repos`. A message targeting a repository not in this list MUST be rejected with a warning.

#### 5.3.2 Item Number Resolution

**RL-016**: The target item number MUST be resolved from the configured `target` mode before considering agent-supplied fields. Omitted `target` MUST be interpreted as `"triggering"`.

**RL-017**: When no item number can be resolved for the configured target mode, the message MUST be rejected with the error "No issue/PR number available".

#### 5.3.3 Target Mode Enforcement

**RL-018**: When `target` is set to `"triggering"` or omitted, the handler MUST use the triggering item's number and MUST ignore agent-supplied item-number fields.

**RL-019**: When `target` is set to an explicit integer, the handler MUST use that integer and MUST ignore agent-supplied item-number fields.

**RL-020**: Only when `target` is set to `"*"` MAY the handler resolve an item number from `item_number`, `issue_number`, `pr_number`, or `pull_number`, subject to repository constraints. If no agent-supplied number is present, the handler MAY fall back to the triggering item.

**RL-020a**: The privileged handler MUST enforce RL-016 and RL-018 through RL-020 at runtime.

### 5.4 Stage 4: Label Validation

**RL-021**: The implementation MUST evaluate `label_to_remove` against `allowed-remove` (if non-empty) and `blocked` (if non-empty). The evaluation order is:

1. If `allowed-remove` is non-empty, `label_to_remove` MUST match at least one pattern. Failure → reject.
2. If `blocked` is non-empty, `label_to_remove` MUST NOT match any pattern. Match → reject.

**RL-022**: The implementation MUST evaluate `label_to_add` against `allowed-add` (if non-empty) and `blocked` (if non-empty). The evaluation order is:

1. If `allowed-add` is non-empty, `label_to_add` MUST match at least one pattern. Failure → reject.
2. If `blocked` is non-empty, `label_to_add` MUST NOT match any pattern. Match → reject.

**RL-023**: Label validation failures MUST be reported as warnings and the message MUST be skipped. The workflow run MUST NOT be marked as failed solely due to a label validation rejection.

### 5.5 Stage 5: Gate Checks

Gate checks require fetching the current state of the target item via the GitHub REST API (`GET /repos/{owner}/{repo}/issues/{issue_number}`) before proceeding.

#### 5.5.1 Required-Labels Gate

**RL-024**: When `required-labels` is non-empty, the implementation MUST retrieve the current labels on the target item and verify that ALL labels in `required-labels` are present. If any required label is absent, the message MUST be skipped (not failed) with an informational log entry.

#### 5.5.2 Required-Title-Prefix Gate

**RL-025**: When `required-title-prefix` is non-empty, the implementation MUST retrieve the title of the target item and verify that it begins with the configured prefix. If the title does not match, the message MUST be skipped (not failed) with an informational log entry.

**RL-026**: Gate check failures MUST result in a `{ success: false, skipped: true }` handler result, distinguishing them from hard errors.

### 5.6 Stage 6: Staged Mode

**RL-027**: When `staged: true` is set in the configuration, the implementation MUST NOT make any GitHub API write calls for this message. Instead, the implementation MUST log a structured preview entry describing what would have been executed, including:

- The target item number
- The target repository
- The label that would be removed (`label_to_remove`)
- The label that would be added (`label_to_add`)
- The item type (issue or pull request)

**RL-028**: Staged mode execution MUST return `{ success: true, staged: true }` from the handler. Note: the operation count IS incremented for staged messages by the `createCountGatedHandler` scaffold (which counts every processed message before delegating to the handler), so staged messages count toward the `max` budget.

### 5.7 Stage 7: Label Set Computation

The implementation derives the new label set from the current labels already attached to the target item (returned by the gate-check REST call in Stage 5). No additional API calls are required at this stage.

**RL-029**: The implementation MUST compute the new label set as: `(current_labels − {label_to_remove}) ∪ {label_to_add}`, deduplicating the result. Label names MUST be preserved exactly as they appear on the item; no truncation or normalisation is applied.

**RL-033**: The presence of `label_to_remove` on the item MUST be determined from the current labels returned by the `GET /repos/{owner}/{repo}/issues/{issue_number}` REST call already performed in Stage 5.

**RL-034**: When `label_to_remove` is not currently attached to the target item, the implementation MUST proceed with the operation, producing a new label set that simply adds `label_to_add` without removing anything. The operation MUST NOT fail in this case (see §1.3, Design Goal 2).

### 5.8 Stage 8: REST Label Update

**RL-036**: The implementation MUST execute the label replacement using a single `PUT /repos/{owner}/{repo}/issues/{issue_number}/labels` REST API call (`issues.setLabels`), passing the complete new label set computed in Stage 7. Separate add and remove calls MUST NOT be used.

**RL-037**: The REST call MUST be retried on GitHub API rate-limit responses using the `RATE_LIMIT_RETRY_CONFIG` policy.

**RL-038**: On a successful call, the implementation MUST log:
- The item type (issue or pull request), item number, and repository
- The label that was removed (or a note that the source label was absent and no removal occurred)
- The label that was added
- The complete updated label set returned by the REST response

**RL-039**: The handler result for a successful operation MUST include the fields `success: true`, `number`, `repo`, `labelRemoved` (null when the source label was absent), `labelAdded`, and `contextType`.

**RL-040**: The handler result MUST include before-state and after-state execution metadata (via `attachExecutionState`) for observability and audit purposes.

---

## 6. REST Interface

### 6.1 API Call

The label replacement is executed via a single REST API call:

```
PUT /repos/{owner}/{repo}/issues/{issue_number}/labels
```

In the Octokit client this is `githubClient.rest.issues.setLabels(params)`.

#### 6.1.1 Parameters

| Parameter | Source |
|-----------|--------|
| `owner` | Repository owner, resolved from target configuration |
| `repo` | Repository name, resolved from target configuration |
| `issue_number` | Target item number, resolved in Stage 3 |
| `labels` | New label name array computed in Stage 7 |

**RL-041**: The `labels` array MUST contain all labels that should be present on the item after the operation: current labels minus `label_to_remove` (if present), plus `label_to_add`, deduplicated.

**RL-042**: `label_to_add` MUST always appear exactly once in the `labels` array (after deduplication).

### 6.2 Execution Semantics

**RL-043**: The `PUT /repos/{owner}/{repo}/issues/{issue_number}/labels` REST call replaces the entire label set of the target item in a single atomic operation. Either the new label set is applied successfully or the call fails — there is no intermediate state where neither label is present.

> **Informative note**: Unlike the former GraphQL approach (two root mutations in one request), this REST call provides true atomicity at the API layer: both the removal and addition are reflected by a single server-side update. While the call itself cannot partially fail (see §7.2), the implementation MUST still verify the returned label set matches the expected post-operation state — see §7.4 for the partial-success response handling requirement.

---

## 7. Error Handling

### 7.1 Error Categories

| Category | Code | Behavior |
|----------|------|----------|
| Schema validation failure | `SCHEMA_INVALID` | Skip message with warning; do not fail run |
| Count gate exceeded | `MAX_EXCEEDED` | Skip message with warning; do not fail run |
| Target resolution failure | `TARGET_UNRESOLVABLE` | Skip message with warning; do not fail run |
| Repository not in allowlist | `REPO_NOT_ALLOWED` | Skip message with warning; do not fail run |
| Label validation failure | `LABEL_BLOCKED` or `LABEL_NOT_ALLOWED` | Skip message with warning; do not fail run |
| Gate check (required-labels) | `GATE_REQUIRED_LABELS` | Skip message (skipped=true); do not fail run |
| Gate check (title prefix) | `GATE_TITLE_PREFIX` | Skip message (skipped=true); do not fail run |
| REST setLabels failure | `SETLABELS_FAILED` | Return hard error; message fails |
| Rate-limit exhausted after retries | `RATE_LIMIT_EXHAUSTED` | Return hard error; message fails |

**RL-044**: A conforming implementation MUST NOT mark the workflow run as failed for soft-skip errors (schema invalid, count gate, target unresolvable, repo not allowed, label validation failure, gate check failures). These errors MUST be surfaced as workflow warnings only.

**RL-045**: A conforming implementation MUST surface hard errors (REST call failure, rate-limit exhaustion) as `core.error()` entries in the GitHub Actions log.

### 7.2 REST Call Failure

**RL-046**: When the `setLabels` REST call fails (e.g., HTTP 422 for an invalid label name), the implementation MUST log a `core.error()` entry and MUST return `{ success: false, error: <message> }`. For HTTP-level failures the call is all-or-nothing — either all label changes are applied or none are. For HTTP 200 responses, see §7.4 for partial-success response handling.

**RL-046a**: If `setLabels` returns HTTP 404, the implementation MUST treat the request as rejected because the authoritative labeling target or repository is no longer reachable.

**RL-046b**: If `setLabels` returns HTTP 5xx, times out, or fails with a transport error, the implementation MUST treat the condition as transient and retry according to the retry policy in §7.3. If retries are exhausted, the implementation MUST return `{ success: false, error: <message> }` without reporting a successful replacement.

### 7.3 Rate-Limit Retry Policy

**RL-048**: The `setLabels` REST call (Stage 8) MUST apply the `RATE_LIMIT_RETRY_CONFIG` retry policy from `actions/setup/js/error_recovery.cjs`. This policy covers secondary rate-limit responses (HTTP 403 with Retry-After header) and primary rate-limit responses (HTTP 429).

### 7.4 Partial-Success Response Handling

A partial-success condition arises when the `setLabels` REST call returns HTTP 200 but the label set on the item after the call does not match the expected post-operation set (i.e., `label_to_add` is absent from the returned labels, or `label_to_remove` is still present in the returned labels).

**RL-057**: After a successful HTTP 200 response from `setLabels`, the implementation MUST verify that the returned label array satisfies all of the following conditions:

1. `label_to_add` is present in the returned label array.
2. `label_to_remove` is absent from the returned label array (when it was present before the call and was not the same label as `label_to_add`).

**RL-058 (Partial-success outcome)**: If either condition in RL-057 is not satisfied, the implementation MUST treat this as a `rejected` outcome and MUST log a `core.error()` entry describing which expected label condition was not met (e.g., `"replace_label: label_to_add '<name>' not found in POST-setLabels response"` or `"replace_label: label_to_remove '<name>' still present after setLabels call"`). The implementation MUST return `{ success: false, error: <message> }` in this case.

**RL-059 (No new error code)**: The partial-success condition described in this section MUST be treated as a `rejected` outcome using the existing `SETLABELS_FAILED` error category (Section 7.1). No new error code is introduced for partial-success; the HTTP status alone is insufficient to determine outcome correctness.

**Rationale:** Although the GitHub REST API `PUT /repos/{owner}/{repo}/issues/{number}/labels` is intended to be atomic, race conditions (e.g., concurrent label modifications by another actor between the read in Stage 7 and the write in Stage 8) or API behavioral edge cases may cause the returned label set to diverge from the intended post-operation state. Verifying the response ensures the implementation does not silently report success when the state transition did not complete as expected.

---

## 8. Security Considerations

### 8.1 Label Allowlist Enforcement

Label allowlists and blocklists are the primary mechanism preventing AI agents from performing unintended or malicious label transitions. For example, a `blocked: ["~*"]` pattern would prohibit any label whose name begins with `~`, while `blocked: ["*[bot]"]` would prohibit adding bot-created labels.

**RL-049**: Allowlist and blocklist evaluation MUST be performed server-side (in the JavaScript handler executing within GitHub Actions), not by the AI agent. Agents MUST NOT be trusted to self-enforce label restrictions.

**RL-049a**: A conforming implementation MUST re-check `label_to_add` against the current server-side allowlist and blocklist state immediately before invoking `PUT /repos/{owner}/{repo}/issues/{issue_number}/labels`. If the label is blocked or no longer allowed at that point, the implementation MUST reject the message without calling the write API.

### 8.2 Cross-Repository Restrictions

By default, `replace-label` operates on the repository of the triggering workflow. Cross-repository operation is opt-in and must be explicitly declared.

**RL-050**: A conforming implementation MUST NOT execute a label replacement on a repository not reachable via the resolved `target-repo` or `allowed-repos` configuration. Unrecognized repository identifiers in agent messages MUST be rejected.

**RL-051**: The GitHub token used for cross-repository operations MUST have `issues: write` permission on the target repository. The implementation SHOULD validate this at startup or log a clear error when the token lacks the required scope.

### 8.3 Label Requirements

Labels referenced in `label_to_add` must pre-exist in the repository. The implementation returns a hard error when a label is not found, consistent with the `add-labels` safe-output type and preventing silent repository side effects.

**RL-052**: The implementation MUST NOT create new labels on behalf of an AI agent. If `label_to_add` does not exist in the target repository, the operation MUST fail with a hard error and the message MUST be rejected.

### 8.4 Required-Labels as an Execution Gate

The `required-labels` configuration field provides an additional execution gate that can be used to constrain when label replacements are permitted. For example, a workflow can require that an issue carries a `triage` label before any agent-driven label transition is allowed.

**RL-053**: The `required-labels` check MUST use the current server-side label state fetched from the GitHub REST API, not any label state provided in the agent message itself.

### 8.5 Token Scope

**RL-054**: The GitHub token used by `replace-label` MUST have the `issues: write` permission (which also covers pull request label operations). This is the minimum required scope.

**RL-055**: The implementation SHOULD use a dedicated per-type token (via `github-token`) when the default workflow token carries broader permissions than required by `replace-label` alone.

### 8.6 Staged Mode as a Security Control

Staged mode provides a mechanism for operators to audit AI agent label-transition behavior before it takes effect.

**RL-056**: When `staged: true`, the implementation MUST NOT call any write API endpoint. Read-only API calls performed during Stage 5 gate checks MAY proceed in staged mode.

The staged-mode preview SHOULD identify the `label_to_add` value that would be revalidated before the write call so operators can compare previewed transitions against protected-label policy before disabling staged mode.

### 8.7 Sync Notes

The REST failure and retry semantics in [Section 7](#7-error-handling) are mirrored by the `replace_label` outcome-evaluation rules in [`safe-output-outcome-evaluation.md` Section 30](safe-output-outcome-evaluation.md#30-replace_label). Changes to `404`, `5xx`, or `429` handling in either document SHOULD be reviewed against the other document in the same change.

---

## 9. Compliance Testing

### 9.1 Test Suite Structure

The test suite for `replace-label` spans two layers:

- **Unit tests** (`actions/setup/js/replace_label.test.cjs`): Test the JavaScript handler in isolation using mocked GitHub API clients.
- **Integration tests** (`pkg/workflow/`): Test Go configuration parsing and schema validation using the common safe-output test infrastructure.

For outcome evaluation compliance (verifying that the `replace_label` outcome evaluator correctly classifies `accepted`, `rejected`, `pending`, and API-failure scenarios), see the outcome evaluation conformance test requirements in [`specs/safe-output-outcome-evaluation.md`](safe-output-outcome-evaluation.md) — specifically the `replace_label` section (Section 30) and the Conformance Test Table entry for `replace_label`.

### 9.2 Test Requirements

#### 9.2.1 Schema Validation Tests

- **T-RL-001**: Verify that a message with a missing `label_to_remove` is rejected.
- **T-RL-002**: Verify that a message with a missing `label_to_add` is rejected.
- **T-RL-003**: Verify that a message with `label_to_remove` exceeding 128 characters is rejected.
- **T-RL-004**: Verify that a message with `label_to_add` exceeding 128 characters is rejected.
- **T-RL-005**: Verify that a message with `repo` exceeding 256 characters is rejected.
- **T-RL-006**: Verify that a valid message passes schema validation without error.

#### 9.2.2 Count Gate Tests

- **T-RL-010**: Verify that the operation succeeds when count < max.
- **T-RL-011**: Verify that the operation is rejected when count = max (gate is exclusive: count must be strictly less than max).
- **T-RL-012**: Verify that the default max of 5 is enforced when `max` is absent from configuration.
- **T-RL-013**: Verify that a GHA expression in `max` is resolved at runtime.

#### 9.2.3 Target Authorization Tests

- **T-RL-014**: Verify that an omitted `target` ignores a conflicting agent-supplied item number and uses the triggering item.
- **T-RL-015**: Verify that `target: "triggering"` ignores a conflicting agent-supplied item number.
- **T-RL-016**: Verify that a fixed numeric target ignores a conflicting agent-supplied item number.
- **T-RL-017**: Verify that a fixed numeric target is used when the message omits an item number.
- **T-RL-018**: Verify that `target: "*"` accepts an agent-supplied item number.

#### 9.2.4 Label Validation Tests

The normative compliance fixtures for the allowlist and blocklist edge cases in
this subsection live in `specs/replace-label-compliance/rl-002-allowlist-enforcement.yaml`
and `specs/replace-label-compliance/rl-003-blocklist-ordering.yaml`.

- **T-RL-020**: Verify that `label_to_add` is accepted when `allowed-add` is empty.
- **T-RL-021**: Verify that `label_to_add` matching a pattern in `allowed-add` is accepted.
- **T-RL-022**: Verify that `label_to_add` not matching any pattern in a non-empty `allowed-add` is rejected.
- **T-RL-023**: Verify that `label_to_add` matching a `blocked` pattern is rejected even when it also matches `allowed-add`.
- **T-RL-024**: Verify that `label_to_remove` matching a `blocked` pattern is rejected.
- **T-RL-025**: Verify that `label_to_remove` is accepted when `allowed-remove` is empty.

Fixture linkage check (2026-08-01):

- [x] T-RL-020 covered by `specs/replace-label-compliance/rl-001-glob-semantics.yaml`
- [x] T-RL-021 covered by `specs/replace-label-compliance/rl-001-glob-semantics.yaml`
- [x] T-RL-022 covered by `specs/replace-label-compliance/rl-001-glob-semantics.yaml`
- [x] T-RL-023 covered by `specs/replace-label-compliance/rl-001-glob-semantics.yaml` and `specs/replace-label-compliance/rl-003-blocklist-ordering.yaml`
- [x] T-RL-024 covered by `specs/replace-label-compliance/rl-003-blocklist-ordering.yaml`
- [x] T-RL-025 covered by `specs/replace-label-compliance/rl-002-allowlist-enforcement.yaml`

#### 9.2.5 Gate Check Tests

- **T-RL-030**: Verify that an item satisfying all `required-labels` proceeds to the mutation stage.
- **T-RL-031**: Verify that an item missing a required label is skipped (`skipped: true`) without failing.
- **T-RL-032**: Verify that an item with a title matching `required-title-prefix` proceeds.
- **T-RL-033**: Verify that an item whose title does not match `required-title-prefix` is skipped without failing.

#### 9.2.6 Label Set Computation Tests

- **T-RL-040**: Verify that when `label_to_remove` is on the item, the computed new label set excludes it and includes `label_to_add`.
- **T-RL-041**: Verify that when `label_to_add` is passed to `setLabels` and the label does not exist in the repository, the call fails with a hard error.
- **T-RL-044**: Verify that when `label_to_remove` is not on the item, the computed new label set adds `label_to_add` without removing any label.

#### 9.2.7 REST setLabels Tests

- **T-RL-050**: Verify that `setLabels` is called with the correct `owner`, `repo`, `issue_number`, and `labels` array.
- **T-RL-051**: Verify that the updated label list returned by `setLabels` is logged.
- **T-RL-052**: Verify that a `setLabels` failure produces a hard error result with `success: false`.
- **T-RL-053**: Verify that `label_to_add` always appears exactly once in the `labels` array.
- **T-RL-054**: Verify that rate-limit responses trigger retry behavior.

#### 9.2.8 Staged Mode Tests

- **T-RL-060**: Verify that no write API call is made when `staged: true`.
- **T-RL-061**: Verify that the preview log entry includes the correct label names, item number, and repository.
- **T-RL-062**: Verify that staged mode returns `{ success: true, staged: true }`.

#### 9.2.9 Cross-Repository Tests

- **T-RL-070**: Verify that a message with a `repo` in `allowed-repos` is accepted.
- **T-RL-071**: Verify that a message with a `repo` not in `allowed-repos` is rejected.
- **T-RL-072**: Verify that `target-repo` is used as the default when `repo` is absent from the message.

### 9.3 Compliance Checklist

| Requirement | Test ID(s) | Level | Status |
|-------------|------------|-------|--------|
| RL-001 Glob pattern matching semantics | T-RL-020 – T-RL-025 | 1 | Required |
| RL-002 Allowlist enforcement | T-RL-021, T-RL-022, T-RL-025 | 1 | Required |
| RL-003 Blocklist enforcement | T-RL-023, T-RL-024 | 1 | Required |
| RL-004 label_to_remove required | T-RL-001 | 1 | Required |
| RL-005 label_to_add required | T-RL-002 | 1 | Required |
| RL-006 repo max length | T-RL-005 | 1 | Required |
| RL-007 String sanitization | T-RL-006 | 1 | Required |
| RL-010 Count gate enforcement | T-RL-010, T-RL-011 | 1 | Required |
| RL-012 Default max = 5 | T-RL-012 | 1 | Required |
| RL-016 Target mode precedence | T-RL-014 – T-RL-018 | 1 | Required |
| RL-017 No item number error | T-RL-006 | 1 | Required |
| RL-018 Triggering target authorization | T-RL-014, T-RL-015 | 1 | Required |
| RL-019 Fixed target authorization | T-RL-016, T-RL-017 | 1 | Required |
| RL-020 Wildcard target authorization | T-RL-018 | 1 | Required |
| RL-024 required-labels gate | T-RL-030, T-RL-031 | 1 | Required |
| RL-025 required-title-prefix gate | T-RL-032, T-RL-033 | 1 | Required |
| RL-027 Staged mode no writes | T-RL-060 | 1 | Required |
| RL-029 Label set computation | T-RL-040 | 1 | Required |
| RL-034 Missing label_to_remove proceeds | T-RL-044 | 1 | Required |
| RL-036 Single REST setLabels call | T-RL-050 | 1 | Required |
| RL-037 Rate-limit retry on REST call | T-RL-054 | 2 | Recommended |
| RL-041 label_to_add in labels array | T-RL-053 | 1 | Required |
| RL-043 Atomic REST operation | T-RL-050 | 1 | Required |
| RL-050 Cross-repo restrictions | T-RL-070 – T-RL-072 | 1 | Required |
| RL-052 setLabels fails for missing label | T-RL-041 | 1 | Required |

---

## 10. Examples

### 10.1 Basic Configuration: Approved Review Workflow

A workflow that allows an AI code-review agent to transition pull requests from `in-review` to `approved` or `changes-requested`:

```yaml
safe-outputs:
  replace-label:
    allowed-remove: ["in-review", "changes-requested"]
    allowed-add: ["approved", "changes-requested"]
    blocked: ["~*", "do-not-merge"]
    max: 3
    target: "triggering"
    required-labels: ["in-review"]
```

When the agent emits:

```json
{
  "label_to_remove": "in-review",
  "label_to_add": "approved"
}
```

The handler will:
1. Validate the message schema.
2. Check the count gate (count=0 < max=3 ✓).
3. Resolve the triggering PR as the target.
4. Validate `in-review` against `allowed-remove` ✓, check against `blocked` ✓.
5. Validate `approved` against `allowed-add` ✓, check against `blocked` ✓.
6. Fetch the PR and verify `in-review` is present (required-labels gate ✓).
7. Compute new label set: `["approved"] ∪ (current_labels − {"in-review"})`.
8. Execute `setLabels` REST call with the new label set in a single request.

### 10.2 Missing Source Label: Graceful Add-Only

If the PR in Example 10.1 does not currently carry `in-review` (e.g., a prior run already removed it), the handler proceeds with add-only:

```
Label "in-review" is not present on pull request #42 in owner/repo — will only add "approved"
```

The handler logs `Label "in-review" is not present on pull request #42 in owner/repo — will only add "approved"`.

The `setLabels` call is made with a label array that does not contain `in-review` (it was never present) but does contain `approved` plus any other existing labels. The operation succeeds and returns `{ labelRemoved: null, labelAdded: "approved" }`.

### 10.3 Staged Mode Preview

With `staged: true` in the configuration:

```yaml
safe-outputs:
  replace-label:
    staged: true
    target: "*"
    allowed-add: ["done"]
    allowed-remove: ["in-progress"]
```

For the message `{ "label_to_remove": "in-progress", "label_to_add": "done", "item_number": 17 }`, the handler logs:

```
[STAGED] Would replace label "in-progress" → "done" on issue #17 in owner/repo
```

No API write calls are made. The handler returns `{ success: true, staged: true }`.

### 10.4 Non-Existent Label

Given the message `{ "label_to_remove": "needs-review", "label_to_add": "ship-it" }` and `ship-it` not existing in the repository, the `setLabels` REST call returns HTTP 422 (Unprocessable Entity). The handler logs:

```
Failed to replace label: Validation Failed
```

The message is rejected with `{ success: false }`. The label must be created in the repository before use.

### 10.5 Cross-Repository Operation

```yaml
safe-outputs:
  replace-label:
    target: "*"
    target-repo: "owner/infra"
    allowed-repos: ["owner/infra", "owner/platform"]
    allowed-add: ["deployed"]
    allowed-remove: ["pending-deploy"]
    github-token: "${{ secrets.INFRA_LABEL_TOKEN }}"
```

Agent message:

```json
{
  "label_to_remove": "pending-deploy",
  "label_to_add": "deployed",
  "item_number": 88,
  "repo": "owner/platform"
}
```

The handler resolves the target repository to `owner/platform` (which is in `allowed-repos`), fetches issue #88 from that repository, and executes the `setLabels` REST call using the `INFRA_LABEL_TOKEN` credential.

### 10.6 Blocked Label Rejection

Configuration:

```yaml
safe-outputs:
  replace-label:
    blocked: ["*[bot]", "~*"]
```

Agent message:

```json
{ "label_to_remove": "review-requested", "label_to_add": "~approved" }
```

Stage 4 evaluation:
- `allowed-add` is empty → no allowlist restriction.
- `~approved` matches blocked pattern `~*` → **rejected** with warning:

```
label_to_add validation failed: label "~approved" matches blocked pattern "~*"
```

The message is skipped. The workflow run is not marked as failed.

---

## References

### Normative References

- **[RFC 2119]** Bradner, S., "Key words for use in RFCs to Indicate Requirement Levels", BCP 14, RFC 2119, March 1997. https://www.ietf.org/rfc/rfc2119.txt

- **[GITHUB-REST-LABELS]** GitHub, Inc., "Labels REST API", GitHub REST API Documentation. https://docs.github.com/en/rest/issues/labels

### Informative References

- **[GH-AW-SECURITY]** GitHub gh-aw Team, "GitHub Agentic Workflows Security Architecture Specification", 2026. `specs/security-architecture-spec.md`

- **[GH-AW-CONFIG]** GitHub gh-aw Team, "AWF Config Canonical Sources Specification", 2026. `specs/awf-config-sources-spec.md`

- **[GOBWAS-GLOB]** Gobwas, "glob — Go glob matching library". https://github.com/gobwas/glob

- **[RATE-LIMIT]** GitHub, Inc., "Rate limits for the REST API". https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api

---

## Change Log

### Version 1.0.2 (Revision) — 2026-09-12

- Clarified that configured target modes take precedence over agent-supplied item numbers.
- Added T-RL-014 through T-RL-018 target authorization tests.

### Version 1.0.1 (Revision) — 2026-06-22

- Replaced GraphQL mutation (Stage 8) with a single REST `PUT /repos/{owner}/{repo}/issues/{issue_number}/labels` call (`setLabels`), achieving true atomicity: either the entire label set update succeeds or fails with no partial-success scenario.
- Removed Stage 7 label-resolution step (no longer requires node-ID lookup); replaced with label-set computation from the already-fetched issue state.
- Removed §6 GraphQL Interface; added §6 REST Interface describing the `setLabels` endpoint, parameters, and execution semantics.
- Removed RL-046/RL-047 partial-mutation-success requirements (not applicable to single-call REST approach).
- Updated RL-029, RL-034, RL-036, RL-041 – RL-043 to reflect REST semantics.
- Updated examples, compliance checklist, and references accordingly.

### Version 1.0.0 (Candidate Recommendation) — 2026-06-20

- Initial publication of the `replace-label` safe-output type specification.
- Covers configuration schema, message schema, eight-stage processing model, GraphQL mutation, error-handling categories, security considerations, and compliance test suite.
- Normative requirement codes RL-001 through RL-056 established.
- Test IDs T-RL-001 through T-RL-072 defined.

---

*Copyright © 2026 GitHub, Inc. All rights reserved.*
