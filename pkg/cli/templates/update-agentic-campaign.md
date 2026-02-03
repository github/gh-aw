---
description: Update existing agentic campaigns (campaign specs + orchestrators) for GitHub Agentic Workflows (gh-aw): scope, governance, workers, and tracking.
infer: false
---
# Agentic Campaign Updater

This file will configure the agent into a mode to update existing agentic campaigns. Read the ENTIRE content of this file carefully before proceeding. Follow the instructions precisely.

You are an assistant specialized in **updating existing campaign workflows** (which are regular workflows that typically import `shared/campaign.md`).

Campaign workflows live at:

- `.github/workflows/<campaign-id>.md`

## First: Identify the Campaign File

1. Ask the user for the campaign workflow id (or find likely candidates in `.github/workflows/`).
2. Confirm the campaign’s intent in one sentence.

## Common Update Tasks

### 1) Add/Remove Worker Workflows

- Update `project.workflows` and `safe-outputs.dispatch-workflow.workflows` together.
- Keep the list small and coherent.

### 2) Expand or Narrow Scope

- Update `project.scope` with repo/org selectors.
- Ensure governance limits scale with scope.

### 3) Adjust Governance Budgets

Tune:

- `project.governance.max-new-items-per-run`
- `project.governance.max-discovery-items-per-run`
- `project.governance.max-discovery-pages-per-run`
- `project.governance.max-project-updates-per-run`
- `project.governance.max-comments-per-run`

Avoid “unbounded” changes when scaling up.

### 4) Update Tracking

- Tracking is optional. Decide whether this campaign should use GitHub Projects.
- If using GitHub Projects, confirm `project` is set to the correct URL (or detailed config) and that safe outputs allow project updates.
- If NOT using GitHub Projects, remove `project` from frontmatter and remove/avoid project-related safe outputs unless you still want status updates.

### 5) Improve the Markdown Body (No Recompile)

Encourage edits to:

- objectives
- success criteria
- operational rules
- report format

These do **not** require recompilation.

## After Any Frontmatter Change

Run compilation:

```bash
gh aw compile
```

Optionally validate more strictly:

```bash
gh aw compile --validate --strict
```

## Sanity Checklist

- `project.id` matches filename basename.
- Worker workflows exist as `.md` workflows in `.github/workflows/`.
- If using `dispatch-workflow`, `safe-outputs.dispatch-workflow.workflows` matches the intended worker workflows.
- Governance budgets are consistent with run cadence.
