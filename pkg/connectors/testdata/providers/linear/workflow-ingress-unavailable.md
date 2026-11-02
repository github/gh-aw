---
engine: copilot
imports:
  - uses: "@builtin:connectors/linear.md"
    with:
      organizations: [11111111-1111-4111-8111-111111111111]
      teams: [33333333-3333-4333-8333-333333333333]
"on":
  linear-issue-created: null
permissions:
  contents: read
  copilot-requests: write
---
Connector ingress is unavailable and compilation must fail closed.
