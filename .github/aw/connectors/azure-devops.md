---
import-schema:
  connection:
    type: string
    required: true
connectors:
  azure-devops:
    definition: .github/aw/connectors/azure-devops.yaml
    connection: ${{ github.aw.import-inputs.connection }}
---

Select Azure DevOps APIs in the consuming workflow's on, tools, context, and safe-outputs sections. Importing alone grants no capabilities. Native ingress is not operational.
