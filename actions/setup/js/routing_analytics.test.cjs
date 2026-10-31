import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { normalizeRuntimeEvent, writeUnifiedSession } from "./unified_session.cjs";
import { normalizeUnifiedSessionEvent } from "./unified_session_payload.cjs";
import { validateSession } from "./scripts/validate_session.cjs";

const req = require;
const { computeInferenceAIC } = req("./model_costs.cjs");
const { estimateBaseline, persistModelConfigBaseline, projectModelConfig } = req("./routing_analytics.cjs");
const fixtures = req("./fixtures/routing_analytics_runs.cjs");
const { readJsonlFile } = req("./jsonl_helpers.cjs");

const { routed, unrouted, routedClaude } = fixtures.RUNS;
const NEW_INFO_KEYS = ["engine_harness", "workflow_ref", "server_url", "model_config"];
const roots = [];

const mockCore = { debug: vi.fn(), info: vi.fn(), warning: vi.fn(), error: vi.fn(), setFailed: vi.fn(), setOutput: vi.fn(), summary: { addRaw: vi.fn().mockReturnThis(), write: vi.fn().mockResolvedValue() } };

/**
 * Run generate_aw_info with the compiled env of the run's workflow and the
 * recorded run context, keeping its fixed /tmp/gh-aw writes in memory.
 */
async function generateAwInfo(run) {
  const recorded = fixtures.awInfo(run.id);
  const env = fixtures.compiledInfoEnv(run.workflow, recorded);
  const saved = { ...process.env };
  const files = new Map();
  const realRead = fs.readFileSync.bind(fs);
  const realExists = fs.existsSync.bind(fs);
  const realWrite = fs.writeFileSync.bind(fs);
  const realMkdir = fs.mkdirSync.bind(fs);
  const inTmp = filename => String(filename).startsWith("/tmp/gh-aw");
  const spies = [
    vi.spyOn(fs, "mkdirSync").mockImplementation((dir, ...args) => (inTmp(dir) ? undefined : realMkdir(dir, ...args))),
    vi.spyOn(fs, "writeFileSync").mockImplementation((filename, content, ...args) => (inTmp(filename) ? void files.set(String(filename), String(content)) : realWrite(filename, content, ...args))),
    vi.spyOn(fs, "existsSync").mockImplementation(filename => (inTmp(filename) ? files.has(String(filename)) : realExists(filename))),
    vi.spyOn(fs, "readFileSync").mockImplementation((filename, ...args) => {
      if (files.has(String(filename))) return files.get(String(filename));
      if (inTmp(filename)) throw Object.assign(new Error("missing"), { code: "ENOENT" });
      return realRead(filename, ...args);
    }),
  ];
  try {
    for (const key of Object.keys(process.env)) if (key.startsWith("GH_AW_") || key.startsWith("GITHUB_")) delete process.env[key];
    Object.assign(process.env, env, { GITHUB_RUN_ATTEMPT: recorded.run_attempt, GITHUB_WORKFLOW_REF: recorded.caller_workflow_ref, GITHUB_SERVER_URL: "https://github.com" });
    global.core = mockCore;
    const { main } = await import("./generate_aw_info.cjs");
    await main(mockCore, fixtures.runContext(recorded));
    return JSON.parse(files.get("/tmp/gh-aw/aw_info.json"));
  } finally {
    for (const spy of spies) spy.mockRestore();
    for (const key of Object.keys(process.env)) if (!(key in saved)) delete process.env[key];
    Object.assign(process.env, saved);
  }
}

/**
 * Copy a recorded run into a collector root. With `published`, the recorded
 * runtime aw_info gains the runner-published fields and the agent-phase
 * baseline, exactly as generate_aw_info and parse_token_usage write them.
 */
async function prepareRoot(run, { published = true } = {}) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), `routing-analytics-${run.id}-`));
  roots.push(root);
  fs.cpSync(fixtures.runDir(run.id), root, { recursive: true, filter: source => path.basename(source) !== "aw_session.jsonl" });
  if (!published) return root;
  const generated = await generateAwInfo(run);
  const recorded = fixtures.awInfo(run.id);
  const info = { ...recorded, ...Object.fromEntries(NEW_INFO_KEYS.filter(key => key in generated).map(key => [key, generated[key]])) };
  fs.writeFileSync(path.join(root, "aw_info.json"), JSON.stringify(info, null, 2));
  persistModelConfigBaseline({ GH_AW_TMP_DIR: root });
  fs.copyFileSync(path.join(root, "aw_info.json"), path.join(root, "runtime/aw_info.json"));
  fs.rmSync(path.join(root, "aw_info.json"));
  return root;
}

