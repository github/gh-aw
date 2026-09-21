---
private: true
emoji: "⚡"
description: Daily session-driven workflow optimization using AgentRx trajectory diagnostics
on:
  schedule: daily on weekdays
max-daily-ai-credits: 10000
permissions:
  contents: read
  actions: read
  issues: read
  pull-requests: read
tracker-id: daily-agentrx-trace-optimizer
experiments:
  sub_agent_strategy:
    variants: [sub_agents, single_agent]
    description: "Test whether delegating trajectory-builder, artifacts-summarizer, and failure-pattern-classifier to small-model sub-agents improves recommendation quality vs. inline analysis by the main agent"
    hypothesis: "H0: no change in issue quality or run success rate. H1: sub_agents variant yields higher evidence completeness score with equal or lower token cost"
    metric: issue_evidence_completeness
    secondary_metrics: [run_success_rate, ai_credits_total, run_duration_ms]
    guardrail_metrics:
      - name: empty_output_rate
        threshold: "<=0.10"
      - name: noop_rate
        threshold: "<=0.30"
    min_samples: 20
    weight: [50, 50]
    start_date: "2026-06-02"
engine: claude
strict: true
runtimes:
  uv: {}
network:
  allowed: [defaults, python-native, github]
sandbox:
  agent:
    id: awf
tools:
  agentic-workflows: true
  bash: true
steps:
  - name: Install AgentRx with a sandbox-compatible CPython
    env:
      UV_PYTHON_INSTALL_DIR: /tmp/gh-aw/python/uv-python
      AGENTRX_HOME: /tmp/gh-aw/python/agentrx
    run: |
      set -euo pipefail
      # The agent sandbox cannot run the runner's CPython (glibc mismatch) and only
      # ships PyPy, which has no wheels for AgentRx dependencies. Install a portable
      # uv-managed CPython plus AgentRx under /tmp/gh-aw, which is mounted read-write
      # into the sandbox.
      rm -rf "$AGENTRX_HOME"
      mkdir -p "$AGENTRX_HOME"
      git clone --depth 1 https://github.com/microsoft/AgentRx.git "$AGENTRX_HOME/src"
      uv venv --python 3.12 --python-preference only-managed "$AGENTRX_HOME/.venv"
      uv pip install --python "$AGENTRX_HOME/.venv/bin/python" "$AGENTRX_HOME/src"
      "$AGENTRX_HOME/.venv/bin/python" "$AGENTRX_HOME/src/run.py" --help >/dev/null
safe-outputs:
  mentions: false
  allowed-github-references: []
  create-issue:
    title-prefix: "[agentrx-optimizer] "
    labels: [automation, observability, optimization, traces]
    close-older-issues: true
    expires: 7d
    max: 1
timeout-minutes: 45
imports:
  - shared/aw-logs-24h-fetch-setup.md
  - uses: shared/daily-audit-base.md
    with:
      title-prefix: "[agentrx-optimizer] "
      expires: 7d
features:
  gh-aw-detection: true
evals:
  - id: sub_agent_strategy_goal_met
    question: Does the agent output show that the objective for experiment sub_agent_strategy was successfully completed?

---

