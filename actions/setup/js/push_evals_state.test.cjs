import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";

const FIXTURES_DIR = new URL("./fixtures/evals/", import.meta.url);
const RUN_FIXTURES = JSON.parse(fs.readFileSync(new URL("runs.json", FIXTURES_DIR), "utf8"));

const mockCore = {
  info: vi.fn(),
  warning: vi.fn(),
  setFailed: vi.fn(),
};

global.core = mockCore;

describe("push_evals_state.cjs", () => {
  let stateDir;
  let module;

  beforeEach(async () => {
    vi.clearAllMocks();
    stateDir = fs.mkdtempSync(path.join(os.tmpdir(), "evals-state-"));
    vi.stubEnv("GH_AW_STATE_DIR", stateDir);
    module = await import("./push_evals_state.cjs");
  });

  afterEach(() => {
    vi.unstubAllEnvs();
    fs.rmSync(stateDir, { recursive: true, force: true });
  });

  /**
   * Writes the downloaded evals artifact of an audited failed run: the
   * not-evaluated marker and execution evidence, but no answer file.
   * @param {string} runID
   */
  function stageNotEvaluatedArtifact(runID) {
    const run = RUN_FIXTURES[runID];
    const marker = {
      status: "not_evaluated",
      reason_code: "judge_not_started",
      reason: "The claude evals judge did not start: the sandbox failed before the engine harness started (execution step outcome: failure).",
      engine: run.engine,
      model: run.model,
      run_id: runID,
      run_attempt: run.run_attempt,
      timestamp: "2026-10-29T21:19:44.470Z",
      execution_outcome: "failure",
      execution_state: "not_started",
    };
    fs.writeFileSync(path.join(stateDir, "evals_status.json"), JSON.stringify(marker) + "\n");
    fs.mkdirSync(path.join(stateDir, "evals"), { recursive: true });
    fs.writeFileSync(path.join(stateDir, "evals", "execution.json"), JSON.stringify(run.execution) + "\n");
  }

  describe.each(["40955414354", "40955410097"])("pushEvalsState_skips_%s_not_evaluated_input", runID => {
    it.each([
      ["an existing evals branch with the cited successful rows", true],
      ["a first-ever evals run", false],
    ])("does not push answer history against %s", async (_description, branchExists) => {
      stageNotEvaluatedArtifact(runID);
      const remoteRows = branchExists ? fs.readFileSync(new URL(RUN_FIXTURES[runID].state_branch_rows, FIXTURES_DIR), "utf8") : null;
      const pushState = vi.fn(async () => {});

      await module.main({ pushState });

      expect(pushState).not.toHaveBeenCalled();
      expect(fs.existsSync(path.join(stateDir, "evals.jsonl"))).toBe(false);
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("judge_not_started"));
      if (remoteRows !== null) {
        // The writer never ran, so the remote history is untouched.
        expect(remoteRows).not.toContain(`"runid":"${runID}"`);
      }
    });
  });

  it("pushes ordinary answer files, including genuine judge UNKNOWN answers", async () => {
    const run = RUN_FIXTURES["40955414354"];
    fs.writeFileSync(path.join(stateDir, "evals.jsonl"), JSON.stringify({ ...run.questions[0], answer: "UNKNOWN", model: "evals", runid: "40955414354" }) + "\n");
    const pushState = vi.fn(async () => {});

    await module.main({ pushState });

    expect(pushState).toHaveBeenCalledTimes(1);
  });

  describe.each([
    ["missing reason_code", { status: "not_evaluated", reason: "The judge did not start." }],
    ["unknown reason_code", { status: "not_evaluated", reason_code: "judge_exploded", reason: "The judge did not start." }],
    ["non-string reason", { status: "not_evaluated", reason_code: "judge_not_started", reason: 42 }],
  ])("a malformed not-evaluated marker (%s)", (_description, marker) => {
    it("is rejected so the answers file is pushed", async () => {
      const { readEvalsNotEvaluatedStatus } = await import("./evals_status.cjs");
      const markerPath = path.join(stateDir, "evals_status.json");
      fs.writeFileSync(markerPath, JSON.stringify(marker) + "\n");
      const rows = JSON.stringify({ ...RUN_FIXTURES["40955414354"].questions[0], answer: "YES", model: "evals", runid: "40955414354" }) + "\n";
      fs.writeFileSync(path.join(stateDir, "evals.jsonl"), rows);
      let pushedRows = null;
      const pushState = vi.fn(async () => {
        pushedRows = fs.readFileSync(path.join(stateDir, "evals.jsonl"), "utf8");
      });

      expect(readEvalsNotEvaluatedStatus(markerPath)).toBeNull();
      await module.main({ pushState });

      expect(pushState).toHaveBeenCalledTimes(1);
      expect(pushedRows).toBe(rows);
    });
  });

  it("accepts a valid not-evaluated marker", async () => {
    const { readEvalsNotEvaluatedStatus } = await import("./evals_status.cjs");
    const markerPath = path.join(stateDir, "evals_status.json");
    fs.writeFileSync(markerPath, JSON.stringify({ status: "not_evaluated", reason_code: "judge_failed", reason: "The judge failed.", engine: "claude" }) + "\n");

    expect(readEvalsNotEvaluatedStatus(markerPath)).toEqual({ status: "not_evaluated", reason_code: "judge_failed", reason: "The judge failed.", engine: "claude" });
  });

  it("ignores a marker that is not a not-evaluated status", async () => {
    fs.writeFileSync(path.join(stateDir, "evals_status.json"), JSON.stringify({ status: "evaluated" }));
    const pushState = vi.fn(async () => {});

    await module.main({ pushState });

    expect(pushState).toHaveBeenCalledTimes(1);
  });
});
