import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "node:module";
import crypto from "node:crypto";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

const require = createRequire(import.meta.url);
const { bindContinuationEvidence, main } = require("./bind_continuation_evidence.cjs");

describe("bind_continuation_evidence", () => {
  let tempDir;
  let outputPath;
  let core;

  beforeEach(() => {
    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-continuation-evidence-"));
    outputPath = path.join(tempDir, "agent_output.json");
    core = { info: vi.fn(), warning: vi.fn(), setOutput: vi.fn() };
  });

  afterEach(() => {
    fs.rmSync(tempDir, { recursive: true, force: true });
  });

  const env = extra => ({ GH_AW_CONTINUATION_AGENT_OUTPUT: outputPath, GITHUB_RUN_ID: "123", GITHUB_RUN_ATTEMPT: "2", ...extra });

  it("binds the exact final bytes to the run attempt", async () => {
    const bytes = Buffer.from('{"items":[{"type":"request_continuation","message":"Summarize page 2"}],"errors":[]}');
    fs.writeFileSync(outputPath, bytes);
    await main({ core, env: env() });
    const digest = crypto.createHash("sha256").update(bytes).digest("hex");
    expect(core.setOutput).toHaveBeenCalledWith("continuation_evidence", `123:2:${digest}`);
    expect(core.setOutput.mock.calls[0][1]).toMatch(/^123:2:[0-9a-f]{64}$/);
    // The message never appears in the output value.
    expect(core.setOutput.mock.calls[0][1]).not.toContain("Summarize");
  });

  it("leaves the output unset for missing or empty output", async () => {
    await main({ core, env: env() });
    fs.writeFileSync(outputPath, "");
    await main({ core, env: env() });
    expect(core.setOutput).not.toHaveBeenCalled();
  });

  it("rejects symlinks and non-regular files", async () => {
    const target = path.join(tempDir, "other.json");
    fs.writeFileSync(target, "{}");
    fs.symlinkSync(target, outputPath);
    await main({ core, env: env() });
    expect(core.setOutput).not.toHaveBeenCalled();
    expect(core.warning).toHaveBeenCalled();
    fs.rmSync(outputPath);
    fs.mkdirSync(outputPath);
    expect(() => bindContinuationEvidence({ path: outputPath, runId: "1", runAttempt: "1" })).toThrow();
  });

  it("rejects an invalid run context", () => {
    fs.writeFileSync(outputPath, "{}");
    for (const [runId, runAttempt] of [
      ["", "1"],
      ["1", "0"],
      ["01", "1"],
      ["1", "x"],
    ]) {
      expect(() => bindContinuationEvidence({ path: outputPath, runId, runAttempt })).toThrow("continuation_evidence_context_invalid");
    }
  });
});
