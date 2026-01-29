---
name: Dependabot Burner
description: Burns down Dependabot security alert work items

on:
  #schedule: daily
  #skip-if-not-match: prnwith dependabot label
  workflow_dispatch:

permissions:
  issues: read
  pull-requests: read
  contents: read
  security-events: read

imports:
  - shared/campaign.md
---

# Dependabot Burner

{{#runtime-import aw/campaign.md}}

## Config

- Project URL: https://github.com/orgs/githubnext/projects/144
- Campaign ID: dependabot-burner
- Target repo: githubnext/gh-aw

## Task

### Discover work items

Find open Dependabot PRs in `githubnext/gh-aw`:
```
repo:githubnext/gh-aw is:pr author:app/dependabot label:dependencies label:javascript is:open
```

Follow `aw/campaign.md` (Budgets & Pacing) for limits and ordering.
If no PRs are found, follow `aw/campaign.md` (No-Work Default).

### Update project items

For each discovered PR, call `update_project` with:
- `project`: "<project_url>"
- `content_type`: "pull_request"
- `content_number`: the PR number
- `fields`: follow the defaults in `aw/campaign.md`
  - Override for this workflow: `worker_workflow`: "dependabot"

### Bundle dependabot PRs into issues

Create up to 3 bundle issues, each representing exactly **one runtime + one manifest file** (never mix runtimes; never mix manifests).

For each bundle:

1. Call `create_issue(...)` with a title like:
   - `[dependabot-burner] Dependabot bundle — <runtime> — <manifest> (YYYY-MM-DD)`
   Issue body should include:
   - Runtime + manifest
   - Checklist of PRs in the bundle
   - A short research section (breaking changes / migration notes / risk)

2. Capture the returned temporary ID, then immediately call `update_project(...)` to add that issue to the project:
   - `project`: "<project_url>"
   - `content_type`: "issue"
   - `content_number`: "<temporary_id>"
   - `fields`: follow the defaults in `aw/campaign.md` (Project Field Defaults)