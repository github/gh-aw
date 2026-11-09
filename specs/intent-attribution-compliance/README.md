# Intent Attribution Compliance Fixtures

This directory defines the minimum compliance scenarios for the
[Intent Attribution & Agent Governance Specification](../intent-attribution-agent-governance.md).

The fixture files in this directory are the normative ground truth for the
minimum attribution-resolution and fail-closed governance behaviors. The formal
test suite in `pkg/intent/compliance_fixtures_formal_test.go` loads these YAML
artifacts directly, testing resolver-produced records against `pkg/intent.Resolver`
and `pkg/intent.PolicyCompiler`. The suggested-attribution fixture seeds a
pre-resolved candidate directly into the compiler because the current resolver
does not generate suggestions.

Implementation status and the runtime enforcement map are documented in the
[governance implementation companion](../intent-attribution-agent-governance-implementation.md).

## Required scenarios

| Scenario | Expected result | Spec references |
|---|---|---|
| Explicit intent wins over linked issues | Attribution uses explicit metadata as the sole source | §RFC 2119 Norms → Attribution-Resolution Order |
| Ambiguous root issue set | Attribution is `status: "ambiguous"` with `source: "closing_issue"` | §RFC 2119 Norms → Ambiguous-Root Handling |
| Unlinked pull request fails closed | Governance resolves to the safest policy (`propose_only`, no writes, approval required) | §RFC 2119 Norms → Fail-Closed Behavior |
| Multiple roots cannot select a relaxed policy | Three closing roots remain ambiguous and compile to the safest policy | §Multiple-root policy; §RFC 2119 Norms → Fail-Closed Behavior |
| Suggested attribution is non-authoritative | A suggestion cannot select an intent-specific policy | §Attribution states → `suggested`; §RFC 2119 Norms → Fail-Closed Behavior |
| Unmapped labels fail closed | A linked root with no configured label match compiles to the safest policy | §Attribution states → `unmapped`; §RFC 2119 Norms → Fail-Closed Behavior |

## Fixture guidance

Each future fixture in this directory SHOULD record:

- the input artifact shape (explicit metadata, linked issues, labels)
- the expected attribution source and status
- the expected compiled execution policy when attribution is missing or ambiguous

The minimum fixture set for conformance claims is:

1. `explicit-intent-wins.yaml`
2. `ambiguous-root-closing-issues.yaml`
3. `unlinked-pr-fail-closed.yaml`
4. `multi-root-policy.yaml`
5. `suggested-attribution-fail-closed.yaml`
6. `unmapped-labels-fail-closed.yaml`

## Formal Model

Let:

- `a ∈ PullRequestArtifact`
- `Explicit(a)` be the optional explicit intent metadata attached to `a`
- `Closing(a)` be the ordered list of linked closing issues for `a`
- `Labels(a)` be the pull-request labels on `a`
- `Resolve(a)` be the attribution result `(status, source, intent_key?)`
- `Compile(Resolve(a))` be the execution policy produced by `PolicyCompiler`

Attribution resolution is modeled as the following guard-ordered function:

```text
Resolve(a) ≜
  IF Explicit(a) ≠ null THEN
    (mapped, explicit_metadata, Explicit(a).key)
  ELSE IF |Closing(a)| = 1 THEN
    FromClosingIssue(Closing(a)[0])
  ELSE IF |Closing(a)| > 1 THEN
    (ambiguous, closing_issue, null)
  ELSE IF |Labels(a)| > 0 THEN
    FromArtifactLabels(Labels(a))
  ELSE
    (unlinked, none, null)
```

The fixture-level invariants are:

```text
F1_ExplicitIntentWins(a) ≜ Explicit(a) ≠ null ⇒ Resolve(a).source = explicit_metadata

F2_AmbiguousRootStatus(a) ≜ Explicit(a) = null ∧ |Closing(a)| > 1 ⇒
  Resolve(a).status = ambiguous ∧ Resolve(a).source = closing_issue

F3_UnlinkedFailsClosed(a) ≜ Explicit(a) = null ∧ |Closing(a)| = 0 ∧ |Labels(a)| = 0 ⇒
  Resolve(a).status = unlinked ∧ Resolve(a).source = none

F4_SafestPolicyOnIndeterminate(a) ≜
  Resolve(a).status ∈ {ambiguous, unlinked} ⇒
  Compile(Resolve(a)) =
    (propose_only, none, approval_required=true, auto_merge=false, max_attempts=1)

F5_MappedStatusPermitsRelaxedPolicy(a) ≜
  Resolve(a).status = mapped ⇒ Compile(Resolve(a)) ≠ safest_policy

F6_PolicyDeterminism(a) ≜
  Resolve(a) = Resolve(a) ∧ Compile(Resolve(a)) = Compile(Resolve(a))

F7_SingleSourcePerRecord(a) ≜
  Resolve(a).source ∈ {explicit_metadata, closing_issue, none, artifact_labels} ∧
  Resolve(a) is attributed to exactly one source

F8_SuggestionIsNonAuthoritative(c) ≜
  c.status = suggested ⇒ Compile(c) = safest_policy

F9_UnmappedAttributionFailsClosed(a) ≜
  Resolve(a).status = unmapped ⇒ Compile(Resolve(a)) = safest_policy
```

