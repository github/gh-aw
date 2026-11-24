// @ts-check
/// <reference types="@actions/github-script" />
"use strict";

/**
 * Routed `request_continuation` runtime.
 *
 * The agent records an explicit request; only the trusted `Process Safe Outputs`
 * step routes it. Every route first binds the request to the producing agent
 * attempt (`continuation_evidence`) and to a clean collection, then:
 *   - workqueue: emits a manager receipt; the worker finalizer admits the child
 *   - issue: creates one approval issue and emits a receipt carrying it
 *   - dispatch: POSTs one workflow_dispatch to the compiled receiver
 */

const crypto = require("crypto");
const fs = require("fs");
const { isDeepStrictEqual } = require("node:util");

const MAX_ROUTED_MESSAGE_BYTES = 16384;
const MAX_DISPATCH_INPUT_BYTES = 65535;
const MAX_DEPTH = 16;
const ROUTES = Object.freeze(["workqueue", "issue", "dispatch"]);
// Bound the agent output read by the publisher (the collector writes regular JSON).
const MAX_AGENT_OUTPUT_BYTES = 64 * 1024 * 1024;
const APPROVAL_INSTRUCTION = "Close as completed to approve one successor. Closing as not planned does not approve it.";
const MAX_TITLE_KEY_CHARS = 200;
const RAW_REQUEST_FIELDS = new Set(["type", "message", "claim_handle", "output_id", "output_ids"]);
// The trusted collector adds `timestamp` to every collected request.
const COLLECTED_REQUEST_FIELDS = new Set([...RAW_REQUEST_FIELDS, "timestamp"]);
const REPOSITORY = /^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/;
const DECIMAL = /^[1-9][0-9]*$/;

/** @param {string} code @param {string} [detail] */
function routeError(code, detail) {
  return Object.assign(new Error(code), { code, ...(detail ? { detail } : {}) });
}

