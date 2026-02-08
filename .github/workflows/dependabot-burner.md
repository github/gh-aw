---
name: Dependabot Burner
description: Automatically bundles Dependabot PRs by runtime and manifest, creates project items, and assigns them to Copilot for remediation with a "Review Required" status column

on:
  #schedule: daily
  workflow_dispatch:

permissions:
  contents: read
  issues: read
  pull-requests: read
  security-events: read

network:
  allowed:
    - defaults
    - github

tools:
  github:
    toolsets:
      - default
      - dependabot
      - projects
  bash:
    - "jq *"

safe-outputs:
  create-issue:
    expires: 7d
    title-prefix: "[Dependabot Burner] "
    labels: [dependabot-burner]
    assignees: [copilot]
    max: 20
    group: false
  update-project:
    project: "https://github.com/orgs/github/projects/24060"
    max: 50
    github-token: ${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}
    views:
      - name: "Dependabot Alerts Board"
        layout: board
        filter: "is:open"
      - name: "Review Required"
        layout: board
        filter: 'is:open status:"Review Required"'
      - name: "All Alerts Table"
        layout: table

  create-project-status-update:
    project: "https://github.com/orgs/github/projects/24060"
    max: 1
    github-token: ${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}
---

# Dependabot Burner

- Only create project views if they don't exist.
- Identify all open Dependabot pull requests.
- Group them by runtime and manifest file.
- Create a parent issue for each group, bundling the related PRs.
- Add each parent issue to the project board.
- Remove the individual PRs from the board once bundled.
- Assign each parent issue to Copilot.
