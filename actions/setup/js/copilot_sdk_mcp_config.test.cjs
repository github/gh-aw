import { afterEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "node:module";
import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";

const require = createRequire(import.meta.url);
const { MAX_MCP_CONFIG_BYTES, loadCopilotSDKMCPConfig, parseCopilotSDKMCPConfig } = require("./copilot_sdk_mcp_config.cjs");
const { runWithCopilotSDK } = require("./copilot_sdk_session.cjs");
const directories = [];

function writeConfig(content) {
  const directory = mkdtempSync(join(tmpdir(), "gh-aw-sdk-mcp-"));
  directories.push(directory);
  const filename = join(directory, "config.json");
  writeFileSync(filename, content);
  return filename;
}

afterEach(() => {
  for (const directory of directories.splice(0)) rmSync(directory, { recursive: true, force: true });
});

describe("Copilot SDK native MCP configuration", () => {
  it("preserves gateway URLs, credentials, tool restrictions, and converted timeouts", () => {
    const mcpServers = {
      github: { type: "http", url: "http://gateway:8080/mcp/github", headers: { Authorization: "Bearer LOCAL_TEST_ONLY" }, tools: ["get_file_contents"], timeout: 120000 },
      safeoutputs: { type: "http", url: "http://gateway:8080/mcp/safeoutputs", tools: ["*"] },
    };
    const filename = writeConfig(JSON.stringify({ mcpServers }));
    expect(loadCopilotSDKMCPConfig(filename)).toEqual(mcpServers);
  });

  it("normalizes URL-only gateway entries and preserves empty tool lists", () => {
    expect(parseCopilotSDKMCPConfig({ mcpServers: { example: { url: "https://gateway/mcp/example", tools: [] } } })).toEqual({
      example: { type: "http", url: "https://gateway/mcp/example", tools: [] },
    });
  });

  it("supports SSE and omits gateway-only metadata from the SDK payload", () => {
    expect(parseCopilotSDKMCPConfig({ mcpServers: { example: { type: "sse", url: "https://gateway/sse", gateway_metadata: true } } })).toEqual({
      example: { type: "sse", url: "https://gateway/sse" },
    });
  });

  it("accepts the empty native map used when servers are CLI-mounted", () => {
    expect(parseCopilotSDKMCPConfig({ mcpServers: {} })).toEqual({});
  });

  it.each([undefined, "", " ", null])("rejects a missing configuration path: %s", filename => {
    expect(() => loadCopilotSDKMCPConfig(filename)).toThrow("GH_AW_MCP_CONFIG is required");
  });

  it("rejects missing and non-regular files", () => {
    const filename = writeConfig("{}");
    expect(() => loadCopilotSDKMCPConfig(join(filename, "missing"))).toThrow("readable regular file");
    const directory = filename + "-directory";
    mkdirSync(directory);
    expect(() => loadCopilotSDKMCPConfig(directory)).toThrow("readable regular file");
  });

  it("enforces the exact file-size boundary", () => {
    const contents = '{"mcpServers":{}}';
    expect(loadCopilotSDKMCPConfig(writeConfig(contents.padEnd(MAX_MCP_CONFIG_BYTES)))).toEqual({});
    expect(() => loadCopilotSDKMCPConfig(writeConfig(contents.padEnd(MAX_MCP_CONFIG_BYTES + 1)))).toThrow("at most 1 MiB");
  });

  it("does not expose malformed JSON payloads in errors", () => {
    const filename = writeConfig('{"mcpServers":{"secret":"LOCAL_TEST_ONLY",BROKEN');
    expect(() => loadCopilotSDKMCPConfig(filename)).toThrow("invalid JSON");
    expect(() => loadCopilotSDKMCPConfig(filename)).not.toThrow("LOCAL_TEST_ONLY");
  });

  it.each([null, [], {}, { mcpServers: [] }, { mcpServers: null }])("rejects invalid envelopes", value => {
    expect(() => parseCopilotSDKMCPConfig(value)).toThrow("mcpServers object");
  });

  it.each([
    null,
    [],
    { command: "node", args: ["untrusted.cjs"] },
    { type: "stdio", command: "node", url: "http://gateway/mcp" },
    { url: "http://gateway/mcp", env: { TOKEN: "LOCAL_TEST_ONLY" } },
    { url: "not-a-url" },
    { url: "file:///sensitive" },
    { url: "https://LOCAL_TEST_ONLY@example.invalid/mcp" },
    { url: "http://gateway/mcp", tools: "*" },
    { url: "http://gateway/mcp", tools: [""] },
    { url: "http://gateway/mcp", tools: [null] },
    { url: "http://gateway/mcp", headers: [] },
    { url: "http://gateway/mcp", headers: { Authorization: 123 } },
    { url: "http://gateway/mcp", headers: { "bad\nheader": "value" } },
    { url: "http://gateway/mcp", headers: { Authorization: "LOCAL_TEST_ONLY\r\nInjected: yes" } },
    { url: "http://gateway/mcp", timeout: 0 },
    { url: "http://gateway/mcp", timeout: 0.5 },
    { url: "http://gateway/mcp", timeout: 2147483648 },
  ])("rejects unsafe or malformed server definitions without leaking data", entry => {
    const parse = () => parseCopilotSDKMCPConfig({ mcpServers: { example: entry } });
    expect(parse).toThrow();
    expect(parse).not.toThrow("LOCAL_TEST_ONLY");
  });

  it.each(["__proto__", "constructor", "prototype", "../outside", "bad\nname"])("rejects unsafe server name %s", name => {
    const mcpServers = Object.fromEntries([[name, { url: "http://gateway/mcp" }]]);
    expect(() => parseCopilotSDKMCPConfig({ mcpServers })).toThrow("invalid server name");
  });

  it("passes the validated native MCP map into SDK session creation without logging credentials", async () => {
    const filename = writeConfig(JSON.stringify({ mcpServers: { safeoutputs: { url: "http://gateway/mcp/safeoutputs", headers: { Authorization: "Bearer LOCAL_TEST_ONLY" } } } }));
    const mcpServers = loadCopilotSDKMCPConfig(filename);
    let sessionConfig;
    let emit;
    const logger = vi.fn();
    class FakeClient {
      async start() {}
      async createSession(config) {
        sessionConfig = config;
        return {
          sessionId: "mcp-config-fixture",
          on: handler => {
            emit = handler;
            return () => {};
          },
          sendAndWait: async () => {
            emit({ type: "assistant.message", data: { content: "Fixture complete" } });
          },
          disconnect: async () => {},
        };
      }
      async stop() {}
    }
    const stdout = vi.spyOn(process.stdout, "write").mockReturnValue(true);
    try {
      const result = await runWithCopilotSDK({
        sdkUri: "http://127.0.0.1:1",
        prompt: "No real inference",
        logger,
        permissionConfig: { allowedTools: ["read", "safeoutputs"] },
        mcpServers,
        sessionStateBaseDir: dirname(filename),
        sdkModule: {
          CopilotClient: FakeClient,
          RuntimeConnection: { forUri: () => ({}) },
          approveAll: () => ({ kind: "approve-once" }),
        },
      });
      expect(result.exitCode).toBe(0);
      expect(sessionConfig.mcpServers).toEqual(mcpServers);
      expect(JSON.stringify(logger.mock.calls)).not.toContain("LOCAL_TEST_ONLY");
    } finally {
      stdout.mockRestore();
    }
  });
});
