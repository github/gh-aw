---
description: GitHub Projects (v2) integration patterns for agentic workflows
---

# GitHub Projects (v2) Integration

When designing workflows that manage GitHub Projects, use these patterns and best practices.

## When to Use Projects Integration

Use GitHub Projects safe-outputs when designing workflows that:

- Track issues and pull requests across multiple repositories
- Maintain project boards with automated status updates
- Generate periodic project status summaries
- Coordinate work across teams with consistent field updates

## Core Projects Patterns

- **Create projects with automation** - Use `gh aw project new --with-project-setup` for quick setup with standard views and fields (recommended)
- **Track items and fields** with `update-project` (add issue/PR items, create/update fields, optionally create views)
- **Post periodic run summaries** with `create-project-status-update` (status, dates, and a concise markdown summary)
- **Create new projects programmatically** with `create-project` safe-output (advanced; prefer CLI for initial setup)

## Prerequisites and Authentication

**Important**: Projects v2 requires a **PAT** or **GitHub App token** with Projects permissions. The default `GITHUB_TOKEN` cannot manage Projects v2.

- Always store the token in a repo/org secret (recommended: `GH_AW_PROJECT_GITHUB_TOKEN`) and reference it in safe-output config
- Always use the **full project URL** (example: `https://github.com/orgs/myorg/projects/42`)
- The agent must include `project` in **every** `update_project` / `create_project_status_update` output
- The configured `project` value is for documentation and validation only

## Workflow Configuration

**Frontmatter setup:**

```yaml
safe-outputs:
  update-project:
    project: "https://github.com/orgs/<ORG>/projects/<PROJECT_NUMBER>"  # required (replace)
    max: 20
    github-token: ${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}

  create-project-status-update:
    project: "https://github.com/orgs/<ORG>/projects/<PROJECT_NUMBER>"  # required (replace)
    max: 1
    github-token: ${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}

  # Optional: only enable if the workflow is allowed to create new projects
  # create-project:
  #   max: 1
  #   github-token: ${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}
  #   target-owner: "myorg"   # optional default owner
  #   title-prefix: "Project" # optional
```

**Notes:**
- Keep `max` small. For `create-project-status-update`, `max: 1` is almost always enough
- If you want the agent to read project metadata (fields/items) during reasoning, also configure `tools.github.toolsets: [projects]` with a token that has Projects access

## Agent Output Patterns

### 1. Add an issue/PR to a project and set fields

```javascript
update_project({
  project: "https://github.com/orgs/myorg/projects/42",
  content_type: "issue",
  content_number: 123,
  fields: {
    Status: "Todo",
    Priority: "High"
  }
})
```

### 2. Create a draft issue in the project

```javascript
update_project({
  project: "https://github.com/orgs/myorg/projects/42",
  content_type: "draft_issue",
  draft_title: "Triage: follow-up investigation",
  draft_body: "Short context and next steps.",
  fields: {
    Status: "Backlog"
  }
})
```

### 3. Post a project status update (run summary)

```javascript
create_project_status_update({
  project: "https://github.com/orgs/myorg/projects/42",
  status: "ON_TRACK",
  start_date: "2026-02-04",
  target_date: "2026-02-18",
  body: "## Run summary\n\n- Processed 12 items\n- Added 3 new issues to the board\n- Next: tackle 2 blocked tasks"
})
```

### 4. Create a new project (optional)

Prefer creating projects manually unless the workflow is explicitly intended to bootstrap new projects.

```javascript
create_project({
  title: "Project: Q1 reliability",
  owner: "myorg",
  owner_type: "org",
  item_url: "https://github.com/myorg/repo/issues/123"
})
```

## Design Guidelines

### Guardrails and Conventions

- **Single source of truth**: Store a concept (e.g., Target ship date) in exactly one place (a single field), not spread across multiple similar fields
- **Prefer small, stable field vocabularies**: Standardize field names like `Status`, `Priority`, `Sprint`, `Target date`. Avoid creating near-duplicates
- **Don't create projects implicitly**: For `update_project`, only set `create_if_missing: true` when the workflow is explicitly allowed to create/own project boards
- **Keep status updates tight**: 5-20 lines is usually plenty; use headings + short bullets
- **Use issues/PRs for detailed discussion**: Put deep context on the issue/PR; keep the project item fields for tracking/triage

### Project Management Best Practices

When designing how your workflow should manage a project board:

- **Communicate via issues and PRs**: Use assignees, @mentions, links between work items, and clear ownership. Let the project reflect the state, not replace conversation
- **Break down large work**: Prefer smaller issues and PRs; use sub-issues and dependencies so blockers are explicit; use milestones/labels to connect work to larger goals
- **Document the project**: Use the project description/README to explain purpose, how to use views, and who to contact. Use status updates for high-level progress
- **Use the right views**: Maintain a few views for the most common workflows (table for detail, board for flow, roadmap for timeline) and keep filters/grouping meaningful
- **Use field types intentionally**: Choose fields that power decisions (iteration, single-select status/priority, dates). Avoid redundant or low-signal metadata
- **Automate the boring parts**: Rely on built-in project workflows where possible; use GitHub Actions + GraphQL (via these safe outputs) for consistent updates
- **Visualize progress**: Consider charts/insights for trends (throughput, blocked items, work by status/iteration) and share them with stakeholders
- **Standardize with templates**: If multiple teams/projects follow the same process, prefer templates with prebuilt views/fields
- **Link to teams and repos**: Connect projects to the team and/or repo for discoverability and consistent access
- **Have a single source of truth**: Track important metadata (dates, status) in one place so updates don't drift

## References

- For full configuration options, see: [github-agentic-workflows.md](https://github.com/github/gh-aw/blob/main/.github/aw/github-agentic-workflows.md)
- For safe-outputs documentation, see the `safe-outputs:` section in the main configuration guide
- GitHub Projects best practices: https://docs.github.com/en/issues/planning-and-tracking-with-projects/learning-about-projects/best-practices-for-projects
