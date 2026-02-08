import { describe, it, expect } from "vitest";

describe("pr_helpers.cjs", () => {
  let detectForkPR;

  // Import the helper before each test
  beforeEach(async () => {
    const helpers = await import("./pr_helpers.cjs");
    detectForkPR = helpers.detectForkPR;
  });

  describe("detectForkPR", () => {
    it("should detect fork using GitHub's fork flag", () => {
      const pullRequest = {
        head: {
          repo: {
            fork: true,
            full_name: "test-owner/test-repo",
          },
        },
        base: {
          repo: {
            full_name: "test-owner/test-repo",
          },
        },
      };

      const result = detectForkPR(pullRequest);

      expect(result.isFork).toBe(true);
      expect(result.reason).toBe("head.repo.fork flag is true");
    });

    it("should detect fork using different repository names", () => {
      const pullRequest = {
        head: {
          repo: {
            fork: false,
            full_name: "fork-owner/test-repo",
          },
        },
        base: {
          repo: {
            full_name: "original-owner/test-repo",
          },
        },
      };

      const result = detectForkPR(pullRequest);

      expect(result.isFork).toBe(true);
      expect(result.reason).toBe("different repository names");
    });

    it("should detect deleted fork (null head repo)", () => {
      const pullRequest = {
        head: {
          // repo is missing/null
        },
        base: {
          repo: {
            full_name: "original-owner/test-repo",
          },
        },
      };

      const result = detectForkPR(pullRequest);

      expect(result.isFork).toBe(true);
      expect(result.reason).toBe("head repository deleted (was likely a fork)");
    });

    it("should detect non-fork when repos match and fork flag is false", () => {
      const pullRequest = {
        head: {
          repo: {
            fork: false,
            full_name: "test-owner/test-repo",
          },
        },
        base: {
          repo: {
            full_name: "test-owner/test-repo",
          },
        },
      };

      const result = detectForkPR(pullRequest);

      expect(result.isFork).toBe(false);
      expect(result.reason).toBe("same repository");
    });

    it("should handle missing fork flag with same repo names", () => {
      const pullRequest = {
        head: {
          repo: {
            // fork flag not present
            full_name: "test-owner/test-repo",
          },
        },
        base: {
          repo: {
            full_name: "test-owner/test-repo",
          },
        },
      };

      const result = detectForkPR(pullRequest);

      expect(result.isFork).toBe(false);
      expect(result.reason).toBe("same repository");
    });

    it("should prioritize fork flag over repository name comparison", () => {
      // Edge case: fork flag is true even though names match
      // This could happen if a user forks and keeps the same name
      const pullRequest = {
        head: {
          repo: {
            fork: true,
            full_name: "test-owner/test-repo",
          },
        },
        base: {
          repo: {
            full_name: "test-owner/test-repo",
          },
        },
      };

      const result = detectForkPR(pullRequest);

      expect(result.isFork).toBe(true);
      expect(result.reason).toBe("head.repo.fork flag is true");
    });

    it("should handle null base repo gracefully", () => {
      const pullRequest = {
        head: {
          repo: {
            fork: false,
            full_name: "test-owner/test-repo",
          },
        },
        base: {
          // repo is missing/null
        },
      };

      const result = detectForkPR(pullRequest);

      // When base.repo is null, comparison with undefined returns true (different)
      expect(result.isFork).toBe(true);
      expect(result.reason).toBe("different repository names");
    });

    it("should handle both repos being null", () => {
      const pullRequest = {
        head: {
          // repo is missing/null
        },
        base: {
          // repo is missing/null
        },
      };

      const result = detectForkPR(pullRequest);

      // Deleted fork takes precedence
      expect(result.isFork).toBe(true);
      expect(result.reason).toBe("head repository deleted (was likely a fork)");
    });
  });
});
