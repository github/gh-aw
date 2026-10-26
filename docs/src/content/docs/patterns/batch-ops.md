---
title: BatchOps
description: Process large volumes of work in parallel or chunked batches using matrix jobs, rate-limit-aware throttling, and result aggregation
sidebar:
  badge: { text: 'Batch processing', variant: 'caution' }
---

BatchOps processes large volumes of work items by splitting them into chunks, parallelizing where possible, tolerating partial failures, and aggregating results into one report.

```mermaid
flowchart LR
    trigger([Trigger]) --> workers[Parallel batch workers]
    workers --> agg[Aggregate results]
```

## When to Use BatchOps

| Scenario | Recommendation |
|----------|----------------|
| < 50 items, order matters, or items depend on each other | Sequential ([WorkQueueOps](/gh-aw/patterns/workqueue-ops/)) |
| 50–500 independent items | BatchOps with chunked processing |
| > 500 items, high parallelism safe | BatchOps with matrix fan-out |
| Strict rate limits or quotas | Rate-limit-aware batching |

## Batch Strategy 1: Chunked Processing

Split work into fixed-size pages using `GITHUB_RUN_NUMBER`; each scheduled run processes the next page. Items need a stable sort key (creation date, issue number) so pagination is deterministic.

```mermaid
flowchart LR
    run([Run N]) --> fetch[Fetch page N]
    fetch --> agent[AI processes batch]
    agent --> next([Run N+1, next page])
```

```aw wrap title=".github/workflows/stale-processor.md"
---
on:
  schedule: daily on weekdays
  workflow_dispatch:

tools:
  github:
    toolsets: [issues]
  bash:
    - "jq"
    - "date"

safe-outputs:
  add-labels:
    allowed: [stale, needs-triage, archived]
    max: 30
  add-comment:
    max: 30

steps:
  - name: compute-page
    id: compute-page
    run: |
      PAGE_SIZE=25
      # Use run number mod to cycle through pages; reset every 1000 runs
      PAGE=$(( (GITHUB_RUN_NUMBER % 1000) * PAGE_SIZE ))
      echo "page_offset=$PAGE" >> "$GITHUB_OUTPUT"
      echo "page_size=$PAGE_SIZE" >> "$GITHUB_OUTPUT"
---

# Chunked Issue Processor

This run covers offset ${{ steps.compute-page.outputs.page_offset }} with page size ${{ steps.compute-page.outputs.page_size }}.

1. List issues sorted by creation date (oldest first), skipping the first ${{ steps.compute-page.outputs.page_offset }} and taking ${{ steps.compute-page.outputs.page_size }}.
2. Add `stale` (with a warning comment) if last updated > 90 days ago; add `needs-triage` if unlabeled.
3. Summarize labels, comments, and errors.
```

## Batch Strategy 2: Fan-Out with Matrix

Run parallel workers with a GitHub Actions matrix, each on a non-overlapping shard. Set `fail-fast: false` so one failure doesn't cancel the others.

```mermaid
flowchart LR
    trigger([Trigger]) --> s0[Shard 0]
    trigger --> s1[Shard 1]
    trigger --> s2[Shard 2]
```

```aw wrap title=".github/workflows/batch-worker.md"
---
on:
  workflow_dispatch:
    inputs:
      total_shards:
        description: "Number of parallel workers"
        default: "4"
        required: false

jobs:
  batch:
    strategy:
      matrix:
        shard: [0, 1, 2, 3]
      fail-fast: false   # Continue other shards even if one fails

tools:
  github:
    toolsets: [issues, pull_requests]

safe-outputs:
  add-labels:
    allowed: [reviewed, duplicate, wontfix]
    max: 50
---

# Matrix Batch Worker — Shard ${{ matrix.shard }} of ${{ inputs.total_shards }}

Process only issues where `(issue_number % ${{ inputs.total_shards }}) == ${{ matrix.shard }}` — this ensures no two shards process the same issue.

1. List open issues (up to 500) and keep this shard's.
2. Label each `reviewed`; if it duplicates another, also add `duplicate` and reference the original.
3. Report shard size, labeled count, and failures.
```

## Batch Strategy 3: Rate-Limit-Aware Batching

