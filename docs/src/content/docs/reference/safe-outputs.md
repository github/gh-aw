---
title: Safe Outputs
description: Learn about safe output processing features that enable creating GitHub issues, comments, and pull requests without giving workflows write permissions.
sidebar:
  order: 800
---

The [`safe-outputs:`](/gh-aw/reference/glossary/#safe-outputs) (validated GitHub operations) element of your workflow's [frontmatter](/gh-aw/reference/glossary/#frontmatter) declares that your agentic workflow should conclude with optional automated actions based on the agentic workflow's output. This enables your workflow to write content that is then automatically processed to create GitHub issues, comments, pull requests, or add labels - all without giving the agentic portion of the workflow any write permissions.

Safe outputs enforce security through separation: agents run read-only and request actions via structured output, while separate permission-controlled jobs execute those requests. This provides least privilege, defense against prompt injection, auditability, and controlled limits per operation.

When no `safe-outputs:` section is present (or when only [system types](#system-types-auto-enabled) are configured), `create-issue` is automatically enabled with conservative defaults (`max: 1`, labels and title-prefix set to the workflow ID). To opt out, add an explicit `safe-outputs:` section with the outputs you want.

Example:

```yaml wrap
safe-outputs:
  create-issue:
```

The agent requests issue creation; a separate job with `issues: write` creates it.

## Available Safe Output Types

The tables below summarize the built-in safe output handlers. `noop`, `missing-tool`, and `missing-data` are always available, and `create-issue` is auto-injected only when no non-system safe outputs are configured.

### Issues & Discussions

| Output | Key | Description |
|--------|-----|-------------|
| [Create Issue](#issue-creation-create-issue) | `create-issue` | Create GitHub issues (max: 1) |
| [Update Issue](#issue-updates-update-issue) | `update-issue` | Update issue status, title, or body (max: 1) |
| [Close Issue](#close-issue-close-issue) | `close-issue` | Close issues with comment (max: 1) |
| [Link Sub-Issue](#link-sub-issue-link-sub-issue) | `link-sub-issue` | Link issues as sub-issues (max: 1) |
| [Create Discussion](#discussion-creation-create-discussion) | `create-discussion` | Create GitHub discussions (max: 1) |
| [Update Discussion](#discussion-updates-update-discussion) | `update-discussion` | Update discussion title, body, or labels (max: 1) |
| [Close Discussion](#close-discussion-close-discussion) | `close-discussion` | Close discussions with comment and resolution (max: 1) |

### Pull Requests

| Output | Key | Description |
|--------|-----|-------------|
| [Create PR](/gh-aw/reference/safe-outputs-pull-requests/#pull-request-creation-create-pull-request) | `create-pull-request` | Create pull requests with code changes (default max: 1, configurable) |
| [Update PR](/gh-aw/reference/safe-outputs-pull-requests/#pull-request-updates-update-pull-request) | `update-pull-request` | Update PR title or body (max: 1) |
| [Close PR](/gh-aw/reference/safe-outputs-pull-requests/#close-pull-request-close-pull-request) | `close-pull-request` | Close pull requests without merging (max: 10) |
| [Approve Workflow Run](/gh-aw/reference/safe-outputs-pull-requests/#approve-workflow-run-approve-workflow-run) | `approve-workflow-run` | Approve a pending workflow run in the "action required" state (max: 1, experimental) |
| [Merge PR](/gh-aw/reference/safe-outputs-pull-requests/#merge-pull-request-merge-pull-request) | `merge-pull-request` | Merge pull requests after policy gates pass (max: 1, experimental) |
| [PR Review Comments](/gh-aw/reference/safe-outputs-pull-requests/#pr-review-comments-create-pull-request-review-comment) | `create-pull-request-review-comment` | Create review comments on code lines (max: 10) |
| [Reply to PR Review Comment](/gh-aw/reference/safe-outputs-pull-requests/#reply-to-pr-review-comment-reply-to-pull-request-review-comment) | `reply-to-pull-request-review-comment` | Reply to existing review comments (max: 10) |
| [Resolve PR Review Thread](/gh-aw/reference/safe-outputs-pull-requests/#resolve-pr-review-thread-resolve-pull-request-review-thread) | `resolve-pull-request-review-thread` | Resolve review threads after addressing feedback (max: 10) |
| [Add Reviewer](/gh-aw/reference/safe-outputs-pull-requests/#add-reviewer-add-reviewer) | `add-reviewer` | Add reviewers to pull requests (max: 3) |
| [Push to PR Branch](/gh-aw/reference/safe-outputs-pull-requests/#push-to-pr-branch-push-to-pull-request-branch) | `push-to-pull-request-branch` | Push changes to PR branch (default max: 1, configurable; cross-repo supported via `target-repo` when the target repository is checked out) |

### Labels, Assignments & Reviews

| Output | Key | Description |
|--------|-----|-------------|
| [Add Comment](#comment-creation-add-comment) | `add-comment` | Post comments on issues, PRs, or discussions (max: 1) |
| [Hide Comment](#hide-comment-hide-comment) | `hide-comment` | Hide comments on issues, PRs, or discussions (max: 5) |
| [Add Labels](#add-labels-add-labels) | `add-labels` | Add labels to issues or PRs (max: 3) |
| [Remove Labels](#remove-labels-remove-labels) | `remove-labels` | Remove labels from issues or PRs (max: 3) |
| [Assign Milestone](#assign-milestone-assign-milestone) | `assign-milestone` | Assign issues to milestones (max: 1) |
| [Assign to Agent](#assign-to-agent-assign-to-agent) | `assign-to-agent` | Assign Copilot coding agent to issues or PRs (max: 1) |
| [Assign to User](#assign-to-user-assign-to-user) | `assign-to-user` | Assign users to issues (max: 1) |
| [Unassign from User](#unassign-from-user-unassign-from-user) | `unassign-from-user` | Remove user assignments from issues or PRs (max: 1) |
| [Set Issue Type](#set-issue-type-set-issue-type) | `set-issue-type` | Set or clear the type of GitHub issues (max: 5) |
| [Set Issue Field](#set-issue-field-set-issue-field) | `set-issue-field` | Set one issue field value by name/value (max: 5) |

### External Integrations

| Output | Key | Description |
|--------|-----|-------------|
| [Jira Create Issue](#jira-safe-outputs) | `jira-create-issue` | Create a Jira issue (max: 1) |
| [Jira Update Issue](#jira-safe-outputs) | `jira-update-issue` | Update a Jira issue summary or description (max: 1) |
| [Jira Add Comment](#jira-safe-outputs) | `jira-add-comment` | Add a comment to a Jira issue (max: 1) |
| [Jira Add Label](#jira-safe-outputs) | `jira-add-label` | Add one label to a Jira issue (max: 1) |

### Projects, Releases & Assets

| Output | Key | Description |
|--------|-----|-------------|
| [Create Project](#project-creation-create-project) | `create-project` | Create new GitHub Projects boards (max: 1, cross-repo) |
| [Update Project](#project-board-updates-update-project) | `update-project` | Manage GitHub Projects boards (max: 10, same-repo only) |
| [Create Project Status Update](#project-status-updates-create-project-status-update) | `create-project-status-update` | Create project status updates |
| [Update Release](#release-updates-update-release) | `update-release` | Update GitHub release descriptions (max: 1) |
| [Upload Artifact](#artifact-uploads-upload-artifact) | `upload-artifact` | Upload files as run-scoped GitHub Actions artifacts (max: 1 by default) |
| [Upload Assets](#asset-uploads-upload-asset) | `upload-asset` | Upload files to orphaned git branch (max: 10, same-repo only). **Prefer `upload-artifact` with `skip-archive` instead.** |

### Azure DevOps Work Items

| Output | Key | Description |
|--------|-----|-------------|
| [Create Work Item](#azure-devops-work-items) | `ado-create-work-item` | Create an Azure DevOps work item (max: 1, experimental) |
| [Update Work Item](#azure-devops-work-items) | `ado-update-work-item` | Update explicitly enabled fields on a scoped work item (max: 1, experimental) |
| [Comment on Work Item](#azure-devops-work-items) | `ado-comment-on-work-item` | Add a comment to a scoped work item (max: 1, experimental) |
| [Assign Work Item](#azure-devops-work-items) | `ado-assign-work-item` | Assign an allowed identity to a scoped work item (max: 1, experimental) |
| [Link Work Items](#azure-devops-work-items) | `ado-link-work-items` | Link two scoped work items (max: 5, experimental) |
| [Upload Work Item Attachment](#azure-devops-work-items) | `ado-upload-workitem-attachment` | Attach a staged workspace file to a work item (max: 1, experimental) |

### Security & Agent Tasks

| Output | Key | Description |
|--------|-----|-------------|
| [Dispatch Workflow](#workflow-dispatch-dispatch-workflow) | `dispatch-workflow` | Trigger other workflows with inputs (max: 3, same-repo only) |
| [Call Workflow](#workflow-call-call-workflow) | `call-workflow` | Call reusable workflows via compile-time fan-out (max: 1, same-repo only) |
| [Dispatch Repository Event](#repository-dispatch-dispatch-repository) | `dispatch-repository` | Trigger `repository_dispatch` events in external repositories, experimental (cross-repo) |
| [Code Scanning Alerts](#code-scanning-alerts-create-code-scanning-alert) | `create-code-scanning-alert` | Generate SARIF security advisories (max: unlimited, same-repo only) |
| [Autofix Code Scanning Alerts](#autofix-code-scanning-alerts-autofix-code-scanning-alert) | `autofix-code-scanning-alert` | Create automated fixes for code scanning alerts (max: 10, same-repo only) |
| [Create Check Run](#check-run-creation-create-check-run) | `create-check-run` | Create GitHub Check Runs to surface analysis results in the PR checks UI (default max: 1, same-repo only) |
| [Create Agent Session](/gh-aw/reference/copilot-cloud-agent/#create-agent-session) | `create-agent-session` | Create Copilot coding agent sessions (max: 1) |

### Linear

| Output | Key | Description |
|--------|-----|-------------|
| [Create Linear Issue](#linear-safe-outputs) | `linear-create-issue` | Create an issue in a configured Linear team (max: 1, experimental) |
| [Add Linear Comment](#linear-safe-outputs) | `linear-add-comment` | Comment on a configured Linear issue (max: 1, experimental) |
| [Update Linear Issue](#linear-safe-outputs) | `linear-update-issue` | Update enabled fields on a configured Linear issue (max: 1, experimental) |

#### Linear Safe Outputs

:::caution[Experimental]
Linear Safe Outputs are experimental. Compiling a workflow that enables any Linear Safe Output emits `Using experimental feature: Linear safe outputs`.
:::

Linear Safe Outputs use Linear's public GraphQL API from the isolated `safe_outputs` job. Configure a personal Linear API key through a secret expression. The credential is not available to the agent.

```yaml wrap
safe-outputs:
  linear-token: ${{ secrets.LINEAR_API_KEY }}
  linear-create-issue:
    team-id: ${{ vars.LINEAR_TEAM_ID }}
    project-id: "810f57a7e383"
    max: 1
  linear-add-comment:
    target: "ENG-123"
  linear-update-issue:
    target: "ENG-123"
    title: true
    body: true
```

`team-id` accepts a Linear team model UUID, available through Linear's model UUID tooling or API, or a GitHub Actions expression such as `${{ vars.LINEAR_TEAM_ID }}`. Optional `project-id` fixes new issues to a trusted project and accepts either the 12-character identifier from a Linear project URL or its model UUID. When omitted, the compiler loads `LINEAR_TEAM_ID` and `LINEAR_PROJECT_ID` from same-named repository or organization variables. Values in `safe-outputs.env` can override those defaults; explicit `team-id` and `project-id` values take precedence over environment fallbacks. Comment and update targets are fixed trusted configuration and accept either a Linear issue model UUID or shorthand identifier such as `ENG-123`. Updates replace only the enabled `title` and `body` fields. All agent-provided titles, descriptions, and comments use standard Safe Outputs sanitization.

### System Types (Auto-Enabled)

| Output | Key | Description |
|--------|-----|-------------|
| [No-Op](#no-op-logging-noop) | `noop` | Log completion message for transparency (max: 1, same-repo only) |
| [Missing Tool](#missing-tool-reporting-missing-tool) | `missing-tool` | Report missing tools (max: unlimited, same-repo only) |
| [Missing Data](#missing-data-reporting-missing-data) | `missing-data` | Report missing data required to achieve goals (max: unlimited, same-repo only) |
| [Create Issue](#issue-creation-create-issue) | `create-issue` | Auto-injected when no `safe-outputs:` section is present or when only system types (`noop`, `missing-tool`, `missing-data`) are configured (max: 1, labels and title-prefix set to workflow ID). |

### Custom Safe Output Jobs (`jobs:`)

Create custom post-processing jobs registered as Model Context Protocol (MCP) tools. Support standard GitHub Actions properties and auto-access agent output via `$GH_AW_AGENT_OUTPUT`. See [Custom Safe Output Jobs](/gh-aw/reference/custom-safe-outputs/).

### GitHub Action Wrappers (`actions:`)

Mount any public GitHub Action as a once-callable MCP tool. The compiler pins the action reference to a SHA at compile time and derives the tool's input schema from the action's `action.yml`. See [GitHub Action Wrappers](/gh-aw/reference/custom-safe-outputs/#github-action-wrappers-safe-outputsactions).

## Jira Safe Outputs

Jira safe outputs call Jira Cloud REST API v3 from the privileged safe-output job. Jira credentials are not exposed to the agent or included in `agent_output`.

```aw wrap
---
on:
  workflow_dispatch:
safe-outputs:
  env:
    JIRA_BASE_URL: ${{ secrets.JIRA_BASE_URL }}
    JIRA_USER_EMAIL: ${{ secrets.JIRA_USER_EMAIL }}
    JIRA_API_TOKEN: ${{ secrets.JIRA_API_TOKEN }}
  jira-create-issue:
    max: 1
  jira-update-issue:
    max: 1
  jira-add-comment:
    max: 1
  jira-add-label:
    max: 3
---

# Jira maintenance

Use Jira safe outputs for Jira mutations.
```

`JIRA_BASE_URL` is the Jira API base without `/rest/api/3`. For unscoped API tokens, use the site URL, such as `https://example.atlassian.net`. For scoped API tokens, use the Atlassian gateway URL, such as `https://api.atlassian.com/ex/jira/<cloudId>`. The initial authentication mechanism uses an Atlassian account email and API token with HTTP Basic authentication.

Each output accepts `max` and `staged`. In staged mode, the handler writes a Jira-specific preview without requiring credentials or sending an HTTP request.

| Frontmatter key | Agent tool | Inputs |
|---|---|---|
| `jira-create-issue` | `jira_create_issue` | Required: `project_key`, `issue_type`, `summary`; optional: `description` |
| `jira-update-issue` | `jira_update_issue` | Required: `issue_key`; at least one of `summary` or `description` |
| `jira-add-comment` | `jira_add_comment` | Required: `issue_key`, `body` |
| `jira-add-label` | `jira_add_label` | Required: `issue_key`, `label` |

Descriptions and comment bodies remain plain strings at the agent boundary. The runtime converts them deterministically to Atlassian Document Format version 1, preserving paragraphs and line breaks. `jira_add_label` uses Jira's additive field-update operation and does not replace existing labels.

> [!IMPORTANT]
> Unprefixed tools such as `create_issue`, `update_issue`, `add_comment`, and `add_labels` operate on GitHub. Jira operations always use the `jira_` prefix.

This initial integration does not support transitions, assignment, custom fields, priorities, components, attachments, issue links, subtasks, label removal, JQL, bulk operations, arbitrary Jira REST calls, or OAuth installation flows. Update, comment, and label operations require a known Jira issue key; they cannot reference a Jira issue created earlier in the same run.

## Steering Issues (`steer:`)

:::caution[Experimental]
`steer` is an experimental option. `gh aw compile` emits an experimental feature warning when a workflow uses it.
:::

Set `steer: true` to create a run-scoped issue during the activation job, before the agent starts. Users can add comments containing the keyword `steer` while the run is in progress. The injected prompt identifies the exact issue and instructs the agent to read relevant user-authored comments with the GitHub MCP `issue_read` tool.

Steering enables the GitHub MCP issues toolset for comment reads and requires top-level `issues: read` permission. The compiler reports an error instead of adding that permission automatically.

```yaml
permissions:
  contents: read
  issues: read

safe-outputs:
  steer: true
```

The activation and conclusion jobs require `issues: write` through the global [`github-token`](#custom-github-token-github-token) or [`github-app`](#using-a-github-app-for-authentication-github-app) safe-output credential. On success, the conclusion job closes the steering issue and links a created pull request when available. On failure, the same issue is retitled and updated with the agent failure report instead of creating a second issue. Because reuse requires a workflow-repository issue, `steer` cannot be combined with `safe-outputs.failure-issue-repo`.

In [staged mode](#staged-mode), no steering issue is created because staged runs must not perform API side effects.

### Issue Creation (`create-issue:`)

Creates GitHub issues based on workflow output.

```yaml wrap
safe-outputs:
  create-issue:
    title-prefix: "[ai] "            # prefix for titles
    labels: [automation, agentic]    # labels to attach
    allowed-fields: [Priority, Iteration] # restrict issue fields this workflow may set
    assignees: [user1, copilot]      # assignees (use 'copilot' for bot)
    max: 5                           # max issues (default: 1)
    expires: 7                       # auto-close after 7 days (or false to disable)
    group: true                      # group as sub-issues under parent
    close-older-issues: true         # close previous issues from same workflow
    deduplicate-by-title: 1          # drop duplicate titles (true=exact, integer=edit distance)
    normalize-closing-keywords: true # strip backticks around recognized issue-closing keywords in body text
    target-repo: "owner/repo"        # cross-repository
    allowed-repos: ["org/repo1", "org/repo2"]  # additional allowed repositories
    github-token: ${{ secrets.SOME_CUSTOM_TOKEN }} # optional custom token for permissions
```

See [Cross-Repository Operations](/gh-aw/reference/cross-repository/) for comprehensive documentation on `target-repo`, `allowed-repos`, and cross-repository authentication.

> [!TIP]
> Use `footer: false` to omit the AI-generated footer while preserving workflow-id markers for searchability. See [Footer Control](/gh-aw/reference/footers/) for details.

#### `create_issue` tool field schema (`fields`)

`create_issue.body` must be between **20** and **65000** characters.

| Parameter | Type | Required | Description | Example |
|-----------|------|----------|-------------|---------|
| `fields` | `array<object>` | No | Optional issue field updates to apply immediately after issue creation. | `[{"name":"Priority","value":"P1"}]` |
| `fields[].name` | `string` | Yes (when item exists) | Issue field display name. Match the repository field label (case-insensitive matching is supported). | `"Priority"` |
| `fields[].value` | `string \| number` | Yes (when item exists) | Field value. Use a number for numeric fields; otherwise use a string (single select, date `YYYY-MM-DD`, text). For multi-select fields, pass a comma-separated list of option names to select multiple options. | `"Sprint 42"` |

```json
{
  "type": "create_issue",
  "title": "Triage: flaky parser test",
  "body": "Intermittent failure detected in CI.",
  "fields": [
    { "name": "Priority", "value": "High" },
    { "name": "Tags", "value": "Bug, Regression" },
    { "name": "Story Points", "value": 3 }
  ]
}
```

#### Auto-Expiration

The `expires` field auto-closes issues after a time period. Supports day-string format (`7d`, `2w`, `1m`, `1y`, `2h`) or `false` to disable expiration. Integer values (e.g., `expires: 7`) are also accepted as shorthand for days and can be migrated to string format with `gh aw fix --write`. Generates `agentics-maintenance.yml` workflow that runs at the minimum required frequency based on the shortest expiration time across all workflows:

- 1 day or less → every 2 hours
- 2 days → every 6 hours
- 3-4 days → every 12 hours
- 5+ days → daily

Hours less than 24 are treated as 1 day minimum for expiration calculation.

To explicitly disable expiration (useful when create-issue has a default expiration), use `expires: false`:

#### Issue Grouping

The `group` field (default: `false`) organizes multiple issues as sub-issues under a parent issue. The parent is identified by a `<!-- gh-aw-group: ... -->` marker derived from the workflow name; children are linked through GitHub sub-issue relationships; and each parent can hold up to 64 sub-issues. This is useful for workflows that create sets of related issues, such as plans broken into tasks or batch processing runs.


```yaml wrap
safe-outputs:
  create-issue:
    title-prefix: "[plan] "
    labels: [plan, ai-generated]
    max: 5
    group: true
```

#### Auto-Close Older Issues

The `close-older-issues` field (default: `false`) closes previous open issues from the same workflow after a new issue is created. It searches for open issues using the `gh-aw-workflow-id` marker (or `gh-aw-close-key` when `close-older-key` is set), closes up to 10 of them as "not planned," and adds a comment linking to the new issue. In reusable-workflow scenarios, the `gh-aw-workflow-call-id` marker is used for precise per-caller matching, so issues from different callers sharing the same reusable workflow are not cross-closed. The cleanup runs only if new issue creation succeeds, which makes it a good fit for recurring reports or status updates where only the latest issue should remain open.

```yaml wrap
safe-outputs:
  create-issue:
    title-prefix: "[weekly-report] "
    labels: [report, automation]
    close-older-issues: true
```

#### Group By Day

The `group-by-day` field (default: `false`) groups same-day workflow runs into a single issue. The handler looks for an existing open issue created **today (UTC)** using the workflow marker (`gh-aw-workflow-id`, or `gh-aw-workflow-call-id` in reusable-workflow scenarios, or `gh-aw-close-key` when `close-older-key` is set), and posts the new content as a **comment** instead of creating a new issue. This is useful for frequent scheduled workflows, such as runs every four hours, because all runs for the day contribute to one issue. Posting as a comment does not consume a max-count slot; if the pre-check fails, normal issue creation is used as a fallback.

```yaml wrap
safe-outputs:
  create-issue:
    title-prefix: "[Contribution Check Report]"
    labels: [report]
    close-older-issues: true
    group-by-day: true
```

#### Title-Based Deduplication

The `deduplicate-by-title` field drops duplicate issues by comparing titles before creation. Accepts:

- `true` — match titles exactly (after normalization)
- integer `0`–`100` — match titles within the given Levenshtein edit distance (e.g., `1` allows one-character differences)

Deduplication runs at both the MCP tool-call boundary (within-run drops with immediate `duplicate_dropped` feedback to the agent) and at apply time (within-run plus open and recently-closed repository issues). Dropped items are recorded in the safe-output summary with the matched title, edit distance, and source (`mcp-precheck`, `within-run`, or `repo-level`).

```yaml wrap
safe-outputs:
  create-issue:
    title-prefix: "[triage] "
    labels: [bug]
    deduplicate-by-title: 1   # tolerate one-character title differences
```

#### Searching for Workflow-Created Items

All items created by workflows (issues, pull requests, discussions, and comments) include a hidden **workflow-id marker** in their body:

```html
<!-- gh-aw-workflow-id: WORKFLOW_NAME -->
```

You can use this marker to find all items created by a specific workflow on GitHub.com.

**Search Examples:**

```
# Open issues from a specific workflow
repo:owner/repo is:issue is:open "gh-aw-workflow-id: daily-team-status" in:body

# All items from any workflow in an org
org:your-org "gh-aw-workflow-id:" in:body

# Comments from a specific workflow
repo:owner/repo "gh-aw-workflow-id: bot-responder" in:comments
```

### Close Issue (`close-issue:`)

Closes GitHub issues with an optional comment and state reason. Filters by labels and title prefix control which issues can be closed.

```yaml wrap
safe-outputs:
  close-issue:
    target: "triggering"              # "triggering" (default), "*", or number
    required-labels: [automated]      # only close if ALL these labels are present
    required-title-prefix: "[bot]"    # only close matching prefix
    max: 20                           # max closures (default: 1)
    target-repo: "owner/repo"         # cross-repository
    allowed-repos: ["org/repo1", "org/repo2"]  # additional allowed repositories
    state-reason: "duplicate"         # completed (default), not_planned, duplicate
    allow-body: false               # prevent closing comment (drop body if provided)
```

**Target**: `"triggering"` (requires issue event), `"*"` (any issue), or number (specific issue).

**State Reasons**: `completed`, `not_planned`, `duplicate` (default: `completed`). Can also be set per-item in agent output.

**`duplicate_of`**: When closing as a duplicate (agent sets `state_reason: duplicate`), the agent may also supply a `duplicate_of` field pointing to the canonical issue. Accepts a bare number (`123`), a `#`-prefixed number (`#123`), an `owner/repo#number` reference, or a full GitHub issue URL. When provided, creates a native GitHub **"marked this as a duplicate of #X"** relationship in the timeline — no separate comment needed for the linkage. Falls back gracefully (logs a warning) if the duplicate marking fails.

**`allow-body: false`**: When set, any `body` field the agent provides is dropped (a warning is logged) and the issue is closed without posting a comment. Use this when you want to guarantee a clean close with no duplicate comment — for example, when a prior `add-comment` step already posted the summary.

### Comment Creation (`add-comment:`)

Posts comments on issues, PRs, or discussions. Defaults to triggering item; use `target: "*"` for any, or number for specific items. When combined with `create-issue`, `create-discussion`, or `create-pull-request`, includes "Related Items" section.

Use `required-labels` to only comment on issues/PRs that have **all** of the specified labels. Use `required-title-prefix` to only comment on issues/PRs whose title starts with the given prefix. These filters apply to issues and PRs only (not discussions).

```yaml wrap
safe-outputs:
  add-comment:
    max: 3                       # max comments (default: 1)
    target: "*"                  # "triggering" (default), "*", or number
    allows-comment-ids: ${{ needs.prepare.outputs.comment_ids }} # comment IDs the agent may update when target is "*"
    discussions: true            # request discussions:write permission (default: false)
    target-repo: "owner/repo"    # cross-repository
    allowed-repos: ["org/repo1", "org/repo2"]  # additional allowed repositories
    hide-older-comments: true    # hide previous comments from same workflow
    allowed-reasons: [outdated]  # restrict hiding reasons (optional)
    footer: false                # omit AI-generated footer (default: true)
    normalize-closing-keywords: true # strip backticks around recognized issue-closing keywords in body text
    required-labels: [bot, automated]  # only comment if item has ALL of these labels
    required-title-prefix: "[bot] "    # only comment if item title starts with this prefix
```

> [!TIP]
> Use `footer: false` to suppress the "Generated by..." attribution line in posted comments. See [Footer Control](/gh-aw/reference/footers/) for global and per-handler options.

When `target: "*"` is configured, the agent may update an existing issue or pull request comment by passing `comment_id` only if that ID appears in `allows-comment-ids`. Populate `allows-comment-ids` from trusted workflow state (for example, an earlier step output) rather than agent output.

#### Normalize closing keywords

Set `normalize-closing-keywords: true` to strip wrapping backticks from recognized issue-closing keywords in body text (for example, `` `Closes #123` `` becomes `Closes #123` so GitHub can process it as a closing keyword). This field is supported by `create-issue` and `add-comment` on this page, and by `create-pull-request` in [Safe Outputs (Pull Requests)](/gh-aw/reference/safe-outputs-pull-requests/#pull-request-creation-create-pull-request).

The author of the parent issue, PR, or discussion receiving the comment is automatically preserved as an allowed mention. This means `@username` references to the issue/PR/discussion author are not neutralized when the workflow posts a reply.

#### Hide Older Comments

Set `hide-older-comments: true` to minimize previous comments from the same workflow (identified by `GITHUB_WORKFLOW`) before posting new ones. Useful for status updates. Allowed reasons: `spam`, `abuse`, `off_topic`, `outdated` (default), `resolved`, `low_quality`.

To also minimize comments from one or more other workflows in the same pass, use the object form with `match`:

```yaml wrap
safe-outputs:
  add-comment:
    hide-older-comments:
      enabled: true
      match:
        - other_workflow
        - yet-another
```

`match` is an exact-match list of workflow IDs (the `GITHUB_WORKFLOW` value, not the file name). The current workflow is always included; entries in `match` are added to the set. Set `enabled: false` to disable hiding while keeping the object form. The boolean form (`hide-older-comments: true`) is still supported for the single-workflow case.

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

Collapses comments in GitHub UI with reason. Requires GraphQL node IDs (e.g., `IC_kwDOABCD123456`), not REST numeric IDs. Reasons: `spam`, `abuse`, `off_topic`, `outdated`, `resolved`, `low_quality`.

```yaml wrap
safe-outputs:
  hide-comment:
    max: 5                    # max comments (default: 5)
    target-repo: "owner/repo" # cross-repository
    discussions: true         # request discussions:write permission (default: false)
```

### Add Labels (`add-labels:`)

Adds labels to issues or PRs. Specify `allowed` to restrict to specific labels or glob patterns, or `blocked` to deny specific label patterns regardless of the allow list.

Use `required-labels` to only add labels to issues/PRs that already have **all** of the specified labels. Use `required-title-prefix` to only add labels to issues/PRs whose title starts with the given prefix.

By default, labels that don't already exist in the target repository are rejected with an error. Set `create-if-missing: true` to automatically create any missing labels before they are applied.

```yaml wrap
safe-outputs:
  add-labels:
    allowed: [bug, team-*, area/*] # restrict to specific labels or glob patterns
    blocked: ["~*", "*[bot]"]   # deny labels matching these glob patterns
    max: 3                       # max labels (default: 3)
    target: "*"                  # "triggering" (default), "*", or number
    target-repo: "owner/repo"    # cross-repository
    allowed-repos: ["org/repo1", "org/repo2"]  # additional allowed repositories
    required-labels: [automated, bot]  # only operate if item has ALL of these labels
    required-title-prefix: "[bot] "    # only operate if item title starts with this prefix
    create-if-missing: true            # auto-create labels that don't already exist (default: false)
```

#### Blocked Label Patterns

Both `allowed` and `blocked` accept glob patterns and are evaluated in this order:
1. `blocked` patterns first (security boundary)
2. `allowed` patterns second (if provided)

Any label matching a blocked pattern is rejected, even if it also matches an allowed pattern. This provides infrastructure-level protection against prompt injection attacks in repositories with many labels where maintaining an exhaustive allowlist is impractical.

Common patterns:

| Pattern | Effect |
|---------|--------|
| `~*` | Denies all labels starting with `~` (often used as workflow triggers) |
| `*[bot]` | Denies all labels ending with `[bot]` (administrative bot labels) |
| `stale` | Denies the exact `stale` label |

```yaml wrap
safe-outputs:
  add-labels:
    blocked: ["~*", "*[bot]"]         # Blocked patterns evaluated first
    allowed: [bug, team-*, area/*]    # Allowed patterns applied after blocked check
    max: 5
```

### Remove Labels (`remove-labels:`)

Removes labels from issues or PRs. Specify `allowed` to restrict which labels can be removed (specific labels or glob patterns), or `blocked` to prevent removal of specific label patterns. If a label is not present on the item, it will be silently skipped.

Use `required-labels` to only remove labels from issues/PRs that already have **all** of the specified labels. Use `required-title-prefix` to only remove labels from issues/PRs whose title starts with the given prefix.

```yaml wrap
safe-outputs:
  remove-labels:
    allowed: [automated, team-*] # restrict to specific labels or glob patterns (optional)
    blocked: ["~*"]              # deny removal of labels matching these glob patterns
    issues: true                 # include issues: write permission (default: true)
    pull-requests: false         # exclude pull-requests: write permission (default: true)
    max: 3                       # max operations (default: 3)
    target: "*"                  # "triggering" (default), "*", or number
    target-repo: "owner/repo"    # cross-repository
    allowed-repos: ["org/repo1", "org/repo2"]  # additional allowed repositories
    required-labels: [automated]  # only operate if item has ALL of these labels
    required-title-prefix: "[bot] "  # only operate if item title starts with this prefix
```

**Target**: `"triggering"` (requires issue/PR event), `"*"` (any issue/PR), or number (specific issue/PR).

Set `issues: false` or `pull-requests: false` to omit the corresponding write permission. Both default to `true` when omitted, and at least one must be enabled.

When `allowed` is omitted or set to `null`, any labels can be removed. Use `allowed` to restrict removal to specific labels or glob patterns, providing control over which labels agents can manipulate. The `blocked` field takes precedence over `allowed`.

**Example use case**: Label lifecycle management where agents add temporary labels during triage and remove them once processed.

```yaml wrap
safe-outputs:
  add-labels:
    allowed: [needs-triage, automation]
  remove-labels:
    allowed: [needs-triage]  # agents can remove triage label after processing
```

### Add Reviewer (`add-reviewer:`)

Adds reviewers to pull requests.

See the full reference: [Safe Outputs (Pull Requests) — add-reviewer](/gh-aw/reference/safe-outputs-pull-requests/#add-reviewer-add-reviewer)

### Assign Milestone (`assign-milestone:`)

Assigns issues to milestones. Specify `allowed` to restrict to specific milestone titles. Agents can provide a milestone by title (`milestone_title`) instead of by number (`milestone_number`), and the handler resolves the number internally.

```yaml wrap
safe-outputs:
  assign-milestone:
    allowed: [v1.0, v2.0]    # restrict to specific milestone titles
    auto_create: true         # auto-create milestones in the allowed list if they don't exist
    max: 1                   # max assignments (default: 1)
    target-repo: "owner/repo" # cross-repository
    required-labels: [automated]     # only assign if item has ALL these labels
    required-title-prefix: "[bot] "  # only assign if item title starts with this prefix
    github-token: ${{ secrets.SOME_CUSTOM_TOKEN }} # optional custom token for permissions
```

When `auto_create: true` is set, any milestone from the `allowed` list that does not yet exist in the repository is created automatically before the assignment. Without `auto_create`, the handler returns a clear error listing the available milestones and suggesting `auto_create: true`.

### Issue Updates (`update-issue:`)

Updates issue status, title, or body. Only explicitly enabled fields can be updated. Status must be "open" or "closed". The `operation` field controls how body updates are applied: `append` (default), `prepend`, `replace`, or `replace-island`. Use `required-title-prefix` to restrict updates to issues whose titles start with a specific prefix, and `required-labels` to restrict to issues that have all the specified labels.

```yaml wrap
safe-outputs:
  update-issue:
    status:                   # enable status updates
    title:                    # enable title updates
    body:                     # enable body updates
    required-title-prefix: "[bot] "  # only update issues with this title prefix
    required-labels: [automated]     # only update if ALL these labels are present
    max: 3                    # max updates (default: 1)
    target: "*"               # "triggering" (default), "*", or number
    target-repo: "owner/repo" # cross-repository
    github-token: ${{ secrets.SOME_CUSTOM_TOKEN }} # optional custom token for permissions
```

**Target**: `"triggering"` (requires issue event), `"*"` (any issue), or number (specific issue).

When using `target: "*"`, the agent must provide `issue_number` or `item_number` in the output to identify which issue to update.

**Required Filters**: When `required-title-prefix` is set, the update is rejected if the target issue's current title does not start with the specified prefix. When `required-labels` is set, the update is rejected unless the issue has **all** of the specified labels. These filters ensure agents can only modify issues that have been explicitly tagged for automated updates.

**Operation Types** (for body updates):

- `append` (default): Adds content to the end with separator and attribution
- `prepend`: Adds content to the start with separator and attribution
- `replace`: Completely replaces existing body with new content and attribution
- `replace-island`: Updates a specific section marked with HTML comments

Agent output format: `{"type": "update_issue", "issue_number": 123, "operation": "append", "body": "..."}`. The `operation` field is optional (defaults to `append`).
For issue field updates, use [`set_issue_field`](#set-issue-field-set-issue-field).

### Pull Request Updates (`update-pull-request:`)

Updates PR title or body.

See the full reference: [Safe Outputs (Pull Requests) — update-pull-request](/gh-aw/reference/safe-outputs-pull-requests/#pull-request-updates-update-pull-request)

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
    allowed-repos: ["owner/repo1"]        # additional allowed repositories
    github-token: ${{ secrets.SOME_CUSTOM_TOKEN }} # optional custom token for permissions
```

Agent output includes `parent_issue_number` and `sub_issue_number`. Validation ensures both issues exist and meet label/prefix requirements before linking.

### Set Issue Type (`set-issue-type:`)

Sets or clears the type of a GitHub issue. Issue types must be configured in repository or organization settings. Pass an empty string `""` to clear the current issue type.

```yaml wrap
safe-outputs:
  set-issue-type:                          # null enables with defaults
    allowed: ["Bug", "Feature", "Task"]   # restrict allowed types (omit for any type)
    max: 5                                 # max operations (default: 5)
    target: "triggering"                   # "triggering" (default), "*", or issue number
    target-repo: "owner/repo"              # cross-repository
    allowed-repos: ["owner/repo1"]         # additional allowed repositories
    required-labels: [automated]           # only operate if item has ALL these labels
    required-title-prefix: "[bot] "        # only operate if item title starts with this prefix
    github-token: ${{ secrets.SOME_CUSTOM_TOKEN }}
```

Agent calls `set_issue_type` with `issue_type` (the type name) and optionally `issue_number`. Omitting `issue_number` targets the triggering issue.

### Set Issue Field (`set-issue-field:`)

Sets one issue field value by field name and value, without needing the broader `update-issue` tool path.

```yaml wrap
safe-outputs:
  set-issue-field:                        # null enables with defaults
    max: 5                                # max operations (default: 5)
    target: "triggering"                  # "triggering" (default), "*", or issue number
    allowed-fields: [Priority, Iteration] # restrict issue fields this workflow may set
    target-repo: "owner/repo"             # cross-repository
    allowed-repos: ["owner/repo1"]        # additional allowed repositories
    required-labels: [automated]          # only operate if item has ALL these labels
    required-title-prefix: "[bot] "       # only operate if item title starts with this prefix
    github-token: ${{ secrets.SOME_CUSTOM_TOKEN }}
```

Agent calls `set_issue_field` with `value`, and either `field_name` (preferred) or `field_node_id`. It can also pass `issue_number`; if omitted, the triggering issue is targeted.

#### `set_issue_field` tool schema

| Parameter | Type | Required | Description | Example |
|-----------|------|----------|-------------|---------|
| `value` | `string` | Yes | Field value to set. For date fields use `YYYY-MM-DD`; for single-select use an existing option label. | `"High"` |
| `field_name` | `string` | Conditional* | Field display name used for automatic discovery. | `"Priority"` |
| `field_node_id` | `string` | Conditional* | GraphQL node ID of the field, used to skip name discovery. | `"PVTF_lADO..."` |
| `issue_number` | `number \| string` | No | Issue number to update. If omitted, uses the triggering issue. | `123` |
| `repo` | `string` | No | Optional `owner/repo` override when cross-repository updates are enabled. | `"owner/repo"` |

\* Provide **at least one** of `field_name` or `field_node_id`.

```json
{
  "type": "set_issue_field",
  "issue_number": 123,
  "field_name": "Priority",
  "value": "High"
}
```

#### Issue field discovery mechanism

When `field_name` is provided, the handler discovers available issue fields for the target repository and resolves the matching field automatically.

1. Agent calls `set_issue_field` with `field_name`.
2. Handler fetches available issue fields and resolves the field by label.
3. If the field is unknown, the error includes available field names and guidance to use `field_node_id`.

```json
{
  "type": "set_issue_field",
  "field_name": "Urgency",
  "value": "P0"
}
```

Example actionable error:

```text
Issue field "Urgency" not found. Available fields: Priority, Iteration, Story Points.
Use a listed field_name or provide field_node_id to bypass discovery.
```

Retrying with explicit ID:

```json
{
  "type": "set_issue_field",
  "field_node_id": "PVTF_lADOExampleFieldId",
  "value": "P0"
}
```

#### End-to-end triage workflow example (discovery + field updates)

```yaml wrap
---
on:
  issues:
    types: [opened, reopened]

permissions:
  contents: read
  issues: write

safe-outputs:
  create-issue:
    title-prefix: "[triage] "
    labels: [triage]
    allowed-fields: [Priority, Iteration, Story Points]
  update-issue:
    target: triggering
    status:
    body:
  set-issue-field:
    target: triggering
    allowed-fields: [Priority, Iteration]
---
```

```json
[
  {
    "type": "update_issue",
    "body": "Initial triage complete. Escalating for review.",
    "operation": "append"
  },
  {
    "type": "set_issue_field",
    "field_name": "Priority",
    "value": "High"
  },
  {
    "type": "set_issue_field",
    "field_name": "Iteration",
    "value": "Sprint 42"
  }
]
```

### Project Creation (`create-project:`)

Creates new GitHub Projects V2 boards. Requires a write-capable PAT or GitHub App token ([project token authentication](/gh-aw/patterns/project-ops/#project-token-authentication)); default `GITHUB_TOKEN` lacks Projects v2 access. Supports optional view configuration to create custom project views at creation time.

Use separate tokens as shown in ProjectOps examples:
- `GH_AW_READ_PROJECT_TOKEN` for `tools.github` reads
- `GH_AW_WRITE_PROJECT_TOKEN` for `safe-outputs` project writes

```yaml wrap
safe-outputs:
  create-project:
    max: 1                              # max operations (default: 1)
    github-token: ${{ secrets.GH_AW_WRITE_PROJECT_TOKEN }}
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
>
> - **Classic PAT**: `project` scope (user projects) or `project` + `repo` scope (org projects)
> - **Fine-grained PAT**: Organization permissions → Projects: Read & Write

> [!NOTE]
> You can configure views directly during project creation using the `views` field (see above), or later using `update-project` to add custom fields and additional views. For pattern guidance, see [Monitoring with Projects](/gh-aw/experimental/monitoring-with-projects/).

### Project Board Updates (`update-project:`)

Manages GitHub Projects boards. Requires a write-capable PAT or GitHub App token ([project token authentication](/gh-aw/patterns/project-ops/#project-token-authentication)); default `GITHUB_TOKEN` lacks Projects v2 access. Update-only by default; set `create_if_missing: true` to create boards (requires appropriate token permissions).

When using `github-app`, issue-backed project item resolution also requires `issues: read` on the minted token (in addition to `organization-projects: write`). This applies to `update-project`, and also to `create-project` when `item_url` is used to resolve an issue into a project item.

```yaml wrap
safe-outputs:
  update-project:
    project: "https://github.com/orgs/myorg/projects/42"  # required: target project URL
    max: 20                         # max operations (default: 10)
    github-token: ${{ secrets.GH_AW_WRITE_PROJECT_TOKEN }}
    target-repo: "org/default-repo"         # optional: default repo for target_repo resolution
    allowed-repos: ["org/repo-a", "org/repo-b"]  # optional: additional repos for cross-repo items
    views:                          # optional: auto-create views
      - name: "Sprint Board"
        layout: board
        filter: "is:issue is:open"
      - name: "Task Tracker"
        layout: table
      - name: "Roadmap"
        layout: roadmap
```

Agent output messages **must** explicitly include the `project` field — the configured value is for documentation purposes only. Exposes outputs: `project-id`, `project-number`, `project-url`, `item-id`.

#### Cross-Repository Content Resolution

For **organization-level projects** that aggregate issues from multiple repositories, use `target_repo` in the agent output to specify which repo contains the issue or PR:

```yaml wrap
safe-outputs:
  update-project:
    github-token: ${{ secrets.GH_AW_WRITE_PROJECT_TOKEN }}
    allowed-repos: ["org/docs", "org/backend", "org/frontend"]
```

The agent can then specify `target_repo` alongside `content_number`:

```json
{
  "type": "update_project",
  "project": "https://github.com/orgs/myorg/projects/42",
  "content_type": "issue",
  "content_number": 123,
  "target_repo": "org/docs",
  "fields": { "Status": "In Progress" }
}
```

Without `target_repo`, the workflow's host repository is used to resolve `content_number`.

#### Supported Field Types

GitHub Projects V2 supports various custom field types. The following field types are automatically detected and handled:

- **`TEXT`** - Text fields (default)
- **`DATE`** - Date fields (format: `YYYY-MM-DD`)
- **`NUMBER`** - Numeric fields (story points, estimates, etc.)
- **`ITERATION`** - Sprint/iteration fields (matched by iteration title)
- **`SINGLE_SELECT`** - Dropdown/select fields (creates missing options automatically)

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
    github-token: ${{ secrets.GH_AW_WRITE_PROJECT_TOKEN }}
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

**Layout types:** `table` (list), `board` (Kanban), `roadmap` (timeline). The `filter` field accepts standard GitHub search syntax (e.g., `is:issue is:open`, `label:bug`).

Views are created automatically during workflow execution. The workflow must include at least one `update_project` operation to provide the target project URL.

### Project Status Updates (`create-project-status-update:`)

Creates status updates on GitHub Projects boards to communicate progress, findings, and trends. Status updates appear in the project's Updates tab and provide a historical record of execution. Requires a write-capable PAT or GitHub App token ([project token authentication](/gh-aw/patterns/project-ops/#project-token-authentication)); default `GITHUB_TOKEN` lacks Projects v2 access.

```yaml wrap
safe-outputs:
  create-project-status-update:
    project: "https://github.com/orgs/myorg/projects/73"  # required: target project URL
    max: 1                          # max updates per run (default: 1)
    github-token: ${{ secrets.GH_AW_WRITE_PROJECT_TOKEN }}
```

Agent output messages **must** explicitly include the `project` field. Often used by scheduled and orchestrator workflows to post run summaries.

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

**Status values:** `ON_TRACK` (on schedule), `AT_RISK` (potential issues), `OFF_TRACK` (behind schedule), `COMPLETE` (finished), `INACTIVE` (paused).

Exposes outputs: `status-update-id`, `project-id`, `status`.

### Pull Request Creation (`create-pull-request:`)

Creates PRs with code changes. Includes configurable [Protected Files](/gh-aw/reference/safe-outputs-pull-requests/#protected-files) against supply chain attacks.

See the full reference: [Safe Outputs (Pull Requests) — create-pull-request](/gh-aw/reference/safe-outputs-pull-requests/#pull-request-creation-create-pull-request)

```yaml wrap
safe-outputs:
  create-pull-request:
    title-prefix: "[ai] "
    labels: [automation]
    reviewers: [user1, copilot]
    assignees: [user1]            # assignees for fallback issues created when PR creation cannot proceed (including protected-files fallback)
    normalize-closing-keywords: true   # strip backticks around recognized issue-closing keywords in PR body text
    protected-files: fallback-to-issue  # create review issue if protected files modified, git commands (`checkout`, `branch`, `switch`, `add`, `rm`, `commit`, `merge`) are automatically enabled.
```

### Close Pull Request (`close-pull-request:`)

Closes PRs without merging.

See the full reference: [Safe Outputs (Pull Requests) — close-pull-request](/gh-aw/reference/safe-outputs-pull-requests/#close-pull-request-close-pull-request)

### PR Review Comments (`create-pull-request-review-comment:`)

Creates review comments on specific code lines in PRs.

See the full reference: [Safe Outputs (Pull Requests) — create-pull-request-review-comment](/gh-aw/reference/safe-outputs-pull-requests/#pr-review-comments-create-pull-request-review-comment)

### Reply to PR Review Comment (`reply-to-pull-request-review-comment:`)

Replies to existing review comments on pull requests.

See the full reference: [Safe Outputs (Pull Requests) — reply-to-pull-request-review-comment](/gh-aw/reference/safe-outputs-pull-requests/#reply-to-pr-review-comment-reply-to-pull-request-review-comment)

### Submit PR Review (`submit-pull-request-review:`)

Submits a consolidated pull request review with a status decision. All `create-pull-request-review-comment` outputs are automatically collected and included as inline comments in the review.

If the agent calls `submit_pull_request_review`, it can specify a review `body` and `event` (APPROVE, REQUEST_CHANGES, or COMMENT). Both fields are optional — `event` defaults to COMMENT when omitted, and `body` is only required for REQUEST_CHANGES. The agent can also submit a body-only review (e.g., APPROVE) without any inline comments.

If the agent does not call `submit_pull_request_review` at all, buffered comments are still submitted as a COMMENT review automatically.

When the workflow is not triggered by a pull request (e.g. `workflow_dispatch`), set `target` to the PR number (e.g. `${{ github.event.inputs.pr_number }}`) so the review can be submitted. Same semantics as [add-comment](#comment-creation-add-comment) `target`: `"triggering"` (default), `"*"` (use `pull_request_number` from the message), or an explicit number.

For cross-repository scenarios, use `target-repo` to specify the repository where the PR lives. This mirrors the behavior of `create-pull-request-review-comment` and `add-comment`.

```yaml wrap
safe-outputs:
  create-pull-request-review-comment:
    max: 10
  submit-pull-request-review:
    max: 1            # max reviews to submit (default: 1)
    target: "triggering"  # or "*", or e.g. ${{ github.event.inputs.pr_number }} when not in pull_request trigger
    target-repo: "owner/repo"  # cross-repository: submit review on PR in another repo
    allowed-repos: ["org/repo1", "org/repo2"]  # additional allowed repositories
    allowed-events: [COMMENT, REQUEST_CHANGES]  # include REQUEST_CHANGES when using supersede mode for blocking reviews
    supersede-older-reviews: true  # dismiss older same-workflow REQUEST_CHANGES reviews after posting a replacement review
    footer: false     # omit AI-generated footer from review body (default: true)
```

Use `allowed-events` to restrict which review event types the agent can submit. This provides infrastructure-level enforcement — for example, `allowed-events: [COMMENT, REQUEST_CHANGES]` prevents the agent from submitting APPROVE reviews regardless of what the agent attempts to output. If omitted, all event types (APPROVE, COMMENT, REQUEST_CHANGES) are allowed.

**Recommendation:** prefer `allowed-events: [COMMENT]` as the default for automated review workflows. This keeps AI feedback visible without creating a persistent merge-blocking state.

Set `supersede-older-reviews: true` only when your workflow intentionally uses `REQUEST_CHANGES` and you want newer runs to dismiss older blocking reviews from the same workflow. Superseding is best-effort and happens after the replacement review is posted.

### Resolve PR Review Thread (`resolve-pull-request-review-thread:`)

Resolves review threads on pull requests.

See the full reference: [Safe Outputs (Pull Requests) — resolve-pull-request-review-thread](/gh-aw/reference/safe-outputs-pull-requests/#resolve-pr-review-thread-resolve-pull-request-review-thread)

### Code Scanning Alerts (`create-code-scanning-alert:`)

Creates security advisories in SARIF format and submits to GitHub Code Scanning. Supports severity: error, warning, info, note.

```yaml wrap
safe-outputs:
  create-code-scanning-alert:
    max: 50  # max findings (default: unlimited)
    github-token: ${{ secrets.SOME_CUSTOM_TOKEN }} # optional custom token for permissions
```

### Autofix Code Scanning Alerts (`autofix-code-scanning-alert:`)

Creates automated fixes for code scanning alerts. Agent outputs fix suggestions that are submitted to GitHub Code Scanning.

```yaml wrap
safe-outputs:
  autofix-code-scanning-alert:
    max: 10  # max autofixes (default: 10)
    github-token: ${{ secrets.SOME_CUSTOM_TOKEN }} # optional custom token for permissions
```

### Check Run Creation (`create-check-run:`)

Creates a GitHub Check Run that surfaces agent analysis results as a first-class status check on a commit or pull request. Check Runs appear in the PR checks UI and on commits with a pass/fail status.

```yaml wrap
safe-outputs:
  create-check-run:
    name: "Security Analysis"     # check run name in the Checks UI (default: workflow name)
    target: "*"                   # "triggering" (default), "*", or explicit PR number expression
    max: 1                        # max check runs per workflow run (default: 1)
    output:                       # optional static fallbacks used when the agent omits them
      title: "Analysis complete"
      summary: "No findings to report."
    staged: true                  # optional preview mode — emits step summary instead of calling the API
```

The check run `name` is configured in frontmatter, **not** accepted as an agent parameter. When `name` equals the workflow name it is auto-suffixed with `(Result)` to avoid being collapsed into the workflow's own check suite entry in compact UI views.

The agent calls `create_check_run` with the required fields:

```json
{
  "type": "create_check_run",
  "conclusion": "failure",
  "title": "3 issues found",
  "summary": "### Findings\n- Issue A\n- Issue B\n- Issue C"
}
```

`conclusion` must be one of: `success`, `failure`, `neutral`, `cancelled`, `skipped`, `timed_out`, `action_required`. `title` (max 256 characters) and `summary` (max 65535 characters) are required; an optional `text` field provides additional detail content.

#### Pull Request Targeting

The `target` field controls which pull request the check run is attached to:

- **omitted** — the handler resolves the head SHA from the triggering event payload (`pull_request.head.sha`, falling back to `GITHUB_SHA` / `context.sha`). No Pulls API call is made.
- **`triggering`** — resolves the PR number from the event context, then fetches the current PR head SHA via `GET /repos/{owner}/{repo}/pulls/{pull_number}`. The API call is intentional so the check run always references the most recent head even if the PR was force-pushed between the triggering event and handler execution.
- **`"*"`** — the agent must include `pull_request_number` (or any of the aliases `pr_number`, `pr`, `pull_number`) in each `create_check_run` call. The handler resolves the head SHA via the Pulls API.
- **explicit PR number expression** (e.g. `"${{ github.event.inputs.pr }}"`) — resolves to the specified PR and fetches the head SHA via the Pulls API.

When `target` is configured, the compiled workflow automatically adds the `pull-requests: read` permission required for PR head SHA resolution.

#### Required Permissions

| Configuration | Permissions |
|---------------|-------------|
| `target` omitted | `contents: read`, `checks: write` |
| `target` configured | `contents: read`, `checks: write`, `pull-requests: read` |

A GitHub App credential block (`github-app:`) can be supplied to mint a short-lived installation token scoped to `checks:write` for this handler only.

### Push to PR Branch (`push-to-pull-request-branch:`)

Pushes changes to a PR's branch. Includes configurable [Protected Files](/gh-aw/reference/safe-outputs-pull-requests/#protected-files) against supply chain attacks.

See the full reference: [Safe Outputs (Pull Requests) — push-to-pull-request-branch](/gh-aw/reference/safe-outputs-pull-requests/#push-to-pr-branch-push-to-pull-request-branch)

```yaml wrap
safe-outputs:
  push-to-pull-request-branch:
    target: "*"                 # "triggering" (default), "*", or number
    required-title-prefix: "[bot] "      # require title prefix
    required-labels: [automated]         # require all labels
    signed-commits: false  # optional: use git push directly when signed commits are not required
    protected-files: fallback-to-issue  # create review issue if protected files modified
```

When `push-to-pull-request-branch` is configured, git commands (`checkout`, `branch`, `switch`, `add`, `rm`, `commit`, `merge`) are automatically enabled.

For multi-checkout workflows, if one checkout is marked `current: true` and the PR tool targets that repository, patch generation for both `create-pull-request` and `push-to-pull-request-branch` uses that checkout directory.

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

### Artifact Uploads (`upload-artifact:`)

Uploads files as run-scoped GitHub Actions artifacts. Artifacts expire automatically after the configured retention period and put less pressure on git storage than `upload-asset`. Recommended for images, reports, and temporary output files.

```yaml wrap
safe-outputs:
  upload-artifact:                         # null enables with defaults
    max-uploads: 1                         # max upload operations per run (default: 1)
    retention-days: 7                      # artifact retention in days
    skip-archive: false                    # upload without zip archiving (single-file only)
    max-size-bytes: 104857600             # max upload size in bytes (default: 100 MB)
    allowed-paths:                         # restrict paths agent may upload
      - "output/**"
    github-token: ${{ secrets.SOME_CUSTOM_TOKEN }}
```

Agent calls `upload_artifact` with a `path` (file or directory) or `filters` (glob-based file selection). Artifacts are available via `gh run download` during the workflow run retention period.

### Asset Uploads (`upload-asset:`)

:::caution[Prefer `upload-artifact` with `skip-archive`]
For uploading images, charts, and screenshots, prefer using `upload-artifact` with `skip-archive: true` instead (see the [shared upload-artifact configuration](https://github.com/github/gh-aw/blob/main/.github/workflows/shared/safe-output-upload-artifact.md)). It puts less pressure on the git storage system and automatically destroys the image once the artifact expires. Use `upload-asset` only when you need persistent, publicly addressable URLs that survive artifact expiration.
:::

Uploads files (screenshots, charts, reports) to orphaned git branch with predictable URLs: `https://github.com/{owner}/{repo}/blob/{branch}/{filename}?raw=true`. Agent registers files via `upload_asset` tool; separate job with `contents: write` commits them.

```yaml wrap
safe-outputs:
  upload-asset:
    branch: "assets/my-workflow"     # default: "assets/${{ github.workflow }}"
    max-size: 5120                   # KB (default: 10240 = 10MB)
    allowed-exts: [.png, .jpg, .svg] # default: [.png, .jpg, .jpeg]
    max: 20                          # default: 10
    github-token: ${{ secrets.SOME_CUSTOM_TOKEN }} # optional custom token for permissions
```

**Branch Requirements**: New branches require `assets/` prefix for security. Existing branches allow any name. Create custom branches manually:

```bash
git checkout --orphan my-custom-branch && git rm -rf . && git commit --allow-empty -m "Initialize" && git push origin my-custom-branch
```

**Security**: File path validation (workspace/`/tmp` only), extension allowlist, size limits, SHA-256 verification, orphaned branch isolation, minimal permissions.

**Outputs**: `published_count`, `branch_name`. **Limits**: Same-repo only, max 50MB/file, 100 assets/run.

### Azure DevOps Work Items

Azure DevOps work-item safe outputs use the same public tool names as [`ado-aw`](https://githubnext.github.io/ado-aw/reference/safe-outputs/). The agent remains read-only; the safe-output job performs trusted Azure DevOps REST requests.

Provide the organization, project, and credential only to the safe-output job:

> These safe outputs are experimental. Compiling a workflow emits an experimental feature warning for each configured Azure DevOps work-item output.

```yaml wrap
safe-outputs:
  env:
    AZURE_DEVOPS_ORG_URL: ${{ vars.AZURE_DEVOPS_ORG_URL }}
    SYSTEM_TEAMPROJECT: ${{ vars.AZURE_DEVOPS_PROJECT }}
    AZURE_DEVOPS_EXT_PAT: ${{ secrets.AZURE_DEVOPS_EXT_PAT }}
  ado-create-work-item:
    work-item-type: Task
    area-path: MyProject\Platform
    allowed-tags: [agent-*]
  ado-update-work-item:
    target: MyProject\Platform
    title: true
    status: true
  ado-comment-on-work-item:
    target: MyProject\Platform
  ado-assign-work-item:
    target: "*"
    allowed: [owner@example.com]
  ado-link-work-items:
    target: MyProject\Platform
    allowed-link-types: [parent, child, related]
  ado-upload-workitem-attachment:
    target: MyProject\Platform
    allowed-extensions: [.txt, .log, .pdf]
    max-file-size: 5242880
```

Authentication uses `SYSTEM_ACCESSTOKEN` when present, otherwise `AZURE_DEVOPS_EXT_PAT`. `AZURE_DEVOPS_ORG_URL` must use `https://dev.azure.com/{organization}` or `https://{organization}.visualstudio.com`; redirects and embedded credentials are rejected.

`ado_create_work_item` returns a run-scoped `#aw_...` temporary ID. The other work-item tools accept that ID, and same-run IDs bypass their consuming `target` policy because creation was already scoped by trusted configuration. Numeric IDs are checked against `target`, which accepts `"*"`, a single ID, a list of IDs, or an area-path prefix.

For `ado-update-work-item`, each mutable field must be explicitly enabled. Assignment always rejects the reserved `Agency` and `GitHub Copilot` identities. Attachments must be regular workspace files and are checked for traversal, symbolic links, size, extension, and Azure Pipelines command sequences before upload.

### No-Op Logging (`noop:`)

:::danger[Required when no action is taken]
**`noop` MUST be called when no GitHub action is needed.** This is the #1 runtime failure mode for safe-output workflows. If the agent finishes without calling any safe-output tool, the workflow fails silently with no output. Always call `noop` when your analysis concludes that no action is required.
:::

Enabled by default. Allows agents to produce completion messages when no actions are needed, preventing silent workflow completion.

```yaml wrap
safe-outputs:
  create-issue:     # noop enabled automatically
  noop: false       # explicitly disable
```

**When to call `noop`**: Any time no GitHub action (issue, comment, PR, label, etc.) is needed — e.g., no issues found, no changes detected, or repository already in desired state. Do NOT call `noop` if any other safe-output action was taken.

Agent output: `{"noop": {"message": "No action needed: analysis complete - no issues found"}}`. Messages appear in the workflow conclusion comment or step summary.

**Always include explicit `noop` instructions in your workflow prompts:**

```markdown
If no action is needed, you MUST call the `noop` tool with a message explaining why:
{"noop": {"message": "No action needed: [brief explanation]"}}
```

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
    github-token: ${{ secrets.SOME_CUSTOM_TOKEN }} # optional custom token for permissions
```

When `create-issue: true`, the agent creates or updates GitHub issues documenting what data is needed and why, possible alternatives, and context for how the data would be used. This rewards honest AI behavior and helps teams improve data accessibility for future agent runs.

### Discussion Creation (`create-discussion:`)

Creates discussions with optional `category` (slug, name, or ID; defaults to first available). `expires` field auto-closes after period (integers, `2h`, `7d`, `2w`, `1m`, `1y`, or `false` to disable; hours < 24 treated as 1 day) as "OUTDATED" with comment. Generates maintenance workflow with dynamic frequency based on shortest expiration time (see Auto-Expiration section above).

**Category Naming Standard**: Use lowercase, plural category names (e.g., `audits`, `general`, `reports`) for consistency and better searchability. GitHub Discussion category IDs (starting with `DIC_`) are also supported.

```yaml wrap
safe-outputs:
  create-discussion:
    title-prefix: "[ai] "        # prefix for titles
    category: "announcements"    # category slug, name, or ID (use lowercase)
    min-body-length: 200         # optional minimum body length guard (fails safe-outputs job if shorter)
    expires: 3                   # auto-close after 3 days (or false to disable)
    max: 3                       # max discussions (default: 1)
    target-repo: "owner/repo"    # cross-repository
    allowed-repos: ["org/repo1", "org/repo2"]  # additional allowed repositories
    fallback-to-issue: true      # fallback to issue creation on permission errors (default: true)
    github-token: ${{ secrets.SOME_CUSTOM_TOKEN }} # optional custom token for permissions
```

Use `min-body-length` when you want a hard floor for report quality (for example, to prevent accidental placeholder bodies like `test` from being posted).

#### Fallback to Issue Creation

The `fallback-to-issue` field (default: `true`) automatically falls back to creating an issue when discussion creation fails (e.g., discussions disabled, insufficient `discussions: write` permissions, or org policy restrictions). The issue body notes it was intended to be a discussion. Set to `false` to fail instead of falling back.

### Close Discussion (`close-discussion:`)

Closes GitHub discussions with optional comment and resolution reason. Filters by category, labels, and title prefix control which discussions can be closed.

```yaml wrap
safe-outputs:
  close-discussion:
    target: "triggering"         # "triggering" (default), "*", or number
    required-category: "Ideas"   # only close in category
    required-labels: [resolved]  # only close if ALL these labels are present
    required-title-prefix: "[ai]" # only close matching prefix
    max: 1                       # max closures (default: 1)
    target-repo: "owner/repo"    # cross-repository
    github-token: ${{ secrets.SOME_CUSTOM_TOKEN }} # optional custom token for permissions
    allow-body: false          # prevent closing comment (drop body if provided)
```

**Target**: `"triggering"` (requires discussion event), `"*"` (any discussion), or number (specific discussion).

**Resolution Reasons**: `RESOLVED`, `DUPLICATE`, `OUTDATED`, `ANSWERED`.

**`allow-body: false`**: When set, any `body` field the agent provides is dropped (a warning is logged) and the discussion is closed without posting a comment. Use this when you want to guarantee a clean close with no duplicate comment — for example, when a prior `add-comment` step already posted the summary.

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
    github-token: ${{ secrets.SOME_CUSTOM_TOKEN }} # optional custom token for permissions
```

**Field Enablement**: Include `title:`, `body:`, or `labels:` keys to enable updates for those fields. Without these keys, the field cannot be updated. Setting `allowed-labels` implicitly enables label updates.

**Target**: `"triggering"` (requires discussion event), `"*"` (any discussion), or number (specific discussion).

When using `target: "*"`, the agent must provide `discussion_number` in the output to identify which discussion to update.

### Workflow Dispatch (`dispatch-workflow:`)

Triggers other workflows in the same repository using GitHub's `workflow_dispatch` event. This enables orchestration patterns, such as orchestrator workflows that coordinate multiple worker workflows.

> [!NOTE]
> When installing a workflow with `gh aw add`, workflows listed in `dispatch-workflow` are automatically fetched and added to the target repository alongside the main workflow.

**Shorthand Syntax:**

```yaml wrap
safe-outputs:
  dispatch-workflow: [worker-workflow, scanner-workflow]
```

#### Configuration

- **`workflows`** (required) - List of workflow names (without `.md` extension) that the agent is allowed to dispatch. For same-repo dispatch, each workflow must exist locally and support the `workflow_dispatch` trigger.
- **`max`** (optional) - Maximum number of workflow dispatches allowed (default: 1, maximum: 50). This prevents excessive workflow triggering.
- **`target-repo`** (optional) - Target repository in `owner/repo` format for cross-repository dispatch.
- **`allowed-repos`** (optional) - Allowlist of cross-repository dispatch targets. Required when `target-repo` points to a different repository. Supports repository slugs and wildcards such as `org/*`, or a GitHub Actions expression string (e.g. `"${{ inputs['allowed-repos'] }}"`) for dynamic allowlists.
- **`target-ref`** (optional) - Git ref to dispatch on. In `workflow_call` relay scenarios, the compiler injects this automatically so the dispatch uses the target repository's branch or tag instead of the caller's `GITHUB_REF`.
- **`allowed-refs`** (optional) - List of ref glob patterns that the agent is allowed to supply via `message.ref` at runtime. Supports arrays and GitHub Actions expressions resolving to a comma-separated list (e.g. `"${{ inputs['allowed-refs'] }}"`). When omitted, the repository default branch is allowed. Branch shorthand (`feature/*`) is automatically expanded to `refs/heads/feature/*`; `tags/v*` is expanded to `refs/tags/v*`; full `refs/…` patterns are used as-is.

#### Validation Rules

At compile time, for same-repo dispatch (`target-repo` unset or `${{ github.repository }}`), the compiler validates that each workflow exists (`.md`, `.lock.yml`, or `.yml`), declares `workflow_dispatch` in its `on:` section, does not self-reference, and resolves the correct file extension. For cross-repo dispatch (`target-repo` set to another repository), local workflow file validation is skipped and GitHub enforces existence at dispatch time.

At runtime, when exactly one workflow is configured, the agent may omit `workflow_name` in the emitted `dispatch_workflow` item and gh-aw infers the only allowed target. When two or more workflows are configured, `workflow_name` remains required so the dispatch target stays explicit.

```json
{ "type": "dispatch_workflow", "inputs": { "message": "hello" } }
```

With `dispatch-workflow: [workflow-handler]`, that item is normalized to target `workflow-handler` automatically before validation.

#### Per-call Ref Override

When an agent needs to dispatch CI against a branch it just created, it can supply `ref` directly in the output payload:

```json
{
  "type": "dispatch_workflow",
  "workflow_name": "ci",
  "ref": "feature/my-branch",
  "inputs": { "reason": "validate new branch" }
}
```

The repository default branch is allowed when `allowed-refs` is omitted. The ref is normalized before matching: bare branch names are expanded to `refs/heads/<name>`, `tags/…` to `refs/tags/…`, and full `refs/…` values are used as-is. Dispatches with a `message.ref` that does not match any configured pattern are rejected at runtime.

Ref resolution priority:
1. `message.ref` (highest — per-call override, restricted by configured or implicit `allowed-refs`)
2. `target-ref` from configuration
3. `GITHUB_HEAD_REF` (PR head branch)
4. `GITHUB_REF` or `context.ref` (push/default branch)
5. target repository default branch

#### Defining Workflow Inputs

Define `workflow_dispatch` inputs in the target workflow so the agent can provide values when dispatching:

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
      dry_run:
        type: boolean
        default: false
---
```

#### Rate Limiting

To respect GitHub API rate limits, the handler automatically enforces a 5-second delay between consecutive workflow dispatches. The first dispatch has no delay.

**Security**: Same-repo only; only allowlisted workflows can be dispatched; compile-time validation catches errors early.

### Workflow Call (`call-workflow:`)

Calls reusable workflows (`workflow_call`) via compile-time fan-out—no GitHub API call at runtime. The compiler reads each worker's `workflow_call.inputs`, generates a typed MCP tool per worker, and emits a conditional `uses:` job for each. At runtime, only the worker whose name the agent selected runs.

Unlike `dispatch-workflow` (which fires a `workflow_dispatch` event and loses the original actor context), `call-workflow` preserves `github.actor` and billing attribution because the worker job is part of the same workflow run.

> [!NOTE]
> When installing a workflow with `gh aw add`, workflows listed in `call-workflow` are automatically fetched and added to the target repository alongside the main workflow.

**Shorthand Syntax:**

```yaml wrap
safe-outputs:
  call-workflow: [spring-boot-bugfix, frontend-dep-upgrade]
```

**Full Syntax:**

```yaml wrap
safe-outputs:
  call-workflow:
    workflows:
      - spring-boot-bugfix
      - frontend-dep-upgrade
    max: 1
```

#### Configuration

- **`workflows`** (required) - List of workflow names (without `.md` extension) that the agent is allowed to call. Each workflow must exist in the same repository and declare `workflow_call` as a trigger.
- **`max`** (optional) - Maximum number of times the agent may invoke the tool per run (default: 1, maximum: 50). Since a single `call_workflow_name` step output is produced, only the last selected worker executes regardless of `max`; in practice, leave this at 1.

#### Worker Inputs

All agent arguments are serialized into a `payload` JSON string passed via `call_workflow_payload`. Workers always receive this `payload` input. To use typed inputs directly (without parsing JSON), declare additional `workflow_call.inputs` beyond `payload` — the compiler auto-derives `fromJSON(...).<inputName>` forwarding for each, so workers can reference `${{ inputs.<name> }}` directly:

```yaml title="deploy.md (worker)"
on:
  workflow_call:
    inputs:
      payload:
        type: string
        required: false
      environment:
        description: Target environment
        type: choice
        options: [dev, staging, production]
        required: true
      dry_run:
        type: boolean
        required: false
```

Supported input types: `string`, `number`, `boolean`, `choice` (rendered as an enum).

#### Validation Rules

At compile time, the compiler validates:

1. **Workflow existence** - Each workflow must exist as a `.lock.yml`, `.yml`, or `.md` file.
2. **`workflow_call` trigger** - Each worker must declare `workflow_call` in its `on:` section.
3. **No self-reference** - A gateway cannot call itself.
4. **File resolution** - The compiler resolves the correct extension and embeds it in the generated job.

#### Comparing `call-workflow` and `dispatch-workflow`

| | `call-workflow` | `dispatch-workflow` |
|---|---|---|
| Mechanism | Compile-time `uses:` job | Runtime `workflow_dispatch` API |
| API calls | None | One per dispatch |
| `github.actor` | Preserved | Replaced by triggering actor |
| Billing | Attributed to triggering run | Attributed to dispatched run |
| Cross-repository | No | No |
| Worker trigger | `workflow_call` | `workflow_dispatch` |

Use `call-workflow` for deterministic fan-out where actor attribution and zero API overhead matter. Use `dispatch-workflow` when workers need to run asynchronously or outlive the parent run.

**Security**: Same-repo only; only allowlisted workflows can be called; compile-time validation catches misconfiguration early.

### Repository Dispatch (`dispatch-repository`)

> [!CAUTION]
> This is an experimental feature. Compiling a workflow with `dispatch-repository` emits a warning: `Using experimental feature: dispatch-repository`. The API may change in future releases.

Triggers [`repository_dispatch`](https://docs.github.com/en/actions/writing-workflows/choosing-when-your-workflow-runs/events-that-trigger-workflows#repository_dispatch) events in external repositories. Unlike `dispatch-workflow` (same-repo only), `dispatch-repository` is designed for cross-repository orchestration.

Each key under `dispatch-repository:` defines a named tool exposed to the agent:

```yaml wrap
safe-outputs:
  dispatch-repository:
    trigger_ci:
      description: Trigger CI in another repository
      workflow: ci.yml
      event_type: ci_trigger
      repository: ${{ inputs.target_repo }}   # GitHub Actions expressions supported
      inputs:
        environment:
          type: choice
          options: [staging, production]
          default: staging
      max: 1
    notify_service:
      workflow: notify.yml
      event_type: notify_event
      allowed_repositories:
        - org/service-repo
        - ${{ vars.DYNAMIC_REPO }}             # Expressions bypass slug format validation
      inputs:
        message:
          type: string
```

#### Configuration Fields (per tool)

- **`workflow`** (required) — Identifier forwarded in `client_payload.workflow` so the receiving workflow can route by job type.
- **`event_type`** (required) — The `event_type` sent with the `repository_dispatch` event.
- **`repository`** (required, unless `allowed_repositories` is set) — Static `owner/repo` slug or a GitHub Actions expression (`${{ ... }}`). Expressions are passed through without format validation.
- **`allowed_repositories`** (required, unless `repository` is set) — List of allowed `owner/repo` slugs or expressions. The agent selects the target from this list at runtime.
- **`inputs`** (optional) — Structured input schema forwarded in `client_payload`. Supports `type: string`, `type: choice` (with `options`), and `default` values.
- **`description`** (optional) — Human-readable description of the tool shown to the agent.
- **`max`** (optional) — Maximum number of dispatches allowed per run (default: 1).

#### Security

- **Cross-repo allowlist** — At runtime the handler validates the target repository against the configured `repository` or `allowed_repositories` before calling the API (SEC-005).
- **Staged mode** — Supports `staged: true` for preview without dispatching.

### Agent Session Creation (`create-agent-session:`)

Creates Copilot coding agent sessions from workflow output. Allows workflows to spawn new agent sessions for follow-up work.

```yaml wrap
safe-outputs:
  create-agent-session:
    base: "main"                 # base branch for agent session PR
    max: 1                       # max sessions (default: 1, maximum: 10)
    target-repo: "owner/repo"    # cross-repository
    allowed-repos: ["org/repo1", "org/repo2"]  # additional allowed repositories
    github-token: ${{ secrets.SOME_CUSTOM_TOKEN }} # optional custom token for permissions
```

See **[Copilot Cloud Agent](/gh-aw/reference/copilot-cloud-agent/#create-agent-session)** for full details and authentication setup.

### Assign to Agent (`assign-to-agent:`)

Programmatically assigns GitHub Copilot coding agent to **existing** issues or pull requests through workflow automation. This safe output automates the [standard GitHub workflow for assigning issues to Copilot](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/coding-agent/create-a-pr#assigning-an-issue-to-copilot).

```yaml wrap
safe-outputs:
  assign-to-agent:
    name: "copilot"            # default agent (default: "copilot")
    model: "claude-sonnet-5"   # default AI model (default: "auto")
    custom-agent: "agent-id"   # default custom agent ID (optional)
    custom-instructions: "..."  # default custom instructions (optional)
    allowed: [copilot]         # restrict to specific agents (optional)
    max: 1                     # max assignments (default: 1)
    target: "triggering"       # "triggering" (default), "*", or number
    target-repo: "owner/repo"  # where the issue lives (cross-repository)
    pull-request-repo: "owner/repo"      # where the PR should be created (may differ from issue repo)
    allowed-pull-request-repos: [owner/repo1, owner/repo2]  # additional allowed PR repositories
    base-branch: "develop"     # target branch for PR (default: target repo's default branch)
    github-token: ${{ secrets.SOME_CUSTOM_TOKEN }} # optional custom token for permissions
```

See **[Copilot Cloud Agent](/gh-aw/reference/copilot-cloud-agent/#assign-to-agent)** for complete configuration options and authorization setup.

If you're creating new issues and want to assign an agent immediately, use `assignees: copilot` in your [`create-issue`](#issue-creation-create-issue) configuration instead.

### Assign to User (`assign-to-user:`)

Assigns users to issues. Restrict with `allowed` list. Target: `"triggering"` (issue event), `"*"` (any), or number. Supports single or multiple assignees.

```yaml wrap
safe-outputs:
  assign-to-user:
    allowed: [user1, user2]    # restrict to specific users
    max: 3                     # max assignments (default: 1)
    target: "*"                # "triggering" (default), "*", or number
    target-repo: "owner/repo"  # cross-repository
    allowed-repos: ["org/repo1", "org/repo2"]  # additional allowed repositories
    unassign-first: true       # unassign all current assignees before assigning (default: false)
    required-labels: [automated]     # only assign if item has ALL these labels
    required-title-prefix: "[bot] "  # only assign if item title starts with this prefix
    github-token: ${{ secrets.SOME_CUSTOM_TOKEN }} # optional custom token for permissions
```

### Unassign from User (`unassign-from-user:`)

Removes user assignments from issues or pull requests. Restrict with `allowed` list to control which users can be unassigned. Target: `"triggering"` (issue/PR event), `"*"` (any), or number.

```yaml wrap
safe-outputs:
  unassign-from-user:
    allowed: [user1, user2]    # restrict to specific users
    max: 1                     # max unassignments (default: 1)
    target: "*"                # "triggering" (default), "*", or number
    target-repo: "owner/repo"  # cross-repository
    allowed-repos: ["org/repo1", "org/repo2"]  # additional allowed repositories
    required-labels: [automated]     # only unassign if item has ALL these labels
    required-title-prefix: "[bot] "  # only unassign if item title starts with this prefix
    github-token: ${{ secrets.SOME_CUSTOM_TOKEN }} # optional custom token for permissions
```

## Cross-Repository Operations

Most safe outputs support cross-repository operations:

- **`target-repo`**: Set a fixed target repository (`owner/repo` format), or use `"*"` as a wildcard to let the agent supply any repository at runtime.
- **`allowed-repos`**: Allow the agent to dynamically choose from an allowlist of repositories (supports glob patterns, e.g. `org/*`).

Using `target-repo: "*"` enables fully dynamic routing — the agent provides the `repo` field in each tool call. Note that `create-pull-request-review-comment`, `reply-to-pull-request-review-comment`, `submit-pull-request-review`, `create-agent-session`, and `manage-project-items` do not support the wildcard; use an explicit repository or `allowed-repos` for those types.

See [Cross-Repository Operations](/gh-aw/reference/cross-repository/) for comprehensive documentation.

## Global Configuration Options

### Workflow Call Outputs (`workflow_call`)

When a workflow uses `on: workflow_call` (or includes `workflow_call` in its triggers) and configures safe outputs, the compiler automatically injects `on.workflow_call.outputs` exposing the results of each configured safe output type. This makes gh-aw workflows composable building blocks in larger automation pipelines.

The following named outputs are exposed for each configured safe output type:

| Safe Output Type | Output Names |
|---|---|
| `create-issue` | `created_issue_number`, `created_issue_url` |
| `create-pull-request` | `created_pr_number`, `created_pr_url` |
| `add-comment` | `comment_id`, `comment_url` |
| `push-to-pull-request-branch` | `push_commit_sha`, `push_commit_url` |

These outputs are automatically available to calling workflows without any additional frontmatter configuration. User-declared `outputs` in the frontmatter are preserved and take precedence over the auto-injected values.

**Example — calling workflow using safe-output results:**

```yaml wrap
jobs:
  run-agent:
    uses: ./.github/workflows/my-agent.lock.yml
  follow-up:
    needs: run-agent
    steps:
      - run: echo "Created issue ${{ needs.run-agent.outputs.created_issue_number }}"
```

### Failure Issue Reporting (`report-failure-as-issue:`)

Controls whether workflow failures are reported as GitHub issues (default: `true`).

#### Simple Boolean (Opt-Out All Failures)

Set to `false` to suppress automatic failure issue creation for a specific workflow:

```yaml wrap
safe-outputs:
  report-failure-as-issue: false
  create-issue:
```

This mirrors the `noop.report-as-issue` pattern. Use this to silence noisy failure reports for workflows where failures are expected or handled externally.

#### Category Filtering (Selective Reporting)

Filter which failure types trigger issue creation by specifying a list of categories. Categories can be included (default) or excluded (using `!` prefix).

##### Include Only Specific Categories

Only failures matching the specified categories will create issues:

```yaml wrap
safe-outputs:
  report-failure-as-issue:
    - agent_failure           # Report only genuine agent-side failures
    - missing_safe_outputs    # Report missing outputs
  create-issue:
```

##### Exclude Specific Categories

Report all failures except the specified categories (use `!` prefix):

```yaml wrap
safe-outputs:
  report-failure-as-issue:
    - "!inference_access_error"        # Exclude AI server transient errors
    - "!ai_credits_rate_limit_error"   # Exclude AI rate limits
    - "!report_incomplete"             # Exclude infrastructure failures
  create-issue:
```

##### Mixed Include and Exclude

Combine both syntaxes - categories must match included AND not match excluded:

```yaml wrap
safe-outputs:
  report-failure-as-issue:
    - agent_failure                    # Include agent failures
    - missing_safe_outputs             # Include missing outputs
    - "!unknown_model_ai_credits"      # But exclude unknown model AI credits
  create-issue:
```

**Common failure categories:**

| Category | Description |
|---|---|
| `agent_failure` | Agent crashed or returned a non-zero exit code (excluding timeouts) |
| `timed_out` | Agent execution exceeded the timeout limit |
| `missing_safe_outputs` | Agent succeeded but produced no safe outputs |
| `report_incomplete` | Agent reported that the task could not be completed (infrastructure or tool failures) |
| `missing_tool` | Required functionality is not available |
| `missing_data` | Required data is not accessible |
| `inference_access_error` | AI inference endpoint authentication or access failures |
| `mcp_policy_error` | MCP server policy violations |
| `ai_credits_rate_limit_error` | AI credits rate limit exceeded |
| `max_ai_credits_exceeded` | Maximum AI credits budget exceeded |
| `cache_miss_misconfiguration` | Cache configuration errors |
| `code_push_failures` | Failures pushing code to branches |
| `assignment_errors` | Failures assigning issues or reviewers |

**Use case: Suppress transient infrastructure failures**

For scheduled workflows that frequently encounter transient infrastructure failures (Docker registry timeouts, AI server 5xx errors, firewall issues), you can either:

Include only actionable categories:
```yaml wrap
safe-outputs:
  report-failure-as-issue:
    - agent_failure
    - missing_safe_outputs
    - missing_tool
    - missing_data
  create-issue:
```

Or exclude transient categories:
```yaml wrap
safe-outputs:
  report-failure-as-issue:
    - "!report_incomplete"           # Exclude infrastructure errors
    - "!inference_access_error"      # Exclude AI server flake
    - "!ai_credits_rate_limit_error" # Exclude rate limits
  create-issue:
```

Both approaches prevent noise while preserving actionable signals, but exclusion syntax is more concise when most categories should be reported.

### Failure Issue Repository (`failure-issue-repo:`)

Redirects failure tracking issues to a different repository. Useful when the current repository has issues disabled (e.g. `github/docs-internal`).

```yaml wrap
safe-outputs:
  failure-issue-repo: github/docs-engineering
  create-issue:
```

The value must be in `owner/repo` format. The `GITHUB_TOKEN` used must have permission to create issues in the target repository. When not set, failure issues are created in the current repository.

### Group Reports (`group-reports:`)

Controls whether failed workflow runs are grouped under a parent "[aw] Failed runs" issue. This is opt-in and defaults to `false`.

```yaml wrap
safe-outputs:
  create-issue:
  group-reports: true   # Enable parent issue grouping for failed runs (default: false)
```

When enabled, individual failed run reports are linked as sub-issues under a shared parent issue, making it easier to track recurring failures across workflow runs. When disabled (the default), each failure is reported independently.

### Custom GitHub Token (`github-token:`)

Override for all safe outputs, or per safe output:

```yaml wrap
safe-outputs:
  github-token: ${{ secrets.CUSTOM_PAT }}  # global
  create-issue:
  create-pull-request:
    github-token: ${{ secrets.PR_PAT }}    # per-output
```

`github-token` accepts these GitHub Actions expression forms:

- `secrets.NAME`
- `needs.<job>.outputs.<name>`
- `steps.<id>.outputs.<name>`

The `steps.*.outputs.*` form is useful when a short-lived token is minted inside the job that uses it, for example with a keyless OIDC token-minting action. Step outputs are only readable inside the job that produced them, so the minting step must be injected into **every** job that consumes the token: the `agent` job (top-level `pre-steps:`), the `safe_outputs` job and the `conclusion` job (`jobs.<job>.pre-steps:` or `jobs.<job>.setup-steps:`).

`pre-steps:` run before the job's checkout, git-credential and token-consuming steps, so the minted token is available everywhere it is needed. `safe-outputs.steps:` is not a valid place to mint such a token because it runs *after* the `safe_outputs` job checkout.

```yaml wrap
permissions:
  contents: read
  id-token: write

pre-steps:                        # agent job
  - name: Mint token
    id: mint_token
    uses: octo-sts/action@v1.1.1
    with:
      scope: ${{ github.repository }}
      identity: my-policy

safe-outputs:
  github-token: ${{ steps.mint_token.outputs.token }}
  push-to-pull-request-branch:

jobs:
  safe_outputs:
    permissions:
      id-token: write
    pre-steps:
      - name: Mint token
        id: mint_token
        uses: octo-sts/action@v1.1.1
        with:
          scope: ${{ github.repository }}
          identity: my-policy
  conclusion:
    permissions:
      id-token: write
    pre-steps:
      - name: Mint token
        id: mint_token
        uses: octo-sts/action@v1.1.1
        with:
          scope: ${{ github.repository }}
          identity: my-policy
```

The compiler fails compilation when a job consumes `${{ steps.<id>.outputs.* }}` but never declares a step with that id, or declares it after the first consumer, rather than emitting a lock file with an unresolvable reference.

### Using a GitHub App for Authentication (`github-app:`)

Use GitHub App tokens for enhanced security: on-demand token minting, automatic revocation, fine-grained permissions, and better attribution.

See [Using a GitHub App for Authentication](/gh-aw/reference/auth/#using-a-github-app-for-authentication).

### Environment Protection (`environment:`)

Specifies the deployment environment for all compiler-generated safe-output jobs (`safe_outputs`, `conclusion`, `pre_activation`, custom safe-jobs). This makes environment-scoped secrets accessible in those jobs — for example, GitHub App credentials stored as environment secrets.

The top-level `environment:` field is automatically propagated to all safe-output jobs. Use `safe-outputs.environment:` to override this independently:

```yaml wrap
safe-outputs:
  environment: dev   # overrides top-level environment for safe-output jobs only
  github-app:
    client-id: ${{ secrets.WORKFLOW_APP_ID }}
    private-key: ${{ secrets.WORKFLOW_APP_PRIVATE_KEY }}
```

Accepts a plain string or an object with `name` and optional `url`, consistent with the top-level `environment:` syntax.

### Safe Outputs Dependencies (`needs:`)

Extend the consolidated `safe_outputs` job dependencies with custom workflow jobs (for example, credential fetchers). `safe-outputs.needs` is merged with built-in dependencies (`agent`, `activation`, optional `detection`, optional `unlock`) and deduplicated.

```yaml wrap
jobs:
  secrets_fetcher:
    runs-on: ubuntu-latest
    outputs:
      app_id: ${{ steps.fetch.outputs.app_id }}
      app_private_key: ${{ steps.fetch.outputs.app_private_key }}
    steps:
      - id: fetch
        run: |
          echo "app_id=123" >> "$GITHUB_OUTPUT"
          echo "app_private_key=***" >> "$GITHUB_OUTPUT"

safe-outputs:
  needs: [secrets_fetcher]
  github-app:
    app-id: ${{ needs.secrets_fetcher.outputs.app_id }}
    private-key: ${{ needs.secrets_fetcher.outputs.app_private_key }}
```

Use the single `safe-outputs.needs` field for all explicit custom dependencies.
The field is schema-defined in `pkg/parser/schemas/main_workflow_schema.json`; update local editor schema integrations when upgrading gh-aw schema versions.

Validation rules:

- Values must reference workflow custom jobs from top-level `jobs:`
- Built-in jobs are rejected (`agent`, `activation`, `pre_activation`/`pre-activation`, `conclusion`, `safe_outputs`, `detection`, `unlock`, `push_repo_memory`, `update_cache_memory`)
- Unknown jobs fail compilation with an actionable error

### Text Sanitization (`allowed-domains:`, `allowed-github-references:`)

The text output by AI agents is automatically sanitized to prevent injection of malicious content and ensure safe rendering on GitHub. The auto-sanitization applied is: XML escaped, HTTPS only, domain allowlist (GitHub by default), 0.5MB/65k line limits, control char stripping.

HTML/XML comments (`<!-- ... -->`) are removed from sanitized body fields.
If you need a machine-readable channel that survives sanitization, configure `safe-outputs.data` in frontmatter:

```yaml wrap
safe-outputs:
  data: false   # default; reject output `data`
  # data: true  # allow any object in output `data`
  # data:       # enforce inline schema for output `data`
  #   verdict: string
  #   score: number
  # data: ${{ fromJSON(needs.schema.outputs.data_schema) }} # runtime schema expression
```

Inline object schemas are validated at compile-time (Go) and runtime (JavaScript). Expression-based schemas are resolved and validated at runtime in JavaScript.

For safe outputs that support `body`, the validator preserves output `data` and appends it to the body as fenced JSON:

```json
{
  "type": "add_comment",
  "body": "Review complete. All criteria pass.",
  "data": {
    "verdict": "APPROVE",
    "criteria_passed": 5
  }
}
```

You can configure sanitization options:

```yaml wrap
safe-outputs:
  allowed-domains: [api.github.com]  # GitHub domains always included
  allowed-github-references: []      # Escape all GitHub references
```

**Domain Filtering** (`allowed-domains`): Controls which domains are allowed in URLs. URLs from other domains are replaced with `(redacted)`. Accepts specific domain strings or [ecosystem identifiers](/gh-aw/reference/network/#ecosystem-identifiers):

> **Note:** `safe-outputs.allowed-domains` applies to **both** output sanitization (the domains the agent may reference in its outputs) and input sanitization (the `sanitized` activation step that redacts URLs in incoming issue/PR text before passing it to the agent). Domains listed here are therefore permitted in both directions.

```yaml wrap
safe-outputs:
  # Allow specific domains
  allowed-domains: [api.example.com, "*.storage.example.com"]

  # Use ecosystem identifiers
  allowed-domains: [default-safe-outputs]  # defaults + dev-tools + github + local

  # Mix identifiers and custom domains
  allowed-domains: [default-safe-outputs, api.example.com]
```

The `default-safe-outputs` compound ecosystem is the recommended baseline — it covers infrastructure certificates (`defaults`), GitHub domains (`github`), popular developer tooling (`dev-tools`), and loopback addresses (`local`).

**Reference Escaping** (`allowed-github-references`): Controls which GitHub repository references (`#123`, `owner/repo#456`) are allowed in workflow output. When configured, references to unlisted repositories are escaped with backticks to prevent GitHub from creating timeline items. This is particularly useful for [side repository](/gh-aw/patterns/multi-repo-ops/#using-a-side-repository) workflows to prevent automation from cluttering your main repository's timeline.

- `[]` - Escape all references (prevents all timeline items)
- `["repo"]` - Allow only the target repository's references
- `["repo", "owner/other-repo"]` - Allow specific repositories
- Not specified (default) - All references allowed

### Bot Mention Limit (`max-bot-mentions:`)

Agent output is automatically scanned for bot trigger phrases (e.g., `@copilot`, `@github-actions`) to prevent accidental automation triggering. By default, the first 10 occurrences are left unchanged and any excess are escaped with backticks. Entries already wrapped in backticks are skipped.

Use `max-bot-mentions` to adjust this threshold:

```yaml wrap
safe-outputs:
  max-bot-mentions: 3   # Allow 3 unescaped bot mentions per output
  create-issue:
```

Accepts a literal integer or a GitHub Actions expression string (e.g., `${{ inputs.max-mentions }}`). Set to `0` to escape all bot trigger phrases. Default: 10.

### Mention Filtering (`mentions:`)

By default, `@mentions` in AI-generated content are escaped with backticks unless the mentioned user is a verified collaborator or inferred from the event context (issue/PR author, assignees, etc.). Use `mentions:` to control this behavior:

```yaml wrap
safe-outputs:
  mentions: false          # Escape all mentions
  add-comment: {}
```

```yaml wrap
safe-outputs:
  mentions:
    allowed-collaborators: true  # Allow repo collaborators (default: true)
    allow-context: true          # Allow event context participants (default: true)
    allowed:                     # Individual users/bots always allowed
      - trusted-bot
    allowed-teams:               # Team members always allowed
      - myorg/eng                # org/team-slug format
      - reviewers                # team-slug only (uses current org)
    max: 50                      # Max mentions per message (default: 50)
  add-comment: {}
```

`allow-team-members` is a deprecated alias for `allowed-collaborators`. Run `gh aw fix` to migrate existing workflows.

**`allowed-teams`** lets organizations allow all members of specific GitHub teams to be mentioned without listing individual usernames. Team members are fetched from the GitHub API at runtime using `GET /orgs/{org}/teams/{team_slug}/members`. Bot accounts within the team are excluded. Use `org/team-slug` for cross-org teams or just `team-slug` to resolve against the current repository's organization.

> [!IMPORTANT]
> `allowed-teams` requires the workflow token to have `read:org` scope. The default `GITHUB_TOKEN` does **not** include this scope. Use one of the following:
> - A **classic PAT** with the `read:org` scope stored as a repository secret
> - A **fine-grained PAT** with the "Members" repository permission (read)
> - A **GitHub App** installation token with the "Members" permission (read)
>
> If the token lacks `read:org`, team membership lookup will fail with HTTP 403/404 and a warning will be logged. The workflow continues without those team members in the allowlist.

### Templatable Fields

`max`, `expires`, and `max-bot-mentions` accept GitHub Actions expression strings in addition to literal integers, allowing workflow inputs or repository variables to control limits at runtime:

```yaml wrap
safe-outputs:
  max-bot-mentions: ${{ inputs.max-mentions }}
  create-issue:
    max: ${{ inputs.max-issues }}
    expires: ${{ inputs.expires-days }}
  create-pull-request:
    max: ${{ inputs.max-prs }}
    draft: ${{ inputs.create-draft }}
```

Most boolean configuration fields also accept expression strings. Fields that influence permission computation (such as `create-pull-request.fallback-as-issue`) remain literal booleans.

### Maximum Patch Size (`max-patch-size:`)

Limits git patch size for PR operations (1-10,240 KB, default: 4096 KB):

```yaml wrap
safe-outputs:
  max-patch-size: 512  # max patch size in KB
  create-pull-request:
```

### Custom Runner Image

Specify a custom runner for safe output jobs (default: `ubuntu-slim`):

```aw
---
safe-outputs:
  runs-on: [self-hosted, linux, x64]
  create-issue: {}
---
```

`safe-outputs.runs-on` overrides `runs-on-slim:` for safe-output jobs specifically. To override the runner for all framework jobs at once, use the top-level [`runs-on-slim:`](/gh-aw/reference/self-hosted-runners/#configuring-the-framework-job-runner) field instead.

Custom safe-jobs can select their own runner with `safe-outputs.jobs.<job>.runs-on`. This field supports runner labels, label arrays, and runner-group objects:

```aw
---
safe-outputs:
  jobs:
    notify:
      runs-on:
        group: safe-job-runners
        labels: [linux]
      inputs:
        message:
          description: Notification message
      steps:
        - run: echo "Notify"
---
```

### Safe Outputs Job Concurrency (`concurrency-group:`)

Control concurrency for the compiled `safe_outputs` job. When set, the job uses this group with `cancel-in-progress: false` (queuing semantics — in-progress runs are never cancelled).

```yaml wrap
safe-outputs:
  concurrency-group: "safe-outputs-${{ github.repository }}"
  create-issue:
```

Supports GitHub Actions expressions. Use this to prevent concurrent safe output jobs from racing on shared resources (e.g., creating duplicate issues or conflicting PRs).

### Custom Messages (`messages:`)

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

The `footer-install` template renders the install instructions that follow the footer attribution line. When a workflow source is available and no custom template is set, the default renders as a collapsed `<details>` disclosure with the summary `Add this agentic workflows to your repo`; the expanded block contains the `gh aw add {workflow_source}` command. Custom `footer-install` overrides bypass the disclosure wrapper, so include `<details>` markup explicitly if you want the same collapsed UX. Supported placeholders: `{workflow_source}`, `{workflow_source_url}`.

**Variables**: `{workflow_name}`, `{run_url}`, `{agentic_workflow_url}`, `{triggering_number}`, `{triggering_type}`, `{workflow_source}`, `{workflow_source_url}`, `{event_type}`, `{status}`, `{operation}`, `{effective_tokens}`, `{effective_tokens_formatted}`, `{ai_credits_suffix}`, `{effective_tokens_suffix}`, `{ai_credits}`, `{ai_credits_formatted}`, `{ai_model}`, `{ai_model_short}`, `{ai_credits_unit}`, `{detection_conclusion}`, `{detection_reason}`, `{agent_ai_credits_formatted}`, `{evals_ai_credits_formatted}`, `{threat_detection_ai_credits_formatted}`, `{history_link}`

`{ai_credits_suffix}` is the preferred pre-formatted, always-safe suffix for run cost (for example, `" · sonnet46 12.4 AIC"` or `""`) and can be inserted directly into footer templates alongside `{history_link}`. `{effective_tokens}` and `{effective_tokens_formatted}` remain available as legacy ET compatibility fields. `{effective_tokens_suffix}` is also preserved as a legacy alias for older templates. When the run's engine model is known, the suffix is prefixed with a deterministic compact model identifier — `sonnetNN` for Sonnet, `gptNN` for GPT, `opusNN` for Opus, `haikuNN` for Haiku, `gemNN` for Gemini, with a stable fallback for other models. Direct short aliases like `opus`, `sonnet`, and `haiku` are preserved. The default footer uses AI Credits formatting; use these variables to customize output as needed. Individual components are also available: `{ai_model}` is the full model name (e.g. `claude-sonnet-4.6`), `{ai_model_short}` is the compact identifier (e.g. `sonnet46`), `{ai_credits_unit}` is always `AIC`, `{detection_conclusion}` and `{detection_reason}` expose the threat detection result. See [AI Credits Specification](/gh-aw/specs/ai-credits-specification/) for AIC details and [Effective Tokens Specification](/gh-aw/specs/effective-tokens-specification/) for legacy ET computation.

## Staged Mode

Staged mode lets you preview what safe outputs a workflow would create without actually creating anything. Every write operation is skipped; instead, a 🎭-labelled preview appears in the GitHub Actions step summary.

Enable it globally by adding `staged: true` to the `safe-outputs:` block:

```yaml wrap
safe-outputs:
  staged: true
  create-issue:
    title-prefix: "[ai] "
    labels: [automation]
```

You can also scope staged mode to a specific output type by adding `staged: true` directly to that type while leaving the global setting at `false`:

```yaml wrap
safe-outputs:
  create-pull-request:
    staged: true   # preview only
  add-comment:     # executes normally
```

To disable staged mode and start creating real resources, remove the `staged: true` setting or set it to `false`.

See [Staged Mode](/gh-aw/reference/staged-mode/) for the full guide, including the preview message format, per-type support table, custom message templates, and how to implement staged mode in [custom safe output jobs](/gh-aw/reference/custom-safe-outputs/#staged-mode-support).

## Replaying Safe Outputs

If the `safe_outputs` job fails or is skipped — for example, due to a transient API error, threat detection blocking the output, or a cancelled run — you can replay safe outputs from a previous run using the **Agentic Maintenance** workflow.

> [!NOTE]
> The Agentic Maintenance workflow (`agentics-maintenance.yml`) is generated automatically when any workflow uses the `expires` field in `create-issue`, `create-discussion`, or `create-pull-request` safe outputs.

To replay safe outputs:

1. Go to your repository's **Actions** tab.
2. Select the **Agentic Maintenance** workflow.
3. Click **Run workflow**.
4. Set **Optional maintenance operation** to `safe_outputs`.
5. Set **Run URL or run ID** to the URL or run ID of the previous workflow run:
   - Full URL: `https://github.com/OWNER/REPO/actions/runs/12345`
   - Run ID only: `12345`
6. Click **Run workflow**.

The `apply_safe_outputs` job downloads the `agent_output.json` artifact from the specified run and applies all safe outputs as if the original run had completed successfully. Authorization requires exact `admin` or `maintain` repository access. For custom organization repository roles, gh-aw authorizes against the inherited standard role metadata reported by GitHub rather than the custom role name. If that inherited standard role cannot be resolved, the replay request is rejected.

> [!TIP]
> Find the run URL by opening the failed or cancelled run in the **Actions** tab — the URL in your browser's address bar is the run URL.

## Learn More

- [Staged Mode](/gh-aw/reference/staged-mode/) - Preview safe output operations without making changes
- [Threat Detection Guide](/gh-aw/reference/threat-detection/) - Complete threat detection documentation and examples
- [Frontmatter](/gh-aw/reference/frontmatter/) - All configuration options for workflows
- [Workflow Structure](/gh-aw/reference/workflow-structure/) - Directory layout and organization
- [Command Triggers](/gh-aw/reference/command-triggers/) - Special /my-bot triggers and context text
- [CLI Commands](/gh-aw/setup/cli/) - CLI commands for workflow management
