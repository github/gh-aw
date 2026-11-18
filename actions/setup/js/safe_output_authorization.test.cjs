import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
const fs = require("node:fs");
const path = require("node:path");
const crypto = require("node:crypto");
const {
  VERSION,
  SafeOutputAuthorizationError,
  canonicalDigest,
  buildAuthorizationCaller,
  stableInvocationId,
  prepareInvocation,
  authorizePreparedInvocation,
  assertAuthorizationFresh,
  assertAuthorizationBatchFresh,
  recordAuthorizationOutcome,
} = require("./safe_output_authorization.cjs");

const clock = Date.parse("2026-11-18T07:00:00.000Z");
const caller = {
  host: "https://github.com",
  repository_id: "123",
  workflow_ref: "acme/project/.github/workflows/test.lock.yml@refs/heads/main",
  workflow_sha: "a".repeat(40),
  workflow_hash: "b".repeat(64),
  run_id: "90071992547409931234567890",
  run_attempt: "2",
};
const output = { type: "add_comment", name: "add_comment", executor: "manager", implementation_digest: "c".repeat(64) };
const requirement = { endpoint: "https://authority.example.test/api/safe-output-authorization", audience: "https://authority.example.test/gh-aw-safe-outputs", source: "global" };
const env = { ACTIONS_ID_TOKEN_REQUEST_URL: "https://oidc.example.test/token?job=bound", ACTIONS_ID_TOKEN_REQUEST_TOKEN: "private-bootstrap", GITHUB_TOKEN: "never-forward-github" };
const inputs = { body: "private-body-not-on-wire", secret_ref: "secrets.REVIEWED_SECRET" };
const makeInvocation = changes => prepareInvocation({ caller, output, normalizedInputs: inputs, batchDigest: canonicalDigest([inputs, inputs]), recordIndex: 0, ...changes });
const token = (changes = {}) => `fixture.${Buffer.from(JSON.stringify({ aud: requirement.audience, exp: clock / 1000 + 300, ...changes })).toString("base64url")}.signature`;
const oidc = changes => Response.json({ value: token(changes) });

// Protocol fixture only: the production service must verify GitHub OIDC and
// durably consume the logical claim before issuing a response.
function authority(request, changes = {}) {
  const envelope = JSON.parse(request.body);
  return {
    version: VERSION,
    decision: "allow",
    request_digest: envelope.request_digest,
    invocation_id: envelope.invocation.invocation_id,
    run_attempt: envelope.invocation.caller.run_attempt,
    policy_revision: "policy-v7",
    decision_id: "decision-fixture-1",
    issued_at: new Date(clock).toISOString(),
    expires_at: new Date(clock + 15000).toISOString(),
    ...changes,
  };
}

