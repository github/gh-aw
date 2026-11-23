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

### Import the inference connector

Import
[the Azure AI Gateway declaration](https://github.com/github/gh-aw/blob/main/.github/workflows/connectors/azure-ai-gateway.md)
and explicitly select its inference profile:

```aw wrap
engine:
  id: copilot
  model: gpt-5.2
  inference:
    connector: azure-ai-gateway
    profile: responses
    model: deployment-checklist
permissions:
  contents: read
  id-token: write
imports:
  - uses: ".github/workflows/connectors/azure-ai-gateway.md"
    with:
      gateway-host: example.ai.gateway.azure.com
      models: [deployment-checklist]
network:
  allowed: [defaults, example.ai.gateway.azure.com, login.microsoftonline.com]
```

Replace the hostname, model metadata and wire alias with the reviewed Catalog
deployment. Keep the declaration at the imported local path, or use a reviewed
commit-pinned remote import. Importing alone does not configure inference or
approve network access. The connector currently supports Copilot, not the
other engines.

The `responses` and `completions` profiles use Entra federation. Set
`AZURE_TENANT_ID` and `AZURE_CLIENT_ID` Actions variables, configure the
application's GitHub federated credential, and grant asset-specific runtime
access to the selected model. The connector sets the explicit gateway scope
`https://ai.gateway.azure.com/.default`; existing engine OIDC safeguards apply.
The tenant and client can instead be supplied with the `azure-tenant-id` and
`azure-client-id` import bindings.

For a gateway key, select `responses-key` or `completions-key` and store it as
`AZURE_AI_GATEWAY_KEY` (or bind `key-secret` to another secret **name**). Remove
`id-token: write` and `login.microsoftonline.com` when no other feature needs
Entra federation. Only the selected profile's
credential is used; there is no credential fallback.

Use the protocol the deployed alias supports. `engine.model` supplies Copilot
model metadata; `engine.inference.model` is the exact approved wire alias.
Native provider/auth/env overrides, routing, model fallback and Copilot SDK
mode cannot be combined with connector inference. A threat detector that
inherits the main engine cannot override its engine or model configuration.
MCP configuration is separate.

Run `gh aw connector azure-ai-gateway setup --phase inference` for offline
provisioning guidance, then compile a consumer with `gh aw compile --strict
--no-emit`. See the
[source-only checklist example](https://github.com/github/gh-aw/blob/main/pkg/cli/workflows/azure-ai-gateway-golden.md).
These checks do not contact Azure or establish live compatibility. Executing
even a staged consumer sends prompts/context to the gateway and incurs model
charges.

The [connector specification](/gh-aw/specs/connector-specification/#54-inference-provider-configuration)
defines profile selection, model grants and rejected overrides. Its
[bounded inference model](/gh-aw/specs/connector-specification/#inference-binding-model)
checks symbolic selection, phase separation and terminal authentication denial,
including a witness that staged writes do not disable inference. It does not
verify Entra cryptography, runtime protocol compatibility or model quality.

### Configure BYOK directly

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
> Azure federation for HTTP MCP requires MCP gateway v0.5.12 or newer.
> For a custom gateway image, explicitly set `sandbox.mcp.container` and
> `sandbox.mcp.version` to a reviewed build supporting federation; custom
> gateway images also require `strict: false`.

Configure an Entra application's federated credential to trust the workflow's
GitHub issuer, repository/branch or environment subject, and
`api://AzureADTokenExchange` audience. Grant that application's service principal
runtime access to the selected gateway MCP server. Backend credentials and
backend access remain the gateway operator's responsibility.

```aw wrap
permissions:
  contents: read
  id-token: write
engine: copilot
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

Gateway-key authentication works with the existing HTTP MCP integration:

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
asset-specific access.
