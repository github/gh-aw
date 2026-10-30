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

const CONTROL_ERROR_CODES = new Set([
  "checkpoint_invalid",
  "resource_limit",
  "work_queue_dispatch_credential_binding_required",
  "work_queue_dispatch_credential_auth_unavailable",
  "work_queue_dispatch_credential_client_mismatch",
  "work_queue_dispatch_credential_identity_unavailable",
  "work_queue_dispatch_credential_identity_unverified",
  "work_queue_dispatch_credential_principal_mismatch",
  "work_queue_dispatch_credential_proof_invalid",
  "work_queue_dispatch_credential_validator_required",
  "work_queue_dispatch_repository_access_unavailable",
  "work_queue_dispatch_repository_identity_mismatch",
  "work_queue_dispatch_tag_mismatch",
  "work_queue_dispatch_tag_preparation_failed",
  "work_queue_assignment_mismatch",
  "work_queue_assignment_profile_missing",
  "work_queue_policy_proposal_mismatch",
  "work_queue_pool_not_installed",
  "work_queue_profile_not_approved",
  "work_queue_worker_not_compiler_approved",
  "work_queue_dispatch_credential_kind_invalid",
  "work_queue_dispatch_app_metadata_missing",
  "work_queue_dispatch_credential_metadata_conflict",
  "queue_control_github_access_denied",
  "queue_control_github_authentication_failed",
  "queue_control_github_not_found",
  "queue_control_github_rate_limited",
  "queue_control_github_api_unavailable",
  "queue_control_github_transport_failed",
  "queue_control_github_request_rejected",
  "queue_control_dispatch_ref_missing",
  "queue_control_dispatch_inputs_mismatch",
  "queue_control_dispatch_trigger_missing",
]);

