import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { runInNewContext } from "node:vm";
import { describe, expect, it } from "vitest";
import { BuiltInTools, ToolSet } from "@github/copilot-sdk";
import { readDocumentMetadata } from "../../../scripts/markdown_metadata_test_adapter.cjs";
import { buildCopilotSDKPermissionHandler, parsePermissionConfigFromServerArgs } from "./copilot_sdk_permissions.cjs";
import { buildCopilotSDKSessionToolConfig } from "./copilot_sdk_tool_config.cjs";

const root = resolve(import.meta.dirname, "../../..");
const source = readFileSync(resolve(root, ".github/workflows/breaking-change-checker.md"), "utf8");
const [, frontmatter, prompt] = source.match(/^---\r?\n([\s\S]*?)\r?\n---\r?\n([\s\S]*)$/);
const workflow = readDocumentMetadata(frontmatter, { version: "1.2", uniqueKeys: true }).value;
const lock = readDocumentMetadata(readFileSync(resolve(root, ".github/workflows/breaking-change-checker.lock.yml"), "utf8"), {
  version: "1.2",
  uniqueKeys: true,
}).value;
const preparation = workflow.steps.find(step => step.name === "Prepare breaking review context");
const sha = "4d73748ff2d044cc0fd9cb05ed13b51bb475cc72";
const githubTools = ["get_commit", "get_file_contents", "list_commits", "pull_request_read", "search_pull_requests"];
const toolsets = JSON.parse(readFileSync(resolve(root, "pkg/workflow/data/github_toolsets_permissions.json"), "utf8")).toolsets;
const githubCatalog = [...new Set([...toolsets.repos.tools, ...toolsets.pull_requests.tools])].sort();

function expectReadOnlyAgent(steps) {
  const gateway = steps.find(step => step.id === "start-mcp-gateway");
  const heredoc = gateway.run.match(/cat << (GH_AW_MCP_CONFIG_\w+_EOF)[^\n]*\n([\s\S]*?)^\1$/m);
  expect(heredoc).not.toBeNull();
  // Decode shell-escaped dollars and substitute only unquoted shell JSON values.
  const mcpConfig = JSON.parse(heredoc[2].replace(/\\\$/g, "$").replace(/:\s*\$(?:\{[^}]+\}|[A-Z_][A-Z_0-9]*)/g, ": null"));
  expect(mcpConfig.mcpServers.github.env.GITHUB_READ_ONLY).toBe("1");
  expect(mcpConfig.mcpServers.github.env.GITHUB_TOOLSETS.split(",").sort()).toEqual(["pull_requests", "repos"]);
  expect(mcpConfig.mcpServers.github["guard-policies"]["allow-only"]).toMatchObject({
    "min-integrity": "approved",
    repos: ["github/gh-aw"],
  });
  expect(mcpConfig.mcpServers.safeoutputs).toBeDefined();
  expect(gateway.run).not.toContain("export GH_AW_MCP_CLI_SERVERS=");
  const execution = steps.find(step => step.name === "Execute GitHub Copilot CLI");
  const config = JSON.parse(execution.env.GH_AW_COPILOT_SDK_TOOL_CONFIG);
  // Infrastructure mounting can still create an unrestricted GitHub wrapper.
  // The actual SDK surface and both permission consumers must make it unreachable.
  expect(config.capabilities.bash).toBe(false);
  expect(config.capabilities.cliProxy).toBe(false);
  expect(config.capabilities.mcp).toBe(true);
  expect(config.capabilities.edit).toBe(false);
  expect(config.explicitlyDisabledTools).toContain("bash");
  expect(config.permissions.allowedTools).not.toContain("write");
  const nativeTools = buildCopilotSDKSessionToolConfig(config, { BuiltInTools, ToolSet }).availableTools.toArray();
  expect(nativeTools).toEqual(expect.arrayContaining(["builtin:view", "builtin:sql", "mcp:*"]));
  for (const tool of ["bash", "read_bash", "stop_bash", "list_bash", "apply_patch", "web_fetch", "web_search", "run_dynamic_workflow"]) {
    expect(nativeTools).not.toContain(`builtin:${tool}`);
  }
  const serverArgs = JSON.parse(execution.env.GH_AW_COPILOT_SDK_SERVER_ARGS);
  expect(serverArgs).toContain("--disable-builtin-mcps");
  expect(serverArgs).not.toContain("--allow-all-tools");
  // Exercise both permission consumers against the backend catalog, not just
  // the manifest's advertised labels; a shell bridge bypasses scoped MCP grants.
  for (const permissions of [config.permissions, parsePermissionConfigFromServerArgs(execution.env.GH_AW_COPILOT_SDK_SERVER_ARGS)]) {
    expect(permissions).toBeDefined();
    expect(permissions.allowedTools.some(tool => tool === "shell" || tool.startsWith("shell("))).toBe(false);
    expect(permissions.allowedTools.filter(tool => tool === "github" || tool.startsWith("github(")).sort()).toEqual(githubTools.map(tool => `github(${tool})`));
    const handler = buildCopilotSDKPermissionHandler(permissions, () => ({ kind: "approve-once" }));
    const callable = githubCatalog.filter(toolName => handler({ kind: "mcp", serverName: "github", toolName }).kind === "approve-once");
    expect(callable).toEqual(githubTools);
    for (const toolName of ["create_issue", "noop", "report_incomplete"]) {
      expect(handler({ kind: "mcp", serverName: "safeoutputs", toolName }).kind).toBe("approve-once");
    }
    for (const fullCommandText of [
      "github list_branches --owner github --repo gh-aw",
      "github get_commit --sha main",
      "gh api repos/github/gh-aw/commits",
      "git log",
      "curl https://api.github.com/repos/github/gh-aw/commits",
      "date +%s",
      "cat /tmp/gh-aw/agent/breaking-review-context.json",
      "jq '.files[0].patch[0:4096]' /tmp/gh-aw/mcp-payloads/response.json",
    ]) {
      expect(handler({ kind: "shell", commands: [], fullCommandText }).kind).toBe("reject");
    }
    expect(handler({ kind: "write", path: "review.json" }).kind).toBe("reject");
  }
}

