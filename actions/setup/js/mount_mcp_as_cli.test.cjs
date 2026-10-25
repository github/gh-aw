// @ts-check
import { afterEach, describe, expect, it, vi } from "vitest";
import fs from "fs";
import http from "http";
import os from "os";
import path from "path";
import { spawnSync } from "node:child_process";

import {
  AWF_GATEWAY_IP,
  buildMCPCLIServersPromptList,
  fetchMCPTools,
  fetchMCPToolsResult,
  clearStaleSafeOutputsRegistrationEvidence,
  fetchMCPToolsWithRetry,
  generateCLIWrapperScript,
  getSafeOutputsGatewayEmptyFlagPath,
  parseMCPResponseBody,
  recoverSafeOutputsToolsIfNeeded,
  toContainerUrl,
  TOOLS_EMPTY_MAX_RETRIES,
  TOOLS_EMPTY_RETRY_DELAY_MS,
} from "./mount_mcp_as_cli.cjs";

describe("mount_mcp_as_cli.cjs", () => {
  it("uses POSIX paths in Bash wrappers generated on Windows", () => {
    const relative = vi.spyOn(path, "relative").mockReturnValueOnce("..\\tools\\fixture.json").mockReturnValueOnce("..\\..\\actions\\mcp_cli_bridge.cjs");
    try {
      const wrapper = generateCLIWrapperScript("fixture", "http://example.test/mcp/fixture", "tools.json", "fixture-agent", "bridge.cjs");
      expect(wrapper).toContain("${CLI_DIR}/../tools/fixture.json");
      expect(wrapper).toContain("${CLI_DIR}/../../actions/mcp_cli_bridge.cjs");
    } finally {
      relative.mockRestore();
      vi.restoreAllMocks();
    }
  });

  it("clears stale safe-output registration evidence without removing other tool caches", () => {
    const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "mount-safeoutputs-cleanup-"));
    const flagPath = path.join(tempDir, "gh-aw/safeoutputs/gateway_empty.flag");
    const toolsPath = path.join(tempDir, "gh-aw/mcp-cli/tools/safeoutputs.json");
    const otherToolsPath = path.join(tempDir, "gh-aw/mcp-cli/tools/github.json");
    fs.mkdirSync(path.dirname(flagPath), { recursive: true });
    fs.mkdirSync(path.dirname(toolsPath), { recursive: true });
    fs.writeFileSync(flagPath, "");
    fs.writeFileSync(toolsPath, "[]");
    fs.writeFileSync(otherToolsPath, "[]");
    try {
      clearStaleSafeOutputsRegistrationEvidence(tempDir);
      expect(fs.existsSync(flagPath)).toBe(false);
      expect(fs.existsSync(toolsPath)).toBe(false);
      expect(fs.existsSync(otherToolsPath)).toBe(true);
    } finally {
      fs.rmSync(tempDir, { recursive: true, force: true });
    }
  });

  it("runs CLI wrappers after the runtime tree is relocated into an NVX workspace", () => {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), "nvx-cli-relocated-"));
    const runtime = path.join(root, "runtime with spaces");
    const originalRoot = path.join(process.env.RUNNER_TEMP || "/home/runner/work/_temp", "gh-aw");
    const bin = path.join(runtime, "mcp-cli", "bin");
    const tools = path.join(runtime, "mcp-cli", "tools", "fixture.json");
    const bridge = path.join(runtime, "actions", "mcp_cli_bridge.cjs");
    try {
      fs.mkdirSync(bin, { recursive: true });
      fs.mkdirSync(path.dirname(tools), { recursive: true });
      fs.mkdirSync(path.dirname(bridge), { recursive: true });
      fs.writeFileSync(tools, "fixture tools");
      fs.writeFileSync(bridge, `const fs = require("fs"); const args = process.argv.slice(2); console.log(fs.readFileSync(args[args.indexOf("--tools-file") + 1], "utf8")); console.log(args.at(-1));`);
      const wrapper = path.join(bin, "fixture");
      fs.writeFileSync(
        wrapper,
        generateCLIWrapperScript("fixture", "http://example.test/mcp/fixture", path.join(originalRoot, "mcp-cli", "tools", "fixture.json"), "fixture-agent", path.join(originalRoot, "actions", "mcp_cli_bridge.cjs")),
        { mode: 0o755 }
      );
      const result = spawnSync(wrapper, ["literal $HOME; $(exit 1)"], { encoding: "utf8", env: { PATH: `${path.dirname(process.execPath)}:/usr/bin:/bin` } });
      expect(result.status, result.stderr).toBe(0);
      expect(result.stdout).toBe("fixture tools\nliteral $HOME; $(exit 1)\n");
      expect(fs.readFileSync(wrapper, "utf8")).not.toContain(originalRoot);
    } finally {
      fs.rmSync(root, { recursive: true, force: true });
    }
  });

  it("parses JSON object responses unchanged", () => {
    const body = { jsonrpc: "2.0", result: { tools: [{ name: "logs" }] } };
    expect(parseMCPResponseBody(body)).toEqual(body);
  });

  it("parses raw JSON string responses", () => {
    const body = '{"jsonrpc":"2.0","result":{"tools":[{"name":"logs"}]}}';
    expect(parseMCPResponseBody(body)).toEqual({
      jsonrpc: "2.0",
      result: { tools: [{ name: "logs" }] },
    });
  });

  it("parses SSE data lines and returns the JSON payload", () => {
    const sseToolListPayload = 'data: {"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"logs","inputSchema":{"properties":{"count":{"type":"integer"}}}}]}}';
    const body = ["event: message", sseToolListPayload, ""].join("\n");

    expect(parseMCPResponseBody(body)).toEqual({
      jsonrpc: "2.0",
      id: 2,
      result: {
        tools: [
          {
            name: "logs",
            inputSchema: {
              properties: {
                count: { type: "integer" },
              },
            },
          },
        ],
      },
    });
  });

  it("recovers after malformed SSE data and ignores non-data and completion lines", () => {
    const result = { jsonrpc: "2.0", id: 2, result: { text: "x".repeat(2 * 1024 * 1024) } };
    const body = ["event: message", 'data: {"broken":}', `data: ${JSON.stringify(result)}`, ' data: {"ignored":true}', "data: [DONE]", "data:"].join("\r\n");
    expect(parseMCPResponseBody(body)).toEqual(result);
  });

  it("rewrites host.docker.internal to the AWF gateway IP for CLI wrappers", () => {
    const originalDomain = process.env.MCP_GATEWAY_DOMAIN;
    const originalPort = process.env.MCP_GATEWAY_PORT;
    process.env.MCP_GATEWAY_DOMAIN = "host.docker.internal";
    process.env.MCP_GATEWAY_PORT = "8080";

    try {
      expect(toContainerUrl("http://0.0.0.0:8080/mcp/safeoutputs")).toBe(`http://${AWF_GATEWAY_IP}:8080/mcp/safeoutputs`);
    } finally {
      if (originalDomain === undefined) {
        delete process.env.MCP_GATEWAY_DOMAIN;
      } else {
        process.env.MCP_GATEWAY_DOMAIN = originalDomain;
      }
      if (originalPort === undefined) {
        delete process.env.MCP_GATEWAY_PORT;
      } else {
        process.env.MCP_GATEWAY_PORT = originalPort;
      }
    }
  });

  it("fails hard even when tools.json has tools, and writes gateway-empty flag", () => {
    const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "mount-safeoutputs-"));
    const fallbackPath = path.join(tempDir, "tools.json");
    fs.writeFileSync(fallbackPath, JSON.stringify([{ name: "create_issue" }]), "utf8");
    const originalPath = process.env.GH_AW_SAFE_OUTPUTS_TOOLS_PATH;
    const originalRunnerTemp = process.env.RUNNER_TEMP;
    // Point at the tools.json to prove the function does not use it even when present
    process.env.GH_AW_SAFE_OUTPUTS_TOOLS_PATH = fallbackPath;
    process.env.RUNNER_TEMP = tempDir;
    try {
      // Must throw regardless of whether a fallback tools.json exists
      expect(() => recoverSafeOutputsToolsIfNeeded([], { warning: () => {} })).toThrow(/safeoutputs tools\/list returned 0 tools/);
      // Flag file must be written so collect_ndjson_output.cjs can detect the outage
      const flagPath = getSafeOutputsGatewayEmptyFlagPath();
      expect(fs.existsSync(flagPath)).toBe(true);
    } finally {
      if (originalPath === undefined) {
        delete process.env.GH_AW_SAFE_OUTPUTS_TOOLS_PATH;
      } else {
        process.env.GH_AW_SAFE_OUTPUTS_TOOLS_PATH = originalPath;
      }
      if (originalRunnerTemp === undefined) {
        delete process.env.RUNNER_TEMP;
      } else {
        process.env.RUNNER_TEMP = originalRunnerTemp;
      }
      fs.rmSync(tempDir, { recursive: true, force: true });
    }
  });

  it("throws when safeoutputs gateway returns 0 tools and writes gateway-empty flag", () => {
    const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "mount-safeoutputs-empty-"));
    const originalPath = process.env.GH_AW_SAFE_OUTPUTS_TOOLS_PATH;
    const originalRunnerTemp = process.env.RUNNER_TEMP;
    process.env.RUNNER_TEMP = tempDir;
    // Point at an explicitly missing file so there is no ambient fallback ambiguity
    process.env.GH_AW_SAFE_OUTPUTS_TOOLS_PATH = path.join(tempDir, "tools.json");
    try {
      expect(() => recoverSafeOutputsToolsIfNeeded([], { warning: () => {} })).toThrow(/safeoutputs tools\/list returned 0 tools/);
      // Flag file must still be written even when the function throws
      const flagPath = getSafeOutputsGatewayEmptyFlagPath();
      expect(fs.existsSync(flagPath)).toBe(true);
    } finally {
      if (originalPath === undefined) {
        delete process.env.GH_AW_SAFE_OUTPUTS_TOOLS_PATH;
      } else {
        process.env.GH_AW_SAFE_OUTPUTS_TOOLS_PATH = originalPath;
      }
      if (originalRunnerTemp === undefined) {
        delete process.env.RUNNER_TEMP;
      } else {
        process.env.RUNNER_TEMP = originalRunnerTemp;
      }
      fs.rmSync(tempDir, { recursive: true, force: true });
    }
  });

  it("does not write gateway-empty flag when tools list is non-empty", () => {
    const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "mount-safeoutputs-ok-"));
    const originalRunnerTemp = process.env.RUNNER_TEMP;
    process.env.RUNNER_TEMP = tempDir;
    try {
      const tools = [{ name: "push_to_pull_request_branch" }];
      const result = recoverSafeOutputsToolsIfNeeded(tools, { warning: () => {} });
      expect(result).toEqual(tools);
      const flagPath = getSafeOutputsGatewayEmptyFlagPath();
      expect(fs.existsSync(flagPath)).toBe(false);
    } finally {
      if (originalRunnerTemp === undefined) {
        delete process.env.RUNNER_TEMP;
      } else {
        process.env.RUNNER_TEMP = originalRunnerTemp;
      }
      fs.rmSync(tempDir, { recursive: true, force: true });
    }
  });

  it("renders schema-derived safeoutputs docs for prompt substitution", () => {
    const docs = buildMCPCLIServersPromptList([
      {
        name: "safeoutputs",
        tools: [
          {
            name: "set_issue_type",
            inputSchema: {
              type: "object",
              properties: {
                issue_number: { type: "integer" },
                issue_type: { type: "string" },
                rationale: { type: "string", maxLength: 280 },
                confidence: { type: "string", enum: ["LOW", "MEDIUM", "HIGH"] },
              },
              required: ["issue_number", "issue_type"],
            },
          },
        ],
      },
      { name: "mcpscripts", tools: [] },
    ]);

    expect(docs).toContain("schema-derived command syntax");
    expect(docs).toContain("--issue_number <number>");
    expect(docs).toContain("[--rationale <reason, max 280 characters>]");
    const recommended = docs.match(/Recommended:\n\s*```bash\n\s*safeoutputs set_issue_type '([^']+)'/)[1];
    expect(JSON.parse(recommended)).toEqual({
      issue_number: 123,
      issue_type: "Bug",
      confidence: "HIGH",
      rationale: "The report describes reproducible incorrect behavior.",
    });
    expect(docs).toContain("`mcpscripts` — run `mcpscripts --help`");
  });

  it("exports retry constants with expected values", () => {
    expect(TOOLS_EMPTY_MAX_RETRIES).toBeGreaterThan(0);
    expect(TOOLS_EMPTY_RETRY_DELAY_MS).toBeGreaterThan(0);
  });

  it("returns tools immediately when fetchFn succeeds on first attempt", async () => {
    const tools = [{ name: "push_to_pull_request_branch" }];
    let callCount = 0;
    const fakeFetch = async () => {
      callCount++;
      return tools;
    };
    const result = await fetchMCPToolsWithRetry(
      "http://localhost/mcp/safeoutputs",
      "key",
      "safeoutputs",
      { warning: () => {} },
      {
        fetchFn: fakeFetch,
        sleep: async () => {},
      }
    );
    expect(result).toEqual(tools);
    expect(callCount).toBe(1);
  });

  it("retries when fetchFn returns empty and succeeds on a later attempt", async () => {
    const tools = [{ name: "push_to_pull_request_branch" }];
    let callCount = 0;
    const fakeFetch = async () => {
      callCount++;
      return callCount < 3 ? [] : tools;
    };
    const warnings = [];
    const result = await fetchMCPToolsWithRetry(
      "http://localhost/mcp/safeoutputs",
      "key",
      "safeoutputs",
      { warning: msg => warnings.push(msg) },
      {
        fetchFn: fakeFetch,
        sleep: async () => {},
      }
    );
    expect(result).toEqual(tools);
    expect(callCount).toBe(3);
    expect(warnings).toHaveLength(2);
    expect(warnings[0]).toContain("tools/list returned 0 tools");
    expect(warnings[0]).toContain("safeoutputs");
  });

  it("stops retrying after TOOLS_EMPTY_MAX_RETRIES, emits final warning, and returns empty when always empty", async () => {
    let callCount = 0;
    const fakeFetch = async () => {
      callCount++;
      return [];
    };
    const warnings = [];
    const result = await fetchMCPToolsWithRetry(
      "http://localhost/mcp/safeoutputs",
      "key",
      "safeoutputs",
      { warning: msg => warnings.push(msg) },
      {
        fetchFn: fakeFetch,
        sleep: async () => {},
      }
    );
    expect(result).toEqual([]);
    // 1 initial attempt + TOOLS_EMPTY_MAX_RETRIES retries
    expect(callCount).toBe(1 + TOOLS_EMPTY_MAX_RETRIES);
    expect(warnings).toHaveLength(TOOLS_EMPTY_MAX_RETRIES + 1);
    expect(warnings[warnings.length - 1]).toContain("still returned 0 tools");
    expect(warnings[warnings.length - 1]).toContain("after");
  });

  it("invokes sleep between retry attempts", async () => {
    let callCount = 0;
    const sleepDelays = [];
    const fakeFetch = async () => {
      callCount++;
      return callCount < 2 ? [] : [{ name: "tool" }];
    };
    await fetchMCPToolsWithRetry(
      "http://localhost/mcp/safeoutputs",
      "key",
      "safeoutputs",
      { warning: () => {} },
      {
        fetchFn: fakeFetch,
        sleep: async ms => {
          sleepDelays.push(ms);
        },
      }
    );
    expect(sleepDelays).toEqual([TOOLS_EMPTY_RETRY_DELAY_MS]);
  });

  it("does not retry when fetchFn reports a non-successful tools/list fetch", async () => {
    let callCount = 0;
    const warnings = [];
    const fakeFetch = async () => {
      callCount++;
      return { tools: [], emptyWasSuccessful: false };
    };
    const result = await fetchMCPToolsWithRetry(
      "http://localhost/mcp/safeoutputs",
      "key",
      "safeoutputs",
      { warning: msg => warnings.push(msg) },
      {
        fetchFn: fakeFetch,
        sleep: async () => {},
      }
    );
    expect(result).toEqual([]);
    expect(callCount).toBe(1);
    expect(warnings).toHaveLength(0);
  });

  it("stops further retries if a later fetchFn call is non-successful", async () => {
    let callCount = 0;
    const warnings = [];
    const fakeFetch = async () => {
      callCount++;
      if (callCount === 1) {
        return [];
      }
      return { tools: [], emptyWasSuccessful: false };
    };
    const result = await fetchMCPToolsWithRetry(
      "http://localhost/mcp/safeoutputs",
      "key",
      "safeoutputs",
      { warning: msg => warnings.push(msg) },
      {
        fetchFn: fakeFetch,
        sleep: async () => {},
      }
    );
    expect(result).toEqual([]);
    expect(callCount).toBe(2);
    expect(warnings).toHaveLength(2);
    expect(warnings[0]).toContain("retrying");
    expect(warnings[1]).toContain("stopping empty tools/list retries");
  });
});

describe("fetchMCPToolsResult handshake and failure classification", () => {
  let server;

  afterEach(async () => {
    if (server) {
      await new Promise(resolve => server.close(resolve));
      server = undefined;
    }
  });

  async function serve(handler) {
    server = http.createServer((req, res) => {
      let data = "";
      req.on("data", chunk => (data += chunk));
      req.on("end", () => handler(req, res, JSON.parse(data)));
    });
    await new Promise(resolve => server.listen(0, "127.0.0.1", resolve));
    return `http://127.0.0.1:${server.address().port}/mcp`;
  }

  function reply(res, body, status = 200, headers = {}) {
    res.writeHead(status, { "Content-Type": "application/json", ...headers });
    res.end(body === undefined ? "" : JSON.stringify(body));
  }

  it.each([{}, { result: null }, { result: [] }, { result: "invalid" }, []])("rejects malformed initialization %j without requesting tools", async body => {
    const methods = [];
    const url = await serve((_req, res, request) => {
      methods.push(request.method);
      reply(res, body);
    });
    expect(await fetchMCPToolsResult(url, "key", { warning: () => {} })).toEqual({ tools: [], emptyWasSuccessful: false, retryable: false, failure: { phase: "initialize", statusCode: 200 } });
    expect(methods).toEqual(["initialize"]);
  });

  it.each(["initialize", "notifications/initialized", "tools/list"])("classifies failures at %s without continuing the handshake", async failedMethod => {
    const failures = [
      { status: 401, body: { message: "credential-sensitive" }, retryable: false },
      { status: 403, body: {}, retryable: false },
      { status: 404, body: {}, retryable: false },
      { status: 429, body: {}, retryable: false },
      { status: 500, body: {}, retryable: true },
      { status: 503, body: {}, retryable: true },
      { status: 200, body: { error: { code: -32600, message: "credential-sensitive" } }, retryable: false },
      { status: 200, body: { error: { code: -32601, message: "credential-sensitive" } }, retryable: false },
      { status: 200, body: { error: { code: -32602, message: "credential-sensitive" } }, retryable: false },
      { status: 200, body: { error: { code: -32000, message: "credential-sensitive" } }, retryable: false },
      { status: 200, body: { error: { code: -32603, message: "credential-sensitive" } }, retryable: true },
      { status: 200, body: {}, retryable: false },
      { status: 200, body: { result: null }, retryable: false },
      { status: 200, body: { result: [] }, retryable: false },
      { status: 200, body: "invalid response", retryable: false },
    ];
    let failure;
    const methods = [];
    const warnings = [];
    const url = await serve((_req, res, request) => {
      methods.push(request.method);
      if (request.method === failedMethod) {
        reply(res, failure.body, failure.status);
      } else if (request.method === "notifications/initialized") {
        reply(res, undefined, 204);
      } else {
        reply(res, { result: { tools: [{ name: "unexpected-tool" }] } });
      }
    });
    for (failure of failures) {
      methods.length = 0;
      expect(await fetchMCPToolsResult(url, "credential-sensitive", { warning: msg => warnings.push(msg) })).toEqual({
        tools: [],
        emptyWasSuccessful: false,
        retryable: failure.retryable,
        failure: { phase: failedMethod, statusCode: failure.status },
      });
      expect(methods.at(-1)).toBe(failedMethod);
    }
    expect(warnings.join("\n")).not.toContain("credential-sensitive");
  });

  it.each(["initialize", "notifications/initialized", "tools/list"])("classifies transport failures at %s as retryable", async failedMethod => {
    const methods = [];
    const url = await serve((req, res, request) => {
      methods.push(request.method);
      if (request.method === failedMethod) {
        req.socket.destroy();
      } else if (request.method === "notifications/initialized") {
        reply(res, undefined, 204);
      } else {
        reply(res, { result: {} });
      }
    });
    expect(await fetchMCPToolsResult(url, "key", { warning: () => {} })).toEqual({ tools: [], emptyWasSuccessful: false, retryable: true, failure: { phase: failedMethod, code: "ECONNRESET" } });
    expect(methods.at(-1)).toBe(failedMethod);
  });

  it.each(["ECONNREFUSED", "credential-sensitive"])("only preserves whitelisted transport codes: %s", async code => {
    const request = vi.spyOn(http, "request").mockImplementation(() => {
      throw Object.assign(new Error("http://credential-sensitive@localhost/private"), { code });
    });
    const warnings = [];
    try {
      const result = await fetchMCPToolsResult("http://localhost/mcp", "credential-sensitive", { warning: msg => warnings.push(msg) });
      expect(result).toEqual({
        tools: [],
        emptyWasSuccessful: false,
        retryable: true,
        failure: { phase: "initialize", code: code === "ECONNREFUSED" ? code : undefined },
      });
      expect(JSON.stringify({ result, warnings })).not.toContain("credential-sensitive");
    } finally {
      request.mockRestore();
    }
  });

  it.each([200, 202, 204])("accepts an empty HTTP %i notification acknowledgment and propagates session headers", async notificationStatus => {
    const requests = [];
    const url = await serve((req, res, request) => {
      requests.push({ method: request.method, headers: req.headers });
      if (request.method === "initialize") {
        reply(res, { result: {} }, 200, { "Mcp-Session-Id": "test-session" });
      } else if (request.method === "notifications/initialized") {
        reply(res, undefined, notificationStatus);
      } else {
        reply(res, { result: { tools: [{ name: "available" }] } });
      }
    });
    expect(await fetchMCPToolsResult(url, "test-auth", { warning: () => {} })).toEqual({ tools: [{ name: "available" }], emptyWasSuccessful: true, retryable: false });
    expect(requests.map(request => request.method)).toEqual(["initialize", "notifications/initialized", "tools/list"]);
    expect(requests.map(request => request.headers.authorization)).toEqual(["test-auth", "test-auth", "test-auth"]);
    expect(requests.map(request => request.headers["mcp-session-id"])).toEqual([undefined, "test-session", "test-session"]);
  });

  it("preserves successful-empty results and the legacy array API", async () => {
    const url = await serve((_req, res, request) => {
      reply(res, { result: request.method === "tools/list" ? { tools: [] } : {} });
    });
    expect(await fetchMCPToolsResult(url, "key", { warning: () => {} })).toEqual({ tools: [], emptyWasSuccessful: true, retryable: false });
    expect(await fetchMCPTools(url, "key", { warning: () => {} })).toEqual([]);
  });

  it("does not make CLI retries for transient failures", async () => {
    const methods = [];
    const url = await serve((_req, res, request) => {
      methods.push(request.method);
      reply(res, {}, 503);
    });
    const sleep = vi.fn();
    expect(await fetchMCPToolsWithRetry(url, "key", "safeoutputs", { warning: () => {} }, { sleep })).toEqual([]);
    expect(methods).toEqual(["initialize"]);
    expect(sleep).not.toHaveBeenCalled();
  });

  it("bounds the entire response duration even while the body keeps arriving", async () => {
    let interval;
    const url = await serve((_req, res) => {
      res.writeHead(200, { "Content-Type": "application/json" });
      res.write(" ");
      interval = setInterval(() => res.write(" "), 5);
      res.on("close", () => clearInterval(interval));
    });
    try {
      expect(await fetchMCPToolsResult(url, "key", { warning: () => {} }, 30)).toEqual({ tools: [], emptyWasSuccessful: false, retryable: true, failure: { phase: "initialize", code: "ETIMEDOUT" } });
    } finally {
      clearInterval(interval);
    }
  }, 1000);
});

