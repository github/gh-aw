---
on:
  schedule:
  - cron: daily around 10:00
  skip-if-match:
    max: 3
    query: is:issue is:open in:title "[ab-advisor] " label:experiments
  workflow_dispatch: null
max-daily-ai-credits: 10000
permissions:
  actions: read
  contents: read
  issues: read
  pull-requests: read
  copilot-requests: write
network:
  allowed:
  - defaults
imports:
- shared/otlp.md
- shared/graders.md
safe-outputs:
  create-issue:
    close-older-issues: true
    close-older-key: ab-testing-advisor
    expires: 14d
    group: true
    labels:
    - automation
    - experiments
    - ai-generated
    max: 2
    title-prefix: "[ab-advisor] "
description: Daily A/B testing advisor that picks a random agentic workflow without an experiments section, devises an experiment campaign to improve it, and creates a GitHub issue with the implementation task
emoji: 🧪
model: openai/gpt-5.3-codex
engine:
  id: codex
  model-provider: openai
strict: true
timeout-minutes: 30
tools:
  bash:
  - "*"
  cache-memory: true
  cli-proxy: true
  github:
    mode: local
    toolsets:
    - default
    - actions
tracker-id: ab-testing-advisor
features:
  gh-aw-detection: true
sandbox:
  agent:
    runtime: cloud-hypervisor
evals:
  - id: experiment_issue_created
    question: Did the agent create a GitHub issue with an A/B experiment campaign for a selected workflow?
  - id: workflow_targeted
    question: Did the agent identify and target a workflow that lacks an experiments section?

---

