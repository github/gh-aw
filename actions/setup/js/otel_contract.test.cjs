import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "fs";
import os from "node:os";
import path from "node:path";

const { sendJobSetupSpan, sendJobConclusionSpan, OTEL_JSONL_PATH } = await import("./send_otlp_span.cjs");

const MANAGED_ENV_VARS = [
  "GH_AW_OTLP_ENDPOINTS",
  "INPUT_JOB_NAME",
  "INPUT_TRACE_ID",
  "INPUT_PARENT_SPAN_ID",
  "GITHUB_AW_OTEL_TRACE_ID",
  "GITHUB_AW_OTEL_PARENT_SPAN_ID",
  "OTEL_SERVICE_NAME",
  "GH_AW_INFO_WORKFLOW_NAME",
  "GITHUB_RUN_ID",
  "GITHUB_RUN_ATTEMPT",
  "GITHUB_ACTOR",
  "GITHUB_REPOSITORY",
  "GITHUB_EVENT_NAME",
  "GITHUB_JOB",
  "GH_AW_AGENT_CONCLUSION",
  "GH_AW_AIC",
];

const originalEnv = {};

function attrValue(attr) {
  const value = attr.value || {};
  if (Object.prototype.hasOwnProperty.call(value, "stringValue")) return value.stringValue;
  if (Object.prototype.hasOwnProperty.call(value, "intValue")) return value.intValue;
  if (Object.prototype.hasOwnProperty.call(value, "doubleValue")) return value.doubleValue;
  if (Object.prototype.hasOwnProperty.call(value, "boolValue")) return value.boolValue;
  if (value.arrayValue?.values) return value.arrayValue.values.map(v => v.stringValue ?? v.intValue ?? v.doubleValue ?? v.boolValue);
  return undefined;
}

function attrsByKey(span) {
  return Object.fromEntries((span.attributes || []).map(attr => [attr.key, attrValue(attr)]));
}

function firstSpan(payload) {
  return payload.resourceSpans[0].scopeSpans[0].spans[0];
}

describe("durable parent OpenTelemetry evidence", () => {
  let root;
  let appendSpy;
  beforeEach(() => {
    root = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-durable-otel-"));
    vi.stubEnv("GH_AW_TMP_DIR", root);
    vi.stubEnv("INPUT_JOB_NAME", "agent");
    vi.stubEnv("GITHUB_RUN_ID", "200");
    vi.stubEnv("GITHUB_RUN_ATTEMPT", "1");
    vi.stubEnv("GH_AW_OTLP_ENDPOINTS", "");
    vi.stubEnv("GH_AW_OTLP_IF_MISSING", "ignore");
    appendSpy = vi.spyOn(fs, "appendFileSync").mockImplementation(() => {});
  });
  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllEnvs();
    fs.rmSync(root, { recursive: true, force: true });
  });

  it.each([
    { status: "restored", parentRunId: "100", expected: "100" },
    { status: "fresh", expected: undefined },
    { status: "restored", parentRunId: "../../secret", expected: undefined },
    { status: "restored", parentRunId: "100", runAttempt: "2", expected: undefined },
  ])("attaches only a validated current-attempt checkpoint parent: $status / $parentRunId / $runAttempt", async ({ status, parentRunId, runAttempt = "1", expected }) => {
    fs.mkdirSync(path.join(root, "agent"));
    fs.writeFileSync(
      path.join(root, "agent/durable-session.jsonl"),
      JSON.stringify({
        type: "durable.restore",
        data: { engine: "codex", storage: "artifact", runId: "200", runAttempt, status, ...(parentRunId ? { parentRunId, artifactId: "99" } : {}) },
      }) + "\n"
    );
    vi.spyOn(console, "warn").mockImplementation(() => {});
    await sendJobConclusionSpan("gh-aw.agent.conclusion", { startMs: Date.now() - 1000 });
    const spans = appendSpy.mock.calls.filter(call => call[0] === OTEL_JSONL_PATH).map(call => firstSpan(JSON.parse(String(call[1]))));
    expect(spans.length).toBeGreaterThan(0);
    for (const span of spans) expect(attrsByKey(span)["gh-aw.durable.parent_run_id"]).toBe(expected);
    if (parentRunId === "../../secret") expect(console.warn).toHaveBeenCalledWith(expect.stringContaining("Durable session parent unavailable"));
  });
});

