import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
const { checkAdmission, main } = require("./check_admission.cjs");
const { FAILURE } = require("./admission_protocol.cjs");
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
};
const jwt = () => `synthetic.${Buffer.from(JSON.stringify({ exp: Math.floor(Date.now() / 1000) + 300 })).toString("base64url")}.signature`;
const oidcResponse = () => Response.json({ value: jwt() });
const success = () => Response.json({ max_ai_credits: 1000 });
const options = env => ({ env: { ...base, ...env }, now: () => Date.now() });
async function settle(promise) {
  await vi.runAllTimersAsync();
  return promise;
}

describe("bounded OIDC-only admission client", () => {
  beforeEach(() => {
    vi.spyOn(require("node:fs"), "appendFileSync").mockImplementation(() => {});
    vi.useFakeTimers();
    vi.spyOn(Math, "random").mockReturnValue(0);
    vi.stubEnv("GH_AW_OTLP_ENDPOINTS", "");
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
    vi.unstubAllEnvs();
    vi.unstubAllGlobals();
  });
  it("is a no-op before touching identity or credentials when disabled", async () => {
    const fetchImpl = vi.fn();
    expect(await checkAdmission({ env: {}, fetchImpl })).toEqual({ result: "skipped" });
    expect(fetchImpl).not.toHaveBeenCalled();
  });
  it("preserves encoded job identity and current trace across admission retries", async () => {
    const env = options({ GITHUB_JOB: "activation/review", GITHUB_AW_OTEL_TRACE_ID: "a".repeat(32), GITHUB_AW_OTEL_PARENT_SPAN_ID: "b".repeat(16) }).env;
    const fetchImpl = vi
      .fn()
      .mockImplementationOnce(oidcResponse)
      .mockImplementationOnce(() => {
        env.GITHUB_JOB = "changed";
        return new Response(null, { status: 503 });
      })
      .mockImplementationOnce(success);
    const decision = await settle(checkAdmission({ env, fetchImpl }));
    for (const [, request] of fetchImpl.mock.calls.slice(1)) {
      expect(request.headers).toMatchObject({
        traceparent: expect.stringMatching(new RegExp(`^00-${"a".repeat(32)}-[0-9a-f]{16}-01$`)),
        baggage: "cicd.pipeline.task.name=activation%2Freview",
      });
    }
    expect(fetchImpl.mock.calls[1][1].headers.traceparent).toBe(`00-${decision.traceId}-${decision.attempts[0].spanId}-01`);
    expect(fetchImpl.mock.calls[2][1].headers.traceparent).toBe(`00-${decision.traceId}-${decision.attempts[1].spanId}-01`);
  });
  it.each(["ACTIONS_ID_TOKEN_REQUEST_URL", "ACTIONS_ID_TOKEN_REQUEST_TOKEN", "GH_AW_INFO_WORKFLOW_ID", "GH_AW_ADMISSION_WORKFLOW_HASH", "GITHUB_RUN_ATTEMPT"])("blocks missing %s without a fallback", async key => {
    const fetchImpl = vi.fn();
    await expect(checkAdmission({ ...options({ [key]: "" }), fetchImpl })).rejects.toThrow(FAILURE);
    expect(fetchImpl).not.toHaveBeenCalled();
  });
  it("sends exactly the OIDC bearer, ten strings including the workflow hash, and no other credentials or context", async () => {
    const fetchImpl = vi.fn().mockImplementationOnce(oidcResponse).mockImplementationOnce(success);
    const mask = vi.fn();
    const decision = await checkAdmission({ ...options({ GITHUB_TOKEN: "never-forward", COPILOT_GITHUB_TOKEN: "never-forward", GH_AW_DEFAULT_ADMISSION_AUDIENCE: "aud/?&=🦊" }), fetchImpl, mask });
    expect(decision).toMatchObject({ result: "admitted", maxAiCredits: 1000 });
    expect(decision.traceId).toMatch(/^(?!0{32}$)[0-9a-f]{32}$/);
    const [url, acquisition] = fetchImpl.mock.calls[0];
    expect(url.searchParams.get("job")).toBe("synthetic");
    expect(url.searchParams.get("audience")).toBe("aud/?&=🦊");
    expect(acquisition.headers.authorization).toBe("bearer synthetic-bootstrap");
    const [endpoint, request] = fetchImpl.mock.calls[1];
    expect(endpoint).toBe(base.GH_AW_DEFAULT_ADMISSION_URL);
    expect(request.method).toBe("POST");
    expect(request.redirect).toBe("manual");
    expect(request.headers).toEqual({ authorization: `bearer ${jwt()}`, "content-type": "application/json", "cache-control": "no-store", traceparent: expect.stringMatching(new RegExp(`^00-${decision.traceId}-[0-9a-f]{16}-01$`)) });
    expect(JSON.parse(request.body)).toMatchObject({ actor: "original", triggering_actor: "rerunner", workflow_id: base.GH_AW_INFO_WORKFLOW_ID, workflow_hash: base.GH_AW_ADMISSION_WORKFLOW_HASH, run_id: base.GITHUB_RUN_ID });
    expect(Object.keys(JSON.parse(request.body))).toHaveLength(10);
    expect(request.body).not.toContain("never-forward");
    expect(mask).toHaveBeenCalledExactlyOnceWith(jwt());
  });
  it.each(["600", "2000", "-1", "0", ""])("retains the service decision without changing the local runtime budget (%s)", async local => {
    const fetchImpl = vi.fn().mockImplementationOnce(oidcResponse).mockImplementationOnce(success);
    const env = options({ GH_AW_MAX_AI_CREDITS: local }).env;
    expect((await checkAdmission({ env, fetchImpl })).maxAiCredits).toBe(1000);
    expect(env.GH_AW_MAX_AI_CREDITS).toBe(local);
  });
  it.each([403, 301, 302, 307, 308, 201, 204, 400, 401, 404])("blocks status %i immediately under ignore", async status => {
    const fetchImpl = vi
      .fn()
      .mockImplementationOnce(oidcResponse)
      .mockImplementationOnce(() => new Response(status === 204 ? null : "secret diagnostic", { status }));
    await expect(checkAdmission({ ...options(), fetchImpl })).rejects.toThrow(FAILURE);
    expect(fetchImpl).toHaveBeenCalledTimes(2);
  });
  it.each([408, 429, 500, 503, 599])("retries transient status %i with stable identity and token", async status => {
    const env = options().env;
    const fetchImpl = vi
      .fn()
      .mockImplementationOnce(oidcResponse)
      .mockImplementationOnce(() => {
        env.GITHUB_ACTOR = "changed-after-snapshot";
        env.GH_AW_ADMISSION_WORKFLOW_HASH = `${"c".repeat(64)}:${"d".repeat(64)}`;
        return new Response(null, { status });
      })
      .mockImplementationOnce(success);
    const decision = await settle(checkAdmission({ env, fetchImpl }));
    expect(decision.result).toBe("admitted");
    expect(fetchImpl.mock.calls[1][1].body).toBe(fetchImpl.mock.calls[2][1].body);
    expect(fetchImpl.mock.calls[1][1].headers.authorization).toBe(fetchImpl.mock.calls[2][1].headers.authorization);
    expect(JSON.parse(fetchImpl.mock.calls[2][1].body).actor).toBe("original");
    expect(JSON.parse(fetchImpl.mock.calls[2][1].body).workflow_hash).toBe(base.GH_AW_ADMISSION_WORKFLOW_HASH);
  });
  it("stops on explicit denial after a transient failure", async () => {
    const fetchImpl = vi
      .fn()
      .mockImplementationOnce(oidcResponse)
      .mockResolvedValueOnce(new Response(null, { status: 503 }))
      .mockResolvedValueOnce(new Response(null, { status: 403 }));
    const assertion = expect(checkAdmission({ ...options(), fetchImpl })).rejects.toThrow(FAILURE);
    await settle(assertion);
    expect(fetchImpl).toHaveBeenCalledTimes(3);
  });
  it.each(["ignore", "block"])("exhausts exactly three HTTP attempts under %s", async policy => {
    const fetchImpl = vi
      .fn()
      .mockImplementationOnce(oidcResponse)
      .mockImplementation(() => new Response("never expose", { status: 503 }));
    const promise = checkAdmission({ ...options({ GH_AW_DEFAULT_ADMISSION_ON_OUTAGE: policy }), fetchImpl });
    const assertion = policy === "ignore" ? expect(promise).resolves.toMatchObject({ result: "ignored" }) : expect(promise).rejects.toThrow(FAILURE);
    await settle(assertion);
    expect(fetchImpl).toHaveBeenCalledTimes(4);
  });
  it.each([0, 0.999])("retains 250/500ms exponential backoff with bounded jitter (%s)", async random => {
    vi.spyOn(Math, "random").mockReturnValue(random);
    const start = Date.now();
    const attempts = [];
    const fetchImpl = vi
      .fn()
      .mockImplementationOnce(oidcResponse)
      .mockImplementation(() => {
        attempts.push(Date.now() - start);
        return new Response(null, { status: 503 });
      });
    expect(await settle(checkAdmission({ ...options(), fetchImpl }))).toMatchObject({ result: "ignored" });
    const jitter = Math.floor(random * 250);
    expect(attempts).toEqual([0, 250 + jitter, 750 + 2 * jitter]);
  });
  it.each(["ignore", "block"])("cancels backoff at the overall deadline without late requests (%s)", async policy => {
    const start = Date.now();
    let offset = 0;
    const fetchImpl = vi
      .fn()
      .mockImplementationOnce(oidcResponse)
      .mockImplementation(() => {
        offset = 59900;
        return new Response(null, { status: 503 });
      });
    const promise = checkAdmission({ ...options({ GH_AW_DEFAULT_ADMISSION_ON_OUTAGE: policy }), now: () => Date.now() + offset, fetchImpl });
    const assertion = policy === "ignore" ? expect(promise).resolves.toMatchObject({ result: "ignored" }) : expect(promise).rejects.toThrow(FAILURE);
    await vi.advanceTimersByTimeAsync(100);
    await assertion;
    expect(Date.now() - start).toBe(100);
    expect(vi.getTimerCount()).toBe(0);
    await vi.advanceTimersByTimeAsync(60000);
    expect(fetchImpl).toHaveBeenCalledTimes(2);
  });
  it("retries network and interrupted-body transport failures", async () => {
    const brokenBody = new ReadableStream({
      start(controller) {
        controller.error(new Error("sensitive transport detail"));
      },
    });
    const fetchImpl = vi
      .fn()
      .mockImplementationOnce(oidcResponse)
      .mockRejectedValueOnce(new Error("secret transport"))
      .mockResolvedValueOnce(new Response(brokenBody, { headers: { "content-type": "application/json" } }))
      .mockImplementationOnce(success);
    expect((await settle(checkAdmission({ ...options(), fetchImpl }))).result).toBe("admitted");
    expect(fetchImpl).toHaveBeenCalledTimes(4);
  });
  it("sanitizes transport errors before the shared retry helper logs them", async () => {
    const core = { info: vi.fn(), warning: vi.fn(), debug: vi.fn() };
    vi.stubGlobal("core", core);
    const error = Object.assign(new Error(`private transport ${base.ACTIONS_ID_TOKEN_REQUEST_TOKEN} ${jwt()}`), {
      response: { status: 403, headers: { "retry-after": "600", "x-ratelimit-resource": "private-header" } },
    });
    const fetchImpl = vi.fn().mockImplementationOnce(oidcResponse).mockRejectedValue(error);
    expect(await settle(checkAdmission({ ...options(), fetchImpl }))).toMatchObject({ result: "ignored" });
    expect(fetchImpl).toHaveBeenCalledTimes(4);
    const diagnostics = JSON.stringify(Object.values(core).flatMap(log => log.mock.calls));
    expect(diagnostics).toContain(FAILURE);
    for (const privateValue of ["private transport", base.ACTIONS_ID_TOKEN_REQUEST_TOKEN, jwt(), "private-header", "Retry-After"]) {
      expect(diagnostics).not.toContain(privateValue);
    }
  });
  it.each([
    Response.json({ max_ai_credits: 1, correlation_id: "8e53c1f4a62049b5b382704fe9d61a0c" }),
    new Response("not json", { headers: { "content-type": "application/json" } }),
    new Response(" ".repeat(4097), { headers: { "content-type": "application/json" } }),
    new Response('{"max_ai_credits":1e3}', { headers: { "content-type": "application/json" } }),
    new Response("{}", { headers: { "content-type": "text/plain" } }),
  ])("does not turn invalid success into a fail-open outage", async response => {
    const fetchImpl = vi.fn().mockImplementationOnce(oidcResponse).mockResolvedValueOnce(response);
    await expect(checkAdmission({ ...options(), fetchImpl })).rejects.toThrow(FAILURE);
    expect(fetchImpl).toHaveBeenCalledTimes(2);
  });
  it.each([401, 403, 302])("blocks explicit issuer failures (%i)", async status => {
    await expect(checkAdmission({ ...options(), fetchImpl: vi.fn().mockResolvedValue(new Response("secret", { status })) })).rejects.toThrow(FAILURE);
  });
  it("bounds each body read to ten seconds and cancels stalled transports", async () => {
    let canceled = 0;
    const fetchImpl = vi
      .fn()
      .mockImplementationOnce(oidcResponse)
      .mockImplementation((_url, init) => {
        return new Response(
          new ReadableStream({
            start(controller) {
              init.signal.addEventListener("abort", () => {
                canceled++;
                controller.error(new Error("cancelled"));
              });
            },
          }),
          { headers: { "content-type": "application/json" } }
        );
      });
    const start = Date.now();
    expect(await settle(checkAdmission({ ...options(), fetchImpl }))).toMatchObject({ result: "ignored" });
    expect(Date.now() - start).toBeLessThan(32000);
    expect(canceled).toBe(3);
  });
  it.each(["ignore", "block"])("enforces one 60-second deadline across acquisition, renewal, requests and backoff (%s)", async policy => {
    const start = Date.now();
    let lateSuccesses = 0;
    const signals = [];
    const fetchImpl = vi.fn(async (url, init) => {
      signals.push(init.signal);
      await new Promise(resolve => setTimeout(resolve, 9999));
      if (String(url).startsWith("https://oidc")) {
        const token = `synthetic.${Buffer.from(JSON.stringify({ exp: Math.ceil(Date.now() / 1000) + 11 })).toString("base64url")}.signature`;
        return Response.json({ value: token });
      }
      if (Date.now() - start > 59000) {
        lateSuccesses++;
        return success();
      }
      return new Response(null, { status: 503 });
    });
    const promise = checkAdmission({ ...options({ GH_AW_DEFAULT_ADMISSION_ON_OUTAGE: policy }), fetchImpl });
    const assertion = policy === "ignore" ? expect(promise).resolves.toMatchObject({ result: "ignored" }) : expect(promise).rejects.toThrow(FAILURE);
    await settle(assertion);
    expect(signals.every(signal => signal.aborted)).toBe(true);
    expect(lateSuccesses).toBeLessThanOrEqual(1);
    expect(fetchImpl.mock.calls.filter(([url]) => typeof url === "string")).toHaveLength(3);
  });
  it("creates distinct attempt spans under one existing trace, with no baggage", async () => {
    vi.stubEnv("GH_AW_OTLP_ENDPOINTS", '[{"url":"https://telemetry.example.test"}]');
    const traceId = "11111111111111111111111111111111";
    const fetchImpl = vi
      .fn()
      .mockImplementationOnce(oidcResponse)
      .mockResolvedValueOnce(new Response(null, { status: 503 }))
      .mockImplementationOnce(success);
    const decision = await settle(checkAdmission({ ...options({ GITHUB_AW_OTEL_TRACE_ID: traceId, GITHUB_AW_OTEL_PARENT_SPAN_ID: "2222222222222222", TRACEPARENT: "untrusted", OTEL_BAGGAGE: "never" }), fetchImpl }));
    expect(decision.traceId).toBe(traceId);
    const headers = fetchImpl.mock.calls.slice(1).map(([, init]) => init.headers);
    expect(headers[0].traceparent).toMatch(new RegExp(`^00-${traceId}-[0-9a-f]{16}-01$`));
    expect(headers[0].traceparent).not.toBe(headers[1].traceparent);
    expect(headers[0].baggage).toBeUndefined();
  });
  it.each(["", "00000000000000000000000000000000", "untrusted", "A".repeat(32)])("generates valid caller trace context when existing context is invalid (%s)", async traceId => {
    const fetchImpl = vi.fn().mockImplementationOnce(oidcResponse).mockImplementationOnce(success);
    const decision = await checkAdmission({ ...options({ GITHUB_AW_OTEL_TRACE_ID: traceId, GITHUB_AW_OTEL_PARENT_SPAN_ID: "2222222222222222", TRACEPARENT: "untrusted" }), fetchImpl });
    expect(decision.traceId).toMatch(/^(?!0{32}$)[0-9a-f]{32}$/);
    expect(decision.parentSpanId).toBeUndefined();
    expect(fetchImpl.mock.calls[1][1].headers.traceparent).toMatch(new RegExp(`^00-${decision.traceId}-[0-9a-f]{16}-01$`));
    expect(decision.endpoints).toEqual([]);
  });
  it.each(["admitted", "ignored"])("publishes the %s decision and exports it for activation's daily-validation conditions", async result => {
    for (const [key, value] of Object.entries(base)) vi.stubEnv(key, value);
    const core = { setOutput: vi.fn(), exportVariable: vi.fn(), setSecret: vi.fn(), warning: vi.fn(), setFailed: vi.fn() };
    const fetchImpl = vi.fn().mockImplementationOnce(oidcResponse);
    fetchImpl.mockImplementation(result === "admitted" ? success : () => new Response(null, { status: 503 }));
    vi.stubGlobal("fetch", fetchImpl);
    await settle(main(core));
    expect(core.setFailed).not.toHaveBeenCalled();
    expect(core.setOutput).toHaveBeenCalledWith("result", result);
    expect(core.setOutput.mock.calls.some(([key]) => key === "correlation-id")).toBe(false);
    expect(core.exportVariable.mock.calls.map(([key]) => key)).toEqual(["GITHUB_AW_OTEL_TRACE_ID", "GH_AW_ADMISSION_RESULT"]);
    expect(core.exportVariable).toHaveBeenCalledWith("GH_AW_ADMISSION_RESULT", result);
    expect(fetchImpl).toHaveBeenCalledTimes(result === "admitted" ? 2 : 4);
    if (result === "admitted") expect(core.setOutput).toHaveBeenCalledWith("max-ai-credits", "1000");
    else expect(core.warning).toHaveBeenCalledOnce();
  });
  it("publishes optional component allowances only as activation step outputs", async () => {
    for (const [key, value] of Object.entries(base)) vi.stubEnv(key, value);
    const core = { setOutput: vi.fn(), exportVariable: vi.fn(), setSecret: vi.fn(), warning: vi.fn(), setFailed: vi.fn() };
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockImplementationOnce(oidcResponse)
        .mockResolvedValueOnce(Response.json({ max_ai_credits: 1000, max_ai_credits_threat_detection: 10, max_ai_credits_evals: 20 }))
    );
    await main(core);
    expect(core.setFailed).not.toHaveBeenCalled();
    expect(core.setOutput).toHaveBeenCalledWith("max-ai-credits-threat-detection", "10");
    expect(core.setOutput).toHaveBeenCalledWith("max-ai-credits-evals", "20");
    expect(core.exportVariable.mock.calls.map(([key]) => key)).toEqual(["GITHUB_AW_OTEL_TRACE_ID", "GH_AW_ADMISSION_RESULT"]);
  });
  it("exposes only generic diagnostics on a faulty issuer response", async () => {
    for (const [key, value] of Object.entries(base)) vi.stubEnv(key, value);
    const core = { setOutput: vi.fn(), exportVariable: vi.fn(), setSecret: vi.fn(), warning: vi.fn(), setFailed: vi.fn() };
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response("private diagnostics secret", { status: 403 })));
    await main(core);
    expect(core.setFailed).toHaveBeenCalledExactlyOnceWith(FAILURE);
    expect(core.setOutput).not.toHaveBeenCalled();
    expect(core.exportVariable).not.toHaveBeenCalled();
    expect(core.warning).not.toHaveBeenCalled();
  });
  it.each(["admitted", "ignored", "blocked"])("writes a collapsed, privacy-preserving %s summary and lifecycle logs", async outcome => {
    for (const [key, value] of Object.entries(base)) vi.stubEnv(key, value);
    const core = {
      info: vi.fn(),
      warning: vi.fn(),
      setFailed: vi.fn(),
      setSecret: vi.fn(),
      setOutput: vi.fn(),
      exportVariable: vi.fn(),
      summary: { addRaw: vi.fn(), addDetails: vi.fn(), write: vi.fn().mockResolvedValue(undefined) },
    };
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockImplementationOnce(oidcResponse)
        .mockImplementation(() =>
          outcome === "admitted" ? Response.json({ max_ai_credits: 98765, max_ai_credits_threat_detection: 87654, max_ai_credits_evals: 76543 }) : new Response("private-service-diagnostic", { status: outcome === "blocked" ? 403 : 503 })
        )
    );
    await settle(main(core));
    expect(core.summary.addRaw).toHaveBeenCalledWith("### Admission gate\n\n");
    expect(core.summary.addDetails).toHaveBeenCalledWith(expect.stringContaining("values withheld"), expect.stringContaining(outcome));
    expect(core.summary.write).toHaveBeenCalledOnce();
    const diagnostics = JSON.stringify([core.info.mock.calls, core.warning.mock.calls, core.summary.addRaw.mock.calls, core.summary.addDetails.mock.calls]);
    for (const value of [base.ACTIONS_ID_TOKEN_REQUEST_TOKEN, base.GH_AW_DEFAULT_ADMISSION_URL, base.GITHUB_REPOSITORY, base.GH_AW_ADMISSION_WORKFLOW_HASH, "rerunner", "private-service-diagnostic", "98765", "87654", "76543"]) {
      expect(diagnostics).not.toContain(value);
    }
    expect(core.info).toHaveBeenCalledWith(expect.stringContaining("OIDC"));
    if (outcome === "admitted") {
      expect(core.setOutput).toHaveBeenCalledWith("max-ai-credits", "98765");
      expect(core.summary.addDetails).toHaveBeenCalledWith(expect.any(String), expect.stringContaining("runtime enforcement"));
    } else if (outcome === "blocked") expect(core.setFailed).toHaveBeenCalledExactlyOnceWith(FAILURE);
  });
  it("reports summary write failures without changing admission approval", async () => {
    for (const [key, value] of Object.entries(base)) vi.stubEnv(key, value);
    const core = {
      info: vi.fn(),
      warning: vi.fn(),
      setFailed: vi.fn(),
      setSecret: vi.fn(),
      setOutput: vi.fn(),
      exportVariable: vi.fn(),
      summary: { addRaw: vi.fn(), addDetails: vi.fn(), write: vi.fn().mockRejectedValue(new Error("private-filesystem-diagnostic")) },
    };
    vi.stubGlobal("fetch", vi.fn().mockImplementationOnce(oidcResponse).mockImplementationOnce(success));
    await main(core);
    expect(core.setFailed).not.toHaveBeenCalled();
    expect(core.setOutput).toHaveBeenCalledWith("result", "admitted");
    expect(core.warning).toHaveBeenCalledExactlyOnceWith("ERR_ADMISSION: Unable to write admission step summary.");
  });
});
