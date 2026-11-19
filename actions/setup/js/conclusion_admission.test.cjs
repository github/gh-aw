import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { sendConclusion, main, readUsageArtifactIds, MAX_REQUEST_BYTES, FAILURE } = require("./conclusion_admission.cjs");
const base = {
  GH_AW_DEFAULT_ADMISSION_URL: "https://admission.example.org/api/admission",
  ACTIONS_ID_TOKEN_REQUEST_URL: "https://oidc.example.test/token?job=synthetic",
  ACTIONS_ID_TOKEN_REQUEST_TOKEN: "synthetic-bootstrap",
  GITHUB_REPOSITORY: "acme/project",
  GH_AW_INFO_WORKFLOW_ID: "platform/ops/.github/workflows/triage.lock.yml@refs/heads/main",
  GH_AW_ADMISSION_WORKFLOW_HASH: `${"a".repeat(64)}:${"b".repeat(64)}`,
  GITHUB_RUN_ID: "90071992547409931234567890",
  GITHUB_RUN_ATTEMPT: "2",
  GITHUB_REF: "refs/heads/main",
  GITHUB_EVENT_NAME: "workflow_dispatch",
  GITHUB_ACTOR: "original",
  GITHUB_TRIGGERING_ACTOR: "rerunner",
  GH_AW_USAGE_ARTIFACT_ID: "12345",
};
const jwt = () => `synthetic.${Buffer.from(JSON.stringify({ exp: Math.floor(Date.now() / 1000) + 300 })).toString("base64url")}.signature`;
const oidc = () => Response.json({ value: jwt() });
const accepted = () => new Response(null, { status: 202 });
const compactSession = '{"type":"session.format","data":{"version":1}}\n{"type":"usage.report","data":{"aic":2,"usage":{"inputTokens":10,"outputTokens":3}}}\n';
const session = compactSession + '{"type":"assistant.message","data":{"content":"done\\nwith Unicode: 🦊"}}\n';

