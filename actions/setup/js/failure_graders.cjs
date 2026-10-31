// @ts-check

const { stableStringify, isRecord, createSessionLookups } = require("./grader_session.cjs");

/** @typedef {import("./types/unified_session").FailureObservation} FailureObservation */
/** @typedef {ReturnType<typeof createSessionLookups>} SessionLookups */

const FAILURE_CATEGORY_IDS = ["progress-stagnation", "ineffective-retries", "premature-termination", "completion-misrepresentation"];
const MAX_FAILURE_OBSERVATIONS = 32;
const MAX_OBJECTIVES = 5000;
const MAX_SIGNATURE_LENGTH = 16384;
const SAFE_FAILURE_SUMMARIES = new Set([
  "An unchanged tool operation failed again.",
  "Repeated unchanged tool failures show no observed progress.",
  "A success claim lacks a complete scoped test result.",
  "A test success claim contradicts the scoped execution result.",
  "A task completion claim lacks complete scoped objective evidence.",
  "A task completion claim contradicts recorded unmet objectives.",
  "The session ended without a result for an outstanding tool call.",
  "The session ended without scoped objective completion evidence.",
  "The session ended with explicitly recorded unmet objectives.",
]);

/**
 * Retain only the bounded derived-evidence wire contract when projecting a
 * historical grader result; arbitrary custom diagnostic payloads remain private.
 * @param {any} diagnostics
 */
function projectFailureDiagnostics(diagnostics) {
  if (!isRecord(diagnostics) || !Array.isArray(diagnostics.failureObservations)) return undefined;
  const observations = [];
  const count = value => Number.isSafeInteger(value) && value >= 0;
  for (const item of diagnostics.failureObservations.slice(0, MAX_FAILURE_OBSERVATIONS)) {
    if (
      !isRecord(item) ||
      item.schemaVersion !== 1 ||
      typeof item.categoryId !== "string" ||
      !/^[a-z][a-z0-9-]{0,63}$/.test(item.categoryId) ||
      !["warning", "error"].includes(item.severity) ||
      !["low", "medium", "high"].includes(item.confidence) ||
      !["observed", "missing"].includes(item.evidenceState) ||
      !["recoverable", "recovered", "unknown"].includes(item.recoverability) ||
      !Array.isArray(item.evidence)
    )
      continue;
    const evidence = [];
    for (const reference of item.evidence.slice(0, 4)) {
      if (!isRecord(reference) || !count(reference.eventIndex) || typeof reference.type !== "string" || reference.type.length > 128 || !isRecord(reference.provenance)) continue;
      const source = reference.provenance;
      if (typeof source.component !== "string" || typeof source.phase !== "string") continue;
      evidence.push({
        eventIndex: reference.eventIndex,
        type: reference.type,
        provenance: {
          component: source.component,
          phase: source.phase,
          ...(typeof source.path === "string" ? { path: source.path } : {}),
          ...(count(source.index) ? { index: source.index } : {}),
          ...(typeof source.attemptId === "string" ? { attemptId: source.attemptId } : {}),
          ...(typeof source.startupAttemptId === "string" ? { startupAttemptId: source.startupAttemptId } : {}),
        },
      });
    }
    observations.push({
      schemaVersion: 1,
      categoryId: item.categoryId,
      severity: item.severity,
      confidence: item.confidence,
      evidenceState: item.evidenceState,
      recoverability: item.recoverability,
      evidence,
      summary: SAFE_FAILURE_SUMMARIES.has(item.summary) ? item.summary : "Derived failure evidence was recorded.",
    });
  }
  const coverage = isRecord(diagnostics.coverage) ? diagnostics.coverage : {};
  return {
    failureObservations: observations,
    ...(count(diagnostics.totalFailureObservations) ? { totalFailureObservations: diagnostics.totalFailureObservations } : {}),
    failureObservationsTruncated: diagnostics.failureObservationsTruncated === true || diagnostics.failureObservations.length > MAX_FAILURE_OBSERVATIONS,
    coverage: {
      ...(count(coverage.assessed) ? { assessed: coverage.assessed } : {}),
      ...(count(coverage.missing) ? { missing: coverage.missing } : {}),
      ...(count(coverage.scopeCount) ? { scopeCount: coverage.scopeCount } : {}),
      ...(["partial", "complete"].includes(coverage.state) ? { state: coverage.state } : {}),
    },
  };
}