/** @param {string} code */
function queueControlErrorExplanation(code) {
  switch (code) {
    case "work_queue_dispatch_repository_access_unavailable":
      return "The dispatch client cannot check the target repository. Use the authenticated GitHub client created by the compiled safe-output job.";
    case "work_queue_dispatch_credential_binding_required":
    case "work_queue_dispatch_credential_auth_unavailable":
    case "work_queue_dispatch_credential_client_mismatch":
      return "The dispatch client could not be bound to the compiler-selected token. Fix trusted client construction so credential validation and workflow dispatch use the same selected token; do not substitute the caller's credential.";
    case "work_queue_dispatch_credential_identity_unavailable":
    case "work_queue_dispatch_credential_identity_unverified":
      return "The dispatch credential's GitHub identity could not be verified. Check the selected token's validity and GitHub API access; do not infer identity from the workflow's human caller.";
    case "work_queue_dispatch_credential_principal_mismatch":
      return "The selected token belongs to a different identity than the original assignment permits. Use the approved worker credential; do not reassign the Claim or bypass the identity check.";
    case "work_queue_dispatch_credential_proof_invalid":
    case "work_queue_dispatch_credential_validator_required":
      return "The assignment has no valid credential proof from the actual dispatch client. Restore the compiled credential validator and verify the original route before launching.";
    case "work_queue_dispatch_repository_identity_mismatch":
      return "GitHub returned a different repository than the approved target. Check the repository and API host configuration; do not broaden the assignment's scope.";
    case "work_queue_dispatch_tag_mismatch":
      return "The worker snapshot tag does not point to the approved immutable commit. Inspect the tag and worker revision; never overwrite the conflicting tag or launch from a moving branch.";
    case "work_queue_dispatch_tag_preparation_failed":
      return "The worker snapshot tag could not be prepared and verified. Check GitHub API access and contents: write on the selected dispatch credential before retrying through trusted reconciliation.";
    case "work_queue_assignment_mismatch":
    case "work_queue_assignment_profile_missing":
      return "The stored assignment does not match a valid approved worker route. Inspect the original Claim and installed deployment; do not rebuild an assignment from task text or a stale snapshot.";
    case "work_queue_policy_proposal_mismatch":
      return "The proposed queue policy differs from the installed policy. Refresh compatible worker deployments, or use an explicit administrator policy update only after the queue is quiescent.";
    case "work_queue_pool_not_installed":
      return "The requested pool could not be installed on demand. Declare it in aw.json, publish new workers' Markdown and compiled locks to the default branch, and recompile the producer or dispatcher. Installed pool settings and profiles must remain unchanged; other changes require an administrator Policy update after the queue is quiescent.";
    case "work_queue_profile_not_approved":
      return "The requested worker profile could not be registered on demand. Publish its tools.work-queue.worker source and compiled lock to the default branch, then recompile the producer or dispatcher. Existing worker authority cannot be changed by additive registration.";
    case "work_queue_worker_not_compiler_approved":
      return "The worker route is not compiler-approved. New workers require a published default-branch AW worker source, matching compiled contract and active registration. Check the caller's dispatch allowlist; existing authority cannot be replaced by registration.";
    case "checkpoint_invalid":
    case "queue_control_checkpoint_invalid":
      return "The queue checkpoint failed validation. Inspect the authoritative ledger and restore a valid causal history; do not delete the queue or reset its scheduling debt.";
    case "resource_limit":
      return "The queue operation exceeded a bounded protocol limit. Reduce the batch or assignment size; compact the ledger only through the supported command without discarding history.";
    case "work_queue_dispatch_credential_kind_invalid":
    case "work_queue_dispatch_app_metadata_missing":
    case "work_queue_dispatch_credential_metadata_conflict":
      return "The compiled dispatch credential configuration is missing or inconsistent. Recompile the dispatcher with its declared safe-outputs.dispatch-workflow workers and approved credential source.";
    case "queue_control_github_access_denied":
      return "GitHub denied the queue API request (HTTP 403). This status alone does not distinguish permission denial from throttling. Inspect the rate-limit and SSO diagnostics before changing credential permissions; keep the agent job read-only.";
    case "queue_control_github_authentication_failed":
      return "GitHub rejected the credential (HTTP 401). Check that the compiler-selected token is present, valid, and unexpired in the trusted safe-output job; never print the token.";
    case "queue_control_github_not_found":
      return "GitHub could not find or expose the requested queue resource (HTTP 404). Check the approved repository, worker revision, workflow path, and selected token's access.";
    case "queue_control_github_rate_limited":
      return "GitHub rate-limited the queue request. Respect the reported reset or retry timing, then inspect the authoritative ledger and reconcile existing reservations before retrying.";
    case "queue_control_github_api_unavailable":
      return "GitHub returned a server error. A write or worker launch may already have happened; inspect the ledger and reconcile the original reservation before retrying.";
    case "queue_control_github_transport_failed":
      return "The GitHub client failed before receiving an HTTP response; its synthetic status is not a GitHub server response. Inspect transport diagnostics and reconcile the authoritative ledger and original reservation before retrying; failure does not prove that no write or worker launch occurred.";
    case "queue_control_dispatch_ref_missing":
      return "GitHub could not find the worker snapshot ref. Verify that the approved immutable snapshot tag exists in the destination repository before trusted reconciliation.";
    case "queue_control_dispatch_inputs_mismatch":
      return "The published worker workflow does not accept the dispatch inputs. Recompile and publish the worker with its work_queue_assignment input, then reconcile the original reservation.";
    case "queue_control_dispatch_trigger_missing":
      return "The published worker workflow has no workflow_dispatch trigger. Recompile and publish the queue worker with that trigger, then reconcile the original reservation.";
    case "queue_control_github_request_rejected":
      return "GitHub rejected the queue API request (HTTP 422). Verify the compiled worker route, snapshot ref, workflow_dispatch trigger, and input schema; inspect the original reservation before retrying.";
    default:
      return "The queue control failed for an unclassified reason. Inspect the durable request and ledger with DEBUG=work-queue:*; raw exception details are withheld to protect credentials and task data.";
  }
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
    if (typeof code === "string" && CONTROL_ERROR_CODES.has(code)) return code;
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
    const requestId = [ownValue(ownValue(response, "headers"), "x-github-request-id"), ownValue(ownValue(error, "headers"), "x-github-request-id")].find(
      value => typeof value === "string" && value.length <= 84 && !/\s/.test(value) && /^[0-9a-f]{1,16}(?::[0-9a-f]{1,16}){4}$/i.test(value)
    );
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

module.exports = { createWorkQueueLogger, debugEnabled, queueControlErrorCode, queueControlErrorExplanation };
