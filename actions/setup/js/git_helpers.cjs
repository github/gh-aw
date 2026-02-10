// @ts-check
/// <reference types="@actions/github-script" />

const { spawnSync } = require("child_process");

/**
 * Safely execute git command using spawnSync with args array to prevent shell injection
 * @param {string[]} args - Git command arguments
 * @param {Object} options - Spawn options
 * @returns {string} Command output
 * @throws {Error} If command fails
 */
function execGitSync(args, options = {}) {
  // Log the git command being executed for debugging (but redact credentials)
  const gitCommand = `git ${args
    .map(arg => {
      // Redact credentials in URLs
      if (typeof arg === "string" && arg.includes("://") && arg.includes("@")) {
        return arg.replace(/(https?:\/\/)[^@]+@/, "$1***@");
      }
      return arg;
    })
    .join(" ")}`;

  if (typeof core !== "undefined" && core.debug) {
    core.debug(`Executing git command: ${gitCommand}`);
  }

  const result = spawnSync("git", args, {
    encoding: "utf8",
    ...options,
  });

  if (result.error) {
    if (typeof core !== "undefined" && core.error) {
      core.error(`Git command failed with error: ${result.error.message}`);
    }
    throw result.error;
  }

  if (result.status !== 0) {
    const errorMsg = result.stderr || `Git command failed with status ${result.status}`;
    if (typeof core !== "undefined" && core.error) {
      core.error(`Git command failed: ${gitCommand}`);
      core.error(`Exit status: ${result.status}`);
      if (result.stderr) {
        core.error(`Stderr: ${result.stderr}`);
      }
    }
    throw new Error(errorMsg);
  }

  if (typeof core !== "undefined" && core.debug) {
    if (result.stdout) {
      core.debug(`Git command output: ${result.stdout.substring(0, 200)}${result.stdout.length > 200 ? "..." : ""}`);
    } else {
      core.debug("Git command completed successfully with no output");
    }
  }

  return result.stdout;
}

module.exports = {
  execGitSync,
};
