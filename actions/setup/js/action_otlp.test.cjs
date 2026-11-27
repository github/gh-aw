import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "fs";
import http from "http";
import os from "os";
import path from "path";
import { execFileSync, spawn } from "child_process";
import { createRequire } from "module";
import { fileURLToPath } from "url";

// ---------------------------------------------------------------------------
// Module imports
// ---------------------------------------------------------------------------

const req = createRequire(import.meta.url);
const { run: runSetup } = await import("./action_setup_otlp.cjs");
const { run: runConclusion } = await import("./action_conclusion_otlp.cjs");
const { main: runPushRoutingHistory } = req("./push_routing_history.cjs");

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const __dirname = path.dirname(fileURLToPath(import.meta.url));

// ---------------------------------------------------------------------------
// action_setup_otlp — run()
// ---------------------------------------------------------------------------

describe("action_setup_otlp run()", () => {
  let originalEnv;

  beforeEach(() => {
    originalEnv = { ...process.env };
    // Clear any OTLP endpoint so send_otlp_span.cjs is a no-op
    delete process.env.GH_AW_OTLP_ENDPOINTS;
    delete process.env.GITHUB_OUTPUT;
    delete process.env.GITHUB_ENV;
    delete process.env.SETUP_START_MS;
    delete process.env.INPUT_TRACE_ID;
    delete process.env.INPUT_PARENT_SPAN_ID;
  });

  afterEach(() => {
    process.env = originalEnv;
  });

  it("resolves without throwing when OTLP endpoint is not configured", async () => {
    await expect(runSetup()).resolves.toBeUndefined();
  });

  it("writes trace-id to GITHUB_OUTPUT even when endpoint is not configured", async () => {
    const tmpOut = path.join(path.dirname(__dirname), `action_setup_otlp_test_no_endpoint_${Date.now()}.txt`);
    try {
      // No OTEL endpoint — span must NOT be sent but trace-id must still be written.
      process.env.GITHUB_OUTPUT = tmpOut;
      process.env.GITHUB_ENV = tmpOut;

      await runSetup();

      const contents = fs.readFileSync(tmpOut, "utf8");
      expect(contents).toMatch(/^trace-id=[0-9a-f]{32}$/m);
      expect(contents).toMatch(/^span-id=[0-9a-f]{16}$/m);
      expect(contents).toMatch(/^GITHUB_AW_OTEL_TRACE_ID=[0-9a-f]{32}$/m);
    } finally {
      fs.rmSync(tmpOut, { force: true });
    }
  });

  it("writes GITHUB_AW_OTEL_JOB_START_MS to GITHUB_ENV after setup span resolves", async () => {
    const tmpOut = path.join(path.dirname(__dirname), `action_setup_otlp_test_job_start_ms_${Date.now()}.txt`);
    try {
      process.env.GITHUB_OUTPUT = tmpOut;
      process.env.GITHUB_ENV = tmpOut;

      const beforeMs = Date.now();
      await runSetup();
      const afterMs = Date.now();

      const contents = fs.readFileSync(tmpOut, "utf8");
      const match = contents.match(/^GITHUB_AW_OTEL_JOB_START_MS=(\d+)$/m);
      expect(match).not.toBeNull();
      const writtenMs = parseInt(match[1], 10);
      expect(writtenMs).toBeGreaterThanOrEqual(beforeMs);
      expect(writtenMs).toBeLessThanOrEqual(afterMs);
    } finally {
      fs.rmSync(tmpOut, { force: true });
    }
  });

  it("uses INPUT_TRACE_ID as trace ID when provided", async () => {
    const inputTraceId = "a".repeat(32);
    const tmpOut = path.join(path.dirname(__dirname), `action_setup_otlp_test_input_tid_${Date.now()}.txt`);
    try {
      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "http://localhost:14317" }]);
      process.env.INPUT_TRACE_ID = inputTraceId;
      process.env.GITHUB_OUTPUT = tmpOut;
      process.env.GITHUB_ENV = tmpOut;

      const fetchSpy = vi.spyOn(global, "fetch").mockResolvedValue(new Response(null, { status: 200 }));

      await runSetup();

      const contents = fs.readFileSync(tmpOut, "utf8");
      expect(contents).toContain(`trace-id=${inputTraceId}`);
      expect(contents).toContain(`GITHUB_AW_OTEL_TRACE_ID=${inputTraceId}`);

      fetchSpy.mockRestore();
    } finally {
      fs.rmSync(tmpOut, { force: true });
    }
  });

  it("writes trace-id to GITHUB_OUTPUT when endpoint is configured", async () => {
    const tmpOut = path.join(path.dirname(__dirname), `action_setup_otlp_test_output_${Date.now()}.txt`);
    try {
      // Provide a fake endpoint (fetch will fail gracefully)
      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "http://localhost:14317" }]);
      process.env.SETUP_START_MS = String(Date.now() - 1000);
      process.env.GITHUB_OUTPUT = tmpOut;
      process.env.GITHUB_ENV = tmpOut;

      // Mock fetch so no real network call is made
      const fetchSpy = vi.spyOn(global, "fetch").mockResolvedValue(new Response(null, { status: 200 }));

      await runSetup();

      const contents = fs.readFileSync(tmpOut, "utf8");
      expect(contents).toMatch(/^trace-id=[0-9a-f]{32}$/m);
      expect(contents).toMatch(/^span-id=[0-9a-f]{16}$/m);
      expect(contents).toMatch(/^GITHUB_AW_OTEL_TRACE_ID=[0-9a-f]{32}$/m);
      expect(contents).toMatch(/^GITHUB_AW_OTEL_PARENT_SPAN_ID=[0-9a-f]{16}$/m);

      fetchSpy.mockRestore();
    } finally {
      fs.rmSync(tmpOut, { force: true });
    }
  });

  it("generates a new trace-id when INPUT_TRACE_ID is absent", async () => {
    const tmpOut = path.join(path.dirname(__dirname), `action_setup_otlp_test_no_input_tid_${Date.now()}.txt`);
    try {
      // INPUT_TRACE_ID is not set — a fresh trace ID must be generated.
      process.env.GITHUB_OUTPUT = tmpOut;
      process.env.GITHUB_ENV = tmpOut;

      await runSetup();

      const contents = fs.readFileSync(tmpOut, "utf8");
      // A generated 32-char hex trace-id must always be written.
      expect(contents).toMatch(/^trace-id=[0-9a-f]{32}$/m);
      expect(contents).toMatch(/^span-id=[0-9a-f]{16}$/m);
      expect(contents).toMatch(/^GITHUB_AW_OTEL_TRACE_ID=[0-9a-f]{32}$/m);
    } finally {
      fs.rmSync(tmpOut, { force: true });
    }
  });

  it("does not throw when GITHUB_OUTPUT is not set", async () => {
    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "http://localhost:14317" }]);
    const fetchSpy = vi.spyOn(global, "fetch").mockResolvedValue(new Response(null, { status: 200 }));
    await expect(runSetup()).resolves.toBeUndefined();
    fetchSpy.mockRestore();
  });

  it("uses INPUT_PARENT_SPAN_ID as setup span parent when provided", async () => {
    const tmpOut = path.join(path.dirname(__dirname), `action_setup_otlp_test_parent_span_${Date.now()}.txt`);
    const parentSpanId = "abcdef1234567890";
    try {
      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "http://localhost:14317" }]);
      process.env.INPUT_PARENT_SPAN_ID = parentSpanId;
      process.env.GITHUB_OUTPUT = tmpOut;
      process.env.GITHUB_ENV = tmpOut;

      let capturedBody;
      const fetchSpy = vi.spyOn(global, "fetch").mockImplementation((_url, opts) => {
        capturedBody = opts?.body;
        return Promise.resolve(new Response(null, { status: 200 }));
      });

      await runSetup();

      const payload = JSON.parse(capturedBody);
      const span = payload?.resourceSpans?.[0]?.scopeSpans?.[0]?.spans?.[0];
      expect(span?.parentSpanId).toBe(parentSpanId);
      fetchSpy.mockRestore();
    } finally {
      fs.rmSync(tmpOut, { force: true });
    }
  });

  it("uses job name from INPUT_JOB-NAME (hyphen form) in setup span when INPUT_JOB_NAME is not set", async () => {
    const tmpOut = path.join(path.dirname(__dirname), `action_setup_otlp_test_job_name_hyphen_${Date.now()}.txt`);
    try {
      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "http://localhost:14317" }]);
      process.env["INPUT_JOB-NAME"] = "agent";
      delete process.env.INPUT_JOB_NAME;
      process.env.GITHUB_OUTPUT = tmpOut;
      process.env.GITHUB_ENV = tmpOut;

      let capturedBody;
      const fetchSpy = vi.spyOn(global, "fetch").mockImplementation((_url, opts) => {
        capturedBody = opts?.body;
        return Promise.resolve(new Response(null, { status: 200 }));
      });

      await runSetup();

      const payload = JSON.parse(capturedBody);
      const spanName = payload?.resourceSpans?.[0]?.scopeSpans?.[0]?.spans?.[0]?.name;
      expect(spanName).toBe("gh-aw.agent.setup");
      fetchSpy.mockRestore();
    } finally {
      fs.rmSync(tmpOut, { force: true });
    }
  });

  it("includes github.repository, github.run_id resource attributes in setup span", async () => {
    const tmpOut = path.join(path.dirname(__dirname), `action_setup_otlp_test_resource_attrs_${Date.now()}.txt`);
    try {
      process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "http://localhost:14317" }]);
      process.env.GITHUB_REPOSITORY = "owner/repo";
      process.env.GITHUB_RUN_ID = "111222333";
      process.env.GITHUB_EVENT_NAME = "workflow_dispatch";
      process.env.GITHUB_OUTPUT = tmpOut;
      process.env.GITHUB_ENV = tmpOut;

      let capturedBody;
      const fetchSpy = vi.spyOn(global, "fetch").mockImplementation((_url, opts) => {
        capturedBody = opts?.body;
        return Promise.resolve(new Response(null, { status: 200 }));
      });

      await runSetup();

      const payload = JSON.parse(capturedBody);
      const resourceAttrs = payload?.resourceSpans?.[0]?.resource?.attributes ?? [];
      expect(resourceAttrs).toContainEqual({ key: "github.repository", value: { stringValue: "owner/repo" } });
      expect(resourceAttrs).toContainEqual({ key: "github.run_id", value: { stringValue: "111222333" } });
      expect(resourceAttrs).toContainEqual({ key: "github.event_name", value: { stringValue: "workflow_dispatch" } });

      fetchSpy.mockRestore();
    } finally {
      fs.rmSync(tmpOut, { force: true });
      delete process.env.GITHUB_REPOSITORY;
      delete process.env.GITHUB_RUN_ID;
      delete process.env.GITHUB_EVENT_NAME;
    }
  });
});

