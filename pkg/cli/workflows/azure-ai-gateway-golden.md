---
name: Azure AI Gateway deployment checklist
description: Use an approved gateway model alias for Copilot inference and stage one deployment checklist.
on: workflow_dispatch
engine:
  id: copilot
  model: gpt-5.2
  inference:
    connector: azure-ai-gateway
    profile: responses
    model: deployment-checklist
strict: true
permissions:
  contents: read
  id-token: write
network:
  allowed: [defaults]
imports:
  - uses: ".github/workflows/connectors/azure-ai-gateway.md"
    with:
      gateway-host: example.ai.gateway.azure.com
      models: [deployment-checklist]
tools:
  github: false
  bash: false
  cli-proxy: false
  edit: false
safe-outputs:
  staged: true
  create-issue:
    max: 1
---

# Deployment checklist

Replace the specimen gateway hostname, wire model alias and Copilot model
metadata with a reviewed compatible deployment before execution. Configure
AZURE_TENANT_ID and AZURE_CLIENT_ID repository variables, Entra federation
and asset-specific runtime RBAC as described by the imported connector.

Draft a short deployment checklist for a service that uses Azure AI Gateway.
Stage one GitHub issue with a factual title of at least 20 characters. Cover
model protocol compatibility, exact model access, token trust, disclosure and
cost review. Do not claim to have inspected a deployment or verified Azure
access. Do not request secrets, call other models, or perform live probes.
State that the issue is staged, not delivered.

Inference sends the workflow prompt, context and tool results to the gateway.
Staging prevents the GitHub write, not inference, disclosure or Azure charges.
Compilation is offline configuration evidence, not proof of provider access,
token validity, model quality or useful deployment advice. Importing alone
selects neither inference nor other connector capabilities.
