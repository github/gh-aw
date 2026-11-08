// @ts-check

const { classifySafeOutputResult, pickOutcomeField } = require("./safe_outputs_status.cjs");
const { getCreatedTemporaryId, extractTemporaryIdReferences } = require("./temporary_id.cjs");
const { isDependencyDelegatedType } = require("./safe_output_submission.cjs");

const OUTPUT_ID_PATTERN = /^so_[A-Za-z0-9_-]{12}$/;

/**
 * Build one stable graph for explicit output dependencies and legacy temporary IDs.
 * Invalid explicit graphs never fall back to executing their handlers.
 *
 * @param {Array<Record<string, any>>} messages
 */
function buildSafeOutputDependencyPlan(messages) {
  /** @type {Map<string, number[]>} */
  const outputs = new Map();
  const temporaryProducers = new Map();
  /** @type {Map<number, string>} */
  const invalid = new Map();
  const prerequisites = messages.map(() => new Set());
  const ordering = messages.map(() => new Set());
  messages.forEach((message, index) => {
    if (message.output_id !== undefined) {
      if (typeof message.output_id !== "string" || !OUTPUT_ID_PATTERN.test(message.output_id)) {
        invalid.set(index, "Malformed safe-output output_id");
      } else {
        const producers = outputs.get(message.output_id) || [];
        producers.push(index);
        outputs.set(message.output_id, producers);
      }
    }
    const temporaryId = getCreatedTemporaryId(message);
    if (temporaryId && !temporaryProducers.has(temporaryId)) temporaryProducers.set(temporaryId, index);
  });
  for (const producers of outputs.values()) {
    if (producers.length > 1) {
      for (const index of producers) invalid.set(index, "Duplicate safe-output output_id");
    }
  }
  messages.forEach((message, index) => {
    const dependencies = message["depends-on"] !== undefined ? message["depends-on"] : message.depends_on;
    if (message["depends-on"] !== undefined && message.depends_on !== undefined) {
      invalid.set(index, "Provide only one depends-on alias");
    }
    if (dependencies !== undefined) {
      const ids = typeof dependencies === "string" ? [dependencies] : dependencies;
      if (!Array.isArray(ids) || ids.length === 0 || ids.some(id => typeof id !== "string" || !OUTPUT_ID_PATTERN.test(id)) || new Set(ids).size !== ids.length) {
        invalid.set(index, "depends-on must contain safe-output output IDs");
      } else {
        for (const id of ids) {
          const producers = outputs.get(id);
          if (!producers || producers.length !== 1) {
            invalid.set(index, "Missing or ambiguous safe-output prerequisite");
          } else if (producers[0] === index) {
            invalid.set(index, "Safe output cannot depend on itself");
          } else {
            prerequisites[index].add(producers[0]);
            ordering[index].add(producers[0]);
          }
        }
      }
    }
    const temporaryReferences = message.type === "create_issue" ? extractTemporaryIdReferences({ blocked_by: message.blocked_by }) : new Set();
    for (const temporaryId of temporaryReferences) {
      const producer = temporaryProducers.get(temporaryId);
      if (producer !== undefined && producer !== index) ordering[index].add(producer);
    }
  });
  /** @type {Array<Array<number>>} */
  const dependents = messages.map(() => []);
  const inDegree = ordering.map(dependencies => dependencies.size);
  ordering.forEach((dependencies, index) => {
    for (const producer of dependencies) dependents[producer].push(index);
  });
  const ready = [];
  inDegree.forEach((degree, index) => {
    if (degree === 0) ready.push(index);
  });
  const order = [];
  while (ready.length > 0) {
    const index = ready.shift();
    order.push(index);
    for (const dependent of dependents[index]) {
      if (--inDegree[dependent] === 0) {
        const position = ready.findIndex(candidate => candidate > dependent);
        ready.splice(position < 0 ? ready.length : position, 0, dependent);
      }
    }
  }
  const cyclic = order.length !== messages.length;
  const visited = new Set(order);
  if (cyclic) {
    function hasPath(start, target) {
      const pending = [start];
      const reached = new Set();
      while (pending.length > 0) {
        const current = pending.pop();
        if (current === target) return true;
        if (reached.has(current)) continue;
        reached.add(current);
        pending.push(...ordering[current]);
      }
      return false;
    }
    prerequisites.forEach((dependencies, index) => {
      if ([...dependencies].some(prerequisite => hasPath(prerequisite, index))) {
        invalid.set(index, "Cyclic safe-output dependency");
      }
    });
  }
  messages.forEach((_, index) => {
    if (!visited.has(index)) order.push(index);
  });
  return { order, prerequisites, invalid, cyclic };
}

/**
 * A processing wrapper can say success even when its handler staged/delegated the
 * effect or a legacy fanout returned individual failures. Neither confirms success.
 *
 * @param {any} record
 */
function isConfirmedDependencySuccess(record) {
  if (isDependencyDelegatedType(record?.type)) return false;
  // @safe-outputs-exempt USE-003: reads the staged flag only to reject unconfirmed success; renders no staged preview
  if (process.env.GH_AW_SAFE_OUTPUTS_STAGED === "true") return false;
  if (!record || record.success !== true || classifySafeOutputResult(record) !== "success") return false;
  if (pickOutcomeField(record, "staged") || pickOutcomeField(record, "queued") || pickOutcomeField(record, "buffered")) return false;
  const result = record.result;
  if (Array.isArray(result)) {
    return result.length > 0 && result.every(item => item && typeof item === "object" && isConfirmedDependencySuccess({ success: item.success !== false, result: item }));
  }
  if (!result || typeof result !== "object" || result.error || (result.success !== undefined && result.success !== true)) return false;
  return true;
}

/**
 * @param {ReturnType<typeof buildSafeOutputDependencyPlan>} plan
 * @param {number} index
 * @param {Array<any>} results
 * @param {boolean} allowWaiting
 * @returns {{state: "ready"|"waiting"|"failed", errorCode?: string, error?: string}}
 */
function checkSafeOutputDependencies(plan, index, results, allowWaiting) {
  if (plan.invalid.has(index)) return { state: "failed", errorCode: "safe_output_dependency_invalid", error: plan.invalid.get(index) };
  let waiting = false;
  for (const prerequisite of plan.prerequisites[index]) {
    const records = results.filter(result => result.messageIndex === prerequisite);
    if (records.length > 1) return { state: "failed", errorCode: "safe_output_dependency_failed", error: "Ambiguous safe-output prerequisite result" };
    const record = records[0];
    if (isConfirmedDependencySuccess(record)) continue;
    if (allowWaiting && (!record || classifySafeOutputResult(record) === "deferred")) {
      waiting = true;
      continue;
    }
    if (record?.type === "ledger_append" && pickOutcomeField(record, "queued")) {
      return {
        state: "failed",
        errorCode: "safe_output_dependency_failed",
        error: "Ledger prerequisite is queued, not persisted. Submit related ledger operations sequentially without depends-on; use temporary record IDs for same-batch references. Durability requires push_ledger_changes to succeed.",
      };
    }
    const skipReason = pickOutcomeField(record, "skipReason") || (pickOutcomeField(record, "skipped") && pickOutcomeField(record, "reason"));
    return { state: "failed", errorCode: "safe_output_dependency_failed", error: `Safe-output prerequisite did not complete successfully${skipReason ? `: ${skipReason}` : ""}` };
  }
  return { state: waiting ? "waiting" : "ready" };
}

module.exports = { buildSafeOutputDependencyPlan, checkSafeOutputDependencies, isConfirmedDependencySuccess };