describe("generic fail-closed safe-output authorization", () => {
  let directory;
  let evidencePath;
  let core;
  beforeEach(() => {
    vi.stubEnv("GITHUB_AW_OTEL_TRACE_ID", "");
    directory = path.resolve(`.safe-output-authorization-test-${crypto.randomUUID()}`);
    fs.mkdirSync(directory);
    evidencePath = path.join(directory, "evidence.jsonl");
    core = { setSecret: vi.fn(), warning: vi.fn(), debug: vi.fn() };
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
    vi.unstubAllEnvs();
    fs.rmSync(directory, { recursive: true, force: true });
  });
  function options(changes = {}, decision = {}) {
    return {
      core,
      requirement,
      invocation: makeInvocation(),
      evidencePath,
      env,
      now: () => 0,
      wallNow: () => clock,
      fetchImpl: vi
        .fn()
        .mockImplementationOnce(() => oidc())
        .mockImplementationOnce((_, request) => Response.json(authority(request, decision))),
      ...changes,
    };
  }
  async function execute(opts) {
    const executor = vi.fn();
    try {
      const decision = await authorizePreparedInvocation(opts);
      if (decision.state !== "skipped_staged") executor();
      return { executor, decision };
    } catch (error) {
      expect(executor).not.toHaveBeenCalled();
      throw error;
    }
  }

  describe("OpenTelemetry", () => {
    let sender;
    let mirrors;
    beforeEach(() => {
      mirrors = [];
      vi.stubEnv("GITHUB_AW_OTEL_TRACE_ID", "a".repeat(32));
      vi.stubEnv("GITHUB_AW_OTEL_PARENT_SPAN_ID", "b".repeat(16));
      vi.stubEnv("GH_AW_OTLP_ENDPOINTS", JSON.stringify([{ url: "https://otel.example.test" }]));
      sender = require("./send_otlp_span.cjs");
      vi.spyOn(sender, "readJSONIfExists").mockReturnValue({});
      vi.spyOn(sender, "appendToOTLPJSONL").mockImplementation(payload => mirrors.push(payload));
      vi.spyOn(sender, "sendOTLPToAllEndpoints").mockResolvedValue([]);
    });
    afterEach(() => vi.unstubAllEnvs());

    it.each(["allow", "deny", "unavailable"])("exports correlated %s spans without authorization secrets", async decision => {
      const opts = options({}, { decision });
      if (decision === "unavailable")
        opts.fetchImpl
          .mockReset()
          .mockImplementationOnce(() => oidc())
          .mockRejectedValueOnce(new Error("private-transport-detail"));
      if (decision === "allow") await authorizePreparedInvocation(opts);
      else await expect(authorizePreparedInvocation(opts)).rejects.toBeInstanceOf(SafeOutputAuthorizationError);
      const spans = mirrors.map(payload => payload.resourceSpans[0].scopeSpans[0].spans[0]);
      expect(spans.map(span => span.name)).toEqual(["safe_output.authorization.oidc.run", "safe_output.authorization.authority.run", "safe_output.authorization.decision.run"]);
      for (const span of spans) expect(span).toMatchObject({ traceId: "a".repeat(32), parentSpanId: "b".repeat(16) });
      const final = spans.at(-1);
      expect(final.status.code).toBe(decision === "allow" ? 1 : 2);
      const attrs = Object.fromEntries(final.attributes.map(({ key, value }) => [key, Object.values(value)[0]]));
      expect(attrs).toMatchObject({
        "gh-aw.safe_output.gate.phase": "decision",
        "gh-aw.safe_output.gate.state": decision === "allow" ? "allowed" : decision === "deny" ? "denied" : "error",
        "gh-aw.safe_output.gate.reason_code": decision === "allow" ? "allowed" : decision === "deny" ? "denied" : "authority_unavailable",
        "gh-aw.safe_output.gate.executor": "manager",
        "gh-aw.safe_output.gate.binding": "invocation",
        "gh-aw.safe_output.gate.requirement_source": "global",
      });
      const serialized = JSON.stringify(mirrors);
      for (const privateValue of [
        requirement.endpoint,
        requirement.audience,
        env.ACTIONS_ID_TOKEN_REQUEST_TOKEN,
        env.GITHUB_TOKEN,
        token(),
        inputs.body,
        inputs.secret_ref,
        "private-transport-detail",
        "policy-v7",
        "decision-fixture-1",
        opts.invocation.invocation_id,
      ]) {
        expect(serialized).not.toContain(privateValue);
      }
      expect(sender.sendOTLPToAllEndpoints).toHaveBeenCalledTimes(3);
      expect(opts.fetchImpl).toHaveBeenCalledTimes(2);
    });

    it("does not await an exporter while authorization is live", async () => {
      sender.sendOTLPToAllEndpoints.mockImplementation(() => new Promise(() => {}));
      const authorization = await authorizePreparedInvocation(options());
      expect(assertAuthorizationFresh({ authorization })).toBe(authorization);
      expect(mirrors).toHaveLength(3);
    });

    it("still rejects a grant that expires while mirroring telemetry", async () => {
      let elapsed = 0;
      sender.appendToOTLPJSONL.mockImplementation(payload => {
        const name = payload.resourceSpans[0].scopeSpans[0].spans[0].name;
        if (name === "safe_output.authorization.decision.run") elapsed = 14000;
      });
      const authorization = await authorizePreparedInvocation(options({ now: () => elapsed }));
      expect(() => assertAuthorizationFresh({ authorization })).toThrow("invalid_validity");
    });

    it.each(["throw", "reject"])("keeps telemetry %s failures non-fatal and redacted", async failure => {
      vi.spyOn(require("./otlp.cjs"), "logSpan").mockImplementation(() => {
        const error = new Error("private-exporter-url-and-token");
        if (failure === "throw") throw error;
        return Promise.reject(error);
      });
      const authorization = await authorizePreparedInvocation(options());
      expect(assertAuthorizationFresh({ authorization })).toBe(authorization);
      expect(core.warning).toHaveBeenCalledWith("ERR_SAFE_OUTPUT_AUTHORIZATION: telemetry_failed");
      expect(JSON.stringify(core.warning.mock.calls)).not.toContain("private-exporter");
    });

    it.each(["staged", "unconfigured", "no-trace"])("does not initialize telemetry for %s execution", async mode => {
      if (mode === "no-trace") vi.stubEnv("GITHUB_AW_OTEL_TRACE_ID", "");
      await authorizePreparedInvocation(options(mode === "staged" ? { staged: true } : mode === "unconfigured" ? { requirement: undefined } : {}));
      expect(mirrors).toEqual([]);
      expect(sender.sendOTLPToAllEndpoints).not.toHaveBeenCalled();
    });
  });

  it.each(["allow", "deny", "unavailable"])("logs the %s decision lifecycle without authority or input secrets", async decision => {
    const opts = options({}, { decision });
    if (decision === "unavailable")
      opts.fetchImpl
        .mockReset()
        .mockImplementationOnce(() => oidc())
        .mockImplementationOnce(() => {
          throw new Error("private-response-body");
        });
    if (decision === "allow") {
      await authorizePreparedInvocation(opts);
      expect(core.debug).toHaveBeenCalledWith(expect.stringContaining("allowed invocation="));
    } else {
      await expect(authorizePreparedInvocation(opts)).rejects.toBeInstanceOf(SafeOutputAuthorizationError);
      expect(core.debug).toHaveBeenCalledWith(`Safe-output authorization: blocked reason=${decision === "deny" ? "denied" : "authority_unavailable"}`);
    }
    const messages = core.debug.mock.calls.flat().join("\n");
    expect(messages).toContain("prepared invocation=");
    expect(messages).toContain("requesting OIDC credential");
    expect(messages).toContain("requesting decision invocation=");
    for (const privateValue of [requirement.endpoint, requirement.audience, env.ACTIONS_ID_TOKEN_REQUEST_TOKEN, env.GITHUB_TOKEN, token(), inputs.body, inputs.secret_ref, "private-response-body"]) {
      expect(messages).not.toContain(privateValue);
    }
  });

  it("uses a stable canonical digest test vector and JSON number spelling", () => {
    expect(canonicalDigest({ b: 2, a: 1 })).toBe("43258cff783fe7036d8a43033f830adfc60ec037382473548ac742b888292777");
    expect(canonicalDigest({ n: 1.5, a: [true, null, "🦊"] })).toBe(canonicalDigest({ a: [true, null, "🦊"], n: 1.5 }));
    expect(canonicalDigest(-0)).toBe(canonicalDigest(0));
  });
  it.each([undefined, NaN, Infinity, new Date(), new Map(), { value: undefined }, "\ud800", Array(1)])("rejects non-JSON canonical input %s", value => {
    expect(() => canonicalDigest(value)).toThrow(SafeOutputAuthorizationError);
  });
  it("rejects cycles, excessive nesting and getters without executing them", () => {
    const cyclic = {};
    cyclic.self = cyclic;
    const getter = vi.fn(() => "secret");
    const object = Object.defineProperty({}, "secret", { enumerable: true, get: getter });
    expect(() => canonicalDigest(cyclic)).toThrow();
    expect(() => canonicalDigest(object)).toThrow();
    expect(getter).not.toHaveBeenCalled();
    expect(() => canonicalDigest(Array.from({ length: 34 }).reduce(acc => [acc], []))).toThrow();
  });
  it("binds reviewed implementation, normalized input digest and scope without raw inputs", () => {
    const invocation = makeInvocation({
      scope: { binding: "invocation", refs: ["repository:123", "artifact:sha256:abc"] },
      context: { claim_id: "claim-123", delivery_id: "delivery-2", policy_digest: "d".repeat(64), evidence_refs: ["review:123/456"] },
    });
    expect(invocation.input_digest).toBe(canonicalDigest(inputs));
    expect(invocation.scope.binding).toBe("invocation");
    expect(invocation.output.implementation_digest).toBe(output.implementation_digest);
    expect(JSON.stringify(invocation)).not.toContain(inputs.body);
    expect(Object.isFrozen(invocation)).toBe(true);
    expect(Object.isFrozen(invocation.output)).toBe(true);
    expect(() => makeInvocation({ scope: { binding: "exact_undocumented", refs: [] } })).toThrow();
    expect(() => makeInvocation({ scope: { binding: "invocation", refs: [], body: "leak" } })).toThrow();
    expect(() => makeInvocation({ context: { body: "leak" } })).toThrow();
  });
  it("distinguishes identical legitimate records but preserves identity across reruns and revisions", () => {
    const first = makeInvocation();
    const second = makeInvocation({ recordIndex: 1 });
    const rerun = makeInvocation({ caller: { ...caller, run_attempt: "3" }, output: { ...output, implementation_digest: "e".repeat(64) } });
    const changedBatch = makeInvocation({ batchDigest: "f".repeat(64) });
    expect(first.invocation_id).not.toBe(second.invocation_id);
    expect(first.invocation_id).toBe(rerun.invocation_id);
    expect(first.invocation_id).not.toBe(changedBatch.invocation_id);
    expect(first.request_id).not.toBe(rerun.request_id);
    expect(canonicalDigest(first)).not.toBe(canonicalDigest(rerun));
    expect(stableInvocationId({ caller, output, batchDigest: first.batch.digest, recordIndex: 0 })).toBe(first.invocation_id);
  });
  it("builds the caller only from executor context", () => {
    expect(
      buildAuthorizationCaller({
        GITHUB_SERVER_URL: caller.host,
        GITHUB_REPOSITORY_ID: caller.repository_id,
        GITHUB_WORKFLOW_REF: caller.workflow_ref,
        GITHUB_WORKFLOW_SHA: caller.workflow_sha,
        GH_AW_AUTHORIZATION_WORKFLOW_HASH: caller.workflow_hash,
        GITHUB_RUN_ID: caller.run_id,
        GITHUB_RUN_ATTEMPT: caller.run_attempt,
      })
    ).toEqual(caller);
    expect(() => buildAuthorizationCaller({})).toThrow("invalid_preparation");
    expect(() => makeInvocation({ caller: { ...caller, run_attempt: "" } })).toThrow();
  });
  it("skips staged before configuration, caller, OIDC, authority, claims or author code", async () => {
    const fetchImpl = vi.fn();
    const { executor, decision } = await execute({ core, staged: true, requirement: { endpoint: "invalid" }, invocation: undefined, env: {}, fetchImpl });
    expect(decision).toEqual({ state: "skipped_staged", reason_code: "skipped_staged" });
    expect(fetchImpl).not.toHaveBeenCalled();
    expect(executor).not.toHaveBeenCalled();
    expect(core.setSecret).not.toHaveBeenCalled();
    expect(core.debug.mock.calls).toEqual([["Safe-output authorization: skipped_staged"]]);
    expect(fs.existsSync(evidencePath)).toBe(false);
  });
  it("does not alter an unconfigured execution path", async () => {
    const opts = options({ requirement: undefined, invocation: undefined });
    expect((await execute(opts)).executor).toHaveBeenCalledOnce();
    expect(opts.fetchImpl).not.toHaveBeenCalled();
  });
  it("resolves compiler-owned repository authorization from managed runtime variables", async () => {
    const opts = options({
      requirement: { from_environment: true, source: "global" },
      env: { ...env, GH_AW_DEFAULT_SAFE_OUTPUT_AUTHORIZATION_URL: requirement.endpoint, GH_AW_DEFAULT_SAFE_OUTPUT_AUTHORIZATION_AUDIENCE: requirement.audience },
    });
    const authorization = await authorizePreparedInvocation(opts);
    expect(authorization).toMatchObject({ state: "allowed", requirement_source: "global" });
    expect(String(opts.fetchImpl.mock.calls[0][0])).toContain(`audience=${encodeURIComponent(requirement.audience)}`);
    expect(opts.fetchImpl.mock.calls[1][0]).toBe(requirement.endpoint);
    expect(assertAuthorizationFresh({ authorization })).toBe(authorization);
  });
  it.each(["missing", "missing-audience", "insecure", "expression", "mixed-config"])("fails closed for %s managed authority without acquiring credentials", async kind => {
    const opts = options({
      requirement: { from_environment: true, source: "global", ...(kind === "mixed-config" ? { endpoint: requirement.endpoint, audience: requirement.audience } : {}) },
      env: {
        ...env,
        GH_AW_DEFAULT_SAFE_OUTPUT_AUTHORIZATION_URL: kind === "missing" ? "" : kind === "insecure" ? "http://insecure.example" : kind === "expression" ? "${{ vars.ENDPOINT }}" : requirement.endpoint,
        GH_AW_DEFAULT_SAFE_OUTPUT_AUTHORIZATION_AUDIENCE: kind === "missing-audience" ? "" : requirement.audience,
      },
    });
    await expect(execute(opts)).rejects.toThrow("invalid_configuration");
    expect(opts.fetchImpl).not.toHaveBeenCalled();
    expect(core.setSecret).not.toHaveBeenCalled();
  });
  it("skips a staged managed authority before resolving missing variables", async () => {
    const opts = options({ staged: true, requirement: { from_environment: true, source: "global" } });
    expect(await authorizePreparedInvocation(opts)).toMatchObject({ state: "skipped_staged" });
    expect(opts.fetchImpl).not.toHaveBeenCalled();
  });
  it("sends a bounded OIDC-only HTTPS POST and durably persists a minimized decision first", async () => {
    const opts = options();
    const { decision, executor } = await execute(opts);
    expect(executor).toHaveBeenCalledOnce();
    expect(decision).toMatchObject({ state: "allowed", reason_code: "allowed", policy_revision: "policy-v7", requirement_source: "global" });
    expect(opts.fetchImpl).toHaveBeenCalledTimes(2);
    const [oidcURL, acquisition] = opts.fetchImpl.mock.calls[0];
    expect(oidcURL.searchParams.get("audience")).toBe(requirement.audience);
    expect(acquisition.redirect).toBe("manual");
    const [url, post] = opts.fetchImpl.mock.calls[1];
    expect(url).toBe(requirement.endpoint);
    expect(post).toMatchObject({ method: "POST", redirect: "manual", headers: { authorization: `bearer ${token()}`, "cache-control": "no-store" } });
    expect(core.setSecret).toHaveBeenCalledWith(token());
    expect(core.setSecret).toHaveBeenCalledWith(env.ACTIONS_ID_TOKEN_REQUEST_TOKEN);
    expect(post.body).not.toContain(inputs.body);
    expect(post.body).not.toContain("never-forward");
    const wire = JSON.parse(post.body);
    expect(wire.request_digest).toBe(canonicalDigest(wire.invocation));
    const persisted = fs.readFileSync(evidencePath, "utf8");
    expect(JSON.parse(persisted)).toEqual(decision);
    for (const secret of [inputs.body, token(), requirement.endpoint, env.ACTIONS_ID_TOKEN_REQUEST_TOKEN]) expect(persisted).not.toContain(secret);
    expect(fs.statSync(evidencePath).mode & 0o777).toBe(0o600);
  });
  it.each([
    ["endpoint", "http://authority.test/api"],
    ["endpoint", "https://user:secret@authority.test/api"],
    ["endpoint", "https://authority.test/api?secret=value"],
    ["endpoint", "https://authority.test/api#secret"],
    ["endpoint", "https://authority.test\\@attacker.test/api"],
    ["audience", "bad\nvalue"],
    ["source", "payload"],
  ])("blocks invalid requirement %s without OIDC", async (key, value) => {
    const opts = options({ requirement: { ...requirement, [key]: value } });
    await expect(execute(opts)).rejects.toThrow("invalid_configuration");
    expect(opts.fetchImpl).not.toHaveBeenCalled();
  });
  it("does not accept fail-open options or payload-created prepared envelopes", async () => {
    await expect(execute(options({ requirement: { ...requirement, onOutage: "ignore" } }))).rejects.toThrow("invalid_configuration");
    await expect(execute(options({ invocation: { ...makeInvocation() } }))).rejects.toThrow("invalid_preparation");
  });
  it.each(["ACTIONS_ID_TOKEN_REQUEST_URL", "ACTIONS_ID_TOKEN_REQUEST_TOKEN"])("blocks missing %s without author code", async key => {
    const opts = options({ env: { ...env, [key]: "" } });
    await expect(execute(opts)).rejects.toThrow("missing_credentials");
    expect(opts.fetchImpl).not.toHaveBeenCalled();
  });
  it.each([{ exp: clock / 1000 }, { aud: "wrong-audience" }, { exp: "future" }])("blocks unusable OIDC %j", async changes => {
    const opts = options({ fetchImpl: vi.fn().mockImplementationOnce(() => oidc(changes)) });
    await expect(execute(opts)).rejects.toThrow("credential_error");
    expect(opts.fetchImpl).toHaveBeenCalledOnce();
  });
  it.each([301, 302, 307, 308, 204, 403, 429, 500, 503])("blocks HTTP %s, never redirects/retries or enters the executor", async status => {
    const opts = options({
      fetchImpl: vi
        .fn()
        .mockImplementationOnce(() => oidc())
        .mockImplementationOnce(() => new Response(status === 204 ? null : "private-response", { status })),
    });
    await expect(execute(opts)).rejects.toThrow("authority_unavailable");
    expect(opts.fetchImpl).toHaveBeenCalledTimes(2);
    expect(fs.readFileSync(evidencePath, "utf8")).not.toContain("private-response");
  });
  it.each([
    [{ decision: "deny" }, "denied"],
    [{ request_digest: "0".repeat(64) }, "binding_mismatch"],
    [{ invocation_id: "0".repeat(64) }, "binding_mismatch"],
    [{ run_attempt: "3" }, "binding_mismatch"],
    [{ version: "future" }, "invalid_response"],
    [{ policy_revision: "" }, "invalid_response"],
    [{ policy_revision: "private response\n" }, "invalid_response"],
    [{ decision_id: "" }, "invalid_response"],
    [{ expires_at: new Date(clock - 1).toISOString() }, "invalid_validity"],
    [{ expires_at: new Date(clock + 1999).toISOString() }, "invalid_validity"],
    [{ expires_at: new Date(clock + 30001).toISOString() }, "invalid_validity"],
    [{ issued_at: new Date(clock + 1).toISOString() }, "invalid_validity"],
    [{ issued_at: "2026-11-18T07:00:00Z" }, "invalid_validity"],
    [{ expires_at: "2026-02-30T07:00:00.000Z" }, "invalid_validity"],
    [{ diagnostic: "private-response" }, "invalid_response"],
  ])("rejects invalid decision %j without entering the executor", async (changes, reason) => {
    const opts = options({}, changes);
    await expect(execute(opts)).rejects.toMatchObject({ reason_code: reason, authorization: { state: reason === "denied" ? "denied" : "error" } });
    expect(fs.readFileSync(evidencePath, "utf8")).not.toContain("private-response");
  });
  it("preserves fully validated deny decision metadata in returned and durable evidence", async () => {
    const opts = options({}, { decision: "deny" });
    const metadata = {
      state: "denied",
      reason_code: "denied",
      decision_id: "decision-fixture-1",
      policy_revision: "policy-v7",
      issued_at: new Date(clock).toISOString(),
      expires_at: new Date(clock + 15000).toISOString(),
    };
    await expect(execute(opts)).rejects.toMatchObject({ reason_code: "denied", authorization: metadata });
    expect(JSON.parse(fs.readFileSync(evidencePath, "utf8"))).toMatchObject(metadata);
  });
  it.each([{ request_digest: "0".repeat(64) }, { policy_revision: "private\nresponse" }, { expires_at: new Date(clock - 1).toISOString() }])("never preserves decision metadata from an invalid deny response %j", async changes => {
    const opts = options({}, { decision: "deny", ...changes });
    await expect(execute(opts)).rejects.toMatchObject({
      authorization: expect.not.objectContaining({ decision_id: "decision-fixture-1" }),
    });
    const evidence = JSON.parse(fs.readFileSync(evidencePath, "utf8"));
    for (const key of ["decision_id", "policy_revision", "issued_at", "expires_at"]) expect(evidence).not.toHaveProperty(key);
  });
  it.each([2000, 30000])("permits validity boundary %s ms", async validity => {
    expect((await authorizePreparedInvocation(options({}, { expires_at: new Date(clock + validity).toISOString() }))).state).toBe("allowed");
  });
  it.each(["duplicate", "oversize", "content-type", "empty", "utf8"])("rejects malformed or oversized response %s", async kind => {
    const opts = options({
      fetchImpl: vi
        .fn()
        .mockImplementationOnce(() => oidc())
        .mockImplementationOnce((_, request) => {
          const valid = JSON.stringify(authority(request));
          if (kind === "duplicate") return new Response(valid.replace('"decision":"allow"', '"decision":"allow","\\u0064ecision":"allow"'), { headers: { "content-type": "application/json" } });
          if (kind === "oversize") return new Response(" ".repeat(8193), { headers: { "content-type": "application/json" } });
          if (kind === "content-type") return new Response(valid, { headers: { "content-type": "text/plain" } });
          if (kind === "utf8") return new Response(new Uint8Array([0xff]), { headers: { "content-type": "application/json" } });
          return Response.json({});
        }),
    });
    await expect(execute(opts)).rejects.toThrow("invalid_response");
  });
  it("does not retry or blindly restart after response loss", async () => {
    const opts = options({
      fetchImpl: vi
        .fn()
        .mockImplementationOnce(() => oidc())
        .mockRejectedValueOnce(new Error("secret transport detail")),
    });
    await expect(execute(opts)).rejects.toThrow("authority_unavailable");
    await expect(execute(opts)).rejects.toThrow("already_requested");
    expect(opts.fetchImpl).toHaveBeenCalledTimes(2);
    expect(fs.readFileSync(evidencePath, "utf8")).not.toContain("secret transport detail");
  });
  it("times out even if a transport ignores abort, with no retry", async () => {
    vi.useFakeTimers();
    const opts = options({
      fetchImpl: vi
        .fn()
        .mockImplementationOnce(() => oidc())
        .mockImplementationOnce(() => new Promise(() => {})),
    });
    const promise = expect(execute(opts)).rejects.toThrow("timeout");
    await vi.advanceTimersByTimeAsync(10000);
    await promise;
    expect(opts.fetchImpl).toHaveBeenCalledTimes(2);
  });
  it("counts credential time against the monotonic total deadline", async () => {
    let elapsed = 0;
    const opts = options({
      now: () => elapsed,
      fetchImpl: vi.fn().mockImplementationOnce(() => {
        elapsed = 20001;
        return oidc();
      }),
    });
    await expect(execute(opts)).rejects.toThrow("timeout");
    expect(opts.fetchImpl).toHaveBeenCalledOnce();
  });
  it("refuses to release expired permission after slow evidence persistence or UTC rollback", async () => {
    let elapsed = 0;
    const opts = options({ now: () => elapsed, wallNow: () => clock - elapsed });
    const original = fs.fsyncSync;
    vi.spyOn(fs, "fsyncSync").mockImplementation(fd => {
      original(fd);
      elapsed = 14000;
    });
    await expect(execute(opts)).rejects.toThrow("invalid_validity");
  });
  it("rechecks original authorization freshness at delayed batch entry using monotonic time", async () => {
    let elapsed = 0;
    const first = await authorizePreparedInvocation(options({ now: () => elapsed, wallNow: () => clock - elapsed }));
    const second = await authorizePreparedInvocation(options({ now: () => elapsed, wallNow: () => clock - elapsed, invocation: makeInvocation({ recordIndex: 1 }) }));
    expect(assertAuthorizationFresh({ authorization: first })).toBe(first);
    expect(assertAuthorizationBatchFresh({ authorizations: [first, second] })).toEqual([first, second]);
    elapsed = 13000;
    expect(assertAuthorizationFresh({ authorization: first })).toBe(first);
    elapsed = 13001;
    const executor = vi.fn();
    expect(() => {
      assertAuthorizationBatchFresh({ authorizations: [first, second] });
      executor();
    }).toThrow("invalid_validity");
    expect(executor).not.toHaveBeenCalled();
    const records = fs.readFileSync(evidencePath, "utf8").trim().split("\n").map(JSON.parse);
    expect(records.at(-1)).toMatchObject({ state: "error", reason_code: "invalid_validity" });
  });
  it("does not accept forged or deserialized decision projections at batch release", async () => {
    const authorization = await authorizePreparedInvocation(options());
    expect(() => assertAuthorizationFresh({ authorization: { ...authorization } })).toThrow("invalid_preparation");
    expect(() => assertAuthorizationBatchFresh({ authorizations: [{ state: "skipped_staged" }] })).toThrow("invalid_preparation");
  });
  it("blocks execution when evidence cannot be created or fsynced", async () => {
    await expect(execute(options({ evidencePath: path.join(directory, "missing", "evidence") }))).rejects.toThrow("evidence_failed");
    const opts = options();
    vi.spyOn(fs, "fsyncSync").mockImplementation(() => {
      throw new Error("private-path");
    });
    await expect(execute(opts)).rejects.toThrow("evidence_failed");
  });
  it("rejects absent evidence destination before consuming authority permission", async () => {
    const opts = options({ evidencePath: undefined });
    await expect(execute(opts)).rejects.toThrow("evidence_failed");
    expect(opts.fetchImpl).not.toHaveBeenCalled();
  });
  it("fails closed if OIDC token masking is unavailable", async () => {
    const opts = options({ core: {} });
    await expect(execute(opts)).rejects.toThrow("missing_credentials");
    expect(opts.fetchImpl).not.toHaveBeenCalled();
  });
  it.each(["http://oidc.test/token", "https://user:secret@oidc.test/token", "https://oidc.test/token#fragment", "https://oidc.test\\@attacker.test/token"])("rejects unsafe OIDC endpoint %s", async endpoint => {
    const opts = options({ env: { ...env, ACTIONS_ID_TOKEN_REQUEST_URL: endpoint } });
    await expect(execute(opts)).rejects.toThrow("credential_error");
    expect(opts.fetchImpl).not.toHaveBeenCalled();
  });
  it("rejects redirected OIDC without forwarding bootstrap credentials elsewhere", async () => {
    const opts = options({ fetchImpl: vi.fn().mockResolvedValue(new Response("withheld", { status: 307, headers: { location: "https://attacker.test" } })) });
    await expect(execute(opts)).rejects.toThrow("credential_error");
    expect(opts.fetchImpl).toHaveBeenCalledOnce();
  });
  it("bounds slow streamed response bodies, not just receipt of response headers", async () => {
    vi.useFakeTimers();
    const opts = options({
      fetchImpl: vi
        .fn()
        .mockImplementationOnce(() => oidc())
        .mockResolvedValueOnce(new Response(new ReadableStream({ start() {} }), { headers: { "content-type": "application/json" } })),
    });
    const promise = expect(execute(opts)).rejects.toThrow("timeout");
    await vi.advanceTimersByTimeAsync(10000);
    await promise;
    expect(opts.fetchImpl).toHaveBeenCalledTimes(2);
  });
  it("rejects a request exceeding the 64 KiB closed envelope bound", () => {
    const refs = Array.from({ length: 64 }, (_, index) => `resource:${index}:${"a".repeat(490)}`);
    expect(() => makeInvocation({ scope: { binding: "invocation", refs }, context: { evidence_refs: refs } })).toThrow("invalid_preparation");
  });
  it("does not follow a symlink evidence file", async () => {
    const target = path.join(directory, "target");
    fs.writeFileSync(target, "untouched");
    fs.symlinkSync(target, evidencePath);
    await expect(execute(options())).rejects.toThrow("evidence_failed");
    expect(fs.readFileSync(target, "utf8")).toBe("untouched");
  });
  it("persists post-effect status separately, without arbitrary result fields", async () => {
    const authorization = await authorizePreparedInvocation(options());
    expect(recordAuthorizationOutcome({ evidencePath, authorization: { ...authorization, responseBody: "private-effect-body" }, operationStatus: "partial", core })).toEqual({ evidenceRecorded: true });
    const records = fs.readFileSync(evidencePath, "utf8").trim().split("\n").map(JSON.parse);
    expect(records[1]).toEqual({ authorization, operation_status: "partial" });
    expect(JSON.stringify(records)).not.toContain("private-effect-body");
  });
  it("post-effect evidence failure preserves actual outcome and cannot request replay", async () => {
    const authorization = await authorizePreparedInvocation(options());
    const result = { status: "indeterminate", authorization };
    expect(recordAuthorizationOutcome({ evidencePath: directory, authorization, operationStatus: result.status, core })).toEqual({ evidenceRecorded: false, reason_code: "post_effect_evidence_failed" });
    expect(result.status).toBe("indeterminate");
    expect(core.warning).toHaveBeenCalledExactlyOnceWith("ERR_SAFE_OUTPUT_AUTHORIZATION: post_effect_evidence_failed");
  });
  it("lets a fixture authority consume a logical claim only once across attempts", async () => {
    const claims = new Set();
    const fetchImpl = vi.fn(async (_url, request) => {
      if (request.method === "GET") return oidc();
      const invocation = JSON.parse(request.body).invocation;
      const duplicate = claims.has(invocation.invocation_id);
      claims.add(invocation.invocation_id);
      return Response.json(authority(request, { decision: duplicate ? "deny" : "allow" }));
    });
    const opts = options({ fetchImpl });
    expect((await execute(opts)).executor).toHaveBeenCalledOnce();
    await expect(execute({ ...opts, invocation: makeInvocation({ caller: { ...caller, run_attempt: "3" } }) })).rejects.toThrow("denied");
    expect((await execute({ ...opts, invocation: makeInvocation({ recordIndex: 1 }) })).executor).toHaveBeenCalledOnce();
  });
});
