import { afterEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "node:module";
import { createHash, createHmac } from "node:crypto";
import { spawnSync } from "node:child_process";
import { Readable, Writable } from "node:stream";

const require = createRequire(import.meta.url);
const { decodePlan, executeConnector, callMCP, serve } = require("./connector_runtime.cjs");
const { connectorEffectBindings } = require("./connector_effect_bindings.cjs");
const VERSION = "github.com/gh-aw/connectors/v1";
const env = { GH_AW_CONNECTOR_SECRET_TEST_TOKEN: "test-token-not-a-real-credential" };
const goJSON = value => JSON.stringify(value).replace(/[<>&\u2028\u2029]/gu, char => "\\u" + char.charCodeAt(0).toString(16).padStart(4, "0"));

// These minimal snapshots follow the Go struct field spelling and frozen-plan
// digest contract; provider requests below mirror the checked-in declarations.
function document(phase = "safe-output", changes = {}, resources = { records: ["approved"] }) {
  const group = { "safe-output": "safeOutputs", context: "context", agent: "mcp" }[phase];
  const op = {
    ...(phase === "agent" ? { tool: "get-issue" } : {}),
    credential: "credential",
    inputs: { record: { type: "string", required: true, maxLength: 80 } },
    resources: { record: "records" },
    request: { endpoint: "api", method: phase === "safe-output" ? "POST" : "GET", path: "/records/{record}", pathInputs: { record: "record" } },
    maxResponseBytes: 4096,
    ...changes,
  };
  return {
    spec: {
      apiVersion: VERSION,
      kind: "ConnectorSpec",
      id: "example.com/issues",
      endpoints: { api: "https://api.example.com" },
      credentials: { credential: { phase, header: "Authorization", scheme: "bearer", scopes: ["records:" + (phase === "safe-output" ? "write" : "read")] } },
      [group]: { operation: op },
    },
    connection: {
      apiVersion: VERSION,
      kind: "ConnectorConnection",
      connector: "example.com/issues",
      credentials: { credential: { secret: "TEST_TOKEN" } },
      resources,
    },
    binding: { apiVersion: VERSION, kind: "ConnectorBinding", definition: "spec.md", connection: "connection.md", [group]: ["operation"] },
  };
}
function encode(document) {
  const spec = goJSON(document.spec);
  const connection = goJSON(document.connection);
  const binding = goJSON(document.binding);
  const digest = createHash("sha256").update(`{"Spec":${spec},"Connection":${connection},"Binding":${binding}}`).digest("hex");
  return Buffer.from(`{"digest":"${digest}","spec":${spec},"connection":${connection},"binding":${binding}}`).toString("base64");
}
const jsonResponse = (data = {}, status = 200, headers = {}) => new Response(typeof data === "string" ? data : JSON.stringify(data), { status, headers: { "content-type": "application/json", ...headers } });
const execute = (doc, args = { record: "approved" }, options = {}, capability = "safe-output", name = "operation") => executeConnector(encode(doc), capability, name, args, { env, fetchImpl: vi.fn(async () => jsonResponse()), ...options });

afterEach(() => vi.restoreAllMocks());

describe("declared bounded retries", () => {
  const retry = { maxAttempts: 3, backoffMs: 1, retryOn: [429, 503] };

  it("retries explicit transient statuses and preserves the exact request", async () => {
    const fetchImpl = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse({}, 503))
      .mockResolvedValueOnce(jsonResponse({}, 429))
      .mockResolvedValueOnce(jsonResponse({ id: "approved" }));
    await execute(document("agent", { retry }), undefined, { fetchImpl }, "mcp");
    expect(fetchImpl).toHaveBeenCalledTimes(3);
    for (const [url, options] of fetchImpl.mock.calls) {
      expect(url).toBe("https://api.example.com/records/approved");
      expect(options.headers.Authorization).toBe("Bearer test-token-not-a-real-credential");
      expect(options.redirect).toBe("manual");
    }
  });

  it.each([401, 403, 404])("never retries authorization or non-transient HTTP %s", async status => {
    const fetchImpl = vi.fn(async () => jsonResponse({}, status));
    await expect(execute(document("agent", { retry }), undefined, { fetchImpl }, "mcp")).rejects.toThrow();
    expect(fetchImpl).toHaveBeenCalledTimes(1);
  });

  it("does not retry transport errors or excessive Retry-After delays", async () => {
    for (const fetchImpl of [
      vi.fn(async () => {
        throw new Error("transport failure");
      }),
      vi.fn(async () => jsonResponse({}, 429, { "retry-after": "3600" })),
    ]) {
      await expect(execute(document("agent", { retry }), undefined, { fetchImpl }, "mcp")).rejects.toThrow();
      expect(fetchImpl).toHaveBeenCalledTimes(1);
    }
  });

  it("rejects automatic create retries even with an idempotency assertion", () => {
    expect(() => decodePlan(encode(document("safe-output", { retry: { ...retry, idempotent: true } })))).toThrow(/idempotent/);
  });

  it("requires explicit idempotency for retrying PUT effects", async () => {
    const doc = document("safe-output", { retry: { ...retry, idempotent: true }, request: { endpoint: "api", method: "PUT", path: "/records/{record}", pathInputs: { record: "record" } } });
    const fetchImpl = vi.fn().mockResolvedValueOnce(jsonResponse({}, 503)).mockResolvedValueOnce(jsonResponse());
    await execute(doc, undefined, { fetchImpl });
    expect(fetchImpl).toHaveBeenCalledTimes(2);
    delete doc.spec.safeOutputs.operation.retry.idempotent;
    expect(() => decodePlan(encode(doc))).toThrow(/idempotent/);
  });
});

describe("minimum title length", () => {
  it.each(["a", "💬"])("counts %s by Unicode code points and rejects titles shorter than 20", async character => {
    const doc = document("safe-output", {
      inputs: { record: { type: "string", required: true, maxLength: 80 }, title: { type: "string", required: true, sanitize: "title", maxLength: 128 } },
      request: { endpoint: "api", method: "POST", path: "/records/{record}", pathInputs: { record: "record" }, body: { title: { input: "title" } } },
    });
    const fetchImpl = vi.fn();
    await expect(execute(doc, { record: "approved", title: character.repeat(19) }, { staged: true, fetchImpl })).rejects.toThrow(/bounded Unicode/);
    await expect(execute(doc, { record: "approved", title: character.repeat(20) }, { staged: true, fetchImpl })).resolves.toMatchObject({ staged: true });
    expect(fetchImpl).not.toHaveBeenCalled();
  });
});

