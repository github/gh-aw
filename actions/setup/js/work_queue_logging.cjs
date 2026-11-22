// @ts-check
"use strict";

const { createLogger, debugEnabled: matchesDebugNamespace } = require("./mcp_logger.cjs");
const { performance } = require("node:perf_hooks");

/** @type {Map<string, number>} */
const lastEmittedAt = new Map();
const MAX_GRAPHQL_ERRORS = 50;
const MAX_GRAPHQL_ERROR_DETAILS = 5;
/** @typedef {"lock" | "unlock" | "projection" | "createLabel" | "updateLabel"} GraphQLOperationContext */
const GRAPHQL_ERROR_FIELDS = new Map([
  ["RATE_LIMITED", "graphql_rate_limited"],
  ["FORBIDDEN", "graphql_forbidden"],
  ["UNPROCESSABLE", "graphql_unprocessable"],
  ["NOT_FOUND", "graphql_not_found"],
  ["GRAPHQL_VALIDATION_FAILED", "graphql_validation_failed"],
  ["INTERNAL", "graphql_internal"],
]);
const GRAPHQL_OPERATIONS = new Map([
  ["createLabel", "graphql_operation_create_label"],
  ["updateLabel", "graphql_operation_update_label"],
]);
const MAX_GRAPHQL_ALIASES = 50;
const GRAPHQL_ALIAS_KINDS = new Map([
  ["l", { context: "lock", field: "graphql_lock_alias", operation: "graphql_operation_create_ref" }],
  ["m", { context: "projection", field: "graphql_projection_alias", operation: "graphql_operation_issue_projection" }],
  ["u", { context: "unlock", field: "graphql_unlock_alias", operation: "graphql_operation_delete_ref" }],
]);

