const fs = require("node:fs");
const path = require("node:path");
const cp = require("node:child_process");
const { computeGraders, runBuiltinGrader, createSessionLookups } = require("./trace_graders.cjs");
const { normalizeUnifiedSessionEvent } = require("./unified_session_payload.cjs");
const { parseEngineSession, mergeSessionSources } = require("./unified_session.cjs");

const GRADER = "end-to-end-lineage-completeness";
const config = { mode: "decision-integrity" };
const event = (type, data, phase = "agent") => ({ type, data, provenance: { component: "agent", phase, path: "agent-session.jsonl", index: 0 } });

function fixture() {
  return {
    events: [
      { index: 11, kind: "tool_call", ref: "call" },
      { index: 22, kind: "observation", ref: "result" },
      { index: 33, kind: "safe_output", ref: "accept" },
    ],
    actions: [{ id: "accept", eventIndex: 33, consequential: true }],
    toolCalls: [{ id: "call", eventIndex: 11, name: "bash", outputRef: "result", success: true }],
    observations: [{ id: "result", eventIndex: 22, sourceToolCallId: "call" }],
    provenanceEdges: [{ from: "result", to: "accept", relation: "informed" }],
    decisionLineage: {
      version: 1,
      complete: true,
      decisions: [{ id: "d", actionId: "accept", eventIndex: 33, revisionId: "r1", requiredValidations: ["unit"], validationIds: ["v"] }],
      validations: [{ id: "v", toolCallId: "call", resultObservationId: "result", revisionId: "r1", requirement: "unit", outcome: "passed" }],
    },
  };
}

function grade(ir = fixture()) {
  return runBuiltinGrader(GRADER, [event("trajectory.ir", ir)], { config });
}

