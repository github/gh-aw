---
title: Safe Outputs
description: Learn about safe output processing features that enable creating GitHub issues, comments, and pull requests without giving workflows write permissions.
sidebar:
  order: 800
---

The [`safe-outputs:`](/gh-aw/reference/glossary/#safe-outputs) (validated GitHub operations) element of your workflow's [frontmatter](/gh-aw/reference/glossary/#frontmatter) declares that your agentic workflow should conclude with optional automated actions based on the agentic workflow's output. This enables your workflow to write content that is then automatically processed to create GitHub issues, comments, pull requests, or add labels—all without giving the agentic portion of the workflow any write permissions.

## Why Safe Outputs?

Safe outputs enforce security through separation: agents run read-only and request actions via structured output, while separate permission-controlled jobs execute those requests. This provides least privilege, defense against prompt injection, auditability, and controlled limits per operation.

Example:
```yaml wrap
safe-outputs:
  create-issue:
```

The agent requests issue creation; a separate job with `issues: write` creates it.

## Available Safe Output Types

> [!NOTE]
> Most safe output types support cross-repository operations. Exceptions are noted below.

### Issues & Discussions

- [**Create Issue**](#issue-creation-create-issue) (`create-issue`) — Create GitHub issues (max: 1)
- [**Update Issue**](#issue-updates-update-issue) (`update-issue`) — Update issue status, title, or body (max: 1)
- [**Close Issue**](#close-issue-close-issue) (`close-issue`) — Close issues with comment (max: 1)
- [**Link Sub-Issue**](#link-sub-issue-link-sub-issue) (`link-sub-issue`) — Link issues as sub-issues (max: 1)
- [**Create Discussion**](#discussion-creation-create-discussion) (`create-discussion`) — Create GitHub discussions (max: 1)
- [**Update Discussion**](#discussion-updates-update-discussion) (`update-discussion`) — Update discussion title, body, or labels (max: 1)
- [**Close Discussion**](#close-discussion-close-discussion) (`close-discussion`) — Close discussions with comment and resolution (max: 1)

### Pull Requests

- [**Create PR**](#pull-request-creation-create-pull-request) (`create-pull-request`) — Create pull requests with code changes (max: 1)
- [**Update PR**](#pull-request-updates-update-pull-request) (`update-pull-request`) — Update PR title or body (max: 1)
- [**Close PR**](#close-pull-request-close-pull-request) (`close-pull-request`) — Close pull requests without merging (max: 10)
- [**PR Review Comments**](#pr-review-comments-create-pull-request-review-comment) (`create-pull-request-review-comment`) — Create review comments on code lines (max: 10)
- [**Push to PR Branch**](#push-to-pr-branch-push-to-pull-request-branch) (`push-to-pull-request-branch`) — Push changes to PR branch (max: 1, same-repo only)

### Labels, Assignments & Reviews

- [**Add Comment**](#comment-creation-add-comment) (`add-comment`) — Post comments on issues, PRs, or discussions (max: 1)
- [**Hide Comment**](#hide-comment-hide-comment) (`hide-comment`) — Hide comments on issues, PRs, or discussions (max: 5)
- [**Add Labels**](#add-labels-add-labels) (`add-labels`) — Add labels to issues or PRs (max: 3)
- [**Remove Labels**](#remove-labels-remove-labels) (`remove-labels`) — Remove labels from issues or PRs (max: 3)
- [**Add Reviewer**](#add-reviewer-add-reviewer) (`add-reviewer`) — Add reviewers to pull requests (max: 3)
- [**Assign Milestone**](#assign-milestone-assign-milestone) (`assign-milestone`) — Assign issues to milestones (max: 1)
- [**Assign to Agent**](#assign-to-agent-assign-to-agent) (`assign-to-agent`) — Assign Copilot agents to issues or PRs (max: 1)
- [**Assign to User**](#assign-to-user-assign-to-user) (`assign-to-user`) — Assign users to issues (max: 1)

### Projects, Releases & Assets

- [**Create Project**](#project-creation-create-project) (`create-project`) — Create new GitHub Projects boards (max: 1, cross-repo)
- [**Update Project**](#project-board-updates-update-project) (`update-project`) — Manage GitHub Projects boards (max: 10, same-repo only)
- [**Create Project Status Update**](#project-status-updates-create-project-status-update) (`create-project-status-update`) — Create project status updates
- [**Update Release**](#release-updates-update-release) (`update-release`) — Update GitHub release descriptions (max: 1)
- [**Upload Assets**](#asset-uploads-upload-asset) (`upload-asset`) — Upload files to orphaned git branch (max: 10, same-repo only)

### Security & Agent Tasks

- [**Dispatch Workflow**](#workflow-dispatch-dispatch-workflow) (`dispatch-workflow`) — Trigger other workflows with inputs (max: 3, same-repo only)
- [**Code Scanning Alerts**](#code-scanning-alerts-create-code-scanning-alert) (`create-code-scanning-alert`) — Generate SARIF security advisories (max: unlimited, same-repo only)
- [**Autofix Code Scanning Alerts**](#autofix-code-scanning-alerts-autofix-code-scanning-alert) (`autofix-code-scanning-alert`) — Create automated fixes for code scanning alerts (max: 10, same-repo only)
- [**Create Agent Session**](#agent-session-creation-create-agent-session) (`create-agent-session`) — Create Copilot agent sessions (max: 1)

### System Types (Auto-Enabled)

- [**No-Op**](#no-op-logging-noop) (`noop`) — Log completion message for transparency (max: 1, same-repo only)
- [**Missing Tool**](#missing-tool-reporting-missing-tool) (`missing-tool`) — Report missing tools (max: unlimited, same-repo only)
- [**Missing Data**](#missing-data-reporting-missing-data) (`missing-data`) — Report missing data required to achieve goals (max: unlimited, same-repo only)

> [!TIP]
> Custom safe output types: [Custom Safe Output Jobs](/gh-aw/reference/custom-safe-outputs/). See [Deterministic & Agentic Patterns](/gh-aw/guides/deterministic-agentic-patterns/) for combining computation and AI reasoning.

### Custom Safe Output Jobs (`jobs:`)

Create custom post-processing jobs registered as Model Context Protocol (MCP) tools. Support standard GitHub Actions properties and auto-access agent output via `$GH_AW_AGENT_OUTPUT`. See [Custom Safe Output Jobs](/gh-aw/reference/custom-safe-outputs/).

> [!NOTE]
> **Internal Implementation**: Custom safe output jobs are internally referred to as "safe-jobs" in the compiler code (`pkg/workflow/safe_jobs.go`), but they are user-facing only through the `safe-outputs.jobs:` configuration. The top-level `safe-jobs:` key is deprecated and not supported.

### Issue Creation (`create-issue:`)

Creates GitHub issues based on workflow output.

```yaml wrap
safe-outputs:
  create-issue:
    title-prefix: "[ai] "            # prefix for titles
    labels: [automation, agentic]    # labels to attach
    assignees: [user1, copilot]      # assignees (use 'copilot' for bot)
    max: 5                           # max issues (default: 1)
    expires: 7                       # auto-close after 7 days (or false to disable)
    group: true                      # group as sub-issues under parent
    close-older-issues: true         # close previous issues from same workflow
    target-repo: "owner/repo"        # cross-repository
```

#### Auto-Expiration

The `expires` field auto-closes issues after a time period. Supports integers (days), relative formats (`2h`, `7d`, `2w`, `1m`, `1y`), or `false` to disable expiration. Generates `agentics-maintenance.yml` workflow that runs at the minimum required frequency based on the shortest expiration time across all workflows:

- 1 day or less → every 2 hours
- 2 days → every 6 hours
- 3-4 days → every 12 hours
- 5+ days → daily

Hours less than 24 are treated as 1 day minimum for expiration calculation.

To explicitly disable expiration (useful when create-issue has a default expiration), use `expires: false`:

#### Issue Grouping

The `group` field (default: `false`) automatically organizes multiple issues as sub-issues under a parent issue. When enabled:

- Parent issues are automatically created and managed using the workflow ID as the group identifier
- Child issues are linked to the parent using GitHub's sub-issue relationships
- Maximum of 64 sub-issues per parent issue
- Parent issues include metadata tracking all sub-issues

This is useful for workflows that create multiple related issues, such as planning workflows that break down epics into tasks, or batch processing workflows that create issues for individual items.

**Example:**
```yaml wrap
safe-outputs:
  create-issue:
    title-prefix: "[plan] "
    labels: [plan, ai-generated]
    max: 5
    group: true
```

In this example, if the workflow creates 5 issues, all will be automatically grouped under a parent issue, making it easy to track related work items together.

#### Temporary IDs for Issue References

Use temporary IDs (`aw_` + 12 hex chars) to reference parent issues before creation. References like `#aw_abc123def456` in bodies are replaced with actual numbers. The `parent` field creates sub-issue relationships.

#### Auto-Close Older Issues

The `close-older-issues` field (default: `false`) automatically closes previous open issues from the same workflow when a new issue is created. This is useful for workflows that generate recurring reports or status updates, ensuring only the latest issue remains open.

```yaml wrap
safe-outputs:
  create-issue:
    title-prefix: "[weekly-report] "
    labels: [report, automation]
    close-older-issues: true
```

When enabled:
- Searches for open issues containing the same workflow-id marker in their body
- Closes found issues as "not planned" with a comment linking to the new issue
- Maximum 10 older issues will be closed
- Only runs if the new issue creation succeeds

#### Searching for Workflow-Created Items

All items created by workflows (issues, pull requests, discussions, and comments) include a hidden **workflow-id marker** in their body:

```html
<!-- gh-aw-workflow-id: WORKFLOW_NAME -->
```

You can use this marker to find all items created by a specific workflow on GitHub.com.

**Search Examples:**

Find all open issues created by the `daily-team-status` workflow:
```
repo:owner/repo is:issue is:open "gh-aw-workflow-id: daily-team-status" in:body
```

Find all pull requests created by the `security-audit` workflow:
```
repo:owner/repo is:pr "gh-aw-workflow-id: security-audit" in:body
```

Find all items (issues, PRs, discussions) from any workflow in your organization:
```
org:your-org "gh-aw-workflow-id:" in:body
```

Find comments from a specific workflow:
```
repo:owner/repo "gh-aw-workflow-id: bot-responder" in:comments
```

> [!TIP]
> **Search Tips for Workflow Markers**
>
> - Use quotes around the marker text to search for the exact phrase
> - Add `in:body` to search issue/PR descriptions, or `in:comments` for comments
> - Combine with other filters like `is:open`, `is:closed`, `created:>2024-01-01`
> - The workflow name in the marker is the workflow filename without the `.md` extension
> - Use GitHub's advanced search to refine results: [Advanced search documentation](https://docs.github.com/en/search-github/searching-on-github/searching-issues-and-pull-requests)

### Close Issue (`close-issue:`)

Closes GitHub issues with an optional comment and state reason. Filters by labels and title prefix control which issues can be closed.

```yaml wrap
safe-outputs:
  close-issue:
    target: "triggering"              # "triggering" (default), "*", or number
    required-labels: [automated]      # only close with any of these labels
    required-title-prefix: "[bot]"    # only close matching prefix
    max: 20                           # max closures (default: 1)
    target-repo: "owner/repo"         # cross-repository
```

**Target**: `"triggering"` (requires issue event), `"*"` (any issue), or number (specific issue).

**State Reasons**: `completed`, `not_planned`, `reopened` (default: `completed`).

### Comment Creation (`add-comment:`)

Posts comments on issues, PRs, or discussions. Defaults to triggering item; use `target: "*"` for any, or number for specific items. When combined with `create-issue`, `create-discussion`, or `create-pull-request`, includes "Related Items" section.

```yaml wrap
safe-outputs:
  add-comment:
    max: 3                       # max comments (default: 1)
    target: "*"                  # "triggering" (default), "*", or number
    discussion: true             # target discussions
    target-repo: "owner/repo"    # cross-repository
    hide-older-comments: true    # hide previous comments from same workflow
    allowed-reasons: [outdated]  # restrict hiding reasons (optional)
```

#### Hide Older Comments

Set `hide-older-comments: true` to minimize previous comments from the same workflow (identified by `GITHUB_WORKFLOW`) before posting new ones. Useful for status updates. Allowed reasons: `spam`, `abuse`, `off_topic`, `outdated` (default), `resolved`.

#### Append-Only Status Comments

By default, gh-aw posts an activation comment when a workflow starts, then updates that same comment with the final status.

If you prefer an append-only timeline (never editing existing comments), set:

```yaml wrap
safe-outputs:
  messages:
    append-only-comments: true
```

When enabled, the workflow completion notifier creates a new comment instead of editing the activation comment.

### Hide Comment (`hide-comment:`)

Collapses comments in GitHub UI with reason. Requires GraphQL node IDs (e.g., `IC_kwDOABCD123456`), not REST numeric IDs. Reasons: `spam`, `abuse`, `off_topic`, `outdated`, `resolved`.

```yaml wrap
safe-outputs:
  hide-comment:
    max: 5                    # max comments (default: 5)
    target-repo: "owner/repo" # cross-repository
```

### Add Labels (`add-labels:`)

Adds labels to issues or PRs. Specify `allowed` to restrict to specific labels.

```yaml wrap
safe-outputs:
  add-labels:
    allowed: [bug, enhancement]  # restrict to specific labels
    max: 3                       # max labels (default: 3)
    target: "*"                  # "triggering" (default), "*", or number
    target-repo: "owner/repo"    # cross-repository
```

### Remove Labels (`remove-labels:`)

Removes labels from issues or PRs. Specify `allowed` to restrict which labels can be removed. If a label is not present on the item, it will be silently skipped.

```yaml wrap
safe-outputs:
  remove-labels:
    allowed: [automated, stale]  # restrict to specific labels (optional)
    max: 3                       # max operations (default: 3)
    target: "*"                  # "triggering" (default), "*", or number
    target-repo: "owner/repo"    # cross-repository
```

**Target**: `"triggering"` (requires issue/PR event), `"*"` (any issue/PR), or number (specific issue/PR).

When `allowed` is omitted or set to `null`, any labels can be removed. Use `allowed` to restrict removal to specific labels only, providing control over which labels agents can manipulate.

**Example use case**: Label lifecycle management where agents add temporary labels during triage and remove them once processed.

```yaml wrap
safe-outputs:
  add-labels:
    allowed: [needs-triage, automation]
  remove-labels:
    allowed: [needs-triage]  # agents can remove triage label after processing
```

### Add Reviewer (`add-reviewer:`)

Adds reviewers to pull requests. Specify `reviewers` to restrict to specific GitHub usernames.

```yaml wrap
safe-outputs:
  add-reviewer:
    reviewers: [user1, copilot]  # restrict to specific reviewers
    max: 3                       # max reviewers (default: 3)
    target: "*"                  # "triggering" (default), "*", or number
    target-repo: "owner/repo"    # cross-repository
```

**Target**: `"triggering"` (requires PR event), `"*"` (any PR), or number (specific PR).

Use `reviewers: copilot` to assign the Copilot PR reviewer bot. Requires a PAT stored as `COPILOT_GITHUB_TOKEN` or `GH_AW_GITHUB_TOKEN` (legacy).

### Assign Milestone (`assign-milestone:`)

Assigns issues to milestones. Specify `allowed` to restrict to specific milestone titles.

```yaml wrap
safe-outputs:
  assign-milestone:
    allowed: [v1.0, v2.0]    # restrict to specific milestone titles
    max: 1                   # max assignments (default: 1)
    target-repo: "owner/repo" # cross-repository
```

### Issue Updates (`update-issue:`)

Updates issue status, title, or body. Only explicitly enabled fields can be updated. Status must be "open" or "closed". The `operation` field controls how body updates are applied: `append` (default), `prepend`, `replace`, or `replace-island`.

```yaml wrap
safe-outputs:
  update-issue:
    status:                   # enable status updates
    title:                    # enable title updates
    body:                     # enable body updates
    max: 3                    # max updates (default: 1)
    target: "*"               # "triggering" (default), "*", or number
    target-repo: "owner/repo" # cross-repository
```

**Target**: `"triggering"` (requires issue event), `"*"` (any issue), or number (specific issue).

When using `target: "*"`, the agent must provide `issue_number` or `item_number` in the output to identify which issue to update.

**Operation Types** (for body updates):
- `append` (default): Adds content to the end with separator and attribution
- `prepend`: Adds content to the start with separator and attribution
- `replace`: Completely replaces existing body with new content and attribution
- `replace-island`: Updates a specific section marked with HTML comments

Agent output format: `{"type": "update_issue", "issue_number": 123, "operation": "append", "body": "..."}`. The `operation` field is optional (defaults to `append`).

### Pull Request Updates (`update-pull-request:`)

Updates PR title or body. Both fields are enabled by default. The `operation` field controls how body updates are applied: `append` (default), `prepend`, or `replace`.

```yaml wrap
safe-outputs:
  update-pull-request:
    title: true               # enable title updates (default: true)
    body: true                # enable body updates (default: true)
    max: 1                    # max updates (default: 1)
    target: "*"               # "triggering" (default), "*", or number
    target-repo: "owner/repo" # cross-repository
```

**Target**: `"triggering"` (requires PR event), `"*"` (any PR), or number (specific PR).

When using `target: "*"`, the agent must provide `pull_request_number` in the output to identify which pull request to update.

**Operation Types**:
- `append` (default): Adds content to the end with separator and attribution
- `prepend`: Adds content to the start with separator and attribution
- `replace`: Completely replaces existing body with new content and attribution

Title updates always replace the existing title. Disable fields by setting to `false`.

### Link Sub-Issue (`link-sub-issue:`)

Links issues as sub-issues using GitHub's parent-child issue relationships. Supports filtering by labels and title prefixes for both parent and sub issues.

```yaml wrap
safe-outputs:
  link-sub-issue:
    parent-required-labels: [epic]        # parent must have these labels
    parent-title-prefix: "[Epic]"         # parent must match prefix
    sub-required-labels: [task]           # sub must have these labels
    sub-title-prefix: "[Task]"            # sub must match prefix
    max: 1                                # max links (default: 1)
    target-repo: "owner/repo"             # cross-repository
```

Agent output includes `parent_issue_number` and `sub_issue_number`. Validation ensures both issues exist and meet label/prefix requirements before linking.

### Project Creation (`create-project:`)

Creates new GitHub Projects V2 boards. Requires PAT or GitHub App token ([`GH_AW_PROJECT_GITHUB_TOKEN`](/gh-aw/reference/tokens/#gh_aw_project_github_token-github-projects-v2))—default `GITHUB_TOKEN` lacks Projects v2 access. Supports optional view configuration to create custom project views at creation time.

```yaml wrap
safe-outputs:
  create-project:
    max: 1                              # max operations (default: 1)
    github-token: ${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}
    target-owner: "myorg"               # default target owner (optional)
    title-prefix: "Project"             # default title prefix (optional)
    views:                              # optional: auto-create views
      - name: "Sprint Board"
        layout: board
        filter: "is:issue is:open"
      - name: "Task Tracker"
        layout: table
```

When `views` are configured, they are created automatically after project creation. GitHub's default "View 1" will remain, and configured views are created as additional views.

The `target-owner` field is an optional default. When configured, the agent can omit the owner field in tool calls, and the default will be used. The agent can still override by providing an explicit owner value.

**Without default** (agent must provide owner):
```javascript
create_project({
  title: "Project: Security Q1 2025",
  owner: "myorg",
  owner_type: "org",  // "org" or "user" (default: "org")
  item_url: "https://github.com/myorg/repo/issues/123"  // Optional issue to add
});
```

**With default configured** (agent only needs title):
```javascript
create_project({
  title: "Project: Security Q1 2025"
  // owner uses configured default
  // owner_type defaults to "org"
  // Can still override: owner: "...", owner_type: "user"
});
```

Optionally include `item_url` (GitHub issue URL) to add the issue as the first project item. Exposes outputs: `project-id`, `project-number`, `project-title`, `project-url`, `item-id` (if item added).

> [!IMPORTANT]
> **Token Requirements**: The default `GITHUB_TOKEN` **cannot** create projects. You **must** configure a PAT with Projects permissions:
> - **Classic PAT**: `project` scope (user projects) or `project` + `repo` scope (org projects)
> - **Fine-grained PAT**: Organization permissions → Projects: Read & Write

> [!NOTE]
> You can configure views directly during project creation using the `views` field (see above), or later using `update-project` to add custom fields and additional views. For pattern guidance, see [Projects & Monitoring](/gh-aw/guides/monitoring/).

### Project Board Updates (`update-project:`)

Manages GitHub Projects boards. Requires PAT or GitHub App token ([`GH_AW_PROJECT_GITHUB_TOKEN`](/gh-aw/reference/tokens/#gh_aw_project_github_token-github-projects-v2))—default `GITHUB_TOKEN` lacks Projects v2 access. Update-only by default; set `create_if_missing: true` to create boards (requires appropriate token permissions).

```yaml wrap
safe-outputs:
  update-project:
    project: "https://github.com/orgs/myorg/projects/42"  # required: target project URL
    max: 20                         # max operations (default: 10)
    github-token: ${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}
    views:                          # optional: auto-create views
      - name: "Sprint Board"
        layout: board
        filter: "is:issue is:open"
      - name: "Task Tracker"
        layout: table
      - name: "Roadmap"
        layout: roadmap
```

**Configuration options:**
- `project` (required in configuration): Default project URL shown in examples. Note: Agent output messages **must** explicitly include the `project` field - the configured value is for documentation purposes only.
- `max`: Maximum number of operations per run (default: 10).
- `github-token`: Custom token with Projects permissions (required for Projects v2 access).
- `views`: Optional array of project views to create automatically.
- Exposes outputs: `project-id`, `project-number`, `project-url`, `item-id`.

#### Supported Field Types

GitHub Projects V2 supports various custom field types. The following field types are automatically detected and handled:

- **`TEXT`** — Text fields (default)
- **`DATE`** — Date fields (format: `YYYY-MM-DD`)
- **`NUMBER`** — Numeric fields (story points, estimates, etc.)
- **`ITERATION`** — Sprint/iteration fields (matched by iteration title)
- **`SINGLE_SELECT`** — Dropdown/select fields (creates missing options automatically)

**Example field usage:**
```yaml
fields:
  status: "In Progress"          # SINGLE_SELECT field
  start_date: "2026-01-04"       # DATE field
  story_points: 8                # NUMBER field
  sprint: "Sprint 42"            # ITERATION field (by title)
  priority: "High"               # SINGLE_SELECT field
```

> [!NOTE]
> Field names are case-insensitive and automatically normalized (e.g., `story_points` matches `Story Points`).

#### Creating Project Views

Project views can be created automatically by declaring them in the `views` array. Views are created when the workflow runs, after processing update_project items from the agent.

**View configuration:**
```yaml
safe-outputs:
  update-project:
    github-token: ${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}
    views:
      - name: "Sprint Board"        # required: view name
        layout: board               # required: table, board, or roadmap
        filter: "is:issue is:open"  # optional: filter query
      - name: "Task Tracker"
        layout: table
        filter: "is:issue is:pr"
      - name: "Roadmap"
        layout: roadmap
```

**View properties:**

| Property | Type | Required | Description |
|----------|------|----------|-------------|
| `name` | string | Yes | View name (e.g., "Sprint Board", "Task Tracker") |
| `layout` | string | Yes | View layout: `table`, `board`, or `roadmap` |
| `filter` | string | No | Filter query (e.g., `is:issue is:open`, `label:bug`) |
| `visible-fields` | array | No | Field IDs to display (table/board only, not roadmap) |

**Layout types:**
- **`table`** — List view with customizable columns for detailed tracking
- **`board`** — Kanban-style cards grouped by status or custom field
- **`roadmap`** — Timeline visualization with date-based swimlanes

**Filter syntax examples:**
- `is:issue is:open` — Open issues only
- `is:pr` — Pull requests only  
- `is:issue is:pr` — Both issues and PRs
- `label:bug` — Items with bug label
- `assignee:@me` — Items assigned to viewer

Views are created automatically during workflow execution. The workflow must include at least one `update_project` operation to provide the target project URL.



### Project Status Updates (`create-project-status-update:`)

Creates status updates on GitHub Projects boards to communicate progress, findings, and trends. Status updates appear in the project's Updates tab and provide a historical record of execution. Requires PAT or GitHub App token ([`GH_AW_PROJECT_GITHUB_TOKEN`](/gh-aw/reference/tokens/#gh_aw_project_github_token-github-projects-v2))—default `GITHUB_TOKEN` lacks Projects v2 access.

```yaml wrap
safe-outputs:
  create-project-status-update:
    project: "https://github.com/orgs/myorg/projects/73"  # required: target project URL
    max: 1                          # max updates per run (default: 1)
    github-token: ${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}
```

**Configuration options:**
- `project` (required in configuration): Default project URL shown in examples. Note: Agent output messages **must** explicitly include the `project` field - the configured value is for documentation purposes only.
- `max`: Maximum number of status updates per run (default: 1).
- `github-token`: Custom token with Projects permissions (required for Projects v2 access).
- Often used by scheduled workflows and orchestrator workflows to post run summaries.

#### Required Fields

| Field | Type | Description |
|-------|------|-------------|
| `project` | URL | Full GitHub project URL (e.g., `https://github.com/orgs/myorg/projects/73`). **Required** in every agent output message. |
| `body` | Markdown | Status update content with summary, findings, and next steps |

#### Optional Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `status` | Enum | `ON_TRACK` | Status indicator: `ON_TRACK`, `AT_RISK`, `OFF_TRACK`, `COMPLETE`, `INACTIVE` |
| `start_date` | Date | Today | Run start date (format: `YYYY-MM-DD`) |
| `target_date` | Date | Today | Projected completion or milestone date (format: `YYYY-MM-DD`) |

#### Example Usage

```yaml
create-project-status-update:
  project: "https://github.com/orgs/myorg/projects/73"
  status: "ON_TRACK"
  start_date: "2026-01-06"
  target_date: "2026-01-31"
  body: |
  ## Run Summary

    **Discovered:** 25 items (15 issues, 10 PRs)
    **Processed:** 10 items added to project, 5 updated
    **Completion:** 60% (30/50 total tasks)

    ### Key Findings
    - Documentation coverage improved to 88%
    - 3 critical accessibility issues identified
    - Worker velocity: 1.2 items/day

    ### Trends
    - Velocity stable at 8-10 items/week
    - Blocked items decreased from 5 to 2
    - On track for end-of-month completion

    ### Next Steps
    - Continue processing remaining 15 items
    - Address 2 blocked items in next run
    - Target 95% documentation coverage by end of month
```

#### Status Indicators

- **`ON_TRACK`**: Progressing as planned, meeting expected targets
- **`AT_RISK`**: Potential issues identified (blocked items, slower velocity, dependencies)
- **`OFF_TRACK`**: Behind schedule, requires intervention or re-planning
- **`COMPLETE`**: Objectives met, no further work needed
- **`INACTIVE`**: Paused or not actively running

Exposes outputs: `status-update-id`, `project-id`, `status`.


### Pull Request Creation (`create-pull-request:`)

Creates PRs with code changes. Falls back to issue if creation fails (e.g., org settings block it). `expires` field (same-repo only) auto-closes after period: integers (days) or `2h`, `7d`, `2w`, `1m`, `1y` (hours < 24 treated as 1 day).

```yaml wrap
safe-outputs:
  create-pull-request:
    title-prefix: "[ai] "         # prefix for titles
    labels: [automation]          # labels to attach
    reviewers: [user1, copilot]   # reviewers (use 'copilot' for bot)
    draft: true                   # create as draft (default: true)
    expires: 14                   # auto-close after 14 days (same-repo only)
    if-no-changes: "warn"         # "warn" (default), "error", or "ignore"
    target-repo: "owner/repo"     # cross-repository
```

> [!NOTE]
> PR creation may fail if "Allow GitHub Actions to create and approve pull requests" is disabled in Organization Settings. Fallback creates issue with branch link.

### Close Pull Request (`close-pull-request:`)

Closes PRs without merging with optional comment. Filter by labels and title prefix. Target: `"triggering"` (PR event), `"*"` (any), or number.

```yaml wrap
safe-outputs:
  close-pull-request:
    target: "triggering"              # "triggering" (default), "*", or number
    required-labels: [automated, stale] # only close with these labels
    required-title-prefix: "[bot]"    # only close matching prefix
    max: 10                           # max closures (default: 1)
    target-repo: "owner/repo"         # cross-repository
```

### PR Review Comments (`create-pull-request-review-comment:`)

Creates review comments on specific code lines in PRs. Supports single-line and multi-line comments.

```yaml wrap
safe-outputs:
  create-pull-request-review-comment:
    max: 3                    # max comments (default: 10)
    side: "RIGHT"             # "LEFT" or "RIGHT" (default: "RIGHT")
    target: "*"               # "triggering" (default), "*", or number
    target-repo: "owner/repo" # cross-repository
```

### Code Scanning Alerts (`create-code-scanning-alert:`)

Creates security advisories in SARIF format and submits to GitHub Code Scanning. Supports severity: error, warning, info, note.

```yaml wrap
safe-outputs:
  create-code-scanning-alert:
    max: 50  # max findings (default: unlimited)
```

### Autofix Code Scanning Alerts (`autofix-code-scanning-alert:`)

Creates automated fixes for code scanning alerts. Agent outputs fix suggestions that are submitted to GitHub Code Scanning.

```yaml wrap
safe-outputs:
  autofix-code-scanning-alert:
    max: 10  # max autofixes (default: 10)
```

### Push to PR Branch (`push-to-pull-request-branch:`)

Pushes changes to a PR's branch. Validates via `title-prefix` and `labels` to ensure only approved PRs receive changes.

```yaml wrap
safe-outputs:
  push-to-pull-request-branch:
    target: "*"                 # "triggering" (default), "*", or number
    title-prefix: "[bot] "      # require title prefix
    labels: [automated]         # require all labels
    if-no-changes: "warn"       # "warn" (default), "error", or "ignore"
```

When `create-pull-request` or `push-to-pull-request-branch` are enabled, file editing tools (Edit, Write, NotebookEdit) and git commands are added.

### Release Updates (`update-release:`)

Updates GitHub release descriptions: replace (complete replacement), append (add to end), or prepend (add to start).

```yaml wrap
safe-outputs:
  update-release:
    max: 1                       # max releases (default: 1, max: 10)
    target-repo: "owner/repo"    # cross-repository
    github-token: ${{ secrets.CUSTOM_TOKEN }}  # custom token
```

Agent output format: `{"type": "update_release", "tag": "v1.0.0", "operation": "replace", "body": "..."}`. The `tag` field is optional for release events (inferred from context). Workflow needs read access; only the generated job receives write permissions.

### Asset Uploads (`upload-asset:`)

Uploads files (screenshots, charts, reports) to orphaned git branch with predictable URLs: `https://raw.githubusercontent.com/{owner}/{repo}/{branch}/{filename}`. Agent registers files via `upload_asset` tool; separate job with `contents: write` commits them.

```yaml wrap
safe-outputs:
  upload-asset:
    branch: "assets/my-workflow"     # default: "assets/${{ github.workflow }}"
    max-size: 5120                   # KB (default: 10240 = 10MB)
    allowed-exts: [.png, .jpg, .svg] # default: [.png, .jpg, .jpeg]
    max: 20                          # default: 10
```

**Branch Requirements**: New branches require `assets/` prefix for security. Existing branches allow any name. Create custom branches manually:
```bash
git checkout --orphan my-custom-branch && git rm -rf . && git commit --allow-empty -m "Initialize" && git push origin my-custom-branch
```

**Security**: File path validation (workspace/`/tmp` only), extension allowlist, size limits, SHA-256 verification, orphaned branch isolation, minimal permissions.

**Outputs**: `published_count`, `branch_name`. **Limits**: Same-repo only, max 50MB/file, 100 assets/run.

### No-Op Logging (`noop:`)

Enabled by default. Allows agents to produce completion messages when no actions are needed, preventing silent workflow completion.

```yaml wrap
safe-outputs:
  create-issue:     # noop enabled automatically
  noop: false       # explicitly disable
```

Agent output: `{"type": "noop", "message": "Analysis complete - no issues found"}`. Messages appear in the workflow conclusion comment or step summary.

### Missing Tool Reporting (`missing-tool:`)

Enabled by default. Automatically detects and reports tools lacking permissions or unavailable functionality.

```yaml wrap
safe-outputs:
  create-issue:           # missing-tool enabled automatically
  missing-tool: false     # explicitly disable
```

### Missing Data Reporting (`missing-data:`)

Enabled by default. Allows AI agents to report missing data required to achieve their goals, encouraging truthfulness over hallucination.

```yaml wrap
safe-outputs:
  missing-data:
    create-issue: true          # create GitHub issues for missing data
    title-prefix: "[data]"      # prefix for issue titles (default: "[missing data]")
    labels: [data, blocked]     # labels to attach to issues
    max: 10                     # max reports per run (default: unlimited)
```

**Why Missing Data Matters**

AI agents work best when they acknowledge data gaps instead of inventing information. By explicitly reporting missing data, agents:
- **Ensure accuracy**: Prevent hallucinations and incorrect outputs
- **Enable improvement**: Help teams identify gaps in documentation, APIs, or configuration
- **Demonstrate responsibility**: Show honest behavior that should be encouraged

**Agent Output Format**

```json
{
  "type": "missing_data",
  "data_type": "user_preferences",
  "reason": "User preferences database not accessible",
  "context": "Needed to customize dashboard layout",
  "alternatives": "Could use default settings"
}
```

**Required Fields**: `data_type`, `reason`  
**Optional Fields**: `context`, `alternatives`

**Issue Creation**

When `create-issue: true`, the agent creates or updates GitHub issues documenting missing data with:
- Detailed explanation of what data is needed and why
- Context about how the data would be used
- Possible alternatives if the data cannot be provided
- Encouragement message praising the agent's truthfulness

This rewards honest AI behavior and helps teams improve data accessibility for future agent runs.

### Discussion Creation (`create-discussion:`)

Creates discussions with optional `category` (slug, name, or ID; defaults to first available). `expires` field auto-closes after period (integers, `2h`, `7d`, `2w`, `1m`, `1y`, or `false` to disable; hours < 24 treated as 1 day) as "OUTDATED" with comment. Generates maintenance workflow with dynamic frequency based on shortest expiration time (see Auto-Expiration section above).

**Category Naming Standard**: Use lowercase, plural category names (e.g., `audits`, `general`, `reports`) for consistency and better searchability. GitHub Discussion category IDs (starting with `DIC_`) are also supported.

```yaml wrap
safe-outputs:
  create-discussion:
    title-prefix: "[ai] "     # prefix for titles
    category: "general"       # category slug, name, or ID (use lowercase)
    expires: 3                # auto-close after 3 days (or false to disable)
    max: 3                    # max discussions (default: 1)
    target-repo: "owner/repo" # cross-repository
```

### Close Discussion (`close-discussion:`)

Closes GitHub discussions with optional comment and resolution reason. Filters by category, labels, and title prefix control which discussions can be closed.

```yaml wrap
safe-outputs:
  close-discussion:
    target: "triggering"         # "triggering" (default), "*", or number
    required-category: "Ideas"   # only close in category
    required-labels: [resolved]  # only close with labels
    required-title-prefix: "[ai]" # only close matching prefix
    max: 1                       # max closures (default: 1)
    target-repo: "owner/repo"    # cross-repository
```

**Target**: `"triggering"` (requires discussion event), `"*"` (any discussion), or number (specific discussion).

**Resolution Reasons**: `RESOLVED`, `DUPLICATE`, `OUTDATED`, `ANSWERED`.

### Discussion Updates (`update-discussion:`)

Updates discussion title, body, or labels. Only explicitly enabled fields can be updated.

```yaml wrap
safe-outputs:
  update-discussion:
    title:                    # enable title updates
    body:                     # enable body updates
    labels:                   # enable label updates
    allowed-labels: [bug, idea] # restrict to specific labels
    max: 1                    # max updates (default: 1)
    target: "*"               # "triggering" (default), "*", or number
    target-repo: "owner/repo" # cross-repository
```

**Field Enablement**: Include `title:`, `body:`, or `labels:` keys to enable updates for those fields. Without these keys, the field cannot be updated. Setting `allowed-labels` implicitly enables label updates.

**Target**: `"triggering"` (requires discussion event), `"*"` (any discussion), or number (specific discussion).

When using `target: "*"`, the agent must provide `discussion_number` in the output to identify which discussion to update.

### Workflow Dispatch (`dispatch-workflow:`)

Triggers other workflows in the same repository using GitHub's `workflow_dispatch` event. This enables orchestration patterns, such as orchestrator workflows that coordinate multiple worker workflows.

**Shorthand Syntax:**
```yaml wrap
safe-outputs:
  dispatch-workflow: [worker-workflow, scanner-workflow]
```

**Detailed Syntax:**
```yaml wrap
safe-outputs:
  dispatch-workflow:
    workflows: [worker-workflow, scanner-workflow]
    max: 3  # maximum dispatches (default: 1, max: 50)
```

#### Configuration

- **`workflows`** (required) — List of workflow names (without `.md` extension) that the agent is allowed to dispatch. Each workflow must exist in the same repository and support the `workflow_dispatch` trigger.
- **`max`** (optional) — Maximum number of workflow dispatches allowed (default: 1, maximum: 50). This prevents excessive workflow triggering.

#### Validation Rules

At compile time, the compiler validates:

1. **Workflow existence** — Each workflow in the `workflows` list must exist as either:
   - A markdown workflow file (`.md`)
   - A compiled lock file (`.lock.yml`)  
   - A standard GitHub Actions workflow (`.yml`)

2. **workflow_dispatch trigger** — Each workflow must include `workflow_dispatch` in its `on:` trigger section:
   ```yaml
   on: [push, workflow_dispatch]  # or
   on:
     push:
     workflow_dispatch:
       inputs:
         tracker_id:
           description: "Tracker identifier"
           required: true
   ```

3. **No self-reference** — A workflow cannot dispatch itself to prevent infinite loops.

4. **File resolution** — The compiler resolves the correct file extension (`.lock.yml` or `.yml`) at compile time and embeds it in the safe output configuration, ensuring the runtime handler dispatches the correct workflow file.

#### How It Works: MCP Tool Generation

When you configure `dispatch-workflow`, the compiler automatically generates MCP (Model Context Protocol) tools that the AI agent can call. Each workflow in your `workflows` list becomes a callable tool:

**Example Configuration:**
```yaml wrap
safe-outputs:
  dispatch-workflow:
    workflows: [deploy-app, run-tests]
```

**Generated MCP Tools:**
- `deploy_app` tool — Dispatches the deploy-app workflow
- `run_tests` tool — Dispatches the run-tests workflow

The compiler:
1. **Reads workflow_dispatch inputs** from the target workflow's YAML
2. **Generates MCP tool schemas** with matching input parameters
3. **Validates workflow files** exist and support workflow_dispatch
4. **Embeds tool definitions** in the compiled workflow

This means the AI agent automatically knows what inputs each workflow expects and can call them directly as tools.

#### Defining Workflow Inputs

To enable the agent to provide inputs when dispatching workflows, define `workflow_dispatch` inputs in the target workflow:

**Target Workflow Example (`deploy-app.md`):**
```yaml wrap
---
on:
  workflow_dispatch:
    inputs:
      environment:
        description: "Target deployment environment"
        required: true
        type: choice
        options: [staging, production]
      version:
        description: "Version to deploy"
        required: true
        type: string
      dry_run:
        description: "Perform dry run without actual deployment"
        required: false
        type: boolean
        default: false
---

# Deploy Application Workflow

Deploys the application to the specified environment...
```

**Agent Output Format:**

When the agent calls the generated MCP tool, it produces:

```json
{
  "type": "dispatch_workflow",
  "workflow_name": "deploy-app",
  "inputs": {
    "environment": "staging",
    "version": "v1.2.3",
    "dry_run": false
  }
}
```

All input values are automatically converted to strings as required by GitHub's workflow_dispatch API:
- Objects are JSON-stringified
- Numbers and booleans are converted to strings
- `null` and `undefined` become empty strings

#### Rate Limiting

To respect GitHub API rate limits, the handler automatically enforces a 5-second delay between consecutive workflow dispatches. The first dispatch has no delay.

#### Best Practices

**1. Always Define Explicit Inputs**

When creating workflows that will be dispatched, explicitly define all required inputs in the `workflow_dispatch` section:

```yaml wrap
---
on:
  workflow_dispatch:
    inputs:
      task_id:
        description: "Unique task identifier"
        required: true
        type: string
      priority:
        description: "Task priority level"
        required: false
        type: choice
        options: [low, medium, high]
        default: medium
---
```

This ensures:
- The MCP tool schema includes all expected parameters
- The agent knows what information to provide
- GitHub validates inputs at dispatch time

**2. Use Descriptive Input Descriptions**

Clear descriptions help the AI agent understand what information to provide:

```yaml wrap
# ✅ GOOD - Clear description
repository_url:
  description: "Full GitHub repository URL (e.g., https://github.com/owner/repo)"
  required: true
  type: string

# ❌ BAD - Vague description
repo:
  description: "Repository"
  type: string
```

**3. Use Choice Types for Limited Options**

When inputs have a fixed set of valid values, use `type: choice`:

```yaml wrap
action:
  description: "Action to perform"
  required: true
  type: choice
  options: [analyze, fix, report]
```

This prevents the agent from providing invalid values and makes the interface clearer.

**4. Provide Sensible Defaults**

For optional inputs, provide defaults that work for the most common use case:

```yaml wrap
timeout:
  description: "Maximum execution time in minutes"
  required: false
  type: number
  default: 30
```

#### Troubleshooting

**Problem: "Workflow file not found"**

Error: `dispatch-workflow: workflow 'my-workflow' not found in .github/workflows/`

**Solutions:**
1. Ensure the workflow file exists in `.github/workflows/`
2. Use the workflow name without extension (e.g., `my-workflow`, not `my-workflow.md`)
3. Compile markdown workflows before dispatching: `gh aw compile my-workflow`

**Problem: "Workflow does not support workflow_dispatch trigger"**

Error: `dispatch-workflow: workflow 'my-workflow' does not support workflow_dispatch trigger`

**Solution:** Add `workflow_dispatch` to the `on:` section of the target workflow:

```yaml wrap
on:
  push:
  workflow_dispatch:
    inputs:
      # Define your inputs here
```

**Problem: "Required input not provided"**

The workflow is dispatched but GitHub rejects it due to missing required inputs.

**Solution:** Ensure the target workflow defines its inputs and they match what the agent is providing:

1. Check the target workflow's `workflow_dispatch.inputs` section
2. Mark required inputs with `required: true`
3. The agent will automatically know to provide these inputs based on the MCP tool schema

**Problem: "Agent doesn't know what inputs to provide"**

The agent dispatches the workflow but doesn't include necessary inputs.

**Solutions:**
1. **Define inputs explicitly** in the target workflow's `workflow_dispatch` section
2. **Add clear descriptions** to help the agent understand what each input is for
3. **Mark required inputs** with `required: true`
4. **Update your dispatcher workflow's prompt** to mention specific inputs if needed

**Example of well-defined inputs:**

```yaml wrap
---
on:
  workflow_dispatch:
    inputs:
      tracker_id:
        description: "Unique identifier for this orchestration run (e.g., 'run-2024-01-15-001')"
        required: true
        type: string
      target_repos:
        description: "JSON array of repository names to process (e.g., '[\"repo1\", \"repo2\"]')"
        required: true
        type: string
      dry_run:
        description: "If true, validate configuration without executing actions"
        required: false
        type: boolean
        default: false
---
```

#### Security Considerations

- **Same-repository only** — Cannot dispatch workflows in other repositories. This prevents cross-repository workflow triggering which could be a security risk.
- **Allowlist enforcement** — Only workflows explicitly listed in the `workflows` configuration can be dispatched. Requests for unlisted workflows are rejected.
- **Compile-time validation** — Workflows are validated at compile time to catch configuration errors early.

#### Use Cases

**Orchestration:**
```yaml wrap
safe-outputs:
  dispatch-workflow:
    workflows: [seeder-worker, processor-worker]
    max: 2
```

An orchestrator workflow can dispatch seeder and worker workflows to discover and process work items.

**Multi-Stage Pipelines:**
```yaml wrap
safe-outputs:
  dispatch-workflow: [deploy-staging, run-tests]
```

A workflow can trigger deployment and testing workflows as separate, trackable runs.

> [!WARNING]
> **Workflow compilation required**: Markdown workflows (`.md` files) must be compiled to `.lock.yml` files before they can be dispatched. If you reference a workflow that only exists as `.md`, compilation will fail with an error asking you to run `gh aw compile <workflow-name>`.

> [!TIP]
> **Define workflow_dispatch inputs explicitly**: The compiler automatically generates MCP tool schemas based on the target workflow's `workflow_dispatch` inputs. Always define inputs in your target workflows to ensure the agent knows what parameters to provide.

> [!NOTE]
> **Input validation**: GitHub validates `workflow_dispatch` inputs at dispatch time. If the agent provides inputs that don't match the workflow's input schema (wrong type, missing required fields, invalid choices), the dispatch will fail with a validation error.

### Agent Session Creation (`create-agent-session:`)

Creates Copilot agent sessions. Requires `COPILOT_GITHUB_TOKEN` or `GH_AW_GITHUB_TOKEN` PAT—default `GITHUB_TOKEN` lacks permissions.

### Assign to Agent (`assign-to-agent:`)

Programmatically assigns GitHub Copilot agents to **existing** issues or pull requests through workflow automation. This safe output automates the [standard GitHub workflow for assigning issues to Copilot](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/coding-agent/create-a-pr#assigning-an-issue-to-copilot). Requires fine-grained PAT with actions, contents, issues, pull requests write access stored as `GH_AW_AGENT_TOKEN`, or GitHub App token. Supported agents: `copilot` (`copilot-swe-agent`).

Auto-resolves target from workflow context (issue/PR events) when `issue_number` or `pull_number` not explicitly provided. Restrict with `allowed` list. Target: `"triggering"` (default), `"*"` (any), or number.

```yaml wrap
safe-outputs:
  assign-to-agent:
    name: "copilot"            # default agent (default: "copilot")
    allowed: [copilot]         # restrict to specific agents (optional)
    max: 1                     # max assignments (default: 1)
    target: "triggering"       # "triggering" (default), "*", or number
    target-repo: "owner/repo"  # cross-repository
```

**Behavior:**
- `target: "triggering"` — Auto-resolves from `github.event.issue.number` or `github.event.pull_request.number`
- `target: "*"` — Requires explicit `issue_number` or `pull_number` in agent output
- `target: "123"` — Always uses issue/PR #123

**Assignee Filtering:**
When `allowed` list is configured, existing agent assignees not in the list are removed while regular user assignees are preserved.

> [!TIP]
> Assignment methods
> 
> Use `assign-to-agent` when you need to programmatically assign agents to **existing** issues or PRs through workflow automation. If you're creating new issues and want to assign an agent immediately, use `assignees: copilot` in your [`create-issue`](#issue-creation-create-issue) configuration instead, which is simpler:
> 
> ```yaml
> safe-outputs:
>   create-issue:
>     assignees: copilot  # Assigns agent when creating issue
> ```
> 
> **Important**: Both methods use the **same token** (`GH_AW_AGENT_TOKEN`) and **same GraphQL API** (`replaceActorsForAssignable` mutation) to assign copilot. When you use `assignees: copilot` in create-issue, the copilot assignee is automatically filtered out and assigned in a separate post-step using the agent token and GraphQL, identical to the `assign-to-agent` safe output.
> 
> Both methods result in the same outcome as [manually assigning issues to Copilot through the GitHub UI](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/coding-agent/create-a-pr#assigning-an-issue-to-copilot). See [GitHub Tokens reference](/gh-aw/reference/tokens/#gh_aw_agent_token-agent-assignment) for token configuration details and [GitHub's official Copilot coding agent documentation](https://docs.github.com/en/copilot/concepts/agents/coding-agent/about-coding-agent) for more about the Copilot agent.

### Assign to User (`assign-to-user:`)

Assigns users to issues. Restrict with `allowed` list. Target: `"triggering"` (issue event), `"*"` (any), or number. Supports single or multiple assignees.

```yaml wrap
safe-outputs:
  assign-to-user:
    allowed: [user1, user2]    # restrict to specific users
    max: 3                     # max assignments (default: 1)
    target: "*"                # "triggering" (default), "*", or number
    target-repo: "owner/repo"  # cross-repository
```

## Cross-Repository Operations

Many safe outputs support `target-repo`. Requires PAT (`github-token` or `GH_AW_GITHUB_TOKEN`)—default `GITHUB_TOKEN` is current-repo only. Use specific names (no wildcards).

```yaml wrap
safe-outputs:
  github-token: ${{ secrets.CROSS_REPO_PAT }}
  create-issue:
    target-repo: "org/tracking-repo"
```

## Automatically Added Tools

When `create-pull-request` or `push-to-pull-request-branch` are configured, file editing tools (Edit, MultiEdit, Write, NotebookEdit) and git commands (`checkout`, `branch`, `switch`, `add`, `rm`, `commit`, `merge`) are automatically enabled.

## Security and Sanitization

Auto-sanitization: XML escaped, HTTPS only, domain allowlist (GitHub by default), 0.5MB/65k line limits, control char stripping.

```yaml wrap
safe-outputs:
  allowed-domains: [api.github.com]  # GitHub domains always included
  allowed-github-references: []      # Escape all GitHub references
```

**Domain Filtering** (`allowed-domains`): Controls which domains are allowed in URLs. URLs from other domains are replaced with `(redacted)`.

**Reference Escaping** (`allowed-github-references`): Controls which GitHub repository references (`#123`, `owner/repo#456`) are allowed in workflow output. When configured, references to unlisted repositories are escaped with backticks to prevent GitHub from creating timeline items. This is particularly useful for [SideRepoOps](/gh-aw/guides/siderepoops/) workflows to prevent automation from cluttering your main repository's timeline.

Configuration options:
- `[]` — Escape all references (prevents all timeline items)
- `["repo"]` — Allow only the target repository's references
- `["repo", "owner/other-repo"]` — Allow specific repositories
- Not specified (default) — All references allowed

Example for clean automation:

```yaml wrap
safe-outputs:
  allowed-github-references: []  # Escape all references
  create-issue:
    target-repo: "my-org/main-repo"
```

With `[]`, references like `#123` become `` `#123` `` and `other/repo#456` becomes `` `other/repo#456` ``, preventing timeline clutter while preserving the information.

## Global Configuration Options

### Custom GitHub Token (`github-token:`)

Token precedence: `GH_AW_GITHUB_TOKEN` → `GITHUB_TOKEN` (default). Override globally or per safe output:

```yaml wrap
safe-outputs:
  github-token: ${{ secrets.CUSTOM_PAT }}  # global
  create-issue:
  create-pull-request:
    github-token: ${{ secrets.PR_PAT }}    # per-output
```

### GitHub App Token (`app:`)

Use GitHub App tokens for enhanced security: on-demand minting, auto-revocation, fine-grained permissions, better attribution. Supports config import from shared workflows.

```yaml wrap
safe-outputs:
  app:
    app-id: ${{ vars.APP_ID }}
    private-key: ${{ secrets.APP_PRIVATE_KEY }}
    owner: "my-org"                # optional: installation owner
    repositories: ["repo1", "repo2"] # optional: scope to repos
  create-issue:
```

### Maximum Patch Size (`max-patch-size:`)

Limits git patch size for PR operations (1-10,240 KB, default: 1024 KB):

```yaml wrap
safe-outputs:
  max-patch-size: 512  # max patch size in KB
  create-pull-request:
```

## Assigning to Copilot

Use `assignees: copilot` or `reviewers: copilot` for bot assignment. Requires `GH_AW_AGENT_TOKEN` (or fallback to `GH_AW_GITHUB_TOKEN`/`GITHUB_TOKEN`)—uses GraphQL API to assign the bot.

## Custom Runner Image

Specify custom runner for safe output jobs (default: `ubuntu-slim`): `runs-on: ubuntu-22.04`

## Threat Detection

Auto-enabled. Analyzes output for prompt injection, secret leaks, malicious patches. See [Threat Detection Guide](/gh-aw/reference/threat-detection/).

## Projects, Monitoring, and Orchestration Patterns

Common combinations:

- **Projects & Monitoring:** `create-issue` + `update-project` + `create-project-status-update`
- **Orchestration:** `dispatch-workflow` (orchestrator/worker pattern), optionally paired with Projects updates

See:
- [Projects & Monitoring](/gh-aw/guides/monitoring/)
- [Orchestration](/gh-aw/guides/orchestration/)

## Custom Messages (`messages:`)

Customize notifications using template variables and Markdown. Import from shared workflows (local overrides imported).

```yaml wrap
safe-outputs:
  messages:
    footer: "> 🤖 Generated by [{workflow_name}]({run_url})"
    append-only-comments: true
    run-started: "🚀 Processing {event_type}..."
    run-success: "✅ Completed successfully"
    run-failure: "❌ Encountered {status}"
  create-issue:
```

**Templates**: `footer`, `footer-install`, `staged-title`, `staged-description`, `run-started`, `run-success`, `run-failure`

**Options**: `append-only-comments` (default: `false`)

**Variables**: `{workflow_name}`, `{run_url}`, `{triggering_number}`, `{workflow_source}`, `{workflow_source_url}`, `{event_type}`, `{status}`, `{operation}`

## Related Documentation

- [Threat Detection Guide](/gh-aw/reference/threat-detection/) - Complete threat detection documentation and examples
- [Frontmatter](/gh-aw/reference/frontmatter/) - All configuration options for workflows
- [Workflow Structure](/gh-aw/reference/workflow-structure/) - Directory layout and organization
- [Command Triggers](/gh-aw/reference/command-triggers/) - Special /my-bot triggers and context text
- [CLI Commands](/gh-aw/setup/cli/) - CLI commands for workflow management