const CONTROL_ERROR_EXPLANATIONS = {
  request_invalid: "The queue request or one of its operations is malformed. Inspect the staged intent and the compiled policy environment; replay rejects requests outside the closed v4 shapes.",
  unauthorized_operation: "This job's queue role cannot publish the requested operation. Check the workflow's tools.work-queue role; dispatchers claim Work, workers settle only their own bound Claims.",
  actor_unauthorized: "The publishing job belongs to another repository than the queue. Queue writes come only from workflows of the queue's own repository.",
  unsupported_protocol: "The queue ledger is not protocol v4. A dispatcher, producer or maintenance run converts a v3 ledger before any v4 dispatch.",
  ledger_invalid: "The queue ledger failed validation. Inspect the work-queue branch history; do not delete the queue.",
  checkpoint_invalid: "The queue checkpoint failed validation. Inspect the work-queue branch history and restore the prior content from Git; do not delete the queue.",
  cas_exhausted: "The queue branch changed on every publication attempt. Concurrent writers are active; the next run re-plans against the new head.",
  ledger_limit: "The ledger reached its byte bound. Run agentics-maintenance to checkpoint the queue.",
  repository_unavailable: "The queue repository could not be read with the selected token. Check the token's contents permission on this repository.",
  policy_missing: "The compiled work queue policy environment is missing. Recompile the workflow so GH_AW_WORK_QUEUE_POLICY is emitted.",
  policy_invalid: "The compiled work queue policy environment is invalid. Fix .github/workflows/aw.json work_queue settings and recompile.",
  pool_invalid: "The requested pool is not configured. Declare it in .github/workflows/aw.json work_queue.pools and recompile.",
  work_queue_profile_not_approved: "The requested worker profile is not configured for the pool. Mark the worker workflow with tools.work-queue.worker: true and recompile.",
  work_queue_pending_limit: "The pool already holds its configured pending_limit of available and claimed Work. Let workers drain the pool or raise work_queue.pending_limit in aw.json.",
  work_queue_graph_limit: "The submission exceeds the bounded number of Work nodes per request. Split the graph into smaller submissions.",
  work_queue_intent_invalid: "A staged queue intent is malformed. Inspect the work_queue_* tool calls of the agent run.",
  work_queue_plan_intent_batch_invalid: "The staged plan is incomplete or mixed with other intents. Stage exactly one submit and one dispatch_next from the plan.",
  work_queue_dispatch_budget_exceeded: "The dispatch request exceeds this run's dispatch budget. Lower max_dispatches or raise safe-outputs.dispatch-workflow.max.",
  work_queue_dispatcher_intents_missing: "The dispatcher staged no queue intents. Inspect the preparation step and tool-denial logs.",
  work_queue_control_claim_not_authorized: "The worker has no open Claim bound to this run, so it cannot submit child Work.",
  work_queue_worker_dispatch_unsupported: "Workers cannot claim Work; only dispatcher workflows publish dispatch_next.",
  work_queue_dispatch_ref_missing: "No dispatch ref is configured. Set safe-outputs.dispatch-workflow target-ref or run the dispatcher on a branch.",
  external_admission_unverified: "An external dependency, subject or backing Issue could not be identified. Check the reference and the dependency read credentials.",
  work_missing: "The request references Work that is not in the queue.",
  ownership_conflict: "The Work is no longer available; another dispatch claimed or settled it first.",
  dispatch_state_conflict: "The Dispatch already advanced past this transition.",
  run_binding_conflict: "The Dispatch is bound to another run. Only the bound run may settle its Claims.",
  claim_scope_invalid: "The operation is outside the bound run's Claim scope.",
  resource_limit: "The queue operation exceeded a bounded protocol limit. Reduce the batch or payload size.",
  assignment_limit: "The Work payload plus predecessor effects cannot fit one worker assignment. Reduce the payload or the number of predecessors.",
  dependency_invalid: "A Work dependency is invalid: predecessors must already exist in the same graph and pool.",
  dependency_cycle: "The submitted Work graph contains a cycle.",
  graph_invalid: "The submitted graph is invalid: node keys must be unique and a graph stays in one pool.",
  resource_invalid: "A GitHub resource reference is not fully identified.",
  issue_binding_conflict: "The backing Issue already belongs to other Work.",
  work_identity_invalid: "The Work identity does not match its graph_id and node_key.",
  work_invalid: "The Work payload or replacement reference is invalid.",
  staged_queue_intents_invalid: "The staged queue intents could not be read. Inspect the work_queue_* tool calls of the agent run.",
  queue_control_github_access_denied: "GitHub denied the queue API request (HTTP 403). Inspect the rate-limit and SSO diagnostics before changing token permissions.",
  queue_control_github_authentication_failed: "GitHub rejected the credential (HTTP 401). Check that the selected token is present and valid; never print the token.",
  queue_control_github_not_found: "GitHub could not find the requested queue resource (HTTP 404). Check the repository and the selected token's access.",
  queue_control_github_rate_limited: "GitHub rate-limited the queue request. The next scheduled run retries against the current ledger.",
  queue_control_github_api_unavailable: "GitHub returned a server error. The next run re-reads the ledger before retrying.",
  queue_control_github_transport_failed: "The GitHub client failed before receiving an HTTP response. The next run re-reads the ledger before retrying.",
  queue_control_github_request_rejected: "GitHub rejected the queue API request (HTTP 422). Verify the worker workflow, ref and input schema.",
  queue_control_dispatch_ref_missing: "GitHub could not find the dispatch ref. Check safe-outputs.dispatch-workflow target-ref.",
  queue_control_dispatch_inputs_mismatch: "The worker workflow does not accept the work_queue_assignment input. Recompile and publish the worker.",
  queue_control_dispatch_trigger_missing: "The worker workflow has no workflow_dispatch trigger. Recompile and publish the queue worker.",
};

/** @param {string} code */
function queueControlErrorExplanation(code) {
  return Object.hasOwn(CONTROL_ERROR_EXPLANATIONS, code)
    ? CONTROL_ERROR_EXPLANATIONS[code]
    : "The queue control failed for an unclassified reason. Inspect the ledger with DEBUG=work-queue:*; raw exception details are withheld to protect credentials and task data.";
}

/** @param {unknown} value @param {string} key */
function ownValue(value, key) {
  try {
    return value && typeof value === "object" ? Object.getOwnPropertyDescriptor(value, key)?.value : undefined;
  } catch {
    return undefined;
  }
}

/** @param {unknown} error */
function responseHeaders(error) {
  return ownValue(ownValue(error, "response"), "headers") ?? ownValue(error, "headers");
}

