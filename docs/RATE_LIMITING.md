# Rate Limiting for Agentic Workflows

## Overview

The rate limiting feature prevents users from triggering workflows too frequently, helping to:
- Prevent abuse and resource exhaustion
- Control costs from programmatic workflow triggers
- Protect against accidental infinite loops
- Ensure fair resource allocation across users

## Configuration

Rate limiting is configured in the workflow frontmatter using the `rate-limit` field:

```yaml
---
name: My Workflow
engine: copilot
on:
  workflow_dispatch:
  issue_comment:
    types: [created]
rate-limit:
  max: 5          # Required: 1-10 runs
  window: 60      # Optional: minutes (default 60, max 180)
  # events field is optional - automatically inferred from 'on:' triggers
---
```

## Parameters

### `max` (integer, **required**)
- Maximum number of workflow runs allowed per user within the time window
- Range: 1-10
- Example: `max: 5` allows 5 runs per window

### `window` (integer, optional)
- Time window in minutes for rate limiting
- Default: 60 (1 hour)
- Range: 1-180 (up to 3 hours)
- Example: `window: 30` creates a 30-minute window

### `events` (array, optional)
- Specific event types to apply rate limiting to
- **If not specified, automatically inferred from the workflow's `on:` triggers**
- Only programmatic trigger types are included in the inference
- Can be explicitly set to override the inference
- Supported events:
  - `workflow_dispatch`
  - `issue_comment`
  - `pull_request_review`
  - `pull_request_review_comment`
  - `issues`
  - `pull_request`
  - `discussion_comment`
  - `discussion`

## How It Works