describe("compiler-frozen connector snapshots", () => {
  it("verifies readable JSON without evaluating expressions or template literals", () => {
    const doc = document();
    doc.binding.definition = "literal ${{ secrets.DO_NOT_EVALUATE }} ${notJavaScript} `backticks` <spec>&\u2028.md";
    doc.spec.safeOutputs.operation.responseChecks = { "/version": 1.5 };
    const raw = Buffer.from(encode(doc), "base64").toString("utf8");
    const readable = JSON.stringify(JSON.parse(raw), null, 2).replaceAll("${", "\\u0024{").replaceAll("`", "\\u0060");
    const decoded = decodePlan(readable);
    expect(decoded.binding.definition).toBe(doc.binding.definition);
    expect(decoded.digest).toBe(decodePlan(encode(doc)).digest);
    expect(() => decodePlan(readable.replace("/records/{record}", "/different"))).toThrow(/digest mismatch/);
  });

  it("preserves exact numeric tokens when verifying formatted JSON", () => {
    const doc = document("safe-output", { responseChecks: { "/version": 1 } });
    const spec = goJSON(doc.spec).replace('"/version":1', '"/version":1.0');
    const connection = goJSON(doc.connection);
    const binding = goJSON(doc.binding);
    const digest = createHash("sha256").update(`{"Spec":${spec},"Connection":${connection},"Binding":${binding}}`).digest("hex");
    const readable = `{\n  "digest": "${digest}",\n  "spec": ${spec},\n  "connection": ${connection},\n  "binding": ${binding}\n}`;
    expect(decodePlan(readable).digest).toBe(digest);
    expect(() => decodePlan(readable.replace('"/version":1.0', '"/version":1'))).toThrow(/digest mismatch/);
  });

  it("bounds readable snapshots by UTF-8 bytes, not string length", () => {
    const raw = Buffer.from(encode(document()), "base64").toString("utf8");
    const boundary = " ".repeat(65536 - Buffer.byteLength(raw)) + raw;
    expect(decodePlan(boundary).spec.id).toBe("example.com/issues");
    expect(() => decodePlan(boundary + " ")).toThrow(/byte bound/);
    const multibyte = boundary.replace("approved", "approvéd");
    expect(multibyte.length).toBeLessThanOrEqual(65536);
    expect(() => decodePlan(multibyte)).toThrow(/byte bound/);
  });

  it("verifies raw Go value slices, including map order and HTML escaping", () => {
    const doc = document();
    doc.binding.definition = "<spec>&\u2028.md";
    const decoded = decodePlan(encode(doc));
    expect(decoded.binding.definition).toBe("<spec>&\u2028.md");
    expect(decoded.spec.id).toBe("example.com/issues");
  });

  it.each([
    encoded => encoded.slice(0, -2) + "!!",
    encoded => Buffer.from(Buffer.from(encoded, "base64").toString().replace("/records/{record}", "/different")).toString("base64"),
    encoded => Buffer.from(Buffer.from(encoded, "base64").toString().replace('"digest":', '"extra":true,"digest":')).toString("base64"),
    encoded => Buffer.from(Buffer.from(encoded, "base64").toString().replace('"digest":', '"digest":"duplicate","digest":')).toString("base64"),
    encoded => Buffer.from(Buffer.from(encoded, "base64").toString().replace('"kind":"ConnectorSpec"', '"kind":"ConnectorSpec","kind":"ConnectorSpec"')).toString("base64"),
    () => "a".repeat(65537),
    () => Buffer.from("{} {}").toString("base64"),
  ])("rejects malformed, oversized, duplicate, unknown and corrupt snapshots", mutate => {
    expect(() => decodePlan(mutate(encode(document())))).toThrow(/connector|Connector|compiled|Compiled/);
  });

  it("rejects unknown nested fields even with a recomputed checksum", () => {
    const doc = document();
    doc.spec.safeOutputs.operation.request.redirect = "https://unapproved.example";
    expect(() => decodePlan(encode(doc))).toThrow(/Invalid compiled/);
  });

  it("rejects a mixed-phase snapshot before credential lookup or network", async () => {
    const doc = document();
    doc.spec.context = { other: { ...doc.spec.safeOutputs.operation, credential: "reader", request: { endpoint: "api", method: "GET", path: "/" } } };
    doc.spec.credentials.reader = { phase: "context", header: "Authorization", scheme: "bearer", scopes: ["read"] };
    doc.connection.credentials.reader = { secret: "READ_TOKEN" };
    doc.binding.context = ["other"];
    const fetchImpl = vi.fn();
    await expect(execute(doc, undefined, { fetchImpl })).rejects.toThrow(/single-phase/);
    expect(fetchImpl).not.toHaveBeenCalled();
  });

  it.each(["endpoint", "credential", "capability", "resource"])("rejects unselected %s authority", async kind => {
    const doc = document();
    if (kind === "endpoint") doc.spec.endpoints.unselected = "https://other.example";
    if (kind === "credential") {
      doc.spec.credentials.unselected = doc.spec.credentials.credential;
      doc.connection.credentials.unselected = { secret: "OTHER_TOKEN" };
    }
    if (kind === "capability") doc.spec.safeOutputs.other = doc.spec.safeOutputs.operation;
    if (kind === "resource") doc.connection.resources.other = ["extra"];
    await expect(execute(doc)).rejects.toThrow(/unselected/);
  });
});

describe("typed authority and credential-free staging", () => {
  it("returns an explicit Go-compatible safe-output intent without reading secrets or making requests", async () => {
    const doc = document();
    const fetchImpl = vi.fn();
    const noSecrets = new Proxy(
      {},
      {
        get: () => {
          throw new Error("must not read any environment field");
        },
      }
    );
    const result = await execute(doc, undefined, { staged: true, env: noSecrets, fetchImpl });
    expect(result).toEqual({ staged: true, request: { digest: decodePlan(encode(doc)).digest, operation: "operation", arguments: { record: "approved" } } });
    expect(fetchImpl).not.toHaveBeenCalled();
  });

  it.each([
    { record: "other" },
    { record: "approved", url: "https://evil.example" },
    { record: "approved", plan: "forged" },
    { record: "approved", GH_AW_CONNECTOR_SECRET_TEST_TOKEN: "forged" },
    { record: "approved", GH_AW_CONNECTOR_BINDING_RUN: "forged" },
    { record: "approved", receipt: "forged" },
    { record: "approved", cookie: "forged" },
    { record: "approved", __proto__: null, unexpected: undefined },
    { record: 3 },
    {},
    null,
    [],
  ])("rejects hostile or invalid actor arguments before any request", async args => {
    const fetchImpl = vi.fn();
    await expect(execute(document(), args, { fetchImpl })).rejects.toThrow(/argument|Arguments|JSON|resources/);
    expect(fetchImpl).not.toHaveBeenCalled();
  });

  it("validates Unicode code-point lengths and normalizes lone surrogates like Go JSON", async () => {
    const doc = document("safe-output", { inputs: { text: { type: "string", required: true, maxLength: 4 } }, resources: {}, request: { endpoint: "api", method: "POST", path: "/", body: { text: { input: "text" } } } }, {});
    await expect(execute(doc, { text: "éé" }, { staged: true })).resolves.toMatchObject({ staged: true });
    await expect(execute(doc, { text: "éééé" }, { staged: true })).resolves.toMatchObject({ staged: true });
    await expect(execute(doc, { text: "😀".repeat(4) }, { staged: true })).resolves.toMatchObject({ staged: true });
    await expect(execute(doc, { text: "é".repeat(5) }, { staged: true })).rejects.toThrow(/bounded Unicode/);
    await expect(execute(doc, { text: "😀".repeat(5) }, { staged: true })).rejects.toThrow(/bounded Unicode/);
    const result = await execute(doc, { text: "\ud800" }, { staged: true });
    expect(result.request.arguments.text).toBe("\uFFFD");
  });

  it("bounds the complete Go-escaped staged argument document, not only individual inputs", async () => {
    const doc = document(
      "safe-output",
      {
        inputs: { text: { type: "string", required: true, maxLength: 65536 } },
        resources: {},
        request: { endpoint: "api", method: "POST", path: "/" },
      },
      {}
    );
    await expect(execute(doc, { text: "<".repeat(65536) }, { staged: true })).rejects.toThrow(/arguments exceed their byte bound/);
  });

  it.each([
    ["string", { enum: ["allowed"] }, "denied"],
    ["string", { pattern: "^APP-[1-9][0-9]*$" }, "APP-1\n"],
    ["boolean", {}, "true"],
    ["integer", { minimum: 1, maximum: 3 }, 1.5],
    ["integer", { minimum: 1, maximum: 3 }, 4],
    ["number", { minimum: 1, maximum: 3 }, 0.9],
    ["integer", {}, Number.MAX_SAFE_INTEGER + 1],
    ["number", {}, Infinity],
  ])("enforces runtime %s constraints without trusting a frontend schema", async (type, constraints, value) => {
    const input = { type, required: true, ...(type === "string" ? { maxLength: 80 } : {}), ...constraints };
    const doc = document("safe-output", { inputs: { value: input }, resources: {}, request: { endpoint: "api", method: "POST", path: "/" } }, {});
    await expect(execute(doc, { value }, { staged: true })).rejects.toThrow();
  });

  it.each(["bearer", "basic", "token", "raw"])("uses only the selected environment secret and declared %s authentication", async scheme => {
    const doc = document();
    doc.spec.credentials.credential.scheme = scheme;
    doc.spec.credentials.credential.header = "X-API-Key";
    const fetchImpl = vi.fn(async () => jsonResponse());
    await execute(doc, undefined, { fetchImpl });
    const [url, init] = fetchImpl.mock.calls[0];
    expect(url).toBe("https://api.example.com/records/approved");
    expect(init.headers["X-API-Key"]).toBe({ bearer: "Bearer ", basic: "Basic ", token: "token ", raw: "" }[scheme] + env.GH_AW_CONNECTOR_SECRET_TEST_TOKEN);
    expect(init).toMatchObject({ redirect: "manual", credentials: "omit", cache: "no-store", referrerPolicy: "no-referrer" });
    expect(init.headers).not.toHaveProperty("Cookie");
  });

  it.each([{}, { TEST_TOKEN: "must-not-fall-back" }, { GH_AW_CONNECTOR_SECRET_TEST_TOKEN: "bad\r\nCookie: stolen" }])("rejects unavailable or malformed credentials without fallback", async badEnv => {
    const fetchImpl = vi.fn();
    await expect(execute(document(), undefined, { env: badEnv, fetchImpl })).rejects.toThrow(/credential resolution/);
    expect(fetchImpl).not.toHaveBeenCalled();
  });

  it.each(["http://api.example.com", "https://user:pass@api.example.com", "https://api.example.com/unapproved", "https://api.example.com?key=value"])("rejects non-fixed HTTPS origin %s", async endpoint => {
    const doc = document();
    doc.spec.endpoints.api = endpoint;
    await expect(execute(doc)).rejects.toThrow(/fixed HTTPS/);
  });

  it.each(["//evil.example", "/../admin", "/%2e%2e/admin", "/route?secret=x", "/route#fragment", "/back\\slash"])("rejects non-fixed path %s", async path => {
    const doc = document();
    doc.spec.safeOutputs.operation.request = { endpoint: "api", method: "POST", path };
    await expect(execute(doc)).rejects.toThrow(/fixed absolute path/);
  });

  it.each(["../admin", "foo/bar", "foo\\bar", "a%2fb", "a?b", "a#b", "."])("rejects hostile path input %s even when explicitly resource-granted", async record => {
    await expect(execute(document("safe-output", {}, { records: [record] }), { record })).rejects.toThrow(/safe segment/);
  });

  it("preserves typed booleans, numbers, optional fields and REST query encoding", async () => {
    const doc = document(
      "context",
      {
        inputs: { limit: { type: "integer", required: true, minimum: 1, maximum: 25 }, enabled: { type: "boolean", required: true }, optional: { type: "string", maxLength: 80 } },
        resources: {},
        request: { endpoint: "api", method: "GET", path: "/records", query: { limit: { input: "limit" }, enabled: { input: "enabled" }, optional: { input: "optional" }, fields: { literal: "name,email address" } } },
      },
      {}
    );
    const fetchImpl = vi.fn(async () => jsonResponse({ ok: true }));
    await execute(doc, { limit: 3, enabled: false }, { fetchImpl }, "context");
    expect(fetchImpl.mock.calls[0][0]).toBe("https://api.example.com/records?enabled=false&fields=name%2Cemail+address&limit=3");
    expect(fetchImpl.mock.calls[0][1].body).toBeUndefined();
  });

  it("uses Go-compatible escaping for safe path segments and scalar queries", async () => {
    const record = "$&+:=@ !~";
    const doc = document(
      "context",
      {
        request: { endpoint: "api", method: "GET", path: "/records/{record}", pathInputs: { record: "record" }, query: { value: { literal: "a~b + c" } } },
      },
      { records: [record] }
    );
    const fetchImpl = vi.fn(async () => jsonResponse());
    await execute(doc, { record }, { fetchImpl }, "context");
    expect(fetchImpl.mock.calls[0][0]).toBe("https://api.example.com/records/$&+:=@%20%21~?value=a~b+%2B+c");
  });
});

