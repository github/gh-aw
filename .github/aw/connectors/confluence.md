---
import-schema:
  connection:
    type: string
    required: true
connectors:
  confluence:
    definition: .github/aw/connectors/confluence.yaml
    connection: ${{ github.aw.import-inputs.connection }}
---

Select Confluence APIs in the consuming workflow's on, tools, context, and safe-outputs sections. Importing alone grants no capabilities. Native ingress is not operational.
