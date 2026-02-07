---
title: MemoryOps
description: Techniques for using cache-memory and repo-memory to build stateful workflows that track progress, share data, and compute trends
sidebar:
  badge: { text: 'Patterns', variant: 'note' }
---

MemoryOps enables workflows to persist state across runs using `cache-memory` and `repo-memory`. Build workflows that remember their progress, resume after interruptions, share data between workflows, and avoid API throttling.

Use MemoryOps for incremental processing, trend analysis, multi-step tasks, and workflow coordination.

## How to Use These Patterns

> [!TIP]
> **Let the AI Agent Do the Work**
>
> When using these patterns, **state your high-level goal** in the workflow prompt and let the AI agent generate the concrete implementation. The patterns below are conceptual guides - you don't need to write the detailed code yourself.
>
> **Example approach:**
>
> ```markdown
> # Process All Open Issues
> 
> Analyze all open issues in the repository. Use cache-memory to track which 
> issues you've already processed so you can resume if interrupted. For each 
> issue, extract sentiment and priority, then generate a summary report.
> ```
>
> The agent will see the cache-memory configuration in your frontmatter and implement the todo/done tracking pattern automatically based on your goal.

## Memory Types

### Cache Memory

Fast, ephemeral storage using GitHub Actions cache (7 days retention):

```yaml
tools:
  cache-memory:
    key: my-workflow-state
```

**Use for**: Temporary state, session data, short-term caching  
**Location**: `/tmp/gh-aw/cache-memory/`

### Repository Memory

Persistent, version-controlled storage in a dedicated Git branch:

```yaml
tools:
  repo-memory:
    branch-name: memory/my-workflow
    file-glob: ["*.json", "*.jsonl"]
```

**Use for**: Historical data, trend tracking, permanent state  
**Location**: `/tmp/gh-aw/repo-memory/default/`

## Pattern 1: Exhaustive Processing

Track progress through large datasets with todo/done lists to ensure complete coverage across multiple runs.

**Your goal**: "Process all items in a collection, tracking which ones are done so I can resume if interrupted."

**How to state it in your workflow**:
```markdown
Analyze all open issues in the repository. Track your progress in cache-memory 
so you can resume if the workflow times out. Mark each issue as done after 
processing it. Generate a final report with statistics.
```

**What the agent will implement**: Maintain a state file with items to process (`todo`) and completed items (`done`). After processing each item, immediately update the state so the workflow can resume if interrupted.

**Example structure the agent might use**:
```json
{
  "todo": [123, 456, 789],
  "done": [101, 102],
  "errors": [],
  "last_run": 1705334400
}
```

**Real examples**: `.github/workflows/repository-quality-improver.md`, `.github/workflows/copilot-agent-analysis.md`

## Pattern 2: State Persistence

Save workflow checkpoints to resume long-running tasks that may timeout.

**Your goal**: "Process data in batches, saving progress so I can continue where I left off in the next run."

**How to state it in your workflow**:
```markdown
Migrate 10,000 records from the old format to the new format. Process 500 
records per run and save a checkpoint. Each run should resume from the last 
checkpoint until all records are migrated.
```

**What the agent will implement**: Store a checkpoint with the last processed position. Each run loads the checkpoint, processes a batch, then saves the new position.

**Example checkpoint the agent might use**:
```json
{
  "last_processed_id": 1250,
  "batch_number": 13,
  "total_migrated": 1250,
  "status": "in_progress"
}
```

**Real examples**: `.github/workflows/daily-news.md`, `.github/workflows/cli-consistency-checker.md`

## Pattern 3: Shared Information

Share data between workflows using repo-memory branches.

**Your goal**: "Collect data in one workflow and analyze it in other workflows."

**How to state it in your workflow**:

*Producer workflow:*
```markdown
Every 6 hours, collect repository metrics (issues, PRs, stars) and store them 
in repo-memory so other workflows can analyze the data later.
```

*Consumer workflow:*
```markdown
Load the historical metrics from repo-memory and compute weekly trends. 
Generate a trend report with visualizations.
```

**What the agent will implement**: One workflow (producer) collects data and stores it in repo-memory. Other workflows (consumers) read and analyze the shared data using the same branch name.

**Configuration both workflows need**:
```yaml
tools:
  repo-memory:
    branch-name: memory/shared-data  # Same branch for producer and consumer
```

**Real examples**: `.github/workflows/metrics-collector.md` (producer), trend analysis workflows (consumers)

## Pattern 4: Data Caching

Cache API responses to avoid rate limits and reduce workflow time.

