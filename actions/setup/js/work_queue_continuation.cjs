// @ts-check
"use strict";

// Finalizer side of routed `request_continuation` for queue workers: match the
// manager's accepted-request receipts to the bound agent output, plan the
// successor Work beside its parent's Completion, and authorize its admission.
const fs = require("fs");
const path = require("path");
const crypto = require("crypto");

const { parseStrictJSON, queueError } = require("./work_queue_codec.cjs");
const { continuationChild, validateResource } = require("./work_queue_graph.cjs");
const { policyAllowsWorker } = require("./work_queue_policy.cjs");

const ROUTES = ["workqueue", "issue"];
const STATUSES = ["ready", "depth_limit"];
const RECEIPT_FIELDS = ["claim_handle", "claim_id", "message_sha256", "status"];
const MAX_RECEIPTS_BYTES = 64 * 1024;
const SHA256 = /^[a-f0-9]{64}$/;
const ADMIT = "request_continuation:admit";

/**
 * @typedef {{route: "workqueue" | "issue", maxDepth: number}} ContinuationConfig
 * @typedef {{claim_handle: string, claim_id: string, message_sha256: string, status: "ready" | "depth_limit", issue?: Record<string, string>}} ContinuationReceipt
 * @typedef {{message: string, message_sha256: string, index: number, receipt: ContinuationReceipt}} ContinuationEntry
 */

function routes() {
  return require("./request_continuation_routes.cjs");
}

function invalid(code, message) {
  return queueError(code, message);
}

/**
 * Compiled route of a routed worker finish step, or null for unrouted workers.
 * @param {NodeJS.ProcessEnv} env
 * @returns {ContinuationConfig | null}
 */
function readContinuationConfig(env = process.env) {
  const raw = env.GH_AW_CONTINUATION_ROUTE;
  if (raw === undefined || raw === "") return null;
  if (!ROUTES.includes(raw)) throw invalid("continuation_config_invalid", "GH_AW_CONTINUATION_ROUTE must be workqueue or issue");
  const route = raw === "issue" ? "issue" : "workqueue";
  const depth = env.GH_AW_CONTINUATION_MAX_DEPTH;
  if (depth === undefined || depth === "") return { route, maxDepth: 1 };
  if (!/^(?:[1-9]|1[0-6])$/.test(depth)) throw invalid("continuation_config_invalid", "GH_AW_CONTINUATION_MAX_DEPTH must be an integer 1..16");
  return { route, maxDepth: Number(depth) };
}

/**
 * Parse the manager's `work_queue_continuations` step output: a bounded JSON array
 * of at most one receipt. Unset, empty or `[]` → undefined.
 * @param {string | undefined} raw
 * @returns {ContinuationReceipt | undefined}
 */
function parseContinuationReceipt(raw) {
  if (raw === undefined || raw === "") return undefined;
  let value;
  try {
    value = parseStrictJSON(raw, { maxBytes: MAX_RECEIPTS_BYTES });
  } catch {
    throw invalid("continuation_receipt_invalid", "work_queue_continuations is not bounded JSON");
  }
  if (!Array.isArray(value) || value.length > 1) throw invalid("continuation_receipt_invalid", "work_queue_continuations must be an array of at most one receipt");
  const [receipt] = value;
  if (receipt === undefined) return undefined;
  if (!receipt || typeof receipt !== "object" || Array.isArray(receipt)) throw invalid("continuation_receipt_invalid", "receipt must be an object");
  const keys = Object.keys(receipt);
  if (keys.some(key => !RECEIPT_FIELDS.includes(key) && key !== "issue") || RECEIPT_FIELDS.some(key => !Object.hasOwn(receipt, key))) throw invalid("continuation_receipt_invalid", "receipt fields are closed");
  if (typeof receipt.claim_handle !== "string" || !receipt.claim_handle || typeof receipt.claim_id !== "string" || !receipt.claim_id) throw invalid("continuation_receipt_invalid", "receipt Claim identity is invalid");
  if (typeof receipt.message_sha256 !== "string" || !SHA256.test(receipt.message_sha256) || !STATUSES.includes(receipt.status)) throw invalid("continuation_receipt_invalid", "receipt digest or status is invalid");
  if (Object.hasOwn(receipt, "issue")) {
    if (receipt.status !== "ready") throw invalid("continuation_receipt_invalid", "only a ready receipt carries an approval Issue");
    try {
      if (validateResource(receipt.issue).kind !== "issue") throw new Error("kind");
    } catch {
      throw invalid("continuation_receipt_invalid", "receipt approval Issue is invalid");
    }
  }
  return receipt;
}

