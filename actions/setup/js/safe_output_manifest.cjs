// @ts-check

const fs = require("fs");
const nodePath = require("path");
const { getErrorMessage } = require("./error_helpers.cjs");
const { ERR_SYSTEM } = require("./error_codes.cjs");
const { MANIFEST_FILE_PATH, TEMPORARY_ID_MAP_FILE_PATH, SAFE_OUTPUT_ERRORS_FILE_PATH } = require("./constants.cjs");
const { redactBuiltInPatterns, redactSecrets, extractMCPGatewayTokens, MCP_GATEWAY_CONFIG_PATHS } = require("./redact_secrets.cjs");

/**
 * Collect custom secret values for artifact redaction.
 *
 * Mirrors the redaction inputs used by redact_secrets.cjs:
 * - workflow-configured secrets from GH_AW_SECRET_NAMES / SECRET_*
 * - dynamically minted MCP gateway bearer tokens discovered from config files
 *
 * @returns {string[]} Unique secret values suitable for redactSecrets()
 */
function collectArtifactSecretValues() {
  /** @type {Set<string>} */
  const secretValues = new Set();

  const secretNames = (process.env.GH_AW_SECRET_NAMES || "")
    .split(",")
    .map(name => name.trim())
    .filter(Boolean);
  for (const secretName of secretNames) {
    const secretValue = process.env[`SECRET_${secretName}`];
    if (typeof secretValue === "string" && secretValue.trim() !== "") {
      secretValues.add(secretValue.trim());
    }
  }

  for (const gatewayToken of extractMCPGatewayTokens(MCP_GATEWAY_CONFIG_PATHS)) {
    if (typeof gatewayToken === "string" && gatewayToken.trim() !== "") {
      secretValues.add(gatewayToken.trim());
    }
  }

  return [...secretValues];
}

/**
 * Safe output types that create new items in GitHub (these typically return a URL,
 * but the URL may be omitted in some cases).
 * Kept for backward compatibility.
 * @type {Set<string>}
 */
const CREATE_ITEM_TYPES = new Set([
  "create_issue",
  "add_comment",
  "create_discussion",
  "create_pull_request",
  "create_project",
  "create_project_status_update",
  "create_pull_request_review_comment",
  "submit_pull_request_review",
  "reply_to_pull_request_review_comment",
  "create_code_scanning_alert",
  "autofix_code_scanning_alert",
]);

/**
 * Safe output types that should NEVER be logged to the manifest.
 * These types represent metadata signals rather than GitHub state changes:
 * - noop: no-op message, produces no GitHub side effects
 * - missing_tool: records a missing tool capability (metadata only)
 * - missing_data: records missing required data (metadata only)
 * - report_incomplete: signals that the task could not be completed (metadata only)
 *
 * All other types — built-in handler types, custom safe job types, and
 * any future types — are logged automatically without needing to update this list.
 * @type {Set<string>}
 */
const NOT_LOGGED_TYPES = new Set(["noop", "missing_tool", "missing_data", "report_incomplete"]);

/**
 * @typedef {Object} ManifestEntry
 * @property {string} type - The safe output type (e.g., "create_issue")
 * @property {string} [url] - URL of the affected item in GitHub (present for creation types; omitted for modification types that don't return a URL)
 * @property {number} [number] - Issue/PR/discussion number if applicable
 * @property {string} [repo] - Repository slug (owner/repo) if applicable
 * @property {string} [provider] - Backing service (for example github, jira, or linear)
 * @property {string|number} [id] - Provider-specific database or node ID
 * @property {string} [identifier] - Provider-specific human-readable identifier
 * @property {Object} [target] - Entity associated with this operation
 * @property {Array<{name: string, database_id?: number, node_id?: string}>} [labels] - Labels added by this operation
 * @property {string} [temporaryId] - Temporary ID assigned to this item, if any
 * @property {Record<string, any>} [metadata] - Persisted outcome metadata captured at execution time
 * @property {Object} [before_state] - Execution-time state snapshot captured before mutation
 * @property {Object} [after_state] - Execution-time state snapshot captured after mutation
 * @property {string[]} [labelsAdded] - Labels added by add_labels handler
 * @property {string[]} [labelsSuggested] - Labels suggested but not applied by add_labels handler
 * @property {string} timestamp - ISO 8601 timestamp of creation
 */