describe("gh-aw OpenTelemetry compatibility contract", () => {
  let appendFileSyncSpy;
  let mkdirSyncSpy;
  let readFileSyncSpy;
  let statSyncSpy;
  let fetchMock;

  beforeEach(() => {
    for (const name of MANAGED_ENV_VARS) {
      originalEnv[name] = process.env[name];
      delete process.env[name];
    }

    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "https://traces.example.com" }]);
    process.env.OTEL_SERVICE_NAME = "gh-aw.customer-contract";
    process.env.GH_AW_INFO_WORKFLOW_NAME = "Customer OTEL Contract";
    process.env.GITHUB_RUN_ID = "1234567890";
    process.env.GITHUB_RUN_ATTEMPT = "2";
    process.env.GITHUB_ACTOR = "octocat";
    process.env.GITHUB_REPOSITORY = "github/gh-aw";
    process.env.GITHUB_EVENT_NAME = "workflow_dispatch";
    process.env.GITHUB_JOB = "agent";
    process.env.GH_AW_AGENT_CONCLUSION = "success";

    appendFileSyncSpy = vi.spyOn(fs, "appendFileSync").mockImplementation(() => {});
    mkdirSyncSpy = vi.spyOn(fs, "mkdirSync").mockImplementation(() => undefined);
    statSyncSpy = vi.spyOn(fs, "statSync").mockReturnValue(/** @type {Partial<fs.Stats>} */ { mtimeMs: 1_700_000_005_000 });
    readFileSyncSpy = vi.spyOn(fs, "readFileSync").mockImplementation(filePath => {
      if (filePath === "/tmp/gh-aw/aw_info.json") {
        return JSON.stringify({
          workflow_name: "Customer OTEL Contract",
          engine_id: "claude",
          model: "claude-3-5-sonnet-20241022",
          staged: false,
        });
      }
      if (filePath === "/tmp/gh-aw/agent_usage.json") {
        return JSON.stringify({ input_tokens: 100, output_tokens: 25, ai_credits: 0.125 });
      }
      if (filePath === "/tmp/gh-aw/agent_output.json") {
        return JSON.stringify({ items: [], errors: [] });
      }
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    });

    fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: "OK" });
    vi.stubGlobal("fetch", fetchMock);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
    for (const name of MANAGED_ENV_VARS) {
      if (originalEnv[name] === undefined) {
        delete process.env[name];
      } else {
        process.env[name] = originalEnv[name];
      }
    }
  });

  it("preserves customer-facing raw OTLP JSONL and GenAI compatibility attributes", async () => {
    process.env.INPUT_JOB_NAME = "agent";

    const setup = await sendJobSetupSpan({
      traceId: "a".repeat(32),
      parentSpanId: "b".repeat(16),
      startMs: 1_700_000_000_000,
    });
    process.env.GITHUB_AW_OTEL_TRACE_ID = setup.traceId;
    process.env.GITHUB_AW_OTEL_PARENT_SPAN_ID = setup.spanId;

    await sendJobConclusionSpan("gh-aw.agent.conclusion", { startMs: 1_700_000_001_000 });

    const mirroredPayloads = appendFileSyncSpy.mock.calls.map(([filePath, line]) => {
      expect(filePath).toBe(OTEL_JSONL_PATH);
      return JSON.parse(String(line).trim());
    });

    expect(mirroredPayloads).toHaveLength(3);
    for (const payload of mirroredPayloads) {
      expect(payload).toHaveProperty("resourceSpans");
      expect(payload).not.toHaveProperty("schema");
      expect(payload).not.toHaveProperty("payload");
    }

    const spans = mirroredPayloads.map(firstSpan);
    expect(spans.map(span => span.name)).toEqual(["gh-aw.agent.setup", "gh-aw.agent.agent", "gh-aw.agent.conclusion"]);
    expect(new Set(spans.map(span => span.traceId))).toEqual(new Set(["a".repeat(32)]));

    const setupAttrs = attrsByKey(spans[0]);
    expect(setupAttrs["gh-aw.workflow.name"]).toBe("Customer OTEL Contract");
    expect(setupAttrs["gh-aw.run.id"]).toBe("1234567890");
    expect(setupAttrs["gh-aw.repository"]).toBe("github/gh-aw");
    expect(setupAttrs["gh-aw.engine.id"]).toBe("claude");
    expect(setupAttrs["gen_ai.system"]).toBe("anthropic");

    const agentAttrs = attrsByKey(spans[1]);
    expect(agentAttrs["gen_ai.operation.name"]).toBe("chat");
    expect(agentAttrs["gen_ai.request.model"]).toBe("claude-3-5-sonnet-20241022");
    expect(agentAttrs["gen_ai.system"]).toBe("anthropic");
    expect(agentAttrs["gen_ai.usage.input_tokens"]).toBe(100);
    expect(agentAttrs["gen_ai.usage.output_tokens"]).toBe(25);
    expect(agentAttrs["gen_ai.usage.total_tokens"]).toBe(125);
    expect(agentAttrs["gh-aw.aic"]).toBe(0.125);
    expect(agentAttrs["gh-aw.workflow.name"]).toBe("Customer OTEL Contract");
    expect(agentAttrs["gh-aw.run.id"]).toBe("1234567890");
    expect(agentAttrs["gh-aw.repository"]).toBe("github/gh-aw");
    expect(spans[1].attributes).toContainEqual({ key: "gh-aw.aic", value: { doubleValue: 0.125 } });
    expect(spans.filter(span => Object.hasOwn(attrsByKey(span), "gh-aw.aic"))).toHaveLength(1);

    const exportedPayloads = fetchMock.mock.calls.map(([, options]) => JSON.parse(String(options.body)));
    expect(exportedPayloads).toHaveLength(3);
    for (const payload of exportedPayloads) {
      expect(payload).toHaveProperty("resourceSpans");
      expect(payload).not.toHaveProperty("schema");
      expect(payload).not.toHaveProperty("payload");
    }
    expect(exportedPayloads.map(firstSpan).filter(span => Object.hasOwn(attrsByKey(span), "gh-aw.aic"))).toHaveLength(1);
  });

  it("exports not-evaluated eval status without fabricating UNKNOWN answers", async () => {
    process.env.INPUT_JOB_NAME = "evals";
    const existsSpy = vi.spyOn(fs, "existsSync").mockImplementation(filePath => filePath === "/tmp/gh-aw/evals_status.json" || filePath === "/tmp/gh-aw/evals.jsonl");
    const originalRead = readFileSyncSpy.getMockImplementation();
    readFileSyncSpy.mockImplementation(filePath => {
      if (filePath === "/tmp/gh-aw/evals_status.json") {
        return JSON.stringify({ status: "not_evaluated", reason_code: "judge_failed", reason: "sensitive judge failure", model: "copilot/gpt-6-luna-decisions" });
      }
      if (filePath === "/tmp/gh-aw/evals.jsonl") {
        return JSON.stringify({ id: "eval-1", answer: "YES" }) + "\n";
      }
      return originalRead(filePath);
    });

    await sendJobConclusionSpan("gh-aw.evals.conclusion");

    for (const payload of [JSON.parse(String(fetchMock.mock.calls[0][1].body)), JSON.parse(String(appendFileSyncSpy.mock.calls[0][1]))]) {
      const span = firstSpan(payload);
      expect(attrsByKey(span)).toMatchObject({ "gh-aw.evals.status": "not_evaluated", "gh-aw.evals.reason_code": "judge_failed", "gh-aw.eval.model": "copilot/gpt-6-luna-decisions" });
      expect(span.events.filter(event => event.name === "eval.status")).toHaveLength(1);
      expect(attrsByKey(span.events.find(event => event.name === "eval.status"))["gh-aw.eval.model"]).toBe("copilot/gpt-6-luna-decisions");
      expect(span.events.some(event => event.name === "eval.result")).toBe(false);
      expect(JSON.stringify(payload)).not.toContain("sensitive");
    }
    existsSpy.mockRestore();
  });
});