/**
 * Read the producing attempt's one request through the bound evidence. A run with no
 * evidence and no agent output has no request (`available:false`); output bytes
 * without matching evidence are an error, never an unbound replay.
 * @param {{env?: NodeJS.ProcessEnv, context: any, claim: {handle: string}}} options
 */
function readContinuationRequest({ env = process.env, context, claim }) {
  const raw = env.GH_AW_CONTINUATION_EVIDENCE;
  const filename = env.GH_AW_AGENT_OUTPUT;
  if (raw === undefined || raw === "") {
    const present = typeof filename === "string" && filename !== "" && fs.existsSync(filename) && fs.statSync(filename).size > 0;
    if (present) throw invalid("continuation_evidence_invalid", "agent output exists without its bound continuation evidence");
    return { available: false, evidence: undefined, request: null };
  }
  const { collectRoutedRequest, parseContinuationEvidence, readBoundAgentOutput } = routes();
  const evidence = parseContinuationEvidence(raw);
  const { output } = readBoundAgentOutput({ path: filename, evidence: raw, context });
  return { available: true, evidence, request: collectRoutedRequest(output.items, { assignment: { claims: [claim] } }) };
}

/**
 * Match the receipt to this Claim's request (handle, original Claim ID, message
 * digest, route). A supplied receipt is validated even when the request is then
 * suppressed by a failed or missing finish; a receipt alone never admits.
 * @param {{config: ContinuationConfig, request: {message: string, message_sha256: string, index: number, claim_handle?: string} | null, receipt: ContinuationReceipt | undefined, intent: string | undefined, claim: {handle: string, claim_id: string}}} options
 * @returns {ContinuationEntry | null | undefined} the matched entry of a completed request,
 *   `null` for a completed request without a receipt, `undefined` for no actionable request
 */
function matchContinuation({ config, request, receipt, intent, claim }) {
  if (receipt) {
    if (!request || request.claim_handle !== claim.handle || receipt.claim_handle !== claim.handle || receipt.claim_id !== claim.claim_id) {
      throw invalid("continuation_receipt_mismatch", "receipt does not match a request of its original Claim");
    }
    if (request.message_sha256 !== receipt.message_sha256 || routes().messageDigest(request.message) !== receipt.message_sha256) throw invalid("continuation_receipt_mismatch", "receipt message digest does not match the bound request");
    if (config.route === "issue" ? receipt.status !== "ready" || !receipt.issue : Object.hasOwn(receipt, "issue")) throw invalid("continuation_receipt_mismatch", `receipt does not match the compiled ${config.route} route`);
  }
  // A failed or missing finish suppresses the request.
  if (!request || intent !== "completed") return undefined;
  return receipt ? { message: request.message, message_sha256: request.message_sha256, index: request.index, receipt } : null;
}

/**
 * The successor operations following one parent's Completion: [] at the depth
 * limit, else one Work built from trusted ledger state.
 * @param {any} state
 * @param {{claim_id: string, work_id: string}} claim the live open Claim
 * @param {ContinuationEntry} entry
 * @param {ContinuationConfig} config
 * @param {import("./work_queue_policy.cjs").PolicyEnv | null | undefined} policy
 */
function continuationOperations(state, claim, entry, config, policy) {
  if (entry.receipt.claim_id !== claim.claim_id) throw invalid("continuation_receipt_mismatch", "receipt is bound to another Claim");
  const parent = state.works.get(claim.work_id);
  let remaining = 0;
  if (config.route === "workqueue") {
    const available = routes().availableDepth(config.maxDepth, parent.continuation?.remaining);
    if ((available > 0 ? "ready" : "depth_limit") !== entry.receipt.status) throw invalid("continuation_receipt_mismatch", "receipt depth status disagrees with the ledger");
    if (available <= 0) return [];
    remaining = available - 1;
  }
  if (!policy) throw invalid("policy_missing", "continuation admission requires the compiled work queue policy");
  if (!policyAllowsWorker(policy, parent.pool, parent.worker_profile)) throw invalid("continuation_destination_invalid", `worker ${parent.worker_profile} or pool ${parent.pool} is no longer approved`);
  return [continuationChild(parent, { route: config.route, message: entry.message, remaining }, config.route === "issue" ? entry.receipt.issue : undefined)];
}

