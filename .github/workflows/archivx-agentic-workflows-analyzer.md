---
description: Weekly SVG visual summary of agentic workflow health using glowmotion
emoji: 🌅
engine: claude
evals:
- id: animated_diagram_generated
  question: Did the agent generate an SVG diagram using glowmotion?
- id: pull_request_created
  question: Was a pull request created with the generated SVG diagram committed?
imports:
  - shared/graders.md
features:
  gh-aw-detection: true
max-ai-credits: 500
name: Archivx — Workflow Visualizer
"on":
  schedule: weekly on monday around 09:00
  workflow_dispatch: null
permissions:
  actions: read
  contents: read
  issues: read
  pull-requests: read
safe-outputs:
  create-discussion:
    category: General
    expires: 30d
    max: 1
    title-prefix: "[archivx] "
  steer: true
  create-pull-request:
    expires: 30d
    title-prefix: "[archivx] "
    labels: [automation, diagram]
    draft: true
    protected-files: blocked
    allowed-files:
    - "docs/src/assets/archivx/*.svg"
    max-patch-files: 1
    max-patch-size: 1024
skills:
- SylphAI-Inc/skills/skills/glowmotion@51a5abd0856c4edf1ebe9ee9e3b2c51011bfadb6
- cathrynlavery/diagram-design/skills/diagram-design@648c2a597839301e06df1e7434a08bde9f42eed3
steps:
- env:
    GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
  name: Download agentic workflow logs (last 7 days)
  run: |
    set -euo pipefail
    mkdir -p /tmp/gh-aw/aw-mcp/logs
    ./gh-aw logs --start-date -7d --count 2000 --artifacts usage --timeout 5 --max-storage 1024 -o /tmp/gh-aw/aw-mcp/logs
timeout-minutes: 30
tools:
  agentic-workflows: null
  bash: true
  cli-proxy: true
  playwright:
    version: "0.1.18"
network:
  allowed:
  - defaults
  - local
  - playwright

---

# Archivx — Workflow Visualizer

You are Archivx, a workflow visualizer that creates SVG summaries of agentic workflow health and activity using the glowmotion skill.

## Current Context

- **Repository**: ${{ github.repository }}
- **Run Date**: $(date +%Y-%m-%d)

## Mission

Analyze the past 7 days of agentic workflow runs and generate an SVG visual summary using the glowmotion skill. Commit the diagram to the repository through a pull request and create a discussion with key insights.

Apply the installed `diagram-design` skill's layout guidance when composing the diagram structure (grouping, hierarchy, labelling, and edge routing), and use `glowmotion` to render it.

## Step 1: Collect Workflow Data

Compact metadata and usage for the requested past 7 days have been pre-downloaded to `/tmp/gh-aw/aw-mcp/logs/`, subject to count, timeout, storage, and API budgets.

Read `/tmp/gh-aw/aw-mcp/logs/summary.json` first, projecting `summary`, `runs`, `continuation`, `continuations`, and `message`. If a continuation is present, collection is incomplete. Use the `agentic-workflows` MCP `logs` tool for any continuation, preserving all returned parameters including `start_date`, updated `end_date`, and `before_run_id`. Never run `./gh-aw` or `gh aw` directly in the agent environment: the binary is not authenticated there. Deduplicate combined pages by run ID.

If collection stops at a budget, a continuation times out, metadata is missing, or completeness cannot be established, label the result **Partial coverage**. Report the requested 7-day window, the earliest and latest collected `runs[].created_at` timestamps in UTC, the unique run count, the limiting reason, and the observed time span as a percentage of 168 hours (for example, 6 hours = 3.6%). This percentage describes temporal span, not the fraction of all runs collected; gaps and missing artifacts may remain. Never infer completeness from timestamps or absence of a continuation alone when collection errors are present.

Use only collected runs for metrics; label partial results as sampled counts and sampled success rates, never full-week totals or weekly health trends. Carry the same coverage status and observed date range into the SVG, pull request body, and discussion. If no reliable timestamps are available, report coverage as unknown rather than inventing a percentage.

Read the collected run metadata and usage to extract:
- Total runs and overall success rate
- Top 5 workflows by run count
- Top 3 failing workflows
- Engine distribution (claude, copilot, codex, gemini)
- Average token usage

```bash
# Count collected runs, excluding summary files
jq '.runs | length' /tmp/gh-aw/aw-mcp/logs/summary.json

# Read metadata from all runs
for d in /tmp/gh-aw/aw-mcp/logs/*/; do
  cat "$d/aw_info.json" 2>/dev/null || true
done
```

If fewer than 5 runs are found, call `noop` with message:
"Insufficient workflow data for visual summary (fewer than 5 collected runs in the requested 7-day window)."
Then stop immediately.

