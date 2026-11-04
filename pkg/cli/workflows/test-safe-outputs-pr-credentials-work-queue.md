---
on:
  workflow_dispatch:
permissions:
  contents: read
engine: copilot
tools:
  work-queue:
    worker: true
safe-outputs:
  github-app:
    app-id: ${{ vars.APP_ID }}
    private-key: ${{ secrets.APP_PRIVATE_KEY }}
  create-pull-request:
---

Compile GP5c work-queue artifact, reconciliation, mint, and checkout ordering.
