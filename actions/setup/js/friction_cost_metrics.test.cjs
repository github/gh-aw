import { describe, it, expect } from "vitest";

const {
  computeFrictionCost,
  parseInvocationsFromJSONL,
  classifyInvocationFriction,
  applyCausalGrouping,
  buildFrictionEvents,
  meanAndRelativeError,
  FRICTION_DRIVERS,
  COST_DIMENSIONS,
  MAX_FRICTION_EVENTS,
} = require("./friction_cost_metrics.cjs");

/**
 * @param {Array<Record<string, any>>} records
 */
const jsonl = records => records.map(record => JSON.stringify(record)).join("\n");

const invocation = (timestamp, aic, overrides = {}) => ({
  timestamp,
  input_tokens: 100,
  output_tokens: 20,
  cache_read_tokens: 5,
  cache_write_tokens: 3,
  reasoning_tokens: 2,
  ai_credits_this_response: aic,
  duration_ms: 500,
  ...overrides,
});

const failedCall = (id, timestamp, overrides = {}) => ({
  tool_call_id: id,
  timestamp,
  server_name: "github",
  tool_name: "issue_read",
  request_size: 100,
  response_size: 50,
  duration_ms: 250,
  outcome: "failure",
  ...overrides,
});

const successCall = (id, timestamp) => ({ ...failedCall(id, timestamp), outcome: "success" });

describe("friction cost driver matrix", () => {
  it("declares every cost dimension for every driver", () => {
    for (const [driver, definition] of Object.entries(FRICTION_DRIVERS)) {
      for (const dimension of COST_DIMENSIONS) {
        expect(definition.dimensions[dimension], `${driver}.${dimension}`).toBeDefined();
        expect(["measured", "derived", "unsupported"]).toContain(definition.dimensions[dimension]);
      }
      expect(definition.class).toBeTruthy();
      expect(definition.source).toBeTruthy();
    }
  });

  it("keeps AIC canonical: every detected driver either measures or derives AIC, or is explicitly unsupported", () => {
    expect(FRICTION_DRIVERS.agent_api_error.dimensions.aic).toBe("measured");
    expect(FRICTION_DRIVERS.mcp_tool_error.dimensions.aic).toBe("derived");
    expect(FRICTION_DRIVERS.firewall_block.dimensions.aic).toBe("derived");
  });
});

describe("token-usage invocation parsing", () => {
  it("parses token classes and orders invocations by timestamp", () => {
    const { invocations, ignoredRecords } = parseInvocationsFromJSONL(jsonl([invocation("2026-01-01T00:00:02Z", 0.2), invocation("2026-01-01T00:00:01Z", 0.1)]));
    expect(ignoredRecords).toBe(0);
    expect(invocations.map(entry => entry.aic)).toEqual([0.1, 0.2]);
    expect(invocations[0].tokens).toEqual({ input: 100, output: 20, cache_read: 5, cache_write: 3, reasoning: 2, total: 130 });
  });

  it("counts malformed records as ignored without throwing", () => {
    const { invocations, ignoredRecords } = parseInvocationsFromJSONL(`${JSON.stringify(invocation("2026-01-01T00:00:01Z", 0.1))}\nnot-json\n[1,2,3]\n`);
    expect(invocations).toHaveLength(1);
    expect(ignoredRecords).toBe(2);
  });

  it("treats a missing AIC field as unavailable rather than zero", () => {
    const { invocations } = parseInvocationsFromJSONL(jsonl([invocation("2026-01-01T00:00:01Z", undefined)]));
    expect(invocations[0].aic).toBeNull();
  });

  it.each([
    { name: "http status", entry: { status_code: 429 }, expected: "http_429" },
    { name: "string status", entry: { status: "error" }, expected: "status_error" },
    { name: "error string", entry: { error: "overloaded" }, expected: "error" },
    { name: "error object", entry: { error: { code: "x" } }, expected: "error" },
    { name: "retry flag", entry: { retry: true }, expected: "retry" },
    { name: "event name", entry: { event: "token_usage_retry" }, expected: "token_usage_retry" },
  ])("classifies $name as friction", ({ entry, expected }) => {
    expect(classifyInvocationFriction(entry)).toBe(expected);
  });

  it("classifies healthy invocations as non-friction", () => {
    expect(classifyInvocationFriction({ status: 200, event: "token_usage" })).toBe("");
  });
});

