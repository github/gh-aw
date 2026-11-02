---
engine: copilot
imports:
  - uses: "@builtin:connectors/jira.md"
    with:
      projects: [APP]
      issue-ids: ["10001"]
network:
  allowed:
    - defaults
    - mcp.atlassian.com
    - example.atlassian.net
"on":
  jira-issue-created: null
permissions:
  contents: read
  copilot-requests: write
---
Connector ingress is unavailable and compilation must fail closed.
