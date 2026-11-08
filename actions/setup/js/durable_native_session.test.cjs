import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { createRequire } from "node:module";
import fs from "node:fs";
import path from "node:path";
import { spawnSync } from "node:child_process";

const require = createRequire(import.meta.url);
const { openDurableSession, observeDurableSession, openPiDurableSession } = require("./durable_native_session.cjs");
const { stageFiles, restoreFiles, validateFiles } = require("./durable_files.cjs");
const { captureCopilotSessions } = require("./durable_session.cjs");
const { createCopilotSDKModelObserver } = require("./copilot_model_selection.cjs");
const { runWithCopilotSDK } = require("./copilot_sdk_session.cjs");
const { TMP_GH_AW_PATH } = require("./constants.cjs");
const core = { info() {}, setOutput() {} };
const id = "00000000-0000-4000-8000-000000000001";
let directory;
beforeEach(() => {
  fs.mkdirSync(TMP_GH_AW_PATH, { recursive: true });
  directory = fs.mkdtempSync(path.join(TMP_GH_AW_PATH, "durable-native-test-"));
});
afterEach(() => {
  vi.unstubAllEnvs();
  vi.restoreAllMocks();
  fs.rmSync(directory, { recursive: true, force: true });
});

function environment(engine) {
  const state = path.join(directory, "agent/durable-sessions", engine);
  return {
    ...process.env,
    HOME: path.join(directory, "home"),
    COPILOT_HOME: path.join(directory, "home/.copilot"),
    COPILOT_MODEL: "",
    GH_AW_MODEL_FALLBACK: "",
    GH_AW_MODEL_ROUTING: "",
    ANTHROPIC_MODEL: "",
    GH_AW_LLM_PROVIDER: "",
    GH_AW_LLM_PROVIDER_EXPLICIT: "0",
    CODEX_API_KEY: "fake-key",
    CODEX_HOME: state,
    CLAUDE_CONFIG_DIR: state,
    RUNNER_TEMP: directory,
    GH_AW_TMP_DIR: directory,
    GH_AW_AWF_CONFIG_PATH: path.join(directory, "missing-config.json"),
    GH_AW_SAFE_OUTPUTS: "",
    GH_AW_CODEX_CONTEXT_REBUILD_CIRCUIT_BREAKER: "false",
    GH_AW_AGENT_OUTPUT: path.join(directory, "agent-output.json"),
    GH_AW_HARNESS_MAX_RETRIES: "0",
    GH_AW_SKIP_REFLECT: "true",
    AWF_REFLECT_ENABLED: "0",
    GH_AW_DURABLE_STORAGE: "artifact",
    GH_AW_DURABLE_ENGINE: engine,
    GH_AW_DURABLE_SESSION_DIR: state,
    GH_AW_DURABLE_RESUMED: "false",
  };
}

function artifactRoundtrip(env, files) {
  const root = path.join(directory, "agent");
  vi.stubEnv("GH_AW_DURABLE_ENGINE", env.GH_AW_DURABLE_ENGINE);
  vi.stubEnv("GH_AW_DURABLE_FILES", JSON.stringify(files));
  stageFiles({ core, root });
  validateFiles({ core, root });
  fs.rmSync(env.GH_AW_DURABLE_SESSION_DIR, { recursive: true });
  fs.rmSync(env.HOME, { recursive: true, force: true });
  restoreFiles({ core, root });
  env.GH_AW_DURABLE_RESUMED = "true";
}

