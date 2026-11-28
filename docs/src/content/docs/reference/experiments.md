---
title: A/B Experiments
description: Run A/B experiments in GitHub Agentic Workflows to test prompt variants and measure the effect of different instructions across runs.
sidebar:
  order: 7
---

Use the `experiments` frontmatter section to compare workflow variants across
repeated runs. Each experiment declares a name and a set of variants. On every
run, the activation job picks one variant and exposes it to the prompt.

Experiments work best when you test one workflow choice at a time, such as:

- prompt wording
- model selection
- whether to delegate to a sub-agent
- which subskill (inline skill) to invoke

## Parallel execution

Parallel experiments are an opt-in compilation pass that appends an experimental
matrix job after compiling the production workflow. Existing production jobs,
dependencies, permissions, and outputs remain unchanged. Without the opt-in,
compilation follows the existing path.

The top-level `parallel-experiments` field selects variants from existing
`experiments` definitions. Omit it or set it to `false` to disable the pass.

```aw wrap
---
on: workflow_dispatch
engine:
  id: copilot
model-routing: false
network:
  allowed: []
permissions:
  contents: read
  issues: read
  pull-requests: read
experiments:
  style: [concise, detailed]
parallel-experiments:
  include:
    - experiment: style
      variant: concise
    - experiment: style
      variant: detailed
---

{{#if experiments.style == 'concise'}}
Summarize this repository concisely.
{{else}}
Summarize this repository in detail.
{{/if}}
```

Each `include` entry names an existing `experiment` and one of its `variant`
values. Optional `engine`, `model`, and `harness` fields override that entry only.
Model overrides must be literal names or aliases. The initial implementation
supports sandboxed Copilot with the `copilot-cli` or `copilot-sdk` harness, not
arbitrary engines or custom harness code.

| Control | Default | Range | Scope |
|---------|---------|-------|-------|
| `max-jobs` | `4` | `1`–`16` | Maximum admitted experimental jobs |
| `max-parallel` | `2` | `1`–`4` | Maximum concurrent experimental jobs |
| `timeout-minutes` | `10` | `1`–`30` | Timeout of each experimental job |
| `max-ai-credits` | `100` | `1`–`1000` | Proxy credit limit of each experimental job |
| `max-total-ai-credits` | `400` | `1`–`4000` | Admission-time experimental credit allocation |

The `include` list accepts up to 16 entries. Admission follows list order and
admits at most the smallest of `max-jobs`, `max-parallel`, and
`floor(max-total-ai-credits / max-ai-credits)`. Remaining entries are recorded as
skipped with an admission-limit reason, rather than scheduled in later batches.
The total credit allocation must cover at least one per-job budget.
Only listed variants participate; existing across-run experiment selection is
unchanged. Parallel selection is explicit: it does not apply the across-run
picker's weights, date windows, or adaptive allocation.

Each admitted entry varies one experiment, not a Cartesian product of all
experiments. Experimental jobs have the production agent's upstream dependencies
and can become runnable alongside it, using the same repository revision, event,
and workflow inputs. Scheduling is best effort: runner availability, organization
concurrency limits, provider throttling, and GitHub Actions scheduling can delay
individual jobs. There is no start barrier or guarantee of simultaneous execution.

Experimental failures do not gate production jobs. The matrix disables fail-fast
and tolerates experimental job failures. Its jobs can still extend the workflow's
overall duration, consume runner capacity, and be cancelled by workflow-level
cancellation or concurrency policies. An overall successful workflow conclusion
does not prove that every experiment succeeded.

### Isolation limits

The initial pass accepts a deliberately restricted configuration. It requires the
standard AWF sandbox on GitHub.com and `network.allowed: []`; inference goes
through the sandbox API proxy. Model routing must be disabled. Experimental jobs
use read-only repository permissions, disable GitHub tools, and do not persist
memory or caches. They force Copilot bare mode and disable dynamic workflows;
their ambient configuration can therefore differ from production.

Unsupported host capabilities fail compilation rather than being treated as safe
by staging alone. These include authored steps, custom credentials, custom MCP
servers, plugins, imports, sub-agents, persistent memory, and custom engine code.
Supported safe-output proposals are `create-issue`, `update-issue`, `add-comment`,
and `noop`; experimental proposals never reach the production safe-output executor.
Production configuration is not rewritten to accommodate these restrictions.

### Artifact contract

Experimental artifacts use the `parallel-experiment-<run-attempt>-<entry-id>-`
prefix rather than production artifact names, avoiding collisions on reruns.
Entry IDs follow configuration order
(`entry-1`, `entry-2`, and so on). The final agent bundle contains:

| File | Contract |
|------|----------|
| `parallel.json` | Versioned manifest: experiment and variant, configured and observed configuration, run ID, run attempt, job and matrix identity, admission, execution timestamps, step outcomes, usage, and missing evidence |
| `aw_session.jsonl` | Redacted unified agent session, collected without running graders |
| `staged-safe-outputs.json` | Collected safe-output intents; raw fallback records are marked as unvalidated |
| `evidence/` | Allowlisted, redacted source evidence, including agent logs, configuration, usage, and available OpenTelemetry mirrors |

The manifest distinguishes configuration and admission from execution and
completion. Its `usage` fields use `null` for unavailable measurements.
`missing_evidence` identifies incomplete collection; configuration evidence
distinguishes authored settings from observed runtime resolution. An absent
resolved model is not replaced with a claim that the requested model ran.
Job identity is the workflow job key plus matrix entry, run ID, and run attempt;
it is not GitHub's numeric job database ID.

Collection and uploads are best effort, including after failures. Abrupt runner
termination or cancellation can prevent final timestamps, consumption records,
or the entire artifact from being saved. Step outcomes recorded during collection
are not the final GitHub Actions job conclusion. Use Actions job records to
reconcile missing artifacts and later cleanup or upload failures.

### Accounting and comparability

Each experimental sandbox has independent usage accounting. AI credits, token
counts, and cache usage describe that execution, not the production agent or a
sum of every job in the run. Partial accounting from failed executions remains
useful but is not evidence of a complete bill. Missing provider or proxy records
remain unavailable rather than becoming zero.
Existing production usage summaries are not combined run-wide totals when
parallel experiments are enabled; account for experimental bundles separately.

Credit enforcement uses the existing sandbox API proxy's per-execution limit.
It is not an atomic reservation against an organization-wide or workflow-wide
credit balance. In-flight requests and provider reporting can affect the final
observed consumption; proxy accounting does not replace the provider's invoice.
Admission limits and per-job credit limits bound planned experimental spending
without changing production limits.

OpenTelemetry export and local telemetry mirrors retain their existing
configuration. Experiment assignments identify experimental telemetry; operators
must also retain job and run-attempt identity when grouping observations. Do not
aggregate solely by workflow run ID, because production and experiments share it.

The same Git revision and event do not freeze remote services or repository
state. Tool responses, caches, provider load, and production writes can differ
between agents. Treat comparisons as observational evidence rather than a
guaranteed controlled execution environment.

### Offline evaluation

Experimental execution collects evidence but does not add threat-detection,
safe-output execution, grader, eval, or aggregation jobs. Safe-output requests are
retained as staged intents, not delivered effects. Operators download experimental
artifacts and run graders, BinEval, or comparative analysis separately.

Download bundles through the Actions UI or
`gh run download <run-id> --pattern 'parallel-experiment-*'`. Keep each artifact
in its own directory. Existing `gh aw experiments analyze` across-run reports do
not automatically merge these parallel bundles.

Keep production and experimental accounting separate. Existing across-run
experiment state and analysis are not evidence that every parallel variant ran.
A configured or admitted variant is not an executed variant: runner cancellation,
setup failure, or missing artifacts can leave execution and consumption unknown.
Do not treat missing consumption as zero or infer a completed execution from an
assignment alone.

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

`metric` can reference an eval question with `eval:<id>` (or `evals.<id>`), or a
grader result with `grader:<id>` (or `graders.<id>.value`). Use `adoption:accepted`
or `adoption:zero_touch` to measure live adopted outputs with
`gh aw experiments analyze <workflow> --adoption`. These references also work in
`secondary_metrics` and `guardrail_metrics` without declaring a grader or eval.

`gh aw experiments analyze <workflow>` joins persisted assignments to each run's eval
answers or grader artifacts, then compares variants with the same statistics. Eval
results also show YES/NO/UNKNOWN totals.

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

### Try different models

Compare speed, cost, and quality with model aliases such as `small` and `large`. See [Model Aliases](/gh-aw/reference/model-tables/).

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

With Copilot, you can also declare `subagent_model: [small, large]` and set the
sub-agent's `model` to `${{ experiments.subagent_model }}`. Activation replaces the
reference with the selected variant before writing the sub-agent file and records that
variant in the run's declared-model metadata. If no assignment is available, activation
fails rather than writing the expression. Audit patterns and model-routing request policy
expand every variant through the model aliases, still constrained by `models.allowed` and
`models.blocked`. Router candidates are unchanged. Matching observed usage does not prove
the variant was honored.

