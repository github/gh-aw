import { afterEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { buildCopilotSDKRecoveryHooks } from "./copilot_sdk_recovery.cjs";

const main = { sessionId: "main" };
const failed = { ...main, toolName: "bash", toolArgs: { private: "PRIVATE" }, error: "PRIVATE failure" };
const success = { ...failed, toolResult: { resultType: "success", textResultForLlm: "PRIVATE result" } };
let directory;
afterEach(() => {
  if (directory) fs.rmSync(directory, { recursive: true, force: true });
  directory = undefined;
});
function harness(overrides = {}) {
  const options = {
    env: { GH_AW_HARNESS_RECOVERY: "1" },
    getSessionId: () => main.sessionId,
    remainingMs: () => 600000,
    isActive: () => true,
    decide: vi.fn().mockResolvedValue("change_strategy"),
    log: vi.fn(),
    ...overrides,
  };
  return { hooks: buildCopilotSDKRecoveryHooks(options), options };
}
async function failures(hooks, count = 3) {
  let result;
  for (let index = 0; index < count; index++) result = await hooks.onPostToolUseFailure(failed, main);
  return result;
}

describe("Copilot SDK Recovery hooks", () => {
  it.each([false, true])("composes independent Firewall Steering with Recovery=%s without exposing logs or modifying tool results", async recovery => {
    directory = fs.mkdtempSync(path.join(os.tmpdir(), "sdk-firewall-steering-"));
    const file = path.join(directory, "sandbox/firewall/audit/log.jsonl");
    fs.mkdirSync(path.dirname(file), { recursive: true });
    fs.writeFileSync(file, "");
    const { hooks, options } = harness({ env: { GH_AW_HARNESS_FIREWALL_STEERING: "1", GH_AW_HARNESS_RECOVERY: recovery ? "1" : "", GH_AW_TMP_DIR: directory } });
    await failures(hooks, 2);
    fs.appendFileSync(file, '{"_schema":"audit/v0.26.0","ts":1790899201.25,"host":"PRIVATE.example:443","status":403,"decision":"TCP_DENIED","url":"https://PRIVATE.example/?secret=PRIVATE"}\n');
    expect(await hooks.onPostToolUseFailure({ ...failed, sessionId: "child" }, { sessionId: "child" })).toBeUndefined();
    const result = await hooks.onPostToolUseFailure(failed, main);
    expect(Object.keys(result)).toEqual(["additionalContext"]);
    expect(result.additionalContext).toContain("firewall denied network access");
    expect(result.additionalContext.includes("Repeated failures")).toBe(recovery);
    expect(JSON.stringify(result)).not.toContain("PRIVATE");
    expect(options.decide).toHaveBeenCalledTimes(recovery ? 1 : 0);
    expect(await hooks.onPostToolUse(success, main)).toBeUndefined();
    expect(success.toolResult).toEqual({ resultType: "success", textResultForLlm: "PRIVATE result" });
  });

  it.each([{}, { GH_AW_HARNESS_RECOVERY: "true" }, { GH_AW_HARNESS_RECOVERY: "1", GH_AW_PHASE: "detection" }])("does not install without eligible opt-in: %j", env => {
    expect(harness({ env }).hooks).toBeUndefined();
  });

  it("adds only native additionalContext after repeated failures, with bounded sanitized evidence", async () => {
    const { hooks, options } = harness();
    expect(await failures(hooks)).toEqual({ additionalContext: expect.stringContaining("never bypass a denial") });
    expect(options.decide).toHaveBeenCalledExactlyOnceWith({ consecutiveFailures: 3, decision: 1, remainingMs: 600000 });
    expect(JSON.stringify(options.decide.mock.calls)).not.toContain("PRIVATE");
    await failures(hooks, 30);
    expect(options.decide).toHaveBeenCalledTimes(2);
  });

  it.each(["success", "unknown"])("resets the streak on %s outcomes", async resultType => {
    const { hooks, options } = harness();
    await failures(hooks, 2);
    await hooks.onPostToolUse({ ...success, toolResult: { ...success.toolResult, resultType } }, main);
    await failures(hooks, 2);
    expect(options.decide).not.toHaveBeenCalled();
  });

  it("ignores child and uncorrelated sessions rather than sharing their failure streak", async () => {
    const { hooks, options } = harness();
    for (let index = 0; index < 3; index++) {
      await hooks.onPostToolUseFailure({ ...failed, sessionId: "child" }, main);
      await hooks.onPostToolUseFailure(failed, { sessionId: "child" });
    }
    expect(options.decide).not.toHaveBeenCalled();
  });

  it.each([{ isActive: () => false }, { getSessionId: () => undefined }, { remainingMs: () => 15000 }, { remainingMs: () => NaN }])("preserves terminal guards, session correlation and completion budget: %j", async overrides => {
    const { hooks, options } = harness(overrides);
    expect(await failures(hooks)).toBeUndefined();
    expect(options.decide).not.toHaveBeenCalled();
  });

  it.each(["guard", "timeout"])("drops a result arriving after the %s limit", async limit => {
    let active = true;
    let remaining = 600000;
    const { hooks } = harness({
      isActive: () => active,
      remainingMs: () => remaining,
      decide: async () => {
        if (limit === "guard") active = false;
        else remaining = 10000;
        return "change_strategy";
      },
    });
    expect(await failures(hooks)).toBeUndefined();
  });

  it("does not leak guidance between overlapping hooks or act on a stale streak", async () => {
    let resolve;
    const { hooks, options } = harness({
      decide: vi.fn(
        () =>
          new Promise(done => {
            resolve = done;
          })
      ),
    });
    await failures(hooks, 2);
    const pending = hooks.onPostToolUseFailure(failed, main);
    expect(await hooks.onPostToolUse(success, main)).toBeUndefined();
    resolve("change_strategy");
    expect(await pending).toBeUndefined();
    expect(options.decide).toHaveBeenCalledOnce();
  });
});
