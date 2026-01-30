---
title: Getting started
description: Quick start guide for creating campaign workflows
banner:
  content: '<strong>⚠️ Experimental:</strong> This feature is under active development and may change or behave unpredictably.'
---

import { LinkCard } from '@astrojs/starlight/components';

<LinkCard
  title="Quick Start - See Campaign Overview"
  description="The quick start guide has moved to the main Campaigns page"
  href="/gh-aw/guides/campaigns/"
/>

## Advanced Configuration

This page provides additional details for campaign workflows beyond the [Quick Start](/gh-aw/guides/campaigns/#quick-start).

## Best practices

- **Use imports** - Include `shared/campaign.md` for standard agent coordination
- **Define campaign ID** - Include a clear Campaign ID in your workflow
- **Specify project URL** - Document the GitHub Projects board URL
- **Test manually** - Use `workflow_dispatch` trigger to test before scheduling
- **Monitor progress** - Check your project board to see tracked items

## Agent coordination details

The `imports: [shared/campaign.md]` provides:

- **Safe-output defaults**: Pre-configured limits for project operations
- **Execution phases**: Discover → Decide → Write → Report
- **Best practices**: Deterministic execution, pagination budgets, cursor management
- **Project integration**: Standard field mappings and status updates

## Example: Dependabot Burner (the smallest useful campaign)

See the [Dependabot Burner](https://github.com/githubnext/gh-aw/blob/main/.github/workflows/dependabot-burner.md) workflow - **the smallest useful campaign** - for a complete example:

- Discovers open Dependabot PRs
- Creates bundle issues for upgrades
- Tracks everything in a GitHub Project
- Runs daily with smart conditional execution

## Next Steps

- [Campaign Overview](/gh-aw/guides/campaigns/) - Main campaigns documentation
- [Campaign Examples](/gh-aw/examples/campaigns/) - Worker patterns and idempotency
- [Project Tracking Example](/gh-aw/examples/project-tracking/) - Complete configuration reference
- [Safe Outputs](/gh-aw/reference/safe-outputs/) - Available project operations
- [Trigger Events](/gh-aw/reference/triggers/) - Workflow trigger options
