// @ts-check

"use strict";

const path = require("path");
const fs = require("node:fs");
const os = require("node:os");
const { spawnSync } = require("child_process");

describe("shell_harness.cjs", () => {
  const harnessPath = path.join(__dirname, "shell_harness.cjs");

  it("runs a shell command through the shared process runner", () => {
    const result = spawnSync(process.execPath, [harnessPath, "test", "printf hello"], { encoding: "utf8" });

    expect(result.status).toBe(0);
    expect(result.stdout).toBe("hello");
    expect(result.stderr).toContain("[test-harness] attempt 1: process closed");
  });

  it("reports invalid invocations", () => {
    const result = spawnSync(process.execPath, [harnessPath], { encoding: "utf8" });

    expect(result.status).toBe(2);
    expect(result.stderr).toContain("Usage:");
  });

  it.each(["deny", "allow", "unknown", "api-error"].flatMap(outcome => ["command", "custom-harness"].map(mode => [outcome, mode])))("gates the actual %s invocation through %s", (outcome, mode) => {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), "shell-decision-"));
    try {
      const preload = path.join(root, "fetch.cjs");
      fs.writeFileSync(
        preload,
        `
global.fetch = async url => {
  if (url.endsWith("/reflect")) return new Response(JSON.stringify({ endpoints: [{ configured: true, provider: "github", models_url: "http://api-proxy:10002/v1/models" }] }));
  if (${JSON.stringify(outcome)} === "api-error") return new Response("", { status: 503 });
  return new Response(JSON.stringify({ answers: [{ name: "skip_1", type: "choice", choice: ${JSON.stringify(outcome)} }] }));
};`
      );
      const customHarness = path.join(root, "custom_harness.cjs");
      fs.writeFileSync(customHarness, 'process.stdout.write("AGENT_INVOKED");');
      const command = mode === "custom-harness" ? `"${process.execPath}" "${customHarness}"` : "printf AGENT_INVOKED";
      const result = spawnSync(process.execPath, ["--require", preload, harnessPath, "test", command], {
        encoding: "utf8",
        env: { ...process.env, GH_AW_SKIP_IF_DECISION: '["Is work complete?"]', AWF_REFLECT_ENABLED: "1", GH_AW_PHASE: "agent", GH_AW_HARNESS_TELEMETRY_DIR: root, GH_AW_SAFE_OUTPUTS: path.join(root, "safeoutputs.jsonl") },
      });
      expect(result.status).toBe(0);
      const skipped = outcome === "deny";
      expect(result.stdout.includes("AGENT_INVOKED")).toBe(!skipped);
      const telemetry = JSON.parse(fs.readFileSync(path.join(root, "harness_retry.json"), "utf8"));
      expect(telemetry.attempts).toBe(skipped ? 0 : 1);
      expect(telemetry.decision_results).toHaveLength(1);
      expect(telemetry.decision_results[0]).toMatchObject({ outcome: outcome === "api-error" ? "unknown" : outcome, avoidedInvocation: skipped });
    } finally {
      fs.rmSync(root, { recursive: true, force: true });
    }
  });
});
