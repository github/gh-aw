import { describe, expect, it } from "vitest";

const { decision, httpsUrl, parseJSON, specification } = require("./experiments_broker_validation.cjs");

describe("experiments broker validation", () => {
  it("validates a decision against the configured variants", () => {
    const spec = specification({ GH_AW_EXPERIMENT_SPEC: '{"feature":["control","candidate"]}' });
    const result = { assignments: { feature: "candidate" }, counts: { feature: { control: 1, candidate: 2 } } };
    expect(decision(result, spec)).toEqual(result);
    expect(() => decision({ ...result, assignments: { feature: "unknown" } }, spec)).toThrow("Experiments broker response is invalid.");
    expect(() => decision({ ...result, counts: { feature: { control: -1, candidate: 2 } } }, spec)).toThrow("Experiments broker response is invalid.");
  });

  it("rejects malformed configuration and input", () => {
    expect(() => specification({ GH_AW_EXPERIMENT_SPEC: '{"feature":["control","control"]}' })).toThrow("Experiments broker configuration is invalid.");
    expect(() => httpsUrl("http://broker.example.test", "CONFIGURATION")).toThrow("Experiments broker configuration is invalid.");
    expect(() => parseJSON('{"duplicate":1,"duplicate":2}')).toThrow("Experiments broker input is invalid.");
  });
});
