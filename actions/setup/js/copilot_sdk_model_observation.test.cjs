import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "module";
import { spawnSync } from "child_process";
import net from "net";
import fs from "fs";
import os from "os";
import path from "path";

const require = createRequire(import.meta.url);
const { COPILOT_MODEL_VERIFICATION_EXIT_CODE, createCopilotSDKModelObserver, readCopilotModelSubstitution, persistCopilotModelSelection } = require("./copilot_model_selection.cjs");
const { runWithCopilotSDK } = require("./copilot_sdk_session.cjs");
const { qualifyModelForMultiProvider } = require("./copilot_sdk_multi_provider.cjs");
const { detectErrors } = require("./agent_error_patterns.cjs");
const { getEffectiveModelLabel } = require("./model_attribution.cjs");

// Captured, credential-free model-bearing records from Smoke Copilot SDK Routed run 39529495361
// (native envelopes and main/sub-agent scope preserved) and the CLI control run 39529502581.
const fixtures = path.join(path.dirname(require.resolve("./copilot_sdk_session.cjs")), "fixtures");
const sdkDir = path.join(fixtures, "copilot-sdk-model-observation-39529495361");
const cliDir = path.join(fixtures, "copilot-cli-model-observation-39529502581");
const readJsonl = file =>
  fs
    .readFileSync(file, "utf8")
    .trim()
    .split("\n")
    .map(line => JSON.parse(line));
const sdkEvents = () => readJsonl(path.join(sdkDir, "events.jsonl"));
const providerConfig = () => JSON.parse(fs.readFileSync(path.join(sdkDir, "multi-provider.json"), "utf8"));
const routingSelection = JSON.parse(fs.readFileSync(path.join(sdkDir, "model-routing-selection.json"), "utf8"));
const SESSION_ID = "aee0ab8e-b091-4008-864d-4ca73e3f95c7";
const mainEvents = () => sdkEvents().filter(event => event.agentId === undefined);
const haikuEvents = () => sdkEvents().filter(event => event.agentId !== undefined);
const startEvent = () => sdkEvents().find(event => event.type === "session.start");
const shutdownEvent = () => sdkEvents().find(event => event.type === "session.shutdown");
// Derived test fixture (not a substitution observed in the supplied runs): the captured Haiku
// model change with its sub-agent scope removed, i.e. the main session switching model.
const derivedMainModelChange = () => {
  const { agentId: _agentId, ...event } = sdkEvents().find(candidate => candidate.type === "session.model_change");
  return event;
};

