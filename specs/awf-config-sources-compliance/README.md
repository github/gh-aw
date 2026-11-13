# AWF Config Sources Compliance Fixtures

This directory contains conformance test IDs and fixture stubs for the `DriftRecord` entity
and safeguards defined in the [AWF Config Canonical Sources Specification](../awf-config-sources-spec.md).

All automation and agents that produce or consume drift reports **MUST** use the `DriftRecord` schema
defined in §3.1 of the specification for structured drift output.

## Completion Criterion

This index is complete when every listed test ID maps to an existing named test
and the documented conformance test command passes. Keep test names and IDs
synchronized when tests are renamed or added; no owner or review date is required.

---

## DriftRecord Conformance Tests

The following test IDs cover the `DriftRecord` schema and its usage requirements from §3.1 and §7.5.

| Test ID | Requirement | Test function(s) | Description | Implementation file |
|---------|-------------|-----------------|-------------|---------------------|
| T-DR-001 | §3.1 — required fields | `TestDriftRecord_TDR001_RequiredFields` ([file](../../pkg/workflow/awf_config_drift_test.go)) | `DriftRecord` requires all four fields; records missing any are rejected. | `pkg/workflow/awf_config_drift_test.go` |
| T-DR-002 | §3.1 — `drift_category` enum | `TestDriftRecord_TDR002_DriftCategoryEnum` ([file](../../pkg/workflow/awf_config_drift_test.go)) | Only the three documented categories are valid. | `pkg/workflow/awf_config_drift_test.go` |
| T-DR-003 | §3.1 — `detected_at` format | `TestDriftRecord_TDR003_DetectedAtFormat` ([file](../../pkg/workflow/awf_config_drift_test.go)) | `detected_at` must be a valid ISO 8601 UTC timestamp. | `pkg/workflow/awf_config_drift_test.go` |
| T-DR-004 | §3.1 — `suggested_action` non-empty | `TestDriftRecord_TDR004_SuggestedActionNonEmpty` ([file](../../pkg/workflow/awf_config_drift_test.go)) | Empty `suggested_action` is rejected. | `pkg/workflow/awf_config_drift_test.go` |
| T-DR-005 | §3.1 — no additional properties | `TestDriftRecord_TDR005_NoAdditionalProperties` ([file](../../pkg/workflow/awf_config_drift_test.go)) | Properties beyond the four required fields are rejected. | `pkg/workflow/awf_config_drift_test.go` |
| T-DR-006 | §7.5.1 — corrective PR trigger | `TestDriftRecord_TDR006_CorrectivePRTrigger` ([file](../../pkg/workflow/awf_config_drift_test.go)) | Actionable drift triggers a corrective PR (CR-05). | `pkg/workflow/awf_config_drift_test.go` |
| T-DR-007 | §7.5.1 — SLA escalation trigger | `TestDriftRecord_TDR007_SLAEscalationTrigger` ([file](../../pkg/workflow/awf_config_drift_test.go)) | Escalation occurs only after the CR-06 SLA window; T-DR-011 covers owner and acknowledgement. | `pkg/workflow/awf_config_drift_test.go` |
| T-DR-008 | §7.5.1 — corrective PR embeds records | `TestDriftRecord_TDR008_CorrectivePREmbedsRecords` ([file](../../pkg/workflow/awf_config_drift_test.go)) | The corrective PR description embeds the full record list as JSON. | `pkg/workflow/awf_config_drift_test.go` |
| T-DR-009 | §7.5.1 — empty list is valid | `TestDriftRecord_TDR009_EmptyListValid` ([file](../../pkg/workflow/awf_config_drift_test.go)) | An empty list does not trigger corrective PR or escalation actions. | `pkg/workflow/awf_config_drift_test.go` |
| T-DR-010 | §7.2 Step 5 integration | `TestDriftRecord_TDR010_Step5Integration` ([file](../../pkg/workflow/awf_config_drift_test.go)) | Step 5 produces a JSON array conforming to the §3.1 schema. | `pkg/workflow/awf_config_drift_test.go` |
| T-DR-011 | §6 CR-06a — escalation-owner acknowledgement | `TestAWFConfigSafeguard_TDR011_EscalationOwnerAssignmentFallbackChain` ([file](../../pkg/workflow/awf_config_safeguards_formal_test.go)); `TestAWFConfigSafeguard_TDR011_EscalationOwnerMustNotBeUnassigned` ([file](../../pkg/workflow/awf_config_safeguards_formal_test.go)); `TestAWFConfigSafeguard_TDR011_EscalationAcknowledgementWindow` ([file](../../pkg/workflow/awf_config_safeguards_formal_test.go)) | Escalations select a non-empty owner using the fallback and require timely acknowledgement. | `pkg/workflow/awf_config_safeguards_formal_test.go` |

