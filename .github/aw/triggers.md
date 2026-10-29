---
description: Trigger patterns for GitHub Agentic Workflows — events, fuzzy scheduling, fork security, slash commands, and label commands.
---

## Trigger Selection

Use the smallest trigger that matches the request.

### Decision Matrix

| User intent | Trigger | Typical read tools | Typical safe output |
|---|---|---|---|
| Review PR changes, comment on quality, suggest fixes | `pull_request` | `github` (`gh-proxy`), optional `playwright` for UI diffs | `add-comment` |
| Investigate failed CI/Actions runs and summarize incident | `workflow_run` | `github` (`gh-proxy`) with `actions: read` | `create-issue` |
| Monitor external service deployment failures (Heroku, Vercel, Fly.io) | `deployment_status` | `github` (`gh-proxy`) with `deployments: read` | `create-issue` |
| Run visual regression checks on PR UI changes | `pull_request` | `playwright` + `cache-memory` | `add-comment` |
| Publish weekly stakeholder/product digest | `schedule` | `github` (`gh-proxy`) | `create-issue` (default), `create-discussion` only if explicitly requested |
| Review dependency licenses or design-token governance on PRs | `pull_request` with `paths:` | `github` (`gh-proxy`) | `add-comment`; `create-issue` only for blocked/policy-violating findings |
| Govern documentation content (stale pages, broken links, outdated ownership) | `schedule` or `pull_request` with `paths:` | `github` (`gh-proxy`) | `add-comment` on PR; `create-issue` for stale content |
| PM / product health digest (release velocity, open issues by area) | `schedule` | `github` (`gh-proxy`) | `create-issue` with `close-older-issues: true` |
| Compliance or regulatory review | `pull_request` with `paths:` or `schedule` | `github` (`gh-proxy`) | `add-comment` for findings; `create-issue` for violations |

> **`workflow_run` vs `deployment_status`**: Use `workflow_run` when monitoring another GitHub Actions workflow in the same repository. Use `deployment_status` when an external service (Heroku, Vercel, Fly.io) reports deployment results back to GitHub via the Deployments API. See [deployment-status.md](deployment-status.md) for the full pattern.
>
> For `workflow_run`, always scope explicitly: set `workflows:` to named upstream workflow(s), use `types: [completed]`, and gate outcomes with an `if:` guard on `${{ github.event.workflow_run.conclusion }}` (for incident triage, usually `failure`, `timed_out`, `cancelled`, `action_required`) unless the user asked for success reporting.

### Scenario Examples

The matrix above gives trigger, tools, and output. These add the `paths:` scoping and definitions the matrix cannot express.

Engineering-focused:

- **Schema/API review on PRs**: `paths:` scoped to backend contract files (`db/migrate/**`, `migrations/**`, `schema/**`, `openapi/**`, `api/**`); `noop` when contracts are unchanged.
- **Visual regression on UI changes**: use only `playwright` + `cache-memory` (no extra tools), allowlist only target preview/app hosts, and state the exact baseline source (`cache-memory` key, artifact, or branch path).
- **Design-token governance on PRs**: `paths:` scoped to token sources and theme/config files (`tokens/**`, `**/*tokens*.json`, `**/theme/**`, `**/tailwind*.{js,ts}`, `**/*design-token*`); validate linked token references (style dictionary source, token registry, or design-spec URL) before assessment; `noop` when required references are absent from the in-scope changes or no token drift is detected.
- **Dependency-license compliance review on PRs**: `paths:` scoped to dependency manifests (`package.json`, `go.mod`, `requirements.txt`, `Cargo.toml`); classify each addition by license tier (allowed / needs-review / blocked); escalate blocked additions with `create-issue`; `noop` when all additions are pre-approved.
- **Deployment incident triage**: `deployment_status` for external provider failures, `workflow_run` for GitHub Actions failures; derive a stable failure key (workflow + job + failing step or error signature); `noop` when a failure self-recovers or matches an existing open incident.

Non-engineering personas:

- **Documentation governance**: `schedule` (weekly) or `pull_request` with `paths:` scoped to docs directories; check stale ownership, broken links, and missing metadata; `create-issue` for pages needing owner action.
- **PM / roadmap / stakeholder digests**: `schedule` (weekly on weekdays) plus optional `workflow_dispatch`; publish with `create-issue` and `close-older-issues: true`. Fix up front: the window (`last 7 full days ending at run start (UTC)` or `since previous successful run`), the grouping dimensions (team, service, owner, severity, or status), and a stable dedup key (`pm-digest:<scope>:2026-W27`). `noop` when the window has zero qualifying updates. See [report.md](report.md) for the canonical defaults.
- **Compliance audit / review**: `schedule: daily on weekdays` plus `workflow_dispatch` for drift detection, or `schedule` (monthly) / `pull_request` with `paths:` scoped to policy files. Define **material drift** as any control-state change that affects compliance posture (required policy file removed, control owner missing, control status downgraded from pass to fail, or required approval evidence link missing). Compare current control evidence against the previous successful run window; publish with `create-issue` using `close-older-issues: true` and a stable key such as `compliance-drift:<framework>:<window-id>`; `noop` when no material drift is detected.

### Pattern-specific `noop` examples

- **PR reviewer (`pull_request`)**: `noop` when only docs/metadata changed outside scoped `paths:`.
- **Failure triage (`workflow_run`)**: `noop` when rerun succeeds, signal is flake-only, or an open incident already exists for the same failure key.
- **Scheduled digest (`schedule`)**: `noop` when the exact reporting window (for example `since previous successful run`) has zero qualifying updates.
- **Deployment monitor (`deployment_status`)**: `noop` when non-terminal statuses (`queued`, `in_progress`) arrive without a terminal failure.

## Trigger Patterns

### Standard GitHub Events

```yaml
on:
  issues:
    types: [opened, edited, closed]
  pull_request:
    types: [opened, edited, closed]
    forks: ["*"]              # Allow from all forks (default: same-repo only)
  push:
    branches: [main]
  schedule:
    - cron: "0 9 * * 1"  # Monday 9AM UTC
  workflow_dispatch:    # Manual trigger
```

### `workflow_run` Failure-Triage Pattern

Use this when reacting to failures from another workflow in the same repository:

```yaml
on:
  workflow_run:
    workflows: ["CI", "Deploy"]
    types: [completed]
  workflow_dispatch:
```

Then gate analysis to failure outcomes:

```yaml
if: contains(fromJson('["failure","timed_out","cancelled","action_required"]'), github.event.workflow_run.conclusion)
```

These are "non-success outcomes requiring triage"; keep the list explicit so readers can tighten (e.g., only `failure`) or broaden it.

Escalation rules for this pattern (required):