describe("mount_mcp_as_cli.cjs main() file permissions", () => {
  /** @type {string | undefined} */
  let tempDir;
  /** @type {http.Server | undefined} */
  let server;

  afterEach(async () => {
    if (server) {
      await new Promise(resolve => server.close(resolve));
      server = undefined;
    }
    if (tempDir) {
      // The bin directory is locked to 0o555 by main(); restore write permissions
      // so the temp directory can be removed during cleanup.
      const binDir = path.join(tempDir, "gh-aw/mcp-cli/bin");
      if (fs.existsSync(binDir)) {
        fs.chmodSync(binDir, 0o755);
      }
      fs.rmSync(tempDir, { recursive: true, force: true });
      tempDir = undefined;
    }
    delete process.env.RUNNER_TEMP;
    delete process.env.MCP_GATEWAY_AGENT_ID;
    delete process.env.MCP_GATEWAY_DOMAIN;
    delete process.env.MCP_GATEWAY_PORT;
    delete global.core;
    vi.resetModules();
  });

  it("rewrites an existing 0o755 CLI wrapper script to owner-only permissions (0o700)", async () => {
    // Minimal fake MCP server that answers initialize / notifications/initialized / tools/list
    server = http.createServer((req, res) => {
      let data = "";
      req.on("data", chunk => (data += chunk));
      req.on("end", () => {
        const parsed = JSON.parse(data);
        if (parsed.method === "tools/list") {
          res.writeHead(200, { "Content-Type": "application/json" });
          res.end(JSON.stringify({ jsonrpc: "2.0", id: parsed.id, result: { tools: [{ name: "echo" }] } }));
        } else {
          res.writeHead(200, { "Content-Type": "application/json" });
          res.end(JSON.stringify({ jsonrpc: "2.0", id: parsed.id, result: {} }));
        }
      });
    });
    await new Promise(resolve => server.listen(0, "127.0.0.1", resolve));
    const address = server.address();
    const port = typeof address === "object" && address ? address.port : 0;

    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "mcp-cli-mount-test-"));
    process.env.RUNNER_TEMP = tempDir;
    process.env.MCP_GATEWAY_AGENT_ID = "super-secret-gateway-agent";
    delete process.env.MCP_GATEWAY_DOMAIN;
    delete process.env.MCP_GATEWAY_PORT;

    const manifestDir = path.join(tempDir, "gh-aw/mcp-cli");
    fs.mkdirSync(manifestDir, { recursive: true });
    fs.writeFileSync(path.join(manifestDir, "manifest.json"), JSON.stringify({ servers: [{ name: "testserver", url: `http://127.0.0.1:${port}/mcp` }] }), "utf8");

    const binDir = path.join(tempDir, "gh-aw/mcp-cli/bin");
    fs.mkdirSync(binDir, { recursive: true });
    const scriptPath = path.join(binDir, "testserver");
    fs.writeFileSync(scriptPath, "#!/usr/bin/env bash\necho preexisting\n", { mode: 0o755 });
    fs.chmodSync(scriptPath, 0o755);

    const infos = [];
    const warnings = [];
    global.core = {
      info: msg => infos.push(msg),
      warning: msg => warnings.push(msg),
      addPath: () => {},
      setOutput: () => {},
    };

    vi.resetModules();
    const mod = await import("./mount_mcp_as_cli.cjs?t=" + Date.now());

    await mod.main();

    expect(fs.existsSync(scriptPath)).toBe(true);

    const mode = fs.statSync(scriptPath).mode & 0o777;
    expect(mode).toBe(0o700);
    expect(mode).not.toBe(0o755);

    const scriptContent = fs.readFileSync(scriptPath, "utf8");
    expect(scriptContent).toContain("super-secret-gateway-agent");
  });
});
