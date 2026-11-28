// @ts-check
/// <reference types="@actions/github-script" />

const { execSync } = require("child_process");
const { ERR_CONFIG } = require("./error_codes.cjs");
const { getSetupTimeoutMs } = require("./child_process_timeouts.cjs");

const GIT_BRANCH_TIMEOUT_MS = getSetupTimeoutMs("gitBranch");

/**
 * Get the current git branch name
 * @param {string} [customCwd] - Optional custom working directory for git commands
 * @returns {string} The current branch name
 */
function getCurrentBranch(customCwd) {
  // Priority 1: Try git command first to get the actual checked-out branch
  // This is more reliable than environment variables which may not reflect
  // branch changes made during the workflow execution
  const cwd = customCwd || process.env.GITHUB_WORKSPACE || process.cwd();
  try {
    const ref = execSync("git symbolic-ref --quiet HEAD", {
      encoding: "utf8",
      cwd: cwd,
      stdio: ["pipe", "pipe", "pipe"],
      timeout: GIT_BRANCH_TIMEOUT_MS,
    }).trim();
    if (ref.startsWith("refs/heads/")) {
      return ref.slice("refs/heads/".length);
    }
  } catch (error) {
    // Ignore error and try fallback
  }

  // Priority 2: Fallback to GitHub Actions environment variables
  // GITHUB_HEAD_REF is set for pull_request events and contains the source branch name
  // GITHUB_REF_NAME is set for all events and contains the branch/tag name
  const ghHeadRef = process.env.GITHUB_HEAD_REF;
  const ghRefName = process.env.GITHUB_REF_NAME;

  if (ghHeadRef) {
    return ghHeadRef;
  }

  if (ghRefName) {
    return ghRefName;
  }

  throw new Error(
    `${ERR_CONFIG}: Failed to determine current branch: git command returned a detached-HEAD state and no GitHub environment variables (GITHUB_HEAD_REF / GITHUB_REF_NAME) are available. Ensure the workflow checks out the pull request's head ref before calling this step.`
  );
}

module.exports = {
  getCurrentBranch,
};