/** Handler-manager configuration of this workflow (for the global `authorization`). */
function readHandlerConfig(env = process.env) {
  const raw = env.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG;
  if (raw === undefined || raw === "") return {};
  const value = parseStrictJSON(raw);
  if (!value || typeof value !== "object" || Array.isArray(value)) throw invalid("continuation_config_invalid", "safe-output handler configuration must be an object");
  return value;
}

/** Global authorization requirement of a routed continuation (never builtin-exempt). */
function admissionRequirement(handlerConfig) {
  return handlerConfig.request_continuation?.authorization || handlerConfig.authorization || undefined;
}

/** The immutable facts one admission decision binds. */
function admissionInputs({ child, claim, dispatchId, repository, evidence, entry }) {
  return {
    child,
    parent: { work_id: claim.work_id, claim_id: claim.claim_id, dispatch_id: dispatchId },
    repository,
    artifact: { run_id: evidence.run_id, run_attempt: evidence.run_attempt, sha256: evidence.sha256, index: entry.index },
    receipt: entry.receipt,
  };
}

/**
 * Prepare and authorize `request_continuation:admit` (executor `queue`) for one
 * successor in this process. Returns the allowed decision and the bound digest.
 * @param {{requirement: any, inputs: ReturnType<typeof admissionInputs>, handlerConfig: Record<string, any>, config: ContinuationConfig, core?: any, env?: NodeJS.ProcessEnv,
 *   authorization?: {evidencePath?: string, fetchImpl?: typeof fetch, now?: () => number, wallNow?: () => number}}} options
 */
async function authorizeAdmission({ requirement, inputs, handlerConfig, config, core: coreApi, env = process.env, authorization = {} }) {
  const protocol = authorizationProtocol();
  const caller = protocol.buildAuthorizationCaller(env);
  const invocation = protocol.prepareInvocation({
    caller,
    output: { type: "request_continuation", name: ADMIT, executor: "queue", implementation_digest: crypto.createHash("sha256").update(fs.readFileSync(__filename)).digest("hex") },
    normalizedInputs: inputs,
    batchDigest: inputs.artifact.sha256,
    recordIndex: inputs.artifact.index,
    scope: { binding: "invocation", refs: [`source-repository-id:${caller.repository_id}`, `queue-repository:${inputs.repository}`, `work:${inputs.child.work_id}`] },
    context: {
      policy_digest: protocol.canonicalDigest({ configuration: handlerConfig.request_continuation ?? {}, route: config.route, max_depth: config.maxDepth }),
      claim_id: inputs.parent.claim_id,
      delivery_id: inputs.parent.dispatch_id,
    },
  });
  const evidencePath = authorization.evidencePath || protocol.authorizationEvidencePath(env);
  fs.mkdirSync(path.dirname(evidencePath), { recursive: true, mode: 0o700 });
  const decision = await protocol.authorizePreparedInvocation({ core: coreApi, requirement, invocation, evidencePath, env, ...pick(authorization, ["fetchImpl", "now", "wallNow"]) });
  return { decision, digest: protocol.canonicalDigest(inputs), evidencePath };
}

function pick(value, keys) {
  return Object.fromEntries(keys.filter(key => value[key] !== undefined).map(key => [key, value[key]]));
}

/**
 * The safe-output authorization protocol, for the trusted finish step only. It masks
 * broker credentials with core.setSecret, which would corrupt an MCP server's stdout,
 * so the MCP and safe-outputs bundles must not deploy it. The non-literal path keeps
 * it out of their dependency lists; the trusted step loads it from the full actions
 * directory.
 * @returns {typeof import("./safe_output_authorization.cjs")}
 */
function authorizationProtocol() {
  return require(path.join(__dirname, "safe_output_authorization.cjs"));
}

module.exports = {
  ADMIT,
  admissionInputs,
  admissionRequirement,
  authorizationProtocol,
  authorizeAdmission,
  continuationOperations,
  matchContinuation,
  parseContinuationReceipt,
  readContinuationConfig,
  readContinuationRequest,
  readHandlerConfig,
};
