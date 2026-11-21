---
title: Experiments Broker Specification
description: Normative configuration, OIDC authorization, atomic assignment, and result-reporting requirements for a deployment-owned experiments broker.
sidebar:
  order: 1364
---

# Experiments Broker Specification

**Version**: 1.0.0<br />
**Status**: Experimental draft<br />
**Date**: 2026-11-20<br />
**Editors**: GitHub Agentic Workflows maintainers<br />
**Latest version**: [Experiments Broker Specification](/gh-aw/specs/experiments-broker-specification/)

## Abstract

This specification defines a deployment-owned HTTPS service for experiment assignment and run-result collection in GitHub Agentic Workflows. A compiler-generated activation client authenticates with a GitHub Actions OpenID Connect (OIDC) identity, submits experiment configuration, and receives assignments and cumulative counts. An isolated reporting job authenticates independently and submits bounded run-summary observations. The specification defines runtime configuration, transport, identity verification, atomic idempotent storage, trust boundaries, failure behavior, and compliance tests. It does not provide a broker server, an analysis client, or a mechanism for authorizing workflow changes from reported results.

## Status of This Document

Experiments broker storage is experimental. This draft may change without deprecation. It follows W3C-style conventions but is not a W3C Recommendation. Repository maintainers govern revisions through the normal contribution and review process. Versions follow semantic versioning: incompatible protocol changes increment the major version, backward-compatible additions increment the minor version, and clarifications increment the patch version.

**Implementation boundary (informative).** gh-aw supplies the compiler/runtime client integration. Server implementation, deployment, durable storage, issuer enrollment, and authorization policy are deployment-owned. Existing `gh aw experiments` commands, including `gh aw experiments analyze`, currently use the git backend; no broker dataset analysis client is provided. A broker dataset is not automatically visible to those commands.

## Table of Contents

