import { describe, it, expect } from "vitest";

describe("git_helpers.cjs", () => {
  describe("execGitSync", () => {
    it("should export execGitSync function", async () => {
      const { execGitSync } = await import("./git_helpers.cjs");
      expect(typeof execGitSync).toBe("function");
    });

    it("should execute git commands safely", async () => {
      const { execGitSync } = await import("./git_helpers.cjs");

      // Test with a simple git command that should work
      const result = execGitSync(["--version"]);
      expect(result).toContain("git version");
    });

    it("should handle git command failures", async () => {
      const { execGitSync } = await import("./git_helpers.cjs");

      // Test with an invalid git command
      expect(() => {
        execGitSync(["invalid-command"]);
      }).toThrow();
    });

    it("should prevent shell injection in branch names", async () => {
      const { execGitSync } = await import("./git_helpers.cjs");

      // Test with malicious branch name
      const maliciousBranch = "feature; rm -rf /";

      // This should fail because the branch doesn't exist,
      // but importantly, it should NOT execute "rm -rf /"
      expect(() => {
        execGitSync(["rev-parse", maliciousBranch]);
      }).toThrow();
    });

    it("should treat special characters as literals", async () => {
      const { execGitSync } = await import("./git_helpers.cjs");

      const specialBranches = ["feature && echo hacked", "feature | cat /etc/passwd", "feature$(whoami)", "feature`whoami`"];

      for (const branch of specialBranches) {
        // All should fail with git error, not execute shell commands
        expect(() => {
          execGitSync(["rev-parse", branch]);
        }).toThrow();
      }
    });

    it("should pass options to spawnSync", async () => {
      const { execGitSync } = await import("./git_helpers.cjs");

      // Test that options are properly passed through
      const result = execGitSync(["--version"], { encoding: "utf8" });
      expect(typeof result).toBe("string");
      expect(result).toContain("git version");
    });

    it("should return stdout from successful commands", async () => {
      const { execGitSync } = await import("./git_helpers.cjs");

      // Use git --version which always succeeds
      const result = execGitSync(["--version"]);
      expect(typeof result).toBe("string");
      expect(result).toContain("git version");
    });

    it("should redact credentials from logged commands", async () => {
      const { execGitSync } = await import("./git_helpers.cjs");

      // Mock core.debug to capture logged output
      const debugLogs = [];
      const originalCore = global.core;
      global.core = {
        debug: msg => debugLogs.push(msg),
        error: () => {},
      };

      try {
        // Try a git command with a URL containing credentials (will fail but that's ok)
        try {
          execGitSync(["fetch", "https://user:token@github.com/repo.git", "main"]);
        } catch (e) {
          // Expected to fail, we're just checking the logging
        }

        // Check that credentials were redacted in the log
        const fetchLog = debugLogs.find(log => log.includes("git fetch"));
        expect(fetchLog).toBeDefined();
        expect(fetchLog).toContain("https://***@github.com/repo.git");
        expect(fetchLog).not.toContain("user:token");
      } finally {
        global.core = originalCore;
      }
    });
  });
});
