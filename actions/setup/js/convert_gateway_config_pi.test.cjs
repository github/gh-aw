import { describe, expect, it } from "vitest";

const { transformPiEntry } = await import("./convert_gateway_config_pi.cjs");

describe("Pi MCP gateway adapter", () => {
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
