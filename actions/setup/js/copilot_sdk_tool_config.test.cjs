import { describe, expect, it, vi } from "vitest";
import { createRequire } from "module";

const require = createRequire(import.meta.url);
const { parseCopilotSDKToolConfig, buildCopilotSDKSessionToolConfig, isReservedSDKPermission } = require("./copilot_sdk_tool_config.cjs");
const { runWithCopilotSDK } = require("./copilot_sdk_session.cjs");
const { buildCopilotSDKPermissionHandler } = require("./copilot_sdk_permissions.cjs");

function validToolConfig(overrides = {}) {
  return {
    version: overrides.version ?? 1,
    capabilities: {
      bash: false,
      edit: false,
      webFetch: true,
      webSearch: false,
      dynamicWorkflows: false,
      mcp: true,
      cliProxy: false,
      ...(overrides.capabilities ?? {}),
    },
    permissions: {
      allowedTools: ["read", "safeoutputs", "web_fetch"],
      ...(overrides.permissions ?? {}),
    },
    explicitlyDisabledTools: overrides.explicitlyDisabledTools ?? ["bash", "cli-proxy", "edit"],
  };
}

class FakeToolSet {
  items = [];

  addBuiltIn(names) {
    for (const name of Array.isArray(names) ? names : [names]) this.items.push(`builtin:${name}`);
    return this;
  }

  addCustom(name) {
    this.items.push(`custom:${name}`);
    return this;
  }

  addMcp(name) {
    this.items.push(`mcp:${name}`);
    return this;
  }

  toArray() {
    return [...this.items];
  }
}

const fakeSDKTools = {
  ToolSet: FakeToolSet,
  BuiltInTools: {
    Isolated: ["ask_user", "task_complete", "exit_plan_mode", "task", "read_agent", "write_agent", "list_agents", "skill"],
  },
  defineTool: (name, config) => ({ name, ...config }),
};