Process items in small sub-batches with explicit pauses: slower, but far fewer rate-limit errors. See [Rate Limiting Controls](/gh-aw/reference/rate-limiting-controls/) for built-in throttling.

```mermaid
flowchart LR
    trigger([Trigger]) --> batch[Process sub-batch]
    batch --> pause[Pause between batches]
    pause --> report[Report totals]
```

```aw wrap title=".github/workflows/rate-limited-batch.md"
---
on:
  workflow_dispatch:
    inputs:
      batch_size:
        description: "Items per sub-batch"
        default: "10"
      pause_seconds:
        description: "Seconds to pause between sub-batches"
        default: "30"

tools:
  github:
    toolsets: [repos, issues]
  bash:
    - "sleep"
    - "jq"

safe-outputs:
  add-comment:
    max: 100
  add-labels:
    allowed: [labeled-by-bot]
    max: 100
---

# Rate-Limited Batch Processor

Process all open issues in sub-batches of ${{ inputs.batch_size }}, pausing ${{ inputs.pause_seconds }} seconds between batches.

1. Fetch all open issue numbers (paginate if needed).
2. For each sub-batch: read each issue, add the correct label, then pause.
3. On HTTP 429: pause 60 seconds, retry once, then mark failed.
4. Report processed, failed, and skipped totals.
```

## Batch Strategy 4: Result Aggregation

Aggregate results from multiple workers or runs into one summary issue, using [cache-memory](/gh-aw/reference/cache-memory/) for intermediate results across days.

```mermaid
flowchart LR
    runs[Past batch runs] --> cache[cache-memory]
    cache --> agent[AI agent]
    agent --> issue[Update tracking issue]
```

```aw wrap title=".github/workflows/batch-aggregator.md"
---
on:
  workflow_dispatch:
    inputs:
      report_issue:
        description: "Issue number to aggregate results into"
        required: true

tools:
  cache-memory: true
  github:
    toolsets: [issues, repos]
  bash:
    - "jq"

safe-outputs:
  add-comment:
    max: 1
  update-issue:
    body: true

steps:
  - name: collect-results
    run: |
      # Aggregate results from all result files written by previous batch runs
      RESULTS_DIR="/tmp/gh-aw/cache-memory/batch-results"
      if [ -d "$RESULTS_DIR" ]; then
        jq -s '
          {
            total_processed: (map(.processed) | add // 0),
            total_failed: (map(.failed) | add // 0),
            total_skipped: (map(.skipped) | add // 0),
            runs: length,
            errors: (map(.errors // []) | add // [])
          }
        ' "$RESULTS_DIR"/*.json > /tmp/gh-aw/cache-memory/aggregate.json
        cat /tmp/gh-aw/cache-memory/aggregate.json
      else
        echo '{"total_processed":0,"total_failed":0,"total_skipped":0,"runs":0,"errors":[]}' \
          > /tmp/gh-aw/cache-memory/aggregate.json
      fi
---

# Batch Result Aggregator

Aggregate results from previous batch runs stored in `/tmp/gh-aw/cache-memory/batch-results/` into issue #${{ inputs.report_issue }}.

1. Read `aggregate.json` for totals and the result files for per-run breakdowns.
2. Update issue #${{ inputs.report_issue }} with a Markdown table (summary row plus per-run breakdown) and any errors needing manual intervention.
3. Comment "Batch complete ✅", or "Batch complete with failures ⚠️" listing failed items.
4. Create a sub-issue for each failed item so it can be retried.
```

## Learn More

- [WorkQueueOps](/gh-aw/patterns/workqueue-ops/) — Sequential queue processing with issue checklists, sub-issues, cache-memory, and Discussions
- [ResearchPlanAssignOps](/gh-aw/patterns/research-plan-assign-ops/) — Research → Plan → Assign for developer-supervised work
- [Cache Memory](/gh-aw/reference/cache-memory/) — Persistent state storage across workflow runs
- [Repo Memory](/gh-aw/reference/repo-memory/) — Git-committed persistent state
- [Rate Limiting Controls](/gh-aw/reference/rate-limiting-controls/) — Built-in throttling for API-heavy workflows
- [Concurrency](/gh-aw/reference/concurrency/) — Prevent overlapping batch runs
