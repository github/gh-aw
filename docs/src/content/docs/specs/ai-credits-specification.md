---
title: AI Credits Specification
description: Formal W3C-style specification for AI Credits (AIC) calculation, model pricing catalog format, and Copilot billing reference requirements.
sidebar:
  order: 1361
---

# AI Credits Specification

**Version**: 1.5.3<br />
**Status**: Draft  
**Publication Date**: 2026-11-05<br />
**Editor**: GitHub Agentic Workflows Team  
**This Version**: [ai-credits-specification](/gh-aw/specs/ai-credits-specification/)  
**Latest Published Version**: This document

---

## Abstract

This specification defines AI Credits (AIC) as the normative inference-cost metric for GitHub Agentic Workflows. It specifies the required calculation model from token usage and provider pricing, the canonical `models.json` catalog format used to store per-model pricing inputs, and the required external references for GitHub Copilot model and billing alignment.

## Status of This Document

This section describes the status of this document at the time of publication. This is a draft specification and may be updated, replaced, or made obsolete by other documents at any time.

This document is governed by the GitHub Agentic Workflows project specifications process.

## Table of Contents

1. [Introduction](#1-introduction)
2. [Conformance](#2-conformance)
3. [AI Credits Accounting Model](#3-ai-credits-accounting-model)
4. [Pricing Catalog Format (`models.json`)](#4-pricing-catalog-format-modelsjson)
5. [Catalog Provisioning and Synchronization](#5-catalog-provisioning-and-synchronization)
6. [Copilot Billing Reference Requirements](#6-copilot-billing-reference-requirements)
7. [Reporting Requirements](#7-reporting-requirements)
8. [Compliance Testing](#8-compliance-testing)
9. [Daily AI Credits Guardrail](#9-daily-ai-credits-guardrail)
10. [Per-Run AI Credits Budget](#10-per-run-ai-credits-budget)
11. [Appendices](#appendices)
12. [References](#references)
13. [Sync Notes](#sync-notes)
14. [Change Log](#change-log)

---

## 1. Introduction

### 1.1 Purpose

AIC provides a single monetary-normalized metric for inference cost across supported model providers. This specification defines how conforming implementations compute AIC, how pricing data is represented, and how Copilot-specific pricing alignment is governed.

### 1.2 Scope

This specification covers:

- The normative AIC unit definition and conversion rules.
- The per-invocation and aggregated AIC calculation formulas.
- Required `models.json` data structure and field semantics.
- Requirements for how pricing catalog data is provided and mirrored in gh-aw.
- Required references to GitHub Copilot model and billing documentation.

This specification does NOT cover:

- GitHub Actions minutes billing.
- Provider-side billing reconciliation and invoice dispute procedures.

### 1.3 Design Goals

The specification is designed to:

1. Provide a testable and deterministic calculation contract.
2. Keep pricing inputs explicit and auditable through structured catalog files.
3. Support model-name drift through normalized lookup and prefix fallback matching.
4. Maintain compatibility between CLI and setup runtime pricing catalogs.

---

## 2. Conformance

### 2.1 Conformance Classes

**Conforming implementation**: Satisfies all MUST/SHALL requirements in Sections 3 through 8.

**Partially conforming implementation**: Computes core AIC from token usage and model pricing (Section 3) but omits one or more optional reporting or synchronization requirements.

### 2.2 Requirements Notation

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and "OPTIONAL" in this document are to be interpreted as described in [RFC 2119](https://www.ietf.org/rfc/rfc2119.txt).

### 2.3 Compliance Levels

- **Level 1 – Calculation**: Implements AIC formulas and provider token handling (Section 3).
- **Level 2 – Catalog**: Implements required `models.json` structure and lookup semantics (Section 4).
- **Level 3 – Operational**: Implements catalog provisioning, Copilot reference alignment, and reporting requirements (Sections 5–7).

---

## 3. AI Credits Accounting Model

### 3.1 Unit Definition

A conforming implementation MUST define:

- `1 AIC = 0.01 USD`
- `AIC = USD / 0.01`

### 3.2 Token Classes

Implementations MUST support the following token classes for cost calculation when available:

- Input tokens
- Output tokens
- Cache read tokens
- Cache write tokens
- Reasoning tokens

### 3.3 Per-Invocation Cost Formula

For a single invocation, implementations MUST compute USD as:

```text
cost_usd =
  (input_tokens × input_price_per_token) +
  (output_tokens × output_price_per_token) +
  (cache_read_tokens × cache_read_price_per_token) +
  (cache_write_tokens × cache_write_price_per_token) +
  (reasoning_tokens × reasoning_price_per_token)
```

A conforming implementation MUST derive AIC as:

```text
aic = cost_usd / 0.01
```

### 3.4 Price Fallback Rules

If a model entry omits optional price fields, implementations MUST apply the following fallback behavior:

- `cache_read_price_per_token` defaults to `input_price_per_token`
- `cache_write_price_per_token` defaults to `input_price_per_token`
- `reasoning_price_per_token` defaults to `output_price_per_token`

### 3.5 Provider-Specific Input Handling

When an invocation record provides the optional, forward-compatible `input_tokens_include_cache` field, implementations MUST use it instead of inferring cache semantics from the provider. If it is `true`, cache-read and cache-write tokens are subsets of `input_tokens` and MUST be subtracted before applying the full input price. If it is `false`, the cache fields are additive and MUST NOT be subtracted.

Legacy records without `input_tokens_include_cache` retain these provider defaults:

| Provider (normalized) | Notes |
|-----------------------|-------|
| `anthropic` | Direct Anthropic API |
| `openai` | Direct OpenAI API |
| `azure-openai` / `azure_openai` | Azure-hosted OpenAI |
| `github-copilot` (and aliases `github`, `copilot`, `github_models`) | Treat cache-read tokens as included to preserve legacy repricing behavior when the optional field is absent. |

### 3.6 Aggregation

For grouped runs (for example, episodes), implementations MUST aggregate AIC by summing per-invocation AIC values.

---

### 3.7 Entities

This subsection defines the core data entities used throughout this specification. Implementations MUST model these entities (or equivalents) when processing AIC calculation, catalog lookup, and reporting. Cross-reference: §3.3 uses `InvocationRecord`, §3.3 and §4 use `PricingCatalogEntry`, and §7 reporting consumes `AICRecord`.

#### AICRecord

An `AICRecord` represents the computed AIC result for a single workflow run or reporting period.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `run_id` | `string` | **MUST** | Unique identifier for the workflow run (e.g., GitHub Actions run ID). |
| `total_aic` | `number` | **MUST** | Sum of all per-invocation AIC values for the run. MUST be `≥ 0`. |
| `invocations` | `array<InvocationRecord>` | **MUST** | Ordered list of per-invocation records comprising this run's total. |
| `computed_at` | `string` (ISO 8601) | **MUST** | UTC timestamp when this record was computed. |
| `catalog_version` | `string` | SHOULD | Version or content hash of the `PricingCatalogEntry` set used for computation. |

#### PricingCatalogEntry

A `PricingCatalogEntry` represents a single model's pricing data within the `models.json` catalog (see §4 for catalog format).

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `model_id` | `string` | **MUST** | Canonical model identifier (normalized; see §4.4). |
| `provider` | `string` | **MUST** | Normalized provider key (see §4.3). |
| `input` | `number` | **MUST** | Price per input token in USD. |
| `output` | `number` | **MUST** | Price per output token in USD. |
| `cache_read` | `number` | SHOULD | Price per cache-read token in USD. Defaults to `input` when absent (§3.4). |
| `cache_write` | `number` | SHOULD | Price per cache-write token in USD. Defaults to `input` when absent (§3.4). |
| `reasoning` | `number` | SHOULD | Price per reasoning token in USD. Defaults to `output` when absent (§3.4). |

#### InvocationRecord

An `InvocationRecord` represents a single LLM API call and its associated token usage and cost.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `invocation_id` | `string` | **MUST** | Unique identifier for this invocation (e.g., request ID or sequential index). |
| `model_id` | `string` | **MUST** | Model identifier used for this invocation (pre-normalization value accepted; normalized for catalog lookup). |
| `provider` | `string` | **MUST** | Provider key for this invocation (normalized before catalog lookup; see §4.3). |
| `input_tokens` | `integer` | **MUST** | Reported input token count (before §3.5 adjustment). |
| `output_tokens` | `integer` | **MUST** | Reported output token count. |
| `cache_read_tokens` | `integer` | SHOULD | Cache-read token count; `0` when not reported. |
| `cache_write_tokens` | `integer` | SHOULD | Cache-write token count; `0` when not reported. |
| `reasoning_tokens` | `integer` | SHOULD | Reasoning token count; `0` when not reported. |
| `cost_usd` | `number` | **MUST** | Computed cost in USD using §3.3 formula. |
| `aic` | `number` | **MUST** | `cost_usd / 0.01` (see §3.3). |

---

## 4. Pricing Catalog Format (`models.json`)

### 4.1 Top-Level Structure

A conforming catalog MUST be valid JSON with this structure:

```json
{
  "providers": {
    "provider-name": {
      "models": {
        "model-id": {
          "cost": {
            "input": "...",
            "output": "...",
            "cache_read": "...",
            "cache_write": "...",
            "reasoning": "..."
          }
        }
      }
    }
  }
}
```

### 4.2 Required and Optional Fields

For each model:

- `cost.input` MUST be present.
- `cost.output` MUST be present.
- `cost.cache_read` MAY be present.
- `cost.cache_write` MAY be present.
- `cost.reasoning` MAY be present.

Cost values MUST be decimal numbers encoded as strings and interpreted as USD per token.

### 4.3 Provider Keys

Provider keys MUST be lowercase identifiers. For Copilot-backed pricing, the canonical provider key MUST be `github-copilot`.

### 4.4 Model Lookup Normalization

A conforming implementation MUST normalize provider and model identifiers for lookup by trimming whitespace and applying case-insensitive comparison. An implementation SHOULD support compatibility matching between punctuation variants (for example, `.` and `_` compared to `-`) and provider-scoped prefix fallback.

A conforming implementation MUST recognize the following provider name aliases and normalize them to `github-copilot` before catalog lookup:

| Input value | Normalized to |
|-------------|---------------|
| `github` | `github-copilot` |
| `copilot` | `github-copilot` |
| `github_models` | `github-copilot` |

The `github_models` alias is written by the AWF proxy for Copilot engine runs and MUST be recognized so that AIC is computed and emitted for all Copilot-backed engines.

---

## 5. Catalog Provisioning and Synchronization

### 5.1 Embedded Runtime Catalogs

gh-aw implementations MUST provide synchronized pricing catalogs at:

- `pkg/cli/data/models.json`
- `actions/setup/js/models.json`

These files SHALL represent the same pricing dataset.

### 5.2 Source and Refresh Expectations

Catalog refresh processes SHOULD use normalized upstream model inventories and SHOULD validate Copilot entries against authoritative GitHub Copilot model and billing documentation.

### 5.3 Change Control

Catalog updates MUST preserve JSON validity and MUST maintain backward-safe handling for historical model IDs that remain in the catalog but are absent from current live inventories.

### 5.4 Catalog Sync Failure Handling

A conforming implementation MUST define and enforce behavior for catalog synchronization failures. Specifically:

1. **Unreachable catalog source**: When the upstream pricing catalog source is unreachable during a sync operation, the implementation MUST fall back to the last successfully synchronized catalog version. The implementation MUST NOT silently use an empty or zero-cost catalog. The implementation MUST emit a diagnostic warning identifying the sync failure and the fallback version in use.

2. **Malformed catalog**: When a fetched catalog update fails JSON schema validation or contains non-numeric cost fields, the implementation MUST reject the update entirely, retain the previous valid catalog, and emit a validation error identifying the malformed field(s). The implementation MUST NOT partially apply a malformed update.

3. **Missing required fields**: When a catalog entry is missing required fields (`input` or `output` cost values), the implementation MUST treat that entry as invalid and MUST NOT compute AIC using zero or undefined costs for that model. The entry MUST be skipped or flagged, and a diagnostic MUST be emitted.

4. **Catalog version mismatch**: When the two required catalog paths (`pkg/cli/data/models.json` and `actions/setup/js/models.json`) diverge in content after a sync operation, the sync tooling/CI gate MUST treat this as a sync failure. The refresh operation MUST fail until consistency is restored. In the gh-aw repository:
   - CI MUST run `make validate-models-json-sync` (or an equivalent normalized-content comparison gate).
   - That gate MUST fail whenever these mirror files cease to represent the same JSON dataset.

### 5.5 Catalog Staleness SLA

A catalog that has not been refreshed from its upstream source within **7 days** is considered stale. Implementations SHOULD track the timestamp of the last successful catalog sync.

When the elapsed time since the last successful sync exceeds 7 days, the implementation SHOULD emit a re-sync warning. The warning MUST include:

- The timestamp of the last successful sync.
- The number of days elapsed since that sync.
- A recommendation to run the catalog refresh process.

This is a SHOULD-level norm; catalog staleness does not constitute a hard failure, but implementations MUST NOT suppress the warning if the threshold is exceeded. Implementations that cannot determine the last sync timestamp SHOULD treat the catalog as potentially stale and emit the warning unconditionally.

---

## 6. Copilot Billing Reference Requirements

Implementations and documentation that describe Copilot AIC behavior MUST reference:

- GitHub Copilot models documentation: <https://docs.github.com/en/copilot/concepts/about-github-copilot-models>
- GitHub Copilot models and pricing reference: <https://docs.github.com/en/copilot/reference/copilot-billing/models-and-pricing>
- GitHub Copilot plan and billing documentation: <https://docs.github.com/en/copilot/about-github-copilot/subscription-plans-for-github-copilot>

These references SHOULD be treated as the external billing-alignment sources for Copilot model pricing validation.

---

## 7. Reporting Requirements

A conforming implementation MUST expose AIC in runtime reporting outputs where cost metrics are emitted.

When AWF token-usage records provide finite, non-negative `ai_credits_this_response` values, reporting MUST prefer those per-request values. The chronologically last valid `ai_credits_total` is the reported run total. If later records omit or contain invalid reported values, reporting MUST surface a warning and extend the last valid total with available per-request values or legacy pricing until another valid cumulative total appears. Implementations MUST recompute AIC from token counts and catalog pricing for legacy records that omit these fields.

> [!NOTE]
> Token-usage files are diagnostic artifacts visible to the agent runtime. Their mirrored AIC fields improve usage summaries, but are not sufficient evidence to classify a provider failure as budget enforcement or to change retry, authentication, or safe-output behavior.
> This reporting contract does not alter per-run or daily guardrail enforcement inputs; those require a separate first-party accounting contract.

Implementations SHOULD provide:

- Per-run AIC values.
- Aggregated AIC values for grouped executions.
- Structured output fields suitable for machine parsing.

---

## 8. Compliance Testing

### 8.1 Test Suite Requirements

A conformance test suite MUST include at least the following test cases:

- **T-AIC-001**: Verify `1 AIC = 0.01 USD` conversion.
- **T-AIC-002**: Verify per-invocation AIC computation using all token classes.
- **T-AIC-003**: Verify cache-read subtraction behavior for providers that include cache-read tokens in input totals.
- **T-AIC-004**: Verify fallback pricing when optional cost fields are omitted.
- **T-AIC-005**: Verify `models.json` rejects missing required fields (`input`, `output`).
- **T-AIC-006**: Verify provider key normalization and `github`, `copilot`, and `github_models` to `github-copilot` mapping behavior.
- **T-AIC-007**: Verify catalog mirror consistency between CLI and setup runtime paths.
- **T-AIC-008**: Verify reporting outputs include per-run AIC values.

### 8.2 Compliance Checklist

| Requirement | Test ID | Level | Status |
|-------------|---------|-------|--------|
| Unit conversion (`1 AIC = 0.01 USD`) | T-AIC-001 | 1 | Required |
| Full token-class formula | T-AIC-002 | 1 | Required |
| Cache-read non-double-charge behavior | T-AIC-003 | 1 | Required |
| Optional price fallback behavior | T-AIC-004 | 1 | Required |
| Catalog schema conformance | T-AIC-005 | 2 | Required |
| Provider/model normalization behavior | T-AIC-006 | 2 | Required |
| Mirrored catalog consistency | T-AIC-007 | 3 | Required |
| AIC reporting visibility | T-AIC-008 | 3 | Required |

---

## 9. Daily AI Credits Guardrail

### 9.1 Purpose

The daily AI Credits guardrail is a per-workflow cumulative budget limit evaluated at action runtime. It compares historical usage with a threshold before the current agent runs. This section specifies threshold resolution, rolling-window membership, accounting-source precedence, conservative estimates, snapshot reuse, retries, and the alternative repo-memory backend.

### 9.2 Guardrail Configuration Field

The guardrail threshold is expressed through the `max-daily-ai-credits` frontmatter field in the workflow YAML source. When set, a conforming compiler MUST emit the resolved threshold as the `GH_AW_MAX_DAILY_AI_CREDITS` environment variable in the compiled workflow YAML. The daily guardrail check step MUST gate its execution on this variable being non-empty.

### 9.3 Normative Resolution Order

A conforming implementation MUST resolve the effective daily AI Credits threshold using the following precedence (highest to lowest):

1. **Frontmatter value** (`max-daily-ai-credits`): Resolved at compile time. Numeric values MUST be normalized to integer strings; suffix notation (`K`, `M`) MUST be expanded (e.g., `100M` → `100000000`). When present, the resolved value MUST be emitted literally as the `GH_AW_MAX_DAILY_AI_CREDITS` value.

2. **Imported workflow configuration** (`max-daily-ai-credits` from imported shared workflows): Resolved at compile time using a first-wins accumulation across all imported workflows. A conforming implementation MUST apply the first usable `max-daily-ai-credits` value found across imports when no frontmatter value is present on the main workflow. Imported values undergo the same normalization and validation rules as frontmatter values.

3. **Runtime organization variable** (`vars.GH_AW_DEFAULT_MAX_DAILY_AI_CREDITS`): A GitHub Actions `vars.*` expression resolved at action runtime by the GitHub Actions runner. A conforming implementation MUST NOT read this variable at compile time via the process environment; it MUST instead be embedded in the compiled YAML as a GitHub Actions expression evaluated by the runner.

4. **Built-in constant default** (`5000`): The fallback literal embedded in the GitHub Actions expression when the organization variable is unset. The value `5000` AIC represents the normative built-in default for the daily guardrail threshold.

A conforming implementation MUST NOT resolve any of these values from repository-local configuration files (e.g., `aw.json`).

### 9.4 Emitted Expression Form

When neither a frontmatter value nor an imported workflow configuration value is present, a conforming implementation MUST emit the following GitHub Actions expression as the `GH_AW_MAX_DAILY_AI_CREDITS` environment variable value in the compiled workflow:

```
${{ vars.GH_AW_DEFAULT_MAX_DAILY_AI_CREDITS || '5000' }}
```

This expression MUST be emitted verbatim (not pre-evaluated) so that the GitHub Actions runner resolves the organization variable at workflow execution time. The `'5000'` fallback ensures the guardrail is active by default when the organization variable is not configured.

### 9.5 Disable Sentinel

A `max-daily-ai-credits` frontmatter value of `-1` MUST disable the daily guardrail. When this sentinel is present:

- A conforming implementation MUST NOT emit `GH_AW_MAX_DAILY_AI_CREDITS` into the compiled workflow.
- The daily guardrail check step MUST be skipped at runtime (its `if:` condition evaluates to false).

### 9.6 Value Validation

A conforming implementation MUST enforce the following at compile time:

- Accept positive integers and positive numeric strings.
- Accept the suffix notation `K` and `M` (case-insensitive) as multipliers (×1,000 and ×1,000,000 respectively).
- Accept `-1` as the explicit disable sentinel.
- Accept GitHub Actions expression strings (verbatim, passed through for runtime evaluation).
- Reject integer values below `-1` with a compile-time validation error.
- Reject non-numeric, non-expression values.

### 9.7 Compliance Tests

| Test ID | Description | Requirement |
|---------|-------------|-------------|
| T-AIC-DG-001 | Frontmatter value emitted literally; no expression wrapper | §9.3 (1) |
| T-AIC-DG-002 | No frontmatter, no imports: emitted expression is `${{ vars.GH_AW_DEFAULT_MAX_DAILY_AI_CREDITS \|\| '5000' }}` | §9.4 |
| T-AIC-DG-003 | Frontmatter `-1` disables guardrail; no env var emitted | §9.5 |
| T-AIC-DG-004 | `K`/`M` suffix values expanded in emitted literal | §9.6 |
| T-AIC-DG-005 | Values below `-1` rejected at compile time | §9.6 |
| T-AIC-DG-006 | Runtime variable resolved by GitHub Actions runner, not compiler process | §9.3 (3) |
| T-AIC-DG-007 | Imported workflow `max-daily-ai-credits` used when no frontmatter value; frontmatter takes precedence over imports | §9.3 (2) |
| T-AIC-DG-008 | `workflow_call` event → guardrail skipped | §9.8 (1) |
| T-AIC-DG-009 | `repository_dispatch` event → guardrail skipped | §9.8 (1) |
| T-AIC-DG-010 | `workflow_dispatch` + absent/empty `GH_AW_WORKFLOW_DISPATCH_AW_CONTEXT` → guardrail skipped | §9.8 (2) |
| T-AIC-DG-011 | `workflow_dispatch` + `aw_context.event_type` in slash-command types → guardrail skipped | §9.8 (3) |
| T-AIC-DG-012 | `workflow_dispatch` + non-empty `aw_context.trigger_label` → guardrail skipped | §9.8 (3) |
| T-AIC-DG-013 | `GH_AW_HAS_SLASH_COMMAND=true` + matching event → guardrail skipped | §9.8 (4) |
| T-AIC-DG-014 | `GH_AW_HAS_SLASH_COMMAND=true` + non-matching event → guardrail enforced | §9.8 (4) |
| T-AIC-DG-015 | `GH_AW_HAS_LABEL_COMMAND=true` + matching event → guardrail skipped | §9.8 (5) |
| T-AIC-DG-016 | `GH_AW_HAS_LABEL_COMMAND=true` + non-matching event → guardrail enforced | §9.8 (5) |
| T-AIC-DG-017 | Complete fresh history is required; duplicate run IDs count once and current/old runs do not count | §9.9 |
| T-AIC-DG-018 | Trusted recorded snapshots match the completed attempt; estimates and stale/legacy observations are reinspected | §9.10 |
| T-AIC-DG-019 | Agent, Detection, and Evaluations accounting is validated independently without overlapping-source double counting | §9.11 |
| T-AIC-DG-020 | Every supported zero-accounting path satisfies its documented evidence conditions | §9.11 |
| T-AIC-DG-021 | Unresolved historical accounting substitutes the positive configured per-run maximum for the whole run | §9.12 |
| T-AIC-DG-022 | Transient reads retry at most three times; exhausted quota stops remain unverified and block enforced startup without estimates | §9.13, §9.14 |
| T-AIC-DG-023 | Workflow-history 404 uses repository-history fallback, not zero usage or an incomplete total | §9.9, §9.13 |
| T-AIC-DG-024 | Estimated credits are reported separately and equality with the threshold is exceeded | §9.14 |
| T-AIC-DG-025 | Repo-memory trust, timestamp filtering, duplicate selection, and permissive read behavior match the separate backend contract | §9.15 |
| T-AIC-DG-026 | A cancelled rerun without a verified producer carries usage only from the unique latest earlier executed job with a verified producer; missing accounting, ambiguous prior jobs, and other outcomes fail closed | §9.11 |
| T-AIC-DG-027 | Run/attempt-bound advisory Decisions failures use the failed-Evaluations accounting policy, preserve other component usage, and replace whole-run estimates on the next scan | §9.11 |
| T-AIC-DG-028 | Successful Decisions model-unavailable skips require coherent run/selected-attempt evidence and no answer records; other component usage and strict accounting precedence remain intact | §9.11 |

### 9.8 Guardrail Bypass Conditions

The runtime MUST skip the daily AI Credits guardrail check under the conditions specified in this section. These conditions represent user-initiated or command-driven invocations where throttling would block intentional user actions rather than automated background activity.

The following environment variables govern bypass evaluation at runtime:

| Variable | Source | Purpose |
|----------|--------|---------|
| `GITHUB_EVENT_NAME` | GitHub Actions runner | The GitHub event that triggered the current workflow run |
| `GH_AW_WORKFLOW_DISPATCH_AW_CONTEXT` | Centralized dispatch router | JSON payload present when the workflow was routed via `workflow_dispatch`; absent for direct manual triggers |
| `GH_AW_HAS_SLASH_COMMAND` | gh-aw compiler | Set to `"true"` when the compiled workflow includes a non-centralized slash command trigger |
| `GH_AW_HAS_LABEL_COMMAND` | gh-aw compiler | Set to `"true"` when the compiled workflow includes a non-centralized label command trigger |

**Condition 1 — Centralized or routed invocations.** When `GITHUB_EVENT_NAME` is `workflow_call` or `repository_dispatch`, the runtime SHALL skip the guardrail. These events indicate the workflow was invoked as a reusable sub-workflow or via programmatic repository dispatch; the initiating workflow is responsible for its own daily budget enforcement.

**Condition 2 — Manual `workflow_dispatch` without routing context.** When `GITHUB_EVENT_NAME` is `workflow_dispatch` and `GH_AW_WORKFLOW_DISPATCH_AW_CONTEXT` is absent or empty, the runtime SHALL skip the guardrail. This condition identifies a human who explicitly triggered the workflow from the GitHub UI or GitHub CLI; blocking such a run would defeat the purpose of a manual invocation.

**Condition 3 — Dispatch-routed command invocations.** When `GITHUB_EVENT_NAME` is `workflow_dispatch` and `GH_AW_WORKFLOW_DISPATCH_AW_CONTEXT` contains a valid JSON payload that identifies a command-driven run — specifically, when `aw_context.event_type` is one of `issue_comment`, `pull_request_review_comment`, or `discussion_comment`, or when `aw_context.trigger_label` is a non-empty string — the runtime SHALL skip the guardrail. These are slash or label command runs routed through the centralized dispatch mechanism.

> [!NOTE]
> When `GH_AW_WORKFLOW_DISPATCH_AW_CONTEXT` is present but contains malformed JSON, the runtime MUST skip the guardrail as a safe fallback, treating the run as a manual dispatch (Condition 2 semantics).

**Condition 4 — Non-centralized slash-command events.** When `GH_AW_HAS_SLASH_COMMAND` is `"true"` and `GITHUB_EVENT_NAME` is one of `issue_comment`, `pull_request_review_comment`, `discussion_comment`, `issues`, `pull_request`, or `discussion`, the runtime SHALL skip the guardrail. This condition covers slash-command triggers compiled directly into the workflow rather than routed through the centralized dispatcher.

**Condition 5 — Non-centralized label-command events.** When `GH_AW_HAS_LABEL_COMMAND` is `"true"` and `GITHUB_EVENT_NAME` is one of `issues`, `pull_request`, or `discussion`, the runtime SHALL skip the guardrail. This condition covers label-command triggers compiled directly into the workflow.

A conforming implementation MUST evaluate bypass conditions before checking the daily AIC total. When any bypass condition is satisfied, the implementation MUST NOT read or compare the daily AIC usage data for the current run.

### 9.9 Authoritative Window and Run Membership

The artifact backend MUST resolve the current workflow ID and obtain fresh completed-run history for that workflow in the same repository. Its rolling window includes runs whose `created_at` is at or after the scan's start time minus 24 hours. It MUST exclude the current run, deduplicate by run ID, and validate completed status, positive integer `run_attempt`, and parseable creation/update timestamps. The accounting unit is a run's latest completed metadata, not a separate sum of every rerun attempt.

The workflow-specific endpoint is tried first. A 404 MAY switch to repository history filtered by the resolved workflow ID; subsequent pages remain in that lookup mode. Repository-history requests include the 24-hour creation filter. Empty filtered pages MUST NOT prove completeness when their unfiltered source page is full. Completion requires an unfiltered page smaller than 100 runs or reaching an older-than-window timestamp. The implementation MUST fail rather than authorize a partial result if completion is not established within 25 pages.

Current-run lookup failures, missing or malformed history arrays, invalid run/workflow identities, unknown timestamps, incomplete attempt metadata, history failures, and pagination exhaustion MUST NOT use the per-run estimate: they do not establish which historical runs must be counted. Repository-history entries MUST expose valid workflow IDs before filtering. A snapshot cannot substitute for this history.

The current implementation applies a lower creation-time bound, not an additional future-time rejection, to Actions history. It relies on GitHub's creation-time ordering when an older run ends the history scan.

### 9.10 Scan Observations and Producer Trust

`aic-usage-scan-v2` is an optimization containing observations, not a certified daily total. Artifact restoration MUST validate matching workflow ID, workflow path, and producer repository. The currently supported trusted producer policies are:

| Producer event | Additional required evidence |
|---|---|
| `push`, `schedule`, `workflow_dispatch`, `issues`, `issue_comment` | Known default branch; producer head branch equals it; head repository equals the current repository |
| Existing `pull_request_target` producer | Matching workflow ID, path, and repository; no additional head-branch check in this compatibility path |
| Other events, including `pull_request` | Not trusted |

This describes existing restoration compatibility, not a recommendation to author `pull_request_target` workflows.

Restore excludes the current run, checks recent workflow history, and uses bounded repository history on workflow-history 404. Missing, expired, empty, or unusable snapshot artifacts are misses. API/permission/quota failures during restoration remain errors; exhausted transient restoration failures are not silently converted into a cache miss.

Workflow-history restoration lists up to ten recent completed runs within the 48-hour observation-retention window, then inspects artifacts from at most two trusted producers. Listed runs and inspected producers have separate bounds so untrusted runs do not hide a usable snapshot in the same page. Repository-history restoration inspects trusted producers on each page before requesting older pages and stops at the first usable snapshot. Snapshot discovery is limited to two history pages and two trusted producers; a bounded miss proceeds to authoritative accounting, not an incomplete total. Stopping restoration early does not establish a complete budget window: the subsequent guardrail MUST still obtain a complete authoritative run listing and resolve every uncached or mismatched attempt.

Accepted observation lines MUST have version 2, component-coverage version 2, matching repository/workflow, positive integer run/attempt IDs, finite non-negative AIC, valid creation/update/observation timestamps, and observation age between zero and 48 hours inclusive. Source is `recorded`, `estimated`, or absent for compatibility. Malformed or invalid lines are ignored. Conflicting recorded values for the same attempt/update identity MUST fail, not choose a lower value.

When merging observations, a newer observation normally wins. Recorded accounting MUST NOT be overwritten by an estimate for the same attempt and creation/update identity. During a fresh scan, reuse additionally requires exact attempt, creation, and update matches and `source: recorded`. Estimates, provenance-less entries, old coverage versions, mismatched reruns, and invalid/expired observations MUST be reinspected.

Individually resolved observations are persisted even if a later historical read fails. Their presence MUST NOT authorize a later scan without another complete authoritative listing. Concurrent incomplete snapshots cause cache misses, not zero usage.

### 9.11 Per-Run Accounting and Source Fallbacks

The runtime MUST list billable jobs with all attempts, validate completed job metadata, and independently account for Agent, Detection, and Evaluations. It normalizes display names to component IDs, rejects ambiguous same-attempt jobs, and retains the relevant executed job when a failed-only rerun preserves a successful earlier attempt. A complete empty job list or a list containing only skipped billable jobs proves zero for the run without an artifact read.

Otherwise, the runtime selects the latest matching `usage` or invocation-prefixed `*-usage` artifact. Missing identity, expiry, missing creation time, or incomplete component coverage is unresolved accounting, not zero. An aggregate artifact MUST cover each executed component's start/end timestamps. On reruns, unless the cancelled-attempt exception below applies, the original component producer artifact MUST also exist, be unexpired, and have a creation time from the job start through strictly before its completion plus one second. Repacking old data with a new aggregate timestamp MUST NOT establish coverage.

If the selected latest component job is `cancelled` and its attempt has no verifiable producer artifact, the runtime MAY count carried-forward usage only when the latest earlier executed job for that component is unambiguous and has a verifiable, unexpired producer artifact created within that job's start/completion bounds, and the selected aggregate accounting completely covers that attempt. The runtime MUST warn that usage from the cancelled attempt may be unaccounted for. Missing accounting, ambiguous jobs on the latest earlier attempt, or an unverifiable producer for that attempt remains unresolved accounting; it MUST NOT be treated as zero or carried forward from an arbitrary older job. This fallback MUST NOT apply to any outcome other than `cancelled`. A verified producer for the selected attempt takes precedence and does not require older attempts to be unambiguous.

Within each component, select the first non-empty accounting file in this order:

| Component | Source precedence |
|---|---|
| Agent | `agent/token_usage.jsonl`, `agent_usage.jsonl`, `agent_usage.json` |
| Detection | `detection/token_usage.jsonl`, `detection_usage.jsonl` |
| Evaluations | `evals/token_usage.jsonl` |

The Evaluations producer collects the runner-visible log exported by AWF as `AWF_TOKEN_USAGE_LOG`, including an empty file, before uploading its accounting artifact. If that file is unavailable, it falls back to non-empty logs in the configured firewall audit and proxy-log directories. Missing accounting MUST NOT create a zero-usage placeholder. An empty collected log does not by itself prove zero usage; the component accounting and zero-accounting fallbacks below still apply.

The selected source is parsed strictly. A malformed or invalid selected source MUST NOT be rescued by a lower-priority summary. An unreadable higher-priority source MUST NOT be silently skipped in favor of a lower-priority source. Different component values are added once; raw accounting and overlapping engine summaries MUST NOT both contribute.

Strict accounting validates supplied numeric fields as finite non-negative values, ignores identical repeated `(event, request_id)` records, and rejects conflicting repetitions. `ai_credits_this_response` takes precedence when present, including explicit zero. Otherwise positive `ai_credits`/`aiCredits` precede positive `aic`, then provider/model token-pricing computation from Section 3 is used. A valid explicit zero counts as an accounting observation; tokens still require resolvable pricing when the other explicit aliases are zero. Only a positive explicit alias or authoritative `ai_credits_this_response` can bypass pricing for token-bearing records. Missing pricing on the selected token-pricing path, malformed data, non-finite totals, or a selected source without usable observations is unresolved accounting.

The following additional component zero-accounting fallbacks are supported. Other billable components MUST still be accounted for:

| Condition | Required evidence and precedence |
|---|---|
| Skipped component | Authoritative job conclusion is `skipped` |
| Runner never assigned | Job failed/cancelled; explicit null/zero runner ID, null runner name, and an explicit empty steps array |
| Empty Detection raw accounting | Verified component/producer coverage and empty `detection/token_usage.jsonl`; this overrides a legacy summary, even if nonzero |
| Failed Agent with empty raw accounting | Verified coverage, failed conclusion, no selected non-empty accounting candidate, and empty authoritative raw file |
| Legacy pre-harness Agent startup failure | Verified coverage, failed conclusion, no selected accounting, and bounded same-job `agent-stdio.log` containing a startup-failure marker but no harness marker |
| Legacy sample replay | Verified coverage, successful Agent, empty raw file, no selected accounting, and verified legacy log marking sample replay |
| Explicit execution-not-started artifact | No selected accounting and version-1 component execution evidence matching run ID, job attempt, component, and `state: not_started` |
| Legacy failed Agent before accounting | Verified coverage, no selected accounting, failed conclusion, missing raw file, no execution-evidence file, and no inspected legacy Agent artifact |
| Failed Evaluations before accounting | Verified coverage, failed conclusion, and no selected accounting |
| Advisory Decisions failure before accounting | Verified coverage, successful job conclusion, no selected accounting, and `evals/evals_status.json` identifying the same run and selected job attempt with engine `decisions`, status `not_evaluated`, reason `judge_failed`, execution outcome `failure`, and execution state `started` |
| Unavailable Decisions model before accounting | Verified coverage, successful job conclusion, no selected accounting, and `evals/evals_status.json` identifying the same run and selected job attempt with engine `decisions`, status `not_evaluated`, reason `model_unavailable`, execution outcome `success`, and execution state `model_unavailable`; both `evals.jsonl` and `evals/evals.jsonl` MUST be missing or empty |

Legacy Agent evidence is inspected only when no non-empty Agent accounting candidate exists. The legacy artifact must be unexpired, timestamp-bound to its job, and contain a regular log no larger than 10 MiB. Detection's empty-raw exception is checked before lower-priority candidate selection. The remaining zero fallbacks MUST NOT override a malformed non-empty or higher-priority unreadable source.

These are implemented evidence/compatibility policies, not a proof that a provider never charged a request. In particular, missing-accounting failure heuristics and absent provider-response usage cannot certify external billing. The advisory Decisions fallback applies the existing failed-Evaluations policy to the judge outcome when `continue-on-error` leaves its wrapper job successful. The unavailable-model fallback covers a rejected Decisions request with no answers, not arbitrary `not_evaluated` markers or missing usage. Mismatched identity, engine, reason, outcome, or state, and non-empty or unreadable answer files MUST NOT establish that fallback. Both Decisions fallbacks emit a warning that provider billing remains unverified and MUST NOT override non-empty or unreadable accounting.

### 9.12 Conservative Whole-Run Maximum Fallback

Let `M` be the finite positive value parsed from `GH_AW_MAX_AI_CREDITS`. The compiler uses the resolved configured per-run maximum when present, otherwise the runtime `GH_AW_DEFAULT_MAX_AI_CREDITS` expression with built-in fallback `1000`. A disabled, missing, invalid, or non-positive parsed maximum does not provide an estimate.

When historical accounting remains unresolved after its available evidence paths, the artifact scan MAY substitute `M` for the **whole run**, marking `source: estimated` and reason `max_ai_credits_fallback`. It MUST NOT add `M` on top of partially recovered component usage, substitute zero, or present the estimate as measured consumption. This covers missing/malformed accounting, invalid coverage, unknown pricing, no-status accounting errors, and exhausted eligible transient reads.

No HTTP 401/403/404/429, quota/reserve stop, or explicit `Retry-After` restriction is eligible. Without positive `M`, unresolved accounting MUST fail the scan. Estimates are retried on subsequent scans and replaced by recorded values when available; they stop contributing when their runs leave the window.

`M` is a conservative policy assumption, not an independently verified upper bound on every historical run. Historical configuration changes, multiple job/attempt budgets, external billing, and incomplete observations can invalidate an actual-charge upper-bound assumption. The computation MUST expose estimated credits separately rather than claim an exact daily bill.

### 9.13 Bounded Read Recovery and Quota Handling

Eligible HTTP 5xx and recognized network/timeout failures MUST use at most three total attempts per read, with one-second and two-second delays before the second and third attempts. Fetch attempts MUST create fresh timeout signals and cover JSON/stream body consumption, not only receipt of headers. Interrupted artifact streams MUST restart into truncated temporary files and MUST NOT publish partial accounting. Successful earlier reads, the redirect lookup, and the whole scan MUST NOT be replayed as a retry. A history 404 MUST be observed before switching endpoints; repository retries MUST NOT repeat that workflow lookup.

Permission failures, HTTP 429, rate-limited 403 responses, explicit `Retry-After`, and the API reserve circuit breaker MUST stop individual read retries without estimating through the restriction. The reserve is 100 remaining requests by default. Observe GitHub business responses, including rejected responses and artifact redirect requests, before retry/fallback decisions. Signed-storage responses do not count as GitHub API quota observations and MUST NOT receive the GitHub authorization header.

On original run attempt 1 only, a quota-blocked snapshot restore or authoritative scan MAY defer once until the later relevant reset or `Retry-After` deadline, plus one second. The failed phase's elapsed time and wait together MUST fit ten minutes; a timer waking more than one minute beyond that budget MUST fail closed. Permission/authentication failures, unknown reset deadlines, longer waits, and renewed exhaustion MUST block startup explicitly. Recovery restarts the complete read-only phase; an authoritative scan MUST refresh current-run identity and its entire rolling-window listing before reusing matching recorded observations. Neither deferral nor a partial snapshot grants `under_budget`, starts billable work, reruns the workflow, or changes credentials. Each phase has its own bounded deferral; the workflow's activation timeout remains authoritative.

Retry-deadline reporting MUST honor explicit `Retry-After` and relevant quota-reset deadlines. The primary reset is relevant only for primary exhaustion or a verified stop at the configured reserve. A secondary restriction with healthy primary quota MUST use a valid `Retry-After` deadline or fail closed; an unrelated primary reset cannot authorize recovery or extend its wait. Ordinary 5xx responses carrying a non-exhausted `x-ratelimit-reset` MUST NOT produce a "must not retry before" message based on that header alone. Diagnostics MUST omit credentials, signed URLs, response bodies, and arbitrary error payloads from new retry/cache-decision logs.

Retry, recovery, and stop diagnostics identify the read using a fixed operation label: `current_run`, `workflow_history`, `repository_history`, `run_jobs`, `artifact_list`, `artifact_redirect`, or `artifact_download` (`unknown` for unlabeled callers). Terminal read failures retain bounded attempt counts, HTTP status when available, and a fixed stop reason. Guardrail failure outputs use this metadata instead of the API error payload, including through artifact transport wrappers, and state that accounting remains unverified and agent startup is blocked. Attribution does not change retry limits, quota restrictions, artifact validation, or accounting decisions.

### 9.14 Aggregation and Outputs

After every authoritative candidate has been resolved, sum its selected measured or estimated value exactly once, including confirmed-zero runs in run counts and statistics. `daily_ai_credits_total` is this sum; `daily_ai_credits_estimated` is the sum restricted to estimated runs. Every supplied run value and aggregate total MUST be finite and non-negative; overflow MUST fail accounting before publishing a total or threshold decision, for either backend. For positive threshold `L`, totals strictly below `L` produce `under_budget`; totals at or above `L` produce `exceeded`, set `daily_ai_credits_exceeded: true`, and gracefully skip the agent.

An unresolved scan MUST NOT report `under_budget` from a partial total. It calls `core.setFailed` and reports `structural_error` for missing credentials, HTTP 401/404, or a non-rate-limited 403; other caught accounting failures report `transient_error`. A marked quota/reserve stop takes precedence over the HTTP resource/permission classification and reports `quota_blocked` without calling `core.setFailed`; the total and estimated outputs remain empty and `daily_ai_credits_exceeded` remains `false`. Enforce mode MUST gate agent startup on this status; an explicitly configured fail-open mode MAY continue, but MUST NOT relabel the result as complete, under budget, or exceeded. Disabled or bypassed checks report `disabled` or `skipped` without accounting.

`continue-on-error: true` is an explicit Actions step-level fail-open policy: it may allow continuation despite an unknown total, but MUST NOT relabel the accounting result as complete or under budget. Summary-rendering failures are outside the accounting state machine; they may change step failure reporting after totals were computed.

### 9.15 Repo-Memory Backend

`backend: repo-memory` is a separate accounting source, not an automatic fallback after artifact/API failure. Its ledger must be marked trusted by the clone step, unless `GH_AW_ALLOW_INSECURE_REPO_MEMORY_AIC=true` explicitly accepts an insecure ledger. A directory is required.

When a successful clone step confirms the branch is absent, activation MUST bootstrap with the authoritative artifact/API accounting contract above, not trust the newly created local orphan or assume zero usage. The trusted downstream ledger writer records a repository/workflow-scoped coverage start alongside the ledger. Trusted branches with missing, invalid, future-dated, or less-than-24-hour coverage metadata also require artifact accounting. This migrates existing ledgers without discarding recent historical usage. Bootstrap requires an Actions-read token and preserves all artifact accounting, quota, threshold, and fail-closed rules; an unclassified clone failure never authorizes bootstrap.

After the trusted coverage start is at least 24 hours old, activation uses the cloned ledger without requiring an artifact API token. The coverage marker is maintained by the trusted writer after replacing agent-supplied ledger content with the branch snapshot; it is not accepted from agent output. Restoring or initializing an empty branch alone does not establish a full rolling window. The explicit insecure override retains its existing ledger-only behavior.

The selected daily AIC memory's trusted writer persists date-bucket JSONL shards and hash-scoped JSON coverage markers together, independently of agent-memory extension and glob filters, only after the compiler-owned reset step successfully removes agent-supplied ledger files. Ordinary memory files and memories without that trusted writer retain their configured filters; file-size, file-count, and patch-size limits still apply.

Read current and previous UTC-day JSONL shards, retaining version-1 records for the same repository/workflow, positive integer run IDs, finite non-negative AIC, and timestamps within the inclusive preceding-24-hour interval ending at the read time. For each run ID, the greatest timestamp wins; equal timestamps use the later encountered line. Sum these records once and report estimated credits as zero.

The current reader ignores malformed/invalid records and missing **or unreadable** shards. An empty readable result therefore produces zero. Unlike the artifact backend, it does not establish completeness through fresh Actions history or repair gaps with per-run maximum estimates. This availability/trust trade-off MUST NOT be described as equivalent fail-closed accounting.

The writer uses staged usage files and the permissive usage parser, appending timestamped version-1 entries. It is not the strict completed-component artifact scanner. Ledger append failures surface as errors; missing writer identity or an invalid computed value is warned and skipped.

### 9.16 Native Contract Coverage and Validation Boundary

Focused Vitest suites exercise the runtime behavior described in Sections 9.9–9.15, including history completeness, snapshot trust and reinspection, component accounting, fallback decisions, API retries and quota handling, and repo-memory ledger rules. The spec-derived fixed-output contract cases are in `actions/setup/js/daily_aic_spec_conformance.test.cjs`; quota-blocked cases verify empty totals, `exceeded: false`, and no activation failure, while compiler tests verify enforced startup gating and configured fail-open behavior. These tests are executable conformance examples, not a proof of provider billing or GitHub API behavior.

---

## 10. Per-Run AI Credits Budget

### 10.1 Purpose

The per-run AI Credits budget is a per-invocation credit limit applied by the AWF firewall to each individual workflow run. It prevents a single run from consuming excessive AI Credits regardless of daily aggregate usage. This section specifies the normative resolution order for the budget threshold and the mechanism by which the runtime default is resolved.

### 10.2 Budget Configuration Field

The budget threshold is expressed through the `max-ai-credits` frontmatter field in the workflow YAML source. When set, a conforming compiler MUST bake the resolved integer threshold directly into the AWF firewall configuration JSON emitted by the compiled workflow. When not set, a conforming implementation MUST resolve the threshold at action runtime using the mechanism specified in §10.4.

### 10.3 Normative Resolution Order

A conforming implementation MUST resolve the effective per-run AI Credits budget using the following precedence (highest to lowest):

1. **Frontmatter value** (`max-ai-credits`): Resolved at compile time. Numeric values MUST be normalized to integers; suffix notation (`K`, `M`) MUST be expanded (e.g., `1M` → `1000000`). When present, the resolved integer MUST be baked into the AWF firewall config JSON at compile time.

2. **Imported workflow configuration** (`max-ai-credits` from imported shared workflows): Resolved at compile time using a first-wins accumulation across all imported workflows. A conforming implementation MUST apply the first usable `max-ai-credits` value found across imports when no frontmatter value is present on the main workflow. Imported values undergo the same normalization rules as frontmatter values.

3. **Runtime organization variable** (`vars.GH_AW_DEFAULT_MAX_AI_CREDITS`): A GitHub Actions `vars.*` expression resolved at action runtime by the GitHub Actions runner. A conforming implementation MUST NOT read this variable at compile time via the process environment; it MUST instead embed a GitHub Actions expression evaluated by the runner at execution time.

4. **Built-in constant default** (`1000`): The fallback literal embedded in the GitHub Actions expression when the organization variable is unset. The value `1000` AIC represents the normative built-in default for the per-run budget.

A conforming implementation MUST NOT resolve any of these values from repository-local configuration files (e.g., `aw.json`).

### 10.4 Runtime Resolution Mechanism

When neither a frontmatter value nor an imported workflow configuration value is present, a conforming implementation MUST emit a runtime patch step that applies the following GitHub Actions expression to the AWF firewall configuration JSON:

```
${{ vars.GH_AW_DEFAULT_MAX_AI_CREDITS || '1000' }}
```

This expression MUST be embedded in the compiled YAML (not pre-evaluated at compile time) so that the GitHub Actions runner resolves the organization variable at workflow execution time. The `'1000'` fallback ensures the budget is active by default when the organization variable is not configured.

### 10.5 Disable Sentinel

A `max-ai-credits` frontmatter value of `-1` MUST disable the per-run budget. When this sentinel is present at runtime (via the organization variable resolving to `-1`), a conforming implementation MUST disable AWF budget enforcement and omit the `maxAiCredits` field from the firewall configuration. Budget notices are controlled separately by `sandbox.agent.token-steering`; with AWF releases after v0.29.11 they remain on by default when the budget is disabled.

For `vars.GH_AW_DEFAULT_MAX_AI_CREDITS`, the current gh-aw runtime patch does not implement the `-1` sentinel: its digits-only validation rejects `-1` and restores the built-in default of `1000`. Therefore, the current implementation does not meet the runtime-sentinel requirement above. T-AIC-PR-003 remains a required conformance test, but is not currently satisfied by gh-aw ([#77140](https://github.com/github/gh-aw/issues/77140)).

### 10.6 Value Validation

A conforming implementation MUST enforce the following at compile time when a frontmatter value is present:

- Accept positive integers and positive numeric strings.
- Accept the suffix notation `K` and `M` (case-insensitive) as multipliers (×1,000 and ×1,000,000 respectively).
- Accept `-1` as the explicit disable sentinel.
- Reject integer values below `-1` with a compile-time validation error.
- Reject non-numeric values.

### 10.7 Compliance Tests

| Test ID | Description | Requirement |
|---------|-------------|-------------|
| T-AIC-PR-001 | Frontmatter value baked into AWF config JSON at compile time | §10.3 (1) |
| T-AIC-PR-002 | No frontmatter, no imports: emitted expression is `${{ vars.GH_AW_DEFAULT_MAX_AI_CREDITS \|\| '1000' }}` | §10.4 |
| T-AIC-PR-003 | Org variable `-1` disables budget enforcement at runtime (not yet implemented by gh-aw) | §10.5 |
| T-AIC-PR-004 | `K`/`M` suffix values expanded in compile-time literal | §10.6 |
| T-AIC-PR-005 | Values below `-1` rejected at compile time | §10.6 |
| T-AIC-PR-006 | Runtime variable resolved by GitHub Actions runner, not compiler process | §10.3 (3) |
| T-AIC-PR-007 | Imported workflow `max-ai-credits` used when no frontmatter value; frontmatter takes precedence over imports | §10.3 (2) |

---

## Appendices

### Appendix A: Worked Example

This example assumes the provider bundles cache-read tokens in the reported input total, so §3.5 applies.

Given:

- Input (raw): 1000 tokens at $0.000003/token
- Output: 200 tokens at $0.000015/token
- Cache read: 400 tokens at $0.0000003/token
- Cache write: 50 tokens at $0.00000375/token
- Reasoning: 25 tokens at $0.000015/token

Step 1 — Apply §3.5: net input = 1000 − 400 = **600 tokens**

Step 2 — Per-class cost:

```text
input:       600 × 0.000003    = 0.0018000
output:      200 × 0.000015    = 0.0030000
cache_read:  400 × 0.0000003   = 0.0001200
cache_write:  50 × 0.00000375  = 0.0001875
reasoning:    25 × 0.000015    = 0.0003750
```

Result:

```text
cost_usd = 0.0054825
aic = 0.54825
```

### Appendix B: Error Conditions

Conforming implementations SHOULD surface explicit validation errors for:

- Invalid `models.json` structure.
- Non-numeric cost values.
- Missing required model cost fields.
- Unknown provider/model pairs with no fallback match.

### Appendix C: Security and Integrity Considerations

Pricing catalogs are configuration inputs. Implementations MUST:

- Treat catalog updates as controlled changes and gate all updates through the project's standard change-control workflow.
- Validate catalog source provenance before applying any update; the source origin MUST be confirmed as an authorized upstream before the catalog is consumed.
- Avoid silently mutating cost values at runtime.
- Review catalog changes for correctness and billing alignment before merging; reviews MUST be performed by at least one team member with knowledge of the active model pricing.

---

## References

### Normative References

- **[RFC 2119]** Key words for use in RFCs to Indicate Requirement Levels. <https://www.ietf.org/rfc/rfc2119.txt>

### Informative References

- **[GH-AW-COST]** Cost Management reference. <https://github.github.com/gh-aw/reference/cost-management/>
- **[GH-COPILOT-MODELS]** About GitHub Copilot models. <https://docs.github.com/en/copilot/concepts/about-github-copilot-models>
- **[GH-COPILOT-BILLING-MODELS]** GitHub Copilot models and pricing. <https://docs.github.com/en/copilot/reference/copilot-billing/models-and-pricing>
- **[GH-COPILOT-BILLING-PLANS]** Subscription plans for GitHub Copilot. <https://docs.github.com/en/copilot/about-github-copilot/subscription-plans-for-github-copilot>
- **[MODELS-DEV]** models.dev API index. <https://models.dev/api.json>
- **[GH-AW-COMPONENT-COVERAGE]** Daily AIC component accounting implementation and focused compliance tests. <https://github.com/github/gh-aw/blob/main/actions/setup/js/daily_aic_component_coverage.cjs>, <https://github.com/github/gh-aw/blob/main/actions/setup/js/daily_aic_component_coverage.test.cjs>

---

<a id="sync-notes"></a>
## Sync Notes

The canonical gh-aw mirror files for this specification are:

- `pkg/cli/data/models.json`
- `actions/setup/js/models.json`

The repository validation target for mirror consistency is `make validate-models-json-sync`.

This specification MUST be revalidated when any of the following occurs:

1. A new model or provider entry is added to either mirror.
2. A pricing value or billing-related field changes in either mirror.
3. This specification is incremented to a new minor version.

---

<a id="change-log"></a>
## Change Log

### Version 1.5.3 (2026-11-10)

- **Clarified**: A marked quota/reserve stop reports `quota_blocked` with empty totals and `exceeded: false`; enforced startup is gated without converting the unknown total into a failed or exceeded accounting result.
- **Updated**: Spec-conformance documentation to identify the quota-blocked output and compiler startup-gating checks, while preserving configured fail-open behavior.

### Version 1.5.2 (2026-11-05)

- **Added**: Section 9.11 and compliance test T-AIC-DG-028 specify the run/selected-attempt-bound Decisions model-unavailable fallback, absence-of-answer evidence, and fail-closed accounting boundaries.
- **Clarified**: The advisory `judge_failed` failure/started policy remains unchanged; unavailable-model skips do not certify external provider billing.

### Version 1.5.1 (2026-10-12)

- **Updated**: Section 9.11 and compliance test T-AIC-DG-026 specify the verified prior-attempt fallback for cancelled reruns and its fail-closed boundaries.
- **Added**: A bounded TLA+ model for cancelled-rerun producer verification and carried-forward accounting.

### Version 1.5.0 (2026-10-12)

- **Added**: Sections 9.9-9.16 specify authoritative history, scan provenance, component/source precedence, every implemented zero/max-credit fallback, bounded transient recovery, output decisions, and the separate repo-memory contract.
- **Added**: Compliance tests T-AIC-DG-017 through T-AIC-DG-025 and spec-derived fixed-output Vitest contract cases.
- **Clarified**: Estimates are policy assumptions, not verified historical billing upper bounds. The repo-memory reader's permissive behavior is not equivalent to artifact-backend fail-closed accounting.

### Version 1.4.0 (2026-06-09)

- **Added**: §9.8 — Guardrail Bypass Conditions. Specifies the five normative runtime conditions under which a conforming implementation MUST skip the daily AI Credits guardrail: (1) `workflow_call` or `repository_dispatch` events; (2) manual `workflow_dispatch` without routing context; (3) dispatch-routed slash or label command invocations; (4) non-centralized slash-command events with `GH_AW_HAS_SLASH_COMMAND=true`; (5) non-centralized label-command events with `GH_AW_HAS_LABEL_COMMAND=true`.
- **Updated**: §9.7 — Added compliance tests T-AIC-DG-008 through T-AIC-DG-016 covering each bypass condition and its negative case.
- **Updated**: Version and publication metadata to 1.4.0.

### Version 1.3.0 (2026-06-09)

- **Updated**: §9.3 — Expanded the Daily AI Credits Guardrail resolution order from three levels to four. Added "Imported workflow configuration" as the second priority (between frontmatter and the runtime org variable), documenting that a conforming implementation MUST apply the first usable `max-daily-ai-credits` value found across imported shared workflows when no frontmatter value is present on the main workflow.
- **Updated**: §9.4 — Clarified that the runtime expression is emitted only when neither frontmatter nor imported config provides a value.
- **Updated**: §9.7 — Added compliance test T-AIC-DG-007 asserting imported workflow `max-daily-ai-credits` resolution.
- **Updated**: §10.3 — Expanded the Per-Run AI Credits Budget resolution order from three levels to four. Added "Imported workflow configuration" as the second priority, documenting that a conforming implementation MUST apply the first usable `max-ai-credits` value found across imported shared workflows when no frontmatter value is present.
- **Updated**: §10.4 — Clarified that the runtime patch step is emitted only when neither frontmatter nor imported config provides a value.
- **Updated**: §10.7 — Added compliance test T-AIC-PR-007 asserting imported workflow `max-ai-credits` resolution.
- **Updated**: Version and publication metadata to 1.3.0.

### Version 1.2.0 (2026-06-09)

- **Added**: Section 10 — Per-Run AI Credits Budget. Specifies the normative three-level resolution order (frontmatter → `vars.GH_AW_DEFAULT_MAX_AI_CREDITS` at runtime → built-in constant `1000`) and the required runtime patch mechanism emitted by conforming compilers when no frontmatter value is set.
- **Added**: §10.7 compliance test matrix (T-AIC-PR-001 through T-AIC-PR-006).
- **Updated**: Table of contents and publication metadata to 1.2.0.

### Version 1.1.0 (2026-06-09)

- **Added**: Section 9 — Daily AI Credits Guardrail. Specifies the normative three-level resolution order (frontmatter → `vars.GH_AW_DEFAULT_MAX_DAILY_AI_CREDITS` at runtime → built-in constant `5000`) and the required GitHub Actions expression form emitted by conforming compilers.
- **Added**: §9.7 compliance test matrix (T-AIC-DG-001 through T-AIC-DG-006).
- **Updated**: Table of contents and publication metadata to 1.1.0.

### Version 1.0.0 (Draft)

- Added initial AI Credits (AIC) normative definition and formulas.
- Added canonical `models.json` format and synchronization requirements.
- Added Copilot billing reference requirements and compliance test matrix.

---

Copyright © 2026 GitHub. All rights reserved.
