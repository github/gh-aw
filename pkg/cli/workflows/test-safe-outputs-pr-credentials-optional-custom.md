---
on:
  workflow_dispatch:
permissions:
  contents: read
engine: copilot
safe-outputs:
  github-token: ${{ secrets.SO_TOKEN }}
  github-app:
    app-id: ${{ vars.APP_ID }}
    private-key: ${{ secrets.APP_PRIVATE_KEY }}
    ignore-if-missing: true
  create-pull-request:
---

Compile GP5c optional App authentication with a configured safe-output token fallback.
