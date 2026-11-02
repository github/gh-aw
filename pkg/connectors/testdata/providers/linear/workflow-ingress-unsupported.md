---
on: workflow_dispatch
permissions:
  contents: read
  copilot-requests: write
engine: copilot
connectors:
  linear:
    definition: .github/aw/connectors/linear.yaml
    connection: pkg/connectors/testdata/providers/linear/connection.yaml
    on: [issue-created]
---

This negative fixture must fail compilation until authenticated connector
ingress is supported. A valid provider definition does not activate a receiver.
