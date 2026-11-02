---
connectors:
  azure-devops:
    definition: pkg/connectors/testdata/providers/azure-devops/spec.yaml
    connection: pkg/connectors/testdata/providers/azure-devops/connection.yaml
    on: [work-item-created]
---

This negative fixture must not compile to an active source subscription.
Connector ingress requires authenticated source enrollment and is unavailable.
