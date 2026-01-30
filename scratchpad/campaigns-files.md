# Campaign Files Architecture (Deprecated)

:::caution[Deprecated]
This document describes the deprecated `.campaign.md` file format and orchestrator pattern, which has been removed from gh-aw.

**Current implementation:** Use the `project` field in workflow frontmatter for project tracking.

See `/docs/src/content/docs/examples/project-tracking.md` for the current approach.
:::

## What Changed

The complex campaign architecture (campaign specs, orchestrators, discovery scripts, worker coordination) has been replaced with a simpler approach:

### Old Architecture (Deprecated)

```
.github/workflows/
├── <campaign-id>.campaign.md       # Campaign spec
├── <campaign-id>.campaign.g.md     # Generated orchestrator (debug)
└── <campaign-id>.campaign.lock.yml # Compiled orchestrator

actions/setup/js/
└── campaign_discovery.cjs          # Discovery script

Orchestrator workflow:
- Discovery precomputation step
- Agent coordinates workers
- Dispatches workers via workflow_dispatch
- Workers report back via labels
```

### New Architecture (Current)

```
.github/workflows/
├── my-workflow.md       # Regular workflow with project field
└── my-workflow.lock.yml # Compiled workflow

Workflow:
- Regular workflow with project field in frontmatter
- Direct project integration via safe-outputs
- No orchestrator or discovery scripts needed
```

## Migration Example

### Before

```yaml
# .github/workflows/security-q1.campaign.md
---
id: security-q1
project-url: https://github.com/orgs/myorg/projects/1
workflows:
  - security-scanner
  - security-fixer
governance:
  max-project-updates-per-run: 20
---

# Security Q1 Campaign
...
```

Plus:
- Orchestrator workflow (generated)
- Discovery script
- Worker input contracts
- Campaign labels

### After

```yaml
# .github/workflows/security-scanner.md
---
on:
  schedule:
    - cron: "0 0 * * *"
project: https://github.com/orgs/myorg/projects/1
safe-outputs:
  create-issue:
    max: 10
---

# Security Scanner
...
```

That's it. No orchestrator, no discovery script, no campaign labels.

## Why the Change

1. **Simpler** - One concept (workflow with project) vs many (campaign, orchestrator, workers, discovery)
2. **More maintainable** - Less generated code, fewer moving parts
3. **More flexible** - Workflows can operate independently or in coordination
4. **Easier to understand** - Direct project integration is clearer than discovery + orchestration

## Historical Context

This file documented the internal architecture of the campaign system, including:
- Campaign discovery process
- Orchestrator generation
- Discovery script architecture
- Worker coordination patterns
- Cursor persistence
- Lock file naming conventions

All of this complexity has been replaced with the simpler `project` field approach.

## See Also

- [Project Tracking Documentation](/docs/src/content/docs/examples/project-tracking.md)
- [Getting Started Guide](/docs/src/content/docs/guides/campaigns/getting-started.md)
