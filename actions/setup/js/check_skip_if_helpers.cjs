// @ts-check
/// <reference types="@actions/github-script" />

const { getErrorMessage } = require("./error_helpers.cjs");
const { ERR_API, ERR_CONFIG } = require("./error_codes.cjs");
const { writeDenialSummary } = require("./pre_activation_summary.cjs");
const { countSearchMatches } = require("./skip_if_query.cjs");

/**
 * Builds the GitHub search query, optionally scoping it to the current repository.
 * @param {string} skipQuery - The base query string
 * @param {string|undefined} skipScope - The scope setting ('none' to disable repo scoping)
 * @returns {string} The final search query
 */
function buildSearchQuery(skipQuery, skipScope) {
  if (skipScope === "none") {
    core.info(`Using raw query (scope: none): ${skipQuery}`);
    return skipQuery;
  }
  const { owner, repo } = context.repo;
  const searchQuery = `${skipQuery} repo:${owner}/${repo}`;
  core.info(`Scoped query: ${searchQuery}`);
  return searchQuery;
}

/**
 * Shared runner for skip-if query gates.
 * @param {{
 *   skipQuery: string | undefined;
 *   workflowName: string | undefined;
 *   thresholdStr: string | undefined;
 *   thresholdEnvVar: string;
 *   thresholdLabel: string;
 *   checkLabel: string;
 *   outputName: string;
 *   skipScope: string | undefined;
 *   shouldSkip: (totalCount: number, threshold: number) => boolean;
 *   warningMessage: (totalCount: number, threshold: number) => string;
 *   successMessage: (totalCount: number, threshold: number) => string;
 *   denialSummaryMessage: (totalCount: number, threshold: number) => string;
 *   denialSummaryNextStep: string;
 * }} options
 */
// Ambient globals provided by @actions/github-script: core, github, context
async function runSkipQueryGate(options) {
  // prettier-ignore
  const {
    skipQuery, workflowName, thresholdStr,
    thresholdEnvVar, thresholdLabel, checkLabel, outputName, skipScope,
    shouldSkip, warningMessage, successMessage,
    denialSummaryMessage, denialSummaryNextStep,
  } = options;

  if (!skipQuery) {
    core.setFailed(`${ERR_CONFIG}: Configuration error: GH_AW_SKIP_QUERY not specified.`);
    return;
  }

  if (!workflowName) {
    core.setFailed(`${ERR_CONFIG}: Configuration error: GH_AW_WORKFLOW_NAME not specified.`);
    return;
  }

  core.info(`Running ${checkLabel} gate for workflow: ${workflowName}`);

  const threshold = parseInt(thresholdStr ?? "", 10);
  if (Number.isNaN(threshold) || threshold < 1) {
    core.setFailed(`${ERR_CONFIG}: Configuration error: ${thresholdEnvVar} must be a positive integer, got "${thresholdStr}".`);
    return;
  }

  core.info(`Checking ${checkLabel} query: ${skipQuery}`);
  core.info(`${thresholdLabel}: ${threshold}`);

  const searchQuery = buildSearchQuery(skipQuery, skipScope);

  try {
    const { totalCount, exact, backend } = await countSearchMatches(searchQuery, threshold);

    core.info(`Query matched ${exact ? "" : "at least "}${totalCount} items (via ${backend} API)`);

    if (shouldSkip(totalCount, threshold)) {
      core.warning(warningMessage(totalCount, threshold));
      core.setOutput(outputName, "false");
      await writeDenialSummary(denialSummaryMessage(totalCount, threshold), denialSummaryNextStep);
      return;
    }

    core.info(successMessage(totalCount, threshold));
    core.setOutput(outputName, "true");
  } catch (error) {
    core.setFailed(`${ERR_API}: Failed to execute search query: ${getErrorMessage(error)}`);
  }
}

module.exports = { buildSearchQuery, runSkipQueryGate };