describe("admission conclusion notification", () => {
  let directory;
  let sessionFile;
  let options;
  beforeEach(() => {
    vi.spyOn(fs, "appendFileSync").mockImplementation(() => {});
    vi.useFakeTimers();
    vi.stubEnv("GH_AW_OTLP_ENDPOINTS", "");
    directory = fs.mkdtempSync(path.join(os.tmpdir(), "admission-conclusion-"));
    sessionFile = path.join(directory, "aw_session.jsonl");
    fs.writeFileSync(sessionFile, session);
    options = { env: { ...base }, sessionFile, now: () => Date.now() };
  });
  afterEach(() => {
    fs.rmSync(directory, { recursive: true, force: true });
    vi.useRealTimers();
    vi.restoreAllMocks();
    vi.unstubAllEnvs();
    vi.unstubAllGlobals();
  });

  it("skips before reading a session or acquiring credentials without an endpoint", async () => {
    const fetchImpl = vi.fn();
    expect(await sendConclusion({ env: { GH_AW_DEFAULT_ADMISSION_AUDIENCE: "not-enabled" }, sessionFile: "/missing", fetchImpl })).toEqual({ result: "skipped" });
    expect(fetchImpl).not.toHaveBeenCalled();
  });

  it.each([202, 204])("posts compact unified JSONL, artifact IDs and invocation identity, accepting %i", async status => {
    const fetchImpl = vi.fn().mockImplementationOnce(oidc).mockResolvedValueOnce(new Response(null, { status }));
    const mask = vi.fn();
    options.env.GITHUB_TOKEN = "never-forward";
    options.env.GH_AW_ADMISSION_RESULT = "ignored";
    options.env.GITHUB_JOB = "conclusion";
    options.env.GITHUB_AW_OTEL_TRACE_ID = "a".repeat(32);
    expect(await sendConclusion({ ...options, fetchImpl, mask })).toEqual({ result: "delivered" });
    const [url, acquisition] = fetchImpl.mock.calls[0];
    expect(url.searchParams.get("job")).toBe("synthetic");
    expect(url.searchParams.get("audience")).toBe(base.GH_AW_DEFAULT_ADMISSION_URL);
    expect(acquisition.headers.authorization).toBe("bearer synthetic-bootstrap");
    const [endpoint, request] = fetchImpl.mock.calls[1];
    expect(endpoint).toBe("https://admission.example.org/api/conclusion");
    expect(request.method).toBe("POST");
    expect(request.redirect).toBe("manual");
    expect(request.headers).toEqual({
      authorization: `bearer ${jwt()}`,
      "content-type": "application/json",
      "cache-control": "no-store",
      traceparent: expect.stringMatching(new RegExp(`^00-${"a".repeat(32)}-[0-9a-f]{16}-01$`)),
      baggage: "cicd.pipeline.task.name=conclusion",
    });
    expect(JSON.parse(request.body)).toEqual({
      repo: base.GITHUB_REPOSITORY,
      workflow_id: base.GH_AW_INFO_WORKFLOW_ID,
      workflow_hash: base.GH_AW_ADMISSION_WORKFLOW_HASH,
      workflow_call_id: `${base.GITHUB_RUN_ID}-2:${base.GH_AW_INFO_WORKFLOW_ID}`,
      run_id: base.GITHUB_RUN_ID,
      run_attempt: "2",
      job_id: "conclusion",
      ref: base.GITHUB_REF,
      event_type: base.GITHUB_EVENT_NAME,
      actor: "original",
      triggering_actor: "rerunner",
      session: compactSession,
      session_compaction: { version: 1, included_events: 2, omitted_events: 1, oversized_lines: 0, scan_truncated: false, truncated: false },
      usage_artifact_ids: ["12345"],
    });
    expect(request.body).not.toContain("never-forward");
    expect(mask).toHaveBeenCalledExactlyOnceWith(jwt());
  });

  it("retains the configured audience instead of deriving one from the conclusion route", async () => {
    options.env.GH_AW_DEFAULT_ADMISSION_AUDIENCE = "authority-deployment";
    const fetchImpl = vi.fn().mockImplementationOnce(oidc).mockImplementationOnce(accepted);
    await sendConclusion({ ...options, fetchImpl });
    expect(fetchImpl.mock.calls[0][0].searchParams.get("audience")).toBe("authority-deployment");
  });

  it("compacts the actual generated session without modifying the full artifact", async () => {
    const { writeUnifiedSession } = require("./unified_session.cjs");
    const { validateSession } = require("./scripts/validate_session.cjs");
    fs.writeFileSync(path.join(directory, "aw_info.json"), JSON.stringify({ engine_id: "copilot", run_id: base.GITHUB_RUN_ID, run_attempt: 2 }));
    fs.writeFileSync(path.join(directory, "agent-session.jsonl"), JSON.stringify({ type: "assistant.message", data: { content: "done with synthetic-private-value" } }) + "\n");
    fs.writeFileSync(path.join(directory, "agent-stdio.log"), "::add-mask::synthetic-private-value\n");
    writeUnifiedSession({ rootDir: directory, outputPath: sessionFile, warn: () => {} });
    const generated = fs.readFileSync(sessionFile, "utf8");
    expect(validateSession(generated)).toBeGreaterThan(0);
    expect(generated).not.toContain("synthetic-private-value");
    const fetchImpl = vi.fn().mockImplementationOnce(oidc).mockImplementationOnce(accepted);
    await sendConclusion({ ...options, fetchImpl });
    const body = JSON.parse(fetchImpl.mock.calls[1][1].body);
    expect(body.session).not.toBe(generated);
    expect(body.session).not.toContain("assistant.message");
    expect(body.session).not.toContain("synthetic-private-value");
    expect(body.usage_artifact_ids).toEqual(["12345"]);
    expect(fs.readFileSync(sessionFile, "utf8")).toBe(generated);
  });

  it("uses validated compact metadata passed from the artifact-publishing conclusion job", async () => {
    options.env.GH_AW_ADMISSION_COMPACT_SESSION = JSON.stringify({
      version: 1,
      session: compactSession,
      included_events: 2,
      omitted_events: 1,
      oversized_lines: 0,
      scan_truncated: false,
      truncated: false,
    });
    const fetchImpl = vi.fn().mockImplementationOnce(oidc).mockImplementationOnce(accepted);
    await sendConclusion({ ...options, sessionFile: "/missing", fetchImpl });
    const body = JSON.parse(fetchImpl.mock.calls[1][1].body);
    expect(body.session).toBe(compactSession);
    expect(body.session_compaction).toEqual({
      version: 1,
      included_events: 2,
      omitted_events: 1,
      oversized_lines: 0,
      scan_truncated: false,
      truncated: false,
    });
  });

  it("rejects malformed compact metadata passed through job outputs before requesting OIDC", async () => {
    options.env.GH_AW_ADMISSION_COMPACT_SESSION = JSON.stringify({
      version: 1,
      session: "not JSONL",
      included_events: 1,
      omitted_events: 0,
      oversized_lines: 0,
      scan_truncated: false,
      truncated: false,
    });
    const fetchImpl = vi.fn();
    await expect(sendConclusion({ ...options, fetchImpl })).rejects.toThrow();
    expect(fetchImpl).not.toHaveBeenCalled();
  });

  it.each([
    { type: "assistant.message", data: { content: "private conversation" } },
    { type: "usage.report", data: { aic: 1, toolOutput: "private output" } },
    { type: "usage.report", data: { model: "x".repeat(257) } },
    { type: "workflow.info", data: { awContext: { nested: "private context" } } },
    null,
    ["not an event"],
  ])("rejects non-compact records supplied through job outputs (%j)", async event => {
    options.env.GH_AW_ADMISSION_COMPACT_SESSION = JSON.stringify({
      version: 1,
      session: JSON.stringify(event) + "\n",
      included_events: 1,
      omitted_events: 0,
      oversized_lines: 0,
      scan_truncated: false,
      truncated: false,
    });
    const fetchImpl = vi.fn().mockImplementationOnce(oidc).mockImplementationOnce(accepted);
    await expect(sendConclusion({ ...options, fetchImpl })).rejects.toThrow();
    expect(fetchImpl).not.toHaveBeenCalled();
  });

  it.each(["omitted_events", "oversized_lines"])("rejects unsafe integer coverage counters in %s", async field => {
    options.env.GH_AW_ADMISSION_COMPACT_SESSION = JSON.stringify({
      version: 1,
      session: compactSession,
      included_events: 2,
      omitted_events: 0,
      oversized_lines: 0,
      scan_truncated: false,
      truncated: false,
      [field]: 1e100,
    });
    const fetchImpl = vi.fn().mockImplementationOnce(oidc).mockImplementationOnce(accepted);
    await expect(sendConclusion({ ...options, fetchImpl })).rejects.toThrow();
    expect(fetchImpl).not.toHaveBeenCalled();
  });

  it.each(["missing", "empty", "invalid UTF-8", "invalid JSON", "symlink"])("rejects %s session files before requesting OIDC", async kind => {
    if (kind === "missing") fs.unlinkSync(sessionFile);
    if (kind === "empty") fs.writeFileSync(sessionFile, "");
    if (kind === "invalid UTF-8") fs.writeFileSync(sessionFile, Buffer.from([0xff]));
    if (kind === "invalid JSON") fs.writeFileSync(sessionFile, "{");
    if (kind === "symlink") {
      const target = path.join(directory, "target.jsonl");
      fs.renameSync(sessionFile, target);
      fs.symlinkSync(target, sessionFile);
    }
    const fetchImpl = vi.fn();
    await expect(sendConclusion({ ...options, fetchImpl })).rejects.toThrow();
    expect(fetchImpl).not.toHaveBeenCalled();
  });

  it.each([
    ["12345", "", ["12345"]],
    ["", "23456", ["23456"]],
    ["12345", "23456", ["23456", "12345"]],
    ["12345", "12345", ["12345"]],
    ["", "", []],
    ["90071992547409931234", "", ["90071992547409931234"]],
  ])("includes available upload artifact IDs without losing precision (%s, %s)", async (initial, retry, expected) => {
    options.env.GH_AW_USAGE_ARTIFACT_ID = initial;
    options.env.GH_AW_USAGE_ARTIFACT_RETRY_ID = retry;
    const fetchImpl = vi.fn().mockImplementationOnce(oidc).mockImplementationOnce(accepted);
    await sendConclusion({ ...options, fetchImpl });
    expect(JSON.parse(fetchImpl.mock.calls[1][1].body).usage_artifact_ids).toEqual(expected);
  });

  it.each([
    [{ GH_AW_USAGE_ARTIFACT_ID: "12345", GH_AW_USAGE_ARTIFACT_RETRY_ID: "23456" }, ["23456", "12345"]],
    [{ GH_AW_USAGE_ARTIFACT_ID: "12345", GH_AW_USAGE_ARTIFACT_RETRY_ID: "12345" }, ["12345"]],
    [{ GH_AW_USAGE_ARTIFACT_ID: "12345" }, ["12345"]],
    [{ GH_AW_USAGE_ARTIFACT_RETRY_ID: "23456" }, ["23456"]],
    [{}, []],
  ])("preserves retry-first artifact ordering in the ID reader (%j)", (env, expected) => {
    expect(readUsageArtifactIds(env)).toEqual(expected);
  });

  it.each(["0", "-1", "1e2", " 1", "1/2", "123\n", "1".repeat(21)])("rejects malformed artifact IDs %j before OIDC", async id => {
    options.env.GH_AW_USAGE_ARTIFACT_ID = id;
    const fetchImpl = vi.fn();
    await expect(sendConclusion({ ...options, fetchImpl })).rejects.toThrow();
    expect(fetchImpl).not.toHaveBeenCalled();
  });

  it("bounds the entire request at 64 KiB after JSON escaping and large source messages", async () => {
    const metadata = JSON.stringify({ type: "workflow.info", data: { model: "\\".repeat(256), engineId: '"'.repeat(256), cliVersion: "\u0000".repeat(256) } }) + "\n";
    fs.writeFileSync(sessionFile, session + JSON.stringify({ type: "assistant.message", data: { content: "private-unbounded".repeat(100000) } }) + "\n" + metadata.repeat(1000));
    options.env.GITHUB_REF = "x".repeat(4000);
    const fetchImpl = vi.fn().mockImplementationOnce(oidc).mockImplementationOnce(accepted);
    expect(await sendConclusion({ ...options, fetchImpl })).toEqual({ result: "delivered" });
    const request = fetchImpl.mock.calls[1][1].body;
    expect(MAX_REQUEST_BYTES).toBe(64 * 1024);
    expect(Buffer.byteLength(request)).toBeLessThanOrEqual(MAX_REQUEST_BYTES);
    const body = JSON.parse(request);
    expect(body.session_compaction.truncated).toBe(true);
    expect(body.session_compaction.oversized_lines).toBe(1);
    expect(body.session).not.toContain("private-unbounded");
    expect(body.session).not.toContain("assistant.message");
  });

  it.each(["GH_AW_ADMISSION_WORKFLOW_HASH", "GH_AW_INFO_WORKFLOW_ID", "ACTIONS_ID_TOKEN_REQUEST_TOKEN"])("rejects missing %s without credential fallback", async key => {
    options.env[key] = "";
    const fetchImpl = vi.fn();
    await expect(sendConclusion({ ...options, fetchImpl })).rejects.toThrow();
    expect(fetchImpl).not.toHaveBeenCalled();
  });

  it.each([200, 301, 302, 307, 308, 400, 401, 403, 404])("does not retry unsupported response %i or follow redirects", async status => {
    const fetchImpl = vi.fn().mockImplementationOnce(oidc).mockResolvedValueOnce(new Response("sensitive response", { status }));
    await expect(sendConclusion({ ...options, fetchImpl })).rejects.toThrow();
    expect(fetchImpl).toHaveBeenCalledTimes(2);
  });

  it("retries transient failures with an unchanged body, audience and trace ID", async () => {
    const fetchImpl = vi
      .fn()
      .mockImplementationOnce(oidc)
      .mockImplementationOnce(() => {
        fs.writeFileSync(sessionFile, "changed");
        options.env.GITHUB_ACTOR = "changed";
        options.env.GH_AW_USAGE_ARTIFACT_ID = "99999";
        return new Response(null, { status: 503 });
      })
      .mockImplementationOnce(accepted);
    const promise = sendConclusion({ ...options, fetchImpl });
    await vi.runAllTimersAsync();
    expect(await promise).toEqual({ result: "delivered" });
    const first = fetchImpl.mock.calls[1][1];
    const second = fetchImpl.mock.calls[2][1];
    expect(first.body).toBe(second.body);
    expect(JSON.parse(first.body).session).toBe(compactSession);
    expect(JSON.parse(first.body).usage_artifact_ids).toEqual(["12345"]);
    expect(JSON.parse(first.body).actor).toBe("original");
    expect(first.headers.traceparent.slice(0, 35)).toBe(second.headers.traceparent.slice(0, 35));
    expect(first.headers.traceparent).not.toBe(second.headers.traceparent);
  });

  it.each(["ignore", "block"])("reports exhausted delivery as unavailable under %s, without changing admission", async policy => {
    options.env.GH_AW_DEFAULT_ADMISSION_ON_OUTAGE = policy;
    const fetchImpl = vi
      .fn()
      .mockImplementationOnce(oidc)
      .mockImplementation(() => new Response(null, { status: 503 }));
    const promise = sendConclusion({ ...options, fetchImpl });
    await vi.runAllTimersAsync();
    expect(await promise).toEqual({ result: "unavailable" });
    expect(fetchImpl).toHaveBeenCalledTimes(4);
  });

  it("bounds token acquisition and retries even for a transport ignoring abort", async () => {
    const fetchImpl = vi.fn().mockImplementation(() => new Promise(() => {}));
    const start = Date.now();
    const promise = sendConclusion({ ...options, fetchImpl });
    await vi.runAllTimersAsync();
    expect(await promise).toEqual({ result: "unavailable" });
    expect(Date.now() - start).toBeLessThanOrEqual(60000);
    expect(fetchImpl).toHaveBeenCalledTimes(3);
    expect(vi.getTimerCount()).toBe(0);
  });

  it("cancels HTTP requests at the overall deadline and ignores late acceptance", async () => {
    let offset = 0;
    let acceptLate;
    const fetchImpl = vi
      .fn()
      .mockImplementationOnce(oidc)
      .mockImplementation(() => {
        offset = 59900;
        return new Promise(resolve => {
          acceptLate = resolve;
        });
      });
    const promise = sendConclusion({ ...options, now: () => Date.now() + offset, fetchImpl });
    await vi.advanceTimersByTimeAsync(10000);
    expect(await promise).toEqual({ result: "unavailable" });
    acceptLate(accepted());
    await vi.advanceTimersByTimeAsync(60000);
    expect(fetchImpl).toHaveBeenCalledTimes(2);
    expect(vi.getTimerCount()).toBe(0);
  });

  it("warns generically on failure without failing the job or exposing exception details", async () => {
    for (const [key, value] of Object.entries(base)) vi.stubEnv(key, value);
    vi.spyOn(fs, "openSync").mockImplementation(() => {
      throw new Error("sensitive session detail");
    });
    const core = { warning: vi.fn(), info: vi.fn(), setSecret: vi.fn(), setFailed: vi.fn() };
    await main(core);
    expect(core.warning).toHaveBeenCalledExactlyOnceWith(FAILURE);
    expect(core.setFailed).not.toHaveBeenCalled();
    expect(JSON.stringify(core.warning.mock.calls)).not.toContain("sensitive session detail");
  });
});
