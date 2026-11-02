---
import-schema:
  connection:
    type: string
    required: true
connectors:
  servicenow:
    definition: .github/aw/connectors/servicenow.yaml
    connection: ${{ github.aw.import-inputs.connection }}
---

Select ServiceNow APIs in the consuming workflow's on, tools, context, and safe-outputs sections. Importing alone grants no capabilities. Native ingress is not operational.