### Structure

`PolicyCompiler` produces an `ExecutionPolicy` that carries the governance
decision (`autonomy`, tool restrictions, `write_scope`, required checks,
approval, auto-merge, attempts, and matched rule IDs). It does not populate
CLI outcome-report fields directly. The outcome evaluator records
`objective_value` and `objective_labels` from objective mapping and
`traced_root_url` from the resolved artifact relationship; its
`attribution_status` and `attribution_source` preserve the attribution
context that was supplied to policy compilation.

## Behavioral Coverage Map

| Predicate / Invariant | Test Function | Description |
|---|---|---|
| `F1_ExplicitIntentWins` | `TestFormalFixture_ExplicitIntentWinsOverLinkedIssues` | Explicit intent metadata resolves as the sole attribution source even when conflicting closing-issue and label signals are present |
| `F2_AmbiguousRootStatus` | `TestFormalFixture_AmbiguousRootIssueSet` | Two or more distinct closing issues with no explicit intent resolve to `status=ambiguous`, `source=closing_issue` |
| `F3_UnlinkedFailsClosed` | `TestFormalFixture_UnlinkedPullRequestFailsClosed` | No explicit intent, no closing issues, and no labels resolve to `status=unlinked`, `source=none` |
| `F4_SafestPolicyOnIndeterminate` (ambiguous branch) | `TestFormalFixture_AmbiguousResolvesToSafestPolicy` | Ambiguous attribution compiles to the safest execution policy tuple |
| `F4_SafestPolicyOnIndeterminate` (unlinked branch) | `TestFormalFixture_UnlinkedResolvesToSafestPolicy` | Unlinked attribution compiles to the same safest execution policy tuple |
| `F5_MappedStatusPermitsRelaxedPolicy` | `TestFormalFixture_MappedExplicitStatusIsNotFailClosed` | Explicitly mapped status is not forced into fail-closed policy when a permissive rule applies |
| `F6_PolicyDeterminism` | `TestFormalFixture_PolicyDeterminismAcrossRepeatedResolution` | Resolving the same fixture input twice yields byte-identical attribution and policy |
| `F7_SingleSourcePerRecord` | `TestFormalFixture_SingleSourcePerRecordAcrossResolverFixtures` | Each resolver-backed fixture resolves to exactly one attribution source, never a blend |
| Edge case: order independence | `TestFormalFixture_AmbiguousOrderIndependence` | Reordering the two closing issues does not change the ambiguous outcome |
| Edge case: empty labels do not mask unlinked | `TestFormalFixture_UnlinkedWithEmptyLabelsSlice` | An explicitly empty labels slice on the unlinked fixture still resolves to unlinked |
| Edge case: explicit intent overrides even when closing issues would otherwise be unambiguous | `TestFormalFixture_ExplicitIntentOverridesSingleClosingIssue` | Explicit metadata wins even when only one closing issue is present |
| Multi-root policy | `TestFormalFixture_MultiRootPolicyFailsClosed` | Three closing roots remain ambiguous and cannot select a relaxed policy |
| `F8_SuggestionIsNonAuthoritative` | `TestFormalFixture_SuggestedAttributionDoesNotSelectPolicy` | A supplied heuristic suggestion cannot select a relaxed policy |
| `F9_UnmappedAttributionFailsClosed` | `TestFormalFixture_UnmappedLabelsFailClosed` | A linked root whose labels do not match configured intent receives the safest policy |

## Usage

1. Copy or verify the test file at `pkg/intent/compliance_fixtures_formal_test.go`.
2. No stubs are required; the suite drives the real resolver and policy compiler.
3. Run:

```bash
go test ./pkg/intent/... -run FormalFixture
```

## Generated Test Suite

The formal compliance suite is implemented in:

- `pkg/intent/compliance_fixtures_formal_test.go`

The suite:

- loads the six required YAML fixtures from this directory
- adapts artifact fixtures into real `intent.PullRequestData` inputs and seeds the suggested candidate directly into the policy compiler
- verifies attribution precedence and mapped, unmapped, ambiguous, and unlinked handling
- verifies that suggestions and other indeterminate attribution states cannot select a relaxed policy