**Your goal**: "Avoid hitting rate limits by caching API responses that don't change frequently."

**How to state it in your workflow**:
```markdown
Fetch repository metadata and contributor lists. Cache the data for 24 hours 
to avoid repeated API calls. If the cache is fresh, use it. Otherwise, fetch 
new data and update the cache.
```

**What the agent will implement**: Before making expensive API calls, check if cached data exists and is fresh. If cache is valid (based on TTL), use cached data. Otherwise, fetch fresh data and update cache.

**TTL guidelines to include in your prompt**:
- Repository metadata: 24 hours
- Contributor lists: 12 hours
- Issues/PRs: 1 hour
- Workflow runs: 30 minutes

**Real examples**: `.github/workflows/daily-news.md`

## Pattern 5: Trend Computation

Store time-series data and compute trends, moving averages, and statistics.

**Your goal**: "Track metrics over time and identify trends."

**How to state it in your workflow**:
```markdown
Collect daily build times and test times. Store them in repo-memory as 
time-series data. Compute 7-day and 30-day moving averages. Generate trend 
charts showing whether performance is improving or declining over time.
```

**What the agent will implement**: Append new data points to a history file (JSON Lines format). Load historical data to compute trends, moving averages, and generate visualizations using Python.

**Real examples**: `.github/workflows/daily-code-metrics.md`, `.github/workflows/shared/charts-with-trending.md`

## Pattern 6: Multiple Memory Stores

Use multiple memory instances for different purposes and retention policies.

**Your goal**: "Organize data with different lifecycles - temporary session data, historical metrics, configuration, and archived snapshots."

**How to state it in your workflow**:

```markdown
Use cache-memory for temporary API responses during this run. Store daily 
metrics in one repo-memory branch for trend analysis. Keep data schemas in 
another branch. Archive full snapshots in a third branch with compression.
```

**What the agent will implement**: Separate hot data (cache-memory) from historical data (repo-memory). Use different repo-memory branches for metrics vs. configuration vs. archives.

**Configuration to include**:

```yaml
tools:
  cache-memory:
    key: session-data  # Fast, temporary
  
  repo-memory:
    - id: metrics
      branch-name: memory/metrics  # Time-series data
    
    - id: config
      branch-name: memory/config  # Schema and metadata
    
    - id: archive
      branch-name: memory/archive  # Compressed backups
```

## Best Practices

### Use JSON Lines for Time-Series Data

Append-only format ideal for logs and metrics:

```bash
# Append without reading entire file
echo '{"date": "2024-01-15", "value": 42}' >> data.jsonl
```

### Include Metadata

Document your data structure:

```json
{
  "dataset": "performance-metrics",
  "schema": {
    "date": "YYYY-MM-DD",
    "value": "integer"
  },
  "retention": "90 days"
}
```

### Implement Data Rotation

Prevent unbounded growth:

```bash
# Keep only last 90 entries
tail -n 90 history.jsonl > history-trimmed.jsonl
mv history-trimmed.jsonl history.jsonl
```

### Validate State

Check integrity before processing:

```bash
if [ -f state.json ] && jq empty state.json 2>/dev/null; then
  echo "Valid state"
else
  echo "Corrupt state, reinitializing..."
  echo '{}' > state.json
fi
```

## Security Considerations

> [!CAUTION]
> **Sensitive Data**
>
> Memory stores are visible to anyone with repository access:
> - **Never store**: Credentials, API tokens, PII, secrets
> - **Store only**: Aggregate statistics, anonymized data
> - Consider encryption for sensitive but non-secret data

**Safe practices**:
```bash
# ✅ GOOD - Aggregate statistics
echo '{"open_issues": 42}' > metrics.json

# ❌ BAD - Individual user data
echo '{"user": "alice", "email": "alice@example.com"}' > users.json
```

## Troubleshooting

**Cache not persisting**: Verify cache key is consistent across runs

**Repo memory not updating**: Check `file-glob` patterns match your files and files are within `max-file-size` limit

**Out of memory errors**: Process data in chunks instead of loading entirely, implement data rotation

**Merge conflicts**: Use JSON Lines format (append-only), separate branches per workflow, or add run ID to filenames

## Related Documentation

- [MCP Servers](/gh-aw/guides/mcps/) - Memory MCP server configuration
- [Deterministic Patterns](/gh-aw/guides/deterministic-agentic-patterns/) - Data preprocessing
- [Safe Outputs](/gh-aw/reference/custom-safe-outputs/) - Storing workflow outputs
- [Frontmatter Reference](/gh-aw/reference/frontmatter/) - Configuration options
