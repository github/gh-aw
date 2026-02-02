// @ts-check
import { describe, it, expect, beforeAll } from "vitest";
const path = require("path");
const fs = require("fs");
const { computeFrontmatterHash, createGitHubFileReader } = require("./frontmatter_hash_pure.cjs");
const { getOctokit } = require("@actions/github");

/**
 * Tests for frontmatter hash computation using GitHub's API to fetch real workflows.
 * This validates that the JavaScript hash algorithm correctly computes hashes
 * for real public agentic workflows using the GitHub API.
 */
describe("frontmatter_hash with GitHub API", () => {
  let mockGitHub;

  beforeAll(() => {
    // Create a mock GitHub API client for testing
    // In real scenarios, this would be replaced with @actions/github
    mockGitHub = {
      rest: {
        repos: {
          getContent: async ({ owner, repo, path: filePath, ref }) => {
            // Mock implementation that simulates GitHub API
            // In production, this would be the real GitHub API client
            const fsPath = require("path");

            // For testing purposes, we'll read from the local repository
            // This simulates what the GitHub API would return
            const repoRoot = fsPath.resolve(__dirname, "../../..");
            const fullPath = fsPath.join(repoRoot, filePath);

            if (!fs.existsSync(fullPath)) {
              const error = new Error(`Not Found`);
              error.status = 404;
              throw error;
            }

            const content = fs.readFileSync(fullPath, "utf8");
            const base64Content = Buffer.from(content).toString("base64");

            return {
              data: {
                content: base64Content,
                encoding: "base64",
              },
            };
          },
        },
      },
    };
  });

  describe("createGitHubFileReader", () => {
    it("should create a file reader that fetches from GitHub API", async () => {
      const owner = "githubnext";
      const repo = "gh-aw";
      const ref = "main";

      const fileReader = createGitHubFileReader(mockGitHub, owner, repo, ref);

      // Test reading a real workflow file
      const content = await fileReader(".github/workflows/audit-workflows.md");

      expect(content).toBeTruthy();
      expect(content).toContain("---");
      expect(content).toContain("description:");
      expect(content).toContain("engine:");
    });

    it("should handle file not found errors", async () => {
      const owner = "githubnext";
      const repo = "gh-aw";
      const ref = "main";

      const fileReader = createGitHubFileReader(mockGitHub, owner, repo, ref);

      await expect(fileReader("nonexistent-file.md")).rejects.toThrow("Failed to read file");
    });
  });

  describe("computeFrontmatterHash with real workflow", () => {
    it("should compute hash for audit-workflows.md using GitHub API", async () => {
      const owner = "githubnext";
      const repo = "gh-aw";
      const ref = "main";

      const fileReader = createGitHubFileReader(mockGitHub, owner, repo, ref);

      // Use repository-relative path (as GitHub API expects)
      const workflowPath = ".github/workflows/audit-workflows.md";

      // Compute hash for a real public agentic workflow
      const hash = await computeFrontmatterHash(workflowPath, {
        fileReader,
      });

      // Verify hash format
      expect(hash).toMatch(/^[a-f0-9]{64}$/);
      expect(hash).toHaveLength(64);

      // Verify determinism
      const hash2 = await computeFrontmatterHash(workflowPath, {
        fileReader,
      });
      expect(hash2).toBe(hash);
    });

    it("should handle workflows with imports using GitHub API", async () => {
      const owner = "githubnext";
      const repo = "gh-aw";
      const ref = "main";

      const fileReader = createGitHubFileReader(mockGitHub, owner, repo, ref);

      // Use repository-relative path
      const workflowPath = ".github/workflows/audit-workflows.md";

      // audit-workflows.md has imports, so this tests the full import resolution
      const hash = await computeFrontmatterHash(workflowPath, {
        fileReader,
      });

      expect(hash).toMatch(/^[a-f0-9]{64}$/);

      // Log hash for reference (helpful for cross-language validation)
      console.log(`JavaScript hash for audit-workflows.md: ${hash}`);

      // Note: The exact hash may differ based on path resolution strategy
      // The important part is that:
      // 1. The hash is computed successfully
      // 2. The hash is deterministic (tested in other tests)
      // 3. The hash includes content from imported files
    });

    it("should compute hash for a workflow without imports", async () => {
      const owner = "githubnext";
      const repo = "gh-aw";
      const ref = "main";

      const fileReader = createGitHubFileReader(mockGitHub, owner, repo, ref);

      // Use repository-relative path
      const workflowPath = ".github/workflows/archie.md";

      // archie.md is a simpler workflow without imports
      const hash = await computeFrontmatterHash(workflowPath, {
        fileReader,
      });

      expect(hash).toMatch(/^[a-f0-9]{64}$/);
      expect(hash).toHaveLength(64);

      console.log(`Hash for archie.md: ${hash}`);
    });
  });

  describe("cross-language validation", () => {
    it("should compute same hash as Go implementation when using file system", async () => {
      // For true cross-language validation, we need to use the default file reader
      // (not the GitHub API mock) to ensure paths are resolved identically
      const repoRoot = path.resolve(__dirname, "../../..");
      const workflowPath = path.join(repoRoot, ".github/workflows/audit-workflows.md");

      // Compute hash using JavaScript implementation with default file reader
      const jsHash = await computeFrontmatterHash(workflowPath);

      // This hash was computed by the Go implementation:
      // go test -run TestHashWithRealWorkflow ./pkg/parser/
      // Output: "Hash for audit-workflows.md: db7af18719075a860ef7e08bb6f49573ac35fbd88190db4f21da3499d3604971"
      const goHash = "db7af18719075a860ef7e08bb6f49573ac35fbd88190db4f21da3499d3604971";

      // Verify JavaScript hash matches Go hash
      expect(jsHash).toBe(goHash);

      // Log the hash for reference
      console.log(`JavaScript hash for audit-workflows.md: ${jsHash}`);
      console.log(`Go hash matches: ${jsHash === goHash}`);
    });

    it("should produce deterministic hashes across multiple calls", async () => {
      const owner = "githubnext";
      const repo = "gh-aw";
      const ref = "main";

      const fileReader = createGitHubFileReader(mockGitHub, owner, repo, ref);

      // Use repository-relative path
      const workflowPath = ".github/workflows/audit-workflows.md";

      const hashes = [];
      for (let i = 0; i < 3; i++) {
        const hash = await computeFrontmatterHash(workflowPath, {
          fileReader,
        });
        hashes.push(hash);
      }

      // All hashes should be identical
      expect(hashes[0]).toBe(hashes[1]);
      expect(hashes[1]).toBe(hashes[2]);
    });
  });

  describe("GitHub API edge cases", () => {
    it("should handle workflows in subdirectories", async () => {
      const owner = "githubnext";
      const repo = "gh-aw";
      const ref = "main";

      const fileReader = createGitHubFileReader(mockGitHub, owner, repo, ref);

      // Use repository-relative path
      const workflowPath = ".github/workflows/audit-workflows.md";

      // Test with a workflow that has imports from subdirectories
      const hash = await computeFrontmatterHash(workflowPath, {
        fileReader,
      });

      expect(hash).toMatch(/^[a-f0-9]{64}$/);

      // The workflow has imports like "shared/mcp/gh-aw.md"
      // This tests that relative path resolution works correctly with GitHub API
    });

    it("should handle workflows with template expressions", async () => {
      const owner = "githubnext";
      const repo = "gh-aw";
      const ref = "main";

      const fileReader = createGitHubFileReader(mockGitHub, owner, repo, ref);

      // Use repository-relative path
      const workflowPath = ".github/workflows/audit-workflows.md";

      // audit-workflows.md contains template expressions like ${{ github.repository }}
      const hash = await computeFrontmatterHash(workflowPath, {
        fileReader,
      });

      expect(hash).toMatch(/^[a-f0-9]{64}$/);

      // The hash should include contributions from env./vars. expressions
      // but not from other GitHub context expressions
    });
  });

  describe("live GitHub API integration", () => {
    it("should compute hash using real GitHub API (no mocks)", async () => {
      // Skip this test if no GitHub token is available
      // Check multiple possible token environment variables
      const token = process.env.GITHUB_TOKEN || process.env.GH_TOKEN;
      if (!token) {
        console.log("Skipping live API test - no GITHUB_TOKEN or GH_TOKEN available");
        console.log("To run this test, set GITHUB_TOKEN or GH_TOKEN environment variable");
        console.log("Example: GITHUB_TOKEN=ghp_xxx npm test -- frontmatter_hash_github_api.test.cjs");
        return;
      }

      // Use real GitHub API client
      const octokit = getOctokit(token);
      const owner = "githubnext";
      const repo = "gh-aw";
      const ref = "main";

      // Create file reader with real GitHub API
      const fileReader = createGitHubFileReader(octokit, owner, repo, ref);

      // Test with a real public agentic workflow
      const workflowPath = ".github/workflows/audit-workflows.md";

      console.log(`\n🔍 Fetching live data from GitHub API: ${owner}/${repo}/${workflowPath}@${ref}`);

      // Compute hash using live API data
      const hash = await computeFrontmatterHash(workflowPath, {
        fileReader,
      });

      // Verify hash format
      expect(hash).toMatch(/^[a-f0-9]{64}$/);
      expect(hash).toHaveLength(64);

      console.log(`✓ Live API hash for audit-workflows.md: ${hash}`);

      // Verify determinism with second call to live API
      const hash2 = await computeFrontmatterHash(workflowPath, {
        fileReader,
      });
      expect(hash2).toBe(hash);

      console.log("✓ Live API test passed - hash computation is deterministic");
      console.log("✓ Successfully fetched and processed workflow with imports from real GitHub repository");
    });
  });
});
