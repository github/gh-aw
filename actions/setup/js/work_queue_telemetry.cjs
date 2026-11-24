// @ts-check
"use strict";

const { AsyncLocalStorage } = require("node:async_hooks");
const { createHash } = require("node:crypto");
const { queueControlErrorCode } = require("./work_queue_logging.cjs");

/** @typedef {Record<string, string | number | boolean>} Attributes */
/** @type {AsyncLocalStorage<{attributes: Attributes, snapshot?: {state: object}}>} */
const activeOperation = new AsyncLocalStorage();
const PREFIX = "gh-aw.work_queue.";
const OUTCOMES = new Set(["started", "launch_failed", "bound", "released", "settled", "blocked"]);
const REASONS = new Set(["no_work", "no_eligible_work", "capacity_blocked", "account_capacity_blocked", "claim_budget_reached", "prefix_complete"]);
const REQUESTS = new Set(["submit", "dispatch_next", "launch", "bind", "finish", "release", "cancel_work", "checkpoint", "issue_link", "migrate"]);
const TRANSITIONS = new Set(["Work", "Claim", "Dispatch", "Bound", "Completion", "ClaimCancellation", "WorkCancellation", "Release"]);
const QUEUE_ERRORS = new Set(["ledger_invalid", "checkpoint_invalid", "resource_limit", "ledger_limit", "repository_unavailable", "branch_invalid", "unsupported_protocol", "cas_exhausted"]);

/** Counts of the committed projection; never a decision input. */
function snapshotAttributes(state) {
  /** @type {Record<string, number>} */
  const attributes = {
    "work.total": state.works.size,
    "work.available": 0,
    "work.claimed": 0,
    "work.completed": 0,
    "work.cancelled": 0,
    "claims.open": 0,
    "dispatches.reserved": 0,
    "dispatches.started": 0,
    "dispatches.bound": 0,
  };
  for (const work of state.works.values()) if (Object.hasOwn(attributes, `work.${work.state}`)) attributes[`work.${work.state}`]++;
  for (const claim of state.claims.values()) if (claim.state === "open") attributes["claims.open"]++;
  for (const dispatch of state.dispatches.values()) if (Object.hasOwn(attributes, `dispatches.${dispatch.state}`)) attributes[`dispatches.${dispatch.state}`]++;
  return attributes;
}

function increment(current, key, amount = 1) {
  const name = `${PREFIX}${key}`;
  current.attributes[name] = Number(current.attributes[name] || 0) + amount;
}

function publicationAttempt() {
  const current = activeOperation.getStore();
  if (current) increment(current, "publication.attempts");
}

/** @param {"conflict" | "ambiguous"} kind */
function publicationRecovery(kind) {
  const current = activeOperation.getStore();
  if (current) increment(current, `publication.${kind}`);
}

function captureResult(current, result) {
  if (result?.state?.works instanceof Map) current.snapshot = { state: result.state };
  if (result?.committed === true && result.recovered !== true) {
    increment(current, "publication.commits");
    for (const operation of result.commit?.operations || []) {
      if (TRANSITIONS.has(operation.kind)) increment(current, `committed.${operation.kind}`);
    }
  }
  if (result?.recovered === true) increment(current, "publication.recovered");
  if (REQUESTS.has(result?.commit?.request?.kind)) current.attributes[`${PREFIX}request.kind`] = result.commit.request.kind;
  if (REASONS.has(result?.reason)) current.attributes[`${PREFIX}reason`] = result.reason;
}

function captureOutcome(current, result) {
  let isError = false;
  const outcome = result?.state;
  if (OUTCOMES.has(outcome)) {
    current.attributes[`${PREFIX}outcome`] = outcome;
    isError = outcome === "blocked";
  }
  return isError;
}

/** @param {unknown} error */
function telemetryErrorType(error) {
  const code = error !== null && typeof error === "object" ? Object.getOwnPropertyDescriptor(error, "code")?.value : undefined;
  if (typeof code === "string" && QUEUE_ERRORS.has(code)) return code;
  return queueControlErrorCode(error);
}

/**
 * Nested ledger reads/publications contribute to one outer operation span.
 * Only verified results are observed; candidate operations never become telemetry state.
 * @template T
 * @param {string} operation Developer-authored operation name.
 * @param options
 * @param {() => Promise<T>} execute
 * @returns {Promise<T>}
 */
async function observeQueueOperation(operation, options, execute) {
  if (!/^[a-f0-9]{32}$/.test(process.env.GITHUB_AW_OTEL_TRACE_ID || "") || /^0{32}$/.test(process.env.GITHUB_AW_OTEL_TRACE_ID || "")) return execute();
  const parent = activeOperation.getStore();
  if (parent) {
    const result = await execute();
    captureResult(parent, result);
    return result;
  }
  const startMs = Date.now();
  /** @type {{attributes: Attributes, snapshot?: {state: object}}} */
  const current = {
    attributes: {
      [`${PREFIX}operation`]: operation,
      [`${PREFIX}publication.attempts`]: 0,
      [`${PREFIX}publication.conflict`]: 0,
      [`${PREFIX}publication.ambiguous`]: 0,
      [`${PREFIX}publication.commits`]: 0,
      [`${PREFIX}publication.recovered`]: 0,
    },
  };
  return activeOperation.run(current, async () => {
    let isError = false;
    try {
      const result = await execute();
      captureResult(current, result);
      isError = captureOutcome(current, result);
      return result;
    } catch (error) {
      isError = true;
      current.attributes[`${PREFIX}error.type`] = telemetryErrorType(error);
      throw error;
    } finally {
      const endMs = Date.now();
      try {
        const owner = options.owner ?? options.context?.repo?.owner ?? "";
        const repo = options.repo ?? options.context?.repo?.repo ?? "";
        current.attributes[`${PREFIX}queue.id`] = createHash("sha256")
          .update(JSON.stringify([owner.toLowerCase(), repo.toLowerCase(), options.branch ?? "work-queue"]))
          .digest("hex");
        current.attributes[`${PREFIX}snapshot.present`] = !!current.snapshot;
        if (current.snapshot) {
          for (const [key, value] of Object.entries(snapshotAttributes(current.snapshot.state))) current.attributes[`${PREFIX}${key}`] = value;
        }
        await require("./otlp.cjs").logSpan(`work_queue.${operation}`, current.attributes, { startMs, endMs, isError });
      } catch {
        // Do not expose exporter errors, which can include credentials or URLs.
        console.warn("[work-queue] Telemetry emission failed; queue state and publication are unchanged.");
      }
    }
  });
}

module.exports = { observeQueueOperation, publicationAttempt, publicationRecovery, snapshotAttributes };
