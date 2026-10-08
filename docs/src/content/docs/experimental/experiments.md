---
title: A/B Experiments
description: Run A/B experiments in GitHub Agentic Workflows to test prompt variants and measure the effect of different instructions across runs.
sidebar:
  order: 7
---

:::caution[Experimental]
A/B Experiments is an experimental feature.
:::

Use the `experiments` frontmatter section to compare workflow variants across
repeated runs. Each experiment declares a name and a set of variants. On every
run, the activation job picks one variant and exposes it to the prompt.

Experiments work best when you test one workflow choice at a time, such as:

- prompt wording
- model selection
- whether to delegate to a sub-agent
- which subskill (inline skill) to invoke

## Declaring experiments

Add an `experiments` map to the workflow frontmatter. Each key names an
experiment. The value is either a simple array of variants (bare-array form) or
a rich object with additional metadata fields.

### Bare-array form

```aw wrap
---
on:
  issues:
    types: [opened]

engine: copilot

experiments:
  style: [concise, detailed]
---

Summarize this issue in a **${{ experiments.style }}** way.
```

### Rich object form

Use the object form when you want built-in reporting and experiment metadata:

```aw wrap
---
on:
  schedule: daily on weekdays

engine: copilot

evals:
  - id: focused
    question: Is the change focused and actionable?

experiments:
  prompt_style:
    variants: [concise, detailed]
    description: "Test whether a concise prompt reduces cost without quality loss"
    hypothesis: "H0: no change in aic. H1: concise reduces AIC by >=15%"
    metric: eval:focused
    secondary_metrics: [duration_ms, discussion_word_count]
    guardrail_metrics:
      - name: success_rate
        threshold: ">=0.95"
      - name: empty_output_rate
        direction: min
        threshold: 0.0
    weight: [50, 50]
    min_samples: 25
    start_date: "2026-05-05"
    end_date: "2026-07-25"
    issue: 1234
---

Summarize the findings in a **${{ experiments.prompt_style }}** way.
```

When `evals` are configured, `metric` can reference an eval question ID using
`eval:<id>` (for example `eval:focused`) or `evals.<id>`.

When `graders` are configured, `metric` can reference a grader result using
`grader:<id>` (for example `grader:tool-success-rate`) or
`graders.<id>.value`.

`gh aw experiments analyze <workflow>` resolves the referenced eval question and, when
eval result data is available, shows YES/NO/UNKNOWN totals for that eval-backed metric.
It also joins per-run eval answers to persisted experiment assignments and analyzes the
valid YES/NO observations by variant using the same statistical comparisons available
for grader-backed metrics. For a grader-backed primary metric, it joins persisted
experiment assignments to completed run artifacts and analyzes the valid grader values
by variant.

> [!NOTE]
> Experiment names must be valid identifiers: start with a letter or
> underscore, followed by letters, digits, or underscores. For example, use
> `style` or `feature_1`. Names that do not match this pattern are ignored.

## Using variants in the prompt

Reference a variant with `${{ experiments.<name> }}`. At runtime, gh-aw
replaces the expression with the selected variant string, such as `concise`.

Use the `{{#if experiments.<name> }}` block syntax for conditional prompt
sections. A variant value of `no` is treated as falsy, which makes yes/no
experiments easy to express:

```aw wrap
---
experiments:
  caveman: [yes, no]
---

{{#if experiments.caveman }}
Talk like a caveman in all your responses. Me test. You run.
{{/if}}

Address the issue described above.
```

## Common experiment ideas

Most experiments compare a single decision in the workflow. The examples below
show common patterns.

### Try different prompt styles

```aw wrap
---
experiments:
  style: [concise, detailed]
---

Summarize this issue in a **${{ experiments.style }}** way.
```

### Try different models

Model experiments are useful when you want to compare speed, cost, and output
quality. gh-aw model aliases such as `small` and `large` are often a good place
to start. See [Model Aliases](/gh-aw/reference/model-tables/).

```aw wrap
---
engine:
  id: copilot
  model: ${{ experiments.model }}

experiments:
  model: [small, large]
---

Review the issue and recommend the next action.
```