/** A missing completion is not a failed execution. @param {any} call */
function outcome(call) {
  if (!call.completed) return "missing";
  if (
    call.success === false ||
    call.isError === true ||
    call.is_error === true ||
    ["error", "failure", "failed"].includes(call.status) ||
    call.error === true ||
    isRecord(call.error) ||
    (typeof call.error === "string" && call.error.length > 0) ||
    (typeof call.exitCode === "number" && Number.isFinite(call.exitCode) && call.exitCode !== 0)
  )
    return "failure";
  if (call.success === true || call.status === "success" || call.exitCode === 0) return "success";
  return "missing";
}

/** @param {any} call */
function signature(call) {
  if (!call.name || call.arguments === undefined || call.inputTruncated === true) return null;
  const value = stableStringify([call.name, call.arguments]);
  return value.length <= MAX_SIGNATURE_LENGTH ? value : null;
}

/** @param {any} call */
function isTestCall(call) {
  const command = call.arguments?.command;
  return (
    /^(run_tests|run_test|test)$/i.test(call.name ?? "") ||
    (typeof command === "string" && command.length <= MAX_SIGNATURE_LENGTH && !/[;&|`$\r\n<>]/.test(command) && /^(?:npm (?:run )?test|npx vitest(?: run)?|go test|pytest|python -m pytest)(?:\s|$)/.test(command.trim()))
  );
}

/**
 * One indexed, data-only pass. All statements remain claims; only scoped
 * execution results or explicit objective state corroborate them.
 * @param {import("./types/unified_session").UnifiedSession} session
 * @param {SessionLookups} lookups
 */
function collectFailureObservations(session, lookups) {
  /** @type {Record<string, { observations: FailureObservation[], total: number, observed: number, missing: number, assessed: number }>} */
  const buckets = Object.fromEntries(FAILURE_CATEGORY_IDS.map(id => [id, { observations: [], total: 0, observed: 0, missing: 0, assessed: 0 }]));
  /** @type {Map<string, any>} */
  const scopes = new Map();
  const stateFor = key => {
    let state = scopes.get(key);
    if (!state) {
      state = {
        pending: new Set(),
        last: null,
        repeated: 0,
        failures: new Map(),
        tests: new Map(),
        failedTests: new Map(),
        missingTests: 0,
        testClaims: [],
        objectives: null,
        objectiveIndex: null,
        objectiveObservations: [],
        terminalSeen: false,
      };
      scopes.set(key, state);
    }
    return state;
  };
  const reference = index => {
    const event = session[index];
    const { component, phase, path, index: sourceIndex, attemptId, startupAttemptId } = event.provenance;
    return {
      eventIndex: index,
      type: event.type,
      provenance: {
        component,
        phase,
        ...(path !== undefined ? { path } : {}),
        ...(sourceIndex !== undefined ? { index: sourceIndex } : {}),
        ...(attemptId !== undefined ? { attemptId } : {}),
        ...(startupAttemptId !== undefined ? { startupAttemptId } : {}),
      },
    };
  };
  /** @param {string} id @param {number[]} indices @param {boolean} missing @param {string} summary @returns {FailureObservation | undefined} */
  const add = (id, indices, missing, summary) => {
    const bucket = buckets[id];
    bucket.total++;
    bucket[missing ? "missing" : "observed"]++;
    if (bucket.observations.length >= MAX_FAILURE_OBSERVATIONS) return undefined;
    /** @type {FailureObservation} */
    const observation = {
      schemaVersion: 1,
      categoryId: id,
      severity: id === "completion-misrepresentation" && !missing ? "error" : "warning",
      confidence: missing ? "low" : id === "completion-misrepresentation" ? "high" : "medium",
      evidenceState: missing ? "missing" : "observed",
      recoverability: missing ? "unknown" : "recoverable",
      evidence: [...new Set(indices)].slice(0, 4).map(reference),
      summary,
    };
    bucket.observations.push(observation);
    return observation;
  };
  const recover = (observations, index) => {
    for (const observation of observations) {
      if (!observation || observation.recoverability === "recovered") continue;
      observation.recoverability = "recovered";
      observation.evidence = [...observation.evidence.slice(0, 3), reference(index)];
    }
  };
  /** @type {Map<number, any[]>} */
  const starts = new Map();
  /** @type {Map<number, any[]>} */
  const completions = new Map();
  const callStates = new Map();
  for (const call of lookups.toolCalls) {
    if (Number.isInteger(call.eventIndex)) {
      const list = starts.get(call.eventIndex) ?? [];
      list.push(call);
      starts.set(call.eventIndex, list);
    }
    if (Number.isInteger(call.completionEventIndex)) {
      const list = completions.get(call.completionEventIndex) ?? [];
      list.push(call);
      completions.set(call.completionEventIndex, list);
    }
  }
  for (let index = 0; index < session.length; index++) {
    const event = session[index];
    if (event.provenance.phase !== "agent" || event.type === "failure.observation" || event.type.startsWith("grader.")) continue;
    const state = starts.has(index) || ["trajectory.ir", "session.task_complete", "assistant.message", "session.shutdown", "session.result"].includes(event.type) ? stateFor(lookups.scopeOf(event)) : null;
    for (const call of starts.get(index) ?? []) {
      state.pending.add(call);
      state.terminalSeen = false;
      callStates.set(call, state);
    }
    for (const call of completions.get(index) ?? []) {
      // Use the start's scope: staged files may differ, compatible resumes may not.
      const callState = callStates.get(call);
      callState.pending.delete(call);
      const result = outcome(call);
      const testCall = isTestCall(call);
      const key = result !== "success" || callState.failures.size > 0 || testCall ? signature(call) : null;
      const rawFailureKey = result === "failure" ? stableStringify([call.status, call.exitCode, call.error, call.result ?? call.output]) : null;
      const failureKey = rawFailureKey !== null && rawFailureKey.length <= MAX_SIGNATURE_LENGTH ? rawFailureKey : null;
      for (const id of ["progress-stagnation", "ineffective-retries"]) {
        buckets[id].assessed++;
        if (result === "missing" || (result === "failure" && (key === null || failureKey === null))) buckets[id].missing++;
      }
      if (result === "failure" && key !== null && failureKey !== null) {
        callState.repeated = callState.last?.key === key && callState.last?.result === "failure" && failureKey === callState.last.failureKey ? callState.repeated + 1 : 1;
        const history = callState.failures.get(key) ?? [];
        if (callState.repeated >= 2) {
          history.push(add("ineffective-retries", [callState.last.index, index], false, "An unchanged tool operation failed again."));
        }
        if (callState.repeated >= 3) {
          history.push(add("progress-stagnation", [callState.last.index, index], false, "Repeated unchanged tool failures show no observed progress."));
        }
        // Retain only bounded references; totals are maintained separately.
        callState.failures.set(key, history.filter(Boolean).slice(0, MAX_FAILURE_OBSERVATIONS * 2));
      } else {
        callState.repeated = 0;
        if (result === "success" && key !== null) recover(callState.failures.get(key) ?? [], index);
      }
      callState.last = { key, result, failureKey, index };
      if (testCall) {
        const testKey = key ?? index;
        if (callState.tests.get(testKey)?.result === "missing") callState.missingTests--;
        callState.tests.set(testKey, { result, index });
        if (result === "missing") callState.missingTests++;
        if (result === "failure") callState.failedTests.set(testKey, { index });
        else callState.failedTests.delete(testKey);
        if (result === "success") {
          for (const claim of callState.testClaims) {
            claim.failedKeys.delete(testKey);
            if (claim.failedKeys.size === 0) recover([claim.observation], index);
          }
        }
      }
    }
    if (event.type === "trajectory.ir") {
      const objectives = event.data.objectives;
      const previousObjectives = state.objectives;
      state.objectives =
        Array.isArray(objectives) &&
        objectives.length > 0 &&
        objectives.length <= MAX_OBJECTIVES &&
        objectives.every(objective => isRecord(objective) && (objective.satisfiedAtEventIndex === null || (Number.isSafeInteger(objective.satisfiedAtEventIndex) && objective.satisfiedAtEventIndex >= 0)))
          ? {
              unmet: objectives.filter(objective => objective.satisfiedAtEventIndex === null).length,
              identity: objectives.every(objective => typeof objective.id === "string") ? stableStringify(objectives.map(objective => objective.id).sort()) : null,
            }
          : null;
      state.objectiveIndex = index;
      state.terminalSeen = false;
      if (state.objectives?.identity !== previousObjectives?.identity) state.objectiveObservations = [];
      if (state.objectives?.unmet === 0 && state.objectives.identity !== null) recover(state.objectiveObservations, index);
    }
    const structuredClaim = event.type === "session.task_complete" && event.data.success === true;
    const content = event.type === "assistant.message" && event.data.partial !== true && event.data.delta !== true && typeof event.data.content === "string" ? event.data.content.trim() : "";
    const testsClaim = /^All tests passed[.!]?$/i.test(content);
    const taskClaim = structuredClaim || /^(?:The task|All tasks) completed successfully[.!]?$/i.test(content);
    if (testsClaim || taskClaim) {
      buckets["completion-misrepresentation"].assessed++;
      if (testsClaim) {
        if (state.failedTests.size) {
          const test = state.failedTests.values().next().value;
          const observation = add("completion-misrepresentation", [test.index, index], false, "A test success claim contradicts the scoped execution result.");
          if (observation) state.testClaims.push({ observation, failedKeys: new Set(state.failedTests.keys()) });
        } else if (!state.tests.size || state.missingTests > 0 || state.pending.size) {
          add("completion-misrepresentation", [index], true, "A success claim lacks a complete scoped test result.");
        }
      } else if (!state.objectives || state.pending.size) {
        add("completion-misrepresentation", [index, ...(state.objectiveIndex !== null ? [state.objectiveIndex] : [])], true, "A task completion claim lacks complete scoped objective evidence.");
      } else if (state.objectives.unmet > 0) {
        const observation = add("completion-misrepresentation", [state.objectiveIndex, index], false, "A task completion claim contradicts recorded unmet objectives.");
        if (observation) state.objectiveObservations.push(observation);
      }
    }
    if (["session.shutdown", "session.result"].includes(event.type)) {
      if (state.terminalSeen) continue;
      state.terminalSeen = true;
      buckets["premature-termination"].assessed++;
      if (state.pending.size) {
        const pending = state.pending.values().next().value;
        add("premature-termination", [pending.eventIndex, index], true, "The session ended without a result for an outstanding tool call.");
      } else if (!state.objectives) {
        add("premature-termination", [index], true, "The session ended without scoped objective completion evidence.");
      } else if (state.objectives.unmet > 0) {
        const observation = add("premature-termination", [state.objectiveIndex, index], false, "The session ended with explicitly recorded unmet objectives.");
        if (observation) state.objectiveObservations.push(observation);
      }
    }
  }
  for (const state of scopes.values()) {
    for (const call of state.pending) {
      for (const id of ["progress-stagnation", "ineffective-retries"]) buckets[id].missing++;
    }
  }
  return Object.fromEntries(
    FAILURE_CATEGORY_IDS.map(id => {
      const bucket = buckets[id];
      return [
        id,
        {
          value: bucket.observed > 0 ? bucket.observed : bucket.assessed > 0 && bucket.missing === 0 ? 0 : null,
          ...(bucket.observed === 0 && (bucket.assessed === 0 || bucket.missing > 0) ? { message: "unavailable: essential scoped evidence is missing" } : {}),
          diagnostics: {
            failureObservations: bucket.observations,
            totalFailureObservations: bucket.total,
            failureObservationsTruncated: bucket.total > bucket.observations.length,
            coverage: { assessed: bucket.assessed, missing: bucket.missing, scopeCount: scopes.size, state: bucket.missing > 0 || bucket.assessed === 0 ? "partial" : "complete" },
          },
        },
      ];
    })
  );
}

module.exports = { collectFailureObservations, projectFailureDiagnostics, FAILURE_CATEGORY_IDS, MAX_FAILURE_OBSERVATIONS };
