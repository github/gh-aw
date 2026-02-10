---
title: DataOps
description: Deterministic data extraction in steps, followed by agentic analysis and reporting
sidebar:
  badge: { text: 'Hybrid', variant: 'caution' }
---

DataOps combines deterministic data extraction with agentic analysis. Shell commands in `steps:` collect and prepare data, then the AI agent in the markdown body analyzes results and produces safe outputs like discussions or comments.

## When to Use DataOps

- **Data aggregation** - Collect metrics from APIs, logs, or repositories
- **Report generation** - Analyze data and produce human-readable summaries
- **Trend analysis** - Process historical data and identify patterns
- **Auditing** - Gather evidence and generate audit reports

## The DataOps Pattern

### Separation of Concerns

DataOps separates two distinct phases:

1. **Deterministic extraction** (`steps:`) - Shell commands that reliably fetch, filter, and structure data. These run before the agent and produce predictable, reproducible results.

2. **Agentic analysis** (markdown body) - The AI agent reads the prepared data, interprets patterns, and generates insights. The agent has access to the data files created by the steps.

This separation ensures data collection is fast, reliable, and cacheable, while the AI focuses on interpretation and communication.

### Basic Structure

```aw wrap
---
on:
  schedule: daily
  workflow_dispatch:

steps:
  - name: Collect data
    run: |
      # Deterministic data extraction
      gh api ... > /tmp/gh-aw/data.json

safe-outputs:
  create-discussion:
    category: "reports"
---

# Analysis Workflow

Analyze the data at `/tmp/gh-aw/data.json` and create a summary report.
```

## Example: PR Activity Summary

This workflow collects statistics from recent pull requests and generates a weekly summary:

````aw wrap
---
name: Weekly PR Summary
description: Summarizes pull request activity from the last week
on:
  schedule: weekly
  workflow_dispatch:

permissions:
  contents: read
  pull-requests: read

engine: copilot
strict: true

network:
  allowed:
    - defaults
    - github

safe-outputs:
  create-discussion:
    title-prefix: "[weekly-summary] "
    category: "announcements"
    max: 1
    close-older-discussions: true

tools:
  bash: ["*"]

steps:
  - name: Fetch recent pull requests
    env:
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
    run: |
      mkdir -p /tmp/gh-aw/pr-data

      # Fetch last 100 PRs with key metadata
      gh pr list \
        --repo "${{ github.repository }}" \
        --state all \
        --limit 100 \
        --json number,title,state,author,createdAt,mergedAt,closedAt,additions,deletions,changedFiles,labels \
        > /tmp/gh-aw/pr-data/recent-prs.json

      echo "Fetched $(jq 'length' /tmp/gh-aw/pr-data/recent-prs.json) PRs"

  - name: Compute summary statistics
    run: |
      cd /tmp/gh-aw/pr-data

      # Generate statistics summary
      jq '{
        total: length,
        merged: [.[] | select(.state == "MERGED")] | length,
        open: [.[] | select(.state == "OPEN")] | length,
        closed: [.[] | select(.state == "CLOSED")] | length,
        total_additions: [.[].additions] | add,
        total_deletions: [.[].deletions] | add,
        total_files_changed: [.[].changedFiles] | add,
        authors: [.[].author.login] | unique | length,
        top_authors: ([.[].author.login] | group_by(.) | map({author: .[0], count: length}) | sort_by(-.count) | .[0:5])
      }' recent-prs.json > stats.json

      echo "Statistics computed:"
      cat stats.json

timeout-minutes: 10
---

# Weekly Pull Request Summary

Generate a summary of pull request activity for the repository.

## Available Data

The following data has been prepared for your analysis:

- `/tmp/gh-aw/pr-data/recent-prs.json` - Last 100 PRs with full metadata
- `/tmp/gh-aw/pr-data/stats.json` - Pre-computed statistics

## Your Task

1. **Read the prepared data** from the files above
2. **Analyze the statistics** to identify:
   - Overall activity levels
   - Merge rate and velocity
   - Most active contributors
   - Code churn (additions vs deletions)
3. **Generate a summary report** as a GitHub discussion with:
   - Key metrics in a clear format
   - Notable trends or observations
   - Top contributors acknowledgment

## Report Format

Create a discussion with this structure:

```markdown
# Weekly PR Summary - [Date Range]

## Key Metrics
- **Total PRs**: X
- **Merged**: X (Y%)
- **Open**: X
- **Code Changes**: +X / -Y lines across Z files

## Top Contributors
1. @author1 - X PRs
2. @author2 - Y PRs
...

## Observations
[Brief insights about activity patterns]
```

Keep the report concise and factual. Focus on the numbers and let them tell the story.
````

## Data Caching

For workflows that run frequently or process large datasets, use caching to avoid redundant API calls:

```aw wrap
---
cache:
  - key: pr-data-${{ github.run_id }}
    path: /tmp/gh-aw/pr-data
    restore-keys: |
      pr-data-

steps:
  - name: Check cache and fetch only new data
    run: |
      if [ -f /tmp/gh-aw/pr-data/recent-prs.json ]; then
        echo "Using cached data"
      else
        gh pr list --limit 100 --json ... > /tmp/gh-aw/pr-data/recent-prs.json
      fi
---
```

## Advanced: Multi-Source Data

Combine data from multiple sources before analysis:

```aw wrap
---
steps:
  - name: Fetch PR data
    run: gh pr list --json ... > /tmp/gh-aw/prs.json

  - name: Fetch issue data
    run: gh issue list --json ... > /tmp/gh-aw/issues.json

  - name: Fetch workflow runs
    run: gh run list --json ... > /tmp/gh-aw/runs.json

  - name: Combine into unified dataset
    run: |
      jq -s '{prs: .[0], issues: .[1], runs: .[2]}' \
        /tmp/gh-aw/prs.json \
        /tmp/gh-aw/issues.json \
        /tmp/gh-aw/runs.json \
        > /tmp/gh-aw/combined.json
---

# Repository Health Report

Analyze the combined data at `/tmp/gh-aw/combined.json` covering:
- Pull request velocity and review times
- Issue response rates and resolution times
- CI/CD success rates and flaky tests
```

## Best Practices

**Keep steps deterministic** - Avoid randomness or time-dependent logic in steps. The same inputs should produce the same outputs.

**Pre-compute aggregations** - Use `jq`, `awk`, or Python in steps to compute statistics. This reduces agent token usage and improves reliability.

**Structure data clearly** - Output JSON with clear field names. Include a summary file alongside raw data.

**Document data locations** - Tell the agent exactly where to find the prepared data and what format to expect.

**Use safe outputs** - Always use `safe-outputs` for agent actions. Discussions are ideal for reports since they support threading and reactions.

## Additional Resources

- [Steps Reference](/gh-aw/reference/frontmatter/#custom-steps-steps) - Shell step configuration
- [Safe Outputs Reference](/gh-aw/reference/safe-outputs/) - Validated GitHub operations
- [Cache Configuration](/gh-aw/reference/memory/) - Caching data between runs
- [DailyOps](/gh-aw/patterns/dailyops/) - Scheduled improvement workflows
