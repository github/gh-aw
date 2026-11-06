# ADR-74939: Allow Status-Only Reporters to Read Proven Pre-Agent Outputs

**Date**: 2026-11-04
**Status**: Draft
**Deciders**: gh-aw maintainers (review pending)

---

### Context

Status-only custom jobs run after the agent and detection jobs so they can report failures even when detection fails. The existing contract allowed those jobs to read outputs only from detection-gated compiler-owned jobs, which prevented a leaf reporter from consuming metadata such as a pull request number or head SHA produced by an authored job before the agent. The issue and regression fixture show that this restriction blocks failure reporting even when the producer has no dependency path through `agent` or `detection`. The implementation must preserve the post-agent data boundary while keeping compiler and linter decisions consistent.

### Decision

We will allow a status-only reporter to read outputs from an authored custom job when the dependency graph proves that the producer is an ancestor of `agent` and has no direct or transitive dependency on `agent` or `detection`. Compiler-owned jobs remain governed by the existing detection-gated allowance, and generated jobs without authored provenance remain excluded. The compiler will use the lowered dependency graph when available, while AW-only linting will infer the compiler's pre-agent ordering; existing leaf, artifact, file, and agent/safe-output restrictions remain in force.

### Alternatives Considered

#### Alternative 1: Keep access limited to detection-gated compiler-owned outputs

This preserves the smallest existing allowlist and avoids additional graph analysis. It was rejected because a status-only reporter could not read valid pre-agent metadata, preventing it from identifying the pull request or revision when detection fails.

#### Alternative 2: Allow outputs from any custom job that does not explicitly depend on `agent` or `detection`

This would be simple for workflow authors and would cover more producers. It was rejected because parallel jobs are not proven to run before `agent`, and generated jobs do not necessarily have authored provenance; admitting them would weaken the post-agent boundary.

#### Alternative 3: Remove status-only output access and require a normal detection-gated reporter

This would avoid the new allowlist entirely. It was rejected because normal detection gating intentionally suppresses the reporter when detection fails, which is the situation the status-only reporter is needed to handle.

### Consequences

#### Positive

- Failure reporters can consume validated pre-agent metadata such as a pull request number and head SHA even when detection fails.
- A shared graph predicate and matching compiler/linter rules preserve the existing boundary against agent- or detection-dependent data.
- Workflow authors do not need a new frontmatter option or runtime mechanism; they can declare the metadata producer in `needs` and pass its outputs through `env`.

#### Negative

- Compiler and linter logic must reason about authored provenance, transitive dependency paths, and both lowered and AW-only graphs, increasing implementation and test complexity.
- Workflows must structure metadata producers as proven ancestors of `agent` and explicitly list them in the reporter's `needs`; parallel or generated producers remain unavailable.
- The compiler and linter must continue to evolve together so their graph interpretations do not drift.

#### Neutral

- The `detection-gate: status-only` key remains compiler-only and is still removed from emitted Actions workflows.
- Existing restrictions on leaf jobs, artifact downloads, opaque reusable workflows, agent/safe-output files, and detection-gated compiler-owned outputs are unchanged.
- The behavior is documented and covered by regressions for transitive metadata, ordering, downstream dependencies, parallel jobs, and generated producers.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