describe("statistics helpers", () => {
  it("returns a null relative error for single-sample means", () => {
    expect(meanAndRelativeError([4])).toEqual({ mean: 4, relativeError: null, sampleSize: 1 });
  });

  it("computes the relative standard error of the mean", () => {
    const { mean, relativeError, sampleSize } = meanAndRelativeError([2, 4]);
    expect(mean).toBe(3);
    expect(sampleSize).toBe(2);
    // sample stddev = sqrt(2), sem = 1, rse = 1/3
    expect(relativeError).toBeCloseTo(1 / 3, 10);
  });

  it("returns zero samples for an empty population", () => {
    expect(meanAndRelativeError([])).toEqual({ mean: null, relativeError: null, sampleSize: 0 });
  });
});

describe("measured friction (errored model invocations)", () => {
  it("attributes the errored invocation cost directly", () => {
    const { friction } = computeFrictionCost({
      tokenUsageContent: jsonl([invocation("2026-01-01T00:00:01Z", 0.5), invocation("2026-01-01T00:00:02Z", 0.25, { status_code: 500 })]),
    });
    expect(friction.measurement_state).toBe("measured");
    expect(friction.canonical_unit).toBe("aic");
    expect(friction.cost.aic).toBe(0.25);
    expect(friction).not.toHaveProperty("estimated_usd");
    expect(friction.total_run_aic).toBe(0.75);
    expect(friction.friction_ratio).toBeCloseTo(1 / 3, 10);
    expect(friction.cost.turns).toBe(1);
    expect(friction.cost.latency_ms).toBe(500);
    expect(friction.cost.tokens.total).toBe(130);
    expect(friction.uncertainty.aic).toMatchObject({ state: "measured", method: "direct_record", confidence: "high", relative_error: 0 });
    expect(friction.drivers).toHaveLength(1);
    expect(friction.drivers[0]).toMatchObject({ driver: "agent_api_error", counted_occurrences: 1, state: "measured" });
    expect(friction.events[0]).toMatchObject({
      id: "agent_api_error:1",
      detail: "http_500",
      state: "measured",
      attribution_class: "directly_measurable",
      estimation_method: "direct_record",
      lower_bound_aic: 0.25,
      upper_bound_aic: 0.25,
      confidence: "high",
    });
  });

  it("marks AIC unavailable when the errored record carries no credit field", () => {
    const { friction } = computeFrictionCost({
      tokenUsageContent: jsonl([invocation("2026-01-01T00:00:01Z", undefined, { status_code: 500 })]),
    });
    expect(friction.measurement_state).toBe("unavailable");
    expect(friction.cost.aic).toBe(0);
    expect(friction.cost.tokens.total).toBe(130);
    expect(friction.dimension_states.tokens).toBe("measured");
    expect(friction.unattributed_occurrences).toBe(1);
  });
});

describe("causal friction (linked follow-up invocations)", () => {
  it("attributes the next healthy invocation to a failed tool call", () => {
    const { friction } = computeFrictionCost({
      gateway: { tool_calls: [failedCall("call-1", "2026-01-01T00:00:01Z")] },
      tokenUsageContent: jsonl([invocation("2026-01-01T00:00:00Z", 0.4), invocation("2026-01-01T00:00:02Z", 0.3)]),
    });
    expect(friction.measurement_state).toBe("causal");
    expect(friction.cost.aic).toBe(0.3);
    expect(friction.cost.tool_calls).toBe(1);
    expect(friction.cost.latency_ms).toBe(250);
    expect(friction.linked_invocations).toBe(1);
    expect(friction.uncertainty.aic).toMatchObject({ state: "causal", method: "next_invocation_linkage", confidence: "medium", lower_bound: 0, upper_bound: 0.3 });
    expect(friction.events[0]).toMatchObject({
      attribution_class: "causally_estimable",
      lower_bound_aic: 0,
      upper_bound_aic: 0.3,
      confidence: "medium",
    });
    expect(friction.events[0].dimension_states).toMatchObject({ aic: "causal", tokens: "causal", tool_calls: "measured", latency_ms: "measured", turns: "unsupported" });
  });

  it("never attributes the same invocation to two friction events", () => {
    const { friction } = computeFrictionCost({
      gateway: {
        tool_calls: [failedCall("call-1", "2026-01-01T00:00:01Z"), failedCall("call-2", "2026-01-01T00:00:02Z")],
      },
      tokenUsageContent: jsonl([invocation("2026-01-01T00:00:03Z", 0.3), invocation("2026-01-01T00:00:04Z", 0.7)]),
    });
    expect(friction.linked_invocations).toBe(2);
    expect(friction.cost.aic).toBeCloseTo(1.0, 10);
    expect(friction.cost.tokens.total).toBe(260);
  });

  it("only links invocations that happened after the friction event", () => {
    const { friction } = computeFrictionCost({
      gateway: { tool_calls: [failedCall("call-1", "2026-01-01T00:00:09Z")] },
      tokenUsageContent: jsonl([invocation("2026-01-01T00:00:01Z", 0.4), invocation("2026-01-01T00:00:02Z", 0.6)]),
    });
    expect(friction.linked_invocations).toBe(0);
    expect(friction.measurement_state).toBe("statistical");
    expect(friction.cost.aic).toBeCloseTo(0.5, 10);
  });
});

