"use strict";

function makeFixture({ engine, model, endpoint, supportedEndpoints, effort = "high", proxyModel = model, proxyEndpoint = endpoint, upstreamEndpoint = proxyEndpoint, api = null }) {
  const selection = {
    provider: "github",
    model,
    wire_model: model,
    effort,
    endpoint,
    selected_endpoint: endpoint,
  };
  const reflectData = {
    routing: { status: "selected", selection },
    endpoints: [
      {
        provider: "github",
        configured: true,
        models: [model],
        routing_models: [
          {
            model_id: model,
            candidate_metadata_complete: true,
            supported_endpoints: supportedEndpoints,
          },
        ],
      },
    ],
  };
  const proxyRecords = [
    {
      _schema: "model-routing/v0.28.49",
      stage: "selection",
      selected_provider: "github",
      selected_model: proxyModel,
      wire_model: proxyModel,
      selected_effort: effort,
      endpoint: proxyEndpoint,
      mode: "awf-routed",
      router: { version: "0.28.49" },
    },
  ];
  if (proxyEndpoint !== upstreamEndpoint) {
    proxyRecords.push({
      _schema: "model-routing/v0.28.49",
      stage: "request",
      requested_model: proxyModel,
      routed: "as_selected",
      upstream_endpoint: upstreamEndpoint,
      deviations: ["endpoint"],
    });
  }
  return { engine, reflectData, proxyRecords, api };
}

module.exports = {
  copilotResponses: makeFixture({
    engine: "copilot",
    model: "gpt-5.6-luna",
    endpoint: "/responses",
    supportedEndpoints: ["/responses", "/chat/completions"],
  }),
  copilotChatCompletions: makeFixture({
    engine: "copilot",
    model: "claude-sonnet-4-6",
    endpoint: "/chat/completions",
    supportedEndpoints: ["/responses", "/chat/completions"],
  }),
  claudeEndpointOverride: makeFixture({
    engine: "claude",
    model: "claude-opus-4-6",
    endpoint: "/chat/completions",
    upstreamEndpoint: "/v1/messages",
    supportedEndpoints: ["/chat/completions", "/v1/messages"],
  }),
  codexEndpointOverride: makeFixture({
    engine: "codex",
    model: "gpt-5.6-luna",
    endpoint: "/chat/completions",
    upstreamEndpoint: "/responses",
    supportedEndpoints: ["/chat/completions", "/responses"],
  }),
  piClaudePick: makeFixture({
    engine: "pi",
    model: "claude-opus-4-6",
    endpoint: "/v1/messages",
    supportedEndpoints: ["/v1/messages", "/responses", "/chat/completions"],
    api: "anthropic-messages",
  }),
  unsupportedCopilotEndpoint: makeFixture({
    engine: "copilot",
    model: "gpt-5.6-luna",
    endpoint: "/v1/messages",
    supportedEndpoints: ["/v1/messages"],
  }),
  unsupportedClaudeEndpoint: makeFixture({
    engine: "claude",
    model: "claude-opus-4-6",
    endpoint: "/chat/completions",
    supportedEndpoints: ["/chat/completions"],
  }),
  unsupportedCodexEndpoint: makeFixture({
    engine: "codex",
    model: "gpt-5.6-luna",
    endpoint: "/chat/completions",
    supportedEndpoints: ["/chat/completions"],
  }),
  unsupportedPiEndpoint: makeFixture({
    engine: "pi",
    model: "claude-opus-4-6",
    endpoint: "/v1/messages",
    supportedEndpoints: ["/v1/messages"],
    api: "openai-completions",
  }),
  harnessProxyMismatch: makeFixture({
    engine: "codex",
    model: "gpt-5.6-luna",
    proxyModel: "gpt-5.5",
    endpoint: "/responses",
    supportedEndpoints: ["/responses"],
  }),
};