---

## Safeguards Conformance Tests

The following test IDs cover the unavailable-source safeguards from §8.

| Test ID | Requirement | Test function(s) | Description | Implementation file |
|---------|-------------|-----------------|-------------|---------------------|
| T-DR-SAFE-001 | §8 item 1 — snapshot storage and freshness | `TestAWFConfigSafeguard_TDRSAFE001_SnapshotExpiryBoundary` ([file](../../pkg/workflow/awf_config_safeguards_formal_test.go)); `TestAWFConfigSafeguard_TDRSAFE001_SnapshotShouldDeleteAt14Days` ([file](../../pkg/workflow/awf_config_safeguards_formal_test.go)); `TestAWFConfigSafeguard_TDRSAFE001_SnapshotStoragePathSelection` ([file](../../pkg/workflow/awf_config_safeguards_formal_test.go)) | Selects runner-specific storage, expires snapshots after 168 hours, and checks 14-day deletion. | `pkg/workflow/awf_config_safeguards_formal_test.go` |
| T-DR-SAFE-002 | §8 item 2 — retrieval warning | `TestAWFConfigSafeguard_TDRSAFE002_RetrievalWarning` ([file](../../pkg/workflow/awf_config_safeguards_formal_test.go)) | Checks source paths and UTC timestamp in the retrieval warning. | `pkg/workflow/awf_config_safeguards_formal_test.go` |
| T-DR-SAFE-003 | §8 item 3 — degraded-run safety | `TestAWFConfigSafeguard_TDRSAFE003_DegradedRunSafety` ([file](../../pkg/workflow/awf_config_safeguards_formal_test.go)) | Unavailable or expired sources mark the run degraded and prevent destructive actions. | `pkg/workflow/awf_config_safeguards_formal_test.go` |
| T-DR-SAFE-004 | §8 item 4 — scheduled persistence | `TestAWFConfigSafeguard_TDRSAFE004_ScheduledPersistence` ([file](../../pkg/workflow/awf_config_safeguards_formal_test.go)) | A tracking issue advances only after scheduled persistence; ad hoc runs do not advance the threshold. | `pkg/workflow/awf_config_safeguards_formal_test.go` |

## Conformance Registry Tests

The registry conformance tests are implemented in
[`pkg/workflow/awf_config_conformance_registry_formal_test.go`](../../pkg/workflow/awf_config_conformance_registry_formal_test.go):

