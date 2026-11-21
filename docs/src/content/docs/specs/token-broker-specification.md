---
title: Token Broker Specification
description: Normative OIDC exchange, authorization, issuance, and conformance requirements for a deployment-owned gh-aw token broker.
sidebar:
  order: 1363
---

# Token Broker Specification

**Version**: 2.0.0<br />
**Status**: Experimental draft<br />
**Date**: 2026-11-19<br />
**Editors**: GitHub Agentic Workflows maintainers<br />
**Latest version**: [Token Broker Specification](/gh-aw/specs/token-broker-specification/)

## Abstract

This specification defines a deployment-owned HTTPS service that exchanges a verified GitHub Actions OpenID Connect (OIDC) identity token for a short-lived credential. Independent capabilities authorize GitHub Copilot inference requests, Copilot coding-agent task operations, and GitHub API operations using installation credentials. The specification defines the wire contract, issuer verification, tenant and workflow authorization, credential issuance, renewal, failure handling, and compliance tests. It does not define a public GitHub credential-minting API or supply a server implementation.

## Status of This Document

Token broker authentication is experimental. The protocol, configuration, and behavior may change without deprecation. Compiling a workflow that enables a broker capability emits an experimental-feature warning.

This is a project specification following W3C-style conventions, not a W3C Recommendation. Repository maintainers govern changes through the normal contribution and review process. Version numbers follow semantic versioning: incompatible contract changes increment the major version, backward-compatible features increment the minor version, and clarifications increment the patch version.

**Implementation status (informative): Partial integration.** The shared client supports all three token types. Coding-agent task creation and issue-assignment integrations obtain credentials at request time and renew them as necessary. GitHub tools and safe outputs can receive a job-local `github-app` credential, without automatic consumer renewal. Brokered Copilot inference is wired for the bounded detection and evals jobs. Main-agent inference remains blocked on a renewable AWF credential provider. The compiler rejects broker-enabled GitHub-backed inference in the main agent job, including applicable fallback models; this is not limited to the engine named `copilot`. A successful standalone `copilot-request` exchange does not establish working cloud inference. Full end-to-end acceptance has not been achieved. Server implementation, deployment, and issuer integrations remain deployment-owned.

## Table of Contents