describe("parseCopilotSDKToolConfig", () => {
  it("fails closed when the compiler contract is absent", () => {
    expect(() => parseCopilotSDKToolConfig(undefined)).toThrow("is required");
    expect(() => parseCopilotSDKToolConfig("")).toThrow("is required");
  });

  it("normalizes a valid compiler contract", () => {
    expect(parseCopilotSDKToolConfig(JSON.stringify(validToolConfig()))).toEqual(validToolConfig());
  });

  it("treats null explicitlyDisabledTools as absent", () => {
    expect(parseCopilotSDKToolConfig(JSON.stringify({ ...validToolConfig(), explicitlyDisabledTools: null })).explicitlyDisabledTools).toEqual([]);
  });

  it("keeps workflows disabled in older version-1 contracts", () => {
    const config = validToolConfig();
    delete config.capabilities.dynamicWorkflows;
    expect(parseCopilotSDKToolConfig(JSON.stringify(config)).capabilities.dynamicWorkflows).toBe(false);
  });

  it("parses a configured aggregate tool-call limit", () => {
    expect(parseCopilotSDKToolConfig(JSON.stringify({ ...validToolConfig(), maxToolCalls: "12" })).maxToolCalls).toBe(12);
  });

  it.each([
    ["invalid JSON", "{", "must be valid JSON"],
    ["unsupported version", JSON.stringify({ ...validToolConfig(), version: 2 }), "unsupported"],
    ["missing capability", JSON.stringify({ ...validToolConfig(), capabilities: { bash: false } }), "capabilities.edit"],
    ["malformed workflow capability", JSON.stringify(validToolConfig({ capabilities: { dynamicWorkflows: "true" } })), "capabilities.dynamicWorkflows"],
    ["duplicate permission", JSON.stringify(validToolConfig({ permissions: { allowedTools: ["read", "read"] } })), "duplicate"],
    ["empty permissions", JSON.stringify(validToolConfig({ permissions: { allowedTools: [] } })), "must not be empty"],
    ["zero tool-call limit", JSON.stringify({ ...validToolConfig(), maxToolCalls: 0 }), "positive safe integer"],
    ["malformed tool-call limit", JSON.stringify({ ...validToolConfig(), maxToolCalls: "1.5" }), "positive safe integer"],
  ])("fails closed for %s", (_name, value, message) => {
    expect(() => parseCopilotSDKToolConfig(value)).toThrow(message);
  });

  it.each([
    ["bash", { capabilities: { bash: true }, permissions: { allowedTools: ["read", "safeoutputs", "web_fetch"] } }, "bash visibility"],
    ["edit", { capabilities: { edit: true }, permissions: { allowedTools: ["read", "safeoutputs", "web_fetch"] } }, "edit visibility"],
    ["web_fetch", { capabilities: { webFetch: false }, permissions: { allowedTools: ["read", "safeoutputs", "web_fetch"] } }, "web_fetch visibility"],
    ["web_search", { capabilities: { webSearch: true }, permissions: { allowedTools: ["read", "safeoutputs", "web_fetch"] } }, "web_search visibility"],
    ["workflow", { capabilities: { dynamicWorkflows: true }, permissions: { allowedTools: ["read", "safeoutputs", "web_fetch"] } }, "workflow visibility"],
    ["disabled workflow permission", { capabilities: { dynamicWorkflows: false }, permissions: { allowedTools: ["read", "safeoutputs", "web_fetch", "workflow"] } }, "workflow visibility"],
    ["MCP", { capabilities: { mcp: false }, permissions: { allowedTools: ["read", "safeoutputs", "web_fetch"] } }, "MCP permissions"],
  ])("rejects %s catalog/permission drift", (_name, partial, message) => {
    const value = validToolConfig({
      capabilities: { ...validToolConfig().capabilities, ...partial.capabilities },
      permissions: partial.permissions,
    });
    expect(() => parseCopilotSDKToolConfig(JSON.stringify(value))).toThrow(message);
  });

  it("rejects an explicitly disabled tool that resolves visible", () => {
    const value = validToolConfig({
      capabilities: { ...validToolConfig().capabilities, bash: true },
      permissions: { allowedTools: ["read", "safeoutputs", "shell", "web_fetch"] },
    });
    expect(() => parseCopilotSDKToolConfig(JSON.stringify(value))).toThrow("explicitly disabled bash");
  });

  it("rejects cliProxy visibility without a matching bash capability", () => {
    const value = validToolConfig({
      capabilities: { ...validToolConfig().capabilities, cliProxy: true },
    });
    expect(() => parseCopilotSDKToolConfig(JSON.stringify(value))).toThrow("cliProxy capability requires bash capability");
  });

  it("accepts cliProxy visibility when bash capability is also present", () => {
    const value = validToolConfig({
      capabilities: { ...validToolConfig().capabilities, bash: true, cliProxy: true },
      permissions: { allowedTools: ["read", "safeoutputs", "shell", "web_fetch"] },
      explicitlyDisabledTools: ["edit"],
    });
    expect(() => parseCopilotSDKToolConfig(JSON.stringify(value))).not.toThrow();
  });
});

describe("isReservedSDKPermission", () => {
  it("distinguishes built-in SDK permissions from MCP server grants", () => {
    expect(isReservedSDKPermission("read")).toBe(true);
    expect(isReservedSDKPermission("read(pkg/**)")).toBe(true);
    expect(isReservedSDKPermission("shell(git:*)")).toBe(true);
    expect(isReservedSDKPermission("web_fetch")).toBe(true);
    expect(isReservedSDKPermission("web_fetch(get)")).toBe(false);
    expect(isReservedSDKPermission("web_search")).toBe(true);
    expect(isReservedSDKPermission("workflow")).toBe(true);
    expect(isReservedSDKPermission("github")).toBe(false);
  });
});

