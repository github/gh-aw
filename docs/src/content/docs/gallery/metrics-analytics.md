---
title: Automated workflow metrics and analytics
description: Use a scheduled gh-aw workflow to record repository workflow health and performance data automatically.
---

Metrics and analytics with GitHub Agentic Workflows means collecting recent GitHub Actions activity on a schedule and storing a compact snapshot for later trend analysis. This example works with any repository that uses GitHub Actions.

```aw wrap title=".github/workflows/metrics-collector.md"
---
on:
  schedule: daily

permissions:
  actions: read
  contents: read
  issues: read
  pull-requests: read

tools:
  repo-memory:
    branch-name: memory/workflow-metrics
    file-glob: "metrics/**"

safe-outputs:
  noop:
---

# Metrics Collector

Collect GitHub Actions activity from the last 24 hours. For each workflow, record total, successful, failed, cancelled, and skipped runs, plus execution duration and queue time when available.

Write a compact JSON snapshot to `metrics/YYYY-MM-DD.json` in repo memory. Include the collection window, repository and workflow names, and any missing or incomplete data. Do not create issues, comments, or pull requests.
```

Repository memory lets later workflows compare snapshots without adding reports to the default branch. The agent receives read-only GitHub permissions and has only the `noop` safe output, so collection cannot directly mutate issues or pull requests.

## Learn More

- [Repository memory](/gh-aw/reference/repo-memory/)
- [Audit commands](/gh-aw/reference/audit/)
- [Cost management](/gh-aw/reference/cost-management/)
