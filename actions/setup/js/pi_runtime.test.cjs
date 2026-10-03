import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

const { parsePiConfig, preparePiRuntime, nativePiProvider } = await import("./pi_runtime.cjs");
let dir;

beforeEach(() => {
  dir = fs.mkdtempSync(path.join(os.tmpdir(), "pi-runtime-"));
  vi.stubEnv("PI_CODING_AGENT_DIR", path.join(dir, "agent"));
  vi.stubEnv("RUNNER_TEMP", dir);
});
afterEach(() => {
  vi.unstubAllEnvs();
  fs.rmSync(dir, { recursive: true, force: true });
});

describe("Pi runtime configuration", () => {
  it("retains installed package declarations in the same runtime directory", () => {
    fs.mkdirSync(path.join(dir, "agent"));
    fs.writeFileSync(path.join(dir, "agent/settings.json"), '{"packages":["npm:example@1.0.0"]}');
    const result = preparePiRuntime({ settings: { defaultThinkingLevel: "high", defaultProjectTrust: "always" } });
    expect(result.settings.packages).toEqual(["npm:example@1.0.0"]);
    expect(result.settings.defaultTools).toEqual(["+codemode", "+tool_search"]);
    expect(result.settings.defaultProjectTrust).toBe("never");
    expect(result.settings.defaultThinkingLevel).toBe("high");
  });

  it("uses only gateway-authorized MCP servers and preserves scoped headers", () => {
    fs.mkdirSync(path.join(dir, "gh-aw/mcp-config"), { recursive: true });
    fs.writeFileSync(path.join(dir, "gh-aw/mcp-config/mcp-servers.json"), JSON.stringify({ mcpServers: { safeoutputs: { url: "http://gateway/mcp/safeoutputs", headers: { Authorization: "gateway-session-token" } } } }));
    preparePiRuntime({ mcp: { exposure: "codemode", toolExposure: { safeoutputs: { noop: "direct" } } } });
    const mcp = JSON.parse(fs.readFileSync(path.join(dir, "agent/mcp.json"), "utf8"));
    expect(mcp.mcpServers.safeoutputs).toMatchObject({ exposure: "codemode", toolExposure: { noop: "direct" }, headers: { Authorization: "gateway-session-token" } });
    expect(fs.statSync(path.join(dir, "agent/mcp.json")).mode & 0o777).toBe(0o600);
  });

  it("reports malformed settings instead of discarding installed packages", () => {
    fs.mkdirSync(path.join(dir, "agent"));
    fs.writeFileSync(path.join(dir, "agent/settings.json"), "invalid");
    expect(() => preparePiRuntime({})).toThrow();
  });

  it.each(["null", "[]", '{"other":{}}', '{"settings":false}', '{"mcp":{"exposure":"invalid"}}'])("rejects invalid configuration: %s", raw => {
    expect(() => parsePiConfig(raw)).toThrow();
  });

  it("preserves native provider identifiers instead of defaulting to Copilot", () => {
    expect(nativePiProvider("google")).toBe("google");
    expect(nativePiProvider("openrouter")).toBe("openrouter");
    expect(nativePiProvider("codex")).toBe("openai");
  });
});