- Derive a stable failure key before any write (for example `<workflow>:<job>:<step>:<error-signature>`). See [create-agentic-workflow-trigger-details.md](create-agentic-workflow-trigger-details.md#incident-dedup-key-templates-workflow_run-and-deployment_status) for concrete key-format templates.
- Search for an existing open incident by that key **before** calling `create-issue`.
- `noop` when the monitored run concludes `success`, or when an open incident already exists for the same key (duplicate suppression).

#### Fuzzy Scheduling

Use fuzzy scheduling instead of exact cron to distribute execution times. Avoids load spikes and the "Monday wall of work" from weekend accumulation.

**Basic Fuzzy Schedules:**

```yaml
on:
  schedule: daily on weekdays    # Monday-Friday only (recommended for daily workflows)
  schedule: daily                # All 7 days
  schedule: weekly               # Once per week
  schedule: hourly               # Every hour
```

**Examples with Intervals:**

```yaml
on:
  schedule: every 2 hours on weekdays    # Every 2 hours, Monday-Friday
  schedule: every 6 hours                # Every 6 hours, all days
```

The compiler converts fuzzy schedules to deterministic cron (e.g., `daily on weekdays` → `43 5 * * 1-5`), scatters execution to avoid load spikes, and adds `workflow_dispatch:` for manual runs.

**Recommended Pattern:**

```yaml
# ✅ GOOD - Weekday schedule avoids Monday wall of work
on:
  schedule: daily on weekdays

# ⚠️ ACCEPTABLE - But may create Monday backlog
on:
  schedule: daily
```

#### Fork Security for Pull Requests

By default, `pull_request` triggers **block all forks** and only allow PRs from the same repository. Use the `forks:` field to explicitly allow forks:

```yaml
# Default: same-repo PRs only (forks blocked)
on:
  pull_request:
    types: [opened]

# Allow all forks
on:
  pull_request:
    types: [opened]
    forks: ["*"]

# Allow specific fork patterns
on:
  pull_request:
    types: [opened]
    forks: ["trusted-org/*", "trusted-user/repo"]
```

`pull_request_target` supports the same `forks:` field and defaults to same-repository PRs only (omitting `forks:` is equivalent to `forks: []`). List exact `owner/repo` or `owner/*` entries to admit specific forks; `"*"` anywhere in the list allows all forks and triggers a strict-mode warning recommending a narrower pattern. `acknowledge-risk: true` and disabled/trusted checkout do not suppress this warning — prefer `pull_request` and avoid `pull_request_target` fork admission unless base-repository context is required.

### Command Triggers (/mentions)

```yaml
on:
  slash_command:
    name: my-bot  # Responds to /my-bot in issues/comments
```

This automatically creates conditions to match `/my-bot` mentions in issue bodies and comments.

You can restrict where commands are active using the `events:` field:

```yaml
on:
  slash_command:
    name: my-bot
    events: [issues, issue_comment]  # Only in issue bodies and issue comments
```

**Supported event identifiers:**

- `issues` - Issue bodies (opened, edited, reopened)
- `issue_comment` - Comments on issues only (excludes PR comments)
- `pull_request_comment` - Comments on pull requests only (excludes issue comments)
- `pull_request` - Pull request bodies (opened, edited, reopened)
- `pull_request_review_comment` - Pull request review comments
- `*` - All comment-related events (default)

**Note**: `issue_comment` and `pull_request_comment` both map to GitHub Actions' `issue_comment` event with filtering to distinguish them.

### Label Command Triggers

Trigger workflows when specific labels are added to issues, PRs, or discussions:

```yaml
# Shorthand: trigger on any labeled event
on: label-command my-label

# Or with explicit configuration
on:
  label_command:
    name: ai-review        # Single label name (or use names: [...] for multiple)
    events: [pull_request] # Optional: restrict to issues, pull_request, discussion (default: all three)
    strategy: decentralized # Optional: route labeled events via generated agentic_commands.yml
    remove_label: false    # Optional: remove triggering label after activation (default: true)
```

Use `names:` for multiple labels that activate the same workflow:

```yaml
on:
  label_command:
    names: [ai-review, copilot-review]
    events: [pull_request]
```

By default, the triggering label is automatically removed after the workflow activates (`remove_label: true`). Set `remove_label: false` to keep the label.

The activated label name is exposed to downstream jobs as `${{ needs.activation.outputs.label_command }}`.

### Semi-Active Agent Pattern

```yaml
on:
  schedule:
    - cron: "0/10 * * * *"  # Every 10 minutes
  issues:
    types: [opened, edited, closed]
  issue_comment:
    types: [created, edited]
  pull_request:
    types: [opened, edited, closed]
  push:
    branches: [main]
  workflow_dispatch:
```

### All You Can Eat Pattern

```yaml
on:
  schedule: every 30 minutes
  skip-if-match: 'is:issue is:open "gh-aw-workflow-id: my-workflow" in:body'
```

A frequent schedule whose activation is skipped while the previous output of the same workflow is still open, so the next item is produced as soon as the user closes the last one. See [All You Can Eat Pattern](workflow-patterns.md#all-you-can-eat-pattern).