describe("eight provider request shapes", () => {
  const cases = [
    {
      provider: "jira",
      path: "/rest/api/3/issue/approved/comment",
      method: "POST",
      body: { body: { input: "text", encoding: "jira-adf" } },
      expected: { body: { version: 1, type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text: "first" }, { type: "hardBreak" }, { type: "text", text: "<&>\"'" }] }] } },
      reply: { id: "comment" },
    },
    {
      provider: "confluence",
      path: "/wiki/api/v2/pages",
      method: "POST",
      body: { body: { object: { representation: { literal: "storage" }, value: { input: "text", encoding: "confluence-storage" } } } },
      expected: { body: { representation: "storage", value: "<p>first<br />&lt;&amp;&gt;&#34;&#39;</p>" } },
      reply: { id: "page", version: { number: 1 } },
    },
    {
      provider: "linear",
      path: "/graphql",
      method: "POST",
      graphql: "mutation AddComment($issue: String!, $text: String!) { commentCreate(input: {issueId: $issue, body: $text}) { success } }",
      query: { issue: { input: "record" }, text: { input: "text" } },
      expected: { query: "mutation AddComment($issue: String!, $text: String!) { commentCreate(input: {issueId: $issue, body: $text}) { success } }", variables: { issue: "approved", text: "first\r\n<&>\"'" } },
      reply: { data: { commentCreate: { success: true } } },
    },
    {
      provider: "slack",
      path: "/api/chat.postMessage",
      method: "POST",
      body: { channel: { input: "record" }, text: { input: "text" }, mrkdwn: { literal: false }, unfurl_links: { literal: false } },
      expected: { channel: "approved", text: "first\r\n<&>\"'", mrkdwn: false, unfurl_links: false },
      reply: { ok: true, ts: "1.2" },
    },
    {
      provider: "sentry",
      path: "/api/0/issues/approved/",
      method: "PUT",
      body: { status: { literal: "resolved" } },
      expected: { status: "resolved" },
      reply: { id: "approved", status: "resolved" },
    },
    {
      provider: "teams",
      path: "/v3/conversations/approved/activities/root",
      method: "POST",
      body: {
        type: { literal: "message" },
        attachments: {
          array: [
            {
              object: {
                contentType: { literal: "application/vnd.microsoft.card.adaptive" },
                content: {
                  object: {
                    type: { literal: "AdaptiveCard" },
                    body: { array: [{ object: { type: { literal: "TextBlock" }, text: { input: "text" }, wrap: { literal: true } } }] },
                  },
                },
              },
            },
          ],
        },
      },
      expected: { type: "message", attachments: [{ contentType: "application/vnd.microsoft.card.adaptive", content: { type: "AdaptiveCard", body: [{ type: "TextBlock", text: "first\r\n<&>\"'", wrap: true }] } }] },
      reply: { id: "activity" },
    },
    {
      provider: "azure-devops",
      path: "/organization/project/_apis/wit/workitems/42",
      method: "PATCH",
      patch: [
        { op: "test", path: "/rev", value: { literal: 3 } },
        { op: "add", path: "/fields/System.Title", value: { input: "text" } },
      ],
      expected: [
        { op: "test", path: "/rev", value: 3 },
        { op: "add", path: "/fields/System.Title", value: "first\r\n<&>\"'" },
      ],
      reply: { id: 42, fields: { "System.TeamProject": "project" } },
    },
    {
      provider: "servicenow",
      path: "/api/now/table/incident/approved",
      method: "PATCH",
      body: { work_notes: { input: "text" } },
      expected: { work_notes: "first\r\n<&>\"'" },
      reply: { result: { sys_id: "approved" } },
    },
  ];
  it.each(cases)("executes the $provider declaration without SDKs", async fixture => {
    const { provider, path, method, body, query, graphql, patch, expected, reply } = fixture;
    const doc = document("safe-output", {
      inputs: { record: { type: "string", required: true, maxLength: 80 }, text: { type: "string", required: true, maxLength: 1024 } },
      request: { endpoint: "api", method, path, ...(body ? { body } : {}), ...(query ? { query } : {}), ...(graphql ? { graphql } : {}), ...(patch ? { patch } : {}) },
    });
    doc.spec.id = `example.com/${provider}`;
    doc.connection.connector = doc.spec.id;
    const fetchImpl = vi.fn(async () => jsonResponse(reply));
    const result = await execute(doc, { record: "approved", text: "first\r\n<&>\"'" }, { fetchImpl });
    expect(JSON.parse(fetchImpl.mock.calls[0][1].body)).toEqual(expected);
    expect(fetchImpl.mock.calls[0][1].headers["Content-Type"]).toBe(patch ? "application/json-patch+json" : "application/json");
    expect(result).toEqual({ connector: doc.spec.id, capability: "safe-output/operation", untrusted: true, statusCode: 200, data: reply });
  });

  it("rejects an absent value inside a structured array or JSON Patch", async () => {
    for (const payload of [{ body: { values: { array: [{ input: "optional" }] } } }, { patch: [{ op: "add", path: "/field", value: { input: "optional" } }] }]) {
      const doc = document(
        "safe-output",
        {
          inputs: { optional: { type: "string", maxLength: 80 } },
          resources: {},
          request: { endpoint: "api", method: "POST", path: "/", ...payload },
        },
        {}
      );
      await expect(execute(doc, {}, { staged: true })).rejects.toThrow(/missing/);
    }
  });
});

