---
private: true
emoji: "🦛"
name: Daily Hippo Learn
description: Runs hippo-memory's learn and sleep commands daily to extract lessons from git commits, consolidate the memory store, and suggest actionable improvements to the team
on:
  schedule:
    - cron: "daily around 7:00"
  workflow_dispatch:

max-daily-ai-credits: 10000
permissions:
  contents: read
  issues: read
  pull-requests: read
  discussions: read

  copilot-requests: write
tracker-id: daily-hippo-learn
model: openai/gpt-5.4
engine:
  id: pi
  model-provider: openai

timeout-minutes: 30

runtimes:
  node:
    version: "22"

network:
  allowed:
    - defaults
    - node

sandbox:
  agent:
    id: awf
tools:
  cli-proxy: true
  bash:
    - "*"
  github:
    mode: gh-proxy
    toolsets: [default]

safe-outputs:
  create-discussion:
    expires: 3d
    category: "audits"
    title-prefix: "🦛 "
    close-older-discussions: true
    max: 1
  create-issue:
    expires: 3d
    labels: [automated-analysis, cookie]
    title-prefix: "[hippo] "
    max: 3

imports:
  - shared/hippo-memory.md
  - shared/reporting.md
  - shared/otlp.md
features:
  gh-aw-detection: true
evals:
  - id: hippo_learn_executed
    question: Did the agent run hippo-memory's learn and sleep commands on recent git commits?
  - id: improvements_suggested
    question: Were actionable improvement suggestions produced from the consolidated memory store?
---

{{#runtime-import? .github/shared-instructions.md}}

# Daily Hippo Memory Learn

You are an AI agent responsible for keeping the project's memory store fresh and
surfacing actionable lessons for the team.

## Context

- **Repository**: ${{ github.repository }}
- **Date**: run `date +%Y-%m-%d` in bash to get today's date
- **Workspace**: ${{ github.workspace }}
- **Memory store**: `.hippo/` (persisted in cache-memory across runs)

## Step 1 — Learn from recent commits

Use `mcpscripts` from bash with the `hippo` tool:

```
mcpscripts hippo --args "learn --git"
```

This scans recent git commits and extracts structural lessons (migrations, breaking
changes, recurring patterns) into the memory store without a full consolidation pass.

## Step 2 — Full consolidation

```
mcpscripts hippo --args "sleep"
```

This runs the complete cycle: learn from commits, import any `MEMORY.md` files,
consolidate by applying decay, merge near-duplicates, and promote high-value lessons
to the global store.

## Step 2.5 — Refresh embeddings

Keep the vector index current so semantic recall stays sharp. Run after every sleep
cycle to embed any memories that were added or updated since the last embed pass:

```
mcpscripts hippo --args "embed"
```

This is fast for incremental updates (only unembedded memories are processed).

## Step 3 — Recall top insights

Recall memories across these four lenses (run each separately):

```
mcpscripts hippo --args 'recall "errors and bugs" --budget 3000'
mcpscripts hippo --args 'recall "code quality and refactoring" --budget 3000'
mcpscripts hippo --args 'recall "CI and workflow improvements" --budget 3000'
mcpscripts hippo --args 'recall "architectural decisions" --budget 2000'
```

Also export a full snapshot for analysis:

```
mcpscripts hippo --args "export"
```

## Step 4 — Analyse and suggest improvements

Review all recalled memories and the export. Produce a structured analysis covering:

1. **Error patterns** — recurring bugs or mistakes the team has hit more than once;
   what preventive measures would eliminate them
2. **Code quality** — technical debt, missing tests, anti-patterns the memory store
   has flagged; specific files or packages worth addressing
3. **CI / workflow health** — patterns of flaky tests, slow jobs, or broken workflows
   visible in the memories
4. **Quick wins** — the top 3–5 highest-confidence improvements that could be
   delivered within a day or two, with a clear rationale from the memory
5. **Longer-term themes** — patterns that appear multiple times, suggesting systemic
   issues worth a dedicated effort

## Step 5 — Create discussion

Create a GitHub discussion with today's findings using this title format
(replace `{YYYY-MM-DD}` with the date you obtained from the bash command above):

```
Hippo Memory Insights — {YYYY-MM-DD}
```

Structure the body as follows (use `###` headers per the reporting guidelines):

### Summary
- Memories in store: N
- New lessons learned from commits today: N
- Highest-confidence memory: …

### Top Memories Surfaced
List the 5–7 memories with the highest confidence / relevance scores, one per line.

### Suggested Improvements
One subsection per category (Error Patterns, Code Quality, CI Health, Quick Wins,
Longer-term Themes). Keep each point specific and actionable — include file paths,
function names, or workflow names where the memory store provides them.

### Memory Health
Any stale, very-low-confidence, or duplicated memories worth pruning. Run
`mcpscripts hippo --args "list"` to get counts.

---

Keep the report concise and focused on what the team can act on. Wrap verbose
memory lists in `<details>` tags to reduce scrolling.

## Step 6 — Create quick-win issues

From the **Quick Wins** section of your analysis, pick the top 3 highest-confidence,
most actionable improvements that a developer could complete within a day or two.
For each one, create a separate GitHub issue using the `create_issue` safe-output tool.

Each issue should:
- Have a short, specific title describing the improvement
- Include the memory ID(s) that support it (e.g. `Memory: <id>`) for traceability
- Explain the rationale in 2–3 sentences backed by the recalled memories
- Describe concrete next steps (commands to run, files to edit, etc.)
- Not duplicate an issue already open in the repository

Do **not** create more than 3 issues. If fewer than 3 compelling quick wins exist,
create only as many as are genuinely actionable.

**Important**: If no action is needed after completing your analysis, you **MUST**
call the `noop` safe-output tool with a brief explanation. Failing to call any
safe-output tool is the most common cause of safe-output workflow failures.

```json
{"noop": {"message": "No action needed: [brief explanation of what was analyzed and why]"}}
```

### Output Format

Structure reports as: overview → key metrics/issues → collapsible detail → next actions.