---
private: true
emoji: "🌫️"
name: Daily Ambient Context Optimizer
description: Samples recent agentic workflow runs, inspects the first DLLM request from API proxy event logs, and recommends prompt, skill, and agent changes to shrink ambient context
on:
  schedule: daily
  workflow_dispatch:
permissions:
  contents: read
  actions: read
  issues: read
  pull-requests: read
  copilot-requests: write
engine:
  id: copilot
  copilot-sdk: true
tracker-id: daily-ambient-context-optimizer
strict: true
max-daily-ai-credits: 10000
network:
  allowed: [defaults, github]
sandbox:
  agent:
    id: awf
    runtime: cloud-hypervisor
tools:
  cli-proxy: true
  github:
    mode: local
  agentic-workflows:
  bash: true
safe-outputs:
  mentions: false
  allowed-github-references: []
  create-issue:
    title-prefix: "[ambient-context] "
    labels: [automation, report, workflow-optimization, analysis]
    close-older-issues: true
    expires: 7d
    max: 3
timeout-minutes: 45
steps:
  - name: Setup Python
    uses: actions/setup-python@v7.0.0
    with:
      python-version: "3.12"
  - name: Prepare analysis workspace
    run: |
      mkdir -p /tmp/gh-aw/ambient-context
  - name: Closed PR deduplication guard
    uses: actions/github-script@v9.0.0
    with:
      github-token: ${{ secrets.GITHUB_TOKEN }}
      script: |
        const fs = require('fs');

        const { owner, repo } = context.repo;
        const OPTIMIZER_PATTERNS = [
          'copilot/ambient-context',
          'copilot/daily-ambient-context',
          'ambient-context-optim',
        ];

        const cutoff = new Date(Date.now() - 14 * 24 * 60 * 60 * 1000);
        cutoff.setUTCHours(0, 0, 0, 0);

        const allClosed = await github.paginate(github.rest.pulls.list, {
          owner, repo, state: 'closed', per_page: 100,
        });

        const closedOptimizerPrs = allClosed.filter((pr) => {
          const branch = pr.head?.ref || '';
          return (
            !pr.merged_at &&
            OPTIMIZER_PATTERNS.some((p) => branch.includes(p)) &&
            new Date(pr.closed_at) >= cutoff
          );
        });

        const blockedFilesSet = new Set();
        for (const pr of closedOptimizerPrs) {
          try {
            const files = await github.paginate(github.rest.pulls.listFiles, {
              owner, repo, pull_number: pr.number, per_page: 100,
            });
            for (const f of files) blockedFilesSet.add(f.filename);
          } catch (err) {
            core.warning(`Could not list files for PR #${pr.number}: ${err.message}`);
          }
        }

        const blockedFiles = [...blockedFilesSet].sort();
        const data = {
          closed_optimizer_prs: closedOptimizerPrs.map((pr) => ({
            number: pr.number,
            title: pr.title,
            closedAt: pr.closed_at,
            mergedAt: pr.merged_at,
            headRefName: pr.head?.ref,
          })),
          blocked_files: blockedFiles,
          pr_count: closedOptimizerPrs.length,
          generated_at: new Date().toISOString(),
        };

        fs.mkdirSync('/tmp/gh-aw/ambient-context', { recursive: true });
        fs.writeFileSync('/tmp/gh-aw/ambient-context/closed-pr-targets.json', JSON.stringify(data, null, 2));

        core.info(`Deduplication guard: ${closedOptimizerPrs.length} closed optimizer PRs, ${blockedFiles.length} blocked files`);
        for (const bf of blockedFiles) core.info(`  BLOCKED: ${bf}`);
  - name: PR close-rate metric
    uses: actions/github-script@v9.0.0
    with:
      github-token: ${{ secrets.GITHUB_TOKEN }}
      script: |
        const fs = require('fs');

        const { owner, repo } = context.repo;
        const OPTIMIZER_PATTERNS = [
          'copilot/ambient-context',
          'copilot/daily-ambient-context',
          'ambient-context-optim',
        ];

        const cutoff7d = new Date(Date.now() - 7 * 24 * 60 * 60 * 1000);

        const allPrs = await github.paginate(github.rest.pulls.list, {
          owner, repo, state: 'all', per_page: 100,
        });

        const optPrs7d = allPrs.filter((pr) => {
          const branch = pr.head?.ref || '';
          return (
            OPTIMIZER_PATTERNS.some((p) => branch.includes(p)) &&
            new Date(pr.created_at) >= cutoff7d
          );
        });

        const merged7d = optPrs7d.filter((pr) => pr.merged_at);
        const closed7d = optPrs7d.filter((pr) => !pr.merged_at && pr.closed_at);
        const totalSettled = merged7d.length + closed7d.length;

        // close_rate is null when sample < 3 to avoid misleading zero reads
        const closeRate = totalSettled >= 3 ? closed7d.length / totalSettled : null;
        const autoPause = closeRate !== null && closeRate >= 0.30;

        const rateData = {
          merged_7d: merged7d.length,
          closed_7d: closed7d.length,
          total_settled_7d: totalSettled,
          close_rate: closeRate !== null ? Math.round(closeRate * 1000) / 1000 : null,
          auto_pause: autoPause,
          note: 'close_rate is null when total_settled_7d < 3 (insufficient data for auto-pause)',
        };

        fs.writeFileSync('/tmp/gh-aw/ambient-context/pr-close-rate.json', JSON.stringify(rateData, null, 2));

        const rateStr = closeRate !== null ? `${Math.round(closeRate * 100)}%` : 'N/A (< 3 settled PRs)';
        core.info(`PR close-rate (7d): ${rateStr} (${closed7d.length} closed, ${merged7d.length} merged)${autoPause ? ' — AUTO-PAUSE ACTIVE' : ''}`);