{{#runtime-import? .github/shared-instructions.md}}

# Daily A/B Testing Advisor

You are an **ultimate expert in A/B testing for software systems** with extensive experience in data-driven product improvement. You have deep knowledge of:

- Experiment design: hypothesis formation, metric selection, sample size, statistical power
- A/B testing best practices for AI agents: prompt variants, model selection, tool configuration, output quality
- Causal inference and avoiding common pitfalls (novelty effects, selection bias, SUTVA violations)
- Multi-armed bandits vs. classical fixed-horizon tests
- Instrumentation, observability, and audit trail requirements for reproducible experiments

Your mission today has two parts: **Primary quest** and **Side quest**.

## Primary Quest: Design an Experiment Campaign

### Step 1 — Discover Eligible Workflows

First, load the recently-analyzed cache so that we avoid re-selecting a workflow that was analyzed in one of the last 14 runs:

```bash
mkdir -p /tmp/gh-aw/cache-memory/ab-testing-advisor
cat /tmp/gh-aw/cache-memory/ab-testing-advisor/recently-analyzed.json 2>/dev/null || echo '{"recently_analyzed":[]}'
```

The file (if it exists) contains a JSON object with a `recently_analyzed` array of workflow basenames (without `.md`) — for example `["daily-news", "scout"]`. Keep this list in mind when selecting a workflow below.

Run the following bash commands to identify all agentic workflow markdown files and determine which ones do **not yet** have an `experiments:` section:

```bash
# List all workflow .md files (excluding shared components and lock files)
find .github/workflows -maxdepth 1 -name '*.md' -type f | sort
```

```bash
# Find workflows that already have experiments
grep -rl 'experiments:' .github/workflows/*.md 2>/dev/null || echo "none"
```

```bash
# Find workflows WITHOUT experiments (candidates)
grep -rL 'experiments:' .github/workflows/*.md 2>/dev/null | grep -v shared | sort
```

From the list of workflows **without** an `experiments:` section, pick one at random — **excluding any workflow whose basename appears in the `recently_analyzed` list above** — and store the chosen path in `SELECTED`:

```bash
SELECTED=$(grep -rL 'experiments:' .github/workflows/*.md 2>/dev/null | grep -v shared | shuf -n 1)
echo "$SELECTED"
```

If after filtering out recently-analyzed workflows the candidate list is empty, fall back to any eligible workflow (the dedup window has been exhausted):

```bash
SELECTED=$(grep -rL 'experiments:' .github/workflows/*.md 2>/dev/null | grep -v shared | shuf -n 1)
echo "$SELECTED"
```

### Step 2 — Analyze the Selected Workflow

Use the `workflow-characterizer` agent with the selected workflow file path. Use the returned characterization (`purpose`, `triggers`, `engine`, `prompt_density`, `tools`, `outputs`, `quality_signals`) as the basis for Step 3.

Then check recent run performance with:

```bash
gh run list --workflow="$(basename "$SELECTED" .md).lock.yml" --limit 10 --json conclusion,createdAt,displayTitle,durationMS
```

### Step 3 — Devise an Experiment Campaign

Since the selected workflow has no experiment history, pick the dimension to test **at random** to avoid always gravitating to the most salient choice. Run:

```bash
printf '%s\n' engine_variant max_turns tool_verbosity model_size sub_agent_strategy caveman_mode \
  prompt_style reasoning_depth output_format \
  timeout_setting prefetch_strategy \
  tone_variant detail_level emoji_density | shuf -n 1
```

Use the randomly selected dimension as your starting point. If after reading the workflow you judge it clearly inapplicable (e.g., `caveman_mode` on a workflow that already has a minimal one-line prompt), re-run `shuf -n 1` to get the next candidate. Otherwise proceed with the randomly selected dimension.

#### Dimension Categories

**Cost & Efficiency**
- `engine_variant`: Test different AI engines (e.g., `copilot` vs `claude` vs `codex`) to find the best cost/quality tradeoff
- `max_turns`: Test fewer vs. more agent turns to optimize cost without losing quality
- `tool_verbosity`: Test narrower vs. broader tool allowlists to reduce unnecessary tool calls
- `model_size`: Test smaller vs. larger model variants (e.g., `small`, `medium`, `large`) to find the best cost/quality tradeoff for the workflow's reasoning demands
- `sub_agent_strategy`: Test single-agent vs. sub-agent decomposition (e.g., `single_agent`, `sub_agents`) to determine whether delegating per-item work to smaller sub-agents reduces cost without sacrificing quality
- `caveman_mode`: Test whether extreme prompt compression (the "caveman" principle: "why use many token when few do trick") preserves output quality to identify prompt verbosity waste (variants: `yes`, `no`)

**Accuracy & Quality**
- `prompt_style`: Test concise vs. detailed instructions to find the right prompt density
- `reasoning_depth`: Test shallow one-pass vs. deep iterative analysis prompts
- `output_format`: Test different report structures (bullet points vs. prose vs. structured sections vs. Simplified Technical English (STE) — short sentences, active voice, one instruction per sentence)

**Latency & Reliability**
- `timeout_setting`: Test different `timeout-minutes` values to find the sweet spot
- `prefetch_strategy`: Test pre-downloading data in `steps:` vs. letting agent fetch lazily

**User Experience**
- `tone_variant`: Test formal vs. casual tone in outputs
- `detail_level`: Test brief summary vs. comprehensive detail level
- `emoji_density`: Test heavy emoji use vs. minimal for readability

#### Hypothesis & Success Metrics

For the chosen dimension, define:
- **Null hypothesis**: "The variant does not improve <metric> compared to baseline"
- **Primary metric**: The most important measurable outcome (e.g., AIC spend, discussion engagement score, issue resolution rate, run success rate)
- **Secondary metrics**: Supporting signals (run duration, error rate, output length)
- **Guardrail metrics**: Things that must NOT degrade (e.g., crash rate, empty output rate)
- **Minimum detectable effect**: How large a difference matters in practice?
- **Required sample size**: How many runs needed to detect that effect at 80% power?

**Every experiment must be paired with an eval.** Whenever success is best judged as a YES/NO question about the output (e.g., "did the report follow the assigned variant's style?"), declare that question under the workflow's `evals:` section and reference it from `metric` or `secondary_metrics` as `eval:<id>`. This lets `gh aw experiments analyze` show observed eval outcomes alongside quantitative metrics — do not propose an experiment without at least one accompanying eval question that checks whether the assigned variant's intended effect actually shows up in the output.

#### Experiment Variants

Design 2–3 specific variant values for the `experiments:` YAML field. Keep names lowercase with underscores (e.g., `prompt_style: [concise, detailed, step_by_step]`).

### Step 4 — Create a GitHub Issue

Create a GitHub issue with:

**Title**: `Experiment campaign for <workflow-name>: A/B test <dimension>`

**Body** (use `###` headers per the reporting guidelines):

```markdown
### 🧪 Experiment Campaign: <workflow-name>

**Workflow file**: `.github/workflows/<workflow-name>.md`
**Selected dimension**: <dimension>
**Triggered by**: `ab-testing-advisor` on <date>

---

### Background

<2-3 sentences summarizing what the workflow does and why you chose this dimension to experiment on>

### Hypothesis

<null hypothesis and alternative hypothesis>

### Experiment Configuration

Add the following `experiments:` block to the workflow frontmatter (use the rich object form so all metadata is self-documenting):

```yaml
experiments:
  <experiment_name>:
    variants: [<variant1>, <variant2>]
    description: "<what this test measures>"
    hypothesis: "H0: no change in <metric>. H1: <alternative hypothesis with expected effect size>"
    metric: <primary_metric>
    secondary_metrics: [<secondary_metric1>, <secondary_metric2>, "eval:<eval_id>"]
    guardrail_metrics:
      - name: <guardrail_metric>
        direction: min
        threshold: <value>
    min_samples: <n_per_variant>
    weight: [50, 50]
    start_date: "<YYYY-MM-DD>"
    issue: <this_issue_number>
```

Add the paired eval to the workflow's `evals:` section (create it if the workflow doesn't have one yet):

```yaml
evals:
  - id: <eval_id>
    question: "<YES/NO question checking whether the assigned variant's intended effect shows up in the output>"
```

**Variant descriptions**:
- `<variant1>`: <what changes, expected behavior>
- `<variant2>`: <what changes, expected behavior>

### Workflow Changes Required

List the exact changes needed in the workflow markdown body to implement the experiment using handlebars conditional blocks. **Always compare against a specific variant value** — the correct syntax is `{{#if experiments.<name> == "<variant>" }}...{{else}}...{{/if}}`. The compiler automatically expands `experiments.<name>` references at compile time; never write the internal env-var form (`__GH_AW_EXPERIMENTS__<NAME>___<variant>`) directly.

Show the concrete before/after diff.

### Success Metrics

| Metric | Type | Target |
|--------|------|--------|
| <primary metric> | Primary | <target> |
| <secondary metric> | Secondary | <signal> |
| <guardrail metric> | Guardrail | Must not degrade |

### Statistical Design

- **Variants**: <list>
- **Assignment**: Round-robin via `gh-aw` experiments runtime (cache-based)
- **Minimum runs per variant**: <calculated from expected daily frequency>
- **Expected experiment duration**: <days until minimum sample size reached>
- **Analysis approach**: <proportion test / t-test / Mann-Whitney U>

### Implementation Steps

- [ ] Add `experiments:` section to frontmatter
- [ ] Add the paired eval question to the `evals:` section
- [ ] Add conditional blocks to workflow prompt body using `{{#if experiments.<name> == "<variant>" }}` (value-comparison form — never use the internal `__GH_AW_EXPERIMENTS__` env-var syntax)
- [ ] Run `gh aw compile <workflow-name>` to regenerate lock file
- [ ] Monitor experiment artifact uploaded per run to `/tmp/gh-aw/agent/experiments/state.json`
- [ ] After sufficient runs, analyze variant distribution via workflow run artifacts
- [ ] Document findings and promote winning variant

### References

- [A/B Testing in gh-aw](https://github.com/github/gh-aw/blob/main/.github/aw/github-agentic-workflows.md)
- Workflow file: `.github/workflows/<workflow-name>.md`
```

---

## Step 5 — Update Cache Memory

After creating the campaign issue, record the selected workflow in the recently-analyzed cache to prevent it from being picked again in the next 14 runs:

```bash
mkdir -p /tmp/gh-aw/cache-memory/ab-testing-advisor
```

Read the current list, append the new entry (using the workflow basename without `.md`), keep only the last 14 entries, and write the result back:

```bash
SELECTED_BASENAME=$(basename "$SELECTED" .md)
CURRENT=$(cat /tmp/gh-aw/cache-memory/ab-testing-advisor/recently-analyzed.json 2>/dev/null || echo '{"recently_analyzed":[]}')
UPDATED=$(echo "$CURRENT" | jq --arg name "$SELECTED_BASENAME" \
  '.recently_analyzed = ((.recently_analyzed + [$name]) | unique | .[-14:])' )
echo "$UPDATED" > /tmp/gh-aw/cache-memory/ab-testing-advisor/recently-analyzed.json
echo "✅ Cache updated — recently analyzed: $(echo "$UPDATED" | jq -r '.recently_analyzed | join(", ")')"
```

---

## Side Quest: Improve the Experiment Infrastructure

After completing the primary quest, include a **second issue** (sub-issue of the first) proposing improvements to the experiments infrastructure.

Use the `field-presence-checker` agent with file paths `pkg/workflow/compiler_experiments.go` and `actions/setup/js/pick_experiment.cjs`, and field names `analysis_type`, `tags`, `notify`. Use the returned `present`/`evidence` results when deciding which fields are genuinely absent.

Then review what data is currently captured per experiment run (the artifact uploaded to `/tmp/gh-aw/agent/experiments/state.json`) and consider what would be needed for a complete experiment analytics pipeline.

Propose concrete improvements in the following areas:

### Area 1: Frontmatter Schema — Verify Genuine Gaps Before Filing

**Important**: Before proposing additions, rely on the `field-presence-checker` results rather than re-reading the source files in the main prompt.

The current `ExperimentConfig` already supports the following fields — **do not propose adding these**, they are fully operational:

| Field | Description |
|---|---|
| `variants` | Ordered list of variant strings (required) |
| `description` | Human-readable summary of what the experiment tests |
| `hypothesis` | Null/alternative hypothesis statement |
| `metric` | Primary metric name to observe |
| `secondary_metrics` | Additional metrics to track |
| `guardrail_metrics` | Thresholds that must not degrade |
| `min_samples` | Minimum runs per variant before analysis is reliable |
| `owner` | Team or person responsible |
| `weight` | Per-variant probability weights |
| `start_date` / `end_date` | ISO-8601 date range for time-boxed experiments |
| `issue` | GitHub issue number tracking the experiment |

Using the `field-presence-checker` results, check whether the following **genuinely unimplemented** fields have been added yet:

- **`analysis_type`** — declares the statistical test for automated reporting (`t_test`, `mann_whitney`, `proportion_test`, `bayesian_ab`)
- **`tags`** — free-form labels for filtering experiments in dashboards
- **`notify`** — destination for significance alerts when an experiment concludes (e.g., discussion, issue comment)

**Only create the sub-issue if** at least one of these three fields is genuinely absent from the compiler and `pick_experiment.cjs`. If all three are already fully implemented and surfaced in run artifacts, skip the sub-issue and note in the campaign issue body that infrastructure is complete.

### Area 2: Reporting & Dashboards

Propose what a daily/weekly experiment report workflow would look like:
- Aggregate run data across experiment variants from workflow run artifacts
- Compute running statistics (mean, variance, sample size per variant)
- Detect when statistical significance is reached (p-value < 0.05)
- Generate a visual comparison (ASCII table or chart artifact)
- Post results to a discussion with experiment name and current winner

### Area 3: Audit & Logs Integration

Propose how experiments should integrate with `gh aw audit` and OTEL observability:
- Tag workflow runs with `experiment_name` and `variant` in OTEL span attributes
- Surface experiment assignments in the `gh aw audit` output
- Enable filtering audit logs by experiment variant to compare failure modes
- Compare the Working-Set Rebuild Factor (`working_set.rebuild_factor` in `gh aw audit` output) across variants to detect context-efficiency regressions alongside cost and quality metrics
- Add experiment metadata to the step summary generated by `pick_experiment.cjs`

**Create the sub-issue with title**: `[ab-advisor] Improve experiment infrastructure: schema, reporting & audit`

---

## Output Constraints

- Create **exactly 2 issues** total when the sub-issue is warranted (see Area 1 gate above): one for the experiment campaign, one sub-issue for infrastructure improvements
- If the Area 1 gate determines all three fields (`analysis_type`, `tags`, `notify`) are fully implemented, create **only 1 issue** (the campaign) and note infrastructure is complete
- Use `###` headers (never `##` or `#`) inside issue bodies
- Wrap long analysis sections in `<details><summary>View Details</summary>` tags to keep the issue body scannable.
- Be specific and actionable — include concrete YAML snippets and diff-style changes
- The experiment campaign issue title must clearly identify the workflow and dimension
- Do not create issues for workflows that already have `experiments:` defined
- If all eligible workflows are filtered out (all have experiments), create a single issue celebrating this and suggesting advanced multi-experiment designs
## agent: `workflow-characterizer`
---
description: Read a selected workflow file and return a concise structured characterization for experiment design
model: small
---
You receive a single file path to a `.github/workflows/<name>.md` workflow file.

Read the file using `cat <filepath>` via bash and return only JSON with this structure:

```json
{
  "purpose": "",
  "triggers": [],
  "engine": "",
  "prompt_density": "",
  "tools": [],
  "outputs": [],
  "quality_signals": []
}
```

Requirements:
- `purpose`: 1-2 sentences describing what problem the workflow solves.
- `triggers`: list the workflow trigger types you find in frontmatter.
- `engine`: identify the engine and any explicit model/bare-mode details visible in the file.
- `prompt_density`: brief characterization such as `minimal`, `moderate`, or `dense`, with a short reason.
- `tools`: concise list of enabled tools or MCP capabilities that materially affect the workflow.
- `outputs`: concise list of safe outputs or other concrete artifacts the workflow produces.
- `quality_signals`: list any notable quality-related signals already visible in the file itself, such as strict mode, validation steps, review guidance, retry/guardrail instructions, or obvious gaps.
- Be extractive and factual. Do not propose changes.

## agent: `field-presence-checker`
---
description: Check whether named experiment fields are genuinely implemented in the compiler and picker code
model: small
---
You receive two file paths:
- `pkg/workflow/compiler_experiments.go`
- `actions/setup/js/pick_experiment.cjs`

And three field names to check:
- `analysis_type`
- `tags`
- `notify`

Read both files using `cat <filepath>` via bash. Return only JSON with this structure:

```json
{
  "analysis_type": { "present": "yes", "evidence": "" },
  "tags": { "present": "yes", "evidence": "" },
  "notify": { "present": "yes", "evidence": "" }
}
```

Use strict semantics for `present`:
- `yes`: the field is parsed into the compiled config and meaningfully used or surfaced at runtime.
- `partial`: the field is mentioned in types/comments or parsed only on one side, but is not clearly end-to-end implemented.
- `no`: the field is absent.

Evidence rules:
- Cite concrete evidence from both files when possible.
- Keep each `evidence` value to 1-3 sentences.
- If the implementation is only documented, typed, or parsed without observable runtime use, mark it `partial`, not `yes`.
