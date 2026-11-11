---
description: Guide for setting up A/B testing experiments in agentic workflows — syntax, design principles, dimensions to test, how to measure results, and anti-patterns.
---

# A/B Testing Experiments in Agentic Workflows

---

## How Experiments Work

Per run:

1. **Restore** — activation job loads experiment state from configured storage (git branch default, or Actions cache).
2. **Pick** — `pick_experiment.cjs` picks the variant with the lowest invocation count (ties broken by array order).
3. **Save** — updated counter written back.
4. **Upload** — state uploaded as workflow artifact `experiment` (30-day retention).
5. **Inject** — variant available as `${{ experiments.<name> }}` and in `{{#if experiments.<name> }}` blocks.

**Key properties**:
- Every run gets one variant per experiment; no sampling.
- Assignment persists across runs automatically.
- Multiple experiments run simultaneously, each independently balanced.

---

## Basic Syntax

```yaml
---
on:
  schedule: daily on weekdays
engine: copilot
experiments:
  prompt_style: [concise, detailed]
---

{{#if experiments.prompt_style == "concise" }}
Summarise the findings in ≤ 5 bullets.
{{#else}}
Provide a detailed analysis with reasoning for each finding.
{{#endif}}
```

### Naming Rules

- Names must match `[a-zA-Z_][a-zA-Z0-9_]*`. Use `lowercase_with_underscores`.
- Non-matching names are silently skipped at compile time.

### Variant Rules

- At least **2 variants** required.
- Plain strings, lowercase descriptive (`concise`, `detailed`, `step_by_step`).
- ~10 variants practical max — sample size per variant grows fast beyond that.

---

## Object Form (Weighted Variants and Date Gating)

Object form supports non-uniform weights, date gating, and governance metadata:

```yaml
experiments:
  prompt_style:
    variants: [concise, detailed, step_by_step]
    weight: [2, 1, 1]           # 50% concise, 25% detailed, 25% step_by_step
    description: "Verbosity A/B test"
    metric: "ai_credits"
    hypothesis: "H0: no change in ai_credits. H1: concise reduces by >=15%"
    guardrail_metrics:
      - name: success_rate
        threshold: ">=0.95"
      - name: empty_output_rate
        direction: min
        threshold: 0.0
    issue: "42"
    start_date: "2026-05-01"
    end_date: "2026-06-01"
```

**Fields:**

- `variants:` — array of variant strings (required, ≥ 2 entries).
- `weight:` — non-negative integers, same length as `variants`. Enables weighted-random selection. `[2, 1, 1]` = 50/25/25. All zeros → always returns control (first variant). Omit for round-robin.
- `start_date:` / `end_date:` — ISO-8601 `YYYY-MM-DD`. Outside this window, control variant is returned and counters do not increment.
- `description:`, `metric:`, `issue:`, `hypothesis:` — governance metadata (no runtime effect).
- `guardrail_metrics:` — array; once minimum samples and supported observations are available for every mandatory guardrail, any failure produces a deterministic core decision of `REJECT`. Each entry:
  - `name` (required) — metric identifier.
  - `threshold` (required) — comparison string (`">=0.95"`, `"==0"`) or bare number paired with `direction`.
  - `direction` (optional, `"min"`/`"max"`) — lower-better vs higher-better. With bare numeric `threshold`: `min` → metric ≤ threshold; `max` → metric ≥ threshold.

Bare-array and object forms can be mixed in the same `experiments:` map.

---

## Storage Configuration

```yaml
experiments:
  storage: repo   # or: cache
  prompt_style: [concise, detailed]
```

| Value | Behaviour | When to use |
|---|---|---|
| `repo` (**default**) | Commits `state.json` to branch `experiments/{sanitizedWorkflowID}` (hyphens stripped, e.g. `my-workflow` → `experiments/myworkflow`). Adds a `push_experiments_state` job; needs `contents: write`. Durable. | Recommended for all experiments. |
| `cache` | GitHub Actions cache. No extra job/permission. May evict after 7 days of inactivity. | Use only when `contents: write` cannot be granted. |