function collect(root) {
  const outputPath = path.join(root, "usage/aw_session.jsonl");
  writeUnifiedSession({ rootDir: root, outputPath, warn: () => {}, admission: {}, jobDurations: {} });
  const events = [...readJsonlFile(outputPath, { strict: true })];
  expect(validateSession(fs.readFileSync(outputPath, "utf8"))).toBe(events.length);
  return events;
}

const ofType = (events, type) => events.filter(event => event.type === type);

/** The proxy token usage of one run, as the four main tuples are recorded. */
const proxyTokenUsage = id => [...readJsonlFile(path.join(fixtures.runDir(id), "sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl"), { strict: true })];
const proxyRouting = id => [...readJsonlFile(path.join(fixtures.runDir(id), "sandbox/firewall/logs/api-proxy-logs/model-routing.jsonl"), { strict: true })];

describe("routing analytics in aw_session.jsonl", () => {
  let savedEnv;
  beforeEach(() => {
    savedEnv = { ...process.env };
  });
  afterEach(() => {
    for (const root of roots.splice(0)) fs.rmSync(root, { recursive: true, force: true });
    for (const key of Object.keys(process.env)) if (!(key in savedEnv)) delete process.env[key];
    Object.assign(process.env, savedEnv);
    vi.restoreAllMocks();
  });

  it("workflow_info_records_sdk_and_cli_identity", async () => {
    for (const [run, harness] of [
      [routed, "copilot-sdk"],
      [routedClaude, "copilot-sdk"],
      [unrouted, "copilot-cli"],
    ]) {
      const recorded = fixtures.awInfo(run.id);
      const generated = await generateAwInfo(run);
      expect(generated).toMatchObject({
        engine_id: "copilot",
        engine_harness: harness,
        workflow_id: recorded.workflow_id,
        workflow_ref: recorded.workflow_id,
        workflow_call_id: recorded.workflow_call_id,
        caller_workflow_ref: recorded.caller_workflow_ref,
        run_attempt: "1",
        server_url: "https://github.com",
      });
      expect(generated.workflow_ref).toMatch(new RegExp(`^github/gh-aw/\\.github/workflows/${run.workflow}\\.lock\\.yml@refs/heads/`));

      const [info] = ofType(collect(await prepareRoot(run)), "workflow.info");
      expect(info.data).toMatchObject({
        engineId: "copilot",
        engineHarness: harness,
        workflowRef: recorded.workflow_id,
        workflowId: recorded.workflow_id,
        workflowCallId: recorded.workflow_call_id,
        callerWorkflowRef: recorded.caller_workflow_ref,
        runAttempt: "1",
        serverUrl: "https://github.com",
      });
    }
  });

  it("model_config_records_routed_compiled_policy", async () => {
    const events = collect(await prepareRoot(routed));
    const configs = ofType(events, "model.config");
    expect(configs).toHaveLength(1);
    expect(configs[0].provenance).toMatchObject({ component: "workflow", phase: "agent" });
    expect(configs[0].data).toEqual({
      routing: { enabled: true, goal: "cost", mode: "economy", allowedModels: ["gpt-5.6-luna"], candidateModels: ["github-copilot/gpt-5.6-luna"] },
      models: { allowed: [], blocked: [] },
      maxAiCredits: 1000,
      maxAiCreditsSource: "default",
      baselineModel: "claude-sonnet-5.5",
    });
    // Configured policy, never the selection's resulting mode or routed model.
    expect(fixtures.awInfo(routed.id).model_routing.mode).toBe("balanced");
    expect(configs[0].data.baselineModel).not.toBe(fixtures.awInfo(routed.id).routed_model);
    // Supplied model allow/block policy is retained separately from routing candidates.
    const supplied = projectModelConfig({ routing: { enabled: true }, models: { allowed: ["github-copilot/*sonnet*"], blocked: ["openai/gpt-5*"] }, max_ai_credits: 250, max_ai_credits_source: "frontmatter" });
    expect(supplied).toEqual({ routing: { enabled: true }, models: { allowed: ["github-copilot/*sonnet*"], blocked: ["openai/gpt-5*"] }, maxAiCredits: 250, maxAiCreditsSource: "frontmatter" });
    // Native routing records its provider; Copilot routing omits it and unknown values are dropped.
    expect(projectModelConfig({ routing: { enabled: true, provider: "anthropic", candidate_models: ["claude-sonnet-5"] } })).toEqual({ routing: { enabled: true, provider: "anthropic", candidateModels: ["claude-sonnet-5"] } });
    expect(projectModelConfig({ routing: { enabled: true, provider: "copilot" } })).toEqual({ routing: { enabled: true } });
  });

  it("model_config_records_unrouted_resolved_baseline", async () => {
    const events = collect(await prepareRoot(unrouted));
    const configs = ofType(events, "model.config");
    expect(configs).toHaveLength(1);
    expect(configs[0].data.routing).toEqual({ enabled: false });
    expect(configs[0].data.baselineModel).toBe("claude-sonnet-5.5");
    expect(fixtures.awInfo(unrouted.id).requested_model).toBe("auto");
    expect(configs[0].data).toMatchObject({ maxAiCredits: 1000, maxAiCreditsSource: "default" });
    // Unrouted runs keep their configuration but omit the counterfactual estimate.
    expect(ofType(events, "model.baseline_estimate")).toEqual([]);
  });

  it("model_config_survives_selection_or_startup_failure", async () => {
    // Runner publication precedes selection: the compiled policy is already recorded.
    const published = await generateAwInfo(routedClaude);
    expect(published.model_routing).toBeUndefined();
    expect(published.model_config).toEqual({
      routing: { enabled: true, goal: "cost", mode: "economy", allowed_models: ["claude-haiku-4.5"], candidate_models: ["github-copilot/claude-haiku-4.5"] },
      models: { allowed: [], blocked: [] },
      max_ai_credits: 1000,
      max_ai_credits_source: "default",
    });

    const events = collect(await prepareRoot(routedClaude));
    expect(ofType(events, "model.resolution").map(event => event.data)).toEqual([expect.objectContaining({ role: "mini-whoami", status: "failed", cause: "model_unavailable" })]);
    const configs = ofType(events, "model.config");
    expect(configs).toHaveLength(1);
    expect(configs[0].data).toEqual({
      routing: { enabled: true, goal: "cost", mode: "economy", allowedModels: ["claude-haiku-4.5"], candidateModels: ["github-copilot/claude-haiku-4.5"] },
      models: { allowed: [], blocked: [] },
      maxAiCredits: 1000,
      maxAiCreditsSource: "default",
      baselineModel: "claude-sonnet-5.5",
    });
    expect(fixtures.awInfo(routedClaude.id).model_routing.mode).toBe("unknown");
    // No main-agent inference followed selection, so the estimate is explicitly incomplete.
    const [estimate] = ofType(events, "model.baseline_estimate");
    expect(estimate.data).toMatchObject({ estimated: true, mainRequests: 0, pricedRequests: 0, complete: false });
    expect(estimate.data.aic).toBeUndefined();
  });

  it("baseline_estimate_reprices_only_main_requests", async () => {
    const events = collect(await prepareRoot(routed));
    const estimates = ofType(events, "model.baseline_estimate");
    expect(estimates).toHaveLength(1);
    expect(estimates[0].provenance).toMatchObject({ component: "collector", phase: "conclusion" });

    const mainIds = new Set(
      proxyRouting(routed.id)
        .filter(record => record.stage === "request" && record.routed === "as_selected")
        .map(record => record.request_id)
    );
    const main = proxyTokenUsage(routed.id).filter(record => mainIds.has(record.request_id));
    expect(main.map(record => [record.input_tokens, record.output_tokens, record.cache_read_tokens, record.cache_write_tokens])).toEqual([
      [3, 175, 0, 18516],
      [3, 61, 18516, 339],
      [3, 210, 18855, 76],
      [3, 60, 18931, 228],
    ]);
    // Copilot's provider-default cache semantics: cache reads are subtracted from input.
    const expected = main.reduce(
      (sum, record) =>
        sum +
        computeInferenceAIC({
          provider: "copilot",
          model: "claude-sonnet-5.5",
          inputTokens: record.input_tokens,
          outputTokens: record.output_tokens,
          cacheReadTokens: record.cache_read_tokens,
          cacheWriteTokens: record.cache_write_tokens,
          reasoningTokens: 0,
        }),
      0
    );
    expect(expected).toBeGreaterThan(0);
    expect(estimates[0].data).toEqual({ estimated: true, baselineModel: "claude-sonnet-5.5", aic: Number(expected.toFixed(6)), mainRequests: 4, pricedRequests: 4, unpricedRequests: 0, unattributedRequests: 0, complete: true });

    // Sub-agent, classifier and detection usage are present but excluded.
    const usage = ofType(events, "firewall.token_usage");
    expect(usage.filter(event => event.provenance.phase === "detection")).toHaveLength(2);
    expect(usage.filter(event => event.provenance.phase === "agent")).toHaveLength(6);
    // Replicated proxy copies of the same request are counted once.
    const config = ofType(events, "model.config")[0].data;
    const tokenUsage = proxyTokenUsage(routed.id);
    const doubled = estimateBaseline({ config, subAgentModels: fixtures.awInfo(routed.id).sub_agent_models, tokenUsage: [...tokenUsage, ...tokenUsage], routing: proxyRouting(routed.id) });
    expect(doubled).toEqual(estimates[0].data);
  });

  it("baseline_estimate_reports_unknown_pricing_and_coverage", () => {
    const info = fixtures.awInfo(routed.id);
    const tokenUsage = proxyTokenUsage(routed.id);
    const routing = proxyRouting(routed.id);
    const base = { routing: { enabled: true }, baselineModel: "claude-sonnet-5.5" };
    const estimate = config => estimateBaseline({ config, subAgentModels: info.sub_agent_models, tokenUsage, routing });

    // Unknown baseline pricing is unpriced, never a complete zero.
    const unknown = estimate({ ...base, baselineModel: "no-such-model-for-pricing" });
    expect(unknown).toEqual({ estimated: true, baselineModel: "no-such-model-for-pricing", mainRequests: 4, pricedRequests: 0, unpricedRequests: 4, unattributedRequests: 0, complete: false });
    // An unknown baseline model is unpriced and incomplete.
    expect(estimate({ routing: { enabled: true } })).toMatchObject({ mainRequests: 4, pricedRequests: 0, unpricedRequests: 4, complete: false });
    expect(estimate({ routing: { enabled: true } }).aic).toBeUndefined();

    // A request with unknown token counts is unpriced; the priced subtotal is incomplete.
    const partial = estimateBaseline({ config: base, subAgentModels: info.sub_agent_models, tokenUsage: tokenUsage.map(record => (record.request_id.startsWith("962c7fa8") ? { ...record, output_tokens: "n/a" } : record)), routing });
    expect(partial).toMatchObject({ mainRequests: 4, pricedRequests: 3, unpricedRequests: 1, complete: false });
    expect(partial.aic).toBeGreaterThan(0);

    // Requests without proxy request attribution are not silently included.
    const ambiguous = estimateBaseline({ config: base, subAgentModels: info.sub_agent_models, tokenUsage, routing: routing.filter(record => !String(record.request_id).startsWith("786666b6")) });
    expect(ambiguous).toMatchObject({ mainRequests: 3, pricedRequests: 3, unattributedRequests: 1, complete: false });
    // An as_selected request matching a declared sub-agent is ambiguous, not main.
    const declared = estimateBaseline({ config: base, subAgentModels: [{ model: "gpt-5.6-luna", patterns: ["github-copilot/gpt-5.6-luna"] }], tokenUsage, routing });
    expect(declared).toMatchObject({ mainRequests: 0, unattributedRequests: 5, complete: false });

    // Unrouted (or unknown routing) runs omit the estimate.
    expect(estimate({ routing: { enabled: false }, baselineModel: "claude-sonnet-5.5" })).toBeUndefined();
    expect(estimate(undefined)).toBeUndefined();
  });

  it("upstream_error_preserves_identifiers_without_payloads", () => {
    const records = [...readJsonlFile(path.join(fixtures.DIR, "awf-10211/upstream-errors.jsonl"), { strict: true })];
    const projected = records.map(record => normalizeUnifiedSessionEvent(normalizeRuntimeEvent("firewall", record), "agent"));
    expect(projected.map(event => event.type)).toEqual(Array(4).fill("firewall.upstream_error"));
    expect(projected.map(event => event.data)).toEqual([
      { requestId: "req-1", provider: "copilot", model: "claude-haiku-4.5", httpStatus: 400, errorCode: "invalid_reasoning_effort" },
      { requestId: "req-2", provider: "copilot", model: "gpt-5.6-luna", httpStatus: 400, errorCode: "c", errorType: "t", errorParam: "p" },
      { requestId: "req-3", provider: "copilot", model: "claude-haiku-4.5", httpStatus: 400, errorType: "overloaded_error" },
      { requestId: "req-4", provider: "copilot", model: "claude-haiku-4.5", httpStatus: 400 },
    ]);
    const serialized = JSON.stringify(projected);
    for (const fragment of ["response_body", "responseBody", "response_headers", "content-type", "output_config", "does not support", "The requested model", '"m"']) {
      expect(serialized).not.toContain(fragment);
    }
    // Non-identifier codes are dropped rather than copied.
    const invalid = normalizeUnifiedSessionEvent(normalizeRuntimeEvent("firewall", { ...records[0], error_code: "bad code with spaces" }), "agent");
    expect(invalid.data.errorCode).toBeUndefined();
  });

  it("failed_request_preserves_error_source", () => {
    const records = [...readJsonlFile(path.join(fixtures.DIR, "awf-10211/model-routing.jsonl"), { strict: true })];
    const projected = records.map(record => normalizeUnifiedSessionEvent(normalizeRuntimeEvent("firewall", record), "agent"));
    expect(projected.map(event => event.type)).toEqual(["firewall.model_routing", "firewall.model_routing"]);
    expect(projected[0].data).toMatchObject({ stage: "request", requestId: "local-req-5", outcome: "failed", status: 400, errorCode: "invalid_reasoning_effort", errorSource: "upstream" });
    expect(projected[0].data).not.toHaveProperty("errorType");
    expect(projected[1].data).toMatchObject({ stage: "request", requestId: "request-1", outcome: "failed", status: 400, errorCode: "unsupported_reasoning_effort", errorType: "invalid_request_error", errorSource: "awf" });
    const invalid = normalizeUnifiedSessionEvent(normalizeRuntimeEvent("firewall", { ...records[1], error_source: "agent", error_type: "free form text" }), "agent");
    expect(invalid.data).not.toHaveProperty("errorSource");
    expect(invalid.data).not.toHaveProperty("errorType");
  });

  it("older_sessions_keep_missing_routing_data_unknown", async () => {
    for (const run of [routed, unrouted, routedClaude]) {
      const recorded = fixtures.session(run.id);
      const content = fs.readFileSync(path.join(fixtures.runDir(run.id), "aw_session.jsonl"), "utf8");
      expect(validateSession(content)).toBe(recorded.length);
      const [info] = ofType(recorded, "workflow.info");
      expect(info.data).toMatchObject({ engineId: "copilot", runAttempt: "1" });
      for (const key of ["engineHarness", "workflowRef", "serverUrl"]) expect(info.data).not.toHaveProperty(key);
      expect(ofType(recorded, "model.config")).toEqual([]);
      expect(ofType(recorded, "model.baseline_estimate")).toEqual([]);

      // Re-collecting the unmodified older artifacts invents no routing metadata.
      const events = collect(await prepareRoot(run, { published: false }));
      const [collected] = ofType(events, "workflow.info");
      for (const key of ["engineHarness", "workflowRef", "serverUrl"]) expect(collected.data).not.toHaveProperty(key);
      expect(ofType(events, "model.config")).toEqual([]);
      expect(ofType(events, "model.baseline_estimate")).toEqual([]);
    }
  });
});
