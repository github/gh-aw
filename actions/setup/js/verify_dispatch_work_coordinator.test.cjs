import { afterEach, beforeEach, describe, expect, it } from "vitest";
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { verifyCoordinatorFinishIntent } = require("./verify_dispatch_work_coordinator.cjs");

describe("verifyCoordinatorFinishIntent", () => {
  let tempDir;
  let safeOutputsPath;
  let finishIntentPath;

  beforeEach(() => {
    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "verify-dispatch-work-coordinator-"));
    safeOutputsPath = path.join(tempDir, "safeoutputs.jsonl");
    finishIntentPath = path.join(tempDir, "finish.jsonl");
  });

  afterEach(() => {
    fs.rmSync(tempDir, { recursive: true, force: true });
  });

  it("accepts a completed finish intent", () => {
    fs.writeFileSync(safeOutputsPath, '{"type":"noop"}\n');
    fs.writeFileSync(finishIntentPath, '{"outcome":"completed"}\n');

    expect(() => verifyCoordinatorFinishIntent({ safeOutputsPath, finishIntentPath })).not.toThrow();
  });

  it("allows a reported failure issue without a finish intent", () => {
    fs.writeFileSync(safeOutputsPath, '{"type":"create_issue"}\n');

    expect(() => verifyCoordinatorFinishIntent({ safeOutputsPath, finishIntentPath })).not.toThrow();
  });

  it("rejects a missing finish intent", () => {
    fs.writeFileSync(safeOutputsPath, '{"type":"noop"}\n');

    expect(() => verifyCoordinatorFinishIntent({ safeOutputsPath, finishIntentPath })).toThrow();
  });

  it("rejects an incomplete finish intent", () => {
    fs.writeFileSync(safeOutputsPath, '{"type":"noop"}\n');
    fs.writeFileSync(finishIntentPath, '{"outcome":"failed"}\n');

    expect(() => verifyCoordinatorFinishIntent({ safeOutputsPath, finishIntentPath })).toThrow("Dispatch coordinator finish intent artifact is missing or incomplete");
  });
});
