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
  ado-create-work-item:
    work-item-type: Task
    max: 1
timeout-minutes: 5
---

# Test ADO Create Work Item Safe Output

Test the ado-create-work-item safe output functionality.

Create an Azure DevOps work item summarizing a task discovered in the repository.

Output as JSONL format using the `ado_create_work_item` tool.
