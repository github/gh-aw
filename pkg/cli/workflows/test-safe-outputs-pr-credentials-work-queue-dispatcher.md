---
on:
  workflow_dispatch:
permissions:
  contents: read
engine: copilot
tools:
  work-queue: true
safe-outputs:
  github-app:
    app-id: ${{ vars.APP_ID }}
    private-key: ${{ secrets.APP_PRIVATE_KEY }}
  create-pull-request:
  dispatch-workflow:
    workflows: [test-safe-outputs-pr-credentials-work-queue]
    github-token: ${{ secrets.DISPATCH_TOKEN }}
---

Compile GP5c claim reconciliation before PR token consumption in a dispatcher.