/**
 * Recursively redacts secrets from every string leaf of a value.
 *
 * Redaction must happen before JSON.stringify: once a value containing a
 * quote, backslash, or newline is serialized, its JSON-escaped form no
 * longer matches the raw secret value that redactBuiltInPatterns/redactSecrets
 * search for, and the secret would be persisted recoverably in the manifest.
 *
 * @param {unknown} value - Value to sanitize (object, array, string, or primitive)
 * @param {string[]} secretValues - Secret values to redact
 * @returns {unknown} Sanitized value safe to JSON.stringify
 */
function redactManifestValue(value, secretValues) {
  if (typeof value === "string") {
    let redacted = redactBuiltInPatterns(value).content;
    redacted = redactSecrets(redacted, secretValues).content;
    return redacted;
  }
  if (Array.isArray(value)) {
    return value.map(entry => redactManifestValue(entry, secretValues));
  }
  if (value && typeof value === "object") {
    return Object.fromEntries(Object.entries(value).map(([key, nestedValue]) => [key, redactManifestValue(nestedValue, secretValues)]));
  }
  return value;
}

/**
 * Create a manifest logger function for recording executed safe output items.
 *
 * The logger writes JSONL entries to the specified manifest file.
 * It is designed to be easily testable by accepting the file path as a parameter.
 *
 * @param {string} [manifestFile] - Path to the manifest file (defaults to MANIFEST_FILE_PATH)
 * @returns {(item: {type: string, url?: string, number?: number, repo?: string, provider?: string, id?: string|number, identifier?: string, target?: Object, labels?: Array<{name: string, database_id?: number, node_id?: string}>, temporaryId?: string, metadata?: Record<string, any>, before_state?: Object, after_state?: Object, labelsAdded?: string[], labelsSuggested?: string[], labelsBefore?: string[]}) => void} Logger function
 */
function createManifestLogger(manifestFile = MANIFEST_FILE_PATH) {
  // Touch the file immediately so it exists for artifact upload
  // even if no items are created during this run.
  ensureManifestExists(manifestFile);

  /**
   * Log an executed safe output item to the manifest file.
   *
   * @param {{type: string, url?: string, number?: number, repo?: string, provider?: string, id?: string|number, identifier?: string, target?: Object, labels?: Array<{name: string, database_id?: number, node_id?: string}>, temporaryId?: string, metadata?: Record<string, any>, before_state?: Object, after_state?: Object, labelsAdded?: string[], labelsSuggested?: string[], labelsBefore?: string[]}} item - Executed item details
   */
  return function logCreatedItem(item) {
    if (!item) return;

    /** @type {ManifestEntry} */
    const entry = {
      type: item.type,
      ...(item.url ? { url: item.url } : {}),
      ...(item.number != null ? { number: item.number } : {}),
      ...(item.repo ? { repo: item.repo } : {}),
      ...(item.provider ? { provider: item.provider } : {}),
      ...(item.id != null ? { id: item.id } : {}),
      ...(item.identifier ? { identifier: item.identifier } : {}),
      ...(item.target ? { target: item.target } : {}),
      ...(Array.isArray(item.labels) ? { labels: item.labels } : {}),
      ...(item.temporaryId ? { temporaryId: item.temporaryId } : {}),
      ...(item.metadata && Object.keys(item.metadata).length > 0 ? { metadata: item.metadata } : {}),
      ...(item.before_state ? { before_state: item.before_state } : {}),
      ...(item.after_state ? { after_state: item.after_state } : {}),
      ...(Array.isArray(item.labelsAdded) ? { labelsAdded: item.labelsAdded } : {}),
      ...(Array.isArray(item.labelsSuggested) ? { labelsSuggested: item.labelsSuggested } : {}),
      ...(Array.isArray(item.labelsBefore) ? { labelsBefore: item.labelsBefore } : {}),
      timestamp: new Date().toISOString(),
    };

    let jsonLine;
    try {
      const secretValues = collectArtifactSecretValues();
      const redactedEntry = /** @type {ManifestEntry} */ redactManifestValue(entry, secretValues);
      jsonLine = JSON.stringify(redactedEntry);
    } catch (error) {
      if (typeof core !== "undefined" && typeof core.warning === "function") {
        core.warning(`Failed to redact safe-output manifest entry (type=${entry.type}); recording minimal fields only to avoid persisting unredacted data: ${getErrorMessage(error)}`);
      }
      jsonLine = JSON.stringify({
        type: entry.type,
        ...(entry.provider ? { provider: entry.provider } : {}),
        ...(entry.number != null ? { number: entry.number } : {}),
        timestamp: entry.timestamp,
      });
    }
    try {
      fs.appendFileSync(manifestFile, jsonLine + "\n");
    } catch (error) {
      throw new Error(`${ERR_SYSTEM}: Failed to write to manifest file: ${getErrorMessage(error)}`, { cause: error });
    }
  };
}

