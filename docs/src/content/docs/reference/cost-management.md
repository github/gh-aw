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

For a concrete model present in gh-aw's embedded pricing catalog, `gh aw compile` includes its provider-specific rates in the firewall configuration. Model parameters such as `?effort=high` are removed for pricing lookup and configuration keys. This keeps AI-credit limits working even when the firewall's built-in pricing table does not yet include the model. Frontmatter `models.providers` pricing takes precedence; compilation does not fetch pricing or infer rates from a related model.

> [!NOTE]
> AIC values are computed on a best-effort basis using pricing data sourced from the [models.dev](https://models.dev/) catalog and may not exactly match your provider's actual billing. Always verify charges in your provider's billing dashboard.

## Cost Components

A typical run includes a short pre-activation job (10–30 seconds) and an agent job (1–15 minutes), each with ~1.5 minutes of runner setup overhead billed at standard [GitHub Actions pricing](https://docs.github.com/en/billing/managing-billing-for-your-products/managing-billing-for-github-actions/about-billing-for-github-actions). Inference is charged by the AI provider based on the engine and API token type used.

## Monitoring Costs with `gh aw logs`

For routed runs, the Model Routing summary separates classifier AIC from selected-model and deviated-traffic cost in each run. Cross-run `--format pretty|markdown` and `--json` output groups task labels and mode by selected model, effort, and router version, with run counts and total/average AIC. Use the selected route to compare the cost of different classification outcomes; classifier cost is routing overhead, while deviated traffic can include sub-agents or overrides.

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

`gh aw logs --json` emits three views of the same data: `.runs[]` for individual workflow runs, `.episodes[]` for runs sharing a recorded canonical identity, and `.edges[]` for recorded parent-child lineage between selected runs in the same episode. Use `.runs[]` to find which run was resource-heavy and `.episodes[]` to measure usage across the selected members of a logical execution.

Episode grouping requires a nonempty propagated `context.episode_id` and preserves that ID, matching OpenTelemetry correlation even when intermediate runs are absent. Runs without canonical identity remain in `.runs[]` and overall summary totals, but do not contribute to `.episodes[]`, episode counts, or edges. Call IDs, matching revision/actor metadata, and `workflow_run` events do not reconstruct episodes. Totals cover only selected members with recorded canonical identity; high confidence indicates lineage evidence, not complete episode coverage. Edges require both runs to share the same canonical ID and connect distinct GitHub run IDs, so reusable workflow hops sharing one run do not produce separate run edges.

Conflicting canonical identities never merge, and uncorrelated runs never bridge episodes. Input ordering does not change episode membership or totals.

Episode root and coverage fields distinguish recorded identity from the selected data:

| Field | Meaning |
|---|---|
| `recorded_roots[]` | Distinct root claims from context: string `run_id`, full `workflow_id`, and `repository` when recorded, including claims without a run ID. `observed` is true only when that root run is selected in this episode without a known repository mismatch. Conflicting claims and original repository spelling are preserved; repository identity comparison ignores case. |
| `representative_run_id`, `representative_workflow` | Selected-data representative, preferring the earliest timestamped run, then the lowest run ID when timestamps are absent or tied. An available workflow name is used when the representative has none. These fields do not identify a missing canonical root. |
| `coverage.scope` | Always `selected_runs`; totals exclude unselected ancestors and descendants. |
| `coverage.status` | `partial` when recorded evidence has gaps, contradictions, invalid run IDs, or collection truncation; otherwise `unknown`. Logs do not establish episode completeness, even when every recorded parent and root is selected. |
| `coverage.reasons` | Machine-readable codes: `missing_recorded_root`, `missing_recorded_parent`, `unverified_root_lineage`, `unverified_parent_lineage`, `conflicting_recorded_roots`, `conflicting_root_lineage`, `conflicting_parent_lineage`, `invalid_recorded_root_run_id`, `invalid_recorded_parent_run_id`, and `collection_truncated`. Unverified lineage means the referenced run is selected but has no canonical episode identity. |
| `coverage.missing_parent_run_ids` | Sorted, unique recorded caller run IDs absent from the selection. Same-run reusable hops are not missing run-level parents. |
| `coverage.collection_truncated` | True when collection truncation applies to this episode's selected repository/workflow target, or a reached count limit cannot be scoped more precisely. A scoped continuation does not downgrade unrelated targets. This is not proof that this particular episode has additional members; false does not establish completeness. |

Missing root fields remain unknown; the CLI does not infer a canonical root from a descendant or parse one from an episode ID. Root presence is run-level evidence, not proof that every reusable workflow hop or artifact was collected.

Both compact and verbose JSON retain all canonical episode and edge details, including single-run episodes. `total_duration` sums execution durations rather than measuring wall-clock time between the first and last hop.

To expand a selected run into a bounded episode selection, use
[`gh aw logs episode`](/gh-aw/setup/cli/#logs-episode-run-id-or-url).
The collector follows recorded parents and roots and scans explicitly allowed
repositories for canonical members within a fixed time window. Candidate-run,
request, and timeout caps are reported in `episode_retrieval`; reaching them
qualifies coverage rather than presenting the observed total as a complete
episode cost.

`episodes[].explanation` separates recorded hops from `run_costs`. Reusable
workflow hops can share a run ID; that run's tokens and AI credits appear once
in the accounting list and are not attributed to individual hops.
`gh aw logs episode <run-id-or-url> --view hops` shows the same evidence and gaps.
Repeated queries reuse the cached snapshot without GitHub requests; use
`--refresh` to recollect rather than treating cached costs as current.

The episode-level `root_run_id` and `primary_workflow` fields have been removed. Migrate selected-data consumers to `representative_run_id` and `representative_workflow`; use `recorded_roots[]` for canonical root claims. Historical data without `context.episode_id` remains available for per-run analysis but is not backfilled into synthetic episodes.

`--count` defaults to 10 even with a date filter. Choose a larger explicit count and inspect `.continuation` or `.continuations[]` before interpreting a date-window report. Continuing a query can recover more selected data, but neither an exhausted cursor nor high lineage confidence proves that all episode descendants were observed.

For usage analysis, focus on `total_runs`, `total_tokens`, `total_aic`, `total_duration`, `representative_workflow`, `resource_heavy_node_count`, and `blocked_request_count`. For Claude, Codex, and Copilot runs, `total_aic` is the preferred cost metric because it maps to provider billing in AI Credits (1 AIC = $0.01 USD).

```bash
# Top 10 costliest selected episodes, from at most 1000 runs in the past 30 days
gh aw logs --start-date -30d --count 1000 --verbose --json | \
  jq '[.episodes[] | {episode: .episode_id, workflow: .representative_workflow, runs: .total_runs, aic: (.total_aic // 0)}]
      | sort_by(.aic) | reverse | .[:10]'

# Top 10 heaviest selected Copilot episodes by AIC
gh aw logs --start-date -30d --count 1000 --engine copilot --verbose --json | \
  jq '[.episodes[] | {episode: .episode_id, workflow: .representative_workflow, runs: .total_runs, aic: (.total_aic // 0)}]
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

### Skip the Agent When It Is Not Needed

The cheapest run is the one that never starts the agent. Use `skip-if-match` and `skip-if-no-match` for simple GitHub-search conditions because they run in the low-cost pre-activation job:

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

When the condition needs scripting — for example calling an API, inspecting a file, or applying custom logic — emit a `noop` to `$GH_AW_SAFE_OUTPUTS` from `steps:` or `pre-agent-steps:`. The harness exits before starting the AI engine, so no AI Credits are charged:

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

Use `skip-if-match` / `skip-if-no-match` whenever possible because they also avoid scheduling the agent job. Use `noop` when you need arbitrary shell logic. See [Triggers](/gh-aw/reference/triggers/) for the full syntax.
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

For task-level model routing, see the [Model Routing guide](/gh-aw/reference/model-routing/) for configuration, and the [Audit](/gh-aw/reference/audit/) page for interpreting classifier overhead, selected-route costs, and deviations in `gh aw audit` and `gh aw logs`.

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

When a configured [admission service](/gh-aw/specs/admission-service-specification/) returns a valid approval, that decision supersedes daily validation for the current activation. The daily scan and its preparatory API work are skipped, but usage collection and historical ledger publication continue. Admission-disabled runs and ignored admission outages still use the daily guardrail below; denial or invalid approval blocks activation. The primary and optional threat-detection/evals allowances are passed to existing jobs and replace their respective runtime credit limits; omitted optional allowances retain the configured component limits.

```aw wrap
max-daily-ai-credits: 15M
```

When the total from the past 24 hours exceeds this threshold, activation reports the exceeded budget, skips the agent job, and lets the conclusion job report the failure context.

When omitted, the guardrail inherits the default daily limit (5000 AI Credits unless overridden). Positive values accept plain integers or `K`/`M` suffixes such as `100M`.

> [!CAUTION]
> Every activation requiring daily fallback lists the current 24-hour window (up to 10 pages of 100 completed runs). A cold scan also reads usage artifacts. Activation publishes all resolved observations, including nonzero usage, so later scans can reuse them without reading each historical artifact again.

Scan snapshots are optional accelerators, not authoritative daily totals. Each record must match the repository, workflow, run attempt, and completed-run timestamps. Missing, expired, and incomplete snapshots require resolving the missing records. The artifact fallback accepts the same workflow's `pull_request_target` runs and default-branch `push`, `schedule`, or `workflow_dispatch` runs; contributor-controlled `pull_request` snapshots are not trusted. This storage path does not require writable Actions cache scopes.

An incomplete history, permanent API failure, or quota restriction stops activation rather than reporting an under-budget result from partial data. Transient reads receive bounded retries; unresolved accounting uses the per-run maximum when available. Respect any logged reset or `Retry-After` deadline before retrying. A missing usage artifact is not zero usage unless complete job metadata confirms that all generated AI-powered jobs were skipped or never assigned a runner, with no executed jobs carried forward from earlier attempts.

Activation logs identify artifact discovery and accounting failures with the underlying error and guidance to check Actions read permissions, artifact retention, and accounting coverage. The activation summary separates recorded usage, confirmed-zero runs, and conservative estimates. The exceeded-guardrail log also reports measured and assumed AIC separately. These totals are guardrail accounting, not provider billing; reconcile charges with the provider.

Cold scans check the compiler-owned `agent`, `detection`, and `evals` jobs independently. Every executed component needs valid accounting; empty collector placeholders and evaluation results alone do not prove zero usage. Raw token usage takes precedence over overlapping summaries. Invalid numeric fields make the accounting unknown. Failed-only reruns retain successful components from earlier attempts, but a rerun component needs a producer artifact from its own job execution. Missing carried-forward usage remains unknown. Older snapshots without component-coverage validation are not reused.

#### Repo-Memory Bootstrap

With `max-daily-ai-credits.backend: repo-memory`, a missing memory branch does not require an insecure override. After the clone step confirms the branch is absent, activation checks authoritative workflow history and usage artifacts under the same daily limit. This requires Actions read access and fails closed on incomplete accounting or API restrictions.

Recompile existing workflows after upgrading so activation receives the clone result required for secure bootstrap.

The trusted memory writer records when ledger coverage starts. New branches and existing ledgers without coverage metadata continue using artifact accounting until that coverage is at least 24 hours old; subsequent activations use the trusted cloned ledger. This prevents a new empty ledger from erasing recent spending. Preserve the coverage files under `daily-aic-ledger/` with the JSONL shards. Do not enable `GH_AW_ALLOW_INSECURE_REPO_MEMORY_AIC` or `continue-on-error` to bootstrap.

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

### Configure Advanced Cost Controls with Admission

Use an admission authority when static per-run and daily limits are not enough—for example, to assign custom AIC allowances by server size or evaluate shared organization budgets before starting an agent. gh-aw implements the client, not a hosted policy server. Deploy or obtain access to an authority that implements the [admission service contract](/gh-aw/specs/admission-service-specification/); policy configuration and trusted usage accounting belong to that service.

#### Enable the Admission Gate

1. Opt in for an individual workflow by adding `admission: true` alongside its existing triggers:

   ```aw wrap
   on:
     workflow_dispatch:
     admission: true
   ```

   Alternatively, set `"admission": true` in the existing `.github/workflows/aw.json` object to opt in repository-wide. Preserve other settings.

   > [!IMPORTANT]
   > A workflow-level `false` cannot override repository-level `true`.

2. Recompile affected workflows with `gh aw compile` and commit the updated Markdown or repository configuration and generated `.lock.yml` files. Admission jobs are omitted by default; setting an endpoint alone does not add them.

3. Export existing managed defaults before editing them:

   ```bash
   gh aw env get defaults.yml --scope org --org MY_ORG
   ```

   Preserve the exported fields and add these settings, replacing the example URL with the deployed authority:

   ```yaml
   default_admission_url: https://admission.example.org/api/admission
   default_admission_audience: https://admission.example.org/api/admission
   default_admission_on_outage: block
   ```

   Apply the file:

   ```bash
   gh aw env update defaults.yml --scope org --org MY_ORG
   ```

   For one repository, use `--scope repo --repo OWNER/REPO` on both commands instead. Omitted or null fields delete existing managed variables, so do not apply a partial defaults file.

The settings map to Actions variables `GH_AW_DEFAULT_ADMISSION_URL`, `GH_AW_DEFAULT_ADMISSION_AUDIENCE`, and `GH_AW_DEFAULT_ADMISSION_ON_OUTAGE`. Repository variables override organization variables. For enterprise rollout, provision each enrolled organization. The URL must be an absolute HTTPS `/api/admission` URL without credentials, query parameters, or fragments. The audience defaults to that URL when unset.

For opted-in workflows, changes to these runtime variables do not require recompilation. An empty URL skips admission and retains existing local guardrails. `block` stops activation after an exhausted transient outage; the default, `ignore`, continues with existing local controls. Explicit denial and invalid approval block under either policy.

The compiler grants `id-token: write` only to the admission-related jobs for this feature. Hand-authored reusable-workflow callers must also allow that permission. Authentication uses GitHub Actions OIDC; do not send PATs, provider tokens, or GitHub App private keys to the authority. The completion callback uses `/api/conclusion` on the same origin.

#### Assign AIC Allowances by Server Size

Configure the size-to-allowance mapping in the authority's protected policy, not in workflow frontmatter. For example, an operator might choose these illustrative primary-agent limits:

| Server size policy tier | Primary allowance (AIC) |
|---|---|
| Small | 250 |
| Medium | 1000 |
| Large | 2500 |

These are example policy values, not built-in tiers or recommended budgets. The admission request does not include server or runner size. Maintain a trusted mapping from verified repository/workflow identity to a size tier, or resolve the tier from independently verified deployment metadata. Do not add a `server-size` request field or trust caller-supplied size hints. A larger runner does not automatically grant more AI Credits; Actions compute charges remain separate.

For the medium tier, the authority could return `200 OK` with `content-type: application/json`, `cache-control: no-store`, and:

```json
{
  "max_ai_credits": 1000,
  "max_ai_credits_threat_detection": 100,
  "max_ai_credits_evals": 50
}
```

`max_ai_credits` is required. The threat-detection and evals fields are independently optional; omitted fields retain the existing component limits. Each supplied allowance must be a plain positive JSON integer from `1` through `2147483647` (the maximum signed 32-bit integer), not a string, decimal, exponent, or unlimited sentinel. The allowances replace their respective runtime limits—even if higher than the workflow's configured limit—and are not a pooled budget.

Valid approval supersedes the local daily-credit check for that activation, so implement any required shared-budget checks in the authority. Keep size-tier allowances independent of hidden remaining balances; exhausted budgets produce a uniform empty `403`, not a reduced remaining-balance response. Monitor subsequent usage with `gh aw logs` and `gh aw audit` and reconcile provider billing separately.

> [!WARNING]
> Admission is cooperative, not a hard spending cap. Workflow editors and repository-variable administrators can bypass participation, and concurrent executions can overshoot shared budgets. Hard quotas, reservations, billing settlement, and mid-run cancellation require independently enforced backend controls; enabling admission does not supply them.

For advanced controls without native gh-aw settings, use this admission integration as the extension point and implement the supported start-time policy in the external authority. Do not invent frontmatter options or describe unsupported billing or enforcement features as available. See the [admission specification](/gh-aw/specs/admission-service-specification/) for identity verification, retry safety, accounting, and response requirements.

### Roll Out Shared Defaults

For large installations, set baseline model and token guardrails once and let individual workflows override them only when needed:

1. Export current defaults:

```bash
gh aw env get defaults.yml --scope org --org MY_ORG
```

2. Update and apply shared defaults:

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

`gh aw env update` previews changes before applying them. Use `--yes` to skip confirmation in automation or `--dry-run` to preview only. Set a field to `null` to delete the corresponding variable. Unknown YAML keys are rejected. `default_max_turns`, `default_timeout_minutes`, `default_agent_job_timeout_minutes`, and `default_detection_job_timeout_minutes` must be positive integers; `default_max_ai_credits` and `default_max_daily_ai_credits` must be non-zero integers. Negative values disable the corresponding guardrail.

3. If you compile workflows in CI, pass compiler-read defaults into the compiler process environment, for example via `${{ vars.* }}`: `GH_AW_DEFAULT_MAX_TURNS`, `GH_AW_DEFAULT_MAX_TURN_CACHE_MISSES`, and `GH_AW_DEFAULT_DETECTION_MODEL`.

> [!TIP]
> `GH_AW_DEFAULT_MODEL_*` and `GH_AW_DEFAULT_*_TIMEOUT_MINUTES` are resolved at workflow runtime via `${{ vars.* }}` in compiled YAML, while max-turn and token defaults are read by the compiler process at compile time.
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

Move large instruction blocks out of the main prompt body using inline skills. At runtime, each `## skill:` block is extracted and written to engine-specific skill locations, so the agent can invoke the skill on demand instead of receiving the guidance upfront.

Each block ends at a matching `## end skill: \`name\`` marker if present, or otherwise at the next `##` heading or EOF. Add the explicit end marker when the skill is imported into the middle of a document so following content is not swallowed into the skill block.

Treat the main prompt as an execution plan and sub-skills as deferred detail: keep the main prompt concise for sequencing and decisions, move verbose checklists and report templates into sub-skills, and invoke those sub-skills only when needed. This progressive-disclosure pattern keeps early turns focused and reduces per-run token overhead:

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

These rough estimates help with early budgeting; actual costs vary by prompt size, tool usage, model, and provider pricing.

| Scenario | Frequency | Actions minutes/month | Inference/month |
|---|---|---:|---|
| Weekly digest (schedule, 1 repo) | 4×/month | ~1 min | Varies by model and prompt size |
| Issue triage (issues opened, 20/month) | 20×/month | ~10 min | Varies by model and prompt size |
| PR review on every push (busy repo, 100 pushes/month) | 100×/month | ~100 min | Varies by model and prompt size |
| On-demand via slash command | User-controlled | Varies | Varies |

> [!TIP]
> Use separate `COPILOT_GITHUB_TOKEN` service accounts per repository or team to attribute spend by workflow.
## Learn More

- [Audit Commands](/gh-aw/reference/audit/) - Single-run analysis, diff, and cross-run reporting
- [Artifacts](/gh-aw/reference/artifacts/) - Artifact names, directory structures, and token usage file locations
- [Triggers](/gh-aw/reference/triggers/) - Configuring workflow triggers and skip conditions
- [Rate Limiting Controls](/gh-aw/reference/rate-limiting-controls/) - Preventing runaway workflows
- [AI Engines](/gh-aw/reference/engines/) - Engine and model configuration
- [FAQ](/gh-aw/reference/faq/) - Common questions about cost and billing