async function prepare({ createdAt = "2026-11-10T14:53:28Z", headSha = sha, contextSha = sha, branch = "main", repository = { default_branch: branch }, writes = [], directories = [], failure } = {}) {
  const calls = [];
  await runInNewContext(
    `(async () => { ${preparation.with.script}\n})()`,
    {
      require: name => {
        expect(name).toBe("node:fs");
        return {
          mkdirSync: (path, options) => {
            expect(path).toBe("/tmp/gh-aw/agent");
            expect(options).toEqual({ recursive: true });
            directories.push(path);
          },
          writeFileSync: (path, content) => writes.push({ path, value: JSON.parse(content) }),
        };
      },
      context: { repo: { owner: "github", repo: "gh-aw" }, runId: 42838691761, sha: contextSha, payload: { repository } },
      github: {
        rest: {
          actions: {
            getWorkflowRun: async args => {
              calls.push(args);
              if (failure) throw failure;
              return { data: { created_at: createdAt, head_sha: headSha } };
            },
          },
        },
      },
    },
    { timeout: 1000 }
  );
  return { writes, calls };
}

describe("Breaking Change Checker coverage contract", () => {
  it("pins the interval to run creation and the run SHA, not invocation time or a moving branch", async () => {
    const { writes, calls } = await prepare();
    expect(calls).toEqual([{ owner: "github", repo: "gh-aw", run_id: 42838691761 }]);
    expect(writes).toEqual([
      {
        path: "/tmp/gh-aw/agent/breaking-review-context.json",
        value: {
          repository: "github/gh-aw",
          sha,
          base_branch: "main",
          start: "2026-11-09T14:53:28.000Z",
          end: "2026-11-10T14:53:28.000Z",
          interval: "start-inclusive,end-exclusive",
        },
      },
    ]);
  });

  it.each([
    [{ createdAt: "not-a-date" }, 'run.created_at must parse as a timestamp (observed "not-a-date")'],
    [{ headSha: "0".repeat(40) }, `run.head_sha must match context.sha (observed head_sha="${"0".repeat(40)}", context.sha="${sha}")`],
    [{ contextSha: "main" }, 'context.sha must be a 40-character lowercase hexadecimal SHA (observed "main")'],
    [{ branch: "" }, 'repository.default_branch must be a non-empty string (observed "")'],
    [{ branch: null }, "repository.default_branch must be a non-empty string (observed null)"],
    [{ repository: {} }, "repository.default_branch must be a non-empty string (observed undefined)"],
    [{ branch: { token: "secret-must-not-be-logged" } }, "repository.default_branch must be a non-empty string (observed <object>)"],
  ])("rejects invalid review metadata with a specific diagnostic: %j", async (options, diagnostic) => {
    const writes = [];
    const directories = [];
    await expect(prepare({ ...options, writes, directories })).rejects.toThrow(`Cannot establish a pinned breaking-change review context: ${diagnostic}`);
    expect(writes).toEqual([]);
    expect(directories).toEqual([]);
  });

  it("reports all failed metadata checks without serializing unexpected structured values", async () => {
    const writes = [];
    const directories = [];
    const result = prepare({
      createdAt: "not-a-date",
      contextSha: "main",
      headSha: { token: "secret-must-not-be-logged" },
      branch: "",
      writes,
      directories,
    });
    await expect(result).rejects.toThrow(
      'Cannot establish a pinned breaking-change review context: run.created_at must parse as a timestamp (observed "not-a-date"); context.sha must be a 40-character lowercase hexadecimal SHA (observed "main"); run.head_sha must match context.sha (observed head_sha=<object>, context.sha="main"); repository.default_branch must be a non-empty string (observed "")'
    );
    expect(writes).toEqual([]);
    expect(directories).toEqual([]);
  });

  it("does not turn metadata API failures into a success-shaped context", async () => {
    const writes = [];
    const directories = [];
    await expect(prepare({ failure: new Error("API unavailable"), writes, directories })).rejects.toThrow("API unavailable");
    expect(writes).toEqual([]);
    expect(directories).toEqual([]);
  });

  it("keeps repository reads narrow and does not depend on Git history or agent writes", () => {
    expect(workflow.permissions["pull-requests"]).toBe("read");
    expect(workflow.tools["cli-proxy"]).toBe(false);
    expect(workflow.tools.github).toEqual({
      mode: "local",
      toolsets: ["repos", "pull_requests"],
      "allowed-repos": ["github/gh-aw"],
      "min-integrity": "approved",
      allowed: ["list_commits", "get_commit", "get_file_contents", "search_pull_requests", "pull_request_read"],
    });
    expect(workflow.tools.bash).toBe(false);
    expect(workflow.tools.edit).toBe(false);
    const steps = lock.jobs.agent.steps;
    expect(steps.findIndex(step => step.name === preparation.name)).toBeLessThan(steps.findIndex(step => step.name === "Execute GitHub Copilot CLI"));
    expect(steps.find(step => step.name === preparation.name).uses).toMatch(/^actions\/github-script@[0-9a-f]{40}$/);
    expect(steps.find(step => step.name === preparation.name).with.script.trim()).toBe(preparation.with.script.trim());
    expectReadOnlyAgent(steps);
  });

  it.each([
    ["disabled read-only mode", '"GITHUB_READ_ONLY": "0"'],
    ["missing read-only mode", '"UNRELATED_READ_ONLY": "1"'],
    ["read-only flag moved outside env", '"UNRELATED": { "GITHUB_READ_ONLY": "1" }'],
  ])("rejects %s even with an unrelated read-only flag", (_name, replacement) => {
    const steps = structuredClone(lock.jobs.agent.steps);
    const gateway = steps.find(step => step.id === "start-mcp-gateway");
    gateway.run = gateway.run.replace('"GITHUB_READ_ONLY": "1"', replacement);
    steps.push({ env: { GITHUB_READ_ONLY: "1" }, run: '"GITHUB_READ_ONLY": "1"' });
    expect(() => expectReadOnlyAgent(steps)).toThrow(expect.objectContaining({ name: "AssertionError" }));
  });

  it.each(["edit", "github", "github(list_branches)", "github(create_issue)", "shell(github:*)", "shell(gh:*)", "shell(git)", "shell", "write"])("rejects compiled tool grant regression: %s", grant => {
    const steps = structuredClone(lock.jobs.agent.steps);
    const execution = steps.find(step => step.name === "Execute GitHub Copilot CLI");
    const config = JSON.parse(execution.env.GH_AW_COPILOT_SDK_TOOL_CONFIG);
    if (grant === "edit") config.capabilities.edit = true;
    else config.permissions.allowedTools.push(grant);
    execution.env.GH_AW_COPILOT_SDK_TOOL_CONFIG = JSON.stringify(config);
    expect(() => expectReadOnlyAgent(steps)).toThrow(expect.objectContaining({ name: "AssertionError" }));
  });

  it("rejects a CLI-side bridge grant even when the SDK MCP grant list remains narrow", () => {
    const steps = structuredClone(lock.jobs.agent.steps);
    const execution = steps.find(step => step.name === "Execute GitHub Copilot CLI");
    const args = JSON.parse(execution.env.GH_AW_COPILOT_SDK_SERVER_ARGS);
    args.push("--allow-tool", "shell(github:*)");
    execution.env.GH_AW_COPILOT_SDK_SERVER_ARGS = JSON.stringify(args);
    expect(() => expectReadOnlyAgent(steps)).toThrow(expect.objectContaining({ name: "AssertionError" }));
  });

  it("rejects filtering native MCP servers into a CLI-only config", () => {
    const steps = structuredClone(lock.jobs.agent.steps);
    steps.find(step => step.id === "start-mcp-gateway").run += '\nexport GH_AW_MCP_CLI_SERVERS=\'["github","safeoutputs"]\'';
    expect(() => expectReadOnlyAgent(steps)).toThrow(expect.objectContaining({ name: "AssertionError" }));
  });

  it("allows infrastructure mounting only with an unreachable shell surface", () => {
    const steps = structuredClone(lock.jobs.agent.steps);
    const execution = steps.find(step => step.name === "Execute GitHub Copilot CLI");
    const config = JSON.parse(execution.env.GH_AW_COPILOT_SDK_TOOL_CONFIG);
    config.capabilities.bash = false;
    config.permissions.allowedTools = config.permissions.allowedTools.filter(tool => tool !== "shell" && !tool.startsWith("shell("));
    config.explicitlyDisabledTools = [...new Set([...config.explicitlyDisabledTools, "bash"])];
    execution.env.GH_AW_COPILOT_SDK_TOOL_CONFIG = JSON.stringify(config);
    const args = JSON.parse(execution.env.GH_AW_COPILOT_SDK_SERVER_ARGS);
    execution.env.GH_AW_COPILOT_SDK_SERVER_ARGS = JSON.stringify(args.filter((arg, index) => !(arg === "--allow-tool" && args[index + 1]?.startsWith("shell")) && !(args[index - 1] === "--allow-tool" && arg.startsWith("shell"))));
    if (!steps.some(step => step.id === "mount-mcp-clis")) steps.push({ id: "mount-mcp-clis" });
    expectReadOnlyAgent(steps);
  });

  it("rejects reachable infrastructure CLI wrappers even with cli-proxy disabled", () => {
    const steps = structuredClone(lock.jobs.agent.steps);
    const execution = steps.find(step => step.name === "Execute GitHub Copilot CLI");
    const config = JSON.parse(execution.env.GH_AW_COPILOT_SDK_TOOL_CONFIG);
    config.capabilities.bash = true;
    config.permissions.allowedTools.push("shell(github:*)");
    execution.env.GH_AW_COPILOT_SDK_TOOL_CONFIG = JSON.stringify(config);
    expect(() => expectReadOnlyAgent(steps)).toThrow(expect.objectContaining({ name: "AssertionError" }));
  });

  it("uses the native transport and realistic bounded recovery without alternate GitHub clients", () => {
    expect(workflow.tools["cli-proxy"]).toBe(false);
    expect(workflow.tools.bash).toBe(false);
    expect(prompt).toContain("five advertised native GitHub MCP tools");
    expect(prompt).toContain("through native safe-output tools, not shell commands");
    expect(prompt).not.toMatch(/github <tool> --help|```(?:bash|sh)\b|allowed `date \+%s`/);
    expect(prompt).toContain("SELECT CAST(strftime('%s', 'now') AS INTEGER) AS epoch_seconds");
    expect(prompt).toContain("even infrastructure-mounted");
    expect(prompt).toContain("does not provide local-reader line ranges");
    expect(prompt).toContain("native `view` cannot decode base64");
    expect(prompt).toContain("single line still exceeds the reader's limit");
    expect(prompt).toMatch(/without a\s+complete saved payload/);
  });

  it("retains documentation guards for evidence and partial outcomes, not proof of agent compliance", () => {
    for (const term of ["list_commits", "search_pull_requests", "get_file_contents", "report_incomplete", "noop"]) expect(prompt).toContain(term);
    expect(prompt).toMatch(/coverage ledger/i);
    expect(workflow["safe-outputs"].messages["run-success"]).not.toContain("has reviewed all changes");
    expect(workflow.evals.find(item => item.id === "analysis-completed").question).toMatch(/coverage.*partial|partial.*coverage/i);
  });

  it("conditions both the mandatory noop instruction and its failure warning on complete clean coverage", () => {
    const noFindings = prompt.split("### If NO Breaking Changes Found")[1].split("### If Breaking Changes Found")[0];
    expect(noFindings).toMatch(/\*\*Only when coverage is complete and no breaking changes are confirmed, YOU MUST\s+CALL\*\* the `noop` tool/);
    expect(noFindings).toMatch(/\*\*When coverage is complete and no breaking changes are confirmed, DO NOT just\s+write this message in your output text\*\*/);
    expect(noFindings).toMatch(/In that case, the workflow will fail if you don't call it\. If coverage is\s+incomplete, call `report_incomplete` instead/);
  });
});
