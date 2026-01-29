---
name: Dependabot Burner Campaign
description: Burns down Dependabot security alert work items
on:
  #schedule:
  #  - cron: "0 * * * *"
  workflow_dispatch:
permissions:
  issues: read
  pull-requests: read
  contents: read
  security-events: read
safe-outputs:
  update-project:
    max: 100
  create-project-status-update:
    max: 1
  create-issue:
    max: 1
    title-prefix: "[dependabot-burner]"
    assignees: copilot
---

# Dependabot Burner Campaign

**Project URL (use for all project safe-output calls):**
- `https://github.com/orgs/githubnext/projects/144`

**Campaign ID (use for all project safe-output calls):**
- `dependabot-burndown`

This workflow discovers security alert work items in the githubnext/gh-aw repository and updates the project board with their status:

- Dependabot-created PRs for JavaScript dependency updates

## Task

You need to discover and update security work items on the project board. Follow these steps:

### Step 1: Discover Dependabot PRs

Use the GitHub MCP server to search for pull requests in the `githubnext/gh-aw` repository with:
- Author: `app/dependabot`
- Labels: `dependencies`, `javascript`
- State: open

Example search query:
```
repo:githubnext/gh-aw is:pr author:app/dependabot label:dependencies label:javascript is:open
```

### Step 2: Check for Work

If *no* Dependabot PRs are found:
- Call the `noop` tool with message: "No security alerts found to process"
- Exit successfully

### Step 3: Update Project Board

For each discovered item (up to 100 total per run):
- Add or update the corresponding work item on the project board:
- Use the `update-project` safe output tool
- Always include the project URL in the safe-output call:
  - `project`: "https://github.com/orgs/githubnext/projects/144"

Note: Any other project-related safe-output calls (like `create_project_status_update`) must also use the same project URL.
- Always include the content identity:
  - `content_type`: `pull_request` (Dependabot PRs)
  - `content_number`: PR/issue number
- Set fields:
  - `campaign_id`: "security-alert-burndown"
  - `status`: "Todo" (for open items)
  - `target_repo`: "githubnext/gh-aw"
  - `worker_workflow`: who discovered it, using one of:
    - "dependabot"
  - `priority`: Estimate priority:
    - "High" for critical/severe alerts
    - "Medium" for moderate alerts
    - "Low" for low/none alerts
  - `size`: Estimate size:
    - "Small" for single dependency updates
    - "Medium" for multiple dependency updates
    - "Large" for complex updates with breaking changes
  - `start_date`: Item created date (YYYY-MM-DD format)
  - `end_date`: Item closed date (YYYY-MM-DD format) or today's date if still open

### Step 4: Create parent issue and assign work

After updating project items, **first complete the bundling analysis below, then immediately perform the safe-output calls below in sequence**. Do not proceed to Step 5 until the calls are complete.

#### Bundling Analysis (Do This First)

Before creating the issue, analyze the discovered PRs and determine which PRs to bundle together.

#### Required Safe-Output Calls

After completing the bundling analysis, you must immediately perform these safe-output calls in order:

1. **Call `create_issue`** to create the parent tracking issue
2. **Call `update_project`** to add the created issue to the project board  

The created issue will be assigned to Copilot automatically via `safe-outputs.create-issue.assignees`.

#### Bundling Guidelines

Analyze all discovered PRs following these rules:

1. Review all discovered PRs
2. Group by **runtime** (Node.js, Python, etc.) and **target dependency file**
3. Select up to **3 bundles** total following the bundling rules below

**Dependabot Bundling Rules:**

- Group work by **runtime** (Node.js, Python, etc.). Never mix runtimes.
- Group changes by **target dependency file**. Each PR must modify **one manifest (and its lockfile) only**.
- Bundle updates **only within a single target file**.
- Patch and minor updates **may be bundled**; major updates **should be isolated** unless dependencies are tightly coupled.
- Bundled releases **must include a research report** describing:
  - Packages updated and old → new versions
  - Breaking or behavioral changes
  - Migration steps or code impact
  - Risk level and test coverage impact
- Prioritize **security alerts and high-risk updates** first within each runtime.
- Enforce **one runtime + one target file per PR**.
- All PRs must pass **CI and relevant runtime tests** before merge.

#### Safe-Output Call: Create Bundle Issues

Create **one issue per planned bundle** (up to 3 total). Each issue should correspond to exactly **one runtime + one manifest file**.

For each bundle, call `create_issue`:

```
create_issue(
  title="[dependabot-burndown] Security Alert Burndown: Dependabot bundle — <runtime> — <manifest> (YYYY-MM-DD)",
  body="<use template below>"
)
```

**IMPORTANT**: After each `create_issue`, save the returned temporary ID (e.g., `aw_sec2026012901`). You MUST use each temporary ID in the corresponding project update.

#### Safe-Output Call: Add Each Bundle Issue to Project Board

For **each** issue you created above, **immediately** call `update_project`:

```
update_project(
  project="https://github.com/orgs/githubnext/projects/144",
  content_type="issue",
  content_number="<temporary_id_from_create_issue>",
  fields={
    "campaign_id": "dependabot-burndown",
    "status": "Todo",
    "target_repo": "githubnext/gh-aw",
    "worker_workflow": "dependabot",
    "priority": "High",
    "size": "Medium",
    "start_date": "YYYY-MM-DD"
  }
)
```

**Example**: If a bundle `create_issue` returned `aw_sec2026012901`, then call:
- `update_project(..., content_number="aw_sec2026012901", ...)`


**Issue Body Template (one bundle per issue):**
```markdown
## Context
This issue tracks one Dependabot PR bundle discovered by the Security Alert Burndown campaign.

## Bundle
- Runtime: [runtime]
- Manifest: [manifest file]

## Bundling Rules
- Group work by runtime. Never mix runtimes.
- Group changes by target dependency file (one manifest + its lockfile).
- Patch/minor updates may be bundled; major updates should be isolated unless tightly coupled.
- Bundled releases must include a research report (packages, versions, breaking changes, migration, risk, tests).

## PRs in Bundle
- [ ] #123 - [title] ([old] → [new])
- [ ] #456 - [title] ([old] → [new])

## Agent Task
1. Research each update for breaking changes and summarize risks.
2. Create a single bundled PR (one runtime + one manifest) with title prefix "[dependabot-burndown]".
3. Ensure CI passes; run relevant runtime tests.
4. Add the research report to the bundled PR.
5. Update this issue checklist as PRs are merged.
```

### Step 5: Report

Summarize how many items were discovered and added/updated on the project board, broken down by category, and include the bundle issue numbers that were created and assigned.

## Important

- Always use the `update-project` tool for project board updates
- If no work is found, call `noop` to indicate successful completion with no actions
- Focus only on open items:
  - PRs: open only
- Limit updates to 100 items per run to respect rate limits (prioritize highest severity/most recent first)
