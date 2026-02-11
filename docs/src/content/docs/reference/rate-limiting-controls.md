---
title: Rate Limiting Controls
description: Built-in protections to prevent runaway agentic workflows and exponential growth.
sidebar:
  order: 1450
---

GitHub Agentic Workflows uses defense-in-depth to prevent runaway workflows: bot non-triggering, concurrency controls, timeouts, rate limiting, read-only agents, safe output limits, built-in delays, and manual review gates.

## Bot Non-Triggering

The `github-actions[bot]` account does not trigger workflow events. When a workflow creates an issue or posts a comment via safe outputs, it won't trigger other workflows - preventing infinite loops.

```yaml wrap
on:
  issues:
    types: [opened]
```

This workflow won't be triggered by issues created by safe outputs.

## Concurrency Groups

Workflows use dual concurrency control: per-workflow (based on context) and per-engine (one agent job at a time per AI engine).

```yaml wrap
concurrency:
  group: gh-aw-${{ github.workflow }}

jobs:
  agent:
    concurrency:
      group: gh-aw-copilot
```

This prevents parallel execution explosions and AI resource exhaustion. See [Concurrency Control](/gh-aw/reference/concurrency/) for trigger-specific patterns.

## Timeouts

Jobs have maximum execution times (default: 360 minutes) controlled by the top-level `timeout-minutes` field:

```yaml wrap
timeout-minutes: 120  # Apply 120-minute timeout to all jobs
```

The `stop-after` field provides additional control for when workflows should stop running:

```yaml wrap
stop-after: +48h  # Stop after 48 hours from trigger
```

This evaluates in the agent job's `if:` condition, preventing execution if the time limit is exceeded. Supports absolute dates and relative time deltas (minimum unit is hours).

## Read-Only Agent Tokens

Agents run with read-only permissions. All write operations (creating issues, posting comments, triggering workflows) go through the [safe outputs system](/gh-aw/reference/safe-outputs/), which provides validation, auditing, and rate limiting.

```yaml wrap
permissions:
  contents: read
  issues: read
  pull-requests: read
```

## Safe Output Limits

High-risk operations have default max limits to prevent exponential growth:

| Operation | Default Max | Purpose |
|-----------|-------------|---------|
| `assign-to-agent` | 1 | Prevent agent cascades |
| `assign-to-bot` | 1 | Prevent bot loops |
| `dispatch-workflow` | 1 | Prevent workflow explosions |

```yaml wrap
safe-outputs:
  assign-to-agent:
    max: 3  # Override default if needed
```

Without limits, one workflow could spawn three agents, each spawning three more, creating exponential growth. The default max of 1 ensures linear progression.

## Built-In Delays

Critical operations have hardcoded, non-disableable delays:

- **Agent assignments**: 10-second delay between each assignment
- **Workflow dispatches**: 5-second delay between each dispatch

```javascript
// Agent assignment delay
await sleep(10000);  // 10 seconds

// Workflow dispatch delay  
await new Promise(resolve => setTimeout(resolve, 5000));  // 5 seconds
```

These prevent burst patterns and spread load over time.

## Manual Review Gates

Require manual approval for sensitive operations using GitHub Environments:

```yaml wrap
safe-outputs:
  dispatch-workflow:
    environment: production  # Requires approval
```

Configure environments in repository Settings → Environments, add reviewers, then reference the environment name. Use for production dispatches, cross-repo operations, or security-sensitive actions.

## Rate Limiting Per User

The `rate-limit` frontmatter field prevents users from triggering workflows too frequently:

```yaml wrap
rate-limit:
  max: 5        # Required: Maximum runs per window (1-10)
  window: 60    # Optional: Time window in minutes (default: 60, max: 180)
  events: [workflow_dispatch, issue_comment]  # Optional: Specific events (auto-inferred if omitted)
  ignored-roles: [admin, maintain]  # Optional: Roles exempt from rate limiting (default: [admin, maintain, write])
```

The pre-activation job checks recent runs and cancels the current run if the limit is exceeded.

**Role exemptions**: By default, users with `admin`, `maintain`, or `write` roles are exempt from rate limiting. To apply rate limiting to all users including admins, set `ignored-roles: []`.

## Example: Multiple Protection Layers

```yaml wrap
---
name: Safe Agent Workflow
engine:
  id: copilot
timeout-minutes: 60  # Job timeout
on:
  issues:
    types: [opened]
rate-limit:
  max: 5
  window: 60
stop-after: +2h  # Workflow time limit
safe-outputs:
  assign-to-agent:
    max: 1
    environment: production
---
```

This workflow combines: rate limiting (5/hour per user), concurrency control (one at a time), timeouts (60 min job, 2h workflow), manual approval (environment), and safe output limits (max 1 agent). The bot non-triggering and built-in delays provide additional protection.

## Best Practices

Start with conservative limits and increase as needed. Use environments for high-risk operations (workflow dispatches, cross-repo operations, production systems). Layer multiple controls: rate limiting with concurrency, timeouts with stop-after, safe output limits with environments. Monitor workflow runs, safe output logs, and rate limit cancellations to identify needed adjustments.

## Troubleshooting

**Workflow immediately cancelled**: Check rate limit in pre-activation logs, verify concurrency queue, or confirm stop-after hasn't exceeded.

**Agent assignments slow**: Built-in 10-second delays are intentional. Five agents = ~40 seconds total.

**Workflow dispatch not triggering**: Verify max dispatch limit (default: 1), check 5-second delay, confirm target workflow has `on: workflow_dispatch`, or check pending environment approvals.

## Related Documentation

- [Safe Outputs](/gh-aw/reference/safe-outputs/) - Write operations with validation
- [Concurrency Control](/gh-aw/reference/concurrency/) - Execution serialization
- [Frontmatter Reference](/gh-aw/reference/frontmatter/) - Complete configuration options
- [Permissions](/gh-aw/reference/permissions/) - Token scopes and access control
- [GitHub Actions Security](https://docs.github.com/en/actions/security-guides) - GitHub's security guidance
