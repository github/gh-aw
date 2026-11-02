---
connectors:
  azure-devops:
    definition: .github/aw/connectors/azure-devops.yaml
    connection: pkg/connectors/testdata/providers/azure-devops/connection.yaml
    on: [work-item-created]
---

This negative fixture must not compile to an active source subscription.
Connector ingress requires authenticated source enrollment and is unavailable.
