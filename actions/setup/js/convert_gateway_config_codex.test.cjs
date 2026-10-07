// @ts-check
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { createRequire } from "module";
import fs from "fs";
import { mkdtempSync, rmSync, writeFileSync } from "fs";
import { spawnSync } from "child_process";
import { join } from "path";
import { tmpdir } from "os";

const req = createRequire(import.meta.url);
const { toCodexTomlSection, main } = req("./convert_gateway_config_codex.cjs");

describe("convert_gateway_config_codex", () => {
  describe("toCodexTomlSection", () => {
    it("emits a TOML section with the correct server URL", () => {
      const toml = toCodexTomlSection("github", { headers: { Authorization: "token abc" } }, "http://172.30.0.1:80");
      expect(toml).toContain("[mcp_servers.github]");
      expect(toml).toContain('url = "http://172.30.0.1:80/mcp/github"');
    });

    it("includes the Authorization header in http_headers", () => {
      const toml = toCodexTomlSection("myserver", { headers: { Authorization: "******" } }, "http://host:80");
      expect(toml).toContain('http_headers = { Authorization = "******" }');
    });

    it("emits an empty Authorization when no headers present", () => {
      const toml = toCodexTomlSection("noauth", {}, "http://host:80");
      expect(toml).toContain('http_headers = { Authorization = "" }');
    });

    it("ignores non-string header values", () => {
      const toml = toCodexTomlSection("srv", { headers: { Authorization: 123, Other: "keep" } }, "http://host:80");
      expect(toml).toContain('http_headers = { Authorization = "" }');
    });
  });

  describe("main", () => {
    /** @type {string} */
    let tempDir;
    /** @type {string} */
    let gatewayOutputFile;
    /** @type {Record<string, string | undefined>} */
    let savedEnv;
    /** @type {string[]} */
    let savedArgv;

    beforeEach(() => {
      tempDir = mkdtempSync(join(tmpdir(), "codex-config-test-"));
      gatewayOutputFile = join(tempDir, "gateway-output.json");

      savedEnv = {
        CODEX_HOME: process.env.CODEX_HOME,
        MCP_GATEWAY_OUTPUT: process.env.MCP_GATEWAY_OUTPUT,
        MCP_GATEWAY_DOMAIN: process.env.MCP_GATEWAY_DOMAIN,
        MCP_GATEWAY_PORT: process.env.MCP_GATEWAY_PORT,
        RUNNER_TEMP: process.env.RUNNER_TEMP,
        GH_AW_MCP_CLI_SERVERS: process.env.GH_AW_MCP_CLI_SERVERS,
      };
      savedArgv = process.argv;

      process.env.MCP_GATEWAY_DOMAIN = "host.docker.internal";
      process.env.MCP_GATEWAY_PORT = "80";
      process.env.RUNNER_TEMP = tempDir;
      process.env.GH_AW_MCP_CLI_SERVERS = "[]";
    });

    afterEach(() => {
      vi.restoreAllMocks();
      process.argv = savedArgv;
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

    it("reads the direct-tool catalog as a Buffer before decoding UTF-8", () => {
      const catalog = { models: [{ slug: "fixture-model", tool_mode: "code_mode_only", instructions: "\u00e9".repeat(70000) }] };
      const read = vi.spyOn(fs, "readFileSync").mockReturnValueOnce(Buffer.from(JSON.stringify(catalog)));
      process.env.CODEX_HOME = join(tempDir, "codex-home");
      process.argv = [...process.argv, "--direct-tools"];

      const output = main();

      expect(read).toHaveBeenNthCalledWith(1, 0);
      expect(JSON.parse(output)).toEqual({ models: [{ ...catalog.models[0], tool_mode: "direct" }] });
      const catalogPath = join(process.env.CODEX_HOME, "models.json");
      expect(fs.readFileSync(catalogPath, "utf8")).toBe(output);
      expect(fs.statSync(catalogPath).mode & 0o777).toBe(0o600);
    });

    it("converts a catalog piped after more than 64 KiB of short reads", () => {
      const result = spawnSync(
        "bash",
        [
          "-o",
          "pipefail",
          "-c",
          `"$1" -e '
let count = 0;
const timer = setInterval(() => {
  process.stdout.write(" ".repeat(4000));
  if (++count === 24) {
    clearInterval(timer);
    process.stdout.write(JSON.stringify({models: [{slug: "fixture-model", tool_mode: "code_mode_only", instructions: "x".repeat(400000)}]}));
  }
}, 5);
' | "$1" "$2" --direct-tools`,
          "--",
          process.execPath,
          new URL("./convert_gateway_config_codex.cjs", import.meta.url).pathname,
        ],
        { env: { ...process.env, CODEX_HOME: tempDir }, encoding: "utf8", timeout: 10000 }
      );

      expect(result.error).toBeUndefined();
      expect(result.status, result.stderr).toBe(0);
      const catalog = JSON.parse(fs.readFileSync(join(tempDir, "models.json"), "utf8"));
      expect(catalog.models).toEqual([{ slug: "fixture-model", tool_mode: "direct", instructions: "x".repeat(400000) }]);
    });

    it("resolves host.docker.internal to 172.30.0.1 in TOML server URLs", () => {
      writeGatewayOutput({
        github: { url: "http://host.docker.internal:80/mcp/github", headers: { Authorization: "token abc" } },
      });

      const toml = main();

      expect(toml).toContain('url = "http://172.30.0.1:80/mcp/github"');
      expect(toml).not.toContain("host.docker.internal");
    });

    it("uses the domain directly when it is not host.docker.internal", () => {
      process.env.MCP_GATEWAY_DOMAIN = "gateway.internal";
      writeGatewayOutput({
        github: { url: "http://gateway.internal:80/mcp/github", headers: { Authorization: "token" } },
      });

      const toml = main();

      expect(toml).toContain('url = "http://gateway.internal:80/mcp/github"');
    });

    it("filters out CLI-mounted servers before serializing", () => {
      writeGatewayOutput({
        github: { url: "http://host.docker.internal:80/mcp/github", headers: {} },
        playwright: { url: "http://host.docker.internal:80/mcp/playwright", headers: {} },
      });
      process.env.GH_AW_MCP_CLI_SERVERS = JSON.stringify(["playwright"]);

      const toml = main();

      expect(toml).toContain("[mcp_servers.github]");
      expect(toml).not.toContain("[mcp_servers.playwright]");
    });

    it("includes TOML persistence header in output", () => {
      writeGatewayOutput({ github: { url: "http://host.docker.internal:80/mcp/github", headers: {} } });

      const toml = main();

      expect(toml).toContain('[history]\npersistence = "none"');
    });
  });
});