> The branch is created automatically on first run as an orphan containing `state.json` and `assignments.json`.

---

## Referencing the Active Variant

Two forms, both resolved before the agent sees the prompt:

### 1 — Conditional blocks (most common)

```markdown
{{#if experiments.tone == "formal" }}
Use formal, professional language throughout the report.
{{#else}}
Use a friendly, conversational tone.
{{#endif}}
```

### 2 — Direct interpolation

```markdown
Use `${{ experiments.tone }}` tone when writing the issue body.
```

---

## Designing a Good Experiment

1. **One dimension** per experiment.
2. **Falsifiable hypothesis**.
3. **Primary metric** measurable from workflow run data (artifacts, outputs, duration, tokens). Prefer `eval:<id>` / `evals.<id>` when success is best measured as a YES/NO question.
4. **Pair the experiment with an eval.** Add at least one `evals:` question that checks whether the assigned variant's intended effect actually shows up in the output (e.g., "does the report follow the STE writing rules?"), and reference it from `metric` or `secondary_metrics` as `eval:<id>`. Purely quantitative metrics (token count, duration, engagement score) don't verify *why* an effect happened — the paired eval does.
5. **Guardrail metrics** — things that must not degrade. Use `direction: min` + bare number for lower-is-better rates, or `">=0.95"` for higher-is-better.
6. **Sample size estimate** per variant.

Prefer high-frequency workflows for faster significance.

---

## Dimensions Worth Experimenting On

### Prompt Design

```yaml
experiments:
  prompt_style: [concise, detailed]
  reasoning_depth: [shallow, deep]
  output_format: [bullets, prose, table, ste]
  tone: [formal, casual]
```

Use `{{#if experiments.prompt_style == "concise" }}` blocks to swap prompt instructions. Always compare against a specific variant value.

> ⚠️ **Never write** the internal env-var form `__GH_AW_EXPERIMENTS__PROMPT_STYLE___detailed`. The compiler expands `experiments.<name>` references automatically.

**Typical metrics**: output quality, AI credits, success rate, output length.

When `metric` references `eval:<id>` or `evals.<id>`, declare that eval question under `evals:`. `gh aw experiments analyze` will then show both the metric question text and observed eval YES/NO/UNKNOWN results.

`ste` (Simplified Technical English) is a text-style variant worth testing alongside `bullets`/`prose`/`table`. It constrains the prompt to a small set of writing rules — short sentences (≤20 words), one instruction or fact per sentence, active voice, present tense, familiar vocabulary, and spelled-out acronyms on first use — to test whether simplified phrasing improves readability, engagement, or token efficiency versus richer prose/table formats.

### Engine & Model

```yaml
experiments:
  engine_variant: [copilot, claude]
```

> ⚠️ **Engine experiments require separate compiled files**: the `engine:` key cannot be switched mid-run from a single file. Use two parallel workflow files and compare run metrics.

**Typical metrics**: run cost (tokens), duration, completion rate, error rate.

### Tool Configuration

```yaml
experiments:
  tool_scope: [narrow, broad]
```

```markdown
{{#if experiments.tool_scope == "narrow" }}
Only use the `issues` and `pull_requests` toolsets.
{{#else}}
Use any available GitHub MCP tools.
{{#endif}}
```

**Typical metrics**: number of tool calls, run duration, output accuracy.

### Skill Usage

```yaml
experiments:
  skill_hint: [enabled, disabled]
```

```markdown
{{#if experiments.skill_hint == "enabled" }}
Check `skills/` and `.github/skills/` for relevant `SKILL.md` files and apply
their guidance.
{{#endif}}
```

**Typical metrics**: output quality, context token consumption, run duration.

### Timeout & Pacing

```yaml
experiments:
  timeout: [short, long]
```

