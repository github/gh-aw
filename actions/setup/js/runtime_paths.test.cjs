import { afterEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import path from "node:path";

const { runtimeRoot, runtimePath, setupRoot, runtimeId, runtimeContainerName, resolveRuntimePaths, runtimePort, registerRuntimeProcess } = require("./runtime_paths.cjs");
const { getGatewayStartupMarkerPath, removeRuntimeGatewayContainer } = require("./start_mcp_gateway.cjs");
const { getBundlePathForBranch } = require("./generate_git_bundle.cjs");
const { resolveHarnessRetryTelemetryPath } = require("./harness_retry_runner.cjs");
const { main: generateUsageArtifacts } = require("./generate_usage_artifacts.cjs");
const { renderPrompt } = require("./create_prompt.cjs");
const { main: setupThreatDetection } = require("./setup_threat_detection.cjs");

const originalRoot = runtimeRoot();
const roots = [];

function isolatedRuntime() {
  const root = fs.mkdtempSync(path.join(originalRoot, "custom runtime-"));
  roots.push(root);
  vi.stubEnv("GH_AW_RUNTIME_DIR", root);
  vi.stubEnv("GH_AW_RUNTIME_ID", "runtime-test-123");
  return root;
}

afterEach(() => {
  vi.unstubAllEnvs();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  for (const root of roots.splice(0)) fs.rmSync(root, { recursive: true, force: true });
});

describe("private runtime paths", () => {
  it("fails closed without runtime state and does not use setup's legacy variable", () => {
    expect(() => runtimeRoot({ GH_AW_TMP_DIR: "/setup/assets" })).toThrow("GH_AW_RUNTIME_DIR");
    for (const root of ["relative", "/", "/runtime\ninvalid"]) {
      expect(() => runtimeRoot({ GH_AW_RUNTIME_DIR: root })).toThrow("GH_AW_RUNTIME_DIR");
    }
    expect(setupRoot({ RUNNER_TEMP: "/runner-temp", GH_AW_RUNTIME_DIR: "/private/runtime" })).toBe("/runner-temp/gh-aw");
  });

  it("keeps all derived paths within a custom root", () => {
    const root = isolatedRuntime();
    expect(runtimePath("agent", "result.json")).toBe(path.join(root, "agent/result.json"));
    expect(() => runtimePath("../outside")).toThrow("inside GH_AW_RUNTIME_DIR");
    expect(() => runtimePath("/outside")).toThrow("inside GH_AW_RUNTIME_DIR");
    expect(runtimeContainerName("awmg-mcpg")).toBe("awmg-mcpg-runtime-test-123");
    expect(() => runtimeId({ GH_AW_RUNTIME_ID: "unsafe;name" })).toThrow("Docker-safe");
  });

  it("resolves only canonical prompt paths, preserving evaluated content and similarly named directories", () => {
    const root = isolatedRuntime();
    const prompts = path.join(root, "prompts");
    fs.mkdirSync(prompts);
    fs.writeFileSync(path.join(prompts, "temp.md"), "Use /tmp/gh-aw/agent and not /tmp/gh-aw-other.");
    const rendered = renderPrompt({ items: [{ file: "temp.md" }, { content_env: "CONTEXT" }] }, { ...process.env, CONTEXT: "literal /tmp/gh-aw/from-context" }, prompts);
    expect(rendered).toBe(`Use ${root}/agent and not /tmp/gh-aw-other.literal /tmp/gh-aw/from-context`);
    expect(resolveRuntimePaths("root=/tmp/gh-aw")).toBe(`root=${root}`);
  });

  it("uses runtime ports without overriding explicit server options", () => {
    expect(runtimePort("GH_AW_MCP_SCRIPTS_PORT", 3000, { GH_AW_MCP_SCRIPTS_PORT: "49152" })).toBe(49152);
    expect(runtimePort("GH_AW_SAFE_OUTPUTS_PORT", 3001, {})).toBe(3001);
    for (const value of ["0", "65536", "123;false"]) {
      expect(() => runtimePort("GH_AW_SAFE_OUTPUTS_PORT", 3001, { GH_AW_SAFE_OUTPUTS_PORT: value })).toThrow("valid port");
    }
  });

  it("records the Linux PID start identity under only the owning runtime", () => {
    if (process.platform !== "linux") return;
    const root = isolatedRuntime();
    registerRuntimeProcess(process.pid, "test-host");
    const record = JSON.parse(fs.readFileSync(path.join(root, ".gh-aw-processes.jsonl"), "utf8"));
    expect(record).toMatchObject({ pid: process.pid, uid: process.getuid(), role: "test-host" });
    expect(record.start).toMatch(/^\d+$/);
  });
});

describe("runtime gateway ownership", () => {
  it("uses a custom runtime startup marker and removes only its labeled container", () => {
    const root = isolatedRuntime();
    expect(getGatewayStartupMarkerPath(process.env)).toBe(path.join(root, "mcp-gateway-started"));
    const containerId = "a".repeat(64);
    const run = vi.fn().mockReturnValueOnce(JSON.stringify({ Name: "/awmg-mcpg-runtime-test-123", Config: { Labels: { "gh-aw.runtime-id": "runtime-test-123" } }, Id: containerId }));
    expect(removeRuntimeGatewayContainer(process.env, run)).toBe(true);
    expect(run.mock.calls[0][1]).toEqual(["inspect", "--type", "container", "--format", "{{json .}}", "awmg-mcpg-runtime-test-123"]);
    expect(run.mock.calls[1][1]).toEqual(["rm", "-f", containerId]);
  });

  it("does not remove an unlabeled, differently owned, or missing container", () => {
    isolatedRuntime();
    for (const label of ["", "other-runtime"]) {
      const run = vi.fn().mockReturnValue(JSON.stringify({ Name: "/awmg-mcpg-runtime-test-123", Config: { Labels: { "gh-aw.runtime-id": label } }, Id: "a".repeat(64) }));
      expect(() => removeRuntimeGatewayContainer(process.env, run)).toThrow("ownership label");
      expect(run).toHaveBeenCalledTimes(1);
    }
    const missing = vi.fn(() => {
      throw new Error("not found");
    });
    expect(removeRuntimeGatewayContainer(process.env, missing)).toBe(false);
    expect(missing).toHaveBeenCalledTimes(1);
  });

  it("rejects a name mismatch, abbreviated container ID, or malformed inspection metadata", () => {
    isolatedRuntime();
    const owned = { Name: "/awmg-mcpg-runtime-test-123", Config: { Labels: { "gh-aw.runtime-id": "runtime-test-123" } }, Id: "a".repeat(64) };
    for (const metadata of [JSON.stringify({ ...owned, Name: "/another-container" }), JSON.stringify({ ...owned, Id: "abcd1234" }), "invalid-json", "null"]) {
      const run = vi.fn().mockReturnValue(metadata);
      expect(() => removeRuntimeGatewayContainer(process.env, run)).toThrow("Refusing to remove gateway container");
      expect(run).toHaveBeenCalledTimes(1);
    }
  });
});

describe("runtime artifacts, detection, and harnesses", () => {
  it("keeps transport artifacts and both harness phases in the same private root", () => {
    const root = isolatedRuntime();
    vi.stubEnv("GH_AW_WORK_QUEUE_ENABLED", "");
    vi.stubEnv("GH_AW_HARNESS_TELEMETRY_DIR", "");
    expect(getBundlePathForBranch("feature/test")).toBe(path.join(root, "aw-feature-test.bundle"));
    expect(resolveHarnessRetryTelemetryPath({ GH_AW_RUNTIME_DIR: root })).toBe(path.join(root, "agent/harness_retry.json"));
    expect(resolveHarnessRetryTelemetryPath({ GH_AW_RUNTIME_DIR: root, GH_AW_PHASE: "detection" })).toBe(path.join(root, "threat-detection/detection_harness_retry.json"));
  });

  it("enumerates only the current invocation's usage artifacts", async () => {
    const root = isolatedRuntime();
    fs.mkdirSync(path.join(root, "usage/agent"), { recursive: true });
    const filename = path.join(root, "usage/agent/tokens.json");
    fs.writeFileSync(filename, "{}");
    const core = { info: vi.fn(), warning: vi.fn() };
    await generateUsageArtifacts({ core, generateSummary: async () => {}, generateSession: async () => {} });
    expect(core.info).toHaveBeenCalledExactlyOnceWith(filename);
  });

  it("reads downloaded detector context and writes its prompt only in the private root", async () => {
    const root = isolatedRuntime();
    const prompts = path.join(root, "templates");
    fs.mkdirSync(prompts);
    fs.writeFileSync(path.join(prompts, "threat_detection.md"), "prompt={WORKFLOW_PROMPT_FILE}\noutput={AGENT_OUTPUT_FILE}\npatch={AGENT_PATCH_FILE}\n");
    fs.mkdirSync(path.join(root, "threat-detection/aw-prompts"), { recursive: true });
    fs.writeFileSync(path.join(root, "threat-detection/aw-prompts/prompt.txt"), "workflow context");
    fs.writeFileSync(path.join(root, "threat-detection/agent_output.json"), "{}");
    vi.stubEnv("GH_AW_PROMPTS_DIR", prompts);
    vi.stubEnv("GH_AW_DETECTION_CONTINUE_ON_ERROR", "true");
    vi.stubEnv("GH_AW_DETECTION_SKIP_PROMPT_SUMMARY", "true");
    const core = { info: vi.fn(), warning: vi.fn(), error: vi.fn(), setFailed: vi.fn(), setOutput: vi.fn(), exportVariable: vi.fn() };
    vi.stubGlobal("core", core);
    await setupThreatDetection();
    expect(core.setFailed).not.toHaveBeenCalled();
    expect(core.exportVariable).toHaveBeenCalledWith("GH_AW_PROMPT", path.join(root, "aw-prompts/prompt.txt"));
    expect(fs.readFileSync(path.join(root, "aw-prompts/prompt.txt"), "utf8")).toContain(path.join(root, "threat-detection/agent_output.json"));
  });
});
