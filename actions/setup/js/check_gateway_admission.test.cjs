import { afterEach, describe, expect, it, vi } from "vitest";
import { createHash } from "node:crypto";
import { once } from "node:events";
import { createServer } from "node:http";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { main, verifyGatewayAdmission } = require("./check_gateway_admission.cjs");
const revision = "a".repeat(40);
const frontmatterHash = "0123456789abcdef".repeat(4);

function hash(bytes) {
  return createHash("sha256").update(bytes).digest("hex");
}

function fixture(compiledHash = frontmatterHash) {
  const env = {
    GH_AW_GATEWAY_GET_TOKEN_URL: "https://gateway.example/getToken",
    GH_AW_GATEWAY_OIDC_AUDIENCE: "https://gateway.example",
    GH_AW_GATEWAY_REPOSITORY: "owner/repository",
    GH_AW_GATEWAY_REPOSITORY_ID: "42",
    GH_AW_GATEWAY_OWNER_ID: "7",
    GH_AW_GATEWAY_WORKFLOW_PATH: ".github/workflows/version-check.lock.yml",
    GH_AW_GATEWAY_WORKFLOW_REF: "owner/repository/.github/workflows/version-check.lock.yml@refs/heads/main",
    GH_AW_GATEWAY_WORKFLOW_SHA: revision,
    GH_AW_GATEWAY_WORKFLOW_HASH: compiledHash,
    GH_AW_GATEWAY_RUN_ID: "101",
    GH_AW_GATEWAY_RUN_ATTEMPT: "2",
    GH_AW_GATEWAY_ACTOR_ID: "99",
    GH_AW_GATEWAY_REF: "refs/heads/main",
    GH_AW_GATEWAY_EVENT_NAME: "repository_dispatch",
    GH_AW_GATEWAY_EVENT_TYPE: `gh-aw-webhook-${hash("version-check").slice(0, 24)}`,
    GITHUB_API_URL: "https://api.github.com",
    // Neither the checked-out revision nor payload-owned data is an authority.
    GITHUB_SHA: "b".repeat(40),
    GITHUB_WORKSPACE: "/untrusted-checkout",
    GITHUB_EVENT_PATH: "/untrusted-payload",
  };
  const claims = {
    repository: env.GH_AW_GATEWAY_REPOSITORY,
    repository_id: env.GH_AW_GATEWAY_REPOSITORY_ID,
    repository_owner_id: env.GH_AW_GATEWAY_OWNER_ID,
    workflow_ref: env.GH_AW_GATEWAY_WORKFLOW_REF,
    workflow_sha: revision,
    run_id: env.GH_AW_GATEWAY_RUN_ID,
    run_attempt: env.GH_AW_GATEWAY_RUN_ATTEMPT,
    actor_id: env.GH_AW_GATEWAY_ACTOR_ID,
    ref: env.GH_AW_GATEWAY_REF,
    event_name: env.GH_AW_GATEWAY_EVENT_NAME,
    check_run_id: "123",
    exp: Math.floor(Date.now() / 1000) + 300,
    nbf: Math.floor(Date.now() / 1000) - 30,
  };
  const core = {
    getInput: vi.fn(() => {
      throw new Error("Repository credentials must not be read for admission.");
    }),
    getIDToken: vi.fn(async audience => `${Buffer.from('{"alg":"RS256"}').toString("base64url")}.${Buffer.from(JSON.stringify({ ...claims, aud: audience })).toString("base64url")}.signature`),
    setSecret: vi.fn(),
    setOutput: vi.fn(),
    setFailed: vi.fn(),
    info: vi.fn(),
  };
  const fetchImpl = vi.fn(async (url, options) => {
    if (url === env.GH_AW_GATEWAY_GET_TOKEN_URL) return Response.json({ ...JSON.parse(options.body), allowed: true });
    throw new Error("unexpected credential-bearing request: github-secret");
  });
  return { env, core, claims, fetchImpl, run: () => verifyGatewayAdmission(core, env, fetchImpl) };
}