### Try using a sub-agent

This pattern compares a direct prompt with a delegated sub-agent flow.

```aw wrap
---
experiments:
  use_summarizer: [yes, no]
---

{{#if experiments.use_summarizer }}
Use the `file-summarizer` sub-agent to summarize `README.md`, then continue.
{{/if}}

Write a short project overview for maintainers.

## agent: `file-summarizer`
---
model: small
description: Summarizes a file in a few sentences
---
Read the given file and return a concise summary.
```

See [Inline Sub-Agents](/gh-aw/reference/inline-sub-agents/) for the full
syntax.

With Copilot, you can also declare `subagent_model: [small, large]` in
`experiments` and set the sub-agent's `model` to
`${{ experiments.subagent_model }}`. The selected variant is used in the
sub-agent definition and recorded in the run's declared-model metadata.
Audit patterns and model-routing request policy expand every declared variant
through the workflow's model aliases, while `models.allowed` and `models.blocked`
still constrain admission. Router candidates remain unchanged. These patterns
describe possible models; matching observed usage does not prove that the selected
variant was honored.

Use a direct `${{ experiments.subagent_model }}` reference for sub-agent models.
Compound expressions such as `${{ experiments.subagent_model || 'small' }}` or
`format(...)` are rejected because the sub-agent runtime does not evaluate them.
Put the complete model or alias in each experiment variant instead.
Pi sub-agents require literal models or aliases, not expressions.

### Try different subskills

This pattern compares two reusable instruction blocks, sometimes called
subskills, without changing the main workflow prompt.

```aw wrap
---
experiments:
  triage_skill: [triage-fast, triage-deep]
---

Use the `${{ experiments.triage_skill }}` skill to classify this issue.

## skill: `triage-fast`
---
description: Fast issue triage
---
Classify the issue and suggest the smallest next step.

## skill: `triage-deep`
---
description: Detailed issue triage
---
Classify the issue, identify missing context, and recommend a fuller follow-up
plan.
```

## Statistical balancing

The activation job tracks how often each variant has been selected. The counter
is stored using the `storage` setting in the `experiments:` block. By default,
gh-aw chooses the least-used variant on each run. If multiple variants are tied,
including on the first run, one of them is chosen at random. Over time, this
keeps usage roughly balanced across variants.

When you provide a `weight` array, gh-aw uses weighted random selection instead
of least-used selection. For example, `[70, 30]` gives the first variant a 70%
selection probability. If `start_date` or `end_date` is set and the current
date falls outside that range, gh-aw returns the control variant (the first
entry) without incrementing any counter.

## Storage Configuration

The `storage` key inside the `experiments:` map controls where experiment state
is persisted:

```yaml
experiments:
  storage: repo   # or: cache (default: repo)
  prompt_style: [concise, detailed]
```

| Value | Behavior |
|---|---|
| `repo` (**default**) | Commits state to a git branch named `experiments/{sanitizedWorkflowID}` (workflow ID lowercased with hyphens removed, e.g. `my-workflow` → `experiments/myworkflow`). Durable — survives cache evictions. Requires `contents: write` permission (added automatically by the compiler). |
| `cache` | Uses GitHub Actions cache (legacy). State may be evicted after 7 days of inactivity. |

When `storage: repo`, the compiler adds a `push_experiments_state` job after the
activation job and commits the updated `state.json` to the experiments branch.

## Accessing assignments downstream

Each experiment exposes its selected variant as an activation job output:

| Expression | Description |
|---|---|
| `needs.activation.outputs.<name>` | Selected variant for experiment `<name>` |
| `needs.activation.outputs.experiments` | All assignments as a JSON object |

Use these expressions in downstream jobs defined in the `jobs:` frontmatter section.

## Analyzing results

The activation job uploads the counter state as an `experiment` artifact. Download and inspect it with the `gh aw` CLI:

```bash
# Download the experiment artifact for a specific run
gh aw audit <run-id> --artifacts experiment

# Display experiment assignments in the audit report
gh aw audit <run-id>
```

