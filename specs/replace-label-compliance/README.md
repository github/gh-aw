# Replace-Label Compliance Fixtures

This directory contains normative compliance fixtures for the
[`replace-label` safe-output type](../replace-label-spec.md) and the formal
predicate model used by the Go testify suite.

The fixtures are the ground truth for RL-001, RL-002, and RL-003 decision
behavior. The formal tests in `pkg/workflow/replace_label_formal_test.go` bind
these fixture scenarios to executable predicates and validate additional
schema, gating, set-computation, staged-mode, and error invariants.

## Fixture Files

| Filename | Scenario | Spec Coverage |
|---|---|---|
| `rl-001-glob-semantics.yaml` | Glob pattern matching for `allowed-add`, `allowed-remove`, and `blocked` follows gobwas/glob semantics | RL-001, T-RL-020–T-RL-023 |
| `rl-002-allowlist-enforcement.yaml` | Non-empty allowlists enforce matches while empty allowlists permit any non-blocked label | RL-002, T-RL-021b, T-RL-022b, T-RL-025 |
| `rl-003-blocklist-ordering.yaml` | Blocklist evaluation occurs before allowlist evaluation (security boundary) | RL-003, T-RL-023–T-RL-024 |
| `rl-004-error-categories.yaml` | Each §7.1 error category has one owning stage and a defined result | `SCHEMA_INVALID`, `MAX_EXCEEDED`, `TARGET_UNRESOLVABLE`, `REPO_NOT_ALLOWED`, `LABEL_BLOCKED`, `LABEL_NOT_ALLOWED`, `GATE_REQUIRED_LABELS`, `GATE_TITLE_PREFIX`, `SETLABELS_FAILED`, `RATE_LIMIT_EXHAUSTED` |
| _Direct test (no standalone fixture file)_ | Empty `setLabels` response fails add-presence verification (`TestFormalTransitionEdge_EmptyResponseLabels`) | RL-057, RL-058 |
| _Direct test (no standalone fixture file)_ | Self-transition is denied unless explicitly listed (`TestFormalTransitionEdge_SelfTransitionRejectedWhenNotListed`) | Transition invariants (Q2, Q4) |
| _Direct test (no standalone fixture file)_ | Duplicate transition entries are idempotent (`TestFormalTransitionEdge_DuplicateTransitionEntriesIdempotent`) | Transition invariants (Q2) |

## Formal Model

- **Preconditions (`F*`)**: non-empty required labels, bounded label/repo lengths,
  count gate (`count < max`), and repository target restrictions.
- **Decision predicates (SMT-style)**:
  - `GlobSemantics(label, pattern[])`
  - `AllowlistPermits(label, allowed[])`
  - `BlocklistRejects(label, blocked[])`
  - `BlocklistBeforeAllowlist` (ordering safety property)
  - `TransitionAllowed(allowedTransitions[], from, to)` (exact-pair state-machine gate)
- **Postconditions**:
  - label set uses remove-then-add arithmetic with deduplication;
  - staged mode reports success with `staged=true` and no write side effects;
  - hard REST failures return `success=false` with non-nil error;
  - post-`setLabels` response verification enforces RL-057/RL-058/RL-059 predicates.

Evaluation order is modeled as: blocked check → allowlist check → gates
(required-labels/title-prefix) → staged/execute branch.

### Error Category Fixtures

`rl-004-error-categories.yaml` contains one scenario for each §7.1 category. The
scenario IDs map to the error codes and their owning stage as follows:

| Category | Code(s) | Fixture scenario | Stage |
|---|---|---|---|
| Schema validation failure | `SCHEMA_INVALID` | `schema-invalid` | 5.1 |
| Count gate exceeded | `MAX_EXCEEDED` | `count-gate-exceeded` | 5.2 |
| Target resolution failure | `TARGET_UNRESOLVABLE` | `target-resolution-failure` | 5.3 |
| Repository not in allowlist | `REPO_NOT_ALLOWED` | `repository-not-allowed` | 5.3 |
| Label validation failure | `LABEL_BLOCKED`, `LABEL_NOT_ALLOWED` | `label-validation-failure` | 5.4 |
| Required-labels gate | `GATE_REQUIRED_LABELS` | `required-labels-gate` | 5.5 |
| Title-prefix gate | `GATE_TITLE_PREFIX` | `title-prefix-gate` | 5.5 |
| REST setLabels failure | `SETLABELS_FAILED` | `set-labels-failure` | 5.8 |
| Rate-limit exhausted | `RATE_LIMIT_EXHAUSTED` | `rate-limit-exhausted` | 5.8 |

