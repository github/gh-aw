---
on: workflow_dispatch
permissions:
  contents: read
  copilot-requests: write
connectors:
  slack:
    definition: .github/aw/connectors/slack.yaml
    connection: pkg/connectors/testdata/providers/slack/connection.yaml
    on: [threaded-app-mention]
---

# Unsupported Slack ingress

This negative fixture must fail compilation until authenticated Slack connector
ingress is supported. Selecting a trigger does not install a verifier, enroll a
source, or authorize dispatch.
