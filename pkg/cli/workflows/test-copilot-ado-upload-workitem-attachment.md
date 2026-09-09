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
  ado-upload-workitem-attachment:
    target: "*"
    allowed-extensions: [.txt, .log]
    max: 1
timeout-minutes: 5
---

# Test ADO Upload Work Item Attachment Safe Output

Test the ado-upload-workitem-attachment safe output functionality.

Upload a small log file as an attachment to an Azure DevOps work item.

Output as JSONL format using the `ado_upload_workitem_attachment` tool.
