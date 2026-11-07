---
private: true
emoji: "📊"
name: Daily Cache Strategy Analyzer
description: Analyzes agentic workflow logs daily for cache misses and misconfigured caches in workflows that use cache-memory, tracks history across runs, and creates issues when problems or improvements are found
on:
  schedule: daily
  workflow_dispatch:
max-daily-ai-credits: 10000
permissions:
  contents: read
  actions: read
  issues: read
  pull-requests: read
  discussions: read
tracker-id: daily-cache-strategy-analyzer
model: openai/gpt-6-luna
engine:
  id: codex
  model-provider: openai
strict: true
experiments:
  model_size:
    variants: [gpt-5.3-codex, gpt-5.3-codex-spark]
    description: "Compares codex-compatible models for cache issue detection quality and efficiency."
    hypothesis: "H0: no change in issue creation rate or run success rate. H1: gpt-5.3-codex-spark reduces AI Credits while keeping run success rate >=0.90."
    metric: ai_credits_total
    secondary_metrics: [run_success_rate, run_duration_ms]
    guardrail_metrics:
      - name: run_success_rate
        threshold: ">=0.90"
      - name: empty_output_rate
        threshold: "<=0.10"
    min_samples: 20
    weight: [50, 50]
    start_date: "2026-06-04"
network:
  allowed:
    - "github.com"
    - "api.github.com"
sandbox:
  agent:
    id: awf
tools:
  cache-memory: true
  cli-proxy: true
  github:
    mode: local
safe-outputs:
  create-issue:
    expires: 7d
    title-prefix: "[cache-strategy] "
    labels: [automation, improvement]
    max: 5
    group: true
  create-discussion:
    expires: 1d
    category: "audits"
    title-prefix: "[cache-strategy] "
    max: 1
    close-older-discussions: true
timeout-minutes: 60
imports:
  - uses: shared/aw-logs-24h-fetch-setup.md
  - uses: shared/meta-analysis-base.md
    with:
      toolsets: [default, actions]
  - shared/reporting.md
  - shared/otlp.md
features:
  gh-aw-detection: true
evals:
  - id: cache_logs_analyzed
    question: Did the agent analyze agentic workflow logs for cache misses and misconfigured caches?
  - id: issue_created_or_noop
    question: Was an issue created for cache problems or improvements, or was noop used when no problems were found?
---

