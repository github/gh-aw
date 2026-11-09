# Intent Attribution & Agent Governance Implementation Notes

This companion records implementation status, safeguards, operational sync
guidance, and the runtime enforcement map for the
[Intent Attribution & Agent Governance Specification](intent-attribution-agent-governance.md).
Normative attribution and policy requirements remain in the main specification.

## Current implementation

The existing implementation provides the initial attribution and reporting foundation:

1. A shared GitHub utility loads `.github/objective-mapping.json`
2. Labels are mapped to numeric weights through `ObjectiveMapping`
3. CLI outcome reports include:

   * `objective_value`
   * `objective_labels`
   * `traced_root_url`
4. Pull request outcomes trace to closing issues before labels are evaluated
5. Direct issue labels are used as a fallback
6. Outcome summaries aggregate attempted and accepted weights
7. Per-label breakdowns aggregate accepted, rejected, and pending outcomes
8. `PolicyCompiler` output shape is defined by `pkg/intent/policy.go` (`ExecutionPolicy`, `PolicyRule`, `PolicyCompiler`) and runtime gate wiring by `pkg/intent/governance.go`

These capabilities remain supported.

In the main specification, they are treated as an early implementation of **intent attribution**, not as proof of business impact.

### Safeguards

The normative safeguards remain in the main specification's
[Safeguards section](intent-attribution-agent-governance.md#safeguards).

### Sync Notes

Until repositories migrate fully to `.github/intent-policy.json`, `.github/objective-mapping.json`
remains the authoritative label-to-intent source for attribution fallback and drift detection.
This specification defines the precedence and sync expectations for that migration, but does
not assign a mandatory repository-by-repository completion deadline.
For this document, "fully migrate" means all active intent keys and governance rules are
defined in `.github/intent-policy.json`, and runtime authorization no longer depends on
reading `.github/objective-mapping.json` except for explicit backward-compatibility paths.

Implementations SHOULD detect drift by comparing the active repository governance inputs —
the compiled rule set from `.github/intent-policy.json` when present, otherwise the effective
label-to-intent selectors derived from `.github/objective-mapping.json` — during validation
or CI. Keys present in one source but not the other SHOULD surface as a sync warning or
compliance failure so attribution and authorization stay aligned.

**Escalation norm**: A sync warning that persists across **3 or more consecutive CI runs**
without a corresponding corrective PR or explicit waiver **MUST** be escalated to a compliance
failure. When a sync warning escalates, the CI check responsible for drift detection **MUST**
fail with a non-zero exit code and open (or update) a tracking issue recording the affected
keys, the first-detected date, and the on-call maintainer as the default assignee. This norm
ensures that aspirational migration language does not mask persistent, unaddressed
configuration drift.

**Status (2026-11-09)**: The escalation norm remains in force. This pass did not verify that
a CI check currently enforces the threshold or opens/updates the required tracking issue.

## Runtime enforcement map

### `Authorizer.AuthorizeTool` Implementation Audit

`Authorizer.AuthorizeTool` and `ResolveRisk` are implemented in `pkg/intent` (see `pkg/intent/governance.go`), but neither is yet called by the Go orchestrator. The following table documents which fields of `ExecutionPolicy` are wired to runtime enforcement and which remain unused.

| `ExecutionPolicy` field | Wired to enforcement? | Notes |
|---|---|---|
| `AllowedTools` | **Implemented, not wired into orchestrator** | `pkg/intent` implements `PolicyCompiler.Compile()`, `mergePolicy()`, and `Authorizer.AuthorizeTool()` for this field, but no orchestrator calls `AuthorizeTool` at tool-call time yet. |
| `DeniedTools` | **Implemented, not wired into orchestrator** | Same as `AllowedTools` — `Authorizer.AuthorizeTool()` checks this field, but it is not yet invoked from the execution path. |
| `Autonomy` | **Not wired** | The autonomy level is compiled into the policy but not checked against actual workflow capabilities at execution time. |
| `WriteScope` | **Not wired** | Defined in the policy model; no runtime enforcement in the Go orchestrator. |
| `HumanApprovalRequired` | **Not wired** | Defined in policy model; human approval gates are not currently tied to `ExecutionPolicy`. |
| `AutoMergeAllowed` | **Not wired** | Not enforced by the orchestrator. |
| `RequiredChecks` | **Not wired** | Not checked before workflow execution. |
| `MaxAttempts` | **Not wired** | Not enforced at the orchestrator level. |
| `RuleIDs` | **Provenance only** | Recorded in the policy for auditing; not used to gate execution. |

**Risk**: Policy constraints defined in `.github/intent-policy.json` (or the equivalent `rules` array) have no runtime effect until the orchestrator calls `Authorizer.AuthorizeTool` and enforces `WriteScope`, `HumanApprovalRequired`, and `RequiredChecks`. Any policy compiled by `PolicyCompiler.Compile()` today is purely advisory.

**Required follow-up**: Wire the now-implemented `Authorizer.AuthorizeTool` (in `pkg/intent`) into the execution path. Gate enforcement behind a feature flag until the policy model is validated in production.
