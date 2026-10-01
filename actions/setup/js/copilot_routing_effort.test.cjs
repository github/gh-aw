import { describe, it, expect } from "vitest";
import { createRequire } from "module";

const require = createRequire(import.meta.url);
const { ROUTING_REASONING_EFFORTS, isRoutingReasoningEffort, resolveRoutingReasoningEffort } = require("./copilot_routing_effort.cjs");

describe("copilot_routing_effort.cjs", () => {
  it("matches the Copilot CLI --reasoning-effort possible values", () => {
    // Keep in sync with `copilot --help` for the pinned Copilot CLI version.
    expect(ROUTING_REASONING_EFFORTS).toEqual(["none", "minimal", "low", "medium", "high", "xhigh", "max"]);
    expect(Object.isFrozen(ROUTING_REASONING_EFFORTS)).toBe(true);
  });

  it("rejects unknown efforts", () => {
    expect(isRoutingReasoningEffort("extreme")).toBe(false);
    expect(isRoutingReasoningEffort("")).toBe(false);
    expect(isRoutingReasoningEffort(undefined)).toBe(false);
  });

  it.each(["none", "minimal", "low", "medium", "high", "xhigh", "max"])("forwards %s to the SDK session when routing is enabled", effort => {
    expect(resolveRoutingReasoningEffort({ GH_AW_MODEL_ROUTING: "1", GH_AW_COPILOT_ROUTING_EFFORT: effort })).toBe(effort);
  });

  it("does not forward an effort when routing is disabled or the effort is unknown or unset", () => {
    expect(resolveRoutingReasoningEffort({ GH_AW_COPILOT_ROUTING_EFFORT: "max" })).toBeUndefined();
    expect(resolveRoutingReasoningEffort({ GH_AW_MODEL_ROUTING: "1", GH_AW_COPILOT_ROUTING_EFFORT: "extreme" })).toBeUndefined();
    expect(resolveRoutingReasoningEffort({ GH_AW_MODEL_ROUTING: "1" })).toBeUndefined();
  });
});
