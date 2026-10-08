# ADR-67023: Acknowledge Experimental-Feature Notices via an Explicit Compile Flag

**Date**: 2026-10-08
**Status**: Draft
**Deciders**: pelikhan (PR author), gh-aw maintainers

---

### Context

`gh aw compile --dry-run` is the validation gate used by agentic debugging workflows: it fails whenever the compiler reports any warning, so that security findings, model diagnostics, and scanner failures cannot be silently ignored. Several built-in features are still marked experimental and emit a compiler notice on every compilation, which means any workflow that legitimately uses them can never produce a clean dry-run. Workflows such as the ESLint factory dispatcher, miner, refiner, and monster depend on those features, so operators had no way to validate them end to end without either disabling the gate wholesale or suppressing warnings globally. The project requires that any relaxation stay narrow, auditable, and visible in both human and JSON output.

### Decision

We will add an explicit, opt-in `compile --allow-experimental` flag that acknowledges *only* built-in experimental-feature notices during dry-run validation. The flag does not suppress the notices: it records `GetExperimentalWarningCount()` as `AcceptedExperimentalWarnings`, keeps the notices in the printed diagnostics and warning counts, reports the accepted count in the JSON report, and relaxes the dry-run validity predicate to `stats.Warnings <= report.AcceptedExperimentalWarnings`. All other warnings, security validation, model diagnostics, and scanner failures remain fatal, and the flag grants no execution or policy-installation authority.

### Alternatives Considered

#### Alternative 1: Keep experimental notices fatal and remove experimental usage

Workflows that need experimental features would have to wait until those features are promoted to stable, or be rewritten to avoid them. This preserves the strictest gate with no new surface area, but it blocks validation of the ESLint factory workflows that already ship in this repository and provides no path to prove those workflows compile cleanly. Rejected because it makes the gate unusable for an existing, supported class of workflows.

#### Alternative 2: Generic warning-suppression flag (e.g. `--ignore-warnings` or a per-code allowlist)

A general mechanism would cover experimental notices and any future noisy diagnostic with one flag. It was a close call for flexibility, but a broad suppression switch would also mask security findings, model diagnostics, and scanner errors — exactly the signals the dry-run gate exists to enforce — and a per-code allowlist adds a configuration surface that must be reviewed on every use. Rejected in favor of a single, narrowly scoped, self-documenting flag.

#### Alternative 3: Frontmatter opt-in per workflow

Each workflow could declare that it accepts experimental features. This moves the acknowledgment into the artifact itself, but it makes the acknowledgment permanent and invisible at validation time, and a committed file cannot represent a human operator's explicit, per-run acceptance. Rejected because acknowledgment should be an operator action, not a workflow property.

### Consequences

#### Positive
- Workflows that legitimately use experimental features can reach a clean, auditable dry-run result without weakening any other check.
- Acceptance is explicit and quantified: the accepted count appears in the JSON report and the human summary message, so reviewers can see exactly how much was waived.
- The relaxation is bounded by a count comparison, so any additional non-experimental warning still fails the gate.

#### Negative
- Adds a user-visible CLI flag and a new field to the dry-run report, both of which are now part of the compatibility surface.
- The validity predicate compares warning *counts* rather than identities, so a future change that emits an unrelated warning while an experimental notice disappears could, in principle, fall under the same threshold.
- Operators may habitually pass `--allow-experimental`, reducing pressure to promote or remove experimental features.

#### Neutral
- Documentation in `.github/aw/debug-agentic-workflow.md` and `docs/src/content/docs/reference/compilation-process.md` must state that the flag conveys no live-execution or policy-installation authorization.
- The flag threads through `CompileConfig.AllowExperimental` and `enforceDevelopmentDiagnostics`, so any future dry-run diagnostic category must decide explicitly whether it is in scope.
- Shipped as a `minor` changeset alongside unrelated ESLint-factory admission and paginated remote-workflow-discovery fixes in the same PR.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