describe("native CLI artifact continuation", () => {
  it("chains Pi's exact native transcript and submits each run's current prompt", () => {
    const env = {
      ...environment("pi"),
      GH_AW_PI_CONFIG: "{}",
      PI_CODING_AGENT_DIR: path.join(directory, "pi-agent"),
      GH_AW_PI_STAGING_DIR: path.join(directory, "missing-staging"),
      GH_AW_PI_SUBAGENT_CHILD: "",
    };
    const file = path.join(env.GH_AW_DURABLE_SESSION_DIR, "session.jsonl");
    const cli = path.join(directory, "pi-cli.cjs");
    fs.writeFileSync(
      cli,
      `#!${process.execPath}
const fs = require("node:fs");
const args = process.argv.slice(2);
if (args.includes("--version")) { console.log("1.3.0"); process.exit(0); }
const file = args[args.indexOf("--session") + 1];
if (!file || args.includes("--no-session")) throw new Error("Native session persistence was not selected");
if (!fs.existsSync(file)) fs.writeFileSync(file, JSON.stringify({ type: "session", version: 3, id: ${JSON.stringify(id)} }) + "\\n");
fs.appendFileSync(file, JSON.stringify({ type: "message", message: { role: "user", content: args.at(-1) } }) + "\\n");
console.log(JSON.stringify({ type: "session", version: 3, id: ${JSON.stringify(id)} }));
console.log(JSON.stringify({ type: "message_end", message: { role: "assistant", content: args.at(-1) } }));
`
    );
    fs.chmodSync(cli, 0o700);
    env.GH_AW_PI_COMMAND = cli;
    const runtime = require.resolve("./pi_runtime.cjs");
    const harness = require.resolve("./shell_harness.cjs");
    function run(prompt) {
      const command = `${JSON.stringify(process.execPath)} ${JSON.stringify(runtime)} && ${JSON.stringify(process.execPath)} ${JSON.stringify(cli)} --session-dir ${JSON.stringify(env.GH_AW_DURABLE_SESSION_DIR)} --session ${JSON.stringify(file)} ${JSON.stringify(prompt)}`;
      const result = spawnSync(process.execPath, [harness, "pi", command], { env, encoding: "utf8", timeout: 15000 });
      expect(result.status, result.stderr).toBe(0);
      return result.stdout;
    }
    run("First task A");
    expect(openPiDurableSession(env).id).toBe(id);
    artifactRoundtrip(env, [file]);
    const output = run("Current task B");
    expect(output).toContain("Current task B");
    expect(output).not.toContain("First task A");
    const records = fs
      .readFileSync(file, "utf8")
      .trim()
      .split("\n")
      .map(line => JSON.parse(line));
    expect(records[0].id).toBe(id);
    expect(records.slice(1).map(record => record.message.content)).toEqual(["First task A", "Current task B"]);
  });

  it.each(["claude", "codex", "copilot"])("starts %s fresh, then restores its exact session and sends new workflow instructions", engine => {
    const env = environment(engine);
    const state = env.GH_AW_DURABLE_SESSION_DIR;
    const nativeDirectory = engine === "copilot" ? path.join(env.COPILOT_HOME, "session-state") : path.join(state, engine === "claude" ? "projects" : "sessions");
    const cli = path.join(directory, "native-cli.cjs");
    fs.writeFileSync(
      cli,
      `#!${process.execPath}
const fs = require("node:fs"), path = require("node:path");
const args = process.argv.slice(2);
const engine = process.env.GH_AW_DURABLE_ENGINE;
const marker = engine === "copilot" ? "--session-id" : engine === "claude" ? "--resume" : "resume";
const selected = args.includes(marker) ? args[args.indexOf(marker)+1] : ${JSON.stringify(id)};
if (process.env.GH_AW_DURABLE_RESUMED === "true" && (!args.includes(marker) || !fs.existsSync(path.join(${JSON.stringify(nativeDirectory)}, selected, "events.jsonl")))) throw new Error("Native continuation was not used");
if (process.env.GH_AW_DURABLE_RESUMED !== "true" && engine !== "copilot" && args.includes(marker)) throw new Error("First run cannot resume");
fs.mkdirSync(path.join(${JSON.stringify(nativeDirectory)}, selected), { recursive: true });
fs.appendFileSync(path.join(${JSON.stringify(nativeDirectory)}, selected, "events.jsonl"), JSON.stringify({type:"assistant.message",data:{content:"completed task"}})+"\\n");
let input = "";
process.stdin.setEncoding("utf8");
process.stdin.on("data", value => input += value);
process.stdin.on("end", () => {
  fs.writeFileSync(${JSON.stringify(path.join(directory, "call.json"))}, JSON.stringify({ args, input }));
  console.log(JSON.stringify(engine === "claude" ? { type: "system", subtype: "init", session_id: selected } : engine === "codex" ? { type: "thread.started", thread_id: selected } : { type: "assistant.message", data: { content: "done" } }));
});
`
    );
    fs.chmodSync(cli, 0o700);
    const prompt = path.join(directory, "prompt.txt");
    const harness = require.resolve(`./${engine}_harness.cjs`);
    const args = engine === "claude" ? ["--print", "--prompt-file", prompt] : engine === "codex" ? ["exec", "--prompt-file", prompt] : ["--prompt", "Continue current task B"];
    function run(text) {
      fs.writeFileSync(prompt, text);
      const result = spawnSync(process.execPath, [harness, cli, ...args], { env, encoding: "utf8", timeout: 15000 });
      expect(result.status, result.stderr).toBe(0);
      return JSON.parse(fs.readFileSync(path.join(directory, "call.json"), "utf8"));
    }
    run("First task A");
    const selector = JSON.parse(fs.readFileSync(path.join(state, "session.json"), "utf8"));
    expect(selector).toMatchObject({ version: 1, engine });
    if (engine === "copilot") captureCopilotSessions({ env, home: env.HOME });
    artifactRoundtrip(env, [path.join(state, "session.json"), engine === "copilot" ? path.join(state, "session-state") : nativeDirectory]);
    const second = run("Continue current task B");
    const marker = engine === "copilot" ? "--session-id" : engine === "claude" ? "--resume" : "resume";
    expect(second.args[second.args.indexOf(marker) + 1]).toBe(selector.id);
    expect(second.input || second.args.join(" ")).toContain("Continue current task B");
    if (engine === "copilot") {
      const baseline = JSON.parse(fs.readFileSync(path.join(state, "capture-offset.json"), "utf8"));
      expect(baseline.bytes).toBeGreaterThan(0);
      const reopened = openDurableSession(engine, env);
      reopened.copilotSession(selector.id);
      expect(JSON.parse(fs.readFileSync(path.join(state, "capture-offset.json"), "utf8"))).toEqual(baseline);
    }
  });
});