/**
 * Ensure the manifest file exists, creating an empty file if it does not.
 * This should be called at the end of safe output processing to guarantee
 * the artifact upload always has a file to upload.
 *
 * @param {string} [manifestFile] - Path to the manifest file (defaults to MANIFEST_FILE_PATH)
 */
function ensureManifestExists(manifestFile = MANIFEST_FILE_PATH) {
  if (!fs.existsSync(manifestFile)) {
    try {
      fs.writeFileSync(manifestFile, "");
    } catch (error) {
      throw new Error(`${ERR_SYSTEM}: Failed to create manifest file: ${getErrorMessage(error)}`, { cause: error });
    }
  }
}

/**
 * Extract executed item details from a handler result for manifest logging.
 * Returns null if the type is explicitly excluded (NOT_LOGGED_TYPES) or if the
 * result is from a staged (preview) run where no item was actually modified.
 *
 * All other types — built-in handlers, custom safe job types, and future types —
 * are logged automatically. For creation types (CREATE_ITEM_TYPES), the result
 * URL is included when present. For modification types (e.g. add_labels,
 * close_issue), the URL is optional.
 *
 * @param {string} type - The handler type (e.g., "create_issue")
 * @param {any} result - The handler result object
 * @returns {{type: string, url?: string, number?: number, repo?: string, provider?: string, id?: string|number, identifier?: string, target?: Object, labels?: Array<{name: string, database_id?: number, node_id?: string}>, temporaryId?: string, metadata?: Record<string, any>, before_state?: Object, after_state?: Object, labelsAdded?: string[], labelsSuggested?: string[], labelsBefore?: string[]}|null}
 */
function extractCreatedItemFromResult(type, result) {
  if (!result || NOT_LOGGED_TYPES.has(type)) return null;

  // PR reviews are buffered first and only gain durable identity fields after the
  // final submitReview() call, so skip logging placeholder buffer results here.
  if (type === "submit_pull_request_review" && !result.review_url && !result.pull_request_number && !result.repo) {
    return null;
  }

  // In staged mode (🎭 Staged Mode Preview), no item was actually modified in GitHub — skip logging
  if (result.staged === true || result.skipped === true || result.deferred_manifest === true) return null;

  // Normalize URL from different result shapes (present for creation types)
  const url = result.url || result.projectUrl || result.html_url || result.pull_request_url || result.review_url || result.issue_url;
  const number = result.number ?? result.pull_request_number ?? result.prNumber ?? result.issue_number ?? result.itemNumber;
  const repo = result.repo || result._repo;
  const provider = result.provider || result.metadata?.provider || (type.startsWith("jira_") ? "jira" : type.startsWith("linear_") ? "linear" : type.startsWith("ado_") ? "azure-devops" : "github");
  const id = result.id ?? result.commentId ?? result.comment_id ?? result.review_id ?? result.issue_id;
  const identifier = result.identifier ?? result.issue_key;
  // result.target may be a scalar provider-specific identifier (e.g. Linear issue key "ENG-123")
  // rather than a structured object. Normalize it to a provider-neutral object so downstream
  // consumers (CreatedItemReport.Target, logs schema) always receive an object.
  const rawTarget = result.target;
  const target =
    (rawTarget && typeof rawTarget === "object" ? rawTarget : undefined) ||
    (typeof rawTarget === "string" && rawTarget ? { provider, identifier: rawTarget } : undefined) ||
    (repo || number != null
      ? {
          provider,
          ...(repo ? { repository: repo } : {}),
          ...(number != null ? { number } : {}),
          ...(result.contextType ? { kind: result.contextType } : {}),
        }
      : undefined);
  const labels = Array.isArray(result.labels) ? result.labels : Array.isArray(result.labelsAdded) ? result.labelsAdded.map(name => ({ name })) : undefined;

  return {
    type,
    ...(url ? { url } : {}),
    ...(number != null ? { number } : {}),
    ...(repo ? { repo } : {}),
    provider,
    ...(id != null ? { id } : {}),
    ...(identifier ? { identifier } : {}),
    ...(target ? { target } : {}),
    ...(labels ? { labels } : {}),
    ...(result.temporaryId ? { temporaryId: result.temporaryId } : {}),
    ...(result.metadata && Object.keys(result.metadata).length > 0 ? { metadata: result.metadata } : {}),
    ...(result.before_state ? { before_state: result.before_state } : {}),
    ...(result.after_state ? { after_state: result.after_state } : {}),
    ...(Array.isArray(result.labelsAdded) ? { labelsAdded: result.labelsAdded } : {}),
    ...(Array.isArray(result.labelsSuggested) ? { labelsSuggested: result.labelsSuggested } : {}),
    ...(Array.isArray(result.labelsBefore) ? { labelsBefore: result.labelsBefore } : {}),
  };
}

