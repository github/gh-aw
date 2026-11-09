import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "module";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

const require = createRequire(import.meta.url);
const piProvider = require("./pi_provider.cjs");
const { runPiSubagent } = require("./pi_subagent_extension.cjs");
const { runWithCopilotSDK } = require("./copilot_sdk_session.cjs");
const { prepareMarkdownFixtures } = require("../../../scripts/markdown_metadata_test_adapter.cjs");
const { resolveMultiProviderFromReflect } = require("./awf_reflect.cjs");
const { normalizeRuntimeEvent } = require("./unified_session.cjs");
const { normalizeUnifiedSessionEvent } = require("./unified_session_payload.cjs");
const { createSessionValidator } = require("./scripts/validate_session.cjs");

const ACTIONS_DIR = import.meta.dirname;

// Verbatim rows of agent artifact sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl
// from Smoke Pi Routed run 39816721390 (AWF v0.28.70). AWF omits agent_name on
// main and classifier rows.
const PI_CLASSIFIER = {
  _schema: "token-usage/v0.28.70",
  timestamp: "2026-10-22T16:49:26.409Z",
  event: "token_usage",
  request_id: "d482afa2-cb18-4b20-9b1f-8682eef3a3bc",
  provider: "copilot",
  model: "gpt-5.6-luna",
  path: "/responses",
  status: 200,
  streaming: false,
  input_tokens: 939,
  output_tokens: 683,
  cache_read_tokens: 0,
  cache_write_tokens: 0,
  duration_ms: 7233,
  response_bytes: 28755,
  purpose: "routing_classification",
  caller_source: "awf",
  x_initiator: "agent",
  ai_credits_this_response: 0.10074,
  ai_credits_total: 0.10074,
  ai_credits_pricing_source: "provider",
  ai_credits_pricing_tier: "default",
  ai_credits_accounting_policy: "concrete_model",
  ai_credits_fallback_pricing_used: false,
  ai_credits_pricing_observed_at: "2026-10-22T16:49:19.145Z",
  ai_credits_pricing_api_version: "2026-07-01",
};
const PI_MAIN = {
  _schema: "token-usage/v0.28.70",
  timestamp: "2026-10-22T16:49:40.800Z",
  event: "token_usage",
  request_id: "30ad7ece-0ee6-46d3-acc1-3f685176d0fd",
  provider: "copilot",
  model: "gpt-5.6-luna",
  path: "/responses",
  status: 200,
  streaming: true,
  input_tokens: 11669,
  output_tokens: 236,
  cache_read_tokens: 0,
  cache_write_tokens: 0,
  duration_ms: 2974,
  response_bytes: 188333,
  purpose: "agent",
  caller_source: "caller_header",
  x_initiator: "agent",
  ai_credits_this_response: 0.2617,
  ai_credits_total: 0.36244,
  ai_credits_pricing_source: "provider",
  ai_credits_pricing_tier: "default",
  ai_credits_accounting_policy: "concrete_model",
  ai_credits_fallback_pricing_used: false,
  ai_credits_pricing_observed_at: "2026-10-22T16:49:19.145Z",
  ai_credits_pricing_api_version: "2026-07-01",
};
const PI_HAIKU = {
  _schema: "token-usage/v0.28.70",
  timestamp: "2026-10-22T16:49:42.214Z",
  event: "token_usage",
  request_id: "2d40ee31-fbc2-410f-b7aa-d3bd8a30a99b",
  provider: "copilot",
  model: "claude-haiku-4-5-20251001",
  path: "/v1/messages?beta=true",
  status: 200,
  streaming: true,
  input_tokens: 3,
  output_tokens: 8,
  cache_read_tokens: 0,
  cache_write_tokens: 16332,
  duration_ms: 1024,
  response_bytes: 1859,
  purpose: "subagent",
  agent_name: "haiku-whoami",
  caller_source: "caller_header",
  x_initiator: "agent",
  ai_credits_this_response: 2.0458,
  ai_credits_total: 2.40824,
  ai_credits_pricing_source: "provider",
  ai_credits_pricing_tier: "default",
  ai_credits_accounting_policy: "concrete_model",
  ai_credits_fallback_pricing_used: false,
  ai_credits_pricing_observed_at: "2026-10-22T16:49:19.145Z",
  ai_credits_pricing_api_version: "2026-07-01",
};
const PI_MINI = {
  _schema: "token-usage/v0.28.70",
  timestamp: "2026-10-22T16:49:47.869Z",
  event: "token_usage",
  request_id: "bfc0ed4b-0845-4fe6-a6a5-5ed20b3eb282",
  provider: "copilot",
  model: "gpt-5.3-codex",
  path: "/responses",
  status: 200,
  streaming: true,
  input_tokens: 11465,
  output_tokens: 134,
  cache_read_tokens: 0,
  cache_write_tokens: 0,
  duration_ms: 2823,
  response_bytes: 179115,
  purpose: "subagent",
  agent_name: "mini-whoami",
  caller_source: "caller_header",
  x_initiator: "agent",
  ai_credits_this_response: 2.193975,
  ai_credits_total: 4.635447,
  ai_credits_pricing_source: "provider",
  ai_credits_pricing_tier: "default",
  ai_credits_accounting_policy: "concrete_model",
  ai_credits_fallback_pricing_used: false,
  ai_credits_pricing_observed_at: "2026-10-22T16:49:19.145Z",
  ai_credits_pricing_api_version: "2026-07-01",
};

