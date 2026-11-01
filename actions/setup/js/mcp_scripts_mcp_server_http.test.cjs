import { describe, it, expect, afterAll, afterEach, vi } from "vitest";
import http from "http";
import { spawn } from "child_process";
import fs from "fs";
import path from "path";
import os from "os";
import { startHttpServer } from "./mcp_scripts_mcp_server_http.cjs";

describe("MCP scripts HTTP startup credentials", () => {
  afterEach(() => vi.unstubAllEnvs());

  it.each([undefined, "", "   "])("requires a key before loading configuration (%s)", async apiKey => {
    vi.stubEnv("GH_AW_MCP_SCRIPTS_API_KEY", apiKey);
    await expect(startHttpServer("unused-config.json", { port: 0 })).rejects.toThrow("GH_AW_MCP_SCRIPTS_API_KEY must be set");
  });
});
describe("mcp_scripts_mcp_server_http.cjs integration", () => {
  const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "http-mcp-test-"));
  let serverProcess = null,
    sessionId = null;
  let serverOutput = "";
  const configPath = path.join(tempDir, "test-config.json"),
    handlerPath = path.join(tempDir, "echo-handler.cjs");
  async function makeRequest(payload, additionalHeaders = {}) {
    return new Promise((resolve, reject) => {
      const data = JSON.stringify(payload),
        headers = { "Content-Type": "application/json", Accept: "application/json, text/event-stream", Authorization: "http-integration-test-key", "Content-Length": Buffer.byteLength(data), ...additionalHeaders },
        req = http.request({ hostname: "localhost", port: 4100, path: "/", method: "POST", headers }, res => {
          let responseData = "";
          (res.on("data", chunk => {
            responseData += chunk;
          }),
            res.on("end", () => {
              try {
                resolve({ status: res.statusCode, data: JSON.parse(responseData), headers: res.headers });
              } catch (e) {
                reject(new Error(`Failed to parse response: ${responseData}`));
              }
            }));
        });
      (req.on("error", reject), req.write(data), req.end());
    });
  }
  (fs.writeFileSync(
    handlerPath,
    'let input = "";\nprocess.stdin.on("data", chunk => { input += chunk; });\nprocess.stdin.on("end", () => {\n  const args = JSON.parse(input);\n  const result = { echo: args.message || "empty", timestamp: Date.now() };\n  console.log(JSON.stringify(result));\n});'
  ),
    fs.writeFileSync(
      configPath,
      JSON.stringify({
        serverName: "http-integration-test-server",
        version: "1.0.0",
        tools: [
          {
            name: "echo_tool",
            description: "Echoes the input message",
            inputSchema: { type: "object", properties: { message: { type: "string", description: "Message to echo", minLength: 20 } }, required: ["message"] },
            handler: "echo-handler.cjs",
          },
        ],
      })
    ),
    it("should start HTTP MCP server successfully", { timeout: 15e3 }, async () => {
      serverProcess = spawn("node", ["mcp_scripts_mcp_server_http.cjs", configPath, "--port", (4100).toString()], {
        cwd: process.cwd(),
        env: { ...process.env, DEBUG: "*", GH_AW_MCP_SCRIPTS_API_KEY: "http-integration-test-key" },
        stdio: ["pipe", "pipe", "pipe"],
      });
      (serverProcess.stderr.on("data", chunk => {
        serverOutput += chunk.toString();
      }),
        serverProcess.on("error", error => {
          console.error("Server process error:", error);
        }));
      const ready = await (async function (maxAttempts = 30) {
        for (let i = 0; i < maxAttempts; i++) {
          try {
            const response = await makeRequest({ jsonrpc: "2.0", id: 0, method: "initialize", params: { protocolVersion: "2024-11-05" } });
            if (200 === response.status && response.data.result) return ((sessionId = response.headers["mcp-session-id"]), !0);
          } catch {}
          await new Promise(resolve => setTimeout(resolve, 200));
        }
        return !1;
      })();
      (expect(ready).toBe(!0), expect(serverOutput).toContain("HTTP server listening"));
    }),
    it.each(["", "incorrect-key"])("rejects unauthenticated MCP requests (%s)", async authorization => {
      const response = await makeRequest({ jsonrpc: "2.0", id: 1, method: "tools/list" }, { Authorization: authorization });
      expect(response.status).toBe(401);
    }),
    it("should initialize with proper MCP protocol response", async () => {
      const response = await makeRequest({ jsonrpc: "2.0", id: 1, method: "initialize", params: { protocolVersion: "2024-11-05", clientInfo: { name: "test-client", version: "1.0.0" }, capabilities: {} } });
      (expect(response.status).toBe(200),
        expect(response.data.jsonrpc).toBe("2.0"),
        expect(response.data.id).toBe(1),
        expect(response.data.result).toBeDefined(),
        expect(response.data.result.protocolVersion).toBe("2024-11-05"),
        expect(response.data.result.serverInfo.name).toBe("http-integration-test-server"),
        expect(response.data.result.serverInfo.version).toBe("1.0.0"),
        expect(response.data.result.capabilities.tools).toBeDefined(),
        expect(response.headers["mcp-session-id"]).toBeDefined(),
        (sessionId = response.headers["mcp-session-id"]));
    }),
    it("should respond to GET /health endpoint", async () =>
      new Promise((resolve, reject) => {
        const req = http.request({ hostname: "localhost", port: 4100, path: "/health", method: "GET" }, res => {
          let responseData = "";
          (res.on("data", chunk => {
            responseData += chunk;
          }),
            res.on("end", () => {
              try {
                expect(res.statusCode).toBe(200);
                const data = JSON.parse(responseData);
                (expect(data.status).toBe("ok"), expect(data.server).toBe("http-integration-test-server"), expect(data.version).toBe("1.0.0"), expect(data.tools).toBe(1), resolve());
              } catch (e) {
                reject(e);
              }
            }));
        });
        (req.on("error", reject), req.end());
      })),
    it("should list tools via HTTP", async () => {
      const headers = sessionId ? { "Mcp-Session-Id": sessionId } : {},
        response = await makeRequest({ jsonrpc: "2.0", id: 2, method: "tools/list" }, headers);
      (expect(response.status).toBe(200), expect(response.data.result).toBeDefined(), expect(response.data.result.tools).toBeInstanceOf(Array), expect(response.data.result.tools.length).toBe(1));
      const tool = response.data.result.tools[0];
      (expect(tool.name).toBe("echo_tool"), expect(tool.description).toBe("Echoes the input message"), expect(tool.inputSchema).toBeDefined(), expect(tool.inputSchema.properties.message).toBeDefined());
    }),
    it("should execute tool via HTTP without logging private inputs or echoed outputs", async () => {
      const privateInput = 'private-http-input-"quoted"\nsecond-line';
      const headers = sessionId ? { "Mcp-Session-Id": sessionId } : {},
        response = await makeRequest({ jsonrpc: "2.0", id: 3, method: "tools/call", params: { name: "echo_tool", arguments: { message: privateInput } } }, headers);
      (expect(response.status).toBe(200),
        expect(response.data.result).toBeDefined(),
        expect(response.data.result.content).toBeInstanceOf(Array),
        expect(response.data.result.content.length).toBe(1),
        expect(response.data.result.content[0].type).toBe("text"));
      const result = JSON.parse(response.data.result.content[0].text);
      (expect(result.echo).toBe(privateInput), expect(result.timestamp).toBeDefined());
      expect(serverOutput).not.toContain("private-http-input");
      expect(serverOutput).not.toContain("second-line");
      expect(serverOutput).toContain("[REDACTED]");
    }),
    it("should handle CORS preflight requests", async () =>
      new Promise((resolve, reject) => {
        const req = http.request({ hostname: "localhost", port: 4100, path: "/", method: "OPTIONS" }, res => {
          (expect(res.statusCode).toBe(200),
            expect(res.headers["access-control-allow-origin"]).toBe("*"),
            expect(res.headers["access-control-allow-methods"]).toContain("POST"),
            expect(res.headers["access-control-allow-headers"]).toContain("Content-Type"),
            resolve());
        });
        (req.on("error", reject), req.end());
      })),
    it("should reject invalid HTTP methods", async () =>
      new Promise((resolve, reject) => {
        const req = http.request({ hostname: "localhost", port: 4100, path: "/", method: "PUT" }, res => {
          let data = "";
          (res.on("data", chunk => {
            data += chunk;
          }),
            res.on("end", () => {
              expect(res.statusCode).toBe(405);
              const parsed = JSON.parse(data);
              (expect(parsed.error).toBe("Method not allowed"), resolve());
            }));
        });
        (req.on("error", reject), req.end());
      })),
    it("should handle missing required arguments", async () => {
      const headers = sessionId ? { "Mcp-Session-Id": sessionId } : {},
        response = await makeRequest({ jsonrpc: "2.0", id: 4, method: "tools/call", params: { name: "echo_tool", arguments: {} } }, headers);
      (expect(response.status).toBe(200), expect(response.data.error).toBeDefined(), expect(response.data.error.message).toContain("not allowed"));
    }),
    it("should enforce minLength from tool schema on HTTP calls", async () => {
      const headers = sessionId ? { "Mcp-Session-Id": sessionId } : {},
        response = await makeRequest({ jsonrpc: "2.0", id: 5, method: "tools/call", params: { name: "echo_tool", arguments: { message: "too short" } } }, headers);
      (expect(response.status).toBe(200),
        expect(response.data.error).toBeDefined(),
        expect(response.data.error.code).toBe(-32602),
        expect(response.data.error.message).toContain("Invalid arguments"),
        expect(response.data.error.message).toContain("minimum 20 characters"));
    }),
    afterAll(async () => {
      (serverProcess &&
        (serverProcess.kill("SIGTERM"),
        await new Promise(resolve => {
          (serverProcess.on("close", () => {
            resolve();
          }),
            setTimeout(() => {
              serverProcess && !serverProcess.killed && (serverProcess.kill("SIGKILL"), resolve());
            }, 2e3));
        })),
        tempDir && fs.existsSync(tempDir) && fs.rmSync(tempDir, { recursive: !0, force: !0 }));
    }, 1e4));
});
