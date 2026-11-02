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
  jira-add-comment:
timeout-minutes: 5
---

# Test Jira Add Comment Safe Output

Test the jira-add-comment safe output functionality.

Add a comment to the approved APP-1 issue with findings from the repository.

Use the `connector_4_jira_add_comment` tool. The trusted write host uses JIRA_REST_BASIC_AUTH.