describe("literal request metadata headers", () => {
  it("applies fixed Notion-Version, Accept and From metadata before credential authentication", async () => {
    const doc = document();
    doc.spec.safeOutputs.operation.request.headers = { "Notion-Version": "2025-09-03", accept: "application/vnd.example+json", From: "workflow@example.com", "content-type": "application/json" };
    const fetchImpl = vi.fn(async () => jsonResponse());
    await execute(doc, undefined, { fetchImpl });
    const headers = fetchImpl.mock.calls[0][1].headers;
    expect(headers).toEqual({ "Notion-Version": "2025-09-03", accept: "application/vnd.example+json", From: "workflow@example.com", "content-type": "application/json", Authorization: "Bearer " + env.GH_AW_CONNECTOR_SECRET_TEST_TOKEN });
    expect(Object.keys(headers).filter(key => key.toLowerCase() === "accept")).toHaveLength(1);
    expect(Object.keys(headers).filter(key => key.toLowerCase() === "content-type")).toHaveLength(1);
  });

  it("accepts exactly 16 metadata headers and 1024 UTF-8 bytes per value", async () => {
    const doc = document();
    doc.spec.safeOutputs.operation.request.headers = Object.fromEntries(Array.from({ length: 16 }, (_, index) => ["X-Metadata-" + index, "é".repeat(512)]));
    await expect(execute(doc, undefined, { staged: true })).resolves.toMatchObject({ staged: true });
    doc.spec.safeOutputs.operation.request.headers["X-Metadata-16"] = "value";
    await expect(execute(doc, undefined, { staged: true })).rejects.toThrow(/count bound/);
    delete doc.spec.safeOutputs.operation.request.headers["X-Metadata-16"];
    doc.spec.safeOutputs.operation.request.headers["X-Metadata-0"] += "é";
    await expect(execute(doc, undefined, { staged: true })).rejects.toThrow(/bounded literal HTTP/);
  });

  it("preserves valid HTTP token names without JavaScript prototype setters", async () => {
    const doc = document();
    doc.spec.safeOutputs.operation.request.headers = Object.fromEntries([
      ["__proto__", "literal"],
      ["constructor", "metadata"],
      ["X!#$%&'*+.^_`|~-", "fixed"],
    ]);
    const fetchImpl = vi.fn(async () => jsonResponse());
    await execute(doc, undefined, { fetchImpl });
    const headers = fetchImpl.mock.calls[0][1].headers;
    expect(Object.hasOwn(headers, "__proto__")).toBe(true);
    expect(headers.__proto__).toBe("literal");
    expect(headers.constructor).toBe("metadata");
    expect(headers["X!#$%&'*+.^_`|~-"]).toBe("fixed");
    expect(Object.getPrototypeOf(headers)).toBe(Object.prototype);
  });

  it.each([
    "Authorization",
    "Proxy-Authorization",
    "Host",
    "Cookie",
    "Set-Cookie",
    "Connection",
    "Content-Length",
    "Transfer-Encoding",
    "TE",
    "Trailer",
    "Upgrade",
    "Keep-Alive",
    "PRIVATE-TOKEN",
    "X-API-Key",
    "X-Client-Token",
    "X-Client-Secret",
    "pRoXy-aUtHoRiZaTiOn",
  ])("rejects authority or secret-bearing %s metadata before secret lookup or network", async name => {
    const doc = document();
    doc.spec.safeOutputs.operation.request.headers = { [name]: "literal" };
    const noSecrets = new Proxy(
      {},
      {
        get: () => {
          throw new Error("must not read any environment field");
        },
      }
    );
    const fetchImpl = vi.fn();
    await expect(execute(doc, undefined, { env: noSecrets, fetchImpl })).rejects.toThrow(/authority or secret-bearing/);
    expect(fetchImpl).not.toHaveBeenCalled();
  });

  it.each([
    { "Bad Name": "value" },
    { "Name:Value": "value" },
    { "": "value" },
    { "Nonascii-é": "value" },
    { "X-Metadata": "bad\rvalue" },
    { "X-Metadata": "bad\nvalue" },
    { "X-Metadata": "${TOKEN}" },
    { "X-Metadata": true },
    { "X-Metadata": 42 },
    { "X-Metadata": { input: "record" } },
    { "X-Metadata": ["literal"] },
  ])("rejects invalid HTTP tokens and nonliteral or unbounded values", async headers => {
    const doc = document();
    doc.spec.safeOutputs.operation.request.headers = headers;
    await expect(execute(doc, undefined, { staged: true })).rejects.toThrow(/bounded literal HTTP/);
  });

  it("rejects case-insensitive duplicates and non-map declarations", async () => {
    const doc = document();
    doc.spec.safeOutputs.operation.request.headers = { "Notion-Version": "2025-09-03", "notion-version": "2025-09-03" };
    await expect(execute(doc, undefined, { staged: true })).rejects.toThrow(/case-insensitive duplicate/);
    doc.spec.safeOutputs.operation.request.headers = ["header"];
    await expect(execute(doc, undefined, { staged: true })).rejects.toThrow(/literal metadata map/);
  });

  it.each(["Authorization", "X-API-Key", "PRIVATE-TOKEN"])("never lets metadata override the actual %s credential header", async name => {
    const doc = document();
    doc.spec.credentials.credential.header = name;
    doc.spec.safeOutputs.operation.request.headers = { [name.toLowerCase()]: "actor supplied" };
    const fetchImpl = vi.fn();
    await expect(execute(doc, undefined, { fetchImpl })).rejects.toThrow(/authority or secret-bearing/);
    expect(fetchImpl).not.toHaveBeenCalled();
  });

  it.each([
    ["GET", undefined, "application/json", true],
    ["POST", { text: { literal: "fixed" } }, "application/json", true],
    ["PATCH", undefined, "application/json-patch+json", true],
    ["GET", undefined, "application/json-patch+json", false],
    ["POST", { text: { literal: "fixed" } }, "text/plain", false],
    ["POST", undefined, "Application/JSON", false],
    ["POST", undefined, "application/json; charset=utf-8", false],
    ["PATCH", undefined, "application/json", false],
  ])("enforces canonical Content-Type for %s payloads", async (method, body, contentType, accepted) => {
    const request = { endpoint: "api", method, path: "/", headers: { "Content-Type": contentType }, ...(body ? { body } : {}), ...(method === "PATCH" ? { patch: [{ op: "add", path: "/title", value: { literal: "fixed" } }] } : {}) };
    const doc = document("safe-output", { request });
    const result = execute(doc, undefined, { staged: true });
    if (accepted) await expect(result).resolves.toMatchObject({ staged: true });
    else await expect(result).rejects.toThrow(/canonical payload type/);
  });
});

describe("optional-empty response evidence", () => {
  function pagedDocument() {
    return document("context", {
      responseChecks: { "/ok": true, "/has_more": false },
      responseOptionalEmpty: ["/response_metadata/next_cursor"],
    });
  }
  it.each([
    { ok: true, has_more: false },
    { ok: true, has_more: false, response_metadata: {} },
    { ok: true, has_more: false, response_metadata: { next_cursor: null } },
    { ok: true, has_more: false, response_metadata: { next_cursor: "" } },
  ])("accepts only absent, null or an exactly empty cursor string", async reply => {
    await expect(execute(pagedDocument(), undefined, { fetchImpl: async () => jsonResponse(reply) }, "context")).resolves.toMatchObject({ statusCode: 200 });
  });

  it.each(["cursor-token", " ", "\t", "\n", 0, 42, false, true, [], {}, [""]])("rejects nonempty or nonstring cursor %j even when has_more is false", async cursor => {
    await expect(execute(pagedDocument(), undefined, { fetchImpl: async () => jsonResponse({ ok: true, has_more: false, response_metadata: { next_cursor: cursor } }) }, "context")).rejects.toThrow(/nonempty optional evidence/);
  });

  it("validates optional-empty pointer declarations and still requires a JSON response", async () => {
    for (const constraint of ["not-an-array", ["not-a-pointer"], ["/bad~2escape"]]) {
      const doc = document("context", { responseOptionalEmpty: constraint });
      await expect(execute(doc, undefined, {}, "context")).rejects.toThrow(/Invalid compiled/);
    }
    const doc = document("context", { responseOptionalEmpty: ["/cursor"] });
    await expect(execute(doc, undefined, { fetchImpl: async () => new Response(null, { status: 204 }) }, "context")).rejects.toThrow(/required evidence/);
  });

  it("propagates optional-empty and request header constraints through trusted preflight before mutation", async () => {
    const doc = trustedDocument();
    doc.spec.safeOutputs.operation.preflight.responseOptionalEmpty = ["/data/project/cursor"];
    doc.spec.safeOutputs.operation.preflight.request.headers = { "Notion-Version": "2025-09-03" };
    for (const cursor of ["", null, "must-not-page"]) {
      const fetchImpl = vi.fn(async (_url, init) => {
        const request = JSON.parse(init.body);
        if (request.query) {
          expect(init.headers["Notion-Version"]).toBe("2025-09-03");
          return jsonResponse({ data: { project: { id: "trusted-id", cursor, teams: { nodes: [{ id: "authored-slug" }] } } } });
        }
        return jsonResponse({ id: "created" });
      });
      const result = execute(doc, { title: "title" }, { env: { ...env, GH_AW_CONNECTOR_SECRET_TRUSTED_RECORD: "authored-slug" }, fetchImpl });
      if (cursor === "must-not-page") {
        await expect(result).rejects.toThrow(/nonempty optional evidence/);
        expect(fetchImpl).toHaveBeenCalledTimes(1);
      } else {
        await expect(result).resolves.toMatchObject({ statusCode: 200 });
        expect(fetchImpl).toHaveBeenCalledTimes(2);
      }
    }
  });
});

