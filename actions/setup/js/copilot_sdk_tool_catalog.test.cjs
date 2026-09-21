import { describe, expect, it, vi } from "vitest";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { isAuthorizedMCPTool, restrictCopilotSDKRepositoryCatalog } = require("./copilot_sdk_tool_catalog.cjs");

class ToolSet {
  items = [];
  addMcp(name) {
    this.items.push(`mcp:${name}`);
    return this;
  }
  toArray() {
    return [...this.items];
  }
}

const native = name => ({ name, description: "Synthetic native tool" });
const mcp = (server, raw, wire = `${server}-${raw}`) => ({ name: wire, description: "Synthetic MCP tool", mcpServerName: server, mcpToolName: raw });
const required = [native("view"), native("grep"), native("glob"), native("edit"), native("create"), native("go_repository"), mcp("safeoutputs", "noop"), mcp("safeoutputs", "create_pull_request")];
const nativeSelectors = ["builtin:view", "builtin:rg", "builtin:glob", "builtin:edit", "builtin:create", "custom:go_repository"];

function fixture({ initial = required, restricted = initial, servers = {}, permissions = ["read", "write", "go_repository", "safeoutputs"] } = {}) {
  return {
    session: {
      rpc: {
        tools: {
          initializeAndValidate: vi.fn().mockResolvedValue({}),
          getCurrentMetadata: vi.fn().mockResolvedValueOnce({ tools: initial }).mockResolvedValue({ tools: restricted }),
        },
        options: { update: vi.fn().mockResolvedValue({ success: true }) },
      },
    },
    options: {
      ToolSet,
      availableTools: { toArray: () => [...nativeSelectors, "mcp:*"] },
      allowedTools: permissions,
      mcpServers: { safeoutputs: { type: "http", url: "http://safeoutputs.invalid/mcp", tools: ["*"] }, ...servers },
    },
  };
}

describe("native MCP authorization", () => {
  it("joins source metadata to scoped permission identifiers, not wire-name guesses", () => {
    const tool = mcp("github", "get_file_contents", "verified-canonical-name");
    const servers = { github: { type: "http", url: "https://github.invalid/mcp" } };
    expect(isAuthorizedMCPTool(tool, new Set(["github(get_file_contents)"]), servers)).toBe(true);
    expect(isAuthorizedMCPTool(tool, new Set(["verified-canonical-name"]), servers)).toBe(false);
    expect(isAuthorizedMCPTool(tool, new Set(["github"]), {})).toBe(false);
    expect(isAuthorizedMCPTool(native("github-get_file_contents"), new Set(["github"]), servers)).toBe(false);
  });

  it("intersects server-level permissions with configured tool restrictions", () => {
    const servers = { github: { tools: ["get_file_contents"] } };
    expect(isAuthorizedMCPTool(mcp("github", "get_file_contents"), new Set(["github"]), servers)).toBe(true);
    expect(isAuthorizedMCPTool(mcp("github", "delete_file"), new Set(["github"]), servers)).toBe(false);
  });
});

