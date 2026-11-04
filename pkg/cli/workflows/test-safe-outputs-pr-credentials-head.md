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
  create-pull-request:
    head-repo: OrgB/fork
    head-github-app:
      app-id: ${{ vars.HEAD_APP_ID }}
      private-key: ${{ secrets.HEAD_APP_PRIVATE_KEY }}
---

Compile GP5c host checkout isolation from distinct fork publishing credentials.
