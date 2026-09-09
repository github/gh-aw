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
  jira-update-issue:
    max: 1
timeout-minutes: 5
---

# Test Jira Update Issue Safe Output

Test the jira-update-issue safe output functionality.

Update the summary of an existing Jira issue.

Output as JSONL format using the `jira_update_issue` tool.