`TestFormalReplaceLabelErrorCategoryFixture` checks that all categories and codes
are present exactly once with the documented stage and result.

### Safeguards

Security-critical ordering is enforced as blocklist-before-allowlist for both
`label_to_add` and `label_to_remove` directions. The suite's P3/P4/P11 checks
(`TestFormalBlocklistOrdering`, `TestFormalBlocklistSymmetry`) ensure blocked
labels are denied before any allowlist permit path is considered.

`replace-label` requires the minimum GitHub token scope `issues: write`, including
for pull-request labels. A cross-repository operation must use a token authorized
for that target repository with this scope; it must not assume the workflow's
default token grants access there. The handler-level conformance suite does not
yet verify target-repository grants (RL-051), the minimum scope (RL-054), or the
dedicated per-type token rule when the default token is broader (RL-055); these
remain explicit security gaps.

## Behavioral Coverage Map

| Predicate / Invariant | Test Function | Description |
|---|---|---|
| P1 — GlobSemantics | `TestFormalGlobSemantics` | Star and char-class glob matching from fixture rl-001 |
| P2 — AllowlistPermits | `TestFormalAllowlistEnforcement` | Empty vs non-empty allowlist, add and remove directions |
| P3 + P4 — BlocklistRejects + BlocklistBeforeAllowlist | `TestFormalBlocklistOrdering` | Blocklist takes priority over allowlist; symmetric for add/remove |
| P5 — SchemaRequiredFields | `TestFormalSchemaRequiredFields` | Missing/empty/too-long label_to_remove and label_to_add |
| P6 — RepoMaxLength | `TestFormalRepoMaxLength` | repo field ≤ 256 characters |
| P7 — CountGateExclusive | `TestFormalCountGate` | count < max allowed, count = max rejected, default max = 5 |
| P8 — LabelSetComputation | `TestFormalLabelSetComputation` | Correct set arithmetic; missing-remove proceeds |
| P9 — StagedNoWrite | `TestFormalStagedMode` | staged=true returns success+staged=true, no writes |
| P10 — SingleRESTCall | `TestFormalSingleRESTCall` | Verify exactly one PUT; no separate add/remove |
| P11 — BlocklistAppliesSymmetrically | `TestFormalBlocklistSymmetry` | blocked applies to both label_to_add and label_to_remove |
| P12 — RequiredLabelsGate | `TestFormalRequiredLabelsGate` | All required labels present → proceed; missing → skip |
| P13 — TitlePrefixGate | `TestFormalTitlePrefixGate` | Matching prefix → proceed; non-matching → skip |
| P14 — AddDeduplication | `TestFormalAddDeduplication` | label_to_add appears exactly once in output |
| P15 — HardErrorOnSetLabelsFail | `TestFormalHardErrorOnRESTFail` | REST failure yields success=false, non-nil error |
| Q1 — TransitionSetEmpty | `TestFormalTransitionSetEmptyAllowsAny` | Empty/absent `allowed-transitions` permits any (from, to) pair |
| Q2 — TransitionExactMatch | `TestFormalTransitionExactMatchRequired` | Only exact (from, to) pairs in the list are allowed; reverse/unrelated pairs rejected |
| Q4 — TransitionRejectedYieldsSoftSkip | `TestFormalTransitionRejectedYieldsSoftSkip` | Disallowed transition yields `success=false` without a hard workflow failure |
| Q9 — TransitionConfigShape (single) | `TestFormalTransitionConfigShape` | `LabelTransition` unmarshals `from`/`to` YAML keys correctly |
| Q9 — TransitionConfigShape (list) | `TestFormalTransitionConfigListShape` | `ReplaceLabelConfig.AllowedTransitions` parses a YAML list of transitions |
| Q5 — PostSetLabelsAddPresent | `TestFormalPostSetLabelsAddPresent` | `label_to_add` found in setLabels response ⇒ success |
| Q6 — PostSetLabelsRemoveAbsent | `TestFormalPostSetLabelsRemoveAbsent` | `label_to_remove` still present in response ⇒ failure |
| Q7 — PartialSuccessRejected | `TestFormalPartialSuccessRejected` | Table-driven: missing add / stale remove / both-satisfied cases |
| Q8 — PartialSuccessNoNewErrorCode | `TestFormalPartialSuccessNoNewErrorCode` | Rejected outcome keeps the standard `{success:false, error}` shape, no new error-code field |
| Edge: Exact glob no-wildcard | `TestFormalGlobExactNoWildcard` | Exact pattern `bug` does not match `bug-fix` |
| Edge: Alias fields | `TestFormalItemNumberAliases` | issue_number/pr_number/pull_number resolve correctly |
| Edge: Cross-repo restriction | `TestFormalCrossRepoRestriction` | repo not in allowed-repos is rejected |
| Edge: empty response labels | `TestFormalTransitionEdge_EmptyResponseLabels` | Empty `setLabels` response array fails the add-presence check |
| Edge: self-transition not implicit | `TestFormalTransitionEdge_SelfTransitionRejectedWhenNotListed` | `from == to` is not implicitly allowed unless explicitly listed |
| Edge: duplicate transition entries | `TestFormalTransitionEdge_DuplicateTransitionEntriesIdempotent` | Duplicate entries in the list don't change the allow/deny decision |

