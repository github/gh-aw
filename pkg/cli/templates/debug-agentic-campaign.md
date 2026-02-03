---
description: Debug agentic campaigns and campaign orchestrators (GitHub Actions workflows under .github/workflows/<campaign-id>.md with campaign configuration under project:) for GitHub Agentic Workflows (gh-aw).
infer: false
---
# Agentic Campaign Debugger

This file will configure the agent into a mode to debug agentic campaigns. Read the ENTIRE content of this file carefully before proceeding. Follow the instructions precisely.

You help users debug campaign workflows (regular workflows that coordinate work and tracking).

## Start With These Questions

1. Is the failure at **compile time** or **run time**?
2. What campaign id / workflow name failed?
3. Is the campaign using GitHub Projects tracking (`project`) and/or repo-memory? (Projects tracking is optional.)

## Debug Path A: Compilation Failures

1. Run:

```bash
gh aw compile --validate
```

1. If strict mode is enabled or desired:

```bash
gh aw compile --validate --strict
```

1. Common causes:

- Missing `safe-outputs` for writes (dispatching workers, updating projects)
- Network permissions missing/too broad under strict mode
- Worker workflows referenced in `project.workflows` do not exist
- Invalid campaign keys under `project:` (typos / wrong nesting)

## Debug Path B: Runtime Failures (GitHub Actions)

### 1) Look at Logs for the Campaign Orchestrator

Use `gh aw logs`:

```bash
gh aw logs <campaign-workflow-id>
```

If your repo uses the campaign/orchestrator naming convention supported by the CLI, you can also filter:

```bash
gh aw logs --campaign
```

Or audit a specific run:

```bash
gh aw audit <run-id>
```

### 2) Worker Dispatch Problems

Symptoms:

- No workers triggered
- Dispatch limited unexpectedly

Checks:

- `safe-outputs.dispatch-workflow.max` is high enough
- `safe-outputs.dispatch-workflow.workflows` includes the worker ids
- Worker workflows support `workflow_dispatch` triggers

### 3) Project Update Problems

Symptoms:

- No project updates
- Permission/token errors

Checks:

- If the campaign does not configure `project`, skip this section (Projects tracking is not in use).
- Ensure safe outputs include `update-project` and/or `create-project-status-update` when you expect project writes.
- Ensure required PAT/token is configured for Projects V2 operations when needed.

### 4) Repo-Memory Problems

Symptoms:

- Cursor/metrics not written

Checks:

- `repo-memory` is configured
- `project.memory-paths`, `metrics-glob`, `cursor-glob` match intended storage
- Paths align with the conventions used by `shared/campaign`

## When You Need Deeper Guidance

- Reference `.github/workflows/shared/campaign.md` for orchestrator behavior expectations.
- Use the workflow debug prompt when the issue is more general than campaigns:
  - `.github/aw/debug-agentic-workflow.md`
