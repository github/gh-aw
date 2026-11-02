---
private: true
emoji: "🧪"
name: Smoke Issues
description: Create a daily haiku issue in Linear and Jira through safe outputs
on:
  schedule: daily
  workflow_dispatch:
permissions:
  contents: read
  actions: read
engine: copilot
imports:
  - uses: "@builtin:connectors/linear.md"
    with:
      writer-secret: LINEAR_API_KEY
      create-team-secret: LINEAR_TEAM_ID
      create-project-slug: "810f57a7e383"
safe-outputs:
  linear-create-issue-in-project:
  jira-create-issue:
    max: 1
  jira-update-issue:
    max: 1
  jira-add-comment:
    max: 1
  jira-add-label:
    max: 1
timeout-minutes: 5
---

# Smoke Issues

Generate one original haiku about code, automation, or workflows using a 5-7-5 syllable pattern.

Create exactly two issues containing the same haiku and the workflow run URL:

1. Use `connector_6_linear_create_issue_in_project` to create one issue in the configured Linear project. The trusted connector resolves and verifies its team and project; provide only the title and body.
2. Use `jira_create_issue` to create one `Task` in Jira project `KAN`. Save the returned temporary ID, then use it as `issue_key` to:
   - update the issue description with `jira_update_issue`,
   - add a comment containing the workflow run URL with `jira_add_comment`, and
   - add the `gh-aw-smoke-test` label with `jira_add_label`.

Use `Smoke Issues — ${{ github.run_id }}` as both issue titles. Include this run URL in both issue bodies:
`${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}`.

Perform the three Jira follow-up operations on the newly created issue only. Do not create GitHub issues or use any other write tools.