/**
 * Returns a GitHub request ID only in its five-group hex shape; anything else could be provider
 * text or a credential. The value is returned unchanged, never trimmed or truncated.
 * @param {unknown} value
 * @returns {string | undefined}
 */
function validGitHubRequestId(value) {
  return typeof value === "string" && value.length <= 84 && !/\s/.test(value) && /^[0-9a-f]{1,16}(?::[0-9a-f]{1,16}){4}$/i.test(value) ? value : undefined;
}

/**
 * The validated `x-github-request-id` of a GitHub API error, preferring the response headers.
 * Accessors and inherited headers are never read.
 * @param {unknown} error
 * @returns {string | undefined}
 */
function getGitHubRequestId(error) {
  return validGitHubRequestId(ownValue(ownValue(ownValue(error, "response"), "headers"), "x-github-request-id")) ?? validGitHubRequestId(ownValue(ownValue(error, "headers"), "x-github-request-id"));
}

/** @param {unknown} headers @param {string} name */
function headerInteger(headers, name) {
  const value = ownValue(headers, name);
  if (typeof value !== "string" || /\s/.test(value) || !/^(?:0|[1-9][0-9]{0,15})$/.test(value)) return undefined;
  const parsed = Number(value);
  return Number.isSafeInteger(parsed) ? parsed : undefined;
}

/** @param {unknown} error @param {unknown} data @param {number} index @param {GraphQLOperationContext} [operationContext] */
function graphqlErrorMetadata(error, data, index, operationContext) {
  /** @type {Record<string, number | boolean>} */
  const fields = { index };
  const type = ownValue(error, "type");
  fields[GRAPHQL_ERROR_FIELDS.get(type) || "graphql_unknown_type"] = true;
  const path = ownValue(error, "path");
  const depth = ownValue(path, "length");
  fields.graphql_path_present = Number.isSafeInteger(depth) && depth > 0;
  const root = fields.graphql_path_present ? ownValue(path, "0") : undefined;
  const alias = typeof root === "string" && root.length <= 3 ? /^(l|m|u)(0|[1-9][0-9]?)$/.exec(root) : null;
  let operation = operationContext && root === operationContext ? GRAPHQL_OPERATIONS.get(operationContext) : undefined;
  const kind = alias && GRAPHQL_ALIAS_KINDS.get(alias[1]);
  if (alias && kind && kind.context === operationContext && Number(alias[2]) < MAX_GRAPHQL_ALIASES) {
    fields[kind.field] = Number(alias[2]);
    operation = kind.operation;
  }
  if (operation) {
    fields[operation] = true;
    fields.graphql_result_present = ownValue(data, root) != null;
  } else if (fields.graphql_path_present) {
    fields.graphql_path_unclassified = true;
  }
  // Exact provider wording is diagnostic evidence only, never proof of no write.
  if (type === "UNPROCESSABLE" && operation === "graphql_operation_create_ref" && ownValue(error, "message") === "A reference with that name already exists.") fields.graphql_reference_already_exists = true;
  return fields;
}

