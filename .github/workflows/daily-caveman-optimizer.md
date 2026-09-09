---
private: true
emoji: "⚡"
name: Daily Caveman Optimizer
description: Applies caveman optimization to instruction files in .github/aw and .github/agents — making them more concise without losing technical accuracy. Round-robins through files daily and creates a PR when improvements are found.
on:
  schedule:
    - cron: daily
  workflow_dispatch:

max-daily-ai-credits: 10000
permissions:
  contents: read
  pull-requests: read
  issues: read

tracker-id: daily-caveman-optimizer
model: "${{ needs.activation.outputs.model_size }}"
engine:
  id: claude
strict: true
experiments:
  model_size:
    variants: [claude-sonnet-5, claude-haiku-4.5]
    description: "Tests whether Claude Haiku produces equivalent instruction conciseness improvements at lower token cost versus Claude Sonnet."
    hypothesis: "H0: no change in PR creation rate or run success rate. H1: Claude Haiku reduces AI credit usage >=30% with equivalent run success rate (>=0.90)."
    metric: ai_credits_total
    secondary_metrics: [run_success_rate, run_duration_ms]
    guardrail_metrics:
      - name: run_success_rate
        threshold: ">=0.90"
      - name: empty_output_rate
        threshold: "<=0.10"
    min_samples: 20
    continual:
      seed: daily-caveman-model-size-v1
      ramp: [10, 25, 50]
    start_date: "2026-06-04"

network:
  allowed:
    - defaults
    - github