it("refuses missing, corrupt, incompatible or redirected Pi transcripts and leaves sub-agents ephemeral", () => {
  const env = environment("pi");
  const session = openPiDurableSession(env);
  expect(session.id).toBeUndefined();
  env.GH_AW_DURABLE_RESUMED = "true";
  expect(() => openPiDurableSession(env)).toThrow("transcript is missing");
  for (const content of ["{", '{"type":"session","version":3,"id":"../../other"}\n', JSON.stringify({ type: "session", version: 2, id }), JSON.stringify({ type: "session", version: 3, id }) + "\n{"]) {
    fs.writeFileSync(session.file, content);
    expect(() => openPiDurableSession(env)).toThrow();
  }
  fs.writeFileSync(session.file, JSON.stringify({ type: "session", version: 3, id }) + "\n");
  expect(openPiDurableSession(env).id).toBe(id);
  fs.unlinkSync(session.file);
  fs.symlinkSync(path.join(directory, "outside"), session.file);
  expect(() => openPiDurableSession(env)).toThrow("regular file");
  expect(openPiDurableSession({ ...env, GH_AW_PI_SUBAGENT_CHILD: "1" })).toBeUndefined();
});

it.each(["claude", "codex", "copilot"])("allows a first %s run without history but refuses incomplete or corrupt restored selectors", engine => {
  const env = environment(engine);
  expect(openDurableSession(engine, env).id).toBe("");
  env.GH_AW_DURABLE_RESUMED = "true";
  expect(() => openDurableSession(engine, env)).toThrow("no exact session ID");
  const selector = path.join(env.GH_AW_DURABLE_SESSION_DIR, "session.json");
  for (const value of ["{", JSON.stringify({ version: 1, engine, id: "../../other" }), JSON.stringify({ version: 1, engine: "other", id })]) {
    fs.writeFileSync(selector, value);
    expect(() => openDurableSession(engine, env)).toThrow();
  }
});

it("records streaming native IDs atomically and refuses a switch to an unrelated conversation", () => {
  const session = openDurableSession("claude", environment("claude"));
  observeDurableSession(session, "claude", "ordinary diagnostic output");
  observeDurableSession(session, "claude", JSON.stringify({ type: "system", subtype: "init", session_id: id }));
  expect(session.id).toBe(id);
  expect(() => session.save("00000000-0000-4000-8000-000000000002")).toThrow("unrelated");
});

