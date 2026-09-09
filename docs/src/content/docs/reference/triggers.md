---
title: Triggers
description: Triggers in GitHub Agentic Workflows
sidebar:
  order: 400
---

The `on:` section uses standard GitHub Actions syntax to define workflow triggers. For example:

```yaml wrap
on:
  issues:
    types: [opened]
```

## Trigger Types

GitHub Agentic Workflows supports all standard GitHub Actions triggers plus additional enhancements for reactions, cost control, and advanced filtering.

### Dispatch Triggers (`workflow_dispatch:`)

Run workflows manually from the GitHub UI, API, or via `gh aw run`/`gh aw trial`. [Full syntax reference](https://docs.github.com/en/actions/using-workflows/workflow-syntax-for-github-actions#on).

**Basic trigger:**

```yaml wrap
on:
  workflow_dispatch:
```

**With input parameters:**

```yaml wrap
on:
  workflow_dispatch:
    inputs:
      topic:
        description: 'Research topic'
        required: true
        type: string
      priority:
        description: 'Task priority'
        required: false
        type: choice
        options:
          - low
          - medium
          - high
        default: medium
      deploy_env:
        description: 'Target environment'
        required: false
        type: environment
        default: staging
```

#### Accessing Inputs in Markdown

Access inputs in your markdown content with `${{ github.event.inputs.INPUT_NAME }}`:

```markdown
Research the following topic: "${{ github.event.inputs.topic }}"
```

**Supported input types:** `string` (free-form text), `boolean` (checkbox), `choice` (dropdown with predefined options), and `environment` (dropdown populated from repository Settings → Environments).

The `environment` input returns the environment name as a string and supports a `default` value. Unlike `manual-approval:`, it does not enforce environment protection rules — it only provides the environment name for use in your workflow logic.

### Scheduled Triggers (`schedule:`)

Run workflows on a recurring schedule using human-friendly expressions or [cron syntax](https://docs.github.com/en/actions/using-workflows/events-that-trigger-workflows#schedule).

**Fuzzy Scheduling:**

Fuzzy schedules scatter execution times to avoid load spikes. Use `around <time>` for a preferred time with ±1 hour flexibility, or `between <a> and <b>` to scatter within a window (such as business hours):

```yaml wrap
on:
  schedule: daily                              # Compiler picks a scattered time
  # schedule: daily around 14:00               # ±1 hour around 2pm
  # schedule: daily between 9:00 and 17:00     # Scatters within 9am-5pm
```

The compiler assigns each workflow a unique, deterministic execution time based on the file path, ensuring load distribution and consistency across recompiles. UTC offsets are supported on any time expression (e.g., `daily between 9am and 5pm utc-5`).

For a fixed time, use standard cron syntax. Add an optional `timezone` field to interpret the cron in a specific IANA timezone instead of UTC:

```yaml wrap
on:
  schedule:
    - cron: "30 6 * * 1"  # Monday at 06:30 UTC
    - cron: "0 9 15 * *"  # 15th of month at 09:00 UTC
    - cron: "30 9 * * 1-5"
      timezone: "America/New_York"  # 9:30 AM EST/EDT Mon-Fri
```

| Format | Example | Result | Notes |
|--------|---------|--------|-------|
| **Hourly (Fuzzy)** | `hourly` | `58 */1 * * *` | Compiler assigns scattered minute |
| **Daily (Fuzzy)** | `daily` | `43 5 * * *` | Compiler assigns scattered time |
| | `daily around 14:00` | `20 14 * * *` | Scattered within ±1 hour (13:00-15:00) |
| | `daily between 9:00 and 17:00` | `37 13 * * *` | Scattered within range (9:00-17:00) |
| | `daily between 9am and 5pm utc-5` | `12 18 * * *` | With UTC offset (9am-5pm EST → 2pm-10pm UTC) |
| | `daily around 3pm utc-5` | `33 19 * * *` | With UTC offset (3 PM EST → 8 PM UTC) |
| **Weekly (Fuzzy)** | `weekly` or `weekly on monday` | `43 5 * * 1` | Compiler assigns scattered time |
| | `weekly on friday around 5pm` | `18 16 * * 5` | Scattered within ±1 hour |
| **Intervals** | `every 10 minutes` | `*/10 * * * *` | Minimum 5 minutes |
| | `every 2h` | `53 */2 * * *` | Fuzzy: scattered minute offset |
| | `0 */2 * * *` | `0 */2 * * *` | Cron syntax for fixed times |

**Time formats:** `HH:MM` (24-hour), `midnight`, `noon`, `1pm`-`12pm`, `1am`-`12am`
**UTC offsets:** Add `utc+N` or `utc-N` to any time (e.g., `daily around 14:00 utc-5`)

Human-friendly formats are automatically converted to standard cron expressions, with the original format preserved as a comment in the generated workflow file.

### Cooldown (`cooldown:`)

Set `on.cooldown` to skip agent execution until a duration has elapsed since the latest completed run that started the `agent` job. This is useful when a workflow runs more frequently than you want the agent to act, such as checking hourly but allowing an agent run every four hours:

```aw wrap
on:
  schedule: hourly
  cooldown: 4h
```

The duration uses Go duration syntax, such as `5m`, `1h`, or `1h30m`, and must be at least five minutes. GitHub Actions expressions are not supported. The cooldown starts when the previous workflow run finishes, provided the `agent` job started, regardless of whether that job succeeded or failed. Runs where the `agent` job was skipped do not restart the cooldown.

The compiler adds `actions: read` to the pre-activation job so it can inspect completed runs. If run history cannot be queried, the cooldown check fails open and allows the agent to run.

### Issue Triggers (`issues:`)

Trigger on issue events. [Full event reference](https://docs.github.com/en/actions/using-workflows/events-that-trigger-workflows#issues).

```yaml wrap
on:
  issues:
    types: [opened, edited, labeled]
```

#### Issue Locking (`lock-for-agent:`)

Prevent concurrent modifications to an issue during workflow execution by setting `lock-for-agent: true`:

```yaml wrap
on:
  issues:
    types: [opened, edited]
    lock-for-agent: true
```

The issue is locked at workflow start and unlocked after completion (or before safe-output processing); the unlock step uses `always()` so cleanup runs even on failure. Useful for workflows that make multiple sequential updates or need to prevent race conditions.

### Pull Request Triggers (`pull_request:`)

Trigger on pull request events. [Full event reference](https://docs.github.com/en/actions/using-workflows/events-that-trigger-workflows#pull_request).

When triggered by a pull request event, the coding agent has access to both the PR branch and the default branch.

```yaml wrap
on:
  pull_request:
    types: [opened, synchronize, labeled]
    names: [ready-for-review, needs-review]
  reaction: "rocket"
```

The `types:` list accepts standard GitHub pull request activity types, including transition events such as `ready_for_review` and `converted_to_draft`.

### Pull Request Target Triggers (`pull_request_target:`)

Trigger on pull request events in the context of the base repository. Use this only when the workflow needs write-capable repository context, and avoid checking out untrusted fork code. See [Checkout](/gh-aw/reference/checkout/#pull_request_target-checkout) for the safe checkout pattern.

```yaml wrap
on:
  pull_request_target:
    types: [opened, ready_for_review]
```

`pull_request_target.types` accepts the same activity names as `pull_request`, including `ready_for_review`.

#### Fork Filtering (`forks:`)

Pull request workflows block forks by default for security. Use the `forks:` field to allow specific fork patterns:

```yaml wrap
on:
  pull_request:
    types: [opened, synchronize]
    forks: ["trusted-org/*"]  # Allow forks from trusted-org
```

Use `["owner/repo"]` for a specific repository, `["owner/*"]` for an entire org/user, or `["*"]` to allow all forks (use with caution). Omit `forks:` for the default behavior (same-repository PRs only). The compiler uses repository ID comparison so fork detection is unaffected by repository renames.

#### Stacked PR Filtering (`max-stack:`)

When using stacked pull requests (a chain of PRs each targeting the previous one), every PR in the stack normally triggers workflows. This multiplies CI cost for identical changes being reviewed at multiple layers. The `max-stack:` field lets you limit which stack layers run the workflow.

By default (`max-stack: 1`), workflows run only on the **top-most PR** in the stack — the one currently under review. Lower-stack PRs are skipped automatically.

This is supported for both `pull_request` and `pull_request_review` triggers.

```yaml wrap
on:
  pull_request:
    types: [opened, synchronize]
    max-stack: 1   # default: run only on the top/latest PR in a stack
```

To run on the **top N** layers (for example, if you review multiple interdependent PRs at once):

```yaml wrap
on:
  pull_request:
    types: [opened, synchronize]
    max-stack: 2   # run on the top 2 PRs in a stack
```

To **disable** stack protection and run on every PR in a stack:

```yaml wrap
on:
  pull_request:
    types: [opened, synchronize]
    max-stack: -1  # run on all pull requests regardless of stack position
```

You can also apply the same filter to review events:

```yaml wrap
on:
  pull_request_review:
    types: [submitted]
    max-stack: 2   # run only for reviews on the top 2 PRs in a stack
```

Non-stacked PRs and non-`pull_request`/`pull_request_review` events are unaffected by this setting.

### Comment Triggers

The triggers `issue_comment:`, `pull_request_review_comment:`, and `discussion_comment:` activate workflows when comments are created or edited.

Note that `issue_comment` events also fire for comments on pull requests (GitHub models PR comments as issue comments). When a comment is on a pull request, the coding agent has access to both the PR branch and the default branch.

```yaml wrap
on:
  issue_comment:
    types: [created]
  pull_request_review_comment:
    types: [created]
  discussion_comment:
    types: [created]
  reaction: "eyes"
```

#### Comment Locking (`lock-for-agent:`)

For `issue_comment` events, you can lock the parent issue during workflow execution:

```yaml wrap
on:
  issue_comment:
    types: [created, edited]
    lock-for-agent: true
```

This prevents concurrent modifications to the issue while processing the comment. The locking behavior is identical to the `issues:` trigger (see [Issue Locking](#issue-locking-lock-for-agent) above for full details).

**Note:** Pull request comments are silently skipped as pull requests cannot be locked via the issues API.

### Workflow Run Triggers (`workflow_run:`)

Trigger workflows after another workflow completes. [Full event reference](https://docs.github.com/en/actions/using-workflows/events-that-trigger-workflows#workflow_run).

```yaml wrap
on:
  workflow_run:
    workflows: ["CI"]
    types: [completed]
    branches:
      - main
      - develop
```

You can also combine `workflow_run` with top-level authorization filters such as `bots:` or `roles:`:

```yaml wrap
on:
  workflow_run:
    workflows: ["CI"]
    types: [completed]
  bots: ["dependabot[bot]"]
```

Workflows with `workflow_run` triggers include automatic security protections: `workflows` must list at least one non-empty entry (empty or missing values are rejected at compile time, since GitHub silently disables such triggers); the compiler injects repository ID and fork checks to reject cross-repository or fork-triggered runs; and `branches` is recommended to limit triggering branches (the compiler warns when omitted, or errors in strict mode). See the [Security Architecture](/gh-aw/introduction/architecture/) for details.

#### Conclusion Filtering (`conclusion:`)

Use `conclusion:` to restrict the trigger to specific workflow run outcomes. Accepts a single value or a list. Compiles into a guarded `if:` condition — other events in the same `on:` block are unaffected.

```yaml wrap
on:
  workflow_run:
    workflows: ["CI"]
    types: [completed]
    conclusion: [failure, cancelled]
```

Valid values: `success`, `failure`, `cancelled`, `skipped`, `timed_out`, `action_required`, `neutral`, `stale`.

### Deployment Status Triggers (`deployment_status:`)

Trigger workflows when a GitHub deployment status changes. [Full event reference](https://docs.github.com/en/actions/using-workflows/events-that-trigger-workflows#deployment_status).

```yaml wrap
on:
  deployment_status:
```

#### State Filtering (`state:`)

Use `state:` to restrict the trigger to specific deployment states (single value or list). The compiler emits a guarded `if:` condition so other combined triggers (such as `workflow_dispatch`) pass through unaffected.

```yaml wrap
on:
  deployment_status:
    state: [error, failure]   # Or a single value: state: failure
  workflow_dispatch:           # Safely combined — guard ensures dispatch passes through
```

Valid `state` values: `error`, `failure`, `pending`, `success`, `inactive`, `in_progress`, `queued`, `waiting`.

Workflows triggered by `deployment_status` need `deployments: read` to access the event payload:

```yaml wrap
permissions:
  contents: read
  deployments: read
```

### Repository Dispatch Trigger (`repository_dispatch:`)

Trigger a workflow from outside GitHub using a single authenticated API call. Any external system that can make an HTTP `POST` request—Jira, PagerDuty, Slack, or a custom API—can start an agentic workflow this way. [Full event reference](https://docs.github.com/en/actions/writing-workflows/choosing-when-your-workflow-runs/events-that-trigger-workflows#repository_dispatch).

```yaml wrap
on:
  repository_dispatch:
    types: [jira-issue-created]
```

Omit `types:` to fire on any `event_type`.

#### Sending the Dispatch Request

Call the GitHub dispatch API with a `repo`-scoped PAT (classic) or a token with `contents: write` permission:

```http
POST https://api.github.com/repos/<owner>/<repo>/dispatches
Authorization: Bearer <token>
Content-Type: application/json

{
  "event_type": "jira-issue-created",
  "client_payload": { "issue_key": "PROJ-123", "summary": "Fix the thing" }
}
```

#### Accessing the Payload

Reference `client_payload` fields in your workflow markdown using standard GitHub Actions expressions:

```markdown
Issue ${{ github.event.client_payload.issue_key }}: ${{ github.event.client_payload.summary }}
```

### Command Triggers (`slash_command:`)

The `slash_command:` trigger creates workflows that respond to `/command-name` mentions in issues, pull requests, and comments.

By default, command triggers listen to **all** comment-related events, which can create noise from skipped runs. Use the `events:` field to restrict where commands are active:

```yaml wrap
on:
  slash_command:
    name: investigate
    events: [issues, issue_comment]  # Only respond in issue contexts
    # strategy: centralized  # Optional: route via generated central trigger workflow
```

See [Command Triggers](/gh-aw/reference/command-triggers/) for complete documentation including event filtering, context text, reactions, and examples.

### Label Command Trigger (`label_command:`)

The `label_command:` trigger activates a workflow when a specific label is applied to an issue, pull request, or discussion, and **automatically removes that label** so it can be re-applied to re-trigger. This treats a label as a one-shot command rather than a persistent state marker.

```yaml wrap
# Fires on issues, pull_request, and discussion by default
on:
  label_command: deploy

# Restrict to specific event types
on:
  label_command:
    name: deploy
    events: [pull_request]

# Disable automatic label removal (label stays on the item after activation)
on:
  label_command:
    name: deploy
    remove_label: false

# Shorthand string form
on: "label-command deploy"
```

The compiler generates `issues`, `pull_request`, and/or `discussion` events with `types: [labeled]`, adds a `workflow_dispatch` trigger with `item_number` for manual testing, and injects a label removal step in the activation job. The matched label name is exposed as `needs.activation.outputs.label_command`.

The `remove_label` field (boolean, default `true`) controls whether the label is automatically removed after activation. Set to `false` to keep the label on the item — useful when the label represents persistent state rather than a one-shot command. When `remove_label: false`, the workflow does not need `issues: write` or `pull-requests: write` permissions for label removal.

`label_command` can be combined with `slash_command:` — the workflow activates when either condition is met. See [LabelOps](/gh-aw/patterns/label-ops/) for patterns and examples.

## Trigger Filtering

Triggers can be filtered by label names, and more. These filters compile into guarded `if:` conditions that ensure the workflow only runs when the specified criteria are met, while allowing other events to pass through unaffected.

### Filtering with Labels (`names:`)

Filter issue and pull request triggers by label names using the `names:` field. Unlike `label_command`, the label stays on the item after the workflow runs.

```yaml wrap
on:
  issues:
    types: [labeled, unlabeled]
    names: [bug, critical, security]
```

Use convenient shorthand for label-based triggers:

```yaml wrap
on: issue labeled bug
on: issue labeled bug, enhancement, priority-high  # Multiple labels
on: pull_request labeled needs-review, ready-to-merge
```

All shorthand formats compile to standard GitHub Actions syntax and automatically include the `workflow_dispatch` trigger. Supported for `issue`, `pull_request`, and `discussion` events. See [LabelOps workflows](/gh-aw/patterns/label-ops/) for automation examples.

### Filtering with Simple Conditions (`:if`)

For conditions that can be expressed directly with GitHub Actions context, use `if:` without a custom job:

```yaml wrap
---
on:
  pull_request:
    types: [opened, synchronize]

if: github.event.pull_request.draft == false
---
```

### Filtering with Search Queries (`skip-if-match:`, `skip-if-no-match:`)

For conditions based on GitHub search results, use [`skip-if-match:`](#skip-if-match-condition-skip-if-match) or [`skip-if-no-match:`](#skip-if-no-match-condition-skip-if-no-match) in the `on:` section. These accept standard [GitHub search query syntax](https://docs.github.com/en/search-github/searching-on-github/searching-issues-and-pull-requests) and produce the same skipped-not-failed behavior.

### Filtering by Repository Access Roles (`on.roles:`, `on.skip-roles:`)

Controls who can trigger agentic workflows using an **exact-match allowlist** against the actor's repository role. Defaults to `[admin, maintainer, write]`. Use `skip-roles:` to exempt team members from checks that should only apply to external contributors.

```yaml wrap
on:
  issues:
    types: [opened]
  roles: [admin, maintainer, write]   # Default; use `all` to allow any user (⚠️ caution)
  # skip-roles: [admin, maintainer, write]
```

:::caution[Exact match, not a minimum threshold]
`roles` is an allowlist, not a privilege threshold. Setting `roles: [write]` will **reject** actors with `admin` or `maintainer` roles because `admin !== write`. To accept all typical contributors, list every role explicitly, e.g. `[admin, maintainer, write]`.
:::

Available roles: `admin`, `maintainer`/`maintain`, `write`, `triage`, `read`, `all`. Workflows with unsafe triggers (`push`, `issues`, `pull_request`) automatically enforce permission checks. Failed checks cancel the workflow with a warning.

#### Custom organization repository roles

GitHub organizations can define [custom repository roles](https://docs.github.com/en/organizations/managing-user-access-to-your-organizations-repositories/managing-repository-roles/about-custom-repository-roles) with an inherited standard role (for example `write` or `maintain`). Actors whose access comes from a custom role (e.g. `Security Champions`) are authorized against that **inherited standard role** — the custom role name itself cannot appear in `on.roles:`. For example, a user with the custom role `Security Champions` (inherited role: `write`) will be authorized when the required roles include `write`, while a custom role inherited from `maintain` will still be rejected by `roles: [write]`.

### Filtering by Bot (`on.bots:`, `on.skip-bots:`)

Configure which GitHub bot accounts can trigger workflows — useful for allowing specific automation bots while maintaining security controls. Use `skip-bots:` for the inverse:

```yaml wrap
on:
  issues:
    types: [opened]
  bots: ["dependabot[bot]", "renovate[bot]", "agentic-workflows-dev[bot]"]
  # skip-bots: [github-actions, copilot, dependabot]
```

The `[bot]` suffix is optional — `github-actions` matches `github-actions[bot]` automatically.

### Filtering by Author Associations (`on.skip-author-associations`)

You can skip workflow execution when a specific event is triggered by an author with a matching event payload `author_association` field (for example `github.event.comment.author_association`, `github.event.issue.author_association`, or `github.event.pull_request.author_association`).

```yaml wrap
on:
  issue_comment:
    types: [created]
  pull_request_review_comment:
    types: [created]
  skip-author-associations:
    issue_comment: contributor
    pull_request_review_comment: [first_time_contributor, none]
```

### Filtering by Custom Steps (`on.steps:`)

Inject deterministic filtering steps directly into the pre-activation job — see [Pre-Activation Steps](#pre-activation-steps-onsteps) for full syntax and examples. This is the recommended approach for lightweight filtering since it saves one workflow job versus the multi-job pattern below.

### Filtering by Custom Jobs (`jobs:`)

For complex custom trigger filtering you can use a separate `jobs:` entry when filtering requires heavy tooling (checkout, compiled tools, multiple runners):

```yaml wrap title=".github/workflows/smart-responder.md"
---
on:
  issues:
    types: [opened]

safe-outputs:
  add-comment:

jobs:
  filter:
    runs-on: ubuntu-latest
    outputs:
      should-run: ${{ steps.check.outputs.result }}
    steps:
      - id: check
        env:
          LABELS: ${{ toJSON(github.event.issue.labels.*.name) }}
        run: |
          if echo "$LABELS" | grep -q '"bug"'; then
            echo "result=true" >> "$GITHUB_OUTPUT"
          else
            echo "result=false" >> "$GITHUB_OUTPUT"
          fi

if: needs.filter.outputs.should-run == 'true'
---

# Bug Issue Responder

Triage bug report: "${{ github.event.issue.title }}" and add-comment with a summary of the next steps.
```

The compiler automatically adds the filter job as a dependency of the activation job, so when the condition is false the workflow run is **skipped** (not failed), keeping the Actions tab clean.

## Additional Trigger Options

Trigger support additional options for reactions, status comments, authentication tokens, and more. These options are configured in the same `on:` block as the trigger and apply to all triggers defined within that block.

### Reactions (`reaction:`)

Enable emoji reactions on triggering items (issues, PRs, comments, discussions) to provide visual workflow status feedback:

```yaml wrap
on:
  issues:
    types: [opened]
  reaction: "eyes"
```

The reaction is added to the triggering item. Use `none` to disable reactions entirely.

**Available reactions:** `+1` 👍, `-1` 👎, `laugh` 😄, `confused` 😕, `heart` ❤️, `hooray` 🎉, `rocket` 🚀, `eyes` 👀

### Status Comments (`status-comment:`)

Post a started/completed comment on the triggering item with a link to the workflow run:

```yaml wrap
on:
  issues:
    types: [opened]
  reaction: "eyes"
  status-comment: true
```

When `status-comment: true`, the activation job posts a comment on workflow start and updates it on completion. `reaction:` and `status-comment:` are independent settings. For `slash_command` and `label_command`, both default to enabled (with `reaction: eyes`); disable either explicitly:

```yaml wrap
on:
  slash_command: my-bot
  reaction: none           # disable the eyes reaction
  status-comment: false    # disable the status comment
```

For all other triggers, `status-comment` must be explicitly set to `true`. Use an object to selectively disable specific targets (each field defaults to `true`):

```yaml wrap
on:
  issues:
    types: [opened]
  pull_request:
    types: [opened]
  discussion:
    types: [created]
  status-comment:
    issues: true          # post on issue events (default)
    pull-requests: false  # skip pull request events
    discussions: false    # skip discussion events
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `issues` | boolean | `true` | Enable status comments for `issues` and `issue_comment` events |
| `pull-requests` | boolean | `true` | Enable status comments for `pull_request` and `pull_request_review_comment` events |
| `discussions` | boolean | `true` | Enable status comments for `discussion` and `discussion_comment` events |

### Activation Token (`on.github-token:`, `on.github-app:`)

Configure a custom GitHub token or GitHub App for the activation job **and all skip-if search checks** — reaction, status comment, and search steps share the same token (default: workflow's `GITHUB_TOKEN`). Use `github-token:` for a PAT or `github-app:` to mint a short-lived installation token:

```yaml wrap
on:
  issues:
    types: [opened]
  reaction: "eyes"
  github-token: ${{ secrets.MY_TOKEN }}
```

```yaml wrap
on:
  issues:
    types: [opened]
  reaction: "rocket"
  github-app:
    client-id: ${{ vars.APP_ID }}
    private-key: ${{ secrets.APP_KEY }}
```

The `github-app` object accepts `client-id`, `private-key`, and optionally `owner` and `repositories` — the same fields used elsewhere in the framework (`app-id` is a deprecated alias for `client-id`). The token is minted once in the pre-activation job.

Both fields can be defined in a **shared agentic workflow** and are inherited by importers (first-wins). A `CentralRepoOps` shared workflow can define the app config once and all importers benefit:

```yaml wrap
# shared-ops.md - define app config once
on:
  workflow_call:
  github-app:
    client-id: ${{ secrets.ORG_APP_ID }}
    private-key: ${{ secrets.ORG_APP_PRIVATE_KEY }}
    owner: myorg
```

```yaml wrap
# any-workflow.md - inherits github-app from the import
imports:
  - .github/workflows/shared/shared-ops.md
on:
  schedule: every 30 minutes
  skip-if-no-match:
    query: "org:myorg label:agent-fix is:issue is:open"
    scope: none
```

> [!NOTE]
> `github-token` and `github-app` affect only the activation job (reactions, status comments, and skip-if searches). For the agent job, configure tokens via `tools.github.github-token`/`tools.github.github-app` or `safe-outputs.github-token`/`safe-outputs.github-app`. See [Authentication](/gh-aw/reference/auth/) for a full overview.

### Stop After Configuration (`stop-after:`)

Automatically disable workflow triggering after a deadline to control costs.

```yaml wrap
on: weekly on monday
  stop-after: "+25h"  # 25 hours from compilation time
```

Accepts absolute dates (`YYYY-MM-DD`, `MM/DD/YYYY`, `DD/MM/YYYY`, `January 2 2006`, `1st June 2025`, ISO 8601) or relative deltas (`+7d`, `+25h`, `+1d12h30m`) calculated from compilation time. The minimum granularity is hours - minute-only units (e.g., `+30m`) are not allowed. Recompiling the workflow resets the stop time.

### Manual Approval Gates (`manual-approval:`)

Require manual approval before workflow execution using GitHub environment protection rules:

```yaml wrap
on:
  workflow_dispatch:
  manual-approval: production
```

Sets the `environment` on the activation job for human-in-the-loop approval before execution. The value must match a configured environment in repository Settings → Environments (approval rules, required reviewers, wait timers). See [GitHub's environment documentation](https://docs.github.com/en/actions/deployment/targeting-different-environments/using-environments-for-deployment) for configuration details.

### Skip-If-Match Condition (`skip-if-match:`)

Conditionally skip workflow execution when a GitHub search query has matches. Useful for preventing duplicate scheduled runs or waiting for prerequisites.

```yaml wrap
on: daily
  skip-if-match: 'is:issue is:open in:title "[daily-report]"'  # Skip if any match
```

```yaml wrap
on: weekly on monday
  skip-if-match:
    query: "is:pr is:open label:urgent"
    max: 3  # Skip if 3 or more PRs match
```

A pre-activation check runs the query against the current repository. If matches reach or exceed the threshold (default `max: 1`), the workflow is skipped. All standard GitHub search qualifiers are supported (`is:`, `label:`, `in:title`, `author:`, etc.).

Use `scope: none` to remove the automatic repo qualifier and search org-wide. For cross-repo or org-wide searches that need elevated permissions, configure `github-token` or `github-app` at the top-level `on:` section — the same token is shared across all skip-if checks and the activation job:

```yaml wrap
on:
  schedule: every 15 minutes
  skip-if-match:
    query: "org:myorg label:ops:in-progress is:issue is:open"
    scope: none
  github-app:
    client-id: ${{ secrets.WORKFLOW_APP_ID }}
    private-key: ${{ secrets.WORKFLOW_APP_PRIVATE_KEY }}
    owner: myorg
```

| Field | Location | Description |
|-------|----------|-------------|
| `scope: none` | inside `skip-if-match` | Disables the automatic `repo:owner/repo` qualifier |
| `github-token` | top-level `on:` | Custom PAT or token for all skip-if searches (e.g. `${{ secrets.CROSS_ORG_TOKEN }}`) |
| `github-app` | top-level `on:` | Mints a short-lived installation token shared across all skip-if steps; requires `client-id` and `private-key` |

`github-token` and `github-app` are mutually exclusive. String shorthand always uses the default `GITHUB_TOKEN` scoped to the current repository.

### Skip-If-No-Match Condition (`skip-if-no-match:`)

Conditionally skip workflow execution when a GitHub search query has **no matches** (or fewer than the minimum required). This is the opposite of `skip-if-match`.

```yaml wrap
on: weekly on monday
  skip-if-no-match: 'is:pr is:open label:ready-to-deploy'  # Skip if no matches
```

```yaml wrap
on:
  workflow_dispatch:
  skip-if-no-match:
    query: "is:issue is:open label:urgent"
    min: 3  # Only run if 3 or more issues match
```

If matches are below the threshold (default `min: 1`), the workflow is skipped. Can be combined with `skip-if-match` for complex conditions. `scope: none`, `github-token`, and `github-app` work identically to [`skip-if-match`](#skip-if-match-condition-skip-if-match) above — a single mint step is shared when both are present.

### Pre-Activation Steps (`on.steps:`)

Inject custom deterministic steps directly into the pre-activation job. Steps run after all built-in checks (membership, stop-time, skip-if, etc.) and **before** agent execution. This saves one workflow job compared to the multi-job pattern and keeps filtering logic co-located with the trigger configuration.

```yaml wrap
on:
  issues:
    types: [opened]
  restore-memory: true
  steps:
    - name: Check issue label
      id: label_check
      env:
        LABELS: ${{ toJSON(github.event.issue.labels.*.name) }}
      run: echo "$LABELS" | grep -q '"bug"'
      # exits 0 (outcome: success) if the label is found, 1 (outcome: failure) if not

if: needs.pre_activation.outputs.label_check_result == 'success'
```

Each step with an `id` automatically gets an output `<id>_result` wired to `${{ steps.<id>.outcome }}` (values: `success`, `failure`, `cancelled`, `skipped`). This lets you gate the workflow on whether the step **succeeded or failed** via its exit code.

To pass an explicit value rather than relying on exit codes, set a step output (e.g., `echo "has_bug_label=true" >> "$GITHUB_OUTPUT"`) and re-expose it via `jobs.pre-activation.outputs`:

```yaml wrap
jobs:
  pre-activation:
    outputs:
      has_bug_label: ${{ steps.label_check.outputs.has_bug_label }}

if: needs.pre_activation.outputs.has_bug_label == 'true'
```

Explicit outputs in `jobs.pre-activation.outputs` take precedence over auto-wired `<id>_result` outputs on key collision.

Set `on.restore-memory: true` to restore memory stores (`cache-memory`, `repo-memory`, `comment-memory`) before `on.steps` run. The default is `false`.

### Pre-Activation and Activation Dependencies (`on.needs:`)

Add custom jobs that both `pre_activation` and `activation` should depend on. Use this when `on.github-app` credentials come from a job output (for example, a secret-manager fetch job).

```yaml wrap
on:
  workflow_dispatch:
  needs: [secrets_fetcher]
  github-app:
    client-id: ${{ needs.secrets_fetcher.outputs.app_id }}
    private-key: ${{ needs.secrets_fetcher.outputs.private_key }}

jobs:
  secrets_fetcher:
    runs-on: ubuntu-latest
    outputs:
      app_id: ${{ steps.fetch.outputs.app_id }}
      private_key: ${{ steps.fetch.outputs.private_key }}
    steps:
      - id: fetch
        run: |
          echo "app_id=123" >> "$GITHUB_OUTPUT"
          echo "private_key=***" >> "$GITHUB_OUTPUT"
```

`on.needs` values must reference custom jobs from top-level `jobs:`. Built-in jobs are rejected.

### Pre-Activation Permissions (`on.permissions:`)

Grant additional GitHub token permission scopes to the pre-activation job. Use when `on.steps:` make GitHub API calls that require permissions beyond the defaults.

```yaml wrap
on:
  schedule: every 30m
  permissions:
    issues: read
    pull-requests: read
  steps:
    - name: Search for candidate issues
      id: search
      uses: actions/github-script@v8
      with:
        script: |
          const issues = await github.rest.issues.listForRepo(context.repo);
          core.setOutput('has_issues', issues.data.length > 0 ? 'true' : 'false');

jobs:
  pre-activation:
    outputs:
      has_issues: ${{ steps.search.outputs.has_issues }}

if: needs.pre_activation.outputs.has_issues == 'true'
```

Supported permission scopes: `actions`, `checks`, `contents`, `deployments`, `discussions`, `issues`, `packages`, `pages`, `pull-requests`, `repository-projects`, `security-events`, `statuses`.

`on.permissions` is merged on top of any permissions already required by the pre-activation job (e.g., `contents: read` for dev-mode checkout, `actions: read` for rate limiting).

## Trigger Shorthands

Instead of writing full YAML trigger configurations, you can use natural-language shorthand strings with `on:`. The compiler expands these into standard GitHub Actions trigger syntax and automatically includes `workflow_dispatch` so the workflow can also be run manually.

For label-based shorthands (`on: issue labeled bug`, `on: pull_request labeled needs-review`), see [Label Filtering](#filtering-with-labels-names) above. For the label-command pattern, see [Label Command Trigger](#label-command-trigger-label_command) above.

### Push and Pull Request

```yaml wrap
on: push to main                    # Push to specific branch
on: push tags v*                    # Push tags matching pattern
on: pull_request opened             # PR with activity type
on: pull_request merged             # PR merged (maps to closed + merge condition)
on: pull_request affecting src/**   # PR touching paths (opened, synchronize, reopened)
on: pull_request opened affecting docs/**  # Activity type + path filter
```

`pull` is an alias for `pull_request`. Valid activity types: `opened`, `edited`, `closed`, `reopened`, `synchronize`, `assigned`, `unassigned`, `labeled`, `unlabeled`, `review_requested`, `merged`.

### Issues and Discussions

```yaml wrap
on: issue opened                    # Issue with activity type
on: issue opened labeled bug        # Issue opened with specific label (adds job condition)
on: discussion created              # Discussion with activity type
```

Valid issue types: `opened`, `edited`, `closed`, `reopened`, `assigned`, `unassigned`, `labeled`, `unlabeled`, `deleted`, `transferred`. Valid discussion types: `created`, `edited`, `deleted`, `transferred`, `pinned`, `unpinned`, `labeled`, `unlabeled`, `locked`, `unlocked`, `category_changed`, `answered`, `unanswered`.

### Other Shorthands

```yaml wrap
on: manual                          # workflow_dispatch (run manually)
on: manual with input version       # workflow_dispatch with a string input
on: workflow completed ci-test       # Trigger after another workflow completes
on: comment created                 # Issue or PR comment created
on: release published               # Release event (published, created, prereleased, etc.)
on: repository starred              # Repository starred (maps to watch event)
on: repository forked               # Repository forked
on: dependabot pull request         # PR from Dependabot (adds actor condition)
on: security alert                  # Code scanning alert
on: code scanning alert             # Alias for security alert (code scanning alert)
on: api dispatch custom-event       # Repository dispatch with custom event type
on: "deployment failed"             # deployment_status with state == 'failure' guard
on: "deployment error"              # deployment_status with state == 'error' guard
on: "deployment failed or error"    # deployment_status with state == 'failure' or 'error' guard
```

## Learn More

- [Schedule Syntax](/gh-aw/reference/schedule-syntax/) - Complete schedule format reference
- [Command Triggers](/gh-aw/reference/command-triggers/) - Special @mention triggers and context text
- [Frontmatter](/gh-aw/reference/frontmatter/) - Complete frontmatter configuration
- [DeterministicOps](/gh-aw/patterns/deterministic-ops/) - Combining deterministic steps with AI reasoning
- [LabelOps](/gh-aw/patterns/label-ops/) - Label-based automation workflows
- [Workflow Structure](/gh-aw/reference/workflow-structure/) - Directory layout and organization
