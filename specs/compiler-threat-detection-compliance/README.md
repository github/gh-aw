# Compiler Threat Detection Compliance Map

This directory maps the threat-detection rule catalog in the [Compiler Threat Detection Specification](../compiler-threat-detection-spec.md#8-compliance-testing) to its conformance test IDs. Each active `CTR-*` rule has one required `T-CTR-*` test ID. Test IDs are allocated from a single sequence shared with the Section 6 norm tests below, so a test ID number does not necessarily match its rule ID number.

## Structure

Baseline rule implementation and test-file locations are maintained in [spec §7.1](../compiler-threat-detection-spec.md#71-baseline-rule-mapping). Use that table as the canonical structure map for `CTR-*` rule source paths and test targets; this README only summarizes the compliance ID crosswalk.

| Rule ID | Test ID |
|---------|---------|
| CTR-001 | T-CTR-001 |
| CTR-002 | T-CTR-002 |
| CTR-003 | T-CTR-003 |
| CTR-004 | T-CTR-004 |
| CTR-005 | T-CTR-005 |
| CTR-006 | T-CTR-006 |
| CTR-007 | T-CTR-007 |
| CTR-008 | T-CTR-008 |
| CTR-009 | T-CTR-009 |
| CTR-010 | T-CTR-010 |
| CTR-011 | T-CTR-011 |
| CTR-012 | T-CTR-012 |
| CTR-013 | T-CTR-013 |
| CTR-014 | T-CTR-014 |
| CTR-015 | T-CTR-015 |
| CTR-016 | T-CTR-016 |
| CTR-017 | T-CTR-017 |
| CTR-018 | T-CTR-018 |
| CTR-019 | T-CTR-019 |
| CTR-020 | T-CTR-020 |
| CTR-021 | T-CTR-021 |
| CTR-022 | T-CTR-022 |
| CTR-023 | T-CTR-023 |
| CTR-025 | T-CTR-039 |
| CTR-026 | T-CTR-041 |
| CTR-027 | T-CTR-042 |

Note: `CTR-025` maps to `T-CTR-039`, `CTR-026` maps to `T-CTR-041`, and `CTR-027` maps to `T-CTR-042` because `T-CTR-024` through `T-CTR-038` and `T-CTR-040` were already allocated to Section 6 false-positive and optimizer protocol norms. The shared `T-CTR-*` sequence is intentionally non-sequential with respect to `CTR-*` rule IDs.

The test triggers, expected compiler actions, and stable diagnostics are defined in [Section 8.1](../compiler-threat-detection-spec.md#81-test-id-catalog). The implementation and concrete test-file mappings are defined in [Section 7.1](../compiler-threat-detection-spec.md#71-baseline-rule-mapping).

## Section 5.4 Deprecation Policy Annotations

So that the [§5.4 Deprecation Policy](../compiler-threat-detection-spec.md#54-deprecation-policy) obligations are mechanically verifiable, a deprecated rule is annotated as follows:

| Artifact | Annotation |
|----------|------------|
| Section 5.1 catalog entry | `- **CTR-NNN Name** [Deprecated in vX.Y.Z: reason]: ...` (row retained) |
| Section 7.1 mapping row | `\| CTR-NNN Name [Deprecated in vX.Y.Z] \| — \| — \|` (row retained, implementation cleared) |
| Section 8.1 test row and the crosswalk row above | annotated with `[DEPRECATED]` and dropped from the required conformance gate |
| Section 10 change log | an entry naming the rule ID, the deprecation version, and the rationale |

These annotations are verified against the specification artifacts by `pkg/workflow/threat_detection_deprecation_policy_formal_test.go`.

## Section 6.4 False-Positive Handling Norms

| Test ID | Norm |
|---------|------|
| T-CTR-024 | Suppression validation requires `rule` and a non-empty `reason`. |
| T-CTR-025 | Active suppressions retain `rule`, `reason`, and `expires` for audit. |
| T-CTR-026 | MUST-level suppressions have a 10-business-day resolution SLA. |
| T-CTR-027 | SLA breaches include `rule`, `reason`, `age_business_days`, `owner`, and `expires`. |
| T-CTR-028 | MUST-level suppressions older than 20 business days create a follow-up sync action. |
| T-CTR-029 | Expired suppressions are re-evaluated and treated as absent. |

## Section 6.6 Optimizer Failure Safeguards

| Test ID | Norm |
|---------|------|
| T-CTR-030 | API unavailability emits `OPTIMIZER_DEGRADED`. |
| T-CTR-031 | Degraded evaluation cannot mutate specifications or open a pull request. |
| T-CTR-032 | API retry uses bounded exponential back-off. |
| T-CTR-033 | Runner timeout emits `OPTIMIZER_TIMEOUT`. |
| T-CTR-034 | Runner timeout discards partial output. |
| T-CTR-035 | The workflow defines a timeout and same-day retry behavior. |
| T-CTR-036 | Rate limiting applies `RATE_LIMIT_RETRY_CONFIG`. |
| T-CTR-037 | Exhausted rate limiting emits `OPTIMIZER_RATE_LIMITED`. |
| T-CTR-038 | Rate-limited runs remain incomplete and retry in the next window. |
| T-CTR-040 | Missed scheduled optimizer runs emit `OPTIMIZER_MISSED_CRON`, remain incomplete, and create a follow-up sync action. |

The Section 6 norm tests are implemented in `pkg/workflow/compiler_threat_optimizer_protocol_test.go`. Run them with:

```bash
go test -run "TestThreatSuppression|TestThreatOptimizer" ./pkg/workflow/
```

`TestFormal_ComplianceReadmeNormTestNamesStaySynced` in
`pkg/workflow/compiler_threat_optimizer_protocol_naming_sync_formal_test.go` enforces that every
`T-CTR-*` ID in the Section 6.4/6.6 norm tables above has a matching test function name in
`compiler_threat_optimizer_protocol_test.go`, and vice versa, so the two artifacts cannot silently
drift apart if a norm test is renamed or a table row changes.
