# ADR-66940: Report Dry-Run Coverage Explicitly and Fail on Stale Model Inventory Diagnostics

**Date**: 2026-10-09
**Status**: Draft
**Deciders**: pelikhan (PR author). Maintainer acceptance is not implied by this record.

---

### Context

`gh aw compile --dry-run` disables compiler-managed GitHub mutations, but its output did not describe what the gate actually covered. Scanner invocation was implicit: a scanner that was never requested, a scanner that ran over zero emitted lock files, and a scanner that ran and passed were indistinguishable in both text and `--json` output. Readers could therefore interpret a clean dry-run as broader assurance than compilation provides — documentation in `.github/aw/debug-agentic-workflow.md` and `docs/src/content/docs/reference/staged-mode.md` reinforced that reading.

Separately, `PrepareCompileModelValidation` built the active model inventory from `buildModelsReport` but discarded `report.Warnings`. When an inventory refresh or log collection partially failed, compilation proceeded silently against stale cached observations, so model-name validation could pass on evidence the compiler itself knew was incomplete.

The constraint is that dry-run is a *compile-only* gate. It must not imply execution authorization, sandboxing, artifact quarantine, or per-script/per-image proof, and the existing `--json` result array shape must stay backward compatible for consumers.

### Decision

We will make dry-run coverage an explicit, machine-readable artifact and treat discarded model-inventory diagnostics as gate failures in dry-run mode.

Concretely: a new `DryRunCompileReport` (`pkg/cli/compile_development_report.go`) records the overall `gate`, `compile_only`/`execution_authorized` flags, the forced required flags, per-scanner `{requested, status}` entries distinguishing `passed`/`failed`/`not_run`, image-validation configuration, model-inventory availability, and the list of unverified effect categories. It is surfaced as a stderr text summary and, under `--json`, as an **additive** `ValidationResult` with `Scope: "batch"` and `Workflow: "dry-run"` carrying a `dry_run` field — existing per-workflow entries are unchanged. Batch scanner plumbing changes from `reportError(tool, err)` to `reportResult(tool, inputs, err)` so that "ran over zero inputs" is reported as `not_run` rather than success.

Model-inventory warnings are preserved on `CompileConfig.modelValidationWarnings` and emitted as `model_inventory_warning` batch diagnostics that count as errors in the development/dry-run path. Ordinary `--models` compilation remains warning-only, so the stricter behavior is scoped to the gate that claims comprehensiveness.

### Alternatives Considered

#### Alternative 1: Keep the text-only summary and leave the JSON schema untouched

A human-readable stderr line alone would have been the smallest change and would avoid any schema surface. It was rejected because the primary consumers of `--dry-run` are CI gates and the debugging workflow, which need to assert on scanner coverage programmatically. A text line cannot be checked without brittle parsing, and the ambiguity between "not requested" and "passed" would persist for automation.

#### Alternative 2: Make model-inventory warnings hard errors for all `compile --models` runs

This is the most internally consistent option: if the inventory is known to be stale, no model validation result is trustworthy. It was a genuinely close call and was rejected on blast radius — ordinary compilation runs on developer machines and in many repository workflows, and transient log-collection failures would start breaking unrelated compiles. Scoping strictness to dry-run keeps the stricter promise where the stronger claim is made.

#### Alternative 3: Add a separate `--dry-run-report` output file or flag

A dedicated report artifact would decouple the summary from the result array entirely. It was rejected because it adds a new CLI surface and a second output channel for consumers to discover and wire up, while the existing `--json` array is already the documented machine-readable contract.

### Consequences

#### Positive
- Dry-run results are now auditable: a consumer can assert that a specific scanner reached `passed` rather than inferring it from the absence of errors.
- Stale model-inventory evidence can no longer silently back a passing dry-run gate.
- Documentation and the report share the same explicit disclaimer that compilation does not authorize execution, reducing overstated staging guarantees.

#### Negative
- The dry-run gate becomes stricter and may newly fail on transient inventory refresh/log-collection failures that previously passed, which can be perceived as flakiness.
- `DryRunCompileReport` is a new serialized structure; its field names (`gate`, scanner status vocabulary) become a de-facto compatibility surface that future changes must preserve.
- Scanner plumbing now threads an `inputs` count through every batch runner, adding a parameter that each new scanner integration must remember to populate correctly.

#### Neutral
- The `--json` change is additive; existing per-workflow result consumers are unaffected but will see one extra array element in dry-run mode.
- `compileBuildModelsReport` is introduced as an indirection over `buildModelsReport` purely to make the diagnostics path testable.
- Coverage reported here describes runner invocations, not per-script or per-image assurance; preflight failures may still terminate before any summary is emitted.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
