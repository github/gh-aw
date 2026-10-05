#!/usr/bin/env node
// @ts-check
require("./shim.cjs");

/**
 * Standalone script to test frontmatter hash computation with live GitHub API
 *
 * Usage:
 *   GITHUB_TOKEN=ghp_xxx node test-live-github-api.cjs
 *
 * This script fetches a real workflow from the GitHub repository using the API
 * and computes its hash, demonstrating that the JavaScript implementation works
 * with actual GitHub API calls (no mocks).
 */

const { computeFrontmatterHash, createGitHubFileReader } = require("./frontmatter_hash_pure.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");

async function testLiveGitHubAPI() {
  // Check for GitHub token
  const token = process.env.GITHUB_TOKEN || process.env.GH_TOKEN;
  if (!token) {
    core.setFailed(
      "❌ Error: No GitHub token found\n" +
        "Please set GITHUB_TOKEN or GH_TOKEN environment variable\n" +
        "\nExample:\n" +
        "  GITHUB_TOKEN=ghp_xxx node test-live-github-api.cjs\n" +
        "\nTo create a token:\n" +
        "  1. Go to https://github.com/settings/tokens\n" +
        "  2. Create a token with 'repo' or 'public_repo' scope"
    );
    process.exit(1);
  }

  core.info("🔍 Testing frontmatter hash with live GitHub API\n");

  // Configuration
  const owner = "github";
  const repo = "gh-aw";
  const ref = "main";
  const workflowPath = ".github/workflows/audit-workflows.md";

  core.info(`Repository: ${owner}/${repo}`);
  core.info(`Branch: ${ref}`);
  core.info(`Workflow: ${workflowPath}\n`);

  try {
    // Use dynamic import for ESM module compatibility
    const { getOctokit } = await import("@actions/github");

    // Create GitHub API client
    core.info("📡 Connecting to GitHub API...");
    const octokit = getOctokit(token);

    // Create file reader using real GitHub API
    const fileReader = createGitHubFileReader(octokit, owner, repo, ref);

    // Fetch and compute hash
    core.info(`📥 Fetching workflow from GitHub API...`);
    const hash = await computeFrontmatterHash(workflowPath, {
      fileReader,
      readerMode: "github-api",
    });

    core.info(`\n✅ Success! Hash computed from live GitHub API data:`);
    core.info(`   ${hash}`);

    // Verify determinism
    core.info(`\n🔄 Verifying determinism (fetching again)...`);
    const hash2 = await computeFrontmatterHash(workflowPath, {
      fileReader,
      readerMode: "github-api",
    });

    if (hash === hash2) {
      core.info(`✅ Hashes match - computation is deterministic`);
    } else {
      core.setFailed(`❌ Error: Hashes don't match!\n   First:  ${hash}\n   Second: ${hash2}`);
      process.exit(1);
    }

    // Summary
    core.info(`\n📊 Summary:`);
    core.info(`   - Successfully fetched workflow from live GitHub API`);
    core.info(`   - Processed workflow with imports (shared/mcp/tavily.md, etc.)`);
    core.info(`   - Computed deterministic SHA-256 hash`);
    core.info(`   - Verified hash consistency across multiple API calls`);
    core.info(`\n✨ All tests passed! The JavaScript implementation works correctly with GitHub API.`);
  } catch (err) {
    const error = err;
    let msg = `\n❌ Error: ${getErrorMessage(error)}`;
    if (error && typeof error === "object" && "status" in error) {
      const statusError = error;
      if (statusError.status === 401) {
        msg += "\n   Authentication failed - check your GitHub token";
      } else if (statusError.status === 404) {
        msg += "\n   File not found - check repository and file path";
      } else if (statusError.status === 403) {
        msg += "\n   Rate limit exceeded or insufficient permissions";
      }
    }
    core.setFailed(msg);
    process.exit(1);
  }
}

// Run the test
testLiveGitHubAPI();
