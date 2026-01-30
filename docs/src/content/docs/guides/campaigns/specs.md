---
title: Campaign Specs (removed)
description: Campaign specs no longer exist - use regular workflows instead
banner:
  content: '<strong>⚠️ Removed:</strong> Campaign specs no longer exist. Campaigns are now regular workflows with optional <code>project</code> and <code>imports</code> fields.'
---

:::caution[Feature removed]
The `.campaign.md` standalone file format and campaign specs have been **removed** from gh-aw.

**Current approach:** Campaigns are regular workflows that optionally use:
- `project:` field for GitHub Projects tracking
- `imports: [shared/campaign.md]` for standard orchestration patterns

See [Campaign Orchestration](/gh-aw/guides/campaigns/) for the current implementation.
:::

## See Also

- [Campaign Orchestration](/gh-aw/guides/campaigns/) - Overview and examples
- [Getting Started](/gh-aw/guides/campaigns/getting-started/) - Quick start tutorial
- [Project Tracking](/gh-aw/examples/project-tracking/) - Complete guide
- [Safe Outputs](/gh-aw/reference/safe-outputs/) - Project operations reference
