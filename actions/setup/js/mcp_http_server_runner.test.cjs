// @ts-check
import { afterEach, describe, it, expect, vi } from "vitest";
import http from "http";
import net from "net";
import { runHttpServer, MAX_REQUEST_BODY_BYTES, REQUEST_BODY_TIMEOUT_MS } from "./mcp_http_server_runner.cjs";

const API_KEY = "runner-test-api-key";

/**
 * Helper: create a minimal mock transport that records requests
 */
function makeMockTransport() {
  return {
    /** @type {Array<{req: unknown, res: unknown, body: unknown}>} */
    calls: [],
    /** @param {unknown} req @param {unknown} res @param {unknown} body */
    async handleRequest(req, res, body) {
      this.calls.push({ req, res, body });
      /** @type {any} */ res.writeHead(200, { "Content-Type": "application/json" });
      /** @type {any} */ res.end(JSON.stringify({ jsonrpc: "2.0", id: 1, result: {} }));
    },
  };
}

/**
 * Helper: create a minimal mock logger (silent)
 */
function makeMockLogger() {
  return {
    debug: vi.fn(),
    debugError: vi.fn(),
  };
}

/**
 * Perform an HTTP request against a running server and collect the response.
 *
 * @param {http.Server} server
 * @param {Object} opts
 * @param {string} opts.method
 * @param {string} opts.path
 * @param {string} [opts.body]
 * @param {string[]} [opts.chunks]
 * @param {boolean} [opts.finish]
 * @param {Record<string,string | undefined>} [opts.headers]
 * @returns {Promise<{statusCode: number, headers: http.IncomingHttpHeaders, body: string}>}
 */
function request(server, { method, path, body, chunks = [], finish = true, headers = {} }) {
  return new Promise((resolve, reject) => {
    const addr = /** @type {import('net').AddressInfo} */ server.address();
    const requestHeaders = { "Content-Type": "application/json", Authorization: API_KEY, ...headers };
    if ("Authorization" in headers && headers.Authorization === undefined) delete requestHeaders.Authorization;
    const req = http.request(
      {
        hostname: "127.0.0.1",
        port: addr.port,
        path,
        method,
        headers: requestHeaders,
      },
      res => {
        let data = "";
        res.on("data", c => (data += c));
        res.on("end", () => resolve({ statusCode: res.statusCode || 0, headers: res.headers, body: data }));
      }
    );
    req.on("error", reject);
    if (body) req.write(body);
    for (const chunk of chunks) req.write(chunk);
    if (finish) req.end();
    else req.flushHeaders();
  });
}

