---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
  copilot-requests: write
connectors:
  jira:
    definition: pkg/connectors/testdata/providers/jira/spec.yaml
    connection: pkg/connectors/testdata/providers/jira/connection.yaml
    on: [issue-created]
---

# Nonactivating Jira trigger fixture

This fixture must fail compilation until authenticated connector ingress exists.
Declaring a verification profile does not authenticate or activate a source.
