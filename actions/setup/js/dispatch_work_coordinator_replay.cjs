// @ts-check

const TRANSACTION_KINDS = new Set(["Work", "Claim", "ClaimCancellation", "Completion", "WorkCancellation"]);
const TRANSACTION_FIELDS = ["kind", "work", "claim", "attempt"];
const SORTED_TRANSACTION_FIELDS = [...TRANSACTION_FIELDS].sort();

/**
 * Log coordinator diagnostics when DEBUG enables this module. Transaction
 * identifiers are deliberately omitted because they may contain submitted data.
 * @param {string} message
 * @param {Record<string, string | number>} [details]
 */
function debugLog(message, details = {}) {
  const debug = process.env.DEBUG || "";
  if (debug === "*" || debug.includes("dispatch_work_coordinator")) {
    console.error(`[dispatch_work_coordinator_replay] ${message} ${JSON.stringify(details)}`);
  }
}

/**
 * @typedef {
 *   | {kind: "Work", work: string, claim: null, attempt: null}
 *   | {kind: "Claim", work: string, claim: string, attempt: null}
 *   | {kind: "ClaimCancellation", work: string, claim: string, attempt: null}
 *   | {kind: "Completion", work: string, claim: string, attempt: string}
 *   | {kind: "WorkCancellation", work: string, claim: null, attempt: null}
 * } DispatchWorkTransaction
 */

/**
 * @param {unknown} transaction
 * @returns {asserts transaction is DispatchWorkTransaction}
 */
function validateTransaction(transaction) {
  if (!transaction || typeof transaction !== "object" || Array.isArray(transaction)) {
    throw new TypeError("transaction must be an object");
  }

  /** @type {Record<string, unknown>} */
  const candidate = Object.assign(Object.create(null), transaction);
  const fields = Object.keys(candidate).sort();
  if (fields.length !== SORTED_TRANSACTION_FIELDS.length || fields.some((field, index) => field !== SORTED_TRANSACTION_FIELDS[index])) {
    throw new TypeError("transaction must contain exactly kind, work, claim, and attempt");
  }
  if (typeof candidate.kind !== "string" || !TRANSACTION_KINDS.has(candidate.kind)) {
    throw new TypeError("transaction kind is invalid");
  }
  if (typeof candidate.work !== "string" || candidate.work.length === 0) {
    throw new TypeError("transaction work must be a non-empty string");
  }
  for (const field of ["claim", "attempt"]) {
    const value = candidate[field];
    if (value !== null && (typeof value !== "string" || value.length === 0)) {
      throw new TypeError(`transaction ${field} must be a non-empty string or null`);
    }
  }

  const hasClaim = candidate.claim !== null;
  const hasAttempt = candidate.attempt !== null;
  switch (candidate.kind) {
    case "Work":
    case "WorkCancellation":
      if (hasClaim || hasAttempt) {
        throw new TypeError(`${candidate.kind} must not include a claim or attempt`);
      }
      break;
    case "Claim":
    case "ClaimCancellation":
      if (!hasClaim || hasAttempt) {
        throw new TypeError(`${candidate.kind} must include a claim and must not include an attempt`);
      }
      break;
    case "Completion":
      if (!hasClaim || !hasAttempt) {
        throw new TypeError("Completion must include a claim and attempt");
      }
      break;
  }
}

/**
 * @param {DispatchWorkTransaction} transaction
 * @returns {string}
 */
function transactionKey(transaction) {
  return JSON.stringify([transaction.kind, transaction.work, transaction.claim, transaction.attempt]);
}

/**
 * @param {DispatchWorkTransaction} transaction
 * @returns {DispatchWorkTransaction}
 */
function copyTransaction(transaction) {
  return Object.freeze({
    kind: transaction.kind,
    work: transaction.work,
    claim: transaction.claim,
    attempt: transaction.attempt,
  });
}

/**
 * @param {DispatchWorkTransaction[]} transactions
 * @returns {Map<string, DispatchWorkTransaction>}
 */
