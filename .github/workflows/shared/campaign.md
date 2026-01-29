---
# Shared safe-outputs defaults for campaign orchestrators.
# Intended to be included via frontmatter `imports:`.
# Workflows may override any specific safe-output type by defining it at top-level.

safe-outputs:
  update-project:
    max: 100
    github-token: "${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}"
  create-project-status-update:
    max: 1
    github-token: "${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}"
  create-issue:
    max: 10
    assignees: copilot
---
