---
title: AWF Reflect Route
description: Use the AWF /reflect route to discover gateway inference endpoints and available models at runtime.
sidebar:
  order: 1355
---

Inside the AWF runtime network, the AWF API proxy exposes `GET /reflect` at `http://api-proxy:10000/reflect`.

Use this route when building shared workflows, tools, or extensions that need runtime model routing.

## Why use `/reflect`

`/reflect` returns the currently configured inference providers and their model availability for the active run. This allows a shared workflow or tool to:

- Discover which gateway endpoints are available
- Check whether each endpoint is configured
- Read or refresh model availability
- Select a provider/model dynamically at runtime

> [!IMPORTANT]
> Do not hardcode direct upstream model API URLs in shared workflow logic. All inference requests should go through the AWF gateway so usage remains controllable and observable for cost control, tracking, and optimization.

## Response shape

The response includes an `endpoints` array and a `models_fetch_complete` flag:

- `endpoints[].provider`: provider identifier (e.g., `openai`, `anthropic`, `copilot`, `gemini`)
- `endpoints[].base_url`: gateway base URL for inference calls
- `endpoints[].configured`: whether credentials/config are present for that provider
- `endpoints[].models`: discovered model IDs, or `null` when model discovery is not yet complete
- `endpoints[].models_url`: gateway URL used to query models for that provider
- `models_fetch_complete`: whether startup model discovery is complete

## Recommended selection flow for shared tools

1. Query `/reflect` at start of execution.
2. Filter endpoints to `configured: true`.
3. Prefer endpoints with a non-empty `models` list.
4. Match requested model aliases/patterns against available models.
5. Route inference to the selected endpoint `base_url`.
6. If `models` is `null`, retry discovery with bounded backoff (for example, every 3 seconds up to 5 attempts) before failing.

This keeps shared tooling portable across repositories and environments where available providers differ.

## Example request

```bash
curl -s http://api-proxy:10000/reflect
```

## Learn More

- [MCP Gateway](/gh-aw/reference/mcp-gateway/)
- [Cost Management](/gh-aw/reference/cost-management/)
- [Model Aliases & Multipliers](/gh-aw/reference/model-tables/)