describe("Copilot SDK main-session model observation", () => {
  let root, selectionPath;
  beforeEach(() => {
    root = fs.mkdtempSync(path.join(os.tmpdir(), "sdk-model-observation-"));
    selectionPath = path.join(root, "sandbox/agent/logs/copilot-model-selection.json");
    vi.stubEnv("GH_AW_TMP_DIR", root);
    vi.stubEnv("HOME", root);
    vi.stubEnv("COPILOT_HOME", path.join(root, ".copilot"));
    vi.stubEnv("GH_AW_PHASE", "agent");
    vi.stubEnv("GH_AW_ENGINE_ID", "copilot");
    vi.stubEnv("GH_AW_SESSION_STATE_BASE_DIR", path.join(root, "driver-session-state"));
    vi.stubEnv("GH_AW_ENGINE_CWD", root);
    vi.stubEnv("GH_AW_MODEL_FALLBACK", "");
    vi.stubEnv("GH_AW_MODEL_ROUTING", "");
    vi.stubEnv("GH_AW_COPILOT_SDK_SERVER_ARGS", "[]");
    vi.stubEnv("GH_AW_COPILOT_SDK_MULTI_PROVIDER_JSON", JSON.stringify(providerConfig()));
    vi.spyOn(process.stderr, "write").mockImplementation(() => true);
  });
  afterEach(() => {
    vi.unstubAllEnvs();
    vi.restoreAllMocks();
    fs.rmSync(root, { recursive: true, force: true });
  });

  const selection = () => JSON.parse(fs.readFileSync(selectionPath, "utf8"));
  const observer = (configuredModel, expectedModel) => createCopilotSDKModelObserver({ configuredModel, expectedModel, providerConfig: providerConfig(), sessionDirs: [path.join(root, "native")] });

  /**
   * Run the SDK session runner against a fake SDK session that replays native events.
   * @param {{ replay: (handler: (event: any) => void) => any, onDisconnect?: (handler: (event: any) => void) => void }} options
   */
  async function runSession({ replay, onDisconnect = () => {} }) {
    let handler = () => {};
    const disconnect = vi.fn(async () => onDisconnect(handler));
    const createSession = vi.fn(async () => ({
      sessionId: SESSION_ID,
      on: callback => {
        handler = callback;
      },
      sendAndWait: async () => replay(handler),
      disconnect,
    }));
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
      workingDirectory: root,
      agentsBaseDir: root,
      sdkModule: { CopilotClient: FakeCopilotClient, RuntimeConnection: { forUri: () => ({}) }, approveAll: () => "allow" },
    });
    return { result, disconnect, createSession, logger };
  }
  const finalMessage = () => {
    const message = mainEvents()
      .filter(event => event.type === "assistant.message")
      .at(-1);
    return { ...message, data: { ...message.data, content: "Done." } };
  };

  it("sdk_main_model_matches_records_observed", async () => {
    vi.stubEnv("COPILOT_MODEL", "gpt-5.6-luna");
    vi.stubEnv("GH_AW_COPILOT_CONFIGURED_MODEL", "gpt-5.6-luna");
    const { result, createSession } = await runSession({
      replay: handler => {
        for (const event of mainEvents().filter(event => event.type !== "session.shutdown")) handler(event);
        return finalMessage();
      },
    });
    expect(createSession.mock.calls[0][0].model).toBe("copilot-responses/gpt-5.6-luna");
    expect(result.exitCode).toBe(0);
    expect(selection()).toEqual({ status: "observed", configured_model: "gpt-5.6-luna", resolved_model: "gpt-5.6-luna", effective_model: "gpt-5.6-luna" });
    expect(readCopilotModelSubstitution()).toBe("");
    // Same schema and outcome as the CLI control run's selection evidence.
    expect(Object.keys(selection()).sort()).toEqual(Object.keys(JSON.parse(fs.readFileSync(path.join(cliDir, "copilot-model-selection.json"), "utf8"))).sort());
  });

  it("sdk_provider_qualified_model_matches_bare_events", () => {
    const watch = observer("copilot-responses/gpt-5.6-luna", "copilot-responses/gpt-5.6-luna");
    watch.setSessionId(SESSION_ID);
    expect(startEvent().data.selectedModel).toBe("copilot-responses/gpt-5.6-luna");
    for (const event of mainEvents()) {
      expect(watch.observe(event)).toBe("");
      expect(selection().status).toBe("observed");
    }
    expect(shutdownEvent().data.currentModel).toBe("gpt-5.6-luna");
    expect(selection()).toMatchObject({ resolved_model: "gpt-5.6-luna", effective_model: "gpt-5.6-luna" });
  });

  it("sdk_same_id_provider_switch_after_match_is_substituted", () => {
    const watch = observer("gpt-5.6-luna", "copilot-responses/gpt-5.6-luna");
    watch.setSessionId(SESSION_ID);
    const [start, firstMessage] = mainEvents();
    expect(watch.observe(start)).toBe("");
    expect(watch.observe(firstMessage)).toBe("");
    expect(selection().status).toBe("observed");
    // Derived from the 39529495361 fixture: the main session switches to the same id on an unconfigured `copilot` provider.
    const diagnostic = watch.observe({ ...derivedMainModelChange(), data: { ...derivedMainModelChange().data, newModel: "copilot/gpt-5.6-luna" } });
    expect(diagnostic).toContain("Copilot substituted 'copilot/gpt-5.6-luna'");
    expect(selection()).toEqual({
      status: "substituted",
      configured_model: "gpt-5.6-luna",
      resolved_model: "gpt-5.6-luna",
      effective_model: "copilot/gpt-5.6-luna",
      observed_models: ["gpt-5.6-luna", "copilot/gpt-5.6-luna"],
    });
    expect(watch.observe(firstMessage)).toBe(diagnostic);
    expect(selection().status).toBe("substituted");
  });

  it.each([
    ["unknown prefix", "x/gpt-5.6-luna", "substituted"],
    ["unmapped github-copilot prefix", "github-copilot/gpt-5.6-luna", "substituted"],
    ["mapped provider row", "copilot-responses/gpt-5.6-luna", "observed"],
    ["matching bare id", "gpt-5.6-luna", "observed"],
  ])("sdk_qualified_ids_resolve_only_through_provider_mapping (%s)", (_name, model, status) => {
    const watch = observer("gpt-5.6-luna", "copilot-responses/gpt-5.6-luna");
    watch.setSessionId(SESSION_ID);
    // Derived from the 39529495361 fixture: the captured main assistant message reporting a different model id.
    const [, firstMessage] = mainEvents();
    const diagnostic = watch.observe({ ...firstMessage, data: { ...firstMessage.data, model } });
    expect(selection()).toEqual({ status, configured_model: "gpt-5.6-luna", resolved_model: "gpt-5.6-luna", effective_model: status === "observed" ? "gpt-5.6-luna" : model });
    expect(diagnostic).toEqual(status === "observed" ? "" : expect.stringContaining(`Copilot substituted '${model}'`));
  });

  it("sdk_fixture_main_session_is_observed", () => {
    const watch = observer("gpt-5.6-luna", "copilot-responses/gpt-5.6-luna");
    watch.setSessionId(SESSION_ID);
    for (const event of sdkEvents()) expect(watch.observe(event)).toBe("");
    expect(selection()).toEqual({ status: "observed", configured_model: "gpt-5.6-luna", resolved_model: "gpt-5.6-luna", effective_model: "gpt-5.6-luna" });
  });

  /**
   * Run copilot_harness.cjs in SDK driver mode with retries enabled. The stub driver runs the real
   * SDK session runner against a fake SDK client that replays `replay.json`, records each attempt
   * and exits with the runner's exit code, as copilot_sdk_driver.cjs does.
   * @param {{ events: any[], final: any }} replay `final: null` keeps sendAndWait pending.
   */
  async function runHarnessWithStubDriver(replay) {
    const freePort = async () => {
      const server = net.createServer();
      await new Promise(resolve => server.listen(0, "127.0.0.1", resolve));
      const { port } = server.address();
      await new Promise(resolve => server.close(resolve));
      return port;
    };
    const [providerPort, sdkPort] = [await freePort(), await freePort()];
    // awf-reflect from the routed run 39147795806 without its routing selection; the provider listener is local.
    const reflect = JSON.parse(fs.readFileSync(path.join(fixtures, "copilot-routed-run-39147795806", "awf-reflect.json"), "utf8"));
    delete reflect.routing;
    Object.assign(reflect.endpoints[0], { base_url: `http://127.0.0.1:${providerPort}`, models_url: `http://127.0.0.1:${providerPort}/models` });
    const reflectPath = path.join(root, "reflect.json");
    fs.writeFileSync(reflectPath, JSON.stringify(reflect));
    const preloadPath = path.join(root, "reflect-preload.cjs");
    fs.writeFileSync(
      preloadPath,
      `const body = require("fs").readFileSync(${JSON.stringify(reflectPath)}, "utf8");
require(${JSON.stringify(require.resolve("./awf_reflect.cjs"))}).fetchAWFReflect = async () => ({ ok: true, reflectData: JSON.parse(body) });
if (process.argv[1].endsWith("copilot_harness.cjs")) require("net").createServer(socket => socket.end()).listen(${providerPort}, "127.0.0.1").unref();`
    );
    const sidecarPath = path.join(root, "copilot-sidecar.cjs");
    fs.writeFileSync(sidecarPath, `#!/usr/bin/env node\nconst args = process.argv.slice(2);\nrequire("net").createServer(socket => socket.end()).listen(Number(args[args.indexOf("--port") + 1]), "127.0.0.1");\n`, { mode: 0o755 });
    const replayPath = path.join(root, "replay.json");
    fs.writeFileSync(replayPath, JSON.stringify(replay));
    const attemptsPath = path.join(root, "attempts");
    const driverPath = path.join(root, "copilot-sdk-driver-stub.cjs");
    fs.writeFileSync(
      driverPath,
      `const fs = require("fs");
const { runWithCopilotSDK } = require(${JSON.stringify(require.resolve("./copilot_sdk_session.cjs"))});
const { events, final } = JSON.parse(fs.readFileSync(${JSON.stringify(replayPath)}, "utf8"));
fs.appendFileSync(${JSON.stringify(attemptsPath)}, "attempt\\n");
let handler = () => {};
class CopilotClient {
  start = async () => {};
  stop = async () => {};
  createSession = async () => ({
    sessionId: ${JSON.stringify(SESSION_ID)},
    on: callback => { handler = callback; },
    sendAndWait: async () => { for (const event of events) handler(event); return final ?? new Promise(() => {}); },
    disconnect: async () => {},
  });
}
runWithCopilotSDK({
  sdkUri: process.env.COPILOT_SDK_URI,
  prompt: "test",
  logger: message => process.stderr.write(message + "\\n"),
  workingDirectory: ${JSON.stringify(root)},
  agentsBaseDir: ${JSON.stringify(root)},
  sdkModule: { CopilotClient, RuntimeConnection: { forUri: () => ({}) }, approveAll: () => "allow" },
}).then(result => process.exit(result.exitCode));`
    );
    const env = { ...process.env, AWF_REFLECT_ENABLED: "1", COPILOT_MODEL: "gpt-5.6-luna", COPILOT_SDK_URI: `http://127.0.0.1:${sdkPort}`, GH_AW_AWF_CONFIG_PATH: path.join(root, "awf-config.json") };
    Object.assign(env, { GH_AW_HARNESS_MAX_RETRIES: "3", GH_AW_HARNESS_INITIAL_DELAY_MS: "1", GH_AW_HARNESS_MAX_DELAY_MS: "1", GH_AW_HARNESS_STALL_WARNING_MS: "0", NODE_OPTIONS: `--require ${preloadPath}` });
    fs.writeFileSync(env.GH_AW_AWF_CONFIG_PATH, JSON.stringify({ network: { allowDomains: [] } }));
    for (const key of [
      "GH_AW_MODEL_ROUTING",
      "GH_AW_MODEL_FALLBACK",
      "GH_AW_SKIP_REFLECT",
      "GH_AW_SAFE_OUTPUTS",
      "COPILOT_PROVIDER_WIRE_API",
      "GH_AW_MODEL_ALIASES",
      "GH_AW_COPILOT_CONFIGURED_MODEL",
      "GH_AW_COPILOT_SDK_MULTI_PROVIDER_JSON",
    ])
      delete env[key];
    const result = spawnSync(process.execPath, ["copilot_harness.cjs", process.execPath, driverPath, sidecarPath], {
      cwd: path.dirname(require.resolve("./copilot_harness.cjs")),
      env,
      encoding: "utf8",
      timeout: 30000,
    });
    const attempts = fs.existsSync(attemptsPath) ? fs.readFileSync(attemptsPath, "utf8").trim().split("\n").length : 0;
    return { result, attempts };
  }

  it("sdk_main_model_switch_is_substituted_and_stops", async () => {
    const [start, firstMessage] = mainEvents();
    // A later matching main-session record cannot clear the mismatch; sendAndWait never resolves.
    const { result, attempts } = await runHarnessWithStubDriver({ events: [start, firstMessage, derivedMainModelChange(), firstMessage], final: null });

    expect(attempts, result.stderr).toBe(1);
    expect(result.status, result.stderr).not.toBe(0);
    expect(result.stderr).toContain("stopping SDK session: Copilot model verification failed");
    expect(result.stderr).toContain("Copilot substituted 'copilot-completions/claude-haiku-4.5'. Refusing silent model substitution.");
    expect(result.stderr).not.toContain("retry 1/3");
    expect(selection()).toEqual({
      status: "substituted",
      configured_model: "gpt-5.6-luna",
      resolved_model: "gpt-5.6-luna",
      effective_model: "copilot-completions/claude-haiku-4.5",
      observed_models: ["gpt-5.6-luna", "copilot-completions/claude-haiku-4.5"],
    });
    const diagnostic = readCopilotModelSubstitution();
    expect(result.stderr).toContain(diagnostic);
    expect(detectErrors(diagnostic).modelNotSupportedError).toBe(true);
    const info = path.join(root, "aw_info.json");
    fs.writeFileSync(info, "{}");
    persistCopilotModelSelection();
    expect(JSON.parse(fs.readFileSync(info, "utf8"))).toMatchObject({ model: "copilot-completions/claude-haiku-4.5", model_selection: { status: "substituted" } });
  });

  it("sdk_model_verification_failure_is_not_retried", async () => {
    // An unreadable native events.jsonl makes the post-disconnect replay fail without recording a substitution.
    fs.mkdirSync(path.join(root, ".copilot", "session-state", SESSION_ID, "events.jsonl"), { recursive: true });
    const { result, attempts } = await runHarnessWithStubDriver({ events: mainEvents().filter(event => event.type !== "session.shutdown"), final: finalMessage() });

    expect(attempts, result.stderr).toBe(1);
    expect(result.status, result.stderr).not.toBe(0);
    expect(result.stderr).toContain("Copilot model verification failed:");
    expect(result.stderr).not.toContain("retry 1/3");
    expect(selection().status).toBe("observed");
    expect(readCopilotModelSubstitution()).toBe("");
  });

  it.each([
    ["matching", "gpt-5.6-luna", "observed", 0],
    // Derived test fixture: the main session served a different model than the router selected.
    ["derived different", "gpt-5.6-sol", "substituted", COPILOT_MODEL_VERIFICATION_EXIT_CODE],
  ])("sdk_routed_model_is_checked_against_selection_wire_model (%s main model)", async (_name, mainModel, status, exitCode) => {
    expect(routingSelection).toMatchObject({ stage: "selection", wire_model: "gpt-5.6-luna" });
    const routedModel = qualifyModelForMultiProvider(routingSelection.wire_model, providerConfig());
    vi.stubEnv("GH_AW_MODEL_ROUTING", "1");
    vi.stubEnv("COPILOT_MODEL", routedModel);
    vi.stubEnv("GH_AW_COPILOT_CONFIGURED_MODEL", "auto");
    const { result } = await runSession({
      replay: handler => {
        handler(startEvent());
        for (const event of mainEvents().filter(event => event.type === "assistant.message")) handler({ ...event, data: { ...event.data, model: mainModel } });
        return { ...finalMessage(), data: { ...finalMessage().data, model: mainModel } };
      },
    });
    expect(result.exitCode).toBe(exitCode);
    expect(selection()).toEqual({
      status,
      configured_model: "auto",
      resolved_model: "gpt-5.6-luna",
      effective_model: mainModel,
      ...(status === "substituted" ? { observed_models: ["gpt-5.6-luna", "gpt-5.6-sol"] } : {}),
    });
  });

  it("sdk_auto_does_not_report_substitution", () => {
    const watch = createCopilotSDKModelObserver({ configuredModel: "auto", expectedModel: "auto", providerConfig: null, sessionDirs: [] });
    for (const event of mainEvents()) expect(watch.observe(event)).toBe("");
    expect(watch.finalize(SESSION_ID)).toBe("");
    expect(selection()).toEqual({
      status: "observed",
      configured_model: "auto",
      resolved_model: "auto",
      effective_model: "gpt-5.6-luna",
      observed_models: ["copilot-responses/gpt-5.6-luna", "gpt-5.6-luna"],
    });
    expect(readCopilotModelSubstitution()).toBe("");
  });

  it("preserves multiple native models for auto attribution without firewall usage", () => {
    const infoPath = path.join(root, "aw_info.json");
    const watch = createCopilotSDKModelObserver({ configuredModel: "auto", expectedModel: "auto", providerConfig: null, sessionDirs: [] });
    expect(watch.observe({ type: "assistant.message", data: { model: "gemini-3.8-flash" } })).toBe("");
    expect(watch.observe({ type: "assistant.message", data: { model: "claude-sonnet-5" } })).toBe("");
    expect(watch.finalize(SESSION_ID)).toBe("");
    expect(selection()).toEqual({
      status: "observed",
      configured_model: "auto",
      resolved_model: "auto",
      effective_model: "claude-sonnet-5",
      observed_models: ["gemini-3.8-flash", "claude-sonnet-5"],
    });
    persistCopilotModelSelection();
    expect(getEffectiveModelLabel(infoPath, "agent", process.env)).toBe("auto (Gemini 3.8 Flash, Claude Sonnet 5)");
  });

  it("sdk_subagent_model_events_do_not_change_main_observation", () => {
    expect(haikuEvents().map(event => [event.type, event.data.newModel || event.data.model])).toEqual([
      ["session.model_change", "copilot-completions/claude-haiku-4.5"],
      ["assistant.message", "claude-haiku-4.5"],
    ]);
    const native = path.join(root, "native", SESSION_ID, "events.jsonl");
    fs.mkdirSync(path.dirname(native), { recursive: true });
    fs.writeFileSync(
      native,
      sdkEvents()
        .map(event => JSON.stringify(event))
        .join("\n") + "\n"
    );
    const watch = observer("auto", "copilot-responses/gpt-5.6-luna");
    for (const event of haikuEvents()) expect(watch.observe(event)).toBe("");
    expect(fs.existsSync(selectionPath)).toBe(false);
    expect(watch.finalize(SESSION_ID)).toBe("");
    expect(selection()).toEqual({ status: "observed", configured_model: "auto", resolved_model: "gpt-5.6-luna", effective_model: "gpt-5.6-luna" });
  });

  it.each(["listener", "native session file"])("sdk_shutdown_mismatch_cannot_be_reported_as_success (%s)", async delivery => {
    vi.stubEnv("COPILOT_MODEL", "gpt-5.6-luna");
    vi.stubEnv("GH_AW_COPILOT_CONFIGURED_MODEL", "gpt-5.6-luna");
    // Derived test fixture: the captured shutdown envelope reporting a different current model.
    const shutdown = { ...shutdownEvent(), data: { ...shutdownEvent().data, currentModel: "gpt-5.6-sol" } };
    const { result, logger } = await runSession({
      replay: handler => {
        for (const event of mainEvents().filter(event => event.type !== "session.shutdown")) handler(event);
        return finalMessage();
      },
      onDisconnect: handler => {
        if (delivery === "listener") return handler(shutdown);
        const native = path.join(root, "sandbox/agent/session-state", SESSION_ID, "events.jsonl");
        fs.mkdirSync(path.dirname(native), { recursive: true });
        fs.writeFileSync(native, [...mainEvents().slice(0, -1), shutdown].map(event => JSON.stringify(event)).join("\n") + "\n");
      },
    });
    expect(result.exitCode).toBe(COPILOT_MODEL_VERIFICATION_EXIT_CODE);
    expect(result.output).toContain("Copilot substituted 'gpt-5.6-sol'");
    expect(selection()).toMatchObject({ status: "substituted", effective_model: "gpt-5.6-sol" });
    expect(readCopilotModelSubstitution()).toContain("Refusing silent model substitution");
    expect(logger.mock.calls.flat().some(message => message.includes("Refusing silent model substitution"))).toBe(true);
  });
});
