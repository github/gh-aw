// @ts-check
/// <reference types="@actions/github-script" />

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

/** @type {string} Safe output type handled by this module */
const HANDLER_TYPE = "request_continuation";

const fs = require("node:fs");
const path = require("node:path");
const { getErrorMessage } = require("./error_helpers.cjs");
const { ERR_VALIDATION } = require("./error_codes.cjs");
const { logStagedPreviewInfo } = require("./staged_preview.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");
const { buildAwContext, readInboundAwContext } = require("./aw_context.cjs");
const { CONTINUATION_REQUEST_FILE_NAME, CONTINUATION_REQUEST_VERSION, MAX_CONTINUATION_HINTS_LENGTH: MAX_HINTS_LENGTH } = require("./continuation_request.cjs");

const DEFAULT_MAX_CONTINUATIONS = 5;
const DEFAULT_CONTINUATION_REQUEST_FILE = `/tmp/gh-aw/continuation/${CONTINUATION_REQUEST_FILE_NAME}`;

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
 * Main handler factory for request_continuation.
 * The agent checkpoint is uploaded only when this output exists. This handler
 * never dispatches: it records a continuation request that the safe-outputs job
 * uploads as an artifact, and the conclusion job hands the run to the generated
 * continuation relay, which validates the request and dispatches the next run.
 * @type {HandlerFactoryFunction}
 */
async function main(config = {}) {
  const maxContinuations = Number.isInteger(config.max_continuations) && config.max_continuations > 0 ? config.max_continuations : DEFAULT_MAX_CONTINUATIONS;
  const isStaged = isStagedMode(config);
  const requestFile = process.env.GH_AW_CONTINUATION_REQUEST_FILE || DEFAULT_CONTINUATION_REQUEST_FILE;
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
        core.warning(`Continuation limit reached (${depth} of ${maxContinuations}); not requesting another run`);
        return { success: true, skipped: true, skipReason: `Continuation limit of ${maxContinuations} reached`, depth };
      }

      const nextDepth = depth + 1;
      if (isStaged) {
        logStagedPreviewInfo(`Would request continuation ${nextDepth} of ${maxContinuations} through the continuation relay`);
        return { success: true, staged: true, depth: nextDepth };
      }

      const request = {
        version: CONTINUATION_REQUEST_VERSION,
        hints,
        depth: nextDepth,
        max: maxContinuations,
        parent_run_id: String(context.runId),
        parent_run_attempt: String(process.env.GITHUB_RUN_ATTEMPT || "1"),
        aw_context: buildAwContext(),
      };
      fs.mkdirSync(path.dirname(requestFile), { recursive: true });
      fs.writeFileSync(requestFile, JSON.stringify(request), { mode: 0o600 });
      core.setOutput("continuation_requested", "true");
      core.info(`✓ Recorded continuation request ${nextDepth} of ${maxContinuations}; the conclusion job hands it to the continuation relay`);
      return { success: true, depth: nextDepth };
    } catch (error) {
      const errorMessage = getErrorMessage(error);
      core.error(`Failed to request continuation: ${errorMessage}`);
      return { success: false, error: `Failed to request continuation: ${errorMessage}` };
    }
  };
}

module.exports = { main, HANDLER_TYPE, readContinuationDepth, DEFAULT_CONTINUATION_REQUEST_FILE };
