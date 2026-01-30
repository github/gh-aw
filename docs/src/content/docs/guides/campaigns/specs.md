---
title: Campaign specs (deprecated)
description: Historical reference for deprecated .campaign.md file format
banner:
  content: '<strong>⚠️ Deprecated:</strong> The <code>.campaign.md</code> file format is no longer supported. Use the <code>project</code> field in workflow frontmatter instead.'
---

:::caution[File format removed]
The `.campaign.md` standalone file format described in this document has been **removed** from gh-aw.

**Current approach:** Use the `project` field in regular workflow frontmatter for project tracking.

See [Project Tracking](/gh-aw/examples/project-tracking/) for the current implementation.
:::

## Migration Guide

If you have existing `.campaign.md` files, migrate to regular workflows with the `project` field:

### Before (deprecated .campaign.md)

```yaml
---
id: security-q1
name: "Security Q1 2025"
project-url: "https://github.com/orgs/myorg/projects/1"
workflows:
  - security-scanner
  - dependency-updater
governance:
  max-project-updates-per-run: 20
---

# Security Campaign

Campaign objectives and KPIs...
```

### After (workflow with project field)

```yaml
---
on:
  schedule:
    - cron: "0 0 * * *"
project:
  url: https://github.com/orgs/myorg/projects/1
  max-updates: 20
safe-outputs:
  create-issue:
    max: 5
---

# Security Scanner

Scan for security issues and track in project board.

1. Scan for security vulnerabilities
2. Create issues for findings
3. Issues are automatically added to the project
```

## Key Changes

1. **No separate .campaign.md files** - Use regular `.md` workflow files
2. **Simpler configuration** - Just add `project` field to frontmatter
3. **No orchestrator generation** - Each workflow operates independently
4. **Direct project integration** - Workflows update projects directly via safe-outputs

## See Also

- [Project Tracking](/gh-aw/examples/project-tracking/) - Complete guide
- [Getting Started](/gh-aw/guides/campaigns/getting-started/) - Quick start tutorial
- [Safe Outputs](/gh-aw/reference/safe-outputs/) - Project operations reference
