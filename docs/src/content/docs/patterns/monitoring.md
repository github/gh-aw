---
title: Projects & Monitoring
description: Use GitHub Projects + safe-outputs to track and monitor workflow work items and progress.
---

Use this pattern when you want a durable “source of truth” for what your agentic workflows discovered, decided, and did.

## What this pattern is

- **Projects** are the dashboard: a GitHub Projects v2 board holds issues/PRs and custom fields.
- **Monitoring** is the behavior: workflows continuously add/update items, and periodically post status updates.

## Building blocks

### 1) Track items with `update-project`

Enable the safe output and point it at your project URL:

```yaml
safe-outputs:
  update-project:
    project: https://github.com/orgs/myorg/projects/123
    max: 10
    github-token: ${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}
```

- Adds issues/PRs to the board and updates custom fields.
- Can also create views and custom fields when configured.

See the full reference: [/reference/safe-outputs/#project-board-updates-update-project](/gh-aw/reference/safe-outputs/#project-board-updates-update-project)

### 2) Post run summaries with `create-project-status-update`

Use project status updates to communicate progress and next steps:

```yaml
safe-outputs:
  create-project-status-update:
    project: https://github.com/orgs/myorg/projects/123
    max: 1
    github-token: ${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}
```

This is useful for scheduled workflows (daily/weekly) or orchestrator workflows.

See the full reference: [/reference/safe-outputs/#project-status-updates-create-project-status-update](/gh-aw/reference/safe-outputs/#project-status-updates-create-project-status-update)

### 3) Correlate work with a Tracker Id field

If you want to correlate multiple runs, add a custom field like **Tracker Id** (text) and populate it from your workflow prompt/output (for example, a run ID, issue number, or “initiative” key).

## Operational monitoring

- Use `gh aw status` to see which workflows are enabled and their latest run state.
- Use `gh aw logs` and `gh aw audit` to inspect tool usage, errors, MCP failures, and network patterns.

See: [/setup/cli/](/gh-aw/setup/cli/)
