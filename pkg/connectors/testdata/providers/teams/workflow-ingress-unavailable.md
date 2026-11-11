---
engine: copilot
imports:
  - uses: .github/workflows/connectors/teams.md
    with:
      tenants: [33333333-3333-4333-8333-333333333333]
      subscriptions: [22222222-2222-4222-8222-222222222222]
      threads: ["1730000000000"]
      notification-resources:
        - teams('11111111-1111-4111-8111-111111111111')/channels('19:approved-channel@thread.tacv2')/messages('1730000000000')
"on":
  teams-channel-message-created: null
permissions:
  contents: read
---
Connector ingress is unavailable and compilation must fail closed.