- [`TestFormalConformanceRegistry_BaselineColumnMapping`](../../pkg/workflow/awf_config_conformance_registry_formal_test.go)
- [`TestFormalConformanceRegistry_P1_TestIDMonotonicity`](../../pkg/workflow/awf_config_conformance_registry_formal_test.go)
- [`TestFormalConformanceRegistry_P1_EmptyRegistryStartsAtOne`](../../pkg/workflow/awf_config_conformance_registry_formal_test.go)
- [`TestFormalConformanceRegistry_P2_TestIDNoDuplicates`](../../pkg/workflow/awf_config_conformance_registry_formal_test.go)
- [`TestFormalConformanceRegistry_P3_TestIDFormatWellFormed`](../../pkg/workflow/awf_config_conformance_registry_formal_test.go)
- [`TestFormalConformanceRegistry_P4_PlaceholderIDRejectedAsFinal`](../../pkg/workflow/awf_config_conformance_registry_formal_test.go)
- [`TestFormalConformanceRegistry_P5_RowHasRequirementReference`](../../pkg/workflow/awf_config_conformance_registry_formal_test.go)
- [`TestFormalConformanceRegistry_P6_RowHasImplementationFile`](../../pkg/workflow/awf_config_conformance_registry_formal_test.go)
- [`TestFormalConformanceRegistry_P7_SafeguardRowRoutingDecision`](../../pkg/workflow/awf_config_conformance_registry_formal_test.go)
- [`TestFormalConformanceRegistry_P8_SpecCrossReferenceRequired`](../../pkg/workflow/awf_config_conformance_registry_formal_test.go)
- [`TestFormalConformanceRegistry_P9_DriftSeriesVsSafeguardSeriesDisjoint`](../../pkg/workflow/awf_config_conformance_registry_formal_test.go)
- [`TestFormalConformanceRegistry_EdgeCase_FourDigitRollover`](../../pkg/workflow/awf_config_conformance_registry_formal_test.go)
- [`TestFormalConformanceRegistry_EdgeCase_SafeguardOnlyRegistryDoesNotAffectPlainSeries`](../../pkg/workflow/awf_config_conformance_registry_formal_test.go)
- [`TestFormalConformanceRegistry_EdgeCase_MissingImplementationFileIsInvalid`](../../pkg/workflow/awf_config_conformance_registry_formal_test.go)

---

## Spec Reference

- **Specification**: `specs/awf-config-sources-spec.md`
- **Repository structure**: [Structure](../awf-config-sources-spec.md#structure)
- **Defining section**: §3.1 — DriftRecord
- **Related sections**: §7.2 (Drift Detection Procedure), §6 (Conformance Requirements CR-05, CR-06), §8 (Safeguards)

---

## Running Conformance Tests

Conformance tests that validate `DriftRecord` schema compliance are implemented in:

```
pkg/workflow/awf_config_drift_test.go   — DriftRecord schema validation and usage (T-DR-001 through T-DR-010; T-DR-005: TestDriftRecord_TDR005_NoAdditionalProperties)
pkg/workflow/awf_config_safeguards_formal_test.go — unavailable-source safeguards (T-DR-SAFE-001 through T-DR-SAFE-004) and CR-06a escalation-owner acknowledgement (T-DR-011)
pkg/workflow/awf_config_conformance_registry_formal_test.go — registry column mapping, identifier invariants, and specification/file cross-references
```

To run related tests:

```bash
go test -v -run "TestDriftRecord|TestAWFConfigSafeguard|TestFormalConformanceRegistry" ./pkg/workflow/
```

---

## Adding New Conformance Tests

1. Assign a new `T-DR-xxx` identifier (increment from the last used ID).
2. Add a row to the table above with the test ID, requirement reference (§ number), and description.
3. Implement the test in the conformance test file listed above.
4. Cross-reference the new test ID from the relevant subsection of `specs/awf-config-sources-spec.md`.

## Adding New Safeguard Conformance Tests

1. Assign the next available `T-DR-###` identifier for new safeguard behavior that is not part of the existing unavailable-source series by running this deterministic lookup from the repository root:
   ```bash
   next=$(grep -Rho 'T-DR-[0-9]\{3\}' specs/awf-config-sources-compliance specs/awf-config-sources-spec.md \
     | sed 's/T-DR-//' | sort -n | tail -1 | awk '{printf "T-DR-%03d", $1 + 1}')
   printf '%s\n' "$next"
   ```
   The unavailable-source series uses the separate `T-DR-SAFE-###` namespace; continue that series independently when adding another unavailable-source safeguard case.
2. Add a row to the Safeguards Conformance Tests table with the requirement reference, for example `§8 Safeguards — degraded-run reporting`.
3. Implement the test in `pkg/workflow/awf_config_safeguards_formal_test.go` when it exercises safeguard behavior, or in the closest AWF config drift test file when the safeguard spans drift output and schema validation.
4. Cross-reference the new safeguard test ID from the relevant safeguard bullet in `specs/awf-config-sources-spec.md` so the spec and fixture index stay synchronized.