Use a direct `${{ experiments.subagent_model }}` reference. Compound expressions such as
`${{ experiments.subagent_model || 'small' }}` or `format(...)` are rejected, because the
sub-agent runtime does not evaluate them. Put the complete model or alias in each variant.
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
  storage: repo   # or: cache, broker (default: repo)
  prompt_style: [concise, detailed]
```

| Value | Behavior |
|---|---|
| `repo` (**default**) | Commits state to a git branch named `experiments/{sanitizedWorkflowID}` (workflow ID lowercased with hyphens removed, e.g. `my-workflow` → `experiments/myworkflow`). Durable — survives cache evictions. Requires `contents: write` permission (added automatically by the compiler). |
| `cache` | Uses GitHub Actions cache (legacy). State may be evicted after 7 days of inactivity. |
| `broker` | Uses a deployment-owned OIDC-authenticated HTTPS service for assignments, cumulative counts, and run-result observations. No experiment git/cache state or added `contents: write` permission. |

When `storage: repo`, the compiler adds a `push_experiments_state` job after the
activation job and commits the updated `state.json` to the experiments branch.

### Broker storage

```aw wrap
---
experiments:
  storage: broker
  broker-url: https://broker.example.com/experiments
  prompt_style: [concise, detailed]
---

Summarize in a **${{ experiments.prompt_style }}** style.
```

`broker-url` must be a literal HTTPS endpoint, not a GitHub Actions expression. If
omitted, it defaults to the `GH_AW_EXPERIMENTS_BROKER_URL` Actions secret (repository or
organization). Assignment and reporting both POST to this exact endpoint, which is also the
OIDC audience. Do not put credentials in the URL. A shared workflow import can provide
`storage: broker` and `broker-url`, but the importing workflow's values win. Imported variants
and non-broker storage modes are not merged, and an explicit non-broker mode in a shared
workflow stops a nested import from enabling broker storage. Imports are trusted
configuration: the endpoint receives the workflow's OIDC identity. gh-aw does not ship the server.

Activation obtains assignments and cumulative counts from the broker. After conclusion (and
graders, when needed), a separate isolated job submits allowlisted numeric and status
observations from `activity/experiment-results.json` in the `usage` artifact. Reporting is
best effort: failures, cancellation, or missing artifacts can leave assignments without
results. Reported values are untrusted observations, not authorization or verified adoption.
Eval answers, custom grader values, and adoption results are not reported. Authored steps and
workflow-level environment hooks cannot customize the report job. It runs on the framework
runner (`runs-on-slim` or `safe-outputs.runs-on`), which must satisfy the deployment's runner
policy. Friction costs are reported only for measured dimensions; unavailable values are
omitted, not recorded as zero. A new attempt needs fresh activation and conclusion artifacts;
rerunning only a failed reporter rejects prior-attempt data.

Broker storage requires a generated agent job and does not support status-only workflows.
Define an agent job, or use `repo` or `cache` storage instead. Repeated reusable-workflow
calls with identical inputs in the same attempt are also unsupported, because they share the
artifact and invocation prefix. The broker must bind invocation hints to trusted job evidence,
not trust the hash alone.

Calling jobs need `id-token: write` within their permission envelope. Broker errors do not
retry or fall back to git or cache. Dry-run uses an ephemeral local picker and makes no broker
HTTP or OIDC calls. `repo` and `cache` behavior is unchanged.

`gh aw experiments` commands, including `gh aw experiments analyze`, currently
use only the git backend; there is no broker analysis client. See the
[Experiments Broker Specification](/gh-aw/specs/experiments-broker-specification/)
for the wire protocol, server authorization, and deployment requirements.

## Accessing assignments downstream

Each experiment exposes its selected variant as an activation job output:

| Expression | Description |
|---|---|
| `needs.activation.outputs.<name>` | Selected variant for experiment `<name>` |
| `needs.activation.outputs.experiments` | All assignments as a JSON object |

Use these expressions in downstream jobs defined in the `jobs:` frontmatter section.

## Analyzing results

The activation job uploads the counter state as an `experiment` artifact. This
per-run artifact can show broker assignments and count snapshots, but does not
provide access to the broker's result dataset. Download and inspect it with the
`gh aw` CLI:

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

### Analyze an adoption-backed metric

Measure whether a workflow's safe outputs were accepted after a maturity window:

```aw wrap
---
experiments:
  prompt_v2:
    variants: [control, candidate]
    metric: adoption:accepted
    adoption_window: auto
    guardrail_metrics:
      - name: adoption:zero_touch
        threshold: ">=0.8"
    min_samples: 20
---