describe("statistical friction (mean apportionment)", () => {
  it("apportions the mean healthy-invocation cost with a propagated relative error", () => {
    const { friction } = computeFrictionCost({
      integrity: { total_filtered: 2, filtered_tool_counts: { issue_read: 2 } },
      tokenUsageContent: jsonl([invocation("2026-01-01T00:00:01Z", 0.2), invocation("2026-01-01T00:00:02Z", 0.4)]),
    });
    expect(friction.measurement_state).toBe("statistical");
    // mean AIC = 0.3 per invocation, two filtered responses
    expect(friction.cost.aic).toBeCloseTo(0.6, 10);
    expect(friction.cost.tool_calls).toBe(2);
    expect(friction.uncertainty.aic.method).toBe("mean_invocation_apportionment");
    expect(friction.uncertainty.aic.sample_size).toBe(2);
    // all cost is statistical, so the aggregate relative error equals the baseline rse
    expect(friction.uncertainty.aic.relative_error).toBeCloseTo(1 / 3, 10);
    expect(friction.uncertainty.aic.confidence).toBe("low");
    expect(friction.uncertainty.aic.confidence_level).toBe(0.95);
    expect(friction.events[0].attribution_class).toBe("statistically_estimated");
  });

  it("reports medium confidence for tight statistical estimates", () => {
    const { friction } = computeFrictionCost({
      integrity: { total_filtered: 1, filtered_tool_counts: { issue_read: 1 } },
      tokenUsageContent: jsonl([invocation("2026-01-01T00:00:01Z", 0.5), invocation("2026-01-01T00:00:02Z", 0.52), invocation("2026-01-01T00:00:03Z", 0.51)]),
    });
    expect(friction.uncertainty.aic.confidence).toBe("medium");
    expect(friction.uncertainty.aic.relative_error).toBeLessThan(0.25);
  });

  it("dilutes the relative error when part of the cost is measured", () => {
    const { friction } = computeFrictionCost({
      integrity: { total_filtered: 1, filtered_tool_counts: { issue_read: 1 } },
      tokenUsageContent: jsonl([invocation("2026-01-01T00:00:01Z", 2), invocation("2026-01-01T00:00:02Z", 4), invocation("2026-01-01T00:00:03Z", 9, { status: "error" })]),
    });
    // measured 9 (errored invocation) + statistical 3 (mean of 2 and 4)
    expect(friction.cost.aic).toBeCloseTo(12, 10);
    expect(friction.measurement_state).toBe("statistical");
    expect(friction.uncertainty.aic.relative_error).toBeCloseTo((3 * (1 / 3)) / 12, 10);
  });
});