describe("exact resource-input response identities", () => {
  function identityDocument(expected = "42", extra = {}) {
    return document("context", { responseResourceInputs: { "/result/id": "record" }, ...extra }, { records: [expected] });
  }
  it.each([
    ['"42"', "42"],
    ["42", "42"],
    ["0", "0"],
    ["9007199254740991", "9007199254740991"],
    ['"042"', "042"],
    ['"APP-42"', "APP-42"],
  ])("accepts identity token %s only for the exact authored string %s", async (token, expected) => {
    await expect(execute(identityDocument(expected), { record: expected }, { fetchImpl: async () => jsonResponse('{"result":{"id":' + token + "}}") }, "context")).resolves.toMatchObject({ statusCode: 200 });
  });

  it.each([
    ["42", "042"],
    ['"042"', "42"],
    ["42.0", "42"],
    ["42e0", "42"],
    ["4.2e1", "42"],
    ["42.0000000000000001", "42"],
    ["-0", "0"],
    ["-42", "-42"],
    ["9007199254740992", "9007199254740992"],
    ["9007199254740993", "9007199254740993"],
    ["null", "42"],
    ["true", "42"],
    ['["42"]', "42"],
    ['{"id":"42"}', "42"],
  ])("rejects noncanonical or mismatched identity token %s for %s", async (token, expected) => {
    await expect(execute(identityDocument(expected), { record: expected }, { fetchImpl: async () => jsonResponse('{"result":{"id":' + token + "}}") }, "context")).rejects.toThrow(/exact resource input|unsafe number/);
  });

  it("requires the exact requested identity even when a different returned ID is granted", async () => {
    const doc = identityDocument();
    doc.connection.resources.records.push("43");
    await expect(execute(doc, { record: "42" }, { fetchImpl: async () => jsonResponse('{"result":{"id":43}}') }, "context")).rejects.toThrow(/exact resource input/);
  });

  it("preserves strict responseInputs without string-to-number coercion", async () => {
    const doc = identityDocument("42", { responseInputs: { "/result/id": "record" } });
    await expect(execute(doc, { record: "42" }, { fetchImpl: async () => jsonResponse('{"result":{"id":42}}') }, "context")).rejects.toThrow(/request input/);
  });

  it("finds raw numeric tokens through nested arrays and escaped JSON Pointer keys", async () => {
    const doc = identityDocument();
    doc.spec.context.operation.responseResourceInputs = { "/result/items/0/a~1b~0c": "record" };
    await expect(execute(doc, { record: "42" }, { fetchImpl: async () => jsonResponse('{"result":{"items":[{"a/b~c":42}]}}') }, "context")).resolves.toMatchObject({ statusCode: 200 });
    await expect(execute(doc, { record: "42" }, { fetchImpl: async () => jsonResponse('{"result":{"items":[{"a/b~c":42e0}]}}') }, "context")).rejects.toThrow(/exact resource input/);
  });

  it("rejects missing evidence and declaration targets that are not required resource-constrained strings", async () => {
    await expect(execute(identityDocument(), { record: "42" }, { fetchImpl: async () => jsonResponse({ result: {} }) }, "context")).rejects.toThrow(/exact resource input/);
    for (const mutate of [
      op => {
        op.responseResourceInputs = { "invalid-pointer": "record" };
      },
      op => {
        op.responseResourceInputs = { "/id": "missing" };
      },
      op => {
        op.inputs.record.required = false;
        delete op.resources.record;
        delete op.request.pathInputs;
        op.request.path = "/records";
      },
      op => {
        op.inputs.record.type = "integer";
        delete op.inputs.record.maxLength;
        delete op.resources.record;
        delete op.request.pathInputs;
        op.request.path = "/records";
      },
      op => {
        delete op.resources.record;
      },
    ]) {
      const doc = identityDocument();
      mutate(doc.spec.context.operation);
      expect(() => decodePlan(encode(doc))).toThrow(/Invalid compiled|resource-constrained/);
    }
  });
});

describe("bounded untrusted response evidence", () => {
  const checked = () =>
    document("context", {
      responseChecks: { "/ok": true, "/nullable": null },
      responseRequired: ["/record/id"],
      responseTypes: { "/record/id": "string", "/revision": "integer" },
      responseResources: { "/record/id": "records" },
      responseInputs: { "/record/id": "record" },
      project: { id: "/record/id", revision: "/revision" },
    });
  const response = { ok: true, nullable: null, record: { id: "approved" }, revision: 3, private: "must not project" };

  it("applies checks, required values, types, resource grants, input equality and projection", async () => {
    await expect(execute(checked(), undefined, { fetchImpl: async () => jsonResponse(response) }, "context")).resolves.toMatchObject({ data: { id: "approved", revision: 3 }, untrusted: true });
  });

  it.each([
    { ...response, ok: false },
    { ...response, nullable: false },
    { ...response, record: { id: "" } },
    { ...response, record: { id: "unapproved" } },
    { ...response, revision: "3" },
    { ...response, revision: 1.5 },
    { ...response, revision: Number.MAX_SAFE_INTEGER + 1 },
    { ok: true, record: { id: "approved" }, revision: 3 },
  ])("rejects missing or mismatched evidence without revealing provider content", async data => {
    await expect(execute(checked(), undefined, { fetchImpl: async () => jsonResponse(data) }, "context")).rejects.toThrow(/Connector/);
  });

  it("requires request equality even if a different returned resource is also granted", async () => {
    const doc = checked();
    doc.connection.resources.records.push("other-approved");
    await expect(execute(doc, undefined, { fetchImpl: async () => jsonResponse({ ...response, record: { id: "other-approved" } }) }, "context")).rejects.toThrow(/request input/);
  });

  it("preserves Go number spelling for exact response resource grants", async () => {
    const doc = document("context", { responseResources: { "/id": "ids" } }, { records: ["approved"], ids: ["3"] });
    await expect(execute(doc, undefined, { fetchImpl: async () => jsonResponse('{"id":3}') }, "context")).resolves.toMatchObject({ data: { id: 3 } });
    await expect(execute(doc, undefined, { fetchImpl: async () => jsonResponse('{"id":3.0}') }, "context")).rejects.toThrow(/approved resources/);
  });

  it("compares nested response checks with canonical keys and exact Go numeric tokens", async () => {
    const doc = document("context", { responseChecks: { "/evidence": { count: 3, empty: false } } });
    await expect(execute(doc, undefined, { fetchImpl: async () => jsonResponse('{"evidence":{"empty":false,"count":3}}') }, "context")).resolves.toMatchObject({ statusCode: 200 });
    await expect(execute(doc, undefined, { fetchImpl: async () => jsonResponse('{"evidence":{"empty":false,"count":3.0}}') }, "context")).rejects.toThrow(/response check/);
  });

  it.each(["", "{invalid", '{"duplicate":1,"duplicate":2}', "null true"])("rejects empty or malformed required JSON evidence", async text => {
    await expect(execute(checked(), undefined, { fetchImpl: async () => jsonResponse(text) }, "context")).rejects.toThrow(/Connector/);
  });

  it("accepts an empty response only when no evidence is declared", async () => {
    await expect(execute(document(), undefined, { fetchImpl: async () => new Response(null, { status: 204 }) })).resolves.toMatchObject({ statusCode: 204, data: null });
  });

  it.each([302, 307, 401, 429, 500])("never redirects, retries, or exposes a provider body on HTTP %s", async status => {
    const fetchImpl = vi.fn(async () => jsonResponse({ diagnostic: "private diagnostic" }, status, { Location: "https://other.example/leak" }));
    await expect(execute(document(), undefined, { fetchImpl })).rejects.toThrow(/non-success/);
    expect(fetchImpl).toHaveBeenCalledTimes(1);
  });

  it("sanitizes thrown transport failures", async () => {
    await expect(
      execute(document(), undefined, {
        fetchImpl: async () => {
          throw new Error("private diagnostic credential");
        },
      })
    ).rejects.toThrow("Connector HTTP request failed; outcome may be unknown, no retry performed");
  });

  it("rejects an already-followed redirect even if an injected fetch implementation ignores options", async () => {
    const response = jsonResponse();
    Object.defineProperty(response, "redirected", { value: true });
    await expect(execute(document(), undefined, { fetchImpl: async () => response })).rejects.toThrow(/redirects/);
  });

  it("enforces exact response byte bounds with and without Content-Length", async () => {
    const doc = document("context", { maxResponseBytes: 16 });
    await expect(execute(doc, undefined, { fetchImpl: async () => jsonResponse('{"data":"12345"}') }, "context")).resolves.toMatchObject({ data: { data: "12345" } });
    await expect(execute(doc, undefined, { fetchImpl: async () => jsonResponse('{"data":"1234567"}') }, "context")).rejects.toThrow(/byte bound/);
    await expect(execute(doc, undefined, { fetchImpl: async () => jsonResponse("{}", 200, { "content-length": "17" }) }, "context")).rejects.toThrow(/byte bound/);
  });

  it("bounds the complete request including a stalled response body", async () => {
    vi.useFakeTimers();
    try {
      let canceled = false;
      const fetchImpl = vi.fn(
        async () =>
          new Response(
            new ReadableStream({
              cancel: () => {
                canceled = true;
              },
            })
          )
      );
      const result = execute(document(), undefined, { fetchImpl });
      const rejected = expect(result).rejects.toThrow(/timed out/);
      await vi.advanceTimersByTimeAsync(20001);
      await rejected;
      expect(fetchImpl).toHaveBeenCalledTimes(1);
      expect(canceled).toBe(true);
    } finally {
      vi.useRealTimers();
    }
  });

  it.each([{ errors: [{ message: "provider private diagnostic" }], data: {} }, { errors: "bad" }, {}])("rejects GraphQL error and missing-data envelopes", async data => {
    const doc = document("context", { request: { endpoint: "api", method: "POST", path: "/graphql", graphql: "query Read { id }" } });
    await expect(execute(doc, undefined, { fetchImpl: async () => jsonResponse(data) }, "context")).rejects.toThrow(/GraphQL/);
  });
});

