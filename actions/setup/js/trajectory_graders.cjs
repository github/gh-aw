// @ts-check

// Built-in trajectory graders. Each grader is a pure, deterministic projection
// over the canonical Trajectory IR (see .github/workflows/shared/graders/trajectory-ir.md).
// They run in-process on every trace: no network or API access, no filesystem
// access, and bounded work (quadratic graders are capped by MAX_TRAJECTORY_ITEMS).

// Maximum number of items in any IR collection before trajectory graders report
// the trace as unavailable, keeping the O(n^2) graders computationally cheap.
const MAX_TRAJECTORY_ITEMS = 5000;

// Maximum length of the text each skill constraint pattern is matched against.
const MAX_CONSTRAINT_TEXT_LENGTH = 4096;

const helpers = Object.freeze({
  /** @param {number} v @param {number} lo @param {number} hi */
  clamp: (v, lo, hi) => Math.max(lo, Math.min(hi, v)),
  /** @param {number} num @param {number} den */
  ratio: (num, den) => (den === 0 ? 0 : num / den),
  /** @param {number[]} arr */
  sum: arr => arr.reduce((a, b) => a + b, 0),
});

const IR_COLLECTION_KEYS = ["events", "states", "actions", "toolCalls", "observations", "resources", "provenanceEdges", "objectives", "outputs"];

/**
 * @param {any} value
 * @returns {value is Record<string, any>}
 */