describe("repository SDK catalog preflight", () => {
  it("replaces the discovery wildcard with exact source-qualified MCP names", async () => {
    const allowed = mcp("github", "get_file_contents", "actual-native-read");
    const denied = mcp("github", "delete_file");
    const unconfigured = mcp("ambient", "execute");
    const { session, options } = fixture({
      initial: [...required, allowed, denied, unconfigured],
      restricted: [...required, allowed],
      servers: { github: { type: "http", url: "http://github.invalid/mcp", tools: ["*"] } },
      permissions: ["read", "write", "go_repository", "safeoutputs", "github(get_file_contents)"],
    });
    const result = await restrictCopilotSDKRepositoryCatalog(session, options);
    expect(result).toEqual([...required, allowed]);
    expect(session.rpc.options.update).toHaveBeenCalledExactlyOnceWith({
      availableTools: [...nativeSelectors, "mcp:safeoutputs-noop", "mcp:safeoutputs-create_pull_request", "mcp:actual-native-read"],
    });
    expect(session.rpc.tools.initializeAndValidate).toHaveBeenCalledTimes(2);
  });

  it.each(["bash", "read_bash", "write_bash", "powershell", "task", "read_agent", "sql", "web_fetch"])("rejects an unrequested native %s tool", async name => {
    const { session, options } = fixture({ restricted: [...required, native(name)] });
    await expect(restrictCopilotSDKRepositoryCatalog(session, options)).rejects.toThrow("unrequested native");
  });

  it("rejects an SDK that keeps unauthorized MCP tools visible", async () => {
    const extra = mcp("ambient", "execute");
    const { session, options } = fixture({ initial: [...required, extra], restricted: [...required, extra] });
    await expect(restrictCopilotSDKRepositoryCatalog(session, options)).rejects.toThrow("unauthorized MCP tool");
  });

  it("rejects source-identity changes under an approved canonical name", async () => {
    const { session, options } = fixture({ restricted: [...required.slice(0, -1), mcp("safeoutputs", "other", "safeoutputs-create_pull_request")] });
    await expect(restrictCopilotSDKRepositoryCatalog(session, options)).rejects.toThrow("unauthorized MCP tool");
  });

  it.each(["view", "grep", "glob", "go_repository", "safeoutputs-noop", "safeoutputs-create_pull_request"])("requires %s before inference", async name => {
    const { session, options } = fixture({ initial: required.filter(tool => tool.name !== name) });
    await expect(restrictCopilotSDKRepositoryCatalog(session, options)).rejects.toThrow("missing required");
  });

  it("requires usable native editing, not just a write permission", async () => {
    const { session, options } = fixture({ initial: required.filter(tool => !["edit", "create"].includes(tool.name)) });
    await expect(restrictCopilotSDKRepositoryCatalog(session, options)).rejects.toThrow("editing tools");
  });

  it("fails before inference when an authorized configured MCP server has no tools", async () => {
    const { session, options } = fixture({
      servers: { github: { type: "http", url: "http://github.invalid/mcp" } },
      permissions: ["read", "write", "go_repository", "safeoutputs", "github"],
    });
    await expect(restrictCopilotSDKRepositoryCatalog(session, options)).rejects.toThrow("no tools for a configured");
    expect(session.rpc.options.update).not.toHaveBeenCalled();
  });

  it.each([null, [], [...required, required[0]], [...required, { ...native("invalid"), mcpServerName: "unknown" }]])("rejects invalid metadata", async initial => {
    const { session, options } = fixture({ initial });
    await expect(restrictCopilotSDKRepositoryCatalog(session, options)).rejects.toThrow(/catalog/);
  });

  it("never falls back when filter updates are unsupported or rejected", async () => {
    const missing = fixture();
    missing.session.rpc.options.update = undefined;
    await expect(restrictCopilotSDKRepositoryCatalog(missing.session, missing.options)).rejects.toThrow("update APIs");
    const rejected = fixture();
    rejected.session.rpc.options.update.mockResolvedValue({ success: false });
    await expect(restrictCopilotSDKRepositoryCatalog(rejected.session, rejected.options)).rejects.toThrow("rejected");
  });

  it("does not leak transport error credentials", async () => {
    const { session, options } = fixture();
    session.rpc.tools.initializeAndValidate.mockRejectedValue(new Error("Authorization: fixture-private-header"));
    await expect(restrictCopilotSDKRepositoryCatalog(session, options)).rejects.toThrow("initialization failed before inference");
    const second = fixture();
    second.session.rpc.options.update.mockRejectedValue(new Error("https://server.invalid/?token=fixture-private-header"));
    await expect(restrictCopilotSDKRepositoryCatalog(second.session, second.options)).rejects.not.toThrow("fixture-private-header");
  });

  it("bounds initialization without sending a model request", async () => {
    const { session, options } = fixture();
    session.rpc.tools.initializeAndValidate.mockReturnValue(new Promise(() => {}));
    await expect(restrictCopilotSDKRepositoryCatalog(session, { ...options, timeoutMs: 5 })).rejects.toThrow("timed out before inference");
  });

  it("fences late RPC continuations after timeout", async () => {
    const { session, options } = fixture();
    let resolve;
    session.rpc.tools.initializeAndValidate.mockReturnValue(
      new Promise(done => {
        resolve = done;
      })
    );
    await expect(restrictCopilotSDKRepositoryCatalog(session, { ...options, timeoutMs: 5 })).rejects.toThrow("timed out");
    resolve({});
    await new Promise(done => setTimeout(done, 10));
    expect(session.rpc.tools.getCurrentMetadata).not.toHaveBeenCalled();
    expect(session.rpc.options.update).not.toHaveBeenCalled();
  });

  it("cancels initialization and cannot update filters after cancellation", async () => {
    const { session, options } = fixture();
    const controller = new AbortController();
    let resolve;
    session.rpc.tools.initializeAndValidate.mockReturnValue(
      new Promise(done => {
        resolve = done;
      })
    );
    const result = restrictCopilotSDKRepositoryCatalog(session, { ...options, signal: controller.signal });
    controller.abort();
    await expect(result).rejects.toThrow("cancelled before inference");
    resolve({});
    await new Promise(done => setTimeout(done, 10));
    expect(session.rpc.options.update).not.toHaveBeenCalled();
  });

  it("rejects deferred tools rather than relying on unexposed tool discovery", async () => {
    const { session, options } = fixture({ restricted: [...required.slice(0, -1), { ...required.at(-1), deferLoading: true }] });
    await expect(restrictCopilotSDKRepositoryCatalog(session, options)).rejects.toThrow("without deferred discovery");
  });

  it("bounds the final model-facing catalog", async () => {
    const extra = Array.from({ length: 129 }, (_, index) => mcp("safeoutputs", `extra_${index}`));
    const { session, options } = fixture({ initial: [...required, ...extra] });
    await expect(restrictCopilotSDKRepositoryCatalog(session, options)).rejects.toThrow("at most 128");
  });

  it("requires a native safeoutputs connection and reserves the custom-tool name", async () => {
    const { session, options } = fixture();
    await expect(restrictCopilotSDKRepositoryCatalog(session, { ...options, mcpServers: {} })).rejects.toThrow("compiler-owned safeoutputs");
    await expect(restrictCopilotSDKRepositoryCatalog(session, { ...options, mcpServers: { ...options.mcpServers, go_repository: { tools: ["*"] } } })).rejects.toThrow("reserves go_repository");
  });
});
