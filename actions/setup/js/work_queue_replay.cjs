// @ts-check

const { CURRENT_VERSION, upgradeTransactions } = require("./work_queue_codemods.cjs");
const TRANSACTION_KINDS = new Set(["Work", "Claim", "ClaimCancellation", "Completion", "WorkCancellation"]);
const TRANSACTION_FIELDS = {
  Work: ["version", "kind", "work_id", "work", "sequence"],
  Claim: ["version", "kind", "work_id", "claim_id", "run_id"],
  ClaimCancellation: ["version", "kind", "work_id", "claim_id"],
  Completion: ["version", "kind", "work_id", "claim_id", "attempt_id"],
  WorkCancellation: ["version", "kind", "work_id"],
};

/**
 * Log coordinator diagnostics when DEBUG enables this module. Transaction
 * identifiers are deliberately omitted because they may contain submitted data.
 * @param {string} message
 * @param {Record<string, string | number>} [details]
 */
function debugLog(message, details = {}) {
  const debug = process.env.DEBUG || "";
  if (debug === "*" || debug.includes("work_queue")) {
    console.error(`[work_queue_replay] ${message} ${JSON.stringify(details)}`);
  }
}

/**
 * @typedef {{version: number, kind: string, work_id: string, work?: Record<string, unknown>,
 *    sequence?: number, claim_id?: string, run_id?: string, attempt_id?: string, outcome?: string}} WorkQueueTransaction
 */

/**
 * @param {unknown} transaction
 * @returns {asserts transaction is WorkQueueTransaction}
 */
function validateTransaction(transaction) {
  if (!transaction || typeof transaction !== "object" || Array.isArray(transaction)) {
    throw new TypeError("transaction must be an object");
  }

  /** @type {Record<string, unknown>} */
  const candidate = Object.assign(Object.create(null), transaction);
  if (candidate.version !== CURRENT_VERSION) {
    throw new TypeError("unsupported work queue transaction version");
  }
  if (typeof candidate.kind !== "string" || !TRANSACTION_KINDS.has(candidate.kind)) {
    throw new TypeError("transaction kind is invalid");
  }
  const required = TRANSACTION_FIELDS[candidate.kind];
  if (required.some(field => !Object.hasOwn(candidate, field)) || Object.keys(candidate).some(field => !required.includes(field) && !(candidate.kind === "Completion" && field === "outcome"))) {
    throw new TypeError(`transaction must contain exactly ${required.join(", ")}${candidate.kind === "Completion" ? " and optional outcome" : ""}`);
  }
  if (typeof candidate.work_id !== "string" || candidate.work_id.length === 0) {
    throw new TypeError("transaction work must be a non-empty string");
  }
  for (const field of ["claim_id", "attempt_id", "run_id"]) {
    const value = candidate[field];
    if (required.includes(field) && (typeof value !== "string" || value.length === 0)) {
      throw new TypeError(`transaction ${field} must be a non-empty string or null`);
    }
  }

  if (candidate.kind === "Work") {
    if (!candidate.work || typeof candidate.work !== "object" || Array.isArray(candidate.work)) throw new TypeError("work payload must be a JSON object");
    validatePayloadNumbers(candidate.work);
    if (typeof candidate.sequence !== "number" || !Number.isSafeInteger(candidate.sequence) || candidate.sequence < 1) throw new TypeError("work sequence must be a positive safe integer");
  }
  if (Object.hasOwn(candidate, "outcome") && typeof candidate.outcome !== "string") throw new TypeError("outcome must be a string");
}

function validatePayloadNumbers(value) {
  if (typeof value === "number" && (!Number.isFinite(value) || Math.abs(value) > Number.MAX_SAFE_INTEGER)) {
    throw new TypeError("work payload numbers must be finite and within the JavaScript-safe range; encode larger values as strings");
  }
  if (value && typeof value === "object") Object.values(value).forEach(validatePayloadNumbers);
}

/**
 * @param {WorkQueueTransaction} transaction
 * @returns {string}
 */
function transactionKey(transaction) {
  return canonicalJSON(transaction);
}

