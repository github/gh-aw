# ADR-72407: Merge Imported Job Outputs by Name with Main Workflow Precedence

**Date**: 2026-10-27
**Status**: Proposed (pending PR acceptance)
**Deciders**: @pelikhan (main-workflow precedence explicitly requested)

---

### Context

Shared agentic workflows can be imported into a main workflow, and imports may contribute `jobs:` entries. Before this change, when an import and the main workflow (or two imports) defined a job with the same ID, only injected step fields (`setup-steps`, `pre-steps`, and steps) were merged; any `jobs.<job-id>.outputs` contributed by the import were silently dropped because the main/earlier job definition won outright. This prevented shared workflows from exposing job outputs (for example on custom jobs or the built-in `pre_activation` job) alongside outputs the main workflow already declares. The import system already establishes the convention that the main workflow takes precedence and that imports are processed in a deterministic discovery order.

### Decision

Merge `jobs.<job-id>.outputs` as a union keyed by output name whenever imports and/or the main workflow define the same job. Main workflow values always win on conflicting names (including explicit empty strings); among imports, later-discovered values win. Absent or null output collections contribute no entries, as do empty maps; an explicit empty string for an individual output remains an override.

Output-only contributions (job mappings whose sole field is `outputs`) are merged without replacing the existing job definition, including its injected steps. Main workflow job fields retain precedence. Among imports, the existing step-merging rules select the later job body when an injected-step merge applies; otherwise the earlier job body is retained. Outputs do not change this selection. Other scalar and map fields come from the selected body, rather than being deep-merged; regular steps from conflicting custom jobs are not concatenated. The merge allocates new maps so input job configurations are never mutated.

After output precedence is applied, compilation rejects output expressions that reference regular step IDs contributed by a job definition but discarded during body selection. Both dot and literal bracket references are checked. References satisfied by retained regular, setup, or pre-steps are allowed, and a main-workflow output override can replace an unsafe imported expression. This check is limited to discarded step IDs, not general validation of unknown step references.

The output-map merge helper does not merge outputs when either collection is a non-null, non-map value; existing job and step precedence remains in effect. Malformed output types do not introduce new merge-time errors. The [frontmatter schema](../../pkg/parser/schemas/main_workflow_schema.json) requires job outputs to be objects with string values, and [shared Markdown validation](../../pkg/parser/schema_validation.go) uses the same schema. This means a literal null in Markdown frontmatter is still invalid even though the merge helper tolerates nil values. [YAML workflow imports](../../pkg/parser/yaml_import.go) validate the workflow's shape, not individual output types. Invalid values that survive merging into generated YAML are subject to the compiler's GitHub Actions schema validation when enabled; discarded imported values are not necessarily diagnosed.

### Alternatives Considered

#### Alternative 1: Keep "main job wins outright" (status quo)

Continue dropping imported outputs for same-named jobs and require authors to redeclare every output in the main workflow. This preserves existing behavior but prevents shared jobs from contributing outputs independently and duplicates declarations in consumers.

#### Alternative 2: Fail compilation on conflicting output names

Union outputs but treat any duplicate output name across imports/main as a compile error, mirroring how duplicate `env:` keys across imports fail. This is stricter and avoids hidden overrides, but it prevents the main workflow from intentionally overriding an imported output and is inconsistent with the main-wins semantics already used for jobs.

#### Alternative 3: Deep-merge the entire job definition

Recursively merge all job fields (outputs, env, permissions, steps, etc.). This would be more general but greatly expands the semantic surface, makes precedence rules hard to reason about, and risks unintended changes to existing compiled workflows.

### Consequences

#### Positive
- Shared workflows can expose job outputs that are combined with those defined by the main workflow, including on built-in jobs such as `pre_activation`.
- Main-workflow precedence preserves intentional consumer overrides; deterministic discovery order makes duplicate imported output names reproducible.
- Input job maps are not mutated, keeping merging side-effect free and testable.
- Output merging cannot silently introduce references to regular steps discarded by job-body precedence.

#### Negative
- Imported outputs can be silently overridden by the main workflow or a later import with no warning, which may hide configuration mistakes.
- `jobs:` merging now has field-specific rules (steps vs. outputs vs. everything else), increasing the cognitive load for authors and maintainers.
- The output-map merge helper does not validate output types. Markdown schema validation rejects malformed output collections, but invalid values from YAML imports can be discarded by existing conflict precedence without a diagnostic.
- Conflicting job bodies can now fail compilation when a final output expression references a discarded regular step. Authors must override that output or retain its producing step.

#### Neutral
- The [imports reference](../src/content/docs/reference/imports.md) describes the field-specific merge behavior.
- Workflows without overlapping job output definitions retain their existing merge behavior. Workflows with overlapping definitions can now expose previously discarded imported outputs.

---

*This record documents the proposed implementation and does not imply reviewer approval.*