imports:
  - shared/mcp-pagination.md
  - shared/otlp.md
features:
  gh-aw-detection: true
evals:
  - id: workflow_runs_sampled
    question: Did the agent sample recent agentic workflow runs and inspect their ambient context?
  - id: recommendations_produced
    question: Were recommendations produced for prompt, skill, and agent changes to reduce ambient context size?
---

# Daily Ambient Context Optimizer

You are a cost-optimization analyst for `${{ github.repository }}`.

Your job is to inspect the **first request sent to the DLLM** for several recent workflow runs, identify avoidable ambient context, and publish exactly one issue with concrete workflow improvements.

## Goals

1. Sample a small but representative set of agentic workflow runs from the last 24 hours.
2. Inspect the first DLLM request text actually sent to the DLLM for each sampled run.
3. Use deterministic Python analysis to measure prompt bloat and repetition.
4. Recommend the highest-leverage improvements to workflow `.md` files, skill usage, and the set of agents/sub-agents.
5. Create exactly one detailed issue report.

## Data Collection

### Step 1 — Download recent runs

Use the `agentic-workflows` MCP server instead of shelling out to `gh aw`:

- call the `logs` MCP tool with `start_date: "-1d"` and `count: 60`
- use the JSON artifacts under `/tmp/gh-aw/aw-mcp/logs/` as your source of run metadata
- keep GitHub reads on the configured GitHub MCP tools
- use `tools.cli-proxy: true` only for other proxied `gh` CLI commands when they are truly needed
- do not run `gh aw logs` or `gh aw audit` through the CLI proxy because the `agentic-workflows` MCP server already provides dedicated `logs` and `audit` tools for those operations

### Step 2 — Pick the sample set

Sample **4 runs** when available. If fewer than 4 eligible runs exist, sample all eligible runs down to a minimum of 2 before falling back to a reduced-data report.

These limits are intentional to keep token usage bounded and avoid model budget failures.

Eligibility rules:

- `status == "completed"`
- exclude this workflow itself
- prefer successful runs, but include up to 2 failed runs when they have usable request artifacts
- prefer breadth: no more than 2 runs from the same workflow when alternatives exist
- require a usable first-request source:
  - preferred: the first DLLM request payload in the canonical `sandbox/firewall/logs/api-proxy-logs/event-logs.jsonl`, accepting the legacy `sandbox/firewall/logs/api-proxy-logs/events.jsonl` name too (including the matching `sandbox/firewall-audit-logs/...` fallback path when present)
  - fallback: the first `user.message` event in `sandbox/agent/logs/copilot-session-state/<session-id>/events.jsonl`
  - use `prompt.txt` only as a compilation-debug cross-check, never as the ambient-context source of truth

Prefer higher-cost runs first by using `aic`, then `effective_tokens`, `token_usage`, `turns`, or first-request size when available.

### Step 3 — Enrich a subset with audits