describe("buildCopilotSDKSessionToolConfig", () => {
  it("keeps neutral and isolated controls while excluding ask_user and the bash family", () => {
    const config = buildCopilotSDKSessionToolConfig(validToolConfig(), fakeSDKTools);
    expect(config.availableTools.toArray()).toEqual([
      "builtin:task_complete",
      "builtin:exit_plan_mode",
      "builtin:task",
      "builtin:read_agent",
      "builtin:write_agent",
      "builtin:list_agents",
      "builtin:skill",
      "builtin:view",
      "builtin:rg",
      "builtin:glob",
      "builtin:sql",
      "mcp:*",
      "builtin:web_fetch",
    ]);
    for (const forbiddenTool of ["builtin:bash", "builtin:read_bash", "builtin:stop_bash", "builtin:list_bash", "builtin:apply_patch"]) {
      expect(config.availableTools.toArray()).not.toContain(forbiddenTool);
    }
    expect(config.tools).toHaveLength(1);
    expect(config.tools[0]).toMatchObject({
      name: "web_fetch",
      overridesBuiltInTool: true,
      defer: "never",
    });
  });

  it("admits all bash lifecycle tools only when shell permission is enabled", () => {
    const toolConfig = validToolConfig({
      capabilities: { ...validToolConfig().capabilities, bash: true },
      permissions: { allowedTools: ["read", "safeoutputs", "shell(git:*)", "web_fetch"] },
      explicitlyDisabledTools: ["cli-proxy", "edit"],
    });
    const config = buildCopilotSDKSessionToolConfig(toolConfig, fakeSDKTools);
    expect(config.availableTools.toArray()).toEqual(expect.arrayContaining(["builtin:bash", "builtin:read_bash", "builtin:stop_bash", "builtin:list_bash"]));
  });

  it("admits web_search only when the webSearch capability is enabled", () => {
    const toolConfig = validToolConfig({
      capabilities: { ...validToolConfig().capabilities, webSearch: true },
      permissions: { allowedTools: ["read", "safeoutputs", "web_fetch", "web_search"] },
    });
    const config = buildCopilotSDKSessionToolConfig(toolConfig, fakeSDKTools);
    expect(config.availableTools.toArray()).toContain("builtin:web_search");
  });

  it("omits web_search when the webSearch capability is disabled", () => {
    const config = buildCopilotSDKSessionToolConfig(validToolConfig(), fakeSDKTools);
    expect(config.availableTools.toArray()).not.toContain("builtin:web_search");
  });

  it("admits workflow built-ins only with the enabled capability", () => {
    const config = buildCopilotSDKSessionToolConfig(
      validToolConfig({
        capabilities: { dynamicWorkflows: true, mcp: false },
        permissions: { allowedTools: ["read", "web_fetch", "workflow"] },
      }),
      fakeSDKTools
    );
    expect(config.availableTools.toArray()).toEqual(expect.arrayContaining(["builtin:run_dynamic_workflow", "builtin:dynamic_workflows_manage"]));
    expect(config.availableTools.toArray()).not.toContain("mcp:*");
  });

  it("removes workflow built-ins from isolated tools when disabled", () => {
    const config = buildCopilotSDKSessionToolConfig(validToolConfig(), {
      ...fakeSDKTools,
      BuiltInTools: { Isolated: [...fakeSDKTools.BuiltInTools.Isolated, "run_dynamic_workflow", "dynamic_workflows_manage"] },
    });
    expect(config.availableTools.toArray()).not.toContain("builtin:run_dynamic_workflow");
    expect(config.availableTools.toArray()).not.toContain("builtin:dynamic_workflows_manage");
  });

  it("preserves legacy SDK behavior only when the compiler contract is absent", () => {
    expect(buildCopilotSDKSessionToolConfig(null, {})).toEqual({});
  });

  it("fails closed when required SDK filtering APIs are missing", () => {
    expect(() => buildCopilotSDKSessionToolConfig(validToolConfig(), {})).toThrow("ToolSet and BuiltInTools.Isolated");
  });

  it("fails closed when the SDK does not preserve the web_fetch override contract", () => {
    expect(() =>
      buildCopilotSDKSessionToolConfig(validToolConfig(), {
        ...fakeSDKTools,
        defineTool: (name, config) => ({ name, ...config, overridesBuiltInTool: false }),
      })
    ).toThrow("web_fetch override contract");
  });
});

describe("workflow permission scope", () => {
  it("keeps managed workflow approvals and unrelated MCP/custom tools denied", () => {
    const handler = buildCopilotSDKPermissionHandler({ allowedTools: ["read", "workflow"] }, () => ({ kind: "approve-once" }));
    expect(handler({ kind: "workflow", name: "review", operation: "run" })).toEqual({ kind: "approve-once" });
    expect(handler({ kind: "workflow", name: "review", operation: "run", managedApprovalRequired: true }).kind).toBe("reject");
    expect(handler({ kind: "mcp", serverName: "workflow", toolName: "other" }).kind).toBe("reject");
    expect(handler({ kind: "custom-tool", toolName: "workflow" }).kind).toBe("reject");
  });
});

