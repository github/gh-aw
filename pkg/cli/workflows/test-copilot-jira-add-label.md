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
  jira-add-label:
timeout-minutes: 5
---

# Test Jira Add Label Safe Output

Test the jira-add-label safe output functionality.

Add a label to the approved APP-1 issue.

Use the `connector_4_jira_add_label` tool. The trusted write host uses JIRA_REST_BASIC_AUTH.
