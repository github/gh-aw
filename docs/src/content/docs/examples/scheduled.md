---
title: Scheduled Workflows
description: Workflows that run automatically on a schedule using cron expressions - daily reports, weekly research, and continuous improvement patterns
sidebar:
  order: 1
---

Scheduled workflows run automatically at specified times using cron expressions. They're perfect for recurring tasks like daily status updates, weekly research reports, continuous code improvements, and automated maintenance.

## When to Use Scheduled Workflows

- **Regular reporting**: Daily team status, weekly summaries
- **Continuous improvement**: Incremental code quality improvements (DailyOps)
- **Research & monitoring**: Weekly industry research, dependency updates
- **Maintenance tasks**: Cleaning up stale issues, archiving old discussions

## Patterns in This Section

- **[DailyOps](/gh-aw/patterns/dailyops/)** - Make incremental improvements through small daily changes
- **[Research & Planning](/gh-aw/examples/scheduled/research-planning/)** - Automated research, status reports, and planning

## Example Schedule Triggers

**Recommended: Short fuzzy syntax**
```yaml
on: daily              # Automatically scattered to different time
on: weekly             # Scattered across days and times
on: weekly on monday   # Scattered time on specific day
```

**Traditional cron syntax**
```yaml
on:
  schedule:
    - cron: "0 9 * * 1"      # Every Monday at 9 AM
    - cron: "0 0 * * *"      # Daily at midnight
    - cron: "0 */6 * * *"    # Every 6 hours
```

See the [Schedule Syntax reference](/gh-aw/reference/schedule-syntax/) for complete documentation of all supported formats.

## Quick Start

Add a scheduled workflow to your repository:

```bash
gh aw add-wizard githubnext/agentics/weekly-research
gh aw add-wizard githubnext/agentics/daily-repo-status
```
