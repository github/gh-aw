// @ts-check
/// <reference types="@actions/github-script" />

const { iterateJsonlEvents } = require("./jsonl_helpers.cjs");
/**
 * load_experiment_state_from_repo
 *
 * Fetches the experiment state file from a git branch using the GitHub API and writes
 * it to the local experiments directory so that pick_experiment.cjs can read it.
 *
 * Falls back gracefully to an empty state when the branch or file does not yet exist
 * (first run), or when any other error occurs while fetching the file.
 *
 * Environment variables (set by the compiled workflow step):
 *   GH_AW_EXPERIMENT_STATE_FILE - Absolute path to the local state file to write
 *                                  e.g. /tmp/gh-aw/experiments/state.jsonl
 *   GH_AW_EXPERIMENT_STATE_DIR  - Directory that holds the state file (created if missing)
 *                                  e.g. /tmp/gh-aw/experiments
 *   GH_AW_EXPERIMENT_BRANCH     - Git branch name to fetch state from
 *                                  e.g. experiments/myworkflow
 */

const fs = require("fs");
const path = require("path");
const { getErrorMessage } = require("./error_helpers.cjs");
const { ERR_SYSTEM, ERR_API } = require("./error_codes.cjs");

const MAX_STATE_FILE_BYTES = 102400;
// Keep this allowlist aligned with actions/setup/js/normalize_branch_name.cjs valid characters.
const BRANCH_NAME_PATTERN = /^[A-Za-z0-9._/-]+$/;
const REPOSITORY_PATTERN = /^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/;

function isExperimentStateContentValid(content) {
  try {
    const parsed = JSON.parse(content);
    return !!parsed && typeof parsed.counts === "object";
  } catch {
    // Fall through to JSONL state validation.
  }

  try {
    for (const event of iterateJsonlEvents(content)) {
      if (event.type === "invalid") {
        if (event.reason === "blank") continue;
        return false;
      }
      const entry = event.value;
      const isSnapshot = !!entry && typeof entry.counts === "object";
      const isRunRecord = !!entry && typeof entry.run_id === "string" && typeof entry.timestamp === "string" && entry.assignments && typeof entry.assignments === "object" && !Array.isArray(entry.assignments);
      if (!isSnapshot && !isRunRecord) {
        return false;
      }
    }
    return true;
  } catch {
    return false;
  }
}

/**
 * Returns true when decoded state content exceeds allowed byte length.
 *
 * @param {string} content
 * @param {number} maxBytes
 * @returns {boolean}
 */
function checkLimit(content, maxBytes) {
  return content.length > maxBytes;
}

/**
 * Validate required input values before any API calls.
 *
 * @param {string} branch
 * @param {string} owner
 * @param {string} repo
 * @param {string} repository
 * @returns {{valid: boolean, error?: string}}
 */
function validateInputs(branch, owner, repo, repository) {
  if (!branch) {
    return { valid: false, error: "GH_AW_EXPERIMENT_BRANCH is not set" };
  }

  if (!BRANCH_NAME_PATTERN.test(branch)) {
    return { valid: false, error: "GH_AW_EXPERIMENT_BRANCH contains invalid characters" };
  }

  if (branch.includes("..")) {
    return { valid: false, error: "GH_AW_EXPERIMENT_BRANCH contains invalid characters" };
  }

  if (!REPOSITORY_PATTERN.test(repository)) {
    return { valid: false, error: "GITHUB_REPOSITORY is not set or invalid" };
  }

  return { valid: true };
}

/**
 * Fetch experiment state from the git branch via the GitHub API.
 * Returns the raw file content as a string, or null when the branch/file is absent.
 *
 * @param {any} octokit - Authenticated Octokit instance
 * @param {string} owner - Repository owner
 * @param {string} repo  - Repository name
 * @param {string} branch - Branch name (e.g. "experiments/myworkflow")
 * @param {string} filePath - File path within the branch (e.g. "state.json")
 * @returns {Promise<string|null>}
 */