/**
 * Write the temporary ID map to a JSON file for inclusion in the safe-outputs-items artifact.
 *
 * The file contains a pretty-printed JSON object mapping temporary IDs to their resolved
 * GitHub resource references for review and audit purposes.
 *
 * @param {Object} temporaryIdMap - The temporary ID map object (keys are temp IDs, values are {repo, number})
 * @param {string} [filePath] - Path to the output file (defaults to TEMPORARY_ID_MAP_FILE_PATH)
 */
function writeTemporaryIdMapFile(temporaryIdMap, filePath = TEMPORARY_ID_MAP_FILE_PATH) {
  try {
    const dir = nodePath.dirname(filePath);
    if (!fs.existsSync(dir)) {
      fs.mkdirSync(dir, { recursive: true });
    }
    fs.writeFileSync(filePath, JSON.stringify(temporaryIdMap, null, 2) + "\n");
  } catch (error) {
    throw new Error(`${ERR_SYSTEM}: Failed to write temporary ID map file: ${getErrorMessage(error)}`, { cause: error });
  }
}

/**
 * Write a structured safe-output error report for artifact upload.
 *
 * The report captures *structured* failure metadata only (error code, error
 * message produced by gh-aw itself, and the list of failing safe-output types).
 * Raw handler stdout/stderr is never captured. Built-in credential patterns are
 * redacted from the serialized report before it is written to disk.
 *
 * The file is uploaded with the safe-outputs-items artifact (which is uploaded
 * with `if: always()`), so failures of the "Process Safe Outputs" step remain
 * diagnosable after the job logs expire.
 *
 * @param {{status?: string, errorCode?: string, message?: string, failures?: Array<{type?: string, errorCode?: string, error?: string}>}} report - Failure diagnostics
 * @param {string} [filePath] - Path to the output file (defaults to SAFE_OUTPUT_ERRORS_FILE_PATH)
 */
function writeSafeOutputErrorReport(report, filePath = SAFE_OUTPUT_ERRORS_FILE_PATH) {
  /** @type {Record<string, any>} */
  const entry = {
    timestamp: new Date().toISOString(),
    status: report?.status || "failure",
    ...(report?.errorCode ? { errorCode: report.errorCode } : {}),
    ...(report?.message ? { message: report.message } : {}),
    ...(process.env.GITHUB_WORKFLOW ? { workflow: process.env.GITHUB_WORKFLOW } : {}),
    ...(process.env.GITHUB_RUN_ID ? { run_id: process.env.GITHUB_RUN_ID } : {}),
    failures: Array.isArray(report?.failures)
      ? report.failures.map(f => ({
          ...(f?.type ? { type: f.type } : {}),
          ...(f?.errorCode ? { errorCode: f.errorCode } : {}),
          ...(f?.error ? { error: f.error } : {}),
        }))
      : [],
  };

  let content = JSON.stringify(entry, null, 2) + "\n";
  try {
    content = redactBuiltInPatterns(content).content;
    content = redactSecrets(content, collectArtifactSecretValues()).content;
  } catch {
    // Redaction is a safety net; if it fails, drop the free-form text rather
    // than risk writing an unredacted credential into an artifact.
    content = JSON.stringify({ timestamp: entry.timestamp, status: entry.status, message: "<redaction unavailable: diagnostics omitted>" }, null, 2) + "\n";
  }

  try {
    const dir = nodePath.dirname(filePath);
    if (!fs.existsSync(dir)) {
      fs.mkdirSync(dir, { recursive: true });
    }
    fs.writeFileSync(filePath, content);
  } catch (error) {
    throw new Error(`${ERR_SYSTEM}: Failed to write safe output error report: ${getErrorMessage(error)}`, { cause: error });
  }
}

module.exports = {
  MANIFEST_FILE_PATH,
  TEMPORARY_ID_MAP_FILE_PATH,
  SAFE_OUTPUT_ERRORS_FILE_PATH,
  CREATE_ITEM_TYPES,
  NOT_LOGGED_TYPES,
  createManifestLogger,
  ensureManifestExists,
  extractCreatedItemFromResult,
  writeTemporaryIdMapFile,
  writeSafeOutputErrorReport,
};
