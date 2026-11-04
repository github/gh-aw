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
    github-app:
      app-id: ${{ vars.PR_APP_ID }}
      private-key: ${{ secrets.PR_APP_PRIVATE_KEY }}
---

Compile GP5c isolation when only issue operations consume the global App.