Run the `audit` MCP tool for the **2 most expensive sampled runs** so you have richer cost context and references. Record each enriched run's `working_set` block (`measurement_state`, `rebuild_factor`, `peak_input_tokens`, `cumulative_input_tokens`) — the Working-Set Rebuild Factor (WSRF) is a direct signal of how much ambient context is being re-sent across turns and complements the first-request char/token metrics gathered in Step 1.

### Step 4 — Closed PR Deduplication Guard

The deduplication guard runs as a deterministic pre-step before the agent starts.
Read `/tmp/gh-aw/ambient-context/closed-pr-targets.json` — it has already been written.

Files listed under `blocked_files` are **retry-blocked** — exclude any recommendation that targets them.

### Step 5 — PR Close-Rate Metric

The close-rate metric runs as a deterministic pre-step before the agent starts.
Read `/tmp/gh-aw/ambient-context/pr-close-rate.json` — it has already been written.

## First-Request Extraction Rules

Treat the first DLLM request text as:

1. the first DLLM request payload captured in the canonical API proxy event log `sandbox/firewall/logs/api-proxy-logs/event-logs.jsonl`, accepting the legacy `sandbox/firewall/logs/api-proxy-logs/events.jsonl` name too (or the same path under `sandbox/firewall-audit-logs/` when that artifact layout is present), because that is the text actually sent to the DLLM
2. otherwise, extract the first user-message payload from `sandbox/agent/logs/copilot-session-state/<session-id>/events.jsonl`
3. read `prompt.txt` only as a secondary compilation-debug artifact for cross-checking; do not use it as the primary request text

For each sampled run, save the extracted text to:

- `/tmp/gh-aw/ambient-context/samples/run-<id>.txt`

Also save one metadata JSON file per run at:

- `/tmp/gh-aw/ambient-context/samples/run-<id>.json`

Include at least:

- `run_id`
- `workflow_name`
- `workflow_path`
- `run_url`
- `status`
- `conclusion`
- `aic`
- `token_usage`
- `turns`
- `request_chars`
- `request_lines`
- `request_source`
- `request_input_tokens` when a matching API proxy token-usage entry is available
- `prompt_chars` when `prompt.txt` exists
- `request_prompt_char_delta` (`request_chars - prompt_chars` when both exist)
- `working_set_rebuild_factor` (WSRF, from the `audit` MCP tool's `working_set.rebuild_factor`) when the run was enriched with an audit in Step 3; omit for runs that were not audited

## Deterministic Analysis

Write and run a Python script at `/tmp/gh-aw/ambient-context/analyze_requests.py`.

Use only the Python standard library. Do **not** install third-party packages.

The script must read every sampled `run-*.txt` and `run-*.json` file and produce:

- `/tmp/gh-aw/ambient-context/request-analysis.json`
- `/tmp/gh-aw/ambient-context/request-analysis.md`

The script must compute deterministic metrics for each sampled first request:

- bytes, characters, lines, words
- markdown heading count
- list item count
- code fence count
- HTML `<details>` count
- table row count
- inline agent count (`## agent:`)
- inline linter count (`## linter:`)
- inline skill count (`## skill:`)
- imported skill reference count (`SKILL.md`)
- duplicate line ratio
- duplicate paragraph ratio
- per-request char-to-token ratio (`request_chars / request_input_tokens`) when `request_input_tokens` is available
- longest 5 sections by heading
- top repeated non-trivial lines or paragraphs
- count of lines mentioning tools, skills, agents, safe outputs, and workflow instructions

Aggregate metrics across the sample set:

- sampled run count
- distinct workflow count
- median request chars
- p95 request chars
- top workflows by first-request size
- most common repeated fragments
- most common large-section headings

## Source Review

For every sampled run, read the current workflow source file from the repository when `workflow_path` resolves to a local `.github/workflows/*.md` file.

Assess whether the request size is likely driven by:

- verbose workflow markdown
- overly broad or duplicated skill instructions
- too many inline agents or agent definitions that are not justified
- duplicated guardrails, examples, or formatting rules
- context that should be moved to deterministic `steps:` or smaller sub-agents
- large output templates (issue body, discussion body, report formats) embedded inline in the prompt body rather than in `## skill:` blocks

Review `prompt.txt` only as a compiler cross-check artifact:

- compare its size to the authoritative API proxy request text when both are present
- if `prompt.txt` contains inline agents or inline linters that do **not** appear in the API proxy request text, classify that as a likely compilation bug instead of ambient-context evidence against the workflow author

Also review tool/CLI feature readiness for each sampled workflow:

- GitHub MCP tools configured with the required toolsets
- CLI proxy enabled (`tools.cli-proxy: true`)

When one or more are missing, include a recommendation to configure them and rewrite raw `gh aw` shell instructions into explicit `agentic-workflows` MCP-tool usage.

## Deep-Dive Analysis

After the deterministic Python script finishes, perform a deeper review yourself for **at most 2 sampled runs** (only when at least 2 sampled runs exist):

1. For each selected run, review `run-<id>.json` alongside the deterministic analysis metrics already computed, including `working_set_rebuild_factor` (WSRF) when present.
2. Identify at most 3 opportunities per run, each described with `category`, `finding`, `evidence`, and `impact`.
3. Base findings only on the compact structured metrics already gathered — never re-read raw full prompt text.
4. Aggregate and deduplicate opportunities across the sampled runs, then do final prioritization.

## Execution Budget Guardrails

- Keep the workflow bounded and avoid exploratory loops.
- Do not repeatedly re-open or re-parse the same artifacts once required metrics are extracted.
- Keep the final issue body concise and evidence-based, with short bullets and compact explanations.
- Keep the final issue body under 9500 bytes (UTF-8); if needed, shorten details before calling the tool. Note: the CI-Validation Checklist for Implementing Agents (~600 bytes) counts toward this limit and must not be omitted.
- Use at most 3 run links in the final References section.
- Never call `create_issue` with placeholders, probes, or test strings (for example `"."` or `"test"`). Call it only with the final intended title/body.
- Call `create_issue` only after validating your final body length and content.

## Recommendation Rules

Produce **3 to 7** recommendations total.

Each recommendation must include:

- category: `workflow-md`, `skills`, or `agents`
- affected workflow(s)
- evidence from deterministic metrics
- why it should shrink the first request
- expected impact: `high`, `medium`, or `low`
- whether the change is likely safe immediately or needs manual review

Prioritize recommendations that:

1. remove repeated context shared across many runs
2. reduce broad skill loading or oversized skill fusion
3. simplify or remove low-value inline agents
4. move deterministic data gathering out of the main prompt
5. configure GitHub MCP tools and `cli-proxy` when missing, then rewrite raw CLI-oriented problem wording to explicit `agentic-workflows` MCP-tool calls
6. move large inline output templates (issue body, discussion body, report formats) into `## skill:` blocks so they are loaded on demand rather than unconditionally inflating the first request
7. when an audited run shows a high WSRF (context repeatedly rebuilt near the peak invocation size rather than growing incrementally), prioritize recommendations that stop resending large static context on every turn

Do not recommend changes that would obviously weaken safety or remove necessary task context.

Apply these additional guards before finalizing recommendations:

- **Deduplication guard**: Load `/tmp/gh-aw/ambient-context/closed-pr-targets.json`. If a recommendation's primary target file appears in `blocked_files`, skip that recommendation entirely. If **all** candidate recommendations are blocked, do not create a standard recommendation issue; instead create a single minimal issue titled `[ambient-context] Deduplication block — YYYY-MM-DD` (UTC date) that lists the blocked files and their associated closed PRs, then stop.

- **Minimum safe size guard**: Do not recommend reducing a workflow markdown file to fewer than **1,000 characters** or by more than **40%** of its current character count in a single recommendation.

- **Auto-pause rule**: Load `/tmp/gh-aw/ambient-context/pr-close-rate.json`. If `auto_pause` is `true` (close rate ≥ 30% across ≥ 3 settled PRs in the last 7 days), limit the final recommendation count to **1** (the highest-confidence finding that does not target any blocked file) and include a close-rate warning in the issue body.

- **CI-validation requirement**: Every recommendation targeting a `.github/workflows/*.md` file must include the "CI-Validation Checklist for Implementing Agents" in the issue body (see Report Requirements).

## Report Requirements

Create exactly one issue titled:

`[ambient-context] Daily Ambient Context Optimizer - YYYY-MM-DD`

Use `###` or lower for all headers in your report. Never use `#` (h1) or `##` (h2) — these are reserved for the issue title rendered by GitHub.

Wrap long sections (>5 items, detailed lists, raw data) in `<details><summary><b>Section Name</b></summary>` blocks to keep the report scannable.

Keep the issue structured like this (concise, no extra sections):

### Executive Summary
- runs sampled
- workflows covered
- median and p95 first-request size
- highest-level conclusion

### Highest-Leverage Changes
- a concise numbered list of the top recommendations

### CI-Validation Checklist for Implementing Agents

Any agent implementing workflow-file recommendations **must** complete every item below before opening a PR:

- [ ] Run `make recompile` for every modified `.github/workflows/*.md` file — zero compilation errors required
- [ ] Confirm no target workflow file is reduced by more than 40% of its current character count
- [ ] Run `make agent-report-progress` before the final commit and confirm it passes
- [ ] Check the `blocked_files` list in `/tmp/gh-aw/ambient-context/closed-pr-targets.json` (written by Step 4) — **do not re-attempt changes to any file that appears in a closed ambient-context optimization PR from the last 14 days**
- [ ] Include a smoke-test result or dry-run output in the PR description for every changed workflow spec
- [ ] Document all generated `.lock.yml` changes in the PR body
- [ ] Do not submit as a draft PR

### Key Metrics
| Metric | Value |
|---|---|
| Sampled runs | ... |
| Distinct workflows | ... |
| Median chars | ... |
| P95 chars | ... |
| Largest sampled request | ... |
| Merged optimizer PRs (7d) | ... |
| Closed optimizer PRs (7d) | ... |
| Optimizer PR close-rate (7d) | ... |
| WSRF (audited runs) | ... |

<details>
<summary><b>Per-Run First-Request Metrics</b></summary>

Include a markdown table with one row per sampled run (max 4 rows).

</details>

<details>
<summary><b>Repeated Ambient Context Signals</b></summary>

Summarize repeated sections, duplicated fragments, and bloated headings in short bullets.

</details>

<details>
<summary><b>Deterministic Analysis Output</b></summary>

Summarize the Python script outputs and cite only the most relevant metrics.

</details>

### Recommendations by Category
#### Workflow Markdown
#### Skills
#### Agents

Do not add a separate "MCP Tools" section. Keep MCP-to-CLI rewrite guidance inside the existing categories, primarily Workflow Markdown.

### References
- Include up to 3 sampled run links in `[§12345](https://github.com/owner/repo/actions/runs/12345)` format

## Final Validation Checklist

Before calling `create_issue`, verify:

- [ ] closed-PR deduplication guard was run (Step 4) and `/tmp/gh-aw/ambient-context/closed-pr-targets.json` was written
- [ ] PR close-rate metric was computed (Step 5) and `/tmp/gh-aw/ambient-context/pr-close-rate.json` was written
- [ ] any recommendation targeting a file in `blocked_files` was excluded from the final set
- [ ] auto-pause rule was checked: if `auto_pause: true`, final recommendations are capped at 1
- [ ] CI-validation checklist is present in the issue body for every workflow-md recommendation
- [ ] Key Metrics table includes `Merged optimizer PRs (7d)`, `Closed optimizer PRs (7d)`, and `Optimizer PR close-rate (7d)`
- [ ] last-14-day filtering was applied to run sample
- [ ] deduplication block issue was created (and `create_issue` was NOT called for normal recommendations) when all candidates were blocked

## Reduced-Data Behavior

If fewer than 2 eligible runs exist, still create the issue.

In that case:

- explain the reduced sample size clearly
- report whatever evidence is available
- prioritize repository-wide recommendations only when supported by the sampled data

Do not use `noop` merely because the sample is small or imperfect. Create exactly one issue whenever logs are available. Use `noop` only if no run logs can be downloaded at all or the repository context is unavailable.

If `create_issue` returns a body-size validation error, shorten the details and retry with a compact body that preserves Executive Summary, Highest-Leverage Changes, Key Metrics, and References.

## Deep-Dive Analysis Output Shape

Structure each per-run finding as:

```json
{
  "run_id": 123,
  "workflow_name": "name",
  "opportunities": [
    {
      "category": "workflow-md|skills|agents",
      "finding": "short statement",
      "evidence": ["metric or source detail"],
      "impact": "high|medium|low"
    }
  ]
}
```

Rules:
- return at most 3 opportunities per run
- use only provided compact evidence
- prefer opportunities that reduce first-request size without reducing safety