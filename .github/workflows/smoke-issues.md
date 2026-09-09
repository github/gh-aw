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
safe-outputs:
  linear-create-issue:
    team-id: ${{ vars.LINEAR_TEAM_ID }}
    project-id: "810f57a7e383"
    max: 1
  jira-create-issue:
    max: 1
timeout-minutes: 5
---

# Smoke Issues

Generate one original haiku about code, automation, or workflows using a 5-7-5 syllable pattern.

Create exactly two issues containing the same haiku and the workflow run URL:

1. Use `linear_create_issue` to create one issue in the configured Linear project.
2. Use `jira_create_issue` to create one `Task` in Jira project `KAN`.

Use `Smoke Issues — ${{ github.run_id }}` as both issue titles. Include this run URL in both issue bodies:
`${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}`.

Do not create GitHub issues or use any other write tools.
