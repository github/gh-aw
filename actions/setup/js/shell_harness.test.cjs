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

  it.each(["deny", "allow", "unknown", "api-error"].flatMap(outcome => ["command", "custom-harness", "pi-durable"].map(mode => [outcome, mode])))("gates the actual %s invocation through %s", (outcome, mode) => {
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
      const nativeSession = { type: "session", version: 3, id: "12345678-1234-1234-1234-123456789abc", timestamp: "2026-11-08T00:00:00Z", cwd: root };
      const durableDirectory = path.join(root, "durable");
      const nativeTranscript = JSON.stringify(nativeSession) + "\n";
      if (mode === "pi-durable") {
        fs.mkdirSync(durableDirectory);
        fs.writeFileSync(path.join(durableDirectory, "session.jsonl"), nativeTranscript);
      }
      fs.writeFileSync(customHarness, `process.stdout.write(${JSON.stringify("AGENT_INVOKED\n" + (mode === "pi-durable" ? nativeTranscript : ""))});`);
      const command = mode === "command" ? "printf AGENT_INVOKED" : `"${process.execPath}" "${customHarness}"`;
      const result = spawnSync(process.execPath, ["--require", preload, harnessPath, mode === "pi-durable" ? "pi" : "test", command], {
        encoding: "utf8",
        env: {
          ...process.env,
          GH_AW_SKIP_IF_DECISION: '["Is work complete?"]',
          AWF_REFLECT_ENABLED: "1",
          GH_AW_PHASE: "agent",
          GH_AW_HARNESS_TELEMETRY_DIR: root,
          GH_AW_SAFE_OUTPUTS: path.join(root, "safeoutputs.jsonl"),
          ...(mode === "pi-durable"
            ? {
                GH_AW_DURABLE_STORAGE: "artifact",
                GH_AW_DURABLE_ENGINE: "pi",
                GH_AW_DURABLE_SESSION_DIR: durableDirectory,
                GH_AW_DURABLE_RESUMED: "true",
                GH_AW_DURABLE_PARENT_RUN_ID: "100",
                GH_AW_TMP_DIR: root,
                GITHUB_RUN_ID: "123",
                GITHUB_RUN_ATTEMPT: "2",
              }
            : {}),
        },
      });
      expect(result.status).toBe(0);
      const skipped = outcome === "deny";
      expect(result.stdout.includes("AGENT_INVOKED")).toBe(!skipped);
      const telemetry = JSON.parse(fs.readFileSync(path.join(root, "harness_retry.json"), "utf8"));
      expect(telemetry.attempts).toBe(skipped ? 0 : 1);
      expect(telemetry.decision_results).toHaveLength(1);
      expect(telemetry.decision_results[0]).toMatchObject({ outcome: outcome === "api-error" ? "unknown" : outcome, avoidedInvocation: skipped });
      if (mode === "pi-durable") {
        expect(fs.readFileSync(path.join(durableDirectory, "session.jsonl"), "utf8")).toBe(nativeTranscript);
        const durableLog = path.join(root, "agent", "durable-session.jsonl");
        expect(fs.existsSync(durableLog)).toBe(!skipped);
        if (!skipped) expect(JSON.parse(fs.readFileSync(durableLog, "utf8")).data).toMatchObject({ status: "resumed", sessionId: nativeSession.id, parentRunId: "100" });
      }
    } finally {
      fs.rmSync(root, { recursive: true, force: true });
    }
  });

  it("rejects invalid durable preflight before evaluating a skip decision", () => {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), "shell-decision-preflight-"));
    try {
      const decisionMarker = path.join(root, "decision-called");
      const preload = path.join(root, "fetch.cjs");
      fs.writeFileSync(preload, `global.fetch = async () => { require("node:fs").writeFileSync(${JSON.stringify(decisionMarker)}, "called"); return new Response("{}"); };`);
      const result = spawnSync(process.execPath, ["--require", preload, harnessPath, "pi", "printf AGENT_INVOKED"], {
        encoding: "utf8",
        env: { ...process.env, GH_AW_SKIP_IF_DECISION: '["Is work complete?"]', GH_AW_DURABLE_STORAGE: "artifact", GH_AW_DURABLE_ENGINE: "pi", GH_AW_DURABLE_SESSION_DIR: root, GH_AW_DURABLE_RESUMED: "true" },
      });
      expect(result.status).toBe(1);
      expect(result.stderr).toContain("Restored durable Pi session transcript is missing");
      expect(result.stdout).not.toContain("AGENT_INVOKED");
      expect(fs.existsSync(decisionMarker)).toBe(false);
    } finally {
      fs.rmSync(root, { recursive: true, force: true });
    }
  });
});
