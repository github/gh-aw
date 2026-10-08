// @ts-check
"use strict";

const fs = require("node:fs");
const path = require("node:path");
const os = require("node:os");
const { randomUUID } = require("node:crypto");
const { spawn } = require("node:child_process");
const { getErrorMessage } = require("./error_helpers.cjs");
const { sanitizeProviderErrorMessage } = require("./pi_provider_error.cjs");

/** @param {any} agent @param {string} promptPath */
function subagentArgs(agent, promptPath) {
  let inherited;
  try {
    inherited = JSON.parse(process.env.GH_AW_PI_SUBAGENT_ARGS || '["--print","--mode","json","--no-session","--no-approve"]');
  } catch (error) {
    throw new Error("Cannot parse Pi sub-agent arguments", { cause: error });
  }
  if (!Array.isArray(inherited) || inherited.some(arg => typeof arg !== "string")) throw new Error("Invalid Pi sub-agent arguments");
  const args = [...inherited, "--no-extensions", "--model", agent.model];
  const actionsDir = process.env.RUNNER_TEMP ? path.join(process.env.RUNNER_TEMP, "gh-aw/actions") : __dirname;
  for (const file of ["pi_provider.cjs", "pi_steering_extension.cjs", "pi_tool_policy.cjs"]) args.push("--extension", path.join(actionsDir, file));
  for (const builtin of ["mcp", "codemode", "tool-search"]) args.push("--extension", `builtin:${builtin}`);
  if (agent.tools) args.push("--tools", agent.tools.join(","));
  if (agent.thinking) args.push("--thinking", agent.thinking);
  args.push("--append-system-prompt", promptPath);
  return args;
}

