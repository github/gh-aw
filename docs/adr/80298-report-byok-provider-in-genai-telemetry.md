# ADR-80298: Report the BYOK Provider in GenAI Telemetry

**Date**: 2026-11-23
**Status**: Draft

## Context

Copilot BYOK runs reported `gen_ai.provider.name: github` even when inference went to Azure AI Gateway or another BYOK endpoint. The engine-derived mapping (`copilot` -> `github`) cannot tell BYOK traffic apart from GitHub-hosted inference, so the telemetry did not identify BYOK runs. This is finding F6 from the Azure AI Gateway BYOK pilot.

BYOK can be configured in three ways:

- `COPILOT_PROVIDER_BASE_URL` in the engine environment;
- connector inference, such as the azure-ai-gateway connector;
- a non-GitHub `engine.provider` override with the firewall enabled.

Values that are `${{ }}` expressions cannot be resolved at compile time, so they are ignored for classification.

Span emission in `actions/setup/js/send_otlp_span.cjs` read only the engine mapping. Forwarding `traceparent` to upstream LLM requests is a separate concern: gh-aw already passes trace context into AWF, but AWF injects `traceparent` only for model-router calls, so it needs an upstream change.

## Decision

Resolve the effective BYOK GenAI provider at compile time and carry it to runtime.

- `pkg/workflow/genai_provider.go` (`resolveBYOKGenAIProviderName`) returns:
  - `azure.ai.openai` for Azure hosts (`*.azure.com`, `*.azure-api.net`, `*.azure.us`, `*.azure.cn`), or when `COPILOT_PROVIDER_TYPE` or the auth provider is `azure`;
  - `anthropic` for `COPILOT_PROVIDER_TYPE=anthropic`;
  - `openai` for any other OpenAI-compatible BYOK endpoint, including a non-Azure auth provider such as `gcp` with a base URL, because auth describes credentials, not the endpoint;
  - an empty value for non-BYOK runs, which keep the engine-derived provider.
- The compiler emits `GH_AW_INFO_GENAI_PROVIDER` in the aw_info step and in both setup-step paths, so post-step spans in non-agent jobs see the same value.
- `actions/setup/js/generate_aw_info.cjs` records it as `genai_provider` in `aw_info.json`.
- `send_otlp_span.cjs` (`resolveRunGenAiProvider`) prefers the BYOK value over the engine mapping for `gen_ai.provider.name`, the legacy `gen_ai.system` (mapped to `az.ai.openai` for Azure), and the metrics `provider` field.
- Spec section 10.3 (`specs/otel-observability-spec.md`) and the OpenTelemetry attributes reference are updated to describe BYOK provider values.

## Alternatives Considered

- **Keep the engine-derived provider (`github` for Copilot).** Rejected as the status quo. It is the reported defect: BYOK traffic is labelled as GitHub-hosted inference.
- **Forward W3C `traceparent` to the BYOK endpoint so the upstream request joins the run trace.** Deferred and out of scope for this PR. It needs an AWF api-proxy change, tracked in github/gh-aw-firewall#11982, that injects `traceparent` and `tracestate` (never `baggage`) only when the target is the configured BYOK host.
- **Derive the provider at span-emission time from runtime environment variables.** Considered and rejected. The compiler already holds the normalized engine config, including connector inference rewrites. Post-step span code sees only raw environment variables, which may contain unresolved expressions, and the engine env is absent in jobs such as non-agent setup spans. Compile-time resolution keeps the logic in one tested Go function and makes the value visible in lock files. The tradeoff is that expression-valued base URLs fall back to `openai`.
- **Keep `gen_ai.provider.name: github` and add a separate BYOK attribute.** Rejected. The OpenTelemetry GenAI semantic conventions define `gen_ai.provider.name` as the actual model provider, so reporting `github` for Azure or OpenAI traffic would remain non-conformant.

## Consequences

Positive:

- BYOK runs are identifiable in `gen_ai.provider.name`, `gen_ai.system`, and the metrics `provider` field, so Azure AI Gateway and other BYOK traffic can be separated from GitHub-hosted inference.
- One resolution path covers the setup, conclusion, and agent spans, including non-agent jobs, because the value is also set in the setup steps.
- Non-BYOK runs are unchanged: no `GH_AW_INFO_GENAI_PROVIDER` is emitted, and only the three BYOK lock files change on recompile.

Negative:

- Classification is heuristic. A gateway on a host outside the listed suffixes is reported as `openai` unless `COPILOT_PROVIDER_TYPE` or the auth provider is `azure`.
- A base URL given as a `${{ }}` expression is not inspected for its host, so an Azure endpoint behind an expression URL is reported as `openai` unless the provider type or auth provider is `azure`.
- The mapping is duplicated in Go (`genai_provider.go`) and JavaScript (`BYOK_PROVIDER_TO_SYSTEM_MAP` in `send_otlp_span.cjs`), so the two must stay in sync with spec section 10.3.
- Dashboards or queries that treat `gen_ai.provider.name = github` as the Copilot signal will see BYOK runs under other values. The spec changelog records this change.
- The `traceparent` gap stays open until the upstream AWF change lands.
