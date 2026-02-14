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
const { hasUnresolvedTemporaryIds, replaceTemporaryIdReferences, normalizeTemporaryId } = require("./temporary_id.cjs");
const { generateMissingInfoSections } = require("./missing_info_formatter.cjs");
const { setCollectedMissings } = require("./missing_messages_helper.cjs");
const { writeSafeOutputSummaries } = require("./safe_output_summary.cjs");
const { getIssuesToAssignCopilot } = require("./create_issue.cjs");
const { createReviewBuffer } = require("./pr_review_buffer.cjs");

/**
 * Handler map configuration
 * Maps safe output types to their handler module file paths
 */
const HANDLER_MAP = {
  create_issue: "./create_issue.cjs",
  add_comment: "./add_comment.cjs",
  create_discussion: "./create_discussion.cjs",
  close_issue: "./close_issue.cjs",
  close_discussion: "./close_discussion.cjs",
  add_labels: "./add_labels.cjs",
  remove_labels: "./remove_labels.cjs",
  update_issue: "./update_issue.cjs",
  update_discussion: "./update_discussion.cjs",
  link_sub_issue: "./link_sub_issue.cjs",
  update_release: "./update_release.cjs",
  create_pull_request_review_comment: "./create_pr_review_comment.cjs",
  submit_pull_request_review: "./submit_pr_review.cjs",
  resolve_pull_request_review_thread: "./resolve_pr_review_thread.cjs",
  create_pull_request: "./create_pull_request.cjs",
  push_to_pull_request_branch: "./push_to_pull_request_branch.cjs",
  update_pull_request: "./update_pull_request.cjs",
  close_pull_request: "./close_pull_request.cjs",
  mark_pull_request_as_ready_for_review: "./mark_pull_request_as_ready_for_review.cjs",
  hide_comment: "./hide_comment.cjs",
  add_reviewer: "./add_reviewer.cjs",
  assign_milestone: "./assign_milestone.cjs",
  assign_to_user: "./assign_to_user.cjs",
  unassign_from_user: "./unassign_from_user.cjs",
  create_code_scanning_alert: "./create_code_scanning_alert.cjs",
  autofix_code_scanning_alert: "./autofix_code_scanning_alert.cjs",
  dispatch_workflow: "./dispatch_workflow.cjs",
  create_missing_tool_issue: "./create_missing_tool_issue.cjs",
  missing_tool: "./missing_tool.cjs",
  create_missing_data_issue: "./create_missing_data_issue.cjs",
  missing_data: "./missing_data.cjs",
  noop: "./noop_handler.cjs",
  create_project: "./create_project.cjs",
  create_project_status_update: "./create_project_status_update.cjs",
  update_project: "./update_project.cjs",
};

/**
 * Message types handled by standalone steps (not through the handler manager)
 * These types should not trigger warnings when skipped by the handler manager
 *
 * Standalone types: assign_to_agent, create_agent_session, upload_asset, noop
 *   - Have dedicated processing steps with specialized logic
 */
const STANDALONE_STEP_TYPES = new Set(["assign_to_agent", "create_agent_session", "upload_asset", "noop"]);

/**
 * Load configuration for safe outputs
 * Reads configuration from GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG environment variable
 * @returns {Object} Safe outputs configuration
 */
function loadConfig() {
  if (!process.env.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG) {
    throw new Error("GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG environment variable is required but not set");
  }

  try {
    const config = JSON.parse(process.env.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG);
    core.info(`Loaded config from GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG: ${JSON.stringify(config)}`);
    // Normalize config keys: convert hyphens to underscores
    return Object.fromEntries(Object.entries(config).map(([k, v]) => [k.replace(/-/g, "_"), v]));
  } catch (error) {
    throw new Error(`Failed to parse GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG: ${getErrorMessage(error)}`);
  }
}

/** @type {Set<string>} Handler types that participate in the PR review buffer */
const PR_REVIEW_HANDLER_TYPES = new Set(["create_pull_request_review_comment", "submit_pull_request_review"]);

