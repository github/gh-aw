// @ts-check
"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { loadPiSDK, resolvePiPackageFile, preparePiRuntime, nativePiProvider } = require("./pi_runtime.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");

/** @param {string[]} args @returns {string[]} */
function rpcArgs(args) {
  const result = [];
  for (let i = 0; i < args.length; i++) {
    if (args[i] === "--print") continue;
    if (args[i] === "--mode") {
      i++;
      continue;
    }
    result.push(args[i]);
  }
  return result;
}

/** @param {{ sdk?: any, cliPath?: string, emit?: (event: any) => void }} [options] */
async function main(options = {}) {
  const sdk = options.sdk || (await loadPiSDK());
  const emit = options.emit || (event => process.stdout.write(JSON.stringify(event) + "\n"));
  const { agentDir, config } = preparePiRuntime();
  const cwd = process.env.GH_AW_ENGINE_CWD || process.env.GITHUB_WORKSPACE || process.cwd();
  const promptPath = process.env.GH_AW_PI_USER_PROMPT || process.env.GH_AW_PROMPT;
  if (!promptPath) throw new Error("GH_AW_PROMPT is required");
  const configuredModel = process.env.GH_AW_PI_MODEL || "";
  const slash = configuredModel.indexOf("/");
  const provider = process.env.GH_AW_PI_NATIVE_PROVIDER || nativePiProvider(slash >= 0 ? configuredModel.slice(0, slash) : "copilot");
  const modelId = slash >= 0 ? configuredModel.slice(slash + 1) : configuredModel;
  const model = fs.existsSync(path.join(agentDir, "models.json")) ? `aw-gateway/${modelId}` : `${provider}/${modelId}`;
  const args = rpcArgs(JSON.parse(process.env.GH_AW_PI_ARGS || '["--no-session","--no-approve"]'));
  const actionsDir = process.env.RUNNER_TEMP ? path.join(process.env.RUNNER_TEMP, "gh-aw/actions") : __dirname;
  for (const file of ["pi_provider.cjs", "pi_steering_extension.cjs", "pi_tool_policy.cjs"]) args.push("--extension", path.join(actionsDir, file));
  for (const builtin of ["mcp", "codemode", "tool-search"]) args.push("--extension", `builtin:${builtin}`);
  if (process.env.GH_AW_PI_SYSTEM_PROMPT) args.push("--append-system-prompt", process.env.GH_AW_PI_SYSTEM_PROMPT);
  const client = new sdk.RpcClient({ cliPath: options.cliPath || resolvePiPackageFile("dist/bundle/cli.js"), cwd, args, ...(configuredModel ? { model } : {}) });
  let settled = false;
  let terminalFailure = false;
  const unsubscribe = client.onEvent(event => {
    emit(event);
    if (event.type === "agent_settled") settled = true;
    if (event.type === "message_end" && event.message?.role === "assistant") terminalFailure = ["error", "aborted"].includes(event.message.stopReason);
    if (event.type === "extension_error") terminalFailure = true;
  });
  try {
    await client.start();
    const state = await client.getState();
    emit({ type: "session", version: 3, id: state.sessionId, cwd });
    const disposition = await client.prompt(fs.readFileSync(promptPath, "utf8"));
    if (disposition !== "handled" && !settled) {
      const minutes = Number(process.env.GH_AW_TIMEOUT_MINUTES || "30");
      if (!Number.isFinite(minutes) || minutes <= 0) throw new Error("GH_AW_TIMEOUT_MINUTES must be positive");
      await client.waitForIdle(minutes * 60_000);
    }
    if (terminalFailure) throw new Error("Pi RPC inference ended with an error; see the streaming log");
    if (config.session?.export) await client.exportHtml(path.join(agentDir, "session.html"));
  } finally {
    unsubscribe();
    await client.stop();
    const diagnostics = client.getStderr();
    if (diagnostics) process.stderr.write(diagnostics);
  }
}

if (require.main === module) {
  main().catch(error => {
    process.stderr.write(`[pi-rpc-driver] ${getErrorMessage(error)}\n`);
    process.exitCode = 1;
  });
}

module.exports = { main, rpcArgs };