function collectFacts(transactions) {
  if (!Array.isArray(transactions)) {
    throw new TypeError("transactions must be an array");
  }

  /** @type {Map<string, DispatchWorkTransaction>} */
  const facts = new Map();
  transactions.forEach((transaction, index) => {
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
        works.add(transaction.work);
        break;
      case "Claim":
        if (claims.has(transaction.claim)) {
          throw new TypeError(`claim ${transaction.claim} has conflicting transactions`);
        }
        claims.set(transaction.claim, transaction.work);
        break;
      case "ClaimCancellation":
        cancellations.add(transaction.claim);
        break;
      case "Completion":
        if (completions.has(transaction.work)) {
          throw new TypeError(`work ${transaction.work} has multiple terminal transactions`);
        }
        if (attempts.has(transaction.attempt)) {
          throw new TypeError(`attempt ${transaction.attempt} has multiple completions`);
        }
        completions.set(transaction.work, transaction);
        attempts.add(transaction.attempt);
        break;
      case "WorkCancellation":
        if (completions.has(transaction.work) || workCancellations.has(transaction.work)) {
          throw new TypeError(`work ${transaction.work} has multiple terminal transactions`);
        }
        workCancellations.add(transaction.work);
        break;
    }
  }

  for (const transaction of facts.values()) {
    if (transaction.kind !== "Work" && !works.has(transaction.work)) {
      throw new TypeError(`${transaction.kind} references missing work ${transaction.work}`);
    }
    if (transaction.kind === "ClaimCancellation" || transaction.kind === "Completion") {
      if (!claims.has(transaction.claim)) {
        throw new TypeError(`${transaction.kind} references missing claim ${transaction.claim}`);
      }
      if (claims.get(transaction.claim) !== transaction.work) {
        throw new TypeError(`${transaction.kind} references claim ${transaction.claim} for different work`);
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
    if (activeClaims[0] !== completion.claim) {
      throw new TypeError(`completion for work ${work} does not belong to its winning claim`);
    }
  }

  return facts;
}

/**
 * Replays a transaction log into a deterministic projection. Exact duplicate
 * records are idempotent; arbitration uses the lexicographically smallest
 * active claim identity, independent of record order.
 * @param {DispatchWorkTransaction[]} transactions
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
        works.add(transaction.work);
        break;
      case "Claim":
        claims.set(transaction.claim, transaction.work);
        break;
      case "ClaimCancellation":
        cancellations.add(transaction.claim);
        break;
      case "Completion":
        completions.set(transaction.work, transaction);
        break;
      case "WorkCancellation":
        workCancellations.add(transaction.work);
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
      const winner = completion ? completion.claim : workCancellations.has(work) ? null : (activeClaims[0] ?? null);
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
 * @param {DispatchWorkTransaction[]} transactions
 * @param {DispatchWorkTransaction[]} intents
 */
function applyTransactions(transactions, intents) {
  const facts = collectFacts(transactions);
  if (!Array.isArray(intents)) {
    throw new TypeError("intents must be an array");
  }

  const accepted = [...facts.values()];
  /** @type {{transaction: DispatchWorkTransaction, reason: string}[]} */
  const rejected = [];

  debugLog("applying intents", { existing: facts.size, intents: intents.length });
  intents.forEach((transaction, index) => {
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
    const hasWork = Object.hasOwn(projection.work, transaction.work);
    const hasClaimState = transaction.claim !== null && Object.hasOwn(projection.claim, transaction.claim);
    const claimState = hasClaimState ? projection.claim[transaction.claim] : undefined;
    const terminal = hasWork && ["completed", "cancelled"].includes(projection.work[transaction.work]);
    const existingClaim = accepted.find(existing => existing.kind === "Claim" && existing.claim === transaction.claim);
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
        else if (existingClaim.work !== transaction.work) reason = "claim belongs to different work";
        else if (terminal) reason = "work is terminal";
        else if (hasClaimState && claimState === "cancelled") reason = "claim is already cancelled";
        break;
      case "Completion":
        if (!hasWork) reason = "work does not exist";
        else if (terminal) reason = "work is terminal";
        else if (!existingClaim) reason = "claim does not exist";
        else if (existingClaim.work !== transaction.work) reason = "claim belongs to different work";
        else if (!hasClaimState || claimState !== "effective") reason = "claim is not effective";
        else if (accepted.some(existing => existing.kind === "Completion" && existing.attempt === transaction.attempt)) reason = "attempt already completed work";
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
 * @param {DispatchWorkTransaction[]} transactions
 */
function compactTransactions(transactions) {
  return replayTransactions(transactions).transactions;
}

/**
 * @param {string} contents
 * @returns {DispatchWorkTransaction[]}
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
  const transactions = lines.map((line, index) => {
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
      validateTransaction(transaction);
    } catch (error) {
      throw new TypeError(`invalid transaction log line ${index + 1}: ${error instanceof Error ? error.message : String(error)}`);
    }
    return copyTransaction(transaction);
  });
  replayTransactions(transactions);
  return transactions;
}

/**
 * @param {DispatchWorkTransaction[]} transactions
 * @returns {string}
 */
function serializeTransactionLog(transactions) {
  const compacted = compactTransactions(transactions);
  return compacted.length ? `${compacted.map(transaction => JSON.stringify(transaction)).join("\n")}\n` : "";
}

module.exports = {
  applyTransactions,
  compactTransactions,
  parseTransactionLog,
  replayTransactions,
  serializeTransactionLog,
  validateTransaction,
};