// ---------------------------------------------------------------------------
// action_conclusion_otlp — run()
// ---------------------------------------------------------------------------

describe("action_conclusion_otlp run()", () => {
  let originalEnv;

  beforeEach(() => {
    originalEnv = { ...process.env };
    delete process.env.GH_AW_OTLP_ENDPOINTS;
    delete process.env.INPUT_JOB_NAME;
  });

  afterEach(() => {
    process.env = originalEnv;
  });

  it("resolves without throwing when OTLP endpoint is not configured", async () => {
    await expect(runConclusion()).resolves.toBeUndefined();
  });

  it("resolves without throwing when endpoint is configured", async () => {
    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "http://localhost:14317" }]);
    const fetchSpy = vi.spyOn(global, "fetch").mockResolvedValue(new Response(null, { status: 200 }));
    await expect(runConclusion()).resolves.toBeUndefined();
    fetchSpy.mockRestore();
  });

  it("uses job name from INPUT_JOB_NAME in span name", async () => {
    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "http://localhost:14317" }]);
    process.env.INPUT_JOB_NAME = "agent";
    let capturedBody;
    const fetchSpy = vi.spyOn(global, "fetch").mockImplementation((_url, opts) => {
      capturedBody = opts?.body;
      return Promise.resolve(new Response(null, { status: 200 }));
    });

    await runConclusion();

    const payload = JSON.parse(capturedBody);
    const spanName = payload?.resourceSpans?.[0]?.scopeSpans?.[0]?.spans?.[0]?.name;
    expect(spanName).toBe("gh-aw.agent.conclusion");
    fetchSpy.mockRestore();
  });

  it("uses default span name when INPUT_JOB_NAME is not set", async () => {
    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "http://localhost:14317" }]);
    let capturedBody;
    const fetchSpy = vi.spyOn(global, "fetch").mockImplementation((_url, opts) => {
      capturedBody = opts?.body;
      return Promise.resolve(new Response(null, { status: 200 }));
    });

    await runConclusion();

    const payload = JSON.parse(capturedBody);
    const spanName = payload?.resourceSpans?.[0]?.scopeSpans?.[0]?.spans?.[0]?.name;
    expect(spanName).toBe("gh-aw.job.conclusion");
    fetchSpy.mockRestore();
  });

  it("records agent failure conclusion as STATUS_CODE_ERROR when GH_AW_AGENT_CONCLUSION is 'failure'", async () => {
    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "http://localhost:14317" }]);
    process.env.GH_AW_AGENT_CONCLUSION = "failure";
    let capturedBody;
    const fetchSpy = vi.spyOn(global, "fetch").mockImplementation((_url, opts) => {
      capturedBody = opts?.body;
      return Promise.resolve(new Response(null, { status: 200 }));
    });

    await runConclusion();

    const payload = JSON.parse(capturedBody);
    const span = payload?.resourceSpans?.[0]?.scopeSpans?.[0]?.spans?.[0];
    expect(span?.status?.code).toBe(2); // STATUS_CODE_ERROR
    expect(span?.status?.message).toBe("agent failure");
    const conclusionAttr = span?.attributes?.find(a => a.key === "gh-aw.agent.conclusion");
    expect(conclusionAttr?.value?.stringValue).toBe("failure");
    fetchSpy.mockRestore();
    delete process.env.GH_AW_AGENT_CONCLUSION;
  });

  it("records timed_out conclusion as STATUS_CODE_ERROR when GH_AW_AGENT_CONCLUSION is 'timed_out'", async () => {
    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "http://localhost:14317" }]);
    process.env.GH_AW_AGENT_CONCLUSION = "timed_out";
    let capturedBody;
    const fetchSpy = vi.spyOn(global, "fetch").mockImplementation((_url, opts) => {
      capturedBody = opts?.body;
      return Promise.resolve(new Response(null, { status: 200 }));
    });

    await runConclusion();

    const payload = JSON.parse(capturedBody);
    const span = payload?.resourceSpans?.[0]?.scopeSpans?.[0]?.spans?.[0];
    expect(span?.status?.code).toBe(2); // STATUS_CODE_ERROR
    expect(span?.status?.message).toBe("agent timed_out");
    const conclusionAttr = span?.attributes?.find(a => a.key === "gh-aw.agent.conclusion");
    expect(conclusionAttr?.value?.stringValue).toBe("timed_out");
    fetchSpy.mockRestore();
    delete process.env.GH_AW_AGENT_CONCLUSION;
  });

  it("records success conclusion as STATUS_CODE_OK when GH_AW_AGENT_CONCLUSION is 'success'", async () => {
    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "http://localhost:14317" }]);
    process.env.GH_AW_AGENT_CONCLUSION = "success";
    let capturedBody;
    const fetchSpy = vi.spyOn(global, "fetch").mockImplementation((_url, opts) => {
      capturedBody = opts?.body;
      return Promise.resolve(new Response(null, { status: 200 }));
    });

    await runConclusion();

    const payload = JSON.parse(capturedBody);
    const span = payload?.resourceSpans?.[0]?.scopeSpans?.[0]?.spans?.[0];
    expect(span?.status?.code).toBe(1); // STATUS_CODE_OK
    expect(span?.status?.message).toBeUndefined();
    const conclusionAttr = span?.attributes?.find(a => a.key === "gh-aw.agent.conclusion");
    expect(conclusionAttr?.value?.stringValue).toBe("success");
    fetchSpy.mockRestore();
    delete process.env.GH_AW_AGENT_CONCLUSION;
  });

  it("records cancelled conclusion as STATUS_CODE_ERROR when GH_AW_AGENT_CONCLUSION is 'cancelled'", async () => {
    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "http://localhost:14317" }]);
    process.env.GH_AW_AGENT_CONCLUSION = "cancelled";
    let capturedBody;
    const fetchSpy = vi.spyOn(global, "fetch").mockImplementation((_url, opts) => {
      capturedBody = opts?.body;
      return Promise.resolve(new Response(null, { status: 200 }));
    });

    await runConclusion();

    const payload = JSON.parse(capturedBody);
    const span = payload?.resourceSpans?.[0]?.scopeSpans?.[0]?.spans?.[0];
    expect(span?.status?.code).toBe(2); // STATUS_CODE_ERROR
    expect(span?.status?.message).toBe("agent cancelled");
    const conclusionAttr = span?.attributes?.find(a => a.key === "gh-aw.agent.conclusion");
    expect(conclusionAttr?.value?.stringValue).toBe("cancelled");
    fetchSpy.mockRestore();
    delete process.env.GH_AW_AGENT_CONCLUSION;
  });

  it("omits gh-aw.agent.conclusion attribute when GH_AW_AGENT_CONCLUSION is not set", async () => {
    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "http://localhost:14317" }]);
    delete process.env.GH_AW_AGENT_CONCLUSION;
    let capturedBody;
    const fetchSpy = vi.spyOn(global, "fetch").mockImplementation((_url, opts) => {
      capturedBody = opts?.body;
      return Promise.resolve(new Response(null, { status: 200 }));
    });

    await runConclusion();

    const payload = JSON.parse(capturedBody);
    const span = payload?.resourceSpans?.[0]?.scopeSpans?.[0]?.spans?.[0];
    expect(span?.status?.code).toBe(1); // STATUS_CODE_OK
    const conclusionAttr = span?.attributes?.find(a => a.key === "gh-aw.agent.conclusion");
    expect(conclusionAttr).toBeUndefined();
    fetchSpy.mockRestore();
  });

  it("uses GITHUB_AW_OTEL_JOB_START_MS as span start time when set", async () => {
    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "http://localhost:14317" }]);
    const jobStartMs = Date.now() - 120_000; // 2 minutes ago
    process.env.GITHUB_AW_OTEL_JOB_START_MS = String(jobStartMs);
    let capturedBody;
    const fetchSpy = vi.spyOn(global, "fetch").mockImplementation((_url, opts) => {
      capturedBody = opts?.body;
      return Promise.resolve(new Response(null, { status: 200 }));
    });

    await runConclusion();

    const payload = JSON.parse(capturedBody);
    const span = payload?.resourceSpans?.[0]?.scopeSpans?.[0]?.spans?.[0];
    // startTimeUnixNano = jobStartMs * 1_000_000
    const expectedStartNano = (BigInt(jobStartMs) * 1_000_000n).toString();
    expect(span?.startTimeUnixNano).toBe(expectedStartNano);
    // endTimeUnixNano must be after the start
    expect(BigInt(span?.endTimeUnixNano)).toBeGreaterThan(BigInt(expectedStartNano));
    fetchSpy.mockRestore();
    delete process.env.GITHUB_AW_OTEL_JOB_START_MS;
  });

  it("falls back to current time as span start when GITHUB_AW_OTEL_JOB_START_MS is not set", async () => {
    process.env.GH_AW_OTLP_ENDPOINTS = JSON.stringify([{ url: "http://localhost:14317" }]);
    delete process.env.GITHUB_AW_OTEL_JOB_START_MS;
    const beforeMs = Date.now();
    let capturedBody;
    const fetchSpy = vi.spyOn(global, "fetch").mockImplementation((_url, opts) => {
      capturedBody = opts?.body;
      return Promise.resolve(new Response(null, { status: 200 }));
    });

    await runConclusion();

    const afterMs = Date.now();
    const payload = JSON.parse(capturedBody);
    const span = payload?.resourceSpans?.[0]?.scopeSpans?.[0]?.spans?.[0];
    const startNano = BigInt(span?.startTimeUnixNano);
    expect(startNano).toBeGreaterThanOrEqual(BigInt(beforeMs) * 1_000_000n);
    expect(startNano).toBeLessThanOrEqual(BigInt(afterMs) * 1_000_000n);
    fetchSpy.mockRestore();
  });
});