Follow the **${{ experiments.prompt_v2 }}** instructions.
```

Run `gh aw experiments analyze <workflow> --adoption` to evaluate adopted outputs on demand.
No scheduled collector is needed. Collection is opt-in because it downloads artifacts and makes
extra GitHub API requests. Without `--adoption`, an adoption-backed metric returns an error that
explains how to enable collection, and no adoption evidence is fetched.

`adoption_window` defaults to `auto`. Analysis estimates the pooled 95th-percentile decision time
across this experiment's variants, using Kaplan–Meier censoring for still-open outputs. It clamps
the result to 24 hours–30 days and uses `72h` until 20 outputs have known decision times. Set a
positive Go duration such as `72h` to override it. Only runs at least the effective window contribute observations. Each run contributes one value: accepted outputs divided by resolved outputs, or
zero-touch outputs divided by resolved outputs. Resolved outputs are accepted, rejected, or still
pending after the window; stale pending outputs score zero.

Zero-touch means a merged PR with no non-bot comments, reviews, or commits after creation. Accepted
PR outputs without complete zero-touch evidence are excluded rather than scored zero, as are unknown
or errored outputs. Historical records without `zero_touch_evidence_complete` remain valid but do not
establish complete evidence. Lifecycle, lifecycle-close, ignored, and skipped outputs do not contribute.
Runs with no adoption-bearing outputs are no-ops, not successes. Other resolved output types stay in
the denominator and score zero for `adoption:zero_touch`.

Text and `--json` reports show per-variant run coverage (`immature`, `stale_pending`, `unavailable`,
and `no_op`). A run with some usable outputs can also count as stale pending or unavailable. Only usable
run observations count toward `min_samples`. When `analysis_type` is omitted, the analyzer uses a
two-proportion test if every run value is 0 or 1, and Welch's t-test otherwise (for example, when a run
creates several PRs). Explicit `proportion_test` or `bayesian_ab` requires binary per-run values and
cannot analyze fractional adopted outputs. Reports include the effective window, its source, and the
sample size used for learning or the cold-start fallback. Adoption-backed secondary metrics also include
per-variant observations, coverage, and comparisons. Prefer `adoption:zero_touch` when acceptance is
already high and reducing human intervention is the goal.

### Analyze a grader-backed metric

Use a deterministic grader as the primary adoption metric:

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

This analysis does not replay traces or invoke an evaluator model. The decision layer treats
grader observations like any other resolved numeric metric.

### Analyze an eval-backed metric

Use a BinEval question as the primary adoption metric:

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

`gh aw experiments analyze <workflow>` joins each assigned run's eval answer (from `evals.jsonl`)
to the assignment ledger by run ID and reports assigned, usable, and excluded YES/NO observations
per variant. Runs with a missing or `UNKNOWN`/unrecognized answer are excluded, not counted as zero
or `NO`. The same t-test, Mann–Whitney, proportion, or Bayesian comparisons used for grader-backed
metrics apply to the YES/NO adopted outputs.

The evals job batches 1–400 binary questions into one OpenAI-compatible `POST /decisions` request,
with `choice` answers (`YES`, `NO`, or `UNKNOWN`). It runs independently of the agentic engine and
defaults to `copilot/gpt-6-luna-decisions`. Set `evals.model` to another Decisions-compatible model,
or to `openai/gpt-6-luna` to use `CODEX_API_KEY` or `OPENAI_API_KEY` instead of Copilot credentials.
Use the provider's actual model ID; names are not remapped. OpenAI WIF under `engine.auth` applies only
to Codex inference through the AWF API proxy and is not inherited here, so OpenAI evals still require
`CODEX_API_KEY` or `OPENAI_API_KEY`. Per-question model overrides are not supported. Transport failures
are recorded as `not_evaluated`, not as negative or unknown answers.

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

Assignment identifies the treatment (`control` or `candidate`), not an adoption. Analysis
estimates the effect and evidence. Readiness checks whether every variant has reached
`min_samples`: usable primary-metric observations for observation-backed metrics, or assignment
counts when no supported observation source is resolved. The decision then applies policy and
mandatory guardrails to the existing analysis.

For two-variant experiments, the command emits one of these decisions:

| Decision | Meaning |
|---|---|
| `EXTEND` | More valid primary or guardrail observations are required, or analysis is not yet computable. |
| `PROMOTE` | The candidate has sufficient statistical evidence, exceeds the practical-effect threshold, and passes all guardrails. |
| `REJECT` | The candidate materially regresses or fails a mandatory guardrail. |
| `INCONCLUSIVE` | Minimum samples exist, but the evidence or practical effect does not establish a winner. |

`EXTEND` means evidence collection or computation is incomplete. `INCONCLUSIVE` means the
available evidence was adjudicated without establishing an improvement or regression. An
unproven candidate is not automatically harmful; `REJECT` requires a failed guardrail or
sufficient evidence of a material regression.

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

  READY FOR ANALYSIS — all 2 variants have reached min_samples (20); adoption metric analysis is available

  Decision   : PROMOTE candidate
  Reason     : candidate materially improves the primary metric with sufficient evidence and all guardrails pass (candidate_improved)
```

