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
    owner: OrgB
    repositories: [target]
  create-pull-request:
    target-repo: OrgB/target
---

Compile GP5c host checkout isolation from a cross-organization PR target App.
