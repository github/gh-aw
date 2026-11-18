import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import crypto from "node:crypto";
import fs from "node:fs";
import path from "node:path";

const { sendJobConclusionSpan, sendJobSetupSpan, OTEL_JSONL_PATH } = await import("./send_otlp_span.cjs");
const { OTEL_METRICS_JSONL_PATH } = await import("./otlp_metrics.cjs");
const golden = JSON.parse(fs.readFileSync(path.join(__dirname, "fixtures/otel_legacy_contract.json"), "utf8"));
const BASE_MS = 1_700_000_000_000;
const BASE_ENV = [
  "GH_AW_AGENT_CONCLUSION",
  "GH_AW_INFO_WORKFLOW_NAME",
  "GH_AW_OTEL_METRICS",
  "GH_AW_OTLP_ENDPOINTS",
  "GITHUB_ACTOR",
  "GITHUB_EVENT_NAME",
  "GITHUB_JOB",
  "GITHUB_REF",
  "GITHUB_REF_NAME",
  "GITHUB_REPOSITORY",
  "GITHUB_RUN_ATTEMPT",
  "GITHUB_RUN_ID",
  "GITHUB_SHA",
  "GITHUB_WORKFLOW",
  "GITHUB_AW_OTEL_TRACE_ID",
  "GITHUB_AW_OTEL_PARENT_SPAN_ID",
  "INPUT_JOB_NAME",
  "OTEL_SEMCONV_STABILITY_OPT_IN",
  "OTEL_SERVICE_NAME",
];
const originalEnv = {};

function attributeValue(value) {
  if (Object.hasOwn(value, "stringValue")) return value.stringValue;
  if (Object.hasOwn(value, "intValue")) return value.intValue;
  if (Object.hasOwn(value, "doubleValue")) return value.doubleValue;
  if (Object.hasOwn(value, "boolValue")) return value.boolValue;
  if (value.arrayValue) return (value.arrayValue.values || []).map(attributeValue);
  if (value.kvlistValue) return Object.fromEntries((value.kvlistValue.values || []).map(item => [item.key, attributeValue(item.value)]));
  return value;
}

function legacyFacts(tracePayloads, metricsPayloads) {
  const facts = new Set();
  const spans = [];
  const add = fact => facts.add(JSON.stringify(fact));
  for (const payload of tracePayloads) {
    for (const resourceSpans of payload.resourceSpans || []) {
      for (const scopeSpans of resourceSpans.scopeSpans || []) {
        for (const span of scopeSpans.spans || []) spans.push({ resourceSpans, scopeSpans, span });
      }
    }
  }
  const namesBySpanId = new Map(spans.map(({ span }) => [span.spanId, span.name]));
  for (const { resourceSpans, scopeSpans, span } of spans) {
    for (const attr of resourceSpans.resource.attributes || []) add(["trace-resource", attr.key, attributeValue(attr.value)]);
    add(["trace-scope", scopeSpans.scope.name, scopeSpans.scope.version]);
    add(["span", span.name, "kind", span.kind]);
    add(["span", span.name, "status", span.status?.code]);
    if (span.status?.message) add(["span", span.name, "status-message", span.status.message]);
    if (span.parentSpanId) add(["span", span.name, "parent", namesBySpanId.get(span.parentSpanId) || "external"]);
    for (const attr of span.attributes || []) add(["span-attribute", span.name, attr.key, attributeValue(attr.value)]);
    for (const event of span.events || []) {
      add(["span-event", span.name, event.name]);
      for (const attr of event.attributes || []) add(["event-attribute", span.name, event.name, attr.key, attributeValue(attr.value)]);
    }
  }
  for (const payload of metricsPayloads) {
    for (const resourceMetrics of payload.resourceMetrics || []) {
      for (const attr of resourceMetrics.resource.attributes || []) add(["metric-resource", attr.key, attributeValue(attr.value)]);
      for (const scopeMetrics of resourceMetrics.scopeMetrics || []) {
        add(["metric-scope", scopeMetrics.scope.name, scopeMetrics.scope.version]);
        for (const metric of scopeMetrics.metrics || []) {
          const [type, body] = Object.entries(metric).find(([key]) => ["sum", "histogram", "gauge"].includes(key)) || ["unknown", {}];
          add(["metric", metric.name, type, metric.unit, metric.description || ""]);
          for (const point of body.dataPoints || []) {
            for (const attr of point.attributes || []) add(["metric-attribute", metric.name, attr.key, attributeValue(attr.value)]);
            if (type === "sum") add(["metric-value", metric.name, point.asInt ?? point.asDouble]);
          }
        }
      }
    }
  }
  return [...facts].sort();
}

