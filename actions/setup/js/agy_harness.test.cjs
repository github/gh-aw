import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "module";
import { spawnSync } from "node:child_process";
import fs from "fs";
import os from "os";
import path from "path";

const require = createRequire(import.meta.url);
const { resolveAgyModel } = require("./agy_harness.cjs");
const reflectFixtures = require("./fixtures/model_resolution_reflect.cjs");

// The recorded /reflect from run 38945917145 did not configure Gemini, so these tests derive a
// configured Gemini endpoint from it (withGeminiModels); the alias map is the recorded one.
const geminiModels = ["gemini-3.5-pro", "gemini-3.8-flash", "gemini-3.9-flash"];

describe("agy_harness.cjs gated attempt outcomes", () => {
  let root;
  beforeEach(() => {
    root = fs.mkdtempSync(path.join(process.cwd(), ".agy-harness-test-"));
  });
  afterEach(() => fs.rmSync(root, { recursive: true, force: true }));

  function run(scenario, decision = "allow") {
    const binary = path.join(root, "fake-agy.cjs");
    const preload = path.join(root, "decision.cjs");
    const prompt = path.join(root, "prompt.txt");
    fs.writeFileSync(prompt, "Complete the task.");
    fs.writeFileSync(
      preload,
      `const fs = require("node:fs");
  require(${JSON.stringify(require.resolve("./skip_if_decision.cjs"))}).evaluateSkipIfDecision = async () => {
    fs.appendFileSync(${JSON.stringify(path.join(root, "decisions"))}, "decision\\n");
    const outcome = process.env.GH_AW_TEST_DECISION;
    return { outcome, questions: [{ question: 1, outcome }], latencyMs: 1 };
  };`
    );
    fs.writeFileSync(
      binary,
      `#!${process.execPath}
  const fs = require("node:fs");
  const scenario = process.env.GH_AW_TEST_SCENARIO;
  if (process.argv.includes("--version")) {
    if (scenario === "spawn_failure") fs.unlinkSync(__filename);
    console.log("1.3.6");
    process.exit(0);
  }
  fs.appendFileSync(${JSON.stringify(path.join(root, "invocations"))}, "run\\n");
  process.stdin.once("data", () => {
    if (scenario === "interrupted") {
      process.on("SIGTERM", () => process.exit(143));
      process.kill(process.ppid, "SIGTERM");
      return;
    }
    const result = { status: "SUCCESS", num_turns: 1, usage: { input_tokens: 1, output_tokens: 1 } };
    if (scenario === "missing_inference") result.usage.output_tokens = 0;
    if (scenario === "permission_denied") console.error("permission denied");
    if (scenario === "tool_denied") console.log(JSON.stringify({ step_update: { step_type: "tool", state: "DONE", step_index: 1, tool_info: { error: { type: "permission_denied" } } } }));
    if (scenario === "error_result") result.status = "ERROR";
    if (scenario !== "no_result") console.log(JSON.stringify({ event: "result", result }));
    process.exitCode = scenario === "nonzero" ? 7 : 0;
    process.stdin.destroy();
  });`,
      { mode: 0o700 }
    );
    const result = spawnSync(process.execPath, ["--require", preload, require.resolve("./agy_harness.cjs"), binary], {
      encoding: "utf8",
      timeout: 5000,
      env: {
        ...process.env,
        TMPDIR: root,
        GH_AW_ENGINE_VERSION: "1.3.6",
        GH_AW_AGY_MODEL: "gemini-3.8-flash",
        GH_AW_PROMPT: prompt,
        GITHUB_WORKSPACE: root,
        GH_AW_ENGINE_CWD: root,
        GH_AW_TMP_DIR: root,
        GH_AW_HARNESS_TELEMETRY_DIR: path.join(root, "agent"),
        GH_AW_DURABLE_SESSION_DIR: root,
        GH_AW_MCP_CONFIG: "provided",
        GH_AW_SAFE_OUTPUTS: "",
        GH_AW_PHASE: "agent",
        AWF_REFLECT_ENABLED: "0",
        GEMINI_API_KEY: "test-key",
        GH_AW_SKIP_IF_DECISION: decision === "disabled" ? "" : '["Is work already complete?"]',
        GH_AW_TEST_DECISION: decision,
        GH_AW_TEST_SCENARIO: scenario,
      },
    });
    expect(result.error).toBeUndefined();
    const telemetryPath = path.join(root, "agent/harness_retry.json");
    const count = name => (fs.existsSync(path.join(root, name)) ? fs.readFileSync(path.join(root, name), "utf8").trim().split("\n").length : 0);
    return {
      ...result,
      events: result.stdout
        .trim()
        .split("\n")
        .filter(line => line.startsWith("{"))
        .map(line => JSON.parse(line)),
      telemetry: fs.existsSync(telemetryPath) ? JSON.parse(fs.readFileSync(telemetryPath, "utf8")) : null,
      invocations: count("invocations"),
      decisions: count("decisions"),
    };
  }

  const failures = [
    ["nonzero", 7],
    ["permission_denied", 1],
    ["tool_denied", 1],
    ["missing_inference", 1],
    ["error_result", 1],
    ["no_result", 1],
    ["spawn_failure", 1],
  ];
  it.each(["allow", "unknown"].flatMap(decision => failures.map(([scenario, code]) => [decision, scenario, code])))("records %s %s as a failed attempt with exit code %s", (decision, scenario, code) => {
    const result = run(scenario, decision);
    expect(result.status).toBe(code);
    expect(result.decisions).toBe(1);
    expect(result.invocations).toBe(scenario === "spawn_failure" ? 0 : 1);
    expect(result.telemetry).toMatchObject({
      attempts: 1,
      exit_code: code,
      attempt_results: [{ outcome: "failure", exit_code: code }],
      decision_results: [{ outcome: decision, avoidedInvocation: false }],
    });
    expect(result.events.filter(event => event.type === "session.error")).toHaveLength(1);
  });

  it.each(["allow", "unknown"])("preserves true interruption after %s rather than recording an ordinary failure", decision => {
    const result = run("interrupted", decision);
    expect(result.status).toBe(1);
    expect(result.invocations).toBe(1);
    expect(result.decisions).toBe(1);
    expect(result.telemetry).toMatchObject({ attempts: 1, exit_code: 1, attempt_results: [{ outcome: "interrupted", exit_code: null }] });
    expect(result.events.filter(event => event.type === "session.error")).toHaveLength(1);
  });

  it.each(failures)("preserves disabled-gate error reporting for %s", scenario => {
    const result = run(scenario, "disabled");
    expect(result.status).toBe(1);
    expect(result.decisions).toBe(0);
    expect(result.telemetry).toBeNull();
    expect(result.events.filter(event => event.type === "session.error")).toHaveLength(1);
  });

  it.each(["allow", "unknown"])("preserves successful inference after %s", decision => {
    const result = run("success", decision);
    expect(result.status).toBe(0);
    expect(result.invocations).toBe(1);
    expect(result.decisions).toBe(1);
    expect(result.telemetry).toMatchObject({ attempts: 1, exit_code: 0, attempt_results: [{ outcome: "success", exit_code: 0 }] });
    expect(result.events.filter(event => event.type === "session.error")).toHaveLength(0);
  });

  it("does not invoke Agy or report an error when the gate denies", () => {
    const result = run("nonzero", "deny");
    expect(result.status).toBe(0);
    expect(result.invocations).toBe(0);
    expect(result.decisions).toBe(1);
    expect(result.telemetry).toMatchObject({ attempts: 0, exit_code: 0, attempt_results: [], decision_results: [{ outcome: "deny", avoidedInvocation: true }] });
    expect(result.events.filter(event => event.type === "session.error")).toHaveLength(0);
  });
});

