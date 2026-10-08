import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { EventEmitter } from "node:events";
import { PassThrough } from "node:stream";

const { default: extension, subagentArgs, runPiSubagent } = await import("./pi_subagent_extension.cjs");
const { writeInlineSubAgents } = await import("./extract_inline_sub_agents.cjs");
const { stagePiArtifacts } = await import("./pi_runtime.cjs");
const { preparePiSubagents } = await import("./pi_subagent_config.cjs");
let dir;
const agent = { name: "reader", description: "Read facts", prompt: "Read only.", declaredModel: "small", modelId: "claude-haiku-4.5", model: "aw-gateway/claude-haiku-4.5", tools: ["read"] };

beforeEach(() => {
  dir = fs.mkdtempSync(path.join(os.tmpdir(), "pi-delegate-"));
  vi.stubEnv("PI_CODING_AGENT_DIR", dir);
  vi.stubEnv("RUNNER_TEMP", dir);
  vi.stubEnv("GH_AW_PI_SUBAGENT_ARGS", '["--print","--mode","json","--no-session","--no-approve","--exclude-tools","bash","--exclude-tools","edit,write"]');
  vi.stubEnv("GH_AW_PI_TOOL_POLICY", '{"bash":false,"edit":false}');
  vi.spyOn(process.stdout, "write").mockImplementation(() => true);
});
afterEach(() => {
  vi.restoreAllMocks();
  vi.useRealTimers();
  vi.unstubAllEnvs();
  fs.rmSync(dir, { recursive: true, force: true });
});

function launcher(message, code = 0) {
  return vi.fn((_command, _args, options) => {
    const child = Object.assign(new EventEmitter(), { stdout: new PassThrough(), stderr: new PassThrough(), stdin: new PassThrough(), kill: vi.fn() });
    queueMicrotask(() => {
      child.stdout.write(JSON.stringify({ type: "message_end", message: { role: "assistant", content: [{ type: "text", text: "Python 3.12" }], stopReason: "stop", ...message } }) + "\n");
      child.emit("close", code, null);
    });
    expect(options.env.GH_AW_PI_TOOL_POLICY).toBe('{"bash":false,"edit":false}');
    expect(options.env.GH_AW_PI_SUBAGENT_CHILD).toBe("1");
    return child;
  });
}

