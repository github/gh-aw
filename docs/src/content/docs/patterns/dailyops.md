---
title: DailyOps
description: Scheduled workflows for incremental daily improvements - small automated changes that compound over time
sidebar:
  badge: { text: 'Scheduled', variant: 'tip' }
---

DailyOps workflows automate incremental progress toward large goals through small, scheduled daily changes. Work happens automatically in manageable pieces that are easy to review and integrate.

## When to Use DailyOps

- **Continuous improvement** - Daily code quality improvements
- **Progressive migrations** - Gradually update dependencies or patterns
- **Documentation maintenance** - Keep docs fresh with daily updates
- **Technical debt** - Chip away at issues one small PR at a time

## The DailyOps Pattern

### Scheduled Execution

Workflows run on weekday schedules (avoiding weekends) with `workflow_dispatch` enabled for manual testing:

```aw wrap
---
on:
  schedule:
    - cron: "0 2 * * 1-5"  # Weekdays only (no short syntax available)
  workflow_dispatch:
---
```

### Phased Approach

Work proceeds through three phases with maintainer approval between each:
1. **Research** - Analyze state, create discussion with findings
2. **Configuration** - Define steps, create config PR
3. **Execution** - Make improvements, verify, create draft PRs

### Progress Tracking

Use GitHub discussions to maintain continuity across runs. The workflow creates a discussion (if none exists) and adds progress comments on subsequent runs:

```aw wrap
safe-outputs:
  create-discussion:
    title-prefix: "${{ github.workflow }}"
    category: "ideas"
```

The [`safe-outputs:`](/gh-aw/reference/safe-outputs/) (validated GitHub operations) configuration lets the AI request discussion creation without requiring write permissions.

### Discussion Comments

For workflows that post updates to an existing discussion, use `add-comment` with `discussion: true` and a specific `target` discussion number:

```aw wrap
safe-outputs:
  add-comment:
    target: "4750"
    discussion: true
```

This pattern is ideal for daily status posts, recurring reports, or community updates. The [daily-fact.md](https://github.com/github/gh-aw/blob/main/.github/workflows/daily-fact.md) workflow demonstrates this by posting daily facts about the repository to a pinned discussion thread.

### Persistent Memory

Enable `cache-memory` to maintain state at `/tmp/gh-aw/cache-memory/` across runs, useful for tracking progress, storing metrics, and building knowledge bases over time:

```aw wrap
tools:
  cache-memory: true
```

## Common DailyOps Workflows

This repository implements several DailyOps workflows demonstrating different use cases:

- **daily-fact.md** - Posts daily facts about the repository to a discussion thread
- **daily-test-improver.md** - Systematically adds tests to improve coverage incrementally
- **daily-perf-improver.md** - Identifies and implements performance optimizations
- **daily-doc-updater.md** - Keeps documentation synchronized with merged code changes
- **daily-team-status** (from [agentics](https://github.com/githubnext/agentics)) - Creates daily team status reports with activity summaries
- **daily-repo-chronicle.md** - Produces newspaper-style repository updates
- **daily-firewall-report.md** - Analyzes and reports on firewall activity

All follow the phased approach with discussions for tracking and draft pull requests for review.

## Implementation Guide

1. **Define Goal** - Identify ongoing goal (test coverage, performance, docs sync)
2. **Design Workflow** - Set weekday schedule, configure `safe-outputs` for discussions/PRs
3. **Research Phase** - Analyze state, create discussion, wait for approval
4. **Config Phase** - Create config files, test, submit PR, wait for approval
5. **Execute Daily** - Make small improvements, verify, create draft PRs, update discussion

## Best Practices

- Keep changes reviewable in 5-10 minutes
- Use draft PRs to signal review needed
- Track progress and issues in discussions
- Handle failures by creating issues and exiting cleanly
- Enable `workflow_dispatch` for manual testing
- Schedule weekdays only to avoid weekend buildup


## Related Patterns

- **IssueOps** - Trigger workflows from issue creation or comments
- **ChatOps** - Trigger workflows from slash commands in comments
- **LabelOps** - Trigger workflows when labels change on issues or pull requests
- **Planning Workflow** - Use `/plan` command to split large discussions into actionable work items, then assign sub-tasks to Copilot for execution

DailyOps complements these patterns by providing scheduled automation that doesn't require manual triggers.