1. [Introduction](#1-introduction)
2. [Conformance](#2-conformance)
3. [Runtime configuration and trust boundaries](#3-runtime-configuration-and-trust-boundaries)
4. [Exchange protocol](#4-exchange-protocol)
5. [Identity verification and authorization](#5-identity-verification-and-authorization)
6. [Credential issuance and lifecycle](#6-credential-issuance-and-lifecycle)
7. [Compliance testing](#7-compliance-testing)
8. [References](#8-references)
9. [Appendices](#9-appendices)
10. [Change log](#10-change-log)

## 1. Introduction

### 1.1. Purpose and scope

The broker removes the need to distribute long-lived capability credentials to participating workflows. It accepts a GitHub-signed job identity, applies deployment policy, and returns only a credential and its expiration.

A **capability** is an operation class identified by `token_type`. A **tenant** is a deployment-enrolled security boundary bound to stable GitHub owner and repository identities. An **execution context** identifies a repository, workflow, run, attempt, and credential-consuming job. An **issuer adapter** is a trusted server component that obtains a credential from an approved backend.

This specification covers broker exchange and its compiler/client integration contract, including explicitly selected GitHub tools and safe-output authentication. It does not cover admission decisions, billing, provider endpoint discovery, deployment provisioning, or a new secret store. The [admission service](/gh-aw/specs/admission-service-specification/) remains independent.

### 1.2. Design goals (informative)

The protocol deliberately excludes caller-selected scopes, tenant IDs, resource URLs, and provider endpoints. The server derives authority from verified identity and enrolled policy, not from caller assertions. Capabilities remain independent so that a deployment can enable only the operations it can securely issue.

## 2. Conformance

### 2.1. Requirements notation

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and "OPTIONAL" in this document are to be interpreted as described in [RFC 2119](https://www.ietf.org/rfc/rfc2119.txt).

Sections 2–7 and Appendix C are normative except where explicitly marked informative. The introduction, implementation status, examples, references, deployment decisions, and change log are informative. Requirements apply to the named actor; a server-only implementer is not responsible for implementing the compiler.

### 2.2. Conformance classes

| Class | Requirements |
| --- | --- |
| Broker server | Satisfies all server requirements, including rejection and isolation requirements, for its declared capability set. |
| Compiler and runtime client | Satisfies configuration, transport, response-validation, caching, and credential-handling requirements. |
| Integrated deployment | Combines conforming components and demonstrates actual downstream use and the capability's lifecycle profile: request-time renewal, or bounded static consumption for `github-app` as defined in Section 6.3. |

A broker MUST declare its supported capability set to operators. The set MAY contain any subset of the defined capabilities. An empty set can conform to the exchange and rejection protocol but cannot claim usable credential issuance. Omission of an optional capability is not partial conformance. An implementation failing a mandatory requirement MUST NOT claim conformance.

These are role-based classes, not escalating Basic/Standard/Complete levels. Capability selections are independent, not tiers. Conformance claims MUST identify the specification version, class, supported capabilities, and test results. Client unit tests alone MUST NOT be presented as server or integrated-deployment certification.

## 3. Runtime Configuration and Trust Boundaries

Workflow source MUST explicitly select Copilot capabilities using `token-broker.copilot-request: true` and/or `token-broker.copilot-task: true`. Omission MUST preserve existing authentication. An inherited broker URL alone MUST NOT enable workflow-handler broker authentication. `copilot-request` MUST NOT be combined with `permissions.copilot-requests: write`.

GitHub API authentication MUST be selected independently using `tools.github.token-broker: true` and/or `safe-outputs.token-broker: true`. Each selects the `github-app` capability for that section; neither requires a top-level Copilot capability. An enabled section-level broker selection MUST NOT be combined with that section's `github-app` or `github-token`, and MUST suppress top-level GitHub App fallback for that section. Omission or `false` MUST preserve existing authentication. Installation selection, repository scope, permissions, and lifetime MUST come from enrolled server policy bound to verified identity, never from caller-selected scope fields.

In these two sections, a present `token-broker:` key with an empty value MUST be equivalent to `token-broker: true`. This shorthand does not select top-level Copilot capabilities.

For GitHub tool consumers, broker policy MUST restrict installation token permissions to read-only. Safe-output consumers MUST receive only the permissions required by their approved effects. Sharing the `github-app` capability name MUST NOT authorize credential reuse between these job contexts.

For opted-in task consumers, the compiler MUST carry the reserved member `_token_broker: {"copilot-task": true}` in the existing `GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG` JSON object. Workflow handlers MUST enable broker authentication only when this member selects `copilot-task` as boolean `true`; missing, false, malformed, or string-valued markers MUST NOT enable it. This marker introduces no new environment variable. A selected capability with a missing or empty `GH_AW_TOKEN_BROKER_URL` MUST fail closed when a credential is requested, rather than disable broker authentication or select a legacy credential.

The explicit low-level `getToken(tokenType)` exchange API is marker-independent. A caller invoking it directly requests an exchange, subject to URL validation and all protocol requirements. The marker gates workflow-handler integration, not the broker server or this low-level API.

The compiler MUST emit the single endpoint environment variable `GH_AW_TOKEN_BROKER_URL`, resolved at Actions runtime using:

```yaml
GH_AW_TOKEN_BROKER_URL: ${{ secrets.GH_AW_TOKEN_BROKER_URL }}
```

An Actions secret supplies this setting through existing Actions secret resolution rules. Actions variables MUST NOT be used as a fallback. The compiler MUST NOT read a deployment URL from its own environment or embed a resolved deployment URL in generated YAML. There is no separate audience, capability-specific URL, or provider-endpoint setting in this protocol.

The endpoint MUST be an absolute HTTPS URL with a hostname and without user information, fragments, whitespace, or backslashes. An enabled credential request with a missing, empty, or invalid URL MUST fail closed. The configured URL string, including any path or query, is the OIDC audience; client and server MUST NOT normalize it into a different audience. Operators SHOULD avoid query parameters, aliases, and trailing-slash ambiguity.

Credential-consuming jobs MUST have `id-token: write` within the caller's permission envelope. Trusted credential-consuming runtime code MUST retain control of the OIDC bootstrap environment. Broker opt-in MUST NOT grant unrelated jobs OIDC access. The Actions bootstrap bearer token is used only with GitHub's OIDC acquisition endpoint; the client MUST NOT send it to the broker.

Admission success, admission absence, and admission's `ignore` outage policy MUST NOT authorize broker issuance or make broker failures fail open. Broker authentication MUST NOT replace credentials for unrelated GitHub operations.

## 4. Exchange Protocol

### 4.1. Transport and request

The configured URL is the complete exchange endpoint; no route suffix or discovery request is appended. The client MUST acquire a GitHub Actions OIDC JWT with that exact audience and send one HTTPS POST with `Content-Type: application/json` and `Accept: application/json`. Redirects MUST NOT be followed during OIDC acquisition or exchange. The broker MUST serve the configured endpoint directly without redirects.

An exchange request MUST be a UTF-8 JSON object containing the two required unique string members for `copilot-task` or `github-app`, or three for `copilot-request`, plus `job_id` whenever the runner provides `GITHUB_JOB`:

```json
{
  "token_type": "copilot-request",
  "provider": "github",
  "oidc_token": "<github-actions-oidc-jwt>",
  "job_id": "agent"
}
```

For `copilot-task` and `github-app`, the `provider` member is omitted:

```json
{
  "token_type": "copilot-task",
  "oidc_token": "<github-actions-oidc-jwt>",
  "job_id": "agent"
}
```

| Member | Contract |
| --- | --- |
| `token_type` | Exactly `copilot-request`, `copilot-task`, or `github-app`, case-sensitive. |
| `provider` | Required only for `copilot-request`. A nonempty inference-provider identifier matching gh-aw's provider IDs (for example, `github`). It MUST NOT be a provider endpoint or URL. |
| `oidc_token` | Nonempty, signed compact JWT issued by the configured trusted GitHub Actions issuer. |
| `job_id` | The runner's logical `GITHUB_JOB` string, JSON-encoded without percent-encoding. Required when available; callers without job context omit it. Diagnostic metadata only, never proof of a job execution or a credential selector. |

For `copilot-task` and `github-app`, `provider` MUST be absent. The broker MUST reject duplicate members, additional members, invalid UTF-8/JSON, arrays, missing required members, and wrong types. It MUST bound request processing and accept no more than 65,536 request-body bytes. Oversized requests MUST receive HTTP 413; unsupported media types MUST receive HTTP 415. Malformed JSON or schema violations MUST receive HTTP 400. A well-formed unknown token type MUST receive HTTP 422. The broker MUST authorize the requested provider for the verified tenant and capability; it MUST NOT derive an endpoint or backend destination from this caller-supplied identifier. No body member or HTTP authentication header can substitute another credential for the OIDC JWT.

### 4.2. Independent capabilities

| Token type | Required downstream capability |
| --- | --- |
| `copilot-request` | Authentication for the deployment's approved GitHub Copilot cloud inference consumer. |
| `copilot-task` | Authentication for the approved Copilot coding-agent task-creation and issue-assignment operations. |
| `github-app` | A GitHub App installation access token for the selected GitHub tools or safe-output consumer, scoped by enrolled deployment policy. |

Support for one capability MUST NOT imply support or authorization for another. The broker MUST NOT return a credential for a different capability as a fallback. The client MUST NOT switch capabilities or fall back to a PAT, bootstrap token, or legacy provider credential after failure.

### 4.3. Success response

Success MUST use HTTP 200, `Content-Type: application/json`, and `Cache-Control: no-store`. The UTF-8 JSON body MUST contain exactly two unique string members, in either order:

```json
{
  "token": "<opaque-credential>",
  "expires_at": "2026-10-30T18:00:00Z"
}
```

`token` MUST be nonempty and contain only printable ASCII characters U+0021–U+007E, excluding spaces and all control characters. Consumers MUST treat its value as opaque, with no prefix or length assumptions beyond the body limit.

`expires_at` MUST follow this restricted [RFC 3339](https://www.rfc-editor.org/rfc/rfc3339) profile: a four-digit year, valid Gregorian calendar date, hour `00`–`23`, minute and second `00`–`59`, and a timezone of `Z`/`z` or a signed numeric offset with hour `00`–`23` and minute `00`–`59`. Fractional seconds and lowercase `t`/`z` are accepted. Leap seconds and hour `24` are not accepted; not every RFC 3339 form is supported. Server generation of ordinary UTC timestamps with uppercase `T` and `Z` is RECOMMENDED. The advertised expiration MUST NOT exceed the actual credential expiration or the deployment's authorized maximum lifetime.

The complete decoded response body MUST NOT exceed 65,536 bytes. Provider endpoints, scopes, tenant identifiers, token types, and other metadata MUST NOT appear as additional response members. The client MUST reject duplicate/additional/missing fields, invalid encoding or JSON, unsafe tokens, invalid dates, and expiration at or before its current time plus 30 seconds. It MUST NOT expose rejected response bodies.

### 4.4. Failure semantics

| HTTP status | Server meaning | Existing client classification |
| --- | --- | --- |
| 400 | Malformed request | Unsupported capability |
| 404, 422, 501 | Unsupported capability/route; 422 for unknown type, 501 RECOMMENDED for a known but unsupported capability | Unsupported capability |
| 401 | Missing or invalid OIDC identity | Unauthorized |
| 403 | Valid identity denied by tenant, workflow, or capability policy | Unauthorized |
| 413, 415 | Request size/media-type rejection | Unavailable |
| 429 | Rate limit exceeded | Unavailable |
| 500, 502, 503, 504 | Internal or upstream issuance failure | Unavailable |
| Any other non-200, including redirects | Not a successful exchange | Unavailable |

The broker MUST use 401 for identity-verification failure and 403 for policy denial; it MUST NOT label a denied supported capability as unsupported. An invalid identity MUST NOT trigger minting, including when a capability is unsupported. Unsupported requests MUST NOT trigger minting either. For known token types, identity verification MUST precede capability-support disclosure.

Failure responses MUST carry `Cache-Control: no-store` and MUST NOT contain credentials, raw claims, upstream responses, stack traces, or sensitive configuration. Empty bodies are RECOMMENDED; any diagnostic body MUST use fixed, sanitized messages. Clients do not rely on an error-body schema and MUST NOT surface raw bodies or underlying exceptions. The 400 classification is existing client behavior, not evidence that the broker lacks a capability.

The client exchange deadline MUST be 10 seconds total, including OIDC acquisition, both response reads, and broker exchange. Each OIDC and broker response read MUST be bounded to 65,536 bytes. The client MUST cancel timed-out work and MUST NOT automatically retry or follow redirects. Servers SHOULD complete well within that deadline; server and upstream timeouts MUST be finite.

### 4.5. Credential invalidation

The client MUST issue an invalidation request to the configured endpoint after each job that can exchange a broker credential. This MUST be a separate POST step with the GitHub Actions `always()` condition, so it runs after success, failure, or cancellation of earlier steps. It MUST request a fresh OIDC identity with the configured audience; it MUST NOT include an exchanged credential in the invalidation request.

The UTF-8 JSON request MUST contain the two required unique string members plus `job_id` whenever the runner provides `GITHUB_JOB`:

```json
{
  "operation": "invalidate",
  "oidc_token": "<github-actions-oidc-jwt>",
  "job_id": "agent"
}
```

The broker MUST verify the identity and revoke all credentials it issued to that exact authorized execution context, including credentials for any provider or capability issued in that job. The operation MUST be idempotent and MUST NOT revoke credentials belonging to another job, run, attempt, repository, or tenant. A successful invalidation MUST return HTTP 204 with an empty body and `Cache-Control: no-store`. The client MUST fail the invalidation step on any other status, without exposing response bodies or transport diagnostics. The server MUST reject malformed requests and MUST NOT disclose whether credentials exist for an unauthorized context.

Job-end invalidation is terminal for that job attempt. The broker MUST deny further exchanges for the closed context, even with a still-valid OIDC identity. It MUST NOT report success while any credential issued to that job remains usable downstream, including one minted by an exchange racing with invalidation. To achieve this, it closes the context before revoking, and atomically re-checks closure when recording each issued credential (SV-28, SV-31).

Every issued credential MUST also have a finite expiration enforced by the downstream service, independently of job cleanup. If runner loss prevents invalidation, the credential can remain usable until that expiration. An earlier client cache deadline or an `expires_at` value that the downstream service does not enforce is not a substitute.

### 4.6. Request correlation

Exchange and invalidation POSTs MUST propagate W3C `traceparent` when both current `GITHUB_AW_OTEL_TRACE_ID` and `GITHUB_AW_OTEL_PARENT_SPAN_ID` are valid; invalid or incomplete context is omitted. When nonempty `GITHUB_JOB` is available, both requests MUST include its original value as JSON `job_id` and a single W3C `baggage` member, `cicd.pipeline.task.name=<encoded-job-id>`. This key follows OpenTelemetry's CI/CD task-name convention, not a signed job-execution claim. Header sanitization uses UTF-8 percent-encoding (`encodeURIComponent`), so whitespace, controls, Unicode, baggage delimiters, and literal percent signs cannot become raw header syntax. JSON payload values are not percent-encoded.

The client snapshots job and trace context before acquiring OIDC credentials. It MUST NOT forward arbitrary incoming trace headers, baggage, or exporter credentials, send these correlation headers to the GitHub OIDC issuer, or enable telemetry export solely to propagate context. The broker MUST NOT use `job_id`, baggage, or trace identifiers to authorize issuance, select scope, or replace Section 5's verified job evidence. Backend instrumentation must explicitly record extracted baggage as a span attribute if desired; extraction alone does not do so.

**Deployment compatibility:** Version 2 extends the closed request schema with `job_id`; version-1 servers reject that member. Update backend schema validation before deploying clients that send it. Requests without job context remain valid without the member. Success response schemas, credential authorization, and invalidation scope are unchanged.

## 5. Identity Verification and Authorization

### 5.1. Trust configuration and JWT verification

The broker MUST configure an exact trusted GitHub Actions issuer, an exact audience equal to its configured public exchange URL, an allowed signing-algorithm set, and trusted key-discovery locations. For GitHub.com, the issuer is `https://token.actions.githubusercontent.com`. Other GitHub deployments require separately verified issuer enrollment, not wildcard trust.

The broker MUST verify the JWT signature using keys from the trusted issuer's JWKS. Discovery MUST start from operator-controlled trust configuration and verify the discovery document's `issuer` exactly. JWT-supplied `iss`, `jku`, `x5u`, or embedded keys MUST NOT select arbitrary network destinations or trust roots. The broker MUST reject unsigned tokens, disallowed algorithms, unknown signing keys after a bounded trusted refresh, invalid signatures, and issuer mismatch. Key caches MUST support rotation and MUST fail closed when no trusted usable key exists.

The broker MUST require `aud` to be a single string exactly equal to the configured endpoint audience, not a substring, alternate URL, or multi-audience array. It MUST require finite numeric `exp`, `nbf`, and `iat` in seconds since the Unix epoch. With server time `now`, nonnegative skew allowance `skew`, maximum identity age `max_age`, and maximum identity lifetime `max_lifetime`, it MUST require all of:

- `iat < exp` and `nbf < exp`;
- `now - skew < exp`, `nbf <= now + skew`, and `iat <= now + skew`;
- `now - iat <= max_age + skew` and `exp - iat <= max_lifetime`.

Operators MUST configure finite positive age/lifetime limits and a finite bounded skew; missing time policy MUST prevent issuance. A 30-second skew allowance is RECOMMENDED. These checks reject expired, not-yet-valid, future-issued, and overly old identities according to that policy. The client-side expiry parse is not signature verification and provides no server authentication assurance.

The broker MUST NOT accept a PAT, `GITHUB_TOKEN`, GitHub App JWT, installation token, or Actions OIDC-request bootstrap bearer in place of the GitHub Actions OIDC identity.

### 5.2. Tenant, workflow, and execution authorization

Before any minting, the broker MUST bind the verified identity to enrolled policy using stable `repository_owner_id` and `repository_id`, and verify their relationship to the tenant and selected backend account/installation. Repository or owner names alone MUST NOT grant authority. Renames and transfers MUST NOT silently preserve authority across unenrolled tenants.

Policy MUST bind the permitted workflow identity/version, ref, event/fork trust, environment where applicable, run ID, attempt, and credential-consuming job context. Signed `sub`, `workflow_ref`, `workflow_sha`, and reusable-workflow claims such as `job_workflow_ref`/`job_workflow_sha` MUST be evaluated according to the deployment's enrolled claim profile, not assumed interchangeable. Required evidence that is missing or unverifiable MUST result in denial.

GitHub's standard OIDC claims do not necessarily identify the logical job name used by the local client cache. `job_workflow_ref` identifies a reusable workflow, not an arbitrary job ID. A deployment requiring job-level authorization MUST obtain sufficient evidence through an approved signed claim profile or trusted GitHub metadata correlated to the verified run/attempt. An environment variable or an unverified caller-provided job name MUST NOT fill this gap.

The broker MUST define explicit allow rules for approved workflow revisions/reusable workflows, refs, environments, and fork/pull-request execution. Unenrolled repositories and untrusted fork execution MUST be denied by default. Merely running in an enrolled base repository, having a writer actor, or possessing a valid issuer signature MUST NOT bypass these rules.

For each supported capability the broker MUST separately authorize the identity, target repository/resource scope, tenant entitlement, and least-privilege credential permissions. Client cache context is not authorization evidence. HTTP 403 is required when the identity is valid but these rules fail.

### 5.3. Verification order

For a syntactically valid request of a known type, the server MUST perform these steps in order:

1. Verify signature, trusted issuer, audience, and time bounds.
2. Resolve and authorize the enrolled identity and execution context.
3. Determine capability support and authorize the capability and resource scope.
4. Apply replay/rate policy, then call only the selected trusted issuer adapter.
5. Validate the returned credential and expiration, then construct the strict success response.

An issuance backend failure MUST NOT cause a different tenant, issuer, capability, or broader credential to be selected.

### 5.4. Server request validation requirements (authoritative)

This section is the authoritative list of checks a conforming broker MUST perform on every exchange and invalidation request. Sections 4–6 give the rationale and detail. Where any text appears to permit a weaker check, the stricter requirement here applies. A failed check MUST end processing without minting, revoking, or disclosing whether credentials exist, and MUST return the status shown.

**Transport and envelope**

| ID | Requirement | On failure |
| --- | --- | --- |
| SV-01 | Accept only HTTPS POST to the exact configured endpoint path. Do not redirect. | 404 or 405 |
| SV-02 | Require `Content-Type: application/json`. | 415 |
| SV-03 | Read at most 65,536 body bytes, with a finite read deadline. | 413 |
| SV-04 | Require one UTF-8 JSON object with unique members, no arrays, and only string values. | 400 |
| SV-05 | Ignore all HTTP authentication headers and cookies. Only the body's `oidc_token` authenticates the caller. | — |

**Request shape**

| ID | Requirement | On failure |
| --- | --- | --- |
| SV-06 | An exchange contains `token_type` and `oidc_token`, plus `provider` only when `token_type` is `copilot-request`. An invalidation contains `operation: "invalidate"` and `oidc_token`. Both allow the diagnostic string `job_id` when job context is available. Any other member set is malformed. | 400 |
| SV-07 | `token_type` is case-sensitive. An unknown well-formed value is rejected before identity verification. | 422 |
| SV-08 | `provider` matches `^[a-z][a-z0-9_-]{0,63}$` and is only used to look up an allow-list entry. It never selects an endpoint, URL, or backend. | 400 or 403 |

**Identity (Section 5.1)**

| ID | Requirement | On failure |
| --- | --- | --- |
| SV-09 | `oidc_token` is a compact JWS. Reject `alg: none`, algorithms outside the configured allow-list, and `jku`, `x5u`, `jwk`, or `x5c` header parameters. | 401 |
| SV-10 | Verify the signature with a key from the operator-configured issuer's JWKS. Refresh keys at a bounded rate and fail closed when no trusted key matches. | 401 |
| SV-11 | `iss` exactly equals the configured issuer. | 401 |
| SV-12 | `aud` is a single string exactly equal to the configured endpoint URL. | 401 |
| SV-13 | `exp`, `nbf`, and `iat` are finite numbers and satisfy every time-bound inequality in Section 5.1 under configured `skew`, `max_age`, and `max_lifetime`. Missing time policy denies issuance. | 401 |
| SV-14 | Reject PATs, `GITHUB_TOKEN`, App JWTs, installation tokens, and Actions bootstrap bearers presented as `oidc_token`. | 401 |

**Execution context and authorization (Section 5.2)**

| ID | Requirement | On failure |
| --- | --- | --- |
| SV-15 | Resolve the tenant only from signed `repository_owner_id` and `repository_id`, never from names. Deny unenrolled IDs. | 403 |
| SV-16 | Require that signed `workflow_ref` and `workflow_sha` match an approved workflow revision in the enrolled claim profile. When `job_workflow_ref` names a reusable workflow, also require that it and `job_workflow_sha` are approved. Never accept an approved reusable workflow in place of an approved caller. | 403 |
| SV-17 | Require that `ref`, `ref_type`, `event_name`, and `environment` (when policy requires one) are allowed. Deny pull-request and fork execution unless explicitly allowed. | 403 |
| SV-18 | Identify the job execution from `run_id`, `run_attempt`, and a signed job-level claim (for example `check_run_id`) or trusted GitHub metadata correlated to them. Never use caller-supplied names, environment variables, or digests. Deny when the policy requires job identity and it cannot be verified. | 403 |
| SV-19 | Deny when this job attempt has been invalidated (Section 4.5). | 403 |
| SV-36 | Require that the signed `runner_environment` claim is `github-hosted`, or that operator-attested enrolment shows the job ran on an ephemeral self-hosted runner. Deny persistent self-hosted runners for credential-consuming jobs, because an earlier job can leave code on them. | 403 |
| SV-20 | Authorize the capability for the verified tenant, workflow revision, and job role. Support for one capability never implies another. | 403, or 501 for a known capability the deployment does not support |

**`github-app` scope**

| ID | Requirement | On failure |
| --- | --- | --- |
| SV-21 | Select the App installation only from tenant-bound policy. | 403 |
| SV-22 | Mint with an explicit repository-ID list and explicit permission map taken from policy for the verified workflow revision and job role. Never mint an installation-wide or default-permission token. | 403 |
| SV-23 | GitHub tool roles receive read-only permissions only. Safe-output roles receive only the permissions their approved effects require. Reader and writer roles MUST be distinct policy entries. | 403 |
| SV-24 | Cap credential lifetime at the deployment maximum, and do not exceed the downstream expiry. | 500 (no credential) |

**Issuance and response (Sections 6.1–6.2)**

| ID | Requirement | On failure |
| --- | --- | --- |
| SV-25 | Apply rate limits keyed by tenant, job execution, and capability before calling an issuer adapter. | 429 |
| SV-26 | Call only the issuer adapter for the selected capability. Never fall back to another tenant, installation, capability, or wider scope. | 502 or 503 |
| SV-27 | Validate that the returned token is printable ASCII without spaces, and that `expires_at` is in the future with more than 30 seconds remaining, before responding. | 502 |
| SV-28 | Record the issued credential against the job execution so invalidation can revoke it, before returning it. Recording MUST atomically check that the job is not closed. If it is closed, revoke the credential and deny. | 500 or 403 (no credential) |
| SV-29 | Respond with exactly `token` and `expires_at` and `Cache-Control: no-store`. | — |

**Invalidation (Section 4.5)**

| ID | Requirement | On failure |
| --- | --- | --- |
| SV-30 | Apply SV-01 to SV-18 to the invalidation request. | As above |
| SV-31 | Close the exact job execution to further exchanges, then revoke downstream every credential recorded against it. With SV-28, this ensures an exchange racing the invalidation cannot survive it. Never affect another job, attempt, run, repository, or tenant. | 500 |
| SV-32 | Be idempotent. Return 204 with an empty body and `Cache-Control: no-store` only after revocation is confirmed. | — |
| SV-37 | SHOULD close the job execution and revoke its credentials as in SV-31 on trusted job-completion evidence, such as a `workflow_job` `completed` webhook, even when no invalidation request arrives. | — |

**Errors and logging (Appendix C)**

| ID | Requirement | On failure |
| --- | --- | --- |
| SV-33 | Verify identity (SV-09 to SV-14) before revealing whether a known capability is supported. Use 401 for identity failure and 403 for policy denial. | — |
| SV-34 | Failure responses carry `Cache-Control: no-store` and either an empty body or a fixed sanitized message. | — |
| SV-35 | Never log, trace, or audit raw JWTs, full claim sets, credentials, or upstream response bodies. | — |

Deployments MUST provide negative tests for SV-06 to SV-24, SV-28, SV-31, and SV-36 before claiming conformance (Section 7).

The [experimental, informative TLA+ model](https://github.com/github/gh-aw/tree/main/specs/token-broker) covers the server validation requirements and Appendix E. It found the gaps that SV-16, SV-28, SV-31, SV-36, and SV-37 now close.

## 6. Credential Issuance and Lifecycle

### 6.1. Deployment-owned issuer adapters

Each adapter MUST use an approved, supported issuance integration and hold any long-lived signing keys or bootstrap credentials only in protected server-side secret storage. Operators MUST verify actual downstream credential compatibility, granted permissions, repository scope, licenses/entitlements, rotation, and revocation before declaring a capability supported.

This specification does not identify a public minting API for `copilot-request`. Its issuance integration is deployment-owned. `copilot-task` likewise requires a supported GitHub credential issuer and permissions accepted by the actual task/assignment endpoints. The ability to create a GitHub App installation token does **not** prove that token works with a legacy coding-agent endpoint. An adapter MUST NOT report success based solely on a token-shaped string or an assumed token class.

Provider endpoints and backend installation/account selection MUST come from trusted tenant-bound configuration. They MUST NOT be taken from request fields, unchecked JWT URLs, repository-controlled text, or issuance-error redirects. Backend requests MUST enforce approved destinations and prevent SSRF and cross-tenant account selection.

Adapters MUST enforce a finite maximum credential lifetime and least-privilege scope. Returned credentials SHOULD be short-lived. Remaining validity at receipt MUST exceed the client's 30-second margin; servers SHOULD include additional transit/processing allowance. If the backend cannot satisfy the contract, the server MUST fail without returning a credential.

### 6.2. Replay, concurrency, and rate limits

The broker MUST implement bounded replay/abuse controls and per-tenant/execution/capability rate limits. Policy MUST prevent reuse of an identity from expanding its authorized tenant, repository, workflow, run, attempt, job, capability, or resource scope. Any replay-cache key MUST include the verified identity binding and authorized scope, with retention bounded by identity validity and configured skew.

The same still-valid JWT MAY be used for separately authorized exchanges, including concurrent requests for multiple capabilities. A global one-use `jti` rule MUST NOT reject these otherwise authorized cross-capability exchanges. Within one authorized capability/context, the server MAY coalesce concurrent issuance, return a still-valid equivalent scoped credential, or apply documented rate limits. It MUST NOT require a request nonce or idempotency field absent from this wire contract.

Replay controls do not make a bearer JWT nontransferable. Theft within its validity window remains a risk; short identity age limits, job isolation, protected transport, and rate limits reduce that exposure.

### 6.3. Client caching and renewal

Client caches MUST remain in memory. Cache and in-flight-exchange keys MUST isolate token type, repository, run ID, run attempt, job, and broker endpoint. Concurrent requests within one key MAY share an exchange. A credential MUST NOT be inherited across keys or persisted between jobs. The compiler MAY transfer a masked `copilot-request` or `github-app` credential through a step output to the intended consumer in the same job; it MUST NOT publish it as a job output or artifact.

Renewable consumers MUST check expiry at request time and renew when remaining validity is 30 seconds or less. Renewal uses the same exchange with a newly acquired OIDC token; no refresh-token endpoint exists. Renewal failure MUST NOT reuse an expired/near-expiry credential, switch capability, or fall back to legacy authentication.

The initial `github-app` integration uses a bounded static-consumer profile: it supplies a fixed credential to existing GitHub consumers rather than a request-time authentication provider. It does not implement automatic renewal and MUST NOT claim request-time renewal conformance. Deployments MUST account for operation duration when setting its bounded lifetime and MUST demonstrate that expired credentials fail without legacy authentication fallback. The unconditional job-scoped invalidation requirement still applies.

Long-running inference requires a renewable provider in the running consumer, not a one-time token passed to a sidecar. A deployment MUST NOT claim integrated inference conformance until that provider and downstream renewal are demonstrated.

## 7. Compliance Testing

The server author MUST run the server scenarios below with a controlled issuer/JWKS fixture, deterministic clock, isolated tenants, and an instrumented issuer adapter. Pre-issuance rejection tests MUST assert zero backend issuance calls; backend-result rejection tests MUST assert that no credential reaches the caller. Real GitHub-signed identities and approved backend credentials are additionally REQUIRED for integrated-deployment claims; synthetic JWTs cannot establish GitHub interoperability.

| Test ID | Class | Scenario and expected outcome |
| --- | --- | --- |
| T-TBR-001 | Client | Omitted capability selection preserves authentication; inherited URL alone does not enable workflow-handler exchange. Only `_token_broker.copilot-task: true` in existing handler-config JSON enables task integration; a selected capability with missing/empty URL fails closed. Explicit low-level exchanges remain marker-independent. Runtime variable/secret resolution works without a compile-time URL. |
| T-TBR-002 | Server | Exercise empty, single-capability, and combined support sets including `github-app`. Authorized supported types succeed; unsupported known types return 501 (or documented 404/422); no alternate capability is minted. |
| T-TBR-003 | Server | Wrong issuer/audience/signature, multi-audience JWT, unsigned token, disallowed algorithm, and attacker-controlled key URLs return 401 without attacker-directed discovery. |
| T-TBR-004 | Server | Missing/non-numeric/expired `exp`, future `nbf`/`iat`, invalid ordering, and excessive age/lifetime return 401 under the configured time policy; test exact boundaries. |
| T-TBR-005 | Server | Wrong tenant/owner/repository IDs, transfer, workflow revision/ref, environment, fork policy, run/attempt, or job evidence returns 403. Missing required evidence also denies. |
| T-TBR-006 | Server | Valid identity denied a supported capability returns 403, not unsupported. An unsupported known type with invalid identity returns 401. Neither path invokes issuance. |
| T-TBR-007 | Server | Extra/duplicate/missing required fields, malformed UTF-8/JSON, arrays, wrong types, or a missing/extra provider field return 400; unknown string type returns 422; oversize/media-type failures return 413/415. |
| T-TBR-008 | Both | Success is 200 with exactly two fields, safe nonempty ASCII token, no-store, bounded body, and valid expiration. Client accepts lowercase `t`/`z`, fractional seconds, and numeric offsets through `23:59`; rejects extra/duplicate fields, invalid calendar dates, leap seconds, out-of-range time/offset components, and expiry at/below 30 seconds. |
| T-TBR-009 | Server | Concurrent use of one valid JWT for multiple authorized capabilities succeeds. Replay across policy boundaries cannot broaden issuance. Test per-context replay rules, rate limits (429), and bounded cache retention. |
| T-TBR-010 | Client | Same-key requests coalesce; changed capability/repository/job/run/attempt/endpoint cannot inherit a token. Refresh occurs at the 30-second boundary; failure never reuses a stale token. |
| T-TBR-011 | Both | Redirects, stalls, oversized responses, upstream failure, and malformed responses fail safely. Client deadline covers OIDC plus exchange and reads; no automatic retry/fallback occurs. |
| T-TBR-012 | Both | Inspect errors, logs, traces, metrics, outputs, artifacts, and storage under success and failure. No JWT, bootstrap bearer, minted credential, raw body, or secret configuration leaks. |
| T-TBR-013 | Server | Backend returns wrong capability/scope, unsafe token, excessive/unknown expiration, or incompatible credential. No successful response is emitted. SSRF and cross-tenant backend selection are rejected. |
| T-TBR-014 | Deployment | For each claimed capability, use an actual GitHub-issued identity and demonstrate authorized downstream operations and request-time renewal. For the `github-app` static-consumer profile, instead demonstrate bounded lifetime, failure after expiration without fallback, and job-scoped invalidation. Broker exchange alone is insufficient. |
| T-TBR-015 | Server | Rotate trusted signing keys; cached valid keys remain usable within policy, unknown keys trigger bounded refresh, and untrusted/unavailable keys fail closed. |
| T-TBR-016 | Both | An unconditional job post-step obtains a fresh OIDC identity and invalidates only credentials for its verified execution context; repeat invalidation succeeds, unauthorized scopes do not revoke credentials, and non-204 responses fail safely. |
| T-TBR-017 | Client | GitHub tools and safe-output broker selections work independently and together. Conflicting section credentials fail compilation; top-level App fallback does not override broker selection. Only consuming jobs exchange credentials and receive broker-related OIDC permission. Tokens remain masked and job-local, and failures never select legacy credentials. |
| T-TBR-018 | Deployment | After job-end invalidation, the downstream service rejects the job's credentials before their original expiry, the broker denies further exchanges for that job, and a sibling job's credential still works. In a separate run without cleanup, verify that the downstream service rejects the credential at `expires_at`. |
| T-TBR-019 | Both | Exchange and invalidation carry unchanged JSON `job_id`, header-safe percent-encoded job baggage, and valid current trace context. Missing job/context is omitted, arbitrary inherited metadata is not forwarded, OIDC-issuer requests receive no correlation headers, and unsigned job/trace values never replace verified job-execution evidence. |

Implementers MUST record pass/fail/not-applicable per class and capability, with non-sensitive evidence and policy settings. Tests SHOULD include skew, calendar, concurrency, and revocation boundary conditions. Optional features marked not applicable MUST NOT hide failed mandatory requirements.

## 8. References

### 8.1. Normative references

- **[RFC 2119]** [Key words for use in RFCs to Indicate Requirement Levels](https://www.ietf.org/rfc/rfc2119.txt).
- **[RFC 8259]** [The JavaScript Object Notation (JSON) Data Interchange Format](https://www.rfc-editor.org/rfc/rfc8259).
- **[RFC 3339]** [Date and Time on the Internet: Timestamps](https://www.rfc-editor.org/rfc/rfc3339), restricted by Section 4.3.
- **[RFC 7519]** [JSON Web Token (JWT)](https://www.rfc-editor.org/rfc/rfc7519).
- **[RFC 7517]** [JSON Web Key (JWK)](https://www.rfc-editor.org/rfc/rfc7517).
- **[OIDC Discovery]** [OpenID Connect Discovery 1.0](https://openid.net/specs/openid-connect-discovery-1_0.html).
- **[GitHub Actions OIDC]** [OpenID Connect reference](https://docs.github.com/en/actions/reference/security/oidc).
- **[W3C Trace Context]** [Trace Context Level 2](https://www.w3.org/TR/trace-context-2/).
- **[W3C Baggage]** [Baggage](https://www.w3.org/TR/baggage/).
- **[OpenTelemetry CI/CD Attributes]** [CI/CD attribute registry](https://opentelemetry.io/docs/specs/semconv/registry/attributes/cicd/).

### 8.2. Informative references

- [Admission Service Specification](/gh-aw/specs/admission-service-specification/).
- [GitHub App installation access tokens](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/generating-an-installation-access-token-for-a-github-app).
- [Semantic Versioning 2.0.0](https://semver.org/spec/v2.0.0.html).
- Repository implementation: `actions/setup/js/token_broker.cjs`, `token_broker.test.cjs`, `token_broker_github.cjs`, and `pkg/workflow/token_broker.go`.

## 9. Appendices

### Appendix A: Exchange examples (informative)

A request-only deployment can return the Section 4.3 success object for an authorized `copilot-request`, while returning HTTP 501 with an empty body for an otherwise authorized `copilot-task`. A task-only deployment reverses that behavior. Neither behavior permits fallback to the other token type.

For a client clock of `2026-10-30T17:55:00Z`, expiration `2026-10-30T18:00:00Z` is acceptable. Expiration `2026-10-30T17:55:30Z` is rejected at the exact margin. `2026-02-29T18:00:00Z` is an invalid date. The following object is invalid even with a usable token because endpoint selection is not part of the response:

```json
{
  "token": "<opaque-credential>",
  "expires_at": "2026-10-30T18:00:00Z",
  "endpoint": "https://provider.example.org"
}
```

### Appendix B: Client error codes (informative)

These are sanitized local client errors, not server response fields:

| Code | Meaning |
| --- | --- |
| `ERR_TOKEN_BROKER_CONFIGURATION` | Missing/invalid broker URL. |
| `ERR_TOKEN_BROKER_OIDC` | OIDC acquisition or local identity-shape/expiry validation failed. |
| `ERR_TOKEN_BROKER_UNAUTHORIZED` | Broker returned 401/403. |
| `ERR_TOKEN_BROKER_UNSUPPORTED` | Invalid local token type or broker returned 400/404/422/501. |
| `ERR_TOKEN_BROKER_UNAVAILABLE` | Transport/deadline failure or other non-200 status. |
| `ERR_TOKEN_BROKER_MALFORMED_RESPONSE` | Invalid broker success body, token, or expiration. |

### Appendix C: Security and privacy considerations (normative)

OIDC identities, bootstrap bearers, returned credentials, and server signing material MUST NOT enter logs, traces, metric labels, exception causes, artifacts, safe outputs, job outputs, or persisted client configuration. Step outputs MUST NOT contain them except for the masked, same-job credential transfers defined in Section 6.3. Client secret masking MUST be applied before credential use; masking is defense in depth, not permission to log secrets.

Server HTTP middleware, reverse proxies, APM agents, and issuer adapters MUST disable or redact credential-bearing request/response capture. Server audit records MAY contain access-controlled non-sensitive metadata such as verified stable repository IDs, run/attempt identifiers, capability, policy decision, timestamp, and a server-generated internal event ID. Raw JWTs, full claim dumps, credentials, and sensitive tenant configuration MUST NOT be included. Audit retention and access MUST be bounded by deployment policy.

Server-side signing/bootstrap credentials MUST remain in protected secret storage. Exchanged credentials MUST NOT be durably stored; any server credential cache MUST be memory-only, short-lived, and isolated by tenant and authorized capability/context/scope. Revocation or policy changes MUST invalidate affected cached authority before further issuance/reuse.

**Operator-authored custom steps, jobs, and actions.** Operator-authored workflow code runs with the full authority of its job. This includes top-level `pre-steps`, `steps`, `pre-agent-steps`, `post-steps`, and `jobs`; `safe-outputs.steps`; and any action referenced through `uses:`, together with its transitive dependencies. In a job that consumes a brokered credential, such code can:

- read the brokered credential through its same-job step output or the environment of a step that consumes it;
- use `id-token: write`, which is job-wide, to request its own OIDC token and perform an independent exchange for every capability the broker policy grants to that job;
- use or exfiltrate a credential before cleanup runs. Cleanup limits use after the job ends, but does not restrict use during the job. An exfiltrated credential remains valid until revocation completes or it expires.

`safe-outputs.jobs` MUST NOT inherit `safe-outputs.token-broker`; they use only their own configured credentials and receive no brokered token or broker-granted `id-token: write`.

Static `github-app` and `github-token` credentials carry the same risk. The broker does not create a new trust boundary between steps in one job. Operators MUST therefore review this code before enabling broker consumption, and again whenever it changes. The review MUST confirm that:

1. The code is operator-authored. Agent output, issue or pull request content, and other untrusted input MUST NOT select executable code, action references, refs, credentials, broker URLs, or target repositories.
2. Every third-party action is pinned to a full-length commit SHA. Actions referenced by mutable tags or branches MUST be treated as unreviewed.
3. No step writes credentials, OIDC tokens, or bootstrap variables to logs, artifacts, caches, job outputs, or external services.
4. Custom jobs that do not need brokered credentials are not granted `id-token: write`. Operators SHOULD isolate custom code that does not need the job's credential in a separate job with minimal permissions.

Broker policy SHOULD bind issuance to approved workflow revisions so that an unreviewed change to custom code cannot receive credentials. GitHub OIDC tokens do not identify the requesting step. The broker therefore cannot distinguish the compiler-generated exchange step from other code in the same job.

Operators MUST protect endpoint configuration and approved egress. HTTPS alone does not establish an approved broker: a repository administrator who can replace the URL may redirect a workflow's OIDC exchange to another service. Workflow opt-in is cooperative configuration, not an organization-wide unbypassable control. Backend authorization remains the authoritative boundary.

### Appendix D: Deployment decisions still required (informative)

Before implementation can claim deployment conformance, operators must resolve:

| Decision | Required deployment evidence |
| --- | --- |
| Issuance integrations | Approved issuer/backend for each capability, actual endpoint/token compatibility, scopes, entitlements, and revocation procedure. No public minting API is assumed. |
| Identity policy | Trusted GitHub deployment/issuer, exact audience URL, allowed algorithms, maximum JWT age/lifetime/skew, enrolled stable IDs, workflow revisions, refs, environments, and fork policy. |
| Job binding | Available signed claims or trusted GitHub metadata sufficient to authorize the consuming job without treating local cache keys as proof. |
| Lifecycle and abuse policy | Credential lifetime limits, replay behavior within one capability/context, tenant rate limits, key-refresh bounds, server timeouts, audit retention, and revocation propagation. |
| Inference integration | Native renewable AWF provider and downstream renewal tests. This remains unresolved in the current repository implementation. |

These choices do not permit extra wire fields, weakened identity verification, or claims that an unimplemented inference path works.

### Appendix E: Threat model (informative)

**Assets:** brokered installation and Copilot credentials, Actions OIDC request credentials and tokens, the `GH_AW_TOKEN_BROKER_URL` setting, server-side signing material, and broker policy.

**Trusted:** GitHub's OIDC issuer, the broker deployment and its policy store, the compiler, compiler-generated steps, and operator-authored workflow code after review.

**Untrusted:** agent output, prompt-injected content, issue and pull request content, forks, and any network path outside approved egress.

| # | Threat | Impact | Mitigations | Residual risk |
| --- | --- | --- | --- | --- |
| TM-1 | A prompt-injected agent reads OIDC request credentials or a brokered token from its environment. | The agent mints or uses credentials beyond its read-only tools. | GitHub tool brokering requires the AWF sandbox. `ACTIONS_ID_TOKEN_REQUEST_*` and `GH_AW_TOKEN_BROKER_URL` are excluded from the agent container. GitHub tool tokens are read-only by policy. | A sandbox escape exposes the job's credentials. The read-only policy limits the damage. |
| TM-2 | Agent output steers a write-capable safe-output job to unintended targets. | Unauthorized writes. | Safe-output handlers validate agent proposals against their configuration. Broker policy, not the request, sets repositories and permissions. Threat detection can gate execution. | Writes allowed by the configuration remain possible. Keep safe-output configuration minimal. |
| TM-3 | Compromised or unreviewed custom steps, jobs, or actions in a brokered job. | Same-job token theft, or independent exchanges using the job-wide `id-token: write`. | Operator review, full-SHA action pinning, and isolating code that needs no credential into separate jobs (Appendix C). `safe-outputs.jobs` do not inherit `safe-outputs.token-broker`. Policy binds issuance to approved workflow revisions. | The broker cannot tell which step is asking. Every step in the job is trusted equally. |
| TM-4 | A token is exfiltrated during the job. | Use from outside the runner until revocation or expiry. | Masking, no tokens in job outputs or artifacts, approved egress, least-privilege scope, terminal invalidation (Section 4.5), and short downstream expiry. | Use before cleanup cannot be prevented. Exposure is bounded by scope and lifetime. |
| TM-5 | Cleanup does not run (runner loss, cancellation, timeout). | The credential stays valid after the job. | The credential's own downstream expiry applies regardless of cleanup. The broker SHOULD revoke on observed job completion (SV-37). | Exposure lasts until expiry, normally at most one hour for installation tokens. |
| TM-6 | A sibling job or a different workflow replays or reuses another job's credential. | Cross-job escalation, for example reader to writer. | Separate exchange per consuming job, caches keyed by verified job execution, and policy binding to workflow revision and role (Section 5.2). Sharing a capability name does not authorize reuse. | Sibling jobs share `run_id`. Binding a role needs signed job identity, which deployments must verify (Appendix D). |
| TM-7 | The broker URL is replaced with an attacker endpoint. | OIDC token disclosure to a third party, and a forged credential returned to the workflow. | The URL comes only from an Actions secret with no variable fallback. The audience equals the exact URL, so a token minted for one URL is rejected elsewhere. Approved egress. | A repository administrator can redirect the exchange. Backend authorization, not workflow opt-in, is the control. |
| TM-8 | A fork, unapproved ref, or unenrolled repository requests credentials. | Credentials issued to untrusted code. | Server-side verification of issuer, audience, repository and owner IDs, ref, environment, workflow revision, and event policy (Sections 5.1–5.3). | Misconfigured policy. Deployments must test negative cases (Section 7). |
| TM-9 | Credentials leak through logs, traces, error messages, or server telemetry. | Credential disclosure. | Masking before use, redacted errors, and no capture of credentials in server middleware or audit logs (Appendix C). | Third-party actions may log carelessly. See TM-3. |
| TM-10 | The broker is unavailable or returns a malformed response. | Workflow failure, or silent fallback to broader credentials. | Fail closed with no legacy fallback. Admission outcomes never authorize issuance (Section 3). | Availability depends on the broker. |
| TM-11 | Server signing material is compromised. | Arbitrary installation tokens across the enrolled scope. | Protected secret storage, no keys in Actions, key rotation, and access-controlled audit. | Equivalent to App key compromise, but on one hardened service instead of many repositories. |
| TM-12 | Work-queue dispatch uses a brokered token without a verified App identity. | Queue claims attributed to the wrong principal. | Brokered dispatch is rejected. An explicit `dispatch-workflow` credential is required. | None within this protocol. |
| TM-13 | The agent leaves code on a persistent self-hosted runner, and a later safe-output job runs there. | The planted code exchanges the safe-output job's OIDC token for a writer credential. | SV-36 denies persistent self-hosted runners. The gh-aw compiler warns when a credential-consuming job selects a self-hosted runner. Use GitHub-hosted or ephemeral runners. | Runner attestation is operator-maintained. |
| TM-14 | Another repository calls an approved reusable workflow. | The caller receives a credential scoped to the reusable workflow's repository. | SV-16 requires both the caller and the reusable workflow revisions to be approved. Policy scopes credentials to the verified caller repository. | Approving a caller approves every reusable workflow it is allowed to call. |
| TM-15 | An exchange that passed SV-19 records its credential after invalidation revoked the job's credentials. | A credential survives a successful invalidation. | SV-31 closes before revoking, and SV-28 atomically re-checks closure when recording. | None within this protocol. |
| TM-16 | A brokered writer credential opens an agent-authored pull request. Unlike `GITHUB_TOKEN`, App installation tokens trigger workflows. | Agent-authored code runs in the repository's CI with its secrets. | SV-16 and SV-17 deny broker exchanges from that CI. Safe-output pull requests are protected by branch rules and required review before untrusted workflows run. | Repository secrets outside the broker remain reachable from that CI. Require approval for workflows on bot-authored pull requests. |

## 10. Change Log

### Version 2.0.0 (Draft)

- Added JSON `job_id` to exchange and invalidation requests when runner job context is available. Closed version-1 server schemas must be updated before accepting these clients.
- Specified current-context `traceparent`, header-safe W3C job baggage using `cicd.pipeline.task.name`, and T-TBR-019 correlation coverage.
- Kept logical job metadata separate from signed execution identity, credential authorization, and invalidation scope.

### Version 1.1.0 (Draft)

- Added the independent `github-app` capability for explicit GitHub tools and safe-output authentication.
- Kept installation and scope selection in deployment policy, with no new caller-controlled request members.
- Documented masked same-job token transfer and the initial static consumers' renewal limitation.
- Documented the operator review required for custom steps, jobs, and actions in credential-consuming jobs.
- Added an informative threat model of potential breaches and mitigations.
- Added Section 5.4, an authoritative list of server request validation requirements.
- Added an experimental TLA+ threat model. Its findings tightened SV-16, SV-28, and SV-31, added SV-36 and SV-37, and added TM-13 to TM-16.

### Version 1.0.0 (Draft)

- Defined the standalone two-capability token-broker protocol and server conformance requirements.
- Specified issuer verification, tenant/workflow/job authorization, deployment-owned issuance, lifecycle, replay, and privacy controls.
- Recorded existing client behavior, compliance scenarios, and the unresolved renewable inference integration.
- Clarified the existing handler-config opt-in marker, marker-independent low-level API, and restricted timestamp profile.
- Included the inference provider ID in `copilot-request` exchanges and specified unconditional job-scoped credential invalidation.