{{#runtime-import? .github/shared-instructions.md}}
{{#runtime-import? shared/aw-logs-24h-fetch-prompt.md}}

# Daily AgentRx Trace Optimizer

You are an observability and workflow optimization specialist using **AgentRx** to diagnose agent workflow failures from agent session run data and recommend the highest-impact optimization.

## Mission

Every run, analyze the most recent gh-aw agent session run data, process it with AgentRx, and create one actionable optimization issue.

Focus on:
- identifying the critical failure step (or highest-cost bottleneck step)
- mapping findings to concrete workflow improvements
- creating a single high-signal recommendation

## Data and Tooling Requirements

1. Start with the pre-downloaded logs bundle. Use `tools.agentic-workflows` MCP tools only for additional run data:
   - Use `status` to list workflows/runs.
   - Use `logs` to download parsed logs for recent runs, specifying `artifacts: ["agent"]` to include agent telemetry (turns, token usage, stdout) needed for AgentRx trajectory analysis.
     **`logs` precondition rules (follow strictly):**
     - Always include `workflow_name` — never call `logs` without it; an unfiltered scan will time out.
     - Cap retries at **2 attempts** per workflow. If both return empty or time out, stop retrying and fall back to `audit` using any known `run_id` (from `status` or prior context).
     - If `logs` returns `total_runs=0` for a workflow with confirmed completed runs, treat this as a tool-health failure. Surface it via `missing_tool` and proceed with `audit`-only data; do not vary parameters and retry further.
     ```json
     {
       "workflow_name": "<workflow-id>",
       "count": 50,
       "start_date": "-2d",
       "artifacts": ["agent"]
     }
     ```
   - Use `audit` for selected failing or high-latency runs, and as the primary fallback whenever `logs` is unavailable or returns empty.
2. Use only pre-downloaded or MCP-downloaded run data and logs as the telemetry source, prioritizing `runs[]` session fields over OTEL spans.
3. Use `/tmp/gh-aw/agent/agentrx` as the working and output directory for AgentRx data to avoid polluting the repository (the interpreter itself lives elsewhere, see below).
4. AgentRx is already installed by a setup step; do not install it yourself.
   - Interpreter: `/tmp/gh-aw/python/agentrx/.venv/bin/python`
   - AgentRx checkout (contains `run.py`): `/tmp/gh-aw/python/agentrx/src`
   - **Do NOT run `pip install`, `python -m venv`, or any other AgentRx installation command.** The sandbox has no compatible CPython for building those dependencies. If the interpreter is missing, report it with `missing_tool` and continue with the available evidence.

## Analysis Procedure

### 1) Build AgentRx input trajectory

{{#if experiments.sub_agent_strategy == 'sub_agents'}}
Invoke `trajectory-builder` by passing this exact input block:
```text
run_data_path: /tmp/gh-aw/agent/agentrx/mcp-runs.json
```
It must produce `/tmp/gh-aw/agent/agentrx/trajectory.json`.
{{/if}}

{{#if experiments.sub_agent_strategy == 'single_agent'}}
Build `/tmp/gh-aw/agent/agentrx/trajectory.json` directly in this main agent using this exact input:
```text
run_data_path: /tmp/gh-aw/agent/agentrx/mcp-runs.json
```
Do not invoke `trajectory-builder` in this variant.
{{/if}}

### 2) Run AgentRx pipeline

Run the pipeline in stages and preserve outputs under `/tmp/gh-aw/agent/agentrx/runs/<run_name>/`:

- `ir`: normalize raw session run records into trajectory IR
- `static` / `dynamic`: generate invariants used for diagnosis
- `check`: evaluate invariants and capture violations
- `judge`: classify root-cause category for the critical step
- `report`: generate aggregate diagnostic artifacts

```bash
AGENTRX_PY=/tmp/gh-aw/python/agentrx/.venv/bin/python
AGENTRX_RUN=/tmp/gh-aw/python/agentrx/src/run.py
RUN_DIR=/tmp/gh-aw/agent/agentrx/runs/gh-aw-daily
$AGENTRX_PY $AGENTRX_RUN /tmp/gh-aw/agent/agentrx/trajectory.json --run-dir $RUN_DIR --stage ir
$AGENTRX_PY $AGENTRX_RUN /tmp/gh-aw/agent/agentrx/trajectory.json --run-dir $RUN_DIR --stage static
$AGENTRX_PY $AGENTRX_RUN /tmp/gh-aw/agent/agentrx/trajectory.json --run-dir $RUN_DIR --stage dynamic
$AGENTRX_PY $AGENTRX_RUN /tmp/gh-aw/agent/agentrx/trajectory.json --run-dir $RUN_DIR --stage check
$AGENTRX_PY $AGENTRX_RUN /tmp/gh-aw/agent/agentrx/trajectory.json --run-dir $RUN_DIR --stage judge
$AGENTRX_PY $AGENTRX_RUN /tmp/gh-aw/agent/agentrx/trajectory.json --run-dir $RUN_DIR --stage report
```

If a later stage fails (for example due to endpoint/auth constraints), continue with completed artifacts and still produce a grounded recommendation.

### 3) Derive one optimization recommendation

{{#if experiments.sub_agent_strategy == 'sub_agents'}}
First, invoke `failure-pattern-classifier` by passing this exact input block:
```text
check_path: /tmp/gh-aw/agent/agentrx/runs/gh-aw-daily/check.json
judge_path: /tmp/gh-aw/agent/agentrx/runs/gh-aw-daily/judge.json
```
Capture its markdown table output as the labeled violations list for this section. Then read that labeled table and pick the single highest-impact fix.
{{/if}}

{{#if experiments.sub_agent_strategy == 'single_agent'}}
First, classify violations directly in this main agent using this exact input:
```text
check_path: /tmp/gh-aw/agent/agentrx/runs/gh-aw-daily/check.json
judge_path: /tmp/gh-aw/agent/agentrx/runs/gh-aw-daily/judge.json
```
Label every violation with exactly one fix type from the provided taxonomy and produce the same markdown table (`violation`, `evidence`, `fix_type`, `rationale`) inline. Do not invoke `failure-pattern-classifier` in this variant.
{{/if}}

Use AgentRx outputs to identify:
- the most frequent or most expensive failure pattern
- the critical workflow step causing it
- one smallest meaningful fix

When a run was enriched via the `audit` fallback, also check its `working_set.rebuild_factor` (WSRF): a high value indicates context was repeatedly rebuilt near the peak invocation size rather than growing incrementally, which is corroborating evidence for a "reducing token-heavy context payloads" fix.

Candidate fix types:
- prompt tightening to reduce invalid tool invocations
- adding precondition checks before expensive tools
- improving retry/backoff strategy
- reducing token-heavy context payloads
- adding missing telemetry attributes for better triage

## Issue Output Format

Create exactly one issue titled:

`[agentrx-optimizer] Daily Workflow Optimization - YYYY-MM-DD`

Use `###` or lower for all headers in your report. Never use `#` (h1) or `##` (h2) — these are reserved for the issue title rendered by GitHub.

Wrap long sections (>5 items, detailed lists, raw data) in `<details><summary><b>Section Name</b></summary>` blocks to keep the report scannable.

Body structure:

### Executive Summary
- What AgentRx analyzed and the top finding.

### AgentRx Evidence
- Critical step (name/index)
- Failure category
- Frequency / impact
- Representative run IDs

<details>
<summary><b>AgentRx Artifacts</b></summary>

{{#if experiments.sub_agent_strategy == 'sub_agents'}}
Invoke `artifacts-summarizer` by passing this exact input block:
```text
run_dir: /tmp/gh-aw/agent/agentrx/runs/gh-aw-daily
```
Paste its markdown output as the body of this details block.
{{/if}}

{{#if experiments.sub_agent_strategy == 'single_agent'}}
Summarize AgentRx artifacts inline in this main agent using this exact input:
```text
run_dir: /tmp/gh-aw/agent/agentrx/runs/gh-aw-daily
```
Cover the same sections (IR summary, invariant/checker highlights, judge classification output when available, and known limitations). Do not invoke `artifacts-summarizer` in this variant.
{{/if}}

</details>

### Recommended Optimization
- One specific change
- Why this is highest impact
- Where to implement (workflow file or code path)

### Validation Plan
- How to confirm improvement on the next run
- Expected success metric changes

### References
- Up to three links to relevant workflow runs or session contexts.

## Guardrails

- Do not invent telemetry or AgentRx outputs.
- Prefer concrete evidence over broad advice.
- If telemetry is unavailable or unusable, call `noop` with a clear reason.
- Otherwise, always call `create_issue` once.
## agent: `trajectory-builder`
---
description: Builds AgentRx trajectory input from MCP run and log data
model: small
---
You are a structured-data extraction agent.
Expected input format:
`run_data_path: <absolute-path-to-mcp-run-data-json>`
Read the file at `run_data_path` and create `/tmp/gh-aw/agent/agentrx/trajectory.json`.
Use the last 24h of data and prioritize failed or high-latency runs.
Map `runs[]` session records to ordered workflow steps.
Include when present: step index, `github.workflow_ref`, `github.run_id`, status/error signal, `duration`, `aic`, `estimated_cost`, `turns`, `agentic_assessments`, `behavior_fingerprint`, `missing_tool_count`.
Output valid JSON only and write it to `/tmp/gh-aw/agent/agentrx/trajectory.json`.

## agent: `artifacts-summarizer`
---
description: Summarizes AgentRx stage artifacts for issue details output
model: small
---
You are an artifact summarization agent.
Expected input format:
`run_dir: <absolute-path-to-agentrx-run-dir>`
Read AgentRx stage outputs from `run_dir` (`ir`, `static`, `dynamic`, `check`, `judge`, `report`).
Produce concise markdown bullets for the AgentRx Artifacts details block.
Cover: IR summary, invariant/checker highlights, judge classification output when available, and known limitations such as missing fields or auth-limited stages.
Do not invent values.

## agent: `failure-pattern-classifier`
---
description: Classifies AgentRx violations into predefined optimization fix types
model: small
---
You are a violation classification agent.
Expected input format:
`check_path: <absolute-path-to-check-artifact-json>`
`judge_path: <absolute-path-to-judge-artifact-json>`
Read `check_path` (required) and `judge_path` (if present).
Label every AgentRx violation with exactly one fix type from this taxonomy:
- prompt tightening to reduce invalid tool invocations
- adding precondition checks before expensive tools
- improving retry/backoff strategy
- reducing token-heavy context payloads
- adding missing telemetry attributes for better triage
Return a markdown table with columns: violation, evidence, fix_type, rationale.
Use only provided AgentRx artifacts.