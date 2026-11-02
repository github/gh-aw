---
import-schema:
  connection:
    type: string
    required: true
connectors:
  jira:
    definition: .github/aw/connectors/jira.yaml
    connection: ${{ github.aw.import-inputs.connection }}
---

Select Jira APIs in the consuming workflow's on, tools, context, and safe-outputs sections. Importing alone grants no capabilities. Native ingress is not operational.