/** @param {unknown} error */
function githubFailureMetadata(error) {
  const response = ownValue(error, "response");
  const headers = responseHeaders(error);
  /** @type {Record<string, number | boolean>} */
  const fields = {};
  for (const [header, field] of [
    ["x-ratelimit-limit", "rate_limit_limit"],
    ["x-ratelimit-remaining", "rate_limit_remaining"],
    ["x-ratelimit-used", "rate_limit_used"],
    ["x-ratelimit-reset", "rate_limit_reset_unix"],
    ["retry-after", "retry_after_seconds"],
  ]) {
    const value = headerInteger(headers, header);
    if (value !== undefined) fields[field] = value;
  }
  const resource = ownValue(headers, "x-ratelimit-resource");
  if (resource === "core") fields.rate_limit_resource_core = true;
  if (resource === "graphql") fields.rate_limit_resource_graphql = true;
  const sso = ownValue(headers, "x-github-sso");
  if (typeof sso === "string" && sso.length <= 8192 && /^required(?:;|$)/.test(sso)) fields.sso_required = true;
  const message = ownValue(ownValue(response, "data"), "message");
  if (typeof message === "string" && message.length <= 8192) {
    if (message.startsWith("API rate limit exceeded")) fields.rate_limit_message = true;
    if (message.startsWith("API rate limit exceeded for installation")) fields.installation_rate_limit = true;
    if (message.startsWith("You have exceeded a secondary rate limit") || message.startsWith("You have triggered an abuse detection mechanism")) fields.secondary_rate_limit = true;
    if (message === "Resource not accessible by integration" || message === "Resource not accessible by personal access token") fields.resource_not_accessible = true;
  }
  const errors = ownValue(error, "errors");
  const request = ownValue(error, "request");
  const method = ownValue(request, "method");
  if (["GET", "POST", "PATCH", "PUT", "DELETE", "HEAD"].includes(method)) fields[`http_method_${method.toLowerCase()}`] = true;
  if (request !== undefined) fields.request_present = true;
  if (request !== undefined && response === undefined && ownValue(error, "status") === 500) fields.transport_response_missing = true;
  if (response !== undefined) fields.response_present = true;
  if (headers !== undefined) {
    fields.response_headers_present = true;
    fields.github_request_id_present = typeof ownValue(headers, "x-github-request-id") === "string";
  }
  const transportCodes = ["ETIMEDOUT", "ECONNRESET", "UND_ERR_SOCKET", "UND_ERR_CONNECT_TIMEOUT", "UND_ERR_HEADERS_TIMEOUT", "UND_ERR_BODY_TIMEOUT", "ERR_TRANSPORT_INTEGRITY"];
  let cause = error;
  for (let depth = 0; depth < 5 && cause && typeof cause === "object"; depth++, cause = ownValue(cause, "cause")) {
    const code = ownValue(cause, "code");
    if (transportCodes.includes(code)) fields[`transport_${code.toLowerCase()}`] = true;
  }
  const count = ownValue(errors, "length");
  if (Number.isSafeInteger(count) && count > 0) {
    fields.graphql_errors = count;
    if (count > MAX_GRAPHQL_ERRORS) {
      fields.graphql_errors_truncated = true;
      fields.graphql_error_types_inspected = false;
    } else {
      for (let index = 0; index < count; index++) {
        const type = ownValue(ownValue(errors, String(index)), "type");
        const field = GRAPHQL_ERROR_FIELDS.get(type);
        if (field) fields[field] = true;
      }
    }
  }
  return fields;
}

/** @param {unknown} error @param {Record<string, number | boolean>} [metadata] */
function queueControlErrorCode(error, metadata) {
  for (const key of ["code", "message"]) {
    const code = ownValue(error, key);
    if (typeof code === "string" && Object.hasOwn(CONTROL_ERROR_EXPLANATIONS, code)) return code;
  }
  const response = ownValue(error, "response");
  const status = ownValue(error, "status") ?? ownValue(response, "status");
  if (status === 401) return "queue_control_github_authentication_failed";
  metadata ??= githubFailureMetadata(error);
  if (
    (status === 403 && ((metadata.rate_limit_remaining === 0 && !metadata.resource_not_accessible) || metadata.retry_after_seconds !== undefined || metadata.rate_limit_message || metadata.secondary_rate_limit)) ||
    metadata.graphql_rate_limited
  )
    return "queue_control_github_rate_limited";
  if (status === 403) return "queue_control_github_access_denied";
  if (status === 404) return "queue_control_github_not_found";
  if (status === 429) return "queue_control_github_rate_limited";
  if (status === 500 && metadata.transport_response_missing) return "queue_control_github_transport_failed";
  if (Number.isInteger(status) && status >= 500 && status <= 599) return "queue_control_github_api_unavailable";
  if (status === 422) {
    const message = ownValue(ownValue(response, "data"), "message");
    if (typeof message === "string" && message.length <= 8192) {
      if (message.startsWith("No ref found for:")) return "queue_control_dispatch_ref_missing";
      if (message.startsWith("Unexpected inputs provided:")) return "queue_control_dispatch_inputs_mismatch";
      if (message === "Workflow does not have 'workflow_dispatch' trigger") return "queue_control_dispatch_trigger_missing";
    }
    return "queue_control_github_request_rejected";
  }
  return "queue_control_authority_or_publication_failed";
}

