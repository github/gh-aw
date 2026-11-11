---
imports:
  - uses: .github/workflows/connectors/confluence.md
    with:
      clouds: ["11111111-2222-4333-8444-555555555555"]
      spaces: ["123"]
      pages: ["456"]
"on":
  confluence-page-created: null
permissions:
  contents: read
---
Connector ingress is unavailable and compilation must fail closed.