Coverage parity check (2026-11-10): verified Behavioral Coverage Map entries
are implemented in:

- `pkg/workflow/replace_label_formal_test.go`
- `pkg/workflow/replace_label_transitions_formal_test.go`
- `actions/setup/js/replace_label.test.cjs`

## Fixture Schema

Each fixture file is a YAML document with the following top-level keys:

```yaml
fixture_id: string          # Unique identifier referencing the RL requirement code
description: string         # Human-readable scenario description
spec_refs:                  # Normative requirements under test (RL codes and § references)
  - string
scenarios:
  - scenario_id: string     # Unique sub-scenario identifier
    description: string     # Sub-scenario description
    input:
      safe_output_config:   # replace-label safe-output configuration under test
        allowed-add: [...]
        allowed-remove: [...]
        blocked: [...]
      message:              # Simulated agent message
        label_to_add: string
        label_to_remove: string
    expected:
      decision: allow | deny   # Required outcome
      error_code: integer | null  # Expected error code on deny
      reason: string           # Expected denial reason substring (informative)
```

## Adding New Fixtures

1. Copy the most relevant existing fixture file.
2. Assign a new `fixture_id` matching the RL requirement code being tested.
3. Update `input.safe_output_config` and `input.message` to reflect the new scenario.
4. Set `expected` fields to match the required outcome.
5. Register the new fixture in the table above and reference it from §9 of
   `specs/replace-label-spec.md`.

## Related Test IDs

The following test IDs defined in the replace-label specification map to these fixtures:

| Test ID | Fixture | Description |
|---------|---------|-------------|
| T-RL-020 | `rl-001-glob-semantics.yaml` | Star glob matches label name substring |
| T-RL-021 | `rl-001-glob-semantics.yaml` | Exact pattern matches only exact name |
| T-RL-021b | `rl-002-allowlist-enforcement.yaml` | Non-empty allowed-add accepts a matching label |
| T-RL-022 | `rl-001-glob-semantics.yaml` | Character class pattern matches correctly |
| T-RL-022b | `rl-002-allowlist-enforcement.yaml` | Non-empty allowed-add rejects a non-matching label |
| T-RL-023 | `rl-001-glob-semantics.yaml`, `rl-003-blocklist-ordering.yaml` | Glob pattern rejects non-matching label; blocked label rejected even when allowed |
| T-RL-024 | `rl-003-blocklist-ordering.yaml` | Blocked label rejected even with wildcard allowed-add |
| T-RL-025 | `rl-002-allowlist-enforcement.yaml` | Empty allowed-remove permits any non-blocked label |