function trustedDocument() {
  const string = { type: "string", required: true, maxLength: 80 };
  return document(
    "safe-output",
    {
      inputs: { record: string, title: string },
      resources: { record: "records" },
      request: { endpoint: "api", method: "POST", path: "/create", body: { record: { input: "record" }, title: { input: "title" } } },
      trustedInputs: { record: { secret: "TRUSTED_RECORD" } },
      preflight: {
        inputs: { record: string },
        request: { endpoint: "api", method: "POST", path: "/graphql", graphql: "query Trusted($record: String!) { project(id: $record) { id teams { nodes { id } } } }", query: { record: { input: "record" } } },
        maxResponseBytes: 4096,
        responseRequired: ["/data/project/id"],
        responseContains: { "/data/project/teams/nodes/*/id": "record" },
        project: { record: "/data/project/id" },
      },
    },
    {}
  );
}

describe("safe-output-only trusted input preflight", () => {
  it("stages without secrets and binds only the validated trusted projection before mutation", async () => {
    const doc = trustedDocument();
    const fetchImpl = vi.fn(async (_url, init) => {
      const request = JSON.parse(init.body);
      if (request.query) {
        expect(request.variables).toEqual({ record: "authored-slug" });
        return jsonResponse({ data: { project: { id: "trusted-id", teams: { nodes: [{ id: "authored-slug" }] } } } });
      }
      expect(request).toEqual({ record: "trusted-id", title: "title" });
      return jsonResponse({ id: "created" });
    });
    const staged = await execute(doc, { title: "title" }, { staged: true, env: {}, fetchImpl });
    expect(staged.request.arguments).toEqual({ title: "title" });
    expect(fetchImpl).not.toHaveBeenCalled();
    await execute(doc, { title: "title" }, { env: { ...env, GH_AW_CONNECTOR_SECRET_TRUSTED_RECORD: "authored-slug" }, fetchImpl });
    expect(fetchImpl).toHaveBeenCalledTimes(2);
  });

  it.each([{ title: "title", record: "actor-override" }, { title: "title", url: "https://evil.example" }, {}])("validates all actor inputs before reading trusted secrets", async args => {
    const noSecrets = new Proxy(
      {},
      {
        get: () => {
          throw new Error("secret was read");
        },
      }
    );
    await expect(execute(trustedDocument(), args, { env: noSecrets, fetchImpl: vi.fn() })).rejects.toThrow(/argument/);
  });

  it.each([{ data: { project: { id: "trusted-id", teams: { nodes: [] } } } }, { data: { project: { id: 42, teams: { nodes: [{ id: "authored-slug" }] } } } }, { errors: [{ message: "private diagnostic" }] }])(
    "does not mutate or retry after failed trusted relationship checks",
    async reply => {
      const fetchImpl = vi.fn(async () => jsonResponse(reply));
      await expect(execute(trustedDocument(), { title: "title" }, { env: { ...env, GH_AW_CONNECTOR_SECRET_TRUSTED_RECORD: "authored-slug" }, fetchImpl })).rejects.toThrow(/Connector/);
      expect(fetchImpl).toHaveBeenCalledTimes(1);
    }
  );

  it("rejects a preflight that consumes actor input or changes endpoint", () => {
    for (const mutate of [
      op => {
        op.preflight.inputs.title = { type: "string", required: true, maxLength: 80 };
      },
      op => {
        op.preflight.request.endpoint = "other";
      },
    ]) {
      const doc = trustedDocument();
      mutate(doc.spec.safeOutputs.operation);
      expect(() => decodePlan(encode(doc))).toThrow();
    }
  });
});

function createdDocument() {
  const doc = document(
    "safe-output",
    {
      inputs: {
        project: { type: "string", required: true, maxLength: 80 },
        temporary: { type: "string", maxLength: 80, pattern: "^#?aw_[A-Za-z0-9_]{3,12}$" },
      },
      resources: { project: "projects" },
      request: { endpoint: "api", method: "POST", path: "/records", body: { project: { input: "project" } } },
      creates: { resource: "records", value: "/id", scope: "project", temporaryId: "temporary", scopePrefix: "-" },
      responseRequired: ["/id"],
    },
    { projects: ["APP"], records: ["APP-1"] }
  );
  doc.spec.safeOutputs.operation = { ...doc.spec.safeOutputs.operation };
  doc.spec.safeOutputs.comment = {
    credential: "credential",
    inputs: { record: { type: "string", required: true, maxLength: 80, pattern: "^APP-[1-9][0-9]*$" } },
    resources: { record: "records" },
    request: { endpoint: "api", method: "POST", path: "/records/{record}/comments", pathInputs: { record: "record" } },
    createdResources: { record: "operation" },
    maxResponseBytes: 4096,
  };
  doc.binding.safeOutputs.push("comment");
  return doc;
}

describe("authenticated invocation-local created-resource receipts", () => {
  it("stages temporary references but never promotes a real ungranted resource", async () => {
    const doc = createdDocument();
    await expect(execute(doc, { record: "#aw_new" }, { staged: true }, "safe-output", "comment")).resolves.toMatchObject({ request: { arguments: { record: "aw_new" } } });
    await expect(execute(doc, { record: "APP-99" }, { staged: true }, "safe-output", "comment")).rejects.toThrow(/approved resources/);
    await expect(execute(doc, { record: "#aw_new" }, {}, "safe-output", "comment")).rejects.toThrow(/authenticated/);
  });

  it("issues Go-compatible uppercase HMAC receipts and resolves only the approved chain", async () => {
    const doc = createdDocument();
    const plan = encode(doc);
    const execution = new Map();
    const binding = connectorEffectBindings(execution, plan);
    const fetchImpl = vi.fn(async url => jsonResponse(url.endsWith("/records") ? { id: "APP-99" } : { id: "comment" }, 201));
    const result = await executeConnector(plan, "safe-output", "operation", { project: "APP", temporary: "#aw_new" }, { env: { ...env, ...binding.env }, fetchImpl });
    const [payload, signature] = result.createdResource.split(".");
    const text = Buffer.from(payload, "base64url").toString();
    const receipt = JSON.parse(text);
    expect(Object.keys(receipt)).toEqual(["Run", "Digest", "Connector", "Producer", "Endpoint", "Resource", "Scope", "ScopeValue", "TemporaryID", "Value"]);
    expect(text).toBe(goJSON(receipt));
    expect(createHmac("sha256", Buffer.from(binding.env.GH_AW_CONNECTOR_BINDING_KEY, "base64")).update(text).digest("base64url")).toBe(signature);
    expect(receipt).toMatchObject({ Scope: "project", ScopeValue: "APP", TemporaryID: "aw_new", Value: "APP-99", Producer: "operation" });
    binding.record(result);
    expect(result.createdResource).toBeUndefined();
    const second = connectorEffectBindings(execution, plan);
    await executeConnector(plan, "safe-output", "comment", { record: "#aw_new" }, { env: { ...env, ...second.env }, fetchImpl });
    expect(fetchImpl.mock.calls[1][0]).toBe("https://api.example.com/records/APP-99/comments");
    await expect(executeConnector(plan, "safe-output", "comment", { record: "APP-99" }, { env: { ...env, ...second.env }, fetchImpl })).rejects.toThrow(/approved resources/);
  });

  it.each(["forged", "cross-run", "cross-plan", "unknown", "duplicate", "wrong-scope", "duplicate-field"])("rejects %s receipts without any network effects", async mode => {
    const doc = createdDocument();
    let plan = encode(doc);
    const binding = connectorEffectBindings(new Map(), plan);
    const result = await executeConnector(plan, "safe-output", "operation", { project: "APP", temporary: "aw_new" }, { env: { ...env, ...binding.env }, fetchImpl: async () => jsonResponse({ id: "APP-99" }) });
    let token = result.createdResource;
    const state = { ...env, ...binding.env };
    if (mode === "forged") token = token.slice(0, -1) + (token.endsWith("a") ? "b" : "a");
    if (mode === "cross-run") state.GH_AW_CONNECTOR_BINDING_RUN = "different";
    if (mode === "cross-plan") {
      doc.binding.definition = "different.md";
      plan = encode(doc);
    }
    if (["wrong-scope", "duplicate-field"].includes(mode)) {
      const receipt = JSON.parse(Buffer.from(token.split(".")[0], "base64url").toString());
      receipt.ScopeValue = "unapproved";
      let text = goJSON(receipt);
      if (mode === "duplicate-field") text = text.replace('"Run":', '"Run":"duplicate","Run":');
      token = Buffer.from(text).toString("base64url") + "." + createHmac("sha256", Buffer.from(state.GH_AW_CONNECTOR_BINDING_KEY, "base64")).update(text).digest("base64url");
    }
    state.GH_AW_CONNECTOR_BINDING_RECEIPTS = JSON.stringify(mode === "duplicate" ? [token, token] : [token]);
    const fetchImpl = vi.fn();
    await expect(executeConnector(plan, "safe-output", "comment", { record: mode === "unknown" ? "aw_unknown" : "aw_new" }, { env: state, fetchImpl })).rejects.toThrow(/receipt|reference|scope|duplicate|execution|base64/i);
    expect(fetchImpl).not.toHaveBeenCalled();
  });

  it("does not mutate when a requested created binding lacks private host state", async () => {
    const fetchImpl = vi.fn();
    await expect(execute(createdDocument(), { project: "APP", temporary: "aw_new" }, { fetchImpl })).rejects.toThrow(/trusted execution/);
    expect(fetchImpl).not.toHaveBeenCalled();
  });

  it("does not bind a failed creation or an out-of-scope returned identifier", async () => {
    const doc = createdDocument();
    const plan = encode(doc);
    const binding = connectorEffectBindings(new Map(), plan);
    for (const reply of [{ id: "OTHER-99" }, {}, { id: "APP-" + "9".repeat(260) }]) {
      await expect(executeConnector(plan, "safe-output", "operation", { project: "APP", temporary: "aw_new" }, { env: { ...env, ...binding.env }, fetchImpl: async () => jsonResponse(reply) })).rejects.toThrow();
    }
    expect(binding.env.GH_AW_CONNECTOR_BINDING_RECEIPTS).toBe("[]");
  });
});