1. **Pre-Activation Check**: Rate limiting is enforced in the pre-activation job, before the main workflow runs
2. **Per-User Per-Workflow**: Limits are applied individually for each user and workflow
3. **Recent Runs Query**: The system queries recent workflow runs from the GitHub API
4. **Filtering**: Runs are filtered by:
   - Actor (user who triggered the workflow)
   - Time window (only runs within the configured window)
   - Event type (if `events` is configured)
   - Excludes the current run from the count
   - Excludes cancelled runs (cancelled runs don't count toward the limit)
   - Excludes runs that completed in less than 15 seconds (treated as failed fast/cancelled)
5. **Progressive Aggregation**: Uses pagination with short-circuit logic for efficiency
6. **Automatic Cancellation**: If the limit is exceeded, the current run is automatically cancelled

## Examples

### Automatic Event Inference (Recommended)
```yaml
on:
  issues:
    types: [opened]
  issue_comment:
    types: [created]
rate-limit:
  max: 5
  window: 60
  # Events automatically inferred: [issues, issue_comment]
```
Events are automatically inferred from the workflow's triggers. Simplest configuration.

### Basic Rate Limiting (Default Window)
```yaml
rate-limit:
  max: 5
  window: 60
```
Allows 5 runs per hour. Events inferred from `on:` section.

### Explicit Event Filtering
```yaml
rate-limit:
  max: 3
  window: 30
  events: [workflow_dispatch, issue_comment]
```
Explicitly specify events to override inference. Allows only 3 runs per 30 minutes for the specified events.

### Generous Rate Limiting
```yaml
rate-limit:
  max: 10
  window: 120
```
Allows 10 runs per 2 hours. Events inferred from triggers.

## Behavior Details

### When Rate Limit is Exceeded
- The workflow run is automatically cancelled
- A warning message is logged with details:
  - Current run count
  - Maximum allowed
  - Time window
- The activation output is set to false, preventing the main job from running

### Logging
The rate limit check provides extensive logging:
```
🔍 Checking rate limit for user 'username' on workflow 'workflow-name'
   Configuration: max=5 runs per 60 minutes
   Current event: workflow_dispatch
   Time window: runs created after 2026-02-11T11:24:33.098Z
📊 Querying workflow runs for 'workflow-name'...
   Fetching page 1 (up to 100 runs per page)...
   Retrieved 10 runs from page 1
   Skipping run 123457 - cancelled (status: cancelled)
   ✓ Run #5 (123456) by username - event: workflow_dispatch, created: 2026-02-11T11:15:00.000Z, status: completed
📈 Rate limit summary for user 'username':
   Total recent runs in last 60 minutes: 3
   Maximum allowed: 5
   Breakdown by event type:
   - workflow_dispatch: 2 runs
   - issue_comment: 1 runs
✅ Rate limit check passed
   User 'username' has 3 runs in the last 60 minutes
   Remaining quota: 2 runs
```

### Error Handling
- **Fail-Open**: If the rate limit check fails due to API errors, the workflow is allowed to proceed
- This ensures that temporary API issues don't block legitimate workflow runs
- Errors are logged with details for troubleshooting

### Performance Optimization
- **Short-Circuit Logic**: Stops querying additional pages once the limit is reached
- **Progressive Filtering**: Filters by actor and time window progressively
- **Pagination**: Efficiently handles workflows with many runs

## Integration with Pre-Activation Job

The rate limit check is automatically added to the pre-activation job when configured:

```yaml
jobs:
  pre-activation:
    runs-on: ubuntu-latest
    outputs:
      activated: ${{ (steps.check_membership.outputs.is_team_member == 'true') && (steps.check_rate_limit.outputs.rate_limit_ok == 'true') }}
    steps:
      - name: Check team membership
        # ... membership check ...
      
      - name: Check user rate limit
        id: check_rate_limit
        uses: actions/github-script@v8
        env:
          GH_AW_RATE_LIMIT_MAX: "5"
          GH_AW_RATE_LIMIT_WINDOW: "60"
          GH_AW_RATE_LIMIT_EVENTS: workflow_dispatch,issue_comment
        with:
          github-token: ${{ secrets.GITHUB_TOKEN }}
          script: |
            const { main } = require('/opt/gh-aw/actions/check_rate_limit.cjs');
            await main();
```

The activation output combines all pre-activation checks using AND logic, so the workflow only proceeds if all checks pass.

## Use Cases

### Preventing Abuse
```yaml
rate-limit:
  max: 3
  window: 60
  events: [workflow_dispatch]
```
Limits manual workflow triggers to prevent spam or abuse.

### Cost Control
```yaml
rate-limit:
  max: 10
  window: 120
```
Controls costs by limiting how often expensive workflows can be triggered.

### Fair Resource Allocation
```yaml
rate-limit:
  max: 5
  window: 30
```
Ensures fair access to shared resources across multiple users.

### Development vs Production
Development workflows might have stricter limits:
```yaml
# Development
rate-limit:
  max: 3
  window: 30

# Production
rate-limit:
  max: 20
  window: 60
```

## Testing

A test workflow is provided at `.github/workflows/test-rate-limit.md`:

```yaml
---
name: Test Rate Limiting
engine: copilot
on:
  workflow_dispatch:
  issue_comment:
    types: [created]
rate-limit:
  max: 3
  window: 30
  events: [workflow_dispatch, issue_comment]
---

Test workflow to demonstrate rate limiting functionality.
This workflow limits each user to 3 runs within a 30-minute window.
```

To test:
1. Trigger the workflow manually 4 times in quick succession
2. The 4th run should be automatically cancelled with a rate limit warning
3. Wait 30 minutes for the window to reset
4. Trigger again to confirm the limit resets

## Troubleshooting

### Rate Limit Not Working
- Check that `rate-limit` is in the workflow frontmatter
- Verify the schema is valid (run `gh aw compile`)
- Check pre-activation job logs for rate limit check output

### Unexpected Cancellations
- Review the rate limit configuration (`max` and `window`)
- Check if other users are triggering the same workflow
- Verify event filters are configured correctly

### API Errors
- Rate limit checks fail-open on API errors
- Check GitHub API status if issues persist
- Review workflow run logs for detailed error messages

## Schema Definition

The rate-limit field is validated against this JSON schema:

```json
{
  "type": "object",
  "required": ["max"],
  "properties": {
    "max": {
      "type": "integer",
      "minimum": 1,
      "maximum": 10
    },
    "window": {
      "type": "integer",
      "minimum": 1,
      "maximum": 180,
      "default": 60
    },
    "events": {
      "type": "array",
      "items": {
        "type": "string",
        "enum": [
          "workflow_dispatch",
          "issue_comment",
          "pull_request_review",
          "pull_request_review_comment",
          "issues",
          "pull_request",
          "discussion_comment",
          "discussion"
        ]
      },
      "minItems": 1
    }
  }
}
```
