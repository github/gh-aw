import { describe, expect, it, vi } from "vitest";
import { createFirewallSteeringPolicy, isFirewallSteeringEnabled } from "./harness_firewall_steering.cjs";

const event = { type: "firewall.observed", severity: "warning", count: 1, remainingMs: 600000 };
const context = { engine: "pi", capabilities: ["steer"] };
const make = () => createFirewallSteeringPolicy({ minimumRemainingMs: 10000, log: vi.fn() });

describe("Firewall Steering policy", () => {
  it("deduplicates warnings, escalates once to denial, and cannot grant network permissions", () => {
    const policy = make();
    expect(policy.onEvent(event, context)).toEqual([{ type: "steer", message: expect.stringContaining("network warning") }]);
    expect(policy.onEvent(event, context)).toBeUndefined();
    const actions = policy.onEvent({ ...event, severity: "denied" }, context);
    expect(actions).toEqual([{ type: "steer", message: expect.stringContaining("firewall denied network access") }]);
    expect(actions[0].message).toContain("Do not change firewall rules or request broader access");
    expect(actions[0].message).toContain("without accessing a denied resource by another route");
    expect(policy.onEvent(event, context)).toBeUndefined();
    expect(policy.onEvent({ ...event, severity: "denied" }, context)).toBeUndefined();
  });

  it("selects only the intervention supported by the adapter", () => {
    const policy = make();
    expect(policy.onEvent(event, { engine: "custom", capabilities: [] })).toBeUndefined();
    expect(policy.onEvent(event, { engine: "codex", capabilities: ["retry_guidance"] })).toEqual([{ type: "retry_guidance", message: expect.any(String) }]);
  });

  it.each([{ count: 0 }, { count: -1 }, { count: NaN }, { remainingMs: 10000 }, { remainingMs: Infinity }, { remainingMs: NaN }, { severity: "unknown" }, { type: "turn.completed" }])(
    "ignores unsupported or budget-limited observations without consuming the intervention: %j",
    override => {
      const policy = make();
      expect(policy.onEvent({ ...event, ...override }, context)).toBeUndefined();
      expect(policy.onEvent(event, context)).toHaveLength(1);
    }
  );

  it.each([{}, { GH_AW_HARNESS_FIREWALL_STEERING: "true" }, { GH_AW_HARNESS_FIREWALL_STEERING: "1", GH_AW_PHASE: "detection" }, { GH_AW_HARNESS_FIREWALL_STEERING: "1", GH_AW_PI_SUBAGENT_CHILD: "1" }])(
    "requires an eligible independent opt-in: %j",
    env => {
      expect(isFirewallSteeringEnabled(env)).toBe(false);
    }
  );
});
