---
import-schema:
  connection:
    type: string
    required: true
connectors:
  teams:
    definition: .github/aw/connectors/teams.yaml
    connection: ${{ github.aw.import-inputs.connection }}
---

Select Teams APIs in the consuming workflow's on, tools, context, and safe-outputs sections. Importing alone grants no capabilities. Native ingress is not operational.