function remoteDocument() {
  const doc = document("agent");
  delete doc.spec.mcp;
  delete doc.binding.mcp;
  doc.spec.endpoints = { "remote-mcp-0": "https://mcp.example.com" };
  doc.spec.httpMcp = {
    registration: {
      url: "https://mcp.example.com/v1/mcp",
      credential: "credential",
      headers: { "X-MCP-Tenant": "fixed" },
      readOnly: true,
      maxResponseBytes: 4096,
      tools: { getIssue: { inputs: { record: { type: "string", required: true, maxLength: 80 } }, resources: { record: "records" } } },
    },
  };
  doc.binding.httpMcp = ["registration"];
  return doc;
}
function remoteFetch(overrides = {}) {
  return vi.fn(async (url, init) => {
    const request = JSON.parse(init.body);
    expect(url).toBe(overrides.url || "https://mcp.example.com/v1/mcp");
    expect(init.headers.Authorization).toBe("Bearer " + env.GH_AW_CONNECTOR_SECRET_TEST_TOKEN);
    expect(init.headers["X-MCP-Tenant"]).toBe("fixed");
    if (request.method === "initialize")
      return jsonResponse({ jsonrpc: "2.0", id: request.id, result: { protocolVersion: "2025-03-26", capabilities: { tools: {} }, serverInfo: { name: "remote", version: "1" } } }, 200, { "Mcp-Session-Id": "bounded-session" });
    expect(init.headers["Mcp-Session-Id"]).toBe("bounded-session");
    expect(init.headers["MCP-Protocol-Version"]).toBe("2025-03-26");
    if (request.method === "notifications/initialized") return new Response(null, { status: 202 });
    expect(request.method).toBe("tools/call");
    expect(request.params).toEqual({ name: "getIssue", arguments: overrides.arguments || { record: "approved" } });
    if (overrides.call) return overrides.call(request);
    return jsonResponse({ jsonrpc: "2.0", id: request.id, result: { content: [{ type: "text", text: "untrusted provider text" }], ...overrides.result } });
  });
}

describe("fixed-endpoint HTTP MCP", () => {
  it.each(["https://mcp.example.com", "https://MCP.EXAMPLE.COM:443/v1/mcp"])("uses the canonical fetch URL for the fixed endpoint %s", async endpoint => {
    const doc = remoteDocument();
    doc.spec.httpMcp.registration.url = endpoint;
    const canonical = new URL(endpoint).href;
    const provider = remoteFetch({ url: canonical });
    const fetchImpl = vi.fn(async (url, init) => {
      expect(url).toBe(canonical);
      const response = await provider(url, init);
      Object.defineProperty(response, "url", { value: canonical });
      return response;
    });
    await expect(callMCP(encode(doc), "registration", "getIssue", { record: "approved" }, { env, fetchImpl })).resolves.toMatchObject({ content: [{ text: "untrusted provider text" }] });
    expect(fetchImpl).toHaveBeenCalledTimes(3);
    expect(fetchImpl.mock.calls.every(([, init]) => init.redirect === "manual")).toBe(true);
  });

  it("rejects a response from a different URL even on the approved origin", async () => {
    const response = jsonResponse({});
    Object.defineProperty(response, "url", { value: "https://mcp.example.com/other" });
    const fetchImpl = vi.fn(async () => response);
    await expect(callMCP(encode(remoteDocument()), "registration", "getIssue", { record: "approved" }, { env, fetchImpl })).rejects.toThrow(/redirects and endpoint changes/);
    expect(fetchImpl).toHaveBeenCalledTimes(1);
  });

  it("initializes, notifies, and calls the exact selected tool with same-endpoint session state", async () => {
    const fetchImpl = remoteFetch();
    const result = await callMCP(encode(remoteDocument()), "registration", "getIssue", { record: "approved" }, { env, fetchImpl });
    expect(result).toEqual({ content: [{ type: "text", text: "untrusted provider text" }] });
    expect(fetchImpl).toHaveBeenCalledTimes(3);
    expect(fetchImpl.mock.calls.every(([, init]) => init.redirect === "manual" && init.credentials === "omit")).toBe(true);
  });

  it("reads a bounded SSE result and closes the stream without waiting for remote EOF", async () => {
    let canceled = false;
    const fetchImpl = remoteFetch({
      call: request =>
        new Response(
          new ReadableStream({
            start(controller) {
              controller.enqueue(Buffer.from(": heartbeat\r\n\r\nevent: message\r\ndata: " + JSON.stringify({ jsonrpc: "2.0", id: request.id, result: { content: [{ type: "text", text: "SSE" }] } }) + "\r\n\r\n"));
            },
            cancel() {
              canceled = true;
            },
          }),
          { headers: { "content-type": "text/event-stream; charset=utf-8" } }
        ),
    });
    await expect(callMCP(encode(remoteDocument()), "registration", "getIssue", { record: "approved" }, { env, fetchImpl })).resolves.toMatchObject({ content: [{ text: "SSE" }] });
    expect(canceled).toBe(true);
  });

  it.each([{ isError: true }, { resultType: "input_required" }, { inputRequests: { authentication: { private: "provider diagnostic" } } }])(
    "rejects remote failures and interactive input without publishing provider content",
    async result => {
      await expect(callMCP(encode(remoteDocument()), "registration", "getIssue", { record: "approved" }, { env, fetchImpl: remoteFetch({ result }) })).rejects.toThrow(/error or requires unsupported interactive/);
    }
  );

  it.each([
    { jsonrpc: "2.0", id: 2, error: { code: 400, message: "private diagnostic" } },
    { jsonrpc: "2.0", id: 999, result: { content: [] } },
    { jsonrpc: "2.0", id: 2, method: "elicitation/create", params: { url: "https://callback.example" } },
  ])("rejects provider JSON-RPC errors, wrong IDs and callback requests", async response => {
    await expect(callMCP(encode(remoteDocument()), "registration", "getIssue", { record: "approved" }, { env, fetchImpl: remoteFetch({ call: () => jsonResponse(response) }) })).rejects.toThrow(/request failed/);
  });

  it("rejects SSE callback requests without invoking them", async () => {
    const fetchImpl = remoteFetch({ call: () => new Response('data: {"jsonrpc":"2.0","id":9,"method":"sampling/createMessage"}\n\n', { headers: { "content-type": "text/event-stream" } }) });
    await expect(callMCP(encode(remoteDocument()), "registration", "getIssue", { record: "approved" }, { env, fetchImpl })).rejects.toThrow(/callbacks are unsupported/);
    expect(fetchImpl).toHaveBeenCalledTimes(3);
  });

  it("enforces remote tool and resource grants before initialization", async () => {
    for (const [tool, args] of [
      ["getIssue", { record: "other" }],
      ["otherTool", { record: "approved" }],
      ["getIssue", { record: "approved", url: "https://evil.example" }],
    ]) {
      const fetchImpl = vi.fn();
      await expect(callMCP(encode(remoteDocument()), "registration", tool, args, { env, fetchImpl })).rejects.toThrow();
      expect(fetchImpl).not.toHaveBeenCalled();
    }
  });

  it("never follows an initialization redirect or retries failed initialization", async () => {
    const fetchImpl = vi.fn(async () => jsonResponse("private diagnostic", 307, { Location: "https://evil.example/mcp" }));
    await expect(callMCP(encode(remoteDocument()), "registration", "getIssue", { record: "approved" }, { env, fetchImpl })).rejects.toThrow(/non-success/);
    expect(fetchImpl).toHaveBeenCalledTimes(1);
  });

  it("rejects unexpected same-endpoint session header changes", async () => {
    const fetchImpl = remoteFetch({ call: request => jsonResponse({ jsonrpc: "2.0", id: request.id, result: { content: [] } }, 200, { "Mcp-Session-Id": "other-session" }) });
    await expect(callMCP(encode(remoteDocument()), "registration", "getIssue", { record: "approved" }, { env, fetchImpl })).rejects.toThrow(/session header/);
  });

  it("rejects unsupported response media types and oversized remote result streams", async () => {
    for (const response of [
      () => new Response('{"jsonrpc":"2.0","id":2,"result":{"content":[]}}', { headers: { "content-type": "text/plain" } }),
      () => new Response("data: " + "x".repeat(4097), { headers: { "content-type": "text/event-stream" } }),
    ]) {
      await expect(callMCP(encode(remoteDocument()), "registration", "getIssue", { record: "approved" }, { env, fetchImpl: remoteFetch({ call: response }) })).rejects.toThrow(/JSON or SSE|byte bound/);
    }
  });
});

