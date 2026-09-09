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
  ado-comment-on-work-item:
    target: "*"
    max: 1
timeout-minutes: 5
---

# Test ADO Comment On Work Item Safe Output

Test the ado-comment-on-work-item safe output functionality.

Add a comment to an existing Azure DevOps work item with findings from the repository.

Output as JSONL format using the `ado_comment_on_work_item` tool.