describe("unavailable friction", () => {
  it("reports unavailable when no data source produced a summary", () => {
    const { friction } = computeFrictionCost({});
    expect(friction.measurement_state).toBe("unavailable");
    expect(friction.sources).toEqual([]);
    expect(friction.total_events).toBe(0);
    expect(friction.cost.aic).toBe(0);
  });

  it("reports measured zero friction when sources exist but nothing went wrong", () => {
    const { friction } = computeFrictionCost({
      gateway: { total_calls: 1, tool_calls: [successCall("call-1", "2026-01-01T00:00:01Z")] },
      tokenUsageContent: jsonl([invocation("2026-01-01T00:00:02Z", 0.3)]),
    });
    expect(friction.measurement_state).toBe("measured");
    expect(friction.total_events).toBe(0);
    expect(friction.cost.aic).toBe(0);
    expect(friction.dimension_states).toEqual({ aic: "measured", tokens: "measured", turns: "measured", tool_calls: "measured", latency_ms: "measured" });
  });

  it("detects firewall blocks but reports unavailable cost without invocation evidence", () => {
    const { friction } = computeFrictionCost({
      firewall: { total_requests: 3, blocked_requests: 2, requests_by_domain: { "blocked.example": { allowed: 0, blocked: 2 }, "ok.example": { allowed: 1, blocked: 0 } } },
    });
    expect(friction.measurement_state).toBe("unavailable");
    expect(friction.total_occurrences).toBe(2);
    expect(friction.cost.aic).toBe(0);
    expect(friction.events).toHaveLength(1);
    expect(friction.events[0]).toMatchObject({ driver: "firewall_block", label: "blocked.example", occurrences: 2, state: "unavailable" });
    expect(friction.events[0].dimension_states).toEqual({ aic: "unavailable", tokens: "unavailable", turns: "unsupported", tool_calls: "unsupported", latency_ms: "unsupported" });
  });

  it("statistically estimates firewall recovery cost when invocation evidence is available", () => {
    const { friction } = computeFrictionCost({
      firewall: { total_requests: 1, blocked_requests: 1, requests_by_domain: { "blocked.example": { allowed: 0, blocked: 1 } } },
      tokenUsageContent: jsonl([invocation("2026-01-01T00:00:01Z", 0.4), invocation("2026-01-01T00:00:02Z", 0.6)]),
    });
    expect(friction.measurement_state).toBe("statistical");
    expect(friction.cost.aic).toBeCloseTo(0.5, 10);
    expect(friction.drivers[0]).toMatchObject({ driver: "firewall_block", state: "statistical" });
    expect(friction.events[0]).toMatchObject({ attribution_class: "statistically_estimated", estimation_method: "mean_invocation_apportionment" });
  });

  it("recognizes the numeric status field used by token-usage records", () => {
    const { friction } = computeFrictionCost({
      tokenUsageContent: jsonl([invocation("2026-01-01T00:00:01Z", 0.2, { status: 500 })]),
    });
    expect(friction.measurement_state).toBe("measured");
    expect(friction.events[0]).toMatchObject({ driver: "agent_api_error", label: "http_500" });
  });

  it("rounds fractional token estimates and latency to the integer artifact contract", () => {
    const { friction } = computeFrictionCost({
      gateway: { tool_calls: [failedCall("call-1", "2026-01-01T00:00:09Z", { duration_ms: 12.5 })] },
      tokenUsageContent: jsonl([invocation("2026-01-01T00:00:01Z", 0.4, { input_tokens: 100 }), invocation("2026-01-01T00:00:02Z", 0.6, { input_tokens: 101 })]),
    });
    expect(friction.cost.latency_ms).toBe(13);
    expect(parseInvocationsFromJSONL(jsonl([invocation("2026-01-01T00:00:01Z", 0.2, { duration_ms: 12.5 })])).invocations[0].durationMs).toBe(13);
    for (const tokenClass of ["input", "output", "cache_read", "cache_write", "reasoning", "total"]) {
      expect(Number.isInteger(friction.cost.tokens[tokenClass])).toBe(true);
    }
    expect(friction.cost.tokens.total).toBe(["input", "output", "cache_read", "cache_write", "reasoning"].reduce((sum, key) => sum + friction.cost.tokens[key], 0));
  });

  it("preserves fractional token estimates across many statistically attributed events", () => {
    const requestsByDomain = {};
    for (let index = 0; index < 100; index += 1) {
      requestsByDomain[`domain-${String(index).padStart(3, "0")}.example`] = { blocked: 1 };
    }
    const healthyInvocations = [
      invocation("2026-01-01T00:00:01Z", 0.1, { input_tokens: 0 }),
      invocation("2026-01-01T00:00:02Z", 0.1, { input_tokens: 0 }),
      invocation("2026-01-01T00:00:03Z", 0.1, { input_tokens: 0 }),
      invocation("2026-01-01T00:00:04Z", 0.1, { input_tokens: 0 }),
      invocation("2026-01-01T00:00:05Z", 0.1, { input_tokens: 2 }),
    ];
    const { friction } = computeFrictionCost({ firewall: { requests_by_domain: requestsByDomain }, tokenUsageContent: jsonl(healthyInvocations) });
    const eventInputTotal = friction.events.reduce((sum, event) => sum + event.cost.tokens.input, 0);
    expect(friction.cost.tokens.input).toBe(40);
    expect(eventInputTotal).toBe(40);
    expect(friction.drivers[0].cost.tokens.input).toBe(40);
    for (const tokenClass of ["input", "output", "cache_read", "cache_write", "reasoning"]) {
      expect(friction.cost.tokens[tokenClass]).toBe(friction.events.reduce((sum, event) => sum + event.cost.tokens[tokenClass], 0));
      expect(friction.cost.tokens[tokenClass]).toBe(friction.drivers[0].cost.tokens[tokenClass]);
    }
    expect(friction.cost.tokens.total).toBe(["input", "output", "cache_read", "cache_write", "reasoning"].reduce((sum, key) => sum + friction.cost.tokens[key], 0));
  });

  it("includes unavailable counted events in aggregate measurement states", () => {
    const { friction } = computeFrictionCost({
      firewall: { requests_by_domain: { "blocked.example": { blocked: 3 } } },
      tokenUsageContent: jsonl([invocation("2026-01-01T00:00:01Z", 1, { status_code: 500 })]),
    });
    expect(friction.measurement_state).toBe("unavailable");
    expect(friction.dimension_states.aic).toBe("unavailable");
    expect(friction.drivers.find(driver => driver.driver === "firewall_block").state).toBe("unavailable");
  });

  it("bounds group event IDs to the emitted event list", () => {
    const requestsByDomain = {};
    for (let index = 0; index < MAX_FRICTION_EVENTS + 5; index += 1) {
      requestsByDomain[`domain-${String(index).padStart(4, "0")}.example`] = { blocked: 1 };
    }
    const { friction } = computeFrictionCost({ firewall: { requests_by_domain: requestsByDomain } });
    const emittedIDs = new Set(friction.events.map(event => event.id));
    expect(friction.groups.flatMap(group => group.event_ids).every(id => emittedIDs.has(id))).toBe(true);
    expect(friction.groups.reduce((sum, group) => sum + group.event_ids.length, 0)).toBe(MAX_FRICTION_EVENTS);
    expect(friction.groups[0].event_ids_truncated).toBe(true);
  });

  it("marks groups whose events are entirely beyond the event listing limit", () => {
    const failedInvocations = Array.from({ length: MAX_FRICTION_EVENTS }, (_, index) => invocation(`2026-01-01T00:00:${String(index).padStart(2, "0")}Z`, 0.2, { status_code: 500 }));
    const { friction } = computeFrictionCost({
      firewall: { requests_by_domain: { "blocked.example": { blocked: 1 } } },
      tokenUsageContent: jsonl(failedInvocations),
    });
    const firewallGroup = friction.groups.find(group => group.group_id === "network_block");
    expect(friction.events).toHaveLength(MAX_FRICTION_EVENTS);
    expect(firewallGroup.event_ids).toEqual([]);
    expect(firewallGroup.event_ids_truncated).toBe(true);
  });

  it("marks run totals partial when valid invocations omit AIC", () => {
    const { friction } = computeFrictionCost({
      tokenUsageContent: jsonl([invocation("2026-01-01T00:00:01Z", 0.2, { status_code: 500 }), invocation("2026-01-01T00:00:02Z", undefined)]),
    });
    expect(friction.total_run_aic).toBe(0.2);
    expect(friction.total_run_aic_partial).toBe(true);
    expect(friction.friction_ratio).toBe(1);
  });

  it("lists drivers that could not be measured with a reason", () => {
    const { friction } = computeFrictionCost({ firewall: { requests_by_domain: { "blocked.example": { allowed: 0, blocked: 1 } } } });
    const reasons = Object.fromEntries(friction.unmeasured_drivers.map(entry => [entry.driver, entry.reason]));
    expect(reasons.mcp_tool_error).toBe("source_unavailable:mcp_gateway");
    expect(reasons.agent_api_error).toBe("source_unavailable:agent_token_usage");
    expect(reasons.firewall_block).toBeUndefined();
  });
});

