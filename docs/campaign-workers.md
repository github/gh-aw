# Campaign Workers (Deprecated)

:::caution[Deprecated]
The campaign worker pattern described in this document was designed for the deprecated `.campaign.md` file format. 

**Current approach:** Use regular workflows with the `project` field in frontmatter for project tracking.

See [Project Tracking](/docs/src/content/docs/examples/project-tracking.md) for the current implementation.
:::

## Migration Guide

The complex orchestrator + worker pattern has been replaced with a simpler approach:

### Before (Orchestrator + Workers)

Complex setup with:
- Campaign spec files (`.campaign.md`)
- Orchestrator workflows (generated)
- Worker workflows (dispatch-only with strict input contracts)
- Discovery precomputation scripts

### After (Workflows with Project Tracking)

Simple setup with:
- Regular workflows with `project` field in frontmatter
- Direct project integration via safe-outputs
- No orchestrator needed
- Workflows operate independently

## Example Migration

### Old Worker Pattern

```yaml
---
name: Security Fix Worker
on:
  workflow_dispatch:
    inputs:
      campaign_id:
        required: true
      payload:
        required: true
---

# Parse campaign_id and payload...
# Check for existing work via deterministic key...
# Create PR with campaign label...
```

### New Simplified Approach

```yaml
---
on:
  schedule:
    - cron: "0 0 * * *"
project: https://github.com/orgs/myorg/projects/1
safe-outputs:
  create-pull-request:
    max: 5
---

# Security Fix Worker

Scan for security issues and create fix PRs.

1. Scan for vulnerabilities
2. Create PR with fix
3. PR is automatically added to project board
```

## Key Simplifications

1. **No campaign_id input** - Not needed, workflows are independent
2. **No payload parsing** - Workflows get their data directly from GitHub
3. **No orchestrator** - Workflows can run on their own schedule
4. **Simpler idempotency** - Use GitHub's built-in duplicate detection
5. **Direct project integration** - No special labels or discovery needed

## See Also

- [Project Tracking Example](/docs/src/content/docs/examples/project-tracking.md)
- [Getting Started Guide](/docs/src/content/docs/guides/campaigns/getting-started.md)
- [Safe Outputs Reference](/docs/src/content/docs/reference/safe-outputs.md)
