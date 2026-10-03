import { describe, expect, it } from "vitest";
const { timeoutMilliseconds, applyClaudeRuntimeTimeouts, applyGatewayRuntimeTimeouts } = require("./tool_timeouts.cjs");

describe("runtime tool timeouts", () => {
  it("converts evaluated workflow inputs into every Claude native timeout", () => {
    const env = { GH_AW_STARTUP_TIMEOUT: "180", GH_AW_TOOL_TIMEOUT: "90", MCP_TIMEOUT: "120000", MCP_TOOL_TIMEOUT: "60000" };
    applyClaudeRuntimeTimeouts(env);
    expect(env).toMatchObject({ MCP_TIMEOUT: "180000", MCP_TOOL_TIMEOUT: "90000", BASH_DEFAULT_TIMEOUT_MS: "90000", BASH_MAX_TIMEOUT_MS: "90000" });
  });
  it("preserves defaults and explicit native values when no workflow value is provided", () => {
    const env = { MCP_TIMEOUT: "42" };
    applyClaudeRuntimeTimeouts(env);
    expect(env).toEqual({ MCP_TIMEOUT: "42" });
  });
  it.each(["0", "-1", "abc", "${{ inputs.timeout }}", "1.5", "2147484"])("rejects invalid resolved timeout %s", raw => {
    expect(() => timeoutMilliseconds(raw, "tools.timeout")).toThrow("tools.timeout");
  });
  it("configures the gateway and its health budget from evaluated input", () => {
    const env = { GH_AW_STARTUP_TIMEOUT: "180", GH_AW_TOOL_TIMEOUT: "90" };
    const config = { gateway: { startupTimeout: 120 } };
    applyGatewayRuntimeTimeouts(config, env);
    expect(config.gateway).toEqual({ startupTimeout: 180, toolTimeout: "90s" });
    expect(env.GH_AW_MCP_GATEWAY_BACKEND_STARTUP_TIMEOUT_MS).toBe("180000");
  });
  it("preserves the more specific gateway tool timeout", () => {
    const config = { gateway: { toolTimeout: "2m" } };
    applyGatewayRuntimeTimeouts(config, { GH_AW_TOOL_TIMEOUT: "90" });
    expect(config.gateway.toolTimeout).toBe("2m");
  });
});
