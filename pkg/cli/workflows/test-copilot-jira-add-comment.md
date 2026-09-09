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
  jira-add-comment:
    max: 1
timeout-minutes: 5
---

# Test Jira Add Comment Safe Output

Test the jira-add-comment safe output functionality.

Add a comment to an existing Jira issue with findings from the repository.

Output as JSONL format using the `jira_add_comment` tool.
