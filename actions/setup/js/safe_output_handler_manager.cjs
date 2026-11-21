// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Safe Output Handler Manager
 *
 * This module manages the dispatch of safe output messages to dedicated handlers.
 * It reads configuration, loads the appropriate handlers for enabled safe output types,
 * and processes messages from the agent output file while maintaining a shared temporary ID map.
 */

const { loadAgentOutput } = require("./load_agent_output.cjs");
const { normalizeSafeOutputConfig } = require("./safe_output_names.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { isRateLimitError } = require("./error_recovery.cjs");
const { getStalePullRequestReason } = require("./pr_lifecycle.cjs");
const { ERR_CONFIG, ERR_PARSE, ERR_SYSTEM, ERR_VALIDATION, SAFE_OUTPUT_E007, SAFE_OUTPUT_E099 } = require("./error_codes.cjs");
const { classifySafeOutputResult, computeSafeOutputsStatus, isFailedProcessingResult } = require("./safe_outputs_status.cjs");
const { buildSafeOutputDependencyPlan, checkSafeOutputDependencies } = require("./safe_output_dependencies.cjs");
const { projectSafeOutputData, finalizeBufferedReviewExecutions, withSafeOutputInvocation, currentSafeOutputInvocation, projectAuthorization } = require("./safe_output_execution.cjs");
const {
  hasUnresolvedTemporaryIds,
  replaceTemporaryIdReferences,
  replaceArtifactUrlReferences,
  normalizeTemporaryId,
  extractTemporaryIdReferences,
  getCreatedTemporaryId,
  isTemporaryId,
  resolveIssueNumber,
  resolveNumberFromTemporaryId,
} = require("./temporary_id.cjs");
const { generateMissingInfoSections } = require("./missing_info_formatter.cjs");
const { setCollectedMissings } = require("./missing_messages_helper.cjs");
const { writeSafeOutputSummaries } = require("./safe_output_summary.cjs");
const { getAssignToAgentAssigned, getAssignToAgentErrors, getAssignToAgentErrorCount, writeAssignToAgentSummary } = require("./assign_to_agent.cjs");
const { createPrReviewBufferRegistry } = require("./pr_review_buffer.cjs");
const { appendExecutionProvenance, stripExecutionProvenance } = require("./execution_provenance.cjs");
const { COMMENT_PREAMBLE } = require("./issue_body.cjs");
const { parseReportMarkdown, transformMarkdownLinkDestinations } = require("./report_markdown.cjs");
const { isTemporaryIdLink } = require("./report_ast.cjs");
const { renderSizedReport } = require("./report_size.cjs");
const { sanitizeContent } = require("./sanitize_content.cjs");
const { buildWorkflowRunUrl } = require("./workflow_metadata_helpers.cjs");
const { parseIntTemplatable } = require("./templatable.cjs");
const { resolveAllowedMentionsFromPayload } = require("./resolve_mentions_from_payload.cjs");
const { getDefaultTargetRepo, parseRepoSlug, isExactRepoSlug, resolveTargetRepoConfig, resolveAndValidateRepo } = require("./repo_helpers.cjs");
const {
  createManifestLogger,
  ensureManifestExists,
  extractCreatedItemFromResult,
  writeTemporaryIdMapFile,
  writeSafeOutputErrorReport,
  writeSafeOutputExecutions,
  writeMergePullRequestDecision,
  SAFE_OUTPUT_ERRORS_FILE_PATH,
} = require("./safe_output_manifest.cjs");
const { loadCustomSafeOutputJobTypes, loadCustomSafeOutputScriptHandlers, loadCustomSafeOutputActionHandlers, isStagedMode, resolveTarget } = require("./safe_output_helpers.cjs");
const { emitSafeOutputActionOutputs } = require("./safe_outputs_action_outputs.cjs");
const { listCommentMemoryFiles, COMMENT_MEMORY_DIR } = require("./comment_memory_helpers.cjs");
const { checkRateLimitHeadroom } = require("./rate_limit_helpers.cjs");
const { redactSensitiveConfig } = require("./safe_outputs_config_redact.cjs");
const { getEffectiveDetectionConclusion, getDetectionReasonText } = require("./threat_detection_warning.cjs");
const nodePath = require("path");
const fs = require("fs");
const {
  readClaimScopeContext,
  normalizeClaimScope,
  normalizeRuntimeMessage,
  assertClaimAuthorized,
  withClaimExecution,
  withClaimOutputType,
  currentClaimHandle,
  currentClaimAssignment,
  claimIdentity,
  assertClaimIdentity,
} = require("./work_queue_claim_scope.cjs");
const { appendClaimEffects, withClaimEffectClients } = require("./work_queue_effect_client.cjs");
const { normalizeSafeOutputName } = require("./safe_output_names.cjs");
const { createClaimAdapterHandler, preparedAdapterPath } = require("./work_queue_claim_adapters.cjs");
// Output types that never write resources; allowed for a Work whose effect_contract sets no_writes.
const NO_WRITE_TYPES = new Set(["noop", "report_missing_tool", "report_missing_data", "missing_tool", "missing_data", "report_incomplete"]);
const GITHUB_TOKEN_CONFIG_KEY = "github-token";

/**
 * Handler map configuration
 * Maps safe output types to their handler module file paths
 */
const HANDLER_MAP = {
  add_security_advisory_comment: "./add_security_advisory_comment.cjs",
  create_security_advisory: "./create_security_advisory.cjs",
  create_issue: "./create_issue.cjs",
  add_comment: "./add_comment.cjs",
  comment_memory: "./comment_memory.cjs",
  create_discussion: "./create_discussion.cjs",
  close_issue: "./close_issue.cjs",
  close_discussion: "./close_discussion.cjs",
  add_labels: "./add_labels.cjs",
  remove_labels: "./remove_labels.cjs",
  replace_label: "./replace_label.cjs",
  update_issue: "./update_issue.cjs",
  update_discussion: "./update_discussion.cjs",
  link_sub_issue: "./link_sub_issue.cjs",
  update_release: "./update_release.cjs",
  create_pull_request_review_comment: "./create_pr_review_comment.cjs",
  submit_pull_request_review: "./submit_pr_review.cjs",
  dismiss_pull_request_review: "./dismiss_pull_request_review.cjs",
  reply_to_pull_request_review_comment: "./reply_to_pr_review_comment.cjs",
  resolve_pull_request_review_thread: "./resolve_pr_review_thread.cjs",
  create_pull_request: "./create_pull_request.cjs",
  push_to_pull_request_branch: "./push_to_pull_request_branch.cjs",
  update_pull_request: "./update_pull_request.cjs",
  merge_pull_request: "./merge_pull_request.cjs",
  close_pull_request: "./close_pull_request.cjs",
  mark_pull_request_as_ready_for_review: "./mark_pull_request_as_ready_for_review.cjs",
  approve_workflow_run: "./approve_workflow_run.cjs",
  hide_comment: "./hide_comment.cjs",
  set_issue_type: "./set_issue_type.cjs",
  set_issue_field: "./set_issue_field.cjs",
  add_reviewer: "./add_reviewer.cjs",
  assign_milestone: "./assign_milestone.cjs",
  assign_to_user: "./assign_to_user.cjs",
  unassign_from_user: "./unassign_from_user.cjs",
  assign_to_agent: "./assign_to_agent.cjs",
  create_agent_session: "./create_agent_session.cjs",
  create_code_scanning_alert: "./create_code_scanning_alert.cjs",
  autofix_code_scanning_alert: "./autofix_code_scanning_alert.cjs",
  create_check_run: "./create_check_run.cjs",
  dispatch_workflow: "./dispatch_workflow.cjs",
  dispatch_repository: "./dispatch_repository.cjs",
  call_workflow: "./call_workflow.cjs",
  create_missing_tool_issue: "./create_missing_tool_issue.cjs",
  report_missing_tool: "./missing_tool.cjs",
  create_missing_data_issue: "./create_missing_data_issue.cjs",
  report_missing_data: "./missing_data.cjs",
  ledger_append: "./ledger_append.cjs",
  ledger_request_compaction: "./ledger_request_compaction.cjs",
  ledger_mutation: "./ledger_mutation.cjs",
  noop: "./noop_handler.cjs",
  report_incomplete: "./report_incomplete_handler.cjs",
  request_continuation: "./request_continuation_handler.cjs",
  create_report_incomplete_issue: "./create_report_incomplete_issue.cjs",
  create_project: "./create_project.cjs",
  create_project_status_update: "./create_project_status_update.cjs",
  update_project: "./update_project.cjs",
  upload_artifact: "./upload_artifact.cjs",
  upload_asset: "./work_queue_upload_assets.cjs",
  upload_code_coverage: "./upload_code_coverage.cjs",
};

/**
 * Message types handled by standalone steps (not through the handler manager)
 * These types should not trigger warnings when skipped by the handler manager
 *
 * Standalone types: upload_asset, noop, request_continuation
 *   - Have dedicated processing steps with specialized logic
 */
const STANDALONE_STEP_TYPES = new Set(["upload_asset", "noop", "request_continuation"]);

/**
 * Code-push safe output types whose failures are tracked for later comment annotations
 * and conclusion reporting. Remaining messages are still processed after a failure.
 */
const CODE_PUSH_TYPES = new Set(["push_to_pull_request_branch", "create_pull_request"]);

/** @type {Set<string>} Project-safe-output handlers that should default to GH_AW_PROJECT_GITHUB_TOKEN when no per-handler github-token is configured. */
const PROJECT_HANDLER_TYPES = new Set(["create_project", "create_project_status_update", "update_project"]);
const MENTION_HANDLER_TYPES = new Set(["add_comment", "close_discussion", "create_discussion", "create_issue", "create_pull_request", "create_pull_request_review_comment", "reply_to_pull_request_review_comment"]);
/** @type {Record<string, string>} */
const TARGET_SELECTOR_KINDS = {
  add_comment: "issue_or_pull_request",
  update_issue: "issue",
  close_issue: "issue",
  update_pull_request: "pull_request",
  merge_pull_request: "pull_request",
  close_pull_request: "pull_request",
  submit_pull_request_review: "pull_request",
};
const REPOSITORY_SELECTOR_TYPES = new Set([
  ...Object.keys(TARGET_SELECTOR_KINDS),
  "add_labels",
  "remove_labels",
  "replace_label",
  "create_issue",
  "create_pull_request",
  "create_discussion",
  "update_discussion",
  "close_discussion",
  "comment_memory",
  "create_security_advisory",
  "add_security_advisory_comment",
  "update_release",
  "create_check_run",
  "create_code_scanning_alert",
  "autofix_code_scanning_alert",
  "create_missing_tool_issue",
  "create_missing_data_issue",
  "create_report_incomplete_issue",
]);
const TEMPORARY_TARGET_FIELDS_BY_TYPE = {
  add_comment: ["item_number", "issue_number", "pr-number"],
  add_labels: ["item_number", "issue_number", "pr_number", "pull_number"],
  assign_milestone: ["issue_number"],
  close_issue: ["issue_number"],
  close_pull_request: ["pull_request_number"],
  merge_pull_request: ["pull_request_number"],
  remove_labels: ["item_number", "issue_number", "pr_number", "pull_number"],
  replace_label: ["item_number", "issue_number", "pr_number", "pull_number"],
  set_issue_field: ["issue_number"],
  set_issue_type: ["issue_number"],
  submit_pull_request_review: ["pull_request_number"],
  update_issue: ["issue_number"],
  update_pull_request: ["pull_request_number"],
};
const TEMPORARY_TARGET_FIELDS = new Set(["item_number", "issue_number", "pr_number", "pull_number", "pull_request_number", "pr-number", "parent_issue_number", "sub_issue_number", "discussion_number", "content_number"]);
const NATIVE_TEMPORARY_TARGET_TYPES = new Set([...Object.keys(TEMPORARY_TARGET_FIELDS_BY_TYPE), "link_sub_issue", "update_discussion", "close_discussion", "update_project", "assign_to_agent"]);
const SELECTOR_TARGET_TYPES = new Set([...Object.keys(TARGET_SELECTOR_KINDS), "add_labels", "remove_labels", "replace_label", "assign_milestone", "assign_to_agent"]);
/** @typedef {"issue" | "strict" | "manager-number" | "draft"} TemporaryTargetResolver */
/** @typedef {{field: string, resolver: TemporaryTargetResolver, titleFallback?: boolean, normalize?: "number"}} EffectiveTemporaryTarget */
/** @typedef {{wasTemporaryId: boolean, resolved: {repo: string, number: number} | number | boolean | null}} EffectiveTemporaryTargetResolution */

// Threat-detection warn-mode requirement IDs from safe-outputs specification:
// - WTD2: Convertible outputs must be mapped to a reviewable type.
// - WTD3: Non-reviewable outputs must be aborted.
const WTD2_REQUIREMENT_ID = "WTD2";
const WTD3_REQUIREMENT_ID = "WTD3";

/**
 * Safe output types that remain reviewable in threat-detection warn mode.
 * Reviewable means the handler creates visible artifacts (issues, comments, pull requests, review items)
 * that humans can inspect before any follow-up automation or merge decision.
 * If a new safe output type is added:
 * - place it here when it follows that same review-first model;
 * - place it in THREAT_WARNING_CONVERTIBLE_TYPES when it must be remapped to a reviewable type;
 * - place it in THREAT_WARNING_ABORT_TYPES when it performs non-reviewable mutation.
 * @type {Set<string>}
 */
const THREAT_WARNING_REVIEWABLE_TYPES = new Set([
  "add_security_advisory_comment",
  "create_security_advisory",
  "create_issue",
  "add_comment",
  "create_pull_request",
  "comment_memory",
  "update_issue",
  "create_discussion",
  "update_discussion",
  "update_pull_request",
  "create_pull_request_review_comment",
  "submit_pull_request_review",
  "reply_to_pull_request_review_comment",
  "create_project_status_update",
  "update_release",
  "create_code_scanning_alert",
  "create_check_run",
  "create_missing_tool_issue",
  "report_missing_tool",
  "create_missing_data_issue",
  "report_missing_data",
  // Log-only: the ledger append already happened in the agent job, and the handler
  // only echoes redacted audit metadata, so it stays reviewable in warn mode.
  "ledger_mutation",
  "create_report_incomplete_issue",
  "report_incomplete",
  "request_continuation",
]);

/**
 * Safe output types that require conversion to a reviewable type in warn mode.
 * Kept as a Map (instead of a single constant) because multiple convertible mappings
 * may be added over time as safe output types evolve.
 * @type {Map<string, string>}
 */
const THREAT_WARNING_CONVERTIBLE_TYPES = new Map([["push_to_pull_request_branch", "create_pull_request"]]);

/**
 * Safe output types that must be aborted in threat-detection warn mode.
 * These handlers perform non-reviewable state-changing operations (merge/close/assign/dispatch/etc.)
 * that cannot be safely inspected before execution and are often irreversible after execution.
 * If a new safe output type performs direct state mutation without a review artifact, classify it here.
 * @type {Set<string>}
 */
const THREAT_WARNING_ABORT_TYPES = new Set([
  "noop",
  "close_issue",
  "link_sub_issue",
  "close_discussion",
  "close_pull_request",
  "merge_pull_request",
  "mark_pull_request_as_ready_for_review",
  "approve_workflow_run",
  "resolve_pull_request_review_thread",
  "dismiss_pull_request_review",
  "add_labels",
  "remove_labels",
  "replace_label",
  "add_reviewer",
  "assign_milestone",
  "assign_to_agent",
  "assign_to_user",
  "unassign_from_user",
  "hide_comment",
  "set_issue_type",
  "set_issue_field",
  "create_project",
  "update_project",
  "upload_asset",
  "upload_artifact",
  "upload_code_coverage",
  "dispatch_workflow",
  "dispatch_repository",
  "ledger_request_compaction",
  "call_workflow",
  "autofix_code_scanning_alert",
  "create_agent_session",
]);

/**
 * Resolve threat warning policy for a safe output type.
 * @param {string} messageType
 * @param {Map<string, Function>} messageHandlers
 * @returns {{policy: "reviewable" | "convertible" | "abort" | "none", mappedType?: string}}
 */
function getThreatWarningPolicy(messageType, messageHandlers) {
  if (THREAT_WARNING_ABORT_TYPES.has(messageType) || handlerThreatWarningAbortTypes.get(messageHandlers)?.has(messageType)) {
    return { policy: "abort" };
  }
  const mappedType = THREAT_WARNING_CONVERTIBLE_TYPES.get(messageType);
  if (mappedType) {
    return { policy: "convertible", mappedType };
  }
  if (THREAT_WARNING_REVIEWABLE_TYPES.has(messageType)) {
    return { policy: "reviewable" };
  }
  // Unknown types return "none". In warning mode this is currently allow-with-warning
  // to preserve backward compatibility for custom/extension handlers, but new built-in
  // safe output types should be explicitly classified in one of the policy sets above.
  return { policy: "none" };
}

function buildCommentMemoryMessagesFromFiles(existingMessages, config) {
  if (!config.comment_memory) {
    return [];
  }

  const fallbackMemoryId = normalizeCommentMemoryId(config?.comment_memory?.memory_id, "default");
  const existingMemoryIds = new Set(existingMessages.filter(isCommentMemoryMessage).map(message => normalizeCommentMemoryId(message.memory_id, fallbackMemoryId)));

  const fileEntries = listCommentMemoryFiles(COMMENT_MEMORY_DIR);
  if (fileEntries.length === 0) {
    return [];
  }

  const messages = [];
  for (const entry of fileEntries) {
    if (existingMemoryIds.has(entry.memoryId)) {
      continue;
    }
    let body = "";
    try {
      body = fs.readFileSync(entry.filePath, "utf8").replace(/\n+$/, "");
    } catch (error) {
      core.warning(`Failed to read comment-memory file '${entry.filePath}': ${getErrorMessage(error)}`);
      continue;
    }
    messages.push({
      type: "comment_memory",
      memory_id: entry.memoryId,
      body,
    });
  }

  if (messages.length > 0) {
    core.info(`Loaded ${messages.length} comment_memory message(s) from ${COMMENT_MEMORY_DIR}`);
  }
  return messages;
}

function isCommentMemoryMessage(message) {
  // memory_id normalization/validation is handled separately in normalizeCommentMemoryId.
  return message?.type === "comment_memory";
}

function normalizeCommentMemoryId(memoryId, fallback = "default") {
  if (typeof memoryId !== "string") {
    return fallback;
  }
  const normalized = memoryId.trim();
  return normalized.length > 0 ? normalized : fallback;
}

/**
 * Load configuration for safe outputs
 * Reads configuration from GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG environment variable
 * @returns {Object} Safe outputs configuration
 */
function loadConfig() {
  if (!process.env.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG) {
    throw new Error(`${ERR_CONFIG}: GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG environment variable is required but not set`);
  }

  try {
    const config = JSON.parse(process.env.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG);
    core.info(`Loaded config from GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG: ${JSON.stringify(redactSensitiveConfig(config))}`);
    // Normalize config keys: convert hyphens to underscores
    return normalizeSafeOutputConfig(config);
  } catch (error) {
    throw new Error(`${ERR_PARSE}: Failed to parse GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG: ${getErrorMessage(error)}`, { cause: error });
  }
}

/** @type {Set<string>} Handler types that participate in the PR review buffer */
const PR_REVIEW_HANDLER_TYPES = new Set(["create_pull_request_review_comment", "submit_pull_request_review"]);

/**
 * @type {WeakMap<Map<string, Function>, Map<string, string>>} Records why configured handlers failed to
 * load, keyed by the handler map returned from loadHandlers(). The reasons are surfaced when a message
 * is skipped because no handler was loaded. Scoping by handler map keeps concurrent loadHandlers()
 * invocations isolated from each other.
 */
const handlerLoadErrorsByHandlerMap = new WeakMap();

/** @type {WeakMap<Map<string, Function>, Set<string>>} Compiler-owned warning policies, never message payloads. */
const handlerThreatWarningAbortTypes = new WeakMap();

/** @type {WeakMap<Map<string, Function>, Map<string, {max: number, attempted: number}>>} Trusted connector proposal budgets, scoped to a handler factory. */
const connectorBudgetsByHandlerMap = new WeakMap();

/** @type {WeakSet<Function>} Only built-in local diagnostics may bypass publication checks. */
const localDiagnosticHandlers = new WeakSet();
const handlerConfigurations = new WeakMap();
const authorizationBatchContexts = new WeakMap();

function trustedDescriptor(config, type, /** @type {any} */ message = null) {
  const descriptor = config[type] || {};
  const tool = type === "dispatch_repository" && message && descriptor.tools && Object.hasOwn(descriptor.tools, message.tool_name) ? descriptor.tools[message.tool_name] : null;
  return tool ? { ...descriptor, ...tool, tools: descriptor.tools, _authorizationName: message.tool_name } : descriptor;
}

function effectiveAuthorization(config, type, /** @type {any} */ message = null) {
  const descriptor = trustedDescriptor(config, type, message);
  if (descriptor.authorization) return descriptor.authorization;
  return descriptor.builtin === true ? undefined : config.authorization;
}

function hasAuthorization(config) {
  return !!config.authorization || Object.values(config).some(value => value && typeof value === "object" && (value.authorization || (value.tools && Object.values(value.tools).some(tool => tool?.authorization))));
}

function authorizationOperationStatus(result, fallback) {
  return ["partial", "indeterminate"].includes(result?.operation_outcome) ? result.operation_outcome : fallback;
}

function freezeInvocationValue(value) {
  if (!value || typeof value !== "object") return value;
  for (const nested of Object.values(value)) freezeInvocationValue(nested);
  return Object.freeze(value);
}

/**
 * Projects a value to its plain JSON data so it is cloneable and digestable even when
 * provider entries carry methods or Dates: toJSON() is applied, non-finite numbers become
 * null, and undefined/function/symbol values are dropped from objects (null in arrays).
 * @param {any} value
 * @param {Set<object>} [ancestors]
 * @returns {any}
 */
function projectInvocationJsonValue(value, ancestors = new Set()) {
  if (value && typeof value.toJSON === "function") value = value.toJSON();
  if (typeof value === "number") return Number.isFinite(value) ? value : null;
  if (value === null || typeof value !== "object") return value;
  if (ancestors.has(value)) throw new TypeError("Safe-output invocation inputs must not be circular");
  ancestors.add(value);
  const omitted = item => item === undefined || typeof item === "function" || typeof item === "symbol";
  /** @type {any} */
  let projected;
  if (Array.isArray(value)) projected = value.map(item => (omitted(item) ? null : projectInvocationJsonValue(item, ancestors)));
  else {
    projected = {};
    for (const [name, item] of Object.entries(value)) if (!omitted(item)) projected[name] = projectInvocationJsonValue(item, ancestors);
  }
  ancestors.delete(value);
  return projected;
}

/**
 * Temporary-ID map passed to protected executors. Reads come from the bound
 * snapshot; writes also publish newly created IDs to the shared map.
 */
class BoundTemporaryIdMap extends Map {
  /**
   * @param {Record<string, any>} snapshot
   * @param {Map<string, any>} shared
   */
  constructor(snapshot, shared) {
    super();
    for (const [tempId, resolved] of Object.entries(snapshot || {})) super.set(normalizeTemporaryId(tempId), resolved);
    this.shared = shared;
  }

  set(key, value) {
    this.shared.set(key, value);
    return super.set(key, value);
  }

  delete(key) {
    this.shared.delete(key);
    return super.delete(key);
  }
}

function protectReviewLocalLinkDestinations(value, reviewLocalTemporaryIds) {
  const placeholders = new Map();
  let placeholderIndex = 0;
  const protectedValue = transformMarkdownLinkDestinations(value, destination => {
    if (!isTemporaryIdLink(destination) || !reviewLocalTemporaryIds.has(normalizeTemporaryId(destination))) return destination;
    let placeholder;
    do {
      placeholder = `gh_aw_review_link_${placeholderIndex++}`;
    } while (value.includes(placeholder) || placeholders.has(placeholder));
    placeholders.set(placeholder, destination);
    return placeholder;
  });
  return {
    value: protectedValue,
    restore(text) {
      for (const [placeholder, destination] of placeholders) text = text.split(placeholder).join(destination);
      return text;
    },
  };
}

function normalizeInvocationInputs(value, temporaryIdMap, repository, key = "", preservedTargetFields = new Set(), numericTargetValues = new Map(), preservedReviewTemporaryIds = new Set()) {
  if (typeof value === "string") {
    if (numericTargetValues.has(key)) return numericTargetValues.get(key);
    if (preservedTargetFields.has(key)) return value;
    if (/(?:number|_id)$/.test(key) && !["temporary_id", "output_id", "claim_id", "work_id"].includes(key) && /^#?aw_[A-Za-z0-9_]+$/.test(value)) {
      const target = temporaryIdMap.get(normalizeTemporaryId(value));
      if (target?.number != null) return target.number;
    }
    const protectedLinks = key === "body" && preservedReviewTemporaryIds.size > 0 ? protectReviewLocalLinkDestinations(value, preservedReviewTemporaryIds) : null;
    const normalized = replaceTemporaryIdReferences(protectedLinks?.value ?? value, temporaryIdMap, repository);
    return protectedLinks ? protectedLinks.restore(normalized) : normalized;
  }
  if (!value || typeof value !== "object") return value;
  const result = Array.isArray(value)
    ? value.map(item => normalizeInvocationInputs(item, temporaryIdMap, repository, "", preservedTargetFields, numericTargetValues, preservedReviewTemporaryIds))
    : Object.fromEntries(Reflect.ownKeys(value).map(name => [name, normalizeInvocationInputs(value[name], temporaryIdMap, repository, String(name), preservedTargetFields, numericTargetValues, preservedReviewTemporaryIds)]));
  return Object.freeze(result);
}

/** @returns {EffectiveTemporaryTarget[]} */
function getEffectiveTemporaryTargets(type, message, config) {
  const configuredTarget = config.target || "triggering";
  if (type === "update_project") {
    const contentType = message.content_type === undefined ? message.contentType : message.content_type;
    if (contentType === "draft_issue") {
      const field = message.draft_issue_id === undefined ? "draftIssueId" : "draft_issue_id";
      const title = message.draft_title === undefined ? message.draftTitle : message.draft_title;
      return message[field] == null ? [] : [{ field, resolver: "draft", titleFallback: typeof title === "string" && title.trim() !== "" }];
    }

    const contentNumber = message.content_number === undefined ? message.contentNumber : message.content_number;
    const hasValue = value => value !== undefined && value !== null && String(value).trim() !== "";
    const field = hasValue(contentNumber) ? (message.content_number === undefined ? "contentNumber" : "content_number") : hasValue(message.issue) ? "issue" : hasValue(message.pull_request) ? "pull_request" : null;
    return field ? [{ field, resolver: "issue" }] : [];
  }

  if (type === "update_discussion" || type === "close_discussion") {
    return message.discussion_number == null ? [] : [{ field: "discussion_number", resolver: "strict" }];
  }

  if (type === "link_sub_issue") {
    /** @type {EffectiveTemporaryTarget[]} */
    const targets = [];
    if (configuredTarget === "*" && message.parent_issue_number != null) targets.push({ field: "parent_issue_number", resolver: "issue" });
    if (message.sub_issue_number != null) targets.push({ field: "sub_issue_number", resolver: "issue" });
    return targets;
  }

  if (type === "assign_to_agent") {
    if (configuredTarget !== "*" || (message.issue_number != null && message.pull_number != null)) return [];
    if (message.issue_number != null) return [{ field: "issue_number", resolver: "issue" }];
    return message.pull_number == null ? [] : [{ field: "pull_number", resolver: "manager-number" }];
  }

  const aliases = TEMPORARY_TARGET_FIELDS_BY_TYPE[type];
  if (!aliases) {
    if (NATIVE_TEMPORARY_TARGET_TYPES.has(type)) return [];
    return Object.entries(message)
      .filter(([field, value]) => typeof value === "string" && TEMPORARY_TARGET_FIELDS.has(field))
      .map(([field]) => ({ field, resolver: field === "discussion_number" ? "strict" : "issue", normalize: "number" }));
  }
  if (SELECTOR_TARGET_TYPES.has(type) && configuredTarget !== "*") return [];
  const field = aliases.find(alias => message[alias] !== undefined && message[alias] !== null);
  if (!field) return [];
  return [{ field, resolver: type === "merge_pull_request" ? "strict" : ["close_pull_request", "submit_pull_request_review"].includes(type) ? "manager-number" : "issue" }];
}

/** @returns {EffectiveTemporaryTargetResolution} */
function resolveEffectiveTemporaryTarget(target, message, temporaryIdMap, resolvedTemporaryIds) {
  const value = message[target.field];
  if (target.resolver === "draft") {
    const temporary = typeof value === "string" && isTemporaryId(value.trim());
    const mapped = temporary && temporaryIdMap.get(normalizeTemporaryId(value.trim()));
    return { wasTemporaryId: temporary, resolved: !!mapped?.draftItemId };
  }
  if (target.resolver === "strict" || target.resolver === "manager-number") {
    return resolveNumberFromTemporaryId(value, resolvedTemporaryIds);
  }
  return resolveIssueNumber(value, temporaryIdMap);
}

/**
 * Register only trusted loaders here. Author modules and factories are evaluated
 * by the invocation boundary, never while discovering protected output types.
 */
async function loadHandlers(config, prReviewBufferRegistry, resolvedAllowedMentionAliases, activationCommentLinks = []) {
  config = normalizeSafeOutputConfig(config);
  if (hasAuthorization(config)) {
    config = JSON.parse(JSON.stringify(config));
    for (const type of REPOSITORY_SELECTOR_TYPES) {
      if (!config[type] || typeof config[type] !== "object") continue;
      const defaultRepository = getDefaultTargetRepo(config[type]);
      if (isExactRepoSlug(parseRepoSlug(defaultRepository))) config[type]["target-repo"] = defaultRepository;
    }
    freezeInvocationValue(config);
  }
  const scripts = loadCustomSafeOutputScriptHandlers();
  const actions = loadCustomSafeOutputActionHandlers();
  const lazy = hasAuthorization(config) || isStagedMode() || [...scripts.keys()].some(type => isStagedMode(config[type]));
  if (!lazy) {
    const handlers = await initializeHandlers(config, prReviewBufferRegistry, resolvedAllowedMentionAliases, activationCommentLinks);
    handlerConfigurations.set(handlers, config);
    return handlers;
  }
  const handlers = new Map();
  const errors = new Map();
  handlerConfigurations.set(handlers, config);
  handlerLoadErrorsByHandlerMap.set(handlers, errors);
  handlerThreatWarningAbortTypes.set(handlers, new Set(Object.keys(config).filter(type => config[type]?.threat_warning_policy === "abort")));
  const budgets = new Map();
  for (const [type, options] of Object.entries(config)) {
    if (options?.connector_operation !== true) continue;
    if (!Number.isSafeInteger(options.max) || options.max < 1 || options.max > 100) throw new Error(`Invalid trusted connector budget for "${type}": max must be an integer from 1 to 100`);
    budgets.set(type, { max: options.max, attempted: 0 });
  }
  connectorBudgetsByHandlerMap.set(handlers, budgets);
  for (const type of new Set([...Object.keys(HANDLER_MAP).filter(type => config[type]), ...scripts.keys(), ...actions.keys(), ...Object.keys(config.claim_adapters || {})])) {
    if (type === "upload_asset" && !currentClaimHandle()) continue;
    let executable;
    /** @type {((...args: any[]) => Promise<any>) & {getReviewTarget?: (message: any) => any}} */
    const handler = async (...args) => {
      const requirement = effectiveAuthorization(config, type, args[0]);
      if (requirement) {
        const state = currentSafeOutputInvocation();
        const deferred =
          actions.has(type) || ["upload_artifact", "call_workflow", "upload_code_coverage"].includes(type) || (!!currentClaimHandle() && (type === "upload_asset" || ["prepared", "script"].includes(config.claim_adapters?.[type]?.mode)));
        if (!state || state.requirement !== requirement || state.invocation?.output.type !== type || (state.authorization?.state !== "allowed" && !(deferred && state.authorization?.state === "not_requested"))) {
          throw new Error("Protected safe-output handlers require their trusted prepared invocation context");
        }
      }
      if (!executable) {
        const initialized = await initializeHandlers(config, prReviewBufferRegistry, resolvedAllowedMentionAliases, activationCommentLinks, type);
        executable = initialized.get(type);
        if (!executable) throw new Error(`Handler initialization failed for "${type}"`);
        Object.assign(handler, executable);
      }
      return executable(...args);
    };
    if (type === "submit_pull_request_review") {
      handler.getReviewTarget = message => {
        const target = resolveTarget({ targetConfig: config[type].target || "triggering", item: message, context, itemType: "PR review", supportsPR: false, supportsIssue: false });
        const { defaultTargetRepo, allowedRepos } = resolveTargetRepoConfig(config[type]);
        const repository = resolveAndValidateRepo(message, defaultTargetRepo, allowedRepos, "PR review");
        return target.success && target.number && repository.success ? { repo: repository.repo, pull_request_number: target.number } : null;
      };
    }
    if (config[type]?.builtin === true) localDiagnosticHandlers.add(handler);
    handlers.set(type, handler);
  }
  return handlers;
}

/**
 * Wrap a handler so project-safe-output execution can temporarily bind global.github
 * to the handler-specific authenticated client.
 *
 * @param {string} type - Safe-output handler type
 * @param {Function} messageHandler - Loaded handler function
 * @param {Object|null} handlerGithubClient - Optional per-handler GitHub client
 * @returns {Function} Wrapped handler function
 */
function wrapWithClientRebinding(type, messageHandler, handlerGithubClient) {
  if (!PROJECT_HANDLER_TYPES.has(type) || !handlerGithubClient) {
    return messageHandler;
  }
  return async (...args) => {
    /** @type {any} */
    const globalState = global;
    const hadGithub = Object.prototype.hasOwnProperty.call(globalState, "github");
    const previousGithub = globalState.github;
    globalState.github = handlerGithubClient;
    try {
      return await messageHandler(...args);
    } finally {
      if (hadGithub) {
        globalState.github = previousGithub;
      } else {
        delete globalState.github;
      }
    }
  };
}

async function finalizeLedgerAppend(config, messageHandlers) {
  let ledgerAppendHandler = messageHandlers.get("ledger_append");
  if (!ledgerAppendHandler && config.ledger_append) {
    if (effectiveAuthorization(config, "ledger_append") || isStagedMode(config.ledger_append) || isStagedMode(config)) return;
    const ledgerAppendModule = require(HANDLER_MAP.ledger_append);
    ledgerAppendHandler = await ledgerAppendModule.main(config.ledger_append);
  }
  if (ledgerAppendHandler && "finalize" in ledgerAppendHandler && typeof ledgerAppendHandler.finalize === "function") {
    if (currentClaimHandle()) {
      await assertClaimAuthorized({ type: "ledger_append", claim_handle: currentClaimHandle(), repo: getDefaultTargetRepo(config.ledger_append) });
    }
    ledgerAppendHandler.finalize();
  }
}

function bindHandlerToFactoryScope(handler) {
  const handle = currentClaimHandle();
  const identity = handle ? claimIdentity(handle) : null;
  const assertScope = () => {
    if (currentClaimHandle() !== handle) throw new Error("Handler state cannot escape its trusted Claim factory context");
    if (identity) assertClaimIdentity(identity);
  };
  const bound = (...args) => {
    assertScope();
    return handler(...args);
  };
  for (const [name, value] of Object.entries(handler)) {
    bound[name] =
      typeof value === "function"
        ? (...args) => {
            assertScope();
            return Reflect.apply(value, handler, args);
          }
        : value;
  }
  return bound;
}

/**
 * Load and initialize handlers for enabled safe output types
 * Calls each handler's factory function (main) to get message processors
 * @param {Object} config - Safe outputs configuration
 * @param {Object} prReviewBufferRegistry - PR review buffer registry instance
 * @param {string[]} [resolvedAllowedMentionAliases] - Pre-resolved mention aliases shared across handlers
 * @param {string[]} [activationCommentLinks] - Link accumulator shared across this invocation's handlers
 * @returns {Promise<Map<string, Function>>} Map of type to message handler function
 */
async function initializeHandlers(config, prReviewBufferRegistry, resolvedAllowedMentionAliases, activationCommentLinks = [], /** @type {string|null} */ onlyType = null) {
  config = normalizeSafeOutputConfig(config);
  const messageHandlers = new Map();
  handlerThreatWarningAbortTypes.set(messageHandlers, new Set(Object.keys(config).filter(type => config[type]?.threat_warning_policy === "abort")));
  const connectorBudgets = new Map();
  for (const [type, options] of Object.entries(config)) {
    if (options?.connector_operation !== true) continue;
    if (!Number.isSafeInteger(options.max) || options.max < 1 || options.max > 100) {
      throw new Error(`Invalid trusted connector budget for "${type}": max must be an integer from 1 to 100`);
    }
    connectorBudgets.set(type, { max: options.max, attempted: 0 });
  }
  connectorBudgetsByHandlerMap.set(messageHandlers, connectorBudgets);

  /** @type {Map<string, string>} */
  const handlerLoadErrors = new Map();
  handlerLoadErrorsByHandlerMap.set(messageHandlers, handlerLoadErrors);

  core.info("Loading and initializing safe output handlers based on configuration...");

  for (const [type, handlerPath] of Object.entries(HANDLER_MAP)) {
    if (onlyType && type !== onlyType) continue;
    // Check if this safe output type is enabled in the config
    // The presence of the config key indicates the handler should be loaded
    if (config[type]) {
      if (type === "upload_asset" && !currentClaimHandle()) continue;
      if (currentClaimHandle() && ["prepared", "script"].includes(config.claim_adapters?.[type]?.mode)) continue;
      try {
        const effectivePath = currentClaimHandle() && type === "create_code_scanning_alert" ? "./work_queue_code_scanning.cjs" : currentClaimHandle() && type === "upload_code_coverage" ? "./work_queue_code_coverage.cjs" : handlerPath;
        const handlerModule = require(effectivePath);
        if (handlerModule && typeof handlerModule.main === "function") {
          // Call the factory function with config to get the message handler
          const handlerConfig = { ...(config[type] || {}) };
          const requirement = effectiveAuthorization(config, type);
          if (requirement) handlerConfig.authorization = requirement;

          if (PROJECT_HANDLER_TYPES.has(type) && !handlerConfig[GITHUB_TOKEN_CONFIG_KEY] && process.env.GH_AW_PROJECT_GITHUB_TOKEN) {
            handlerConfig[GITHUB_TOKEN_CONFIG_KEY] = process.env.GH_AW_PROJECT_GITHUB_TOKEN;
          }

          // Pass the mentions policy to handlers; aliases are resolved for each destination.
          if (MENTION_HANDLER_TYPES.has(type) && handlerConfig.mentions == null) {
            handlerConfig.mentions = config.mentions ?? {};
          }
          // Inject shared PR review buffer registry into handlers that need it
          if (PR_REVIEW_HANDLER_TYPES.has(type)) {
            handlerConfig._prReviewBufferRegistry = prReviewBufferRegistry;
          }
          if (type === "create_pull_request" || type === "push_to_pull_request_branch") {
            handlerConfig._activationCommentLinks = activationCommentLinks;
          }

          /** @type {any|null} */
          let handlerGithubClient = null;
          /** @type {any} */
          const globalState = global;
          if (handlerConfig[GITHUB_TOKEN_CONFIG_KEY] && typeof globalState.getOctokit === "function") {
            handlerGithubClient = globalState.getOctokit(handlerConfig[GITHUB_TOKEN_CONFIG_KEY]);
          }
          if (MENTION_HANDLER_TYPES.has(type) && handlerConfig.allowedMentionAliases == null) {
            if (Array.isArray(resolvedAllowedMentionAliases)) {
              handlerConfig.allowedMentionAliases = resolvedAllowedMentionAliases;
            } else {
              const targetRepo = parseRepoSlug(getDefaultTargetRepo(handlerConfig));
              handlerConfig.allowedMentionAliases = targetRepo ? await resolveAllowedMentionsFromPayload(context, handlerGithubClient ?? github, core, handlerConfig.mentions, undefined, targetRepo) : [];
            }
          }
          const messageHandler = await handlerModule.main(handlerConfig, handlerGithubClient);

          if (typeof messageHandler !== "function") {
            // This is a fatal error - the handler is misconfigured
            // Re-throw to fail the step rather than continuing
            const error = new Error(`Handler ${type} main() did not return a function - expected a message handler function but got ${typeof messageHandler}`);
            core.error(`✗ Fatal error loading handler ${type}: ${error.message}`);
            throw error;
          }

          const executable = wrapWithClientRebinding(type, messageHandler, handlerGithubClient);
          const factoryClaim = currentClaimHandle();
          const factoryIdentity = factoryClaim ? claimIdentity(factoryClaim) : null;
          const assertFactoryScope = () => {
            if (currentClaimHandle() !== factoryClaim) throw new Error("Handler state cannot escape its trusted Claim factory context");
            if (factoryIdentity) assertClaimIdentity(factoryIdentity);
          };
          const authorizedHandler = async (message, ...args) => {
            assertFactoryScope();
            const scopedMessage = await assertClaimAuthorized({ ...message, repo: message.repo ?? getDefaultTargetRepo(handlerConfig) });
            return executable(scopedMessage, ...args);
          };
          if (factoryClaim) {
            for (const [name, value] of Object.entries(executable)) {
              authorizedHandler[name] =
                typeof value === "function"
                  ? (...args) => {
                      assertFactoryScope();
                      return value(...args);
                    }
                  : value;
            }
          }
          const loadedHandler = factoryClaim ? authorizedHandler : bindHandlerToFactoryScope(executable);
          if (type === "report_incomplete" || type === "request_continuation") localDiagnosticHandlers.add(loadedHandler);
          messageHandlers.set(type, loadedHandler);
          core.info(`✓ Loaded and initialized handler for: ${type}`);
        } else {
          handlerLoadErrors.set(type, "handler module does not export a main function");
          core.warning(`Handler module ${type} does not export a main function`);
        }
      } catch (error) {
        // Re-throw fatal handler validation errors
        const errorMessage = getErrorMessage(error);
        if (errorMessage.includes("did not return a function")) {
          throw error;
        }
        // For other errors (e.g., module not found), log warning and continue
        const diagnostic = type === "merge_pull_request" ? "merge_pull_request handler initialization failed; check trusted policy and authentication configuration" : errorMessage;
        handlerLoadErrors.set(type, diagnostic);
        core.warning(`Failed to load handler for ${type}: ${diagnostic}`);
      }
    } else {
      core.debug(`Handler not enabled: ${type}`);
    }
  }

  // Load custom script handlers from GH_AW_SAFE_OUTPUT_SCRIPTS
  // These are inline scripts defined in safe-outputs.scripts that run in the handler loop
  const customScriptHandlers = currentClaimHandle() ? new Map() : loadCustomSafeOutputScriptHandlers();
  if (customScriptHandlers.size > 0) {
    core.info(`Loading ${customScriptHandlers.size} custom script handler(s): ${[...customScriptHandlers.keys()].join(", ")}`);
    const scriptBaseDir = nodePath.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw", "actions");
    for (const [scriptType, scriptFilename] of customScriptHandlers) {
      if (onlyType && scriptType !== onlyType) continue;
      // Sanitize scriptFilename to prevent path traversal attacks: only the basename
      // (no directory separators or ".." sequences) is allowed.
      const safeFilename = nodePath.basename(scriptFilename);
      if (safeFilename !== scriptFilename) {
        core.error(`Invalid script filename for ${scriptType}: path traversal detected in "${scriptFilename}" — skipping`);
        continue;
      }
      const scriptPath = nodePath.join(scriptBaseDir, safeFilename);
      // Defense-in-depth: verify the resolved path remains within the expected directory.
      // Use path.relative() to check containment robustly across all platforms.
      const relativeToBase = nodePath.relative(scriptBaseDir, scriptPath);
      if (relativeToBase.startsWith("..") || nodePath.isAbsolute(relativeToBase)) {
        core.error(`Script path outside expected directory for ${scriptType}: "${scriptPath}" — skipping`);
        continue;
      }
      try {
        const scriptModule = require(scriptPath);
        if (scriptModule && typeof scriptModule.main === "function") {
          const handlerConfig = config[scriptType] || {};
          const messageHandler = await scriptModule.main(handlerConfig);
          if (typeof messageHandler !== "function") {
            // Non-fatal: warn and skip this custom script handler rather than crashing the
            // entire safe-output loop. A misconfigured user script should not block all
            // other safe-output operations.
            core.warning(`✗ Custom script handler ${scriptType} main() did not return a function (got ${typeof messageHandler}) — this handler will be skipped`);
          } else {
            messageHandlers.set(scriptType, bindHandlerToFactoryScope(messageHandler));
            core.info(`✓ Loaded and initialized custom script handler for: ${scriptType}`);
          }
        } else {
          handlerLoadErrors.set(scriptType, "custom script module does not export a main function");
          core.warning(`Custom script handler module ${scriptType} does not export a main function — skipping`);
        }
      } catch (error) {
        // Non-fatal: log a warning and continue loading the remaining handlers. A broken
        // custom script should not prevent built-in or other custom handlers from running.
        handlerLoadErrors.set(scriptType, getErrorMessage(error));
        core.warning(`Failed to load custom script handler for ${scriptType}: ${getErrorMessage(error)} — this handler will be skipped`);
      }
    }
  }

  // Load custom action handlers from GH_AW_SAFE_OUTPUT_ACTIONS
  // These are GitHub Actions configured in safe-outputs.actions. The handler applies
  // temporary ID substitutions to the payload and exports `action_<name>_payload` outputs
  // that compiler-generated `uses:` steps consume.
  const customActionHandlers = loadCustomSafeOutputActionHandlers();
  if (customActionHandlers.size > 0) {
    core.info(`Loading ${customActionHandlers.size} custom action handler(s): ${[...customActionHandlers.keys()].join(", ")}`);
    const actionHandlerPath = require("path").join(__dirname, "safe_output_action_handler.cjs");
    for (const [actionType, actionName] of customActionHandlers) {
      if (onlyType && actionType !== onlyType) continue;
      try {
        const actionModule = require(actionHandlerPath);
        if (actionModule && typeof actionModule.main === "function") {
          const handlerConfig = { action_name: actionName, ...(config[actionType] || {}) };
          const messageHandler = await actionModule.main(handlerConfig);
          if (typeof messageHandler !== "function") {
            core.warning(`✗ Custom action handler ${actionType} main() did not return a function (got ${typeof messageHandler}) — this handler will be skipped`);
          } else {
            messageHandlers.set(actionType, bindHandlerToFactoryScope(messageHandler));
            core.info(`✓ Loaded and initialized custom action handler for: ${actionType}`);
          }
        } else {
          handlerLoadErrors.set(actionType, "custom action module does not export a main function");
          core.warning(`Custom action handler module does not export a main function — skipping ${actionType}`);
        }
      } catch (error) {
        handlerLoadErrors.set(actionType, getErrorMessage(error));
        core.warning(`Failed to load custom action handler for ${actionType}: ${getErrorMessage(error)} — this handler will be skipped`);
      }
    }
  }

  if (currentClaimHandle()) {
    for (const [type, adapter] of Object.entries(config.claim_adapters || {})) {
      if (onlyType && type !== onlyType) continue;
      if (!["prepared", "script"].includes(adapter.mode)) continue;
      messageHandlers.delete(type);
      try {
        const prepared = await createClaimAdapterHandler({
          adapter,
          authorization: effectiveAuthorization(config, type),
          github,
          filename: preparedAdapterPath(nodePath.join("/tmp/gh-aw/claim-adapters", type, String(currentClaimAssignment().claims.findIndex(member => member.handle === currentClaimHandle()))), currentClaimHandle(), type),
          loadEffectHandler: async effectType => {
            const handlerModule = require(HANDLER_MAP[effectType]);
            return handlerModule.main({ ...(config[effectType] || {}), "target-repo": adapter["target-repo"] });
          },
        });
        messageHandlers.set(type, prepared);
      } catch (error) {
        handlerLoadErrors.set(type, getErrorMessage(error));
      }
    }
  }

  core.info(`Loaded ${messageHandlers.size} handler(s)`);
  return messageHandlers;
}

/**
 * Collect report_missing_tool, report_missing_data, noop, and report_incomplete messages from the messages array
 * @param {Array<Object>} messages - Array of safe output messages
 * @returns {{missingTools: Array<any>, missingData: Array<any>, noopMessages: Array<any>, reportIncomplete: Array<any>}} Object with collected missing items, noop messages, and incomplete signals
 */
function collectMissingMessages(messages) {
  const missingTools = [];
  const missingData = [];
  const noopMessages = [];
  const reportIncomplete = [];

  for (const message of messages) {
    if (message.type === "report_missing_tool") {
      // Extract relevant fields from report_missing_tool message
      if (message.tool && message.reason) {
        missingTools.push({
          tool: message.tool,
          reason: message.reason,
          alternatives: message.alternatives || null,
        });
      }
    } else if (message.type === "report_missing_data") {
      // Extract relevant fields from report_missing_data message
      if (message.data_type && message.reason) {
        missingData.push({
          data_type: message.data_type,
          reason: message.reason,
          context: message.context || null,
          alternatives: message.alternatives || null,
        });
      }
    } else if (message.type === "noop") {
      // Extract relevant fields from noop message
      if (message.message) {
        noopMessages.push({
          message: message.message,
        });
      }
    } else if (message.type === "report_incomplete") {
      // Extract relevant fields from report_incomplete message
      if (message.reason) {
        reportIncomplete.push({
          reason: message.reason,
          details: message.details || null,
        });
      }
    }
  }

  core.info(`Collected ${missingTools.length} missing tool(s), ${missingData.length} missing data item(s), ${noopMessages.length} noop message(s), and ${reportIncomplete.length} incomplete signal(s)`);
  return { missingTools, missingData, noopMessages, reportIncomplete };
}

function rejectCrossClaimTemporaryReferences(partitions, rejected) {
  const temporaryOwners = new Map();
  for (const [handle, members] of partitions) {
    for (const member of members) {
      const id = getCreatedTemporaryId(member);
      if (id) {
        if (!temporaryOwners.has(id)) temporaryOwners.set(id, new Set());
        temporaryOwners.get(id).add(handle);
      }
    }
  }
  for (const [handle, members] of partitions) {
    const retainedIndices = [];
    const sourceBatch = authorizationBatchContexts.get(members);
    const retained = members.filter((member, index) => {
      const foreign = [...extractTemporaryIdReferences(member)].find(id => temporaryOwners.has(id) && !temporaryOwners.get(id).has(handle));
      if (!foreign) {
        if (sourceBatch) retainedIndices.push(sourceBatch.indices[index]);
        return true;
      }
      rejected.push({ type: member.type, claim_handle: handle, success: false, errorCode: "claim_scope_invalid", error: "Cross-Claim temporary-ID references are forbidden; exchange data through verified DAG Results" });
      return false;
    });
    if (sourceBatch) authorizationBatchContexts.set(retained, { digest: sourceBatch.digest, indices: retainedIndices });
    partitions.set(handle, retained);
  }
}

/**
 * Format a log message for a manifest entry.
 * Prefers the URL when available, otherwise falls back to the item number.
 *
 * @param {{type: string, url?: string, number?: number}} item - Manifest item
 * @returns {string} Formatted log message
 */
function formatManifestLogMessage(item) {
  if (item.url) return `📝 Manifest: logged ${item.type} → ${item.url}`;
  if (item.number != null) return `📝 Manifest: logged ${item.type} #${item.number}`;
  return `📝 Manifest: logged ${item.type}`;
}

function logCreatedItemFromResult(onItemCreated, messageType, result) {
  if (!onItemCreated) {
    return;
  }
  if (Array.isArray(result)) {
    for (const item of result) {
      const createdItem = extractCreatedItemFromResult(messageType, item);
      if (createdItem) {
        core.info(formatManifestLogMessage(createdItem));
        onItemCreated(createdItem);
      }
    }
    return;
  }
  const createdItem = extractCreatedItemFromResult(messageType, result);
  if (createdItem) {
    core.info(formatManifestLogMessage(createdItem));
    onItemCreated(createdItem);
  }
}

/**
 * Retroactively mark buffered review results as failed when the finalization POST fails.
 * Both submit_pull_request_review and create_pull_request_review_comment return
 * success:true during message processing (they only buffer), so the failure must be
 * reflected here to ensure the Processing Summary shows the correct counts.
 *
 * @param {Array<{type: string, success: boolean, error?: string}>} results - Processing results to mutate
 * @param {string} errorMessage - Error message to attach to the rolled-back results
 */
function rollbackReviewResults(results, errorMessage) {
  for (const r of results) {
    if ((r.type === "submit_pull_request_review" || r.type === "create_pull_request_review_comment") && r.success === true) {
      r.success = false;
      r.error = `Review finalization failed: ${errorMessage}`;
    }
  }
}

/**
 * Roll back processing results for a specific PR when its review finalization fails.
 * Matches results by repo and pull_request_number. Falls back to rolling back all
 * review results when no results carry per-PR identifiers.
 *
 * @param {Array<{type: string, success: boolean, error?: string, repo?: string, pull_request_number?: number, result?: {repo?: string, pull_request_number?: number}}>} results
 * @param {string} repo - Repository slug (owner/repo)
 * @param {number} prNumber - Pull request number
 * @param {string} errorMessage - Error message to attach to the rolled-back results
 */
function rollbackReviewResultsForPR(results, repo, prNumber, errorMessage) {
  // processMessages wraps each handler result under r.result, so per-PR identifiers
  // are nested there. Fall back to top-level for backward compatibility with callers
  // that pass raw handler results directly.
  const prResults = results.filter(
    r => (r.type === "submit_pull_request_review" || r.type === "create_pull_request_review_comment") && r.success === true && (r.result?.repo ?? r.repo) === repo && (r.result?.pull_request_number ?? r.pull_request_number) === prNumber
  );
  if (prResults.length > 0) {
    for (const r of prResults) {
      r.success = false;
      r.error = `Review finalization failed: ${errorMessage}`;
    }
  } else {
    // No results carry per-PR identifiers (e.g. legacy submit_pr_review results without repo/pull_request_number).
    // Fall back to rolling back all buffered review results for this run.
    core.warning(`rollbackReviewResultsForPR: no results matched ${repo}#${prNumber} — falling back to rolling back all review results`);
    for (const r of results) {
      if ((r.type === "submit_pull_request_review" || r.type === "create_pull_request_review_comment") && r.success === true) {
        r.success = false;
        r.error = `Review finalization failed: ${errorMessage}`;
      }
    }
  }
}

/**
 * Mark buffered review results as skipped when the PR is locked and submission was
 * soft-skipped (success:true, skipped:true). Both submit_pull_request_review and
 * create_pull_request_review_comment handlers buffer during message processing, so
 * the skip must be back-propagated here so the Processing Summary reflects the actual
 * outcome (skipped) rather than a misleading success count.
 *
 * @param {Array<{type: string, success: boolean, skipped?: boolean, skipReason?: string, deferred_manifest?: boolean, result?: {deferred_manifest?: boolean}}>} results - Processing results to mutate
 * @param {string} skipReason - Human-readable reason for the skip
 */
function skipReviewResults(results, skipReason) {
  for (const r of results) {
    if ((r.type === "submit_pull_request_review" || r.type === "create_pull_request_review_comment") && r.success === true) {
      r.skipped = true;
      r.skipReason = skipReason;
      r.deferred_manifest = false;
      if (r.result) r.result.deferred_manifest = false;
    }
  }
}

/**
 * Mark buffered review results for a specific PR as skipped.
 *
 * @param {Array<{type: string, success: boolean, skipped?: boolean, skipReason?: string, deferred_manifest?: boolean, repo?: string, pull_request_number?: number, result?: {repo?: string, pull_request_number?: number, deferred_manifest?: boolean}}>} results
 * @param {string} repo - Repository slug (owner/repo)
 * @param {number} prNumber - Pull request number
 * @param {string} skipReason - Human-readable reason for the skip
 */
function skipReviewResultsForPR(results, repo, prNumber, skipReason) {
  for (const r of results) {
    // processMessages wraps each handler result under r.result, so per-PR identifiers
    // are nested there. Fall back to top-level for backward compatibility with callers
    // that pass raw handler results directly.
    if ((r.type === "submit_pull_request_review" || r.type === "create_pull_request_review_comment") && r.success === true && (r.result?.repo ?? r.repo) === repo && (r.result?.pull_request_number ?? r.pull_request_number) === prNumber) {
      r.skipped = true;
      r.skipReason = skipReason;
      r.deferred_manifest = false;
      if (r.result) r.result.deferred_manifest = false;
    }
  }
}

/** Types whose failures are surfaced as warnings rather than failing the safe_outputs job. */
const REPORT_ONLY_FAILURE_TYPES = new Set(["assign_to_agent", "upload_artifact", "upload_code_coverage"]);

/**
 * Determine whether a failed result should be reported without failing the safe_outputs job.
 * Agent assignment can fail after other safe outputs already succeeded, so those failures
 * are surfaced through dedicated outputs and summaries instead of failing the entire job.
 * Artifact uploads are best-effort and non-critical: a failed upload should not fail an
 * otherwise-successful run.
 *
 * @param {{type?: string, success?: boolean, deferred?: boolean, skipped?: boolean, cancelled?: boolean}|null|undefined} result
 * @returns {boolean}
 */
function isReportOnlyFailureResult(result) {
  return isFailedProcessingResult(result) && !!(result?.type && REPORT_ONLY_FAILURE_TYPES.has(result.type));
}

/**
 * Partition processing results into fatal and report-only failures.
 *
 * @param {Array<{type?: string, success?: boolean, deferred?: boolean, skipped?: boolean, cancelled?: boolean, error?: string}>} results
 * @returns {{fatalFailures: Array<any>, reportOnlyFailures: Array<any>}}
 */
function partitionFailureResults(results) {
  const failedResults = results.filter(isFailedProcessingResult);
  const reportOnlyFailures = failedResults.filter(r => REPORT_ONLY_FAILURE_TYPES.has(r?.type ?? ""));
  const fatalFailures = failedResults.filter(r => !REPORT_ONLY_FAILURE_TYPES.has(r?.type ?? ""));
  return { fatalFailures, reportOnlyFailures };
}

/**
 * Export item-level safe-output status as GitHub Actions outputs.
 *
 * @param {{itemsSucceeded: number, itemsApplied?: number, itemsSkipped?: number, itemsWarnings?: number, itemsCancelled?: number, itemsDeferred?: number, itemsFailed: number, status: string}} status
 */
function setSafeOutputsStatusOutputs(status) {
  core.setOutput("items_succeeded", String(status.itemsSucceeded));
  core.setOutput("items_applied", String(status.itemsApplied ?? status.itemsSucceeded));
  core.setOutput("items_skipped", String(status.itemsSkipped ?? 0));
  core.setOutput("items_warnings", String(status.itemsWarnings ?? 0));
  core.setOutput("items_cancelled", String(status.itemsCancelled ?? 0));
  core.setOutput("items_deferred", String(status.itemsDeferred ?? 0));
  core.setOutput("items_failed", String(status.itemsFailed));
  core.setOutput("status", status.status);
}

/**
 * @param {string} type
 * @param {number} messageIndex
 * @param {Record<string, any>} result
 * @returns {Record<string, any>}
 */
function buildSkippedResult(type, messageIndex, result) {
  const message = result.reason || result.warning || result.error || "Handler returned skipped: true";
  return {
    type,
    messageIndex,
    success: result.success === true,
    skipped: true,
    ...(result.warning ? { warning: result.warning } : {}),
    ...(result.reason ? { reason: result.reason } : {}),
    ...(result.reasonCode ? { reasonCode: result.reasonCode } : {}),
    ...(result.errorCode ? { errorCode: result.errorCode } : {}),
    error: message,
    result,
  };
}

/** @param {string} type @param {any} result @param {number} messageIndex */
function recordMergeDecision(type, result, messageIndex) {
  if (type !== "merge_pull_request" || !result || result.deferred) return result;
  if (result.success === false) result = { ...result, error: mergeProcessingError(result) };
  try {
    writeMergePullRequestDecision(result, messageIndex);
    return { ...result, audit_recorded: true };
  } catch {
    const auditError = `${ERR_SYSTEM}: Failed to persist merge pull request decision`;
    core.warning(auditError);
    return { ...result, audit_failed: true, audit_error: auditError };
  }
}

/** @param {any} [result] */
function mergeProcessingError(result) {
  if (result?.operation_outcome === "indeterminate") return "Merge outcome is indeterminate; inspect the live pull request before any further action. The merge was not retried.";
  // Only preserve exact fixed diagnostics; other handler errors can contain private API data.
  switch (result?.error) {
    case "merge_pull_request gate checks failed":
    case `${SAFE_OUTPUT_E007}: Missing check producer evidence for required check`:
    case `${SAFE_OUTPUT_E007}: Unexpected check producer for required check`:
    case `${SAFE_OUTPUT_E007}: Ambiguous check producers for required check`:
    case `${SAFE_OUTPUT_E007}: GitHub rejected the merge request (HTTP 405); the merge was not retried`:
    case `${SAFE_OUTPUT_E007}: GitHub rejected the merge request (HTTP 409); the merge was not retried`:
    case `${SAFE_OUTPUT_E007}: GitHub rejected the merge request (HTTP 422); the merge was not retried`:
      return result.error;
    default:
      return "merge_pull_request failed";
  }
}

/** @param {any} result */
function projectMergeFailure(result) {
  return {
    failure_reasons: (result.failure_reasons || []).filter(reason => typeof reason.code === "string" && /^[a-z_]{1,80}$/.test(reason.code)).map(reason => ({ code: reason.code })),
    ...(result.audit_recorded ? { audit_recorded: true } : {}),
    ...(result.audit_failed ? { audit_failed: true, audit_error: result.audit_error } : {}),
    ...(typeof result.staged === "boolean" ? { staged: result.staged } : {}),
    ...(result.operation_outcome === "indeterminate" ? { operation_outcome: "indeterminate", merged: null, data_trust: "untrusted" } : {}),
  };
}

/**
 * Compute the processing order (original message indices) so that temporary-ID
 * producers run before consumers while preserving the original order for
 * independent messages and dependency cycles.
 *
 * @param {Array<Record<string, any>>} messages
 * @returns {Array<number>} original message indices in processing order
 */
function sortMessageIndicesByTemporaryIdDependencies(messages) {
  const plan = buildSafeOutputDependencyPlan(messages);
  if (plan.cyclic) {
    core.warning("Temporary ID dependency cycle detected; preserving original order for cyclic safe outputs");
  }
  return plan.order;
}

/**
 * Sort messages so temporary-ID producers run before consumers while preserving
 * the original order for independent messages and dependency cycles.
 *
 * @param {Array<Record<string, any>>} messages
 * @returns {Array<Record<string, any>>}
 */
function sortMessagesByTemporaryIdDependencies(messages) {
  return sortMessageIndicesByTemporaryIdDependencies(messages).map(index => messages[index]);
}

/**
 * Process all messages from agent output in the order they appear
 * Dispatches each message to the appropriate handler while maintaining shared state (temporary ID map)
 * Tracks outputs created with unresolved temporary IDs and generates synthetic updates after resolution
 *
 * @param {Map<string, Function>} messageHandlers - Map of message handler functions
 * @param {Array<Object>} messages - Array of safe output messages
 * @param {((item: {type: string, url?: string, number?: number, repo?: string, temporaryId?: string}) => void)|null} [onItemCreated] - Optional callback invoked after each successful create operation (for manifest logging)
 * @param {((results: Array<any>, protectedTargets: Set<string>, requiredComments: Map<string, any[]>) => Promise<void>)|null} [finalizeBufferedOutputs] - Finalize buffered effects before resolving waiting dependencies
 * @returns {Promise<{success: boolean, results: Array<any>, temporaryIdMap: Object, artifactUrlMap: Map<string, string>, outputsWithUnresolvedIds: Array<any>, missings: Object, codePushFailures: Array<{type: string, error: string}>, authorizeSyntheticUpdate: Function}>}
 */
async function processMessages(messageHandlers, messages, onItemCreated = null, finalizeBufferedOutputs = null) {
  const trustedConfig = handlerConfigurations.get(messageHandlers) || {};
  const originalMessages = JSON.parse(JSON.stringify(messages));
  const reviewLocalTemporaryIds = new Set(originalMessages.filter(message => message.type === "create_pull_request_review_comment" && isTemporaryId(message.temporary_id)).map(message => normalizeTemporaryId(message.temporary_id)));
  const authorizations = new Map();
  const permissions = new Map();
  const actionTypes = loadCustomSafeOutputActionHandlers();
  const scriptTypes = loadCustomSafeOutputScriptHandlers();
  const authorizationConfigured = hasAuthorization(trustedConfig);
  const protocol = require("./safe_output_authorization.cjs");
  const sourceBatch = authorizationBatchContexts.get(messages);
  const batchDigest = authorizationConfigured ? sourceBatch?.digest || protocol.canonicalDigest(originalMessages) : null;
  const stagedFor = (type, message = null) => isStagedMode(trustedConfig) || isStagedMode(trustedDescriptor(trustedConfig, type, message));
  const previewOnly = (type, message) => !!effectiveAuthorization(trustedConfig, type, message) || scriptTypes.has(type) || actionTypes.has(type);

  // Claim effects recorded during an invocation carry its safe-output type.
  function executeInvocation(type, index, message, resolvedTemporaryIds, temporaryIdMap, handler, phase = "primary") {
    const run = () => executeScopedInvocation(type, index, message, resolvedTemporaryIds, temporaryIdMap, handler, phase);
    return currentClaimHandle() ? withClaimOutputType(type, run) : run();
  }

  async function executeScopedInvocation(type, index, message, resolvedTemporaryIds, temporaryIdMap, handler, phase) {
    if (type === "dispatch_repository" && trustedConfig[type]?.tools && !Object.hasOwn(trustedConfig[type].tools, message.tool_name)) {
      return { success: false, error: "Repository dispatch tool is not configured" };
    }
    const descriptor = trustedDescriptor(trustedConfig, type, message);
    const requirement = effectiveAuthorization(trustedConfig, type, message);
    if (stagedFor(type, message)) {
      if (authorizationConfigured || previewOnly(type, message)) authorizations.set(index, { state: "skipped_staged", reason_code: "skipped_staged" });
      return previewOnly(type, message) ? { success: true, staged: true, preview: true } : handler(message, resolvedTemporaryIds, temporaryIdMap);
    }
    if (!requirement) {
      if (authorizationConfigured) authorizations.set(index, { state: descriptor.builtin === true && trustedConfig.authorization ? "exempt_builtin" : "not_configured" });
      return handler(message, resolvedTemporaryIds, temporaryIdMap);
    }
    const effectiveTargets = getEffectiveTemporaryTargets(type, message, descriptor);
    const preservedTargetFields = new Set();
    const numericTargetValues = new Map();
    for (const targetField of effectiveTargets) {
      const target = resolveEffectiveTemporaryTarget(targetField, message, temporaryIdMap, resolvedTemporaryIds);
      if (target.wasTemporaryId && !target.resolved && !targetField.titleFallback) {
        return { deferred: true, reason: "Waiting for temporary target resolution" };
      }
      if (!target.wasTemporaryId) continue;
      if (targetField.resolver === "manager-number" || targetField.normalize === "number") {
        const value = typeof target.resolved === "object" && target.resolved !== null ? target.resolved.number : target.resolved;
        if (typeof value === "number") numericTargetValues.set(targetField.field, value);
      } else preservedTargetFields.add(targetField.field);
    }
    const preparedAdapter = !!currentClaimHandle() && ["prepared", "script"].includes(trustedConfig.claim_adapters?.[type]?.mode);
    const preparedMessage = normalizeInvocationInputs(
      message,
      preparedAdapter ? new Map() : temporaryIdMap,
      getDefaultTargetRepo(descriptor),
      "",
      preservedTargetFields,
      numericTargetValues,
      type === "submit_pull_request_review" ? reviewLocalTemporaryIds : new Set()
    );
    authorizations.set(index, { state: "error", reason_code: "invalid_preparation" });
    const invocationBatchDigest = sourceBatch?.digests?.[index] || batchDigest || protocol.canonicalDigest(originalMessages);
    const implementationPath = preparedAdapter
      ? nodePath.join(__dirname, "work_queue_claim_adapters.cjs")
      : phase !== "primary"
        ? __filename
        : scriptTypes.has(type)
          ? nodePath.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw", "actions", nodePath.basename(scriptTypes.get(type) || ""))
          : nodePath.join(
              __dirname,
              currentClaimHandle() && type === "create_code_scanning_alert"
                ? "work_queue_code_scanning.cjs"
                : currentClaimHandle() && type === "upload_code_coverage"
                  ? "work_queue_code_coverage.cjs"
                  : HANDLER_MAP[type] || "safe_output_action_handler.cjs"
            );
    const caller = protocol.buildAuthorizationCaller();
    const targetKind = TARGET_SELECTOR_KINDS[type];
    const defaults = freezeInvocationValue({
      repository: getDefaultTargetRepo(descriptor),
      ...(targetKind ? { target_selector: descriptor.target || "triggering" } : {}),
      ...(MENTION_HANDLER_TYPES.has(type) ? { mentions: descriptor.mentions ?? trustedConfig.mentions ?? {} } : {}),
    });
    const refs = [`source-repository-id:${caller.repository_id}`];
    if (isExactRepoSlug(parseRepoSlug(defaults.repository))) refs.push(`${descriptor["target-repo"] ? "configured" : "default"}-repository:${defaults.repository}`);
    const nativeSelector = REPOSITORY_SELECTOR_TYPES.has(type) && !scriptTypes.has(type) && !actionTypes.has(type) && !preparedAdapter;
    if (nativeSelector) {
      const { defaultTargetRepo, allowedRepos } = resolveTargetRepoConfig(descriptor);
      const repository = resolveAndValidateRepo(preparedMessage, defaultTargetRepo, allowedRepos, type);
      if (!repository.success) {
        authorizations.set(index, { state: "not_requested", reason_code: "target_selector_invalid" });
        return { success: false, error: repository.error };
      }
      if (repository.success && isExactRepoSlug(repository.repoParts)) {
        refs.push(`validated-repository-selector:${repository.repo}`);
        if (targetKind && preparedMessage.comment_id == null) {
          const targetMessage = { ...preparedMessage };
          for (const targetField of effectiveTargets) {
            const resolvedTarget = resolveEffectiveTemporaryTarget(targetField, message, temporaryIdMap, resolvedTemporaryIds);
            if (!resolvedTarget.wasTemporaryId || resolvedTarget.resolved === null || resolvedTarget.resolved === false) continue;
            const field = targetField.field === "pr-number" ? "pull_request_number" : targetField.field;
            const value = resolvedTarget.resolved !== null && typeof resolvedTarget.resolved === "object" ? resolvedTarget.resolved.number : resolvedTarget.resolved;
            if (typeof value === "number") targetMessage[field] = value;
          }
          const target = resolveTarget({
            targetConfig: String(defaults.target_selector),
            item: targetMessage,
            context,
            itemType: type,
            supportsPR: targetKind === "issue_or_pull_request",
            supportsIssue: targetKind === "issue",
          });
          if (target.success && Number.isSafeInteger(target.number) && target.number > 0) refs.push(`validated-${targetKind}-selector:${repository.repo}#${target.number}`);
        }
      }
    }
    const policy = { configuration: redactSensitiveConfig(descriptor), defaults: redactSensitiveConfig(defaults), requirement };
    // Bind a frozen snapshot; the handler gets a mutable message copy and snapshot-backed resolutions.
    const boundInputs = freezeInvocationValue(structuredClone(projectInvocationJsonValue({ message: preparedMessage, resolvedTemporaryIds, defaults })));
    const invocation = protocol.prepareInvocation({
      caller,
      output: {
        type,
        name: phase === "primary" ? descriptor._authorizationName || type : `${descriptor._authorizationName || type}:${phase}`,
        executor: descriptor.authorization_executor || (scriptTypes.has(type) ? "script" : "manager"),
        implementation_digest: require("node:crypto").createHash("sha256").update(fs.readFileSync(implementationPath)).digest("hex"),
      },
      normalizedInputs: boundInputs,
      batchDigest: invocationBatchDigest,
      recordIndex: sourceBatch?.indices[index] ?? index,
      scope: { binding: "invocation", refs },
      context: {
        policy_digest: protocol.canonicalDigest(JSON.parse(JSON.stringify(policy))),
        ...(currentClaimHandle() ? { claim_id: claimIdentity(currentClaimHandle()).claim_id, delivery_id: claimIdentity(currentClaimHandle()).dispatch_id } : {}),
      },
    });
    /** @type {any} */
    const invocationState = { invocation, requirement, config: descriptor, batchDigest: invocationBatchDigest, recordIndex: sourceBatch?.indices[index] ?? index, authorization: undefined };
    invocationState.authorize = options => require("./safe_output_authorization_gate.cjs").authorizeHandlerInvocation(options);
    const deferredGate = phase === "primary" && (preparedAdapter || actionTypes.has(type) || ["upload_artifact", "call_workflow", "upload_code_coverage"].includes(type) || (type === "upload_asset" && !!currentClaimHandle()));
    const evidencePath = protocol.authorizationEvidencePath();
    let authorization;
    try {
      authorization = deferredGate ? { state: "not_requested", reason_code: "deferred_executor" } : await protocol.authorizePreparedInvocation({ core, requirement, invocation, staged: false, evidencePath });
      if (authorization.state === "allowed") permissions.set(index, authorization);
      invocationState.authorization = authorization;
      authorizations.set(index, authorization);
      // Recheck the live Claim after the remote authority wait. Both gates apply.
      await assertClaimAuthorized(preparedMessage);
      if (!deferredGate) protocol.assertAuthorizationFresh({ authorization });
      if (require("node:crypto").createHash("sha256").update(fs.readFileSync(implementationPath)).digest("hex") !== invocation.output.implementation_digest) {
        throw new Error("Safe-output implementation changed after preparation");
      }
      const result = await withSafeOutputInvocation(invocationState, () => handler(structuredClone(boundInputs.message), boundInputs.resolvedTemporaryIds, new BoundTemporaryIdMap(boundInputs.resolvedTemporaryIds, temporaryIdMap)));
      const actualAuthorization = invocationState.authorization;
      authorizations.set(index, actualAuthorization);
      if (actualAuthorization?.state === "allowed" && result?.deferred) {
        throw new Error("Authorized safe-output invocation deferred after executor entry; reconcile before retrying");
      }
      if (actualAuthorization?.state === "allowed" && !result?.deferred_manifest) {
        const outcome = protocol.recordAuthorizationOutcome({
          evidencePath,
          authorization: actualAuthorization,
          operationStatus: authorizationOperationStatus(result, result?.success === false ? "failed" : result?.skipped ? "skipped" : "success"),
          core,
        });
        if (outcome.evidenceRecorded === false) authorizations.set(index, { ...actualAuthorization, evidenceRecorded: false, reason_code: outcome.reason_code });
      }
      return result;
    } catch (error) {
      const actualAuthorization = error.authorization || invocationState.authorization || authorization;
      if (actualAuthorization) authorizations.set(index, actualAuthorization);
      if (actualAuthorization?.state === "allowed") {
        const outcome = protocol.recordAuthorizationOutcome({ evidencePath, authorization: actualAuthorization, operationStatus: authorizationOperationStatus(error, "failed"), core });
        if (outcome.evidenceRecorded === false) authorizations.set(index, { ...actualAuthorization, evidenceRecorded: false, reason_code: outcome.reason_code });
      }
      throw error;
    }
  }

  if (!messages.every(message => stagedFor(message.type, message)) && readClaimScopeContext() && !currentClaimHandle()) {
    throw new Error("Queue safe outputs must execute in a trusted per-Claim handler context; use the scoped manager");
  }
  messages = messages.map(message => {
    if (stagedFor(message.type, message)) return { ...message, type: normalizeSafeOutputName(message.type) };
    try {
      return normalizeRuntimeMessage(message);
    } catch (error) {
      return { ...message, _claimScopeError: getErrorMessage(error) };
    }
  });
  const dependencyPlan = buildSafeOutputDependencyPlan(messages);
  if (dependencyPlan.cyclic) core.warning("Safe-output dependency cycle detected; preserving legacy temporary-ID ordering only for messages without depends-on");
  const processingOrder = dependencyPlan.order;
  const waitingMessages = [];
  const results = [];
  /** @type {any} */
  const reviewHandler = messageHandlers.get("submit_pull_request_review");
  const reviewTargets = messages.map(message => (message.type === "submit_pull_request_review" && typeof reviewHandler?.getReviewTarget === "function" ? reviewHandler.getReviewTarget(message) : null));
  const reviewKey = target => (target?.repo && target?.pull_request_number ? `${target.repo}#${target.pull_request_number}` : null);

  function reviewCommentPrerequisites(i) {
    const target = reviewKey(reviewTargets[i]);
    if (!target) return new Set();
    return new Set(
      [...dependencyPlan.prerequisites[i]].filter(prerequisite => {
        const record = results.find(result => result.messageIndex === prerequisite);
        return record?.type === "create_pull_request_review_comment" && record.result?.buffered && record.result?.deferred_manifest && reviewKey(record.result) === target;
      })
    );
  }

  function protectedReviews() {
    const published = new Set(results.filter(record => record.type === "submit_pull_request_review" && record.success && !record.skipped && record.result?.deferred_manifest).map(record => reviewKey(record.result)));
    return new Set(
      reviewTargets.flatMap((target, i) => {
        const key = reviewKey(target);
        const result = results.find(record => record.messageIndex === i);
        if (!key && messages[i].type === "submit_pull_request_review" && typeof reviewHandler?.getReviewTarget === "function" && (!result || !result.success || result.skipped)) {
          return [...dependencyPlan.prerequisites[i]].flatMap(prerequisite => {
            const record = results.find(item => item.messageIndex === prerequisite && item.type === "create_pull_request_review_comment");
            const commentKey = reviewKey(record?.result);
            return commentKey && !published.has(commentKey) ? [commentKey] : [];
          });
        }
        const dependsOnOwnComment = reviewCommentPrerequisites(i).size > 0;
        const failedDependency = result && (!result.success || result.skipped) && dependencyPlan.prerequisites[i].size > 0;
        return key && !published.has(key) && (dependsOnOwnComment || failedDependency) ? [key] : [];
      })
    );
  }

  function requiredReviewComments() {
    const required = new Map();
    for (let i = 0; i < messages.length; i++) {
      const target = reviewKey(reviewTargets[i]);
      const review = results.find(record => record.messageIndex === i);
      if (!target || !review?.success || !review.result?.deferred_manifest) continue;
      const comments = [...reviewCommentPrerequisites(i)].map(index => results.find(record => record.messageIndex === index)?.result?.metadata);
      if (comments.length) required.set(target, comments);
    }
    return required;
  }
  const detectionConclusion = getEffectiveDetectionConclusion(process.env.GH_AW_DETECTION_CONCLUSION, process.env.GH_AW_DETECTION_REASON);
  const detectionConclusionMissing = String(process.env.GH_AW_RUN_DETECTION || "").trim() === "true" && detectionConclusion === "";

  // Collect report_missing_tool, report_missing_data, noop, and report_incomplete messages first
  const missings = collectMissingMessages(messages.filter(message => !currentClaimHandle() || !message._claimScopeError));
  if (currentClaimHandle()) setCollectedMissings(missings);

  // Initialize shared temporary ID map
  // This will be populated by handlers as they create entities with temporary IDs
  /** @type {Map<string, {repo: string, number: number}>} */
  const temporaryIdMap = new Map();

  // Track artifact URL mappings: normalised tmpId → artifact download URL.
  // Populated after each successful upload_artifact call so that subsequent
  // messages can have '#aw_ID' references replaced with the real artifact URL.
  /** @type {Map<string, string>} */
  const artifactUrlMap = new Map();

  // Track outputs that were created with unresolved temporary IDs
  // Format: {type, message, result, originalTempIdMapSize}
  /** @type {Array<{type: string, message: any, result: any, originalTempIdMapSize: number}>} */
  const outputsWithUnresolvedIds = [];

  // Track messages that were deferred due to unresolved temporary IDs
  // These will be retried after the first pass when more temp IDs may be resolved
  /** @type {Array<{type: string, message: any, messageIndex: number, handler: Function}>} */
  const deferredMessages = [];

  // Track code-push failures for later comment annotations and conclusion reporting.
  /** @type {Array<{type: string, error: string}>} */
  const codePushFailures = [];

  /** @type {{type: string, error: string, skipped: boolean}|null} */
  let codePushSkip = null;

  // Track when a code-push operation falls back to creating an issue or pull request instead.
  // When set, subsequent add_comment messages will receive a correction note prepended
  // to their body so the posted comment accurately reflects the actual fallback target.
  /** @type {{type: string, fallbackTargetType: "issue" | "pull_request", number: number, url: string}|null} */
  let codePushFallbackInfo = null;

  // Load custom safe output job types (from GH_AW_SAFE_OUTPUT_JOBS env var)
  // These are processed by dedicated custom job steps, not by this handler manager
  const customJobTypes = loadCustomSafeOutputJobTypes();
  if (customJobTypes.size > 0) {
    core.info(`Loaded ${customJobTypes.size} custom safe output job type(s): ${[...customJobTypes].join(", ")}`);
  }

  core.info(`Processing ${messages.length} message(s) in order of appearance...`);

  /** @param {number} i @param {boolean} allowWaiting */
  async function processMessage(i, allowWaiting) {
    let message = messages[i];
    const messageType = message.type;
    const dependency = checkSafeOutputDependencies(dependencyPlan, i, results, allowWaiting, reviewCommentPrerequisites(i));
    if (dependency.state === "waiting") {
      waitingMessages.push(i);
      return;
    }
    if (dependency.state === "failed") {
      results.push({ type: messageType, messageIndex: i, success: false, errorCode: dependency.errorCode, error: dependency.error });
      return;
    }
    if (!stagedFor(messageType, message)) {
      try {
        if (currentClaimHandle() && message._claimScopeError) throw new Error(message._claimScopeError);
        message = await assertClaimAuthorized(message);
      } catch (error) {
        results.push({
          type: messageType,
          messageIndex: i,
          claim_handle: message.claim_handle,
          success: false,
          ...(error.suppressed
            ? { cancelled: error.state === "cancelled", skipped: true, reason: error.state === "result" ? "Durable Result settled; effects not replayed" : "Claim cancelled; outputs suppressed", errorCode: error.code }
            : { errorCode: message._claimScopeErrorCode || error.code || "claim_scope_invalid", error: getErrorMessage(error) }),
        });
        return;
      }
    }

    if (!messageType) {
      core.warning(`Skipping message ${i + 1} without type`);
      return;
    }

    if (detectionConclusion === "failure" || detectionConclusionMissing) {
      // Defence in depth: a failure conclusion that is not a warn-mode tooling failure
      // (those are normalized to "warning" above), or a missing conclusion when detection
      // was required, never reaches safe_outputs in a valid run. Never apply outputs
      // unrestricted when it does.
      const errorCode = detectionConclusionMissing ? "threat_detection_conclusion_missing" : "threat_detection_failure_blocked";
      const error = detectionConclusionMissing
        ? `Threat detection was required but reported no conclusion; "${messageType}" was not applied.`
        : `Threat detection concluded failure. ${getDetectionReasonText(process.env.GH_AW_DETECTION_REASON)} "${messageType}" was not applied.`;
      core.warning(`🚫 ${error}`);
      results.push({
        type: messageType,
        messageIndex: i,
        success: false,
        cancelled: true,
        threatDetected: true,
        errorCode,
        error,
      });
      return;
    }

    if (detectionConclusion === "warning") {
      const threatPolicy = getThreatWarningPolicy(messageType, messageHandlers);
      if (threatPolicy.policy === "abort") {
        const errorCode = "threat_detected_abort_policy";
        const error = `Threat-detection warn policy aborted "${messageType}" (Requirement ${WTD3_REQUIREMENT_ID}): non-reviewable outputs must not be applied when detection conclusion is warning.`;
        core.warning(`🚫 ${error}`);
        results.push({
          type: messageType,
          messageIndex: i,
          success: false,
          cancelled: true,
          threatDetected: true,
          errorCode,
          error,
        });
        return;
      }
      if (threatPolicy.policy === "convertible") {
        // Conversion execution is implemented in the handler for the convertible type.
        // Keep THREAT_WARNING_CONVERTIBLE_TYPES and handler conversion logic in sync.
        // Current mapping: push_to_pull_request_branch -> create_pull_request
        // (implemented in push_to_pull_request_branch.cjs warning-mode review flow).
        core.info(`Threat-detection warn policy conversion required for "${messageType}" -> "${threatPolicy.mappedType}" (${WTD2_REQUIREMENT_ID})`);
      } else if (threatPolicy.policy === "none") {
        core.warning(`Threat-detection warn policy has no explicit classification for "${messageType}"; allowing handler execution by default`);
      }
    }

    if (stagedFor(messageType, message) && previewOnly(messageType, message)) {
      authorizations.set(i, { state: "skipped_staged", reason_code: "skipped_staged" });
      core.info(`Staged safe-output invocation preview: ${messageType}`);
      results.push({ type: messageType, messageIndex: i, success: true, staged: true, result: { staged: true, preview: true } });
      return;
    }

    // Ordinary continuation requests are recorded once in conclusion; Claims
    // retain their scoped diagnostic handler rather than an anonymous replay.
    const messageHandler = messageType === "request_continuation" && !currentClaimHandle() ? undefined : messageHandlers.get(messageType);

    if (!messageHandler) {
      // Check if this message type is handled by a standalone step
      if (STANDALONE_STEP_TYPES.has(messageType)) {
        if (currentClaimHandle() && messageType === "noop") {
          results.push({ type: messageType, messageIndex: i, claim_handle: currentClaimHandle(), success: true, result: { effect: "none" } });
          return;
        }
        // Silently skip - this is handled by a dedicated step
        core.debug(`Message ${i + 1} (${messageType}) will be handled by standalone step`);
        results.push({
          type: messageType,
          messageIndex: i,
          success: false,
          skipped: true,
          delegated: true,
          reason: "Handled by standalone step",
        });
        return;
      }

      // Check if this message type is handled by a custom safe output job
      if (customJobTypes.has(messageType)) {
        core.debug(`Message ${i + 1} (${messageType}) will be handled by custom safe output job`);

        // Log the dispatch to the manifest so the operation is counted in SafeItemsCount.
        // The custom job does the actual work; here we record the intent from the message.
        if (onItemCreated) {
          // Prefer item_number (explicit target), fall back to issue_number then pull_request_number.
          // This mirrors the precedence order used by individual safe output handlers.
          const rawNumber = message.item_number ?? message.issue_number ?? message.pull_request_number;
          const itemNumber = rawNumber != null ? parseInt(String(rawNumber), 10) : undefined;
          const validNumber = itemNumber != null && !Number.isNaN(itemNumber) ? itemNumber : undefined;

          const messageResult = {
            ...(validNumber != null ? { number: validNumber } : {}),
            ...(message.repo ? { repo: message.repo } : {}),
          };
          const createdItem = extractCreatedItemFromResult(messageType, messageResult);
          if (createdItem) {
            core.info(formatManifestLogMessage(createdItem));
            onItemCreated(createdItem);
          }
        }

        results.push({
          type: messageType,
          messageIndex: i,
          success: false,
          skipped: true,
          delegated: true,
          reason: "Handled by custom safe output job",
        });
        return;
      }

      // Unknown message type - warn the user
      const loadError = handlerLoadErrorsByHandlerMap.get(messageHandlers)?.get(messageType);
      const warning = loadError
        ? `No handler available for message type '${messageType}' (message ${i + 1}/${messages.length}). The handler was configured but failed to load: ${loadError}`
        : `No handler loaded for message type '${messageType}' (message ${i + 1}/${messages.length}). The message will be skipped. This may happen if the safe output type is not configured in the workflow's safe-outputs section.`;
      core.warning(warning);
      results.push({
        type: messageType,
        messageIndex: i,
        success: false,
        error: loadError ? `No handler loaded for type '${messageType}': ${loadError}` : `No handler loaded for type '${messageType}'`,
      });
      return;
    }

    try {
      const budget = connectorBudgetsByHandlerMap.get(messageHandlers)?.get(messageType);
      if (budget) {
        if (budget.attempted >= budget.max) {
          results.push({
            type: messageType,
            messageIndex: i,
            success: false,
            errorCode: "connector_budget_exceeded",
            error: `Connector operation "${messageType}" exceeded its max of ${budget.max} attempted invocation(s)`,
          });
          return;
        }
        // Reserve synchronously before invoking the credentialed handler. Failed
        // proposals consume budget too; HTTP retries stay inside that invocation.
        budget.attempted++;
      }
      core.info(`Processing message ${i + 1}/${messages.length}: ${messageType}`);

      // Convert Map to plain object for handler
      const resolvedTemporaryIds = Object.fromEntries(temporaryIdMap);

      // Record the temp ID map size before processing to detect new IDs
      const tempIdMapSizeBefore = temporaryIdMap.size;

      // For add_comment messages: prepend any relevant correction notes before the AI-generated
      // body so users see the clarification immediately.
      let effectiveMessage = message;
      if (messageType === "add_comment") {
        // If a previous code-push operation fell back to a review issue, prepend a correction note
        // so the posted comment accurately reflects the outcome.
        if (codePushFallbackInfo) {
          const fallbackNote =
            codePushFallbackInfo.fallbackTargetType === "pull_request"
              ? "Direct push to the original pull request branch was not possible (diverged/non-fast-forward). A fallback pull request was created instead: "
              : "The pull request was not created — a fallback review issue was created instead due to protected file changes: ";
          effectiveMessage = {
            ...effectiveMessage,
            [COMMENT_PREAMBLE]: [
              { type: "separator", agentTainted: false },
              {
                type: "alert",
                kind: "NOTE",
                agentTainted: false,
                children: [
                  {
                    type: "paragraph",
                    agentTainted: false,
                    children: [
                      { type: "text", value: fallbackNote, agentTainted: false },
                      { type: "link", url: codePushFallbackInfo.url, children: [{ type: "text", value: `#${codePushFallbackInfo.number}`, agentTainted: false }], agentTainted: false },
                    ],
                  },
                ],
              },
            ],
          };
          core.info(`Prepending fallback correction note to add_comment body (fallback ${codePushFallbackInfo.fallbackTargetType}: #${codePushFallbackInfo.number})`);
        }
        // Failed and skipped publications both need a correction, but skips remain
        // non-fatal and must not inflate the code-push failure count.
        const failure = codePushFailures[0] || codePushSkip;
        if (failure) {
          effectiveMessage = {
            ...effectiveMessage,
            [COMMENT_PREAMBLE]: [
              { type: "separator", agentTainted: false },
              {
                type: "alert",
                kind: "WARNING",
                agentTainted: false,
                children: [
                  {
                    type: "paragraph",
                    agentTainted: false,
                    children: [
                      { type: "text", value: "The ", agentTainted: false },
                      { type: "code", value: failure.type, agentTainted: false },
                      { type: "text", value: "skipped" in failure ? " operation was skipped: " : " operation failed: ", agentTainted: false },
                      { type: "text", value: `${failure.error}. The code changes were not applied.`, agentTainted: true },
                    ],
                  },
                ],
              },
              ...(effectiveMessage[COMMENT_PREAMBLE] || []),
            ],
          };
          core.info(`Prepending code push failure note to add_comment body (${failure.type}: ${failure.error})`);
        }
      }

      // Pre-process: replace any '#aw_ID' artifact URL references in the message body
      // with the actual artifact URL so handlers receive the resolved URL directly.
      // This is applied to all message types that carry a 'body' field.
      if (artifactUrlMap.size > 0 && effectiveMessage.body && typeof effectiveMessage.body === "string") {
        const protectedLinks = messageType === "submit_pull_request_review" ? protectReviewLocalLinkDestinations(effectiveMessage.body, reviewLocalTemporaryIds) : null;
        const resolvedBody = protectedLinks ? protectedLinks.restore(replaceArtifactUrlReferences(protectedLinks.value, artifactUrlMap)) : replaceArtifactUrlReferences(effectiveMessage.body, artifactUrlMap);
        if (resolvedBody !== effectiveMessage.body) {
          effectiveMessage = { ...effectiveMessage, body: resolvedBody };
          core.info(`Resolved artifact URL reference(s) in ${messageType} body`);
        }
      }

      // Call the message handler with the individual message and resolved temp IDs
      await assertClaimAuthorized(effectiveMessage);
      // Incomplete signals only record local diagnostics; they do not publish to the PR.
      const stalePRReason = localDiagnosticHandlers.has(messageHandler) ? null : await getStalePullRequestReason(global.github, global.context);
      if (stalePRReason) {
        core.info(stalePRReason);
        results.push(buildSkippedResult(messageType, i, { skipped: true, reason: stalePRReason }));
        return;
      }
      const result = recordMergeDecision(messageType, await executeInvocation(messageType, i, effectiveMessage, resolvedTemporaryIds, temporaryIdMap, messageHandler), i);

      // Check if the handler explicitly returned a skipped result (e.g. policy filters,
      // no-op warnings, or if_no_changes: warn/ignore). Subsequent messages are still
      // processed, and any summary-safe diagnostics supplied by the handler are preserved.
      if (result && result.skipped === true && !result.deferred) {
        const msg = result.reason || result.warning || result.error || "Handler returned skipped: true";
        core.info(`⏭ Message ${i + 1} (${messageType}) skipped — ${msg}`);
        results.push(buildSkippedResult(messageType, i, result));
        if (CODE_PUSH_TYPES.has(messageType) && !codePushSkip) {
          codePushSkip = { type: messageType, error: msg, skipped: true };
        }
        return;
      }

      // Check if the handler explicitly returned a failure
      if (result && result.success === false && !result.deferred) {
        const errorMsg = result.error || "Handler returned success: false";
        core.error(`✗ Message ${i + 1} (${messageType}) failed: ${errorMsg}`);
        results.push({
          type: messageType,
          messageIndex: i,
          success: false,
          error: errorMsg,
          ...projectSafeOutputData({ execution_trace: result.execution_trace }),
          ...(messageType === "merge_pull_request" ? { result: projectMergeFailure(result) } : result.operation_outcome === "indeterminate" ? { result: { operation_outcome: "indeterminate" } } : {}),
        });
        // Track code-push failures for later comment annotations and conclusion reporting.
        if (CODE_PUSH_TYPES.has(messageType)) {
          codePushFailures.push({ type: messageType, error: errorMsg });
          core.warning(`⚠️ Code push operation '${messageType}' failed — continuing with remaining safe outputs (add_comment messages will include a failure note)`);
        }
        return;
      }

      // Check if the operation was deferred due to unresolved temporary IDs
      if (result && result.deferred === true) {
        core.info(`⏸ Message ${i + 1} (${messageType}) deferred - will retry after first pass`);
        deferredMessages.push({
          type: messageType,
          message: effectiveMessage,
          messageIndex: i,
          handler: messageHandler,
        });
        results.push({
          type: messageType,
          messageIndex: i,
          success: false,
          deferred: true,
          result,
        });
        return;
      }

      // If handler returned a temp ID mapping, add it to our map
      if (result && result.temporaryId && result.repo && result.number) {
        const normalizedTempId = normalizeTemporaryId(result.temporaryId);
        temporaryIdMap.set(normalizedTempId, {
          repo: result.repo,
          number: result.number,
        });
        core.info(`Registered temporary ID: ${result.temporaryId} -> ${result.repo}#${result.number}`);
      }
      if (result && result.temporaryId && result.temporaryIdEntry) {
        const normalizedTempId = normalizeTemporaryId(result.temporaryId);
        temporaryIdMap.set(normalizedTempId, result.temporaryIdEntry);
        core.info(`Registered provider temporary ID: ${result.temporaryId}`);
      }

      // If this was a successful upload_artifact, register the artifact URL so that
      // subsequent messages can have '#aw_ID' references replaced with the real URL.
      // upload_artifact returns { tmpId, artifactUrl } (not temporaryId/repo/number).
      if (messageType === "upload_artifact" && result && result.tmpId && result.artifactUrl) {
        const normalizedTmpId = normalizeTemporaryId(result.tmpId);
        if (!artifactUrlMap.has(normalizedTmpId)) {
          artifactUrlMap.set(normalizedTmpId, result.artifactUrl);
          core.info(`Registered artifact URL for temporary ID: ${result.tmpId}`);
        } else {
          core.warning(`Duplicate artifact temporary ID '${result.tmpId}' encountered; keeping the first registered URL and ignoring the later upload.`);
        }
      }

      // Track when a code-push operation falls back to an issue or pull request so subsequent
      // add_comment messages can include a correction note.
      if (CODE_PUSH_TYPES.has(messageType) && result && result.fallback_used === true) {
        if (result.issue_number != null && result.issue_url) {
          codePushFallbackInfo = {
            type: messageType,
            fallbackTargetType: "issue",
            number: result.issue_number,
            url: result.issue_url,
          };
          core.info(`Code push '${messageType}' fell back to review issue #${result.issue_number} — add_comment messages will be annotated`);
        } else if (result.pull_request_number != null && result.pull_request_url) {
          codePushFallbackInfo = {
            type: messageType,
            fallbackTargetType: "pull_request",
            number: result.pull_request_number,
            url: result.pull_request_url,
          };
          core.info(`Code push '${messageType}' fell back to pull request #${result.pull_request_number} — add_comment messages will be annotated`);
        }
      }

      // Check if this output was created with unresolved temporary IDs
      // For create_issue, create_discussion, add_comment - check if body has unresolved IDs

      // Handle the current add_comment result shape.
      if (messageType === "add_comment" && result?.commentId && result?.repo) {
        const contentToCheck = getContentToCheck(messageType, message, result);
        if (contentToCheck && hasUnresolvedTemporaryIds(contentToCheck, temporaryIdMap, artifactUrlMap)) {
          core.info(`Comment ${result.commentId} on ${result.repo}#${result.itemNumber} was created with unresolved temporary IDs - tracking for update`);
          outputsWithUnresolvedIds.push({
            type: messageType,
            message,
            result,
            originalTempIdMapSize: tempIdMapSizeBefore,
          });
        }
      }

      // Handle the legacy add_comment result shape.
      if (messageType === "add_comment" && Array.isArray(result)) {
        const contentToCheck = getContentToCheck(messageType, message, result);
        if (contentToCheck && hasUnresolvedTemporaryIds(contentToCheck, temporaryIdMap, artifactUrlMap)) {
          // Track each comment that was created with unresolved temp IDs
          for (const comment of result) {
            if (comment._tracking) {
              core.info(`Comment ${comment._tracking.commentId} on ${comment._tracking.repo}#${comment._tracking.itemNumber} was created with unresolved temporary IDs - tracking for update`);
              outputsWithUnresolvedIds.push({
                type: messageType,
                message: message,
                result: { ...comment._tracking, ...(comment.body ? { body: comment.body } : {}) },
                originalTempIdMapSize: tempIdMapSizeBefore,
              });
            }
          }
        }
      } else if (result && result.number && result.repo) {
        // Handle create_issue, create_discussion
        const contentToCheck = getContentToCheck(messageType, message, result);
        if (contentToCheck && hasUnresolvedTemporaryIds(contentToCheck, temporaryIdMap, artifactUrlMap)) {
          core.info(`Output ${result.repo}#${result.number} was created with unresolved temporary IDs - tracking for update`);
          outputsWithUnresolvedIds.push({
            type: messageType,
            message: message,
            result: result,
            originalTempIdMapSize: tempIdMapSizeBefore,
          });
        }
      }

      results.push({
        type: messageType,
        messageIndex: i,
        success: true,
        result,
      });

      // Log to manifest if this was a create operation
      logCreatedItemFromResult(onItemCreated, messageType, result);

      core.info(`✓ Message ${i + 1} (${messageType}) completed successfully`);
    } catch (error) {
      const errorMsg = messageType === "merge_pull_request" ? mergeProcessingError() : getErrorMessage(error);
      core.error(`✗ Message ${i + 1} (${messageType}) failed: ${errorMsg}`);
      results.push({
        type: messageType,
        messageIndex: i,
        success: false,
        error: errorMsg,
        ...projectSafeOutputData({ execution_trace: error?.execution_trace }),
      });
      // Track code-push failures for later comment annotations and conclusion reporting.
      if (CODE_PUSH_TYPES.has(messageType)) {
        codePushFailures.push({ type: messageType, error: getErrorMessage(error) });
        core.warning(`⚠️ Code push operation '${messageType}' failed — continuing with remaining safe outputs (add_comment messages will include a failure note)`);
      }
    }
  }

  // Keep the legacy first pass/retry boundary: a dependent waits for its
  // prerequisite's real retry result, not its provisional deferred record.
  for (const i of processingOrder) await processMessage(i, true);

  async function drainWaitingMessages() {
    for (let position = 0; position < waitingMessages.length;) {
      const i = waitingMessages[position];
      if (checkSafeOutputDependencies(dependencyPlan, i, results, true, reviewCommentPrerequisites(i)).state === "waiting") {
        position++;
        continue;
      }
      waitingMessages.splice(position, 1);
      await processMessage(i, false);
    }
  }

  // Retry deferred messages now that more temporary IDs may have been resolved
  // This retry loop mirrors the main processing loop but operates on messages that were
  // deferred during the first pass (e.g., link_sub_issue waiting for parent/sub creation).
  // IMPORTANT: Like the main loop, this must register temporary IDs and track outputs
  // with unresolved IDs to enable full synthetic update resolution.
  async function retryDeferredMessages() {
    if (deferredMessages.length === 0) return;
    core.info(`\n=== Retrying Deferred Messages ===`);
    core.info(`Found ${deferredMessages.length} deferred message(s) to retry`);

    let pending = deferredMessages.filter(deferred => results.some(result => result.messageIndex === deferred.messageIndex && result.deferred));
    for (let round = 0; pending.length > 0 && round <= messages.length; round++) {
      const roundStartTemporaryIdCount = temporaryIdMap.size;
      const attempted = new Set(pending.map(deferred => deferred.messageIndex));
      const stillDeferred = [];
      for (const deferred of pending) {
        try {
          core.info(`Retrying message ${deferred.messageIndex + 1}/${messages.length}: ${deferred.type}`);

          // Convert Map to plain object for handler
          const resolvedTemporaryIds = Object.fromEntries(temporaryIdMap);

          // Record the temp ID map size before processing to detect new IDs
          const tempIdMapSizeBefore = temporaryIdMap.size;

          // Call the handler again with updated temp ID map
          await assertClaimAuthorized(deferred.message);
          const stalePRReason = localDiagnosticHandlers.has(deferred.handler) ? null : await getStalePullRequestReason(global.github, global.context);
          const result = stalePRReason
            ? { skipped: true, reason: stalePRReason }
            : recordMergeDecision(deferred.type, await executeInvocation(deferred.type, deferred.messageIndex, deferred.message, resolvedTemporaryIds, temporaryIdMap, deferred.handler), deferred.messageIndex);

          if (result && result.skipped === true && !result.deferred) {
            const msg = result.reason || result.warning || result.error || "Handler returned skipped: true";
            core.info(`⏭ Retry of message ${deferred.messageIndex + 1} (${deferred.type}) skipped — ${msg}`);
            const resultIndex = results.findIndex(r => r.messageIndex === deferred.messageIndex);
            if (resultIndex >= 0) {
              results[resultIndex] = buildSkippedResult(deferred.type, deferred.messageIndex, result);
            }
            continue;
          }

          // Check if the handler explicitly returned a failure
          if (result && result.success === false && !result.deferred) {
            const errorMsg = result.error || "Handler returned success: false";
            core.error(`✗ Retry of message ${deferred.messageIndex + 1} (${deferred.type}) failed: ${errorMsg}`);
            // Replace the deferred record so terminal retry failures classify as failed.
            const resultIndex = results.findIndex(r => r.messageIndex === deferred.messageIndex);
            if (resultIndex >= 0) {
              results[resultIndex] = {
                type: deferred.type,
                messageIndex: deferred.messageIndex,
                success: false,
                deferred: false,
                error: errorMsg,
                ...projectSafeOutputData({ execution_trace: result.execution_trace }),
                result: deferred.type === "merge_pull_request" ? projectMergeFailure(result) : { ...result, success: false, deferred: false, error: errorMsg },
              };
            }
            continue;
          }

          // Check if still deferred
          if (result && result.deferred === true) {
            core.warning(`⏸ Message ${deferred.messageIndex + 1} (${deferred.type}) still deferred - some temporary IDs remain unresolved`);
            // Update the existing result entry
            const resultIndex = results.findIndex(r => r.messageIndex === deferred.messageIndex);
            if (resultIndex >= 0) {
              results[resultIndex].result = result;
            }
            stillDeferred.push(deferred);
          } else {
            core.info(`✓ Message ${deferred.messageIndex + 1} (${deferred.type}) completed on retry`);

            // If handler returned a temp ID mapping, add it to our map
            // This ensures that sub-issues created during deferred retry have their temporary IDs
            // registered so parent issues can reference them in synthetic updates
            if (result && result.temporaryId && result.repo && result.number) {
              const normalizedTempId = normalizeTemporaryId(result.temporaryId);
              temporaryIdMap.set(normalizedTempId, {
                repo: result.repo,
                number: result.number,
              });
              core.info(`Registered temporary ID: ${result.temporaryId} -> ${result.repo}#${result.number}`);
            }

            // Check if this output was created with unresolved temporary IDs
            // For create_issue, create_discussion - check if body has unresolved IDs
            // This enables synthetic updates to resolve references after all items are created
            if (result && result.number && result.repo) {
              const contentToCheck = getContentToCheck(deferred.type, deferred.message, result);
              if (contentToCheck && hasUnresolvedTemporaryIds(contentToCheck, temporaryIdMap, artifactUrlMap)) {
                core.info(`Output ${result.repo}#${result.number} was created with unresolved temporary IDs - tracking for update`);
                outputsWithUnresolvedIds.push({
                  type: deferred.type,
                  message: deferred.message,
                  result: result,
                  originalTempIdMapSize: tempIdMapSizeBefore,
                });
              }
              if (result && result.temporaryId && result.temporaryIdEntry) {
                const normalizedTempId = normalizeTemporaryId(result.temporaryId);
                temporaryIdMap.set(normalizedTempId, result.temporaryIdEntry);
              }
            }

            // Update the result to success
            const resultIndex = results.findIndex(r => r.messageIndex === deferred.messageIndex);
            if (resultIndex >= 0) {
              results[resultIndex].success = true;
              results[resultIndex].deferred = false;
              results[resultIndex].result = result;
            }

            // Log to manifest after deferred retry success
            logCreatedItemFromResult(onItemCreated, deferred.type, result);
          }
        } catch (error) {
          const errorMsg = deferred.type === "merge_pull_request" ? mergeProcessingError() : getErrorMessage(error);
          core.error(`✗ Retry of message ${deferred.messageIndex + 1} (${deferred.type}) failed: ${errorMsg}`);
          // Replace the deferred record so terminal retry exceptions classify as failed.
          const resultIndex = results.findIndex(r => r.messageIndex === deferred.messageIndex);
          if (resultIndex >= 0) {
            results[resultIndex] = {
              type: deferred.type,
              messageIndex: deferred.messageIndex,
              success: false,
              deferred: false,
              error: errorMsg,
              ...projectSafeOutputData({ execution_trace: error?.execution_trace }),
              result: { success: false, deferred: false, error: errorMsg },
            };
          }
        } finally {
          await drainWaitingMessages();
        }
      }

      const waitingIndexes = new Set(stillDeferred.map(deferred => deferred.messageIndex));
      let newlyDeferred = false;
      for (const deferred of deferredMessages) {
        const result = results.find(item => item.messageIndex === deferred.messageIndex);
        if (result?.deferred && !attempted.has(deferred.messageIndex) && !waitingIndexes.has(deferred.messageIndex)) {
          stillDeferred.push(deferred);
          waitingIndexes.add(deferred.messageIndex);
          newlyDeferred = true;
        }
      }
      if (temporaryIdMap.size === roundStartTemporaryIdCount && !newlyDeferred) break;
      pending = stillDeferred;
    }
  }

  await retryDeferredMessages();
  const expiredReviewTargets = new Set();
  const auditedBufferedOutputs = new Set();
  if (finalizeBufferedOutputs) {
    for (let pass = 0; pass <= messages.length; pass++) {
      const pendingPublications = results.filter(record => record.success && !record.skipped && record.result?.deferred_manifest);
      for (const record of pendingPublications) {
        const permission = permissions.get(record.messageIndex);
        if (!permission) continue;
        try {
          require("./safe_output_authorization.cjs").assertAuthorizationFresh({ authorization: permission });
        } catch (error) {
          record.success = false;
          record.error = "Safe-output authorization expired before buffered publication";
          authorizations.set(record.messageIndex, error.authorization || { state: "error", reason_code: "invalid_validity" });
          const target = reviewKey(record.result);
          if (target) expiredReviewTargets.add(target);
        }
      }
      const pendingResults = new Set(pendingPublications.map(record => record.result));
      const temporaryIdCount = temporaryIdMap.size;
      const deferredCount = deferredMessages.length;
      await finalizeBufferedOutputs(results, new Set([...protectedReviews(), ...expiredReviewTargets]), requiredReviewComments());
      for (const record of pendingPublications) {
        const authorization = authorizations.get(record.messageIndex);
        const status = classifySafeOutputResult(record);
        if (authorization?.state !== "allowed" || status === "deferred" || auditedBufferedOutputs.has(record.messageIndex)) continue;
        auditedBufferedOutputs.add(record.messageIndex);
        const outcome = protocol.recordAuthorizationOutcome({
          evidencePath: protocol.authorizationEvidencePath(),
          authorization,
          operationStatus: authorizationOperationStatus(record.result, status === "success" || status === "warning" ? "success" : status === "skipped" ? "skipped" : "failed"),
          core,
        });
        if (outcome.evidenceRecorded === false) authorizations.set(record.messageIndex, { ...authorization, evidenceRecorded: false, reason_code: outcome.reason_code });
      }
      const publicationProgress = pendingPublications.some(record => classifySafeOutputResult(record) !== "deferred");
      await drainWaitingMessages();
      if (publicationProgress || temporaryIdMap.size !== temporaryIdCount || deferredMessages.length !== deferredCount) {
        await retryDeferredMessages();
      }
      if (!(publicationProgress && protectedReviews().size > 0) && !results.some(record => record.success && !record.skipped && record.result?.deferred_manifest && !pendingResults.has(record.result))) break;
    }
  }
  for (const i of waitingMessages) {
    await processMessage(i, false);
  }

  const protectedTargets = protectedReviews();
  for (const record of results) {
    const key = reviewKey(record.result);
    if (key && record.success && record.result?.deferred_manifest && protectedTargets.has(key)) {
      record.success = false;
      record.error = "Review was not submitted because its submit_pull_request_review output did not succeed";
    }
  }

  for (const result of results) {
    const outputId = messages[result.messageIndex]?.output_id;
    if (outputId !== undefined) result.output_id = outputId;
    const authorization = projectAuthorization(authorizations.get(result.messageIndex) || (authorizationConfigured ? { state: "not_requested", reason_code: result.delegated ? "deferred_executor" : "execution_prevented" } : null));
    if (authorization) result.authorization = authorization;
  }

  // Return outputs with unresolved IDs for synthetic update processing
  // Convert temporaryIdMap to plain object for serialization
  const temporaryIdMapObj = Object.fromEntries(temporaryIdMap);
  const authorizeSyntheticUpdate = async (tracked, update, performUpdate) => {
    const index = messages.indexOf(tracked.message);
    if (index < 0 && effectiveAuthorization(trustedConfig, tracked.type)) throw new Error("Missing trusted synthetic-update invocation identity");
    try {
      await executeInvocation(tracked.type, index, { ...tracked.message, ...update }, temporaryIdMapObj, temporaryIdMap, performUpdate, "temporary_id_update");
    } catch (error) {
      const record = results.find(item => item.messageIndex === index);
      if (record) {
        record.success = false;
        record.error = "Synthetic safe-output update failed";
        record.authorization = projectAuthorization(authorizations.get(index));
      }
      throw error;
    }
  };

  return {
    success: true,
    results,
    temporaryIdMap: temporaryIdMapObj,
    artifactUrlMap,
    outputsWithUnresolvedIds,
    missings,
    codePushFailures,
    authorizeSyntheticUpdate,
  };
}

/**
 * Get the content field to check for unresolved temporary IDs based on message type
 * @param {string} messageType - Type of the message
 * @param {any} message - The message object
 * @param {any} [result] - Handler result (used for transformed/managed bodies)
 * For comment_memory, handlers return a managedBody that includes XML wrapper/footer;
 * this differs from message.body and must be used for temporary ID detection.
 * @returns {string|null} Content to check for temporary IDs
 */
function getContentToCheck(messageType, message, result) {
  switch (messageType) {
    case "create_issue":
      return result?.body || message.body || "";
    case "create_discussion":
      return result?.body || message.body || "";
    case "add_comment":
      return result?.body || message.body || "";
    case "comment_memory":
      return result?.managedBody || message.body || "";
    case "create_pull_request":
      return result?.managedBody || message.body || "";
    default:
      return null;
  }
}

/**
 * Update the body of an issue with resolved temporary IDs
 * @param {any} github - GitHub API client
 * @param {any} context - GitHub Actions context
 * @param {string} repo - Repository in "owner/repo" format
 * @param {number} issueNumber - Issue number to update
 * @param {string} updatedBody - Updated body content with resolved temp IDs
 * @param {string[]} [allowedAliases] - Mentions allowed for the destination repository
 * @param {number} [maxMentions] - Maximum allowed mentions
 * @returns {Promise<void>}
 */
async function updateIssueBody(github, context, repo, issueNumber, updatedBody, allowedAliases = [], maxMentions = 50) {
  const [owner, repoName] = repo.split("/");

  core.info(`Updating issue ${repo}#${issueNumber} body with resolved temporary IDs`);

  await github.rest.issues.update({
    owner,
    repo: repoName,
    issue_number: issueNumber,
    body: sanitizeSyntheticUpdateBody(updatedBody, allowedAliases, maxMentions, context),
  });

  core.info(`✓ Updated issue ${repo}#${issueNumber}`);
}

/**
 * Update the body of a pull request with resolved temporary IDs
 * @param {any} github - GitHub API client
 * @param {any} context - GitHub Actions context
 * @param {string} repo - Repository in "owner/repo" format
 * @param {number} prNumber - Pull request number to update
 * @param {string} updatedBody - Updated body content with resolved temp IDs
 * @param {string[]} [allowedAliases] - Mentions allowed for the destination repository
 * @param {number} [maxMentions] - Maximum allowed mentions
 * @returns {Promise<void>}
 */
async function updatePullRequestBody(github, context, repo, prNumber, updatedBody, allowedAliases = [], maxMentions = 50) {
  const [owner, repoName] = repo.split("/");

  core.info(`Updating pull request ${repo}#${prNumber} body with resolved temporary IDs`);

  await github.rest.pulls.update({
    owner,
    repo: repoName,
    pull_number: prNumber,
    body: sanitizeSyntheticUpdateBody(updatedBody, allowedAliases, maxMentions, context),
  });

  core.info(`✓ Updated pull request ${repo}#${prNumber}`);
}

/**
 * Update the body of a discussion with resolved temporary IDs
 * @param {any} github - GitHub API client
 * @param {any} context - GitHub Actions context
 * @param {string} repo - Repository in "owner/repo" format
 * @param {number} discussionNumber - Discussion number to update
 * @param {string} updatedBody - Updated body content with resolved temp IDs
 * @param {string[]} [allowedAliases] - Mentions allowed for the destination repository
 * @param {number} [maxMentions] - Maximum allowed mentions
 * @returns {Promise<void>}
 */
async function updateDiscussionBody(github, context, repo, discussionNumber, updatedBody, allowedAliases = [], maxMentions = 50) {
  const [owner, repoName] = repo.split("/");

  core.info(`Updating discussion ${repo}#${discussionNumber} body with resolved temporary IDs`);

  // Get the discussion node ID first
  const query = `
    query($owner: String!, $repo: String!, $number: Int!) {
      repository(owner: $owner, name: $repo) {
        discussion(number: $number) {
          id
        }
      }
    }
  `;

  const result = await github.graphql(query, {
    owner,
    repo: repoName,
    number: discussionNumber,
  });

  const discussionId = result.repository.discussion.id;

  // Update the discussion body using GraphQL mutation
  const mutation = `
    mutation($discussionId: ID!, $body: String!) {
      updateDiscussion(input: {discussionId: $discussionId, body: $body}) {
        discussion {
          id
          number
        }
      }
    }
  `;

  await github.graphql(mutation, {
    discussionId,
    body: sanitizeSyntheticUpdateBody(updatedBody, allowedAliases, maxMentions, context),
  });

  core.info(`✓ Updated discussion ${repo}#${discussionNumber}`);
}

/**
 * Update the body of a comment with resolved temporary IDs
 * @param {any} github - GitHub API client
 * @param {any} context - GitHub Actions context
 * @param {string} repo - Repository in "owner/repo" format
 * @param {number} commentId - Comment ID to update
 * @param {string} updatedBody - Updated body content with resolved temp IDs
 * @param {boolean} isDiscussion - Whether this is a discussion comment
 * @param {string[]} [allowedAliases] - Mentions allowed for the destination repository
 * @param {number} [maxMentions] - Maximum allowed mentions
 * @returns {Promise<void>}
 */
async function updateCommentBody(github, context, repo, commentId, updatedBody, isDiscussion = false, allowedAliases = [], maxMentions = 50) {
  const [owner, repoName] = repo.split("/");

  core.info(`Updating comment ${commentId} body with resolved temporary IDs`);

  const body = sanitizeSyntheticUpdateBody(updatedBody, allowedAliases, maxMentions, context);

  if (isDiscussion) {
    // For discussion comments, we need to use GraphQL
    // Get the comment node ID first
    const mutation = `
      mutation($commentId: ID!, $body: String!) {
        updateDiscussionComment(input: {commentId: $commentId, body: $body}) {
          comment {
            id
          }
        }
      }
    `;

    await github.graphql(mutation, {
      commentId,
      body,
    });
  } else {
    // For issue/PR comments, use REST API
    await github.rest.issues.updateComment({
      owner,
      repo: repoName,
      comment_id: commentId,
      body,
    });
  }

  core.info(`✓ Updated comment ${commentId}`);
}

/**
 * Sanitize synthetic content and restore trusted workflow grouping markers.
 * @param {string} body
 * @param {string[]} allowedAliases
 * @param {number} maxMentions
 * @param {any} ctx
 * @returns {string}
 */
function sanitizeSyntheticUpdateBody(body, allowedAliases, maxMentions, ctx) {
  const sanitizedBody = sanitizeContent(body, { allowedAliases, maxMentions });
  const children = parseReportMarkdown(stripExecutionProvenance(sanitizedBody), { minimumHeadingLevel: 2, allowMetadata: true, allowedMentions: allowedAliases, agentTainted: true });
  if (process.env.GH_AW_WORKFLOW_ID) children.push({ type: "xml_marker", name: "gh-aw-workflow-id", value: process.env.GH_AW_WORKFLOW_ID, agentTainted: false });
  if (process.env.GH_AW_CALLER_WORKFLOW_ID) children.push({ type: "xml_marker", name: "gh-aw-workflow-call-id", value: process.env.GH_AW_CALLER_WORKFLOW_ID, agentTainted: false });
  children.push(...parseReportMarkdown(appendExecutionProvenance("", process.env.GH_AW_WORKFLOW_NAME || "Workflow", buildWorkflowRunUrl(ctx, ctx.repo)), { allowMetadata: true, agentTainted: false }));
  return renderSizedReport({ type: "report", title: "Updated body", children }).body;
}

/**
 * Process synthetic updates by directly updating the body of outputs with resolved temporary IDs
 * Does not use safe output handlers - directly calls GitHub API to update content
 * @param {any} github - GitHub API client
 * @param {any} context - GitHub Actions context
 * @param {Array<{type: string, message: any, result: any, originalTempIdMapSize: number}>} trackedOutputs - Outputs that need updating
 * @param {Map<string, {repo: string, number: number}>} temporaryIdMap - Current temporary ID map
 * @param {Map<string, string>} [artifactUrlMap] - Optional artifact URL map for resolving artifact references
 * @param {any} [mentionsConfig] - Safe-output mention policy
 * @param {number} [maxMentions] - Maximum allowed mentions
 * @returns {Promise<number>} Number of successful updates
 */
async function processSyntheticUpdates(github, context, trackedOutputs, temporaryIdMap, artifactUrlMap, mentionsConfig = {}, maxMentions = 50, /** @type {Function|null} */ authorizeUpdate = null) {
  if (readClaimScopeContext() && !currentClaimHandle()) throw new Error("Synthetic queue writes require a trusted per-Claim execution context");
  if (!authorizeUpdate && process.env.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG) {
    const config = loadConfig();
    if (trackedOutputs.some(tracked => effectiveAuthorization(config, tracked.type, tracked.message))) throw new Error("Synthetic safe-output updates require their trusted authorization context");
  }
  let updateCount = 0;

  core.info(`\n=== Processing Synthetic Updates ===`);
  core.info(`Found ${trackedOutputs.length} output(s) with unresolved temporary IDs`);

  for (const tracked of trackedOutputs) {
    await assertClaimAuthorized({ ...tracked.message, repo: tracked.result.repo, item_number: tracked.result.number });
    // Check if any new temporary IDs were resolved since this output was created.
    // Also trigger an update when artifact URLs have been registered (artifactUrlMap is non-empty),
    // since artifact IDs embedded in the body need to be replaced with their real URLs.
    const resolvedArtifacts = artifactUrlMap && artifactUrlMap.size > 0;
    if (temporaryIdMap.size > tracked.originalTempIdMapSize || resolvedArtifacts) {
      const contentToCheck = getContentToCheck(tracked.type, tracked.message, tracked.result);

      // Only process if we have content to check
      if (contentToCheck !== null && contentToCheck !== "") {
        // Check if the content still has unresolved IDs (some may now be resolved)
        const stillHasUnresolved = hasUnresolvedTemporaryIds(contentToCheck, temporaryIdMap, artifactUrlMap);
        const resolvedCount = temporaryIdMap.size - tracked.originalTempIdMapSize;

        if (!stillHasUnresolved) {
          // All temporary IDs are now resolved - update the body directly
          let logInfo = tracked.result.commentId ? `comment ${tracked.result.commentId} on ${tracked.result.repo}#${tracked.result.itemNumber}` : `${tracked.result.repo}#${tracked.result.number}`;
          core.info(`Updating ${tracked.type} ${logInfo} (${resolvedCount} temp ID(s) resolved)`);

          try {
            const stalePRReason = await getStalePullRequestReason(github, context);
            if (stalePRReason) {
              core.info(stalePRReason);
              continue;
            }
            const targetRepo = parseRepoSlug(tracked.result.repo);
            const allowedAliases = targetRepo ? await resolveAllowedMentionsFromPayload(context, github, core, mentionsConfig, undefined, targetRepo) : [];
            // Replace artifact URL references first, then issue number references
            let updatedContent = replaceArtifactUrlReferences(contentToCheck, artifactUrlMap);
            updatedContent = replaceTemporaryIdReferences(updatedContent, temporaryIdMap, tracked.result.repo);
            // Update based on the original type
            const performUpdate = async () => {
              switch (tracked.type) {
                case "create_issue":
                  await updateIssueBody(github, context, tracked.result.repo, tracked.result.number, updatedContent, allowedAliases, maxMentions);
                  updateCount++;
                  break;
                case "create_discussion":
                  await updateDiscussionBody(github, context, tracked.result.repo, tracked.result.number, updatedContent, allowedAliases, maxMentions);
                  updateCount++;
                  break;
                case "add_comment":
                  // Update comment using the tracked comment ID
                  if (tracked.result.commentId) {
                    await updateCommentBody(github, context, tracked.result.repo, tracked.result.commentId, updatedContent, tracked.result.isDiscussion, allowedAliases, maxMentions);
                    updateCount++;
                  } else {
                    core.debug(`Skipping synthetic update for comment - comment ID not tracked`);
                  }
                  break;
                case "comment_memory":
                  if (tracked.result.commentId) {
                    await updateCommentBody(github, context, tracked.result.repo, tracked.result.commentId, updatedContent, false, allowedAliases, maxMentions);
                    updateCount++;
                  } else {
                    core.debug(`Skipping synthetic update for comment_memory - comment ID not tracked`);
                  }
                  break;
                case "create_pull_request":
                  await updatePullRequestBody(github, context, tracked.result.repo, tracked.result.number, updatedContent, allowedAliases, maxMentions);
                  updateCount++;
                  break;
                default:
                  core.debug(`Unknown output type: ${tracked.type}`);
              }
            };
            if (authorizeUpdate) {
              await authorizeUpdate(tracked, { body: updatedContent, repo: tracked.result.repo, item_number: tracked.result.number ?? tracked.result.itemNumber, comment_id: tracked.result.commentId ?? null }, performUpdate);
            } else {
              await performUpdate();
            }
          } catch (error) {
            core.warning(`✗ Failed to update ${tracked.type} ${tracked.result.repo}#${tracked.result.number}: ${getErrorMessage(error)}`);
          }
        } else {
          core.debug(`Output ${tracked.result.repo}#${tracked.result.number} still has unresolved temporary IDs`);
        }
      }
    }
  }

  if (updateCount > 0) {
    core.info(`Completed ${updateCount} synthetic update(s)`);
  } else {
    core.info(`No synthetic updates needed`);
  }

  return updateCount;
}

/**
 * Best-effort write of the structured safe-output failure report.
 *
 * The report is uploaded with the safe-outputs-items artifact (which uses
 * `if: always()`), so a failing "Process Safe Outputs" step leaves behind
 * machine-readable diagnostics even after the job logs expire. Writing the
 * report must never mask the original failure, so errors are swallowed.
 *
 * @param {{errorCode?: string, message?: string, failures?: Array<{type?: string, errorCode?: string, error?: string}>}} report
 */
function recordSafeOutputFailure(report) {
  try {
    writeSafeOutputErrorReport({ status: "failure", ...report });
  } catch (error) {
    core.warning(`Failed to write safe output error report: ${getErrorMessage(error)}`);
  }
}

/**
 * Whether a fatal failure is a rate-limit failure that exhausted its retries.
 * Rate-limited requests are rejected before any side effect is applied, so replaying them is safe.
 * Manager-classified failures (dependency, budget, threat, claim) carry an errorCode and are never replayable.
 * @param {any} result
 * @returns {boolean}
 */
function isReplayableSafeOutputFailure(result) {
  if (result?.errorCode || result?.result?.operation_outcome === "indeterminate") {
    return false;
  }
  const error = typeof result?.error === "string" ? result.error : "";
  return /^Retryable: true$/m.test(error) && (/\bRATE_LIMIT_EXCEEDED\b/.test(error) || isRateLimitError(error));
}

/**
 * Resolve the `[HOST/]OWNER/REPO` selector and run URL for the current run.
 * @param {any} ctx
 * @returns {{repoSelector: string, runUrl: string} | null}
 */
function resolveReplayTarget(ctx) {
  let repo;
  try {
    repo = ctx.repo;
  } catch {
    repo = undefined;
  }
  const [envOwner = "", envRepo = ""] = (process.env.GITHUB_REPOSITORY || "").split("/");
  const owner = repo?.owner || envOwner;
  const name = repo?.repo || envRepo;
  if (!owner || !name) {
    return null;
  }
  const serverUrl = ctx.serverUrl || process.env.GITHUB_SERVER_URL || "https://github.com";
  let host;
  try {
    host = new URL(serverUrl).host;
  } catch {
    return null;
  }
  const repoSelector = host === "github.com" ? `${owner}/${name}` : `${host}/${owner}/${name}`;
  return { repoSelector, runUrl: buildWorkflowRunUrl({ ...ctx, serverUrl }, { owner, repo: name }) };
}

/**
 * Log a command that replays this run's safe outputs through the Agentic Maintenance workflow.
 * Replay re-applies every safe output in the run, so the hint is only logged when every fatal
 * failure is a rate-limit failure and no output was applied (or delegated to another job).
 * @param {Array<any>} results - Processing results for this run
 */
function logSafeOutputReplayHint(results) {
  const ctx = typeof context === "undefined" ? undefined : context;
  if (!ctx?.runId || !Array.isArray(results)) {
    return;
  }
  const { fatalFailures } = partitionFailureResults(results);
  if (fatalFailures.length === 0 || !fatalFailures.every(isReplayableSafeOutputFailure)) {
    return;
  }
  if (results.some(r => ["success", "warning", "delegated"].includes(classifySafeOutputResult(r)))) {
    return;
  }
  const target = resolveReplayTarget(ctx);
  if (!target) {
    return;
  }
  core.info(
    `The failure was a transient rate limit and no safe output was applied. Replay this run's safe outputs with:\n` +
      `  gh workflow run agentics-maintenance.yml --repo ${target.repoSelector} -f operation=safe_outputs -f run_url=${target.runUrl}\n` +
      `Requires the Agentic Maintenance workflow. See https://github.github.com/gh-aw/reference/safe-outputs/#replaying-safe-outputs`
  );
}

/**
 * Output types a Work's effect_contract does not declare (none when it declares no outputs list).
 * @param {any} work
 * @param {any[]} messages
 * @returns {string[]}
 */
function undeclaredOutputTypes(work, messages) {
  const contract = work?.effect_contract;
  if (!contract || typeof contract !== "object" || !Array.isArray(contract.outputs)) return [];
  const known = new Set(contract.outputs.map(output => normalizeSafeOutputName(String(output?.type))));
  return [...new Set(messages.map(message => normalizeSafeOutputName(message.type)).filter(type => !known.has(type) && !(contract.no_writes === true && NO_WRITE_TYPES.has(type))))];
}

/**
 * Main entry point for the handler manager
 * This is called by the consolidated safe output step
 *
 * @param {{claimPartition?: boolean, messages?: object[], queueClient?: object, authorize?: (request: Record<string, unknown>) => unknown, activationCommentLinks?: string[]}} [options]
 */
async function main(options = {}) {
  const activationCommentLinks = options.activationCommentLinks ?? [];
  const scope = readClaimScopeContext();
  if (scope && !options.claimPartition) {
    if (!scope.assignment) {
      const output = loadAgentOutput({ partitioning: true });
      if (output.success && output.items.length) throw new Error("Unassigned queue dispatcher cannot execute worker safe outputs");
      return undefined;
    }
    const output = loadAgentOutput({ partitioning: true });
    const messages = output.success ? output.items : [];
    const partitions = new Map(scope.assignment.claims.map(claim => [claim.handle, []]));
    const sourceDigest = require("./safe_output_authorization.cjs").canonicalDigest(JSON.parse(JSON.stringify(messages)));
    for (const partition of partitions.values()) authorizationBatchContexts.set(partition, { digest: sourceDigest, indices: [] });
    const rejected = [];
    for (const [sourceIndex, message] of messages.entries()) {
      try {
        if (message._claimScopeError) throw new Error(message._claimScopeError);
        const normalized = normalizeClaimScope(message, scope.assignment);
        const partition = partitions.get(normalized.claim_handle);
        partition.push(normalized);
        authorizationBatchContexts.get(partition).indices.push(sourceIndex);
      } catch (error) {
        rejected.push({ success: false, type: message.type, claim_handle: message.claim_handle, error: getErrorMessage(error), errorCode: message._claimScopeErrorCode || error.code || "claim_scope_invalid" });
      }
    }
    rejectCrossClaimTemporaryReferences(partitions, rejected);
    const settlements = [];
    const queueClient = options.queueClient || github;
    for (const [handle, scopedMessages] of partitions) {
      /** @type {Array<Record<string, any>>} */
      const effects = [];
      try {
        const unscopedGithub = github;
        const authorize = options.authorize || (request => require("./finish_work_queue_claim.cjs").authorizeWorkerClaim({ ...request, github: unscopedGithub, queueClient }));
        const undeclared = undeclaredOutputTypes(scope.assignment.claims.find(claim => claim.handle === handle).work, scopedMessages);
        if (undeclared.length > 0 && !isStagedMode()) {
          core.warning(`Claim ${handle}: refusing ${scopedMessages.length} scoped output(s); undeclared type(s): ${undeclared.join(", ")}`);
          settlements.push({ claim_handle: handle, success: false, error: `Undeclared output type(s) in Work effect_contract: ${undeclared.join(", ")}` });
          continue;
        }
        const settlement = await withClaimExecution({ ...scope, claim_handle: handle, authorize, effects }, () =>
          withClaimEffectClients({ claim_handle: handle, authorize, authorizeGithub: unscopedGithub, context }, async () => {
            try {
              await assertClaimAuthorized({ type: "work_queue_effect_pass", claim_handle: handle }, { requireCompletion: !isStagedMode() });
            } catch (error) {
              if (error.suppressed) {
                core.info(`Claim ${handle}: ${scopedMessages.length} scoped output(s) suppressed (${error.state})`);
                return { success: true, suppressed: true, state: error.state };
              }
              return { success: false, error: getErrorMessage(error) };
            }
            if (isStagedMode()) {
              core.info(`Read-only preview for Claim ${handle}: ${scopedMessages.length} scoped output(s); no handlers or queue writes`);
              return { success: true, messages: scopedMessages };
            }
            return (await main({ ...options, queueClient, authorize, claimPartition: true, messages: scopedMessages, activationCommentLinks })) ?? { success: false };
          })
        );
        settlements.push({ claim_handle: handle, ...settlement });
      } catch (error) {
        settlements.push({ claim_handle: handle, success: false, error: getErrorMessage(error) });
      } finally {
        if (!isStagedMode()) appendClaimEffects(effects);
      }
    }
    core.setOutput("claim_results", JSON.stringify({ settlements }));
    if (rejected.length) {
      core.setOutput("claim_scope_errors", JSON.stringify(rejected));
      core.setFailed(`${rejected.length} safe-output message(s) have invalid Claim scope`);
    }
    if (settlements.some(settlement => settlement.success === false)) {
      core.setFailed("One or more Claim-scoped safe-output passes failed; independently valid siblings were processed");
    }
    return { settlements, rejected };
  }
  // Detect staged mode before try/finally so it's accessible in the finally block.
  // In staged mode (🎭 Staged Mode Preview) no real items are created in GitHub so no manifest should be emitted.
  const isStaged = isStagedMode();
  /** @type {string | null} */
  let failedOutputsMessage = null;
  /** @type {Array<{type?: string, errorCode?: string, error?: string}>} */
  let failureDetails = [];
  let statusOutputsSet = false;
  /** @type {any[]} */
  let outcomeResults = [];

  try {
    core.info("Safe Output Handler Manager starting...");

    // Load configuration
    const config = loadConfig();
    core.debug(`Configuration: ${JSON.stringify(Object.keys(config))}`);

    // Load agent output
    /** @type {any} */
    const agentOutput = options.messages ? { success: true, items: options.messages } : loadAgentOutput();
    const agentOutputItems = agentOutput.success ? agentOutput.items : [];
    if (!agentOutput.success) {
      core.info("No agent output available from tool calls");
    } else {
      core.info(`Found ${agentOutput.items.length} message(s) in agent output`);
    }

    const fileBackedCommentMemoryMessages = currentClaimHandle() ? [] : buildCommentMemoryMessagesFromFiles(agentOutputItems, config);
    const allMessages = [...agentOutputItems, ...fileBackedCommentMemoryMessages];
    const sourceBatch = options.messages && authorizationBatchContexts.get(options.messages);
    if (sourceBatch) {
      authorizationBatchContexts.set(allMessages, sourceBatch);
    } else if (hasAuthorization(config)) {
      const { canonicalDigest } = require("./safe_output_authorization.cjs");
      const digest = canonicalDigest(JSON.parse(JSON.stringify(agentOutputItems)));
      const derivedDigest = fileBackedCommentMemoryMessages.length ? canonicalDigest(JSON.parse(JSON.stringify(allMessages))) : digest;
      authorizationBatchContexts.set(allMessages, { digest, indices: allMessages.map((_, index) => index), digests: allMessages.map((_, index) => (index < agentOutputItems.length ? digest : derivedDigest)) });
    }
    if (currentClaimHandle()) setCollectedMissings({ missingTools: [], missingData: [], noopMessages: [], reportIncomplete: [] });
    if (allMessages.length === 0) {
      core.info("No safe-output messages available - nothing to process");
      await finalizeLedgerAppend(config, new Map());
      if (!isStaged) ensureManifestExists();
      core.setOutput("processed_count", "0");
      setSafeOutputsStatusOutputs({ itemsSucceeded: 0, itemsFailed: 0, status: "success" });
      statusOutputsSet = true;
      return undefined;
    }

    // Create the PR review buffer registry (one per-PR buffer created on demand)
    const prReviewBufferRegistry = createPrReviewBufferRegistry();

    // Apply footer config with priority:
    // 1. submit_pull_request_review.footer (highest priority — footer controls review body)
    // 2. Default: "always"
    /** @type {any} */
    let footerConfig = undefined;
    if (config.submit_pull_request_review?.footer !== undefined) {
      footerConfig = config.submit_pull_request_review.footer;
      core.info(`Using footer config from submit_pull_request_review: ${footerConfig}`);
    }

    if (footerConfig !== undefined) {
      prReviewBufferRegistry.setDefaultFooterMode(footerConfig);
    }

    const maxMentions = parseIntTemplatable(config.mentions?.max, 50);

    // Load and initialize handlers based on configuration (factory pattern)
    const messageHandlers = await loadHandlers(config, prReviewBufferRegistry, undefined, activationCommentLinks);

    if (messageHandlers.size === 0) {
      const delegatedTypes = new Set([...STANDALONE_STEP_TYPES, ...loadCustomSafeOutputJobTypes()]);
      outcomeResults = allMessages.map((message, messageIndex) => ({
        type: message.type,
        messageIndex,
        ...(message.output_id !== undefined ? { output_id: message.output_id } : {}),
        skipped: true,
        delegated: delegatedTypes.has(message.type),
      }));
      core.info("No handlers loaded - nothing to process");
      // Ensure manifest file exists even when no handlers are loaded (skip in staged mode)
      if (!isStaged) ensureManifestExists();
      core.setOutput("processed_count", "0");
      setSafeOutputsStatusOutputs({ itemsSucceeded: 0, itemsFailed: 0, status: "success" });
      statusOutputsSet = true;
      return undefined;
    }

    // Create manifest logger for recording created items.
    // createManifestLogger() touches the file immediately so it exists for artifact upload.
    // In staged mode, pass null so no items are logged (nothing is actually created).
    const logCreatedItem = isStaged ? null : createManifestLogger();

    // Pre-check: log a warning when the installation token's rate-limit headroom is low.
    // This surfaces quota pressure before writes start so it is visible in the job log
    // even if no individual write fails.  The check is best-effort – failures are non-fatal.
    await checkRateLimitHeadroom(github, "safe_outputs_pre_check");

    // Finalize buffered PR reviews before releasing their dependent outputs.
    const finalizedReviewResults = new Set();
    /** @param {Array<any>} results @param {Set<string>} protectedTargets @param {Map<string, any[]>} requiredComments */
    async function finalizeBufferedReviews(results, protectedTargets, requiredComments) {
      const registryEntries = prReviewBufferRegistry.drainEntries(protectedTargets);
      for (const { repo: reviewRepo, prNumber: reviewPrNum, buffer: reviewBuffer } of registryEntries) {
        const reviewResults = results.filter(record => record.result?.deferred_manifest && !finalizedReviewResults.has(record) && record.result.repo === reviewRepo && record.result.pull_request_number === reviewPrNum);
        for (const record of reviewResults) finalizedReviewResults.add(record);
        if (reviewBuffer.hasBufferedComments() || reviewBuffer.hasReviewMetadata()) {
          core.info(`\n=== Finalizing PR Review for ${reviewRepo}#${reviewPrNum} ===`);
          const bufferedCount = reviewBuffer.getBufferedCount();
          if (bufferedCount > 0) {
            core.info(`Submitting ${bufferedCount} buffered review comment(s) for ${reviewRepo}#${reviewPrNum}`);
          } else {
            core.info(`Submitting PR review for ${reviewRepo}#${reviewPrNum} (body-only, no inline comments)`);
          }
          /** @type {any} */
          let reviewFailureError = null;
          try {
            await assertClaimAuthorized({ type: "submit_pull_request_review", claim_handle: currentClaimHandle(), repo: reviewRepo, pull_request_number: reviewPrNum });
            const stalePRReason = await getStalePullRequestReason(github, context);
            if (stalePRReason) {
              core.info(stalePRReason);
              skipReviewResultsForPR(reviewResults, reviewRepo, reviewPrNum, stalePRReason);
              continue;
            }
            const reviewResult = await reviewBuffer.submitReview(requiredComments.get(`${reviewRepo}#${reviewPrNum}`));
            if (reviewResult.success && !reviewResult.skipped) {
              finalizeBufferedReviewExecutions(reviewResults, reviewRepo, reviewPrNum, reviewResult);
              logCreatedItemFromResult(logCreatedItem, "submit_pull_request_review", reviewResult);
              logCreatedItemFromResult(
                logCreatedItem,
                "create_pull_request_review_comment",
                reviewResult.review_comments?.map(comment => (comment && typeof comment === "object" ? { ...comment, success: true } : comment))
              );
              core.info(`✓ PR review submitted for ${reviewRepo}#${reviewPrNum}: ${reviewResult.review_url}`);
            } else if (reviewResult.success && reviewResult.skipped) {
              const skipReason = reviewResult.reason || `PR review for ${reviewRepo}#${reviewPrNum} skipped`;
              core.warning(`⚠ ${skipReason}`);
              if (reviewResult.pr_locked) {
                core.setOutput("pr_locked", "true");
              }
              skipReviewResultsForPR(reviewResults, reviewRepo, reviewPrNum, skipReason);
            } else if (!reviewResult.success) {
              reviewFailureError = reviewResult.error || `PR review finalization failed for ${reviewRepo}#${reviewPrNum}`;
              core.error(`✗ Failed to submit PR review for ${reviewRepo}#${reviewPrNum}: ${reviewFailureError}`);
            }
          } catch (reviewError) {
            reviewFailureError = getErrorMessage(reviewError);
            core.error(`✗ Exception while submitting PR review for ${reviewRepo}#${reviewPrNum}: ${reviewFailureError}`);
          }

          if (reviewFailureError !== null) {
            rollbackReviewResultsForPR(reviewResults, reviewRepo, reviewPrNum, reviewFailureError);
          }
        }
      }
    }

    // Process all messages in order of appearance
    const processingResult = await processMessages(messageHandlers, allMessages, logCreatedItem, finalizeBufferedReviews);
    outcomeResults = processingResult.results;
    await finalizeLedgerAppend(config, messageHandlers);

    // Store collected missings in helper module for handlers to access
    if (processingResult.missings) {
      setCollectedMissings(processingResult.missings);
      core.info(
        `Stored ${processingResult.missings.missingTools.length} missing tool(s), ${processingResult.missings.missingData.length} missing data item(s), ${processingResult.missings.noopMessages.length} noop message(s), and ${processingResult.missings.reportIncomplete.length} incomplete signal(s) for footer generation`
      );
    }

    // Process synthetic updates by directly updating issue/discussion bodies
    let syntheticUpdateCount = 0;
    if (processingResult.outputsWithUnresolvedIds && processingResult.outputsWithUnresolvedIds.length > 0) {
      // Convert temp ID map back to Map
      const temporaryIdMap = new Map(Object.entries(processingResult.temporaryIdMap));

      syntheticUpdateCount = await processSyntheticUpdates(
        github,
        context,
        processingResult.outputsWithUnresolvedIds,
        temporaryIdMap,
        processingResult.artifactUrlMap,
        config.mentions ?? {},
        maxMentions,
        processingResult.authorizeSyntheticUpdate
      );
    }

    // Write step summaries for all processed safe-outputs
    await writeSafeOutputSummaries(processingResult.results, allMessages);

    // Log summary
    const safeOutputsStatus = computeSafeOutputsStatus(processingResult.results);
    const successCount = safeOutputsStatus.itemsSucceeded;
    const { fatalFailures, reportOnlyFailures } = partitionFailureResults(processingResult.results);
    const failureCount = fatalFailures.length;
    const reportOnlyFailureCount = reportOnlyFailures.length;
    const cancelledCount = processingResult.results.filter(r => r.cancelled).length;
    const deferredCount = processingResult.results.filter(r => r.deferred).length;
    const skippedStandaloneResults = processingResult.results.filter(r => r.delegated && r.reason === "Handled by standalone step");
    const skippedCustomJobResults = processingResult.results.filter(r => r.delegated && r.reason === "Handled by custom safe output job");
    const skippedNoHandlerResults = processingResult.results.filter(r => !r.success && !r.skipped && r.error?.includes("No handler loaded"));
    const skippedHandlerResults = processingResult.results.filter(r => classifySafeOutputResult(r) === "skipped");

    core.info(`\n=== Processing Summary ===`);
    core.info(`Total messages: ${processingResult.results.length}`);
    core.info(`Status: ${safeOutputsStatus.status}`);
    core.info(`Successful: ${successCount}`);
    core.info(`Failed: ${safeOutputsStatus.itemsFailed}`);
    if (reportOnlyFailureCount > 0) {
      core.info(`Reported assignment failures: ${reportOnlyFailureCount}`);
    }
    if (cancelledCount > 0) {
      core.info(`Cancelled: ${cancelledCount}`);
    }
    if (deferredCount > 0) {
      core.info(`Deferred: ${deferredCount}`);
    }
    if (skippedHandlerResults.length > 0) {
      core.info(`Skipped (no context or limit reached): ${skippedHandlerResults.length}`);
      const skippedHandlerTypes = [...new Set(skippedHandlerResults.map(r => r.type))];
      core.info(`  Types: ${skippedHandlerTypes.join(", ")}`);
    }
    if (skippedStandaloneResults.length > 0) {
      core.info(`Skipped (standalone step): ${skippedStandaloneResults.length}`);
      const standaloneTypes = [...new Set(skippedStandaloneResults.map(r => r.type))];
      core.info(`  Types: ${standaloneTypes.join(", ")}`);
    }
    if (skippedCustomJobResults.length > 0) {
      core.info(`Skipped (custom safe output job): ${skippedCustomJobResults.length}`);
      const customJobTypesList = [...new Set(skippedCustomJobResults.map(r => r.type))];
      core.info(`  Types: ${customJobTypesList.join(", ")}`);
    }
    if (skippedNoHandlerResults.length > 0) {
      core.warning(`Skipped (no handler): ${skippedNoHandlerResults.length}`);
      const noHandlerTypes = [...new Set(skippedNoHandlerResults.map(r => r.type))];
      core.info(`  Types: ${noHandlerTypes.join(", ")}`);
    }
    core.info(`Temporary IDs registered: ${Object.keys(processingResult.temporaryIdMap).length}`);
    core.info(`Synthetic updates: ${syntheticUpdateCount}`);

    if (failureCount > 0) {
      core.warning(`${failureCount} message(s) failed to process`);
      logSafeOutputReplayHint(processingResult.results);
      const failedItemLines = fatalFailures.map(r => `  - ${r.type}: ${r.error || "Unknown error"}`);
      const failedItems = failedItemLines.join("\n");
      failedOutputsMessage = `${failureCount} safe output(s) failed:\n${failedItems}`;
      failureDetails = fatalFailures.map(r => ({
        type: r.type,
        ...(r.errorCode ? { errorCode: r.errorCode } : {}),
        error: r.error || "Unknown error",
      }));
    }
    if (reportOnlyFailureCount > 0) {
      const reportOnlyTypes = [...new Set(reportOnlyFailures.map(r => r.type || "unknown"))];
      core.warning(`${reportOnlyFailureCount} non-fatal safe output(s) failed but were reported without failing safe_outputs: ${reportOnlyTypes.join(", ")}`);
    }
    if (cancelledCount > 0) {
      core.warning(`${cancelledCount} message(s) were cancelled`);
    }
    if (skippedNoHandlerResults.length > 0) {
      core.warning(`${skippedNoHandlerResults.length} message(s) were skipped because no handler was loaded. Check your workflow's safe-outputs configuration.`);
    }

    // Write temporary ID map to file for inclusion in the safe-outputs-items artifact.
    // This allows reviewers and auditors to inspect the full map of temporary IDs
    // to resolved GitHub resources (issue numbers, repos) without parsing step outputs.
    if (!isStaged) {
      writeTemporaryIdMapFile(processingResult.temporaryIdMap);
      core.info(`Wrote temporary ID map to file for artifact upload`);
    }

    // Export processed count for consistency with project handler
    core.setOutput("processed_count", String(successCount));
    setSafeOutputsStatusOutputs(safeOutputsStatus);
    statusOutputsSet = true;

    // Export assign_to_agent outputs when the handler was loaded
    if (messageHandlers.has("assign_to_agent")) {
      const assignToAgentHandler = messageHandlers.get("assign_to_agent");
      const assignToAgentAssigned = getAssignToAgentAssigned(assignToAgentHandler);
      const assignToAgentErrors = getAssignToAgentErrors(assignToAgentHandler);
      const assignToAgentErrorCount = getAssignToAgentErrorCount(assignToAgentHandler);
      core.setOutput("assign_to_agent_assigned", assignToAgentAssigned);
      core.setOutput("assign_to_agent_assignment_errors", assignToAgentErrors);
      core.setOutput("assign_to_agent_assignment_error_count", assignToAgentErrorCount.toString());
      if (assignToAgentErrorCount > 0) {
        core.warning(`${assignToAgentErrorCount} agent assignment(s) failed`);
      }
      core.info(`Exported assign_to_agent outputs (${assignToAgentErrorCount} error(s))`);
      await writeAssignToAgentSummary(assignToAgentHandler);
    }

    // Export create_agent_session outputs when the handler was loaded
    if (messageHandlers.has("create_agent_session")) {
      /** @type {any} */
      const createAgentSessionHandler = messageHandlers.get("create_agent_session");
      const sessionNumber = createAgentSessionHandler.getSessionNumber();
      const sessionUrl = createAgentSessionHandler.getSessionUrl();
      core.setOutput("session_number", sessionNumber);
      core.setOutput("session_url", sessionUrl);
      core.info(`Exported create_agent_session outputs (session_number=${sessionNumber})`);
      await createAgentSessionHandler.writeSummary();
    }

    // Export create_discussion errors for conclusion job
    // Exclude cancelled results (cancelled == the discussion was skipped because a code-push
    // operation failed earlier in the same run; that failure is already reported separately).
    const createDiscussionErrors = processingResult.results
      .filter(r => r.type === "create_discussion" && !r.success && !r.deferred && !r.skipped && !r.cancelled)
      .map((r, index) => {
        const message = allMessages[r.messageIndex];
        const title = message?.title || "Discussion";
        const repo = message?.repo || process.env.GITHUB_REPOSITORY || "unknown";
        const errorMsg = r.error || r.reason || "Unknown error";
        return `discussion:${index}:${repo}:${title}:${errorMsg}`;
      })
      .join("\n");

    const createDiscussionErrorCount = processingResult.results.filter(r => r.type === "create_discussion" && !r.success && !r.deferred && !r.skipped && !r.cancelled).length;

    core.setOutput("create_discussion_errors", createDiscussionErrors);
    core.setOutput("create_discussion_error_count", createDiscussionErrorCount.toString());

    if (createDiscussionErrorCount > 0) {
      core.info(`Exported ${createDiscussionErrorCount} create_discussion error(s)`);
    }

    // Export code-push failure outputs for conclusion job
    const codePushFailureErrors = (processingResult.codePushFailures || []).map(f => `${f.type}:${f.error}`).join("\n");
    const codePushFailureCount = (processingResult.codePushFailures || []).length;

    core.setOutput("code_push_failure_errors", codePushFailureErrors);
    core.setOutput("code_push_failure_count", codePushFailureCount.toString());

    if (codePushFailureCount > 0) {
      core.info(`Exported ${codePushFailureCount} code push failure(s)`);
    }

    // Emit named action outputs (e.g. created_issue_number, created_issue_url)
    // for the first successful result of each safe output type.
    emitSafeOutputActionOutputs(processingResult);

    // Ensure the manifest file always exists for artifact upload (even if no items were created).
    // Skip in staged mode — no real items were created so no manifest should be emitted.
    // Note: createManifestLogger() also calls ensureManifestExists() when the logger is created,
    // so this is a safety net for cases where we never reached the logger creation.
    if (!isStaged) ensureManifestExists();

    if (failedOutputsMessage !== null) {
      recordSafeOutputFailure({ errorCode: SAFE_OUTPUT_E099, message: failedOutputsMessage, failures: failureDetails });
      core.setFailed(failedOutputsMessage);
      return { success: false };
    }
    core.info("Safe Output Handler Manager completed");
    return { success: true };
  } catch (error) {
    const handlerError = `${ERR_VALIDATION}: Handler manager failed: ${getErrorMessage(error)}`;
    if (!statusOutputsSet) {
      setSafeOutputsStatusOutputs({ itemsSucceeded: 0, itemsFailed: 0, status: "failure" });
    }
    if (failedOutputsMessage !== null) {
      recordSafeOutputFailure({ errorCode: ERR_VALIDATION, message: `${failedOutputsMessage}\n${handlerError}`, failures: failureDetails });
      core.setFailed(`${failedOutputsMessage}\n${handlerError}`);
      return undefined;
    }
    recordSafeOutputFailure({ errorCode: ERR_VALIDATION, message: handlerError, failures: failureDetails });
    core.setFailed(handlerError);
    return undefined;
  } finally {
    try {
      writeSafeOutputExecutions(outcomeResults, isStaged);
    } catch {
      core.warning("Safe-output application outcomes could not be persisted");
    }
    // Guarantee the manifest file exists for artifact upload even when the handler fails.
    // This is a no-op if the file was already created by createManifestLogger().
    if (!isStaged) {
      try {
        ensureManifestExists();
      } catch (_e) {
        // Ignore errors here — we must not mask the original failure
      }
    }
  }
}

module.exports = {
  main,
  logSafeOutputReplayHint,
  loadConfig,
  loadHandlers,
  normalizeInvocationInputs,
  processMessages,
  sortMessagesByTemporaryIdDependencies,
  sortMessageIndicesByTemporaryIdDependencies,
  buildCommentMemoryMessagesFromFiles,
  rollbackReviewResults,
  rollbackReviewResultsForPR,
  skipReviewResults,
  skipReviewResultsForPR,
  logCreatedItemFromResult,
  isFailedProcessingResult,
  isReportOnlyFailureResult,
  partitionFailureResults,
  computeSafeOutputsStatus,
  setSafeOutputsStatusOutputs,
  processSyntheticUpdates,
  rejectCrossClaimTemporaryReferences,
};