describe("runWithCopilotSDK compiler-owned catalog", () => {
  it.each([true, false])("wires workflow visibility and approvals into the session when enabled=%s", async enabled => {
    const createSession = vi.fn().mockResolvedValue({
      sessionId: `session-workflow-contract-${enabled}`,
      on: () => {},
      sendAndWait: vi.fn().mockResolvedValue({ data: { content: "ok" } }),
      disconnect: vi.fn().mockResolvedValue(undefined),
    });
    class FakeCopilotClient {
      start = vi.fn().mockResolvedValue(undefined);
      createSession = createSession;
      stop = vi.fn().mockResolvedValue(undefined);
    }
    const toolConfig = validToolConfig({
      capabilities: { dynamicWorkflows: enabled },
      permissions: { allowedTools: ["read", "safeoutputs", "web_fetch", ...(enabled ? ["workflow"] : [])] },
    });
    const result = await runWithCopilotSDK({
      sdkUri: "http://127.0.0.1:3002",
      prompt: "test prompt",
      logger: () => {},
      permissionConfig: toolConfig.permissions,
      toolConfig,
      sdkModule: {
        ...fakeSDKTools,
        CopilotClient: FakeCopilotClient,
        RuntimeConnection: { forUri: vi.fn(() => ({})) },
        approveAll: () => ({ kind: "approve-once" }),
      },
    });
    expect(result.exitCode).toBe(0);
    const sessionConfig = createSession.mock.calls[0][0];
    expect(sessionConfig.availableTools.toArray().includes("builtin:run_dynamic_workflow")).toBe(enabled);
    expect(sessionConfig.availableTools.toArray().includes("builtin:dynamic_workflows_manage")).toBe(enabled);
    expect(sessionConfig.onPermissionRequest({ kind: "workflow", name: "review", operation: "run" }).kind).toBe(enabled ? "approve-once" : "reject");
    expect(sessionConfig.onPermissionRequest({ kind: "shell", fullCommandText: "git status" }).kind).toBe("reject");
  });

  it("passes one filtered catalog to the parent session for inherited subagent enforcement", async () => {
    const createSession = vi.fn().mockResolvedValue({
      sessionId: "session-tool-contract",
      on: () => {},
      sendAndWait: vi.fn().mockResolvedValue({ data: { content: "ok" } }),
      disconnect: vi.fn().mockResolvedValue(undefined),
    });

    class FakeCopilotClient {
      start = vi.fn().mockResolvedValue(undefined);
      createSession = createSession;
      stop = vi.fn().mockResolvedValue(undefined);
    }
    const sdkModule = {
      ...fakeSDKTools,
      CopilotClient: FakeCopilotClient,
      RuntimeConnection: { forUri: vi.fn(() => ({})) },
      approveAll: () => ({ kind: "approve-once" }),
    };

    const result = await runWithCopilotSDK({
      sdkUri: "http://127.0.0.1:3002",
      prompt: "test prompt",
      logger: () => {},
      permissionConfig: validToolConfig().permissions,
      toolConfig: validToolConfig(),
      sdkModule,
    });

    expect(result.exitCode).toBe(0);
    const sessionConfig = createSession.mock.calls[0][0];
    for (const forbiddenTool of ["builtin:bash", "builtin:read_bash", "builtin:stop_bash", "builtin:list_bash", "builtin:apply_patch"]) {
      expect(sessionConfig.availableTools.toArray()).not.toContain(forbiddenTool);
    }
    expect(sessionConfig.tools.map(tool => tool.name)).toEqual(["web_fetch"]);
  });

  it("enforces one shared pre-tool budget across the parent and subagent hooks", async () => {
    let onEvent = () => {};
    let sessionConfig;
    const stderrWriteSpy = vi.spyOn(process.stderr, "write").mockImplementation(() => true);
    const session = {
      sessionId: "session-tool-budget",
      on: handler => {
        onEvent = handler;
      },
      sendAndWait: vi.fn().mockImplementation(async () => {
        const hook = sessionConfig.hooks.onPreToolUse;
        expect(hook({ toolName: "bash", sessionId: "session-tool-budget", toolArgs: {} }, { sessionId: "session-tool-budget" })).toBeUndefined();
        const denied = hook({ toolName: "mcp", sessionId: "subagent-session", toolArgs: {} }, { sessionId: "session-tool-budget" });
        expect(denied.permissionDecision).toBe("deny");
        expect(denied.permissionDecisionReason).toContain("budget is exhausted (1 calls)");
        onEvent({
          type: "assistant.message",
          ephemeral: false,
          timestamp: new Date().toISOString(),
          data: { content: "done" },
        });
        return { data: { content: "done" } };
      }),
      disconnect: vi.fn().mockResolvedValue(undefined),
    };
    class FakeCopilotClient {
      start = vi.fn().mockResolvedValue(undefined);
      createSession = vi.fn().mockImplementation(config => {
        sessionConfig = config;
        return session;
      });
      stop = vi.fn().mockResolvedValue(undefined);
    }
    const sdkModule = {
      ...fakeSDKTools,
      CopilotClient: FakeCopilotClient,
      RuntimeConnection: { forUri: vi.fn(() => ({})) },
      approveAll: () => ({ kind: "approve-once" }),
    };

    try {
      const result = await runWithCopilotSDK({
        sdkUri: "http://127.0.0.1:3002",
        prompt: "test prompt",
        logger: () => {},
        permissionConfig: validToolConfig().permissions,
        toolConfig: { ...validToolConfig(), maxToolCalls: 1 },
        sdkModule,
      });
      expect(result.exitCode).toBe(0);
      const events = stderrWriteSpy.mock.calls
        .map(([line]) => {
          try {
            return JSON.parse(line.trim());
          } catch {
            return null;
          }
        })
        .filter(Boolean);
      expect(events).toContainEqual(
        expect.objectContaining({
          type: "guard.tool_call_budget_exceeded",
          data: expect.objectContaining({ toolName: "mcp", sessionId: "subagent-session", callCount: 2, limit: 1 }),
        })
      );
    } finally {
      stderrWriteSpy.mockRestore();
    }
  });

  it("debits failed tool executions and permission denials through the SDK harness", async () => {
    let onEvent = () => {};
    let sessionConfig;
    const stderrWriteSpy = vi.spyOn(process.stderr, "write").mockImplementation(() => true);
    const session = {
      sessionId: "session-tool-budget-outcomes",
      on: handler => {
        onEvent = handler;
      },
      sendAndWait: vi.fn().mockImplementation(async () => {
        const onPreToolUse = sessionConfig.hooks.onPreToolUse;
        expect(onPreToolUse({ toolName: "web_fetch", sessionId: "root", toolArgs: {} }, { sessionId: "root" })).toBeUndefined();
        await expect(sessionConfig.tools[0].handler({ url: "https://example.com" })).rejects.toThrow("fetch failed");
        expect(onPreToolUse({ toolName: "bash", sessionId: "root", toolArgs: {} }, { sessionId: "root" })).toBeUndefined();
        expect(sessionConfig.onPermissionRequest({ kind: "shell", fullCommandText: "git status" })).toMatchObject({ kind: "reject" });
        onEvent({ type: "assistant.message", ephemeral: false, timestamp: new Date().toISOString(), data: { content: "done" } });
        return { data: { content: "done" } };
      }),
      disconnect: vi.fn().mockResolvedValue(undefined),
    };
    class FakeCopilotClient {
      start = vi.fn().mockResolvedValue(undefined);
      createSession = vi.fn().mockImplementation(config => {
        sessionConfig = config;
        return session;
      });
      stop = vi.fn().mockResolvedValue(undefined);
    }

    try {
      const result = await runWithCopilotSDK({
        sdkUri: "http://127.0.0.1:3002",
        prompt: "test prompt",
        logger: () => {},
        permissionConfig: validToolConfig().permissions,
        toolConfig: { ...validToolConfig(), maxToolCalls: 2 },
        webFetchOptions: { fetchImpl: vi.fn().mockRejectedValue(new Error("fetch failed")) },
        sdkModule: { ...fakeSDKTools, CopilotClient: FakeCopilotClient, RuntimeConnection: { forUri: vi.fn(() => ({})) }, approveAll: () => ({ kind: "approve-once" }) },
      });

      expect(result.exitCode).toBe(0);
      const events = stderrWriteSpy.mock.calls
        .map(([line]) => {
          try {
            return JSON.parse(line.trim());
          } catch {
            return null;
          }
        })
        .filter(Boolean);
      expect(events).toContainEqual(expect.objectContaining({ type: "guard.tool_call_budget_debit", data: expect.objectContaining({ toolName: "web_fetch", callCount: 1 }) }));
      expect(events).toContainEqual(expect.objectContaining({ type: "guard.tool_call_budget_debit", data: expect.objectContaining({ toolName: "bash", callCount: 2 }) }));
    } finally {
      stderrWriteSpy.mockRestore();
    }
  });
});