function removeAdditiveFacts(facts) {
  const additiveResourceKeys = new Set([
    "cicd.pipeline.name",
    "cicd.pipeline.run.id",
    "cicd.pipeline.run.url.full",
    "cicd.pipeline.task.name",
    "vcs.provider.name",
    "vcs.repository.url.full",
    "vcs.ref.head.name",
    "vcs.ref.head.revision",
    "deployment.environment.name",
  ]);
  const additiveSpanKeys = new Set(["gen_ai.provider.name", "gen_ai.agent.name", "gen_ai.usage.cache_write.input_tokens", "error.type"]);
  return facts.filter(serialized => {
    const [kind, owner, key] = JSON.parse(serialized);
    if ((kind === "trace-resource" || kind === "metric-resource") && additiveResourceKeys.has(owner)) return false;
    if (kind === "span-attribute" && additiveSpanKeys.has(key)) return false;
    if (kind === "span-attribute" && key === "gh-aw.action_minutes") return false;
    if (["metric", "metric-attribute", "metric-value"].includes(kind) && (owner.startsWith("gen_ai.client.inference.usage.") || owner === "gen_ai.invoke_agent.duration")) return false;
    return true;
  });
}

describe("T-OT-020 default GenAI compatibility golden", () => {
  let tracePayloads;
  let metricPayloads;
  let originalReadFileSync;
  let originalStatSync;
  let originalAppendFileSync;
  let originalOpenSync;
  let originalFchmodSync;
  let originalWriteSync;
  let originalCloseSync;

  beforeEach(() => {
    for (const name of BASE_ENV) {
      originalEnv[name] = process.env[name];
      delete process.env[name];
    }
    Object.assign(process.env, {
      GH_AW_INFO_WORKFLOW_NAME: "OTel Legacy Golden",
      GH_AW_OTEL_METRICS: "true",
      GH_AW_OTLP_ENDPOINTS: "",
      GITHUB_ACTOR: "conformance",
      GITHUB_EVENT_NAME: "workflow_dispatch",
      GITHUB_JOB: "agent",
      GITHUB_REF: "refs/heads/main",
      GITHUB_REF_NAME: "main",
      GITHUB_REPOSITORY: "github/gh-aw",
      GITHUB_RUN_ATTEMPT: "1",
      GITHUB_RUN_ID: "123456789",
      GITHUB_SHA: "0123456789abcdef0123456789abcdef01234567",
      GITHUB_WORKFLOW: "OTel Legacy Golden",
      INPUT_JOB_NAME: "agent",
      OTEL_SERVICE_NAME: "gh-aw",
    });
    tracePayloads = [];
    metricPayloads = [];
    vi.spyOn(Date, "now").mockReturnValue(BASE_MS + 2000);

    originalReadFileSync = fs.readFileSync;
    originalStatSync = fs.statSync;
    originalAppendFileSync = fs.appendFileSync;
    vi.spyOn(fs, "readFileSync").mockImplementation((filePath, ...args) => {
      const fixture = {
        "/tmp/gh-aw/aw_info.json": { workflow_name: "OTel Legacy Golden", engine_id: "claude", model: "claude-3-5-sonnet", cli_version: "test" },
        "/tmp/gh-aw/agent_usage.json": { input_tokens: 100, output_tokens: 25, cache_read_tokens: 10, cache_write_tokens: 5, ai_credits: 0.125 },
        "/tmp/gh-aw/agent_output.json": { items: [], errors: [] },
        "/tmp/gh-aw/agent_cli_start_ms.txt": BASE_MS,
      };
      if (Object.hasOwn(fixture, String(filePath))) return JSON.stringify(fixture[String(filePath)]);
      return originalReadFileSync.call(fs, filePath, ...args);
    });
    vi.spyOn(fs, "statSync").mockImplementation((filePath, ...args) => {
      if (String(filePath) === "/tmp/gh-aw/agent_output.json") return { mtimeMs: BASE_MS + 1000 };
      return originalStatSync.call(fs, filePath, ...args);
    });
    vi.spyOn(fs, "appendFileSync").mockImplementation((filePath, content, ...args) => {
      if (filePath === OTEL_JSONL_PATH) tracePayloads.push(JSON.parse(String(content).trim()));
      else originalAppendFileSync.call(fs, filePath, content, ...args);
    });
    originalOpenSync = fs.openSync;
    originalFchmodSync = fs.fchmodSync;
    originalWriteSync = fs.writeSync;
    originalCloseSync = fs.closeSync;
    vi.spyOn(fs, "openSync").mockImplementation((filePath, ...args) => (filePath === OTEL_METRICS_JSONL_PATH ? 12345 : originalOpenSync.call(fs, filePath, ...args)));
    vi.spyOn(fs, "fchmodSync").mockImplementation((fd, ...args) => (fd === 12345 ? undefined : originalFchmodSync.call(fs, fd, ...args)));
    vi.spyOn(fs, "writeSync").mockImplementation((fd, content, ...args) => {
      if (fd === 12345) {
        metricPayloads.push(JSON.parse(Buffer.isBuffer(content) ? content.toString("utf8") : String(content)));
        return Buffer.byteLength(Buffer.isBuffer(content) ? content : String(content));
      }
      return originalWriteSync.call(fs, fd, content, ...args);
    });
    vi.spyOn(fs, "closeSync").mockImplementation((fd, ...args) => (fd === 12345 ? undefined : originalCloseSync.call(fs, fd, ...args)));
  });

  afterEach(() => {
    vi.restoreAllMocks();
    for (const name of BASE_ENV) {
      if (originalEnv[name] === undefined) delete process.env[name];
      else process.env[name] = originalEnv[name];
    }
  });

  it.each([
    { conclusion: "success", optIn: undefined },
    { conclusion: "failure", optIn: undefined },
    { conclusion: "success", optIn: "not_genai" },
    { conclusion: "failure", optIn: "not_genai" },
  ])("retains every pre-change fact for $conclusion with opt-in $optIn", async ({ conclusion, optIn }) => {
    if (optIn) process.env.OTEL_SEMCONV_STABILITY_OPT_IN = optIn;
    process.env.GH_AW_AGENT_CONCLUSION = conclusion;
    const setup = await sendJobSetupSpan({ traceId: "0123456789abcdef0123456789abcdef", startMs: BASE_MS });
    process.env.GITHUB_AW_OTEL_TRACE_ID = setup.traceId;
    process.env.GITHUB_AW_OTEL_PARENT_SPAN_ID = setup.spanId;
    await sendJobConclusionSpan("gh-aw.agent.conclusion", { startMs: BASE_MS });

    const facts = removeAdditiveFacts(legacyFacts(tracePayloads, metricPayloads));
    const digest = crypto.createHash("sha256").update(facts.join("\n")).digest("hex");
    expect(facts).toHaveLength(golden[conclusion].facts);
    expect(digest).toBe(golden[conclusion].sha256);
  });
});