describe("agy_harness.cjs resolveAgyModel", () => {
  let tmpDir;
  let env;
  beforeEach(() => {
    tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "agy-model-resolution-"));
    env = { GH_AW_TMP_DIR: tmpDir, GH_AW_AWF_CONFIG_PATH: path.join(tmpDir, "missing-awf-config.json") };
  });
  afterEach(() => fs.rmSync(tmpDir, { recursive: true, force: true }));
  const records = () => JSON.parse(fs.readFileSync(path.join(tmpDir, "agent/model-resolution.json"), "utf8"));
  const resolve = (model, reflectData, options = {}) => resolveAgyModel({ model, reflectData, env, logger: () => {}, ...options });
  const withAliases = (reflect, aliases) => ({ ...reflect, model_aliases: { ...reflect.model_aliases, ...aliases } });

  it("passes an exact slug unchanged", async () => {
    expect(await resolve("gemini-3.8-flash", reflectFixtures.withGeminiModels(geminiModels))).toBe("gemini-3.8-flash");
    expect(await resolve("gemini-9-unlisted", reflectFixtures.withGeminiModels(geminiModels))).toBe("gemini-9-unlisted");
    expect(records().at(-1)).toEqual({ engine: "agy", role: "main", configured_model: "gemini-9-unlisted", resolved_model: "gemini-9-unlisted", provider: "gemini", status: "exact", cause: null });
  });

  it("resolves an alias to a native Gemini slug", async () => {
    const reflect = withAliases(reflectFixtures.withGeminiModels(geminiModels), { flash: ["gemini/gemini-*-flash"] });
    const logs = [];
    expect(await resolve("flash", reflect, { logger: message => logs.push(message) })).toBe("gemini-3.9-flash");
    expect(await resolve("gemini/flash", reflect)).toBe("gemini-3.9-flash");
    expect(logs.filter(line => line.startsWith("model resolution: engine=agy"))).toEqual([expect.stringContaining("configured=flash resolved=gemini-3.9-flash")]);
    expect(records()).toEqual([{ engine: "agy", role: "main", configured_model: "gemini/flash", resolved_model: "gemini-3.9-flash", provider: "gemini", status: "resolved", cause: null }]);
  });

  // The recorded alias map predates the built-in gemini-3.8-flash alias, so the test adds the
  // built-in definition; gemini-3.8-flash is then both an alias and a live slug.
  it("a live exact slug wins over an alias with the same name", async () => {
    const aliases = { "gemini-3.8-flash": ["copilot/gemini-3.8*flash*", "google/gemini-3.8*flash*", "gemini/gemini-3.8*flash*"] };
    const live = withAliases(reflectFixtures.withGeminiModels(["gemini-3.8-flash-lite", ...geminiModels]), aliases);
    expect(await resolve("gemini-3.8-flash", live)).toBe("gemini-3.8-flash");
    expect(records().at(-1)).toEqual({ engine: "agy", role: "main", configured_model: "gemini-3.8-flash", resolved_model: "gemini-3.8-flash", provider: "gemini", status: "exact", cause: null });
    const absent = withAliases(reflectFixtures.withGeminiModels(["gemini-3.5-pro", "gemini-3.8-flash-lite"]), aliases);
    expect(await resolve("gemini-3.8-flash", absent)).toBe("gemini-3.8-flash-lite");
  });

  it("rejects provider/model syntax after resolution", async () => {
    await expect(resolve("gemini/gemini-3.8-flash", reflectFixtures.withGeminiModels(geminiModels))).rejects.toThrow("Agy requires a native model slug");
  });

  it("workflow-defined alias overrides built-in", async () => {
    const reflect = reflectFixtures.withGeminiModels(geminiModels);
    expect(await resolve("any", reflect)).toBe("gemini-3.9-flash");
    expect(await resolve("any", withAliases(reflect, { any: ["gemini/gemini-3.8*"] }))).toBe("gemini-3.8-flash");
  });

  // Incomplete discovery is derived from the recorded file (models_fetch_complete: false);
  // the Gemini endpoint is configured but has no model list. No recorded run has this shape.
  it("incomplete discovery passes exact ids through and fails aliases after one refetch", async () => {
    const incomplete = () => ({ ...reflectFixtures.withGeminiModels(null), models_fetch_complete: false });
    expect(await resolve("gemini-3.8-flash", incomplete())).toBe("gemini-3.8-flash");
    const refetchReflectData = vi.fn().mockResolvedValue(incomplete());
    await expect(resolve("any", incomplete(), { refetchReflectData })).rejects.toMatchObject({ name: "ModelAliasResolutionError", reason: "catalog_unavailable" });
    expect(refetchReflectData).toHaveBeenCalledTimes(1);
    expect(records().at(-1)).toEqual({ engine: "agy", role: "main", configured_model: "any", resolved_model: null, provider: "gemini", status: "failed", cause: "catalog_unavailable" });
  });

  it("does not use the Copilot integration sentence for Gemini failures", async () => {
    const reflect = withAliases(reflectFixtures.withGeminiModels(geminiModels), { flash: ["gemini/gemini-*-ultra"] });
    const error = await resolve("flash", reflect).catch(err => err);
    expect(error).toMatchObject({ name: "ModelAliasResolutionError", reason: "model_unavailable" });
    expect(error.message).toContain("The requested model is not supported");
    expect(error.message).not.toContain("agentic-workflows integration");
  });
});