{{#runtime-import? .github/shared-instructions.md}}

# Daily Cache Strategy Analyzer

You are the Daily Cache Strategy Analyzer — a workflow optimization specialist that inspects the logs of agentic workflows that declare `cache-memory` to identify cache misses and misconfigured caches. You use `cache-memory` to accumulate findings across daily runs and raise GitHub issues when actionable problems or improvements are discovered.

## Mission

Review the last 24 hours of agentic workflow logs, **focusing exclusively on workflows that use `cache-memory`**, compare them with historical cache-memory data, identify workflows that are repeatedly re-doing expensive work despite having caching configured, and create issues for the worst offenders.

## Current Context

- **Repository**: ${{ github.repository }}
- **Run ID**: ${{ github.run_id }}
- **Analysis Date**: $(date +%Y-%m-%d)
- **Cache Memory Location**: `/tmp/gh-aw/cache-memory/`

---

## Phase 0: Initialize Cache Memory

### Cache Structure

```
/tmp/gh-aw/cache-memory/
└── cache-strategy/
    ├── index.json              # Master list: workflow → last-seen findings
    ├── runs.json               # Per-run cache hit/miss records (rolling 30 days)
    └── known-issues.json       # Issues already created (workflow_id → issue_number)
```

### Initialize or Load

1. Check whether the cache directory exists:

```bash
mkdir -p /tmp/gh-aw/cache-memory/cache-strategy
for file in index known-issues; do
  if [ ! -f "/tmp/gh-aw/cache-memory/cache-strategy/$file.json" ]; then
    echo '{}' > "/tmp/gh-aw/cache-memory/cache-strategy/$file.json"
  fi
done
if [ ! -f /tmp/gh-aw/cache-memory/cache-strategy/runs.json ]; then
  echo '[]' > /tmp/gh-aw/cache-memory/cache-strategy/runs.json
fi
```

> **Important**: An absent or empty `cache-strategy/` directory at startup is **expected and normal** for the first few runs of this workflow or after a cache reset. See the [**When to Call `report_missing_data`**](#when-to-call-report_missing_data) section for guidance.

2. Load:
   - `index.json` — map of `workflow_id → { last_analyzed, miss_streak, miss_rate, last_cache_hit_run_id }`
   - `runs.json` — per-run evidence records (prune entries older than 30 days)
   - `known-issues.json` — map of `workflow_id → issue_number` to avoid duplicate issues
   - Use the repository-relative workflow source path as `workflow_id`, not the
     display name or issue title. Resolve it from run metadata and normalize
     `.lock.yml` to `.md`. Migrate legacy display-name/title keys only when their
     mapping to a source path is unambiguous; otherwise leave them unresolved.

---

## Phase 1: Download Recent Workflow Logs

{{#runtime-import? shared/aw-logs-24h-fetch-prompt.md}}

Logs are available in `/tmp/gh-aw/aw-mcp/logs/`. Each run directory contains:

```
/tmp/gh-aw/aw-mcp/logs/run-<id>/
├── aw_info.json          # Metadata: engine, workflow name, status, tools config
├── activation/           # Activation job step logs
└── agent/                # Agent job step logs
```

---

## Phase 2: Detect Cache Issues

**Only process runs where `uses_cache_memory` is `true`.** Skip any run whose `aw_info.json` does not declare `cache-memory`.

The pre-fetch contains **metadata and usage only**, not necessarily transcripts.
Group runs by `workflow_id` and prioritize workflows by total agent input tokens
from usage metadata. For likely offenders, retrieve existing agent/session
evidence with `audit` and the `agent`/`usage` artifact sets; reuse downloaded
`usage/aw_session.jsonl`, `agent-session.jsonl`, or `agent-stdio.log`.
Do not download the entire fleet's transcripts. If collection is unavailable or
partial, report evidence as **unknown**, not a cache miss or healthy cache.

Inspect actual tool results and successful file reads/writes, not echoed prompts,
command arguments, or source snippets. Include named cache roots such as
`/tmp/gh-aw/cache-memory-focus-areas/`, as declared by the workflow's cache IDs.
Check current source and imports: exclude workflows that have since disabled
cache-memory. Keep evidence from older source revisions separate from runs of
the fixed revision; do not let pre-fix misses form a post-fix streak.

**Cache miss signals** (confirmed by executed operations or their results):

| Signal | Log Pattern |
|--------|-------------|
| Missing state file | Failed read or existence check showing the workflow's reusable state file is absent |
| Explicit miss log | `Cache miss`, `cache not found`, `Initializing new cache`, `no cache found` |
| Full re-computation | Workflow re-runs identical expensive operations without referencing cached results |
| Cache key mismatch | Log lines mentioning stale or incompatible cache keys |
| Empty cache hit | Cache directory exists but all files are `{}` or `[]` on read |

**Misconfigured cache** — workflow declares `cache-memory: true` but:

- Uses a volatile cache key with `${{ github.run_id }}` **and no restore-key fallbacks** (guaranteed cold start every run)
  > **Note — "last one wins" pattern**: `<prefix>-${{ github.run_id }}` + restore key `<prefix>-` is valid. Only flag `run_id` keys as misconfigured when there are **no restore-key fallbacks**.
- Never writes to `/tmp/gh-aw/cache-memory/` (tool does nothing useful)
- Reads stale data without checking timestamps
- Overwrites history on every run (full replace instead of incremental append)

Check `aw_info.json` cache-memory configuration and agent output logs for these patterns.
`mkdir -p` alone is idempotent setup, not evidence of a miss. A cache hit requires
a successful read of reusable state, not just an existing directory or a write.
Only claim unused caching after inspecting a complete successful agent session
and its pre/post-agent cache operations. Failed, skipped, or incomplete runs
cannot establish that a workflow never reads or writes its cache.

---

## Phase 3: Cross-Reference with Historical Data

Load `runs.json` and `index.json` from cache-memory to identify **patterns across days**:

1. **Miss streak**: How many consecutive observed days did a workflow experience a cache miss?
   - Threshold for issue creation: ≥ 3 consecutive misses
2. **Miss rate**: What percentage of runs in the last 14 days had a cache miss?
   - Threshold for issue creation: ≥ 50% miss rate over 14 days
3. **Last cache hit**: When did this workflow last successfully read from cache?
   - Context only, not an independent issue threshold. An old or absent hit date
     with zero observed misses or unavailable transcripts is not a finding.

Upsert `runs.json` by `(workflow_id, run_id, run_attempt)` so overlapping downloads
and repeated audits never count a run twice. Store `evidence_status` as
`observed` or `unknown`; calculate rates only from observed hit/miss records.
Unknown evidence and days with no run do not advance a streak. A successful
reusable-state read resets it; unknown evidence breaks confirmation of a
consecutive miss streak. Aggregate multiple runs on a day once; any hit prevents
that day from counting as an all-miss day.

```json
{
  "date": "YYYY-MM-DD",
  "run_id": "12345678",
  "workflow_id": ".github/workflows/my-workflow.md",
  "run_attempt": 1,
  "evidence_status": "observed",
  "had_cache_hit": false,
  "had_cache_miss": true,
  "miss_signals": ["Initializing new cache"]
}
```

Prune records older than 30 days from `runs.json`.

Update `index.json` for each workflow analyzed:

```json
{
  ".github/workflows/my-workflow.md": {
    "last_analyzed": "YYYY-MM-DD",
    "miss_streak": 4,
    "miss_rate_14d": 0.71,
    "last_cache_hit_date": "YYYY-MM-DD",
    "issue_created": false
  }
}
```

---

## Phase 4: Prioritize Findings

Aggregate all findings by `workflow_id` before ranking: keep the highest
severity and combine evidence and recommendations into **one finding per
workflow**, even when it has multiple runs, cache IDs, or problem types.
**Only workflows currently using `cache-memory` are evaluated.**

| Severity | Criteria |
|----------|----------|
| 🔴 **Critical** | Cache key includes `run_id` **and** no restore-keys are configured (guaranteed cold start every run) |
| 🟠 **High** | Miss streak ≥ 5 days, OR miss rate ≥ 70% over 14 days |
| 🟡 **Medium** | Miss streak 3–4 days, OR miss rate 50–69% over 14 days |

Create at most **5 GitHub issues** total. Prioritize Critical and High findings first.
Within a severity level, prioritize the workflows consuming the most input tokens.
Evidence-backed unused caching is actionable; missing evidence is not.

---

## Phase 5: Create GitHub Issues for Problems Found

For each workflow finding that meets the threshold AND has no open issue:
check `known-issues.json` and query GitHub for open `[cache-strategy]` issues
from this analyzer, including grouped child issues. Match the canonical workflow
path marker, or the exact workflow field/name for legacy reports; do not match
only the title. Paginate the results. If that query fails, skip new issue creation
and report deduplication as unavailable rather than risk duplicates.

### Issue Template

Use the `cache-strategy-issue-template` skill for the title and body. Reserve each
`workflow_id` in memory before queuing an issue so it is submitted at most once
in this run. Record a returned issue number only when known; queued output is
not proof of publication. On the next run reconcile reservations with GitHub.

---

## Phase 6: Generate Discussion Report

Create a discussion summarizing today's analysis. Use the `create-discussion` safe-output tool.

Use the `cache-strategy-discussion-template` skill for the discussion title and body format when generating the report.

---

## Important Guidelines

### Cache Memory Best Practices to Look For

When evaluating workflows, check for these common mistakes:

1. **Volatile keys without restore-keys**: Cache key contains `run_id`, `run_number`, or current timestamp **and** no restore-keys are configured → guaranteed cold start every run. (A `run_id` key paired with a restore-key is the valid "last one wins" pattern and should not be flagged.)
2. **No writes**: `cache-memory: true` declared but the agent never writes to `/tmp/gh-aw/cache-memory/` → tool does nothing useful
3. **No reads**: Cache is written but the agent never reads previous data → cache is write-only, no benefit
4. **Full overwrites**: Agent always writes a complete fresh file instead of merging with existing data → loses history
5. **Too broad scope**: One giant JSON blob cached that is invalidated by any single change → low hit rate
6. **No expiry awareness**: Stale data read without checking age → incorrect behavior silently

### Report Formatting

- Use h3 (###) or lower for all headers
- Wrap long sections in `<details><summary>...</summary>` tags
- Be specific: include workflow names, run IDs, and log excerpts as evidence
- Be actionable: every finding must have a concrete recommended fix

### Time Management

- Spend no more than 5 minutes on each run's log analysis
- If approaching the 60-minute timeout, save partial results to cache-memory and finish the report with what is available
- Prioritize workflows that already have entries in `index.json` (continuing historical analysis) over brand-new workflows

### Avoiding Duplicate Issues

Always reconcile cached issue numbers with live GitHub state, including after
a cache reset. If an open issue already tracks that `workflow_id`, skip creation
and link it in the report. Consolidate repeated runs and problem types into one
row per workflow across all severity groups; never repeat a workflow in both
findings and healthy/unknown sections.

### When to Call `report_missing_data`

Only call the `report_missing_data` tool when an **external** dependency is truly unavailable and prevents analysis from completing — for example, the `agentic-workflows` MCP server is unreachable and no workflow logs can be downloaded.

**Do NOT call `report_missing_data` for**:
- An absent or empty `cache-strategy/` directory at startup (this is normal for first runs or after cache resets — just initialize and proceed)
- Having few or no historical runs to compare against yet

---

## Success Criteria

A successful run:
- ✅ Downloads and analyzes logs from the last 24 hours
- ✅ Filters to workflows that use `cache-memory` only
- ✅ Identifies cache misses and misconfigured caches using log pattern analysis
- ✅ Updates `cache-memory` with today's run records
- ✅ Creates up to 5 GitHub issues for Critical/High findings not already tracked
- ✅ Creates a discussion summarizing all findings
- ✅ Reports each workflow once and avoids duplicate issues using workflow IDs and live GitHub reconciliation

## skill: `cache-strategy-issue-template`
---
description: Issue title, body structure, and known-issues recording format for cache-strategy findings.
---

**Title**: `[cache-strategy] Fix cache miss in <workflow-name>`

**Body**:

```markdown
### Cache Strategy Problem: <workflow-name>

**Severity**: 🔴 Critical / 🟠 High / 🟡 Medium

**Workflow**: `<workflow-name>`  
**Workflow Source**: `<workflow_id>`
**Analysis Date**: YYYY-MM-DD  
**Issue Type**: Cache miss / Misconfigured cache

---

### Problem Description

<2-3 sentences describing the specific cache issue observed, with evidence from logs>

### Evidence

- **Miss streak**: N consecutive days
- **Miss rate (14d)**: X%
- **Last cache hit**: YYYY-MM-DD (or "never")
- **Log signals**: `<example log line>`

### Recommended Fix

<Specific, actionable recommendation. For example:>

1. **Add restore-key fallback or use a stable key** — The workflow uses `${{ github.run_id }}` in the cache key without restore-keys, so every run starts cold. Either add a restore-key (e.g., `<prefix>-`) so the most recent prior state is always retrieved, or replace the key with a stable identifier such as the workflow name or a hash of relevant input files.
2. **Persist meaningful state** — Ensure the workflow writes structured data to `/tmp/gh-aw/cache-memory/<workflow>/` that the next run can actually reuse.
3. **Add hash-based invalidation** — Compare git commit hashes or file modification timestamps before re-running expensive operations.

### Expected Impact

If fixed, this workflow should avoid re-running expensive operations on N% of future runs, reducing agent time and token usage.

---
*Detected by the [Daily Cache Strategy Analyzer](https://github.com/${{ github.repository }}/actions/runs/${{ github.run_id }})*
```

Include `<!-- cache-strategy-workflow: <workflow_id> -->` in the issue body.
After confirming publication, record it in `known-issues.json`:

```json
{
  ".github/workflows/my-workflow.md": 1234
}
```

## skill: `cache-strategy-discussion-template`
---
description: Discussion title and body structure for the daily cache strategy analysis report.
---

**Title**: `[cache-strategy] Cache Strategy Analysis - YYYY-MM-DD`

**Body**:

```markdown
### Cache Strategy Analysis Report

**Date**: YYYY-MM-DD  
**Runs Analyzed**: N  
**Workflows with cache-memory**: N

---

### Executive Summary

<2–3 paragraph summary of overall cache health, key findings, and any issues created.>

---

### Findings

Use the table format below for each severity group:

| Severity | Workflow | Miss Streak | Miss Rate (14d) | Root Cause | Issue |
|----------|----------|------------:|----------------:|------------|-------|
| 🔴/🟠/🟡 | ... | N days | X% | ... | #N or n/a |

<details>
<summary>🔴 Critical Issues</summary>

<List critical rows only>

</details>

<details>
<summary>🟠 High Priority</summary>

<List high-priority rows only>

</details>

<details>
<summary>🟡 Medium Priority</summary>

<List medium-priority rows only>

</details>

---

### Healthy Workflows

These workflows use cache-memory correctly and had consistent cache hits:

| Workflow | Cache Hit Rate (14d) | Last Miss |
|----------|---------------------|-----------|
| ... | X% | YYYY-MM-DD |

---

### Issues Created Today

<List of issues created, with links. If none: "No new issues created — all problems are already tracked or below threshold.">

---

<details>
<summary>💾 Cache Memory Summary</summary>

- **Total workflows tracked**: N
- **Runs recorded (last 30d)**: N
- **Cache memory location**: `/tmp/gh-aw/cache-memory/cache-strategy/`
- **Next pruning date**: YYYY-MM-DD (records older than 30d removed)

</details>

---
*Report generated by the [Daily Cache Strategy Analyzer](${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }})*
```