// @ts-check
/// <reference types="@actions/github-script" />

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

/** @type {string} Safe output type handled by this module */
const HANDLER_TYPE = "request_continuation";

const { getErrorMessage } = require("./error_helpers.cjs");
const { ERR_CONFIG, ERR_VALIDATION } = require("./error_codes.cjs");
const { createAuthenticatedGitHubClient } = require("./handler_auth.cjs");
const { logStagedPreviewInfo } = require("./staged_preview.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");
const { buildAwContext, readInboundAwContext } = require("./aw_context.cjs");
const { dispatchWorkflow } = require("./dispatch_workflow_transport.cjs");

const DEFAULT_MAX_CONTINUATIONS = 5;
const MAX_HINTS_LENGTH = 4000;

/**
 * Returns the continuation depth of the current run (0 for the first run).
 * @param {any} awContext
 * @returns {number}
 */
function readContinuationDepth(awContext) {
  const depth = awContext?.continuation?.depth;
  if (depth === undefined || depth === null) return 0;
  const parsed = Number(depth);
  if (!Number.isInteger(parsed) || parsed < 0) throw new Error(`${ERR_VALIDATION}: aw_context.continuation.depth must be a non-negative integer`);
  return parsed;
}

/**
 * Resolves the workflow file of the current run from GITHUB_WORKFLOW_REF.
 * @param {string | undefined} workflowRef
 * @returns {string}
 */
function resolveCurrentWorkflowFile(workflowRef) {
  const match = /\.github\/workflows\/([^/@]+\.ya?ml)@/.exec(workflowRef || "");
  if (!match) throw new Error(`${ERR_CONFIG}: Cannot resolve the current workflow file from GITHUB_WORKFLOW_REF`);
  return match[1];
}

/**
 * Resolves the branch ref for the next run so it shares the checkpoint scope of this run.
 * @returns {string}
 */
function resolveContinuationRef() {
  if (process.env.GITHUB_HEAD_REF) return `refs/heads/${process.env.GITHUB_HEAD_REF}`;
  const ref = process.env.GITHUB_REF || context.ref;
  if (!ref) throw new Error(`${ERR_CONFIG}: Cannot resolve the ref for the continuation run`);
  return ref;
}

/**
 * Main handler factory for request_continuation.
 * The agent checkpoint is uploaded only when this output exists; this handler
 * dispatches the same workflow so the next run restores that checkpoint.
 * @type {HandlerFactoryFunction}
 */
async function main(config = {}) {
  const maxContinuations = Number.isInteger(config.max_continuations) && config.max_continuations > 0 ? config.max_continuations : DEFAULT_MAX_CONTINUATIONS;
  const isStaged = isStagedMode(config);
  const githubClient = await createAuthenticatedGitHubClient(config);
  let handled = false;

  core.info(`Request continuation configuration: max continuations=${maxContinuations}`);

  /**
   * @param {any} message
   * @returns {Promise<Object>}
   */
  return async function handleRequestContinuation(message) {
    if (handled) {
      core.warning("Skipping request_continuation: only one continuation can be requested per run");
      return { success: false, error: "Only one continuation can be requested per run" };
    }
    handled = true;

    const hints = typeof message?.hints === "string" ? message.hints.trim() : "";
    if (!hints) return { success: false, error: "request_continuation requires non-empty hints" };
    if (hints.length > MAX_HINTS_LENGTH) return { success: false, error: `request_continuation hints must be at most ${MAX_HINTS_LENGTH} characters` };

    try {
      const depth = readContinuationDepth(readInboundAwContext(context.payload));
      if (depth >= maxContinuations) {
        core.warning(`Continuation limit reached (${depth} of ${maxContinuations}); not dispatching another run`);
        return { success: true, skipped: true, skipReason: `Continuation limit of ${maxContinuations} reached`, depth };
      }

      const workflowFile = resolveCurrentWorkflowFile(process.env.GITHUB_WORKFLOW_REF);
      const ref = resolveContinuationRef();
      const nextDepth = depth + 1;
      const awContext = { ...buildAwContext(), continuation: { depth: nextDepth, max: maxContinuations, hints } };
      const inputs = { aw_context: JSON.stringify(awContext) };

      if (isStaged) {
        logStagedPreviewInfo(`Would dispatch continuation ${nextDepth} of ${maxContinuations}: ${workflowFile} with ref: ${ref}`);
        return { success: true, staged: true, workflow_name: workflowFile, depth: nextDepth };
      }

      core.info(`Dispatching continuation ${nextDepth} of ${maxContinuations}: ${workflowFile} (${ref})`);
      const response = await dispatchWorkflow(githubClient, { owner: context.repo.owner, repo: context.repo.repo, workflow_id: workflowFile, ref, inputs, return_run_details: true }, { allowLegacyFallback: true, coreApi: core });
      const runId = response?.data?.workflow_run_id;
      core.info(runId ? `✓ Dispatched continuation run ${runId}` : "✓ Dispatched continuation run");
      return { success: true, workflow_name: workflowFile, depth: nextDepth, run_id: runId };
    } catch (error) {
      const errorMessage = getErrorMessage(error);
      core.error(`Failed to request continuation: ${errorMessage}`);
      return { success: false, error: `Failed to request continuation: ${errorMessage}` };
    }
  };
}

module.exports = { main, HANDLER_TYPE, readContinuationDepth, resolveCurrentWorkflowFile };
