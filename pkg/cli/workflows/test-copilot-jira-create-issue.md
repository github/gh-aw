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
  jira-create-issue:
    max: 1
timeout-minutes: 5
---

# Test Jira Create Issue Safe Output

Test the jira-create-issue safe output functionality.

Create a Jira issue summarizing a task discovered in the repository.

Output as JSONL format using the `jira_create_issue` tool.
