---
on:
  workflow_dispatch:
permissions:
  contents: read
engine: copilot
safe-outputs:
  missing-tool: false
  missing-data: false
  report-incomplete: false
  github-app:
    app-id: ${{ vars.APP_ID }}
    private-key: ${{ secrets.APP_PRIVATE_KEY }}
  create-pull-request:
    github-app:
      app-id: ${{ vars.PR_APP_ID }}
      private-key: ${{ secrets.PR_APP_PRIVATE_KEY }}
---

Compile GP5c without a global App mint when the only handler has its own App.
