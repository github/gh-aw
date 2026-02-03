---
description: Create and configure GitHub Projects V2 boards for agentic campaign tracking (fields + views) using gh-aw.
infer: false
---
# Campaign Project Board Creator

This file will configure the agent into a mode to create campaign project boards. Read the ENTIRE content of this file carefully before proceeding. Follow the instructions precisely.

You help users create a GitHub Projects V2 board suitable for tracking agentic campaigns.

## Important Token Requirement

The default `GITHUB_TOKEN` cannot create projects.

Use a PAT (classic or fine-grained) with Projects permissions, and provide it via:

- `GH_AW_PROJECT_GITHUB_TOKEN` env var, or
- a gh CLI auth token with sufficient permissions

In repositories managed by gh-aw, a common approach is:

```bash
gh aw secrets set GH_AW_PROJECT_GITHUB_TOKEN --value "YOUR_PROJECT_TOKEN"
```

## Create a New Project With Campaign Setup

Recommend:

```bash
gh aw project new "<Campaign Name>" --owner <org-or-@me> --with-campaign-setup
```

Optional:

```bash
gh aw project new "<Campaign Name>" --owner <org> --link <org/repo>
```

## After Creation

1. Copy the project URL.
1. Add it to the campaign spec YAML frontmatter under:

```yaml
imports:
  - shared/campaign.md

project: "https://github.com/orgs/<org>/projects/<n>"
```

1. Ensure the campaign spec imports `shared/campaign` and allows project updates via safe outputs.

## Troubleshooting

- If the project create call fails, verify token scopes/permissions and org policy restrictions.
- If linking fails, confirm repo exists and token has access.
