#!/usr/bin/env node
// @ts-check
"use strict";

/**
 * Self-contained Pi coding-agent SDK driver example.
 *
 * Declare @earendil-works/pi-coding-agent@1.0.0 as a project dependency and
 * install it in setup-steps before the agent runs. Select this file with
 * engine.driver: .github/drivers/pi_agent_core_driver_sample_node.cjs.
 *
 * Customize auditExtension, the resource loader, or the finalized-event
 * subscriber below. Use the built-in pi_agent_core_driver.cjs instead when
 * you need gh-aw's standard tool-budget and permission enforcement.
 */
const fs = require("node:fs");
const path = require("node:path");

/** @param {any} pi */
function auditExtension(pi) {
  pi.on("tool_call", event => {
    process.stderr.write(`[sample-pi-driver] dispatch ${event.toolName}\n`);
  });
}

async function main() {
  const packageName = "@earendil-works/pi-coding-agent";
  const sdk = await import(packageName);
  const cwd = process.env.GH_AW_ENGINE_CWD || process.env.GITHUB_WORKSPACE || process.cwd();
  const agentDir = process.env.PI_CODING_AGENT_DIR;
  const promptPath = process.env.GH_AW_PI_USER_PROMPT || process.env.GH_AW_PROMPT;
  if (!agentDir || !promptPath) throw new Error("This driver requires gh-aw's managed agent directory and prompt environment");
  const modelString = process.env.GH_AW_PI_MODEL || "";
  const slash = modelString.indexOf("/");
  const native = process.env.GH_AW_PI_NATIVE_PROVIDER || (slash < 0 ? "github-copilot" : modelString.slice(0, slash));
  const modelId = slash < 0 ? modelString : modelString.slice(slash + 1);
  const modelsPath = path.join(agentDir, "models.json");
  const runtime = await sdk.ModelRuntime.create({ modelsPath, authPath: path.join(agentDir, "auth.json") });
  const provider = fs.existsSync(modelsPath) ? "aw-gateway" : native;
  const model = runtime.getModel(provider, modelId);
  if (!model) throw new Error(`The requested Pi model is unavailable: ${provider}/${modelId}`);
  const settingsManager = sdk.SettingsManager.create(cwd, agentDir, { projectTrusted: false });
  settingsManager.applyOverrides({ defaultTools: ["+codemode", "+tool_search"], defaultProjectTrust: "never" });
  const loader = new sdk.DefaultResourceLoader({
    cwd,
    agentDir,
    settingsManager,
    noContextFiles: process.env.GH_AW_PI_BARE === "true",
    appendSystemPrompt: process.env.GH_AW_PI_SYSTEM_PROMPT ? [fs.readFileSync(process.env.GH_AW_PI_SYSTEM_PROMPT, "utf8")] : undefined,
    extensionFactories: [auditExtension, sdk.createCodemodeExtension({ mode: "on" }), sdk.createToolSearchExtension(), sdk.createMcpExtension()],
  });
  await loader.reload();
  const { session } = await sdk.createAgentSession({
    cwd,
    agentDir,
    model,
    modelRuntime: runtime,
    settingsManager,
    resourceLoader: loader,
    sessionManager: sdk.SessionManager.inMemory(cwd),
  });
  let failed = false;
  try {
    process.stdout.write(JSON.stringify({ type: "session", version: 3, id: session.sessionId, cwd }) + "\n");
    session.subscribe(event => {
      // Finalized envelopes are sufficient for gh-aw's conversation and usage parser.
      if (event.type !== "message_update") process.stdout.write(JSON.stringify(event) + "\n");
      if (event.type === "message_end" && event.message?.role === "assistant") failed = ["error", "aborted"].includes(event.message.stopReason);
    });
    await session.bindExtensions({});
    await session.prompt(fs.readFileSync(promptPath, "utf8"));
    if (failed) throw new Error("Pi inference failed; inspect the finalized assistant event");
  } finally {
    session.dispose();
  }
}

main().catch(error => {
  process.stderr.write(`[sample-pi-driver] ${error instanceof Error ? error.message : String(error)}\n`);
  process.exitCode = 1;
});