1. [Introduction](#1-introduction)
2. [Conformance](#2-conformance)
3. [Runtime configuration and integration](#3-runtime-configuration-and-integration)
4. [Assignment and reporting protocol](#4-assignment-and-reporting-protocol)
5. [Identity verification and authorization](#5-identity-verification-and-authorization)
6. [Assignment state and observations](#6-assignment-state-and-observations)
7. [Compliance testing](#7-compliance-testing)
8. [References](#8-references)
9. [Appendices](#9-appendices)
10. [Change log](#10-change-log)

## 1. Introduction

### 1.1. Purpose and scope

A **broker** is an operator-deployed service that owns experiment assignment state and accepts result observations. A **tenant** is an enrolled security boundary bound to stable repository-owner and repository identities. An **execution key** identifies an authorized tenant, repository, workflow, run, run attempt, and workflow invocation. An **assignment** maps an experiment name to a configured variant. A **count snapshot** maps experiment names and variants to cumulative assignment counts.

The protocol separates assignment from observation: an assignment is committed before the agent runs; a later report records observations without changing that assignment. The broker replaces experiment git/cache persistence only when explicitly selected. It does not replace admission, token brokerage, safe-output authorization, or analysis policy.

### 1.2. Design goals (informative)

The broker supports concurrent executions without competing git writes, while keeping state authority outside agent-controlled code. Authenticated stable identities define isolation; request metadata only assists correlation. A lost response cannot cause another allocation. Reporting is best effort rather than a guarantee that every assigned execution produces a result.

## 2. Conformance

### 2.1. Requirements notation

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and "OPTIONAL" in this document are to be interpreted as described in [RFC 2119](https://www.ietf.org/rfc/rfc2119.txt).

Sections 2–7 and Appendix C are normative except where explicitly marked informative. The introduction, implementation boundary, examples, references, formal model, and change log are informative. Requirements apply to the actor named by each statement.

### 2.2. Conformance classes

| Class | Requirements |
| --- | --- |
| Compiler and runtime client | Configuration, job isolation, dry-run, transport, request construction, response validation, and secret handling requirements. |
| Broker server | Authentication, authorization, request validation, selection, durable atomic idempotency, and dataset isolation requirements. |
| Integrated deployment | Both classes, with actual GitHub-issued identities, trusted job evidence, approved runners, and durable storage. |

These are role-based classes, not escalating compliance levels. Omitting an OPTIONAL feature does not imply partial conformance. An implementation failing a mandatory requirement MUST NOT claim conformance. Claims MUST identify the specification version, class, authorization policy profile, and test results. Client tests alone MUST NOT be presented as server or integrated-deployment certification.

## 3. Runtime Configuration and Integration

### 3.1. Storage selection

Workflow authors select this backend with `experiments.storage: broker`:

```aw wrap
---
experiments:
  storage: broker
  prompt_style:
    variants: [concise, detailed]
    weight: [50, 50]
---

Summarize in a **${{ experiments.prompt_style }}** style.
```

The compiler MUST recognize `broker` as a storage selector, not an experiment name. Omitted storage MUST remain `repo`; `repo` and `cache` behavior MUST remain unchanged. Broker selection MUST NOT create, fetch, or update experiment git state, restore or save experiment caches, or add `contents: write`. An unrelated explicitly configured operation can still require repository permissions.

When `experiments.broker-url` is omitted, the compiler MUST resolve the endpoint at Actions runtime through:

```yaml
GH_AW_BROKER_EXPERIMENTS_URL: ${{ secrets.GH_AW_EXPERIMENTS_BROKER_URL }}
```

The workflow author MAY supply `experiments.broker-url` as an HTTPS URL or a secrets expression. Shared imports MAY supply `storage: broker` and `broker-url`, with the importing workflow taking precedence. The compiler MUST use the same configured endpoint for both operations and MUST NOT resolve it from its own environment or embed a resolved secret in generated YAML. Actions variables MUST NOT be a fallback. Merely defining the secret MUST NOT enable broker storage.

The endpoint MUST be an absolute HTTPS URL with a hostname and no user information, fragment, whitespace, or backslash. Its complete configured string, including path or query, is both the POST destination and the exact OIDC audience. The client MUST NOT append a route, discover another endpoint, or normalize the audience. Operators SHOULD avoid query parameters, aliases, and trailing-slash ambiguity. Missing, empty, or invalid configuration MUST fail closed.

### 3.2. Assignment and isolated reporting

The compiler MUST perform `assign` in activation, before consuming experiment expressions or starting the agent. Only a strictly validated success response can supply activation assignments and count snapshots. The compiler MUST preserve existing assignment outputs and experiment-artifact consumers.

The compiler MUST generate a separate, isolated report job after conclusion. The report job MAY also wait for the graders job when its completion is needed for the run summary. It MUST use a fresh OIDC identity and receive only the assignment metadata and summary artifacts needed for reporting; it MUST NOT execute agent output, authored steps, custom graders, or other untrusted executable content. Reporting MUST NOT be embedded as authored code in conclusion.

Authored workflow-level environment hooks MUST NOT alter reporter executable loading, interpreter startup, credential acquisition, endpoint configuration, or artifact processing. Before any report-job setup action, the compiler MUST override every authored or imported workflow-level environment key with an empty value, except authored `HOME`, `TMPDIR`, `TMP`, and `TEMP`, which MUST use a trusted literal at job scope, and authored `PATH`, which MUST use the compiler's fixed Ubuntu system search path. Compiler-controlled steps MUST override the temporary directory variables with `runner.temp` at step scope, where that context is available. Compiler-generated step environment values for the endpoint, configuration, and producer paths MUST remain controlled by the compiler.

When live broker reporting is enabled, the `report_experiments` job MUST be entirely compiler-owned. Authored customization, including its `needs`, `env`, `runs-on`, `steps`, and `permissions`, MUST be rejected. The compiler MUST select its runner using the framework runner configuration, subject to runner policies. Job isolation MUST NOT be inferred merely from a separate job name while inherited hooks or augmentation can execute authored code with that job's OIDC authority.

Broker storage MUST be rejected when compilation does not generate an agent job whose result can be reported, including status-only workflows. The diagnostic MUST explain that an agent job is required and that status-only workflows are unsupported.

Only the broker-calling activation and report jobs MUST receive broker-required `id-token: write`, within the caller's permission envelope. Broker selection MUST NOT grant OIDC permission to the agent or unrelated jobs. The Actions OIDC bootstrap bearer MUST be used only with GitHub's OIDC acquisition endpoint, never as the broker identity.

The report job MUST be eligible after failed dependencies when assignment and summary data are available. Failures, cancellation, runner loss, skipped jobs, absent artifacts, or service outages can prevent reporting. Implementations MUST NOT claim eventual delivery or assume that a missing report denotes success, failure, zero usage, or a negative observation.

### 3.3. Dry-run

For dry-run execution, the compiler/runtime MUST select variants locally using ephemeral state. It MUST clear prior local assignment/state files before selection, including on reused self-hosted runners. It MUST NOT request an OIDC identity for experiments, make broker HTTP requests, submit reports, persist broker state, or fall back to git/cache state. Dry-run assignments and counts MUST NOT be represented as broker-committed observations.

## 4. Assignment and Reporting Protocol

### 4.1. Transport and common request envelope

Both operations MUST POST UTF-8 JSON to the same full HTTPS endpoint with `Content-Type: application/json` and `Accept: application/json`. The client MUST acquire a GitHub Actions OIDC JWT with the exact endpoint audience. Neither OIDC acquisition nor broker requests may follow redirects; the server MUST serve the endpoint directly.

Every request MUST contain the following unique members:

| Member | Contract |
| --- | --- |
| `operation` | Exactly `assign` or `report`, case-sensitive. |
| `oidc_token` | Nonempty GitHub Actions OIDC JWT. |
| `job_id` | Nonempty logical job identifier from runner context. Correlation hint, not proof of the calling job. |
| `workflow_id` | Nonempty compiler/runtime workflow identifier. Correlation hint, not an authorization identity. |
| `workflow_ref` | Nonempty workflow reference from compiler/runtime context, including reusable-workflow context when applicable. Correlation hint, not signed proof of the executing revision. |
| `invocation_id` | Compiler-generated invocation hint from required `GH_AW_EXPERIMENT_INVOCATION_ID`, matching `^[A-Za-z0-9_-]{1,128}$`: the existing artifact prefix for reusable workflows, or `default` for non-reusable workflows. Not proof of invocation identity. |
| `repository` | Nonempty repository name from runner context. Not a stable repository identifier. |
| `server_url` | Nonempty GitHub server URL from runner context. Not an issuer/discovery URL selector. |
| `run_id` | Nonempty run identifier encoded as a string. |
| `run_attempt` | Nonempty attempt identifier encoded as a string. |

All common member values MUST be strings. Only the operation-specific members in Sections 4.2 and 4.3 may be added. Clients and servers MUST reject duplicate members, malformed UTF-8/JSON, arrays in place of objects, missing required members, wrong types, and unknown envelope members. Body hints MUST NOT replace verified claims, choose trust roots, or broaden authorization. Servers MUST reject hints inconsistent with the independently verified execution context.

The server MUST enforce finite request-processing and body-size limits appropriate to the full experiment configuration. A size rejection MUST occur without mutating assignment or report state.

### 4.2. Assignment

An `assign` request MUST additionally contain `experiments`: the complete configured experiment map with all experiment metadata, rather than only variant arrays. The reserved `storage` selector MUST NOT appear in that map. The client MUST bound the serialized experiment specification to 65,536 bytes before constructing the request. The broker MUST validate this configuration and bind it to the authorized workflow revision before allocating state. Repository-controlled configuration is input, not authorization policy.

```json
{
  "operation": "assign",
  "oidc_token": "<github-actions-oidc-jwt>",
  "job_id": "activation",
  "workflow_id": "issue-summary",
  "workflow_ref": "example/project/.github/workflows/issue-summary.lock.yml@refs/heads/main",
  "invocation_id": "default",
  "repository": "example/project",
  "server_url": "https://github.com",
  "run_id": "123456",
  "run_attempt": "1",
  "experiments": {
    "style": { "variants": ["concise", "detailed"] }
  }
}
```

Success MUST return HTTP 200, `Content-Type: application/json`, and `Cache-Control: no-store`. The body MUST contain exactly `assignments` and `counts`, both objects:

```json
{
  "assignments": { "style": "concise" },
  "counts": { "style": { "concise": 12, "detailed": 11 } }
}
```

`counts` is REQUIRED, including when counters are unchanged by an inactive experiment. The client MUST require exactly the configured experiment names in both maps, excluding the `storage` selector, and exactly the configured variant names in each count map. Each assignment MUST be a string equal to one of that experiment's variants. Each count MUST be a nonnegative safe integer in the inclusive range `0`–`9007199254740991`. Unknown experiments or variants, missing entries, nulls, strings in place of counts, fractions, and unsafe integers MUST fail validation.

The count snapshot MUST include the committed increment for the new assignment, except when existing selection semantics require no increment. Repeating an assignment returns its recorded response, not a fresh allocation or a count increment. Subsequent allocations can make an earlier snapshot stale; a snapshot is not a live aggregate or proof of completed samples.

### 4.3. Reporting

A `report` request MUST additionally contain `assignments` and `results`. The assignments MUST exactly match the record committed for the verified execution key. A report MUST NOT allocate a missing assignment or infer one from reported variants.

The compiler MUST supply the agent job's dependency result to the reporter through `GH_AW_EXPERIMENT_AGENT_RESULT`, using `needs.agent.result` rather than an artifact-provided status. `results.agent_result` is REQUIRED and MUST be exactly `success`, `failure`, `cancelled`, or `skipped`. A missing or invalid value MUST fail report construction. This is the agent job result, not a claim that the entire workflow has completed.

The reporter MUST validate producer metadata from `aw_info.json` in the same downloaded `usage` artifact. The compiler supplies its path using `GH_AW_EXPERIMENT_METADATA_FILE`. Metadata `run_id` MUST be a positive safe-integer JSON number whose string representation exactly equals `GITHUB_RUN_ID`. Metadata `run_attempt` MUST be a string exactly equal to `GITHUB_RUN_ATTEMPT`; `repository` MUST exactly equal the current repository. Metadata `server_url` MUST equal the HTTPS origin of `GITHUB_SERVER_URL`. Both metadata `workflow_id` and metadata `workflow_ref` MUST match `GH_AW_WORKFLOW_REF`: the producer records the full executing workflow reference in both fields, unlike the shorter logical `workflow_id` in the broker envelope. Missing or mismatched metadata MUST fail closed. These consistency checks detect stale or misbound artifacts; they do not prove that artifact observations are trustworthy.

The client MUST bound each local JSON artifact read to 1,048,576 bytes. Its strict JSON parser MUST reject duplicate decoded member names and invalid Unicode scalar strings, and enforce maximum nesting depth 64 and maximum array length 16,384. Numeric parsing MAY accept finite fractional values; field-specific counter and producer-identity constraints still apply.

Other `results` members MUST be projected from the compact `activity/experiment-results.json` in the `usage` artifact, whose source `schema` MUST equal `usage-activity-summary/v1`. The trusted summary producer MUST include only the allowlisted observations and assignment binding, rather than unbounded per-call lists. The source's `experiments.assignments` MUST exactly match the downloaded assignment metadata before reporting. A mismatch or missing assignment binding MUST fail report construction, not be repaired from the reported observations. Producer metadata, the schema marker, and source assignment metadata MUST NOT be forwarded inside `results`. The client MUST include only the following optional nested numeric/status observations, preserving their source member names:

| Result object | Allowlisted members | Value contract |
| --- | --- | --- |
| `firewall` | `total_requests`, `allowed_requests`, `blocked_requests` | Counters. |
| `session` | `total_events`, `session_starts`, `session_shutdowns`, `turns`, `assistant_messages`, `reasoning_events`, `tool_execution_starts`, `tool_execution_completes`, `failed_tool_executions` | Counters. |
| `gateway` | `total_calls`, `failed_calls`, `total_input_size`, `total_output_size`, `max_input_size`, `max_output_size` | Counters. |
| `gateway` | `total_duration_ms`, `max_duration_ms` | Numbers. |
| `integrity` | `total_filtered` | Counter. |
| `safe_outputs` | `total_items` | Counter. |
| `ledger` | `transactions_added` | Counter. |
| `friction` | `counted_occurrences` | Counter. |
| `friction` | `total_run_aic`, `friction_ratio` | Numbers. |
| `friction.cost` | `aic`, `turns`, `tool_calls`, `latency_ms` | Numbers. |
| `friction.cost.tokens` | `input`, `output`, `cache_read`, `cache_write`, `reasoning`, `total` | Counters attributed to friction, not total run token usage. |
| `friction` | `measurement_state` | Exactly `measured`, `causal`, `statistical`, or `unavailable`. |
| `friction.dimension_states` | `aic`, `tokens`, `turns`, `tool_calls`, `latency_ms` | Each exactly `measured`, `causal`, `statistical`, `unavailable`, or `unsupported`. |

A **number** MUST be finite, nonnegative, and at most `9007199254740991`; fractions are permitted. A **counter** additionally MUST be a safe integer. Missing optional observations and absent/null aggregate source sections MUST be omitted, not converted to zero. A malformed present known observation MUST fail report construction; unknown source members MUST be dropped. Status observations MUST match their enumeration. Servers MUST reject report-result members outside this allowlist.

For each friction cost dimension, the client MUST emit its numeric observation only when the corresponding `dimension_states` member is `measured`, `causal`, or `statistical`. A state of `unavailable`, `unsupported`, or absent MUST cause the associated numeric cost to be omitted, even when its source value is zero. The `tokens` state gates all six `friction.cost.tokens` members. Known malformed numeric or state values MUST fail validation even when a dimension would otherwise be omitted. An overall `measurement_state` MUST NOT substitute for a dimension's state.

The client MUST emit `friction_ratio` only when the `aic` dimension has one of those three available states and source `total_run_aic_partial` is not `true`. It MUST omit both `total_run_aic` and `friction_ratio` when that source flag is `true`; the flag itself is not a report-result member. These rules prevent unavailable or partial measurements from being presented as observed zero cost.

The activity summary does not currently expose eval answers, custom grader values, or adoption results through this report projection. Those observations are unavailable/unreported, not successful, negative, or zero-valued measurements.

The client MUST NOT forward the raw artifact, arbitrary nested data, free-form text, prompts, tool arguments, resource URLs, secrets, or executable content. For example, the operation-specific portion of a report can be:

```json
{
  "assignments": { "style": "concise" },
  "results": {
    "agent_result": "success",
    "firewall": { "total_requests": 8, "blocked_requests": 0 }
  }
}
```

The common envelope, including the fresh `oidc_token`, is also required.

Success MUST return HTTP 204 with an empty body and `Cache-Control: no-store`. A report success records receipt of observations, not verified execution success, adoption, or authorization. Section 6 defines idempotency and conflicts.

### 4.4. Bounded client behavior and errors

Each operation MUST have one total 10-second deadline covering OIDC acquisition, response reads, and the broker exchange. Each decoded OIDC or broker response body MUST be bounded to 65,536 bytes. The client MUST cancel timed-out work and reject oversized bodies without exposing them. It MUST strictly validate success status, encoding, JSON shape, field types, and assignments/counts before use; `report` success requires 204 with no body.

The client MUST NOT automatically retry either operation, follow redirects, or fall back to git, cache, a local non-dry-run picker, or another endpoint. If an assignment response is lost, the workflow fails without locally reallocating. A separately authenticated duplicate request is handled idempotently by the server.

| HTTP status | Meaning |
| --- | --- |
| 200 | Successful assignment only. |
| 204 | Successful report only. |
| 400 | Malformed request or invalid configuration/observations. |
| 401 | Invalid or missing authenticated identity. |
| 403 | Valid identity denied by deployment policy. |
| 409 | Conflict with an existing assignment/configuration or report; no overwrite. |
| 413, 415 | Request exceeds the deployment limit or uses an unsupported media type. |
| 429 | Rate limit exceeded. |
| 5xx | Broker or durable-store failure. |
| Other statuses, including redirects | Not a successful operation. |

Errors MUST be generic and sanitized. Servers MUST return `Cache-Control: no-store` and MUST NOT expose raw claims, storage diagnostics, or sensitive configuration. Clients MUST NOT print raw bodies, endpoint secrets, identity tokens, bootstrap bearers, or underlying transport exceptions. Assignment failure MUST prevent use of uncommitted assignments; reporting failure MUST NOT roll back an assignment or fabricate a successful observation.

## 5. Identity Verification and Authorization

### 5.1. JWT verification

The broker MUST verify every operation independently using an exact operator-enrolled GitHub Actions issuer, a trusted signing-key source, an allowed algorithm set, and a single-string `aud` exactly equal to the full public endpoint. It MUST reject unsigned tokens, disallowed algorithms, invalid signatures, issuer mismatch, alternate audiences, and multi-audience arrays. For GitHub.com the issuer is `https://token.actions.githubusercontent.com`; other issuers require explicit enrollment.

Discovery MUST start from trusted configuration and verify the discovery document's issuer exactly. JWT-supplied key material, `jku`, `x5u`, or unchecked issuer URLs MUST NOT select trust roots or network destinations. Key rotation MUST use bounded trusted refresh and fail closed without a usable trusted key.

The broker MUST require finite numeric `exp`, `nbf`, and `iat`, finite bounded clock skew, and positive maximum identity age/lifetime limits. It MUST reject expired or not-yet-valid tokens, future-issued tokens beyond skew, invalid time ordering, and identities exceeding those limits. A PAT, installation token, `GITHUB_TOKEN`, App JWT, or Actions OIDC bootstrap bearer MUST NOT substitute for an OIDC identity.

### 5.2. Stable execution and job binding

Before reading or mutating state, the broker MUST bind verified `repository_owner_id` and `repository_id` to the enrolled tenant and verify their relationship. Repository and owner names alone MUST NOT grant access. Transfers MUST NOT silently carry authorization into an unenrolled tenant.

The broker MUST bind signed run ID and attempt claims to the execution key and authorize the workflow identity, revision, ref, event/fork context, and applicable environment. Both the caller workflow and any reusable workflow MUST be approved at their actual revisions. Approval of a reusable workflow alone MUST NOT authorize an unapproved caller, and caller approval MUST NOT authorize an unapproved reusable workflow.

The broker MUST verify the actual calling job's role: activation may assign; the isolated report job may report. It MUST use an approved signed claim profile or trusted GitHub evidence correlated to the verified repository, run, and attempt. `job_id` in the body is not job evidence. `job_workflow_ref` identifies a reusable workflow, not an arbitrary logical job identity. A matching string in either field MUST NOT establish job authorization. Missing or ambiguous trusted evidence MUST result in denial.

The broker MUST bind `invocation_id` to trusted evidence identifying the actual workflow invocation and its activation/report jobs within the verified workflow, run, and attempt. An artifact-prefix hash or other body-supplied hint alone MUST NOT create an independently authorized execution key. The same verified activation-job execution MUST NOT allocate multiple logical keys by changing `invocation_id`. Reports MUST resolve to that invocation's existing assignment, not another call to the same reusable workflow.

OIDC permission is job-wide, not step-specific. Deployment policy MUST include runner trust and approved executable workflow revisions; a valid JWT does not prove that runner code or its observations are trustworthy. Unenrolled repositories, untrusted fork execution, and unauthorized jobs MUST be denied by default. Assignment and report jobs can differ while sharing the same execution key.

## 6. Assignment State and Observations

### 6.1. Atomic idempotent assignment

The durable execution key MUST include authenticated tenant, stable repository identity, stable workflow identity, run ID, run attempt, and trusted workflow-invocation identity. Body-supplied workflow or invocation names MUST NOT create independent authority namespaces. Distinct tenants, repositories, workflows, runs, attempts, and supported invocations MUST remain isolated.

The broker MUST atomically check for an existing execution record, choose assignments, increment applicable counters, and persist the assignment response plus configuration binding before returning 200. Concurrent requests for the same key MUST commit at most one assignment and increment each applicable counter at most once. A duplicate with equivalent configuration MUST return the committed response; conflicting configuration MUST return 409 without mutation.

The broker MUST NOT reallocate after a timeout, connection loss, process restart, or lost response. Durable idempotency records MUST remain available throughout the deployment's documented replay/report acceptance window. After record expiry, the broker MUST reject an old key rather than silently treating it as a new allocation. An execution with the same run ID but a different attempt is a distinct key.

### 6.2. Selection compatibility

The broker MUST preserve the existing [experiment selection semantics](/gh-aw/reference/experiments/#statistical-balancing), including supported configuration, date windows, variant order, relative weights, and [continual ramps](/gh-aw/reference/experiments/#continual-experiment-ramps). Unweighted selection uses least-used variants with random tie-breaking; weights use weighted random selection. An experiment outside its active date range returns its first/control variant without incrementing counters.

Continual selection MUST retain the existing deterministic seed, experiment, repository, workflow, and run-ID inputs and advance stages according to candidate-assignment thresholds. Broker adoption MUST NOT introduce run-attempt entropy into that established selector merely because attempts have distinct idempotency keys. Stage changes and applicable counters MUST be committed atomically with allocation.

The broker MUST validate submitted configuration against the approved workflow context. Configuration or policy changes MUST NOT rewrite earlier assignments, count snapshots, or observations. Counts measure assignments, not successful runs, usable statistical samples, or adopted outputs.

### 6.3. Idempotent report receipt

The broker MUST authenticate and authorize reporting independently of assignment, then locate the existing execution record and compare the reported assignments. It MUST reject absent records or assignment mismatches without allocation or mutation.

Report receipt MUST be atomic for the same execution key. An equivalent duplicate report MUST return 204 without adding another observation. A conflicting report MUST return 409 and MUST NOT overwrite the original. Equivalence MUST be based on validated semantic content, not JSON member order, OIDC token bytes, or the diagnostic job name. Reporting MUST NOT increment assignment counters.

The dataset MUST treat reported summaries as untrusted observations. It MUST NOT treat them as authority to change policy, authorize a workflow, issue credentials, promote a variant, or assert verified human adoption. Analysis SHOULD distinguish missing, invalid, conflicting, and usable observations. Runner or artifact compromise can corrupt measurements despite valid OIDC authentication.

## 7. Compliance Testing

Tests MUST use deterministic clocks, controlled OIDC/JWKS fixtures, isolated tenant/workflow datasets, instrumented HTTP clients, and durable-store concurrency/fault injection where applicable. Denial tests MUST assert no state mutation. Integrated-deployment claims additionally require actual GitHub-issued identities and trusted evidence for both job roles.

| Test ID | Class | Scenario and expected outcome |
| --- | --- | --- |
| T-EXB-001 | Client | Omitted storage stays `repo`; `cache` is unchanged. `broker` selects the configured endpoint or runtime secret default, adds no experiment git/cache operations or `contents: write`, and is not parsed as an experiment. |
| T-EXB-002 | Client | Activation assigns before consuming variants. A separate report job follows conclusion and applicable graders, uses fresh OIDC, and executes no authored or artifact-supplied code. Workflow-level environment keys are neutralized before setup; authored augmentation is rejected. Status-only/no-agent workflows fail compilation with actionable guidance. Unrelated jobs gain no broker OIDC permission. |
| T-EXB-003 | Client | Dry-run clears stale local state, uses ephemeral local assignments, and makes zero experiments broker HTTP/OIDC calls, reports, git writes, or cache writes, including on reused self-hosted runners. |
| T-EXB-004 | Both | Both operations POST the exact configured HTTPS URL; audience retains path/query bytes. Reject invalid/missing URLs, alternate audiences, route discovery, and all redirects. |
| T-EXB-005 | Both | Validate common envelope, full experiment config, duplicate/unknown/missing fields, UTF-8, numeric/status observations, and media/body limits. Missing/mismatched `aw_info.json` producer context or summary assignments fail report construction. Agent status comes from the dependency result and matches its enum. Malformed requests do not mutate state. |
| T-EXB-006 | Client | Accept only a 200 assignment with complete exact assignment/count maps. Reject unknown variants, missing `counts`, negative/fractional/unsafe counts, arrays, nulls, and malformed JSON; inactive experiments still require counts. |
| T-EXB-007 | Both | Invalid signature, algorithm, issuer, audience, expiry, `nbf`, `iat`, key URL, and missing time policy deny. Rotation uses only trusted bounded refresh. No alternate bearer authenticates. |
| T-EXB-008 | Server | Deny wrong immutable repository/owner IDs, tenant, caller/reusable revision, run/attempt, invocation, ref/fork context, environment, runner policy, and job role. Spoofed `job_id`, `job_workflow_ref`, or invocation hashes cannot satisfy job/invocation evidence or let one activation allocate multiple keys. |
| T-EXB-009 | Server | Race duplicate assignments, lose responses, restart the service, and repeat with reordered equivalent configuration. One allocation/increment occurs; the recorded response is stable. Conflict returns 409. Expired old keys cannot allocate again. |
| T-EXB-010 | Server | Distinct tenants/repositories/workflows/runs/attempts/supported invocations cannot read or overwrite each other's assignments/reports. Attempts and supported invocations have independent idempotency keys. |
| T-EXB-011 | Server | Verify least-used tie-breaking, weights, inactive dates, and continual seed/ramp boundaries against existing selection fixtures. Allocation, counters, and stage commit atomically. |
| T-EXB-012 | Both | Reports require existing matching assignments; first report returns empty 204, equivalent duplicates return 204, conflicting reports return 409 without overwrite. Reports never increment counts. |
| T-EXB-013 | Client | OIDC/broker stalls, oversized responses, non-success status, and malformed responses fail within one 10-second operation deadline; bodies stay within 65,536 bytes. No retry or git/cache/local fallback occurs. |
| T-EXB-014 | Both | Inspect logs, artifacts, traces, and error paths: identities/endpoint secrets are masked and never emitted; only allowlisted summary observations are submitted. Friction costs require available per-dimension states; unavailable/unsupported/missing states omit even zero costs, partial run AIC is omitted, and malformed known values fail closed. Missing data is not zero or adoption evidence. |
| T-EXB-015 | Deployment | Failure/cancellation/missing artifacts can leave assignments unreported. Reporting never reallocates or rolls back. No eventual-delivery claim is made. |
| T-EXB-016 | Server | Check the bounded formal model in Appendix D, document bounds/fairness and results, and exercise corresponding invariants with store-level fault/concurrency tests. Model checking alone does not establish conformance. |

Implementers MUST record pass/fail/not-applicable per class with non-sensitive evidence. A mandatory requirement MUST NOT be hidden as not applicable. Tests SHOULD cover deadline, integer, date, and transaction boundaries.

## 8. References

### 8.1. Normative references

- **[RFC 2119]** [Key words for use in RFCs to Indicate Requirement Levels](https://www.ietf.org/rfc/rfc2119.txt).
- **[RFC 8259]** [The JavaScript Object Notation (JSON) Data Interchange Format](https://www.rfc-editor.org/rfc/rfc8259).
- **[RFC 7519]** [JSON Web Token (JWT)](https://www.rfc-editor.org/rfc/rfc7519).
- **[RFC 7517]** [JSON Web Key (JWK)](https://www.rfc-editor.org/rfc/rfc7517).
- **[OIDC Discovery]** [OpenID Connect Discovery 1.0](https://openid.net/specs/openid-connect-discovery-1_0.html).
- **[GitHub Actions OIDC]** [OpenID Connect reference](https://docs.github.com/en/actions/reference/security/oidc).
- **[Experiments]** [Experiment configuration and selection](/gh-aw/reference/experiments/).

### 8.2. Informative references

- [Experiments Specification](/gh-aw/specs/experiments-specification/).
- [Token Broker Specification](/gh-aw/specs/token-broker-specification/).
- [Semantic Versioning 2.0.0](https://semver.org/spec/v2.0.0.html).
- [TLA+](https://lamport.azurewebsites.net/tla/tla.html).

## 9. Appendices

### Appendix A: Invalid response examples (informative)

`{"assignments":{"style":"concise"}}` is invalid because `counts` is missing. `{"assignments":{"style":"unknown"},"counts":{"style":{"concise":1,"detailed":0}}}` is invalid because `unknown` is not configured. A count of `9007199254740992` is outside the safe-integer range even if the JSON parser accepts it.

A report that changes `style` from its committed `concise` assignment to `detailed` does not correct an allocation; it conflicts and cannot overwrite the record.

### Appendix B: Recovery and operational limits (informative)

A timeout does not establish whether the broker committed an operation. Operators diagnose state using protected server-side records; the client neither retries nor switches backends. Best-effort reporting can leave allocated executions without observations. Missing reports and unequal completion rates can bias analysis and require explicit coverage reporting.

The reporter requires `aw_info.run_attempt` to equal the current attempt. Rerunning only a failed reporting job without rerunning activation/conclusion leaves prior-attempt artifacts and fails closed. Each new attempt requires fresh assignment and summary artifacts; prior-attempt data cannot be attributed to it.

The current git-backed CLI cannot analyze broker records. Operators need a deployment-owned analysis/export facility; such a facility is outside this protocol and must preserve assignment attribution, missingness, and observation trust labels.

Reusable-workflow invocation hints reuse the existing artifact prefix, which hashes inputs and attempt. Repeated calls with identical inputs within the same attempt therefore remain unsupported: they do not have distinct artifact/invocation hints. Operators must not assume those calls have isolated assignments or reports. The hint is not an attestation; a deployment lacking trusted evidence to distinguish supported invocations must deny them rather than use an unverified hash as authority.

### Appendix C: Security and privacy considerations (normative)

Clients MUST mask acquired OIDC identities before use. Identities, bootstrap bearers, the secret endpoint, raw request/response bodies, and complete claim sets MUST NOT enter logs, traces, metric labels, artifacts, caches, or job outputs. Required assignment/count artifacts MUST contain no such secrets.

Server middleware, reverse proxies, and observability systems MUST disable or redact credential-bearing body capture. Audit records MAY retain access-controlled verified stable IDs, operation, run/attempt, policy decision, and internal correlation IDs under bounded retention. Servers MUST isolate tenant datasets and protect durable records against unauthorized access or alteration.

Operators MUST approve runner environments and all executable code in broker-calling jobs. Any code with that job's OIDC permission can request an identity; OIDC does not distinguish an approved generated step from malicious code in the same job. Untrusted artifacts MUST remain data, never sources of commands, endpoint selection, trust policy, or credentials.

The broker MUST apply bounded rate, request-size, and storage-abuse controls before allocating or reporting. Server failures MUST NOT cause cross-tenant access or fail-open authorization. Result values MUST remain observations even when submitted from an authenticated approved job.

### Appendix D: Formal model (informative)

The accompanying [ExperimentsBroker TLA+ module](https://github.com/github/gh-aw/blob/main/specs/experiments-broker/ExperimentsBroker.tla) models the assignment/report state machine. Prose remains normative if the model and this document diverge; maintainers should correct the divergence rather than infer a weaker contract.

The module defines assignment/report state, counters, immutable histories, `Init`, `Next`, and `Spec == Init /\ [][Next]_vars`. It specifies no fairness or liveness property. The normative obligations map to the following named invariants:

| Prose obligation | Model invariant |
| --- | --- |
| Sections 4.2 and 6: valid state domains | `TypeOK`. |
| Sections 5.2 and 6.1: authenticated execution scope | `IdentityBound`; identity verification itself is abstract. |
| Section 6.1: atomic assignment and replay, including response loss | `AssignmentImmutable`, `CountsExactlyOnce`; rejected and duplicate requests stutter. |
| Section 6.3: an assignment precedes a report | `ReportRequiresAssignment`; concrete reported-assignment comparison remains a server test. |
| Section 6.3: duplicate/conflicting reports never overwrite | `ReportImmutable`. |
| Section 3.3: dry-run cannot mutate broker state | `DryRunIsLocal`; zero HTTP/OIDC calls remain a client test. |

The checked `Bounded.cfg` uses two tenants × two attempts, two variants, and two observations; workflow, run, and invocation are fixed. Lost responses, identical/conflicting repeats, rejected identities, outages, and cancellation are represented by stuttering. TLC 2.19 from TLA+ Tools v1.7.4, running on Temurin Java 17.0.21, exhausted 2,401 distinct states (8,233 generated; depth 9) with all seven invariants satisfied and exit code 0. The four deliberately broken configurations exited 12 with the expected counterexamples:

| Configuration | Violated invariant |
| --- | --- |
| `BrokenAuthorization.cfg` | `IdentityBound` |
| `BrokenAssignment.cfg` | `CountsExactlyOnce` |
| `BrokenReport.cfg` | `ReportImmutable` |
| `BrokenDryRun.cfg` | `DryRunIsLocal` |

To reproduce from the repository root, provide Java and the v1.7.4 `tla2tools.jar`, whose SHA-256 is `936a262061c914694dfd669a543be24573c45d5aa0ff20a8b96b23d01e050e88`:

```bash
cd specs/experiments-broker
java -cp /path/to/tla2tools.jar tlc2.TLC -workers 2 -seed 1 -fp 0 \
  -config Bounded.cfg ExperimentsBroker.tla
```

Replace `Bounded.cfg` with each broken configuration to reproduce its counterexample. Evidence applies to module SHA-256 `dad7dff627899dc71056fb3f707e999a733bc1f34839d6e9e47f7de37bae2d8f` and bounded-configuration SHA-256 `ab0ba5a40957021dee11bd5a0320270b68ab67fe5059e81a15d67e193c272b62`.

Bounded model checking cannot prove unbounded correctness, JWT cryptography, caller/reusable revision or job/invocation evidence, runner trust, network behavior, storage durability, full configuration/selection semantics, or equivalence of deployed transactions to abstract transitions. The model's tenant-indexed counters abstract one configured experiment per tenant, with workflow, run, and invocation fixed; it does not demonstrate isolation across multiple workflows or reusable invocations. Further bounds and implementation tests are needed for those dimensions. This is a server-contract model, not proof of an implemented server. No fairness assumption can guarantee reports after cancellation or permanent runner loss.

## 10. Change Log

### Version 1.0.0 (Experimental draft)

- **Added**: Broker storage opt-in, exact-audience OIDC assignment/report protocol, isolated reporting, and dry-run isolation.
- **Added**: Stable authenticated execution keys, atomic idempotency, strict validation, observation trust boundaries, and conformance tests.
- **Added**: Deployment ownership, current CLI limitations, and bounded formal-model guidance.