/** @param {string} namespace */
function debugEnabled(namespace) {
  if (process.env.ACTIONS_RUNNER_DEBUG === "true" || process.env.RUNNER_DEBUG === "1") return true;
  return matchesDebugNamespace(namespace);
}

/**
 * Events and field names must be developer-authored. Values are counts/flags
 * only; failure explanations may include a validated GitHub request ID.
 * Never pass identities, payloads, credentials or raw errors.
 * @param {string} component
 */
function createWorkQueueLogger(component) {
  const namespace = `work-queue:${component}`;
  const logger = createLogger(namespace);
  /** @param {string} event @param {Record<string, unknown>} [fields] @param {string} [explanation] */
  function diagnostic(event, fields = {}, explanation) {
    const metadata = Object.entries(fields).filter(([, value]) => typeof value === "boolean" || (typeof value === "number" && Number.isSafeInteger(value) && value >= 0));
    const now = performance.now();
    const previous = lastEmittedAt.get(namespace) ?? now;
    const elapsed = Math.max(0, Math.round(now - previous));
    logger.diagnostic([event, ...metadata.map(([key, value]) => `${key}=${value}`), ...(explanation ? ["--", explanation] : []), `+${elapsed}ms`].join(" "));
    lastEmittedAt.set(namespace, now);
  }
  /** @param {string} event @param {Record<string, unknown>} [fields] */
  function debug(event, fields = {}) {
    if (!debugEnabled(namespace)) return;
    diagnostic(event, fields);
  }
  /** @param {string} event @param {unknown} error @param {boolean} [always] @param {GraphQLOperationContext} [operationContext] */
  function failure(event, error, always = false, operationContext) {
    if (!always && !debugEnabled(namespace)) return;
    // Do not use debugError: API errors may contain request bodies and tokens.
    const response = ownValue(error, "response");
    const status = ownValue(error, "status") ?? ownValue(response, "status");
    const message = ownValue(ownValue(response, "data"), "message");
    const boundedMessage = typeof message === "string" && message.length <= 8192 ? message : "";
    const metadata = githubFailureMetadata(error);
    const count = typeof metadata.graphql_errors === "number" && debugEnabled(namespace) ? Math.min(metadata.graphql_errors, MAX_GRAPHQL_ERROR_DETAILS) : 0;
    if (typeof metadata.graphql_errors === "number" && metadata.graphql_errors > count) metadata.graphql_error_details_omitted = metadata.graphql_errors - count;
    const code = queueControlErrorCode(error, metadata);
    const headers = responseHeaders(error);
    const requestId = getGitHubRequestId(error);
    const requestDiagnostic = (Number.isInteger(status) && status >= 100 && status <= 599) || headers !== undefined || metadata.graphql_errors ? ` GitHub request ID: ${requestId ?? "unknown"}.` : "";
    diagnostic(
      event,
      {
        failed: true,
        ...(Number.isInteger(status) && status >= 100 && status <= 599 ? { http_status: status } : {}),
        ...metadata,
        ...(status === 422
          ? {
              ref_not_found: boundedMessage.startsWith("No ref found for:"),
              unexpected_inputs: boundedMessage.startsWith("Unexpected inputs provided:"),
              dispatch_trigger_missing: boundedMessage === "Workflow does not have 'workflow_dispatch' trigger",
            }
          : {}),
        [code]: true,
      },
      queueControlErrorExplanation(code) + requestDiagnostic
    );
    const errors = ownValue(error, "errors");
    const data = ownValue(error, "data") ?? ownValue(ownValue(response, "data"), "data");
    for (let index = 0; index < count; index++) {
      const detail = graphqlErrorMetadata(ownValue(errors, String(index)), data, index, operationContext);
      if (metadata.graphql_errors_truncated) detail.graphql_error_sample_only = true;
      diagnostic(`${event}.graphql_error`, detail);
    }
  }
  return { debug, diagnostic, failure };
}

module.exports = { createWorkQueueLogger, debugEnabled, getGitHubRequestId, ownValue, queueControlErrorCode, queueControlErrorExplanation, validGitHubRequestId };
