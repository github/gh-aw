// @ts-check

// Experimental producer-supplied annotations, not native engine evidence.
// This checks recorded consistency, not the truth of a producer's assertions.
const MAX_ITEMS = 5000;
const MAX_REFERENCES = 20000;
const MAX_GRAPH_VISITS = 100000;
const MAX_DIAGNOSTICS = 20;
const MAX_SUPPORT = 8;
const { isRecord, isToolFailure } = require("./grader_session.cjs");

const VIOLATIONS = ["missing-required-validation", "stale-validation", "unsupported-acceptance"];
const NODE_COLLECTIONS = ["actions", "states", "toolCalls", "observations", "resources", "objectives", "outputs"];
const id = value => typeof value === "string" && value.length > 0 && value.length <= 256 && !value.includes("***");
const index = value => Number.isSafeInteger(value) && value >= 0;
const list = value => Array.isArray(value) && value.length <= MAX_ITEMS;
const ids = value => list(value) && value.every(id) && new Set(value).size === value.length;

/** @param {any} trace @returns {any} */
function gradeDecisionIntegrity(trace) {
  // Use only the shared projection's selected IR, never combine separate scopes.
  const ir = trace?.trajectoryIR;
  const annotation = ir?.decisionLineage;
  const decisions = isRecord(annotation) && list(annotation.decisions) ? annotation.decisions : [];
  const diagnostics = {
    mode: "decision-integrity",
    version: 1,
    evidenceBasis: "producer-asserted",
    scope: {
      population: "annotated-decisions",
      eventIndices: "ir-local",
      source: index(trace?.trajectoryIRSource?.sessionEventIndex) ? { sessionEventIndex: trace.trajectoryIRSource.sessionEventIndex, phase: "agent" } : null,
    },
    counts: { total: decisions.length, valid: 0, invalid: 0, unknown: 0, evaluable: 0 },
    /** @type {number|null} */
    coverage: null,
    violations: Object.fromEntries(VIOLATIONS.map(reason => [reason, 0])),
    /** @type {any[]} */
    decisions: [],
    truncated: false,
  };
  const finish = message => {
    const counts = diagnostics.counts;
    counts.evaluable = counts.valid + counts.invalid;
    diagnostics.coverage = counts.total ? counts.evaluable / counts.total : null;
    diagnostics.truncated = counts.total > diagnostics.decisions.length;
    return {
      value: counts.evaluable ? counts.valid / counts.evaluable : null,
      unit: "ratio",
      ...(message ? { message } : {}),
      details: `decisions=${counts.total} valid=${counts.valid} invalid=${counts.invalid} unknown=${counts.unknown}`,
      diagnostics,
    };
  };
  const record = (decisionIndex, status, reasons, support = []) => {
    diagnostics.counts[status]++;
    if (status === "invalid") for (const reason of reasons) diagnostics.violations[reason]++;
    if (diagnostics.decisions.length < MAX_DIAGNOSTICS) {
      const eventIndex = decisions[decisionIndex]?.eventIndex;
      diagnostics.decisions.push({
        decisionIndex,
        ...(index(eventIndex) ? { eventIndex } : {}),
        status,
        reasons,
        support: support.slice(0, MAX_SUPPORT),
      });
    }
  };
  const unavailable = reason => {
    for (let i = 0; i < decisions.length; i++) record(i, "unknown", [reason]);
    return finish(`unavailable: ${reason}`);
  };
  if (annotation === undefined) return finish("unavailable: no decision lineage annotations");
  if (!isRecord(annotation) || annotation.version !== 1 || (annotation.complete !== undefined && typeof annotation.complete !== "boolean") || !list(annotation.decisions) || !list(annotation.validations))
    return unavailable("malformed-annotations");
  if (!decisions.length) return finish("unavailable: no declared decisions");
  if (!isRecord(ir) || !list(ir.events) || !list(ir.actions) || !list(ir.toolCalls) || !list(ir.observations) || !list(ir.provenanceEdges)) return unavailable("incomplete-ir");

  /** @type {Map<string, {collection: string, index: number, item: any}>} */
  const nodes = new Map();
  for (const collection of NODE_COLLECTIONS) {
    if (ir[collection] === undefined) continue;
    if (!list(ir[collection])) return unavailable("malformed-ir");
    for (let i = 0; i < ir[collection].length; i++) {
      const item = ir[collection][i];
      if (!isRecord(item) || !id(item.id) || nodes.has(item.id)) return unavailable("ambiguous-node-id");
      nodes.set(item.id, { collection, index: i, item });
    }
  }
  const events = new Map();
  for (let i = 0; i < ir.events.length; i++) {
    const event = ir.events[i];
    if (!isRecord(event) || !index(event.index) || events.has(event.index)) return unavailable("ambiguous-event-index");
    events.set(event.index, { item: event, index: i });
  }
  const validations = new Map();
  for (let i = 0; i < annotation.validations.length; i++) {
    const validation = annotation.validations[i];
    if (!isRecord(validation) || !id(validation.id) || validations.has(validation.id)) return unavailable("ambiguous-validation-id");
    validations.set(validation.id, { item: validation, index: i });
  }
  const decisionIds = new Set();
  for (const decision of decisions) {
    if (!isRecord(decision) || !id(decision.id) || decisionIds.has(decision.id)) return unavailable("ambiguous-decision-id");
    decisionIds.add(decision.id);
  }
  /** @type {Map<string, string[]>} */
  const parents = new Map();
  for (const edge of ir.provenanceEdges) {
    if (!isRecord(edge) || !id(edge.from) || !id(edge.to) || !nodes.has(edge.from) || !nodes.has(edge.to) || !["informed", "required-for", "derived-from"].includes(edge.relation)) return unavailable("dangling-provenance");
    const bucket = parents.get(edge.to) ?? [];
    bucket.push(edge.from);
    parents.set(edge.to, bucket);
  }
  let references = ir.provenanceEdges.length;
  for (const decision of decisions) {
    if (!ids(decision.requiredValidations) || !ids(decision.validationIds)) return unavailable("malformed-declaration");
    references += decision.requiredValidations.length + decision.validationIds.length;
    if (references > MAX_REFERENCES) return unavailable("reference-limit");
  }
  let visits = 0;
  // Iterative DFS detects cycles even when another branch reaches a root.
  const evidenceGraph = start => {
    const active = new Set();
    const seen = new Set();
    const roots = new Set();
    const pending = [{ key: start, exit: false }];
    while (pending.length) {
      if (++visits > MAX_GRAPH_VISITS) return { reason: "graph-limit", roots };
      const next = pending.pop();
      if (!next) break;
      if (next.exit) {
        active.delete(next.key);
        seen.add(next.key);
        continue;
      }
      if (active.has(next.key)) return { reason: "cyclic-provenance", roots };
      if (seen.has(next.key)) continue;
      const node = nodes.get(next.key);
      if (node?.collection === "toolCalls" || node?.collection === "observations") roots.add(next.key);
      active.add(next.key);
      pending.push({ key: next.key, exit: true });
      for (const parent of parents.get(next.key) ?? []) pending.push({ key: parent, exit: false });
    }
    return { reason: null, roots };
  };
  for (let i = 0; i < decisions.length; i++) {
    const decision = decisions[i];
    const action = nodes.get(decision.actionId);
    const event = events.get(decision.eventIndex);
    const support = /** @type {any[]} */ [];
    if (!id(decision.actionId) || !index(decision.eventIndex) || !id(decision.revisionId) || action?.collection !== "actions" || action.item.eventIndex !== decision.eventIndex || event?.item.ref !== decision.actionId) {
      record(i, "unknown", ["unresolved-decision"]);
      continue;
    }
    support.push({ collection: "actions", index: action.index }, { collection: "events", index: event.index });
    if (!decision.requiredValidations.length) {
      record(i, "unknown", ["no-required-validations"], support);
      continue;
    }
    const graph = evidenceGraph(decision.actionId);
    if (graph.reason) {
      record(i, "unknown", [graph.reason], support);
      continue;
    }
    const violations = new Set();
    const unknown = new Set();
    const requirements = new Set();
    const required = new Set(decision.requiredValidations);
    for (const validationId of decision.validationIds) {
      const entry = validations.get(validationId);
      const validation = entry?.item;
      const call = nodes.get(validation?.toolCallId);
      const result = nodes.get(validation?.resultObservationId);
      const callEvent = events.get(call?.item.eventIndex);
      const resultEvent = events.get(result?.item.eventIndex);
      if (
        !entry ||
        !id(validation.requirement) ||
        !id(validation.revisionId) ||
        !["passed", "failed"].includes(validation.outcome) ||
        call?.collection !== "toolCalls" ||
        result?.collection !== "observations" ||
        call.item.outputRef !== validation.resultObservationId ||
        result.item.sourceToolCallId !== validation.toolCallId ||
        !index(call.item.eventIndex) ||
        !index(result.item.eventIndex) ||
        callEvent?.item.ref !== validation.toolCallId ||
        resultEvent?.item.ref !== validation.resultObservationId ||
        callEvent.item.kind !== "tool_call" ||
        resultEvent.item.kind !== "observation"
      ) {
        unknown.add("unresolved-validation");
        continue;
      }
      if (!required.has(validation.requirement)) {
        unknown.add("undeclared-validation");
        continue;
      }
      requirements.add(validation.requirement);
      if (support.length < MAX_SUPPORT)
        support.push(
          { collection: "validations", index: entry.index },
          { collection: "toolCalls", index: call.index },
          { collection: "observations", index: result.index },
          { collection: "events", index: callEvent.index },
          { collection: "events", index: resultEvent.index }
        );
      if (validation.revisionId !== decision.revisionId) violations.add("stale-validation");
      if (validation.outcome === "failed") violations.add("unsupported-acceptance");
      // An execution failure cannot establish a passing validation. Conversely,
      // a successful execution alone never establishes a passing validation.
      if (validation.outcome === "passed" && (isToolFailure(call.item) || call.item.status === "failed" || call.item.is_error === true || (call.item.exitCode !== undefined && call.item.exitCode !== 0)))
        unknown.add("conflicting-validation");
      if (!graph.roots.has(validation.toolCallId) && !graph.roots.has(validation.resultObservationId)) {
        if (annotation.complete === true) violations.add("unsupported-acceptance");
        else unknown.add("incomplete-provenance");
      }
    }
    if (decision.requiredValidations.some(requirement => !requirements.has(requirement))) {
      if (annotation.complete === true) violations.add("missing-required-validation");
      else unknown.add("incomplete-validations");
    }
    if (!graph.roots.size) {
      if (annotation.complete === true) violations.add("unsupported-acceptance");
      else unknown.add("incomplete-provenance");
    }
    if (unknown.size) record(i, "unknown", [...unknown], support);
    else if (violations.size)
      record(
        i,
        "invalid",
        VIOLATIONS.filter(reason => violations.has(reason)),
        support
      );
    else record(i, "valid", [], support);
  }
  return finish(null);
}

module.exports = { gradeDecisionIntegrity };
