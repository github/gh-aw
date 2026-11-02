---
on: workflow_dispatch
permissions:
  contents: read
  copilot-requests: write
connectors:
  servicenow:
    definition: .github/aw/connectors/servicenow.yaml
    connection: pkg/connectors/testdata/providers/servicenow/connection.yaml
    on: [incident-updated]
---

This is a negative configuration fixture. Connector event activation must fail
until an approved ServiceNow receiver, enrollment, authentication, and replay
protection are installed. Do not replace the missing receiver with an unsigned
repository dispatch, polling, or a fabricated webhook signature.
