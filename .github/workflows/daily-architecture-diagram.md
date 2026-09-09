---
private: true
emoji: "🏗️"
description: Generates a weekly high-level ASCII architecture diagram of the repository, using cache-memory to focus only on what changed since the last run.
on:
  schedule: weekly on Monday around 08:00
  workflow_dispatch:

max-daily-ai-credits: 10000
permissions:
  contents: read
  issues: read
  pull-requests: read

  copilot-requests: write
engine:
  id: codex
model: copilot/gpt-5.3-codex

experiments:
  detail_level:
    variants: [brief, comprehensive]
    description: "Tests whether a leaner output format (diagram + brief change log only) delivers equivalent reader value vs. the full report with Package Reference table and verbose summaries."
    hypothesis: "H0: no change in run success rate or issue engagement. H1: brief variant reduces token usage ≥20% and run duration ≥15% with no drop in success rate."
    metric: run_duration_ms
    secondary_metrics: [output_issue_body_length, run_success_rate]
    guardrail_metrics:
      - name: run_success_rate
        threshold: ">=0.85"
    min_samples: 10
    weight: [50, 50]
    start_date: "2026-05-19"
    analysis_type: mann_whitney
    tags: [output-format, cost, latency]
    issue: 31926

sandbox:
  agent:
    id: awf
    runtime: cloud-hypervisor
tools:
  cli-proxy: true
  edit:
  bash:
    - "*"
  cache-memory: true

safe-outputs:
  create-issue:
    title-prefix: "🏗️ Architecture Diagram:"
    labels: [architecture, diagram]
    close-older-issues: true
    expires: 7d
    max: 1
  steer: true
  create-pull-request:
    expires: 7d
    title-prefix: "[architecture] "
    labels: [architecture, diagram, documentation]
  noop:

imports:
  - uses: shared/daily-audit-base.md
    with:
      title-prefix: "[architecture-diagram] "
      expires: 3d
  - shared/reporting.md

  - shared/otlp.md
timeout-minutes: 20
strict: true
features:
  gh-aw-detection: true
evals:
  - id: architecture_analyzed
    question: Did the agent analyze repository changes to produce an updated architecture diagram?
  - id: diagram_created_or_noop
    question: Was an ASCII architecture diagram created or updated, or was noop used when no significant changes were detected?
---

### Architecture Diagram Generator

You are an AI agent that generates a **high-level ASCII architecture diagram** of this repository, focusing on the layered structure from CLI entry points down to utility packages.

#### Cache Strategy

Before doing any work, check cache-memory for a file named `architecture-state.json`.

### If the cache file exists:

1. Read `architecture-state.json` from cache-memory. It contains:
   - `last_commit`: the last analyzed commit SHA
   - `package_map`: a JSON object mapping each package path to its description and layer
2. Run `git log --oneline <last_commit>..HEAD --name-only` to get the list of files changed since the last run.
3. Check whether `scratchpad/architecture.md` exists and contains diagram content (look for a code block with ASCII art).
4. If **no Go files** (`.go`) changed AND no new directories were added under `pkg/` or `cmd/` AND `scratchpad/architecture.md` exists with diagram content:
   - Call the `noop` safe output with message: "No structural changes since last run (last commit: `<last_commit>`). Architecture diagram is still current."
   - **Stop here.**
5. Otherwise, focus your analysis **only on the changed packages** — re-analyze those and merge the updates into the cached `package_map`.

### If the cache file does NOT exist:

Perform a full analysis of the repository structure (see below).

#### Analysis Steps

Use bash to gather structural information:

```bash
# 1. List all Go packages with their doc comments
find pkg/ cmd/ -name "*.go" -not -name "*_test.go" | head -80

# 2. Get top-level directory structure
ls -d pkg/*/

# 3. For each package, get the package doc comment (first comment block)
for dir in pkg/*/; do
  pkg=$(basename "$dir")
  doc=$(head -20 "$dir"/*.go 2>/dev/null | grep -A2 "^// Package" | head -3)
  echo "=== $pkg === $doc"
done

# 4. Identify import relationships between packages
grep -r '"github.com/github/gh-aw/pkg/' pkg/ --include="*.go" -h | sort -u | head -60
```

#### Diagram Requirements

