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
  - uses: "@builtin:connectors/jira.md"
    with:
      site: https://pelidehalleux.atlassian.net
      projects: [KAN]
safe-outputs:
  linear-create-issue:
    team-id: ${{ secrets.LINEAR_TEAM_ID }}
    project-id: "810f57a7e383"
    max: 1
  jira-create-issue:
  jira-update-issue:
  jira-add-comment:
  jira-add-label:
timeout-minutes: 5
---

# Smoke Issues

Generate one original haiku about code, automation, or workflows using a 5-7-5 syllable pattern.

Create exactly two issues containing the same haiku and the workflow run URL:

1. Use `linear_create_issue` to create one issue in the configured Linear project.
2. Use `connector_4_jira_create_issue` to create one `Task` in Jira project `KAN`,
   setting `temporary_id` to `aw_jira_haiku`. Reference `#aw_jira_haiku` as `issue`
   for each follow-up:
   - use `connector_4_jira_update_issue` to set the same summary and update the description,
   - use `connector_4_jira_add_comment` to add a comment containing the workflow run URL, and
   - use `connector_4_jira_add_label` to add the `gh-aw-smoke-test` label.

Use `Smoke Issues — ${{ github.run_id }}` as both issue titles. Include this run URL in both issue bodies:
`${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}`.

Perform the three Jira follow-up operations on the newly created issue only.
Only a successful validated creation can bind the temporary reference in this
run; never substitute an arbitrary real issue key. The trusted Jira write host
requires JIRA_REST_BASIC_AUTH. Do not create GitHub issues or use any other write tools.
