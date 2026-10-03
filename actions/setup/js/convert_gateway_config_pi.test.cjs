import { describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

const { transformPiEntry, main } = await import("./convert_gateway_config_pi.cjs");

describe("Pi MCP gateway adapter", () => {
  it.each([
    ["1", true],
    ["", false],
  ])("retains infrastructure MCP tools only in native mode (%s)", (native, included) => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), "pi-mcp-converter-"));
    const input = path.join(dir, "gateway.json");
    fs.writeFileSync(input, JSON.stringify({ mcpServers: { safeoutputs: { url: "http://localhost:80/mcp/safeoutputs", headers: { Authorization: "scoped-test-token" } } } }));
    vi.stubEnv("RUNNER_TEMP", dir);
    vi.stubEnv("MCP_GATEWAY_OUTPUT", input);
    vi.stubEnv("MCP_GATEWAY_DOMAIN", "host.docker.internal");
    vi.stubEnv("MCP_GATEWAY_PORT", "8080");
    vi.stubEnv("GH_AW_MCP_CLI_SERVERS", '["safeoutputs"]');
    vi.stubEnv("GH_AW_PI_NATIVE_MCP", native);
    try {
      const result = JSON.parse(main());
      expect(Object.hasOwn(result.mcpServers, "safeoutputs")).toBe(included);
      if (included) expect(result.mcpServers.safeoutputs.headers).toEqual({ Authorization: "scoped-test-token" });
    } finally {
      vi.unstubAllEnvs();
      fs.rmSync(dir, { recursive: true, force: true });
    }
  });

  it("connects to the policy gateway with deferred exposure and scoped authentication", () => {
    const input = { url: "http://localhost:80/mcp/safeoutputs", headers: { Authorization: "gateway-session-token" }, tools: ["noop"] };
    expect(transformPiEntry(input, "http://host.docker.internal:8080")).toEqual({
      type: "http",
      url: "http://host.docker.internal:8080/mcp/safeoutputs",
      headers: input.headers,
      exposure: "deferred",
    });
    expect(input.tools).toEqual(["noop"]);
  });
});