describe("stdio MCP and CLI adapters", () => {
  async function protocol(doc, messages, options = {}) {
    let text = "";
    const output = new Writable({
      write(chunk, _encoding, callback) {
        text += chunk.toString();
        callback();
      },
    });
    await serve(encode(doc), Readable.from(messages.map(message => JSON.stringify(message) + "\n")), output, { env, fetchImpl: async () => jsonResponse({ id: "read" }), ...options });
    return text
      .trim()
      .split("\n")
      .map(line => JSON.parse(line));
  }

  it("accepts omitted arguments for no-input tools while rejecting null and missing required inputs", async () => {
    const noInputs = document("agent", { inputs: {}, resources: {}, request: { endpoint: "api", method: "GET", path: "/records" } }, {});
    const fetchImpl = vi.fn(async () => jsonResponse({ id: "read" }));
    const replies = await protocol(
      noInputs,
      [
        { jsonrpc: "2.0", id: 1, method: "tools/call", params: { name: "get-issue" } },
        { jsonrpc: "2.0", id: 2, method: "tools/call", params: { name: "get-issue", arguments: null } },
      ],
      { fetchImpl }
    );
    expect(replies[0].result.isError).toBeUndefined();
    expect(JSON.parse(replies[0].result.content[0].text)).toMatchObject({ untrusted: true, capability: "mcp/operation" });
    expect(replies[1].result.isError).toBe(true);
    expect(fetchImpl).toHaveBeenCalledTimes(1);
    fetchImpl.mockClear();
    const required = await protocol(document("agent"), [{ jsonrpc: "2.0", id: 3, method: "tools/call", params: { name: "get-issue" } }], { fetchImpl });
    expect(required[0].result.isError).toBe(true);
    expect(fetchImpl).not.toHaveBeenCalled();
  });

  it("preserves exact hyphenated REST ToolNames and handles initialization, notifications and ping", async () => {
    const replies = await protocol(document("agent"), [
      { jsonrpc: "2.0", id: 1, method: "initialize", params: { protocolVersion: "2025-03-26" } },
      { jsonrpc: "2.0", method: "notifications/initialized" },
      { jsonrpc: "2.0", id: 2, method: "ping" },
      { jsonrpc: "2.0", id: 3, method: "tools/list" },
      { jsonrpc: "2.0", id: 4, method: "tools/call", params: { name: "get-issue", arguments: { record: "approved" } } },
      { jsonrpc: "2.0", id: 5, method: "tools/call", params: { name: "get_issue", arguments: { record: "approved" } } },
    ]);
    expect(replies).toHaveLength(5);
    expect(replies[2].result.tools[0].name).toBe("get-issue");
    expect(replies[2].result.tools[0].inputSchema).toMatchObject({ additionalProperties: false, required: ["record"] });
    expect(JSON.parse(replies[3].result.content[0].text)).toMatchObject({ untrusted: true, capability: "mcp/operation" });
    expect(replies[4].result.isError).toBe(true);
  });

  it("preserves exact number tokens across stdin, typed requests, response input checks and projections", async () => {
    const doc = document(
      "agent",
      {
        inputs: { value: { type: "number", required: true } },
        resources: {},
        request: { endpoint: "api", method: "POST", path: "/graphql", graphql: "query Read($value: Float!) { value }", query: { value: { input: "value" } } },
        responseInputs: { "/data/value": "value" },
        project: { value: "/data/value" },
      },
      {}
    );
    for (const token of ["1.0", "1e0", "-0"]) {
      let output = "";
      const fetchImpl = vi.fn(async (_url, init) => {
        expect(init.body).toContain('"variables":{"value":' + token + "}");
        return jsonResponse('{"data":{"value":' + token + "}}");
      });
      await serve(
        encode(doc),
        Readable.from(['{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get-issue","arguments":{"value":' + token + "}}}\n"]),
        new Writable({
          write(chunk, _encoding, callback) {
            output += chunk.toString();
            callback();
          },
        }),
        { env, fetchImpl }
      );
      const result = JSON.parse(output);
      expect(result.result.isError).toBeUndefined();
      expect(result.result.content[0].text).toContain('"data":{"value":' + token + "}");
    }
  });

  it("preserves registration__remoteTool names and wraps provider results as untrusted", async () => {
    const replies = await protocol(
      remoteDocument(),
      [
        { jsonrpc: "2.0", id: 1, method: "tools/list" },
        { jsonrpc: "2.0", id: 2, method: "tools/call", params: { name: "registration__getIssue", arguments: { record: "approved" } } },
      ],
      { fetchImpl: remoteFetch() }
    );
    expect(replies[0].result.tools[0].name).toBe("registration__getIssue");
    expect(JSON.parse(replies[1].result.content[0].text)).toMatchObject({ untrusted: true, connector: "example.com/issues", result: { content: [{ text: "untrusted provider text" }] } });
  });

  it("forwards omitted arguments to an exactly selected no-input remote tool as an empty object", async () => {
    const doc = remoteDocument();
    doc.spec.httpMcp.registration.tools.getIssue = { inputs: {} };
    doc.connection.resources = {};
    const fetchImpl = remoteFetch({ arguments: {} });
    const replies = await protocol(doc, [{ jsonrpc: "2.0", id: 1, method: "tools/call", params: { name: "registration__getIssue" } }], { fetchImpl });
    expect(replies[0].result.isError).toBeUndefined();
    expect(JSON.parse(replies[0].result.content[0].text)).toMatchObject({ untrusted: true, result: { content: [{ text: "untrusted provider text" }] } });
    expect(fetchImpl).toHaveBeenCalledTimes(3);
  });

  it("rejects oversized stdio messages", async () => {
    await expect(
      serve(
        encode(document("agent")),
        Readable.from(["x".repeat(256 * 1024 + 1)]),
        new Writable({
          write(_chunk, _encoding, callback) {
            callback();
          },
        })
      )
    ).rejects.toThrow(/byte bound/);
  });

  function cli(args, input = '{"record":"approved"}') {
    return spawnSync(process.execPath, ["connector_runtime.cjs", ...args], { cwd: import.meta.dirname, env: { ...process.env, GH_AW_CONNECTOR_PLAN: encode(document()) }, input, encoding: "utf8", timeout: 5000 });
  }
  it("stages one bounded JSON object as a standalone bundled CLI", () => {
    const result = cli(["invoke", "safe-output", "operation", "--stage"]);
    expect(result.status).toBe(0);
    expect(result.stderr).toBe("");
    expect(JSON.parse(result.stdout)).toMatchObject({ staged: true, request: { operation: "operation", arguments: { record: "approved" } } });
  });

  it.each([
    [["invoke", "safe-output", "operation"], '{"record":"approved"}'],
    [["invoke", "safe-output", "operation", "--stage", "--allow-writes"], '{"record":"approved"}'],
    [["invoke", "safe-output", "operation", "--stage"], '{"record":"approved"} {}'],
    [["invoke", "safe-output", "operation", "--stage"], '{"record":"approved","record":"approved"}'],
    [["invoke", "safe-output", "operation", "--stage"], '{"record":9007199254740993}'],
    [["invoke", "safe-output", "operation", "--stage"], '{"record":"approved","private diagnostic":true}'],
    [["invoke", "safe-output", "operation", "--stage"], "x".repeat(256 * 1024 + 1)],
  ])("fails closed with sanitized stderr and no result for invalid CLI input", (args, input) => {
    const result = cli(args, input);
    expect(result.status).not.toBe(0);
    expect(result.stdout).toBe("");
    expect(result.stderr).not.toContain("private diagnostic");
    expect(result.stderr).not.toContain("test-token");
  });
});
