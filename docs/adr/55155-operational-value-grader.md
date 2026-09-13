# ADR-55155: Introduce a Dedicated Operational-Value Grader Type

**Date**: 2026-08-24
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

The existing built-in and inline graders measure execution quality from a run's trace, while operational value is domain-specific and may depend on repository evidence. Encoding every domain's evidence lifecycle, baseline, maturation, replay, and aggregation policy in gh-aw would make the generic grader protocol costly and difficult to use. Allowing arbitrary evaluator locations or mutable evaluator code would still weaken auditability.

### Decision

We will reserve the `operational-value` grader ID for a repository-relative Bash evaluator under `.github/graders/*.sh`. At compile time, gh-aw will reject traversal, symlinks, and non-regular files, validate Bash syntax, and freeze the evaluator bytes and SHA-256 digest into the compiled workflow.

At runtime, gh-aw will invoke the evaluator once with run, event, and config JSON on standard input. The evaluator returns a non-empty ordered array of unique domain-named `{id,value}` metrics. Values are finite numbers in `[0,1]` or `null`; the first metric is primary. Domain evidence decisions belong in the evaluator and its authoring skill. Enabling the grader does not modify the agent job's permissions; API access must be declared explicitly by the workflow.

The unified agent artifact will contain `grader_manifest.json`, `grader_results.json`, and the frozen evaluator for auditability. gh-aw preserves the metric array and exposes the first value through the existing scalar grader interface.

### Alternatives Considered

#### Alternative 1: Use Ordinary Inline Execution-Quality Graders

Represent operational value as another inline JavaScript grader over the preprocessed execution trace. This would reuse the existing isolated worker and artifact schema, but traces describe how the agent executed rather than whether repository-level outcomes were attained. Inline graders are also deliberately restricted from repository and API access needed by some operational metrics.

#### Alternative 2: Embed Evaluator Logic in Workflow Frontmatter or JavaScript

Place the complete evaluator directly in frontmatter or implement it as gh-aw-owned JavaScript. Frontmatter would make non-trivial evidence contracts difficult to review and maintain, while a built-in JavaScript implementation would couple repository-specific value definitions to gh-aw releases. A repository file keeps the evaluator versioned with the workflow while allowing the compiler to freeze and validate the exact bytes used.

#### Alternative 3: Compute Operational Value Only Asynchronously

Run value evaluation later in an external service or periodic process, outside the original run artifacts. This could wait for durable outcomes and avoid Bash execution in the agent job, but it would require a separate service, identity model, and storage contract. The in-run grader instead measures evidence available when the agent job finishes.

### Consequences

#### Positive
- Domain-specific metric names remain visible in artifacts instead of being collapsed into a generic score.
- The manifest, results, and frozen evaluator form a self-contained artifact set for auditing the original run.
- Missing evidence remains `null` rather than being coerced to zero.

#### Negative
- The evaluator can only measure evidence available before the grader step; durable outcomes applied by later jobs require another measurement system.
- The feature still requires Bash availability, syntax validation, process timeout, output validation, a curated environment, and temporary-file handling.
- Run artifacts contain trusted executable bytes and should be treated accordingly by artifact consumers.

#### Neutral
- Operational value is a deterministic measurement, not proof that the workflow caused the observed outcome.
- Evaluators use a curated runtime environment, including the workflow token and GitHub host variables, rather than inheriting the full workflow environment.
- Baselines, maturity, replay, and aggregation may be implemented externally when a specific domain needs them; they are not part of the core protocol.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
