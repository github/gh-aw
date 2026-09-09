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
const { getErrorMessage } = require("./error_helpers.cjs");
const { ERR_CONFIG, ERR_PARSE, ERR_VALIDATION, SAFE_OUTPUT_E099 } = require("./error_codes.cjs");
const { classifySafeOutputResult, computeSafeOutputsStatus, isFailedProcessingResult } = require("./safe_outputs_status.cjs");
const { hasUnresolvedTemporaryIds, replaceTemporaryIdReferences, replaceArtifactUrlReferences, normalizeTemporaryId, extractTemporaryIdReferences, getCreatedTemporaryId } = require("./temporary_id.cjs");
const { generateMissingInfoSections } = require("./missing_info_formatter.cjs");
const { setCollectedMissings } = require("./missing_messages_helper.cjs");
const { writeSafeOutputSummaries } = require("./safe_output_summary.cjs");
const { getAssignToAgentAssigned, getAssignToAgentErrors, getAssignToAgentErrorCount, writeAssignToAgentSummary } = require("./assign_to_agent.cjs");
const { createPrReviewBufferRegistry } = require("./pr_review_buffer.cjs");
const { sanitizeContent } = require("./sanitize_content.cjs");
const { resolveAllowedMentionsFromPayload } = require("./resolve_mentions_from_payload.cjs");
const { parseIntTemplatable } = require("./templatable.cjs");
const { createManifestLogger, ensureManifestExists, extractCreatedItemFromResult, writeTemporaryIdMapFile, writeSafeOutputErrorReport } = require("./safe_output_manifest.cjs");
const { loadCustomSafeOutputJobTypes, loadCustomSafeOutputScriptHandlers, loadCustomSafeOutputActionHandlers, isStagedMode } = require("./safe_output_helpers.cjs");
const { emitSafeOutputActionOutputs } = require("./safe_outputs_action_outputs.cjs");
const { listCommentMemoryFiles, COMMENT_MEMORY_DIR } = require("./comment_memory_helpers.cjs");
const { checkRateLimitHeadroom } = require("./rate_limit_helpers.cjs");
const { redactSensitiveConfig } = require("./safe_outputs_config_redact.cjs");
const nodePath = require("path");
const fs = require("fs");
const GITHUB_TOKEN_CONFIG_KEY = "github-token";

/**
 * Handler map configuration
 * Maps safe output types to their handler module file paths
 */
const HANDLER_MAP = {
  linear_create_issue: "./linear_create_issue.cjs",
  linear_add_comment: "./linear_add_comment.cjs",
  linear_update_issue: "./linear_update_issue.cjs",
  create_issue: "./create_issue.cjs",
  jira_create_issue: "./jira_create_issue.cjs",
  jira_update_issue: "./jira_update_issue.cjs",
  jira_add_comment: "./jira_add_comment.cjs",
  jira_add_label: "./jira_add_label.cjs",
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
  missing_tool: "./missing_tool.cjs",
  create_missing_data_issue: "./create_missing_data_issue.cjs",
  missing_data: "./missing_data.cjs",
  noop: "./noop_handler.cjs",
  report_incomplete: "./report_incomplete_handler.cjs",
  create_report_incomplete_issue: "./create_report_incomplete_issue.cjs",
  create_project: "./create_project.cjs",
  ado_create_work_item: "./create_work_item.cjs",
  ado_update_work_item: "./update_work_item.cjs",
  ado_comment_on_work_item: "./comment_on_work_item.cjs",
  ado_assign_work_item: "./assign_work_item.cjs",
  ado_link_work_items: "./link_work_items.cjs",
  ado_upload_workitem_attachment: "./upload_workitem_attachment.cjs",
  create_project_status_update: "./create_project_status_update.cjs",
  update_project: "./update_project.cjs",
  upload_artifact: "./upload_artifact.cjs",
  upload_code_coverage: "./upload_code_coverage.cjs",
};

/**
 * Message types handled by standalone steps (not through the handler manager)
 * These types should not trigger warnings when skipped by the handler manager
 *
 * Standalone types: upload_asset, noop
 *   - Have dedicated processing steps with specialized logic
 */
const STANDALONE_STEP_TYPES = new Set(["upload_asset", "noop"]);