afterEach(() => {
  vi.unstubAllEnvs();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("hash-bound gateway workflow admission", () => {
  it("sends the current trace and encoded job without extending the closed admission binding", async () => {
    const f = fixture();
    Object.assign(f.env, { GITHUB_JOB: "gateway/admission", GITHUB_AW_OTEL_TRACE_ID: "a".repeat(32), GITHUB_AW_OTEL_PARENT_SPAN_ID: "b".repeat(16) });
    await f.run();
    expect(f.fetchImpl.mock.calls[0][1].headers).toMatchObject({
      traceparent: `00-${"a".repeat(32)}-${"b".repeat(16)}-01`,
      baggage: "cicd.pipeline.task.name=gateway%2Fadmission",
    });
    expect(JSON.parse(f.fetchImpl.mock.calls[0][1].body)).not.toHaveProperty("job_id");
  });

  it("passes the compiler header hash unchanged to OIDC and the closed POST without reading repository credentials or content", async () => {
    const f = fixture();
    await expect(f.run()).resolves.toEqual({ verified: true, workflow_hash: frontmatterHash });
    const audience = `https://gateway.example/gh-aw/gateway/v1/workflow/sha256/${frontmatterHash}`;
    expect(f.core.getIDToken).toHaveBeenCalledExactlyOnceWith(audience);
    expect(f.fetchImpl).toHaveBeenCalledOnce();
    expect(f.core.getInput).not.toHaveBeenCalled();
    for (const [, request] of f.fetchImpl.mock.calls) {
      expect(request.redirect).toBe("error");
      expect(request.cache).toBe("no-store");
      expect(request.signal).toBeInstanceOf(AbortSignal);
    }
    const [endpoint, request] = f.fetchImpl.mock.calls[0];
    expect(endpoint).toBe(f.env.GH_AW_GATEWAY_GET_TOKEN_URL);
    expect(request.method).toBe("POST");
    const token = await f.core.getIDToken.mock.results[0].value;
    expect(request.headers.Authorization).toBe(`Bearer ${token}`);
    expect(f.core.setSecret).toHaveBeenCalledExactlyOnceWith(token);
    expect(request.body).not.toContain("github-secret");
    expect(request.body).not.toContain(token);
    expect(JSON.parse(request.body)).toEqual({
      schema_version: 1,
      repository: "owner/repository",
      repository_id: "42",
      repository_owner_id: "7",
      workflow_path: ".github/workflows/version-check.lock.yml",
      workflow_ref: f.env.GH_AW_GATEWAY_WORKFLOW_REF,
      workflow_sha: revision,
      run_id: "101",
      run_attempt: "2",
      actor_id: "99",
      ref: "refs/heads/main",
      event_name: "repository_dispatch",
      event_type: f.env.GH_AW_GATEWAY_EVENT_TYPE,
      workflow_hash: frontmatterHash,
      check_run_id: "123",
    });
  });

  it.each(["0".repeat(64), "f".repeat(64)])("uses the supplied configuration hash rather than the workflow commit SHA", async compiledHash => {
    const f = fixture(compiledHash);
    const result = await f.run();
    expect(result.workflow_hash).toBe(compiledHash);
    expect(result.workflow_hash).not.toBe(revision);
    expect(f.core.getIDToken).toHaveBeenCalledWith(`https://gateway.example/gh-aw/gateway/v1/workflow/sha256/${compiledHash}`);
    expect(JSON.parse(f.fetchImpl.mock.calls[0][1].body).workflow_hash).toBe(compiledHash);
  });

  it("supports a path-qualified audience without consulting a GitHub API or checkout", async () => {
    const f = fixture();
    f.env.GITHUB_API_URL = "http://untrusted.invalid/api/v3";
    f.env.GH_AW_GATEWAY_OIDC_AUDIENCE = "https://gateway.example/tenant";
    await f.run();
    expect(f.fetchImpl).toHaveBeenCalledOnce();
    expect(f.fetchImpl.mock.calls[0][0]).toBe(f.env.GH_AW_GATEWAY_GET_TOKEN_URL);
    expect(f.core.getIDToken).toHaveBeenCalledWith(`https://gateway.example/tenant/gh-aw/gateway/v1/workflow/sha256/${frontmatterHash}`);
  });

  it.each([
    ["GH_AW_GATEWAY_GET_TOKEN_URL", ""],
    ["GH_AW_GATEWAY_GET_TOKEN_URL", "http://gateway.example/getToken"],
    ["GH_AW_GATEWAY_GET_TOKEN_URL", "https://user:password@gateway.example/getToken"],
    ["GH_AW_GATEWAY_GET_TOKEN_URL", "https://gateway.example/getToken?"],
    ["GH_AW_GATEWAY_GET_TOKEN_URL", "https://gateway.example/getToken#"],
    ["GH_AW_GATEWAY_GET_TOKEN_URL", "https://GATEWAY.example/getToken"],
    ["GH_AW_GATEWAY_GET_TOKEN_URL", "https://gateway.example:443/getToken"],
    ["GH_AW_GATEWAY_OIDC_AUDIENCE", "https://gateway.example/"],
    ["GH_AW_GATEWAY_OIDC_AUDIENCE", "https://gateway.example/tenant/"],
    ["GH_AW_GATEWAY_OIDC_AUDIENCE", "https://gateway.example/tenant/../other"],
    ["GH_AW_GATEWAY_REPOSITORY_ID", "042"],
    ["GH_AW_GATEWAY_OWNER_ID", "0"],
    ["GH_AW_GATEWAY_RUN_ID", ""],
    ["GH_AW_GATEWAY_RUN_ATTEMPT", "1e2"],
    ["GH_AW_GATEWAY_ACTOR_ID", "-1"],
    ["GH_AW_GATEWAY_WORKFLOW_SHA", "main"],
    ["GH_AW_GATEWAY_WORKFLOW_HASH", ""],
    ["GH_AW_GATEWAY_WORKFLOW_HASH", "a".repeat(63)],
    ["GH_AW_GATEWAY_WORKFLOW_HASH", "a".repeat(65)],
    ["GH_AW_GATEWAY_WORKFLOW_HASH", "A".repeat(64)],
    ["GH_AW_GATEWAY_WORKFLOW_HASH", "g".repeat(64)],
    ["GH_AW_GATEWAY_WORKFLOW_HASH", `${frontmatterHash}\n`],
    ["GH_AW_GATEWAY_WORKFLOW_PATH", ".github/workflows/../other.lock.yml"],
    ["GH_AW_GATEWAY_WORKFLOW_REF", "other/repo/.github/workflows/version-check.lock.yml@refs/heads/main"],
    ["GH_AW_GATEWAY_EVENT_NAME", "workflow_dispatch"],
    ["GH_AW_GATEWAY_EVENT_TYPE", "forged"],
    ["GH_AW_GATEWAY_EVENT_TYPE", `gh-aw-webhook-${"0".repeat(24)}`],
    ["GH_AW_GATEWAY_REF", "refs/pull/1/merge"],
  ])("rejects invalid configuration/context %s before any credentialed request", async (name, value) => {
    const f = fixture();
    f.env[name] = value;
    await expect(f.run()).rejects.toThrow(/ERR_CONFIG|ERR_PERMISSION/);
    expect(f.fetchImpl).not.toHaveBeenCalled();
    expect(f.core.getIDToken).not.toHaveBeenCalled();
  });

  it.each(["aud", "repository", "repository_id", "repository_owner_id", "workflow_ref", "workflow_sha", "run_id", "run_attempt", "actor_id", "ref", "event_name", "check_run_id", "exp", "nbf"])(
    "rejects an inconsistent or absent issued %s claim without sending the JWT to the backend",
    async field => {
      const f = fixture();
      if (field === "aud") f.core.getIDToken.mockImplementationOnce(async () => "e30.eyJhdWQiOiJ3cm9uZyJ9.signature");
      else delete f.claims[field];
      await expect(f.run()).rejects.toThrow("issued OIDC audience and workflow/run claims");
      expect(f.fetchImpl).not.toHaveBeenCalled();
    }
  );

  it.each(["", "not-a-jwt", "a".repeat(32769), "e30.W10.signature"])("rejects a malformed or oversized JWT", async token => {
    const f = fixture();
    f.core.getIDToken.mockResolvedValueOnce(token);
    await expect(f.run()).rejects.toThrow(/OIDC|JSON object/);
    expect(f.fetchImpl).not.toHaveBeenCalled();
  });

  it.each(["expired", "future"])("rejects %s issued JWT validity without imposing a renewal policy", async validity => {
    const f = fixture();
    if (validity === "expired") f.claims.exp = Math.floor(Date.now() / 1000) - 1;
    else f.claims.nbf = Math.floor(Date.now() / 1000) + 60;
    await expect(f.run()).rejects.toThrow("issued OIDC");
  });

  it.each(["schema_version", "repository", "repository_id", "repository_owner_id", "workflow_path", "workflow_ref", "workflow_sha", "workflow_hash", "run_id", "run_attempt", "check_run_id", "actor_id", "ref", "event_name", "event_type"])(
    "requires exact %s correlation in the explicit backend decision",
    async field => {
      const f = fixture();
      f.fetchImpl.mockImplementationOnce(async (_url, options) => Response.json({ ...JSON.parse(options.body), allowed: true, [field]: "different" }));
      await expect(f.run()).rejects.toThrow("exact closed workflow/run binding");
    }
  );

  it.each([{ allowed: true }, { allowed: false }, { allowed: "true" }, { allowed: true, access_token: "must-not-be-returned" }, { allowed: true, pending: true }, [], null])(
    "rejects unbound, denied, deferred, or credential-bearing success responses",
    async decision => {
      const f = fixture();
      f.fetchImpl.mockImplementationOnce(async (_url, options) => Response.json(decision?.access_token || decision?.pending || typeof decision?.allowed === "string" ? { ...JSON.parse(options.body), ...decision } : decision));
      await expect(f.run()).rejects.toThrow(/closed workflow\/run|JSON object/);
    }
  );

  it.each([202, 204, 301, 307, 400, 401, 403, 409, 410, 429, 500, 503])("keeps admission blocked on HTTP %i without automatic retries or forwarding error bodies", async status => {
    const f = fixture();
    f.fetchImpl.mockResolvedValueOnce(new Response(status === 204 ? null : "secret-server-detail", { status, headers: { Location: "https://other.example" } }));
    await expect(f.run()).rejects.toThrow(`Gateway getToken admission failed (HTTP ${status})`);
    expect(f.fetchImpl).toHaveBeenCalledOnce();
  });

  it.each(["decision-limit", "declared-length", "invalid-json", "invalid-utf8", "empty-response", "missing-body"])("enforces streaming byte limits and parsing on %s", async failure => {
    const f = fixture();
    f.fetchImpl.mockResolvedValueOnce(
      failure === "decision-limit"
        ? new Response(Buffer.alloc(16385))
        : failure === "declared-length"
          ? new Response("{}", { headers: { "content-length": "16385" } })
          : failure === "invalid-utf8"
            ? new Response(Buffer.from([0xff]))
            : failure === "missing-body"
              ? new Response(null)
              : new Response(failure === "empty-response" ? "" : "{")
    );
    await expect(f.run()).rejects.toThrow(/limits|UTF-8 JSON/);
  });

  it("cancels an oversized streaming response and redacts transport/issuance errors", async () => {
    const f = fixture();
    const cancel = vi.fn();
    f.fetchImpl.mockResolvedValueOnce(
      new Response(
        new ReadableStream({
          pull(controller) {
            controller.enqueue(new Uint8Array(16385));
          },
          cancel,
        })
      )
    );
    await expect(f.run()).rejects.toThrow("response limits");
    expect(cancel).toHaveBeenCalledOnce();
    for (const phase of ["oidc", "backend"]) {
      const g = fixture();
      if (phase === "oidc") g.core.getIDToken.mockRejectedValueOnce(new Error("secret-id-token"));
      else {
        g.fetchImpl.mockRejectedValueOnce(new Error("secret-id-token github-secret"));
      }
      await expect(g.run()).rejects.not.toThrow("secret-");
    }
  });

  it("applies the request abort deadline through streamed body reads", async () => {
    const controller = new AbortController();
    const timeout = vi.spyOn(AbortSignal, "timeout").mockReturnValue(controller.signal);
    const f = fixture();
    let signal;
    f.fetchImpl.mockImplementationOnce(async (_url, options) => {
      signal = options.signal;
      return new Response(
        new ReadableStream({
          start(controller) {
            signal.addEventListener("abort", () => controller.error(new Error("secret-deadline-detail")), { once: true });
          },
        })
      );
    });
    const result = expect(f.run()).rejects.toThrow("admission remains blocked");
    await vi.waitFor(() => expect(signal).toBeInstanceOf(AbortSignal));
    expect(timeout).toHaveBeenCalledExactlyOnceWith(30000);
    controller.abort();
    await result;
  });

  it("accepts the exact decision byte ceiling without an off-by-one rejection", async () => {
    const f = fixture();
    f.fetchImpl.mockImplementationOnce(async (_url, options) => new Response(JSON.stringify({ ...JSON.parse(options.body), allowed: true }).padEnd(16384, " ")));
    await expect(f.run()).resolves.toEqual({ verified: true, workflow_hash: frontmatterHash });
  });

  it.each([16384, 16385])("enforces the exact outbound request byte ceiling at %i UTF-8 bytes", async size => {
    const baseline = fixture();
    await baseline.run();
    const baselineSize = Buffer.byteLength(baseline.fetchImpl.mock.calls[0][1].body, "utf8");
    const f = fixture();
    let paddingSize = size - baselineSize;
    if (paddingSize % 2) {
      f.env.GH_AW_GATEWAY_ACTOR_ID += "0";
      f.claims.actor_id = f.env.GH_AW_GATEWAY_ACTOR_ID;
      paddingSize--;
    }
    f.env.GH_AW_GATEWAY_REF += "a".repeat(paddingSize / 2);
    f.env.GH_AW_GATEWAY_WORKFLOW_REF = `owner/repository/${f.env.GH_AW_GATEWAY_WORKFLOW_PATH}@${f.env.GH_AW_GATEWAY_REF}`;
    f.claims.ref = f.env.GH_AW_GATEWAY_REF;
    f.claims.workflow_ref = f.env.GH_AW_GATEWAY_WORKFLOW_REF;
    // An echoed approval would exceed the response ceiling; isolate the request ceiling with a denial.
    f.fetchImpl.mockResolvedValueOnce(new Response(null, { status: 403 }));
    if (size === 16384) {
      await expect(f.run()).rejects.toThrow("Gateway getToken admission failed (HTTP 403)");
      expect(Buffer.byteLength(f.fetchImpl.mock.calls[0][1].body, "utf8")).toBe(size);
    } else {
      await expect(f.run()).rejects.toThrow("request exceeds its UTF-8 byte limit");
      expect(f.fetchImpl).not.toHaveBeenCalled();
    }
  });

  it("bounds the serialized outbound UTF-8 request before presenting the JWT", async () => {
    const f = fixture();
    f.env.GH_AW_GATEWAY_REF = `refs/heads/${"é".repeat(4300)}`;
    f.env.GH_AW_GATEWAY_WORKFLOW_REF = `owner/repository/${f.env.GH_AW_GATEWAY_WORKFLOW_PATH}@${f.env.GH_AW_GATEWAY_REF}`;
    f.claims.ref = f.env.GH_AW_GATEWAY_REF;
    f.claims.workflow_ref = f.env.GH_AW_GATEWAY_WORKFLOW_REF;
    await expect(f.run()).rejects.toThrow("request exceeds its UTF-8 byte limit");
    expect(f.fetchImpl).not.toHaveBeenCalled();
  });
});

describe("gateway admission action entrypoint", () => {
  it.each([true, false])("publishes verified=true only for a complete accepted admission (%s)", async accepted => {
    const f = fixture();
    for (const [key, value] of Object.entries(f.env)) vi.stubEnv(key, value);
    vi.stubGlobal("core", f.core);
    vi.stubGlobal("fetch", f.fetchImpl);
    if (!accepted) f.core.getIDToken.mockRejectedValueOnce(new Error("secret-id-token"));
    await main();
    expect(f.core.setOutput.mock.calls).toEqual(
      accepted
        ? [
            ["verified", "false"],
            ["verified", "true"],
          ]
        : [["verified", "false"]]
    );
    expect(f.core.setFailed).toHaveBeenCalledTimes(accepted ? 0 : 1);
    expect(JSON.stringify([...f.core.setOutput.mock.calls, ...f.core.setFailed.mock.calls, ...f.core.info.mock.calls])).not.toContain("secret-");
  });

  describe("gateway admission HTTP interoperability", () => {
    it.each([200, 307, 503])("exercises actual loopback HTTP serialization and response handling for HTTP %i", async status => {
      const f = fixture();
      const requests = [];
      const server = createServer(async (request, response) => {
        const chunks = [];
        for await (const chunk of request) chunks.push(chunk);
        const body = Buffer.concat(chunks).toString("utf8");
        requests.push({ url: request.url, headers: request.headers, body });
        if (request.url === "/getToken") {
          response.writeHead(status, { "Content-Type": "application/json", Location: "/stolen" });
          response.end(JSON.stringify({ ...JSON.parse(body), allowed: true }));
        } else {
          response.writeHead(404);
          response.end();
        }
      });
      server.listen(0, "127.0.0.1");
      await once(server, "listening");
      const address = server.address();
      const transport = async (url, options) => {
        const original = new URL(url);
        expect(original.host).toBe("gateway.example");
        expect(original.protocol).toBe("https:");
        // Only the test adapter maps the approved HTTPS destinations to loopback.
        return fetch(`http://127.0.0.1:${address.port}${original.pathname}${original.search}`, options);
      };
      try {
        const result = verifyGatewayAdmission(f.core, f.env, transport);
        if (status === 200) await expect(result).resolves.toEqual({ verified: true, workflow_hash: frontmatterHash });
        else await expect(result).rejects.toThrow("Gateway getToken admission failed");
        expect(requests).toHaveLength(1);
        expect(requests[0].url).toBe("/getToken");
        expect(requests[0].headers.authorization).toBe(`Bearer ${await f.core.getIDToken.mock.results[0].value}`);
        expect(requests[0].headers["content-type"]).toBe("application/json");
        expect(JSON.parse(requests[0].body).workflow_hash).toBe(frontmatterHash);
        expect(JSON.parse(requests[0].body).run_attempt).toBe("2");
        expect(requests[0].body).not.toContain("github-secret");
      } finally {
        await new Promise((resolve, reject) => server.close(error => (error ? reject(error) : resolve())));
      }
    });
  });
});
