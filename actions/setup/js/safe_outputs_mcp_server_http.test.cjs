import { afterEach, describe, it, expect, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

// normalizeMcpToolResult is the shared helper used by BOTH tool-registration
// paths in safe_outputs_mcp_server_http.cjs (predefined tools and dynamic
// safe-job tools). The HTTP server cannot be unit-tested without real
// transport/config bootstrap, so the isError-preservation logic that both
// `server.tool(...)` callbacks rely on is exercised here directly.
const { normalizeMcpToolResult, startHttpServer } = require("./safe_outputs_mcp_server_http.cjs");

describe("safe-outputs HTTP authentication", () => {
  let server;
  let tempDir;

  afterEach(async () => {
    if (server) {
      server.closeAllConnections();
      await new Promise(resolve => server.close(resolve));
      server = undefined;
    }
    vi.unstubAllEnvs();
    if (tempDir) {
      fs.rmSync(tempDir, { recursive: true, force: true });
      tempDir = undefined;
    }
  });

  it.each([undefined, "", "   "])("requires a key before loading configuration (%s)", async apiKey => {
    vi.stubEnv("GH_AW_SAFE_OUTPUTS_API_KEY", apiKey);
    await expect(startHttpServer({ port: 0 })).rejects.toThrow("GH_AW_SAFE_OUTPUTS_API_KEY must be set");
  });

  it("uses the safe-outputs environment key and preserves unauthenticated health checks", async () => {
    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "safeoutputs-http-auth-"));
    const configPath = path.join(tempDir, "config.json");
    const toolsPath = path.join(tempDir, "tools.json");
    fs.writeFileSync(configPath, "{}");
    fs.writeFileSync(toolsPath, "[]");
    vi.stubEnv("GH_AW_SAFE_OUTPUTS_CONFIG_PATH", configPath);
    vi.stubEnv("GH_AW_SAFE_OUTPUTS_TOOLS_PATH", toolsPath);
    vi.stubEnv("GH_AW_SAFE_OUTPUTS_API_KEY", "safeoutputs-http-test-key");
    server = await startHttpServer({ port: 0 });
    await new Promise(resolve => server.once("listening", resolve));
    const url = `http://127.0.0.1:${server.address().port}`;
    expect((await fetch(`${url}/health`)).status).toBe(200);

    for (const authorization of ["", "wrong-key", "safeoutputs-http-test-key"]) {
      const response = await fetch(url, {
        method: "POST",
        headers: { Authorization: authorization, "Content-Type": "application/json", Accept: "application/json" },
        body: JSON.stringify({ jsonrpc: "2.0", id: 1, method: "tools/list" }),
      });
      expect(response.status).toBe(authorization === "safeoutputs-http-test-key" ? 200 : 401);
      await response.json();
    }
    expect(server.timeout).toBe(0);
    expect(server.keepAliveTimeout).toBe(0);
    expect(server.headersTimeout).toBe(30_000);
    expect(server.requestTimeout).toBe(30_000);
  });
});

describe("safe_outputs_mcp_server_http.cjs normalizeMcpToolResult", () => {
  it("preserves isError:true from the handler result", () => {
    const result = {
      content: [{ type: "text", text: JSON.stringify({ result: "error", error: "boom" }) }],
      isError: true,
    };
    expect(normalizeMcpToolResult(result)).toEqual({
      content: result.content,
      isError: true,
    });
  });

  it("reports isError:false for a successful handler result", () => {
    const result = {
      content: [{ type: "text", text: JSON.stringify({ result: "success", id: 1 }) }],
    };
    expect(normalizeMcpToolResult(result)).toEqual({
      content: result.content,
      isError: false,
    });
  });

  it("coerces a truthy non-boolean isError to a boolean", () => {
    const result = { content: [{ type: "text", text: "x" }], isError: "yes" };
    expect(normalizeMcpToolResult(result).isError).toBe(true);
  });

  it("defaults content to an empty array when the handler returns no content", () => {
    expect(normalizeMcpToolResult({ isError: true })).toEqual({ content: [], isError: true });
  });

  it("returns a safe shape for null/undefined results", () => {
    expect(normalizeMcpToolResult(null)).toEqual({ content: [], isError: false });
    expect(normalizeMcpToolResult(undefined)).toEqual({ content: [], isError: false });
  });
});
