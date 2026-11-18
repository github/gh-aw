---
title: Token Broker Specification
description: Normative OIDC exchange, authorization, issuance, and conformance requirements for a deployment-owned gh-aw token broker.
sidebar:
  order: 1363
---

# Token Broker Specification

**Version**: 1.1.0<br />
**Status**: Experimental draft<br />
**Date**: 2026-11-18<br />
**Editors**: GitHub Agentic Workflows maintainers<br />
**Latest version**: [Token Broker Specification](/gh-aw/specs/token-broker-specification/)

## Abstract

This specification defines a deployment-owned HTTPS service that exchanges a verified GitHub Actions OpenID Connect (OIDC) identity token for a short-lived credential. Three independent capabilities authorize GitHub Copilot inference requests, Copilot coding-agent task operations, and GitHub API access across explicitly selected repositories. GitHub credentials are read-only in agent execution; write access is restricted to trusted safe-output execution. The specification defines the wire contract, issuer verification, tenant and workflow authorization, credential issuance, renewal, failure handling, and compliance tests. It does not define a new public GitHub credential-minting API or supply a server implementation.

## Status of This Document

Token broker authentication is experimental. The protocol, configuration, and behavior may change without deprecation. Compiling a workflow that enables any broker capability MUST emit an experimental-feature warning.

This is a project specification following W3C-style conventions, not a W3C Recommendation. Repository maintainers govern changes through the normal contribution and review process. Version numbers follow semantic versioning: incompatible contract changes increment the major version, backward-compatible features increment the minor version, and clarifications increment the patch version.

**Implementation status (informative): Partial integration.** The shared client supports the two Copilot token types. The `github` capability and its configuration, exchange fields, and context restrictions defined here are proposed and are not implemented by the current compiler or client. Coding-agent task creation and issue-assignment integrations obtain credentials at request time and renew them as necessary. Brokered Copilot inference is wired for the bounded detection and evals jobs. Main-agent inference remains blocked on a renewable AWF credential provider. The compiler rejects broker-enabled GitHub-backed inference in the main agent job, including applicable fallback models; this is not limited to the engine named `copilot`. A successful standalone `copilot-request` exchange does not establish working cloud inference. Full end-to-end acceptance has not been achieved. Server implementation, deployment, and issuer integrations remain deployment-owned.

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

This specification covers broker exchange and its compiler/client integration contract, including repository-scoped GitHub authentication. It does not cover admission decisions, billing, arbitrary credential issuance outside the declared capabilities, provider endpoint discovery, deployment provisioning, or a new secret store. The [admission service](/gh-aw/specs/admission-service-specification/) remains independent.

### 1.2. Design goals (informative)

The protocol excludes caller-selected tenant IDs, resource URLs, and provider endpoints. GitHub repository and permission selections request a bounded scope; they do not grant authority. The server derives authority from verified identity and enrolled policy, not from caller assertions. Capabilities remain independent so that a deployment can enable only the operations it can securely issue.

## 2. Conformance