describe("deterministic causal grouping", () => {
  it("suppresses session tool failures already reported by the gateway", () => {
    const { friction } = computeFrictionCost({
      gateway: { tool_calls: [failedCall("call-1", "2026-01-01T00:00:01Z"), failedCall("call-2", "2026-01-01T00:00:02Z")] },
      session: { total_events: 10, failed_tool_executions: 3 },
      tokenUsageContent: jsonl([invocation("2026-01-01T00:00:05Z", 0.1), invocation("2026-01-01T00:00:06Z", 0.1), invocation("2026-01-01T00:00:07Z", 0.1)]),
    });
    const group = friction.groups.find(entry => entry.group_id === "tool_failure");
    expect(group).toMatchObject({
      primary_source: "mcp_gateway",
      total_occurrences: 5,
      counted_occurrences: 3,
      suppressed_occurrences: 2,
    });
    expect(friction.counted_occurrences).toBe(3);
    expect(friction.cost.tool_calls).toBe(3);
    const sessionEvent = friction.events.find(entry => entry.driver === "session_tool_failure");
    expect(sessionEvent).toMatchObject({ occurrences: 3, counted_occurrences: 1, suppressed_occurrences: 2, suppressed_by: "causal-group:tool_failure" });
  });

  it("fully suppresses a lower-fidelity source that adds no new occurrences", () => {
    const { friction } = computeFrictionCost({
      gateway: { tool_calls: [failedCall("call-1", "2026-01-01T00:00:01Z"), failedCall("call-2", "2026-01-01T00:00:02Z")] },
      session: { total_events: 4, failed_tool_executions: 2 },
    });
    const sessionEvent = friction.events.find(entry => entry.driver === "session_tool_failure");
    expect(sessionEvent).toMatchObject({ counted_occurrences: 0, suppressed_occurrences: 2 });
    expect(friction.counted_occurrences).toBe(2);
    expect(friction.cost.tool_calls).toBe(2);
  });

  it("keeps independent causal classes separate", () => {
    const { friction } = computeFrictionCost({
      gateway: { tool_calls: [failedCall("call-1", "2026-01-01T00:00:01Z")] },
      integrity: { total_filtered: 1, filtered_tool_counts: { issue_read: 1 } },
      firewall: { requests_by_domain: { "blocked.example": { allowed: 0, blocked: 1 } } },
    });
    expect(friction.groups.map(group => group.group_id)).toEqual(["integrity_filter", "network_block", "tool_failure"]);
    expect(friction.suppressed_occurrences).toBe(0);
  });

  it("produces byte-identical output for identical inputs regardless of input ordering", () => {
    const build = calls =>
      computeFrictionCost({
        gateway: { tool_calls: calls },
        integrity: { total_filtered: 2, filtered_tool_counts: { issue_read: 1, list_issues: 1 } },
        tokenUsageContent: jsonl([invocation("2026-01-01T00:00:05Z", 0.2), invocation("2026-01-01T00:00:06Z", 0.3)]),
      }).friction;
    const forward = build([failedCall("call-1", "2026-01-01T00:00:01Z"), failedCall("call-2", "2026-01-01T00:00:02Z")]);
    const reversed = build([failedCall("call-2", "2026-01-01T00:00:02Z"), failedCall("call-1", "2026-01-01T00:00:01Z")]);
    expect(JSON.stringify(reversed)).toBe(JSON.stringify(forward));
  });

  it("orders drivers, groups and events deterministically", () => {
    const { friction } = computeFrictionCost({
      gateway: { tool_calls: [failedCall("call-2", "2026-01-01T00:00:02Z"), failedCall("call-1", "2026-01-01T00:00:01Z")] },
      session: { failed_tool_executions: 4 },
      firewall: { requests_by_domain: { "zebra.example": { blocked: 1 }, "alpha.example": { blocked: 1 } } },
    });
    expect(friction.drivers.map(driver => driver.driver)).toEqual(["firewall_block", "mcp_tool_error", "session_tool_failure"]);
    expect(friction.events.filter(event => event.driver === "firewall_block").map(event => event.label)).toEqual(["alpha.example", "zebra.example"]);
    expect(friction.events.filter(event => event.driver === "mcp_tool_error").map(event => event.id)).toEqual(["mcp_tool_error:call-1", "mcp_tool_error:call-2"]);
  });
});

