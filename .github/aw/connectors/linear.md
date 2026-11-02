---
import-schema:
  connection:
    type: string
    required: true
connectors:
  linear:
    definition: .github/aw/connectors/linear.yaml
    connection: ${{ github.aw.import-inputs.connection }}
---

Select Linear APIs in the consuming workflow's on, tools, context, and safe-outputs sections. Importing alone grants no capabilities. Native ingress is not operational.
