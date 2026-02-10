---
name: Test Multiple Tokens
on:
  workflow_dispatch:
engine: copilot
safe-outputs:
  create-issue:
    github-token: ${{ secrets.AGENT_GITHUB_TOKEN }}
    title-prefix: '[dependabot-burner] '
    assignees: ['copilot']
    max: 10
  update-project:
    github-token: ${{ secrets.PROJECT_GITHUB_TOKEN }}
    project: "https://github.com/orgs/my-mona-org/projects/1"
    max: 50
---

# Test Multiple GitHub Tokens

Test workflow to investigate why different github-tokens in safe-outputs don't compile.
