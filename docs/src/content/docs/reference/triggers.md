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
```

#### Accessing Inputs in Markdown

Use `${{ github.event.inputs.INPUT_NAME }}` expressions to access workflow_dispatch inputs in your markdown content:

```aw wrap
---
on:
  workflow_dispatch:
    inputs:
      topic:
        description: 'Research topic'
        required: true
        type: string
permissions:
  contents: read
safe-outputs:
  create-discussion:
---

# Research Assistant

Research the following topic: "${{ github.event.inputs.topic }}"

Provide a comprehensive summary with key findings and recommendations.
```

**Supported input types:**
- `string` - Free-form text input
- `boolean` - True/false checkbox
- `choice` - Dropdown selection with predefined options
- `environment` - Repository environment selector

### Scheduled Triggers (`schedule:`)

Run workflows on a recurring schedule using human-friendly expressions or [cron syntax](https://docs.github.com/en/actions/using-workflows/events-that-trigger-workflows#schedule).

**Fuzzy Scheduling (Recommended):**

To distribute workflow execution times and prevent load spikes, use fuzzy schedules that let the compiler automatically scatter execution times:

```yaml wrap
on:
  schedule: hourly  # Compiler scatters minute offset deterministically
```

```yaml wrap
on:
  schedule: daily  # Compiler scatters execution time deterministically
```

```yaml wrap
on:
  schedule:
    - cron: daily  # Each workflow gets a different scattered time
```

For workflows that need to run around a specific time (with some flexibility), use the `around` constraint:

```yaml wrap
on:
  schedule: daily around 14:00  # Scatters within ±1 hour (13:00-15:00)
```

For workflows that should only run during specific hours (like business hours), use the `between` constraint:

```yaml wrap
on:
  schedule: daily between 9:00 and 17:00  # Scatters within 9am-5pm range
```

```yaml wrap
on:
  schedule: daily between 9am and 5pm utc-5  # Business hours in EST timezone
```

The compiler deterministically assigns each workflow a unique execution time based on the workflow file path. This ensures:
- **Load distribution**: Workflows run at different times, reducing server load spikes
- **Consistency**: The same workflow always gets the same execution time across recompiles
- **Simplicity**: No need to manually coordinate schedules across multiple workflows
- **Flexibility with constraints**: Use `around` to hint preferred times or `between` to restrict to time ranges

> [!TIP]
> Complete Schedule Syntax Reference
> See the [Schedule Syntax reference](/gh-aw/reference/schedule-syntax/) for complete documentation of all supported schedule formats, including:
> - Fuzzy schedules (daily, hourly, weekly)
> - Time constraints (around, between)
> - Fixed schedules
> - Monthly and interval schedules
> - UTC offset support
> - Standard cron expressions

**Human-Friendly Format:**

```yaml wrap
on: daily  # Recommended: automatically scattered
```

```yaml wrap
on: weekly on monday  # Recommended: scattered time on Mondays
```

```yaml wrap
on: every 6h  # Run every 6 hours
```

**Fixed-Time Cron Format:**

For workflows that must run at a specific fixed time, use standard cron syntax:

```yaml wrap
on:
  schedule:
    - cron: "30 6 * * 1"  # Monday at 06:30 UTC
    - cron: "0 9 15 * *"  # 15th of month at 09:00 UTC
```

> [!TIP]
> Use Fuzzy Schedules
> Use fuzzy schedules like `daily`, `weekly`, `hourly`, or `every Nh` to automatically distribute execution times and avoid load spikes.

**Supported Formats:**

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

The human-friendly format is automatically converted to standard cron expressions, with the original format preserved as a comment in the generated workflow file.

**Standard Cron Format:**

```yaml wrap
on: weekly on monday
  stop-after: "+7d"      # Stop after a week
```

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

When enabled:
- The issue is **locked** at the start of the workflow (in the activation job)
- The issue is **unlocked** after workflow completion (in the conclusion job)
- If safe-outputs are configured, the issue is unlocked before safe output processing to allow comments/updates
- The unlock step runs with `always()` condition to ensure unlocking even if the workflow fails

**When to use `lock-for-agent`:**
- Workflows that make multiple sequential updates to an issue
- Preventing race conditions when multiple workflow runs might modify the same issue
- Ensuring consistent state during complex issue processing

**Requirements and behavior:**
- Requires `issues: write` permission (automatically added to activation and conclusion jobs)
- Pull requests are silently skipped (they cannot be locked via the issues API)
- Already-locked issues are skipped without error

**Example workflow:**
```aw wrap
---
on:
  issues:
    types: [opened]
    lock-for-agent: true
permissions:
  contents: read
  issues: write
safe-outputs:
  add-comment:
    max: 3
---

# Issue Processor with Locking

Process the issue and make multiple updates without interference
from concurrent modifications.

Context: "${{ needs.activation.outputs.text }}"
```

### Pull Request Triggers (`pull_request:`)

Trigger on pull request events. [Full event reference](https://docs.github.com/en/actions/using-workflows/events-that-trigger-workflows#pull_request).

```yaml wrap
on:
  pull_request:
    types: [opened, synchronize, labeled]
    names: [ready-for-review, needs-review]
  reaction: "rocket"
```

#### Fork Filtering (`forks:`)

Pull request workflows block forks by default for security. Use the `forks:` field to allow specific fork patterns:

```yaml wrap
on:
  pull_request:
    types: [opened, synchronize]
    forks: ["trusted-org/*"]  # Allow forks from trusted-org
