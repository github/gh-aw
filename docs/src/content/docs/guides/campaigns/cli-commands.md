---
title: CLI commands (deprecated)
description: Historical reference for deprecated campaign CLI commands
banner:
  content: '<strong>⚠️ Deprecated:</strong> Campaign CLI commands for <code>.campaign.md</code> files are no longer supported. Use the <code>project</code> field in workflow frontmatter instead.'
---

:::caution[Commands removed]
The `gh aw campaign` commands described here operated on the deprecated `.campaign.md` file format, which has been removed.

**Current approach:** Use regular workflows with the `project` field in frontmatter. No special CLI commands needed.

See [Project Tracking](/gh-aw/examples/project-tracking/) for the current implementation.
:::

## Migration

Instead of using campaign CLI commands, create regular workflows with project tracking:

### Creating a workflow with project tracking

```bash
# Create a workflow file
vim .github/workflows/my-workflow.md
```

Add the `project` field to frontmatter:

```yaml
---
on:
  schedule:
    - cron: "0 0 * * *"
project: https://github.com/orgs/myorg/projects/1
safe-outputs:
  create-issue:
    max: 5
---

# My Workflow

Your workflow instructions...
```

### Compiling workflows

```bash
gh aw compile  # Compile all workflows
```

### Viewing workflows

```bash
gh aw status  # List all workflows
```

## See Also

- [Getting Started](/gh-aw/guides/campaigns/getting-started/) - Create workflows with project tracking
- [Project Tracking](/gh-aw/examples/project-tracking/) - Complete configuration reference
- [CLI Reference](/gh-aw/reference/cli/) - Current CLI commands
