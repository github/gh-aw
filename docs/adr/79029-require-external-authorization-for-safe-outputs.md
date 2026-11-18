# ADR-79029: Require External Authorization for Safe-Output Execution

**Date**: 2026-11-18
**Status**: Proposed
**Deciders**: PR requester (implementation approved); maintainer acceptance of this ADR is not recorded

---

### Context

Safe-output jobs perform GitHub writes on behalf of an agent. Existing restrictions on those writes (for example `max` and per-output target limits) are configured inside the same workflow whose output they constrain. Execution happens on several paths: native handlers, custom scripts, actions and jobs, standalone uploads, reusable-workflow launches, and queue/Claim execution. A check added to one path does not cover the others, and existing restrictions alone cannot require an outside party's permission before an invocation starts.

### Decision

Require opt-in, fail-closed authorization at each protected safe-output execution boundary. When `safe-outputs.authorization`, or a local `authorization` on an output, script, action, or job, is configured, each protected invocation must obtain an allow decision from a separately provisioned HTTPS authority before its executor runs; the request is authenticated with an OIDC token. Missing credentials, an unavailable authority, a denial, or failed pre-execution evidence recording all block execution, and the requirement is ANDed with existing restrictions. The primary driver is an independent policy decision for participating trusted workflows, not mandatory organization-wide enforcement or custody of their write credentials.

Preserve global and local requirements through direct and nested shared imports. Repository defaults in `.github/workflows/aw.json` accept a static `safe_output_authorization` object or `true`; the latter requires URL and audience Actions variables managed through `gh aw env`, with missing runtime values blocking execution rather than disabling the requirement.

### Alternatives Considered

The following are architectural comparisons against the implemented design, not a claim that each option was discussed or approved in review.

#### Alternative 1: Manager-only enforcement

Enforce authorization once in the safe-output handler manager before dispatch. This is a single checkpoint that is easy to audit. It is insufficient as the sole boundary because execution also happens outside the manager: standalone executors, custom actions and jobs, reusable-workflow launches, and queue/Claim execution also require gates before effects or author code.

#### Alternative 2: Merge-specific authorization

Gate only `merge-pull-request` with an external check. This would be narrower and cheaper to roll out. It does not meet the generic invocation-authorization requirement because other output types also produce effects, and separate per-operation protocols would duplicate the authority contract.

#### Alternative 3: Rely on existing in-workflow restrictions

Keep the current per-output restrictions and add nothing. This adds no runtime cost and no service dependency, but those restrictions alone cannot give an outside party a veto before execution. The implemented design retains these controls and adds external authorization rather than replacing them.

### Consequences

#### Positive
- One external decision model covers native handlers, custom scripts, actions, jobs, standalone uploads, reusable-workflow launches, and queue/Claim execution.
- Fail-closed for configured outputs: credential, authority, binding, freshness, and evidence failures all stop execution.
- Opt-in: workflows without `authorization` keep their current behavior.
- Minimal decision evidence is written durably before execution and kept separate from the operation outcome, which supports audit. Redacted OpenTelemetry spans provide best-effort correlation without exporting credentials, request bodies or authority URLs.
- Staged mode previews protected outputs without contacting the authority, minting credentials, or creating execution claims.

#### Negative
- Live protected invocations depend on a separately provisioned, conforming authority. The PR does not add one; the authority must implement durable claims and replay reconciliation.
- Each protected invocation adds an OIDC token request and a decision call, bounded at 10 seconds per HTTP request and 20 seconds in total. An authority outage blocks protected outputs.
- Consuming requests are not automatically retried; the bounded `withRetry` wrapper uses zero retries. A lost response blocks execution even if the authority already recorded a claim.
- Native Windows runners cannot execute protected live invocations because durable containing-directory synchronization is unavailable; they fail with `evidence_failed` before authorization requests. Staged and unprotected execution is unchanged.
- Bindings are invocation-level where native preparation or custom code does not resolve exact effects, so the authority approves a request shape rather than a verified final effect.
- Reusable-workflow authorization approves only the launch. Writes made by the downstream workflow need their own authorization.
- An author with independent broad credentials can bypass this opt-in client gate. Configuration and credential protection remain external responsibilities; target-side enforcement is necessary for stronger atomicity and revocation.
- Authors must learn compile-time inheritance, conflict rules, and diagnostic exemptions.

#### Neutral
- Global inheritance exempts only the four compiler-classified diagnostic outputs (`noop`, `report-missing-tool`, `report-missing-data`, `report-incomplete`). An explicit local requirement still protects them.
- Identical global and local authorities resolve to one requirement. Conflicting authorities or disabling overrides fail compilation.
- Evidence is written to a trusted runner-temporary JSONL file. Neither workflow nor agent input selects its path.
- The [authorization contract](../src/content/docs/specs/safe-outputs-specification.md#safe-output-invocation-authorization) specifies protocol and execution requirements; the [governance reference](../src/content/docs/reference/governance.md#safe-output-authorization) describes configuration.

---

This proposed record documents the implemented direction and the requester's implementation approval. It does not record maintainer acceptance, successful live-authority validation, or clearance of any review or threat-detection warning.