async function fetchFileFromBranch(octokit, owner, repo, branch, filePath) {
  try {
    const response = await octokit.rest.repos.getContent({
      owner,
      repo,
      path: filePath,
      ref: branch,
    });
    const data = response.data;
    if (Array.isArray(data) || data.type !== "file" || !data.content) {
      return null;
    }
    // GitHub API returns base64-encoded content.
    return Buffer.from(data.content, "base64").toString("utf8");
  } catch (/** @type {any} */ err) {
    // 404 means the branch or file does not exist yet – that is normal on first run.
    const errAny = /** @type {any} */ err;
    if (errAny.status === 404) {
      return null;
    }
    throw new Error(`${ERR_API}: Failed to fetch file "${filePath}" from branch "${branch}": ${getErrorMessage(err)}`, { cause: err });
  }
}

/**
 * Main entry point called by the actions/github-script step.
 */
async function main() {
  const stateFile = process.env.GH_AW_EXPERIMENT_STATE_FILE || require("./runtime_paths.cjs").runtimePath("experiments/state.jsonl");
  const stateDir = process.env.GH_AW_EXPERIMENT_STATE_DIR || require("./runtime_paths.cjs").runtimePath("experiments");
  const branch = process.env.GH_AW_EXPERIMENT_BRANCH || "";
  const repository = process.env.GITHUB_REPOSITORY || "";
  const [owner, repo] = repository.split("/");

  const validationResult = validateInputs(branch, owner, repo, repository);
  if (!validationResult.valid) {
    core.warning(`${validationResult.error} – starting with empty experiment state`);
    try {
      fs.mkdirSync(stateDir, { recursive: true });
    } catch (err) {
      throw new Error(`${ERR_SYSTEM}: Failed to create directory ${stateDir}: ${getErrorMessage(err)}`, { cause: err });
    }
    return;
  }

  // Use the authenticated `github` client provided by actions/github-script (via setupGlobals).
  // This avoids requiring GITHUB_TOKEN to be explicitly set in the step env.
  const octokit = github;
  const stateFileName = path.basename(stateFile);
  const stateFileCandidates = Array.from(new Set(stateFileName === "state.json" ? ["state.jsonl", "state.json"] : stateFileName === "state.jsonl" ? ["state.jsonl", "state.json"] : [stateFileName]));

  core.info(`Loading experiment state from branch "${branch}" (file: ${stateFileCandidates.join(" or ")})`);

  /** @type {any} */
  let content = null;
  try {
    for (const candidate of stateFileCandidates) {
      content = await fetchFileFromBranch(octokit, owner, repo, branch, candidate);
      if (content !== null) {
        break;
      }
    }
  } catch (/** @type {any} */ err) {
    core.warning(`Failed to fetch experiment state from branch "${branch}": ${getErrorMessage(err)} – starting fresh`);
  }

  // Ensure the directory exists regardless of whether we fetched the file.
  try {
    fs.mkdirSync(stateDir, { recursive: true });
  } catch (err) {
    throw new Error(`${ERR_SYSTEM}: Failed to create directory ${stateDir}: ${getErrorMessage(err)}`, { cause: err });
  }

  if (content === null) {
    core.info(`No experiment state found in branch "${branch}" – starting with empty state`);
    return;
  }

  if (checkLimit(content, MAX_STATE_FILE_BYTES)) {
    core.warning(`Experiment state file exceeds max limit (${MAX_STATE_FILE_BYTES} bytes) – starting fresh`);
    return;
  }

  if (!isExperimentStateContentValid(content)) {
    core.warning(`Experiment state in branch "${branch}" could not be parsed – starting fresh`);
    return;
  }

  try {
    fs.writeFileSync(stateFile, content, "utf8");
  } catch (err) {
    throw new Error(`${ERR_SYSTEM}: Failed to write file ${stateFile}: ${getErrorMessage(err)}`, { cause: err });
  }
  core.info(`Experiment state written to ${stateFile}`);
}

module.exports = { main, fetchFileFromBranch, validateInputs };
