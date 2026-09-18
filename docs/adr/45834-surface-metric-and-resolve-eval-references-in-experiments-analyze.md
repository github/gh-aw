# ADR-45834: Surface Metric and Resolve Eval References in experiments analyze

**Date**: 2026-07-15
**Status**: Proposed
**Deciders**: PR author (@copilot), gh-aw maintainers

---

### Context

The `experiments analyze` command loads `ExperimentConfig` from workflow frontmatter, which includes a `metric` field that names the primary success metric for an experiment (e.g., `"aic"` or `"evals.builds"`). This field was parsed but never returned to callers, so users running `experiments analyze` had no way to see which metric an experiment was optimizing for. When the metric is an eval-backed reference (`evals.<id>` or `eval:<id>`), the reference also points to a declared eval question that carries human-readable context (e.g., `"Does the generated code compile?"`), which was likewise silently dropped. Surfacing this data requires threading `EvalsConfig` through the analysis pipeline alongside the existing `ExperimentConfig` map.

Two operational constraints shaped the implementation. First, the normal first-run experiment state contains only the single selected variant, so the analysis code must preserve config metadata even when it cannot yet compute statistics for two or more observed variants. Second, both local and remote workflow frontmatter loaders need to parse evals consistently so `experiments analyze --repo ...` does not lose metric-question context that is available for local workflows.

### Decision

We will introduce an `experimentFrontmatterResult` struct that bundles both `ExperimentConfigs` and `EvalsConfig` returned from frontmatter loaders, thread it through `computeExperimentAnalyses` and `computeExperimentAnalysis`, and resolve eval-backed metric references to their question text at analysis time. Config metadata extraction happens before the "fewer than two observed variants" early return so first-run output still includes the metric and resolved question. Remote frontmatter loading parses evals on the same best-effort basis as local loading, even if no experiment configs are ultimately returned from that file. The resolved `metric` and `metric_question` fields are added to `ExperimentAnalysis` and surfaced in both human-readable and JSON output. Eval resolution is best-effort: a missing `EvalsConfig`, an unrecognized eval ID, or a parse error leaves `MetricQuestion` empty without failing the command.

### Alternatives Considered

#### Alternative 1: Eager resolution at parse time

Resolve the eval question text immediately when parsing the frontmatter config (inside `loadLocalExperimentConfigs` / `loadRemoteExperimentConfigs`), storing the resolved string directly on `ExperimentConfig.Metric` or a new field. This would avoid threading `EvalsConfig` through the analysis pipeline. It was rejected because it conflates parsing with resolution and requires the loader to know about eval lookup semantics; the loader's responsibility is to extract structured data, not to resolve cross-references. Keeping `EvalsConfig` separate preserves the existing separation of concerns and makes it easier to test resolution logic independently.

#### Alternative 2: Display raw metric string only, no eval resolution

Add `Metric string` to `ExperimentAnalysis` and populate it from `cfg.Metric`, but skip resolving eval references to question text. Users would see `evals.builds` in the output without knowing what that question asks. This was rejected because the question text is the primary human-readable meaning of an eval-backed metric, and omitting it forces users to look up the question definition manually in the workflow file—exactly the information the output should provide.

### Consequences

#### Positive
- `experiments analyze` output now shows the experiment's primary metric, giving users immediate context about what the experiment is measuring.
- When the metric is an eval reference, the resolved question text (`"Does the generated code compile?"`) is displayed alongside the reference (`evals.builds`), making the output self-documenting without requiring users to consult the workflow file.
- Eval resolution is nil-safe and non-fatal; the command remains functional when evals are absent, the ID is unknown, or the evals block fails to parse.
- First-run analyses with a single observed variant still show the metric and resolved question, which gives users useful context before statistical analysis is possible.
- The `metric` and `metric_question` fields appear in JSON output (`--json`), enabling downstream tooling to consume them.

#### Negative
- `EvalsConfig` must be plumbed through three function call layers (`loadXxxExperimentConfigs` → `computeExperimentAnalyses` → `computeExperimentAnalysis`), increasing the parameter surface of internal functions and requiring all existing test call sites to be updated.
- Metadata extraction now runs even for degenerate single-variant states, so the analysis path does a small amount of extra work before returning early.
- Two new public symbols are exported from `pkg/workflow` (`ParseExperimentMetricEvalReference`, `ParseEvalsFromFrontmatter`), widening the package's API surface and creating a cross-package dependency on eval parsing from the CLI layer.

#### Neutral
- The `experimentFrontmatterResult` struct is an internal type (unexported) that bundles the two returned values; it replaces a `map[string]*workflow.ExperimentConfig` return type with a struct return type, which is a mechanical but pervasive change across the loader functions and their callers.
- Eval resolution performs a linear scan over `evals.Questions` for each experiment; for typical experiment counts this is negligible, but it is not indexed.

---

*ADR reviewed and completed by @copilot from the generated draft.*
