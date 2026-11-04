# AWF Config Sources Compliance Fixtures

This directory contains conformance test IDs and fixture stubs for the `DriftRecord` entity
and safeguards defined in the [AWF Config Canonical Sources Specification](../awf-config-sources-spec.md).

All automation and agents that produce or consume drift reports **MUST** use the `DriftRecord` schema
defined in §3.1 of the specification for structured drift output.

---

## DriftRecord Conformance Tests

The following test IDs cover the `DriftRecord` schema and its usage requirements from §3.1 and §7.5.

| Test ID | Requirement | Description | Implementation file |
|---------|-------------|-------------|---------------------|
| T-DR-001 | §3.1 — required fields | `DriftRecord` MUST include `property_path`, `drift_category`, `suggested_action`, and `detected_at`; records missing any required field are invalid and MUST be rejected. | `pkg/workflow/awf_config_drift_test.go` |
| T-DR-002 | §3.1 — `drift_category` enum | `drift_category` MUST be one of `missing_in_ghaw`, `missing_in_schema`, or `spec_mismatch`; any other value is invalid. | `pkg/workflow/awf_config_drift_test.go` |
| T-DR-003 | §3.1 — `detected_at` format | `detected_at` MUST be a valid ISO 8601 UTC timestamp; non-conforming values MUST be rejected. | `pkg/workflow/awf_config_drift_test.go` |
| T-DR-004 | §3.1 — `suggested_action` non-empty | `suggested_action` MUST NOT be empty (`minLength: 1`); an empty string MUST be rejected. | `pkg/workflow/awf_config_drift_test.go` |
| T-DR-005 | §3.1 — no additional properties | `DriftRecord` objects MUST NOT include properties beyond the four required fields; additional properties MUST be rejected. | `pkg/workflow/awf_config_drift_test.go` |
| T-DR-006 | §7.5.1 — corrective PR trigger | When any `DriftRecord` in the output list has `drift_category` of `missing_in_ghaw` or `spec_mismatch`, the detecting automation MUST open a corrective PR (CR-05). | `pkg/workflow/awf_config_drift_test.go` |
| T-DR-007 | §7.5.1 — SLA escalation trigger | The test verifies that actionable drift triggers escalation only after the CR-06 SLA window is exceeded. CR-06 separately requires the issue within 1 business day with an owner, unblock plan, and revised ETA; T-DR-011 covers owner selection and acknowledgement. | `pkg/workflow/awf_config_drift_test.go` |
| T-DR-008 | §7.5.1 — corrective PR embeds records | The corrective PR description MUST embed the full `DriftRecord` list as JSON. | `pkg/workflow/awf_config_drift_test.go` |
| T-DR-009 | §7.5.1 — empty list is valid | An empty `DriftRecord` list (no drift detected) is a valid output and MUST NOT trigger corrective PR or escalation actions. | `pkg/workflow/awf_config_drift_test.go` |
| T-DR-010 | §7.2 Step 5 integration | The drift detection procedure Step 5 MUST produce a list of zero or more `DriftRecord` objects; the output format MUST be a JSON array conforming to the §3.1 schema. | `pkg/workflow/awf_config_drift_test.go` |
| T-DR-011 | §6 CR-06a — escalation-owner acknowledgement | Escalations select a non-empty owner using the documented fallback and require acknowledgement within one business day. | `pkg/workflow/awf_config_safeguards_formal_test.go` |

---

## Safeguards Conformance Tests

The following test IDs cover the unavailable-source safeguards from §8.

| Test ID | Requirement | Description | Implementation file |
|---------|-------------|-------------|---------------------|
| T-DR-SAFE-001 | §8 item 1 — snapshot storage and freshness | Every invocation MUST select the stable path for its runner type, expire snapshots older than 168 hours, mark expired-snapshot runs degraded, and SHOULD delete snapshots older than 14 days. | `pkg/workflow/awf_config_safeguards_formal_test.go` |
| T-DR-SAFE-002 | §8 item 2 — retrieval warning | A canonical-source retrieval failure SHOULD identify the failing source paths and UTC timestamp. | `pkg/workflow/awf_config_safeguards_formal_test.go` |
| T-DR-SAFE-003 | §8 item 3 — degraded-run safety | An unavailable or expired canonical source MUST mark the run degraded and MUST prevent destructive validation actions. | `pkg/workflow/awf_config_safeguards_formal_test.go` |
| T-DR-SAFE-004 | §8 item 4 — scheduled persistence | A tracking issue SHOULD be opened or updated only when unavailability persists through the next scheduled cron invocation; manual and ad hoc runs do not advance the threshold. | `pkg/workflow/awf_config_safeguards_formal_test.go` |

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
```

To run related tests:

```bash
go test -v -run "TestDriftRecord|TestAWFConfigSafeguard" ./pkg/workflow/
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
