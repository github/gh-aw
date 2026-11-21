---
title: How to use Azure OpenAI with Copilot BYOK
description: Configure GitHub Agentic Workflows to route Copilot through Azure OpenAI using API keys or Microsoft Entra authentication.
sidebar:
  order: 325
---

Azure OpenAI is supported in Copilot Bring Your Own Key (BYOK) mode. Use the
Azure OpenAI resource's OpenAI v1 endpoint, put provider credentials in
`engine.env`, and choose the wire model deliberately when Azure deployment names
do not match Azure model IDs.

For the separate Azure AI Gateway service at `ai.gateway.azure.com`, see
[Azure AI Gateway](/gh-aw/reference/azure-ai-gateway/). Its token scope and
gateway-key header conventions differ from Azure OpenAI.

## Use the Azure OpenAI v1 endpoint

Set `COPILOT_PROVIDER_BASE_URL` to the Azure resource endpoint with the
`/openai/v1` suffix:

```aw wrap
engine:
  id: copilot
  model: gpt-5.4-2026-03-05
  env:
    COPILOT_PROVIDER_BASE_URL: https://RESOURCE.openai.azure.com/openai/v1
```

Do not use the older `/openai/v1-preview` path unless the Azure resource
explicitly exposes it. The repository smoke workflows
`smoke-copilot-aoai-apikey.md` and `smoke-copilot-aoai-entra.md` both use the
v1 endpoint.

## Choose the model name and deployment name

`engine.model` (compiled to `COPILOT_MODEL`) should name the Azure model ID that
the provider exposes from `GET /openai/v1/models`. For versioned GPT-5 models,
that is often a fully qualified name such as `gpt-5.4-2026-03-05`.

If the Azure deployment name is different, set `COPILOT_PROVIDER_MODEL_ID` to
the deployment name that Azure expects on the request body:

```aw wrap
engine:
  id: copilot
  model: gpt-5.4-2026-03-05
  env:
    COPILOT_PROVIDER_BASE_URL: https://RESOURCE.openai.azure.com/openai/v1
    COPILOT_PROVIDER_MODEL_ID: gpt-5.4
```

This keeps the AWF proxy and Copilot CLI aligned on the selected model while
still sending the Azure deployment name upstream.

## Configure API-key authentication

```aw wrap
engine:
  id: copilot
  model: gpt-5.4-2026-03-05
  env:
    COPILOT_PROVIDER_BASE_URL: https://RESOURCE.openai.azure.com/openai/v1
    COPILOT_PROVIDER_API_KEY: ${{ secrets.AZURE_OPENAI_API_KEY }}
    COPILOT_PROVIDER_MODEL_ID: gpt-5.4
    COPILOT_PROVIDER_WIRE_API: responses

network:
  allowed:
    - defaults
    - RESOURCE.openai.azure.com
```

`COPILOT_PROVIDER_WIRE_API: responses` is required for GPT-5 and o-series
models — see [Use the `responses` wire
API](#use-the-responses-wire-api-for-gpt-5-and-o-series-models) below for
details.

> [!IMPORTANT]
> Put Azure BYOK credentials in `engine.env`, not in top-level `secrets:`. The
> BYOK proxy only receives provider credentials from the engine environment.

## Configure Microsoft Entra authentication

Use GitHub OIDC when the Azure resource trusts a federated identity:

```aw wrap
permissions:
  id-token: write

engine:
  id: copilot
  model: gpt-5.4-2026-03-05
  auth:
    type: github-oidc
    provider: azure
    azure-tenant-id: <tenant-id>
    azure-client-id: <client-id>
  env:
    COPILOT_PROVIDER_BASE_URL: https://RESOURCE.openai.azure.com/openai/v1
    COPILOT_PROVIDER_MODEL_ID: gpt-5.4
    COPILOT_PROVIDER_WIRE_API: responses

network:
  allowed:
    - defaults
    - RESOURCE.openai.azure.com
    - login.microsoftonline.com
```

`COPILOT_PROVIDER_WIRE_API: responses` is required for GPT-5 and o-series
models — see [Use the `responses` wire
API](#use-the-responses-wire-api-for-gpt-5-and-o-series-models) below for
details.

## Use the `responses` wire API for GPT-5 and o-series models

The Copilot CLI defaults custom providers to the legacy `completions` wire API.
Azure GPT-5 and o-series deployments typically require the newer `responses`
wire API instead:

```aw wrap
engine:
  env:
    COPILOT_PROVIDER_WIRE_API: responses
```

The repository smoke workflows `smoke-copilot-aoai-apikey.md` and
`smoke-copilot-aoai-entra.md` both use this setting.

## Troubleshooting

If Azure works with direct `curl` requests but the AWF proxy returns
`model not found`, check these items first:

- Verify that `engine.model` matches a model returned by
  `GET /openai/v1/models`.
- If Azure requires a different deployment name on the wire, set
  `COPILOT_PROVIDER_MODEL_ID` to that deployment name.
- Keep `COPILOT_PROVIDER_BASE_URL` on the `/openai/v1` endpoint.
- Set `COPILOT_PROVIDER_WIRE_API: responses` for GPT-5 and o-series models.

If the proxy still rewrites the deployment name and Azure returns HTTP 404,
disable AWF model fallback for that workflow:

```aw wrap
sandbox:
  agent:
    id: awf
    model-fallback: false
```

## Recompile after workflow edits

Azure BYOK settings live in workflow frontmatter. All workflow edits require a
recompile:

```bash
gh aw compile .github/workflows/my-workflow.md --watch
```

## Learn More

- [AI Engines Reference](/gh-aw/reference/engines/copilot/#copilot-bring-your-own-key-byok-mode)
- [Network Configuration Guide](/gh-aw/guides/network-configuration/)
- [Sandbox Reference](/gh-aw/reference/sandbox/)
