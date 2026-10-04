// @ts-check
"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { execFileSync } = require("node:child_process");
const { loadPiSDK, nativePiProvider, preparePiRuntime, DEFAULT_SESSION_DIR } = require("./pi_runtime.cjs");
const { sanitizeProviderErrorMessage } = require("./pi_provider.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");

/** @param {any} event @returns {Record<string, any>} */
function jsonEvent(event) {
  if (event.type !== "message_update") {
    const sanitized = { ...event };
    if (typeof event.errorMessage === "string") sanitized.errorMessage = sanitizeProviderErrorMessage(event.errorMessage);
    if (typeof event.message?.errorMessage === "string") {
      sanitized.message = { ...event.message, errorMessage: sanitizeProviderErrorMessage(event.message.errorMessage) };
    }
    return sanitized;
  }
  const { partial, ...update } = event.assistantMessageEvent;
  if (update.type === "toolcall_start") {
    const call = partial?.content?.[update.contentIndex];
    update.id = call?.id;
    update.toolName = call?.name;
  }
  if (typeof update.errorMessage === "string") update.errorMessage = sanitizeProviderErrorMessage(update.errorMessage);
  return { type: event.type, usage: event.usage ?? event.message?.usage, assistantMessageEvent: update };
}

/** @param {{ sdk?: any, emit?: (event: any) => void }} [options] */
async function main(options = {}) {
  const sdk = options.sdk || (await loadPiSDK());
  const emit = options.emit || (event => process.stdout.write(JSON.stringify(event) + "\n"));
  const { agentDir, settings, config } = preparePiRuntime();
  const cwd = process.env.GH_AW_ENGINE_CWD || process.env.GITHUB_WORKSPACE || process.cwd();
  const bare = process.env.GH_AW_PI_BARE === "true";
  const promptPath = process.env.GH_AW_PI_USER_PROMPT || process.env.GH_AW_PROMPT;
  if (!promptPath) throw new Error("GH_AW_PROMPT is required");
  const prompt = fs.readFileSync(promptPath, "utf8");
  const systemPath = process.env.GH_AW_PI_SYSTEM_PROMPT;
  const settingsManager = sdk.SettingsManager.create(cwd, agentDir, { projectTrusted: false });
  settingsManager.applyOverrides(settings);
  const modelRuntime = await sdk.ModelRuntime.create({ authPath: path.join(agentDir, "auth.json"), modelsPath: path.join(agentDir, "models.json") });
  const modelString = process.env.GH_AW_PI_MODEL || process.env.PI_MODEL || "";
  const slash = modelString.indexOf("/");
  const provider = process.env.GH_AW_PI_NATIVE_PROVIDER || nativePiProvider(slash >= 0 ? modelString.slice(0, slash) : "copilot");
  const modelId = slash >= 0 ? modelString.slice(slash + 1) : modelString;
  const model = modelString ? modelRuntime.getModel(fs.existsSync(path.join(agentDir, "models.json")) ? "aw-gateway" : provider, modelId) : undefined;
  if (modelString && !model) settingsManager.applyOverrides({ defaultProvider: provider, defaultModel: modelId });

  const actionsDir = process.env.RUNNER_TEMP ? path.join(process.env.RUNNER_TEMP, "gh-aw/actions") : __dirname;
  const resourceLoader = new sdk.DefaultResourceLoader({
    cwd,
    agentDir,
    settingsManager,
    noContextFiles: bare,
    noSkills: bare,
    noPromptTemplates: bare,
    noExtensions: bare,
    noThemes: true,
    appendSystemPrompt: systemPath ? [fs.readFileSync(systemPath, "utf8")] : undefined,
    additionalExtensionPaths: ["pi_provider.cjs", "pi_steering_extension.cjs", "pi_tool_policy.cjs"].map(file => path.join(actionsDir, file)),
    extensionFactories: [sdk.createCodemodeExtension({ mode: settings.codemode?.mode || "on" }), sdk.createToolSearchExtension(), sdk.createMcpExtension()],
  });
  await resourceLoader.reload();
  const sessionOptions = config.session || {};
  const sessionManager = !sessionOptions.enabled
    ? sdk.SessionManager.inMemory(cwd)
    : sessionOptions.resume
      ? sdk.SessionManager.open(sessionOptions.resume, DEFAULT_SESSION_DIR, cwd)
      : sessionOptions.fork
        ? sdk.SessionManager.forkFrom(sessionOptions.fork, cwd, DEFAULT_SESSION_DIR, sessionOptions.id ? { id: sessionOptions.id } : undefined)
        : sdk.SessionManager.create(cwd, DEFAULT_SESSION_DIR, sessionOptions.id ? { id: sessionOptions.id } : undefined);
  const { session, modelFallbackMessage } = await sdk.createAgentSession({
    cwd,
    agentDir,
    modelRuntime,
    model,
    settingsManager,
    resourceLoader,
    sessionManager,
  });
  try {
    if (modelFallbackMessage) throw new Error(modelFallbackMessage);
    if (modelString && !model && (session.model?.provider !== provider || session.model?.id !== modelId)) throw new Error(`Pi model is not available: ${modelString}`);
    emit({ type: "session", version: 3, id: session.sessionId, cwd });
    let terminalFailure = false;
    session.subscribe(event => {
      emit(jsonEvent(event));
      if (event.type === "message_end" && event.message?.role === "assistant") terminalFailure = ["error", "aborted"].includes(event.message.stopReason);
    });
    await session.bindExtensions({});
    await session.prompt(prompt);
    if (terminalFailure) throw new Error("Pi SDK inference ended with an error; see the streaming log");
    if (sessionOptions.export && session.sessionFile) {
      execFileSync(process.env.GH_AW_PI_COMMAND || "pi", ["--export", session.sessionFile, path.join(agentDir, "session.html")], { timeout: 60_000, stdio: ["ignore", "ignore", "inherit"] });
    }
  } finally {
    session.dispose();
  }
}

if (require.main === module) {
  main().catch(error => {
    process.stderr.write(`[pi-sdk-driver] ${getErrorMessage(error)}\n`);
    process.exitCode = 1;
  });
}

module.exports = { main, jsonEvent };
