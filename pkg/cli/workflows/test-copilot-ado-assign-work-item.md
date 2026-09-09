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
  ado-assign-work-item:
    target: "*"
    allowed: [owner@example.com]
    max: 1
timeout-minutes: 5
---

# Test ADO Assign Work Item Safe Output

Test the ado-assign-work-item safe output functionality.

Assign an existing Azure DevOps work item to an allowed teammate.

Output as JSONL format using the `ado_assign_work_item` tool.
