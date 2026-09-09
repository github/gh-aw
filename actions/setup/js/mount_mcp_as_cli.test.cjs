// @ts-check
import { afterEach, describe, expect, it, vi } from "vitest";
import fs from "fs";
import http from "http";
import os from "os";
import path from "path";

import {
  AWF_GATEWAY_IP,
  buildMCPCLIServersPromptList,
  fetchMCPToolsWithRetry,
  getSafeOutputsGatewayEmptyFlagPath,
  parseMCPResponseBody,
  recoverSafeOutputsToolsIfNeeded,
  toContainerUrl,
  TOOLS_EMPTY_MAX_RETRIES,
  TOOLS_EMPTY_RETRY_DELAY_MS,
} from "./mount_mcp_as_cli.cjs";

describe("mount_mcp_as_cli.cjs", () => {
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
    expect(docs).toContain('--confidence \"HIGH\"');
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