function canonicalJSON(value) {
  if (Array.isArray(value)) return `[${value.map(canonicalJSON).join(",")}]`;
  if (value !== null && typeof value === "object")
    return `{${Object.keys(value)
      .sort()
      .map(key => `${JSON.stringify(key)}:${canonicalJSON(value[key])}`)
      .join(",")}}`;
  return JSON.stringify(value);
}

/**
 * @param {WorkQueueTransaction} transaction
 * @returns {WorkQueueTransaction}
 */
function copyTransaction(transaction) {
  const copy = structuredClone(transaction);
  const freeze = value => {
    if (Array.isArray(value)) return Object.freeze(value.map(freeze));
    if (value && typeof value === "object")
      return Object.freeze(
        Object.fromEntries(
          Object.keys(value)
            .sort()
            .map(key => [key, freeze(value[key])])
        )
      );
    return value;
  };
  return freeze(copy);
}

/**
 * @param {WorkQueueTransaction[]} transactions
 * @returns {Map<string, WorkQueueTransaction>}
 */
function collectFacts(transactions) {
  if (!Array.isArray(transactions)) {
    throw new TypeError("transactions must be an array");
  }

  /** @type {Map<string, WorkQueueTransaction>} */
  const facts = new Map();
  upgradeTransactions(transactions).forEach((transaction, index) => {
    try {
      validateTransaction(transaction);
    } catch (error) {
      throw new TypeError(`invalid transaction at index ${index}: ${error instanceof Error ? error.message : String(error)}`);
    }
    const fact = copyTransaction(transaction);
    facts.set(transactionKey(fact), fact);
  });

  const works = new Set();
  const claims = new Map();
  const cancellations = new Set();
  const completions = new Map();
  const workCancellations = new Set();
  const attempts = new Set();

  for (const transaction of facts.values()) {
    switch (transaction.kind) {
      case "Work":
        if (works.has(transaction.work_id)) throw new TypeError(`work ${transaction.work_id} has conflicting transactions`);
        works.add(transaction.work_id);
        break;
      case "Claim":
        if (claims.has(transaction.claim_id)) {
          throw new TypeError(`claim ${transaction.claim_id} has conflicting transactions`);
        }
        claims.set(transaction.claim_id, transaction.work_id);
        break;
      case "ClaimCancellation":
        cancellations.add(transaction.claim_id);
        break;
      case "Completion":
        if (completions.has(transaction.work_id)) {
          throw new TypeError(`work ${transaction.work_id} has multiple terminal transactions`);
        }
        if (attempts.has(transaction.attempt_id)) {
          throw new TypeError(`attempt ${transaction.attempt_id} has multiple completions`);
        }
        completions.set(transaction.work_id, transaction);
        attempts.add(transaction.attempt_id);
        break;
      case "WorkCancellation":
        if (completions.has(transaction.work_id) || workCancellations.has(transaction.work_id)) {
          throw new TypeError(`work ${transaction.work_id} has multiple terminal transactions`);
        }
        workCancellations.add(transaction.work_id);
        break;
    }
  }

  for (const transaction of facts.values()) {
    if (transaction.kind !== "Work" && !works.has(transaction.work_id)) {
      throw new TypeError(`${transaction.kind} references missing work ${transaction.work_id}`);
    }
    if (transaction.kind === "ClaimCancellation" || transaction.kind === "Completion") {
      if (!claims.has(transaction.claim_id)) {
        throw new TypeError(`${transaction.kind} references missing claim ${transaction.claim_id}`);
      }
      if (claims.get(transaction.claim_id) !== transaction.work_id) {
        throw new TypeError(`${transaction.kind} references claim ${transaction.claim_id} for different work`);
      }
    }
  }

  for (const [work, completion] of completions) {
    if (workCancellations.has(work)) {
      throw new TypeError(`work ${work} has multiple terminal transactions`);
    }
    const activeClaims = [...claims.entries()]
      .filter(([claim, claimWork]) => claimWork === work && !cancellations.has(claim))
      .map(([claim]) => claim)
      .sort();
    if (activeClaims[0] !== completion.claim_id) {
      throw new TypeError(`completion for work ${work} does not belong to its winning claim`);
    }
  }

  return facts;
}

