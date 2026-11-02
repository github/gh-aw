---
import-schema:
  connection:
    type: string
    required: true
connectors:
  sentry:
    definition: .github/aw/connectors/sentry.yaml
    connection: ${{ github.aw.import-inputs.connection }}
---

Select Sentry APIs in the consuming workflow's on, tools, context, and safe-outputs sections. Importing alone grants no capabilities. Native ingress is not operational.