// ---------------------------------------------------------------------------
// push_routing_history failure reporting across action mode and script mode
// ---------------------------------------------------------------------------

function parseGitHubEnvFile(filePath) {
  const vars = {};
  if (!fs.existsSync(filePath)) return vars;
  for (const line of fs.readFileSync(filePath, "utf8").split(/\r?\n/)) {
    const idx = line.indexOf("=");
    if (idx > 0) vars[line.slice(0, idx)] = line.slice(idx + 1);
  }
  return vars;
}

function runCommandAsync(command, args, options) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, { ...options, stdio: ["ignore", "pipe", "pipe"] });
    let stdout = "";
    let stderr = "";
    child.stdout.on("data", chunk => {
      stdout += chunk.toString("utf8");
    });
    child.stderr.on("data", chunk => {
      stderr += chunk.toString("utf8");
    });
    child.on("error", reject);
    child.on("close", status => resolve({ status, stdout, stderr }));
  });
}

async function startLocalOTLPCollector() {
  const traces = [];
  const metrics = [];
  const server = http.createServer((reqMessage, res) => {
    const chunks = [];
    reqMessage.on("data", chunk => chunks.push(chunk));
    reqMessage.on("end", () => {
      const body = JSON.parse(Buffer.concat(chunks).toString("utf8"));
      if (reqMessage.url?.includes("/v1/traces")) traces.push(body);
      if (reqMessage.url?.includes("/v1/metrics")) metrics.push(body);
      res.writeHead(200, { "Content-Type": "application/json", Connection: "close" });
      res.end("{}");
    });
  });
  await new Promise(resolve => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  const port = typeof address === "object" && address ? address.port : 0;
  return {
    endpoint: `http://127.0.0.1:${port}`,
    traces,
    metrics,
    close: () =>
      new Promise(resolve => {
        server.closeAllConnections?.();
        server.close(resolve);
      }),
  };
}

async function simulatePushRoutingHistoryFailure(root, failureKind) {
  if (failureKind === "artifact-download") {
    // Download usage artifact fails with continue-on-error omitted, so the push step is skipped.
    return "failure";
  }

  // Trigger a real push failure in push_routing_history.cjs after a valid usage session is read.
  const remote = path.join(root, "remote.git");
  execFileSync("git", ["init", "--quiet", "--bare", remote]);
  execFileSync("git", ["config", "user.name", "remote"], { cwd: remote });
  execFileSync("git", ["config", "user.email", "remote@example.com"], { cwd: remote });
  const initialCommit = execFileSync("git", ["commit-tree", "4b825dc642cb6eb9a060e54bf8d69288fbee4904", "-m", "Initialize routing history"], { cwd: remote, encoding: "utf8" }).trim();
  execFileSync("git", ["update-ref", "refs/heads/memory/adaptive-routing/triage", initialCommit], { cwd: remote });
  const sessionPath = path.join(root, "aw_session.jsonl");
  const events = [
    { type: "routing.observation", data: { outcome: "success" }, provenance: { component: "collector", phase: "conclusion" } },
    { type: "firewall.model_routing", data: { stage: "selection", selectedProvider: "copilot", wireModel: "model-a", selectedEffort: "high" }, provenance: { component: "firewall", phase: "agent" } },
    { type: "firewall.model_routing", data: { stage: "request", requestId: "r1", routed: "as_selected", requestedModel: "model-a" }, provenance: { component: "firewall", phase: "agent" } },
    {
      type: "firewall.token_usage",
      data: { requestId: "r1", provider: "copilot", aic: 1, usage: { inputTokens: 100, cacheReadInputTokens: 0, cacheCreationInputTokens: 0, outputTokens: 20 } },
      provenance: { component: "firewall", phase: "agent" },
    },
  ];
  fs.writeFileSync(sessionPath, events.map(event => `${JSON.stringify(event)}\n`).join(""));

  const pushError = "ERR_API: failed to push memory/adaptive-routing/triage";
  const pushSignedCommitsFn = vi.fn(async () => {
    throw new Error(pushError);
  });
  const previousCore = global.core;
  const setFailed = vi.fn();
  global.core = { info: vi.fn(), warning: vi.fn(), debug: vi.fn(), error: vi.fn(), setFailed, summary: { addRaw: vi.fn().mockReturnThis(), write: vi.fn() } };
  try {
    const github = {
      rest: {
        repos: {
          get: async () => ({ data: { default_branch: "main" } }),
        },
        actions: {
          getWorkflowRun: async () => ({ data: { workflow_id: 42 } }),
          listWorkflowRuns: async () => ({
            data: { total_count: 1, workflow_runs: [{ id: 9001, run_number: 1, run_attempt: 1, status: "in_progress", conclusion: null, head_branch: "main", event: "workflow_dispatch", created_at: "2026-11-27T00:00:00Z" }] },
          }),
          listJobsForWorkflowRunAttempt: async () => ({ data: { jobs: [] } }),
        },
      },
    };
    let thrownMessage = "";
    try {
      await runPushRoutingHistory({
        github,
        context: { repo: { owner: "octo", repo: "demo" }, runId: 9001, runNumber: 1, payload: { repository: { default_branch: "main" } } },
        env: {
          GH_AW_ROUTING_WORKFLOW: "triage",
          GH_AW_ROUTING_GENERATION: `${"a".repeat(64)}:${"b".repeat(64)}`,
          GH_AW_ROUTING_MODE: "observe",
          GH_AW_ROUTING_ENGINE: "copilot",
          GH_AW_ROUTING_GOAL: "cost",
          GH_AW_ROUTING_LEVEL: "",
          GH_AW_ROUTING_SESSION_FILE: sessionPath,
          GITHUB_RUN_ATTEMPT: "1",
          GITHUB_WORKFLOW_REF: "octo/demo/.github/workflows/triage.lock.yml@refs/heads/main",
          GH_TOKEN: "test-token",
          GITHUB_SERVER_URL: "https://github.com",
        },
        workspaceDir: fs.mkdtempSync(path.join(root, "writer-")),
        remoteUrl: remote,
        pushSignedCommitsFn,
        execGetExecOutput: async (command, args, options) => ({
          stdout: execFileSync(command, args, { cwd: options.cwd, encoding: "utf8" }),
          stderr: "",
          exitCode: 0,
        }),
        sleepFn: async () => {},
      });
    } catch (err) {
      thrownMessage = err instanceof Error ? err.message : String(err);
    }
    expect(pushSignedCommitsFn).toHaveBeenCalled();
    const reportedFailure = thrownMessage || setFailed.mock.calls.map(([msg]) => String(msg)).join("\n");
    expect(reportedFailure).toContain(pushError);
    return "failure";
  } finally {
    global.core = previousCore;
  }
}

describe("push_routing_history OTLP failure reporting", () => {
  const cases = [
    ["action", "artifact-download", false],
    ["action", "artifact-download", true],
    ["action", "history-push", false],
    ["action", "history-push", true],
    ["script", "artifact-download", false],
    ["script", "artifact-download", true],
    ["script", "history-push", false],
    ["script", "history-push", true],
  ];

  it.each(cases)("emits one ERROR conclusion span in %s mode on %s (metrics=%s)", async (mode, failureKind, metricsEnabled) => {
    const collector = await startLocalOTLPCollector();
    const root = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-push-routing-otlp-"));
    try {
      const githubEnv = path.join(root, "github_env");
      const githubOutput = path.join(root, "github_output");
      const ghAwTmp = path.join(root, "gh-aw-tmp");
      fs.mkdirSync(ghAwTmp, { recursive: true });

      const baseEnv = {
        ...process.env,
        RUNNER_TEMP: root,
        GH_AW_TMP_DIR: ghAwTmp,
        GH_AW_OTLP_ENDPOINTS: JSON.stringify([{ url: collector.endpoint }]),
        GH_AW_OTEL_METRICS: metricsEnabled ? "true" : "false",
        INPUT_JOB_NAME: "push_routing_history",
        GITHUB_JOB: "push_routing_history",
        GITHUB_REPOSITORY: "octo/demo",
        GITHUB_RUN_ID: "9001",
        GITHUB_WORKFLOW: "Triage",
        GH_AW_INFO_ENGINE_ID: "copilot",
        GITHUB_ENV: githubEnv,
        GITHUB_OUTPUT: githubOutput,
      };
      delete baseEnv.RUNNER_ENVIRONMENT;
      delete baseEnv.GH_AW_AGENT_CONCLUSION;
      delete baseEnv.GH_AW_JOB_RESULT;
      delete baseEnv.GH_AW_OTEL_CONCLUSION_PREUPLOAD;
      delete baseEnv.OTEL_SEMCONV_STABILITY_OPT_IN;
      delete baseEnv.OTEL_EXPORTER_OTLP_PROTOCOL;

      const setupResult = await runCommandAsync(process.execPath, [path.join(__dirname, "action_setup_otlp.cjs")], {
        cwd: root,
        env: { ...baseEnv, SETUP_START_MS: String(Date.now() - 100) },
      });
      expect(setupResult.status, setupResult.stderr).toBe(0);

      const jobResult = await simulatePushRoutingHistoryFailure(root, failureKind);

      if (mode === "action") {
        // Mirror the "Record job result for OTLP" step writing GH_AW_JOB_RESULT to GITHUB_ENV
        // before GitHub Actions runs actions/setup/post.js with that inherited environment.
        fs.appendFileSync(githubEnv, `GH_AW_JOB_RESULT=${jobResult}\n`);
        const postEnv = { ...baseEnv, ...parseGitHubEnvFile(githubEnv) };
        const postResult = await runCommandAsync(process.execPath, [path.resolve(__dirname, "../post.js")], {
          cwd: root,
          env: postEnv,
        });
        expect(postResult.status, postResult.stderr).toBe(0);
      } else {
        // Mirror the script-mode "Clean Scripts" step running clean.sh with GH_AW_JOB_RESULT.
        const cleanEnv = {
          ...baseEnv,
          ...parseGitHubEnvFile(githubEnv),
          INPUT_DESTINATION: __dirname,
          GH_AW_NODE: process.execPath,
          GH_AW_JOB_RESULT: jobResult,
        };
        const cleanResult = await runCommandAsync("bash", [path.resolve(__dirname, "../clean.sh")], {
          cwd: root,
          env: cleanEnv,
        });
        expect(cleanResult.status, cleanResult.stderr).toBe(0);
      }

      const spans = collector.traces.flatMap(payload => payload.resourceSpans[0].scopeSpans[0].spans);
      const setupSpans = spans.filter(span => span.name === "gh-aw.push_routing_history.setup");
      const conclusionSpans = spans.filter(span => span.name === "gh-aw.push_routing_history.conclusion");
      expect(setupSpans).toHaveLength(1);
      expect(conclusionSpans).toHaveLength(1);

      const conclusionSpan = conclusionSpans[0];
      expect(conclusionSpan.traceId).toBe(setupSpans[0].traceId);
      expect(conclusionSpan.parentSpanId).toBe(setupSpans[0].spanId);
      expect(conclusionSpan.status.code).toBe(2);
      expect(conclusionSpan.status.message).toBe("push_routing_history failure");
      const attrs = Object.fromEntries(conclusionSpan.attributes.map(a => [a.key, a.value.stringValue ?? a.value.intValue]));
      expect(attrs["gh-aw.job.name"]).toBe("push_routing_history");
      expect(attrs["gh-aw.run.status"]).toBe("success");

      if (metricsEnabled) {
        expect(collector.metrics).toHaveLength(1);
        const emittedMetrics = collector.metrics[0].resourceMetrics[0].scopeMetrics[0].metrics;
        const jobErrors = emittedMetrics.filter(metric => metric.name === "gh_aw.job.errors");
        expect(jobErrors).toHaveLength(1);
        const errorPoint = jobErrors[0].sum.dataPoints[0];
        expect(errorPoint.asInt).toBe("1");
        expect(errorPoint.attributes).toContainEqual({ key: "gh-aw.job.name", value: { stringValue: "push_routing_history" } });
        expect(errorPoint.attributes).toContainEqual({ key: "cicd.pipeline.task.run.result", value: { stringValue: "failure" } });
        expect(emittedMetrics.some(metric => metric.name === "gh_aw.run.errors")).toBe(false);
      } else {
        expect(collector.metrics).toHaveLength(0);
      }
    } finally {
      await collector.close();
      fs.rmSync(root, { recursive: true, force: true });
    }
  });
});
