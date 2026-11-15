// @ts-check

import { describe, it, expect, vi } from "vitest";
import { DECISIONS, restrictiveDecision, parseItemDecisions, evaluateItemDecision } from "./write_gate_decisions.cjs";
import { DEFECTS, runProperties } from "./write_gate_decisions.test_helpers.cjs";

describe("write gate item decisions", () => {
  it("enumerates all 564,480 cases and detects every seeded defect's required properties", () => {
    const result = runProperties();
    expect(result.count).toBe(564480);
    expect(result.failures).toEqual({});
    for (const [name, defect] of Object.entries(DEFECTS)) {
      expect(result.defects[name], name).toEqual(expect.arrayContaining(defect.required));
    }
  }, 60000);

  it("composes every pair and triple restrictively, with invalid values dropping", () => {
    for (let i = 0; i < DECISIONS.length; i++) {
      for (let j = 0; j < DECISIONS.length; j++) {
        expect(restrictiveDecision(DECISIONS[i], DECISIONS[j])).toBe(DECISIONS[Math.max(i, j)]);
        for (let k = 0; k < DECISIONS.length; k++) {
          expect(restrictiveDecision(DECISIONS[i], DECISIONS[j], DECISIONS[k])).toBe(DECISIONS[Math.max(i, j, k)]);
        }
      }
    }
    expect(restrictiveDecision("apply", "invalid")).toBe("drop");
  });

  it("poisons invalid duplicates and ignores unknown IDs with a warning", () => {
    const warn = vi.fn();
    const parsed = parseItemDecisions(
      [
        { output_id: "known", decision: "apply", reason: "clear" },
        { output_id: "known", decision: "hold", reason: "approval_required" },
        { output_id: "known", decision: "apply", reason: "clear" },
        { output_id: "poisoned", decision: "invalid", reason: "invalid" },
        { output_id: "poisoned", decision: "apply", reason: "clear" },
        { output_id: "unknown", decision: "apply", reason: "clear" },
      ],
      new Set(["known", "poisoned", "missing"]),
      warn
    );
    expect(parsed.get("known")).toEqual({ decision: "hold", reason: "approval_required" });
    expect(parsed.get("poisoned")).toEqual({ decision: "drop", reason: "gate_item_invalid" });
    expect(parsed.has("unknown")).toBe(false);
    expect(parsed.has("missing")).toBe(false);
    expect(warn).toHaveBeenCalledTimes(2);
    expect(() => parseItemDecisions({}, new Set(), warn)).toThrow("items array");
    expect(() => parseItemDecisions([{}], new Set(), warn)).toThrow("output_id");
  });

  it("keeps held-item replay subject to staging, warn veto, dependencies and review scope", () => {
    const input = { gate: "partial", entry: "hold", outcome: "apply", detection: "pass", manifestMatches: true, staged: false, reviewRepoUsable: false, dependenciesApplied: true, replay: "run", approverRecorded: false };
    expect(evaluateItemDecision(input).decision).toBe("hold");
    expect(evaluateItemDecision({ ...input, replay: "approved" }).decision).toBe("drop");
    expect(evaluateItemDecision({ ...input, replay: "approved", approverRecorded: true }).decision).toBe("apply");
    expect(evaluateItemDecision({ ...input, replay: "approved", approverRecorded: true, staged: true }).decision).toBe("stage");
    expect(evaluateItemDecision({ ...input, replay: "approved", approverRecorded: true, outcome: "review" }).decision).toBe("stage");
    expect(evaluateItemDecision({ ...input, replay: "approved", approverRecorded: true, detection: "warn-veto" }).decision).toBe("drop");
    expect(evaluateItemDecision({ ...input, replay: "approved", approverRecorded: true, dependenciesApplied: false }).decision).toBe("drop");
  });
});
