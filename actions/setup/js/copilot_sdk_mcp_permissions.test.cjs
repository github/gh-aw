import { describe, expect, it, vi } from "vitest";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { buildCopilotSDKPermissionHandler } = require("./copilot_sdk_permissions.cjs");
const metadata = [{ name: "github-get_file_contents", description: "Read repository content", mcpServerName: "github", mcpToolName: "get_file_contents" }];
const request = (serverName, toolName) => ({ kind: "mcp", serverName, toolName });

describe("verified native MCP permission identities", () => {
  it("authorizes the SDK wire name using the raw compiler permission", () => {
    const handler = buildCopilotSDKPermissionHandler({ allowedTools: ["github(get_file_contents)"] }, vi.fn(), { getMCPToolMetadata: () => metadata });
    expect(handler(request("github", "github-get_file_contents"))).toEqual({ kind: "approve-once" });
  });

  it("does not guess namespace prefixes or accept unregistered aliases", () => {
    const handler = buildCopilotSDKPermissionHandler({ allowedTools: ["github(get_file_contents)"] }, vi.fn(), { getMCPToolMetadata: () => metadata });
    for (const name of ["get_file_contents", "github/get_file_contents", "github-github-get_file_contents", "github-delete_file"]) expect(handler(request("github", name)).kind).toBe("reject");
  });

  it("requires the canonical server identity even with a server-wide grant", () => {
    const handler = buildCopilotSDKPermissionHandler({ allowedTools: ["github", "other"] }, vi.fn(), { getMCPToolMetadata: () => metadata });
    expect(handler(request("github", "github-get_file_contents")).kind).toBe("approve-once");
    expect(handler(request("other", "github-get_file_contents")).kind).toBe("reject");
    expect(handler(request("github", "github-delete_file")).kind).toBe("reject");
  });

  it("fails closed before the verified catalog is installed", () => {
    let catalog = [];
    const handler = buildCopilotSDKPermissionHandler({ allowedTools: ["github"] }, vi.fn(), { getMCPToolMetadata: () => catalog });
    expect(handler(request("github", "github-get_file_contents")).kind).toBe("reject");
    catalog = metadata;
    expect(handler(request("github", "github-get_file_contents")).kind).toBe("approve-once");
  });

  it("cannot confuse a prefixed raw tool name with a different tool", () => {
    const catalog = [...metadata, { name: "github-github-get_file_contents", description: "Distinct raw tool", mcpServerName: "github", mcpToolName: "github-get_file_contents" }];
    const handler = buildCopilotSDKPermissionHandler({ allowedTools: ["github(get_file_contents)"] }, vi.fn(), { getMCPToolMetadata: () => catalog });
    expect(handler(request("github", "github-get_file_contents")).kind).toBe("approve-once");
    expect(handler(request("github", "github-github-get_file_contents")).kind).toBe("reject");
  });

  it("preserves the raw-name legacy API when no catalog callback is supplied", () => {
    const handler = buildCopilotSDKPermissionHandler({ allowedTools: ["github(get_file_contents)"] }, vi.fn());
    expect(handler(request("github", "get_file_contents")).kind).toBe("approve-once");
    expect(handler(request("github", "delete_file")).kind).toBe("reject");
  });
});