### 2.1. Requirements notation

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and "OPTIONAL" in this document are to be interpreted as described in [RFC 2119](https://www.ietf.org/rfc/rfc2119.txt).

Sections 2–7 and Appendix C are normative except where explicitly marked informative. The introduction, implementation status, examples, references, deployment decisions, and change log are informative. Requirements apply to the named actor; a server-only implementer is not responsible for implementing the compiler.

### 2.2. Conformance classes

| Class | Requirements |
| --- | --- |
| Broker server | Satisfies all server requirements, including rejection and isolation requirements, for its declared capability set. |
| Compiler and runtime client | Satisfies configuration, transport, response-validation, caching, and credential-handling requirements. |
| Integrated deployment | Combines conforming components and demonstrates actual downstream use and renewal for each claimed capability. |

A broker MUST declare its supported capability set to operators. The set MAY contain any subset of the three capabilities. An empty set can conform to the exchange and rejection protocol but cannot claim usable credential issuance. Omission of an optional capability is not partial conformance. An implementation failing a mandatory requirement MUST NOT claim conformance.

These are role-based classes, not escalating Basic/Standard/Complete levels. Inference, task, and repository-access support are independent, not tiers. Conformance claims MUST identify the specification version, class, supported capabilities, supported GitHub execution contexts where applicable, and test results. Client unit tests alone MUST NOT be presented as server or integrated-deployment certification.

## 3. Runtime Configuration and Trust Boundaries

Workflow source MUST explicitly select capabilities using `token-broker.copilot-request: true`, `token-broker.copilot-task: true`, and/or the `token-broker.github` configuration below. Omission MUST preserve existing authentication. An inherited broker URL alone MUST NOT enable workflow-handler broker authentication. `copilot-request` MUST NOT be combined with `permissions.copilot-requests: write`.

For opted-in task consumers, the compiler MUST carry the reserved member `_token_broker: {"copilot-task": true}` in the existing `GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG` JSON object. Workflow handlers MUST enable broker authentication only when this member selects `copilot-task` as boolean `true`; missing, false, malformed, or string-valued markers MUST NOT enable it. This marker introduces no new environment variable. A selected capability with a missing or empty `GH_AW_TOKEN_BROKER_URL` MUST fail closed when a credential is requested, rather than disable broker authentication or select a legacy credential.

The explicit low-level `getToken(tokenType)` exchange API is marker-independent. A caller invoking it directly requests an exchange, subject to URL validation and all protocol requirements. The marker gates workflow-handler integration, not the broker server or this low-level API.

The compiler MUST emit the single endpoint environment variable `GH_AW_TOKEN_BROKER_URL`, resolved at Actions runtime using:

```yaml
GH_AW_TOKEN_BROKER_URL: ${{ vars.GH_AW_TOKEN_BROKER_URL || secrets.GH_AW_TOKEN_BROKER_URL }}
```

Organization/repository Actions variables and secrets supply this setting through existing Actions resolution rules. The compiler MUST NOT read a deployment URL from its own environment or embed a resolved deployment URL in generated YAML. There is no separate audience, capability-specific URL, or provider-endpoint setting in this protocol.

The endpoint MUST be an absolute HTTPS URL with a hostname and without user information, fragments, whitespace, or backslashes. An enabled credential request with a missing, empty, or invalid URL MUST fail closed. The configured URL string, including any path or query, is the OIDC audience; client and server MUST NOT normalize it into a different audience. Operators SHOULD avoid query parameters, aliases, and trailing-slash ambiguity.

Credential-consuming jobs MUST have `id-token: write` within the caller's permission envelope. Trusted credential-consuming runtime code MUST retain control of the OIDC bootstrap environment. Broker opt-in MUST NOT grant unrelated jobs OIDC access. The Actions bootstrap bearer token is used only with GitHub's OIDC acquisition endpoint; the client MUST NOT send it to the broker.

Admission success, admission absence, and admission's `ignore` outage policy MUST NOT authorize broker issuance or make broker failures fail open. Broker authentication MUST NOT replace credentials for unrelated GitHub operations.

### 3.1. Repository-scoped GitHub configuration

`token-broker.github` MUST contain `agent`, `safe-outputs`, or both. Each entry MUST contain exactly `repositories` and `permissions`, with the same scope syntax as Section 4.1. Omitted entries MUST NOT enable GitHub broker authentication for that context. The following proposed configuration is informative, not currently compilable:

```yaml
token-broker:
  github:
    agent:
      repositories: [octo/service, octo/shared]
      permissions:
        contents: read
        issues: read
    safe-outputs:
      repositories: [octo/service, octo/shared]
      permissions:
        contents: write
        pull-requests: write
```

The compiler MUST reject `write` permissions in `agent`, empty or malformed scopes, unknown context keys, and ambiguous authentication sources for the same consumer. Safe-output permissions MAY be `read` or `write`; write access MUST be limited to the configured safe-output operations. The compiler MUST NOT copy or merge safe-output authority into the agent configuration.

The compiler MUST bind each scope to its consuming job and trusted runtime integration. Agent and write-capable safe-output consumers MUST run in separate jobs with independently authorized OIDC identities. The agent and the safe-outputs MCP server that accepts agent-generated requests MUST NOT receive a write credential, a write-capable OIDC bootstrap environment, or access to a write exchange proxy. Only the trusted safe-output executor, after validating the requested operation and target against the configured safe-output policy, MAY request and use a write credential.

For GitHub consumers, trusted compiler-generated configuration MUST carry the selected context and scope to the corresponding runtime integration; an inherited URL, task opt-in marker, or agent-provided payload MUST NOT enable or broaden this integration. The broker MUST independently verify the context and scope as specified in Section 5.4. Workflow configuration is a request for authority, not proof of authorization.

## 4. Exchange Protocol

### 4.1. Transport and request

The configured URL is the complete exchange endpoint; no route suffix or discovery request is appended. The client MUST acquire a GitHub Actions OIDC JWT with that exact audience and send one HTTPS POST with `Content-Type: application/json` and `Accept: application/json`. Redirects MUST NOT be followed during OIDC acquisition or exchange. The broker MUST serve the configured endpoint directly without redirects.

An exchange request MUST be a UTF-8 JSON object with unique members in any order. A `copilot-task` exchange MUST contain exactly two string members; a `copilot-request` exchange MUST contain exactly three:

```json
{
  "token_type": "copilot-request",
  "provider": "github",
  "oidc_token": "<github-actions-oidc-jwt>"
}
```

For `copilot-task`, the `provider` member is omitted:

```json
{
  "token_type": "copilot-task",
  "oidc_token": "<github-actions-oidc-jwt>"
}
```

For `github`, the request MUST contain exactly four members:

```json
{
  "token_type": "github",
  "repositories": ["octo/service", "octo/shared"],
  "permissions": {
    "contents": "read",
    "issues": "read"
  },
  "oidc_token": "<github-actions-oidc-jwt>"
}
```

| Member | Contract |
| --- | --- |
| `token_type` | Exactly `copilot-request`, `copilot-task`, or `github`, case-sensitive. |
| `provider` | Required only for `copilot-request`. A nonempty inference-provider identifier matching gh-aw's provider IDs (for example, `github`). It MUST NOT be a provider endpoint or URL. |
| `repositories` | Required only for `github`. A nonempty array of at most 500 explicit `owner/repo` strings on the enrolled GitHub deployment, without URLs, wildcards, or duplicates under case-insensitive comparison. |
| `permissions` | Required only for `github`. A nonempty object mapping repository permission names supported by the approved issuer to exactly `read` or `write`. Duplicate keys, unknown names, organization/account permissions, and administrative permissions such as `administration` are forbidden. |
| `oidc_token` | Nonempty, signed compact JWT issued by the configured trusted GitHub Actions issuer. |

`provider` MUST be absent for `copilot-task` and `github`; `repositories` and `permissions` MUST be absent for both Copilot types. There is no caller-supplied execution-mode field: the broker derives agent versus safe-output authority from verified execution context. For GitHub tokens, the same permission map applies to every selected repository. Different permission maps require separate exchanges.

The broker MUST reject duplicate members, additional members, invalid UTF-8/JSON, top-level arrays, missing members, and wrong types. It MUST bound request processing and accept no more than 65,536 request-body bytes. Oversized requests MUST receive HTTP 413; unsupported media types MUST receive HTTP 415. Malformed JSON or schema violations MUST receive HTTP 400. A well-formed unknown token type MUST receive HTTP 422. The broker MUST authorize the requested provider for the verified tenant and capability; it MUST NOT derive an endpoint or backend destination from this caller-supplied identifier. No body member or HTTP authentication header can substitute another credential for the OIDC JWT.

### 4.2. Independent capabilities

| Token type | Required downstream capability |
| --- | --- |
| `copilot-request` | Authentication for the deployment's approved GitHub Copilot cloud inference consumer. |
| `copilot-task` | Authentication for the approved Copilot coding-agent task-creation and issue-assignment operations. |
| `github` | A GitHub token scoped to the complete requested repository set and permission map, read-only for agent consumers and optionally write-capable for trusted safe-output consumers. |

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

Every broker that issues credentials MUST provide an HTTPS POST endpoint for invalidating them, regardless of its supported capability set. The configured exchange endpoint MUST also serve the invalidation operations defined below; no additional URL, route discovery, or provider-specific revocation endpoint is required from the client. A broker MUST NOT declare a capability supported unless it can effectively revoke the tokens it issues for that capability.

The compiler MUST emit a final credential-invalidation step in every job that can obtain a token from the token broker, regardless of capability, provider, repository scope, or read/write permissions. This includes agent jobs and trusted safe-output jobs. The step MUST run after all credential consumers and before the job exits, using the GitHub Actions `always()` condition so it runs after success, failure, or cancellation of earlier steps. It MUST NOT depend on an exchange step succeeding or returning an output: issuance may have occurred even when the client did not receive a response.

The client MUST issue a separate invalidation POST to the configured endpoint from this step. It MUST request a fresh OIDC identity with the configured audience; it MUST NOT include an exchanged credential in the invalidation request.

The UTF-8 JSON request MUST contain exactly these two unique string members:

```json
{
  "operation": "invalidate",
  "oidc_token": "<github-actions-oidc-jwt>"
}
```

The broker MUST verify the identity and revoke every token it issued to that exact authorized execution context, including all capabilities, providers, repository sets, permission maps, and tokens superseded by renewal. It MUST NOT limit revocation to tokens retained in the client's cache or the latest exchange. Successful invalidation MUST leave no token issued to that job usable. The operation MUST be idempotent and MUST NOT revoke credentials belonging to another job, run, attempt, repository, or tenant. A successful invalidation MUST return HTTP 204 with an empty body and `Cache-Control: no-store` only after revocation is effective. The client MUST fail the invalidation step on any other status, without exposing response bodies or transport diagnostics; the compiler MUST NOT suppress this failure with `continue-on-error`. The server MUST reject malformed requests and MUST NOT disclose whether credentials exist for an unauthorized context.

The invalidation step is cleanup, not a fallback or substitute for short credential lifetimes. If the job is force-terminated before the step can run, short server-enforced lifetimes remain the bound on credential exposure.

#### 4.5.1. Individual-token invalidation

The endpoint MUST additionally accept a request to invalidate a single broker-issued token. This UTF-8 JSON request MUST contain exactly three unique string members:

```json
{
  "operation": "invalidate-token",
  "token": "<broker-issued-credential>",
  "oidc_token": "<github-actions-oidc-jwt>"
}
```

The `token` member MUST satisfy the token syntax in Section 4.3. The client MUST acquire a fresh OIDC identity with the configured audience. Possession of the token alone MUST NOT authorize revocation: the broker MUST verify the OIDC identity and bind the token to that exact authorized execution context before revoking it. It MUST NOT revoke tokens from another job, run, attempt, repository, or tenant. A valid identity submitting an unknown or foreign token MUST receive the same sanitized HTTP 403 response, without disclosing token ownership or existence.

Successful revocation MUST return HTTP 204 with an empty body and `Cache-Control: no-store` only after the token is unusable. Repeating the operation for an already revoked or expired token belonging to the same authorized context MUST also return 204. The broker MUST retain sufficient bounded, non-secret issuance evidence to support idempotency until the token's original expiration. Revocation of one token MUST NOT revoke unrelated tokens. The transport, request-size, identity-verification, timeout, failure, and credential-redaction requirements apply equally to this operation.

Individual-token invalidation MUST NOT replace compiler-generated job-wide invalidation, which also covers renewed tokens and exchanges whose responses were lost. Unlike job-wide invalidation, this operation necessarily carries the token in its request body; it MUST NOT place the token in a URL, query string, log, or diagnostic.

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

### 5.4. GitHub repository scope and execution context

For `github`, the broker MUST resolve every requested repository through trusted GitHub metadata and bind its stable repository and owner IDs to the enrolled tenant, workflow revision, execution context, and approved installation. The source repository's OIDC identity does not authorize access to any target repository by itself. Cross-repository access MUST have explicit enrolled allow rules; renames, transfers, or name reuse MUST NOT redirect existing authority to an unenrolled identity.

Authorization MUST cover the complete repository set and every requested permission for every repository. A syntactically valid but unauthorized repository or permission MUST cause HTTP 403 before issuance, with no partial result, silent scope reduction, or broader fallback. Repository names select resources, not arbitrary API hosts, issuer adapters, or installation IDs.

The broker MUST independently establish whether the consuming job is an approved agent job or trusted safe-output executor using the evidence required by Section 5.2. A request body, environment variable, token-type selection, or client cache key MUST NOT establish safe-output status. Missing or ambiguous context evidence MUST result in HTTP 403. Every `write` request from an agent context MUST receive HTTP 403, even if the workflow repository or backing installation has write access. Agent policy MUST allow only non-mutating repository permissions at `read`; a permission such as `metadata: read` that GitHub adds implicitly does not permit writes.

Safe-output write issuance MUST require an enrolled trusted executor and a scope bounded by the approved workflow's safe-output configuration and allowed operation targets. Agent-generated repository names and payloads MUST NOT expand that scope. A broker supporting only agent reads MUST deny safe-output write requests rather than downgrade them. Broader credentials minted for another capability MUST NOT be exposed to the agent as a way to bypass these restrictions.

## 6. Credential Issuance and Lifecycle

### 6.1. Deployment-owned issuer adapters

Each adapter MUST use an approved, supported issuance integration and hold any long-lived signing keys or bootstrap credentials only in protected server-side secret storage. Operators MUST verify actual downstream credential compatibility, granted permissions, repository scope, licenses/entitlements, rotation, and revocation before declaring a capability supported.

This specification does not identify a public minting API for `copilot-request`. Its issuance integration is deployment-owned. `copilot-task` likewise requires a supported GitHub credential issuer and permissions accepted by the actual task/assignment endpoints. The ability to create a GitHub App installation token does **not** prove that token works with a legacy coding-agent endpoint. An adapter MUST NOT report success based solely on a token-shaped string or an assumed token class.

For `github`, an approved GitHub App installation-token adapter SHOULD mint one installation access token using the explicitly resolved `repository_ids` and requested `permissions`. It MUST NOT omit repository or permission restrictions and thereby inherit installation-wide access. All selected repositories MUST be accessible through one approved installation on one GitHub deployment. A request spanning installations or owners that cannot share one installation MUST fail with HTTP 403 before minting; callers MAY make separately authorized exchanges for installation-compatible subsets. The broker MUST NOT aggregate tokens, return a PAT, or substitute an unrestricted installation token.

The adapter MUST verify the issued GitHub token's repository set and permissions against the complete authorized request using trusted issuance metadata. Only GitHub's unavoidable implicit `metadata: read` permission MAY be additional. A missing repository, additional repository, missing permission, or broader permission MUST prevent success; the adapter MUST revoke any nonconforming token it has already minted. GitHub App installation permissions are a ceiling, not the scope of the returned token.

Provider endpoints and backend installation/account selection MUST come from trusted tenant-bound configuration. They MUST NOT be taken from request fields, unchecked JWT URLs, repository-controlled text, or issuance-error redirects. Backend requests MUST enforce approved destinations and prevent SSRF and cross-tenant account selection.

Adapters MUST enforce a finite maximum credential lifetime and least-privilege scope. Returned credentials SHOULD be short-lived. Remaining validity at receipt MUST exceed the client's 30-second margin; servers SHOULD include additional transit/processing allowance. If the backend cannot satisfy the contract, the server MUST fail without returning a credential.

### 6.2. Replay, concurrency, and rate limits

The broker MUST implement bounded replay/abuse controls and per-tenant/execution/capability rate limits. Policy MUST prevent reuse of an identity from expanding its authorized tenant, repository, workflow, run, attempt, job, capability, or resource scope. Any replay-cache key MUST include the verified identity binding and authorized scope, with retention bounded by identity validity and configured skew.

The same still-valid JWT MAY be used for separately authorized exchanges, including concurrent requests for different capabilities or GitHub repository/permission scopes within that job's authority. A global one-use `jti` rule MUST NOT reject these otherwise authorized exchanges. Within one authorized capability/context/scope, the server MAY coalesce concurrent issuance, return a still-valid equivalent scoped credential, or apply documented rate limits. It MUST NOT require a request nonce or idempotency field absent from this wire contract.

Replay controls do not make a bearer JWT nontransferable. Theft within its validity window remains a risk; short identity age limits, job isolation, protected transport, and rate limits reduce that exposure.

### 6.3. Client caching and renewal

Credentials MUST remain in client memory. Cache and in-flight-exchange keys MUST isolate token type, repository, run ID, run attempt, job, and broker endpoint. Concurrent requests within one key MAY share an exchange. A credential MUST NOT be inherited across keys or persisted between processes/jobs.

For `github`, cache and in-flight keys MUST additionally isolate the verified execution context, complete target repository set, and complete permission map. Repository ordering and permission-key ordering MAY be canonicalized; different sets, permission levels, or agent/safe-output contexts MUST NOT share credentials. Renewal MUST reauthorize the same scope under current policy. Job-scoped invalidation in Section 4.5 MUST revoke every GitHub token issued to that job, including tokens for different repository sets, without revoking another job's tokens.

The consumer MUST check expiry at request time and renew when remaining validity is 30 seconds or less. Renewal uses the same exchange with a newly acquired OIDC token; no refresh-token endpoint exists. Renewal failure MUST NOT reuse an expired/near-expiry credential, switch capability, or fall back to legacy authentication.

Long-running inference requires a renewable provider in the running consumer, not a one-time token passed to a sidecar. A deployment MUST NOT claim integrated inference conformance until that provider and downstream renewal are demonstrated.

## 7. Compliance Testing

The server author MUST run the server scenarios below with a controlled issuer/JWKS fixture, deterministic clock, isolated tenants, and an instrumented issuer adapter. Pre-issuance rejection tests MUST assert zero backend issuance calls; backend-result rejection tests MUST assert that no credential reaches the caller. Real GitHub-signed identities and approved backend credentials are additionally REQUIRED for integrated-deployment claims; synthetic JWTs cannot establish GitHub interoperability.

| Test ID | Class | Scenario and expected outcome |
| --- | --- | --- |
| T-TBR-001 | Client | Omitted capability selection preserves authentication; inherited URL alone does not enable workflow-handler exchange. Only `_token_broker.copilot-task: true` in existing handler-config JSON enables task integration; a selected capability with missing/empty URL fails closed. Explicit low-level exchanges remain marker-independent. Runtime variable/secret resolution works without a compile-time URL. |
| T-TBR-002 | Server | Exercise all subsets of the three capabilities, including the empty set and GitHub-only. Authorized supported types succeed; unsupported known types return 501 (or documented 404/422); no alternate capability is minted. |
| T-TBR-003 | Server | Wrong issuer/audience/signature, multi-audience JWT, unsigned token, disallowed algorithm, and attacker-controlled key URLs return 401 without attacker-directed discovery. |
| T-TBR-004 | Server | Missing/non-numeric/expired `exp`, future `nbf`/`iat`, invalid ordering, and excessive age/lifetime return 401 under the configured time policy; test exact boundaries. |
| T-TBR-005 | Server | Wrong tenant/owner/repository IDs, transfer, workflow revision/ref, environment, fork policy, run/attempt, or job evidence returns 403. Missing required evidence also denies. |
| T-TBR-006 | Server | Valid identity denied a supported capability returns 403, not unsupported. An unsupported known type with invalid identity returns 401. Neither path invokes issuance. |
| T-TBR-007 | Server | Extra/duplicate/missing fields, malformed UTF-8/JSON, top-level arrays, wrong types, or fields inappropriate to the token type return 400; unknown string type returns 422; oversize/media-type failures return 413/415. |
| T-TBR-008 | Both | Success is 200 with exactly two fields, safe nonempty ASCII token, no-store, bounded body, and valid expiration. Client accepts lowercase `t`/`z`, fractional seconds, and numeric offsets through `23:59`; rejects extra/duplicate fields, invalid calendar dates, leap seconds, out-of-range time/offset components, and expiry at/below 30 seconds. |
| T-TBR-009 | Server | Concurrent use of one valid JWT for separately authorized capabilities and GitHub scopes succeeds. Replay across policy boundaries cannot broaden issuance. Test per-context replay rules, rate limits (429), and bounded cache retention. |
| T-TBR-010 | Client | Same-key requests coalesce; changed capability/repository/job/run/attempt/endpoint cannot inherit a token. Refresh occurs at the 30-second boundary; failure never reuses a stale token. |
| T-TBR-011 | Both | Redirects, stalls, oversized responses, upstream failure, and malformed responses fail safely. Client deadline covers OIDC plus exchange and reads; no automatic retry/fallback occurs. |
| T-TBR-012 | Both | Inspect errors, logs, traces, metrics, outputs, artifacts, and storage under success and failure. No JWT, bootstrap bearer, minted credential, raw body, or secret configuration leaks. |
| T-TBR-013 | Server | Backend returns wrong capability/scope, unsafe token, excessive/unknown expiration, or incompatible credential. No successful response is emitted. SSRF and cross-tenant backend selection are rejected. |
| T-TBR-014 | Deployment | For each claimed capability, use an actual GitHub-issued identity and demonstrate authorized downstream operations and request-time renewal. Broker exchange alone is insufficient. |
| T-TBR-015 | Server | Rotate trusted signing keys; cached valid keys remain usable within policy, unknown keys trigger bounded refresh, and untrusted/unavailable keys fail closed. |
| T-TBR-016 | Both | The compiler emits a final `always()` invalidation step in every broker-consuming job, including agent and safe-output jobs, after all consumers. Exercise success, earlier failure/cancellation, and issuance with a lost exchange response. Fresh OIDC invalidation revokes every token issued to that job across capabilities, providers, scopes, permissions, and renewals before returning 204. Repeat invalidation succeeds; other jobs remain usable; non-204 responses fail the step without `continue-on-error`. |
| T-TBR-017 | Both | GitHub configuration rejects agent writes and invalid scopes. Wire validation rejects empty/oversized repository lists, case-insensitive duplicates, URLs, wildcards, empty permission maps, duplicate/unknown/administrative permissions, invalid levels, and caller-supplied mode fields with 400. Exercise one, multiple, 500, and 501 repositories. |
| T-TBR-018 | Server | An authorized multi-repository read request succeeds with exactly the requested repository set and permissions. One unauthorized repository or permission, a transfer/name-reuse mismatch, or an incompatible installation set returns 403 with zero issuance calls and no partial token. |
| T-TBR-019 | Both | Every agent write request is rejected: compiler rejects configuration; server returns 403 even for direct exchanges using a valid agent identity. Spoofed safe-output labels, missing job evidence, inherited URLs, and task markers cannot authorize GitHub writes. |
| T-TBR-020 | Deployment | Approved agent reads work across selected private repositories; access outside the set and mutations fail. Validated safe-output operations can write only to approved targets. Agent/MCP processes cannot obtain the write token or its OIDC bootstrap authority; unapproved output targets are rejected before issuance. |
| T-TBR-021 | Both | Changing any target repository, permission level, or execution context prevents cache/in-flight reuse. Equivalent reordered scopes may coalesce. Renewal rechecks policy; invalidation revokes all scopes for only the issuing job. |
| T-TBR-022 | Server | Instrument the GitHub adapter: explicit repository IDs and permissions reach issuance. Broader, narrower, or installation-wide backend results are rejected and revoked; only implicit metadata read is accepted as additional authority. |
| T-TBR-023 | Both | The configured endpoint supports `invalidate-token` for every issued capability. A fresh authorized OIDC identity revokes only the specified token; repeat revocation and an owned expired token return 204. Missing/invalid identity returns 401; unknown and foreign tokens return indistinguishable 403 responses without revoking foreign tokens. Malformed requests fail safely, token-bearing bodies remain redacted, and 204 is returned only after revocation is effective. |

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

OIDC identities, bootstrap bearers, returned credentials, and server signing material MUST NOT enter logs, traces, metric labels, exception causes, artifacts, safe outputs, step/job outputs, or persisted client configuration. Client secret masking MUST be applied before credential use; masking is defense in depth, not permission to log secrets.

Server HTTP middleware, reverse proxies, APM agents, and issuer adapters MUST disable or redact credential-bearing request/response capture. Server audit records MAY contain access-controlled non-sensitive metadata such as verified stable repository IDs, run/attempt identifiers, capability, policy decision, timestamp, and a server-generated internal event ID. Raw JWTs, full claim dumps, credentials, and sensitive tenant configuration MUST NOT be included. Audit retention and access MUST be bounded by deployment policy.

Server-side signing/bootstrap credentials MUST remain in protected secret storage. Exchanged credentials MUST NOT be durably stored; any server credential cache MUST be memory-only, short-lived, and isolated by tenant and authorized capability/context/scope. Revocation or policy changes MUST invalidate affected cached authority before further issuance/reuse.

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
| GitHub integration | Compiler/client implementation of context-specific scopes, stable target-ID enrollment, installation compatibility, exact-scope issuance and revocation, and independent safe-output job authorization. This capability remains unimplemented. |

These choices do not permit extra wire fields, weakened identity verification, or claims that an unimplemented inference path works.

## 10. Change Log

### Version 1.1.0 (Draft)

- Added the proposed `github` capability for one token covering multiple explicitly authorized repositories with a shared repository-permission map.
- Restricted agent credentials to read access and write credentials to independently authorized trusted safe-output jobs.
- Defined GitHub App installation scoping, all-or-nothing authorization, scope-isolated caching, renewal, invalidation, and compliance scenarios.
- Required compiler-generated job-exit cleanup to invalidate every broker-issued token, including renewed tokens and exchanges whose responses were lost.
- Required the broker endpoint to support authenticated individual-token invalidation in addition to job-wide cleanup.
- Distinguished the new specification requirements from the existing two-capability compiler/client implementation.

### Version 1.0.0 (Draft)

- Defined the standalone two-capability token-broker protocol and server conformance requirements.
- Specified issuer verification, tenant/workflow/job authorization, deployment-owned issuance, lifecycle, replay, and privacy controls.
- Recorded existing client behavior, compliance scenarios, and the unresolved renewable inference integration.
- Clarified the existing handler-config opt-in marker, marker-independent low-level API, and restricted timestamp profile.
- Included the inference provider ID in `copilot-request` exchanges and specified unconditional job-scoped credential invalidation.
