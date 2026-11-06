import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "module";
import { spawnSync } from "child_process";
import fs from "fs";
import path from "path";

const require = createRequire(import.meta.url);
const { createCopilotModelObserver, recordCopilotModelSelection, persistCopilotModelSelection } = require("./copilot_model_selection.cjs");
const { resolveEffectiveModel, getEffectiveModelLabel } = require("./model_attribution.cjs");
const { detectErrors } = require("./agent_error_patterns.cjs");

describe("Copilot configured versus observed model", () => {
  let root, env, session, infoPath;
  beforeEach(() => {
    root = fs.mkdtempSync(path.join(process.cwd(), ".copilot-model-selection-"));
    env = { GH_AW_TMP_DIR: root, GH_AW_AWF_CONFIG_PATH: path.join(root, "awf-config.json"), HOME: root, COPILOT_HOME: path.join(root, ".copilot"), GH_AW_ENGINE_ID: "copilot", GH_AW_ENGINE_MODEL: "gemini-3.8-flash" };
    session = path.join(root, ".copilot/session-state/session/events.jsonl");
    infoPath = path.join(root, "aw_info.json");
    fs.mkdirSync(path.dirname(session), { recursive: true });
    fs.writeFileSync(infoPath, JSON.stringify({ model: "gemini-3.8-flash" }));
  });
  afterEach(() => fs.rmSync(root, { recursive: true, force: true }));

  const startup = model => ({ type: "session.model_change", data: { source: "startup", oldModel: "gemini-3.8-flash", newModel: model } });
  function append(event) {
    fs.appendFileSync(session, JSON.stringify(event) + "\n");
  }
  function observer(resolvedModel = "gemini-3.8-flash") {
    return createCopilotModelObserver({ configuredModel: "gemini-3.8-flash", resolvedModel, sessionId: "session", env });
  }
  function readInfo() {
    persistCopilotModelSelection(env);
    return JSON.parse(fs.readFileSync(infoPath, "utf8"));
  }

  it.each(["gemini-3.8-flash", "claude-sonnet-5"])("verifies %s with read-only workflow metadata and persists only on the runner", model => {
    const original = fs.readFileSync(infoPath, "utf8");
    const writeFileSync = fs.writeFileSync;
    const write = vi.spyOn(fs, "writeFileSync").mockImplementation((file, ...args) => {
      if (file === infoPath) throw Object.assign(new Error("read-only file system"), { code: "EROFS" });
      return writeFileSync(file, ...args);
    });
    try {
      const watch = observer();
      append(startup(model));
      expect(watch.check()).toEqual(model === "gemini-3.8-flash" ? "" : expect.stringContaining("Refusing silent model substitution"));
      expect(fs.readFileSync(infoPath, "utf8")).toBe(original);
      expect(write.mock.calls.some(([file]) => file === infoPath)).toBe(false);
    } finally {
      write.mockRestore();
    }
    expect(readInfo()).toMatchObject({
      model,
      model_selection: { status: model === "gemini-3.8-flash" ? "observed" : "substituted", effective_model: model },
    });
  });

  it("keeps models observed before a retry when the resumed session switches models", () => {
    const autoObserver = () => createCopilotModelObserver({ configuredModel: "auto", resolvedModel: "auto", sessionId: "session", env });
    const first = autoObserver();
    append({ type: "assistant.message", data: { model: "gemini-3.8-flash" } });
    expect(first.check()).toBe("");
    const resumed = autoObserver();
    append({ type: "assistant.message", data: { model: "claude-sonnet-5" } });
    expect(resumed.check()).toBe("");
    expect(readInfo().model_selection).toMatchObject({ status: "observed", effective_model: "claude-sonnet-5", observed_models: ["gemini-3.8-flash", "claude-sonnet-5"] });
    expect(getEffectiveModelLabel(infoPath, "agent", { GH_AW_ENGINE_MODEL: "auto" })).toBe("auto (Gemini 3.8 Flash, Claude Sonnet 5)");
  });

  it("records an unavailable model without creating workflow metadata in the sandbox", () => {
    fs.rmSync(infoPath);
    recordCopilotModelSelection({ status: "unavailable", configured_model: "gemini-3.8-flash", resolved_model: "", effective_model: "" }, env);
    expect(fs.existsSync(infoPath)).toBe(false);
    expect(readInfo()).toMatchObject({ requested_model: "gemini-3.8-flash", model: "", model_selection: { status: "unavailable" } });
  });

  it("persists only validated selection fields, not arbitrary sandbox metadata", () => {
    recordCopilotModelSelection(
      { status: "observed", configured_model: "gemini-3.8-flash", resolved_model: "gemini-3.8-flash", effective_model: "gemini-3.8-flash", fallback_model: "forged", model_routing: { status: "selected", wire_model: "forged" } },
      env
    );
    const info = readInfo();
    expect(info.fallback_model).toBeUndefined();
    expect(info.model_routing).toBeUndefined();
    expect(Object.keys(info.model_selection).sort()).toEqual(["configured_model", "effective_model", "resolved_model", "status"]);
  });

  it.each([
    { status: "forged", configured_model: "gemini-3.8-flash" },
    { status: "observed", configured_model: "gemini-3.8-flash", resolved_model: "gemini-3.8-flash", effective_model: "model -->" },
    { status: "observed", configured_model: "gemini-3.8-flash", resolved_model: "gemini-3.8-flash", effective_model: 42 },
  ])("rejects invalid sandbox selection evidence (%j)", selection => {
    const original = fs.readFileSync(infoPath, "utf8");
    recordCopilotModelSelection(selection, env);
    expect(() => persistCopilotModelSelection(env)).toThrow("Invalid Copilot model selection");
    expect(fs.readFileSync(infoPath, "utf8")).toBe(original);
  });

  it.each([{ GH_AW_PHASE: "detection" }, { GH_AW_ENGINE_ID: "claude" }])("ignores Copilot evidence outside the agent phase and engine (%j)", overrides => {
    recordCopilotModelSelection({ status: "unavailable", configured_model: "gemini-3.8-flash", resolved_model: "", effective_model: "" }, env);
    persistCopilotModelSelection({ ...env, ...overrides });
    expect(JSON.parse(fs.readFileSync(infoPath, "utf8"))).toEqual({ model: "gemini-3.8-flash" });
  });

  it("rejects a startup vendor substitution and preserves configured and effective models", () => {
    const watch = observer();
    append(startup("claude-sonnet-5"));
    const diagnostic = watch.check();
    expect(diagnostic).toContain("Copilot substituted 'claude-sonnet-5'");
    expect(detectErrors(diagnostic).modelNotSupportedError).toBe(true);
    expect(readInfo()).toMatchObject({
      requested_model: "gemini-3.8-flash",
      model: "claude-sonnet-5",
      model_selection: { status: "substituted", effective_model: "claude-sonnet-5" },
    });
    expect(resolveEffectiveModel(infoPath, "agent", env)).toMatchObject({ model: "claude-sonnet-5", requestedModel: "gemini-3.8-flash" });
    expect(getEffectiveModelLabel(infoPath, "agent", env)).toContain("sonnet");
    expect(watch.check()).toBe(diagnostic);
  });

  it("also detects substitution from main assistant messages without a startup event", () => {
    const watch = observer();
    append({ type: "assistant.message", data: { model: "claude-sonnet-5", content: "Hello" } });
    expect(watch.check()).toContain("Refusing silent model substitution");
  });

  it("accepts resolved aliases, provider prefixes, and reasoning query parameters", () => {
    const watch = observer("copilot/gpt-6.1-sol?effort=high");
    append({ type: "assistant.message", data: { model: "github-copilot/gpt-6.1-sol?effort=low" } });
    expect(watch.check()).toBe("");
    persistCopilotModelSelection(env);
    expect(resolveEffectiveModel(infoPath, "agent", env).model).toBe("gpt-6.1-sol");
  });

  it.each(["github-copilot/claude-sonnet-5?effort=low", "copilot/gpt-6-sol?effort=high"])("rejects a different underlying base model despite provider/effort normalization (%s)", model => {
    const watch = observer("copilot/gpt-6.1-sol?effort=high");
    append({ type: "assistant.message", data: { model } });
    expect(watch.check()).toContain("Refusing silent model substitution");
    expect(readInfo().model_selection.status).toBe("substituted");
  });

  it("ignores unrelated sessions appending main-agent events after observer initialization", () => {
    const unrelated = path.join(root, ".copilot/session-state/unrelated/events.jsonl");
    fs.mkdirSync(path.dirname(unrelated), { recursive: true });
    fs.writeFileSync(unrelated, JSON.stringify({ type: "session.start", data: { sessionId: "unrelated" } }) + "\n");
    const watch = observer();
    fs.appendFileSync(unrelated, JSON.stringify(startup("claude-sonnet-5")) + "\n");
    expect(watch.check()).toBe("");
    expect(readInfo().model_selection).toBeUndefined();
    append({ type: "assistant.message", data: { sessionId: "unrelated", model: "claude-sonnet-5" } });
    append(startup("gemini-3.8-flash"));
    expect(watch.check()).toBe("");
    expect(readInfo().model_selection).toMatchObject({ status: "observed", effective_model: "gemini-3.8-flash" });
    fs.appendFileSync(unrelated, JSON.stringify(startup("claude-opus-5")) + "\n");
    expect(watch.check()).toBe("");
    expect(readInfo().model).toBe("gemini-3.8-flash");
  });

  it("requires invocation ownership before inspecting session files", () => {
    expect(() => createCopilotModelObserver({ configuredModel: "gemini", resolvedModel: "gemini-3.8-flash", env })).toThrow("exact invocation session ID");
  });

  it("ignores cached records, nested agents, quoted event text, and partial lines", () => {
    append(startup("claude-sonnet-5"));
    const watch = observer();
    append({ ...startup("claude-sonnet-5"), agentId: "explore" });
    append({ type: "assistant.message", parentToolCallId: "task-1", data: { model: "claude-sonnet-5" } });
    append({ type: "assistant.message", data: { content: JSON.stringify(startup("claude-sonnet-5")) } });
    fs.appendFileSync(session, JSON.stringify(startup("gemini-3.8-flash")));
    expect(watch.check()).toBe("");
    fs.appendFileSync(session, "\n");
    expect(watch.check()).toBe("");
    expect(readInfo().model_selection.status).toBe("observed");
  });

  it("does not inspect or change detection-phase models", () => {
    env.GH_AW_PHASE = "detection";
    const watch = observer();
    append(startup("claude-sonnet-5"));
    expect(watch.check()).toBe("");
    expect(JSON.parse(fs.readFileSync(infoPath, "utf8")).model).toBe("gemini-3.8-flash");
    expect(JSON.parse(fs.readFileSync(infoPath, "utf8")).model_selection).toBeUndefined();
  });

  it("does not attribute execution to an unavailable model", () => {
    recordCopilotModelSelection({ status: "unavailable", configured_model: "gemini-3.8-flash", resolved_model: "", effective_model: "" }, env);
    persistCopilotModelSelection(env);
    expect(resolveEffectiveModel(infoPath, "agent", env)).toMatchObject({ model: "", requestedModel: "gemini-3.8-flash" });
  });

  it("accepts native auto selection but retains the requested model", () => {
    const watch = observer("auto");
    append(startup("claude-sonnet-5"));
    expect(watch.check()).toBe("");
    expect(readInfo().model_selection.status).toBe("observed");
  });

  it("checks a final complete event even without a trailing newline", () => {
    const watch = observer();
    fs.writeFileSync(session, JSON.stringify(startup("claude-sonnet-5")));
    expect(watch.check()).toBe("");
    expect(watch.check(true)).toContain("substituted");
  });

  it("preserves authorized proxy fallback attribution", () => {
    const watch = observer();
    append(startup("gemini-3.8-flash"));
    expect(watch.check()).toBe("");
    const info = readInfo();
    info.fallback_model = "gpt-6.1-sol";
    fs.writeFileSync(infoPath, JSON.stringify(info));
    expect(resolveEffectiveModel(infoPath, "agent", env).model).toBe("gpt-6.1-sol");
  });

  it.each([["--model", "gpt-6.1-sol"], ["--model=gpt-6.1-sol"]])("accepts a correct native GPT override of a Claude environment default (%j)", (...modelArgs) => {
    const stub = path.join(root, "override-cli.cjs");
    fs.writeFileSync(
      stub,
      `
      const fs = require("fs"), path = require("path");
      const sessionId = process.argv[process.argv.indexOf("--session-id") + 1];
      const file = path.join(process.env.COPILOT_HOME, "session-state", sessionId, "events.jsonl");
      fs.mkdirSync(path.dirname(file), { recursive: true });
      fs.appendFileSync(file, JSON.stringify({ type: "assistant.message", data: { model: "gpt-6.1-sol" } }) + "\\n");
      fs.writeFileSync(path.join(process.env.GH_AW_TMP_DIR, "invocation.json"), JSON.stringify({
        args: process.argv.slice(2), model: process.env.COPILOT_MODEL, wire: process.env.COPILOT_PROVIDER_WIRE_API
      }));
      console.log("CLI says success");
      `
    );
    const run = spawnSync(process.execPath, [require.resolve("./copilot_harness.cjs"), process.execPath, stub, ...modelArgs], {
      env: {
        ...process.env,
        ...env,
        COPILOT_MODEL: "claude-sonnet-5",
        COPILOT_PROVIDER_WIRE_API: "",
        GH_AW_PHASE: "agent",
        GH_AW_MODEL_ROUTING: "0",
        GH_AW_SAFE_OUTPUTS: "",
        AWF_REFLECT_ENABLED: "0",
        GH_AW_COPILOT_SDK: "0",
        GH_AW_HARNESS_MAX_RETRIES: "0",
      },
      encoding: "utf8",
      timeout: 10000,
    });
    expect(run.error, run.stderr).toBeUndefined();
    expect(run.status, run.stderr).toBe(0);
    const invocation = JSON.parse(fs.readFileSync(path.join(root, "invocation.json"), "utf8"));
    expect(invocation).toMatchObject({ model: "gpt-6.1-sol", wire: "responses" });
    expect(invocation.args).toEqual(expect.arrayContaining(modelArgs));
    expect(readInfo().model_selection).toMatchObject({ status: "observed", configured_model: "gpt-6.1-sol", effective_model: "gpt-6.1-sol" });
  });

  it("retries transient 400 after ordinary tool model text and resumes only its own session", () => {
    const stub = path.join(root, "retry-cli.cjs");
    const attemptsPath = path.join(root, "attempts.json");
    fs.writeFileSync(
      stub,
      `
      const fs = require("fs"), path = require("path");
      const attemptsPath = ${JSON.stringify(attemptsPath)};
      const attempts = fs.existsSync(attemptsPath) ? JSON.parse(fs.readFileSync(attemptsPath)) : [];
      const sessionId = process.argv[process.argv.indexOf("--session-id") + 1];
      const file = path.join(process.env.COPILOT_HOME, "session-state", sessionId, "events.jsonl");
      attempts.push(sessionId);
      fs.writeFileSync(attemptsPath, JSON.stringify(attempts));
      if (attempts.length === 1) {
        fs.mkdirSync(path.dirname(file), { recursive: true });
        fs.writeFileSync(file, JSON.stringify({ type: "session.start", data: { sessionId } }) + "\\n");
        fs.appendFileSync(file, JSON.stringify({ type: "assistant.message", data: { model: "gpt-6.1-sol", content: "preserved first-turn history" } }) + "\\n");
        console.log('Tool output: model "foo" not found');
        console.error("Execution failed: CAPIError: 400 Bad Request");
        process.exit(1);
      }
      // Mirror pinned CLI --session-id: load existing history instead of creating again.
      if (!fs.existsSync(file) || !fs.readFileSync(file, "utf8").includes("preserved first-turn history")) {
        console.error("Exact-session continuation lost its prior history");
        process.exit(2);
      }
      fs.appendFileSync(file, JSON.stringify({ type: "assistant.message", data: { model: "gpt-6.1-sol" } }) + "\\n");
      console.log("CLI says success");
      `
    );
    const run = spawnSync(process.execPath, [require.resolve("./copilot_harness.cjs"), process.execPath, stub], {
      env: {
        ...process.env,
        ...env,
        COPILOT_MODEL: "gpt-6.1-sol",
        COPILOT_PROVIDER_WIRE_API: "",
        GH_AW_PHASE: "agent",
        GH_AW_MODEL_ROUTING: "0",
        GH_AW_SAFE_OUTPUTS: "",
        AWF_REFLECT_ENABLED: "0",
        GH_AW_COPILOT_SDK: "0",
        GH_AW_HARNESS_INITIAL_DELAY_MS: "1",
        GH_AW_HARNESS_MAX_RETRIES: "1",
      },
      encoding: "utf8",
      timeout: 10000,
    });
    expect(run.error, run.stderr).toBeUndefined();
    expect(run.status, run.stderr).toBe(0);
    const attempts = JSON.parse(fs.readFileSync(attemptsPath, "utf8"));
    expect(attempts).toHaveLength(2);
    expect(attempts[1]).toBe(attempts[0]);
    const history = fs
      .readFileSync(path.join(env.COPILOT_HOME, "session-state", attempts[0], "events.jsonl"), "utf8")
      .trim()
      .split("\n")
      .map(line => JSON.parse(line));
    expect(history).toHaveLength(3);
    expect(history.filter(event => event.type === "session.start")).toHaveLength(1);
    expect(history[1].data.content).toBe("preserved first-turn history");
    expect(run.stderr).toContain("attempt 2: spawning");
    expect(readInfo().model_selection).toMatchObject({ status: "observed", effective_model: "gpt-6.1-sol" });
  });

  it.each([
    [["--continue"], "exact --session-id UUID"],
    [["--model=gpt-6.1-sol", "--model=claude-sonnet-5"], "Conflicting Copilot --model"],
  ])("rejects ambiguous selectors and conflicting invocation models before spawning (%j)", (args, diagnostic) => {
    const stub = path.join(root, "must-not-spawn.cjs");
    const marker = path.join(root, "spawned");
    fs.writeFileSync(stub, `require("fs").writeFileSync(${JSON.stringify(marker)}, "spawned");`);
    const run = spawnSync(process.execPath, [require.resolve("./copilot_harness.cjs"), process.execPath, stub, ...args], {
      env: {
        ...process.env,
        ...env,
        COPILOT_MODEL: "gpt-6.1-sol",
        COPILOT_PROVIDER_WIRE_API: "",
        GH_AW_PHASE: "agent",
        GH_AW_MODEL_ROUTING: "0",
        GH_AW_SAFE_OUTPUTS: "",
        AWF_REFLECT_ENABLED: "0",
        GH_AW_COPILOT_SDK: "0",
      },
      encoding: "utf8",
      timeout: 10000,
    });
    expect(run.error, run.stderr).toBeUndefined();
    expect(run.status).toBe(1);
    expect(run.stderr).toContain(diagnostic);
    expect(fs.existsSync(marker)).toBe(false);
  });

  it.each([false, true])("fails an otherwise successful direct CLI run without retrying (long-lived=%s)", longLived => {
    const stub = path.join(root, "cli.cjs");
    fs.writeFileSync(
      stub,
      `
      const fs = require("fs"), path = require("path");
      const sessionId = process.argv[process.argv.indexOf("--session-id") + 1];
      const file = path.join(process.env.COPILOT_HOME, "session-state", sessionId, "events.jsonl");
      fs.mkdirSync(path.dirname(file), { recursive: true });
      fs.appendFileSync(file, JSON.stringify({ type: "session.model_change", data: { source: "startup", newModel: "claude-sonnet-5" } }) + "\\n");
      console.log("CLI says success");
      ${longLived ? "setInterval(() => {}, 1000);" : ""}
    `
    );
    const run = spawnSync(process.execPath, [require.resolve("./copilot_harness.cjs"), process.execPath, stub], {
      env: {
        ...process.env,
        ...env,
        COPILOT_MODEL: "gemini-3.8-flash",
        GH_AW_PHASE: "agent",
        GH_AW_MODEL_ROUTING: "0",
        GH_AW_SAFE_OUTPUTS: "",
        AWF_REFLECT_ENABLED: "0",
        GH_AW_COPILOT_SDK: "0",
        GH_AW_HARNESS_INITIAL_DELAY_MS: "1",
        GH_AW_HARNESS_MAX_RETRIES: "1",
      },
      encoding: "utf8",
      timeout: 10000,
    });
    expect(run.error, run.stderr).toBeUndefined();
    expect(run.status).toBe(1);
    expect(run.stderr).toContain("Refusing silent model substitution");
    expect(run.stderr).not.toContain("attempt 2: spawning");
    expect(readInfo().model).toBe("claude-sonnet-5");
  });
});
