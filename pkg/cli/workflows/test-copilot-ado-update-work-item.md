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
  ado-update-work-item:
    target: "*"
    title: true
    max: 1
timeout-minutes: 5
---

# Test ADO Update Work Item Safe Output

Test the ado-update-work-item safe output functionality.

Update an existing Azure DevOps work item's title based on repository findings.

Output as JSONL format using the `ado_update_work_item` tool.
