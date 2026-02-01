// @ts-check
/// <reference types="@actions/github-script" />

/**
 * GitHub API helper functions
 * Provides common GitHub API operations with consistent error handling
 */

const { getErrorMessage } = require("./error_helpers.cjs");

/**
 * Get file content from GitHub repository using the API
 * @param {Object} github - GitHub API client (@actions/github)
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {string} path - File path within the repository
 * @param {string} ref - Git reference (branch, tag, or commit SHA)
 * @returns {Promise<string|null>} File content as string, or null if not found/error
 */
async function getFileContent(github, owner, repo, path, ref) {
  try {
    const response = await github.rest.repos.getContent({
      owner,
      repo,
      path,
      ref,
    });

    // Handle case where response is an array (directory listing)
    if (Array.isArray(response.data)) {
      core.info(`Path ${path} is a directory, not a file`);
      return null;
    }

    // Check if this is a file (not a symlink or submodule)
    if (response.data.type !== "file") {
      core.info(`Path ${path} is not a file (type: ${response.data.type})`);
      return null;
    }

    // Decode base64 content
    if (response.data.encoding === "base64" && response.data.content) {
      return Buffer.from(response.data.content, "base64").toString("utf8");
    }

    return response.data.content || null;
  } catch (error) {
    const errorMessage = getErrorMessage(error);
    core.info(`Could not fetch content for ${path}: ${errorMessage}`);
    return null;
  }
}

module.exports = {
  getFileContent,
};