The `🧪 A/B Experiments` section of the audit report shows the variant chosen on the most recent run and the cumulative counts across all runs:

```
🧪 A/B Experiments
  • caveman = yes (cumulative: no:4, yes:5)
  • style = concise (cumulative: concise:5, detailed:4)
```

### Analyze a grader-backed metric

Use a deterministic grader as the primary outcome metric:

```aw wrap
---
graders:
  trajectory-efficiency:
    direction: higher_is_better
  tool-failure-count:
    direction: lower_is_better

experiments:
  prompt_v2:
    variants: [control, candidate]
    metric: grader:trajectory-efficiency
    guardrail_metrics:
      - name: grader:tool-failure-count
        threshold: "<=0"
    min_samples: 20
    analysis_type: mann_whitney
    decision:
      minimum_effect: 0.05
      confidence: 0.95
---

Follow the **${{ experiments.prompt_v2 }}** instructions.
```

After assigned runs complete, analyze their grader artifacts:

```bash
gh aw experiments analyze <workflow>
```

The assignment ledger remains the source of variant attribution. The command downloads each
assigned run's unified `agent` artifact on demand, reads `grader_results.json`, and reports
assigned, usable, and excluded observations per variant. Missing artifacts, missing graders,
failed graders, and invalid values are excluded rather than counted as zero. Only usable
observations count toward `min_samples`.

This analysis does not replay traces or invoke an evaluator model. A deterministic grader
measures only the behavior encoded by that grader and is not a universal correctness metric.
The decision layer treats grader observations like any other resolved numeric metric.

### Analyze an eval-backed metric

Use a BinEval question as the primary outcome metric:

```aw wrap
---
evals:
  questions:
    - id: focused
      question: Does the response stay on topic?

experiments:
  prompt_v2:
    variants: [control, candidate]
    metric: eval:focused
    min_samples: 20
---

Follow the **${{ experiments.prompt_v2 }}** instructions.
```

`gh aw experiments analyze <workflow>` joins each assigned run's recorded eval answer
(from `evals.jsonl`) to the assignment ledger by run ID and reports assigned, usable, and
excluded YES/NO observations per variant. Runs missing an eval answer, or with an
`UNKNOWN`/unrecognized answer, are excluded rather than counted as zero. Only usable
observations count toward `min_samples`, and the same t-test, Mann–Whitney, proportion, or
Bayesian comparisons available for grader-backed metrics are applied to the YES/NO outcomes.
`UNKNOWN` answers remain excluded. They are not treated as `NO`.

### Deterministic decisions

`gh aw experiments analyze <workflow>` keeps each experiment stage distinct:

| Stage | Question answered |
|---|---|
| Assignment | Which variant ran? |
| Observation | What happened during the assigned run? |
| Analysis | What treatment effect and evidence were estimated? |
| Readiness | Are there enough usable observations for normal analysis? |
| Decision | Should collection extend, or should the candidate be promoted, rejected, or remain inconclusive? |
| Promotion | What future automation changes the workflow or traffic? |

The command performs the stages through decision. It does not promote a variant, edit the workflow,
or change traffic. `readiness` is `COLLECTING` or `READY`; the legacy `recommendation` field remains
`EXTEND` or `READY_FOR_ANALYSIS` for compatibility. A ready experiment can still have an
`INCONCLUSIVE` decision.

The complete flow is:

```text
variant assignment
→ run observation (for example, a grader value)
→ statistical analysis
→ readiness
→ deterministic decision
```

Assignment identifies the treatment (`control` or `candidate`); it is not an outcome. Observation
records what happened during that assigned run. Analysis estimates the effect and evidence.
Readiness only answers whether every variant has reached `min_samples`: usable primary-metric
observations for observation-backed metrics, or assignment counts when no supported observation
source is resolved. The decision then applies policy and mandatory guardrails to the existing
analysis.

For two-variant experiments, the command emits one of these decisions:

| Decision | Meaning |
|---|---|
| `EXTEND` | More valid primary or guardrail observations are required, or analysis is not yet computable. |
| `PROMOTE` | The candidate has sufficient statistical evidence, exceeds the practical-effect threshold, and passes all guardrails. |
| `REJECT` | The candidate materially regresses or fails a mandatory guardrail. |
| `INCONCLUSIVE` | Minimum samples exist, but the evidence or practical effect does not establish a winner. |

In particular, `EXTEND` means that evidence collection or computation is incomplete.
`INCONCLUSIVE` means that the engine could adjudicate the available evidence, but it did not
establish a decision-quality improvement or regression. A candidate that fails to prove an
improvement is not automatically harmful; `REJECT` requires a failed guardrail or sufficient
evidence of a material regression.

The `decision` configuration uses absolute primary-metric units. `minimum_effect` defaults to
`0`. `regression_tolerance` defaults to `minimum_effect`. `confidence` defaults to `0.95`.
Frequentist methods require `p <= 1-confidence`; `bayesian_ab` uses the analyzer's probability
of superiority directly. Statistical significance alone does not override `minimum_effect`.

Metric direction is normalized so a positive effect is always better for the candidate.
Grader direction supplies this metadata, while eval metrics default to higher-is-better.
Native metric names without resolved per-run observations return `EXTEND` rather than guessing.
Automatic decisions for experiments with more than two variants return `INCONCLUSIVE`.

For example, with `direction: max`, a grader increase from `0.80` to `0.84` has an absolute
effect of `+0.04`. If `minimum_effect` is `0.05`, even a statistically significant result is
`INCONCLUSIVE` with `effect_below_threshold`. With `direction: min`, a duration decrease from
10 seconds to 8 seconds has a raw absolute effect of `-2`, but a normalized effect of `+2`;
users do not reverse lower-is-better metrics themselves.

Mandatory guardrails take precedence over the primary metric. A primary-metric improvement cannot
override a failed guardrail:

```text
Primary metric: +12%
Tool-failure guardrail: FAIL
Decision: REJECT (guardrail_failed)
```

Missing or undersampled guardrail observations do not pass. They produce `EXTEND` with
`insufficient_observations`; an unsupported guardrail metric produces `EXTEND` with
`guardrail_unsupported`.

#### Human-readable output

The decision portion of the default CLI output uses these fields:

```text
  Readiness  : READY

  READY FOR ANALYSIS — all 2 variants have reached min_samples (20); outcome metric analysis is available

  Decision   : PROMOTE candidate
  Reason     : candidate materially improves the primary metric with sufficient evidence and all guardrails pass (candidate_improved)
```

An adequately sampled result without sufficient evidence remains distinct:

```text
  Readiness  : READY

  READY FOR ANALYSIS — all 2 variants have reached min_samples (20); outcome metric analysis is available

  Decision   : INCONCLUSIVE
  Reason     : minimum samples are available but the configured evidence threshold is not satisfied (evidence_insufficient)
```

While observations are still accumulating, output instead includes:

```text
  Readiness  : COLLECTING

  EXTEND — 1 of 2 variant(s) below min_samples threshold (min observed: 19 / 20)

  Decision   : EXTEND
  Reason     : one or more variants have fewer usable observations than min_samples (insufficient_samples)
```

#### Reason codes

`reason_code` is the stable machine-readable explanation for the core decision:

| Reason code | Interpretation |
|---|---|
| `insufficient_samples` | At least one variant has fewer than `min_samples` usable observations. |
| `insufficient_observations` | A required comparison or mandatory guardrail lacks usable observations. |
| `candidate_improved` | The candidate has sufficient evidence of an improvement meeting `minimum_effect`. |
| `candidate_regressed` | The candidate has sufficient evidence of a regression exceeding `regression_tolerance`. |
| `guardrail_failed` | A mandatory guardrail failed; this overrides a primary-metric win. |
| `guardrail_unsupported` | A mandatory guardrail is not backed by a supported observation source. |
| `effect_below_threshold` | Evidence is sufficient, but the practical effect does not meet the configured threshold. |
| `evidence_insufficient` | Samples are ready, but the configured frequentist or Bayesian evidence threshold is not met. |
| `unsupported_multi_variant` | Automatic decisions currently require exactly two variants. |
| `analysis_unavailable` | The configured analysis cannot produce the required effect or evidence. |