describe("mcp_http_server_runner.cjs - runHttpServer", () => {
  /** @type {http.Server | null} */
  let currentServer = null;

  afterEach(async () => {
    vi.useRealTimers();
    if (currentServer) {
      // Close all keep-alive connections before stopping the server to prevent
      // lingering open sockets from keeping the Node.js event loop alive.
      currentServer.closeAllConnections();
      await new Promise(r => currentServer.close(r));
      currentServer = null;
    }
  });

  it("sets CORS headers on every response", async () => {
    const transport = makeMockTransport();
    const logger = makeMockLogger();
    currentServer = await runHttpServer({
      transport,
      port: 0,
      getHealthPayload: () => ({ status: "ok" }),
      logger,
      serverLabel: "Test",
      apiKey: API_KEY,
    });

    // server.listen(0) picks a random port
    await new Promise(r => currentServer.once("listening", r));
    expect(currentServer.address().address).toBe("127.0.0.1");

    const res = await request(currentServer, { method: "POST", path: "/", body: '{"jsonrpc":"2.0","id":1,"method":"ping"}' });
    expect(res.headers["access-control-allow-origin"]).toBe("*");
    expect(res.headers["access-control-allow-methods"]).toContain("POST");
    expect(res.headers["access-control-allow-headers"]).toContain("Authorization");
  });

  it("responds 200 to OPTIONS preflight without calling transport", async () => {
    const transport = makeMockTransport();
    const logger = makeMockLogger();
    currentServer = await runHttpServer({
      transport,
      port: 0,
      getHealthPayload: () => ({ status: "ok" }),
      logger,
      serverLabel: "Test",
      apiKey: API_KEY,
    });
    await new Promise(r => currentServer.once("listening", r));

    const res = await request(currentServer, { method: "OPTIONS", path: "/", headers: { Authorization: "" } });
    expect(res.statusCode).toBe(200);
    expect(transport.calls).toHaveLength(0);
  });

  it("responds to GET /health with payload from getHealthPayload", async () => {
    const transport = makeMockTransport();
    const logger = makeMockLogger();
    currentServer = await runHttpServer({
      transport,
      port: 0,
      getHealthPayload: () => ({ status: "ok", server: "test-server", version: "2.0.0", tools: 42 }),
      logger,
      serverLabel: "Test",
      apiKey: API_KEY,
    });
    await new Promise(r => currentServer.once("listening", r));

    const res = await request(currentServer, { method: "GET", path: "/health", headers: { Authorization: "" } });
    expect(res.statusCode).toBe(200);
    const payload = JSON.parse(res.body);
    expect(payload.status).toBe("ok");
    expect(payload.server).toBe("test-server");
    expect(payload.version).toBe("2.0.0");
    expect(payload.tools).toBe(42);
  });

  it("responds 405 for non-POST methods other than GET /health and OPTIONS", async () => {
    const transport = makeMockTransport();
    const logger = makeMockLogger();
    currentServer = await runHttpServer({
      transport,
      port: 0,
      getHealthPayload: () => ({ status: "ok" }),
      logger,
      serverLabel: "Test",
      apiKey: API_KEY,
    });
    await new Promise(r => currentServer.once("listening", r));

    const res = await request(currentServer, { method: "PUT", path: "/" });
    expect(res.statusCode).toBe(405);
    const body = JSON.parse(res.body);
    expect(body.error).toBe("Method not allowed");
  });

  it("responds 400 with JSON-RPC error for invalid JSON body", async () => {
    const transport = makeMockTransport();
    const logger = makeMockLogger();
    currentServer = await runHttpServer({
      transport,
      port: 0,
      getHealthPayload: () => ({ status: "ok" }),
      logger,
      serverLabel: "Test",
      apiKey: API_KEY,
    });
    await new Promise(r => currentServer.once("listening", r));

    const res = await request(currentServer, { method: "POST", path: "/", body: "{ not valid json" });
    expect(res.statusCode).toBe(400);
    const body = JSON.parse(res.body);
    expect(body.jsonrpc).toBe("2.0");
    expect(body.error.code).toBe(-32700);
    expect(body.error.message).toContain("Parse error");
  });

  it("delegates valid POST requests to transport.handleRequest with parsed body", async () => {
    const transport = makeMockTransport();
    const logger = makeMockLogger();
    currentServer = await runHttpServer({
      transport,
      port: 0,
      getHealthPayload: () => ({ status: "ok" }),
      logger,
      serverLabel: "Test",
      apiKey: API_KEY,
    });
    await new Promise(r => currentServer.once("listening", r));

    const payload = { jsonrpc: "2.0", id: 5, method: "tools/list" };
    await request(currentServer, { method: "POST", path: "/", body: JSON.stringify(payload) });
    expect(transport.calls).toHaveLength(1);
    expect(transport.calls[0].body).toEqual(payload);
  });

  it("calls configureServer callback with the http.Server instance before binding", async () => {
    const transport = makeMockTransport();
    const logger = makeMockLogger();
    /** @type {any} */
    let capturedServer = null;
    currentServer = await runHttpServer({
      transport,
      port: 0,
      getHealthPayload: () => ({ status: "ok" }),
      logger,
      serverLabel: "Test",
      apiKey: API_KEY,
      configureServer: s => {
        capturedServer = s;
        s.timeout = 0;
        s.headersTimeout = 0;
        s.requestTimeout = 0;
      },
    });
    await new Promise(r => currentServer.once("listening", r));

    expect(capturedServer).toBe(currentServer);
    expect(currentServer.timeout).toBe(0);
    expect(currentServer.headersTimeout).toBe(REQUEST_BODY_TIMEOUT_MS);
    expect(currentServer.requestTimeout).toBe(REQUEST_BODY_TIMEOUT_MS);
  });

  it("returns 500 when transport.handleRequest throws", async () => {
    const logger = makeMockLogger();
    const throwingTransport = {
      async handleRequest(_req, _res, _body) {
        throw new Error("boom");
      },
    };
    currentServer = await runHttpServer({
      transport: throwingTransport,
      port: 0,
      getHealthPayload: () => ({ status: "ok" }),
      logger,
      serverLabel: "Test",
      apiKey: API_KEY,
    });
    await new Promise(r => currentServer.once("listening", r));

    const res = await request(currentServer, { method: "POST", path: "/", body: '{"jsonrpc":"2.0","id":1,"method":"ping"}' });
    expect(res.statusCode).toBe(500);
    const body = JSON.parse(res.body);
    expect(body.error.code).toBe(-32603);
  });

  it.each([undefined, "", "   "])("fails explicitly when the API key is missing or empty (%s)", async apiKey => {
    await expect(runHttpServer({ transport: makeMockTransport(), port: 0, getHealthPayload: () => ({}), logger: makeMockLogger(), serverLabel: "Test", apiKey })).rejects.toThrow("must be set to a non-empty API key");
  });

  it.each([undefined, "", "wrong-key", "runner-test-api-keX", `Bearer ${API_KEY}`])("rejects an unauthorized POST before reading its body (%s)", async authorization => {
    const transport = makeMockTransport();
    currentServer = await runHttpServer({ transport, port: 0, getHealthPayload: () => ({}), logger: makeMockLogger(), serverLabel: "Test", apiKey: API_KEY });
    await new Promise(r => currentServer.once("listening", r));

    const res = await request(currentServer, { method: "POST", path: "/", finish: false, headers: { Authorization: authorization, "Content-Length": "100" } });
    expect(res.statusCode).toBe(401);
    expect(res.headers.connection).toBe("close");
    expect(transport.calls).toHaveLength(0);
  });

  it("rejects an oversized Content-Length without waiting for the body", async () => {
    const transport = makeMockTransport();
    currentServer = await runHttpServer({ transport, port: 0, getHealthPayload: () => ({}), logger: makeMockLogger(), serverLabel: "Test", apiKey: API_KEY });
    await new Promise(r => currentServer.once("listening", r));

    const res = await request(currentServer, { method: "POST", path: "/", finish: false, headers: { "Content-Length": String(MAX_REQUEST_BODY_BYTES + 1) } });
    expect(res.statusCode).toBe(413);
    expect(transport.calls).toHaveLength(0);
  });

  it.each(["not-a-number", "-1", "1.5"])("rejects malformed Content-Length (%s)", async contentLength => {
    const transport = makeMockTransport();
    currentServer = await runHttpServer({ transport, port: 0, getHealthPayload: () => ({}), logger: makeMockLogger(), serverLabel: "Test", apiKey: API_KEY });
    await new Promise(r => currentServer.once("listening", r));

    const res = await request(currentServer, { method: "POST", path: "/", headers: { "Content-Length": contentLength } });
    expect(res.statusCode).toBe(400);
    expect(transport.calls).toHaveLength(0);
  });

  it.each([false, true])("accepts a JSON body at the exact byte limit (chunked: %s)", async chunked => {
    const transport = makeMockTransport();
    currentServer = await runHttpServer({ transport, port: 0, getHealthPayload: () => ({}), logger: makeMockLogger(), serverLabel: "Test", apiKey: API_KEY });
    await new Promise(r => currentServer.once("listening", r));

    expect(MAX_REQUEST_BODY_BYTES).toBe(1_048_576);
    const body = JSON.stringify("x".repeat(MAX_REQUEST_BODY_BYTES - 2));
    expect(Buffer.byteLength(body)).toBe(MAX_REQUEST_BODY_BYTES);
    const res = await request(currentServer, { method: "POST", path: "/", body, headers: chunked ? {} : { "Content-Length": String(MAX_REQUEST_BODY_BYTES) } });
    expect(res.statusCode).toBe(200);
    expect(transport.calls).toHaveLength(1);
  });

  it("bounds actual chunked bytes, including multi-byte characters", async () => {
    const transport = makeMockTransport();
    currentServer = await runHttpServer({ transport, port: 0, getHealthPayload: () => ({}), logger: makeMockLogger(), serverLabel: "Test", apiKey: API_KEY });
    await new Promise(r => currentServer.once("listening", r));

    const chunks = ['"', "\u00e9".repeat(MAX_REQUEST_BODY_BYTES / 2), '"'];
    const res = await request(currentServer, { method: "POST", path: "/", chunks });
    expect(res.statusCode).toBe(413);
    expect(transport.calls).toHaveLength(0);
    expect((await request(currentServer, { method: "POST", path: "/", body: "{}" })).statusCode).toBe(200);
  });

  it("closes a chunked upload one byte above the limit without waiting for its end", async () => {
    const transport = makeMockTransport();
    currentServer = await runHttpServer({ transport, port: 0, getHealthPayload: () => ({}), logger: makeMockLogger(), serverLabel: "Test", apiKey: API_KEY });
    await new Promise(r => currentServer.once("listening", r));
    const incoming = new Promise(resolve => currentServer.once("request", resolve));
    const response = request(currentServer, { method: "POST", path: "/", body: " ".repeat(MAX_REQUEST_BODY_BYTES + 1), finish: false });
    const stream = /** @type {http.IncomingMessage} */ await incoming;
    const closed = new Promise(resolve => stream.socket.once("close", resolve));

    const res = await response;
    await closed;
    expect(res.statusCode).toBe(413);
    expect(JSON.parse(res.body)).toEqual({ error: "Request body too large" });
    expect(res.headers.connection).toBe("close");
    expect(stream.socket.destroyed).toBe(true);
    expect(transport.calls).toHaveLength(0);
    expect((await request(currentServer, { method: "POST", path: "/", body: "{}" })).statusCode).toBe(200);
  });

  it("closes a continuously trickled upload at the absolute 30-second deadline", async () => {
    expect(REQUEST_BODY_TIMEOUT_MS).toBe(30_000);
    const transport = makeMockTransport();
    const logger = makeMockLogger();
    currentServer = await runHttpServer({ transport, port: 0, getHealthPayload: () => ({}), logger, serverLabel: "Test", apiKey: API_KEY });
    await new Promise(r => currentServer.once("listening", r));
    const addr = /** @type {import('net').AddressInfo} */ currentServer.address();
    const socket = net.createConnection({ host: "127.0.0.1", port: addr.port });
    socket.setEncoding("utf8");
    let response = "";
    socket.on("data", chunk => (response += chunk));
    await new Promise((resolve, reject) => {
      socket.once("connect", resolve);
      socket.once("error", reject);
    });
    const closed = new Promise(resolve => socket.once("close", resolve));
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    const incoming = new Promise(resolve => currentServer.once("request", resolve));
    socket.write(`POST / HTTP/1.1\r\nHost: localhost\r\nAuthorization: ${API_KEY}\r\nTransfer-Encoding: chunked\r\n\r\n`);
    const stream = /** @type {http.IncomingMessage} */ await incoming;

    for (const elapsed of [10_000, 10_000, 9_999]) {
      await vi.advanceTimersByTimeAsync(elapsed);
      const received = new Promise(resolve => stream.once("data", resolve));
      socket.write("1\r\n \r\n");
      await received;
      expect(response).toBe("");
      expect(transport.calls).toHaveLength(0);
    }
    await vi.advanceTimersByTimeAsync(1);
    await closed;

    expect(response).toContain("408 Request Timeout");
    expect(response).toContain('{"error":"Request body timeout"}');
    expect(response.toLowerCase()).toContain("connection: close");
    expect(stream.socket.destroyed).toBe(true);
    expect(vi.getTimerCount()).toBe(0);
    expect(transport.calls).toHaveLength(0);
    expect(logger.debugError).toHaveBeenCalled();
    vi.useRealTimers();
    expect((await request(currentServer, { method: "POST", path: "/", body: "{}" })).statusCode).toBe(200);
  });

  it("expires an incomplete upload", async () => {
    const transport = makeMockTransport();
    currentServer = await runHttpServer({ transport, port: 0, getHealthPayload: () => ({}), logger: makeMockLogger(), serverLabel: "Test", apiKey: API_KEY });
    await new Promise(r => currentServer.once("listening", r));
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    const received = new Promise(r => currentServer.once("request", r));
    const response = request(currentServer, { method: "POST", path: "/", body: "{", finish: false });
    await received;
    await vi.advanceTimersByTimeAsync(REQUEST_BODY_TIMEOUT_MS);
    expect((await response).statusCode).toBe(408);
    expect(transport.calls).toHaveLength(0);
  });

  it("clears the upload deadline before a long-running tool executes", async () => {
    let releaseTool = () => {};
    const toolWait = new Promise(resolve => {
      releaseTool = resolve;
    });
    let markEntered = () => {};
    const entered = new Promise(resolve => {
      markEntered = resolve;
    });
    const transport = makeMockTransport();
    const delayedTransport = {
      async handleRequest(req, res, body) {
        markEntered();
        await toolWait;
        await transport.handleRequest(req, res, body);
      },
    };
    currentServer = await runHttpServer({ transport: delayedTransport, port: 0, getHealthPayload: () => ({}), logger: makeMockLogger(), serverLabel: "Test", apiKey: API_KEY });
    await new Promise(r => currentServer.once("listening", r));
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    const response = request(currentServer, { method: "POST", path: "/", body: "{}" });
    await entered;
    expect(vi.getTimerCount()).toBe(0);
    await vi.advanceTimersByTimeAsync(REQUEST_BODY_TIMEOUT_MS + 1);
    releaseTool();
    expect((await response).statusCode).toBe(200);
  });

  it("handles an aborted upload and remains available for subsequent requests", async () => {
    const transport = makeMockTransport();
    const logger = makeMockLogger();
    currentServer = await runHttpServer({ transport, port: 0, getHealthPayload: () => ({}), logger, serverLabel: "Test", apiKey: API_KEY });
    await new Promise(r => currentServer.once("listening", r));
    const incoming = new Promise(resolve => currentServer.once("request", resolve));
    const addr = /** @type {import('net').AddressInfo} */ currentServer.address();
    const req = http.request({ hostname: "127.0.0.1", port: addr.port, method: "POST", headers: { Authorization: API_KEY } });
    const failure = new Promise(resolve => req.once("error", resolve));
    req.write("{");
    const stream = /** @type {http.IncomingMessage} */ await incoming;
    const aborted = new Promise(resolve => stream.once("aborted", resolve));
    req.destroy(new Error("test abort"));
    await failure;
    await aborted;
    expect(transport.calls).toHaveLength(0);
    expect(logger.debugError).toHaveBeenCalled();
    expect((await request(currentServer, { method: "POST", path: "/", body: "{}" })).statusCode).toBe(200);
  });

  it("closes unauthenticated incomplete headers at the header timeout without invoking transport", async () => {
    const transport = makeMockTransport();
    currentServer = await runHttpServer({
      transport,
      port: 0,
      getHealthPayload: () => ({}),
      logger: makeMockLogger(),
      serverLabel: "Test",
      apiKey: API_KEY,
      configureServer: server => {
        server.connectionsCheckingInterval = 10;
      },
    });
    await new Promise(r => currentServer.once("listening", r));
    currentServer.headersTimeout = 100;

    const addr = /** @type {import('net').AddressInfo} */ currentServer.address();
    const socket = net.createConnection({ host: "127.0.0.1", port: addr.port });
    let response = "";
    socket.setEncoding("utf8");
    socket.on("data", chunk => (response += chunk));
    await new Promise((resolve, reject) => {
      socket.once("connect", resolve);
      socket.once("error", reject);
    });
    socket.write("POST / HTTP/1.1\r\nHost: localhost\r\nContent-Length: 100\r\nX-");
    await new Promise(resolve => socket.once("close", resolve));
    socket.destroy();

    expect(response).toContain("408 Request Timeout");
    expect(transport.calls).toHaveLength(0);
  });
});

describe("mcp_http_server_runner.cjs - logStartupError", () => {
  it("re-throws the error after logging", () => {
    const { logStartupError } = require("./mcp_http_server_runner.cjs");
    const err = new Error("test-error");
    expect(() => logStartupError(err, "test-ns", { Port: 3000 })).toThrow("test-error");
  });
});