Before enough samples accumulate, output looks like:

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

Each `analyses` entry includes `decision`, `reason_code`, `samples`, `decision_guardrails`,
and `decision_policy`, plus the separate `readiness` field. `control`, `candidate`, `effect`,
and `evidence` appear only when available, such as for two-variant statistical comparisons.
Automation should consume these fields rather than recomputing statistics.

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
but must preserve the core decision.

The CLI supports four analysis methods (`t_test`, `mann_whitney`, `proportion_test`, and
`bayesian_ab`). Frequentist methods provide p-values. `bayesian_ab` provides
`probability_superiority`, which is significant when it is at least `confidence` or at most
`1-confidence`. It is never described as a p-value.

The daily experiment report uses this structured core decision and adds only the interaction
safety hold described above. Older terms such as `ABANDON` and `GUARDRAIL_FAILED` are not core
decision values.

Existing experiment configurations remain compatible. Analysis still defaults to 20 samples per
variant and the configured or inferred statistical method. A deterministic `decision` is emitted
even when no `decision:` block exists, using `minimum_effect: 0`, `regression_tolerance: 0`, and
`confidence: 0.95`. The legacy `recommendation` remains in JSON. No migration is required.

Decisions are only as meaningful as their observations. Low-frequency or heterogeneous tasks may need larger samples, and simultaneous experiments can complicate attribution. A passing non-regression check covers only the configured evidence, not universal non-regression.

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

### Map fields

| Field | Type | Description |
|---|---|---|
| `experiments` | `object` | Map of experiment name → variant array or config object |
| `experiments.storage` | `string` | Persistence backend: `repo` (default), `cache`, or `broker`; reserved, not an experiment name |
| `experiments.<name>` | `string[]` | Array of two or more variant strings for one experiment |

### Object form fields

| Field | Type | Required | Description |
|---|---|---|---|
| `variants` | `string[]` | ✅ | Array of two or more variant strings |
| `description` | `string` | | Human-readable explanation of what the experiment tests |
| `hypothesis` | `string` | | Null and alternative hypothesis (e.g. `"H0: no change. H1: concise reduces AIC by >=15%"`) |
| `metric` | `string` | | Primary metric to observe (e.g. `aic`, `duration_ms`) |
| `secondary_metrics` | `string[]` | | Additional metrics to track alongside the primary metric |
| `guardrail_metrics` | `object[]` | | List of guardrail objects with `name` (string), `threshold` (comparison string like `>=0.95` or bare number like `0.0`), and optional `direction` (`"min"` or `"max"`). When `threshold` is a bare number, `direction` governs the pass condition (≤ for `min`, ≥ for `max`). See [experiments-specification §4.4](/gh-aw/specs/experiments-specification/#44-guardrail-metrics) for full semantics. |
| `min_samples` | `integer` | | Minimum usable runs per variant before analysis is considered reliable. The step summary shows progress toward it. |
| `analysis_type` | `string` | | Statistical method: `t_test`, `mann_whitney`, `proportion_test`, or `bayesian_ab`. |
| `decision` | `object` | | Deterministic policy with optional non-negative `minimum_effect`, non-negative `regression_tolerance`, and `confidence` between 0 and 1. Effects use absolute primary-metric units. |
| `weight` | `integer[]` | | Per-variant probability weights (same length as `variants`). Enables weighted-random selection; values are relative and need not sum to 100. |
| `issue` | `integer` | | GitHub issue number that tracks this experiment's lifecycle |
| `start_date` | `string` | | ISO-8601 date (`YYYY-MM-DD`) before which the experiment is inactive. The control variant is returned, and no counter increments. |
| `end_date` | `string` | | ISO-8601 date (`YYYY-MM-DD`) after which the experiment is inactive. The control variant is returned, and no counter increments. |
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
The assignment backend advances the ramp after each `min_samples` candidate
assignments and persists the current stage in the configured storage backend
(the experiment branch for `repo`), leaving the workflow source immutable.
The logs show the stage, assignment counts, weights, and selected variant.

The ramp does not evaluate adopted outputs or promote a winner. Use the existing experiment
analysis commands and configured metrics to decide whether to stop the experiment
or make a variant permanent.
