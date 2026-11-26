// @ts-check
import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { createRequire } from "module";
import { mkdtempSync, rmSync, writeFileSync, readFileSync, statSync } from "fs";
import { join } from "path";
import { tmpdir } from "os";

const req = createRequire(import.meta.url);
const { transformGeminiEntry, main } = req("./convert_gateway_config_gemini.cjs");

describe("convert_gateway_config_gemini", () => {
  describe("transformGeminiEntry", () => {
    const urlPrefix = "http://localhost:8080";

    it("removes the type field from the entry", () => {
      const entry = { type: "http", url: "http://old/mcp/github" };
      const result = transformGeminiEntry(entry, urlPrefix);
      expect(result).not.toHaveProperty("type");
    });

    it("rewrites the url to use the configured domain and port", () => {
      const entry = { url: "http://host.docker.internal:80/mcp/github" };
      const result = transformGeminiEntry(entry, urlPrefix);
      expect(result.url).toBe("http://localhost:8080/mcp/github");
    });

    it("preserves all other fields from the entry", () => {
      const entry = {
        type: "http",
        url: "http://old/mcp/server",
        headers: { Authorization: "Bearer token" },
        tools: ["read", "write"],
      };
      const result = transformGeminiEntry(entry, urlPrefix);
      expect(result.headers).toEqual({ Authorization: "Bearer token" });
      expect(result.tools).toEqual(["read", "write"]);
    });

    it("does not mutate the original entry (including nested fields)", () => {
      const entry = {
        type: "http",
        url: "http://old/mcp/github",
        headers: { Authorization: "Bearer token", "X-Custom": "value" },
        tools: ["read", "write"],
      };
      const original = JSON.parse(JSON.stringify(entry));
      transformGeminiEntry(entry, urlPrefix);
      expect(entry).toEqual(original);
    });

    it("handles entries without a url field gracefully", () => {
      const entry = { type: "http", headers: { Authorization: "Bearer x" } };
      const result = transformGeminiEntry(entry, urlPrefix);
      expect(result).not.toHaveProperty("type");
      expect(result).not.toHaveProperty("url");
      expect(result.headers).toEqual({ Authorization: "Bearer x" });
    });

    it("handles entries with non-string url values unchanged", () => {
      const entry = { type: "http", url: 42 };
      const result = transformGeminiEntry(entry, urlPrefix);
      expect(result.url).toBe(42);
    });

    it("works with a different urlPrefix", () => {
      const entry = { url: "http://host.docker.internal:80/mcp/playwright" };
      const result = transformGeminiEntry(entry, "http://host.docker.internal:9090");
      expect(result.url).toBe("http://host.docker.internal:9090/mcp/playwright");
    });

    it("handles entries with empty object", () => {
      const entry = {};
      const result = transformGeminiEntry(entry, urlPrefix);
      expect(result).toEqual({});
    });
  });

  describe("main", () => {
    /** @type {string} */
    let tempDir;
    /** @type {string} */
    let workspace;
    /** @type {string} */
    let gatewayOutputFile;
    /** @type {Record<string, string | undefined>} */
    let savedEnv;

    beforeEach(() => {
      tempDir = mkdtempSync(join(tmpdir(), "gemini-config-test-"));
      workspace = join(tempDir, "workspace");
      gatewayOutputFile = join(tempDir, "gateway-output.json");

      savedEnv = {
        MCP_GATEWAY_OUTPUT: process.env.MCP_GATEWAY_OUTPUT,
        MCP_GATEWAY_DOMAIN: process.env.MCP_GATEWAY_DOMAIN,
        MCP_GATEWAY_HOST_DOMAIN: process.env.MCP_GATEWAY_HOST_DOMAIN,
        MCP_GATEWAY_PORT: process.env.MCP_GATEWAY_PORT,
        GITHUB_WORKSPACE: process.env.GITHUB_WORKSPACE,
        GH_AW_MCP_CLI_SERVERS: process.env.GH_AW_MCP_CLI_SERVERS,
        GH_AW_WEB_FETCH_MCP: process.env.GH_AW_WEB_FETCH_MCP,
        RUNNER_TEMP: process.env.RUNNER_TEMP,
      };

      process.env.MCP_GATEWAY_DOMAIN = "host.docker.internal";
      process.env.MCP_GATEWAY_PORT = "80";
      process.env.MCP_GATEWAY_HOST_DOMAIN = "localhost";
      process.env.GITHUB_WORKSPACE = workspace;
      process.env.GH_AW_MCP_CLI_SERVERS = "[]";
      process.env.RUNNER_TEMP = tempDir;
      delete process.env.GH_AW_WEB_FETCH_MCP;
    });

    afterEach(() => {
      for (const [key, value] of Object.entries(savedEnv)) {
        if (value === undefined) {
          delete process.env[key];
        } else {
          process.env[key] = value;
        }
      }
      rmSync(tempDir, { recursive: true, force: true });
    });

    /**
     * @param {object} mcpServers - MCP servers config to write to the gateway output
     */
    function writeGatewayOutput(mcpServers) {
      writeFileSync(gatewayOutputFile, JSON.stringify({ mcpServers }));
      process.env.MCP_GATEWAY_OUTPUT = gatewayOutputFile;
    }

    it("registers the local web-fetch server only when enabled", () => {
      writeGatewayOutput({});
      expect(JSON.parse(main()).mcpServers).not.toHaveProperty("web-fetch");
      process.env.GH_AW_WEB_FETCH_MCP = "1";
      expect(JSON.parse(main()).mcpServers["web-fetch"]).toEqual({
        command: "node",
        args: [join(tempDir, "gh-aw/actions/web_fetch_mcp_server.cjs")],
      });
    });

    /**
     * @param {unknown} payload - Raw payload to write to the gateway output
     */
    function writeRawGatewayOutput(payload) {
      writeFileSync(gatewayOutputFile, JSON.stringify(payload));
      process.env.MCP_GATEWAY_OUTPUT = gatewayOutputFile;
    }

    it("writes settings.json to .gemini directory in workspace", () => {
      writeGatewayOutput({ github: { url: "http://host.docker.internal:80/mcp/github" } });

      main();

      const settingsPath = join(workspace, ".gemini", "settings.json");
      const settings = JSON.parse(readFileSync(settingsPath, "utf8"));
      expect(settings).toHaveProperty("mcpServers");
      expect(settings).toHaveProperty("context.includeDirectories");
    });

    it("rewrites server URLs to use MCP_GATEWAY_HOST_DOMAIN", () => {
      writeGatewayOutput({ github: { type: "http", url: "http://host.docker.internal:80/mcp/github" } });

      main();

      const settings = JSON.parse(readFileSync(join(workspace, ".gemini", "settings.json"), "utf8"));
      expect(settings.mcpServers.github.url).toBe("http://localhost:80/mcp/github");
    });

    it("removes type field from all server entries", () => {
      writeGatewayOutput({
        github: { type: "http", url: "http://old/mcp/github" },
        playwright: { type: "http", url: "http://old/mcp/playwright" },
      });

      main();

      const settings = JSON.parse(readFileSync(join(workspace, ".gemini", "settings.json"), "utf8"));
      expect(settings.mcpServers.github).not.toHaveProperty("type");
      expect(settings.mcpServers.playwright).not.toHaveProperty("type");
    });

    it("includes /tmp/ in context.includeDirectories", () => {
      writeGatewayOutput({ github: { url: "http://old/mcp/github" } });

      main();

      const settings = JSON.parse(readFileSync(join(workspace, ".gemini", "settings.json"), "utf8"));
      expect(settings.context.includeDirectories).toContain("/tmp/");
    });

    it("filters out CLI-mounted servers", () => {
      writeGatewayOutput({
        github: { url: "http://old/mcp/github" },
        playwright: { url: "http://old/mcp/playwright" },
      });
      process.env.GH_AW_MCP_CLI_SERVERS = JSON.stringify(["playwright"]);

      main();

      const settings = JSON.parse(readFileSync(join(workspace, ".gemini", "settings.json"), "utf8"));
      expect(settings.mcpServers).toHaveProperty("github");
      expect(settings.mcpServers).not.toHaveProperty("playwright");
    });

    it("writes settings.json with 0o600 file permissions", () => {
      writeGatewayOutput({ github: { url: "http://old/mcp/github" } });

      main();

      const settingsPath = join(workspace, ".gemini", "settings.json");
      const mode = statSync(settingsPath).mode & 0o777;
      expect(mode).toBe(0o600);
    });

    it("uses localhost as default when MCP_GATEWAY_HOST_DOMAIN is not set", () => {
      delete process.env.MCP_GATEWAY_HOST_DOMAIN;
      writeGatewayOutput({ server: { url: "http://host.docker.internal:80/mcp/server" } });

      main();

      const settings = JSON.parse(readFileSync(join(workspace, ".gemini", "settings.json"), "utf8"));
      expect(settings.mcpServers.server.url).toBe("http://localhost:80/mcp/server");
    });

    it("handles empty mcpServers gracefully", () => {
      writeGatewayOutput({});

      main();

      const settings = JSON.parse(readFileSync(join(workspace, ".gemini", "settings.json"), "utf8"));
      expect(settings.mcpServers).toEqual({});
      expect(settings.context.includeDirectories).toContain("/tmp/");
    });

    it("handles missing mcpServers key in gateway payload", () => {
      writeRawGatewayOutput({});

      main();

      const settings = JSON.parse(readFileSync(join(workspace, ".gemini", "settings.json"), "utf8"));
      expect(settings.mcpServers).toEqual({});
    });

    it("handles null mcpServers in gateway payload", () => {
      writeRawGatewayOutput({ mcpServers: null });

      main();

      const settings = JSON.parse(readFileSync(join(workspace, ".gemini", "settings.json"), "utf8"));
      expect(settings.mcpServers).toEqual({});
    });

    it("handles array mcpServers in gateway payload", () => {
      writeRawGatewayOutput({ mcpServers: ["server1", "server2"] });

      main();

      const settings = JSON.parse(readFileSync(join(workspace, ".gemini", "settings.json"), "utf8"));
      expect(settings.mcpServers).toEqual({});
    });
  });
});
