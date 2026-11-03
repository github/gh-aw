---
private: true
name: Smoke Azure PI
description: Smoke test for Pi inference through Azure OpenAI with GitHub OIDC.
intent: Detect whether Pi can complete Azure OpenAI inference through the AWF Entra-authenticated proxy.
on:
  schedule: every 2 days
  workflow_dispatch:
permissions:
  contents: read
  id-token: write
environment: aoai-model
model: openai/o4-mini-aw
engine:
  id: pi
  auth:
    type: github-oidc
    provider: azure
    azure-tenant-id: ${{ vars.AZURE_TENANT_ID }}
    azure-client-id: ${{ vars.AZURE_CLIENT_ID }}
    azure-scope: ${{ vars.AI_GATEWAY_SCOPE }}
  env:
    OPENAI_BASE_URL: ${{ vars.AI_GATEWAY_BASE_URL }}
checkout: false
tools:
  github: false
evals:
  - id: azure_pi_marker
    question: Does the output contain exactly one noop safe-output request with the exact message AZURE_PI_OIDC_OK?
  - id: inference_only_scope
    question: Does the output show no repository inspection, command execution, or additional task beyond the required noop?
safe-outputs:
  noop:
  threat-detection: false
network:
  allowed:
    - defaults
    - login.microsoftonline.com
    - openai.azure.com
    - services.ai.azure.com
timeout-minutes: 5
---

# Azure Pi Smoke Test

Call the `noop` safe-output tool once with message `AZURE_PI_OIDC_OK`.
Do not inspect the repository, run commands, or perform any other task.
