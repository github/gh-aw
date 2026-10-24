---
description: Guide for reducing token consumption in agentic workflows — DataOps, gh-proxy, inline sub-agents, caveman experiments, and audit-based measurement.
---

# Token Consumption Optimization

If a task can be solved using deterministic tools, use deterministic tools. Only use agents when necessary, as they incur higher cost. Agentic workflows allow you to run deterministic tools first and gate agent execution using conditions, enabling workflows that avoid triggering agents most of the time and only use them when needed.

For advanced cost controls without native settings, such as custom AIC allowances by server size or shared organization budget policies, follow [Configure Advanced Cost Controls with Admission](https://github.github.com/gh-aw/reference/cost-management/#configure-advanced-cost-controls-with-admission) (source: `docs/src/content/docs/reference/cost-management.md`). Route these requests to that setup and server-side policy guidance rather than inventing frontmatter options. Admission requires an external authority and is cooperative; hard caps, reservations, billing settlement, and mid-run cancellation require independent backend controls.

## Quick-Reference Checklist

Apply these in order, measuring cost and quality after each change:

- [ ] **Cheap triage first**: classify duplicates, stale items, low-value events, and known cases before escalating
- [ ] **Frontier model as planner**: use frontier models for planning, synthesis, ambiguous decisions, and final judgment — not bulk extraction
- [ ] **DataOps**: Move data fetching into `steps:` — agent reads compact JSON, not raw API responses
- [ ] **gh-proxy**: Set `tools.github.mode: gh-proxy` — skips Docker MCP server startup and extra tool definitions
- [ ] **cli-proxy**: Mount additional MCP servers as CLIs via `cli-proxy: true` — agent pipes output through `jq` before it enters context
- [ ] **Sub-agents**: Delegate repetitive per-item tasks to `model: small` sub-agents (~10–20× cheaper)
- [ ] **Sub-skills (inline `## skill:` blocks)**: Keep the main prompt as a short execution plan; move detailed playbooks, output templates, and formatting rules into `## skill:` blocks — the runtime extracts these before the first model call, so they are available on demand without entering the initial request context
- [ ] **Prompt size**: Strip redundant instructions, examples, and pleasantries from the prompt body
- [ ] **Dynamic context**: Inject only required fields — `${{ github.event.issue.number }}` not the full event payload
- [ ] **Pull context on demand**: query logs/data only after a hypothesis forms; avoid preloading large raw dumps into the initial prompt
- [ ] **Bound file reads**: for files > 20 KB, use `bash`/`grep`/`glob`/`view_range` instead of full-file MCP reads — late-session token spikes most often trace to unguarded `get_file_contents` calls on large workflow or skill markdown
- [ ] **Prompt caching**: Put stable instructions before dynamic content to maximize cache hits
- [ ] **Context hygiene**: keep the orchestrator context compact; prefer short worker summaries over raw output
- [ ] **Harness-wide diagnosis**: classify failures across context, tools, generation, orchestration, memory, and output processing before changing configuration
- [ ] **Execution experience**: retain compact diagnoses and outcomes, then reuse recurring patterns instead of restarting optimization from scratch
- [ ] **Correctness first**: compare quality before cost; use AIC or token count only to choose among equally successful variants
- [ ] **Cadence**: If the result is not time-sensitive, schedule less often (`hourly` → `daily`, `daily` → `weekly`)
- [ ] **Batching**: Prefer scheduled batch processing over reactive events when delayed processing is acceptable
- [ ] **Bounded subsets**: For large repetitive backlogs, process only a budget-safe subset per run and use a cache cursor or deterministic heuristic to rotate fairly through the remaining work
- [ ] **Telemetry**: Configure `observability.otlp` so token usage and run phases are measurable outside individual run logs
- [ ] **AgenticOps**: Add `copilot-token-audit` / `copilot-token-optimizer` workflows so the repository keeps finding waste automatically
- [ ] **Measure first**: Back every change with an `experiments:` field and `metric: "aic"` before promoting
- [ ] **Budget increase last**: Increase `max-ai-credits` only after all applicable optimizations above have been exhausted and measured

---

## Frontier-Model Cost Pattern

A frontier model can reduce **total** cost when architecture prevents unnecessary invocations and keeps expensive context narrow.

- use frontier model for planning, hypothesis selection, synthesis, ambiguous decisions, final judgment
- do not spend frontier turns on repetitive extraction, duplicate detection, or broad first-pass scanning
- add a cheap triage stage for known/duplicate/stale/low-value events; stop with `noop` when escalation is unnecessary
- escalate to frontier model only when triage is uncertain or the case is genuinely new/high-value
- cap sub-agent fan-out so escalations cannot recurse without bound

Cost wins come from architecture and selective execution, not model tier alone.

---

## Pull Context, Do Not Push Context

Avoid front-loading large raw context when data can be fetched on demand. Prefer deterministic pre-steps that materialize compact files under `/tmp/gh-aw/`, `gh` + filtering (`jq`, `grep`) before context reaches the model, pre-aggregated summaries over full API payloads, and directed tool calls issued only after the agent forms a hypothesis. Anchoring warning: preselecting raw logs too early can make the model over-focus and miss the actual cause.

---

## How to Measure Token Usage

`gh aw audit` reports per-run cost. See [cli-commands.md](cli-commands.md#gh-aw-audit) for full command syntax (single run, `--json`, multi-run diff) and the MCP `audit` equivalent.

Token-specific fields in `gh aw audit <run-id> --json`:

- `agent_usage.aic` — AI Credits (AIC), the normalized cost metric (1 AIC = $0.01; accounts for model price differences and cache discounts)
- `agent_usage.input_tokens` / `agent_usage.output_tokens` — raw token counts
- `agent_usage.cache_read_tokens` / `agent_usage.cache_write_tokens` — tokens served from the prompt cache

For per-call detail, `gh aw audit <run-id>` downloads artifacts into `logs/run-<run-id>/`; read `firewall-audit-logs/api-proxy-logs/token-usage.jsonl` (one API call per line, with `model` and token counts) to find the most expensive calls. Diff two runs with `gh aw audit <base-id> <optimized-id>` to detect AI-credit regressions.

Treat optimization as successful only when quality remains acceptable. A quality regression is a failure even if AI Credits decrease.

---

## Technique 1 — DataOps: Move Compute to Steps

The single biggest optimization. Replace agentic data fetching with deterministic shell commands in `steps:`. Shell steps run outside the AI sandbox (no tokens) and produce structured output the agent reads directly.

### Before (agent does all the work)

```markdown
---
engine: copilot
tools:
  github:
    mode: gh-proxy
    toolsets: [default, pull_requests]
---

Fetch all open PRs in ${{ github.repository }}, compute the merge rate, identify authors with the most contributions, and create a weekly summary discussion.
```

### After (DataOps pattern)

```markdown
---
engine: copilot
tools:
  github:
    mode: gh-proxy
  bash: ["*"]

steps:
  - name: Fetch and aggregate PR data
    env:
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
    run: |
      mkdir -p /tmp/gh-aw/data
      gh pr list --repo "${{ github.repository }}" \
        --state all --limit 100 \
        --json number,title,state,author,createdAt,mergedAt,additions,deletions \
        > /tmp/gh-aw/data/prs.json

      jq '{
        total: length,
        merged: [.[] | select(.state=="MERGED")] | length,
        open: [.[] | select(.state=="OPEN")] | length,
        top_authors: ([.[].author.login] | group_by(.) | map({author:.[0], count:length}) | sort_by(-.count) | .[0:5])
      }' /tmp/gh-aw/data/prs.json > /tmp/gh-aw/data/stats.json

safe-outputs:
  create-discussion:
    title-prefix: "[weekly-pr] "
    category: "General"
    close-older-discussions: true
---

Read the pre-computed stats at `/tmp/gh-aw/data/stats.json` and `/tmp/gh-aw/data/prs.json`.
Create a concise weekly PR summary discussion.
```

**Best practices:**

- One JSON file per data source; `jq` to pre-aggregate
- Store files under `/tmp/gh-aw/`
- Document file locations and schema in the prompt body so the agent doesn't need to explore

---

## Technique 2 — Use `gh-proxy` and `cli-proxy` Instead of the MCP Server

### `mode: gh-proxy` (GitHub reads)

```yaml
tools:
  github:
    mode: gh-proxy      # ✅ preferred — pre-authenticated gh CLI, no MCP server startup
    toolsets: [default]
```

Agent reads GitHub via `gh issue list`, `gh pr view`, etc. and pipes through `jq` before data enters context. `mode: local` starts a Docker-based MCP server with startup latency and verbose tool results.

### `cli-proxy: true` (other MCP servers as CLIs)

When a workflow uses additional MCP servers (e.g., a custom Notion or Slack MCP), `cli-proxy: true` mounts each server as a standalone CLI tool on `PATH`:

```yaml
tools:
  cli-proxy: true
  github:
    mode: gh-proxy
  my-custom-mcp:
    ...
```

With `cli-proxy`, the agent calls `my-custom-mcp <tool> <args>` from bash and pipes output through `jq`/`grep` to extract only needed fields — instead of receiving the full MCP tool response in context.

**Summary:**

| Mode | Docker startup | Extra tool definitions | Agent output processing |
|---|---|---|---|
| `mode: local` + MCP tools | Yes | Yes | Tool result (full JSON) |
| `mode: gh-proxy` + bash | No | No | Agent pipes through jq |
| `cli-proxy: true` + bash | Yes (once) | Reduced | Agent pipes through jq |

---

## Technique 3 — Inline Sub-Agents with Smaller Models

Sub-agents with `model: small` cost 10–20× less than the parent model. Use them for classification, one-sentence summarization, structured extraction, and scoring; reserve the large model for synthesis.

### Pattern

```
steps:        → deterministic shell (zero AI tokens)
sub-agents:   → small model per item (cheap, parallelizable)
main agent:   → synthesizes compact sub-agent results (one high-quality pass)
```

### Example (skeleton — see [subagents.md](subagents.md) for full syntax)

A shell step splits issues into per-item files; the main prompt dispatches a `model: small` sub-agent per file and synthesizes the compact results:

```markdown
## agent: `classifier`
---
description: Classifies a GitHub issue into a single category
model: small
---
Read the JSON file provided. Return only:
`{"number": <n>, "category": "bug|feature|question|docs|security|other"}`
Nothing else.
```

**Why this saves tokens:** sub-agents run the cheap `small` model; main agent reads only compact `{"number":…, "category":…}` JSON; dispatches run in parallel.

### Pair sub-agents with sub-skills (progressive disclosure)

- Keep the main prompt short and plan-like (what to do, in what order).
- Put verbose instructions (report layout, rubric details, formatting constraints) into `## skill:` blocks.
- Invoke skills only when needed (e.g., producing final output), so early turns stay lean.

This delays expensive instruction payloads until the final phase, lowering ambient context. See [subagents.md](subagents.md) for full syntax.

**Sub-agent model aliases:**

| Alias | Use when |
|---|---|
| `small` | Classification, extraction, one-sentence summaries, scoring |
| `large` | Complex reasoning, multi-step synthesis, code generation |
| `inherited` | Sub-agent needs same capability as the parent (default) |

Always use aliases, not model IDs — aliases resolve to the best available model per provider.

---

## Technique 3b — Inline Skills for Delayed Instruction Loading

Large output templates, formatting rubrics, and phase-specific playbooks are often included verbatim in the workflow prompt body even though they are only needed when the agent is about to produce output. Moving them into `## skill:` blocks keeps the initial request lean while still making the content available on demand.

The gh-aw runtime extracts `## skill:` blocks from the prompt before the first model call and stores them at engine-specific skill locations. The agent retrieves a skill only when it explicitly needs that guidance — the content does not appear in the ambient context of early turns.

### When to use inline skills

Use `## skill:` blocks for content that:

- is only needed in the final output phase (issue body templates, report formats, discussion templates)
- describes a specific sub-task rubric (scoring criteria, formatting rules, classification guides)
- is verbose (> ~500 characters) and not required to understand the task

Keep in the main prompt body anything the agent needs from the very first turn: task goal, inputs, decision criteria, tool guidance.

### Pattern

````markdown
---
engine: copilot
---

Analyze the run logs. For each finding that meets threshold, create a GitHub issue using the `report-issue-template` skill. Record each created issue in `known-issues.json`.

## skill: `report-issue-template`
---
description: Issue title, body structure, and known-issues recording format.
---

**Title**: `[my-workflow] <finding-title>`

**Body**:

```markdown
### Finding: <title>

**Severity**: ...

...full template...
```
````

### Technique scope

Prefer inline skills over separate `.github/aw/*.md` shared files when the content is only relevant to one workflow. Use a shared import (see [reuse.md](reuse.md)) when the same template is used by multiple workflows.

---

## Technique 4 — Apply the Caveman Technique

A/B compare a verbose prompt against a minimal one. Adopt minimal if quality holds.

```yaml
experiments:
  prompt_style: [verbose, minimal]
```

```markdown
{{#if experiments.prompt_style == "verbose" }}
Please analyze all of the open issues in this repository and provide a comprehensive, detailed report covering: the number of open issues, any significant trends or patterns you observe, the most frequently occurring labels, the oldest unresolved issues, a prioritized list of the most critical items, and any recommendations for the team.
{{#else}}
List open issues by priority. Top 5 critical items. Be brief.
{{/if}}
```

Measure AIC via run summary or `gh aw audit`. If `minimal` wins on cost at acceptable quality, promote as baseline.

---

## Technique 5 — Use Experiments to Measure Impact

Declare an experiment before making any prompt or configuration change, and compare before/after cost and quality. Run ≥ 20 cycles per variant for statistical significance on high-frequency workflows.

```yaml
experiments:
  optimization_v1:
    variants: [control, optimized]
    description: "DataOps refactor — move issue fetching to steps:"
    metric: "aic"
    issue: "123"
```

Reference the active variant in the prompt:

```markdown
{{#if experiments.optimization_v1 == "optimized" }}
Read the pre-fetched data from `/tmp/gh-aw/data/`.
{{#else}}
Fetch open issues from ${{ github.repository }} using the GitHub tools.
{{/if}}
```

**After enough runs:**

1. Compare variants using `gh aw audit <control-run-id> <optimized-run-id>`
2. Inspect `aic`, `input_tokens`, `output_tokens`, `cache_read_tokens`, and `cache_write_tokens`
3. Validate output quality and decision accuracy against the control run
4. If the optimized variant wins on cost **and** quality, rewrite the baseline prompt and remove the `experiments:` field. See [experiments.md](experiments.md) for A/B testing details.

**Key experiment dimensions for token optimization:**

| Dimension | Example variants |
|---|---|
| Prompt verbosity | `verbose` / `concise` / `minimal` |
| Data source | `agentic-fetch` / `dataops-steps` |
| Model tier | Run separate workflows for each engine |
| Sub-agent usage | `single-agent` / `with-subagents` |
| Tool mode | `mcp-local` / `gh-proxy` |

---

## Technique 6 — Reduce Trigger Frequency and Batch Work

The cheapest run is the one you don't execute. If a workflow doesn't need near-real-time feedback, run it less often and batch.

### Prefer slower schedules when latency is acceptable

- `hourly` → `daily on weekdays` for team-facing summaries or audits
- `daily` → `weekly` for trend reports, optimization reviews, backlog hygiene
- `every N hours` → daily/weekly batch when the workflow only produces guidance

### Prefer scheduled batches over reactive triggers

Reactive triggers (`issues:`, `pull_request:`, comment commands) suit immediate feedback. Otherwise prefer `schedule: daily on weekdays` and batch work. Typical batch-friendly tasks: triage summaries, stale backlog review, token audits, security digests. Combine with `cache-memory` or `repo-memory` to track processed items.

### Bound repetitive work to a manageable subset

Do not require one run to finish an unbounded backlog such as hundreds of lint violations. Set a per-run item, time, turn, or AI-credit budget and stop after a useful subset. Persist a compact cursor or processed-item set in `cache-memory` when stable state is available; otherwise use a deterministic heuristic such as file-path buckets, issue-number modulo, or oldest-first ordering. Rotate buckets round-robin across runs so every item eventually receives attention without repeatedly selecting the easiest items.

Keep each batch idempotent, skip items already fixed, and report the processed subset plus remaining work. Prefer smaller complete batches over a broad set of partial fixes that may exhaust the budget.

---

## Techniques 7–8 — Observability and Harness Learning

See [token-optimization-observability.md](token-optimization-observability.md) for OpenTelemetry export, AgenticOps token workflows, and learning from harness execution experience.

---

## Techniques 9–11 — Caching, AI-Credit Guardrails, and Bounded File Reads

See [token-optimization-caching-budgets.md](token-optimization-caching-budgets.md) for prompt-caching mechanics, `max-ai-credits`/`max-daily-ai-credits` guardrails, custom model pricing, and the 20 KB bounded-file-read rule.

---

## Additional Resources

| Topic | File |
|---|---|
| OpenTelemetry export, AgenticOps, harness-experience learning | [token-optimization-observability.md](token-optimization-observability.md) |
| Prompt caching, AI-credit guardrails, bounded file reads | [token-optimization-caching-budgets.md](token-optimization-caching-budgets.md) |
| Inline sub-agents syntax | [subagents.md](subagents.md) |
| A/B experiments | [experiments.md](experiments.md) |
| Persistent memory | [memory.md](memory.md) |
| DataOps pattern | [DataOps guide](https://github.com/github/gh-aw/blob/main/docs/src/content/docs/patterns/data-ops.md) |
| Audit command reference | [cli-commands.md](cli-commands.md) |
| Frontmatter syntax | [syntax.md](syntax.md) |
