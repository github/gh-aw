---
# Shared: GitHub Projects (v2)
#
# Import this file in workflows that manage GitHub Projects via safe-outputs.
# This file is intentionally instruction-only (no config is applied automatically).
#
# Related safe-outputs:
# - update-project               (agent output: update_project)
# - create-project-status-update (agent output: create_project_status_update)
# - create-project               (agent output: create_project)
---

## GitHub Projects (v2): safe-output patterns

Use these patterns when a workflow should keep a GitHub Project up-to-date:

- **Track items and fields** with `update-project` (add issue/PR items, create/update fields, optionally create views).
- **Post periodic run summaries** with `create-project-status-update` (status, dates, and a concise markdown summary).
- **Create new projects** with `create-project` (optional; prefer manual creation unless automation is explicitly desired).

### Import this shared file

Add this to the importing workflow’s frontmatter:

```yaml wrap
imports:
	- shared/projects.md
```

### Prerequisites (read this first)

- Projects v2 requires a **PAT** or **GitHub App token** with Projects permissions. The default `GITHUB_TOKEN` cannot manage Projects v2.
- Always store the token in a repo/org secret (recommended: `GH_AW_PROJECT_GITHUB_TOKEN`) and reference it in safe-output config.
- Always use the **full project URL** (example: `https://github.com/orgs/myorg/projects/42`).
- The agent must include `project` in **every** `update_project` / `create_project_status_update` output. The configured `project` value is for documentation and validation only.

### Recommended workflow frontmatter

Configure the safe outputs you intend to use in your workflow frontmatter:

```yaml wrap
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

Notes:
- Keep `max` small. For `create-project-status-update`, `max: 1` is almost always enough.
- If you want the agent to read project metadata (fields/items) during reasoning, also configure `tools.github.toolsets: [projects]` with a token that has Projects access.

### How to call the tools (agent output)

#### 1) Add an issue/PR to a project and set fields

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

#### 2) Create a draft issue in the project

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

#### 3) Post a project status update (run summary)

```javascript
create_project_status_update({
	project: "https://github.com/orgs/myorg/projects/42",
	status: "ON_TRACK",
	start_date: "2026-02-04",
	target_date: "2026-02-18",
	body: "## Run summary\n\n- Processed 12 items\n- Added 3 new issues to the board\n- Next: tackle 2 blocked tasks"
})
```

#### 4) Create a new project (optional)

Prefer creating projects manually unless the workflow is explicitly intended to bootstrap new projects.

```javascript
create_project({
	title: "Project: Q1 reliability",
	owner: "myorg",
	owner_type: "org",
	item_url: "https://github.com/myorg/repo/issues/123"
})
```

### Guardrails and conventions (recommended)

- **Single source of truth**: store a concept (e.g., Target ship date) in exactly one place (a single field), not spread across multiple similar fields.
- **Prefer small, stable field vocabularies**: standardize field names like `Status`, `Priority`, `Sprint`, `Target date`. Avoid creating near-duplicates.
- **Don’t create projects implicitly**: for `update_project`, only set `create_if_missing: true` when the workflow is explicitly allowed to create/own project boards.
- **Keep status updates tight**: 5–20 lines is usually plenty; use headings + short bullets.
- **Use issues/PRs for detailed discussion**: put deep context on the issue/PR; keep the project item fields for tracking/triage.

## Optional: Project management best practices (GitHub Docs)

Use these as guidance when you’re designing how your workflow should manage a project board (not just how it calls tools).

Source: https://docs.github.com/en/issues/planning-and-tracking-with-projects/learning-about-projects/best-practices-for-projects

- **Communicate via issues and PRs**: use assignees, @mentions, links between work items, and clear ownership. Let the project reflect the state, not replace conversation.
- **Break down large work**: prefer smaller issues and PRs; use sub-issues and dependencies so blockers are explicit; use milestones/labels to connect work to larger goals.
- **Document the project**: use the project description/README to explain purpose, how to use views, and who to contact. Use status updates for high-level progress.
- **Use the right views**: maintain a few views for the most common workflows (table for detail, board for flow, roadmap for timeline) and keep filters/grouping meaningful.
- **Use field types intentionally**: choose fields that power decisions (iteration, single-select status/priority, dates). Avoid redundant or low-signal metadata.
- **Automate the boring parts**: rely on built-in project workflows where possible; use GitHub Actions + GraphQL (via these safe outputs) for consistent updates.
- **Visualize progress**: consider charts/insights for trends (throughput, blocked items, work by status/iteration) and share them with stakeholders.
- **Standardize with templates**: if multiple teams/projects follow the same process, prefer templates with prebuilt views/fields.
- **Link to teams and repos**: connect projects to the team and/or repo for discoverability and consistent access.
- **Have a single source of truth**: track important metadata (dates, status) in one place so updates don’t drift.
