import { afterEach, describe, expect, it, vi } from "vitest";
const { createTokenBrokerClient, getBrokerToken, isTokenBrokerEnabled } = require("./token_broker.cjs");

const START = Date.parse("2026-10-30T12:00:00Z");
const jwt = exp => [JSON.stringify({ alg: "synthetic" }), JSON.stringify({ exp }), "synthetic-signature"].map(value => Buffer.from(value).toString("base64url")).join(".");
const OIDC = jwt(START / 1000 + 300);
const makeEnv = () => ({
  GH_AW_TOKEN_BROKER_URL: "https://broker.example.test/token",
  ACTIONS_ID_TOKEN_REQUEST_URL: "https://oidc.example.test/token?job=synthetic",
  ACTIONS_ID_TOKEN_REQUEST_TOKEN: "synthetic-bootstrap",
  GITHUB_RUN_ID: "123",
  GITHUB_RUN_ATTEMPT: "1",
  GITHUB_JOB: "agent",
  GITHUB_REPOSITORY: "acme/project",
});
const json = value => new Response(JSON.stringify(value));
const success = (token = "synthetic-credential", expires_at = "2026-10-30T12:05:00Z") => json({ token, expires_at });
function setup(overrides = {}) {
  const env = makeEnv();
  const fetchImpl = vi.fn(async (_url, init) => (init.method === "POST" ? success() : json({ value: OIDC })));
  const options = { env, fetchImpl, now: () => START, mask: vi.fn(), ...overrides };
  return { ...options, client: createTokenBrokerClient(options), options };
}
async function fails(promise, kind) {
  const error = await promise.then(
    () => {
      throw new Error("Expected rejection");
    },
    error => error
  );
  expect(error.code).toBe(`ERR_TOKEN_BROKER_${kind}`);
  expect(error.cause).toBeUndefined();
  for (const secret of [OIDC, "synthetic-bootstrap", "synthetic-credential", "broker.example.test", "oidc.example.test", "sensitive-detail"]) {
    expect(`${error.stack} ${JSON.stringify(error)}`).not.toContain(secret);
  }
}

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("token broker", () => {
  it("requires the compiler-owned top-level boolean for workflow opt-in, never inherited URL alone", () => {
    expect(isTokenBrokerEnabled({})).toBe(false);
    expect(isTokenBrokerEnabled(makeEnv())).toBe(false);
    expect(isTokenBrokerEnabled(Object.create({ GH_AW_TOKEN_BROKER_URL: "https://broker.example.test" }))).toBe(false);
    for (const config of [
      "",
      "not-json",
      "null",
      "true",
      "[]",
      "{}",
      '{"_token_broker":false}',
      '{"_token_broker":"true"}',
      '{"_token_broker":1}',
      '{"_token_broker":{"copilot-task":"true"}}',
      '{"assign_to_agent":{"_token_broker":{"copilot-task":true}}}',
    ]) {
      expect(isTokenBrokerEnabled({ ...makeEnv(), GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG: config })).toBe(false);
    }
    expect(isTokenBrokerEnabled({ GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG: '{"_token_broker":{"copilot-task":true}}' })).toBe(true);
    expect(isTokenBrokerEnabled({ ...makeEnv(), GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG: '{"_token_broker":{"copilot-task":true},"assign_to_agent":{}}' })).toBe(true);
  });

  it("fails closed on missing or empty URL even when the workflow marker is enabled", async () => {
    const marker = { GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG: '{"_token_broker":{"copilot-task":true}}' };
    for (const env of [marker, { ...marker, GH_AW_TOKEN_BROKER_URL: "" }, { ...marker, GH_AW_TOKEN_BROKER_URL: undefined }]) {
      expect(isTokenBrokerEnabled(env)).toBe(true);
      const fixture = setup({ env });
      await fails(fixture.client.getToken("copilot-request"), "CONFIGURATION");
      expect(fixture.fetchImpl).not.toHaveBeenCalled();
    }
  });

  it("permits explicitly requested low-level exchanges independently of the workflow marker", async () => {
    for (const config of [undefined, '{"_token_broker":false}', "invalid-json"]) {
      const fixture = setup({ env: { ...makeEnv(), GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG: config } });
      expect(await fixture.client.getToken("copilot-task")).toBe("synthetic-credential");
    }
    await fails(setup({ env: {} }).client.getToken("copilot-request"), "CONFIGURATION");
  });

  it.each(["copilot-request", "copilot-task"])("exchanges and masks %s without logging or exporting credentials", async tokenType => {
    const log = vi.spyOn(console, "log").mockImplementation(() => {});
    const fixture = setup();
    const before = { ...fixture.env };
    expect(await fixture.client.getToken(tokenType)).toBe("synthetic-credential");
    const [oidcUrl, oidcInit] = fixture.fetchImpl.mock.calls[0];
    expect(oidcUrl.searchParams.get("audience")).toBe(fixture.env.GH_AW_TOKEN_BROKER_URL);
    expect(oidcUrl.searchParams.get("job")).toBe("synthetic");
    expect(oidcInit).toMatchObject({ redirect: "manual", headers: { authorization: "bearer synthetic-bootstrap" } });
    const [brokerUrl, brokerInit] = fixture.fetchImpl.mock.calls[1];
    expect(brokerUrl).toBe(fixture.env.GH_AW_TOKEN_BROKER_URL);
    expect(brokerInit).toMatchObject({ method: "POST", redirect: "manual", headers: { "content-type": "application/json" } });
    expect(JSON.parse(brokerInit.body)).toEqual({ token_type: tokenType, oidc_token: OIDC });
    expect(fixture.mask.mock.calls).toEqual([[OIDC], ["synthetic-credential"]]);
    expect(log).not.toHaveBeenCalled();
    expect(fixture.env).toEqual(before);
    expect(Object.keys(fixture.client)).toEqual(["getToken"]);
  });

  it("deduplicates concurrent requests, caches per capability, and renews at the skew boundary", async () => {
    let time = START;
    const fixture = setup({ now: () => time });
    expect(await Promise.all(Array.from({ length: 10 }, () => fixture.client.getToken("copilot-request")))).toEqual(Array(10).fill("synthetic-credential"));
    expect(fixture.fetchImpl).toHaveBeenCalledTimes(2);
    await fixture.client.getToken("copilot-task");
    expect(fixture.fetchImpl).toHaveBeenCalledTimes(4);
    time += 269999;
    await fixture.client.getToken("copilot-request");
    expect(fixture.fetchImpl).toHaveBeenCalledTimes(4);
    time++;
    fixture.fetchImpl.mockImplementation(async (_url, init) => (init.method === "POST" ? success("synthetic-renewed", "2026-10-30T12:10:00Z") : json({ value: jwt(time / 1000 + 300) })));
    expect(await fixture.client.getToken("copilot-request")).toBe("synthetic-renewed");
    expect(fixture.fetchImpl).toHaveBeenCalledTimes(6);
  });

  it.each(["GITHUB_RUN_ID", "GITHUB_RUN_ATTEMPT", "GITHUB_JOB", "GITHUB_REPOSITORY", "GH_AW_TOKEN_BROKER_URL"])("isolates cache and pending requests by %s", async key => {
    const fixture = setup();
    const original = fixture.env[key];
    const first = fixture.client.getToken("copilot-request");
    fixture.env[key] = key === "GH_AW_TOKEN_BROKER_URL" ? "https://other.example.test/token" : "different";
    await Promise.all([first, fixture.client.getToken("copilot-request")]);
    expect(fixture.fetchImpl).toHaveBeenCalledTimes(4);
    fixture.env[key] = original;
    await fixture.client.getToken("copilot-request");
    expect(fixture.fetchImpl).toHaveBeenCalledTimes(4);
  });

  it("keeps explicitly created clients separate and reuses convenience options", async () => {
    const fixture = setup();
    await getBrokerToken("copilot-request", fixture.options);
    await getBrokerToken("copilot-request", fixture.options);
    expect(fixture.fetchImpl).toHaveBeenCalledTimes(2);
    await fixture.client.getToken("copilot-request");
    expect(fixture.fetchImpl).toHaveBeenCalledTimes(4);
  });

  it("uses a context-keyed default singleton and live Actions masking", async () => {
    const fixture = setup();
    for (const [key, value] of Object.entries(fixture.env)) vi.stubEnv(key, value);
    vi.stubGlobal("fetch", fixture.fetchImpl);
    vi.spyOn(Date, "now").mockReturnValue(START);
    const write = vi.spyOn(process.stdout, "write").mockImplementation(() => true);
    try {
      await getBrokerToken("copilot-request");
      await getBrokerToken("copilot-request");
      expect(fixture.fetchImpl).toHaveBeenCalledTimes(2);
      expect(write.mock.calls.map(([value]) => value).join("")).toContain(`::add-mask::${OIDC}`);
      expect(write.mock.calls.map(([value]) => value).join("")).toContain("::add-mask::synthetic-credential");
      vi.stubEnv("GITHUB_RUN_ATTEMPT", "2");
      await getBrokerToken("copilot-request");
      expect(fixture.fetchImpl).toHaveBeenCalledTimes(4);
    } finally {
      vi.unstubAllEnvs();
    }
  });

  it.each(["oidc", "broker"])("rejects automatically followed %s redirects even on HTTP 200", async stage => {
    const fixture = setup();
    const response = stage === "oidc" ? json({ value: OIDC }) : success();
    Object.defineProperty(response, "redirected", { value: true });
    if (stage === "broker") fixture.fetchImpl.mockResolvedValueOnce(json({ value: OIDC }));
    fixture.fetchImpl.mockResolvedValueOnce(response);
    await fails(fixture.client.getToken("copilot-request"), stage === "oidc" ? "OIDC" : "UNAVAILABLE");
  });

  it.each([
    "http://broker.example.test",
    "******broker.example.test",
    "https://broker.example.test/#",
    "https://broker.example.test/#fragment",
    " https://broker.example.test",
    "https://broker.example.test\n",
    "https://broker.example.test\\path",
    "not-a-url",
  ])("rejects unsafe broker URL %s before network access", async url => {
    const fixture = setup({ env: { ...makeEnv(), GH_AW_TOKEN_BROKER_URL: url } });
    await fails(fixture.client.getToken("copilot-request"), "CONFIGURATION");
    expect(fixture.fetchImpl).not.toHaveBeenCalled();
  });

  it.each([undefined, null, {}, [], { token: "synthetic-credential" }, { expires_at: "2026-10-30T12:05:00Z" }, { token: "synthetic-credential", expires_at: "2026-10-30T12:05:00Z", metadata: "sensitive-detail" }])(
    "rejects malformed or missing response fields %#",
    async value => {
      const fixture = setup();
      fixture.fetchImpl.mockResolvedValueOnce(json({ value: OIDC })).mockResolvedValueOnce(value === undefined ? new Response("") : json(value));
      await fails(fixture.client.getToken("copilot-request"), "MALFORMED_RESPONSE");
    }
  );

  it.each(["", " leading", "trailing ", "a b", "a\r\nb", "a\u0000b", "a\u007fb", "a\u0085b", "non-ascii-é", 123, null])("rejects unsafe credential %#", async token => {
    const fixture = setup();
    fixture.fetchImpl.mockResolvedValueOnce(json({ value: OIDC })).mockResolvedValueOnce(success(token));
    await fails(fixture.client.getToken("copilot-request"), "MALFORMED_RESPONSE");
  });

  it.each([
    "2026-10-30T12:00:00Z",
    "2026-10-30T12:00:30Z",
    "2026-10-30",
    "2026-10-30T12:05:00",
    "2026-02-29T12:05:00Z",
    "2026-11-31T12:05:00Z",
    "2026-13-01T12:05:00Z",
    "2026-10-00T12:05:00Z",
    "2026-10-30T24:05:00Z",
    "2026-10-30T12:60:00Z",
    "2026-10-30T12:05:60Z",
    "2026-10-30T12:05:00+24:00",
    "2026-10-30T12:05:00+00:60",
    123,
  ])("rejects expired, near-expiry or invalid RFC3339 timestamps %#", async expires => {
    const fixture = setup();
    fixture.fetchImpl.mockResolvedValueOnce(json({ value: OIDC })).mockResolvedValueOnce(success("synthetic-credential", expires));
    await fails(fixture.client.getToken("copilot-request"), "MALFORMED_RESPONSE");
  });

  it.each(["2026-10-30T13:05:00+01:00", "2026-10-30T11:05:00-01:00", "2026-10-30t12:05:00.123z", "2028-02-29T00:00:00Z"])("accepts valid RFC3339 expiry %s", async expires => {
    const fixture = setup();
    fixture.fetchImpl.mockResolvedValueOnce(json({ value: OIDC })).mockResolvedValueOnce(success("synthetic-credential", expires));
    expect(await fixture.client.getToken("copilot-request")).toBe("synthetic-credential");
  });

  it.each([400, 404, 422, 501, 401, 403, 302, 307, 429, 500])("sanitizes broker HTTP %s, without fallback", async status => {
    const fixture = setup();
    fixture.fetchImpl.mockResolvedValueOnce(json({ value: OIDC })).mockResolvedValueOnce(new Response("sensitive-detail", { status }));
    await fails(fixture.client.getToken("copilot-task"), [400, 404, 422, 501].includes(status) ? "UNSUPPORTED" : [401, 403].includes(status) ? "UNAUTHORIZED" : "UNAVAILABLE");
    expect(fixture.fetchImpl).toHaveBeenCalledTimes(2);
    await fixture.client.getToken("copilot-request");
    expect(fixture.fetchImpl).toHaveBeenCalledTimes(4);
    await fixture.client.getToken("copilot-task");
    expect(fixture.fetchImpl).toHaveBeenCalledTimes(6);
  });

  it.each(["ACTIONS_ID_TOKEN_REQUEST_URL", "ACTIONS_ID_TOKEN_REQUEST_TOKEN"])("rejects missing OIDC configuration %s", async key => {
    const env = makeEnv();
    delete env[key];
    const fixture = setup({ env });
    await fails(fixture.client.getToken("copilot-request"), "OIDC");
    expect(fixture.fetchImpl).not.toHaveBeenCalled();
  });

  it.each(["http://oidc.example.test", "******oidc.example.test", "https://oidc.example.test/#"])("rejects unsafe OIDC URL %s", async url => {
    const fixture = setup({ env: { ...makeEnv(), ACTIONS_ID_TOKEN_REQUEST_URL: url } });
    await fails(fixture.client.getToken("copilot-request"), "OIDC");
    expect(fixture.fetchImpl).not.toHaveBeenCalled();
  });

  it.each([{}, { value: "" }, { value: "invalid" }, { value: jwt(START / 1000) }, { value: jwt("9999999999") }, { value: jwt(null) }, { value: "e30.bm90LWpzb24.c2ln" }])("rejects missing, invalid or expired OIDC JWT %#", async value => {
    const fixture = setup();
    fixture.fetchImpl.mockResolvedValueOnce(json(value));
    await fails(fixture.client.getToken("copilot-request"), "OIDC");
    expect(fixture.fetchImpl).toHaveBeenCalledTimes(1);
  });

  it.each([302, 401, 500])("rejects OIDC HTTP %s without requesting broker", async status => {
    const fixture = setup();
    fixture.fetchImpl.mockResolvedValueOnce(new Response("sensitive-detail", { status }));
    await fails(fixture.client.getToken("copilot-request"), "OIDC");
    expect(fixture.fetchImpl).toHaveBeenCalledTimes(1);
  });

  it.each(["oidc", "broker"])("bounds %s response bodies and rejects invalid encoding", async stage => {
    for (const body of ["a".repeat(65537), new Uint8Array([0xff])]) {
      const fixture = setup();
      if (stage === "broker") fixture.fetchImpl.mockResolvedValueOnce(json({ value: OIDC }));
      fixture.fetchImpl.mockResolvedValueOnce(new Response(body));
      await fails(fixture.client.getToken("copilot-request"), stage === "oidc" ? "OIDC" : "MALFORMED_RESPONSE");
    }
  });

  it("rejects duplicate fields and invalid JSON", async () => {
    for (const body of ['{"token":"x","token":"y","expires_at":"2026-10-30T12:05:00Z"}', '{"token":', "sensitive-detail"]) {
      const fixture = setup();
      fixture.fetchImpl.mockResolvedValueOnce(json({ value: OIDC })).mockResolvedValueOnce(new Response(body));
      await fails(fixture.client.getToken("copilot-request"), "MALFORMED_RESPONSE");
    }
  });

  it.each(["oidc-fetch", "broker-fetch", "oidc-body", "broker-body"])("enforces finite deadline including stalled %s and allows subsequent attempts", async stage => {
    vi.useFakeTimers();
    const fixture = setup();
    if (stage.startsWith("broker")) fixture.fetchImpl.mockResolvedValueOnce(json({ value: OIDC }));
    if (stage.endsWith("fetch")) fixture.fetchImpl.mockImplementationOnce(() => new Promise(() => {}));
    else fixture.fetchImpl.mockResolvedValueOnce(new Response(new ReadableStream({ start() {} })));
    const result = fails(fixture.client.getToken("copilot-request"), "UNAVAILABLE");
    await vi.advanceTimersByTimeAsync(10000);
    await result;
    expect(fixture.fetchImpl.mock.calls[0][1].signal.aborted).toBe(true);
    expect(await fixture.client.getToken("copilot-request")).toBe("synthetic-credential");
  });

  it.each(["oidc", "broker", "mask"])("sanitizes raw exceptions from %s", async stage => {
    const fixture = setup(
      stage === "mask"
        ? {
            mask: () => {
              throw new Error("sensitive-detail");
            },
          }
        : {}
    );
    if (stage === "broker") fixture.fetchImpl.mockResolvedValueOnce(json({ value: OIDC }));
    if (stage !== "mask") fixture.fetchImpl.mockRejectedValueOnce(new Error("sensitive-detail"));
    await fails(fixture.client.getToken("copilot-request"), stage === "broker" ? "UNAVAILABLE" : "OIDC");
  });

  it("rejects invalid capabilities before network access", async () => {
    const fixture = setup();
    for (const type of ["", "a b", "copilot-request\r\n", "github", "copilot", "admission-oidc", "COPILOT_CONNECTION_TOKEN", null]) await fails(fixture.client.getToken(type), "UNSUPPORTED");
    expect(fixture.fetchImpl).not.toHaveBeenCalled();
  });
});