// AWF writes no caller fields for a request without a valid x-awf-caller header
// (older AWF, headerless engines, or a rejected value), as in Smoke Pi Routed
// run 39540473269 before the harness sent the header.
const withoutCallerFields = ({ purpose: _purpose, agent_name: _name, caller_source: _source, ...record }) => record;

/** Collector path: raw api-proxy record -> runtime event -> unified projection. */
function project(record) {
  const event = normalizeUnifiedSessionEvent(normalizeRuntimeEvent("firewall", record));
  expect(event.type).toBe("firewall.token_usage");
  const validate = createSessionValidator("unified").event;
  const published = { ...event, provenance: { component: "firewall", phase: "agent", path: "sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl", index: 0 } };
  expect(validate(published), JSON.stringify(validate.errors)).toBe(true);
  expect(normalizeUnifiedSessionEvent(event)).toEqual(event);
  return event.data;
}

function callerFields(data) {
  return Object.fromEntries(["purpose", "agentName", "callerSource"].filter(key => Object.hasOwn(data, key)).map(key => [key, data[key]]));
}

/** Load pi_provider.cjs as Pi would and capture headers from its provider hook. */
function captureProviderHeaders(initial = {}) {
  const handlers = {};
  piProvider({ registerProvider: () => {}, on: (name, handler) => (handlers[name] = handler) });
  const headers = { authorization: "******", ...initial };
  handlers.before_provider_headers?.({ type: "before_provider_headers", headers });
  return headers;
}

