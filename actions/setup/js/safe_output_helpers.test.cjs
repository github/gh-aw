import { describe, it, expect, beforeEach, vi } from "vitest";

describe("safe_output_helpers", () => {
  let helpers;

  beforeEach(async () => {
    helpers = await import("./safe_output_helpers.cjs");
  });

  describe("parseAllowedItems", () => {
    it("should return undefined for empty string", () => {
      expect(helpers.parseAllowedItems("")).toBeUndefined();
    });

    it("should return undefined for whitespace-only string", () => {
      expect(helpers.parseAllowedItems("   ")).toBeUndefined();
    });

    it("should return undefined for undefined input", () => {
      expect(helpers.parseAllowedItems(undefined)).toBeUndefined();
    });

    it("should parse single item", () => {
      expect(helpers.parseAllowedItems("bug")).toEqual(["bug"]);
    });

    it("should parse multiple items", () => {
      expect(helpers.parseAllowedItems("bug,enhancement,feature")).toEqual(["bug", "enhancement", "feature"]);
    });

    it("should trim whitespace from items", () => {
      expect(helpers.parseAllowedItems(" bug , enhancement , feature ")).toEqual(["bug", "enhancement", "feature"]);
    });

    it("should filter out empty entries", () => {
      expect(helpers.parseAllowedItems("bug,,enhancement,")).toEqual(["bug", "enhancement"]);
    });
  });

  describe("parseMaxCount", () => {
    it("should return default value when env is undefined", () => {
      const result = helpers.parseMaxCount(undefined);
      expect(result.valid).toBe(true);
      expect(result.value).toBe(3);
    });

    it("should return custom default value", () => {
      const result = helpers.parseMaxCount(undefined, 5);
      expect(result.valid).toBe(true);
      expect(result.value).toBe(5);
    });

    it("should parse valid positive integer", () => {
      const result = helpers.parseMaxCount("10");
      expect(result.valid).toBe(true);
      expect(result.value).toBe(10);
    });

    it("should return error for non-numeric string", () => {
      const result = helpers.parseMaxCount("invalid");
      expect(result.valid).toBe(false);
      expect(result.error).toContain("Invalid max value");
    });

    it("should return error for zero", () => {
      const result = helpers.parseMaxCount("0");
      expect(result.valid).toBe(false);
      expect(result.error).toContain("Invalid max value");
    });

    it("should return error for negative number", () => {
      const result = helpers.parseMaxCount("-5");
      expect(result.valid).toBe(false);
      expect(result.error).toContain("Invalid max value");
    });
  });

  describe("resolveTarget", () => {
    describe("with supportsPR=true (for labels)", () => {
      const baseParams = {
        targetConfig: "triggering",
        item: {},
        context: {
          eventName: "issues",
          payload: { issue: { number: 123 } },
        },
        itemType: "label addition",
        supportsPR: true,
      };

      it("should resolve triggering issue context", () => {
        const result = helpers.resolveTarget(baseParams);
        expect(result.success).toBe(true);
        expect(result.number).toBe(123);
        expect(result.contextType).toBe("issue");
      });

      it("should ignore agent-supplied issue numbers for triggering targets", () => {
        const result = helpers.resolveTarget({
          ...baseParams,
          item: { item_number: 999, issue_number: 999 },
        });
        expect(result.success).toBe(true);
        expect(result.number).toBe(123);
        expect(result.contextType).toBe("issue");
      });

      it("should resolve workflow_dispatch issue context from aw_context", () => {
        const result = helpers.resolveTarget({
          ...baseParams,
          context: {
            eventName: "workflow_dispatch",
            repo: { owner: "test-owner", repo: "test-repo" },
            payload: {
              inputs: {
                aw_context: JSON.stringify({
                  event_type: "issue_comment",
                  item_type: "issue",
                  item_number: 321,
                  repo: "test-owner/test-repo",
                }),
              },
            },
          },
        });
        expect(result.success).toBe(true);
        expect(result.number).toBe(321);
        expect(result.contextType).toBe("issue");
      });

      it("should resolve triggering PR context", () => {
        const result = helpers.resolveTarget({
          ...baseParams,
          context: {
            eventName: "pull_request",
            payload: { pull_request: { number: 456 } },
          },
        });
        expect(result.success).toBe(true);
        expect(result.number).toBe(456);
        expect(result.contextType).toBe("pull request");
      });

      it("should fail when triggering and not in issue/PR context", () => {
        const result = helpers.resolveTarget({
          ...baseParams,
          context: {
            eventName: "push",
            payload: {},
          },
        });
        expect(result.success).toBe(false);
        expect(result.error).toContain('Target is "triggering"');
        expect(result.shouldFail).toBe(false); // Should skip, not fail
      });

      it("should resolve explicit issue number", () => {
        const result = helpers.resolveTarget({
          ...baseParams,
          targetConfig: "999",
        });
        expect(result.success).toBe(true);
        expect(result.number).toBe(999);
        expect(result.contextType).toBe("issue");
      });

      it("should ignore conflicting agent-supplied issue numbers for fixed targets", () => {
        const result = helpers.resolveTarget({
          ...baseParams,
          targetConfig: "999",
          item: { item_number: 123, issue_number: 123 },
        });
        expect(result.success).toBe(true);
        expect(result.number).toBe(999);
        expect(result.contextType).toBe("issue");
      });

      it("should fail for invalid explicit number", () => {
        const result = helpers.resolveTarget({
          ...baseParams,
          targetConfig: "invalid",
        });
        expect(result.success).toBe(false);
        expect(result.error).toContain("Invalid issue or pull request number");
        expect(result.shouldFail).toBe(true);
      });

      it("should resolve wildcard with item_number", () => {
        const result = helpers.resolveTarget({
          ...baseParams,
          targetConfig: "*",
          item: { item_number: 555 },
        });
        expect(result.success).toBe(true);
        expect(result.number).toBe(555);
        expect(result.contextType).toBe("issue");
      });

      it("should fail wildcard without item_number", () => {
        const result = helpers.resolveTarget({
          ...baseParams,
          targetConfig: "*",
          item: {},
        });
        expect(result.success).toBe(false);
        expect(result.error).toContain('Target is "*"');
        expect(result.shouldFail).toBe(true);
      });

      it("should fail for invalid item_number in wildcard", () => {
        const result = helpers.resolveTarget({
          ...baseParams,
          targetConfig: "*",
          item: { item_number: -5 },
        });
        expect(result.success).toBe(false);
        expect(result.error).toContain("Invalid item_number");
        expect(result.shouldFail).toBe(true);
      });

      it("should handle issue_comment event", () => {
        const result = helpers.resolveTarget({
          ...baseParams,
          context: {
            eventName: "issue_comment",
            payload: { issue: { number: 789 } },
          },
        });
        expect(result.success).toBe(true);
        expect(result.number).toBe(789);
        expect(result.contextType).toBe("issue");
      });

      it("should handle pull_request_review event", () => {
        const result = helpers.resolveTarget({
          ...baseParams,
          context: {
            eventName: "pull_request_review",
            payload: { pull_request: { number: 321 } },
          },
        });
        expect(result.success).toBe(true);
        expect(result.number).toBe(321);
        expect(result.contextType).toBe("pull request");
      });

      it("should handle pull_request_target event", () => {
        const result = helpers.resolveTarget({
          ...baseParams,
          context: {
            eventName: "pull_request_target",
            payload: { pull_request: { number: 654 } },
          },
        });
        expect(result.success).toBe(true);
        expect(result.number).toBe(654);
        expect(result.contextType).toBe("pull request");
      });

      it("should fail when issue context but no issue in payload", () => {
        const result = helpers.resolveTarget({
          ...baseParams,
          context: {
            eventName: "issues",
            payload: {},
          },
        });
        expect(result.success).toBe(false);
        expect(result.error).toContain("Issue context detected but no issue found");
        expect(result.shouldFail).toBe(true);
      });

      it("should fail when PR context but no PR in payload", () => {
        const result = helpers.resolveTarget({
          ...baseParams,
          context: {
            eventName: "pull_request",
            payload: {},
          },
        });
        expect(result.success).toBe(false);
        expect(result.error).toContain("Pull request context detected but no pull request found");
        expect(result.shouldFail).toBe(true);
      });
    });

    describe("with supportsPR=false (for reviewers)", () => {
      const baseParams = {
        targetConfig: "triggering",
        item: {},
        context: {
          eventName: "pull_request",
          payload: { pull_request: { number: 123 } },
        },
        itemType: "reviewer addition",
        supportsPR: false,
      };

      it("should resolve triggering PR context", () => {
        const result = helpers.resolveTarget(baseParams);
        expect(result.success).toBe(true);
        expect(result.number).toBe(123);
        expect(result.contextType).toBe("pull request");
      });

      it("should ignore agent-supplied PR numbers for triggering targets", () => {
        const result = helpers.resolveTarget({
          ...baseParams,
          item: { pull_request_number: 999, pr_number: 999, pr: 999, pull_number: 999 },
        });
        expect(result.success).toBe(true);
        expect(result.number).toBe(123);
        expect(result.contextType).toBe("pull request");
      });

      it("should resolve triggering pull_request_target context", () => {
        const result = helpers.resolveTarget({
          ...baseParams,
          context: {
            eventName: "pull_request_target",
            payload: { pull_request: { number: 987 } },
          },
        });
        expect(result.success).toBe(true);
        expect(result.number).toBe(987);
        expect(result.contextType).toBe("pull request");
      });

      it("should resolve issue_comment on PR context for PR-only handlers", () => {
        const result = helpers.resolveTarget({
          ...baseParams,
          context: {
            eventName: "issue_comment",
            payload: {
              issue: {
                number: 246,
                pull_request: { url: "https://api.github.com/repos/o/r/pulls/246" },
              },
            },
          },
        });
        expect(result.success).toBe(true);
        expect(result.number).toBe(246);
        expect(result.contextType).toBe("pull request");
      });

      it("should fail when triggering and not in PR context", () => {
        const result = helpers.resolveTarget({
          ...baseParams,
          context: {
            eventName: "issues",
            payload: { issue: { number: 123 } },
          },
        });
        expect(result.success).toBe(false);
        expect(result.error).toContain('Target is "triggering"');
        expect(result.shouldFail).toBe(false); // Should skip, not fail
      });

      it("should resolve explicit PR number", () => {
        const result = helpers.resolveTarget({
          ...baseParams,
          targetConfig: "456",
        });
        expect(result.success).toBe(true);
        expect(result.number).toBe(456);
        expect(result.contextType).toBe("pull request");
      });

      it("should ignore conflicting agent-supplied PR numbers for fixed targets", () => {
        const result = helpers.resolveTarget({
          ...baseParams,
          targetConfig: "456",
          item: { pull_request_number: 123, pr_number: 123, pr: 123, pull_number: 123 },
        });
        expect(result.success).toBe(true);
        expect(result.number).toBe(456);
        expect(result.contextType).toBe("pull request");
      });

      it("should resolve wildcard with pull_request_number", () => {
        const result = helpers.resolveTarget({
          ...baseParams,
          targetConfig: "*",
          item: { pull_request_number: 789 },
        });
        expect(result.success).toBe(true);
        expect(result.number).toBe(789);
        expect(result.contextType).toBe("pull request");
      });

      it("should resolve wildcard with pr_number alias", () => {
        const result = helpers.resolveTarget({
          ...baseParams,
          targetConfig: "*",
          item: { pr_number: 790 },
        });
        expect(result.success).toBe(true);
        expect(result.number).toBe(790);
        expect(result.contextType).toBe("pull request");
      });

      it("should resolve wildcard with pr alias", () => {
        const result = helpers.resolveTarget({
          ...baseParams,
          targetConfig: "*",
          item: { pr: "791" },
        });
        expect(result.success).toBe(true);
        expect(result.number).toBe(791);
        expect(result.contextType).toBe("pull request");
      });

      it("should resolve wildcard with pull_number alias", () => {
        const result = helpers.resolveTarget({
          ...baseParams,
          targetConfig: "*",
          item: { pull_number: 792 },
        });
        expect(result.success).toBe(true);
        expect(result.number).toBe(792);
        expect(result.contextType).toBe("pull request");
      });

      it("should fail wildcard without pull_request_number", () => {
        const result = helpers.resolveTarget({
          ...baseParams,
          targetConfig: "*",
          item: {},
        });
        expect(result.success).toBe(false);
        expect(result.error).toContain('Target is "*"');
        expect(result.shouldFail).toBe(true);
      });
    });

    describe("target authorization invariants", () => {
      it("should fail closed when a non-wildcard resolved target diverges from the authorized target", () => {
        const result = helpers.assertTargetAuthorizationInvariant({
          targetConfig: "triggering",
          resolvedNumber: 999,
          authorizedNumber: 123,
          itemType: "test operation",
        });
        expect(result.success).toBe(false);
        expect(result.error).toContain("ERR_TARGET_AUTHORIZATION");
        expect(result.shouldFail).toBe(true);
      });

      it("should allow wildcard targets because they are explicitly agent-selected", () => {
        const result = helpers.assertTargetAuthorizationInvariant({
          targetConfig: "*",
          resolvedNumber: 999,
          authorizedNumber: undefined,
          itemType: "test operation",
        });
        expect(result.success).toBe(true);
      });
    });

    describe("default target handling", () => {
      it("should use 'triggering' when targetConfig is undefined", () => {
        const result = helpers.resolveTarget({
          targetConfig: undefined,
          item: {},
          context: {
            eventName: "issues",
            payload: { issue: { number: 999 } },
          },
          itemType: "test",
          supportsPR: true,
        });
        expect(result.success).toBe(true);
        expect(result.number).toBe(999);
      });

      it("should use 'triggering' when targetConfig is empty string", () => {
        const result = helpers.resolveTarget({
          targetConfig: "",
          item: {},
          context: {
            eventName: "issues",
            payload: { issue: { number: 888 } },
          },
          itemType: "test",
          supportsPR: true,
        });
        expect(result.success).toBe(true);
        expect(result.number).toBe(888);
      });
    });

    describe("string number conversion", () => {
      it("should handle string item_number", () => {
        const result = helpers.resolveTarget({
          targetConfig: "*",
          item: { item_number: "123" },
          context: { eventName: "push", payload: {} },
          itemType: "test",
          supportsPR: true,
        });
        expect(result.success).toBe(true);
        expect(result.number).toBe(123);
      });

      it("should handle string pull_request_number", () => {
        const result = helpers.resolveTarget({
          targetConfig: "*",
          item: { pull_request_number: "456" },
          context: { eventName: "push", payload: {} },
          itemType: "test",
          supportsPR: false,
        });
        expect(result.success).toBe(true);
        expect(result.number).toBe(456);
      });

      it("should handle string pr_number alias", () => {
        const result = helpers.resolveTarget({
          targetConfig: "*",
          item: { pr_number: "457" },
          context: { eventName: "push", payload: {} },
          itemType: "test",
          supportsPR: false,
        });
        expect(result.success).toBe(true);
        expect(result.number).toBe(457);
      });
    });

    describe("with supportsIssue=true (for issues-only)", () => {
      const baseParams = {
        targetConfig: "triggering",
        item: {},
        context: {
          eventName: "issues",
          payload: { issue: { number: 123 } },
        },
        itemType: "update_issue",
        supportsIssue: true,
      };

      it("should resolve triggering issue context", () => {
        const result = helpers.resolveTarget(baseParams);
        expect(result.success).toBe(true);
        expect(result.number).toBe(123);
        expect(result.contextType).toBe("issue");
      });

      it("should resolve workflow_dispatch issue context from aw_context", () => {
        const result = helpers.resolveTarget({
          ...baseParams,
          context: {
            eventName: "workflow_dispatch",
            repo: { owner: "test-owner", repo: "test-repo" },
            payload: {
              inputs: {
                aw_context: JSON.stringify({
                  event_type: "issue_comment",
                  item_type: "issue",
                  item_number: 654,
                  repo: "test-owner/test-repo",
                }),
              },
            },
          },
        });
        expect(result.success).toBe(true);
        expect(result.number).toBe(654);
        expect(result.contextType).toBe("issue");
      });

      it("should fail when triggering and not in issue context", () => {
        const result = helpers.resolveTarget({
          ...baseParams,
          context: {
            eventName: "pull_request",
            payload: { pull_request: { number: 456 } },
          },
        });
        expect(result.success).toBe(false);
        expect(result.error).toContain('Target is "triggering"');
        expect(result.error).toContain("not running in issue context");
        expect(result.shouldFail).toBe(false); // Should skip, not fail
      });

      it("should resolve explicit issue number", () => {
        const result = helpers.resolveTarget({
          ...baseParams,
          targetConfig: "789",
        });
        expect(result.success).toBe(true);
        expect(result.number).toBe(789);
        expect(result.contextType).toBe("issue");
      });

      it("should fail for invalid explicit number with correct error message", () => {
        const result = helpers.resolveTarget({
          ...baseParams,
          targetConfig: "event",
        });
        expect(result.success).toBe(false);
        expect(result.error).toContain("Invalid issue number");
        expect(result.error).toContain("GitHub Actions expression that didn't evaluate correctly");
        expect(result.error).toContain("github.event.issue.number");
        expect(result.error).not.toContain("pull request");
        expect(result.shouldFail).toBe(true);
      });

      it("should resolve wildcard with issue_number", () => {
        const result = helpers.resolveTarget({
          ...baseParams,
          targetConfig: "*",
          item: { issue_number: 555 },
        });
        expect(result.success).toBe(true);
        expect(result.number).toBe(555);
        expect(result.contextType).toBe("issue");
      });

      it("should resolve wildcard with item_number", () => {
        const result = helpers.resolveTarget({
          ...baseParams,
          targetConfig: "*",
          item: { item_number: 666 },
        });
        expect(result.success).toBe(true);
        expect(result.number).toBe(666);
        expect(result.contextType).toBe("issue");
      });

      it("should fail wildcard without issue_number or item_number", () => {
        const result = helpers.resolveTarget({
          ...baseParams,
          targetConfig: "*",
          item: {},
        });
        expect(result.success).toBe(false);
        expect(result.error).toContain('Target is "*"');
        expect(result.error).toContain("item_number/issue_number");
        expect(result.error).not.toContain("pull_request_number");
        expect(result.shouldFail).toBe(true);
      });

      it("should ignore pull_request_number for issues-only handlers", () => {
        const result = helpers.resolveTarget({
          ...baseParams,
          targetConfig: "*",
          item: { pull_request_number: 999 },
        });
        expect(result.success).toBe(false);
        expect(result.error).toContain('Target is "*"');
        expect(result.error).toContain("no item_number/issue_number specified");
      });
    });
  });

  describe("loadCustomSafeOutputJobTypes", () => {
    beforeEach(() => {
      // Clean up environment variables
      delete process.env.GH_AW_SAFE_OUTPUT_JOBS;
    });

    it("should return empty set when GH_AW_SAFE_OUTPUT_JOBS is not set", () => {
      const result = helpers.loadCustomSafeOutputJobTypes();

      expect(result).toBeInstanceOf(Set);
      expect(result.size).toBe(0);
    });

    it("should parse and return custom job types from GH_AW_SAFE_OUTPUT_JOBS", () => {
      process.env.GH_AW_SAFE_OUTPUT_JOBS = JSON.stringify({
        notion_add_comment: "comment_url",
        slack_post_message: "message_url",
        custom_job: "output_url",
      });

      const result = helpers.loadCustomSafeOutputJobTypes();

      expect(result).toBeInstanceOf(Set);
      expect(result.size).toBe(3);
      expect(result.has("notion_add_comment")).toBe(true);
      expect(result.has("slack_post_message")).toBe(true);
      expect(result.has("custom_job")).toBe(true);
    });

    it("should return empty set when GH_AW_SAFE_OUTPUT_JOBS is invalid JSON", () => {
      process.env.GH_AW_SAFE_OUTPUT_JOBS = "invalid json";

      const result = helpers.loadCustomSafeOutputJobTypes();

      expect(result).toBeInstanceOf(Set);
      expect(result.size).toBe(0);
      // Note: Warning is logged but we don't test for it since core is not mocked in this test file
    });

    it("should handle empty object in GH_AW_SAFE_OUTPUT_JOBS", () => {
      process.env.GH_AW_SAFE_OUTPUT_JOBS = JSON.stringify({});

      const result = helpers.loadCustomSafeOutputJobTypes();

      expect(result).toBeInstanceOf(Set);
      expect(result.size).toBe(0);
    });
  });

  describe("matchesBlockedPattern", () => {
    it("should match exact username", () => {
      expect(helpers.matchesBlockedPattern("copilot", "copilot")).toBe(true);
    });

    it("should be case-insensitive for exact match", () => {
      expect(helpers.matchesBlockedPattern("Copilot", "copilot")).toBe(true);
      expect(helpers.matchesBlockedPattern("COPILOT", "copilot")).toBe(true);
    });

    it("should not match different usernames", () => {
      expect(helpers.matchesBlockedPattern("alice", "copilot")).toBe(false);
    });

    it("should match wildcard pattern *[bot]", () => {
      expect(helpers.matchesBlockedPattern("dependabot[bot]", "*[bot]")).toBe(true);
      expect(helpers.matchesBlockedPattern("github-actions[bot]", "*[bot]")).toBe(true);
      expect(helpers.matchesBlockedPattern("renovate[bot]", "*[bot]")).toBe(true);
    });

    it("should not match non-bot usernames with *[bot] pattern", () => {
      expect(helpers.matchesBlockedPattern("alice", "*[bot]")).toBe(false);
      expect(helpers.matchesBlockedPattern("bot-user", "*[bot]")).toBe(false);
    });

    it("should match wildcard at end", () => {
      expect(helpers.matchesBlockedPattern("github-actions-bot", "github-*")).toBe(true);
      expect(helpers.matchesBlockedPattern("github-bot", "github-*")).toBe(true);
    });

    it("should match wildcard at start", () => {
      expect(helpers.matchesBlockedPattern("my-bot", "*-bot")).toBe(true);
      expect(helpers.matchesBlockedPattern("github-bot", "*-bot")).toBe(true);
    });

    it("should handle empty or null inputs", () => {
      expect(helpers.matchesBlockedPattern("", "copilot")).toBe(false);
      expect(helpers.matchesBlockedPattern("copilot", "")).toBe(false);
      expect(helpers.matchesBlockedPattern(null, "copilot")).toBe(false);
      expect(helpers.matchesBlockedPattern("copilot", null)).toBe(false);
    });

    it("should escape special regex characters", () => {
      expect(helpers.matchesBlockedPattern("user.name", "user.name")).toBe(true);
      expect(helpers.matchesBlockedPattern("user+test", "user+test")).toBe(true);
    });
  });

  describe("isUsernameBlocked", () => {
    it("should return false for empty blocked list", () => {
      expect(helpers.isUsernameBlocked("copilot", [])).toBe(false);
    });

    it("should return false for undefined blocked list", () => {
      expect(helpers.isUsernameBlocked("copilot", undefined)).toBe(false);
    });

    it("should return true if username matches any pattern", () => {
      const blocked = ["copilot", "*[bot]"];
      expect(helpers.isUsernameBlocked("copilot", blocked)).toBe(true);
      expect(helpers.isUsernameBlocked("dependabot[bot]", blocked)).toBe(true);
    });

    it("should return false if username matches no patterns", () => {
      const blocked = ["copilot", "*[bot]"];
      expect(helpers.isUsernameBlocked("alice", blocked)).toBe(false);
      expect(helpers.isUsernameBlocked("bob", blocked)).toBe(false);
    });

    it("should handle multiple patterns", () => {
      const blocked = ["admin", "copilot", "*[bot]", "test-*"];
      expect(helpers.isUsernameBlocked("admin", blocked)).toBe(true);
      expect(helpers.isUsernameBlocked("copilot", blocked)).toBe(true);
      expect(helpers.isUsernameBlocked("github-actions[bot]", blocked)).toBe(true);
      expect(helpers.isUsernameBlocked("test-user", blocked)).toBe(true);
      expect(helpers.isUsernameBlocked("alice", blocked)).toBe(false);
    });
  });

  describe("resolveIssueNumber", () => {
    it("should return issue number from message.issue_number", () => {
      const result = helpers.resolveIssueNumber({ issue_number: 42 });
      expect(result.success).toBe(true);
      if (result.success) {
        expect(result.issueNumber).toBe(42);
      }
    });

    it("should return error for invalid issue_number in message", () => {
      const result = helpers.resolveIssueNumber({ issue_number: "not-a-number" });
      expect(result.success).toBe(false);
    });

    it("should fall back to global context payload issue number when message has no issue_number", () => {
      global.context = { payload: { issue: { number: 99 } } };
      try {
        const result = helpers.resolveIssueNumber({});
        expect(result.success).toBe(true);
        if (result.success) {
          expect(result.issueNumber).toBe(99);
        }
      } finally {
        delete global.context;
      }
    });

    it("should return error when context is not defined and message has no issue_number", () => {
      const savedContext = global.context;
      delete global.context;
      try {
        const result = helpers.resolveIssueNumber({});
        expect(result.success).toBe(false);
        expect(result.error).toBe("No issue number available");
      } finally {
        global.context = savedContext;
      }
    });

    it("should return error when context has no issue in payload (e.g. schedule event)", () => {
      global.context = { eventName: "schedule", payload: {} };
      try {
        const result = helpers.resolveIssueNumber({});
        expect(result.success).toBe(false);
        expect(result.error).toBe("No issue number available");
      } finally {
        delete global.context;
      }
    });
  });

  describe("loadCustomSafeOutputActionHandlers", () => {
    beforeEach(() => {
      delete process.env.GH_AW_SAFE_OUTPUT_ACTIONS;
      global.core = {
        info: vi.fn(),
        debug: vi.fn(),
        warning: vi.fn(),
        error: vi.fn(),
      };
    });

    it("should return empty map when GH_AW_SAFE_OUTPUT_ACTIONS is not set", () => {
      const result = helpers.loadCustomSafeOutputActionHandlers();
      expect(result).toBeInstanceOf(Map);
      expect(result.size).toBe(0);
    });

    it("should parse and return action handlers map from env var", () => {
      process.env.GH_AW_SAFE_OUTPUT_ACTIONS = JSON.stringify({
        add_smoked_label: "add_smoked_label",
        notify_slack: "notify_slack",
      });

      const result = helpers.loadCustomSafeOutputActionHandlers();
      expect(result).toBeInstanceOf(Map);
      expect(result.size).toBe(2);
      expect(result.get("add_smoked_label")).toBe("add_smoked_label");
      expect(result.get("notify_slack")).toBe("notify_slack");
    });

    it("should return empty map when env var contains invalid JSON", () => {
      process.env.GH_AW_SAFE_OUTPUT_ACTIONS = "not-valid-json";
      const result = helpers.loadCustomSafeOutputActionHandlers();
      expect(result).toBeInstanceOf(Map);
      expect(result.size).toBe(0);
    });

    it("should return empty map when env var contains empty object", () => {
      process.env.GH_AW_SAFE_OUTPUT_ACTIONS = JSON.stringify({});
      const result = helpers.loadCustomSafeOutputActionHandlers();
      expect(result).toBeInstanceOf(Map);
      expect(result.size).toBe(0);
    });
  });
});