describe("event listing limits", () => {
  it("truncates the event list while keeping aggregates complete", () => {
    const requestsByDomain = {};
    for (let index = 0; index < MAX_FRICTION_EVENTS + 5; index += 1) {
      requestsByDomain[`domain-${String(index).padStart(4, "0")}.example`] = { allowed: 0, blocked: 1 };
    }
    const { friction } = computeFrictionCost({
      firewall: { requests_by_domain: requestsByDomain },
      tokenUsageContent: jsonl([invocation("2026-01-01T00:00:01Z", 0.5)]),
    });
    expect(friction.total_events).toBe(MAX_FRICTION_EVENTS + 5);
    expect(friction.events).toHaveLength(MAX_FRICTION_EVENTS);
    expect(friction.events_truncated).toBe(true);
    expect(friction.total_occurrences).toBe(MAX_FRICTION_EVENTS + 5);
    expect(friction.cost.tokens.input).toBe((MAX_FRICTION_EVENTS + 5) * 100);
    expect(friction.events.reduce((sum, event) => sum + event.cost.tokens.input, 0)).toBe(MAX_FRICTION_EVENTS * 100);
  });
});

describe("building blocks", () => {
  it("builds one event per failed tool call and skips successful calls", () => {
    const events = buildFrictionEvents({
      gateway: { tool_calls: [successCall("call-1", "2026-01-01T00:00:01Z"), failedCall("call-2", "2026-01-01T00:00:02Z")] },
      integrity: null,
      session: null,
      firewall: null,
      invocations: [],
    });
    expect(events).toHaveLength(1);
    expect(events[0]).toMatchObject({ id: "mcp_tool_error:call-2", label: "github/issue_read", latencyMs: 250 });
  });

  it("groups by causal class and reports the dedupe rule", () => {
    const groups = applyCausalGrouping([
      { id: "a", driver: "mcp_tool_error", source: "mcp_gateway", group_id: "tool_failure", label: "x", timestampMs: null, occurrences: 1, counted_occurrences: 1, suppressed_occurrences: 0, latencyMs: 0 },
      { id: "b", driver: "session_tool_failure", source: "agent_session", group_id: "tool_failure", label: "y", timestampMs: null, occurrences: 1, counted_occurrences: 1, suppressed_occurrences: 0, latencyMs: 0 },
    ]);
    expect(groups).toHaveLength(1);
    expect(groups[0]).toMatchObject({ group_id: "tool_failure", primary_source: "mcp_gateway", counted_occurrences: 1, suppressed_occurrences: 1 });
    expect(groups[0].rule).toContain("highest-fidelity");
  });
});

