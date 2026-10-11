import { describe, it, expect } from "vitest";
import { createRequire } from "module";
import fs from "fs";

const req = createRequire(import.meta.url);
const { evaluateItem } = req("./evaluate_outcomes.cjs");
const cases = JSON.parse(fs.readFileSync(new URL("../../../pkg/cli/testdata/outcome_conformance.json", import.meta.url), "utf8"));

describe("shared Go/JS outcome conformance", () => {
  for (const fixture of cases) {
    it(fixture.name, () => {
      const api = endpoint => {
        const key = endpoint.replace("repos/acme/repo/", "");
        if (fixture.errors?.[key]) throw Object.assign(new Error("fixture API failure"), { status: fixture.errors[key] });
        return fixture.responses[key] ?? null;
      };
      const report = evaluateItem(fixture.item, "acme/repo", { ghAPI: api, nowMs: Date.parse("2026-01-03T00:00:00Z") });
      expect({ outcome_status: report.outcome_status, evidence_strength: report.evidence_strength, signal: report.signal }).toEqual(fixture.expected);
      if ("zero_touch" in fixture) expect(report.zero_touch).toBe(fixture.zero_touch);
    });
  }
});
