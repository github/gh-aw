---
engine: copilot
imports:
  - .github/aw/connectors/jira.md
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