`interaction_underpowered` is not a core reason code. The daily experiment report uses it only for
a presentation-level safety hold when simultaneous-experiment cells are sparse. It preserves the
core decision and reports `report_action: EXTEND`.

Use `--json` for the stable automation boundary:

```bash
gh aw experiments analyze <workflow> --json
```

Each entry in `analyses` includes `decision`, `reason_code`, `samples`, `decision_guardrails`, and
`decision_policy`, alongside the separate `readiness` field. `control`, `candidate`, `effect`, and
`evidence` are emitted when those values are available for the decision path (for example
two-variant statistical comparisons); early `EXTEND` and multi-variant `INCONCLUSIVE` results may
omit them. Future promotion automation can consume these fields without rerunning statistics or
interpreting grader artifacts.

A ready two-variant result can contain:

```json
{
  "decision": "PROMOTE",
  "reason_code": "candidate_improved",
  "reason": "candidate materially improves the primary metric with sufficient evidence and all guardrails pass",
  "control": "control",
  "candidate": "candidate",
  "direction": "max",
  "samples": {
    "control": 20,
    "candidate": 20
  },
  "effect": {
    "absolute": 0.08,
    "relative": 0.1,
    "normalized_absolute": 0.08
  },
  "evidence": {
    "analysis_type": "mann_whitney",
    "significant": true,
    "p_value": 0.01
  },
  "decision_guardrails": {
    "configured": true,
    "passed": true
  },
  "decision_policy": {
    "minimum_effect": 0.05,
    "regression_tolerance": 0.05,
    "confidence": 0.95
  },
  "experiment_name": "prompt_v2",
  "analysis_type": "mann_whitney",
  "metric": "grader:trajectory-efficiency",
  "min_samples": 20,
  "readiness": "READY",
  "recommendation": "READY_FOR_ANALYSIS"
}
```

The stable automation boundary is `analyses[].readiness`, `analyses[].decision`, and
`analyses[].reason_code`. Reporting workflows may add a presentation or interaction-safety hold,
but must preserve the core decision rather than recomputing it.

The core CLI supports all four analysis methods (`t_test`, `mann_whitney`, `proportion_test`, and
`bayesian_ab`). Frequentist methods provide p-value evidence. Bayesian analysis provides
`probability_superiority`; it is significant when that probability is at least `confidence` or at
most `1-confidence`, and it is never described as a p-value.

The daily experiment report consumes this structured core decision. It does not independently
recompute the decision policy. Its only additional outcome handling is the interaction safety hold
described above. Older reporting terminology such as `ABANDON`, and guardrail statuses such as
`GUARDRAIL_FAILED`, are not core decision values.

Existing experiment configurations remain compatible. Analysis still defaults to 20 samples per
variant and the configured or inferred statistical method. A deterministic `decision` is emitted
even when no `decision:` block exists, using `minimum_effect: 0`, `regression_tolerance: 0`, and
`confidence: 0.95`. The legacy `recommendation` remains in JSON. No migration is required.

Decisions are only as meaningful as their configured observations. A grader measures the behavior
it encodes, not universal task correctness. Low-frequency or heterogeneous tasks may need larger
samples, simultaneous experiments can complicate attribution, and no observed regression proves
only the configured evidence—not universal non-regression.

### Filtering audit results by variant

Use `--experiment` and `--variant` to filter audit runs to a specific variant:

```bash
gh aw audit <run-id> --experiment prompt_style --variant concise
```

### Step summary

Each activation job writes a Markdown step summary that shows the selected
variants, cumulative counts, and, when you use the object form, progress toward
`min_samples`:

```
## 🧪 A/B Experiment Assignments

| Experiment   | Selected Variant | All Variants      | Cumulative Counts      |
| ---          | ---              | ---               | ---                    |
| prompt_style | concise          | concise, detailed | concise: 8, detailed: 7|

### 📊 Sampling Progress

prompt_style (target: 25 per variant)
  concise: ████████░░░░░░░░░░░░ 8/25 (32%)
  detailed: ███████░░░░░░░░░░░░░ 7/25 (28%)

### Experiment Details

**prompt_style**

> Test whether a concise prompt reduces cost without quality loss

**Hypothesis:** H0: no change in aic. H1: concise reduces AIC by >=15%

**Guardrail metrics:**
- `success_rate` >=0.95
- `empty_output_rate` ==0

Tracking issue: [#1234](https://github.com/owner/repo/issues/1234)
```

## Frontmatter reference

### Bare-array form

| Field | Type | Description |
|---|---|---|
| `experiments` | `object` | Map of experiment name → variant array or config object |
| `experiments.<name>` | `string[]` | Array of two or more variant strings for one experiment |

### Object form fields

| Field | Type | Required | Description |
|---|---|---|---|
| `variants` | `string[]` | ✅ | Array of two or more variant strings |
| `description` | `string` | | Human-readable explanation of what the experiment tests |
| `hypothesis` | `string` | | Null and alternative hypothesis (e.g. `"H0: no change. H1: concise reduces AIC by >=15%"`) |
| `metric` | `string` | | Primary metric to observe (e.g. `aic`, `duration_ms`) |
| `secondary_metrics` | `string[]` | | Additional metrics to track alongside the primary metric |
| `guardrail_metrics` | `object[]` | | List of guardrail objects with `name` (string), `threshold` (comparison string like `>=0.95` or bare number like `0.0`), and optional `direction` (`"min"` or `"max"`). When `threshold` is a bare number, `direction` governs the pass condition (≤ for `min`, ≥ for `max`). See [experiments-specification §4.4](/gh-aw/experimental/experiments-specification/#44-guardrail-metrics) for full semantics. |
| `min_samples` | `integer` | | Minimum runs per variant required before statistical analysis is considered reliable. The step summary shows a progress bar toward this target. |
| `analysis_type` | `string` | | Statistical method: `t_test`, `mann_whitney`, `proportion_test`, or `bayesian_ab`. |
| `decision` | `object` | | Deterministic policy with optional non-negative `minimum_effect`, non-negative `regression_tolerance`, and `confidence` between 0 and 1. Effects use absolute primary-metric units. |
| `weight` | `integer[]` | | Per-variant probability weights (same length as `variants`). Enables weighted-random selection; values are relative and need not sum to 100. |
| `issue` | `integer` | | GitHub issue number that tracks this experiment's lifecycle |
| `start_date` | `string` | | ISO-8601 date (`YYYY-MM-DD`) before which the experiment is inactive. The control variant is returned before this date without incrementing any counter. |
| `end_date` | `string` | | ISO-8601 date (`YYYY-MM-DD`) after which the experiment is inactive. The control variant is returned after this date without incrementing any counter. |
| `continual` | `object` | | Experimental deterministic control/candidate assignment with automatic traffic ramping. |

## Continual experiment ramps

:::caution[Experimental]
Continual experiments are experimental and may change in future releases.
:::

A continual experiment assigns a candidate to a bounded share of future executions.
The first variant is the control and the second is the candidate.

```yaml
experiments:
  optimize_tool_use:
    variants: [control, candidate]
    metric: eval:quality
    min_samples: 20
    continual:
      seed: tool-use-v1
      ramp: [10, 25, 50]
```

Assignment happens in the activation job before agent execution. A SHA-256 hash of
the seed, experiment name, repository, workflow, and run ID selects the variant.
The activation job advances the ramp after each `min_samples` candidate assignments
and stores the current stage on the experiment branch, leaving the workflow source
immutable. The logs show the stage, assignment counts, weights, and selected variant.

The ramp does not evaluate outcomes or promote a winner. Use the existing experiment
analysis commands and configured metrics to decide whether to stop the experiment
or make a variant permanent.
