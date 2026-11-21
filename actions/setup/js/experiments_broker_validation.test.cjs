import { describe, expect, it } from "vitest";

const { LIMIT, assignments, decision, httpsUrl, parseJSON, specification } = require("./experiments_broker_validation.cjs");

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

  it("accepts every well-formed Unicode variant within the document limit", () => {
    const variants = ["", "line\nbreak", "\u0000", "é😀", "x".repeat(5000), "__proto__"];
    const spec = specification({ GH_AW_EXPERIMENT_SPEC: JSON.stringify({ feature: variants }) });
    for (const variant of variants) {
      const selected = assignments({ feature: variant }, spec, "INPUT");
      const counts = Object.fromEntries(variants.map(value => [value, 1]));
      expect(decision({ assignments: selected, counts: { feature: counts } }, spec).assignments.feature).toBe(variant);
    }
    expect(() => assignments({ feature: "\ud800" }, undefined, "INPUT")).toThrow();
    expect(() => specification({ GH_AW_EXPERIMENT_SPEC: JSON.stringify({ feature: ["ok", "\ud800"] }) })).toThrow();
    expect(() => specification({ GH_AW_EXPERIMENT_SPEC: JSON.stringify({ feature: ["ok", "x".repeat(LIMIT)] }) })).toThrow();
  });

  it("checks raw integer counter tokens without restricting fractional observations", () => {
    const paths = [
      value => `{"counts":{"feature":{"v":${value}}}}`,
      value => `{"run_id":${value}}`,
      ...Object.entries({
        firewall: ["total_requests", "allowed_requests", "blocked_requests"],
        session: ["total_events", "session_starts", "session_shutdowns", "turns", "assistant_messages", "reasoning_events", "tool_execution_starts", "tool_execution_completes", "failed_tool_executions"],
        gateway: ["total_calls", "failed_calls", "total_input_size", "total_output_size", "max_input_size", "max_output_size"],
        integrity: ["total_filtered"],
        safe_outputs: ["total_items"],
        ledger: ["transactions_added"],
        friction: ["counted_occurrences"],
      }).flatMap(([section, fields]) => fields.map(field => value => `{"${section}":{"${field}":${value}}}`)),
      ...["turns", "tool_calls"].map(field => value => `{"friction":{"cost":{"${field}":${value}}}}`),
      ...["input", "output", "cache_read", "cache_write", "reasoning", "total"].map(field => value => `{"friction":{"cost":{"tokens":{"${field}":${value}}}}}`),
    ];
    for (const build of paths) {
      for (const value of ["0", "1.0", "1e2", "9007199254740991", "900719925474099100e-2"]) expect(() => parseJSON(build(value))).not.toThrow();
      for (const value of ["1.0000000000000001", "1e-9999", "-1e-400", "9007199254740991.1", "9007199254740993", "1e400"]) expect(() => parseJSON(build(value))).toThrow();
    }
    expect(parseJSON('{"gateway":{"total_duration_ms":1.25},"friction":{"cost":{"aic":0.125,"latency_ms":1.0000000000000001}}}').gateway.total_duration_ms).toBe(1.25);
    expect(() => parseJSON('{"counts":{"feature":{"v":1,"\\u0076":2}}}')).toThrow();
    expect(() => parseJSON('{"x":1e400}')).toThrow();
  });
});
