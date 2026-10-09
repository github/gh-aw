# ADR-67023: Acknowledge Experimental-Feature Notices via an Explicit Compile Flag

**Date**: 2026-10-08
**Status**: Proposed
**Deciders**: pelikhan (PR author), gh-aw maintainers

---

### Context

`gh aw compile --dry-run` is the compile-only validation gate used by agentic debugging workflows. It rejects compiler warnings, structured validation warnings, model diagnostics, and scanner failures. The ESLint factory workers use experimental built-ins such as LSP and steering, whose notices also fail this gate even when every other check passes. The operator explicitly requested a way to acknowledge those notices while validating the factory with the locally built CLI.

Acknowledgment must remain narrow, auditable, and visible in human and JSON output. Successful compilation must not authorize live execution, install work-queue Policy, or bypass runtime controls. This decision covers notice acknowledgment; daily factory admission and paginated workflow discovery are separate changes needed for the original factory debugging task.

### Decision

Add an explicit `compile --allow-experimental` flag, defaulting to false, that acknowledges only built-in experimental-feature notices during dry-run validation.

The fixed built-in feature table records each notice in a dedicated experimental counter and the ordinary warning counter. Neither warning-text prefixes nor the batch-only feature-usage map classify accepted notices. The flag preserves printed notices and warning counts, and reports the accepted count as `accepted_experimental_warnings` in dry-run JSON.

Both aggregate and compiler warning counts must not exceed the acknowledged experimental count. Structured workflow warnings, safe-update and schedule warnings, model-inventory diagnostics, and scanner failures are checked independently and remain fatal. The final report also requires successful workflow results without errors or structured warnings. No required validation flag or runtime protection is disabled.

Regression coverage includes single and batch notice accounting, counter resets, CLI/config propagation, and rejection of forged notice text, ordinary warnings, structured warnings, model diagnostics, and scanner failures. A local dry-run of the four factory workflows accepted three genuine notices while retaining shellcheck and model validation.

### Alternatives Considered

#### Alternative 1: Keep experimental notices fatal and remove experimental usage

Workflows that need experimental features would have to wait until those features are promoted to stable, or be rewritten to avoid them. This preserves the strictest gate with no new surface area, but it blocks validation of the ESLint factory workflows that already ship in this repository and provides no path to prove those workflows compile cleanly. Rejected because it makes the gate unusable for an existing, supported class of workflows.

#### Alternative 2: Generic warning-suppression flag (e.g. `--ignore-warnings` or a per-code allowlist)

A general mechanism would cover experimental notices and other diagnostics with one flag, but accepting arbitrary warnings would weaken the gate's fail-closed contract. A per-code allowlist would also add configuration and review obligations beyond this request. Rejected in favor of a narrowly scoped flag whose accepted category is classified by the compiler.

#### Alternative 3: Frontmatter opt-in per workflow

Each workflow could declare that it accepts experimental features. This moves the acknowledgment into the artifact itself, but it makes the acknowledgment permanent and invisible at validation time, and a committed file cannot represent a human operator's explicit, per-run acceptance. Rejected because acknowledgment should be an operator action, not a workflow property.

### Consequences

#### Positive
- Workflows that legitimately use experimental features can reach a clean, auditable dry-run result without weakening any other check.
- Acceptance is explicit and quantified: the accepted count appears in the JSON report and the human summary message, so reviewers can see exactly how much was waived.
- The relaxation is bounded by a count comparison, so any additional non-experimental warning still fails the gate.

#### Negative
- Adds a user-visible CLI flag and a new field to the dry-run report, both of which are now part of the compatibility surface.
- Correctness depends on keeping ordinary and experimental counters synchronized and resetting both together. New diagnostic paths must preserve that invariant; regression tests cover it.
- Operators may habitually pass `--allow-experimental`, reducing pressure to promote or remove experimental features.

#### Neutral
- Documentation in `.github/aw/debug-agentic-workflow.md` and `docs/src/content/docs/reference/compilation-process.md` states that the flag conveys no live-execution or policy-installation authorization.
- The flag threads through `CompileConfig.AllowExperimental` and `enforceDevelopmentDiagnostics`, so any future dry-run diagnostic category must decide explicitly whether it is in scope.
- Shipped as a `minor` changeset alongside the factory admission and workflow-discovery fixes required by the same debugging task.

---

The decision remains proposed until maintainer acceptance.
