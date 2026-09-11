---
private: true
emoji: "🧪"
name: Smoke Issues
description: Create a daily haiku issue in Jira through safe outputs
on:
  schedule: daily
  workflow_dispatch:
permissions:
  contents: read
  actions: read
engine: copilot
safe-outputs:
  jira-create-issue:
    max: 1
    staged: true
timeout-minutes: 5
---

# Smoke Issues

Generate one original haiku about code, automation, or workflows using a 5-7-5 syllable pattern.

Create exactly one issue containing the haiku and the workflow run URL:

Use `jira_create_issue` to create one `Task` in Jira project `KAN`.

Use `Smoke Issues — ${{ github.run_id }}` as the issue title. Include this run URL in the issue body:
`${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}`.

Do not create GitHub issues or use any other write tools.
