---
title: How to use Azure AI Gateway
description: Connect workflows to Azure AI Gateway models and HTTP MCP servers with gateway keys or Microsoft Entra federation.
sidebar:
  order: 326
---

[Azure AI Gateway](https://ai.gateway.azure.com/docs) publishes governed model
aliases and MCP servers. Copy each asset's endpoint from the gateway Catalog;
the portal URL itself is not an inference endpoint.

## Connect a model

For Copilot BYOK, use the model's OpenAI-compatible SDK base URL and a gateway
key stored as a GitHub Actions secret:

```aw wrap
engine:
  id: copilot
  model: <published-model-id>
  env:
    COPILOT_PROVIDER_BASE_URL: https://YOUR_GATEWAY_HOST/default/models/openai/v1
    COPILOT_PROVIDER_TYPE: openai
    COPILOT_PROVIDER_WIRE_API: responses
    COPILOT_PROVIDER_API_KEY: ${{ secrets.AZURE_AI_GATEWAY_KEY }}
network:
  allowed:
    - defaults
    - YOUR_GATEWAY_HOST
```

Select `responses` only for an alias that exposes Responses; use `completions`
for Chat Completions. If the gateway alias differs from the model ID, set
`COPILOT_PROVIDER_MODEL_ID` to the alias. Gateway keys use `Authorization: Bearer`,
not Azure OpenAI's `api-key` header: keep the provider type `openai`, not `azure`.

For keyless Codex inference, use the existing
[Azure Entra OIDC configuration](/gh-aw/reference/auth/#codex-azure-entra-oidc).
Set `OPENAI_BASE_URL` to the gateway's Responses-compatible SDK base URL and
`engine.auth.azure-scope` to the gateway's Entra scope. Current gateway SDK
guidance uses `https://ai.gateway.azure.com/.default`, which differs from
gh-aw's Azure OpenAI default. Confirm the scope against the deployed gateway.

## Connect an MCP server with Entra federation

> [!IMPORTANT]
> Azure federation for HTTP MCP depends on
> [github/gh-aw-mcpg#16956](https://github.com/github/gh-aw-mcpg/pull/16956)
> and is not in the default gateway release. Compilation requires an explicit
> custom `sandbox.mcp.container` and `sandbox.mcp.version` containing that
> change. Custom gateway images also require `strict: false`. The example below
> is a configuration template, not a published image.

Configure an Entra application's federated credential to trust the workflow's
GitHub issuer, repository/branch or environment subject, and
`api://AzureADTokenExchange` audience. Grant that application's service principal
runtime access to the selected gateway MCP server. Backend credentials and
backend access remain the gateway operator's responsibility.

```aw wrap
strict: false
permissions:
  contents: read
  id-token: write
engine: copilot
sandbox:
  mcp:
    container: ghcr.io/YOUR_ORG/gh-aw-mcpg-azure
    version: YOUR_REVIEWED_BUILD_TAG
mcp-servers:
  azure-search:
    type: http
    url: https://YOUR_GATEWAY_HOST/default/toolservers/YOUR_SERVER_NAME/mcp
    allowed:
      - YOUR_READ_TOOL
    auth:
      type: github-oidc
      provider: azure
      azure-tenant-id: ${{ vars.AZURE_TENANT_ID }}
      azure-client-id: ${{ vars.AZURE_CLIENT_ID }}
      azure-scope: https://ai.gateway.azure.com/.default
network:
  allowed:
    - defaults
    - YOUR_GATEWAY_HOST
    - login.microsoftonline.com
```

Keep the tool allowlist narrow. Importing an MCP endpoint does not convert its
write tools into gh-aw safe outputs. Configure the final endpoint directly;
OIDC-authenticated gateway connections reject redirects.

The MCP gateway exchanges and refreshes tokens. Actions OIDC request
credentials and exchanged Entra tokens stay outside the agent process.
MCP authentication is per server and independent of `engine.auth`; configuring
one does not configure the other.

`auth.audience` changes the GitHub assertion audience, not the Entra scope.
`auth.azure-cloud` accepts `public` (default), `usgovernment`, or `china`;
sovereign deployments also need matching audience, scope, endpoint, and network
configuration. Omit `provider` and all `azure-*` fields to retain direct
GitHub OIDC authentication.

## Connect an MCP server with a gateway key

Gateway-key authentication works with the existing HTTP MCP integration and
does not require the unreleased Azure federation feature:

```aw wrap
mcp-servers:
  azure-search:
    type: http
    url: https://YOUR_GATEWAY_HOST/default/toolservers/YOUR_SERVER_NAME/mcp
    allowed:
      - YOUR_READ_TOOL
    headers:
      Authorization: "Bearer ${{ secrets.AZURE_AI_GATEWAY_KEY }}"
```

Gateway keys grant broad runtime access. Prefer Entra federation with
asset-specific access when the supporting MCP gateway release is available.