/** @param {unknown} value */
function plainObject(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

/** @param {string} value */
function utf8Bytes(value) {
  return Buffer.byteLength(value, "utf8");
}

/** 64-hex SHA-256 of the UTF-8 message bytes. @param {string} message */
function messageDigest(message) {
  if (typeof message !== "string") throw routeError("continuation_request_invalid");
  return crypto.createHash("sha256").update(Buffer.from(message, "utf8")).digest("hex");
}

/**
 * Parse `<run_id>:<run_attempt>:<64 lowercase hex>`.
 * @param {unknown} value
 * @returns {{run_id: string, run_attempt: number, sha256: string}}
 */
function parseContinuationEvidence(value) {
  const match = typeof value === "string" ? value.match(/^([1-9][0-9]{0,19}):([1-9][0-9]{0,9}):([0-9a-f]{64})$/) : null;
  const attempt = match ? Number(match[2]) : NaN;
  if (!match || !Number.isSafeInteger(attempt)) throw routeError("continuation_evidence_invalid");
  return { run_id: match[1], run_attempt: attempt, sha256: match[3] };
}

/** @param {any} context */
function currentRun(context) {
  const runId = String(context?.runId ?? process.env.GITHUB_RUN_ID ?? "");
  const runAttempt = Number(context?.runAttempt ?? process.env.GITHUB_RUN_ATTEMPT ?? 1);
  if (!DECIMAL.test(runId) || !Number.isSafeInteger(runAttempt) || runAttempt < 1) throw routeError("continuation_evidence_invalid");
  return { runId, runAttempt };
}

/**
 * Read the agent output once and accept it only when it is the artifact this run
 * attempt's agent job bound, and its collection is clean.
 * @param {{path: string | undefined, evidence: unknown, context: any}} options
 * @returns {{output: {items: any[], errors: unknown[]} & Record<string, unknown>, sha256: string}}
 */
function readBoundAgentOutput({ path, evidence, context }) {
  const bound = parseContinuationEvidence(evidence);
  const run = currentRun(context);
  if (bound.run_id !== run.runId || bound.run_attempt !== run.runAttempt) throw routeError("continuation_evidence_invalid");
  if (typeof path !== "string" || !path) throw routeError("continuation_evidence_invalid");
  /** @type {Buffer} */
  let bytes;
  try {
    const stat = fs.lstatSync(path);
    if (!stat.isFile() || stat.size > MAX_AGENT_OUTPUT_BYTES) throw routeError("continuation_evidence_invalid");
    bytes = fs.readFileSync(path);
  } catch {
    throw routeError("continuation_evidence_invalid");
  }
  if (bytes.length > MAX_AGENT_OUTPUT_BYTES) throw routeError("continuation_evidence_invalid");
  const sha256 = crypto.createHash("sha256").update(bytes).digest("hex");
  if (sha256 !== bound.sha256) throw routeError("continuation_evidence_invalid");
  let output;
  try {
    output = JSON.parse(new TextDecoder("utf-8", { fatal: true, ignoreBOM: true }).decode(bytes));
  } catch {
    throw routeError("continuation_collection_invalid");
  }
  if (!plainObject(output) || !Array.isArray(output.items) || !isDeepStrictEqual(output.errors, [])) throw routeError("continuation_collection_invalid");
  return { output, sha256 };
}

/**
 * Field and message checks for one routed request. Raw agent records may carry only
 * the message, Claim selector and submission metadata; collected records also the
 * trusted timestamp. Route, destination, policy or budget fields are never accepted.
 * @param {any} item
 * @param {{collected?: boolean}} [options]
 * @returns {string | null} error, or null when valid
 */
function routedRequestError(item, { collected = false } = {}) {
  if (!plainObject(item)) return "request_continuation must be an object";
  const allowed = collected ? COLLECTED_REQUEST_FIELDS : RAW_REQUEST_FIELDS;
  const extra = Object.keys(item).filter(field => !allowed.has(field));
  if (extra.length) return `request_continuation does not accept agent-supplied field(s): ${extra.sort().join(", ")}`;
  if (typeof item.message !== "string" || !item.message.trim()) return "request_continuation requires a nonblank message";
  if (utf8Bytes(item.message) > MAX_ROUTED_MESSAGE_BYTES) return `request_continuation message exceeds ${MAX_ROUTED_MESSAGE_BYTES} UTF-8 bytes`;
  return null;
}

/**
 * The one collected request of this scope, bound to the sole Claim handle for a
 * worker (omission selects it) and unscoped for an ordinary source. A second
 * request or a foreign selector is invalid. Only `assignment.claims[0].handle` is consulted.
 * @param {any[]} items
 * @param {{assignment?: {claims: Array<{handle: string}>} | null}} [options]
 * @returns {{message: string, message_sha256: string, index: number, claim_handle?: string} | null}
 */
function collectRoutedRequest(items, { assignment = null } = {}) {
  if (!Array.isArray(items)) throw routeError("continuation_request_invalid");
  /** @type {string | null} */
  let handle = null;
  if (assignment) {
    if (!Array.isArray(assignment.claims) || assignment.claims.length !== 1) throw routeError("continuation_request_invalid");
    handle = assignment.claims[0]?.handle;
    if (typeof handle !== "string" || !handle) throw routeError("continuation_request_invalid");
  }
  /** @type {{message: string, message_sha256: string, index: number, claim_handle?: string} | null} */
  let request = null;
  for (const [index, item] of items.entries()) {
    if (!plainObject(item) || typeof item.type !== "string" || item.type.replace(/-/g, "_") !== "request_continuation") continue;
    if (routedRequestError(item, { collected: true })) throw routeError("continuation_request_invalid");
    if (Object.hasOwn(item, "claim_handle") && (handle === null || item.claim_handle !== handle)) throw routeError("continuation_request_invalid");
    if (request) throw routeError("continuation_request_invalid");
    request = { message: item.message, message_sha256: messageDigest(item.message), index, ...(handle === null ? {} : { claim_handle: handle }) };
  }
  return request;
}

/**
 * Automatic hops still available: the compiled budget at a root, otherwise the
 * smaller of the compiled and inbound budgets.
 * @param {number} compiledMaxDepth
 * @param {number | undefined} inboundRemaining
 */
function availableDepth(compiledMaxDepth, inboundRemaining) {
  if (!Number.isSafeInteger(compiledMaxDepth) || compiledMaxDepth < 1 || compiledMaxDepth > MAX_DEPTH) throw routeError("continuation_config_invalid");
  if (inboundRemaining === undefined) return compiledMaxDepth;
  if (!Number.isSafeInteger(inboundRemaining) || inboundRemaining < 0 || inboundRemaining > MAX_DEPTH - 1) throw routeError("continuation_inbound_invalid");
  return Math.min(compiledMaxDepth, inboundRemaining);
}

/**
 * Closed, bounded `gh_aw_continuation` workflow_dispatch input. Absent or empty is a
 * normal root; anything malformed is rejected, never reset to a root.
 * @param {any} payload
 * @returns {null | Readonly<{version: 1, message: string, remaining: number, source: Readonly<{repository: string, workflow: string, run_id: string, run_attempt: number}>}>}
 */
function parseInboundDispatchContinuation(payload) {
  const inputs = plainObject(payload) && plainObject(payload.inputs) ? payload.inputs : null;
  const raw = inputs && Object.hasOwn(inputs, "gh_aw_continuation") ? inputs.gh_aw_continuation : undefined;
  if (raw === undefined || raw === null || raw === "") return null;
  const invalid = () => routeError("continuation_inbound_invalid");
  if (typeof raw !== "string" || utf8Bytes(raw) > MAX_DISPATCH_INPUT_BYTES) throw invalid();
  let value;
  try {
    value = JSON.parse(raw);
  } catch {
    throw invalid();
  }
  const exact = (object, fields) => plainObject(object) && Object.keys(object).length === fields.length && fields.every(field => Object.hasOwn(object, field));
  if (!exact(value, ["version", "message", "remaining", "source"]) || value.version !== 1) throw invalid();
  if (typeof value.message !== "string" || !value.message.trim() || utf8Bytes(value.message) > MAX_ROUTED_MESSAGE_BYTES) throw invalid();
  if (!Number.isSafeInteger(value.remaining) || value.remaining < 0 || value.remaining > MAX_DEPTH - 1) throw invalid();
  const source = value.source;
  if (!exact(source, ["repository", "workflow", "run_id", "run_attempt"])) throw invalid();
  if (typeof source.repository !== "string" || !REPOSITORY.test(source.repository) || source.repository.length > 256) throw invalid();
  if (typeof source.workflow !== "string" || !source.workflow || source.workflow.length > 1024 || /[\u0000-\u001f\u007f]/.test(source.workflow)) throw invalid();
  if (typeof source.run_id !== "string" || !DECIMAL.test(source.run_id) || source.run_id.length > 20) throw invalid();
  if (!Number.isSafeInteger(source.run_attempt) || source.run_attempt < 1) throw invalid();
  return Object.freeze({
    version: 1,
    message: value.message,
    remaining: value.remaining,
    source: Object.freeze({ repository: source.repository, workflow: source.workflow, run_id: source.run_id, run_attempt: source.run_attempt }),
  });
}

/**
 * The trusted routed descriptor from the handler-manager config, or null for the
 * advisory form.
 * @param {any} descriptor
 * @returns {null | {route: "workqueue" | "issue" | "dispatch", max_depth: number, workflow?: string, workflow_file?: string, aw_context: boolean}}
 */
function normalizeRouteConfig(descriptor) {
  if (!plainObject(descriptor) || descriptor.route === undefined) return null;
  const route = descriptor.route;
  if (route !== "workqueue" && route !== "issue" && route !== "dispatch") throw routeError("continuation_config_invalid", "route must be workqueue, issue or dispatch");
  if (descriptor.max !== undefined && Number(descriptor.max) !== 1) throw routeError("continuation_config_invalid", "a routed request_continuation accepts max 1");
  // The issue route never spends automatic hops; its descriptor depth is unused.
  const maxDepth = descriptor.max_depth === undefined ? 1 : descriptor.max_depth;
  if (!Number.isSafeInteger(maxDepth) || maxDepth < 1 || maxDepth > MAX_DEPTH) throw routeError("continuation_config_invalid", "max_depth must be an integer from 1 to 16");
  if (route !== "dispatch") {
    if (descriptor.workflow !== undefined || descriptor.workflow_file !== undefined || descriptor.aw_context !== undefined) throw routeError("continuation_config_invalid", "only the dispatch route names a workflow");
    return { route, max_depth: maxDepth, aw_context: false };
  }
  const file = descriptor.workflow_file;
  if (typeof file !== "string" || !/^[A-Za-z0-9_.-]+\.(?:lock\.yml|yml|yaml)$/.test(file) || file.startsWith(".")) throw routeError("continuation_config_invalid", "dispatch requires a compiled local workflow_file");
  if (descriptor.aw_context !== undefined && typeof descriptor.aw_context !== "boolean") throw routeError("continuation_config_invalid", "aw_context must be boolean");
  return { route, max_depth: maxDepth, ...(typeof descriptor.workflow === "string" ? { workflow: descriptor.workflow } : {}), workflow_file: file, aw_context: descriptor.aw_context === true };
}

/**
 * Bind the one request of this run attempt's verified agent output.
 * @param {{assignment?: {claims: Array<{handle: string}>} | null, context?: any, path?: string, evidence?: unknown}} [options]
 * @returns {{ok: true, request: ReturnType<typeof collectRoutedRequest>, sha256: string} | {ok: false, reason: string}}
 */
function bindRoutedRequest(options = {}) {
  try {
    const { output, sha256 } = readBoundAgentOutput({
      path: options.path ?? process.env.GH_AW_AGENT_OUTPUT,
      evidence: options.evidence ?? process.env.GH_AW_CONTINUATION_EVIDENCE,
      context: options.context ?? global.context,
    });
    return { ok: true, request: collectRoutedRequest(output.items, { assignment: options.assignment ?? null }), sha256 };
  } catch (error) {
    return { ok: false, reason: error && typeof error === "object" && "code" in error ? String(error.code) : "continuation_evidence_invalid" };
  }
}

/**
 * Child size rule shared with the ledger: payload plus continuation within PAYLOAD_BYTES.
 * @param {unknown} payload @param {{route: string, message: string, remaining: number}} continuation
 */
function assertContinuationBudget(payload, continuation) {
  const { canonicalBytes } = require("./work_queue_codec.cjs");
  const { PAYLOAD_BYTES } = require("./work_queue_limits.cjs");
  if (canonicalBytes(payload) + canonicalBytes(continuation) > PAYLOAD_BYTES) throw routeError("continuation_payload_too_large");
}

/** Mirrors the Claim scope rule: a Work whose effect_contract forbids resource writes. @param {any} work */
function writesProhibited(work) {
  const contract = work?.effect_contract;
  return plainObject(contract) && (contract.kind === "none" || contract.no_writes === true);
}

/**
 * Manager decision for a `workqueue` request of one completed, authorized Claim.
 * @param {{member: any, request: {message: string, message_sha256: string}, maxDepth: number}} options
 */
function workqueueReceipt({ member, request, maxDepth }) {
  const inbound = member.continuation === undefined ? undefined : member.continuation?.remaining;
  if (member.continuation !== undefined && !Number.isSafeInteger(inbound)) throw routeError("continuation_inbound_invalid");
  const available = availableDepth(maxDepth, inbound);
  const receipt = { claim_handle: member.handle, claim_id: member.claim_id, message_sha256: request.message_sha256 };
  if (available === 0) return { ...receipt, status: "depth_limit" };
  assertContinuationBudget(member.work, { route: "workqueue", message: request.message, remaining: available - 1 });
  return { ...receipt, status: "ready" };
}

/** @param {any} context */
function sourceRunUrl(context) {
  const server = String(context?.serverUrl || process.env.GITHUB_SERVER_URL || "https://github.com").replace(/\/+$/, "");
  const { runId, runAttempt } = currentRun(context);
  return `${server}/${context.repo.owner}/${context.repo.repo}/actions/runs/${runId}/attempts/${runAttempt}`;
}

/**
 * Approval issue report: trusted text, with the agent message as an inert code block.
 * @param {{nodeKey: string, workId: string, message: string, runUrl: string}} options
 * @returns {import("./report_ast.cjs").Report}
 */
function approvalIssueReport({ nodeKey, workId, message, runUrl }) {
  const key = Array.from(String(nodeKey).replace(/[\u0000-\u001f\u007f]/g, " "))
    .slice(0, MAX_TITLE_KEY_CHARS)
    .join("");
  return {
    type: "report",
    title: `Approve continuation of ${key}`,
    agentTainted: false,
    children: [
      {
        type: "paragraph",
        agentTainted: false,
        children: [
          { type: "text", value: "Source run: ", agentTainted: false },
          { type: "link", url: runUrl, agentTainted: false, children: [{ type: "text", value: runUrl, agentTainted: false }] },
        ],
      },
      {
        type: "paragraph",
        agentTainted: false,
        children: [
          { type: "text", value: "Parent Work: ", agentTainted: false },
          { type: "code", value: workId, agentTainted: false },
        ],
      },
      { type: "paragraph", agentTainted: false, children: [{ type: "text", value: "Remaining work requested by the agent (untrusted text):", agentTainted: false }] },
      { type: "code_block", language: "text", value: message, agentTainted: true },
      { type: "paragraph", agentTainted: false, children: [{ type: "text", value: APPROVAL_INSTRUCTION, agentTainted: false }] },
    ],
  };
}

/** Parent Work's node key from the trusted activation snapshot, else its Work ID. @param {string} workId */
function parentNodeKey(workId) {
  try {
    const snapshot = require("./work_queue_claim_scope.cjs").readClaimScopeContext()?.snapshot;
    if (snapshot && typeof snapshot.transactionLog === "string" && snapshot.transactionLog) {
      const work = require("./work_queue_replay.cjs").replayTransactionLog(snapshot.transactionLog).works.get(workId);
      if (work && typeof work.node_key === "string" && work.node_key) return work.node_key;
    }
  } catch {
    // The title is presentation only; identity comes from the creation response.
  }
  return workId;
}

/**
 * Create the one approval issue in the queue repository through the caller's
 * (Claim-scoped) client. One POST, no transport retries; no valid identity fails closed.
 * @param {{client: any, context: any, member: any, request: {message: string}}} options
 */
async function createApprovalIssue({ client, context, member, request }) {
  const { createReportIssue } = require("./issue_report.cjs");
  const { issueResource, repositoryId } = require("./work_queue_issue_api.cjs");
  const repo = { owner: context.repo.owner, repo: context.repo.repo };
  const repository = `${repo.owner}/${repo.repo}`;
  const payloadRepository = context.payload?.repository;
  let repoId = payloadRepository?.full_name === repository && payloadRepository.id !== undefined ? String(payloadRepository.id) : undefined;
  repoId ??= await repositoryId(client, repo);
  if (!DECIMAL.test(String(repoId))) throw routeError("continuation_issue_invalid", "the queue repository has no native identity");
  const report = approvalIssueReport({ nodeKey: parentNodeKey(member.work_id), workId: member.work_id, message: request.message, runUrl: sourceRunUrl(context) });
  const response = await createReportIssue(client, { ...repo, title: report.title, body: "", request: { retries: 0 } }, report);
  const data = response?.data;
  const id = data?.id;
  const number = data?.number;
  if (!data || data.pull_request || !(Number.isSafeInteger(id) ? id > 0 : typeof id === "string" && DECIMAL.test(id)) || !Number.isSafeInteger(number) || number < 1) {
    throw routeError("continuation_issue_invalid", "approval issue creation returned no valid issue identity");
  }
  return { resource: issueResource({ id, number }, repository, String(repoId)), number, url: typeof data.html_url === "string" ? data.html_url : undefined };
}

/**
 * The repository's default branch; never a caller ref and never a guess.
 * @param {any} client @param {any} context
 */
async function resolveDefaultBranch(client, context) {
  const repository = `${context.repo.owner}/${context.repo.repo}`;
  const payloadRepository = context.payload?.repository;
  let branch = payloadRepository && (payloadRepository.full_name === undefined || payloadRepository.full_name === repository) ? payloadRepository.default_branch : undefined;
  if (typeof branch !== "string" || !branch) {
    try {
      const { data } = await client.rest.repos.get({ owner: context.repo.owner, repo: context.repo.repo });
      branch = data?.default_branch;
    } catch {
      branch = undefined;
    }
  }
  if (typeof branch !== "string" || !branch || /[\s\u0000-\u001f\u007f~^:?*[\\]/.test(branch) || branch.startsWith("refs/")) throw routeError("continuation_default_branch_unresolved");
  return branch;
}

/**
 * Inputs for the compiled receiver; agent text only inside the JSON string value.
 * @param {{message: string, remaining: number, context: any, awContext?: boolean}} options
 */
function buildDispatchInputs({ message, remaining, context, awContext = false }) {
  const { runId, runAttempt } = currentRun(context);
  const workflow = process.env.GITHUB_WORKFLOW_REF;
  if (typeof workflow !== "string" || !workflow || /[\u0000-\u001f\u007f]/.test(workflow)) throw routeError("continuation_source_invalid");
  const repository = `${context.repo.owner}/${context.repo.repo}`;
  /** @type {Record<string, string>} */
  const inputs = { gh_aw_continuation: JSON.stringify({ version: 1, message, remaining, source: { repository, workflow, run_id: runId, run_attempt: runAttempt } }) };
  if (awContext) inputs.aw_context = JSON.stringify(require("./aw_context.cjs").buildAwContext());
  if (utf8Bytes(JSON.stringify(inputs)) > MAX_DISPATCH_INPUT_BYTES) throw routeError("continuation_inputs_too_large");
  return inputs;
}

/** @param {unknown} error */
function httpStatus(error) {
  const status = error && typeof error === "object" && "status" in error ? Number(error.status) : NaN;
  return Number.isInteger(status) ? status : undefined;
}

/**
 * Route one accepted ordinary request to the compiled receiver.
 * @param {{client: any, context: any, descriptor: NonNullable<ReturnType<typeof normalizeRouteConfig>>, request: {message: string}, staged?: boolean, coreApi?: any}} options
 */
async function dispatchContinuation({ client, context, descriptor, request, staged = false, coreApi = global.core }) {
  const workflow = /** @type {string} */ descriptor.workflow_file;
  const base = { route: "dispatch", workflow };
  let inbound;
  try {
    inbound = parseInboundDispatchContinuation(context.payload);
  } catch {
    return { success: false, ...base, errorCode: "continuation_inbound_invalid", error: "Inbound gh_aw_continuation is malformed; the request was not routed" };
  }
  const available = availableDepth(descriptor.max_depth, inbound?.remaining);
  if (available === 0) {
    coreApi.info("request_continuation: depth limit reached; request kept, no dispatch");
    return { success: true, ...base, status: "depth_limit" };
  }
  const inputs = buildDispatchInputs({ message: request.message, remaining: available - 1, context, awContext: descriptor.aw_context });
  if (staged) {
    coreApi.info(`🎭 Staged Mode Preview — would dispatch ${workflow} on the default branch`);
    return { success: true, staged: true, ...base, status: "preview" };
  }
  const branch = await resolveDefaultBranch(client, context);
  const { dispatchWorkflow } = require("./dispatch_workflow_transport.cjs");
  let response;
  try {
    response = await dispatchWorkflow(
      client,
      { owner: context.repo.owner, repo: context.repo.repo, workflow_id: workflow, ref: `refs/heads/${branch}`, inputs, return_run_details: true, request: { retries: 0 } },
      { allowLegacyFallback: true, coreApi }
    );
  } catch (error) {
    const status = httpStatus(error);
    if (status !== undefined && status >= 400 && status < 500) {
      return { success: false, ...base, status: "rejected", errorCode: "continuation_dispatch_rejected", error: `Continuation dispatch of ${workflow} was rejected (HTTP ${status})` };
    }
    // Sent or possibly sent: never report as not run, never resend.
    return { success: false, ...base, status: "unknown", operation_outcome: "indeterminate", errorCode: "continuation_dispatch_outcome_unknown", error: `Continuation dispatch of ${workflow}: outcome unknown; it was not retried` };
  }
  const runId = response?.data?.workflow_run_id;
  coreApi.info(`request_continuation: dispatch of ${workflow} accepted${runId ? ` (run ID: ${runId})` : ""}`);
  return { success: true, ...base, status: "accepted", ...(runId !== undefined && runId !== null ? { run_id: String(runId) } : {}) };
}

/**
 * Step-summary lines for routed results (no agent text).
 * @param {Array<any>} results
 */
function continuationSummary(results) {
  const lines = [];
  for (const record of results || []) {
    if (record?.type !== "request_continuation" || !record.result?.route) continue;
    const result = record.result;
    const parts = [`route \`${result.route}\``, `status \`${result.status ?? (record.success ? "accepted" : "failed")}\``];
    if (result.workflow) parts.push(`workflow \`${result.workflow}\``);
    if (result.run_id) parts.push(`run \`${result.run_id}\``);
    if (result.issue_number) parts.push(`approval issue #${result.issue_number}`);
    lines.push(`- ${parts.join(", ")}`);
  }
  return lines.length ? `### Continuation routing\n\n${lines.join("\n")}\n\n` : "";
}

module.exports = {
  APPROVAL_INSTRUCTION,
  MAX_DEPTH,
  MAX_DISPATCH_INPUT_BYTES,
  MAX_ROUTED_MESSAGE_BYTES,
  ROUTES,
  approvalIssueReport,
  assertContinuationBudget,
  availableDepth,
  bindRoutedRequest,
  buildDispatchInputs,
  collectRoutedRequest,
  continuationSummary,
  createApprovalIssue,
  dispatchContinuation,
  messageDigest,
  normalizeRouteConfig,
  parseContinuationEvidence,
  parseInboundDispatchContinuation,
  readBoundAgentOutput,
  resolveDefaultBranch,
  routedRequestError,
  workqueueReceipt,
  writesProhibited,
};
