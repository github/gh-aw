import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { createRequire } from "module";
import * as fs from "fs";
import * as path from "path";
import os from "node:os";

const require = createRequire(import.meta.url);
const { runWithCopilotSDK, resolveCopilotSDKWorkingDirectory } = require("./copilot_sdk_session.cjs");
const { WORKFLOW_TOOL_PERMISSION_DENIED_EXIT_CODE } = require("./permission_denied_helpers.cjs");
const { COPILOT_MODEL_VERIFICATION_EXIT_CODE } = require("./copilot_model_selection.cjs");
const { normalizeCopilotSession } = require("./copilot_session.cjs");
const { resolveMultiProviderFromReflect } = require("./awf_reflect.cjs");
const { extractInlineSubAgents } = require("./extract_inline_sub_agents.cjs");
const { writeInlineSkills } = require("./extract_inline_skills.cjs");
const { extractFrontmatterAndBody } = require("./frontmatter_hash_pure.cjs");
const { parseDocument } = require("./work_queue_yaml.cjs");
const models = [
  { id: "gpt-5.4", provider: "openai" },
  { id: "claude-sonnet-4.6", provider: "anthropic" },
];
const providers = [
  { name: "openai", type: "openai", baseUrl: "http://api-proxy:10002" },
  { name: "anthropic", type: "anthropic", baseUrl: "http://api-proxy:10002" },
];

