// @ts-check

const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
const fixtures = require("./fixtures/model_routing_contract.cjs");
const copilot = require("./copilot_harness.cjs");
const claude = require("./claude_harness.cjs");
const codex = require("./codex_harness.cjs");
const pi = require("./pi_models_json.cjs");
const { ROUTING_REASONING_EFFORTS } = require("./awf_model_routing.cjs");
const { resolveModelRoutingOutcome } = require("./parse_token_usage.cjs");
const { recordModelRouting, resolveEffectiveModel, getEffectiveModelLabel } = require("./model_attribution.cjs");
const { writeUnifiedSession } = require("./unified_session.cjs");
const { sendJobConclusionSpan } = require("./send_otlp_span.cjs");

describe("cross-module model routing contract", () => {
  let root;
  let originalEnv;
  let originalCore;

  beforeEach(() => {
    originalEnv = { ...process.env };
    originalCore = global.core;
    root = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-model-routing-contract-"));
    process.env.GH_AW_TMP_DIR = root;
    process.env.GH_AW_MODEL_ROUTING = "1";
    process.env.GH_AW_MODEL_ROUTING_ENABLED = "true";
    process.env.GH_AW_INFO_MODEL = "auto";
    process.env.GH_AW_ENGINE_MODEL = "agent";
    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.INPUT_JOB_NAME = "agent";
    process.env.GITHUB_WORKFLOW = "model-routing-contract";
    global.core = { setOutput: vi.fn(), info: vi.fn(), warning: vi.fn() };
  });

  afterEach(() => {
    process.env = originalEnv;
    global.core = originalCore;
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
    fs.rmSync(root, { recursive: true, force: true });
  });

  function writeFixture(fixture) {
    process.env.GH_AW_ENGINE_ID = fixture.engine;
    const agentDir = path.join(root, "agent");
    fs.mkdirSync(agentDir, { recursive: true });
    fs.writeFileSync(path.join(agentDir, "awf-reflect.json"), JSON.stringify(fixture.reflectData));
    const routingPath = path.join(root, "sandbox/firewall/logs/api-proxy-logs/model-routing.jsonl");
    fs.mkdirSync(path.dirname(routingPath), { recursive: true });
    fs.writeFileSync(routingPath, fixture.proxyRecords.map(record => JSON.stringify(record)).join("\n") + "\n");
    const infoPath = path.join(root, "aw_info.json");
    fs.writeFileSync(infoPath, JSON.stringify({ model: "agent", requested_model: "auto", engine_id: fixture.engine }));
    return infoPath;
  }

  async function exerciseHarness(fixture) {
    const reflectData = fixture.reflectData;
    if (fixture.engine === "copilot") {
      return copilot.resolveCopilotModelRouting(reflectData, true, () => {});
    }
    if (fixture.engine === "claude") {
      try {
        const childEnv = await claude.buildClaudeChildEnv(reflectData, process.env, () => {});
        return { selection: reflectData.routing.selection, childEnv, error: null };
      } catch (error) {
        return { selection: null, error: error.message };
      }
    }
    if (fixture.engine === "codex") {
      return codex.resolveCodexModelRouting(reflectData, ["exec"]);
    }
    const resolved = pi.resolvePiModelRouting(reflectData);
    if (resolved.error || !resolved.selection) return resolved;
    const endpoint = pi.resolveAndRecordPiModelRoutingEndpoint({
      reflectData,
      modelId: resolved.selection.wire_model,
      api: fixture.api,
      selection: resolved.selection,
    });
    return { ...resolved, ...endpoint };
  }

  async function runContract(fixture) {
    const infoPath = writeFixture(fixture);
    const harness = await exerciseHarness(fixture);
    const advisory = JSON.parse(fs.readFileSync(path.join(root, "agent/awf-routing-outcome.json"), "utf8"));
    const resolved = resolveModelRoutingOutcome(process.env, root);
    const infoRouting = recordModelRouting(resolved, process.env, infoPath);
    const effective = resolveEffectiveModel(infoPath, "agent", process.env);
    const label = getEffectiveModelLabel(infoPath, "agent", process.env);
    const sessionEvents = writeUnifiedSession({ rootDir: root, engine: fixture.engine });

    const mockFetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", mockFetch);
    await sendJobConclusionSpan("gh-aw.agent.conclusion", { startMs: 1_700_000_000_000 });
    const spanBody = JSON.parse(mockFetch.mock.calls[0][1].body);
    const span = spanBody.resourceSpans[0].scopeSpans[0].spans[0];
    const otel = Object.fromEntries(span.attributes.map(attribute => [attribute.key, attribute.value.stringValue ?? attribute.value.intValue]));
    const unifiedOutcome = sessionEvents.find(event => event.type === "model_routing.outcome");
    const workflowInfo = sessionEvents.find(event => event.type === "workflow.info");
    return { harness, advisory, resolved, infoRouting, effective, label, unifiedOutcome, workflowInfo, otel };
  }

  const endpointCases = [
    ["Copilot /responses", fixtures.copilotResponses, "gpt-5.6-luna", "/responses"],
    ["Copilot /chat/completions", fixtures.copilotChatCompletions, "claude-sonnet-4-6", "/chat/completions"],
    ["Claude endpoint override", fixtures.claudeEndpointOverride, "claude-opus-4-6", "/v1/messages"],
    ["Codex endpoint override", fixtures.codexEndpointOverride, "gpt-5.6-luna", "/responses"],
    ["Pi Claude pick", fixtures.piClaudePick, "claude-opus-4-6", "/v1/messages"],
  ];

  it("resolves Pi's routing selection directly in-process", () => {
    const result = pi.resolvePiModelRouting(fixtures.piClaudePick.reflectData);
    expect(result).toMatchObject({
      error: null,
      selection: {
        wire_model: "claude-opus-4-6",
        endpoint: "/v1/messages",
        selected_endpoint: "/v1/messages",
        effort: "high",
        mapped_effort: "high",
      },
    });
  });

  it.each(endpointCases)("%s agrees across harness, attribution, session, and OTEL", async (_name, fixture, model, endpoint) => {
    const result = await runContract(fixture);
    const selectedEndpoint = fixture.reflectData.routing.selection.endpoint;
    expect(result.advisory).toMatchObject({ status: "selected", wire_model: model, endpoint, selected_endpoint: selectedEndpoint, effort: "high", applied_effort: "high" });
    expect(result.resolved).toMatchObject({ status: "selected", wire_model: model, endpoint, selected_endpoint: selectedEndpoint, effort: "high" });
    expect(result.infoRouting).toMatchObject({ status: "selected", wire_model: model, endpoint, selected_endpoint: selectedEndpoint, effort: "high" });
    expect(result.effective).toMatchObject({ model, effort: "high", routing: { status: "selected", endpoint, selected_endpoint: selectedEndpoint } });
    expect(result.label).toMatch(/^routed: \S+ high$/);
    expect(result.unifiedOutcome.data).toMatchObject({ status: "selected", wireModel: model, effectiveEndpoint: endpoint, selectedEndpoint, effort: "high" });
    expect(result.workflowInfo.data.modelRouting).toMatchObject({ status: "selected", wireModel: model, effectiveEndpoint: endpoint, selectedEndpoint, effort: "high" });
    expect(result.otel["gen_ai.request.model"]).toBe(model);
    expect(result.otel["gh-aw.model.effort"]).toBe("high");
    expect(result.otel["gh-aw.model_routing.status"]).toBe("selected");
  });

  const unsupportedEndpointCases = [
    ["copilot", fixtures.unsupportedCopilotEndpoint],
    ["claude", fixtures.unsupportedClaudeEndpoint],
    ["codex", fixtures.unsupportedCodexEndpoint],
    ["pi", fixtures.unsupportedPiEndpoint],
  ];

  it.each(unsupportedEndpointCases)("%s rejects an endpoint it cannot route", async (_engine, fixture) => {
    const result = await runContract(fixture);
    expect(result.advisory).toMatchObject({ status: "rejected", failure_code: "unsupported_endpoint" });
    expect(result.resolved).toMatchObject({ status: "rejected", failure_code: "unsupported_endpoint" });
    expect(result.infoRouting).toMatchObject({ status: "rejected", failure_code: "unsupported_endpoint" });
    expect(result.effective.model).toBe("");
    expect(result.label).toBe("routing rejected (unsupported_endpoint)");
    expect(result.unifiedOutcome.data).toMatchObject({ failureCode: "unsupported_endpoint" });
    expect(result.workflowInfo.data.modelRouting).toMatchObject({ status: "rejected", failureCode: "unsupported_endpoint" });
    expect(result.otel["gen_ai.request.model"]).toBeUndefined();
    expect(result.otel["gh-aw.model.effort"]).toBeUndefined();
    expect(result.otel["gh-aw.model_routing.status"]).toBe("rejected");
  });

  it("reports the harness selection even when proxy attribution rejects it", async () => {
    const result = await runContract(fixtures.harnessProxyMismatch);
    expect(result.advisory).toMatchObject({ status: "selected", wire_model: "gpt-5.6-luna" });
    expect(result.resolved).toMatchObject({ status: "rejected", failure_code: "harness_selection_mismatch" });
    expect(result.infoRouting).toMatchObject({ status: "rejected", failure_code: "harness_selection_mismatch" });
    expect(result.effective.model).toBe("");
    expect(result.label).toBe("routing rejected (harness_selection_mismatch)");
    expect(result.unifiedOutcome.data).toMatchObject({ status: "selected", wireModel: "gpt-5.6-luna" });
    expect(result.workflowInfo.data.modelRouting).toMatchObject({ status: "selected", wireModel: "gpt-5.6-luna" });
    expect(result.otel["gen_ai.request.model"]).toBeUndefined();
    expect(result.otel["gh-aw.model_routing.status"]).toBe("rejected");
  });

  const expectedEffort = {
    copilot: Object.fromEntries(ROUTING_REASONING_EFFORTS.map(effort => [effort, effort])),
    claude: Object.fromEntries(ROUTING_REASONING_EFFORTS.map(effort => [effort, ["low", "medium", "high", "xhigh", "max"].includes(effort) ? effort : null])),
    codex: Object.fromEntries(ROUTING_REASONING_EFFORTS.map(effort => [effort, ["minimal", "low", "medium", "high", "xhigh"].includes(effort) ? effort : null])),
    pi: Object.fromEntries(ROUTING_REASONING_EFFORTS.map(effort => [effort, effort === "none" ? "off" : effort])),
  };

  const effortCases = ["copilot", "claude", "codex", "pi"].flatMap(engine => ROUTING_REASONING_EFFORTS.map(effort => [engine, effort, expectedEffort[engine][effort]]));

  it.each(effortCases)("%s effort %s is classified end to end", async (engine, effort, appliedEffort) => {
    const base = engine === "copilot" ? fixtures.copilotResponses : engine === "claude" ? fixtures.claudeEndpointOverride : engine === "codex" ? fixtures.codexEndpointOverride : fixtures.piClaudePick;
    const fixture = structuredClone(base);
    fixture.reflectData.routing.selection.effort = effort;
    for (const record of fixture.proxyRecords) {
      if (record.stage === "selection") record.selected_effort = effort;
    }
    const result = await runContract(fixture);
    if (appliedEffort === null) {
      expect(result.advisory).toMatchObject({ status: "rejected", failure_code: "unsupported_effort" });
      expect(result.advisory.detail).toContain(effort);
      expect(result.advisory.detail).toContain(engine);
      expect(result.resolved).toMatchObject({ status: "rejected", failure_code: "unsupported_effort" });
      expect(result.infoRouting).toMatchObject({ status: "rejected", failure_code: "unsupported_effort" });
      expect(result.effective.model).toBe("");
      expect(result.label).toBe("routing rejected (unsupported_effort)");
      expect(result.unifiedOutcome.data).toMatchObject({ status: "rejected", failureCode: "unsupported_effort" });
      expect(result.workflowInfo.data.modelRouting).toMatchObject({ status: "rejected", failureCode: "unsupported_effort" });
      expect(result.otel["gen_ai.request.model"]).toBeUndefined();
      expect(result.otel["gh-aw.model.effort"]).toBeUndefined();
      expect(result.otel["gh-aw.model_routing.status"]).toBe("rejected");
      return;
    }
    expect(result.advisory).toMatchObject({ status: "selected", effort, applied_effort: appliedEffort });
    expect(result.resolved).toMatchObject({ status: "selected", effort });
    if (appliedEffort !== effort) expect(result.resolved.applied_effort).toBe(appliedEffort);
    expect(result.effective).toMatchObject({ model: fixture.reflectData.routing.selection.wire_model, effort: appliedEffort });
    expect(result.unifiedOutcome.data).toMatchObject({ status: "selected", effort, appliedEffort });
    expect(result.workflowInfo.data.modelRouting).toMatchObject({ status: "selected", effort, appliedEffort });
    expect(result.otel["gh-aw.model.effort"]).toBe(appliedEffort);
    if (engine === "claude") expect(result.harness.childEnv.CLAUDE_CODE_EFFORT_LEVEL).toBe(appliedEffort);
    if (engine === "codex") expect(result.harness.args).toContain(`model_reasoning_effort="${appliedEffort}"`);
    if (engine === "copilot") expect(process.env.GH_AW_COPILOT_ROUTING_EFFORT).toBe(appliedEffort);
    if (engine === "pi") expect(result.harness.selection.mapped_effort).toBe(appliedEffort);
  });
});
