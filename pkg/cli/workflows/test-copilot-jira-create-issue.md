---
on:
  workflow_dispatch:
permissions:
  contents: read
  actions: read
engine: copilot
imports:
  - uses: ".github/workflows/connectors/jira.md"
    with:
      site: https://example.atlassian.net
      projects: [APP]
safe-outputs:
  jira-create-issue:
timeout-minutes: 5
---

# Test Jira Create Issue Safe Output

Test the jira-create-issue safe output functionality.

Create a Task in the approved APP project summarizing a task discovered in the repository.

Use the `jira_create_issue` tool. The trusted write host uses JIRA_REST_BASIC_AUTH.
