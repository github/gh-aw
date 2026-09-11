---
title: Cost Management
description: Understand and control the cost of running GitHub Agentic Workflows, including Actions minutes, AI Credits (AIC) inference billing, and strategies to reduce spend.
sidebar:
  order: 296
---

If a task can be solved using deterministic tools, use deterministic tools. Only use agents when necessary, as they incur higher cost. Agentic workflows allow you to run deterministic tools first and gate agent execution using conditions, enabling workflows that avoid triggering agents most of the time and only use them when needed.

The cost of running an agentic workflow is the sum of two components: **GitHub Actions minutes** consumed by the workflow jobs, and **inference costs** charged by the AI provider for each agent run. For an overview of how billing works across providers and Copilot licensing, see [Billing](/gh-aw/reference/billing/).

## AI Credits (AIC)

**AI Credits (AIC)** are the primary metric for monitoring and budgeting inference costs in gh-aw. One AIC equals $0.01 USD. AIC values are computed from pricing data sourced from the [models.dev](https://models.dev/) catalog and appear in `gh aw logs`, `gh aw audit`, and run footer messages.

AIC is shown in the `gh aw logs` output table under the **AIC** column, in audit reports alongside raw token counts, and as `{ai_credits_suffix}` in workflow footer templates. For structured output, each run under `.runs[]` includes an `aic` field and each episode under `.episodes[]` includes `total_aic`.

> [!NOTE]
> AIC values are computed on a best-effort basis using pricing data sourced from the [models.dev](https://models.dev/) catalog and may not exactly match your provider's actual billing. Always verify charges in your provider's billing dashboard.

## Cost Components

A typical run includes a short pre-activation job (10–30 seconds) and an agent job (1–15 minutes), each with ~1.5 minutes of runner setup overhead billed at standard [GitHub Actions pricing](https://docs.github.com/en/billing/managing-billing-for-your-products/managing-billing-for-github-actions/about-billing-for-github-actions). Inference is charged by the AI provider based on the engine and API token type used.

## Monitoring Costs with `gh aw logs`

The `gh aw logs` command surfaces per-run metrics — elapsed duration, token usage, AIC (AI Credits), and turn count — before you decide what to optimize. Use `gh aw audit <run-id>` to deep-dive into a single run's token usage, tool calls, and inference spend; its **Metrics** and **Performance Metrics** sections cover token counts, AIC, turn counts, and estimated cost in one place. For cost trends across multiple runs, use `gh aw logs --format markdown [workflow]` to generate a cross-run report with anomaly detection.

### View recent run durations

```bash
# Overview table for all agentic workflows (last 10 runs)
gh aw logs

# Narrow to a single workflow
gh aw logs issue-triage-agent

# Last 30 days for Copilot workflows
gh aw logs --engine copilot --start-date -30d
```

The overview table includes a **Duration** column showing elapsed wall-clock time per run. Because GitHub Actions bills compute time by the minute (rounded up per job), duration is the primary indicator of Actions spend.

### Export metrics as JSON

Use `--json` to get structured output suitable for scripting or trend analysis:

```bash
# Write JSON to a file for further processing
gh aw logs --start-date -1w --json > /tmp/logs.json

# List per-run duration, tokens, and AIC across all workflows
gh aw logs --start-date -30d --json | \
  jq '.runs[] | {workflow: .workflow_name, duration: .duration, tokens: .token_usage, aic: .aic}'

# AIC spend grouped by workflow over the past 30 days
gh aw logs --start-date -30d --json | \
  jq '[.runs[]] | group_by(.workflow_name) |
  map({workflow: .[0].workflow_name, runs: length, total_aic: (map(.aic // 0) | add)})'
```

Each run under `.runs[]` includes `duration`, `token_usage`, `aic`, `workflow_name`, and `agent`. For orchestrated workflows, the same JSON includes deterministic lineage under `.episodes[]` and `.edges[]` — see the next section.

### Interpret Episode-Level Usage

`gh aw logs --json` emits three views of the same data: `.runs[]` for individual workflow runs, `.episodes[]` for related runs grouped into one logical execution, and `.edges[]` for inferred parent-child lineage. Use `.runs[]` to find which run was resource-heavy and `.episodes[]` to answer "what did this job use end-to-end?". For non-orchestrated workflows, an episode collapses to a single run.

For usage analysis, focus on `total_runs`, `total_tokens`, `total_aic`, `total_duration`, `primary_workflow`, `resource_heavy_node_count`, and `blocked_request_count`. For Claude, Codex, and Copilot runs, `total_aic` is the preferred cost metric because it maps to provider billing in AI Credits (1 AIC = $0.01 USD).

```bash
# Top 10 costliest logical executions over the past 30 days by AIC
gh aw logs --start-date -30d --json | \
  jq '[.episodes[] | {episode: .episode_id, workflow: .primary_workflow, runs: .total_runs, aic: (.total_aic // 0)}]
      | sort_by(.aic) | reverse | .[:10]'

# Top 10 heaviest Copilot executions by AIC
gh aw logs --start-date -30d --engine copilot --json | \
  jq '[.episodes[] | {episode: .episode_id, workflow: .primary_workflow, runs: .total_runs, aic: (.total_aic // 0)}]
      | sort_by(.aic) | reverse | .[:10]'
```

## Track Costs at Scale with OpenTelemetry

Use `observability.otlp` to stream run telemetry into a central OpenTelemetry backend when one repository or one `gh aw logs` report is no longer enough. This is best for organization-wide dashboards, alerting, and cross-repository cost analysis.

```aw wrap
observability:
  otlp:
    endpoint: ${{ secrets.OTLP_ENDPOINT }}
    headers:
      Authorization: ${{ secrets.OTLP_TOKEN }}
```

The exported spans include workflow and model metadata such as `gh-aw.engine.id`, `gen_ai.request.model`, `gen_ai.usage.input_tokens`, and `gen_ai.usage.output_tokens`. Use these attributes to group usage by workflow, engine, model, repository, or team in the backend of your choice. For inference cost, derive AIC from raw token counts using provider pricing.

OpenTelemetry helps answer questions like "Which repositories are driving the most token usage?", "Which model change caused a cost spike?", and "Which workflows should move to a smaller model or stricter trigger policy?" See the [OpenTelemetry guide](/gh-aw/reference/open-telemetry/) for collector configuration and the [OpenTelemetry attribute reference](/gh-aw/reference/open-telemetry-attributes/) for emitted fields.

## Trigger Frequency and Cost Risk

The primary cost lever for most workflows is how often they run.

| Risk level | Triggers | Why |
|---|---|---|
| High | `push`, `check_run`, `check_suite` | Fires on nearly every code change; busy repositories can generate many runs per day. |
| Medium-high | `pull_request`, `issues` | Fires on many lifecycle events such as open, sync, label, edit, and close. |
| Medium | `issue_comment`, `pull_request_review_comment` | Scales with discussion activity. |
| Low / predictable | `schedule`, `workflow_dispatch` | Fixed cadence or human-initiated, so easier to budget. |

> [!CAUTION]
> Attaching an agentic workflow to `push`, `check_run`, or `check_suite` in an active repository can generate hundreds of runs per day. Start with `schedule` or `workflow_dispatch` while evaluating cost, then move to event-based triggers with safeguards in place.

## Reducing Cost

### Use Deterministic Checks to Skip the Agent

The most effective cost reduction is skipping the agent job entirely when it is not needed. The `skip-if-match` and `skip-if-no-match` conditions run during the low-cost pre-activation job and cancel the workflow before the agent starts:

```aw wrap
on:
  issues:
    types: [opened]
  skip-if-match: 'label:duplicate OR label:wont-fix'
```

```aw wrap
on:
  issues:
    types: [labeled]
  skip-if-no-match: 'label:needs-triage'
```

Use these to filter out noise before incurring inference costs. See [Triggers](/gh-aw/reference/triggers/) for the full syntax.

### Skip the Agent from Steps Using `noop`

When a condition is too complex for a GitHub search query — for example, when you need to call an API, inspect a file, or apply custom business logic — write a `noop` entry to `$GH_AW_SAFE_OUTPUTS` from a `steps:` block. The harness checks for this entry before starting the AI engine and exits cleanly without incurring any AI Credits.

```aw wrap
steps:
  - name: Skip if no open issues exist
    run: |
      count=$(gh issue list --state open --json number --jq length)
      if [ "$count" -eq 0 ]; then
        echo '{"type":"noop","message":"No open issues to process"}' >> "$GH_AW_SAFE_OUTPUTS"
      fi
    env:
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

When a `noop` entry is present at harness startup, the agent is never started and no AI Credits are charged. The `noop` message appears in the workflow conclusion comment or step summary. The same check also suppresses retries: if a `noop` is written during a failed run, the harness exits 0 instead of retrying.

Use `pre-agent-steps:` instead of `steps:` when the check must run right before the engine starts (for example, after MCP configuration is complete).

Compared to `skip-if-match` and `skip-if-no-match`:

| | `skip-if-match` / `skip-if-no-match` | `noop` in `steps:` |
|---|---|---|
| **Evaluated in** | Pre-activation job (earliest, cheapest) | Agent job (after checkout and steps) |
| **Condition type** | GitHub search query | Arbitrary shell or script logic |
| **Actions minutes saved** | Yes — agent job is never scheduled | No — agent job still runs through setup |
| **AI Credits saved** | Yes | Yes |
| **Best for** | Simple label/status/title filters | Complex API calls or file-based conditions |

For maximum savings, prefer `skip-if-match` / `skip-if-no-match` when possible. Reserve `noop` in `steps:` for conditions that require full scripting access or the agent job environment.

### Choose a Cheaper Model

The `engine.model` field selects the AI model. Smaller or faster models cost significantly less per token while still handling many routine tasks:

```aw wrap
engine:
  id: copilot
  model: gpt-4.1-mini
```

```aw wrap
engine:
  id: claude
  model: claude-haiku-4-5
```

Reserve frontier models (GPT-5, Claude Sonnet, etc.) for complex tasks. Use lighter models for triage, labeling, summarization, and other structured outputs.

### Limit Context Size

Inference cost scales with prompt size. Write focused prompts, avoid whole-file reads when only a few lines matter, cap result counts in tool calls, and use `imports` to compose a smaller subset of prompt sections at runtime.

### Prevent Runaway Costs from Agents

GitHub Agentic Workflows includes default guardrails: a 20-minute timeout on the agentic step, a 60-minute timeout on the generated agent job, a 10-minute timeout on the generated detection job, 1000 AI Credits per workflow run, and 5000 AI Credits per workflow per day (24-hour window). Override them with frontmatter (`timeout-minutes`, `jobs.agent.timeout-minutes`, `jobs.detection.timeout-minutes`, `max-ai-credits`, `max-daily-ai-credits`) or GitHub Actions variables (`GH_AW_DEFAULT_TIMEOUT_MINUTES`, `GH_AW_DEFAULT_AGENT_JOB_TIMEOUT_MINUTES`, `GH_AW_DEFAULT_DETECTION_JOB_TIMEOUT_MINUTES`, `GH_AW_DEFAULT_MAX_AI_CREDITS`, `GH_AW_DEFAULT_MAX_DAILY_AI_CREDITS`).

### Cap AI Credits per Run

Use the top-level `max-ai-credits` frontmatter field to cap the AI Credits (AIC) budget for a single workflow run. It provides a hard stop for unusually expensive runs and a consistent guardrail across all supported engines. The field accepts plain integers or `K`/`M` suffixes such as `100M`.

```aw wrap
max-ai-credits: 500
```

When the budget is approached, gh-aw emits steering warnings before the run reaches the limit. Set a negative value only to disable budget enforcement explicitly.

> [!NOTE]
> Threat-detection runs have their own AI Credits cap, separate
> from the main agent budget. See
> [Threat Detection → Detection Budget](/gh-aw/reference/threat-detection/#detection-budget)
> for `safe-outputs.threat-detection.max-ai-credits` (defaults to
> `400`, overridable via `GH_AW_DEFAULT_DETECTION_MAX_AI_CREDITS`).

### AI Credits with Dynamic Model Selectors

Some engines accept a dynamic model selector instead of a concrete model name — for example `model: auto`, which lets the Copilot service pick the model server-side. AI Credits accounting for those selectors is handled natively by the firewall API proxy (gh-aw-firewall `v0.28.4` and newer), so `max-ai-credits` works with zero extra configuration:

```aw wrap
engine: copilot
model: auto
max-ai-credits: 500
```

gh-aw does not compile a synthetic price for the selector itself. Requests are accounted against the concrete model reported by the response, so each model is charged at its own catalog rate. When a response omits model metadata, the proxy falls back to its conservative rate rather than undercounting.

The same rule applies to `apiProxy.maxModelMultiplierCap`: the cap is evaluated against the multiplier of the *resolved* concrete model, not against the dynamic selector. A selector such as `auto` therefore never bypasses the cap — a request routed to a model whose multiplier exceeds the cap is rejected with HTTP 400 (`model_multiplier_cap_exceeded`). Models without an explicit multiplier use `defaultModelMultiplier` when configured, otherwise the highest configured multiplier.

### Cap Turns per Run

Use the top-level `max-turns` frontmatter field to cap the number of chat iterations (model responses and tool calls) for a single workflow run. Each additional turn consumes more tokens and Actions compute time, so a turn limit bounds both runaway loops and cost.

```aw wrap
max-turns: 20
```

`max-turns` works across Claude, Codex, Copilot, Gemini, and Pi. When set, gh-aw exports the compiled value as `GH_AW_MAX_TURNS` for the engine runtime, so you do not need to set `CLAUDE_CODE_MAX_TURNS` or an equivalent variable separately.

The field accepts integer literals or GitHub Actions expressions, making it composable with `workflow_call` inputs:

```aw wrap
max-turns: ${{ inputs.max-turns || 15 }}
```

> [!NOTE]
> `engine.max-turns` is a deprecated alias for the top-level field
> and continues to compile for backward compatibility. Use
> `gh aw fix engine-max-turns-to-top-level` to migrate existing
> workflows automatically.

An enterprise-wide default can be set with the compiler process environment variable `GH_AW_DEFAULT_MAX_TURNS`. Individual workflows override it by setting `max-turns` in frontmatter.

### Cap Daily AI Credits per Workflow

Use `max-daily-ai-credits` to set a 24-hour AI Credits cap for one workflow. The guardrail sums runs from the past 24 hours of the same workflow across the repository, regardless of who triggered them.

```aw wrap
max-daily-ai-credits: 15M
```

When the total from the past 24 hours exceeds this threshold, activation reports the exceeded budget, skips the agent job, and lets the conclusion job report the failure context.

When omitted, the guardrail inherits the default daily limit (5000 AI Credits unless overridden). Positive values accept plain integers or `K`/`M` suffixes such as `100M`.

> [!CAUTION]
> Every activation lists the current 24-hour window (up to 10 pages of 100 completed runs). A cold scan also reads usage artifacts. Activation publishes all resolved observations, including nonzero usage, so later scans can reuse them without reading each historical artifact again.

Scan snapshots are optional accelerators, not authoritative daily totals. Each record must match the repository, workflow, run attempt, and completed-run timestamps. Missing, expired, and incomplete snapshots require resolving the missing records. The artifact fallback accepts the same workflow's `pull_request_target` runs and default-branch `push`, `schedule`, or `workflow_dispatch` runs; contributor-controlled `pull_request` snapshots are not trusted. This storage path does not require writable Actions cache scopes.

An API failure or incomplete history stops activation rather than reporting an under-budget result from partial data. Quota decisions use business-response headers, including artifact requests; inspection does not continue to the next run after a failure. Respect any logged reset or `Retry-After` deadline before retrying. A missing usage artifact is not zero usage unless the completed attempt's job metadata confirms that the generated billable jobs were skipped.

Cold scans check the compiler-owned `agent`, `detection`, and `evals` jobs independently. Every executed component needs valid accounting; empty collector placeholders and evaluation results alone do not prove zero usage. Raw token usage takes precedence over overlapping summaries. Invalid numeric fields make the accounting unknown. Failed-only reruns retain successful components from earlier attempts, but a rerun component needs a producer artifact from its own job execution. Missing carried-forward usage remains unknown. Older snapshots without component-coverage validation are not reused.

To disable the guardrail explicitly, set `-1`:

```aw wrap
max-daily-ai-credits: -1
```

You can also disable the guardrail at runtime by setting the env variable to `-1`:

```aw wrap
env:
  GH_AW_MAX_DAILY_AI_CREDITS: "-1"
```

For org-wide or enterprise-wide control, prefer setting the default once at the
org or enterprise level instead of per-workflow. Set
`GH_AW_DEFAULT_MAX_DAILY_AI_CREDITS` as an [Actions variable](https://docs.github.com/en/actions/writing-workflows/choosing-what-your-workflow-does/store-information-in-variables)
at the **organization** or **enterprise** scope so every workflow in the org
inherits it without any frontmatter change:

```bash
# Disable the daily guardrail org-wide
gh aw env update - --scope org --org MY_ORG <<'EOF'
default_max_daily_ai_credits: "-1"
EOF
```

> [!NOTE]
> The daily guardrail is skipped for `workflow_call`,
> `repository_dispatch`, and `workflow_dispatch` runs, including dispatches carrying internal
> `aw_context` metadata.

### Roll out org/repo defaults with enterprise controls

For large installations, set baseline model and token guardrails once and let individual workflows override them only when needed:

1. Export current defaults:

```bash
gh aw env get defaults.yml --scope org --org MY_ORG
```

2. Update and apply shared defaults in batch:

```yaml
default_max_ai_credits: "5M"
default_max_daily_ai_credits: "15M"
default_model_copilot: "gpt-5-mini"
default_model_claude: "claude-haiku-4-5"
default_model_codex: "gpt-5.4-mini"
```

```bash
gh aw env update defaults.yml --scope org --org MY_ORG
```

`gh aw env update` shows a confirmation preview before applying changes. Pass `--yes` to skip the prompt in automation or `--dry-run` to preview without changing variables. Set a field to `null` to delete the corresponding variable from the target scope. Unknown YAML keys are rejected, `default_max_turns`, `default_timeout_minutes`, `default_agent_job_timeout_minutes`, and `default_detection_job_timeout_minutes` must be positive integers, and `default_max_ai_credits` and `default_max_daily_ai_credits` must be non-zero integers; negative values disable the corresponding guardrail.

3. If you compile workflows in CI, pass compiler-read defaults into the compiler process environment, for example via `${{ vars.* }}`: `GH_AW_DEFAULT_MAX_TURNS`, `GH_AW_DEFAULT_MAX_TURN_CACHE_MISSES`, and `GH_AW_DEFAULT_DETECTION_MODEL`.

> [!TIP]
> `GH_AW_DEFAULT_MODEL_*` and the `GH_AW_DEFAULT_*_TIMEOUT_MINUTES` values are resolved at workflow runtime via `${{ vars.* }}` in compiled YAML, while max-turn and token defaults are read by the compiler process at compile time.

### Rate Limiting and Concurrency

Use `user-rate-limit` to cap how many times a user can trigger the workflow in a given window, and rely on concurrency controls to serialize runs rather than letting them pile up:

```aw wrap
user-rate-limit:
  max-runs-per-window: 3
  window: 60  # 3 runs per hour per user
```

See [Rate Limiting Controls](/gh-aw/reference/rate-limiting-controls/) and [Concurrency](/gh-aw/reference/concurrency/) for details.

### Use Schedules for Predictable Budgets

Scheduled workflows fire at a fixed cadence, making cost easy to estimate and cap. The less often a workflow runs, the lower the cost:

```aw wrap
# Once per day on weekdays — 5 runs/week
schedule: daily on weekdays
```

```aw wrap
# Every two days — roughly 15 runs/month
schedule: every 2 days
```

```aw wrap
# Weekly on Monday mornings — 4–5 runs/month
schedule: weekly
```

When an event-based trigger fires far more often than the agent actually needs to act, a schedule is almost always cheaper. Replace `push` or `issues` triggers with a daily or weekly schedule and let the agent work through a backlog of items in one run.

See [Schedule Syntax](/gh-aw/reference/schedule-syntax/) for the full fuzzy schedule syntax.

### Batch Instead of Reacting to Events

Reactive triggers like `issues` or `pull_request` launch one agent run per event. When many events arrive in a short window, that adds up quickly. A scheduled batch run groups all pending items into a single invocation — and because the shared system prompt and instructions are sent once for the whole batch, AI providers can cache that context across items, further reducing AI Credits consumption.

```aw wrap
description: Nightly issue triage (replaces reactive issues trigger)
on:
  schedule: daily
  workflow_dispatch:

permissions:
  issues: read
engine:
  id: copilot
  model: gpt-4.1-mini
tools:
  github:
    toolsets: [issues]
---

Fetch all issues opened in the past 24 hours with no labels.
For each issue, apply the most appropriate label. Process them in a single pass.
```

> [!TIP]
> For high-volume repositories, combine a scheduled trigger with [BatchOps](/gh-aw/patterns/batch-ops/) to split work across parallel matrix jobs and stay within per-run token budgets.

### Use Inline Sub-Agents with Smaller Models

When a workflow delegates specialized tasks to sub-agents, each sub-agent can use a different model. Assign cheap, fast models to high-frequency sub-tasks (summarization, labeling, classification) and reserve frontier models only for the orchestrator.

```aw wrap
engine:
  id: copilot
  model: small
permissions:
  pull-requests: read

---

Use the `summarizer` sub-agent to summarize the diff, then post the result as a review comment.

## agent: `summarizer`
---
model: small
description: Summarizes a pull request diff in one paragraph
---
Read the diff and return a single paragraph describing what changed and why.
```

See [Inline Sub-Agents](/gh-aw/reference/inline-sub-agents/) for the full syntax.

### Use Inline Skills to Reduce Context

Move large instruction blocks out of the main prompt body using inline skills. At runtime, each `## skill:` block is extracted and written to engine-specific skill locations — the agent can invoke the skill on demand instead of receiving the guidance upfront, keeping the ambient context slim.

Each block ends at a matching `## end skill: \`name\`` marker if present, or otherwise at the next `##` heading or EOF. Add the explicit end marker when the skill is imported into the middle of a document, so content following it is not swallowed into the skill block.

Treat the main prompt as an execution plan and sub-skills as deferred detail:

- Main prompt: concise plan, sequencing, and decision points.
- Sub-skills: verbose checklists, report templates/layout rules, and domain rubrics.
- Invoke sub-skills only when needed (for example, at final report generation), not at startup.

This progressive-disclosure pattern keeps early turns focused and reduces per-run token overhead:

```aw wrap
engine:
  id: copilot
  model: small
permissions:
  issues: read
tools:
  github:
    toolsets: [issues]

---

Triage the issue using the `triage-rules` skill.

## skill: `triage-rules`
---
description: Classify issues and suggest next actions.
---
Classify by bug / feature / question, identify missing information, and suggest
the smallest actionable next step.
```

> [!TIP]
> Include the `agentic-workflows` tool only in workflows that need self-inspection. Omitting it from unrelated workflows eliminates several hundred tokens of ambient context per run.

## Agentic Cost Optimization

The `agentic-workflows` MCP tool exposes the same operations as the CLI (`logs`, `audit`, `status`) to any workflow agent, so a scheduled meta-agent can inspect and optimize other agentic workflows automatically — fetching aggregate cost data, deep-diving into individual runs, and proposing frontmatter changes (cheaper model, tighter `skip-if-match`, lower `user-rate-limit`) via a pull request.

```aw wrap
description: Weekly Actions minutes cost report
on: weekly
permissions:
  actions: read
engine: copilot
tools:
  agentic-workflows:
```

### What to Optimize Automatically

Target workflows with high AIC per run by moving to a smaller model or reducing context size, cap high turn counts with `max-turns`, tighten `skip-if-match` when runs often produce no safe output, reduce queue pressure with `user-rate-limit.max-runs-per-window` or `concurrency`, and switch overly chatty workflows to `schedule` or `workflow_dispatch`.

> [!NOTE]
> The `agentic-workflows` tool requires `actions: read` permission and is configured under the `tools:` frontmatter key. See [GH-AW as an MCP Server](/gh-aw/reference/gh-aw-as-mcp-server/) for available operations.

## Optimize at Scale with github/agentic-ops

The [githubnext/agentic-ops](https://github.com/githubnext/agentic-ops) repository is the reference implementation for organization-wide agentic workflow monitoring and optimization. It applies the [MonitorOps](/gh-aw/patterns/monitor-ops/) pattern to summarize spend, escalate failures, and propose workflow improvements on a schedule.

## Common Scenario Estimates

These are rough budgeting estimates; actual costs vary by prompt size, tool usage, model, and provider pricing.

| Scenario | Frequency | Actions minutes/month | Inference/month |
|---|---|---:|---|
| Weekly digest (schedule, 1 repo) | 4×/month | ~1 min | Varies by model and prompt size |
| Issue triage (issues opened, 20/month) | 20×/month | ~10 min | Varies by model and prompt size |
| PR review on every push (busy repo, 100 pushes/month) | 100×/month | ~100 min | Varies by model and prompt size |
| On-demand via slash command | User-controlled | Varies | Varies |

> [!TIP]
> Create separate `COPILOT_GITHUB_TOKEN` service accounts per repository or team to attribute spend by workflow.

## Learn More

- [Audit Commands](/gh-aw/reference/audit/) - Single-run analysis, diff, and cross-run reporting
- [Artifacts](/gh-aw/reference/artifacts/) - Artifact names, directory structures, and token usage file locations
- [Triggers](/gh-aw/reference/triggers/) - Configuring workflow triggers and skip conditions
- [Rate Limiting Controls](/gh-aw/reference/rate-limiting-controls/) - Preventing runaway workflows
- [AI Engines](/gh-aw/reference/engines/) - Engine and model configuration
- [FAQ](/gh-aw/reference/faq/) - Common questions about cost and billing