describe("AWF x-awf-caller attribution", () => {
  let dir;
  beforeEach(() => {
    dir = fs.mkdtempSync(path.join(os.tmpdir(), "awf-caller-"));
    vi.spyOn(process.stderr, "write").mockImplementation(() => true);
    vi.stubEnv("COPILOT_GITHUB_TOKEN", "");
    vi.stubEnv("GITHUB_TOKEN", "");
    vi.stubEnv("ANTHROPIC_API_KEY", "");
    vi.stubEnv("CODEX_API_KEY", "");
    vi.stubEnv("OPENAI_API_KEY", "");
    vi.stubEnv("GH_AW_PI_SUBAGENT_CHILD", "");
    vi.stubEnv("GH_AW_PI_SUBAGENT_NAME", "");
    vi.stubEnv("AWF_REFLECT_ENABLED", "1");
  });
  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllEnvs();
    fs.rmSync(dir, { recursive: true, force: true });
  });

  it("pi_main_and_subagent_caller_attribution", async () => {
    // Parent Pi process: the provider hook declares the main agent.
    const parentHeaders = captureProviderHeaders({ "X-AWF-Caller": "subagent/spoofed" });
    expect(parentHeaders).toEqual({ authorization: "******", "x-awf-caller": "main" });

    // Child Pi process: launched by the sub-agent extension with its own
    // environment; a real process loads pi_provider.cjs and fires the hook.
    const executable = path.join(dir, "fake-pi");
    fs.writeFileSync(
      executable,
      `#!/usr/bin/env node
const provider = require(${JSON.stringify(path.join(ACTIONS_DIR, "pi_provider.cjs"))});
const handlers = {};
provider({ registerProvider: () => {}, on: (name, handler) => (handlers[name] = handler) });
process.stdin.resume();
process.stdin.on("end", () => {
  const headers = {};
  handlers.before_provider_headers({ type: "before_provider_headers", headers });
  const event = { type: "message_end", message: { role: "assistant", content: [{ type: "text", text: JSON.stringify(headers) }] } };
  process.stdout.write(JSON.stringify(event) + "\\n");
});
`,
      { mode: 0o700 }
    );
    vi.stubEnv("GH_AW_PI_COMMAND", executable);
    vi.stubEnv("RUNNER_TEMP", "");
    vi.stubEnv("PI_CODING_AGENT_DIR", dir);
    const write = vi.spyOn(process.stdout, "write").mockImplementation(() => true);
    const agent = { name: "haiku-whoami", description: "Who am I", prompt: "Answer.", declaredModel: "claude-haiku-4.5", modelId: "claude-haiku-4.5", model: "aw-gateway/claude-haiku-4.5" };
    const result = await runPiSubagent(agent, "Who are you?", { cwd: dir });
    expect(JSON.parse(result.content[0].text)).toEqual({ "x-awf-caller": "subagent/haiku-whoami" });
    write.mockRestore();

    // AWF records of those requests keep their caller fields through the collector.
    expect(callerFields(project(PI_MAIN))).toEqual({ purpose: "agent", callerSource: "caller_header" });
    expect(callerFields(project(PI_HAIKU))).toEqual({ purpose: "subagent", agentName: "haiku-whoami", callerSource: "caller_header" });
    expect(callerFields(project(PI_MINI))).toEqual({ purpose: "subagent", agentName: "mini-whoami", callerSource: "caller_header" });
    // Canonical camelCase input is preserved too.
    expect(callerFields(normalizeUnifiedSessionEvent({ type: "firewall.token_usage", data: { purpose: "subagent", agentName: "haiku-whoami", callerSource: "caller_header" } }).data)).toEqual({
      purpose: "subagent",
      agentName: "haiku-whoami",
      callerSource: "caller_header",
    });
  });

  it("copilot_sdk_main_and_subagent_caller_attribution", async () => {
    // SDK 1.0.16 headers are provider-scoped (providers[].headers) or per turn
    // (MessageOptions.requestHeaders); neither identifies a child request. Main
    // and haiku-whoami share the copilot-completions provider and served model,
    // so any header would also label child traffic. The SDK sends none and
    // relies on native per-agent metrics instead.
    fs.mkdirSync(path.join(dir, ".github/agents"), { recursive: true });
    const agent = "---\nmodel: claude-haiku-4.5\n---\nSay which model you are.";
    fs.writeFileSync(path.join(dir, ".github/agents/haiku-whoami.agent.md"), agent);
    prepareMarkdownFixtures(agent);
    vi.stubEnv("GH_AW_SESSION_STATE_BASE_DIR", dir);
    vi.stubEnv("GH_AW_ENGINE_CWD", dir);
    vi.stubEnv("COPILOT_MODEL", "");
    const config = resolveMultiProviderFromReflect({
      model: "claude-haiku-4.5",
      reflectData: {
        endpoints: [
          {
            provider: "copilot",
            configured: true,
            port: 10002,
            models: ["claude-haiku-4.5", "gpt-5.6-luna"],
            routing_models: [
              { model_id: "claude-haiku-4.5", supported_endpoints: ["/chat/completions", "/v1/messages"] },
              { model_id: "gpt-5.6-luna", supported_endpoints: ["/responses"] },
            ],
          },
        ],
      },
    });
    vi.stubEnv("GH_AW_COPILOT_SDK_MULTI_PROVIDER_JSON", JSON.stringify(config));
    const sendAndWait = vi.fn(async () => ({ data: { content: "done" } }));
    const createSession = vi.fn().mockResolvedValue({ sessionId: "caller-sdk", on: () => {}, sendAndWait, disconnect: async () => {} });
    class FakeCopilotClient {
      start = async () => {};
      stop = async () => {};
      createSession = createSession;
    }
    const result = await runWithCopilotSDK({
      sdkUri: "http://127.0.0.1:3002",
      prompt: "test",
      logger: () => {},
      workingDirectory: dir,
      agentsBaseDir: dir,
      sdkModule: { CopilotClient: FakeCopilotClient, RuntimeConnection: { forUri: () => ({}) }, approveAll: () => "allow" },
    });
    expect(result.exitCode).toBe(0);
    const session = createSession.mock.calls[0][0];
    // Wire APIs, providers and model resolution are unchanged.
    expect(session.providers).toEqual(config.providers);
    expect(session.models).toEqual(config.models);
    expect(session.model).toBe("copilot-completions/claude-haiku-4.5");
    expect(session.customAgents).toEqual([expect.objectContaining({ name: "haiku-whoami", model: "copilot-completions/claude-haiku-4.5" })]);
    expect(JSON.stringify(session).toLowerCase()).not.toContain("x-awf-caller");
    expect(session.providers.every(provider => provider.headers === undefined)).toBe(true);
    for (const call of sendAndWait.mock.calls) expect(JSON.stringify(call).toLowerCase()).not.toContain("x-awf-caller");
  });

  it("unsupported_engine_does_not_mislabel_subagents", () => {
    // A child process whose name cannot form a valid caller sends no header and
    // never inherits the parent's main declaration.
    for (const name of ["", "has space", "a".repeat(120)]) {
      vi.stubEnv("GH_AW_PI_SUBAGENT_CHILD", "1");
      vi.stubEnv("GH_AW_PI_SUBAGENT_NAME", name);
      expect(piProvider.resolveAwfCaller()).toBeNull();
      expect(captureProviderHeaders({ "x-awf-caller": "main" })).toEqual({ authorization: "******" });
    }
    // Boundary-length names are sent unchanged.
    vi.stubEnv("GH_AW_PI_SUBAGENT_NAME", "a".repeat(119));
    expect(captureProviderHeaders()["x-awf-caller"]).toBe(`subagent/${"a".repeat(119)}`);
    expect(captureProviderHeaders({ "x-awf-caller": "main" })["x-awf-caller"]).toBe(`subagent/${"a".repeat(119)}`);
    vi.stubEnv("GH_AW_PI_SUBAGENT_NAME", "a_b.c:d/e-f");
    expect(captureProviderHeaders()["x-awf-caller"]).toBe("subagent/a_b.c:d/e-f");
  });

  it("no_caller_header_without_awf_proxy", () => {
    // sandbox.agent: false has no api-proxy, so caller names must not reach the provider.
    vi.stubEnv("AWF_REFLECT_ENABLED", "");
    expect(captureProviderHeaders()).toEqual({ authorization: "******" });
    vi.stubEnv("GH_AW_PI_SUBAGENT_CHILD", "1");
    vi.stubEnv("GH_AW_PI_SUBAGENT_NAME", "haiku-whoami");
    expect(captureProviderHeaders()).toEqual({ authorization: "******" });
  });

  it("missing_caller_fields_are_absent_and_backward_compatible", () => {
    // Records without caller fields project without crashing and gain none.
    for (const record of [PI_MAIN, PI_HAIKU, PI_MINI, PI_CLASSIFIER]) {
      const data = project(withoutCallerFields(record));
      expect(callerFields(data)).toEqual({});
      expect(data).toMatchObject({ model: record.model, requestId: record.request_id, path: record.path, xInitiator: "agent", status: 200 });
    }
  });

  it("awf_owned_purpose_wins_over_caller", () => {
    // The real classifier record keeps AWF's purpose and caller source, and
    // AWF omits agent_name, so no child name is fabricated.
    expect(PI_CLASSIFIER).toMatchObject({ model: "gpt-5.6-luna", path: "/responses" });
    expect(Object.hasOwn(PI_CLASSIFIER, "agent_name")).toBe(false);
    expect(callerFields(project(PI_CLASSIFIER))).toEqual({ purpose: "routing_classification", callerSource: "awf" });
  });
});