Generate an ASCII architecture diagram showing **three layers**:

### Layer 1: Entry Points (top)
- `cmd/gh-aw` — main CLI binary
- `cmd/gh-aw-wasm` — WebAssembly target

### Layer 2: Core Packages (middle)
- `pkg/cli` — command implementations
- `pkg/workflow` — workflow compilation engine
- `pkg/parser` — markdown/YAML parsing
- `pkg/console` — terminal UI rendering
- Any other substantial packages discovered

### Layer 3: Utility Packages (bottom)
- `pkg/fileutil`, `pkg/gitutil`, `pkg/stringutil`, `pkg/logger`, etc.
- Group small utilities together

### Diagram Style

Use box-drawing characters to create clean ASCII art:

```
┌─────────────────────────────────────────────┐
│              ENTRY POINTS                    │
│  ┌──────────┐          ┌──────────────┐     │
│  │ cmd/gh-aw│          │cmd/gh-aw-wasm│     │
│  └────┬─────┘          └──────┬───────┘     │
│       │                       │              │
├───────┼───────────────────────┼──────────────┤
│       ▼     CORE PACKAGES    ▼              │
│  ┌────────┐  ┌──────────┐  ┌────────┐      │
│  │  cli   │─▶│ workflow  │─▶│ parser │      │
│  └────┬───┘  └────┬─────┘  └───┬────┘      │
│       │           │             │            │
├───────┼───────────┼─────────────┼────────────┤
│       ▼     UTILITIES          ▼             │
│  ┌────────┐ ┌────────┐ ┌──────────┐        │
│  │fileutil│ │ logger │ │stringutil│ ...     │
│  └────────┘ └────────┘ └──────────┘        │
└─────────────────────────────────────────────┘
```

This is just an example skeleton. Your actual diagram should:
- Reflect the **real** packages and their **actual** dependency arrows
- Show which core packages depend on which utilities
- Be **wide enough** to fit all packages without clutter (use up to 100 characters width)
- Use arrows (─▶, ──▶, ─▷) to indicate dependency direction
- Include a brief one-line description next to or below each core package

#### Updating the Cache

After generating the diagram, write an updated `architecture-state.json` to cache-memory with:

```json
{
  "last_commit": "<current HEAD SHA>",
  "package_map": {
    "cli": { "description": "Command implementations", "layer": "core" },
    "workflow": { "description": "Workflow compilation", "layer": "core" }
  }
}
```

Use a filesystem-safe filename: `architecture-state.json` (no colons or special characters).

#### Output Format

Create an issue with this structure:

### Architecture Diagram

Wrap the ASCII diagram in a `<details><summary><b>View Diagram</b></summary>...</details>` block (with the diagram inside a code block, triple backticks, so it renders with monospace font) so the issue stays short by default.

{{#if experiments.detail_level == 'comprehensive' }}
### Summary

State whether this is a **full rebuild** or an **incremental update**, and list which packages changed.

### Change Log (incremental only)

If this was an incremental update, include a short section listing:
- Packages added/removed/modified since last run
- New dependencies detected
- Any structural shifts (e.g., a utility promoted to core)

### Package Reference

A compact table of all packages with their layer and one-line description, wrapped in a `<details><summary><b>View Package Reference</b></summary>...</details>` block:

| Package | Layer | Description |
|---------|-------|-------------|
| cli | Core | Command implementations |
| workflow | Core | Workflow compilation engine |
| ... | ... | ... |
{{else}}
### Changes (incremental only)

If this was an incremental update, list changed packages in a brief bullet list (max 5 items).
{{/if}}

#### Scratchpad File

After creating the issue, update `scratchpad/architecture.md` with the latest diagram via `create_pull_request`.

The file should contain:

````markdown
# Architecture Diagram

> Last updated: <YYYY-MM-DD> · Source: [Issue #<number>](<issue_url>)

## Overview

This diagram shows the package structure and dependencies of the `gh-aw` codebase.

```
<ASCII diagram>
```

## Package Reference

<package table>
````

- When the diagram **changes**: update `scratchpad/architecture.md` via `create_pull_request` with a PR titled `[architecture] Update architecture diagram - <date>`.
- When the diagram is **unchanged** (noop path): skip the scratchpad update entirely.