/**
 * Load and initialize handlers for enabled safe output types
 * Calls each handler's factory function (main) to get message processors
 * @param {Object} config - Safe outputs configuration
 * @param {Object} prReviewBuffer - Shared PR review buffer instance
 * @returns {Promise<Map<string, Function>>} Map of type to message handler function
 */
async function loadHandlers(config, prReviewBuffer) {
  const messageHandlers = new Map();

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

          // Inject shared PR review buffer into handlers that need it
          if (PR_REVIEW_HANDLER_TYPES.has(type)) {
            handlerConfig._prReviewBuffer = prReviewBuffer;
          }

          const messageHandler = await handlerModule.main(handlerConfig);

          if (typeof messageHandler !== "function") {
            // This is a fatal error - the handler is misconfigured
            // Re-throw to fail the step rather than continuing
            const error = new Error(`Handler ${type} main() did not return a function - expected a message handler function but got ${typeof messageHandler}`);
            core.error(`✗ Fatal error loading handler ${type}: ${error.message}`);
            throw error;
          }

          messageHandlers.set(type, messageHandler);
          core.info(`✓ Loaded and initialized handler for: ${type}`);
        } else {
          core.warning(`Handler module ${type} does not export a main function`);
        }
      } catch (error) {
        // Re-throw fatal handler validation errors
        const errorMessage = getErrorMessage(error);
        if (errorMessage.includes("did not return a function")) {
          throw error;
        }
        // For other errors (e.g., module not found), log warning and continue
        core.warning(`Failed to load handler for ${type}: ${errorMessage}`);
      }
    } else {
      core.debug(`Handler not enabled: ${type}`);
    }
  }

  core.info(`Loaded ${messageHandlers.size} handler(s)`);
  return messageHandlers;
}

/**
 * Collect missing_tool, missing_data, and noop messages from the messages array
 * @param {Array<Object>} messages - Array of safe output messages
 * @returns {{missingTools: Array<any>, missingData: Array<any>, noopMessages: Array<any>}} Object with collected missing items and noop messages
 */
function collectMissingMessages(messages) {
  const missingTools = [];
  const missingData = [];
  const noopMessages = [];

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
    }
  }

  core.info(`Collected ${missingTools.length} missing tool(s), ${missingData.length} missing data item(s), and ${noopMessages.length} noop message(s)`);
  return { missingTools, missingData, noopMessages };
}

/**
 * Process all messages from agent output in the order they appear
 * Dispatches each message to the appropriate handler while maintaining shared state (temporary ID map)
 * Tracks outputs created with unresolved temporary IDs and generates synthetic updates after resolution
 *
 * @param {Map<string, Function>} messageHandlers - Map of message handler functions
 * @param {Array<Object>} messages - Array of safe output messages
 * @returns {Promise<{success: boolean, results: Array<any>, temporaryIdMap: Object, outputsWithUnresolvedIds: Array<any>, missings: Object}>}
 */
