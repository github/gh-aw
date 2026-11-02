---
on:
  workflow_dispatch:
permissions:
  contents: read
  actions: read
engine: copilot
imports:
  - uses: "@builtin:connectors/jira.md"
    with:
      site: https://example.atlassian.net
      issues: [APP-1]
safe-outputs:
  jira-update-issue:
timeout-minutes: 5
---

# Test Jira Update Issue Safe Output

Test the jira-update-issue safe output functionality.

Update both summary and description of the approved APP-1 issue.

Use the `connector_4_jira_update_issue` tool. The trusted write host uses JIRA_REST_BASIC_AUTH.
