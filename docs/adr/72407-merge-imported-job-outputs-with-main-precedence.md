# ADR-72407: Merge Imported Job Outputs by Name with Main Workflow Precedence

**Date**: 2026-07-03
**Status**: Draft
**Deciders**: pelikhan [TODO: verify additional reviewers]

---

### Context

Shared agentic workflows can be imported into a main workflow, and imports may contribute `jobs:` entries. Before this change, when an import and the main workflow (or two imports) defined a job with the same ID, only injected step fields (`setup-steps`, `pre-steps`, and steps) were merged; any `jobs.<job-id>.outputs` contributed by the import were silently dropped because the main/earlier job definition won outright. This prevented shared workflows from exposing job outputs (for example on custom jobs or the built-in `pre_activation` job) alongside outputs the main workflow already declares. The import system already establishes the convention that the main workflow takes precedence and that imports are processed in a deterministic discovery order.

### Decision

We will merge `jobs.<job-id>.outputs` as a union keyed by output name whenever imports and/or the main workflow define the same job. Main workflow values always win on conflicting names (including explicit empty strings); among imports, later-discovered values win. Output-only contributions are merged without replacing the existing job definition, and all other job fields keep their existing precedence and step-merging rules (regular steps from conflicting custom jobs are still not concatenated). The merge allocates new maps so input job configurations are never mutated.

### Alternatives Considered

#### Alternative 1: Keep "main job wins outright" (status quo)

Continue dropping imported outputs for same-named jobs and require authors to redeclare every output in the main workflow. This is the simplest and most predictable rule, but it defeats the purpose of shared workflows that want to publish outputs, and silently discarding configuration is surprising to authors.

#### Alternative 2: Fail compilation on conflicting output names

Union outputs but treat any duplicate output name across imports/main as a compile error, mirroring how duplicate `env:` keys across imports fail. This is stricter and avoids hidden overrides, but it prevents the main workflow from intentionally overriding an imported output and is inconsistent with the main-wins semantics already used for jobs.

#### Alternative 3: Deep-merge the entire job definition

Recursively merge all job fields (outputs, env, permissions, steps, etc.). This would be more general but greatly expands the semantic surface, makes precedence rules hard to reason about, and risks unintended changes to existing compiled workflows.

### Consequences

#### Positive
- Shared workflows can expose job outputs that are combined with those defined by the main workflow, including on built-in jobs such as `pre_activation`.
- Precedence is consistent with existing import rules: main workflow wins, imports are resolved in deterministic discovery order.
- Input job maps are not mutated, keeping merging side-effect free and testable.

#### Negative
- Imported outputs can be silently overridden by the main workflow or a later import with no warning, which may hide configuration mistakes.
- `jobs:` merging now has field-specific rules (steps vs. outputs vs. everything else), increasing the cognitive load for authors and maintainers.
- Malformed (non-map) `outputs` values are skipped rather than reported, so errors may surface only at runtime. [TODO: verify whether validation catches this elsewhere]

#### Neutral
- Documentation in `reference/imports.md` changes from "jobs: Not merged" to describing the new merge behavior.
- Existing compiled workflows are unaffected (PR reports no lock-file drift across 349 workflows).

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
