---
imports:
  - uses: .github/workflows/connectors/servicenow.md
    with:
      instance: https://example.service-now.com
      incidents: ["0123456789abcdef0123456789abcdef"]
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