/**
 * Code-push safe output types that must succeed before remaining outputs are processed.
 * If any of these fail, the remaining non-code-push messages are cancelled with a clear reason.
 */
const CODE_PUSH_TYPES = new Set(["push_to_pull_request_branch", "create_pull_request"]);

/** @type {Set<string>} Project-safe-output handlers that should default to GH_AW_PROJECT_GITHUB_TOKEN when no per-handler github-token is configured. */
const PROJECT_HANDLER_TYPES = new Set(["create_project", "create_project_status_update", "update_project"]);

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
  "linear_create_issue",
  "linear_add_comment",
  "create_issue",
  "jira_create_issue",
  "jira_update_issue",
  "jira_add_comment",
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
  "missing_tool",
  "create_missing_data_issue",
  "missing_data",
  "create_report_incomplete_issue",
  "report_incomplete",
  "ado_create_work_item",
  "ado_comment_on_work_item",
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
  "linear_update_issue",
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
  "jira_add_label",
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
  "call_workflow",
  "autofix_code_scanning_alert",
  "create_agent_session",
  "ado_update_work_item",
  "ado_assign_work_item",
  "ado_link_work_items",
  "ado_upload_workitem_attachment",
]);

/**
 * Resolve threat warning policy for a safe output type.
 * @param {string} messageType
 * @returns {{policy: "reviewable" | "convertible" | "abort" | "none", mappedType?: string}}
 */
