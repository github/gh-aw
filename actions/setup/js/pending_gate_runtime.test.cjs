import { afterEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import path from "node:path";
import os from "node:os";
import { createRequire } from "node:module";
const require = createRequire(import.meta.url);
const runtime = require("./pending_writes_runtime.cjs");
const verdict = require("./write_gate_verdict.cjs");
const { assertPendingWritesIntegrity } = require("./pending_writes.cjs");
const { publishWriteDecisionInventory } = require("./write_gate_runtime.cjs");

describe("compiler-owned pending gate handoff", () => {
  let directory;
  afterEach(() => {
    vi.unstubAllEnvs();
    vi.unstubAllGlobals();
    if (directory) fs.rmSync(directory, { recursive: true, force: true });
  });
  it("allows exact inputs and changes gate allow to deny/inputs_changed after an edit", async () => {
    directory = fs.mkdtempSync(path.join(os.tmpdir(), "pending-gate-integration-"));
    const root = path.join(directory, "gh-aw");
    fs.mkdirSync(root);
    const output = path.join(root, "agent_output.json");
    const manifest = path.join(root, "pending-writes.json");
    fs.writeFileSync(output, JSON.stringify({ items: [{ type: "create_issue", title: "original", body: "body" }] }));
    const outputs = {};
    vi.stubGlobal("core", {
      info: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
      debug: vi.fn(),
      setFailed: vi.fn(),
      setOutput: (name, value) => {
        outputs[name] = value;
      },
      exportVariable: (name, value) => vi.stubEnv(name, value),
    });
    vi.stubGlobal("context", { repo: { owner: "owner", repo: "repo" }, eventName: "issues", payload: { issue: { number: 1 } } });
    vi.stubEnv("RUNNER_TEMP", directory);
    vi.stubEnv("GITHUB_REPOSITORY", "owner/repo");
    vi.stubEnv("GITHUB_WORKFLOW_REF", "owner/repo/.github/workflows/test.md@main");
    vi.stubEnv("GITHUB_WORKFLOW_SHA", "a".repeat(40));
    vi.stubEnv("GITHUB_RUN_ID", "123");
    vi.stubEnv("GITHUB_RUN_ATTEMPT", "1");
    vi.stubEnv("GITHUB_EVENT_NAME", "issues");
    vi.stubEnv("GH_AW_AGENT_OUTPUT", output);
    vi.stubEnv("GH_AW_PENDING_WRITES", manifest);
    vi.stubEnv("GH_AW_PENDING_WRITES_SPEC", JSON.stringify({ bindings: [], publications: [] }));
    vi.stubEnv("GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG", JSON.stringify({ create_issue: {} }));
    await runtime.main();
    expect(global.core.setFailed).not.toHaveBeenCalled();
    vi.stubEnv("GH_AW_PENDING_WRITES_SHA256", outputs.sha256);
    vi.stubEnv("GH_AW_PENDING_WRITE_INPUTS_SHA256", outputs.pending_write_inputs_sha256);
    vi.stubEnv("GH_AW_PENDING_WRITES_BINDINGS", outputs.bindings);
    vi.stubEnv("GH_AW_PENDING_WRITES_REQUIRED", "true");
    expect(await runtime.verifyFromEnv()).toMatchObject({ allow: true });
    const frozenManifest = path.join(root, "pending-write-inputs", "pending-writes.json");
    const originalFrozenBytes = fs.readFileSync(frozenManifest);
    fs.writeFileSync(frozenManifest, "{}");
    expect(await runtime.verifyFromEnv()).toEqual({ allow: false, reason: "inputs_changed" });
    fs.writeFileSync(frozenManifest, originalFrozenBytes);
    const decisions = path.join(root, "decisions.json");
    fs.writeFileSync(decisions, '{"decision":"allow","reason":"checked"}');
    vi.stubEnv("GH_AW_WRITE_DECISIONS", decisions);
    vi.stubEnv("GH_AW_WRITE_GATE_STEP_OUTCOMES", '["success"]');
    vi.stubEnv("GH_AW_PENDING_INPUTS_MATCH", "true");
    verdict.main();
    expect(outputs.write_gate).toBe("allow");
    expect(outputs.write_gate_decisions_sha256).toBeUndefined();
    vi.stubEnv("GH_AW_VALIDATED_WRITE_GATE", outputs.write_gate);
    vi.stubEnv("GH_AW_VALIDATED_WRITE_GATE_REASON", outputs.write_gate_reason);
    vi.stubEnv("GH_AW_VALIDATED_WRITE_DECISIONS_SHA256", outputs.decisions_sha256);
    expect(publishWriteDecisionInventory().inventory.decision).toBe("allow");
    fs.writeFileSync(output, JSON.stringify({ items: [{ type: "create_issue", title: "edited", body: "body" }] }));
    expect(await runtime.verifyFromEnv()).toEqual({ allow: false, reason: "inputs_changed" });
    vi.stubEnv("GH_AW_PENDING_INPUTS_MATCH", "false");
    verdict.main();
    expect(outputs.write_gate).toBe("deny");
    expect(outputs.write_gate_reason).toBe("inputs_changed");
    expect(() => assertPendingWritesIntegrity()).toThrow("inputs_changed");
  });
});
