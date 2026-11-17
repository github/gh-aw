# ADR-55155: Introduce a Dedicated Operational-Value Grader Type

**Date**: 2026-08-24
**Status**: Draft
**Deciders**: gh-aw maintainers

> **Superseded (2026-11-16):** Operational value is now JavaScript-only through
> `graders.operational-value.script`; the Bash design below is historical.

---

### Context

The existing built-in and inline graders measure execution quality from a run's trace, but operational value depends on repository evidence, maturation windows, and a stable baseline contract. That evidence must remain attributable to the workflow run and evaluator version while supporting deterministic replay after the original run artifact is sealed. Allowing arbitrary evaluator locations or mutable evaluator code would weaken provenance and make historical results difficult to reproduce. Authoritative workflow-run creation time is useful for assigning time-based opportunities, but fetching it must not broaden the token available to the agent.

### Decision

We will reserve the `operational-value` grader ID for a repository-relative Bash evaluator under `.github/graders/*.sh`. At compile time, gh-aw will reject traversal, symlinks, and non-regular files, validate Bash syntax, and freeze the evaluator bytes and SHA-256 digest into the compiled workflow. At runtime, gh-aw will execute deterministic `--definition` and `--grade-run` modes in the gh-aw temporary area with a curated environment, producing absolute attainment in `[0,1]` or `null` plus evidence provenance, maturity, a frozen baseline, and an optional derived delta. The activation job will fetch authoritative workflow-run creation time with its compiler-owned `actions: read` permission and pass it to the agent job as non-secret metadata. Enabling the grader will not modify the agent job's permissions; evaluator evidence access must be declared explicitly by the workflow.

The unified agent artifact will contain `grader_manifest.json`, `grader_results.json`, and the frozen evaluator for replay. Historical regrading will execute only after verifying the archived bytes against the manifest/result digests and the evaluator at the recorded commit in a trusted checkout, while preserving the original run identity, attempt, subject, and operational case.

### Alternatives Considered

#### Alternative 1: Use Ordinary Inline Execution-Quality Graders

Represent operational value as another inline JavaScript grader over the preprocessed execution trace. This would reuse the existing isolated worker and artifact schema, but traces describe how the agent executed rather than whether repository-level outcomes were attained. Inline graders also lack the evidence cutoff, maturity, baseline, provenance, and trusted-checkout replay contract required for operational value.

#### Alternative 2: Embed Evaluator Logic in Workflow Frontmatter or JavaScript

Place the complete evaluator directly in frontmatter or implement it as gh-aw-owned JavaScript. Frontmatter would make non-trivial evidence contracts difficult to review and maintain, while a built-in JavaScript implementation would couple repository-specific value definitions to gh-aw releases. A repository file keeps the evaluator versioned with the workflow while allowing the compiler to freeze and validate the exact bytes used.

#### Alternative 3: Compute Operational Value Only Asynchronously

Run value evaluation later in an external service or periodic process, outside the original run artifacts. This could wait naturally for mature evidence and avoid adding Bash execution to the agent job, but it would separate observations from their original run identity and frozen evaluator unless a parallel provenance system were built. It would also make immediate attainment unavailable and weaken self-contained replay from the run artifact.

### Consequences

#### Positive
- Each observation is tied to a run, operational case, evidence cutoff, provenance, and evaluator digest, enabling reproducible historical regrading.
- The manifest, results, and frozen evaluator form a self-contained artifact set for audit and replay without mutating the original run.
- Baseline-comparable and attainment-only definitions share one normalized result contract while preserving `null` for unavailable or immature evidence.

#### Negative
- The activation job requires `actions: read` to obtain authoritative run creation time; a failed lookup leaves that optional subject field empty.
- The feature adds Bash availability, syntax validation, process timeout, output validation, curated-environment, and temporary-file complexity to compiler and runtime maintenance.
- Run artifacts contain trusted executable bytes; consumers must continue verifying digests and checkout provenance before executing them.

#### Neutral
- Operational value is absolute attainment, not proof that the workflow caused the observed outcome; the optional baseline delta remains descriptive rather than causal.
- Evaluators may access only the curated runtime environment, including the workflow token and GitHub host variables, and execute from the gh-aw temporary area rather than inheriting the full workflow environment.
- Historical regrading creates a new observation keyed by run ID, evaluator digest, and evidence time while preserving the original run artifact and case.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
