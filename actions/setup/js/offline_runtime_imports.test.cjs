import { afterEach, describe, expect, it, vi } from "vitest";
import { execFileSync } from "node:child_process";
import path from "node:path";

afterEach(() => vi.unstubAllEnvs());

describe("offline library imports", () => {
  it("imports session schemas and pure session helpers without a runtime environment", () => {
    const env = { ...process.env };
    for (const name of ["GH_AW_RUNTIME_DIR", "GH_AW_RUNTIME_ID", "GH_AW_TMP_DIR", "RUNNER_TEMP", "NODE_COMPILE_CACHE"]) delete env[name];
    const source = `
      const assert = require("node:assert/strict");
      const constants = require("./constants.cjs");
      assert.equal(constants.AGENT_OUTPUT_FILENAME, "agent_output.json");
      assert.equal(constants.GITHUB_API_VERSION, "2026-03-10");
      assert.throws(() => constants.TMP_GH_AW_PATH, /GH_AW_RUNTIME_DIR/);
      assert.throws(() => constants.MANIFEST_FILE_PATH, /GH_AW_RUNTIME_DIR/);
      const redaction = require("./redact_secrets.cjs");
      assert.equal(redaction.redactBuiltInPatterns("diagnostic").content, "diagnostic");
      assert.throws(() => redaction.AWF_RESOLVED_CONFIG_STAGING_PATH, /GH_AW_RUNTIME_DIR/);
      assert.throws(() => redaction.MCP_GATEWAY_CONFIG_PATHS, /GH_AW_RUNTIME_DIR/);
      const session = require("./unified_session.cjs");
      assert.equal(session.SESSION_FILE_FORMAT_VERSION, 1);
      assert.deepEqual(session.mergeSessionSources([]), []);
      require("./scripts/generate_session_schemas.cjs");
    `;
    expect(() => execFileSync(process.execPath, ["-e", source], { cwd: __dirname, env, stdio: "pipe" })).not.toThrow();
  });

  it("resolves lazy exported paths only from the current invocation", () => {
    const constants = require("./constants.cjs");
    const experiments = require("./experiment_helpers.cjs");
    vi.stubEnv("GH_AW_RUNTIME_DIR", undefined);
    vi.stubEnv("GH_AW_TMP_DIR", "/setup-only");
    expect(() => constants.TMP_GH_AW_PATH).toThrow("GH_AW_RUNTIME_DIR");
    expect(() => experiments.EXPERIMENT_ASSIGNMENTS_PATH).toThrow("GH_AW_RUNTIME_DIR");
    vi.stubEnv("GH_AW_RUNTIME_DIR", "/private/invocation-one");
    expect(constants.MANIFEST_FILE_PATH).toBe(path.join("/private/invocation-one", "safe-output-items.jsonl"));
    expect(experiments.EXPERIMENT_ASSIGNMENTS_PATH).toBe(path.join("/private/invocation-one", "experiments/assignments.json"));
    vi.stubEnv("GH_AW_RUNTIME_DIR", "/private/invocation-two");
    expect(constants.MANIFEST_FILE_PATH).toBe(path.join("/private/invocation-two", "safe-output-items.jsonl"));
  });
});
