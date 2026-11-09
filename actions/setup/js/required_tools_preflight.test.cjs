import { describe, expect, it, vi } from "vitest";
import { preflightRequiredTools } from "./required_tools_preflight.cjs";

describe("required tool preflight", () => {
  const output = { mcpServers: { github: {}, safeoutputs: {} } };
  const core = { info: vi.fn() };
  const required = { github: ["issue_read", "actions_list"] };

  it("checks actual exposed tools without expanding configuration", async () => {
    const fetch = vi.fn().mockResolvedValue({ tools: [{ name: "issue_read" }, { name: "actions_list" }], emptyWasSuccessful: true });
    await preflightRequiredTools(output, required, "8080", "test-agent", core, fetch);
    expect(fetch).toHaveBeenCalledWith("http://localhost:8080/mcp/github", "test-agent", core, 10000, required.github);
    expect(required).toEqual({ github: ["issue_read", "actions_list"] });
  });

  it("fails before the agent for a filtered or unavailable tool", async () => {
    const fetch = vi.fn().mockResolvedValue({ tools: [{ name: "issue_read" }], emptyWasSuccessful: true });
    await expect(preflightRequiredTools(output, required, "8080", "test-agent", core, fetch)).rejects.toThrow('ERR_CONFIG: Required-tool preflight: "github" does not expose required tool(s): actions_list.');
  });

  it("treats a successful empty tool list as a configuration mismatch", async () => {
    const fetch = vi.fn().mockResolvedValue({ tools: [], emptyWasSuccessful: true });
    await expect(preflightRequiredTools(output, required, "8080", "test-agent", core, fetch)).rejects.toThrow("issue_read, actions_list");
  });

  it("distinguishes authentication and transport failures from missing tools", async () => {
    const fetch = vi.fn().mockResolvedValue({ tools: [], emptyWasSuccessful: false });
    await expect(preflightRequiredTools(output, required, "8080", "test-agent", core, fetch)).rejects.toThrow("ERR_SYSTEM: Required-tool preflight could not list tools");
  });

  it("rejects a missing server without making a network request", async () => {
    const fetch = vi.fn();
    await expect(preflightRequiredTools(output, { inventory: ["lookup"] }, "8080", "test-agent", core, fetch)).rejects.toThrow('server "inventory" is not exposed');
    expect(fetch).not.toHaveBeenCalled();
  });

  it("checks safe output requirements as well as read tools", async () => {
    const fetch = vi.fn().mockResolvedValue({ tools: [{ name: "noop" }], emptyWasSuccessful: true });
    await expect(preflightRequiredTools(output, { safeoutputs: ["add_comment"] }, "8080", "test-agent", core, fetch)).rejects.toThrow("add_comment");
  });

  it("does not probe workflows without explicit requirements", async () => {
    const fetch = vi.fn();
    await preflightRequiredTools(output, {}, "8080", "test-agent", core, fetch);
    expect(fetch).not.toHaveBeenCalled();
  });

  it("rejects invalid names instead of constructing a request path", async () => {
    await expect(preflightRequiredTools(output, { "../github": ["issue_read"] }, "8080", "test-agent", core)).rejects.toThrow("Invalid required MCP tools");
  });

  it.each([null, [], "github"])("rejects invalid requirement maps: %j", async required => {
    await expect(preflightRequiredTools(output, required, "8080", "test-agent", core)).rejects.toThrow("ERR_CONFIG: Invalid required MCP tools configuration");
  });
});
