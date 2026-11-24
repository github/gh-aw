import { describe, expect, it, vi } from "vitest";
import { readFileSync } from "node:fs";
import { runDailyGitHubDocsSEOOptimizerDriver, REQUEST_COUNT, REQUEST_GENERATOR_PROMPT, buildEvaluatorPrompt } from "../../../.github/drivers/daily_github_docs_seo_optimizer_driver.ts";

const toolConfig = {
  version: 1,
  capabilities: { bash: false, edit: false, webFetch: false, webSearch: false, dynamicWorkflows: false, mcp: true, cliProxy: false },
  permissions: { allowedTools: ["read", "safeoutputs"] },
  explicitlyDisabledTools: ["bash", "cli-proxy", "edit", "github"],
};

function fixture() {
  const requests = Array.from({ length: REQUEST_COUNT }, (_, index) => `Automation request ${index + 1}`);
  const evaluations = requests.map(request => ({
    request,
    options: [1, 2, 3].map(rank => ({ rank, name: `Option ${rank}`, reason: "Fits the request" })),
    documentation_pages: [],
  }));
  const mcpServers = {
    safeoutputs: { type: "http", url: "http://gateway.invalid/mcp/safeoutputs", tools: ["create_issue"], headers: { Authorization: "fixture-session-token" } },
  };
  const finalResult = { exitCode: 0, output: "Issue reported", hasOutput: true, durationMs: 1 };
  let sessionIndex = 0;
  const runWithCopilotSDKImpl = vi.fn(async () => {
    const index = sessionIndex++;
    const payload = index === 0 ? { requests } : evaluations[index - 1];
    return index <= REQUEST_COUNT ? { exitCode: 0, output: JSON.stringify(payload), hasOutput: true, durationMs: 1 } : finalResult;
  });
  const loadMCPServersImpl = vi.fn(() => mcpServers);
  const options = {
    env: {
      GH_AW_PROMPT: "fixture-prompt.md",
      COPILOT_SDK_URI: "http://sidecar.invalid",
      COPILOT_CONNECTION_TOKEN: "fixture-connection-token",
      GH_AW_COPILOT_SDK_MULTI_PROVIDER_JSON: "fixture-provider-config",
      GH_AW_COPILOT_SDK_TOOL_CONFIG: JSON.stringify(toolConfig),
      GH_AW_COPILOT_SDK_SERVER_ARGS: '["--allow-all-tools"]',
    },
    fsModule: { readFileSync: vi.fn(() => "Analyze the dataset and create exactly one issue.") },
    runWithCopilotSDKImpl,
    loadMCPServersImpl,
    parseMultiProviderJsonImpl: () => ({ model: "fixture-model", providers: [], models: [] }),
    applyModelFallbackImpl: () => "fixture-model",
  };
  return { options, runWithCopilotSDKImpl, loadMCPServersImpl, mcpServers, finalResult, evaluations };
}

