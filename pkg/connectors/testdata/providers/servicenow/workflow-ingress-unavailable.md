---
imports:
  - .github/aw/connectors/servicenow.md
network:
  allowed:
    - example.service-now.com
"on":
  servicenow-incident-updated: null
permissions:
  contents: read
  copilot-requests: write
---
Connector ingress is unavailable and compilation must fail closed.
