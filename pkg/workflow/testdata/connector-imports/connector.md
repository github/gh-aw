---
import-schema:
  connection:
    type: string
    required: true
connectors:
  tracker:
    definition: pkg/connectors/testdata/example.yaml
    connection: ${{ github.aw.import-inputs.connection }}
---

Select tracker capabilities explicitly. This local fixture reuses the repository's example definition; a distributed connector can embed the same definition in spec.