Pair with a conditional step, or use two compiled files with different `timeout-minutes:`.

---

## Minimal Working Example

```markdown
---
description: Daily PR summary — A/B test concise vs. detailed output
on:
  schedule: daily on weekdays
engine: copilot
permissions:
  pull-requests: read
tools:
  github:
    toolsets: [pull_requests]
safe-outputs:
  create-discussion:
    title-prefix: "[pr-summary] "
    close-older-discussions: true
timeout-minutes: 15
experiments:
  output_style: [concise, detailed]
---

Summarise the pull requests merged in ${{ github.repository }} today.

{{#if experiments.output_style == "concise" }}
Write a maximum of 5 bullet points. Each bullet is one sentence.
{{#else}}
Write a structured report with sections for: new features, bug fixes, refactors,
and documentation changes. Include a one-paragraph executive summary at the top.
{{#endif}}

Include links to each PR. Use ${{ github.server_url }}/${{ github.repository }}/pull/<number> format.
```

Compile and deploy:

```bash
gh aw compile pr-summary
```

First run picks `concise` (count 0), second picks `detailed`, alternating until one variant wins.

---

## Multiple Simultaneous Experiments

Independent assignment, all three injected into the prompt:

```yaml
experiments:
  prompt_style: [concise, detailed]
  emoji_density: [heavy, minimal]
  skill_hint: [enabled, disabled]
```

> ⚠️ **Interaction effects** — limit to 2–3 simultaneous experiments unless you can run factorial analysis.

---

## Lifecycle of an Experiment

1. **Design** — hypothesis, dimension, primary + guardrail metrics.
2. **Instrument** — add `experiments:` and `{{#if experiments.<name> == "<variant>" }}` blocks. Never use `__GH_AW_EXPERIMENTS__*`.
3. **Compile** — `gh aw compile <workflow-name>`.
4. **Run** — check activation job step summary for variant assignment.
5. **Analyse** — once min sample size reached, compare distributions; for eval-backed metrics, use `gh aw experiments analyze <workflow>` to inspect the resolved question and current eval outcomes.
6. **Conclude** — rewrite baseline to winning variant, remove `experiments:`, recompile.

## Continual Experiment Ramps

`continual:` provides deterministic control/candidate
assignment with a runtime-managed traffic ramp. The first variant is the control
and the second is the candidate. Existing experiments without this block keep
their current behavior.

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

Assignment occurs in the activation job before agent execution. It hashes the seed,
experiment name, repository, workflow, and run ID. The activation job advances the
ramp after each `min_samples` candidate assignments and stores the current stage on
the experiment branch, leaving the workflow source immutable. Each decision logs
the stage, assignment counts, active weights, and selected variant.

The ramp only changes candidate traffic; it does not evaluate outcomes or promote a
winner. Use the existing experiment analysis commands and metrics to decide whether
to stop the experiment or make a variant permanent.

---

## Anti-Patterns

- ❌ **Multiple dimensions in one experiment** — can't attribute the improvement.
- ❌ **Removing `experiments:` before sample size reached** — resets state, invalidates counts.
- ❌ **Interpreting early results** (<~20 runs/variant) — chance variation dominates.
- ❌ **Experiments as feature flags** — use `features:` for deterministic switches.
- ❌ **Engine experiments in one file** — `engine:` cannot switch mid-run; use two parallel files.
- ❌ **Conditional frontmatter imports** — keep imports security-stable and use `{{#if experiments.<name> }}` with `{{#runtime-import? path}}` (optional form, not promoted to unconditional lock-file macros) for prompt experiments instead.
- ❌ **Nesting `{{#if experiments.<name> }}` inside `{{#runtime-import? }}`** — evaluation order is brittle across import boundaries. Prefer explicit branching in the main workflow prompt or separate workflow files per variant.
- ❌ **Writing the internal env-var form** `__GH_AW_EXPERIMENTS__*` — implementation detail, may change.
