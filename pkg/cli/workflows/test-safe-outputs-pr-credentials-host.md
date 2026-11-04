---
on:
  workflow_dispatch:
permissions:
  contents: read
engine: copilot
checkout:
  - repository: OrgA/static
    path: static
  - repository: OrgA/private
    path: private
    github-token: ${{ secrets.STATIC_TOKEN }}
safe-outputs:
  github-app:
    app-id: ${{ vars.APP_ID }}
    private-key: ${{ secrets.APP_PRIVATE_KEY }}
  create-pull-request:
  push-to-pull-request-branch:
  create-code-scanning-alert:
    github-token: ${{ secrets.SCAN_TOKEN }}
---

Compile GP5c host PR publishing with isolated static checkout and SARIF credentials.
