---
on:
  workflow_dispatch:
permissions:
  contents: read
engine: copilot
checkout:
  github-app:
    app-id: ${{ vars.CHECKOUT_APP_ID }}
    private-key: ${{ secrets.CHECKOUT_APP_PRIVATE_KEY }}
safe-outputs:
  github-app:
    app-id: ${{ vars.APP_ID }}
    private-key: ${{ secrets.APP_PRIVATE_KEY }}
  create-pull-request:
---

Compile GP5c precedence for explicit default checkout App authentication.
