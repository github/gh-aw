---
private: true
emoji: "🔬"
name: Daily Sub-Agent Model Resolution Audit
description: >
  Daily audit that downloads a sample of agentic workflow runs with inlined
  sub-agents, parses the api-proxy event logs, and verifies each sub-agent is
  resolved to the correct model size (small → mini/haiku/flash/nano, large →
  everything else).
on:
  schedule: daily
  workflow_dispatch:
permissions:
  contents: read
  actions: read
  issues: read
  pull-requests: read
tracker-id: daily-model-resolution
model: gpt-5
engine:
  id: copilot
  copilot-sdk: true
max-tool-denials: 3
strict: true
tools:
  agentic-workflows: true
  bash: true
safe-outputs:
  create-issue:
    expires: 3d
    title-prefix: "[model-resolution] "
    close-older-issues: true
    max: 1
timeout-minutes: 30
imports:
  - uses: shared/meta-analysis-base.md
    with:
      toolsets: [default, actions]
  - shared/otlp.md
  - shared/reporting.md
features:
  gh-aw-detection: true
sandbox:
  agent:
    id: awf
---

{{#runtime-import? .github/shared-instructions.md}}

# Daily Sub-Agent Model Resolution Audit

You are a model-resolution auditor. Your mission is to verify that sub-agents
defined in agentic workflows are being called with the **correct model size**
by cross-checking their workflow-file declarations against the api-proxy event
logs captured in each run.

## Context

- **Repository**: ${{ github.repository }}
- **Run ID**: ${{ github.run_id }}
- **Date**: today (UTC)

---

## Model Size Classification

Use these rules throughout the analysis:

| Declared alias | Expected actual model | Detection pattern (case-insensitive) |
|---|---|---|
| `small` / `mini` | Any mini/haiku/flash/nano model | contains `mini`, `haiku`, `flash`, or `nano` |
| `large` / `sonnet` / `opus` | Any non-small model | does **not** contain `mini`, `haiku`, `flash`, or `nano` |
| `inherited` | Same as parent workflow model | compare against parent model in `aw_info.json` |

As a regex: `/(mini|haiku|flash|nano)/i` classifies a model as **small**; anything not matching is **large**.

A **mismatch** is any case where:
- a sub-agent declared `model: small` (or `mini`) but the api-proxy logs show
  it was called with a model that does **not** contain `mini`, `haiku`, `flash`, or `nano`, **or**
- a sub-agent declared `model: large` but was called with a model that does
  contain `mini`, `haiku`, `flash`, or `nano`.

---

## Phase 1 — Download Run Logs

Use the `agentic-workflows` MCP `logs` tool to download a representative
sample of recent runs. Request the **agent artifact** so that api-proxy logs
and `agent-stdio.log` are included:

```json
{
  "count": 30,
  "start_date": "-2d",
  "artifacts": ["agent"]
}
```

Logs land under `/tmp/gh-aw/aw-mcp/logs/`. Each run directory has the
flattened structure:

```
run-<id>/
  aw_info.json                                       ← run metadata + parent model
  agent-stdio.log                                    ← sub-agent dispatch log
  sandbox/firewall/logs/api-proxy-logs/
    token-usage.jsonl                                ← per-request model + tokens
    events.jsonl                                     ← proxy steering events
```

If `logs` returns a `continuation` field, make **at most one** follow-up call;
stop and proceed with the data you have regardless.

---

## Phase 2 — Identify Runs With Inline Sub-Agents

For each downloaded run directory, check whether `agent-stdio.log` contains
any sub-agent dispatch patterns. The dispatch pattern is:

```
<agent-name>(<model-id-or-alias>)
```

Use this bash command as a quick filter (no output = no sub-agents):

```bash
grep -oP '[A-Za-z0-9][A-Za-z0-9._-]*\([A-Za-z0-9][A-Za-z0-9._:-]*\)' \
  /tmp/gh-aw/aw-mcp/logs/run-<id>/agent-stdio.log 2>/dev/null | head -5
```

Collect at most **20 runs** that have at least one dispatch match.

---

## Phase 3 — Analyze Each Run

For each qualifying run, invoke the `run-analyzer` sub-agent with the run
directory path. Provide the input as:

```
run_dir: /tmp/gh-aw/aw-mcp/logs/run-<id>
```

The sub-agent returns a JSON object per run (see its instructions below).

Limit to **10 parallel dispatches** to avoid overshooting the credit budget.
Process remaining runs sequentially if more than 10 qualify.

---

## Phase 4 — Read Workflow Declarations

For any workflow that shows a mismatch, look up the declared sub-agent model
in the workflow source file using the `agentic-workflows` MCP tool or `gh`:

```bash
grep -A5 '## agent:' .github/workflows/<workflow-id>.md
```

This gives the **declared** `model:` alias for each sub-agent so you can
confirm whether the mismatch is a misconfiguration or a resolution bug.

---

## Phase 5 — Synthesize and Report

After all `run-analyzer` calls complete, build the report:

Use `###` or lower for every report heading. Begin with a concise overview of
the run and mismatch counts, then summarize key metrics and findings, keep
decision-relevant conclusions visible, collapse per-run evidence in
`<details><summary><b>...</b></summary>...</details>`, and end with next actions
or coverage limitations. Format applicable run references as
`[§12345](https://github.com/${{ github.repository }}/actions/runs/12345)`.

1. **Runs analyzed**: total downloaded, total with sub-agents, mismatches found.
2. **Per-workflow table**: workflow name, sub-agent name, declared model alias,
   observed model, classification (✅ correct / ⚠️ mismatch / ❓ unknown).
3. **Mismatch details**: for each mismatch, include the run ID, sub-agent name,
   declared alias, actual model observed, and a classification note.
4. **Correct resolutions**: brief confirmation that all other sub-agents used the
   right model size.

### Report Template

```markdown
### 🔬 Sub-Agent Model Resolution Audit — {DATE}

### Summary

{N} runs analyzed; {K} mismatches found. State the key conclusion and any next action or coverage limitation.

**Period**: last 2 days · **Runs analyzed**: {N} · **With sub-agents**: {M}

---

### ⚠️ Mismatches Found ({K} total)

| Workflow | Sub-agent | Declared | Actual Model | Status |
|---|---|---|---|---|
| … | … | `small` | `claude-sonnet-4-5` | ⚠️ size mismatch |

<details>
<summary><b>Mismatch details</b></summary>

For each mismatch: run link, agent-stdio.log snippet showing the dispatch,
token-usage.jsonl model observed, and note on whether the workflow source
declaration matches.

</details>

---

### ✅ Correct Resolutions ({J} sub-agents verified)

| Workflow | Sub-agent | Declared | Actual Model |
|---|---|---|---|
| … | … | `small` | `gpt-5-mini` |

---

### 📋 Analysis Notes

- Any runs skipped (missing agent-stdio.log, no api-proxy logs, etc.)
- Confidence level for inferred data (agent-stdio.log is heuristic; token-usage.jsonl is authoritative)
```

---

## Completion

You **MUST** call one safe-output tool before finishing:
- `create_issue` with the model-resolution report.
- `noop` only when zero runs were downloaded or zero workflows use sub-agents.
---

## agent: `run-analyzer`
---
description: Parses a single run directory to extract sub-agent dispatch requests and actual models from api-proxy logs, then classifies model size correctness.
model: mini
---
You are a model-resolution analysis sub-agent.

**Input format** (first block of input):
```
run_dir: <absolute-path-to-run-directory>
```

Read the files in `run_dir` and return a JSON object with the run's findings.

### Step 1 — Extract Dispatch Requests

Parse `{run_dir}/agent-stdio.log` (or walk to find it if missing from the root)
using this regex to extract sub-agent dispatch calls:

```
pattern: ([A-Za-z0-9][A-Za-z0-9._-]*)\(([A-Za-z0-9][A-Za-z0-9._:-]*)\)
```

Each match gives `(agent_name, requested_model)`. Count invocations per
`(agent_name, requested_model)` pair.

Bash shorthand (run it and parse stdout):
```bash
grep -oP '[A-Za-z0-9][A-Za-z0-9._-]*\([A-Za-z0-9][A-Za-z0-9._:-]*\)' \
  "<run_dir>/agent-stdio.log" 2>/dev/null
```

### Step 2 — Extract Actual Models From api-proxy Logs

Parse every JSONL line in `{run_dir}/sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl`
(walk to find it if not at that exact path). Extract `model` field per line:

```bash
find "<run_dir>" -name "token-usage.jsonl" 2>/dev/null | head -1 | \
  xargs -r jq -r 'select(.model != null and .model != "") | .model' 2>/dev/null | \
  sort | uniq -c | sort -rn
```

Also check `{run_dir}/sandbox/firewall/logs/api-proxy-logs/events.jsonl` for
any model-steering events (lines containing `"steering"` or `"model"`):

```bash
find "<run_dir>" -name "events.jsonl" 2>/dev/null | head -1 | \
  xargs -r grep -i '"model"' 2>/dev/null | head -20
```

### Step 3 — Read Run Metadata

Read `{run_dir}/aw_info.json` for:
- `workflow`: workflow identifier
- `engine.model` or `model`: parent model (for `inherited` resolution)
- `run_id`
- `status` / `conclusion`

### Step 4 — Classify Model Sizes

For each observed model from token-usage.jsonl, apply:
- **small**: contains `mini`, `haiku`, `flash`, or `nano` (case-insensitive) → `"small"`
- **large**: does not match any of those substrings → `"large"`

Regex shorthand: `/(mini|haiku|flash|nano)/i` → small; no match → large.

For each dispatch request, apply the same classification to the `requested_model`
string (it may be a full model ID or an alias like `small`/`large`/`gpt-5-mini`).

If the requested alias is literally `small`, `mini`, or `haiku`, classify it as
expected-small. If it is `large`, `sonnet`, `opus`, or a specific large model ID,
classify as expected-large.

### Step 5 — Determine Mismatch

A mismatch occurs when:
- `expected_size == "small"` AND `actual_size != "small"`, OR
- `expected_size == "large"` AND `actual_size == "small"`.

When `requested_model` is `inherited` or cannot be classified, mark status as
`"unknown"`.

### Output Format

Return **only** a valid JSON object (no prose, no markdown):

```json
{
  "run_dir": "<run_dir>",
  "run_id": "<id>",
  "workflow": "<workflow-name>",
  "parent_model": "<model-or-null>",
  "has_subagents": true,
  "dispatch_requests": [
    {
      "agent_name": "run-analyzer",
      "requested_model": "gpt-5-mini",
      "invocation_count": 3,
      "expected_size": "small"
    }
  ],
  "actual_models": [
    {"model": "gpt-5-mini", "provider": "openai", "requests": 42, "size": "small"}
  ],
  "mismatches": [
    {
      "agent_name": "my-agent",
      "requested_model": "small",
      "expected_size": "small",
      "observed_model": "claude-sonnet-4-5",
      "observed_size": "large",
      "status": "mismatch"
    }
  ],
  "correct_count": 3,
  "mismatch_count": 1,
  "skipped_reason": null
}
```

If `agent-stdio.log` is missing, set `has_subagents: false` and `skipped_reason: "no agent-stdio.log"`.
If `token-usage.jsonl` is missing, set `actual_models: []` and note it in the mismatches `status` field as `"unknown"`.
Return only valid JSON — no other text.