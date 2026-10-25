import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { collectUnifiedSession, mergeSessionSources, normalizeRuntimeEvent, parseEngineSession, sessionTimestamp, writeUnifiedSession } from "./unified_session.cjs";
import { serializeSessionArtifact, writeSessionArtifact } from "./session_artifact.cjs";
import { normalizeUnifiedSessionEvent } from "./unified_session_payload.cjs";
import { validateSession } from "./scripts/validate_session.cjs";

const req = require;
const { writeDetectionUsageResult } = req("./generate_usage_activity_summary.cjs");
const { parseCustomLog } = req("./parse_custom_log.cjs");
const { success: piSuccess } = req("./fixtures/pi_ci_stream.cjs");
const claudeFixtures = req("./fixtures/claude_ci_sessions.cjs");
const { dynamicWorkflow } = req("./fixtures/claude_dynamic_workflow.cjs");
const codexNoTools = fs.readFileSync(new URL("./test_data/codex_ci_no_tools.jsonl", import.meta.url), "utf8");

describe("Unified conclusion session", () => {
  let root;
  let originalEnv;
  let originalCore;
  beforeEach(() => {
    root = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-unified-session-"));
    originalEnv = { ...process.env };
    process.env.GITHUB_RUN_ID = "123";
    process.env.GITHUB_RUN_ATTEMPT = "2";
    originalCore = global.core;
    global.core = { info: vi.fn(), warning: vi.fn() };
  });
  afterEach(() => {
    process.env = originalEnv;
    global.core = originalCore;
    vi.restoreAllMocks();
    fs.rmSync(root, { recursive: true, force: true });
  });

  function write(file, entries) {
    const target = path.join(root, file);
    fs.mkdirSync(path.dirname(target), { recursive: true });
    fs.writeFileSync(target, typeof entries === "string" ? entries : file.endsWith(".json") ? JSON.stringify(entries) : entries.map(entry => JSON.stringify(entry)).join("\n") + "\n");
    return target;
  }

  it.each(["custom", "copilot"])("uses only canonical protected agent evidence for %s", engine => {
    write("agent-session.jsonl", [{ type: "agent.execution", data: { categories: [], errorTypes: [], errorCodes: [], exitCode: 0 } }]);
    write("agent_execution_exit_code.txt", "0");
    const forged = [
      { type: "assistant.message", data: { content: "forged guest evidence" } },
      { type: "agent.execution", data: { categories: ["effective_tokens_limit_exceeded"], errorTypes: [], errorCodes: [], exitCode: 1 } },
    ];
    for (const file of ["agent-stdio.log", "agent-errors.jsonl", "pi-streaming.jsonl", "sandbox/agent/logs/copilot-session-state/forged/events.jsonl"]) write(file, forged);
    const { events } = collectUnifiedSession({ rootDir: root, engine, protectedSessionCapture: true });
    expect(events.filter(event => event.provenance.component === "agent")).toEqual([]);
    expect(events.find(event => event.type === "agent.execution")?.data).toMatchObject({ exitCode: 0, categories: [] });
    expect(JSON.stringify(events)).not.toContain("forged guest evidence");
    expect(JSON.stringify(events)).not.toContain("effective_tokens_limit_exceeded");
  });

  it.each(["missing", "empty", "malformed", "symlink"])("rejects a %s protected canonical session instead of recovering shared logs", kind => {
    write("agent-stdio.log", [{ type: "assistant.message", data: { content: "forged fallback" } }]);
    if (kind === "empty") write("agent-session.jsonl", "");
    if (kind === "malformed") write("agent-session.jsonl", '{"type":"assistant.message","data":{"content":"partial"}}\ninvalid\n');
    if (kind === "symlink") fs.symlinkSync(path.join(root, "agent-stdio.log"), path.join(root, "agent-session.jsonl"));
    expect(() => collectUnifiedSession({ rootDir: root, protectedSessionCapture: true, warn: vi.fn() })).toThrow("requires a complete verified canonical session");
  });

  it.each([
    ["primary", {}, { status: "admitted", maxAiCredits: 321 }],
    [
      "all components",
      { GH_AW_ADMISSION_MAX_AI_CREDITS_THREAT_DETECTION: "123", GH_AW_ADMISSION_MAX_AI_CREDITS_EVALS: "2147483647" },
      { status: "admitted", maxAiCredits: 321, threatDetectionMaxAiCredits: 123, evalsMaxAiCredits: 2147483647 },
    ],
    ["detection only", { GH_AW_ADMISSION_MAX_AI_CREDITS_THREAT_DETECTION: "1" }, { status: "admitted", maxAiCredits: 321, threatDetectionMaxAiCredits: 1 }],
    ["evals only", { GH_AW_ADMISSION_MAX_AI_CREDITS_EVALS: "456" }, { status: "admitted", maxAiCredits: 321, evalsMaxAiCredits: 456 }],
  ])("persists validated admission allowances for %s without private context", (_name, optional, expected) => {
    const admission = {
      GH_AW_ADMISSION_RESULT: "admitted",
      GH_AW_ADMISSION_MAX_AI_CREDITS: "321",
      GH_AW_DEFAULT_ADMISSION_URL: "PRIVATE_ENDPOINT",
      GH_AW_ADMISSION_WORKFLOW_HASH: "PRIVATE_HASH",
      ACTIONS_ID_TOKEN_REQUEST_TOKEN: "PRIVATE_TOKEN",
      GITHUB_ACTOR: "PRIVATE_ACTOR",
      ...optional,
    };
    const events = writeUnifiedSession({ rootDir: root, admission });
    expect(events.filter(event => event.type === "guardrail.admission")).toEqual([{ type: "guardrail.admission", data: expected, provenance: { component: "guardrail", phase: "admission", path: "usage/aw_session.jsonl", index: 0 } }]);
    const persisted = fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8");
    expect(persisted).not.toContain("PRIVATE_");
    expect(validateSession(persisted)).toBe(events.length);
    expect(writeUnifiedSession({ rootDir: root, admission }).filter(event => event.type === "guardrail.admission")).toHaveLength(1);
  });

  it.each(["ignored", "skipped"])("records %s without stale service allowances", status => {
    const { events } = collectUnifiedSession({ rootDir: root, admission: { GH_AW_ADMISSION_RESULT: status, GH_AW_ADMISSION_MAX_AI_CREDITS: "321", GH_AW_ADMISSION_MAX_AI_CREDITS_EVALS: "123" } });
    expect(events.filter(event => event.type === "guardrail.admission")).toMatchObject([{ data: { status } }]);
    expect(events.find(event => event.type === "guardrail.admission").data).toEqual({ status });
  });

  it.each(["", "0", "-1", "1.5", "1e2", "01", "2147483648", "PRIVATE_INVALID", "$(echo PRIVATE_PAYLOAD)"])("rejects invalid admission allowances without exposing values: %s", raw => {
    const warn = vi.fn();
    const { events } = collectUnifiedSession({ rootDir: root, admission: { GH_AW_ADMISSION_RESULT: "admitted", GH_AW_ADMISSION_MAX_AI_CREDITS: raw }, warn });
    expect(events.some(event => event.type === "guardrail.admission")).toBe(false);
    expect(warn).toHaveBeenCalledWith(expect.stringContaining("invalid_admission_metadata"));
    expect(JSON.stringify([events, warn.mock.calls])).not.toContain("PRIVATE_");
  });

  it("rejects invalid optional allowances and unknown decisions atomically", () => {
    for (const admission of [{ GH_AW_ADMISSION_RESULT: "admitted", GH_AW_ADMISSION_MAX_AI_CREDITS: "123", GH_AW_ADMISSION_MAX_AI_CREDITS_EVALS: "0" }, { GH_AW_ADMISSION_RESULT: "PRIVATE_STATUS" }]) {
      const warn = vi.fn();
      const { events } = collectUnifiedSession({ rootDir: root, admission, warn });
      expect(events.some(event => event.type === "guardrail.admission")).toBe(false);
      expect(warn).toHaveBeenCalled();
      expect(JSON.stringify([events, warn.mock.calls])).not.toContain("PRIVATE_");
    }
    expect(collectUnifiedSession({ rootDir: root, admission: {} }).events.some(event => event.type === "guardrail.admission")).toBe(false);
  });

  it("grades the complete redacted log before persisting conclusion results", () => {
    write("aw_info.json", { engine_id: "copilot", run_id: 123, run_attempt: 2 });
    write("agent-session.jsonl", [
      { type: "tool.execution_start", data: { toolName: "view", toolCallId: "a", input: { path: "private-one" } } },
      { type: "tool.execution_complete", data: { toolCallId: "a", success: true } },
      { type: "tool.execution_start", data: { toolName: "view", toolCallId: "b", input: { path: "private-two" } } },
      { type: "tool.execution_complete", data: { toolCallId: "b", success: true } },
    ]);
    write("agent-stdio.log", "::add-mask::private-one\n::add-mask::private-two\n");
    write("safe-output-items.jsonl", [{ type: "issue", number: 1 }]);
    write("evals.jsonl", [{ runid: "123", id: "goal", answer: "YES" }]);
    const events = writeUnifiedSession({ rootDir: root, graders: [] });
    const output = JSON.parse(fs.readFileSync(path.join(root, "usage/graders/grader_results.json"), "utf8"));
    expect(output.results.find(result => result.id === "loops").value).toBe(1);
    expect(output.results.find(result => result.id === "artifact-production").value).toBe(1);
    expect(output.context.evals.byQuestion.goal).toBe("YES");
    const resultEvent = events.find(event => event.type === "grader.result");
    expect(resultEvent.provenance).toMatchObject({ component: "grader", phase: "conclusion" });
    expect(resultEvent.data.results).toMatchObject(output.results.map(({ id, value, status }) => ({ id, value, status })));
    expect(events.find(event => event.type === "session.collection").data.absentComponents).not.toContain("grader");
    expect(events.find(event => event.type === "session.collection").data.sources.filter(source => source.component === "grader")).toHaveLength(2);
    const persisted = fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8");
    expect(persisted).not.toContain("private-one");
    expect(persisted).not.toContain("private-two");
    expect(persisted.trim().split("\n").map(JSON.parse)).toEqual(events);
    expect(validateSession(persisted)).toBe(events.length);
    expect(writeUnifiedSession({ rootDir: root, graders: [] }).filter(event => event.type === "grader.result")).toEqual([resultEvent]);
  });

  it("removes stale grading artifacts when the unified session cannot be published", () => {
    write("usage/graders/grader_manifest.json", {});
    write("usage/graders/grader_results.json", {});
    expect(() => writeUnifiedSession({ rootDir: root, graders: [{ id: "custom", script: "return 1" }] })).toThrow(/custom graders/);
    for (const file of ["aw_session.jsonl", "graders/grader_manifest.json", "graders/grader_results.json"]) {
      expect(fs.existsSync(path.join(root, "usage", file))).toBe(false);
    }
  });

  it("records custom IDs without executing code found in collected evidence", () => {
    const marker = path.join(root, "must-not-exist");
    const script = `require("node:fs").writeFileSync(${JSON.stringify(marker)}, "executed")`;
    write("graders/grader_manifest.json", { graders: [{ id: "custom-count", source: "inline", script }] });
    write("agent-session.jsonl", [{ type: "assistant.message", data: { content: script } }]);
    const events = writeUnifiedSession({ rootDir: root, graders: [], customGraderIds: ["custom-count"] });
    const manifest = events.find(event => event.type === "grader.manifest").data;
    expect(manifest.customGraders).toEqual(["custom-count"]);
    expect(manifest.graders.every(grader => grader.source === "builtin" && !Object.hasOwn(grader, "script"))).toBe(true);
    expect(fs.existsSync(marker)).toBe(false);
    expect(validateSession(fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8"))).toBe(events.length);
  });

  it("persists dependency job outcomes without outputs or invented timing", () => {
    process.env.GH_AW_JOB_RESULTS = JSON.stringify({
      safe_outputs: { result: "failure", outputs: { token: "PRIVATE_OUTPUT" } },
      activation: { result: "success", outputs: { activated: "true" } },
      detection: { result: "cancelled" },
      "custom-job": { result: "skipped", conclusion: "PRIVATE_CONCLUSION" },
    });
    const events = writeUnifiedSession({ rootDir: root });
    const results = events.filter(event => event.type === "workflow.job_result");
    expect(results).toEqual(
      [
        ["activation", "success"],
        ["custom-job", "skipped"],
        ["detection", "cancelled"],
        ["safe_outputs", "failure"],
      ].map(([jobId, result], index) => ({
        type: "workflow.job_result",
        data: { jobId, result },
        provenance: { component: "workflow", phase: "conclusion", path: "usage/aw_session.jsonl", index },
      }))
    );
    const persisted = fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8");
    expect(persisted).not.toContain("PRIVATE_");
    expect(persisted.trim().split("\n").map(JSON.parse)).toEqual(events);
    expect(writeUnifiedSession({ rootDir: root })).toEqual(events);
  });

  it.each([0, 1234])("persists observed job duration %s in milliseconds", durationMs => {
    process.env.GH_AW_JOB_RESULTS = '{"agent":{"result":"failure"},"detection":{"result":"skipped"}}';
    writeUnifiedSession({ rootDir: root, jobDurations: { agent: durationMs, detection: 0 } });
    const events = fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8").trim().split("\n").map(JSON.parse);
    expect(events.filter(event => event.type === "workflow.job_result").map(event => event.data)).toEqual([
      { jobId: "agent", result: "failure", durationMs },
      { jobId: "detection", result: "skipped" },
    ]);
  });

  it.each([-1, 0.5, NaN, Infinity])("omits invalid duration %s without losing the job outcome", durationMs => {
    const { events } = collectUnifiedSession({ rootDir: root, jobResults: '{"agent":{"result":"success"}}', jobDurations: { agent: durationMs } });
    expect(events.find(event => event.type === "workflow.job_result").data).toEqual({ jobId: "agent", result: "success" });
  });

  it.each([undefined, "", "{}"])("does not fabricate job outcomes without dependency evidence: %s", jobResults => {
    const warn = vi.fn();
    delete process.env.GH_AW_JOB_RESULTS;
    const { events } = collectUnifiedSession({ rootDir: root, jobResults, warn });
    expect(events.some(event => event.type === "workflow.job_result")).toBe(false);
    expect(warn).not.toHaveBeenCalled();
  });

  it.each(["invalid PRIVATE_JSON", "null", "[]", "42", '"PRIVATE_VALUE"'])("warns without exposing invalid dependency metadata: %s", jobResults => {
    const warn = vi.fn();
    const { events } = collectUnifiedSession({ rootDir: root, jobResults, warn });
    expect(events.some(event => event.type === "workflow.job_result")).toBe(false);
    expect(warn).toHaveBeenCalledExactlyOnceWith("Unified session: invalid_job_results in usage/aw_session.jsonl");
    expect(events.find(event => event.type === "session.collection_warning").data.code).toBe("invalid_job_results");
    expect(JSON.stringify(events)).not.toContain("PRIVATE_");
  });

  it.each([null, [], "PRIVATE_VALUE", {}, { result: "" }, { result: "timed_out" }, { result: 0 }])("skips invalid job results while retaining valid outcomes: %j", job => {
    const warn = vi.fn();
    const jobResults = JSON.stringify({ invalid: job, agent: { result: "failure" } });
    const { events } = collectUnifiedSession({ rootDir: root, jobResults, warn });
    expect(events.filter(event => event.type === "workflow.job_result").map(event => event.data)).toEqual([{ jobId: "agent", result: "failure" }]);
    expect(warn).toHaveBeenCalledExactlyOnceWith("Unified session: invalid_job_result in usage/aw_session.jsonl");
    expect(JSON.stringify(events)).not.toContain("PRIVATE_");
  });

  it("rejects invalid job IDs without publishing them", () => {
    const warn = vi.fn();
    const { events } = collectUnifiedSession({ rootDir: root, jobResults: '{"PRIVATE ID":{"result":"success"}}', warn });
    expect(events.some(event => event.type === "workflow.job_result")).toBe(false);
    expect(warn).toHaveBeenCalledExactlyOnceWith("Unified session: invalid_job_result in usage/aw_session.jsonl");
    expect(JSON.stringify(events)).not.toContain("PRIVATE ID");
  });

  it.each(["agent", "detection"])("persists the complete AWF model inventory for the %s phase without credentials", phase => {
    const prefix = phase === "agent" ? "" : "threat-detection/";
    const models = Array.from({ length: 20 }, (_, index) => `model-${index}`);
    const reflect = {
      models_fetch_complete: false,
      endpoints: [
        {
          provider: "copilot",
          models,
          routing_models: [{ model_id: "model-0", supported_endpoints: ["/responses", "ws:/responses"], apiKey: "PRIVATE_ROUTING_KEY" }],
          headers: { authorization: "PRIVATE_TOKEN" },
          base_url: "https://PRIVATE_URL",
        },
      ],
      token: "PRIVATE_TOKEN",
    };
    write(`${prefix}agent/awf-reflect.json`, reflect);
    write(`${prefix}sandbox/firewall/awf-reflect.json`, { endpoints: [{ provider: "stale", models: ["stale"] }] });
    write(`${prefix}sandbox/firewall/models.json`, { providers: { copilot: { models: models.map(id => ({ id, apiKey: "PRIVATE_KEY" })), headers: { authorization: "PRIVATE_TOKEN" } } } });
    const events = writeUnifiedSession({ rootDir: root });
    expect(events.filter(event => event.type === "model.inventory")).toEqual([
      {
        type: "model.inventory",
        data: { endpoints: [{ provider: "copilot", models, routingModels: [{ modelId: "model-0", supportedEndpoints: ["/responses", "ws:/responses"] }] }], modelsFetchComplete: false },
        provenance: { component: "firewall", phase, path: `${prefix}agent/awf-reflect.json`, index: 0 },
      },
      {
        type: "model.inventory",
        data: { endpoints: [{ provider: "copilot", models }] },
        provenance: { component: "firewall", phase, path: `${prefix}sandbox/firewall/models.json`, index: 0 },
      },
    ]);
    const persisted = fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8");
    expect(persisted).not.toContain("PRIVATE_");
    expect(persisted).not.toContain("stale");
    expect(writeUnifiedSession({ rootDir: root })).toEqual(events);
  });

  it.each([{ endpoints: [{ provider: "copilot", models: [{ id: "a" }, { name: "b" }, { model: "c" }] }] }, { providers: { copilot: { available_models: ["a", "b", "c"] } } }, { provider: "copilot", models: ["a", "b", "c"] }])(
    "collects supported runtime model list shapes without opaque model metadata: %j",
    inventory => {
      write("sandbox/firewall/models.json", inventory);
      expect(collectUnifiedSession({ rootDir: root }).events.find(event => event.type === "model.inventory").data).toEqual({ endpoints: [{ provider: "copilot", models: ["a", "b", "c"] }] });
    }
  );

  it("uses the firewall reflect copy when the agent copy is absent and preserves empty inventories", () => {
    write("sandbox/firewall/awf-reflect.json", { endpoints: [], models_fetch_complete: true });
    expect(collectUnifiedSession({ rootDir: root }).events.find(event => event.type === "model.inventory")).toMatchObject({
      data: { endpoints: [], modelsFetchComplete: true },
      provenance: { path: "sandbox/firewall/awf-reflect.json" },
    });
  });

  it.each([
    "not-json",
    "null",
    "[]",
    "{}",
    '{"providers":[]}',
    '{"endpoints":[null]}',
    '{"endpoints":[{"provider":42,"models":[]}]}',
    '{"endpoints":[{"provider":"copilot","models":{}}]}',
    '{"endpoints":[{"provider":"copilot","models":null}]}',
    '{"endpoints":[{"provider":"copilot","models":[{"id":{}}]}]}',
    '{"providers":{"copilot":null}}',
    '{"providers":{"copilot":{"models":["a",false]}}}',
    '{"provider":"copilot","models":["a",42]}',
    '{"endpoints":[{"provider":"copilot","models":[],"routing_models":[{"model_id":42}]}]}',
    '{"endpoints":[{"provider":"copilot","configured":true,"models":["a"],"routing_models":[{"model_id":"a","supported_reasoning_efforts":["very high"]}]}]}',
    '{"endpoints":[{"provider":"copilot","configured":true,"models":["a"],"routing_models":[{"model_id":"a","context_window_tokens":0}]}]}',
  ])("warns once and skips malformed model inventories: %s", content => {
    write("sandbox/firewall/models.json", content);
    const warn = vi.fn();
    const { events } = collectUnifiedSession({ rootDir: root, warn });
    expect(events.some(event => event.type === "model.inventory")).toBe(false);
    const code = content === "not-json" ? "malformed_json" : "invalid_model_inventory";
    expect(warn).toHaveBeenCalledExactlyOnceWith(`Unified session: ${code} in sandbox/firewall/models.json`);
    expect(events.filter(event => event.type === "session.collection_warning").map(event => event.data)).toEqual([{ path: "sandbox/firewall/models.json", code }]);
  });

  it.each(["agent", "detection"])("falls back after corrupt or invalid preferred reflect inventories in the %s phase", phase => {
    const prefix = phase === "agent" ? "" : "threat-detection/";
    const fallback = { endpoints: [{ provider: "copilot", models: ["fallback"], routing_models: [{ model_id: "fallback", supported_endpoints: ["/responses"] }] }], models_fetch_complete: true };
    write(`${prefix}sandbox/firewall/awf-reflect.json`, fallback);
    write(`${prefix}sandbox/firewall/models.json`, { provider: "copilot", models: ["runtime"] });
    for (const [preferred, code] of [
      ["not-json", "malformed_json"],
      [{ endpoints: [{ provider: "copilot", models: "invalid" }] }, "invalid_model_inventory"],
    ]) {
      write(`${prefix}agent/awf-reflect.json`, preferred);
      const warn = vi.fn();
      const { events } = collectUnifiedSession({ rootDir: root, warn });
      expect(events.filter(event => event.type === "model.inventory")).toEqual([
        {
          type: "model.inventory",
          data: { endpoints: [{ provider: "copilot", models: ["fallback"], routingModels: [{ modelId: "fallback", supportedEndpoints: ["/responses"] }] }], modelsFetchComplete: true },
          provenance: { component: "firewall", phase, path: `${prefix}sandbox/firewall/awf-reflect.json`, index: 0 },
        },
        {
          type: "model.inventory",
          data: { endpoints: [{ provider: "copilot", models: ["runtime"] }] },
          provenance: { component: "firewall", phase, path: `${prefix}sandbox/firewall/models.json`, index: 0 },
        },
      ]);
      expect(warn).toHaveBeenCalledExactlyOnceWith(`Unified session: ${code} in ${prefix}agent/awf-reflect.json`);
      expect(events.filter(event => event.type === "session.collection_warning").map(event => event.data)).toEqual([{ path: `${prefix}agent/awf-reflect.json`, code }]);
    }
  });

  it.each([
    [
      { endpoints: [], models_fetch_complete: true },
      { endpoints: [], modelsFetchComplete: true },
    ],
    [{ providers: {} }, { endpoints: [] }],
    [{ provider: "copilot", models: [] }, { endpoints: [{ provider: "copilot", models: [] }] }],
    [{ endpoints: [{ provider: "copilot", configured: false, models: null }] }, { endpoints: [{ provider: "copilot", configured: false }] }],
  ])("keeps genuine empty or unconfigured preferred inventories instead of stale mirrors: %j", (inventory, expected) => {
    write("agent/awf-reflect.json", inventory);
    write("sandbox/firewall/awf-reflect.json", { endpoints: [{ provider: "stale", models: ["stale"] }] });
    const warn = vi.fn();
    const { events } = collectUnifiedSession({ rootDir: root, warn });
    expect(events.filter(event => event.type === "model.inventory")).toEqual([{ type: "model.inventory", data: expected, provenance: { component: "firewall", phase: "agent", path: "agent/awf-reflect.json", index: 0 } }]);
    expect(warn).not.toHaveBeenCalled();
  });

  it("stops at the valid preferred reflect copy without reading a corrupt mirror", () => {
    write("agent/awf-reflect.json", { endpoints: [{ provider: "copilot", models: ["preferred"] }] });
    write("sandbox/firewall/awf-reflect.json", "not-json");
    const warn = vi.fn();
    const { events } = collectUnifiedSession({ rootDir: root, warn });
    expect(events.filter(event => event.type === "model.inventory")).toEqual([
      {
        type: "model.inventory",
        data: { endpoints: [{ provider: "copilot", models: ["preferred"] }] },
        provenance: { component: "firewall", phase: "agent", path: "agent/awf-reflect.json", index: 0 },
      },
    ]);
    expect(warn).not.toHaveBeenCalled();
  });

  it("skips a symlinked preferred reflect copy and uses the safe fallback", () => {
    const target = write("private-reflect.json", { endpoints: [{ provider: "private", models: ["PRIVATE_MODEL"] }] });
    fs.mkdirSync(path.join(root, "agent"), { recursive: true });
    fs.symlinkSync(target, path.join(root, "agent/awf-reflect.json"));
    write("sandbox/firewall/awf-reflect.json", { endpoints: [] });
    const warn = vi.fn();
    const { events } = collectUnifiedSession({ rootDir: root, warn });
    expect(events.filter(event => event.type === "model.inventory")).toEqual([
      { type: "model.inventory", data: { endpoints: [] }, provenance: { component: "firewall", phase: "agent", path: "sandbox/firewall/awf-reflect.json", index: 0 } },
    ]);
    expect(warn).toHaveBeenCalledExactlyOnceWith("Unified session: symlink_not_read in agent/awf-reflect.json");
  });

  it("does not invent or follow symlinked model inventory", () => {
    expect(collectUnifiedSession({ rootDir: root }).events.some(event => event.type === "model.inventory")).toBe(false);
    const target = write("private-models.json", { provider: "copilot", models: ["PRIVATE_MODEL"] });
    fs.mkdirSync(path.join(root, "sandbox/firewall"), { recursive: true });
    fs.symlinkSync(target, path.join(root, "sandbox/firewall/models.json"));
    const warn = vi.fn();
    const { events } = collectUnifiedSession({ rootDir: root, warn });
    expect(events.some(event => event.type === "model.inventory")).toBe(false);
    expect(warn).toHaveBeenCalledWith("Unified session: symlink_not_read in sandbox/firewall/models.json");
  });

  it.each(["plugin-installations.jsonl", "usage/plugin-installations.jsonl"])("persists installed plugins from %s without credentials or duplicate copies", sourcePath => {
    const installed = {
      timestamp: "2026-10-12T00:00:00Z",
      plugin: "org/plugins/agents/example@" + "a".repeat(40),
      repository: "org/plugins",
      ref: "a".repeat(40),
      path: ".gh-aw-plugins/plugin-0/agents/example",
      status: "installed",
    };
    write(sourcePath, [{ ...installed, githubToken: "PRIVATE_TOKEN", githubApp: { privateKey: "PRIVATE_KEY" } }]);
    if (sourcePath === "plugin-installations.jsonl") write("usage/plugin-installations.jsonl", [{ ...installed, plugin: "stale" }]);
    const events = writeUnifiedSession({ rootDir: root });
    const { timestamp, ...data } = installed;
    expect(events.filter(event => event.type === "plugin.installation")).toEqual([
      { type: "plugin.installation", timestamp, data, provenance: { component: "plugin", phase: "agent", path: sourcePath, index: 0, timestampMs: Date.parse(timestamp) } },
    ]);
    expect(fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8")).not.toContain("PRIVATE_");
    expect(writeUnifiedSession({ rootDir: root })).toEqual(events);
  });

  it("warns and skips malformed plugin installation records", () => {
    const installed = {
      plugin: "org/plugin@main",
      repository: "org/plugin",
      ref: "main",
      path: ".gh-aw-plugins/plugin-0",
      status: "installed",
    };
    const malformed = [{}, { ...installed, status: "failed" }, { ...installed, plugin: 42 }, { ...installed, repository: null }, { ...installed, ref: {} }, { ...installed, path: false }];
    write("plugin-installations.jsonl", malformed);
    const warn = vi.fn();
    const { events } = collectUnifiedSession({ rootDir: root, warn });
    const warnings = events.filter(event => event.type === "session.collection_warning");

    expect(events.filter(event => event.type === "plugin.installation")).toEqual([]);
    expect(warnings.map(event => event.data)).toEqual(malformed.map((_, index) => ({ path: "plugin-installations.jsonl", code: "invalid_plugin_installation", line: index + 1 })));
    expect(warn).toHaveBeenCalledWith("Unified session: invalid_plugin_installation in plugin-installations.jsonl:1");
  });

  it("does not invent plugin installations when installation evidence is absent", () => {
    write("aw_info.json", { engine_id: "copilot", plugins: ["org/plugin@main"] });
    expect(collectUnifiedSession({ rootDir: root }).events.some(event => event.type === "plugin.installation")).toBe(false);
  });

  it("projects installed skill inventory, conflicts, and observed skill selection without instructions", () => {
    write("skill_inventory.json", {
      skills: [{ name: "review", source: "staged/.claude/skills/review", scope: "engine", digest: "sha256:abc", purpose: "Review", content: "PRIVATE_SKILL" }],
      conflicts: [
        { name: "review", kind: "scope_collision", sources: ["skills/review", "staged/.claude/skills/review"], instructions: "PRIVATE_CONFLICT" },
        { name: "invalid", kind: "tampered_kind", sources: ["skills/invalid"] },
      ],
    });
    write("agent-session.jsonl", [
      { type: "skill.invoked", data: { name: "review", source: "engine", instructions: "PRIVATE_INSTRUCTIONS" } },
      {
        type: "skill.inventory",
        data: {
          skills: [{ name: "native", source: "staged/native", scope: "engine", digest: "sha256:def", purpose: "Native", content: "PRIVATE_NATIVE_CONTENT", instructions: { text: "PRIVATE_NATIVE_INSTRUCTIONS" } }],
        },
      },
    ]);
    const events = writeUnifiedSession({ rootDir: root });
    expect(events.find(event => event.type === "skill.inventory")).toMatchObject({ data: { skills: [{ name: "review", scope: "engine" }] }, provenance: { component: "skill", phase: "activation", path: "skill_inventory.json" } });
    expect(events.filter(event => event.type === "skill.conflict")).toHaveLength(1);
    expect(events.filter(event => event.type === "skill.invoked").map(event => event.data)).toEqual([{ name: "review", source: "engine" }]);
    expect(events.filter(event => event.type === "skill.inventory").map(event => event.data.skills)).toEqual([
      [{ name: "review", source: "staged/.claude/skills/review", scope: "engine", digest: "sha256:abc", purpose: "Review" }],
      [{ name: "native", source: "staged/native", scope: "engine", digest: "sha256:def", purpose: "Native" }],
    ]);
    expect(fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8")).not.toContain("PRIVATE_");
  });

  it.each(["evals-job", "conclusion", "usage-only"])("persists grader and eval evidence from the %s layout exactly once", layout => {
    const gradersDir = layout === "usage-only" ? "usage/graders" : "agent/graders";
    const evalsPath = layout === "evals-job" ? "evals.jsonl" : layout === "conclusion" ? "evals/evals.jsonl" : "usage/evals.jsonl";
    const tokenPath = layout === "evals-job" ? "evals_token_usage.jsonl" : layout === "conclusion" ? "evals/evals_token_usage.jsonl" : "usage/evals/token_usage.jsonl";
    const manifest = { version: 1, graders: [{ id: "coverage", enabled: true, source: "builtin", threshold: 0 }] };
    const result = { id: "coverage", status: "unavailable", value: null, passed: null, source: "builtin", message: "No evidence", implementation: { id: "gh-aw/graders", version: 1 } };
    const run = { id: "123", attempt: 2, graderCount: 1, passed: 0, failed: 0, errors: 0 };
    write(`${gradersDir}/grader_manifest.json`, manifest);
    write(`${gradersDir}/grader_results.json`, { version: 1, run, results: [result] });
    write(evalsPath, [{ id: "quality", answer: "NO", model: "fixture", runid: "123", timestamp: "2026-10-11T00:00:00Z", question: "PRIVATE_QUESTION" }]);
    write(tokenPath, [{ model: "fixture", input_tokens: 0, output_tokens: 0 }]);
    if (layout !== "usage-only") {
      write("usage/graders/grader_manifest.json", manifest);
      write("usage/graders/grader_results.json", { results: [{ id: "stale" }] });
      write("usage/evals.jsonl", [{ id: "stale" }]);
      write("usage/evals/token_usage.jsonl", [{ model: "stale" }]);
    }
    const events = writeUnifiedSession({ rootDir: root, dailyAIC: { GITHUB_RUN_ID: "123", GITHUB_RUN_ATTEMPT: "2" } });
    expect(events.filter(event => event.type === "grader.manifest").map(event => event.data)).toEqual([manifest]);
    expect(events.filter(event => event.type === "grader.result").map(event => event.data)).toEqual([{ version: 1, run, results: [result] }]);
    expect(events.filter(event => event.type === "eval.result")).toMatchObject([{ data: { id: "quality", answer: "NO", model: "fixture", runId: "123" }, provenance: { phase: "evals", path: evalsPath } }]);
    expect(events.filter(event => event.type === "usage.report" && event.provenance.phase === "evals")).toMatchObject([{ data: { model: "fixture", usage: { inputTokens: 0, outputTokens: 0 } }, provenance: { path: tokenPath } }]);
    expect(events.at(-1).data.absentComponents).not.toContain("grader");
    expect(events.at(-1).data.absentComponents).not.toContain("eval");
    const persisted = fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8");
    expect(persisted).not.toContain("PRIVATE_");
    expect(persisted).not.toContain("stale");
    expect(writeUnifiedSession({ rootDir: root, dailyAIC: { GITHUB_RUN_ID: "123", GITHUB_RUN_ATTEMPT: "2" } })).toEqual(events);
  });

  it.each(["evals/evals.jsonl", "evals.jsonl", "usage/evals.jsonl"])("imports only current-run evals from mixed history in %s", evalsPath => {
    const history = [
      { id: "previous", runid: "122", answer: "YES" },
      { id: "current-a", runid: 123, answer: "NO" },
      { id: "older-attempt", runId: "123", runAttempt: 1, answer: "YES" },
      { id: "current-b", run_id: "123", run_attempt: 2, answer: "YES" },
      { id: "unattributed", answer: "YES" },
      { id: "later", runid: "124", answer: "NO" },
    ];
    const file = write(evalsPath, history);
    const warn = vi.fn();
    const events = writeUnifiedSession({ rootDir: root, warn });
    expect(events.filter(event => event.type === "eval.result").map(event => [event.data.id, event.data.runId, event.provenance.index])).toEqual([
      ["current-a", 123, 1],
      ["current-b", "123", 3],
    ]);
    expect(warn).toHaveBeenCalledExactlyOnceWith(`Unified session: unattributed_run_result in ${evalsPath}:5`);
    expect(fs.readFileSync(file, "utf8")).toBe(history.map(record => JSON.stringify(record)).join("\n") + "\n");
    expect(writeUnifiedSession({ rootDir: root, warn })).toEqual(events);
  });

  it.each(["runId", "runid", "run_id"])("collects and projects eval run attribution through %s", alias => {
    write("evals.jsonl", [
      { id: "current", [alias]: "123" },
      { id: "previous", [alias]: "122" },
    ]);
    const { events } = collectUnifiedSession({ rootDir: root });
    expect(events.filter(event => event.type === "eval.result").map(event => event.data)).toEqual([{ id: "current", runId: "123" }]);
  });

  it.each([
    { attribution: { runId: "123", runid: "122", run_id: "121" }, currentRunId: "123", included: true, projectedRunId: "123" },
    { attribution: { runId: "122", runid: "123", run_id: "123" }, currentRunId: "123", included: false },
    { attribution: { runid: "123", run_id: "122" }, currentRunId: "123", included: true, projectedRunId: "123" },
    { attribution: { runId: null, runid: "123", run_id: "122" }, currentRunId: "123", included: true, projectedRunId: null },
    { attribution: { runId: null, runid: null, run_id: "123" }, currentRunId: "123", included: true, projectedRunId: null },
    { attribution: { runId: "", runid: "123" }, currentRunId: "123", included: false },
    { attribution: { runId: 0, runid: "123" }, currentRunId: "123", included: false },
    { attribution: { runId: 0, runid: "123" }, currentRunId: "0", included: true, projectedRunId: 0 },
    { attribution: { runId: null, runid: null, run_id: null }, currentRunId: "123", included: false },
  ])("preserves eval alias precedence for $attribution with current run $currentRunId", ({ attribution, currentRunId, included, projectedRunId }) => {
    write("evals.jsonl", [{ id: "quality", ...attribution }]);
    const events = writeUnifiedSession({ rootDir: root, dailyAIC: { GITHUB_RUN_ID: currentRunId } });
    expect(events.filter(event => event.type === "eval.result").map(event => event.data)).toEqual(included ? [{ id: "quality", runId: projectedRunId }] : []);
  });

  it.each(["agent/graders/grader_results.json", "usage/graders/grader_results.json"])("imports only the current-run grader snapshot from %s", file => {
    write(file, [
      { run: { id: "122", attempt: 2 }, results: [{ id: "previous" }] },
      { run: { id: "123", attempt: 1 }, results: [{ id: "older-attempt" }] },
      { run: { id: 123, attempt: 2 }, results: [{ id: "current", passed: false }] },
    ]);
    const { events } = collectUnifiedSession({ rootDir: root });
    expect(events.filter(event => event.type === "grader.result")).toMatchObject([{ data: { results: [{ id: "current", passed: false }] }, provenance: { index: 2 } }]);
    expect(events.filter(event => event.type === "grader.result")).toHaveLength(1);
  });

  it.each(["aw_info.json", "activation/aw_info.json", "usage/aw_info.json"])("uses recorded run identity rather than the collector environment from %s", metadataPath => {
    write(metadataPath, { run_id: 456, run_attempt: 3 });
    write("evals.jsonl", [
      { id: "collector-run", runid: "123", answer: "YES" },
      { id: "recorded-run", runid: "456", answer: "NO" },
    ]);
    write("agent/graders/grader_results.json", { run: { id: "456", attempt: 3 }, results: [{ id: "current" }] });
    const { events } = collectUnifiedSession({ rootDir: root });
    expect(events.filter(event => event.type === "eval.result").map(event => event.data.id)).toEqual(["recorded-run"]);
    expect(events.filter(event => event.type === "grader.result")).toHaveLength(1);
  });

  it("warns and omits results when the current run cannot be identified", () => {
    delete process.env.GITHUB_RUN_ID;
    delete process.env.GITHUB_RUN_ATTEMPT;
    write("evals.jsonl", [{ id: "quality", runid: "123", answer: "YES" }]);
    write("agent/graders/grader_results.json", { run: { id: "123" }, results: [{ id: "quality" }] });
    const warn = vi.fn();
    const { events } = collectUnifiedSession({ rootDir: root, warn });
    expect(events.some(event => event.type === "eval.result" || event.type === "grader.result")).toBe(false);
    expect(warn).toHaveBeenCalledTimes(2);
    expect(events.filter(event => event.type === "session.collection_warning").every(event => event.data.code === "unattributed_run_result")).toBe(true);
  });

  it("uses injected run identity and attempt instead of the ambient environment", () => {
    write("evals.jsonl", [
      { id: "ambient-run", runid: "123", runAttempt: 2, answer: "YES" },
      { id: "ambient-attempt", runid: "456", runAttempt: 2, answer: "YES" },
      { id: "injected", runid: "456", runAttempt: 3, answer: "NO" },
    ]);
    write("agent/graders/grader_results.json", [
      { run: { id: "123", attempt: 2 }, results: [{ id: "ambient-run" }] },
      { run: { id: "456", attempt: 2 }, results: [{ id: "ambient-attempt" }] },
      { run: { id: "456", attempt: 3 }, results: [{ id: "injected" }] },
    ]);
    const { events } = collectUnifiedSession({ rootDir: root, dailyAIC: { GITHUB_RUN_ID: "456", GITHUB_RUN_ATTEMPT: "3" } });
    expect(events.filter(event => event.type === "eval.result").map(event => event.data.id)).toEqual(["injected"]);
    expect(events.filter(event => event.type === "grader.result").map(event => event.data.results[0].id)).toEqual(["injected"]);
  });

  it("does not attribute results to ambient run identity with an empty injected environment", () => {
    write("evals.jsonl", [{ id: "ambient", runid: "123", answer: "YES" }]);
    write("agent/graders/grader_results.json", { run: { id: "123", attempt: 2 }, results: [{ id: "ambient" }] });
    const warn = vi.fn();
    const { events } = collectUnifiedSession({ rootDir: root, dailyAIC: {}, warn });
    expect(events.some(event => event.type === "eval.result" || event.type === "grader.result")).toBe(false);
    expect(warn).toHaveBeenCalledTimes(2);
    expect(events.filter(event => event.type === "session.collection_warning").every(event => event.data.code === "unattributed_run_result")).toBe(true);
  });

  it.each([{ GITHUB_RUN_ID: "456", GITHUB_RUN_ATTEMPT: "3" }, {}])("keeps recorded run identity authoritative over injected environment %j", dailyAIC => {
    write("aw_info.json", { run_id: "789", run_attempt: 4 });
    write("evals.jsonl", [
      { id: "injected-run", runid: "456", runAttempt: 3, answer: "YES" },
      { id: "injected-attempt", runid: "789", runAttempt: 3, answer: "YES" },
      { id: "recorded", runid: "789", runAttempt: 4, answer: "NO" },
    ]);
    write("agent/graders/grader_results.json", [
      { run: { id: "456", attempt: 3 }, results: [{ id: "injected-run" }] },
      { run: { id: "789", attempt: 3 }, results: [{ id: "injected-attempt" }] },
      { run: { id: "789", attempt: 4 }, results: [{ id: "recorded" }] },
    ]);
    const { events } = collectUnifiedSession({ rootDir: root, dailyAIC });
    expect(events.filter(event => event.type === "eval.result").map(event => event.data.id)).toEqual(["recorded"]);
    expect(events.filter(event => event.type === "grader.result").map(event => event.data.results[0].id)).toEqual(["recorded"]);
  });

  it("does not borrow the collector attempt for recorded runs without attempt metadata", () => {
    write("aw_info.json", { run_id: "456" });
    write("agent/graders/grader_results.json", { run: { id: "456", attempt: 1 }, results: [{ id: "current" }] });
    expect(collectUnifiedSession({ rootDir: root, dailyAIC: { GITHUB_RUN_ID: "456", GITHUB_RUN_ATTEMPT: "3" } }).events.filter(event => event.type === "grader.result")).toHaveLength(1);
  });

  it("does not resurrect mirrored historical results when the original has no current-run results", () => {
    write("evals/evals.jsonl", [{ id: "previous", runid: "122", answer: "YES" }]);
    write("usage/evals.jsonl", [{ id: "stale", runid: "123", answer: "NO" }]);
    write("agent/graders/grader_results.json", { run: { id: "122" }, results: [{ id: "previous" }] });
    write("usage/graders/grader_results.json", { run: { id: "123" }, results: [{ id: "stale" }] });
    const { events } = collectUnifiedSession({ rootDir: root });
    expect(events.some(event => event.type === "eval.result" || event.type === "grader.result")).toBe(false);
  });

  it.each(["evals/evals.jsonl", "evals.jsonl", "usage/evals.jsonl"])("preserves raw JSONL positions for warnings and current-run evidence in %s", evalsPath => {
    write(evalsPath, '\n{broken\n{"id":"unattributed","answer":"YES"}\n\n{"id":"current","runid":"123","answer":"NO"}\n');
    const warn = vi.fn();
    const { events } = collectUnifiedSession({ rootDir: root, warn });
    expect(warn.mock.calls).toEqual([[`Unified session: malformed_jsonl in ${evalsPath}:2`], [`Unified session: unattributed_run_result in ${evalsPath}:3`]]);
    expect(events.filter(event => event.type === "session.collection_warning").map(event => event.data)).toEqual([
      { path: evalsPath, code: "malformed_jsonl", line: 2 },
      { path: evalsPath, code: "unattributed_run_result", line: 3 },
    ]);
    expect(events.filter(event => event.type === "eval.result")).toMatchObject([{ data: { id: "current" }, provenance: { index: 4 } }]);
  });

  it.each(["agent/graders/grader_results.json", "usage/graders/grader_results.json"])("omits file line numbers for unattributed JSON grader snapshots in %s", file => {
    write(file, [{ results: [{ id: "unattributed" }] }, { run: { id: "123", attempt: 2 }, results: [{ id: "current" }] }]);
    const warn = vi.fn();
    const { events } = collectUnifiedSession({ rootDir: root, warn });
    expect(warn).toHaveBeenCalledExactlyOnceWith(`Unified session: unattributed_run_result in ${file}`);
    expect(events.filter(event => event.type === "session.collection_warning").map(event => event.data)).toEqual([{ path: file, code: "unattributed_run_result" }]);
    expect(events.filter(event => event.type === "grader.result")).toMatchObject([{ data: { results: [{ id: "current" }] }, provenance: { index: 1 } }]);
  });

  it.each(["startup", "runtime"])("collects a single model/endpoint mismatch record in the %s phase", phase => {
    const data = {
      category: "model_endpoint_mismatch",
      phase,
      configured_model: "model-alias",
      resolved_model: "model-resolved",
      wire_api: "responses",
      wire_api_source: "engine",
      supported_endpoints: ["/chat/completions"],
      detail: "The model does not support the responses API",
      fix: "Use a chat/completions-compatible engine",
      ...(phase === "runtime" ? { model: "provider-model", endpoint: "/responses" } : {}),
    };
    write("agent/model-endpoint-mismatch.json", { ...data, private: "omitted" });
    const events = writeUnifiedSession({ rootDir: root });
    expect(events.filter(event => event.type === "model_endpoint.mismatch")).toEqual([{ type: "model_endpoint.mismatch", data, provenance: { component: "model_endpoint", phase, path: "agent/model-endpoint-mismatch.json", index: 0 } }]);
    expect(writeUnifiedSession({ rootDir: root })).toEqual(events);
    expect(events.at(-1).data.sources).toContainEqual({ component: "model_endpoint", phase, path: "agent/model-endpoint-mismatch.json", events: 1 });
  });

  it.each(["{broken", "[]", "null", '{"category":"model_endpoint_mismatch","phase":"other"}'])("warns rather than collecting an invalid mismatch record: %s", record => {
    write("agent/model-endpoint-mismatch.json", record);
    const warn = vi.fn();
    const { events } = collectUnifiedSession({ rootDir: root, warn });
    expect(events.some(event => event.type === "model_endpoint.mismatch")).toBe(false);
    expect(warn).toHaveBeenCalled();
    expect(events.some(event => event.type === "session.collection_warning" && event.data.path === "agent/model-endpoint-mismatch.json")).toBe(true);
  });

  it("projects the model resolution record", () => {
    const records = [
      { engine: "pi", role: "main", configured_model: "copilot/opus", resolved_model: "claude-opus-5.5", provider: "copilot", status: "resolved", cause: null },
      { engine: "pi", role: "reader", configured_model: "copilot/claude-opus-5", resolved_model: null, provider: "copilot", status: "failed", cause: "model_unavailable" },
      { engine: "pi", role: "writer", configured_model: "claude-sonnet-5.5", resolved_model: null, provider: "copilot", status: "failed", cause: "unsupported_endpoint" },
    ];
    write(
      "agent/model-resolution.json",
      records.map(record => ({ ...record, private: "omitted" }))
    );
    const events = writeUnifiedSession({ rootDir: root });
    expect(events.filter(event => event.type === "model.resolution")).toEqual(
      records.map((data, index) => ({ type: "model.resolution", data, provenance: { component: "model_resolution", phase: "agent", path: "agent/model-resolution.json", index } }))
    );
    expect(writeUnifiedSession({ rootDir: root })).toEqual(events);
    expect(events.at(-1).data.sources).toContainEqual({ component: "model_resolution", phase: "agent", path: "agent/model-resolution.json", events: 3 });
  });

  it.each([
    "{broken",
    "[]",
    "{}",
    JSON.stringify([{ engine: "pi", role: "main", configured_model: "opus", resolved_model: null, provider: null, status: "resolved", cause: null }]),
    JSON.stringify([{ engine: "pi", role: "main", configured_model: "opus", resolved_model: null, provider: null, status: "failed", cause: "other" }]),
  ])("warns rather than collecting an invalid model resolution record: %s", record => {
    write("agent/model-resolution.json", record);
    const warn = vi.fn();
    const { events } = collectUnifiedSession({ rootDir: root, warn });
    expect(events.some(event => event.type === "model.resolution")).toBe(false);
    expect(warn).toHaveBeenCalled();
    expect(events.some(event => event.type === "session.collection_warning" && event.data.path === "agent/model-resolution.json")).toBe(true);
  });

  it.each([
    { engine: "copilot", metadata: undefined, restricted: false },
    { engine: "custom", metadata: undefined, restricted: false },
    { engine: undefined, metadata: undefined, restricted: false },
    { engine: "aider", metadata: undefined, restricted: true },
    { engine: undefined, metadata: "aider", restricted: true },
    { engine: undefined, metadata: "copilot", restricted: false },
    { engine: "copilot", metadata: "aider", restricted: false },
  ])("selects raw Aider diagnostic policy only from trusted caller or runner context: %j", ({ engine, metadata, restricted }) => {
    const nativeError = { type: "session.error", data: { sourceEngine: "aider", code: 400, errorType: "NativeError", message: "genuine provider failure" } };
    write("agent-session.jsonl", [{ type: "assistant.message", data: { sourceEngine: "aider", content: "observed answer" } }, nativeError]);
    write("agent-stdio.log", JSON.stringify({ type: "session.error", data: { sourceEngine: "copilot", code: 429, errorType: "ProviderError", message: "CAPIError: 429 Too Many Requests" } }));
    write("agent_execution_exit_code.txt", "0\n");
    if (metadata !== undefined) write("aw_info.json", { engine_id: metadata });
    const events = collectUnifiedSession({ rootDir: root, engine, warn() {} }).events;
    const execution = events.find(event => event.type === "agent.execution");
    expect(execution.data).toEqual({
      categories: restricted ? [] : ["capi_quota_exceeded_error"],
      errorCodes: restricted ? [400] : [400, 429],
      errorTypes: restricted ? ["NativeError"] : ["NativeError", "ProviderError"],
      exitCode: 0,
    });
    expect(events.find(event => event.type === "assistant.message").data.content).toBe("observed answer");
    expect(events.find(event => event.type === "session.error").data).toEqual(normalizeUnifiedSessionEvent(nativeError).data);
  });

  it("persists Claude workflow launch and lifecycle projections without scripts, prompts, or duplicate parent usage", () => {
    write("agent-stdio.log", dynamicWorkflow.map(JSON.stringify).join("\n"));
    const events = writeUnifiedSession({ rootDir: root, engine: "claude" });
    const launch = events.find(event => event.type === "tool.execution_complete");
    expect(launch.data).toMatchObject({
      toolName: "Workflow",
      toolCallId: "workflow-tool",
      success: true,
      status: "async_launched",
      taskId: "dynamic-task",
      taskType: "local_workflow",
      workflowName: "smoke-claude-dynamic",
      workflowRunId: "dynamic-run",
    });
    const started = events.find(event => event.type === "dynamicWorkflows.task_started");
    expect(started.data).toMatchObject({ taskId: "dynamic-task", toolCallId: "workflow-tool", taskType: "local_workflow", workflowName: "smoke-claude-dynamic", sessionId: "dynamic-session" });
    const progress = events.filter(event => event.type === "dynamicWorkflows.task_progress");
    expect(progress[0].data.usage).toEqual({ totalTokens: 0, toolUses: 0, durationMs: 0 });
    expect(progress[1].data.workflowProgress[1]).toMatchObject({ agentId: "dynamic-agent", phaseIndex: 1, model: "claude-sonnet-4-6", state: "done" });
    expect(events.find(event => event.type === "dynamicWorkflows.task_updated").data).toMatchObject({ taskId: "dynamic-task", status: "completed" });
    expect(events.find(event => event.type === "dynamicWorkflows.task_notification").data).toMatchObject({
      taskId: "dynamic-task",
      toolCallId: "workflow-tool",
      status: "completed",
      usage: { totalTokens: 250, toolUses: 2, durationMs: 5000 },
    });
    expect(events.some(event => event.type.startsWith("claude.task_") || event.type === "claude.background_tasks_changed")).toBe(false);
    expect(events.find(event => event.type === "session.result").data.usage).toEqual({ inputTokens: 7, outputTokens: 11, inputTokensIncludeCache: false });
    const persisted = fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8");
    expect(persisted).not.toContain("PRIVATE_");
    for (const event of events.filter(event => event.type !== "session.collection")) {
      expect(normalizeUnifiedSessionEvent(normalizeUnifiedSessionEvent(event))).toEqual(normalizeUnifiedSessionEvent(event));
    }
  });

  it.each([0, 2])("preserves explicit mixed native execution exit %i without a runtime exit artifact", exitCode => {
    const execution = { type: "agent.execution", data: { categories: [], errorCodes: [], errorTypes: [], exitCode } };
    const entries = [{ type: "assistant", message: { content: "observed answer" } }, execution];
    expect(parseEngineSession(entries.map(JSON.stringify).join("\n"), "cursor").filter(event => event.type === "agent.execution")).toEqual([execution]);
    write("agent-session.jsonl", entries);
    const { events } = collectUnifiedSession({ rootDir: root, engine: "cursor" });
    expect(events.filter(event => event.type === "agent.execution").map(event => event.data)).toEqual([execution.data]);
    expect(events.filter(event => event.type === "assistant.message").map(event => event.data.content)).toEqual(["observed answer"]);
  });

  it("uses the last observed native execution exit when no runtime exit artifact exists", () => {
    const observations = [2, 0].map(exitCode => ({ type: "agent.execution", data: { categories: [], errorCodes: [], errorTypes: [], exitCode } }));
    const entries = [{ type: "assistant", message: { content: "observed answer" } }, ...observations];
    expect(parseEngineSession(entries.map(JSON.stringify).join("\n"), "cursor").filter(event => event.type === "agent.execution")).toEqual(observations);
    write("agent-session.jsonl", entries);
    const { events } = collectUnifiedSession({ rootDir: root, engine: "cursor" });
    expect(events.filter(event => event.type === "agent.execution").map(event => event.data)).toEqual([observations[1].data]);
  });

  it("includes both activation prompts verbatim with distinct provenance when present", () => {
    const system = "System instructions\n";
    const user = "User request\n";
    write("aw-prompts/system.txt", system);
    write("aw-prompts/user.txt", user);
    const events = writeUnifiedSession({ rootDir: root });
    expect(events.filter(event => event.provenance.component === "prompt")).toEqual([
      { type: "prompt.system", data: { content: system }, provenance: { component: "prompt", phase: "activation", path: "aw-prompts/system.txt", index: 0 } },
      { type: "prompt.user", data: { content: user }, provenance: { component: "prompt", phase: "activation", path: "aw-prompts/user.txt", index: 0 } },
    ]);
    expect(fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8")).toContain(
      JSON.stringify({ type: "prompt.user", data: { content: user }, provenance: { component: "prompt", phase: "activation", path: "aw-prompts/user.txt", index: 0 } })
    );
    expect(events.at(-1).data.absentComponents).toContain("agent");
  });

  it("does not follow a symlinked prompt or invent missing prompt content", () => {
    write("aw-prompts/user.txt", "");
    fs.symlinkSync(write("private.txt", "not a prompt"), path.join(root, "aw-prompts/system.txt"));
    const warnings = [];
    const { events } = collectUnifiedSession({ rootDir: root, warn: warning => warnings.push(warning) });
    expect(events.filter(event => event.provenance.component === "prompt")).toMatchObject([{ type: "prompt.user", data: { content: "" } }]);
    expect(warnings).toHaveLength(1);
    expect(warnings[0]).toContain("aw-prompts/system.txt");
  });

  it("records the daily AIC activation decision once, including zero accounting values", () => {
    const dailyAIC = {
      GH_AW_DAILY_AI_CREDITS_GUARDRAIL_STATUS: "exceeded",
      GH_AW_DAILY_AI_CREDITS_EXCEEDED: "true",
      GH_AW_DAILY_AI_CREDITS_TOTAL: "0",
      GH_AW_DAILY_AI_CREDITS_ESTIMATED: "0",
      GH_AW_DAILY_AI_CREDITS_THRESHOLD: "0",
    };
    const events = writeUnifiedSession({ rootDir: root, dailyAIC });
    expect(events.filter(event => event.type === "guardrail.daily_aic")).toEqual([
      {
        type: "guardrail.daily_aic",
        data: { status: "exceeded", exceeded: true, total: 0, estimated: 0, threshold: 0 },
        provenance: { component: "guardrail", phase: "activation", path: "usage/aw_session.jsonl", index: 0 },
      },
    ]);
    expect(fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8")).toContain('"type":"guardrail.daily_aic"');
    expect(writeUnifiedSession({ rootDir: root, dailyAIC })).toEqual(events);
  });

  it("omits an absent decision and does not duplicate existing guardrail evidence", () => {
    expect(collectUnifiedSession({ rootDir: root, dailyAIC: {} }).events.some(event => event.type === "guardrail.daily_aic")).toBe(false);
    const existing = { type: "guardrail.daily_aic", data: { status: "skipped", exceeded: false } };
    write("agent-session.jsonl", [existing]);
    const events = collectUnifiedSession({ rootDir: root, dailyAIC: { GH_AW_DAILY_AI_CREDITS_GUARDRAIL_STATUS: "under_budget" } }).events;
    expect(events.filter(event => event.type === "guardrail.daily_aic")).toMatchObject([{ data: existing.data }]);
    expect(events.filter(event => event.type === "guardrail.daily_aic")).toHaveLength(1);
  });

  it("emits one execution entry across canonical, stdio, persisted detector, and exit-code evidence", () => {
    const execution = { type: "agent.execution", data: { categories: ["agentic_engine_timeout"], errorCodes: [502], errorTypes: ["server_error"], exitCode: 1 } };
    write("agent-session.jsonl", [...require("./claude_session.cjs").normalizeClaudeSession(claudeFixtures.failure), execution]);
    write("agent-errors.jsonl", [execution]);
    write("agent-stdio.log", claudeFixtures.failure.map(JSON.stringify).join("\n") + "\n[claude-harness] done: exitCode=1\n");
    write("agent_execution_exit_code.txt", "0\n");
    const events = writeUnifiedSession({ rootDir: root, engine: "claude" });
    const executions = events.filter(event => event.type === "agent.execution");
    expect(executions).toHaveLength(1);
    expect(executions[0]).toMatchObject({
      data: { categories: ["agentic_engine_timeout"], errorCodes: [502], errorTypes: ["UnknownError", "api_error", "server_error"], exitCode: 0 },
      provenance: { component: "execution", phase: "agent", path: "agent-errors.jsonl" },
    });
    expect(executions[0]).not.toHaveProperty("timestamp");
    expect(events.filter(event => event.type === "claude.assistant_error")).toHaveLength(0);
    expect(events.filter(event => event.type === "session.result" && event.data.errors?.some(error => error?.error === "server_error"))).toHaveLength(1);
    expect(writeUnifiedSession({ rootDir: root, engine: "claude" })).toEqual(events);
  });

  it("uses an execution snapshot exit code when the raw exit-code file is absent", () => {
    write("usage/agent/execution.json", { exit_code: 0, outcome: "success" });
    const { events } = collectUnifiedSession({ rootDir: root });
    expect(events.filter(event => event.type === "agent.execution")).toMatchObject([{ data: { categories: [], errorCodes: [], errorTypes: [], exitCode: 0 }, provenance: { path: "usage/agent/execution.json" } }]);
  });

  it("prefers persisted live detector exit evidence over an earlier canonical observation", () => {
    const data = { categories: [], errorCodes: [], errorTypes: [] };
    write("agent-session.jsonl", [{ type: "agent.execution", data: { ...data, exitCode: 1 } }]);
    write("agent-errors.jsonl", [{ type: "agent.execution", data: { ...data, exitCode: 0 } }]);
    expect(collectUnifiedSession({ rootDir: root }).events.filter(event => event.type === "agent.execution")).toMatchObject([{ data: { exitCode: 0 } }]);
  });

  it("retains observed execution results without requiring a conversation", () => {
    write("agent-stdio.log", "Access denied by policy settings\n[copilot-harness] done: exitCode=1\n");
    const { events } = collectUnifiedSession({ rootDir: root, engine: "copilot" });
    expect(events.filter(event => event.type === "agent.execution")).toMatchObject([{ data: { categories: ["inference_access_error"], exitCode: 1 } }]);
  });

  it("normalizes mixed legacy Cursor entries without letting canonical execution suppress the conversation", () => {
    write("agent-session.jsonl", [
      { type: "system", subtype: "init", model: null, session_id: null },
      { type: "user", message: { content: [{ type: "text", text: "PRIVATE_USER_PROMPT" }] } },
      { type: "assistant", message: { content: [{ type: "text", text: "First answer\nSecond line" }] } },
      { type: "result", num_turns: 1, usage: {} },
      { type: "agent.execution", data: { categories: [], errorCodes: [], errorTypes: [], exitCode: 0 } },
    ]);
    write("agent-stdio.log", "Assistant: duplicate raw answer");
    const events = writeUnifiedSession({
      rootDir: root,
      engine: "cursor",
      warn: message => {
        throw new Error(message);
      },
    });
    expect(events.filter(event => event.type === "assistant.message")).toMatchObject([{ data: { content: "First answer\nSecond line" }, provenance: { path: "agent-session.jsonl" } }]);
    expect(events.find(event => event.type === "session.init").data.sourceEngine).toBe("cursor");
    expect(events.filter(event => event.type === "agent.execution")).toHaveLength(1);
    const persisted = fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8");
    expect(require("./log_parser_shared.cjs").generateCopilotCliStyleSummary(events)).not.toContain("PRIVATE_USER_PROMPT");
    expect(persisted).not.toContain("duplicate raw answer");
  });

  it("recovers declared-engine stdout when persisted evidence contains only execution", () => {
    const source = "Assistant: recovered answer\n";
    const expectedEvents = parseCustomLog(source, "cursor").logEntries;
    const expectedAssistantEvents = expectedEvents.filter(event => event.type === "assistant.message");
    expect(expectedAssistantEvents).toHaveLength(1);
    write("agent-session.jsonl", [{ type: "agent.execution", data: { categories: ["agentic_engine_timeout"], errorCodes: [], errorTypes: [], exitCode: 1 } }]);
    write("agent-stdio.log", source);
    const { events } = collectUnifiedSession({ rootDir: root, engine: "cursor" });
    expect(events.filter(event => event.type === "assistant.message")).toEqual(
      expectedAssistantEvents.map(event => ({
        ...normalizeUnifiedSessionEvent(event, "agent"),
        provenance: { component: "agent", phase: "agent", path: "agent-stdio.log", index: expectedEvents.indexOf(event) },
      }))
    );
    expect(events.filter(event => event.type === "agent.execution")).toMatchObject([{ data: { categories: ["agentic_engine_timeout"], exitCode: 1 } }]);
  });

  it("keeps canonical messages authoritative over equivalent legacy records and raw fallback", () => {
    const canonical = { type: "assistant.message", data: { content: "answer" } };
    const legacy = { type: "assistant", message: { content: [{ type: "text", text: "answer" }] } };
    write("agent-session.jsonl", [legacy, canonical, legacy, { type: "assistant", message: { content: [{ type: "text", text: "another answer" }] } }]);
    write("agent-stdio.log", "Assistant: must not be selected");
    const { events } = collectUnifiedSession({ rootDir: root, engine: "cursor" });
    expect(events.filter(event => event.type === "assistant.message").map(event => event.data.content)).toEqual(["answer", "another answer"]);
    expect(events.filter(event => event.provenance.component === "agent").every(event => event.provenance.path === "agent-session.jsonl")).toBe(true);
  });

  it("does not convert runtime tool metadata into fabricated native tool transcripts", () => {
    write("agent-stdio.log", "Assistant: completed\n");
    write("mcp-logs/rpc-messages.jsonl", [{ event: "rpc_request", method: "tools/call", payload: { params: { name: "bash", arguments: { command: "pwd" } } } }]);
    const { events } = collectUnifiedSession({ rootDir: root, engine: "cursor" });
    expect(events.filter(event => event.type === "assistant.message")).toHaveLength(1);
    expect(events.some(event => event.type === "tool.execution_start" || event.type === "tool.execution_complete")).toBe(false);
    expect(events.filter(event => event.type === "mcp.rpc.request")).toHaveLength(1);
  });

  it("rejects malformed recorded exit codes and does not follow an exit-code symlink", () => {
    write("agent_execution_exit_code.txt", "1 trailing");
    expect(() => collectUnifiedSession({ rootDir: root })).toThrow("Invalid agent execution exit code");
    fs.unlinkSync(path.join(root, "agent_execution_exit_code.txt"));
    fs.symlinkSync(write("private-exit.txt", "139"), path.join(root, "agent_execution_exit_code.txt"));
    const warnings = [];
    const { events } = collectUnifiedSession({ rootDir: root, warn: warning => warnings.push(warning) });
    expect(events.some(event => event.type === "agent.execution")).toBe(false);
    expect(warnings).toHaveLength(1);
  });

  it("reconstructs OpenCode sessions from stdio with native timestamps and tool correlation", () => {
    write("aw_info.json", { engine_id: "opencode", agent_version: "fixture" });
    write("agent-stdio.log", [
      { type: "text", sessionID: "ses_fixture", timestamp: 1790899201000, part: { id: "text", text: "Done." } },
      {
        type: "tool_use",
        sessionID: "ses_fixture",
        timestamp: 1790899202000,
        part: { id: "tool", callID: "call", tool: "bash", state: { status: "completed", input: { command: "pwd" }, output: "/workspace", time: { start: 1790899200500, end: 1790899202000 } } },
      },
      { type: "step_finish", sessionID: "ses_fixture", timestamp: 1790899203000, part: { id: "finish", cost: 0, tokens: { input: 10, output: 5, cache: { read: 20, write: 0 } } } },
    ]);
    const events = writeUnifiedSession({
      rootDir: root,
      warn: message => {
        throw new Error(message);
      },
    });
    const start = events.find(event => event.type === "tool.execution_start");
    expect(start).toMatchObject({ provenance: { path: "agent-stdio.log", timestampMs: 1790899202000 }, data: { toolCallId: "call", toolName: "bash", input: { command: "pwd" } } });
    expect(events.find(event => event.type === "tool.execution_complete").data).toMatchObject({ toolCallId: "call", success: true, durationMs: 1500 });
    expect(events.find(event => event.type === "session.result").data).toMatchObject({ totalCostUsd: 0, numTurns: 1, usage: { inputTokens: 10, outputTokens: 5, cacheReadInputTokens: 20 } });
    expect(events.find(event => event.type === "assistant.message").data.content).toBe("Done.");
  });

  it("merges every required component with essential payloads and timestamp ordering", () => {
    const message = { type: "assistant.message", id: "same-id", timestamp: "2026-10-02T00:00:03Z", data: { content: "Done.\n", extra: [0, false, null] }, nativeField: "preserved" };
    const rpc = { timestamp: "2026-10-02T00:00:02+00:00", event: "rpc_response", payload: { jsonrpc: "2.0", id: 0, result: { content: [{ text: "response" }] } }, server_id: "github", direction: "IN" };
    const audit = { ts: 1790899201.25, host: "example.com", decision: "TCP_DENIED", status: 403, extra: false };
    write("agent-session.jsonl", [message]);
    write("mcp-logs/rpc-messages.jsonl", [rpc]);
    write("sandbox/firewall/audit/audit.jsonl", [audit]);
    write("sandbox/firewall/logs/api-proxy-logs/events.jsonl", [{ timestamp: "2026-10-02T00:00:03.5Z", eventName: "token_steering", message: "budget warning" }]);
    write("sandbox/firewall/logs/api-proxy-logs/token-tracker-audit.jsonl", [{ ts: 1790899203600, event: "TRACK_END", rid: "request" }]);
    write("threat-detection/sandbox/firewall/logs/audit.jsonl", [{ timestamp: "2026-10-02T00:00:04Z", event: "http_access", status: 200 }]);
    write("safeoutputs.jsonl", [{ type: "create_issue", title: "Requested" }]);
    write("safe-output-items.jsonl", [{ type: "create_issue", timestamp: "2026-10-02T00:00:05Z", url: "https://github.com/example/repo/issues/1" }]);
    write("safe-output-errors.json", { errors: [{ type: "add_comment", error: "rejected" }] });
    write("experiments/assignments.json", { experiment: "variant-b" });
    write("experiments/state.jsonl", [{ run_id: "1", timestamp: "2026-10-02T00:00:00Z", assignments: { experiment: "variant-b" } }]);
    write("agent/graders/grader_manifest.json", { graders: [{ id: "quality" }] });
    write("agent/graders/grader_results.json", { run: { id: "123", attempt: 2 }, results: [{ id: "quality", score: 0, passed: false }] });
    write("evals/evals.jsonl", [{ id: "quality", runid: "123", timestamp: "2026-10-02T00:00:06Z", answer: "no" }]);
    write("usage/agent/execution.json", { outcome: "failure", duration_ms: 0 });
    write("usage/detection/detection_result.json", { conclusion: "failure", secret_leak: false });
    const events = writeUnifiedSession({ rootDir: root });
    const persisted = fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8").trimEnd().split("\n").map(JSON.parse);
    expect(persisted).toEqual(events);
    expect(persisted[0]).toEqual({
      type: "session.format",
      data: { version: 1 },
      provenance: { component: "collector", phase: "conclusion", path: "usage/aw_session.jsonl", index: 0 },
    });
    expect(persisted.filter(event => event.type === "session.format")).toHaveLength(1);
    expect(new Set(events.map(event => event.provenance.component))).toEqual(new Set(["agent", "mcp", "firewall", "safe_output", "experiment", "grader", "eval", "execution", "detection", "collector"]));
    expect(events.find(event => event.type === "assistant.message")).toMatchObject(normalizeUnifiedSessionEvent(message));
    expect(events.find(event => event.type === "assistant.message")).not.toHaveProperty("nativeField");
    expect(events.find(event => event.type === "mcp.rpc.response").data).toEqual({ serverName: "github", direction: "IN", rpcId: 0 });
    expect(events.find(event => event.type === "firewall.http_access").data).toEqual({ host: "example.com", decision: "TCP_DENIED", status: 403 });
    expect(events.find(event => event.type === "firewall.http_access").provenance.timestampMs).toBe(1790899201250);
    expect(events.find(event => event.data.requestId === "request").provenance.timestampMs).toBe(1790899203600);
    expect(events.filter(event => event.provenance.phase === "detection")).toHaveLength(2);
    expect(events.find(event => event.type === "grader.result").data.results[0]).toEqual({ id: "quality", score: 0, passed: false });
    const times = events.slice(1).map(event => event.provenance.timestampMs ?? Infinity);
    expect(times).toEqual([...times].sort((a, b) => (a === b ? 0 : a < b ? -1 : 1)));
    expect(events.at(-1).type).toBe("session.collection");
    expect(events.at(-1).data).toMatchObject({ warnings: 0, absentComponents: ["workflow"] });
    const original = fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8");
    writeUnifiedSession({ rootDir: root });
    expect(fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8")).toBe(original);
  });

  it.each(["experiments", "usage/experiment"])("persists exact current-run assignments from %s independently of historical state", directory => {
    const assignments = { assignments: "treatment", model_size: "large", prompt_style: "detailed" };
    const history = [
      { run_id: "122", timestamp: "2026-10-01T00:00:00Z", assignments: { assignments: "control", model_size: "small", prompt_style: "concise" } },
      { run_id: "123", timestamp: "2026-10-02T00:00:00Z", assignments },
    ];
    const assignmentPath = `${directory}/assignments.json`;
    write(assignmentPath, assignments);
    write(`${directory}/state.jsonl`, history);
    const events = writeUnifiedSession({ rootDir: root });
    expect(events.filter(event => event.type === "experiment.assignment")).toEqual([
      {
        type: "experiment.assignment",
        data: { assignments },
        provenance: { component: "experiment", phase: "activation", path: assignmentPath, index: 0 },
      },
    ]);
    expect(events.filter(event => event.type === "experiment.state").map(event => event.data)).toEqual(history.map(({ run_id, assignments }) => ({ runId: run_id, assignments })));
    const persisted = fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8");
    expect(persisted.trim().split("\n").map(JSON.parse)).toEqual(events);
    expect(validateSession(persisted)).toBe(events.length);
    expect(events.at(-1).data.warnings).toBe(0);
    expect(writeUnifiedSession({ rootDir: root })).toEqual(events);
    expect(fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8")).toBe(persisted);
  });

  it("collects available tool and engine versions from workflow metadata", () => {
    write("aw_info.json", {
      cli_version: "v1.2.3",
      awf_version: "v0.4.5",
      awmg_version: "v0.6.7",
      engine_id: "copilot",
      agent_version: "v2.3.4",
      model: "fixture",
      event_name: "workflow_dispatch",
      secret: "omit",
    });
    const { events } = collectUnifiedSession({ rootDir: root });
    expect(events.find(event => event.type === "workflow.info")).toMatchObject({
      data: {
        cliVersion: "v1.2.3",
        awfVersion: "v0.4.5",
        mcpgVersion: "v0.6.7",
        engineId: "copilot",
        agentVersion: "v2.3.4",
        requestedModel: "fixture",
        triggerType: "workflow_dispatch",
      },
      provenance: { path: "aw_info.json", component: "workflow" },
    });
    expect(events.find(event => event.type === "workflow.info").data).not.toHaveProperty("secret");
  });

  it("prefers runner routing metadata over agent metadata and preserves the harness outcome", () => {
    write("aw_info.json", {
      engine_id: "claude",
      model: "runner-model",
      requested_model: "agent",
      model_routing: { status: "selected", wire_model: "runner-model", effort: "medium", applied_effort: "low", endpoint: "/v1/messages" },
    });
    write("agent/aw_info.json", {
      engine_id: "claude",
      model: "forged-model",
      model_routing: { status: "selected", wire_model: "forged-model", effort: "max" },
    });
    write("agent/awf-routing-outcome.json", {
      status: "rejected",
      wire_model: "forged-model",
      endpoint: "/forged",
      selected_endpoint: "/forged-selected",
      effort: "forged-effort",
      applied_effort: "forged-applied-effort",
      failure_code: "forged-failure",
      detail: "omit this detail",
    });

    const { events } = collectUnifiedSession({ rootDir: root, warn: vi.fn() });
    const workflow = events.find(event => event.type === "workflow.info");
    expect(workflow).toMatchObject({
      data: {
        model: "runner-model",
        requestedModel: "agent",
        modelRouting: {
          status: "selected",
          wireModel: "runner-model",
          effort: "medium",
          appliedEffort: "low",
          effectiveEndpoint: "/v1/messages",
        },
      },
      provenance: { component: "workflow", phase: "activation", path: "aw_info.json" },
    });
    const outcome = events.find(event => event.type === "model_routing.outcome");
    expect(outcome).toMatchObject({
      data: {
        status: "rejected",
        wireModel: "forged-model",
        effectiveEndpoint: "/forged",
        selectedEndpoint: "/forged-selected",
        effort: "forged-effort",
        appliedEffort: "forged-applied-effort",
        failureCode: "forged-failure",
      },
      provenance: { component: "agent", phase: "agent", path: "agent/awf-routing-outcome.json" },
    });
    expect(outcome.data).not.toHaveProperty("detail");
    expect(events.at(-1).data.absentComponents).not.toContain("workflow");
  });

  it.each(["runtime/aw_info.json", "aw_info.json", "activation/aw_info.json", "usage/aw_info.json"])("persists caller context and observed episode lineage from trusted %s", metadataPath => {
    write(metadataPath, {
      engine_id: "copilot",
      run_id: "300",
      run_attempt: "2",
      workflow_id: "org/repo/.github/workflows/worker.yml@refs/heads/main",
      workflow_call_id: "300-2:org/repo/.github/workflows/worker.yml@refs/heads/main",
      caller_workflow_ref: "org/parent/.github/workflows/caller.yml@refs/heads/main",
      repository: "org/repo",
      ref: "refs/heads/main",
      sha: "abc123",
      actor: "octocat",
      target_repo: "org/target",
      context: {
        episode_id: "100-1:root",
        hop_id: "200-1:caller",
        parent_hop_id: "100-1:root",
        origin_event: "issues",
        root_repo: "org/repo",
        root_workflow_id: "root.yml",
        root_run_id: "100",
        workflow_call_id: "200-1:caller",
        repo: "org/parent",
        run_id: "200",
        run_attempt: "1",
        workflow_id: "caller.yml",
        event_type: "workflow_dispatch",
        work_queue_assignment: { payload: { enabled: false, count: 0, optional: null } },
        secret: "PRIVATE_CONTEXT",
      },
    });
    write("agent/aw_info.json", { context: { episode_id: "forged", hop_id: "forged" } });
    write("agent-session.jsonl", [{ type: "session.init", data: { sourceEngine: "copilot" } }]);
    write("agent-stdio.log", "::add-mask::PRIVATE_CONTEXT\n");
    const events = writeUnifiedSession({ rootDir: root });
    const persisted = fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8").trimEnd().split("\n").map(JSON.parse);
    const workflow = persisted.find(event => event.type === "workflow.info");
    expect(workflow).toMatchObject({
      data: {
        runId: "300",
        runAttempt: "2",
        workflowId: "org/repo/.github/workflows/worker.yml@refs/heads/main",
        workflowCallId: "300-2:org/repo/.github/workflows/worker.yml@refs/heads/main",
        callerWorkflowRef: "org/parent/.github/workflows/caller.yml@refs/heads/main",
        repository: "org/repo",
        ref: "refs/heads/main",
        sha: "abc123",
        actor: "octocat",
        targetRepo: "org/target",
        awContext: {
          workflow_call_id: "200-1:caller",
          work_queue_assignment: { payload: { enabled: false, count: 0, optional: null } },
          secret: "***",
        },
        episode: {
          episodeId: "100-1:root",
          hopId: "200-1:caller",
          parentHopId: "100-1:root",
          originEvent: "issues",
          rootRepo: "org/repo",
          rootWorkflowId: "root.yml",
          rootRunId: "100",
          parentRepo: "org/parent",
          parentRunId: "200",
          parentRunAttempt: "1",
          parentWorkflowId: "caller.yml",
          parentWorkflowCallId: "200-1:caller",
          parentEventType: "workflow_dispatch",
        },
      },
      provenance: { component: "workflow", path: metadataPath },
    });
    expect(persisted).toEqual(serializeSessionArtifact(events, ["PRIVATE_CONTEXT"]).trimEnd().split("\n").map(JSON.parse));
    expect(JSON.stringify(workflow)).not.toContain("PRIVATE_CONTEXT");
    expect(JSON.stringify(workflow)).not.toContain("forged");
    const original = fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8");
    writeUnifiedSession({ rootDir: root });
    expect(fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8")).toBe(original);
  });

  it("normalizes the runner snapshot of routed Copilot CLI run 39147795806 into workflow.info", async () => {
    // Records from the run's agent artifact, with the harness selection carrying the routed request `auto`.
    const fixtureDir = path.join(import.meta.dirname, "fixtures", "copilot-routed-run-39147795806");
    const fixture = name => fs.readFileSync(path.join(fixtureDir, name), "utf8");
    write("aw_info.json", fixture("aw_info.json"));
    write("agent/awf-routing-outcome.json", fixture("awf-routing-outcome.json"));
    write("sandbox/firewall/logs/api-proxy-logs/model-routing.jsonl", fixture("model-routing.jsonl"));
    write("sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl", fixture("token-usage.jsonl"));
    write("sandbox/agent/logs/copilot-model-selection.json", { ...JSON.parse(fixture("copilot-model-selection.json")), configured_model: "auto" });
    process.env.GH_AW_TMP_DIR = root;
    process.env.GH_AW_MODEL_ROUTING_ENABLED = "true";
    process.env.GH_AW_ENGINE_ID = "copilot";
    process.env.GITHUB_STEP_SUMMARY = path.join(root, "step-summary.md");
    for (const key of ["GH_AW_PHASE", "GH_AW_AGENT_USAGE_PATH", "GH_AW_AGENT_USAGE_JSONL_PATH", "GH_AW_ENGINE_MODEL"]) delete process.env[key];
    global.core = { info: vi.fn(), warning: vi.fn(), debug: vi.fn(), setFailed: vi.fn(), setOutput: vi.fn(), exportVariable: vi.fn() };
    await req("./parse_token_usage.cjs").main();
    expect(global.core.setFailed).not.toHaveBeenCalled();
    // The uploaded artifact carries the runner snapshot, not the runner-root metadata.
    fs.rmSync(path.join(root, "aw_info.json"));

    const { events } = collectUnifiedSession({ rootDir: root, warn: vi.fn() });
    expect(events.find(event => event.type === "workflow.info")).toMatchObject({
      data: {
        engineId: "copilot",
        model: "gpt-5.6-luna",
        requestedModel: "auto",
        modelRouting: { status: "selected", wireModel: "gpt-5.6-luna", effort: "max", effectiveEndpoint: "/responses" },
        modelSelection: { status: "observed", configuredModel: "auto", resolvedModel: "gpt-5.6-luna", effectiveModel: "gpt-5.6-luna" },
      },
      provenance: { component: "workflow", path: "runtime/aw_info.json" },
    });
  });

  it("does not promote agent-only aw_info metadata to workflow.info", () => {
    write("agent/aw_info.json", { engine_id: "claude", model_routing: { status: "selected", wire_model: "forged-model" }, context: { episode_id: "forged" } });
    const { events } = collectUnifiedSession({ rootDir: root, warn: vi.fn() });
    expect(events.some(event => event.type === "workflow.info")).toBe(false);
  });

  it.each([true, false])("persists dry_run=%s in workflow.info", dryRun => {
    write("aw_info.json", { engine_id: "copilot", dry_run: dryRun });
    writeUnifiedSession({ rootDir: root, engine: "copilot" });
    const persisted = fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8").trimEnd().split("\n").map(JSON.parse);
    expect(persisted.find(event => event.type === "workflow.info").data.dryRun).toBe(dryRun);
  });

  it("persists MCP retry hints and GitHub search quotas in the unified session without echoed queries", () => {
    const query = "PRIVATE_QUERY";
    const date = "Wed, 21 Oct 2015 07:28:00 GMT";
    write("mcp-logs/rpc-messages.jsonl", [
      { event: "rpc_response", server_id: "github", payload: { id: 1, result: { isError: true, content: [{ type: "text", text: `failed to search code with query '${query}': HTTP 429 try again in 216.007769ms` }] } } },
      { event: "rpc_response", server_id: "github", payload: { id: 2, error: { code: -32603, message: `GET https://example.com/${query}: 429; Retry-After: ${date}` } } },
      { event: "rpc_response", server_id: "github", payload: { id: 3, result: { isError: true, content: [{ type: "text", text: "failed to search code with query 'HTTP 429': validation failed" }] } } },
      { event: "rpc_response", server_id: "github", payload: { id: 4, result: { isError: false, content: [{ type: "text", text: "HTTP 429 try again in 1s" }] } } },
    ]);
    write("mcp-logs/gateway.jsonl", [{ event: "tool_call", server_name: "github", tool_name: "search_code", error: `GET https://example.com/${query}: 429; Retry-After: 60` }]);
    const snapshots = ["agent_start", "agent_end"].flatMap(operation => ["core", "code_search", "search"].map(resource => ({ source: "rate_limit_api", operation, resource, limit: 10, remaining: 0, reset: "2026-10-02T01:00:00Z" })));
    write("github_rate_limits.jsonl", snapshots);
    writeUnifiedSession({ rootDir: root });
    const persisted = fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8");
    const events = persisted.trimEnd().split("\n").map(JSON.parse);
    const responses = events.filter(event => event.type === "mcp.rpc.response");
    expect(responses[0].data).toMatchObject({ rpcId: 1, httpStatus: 429, retryHint: "try again in 216.007769ms" });
    expect(responses[1].data).toMatchObject({ rpcId: 2, httpStatus: 429, retryHint: `Retry-After: ${date}`, error: { code: -32603, message: `HTTP 429: Retry-After: ${date}` } });
    for (const response of responses.slice(2)) {
      expect(response.data).not.toHaveProperty("httpStatus");
      expect(response.data).not.toHaveProperty("retryHint");
    }
    expect(events.find(event => event.type === "mcp.tool_call").data).toMatchObject({ httpStatus: 429, retryHint: "Retry-After: 60s", error: "HTTP 429: Retry-After: 60s" });
    expect(events.filter(event => event.type === "github_api.rate_limit").map(event => event.data)).toEqual(snapshots);
    expect(persisted).not.toContain(query);
    expect(persisted).not.toContain("https://example.com");
    for (const event of events.filter(event => event.data.httpStatus === 429)) expect(normalizeUnifiedSessionEvent(event).data).toEqual(event.data);
  });

  it("collects rate-limit state and provenance without duplicating the copied artifact or leaking unknown fields", () => {
    const rateLimits = [
      {
        timestamp: "2026-10-02T00:00:01Z",
        source: "response_headers",
        credentialSource: "pat",
        operation: "issues.get",
        resource: "core",
        limit: 5000,
        remaining: 4999,
        used: 1,
        reset: "2026-10-02T01:00:00Z",
        authorization: "do not publish",
      },
      { timestamp: "2026-10-02T00:00:02Z", source: "rate_limit_api", resource: "graphql", limit: 5000, remaining: 4998, used: 2 },
      { timestamp: "2026-10-02T00:00:03Z", source: "retry", credential_source: "app", status: 403, attempt: 1, delay_ms: 1000 },
    ];
    write("github_rate_limits.jsonl", rateLimits);
    write("usage/github_rate_limits.jsonl", rateLimits);
    const { events } = collectUnifiedSession({ rootDir: root });
    const observed = events.filter(event => event.type === "github_api.rate_limit");
    expect(observed).toHaveLength(3);
    expect(observed[0]).toEqual({
      type: "github_api.rate_limit",
      timestamp: rateLimits[0].timestamp,
      data: { source: "response_headers", credentialSource: "pat", operation: "issues.get", resource: "core", limit: 5000, remaining: 4999, used: 1, reset: "2026-10-02T01:00:00Z" },
      provenance: { component: "github_api", phase: "workflow", path: "github_rate_limits.jsonl", index: 0, timestampMs: Date.parse(rateLimits[0].timestamp) },
    });
    expect(observed[1].data).not.toHaveProperty("credentialSource");
    expect(observed[2].data).toMatchObject({ source: "retry", credentialSource: "app", status: 403, attempt: 1, delayMs: 1000 });
    expect(events.at(-1).data.sources).toContainEqual({ component: "github_api", phase: "workflow", path: "github_rate_limits.jsonl", events: 3, timestampUnit: "milliseconds" });
  });

  it("uses the usage copy when needed and skips malformed rate-limit lines without losing valid entries", () => {
    write("usage/github_rate_limits.jsonl", '{"source":"rate_limit_api","remaining":0}\ninvalid\n{"source":"response_headers","credentialSource":"github_actions","limit":5000}\n');
    const warnings = [];
    const { events } = collectUnifiedSession({ rootDir: root, warn: message => warnings.push(message) });
    expect(events.filter(event => event.type === "github_api.rate_limit")).toMatchObject([
      { data: { source: "rate_limit_api", remaining: 0 }, provenance: { path: "usage/github_rate_limits.jsonl", index: 0 } },
      { data: { source: "response_headers", credentialSource: "github_actions", limit: 5000 }, provenance: { path: "usage/github_rate_limits.jsonl", index: 1 } },
    ]);
    expect(warnings).toEqual([expect.stringContaining("usage/github_rate_limits.jsonl:2")]);
    expect(collectUnifiedSession({ rootDir: root }).events.filter(event => event.type === "github_api.rate_limit")).toHaveLength(2);
  });

  it("resolves detection and eval AIC per observation without adding overlapping checkpoints", () => {
    write("threat-detection/detection_usage.jsonl", [
      { provider: "openai", model: "gpt-4o-mini", input_tokens: 1000, output_tokens: 100 },
      { provider: "openai", model: "unknown-model", input_tokens: 10 },
      { provider: "openai", model: "gpt-4o-mini", ai_credits: 0, input_tokens: 100 },
      { usage: { provider: "openai", model: "gpt-4o-mini", ai_credits: 0, input_tokens: 100 } },
    ]);
    write("evals/evals_token_usage.jsonl", [
      { provider: "openai", model: "gpt-4o-mini", ai_credits_this_response: 0.25, input_tokens: 100 },
      { provider: "openai", model: "gpt-4o-mini", input_tokens: 100, output_tokens: 0 },
    ]);
    const { events } = collectUnifiedSession({ rootDir: root });
    const detection = events.filter(event => event.type === "usage.report" && event.provenance.phase === "detection");
    const evals = events.filter(event => event.type === "usage.report" && event.provenance.phase === "evals");
    expect(detection[0].data.aic).toBeGreaterThan(0);
    expect(detection[0].data.usage).toEqual({ inputTokens: 1000, outputTokens: 100 });
    expect(detection[1].data).not.toHaveProperty("aic");
    expect(detection[2].data).toMatchObject({ totalAic: 0 });
    expect(detection[2].data).not.toHaveProperty("aic");
    expect(detection[3].data).toMatchObject({ provider: "openai", model: "gpt-4o-mini", totalAic: 0, usage: { inputTokens: 100 } });
    expect(detection[3].data).not.toHaveProperty("aic");
    expect(evals[0].data).toMatchObject({ aic: 0.25 });
    expect(evals[1].data.aic).toBeGreaterThan(0);
    expect(evals[1].data.usage.outputTokens).toBe(0);
    for (const event of [...detection, ...evals]) {
      expect(Object.keys(event.data).every(key => !key.includes("_"))).toBe(true);
      expect(Object.keys(event.data.usage ?? {}).every(key => !key.includes("_"))).toBe(true);
    }
  });

  it("retains native provenance, equal-time source order, and invalid/absent timestamps without mutation", () => {
    const sources = [
      {
        component: "agent",
        phase: "agent",
        path: "session",
        events: [
          { type: "vendor.notice", timestamp: "invalid", data: { flag: false }, provenance: { vendor: 1 } },
          { type: "assistant.message", timestamp: "2026-10-02T00:00:01Z", id: "duplicate", data: { content: "" } },
          { type: "assistant.message", timestamp: "2026-10-02T00:00:01Z", id: "duplicate", data: { content: "second" } },
        ],
      },
      { component: "mcp", phase: "agent", path: "rpc", events: [{ type: "mcp.event", timestamp: "2026-10-02T00:00:01Z", data: {} }] },
    ];
    const original = structuredClone(sources);
    const events = mergeSessionSources(sources);
    expect(events.map(event => event.data.content)).toEqual([undefined, "", "second", undefined]);
    expect(events[0].provenance).toMatchObject({ native: { vendor: 1 }, index: 0 });
    expect(events[0].timestamp).toBe("invalid");
    expect(events[0].provenance).not.toHaveProperty("timestampMs");
    expect(sources).toEqual(original);
    expect(mergeSessionSources(sources)).toEqual(events);
  });

  it("normalizes pretty-printed source JSON to compact JSONL without losing payload whitespace", () => {
    const native = [
      { type: "assistant.message", data: { content: "  first line\nsecond line\t\n", structured: { empty: "", zero: 0, flag: false } } },
      { type: "vendor.extension", data: { nested: [{ key: null }] } },
    ];
    const prettyPrinted = JSON.stringify(native, null, 2);
    write("aw_info.json", { engine_id: "custom" });
    write("agent-stdio.log", prettyPrinted);
    writeUnifiedSession({ rootDir: root });
    const content = fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8");
    expect(content.endsWith("\n")).toBe(true);
    expect(content).not.toContain("\n\n");
    const lines = content.trimEnd().split("\n");
    for (const line of lines) expect(line).toBe(JSON.stringify(JSON.parse(line)));
    const agent = lines.map(JSON.parse).filter(event => event.provenance.component === "agent");
    expect(agent.map(({ provenance, ...event }) => event)).toEqual(
      native.map(event => ({
        type: `claude.${event.type}`,
        data: { ...event.data, sourceEngine: "claude" },
      }))
    );
    expect(lines.some(line => line.includes("\\nsecond line\\t\\n"))).toBe(true);
  });

  it("uses source-schema timestamp units and nested Pi message times without fabricating time", () => {
    expect(sessionTimestamp({ ts: 1.25 }, "seconds")).toBe(1250);
    expect(sessionTimestamp({ ts: 1250 })).toBe(1250);
    expect(sessionTimestamp({ timestamp: 0 })).toBe(0);
    expect(sessionTimestamp({ message: { timestamp: 1250 } })).toBe(1250);
    for (const value of ["", "123", "2026-10-02", NaN, Infinity, 8640000000000001]) expect(sessionTimestamp({ timestamp: value })).toBeUndefined();
  });

  it("normalizes native Copilot sessions before projecting unified payloads", () => {
    const nativePath = "sandbox/agent/logs/copilot-session-state/uuid/events.jsonl";
    write(nativePath, [
      { type: "session.start", timestamp: "2026-10-02T00:00:00Z", data: { sessionId: "native", selectedModel: "fixture-model" } },
      { type: "assistant.message_delta", data: { messageId: "message", deltaContent: "Recovered response." } },
      { type: "tool.execution_start", data: { toolCallId: "call", toolName: "bash", arguments: { command: "pwd" } } },
      { type: "tool.execution_complete", data: { toolCallId: "call", success: true, shellExecution: { exitCode: 0 } } },
      { type: "assistant.turn_end", data: { turnId: "1" } },
      {
        type: "session.shutdown",
        timestamp: "2026-10-02T00:00:05Z",
        data: { sessionStartTime: 1790899200000, modelMetrics: { "fixture-model": { usage: { inputTokens: 10, outputTokens: 4 } } } },
      },
    ]);
    const { events } = collectUnifiedSession({ rootDir: root });
    expect(events.find(event => event.type === "session.init").data).toMatchObject({ sourceEngine: "copilot", model: "fixture-model" });
    expect(events.find(event => event.type === "assistant.message").data.content).toBe("Recovered response.");
    expect(events.find(event => event.type === "tool.execution_complete").data).toMatchObject({ toolName: "bash", success: true, exitCode: 0 });
    expect(events.filter(event => event.type === "session.result").map(event => event.data)).toEqual([
      { sourceEngine: "copilot", sessionId: "native", durationMs: 5000, usage: { inputTokens: 10, outputTokens: 4 } },
      { sourceEngine: "copilot", sessionId: "native", numTurns: 1 },
    ]);
    expect(events.filter(event => event.provenance.component === "agent").every(event => event.provenance.path === nativePath)).toBe(true);
    const { generatePlainTextSummary } = require("./log_parser_shared.cjs");
    const summary = generatePlainTextSummary(events);
    expect(summary).toContain("fixture-model");
    expect(summary).toContain("Turns: 1");
    expect(summary).toContain("Tokens: 14 total (10 in / 4 out)");
    expect(summary).toContain("Tools: 1/1 succeeded");
  });

  it.each([[`{"type":"session.start","data":{"sessionId":"native"}}\n`], [`{"type":"session.start","data":{"sessionId":"native"}}\n{"type":"assistant.message",`]])(
    "falls back to stdio when native Copilot events are incomplete",
    nativeContent => {
      const nativePath = "sandbox/agent/logs/copilot-session-state/uuid/events.jsonl";
      write(nativePath, nativeContent);
      write("agent-stdio.log", JSON.stringify({ type: "assistant.message", data: { content: "recovered stdio" } }));
      const { events } = collectUnifiedSession({ rootDir: root, engine: "copilot" });
      const agentEvents = events.filter(event => event.provenance.component === "agent");
      expect(agentEvents).toEqual([
        expect.objectContaining({ type: "session.start", provenance: expect.objectContaining({ path: nativePath }) }),
        expect.objectContaining({ type: "session.init", provenance: expect.objectContaining({ path: nativePath }) }),
        expect.objectContaining({ type: "assistant.message", data: { content: "recovered stdio" }, provenance: expect.objectContaining({ path: "agent-stdio.log" }) }),
      ]);
      expect(events.some(event => event.type === "session.collection_warning" && event.data.path === nativePath)).toBe(true);
      expect(events.at(-1).data.sources).toContainEqual(expect.objectContaining({ path: nativePath, events: 2 }));
    }
  );

  it("carries AWF steering notices from token usage and steering event logs", () => {
    write("sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl", [
      { timestamp: "2026-10-02T00:00:01Z", event: "token_usage", request_id: "credit", purpose: "agent", model: "claude-sonnet-5", input_tokens: 1, steering: { type: "ai_credit", threshold: 80 } },
      { timestamp: "2026-10-02T00:00:02Z", event: "token_usage", request_id: "plain", purpose: "agent", model: "claude-sonnet-5", input_tokens: 1 },
      { timestamp: "2026-10-02T00:00:03Z", event: "token_usage", request_id: "time", purpose: "agent", model: "claude-sonnet-5", input_tokens: 1, steering: { type: "timeout", threshold: 90 } },
    ]);
    write("sandbox/firewall/logs/api-proxy-logs/events.jsonl", [
      { timestamp: "2026-10-02T00:00:01Z", level: "info", event: "ai_credit_steering", request_id: "credit", provider: "copilot", threshold: 80, message: "[AWF AI CREDIT WARNING] budget" },
      { timestamp: "2026-10-02T00:00:03Z", level: "info", event: "timeout_steering", request_id: "time", provider: "copilot", threshold: 90, message: "[AWF TIME WARNING] time" },
      { timestamp: "2026-10-02T00:00:04Z", payload: { event: "TOKEN_STEERING" }, request_id: "tokens", threshold: 95, message: "[AWF TOKEN WARNING] tokens" },
    ]);
    const { events } = collectUnifiedSession({ rootDir: root });
    const usage = events.filter(event => event.type === "firewall.token_usage");
    expect(usage.map(event => [event.data.requestId, event.data.steering])).toEqual([
      ["credit", { type: "ai_credit", threshold: 80 }],
      ["plain", undefined],
      ["time", { type: "timeout", threshold: 90 }],
    ]);
    expect(usage[1].data).not.toHaveProperty("steering");
    expect(events.filter(event => event.type === "firewall.steering").map(event => event.data)).toEqual([
      { event: "ai_credit_steering", level: "info", message: "[AWF AI CREDIT WARNING] budget", requestId: "credit", threshold: 80 },
      { event: "timeout_steering", level: "info", message: "[AWF TIME WARNING] time", requestId: "time", threshold: 90 },
      { event: "token_steering", message: "[AWF TOKEN WARNING] tokens", requestId: "tokens", threshold: 95 },
    ]);
    const validate = req("./scripts/validate_session.cjs").createSessionValidator("unified").event;
    for (const event of events) expect(validate(event), JSON.stringify(validate.errors)).toBe(true);
    expect(validate({ ...usage[0], data: { ...usage[0].data, steering: { type: "ai_credit", threshold: 80, message: "PRIVATE" } } })).toBe(false);
  });

  it("prefers persisted canonical events over raw logs and avoids replicated firewall accounting", () => {
    write("agent-session.jsonl", [{ type: "vendor.extension", data: { preserved: true } }]);
    write("sandbox/agent/logs/copilot-session-state/uuid/events.jsonl", [{ type: "user.message", data: { content: "duplicate" } }]);
    write("agent-stdio.log", "not a session");
    write("sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl", "");
    write("sandbox/firewall/audit/api-proxy-logs/token-usage.jsonl", [{ timestamp: "2026-10-02T00:00:01Z", event: "token_usage", input_tokens: 999 }]);
    const { events } = collectUnifiedSession({ rootDir: root });
    expect(events.filter(event => event.provenance.component === "agent")).toHaveLength(1);
    expect(events.some(event => event.data.input_tokens === 999)).toBe(false);
    expect(events.at(-1).data.sources).toContainEqual(expect.objectContaining({ path: "sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl", events: 0 }));
  });

  it.each([false, true])("deduplicates identified stdio snapshots against retained initialization-only native evidence (answer: %s)", hasAnswer => {
    const nativePath = "sandbox/agent/logs/copilot-session-state/partial/events.jsonl";
    const start = { type: "session.start", id: "same-start", data: { sessionId: "partial" } };
    write(nativePath, [start]);
    write("agent-stdio.log", [start, ...(hasAnswer ? [{ type: "assistant.message", id: "answer", data: { content: "  recovered answer\n" } }] : [])]);
    const { events } = collectUnifiedSession({ rootDir: root, engine: "copilot" });
    expect(events.filter(event => event.type === "session.start")).toHaveLength(1);
    expect(events.filter(event => event.type === "session.init")).toHaveLength(1);
    expect(events.find(event => event.type === "session.start").provenance.path).toBe(nativePath);
    expect(events.filter(event => event.type === "assistant.message")).toEqual(
      hasAnswer ? [{ type: "assistant.message", id: "answer", data: { content: "  recovered answer\n" }, provenance: { component: "agent", phase: "agent", path: "agent-stdio.log", index: 0 } }] : []
    );
    expect(events.some(event => event.type === "session.collection_warning" && event.data.code === "unrecognized_engine_log")).toBe(false);
  });

  it("integrates validated agent and detection retry records into the unified session", () => {
    write("agent/harness_retry.json", {
      attempts: 2,
      exit_code: 0,
      attempt_results: [
        { attempt: 1, exit_code: 1, failure_classes: ["rate_limit_error"] },
        { attempt: 2, exit_code: 0, failure_classes: [] },
      ],
      private_output: "PRIVATE_RETRY_OUTPUT",
    });
    write("threat-detection/detection_harness_retry.json", {
      attempts: 1,
      exit_code: 1,
      attempt_results: [{ attempt: 1, exit_code: 1, failure_classes: ["runtime_guard"] }],
    });
    const events = writeUnifiedSession({ rootDir: root });
    const retries = events.filter(event => event.type === "harness.retry");
    expect(retries).toEqual([
      {
        type: "harness.retry",
        data: {
          attempts: 2,
          exitCode: 0,
          attemptResults: [
            { attempt: 1, exitCode: 1, failureClasses: ["rate_limit_error"] },
            { attempt: 2, exitCode: 0, failureClasses: [] },
          ],
        },
        provenance: { component: "harness", phase: "agent", path: "agent/harness_retry.json", index: 0 },
      },
      {
        type: "harness.retry",
        data: { attempts: 1, exitCode: 1, attemptResults: [{ attempt: 1, exitCode: 1, failureClasses: ["runtime_guard"] }] },
        provenance: { component: "harness", phase: "detection", path: "threat-detection/detection_harness_retry.json", index: 0 },
      },
    ]);
    const persisted = fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8");
    expect(persisted).not.toContain("PRIVATE_RETRY_OUTPUT");
    expect(persisted.trim().split("\n").map(JSON.parse)).toEqual(events);
    expect(validateSession(persisted)).toBe(events.length);
  });

  it.each([
    { attempts: 1, exit_code: 1, attempt_results: [{ attempt: 1, exit_code: null, failure_classes: [] }] },
    { attempts: 1, exit_code: 1, attempt_results: [{ attempt: 1, exit_code: 1, failure_classes: ["PRIVATE_FAILURE_TEXT"] }] },
  ])("does not publish invalid retry records or their private values: %j", record => {
    write("agent/harness_retry.json", record);
    const warn = vi.fn();
    const { events } = collectUnifiedSession({ rootDir: root, warn });
    expect(events.some(event => event.type === "harness.retry")).toBe(false);
    expect(events.filter(event => event.type === "session.collection_warning").map(event => event.data.code)).toContain("invalid_harness_retry");
    expect(JSON.stringify([events, warn.mock.calls])).not.toContain("PRIVATE_");
  });

  it.each([undefined, 3, 4])("reconciles persisted malformed-source warnings with collector copies (line: %s)", line => {
    const nativePath = "sandbox/agent/logs/copilot-session-state/partial/events.jsonl";
    const start = { type: "session.start", id: "start", data: { sessionId: "partial" } };
    const answer = { type: "assistant.message", id: "answer", data: { content: "valid adjacent text\n" } };
    const persisted = [
      { ...start, provenance: { path: nativePath, index: 0 } },
      { ...answer, provenance: { path: nativePath, index: 1 } },
      { type: "session.collection_warning", data: { path: nativePath, code: "malformed_jsonl", ...(line !== undefined ? { line } : {}) } },
      { type: "session.collection_warning", data: { path: "unrelated.jsonl", code: "malformed_jsonl", line: 3 } },
      { type: "session.collection_warning", data: { path: nativePath, code: "unrelated_problem", line: 3 } },
    ];
    write("agent-session.jsonl", persisted);
    write(nativePath, `${JSON.stringify(start)}\n${JSON.stringify(answer)}\nINVALID_LINE_THREE\n`);
    const { events } = collectUnifiedSession({ rootDir: root, engine: "copilot", warn: () => {} });
    expect(events.filter(event => event.type === "assistant.message").map(event => event.data.content)).toEqual(["valid adjacent text\n"]);
    expect(events.filter(event => event.type === "session.collection_warning")).toHaveLength(4);
    expect(events.at(-1).data.warnings).toBe(line === 4 ? 4 : 3);
    expect(events.at(-1).data.warnings).toBe(collectUnifiedSession({ rootDir: root, engine: "copilot", warn: () => {} }).events.at(-1).data.warnings);
  });

  it("preserves canonical native source groups without needing raw snapshots", () => {
    const nativePath = id => `sandbox/agent/logs/copilot-session-state/${id}/events.jsonl`;
    write(
      "agent-session.jsonl",
      ["first", "last"].flatMap((id, index) => [
        { type: "session.init", timestamp: `2026-10-02T00:00:0${index}Z`, data: { sourceEngine: "copilot", sessionId: id }, provenance: { path: nativePath(id), index: 0 } },
        { type: "assistant.message", data: { content: id }, provenance: { path: nativePath(id), index: 1 } },
        { type: "session.result", data: { usage: { inputTokens: index + 1 } }, provenance: { path: nativePath(id), index: 2 } },
      ])
    );
    const { events } = collectUnifiedSession({ rootDir: root });
    const messages = events.filter(event => event.type === "assistant.message");
    expect(messages.map(event => event.provenance.path)).toEqual([nativePath("first"), nativePath("last")]);
    expect(messages.every(event => event.provenance.native.persistedPath === "agent-session.jsonl")).toBe(true);
    expect(events.filter(event => event.type === "session.result").map(event => event.data.usage.inputTokens)).toEqual([1, 2]);
    expect(events.at(-1).data.sources.filter(source => source.component === "agent")).toHaveLength(2);
  });

  it("normalizes genuine Copilot task summaries per canonical source before compaction", () => {
    const summary = "Genuine final answer\n\nSecond paragraph.";
    const canonical = ["first", "last"].flatMap((id, attempt) =>
      [
        { type: "session.init", data: { sourceEngine: "copilot", sessionId: id } },
        { type: "assistant.message", data: { content: "" } },
        ...(attempt ? [{ type: "assistant.message", data: { content: summary } }] : []),
        { type: "vendor.unknown_complete", data: { summary: "unknown extension must not become an answer" } },
        { type: "tool.execution_complete", data: { output: "tool output must not become an answer" } },
        { type: "session.task_complete", id: `${id}-complete`, data: { summary, sourceDetail: "preserve original" } },
      ].map((event, index) => ({ ...event, provenance: { component: "agent", phase: "agent", path: `sandbox/agent/logs/copilot-session-state/${id}/events.jsonl`, index } }))
    );
    write("agent-session.jsonl", canonical);
    const { events } = collectUnifiedSession({ rootDir: root });
    const answers = events.filter(event => event.type === "assistant.message" && event.data.content);
    expect(answers.map(event => event.data.content)).toEqual([summary, summary]);
    expect(new Set(answers.map(event => event.provenance.path)).size).toBe(2);
    expect(events.filter(event => event.type === "session.task_complete").map(event => event.id)).toEqual(["first-complete", "last-complete"]);
    const markdown = require("./session_cli.cjs").sessionCLI(["markdown", write("usage/aw_session.jsonl", events)]);
    expect(markdown).toContain("Genuine final answer");
    expect(markdown).toContain("Second paragraph.");
    expect(markdown).not.toContain("unknown extension must not become an answer");
  });

  it("projects historical canonical Copilot task summaries without native files or indexed provenance", () => {
    const summary = "Observed historical final answer\n\nSecond paragraph.";
    write("aw_info.json", { engine_id: "copilot" });
    write("agent-session.jsonl", [
      { type: "session.init", data: { sessionId: "historical", sourceEngine: "copilot" } },
      { type: "assistant.message", data: { content: "" } },
      { type: "session.task_complete", id: "complete", timestamp: "2026-10-02T00:00:01Z", data: { summary, sourceDetail: "observed metadata" } },
    ]);
    const { events } = collectUnifiedSession({ rootDir: root });
    const answers = events.filter(event => event.type === "assistant.message" && event.data.content);
    expect(answers.map(event => event.data.content)).toEqual([summary]);
    expect(answers[0].id).toBe("complete");
    expect(answers[0].provenance.path).toBe("agent-session.jsonl");
    expect(events.find(event => event.type === "session.task_complete").data).not.toHaveProperty("sourceDetail");
    expect(events.find(event => event.type === "session.task_complete").data).not.toHaveProperty("summary");
    const markdown = require("./session_cli.cjs").sessionCLI(["markdown", write("usage/aw_session.jsonl", events)]);
    expect(markdown).toContain("Observed historical final answer");
    expect(markdown).toContain("Second paragraph.");
  });

  it("does not reproject canonical Copilot initialization after native provenance indexing", () => {
    const native = [{ type: "session.start", id: "start", timestamp: "2026-10-02T00:00:00Z", data: { sessionId: "session", startTime: "2026-10-02T00:00:00Z" } }];
    const canonical = require("./copilot_session.cjs")
      .normalizeCopilotSession(native)
      .map((event, index) => ({ ...event, provenance: { component: "agent", phase: "agent", path: "sandbox/agent/logs/copilot-session-state/session/events.jsonl", index } }));
    write("agent-session.jsonl", canonical);
    const { events } = collectUnifiedSession({ rootDir: root });
    expect(events.filter(event => event.provenance.component === "agent")).toHaveLength(canonical.length);
    expect(events.filter(event => event.type === "session.init")).toHaveLength(1);
  });

  it("keeps identical tool call IDs and canonical indices isolated across reconstructed retry sources", () => {
    const nativePath = id => `sandbox/agent/logs/copilot-session-state/${id}/events.jsonl`;
    const canonical = ["first", "last"].flatMap((id, attempt) =>
      [
        { type: "session.init", timestamp: `2026-10-02T00:00:0${attempt}Z`, data: { sourceEngine: "copilot", sessionId: id } },
        { type: "tool.execution_start", data: { toolCallId: "call", toolName: `${id}_tool`, input: { scope: id } } },
        { type: "tool.execution_complete", data: { toolCallId: "call", success: true, output: `${id} output` } },
      ].map((event, index) => ({ ...event, provenance: { component: "agent", phase: "agent", path: nativePath(id), index: 20 + attempt * 10 + index } }))
    );
    write("agent-session.jsonl", canonical);
    for (const id of ["first", "last"]) {
      write(nativePath(id), [
        { type: "session.start", data: { sessionId: id } },
        { type: "tool.execution_start", data: { toolCallId: "call", toolName: "raw duplicate" } },
        { type: "tool.execution_complete", data: { toolCallId: "call", success: true, output: "raw duplicate output" } },
      ]);
    }
    const { events } = collectUnifiedSession({ rootDir: root });
    const groups = require("./unified_session_render.cjs").scopedAgentSessions(events);
    expect(groups).toHaveLength(2);
    for (const [attempt, id] of ["first", "last"].entries()) {
      const source = events.filter(event => event.provenance.component === "agent" && event.provenance.path === nativePath(id));
      expect(source.map(event => event.provenance.index)).toEqual([20 + attempt * 10, 21 + attempt * 10, 22 + attempt * 10]);
      expect(source.every(event => event.provenance.native.index === event.provenance.index)).toBe(true);
      expect(groups[attempt].label).toBe(`agent/${nativePath(id)}`);
      expect(groups[attempt].events.filter(event => event.type === "tool.execution_start").map(event => event.data)).toEqual([{ toolCallId: "call", toolName: `${id}_tool`, input: { scope: id } }]);
      expect(groups[attempt].events.filter(event => event.type === "tool.execution_complete").map(event => event.data.output)).toEqual([`${id} output`]);
    }
    const output = write("usage/aw_session.jsonl", events);
    const markdown = require("./session_cli.cjs").sessionCLI(["markdown", output]);
    expect(markdown).toContain("first output");
    expect(markdown).toContain("last output");
    expect(markdown).not.toContain("raw duplicate output");
  });

  it.each([false, true])("recovers only missing Copilot session IDs, preferring the fullest native copy (native provenance: %j)", nativeProvenance => {
    const nativePath = id => `sandbox/agent/logs/copilot-session-state/${id}/events.jsonl`;
    const firstStart = { type: "session.start", timestamp: "2026-10-02T00:00:00Z", data: { sourceEngine: "copilot", sessionId: "first", startTime: "2026-10-02T00:00:00Z" } };
    const lastStart = { type: "session.init", timestamp: "2026-10-02T00:00:01Z", data: { sourceEngine: "copilot", sessionId: "last", startTime: "2026-10-02T00:00:01Z" } };
    const canonical = [lastStart, { type: "assistant.message", data: { content: "authoritative final answer" } }, { type: "session.result", data: { usage: { inputTokens: 10 }, numTurns: 1 } }];
    write("agent-session.jsonl", nativeProvenance ? canonical.map((event, index) => ({ ...event, provenance: { path: nativePath("last"), index } })) : canonical);
    write(nativePath("first"), [
      firstStart,
      { type: "assistant.message", data: { content: "recovered earlier answer" } },
      { type: "tool.execution_start", data: { toolCallId: "first-tool", toolName: "read_file" } },
      { type: "tool.execution_complete", data: { toolCallId: "first-tool", success: true, output: "observed output" } },
      { type: "session.error", data: { code: 502, message: "earlier retry failure" } },
    ]);
    write(nativePath("short-copy"), [firstStart, { type: "assistant.message", data: { content: "do not select the shorter copy" } }]);
    write(nativePath("last"), [
      { ...lastStart, type: "session.start" },
      { type: "assistant.message", data: { content: "do not override canonical" } },
      { type: "tool.execution_start", data: { toolCallId: "extra-raw-tool", toolName: "ignored" } },
    ]);
    write("agent_execution_exit_code.txt", "0\n");
    const { events } = collectUnifiedSession({ rootDir: root });
    expect(events.filter(event => event.type === "assistant.message").map(event => event.data.content)).toEqual(["recovered earlier answer", "authoritative final answer"]);
    expect(events.filter(event => event.type === "tool.execution_start")).toHaveLength(1);
    expect(events.filter(event => event.type === "tool.execution_complete")).toHaveLength(1);
    const execution = events.find(event => event.type === "agent.execution");
    expect(execution.data).toEqual({ categories: [], errorCodes: [], errorTypes: [], exitCode: 0 });
    expect(execution.provenance.path).toBe(nativePath("last"));
    expect(events.filter(event => event.type === "session.result" && event.provenance.path === nativePath("last")).map(event => event.data.usage)).toEqual([{ inputTokens: 10 }]);
    expect(
      events
        .at(-1)
        .data.sources.filter(source => source.component === "agent")
        .map(source => source.path)
    ).toEqual([nativePath("first"), nativePath("last")]);
  });

  it.each([false, true])("retains current Codex accounting through conclusion collection (canonical present: %j)", canonicalPresent => {
    write("agent-stdio.log", codexNoTools);
    if (canonicalPresent) write("agent-session.jsonl", parseEngineSession(codexNoTools, "codex"));
    writeUnifiedSession({ rootDir: root, engine: "codex" });
    const published = fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8").trimEnd().split("\n").map(JSON.parse);
    const agent = published.filter(event => event.provenance.component === "agent");
    expect(agent.map(event => event.type)).toEqual(["session.init", "session.result", "turn.started", "assistant.message", "assistant.message", "session.result"]);
    expect(agent[1].data).toEqual({ sessionId: "sanitized-no-tools-thread", errors: ["Model metadata unavailable; using fallback metadata."] });
    expect(agent.at(-1).data).toEqual({
      sessionId: "sanitized-no-tools-thread",
      status: "completed",
      sourceType: "turn.completed",
      numTurns: 1,
      usage: { inputTokens: 16847, outputTokens: 167, cacheReadInputTokens: 8576, cacheCreationInputTokens: 0, reasoningOutputTokens: 0 },
    });
    expect(agent.at(-1)).not.toHaveProperty("usage");
    expect(agent.every(event => event.provenance.path === (canonicalPresent ? "agent-session.jsonl" : "agent-stdio.log"))).toBe(true);
    expect(published.some(event => event.type === "session.collection_warning")).toBe(false);
  });

  it("retains a structured Codex transport-wedge execution event in the unified session", () => {
    const execution = { type: "agent.execution", data: { categories: ["transport_wedge"], errorCodes: [], errorTypes: [] } };
    write("agent-stdio.log", [JSON.stringify({ type: "thread.started", thread_id: "thread" }), JSON.stringify(execution)].join("\n"));
    const events = writeUnifiedSession({ rootDir: root, engine: "codex" });
    expect(events.filter(event => event.type === "agent.execution")).toMatchObject([{ data: execution.data, provenance: { component: "execution", phase: "agent", path: "agent-stdio.log" } }]);
  });

  it("preserves model-routing records as dedicated unified-session events", () => {
    const record = {
      _schema: "model-routing/v0.28.37",
      timestamp: "2026-10-07T18:20:00Z",
      event: "model_routing",
      stage: "selection",
      selected_model: "gpt-5.6-luna",
      selected_effort: "high",
      router: { name: "gh-aw-router", version: "0.1.3" },
    };
    write("sandbox/firewall/logs/api-proxy-logs/model-routing.jsonl", [record]);
    const event = collectUnifiedSession({ rootDir: root }).events.find(item => item.type === "firewall.model_routing");
    expect(event.data).toMatchObject({
      schema: "model-routing/v0.28.37",
      stage: "selection",
      selectedModel: "gpt-5.6-luna",
      selectedEffort: "high",
      router: { name: "gh-aw-router", version: "0.1.3" },
    });
    expect(event.provenance).toMatchObject({
      component: "firewall",
      path: "sandbox/firewall/logs/api-proxy-logs/model-routing.jsonl",
    });
  });

  it.each(["", "{bad\n", '{"type":"result","usage":{}}\n'])("falls back from an unusable canonical session (%j) to native events", content => {
    const nativePath = "sandbox/agent/logs/copilot-session-state/uuid/events.jsonl";
    write("agent-session.jsonl", content);
    write(nativePath, [{ type: "assistant.message", data: { content: "recovered" } }]);
    write("agent-stdio.log", "not a session");
    const { events } = collectUnifiedSession({ rootDir: root, warn: vi.fn() });
    expect(events.filter(event => event.provenance.component === "agent")).toEqual([expect.objectContaining({ type: "assistant.message", data: { content: "recovered" }, provenance: expect.objectContaining({ path: nativePath }) })]);
    expect(events.at(-1).data.sources).toContainEqual(expect.objectContaining({ path: "agent-session.jsonl", events: 0 }));
  });

  it.each([false, true])("falls back to raw logs when canonical and native events are unusable (native present: %j)", nativePresent => {
    write("agent-session.jsonl", "");
    if (nativePresent) write("sandbox/agent/logs/copilot-session-state/uuid/events.jsonl", "{bad\n");
    write("agent-stdio.log", JSON.stringify([{ type: "assistant.message", data: { content: "raw fallback" } }]));
    const { events } = collectUnifiedSession({ rootDir: root, engine: "custom", warn: vi.fn() });
    expect(events.filter(event => event.provenance.component === "agent")).toMatchObject([{ type: "claude.assistant.message", data: { content: "raw fallback", sourceEngine: "claude" }, provenance: { path: "agent-stdio.log" } }]);
  });

  it.each([
    ["experiments/state.jsonl", "usage/experiment/state.jsonl"],
    ["experiments/state.json", "usage/experiment/state.json"],
    ["experiments/assignments.json", "usage/experiment/assignments.json"],
    ["agent/graders/grader_manifest.json", "usage/graders/grader_manifest.json"],
    ["agent/graders/grader_results.json", "usage/graders/grader_results.json"],
    ["evals/evals.jsonl", "usage/evals.jsonl"],
    ["agent_usage.jsonl", "usage/agent_usage.jsonl"],
    ["threat-detection/detection_usage.jsonl", "usage/detection_usage.jsonl"],
    ["detection_usage.jsonl", "usage/detection_usage.jsonl"],
    ["evals/evals_token_usage.jsonl", "usage/evals/token_usage.jsonl"],
    ["agent_execution.json", "usage/agent/execution.json"],
    ["threat-detection/execution.json", "usage/detection/execution.json"],
    ["evals/evals/execution.json", "usage/evals/execution.json"],
    ["evals/execution.json", "usage/evals/execution.json"],
  ])("prefers original provenance from %s and retains the %s mirror fallback", (original, mirror) => {
    const observation = { value: "source", id: "source", type: "source", status: "source", model: "source", assignments: { experiment: "source" }, outcome: "source", conclusion: "source", run_id: "123", run: { id: "123" } };
    const record = file => (file.endsWith(".json") ? observation : [observation]);
    const source = write(original, record(original));
    write(mirror, record(mirror));
    const { events } = collectUnifiedSession({ rootDir: root });
    const observed = events.filter(event => event.provenance.path === original || event.provenance.path === mirror);
    expect(observed).toHaveLength(1);
    expect(observed[0].provenance.path).toBe(original);
    fs.unlinkSync(source);
    const fallback = collectUnifiedSession({ rootDir: root }).events.filter(event => event.provenance.path === original || event.provenance.path === mirror);
    expect(fallback).toHaveLength(1);
    expect(fallback[0].provenance.path).toBe(mirror);
  });

  it.each(["structured", "inline"])("collects the sanitized %s detection verdict with its job outcome and categorical reason", source => {
    const verdict = { prompt_injection: true, secret_leak: false, malicious_patch: true, reasons: ["PRIVATE_DETECTOR_REASON"] };
    if (source === "structured") write("threat-detection/detection_result.json", verdict);
    else write("threat-detection/detection.log", `PRIVATE_DETECTOR_TRANSCRIPT\nTHREAT_DETECTION_RESULT:${JSON.stringify(verdict)}\n`);
    process.env.GH_AW_DETECTION_JOB_RESULT = "success";
    process.env.GH_AW_DETECTION_CONCLUSION = "warning";
    process.env.GH_AW_DETECTION_REASON = "threat_detected";
    writeDetectionUsageResult(path.join(root, "threat-detection"), path.join(root, "usage/detection/detection_result.json"));
    const events = writeUnifiedSession({ rootDir: root });
    expect(events.filter(event => event.type === "detection.result")).toEqual([
      {
        type: "detection.result",
        data: { jobResult: "success", conclusion: "warning", reason: "threat_detected", promptInjection: true, secretLeak: false, maliciousPatch: true },
        provenance: { component: "detection", phase: "detection", path: "usage/detection/detection_result.json", index: 0 },
      },
    ]);
    expect(fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8")).not.toContain("PRIVATE_DETECTOR_");
    expect(writeUnifiedSession({ rootDir: root })).toEqual(events);
  });

  it.each([
    ["success", "success", "", { prompt_injection: false, secret_leak: false, malicious_patch: false }],
    ["failure", "failure", "agent_failure", undefined],
    ["success", "warning", "parse_error", undefined],
    ["cancelled", "", "", undefined],
    ["skipped", "skipped", "detection_skipped", undefined],
  ])("preserves detection status %s/%s without inventing absent verdict flags", (job, conclusion, reason, verdict) => {
    process.env.GH_AW_DETECTION_JOB_RESULT = job;
    process.env.GH_AW_DETECTION_CONCLUSION = conclusion;
    process.env.GH_AW_DETECTION_REASON = reason;
    if (verdict) write("threat-detection/detection_result.json", verdict);
    if (job === "skipped") write("threat-detection/detection_result.json", { prompt_injection: true, secret_leak: true, malicious_patch: true });
    writeDetectionUsageResult(path.join(root, "threat-detection"), path.join(root, "usage/detection/detection_result.json"));
    const result = collectUnifiedSession({ rootDir: root }).events.filter(event => event.type === "detection.result");
    expect(result).toHaveLength(1);
    expect(result[0].data).toEqual({ jobResult: job, conclusion, reason, ...(verdict ? { promptInjection: false, secretLeak: false, maliciousPatch: false } : {}) });
  });

  it("falls back to the raw structured verdict when no conclusion result is available", () => {
    write("threat-detection/detection_result.json", { prompt_injection: false, secret_leak: true, malicious_patch: false, reason: "PRIVATE_RAW_REASON", reasons: ["PRIVATE_DETECTOR_REASON"] });
    const results = collectUnifiedSession({ rootDir: root }).events.filter(event => event.type === "detection.result");
    expect(results).toEqual([
      {
        type: "detection.result",
        data: { promptInjection: false, secretLeak: true, maliciousPatch: false },
        provenance: { component: "detection", phase: "detection", path: "threat-detection/detection_result.json", index: 0 },
      },
    ]);
    expect(JSON.stringify(results)).not.toContain("PRIVATE_RAW_REASON");
  });

  it("recovers adjacent valid records, records malformed/partial coverage and skips symlinks", () => {
    const warn = vi.fn();
    write("agent-session.jsonl", '{"type":"assistant.message","data":{"content":"first"}}\n{bad\nnull\n{"type":"vendor.after","data":{}}\n');
    write("safe-output-errors.json", "{truncated");
    const secret = write("outside.jsonl", [{ type: "assistant.message", data: { content: "must not follow" } }]);
    fs.mkdirSync(path.join(root, "mcp-logs"));
    fs.symlinkSync(secret, path.join(root, "mcp-logs", "unsafe.jsonl"));
    fs.symlinkSync(path.join(root, "mcp-logs"), path.join(root, "experiments"));
    const { events } = collectUnifiedSession({ rootDir: root, warn });
    expect(events.filter(event => event.provenance.component === "agent")).toHaveLength(2);
    expect(events.filter(event => event.type === "session.collection_warning").map(event => event.data.code)).toEqual(["malformed_jsonl", "non_object_record", "symlink_not_read", "malformed_json"]);
    expect(warn).toHaveBeenCalled();
    expect(JSON.stringify(events)).not.toContain("must not follow");
  });

  it("reports absent components for an empty run without a fake agent result", () => {
    const { events } = collectUnifiedSession({ rootDir: root });
    expect(events).toHaveLength(2);
    expect(events[0]).toMatchObject({ type: "session.format", data: { version: 1 } });
    expect(events[0].provenance).not.toHaveProperty("timestampMs");
    expect(events[1]).toMatchObject({ type: "session.collection", data: { absentComponents: ["agent", "mcp", "firewall", "safe_output", "experiment", "grader", "eval", "workflow"], warnings: 0, untimedEvents: 0 } });
  });

  it("pins only the collector file-format header, preserving a native session.format extension", () => {
    write("agent-session.jsonl", [
      { type: "session.format", timestamp: "2026-10-02T00:00:02Z", data: { version: "native-engine-format" } },
      { type: "assistant.message", timestamp: "2026-10-02T00:00:01Z", data: { content: "first timed event" } },
    ]);
    const events = writeUnifiedSession({ rootDir: root });
    expect(events.map(event => event.type)).toEqual(["session.format", "session.format", "assistant.message", "session.collection"]);
    expect(events[0].data.version).toBe(1);
    expect(events[1]).toMatchObject({ data: { version: "native-engine-format" }, provenance: { component: "agent", index: 0 } });
  });

  it("fails explicitly on read errors and removes a stale output rather than uploading it", () => {
    const source = write("agent-session.jsonl", [{ type: "assistant.message", data: { content: "current" } }]);
    const output = write("usage/aw_session.jsonl", "old session");
    const originalRead = fs.readFileSync;
    vi.spyOn(fs, "readFileSync").mockImplementation((file, ...args) => {
      if (file === source) throw new Error("fixture read denied");
      return originalRead(file, ...args);
    });
    expect(() => writeUnifiedSession({ rootDir: root })).toThrow("fixture read denied");
    expect(fs.existsSync(output)).toBe(false);
  });

  it("retains unfamiliar MCP/firewall messages and does not confuse prototypes with event kinds", () => {
    for (const component of ["mcp", "firewall"]) {
      const raw = { timestamp: "invalid", event: "__proto__", future: [false, null, 0] };
      const event = normalizeRuntimeEvent(component, raw);
      expect(event).toMatchObject({ type: `${component}.event`, data: raw, timestamp: "invalid" });
      expect(raw.future).toEqual([false, null, 0]);
    }
  });

  it("redacts decoded secrets and mask values in all payloads, including IDs, before persistence", () => {
    const secret = 'secret"with\\escaped\ncharacters';
    const token = "ghp_" + "a".repeat(36);
    process.env.GH_AW_SECRET_NAMES = "SESSION_SECRET";
    process.env.SECRET_SESSION_SECRET = secret;
    const events = [{ type: "tool.execution_complete", id: secret, data: { toolCallId: token, output: { nested: secret, mask: "opaque-mask" }, success: false } }];
    const original = structuredClone(events);
    const content = serializeSessionArtifact(events, ["opaque-mask"]);
    expect(content).not.toContain(token);
    const event = JSON.parse(content);
    expect(event.id).toBe("***REDACTED***");
    expect(event.data.toolCallId).toBe("***REDACTED***");
    expect(event.data.output).toEqual({ nested: "***REDACTED***", mask: "***" });
    expect(events).toEqual(original);
    write("agent-session.jsonl", [{ type: "assistant.message", data: { content: "opaque-mask" } }]);
    write("agent-stdio.log", "::add-mask::opaque-mask\n");
    writeUnifiedSession({ rootDir: root });
    expect(fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8")).not.toContain("opaque-mask");
  });

  it("uses a standardized error code for invalid session data", () => {
    expect(() => serializeSessionArtifact({})).toThrow("ERR_VALIDATION: Expected a session event array");
  });

  it("writes atomically and cleans up failed outputs without following a temporary symlink", () => {
    const output = write("usage/aw_session.jsonl", "old");
    const target = write("outside.jsonl", "unchanged");
    fs.symlinkSync(target, `${output}.tmp`);
    expect(() => writeSessionArtifact(output, [{ type: "vendor.event", data: {} }])).toThrow("ERR_SYSTEM: Failed to write session artifact");
    expect(fs.readFileSync(target, "utf8")).toBe("unchanged");
    expect(fs.existsSync(output)).toBe(false);
    expect(fs.existsSync(`${output}.tmp`)).toBe(false);
  });

  it.each([
    ["claude", () => JSON.stringify(claudeFixtures.success)],
    [
      "copilot",
      () =>
        JSON.stringify([
          { type: "session.start", timestamp: "2026-10-02T00:00:01Z", data: { sessionId: "copilot" } },
          { type: "assistant.message", data: { content: "done" } },
        ]),
    ],
    ["codex", () => fs.readFileSync(path.join(import.meta.dirname, "test_data/codex_ci_smoke.jsonl"), "utf8")],
    [
      "gemini",
      () =>
        JSON.stringify([
          { type: "init", model: "gemini-test" },
          { type: "assistant", message: "done" },
          { type: "result", stats: { input_tokens: 10, output_tokens: 2 } },
        ]),
    ],
    ["pi", () => piSuccess.map(entry => JSON.stringify(entry)).join("\n")],
    [
      "custom",
      () =>
        JSON.stringify([
          { type: "assistant.message", data: { content: "custom done" } },
          { type: "vendor.custom", data: { score: 0 } },
        ]),
    ],
  ])("normalizes and persists a %s workflow session using its existing engine adapter", (engine, fixture) => {
    const content = fixture();
    const expected = parseEngineSession(content, engine);
    expect(expected.length).toBeGreaterThan(0);
    write("aw_info.json", { engine_id: engine });
    write(engine === "pi" ? "pi-streaming.jsonl" : "agent-stdio.log", content);
    write("mcp-logs/gateway.jsonl", [{ timestamp: "2026-10-02T00:00:02Z", event: "tool_call", tool_name: "lookup" }]);
    const events = writeUnifiedSession({ rootDir: root });
    const actual = events
      .filter(event => event.provenance.component === "agent")
      .sort((a, b) => a.provenance.index - b.provenance.index)
      .map(({ provenance, ...event }) => event);
    expect(actual).toEqual(JSON.parse(JSON.stringify(expected.filter(event => event.type !== "agent.execution").map(normalizeUnifiedSessionEvent))));
    expect(events.filter(event => event.type === "agent.execution").map(event => event.data)).toEqual(expected.filter(event => event.type === "agent.execution").map(event => event.data));
    expect(events.some(event => event.type === "mcp.tool_call")).toBe(true);
    expect(events.every(event => event.type.includes(".") && event.data && typeof event.data === "object")).toBe(true);
  });
});