describe("daily GitHub Docs SEO reporting driver", () => {
  it("permits configured reporting tools without permitting discovery or baseline tools", () => {
    const workflowPrompt = readFileSync(new URL("../../../.github/workflows/daily-github-docs-seo-optimizer.md", import.meta.url), "utf8");
    expect(workflowPrompt).toContain("Do not inspect the workspace, discover tools, or add facts not supported by the provided evaluator outputs.");
    expect(workflowPrompt).toContain("Use only configured safe-output reporting tools in this final session to create exactly one issue");
    expect(workflowPrompt).toContain("  bash: false");
    expect(workflowPrompt).toContain("  cli-proxy: false");
    for (const prompt of [REQUEST_GENERATOR_PROMPT, buildEvaluatorPrompt("Fixture request")]) {
      expect(prompt).toContain("Do not use tools, read files, inspect the workspace");
    }
  });

  it("registers compiler-owned MCP tools only in the final reporting session", async () => {
    const { options, runWithCopilotSDKImpl, loadMCPServersImpl, mcpServers, finalResult, evaluations } = fixture();

    expect(await runDailyGitHubDocsSEOOptimizerDriver(options)).toBe(finalResult);
    expect(loadMCPServersImpl).toHaveBeenCalledWith(toolConfig);
    expect(runWithCopilotSDKImpl).toHaveBeenCalledTimes(REQUEST_COUNT + 2);
    for (const [session] of runWithCopilotSDKImpl.mock.calls.slice(0, REQUEST_COUNT + 1)) {
      expect(session.permissionConfig).toEqual({ allowAllTools: false, allowedTools: ["__daily_github_docs_seo_optimizer_no_tools__"] });
      expect(session.mcpServers).toEqual({});
      expect(session.toolConfig).toBeUndefined();
    }
    const [reporting] = runWithCopilotSDKImpl.mock.calls.at(-1);
    expect(reporting.permissionConfig).toEqual({ allowedTools: ["safeoutputs"] });
    expect(reporting.toolConfig).toEqual(toolConfig);
    expect(reporting.mcpServers).toEqual(mcpServers);
    expect(reporting.mcpOnly).toBe(true);
    expect(reporting.connectionToken).toBe(options.env.COPILOT_CONNECTION_TOKEN);
    const dataset = JSON.parse(reporting.prompt.match(/```json\n([\s\S]*?)\n```/)[1]);
    expect(dataset.evaluations).toEqual(evaluations);
    expect(reporting.prompt).not.toContain("fixture-session-token");
  });

  it("does not forward unrelated MCP servers or compiler permissions to reporting", async () => {
    const { options, runWithCopilotSDKImpl, mcpServers } = fixture();
    options.env.GH_AW_COPILOT_SDK_TOOL_CONFIG = JSON.stringify({ ...toolConfig, permissions: { allowedTools: ["read", "github", "safeoutputs(create_issue)"] } });
    options.loadMCPServersImpl.mockReturnValue({ ...mcpServers, github: { type: "http", url: "http://gateway.invalid/mcp/github" } });

    await runDailyGitHubDocsSEOOptimizerDriver(options);
    const [reporting] = runWithCopilotSDKImpl.mock.calls.at(-1);
    expect(reporting.permissionConfig).toEqual({ allowedTools: ["safeoutputs(create_issue)"] });
    expect(reporting.mcpServers).toEqual(mcpServers);
  });

  it("fails before opening sessions when reporting tools are not registered", async () => {
    const { options, runWithCopilotSDKImpl } = fixture();
    options.loadMCPServersImpl.mockReturnValue({});

    await expect(runDailyGitHubDocsSEOOptimizerDriver(options)).rejects.toThrow("Configured safeoutputs tools are required");
    expect(runWithCopilotSDKImpl).not.toHaveBeenCalled();
  });

  it.each([undefined, "{", JSON.stringify({ ...toolConfig, version: 2 })])("rejects missing or invalid compiler tool contracts before opening sessions: %s", async value => {
    const { options, runWithCopilotSDKImpl, loadMCPServersImpl } = fixture();
    options.env.GH_AW_COPILOT_SDK_TOOL_CONFIG = value;

    await expect(runDailyGitHubDocsSEOOptimizerDriver(options)).rejects.toThrow(/GH_AW_COPILOT_SDK_TOOL_CONFIG/);
    expect(loadMCPServersImpl).not.toHaveBeenCalled();
    expect(runWithCopilotSDKImpl).not.toHaveBeenCalled();
  });

  it("does not open sessions when the generated MCP catalog fails integrity validation", async () => {
    const { options, runWithCopilotSDKImpl, loadMCPServersImpl } = fixture();
    loadMCPServersImpl.mockImplementation(() => {
      throw new Error("Copilot SDK MCP configuration integrity check failed");
    });

    await expect(runDailyGitHubDocsSEOOptimizerDriver(options)).rejects.toThrow("integrity check failed");
    expect(runWithCopilotSDKImpl).not.toHaveBeenCalled();
  });
});
