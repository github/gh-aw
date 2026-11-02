---
on: workflow_dispatch
permissions:
  contents: read
  copilot-requests: write
network:
  allowed:
    - example.service-now.com
connectors:
  servicenow:
    definition: .github/aw/connectors/servicenow.yaml
    connection: pkg/connectors/testdata/providers/servicenow/connection.yaml
    safe-outputs: [append-requested-item-work-note]
---

No action is required. Do not fabricate a note or claim a journal entry was
written. This fixture selects a safe output without selecting ServiceNow read
tools, context hydration, or connector event activation.