function getThreatWarningPolicy(messageType) {
  if (THREAT_WARNING_ABORT_TYPES.has(messageType)) {
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
    return Object.fromEntries(Object.entries(config).map(([k, v]) => [k.replace(/-/g, "_"), v]));
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

/**
 * Load and initialize handlers for enabled safe output types
 * Calls each handler's factory function (main) to get message processors
 * @param {Object} config - Safe outputs configuration
 * @param {Object} prReviewBufferRegistry - PR review buffer registry instance
 * @param {string[]} [resolvedAllowedMentionAliases] - Pre-resolved mention aliases shared across handlers
 * @returns {Promise<Map<string, Function>>} Map of type to message handler function
 */
async function loadHandlers(config, prReviewBufferRegistry, resolvedAllowedMentionAliases = []) {
  const messageHandlers = new Map();

  /** @type {Map<string, string>} */
  const handlerLoadErrors = new Map();
  handlerLoadErrorsByHandlerMap.set(messageHandlers, handlerLoadErrors);

  core.info("Loading and initializing safe output handlers based on configuration...");

  for (const [type, handlerPath] of Object.entries(HANDLER_MAP)) {
    // Check if this safe output type is enabled in the config
    // The presence of the config key indicates the handler should be loaded
    if (config[type]) {
      try {
        const handlerModule = require(handlerPath);
        if (handlerModule && typeof handlerModule.main === "function") {
          // Call the factory function with config to get the message handler
          const handlerConfig = { ...(config[type] || {}) };

          if (PROJECT_HANDLER_TYPES.has(type) && !handlerConfig[GITHUB_TOKEN_CONFIG_KEY] && process.env.GH_AW_PROJECT_GITHUB_TOKEN) {
            handlerConfig[GITHUB_TOKEN_CONFIG_KEY] = process.env.GH_AW_PROJECT_GITHUB_TOKEN;
          }

          // Pass top-level mentions policy through so handlers can preserve
          // the same allowed mention aliases used during collection.
          if (handlerConfig.mentions == null && config.mentions != null) {
            handlerConfig.mentions = config.mentions;
          }
          if (handlerConfig.mentions != null && handlerConfig.allowedMentionAliases == null && Array.isArray(resolvedAllowedMentionAliases)) {
            handlerConfig.allowedMentionAliases = resolvedAllowedMentionAliases;
          }

          // Inject shared PR review buffer registry into handlers that need it
          if (PR_REVIEW_HANDLER_TYPES.has(type)) {
            handlerConfig._prReviewBufferRegistry = prReviewBufferRegistry;
          }

          /** @type {any|null} */
          let handlerGithubClient = null;
          /** @type {any} */
          const globalState = global;
          if (handlerConfig[GITHUB_TOKEN_CONFIG_KEY] && typeof globalState.getOctokit === "function") {
            handlerGithubClient = globalState.getOctokit(handlerConfig[GITHUB_TOKEN_CONFIG_KEY]);
          }
          const messageHandler = await handlerModule.main(handlerConfig, handlerGithubClient);

          if (typeof messageHandler !== "function") {
            // This is a fatal error - the handler is misconfigured
            // Re-throw to fail the step rather than continuing
            const error = new Error(`Handler ${type} main() did not return a function - expected a message handler function but got ${typeof messageHandler}`);
            core.error(`✗ Fatal error loading handler ${type}: ${error.message}`);
            throw error;
          }

          messageHandlers.set(type, wrapWithClientRebinding(type, messageHandler, handlerGithubClient));
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
        handlerLoadErrors.set(type, errorMessage);
        core.warning(`Failed to load handler for ${type}: ${errorMessage}`);
      }
    } else {
      core.debug(`Handler not enabled: ${type}`);
    }
  }

  // Load custom script handlers from GH_AW_SAFE_OUTPUT_SCRIPTS
  // These are inline scripts defined in safe-outputs.scripts that run in the handler loop
  const customScriptHandlers = loadCustomSafeOutputScriptHandlers();
  if (customScriptHandlers.size > 0) {
    core.info(`Loading ${customScriptHandlers.size} custom script handler(s): ${[...customScriptHandlers.keys()].join(", ")}`);
    const scriptBaseDir = nodePath.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw", "actions");
    for (const [scriptType, scriptFilename] of customScriptHandlers) {
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
            messageHandlers.set(scriptType, messageHandler);
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
      try {
        const actionModule = require(actionHandlerPath);
        if (actionModule && typeof actionModule.main === "function") {
          const handlerConfig = { action_name: actionName, ...(config[actionType] || {}) };
          const messageHandler = await actionModule.main(handlerConfig);
          if (typeof messageHandler !== "function") {
            core.warning(`✗ Custom action handler ${actionType} main() did not return a function (got ${typeof messageHandler}) — this handler will be skipped`);
          } else {
            messageHandlers.set(actionType, messageHandler);
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

  core.info(`Loaded ${messageHandlers.size} handler(s)`);
  return messageHandlers;
}

/**
 * Collect missing_tool, missing_data, noop, and report_incomplete messages from the messages array
 * @param {Array<Object>} messages - Array of safe output messages
 * @returns {{missingTools: Array<any>, missingData: Array<any>, noopMessages: Array<any>, reportIncomplete: Array<any>}} Object with collected missing items, noop messages, and incomplete signals
 */
function collectMissingMessages(messages) {
  const missingTools = [];
  const missingData = [];
  const noopMessages = [];
  const reportIncomplete = [];

  for (const message of messages) {
    if (message.type === "missing_tool") {
      // Extract relevant fields from missing_tool message
      if (message.tool && message.reason) {
        missingTools.push({
          tool: message.tool,
          reason: message.reason,
          alternatives: message.alternatives || null,
        });
      }
    } else if (message.type === "missing_data") {
      // Extract relevant fields from missing_data message
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
 * @param {Array<{type: string, success: boolean, skipped?: boolean, skipReason?: string}>} results - Processing results to mutate
 * @param {string} skipReason - Human-readable reason for the skip
 */
function skipReviewResults(results, skipReason) {
  for (const r of results) {
    if ((r.type === "submit_pull_request_review" || r.type === "create_pull_request_review_comment") && r.success === true) {
      r.skipped = true;
      r.skipReason = skipReason;
    }
  }
}

/**
 * Mark buffered review results for a specific PR as skipped.
 *
 * @param {Array<{type: string, success: boolean, skipped?: boolean, skipReason?: string, repo?: string, pull_request_number?: number, result?: {repo?: string, pull_request_number?: number}}>} results
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

/**
 * Compute the processing order (original message indices) so that temporary-ID
 * producers run before consumers while preserving the original order for
 * independent messages and dependency cycles.
 *
 * @param {Array<Record<string, any>>} messages
 * @returns {Array<number>} original message indices in processing order
 */
function sortMessageIndicesByTemporaryIdDependencies(messages) {
  const producers = new Map();
  messages.forEach((message, index) => {
    const temporaryId = getCreatedTemporaryId(message);
    if (temporaryId && !producers.has(temporaryId)) {
      producers.set(temporaryId, index);
    }
  });

  /** @type {Array<Array<number>>} */
  const dependents = messages.map(() => []);
  const inDegree = messages.map(() => 0);
  messages.forEach((message, index) => {
    const dependencies = message.type === "create_issue" ? extractTemporaryIdReferences({ blocked_by: message.blocked_by }) : new Set();
    for (const temporaryId of dependencies) {
      const producerIndex = producers.get(temporaryId);
      if (producerIndex !== undefined && producerIndex !== index) {
        dependents[producerIndex].push(index);
        inDegree[index]++;
      }
    }
  });

  /** @type {Array<number>} */
  const ready = [];
  /** @param {number} index */
  const pushReady = index => {
    // Keep the ready queue ordered by original message index so independent
    // messages keep their original relative order (stable topological sort).
    let position = ready.length;
    while (position > 0 && ready[position - 1] > index) position--;
    ready.splice(position, 0, index);
  };
  inDegree.forEach((degree, index) => {
    if (degree === 0) pushReady(index);
  });
  /** @type {Array<number>} */
  const sorted = [];
  while (ready.length > 0) {
    const index = ready.shift();
    if (index === undefined) break;
    sorted.push(index);
    for (const dependentIndex of dependents[index]) {
      inDegree[dependentIndex]--;
      if (inDegree[dependentIndex] === 0) {
        pushReady(dependentIndex);
      }
    }
  }

  if (sorted.length !== messages.length) {
    core.warning("Temporary ID dependency cycle detected; preserving original order for cyclic safe outputs");
    const sortedIndices = new Set(sorted);
    for (let index = 0; index < messages.length; index++) {
      if (!sortedIndices.has(index)) sorted.push(index);
    }
  }
  return sorted;
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
 * @returns {Promise<{success: boolean, results: Array<any>, temporaryIdMap: Object, artifactUrlMap: Map<string, string>, outputsWithUnresolvedIds: Array<any>, missings: Object, codePushFailures: Array<{type: string, error: string}>}>}
 */
async function processMessages(messageHandlers, messages, onItemCreated = null) {
  const processingOrder = sortMessageIndicesByTemporaryIdDependencies(messages);
  const results = [];
  const detectionConclusion = process.env.GH_AW_DETECTION_CONCLUSION || "";

  // Collect missing_tool, missing_data, noop, and report_incomplete messages first
  const missings = collectMissingMessages(messages);

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

  // Track code-push failures for fail-fast behaviour.
  // If a code-push type (push_to_pull_request_branch / create_pull_request) fails,
  // all subsequent non-code-push messages are cancelled with a clear reason.
  /** @type {Array<{type: string, error: string}>} */
  const codePushFailures = [];

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

  // Process messages in dependency order while reporting original message indices
  for (let position = 0; position < processingOrder.length; position++) {
    const i = processingOrder[position];
    const message = messages[i];
    const messageType = message.type;

    if (!messageType) {
      core.warning(`Skipping message ${i + 1} without type`);
      continue;
    }

    if (detectionConclusion === "warning") {
      const threatPolicy = getThreatWarningPolicy(messageType);
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
        continue;
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

    const messageHandler = messageHandlers.get(messageType);

    if (!messageHandler) {
      // Check if this message type is handled by a standalone step
      if (STANDALONE_STEP_TYPES.has(messageType)) {
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
        continue;
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
        continue;
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
      continue;
    }

    try {
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
              ? `\n\n---\n> [!NOTE]\n> Direct push to the original pull request branch was not possible (diverged/non-fast-forward). A fallback pull request was created instead: [#${codePushFallbackInfo.number}](${codePushFallbackInfo.url})\n\n`
              : `\n\n---\n> [!NOTE]\n> The pull request was not created — a fallback review issue was created instead due to protected file changes: [#${codePushFallbackInfo.number}](${codePushFallbackInfo.url})\n\n`;
          effectiveMessage = { ...effectiveMessage, body: fallbackNote + (effectiveMessage.body || "") };
          core.info(`Prepending fallback correction note to add_comment body (fallback ${codePushFallbackInfo.fallbackTargetType}: #${codePushFallbackInfo.number})`);
        }
        // If a previous code-push operation failed outright (e.g. patch application error),
        // prepend a failure warning so the status comment accurately reflects that the
        // code changes were not applied.
        if (codePushFailures.length > 0) {
          const failure = codePushFailures[0];
          const failureNote = `\n\n---\n> [!WARNING]\n> The \`${failure.type}\` operation failed: ${failure.error}. The code changes were not applied.\n\n`;
          effectiveMessage = { ...effectiveMessage, body: failureNote + (effectiveMessage.body || "") };
          core.info(`Prepending code push failure note to add_comment body (${failure.type}: ${failure.error})`);
        }
      }

      // Pre-process: replace any '#aw_ID' artifact URL references in the message body
      // with the actual artifact URL so handlers receive the resolved URL directly.
      // This is applied to all message types that carry a 'body' field.
      if (artifactUrlMap.size > 0 && effectiveMessage.body && typeof effectiveMessage.body === "string") {
        const resolvedBody = replaceArtifactUrlReferences(effectiveMessage.body, artifactUrlMap);
        if (resolvedBody !== effectiveMessage.body) {
          effectiveMessage = { ...effectiveMessage, body: resolvedBody };
          core.info(`Resolved artifact URL reference(s) in ${messageType} body`);
        }
      }

      // Call the message handler with the individual message and resolved temp IDs
      const result = await messageHandler(effectiveMessage, resolvedTemporaryIds, temporaryIdMap);

      // Check if the handler explicitly returned a skipped result (e.g. policy filters,
      // no-op warnings, or if_no_changes: warn/ignore). Skipped results should NOT
      // trigger fail-fast cancellation of subsequent messages, and any summary-safe
      // diagnostics supplied by the handler must be preserved.
      if (result && result.skipped === true && !result.deferred) {
        const msg = result.reason || result.warning || result.error || "Handler returned skipped: true";
        core.info(`⏭ Message ${i + 1} (${messageType}) skipped — ${msg}`);
        results.push(buildSkippedResult(messageType, i, result));
        continue;
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
        });
        // Track code-push failures for fail-fast behaviour
        if (CODE_PUSH_TYPES.has(messageType)) {
          codePushFailures.push({ type: messageType, error: errorMsg });
          core.warning(`⚠️ Code push operation '${messageType}' failed — continuing with remaining safe outputs (add_comment messages will include a failure note)`);
        }
        continue;
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
        continue;
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
        core.info(`Registered Azure DevOps temporary ID: ${result.temporaryId}`);
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
      core.error(`✗ Message ${i + 1} (${messageType}) failed: ${getErrorMessage(error)}`);
      results.push({
        type: messageType,
        messageIndex: i,
        success: false,
        error: getErrorMessage(error),
      });
      // Track code-push failures for fail-fast behaviour
      if (CODE_PUSH_TYPES.has(messageType)) {
        codePushFailures.push({ type: messageType, error: getErrorMessage(error) });
        core.warning(`⚠️ Code push operation '${messageType}' failed — continuing with remaining safe outputs (add_comment messages will include a failure note)`);
      }
    }
  }

  // Retry deferred messages now that more temporary IDs may have been resolved
  // This retry loop mirrors the main processing loop but operates on messages that were
  // deferred during the first pass (e.g., link_sub_issue waiting for parent/sub creation).
  // IMPORTANT: Like the main loop, this must register temporary IDs and track outputs
  // with unresolved IDs to enable full synthetic update resolution.
  if (deferredMessages.length > 0) {
    core.info(`\n=== Retrying Deferred Messages ===`);
    core.info(`Found ${deferredMessages.length} deferred message(s) to retry`);

    for (const deferred of deferredMessages) {
      try {
        core.info(`Retrying message ${deferred.messageIndex + 1}/${messages.length}: ${deferred.type}`);

        // Convert Map to plain object for handler
        const resolvedTemporaryIds = Object.fromEntries(temporaryIdMap);

        // Record the temp ID map size before processing to detect new IDs
        const tempIdMapSizeBefore = temporaryIdMap.size;

        // Call the handler again with updated temp ID map
        const result = await deferred.handler(deferred.message, resolvedTemporaryIds, temporaryIdMap);

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
              result: { ...result, success: false, deferred: false, error: errorMsg },
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
        const errorMsg = getErrorMessage(error);
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
            result: { success: false, deferred: false, error: errorMsg },
          };
        }
      }
    }
  }

  // Return outputs with unresolved IDs for synthetic update processing
  // Convert temporaryIdMap to plain object for serialization
  const temporaryIdMapObj = Object.fromEntries(temporaryIdMap);

  return {
    success: true,
    results,
    temporaryIdMap: temporaryIdMapObj,
    artifactUrlMap,
    outputsWithUnresolvedIds,
    missings,
    codePushFailures,
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
      return message.body || "";
    case "create_discussion":
      return message.body || "";
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
 * @param {string[]} [allowedMentionAliases] - Mention aliases allowed by the workflow
 * @param {number} [maxMentions] - Maximum distinct allowed mentions to preserve
 * @returns {Promise<void>}
 */
async function updateIssueBody(github, context, repo, issueNumber, updatedBody, allowedMentionAliases = [], maxMentions = undefined) {
  const [owner, repoName] = repo.split("/");

  core.info(`Updating issue ${repo}#${issueNumber} body with resolved temporary IDs`);

  await github.rest.issues.update({
    owner,
    repo: repoName,
    issue_number: issueNumber,
    body: sanitizeContent(updatedBody, { allowedAliases: allowedMentionAliases, maxMentions }),
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
 * @param {string[]} [allowedMentionAliases] - Mention aliases allowed by the workflow
 * @param {number} [maxMentions] - Maximum distinct allowed mentions to preserve
 * @returns {Promise<void>}
 */
async function updatePullRequestBody(github, context, repo, prNumber, updatedBody, allowedMentionAliases = [], maxMentions = undefined) {
  const [owner, repoName] = repo.split("/");

  core.info(`Updating pull request ${repo}#${prNumber} body with resolved temporary IDs`);

  await github.rest.pulls.update({
    owner,
    repo: repoName,
    pull_number: prNumber,
    body: sanitizeContent(updatedBody, { allowedAliases: allowedMentionAliases, maxMentions }),
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
 * @param {string[]} [allowedMentionAliases] - Mention aliases allowed by the workflow
 * @param {number} [maxMentions] - Maximum distinct allowed mentions to preserve
 * @returns {Promise<void>}
 */
async function updateDiscussionBody(github, context, repo, discussionNumber, updatedBody, allowedMentionAliases = [], maxMentions = undefined) {
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
    body: sanitizeContent(updatedBody, { allowedAliases: allowedMentionAliases, maxMentions }),
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
 * @param {string[]} [allowedMentionAliases] - Mention aliases allowed by the workflow
 * @param {number} [maxMentions] - Maximum distinct allowed mentions to preserve
 * @returns {Promise<void>}
 */
async function updateCommentBody(github, context, repo, commentId, updatedBody, isDiscussion = false, allowedMentionAliases = [], maxMentions = undefined) {
  const [owner, repoName] = repo.split("/");

  core.info(`Updating comment ${commentId} body with resolved temporary IDs`);

  const sanitizedBody = sanitizeContent(updatedBody, { allowedAliases: allowedMentionAliases, maxMentions });

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
      body: sanitizedBody,
    });
  } else {
    // For issue/PR comments, use REST API
    await github.rest.issues.updateComment({
      owner,
      repo: repoName,
      comment_id: commentId,
      body: sanitizedBody,
    });
  }

  core.info(`✓ Updated comment ${commentId}`);
}

/**
 * Process synthetic updates by directly updating the body of outputs with resolved temporary IDs
 * Does not use safe output handlers - directly calls GitHub API to update content
 * @param {any} github - GitHub API client
 * @param {any} context - GitHub Actions context
 * @param {Array<{type: string, message: any, result: any, originalTempIdMapSize: number}>} trackedOutputs - Outputs that need updating
 * @param {Map<string, {repo: string, number: number}>} temporaryIdMap - Current temporary ID map
 * @param {Map<string, string>} [artifactUrlMap] - Optional artifact URL map for resolving artifact references
 * @param {string[]} [allowedMentionAliases] - Mention aliases allowed by the workflow
 * @param {number} [maxMentions] - Maximum distinct allowed mentions to preserve
 * @returns {Promise<number>} Number of successful updates
 */
async function processSyntheticUpdates(github, context, trackedOutputs, temporaryIdMap, artifactUrlMap, allowedMentionAliases = [], maxMentions = undefined) {
  let updateCount = 0;

  core.info(`\n=== Processing Synthetic Updates ===`);
  core.info(`Found ${trackedOutputs.length} output(s) with unresolved temporary IDs`);

  for (const tracked of trackedOutputs) {
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
            // Replace artifact URL references first, then issue number references
            let updatedContent = replaceArtifactUrlReferences(contentToCheck, artifactUrlMap);
            updatedContent = replaceTemporaryIdReferences(updatedContent, temporaryIdMap, tracked.result.repo);

            // Update based on the original type
            switch (tracked.type) {
              case "create_issue":
                await updateIssueBody(github, context, tracked.result.repo, tracked.result.number, updatedContent, allowedMentionAliases, maxMentions);
                updateCount++;
                break;
              case "create_discussion":
                await updateDiscussionBody(github, context, tracked.result.repo, tracked.result.number, updatedContent, allowedMentionAliases, maxMentions);
                updateCount++;
                break;
              case "add_comment":
                // Update comment using the tracked comment ID
                if (tracked.result.commentId) {
                  await updateCommentBody(github, context, tracked.result.repo, tracked.result.commentId, updatedContent, tracked.result.isDiscussion, allowedMentionAliases, maxMentions);
                  updateCount++;
                } else {
                  core.debug(`Skipping synthetic update for comment - comment ID not tracked`);
                }
                break;
              case "comment_memory":
                if (tracked.result.commentId) {
                  await updateCommentBody(github, context, tracked.result.repo, tracked.result.commentId, updatedContent, false, allowedMentionAliases, maxMentions);
                  updateCount++;
                } else {
                  core.debug(`Skipping synthetic update for comment_memory - comment ID not tracked`);
                }
                break;
              case "create_pull_request":
                await updatePullRequestBody(github, context, tracked.result.repo, tracked.result.number, updatedContent, allowedMentionAliases, maxMentions);
                updateCount++;
                break;
              default:
                core.debug(`Unknown output type: ${tracked.type}`);
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
 * Main entry point for the handler manager
 * This is called by the consolidated safe output step
 *
 * @returns {Promise<void>}
 */
async function main() {
  // Detect staged mode before try/finally so it's accessible in the finally block.
  // In staged mode (🎭 Staged Mode Preview) no real items are created in GitHub so no manifest should be emitted.
  const isStaged = isStagedMode();
  /** @type {string | null} */
  let failedOutputsMessage = null;
  /** @type {Array<{type?: string, errorCode?: string, error?: string}>} */
  let failureDetails = [];
  let statusOutputsSet = false;

  try {
    core.info("Safe Output Handler Manager starting...");

    // Load configuration
    const config = loadConfig();
    core.debug(`Configuration: ${JSON.stringify(Object.keys(config))}`);

    // Load agent output
    const agentOutput = loadAgentOutput();
    const agentOutputItems = agentOutput.success ? agentOutput.items : [];
    if (!agentOutput.success) {
      core.info("No agent output available from tool calls");
    } else {
      core.info(`Found ${agentOutput.items.length} message(s) in agent output`);
    }

    const fileBackedCommentMemoryMessages = buildCommentMemoryMessagesFromFiles(agentOutputItems, config);
    const allMessages = [...agentOutputItems, ...fileBackedCommentMemoryMessages];
    if (allMessages.length === 0) {
      core.info("No safe-output messages available - nothing to process");
      if (!isStaged) ensureManifestExists();
      core.setOutput("temporary_id_map", "{}");
      core.setOutput("processed_count", "0");
      setSafeOutputsStatusOutputs({ itemsSucceeded: 0, itemsFailed: 0, status: "success" });
      statusOutputsSet = true;
      return;
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

    const allowedMentionAliases = config.mentions != null ? await resolveAllowedMentionsFromPayload(context, github, core, config.mentions) : [];
    const maxMentions = parseIntTemplatable(config.mentions?.max, 50);

    // Load and initialize handlers based on configuration (factory pattern)
    const messageHandlers = await loadHandlers(config, prReviewBufferRegistry, allowedMentionAliases);

    if (messageHandlers.size === 0) {
      core.info("No handlers loaded - nothing to process");
      // Ensure manifest file exists even when no handlers are loaded (skip in staged mode)
      if (!isStaged) ensureManifestExists();
      // Set empty outputs for downstream steps
      core.setOutput("temporary_id_map", "{}");
      core.setOutput("processed_count", "0");
      setSafeOutputsStatusOutputs({ itemsSucceeded: 0, itemsFailed: 0, status: "success" });
      statusOutputsSet = true;
      return;
    }

    // Create manifest logger for recording created items.
    // createManifestLogger() touches the file immediately so it exists for artifact upload.
    // In staged mode, pass null so no items are logged (nothing is actually created).
    const logCreatedItem = isStaged ? null : createManifestLogger();

    // Pre-check: log a warning when the installation token's rate-limit headroom is low.
    // This surfaces quota pressure before writes start so it is visible in the job log
    // even if no individual write fails.  The check is best-effort – failures are non-fatal.
    await checkRateLimitHeadroom(github, "safe_outputs_pre_check");

    // Process all messages in order of appearance
    const processingResult = await processMessages(messageHandlers, allMessages, logCreatedItem);

    // Finalize buffered PR reviews — one review submission per distinct PR
    const registryEntries = prReviewBufferRegistry.getAllEntries();
    for (const { repo: reviewRepo, prNumber: reviewPrNum, buffer: reviewBuffer } of registryEntries) {
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
          const reviewResult = await reviewBuffer.submitReview();
          if (reviewResult.success && !reviewResult.skipped) {
            logCreatedItemFromResult(logCreatedItem, "submit_pull_request_review", reviewResult);
            core.info(`✓ PR review submitted for ${reviewRepo}#${reviewPrNum}: ${reviewResult.review_url}`);
          } else if (reviewResult.success && reviewResult.skipped) {
            const skipReason = reviewResult.reason || `PR review for ${reviewRepo}#${reviewPrNum} skipped`;
            core.warning(`⚠ ${skipReason}`);
            if (reviewResult.pr_locked) {
              core.setOutput("pr_locked", "true");
            }
            skipReviewResultsForPR(processingResult.results, reviewRepo, reviewPrNum, skipReason);
          } else if (!reviewResult.success) {
            reviewFailureError = reviewResult.error || `PR review finalization failed for ${reviewRepo}#${reviewPrNum}`;
            core.error(`✗ Failed to submit PR review for ${reviewRepo}#${reviewPrNum}: ${reviewFailureError}`);
          }
        } catch (reviewError) {
          reviewFailureError = getErrorMessage(reviewError);
          core.error(`✗ Exception while submitting PR review for ${reviewRepo}#${reviewPrNum}: ${reviewFailureError}`);
        }

        if (reviewFailureError !== null) {
          rollbackReviewResultsForPR(processingResult.results, reviewRepo, reviewPrNum, reviewFailureError);
        }
      }
    }

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

      syntheticUpdateCount = await processSyntheticUpdates(github, context, processingResult.outputsWithUnresolvedIds, temporaryIdMap, processingResult.artifactUrlMap, allowedMentionAliases, maxMentions);
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
      core.info(`Cancelled (code push failed): ${cancelledCount}`);
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
      core.warning(`${cancelledCount} message(s) were cancelled because a code push operation failed`);
    }
    if (skippedNoHandlerResults.length > 0) {
      core.warning(`${skippedNoHandlerResults.length} message(s) were skipped because no handler was loaded. Check your workflow's safe-outputs configuration.`);
    }

    // Export temporary ID map as output for downstream steps
    const temporaryIdMapJson = JSON.stringify(processingResult.temporaryIdMap);
    core.setOutput("temporary_id_map", temporaryIdMapJson);
    core.info(`Exported temporary ID map with ${Object.keys(processingResult.temporaryIdMap).length} mapping(s)`);

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
      return;
    }
    core.info("Safe Output Handler Manager completed");
  } catch (error) {
    const handlerError = `${ERR_VALIDATION}: Handler manager failed: ${getErrorMessage(error)}`;
    if (!statusOutputsSet) {
      setSafeOutputsStatusOutputs({ itemsSucceeded: 0, itemsFailed: 0, status: "failure" });
    }
    if (failedOutputsMessage !== null) {
      recordSafeOutputFailure({ errorCode: ERR_VALIDATION, message: `${failedOutputsMessage}\n${handlerError}`, failures: failureDetails });
      core.setFailed(`${failedOutputsMessage}\n${handlerError}`);
      return;
    }
    recordSafeOutputFailure({ errorCode: ERR_VALIDATION, message: handlerError, failures: failureDetails });
    core.setFailed(handlerError);
  } finally {
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
  loadConfig,
  loadHandlers,
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
};
