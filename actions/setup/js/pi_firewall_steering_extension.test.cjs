import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { installPiFirewallSteering } from "./pi_firewall_steering_extension.cjs";

let directory;
let file;
let previousExitCode;
const denied = '{"_schema":"audit/v0.26.0","ts":1790899201.25,"host":"PRIVATE.example:443","status":403,"decision":"TCP_DENIED","url":"https://PRIVATE.example/?secret=PRIVATE"}\n';
beforeEach(() => {
  previousExitCode = process.exitCode;
  directory = fs.mkdtempSync(path.join(os.tmpdir(), "pi-firewall-steering-"));
  file = path.join(directory, "sandbox/firewall/audit/log.jsonl");
  fs.mkdirSync(path.dirname(file), { recursive: true });
  fs.writeFileSync(file, "");
});
afterEach(() => {
  process.exitCode = previousExitCode;
  fs.rmSync(directory, { recursive: true, force: true });
});
function harness(overrides = {}) {
  const handlers = {};
  const pi = {
    on: (name, handler) => {
      handlers[name] = handler;
    },
    sendUserMessage: vi.fn(),
  };
  const options = { env: { GH_AW_HARNESS_FIREWALL_STEERING: "1", GH_AW_TMP_DIR: directory }, remainingMs: () => 600000, minimumRemainingMs: 300000, log: vi.fn(), ...overrides };
  installPiFirewallSteering(pi, options);
  return { pi, handlers, options };
}
const turn = { message: { stopReason: "toolUse" }, toolResults: [{ toolName: "bash", content: denied }] };

describe("Pi Firewall Steering adapter", () => {
  it("steers only from fresh firewall evidence and never copies tainted log or tool text", async () => {
    const { pi, handlers, options } = harness();
    await handlers.turn_end(turn);
    expect(pi.sendUserMessage).not.toHaveBeenCalled();
    fs.appendFileSync(file, denied);
    await handlers.turn_end(turn);
    fs.appendFileSync(file, denied);
    await handlers.turn_end(turn);
    expect(pi.sendUserMessage).toHaveBeenCalledExactlyOnceWith(expect.stringContaining("firewall denied network access"), { deliverAs: "steer" });
    expect(JSON.stringify([...pi.sendUserMessage.mock.calls, ...options.log.mock.calls])).not.toContain("PRIVATE");
  });

  it.each([undefined, 300000, NaN])("respects the existing clock and completion reserve: %s", async remaining => {
    const { pi, handlers } = harness({ remainingMs: () => remaining });
    fs.appendFileSync(file, denied);
    await handlers.turn_end(turn);
    expect(pi.sendUserMessage).not.toHaveBeenCalled();
  });

  it.each(["stop", "error", "aborted"])("does not revive a terminal %s turn", async stopReason => {
    const { pi, handlers } = harness();
    fs.appendFileSync(file, denied);
    await handlers.turn_end({ ...turn, message: { stopReason } });
    expect(pi.sendUserMessage).not.toHaveBeenCalled();
  });

  it("cannot revive an execution stopped by a terminal tool guard", async () => {
    const { pi, handlers } = harness();
    fs.appendFileSync(file, denied);
    process.exitCode = 1;
    await handlers.turn_end(turn);
    expect(pi.sendUserMessage).not.toHaveBeenCalled();
  });

  it.each([{}, { GH_AW_HARNESS_FIREWALL_STEERING: "true" }, { GH_AW_HARNESS_FIREWALL_STEERING: "1", GH_AW_PHASE: "detection" }, { GH_AW_HARNESS_FIREWALL_STEERING: "1", GH_AW_PI_SUBAGENT_CHILD: "1" }])(
    "installs no hooks without an eligible opt-in: %j",
    env => {
      expect(harness({ env }).handlers).toEqual({});
    }
  );
});
