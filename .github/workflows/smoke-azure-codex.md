---
private: true
name: Smoke Azure Codex
description: Smoke test for Codex inference through Azure OpenAI with GitHub OIDC.
intent: Detect whether Codex can complete Azure OpenAI inference through the AWF Entra-authenticated proxy.
on:
  schedule: every 2 days
  workflow_dispatch:
permissions:
  contents: read
  id-token: write
environment: aoai-model
model: gpt-5.3-codex
engine:
  id: codex
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
  - id: azure_codex_marker
    question: Does the output contain exactly one noop safe-output request with the exact message AZURE_CODEX_OIDC_OK?
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

# Azure Codex Smoke Test

Call the `noop` safe-output tool once with message `AZURE_CODEX_OIDC_OK`.
Do not inspect the repository, run commands, or perform any other task.
