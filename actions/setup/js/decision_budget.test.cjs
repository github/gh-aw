// @ts-check
import { describe, expect, it } from "vitest";
import { createRequire } from "module";

const require = createRequire(import.meta.url);
const { resolveDecisionBudget, decisionReservation } = require("./decision_budget.cjs");

describe("resolved decision credit budget", () => {
  it("uses admission allowance over local defaults only after admission", () => {
    expect(resolveDecisionBudget({ GH_AW_ADMISSION_RESULT: "admitted", GH_AW_ADMISSION_MAX_AI_CREDITS: "12", GH_AW_MAX_AI_CREDITS: "1000" })).toBe(12);
    for (const result of ["skipped", "ignored", undefined]) {
      expect(resolveDecisionBudget({ GH_AW_ADMISSION_RESULT: result, GH_AW_ADMISSION_MAX_AI_CREDITS: "12", GH_AW_MAX_AI_CREDITS: "20" })).toBe(20);
    }
  });

  it.each([undefined, "", "0", "-1", "1.5", "Infinity", "2147483648", "01"])("rejects unresolved or invalid budget %s", value => {
    expect(() => resolveDecisionBudget({ GH_AW_MAX_AI_CREDITS: value })).toThrow("ERR_DECISION");
    expect(() => resolveDecisionBudget({ GH_AW_ADMISSION_RESULT: "admitted", GH_AW_ADMISSION_MAX_AI_CREDITS: value, GH_AW_MAX_AI_CREDITS: "1000" })).toThrow("ERR_DECISION");
  });

  it("reserves worst-case input, framing and capped output using the highest applicable rate", () => {
    const selection = { provider: "test", model: "test", type: "responses", pricing: { input: "0.000001", output: "0.000002", cache_write: "0.000003", reasoning: "0.000004" } };
    expect(decisionReservation(selection, 100, 64)).toBeCloseTo(((100 + 512) * 0.000003 + 64 * 0.000004) / 0.01);
    expect(decisionReservation({ ...selection, type: "decisions" }, 100, 64)).toBeCloseTo(((100 + 512) * 0.000003) / 0.01);
  });

  it.each([
    undefined,
    {},
    { input: 0, output: 0 },
    { input: 0.1 },
    { input: -1, output: 1 },
    { input: 1, output: null },
    { input: 1, output: "" },
    { input: 1, output: Infinity },
    { input: 1, output: 1, cache_write: "invalid" },
    { input: 1, output: 1, cache_write: -1 },
  ])("refuses unavailable or invalid pricing", pricing => {
    expect(decisionReservation({ provider: "test", model: "test", type: "chat", pricing }, 100, 64)).toBeNull();
  });
});
