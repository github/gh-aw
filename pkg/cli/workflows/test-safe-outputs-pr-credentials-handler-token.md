---
on:
  workflow_dispatch:
permissions:
  contents: read
engine: copilot
safe-outputs:
  github-app:
    app-id: ${{ vars.APP_ID }}
    private-key: ${{ secrets.APP_PRIVATE_KEY }}
  create-issue:
  create-pull-request:
    github-token: ${{ secrets.PR_TOKEN }}
---

Compile GP5c isolation when the PR handler has a dedicated token.