/**
 * Replays a transaction log into a deterministic projection. Exact duplicate
 * records are idempotent; arbitration uses the lexicographically smallest
 * active claim identity, independent of record order.
 * @param {WorkQueueTransaction[]} transactions
 */
function replayTransactions(transactions) {
  const facts = collectFacts(transactions);
  const works = new Set();
  const claims = new Map();
  const cancellations = new Set();
  const completions = new Map();
  const workCancellations = new Set();

  for (const transaction of facts.values()) {
    switch (transaction.kind) {
      case "Work":
        works.add(transaction.work_id);
        break;
      case "Claim":
        claims.set(transaction.claim_id, transaction.work_id);
        break;
      case "ClaimCancellation":
        cancellations.add(transaction.claim_id);
        break;
      case "Completion":
        completions.set(transaction.work_id, transaction);
        break;
      case "WorkCancellation":
        workCancellations.add(transaction.work_id);
        break;
    }
  }

  const sortedWorks = [...works].sort();
  const sortedClaims = [...claims.keys()].sort();
  const workState = new Map(
    sortedWorks.map(work => {
      const completion = completions.get(work);
      const activeClaims = [...claims.entries()]
        .filter(([claim, claimWork]) => claimWork === work && !cancellations.has(claim))
        .map(([claim]) => claim)
        .sort();
      const winner = completion ? completion.claim_id : workCancellations.has(work) ? null : (activeClaims[0] ?? null);
      const state = completion ? "completed" : workCancellations.has(work) ? "cancelled" : winner ? "claimed" : "available";
      return [work, { state, winner }];
    })
  );
  const getWorkState = work => {
    const state = workState.get(work);
    if (!state) throw new TypeError(`projection is missing work ${work}`);
    return state;
  };
  const claimState = Object.fromEntries(
    sortedClaims.map(claim => {
      const work = claims.get(claim);
      const workStatus = getWorkState(work);
      const state = cancellations.has(claim) || workStatus.state === "cancelled" ? "cancelled" : workStatus.winner === claim ? "effective" : "superseded";
      return [claim, state];
    })
  );

  debugLog("replay completed", { transactions: facts.size, works: works.size, claims: claims.size });
  return {
    work: Object.fromEntries(sortedWorks.map(work => [work, getWorkState(work).state])),
    winner: Object.fromEntries(sortedWorks.map(work => [work, getWorkState(work).winner])),
    claim: claimState,
    transactions: [...facts.values()].sort((left, right) => {
      const leftKey = transactionKey(left);
      const rightKey = transactionKey(right);
      return leftKey < rightKey ? -1 : leftKey > rightKey ? 1 : 0;
    }),
  };
}

/**
 * Applies intents in order against the current fact set. Invalid intents are
 * returned to the caller and never silently become durable facts.
 * @param {WorkQueueTransaction[]} transactions
 * @param {WorkQueueTransaction[]} intents
 */