describe("experimental decision integrity over optional trajectory IR", () => {
  it("checks explicitly linked same-revision validation, with bounded local supporting references", () => {
    const result = grade();
    expect(result.value).toBe(1);
    expect(result.diagnostics).toMatchObject({
      evidenceBasis: "producer-asserted",
      scope: { population: "annotated-decisions", eventIndices: "ir-local", source: { sessionEventIndex: 0, phase: "agent" } },
      counts: { total: 1, evaluable: 1, valid: 1, invalid: 0, unknown: 0 },
      coverage: 1,
      decisions: [{ decisionIndex: 0, eventIndex: 33, status: "valid", reasons: [] }],
    });
    expect(result.diagnostics.decisions[0].support).toEqual([
      { collection: "actions", index: 0 },
      { collection: "events", index: 2 },
      { collection: "validations", index: 0 },
      { collection: "toolCalls", index: 0 },
      { collection: "observations", index: 0 },
      { collection: "events", index: 0 },
      { collection: "events", index: 1 },
    ]);
  });

  it.each([
    [
      "missing-required-validation",
      ir => {
        ir.decisionLineage.decisions[0].validationIds = [];
      },
    ],
    [
      "stale-validation",
      ir => {
        ir.decisionLineage.validations[0].revisionId = "r0";
      },
    ],
    [
      "unsupported-acceptance",
      ir => {
        ir.decisionLineage.validations[0].outcome = "failed";
      },
    ],
    [
      "unsupported-acceptance",
      ir => {
        ir.provenanceEdges = [];
      },
    ],
  ])("reports grounded %s violations", (reason, edit) => {
    const ir = fixture();
    edit(ir);
    const result = grade(ir);
    expect(result.value).toBe(0);
    expect(result.diagnostics.counts).toMatchObject({ invalid: 1, unknown: 0 });
    expect(result.diagnostics.violations[reason]).toBe(1);
    expect(result.diagnostics.decisions[0].reasons).toContain(reason);
  });

  it.each([undefined, false])("does not turn absent evidence into penalties when complete=%s", complete => {
    const ir = fixture();
    ir.decisionLineage.complete = complete;
    ir.decisionLineage.decisions[0].validationIds = [];
    ir.provenanceEdges = [];
    const result = grade(ir);
    expect(result).toMatchObject({ value: null, status: "unavailable", diagnostics: { coverage: 0, counts: { unknown: 1, evaluable: 0 } } });
    expect(Object.values(result.diagnostics.violations)).toEqual([0, 0, 0]);
  });

  it("can establish explicit failed or stale evidence without a completeness assertion", () => {
    const ir = fixture();
    delete ir.decisionLineage.complete;
    ir.decisionLineage.validations[0].outcome = "failed";
    ir.decisionLineage.validations[0].revisionId = "r0";
    expect(grade(ir)).toMatchObject({
      value: 0,
      diagnostics: { violations: { "stale-validation": 1, "unsupported-acceptance": 1 } },
    });
  });

  it.each([
    [
      "no instrumentation",
      ir => {
        delete ir.decisionLineage;
      },
    ],
    [
      "no declarations",
      ir => {
        ir.decisionLineage.decisions[0].requiredValidations = [];
      },
    ],
    [
      "missing revision",
      ir => {
        delete ir.decisionLineage.decisions[0].revisionId;
      },
    ],
    [
      "missing validation revision",
      ir => {
        delete ir.decisionLineage.validations[0].revisionId;
      },
    ],
    [
      "dangling action",
      ir => {
        ir.decisionLineage.decisions[0].actionId = "missing";
      },
    ],
    [
      "dangling validation",
      ir => {
        ir.decisionLineage.decisions[0].validationIds = ["missing"];
      },
    ],
    [
      "dangling tool",
      ir => {
        ir.decisionLineage.validations[0].toolCallId = "missing";
      },
    ],
    [
      "dangling result",
      ir => {
        ir.decisionLineage.validations[0].resultObservationId = "missing";
      },
    ],
    [
      "uncorrelated result",
      ir => {
        ir.observations[0].sourceToolCallId = "other";
      },
    ],
    [
      "uncorrelated call",
      ir => {
        ir.toolCalls[0].outputRef = "other";
      },
    ],
    [
      "missing call occurrence",
      ir => {
        delete ir.toolCalls[0].eventIndex;
      },
    ],
    [
      "missing result occurrence",
      ir => {
        delete ir.observations[0].eventIndex;
      },
    ],
    [
      "dangling occurrence",
      ir => {
        ir.events[0].ref = "other";
      },
    ],
    [
      "dangling decision event",
      ir => {
        ir.actions[0].eventIndex = 99;
      },
    ],
    [
      "dangling edge",
      ir => {
        ir.provenanceEdges[0].from = "missing";
      },
    ],
    [
      "unknown relation",
      ir => {
        ir.provenanceEdges[0].relation = "suggests";
      },
    ],
    [
      "cyclic provenance",
      ir => {
        ir.provenanceEdges.push({ from: "accept", to: "result", relation: "derived-from" });
      },
    ],
    [
      "duplicate node IDs",
      ir => {
        ir.observations.push({ ...ir.observations[0] });
      },
    ],
    [
      "cross-collection duplicate IDs",
      ir => {
        ir.actions[0].id = "result";
      },
    ],
    [
      "duplicate event indices",
      ir => {
        ir.events.push({ ...ir.events[0] });
      },
    ],
    [
      "duplicate decisions",
      ir => {
        ir.decisionLineage.decisions.push({ ...ir.decisionLineage.decisions[0] });
      },
    ],
    [
      "duplicate validations",
      ir => {
        ir.decisionLineage.validations.push({ ...ir.decisionLineage.validations[0] });
      },
    ],
    [
      "duplicate references",
      ir => {
        ir.decisionLineage.decisions[0].validationIds.push("v");
      },
    ],
    [
      "malformed decision",
      ir => {
        ir.decisionLineage.decisions[0] = null;
      },
    ],
    [
      "malformed validation",
      ir => {
        ir.decisionLineage.validations[0] = null;
      },
    ],
    [
      "malformed annotations",
      ir => {
        ir.decisionLineage = "not-a-record";
      },
    ],
    [
      "malformed collection",
      ir => {
        ir.observations = {};
      },
    ],
    [
      "redacted identifiers",
      ir => {
        ir.decisionLineage.decisions[0].revisionId = "***REDACTED***";
      },
    ],
    [
      "partially redacted revisions",
      ir => {
        ir.decisionLineage.decisions[0].revisionId = ir.decisionLineage.validations[0].revisionId = "rev-***REDACTED***";
      },
    ],
    [
      "unknown outcome",
      ir => {
        ir.decisionLineage.validations[0].outcome = "unknown";
      },
    ],
    [
      "no explicit outcome",
      ir => {
        delete ir.decisionLineage.validations[0].outcome;
      },
    ],
    [
      "execution contradiction",
      ir => {
        ir.toolCalls[0].success = false;
      },
    ],
    [
      "unfinished call",
      ir => {
        ir.toolCalls[0].completed = false;
      },
    ],
    [
      "nonzero exit",
      ir => {
        ir.toolCalls[0].exitCode = 1;
      },
    ],
  ])("conservatively leaves %s unknown", (_label, edit) => {
    const ir = fixture();
    edit(ir);
    const result = grade(ir);
    expect(result.value).toBeNull();
    expect(result.status).toBe("unavailable");
    expect(result.diagnostics.counts.evaluable).toBe(0);
    expect(Object.values(result.diagnostics.violations)).toEqual([0, 0, 0]);
  });

  it("never treats a tool name, successful process, or prose as validation success", () => {
    const ir = fixture();
    delete ir.decisionLineage.validations[0].outcome;
    ir.toolCalls[0].name = "run_all_tests_successfully";
    ir.toolCalls[0].exitCode = 0;
    ir.observations[0].content = "All tests passed";
    expect(grade(ir).value).toBeNull();
  });

  it.each([
    { error: { message: "recorded execution error" } },
    { error: "" },
    { status: "failed" },
    { status: "failure" },
    { status: "error" },
    { is_error: true },
    { isError: true },
    { success: false },
    { completed: false },
    { exitCode: 1 },
  ])("leaves a passed assertion conflicting with explicit failure %j unknown", failure => {
    const ir = fixture();
    delete ir.toolCalls[0].success;
    Object.assign(ir.toolCalls[0], failure);
    const result = grade(ir);
    expect(result).toMatchObject({
      value: null,
      diagnostics: { counts: { valid: 0, invalid: 0, unknown: 1 }, decisions: [{ status: "unknown", reasons: ["conflicting-validation"] }] },
    });
  });

  it.each([
    [0, "message"],
    [1, "message"],
    [0, "observation"],
    [1, "tool_call"],
    [0, undefined],
    [1, undefined],
  ])("requires the referenced validation occurrence %i to have its canonical kind, not %s", (position, kind) => {
    const ir = fixture();
    ir.events[position].kind = kind;
    expect(grade(ir)).toMatchObject({
      value: null,
      diagnostics: { counts: { unknown: 1 }, decisions: [{ status: "unknown", reasons: ["unresolved-validation"] }] },
    });
  });

  it("excludes unknown decisions from the score but retains annotated-decision coverage", () => {
    const ir = fixture();
    const first = ir.decisionLineage.decisions[0];
    ir.decisionLineage.decisions.push({ ...first, id: "invalid", revisionId: "r2" }, { ...first, id: "unknown", revisionId: undefined });
    const result = grade(ir);
    expect(result).toMatchObject({
      value: 0.5,
      diagnostics: { coverage: 2 / 3, counts: { total: 3, valid: 1, invalid: 1, unknown: 1, evaluable: 2 } },
    });
  });

  it("unknown structural evidence takes precedence over a simultaneous violation", () => {
    const ir = fixture();
    ir.decisionLineage.decisions[0].revisionId = "r2";
    ir.decisionLineage.decisions[0].validationIds.push("dangling");
    expect(grade(ir)).toMatchObject({ value: null, diagnostics: { counts: { unknown: 1 }, violations: { "stale-validation": 0 } } });
  });

  it("does not infer revisions or causality from timestamp or event order", () => {
    const ir = fixture();
    const before = grade(ir).value;
    ir.events.reverse();
    ir.events.forEach((entry, i) => {
      entry.timestamp = `2026-01-01T00:00:0${i}Z`;
    });
    expect(grade(ir).value).toBe(before);
    delete ir.decisionLineage.validations[0].revisionId;
    expect(grade(ir).value).toBeNull();
  });

  it("uses only the latest agent IR, never merges IDs across phase, session, or attempt scopes", () => {
    const older = event("trajectory.ir", fixture());
    older.provenance.attemptId = "old";
    const latest = fixture();
    latest.toolCalls = [];
    latest.observations = [];
    latest.provenanceEdges = [];
    const session = [older, event("trajectory.ir", latest), event("trajectory.ir", fixture(), "evals")];
    const result = runBuiltinGrader(GRADER, session, { config });
    expect(result.value).toBeNull();
    expect(result.diagnostics.scope.source).toEqual({ sessionEventIndex: 1, phase: "agent" });
    expect(createSessionLookups(session).trace.trajectoryIR.toolCalls).toEqual([]);
  });

  it("bounds diagnostics and never copies IDs, command text, or raw observations", () => {
    const ir = fixture();
    ir.toolCalls[0].arguments = { command: "PRIVATE_COMMAND" };
    ir.observations[0].content = "PRIVATE_PAYLOAD";
    ir.decisionLineage.decisions = Array.from({ length: 30 }, (_, i) => ({ ...ir.decisionLineage.decisions[0], id: `PRIVATE_ID_${i}` }));
    const result = grade(ir);
    expect(result.diagnostics.counts.valid).toBe(30);
    expect(result.diagnostics.decisions).toHaveLength(20);
    expect(result.diagnostics.truncated).toBe(true);
    expect(result.diagnostics.decisions.every(decision => decision.support.length <= 8)).toBe(true);
    expect(JSON.stringify(result)).not.toContain("PRIVATE_");
  });

  it("bounds annotation collections, reference expansion, and graph work", () => {
    const ir = fixture();
    ir.decisionLineage.decisions = Array(5001).fill(ir.decisionLineage.decisions[0]);
    expect(grade(ir)).toMatchObject({ value: null, message: "unavailable: malformed-annotations" });
    const many = fixture();
    many.decisionLineage.decisions = Array.from({ length: 5 }, (_, i) => ({ ...many.decisionLineage.decisions[0], id: `d${i}`, requiredValidations: Array.from({ length: 5000 }, (_, j) => `check${j}`) }));
    expect(grade(many)).toMatchObject({ value: null, message: "unavailable: reference-limit" });
    const graph = fixture();
    graph.states = Array.from({ length: 500 }, (_, i) => ({ id: `s${i}` }));
    graph.provenanceEdges.push({ from: "s0", to: "accept", relation: "informed" });
    for (let i = 1; i < 500; i++) graph.provenanceEdges.push({ from: `s${i}`, to: `s${i - 1}`, relation: "derived-from" });
    graph.decisionLineage.decisions = Array.from({ length: 150 }, (_, i) => ({ ...graph.decisionLineage.decisions[0], id: `d${i}` }));
    const result = grade(graph);
    expect(result.diagnostics.counts.unknown).toBeGreaterThan(0);
    expect(result.diagnostics.counts.valid).toBeGreaterThan(0);
  });

  it("is deterministic, immutable, and pure within the production compute boundary", () => {
    const session = [event("trajectory.ir", fixture())];
    const frozen = structuredClone(session);
    const freeze = value => {
      if (value && typeof value === "object") {
        Object.values(value).forEach(freeze);
        Object.freeze(value);
      }
    };
    freeze(session);
    const first = grade();
    const spies = [
      vi.spyOn(fs, "readFileSync").mockImplementation(() => {
        throw new Error("I/O forbidden");
      }),
      vi.spyOn(cp, "execSync").mockImplementation(() => {
        throw new Error("execution forbidden");
      }),
      vi.spyOn(globalThis, "fetch").mockImplementation(() => {
        throw new Error("network forbidden");
      }),
      vi.spyOn(Date, "now").mockImplementation(() => {
        throw new Error("clock forbidden");
      }),
    ];
    try {
      expect(runBuiltinGrader(GRADER, session, { config })).toEqual(first);
      expect(computeGraders(session, [{ id: GRADER, config }]).output.results[0].value).toBe(1);
      expect(session).toEqual(frozen);
      for (const spy of spies) expect(spy).not.toHaveBeenCalled();
    } finally {
      spies.forEach(spy => spy.mockRestore());
    }
  });

  it("preserves annotations through existing unknown-event projection without expanding grader.result", () => {
    const input = event("trajectory.ir", fixture());
    const normalized = normalizeUnifiedSessionEvent(input);
    expect(normalized.data.decisionLineage).toEqual(input.data.decisionLineage);
    expect(runBuiltinGrader(GRADER, [{ ...normalized, provenance: input.provenance }], { config }).value).toBe(1);
    const result = grade();
    const projected = normalizeUnifiedSessionEvent(event("grader.result", result));
    expect(projected.data.diagnostics).toBeUndefined();
  });

  it("leaves default end-to-end lineage semantics unchanged", () => {
    const session = [event("trajectory.ir", fixture())];
    expect(runBuiltinGrader(GRADER, session, {}).value).toBe(1);
    delete session[0].data.decisionLineage;
    expect(runBuiltinGrader(GRADER, session, {}).value).toBe(1);
    expect(runBuiltinGrader(GRADER, session, { config }).value).toBeNull();
  });

  it.each([
    ["copilot", () => JSON.stringify(require("./fixtures/copilot_ci_audit.cjs"))],
    ["claude", () => JSON.stringify(require("./fixtures/claude_ci_sessions.cjs").success)],
    ["codex", () => fs.readFileSync(path.join(__dirname, "test_data/codex_ci_smoke.jsonl"), "utf8")],
  ])("leaves existing genuine %s adapter evidence unknown without annotations", (engine, content) => {
    const native = parseEngineSession(content(), engine);
    expect(native.length).toBeGreaterThan(0);
    const session = mergeSessionSources([{ component: "agent", phase: "agent", path: "agent-session.jsonl", events: native }], engine);
    expect(session.some(entry => entry.type.startsWith("tool."))).toBe(true);
    expect(runBuiltinGrader(GRADER, session, { config })).toMatchObject({
      value: null,
      status: "unavailable",
      message: "unavailable: no decision lineage annotations",
      diagnostics: { counts: { total: 0, evaluable: 0 }, coverage: null },
    });
  });
});