## Generated Test Suite

The formal compliance suite is implemented in:

- `pkg/workflow/replace_label_formal_test.go`
- `pkg/workflow/replace_label_transitions_formal_test.go`

The suite runs fully in-process under Go test, reads the fixture YAML files in
this directory, and does not require a JavaScript runtime to validate the
formal predicates.

The RL-057/RL-058/RL-059 post-`setLabels` checks are enforced by
`actions/setup/js/replace_label.cjs` and covered by
`actions/setup/js/replace_label.test.cjs`.

## Normative MUST Evidence Index (§§5–8)

Each MUST-level requirement is mapped to a test ID and its implementation test,
or to a specific evidence gap. This index is complete when every requirement in
§§5–8 of `specs/replace-label-spec.md` appears below with a test mapping or an
explicit gap; completion does not depend on an owner or a review date.

The remaining gaps are grouped by risk: **pipeline** (RL-009, RL-011, RL-033),
**observability** (RL-027), and **security** (RL-051, RL-054, RL-055). Each
remaining gap below has a risk tag and a one-line rationale.

| Requirement | Test ID(s) or documented gap | Implementation evidence |
|---|---|---|
| §5 ordered processing pipeline | `replace_label.test.cjs`: “executes the label pipeline in order and stops before writes on a gate skip” | Asserts read → read → write order and that a gate skip stops after the first read. |
| RL-008 | T-RL-001–006 | `TestFormalSchemaRequiredFields` |
| RL-009 | **Gap [Pipeline]:** common-pipeline invocation is not directly tested; execution-path coverage is needed to detect divergence from shared safe-output processing. | `TestFormalReplaceLabelP3_ValidationConfigFields` covers config shape only. |
| RL-010 | T-RL-010–011 | `TestFormalCountGate` |
| RL-011 | **Gap [Pipeline]:** expression resolution for `max` is not directly tested; unresolved limits could bypass or misapply the processing bound. | — |
| RL-012 | T-RL-012 | `TestFormalReplaceLabelP3_DefaultMaxFive` |
| RL-013 | T-RL-070–072 | `TestFormalCrossRepoRestriction` |
| RL-014 | **Gap:** malformed `owner/repo` rejection is not directly tested. | — |
| RL-015 | T-RL-070–072 | `TestFormalCrossRepoRestriction` |
| RL-016 | T-RL-014–018 | `TestFormalReplaceLabelP15_TargetModeEnforcement` |
| RL-017 | T-RL-019 | `T-RL-019 rejects when no item number is available` |
| RL-018 | T-RL-014–015 | `TestFormalReplaceLabelP15_TargetModeEnforcement` |
| RL-019 | T-RL-016–017 | `TestFormalReplaceLabelP15_TargetModeEnforcement` |
| RL-020 | T-RL-018 | `TestFormalItemNumberAliases` |
| RL-020a | T-RL-014–019 | `TestFormalReplaceLabelP15_TargetModeEnforcement` |
| RL-021 | T-RL-023–024 | `TestFormalBlocklistOrdering`, `TestFormalBlocklistSymmetry` |
| RL-022 | T-RL-020–023 | `TestFormalBlocklistOrdering`, `TestFormalBlocklistSymmetry` |
| RL-023 | `replace_label.test.cjs`: warning-only skip assertions for target resolution, validation, count, repository, and gate outcomes. | `replace_label.test.cjs` checks `core.warning()` and no `core.error()` for soft skips. |
| RL-024 | T-RL-030–031 | `TestFormalRequiredLabelsGate`; JS required-label skip test |
| RL-025 | T-RL-032–033 | `TestFormalTitlePrefixGate`; JS title-prefix skip test |
| RL-026 | T-RL-030–033 | JS required-label and title-prefix skip tests |
| RL-027 | T-RL-060–061; **Gap [Observability]:** preview log text is not asserted directly; operators need a verified record of the proposed change. | `T-RL-060 does not call setLabels in staged mode` |
| RL-028 | T-RL-062 | `TestFormalReplaceLabelP9_StagedHandlerResult`; staged handler test |
| RL-029 | T-RL-040 | `TestFormalLabelSetComputation` |
| RL-033 | T-RL-040; **Gap [Pipeline]:** computation is not tied to the live stage-5 REST response; stale or agent-supplied state must not drive the write. | `TestFormalLabelSetComputation` models the current-label input. |
| RL-034 | T-RL-044 | `TestFormalReplaceLabelP11_IdempotentMissingRemove`; JS missing-remove test |
| RL-036 | T-RL-050 | `TestFormalSingleRESTCall` |
| RL-037 | T-RL-054; `replace_label.test.cjs`: “T-RL-046b retries” and “T-RL-037 returns a hard error after transient retries are exhausted.” | Handler tests cover 403/429, 5xx, transport timeout, and retry exhaustion. |
| RL-038 | T-RL-051; `replace_label.test.cjs`: “asserts success logs, complete result fields, and execution-state metadata.” | Asserts the success and updated-label log entries. |
| RL-039 | `replace_label.test.cjs`: “asserts success logs, complete result fields, and execution-state metadata.” | Asserts the exact success-result key set. |
| RL-040 | `replace_label.test.cjs`: “asserts success logs, complete result fields, and execution-state metadata.” | Asserts `before_state` and `after_state` fields and label values. |
| RL-041 | T-RL-040, T-RL-050 | `TestFormalLabelSetComputation` |
| RL-042 | T-RL-053 | `TestFormalAddDeduplication` |
| RL-043 | T-RL-050; **gap:** GitHub's server-side atomicity guarantee is not locally testable. | `TestFormalSingleRESTCall` |
| RL-044 | `replace_label.test.cjs`: warning-only assertions cover missing fields, count limit, target/repository rejection, blocked/allowlist validation, and required-label/title gates. | Each soft-skip path asserts warning output and no `core.error()`. |
| RL-045 | `replace_label.test.cjs`: HTTP 404, retry exhaustion, REST failure, and postcondition failures assert hard-error logging. | `core.error()` is asserted for each hard-error category exercised by the handler. |
| RL-046 | T-RL-052 | `TestFormalHardErrorOnRESTFail`; JS setLabels failure test |
| RL-046a | `replace_label.test.cjs`: “T-RL-046a rejects an HTTP 404 without retrying and logs a hard error.” | Asserts one write attempt, failure, and `core.error()`. |
| RL-046b | `replace_label.test.cjs`: “T-RL-046b retries” and “T-RL-037 returns a hard error after transient retries are exhausted.” | Covers retry success for 5xx/transport timeout and failure after exhaustion. |
| RL-048 | T-RL-054 | `T-RL-054 retries rate-limit response` (403 and 429) |
| RL-057 | T-RL-057 | `TestFormalPostSetLabelsAddPresent`, `TestFormalPostSetLabelsRemoveAbsent` |
| RL-058 | T-RL-058 | `TestFormalPartialSuccessRejected` |
| RL-059 | T-RL-059 | `TestFormalPartialSuccessNoNewErrorCode` |
| RL-049 | T-RL-020–025 | `TestFormalBlocklistOrdering`; JS allow/blocklist tests |
| RL-049a | T-RL-026 | `T-RL-026 rechecks blocklist state before setLabels`; `TestFormalBlockedLabelAddedViaReplaceLabelMidBlocklistChange_NoWriteAPI` |
| RL-050 | T-RL-070–072 | `TestFormalCrossRepoRestriction` |
| RL-051 | **Gap [Security]:** target-repository token grants are not tested; repository allowlisting alone does not establish write authorization. | — |
| RL-052 | T-RL-041 | JS nonexistent-label hard-error test |
| RL-053 | T-RL-030–031 | JS required-label gate test uses the REST response; the agent message has no label-state field. |
| RL-054 | **Gap [Security]:** the minimum `issues: write` scope is unverified; weaker grants could fail at runtime while broader grants violate least privilege. | — |
| RL-055 | **Gap [Security]:** per-type token selection is unverified; the workflow-wide token may carry unrelated permissions. | — |
| RL-056 | T-RL-060 | `T-RL-060 does not call setLabels in staged mode` |