function applyTransactions(transactions, intents) {
  const facts = collectFacts(transactions);
  if (!Array.isArray(intents)) {
    throw new TypeError("intents must be an array");
  }

  const accepted = [...facts.values()];
  /** @type {{transaction: WorkQueueTransaction, reason: string}[]} */
  const rejected = [];

  debugLog("applying intents", { existing: facts.size, intents: intents.length });
  upgradeTransactions(intents).forEach((transaction, index) => {
    try {
      validateTransaction(transaction);
    } catch (error) {
      throw new TypeError(`invalid intent at index ${index}: ${error instanceof Error ? error.message : String(error)}`);
    }
    const key = transactionKey(transaction);
    if (facts.has(key)) {
      debugLog("intent already present", { kind: transaction.kind });
      return;
    }

    const projection = replayTransactions(accepted);
    const hasWork = Object.hasOwn(projection.work, transaction.work_id);
    const hasClaimState = transaction.claim_id !== undefined && Object.hasOwn(projection.claim, transaction.claim_id);
    const claimState = typeof transaction.claim_id === "string" ? projection.claim[transaction.claim_id] : undefined;
    const terminal = hasWork && ["completed", "cancelled"].includes(projection.work[transaction.work_id]);
    const existingClaim = accepted.find(existing => existing.kind === "Claim" && existing.claim_id === transaction.claim_id);
    let reason = "";
    switch (transaction.kind) {
      case "Work":
        if (hasWork) reason = "work already exists";
        break;
      case "Claim":
        if (!hasWork) reason = "work does not exist";
        else if (terminal) reason = "work is terminal";
        else if (existingClaim) reason = "claim already exists for different work";
        break;
      case "ClaimCancellation":
        if (!existingClaim) reason = "claim does not exist";
        else if (existingClaim.work_id !== transaction.work_id) reason = "claim belongs to different work";
        else if (terminal) reason = "work is terminal";
        else if (hasClaimState && claimState === "cancelled") reason = "claim is already cancelled";
        break;
      case "Completion":
        if (!hasWork) reason = "work does not exist";
        else if (terminal) reason = "work is terminal";
        else if (!existingClaim) reason = "claim does not exist";
        else if (existingClaim.work_id !== transaction.work_id) reason = "claim belongs to different work";
        else if (!hasClaimState || claimState !== "effective") reason = "claim is not effective";
        else if (accepted.some(existing => existing.kind === "Completion" && existing.attempt_id === transaction.attempt_id)) reason = "attempt already completed work";
        break;
      case "WorkCancellation":
        if (!hasWork) reason = "work does not exist";
        else if (terminal) reason = "work is terminal";
        break;
    }

    if (reason) {
      rejected.push({ transaction, reason });
      debugLog("intent rejected", { kind: transaction.kind, reason });
      return;
    }

    const fact = copyTransaction(transaction);
    facts.set(key, fact);
    accepted.push(fact);
    debugLog("intent accepted", { kind: transaction.kind });
  });

  debugLog("intent application completed", { accepted: accepted.length, rejected: rejected.length });
  return { transactions: accepted, rejected };
}

/**
 * Removes duplicate records and writes the remaining facts in stable order.
 * @param {WorkQueueTransaction[]} transactions
 */
function compactTransactions(transactions) {
  return replayTransactions(transactions).transactions;
}

/**
 * @param {string} contents
 * @returns {WorkQueueTransaction[]}
 */
function parseTransactionLog(contents) {
  if (typeof contents !== "string") {
    throw new TypeError("transaction log must be a string");
  }
  if (contents === "") {
    return [];
  }

  const lines = contents.split("\n");
  if (lines[lines.length - 1] === "") {
    lines.pop();
  }
  const messages = lines.map((line, index) => {
    if (!line.trim()) {
      throw new TypeError(`invalid transaction log line ${index + 1}: blank lines are not allowed`);
    }
    let transaction;
    try {
      transaction = JSON.parse(line);
    } catch (error) {
      throw new TypeError(`invalid transaction log line ${index + 1}: malformed JSON`);
    }
    try {
      if (!transaction || typeof transaction !== "object" || Array.isArray(transaction)) throw new TypeError("transaction must be an object");
    } catch (error) {
      throw new TypeError(`invalid transaction log line ${index + 1}: ${error instanceof Error ? error.message : String(error)}`);
    }
    return transaction;
  });
  let transactions;
  try {
    transactions = upgradeTransactions(messages).map(transaction => {
      validateTransaction(transaction);
      return copyTransaction(transaction);
    });
  } catch (error) {
    throw new TypeError(`invalid transaction log: ${error instanceof Error ? error.message : String(error)}`);
  }
  replayTransactions(transactions);
  return transactions;
}

/**
 * @param {WorkQueueTransaction[]} transactions
 * @returns {string}
 */
function serializeTransactionLog(transactions) {
  const compacted = compactTransactions(transactions);
  return compacted.length ? `${compacted.map(canonicalJSON).join("\n")}\n` : "";
}

module.exports = {
  canonicalJSON,
  applyTransactions,
  compactTransactions,
  parseTransactionLog,
  replayTransactions,
  serializeTransactionLog,
  validateTransaction,
};