describe("robustness", () => {
  it("ignores malformed token-usage records and reports a warning", () => {
    const { friction, warnings } = computeFrictionCost({
      gateway: { tool_calls: [failedCall("call-1", "2026-01-01T00:00:01Z")] },
      tokenUsageContent: `garbage\n${JSON.stringify(invocation("2026-01-01T00:00:02Z", 0.4))}`,
    });
    expect(warnings[0]).toContain("ignored 1 malformed");
    expect(friction.ignored_token_records).toBe(1);
    expect(friction.cost.aic).toBe(0.4);
    expect(friction).not.toHaveProperty("total_run_aic");
    expect(friction).not.toHaveProperty("friction_ratio");
  });

  it("tolerates missing and malformed activity sections", () => {
    const { friction } = computeFrictionCost({ gateway: { tool_calls: null }, integrity: {}, session: {}, firewall: {} });
    expect(friction.total_events).toBe(0);
    expect(friction.sources).toEqual(["agent_session", "firewall", "mcp_gateway"]);
  });

  it("falls back to file order when timestamps are absent", () => {
    const { friction } = computeFrictionCost({
      gateway: { tool_calls: [failedCall("call-1", "")] },
      tokenUsageContent: jsonl([invocation(undefined, 0.5), invocation(undefined, 0.7)]),
    });
    expect(friction.linked_invocations).toBe(0);
    expect(friction.measurement_state).toBe("statistical");
    expect(friction.cost.aic).toBeCloseTo(0.6, 10);
  });
});
