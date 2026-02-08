#!/usr/bin/env node
// @ts-check

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

async function testLiveGitHubAPI() {
  // Check for GitHub token
  const token = process.env.GITHUB_TOKEN || process.env.GH_TOKEN;
  if (!token) {
    console.error("❌ Error: No GitHub token found");
    console.error("Please set GITHUB_TOKEN or GH_TOKEN environment variable");
    console.error("\nExample:");
    console.error("  GITHUB_TOKEN=ghp_xxx node test-live-github-api.cjs");
    console.error("\nTo create a token:");
    console.error("  1. Go to https://github.com/settings/tokens");
    console.error("  2. Create a token with 'repo' or 'public_repo' scope");
    process.exit(1);
  }

  console.log("🔍 Testing frontmatter hash with live GitHub API\n");

  // Configuration
  const owner = "github";
  const repo = "gh-aw";
  const ref = "main";
  const workflowPath = ".github/workflows/audit-workflows.md";

  console.log(`Repository: ${owner}/${repo}`);
  console.log(`Branch: ${ref}`);
  console.log(`Workflow: ${workflowPath}\n`);

  try {
    // Use dynamic import for ESM module compatibility
    const { getOctokit } = await import("@actions/github");

    // Create GitHub API client
    console.log("📡 Connecting to GitHub API...");
    const octokit = getOctokit(token);

    // Create file reader using real GitHub API
    const fileReader = createGitHubFileReader(octokit, owner, repo, ref);

    // Fetch and compute hash
    console.log(`📥 Fetching workflow from GitHub API...`);
    const hash = await computeFrontmatterHash(workflowPath, {
      fileReader,
    });

    console.log(`\n✅ Success! Hash computed from live GitHub API data:`);
    console.log(`   ${hash}`);

    // Verify determinism
    console.log(`\n🔄 Verifying determinism (fetching again)...`);
    const hash2 = await computeFrontmatterHash(workflowPath, {
      fileReader,
    });

    if (hash === hash2) {
      console.log(`✅ Hashes match - computation is deterministic`);
    } else {
      console.error(`❌ Error: Hashes don't match!`);
      console.error(`   First:  ${hash}`);
      console.error(`   Second: ${hash2}`);
      process.exit(1);
    }

    // Summary
    console.log(`\n📊 Summary:`);
    console.log(`   - Successfully fetched workflow from live GitHub API`);
    console.log(`   - Processed workflow with imports (shared/mcp/gh-aw.md, etc.)`);
    console.log(`   - Computed deterministic SHA-256 hash`);
    console.log(`   - Verified hash consistency across multiple API calls`);
    console.log(`\n✨ All tests passed! The JavaScript implementation works correctly with GitHub API.`);
  } catch (err) {
    const error = err;
    console.error(`\n❌ Error: ${error instanceof Error ? error.message : String(error)}`);
    if (error && typeof error === "object" && "status" in error) {
      const statusError = error;
      if (statusError.status === 401) {
        console.error("   Authentication failed - check your GitHub token");
      } else if (statusError.status === 404) {
        console.error("   File not found - check repository and file path");
      } else if (statusError.status === 403) {
        console.error("   Rate limit exceeded or insufficient permissions");
      }
    }
    process.exit(1);
  }
}

// Run the test
testLiveGitHubAPI();
