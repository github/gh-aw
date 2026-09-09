---
on:
  workflow_dispatch:
permissions:
  contents: read
  actions: read
engine: copilot
safe-outputs:
  env:
    JIRA_BASE_URL: ${{ vars.JIRA_BASE_URL }}
    JIRA_USER_EMAIL: ${{ secrets.JIRA_USER_EMAIL }}
    JIRA_API_TOKEN: ${{ secrets.JIRA_API_TOKEN }}
  jira-add-label:
    max: 3
timeout-minutes: 5
---

# Test Jira Add Label Safe Output

Test the jira-add-label safe output functionality.

Add a label to an existing Jira issue.

Output as JSONL format using the `jira_add_label` tool.
