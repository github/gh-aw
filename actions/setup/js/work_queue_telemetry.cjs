// @ts-check
"use strict";

const { AsyncLocalStorage } = require("node:async_hooks");
const { createHash } = require("node:crypto");
const { workExpired } = require("./work_queue_expiration.cjs");
const { queueControlErrorCode } = require("./work_queue_logging.cjs");

/** @typedef {Record<string, string | number | boolean>} Attributes */
/** @type {AsyncLocalStorage<{attributes: Attributes, snapshot?: {state: object, at: number}}>} */
const activeOperation = new AsyncLocalStorage();
const PREFIX = "gh-aw.work_queue.";
const OUTCOMES = new Set(["activation_pending", "bound", "released", "rejected", "reserved", "cancellation_requested", "launch_unresolved", "run_binding_conflict", "staged_preview"]);
const REASONS = new Set([
  "selected",
  "no_work",
  "no_eligible_work",
  "capacity_blocked",
  "native_capacity_blocked",
  "grants_paused",
  "already_submitted",
  "dispatch_budget_blocked",
  "assignment_bytes_blocked",
  "claim_budget_reached",
  "operation_budget_reached",
  "operation_budget_blocked",
  "prefix_complete",
]);
const REQUESTS = new Set(["policy", "pool_install", "deployment", "control", "submit", "dispatch_next", "finish", "observe", "dispatch", "release", "result", "delivery_failure", "cancel_work", "cancel_claim", "checkpoint", "issue_link"]);
const TRANSITIONS = new Set(["Work", "Claim", "Completion", "ClaimCancellation", "WorkCancellation", "Result", "DeliveryFailure", "Release", "Dispatch"]);
const QUEUE_ERRORS = new Set(["ledger_invalid", "checkpoint_invalid", "resource_limit", "repository_unavailable", "queue_missing", "policy_missing", "branch_invalid", "unsupported_protocol"]);

function snapshotAttributes(state, at) {
  /** @type {Record<string, number>} */
  const attributes = {
    observed_at_unix_ms: at,
    "work.total": state.works.size,
    "work.available": 0,
    "work.claimed": 0,
    "work.completed": 0,
    "work.cancelled": 0,
    "work.expired": 0,
    "work.retry_delayed": 0,
    "work.oldest_available_age_ms": 0,
    "delivery.pending": 0,
    "delivery.failed": 0,
    "claims.open": 0,
    "reservations.active": 0,
    "reservations.unbound": 0,
    "reservations.uncertain": 0,
    "reservations.unresolved": 0,
    "capacity.logical_limit": 0,
    "capacity.native_limit": 0,
    "capacity.logical_saturated_pools": 0,
    "capacity.native_saturated_pools": 0,
  };
  const logical = new Map();
  const native = new Map();
  const time = Math.max(at, state.time_watermark ?? 0);
  for (const work of state.works.values()) {
    const expired = workExpired(work, time) || work.cancellation_reason === "work_expired";
    const ownership = expired ? "cancelled" : work.state;
    if (Object.hasOwn(attributes, `work.${ownership}`)) attributes[`work.${ownership}`]++;
    if (expired) attributes["work.expired"]++;
    if (ownership === "available") {
      if (work.retry_not_before > time) attributes["work.retry_delayed"]++;
      attributes["work.oldest_available_age_ms"] = Math.max(Number(attributes["work.oldest_available_age_ms"]), Math.max(0, time - work.enqueued));
    }
    if (["pending", "failed"].includes(work.barrier)) attributes[`delivery.${work.barrier}`]++;
  }
  for (const claim of state.claims.values()) {
    if (claim.state === "open") {
      attributes["claims.open"]++;
      const pool = state.works.get(claim.work_id).pool;
      logical.set(pool, (logical.get(pool) || 0) + 1);
    }
    // keep_open completions have Claim-local barriers, independent of Work.
    if (claim.keep_open && ["pending", "failed"].includes(claim.barrier)) attributes[`delivery.${claim.barrier}`]++;
  }
  for (const dispatch of state.dispatches.values()) {
    if (dispatch.released) continue;
    attributes["reservations.active"]++;
    if (!dispatch.run) attributes["reservations.unbound"]++;
    if (["uncertain", "unresolved"].includes(dispatch.state)) attributes[`reservations.${dispatch.state}`]++;
    native.set(dispatch.pool, (native.get(dispatch.pool) || 0) + 1);
  }
  for (const [pool, policy] of Object.entries(state.policy?.pools || {})) {
    attributes["capacity.logical_limit"] += policy.logical_limit;
    attributes["capacity.native_limit"] += policy.native_limit;
    if ((logical.get(pool) || 0) >= policy.logical_limit) attributes["capacity.logical_saturated_pools"]++;
    if ((native.get(pool) || 0) >= policy.native_limit) attributes["capacity.native_saturated_pools"]++;
  }
  return { ...attributes, initialized: !!state.policy, admission_paused: state.admission_paused === true, grants_paused: state.grants_paused === true };
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
  if (result?.state?.works instanceof Map) current.snapshot = { state: result.state, at: Date.now() };
  if (result?.publishedNow === true) {
    increment(current, "publication.commits");
    for (const operation of result.operations || []) {
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
    isError = ["rejected", "launch_unresolved", "run_binding_conflict"].includes(outcome);
  }
  if (Array.isArray(result?.results)) {
    current.attributes[`${PREFIX}reconciliation.checked`] = result.results.length;
    current.attributes[`${PREFIX}reconciliation.unresolved`] = result.results.filter(item => ["launch_unresolved", "run_binding_conflict"].includes(item.state)).length;
    isError ||= Number(current.attributes[`${PREFIX}reconciliation.unresolved`]) > 0;
  }
  return isError;
}

/** @param {unknown} error */
function telemetryErrorType(error) {
  const code = error !== null && typeof error === "object" ? Object.getOwnPropertyDescriptor(error, "code")?.value : undefined;
  if (typeof code === "string" && QUEUE_ERRORS.has(code)) return code;
  if (error instanceof Error && error.message.startsWith("publication_unresolved:")) return "publication_unresolved";
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
  /** @type {{attributes: Attributes, snapshot?: {state: object, at: number}}} */
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
          for (const [key, value] of Object.entries(snapshotAttributes(current.snapshot.state, current.snapshot.at))) current.attributes[`${PREFIX}${key}`] = value;
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