/** @param {any} agent @param {string} task @param {any} ctx @param {AbortSignal|undefined} signal @param {typeof spawn} [launch] */
async function runPiSubagent(agent, task, ctx, signal, launch = spawn) {
  if (signal?.aborted) throw new Error("Pi sub-agent was aborted before dispatch");
  const minutes = Number(process.env.GH_AW_TIMEOUT_MINUTES || "30");
  if (!Number.isFinite(minutes) || minutes <= 0) throw new Error("GH_AW_TIMEOUT_MINUTES must be positive");
  const invocationId = randomUUID();
  let resultEmitted = false;
  const emitResult = (outcome, error = "") => {
    if (resultEmitted) return;
    resultEmitted = true;
    process.stdout.write(
      JSON.stringify({
        type: "gh_aw_subagent_result",
        invocation_id: invocationId,
        agent: agent.name,
        outcome,
        ...(error ? { error: sanitizeProviderErrorMessage(error).slice(0, 300) } : {}),
      }) + "\n"
    );
  };
  let dir;
  try {
    dir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-pi-subagent-"));
    const promptPath = path.join(dir, "system.txt");
    const systemPath = process.env.GH_AW_PI_SYSTEM_PROMPT;
    const system = systemPath ? fs.readFileSync(systemPath, "utf8") + "\n\n" : "";
    const delegationScope =
      "You are a delegated sub-agent, not the workflow's main agent. Complete only the delegated task and return your answer to the parent, following the declared agent's output format exactly. " +
      "The parent is responsible for finalizing the workflow and its required safe-output reporting. Do not emit noop safe outputs or workflow-completion reports for this child session. " +
      "Safe-output actions required to perform the delegated task remain permitted.";
    fs.writeFileSync(promptPath, system + agent.prompt + "\n\n" + delegationScope, { mode: 0o600 });
    process.stdout.write(JSON.stringify({ type: "gh_aw_subagent_dispatch", invocation_id: invocationId, agent: agent.name, requested_model: agent.declaredModel || agent.modelId, resolved_model: agent.modelId }) + "\n");
    return await new Promise((resolve, reject) => {
      const child = launch(process.env.GH_AW_PI_COMMAND || "pi", subagentArgs(agent, promptPath), {
        cwd: ctx.cwd,
        shell: false,
        stdio: ["pipe", "pipe", "pipe"],
        env: { ...process.env, GH_AW_PI_MODEL: `${process.env.GH_AW_PI_NATIVE_PROVIDER || "github-copilot"}/${agent.modelId}`, GH_AW_PI_SUBAGENT_CHILD: "1" },
      });
      if (!child.stdout || !child.stderr || !child.stdin) {
        child.kill("SIGTERM");
        reject(new Error("Pi sub-agent requires piped input and output"));
        return;
      }
      let buffer = "";
      let stderr = "";
      let output = "";
      let failure = "";
      let killTimer;
      const terminate = () => {
        child.kill("SIGTERM");
        killTimer ??= setTimeout(() => child.kill("SIGKILL"), 5000);
        killTimer.unref();
      };
      child.stdout.setEncoding("utf8");
      child.stderr.setEncoding("utf8");
      const parseLine = line => {
        if (!line.trim()) return;
        let event;
        try {
          event = JSON.parse(line);
        } catch {
          failure ||= "Pi sub-agent emitted an invalid JSON event";
          return;
        }
        if (!event || typeof event !== "object" || Array.isArray(event)) {
          failure ||= "Pi sub-agent emitted an invalid JSON event";
          return;
        }
        if (event.type === "extension_error") failure = event.error ? getErrorMessage(event.error) : "Pi sub-agent extension failed";
        if (event.type === "message_end" && event.message?.role === "assistant") {
          if (typeof event.message.errorMessage === "string") event.message.errorMessage = sanitizeProviderErrorMessage(event.message.errorMessage);
          output = (Array.isArray(event.message.content) ? event.message.content : [])
            .filter(part => part?.type === "text" && typeof part.text === "string")
            .map(part => part.text)
            .join("\n");
          if (["error", "aborted"].includes(event.message.stopReason))
            failure = typeof event.message.errorMessage === "string" && event.message.errorMessage ? event.message.errorMessage : `Pi sub-agent ended with ${event.message.stopReason}`;
          // Preserve child telemetry without mixing it with the parent's session events.
          process.stdout.write(JSON.stringify({ type: "gh_aw_subagent_event", invocation_id: invocationId, agent: agent.name, event }) + "\n");
        }
      };
      child.stdout.on("data", data => {
        buffer += data.toString();
        if (Buffer.byteLength(buffer) > 1024 * 1024) {
          failure = "Pi sub-agent event exceeded the 1 MiB limit";
          buffer = "";
          terminate();
          return;
        }
        const lines = buffer.split("\n");
        buffer = lines.pop() || "";
        for (const line of lines) parseLine(line);
      });
      child.stderr.on("data", data => {
        stderr = (stderr + data.toString()).slice(-16384);
      });
      const abort = () => {
        failure = "Pi sub-agent was aborted";
        terminate();
      };
      const timeout = setTimeout(() => {
        failure = `Pi sub-agent exceeded the workflow timeout (${minutes} minutes)`;
        terminate();
      }, minutes * 60000);
      timeout.unref();
      signal?.addEventListener("abort", abort, { once: true });
      child.on("error", error => {
        clearTimeout(timeout);
        clearTimeout(killTimer);
        signal?.removeEventListener("abort", abort);
        emitResult("failed", getErrorMessage(error));
        reject(new Error(`Cannot launch Pi sub-agent "${agent.name}": ${getErrorMessage(error)}`, { cause: error }));
      });
      child.on("close", (code, terminationSignal) => {
        clearTimeout(timeout);
        clearTimeout(killTimer);
        signal?.removeEventListener("abort", abort);
        if (buffer.trim()) parseLine(buffer);
        if (code !== 0 || terminationSignal || failure || !output.trim()) {
          const error = sanitizeProviderErrorMessage(failure || stderr || `Pi sub-agent exited ${code ?? terminationSignal} without an answer`);
          emitResult("failed", error);
          reject(new Error(error));
        } else {
          emitResult("completed");
          const text = output.length > 64000 ? output.slice(0, 64000) + "\n[Sub-agent output truncated at 64000 characters]" : output;
          resolve({ content: [{ type: "text", text }], details: { agent: agent.name, requestedModel: agent.declaredModel, model: agent.modelId } });
        }
      });
      child.stdin.on("error", error => {
        if (!("code" in error) || error.code !== "EPIPE") failure = getErrorMessage(error);
      });
      child.stdin.end(task);
      if (signal?.aborted) abort();
    });
  } catch (error) {
    emitResult("failed", getErrorMessage(error));
    throw new Error(`Pi sub-agent "${agent.name}" failed: ${getErrorMessage(error)}`, { cause: error });
  } finally {
    if (dir) {
      try {
        fs.rmSync(dir, { recursive: true });
      } catch (error) {
        throw new Error("Cannot clean up Pi sub-agent prompt directory", { cause: error });
      }
    }
  }
}

/** @param {any} pi */
function piSubagentExtension(pi) {
  if (process.env.GH_AW_PI_SUBAGENT_CHILD === "1") return;
  const manifest = path.join(process.env.PI_CODING_AGENT_DIR || "/tmp/gh-aw/pi-agent-dir", "subagents.json");
  if (!fs.existsSync(manifest)) return;
  let agents;
  try {
    agents = JSON.parse(fs.readFileSync(manifest, "utf8"));
  } catch (error) {
    throw new Error("Cannot read Pi sub-agent manifest", { cause: error });
  }
  if (!Array.isArray(agents)) throw new Error("Invalid Pi sub-agent manifest");
  if (agents.length === 0) return;
  pi.registerTool({
    name: "subagent",
    label: "Sub-agent",
    description: `Delegate a task to a declared agent in an isolated Pi session. Available agents: ${agents.map(agent => `${agent.name}: ${agent.description}`).join("; ")}`,
    parameters: { type: "object", properties: { agent: { type: "string", enum: agents.map(agent => agent.name) }, task: { type: "string", minLength: 1 } }, required: ["agent", "task"], additionalProperties: false },
    async execute(_id, params, signal, _onUpdate, ctx) {
      const agent = agents.find(candidate => candidate.name === params.agent);
      if (!agent || typeof params.task !== "string" || !params.task.trim()) throw new Error("Choose a declared Pi sub-agent and provide a non-empty task");
      return runPiSubagent(agent, params.task, ctx, signal);
    },
  });
}

module.exports = Object.assign(piSubagentExtension, { subagentArgs, runPiSubagent });
