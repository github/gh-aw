# ADR-79029: Require External Authorization for Safe-Output Execution

**Date**: 2026-11-18
**Status**: Draft
**Deciders**: Unknown (AI-drafted from PR #79029 description and diff; human deciders [TODO: verify])

---

### Context

Safe-output jobs perform GitHub writes on behalf of an agent. Today the restrictions on those writes (for example `max` and per-output target limits) are configured inside the same workflow whose output they constrain. Execution happens on several paths: native handlers, custom scripts, actions and jobs, standalone uploads, reusable-workflow launches, and queue/Claim execution. A check added to one path does not cover the others, and a workflow has no way to require an outside party to approve a write before it runs. The change adds about 1,800 lines in business-logic directories, which exceeds the 100-line default ADR threshold.

### Decision

We will add an opt-in, fail-closed authorization boundary for safe-output execution. When `safe-outputs.authorization`, or a local `authorization` on an output, script, action, or job, is configured, each protected invocation must obtain an allow decision from a separately provisioned HTTPS authority before its executor runs; the request is authenticated with an OIDC token. Missing credentials, an unavailable authority, a denial, or failed evidence recording all block execution, and the requirement is ANDed with existing restrictions. The primary driver is security: an independent decision point that can deny an effect regardless of the workflow's own configuration.

### Alternatives Considered

#### Alternative 1: Manager-only enforcement

Enforce authorization once in the safe-output handler manager before dispatch. This is a single checkpoint that is easy to audit. It was not chosen because execution also happens outside the manager: native job paths, custom scripts, actions, jobs, standalone uploads, reusable-workflow launches, and queue/Claim execution would bypass it.

#### Alternative 2: Merge-specific authorization

Gate only `merge-pull-request` with an external check. This would be narrower and cheaper to roll out. It was not chosen because the risk of unreviewed effects applies to every output type, and a per-operation rule would have to be re-implemented for each new output.

#### Alternative 3: Rely on existing in-workflow restrictions

Keep the current per-output restrictions and add nothing. This adds no runtime cost and no service dependency. It was not chosen because those restrictions live in the workflow they constrain, so they cannot give an outside party a veto. The PR keeps them in place, so the new boundary adds to them rather than replacing them. [TODO: verify whether this option was discussed in review.]

### Consequences

#### Positive
- One external decision model covers native handlers, custom scripts, actions, jobs, standalone uploads, reusable-workflow launches, and queue/Claim execution.
- Fail-closed for configured outputs: credential, authority, binding, freshness, and evidence failures all stop execution.
- Opt-in: workflows without `authorization` keep their current behavior.
- Decision evidence is written durably before execution and kept separate from the operation outcome, which supports audit.
- Staged mode previews protected outputs without contacting the authority, minting credentials, or creating execution claims.

#### Negative
- Protected workflows depend on a separately provisioned, conforming authority. The PR does not add one, so production use is blocked until one exists.
- Each protected invocation adds an OIDC token request and a decision call, bounded at 10 seconds per HTTP request and 20 seconds in total. An authority outage blocks protected outputs.
- Bindings are invocation-level where native preparation or custom code does not resolve exact effects, so the authority approves a request shape rather than a verified final effect.
- Reusable-workflow authorization approves only the launch. Writes made by the downstream workflow need their own authorization.
- Authors must learn compile-time inheritance, conflict rules, and diagnostic exemptions.

#### Neutral
- Global inheritance exempts only the four compiler-classified diagnostic outputs (`noop`, `report-missing-tool`, `report-missing-data`, `report-incomplete`). An explicit local requirement still protects them.
- Identical global and local authorities resolve to one requirement. Conflicting authorities or disabling overrides fail compilation.
- Evidence is written to a trusted runner-temporary JSONL file. Neither workflow nor agent input selects its path.
- Schema and documentation are updated (`main_workflow_schema.json`, `safe-outputs-specification.md`, `governance.md`).
- Focused Go and runtime tests pass. The full `make agent-report-progress` gate remains blocked by pre-existing custom-linter findings, per the PR description.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