describe("Pi managed delegation", () => {
  it("loads only declared user-scope agents without granting project trust", () => {
    fs.writeFileSync(path.join(dir, "subagents.json"), JSON.stringify([agent]));
    const registerTool = vi.fn();
    extension({ registerTool });
    expect(registerTool).toHaveBeenCalledWith(expect.objectContaining({ name: "subagent", parameters: expect.objectContaining({ required: ["agent", "task"] }) }));
  });

  it("does not expose delegation without declarations or inside a child", () => {
    const registerTool = vi.fn();
    extension({ registerTool });
    fs.writeFileSync(path.join(dir, "subagents.json"), JSON.stringify([agent]));
    vi.stubEnv("GH_AW_PI_SUBAGENT_CHILD", "1");
    extension({ registerTool });
    expect(registerTool).not.toHaveBeenCalled();
  });

  it("passes the selected model and all parent infrastructure and restrictions", () => {
    const args = subagentArgs(agent, "/trusted/system.txt");
    expect(args).toEqual(
      expect.arrayContaining([
        "--no-session",
        "--no-approve",
        "--exclude-tools",
        "bash",
        "edit,write",
        "--model",
        agent.model,
        path.join(dir, "gh-aw/actions/pi_provider.cjs"),
        path.join(dir, "gh-aw/actions/pi_tool_policy.cjs"),
        path.join(dir, "gh-aw/actions/pi_steering_extension.cjs"),
        "builtin:mcp",
        "builtin:codemode",
        "builtin:tool-search",
      ])
    );
    expect(args).not.toContain(path.join(dir, "gh-aw/actions/pi_subagent_extension.cjs"));
  });

  it("returns the child's answer and records alias and concrete model attribution", async () => {
    const launch = launcher();
    const result = await runPiSubagent(agent, "Which Python version?", { cwd: dir }, undefined, launch);
    expect(result).toMatchObject({ content: [{ type: "text", text: "Python 3.12" }], details: { requestedModel: "small", model: "claude-haiku-4.5" } });
    expect(process.stdout.write).toHaveBeenCalledWith(expect.stringContaining('"type":"gh_aw_subagent_dispatch"'));
    expect(process.stdout.write).toHaveBeenCalledWith(expect.stringContaining('"resolved_model":"claude-haiku-4.5"'));
    expect(launch.mock.calls[0][2]).toMatchObject({ shell: false, cwd: dir });
    expect(fs.existsSync(launch.mock.calls[0][1].at(-1))).toBe(false);
  });

  it("does not claim success when inference ends with an error and CLI exits zero", async () => {
    await expect(runPiSubagent(agent, "Read.", { cwd: dir }, undefined, launcher({ stopReason: "error", errorMessage: "Model unavailable" }))).rejects.toThrow("Model unavailable");
  });

  it("inherits workflow controls without making the child finalize the workflow", async () => {
    const systemPath = path.join(dir, "workflow-system.txt");
    fs.writeFileSync(systemPath, "Workflow instructions and tool restrictions.");
    vi.stubEnv("GH_AW_PI_SYSTEM_PROMPT", systemPath);
    const launch = launcher();
    await runPiSubagent(agent, "Read.", { cwd: dir }, undefined, (...args) => {
      const prompt = fs.readFileSync(args[1].at(-1), "utf8");
      expect(prompt).toContain("Workflow instructions and tool restrictions.\n\nRead only.");
      expect(prompt).toContain("The parent is responsible for finalizing the workflow");
      expect(prompt).toContain("Do not emit noop safe outputs");
      expect(prompt).toContain("Safe-output actions required to perform the delegated task remain permitted");
      return launch(...args);
    });
  });

  it("propagates nonzero child exits", async () => {
    await expect(runPiSubagent(agent, "Read.", { cwd: dir }, undefined, launcher({}, 1))).rejects.toThrow();
  });

  it("refuses already aborted dispatches before launching", async () => {
    const launch = vi.fn();
    await expect(runPiSubagent(agent, "Read.", { cwd: dir }, AbortSignal.abort(), launch)).rejects.toThrow("aborted");
    expect(launch).not.toHaveBeenCalled();
  });

  it("validates the timeout before starting a child", async () => {
    vi.stubEnv("GH_AW_TIMEOUT_MINUTES", "invalid");
    const launch = vi.fn();
    await expect(runPiSubagent(agent, "Read.", { cwd: dir }, undefined, launch)).rejects.toThrow("must be positive");
    expect(launch).not.toHaveBeenCalled();
  });

  it("escalates cancellation when a child ignores SIGTERM", async () => {
    vi.useFakeTimers();
    const child = Object.assign(new EventEmitter(), { stdout: new PassThrough(), stderr: new PassThrough(), stdin: new PassThrough(), kill: vi.fn() });
    const controller = new AbortController();
    const pending = runPiSubagent(agent, "Read.", { cwd: dir }, controller.signal, () => child);
    controller.abort();
    expect(child.kill).toHaveBeenCalledWith("SIGTERM");
    await vi.advanceTimersByTimeAsync(5000);
    expect(child.kill).toHaveBeenCalledWith("SIGKILL");
    child.emit("close", null, "SIGKILL");
    await expect(pending).rejects.toThrow("aborted");
  });

  it.each(["not JSON\n", "null\n", "x".repeat(1024 * 1024 + 1)])("fails explicitly on invalid or oversized protocol output", async output => {
    const launch = () => {
      const child = Object.assign(new EventEmitter(), { stdout: new PassThrough(), stderr: new PassThrough(), stdin: new PassThrough(), kill: vi.fn() });
      queueMicrotask(() => {
        child.stdout.write(output);
        child.emit("close", 0, null);
      });
      return child;
    };
    await expect(runPiSubagent(agent, "Read.", { cwd: dir }, undefined, launch)).rejects.toThrow();
  });

  it("runs a real isolated child process with stdin instructions and preserved UTF-8", async () => {
    const executable = path.join(dir, "fake-pi");
    fs.writeFileSync(
      executable,
      `#!/usr/bin/env node
let task = "";
process.stdin.setEncoding("utf8");
process.stdin.on("data", chunk => task += chunk);
process.stdin.on("end", () => {
  if (task !== "Summarize README.md" || process.env.GH_AW_PI_SUBAGENT_CHILD !== "1") process.exit(2);
  const args = process.argv.slice(2);
  if (!args.includes("aw-gateway/claude-haiku-4.5") || !args.includes("--no-session")) process.exit(3);
  const event = { type: "message_end", message: { role: "assistant", content: [{ type: "text", text: "R\\u00e9sum\\u00e9" }] } };
  const bytes = Buffer.from(JSON.stringify(event) + "\\n");
  for (const byte of bytes) process.stdout.write(Buffer.from([byte]));
});
`,
      { mode: 0o700 }
    );
    vi.stubEnv("GH_AW_PI_COMMAND", executable);
    const result = await runPiSubagent(agent, "Summarize README.md", { cwd: dir });
    expect(result.content[0].text).toBe("R\u00e9sum\u00e9");
  });

  it("extracts and dispatches all three agents from the Pi smoke workflow", async () => {
    const originalEnv = process.env;
    try {
      process.env = {
        PATH: originalEnv.PATH,
        RUNNER_TEMP: dir,
        PI_CODING_AGENT_DIR: path.join(dir, "managed"),
        GH_AW_PI_STAGING_DIR: path.join(dir, ".pi"),
        GH_AW_SUB_AGENT_DIR: ".pi/agents",
        GH_AW_SUB_AGENT_EXT: ".md",
        GH_AW_PI_SUBAGENT_ARGS: originalEnv.GH_AW_PI_SUBAGENT_ARGS,
        GH_AW_PI_TOOL_POLICY: originalEnv.GH_AW_PI_TOOL_POLICY,
        GH_AW_PI_MODEL_ALIASES: JSON.stringify({ "gpt-5-mini": ["copilot/gpt-5*mini*"], "gpt-5-nano": ["copilot/gpt-5*nano*"] }),
      };
      vi.stubGlobal("core", { info: vi.fn() });
      const source = fs.readFileSync(new URL("../../../.github/workflows/smoke-pi-sub-agents.md", import.meta.url), "utf8");
      const parentPrompt = writeInlineSubAgents(source, dir, dir, "pi");
      expect(parentPrompt).not.toContain("## agent:");
      stagePiArtifacts(process.env.PI_CODING_AGENT_DIR);
      const expected = ["claude-haiku-4.5", "gpt-5-mini", "gpt-4o-mini"];
      const sdk = {
        parseFrontmatter: content => {
          const [, header, body] = content.split("---");
          const frontmatter = Object.fromEntries(
            header
              .trim()
              .split("\n")
              .map(line => {
                const colon = line.indexOf(":");
                return [line.slice(0, colon), line.slice(colon + 1).trim()];
              })
          );
          return { frontmatter, body };
        },
      };
      const agents = preparePiSubagents({
        agentDir: process.env.PI_CODING_AGENT_DIR,
        sdk,
        provider: "github-copilot",
        catalog: [...expected, "gpt-5.4-mini"].map(model => `github-copilot/${model}`),
        gateway: true,
        parentModel: "github-copilot/gpt-5.3-codex",
      });
      expect(agents.map(agent => agent.modelId).sort()).toEqual([...expected].sort());
      const executable = path.join(dir, "fixture-pi");
      fs.writeFileSync(
        executable,
        `#!/usr/bin/env node
let task = "";
process.stdin.setEncoding("utf8");
process.stdin.on("data", chunk => task += chunk);
process.stdin.on("end", () => {
  const args = process.argv.slice(2);
  const model = args[args.indexOf("--model") + 1]?.replace(/^aw-gateway\\//, "");
  if (task !== "who am i?" || !${JSON.stringify(expected)}.includes(model) || !args.includes("--no-session")) process.exit(2);
  process.stdout.write(JSON.stringify({ type: "message_end", message: { role: "assistant", model, content: [{ type: "text", text: model }] } }) + "\\n");
});
`,
        { mode: 0o700 }
      );
      process.env.GH_AW_PI_COMMAND = executable;
      for (const agent of agents) {
        const result = await runPiSubagent(agent, "who am i?", { cwd: dir });
        expect(result.content[0].text).toBe(agent.modelId);
        expect(result.details.model).toBe(agent.modelId);
      }
    } finally {
      process.env = originalEnv;
      vi.unstubAllGlobals();
    }
  });
});
