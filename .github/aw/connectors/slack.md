---
import-schema:
  connection:
    type: string
    required: true
connectors:
  slack:
    definition: .github/aw/connectors/slack.yaml
    connection: ${{ github.aw.import-inputs.connection }}
---

Select Slack APIs in the consuming workflow's on, tools, context, and safe-outputs sections. Importing alone grants no capabilities. Native ingress is not operational.