function isRecord(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

/**
 * Return the first IR collection path whose length exceeds MAX_TRAJECTORY_ITEMS,
 * or null when every collection is within bounds.
 * @param {any} trace
 * @returns {string|null}
 */
function findOversizedTrajectoryCollection(trace) {
  if (!isRecord(trace)) return null;
  const agentOutput = isRecord(trace.agentOutput) ? trace.agentOutput : null;
  const candidates = [trace, trace.trajectoryIR, trace.trajectoryIr, trace.ir, agentOutput?.trajectoryIR, agentOutput?.trajectoryIr, agentOutput?.trajectory, agentOutput].filter(isRecord);
  for (const candidate of candidates) {
    for (const key of IR_COLLECTION_KEYS) {
      if (Array.isArray(candidate[key]) && candidate[key].length > MAX_TRAJECTORY_ITEMS) return key;
    }
    const reference = candidate.reference;
    if (isRecord(reference)) {
      for (const [key, value] of Object.entries(reference)) {
        if (Array.isArray(value) && value.length > MAX_TRAJECTORY_ITEMS) return `reference.${key}`;
      }
      if (isRecord(reference.patch) && Array.isArray(reference.patch.files) && reference.patch.files.length > MAX_TRAJECTORY_ITEMS) return "reference.patch.files";
    }
  }
  return null;
}

/**
 * Wrap a trajectory grader so oversized traces are reported as unavailable
 * instead of running unbounded work.
 * @param {(trace: any, config: Record<string, any>) => any} fn
 * @returns {(trace: any, config?: Record<string, any>) => any}
 */
function withTrajectoryLimits(fn) {
  return (trace, config) => {
    const oversized = findOversizedTrajectoryCollection(trace);
    if (oversized !== null) {
      return { value: null, passed: null, message: `unavailable: ${oversized} exceeds ${MAX_TRAJECTORY_ITEMS} items` };
    }
    return fn(trace, isRecord(config) ? config : {});
  };
}

/**
 * policy-near-miss: Fraction of successful traces that left guard or policy objectives unsatisfied.
 * @param {any} trace
 * @param {Record<string, any>} config
 * @returns {any}
 */
function gradePolicyNearMiss(trace, config) {
  const isRecord = value => value !== null && typeof value === "object" && !Array.isArray(value);
  const candidates = [
    trace.trajectoryIR,
    trace.trajectoryIr,
    trace.ir,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectoryIR : null,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectoryIr : null,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectory : null,
    isRecord(trace.agentOutput) ? trace.agentOutput : null,
  ].filter(isRecord);

  const candidate = candidates.find(value => Array.isArray(value.events) && Array.isArray(value.objectives));
  const events = candidate?.events ?? candidates.find(value => Array.isArray(value.events))?.events ?? [];
  const objectives = candidate?.objectives ?? candidates.find(value => Array.isArray(value.objectives))?.objectives ?? [];

  if (objectives.length === 0) {
    return { value: null, unit: "ratio", passed: null, message: "not applicable: no declared objectives in the trace" };
  }

  const reachedOutcome = events.some(event => isRecord(event) && event.kind === "safe_output");
  if (!reachedOutcome) {
    return { value: null, unit: "ratio", passed: null, message: "not applicable: no safe_output event; run did not reach an outcome" };
  }

  // Guard/policy-shaped objectives: matched by keyword against the
  // objective's description, not by an explicit "guard" flag, since the
  // IR does not distinguish guard objectives from other objectives.
  const guardPattern = /\b(check|verify|verification|policy|approval|approve|guard|confirm|authorize|authorization)\b/i;
  const guardObjectives = objectives.filter(objective => isRecord(objective) && typeof objective.description === "string" && guardPattern.test(objective.description));

  if (guardObjectives.length === 0) {
    return { value: null, unit: "ratio", passed: null, message: "not applicable: no guard/policy-shaped objectives in the trace" };
  }

  const unmet = guardObjectives.filter(objective => objective.satisfiedAtEventIndex === null || objective.satisfiedAtEventIndex === undefined);
  const value = helpers.ratio(unmet.length, guardObjectives.length);
  const unmetDescriptions = unmet.slice(0, 5).map(objective => (typeof objective.id === "string" && objective.id !== "" ? objective.id : objective.description));

  return {
    value,
    unit: "ratio",
    details: `guardObjectives=${guardObjectives.length} unmet=${unmet.length}${unmetDescriptions.length === 0 ? "" : `; unmet guards: ${unmetDescriptions.join(", ")}`}`,
  };
}

/**
 * skill-constraint-coverage: Fraction of configured skill constraints that were exercised and passed.
 * @param {any} trace
 * @param {Record<string, any>} config
 * @returns {any}
 */
function gradeSkillConstraintCoverage(trace, config) {
  const isRecord = value => value !== null && typeof value === "object" && !Array.isArray(value);
  const constraints = Array.isArray(config.constraints) ? config.constraints : [];

  if (constraints.length === 0) {
    return { value: null, unit: "ratio", passed: null, message: "not applicable: no constraints configured" };
  }

  const candidates = [
    trace,
    trace.trajectoryIR,
    trace.trajectoryIr,
    trace.ir,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectoryIR : null,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectoryIr : null,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectory : null,
    isRecord(trace.agentOutput) ? trace.agentOutput : null,
  ].filter(isRecord);

  const toolCalls = (candidates.find(value => Array.isArray(value.toolCalls) && value.toolCalls.some(isRecord))?.toolCalls ?? []).filter(isRecord);
  const actions = (candidates.find(value => Array.isArray(value.actions) && value.actions.some(isRecord))?.actions ?? []).filter(isRecord);

  if (toolCalls.length === 0 && actions.length === 0) {
    return { value: null, unit: "ratio", passed: null, message: "not applicable: trace lacks toolCalls/actions" };
  }

  // Project toolCalls/actions into a flat list of { text, ok } entries: text is
  // matched against each constraint's pattern, ok reflects success/validity.
  const entries = [];
  for (const call of toolCalls) {
    const name = typeof call.name === "string" ? call.name : "";
    let argsText = "";
    try {
      argsText = call.arguments === undefined ? "" : JSON.stringify(call.arguments);
    } catch {
      argsText = "";
    }
    // An unfinished call (completed: false, no completion event) never produced
    // a successful result, so it cannot satisfy a requireSuccess constraint.
    entries.push({ text: `${name} ${argsText}`.slice(0, MAX_CONSTRAINT_TEXT_LENGTH), ok: call.success !== false && call.completed !== false });
  }
  for (const action of actions) {
    const type = typeof action.type === "string" ? action.type : "";
    const target = typeof action.target === "string" ? action.target : "";
    entries.push({ text: `${type} ${target}`.slice(0, MAX_CONSTRAINT_TEXT_LENGTH), ok: action.validAtIssueTime !== false });
  }

  // The denominator is every supplied constraint, including ones with an
  // invalid/empty pattern -- malformed constraints count as unmet rather
  // than being silently dropped from the fraction (which would inflate
  // the score). validConstraints tracks how many had a usable pattern.
  let validConstraints = 0;
  let invalidConstraints = 0;
  let exercised = 0;
  let covered = 0;
  const failing = [];
  for (const constraint of constraints) {
    const constraintRecord = isRecord(constraint) ? constraint : null;
    const patternSource = constraintRecord !== null && typeof constraintRecord.pattern === "string" ? constraintRecord.pattern : "";
    /** @type {RegExp|null} */
    let regex = null;
    if (patternSource !== "") {
      try {
        regex = new RegExp(patternSource, "i");
      } catch {
        regex = null;
      }
    }
    if (regex === null) {
      invalidConstraints += 1;
      continue;
    }
    validConstraints += 1;
    const matches = entries.filter(entry => regex.test(entry.text));
    if (matches.length === 0) continue;
    exercised += 1;
    // Passing requires every matched entry to have succeeded/been valid
    // at issue time, unless the constraint opts out via requireSuccess: false.
    const requireSuccess = constraintRecord === null || constraintRecord.requireSuccess !== false;
    const passedConstraint = !requireSuccess || matches.every(entry => entry.ok);
    const label = constraintRecord !== null && typeof constraintRecord.id === "string" && constraintRecord.id !== "" ? constraintRecord.id : patternSource;
    if (passedConstraint) {
      covered += 1;
    } else if (failing.length < 5) {
      failing.push(label);
    }
  }

  if (validConstraints === 0) {
    return { value: null, unit: "ratio", passed: null, message: "not applicable: no constraints with a valid pattern configured" };
  }

  return {
    value: helpers.ratio(covered, constraints.length),
    unit: "ratio",
    details: `constraints=${constraints.length} exercised=${exercised} covered=${covered}${invalidConstraints === 0 ? "" : ` invalidPattern=${invalidConstraints}`}${failing.length === 0 ? "" : `; unmet: ${failing.join(", ")}`}`,
  };
}

/**
 * exploration-error: Unmet objectives attributable to insufficient search.
 * @param {any} trace
 * @param {Record<string, any>} config
 * @returns {any}
 */
function gradeExplorationError(trace, config) {
  const isRecord = value => value !== null && typeof value === "object" && !Array.isArray(value);
  const candidates = [
    trace.trajectoryIR,
    trace.trajectoryIr,
    trace.ir,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectoryIR : null,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectoryIr : null,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectory : null,
    isRecord(trace.agentOutput) ? trace.agentOutput : null,
  ].filter(isRecord);

  const candidate =
    candidates.find(value => Array.isArray(value.objectives) && value.objectives.some(isRecord)) ??
    candidates.find(value => Array.isArray(value.objectives) || Array.isArray(value.events) || Array.isArray(value.states) || Array.isArray(value.observations)) ??
    null;
  const objectives = (candidate && Array.isArray(candidate.objectives) ? candidate.objectives : []).filter(isRecord);
  if (objectives.length === 0) {
    return { value: null, unit: "ratio", passed: null, message: "not applicable: no declared objectives in the trace" };
  }

  const unmet = objectives.filter(objective => objective.satisfiedAtEventIndex === null || objective.satisfiedAtEventIndex === undefined);
  if (unmet.length === 0) {
    return { value: 0, unit: "ratio", details: `objectives=${objectives.length} unmet=0; all objectives satisfied` };
  }

  const events = (candidate && Array.isArray(candidate.events) ? candidate.events : []).filter(isRecord);
  const states = (candidate && Array.isArray(candidate.states) ? candidate.states : []).filter(isRecord);
  const observations = (candidate && Array.isArray(candidate.observations) ? candidate.observations : []).filter(isRecord);

  const stateChangeEvents = events.filter(event => event.kind === "state_change");
  let distinctStatesVisited = 0;
  let source = "";
  if (stateChangeEvents.length > 0) {
    const visited = new Set(stateChangeEvents.map(event => (typeof event.ref === "string" ? event.ref : JSON.stringify(event.ref))));
    distinctStatesVisited = visited.size;
    source = "state_change events";
  } else if (states.length > 0) {
    distinctStatesVisited = states.length;
    source = "declared states[]";
  } else {
    return { value: null, unit: "ratio", passed: null, message: "not applicable: no state_change events or declared states in the trace" };
  }

  const value = helpers.clamp(1 - observations.length / distinctStatesVisited, 0, 1);
  const unmetDescriptions = unmet.slice(0, 5).map(objective => (typeof objective.id === "string" && objective.id !== "" ? objective.id : objective.description));

  return {
    value,
    unit: "ratio",
    details: `objectives=${objectives.length} unmet=${unmet.length} observations=${observations.length} distinctStatesVisited=${distinctStatesVisited} (from ${source})${unmetDescriptions.length === 0 ? "" : `; unmet objectives: ${unmetDescriptions.join(", ")}`}`,
  };
}

/**
 * exploitation-error: Unmet objectives attributable to gathered evidence that was never used.
 * @param {any} trace
 * @param {Record<string, any>} config
 * @returns {any}
 */
function gradeExploitationError(trace, config) {
  const isRecord = value => value !== null && typeof value === "object" && !Array.isArray(value);
  const candidates = [
    trace.trajectoryIR,
    trace.trajectoryIr,
    trace.ir,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectoryIR : null,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectoryIr : null,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectory : null,
    isRecord(trace.agentOutput) ? trace.agentOutput : null,
  ].filter(isRecord);

  const candidate =
    candidates.find(value => Array.isArray(value.objectives) && value.objectives.some(isRecord)) ??
    candidates.find(value => Array.isArray(value.objectives) || Array.isArray(value.events) || Array.isArray(value.states) || Array.isArray(value.observations)) ??
    null;
  const objectives = (candidate && Array.isArray(candidate.objectives) ? candidate.objectives : []).filter(isRecord);
  if (objectives.length === 0) {
    return { value: null, unit: "ratio", passed: null, message: "not applicable: no declared objectives in the trace" };
  }

  const unmet = objectives.filter(objective => objective.satisfiedAtEventIndex === null || objective.satisfiedAtEventIndex === undefined);
  if (unmet.length === 0) {
    return { value: 0, unit: "ratio", details: `objectives=${objectives.length} unmet=0; all objectives satisfied` };
  }

  const events = (candidate && Array.isArray(candidate.events) ? candidate.events : []).filter(isRecord);
  const states = (candidate && Array.isArray(candidate.states) ? candidate.states : []).filter(isRecord);
  const observations = (candidate && Array.isArray(candidate.observations) ? candidate.observations : []).filter(isRecord);

  const stateChangeEvents = events.filter(event => event.kind === "state_change");
  let distinctStatesVisited = 0;
  let source = "";
  if (stateChangeEvents.length > 0) {
    const visited = new Set(stateChangeEvents.map(event => (typeof event.ref === "string" ? event.ref : JSON.stringify(event.ref))));
    distinctStatesVisited = visited.size;
    source = "state_change events";
  } else if (states.length > 0) {
    distinctStatesVisited = states.length;
    source = "declared states[]";
  } else {
    return { value: null, unit: "ratio", passed: null, message: "not applicable: no state_change events or declared states in the trace" };
  }

  if (observations.length === 0) {
    return { value: null, unit: "ratio", passed: null, message: "not applicable: no observations in the trace" };
  }

  if (observations.length < distinctStatesVisited) {
    return {
      value: null,
      unit: "ratio",
      passed: null,
      message: `not applicable: exploration was insufficient (observations=${observations.length} < distinctStatesVisited=${distinctStatesVisited}); see exploration-error`,
    };
  }

  const unused = observations.filter(observation => !Array.isArray(observation.consumedByActionIds) || observation.consumedByActionIds.length === 0);
  const value = helpers.clamp(unused.length / observations.length, 0, 1);
  const unmetDescriptions = unmet.slice(0, 5).map(objective => (typeof objective.id === "string" && objective.id !== "" ? objective.id : objective.description));

  return {
    value,
    unit: "ratio",
    details: `objectives=${objectives.length} unmet=${unmet.length} observations=${observations.length} unused=${unused.length} distinctStatesVisited=${distinctStatesVisited} (from ${source})${unmetDescriptions.length === 0 ? "" : `; unmet objectives: ${unmetDescriptions.join(", ")}`}`,
  };
}

/**
 * state-revisit-probability-rep: Fraction of canonical state visits that revisit an already visited state.
 * @param {any} trace
 * @param {Record<string, any>} config
 * @returns {any}
 */
function gradeStateRevisitProbabilityRep(trace, config) {
  const isRecord = value => value !== null && typeof value === "object" && !Array.isArray(value);
  const candidates = [
    trace.trajectoryIR,
    trace.trajectoryIr,
    trace.ir,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectoryIR : null,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectoryIr : null,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectory : null,
    isRecord(trace.agentOutput) ? trace.agentOutput : null,
  ].filter(isRecord);

  let states = [];
  let events = [];
  for (const candidate of candidates) {
    if (Array.isArray(candidate.states)) {
      states = candidate.states;
      events = Array.isArray(candidate.events) ? candidate.events : [];
      break;
    }
  }

  const eventOrder = new Map();
  for (let i = 0; i < events.length; i += 1) {
    const event = events[i];
    if (!isRecord(event)) continue;
    const index = Number(event.index);
    if (Number.isFinite(index)) eventOrder.set(index, i);
  }

  const ordered = states
    .map((state, position) => {
      if (typeof state === "string") return { id: state, order: position, eventIndex: null };
      if (!isRecord(state) || typeof state.id !== "string" || state.id === "") return null;
      const firstEventIndex = Number(state.firstEventIndex);
      const hasFirstEventIndex = Number.isFinite(firstEventIndex);
      const eventPosition = hasFirstEventIndex && eventOrder.has(firstEventIndex) ? eventOrder.get(firstEventIndex) : null;
      const order = eventPosition !== null ? eventPosition : hasFirstEventIndex ? firstEventIndex : position;
      return { id: state.id, order, eventIndex: hasFirstEventIndex ? firstEventIndex : null };
    })
    .filter(entry => entry !== null)
    .sort((a, b) => a.order - b.order);

  const visited = ordered.length;
  if (visited < 2) {
    return { value: 0, unit: "ratio", passed: null, message: "not applicable: fewer than two state visits" };
  }

  const seen = new Set();
  const repeated = [];
  for (const entry of ordered) {
    if (seen.has(entry.id) && repeated.length < 5) {
      repeated.push(entry.eventIndex === null ? entry.id : `${entry.id} at event ${entry.eventIndex}`);
    }
    seen.add(entry.id);
  }
  const distinct = seen.size;
  const revisits = visited - distinct;
  return {
    value: helpers.ratio(revisits, visited),
    unit: "ratio",
    details: `visited=${visited} distinct=${distinct} revisits=${revisits}${repeated.length === 0 ? "" : `; repeated states: ${repeated.join(", ")}`}`,
  };
}

/**
 * recurrence-determinism: RQA DET: fraction of recurrent points forming diagonal (repeated-subsequence) lines.
 * @param {any} trace
 * @param {Record<string, any>} config
 * @returns {any}
 */
function gradeRecurrenceDeterminism(trace, config) {
  const isRecord = value => value !== null && typeof value === "object" && !Array.isArray(value);
  const candidates = [
    trace.trajectoryIR,
    trace.trajectoryIr,
    trace.ir,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectoryIR : null,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectoryIr : null,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectory : null,
    isRecord(trace.agentOutput) ? trace.agentOutput : null,
  ].filter(isRecord);

  let states = [];
  let events = [];
  for (const candidate of candidates) {
    if (Array.isArray(candidate.states)) {
      states = candidate.states;
      events = Array.isArray(candidate.events) ? candidate.events : [];
      break;
    }
  }

  const eventOrder = new Map();
  for (let i = 0; i < events.length; i += 1) {
    const event = events[i];
    if (!isRecord(event)) continue;
    const index = Number(event.index);
    if (Number.isFinite(index)) eventOrder.set(index, i);
  }

  // Build the ordered canonical state-id sequence (one entry per visit,
  // in execution order), same normalization as state-revisit-probability-rep.
  const ordered = states
    .map((state, position) => {
      if (typeof state === "string") return { id: state, order: position };
      if (!isRecord(state) || typeof state.id !== "string" || state.id === "") return null;
      const firstEventIndex = Number(state.firstEventIndex);
      const hasFirstEventIndex = Number.isFinite(firstEventIndex);
      const eventPosition = hasFirstEventIndex && eventOrder.has(firstEventIndex) ? eventOrder.get(firstEventIndex) : null;
      const order = eventPosition !== null ? eventPosition : hasFirstEventIndex ? firstEventIndex : position;
      return { id: state.id, order };
    })
    .filter(entry => entry !== null)
    .sort((a, b) => a.order - b.order);

  const sequence = ordered.map(entry => entry.id);
  const n = sequence.length;
  if (n < 4) {
    return { value: 0, unit: "ratio", passed: null, message: "not applicable: fewer than four canonical state visits" };
  }

  // Recurrence matrix R[i][j] = 1 iff sequence[i] === sequence[j] and i !== j
  // (the main diagonal / line of identity is excluded by definition).
  // Diagonal line minimum length for RQA DET.
  const lmin = 2;
  let totalRecurrentPoints = 0;
  let diagonalPoints = 0;
  const diagonalLineLengths = [];

  // Walk every diagonal offset d = j - i (d != 0), scanning each diagonal
  // once and grouping consecutive recurrent points into line lengths.
  for (let d = 1; d < n; d += 1) {
    let runLength = 0;
    for (let i = 0; i + d < n; i += 1) {
      const j = i + d;
      const recurrent = sequence[i] === sequence[j];
      if (recurrent) {
        totalRecurrentPoints += 2; // symmetric: (i,j) and (j,i)
        runLength += 1;
      } else {
        if (runLength > 0) diagonalLineLengths.push(runLength);
        runLength = 0;
      }
    }
    if (runLength > 0) diagonalLineLengths.push(runLength);
  }

  for (const length of diagonalLineLengths) {
    if (length >= lmin) diagonalPoints += length * 2; // symmetric contribution
  }

  if (totalRecurrentPoints === 0) {
    return {
      value: 0,
      unit: "ratio",
      details: `no recurrent state pairs found across ${n} state visits; DET is vacuously 0`,
    };
  }

  const det = helpers.ratio(diagonalPoints, totalRecurrentPoints);
  const longLines = diagonalLineLengths
    .filter(length => length >= lmin)
    .sort((a, b) => b - a)
    .slice(0, 5);
  return {
    value: det,
    unit: "ratio",
    details: `visits=${n} recurrentPoints=${totalRecurrentPoints} diagonalPoints=${diagonalPoints} lmin=${lmin}${longLines.length === 0 ? "" : `; longest diagonal lines: ${longLines.join(", ")}`}`,
  };
}

/**
 * recurrence-laminarity: RQA LAM: fraction of recurrent points forming vertical (stagnation) lines.
 * @param {any} trace
 * @param {Record<string, any>} config
 * @returns {any}
 */
function gradeRecurrenceLaminarity(trace, config) {
  const isRecord = value => value !== null && typeof value === "object" && !Array.isArray(value);
  const candidates = [
    trace.trajectoryIR,
    trace.trajectoryIr,
    trace.ir,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectoryIR : null,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectoryIr : null,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectory : null,
    isRecord(trace.agentOutput) ? trace.agentOutput : null,
  ].filter(isRecord);

  let states = [];
  let events = [];
  for (const candidate of candidates) {
    if (Array.isArray(candidate.states)) {
      states = candidate.states;
      events = Array.isArray(candidate.events) ? candidate.events : [];
      break;
    }
  }

  const eventOrder = new Map();
  for (let i = 0; i < events.length; i += 1) {
    const event = events[i];
    if (!isRecord(event)) continue;
    const index = Number(event.index);
    if (Number.isFinite(index)) eventOrder.set(index, i);
  }

  // Build the ordered canonical state-id sequence (one entry per visit,
  // in execution order), same normalization as recurrence-determinism.
  const ordered = states
    .map((state, position) => {
      if (typeof state === "string") return { id: state, order: position };
      if (!isRecord(state) || typeof state.id !== "string" || state.id === "") return null;
      const firstEventIndex = Number(state.firstEventIndex);
      const hasFirstEventIndex = Number.isFinite(firstEventIndex);
      const eventPosition = hasFirstEventIndex && eventOrder.has(firstEventIndex) ? eventOrder.get(firstEventIndex) : null;
      const order = eventPosition !== null ? eventPosition : hasFirstEventIndex ? firstEventIndex : position;
      return { id: state.id, order };
    })
    .filter(entry => entry !== null)
    .sort((a, b) => a.order - b.order);

  const sequence = ordered.map(entry => entry.id);
  const n = sequence.length;
  if (n < 4) {
    return { value: 0, unit: "ratio", passed: null, message: "not applicable: fewer than four canonical state visits" };
  }

  // Recurrence matrix R[i][j] = 1 iff sequence[i] === sequence[j] and i !== j
  // (the main diagonal / line of identity is excluded by definition).
  // Vertical line minimum length for RQA LAM.
  const vmin = 2;
  let totalRecurrentPoints = 0;
  let verticalPoints = 0;
  const verticalLineLengths = [];

  // Scan every column j once, grouping consecutive recurrent rows i into
  // vertical line lengths. Scanning all columns visits every *ordered*
  // recurrent pair exactly once, so each unordered pair {i, j} counts
  // twice — the same ordered-pair convention recurrence-determinism uses,
  // and it applies to both the numerator and the denominator.
  for (let j = 0; j < n; j += 1) {
    let runLength = 0;
    for (let i = 0; i < n; i += 1) {
      const recurrent = i !== j && sequence[i] === sequence[j];
      if (recurrent) {
        totalRecurrentPoints += 1;
        runLength += 1;
      } else {
        if (runLength > 0) verticalLineLengths.push(runLength);
        runLength = 0;
      }
    }
    if (runLength > 0) verticalLineLengths.push(runLength);
  }

  for (const length of verticalLineLengths) {
    if (length >= vmin) verticalPoints += length;
  }

  if (totalRecurrentPoints === 0) {
    return {
      value: 0,
      unit: "ratio",
      details: `no recurrent state pairs found across ${n} state visits; LAM is vacuously 0`,
    };
  }

  const lam = helpers.ratio(verticalPoints, totalRecurrentPoints);
  const longLines = verticalLineLengths
    .filter(length => length >= vmin)
    .sort((a, b) => b - a)
    .slice(0, 5);
  return {
    value: lam,
    unit: "ratio",
    details: `visits=${n} recurrentPoints=${totalRecurrentPoints} verticalPoints=${verticalPoints} vmin=${vmin}${longLines.length === 0 ? "" : `; longest vertical lines: ${longLines.join(", ")}`}`,
  };
}

/**
 * recurrence-trapping-time: RQA TT: average length of vertical recurrence lines.
 * @param {any} trace
 * @param {Record<string, any>} config
 * @returns {any}
 */
function gradeRecurrenceTrappingTime(trace, config) {
  const isRecord = value => value !== null && typeof value === "object" && !Array.isArray(value);
  const candidates = [
    trace.trajectoryIR,
    trace.trajectoryIr,
    trace.ir,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectoryIR : null,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectoryIr : null,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectory : null,
    isRecord(trace.agentOutput) ? trace.agentOutput : null,
  ].filter(isRecord);

  let states = [];
  let events = [];
  for (const candidate of candidates) {
    if (Array.isArray(candidate.states)) {
      states = candidate.states;
      events = Array.isArray(candidate.events) ? candidate.events : [];
      break;
    }
  }

  const eventOrder = new Map();
  for (let i = 0; i < events.length; i += 1) {
    const event = events[i];
    if (!isRecord(event)) continue;
    const index = Number(event.index);
    if (Number.isFinite(index)) eventOrder.set(index, i);
  }

  // Build the ordered canonical state-id sequence (one entry per visit,
  // in execution order), same normalization as recurrence-laminarity.
  const ordered = states
    .map((state, position) => {
      if (typeof state === "string") return { id: state, order: position };
      if (!isRecord(state) || typeof state.id !== "string" || state.id === "") return null;
      const firstEventIndex = Number(state.firstEventIndex);
      const hasFirstEventIndex = Number.isFinite(firstEventIndex);
      const eventPosition = hasFirstEventIndex && eventOrder.has(firstEventIndex) ? eventOrder.get(firstEventIndex) : null;
      const order = eventPosition !== null ? eventPosition : hasFirstEventIndex ? firstEventIndex : position;
      return { id: state.id, order };
    })
    .filter(entry => entry !== null)
    .sort((a, b) => a.order - b.order);

  const sequence = ordered.map(entry => entry.id);
  const n = sequence.length;
  if (n < 4) {
    return { value: 0, unit: "steps", passed: null, message: "not applicable: fewer than four canonical state visits" };
  }

  // Recurrence matrix R[i][j] = 1 iff sequence[i] === sequence[j] and i !== j
  // (the main diagonal / line of identity is excluded by definition).
  // Vertical line minimum length for RQA TT.
  const vmin = 2;
  const verticalLineLengths = [];

  // Scan every column j once, grouping consecutive recurrent rows i into
  // vertical line lengths. TT is the mean length among vertical lines with
  // length >= vmin.
  for (let j = 0; j < n; j += 1) {
    let runLength = 0;
    for (let i = 0; i < n; i += 1) {
      const recurrent = i !== j && sequence[i] === sequence[j];
      if (recurrent) {
        runLength += 1;
      } else {
        if (runLength > 0) verticalLineLengths.push(runLength);
        runLength = 0;
      }
    }
    if (runLength > 0) verticalLineLengths.push(runLength);
  }

  const qualifyingLines = verticalLineLengths.filter(length => length >= vmin);
  if (qualifyingLines.length === 0) {
    return {
      value: 0,
      unit: "steps",
      details: `no vertical recurrence lines (vmin=${vmin}) found across ${n} state visits; TT is vacuously 0`,
    };
  }

  let totalLength = 0;
  for (const length of qualifyingLines) totalLength += length;
  const tt = totalLength / qualifyingLines.length;
  const longestLines = [...qualifyingLines].sort((a, b) => b - a).slice(0, 5);
  return {
    value: tt,
    unit: "steps",
    details: `visits=${n} verticalLines=${qualifyingLines.length} totalVerticalLength=${totalLength} vmin=${vmin}; longest vertical lines: ${longestLines.join(", ")}`,
  };
}

/**
 * recurrence-rate: RQA RR: density of recurrent state pairs across the run.
 * @param {any} trace
 * @param {Record<string, any>} config
 * @returns {any}
 */
function gradeRecurrenceRate(trace, config) {
  const isRecord = value => value !== null && typeof value === "object" && !Array.isArray(value);
  const candidates = [
    trace.trajectoryIR,
    trace.trajectoryIr,
    trace.ir,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectoryIR : null,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectoryIr : null,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectory : null,
    isRecord(trace.agentOutput) ? trace.agentOutput : null,
  ].filter(isRecord);

  let states = [];
  let events = [];
  for (const candidate of candidates) {
    if (Array.isArray(candidate.states)) {
      states = candidate.states;
      events = Array.isArray(candidate.events) ? candidate.events : [];
      break;
    }
  }

  const eventOrder = new Map();
  for (let i = 0; i < events.length; i += 1) {
    const event = events[i];
    if (!isRecord(event)) continue;
    const index = Number(event.index);
    if (Number.isFinite(index)) eventOrder.set(index, i);
  }

  // Build the ordered canonical state-id sequence (one entry per visit,
  // in execution order), same normalization as recurrence-determinism.
  const ordered = states
    .map((state, position) => {
      if (typeof state === "string") return { id: state, order: position };
      if (!isRecord(state) || typeof state.id !== "string" || state.id === "") return null;
      const firstEventIndex = Number(state.firstEventIndex);
      const hasFirstEventIndex = Number.isFinite(firstEventIndex);
      const eventPosition = hasFirstEventIndex && eventOrder.has(firstEventIndex) ? eventOrder.get(firstEventIndex) : null;
      const order = eventPosition !== null ? eventPosition : hasFirstEventIndex ? firstEventIndex : position;
      return { id: state.id, order };
    })
    .filter(entry => entry !== null)
    .sort((a, b) => a.order - b.order);

  const sequence = ordered.map(entry => entry.id);
  const n = sequence.length;
  if (n < 2) {
    return { value: 0, unit: "ratio", passed: null, message: "not applicable: fewer than two canonical state visits" };
  }

  // RR = recurrentPoints / (n * (n - 1)), where recurrentPoints counts all
  // ordered pairs (i, j) with i != j and sequence[i] === sequence[j].
  const counts = new Map();
  for (const id of sequence) counts.set(id, (counts.get(id) || 0) + 1);

  let recurrentPoints = 0;
  for (const count of counts.values()) {
    if (count > 1) recurrentPoints += count * (count - 1);
  }

  const possiblePoints = n * (n - 1);
  const rr = helpers.ratio(recurrentPoints, possiblePoints);
  const repeatedStates = [...counts.entries()]
    .filter(([, count]) => count > 1)
    .sort((a, b) => b[1] - a[1])
    .slice(0, 5)
    .map(([id, count]) => `${id}:${count}`);

  return {
    value: rr,
    unit: "ratio",
    details: `visits=${n} recurrentPoints=${recurrentPoints} possiblePoints=${possiblePoints}${repeatedStates.length === 0 ? "" : `; repeated states (id:count): ${repeatedStates.join(", ")}`}`,
  };
}

/**
 * event-entropy-rate: Normalized conditional Shannon entropy rate of the ordered event sequence.
 * @param {any} trace
 * @param {Record<string, any>} config
 * @returns {any}
 */
function gradeEventEntropyRate(trace, config) {
  const isRecord = value => value !== null && typeof value === "object" && !Array.isArray(value);
  const candidates = [
    trace.trajectoryIR,
    trace.trajectoryIr,
    trace.ir,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectoryIR : null,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectoryIr : null,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectory : null,
    isRecord(trace.agentOutput) ? trace.agentOutput : null,
  ].filter(isRecord);

  let events = [];
  for (const candidate of candidates) {
    if (Array.isArray(candidate.events)) {
      events = candidate.events;
      break;
    }
  }

  // Build the ordered canonical event-symbol sequence: tool_call events
  // are qualified by their ref for finer alphabet granularity, other
  // events use their kind alone.
  const ordered = events
    .map((event, position) => {
      if (!isRecord(event) || typeof event.kind !== "string" || event.kind === "") return null;
      const index = Number(event.index);
      const order = Number.isFinite(index) ? index : position;
      const symbol = event.kind === "tool_call" && typeof event.ref === "string" && event.ref !== "" ? `tool_call:${event.ref}` : event.kind;
      return { symbol, order };
    })
    .filter(entry => entry !== null)
    .sort((a, b) => a.order - b.order);

  const sequence = ordered.map(entry => entry.symbol);
  const n = sequence.length;
  if (n < 2) {
    return { value: 0, unit: "ratio", passed: null, message: "not applicable: fewer than two events" };
  }

  const alphabet = new Set(sequence);
  const alphabetSize = alphabet.size;
  if (alphabetSize < 2) {
    return { value: 0, unit: "ratio", details: `events=${n} alphabetSize=${alphabetSize}; entropy is vacuously 0 with a single symbol` };
  }

  // H(X_t | X_{t-1}): first-order (bigram) conditional Shannon entropy
  // of the symbol sequence, computed from observed transition
  // frequencies: sum over (prev, curr) pairs of p(prev, curr) * log2(total(prev) / count(prev, curr)).
  const nextCounts = new Map();
  const priorTotals = new Map();
  for (let i = 1; i < n; i += 1) {
    const prev = sequence[i - 1];
    const curr = sequence[i];
    if (!nextCounts.has(prev)) nextCounts.set(prev, new Map());
    const next = nextCounts.get(prev);
    next.set(curr, (next.get(curr) || 0) + 1);
    priorTotals.set(prev, (priorTotals.get(prev) || 0) + 1);
  }

  const transitionCount = n - 1;
  let conditionalEntropyBits = 0;
  for (const [prev, next] of nextCounts) {
    const total = priorTotals.get(prev);
    for (const count of next.values()) {
      const p = count / transitionCount;
      conditionalEntropyBits += p * Math.log2(total / count);
    }
  }

  const maxEntropyBits = Math.log2(alphabetSize);
  const normalized = helpers.clamp(conditionalEntropyBits / maxEntropyBits, 0, 1);

  return {
    value: normalized,
    unit: "ratio",
    details: `events=${n} alphabetSize=${alphabetSize} rawEntropyBits=${conditionalEntropyBits.toFixed(4)} maxEntropyBits=${maxEntropyBits.toFixed(4)}`,
  };
}

/**
 * lempel-ziv-trajectory-complexity: Normalized LZ76 complexity of the canonical event sequence.
 * @param {any} trace
 * @param {Record<string, any>} config
 * @returns {any}
 */
function gradeLempelZivTrajectoryComplexity(trace, config) {
  const isRecord = value => value !== null && typeof value === "object" && !Array.isArray(value);
  const candidates = [
    trace.trajectoryIR,
    trace.trajectoryIr,
    trace.ir,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectoryIR : null,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectoryIr : null,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectory : null,
    isRecord(trace.agentOutput) ? trace.agentOutput : null,
  ].filter(isRecord);

  let events = [];
  for (const candidate of candidates) {
    if (Array.isArray(candidate.events)) {
      events = candidate.events;
      break;
    }
  }

  // Build the ordered canonical event-symbol sequence: tool_call events
  // are qualified by their ref for finer alphabet granularity, other
  // events use their kind alone.
  const ordered = events
    .map((event, position) => {
      if (!isRecord(event) || typeof event.kind !== "string" || event.kind === "") return null;
      const index = Number(event.index);
      const order = Number.isFinite(index) ? index : position;
      const symbol = event.kind === "tool_call" && typeof event.ref === "string" && event.ref !== "" ? `tool_call:${event.ref}` : event.kind;
      return { symbol, order };
    })
    .filter(entry => entry !== null)
    .sort((a, b) => a.order - b.order);

  const sequence = ordered.map(entry => entry.symbol);
  const n = sequence.length;
  if (n < 2) {
    return { value: 0, unit: "ratio", passed: null, message: "not applicable: fewer than two events" };
  }

  const alphabetSize = new Set(sequence).size;
  if (alphabetSize < 2) {
    return { value: 0, unit: "ratio", details: `events=${n} alphabetSize=${alphabetSize}; LZ76 complexity is vacuously 0 with a single repeated symbol` };
  }

  // LZ76 incremental parsing (Kaspar & Schuster, 1987): scan the
  // sequence, incrementally extending the current phrase while it can
  // be found as a substring starting anywhere in [0, prefixLen);
  // start a new phrase (increment complexity) each time the search
  // pointer catches up to the end of the already-parsed prefix.
  let complexity = 1;
  let prefixLen = 1;
  let subSeqLen = 1;
  let maxSubSeqLen = 1;
  let pointer = 0;
  while (prefixLen + subSeqLen <= n) {
    if (sequence[pointer + subSeqLen - 1] === sequence[prefixLen + subSeqLen - 1]) {
      subSeqLen += 1;
    } else {
      maxSubSeqLen = Math.max(subSeqLen, maxSubSeqLen);
      pointer += 1;
      if (pointer === prefixLen) {
        complexity += 1;
        prefixLen += maxSubSeqLen;
        pointer = 0;
        maxSubSeqLen = 1;
        subSeqLen = 1;
      } else {
        subSeqLen = 1;
      }
    }
  }
  if (subSeqLen !== 1) complexity += 1;

  // Normalize by the theoretical asymptotic upper bound n / log_b(n)
  // (b = alphabet size) so the value stays in [0, 1] and remains
  // comparable across traces of different lengths/alphabets.
  const upperBound = n / (Math.log(n) / Math.log(alphabetSize));
  const normalized = helpers.clamp(complexity / upperBound, 0, 1);

  return {
    value: normalized,
    unit: "ratio",
    details: `events=${n} alphabetSize=${alphabetSize} rawComplexity=${complexity} upperBound=${upperBound.toFixed(4)}`,
  };
}

/**
 * tool-output-consumption-rate: Fraction of tool outputs referenced by a later action.
 * @param {any} trace
 * @param {Record<string, any>} config
 * @returns {any}
 */
function gradeToolOutputConsumptionRate(trace, config) {
  const isRecord = value => value !== null && typeof value === "object" && !Array.isArray(value);
  const candidates = [
    trace,
    trace.trajectoryIR,
    trace.trajectoryIr,
    trace.ir,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectoryIR : null,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectoryIr : null,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectory : null,
    isRecord(trace.agentOutput) ? trace.agentOutput : null,
  ].filter(isRecord);

  const candidate =
    candidates.find(value => Array.isArray(value.observations) && value.observations.some(isRecord) && Array.isArray(value.toolCalls) && value.toolCalls.some(isRecord)) ??
    // Preserve observations-only traces so missing toolCalls is reported
    // as no tool-originated observations rather than no observations.
    candidates.find(value => Array.isArray(value.observations) && value.observations.some(isRecord)) ??
    candidates.find(value => Array.isArray(value.observations)) ??
    null;
  const observations = (candidate && Array.isArray(candidate.observations) ? candidate.observations : []).filter(isRecord);

  if (observations.length === 0) {
    return { value: null, unit: "ratio", passed: null, message: "not applicable: no observations in the trace" };
  }

  const toolCalls = (candidate && Array.isArray(candidate.toolCalls) ? candidate.toolCalls : []).filter(isRecord);
  const toolCallIds = new Set(toolCalls.map(toolCall => toolCall.id).filter(id => typeof id === "string" && id !== ""));
  const toolObservations = observations.filter(observation => typeof observation.sourceToolCallId === "string" && observation.sourceToolCallId !== "" && toolCallIds.has(observation.sourceToolCallId));

  if (toolObservations.length === 0) {
    return { value: null, unit: "ratio", passed: null, message: "not applicable: no tool-originated observations in the trace" };
  }

  const isConsumed = observation => Array.isArray(observation.consumedByActionIds) && observation.consumedByActionIds.some(actionId => typeof actionId === "string" && actionId !== "");

  const consumed = toolObservations.filter(isConsumed);
  const unconsumedIds = toolObservations
    .filter(observation => !isConsumed(observation))
    .slice(0, 5)
    .map(observation => (typeof observation.id === "string" && observation.id !== "" ? observation.id : observation.sourceToolCallId));

  return {
    value: helpers.ratio(consumed.length, toolObservations.length),
    unit: "ratio",
    details: `toolObservations=${toolObservations.length} consumed=${consumed.length}${unconsumedIds.length === 0 ? "" : `; unconsumed: ${unconsumedIds.join(", ")}`}`,
  };
}

/**
 * end-to-end-lineage-completeness: Fraction of final outputs traceable to tool or observation evidence roots.
 * @param {any} trace
 * @param {Record<string, any>} config
 * @returns {any}
 */
function gradeEndToEndLineageCompleteness(trace, config) {
  const isRecord = value => value !== null && typeof value === "object" && !Array.isArray(value);
  const candidates = [
    trace,
    trace.trajectoryIR,
    trace.trajectoryIr,
    trace.ir,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectoryIR : null,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectoryIr : null,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectory : null,
    isRecord(trace.agentOutput) ? trace.agentOutput : null,
  ].filter(isRecord);
  const ir = candidates.find(value => Array.isArray(value.events) && Array.isArray(value.provenanceEdges) && Array.isArray(value.toolCalls) && Array.isArray(value.observations));
  if (!ir) {
    return { value: null, unit: "ratio", passed: null, message: "not applicable: trace lacks events, provenanceEdges, toolCalls, or observations" };
  }

  const outputEvents = ir.events.filter(event => isRecord(event) && event.kind === "safe_output");
  if (outputEvents.length === 0) {
    return { value: null, unit: "ratio", passed: null, message: "not applicable: no final safe outputs" };
  }
  const outputIds = outputEvents.map(event => event.ref);
  if (outputIds.some(id => typeof id !== "string" || id === "")) {
    return { value: null, unit: "ratio", passed: null, message: "unavailable: safe output is missing a provenance reference" };
  }

  const roots = new Set();
  for (const item of [...ir.toolCalls, ...ir.observations]) {
    if (isRecord(item) && typeof item.id === "string" && item.id !== "") roots.add(item.id);
  }
  const parents = new Map();
  for (const edge of ir.provenanceEdges) {
    if (!isRecord(edge) || typeof edge.from !== "string" || edge.from === "" || typeof edge.to !== "string" || edge.to === "") continue;
    if (!parents.has(edge.to)) parents.set(edge.to, []);
    parents.get(edge.to).push(edge.from);
  }
  const hasRoot = start => {
    const pending = [start];
    const seen = new Set();
    while (pending.length > 0) {
      const current = pending.pop();
      if (roots.has(current)) return true;
      if (seen.has(current)) continue;
      seen.add(current);
      for (const parent of parents.get(current) ?? []) pending.push(parent);
    }
    return false;
  };
  const complete = outputIds.filter(hasRoot);
  const missing = outputIds.filter(id => !hasRoot(id)).slice(0, 5);
  return {
    value: helpers.ratio(complete.length, outputIds.length),
    unit: "ratio",
    details: `outputs=${outputIds.length} traceable=${complete.length}${missing.length === 0 ? "" : `; missing lineage: ${missing.join(", ")}`}`,
  };
}

/**
 * action-provenance-coverage: Fraction of consequential actions with a provenance path to tool or observation evidence.
 * @param {any} trace
 * @param {Record<string, any>} config
 * @returns {any}
 */
function gradeActionProvenanceCoverage(trace, config) {
  const isRecord = value => value !== null && typeof value === "object" && !Array.isArray(value);
  const candidates = [
    trace,
    trace.trajectoryIR,
    trace.trajectoryIr,
    trace.ir,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectoryIR : null,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectoryIr : null,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectory : null,
    isRecord(trace.agentOutput) ? trace.agentOutput : null,
  ].filter(isRecord);
  const ir = candidates.find(value => Array.isArray(value.actions) && Array.isArray(value.provenanceEdges) && Array.isArray(value.toolCalls) && Array.isArray(value.observations));
  if (!ir) {
    return { value: null, unit: "ratio", passed: null, message: "not applicable: trace lacks actions, provenanceEdges, toolCalls, or observations" };
  }
  if (ir.actions.some(action => !isRecord(action))) {
    return { value: null, unit: "ratio", passed: null, message: "unavailable: action entry is not a record" };
  }
  if (ir.actions.some(action => typeof action.consequential !== "boolean")) {
    return { value: null, unit: "ratio", passed: null, message: "unavailable: action is missing the canonical consequential flag" };
  }

  const consequential = ir.actions.filter(action => action.consequential === true);
  if (consequential.length === 0) {
    return { value: null, unit: "ratio", passed: null, message: "not applicable: no consequential actions" };
  }
  if (consequential.some(action => typeof action.id !== "string" || action.id === "")) {
    return { value: null, unit: "ratio", passed: null, message: "unavailable: consequential action is missing an id" };
  }

  const roots = new Set();
  for (const item of [...ir.toolCalls, ...ir.observations]) {
    if (isRecord(item) && typeof item.id === "string" && item.id !== "") roots.add(item.id);
  }
  const parents = new Map();
  for (const edge of ir.provenanceEdges) {
    if (!isRecord(edge) || typeof edge.from !== "string" || edge.from === "" || typeof edge.to !== "string" || edge.to === "") continue;
    if (!parents.has(edge.to)) parents.set(edge.to, []);
    parents.get(edge.to).push(edge.from);
  }
  const hasRoot = start => {
    const pending = [start];
    const seen = new Set();
    while (pending.length > 0) {
      const current = pending.pop();
      if (roots.has(current)) return true;
      if (seen.has(current)) continue;
      seen.add(current);
      for (const parent of parents.get(current) ?? []) pending.push(parent);
    }
    return false;
  };
  const covered = consequential.filter(action => hasRoot(action.id));
  const missing = consequential
    .filter(action => !hasRoot(action.id))
    .slice(0, 5)
    .map(action => action.id);
  return {
    value: helpers.ratio(covered.length, consequential.length),
    unit: "ratio",
    details: `consequentialActions=${consequential.length} covered=${covered.length}${missing.length === 0 ? "" : `; missing provenance: ${missing.join(", ")}`}`,
  };
}

/**
 * premature-termination-gap: Declared completion conditions still unsatisfied at termination.
 * @param {any} trace
 * @param {Record<string, any>} config
 * @returns {any}
 */
function gradePrematureTerminationGap(trace, config) {
  const isRecord = value => value !== null && typeof value === "object" && !Array.isArray(value);
  const candidates = [
    trace,
    trace.trajectoryIR,
    trace.trajectoryIr,
    trace.ir,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectoryIR : null,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectoryIr : null,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectory : null,
    isRecord(trace.agentOutput) ? trace.agentOutput : null,
  ].filter(isRecord);
  const ir = candidates.find(value => Array.isArray(value.objectives));
  if (!ir || ir.objectives.length === 0) {
    return { value: null, unit: "count", passed: null, message: "not applicable: no declared objectives" };
  }
  if (ir.objectives.some(objective => !isRecord(objective))) {
    return { value: null, unit: "count", passed: null, message: "unavailable: malformed objective metadata" };
  }
  const hasInvalidIndex = ir.objectives.some(
    objective => objective.satisfiedAtEventIndex !== null && objective.satisfiedAtEventIndex !== undefined && (!Number.isSafeInteger(objective.satisfiedAtEventIndex) || objective.satisfiedAtEventIndex < 0)
  );
  if (hasInvalidIndex) {
    return { value: null, unit: "count", passed: null, message: "unavailable: invalid objective satisfaction index" };
  }

  const unmet = ir.objectives.filter(objective => objective.satisfiedAtEventIndex === null || objective.satisfiedAtEventIndex === undefined);
  const labels = unmet.slice(0, 5).map((objective, index) => (typeof objective.id === "string" && objective.id !== "" ? objective.id : `objective-${index + 1}`));
  return {
    value: unmet.length,
    unit: "count",
    details: `objectives=${ir.objectives.length} unsatisfied=${unmet.length}${labels.length === 0 ? "" : `; unsatisfied: ${labels.join(", ")}`}`,
  };
}

/**
 * evidence-saturation-stopping-lag: Events elapsed between all objectives being satisfied and the run stopping.
 * @param {any} trace
 * @param {Record<string, any>} config
 * @returns {any}
 */
function gradeEvidenceSaturationStoppingLag(trace, config) {
  const isRecord = value => value !== null && typeof value === "object" && !Array.isArray(value);
  const candidates = [
    trace,
    trace.trajectoryIR,
    trace.trajectoryIr,
    trace.ir,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectoryIR : null,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectoryIr : null,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectory : null,
    isRecord(trace.agentOutput) ? trace.agentOutput : null,
  ].filter(isRecord);
  const ir = candidates.find(value => Array.isArray(value.objectives) && Array.isArray(value.events));
  if (!ir || ir.objectives.length === 0) {
    return { value: null, unit: "count", passed: null, message: "not applicable: no declared objectives" };
  }
  const objectives = ir.objectives.filter(isRecord);
  if (objectives.length !== ir.objectives.length) {
    return { value: null, unit: "count", passed: null, message: "unavailable: malformed objective metadata" };
  }
  if (objectives.some(objective => objective.satisfiedAtEventIndex === null || objective.satisfiedAtEventIndex === undefined)) {
    return { value: null, unit: "count", passed: null, message: "not applicable: evidence never saturated" };
  }
  const satisfactionIndexes = objectives.map(objective => objective.satisfiedAtEventIndex);
  if (satisfactionIndexes.some(index => !Number.isSafeInteger(index) || index < 0)) {
    return { value: null, unit: "count", passed: null, message: "unavailable: invalid objective satisfaction index" };
  }

  const eventIndexes = ir.events.filter(isRecord).map(event => event.index);
  if (eventIndexes.length !== ir.events.length || eventIndexes.some(index => !Number.isSafeInteger(index) || index < 0)) {
    return { value: null, unit: "count", passed: null, message: "unavailable: invalid event index" };
  }
  const eventIndexSet = new Set(eventIndexes);
  if (satisfactionIndexes.some(index => !eventIndexSet.has(index))) {
    return { value: null, unit: "count", passed: null, message: "unavailable: an objective's satisfaction event is absent from the trace" };
  }
  const saturationIndex = satisfactionIndexes.reduce((a, b) => Math.max(a, b), -Infinity);
  const lag = eventIndexes.filter(index => index > saturationIndex).length;
  return {
    value: lag,
    unit: "count",
    details: `objectives=${objectives.length} saturationEventIndex=${saturationIndex} eventsAfterSaturation=${lag}`,
  };
}

/**
 * dependency-order-violation-rate: Fraction of dependent objectives satisfied before their prerequisites.
 * @param {any} trace
 * @param {Record<string, any>} config
 * @returns {any}
 */
function gradeDependencyOrderViolationRate(trace, config) {
  const isRecord = value => value !== null && typeof value === "object" && !Array.isArray(value);
  const candidates = [
    trace,
    trace.trajectoryIR,
    trace.trajectoryIr,
    trace.ir,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectoryIR : null,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectoryIr : null,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectory : null,
    isRecord(trace.agentOutput) ? trace.agentOutput : null,
  ].filter(isRecord);
  const ir = candidates.find(value => Array.isArray(value.objectives));
  if (!ir || ir.objectives.length === 0) {
    return { value: null, unit: "ratio", passed: null, message: "not applicable: no declared objectives" };
  }
  if (ir.objectives.some(objective => !isRecord(objective) || typeof objective.id !== "string" || objective.id === "")) {
    return { value: null, unit: "ratio", passed: null, message: "unavailable: objectives require unique non-empty ids" };
  }
  const byId = new Map(ir.objectives.map(objective => [objective.id, objective]));
  if (byId.size !== ir.objectives.length) {
    return { value: null, unit: "ratio", passed: null, message: "unavailable: objective ids are not unique" };
  }
  if (ir.objectives.some(objective => objective.dependsOn !== undefined && objective.dependsOn !== null && !Array.isArray(objective.dependsOn))) {
    return { value: null, unit: "ratio", passed: null, message: "unavailable: objective dependsOn must be an array" };
  }
  const dependent = ir.objectives.filter(objective => Array.isArray(objective.dependsOn) && objective.dependsOn.length > 0);
  if (dependent.length === 0) {
    return { value: null, unit: "ratio", passed: null, message: "not applicable: no objective dependencies" };
  }
  const hasUnknown = dependent.flatMap(objective => objective.dependsOn).some(id => typeof id !== "string" || !byId.has(id));
  if (hasUnknown) {
    return { value: null, unit: "ratio", passed: null, message: "unavailable: dependency references an unknown objective" };
  }

  const completed = dependent.filter(objective => Number.isSafeInteger(objective.satisfiedAtEventIndex) && objective.satisfiedAtEventIndex >= 0);
  if (completed.length === 0) {
    return { value: null, unit: "ratio", passed: null, message: "not applicable: no dependent objective completed" };
  }
  const violates = objective => {
    const completedAt = objective.satisfiedAtEventIndex;
    return objective.dependsOn.some(id => {
      const prerequisite = byId.get(id);
      return (
        prerequisite.satisfiedAtEventIndex === null ||
        prerequisite.satisfiedAtEventIndex === undefined ||
        !Number.isSafeInteger(prerequisite.satisfiedAtEventIndex) ||
        prerequisite.satisfiedAtEventIndex < 0 ||
        prerequisite.satisfiedAtEventIndex > completedAt
      );
    });
  };
  const violations = completed.filter(violates);
  return {
    value: helpers.ratio(violations.length, completed.length),
    unit: "ratio",
    details: `completedDependentObjectives=${completed.length} violations=${violations.length}${
      violations.length === 0
        ? ""
        : `; out of order: ${violations
            .slice(0, 5)
            .map(objective => objective.id)
            .join(", ")}`
    }`,
  };
}

/**
 * objective-coverage: Fraction of declared objectives that were completed.
 * @param {any} trace
 * @param {Record<string, any>} config
 * @returns {any}
 */
function gradeObjectiveCoverage(trace, config) {
  const isRecord = value => value !== null && typeof value === "object" && !Array.isArray(value);
  const candidates = [
    trace,
    trace.trajectoryIR,
    trace.trajectoryIr,
    trace.ir,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectoryIR : null,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectoryIr : null,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectory : null,
    isRecord(trace.agentOutput) ? trace.agentOutput : null,
  ].filter(isRecord);
  const ir = candidates.find(value => Array.isArray(value.objectives));
  if (!ir || ir.objectives.length === 0) {
    return { value: null, passed: null, message: "not applicable: no declared objectives" };
  }
  if (ir.objectives.some(objective => !isRecord(objective))) {
    return { value: null, passed: null, message: "unavailable: malformed objective metadata" };
  }
  const isUnset = index => index === null || index === undefined;
  if (ir.objectives.some(objective => !isUnset(objective.satisfiedAtEventIndex) && (!Number.isSafeInteger(objective.satisfiedAtEventIndex) || objective.satisfiedAtEventIndex < 0))) {
    return { value: null, passed: null, message: "unavailable: invalid objective satisfaction index" };
  }

  const total = ir.objectives.length;
  const unmet = ir.objectives
    .map((objective, index) => ({ objective, label: typeof objective.id === "string" && objective.id !== "" ? objective.id : `objective-${index + 1}` }))
    .filter(entry => isUnset(entry.objective.satisfiedAtEventIndex));
  const completed = total - unmet.length;
  const labels = unmet.slice(0, 5).map(entry => entry.label);
  return {
    value: helpers.ratio(completed, total),
    details: `objectives=${total} completed=${completed}${labels.length === 0 ? "" : `; incomplete: ${labels.join(", ")}`}`,
  };
}

/**
 * grounding-accuracy: Fraction of actions that were valid in the state in which they were issued.
 * @param {any} trace
 * @param {Record<string, any>} config
 * @returns {any}
 */
function gradeGroundingAccuracy(trace, config) {
  const isRecord = value => value !== null && typeof value === "object" && !Array.isArray(value);
  const candidates = [
    trace,
    trace.trajectoryIR,
    trace.trajectoryIr,
    trace.ir,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectoryIR : null,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectoryIr : null,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectory : null,
    isRecord(trace.agentOutput) ? trace.agentOutput : null,
  ].filter(isRecord);
  const ir = candidates.find(value => Array.isArray(value.actions));
  if (!ir || ir.actions.length === 0) {
    return { value: null, passed: null, message: "not applicable: no issued actions" };
  }
  if (ir.actions.some(action => !isRecord(action) || typeof action.validAtIssueTime !== "boolean")) {
    return { value: null, passed: null, message: "unavailable: every action requires a boolean validAtIssueTime" };
  }

  const invalid = ir.actions.map((action, index) => ({ action, label: typeof action.id === "string" && action.id !== "" ? action.id : `action-${index + 1}` })).filter(entry => entry.action.validAtIssueTime === false);
  const valid = ir.actions.length - invalid.length;
  const labels = invalid.slice(0, 5).map(entry => entry.label);
  return {
    value: helpers.ratio(valid, ir.actions.length),
    details: `actions=${ir.actions.length} valid=${valid}${labels.length === 0 ? "" : `; ungrounded: ${labels.join(", ")}`}`,
  };
}

/**
 * tool-wise-score: Longest correct execution prefix against a reference trajectory, with parameter credit.
 * @param {any} trace
 * @param {Record<string, any>} config
 * @returns {any}
 */
function gradeToolWiseScore(trace, config) {
  const isRecord = value => value !== null && typeof value === "object" && !Array.isArray(value);
  const candidates = [
    trace,
    trace.trajectoryIR,
    trace.trajectoryIr,
    trace.ir,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectoryIR : null,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectoryIr : null,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectory : null,
    isRecord(trace.agentOutput) ? trace.agentOutput : null,
  ].filter(isRecord);
  const ir = candidates.find(value => isRecord(value.reference) && Array.isArray(value.reference.toolCalls));
  if (!ir || ir.reference.toolCalls.length === 0) {
    return { value: null, passed: null, message: "not applicable: no reference tool-call trajectory" };
  }
  const validCall = call => isRecord(call) && typeof call.name === "string" && call.name !== "" && (call.arguments === undefined || call.arguments === null || isRecord(call.arguments));
  const expected = ir.reference.toolCalls;
  if (!expected.every(validCall)) {
    return { value: null, passed: null, message: "unavailable: malformed reference tool call" };
  }
  if (!Array.isArray(ir.toolCalls) || !ir.toolCalls.every(validCall)) {
    return { value: null, passed: null, message: "unavailable: trace lacks well-formed toolCalls" };
  }
  const indexed = ir.toolCalls.filter(call => Number.isSafeInteger(call.eventIndex) && call.eventIndex >= 0).length;
  if (indexed !== 0 && indexed !== ir.toolCalls.length) {
    return { value: null, passed: null, message: "unavailable: toolCalls mix indexed and unindexed entries" };
  }
  const observed = ir.toolCalls
    .map((call, position) => ({ call, position }))
    .sort((a, b) => (indexed === 0 ? 0 : a.call.eventIndex - b.call.eventIndex) || a.position - b.position)
    .map(entry => entry.call);

  const canonical = value => {
    if (Array.isArray(value)) return `[${value.map(canonical).join(",")}]`;
    if (isRecord(value))
      return `{${Object.keys(value)
        .sort()
        .map(key => `${JSON.stringify(key)}:${canonical(value[key])}`)
        .join(",")}}`;
    return JSON.stringify(value) ?? "undefined";
  };
  const argumentScore = (reference, actual) => {
    const keys = Object.keys(reference ?? {});
    if (keys.length === 0) return 1;
    const got = actual ?? {};
    return keys.filter(key => Object.hasOwn(got, key) && canonical(got[key]) === canonical(reference[key])).length / keys.length;
  };

  let prefix = 0;
  let credit = 0;
  while (prefix < expected.length && prefix < observed.length && observed[prefix].name === expected[prefix].name) {
    credit += (1 + argumentScore(expected[prefix].arguments, observed[prefix].arguments)) / 2;
    prefix += 1;
  }
  const diverged = prefix < expected.length ? `; diverged at step ${prefix + 1}: expected ${expected[prefix].name}, got ${prefix < observed.length ? observed[prefix].name : "end of trace"}` : "";
  return {
    value: helpers.ratio(credit, expected.length),
    details: `referenceSteps=${expected.length} observedSteps=${observed.length} correctPrefix=${prefix} credit=${credit.toFixed(2)}${diverged}`,
  };
}

/**
 * trajectory-ndtw: Normalized dynamic time warping similarity to a reference state trajectory.
 * @param {any} trace
 * @param {Record<string, any>} config
 * @returns {any}
 */
function gradeTrajectoryNdtw(trace, config) {
  const isRecord = value => value !== null && typeof value === "object" && !Array.isArray(value);
  const candidates = [
    trace,
    trace.trajectoryIR,
    trace.trajectoryIr,
    trace.ir,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectoryIR : null,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectoryIr : null,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectory : null,
    isRecord(trace.agentOutput) ? trace.agentOutput : null,
  ].filter(isRecord);
  const ir = candidates.find(value => isRecord(value.reference) && Array.isArray(value.reference.states));
  if (!ir || ir.reference.states.length === 0) {
    return { value: null, passed: null, message: "not applicable: no reference state trajectory" };
  }
  const isId = value => typeof value === "string" && value !== "";
  const reference = ir.reference.states;
  if (!reference.every(isId)) {
    return { value: null, passed: null, message: "unavailable: reference states must be non-empty state ids" };
  }
  if (!Array.isArray(ir.events) || !ir.events.every(isRecord)) {
    return { value: null, passed: null, message: "unavailable: trace lacks well-formed events" };
  }
  const path = ir.events.filter(event => event.kind === "state_change").map(event => event.ref);
  if (path.length === 0) {
    return { value: null, passed: null, message: "unavailable: trace has no state_change events" };
  }
  if (!path.every(isId)) {
    return { value: null, passed: null, message: "unavailable: state_change event is missing a state id" };
  }

  const threshold = 1;
  let previous = [0, ...path.map(() => Infinity)];
  for (let i = 1; i <= reference.length; i += 1) {
    const current = [Infinity];
    for (let j = 1; j <= path.length; j += 1) {
      const cost = reference[i - 1] === path[j - 1] ? 0 : 1;
      current.push(cost + Math.min(previous[j], current[j - 1], previous[j - 1]));
    }
    previous = current;
  }
  const distance = previous[path.length];
  const value = Math.exp(-distance / (reference.length * threshold));
  return {
    value: Math.min(1, Math.max(0, value)),
    details: `referenceStates=${reference.length} observedStates=${path.length} dtwDistance=${distance}`,
  };
}

/**
 * code-search-recall: Fraction of reference patch files located during the run.
 * @param {any} trace
 * @param {Record<string, any>} config
 * @returns {any}
 */
function gradeCodeSearchRecall(trace, config) {
  const isRecord = value => value !== null && typeof value === "object" && !Array.isArray(value);
  const candidates = [
    trace,
    trace.trajectoryIR,
    trace.trajectoryIr,
    trace.ir,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectoryIR : null,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectoryIr : null,
    isRecord(trace.agentOutput) ? trace.agentOutput.trajectory : null,
    isRecord(trace.agentOutput) ? trace.agentOutput : null,
  ].filter(isRecord);
  const ir = candidates.find(value => isRecord(value.reference) && isRecord(value.reference.patch) && Array.isArray(value.reference.patch.files));
  if (!ir || ir.reference.patch.files.length === 0) {
    return { value: null, passed: null, message: "not applicable: no reference patch files" };
  }
  const normalize = value => value.trim().replace(/^\.\//, "").replace(/^\/+/, "");
  const required = ir.reference.patch.files.map(file => (typeof file === "string" ? file : isRecord(file) && typeof file.path === "string" ? file.path : ""));
  if (required.some(file => normalize(file) === "")) {
    return { value: null, passed: null, message: "unavailable: reference patch file is missing a path" };
  }
  if (!Array.isArray(ir.resources) || !ir.resources.every(isRecord)) {
    return { value: null, passed: null, message: "unavailable: trace lacks well-formed resources" };
  }

  const located = new Set();
  for (const resource of ir.resources) {
    if (resource.kind === "file" && typeof resource.uri === "string" && normalize(resource.uri) !== "") located.add(normalize(resource.uri));
  }
  const requiredSet = [...new Set(required.map(normalize))];
  const missed = requiredSet.filter(file => !located.has(file));
  const found = requiredSet.length - missed.length;
  return {
    value: helpers.ratio(found, requiredSet.length),
    details: `referenceFiles=${requiredSet.length} located=${found}${missed.length === 0 ? "" : `; never located: ${missed.slice(0, 5).join(", ")}`}`,
  };
}

/** @type {Record<string, {name: string, description: string, unit: string, direction: string, threshold?: number, min?: number, max?: number}>} */
const TRAJECTORY_GRADER_META = {
  "policy-near-miss": { name: "Policy Near-Miss Rate", description: "Fraction of successful traces that left guard or policy objectives unsatisfied", unit: "ratio", direction: "lower_is_better", min: 0, max: 1 },
  "skill-constraint-coverage": { name: "Skill Constraint Coverage", description: "Fraction of configured skill constraints that were exercised and passed", unit: "ratio", direction: "higher_is_better", min: 0, max: 1 },
  "exploration-error": { name: "Exploration Error", description: "Unmet objectives attributable to insufficient search", unit: "ratio", direction: "lower_is_better", min: 0, max: 1 },
  "exploitation-error": { name: "Exploitation Error", description: "Unmet objectives attributable to gathered evidence that was never used", unit: "ratio", direction: "lower_is_better", min: 0, max: 1 },
  "state-revisit-probability-rep": { name: "State Revisit Probability REP", description: "Fraction of canonical state visits that revisit an already visited state", unit: "ratio", direction: "lower_is_better", min: 0, max: 1 },
  "recurrence-determinism": { name: "Recurrence Determinism (RQA DET)", description: "RQA DET: fraction of recurrent points forming diagonal (repeated-subsequence) lines", unit: "ratio", direction: "lower_is_better", min: 0, max: 1 },
  "recurrence-laminarity": { name: "Recurrence Laminarity (RQA LAM)", description: "RQA LAM: fraction of recurrent points forming vertical (stagnation) lines", unit: "ratio", direction: "lower_is_better", min: 0, max: 1 },
  "recurrence-trapping-time": { name: "Recurrence Trapping Time (RQA TT)", description: "RQA TT: average length of vertical recurrence lines", unit: "steps", direction: "lower_is_better", min: 0 },
  "recurrence-rate": { name: "Recurrence Rate (RQA RR)", description: "RQA RR: density of recurrent state pairs across the run", unit: "ratio", direction: "lower_is_better", min: 0, max: 1 },
  "event-entropy-rate": { name: "Event Entropy Rate", description: "Normalized conditional Shannon entropy rate of the ordered event sequence", unit: "ratio", direction: "higher_is_better", min: 0, max: 1 },
  "lempel-ziv-trajectory-complexity": { name: "Lempel-Ziv Trajectory Complexity", description: "Normalized LZ76 complexity of the canonical event sequence", unit: "ratio", direction: "higher_is_better", min: 0, max: 1 },
  "tool-output-consumption-rate": { name: "Tool Output Consumption Rate", description: "Fraction of tool outputs referenced by a later action", unit: "ratio", direction: "higher_is_better", min: 0, max: 1 },
  "end-to-end-lineage-completeness": { name: "End-to-End Lineage Completeness", description: "Fraction of final outputs traceable to tool or observation evidence roots", unit: "ratio", direction: "higher_is_better", min: 0, max: 1 },
  "action-provenance-coverage": { name: "Action Provenance Coverage", description: "Fraction of consequential actions with a provenance path to tool or observation evidence", unit: "ratio", direction: "higher_is_better", min: 0, max: 1 },
  "premature-termination-gap": { name: "Premature Termination Gap", description: "Declared completion conditions still unsatisfied at termination", unit: "count", direction: "lower_is_better", min: 0 },
  "evidence-saturation-stopping-lag": { name: "Evidence Saturation Stopping Lag", description: "Events elapsed between all objectives being satisfied and the run stopping", unit: "count", direction: "lower_is_better", min: 0 },
  "dependency-order-violation-rate": { name: "Dependency Order Violation Rate", description: "Fraction of dependent objectives satisfied before their prerequisites", unit: "ratio", direction: "lower_is_better", min: 0, max: 1 },
  "objective-coverage": { name: "Objective Coverage", description: "Fraction of declared objectives that were completed", unit: "ratio", direction: "higher_is_better", min: 0, max: 1 },
  "grounding-accuracy": { name: "Grounding Accuracy", description: "Fraction of actions that were valid in the state in which they were issued", unit: "ratio", direction: "higher_is_better", min: 0, max: 1 },
  "tool-wise-score": { name: "Tool-Wise Score", description: "Longest correct execution prefix against a reference trajectory, with parameter credit", unit: "ratio", direction: "higher_is_better", min: 0, max: 1 },
  "trajectory-ndtw": { name: "Trajectory nDTW", description: "Normalized dynamic time warping similarity to a reference state trajectory", unit: "ratio", direction: "higher_is_better", min: 0, max: 1 },
  "code-search-recall": { name: "Code Search Recall", description: "Fraction of reference patch files located during the run", unit: "ratio", direction: "higher_is_better", min: 0, max: 1 },
};

/** @type {Record<string, (trace: any, config?: Record<string, any>) => any>} */
const TRAJECTORY_GRADERS = {
  "policy-near-miss": withTrajectoryLimits(gradePolicyNearMiss),
  "skill-constraint-coverage": withTrajectoryLimits(gradeSkillConstraintCoverage),
  "exploration-error": withTrajectoryLimits(gradeExplorationError),
  "exploitation-error": withTrajectoryLimits(gradeExploitationError),
  "state-revisit-probability-rep": withTrajectoryLimits(gradeStateRevisitProbabilityRep),
  "recurrence-determinism": withTrajectoryLimits(gradeRecurrenceDeterminism),
  "recurrence-laminarity": withTrajectoryLimits(gradeRecurrenceLaminarity),
  "recurrence-trapping-time": withTrajectoryLimits(gradeRecurrenceTrappingTime),
  "recurrence-rate": withTrajectoryLimits(gradeRecurrenceRate),
  "event-entropy-rate": withTrajectoryLimits(gradeEventEntropyRate),
  "lempel-ziv-trajectory-complexity": withTrajectoryLimits(gradeLempelZivTrajectoryComplexity),
  "tool-output-consumption-rate": withTrajectoryLimits(gradeToolOutputConsumptionRate),
  "end-to-end-lineage-completeness": withTrajectoryLimits(gradeEndToEndLineageCompleteness),
  "action-provenance-coverage": withTrajectoryLimits(gradeActionProvenanceCoverage),
  "premature-termination-gap": withTrajectoryLimits(gradePrematureTerminationGap),
  "evidence-saturation-stopping-lag": withTrajectoryLimits(gradeEvidenceSaturationStoppingLag),
  "dependency-order-violation-rate": withTrajectoryLimits(gradeDependencyOrderViolationRate),
  "objective-coverage": withTrajectoryLimits(gradeObjectiveCoverage),
  "grounding-accuracy": withTrajectoryLimits(gradeGroundingAccuracy),
  "tool-wise-score": withTrajectoryLimits(gradeToolWiseScore),
  "trajectory-ndtw": withTrajectoryLimits(gradeTrajectoryNdtw),
  "code-search-recall": withTrajectoryLimits(gradeCodeSearchRecall),
};

module.exports = {
  MAX_TRAJECTORY_ITEMS,
  MAX_CONSTRAINT_TEXT_LENGTH,
  findOversizedTrajectoryCollection,
  TRAJECTORY_GRADER_META,
  TRAJECTORY_GRADERS,
  gradePolicyNearMiss,
  gradeSkillConstraintCoverage,
  gradeExplorationError,
  gradeExploitationError,
  gradeStateRevisitProbabilityRep,
  gradeRecurrenceDeterminism,
  gradeRecurrenceLaminarity,
  gradeRecurrenceTrappingTime,
  gradeRecurrenceRate,
  gradeEventEntropyRate,
  gradeLempelZivTrajectoryComplexity,
  gradeToolOutputConsumptionRate,
  gradeEndToEndLineageCompleteness,
  gradeActionProvenanceCoverage,
  gradePrematureTerminationGap,
  gradeEvidenceSaturationStoppingLag,
  gradeDependencyOrderViolationRate,
  gradeObjectiveCoverage,
  gradeGroundingAccuracy,
  gradeToolWiseScore,
  gradeTrajectoryNdtw,
  gradeCodeSearchRecall,
};