it("uses native SDK createSession on first run and resumeSession with rebound policy after artifact restore", async () => {
  const env = environment("copilot");
  for (const [key, value] of Object.entries(env)) vi.stubEnv(key, value);
  vi.stubEnv("GH_AW_SESSION_STATE_BASE_DIR", path.join(directory, "sdk-events"));
  vi.stubEnv("GH_AW_COPILOT_CONFIGURED_MODEL", "new-model");
  vi.stubEnv("GH_AW_COPILOT_SDK_MULTI_PROVIDER_JSON", "");
  const create = vi.fn();
  const resume = vi.fn();
  let handler;
  function fakeSession(sessionId) {
    const native = path.join(env.COPILOT_HOME, "session-state", sessionId, "events.jsonl");
    return {
      sessionId,
      on: callback => {
        handler = callback;
      },
      sendAndWait: async () => {
        const event = { type: "assistant.message", data: { content: "done", model: "new-model" } };
        fs.mkdirSync(path.dirname(native), { recursive: true });
        fs.appendFileSync(native, JSON.stringify(event) + "\n");
        handler(event);
        return event;
      },
      disconnect: async () => {},
    };
  }
  class Client {
    start = async () => {};
    stop = async () => {};
    createSession = async config => {
      create(config);
      return fakeSession(config.sessionId);
    };
    resumeSession = async (sessionId, config) => {
      resume(sessionId, config);
      expect(fs.readFileSync(path.join(env.COPILOT_HOME, "session-state", sessionId, "events.jsonl"), "utf8")).toContain("old-model");
      return fakeSession(sessionId);
    };
  }
  const options = {
    sdkUri: "http://127.0.0.1:3002",
    prompt: "Current workflow task",
    model: "new-model",
    workingDirectory: directory,
    agentsBaseDir: directory,
    logger() {},
    sdkModule: { CopilotClient: Client, RuntimeConnection: { forUri: () => ({}) }, approveAll: () => "allow" },
  };
  vi.spyOn(process.stderr, "write").mockImplementation(() => true);
  expect((await runWithCopilotSDK(options)).exitCode).toBe(0);
  const sessionId = create.mock.calls[0][0].sessionId;
  expect(create).toHaveBeenCalledTimes(1);
  expect(resume).not.toHaveBeenCalled();
  fs.appendFileSync(path.join(env.COPILOT_HOME, "session-state", sessionId, "events.jsonl"), '{"type":"assistant.message","data":{"model":"old-model"}}\n');
  captureCopilotSessions({ env, home: env.HOME });
  const state = env.GH_AW_DURABLE_SESSION_DIR;
  artifactRoundtrip(env, [path.join(state, "session.json"), path.join(state, "session-state")]);
  // The driver capture is per-run, unlike the full native transcript.
  fs.rmSync(path.join(directory, "sdk-events"), { recursive: true });
  vi.stubEnv("GH_AW_DURABLE_RESUMED", "true");
  expect((await runWithCopilotSDK(options)).exitCode).toBe(0);
  expect(create).toHaveBeenCalledTimes(1);
  expect(resume).toHaveBeenCalledWith(sessionId, expect.objectContaining({ allowTranscriptRecovery: false, onPermissionRequest: expect.any(Function), model: "new-model", workingDirectory: directory }));
});

it("ignores restored SDK model history but still rejects a new substitution", () => {
  const native = path.join(directory, id, "events.jsonl");
  fs.mkdirSync(path.dirname(native), { recursive: true });
  fs.writeFileSync(native, '{"type":"assistant.message","data":{"model":"old-model"}}\n');
  const observer = createCopilotSDKModelObserver({ configuredModel: "new-model", expectedModel: "new-model", sessionId: id, sessionDirs: [directory], env: { GH_AW_TMP_DIR: directory } });
  fs.appendFileSync(native, '{"type":"assistant.message","data":{"model":"new-model"}}\n');
  expect(observer.finalize(id)).toBe("");
  fs.appendFileSync(native, '{"type":"assistant.message","data":{"model":"wrong-model"}}\n');
  expect(observer.finalize(id)).toContain("substituted");
});