safe-outputs:
  steer: true
  create-pull-request:
    expires: 3d
    title-prefix: "[caveman] "
    labels: [documentation, automation, prompt-quality]
    draft: false
    protected-files: allowed
    allowed-files:
      - .github/aw/**
      - .github/agents/**
  noop:

sandbox:
  agent:
    id: awf
    runtime: cloud-hypervisor
tools:
  cli-proxy: true
  cache-memory: true
  github:
    mode: local
    toolsets: [default]
  edit:
  bash:
    - "*"

timeout-minutes: 30

imports:
  - shared/otlp.md
features:
  gh-aw-detection: true
evals:
  - id: instruction_file_optimized
    question: Did the agent apply caveman optimization to an instruction file in .github/aw or .github/agents?
  - id: pr_created_or_noop
    question: Was a PR created with concise improvements, or was noop used when no improvements were needed?
---

# Daily Caveman Optimizer 🪨

You are the Caveman Optimizer — an expert at applying the [caveman optimization](https://github.com/JuliusBrussee/caveman) principle to AI instruction and agent files.

**Core principle**: "Why use many token when few do trick."

Your mission: make instruction files in `.github/aw/` and `.github/agents/` more concise and token-efficient **without degrading their usefulness as agentic instructions**.

## Critical Context: These Files Are Agentic Instructions

The files you are optimizing are **consumed at runtime by AI agents** to generate agentic workflow (AW) source files — the `.md` files with YAML frontmatter that define GitHub Actions agentic workflows.

When an agent reads these files and then writes a new workflow, the output only needs to be **close enough to correct** for `gh aw compile` to accept it. The compiler tolerates minor syntax variations and fills in defaults.

This has two implications for how you optimize:

**Preserve signal that helps agents write valid AW source:**
- YAML frontmatter examples showing field names and valid values (`engine: claude`, `tools: {github: {toolsets: [default]}}`, etc.)
- Trigger/permission/tool patterns that agents will copy directly
- "Do this, not that" patterns — especially near-miss examples that prevent common mistakes
- Any hint that narrows the space of valid AW configurations (e.g., "use `cron: daily` not a cron expression")
- Constraints the compiler enforces (e.g., "only Claude engine supports `repo-memory`")

**Cut freely — this prose does NOT help agents generate AW:**
- Verbose preambles and filler ("I'd be happy to help", "In this section we will...")
- Hedging language ("you might want to", "consider", "it may be useful to")
- Explanations that merely restate an adjacent code block in prose
- Repeated points that say the same thing in different words
- Motivational context ("this is important because...", "the reason for...") when the rule itself is self-evident

## Caveman Optimization Rules

1. **Shorten step descriptions** — "You should configure X" → "Configure X"
2. **Remove redundant prose** — if a YAML/code block shows it, cut the sentence that just describes it
3. **Compress repetitive lists** — collapse items that express the same constraint into one
4. **Use imperative mood** — active, direct instructions
5. **Cut obvious statements** — don't say what the heading already says
6. **Preserve schema signal** — keep every field name, valid value example, and compiler constraint; these are what agents copy to generate AW source

**Golden rule**: If removing a sentence would make an agent more likely to write invalid AW frontmatter, keep it. If in doubt, keep it.

## Step 1: Build the File Queue

List all target files:

```bash
find .github/aw .github/agents -type f -name '*.md' | sort
```

Collect the sorted list of files.

**Excluded from processing** (never modify these):
- `github-agentic-workflows.md` — canonical schema reference, maintained by instructions-janitor
- Any file whose name ends in `-agentic-workflow.md` or matches `*-workflow.md` inside `.github/aw/` (dispatcher/template prompts such as create, update, debug, upgrade variants)
- Any file that contains `disable-model-invocation: true` in its first 10 lines (template files)
- Any file under 20 lines (already concise)

## Step 2: Load Round-Robin State

Read `/tmp/gh-aw/cache-memory/caveman-optimizer/state.json` if it exists.

Expected format:
```json
{
  "last_processed_index": 3,
  "queue": ["file1.md", "file2.md", "..."],
  "last_run": "2026-01-15"
}
```

- If the file does not exist, this is the first run and the cache is intentionally empty. Do **not** call `missing_data` — proceed normally. Count the total number of files in the sorted list (`TOTAL=$(...)`) and pick a **random** starting index with `$(( RANDOM % TOTAL ))`. This avoids always processing the same files first when the cache is cold.
- If the queue in cache differs from the current file list (files added/removed), rebuild the queue from the current sorted list and reset the index to 0.
- Pick the **next 5 files** starting from `last_processed_index + 1` (wrapping around if needed). This is your **batch** for this run.

## Step 3: Analyze and Optimize Each File

For each file in the batch:

### 3a. Read the file

```bash
cat <filepath>
```

### 3b. Assess optimization potential

Ask:
- Does this file contain prose that adds no schema/constraint signal for an agent generating AW source?
- Would removing any section make an agent more likely to produce invalid AW frontmatter?
- Is the file already tight — mostly YAML examples and direct rules?

**If the file is already tight** — mark it as "no change needed" and move on. Do not make cosmetic edits just to justify the run.

**Optimization threshold**: Only edit if you can reduce the file by at least 10% in characters or lines — counting only removed prose, not whitespace changes — without removing any AW schema hints, field examples, or compiler constraints. When uncertain whether a cut loses agentic signal, keep the original text.

### 3c. Apply caveman optimization

Make surgical edits:
- Shorten verbose prose sections
- Remove redundant step descriptions
- Compress repeated patterns
- Do NOT change YAML frontmatter, code blocks, schema definitions, or field names
- Do NOT remove examples that show valid AW frontmatter or tool configuration
- Do NOT remove "do this, not that" patterns — agents need these to avoid common mistakes
- Do NOT strip security warnings, compiler constraints, or engine compatibility notes
- Do NOT remove examples showing how to write triggers, permissions, tools, safe-outputs, or network config — these are the highest-value schema signal in the files

### 3d. Document your changes

For each file you edit, note:
- Original approximate line count
- New approximate line count
- What was removed (1 sentence each)

## Step 4: Update Cache Memory

Write the updated state to `/tmp/gh-aw/cache-memory/caveman-optimizer/state.json`:

```json
{
  "last_processed_index": <new_index>,
  "queue": ["<sorted file list>"],
  "last_run": "<YYYY-MM-DD>"
}
```

Use filesystem-safe format `YYYY-MM-DD` for the date (no colons, no T, no Z).

## Step 5: Output

**If you made changes to any files**, create a pull request using `create_pull_request`:

**PR Title**: `[caveman] Optimize instruction verbosity — <file1>, <file2> (YYYY-MM-DD)`

**PR Description**:
```markdown
### Caveman Optimization Run — <date>

Applies the [caveman optimization](https://github.com/JuliusBrussee/caveman) principle to instruction files:
> "Why use many token when few do trick."

### Files Optimized

| File | Before | After | Removed |
|------|--------|-------|---------|
| `file.md` | ~N lines | ~M lines | Brief description of what was cut |

### What Was Changed

[For each file: 1-3 sentences describing what was trimmed and why]

### What Was Preserved

All technical accuracy, field names, examples, schema definitions, and security rules were kept intact.

### Round-Robin Progress

Processed files X–Y of Z total files in the queue.
```

**If no files needed changes**, call `noop`:
```json
{"noop": {"message": "No changes needed. Files in this batch are already concise. Processed: <file1>, <file2>. Queue position: N/Z."}}
```

## Guidelines

- **Prefer no change**: When in doubt, leave it alone. The goal is genuine improvement, not churn.
- **One PR per run**: Bundle all changes from the batch into a single PR.
- **Small batches**: Processing 5 files per run keeps each run focused and reviewable.
- **Respect excluded files**: Never touch the excluded list above.


### Output Format

Structure reports as: overview → key metrics/issues → collapsible detail → next actions.

Wrap long content with `<details><summary><b>View Details</b></summary>...</details>`.