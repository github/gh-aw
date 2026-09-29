---
on:
  issues:
    types: [opened]
engine: copilot
permissions:
  contents: read
  issues: read
  pull-requests: read
tools:
  github:
    mode: gh-proxy
    github-app:
      client-id: ${{ vars.SOURCE_APP_ID }}
      private-key: ${{ secrets.SOURCE_APP_PRIVATE_KEY }}
      owner: example-org
      repositories: [private-source-repo]
---

# Test Copilot GH Proxy

Verify that `tools.github.mode: gh-proxy` uses CLI proxy guidance and does not register GitHub MCP.