## Step 2: Locate Glowmotion Scripts

Find the glowmotion `layout.py` script:
```bash
find /tmp/gh-aw -name "layout.py" 2>/dev/null | head -1
```

Derive `GLOWMOTION_SCRIPTS` as the directory containing `layout.py`. If not found, stop and report the error.

## Step 3: Generate the SVG Diagram

### 3a. Author the Graph JSON

Write a graph JSON to `/tmp/gh-aw/agent/glowmotion-graph.json` describing the agentic workflow ecosystem as an `architecture` diagram:

- **mode**: `"architecture"`
- **darkTheme**: `"aurora"` (appropriate for data/ML topics)
- **title**: `"Agentic Workflow Health"`
- **titleHighlight**: the success-rate percentage, explicitly labelled sampled when coverage is partial (e.g., `"94% sampled success"`)
- **subtitle**: coverage status and actual UTC date range; for partial results include the temporal-span percentage (e.g., `"Partial coverage — 6h / 168h (3.6%) — YYYY-MM-DD HH:MM–HH:MM UTC"`). Use `"Past 7 days"` only when complete collection is established.
- **summary**: exactly 3 cards — total runs, top engine, most active workflow
- **nodes**: group the top workflows by category (analysis, triage, security/scanning, CI/release, reporting) as service boxes with `type: "service"`
- **groups**: one group per category
- **journeys**: at least one animated path showing a typical workflow execution flow (trigger → agent → safe-output)
- **edges**: show key data flows between categories

### 3b. Render

Render directly into the docs assets path in the repository checkout so the file can be included in the pull request:

```bash
mkdir -p docs/src/assets/archivx
python3 "${GLOWMOTION_SCRIPTS}/layout.py" /tmp/gh-aw/agent/glowmotion-graph.json --render docs/src/assets/archivx/agentic-workflows-archivx.svg
```

### 3c. Verify

```bash
python3 "${GLOWMOTION_SCRIPTS}/check_diagram.py" docs/src/assets/archivx/agentic-workflows-archivx.svg
```

Fix every violation by editing `/tmp/gh-aw/agent/glowmotion-graph.json` and re-running the Step 3b render command (which rewrites the repository SVG) until the checker prints `0 violations`.

### 3d. Inspect Rendered Readability

The generated `.svg` is an HTML document containing an SVG, so serve `docs/src/assets/archivx/` locally and use `playwright-cli` to inspect `agentic-workflows-archivx.svg` in a browser.

1. Capture a full-page desktop screenshot and inspect the accessibility snapshot.
2. Use browser evaluation to find text that is clipped outside its containing SVG/card bounds, overlaps another label, or is smaller than 10px.
3. Calculate WCAG contrast for every visible text element against its effective immediate background. Require at least 4.5:1 for normal text and 3:1 for large text. Check the initial theme and the theme-toggle state.
4. If any check fails, revise only `/tmp/gh-aw/agent/glowmotion-graph.json`, rerender, rerun `check_diagram.py`, and repeat this inspection until every check passes.

## Step 4: Create the Diagram Pull Request

Confirm the generated file exists:

```bash
ls -l docs/src/assets/archivx/agentic-workflows-archivx.svg
```

Then call `create_pull_request` **exactly once** with a title like `[archivx] Update workflow diagram — YYYY-MM-DD` and a body summarizing the data range, main findings, and `check_diagram.py` plus Playwright readability validation results. The pull request must include only `docs/src/assets/archivx/agentic-workflows-archivx.svg`.

## Step 5: Create Discussion

Create a discussion titled `[archivx] Agentic Workflow Visual Summary — YYYY-MM-DD`.

**Comment Formatting**: Use h3 (`###`) or lower for all headers. Wrap long content with `<details><summary>View Details</summary>...</details>`.

### Discussion Body Structure

```
### Overview

2-3 sentence narrative of workflow health for the observed range, explicitly stating complete, partial, or unknown coverage of the requested 7 days and any limiting reason. Do not extrapolate partial results to weekly health.

---

### Key Metrics

| Metric | Value |
|---|---|
| Coverage | Complete / Partial / Unknown; observed UTC range and temporal-span percentage |
| Total Runs | N |
| Success Rate | N% |
| Top Engine | engine-name |
| Most Active Workflow | workflow-name |

---

### Diagram

The SVG architecture diagram has been committed to `docs/src/assets/archivx/agentic-workflows-archivx.svg` in the generated pull request.

> The SVG can be embedded directly in docs pages using standard markdown image syntax.

---

### Top Failures

(collapsible — top 3 failing workflows with failure counts)

---

### Workflow Activity

(collapsible — table of top 10 workflows with run counts and success rates)
```