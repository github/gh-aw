// @ts-check
/// <reference types="@actions/github-script" />

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

/** @type {string} Safe output type handled by this module */
const HANDLER_TYPE = "dispatch_repository";

const { getErrorMessage } = require("./error_helpers.cjs");
const { createAuthenticatedGitHubClient } = require("./handler_auth.cjs");
const { parseRepoSlug, validateTargetRepo, parseAllowedRepos } = require("./repo_helpers.cjs");
const { logStagedPreviewInfo } = require("./staged_preview.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");
const { buildAwContext } = require("./aw_context.cjs");
const { redactWebhookPayloads } = require("./webhook_context.cjs");
const { SAFE_OUTPUT_E001, SAFE_OUTPUT_E099 } = require("./error_codes.cjs");

/**
 * Main handler factory for dispatch_repository
 * Returns a message handler function that processes individual dispatch_repository messages
 * @type {HandlerFactoryFunction}
 */
async function main(config = {}) {
  const tools = config.tools || {};
  const isStaged = isStagedMode(config);

  const contextRepoSlug = `${context.repo.owner}/${context.repo.repo}`;
  core.info(`dispatch_repository handler initialized: tools=${Object.keys(tools).join(", ")}, context_repo=${contextRepoSlug}`);

  // Per-tool dispatch counters for max enforcement
  /** @type {Record<string, number>} */
  const dispatchCounts = {};

  // Per-tool authenticated clients, cached by tool name. A tool's own github-token/github-app
  // override (resolved by the compiler into toolConfig["github-token"]) must be used instead of
  // the handler-level credential so permission exclusions computed for per-tool credentials
  // (GP5b rule 1) hold true at runtime (GP5b rule 4).
  /** @type {Map<string, any>} */
  const toolClients = new Map();

  /**
   * Message handler function that processes a single dispatch_repository message
   * @param {Object} message - The dispatch_repository message to process
   * @param {Object} resolvedTemporaryIds - Map of temporary IDs to resolved values
   * @returns {Promise<Object>} Result with success/error status
   */
  return async function handleDispatchRepository(message, resolvedTemporaryIds) {
    const toolName = message.tool_name;

    if (!toolName || toolName.trim() === "") {
      core.warning("dispatch_repository: tool_name is empty, skipping");
      return {
        success: false,
        error: `${SAFE_OUTPUT_E001}: tool_name is required`,
      };
    }

    // Look up the tool configuration
    const toolConfig = tools[toolName];
    if (!toolConfig) {
      core.warning(`dispatch_repository: unknown tool "${toolName}", skipping`);
      return {
        success: false,
        error: `${SAFE_OUTPUT_E001}: tool "${toolName}" is not configured in dispatch_repository`,
      };
    }

    const maxCount = typeof toolConfig.max === "number" ? toolConfig.max : parseInt(String(toolConfig.max || "1"), 10) || 1;
    const currentCount = dispatchCounts[toolName] || 0;

    if (currentCount >= maxCount) {
      core.warning(`dispatch_repository: max count of ${maxCount} reached for tool "${toolName}", skipping`);
      return {
        success: false,
        error: `E002: Max count of ${maxCount} reached for tool "${toolName}"`,
      };
    }

    // Resolve target repository
    // Prefer message.repository > toolConfig.repository > first allowed_repository
    const messageRepo = message.repository || "";
    const configuredRepo = toolConfig.repository || "";
    const allowedReposConfig = toolConfig.allowed_repositories || [];
    const allowedRepos = parseAllowedRepos(allowedReposConfig);

    let targetRepoSlug = messageRepo || configuredRepo;

    if (!targetRepoSlug && allowedReposConfig.length > 0) {
      // Default to first allowed repository if no specific target given
      targetRepoSlug = allowedReposConfig[0];
    }

    if (!targetRepoSlug) {
      core.warning(`dispatch_repository: no target repository for tool "${toolName}"`);
      return {
        success: false,
        error: `${SAFE_OUTPUT_E001}: No target repository configured for tool "${toolName}"`,
      };
    }

    // Validate cross-repo dispatch (SEC-005 pattern)
    const isCrossRepo = targetRepoSlug !== contextRepoSlug;
    if (isCrossRepo && allowedRepos.size > 0) {
      const repoValidation = validateTargetRepo(targetRepoSlug, contextRepoSlug, allowedRepos);
      if (!repoValidation.valid) {
        core.warning(`dispatch_repository: cross-repo check failed for "${targetRepoSlug}": ${repoValidation.error}`);
        return {
          success: false,
          error: `E004: ${repoValidation.error}`,
        };
      }
    }

    const parsedRepo = parseRepoSlug(targetRepoSlug);
    if (!parsedRepo) {
      core.warning(`dispatch_repository: invalid repository slug "${targetRepoSlug}"`);
      return {
        success: false,
        error: `${SAFE_OUTPUT_E001}: Invalid repository slug "${targetRepoSlug}" (expected "owner/repo")`,
      };
    }

    // Build client_payload from message inputs + workflow identifier
    /** @type {Record<string, any>} */
    const clientPayload = {
      workflow: toolConfig.workflow || "",
      ...(message.inputs && typeof message.inputs === "object" ? message.inputs : {}),
    };

    // Inject aw_context so the receiving repository can trace the dispatch back to its caller.
    clientPayload["aw_context"] = buildAwContext();

    const eventType = toolConfig.event_type || toolConfig.eventType || "";
    if (!eventType) {
      core.warning(`dispatch_repository: tool "${toolName}" has no event_type configured`);
      return {
        success: false,
        error: `E001: event_type is required for tool "${toolName}"`,
      };
    }

    core.info(`dispatch_repository: dispatching event_type="${eventType}" to ${targetRepoSlug} (workflow: ${toolConfig.workflow || "unspecified"})`);

    // If in staged mode, preview without executing
    if (isStaged || isStagedMode(toolConfig)) {
      logStagedPreviewInfo(`Would dispatch repository_dispatch event: event_type="${eventType}" to ${targetRepoSlug}, client_payload=${JSON.stringify({ ...clientPayload, aw_context: redactWebhookPayloads(clientPayload.aw_context) })}`);
      dispatchCounts[toolName] = currentCount + 1;
      return {
        success: true,
        staged: true,
        tool_name: toolName,
        repository: targetRepoSlug,
        event_type: eventType,
        client_payload: clientPayload,
      };
    }

    try {
      let toolClient = toolClients.get(toolName);
      if (!toolClient) {
        if (Object.hasOwn(toolConfig, "github-token") && !toolConfig["github-token"]) {
          return {
            success: false,
            error: `${SAFE_OUTPUT_E099}: Failed to dispatch repository_dispatch event "${eventType}" to ${targetRepoSlug}: github-token for tool "${toolName}" resolved to an empty value`,
          };
        }
        toolClient = await createAuthenticatedGitHubClient(toolConfig);
        toolClients.set(toolName, toolClient);
      }

      await toolClient.rest.repos.createDispatchEvent({
        owner: parsedRepo.owner,
        repo: parsedRepo.repo,
        event_type: eventType,
        client_payload: clientPayload,
      });

      dispatchCounts[toolName] = currentCount + 1;
      core.info(`✓ Successfully dispatched repository_dispatch: event_type="${eventType}" to ${targetRepoSlug}`);

      return {
        success: true,
        tool_name: toolName,
        repository: targetRepoSlug,
        event_type: eventType,
        client_payload: clientPayload,
      };
    } catch (error) {
      const errorMessage = getErrorMessage(error);
      core.error(`dispatch_repository: failed to dispatch event_type="${eventType}" to ${targetRepoSlug}: ${errorMessage}`);

      return {
        success: false,
        error: `${SAFE_OUTPUT_E099}: Failed to dispatch repository_dispatch event "${eventType}" to ${targetRepoSlug}: ${errorMessage}`,
      };
    }
  };
}

module.exports = { main };
