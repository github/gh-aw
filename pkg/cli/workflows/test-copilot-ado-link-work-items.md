---
on:
  workflow_dispatch:
permissions:
  contents: read
  actions: read
engine: copilot
safe-outputs:
  env:
    AZURE_DEVOPS_ORG_URL: ${{ vars.AZURE_DEVOPS_ORG_URL }}
    SYSTEM_TEAMPROJECT: ${{ vars.AZURE_DEVOPS_PROJECT }}
    AZURE_DEVOPS_EXT_PAT: ${{ secrets.AZURE_DEVOPS_EXT_PAT }}
  ado-link-work-items:
    target: "*"
    allowed-link-types: [parent, child, related]
    max: 5
timeout-minutes: 5
---

# Test ADO Link Work Items Safe Output

Test the ado-link-work-items safe output functionality.

Link two related Azure DevOps work items together.

Output as JSONL format using the `ado_link_work_items` tool.