async function processMessages(messageHandlers, messages) {
  const results = [];

  // Collect missing_tool and missing_data messages first
  const missings = collectMissingMessages(messages);

  // Initialize shared temporary ID map
  // This will be populated by handlers as they create entities with temporary IDs
  /** @type {Map<string, {repo: string, number: number}>} */
  const temporaryIdMap = new Map();

  // Track outputs that were created with unresolved temporary IDs
  // Format: {type, message, result, originalTempIdMapSize}
  /** @type {Array<{type: string, message: any, result: any, originalTempIdMapSize: number}>} */
  const outputsWithUnresolvedIds = [];

  // Track messages that were deferred due to unresolved temporary IDs
  // These will be retried after the first pass when more temp IDs may be resolved
  /** @type {Array<{type: string, message: any, messageIndex: number, handler: Function}>} */
  const deferredMessages = [];

  core.info(`Processing ${messages.length} message(s) in order of appearance...`);

  // Process messages in order of appearance
  for (let i = 0; i < messages.length; i++) {
    const message = messages[i];
    const messageType = message.type;

    if (!messageType) {
      core.warning(`Skipping message ${i + 1} without type`);
      continue;
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
          reason: "Handled by standalone step",
        });
        continue;
      }

      // Unknown message type - warn the user
      core.warning(
        `⚠️ No handler loaded for message type '${messageType}' (message ${i + 1}/${messages.length}). The message will be skipped. This may happen if the safe output type is not configured in the workflow's safe-outputs section.`
      );
      results.push({
        type: messageType,
        messageIndex: i,
        success: false,
        error: `No handler loaded for type '${messageType}'`,
      });
      continue;
    }

    try {
      core.info(`Processing message ${i + 1}/${messages.length}: ${messageType}`);

      // Convert Map to plain object for handler
      const resolvedTemporaryIds = Object.fromEntries(temporaryIdMap);

      // Record the temp ID map size before processing to detect new IDs
      const tempIdMapSizeBefore = temporaryIdMap.size;

      // Call the message handler with the individual message and resolved temp IDs
      const result = await messageHandler(message, resolvedTemporaryIds, temporaryIdMap);

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
        continue;
      }

      // Check if the operation was deferred due to unresolved temporary IDs
      if (result && result.deferred === true) {
        core.info(`⏸ Message ${i + 1} (${messageType}) deferred - will retry after first pass`);
        deferredMessages.push({
          type: messageType,
          message: message,
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

      // Check if this output was created with unresolved temporary IDs
      // For create_issue, create_discussion, add_comment - check if body has unresolved IDs

      // Handle add_comment which returns an array of comments
      if (messageType === "add_comment" && Array.isArray(result)) {
        const contentToCheck = getContentToCheck(messageType, message);
        if (contentToCheck && hasUnresolvedTemporaryIds(contentToCheck, temporaryIdMap)) {
          // Track each comment that was created with unresolved temp IDs
          for (const comment of result) {
            if (comment._tracking) {
              core.info(`Comment ${comment._tracking.commentId} on ${comment._tracking.repo}#${comment._tracking.itemNumber} was created with unresolved temporary IDs - tracking for update`);
              outputsWithUnresolvedIds.push({
                type: messageType,
                message: message,
                result: {
                  commentId: comment._tracking.commentId,
                  itemNumber: comment._tracking.itemNumber,
                  repo: comment._tracking.repo,
                  isDiscussion: comment._tracking.isDiscussion,
                },
                originalTempIdMapSize: tempIdMapSizeBefore,
              });
            }
          }
        }
      } else if (result && result.number && result.repo) {
        // Handle create_issue, create_discussion
        const contentToCheck = getContentToCheck(messageType, message);
        if (contentToCheck && hasUnresolvedTemporaryIds(contentToCheck, temporaryIdMap)) {
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

      core.info(`✓ Message ${i + 1} (${messageType}) completed successfully`);
    } catch (error) {
      core.error(`✗ Message ${i + 1} (${messageType}) failed: ${getErrorMessage(error)}`);
      results.push({
        type: messageType,
        messageIndex: i,
        success: false,
        error: getErrorMessage(error),
      });
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

        // Check if the handler explicitly returned a failure
        if (result && result.success === false && !result.deferred) {
          const errorMsg = result.error || "Handler returned success: false";
          core.error(`✗ Retry of message ${deferred.messageIndex + 1} (${deferred.type}) failed: ${errorMsg}`);
          // Update the result to error
          const resultIndex = results.findIndex(r => r.messageIndex === deferred.messageIndex);
          if (resultIndex >= 0) {
            results[resultIndex].success = false;
            results[resultIndex].error = errorMsg;
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
            const contentToCheck = getContentToCheck(deferred.type, deferred.message);
            if (contentToCheck && hasUnresolvedTemporaryIds(contentToCheck, temporaryIdMap)) {
              core.info(`Output ${result.repo}#${result.number} was created with unresolved temporary IDs - tracking for update`);
              outputsWithUnresolvedIds.push({
                type: deferred.type,
                message: deferred.message,
                result: result,
                originalTempIdMapSize: tempIdMapSizeBefore,
              });
            }
          }

          // Update the result to success
          const resultIndex = results.findIndex(r => r.messageIndex === deferred.messageIndex);
          if (resultIndex >= 0) {
            results[resultIndex].success = true;
            results[resultIndex].deferred = false;
            results[resultIndex].result = result;
          }
        }
      } catch (error) {
        core.error(`✗ Retry of message ${deferred.messageIndex + 1} (${deferred.type}) failed: ${getErrorMessage(error)}`);
        // Update the result to error
        const resultIndex = results.findIndex(r => r.messageIndex === deferred.messageIndex);
        if (resultIndex >= 0) {
          results[resultIndex].error = getErrorMessage(error);
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
    outputsWithUnresolvedIds,
    missings,
  };
}

/**
 * Get the content field to check for unresolved temporary IDs based on message type
 * @param {string} messageType - Type of the message
 * @param {any} message - The message object
 * @returns {string|null} Content to check for temporary IDs
 */
function getContentToCheck(messageType, message) {
  switch (messageType) {
    case "create_issue":
      return message.body || "";
    case "create_discussion":
      return message.body || "";
    case "add_comment":
      return message.body || "";
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
 * @returns {Promise<void>}
 */
async function updateIssueBody(github, context, repo, issueNumber, updatedBody) {
  const [owner, repoName] = repo.split("/");

  core.info(`Updating issue ${repo}#${issueNumber} body with resolved temporary IDs`);

  await github.rest.issues.update({
    owner,
    repo: repoName,
    issue_number: issueNumber,
    body: updatedBody,
  });

  core.info(`✓ Updated issue ${repo}#${issueNumber}`);
}

/**
 * Update the body of a discussion with resolved temporary IDs
 * @param {any} github - GitHub API client
 * @param {any} context - GitHub Actions context
 * @param {string} repo - Repository in "owner/repo" format
 * @param {number} discussionNumber - Discussion number to update
 * @param {string} updatedBody - Updated body content with resolved temp IDs
 * @returns {Promise<void>}
 */
async function updateDiscussionBody(github, context, repo, discussionNumber, updatedBody) {
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
    body: updatedBody,
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
 * @returns {Promise<void>}
 */
async function updateCommentBody(github, context, repo, commentId, updatedBody, isDiscussion = false) {
  const [owner, repoName] = repo.split("/");

  core.info(`Updating comment ${commentId} body with resolved temporary IDs`);

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
      body: updatedBody,
    });
  } else {
    // For issue/PR comments, use REST API
    await github.rest.issues.updateComment({
      owner,
      repo: repoName,
      comment_id: commentId,
      body: updatedBody,
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
 * @returns {Promise<number>} Number of successful updates
 */
async function processSyntheticUpdates(github, context, trackedOutputs, temporaryIdMap) {
  let updateCount = 0;

  core.info(`\n=== Processing Synthetic Updates ===`);
  core.info(`Found ${trackedOutputs.length} output(s) with unresolved temporary IDs`);

  for (const tracked of trackedOutputs) {
    // Check if any new temporary IDs were resolved since this output was created
    // Only check and update if we have content to check
    if (temporaryIdMap.size > tracked.originalTempIdMapSize) {
      const contentToCheck = getContentToCheck(tracked.type, tracked.message);

      // Only process if we have content to check
      if (contentToCheck !== null && contentToCheck !== "") {
        // Check if the content still has unresolved IDs (some may now be resolved)
        const stillHasUnresolved = hasUnresolvedTemporaryIds(contentToCheck, temporaryIdMap);
        const resolvedCount = temporaryIdMap.size - tracked.originalTempIdMapSize;

        if (!stillHasUnresolved) {
          // All temporary IDs are now resolved - update the body directly
          let logInfo = tracked.result.commentId ? `comment ${tracked.result.commentId} on ${tracked.result.repo}#${tracked.result.itemNumber}` : `${tracked.result.repo}#${tracked.result.number}`;
          core.info(`Updating ${tracked.type} ${logInfo} (${resolvedCount} temp ID(s) resolved)`);

          try {
            // Replace temporary ID references with resolved values
            const updatedContent = replaceTemporaryIdReferences(contentToCheck, temporaryIdMap, tracked.result.repo);

            // Update based on the original type
            switch (tracked.type) {
              case "create_issue":
                await updateIssueBody(github, context, tracked.result.repo, tracked.result.number, updatedContent);
                updateCount++;
                break;
              case "create_discussion":
                await updateDiscussionBody(github, context, tracked.result.repo, tracked.result.number, updatedContent);
                updateCount++;
                break;
              case "add_comment":
                // Update comment using the tracked comment ID
                if (tracked.result.commentId) {
                  await updateCommentBody(github, context, tracked.result.repo, tracked.result.commentId, updatedContent, tracked.result.isDiscussion);
                  updateCount++;
                } else {
                  core.debug(`Skipping synthetic update for comment - comment ID not tracked`);
                }
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
 * Main entry point for the handler manager
 * This is called by the consolidated safe output step
 *
 * @returns {Promise<void>}
 */
async function main() {
  try {
    core.info("Safe Output Handler Manager starting...");

    // Reset create_issue handler's global state to ensure clean state for this run
    // This prevents stale data accumulation if the module is reused
    const { resetIssuesToAssignCopilot } = require("./create_issue.cjs");
    resetIssuesToAssignCopilot();

    // Load configuration
    const config = loadConfig();
    core.debug(`Configuration: ${JSON.stringify(Object.keys(config))}`);

    // Load agent output
    const agentOutput = loadAgentOutput();
    if (!agentOutput.success) {
      core.info("No agent output available - nothing to process");
      // Set empty outputs for downstream steps
      core.setOutput("temporary_id_map", "{}");
      core.setOutput("processed_count", 0);
      return;
    }

    core.info(`Found ${agentOutput.items.length} message(s) in agent output`);

    // Create the shared PR review buffer instance (no global state)
    const prReviewBuffer = createReviewBuffer();

    // Apply footer config with priority:
    // 1. create_pull_request_review_comment.footer (highest priority)
    // 2. submit_pull_request_review.footer (fallback)
    // 3. Default: "always"
    let footerConfig = undefined;
    if (config.create_pull_request_review_comment?.footer !== undefined) {
      footerConfig = config.create_pull_request_review_comment.footer;
      core.info(`Using footer config from create_pull_request_review_comment: ${footerConfig}`);
    } else if (config.submit_pull_request_review?.footer !== undefined) {
      footerConfig = config.submit_pull_request_review.footer;
      core.info(`Using footer config from submit_pull_request_review: ${footerConfig}`);
    }

    if (footerConfig !== undefined) {
      prReviewBuffer.setFooterMode(footerConfig);
    }

    // Load and initialize handlers based on configuration (factory pattern)
    const messageHandlers = await loadHandlers(config, prReviewBuffer);

    if (messageHandlers.size === 0) {
      core.info("No handlers loaded - nothing to process");
      // Set empty outputs for downstream steps
      core.setOutput("temporary_id_map", "{}");
      core.setOutput("processed_count", 0);
      return;
    }

    // Process all messages in order of appearance
    const processingResult = await processMessages(messageHandlers, agentOutput.items);

    // Finalize buffered PR review — submit when comments or metadata exist
    if (prReviewBuffer.hasBufferedComments() || prReviewBuffer.hasReviewMetadata()) {
      core.info(`\n=== Finalizing PR Review ===`);
      const bufferedCount = prReviewBuffer.getBufferedCount();
      if (bufferedCount > 0) {
        core.info(`Submitting ${bufferedCount} buffered review comment(s) as a single PR review`);
      } else {
        core.info("Submitting PR review (body-only, no inline comments)");
      }
      try {
        const reviewResult = await prReviewBuffer.submitReview();
        if (reviewResult.success && !reviewResult.skipped) {
          core.info(`✓ PR review submitted successfully: ${reviewResult.review_url}`);
        } else if (!reviewResult.success) {
          core.warning(`✗ Failed to submit PR review: ${reviewResult.error}`);
        }
      } catch (reviewError) {
        const errorMessage = reviewError instanceof Error ? reviewError.message : String(reviewError);
        core.warning(`✗ Exception while submitting PR review: ${errorMessage}`);
      }
    }

    // Store collected missings in helper module for handlers to access
    if (processingResult.missings) {
      setCollectedMissings(processingResult.missings);
      core.info(
        `Stored ${processingResult.missings.missingTools.length} missing tool(s), ${processingResult.missings.missingData.length} missing data item(s), and ${processingResult.missings.noopMessages.length} noop message(s) for footer generation`
      );
    }

    // Process synthetic updates by directly updating issue/discussion bodies
    let syntheticUpdateCount = 0;
    if (processingResult.outputsWithUnresolvedIds && processingResult.outputsWithUnresolvedIds.length > 0) {
      // Convert temp ID map back to Map
      const temporaryIdMap = new Map(Object.entries(processingResult.temporaryIdMap));

      syntheticUpdateCount = await processSyntheticUpdates(github, context, processingResult.outputsWithUnresolvedIds, temporaryIdMap);
    }

    // Write step summaries for all processed safe-outputs
    await writeSafeOutputSummaries(processingResult.results, agentOutput.items);

    // Log summary
    const successCount = processingResult.results.filter(r => r.success).length;
    const failureCount = processingResult.results.filter(r => !r.success && !r.deferred && !r.skipped).length;
    const deferredCount = processingResult.results.filter(r => r.deferred).length;
    const skippedStandaloneResults = processingResult.results.filter(r => r.skipped && r.reason === "Handled by standalone step");
    const skippedNoHandlerResults = processingResult.results.filter(r => !r.success && !r.skipped && r.error?.includes("No handler loaded"));

    core.info(`\n=== Processing Summary ===`);
    core.info(`Total messages: ${processingResult.results.length}`);
    core.info(`Successful: ${successCount}`);
    core.info(`Failed: ${failureCount}`);
    if (deferredCount > 0) {
      core.info(`Deferred: ${deferredCount}`);
    }
    if (skippedStandaloneResults.length > 0) {
      core.info(`Skipped (standalone step): ${skippedStandaloneResults.length}`);
      const standaloneTypes = [...new Set(skippedStandaloneResults.map(r => r.type))];
      core.info(`  Types: ${standaloneTypes.join(", ")}`);
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
    }
    if (skippedNoHandlerResults.length > 0) {
      core.warning(`${skippedNoHandlerResults.length} message(s) were skipped because no handler was loaded. Check your workflow's safe-outputs configuration.`);
    }

    // Export temporary ID map as output for downstream steps (e.g., assign_to_agent)
    const temporaryIdMapJson = JSON.stringify(processingResult.temporaryIdMap);
    core.setOutput("temporary_id_map", temporaryIdMapJson);
    core.info(`Exported temporary ID map with ${Object.keys(processingResult.temporaryIdMap).length} mapping(s)`);

    // Export processed count for consistency with project handler
    core.setOutput("processed_count", successCount);

    // Export issues that need copilot assignment (if any)
    const issuesToAssignCopilot = getIssuesToAssignCopilot();
    if (issuesToAssignCopilot.length > 0) {
      const issuesToAssignStr = issuesToAssignCopilot.join(",");
      core.setOutput("issues_to_assign_copilot", issuesToAssignStr);
      core.info(`Exported ${issuesToAssignCopilot.length} issue(s) for copilot assignment: ${issuesToAssignStr}`);
    } else {
      core.setOutput("issues_to_assign_copilot", "");
    }

    // Export create_discussion errors for conclusion job
    const createDiscussionErrors = processingResult.results
      .filter(r => r.type === "create_discussion" && !r.success && !r.deferred && !r.skipped)
      .map((r, index) => {
        const message = agentOutput.items[r.messageIndex];
        const title = message?.title || "Discussion";
        const repo = message?.repo || "unknown";
        const errorMsg = r.error || "Unknown error";
        return `discussion:${index}:${repo}:${title}:${errorMsg}`;
      })
      .join("\n");

    const createDiscussionErrorCount = processingResult.results.filter(r => r.type === "create_discussion" && !r.success && !r.deferred && !r.skipped).length;

    core.setOutput("create_discussion_errors", createDiscussionErrors);
    core.setOutput("create_discussion_error_count", createDiscussionErrorCount.toString());

    if (createDiscussionErrorCount > 0) {
      core.info(`Exported ${createDiscussionErrorCount} create_discussion error(s)`);
    }

    core.info("Safe Output Handler Manager completed");
  } catch (error) {
    core.setFailed(`Handler manager failed: ${getErrorMessage(error)}`);
  }
}

module.exports = { main, loadConfig, loadHandlers, processMessages };