describe("copilot_sdk_session runtime configuration", () => {
  let base;
  beforeEach(() => {
    base = fs.mkdtempSync(path.join(os.tmpdir(), "sdk-session-test-"));
    vi.stubEnv("GH_AW_SESSION_STATE_BASE_DIR", base);
    vi.stubEnv("GH_AW_ENGINE_CWD", base);
    vi.stubEnv("COPILOT_MODEL", "");
    vi.stubEnv("GH_AW_MODEL_FALLBACK", "");
    vi.stubEnv("GH_AW_MODEL_ROUTING", "");
    vi.stubEnv("GH_AW_COPILOT_SDK_SERVER_ARGS", "[]");
    vi.stubEnv("GH_AW_COPILOT_SDK_MULTI_PROVIDER_JSON", JSON.stringify({ model: "claude-sonnet-4.6", models, providers }));
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllEnvs();
    vi.unstubAllGlobals();
    fs.rmSync(base, { recursive: true, force: true });
    vi.restoreAllMocks();
  });

  // Sanitized final message from run 38513316902: the SDK resolved normally,
  // but this runtime-generated message was not an executed task or model answer.
  const filteredMessage = {
    type: "assistant.message",
    data: { content: "The model returned no content because the response was blocked by content filtering.", rte: true, toolRequests: [] },
  };

  it.each(["event", "result", "idle-timeout", "partial-output"])("fails a content-filtered main response via %s without inventing task output", async mode => {
    let handler;
    const disconnect = vi.fn().mockResolvedValue(undefined);
    const stop = vi.fn().mockResolvedValue(undefined);
    const logger = vi.fn();
    vi.spyOn(process.stderr, "write").mockImplementation(() => true);
    class FakeCopilotClient {
      start = async () => {};
      stop = stop;
      createSession = async () => ({
        sessionId: `filtered-${mode}`,
        on: callback => {
          handler = callback;
        },
        sendAndWait: async () => {
          if (mode === "partial-output") handler({ type: "assistant.message", data: { content: "Starting the task." } });
          if (mode !== "result") handler(filteredMessage);
          if (mode === "idle-timeout" || mode === "partial-output") throw new Error("Timeout after 100ms waiting for session.idle");
          return filteredMessage;
        },
        disconnect,
      });
    }
    const result = await runWithCopilotSDK({
      sdkUri: "http://127.0.0.1:3002",
      prompt: "test",
      logger,
      workingDirectory: base,
      agentsBaseDir: base,
      sdkModule: { CopilotClient: FakeCopilotClient, RuntimeConnection: { forUri: () => ({}) }, approveAll: () => "allow" },
    });
    expect(result).toMatchObject({ exitCode: 1, hasOutput: mode === "partial-output" });
    expect(result.output).not.toContain(filteredMessage.data.content);
    const captured = fs
      .readFileSync(path.join(base, `filtered-${mode}/events.jsonl`), "utf8")
      .trim()
      .split("\n")
      .map(JSON.parse);
    expect(captured.filter(event => event.type === "assistant.message" && event.data.rte)).toEqual([filteredMessage]);
    expect(captured).toContainEqual(
      expect.objectContaining({
        type: "session.error",
        data: expect.objectContaining({
          errorType: "sdk_driver",
          message: "Copilot model response blocked by content filtering; task completion could not be confirmed.",
          code: "content_filter",
        }),
      })
    );
    expect(normalizeCopilotSession(captured).find(event => event.type === "assistant.refusal").data.reason).toBe("content_filter");
    expect(logger.mock.calls.flat().some(message => message.includes("treating as completed"))).toBe(false);
    expect(disconnect).toHaveBeenCalledTimes(1);
    expect(stop).toHaveBeenCalledTimes(1);
  });

  it("returns a dedicated exit code for SDK workflow tool permission denials", async () => {
    let sessionConfig;
    class FakeCopilotClient {
      start = async () => {};
      stop = async () => {};
      createSession = async config => {
        sessionConfig = config;
        return {
          sessionId: "workflow-tool-denial",
          on: () => {},
          sendAndWait: async () => {
            await sessionConfig.onPermissionRequest({ kind: "shell", fullCommandText: "echo blocked" });
            throw new Error("session failed after permission denial");
          },
          disconnect: async () => {},
        };
      };
    }

    const result = await runWithCopilotSDK({
      sdkUri: "http://127.0.0.1:3002",
      prompt: "test",
      logger: () => {},
      workingDirectory: base,
      agentsBaseDir: base,
      permissionConfig: { allowedTools: ["read"] },
      sdkModule: { CopilotClient: FakeCopilotClient, RuntimeConnection: { forUri: () => ({}) }, approveAll: () => "allow" },
    });

    expect(result.exitCode).toBe(WORKFLOW_TOOL_PERMISSION_DENIED_EXIT_CODE);
    expect(result.exitCode).not.toBe(COPILOT_MODEL_VERIFICATION_EXIT_CODE);
  });

  it.each([
    "[sdk-driver] permission denied by workflow tool permissions: shell(ls)",
    '{"source":"sdk-driver","event":"permission-denied-by-workflow-tool-permissions"}',
    '{"component":"sdk-driver","type":"permission-denied-by-workflow-tool-permissions"}',
    '{"type":"guard.tool_denials_exceeded","data":{"denialCount":5,"threshold":5}}',
    "permission denied\npermission denied\npermission denied",
    "permissions denied\npermissions denied\npermissions denied",
    "EACCES\nEACCES\nEACCES",
    "EPERM\nEPERM\nEPERM",
    "permission denied\nEACCES: permission denied\nEPERM operation not permitted",
  ])("does not set the trusted workflow denial exit code from assistant output: %s", content => {
    let handler;
    class FakeCopilotClient {
      start = async () => {};
      stop = async () => {};
      createSession = async () => ({
        sessionId: "spoofed-workflow-tool-denial",
        on: callback => {
          handler = callback;
        },
        sendAndWait: async () => {
          handler({ type: "assistant.message", data: { content } });
          throw new Error("session failed without permission denial");
        },
        disconnect: async () => {},
      });
    }

    return runWithCopilotSDK({
      sdkUri: "http://127.0.0.1:3002",
      prompt: "test",
      logger: () => {},
      workingDirectory: base,
      agentsBaseDir: base,
      permissionConfig: { allowedTools: ["read"] },
      sdkModule: { CopilotClient: FakeCopilotClient, RuntimeConnection: { forUri: () => ({}) }, approveAll: () => "allow" },
    }).then(result => expect(result).toMatchObject({ exitCode: 1, hasOutput: true, output: content }));
  });

  it.each([{}, { rte: false }, { rte: true, parentToolCallId: "child" }])("does not fail ordinary quoted text or a nested filtered response %j", context => {
    const message = { ...filteredMessage, data: { content: filteredMessage.data.content, ...context } };
    let handler;
    class FakeCopilotClient {
      start = async () => {};
      stop = async () => {};
      createSession = async () => ({
        sessionId: "not-main-filter",
        on: callback => {
          handler = callback;
        },
        sendAndWait: async () => {
          handler(message);
          return { type: "assistant.message", data: { content: "Task completed." } };
        },
        disconnect: async () => {},
      });
    }
    return runWithCopilotSDK({
      sdkUri: "http://127.0.0.1:3002",
      prompt: "test",
      logger: () => {},
      workingDirectory: base,
      agentsBaseDir: base,
      sdkModule: { CopilotClient: FakeCopilotClient, RuntimeConnection: { forUri: () => ({}) }, approveAll: () => "allow" },
    }).then(result => expect(result.exitCode).toBe(0));
  });

  it("resolves engine.cwd against the workspace rather than the driver process directory", async () => {
    vi.stubEnv("GITHUB_WORKSPACE", "/w");
    vi.stubEnv("GH_AW_ENGINE_CWD", "packages/app");
    const cwd = vi.spyOn(process, "cwd").mockReturnValue("/w/packages/app");
    expect(resolveCopilotSDKWorkingDirectory()).toBe("/w/packages/app");
    cwd.mockRestore();

    const app = path.join(base, "packages/app");
    fs.mkdirSync(path.join(app, ".github/agents"), { recursive: true });
    fs.writeFileSync(path.join(app, ".github/agents/local.agent.md"), "---\nmodel: small\n---\nApp agent.");
    // The harness resolved `small` before the driver started.
    vi.stubEnv("GH_AW_COPILOT_SDK_SUBAGENT_MODELS", JSON.stringify({ local: "claude-sonnet-4.6" }));
    vi.stubEnv("GITHUB_WORKSPACE", base);
    vi.spyOn(process, "cwd").mockReturnValue(app);
    const createSession = vi.fn().mockResolvedValue({
      sessionId: "relative-cwd",
      on: () => {},
      sendAndWait: async () => ({ data: { content: "done" } }),
      disconnect: async () => {},
    });
    class FakeCopilotClient {
      start = async () => {};
      stop = async () => {};
      createSession = createSession;
    }
    const result = await runWithCopilotSDK({
      sdkUri: "http://127.0.0.1:3002",
      prompt: "test",
      logger: () => {},
      agentsBaseDir: base,
      sdkModule: { CopilotClient: FakeCopilotClient, RuntimeConnection: { forUri: () => ({}) }, approveAll: () => "allow" },
    });
    expect(result.exitCode).toBe(0);
    expect(createSession).toHaveBeenCalledWith(
      expect.objectContaining({
        workingDirectory: app,
        customAgents: [{ name: "local", displayName: "local", prompt: "App agent.", model: "anthropic/claude-sonnet-4.6" }],
      })
    );
  });

  it.each([false, true])("registers all four staged Copilot Opt skills without ambient discovery (bare=%s)", async bare => {
    const workflow = fs.readFileSync(path.resolve(import.meta.dirname, "../../../.github/workflows/copilot-opt.md"), "utf8");
    const staging = path.join(base, "staged");
    vi.stubGlobal("core", { info: vi.fn(), warning: vi.fn() });
    vi.stubEnv("GH_AW_SKILL_DIR", ".github/skills");
    vi.stubEnv("GH_AW_SKILL_EXT", "/SKILL.md");
    const prompt = writeInlineSkills(workflow, base, staging, "copilot");
    const createSession = vi.fn().mockResolvedValue({
      sessionId: "staged-skills",
      on: () => {},
      sendAndWait: async () => ({ data: { content: "done" } }),
      disconnect: async () => {},
    });
    class FakeCopilotClient {
      start = async () => {};
      stop = async () => {};
      createSession = createSession;
    }
    const result = await runWithCopilotSDK({
      sdkUri: "http://127.0.0.1:3002",
      prompt,
      logger: () => {},
      workingDirectory: base,
      agentsBaseDir: staging,
      bare,
      sdkModule: { CopilotClient: FakeCopilotClient, RuntimeConnection: { forUri: () => ({}) }, approveAll: () => "allow" },
    });
    expect(result.exitCode).toBe(0);
    const config = createSession.mock.calls[0][0];
    expect(config).toMatchObject({ skipCustomInstructions: bare, enableConfigDiscovery: false, enableSkills: true, skillDirectories: [path.join(staging, ".github/skills")] });
    const names = ["opt-phase-performance", "opt-phase-pr-analysis", "opt-phase-recommendations", "opt-phase-issue-creation"];
    expect(fs.readdirSync(config.skillDirectories[0]).sort()).toEqual([...names].sort());
    for (const name of names) {
      expect(fs.readFileSync(path.join(config.skillDirectories[0], name, "SKILL.md"), "utf8")).toContain("description:");
      expect(prompt).toContain(`/tmp/gh-aw/.github/skills/${name}/SKILL.md`);
      expect(prompt).not.toContain(`Invoke skill \`${name}\``);
    }
    expect(prompt).toContain("Do not use `cd`, `cut`, or `sed`");
    const document = parseDocument(extractFrontmatterAndBody(workflow).frontmatterText).toJS();
    expect(document["max-tool-denials"]).toBe(3);
    for (const command of ["cd *", "cut *", "sed *", "*"]) expect(document.tools.bash).not.toContain(command);
  });

  it("does not discover repository skills when no workflow skills were staged", async () => {
    fs.mkdirSync(path.join(base, ".github/skills/ambient"), { recursive: true });
    fs.writeFileSync(path.join(base, ".github/skills/ambient/SKILL.md"), "---\ndescription: Ambient repository skill\n---\nNot staged.");
    const createSession = vi.fn().mockResolvedValue({
      sessionId: "no-staged-skills",
      on: () => {},
      sendAndWait: async () => ({ data: { content: "done" } }),
      disconnect: async () => {},
    });
    class FakeCopilotClient {
      start = async () => {};
      stop = async () => {};
      createSession = createSession;
    }
    const result = await runWithCopilotSDK({
      sdkUri: "http://127.0.0.1:3002",
      prompt: "test",
      logger: () => {},
      workingDirectory: base,
      agentsBaseDir: path.join(base, "staged"),
      sdkModule: { CopilotClient: FakeCopilotClient, RuntimeConnection: { forUri: () => ({}) }, approveAll: () => "allow" },
    });
    expect(result.exitCode).toBe(0);
    expect(createSession.mock.calls[0][0]).toMatchObject({ enableConfigDiscovery: false, skillDirectories: [] });
    expect(createSession.mock.calls[0][0].enableSkills).toBeUndefined();
  });

  it.each(["file", "symlink", "dangling symlink"])("rejects a staged skills root that is a %s", async kind => {
    const staging = path.join(base, "staged");
    const skills = path.join(staging, ".github/skills");
    fs.mkdirSync(path.dirname(skills), { recursive: true });
    if (kind === "file") fs.writeFileSync(skills, "Not a directory.");
    else fs.symlinkSync(kind === "dangling symlink" ? path.join(base, "does-not-exist") : base, skills, "dir");
    const createSession = vi.fn();
    class FakeCopilotClient {
      start = async () => {};
      stop = async () => {};
      createSession = createSession;
    }
    const logger = vi.fn();
    const result = await runWithCopilotSDK({
      sdkUri: "http://127.0.0.1:3002",
      prompt: "test",
      logger,
      workingDirectory: base,
      agentsBaseDir: staging,
      sdkModule: { CopilotClient: FakeCopilotClient, RuntimeConnection: { forUri: () => ({}) }, approveAll: () => "allow" },
    });
    expect(result.exitCode).toBe(1);
    expect(result.output).toContain("Staged Copilot skills must be a directory:");
    expect(logger).toHaveBeenCalledWith(expect.stringContaining("error: Staged Copilot skills must be a directory:"));
    expect(createSession).not.toHaveBeenCalled();
  });

  it.each(["{bad json", '{"args":["--no-custom-instructions"]}'])("warns and starts a session for invalid server args %s", serverArgs => {
    vi.stubEnv("GH_AW_COPILOT_SDK_SERVER_ARGS", serverArgs);
    const createSession = vi.fn().mockResolvedValue({
      sessionId: "invalid-args",
      on: () => {},
      sendAndWait: async () => ({ data: { content: "done" } }),
      disconnect: async () => {},
    });
    class FakeCopilotClient {
      start = async () => {};
      stop = async () => {};
      createSession = createSession;
    }
    const logger = vi.fn();
    return runWithCopilotSDK({
      sdkUri: "http://127.0.0.1:3002",
      prompt: "test",
      logger,
      agentsBaseDir: base,
      sdkModule: { CopilotClient: FakeCopilotClient, RuntimeConnection: { forUri: () => ({}) }, approveAll: () => "allow" },
    }).then(result => {
      expect(result.exitCode).toBe(0);
      expect(createSession).toHaveBeenCalledWith(expect.objectContaining({ skipCustomInstructions: false }));
      expect(logger).toHaveBeenCalledWith(expect.stringMatching(/warning: Invalid GH_AW_COPILOT_SDK_SERVER_ARGS.*using sidecar default args/));
    });
  });

  it("preserves explicit agent models without a BYOK catalog or misleading warnings", async () => {
    vi.stubEnv("GH_AW_COPILOT_SDK_MULTI_PROVIDER_JSON", "");
    fs.mkdirSync(path.join(base, ".github/agents"), { recursive: true });
    fs.writeFileSync(path.join(base, ".github/agents/researcher.agent.md"), "---\nmodel: claude-sonnet-4.6\n---\nResearch carefully.");
    const createSession = vi.fn().mockResolvedValue({
      sessionId: "no-catalog",
      on: () => {},
      sendAndWait: async () => ({ data: { content: "done" } }),
      disconnect: async () => {},
    });
    class FakeCopilotClient {
      start = async () => {};
      stop = async () => {};
      createSession = createSession;
    }
    const logger = vi.fn();
    const result = await runWithCopilotSDK({
      sdkUri: "http://127.0.0.1:3002",
      prompt: "test",
      model: "gpt-5.4",
      logger,
      agentsBaseDir: base,
      sdkModule: { CopilotClient: FakeCopilotClient, RuntimeConnection: { forUri: () => ({}) }, approveAll: () => "allow" },
    });
    expect(result.exitCode).toBe(0);
    expect(createSession).toHaveBeenCalledWith(expect.objectContaining({ model: "gpt-5.4", customAgents: [{ name: "researcher", displayName: "researcher", prompt: "Research carefully.", model: "claude-sonnet-4.6" }] }));
    expect(logger.mock.calls.flat().some(message => message.includes("warning: custom agent"))).toBe(false);
  });

  it("fails an unknown session model explicitly rather than warning and continuing", async () => {
    const createSession = vi.fn();
    class FakeCopilotClient {
      start = async () => {};
      stop = async () => {};
      createSession = createSession;
    }
    const logger = vi.fn();
    const result = await runWithCopilotSDK({
      sdkUri: "http://127.0.0.1:3002",
      prompt: "test",
      model: "unavailable-session-model",
      logger,
      agentsBaseDir: base,
      sdkModule: { CopilotClient: FakeCopilotClient, RuntimeConnection: { forUri: () => ({}) }, approveAll: () => "allow" },
    });
    expect(result.exitCode).toBe(1);
    expect(result.output).toMatch(/Cannot qualify session model "unavailable-session-model"/);
    expect(logger).toHaveBeenCalledWith(expect.stringMatching(/error: Cannot qualify session model/));
    expect(createSession).not.toHaveBeenCalled();
  });

  it("routes Contribution Check's GPT delegates through Responses under a Claude main without changing their models or prompts", async () => {
    const root = path.resolve(import.meta.dirname, "../../..");
    const workflow = fs.readFileSync(path.join(root, ".github/workflows/contribution-check.md"), "utf8");
    const { frontmatterText } = extractFrontmatterAndBody(workflow);
    const document = parseDocument(frontmatterText);
    expect(document.errors).toEqual([]);
    expect(document.toJS().engine).toMatchObject({ id: "copilot", "copilot-sdk": true, agent: "contribution-checker" });
    const lock = parseDocument(fs.readFileSync(path.join(root, ".github/workflows/contribution-check.lock.yml"), "utf8"));
    expect(lock.errors).toEqual([]);
    const executionEnv = lock.toJS().jobs.agent.steps.find(step => step.id === "agentic_execution").env;
    expect(String(executionEnv.GH_AW_COPILOT_SDK_DRIVER)).toBe("1");
    expect(executionEnv.COPILOT_PROVIDER_WIRE_API).toBeUndefined();

    const { agents } = extractInlineSubAgents(workflow);
    expect(agents.map(agent => agent.name)).toEqual(["report-formatter", "comment-dispatcher"]);
    fs.mkdirSync(path.join(base, ".github/agents"), { recursive: true });
    for (const agent of agents) fs.writeFileSync(path.join(base, `.github/agents/${agent.name}.agent.md`), agent.content);

    // Endpoint capabilities observed in Contribution Check run 39098581712.
    const config = resolveMultiProviderFromReflect({
      model: "claude-sonnet-5.5",
      reflectData: {
        endpoints: [
          {
            provider: "copilot",
            configured: true,
            port: 10002,
            models: ["claude-sonnet-5.5", "claude-haiku-4.5", "gpt-6-luna"],
            routing_models: [
              { model_id: "claude-sonnet-5.5", supported_endpoints: ["/v1/messages", "/chat/completions"] },
              { model_id: "claude-haiku-4.5", supported_endpoints: ["/chat/completions", "/v1/messages"] },
              { model_id: "gpt-6-luna", supported_endpoints: ["/responses", "ws:/responses"] },
            ],
          },
        ],
      },
    });
    vi.stubEnv("GH_AW_COPILOT_SDK_MULTI_PROVIDER_JSON", JSON.stringify(config));
    const createSession = vi.fn().mockResolvedValue({
      sessionId: "contribution-check-endpoints",
      on: () => {},
      sendAndWait: async () => ({ data: { content: "done" } }),
      disconnect: async () => {},
    });
    class FakeCopilotClient {
      start = async () => {};
      stop = async () => {};
      createSession = createSession;
    }
    const logger = vi.fn();
    const result = await runWithCopilotSDK({
      sdkUri: "http://127.0.0.1:3002",
      prompt: "test",
      logger,
      workingDirectory: base,
      agentsBaseDir: base,
      sdkModule: { CopilotClient: FakeCopilotClient, RuntimeConnection: { forUri: () => ({}) }, approveAll: () => "allow" },
    });
    expect(result.exitCode).toBe(0);
    const session = createSession.mock.calls[0][0];
    expect(session.model).toBe("copilot-completions/claude-sonnet-5.5");
    expect(session.providers).toEqual([
      { name: "copilot-responses", type: "openai", baseUrl: "http://api-proxy:10002", wireApi: "responses" },
      { name: "copilot-completions", type: "openai", baseUrl: "http://api-proxy:10002", wireApi: "completions" },
    ]);
    expect(session.models).toContainEqual({ id: "claude-haiku-4.5", provider: "copilot-completions" });
    expect(session.customAgents).toHaveLength(2);
    for (const agent of agents) {
      expect(session.customAgents).toContainEqual(
        expect.objectContaining({
          name: agent.name,
          model: "copilot-responses/gpt-6-luna",
          prompt: extractFrontmatterAndBody(agent.content).markdown.trim(),
        })
      );
    }
    expect(logger.mock.calls.flat().some(message => message.includes("warning: custom agent"))).toBe(false);
  });

  it("attributes SDK lifecycle and metrics to declared names instead of task display names", async () => {
    let handler;
    const events = [
      { type: "subagent.started", data: { invocationId: "child", agentName: "file-summarizer", agentDisplayName: "readme-summarizer" } },
      { type: "subagent.completed", data: { invocationId: "child", agentName: "file-summarizer", agentDisplayName: "summarize-readme" } },
      { type: "session.shutdown", data: { agentMetrics: { child: { agentName: "file-summarizer", agentDisplayName: "readme-summarizer" }, main: { totalNanoAiu: 1 } } } },
    ];
    class FakeCopilotClient {
      start = async () => {};
      stop = async () => {};
      createSession = async () => ({
        sessionId: "declared-names",
        on: callback => {
          handler = callback;
        },
        sendAndWait: async () => {
          events.forEach(event => handler(event));
          return { data: { content: "done" } };
        },
        disconnect: async () => {},
      });
    }
    const result = await runWithCopilotSDK({
      sdkUri: "http://127.0.0.1:3002",
      prompt: "test",
      logger: () => {},
      agentsBaseDir: base,
      sdkModule: { CopilotClient: FakeCopilotClient, RuntimeConnection: { forUri: () => ({}) }, approveAll: () => "allow" },
    });
    expect(result.exitCode).toBe(0);
    const captured = fs
      .readFileSync(path.join(base, "declared-names/events.jsonl"), "utf8")
      .trim()
      .split("\n")
      .map(line => JSON.parse(line));
    expect(captured[0].data.agentDisplayName).toBe("file-summarizer");
    expect(captured[1].data.agentDisplayName).toBe("file-summarizer");
    expect(captured[2].data.agentMetrics.child.agentDisplayName).toBe("file-summarizer");
    expect(captured[2].data.agentMetrics.main).toEqual({ totalNanoAiu: 1 });
    expect(events[0].data.agentDisplayName).toBe("readme-summarizer");
    expect(events[2].data.agentMetrics.child.agentDisplayName).toBe("readme-summarizer");
  });

  it("captures native tool evidence once and enriches only in the canonical adapter", async () => {
    let handler;
    // Native arguments and nameless completion shapes rechecked against Copilot
    // 1.0.90 run 38099607416, artifact 11686649002, copilot-session-state events.jsonl.
    // SDK transport, aliases and child interleaving are synthetic compatibility cases.
    const events = [
      { type: "tool.execution_start", id: "main-start", data: { toolCallId: "same", toolName: "bash", arguments: { command: "echo sanitized" }, toolTitle: "Sanitized title" } },
      { type: "tool.execution_start", id: "child-start", data: { parentToolCallId: "launch", toolCallId: "same", toolName: "read", mcpServerName: "", parameters: { path: "example.txt" } } },
      { type: "tool.execution_complete", id: "child-end", data: { parentToolCallId: "launch", toolCallId: "same", success: false, error: { code: "DENIED" } } },
      { type: "tool.execution_complete", id: "main-end", data: { toolCallId: "same", success: true, shellExecution: { exitCode: 0 }, result: { content: "sanitized" } } },
      { type: "tool.execution_complete", id: "orphan", data: { toolCallId: "orphan", toolDescription: { name: "observed-name" } } },
      { type: "vendor.observation", id: "unknown", data: { native: [null, false, 0] } },
    ];
    const original = structuredClone(events);
    class FakeCopilotClient {
      start = async () => {};
      stop = async () => {};
      createSession = async () => ({
        sessionId: "native-tools",
        on: callback => {
          handler = callback;
        },
        sendAndWait: async () => {
          events.forEach(event => handler(event));
          return { data: { content: "done" } };
        },
        disconnect: async () => {},
      });
    }
    const result = await runWithCopilotSDK({
      sdkUri: "http://127.0.0.1:3002",
      prompt: "test",
      logger: () => {},
      agentsBaseDir: base,
      sdkModule: { CopilotClient: FakeCopilotClient, RuntimeConnection: { forUri: () => ({}) }, approveAll: () => "allow" },
    });
    expect(result.exitCode).toBe(0);
    const captured = fs.readFileSync(path.join(base, "native-tools/events.jsonl"), "utf8").trim().split("\n").map(JSON.parse);
    expect(captured.slice(0, events.length)).toEqual(original);
    expect(events).toEqual(original);
    const normalized = normalizeCopilotSession(captured);
    const byId = id => normalized.find(event => event.id === id);
    expect(byId("main-start").data).toMatchObject({ input: { command: "echo sanitized" }, command: "echo sanitized", toolTitle: "Sanitized title" });
    expect(byId("child-start").data.input).toEqual({ path: "example.txt" });
    expect(byId("child-end").data).toMatchObject({ toolName: "read", mcpServerName: "", parentToolUseId: "launch", success: false, error: { code: "DENIED" } });
    expect(byId("main-end").data).toMatchObject({ toolName: "bash", exitCode: 0, success: true, result: { content: "sanitized" } });
    expect(byId("orphan").data.toolName).toBe("observed-name");
    expect(byId("orphan").data.success).toBeUndefined();
    expect(byId("unknown")).toEqual(original.at(-1));
    expect(normalizeCopilotSession(JSON.parse(JSON.stringify(normalized)))).toEqual(normalized);
  });

  it("retains CLI 1.0.98 / SDK 1.0.22 dispatch, file-edit and human-response evidence but excludes shell chunks", async () => {
    let handler;
    // Synthetic transport fixtures follow SDK 1.0.22's generated/session-events.d.ts,
    // not a live Actions run. Shell chunks and logical dispatches are not tool or session completions.
    const events = [
      { type: "tool.execution_start", id: "start", data: { toolCallId: "shell", toolName: "bash", arguments: { command: "printf 'hello\\n'" } } },
      { type: "tool.shell_output", id: "stdout", ephemeral: true, data: { toolCallId: "shell", sequence: 0, stream: "stdout", text: "hel" } },
      { type: "tool.shell_output", id: "stderr", ephemeral: true, agentId: "child", data: { toolCallId: "shell", sequence: 1, stream: "stderr", text: "warning\n" } },
      { type: "tool.shell_output", id: "stdout-next", ephemeral: true, data: { toolCallId: "shell", sequence: 2, stream: "stdout", text: "lo\n" } },
      { type: "model.call_finished", id: "dispatch", ephemeral: true, data: { dispatchDurationMs: 100, editClassifierVersion: 1, outcome: "success", turnId: "0" } },
      { type: "tool.execution_complete", id: "edit", data: { toolCallId: "edit", success: false, fileEdits: [{ kind: "edit", path: "/workspace/partial.txt" }], error: { message: "Second edit failed" } } },
      { type: "human_response.recorded", id: "response", data: { actor: "host_automation", requestId: "request", response: { responseKind: "user_input", question: "Continue?", answer: "No", wasFreeform: false } } },
    ];
    class FakeCopilotClient {
      start = async () => {};
      stop = async () => {};
      createSession = async () => ({
        sessionId: "release-events",
        on: callback => {
          handler = callback;
        },
        sendAndWait: async () => {
          events.forEach(event => handler(event));
          handler({ type: "vendor.transient", ephemeral: true, data: {} });
          return { data: { content: "done" } };
        },
        disconnect: async () => {},
      });
    }
    const original = structuredClone(events);
    const result = await runWithCopilotSDK({
      sdkUri: "http://127.0.0.1:3002",
      prompt: "test",
      logger: () => {},
      agentsBaseDir: base,
      sdkModule: { CopilotClient: FakeCopilotClient, RuntimeConnection: { forUri: () => ({}) }, approveAll: () => "allow" },
    });
    expect(result.exitCode).toBe(0);
    const captured = fs.readFileSync(path.join(base, "release-events/events.jsonl"), "utf8").trim().split("\n").map(JSON.parse);
    const retained = original.filter(event => event.type !== "tool.shell_output");
    expect(captured.slice(0, retained.length)).toEqual(retained);
    expect(captured.some(event => event.type === "tool.shell_output")).toBe(false);
    expect(captured.some(event => event.type === "vendor.transient")).toBe(false);
    const normalized = normalizeCopilotSession(captured);
    for (const event of retained.slice(1)) expect(normalized.find(entry => entry.id === event.id)).toEqual(event);
    expect(normalized.filter(event => event.type === "tool.execution_complete")).toHaveLength(1);
    expect(normalized.some(event => event.type === "session.result")).toBe(false);
    expect(events).toEqual(original);
    expect(normalizeCopilotSession(JSON.parse(JSON.stringify(normalized)))).toEqual(normalized);
  });

  it.each([true, false])("excludes credentials split across shell chunks from JSONL and stderr (ephemeral=%s)", async ephemeral => {
    let handler;
    const stderr = vi.spyOn(process.stderr, "write").mockImplementation(() => true);
    const fragments = ["ghp_" + "A".repeat(18), "B".repeat(18)];
    class FakeCopilotClient {
      start = async () => {};
      stop = async () => {};
      createSession = async () => ({
        sessionId: "split-credential",
        on: callback => {
          handler = callback;
        },
        sendAndWait: async () => {
          for (const agentId of [undefined, "child"]) {
            for (const stream of ["stdout", "stderr"]) {
              fragments.forEach((text, sequence) => handler({ type: "tool.shell_output", ephemeral, agentId, data: { toolCallId: "shell", stream, sequence, text } }));
            }
          }
          return { data: { content: "done" } };
        },
        disconnect: async () => {},
      });
    }
    const result = await runWithCopilotSDK({
      sdkUri: "http://127.0.0.1:3002",
      prompt: "test",
      logger: () => {},
      agentsBaseDir: base,
      sdkModule: { CopilotClient: FakeCopilotClient, RuntimeConnection: { forUri: () => ({}) }, approveAll: () => "allow" },
    });
    expect(result.exitCode).toBe(0);
    const raw = fs.readFileSync(path.join(base, "split-credential/events.jsonl"), "utf8");
    const serialized = JSON.stringify(normalizeCopilotSession(raw.trim().split("\n").map(JSON.parse)));
    const logged = stderr.mock.calls.map(([text]) => String(text)).join("");
    for (const text of [raw, serialized, logged]) {
      expect(text.includes("tool.shell_output")).toBe(false);
      for (const fragment of fragments) expect(text.includes(fragment)).toBe(false);
    }
  });

  it("does not postpone the completion watchdog for background shell chunks", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    vi.stubEnv("GH_AW_SDK_IDLE_MS", "30");
    vi.spyOn(process.stderr, "write").mockImplementation(() => true);
    let handler;
    let rejectSend;
    let markSending;
    const sending = new Promise(resolve => {
      markSending = resolve;
    });
    const disconnect = vi.fn(async () => {
      rejectSend(new Error("Session disconnected"));
    });
    const logger = vi.fn();
    class FakeCopilotClient {
      start = async () => {};
      stop = async () => {};
      createSession = async () => ({
        sessionId: "background-shell",
        on: callback => {
          handler = callback;
        },
        sendAndWait: () => {
          const pending = new Promise((_resolve, reject) => {
            rejectSend = reject;
          });
          handler({ type: "tool.execution_start", data: { toolCallId: "shell", toolName: "bash" } });
          handler({ type: "assistant.message", data: { content: "done" } });
          handler({ type: "tool.execution_complete", data: { toolCallId: "shell", success: true } });
          markSending();
          return pending;
        },
        disconnect,
      });
    }
    const running = runWithCopilotSDK({
      sdkUri: "http://127.0.0.1:3002",
      prompt: "test",
      logger,
      agentsBaseDir: base,
      sdkModule: { CopilotClient: FakeCopilotClient, RuntimeConnection: { forUri: () => ({}) }, approveAll: () => "allow" },
    });
    await sending;
    for (const sequence of [0, 1]) {
      await vi.advanceTimersByTimeAsync(10);
      handler({ type: "tool.shell_output", ephemeral: true, data: { toolCallId: "shell", stream: "stdout", sequence, text: "background output" } });
    }
    expect(disconnect).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(10);
    expect(disconnect).toHaveBeenCalled();
    expect(await running).toMatchObject({ exitCode: 0, output: "done", hasOutput: true });
    expect(logger.mock.calls.flat().some(message => message.includes("post-completion watchdog triggered disconnect"))).toBe(true);
    expect(vi.getTimerCount()).toBe(0);
  });

  it("ignores shell and completion callbacks during shutdown and after disconnect", async () => {
    let handler;
    const stderr = vi.spyOn(process.stderr, "write").mockImplementation(() => true);
    const lateEvents = [
      { type: "tool.shell_output", ephemeral: true, data: { toolCallId: "late", stream: "stdout", text: "late output" } },
      { type: "tool.execution_complete", data: { toolCallId: "late", success: true } },
      { type: "assistant.message", data: { content: "late answer" } },
    ];
    const disconnect = vi.fn(async () => {
      lateEvents.forEach(event => handler(event));
    });
    class FakeCopilotClient {
      start = async () => {};
      stop = async () => {};
      createSession = async () => ({
        sessionId: "late-callback",
        on: callback => {
          handler = callback;
        },
        sendAndWait: async () => ({ data: { content: "done" } }),
        disconnect,
      });
    }
    const result = await runWithCopilotSDK({
      sdkUri: "http://127.0.0.1:3002",
      prompt: "test",
      logger: () => {},
      agentsBaseDir: base,
      sdkModule: { CopilotClient: FakeCopilotClient, RuntimeConnection: { forUri: () => ({}) }, approveAll: () => "allow" },
    });
    expect(result).toMatchObject({ exitCode: 0, output: "done" });
    expect(disconnect).toHaveBeenCalledTimes(1);
    lateEvents.forEach(event => handler(event));
    await new Promise(resolve => setImmediate(resolve));
    const captured = fs.readFileSync(path.join(base, "late-callback/events.jsonl"), "utf8").trim().split("\n").map(JSON.parse);
    expect(captured).toEqual([{ type: "assistant.message", data: { content: "done" } }]);
    expect(stderr.mock.calls.map(([text]) => String(text)).join("")).not.toContain("late");
  });

  it.each(["routing", "environment", "fallback", "reflect"])("qualifies the %s model before createSession", async source => {
    const expected = source === "reflect" ? "anthropic/claude-sonnet-4.6" : "openai/gpt-5.4";
    if (source === "routing" || source === "environment") vi.stubEnv("COPILOT_MODEL", "gpt-5.4");
    if (source === "routing") {
      vi.stubEnv("GH_AW_MODEL_ROUTING", "1");
      vi.stubEnv("GH_AW_MODEL_FALLBACK", "claude-sonnet-4.6");
    }
    if (source === "fallback") vi.stubEnv("GH_AW_MODEL_FALLBACK", "gpt-5.4");
    vi.stubEnv("GH_AW_COPILOT_SDK_SERVER_ARGS", '["--no-custom-instructions"]');
    const createSession = vi.fn().mockResolvedValue({
      sessionId: source,
      on: () => {},
      sendAndWait: async () => ({ data: { content: "done" } }),
      disconnect: async () => {},
    });
    class FakeCopilotClient {
      start = async () => {};
      stop = async () => {};
      createSession = createSession;
    }
    const result = await runWithCopilotSDK({
      sdkUri: "http://127.0.0.1:3002",
      prompt: "test",
      logger: () => {},
      agentsBaseDir: base,
      sdkModule: { CopilotClient: FakeCopilotClient, RuntimeConnection: { forUri: () => ({}) }, approveAll: () => "allow" },
    });
    expect(result.exitCode).toBe(0);
    expect(createSession).toHaveBeenCalledWith(expect.objectContaining({ model: expected, providers, models, workingDirectory: base, skipCustomInstructions: true, customAgents: [] }));
  });
});
