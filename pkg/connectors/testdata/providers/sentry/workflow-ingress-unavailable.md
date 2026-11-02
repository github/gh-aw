---
engine: copilot
imports:
  - .github/workflows/connectors/sentry.md
"on":
  sentry-error-created: null
permissions:
  contents: read
---
Connector ingress is unavailable and compilation must fail closed.