```

**Available patterns:**
- `["*"]` - Allow all forks (use with caution)
- `["owner/*"]` - Allow forks from specific organization or user
- `["owner/repo"]` - Allow specific repository
- Omit `forks` field - Default behavior (same-repository PRs only)

The compiler uses repository ID comparison for reliable fork detection that is not affected by repository renames.

### Comment Triggers
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

#### Security Protections

Workflows with `workflow_run` triggers include automatic security protections:

**Automatic repository and fork validation:** The compiler automatically injects repository ID and fork checks to prevent cross-repository attacks and fork execution. This safety condition ensures workflows only execute when triggered by workflow runs from the same repository and not from forked repositories.

**Branch restrictions required:** Include `branches` to limit which branch workflows can trigger the event. Without branch restrictions, the compiler emits warnings (or errors in strict mode). This prevents unexpected execution for workflow runs on all branches.

See the [Security Architecture](/gh-aw/introduction/architecture/) for detailed security behavior and implementation.

### Command Triggers (`slash_command:`)

The `slash_command:` trigger creates workflows that respond to `/command-name` mentions in issues, pull requests, and comments. See [Command Triggers](/gh-aw/reference/command-triggers/) for complete documentation.

**Basic Configuration:**
```yaml wrap
on:
  slash_command:
    name: my-bot
```

**Shorthand Format (String):**
```yaml wrap
on:
  slash_command: "my-bot"
```

**Shorthand Format (Slash Command):**
```yaml wrap
on: /my-bot
```

This ultra-short syntax automatically expands to include `slash_command` and `workflow_dispatch` triggers, similar to how `on: daily` expands to include schedule and workflow_dispatch.

**With Event Filtering:**
```yaml wrap
on:
  slash_command:
    name: summarize
    events: [issues, issue_comment]  # Only in issue bodies and comments
```

**Complete Workflow Example:**
```aw wrap
---
on:
  slash_command:
    name: code-review
    events: [pull_request, pull_request_comment]
permissions:
  contents: read
  pull-requests: write
tools:
  github:
    toolsets: [pull_requests]
safe-outputs:
  add-comment:
    max: 5
timeout-minutes: 10
---

# Code Review Assistant

When someone mentions /code-review in a pull request or PR comment,
analyze the code changes and provide detailed feedback.

The current context is: "${{ needs.activation.outputs.text }}"

Review the pull request changes and add helpful review comments on specific
lines of code where improvements can be made.
```

The command must appear as the **first word** in the comment or body text. Command workflows automatically add the "eyes" (👀) reaction and edit comments with workflow run links.

> [!NOTE]
> Deprecated Syntax
> The `command:` trigger field is deprecated. Use `slash_command:` instead.

### Label Filtering (`names:`)

Filter issue and pull request triggers by label names using the `names:` field:

```yaml wrap
on:
  issues:
    types: [labeled, unlabeled]
    names: [bug, critical, security]
```

#### Shorthand Syntax

Use convenient shorthand for label-based triggers:

```yaml wrap
on: issue labeled bug
on: issue labeled bug, enhancement, priority-high  # Multiple labels
on: pull_request labeled needs-review, ready-to-merge
```

All shorthand formats compile to standard GitHub Actions syntax and automatically include the `workflow_dispatch` trigger. Supported for `issue`, `pull_request`, and `discussion` events. See [LabelOps workflows](/gh-aw/patterns/labelops/) for automation examples.

### Reactions (`reaction:`)

Enable emoji reactions on triggering items (issues, PRs, comments, discussions) to provide visual workflow status feedback:

```yaml wrap
on:
  issues:
    types: [opened]
  reaction: "eyes"
```

The reaction is added to the triggering item. For issues/PRs, a comment with the workflow run link is created. For comment events in command workflows, the comment is edited to include the run link.

**Available reactions:** `+1` 👍, `-1` 👎, `laugh` 😄, `confused` 😕, `heart` ❤️, `hooray` 🎉, `rocket` 🚀, `eyes` 👀

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

The `manual-approval` field sets the `environment` on the activation job, enabling manual approval gates configured in repository or organization settings. This provides human-in-the-loop control for sensitive operations.

The field accepts a string environment name that must match a configured environment in the repository. Configure approval rules, required reviewers, and wait timers in repository Settings → Environments. See [GitHub's environment documentation](https://docs.github.com/en/actions/deployment/targeting-different-environments/using-environments-for-deployment) for environment configuration details.

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

A pre-activation check runs the search query against the current repository. If matches reach or exceed the threshold (default `max: 1`), the workflow is skipped. The query is automatically scoped to the current repository and supports all standard GitHub search qualifiers (`is:`, `label:`, `in:title`, `author:`, etc.).

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

A pre-activation check runs the search query against the current repository. If matches are below the threshold (default `min: 1`), the workflow is skipped. Can be combined with `skip-if-match` for complex conditions. Supports all standard GitHub search qualifiers.

## Related Documentation

- [Schedule Syntax](/gh-aw/reference/schedule-syntax/) - Complete schedule format reference
- [Command Triggers](/gh-aw/reference/command-triggers/) - Special @mention triggers and context text
- [Frontmatter](/gh-aw/reference/frontmatter/) - Complete frontmatter configuration
- [LabelOps](/gh-aw/patterns/labelops/) - Label-based automation workflows
- [Workflow Structure](/gh-aw/reference/workflow-structure/) - Directory layout and organization
