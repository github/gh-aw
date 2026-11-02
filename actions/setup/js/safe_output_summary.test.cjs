import { describe, it, expect, beforeEach, vi, afterEach } from "vitest";
import * as fs from "fs";
import * as path from "path";
import * as os from "os";

// Mock the global objects that GitHub Actions provides
const mockCore = {
  info: vi.fn(),
  debug: vi.fn(),
  warning: vi.fn(),
  startGroup: vi.fn(),
  endGroup: vi.fn(),
  summary: {
    addRaw: vi.fn().mockReturnThis(),
    write: vi.fn().mockResolvedValue(undefined),
  },
};

// Set up global mocks before importing the module
globalThis.core = mockCore;

const { generateSafeOutputSummary, writeSafeOutputSummaries } = await import("./safe_output_summary.cjs");

describe("safe_output_summary", () => {
  const originalGithubRepository = process.env.GITHUB_REPOSITORY;

  beforeEach(() => {
    vi.clearAllMocks();
  });

  afterEach(() => {
    if (originalGithubRepository === undefined) {
      delete process.env.GITHUB_REPOSITORY;
    } else {
      process.env.GITHUB_REPOSITORY = originalGithubRepository;
    }
  });

  describe("generateSafeOutputSummary", () => {
    it("should generate summary for successful create_issue", () => {
      const options = {
        type: "create_issue",
        messageIndex: 1,
        success: true,
        result: {
          repo: "owner/repo",
          number: 123,
          url: "https://github.com/owner/repo/issues/123",
          temporaryId: "issue-1",
        },
        message: {
          title: "Test Issue",
          body: "This is a test issue body",
          labels: ["bug", "enhancement"],
        },
      };

      const summary = generateSafeOutputSummary(options);

      expect(summary).toContain("<details>");
      expect(summary).toContain("</details>");
      expect(summary).toContain("✅");
      expect(summary).toContain("Create Issue");
      expect(summary).toContain("Message 1");
      expect(summary).toContain("owner/repo#123");
      expect(summary).toContain("https://github.com/owner/repo/issues/123");
      expect(summary).toContain("issue-1");
      expect(summary).toContain("Test Issue");
      expect(summary).toContain("bug, enhancement");
    });

    it("should generate summary for failed message with error", () => {
      const options = {
        type: "create_project",
        messageIndex: 2,
        success: false,
        result: null,
        message: {
          title: "Test Project",
        },
        error: "ERR_PERMISSION: Failed to create project: permission denied",
      };

      const summary = generateSafeOutputSummary(options);

      expect(summary).toContain("❌");
      expect(summary).toContain("Failed");
      expect(summary).toContain("Create Project");
      expect(summary).toContain("Message 2");
      // Only the allowlisted error code is rendered; raw exception text is omitted
      expect(summary).toContain("ERR_PERMISSION");
      expect(summary).not.toContain("permission denied");
    });

    it("should generate summary for dropped duplicate issue", () => {
      const options = {
        type: "create_issue",
        messageIndex: 3,
        success: true,
        result: {
          dropped_duplicate: true,
          title: "Duplicate title",
          duplicate_of_title: "Duplicate title",
          duplicate_distance: 0,
          dedup_source: "within-run",
        },
        message: {
          title: "Duplicate title",
        },
      };

      const summary = generateSafeOutputSummary(options);

      expect(summary).toContain("⚠️");
      expect(summary).toContain("Duplicate Dropped");
      expect(summary).toContain("Matched Existing Title");
      expect(summary).toContain("Levenshtein Distance");
      expect(summary).toContain("Dedup Source");
    });

    it("should truncate long body content", () => {
      const longBody = "a".repeat(1000);

      const options = {
        type: "create_discussion",
        messageIndex: 3,
        success: true,
        result: {
          repo: "owner/repo",
          number: 456,
        },
        message: {
          title: "Test Discussion",
          body: longBody,
        },
      };

      const summary = generateSafeOutputSummary(options);

      // Body preview is omitted to prevent secret leakage into step summaries
      expect(summary).not.toContain("Body Preview");
      expect(summary).not.toContain("a".repeat(100));
      expect(summary).toContain("Test Discussion");
    });

    it("should not include body content with backticks in step summary", () => {
      const bodyWithBackticks = "Here is some code:\n```javascript\nconsole.log('hello');\n```\nEnd of body.";

      const options = {
        type: "create_issue",
        messageIndex: 1,
        success: true,
        result: {
          repo: "owner/repo",
          number: 123,
        },
        message: {
          title: "Issue with code",
          body: bodyWithBackticks,
        },
      };

      const summary = generateSafeOutputSummary(options);

      // Body content is omitted to prevent secret leakage into step summaries
      expect(summary).not.toContain("Body Preview");
      expect(summary).not.toContain("```javascript");
      expect(summary).toContain("Issue with code");
    });

    it("should not include raw message details in error summary", () => {
      const messageWithBackticks = {
        title: "Test Issue",
        body: "Code: ```\nconsole.log('test');\n```",
      };

      const options = {
        type: "create_issue",
        messageIndex: 1,
        success: false,
        result: null,
        message: messageWithBackticks,
        error: "Failed to create issue",
      };

      const summary = generateSafeOutputSummary(options);

      // Raw message content is omitted to prevent secret leakage into step summaries
      expect(summary).not.toContain("Message Details");
      expect(summary).not.toContain("console.log");
      expect(summary).not.toContain("Failed to create issue");
      expect(summary).toContain("UNCLASSIFIED");
    });

    it("should handle project-specific results", () => {
      const options = {
        type: "create_project",
        messageIndex: 4,
        success: true,
        result: {
          projectUrl: "https://github.com/orgs/owner/projects/123",
        },
        message: {
          title: "Test Project",
        },
      };

      const summary = generateSafeOutputSummary(options);

      expect(summary).toContain("**Target:**");
      expect(summary).toContain("https://github.com/orgs/owner/projects/123");
    });

    it("renders skipped policy diagnostics without classifying the item as failed", () => {
      const summary = generateSafeOutputSummary({
        type: "add_comment",
        messageIndex: 1,
        success: false,
        skipped: true,
        result: {
          success: false,
          skipped: true,
          reasonCode: "REQUIRED_LABELS_MISMATCH",
          reason: "Required labels missing",
          target: {
            repo: "github/github",
            number: 434183,
            url: "https://github.com/github/github/issues/434183",
          },
          safeDetails: {
            requiredLabels: ["automation", "n-plus-1"],
            missingLabels: ["automation", "n-plus-1"],
          },
        },
        message: {},
      });

      expect(summary).toContain("⚠️ Add Comment - Skipped (Message 1)");
      expect(summary).not.toContain("Failed");
      expect(summary).toContain("[github/github#434183](https://github.com/github/github/issues/434183)");
      expect(summary).toContain("**Reason Code:** `REQUIRED_LABELS_MISMATCH`");
      expect(summary).toContain("**Reason:** Required labels missing");
      expect(summary).toContain("**Required:** `automation`, `n-plus-1`");
      expect(summary).toContain("**Missing:** `automation`, `n-plus-1`");
    });

    it("renders success true skipped warning outcomes as skipped rather than success", () => {
      const summary = generateSafeOutputSummary({
        type: "add_comment",
        messageIndex: 2,
        success: true,
        result: {
          success: true,
          skipped: true,
          warning: "Target is locked: raw API details are omitted",
          reasonCode: "TARGET_LOCKED",
          reason: "Target is locked",
          target: { repo: "owner/repo", number: 5 },
        },
        message: {},
      });

      expect(summary).toContain("⚠️ Add Comment - Skipped (Message 2)");
      expect(summary).not.toContain("- Success");
      expect(summary).not.toContain("- Failed");
      expect(summary).toContain("**Reason:** Target is locked");
      expect(summary).not.toContain("raw API details");
    });

    it("renders handler-independent safe detail fields", () => {
      const summary = generateSafeOutputSummary({
        type: "dispatch_workflow",
        messageIndex: 3,
        success: false,
        result: {
          success: false,
          skipped: true,
          reason: "Branch is not allowed",
          safeDetails: { allowedBranches: ["main", "release"], protected: true },
        },
        message: {},
      });

      expect(summary).toContain("**Allowed Branches:** `main`, `release`");
      expect(summary).toContain("**Protected:** `true`");
    });

    it("should display secrecy field when present in message", () => {
      const options = {
        type: "create_issue",
        messageIndex: 1,
        success: true,
        result: {
          repo: "owner/repo",
          number: 123,
        },
        message: {
          title: "Secure Issue",
          body: "Sensitive content",
          secrecy: "private",
        },
      };

      const summary = generateSafeOutputSummary(options);

      expect(summary).toContain("Secrecy:");
      expect(summary).toContain("private");
    });

    it("should display integrity field when present in message", () => {
      const options = {
        type: "create_issue",
        messageIndex: 1,
        success: true,
        result: {
          repo: "owner/repo",
          number: 123,
        },
        message: {
          title: "Trusted Issue",
          body: "Verified content",
          integrity: "high",
        },
      };

      const summary = generateSafeOutputSummary(options);

      expect(summary).toContain("Integrity:");
      expect(summary).toContain("high");
    });

    it("should display both secrecy and integrity fields when present", () => {
      const options = {
        type: "add_comment",
        messageIndex: 2,
        success: true,
        result: {
          repo: "owner/repo",
          number: 456,
        },
        message: {
          body: "A comment",
          secrecy: "internal",
          integrity: "medium",
        },
      };

      const summary = generateSafeOutputSummary(options);

      expect(summary).toContain("Secrecy:");
      expect(summary).toContain("internal");
      expect(summary).toContain("Integrity:");
      expect(summary).toContain("medium");
    });

    it("should not render message data in step summary", () => {
      const options = {
        type: "add_comment",
        messageIndex: 2,
        success: true,
        result: {
          repo: "owner/repo",
          number: 456,
        },
        message: {
          body: "A comment",
          data: {
            verdict: "APPROVE",
            criteria_passed: 5,
          },
        },
      };

      const summary = generateSafeOutputSummary(options);

      // message.data is omitted to prevent secret leakage into step summaries
      expect(summary).not.toContain("**Data:**");
      expect(summary).not.toContain('"verdict": "APPROVE"');
      expect(summary).not.toContain('"criteria_passed": 5');
    });

    it("should not include body content in step summary (result.body or message.body)", () => {
      const options = {
        type: "add_comment",
        messageIndex: 1,
        success: true,
        result: {
          url: "https://github.com/owner/repo/issues/1#issuecomment-123",
          body: "Submitted body\n\n> *Footer added by workflow*",
        },
        message: {
          body: "Submitted body",
        },
      };

      const summary = generateSafeOutputSummary(options);

      // Body content is omitted to prevent secret leakage into step summaries
      expect(summary).not.toContain("Footer added by workflow");
      expect(summary).not.toContain("Body Preview");
      expect(summary).not.toContain("Submitted body");
    });

    it("should not include message.body in step summary", () => {
      const options = {
        type: "add_comment",
        messageIndex: 1,
        success: true,
        result: {
          url: "https://github.com/owner/repo/issues/1#issuecomment-123",
        },
        message: {
          body: "Submitted body only",
        },
      };

      const summary = generateSafeOutputSummary(options);

      // Body content is omitted to prevent secret leakage into step summaries
      expect(summary).not.toContain("Submitted body only");
      expect(summary).not.toContain("Body Preview");
    });

    it("should not display secrecy or integrity when absent from message", () => {
      const options = {
        type: "create_issue",
        messageIndex: 1,
        success: true,
        result: {
          repo: "owner/repo",
          number: 123,
        },
        message: {
          title: "Normal Issue",
          body: "Normal content",
        },
      };

      const summary = generateSafeOutputSummary(options);

      expect(summary).not.toContain("Secrecy:");
      expect(summary).not.toContain("Integrity:");
    });

    it("should display secrecy and integrity fields even when operation fails", () => {
      const options = {
        type: "create_issue",
        messageIndex: 1,
        success: false,
        result: null,
        message: {
          title: "Failed Issue",
          secrecy: "public",
          integrity: "low",
        },
        error: "Permission denied",
      };

      const summary = generateSafeOutputSummary(options);

      expect(summary).toContain("Secrecy:");
      expect(summary).toContain("public");
      expect(summary).toContain("Integrity:");
      expect(summary).toContain("low");
    });

    it("should link to the closed pull request for close_pull_request results", () => {
      const summary = generateSafeOutputSummary({
        type: "close_pull_request",
        messageIndex: 1,
        success: true,
        result: {
          pull_request_number: 445738,
          pull_request_url: "https://github.com/owner/repo/pull/445738",
        },
        message: {},
      });

      expect(summary).toContain("[#445738](https://github.com/owner/repo/pull/445738)");
    });

    it("should link to the review comment reply for reply_to_pull_request_review_comment results", () => {
      const summary = generateSafeOutputSummary({
        type: "reply_to_pull_request_review_comment",
        messageIndex: 1,
        success: true,
        result: {
          comment_id: 42,
          reply_url: "https://github.com/owner/repo/pull/1#discussion_r42",
        },
        message: {},
      });

      expect(summary).toContain("**Target:**");
      expect(summary).toContain("https://github.com/owner/repo/pull/1#discussion_r42");
    });

    it("should render label objects by name instead of [object Object]", () => {
      const summary = generateSafeOutputSummary({
        type: "add_labels",
        messageIndex: 1,
        success: true,
        result: { repo: "owner/repo", number: 5 },
        message: { labels: [{ name: "bug" }, { name: "enhancement" }] },
      });

      expect(summary).not.toContain("[object Object]");
      expect(summary).toContain("bug, enhancement");
    });

    it("should prefer labels reported by the handler result", () => {
      const summary = generateSafeOutputSummary({
        type: "add_labels",
        messageIndex: 1,
        success: true,
        result: { repo: "owner/repo", number: 5, labelsAdded: ["triage"] },
        message: { labels: ["ignored"] },
      });

      expect(summary).toContain("triage");
      expect(summary).not.toContain("ignored");
    });

    it("should derive an entity link from repo and number when no URL is reported", () => {
      const summary = generateSafeOutputSummary({
        type: "add_labels",
        messageIndex: 1,
        success: true,
        result: { repo: "owner/repo", number: 5, labelsAdded: ["bug"] },
        message: {},
      });

      expect(summary).toContain("**Target:** [owner/repo#5](https://github.com/owner/repo/issues/5)");
    });

    it.each([
      ["create_check_run", { check_run_url: "https://github.com/owner/repo/runs/1" }],
      ["autofix_code_scanning_alert", { autofixUrl: "https://github.com/owner/repo/security/code-scanning/2" }],
      ["upload_artifact", { artifactUrl: "https://github.com/owner/repo/actions/runs/3/artifacts/4" }],
    ])("should link to explicit %s handler URL fields", (type, result) => {
      const summary = generateSafeOutputSummary({
        type,
        messageIndex: 1,
        success: true,
        result,
        message: {},
      });

      expect(summary).toContain("**Target:**");
      expect(summary).toContain(Object.values(result)[0]);
    });

    it("should derive an entity link from the message repository when the result omits it", () => {
      const summary = generateSafeOutputSummary({
        type: "assign_milestone",
        messageIndex: 1,
        success: true,
        result: { issue_number: 8 },
        message: { target_repo: "owner/repo" },
      });

      expect(summary).toContain("**Target:** [owner/repo#8](https://github.com/owner/repo/issues/8)");
    });

    it("should derive an entity link from GITHUB_REPOSITORY when result and message omit the repo", () => {
      process.env.GITHUB_REPOSITORY = "env/repo";

      const summary = generateSafeOutputSummary({
        type: "assign_milestone",
        messageIndex: 1,
        success: true,
        result: { issue_number: 9 },
        message: {},
      });

      expect(summary).toContain("**Target:** [env/repo#9](https://github.com/env/repo/issues/9)");
    });

    it("should link to the project URL from update_project messages with sparse results", () => {
      const summary = generateSafeOutputSummary({
        type: "update_project",
        messageIndex: 1,
        success: true,
        result: { success: true },
        message: { project: "https://github.com/orgs/owner/projects/10" },
      });

      expect(summary).toContain("**Target:** [https://github.com/orgs/owner/projects/10](https://github.com/orgs/owner/projects/10)");
    });

    it("should render plain text when the entity URL is not an http(s) URL", () => {
      const summary = generateSafeOutputSummary({
        type: "add_labels",
        messageIndex: 1,
        success: true,
        result: { repo: "owner/repo", number: 5, url: "javascript:alert(1)" },
        message: {},
      });

      expect(summary).toContain("**Target:** owner/repo#5");
      expect(summary).not.toContain("javascript:alert(1)");
    });

    it("should show fallback issue status when create_pull_request falls back to issue", () => {
      const options = {
        type: "create_pull_request",
        messageIndex: 1,
        success: true,
        result: {
          fallback_used: true,
          issue_number: 42,
          issue_url: "https://github.com/owner/repo/issues/42",
          branch_name: "fix-branch",
          repo: "owner/repo",
        },
        message: {
          title: "Fix: upgrade dependencies",
          body: "This upgrades the dependencies.",
        },
      };

      const summary = generateSafeOutputSummary(options);

      // Should use warning emoji and "Fallback Issue Created" status
      expect(summary).toContain("⚠️");
      expect(summary).toContain("Fallback Issue Created");
      expect(summary).toContain("Message 1");
      // Should NOT show "Success"
      expect(summary).not.toContain("✅");
      // Should show fallback explanation
      expect(summary).toContain("protected file");
      expect(summary).toContain("https://github.com/owner/repo/issues/42");
      expect(summary).toContain("owner/repo#42");
      expect(summary).toContain("fix-branch");
      expect(summary).toContain("Fix: upgrade dependencies");
    });

    it("should show fallback issue status when push_to_pull_request_branch falls back to issue", () => {
      const options = {
        type: "push_to_pull_request_branch",
        messageIndex: 2,
        success: true,
        result: {
          fallback_used: true,
          issue_number: 99,
          issue_url: "https://github.com/owner/repo/issues/99",
        },
        message: {
          body: "Pushing to PR branch.",
        },
      };

      const summary = generateSafeOutputSummary(options);

      expect(summary).toContain("⚠️");
      expect(summary).toContain("Fallback Issue Created");
      expect(summary).toContain("https://github.com/owner/repo/issues/99");
      expect(summary).not.toContain("✅");
    });

    it("should show normal success for create_pull_request when no fallback", () => {
      const options = {
        type: "create_pull_request",
        messageIndex: 1,
        success: true,
        result: {
          pull_request_number: 5,
          pull_request_url: "https://github.com/owner/repo/pull/5",
          url: "https://github.com/owner/repo/pull/5",
          repo: "owner/repo",
          number: 5,
        },
        message: { title: "My PR" },
      };

      const summary = generateSafeOutputSummary(options);

      expect(summary).toContain("✅");
      expect(summary).toContain("Success");
      expect(summary).not.toContain("⚠️");
      expect(summary).not.toContain("Fallback");
    });

    it("should show fallback pull request status when push_to_pull_request_branch falls back to pull request", () => {
      const options = {
        type: "push_to_pull_request_branch",
        messageIndex: 3,
        success: true,
        result: {
          fallback_used: true,
          fallback_type: "pull_request",
          pull_request_number: 71,
          pull_request_url: "https://github.com/owner/repo/pull/71",
          repo: "owner/repo",
        },
        message: {
          body: "Pushing to PR branch.",
        },
      };

      const summary = generateSafeOutputSummary(options);

      expect(summary).toContain("⚠️");
      expect(summary).toContain("Fallback Pull Request Created");
      expect(summary).toContain("https://github.com/owner/repo/pull/71");
      expect(summary).toContain("owner/repo#71");
      expect(summary).toContain("non-fast-forward");
    });

    it("should prefer explicit fallback_type over inferred shape for backward compatibility", () => {
      const options = {
        type: "push_to_pull_request_branch",
        messageIndex: 4,
        success: true,
        result: {
          fallback_used: true,
          fallback_type: "issue",
          // pull_request_url present by shape, but explicit fallback_type should win
          pull_request_url: "https://github.com/owner/repo/pull/72",
          issue_number: 123,
          issue_url: "https://github.com/owner/repo/issues/123",
          repo: "owner/repo",
        },
        message: {
          body: "Pushing to PR branch.",
        },
      };

      const summary = generateSafeOutputSummary(options);

      expect(summary).toContain("Fallback Issue Created");
      expect(summary).toContain("Fallback Issue:");
      expect(summary).toContain("https://github.com/owner/repo/issues/123");
      expect(summary).not.toContain("Fallback Pull Request Created");
    });

    describe("sentinel secret exclusion", () => {
      const SENTINEL = "SUPERSECRETVALUE_xK9mQ2wR";

      it("should not include secret from message.body in step summary", () => {
        const options = {
          type: "create_issue",
          messageIndex: 1,
          success: true,
          result: { repo: "owner/repo", number: 1 },
          message: { title: "Issue", body: `Token: ${SENTINEL}` },
        };
        expect(generateSafeOutputSummary(options)).not.toContain(SENTINEL);
      });

      it("should not include secret from result.body in step summary", () => {
        const options = {
          type: "add_comment",
          messageIndex: 1,
          success: true,
          result: { url: "https://github.com/owner/repo/issues/1#issuecomment-1", body: `Secret: ${SENTINEL}` },
          message: {},
        };
        expect(generateSafeOutputSummary(options)).not.toContain(SENTINEL);
      });

      it("should not include secret from message.data in step summary", () => {
        const options = {
          type: "add_comment",
          messageIndex: 2,
          success: true,
          result: { repo: "owner/repo", number: 2 },
          message: { data: { token: SENTINEL, nested: { key: SENTINEL } } },
        };
        expect(generateSafeOutputSummary(options)).not.toContain(SENTINEL);
      });

      it("should not include secret from message body in error path step summary", () => {
        const options = {
          type: "create_issue",
          messageIndex: 1,
          success: false,
          result: null,
          message: { title: "Issue", body: `Password: ${SENTINEL}` },
          error: "Handler failed",
        };
        expect(generateSafeOutputSummary(options)).not.toContain(SENTINEL);
      });

      it("should not include secret from the error message itself", () => {
        const options = {
          type: "create_issue",
          messageIndex: 1,
          success: false,
          result: null,
          message: { title: "Issue" },
          error: `ERR_API: request to https://api.github.com failed with token ${SENTINEL}`,
        };
        const summary = generateSafeOutputSummary(options);
        expect(summary).not.toContain(SENTINEL);
        expect(summary).toContain("ERR_API");
      });

      it("should not include secret from an unclassified error message", () => {
        const options = {
          type: "create_issue",
          messageIndex: 1,
          success: false,
          result: null,
          message: { title: "Issue" },
          error: `Handler failed: ${SENTINEL}`,
        };
        const summary = generateSafeOutputSummary(options);
        expect(summary).not.toContain(SENTINEL);
        expect(summary).toContain("UNCLASSIFIED");
      });

      it("should not include multiline secret string from message.body", () => {
        const options = {
          type: "create_issue",
          messageIndex: 1,
          success: true,
          result: { repo: "owner/repo", number: 1 },
          message: { title: "Issue", body: `line1\n${SENTINEL}\nline3` },
        };
        expect(generateSafeOutputSummary(options)).not.toContain(SENTINEL);
      });

      it("should not include URL-like secret from message.body", () => {
        const options = {
          type: "create_issue",
          messageIndex: 1,
          success: true,
          result: { repo: "owner/repo", number: 1 },
          message: { title: "Issue", body: `https://example.com/api?token=${SENTINEL}` },
        };
        expect(generateSafeOutputSummary(options)).not.toContain(SENTINEL);
      });

      it("should not include base64-encoded secret from message.data", () => {
        const encoded = Buffer.from(SENTINEL).toString("base64");
        const options = {
          type: "add_comment",
          messageIndex: 1,
          success: true,
          result: { repo: "owner/repo", number: 1 },
          message: { data: { encoded } },
        };
        expect(generateSafeOutputSummary(options)).not.toContain(encoded);
      });

      it("should still include safe metadata when message contains secrets", () => {
        const options = {
          type: "create_issue",
          messageIndex: 1,
          success: true,
          result: { repo: "owner/repo", number: 42, url: "https://github.com/owner/repo/issues/42" },
          message: { title: "Safe Title", body: `${SENTINEL}`, labels: ["bug"] },
        };
        const summary = generateSafeOutputSummary(options);
        expect(summary).not.toContain(SENTINEL);
        expect(summary).toContain("Safe Title");
        expect(summary).toContain("owner/repo#42");
        expect(summary).toContain("bug");
      });
    });
  });

  it("renders bounded merge failure codes and the audit artifact location, not raw evidence", () => {
    const summary = generateSafeOutputSummary({
      type: "merge_pull_request",
      messageIndex: 1,
      success: false,
      error: "merge_pull_request gate checks failed",
      result: {
        decision_evidence: { labels: ["private-instruction-like-label"] },
        failure_reasons: [{ code: "required_approval_missing" }, { code: "unsafe\n@someone" }],
      },
    });
    expect(summary).toContain("`required_approval_missing`");
    expect(summary).toContain("merge-pull-request-decisions.jsonl");
    expect(summary).not.toContain("private-instruction-like-label");
    expect(summary).not.toContain("@someone");
  });

  describe("writeSafeOutputSummaries", () => {
    it.each([
      { success: false, error: "ERR_API: operation failed" },
      { success: false, skipped: true, result: { skipped: true, reason_code: "POLICY_FILE_PROTECTION_DENIED" } },
    ])("links diagnostic outcomes to the playbook without changing status", async outcome => {
      await writeSafeOutputSummaries([{ type: "create_issue", messageIndex: 0, ...outcome }], [{ title: "Issue" }]);
      const summary = mockCore.summary.addRaw.mock.calls[0][0];
      expect(summary).toContain("debug-playbook.md#completion-and-safe-output-errors");
      expect(mockCore.info.mock.calls.flat().join("\n")).toContain("Debug playbook:");
      if (outcome.skipped) {
        expect(summary).toContain("0 failed");
      }
    });

    it("omits diagnostic links for successful output", async () => {
      await writeSafeOutputSummaries([{ type: "create_issue", messageIndex: 0, success: true, result: { number: 1 } }], [{ title: "Issue" }]);
      expect(mockCore.summary.addRaw.mock.calls[0][0]).not.toContain("debug-playbook.md");
    });

    it("should redact credential-shaped strings before writing the step summary", async () => {
      // Built from parts so the fixture is never a literal credential string in source.
      const fakePat = "ghp_" + "a1b2c3d4e5".repeat(3) + "f6g7h8";
      const results = [
        {
          type: "create_issue",
          messageIndex: 0,
          success: true,
          result: { repo: "owner/repo", number: 123 },
        },
      ];
      const messages = [{ title: `Issue with token ${fakePat}` }];

      await writeSafeOutputSummaries(results, messages);

      const summaryContent = mockCore.summary.addRaw.mock.calls[0][0];
      expect(summaryContent).not.toContain(fakePat);
      expect(summaryContent).toContain("***REDACTED***");
    });

    it("should write summaries for multiple results", async () => {
      const results = [
        {
          type: "create_issue",
          messageIndex: 0,
          success: true,
          result: {
            repo: "owner/repo",
            number: 123,
            url: "https://github.com/owner/repo/issues/123",
          },
        },
        {
          type: "create_project",
          messageIndex: 1,
          success: true,
          result: {
            projectUrl: "https://github.com/orgs/owner/projects/456",
          },
        },
      ];

      const messages = [{ title: "Issue 1", body: "Body 1" }, { title: "Project 1" }];

      await writeSafeOutputSummaries(results, messages);

      expect(mockCore.summary.addRaw).toHaveBeenCalledTimes(1);
      expect(mockCore.summary.write).toHaveBeenCalledTimes(1);
      expect(mockCore.info).toHaveBeenCalledWith("📝 Safe output summaries written to step summary");

      const summaryContent = mockCore.summary.addRaw.mock.calls[0][0];
      expect(summaryContent).toContain("Safe Output Processing Summary");
      expect(summaryContent).toContain("Processed 2 safe-output message(s)");
      expect(summaryContent).toContain("Status: **success**");
      expect(summaryContent).toContain("Applied: **2** · Skipped: **0** · Warnings: **0** · Failed: **0** · Cancelled: **0** · Deferred: **0**");
      expect(summaryContent).toContain("Create Issue");
      expect(summaryContent).toContain("Create Project");
    });

    it("should wrap the whole section in a collapsible details block", async () => {
      const results = [
        {
          type: "create_issue",
          messageIndex: 0,
          success: true,
          result: { repo: "owner/repo", number: 123, url: "https://github.com/owner/repo/issues/123" },
        },
      ];

      await writeSafeOutputSummaries(results, [{ title: "Issue 1" }]);

      const summaryContent = mockCore.summary.addRaw.mock.calls[0][0];
      expect(summaryContent.startsWith("<details>\n<summary>✅ Safe Output Processing Summary")).toBe(true);
      expect(summaryContent.trimEnd().endsWith("</details>")).toBe(true);
    });

    it("should include partial success item counts in the summary", async () => {
      const results = [
        {
          type: "create_issue",
          messageIndex: 0,
          success: true,
          result: { repo: "owner/repo", number: 123 },
        },
        {
          type: "create_discussion",
          messageIndex: 1,
          success: false,
          error: "Validation failed",
        },
      ];

      const messages = [{ title: "Issue 1" }, { title: "Discussion 1" }];

      await writeSafeOutputSummaries(results, messages);

      const summaryContent = mockCore.summary.addRaw.mock.calls[0][0];
      expect(summaryContent).toContain("Status: **partial_success**");
      expect(summaryContent).toContain("Applied: **1** · Skipped: **0** · Warnings: **0** · Failed: **1** · Cancelled: **0** · Deferred: **0**");
    });

    it("writes aggregate counts and grouped overview matching per-item classifications", async () => {
      const results = [
        {
          type: "add_comment",
          messageIndex: 0,
          success: true,
          result: { success: true, repo: "owner/repo", number: 1 },
        },
        {
          type: "add_comment",
          messageIndex: 1,
          success: false,
          skipped: true,
          error: "Required labels missing",
          result: {
            success: false,
            skipped: true,
            reasonCode: "REQUIRED_LABELS_MISMATCH",
            reason: "Required labels missing",
            target: { repo: "owner/repo", number: 2 },
            safeDetails: { requiredLabels: ["automation"], missingLabels: ["automation"] },
          },
        },
        {
          type: "add_labels",
          messageIndex: 2,
          success: true,
          skipped: true,
          warning: "Target locked",
          result: {
            success: true,
            skipped: true,
            reasonCode: "TARGET_LOCKED",
            reason: "Target is locked",
            target: { repo: "owner/repo", number: 3 },
          },
        },
        {
          type: "dispatch_workflow",
          messageIndex: 3,
          success: false,
          error: "ERR_PERMISSION: denied",
          result: null,
        },
        {
          type: "upload_artifact",
          messageIndex: 4,
          success: false,
          deferred: true,
          result: { success: false, deferred: true },
        },
        {
          type: "merge_pull_request",
          messageIndex: 5,
          success: false,
          cancelled: true,
          errorCode: "THREAT_DETECTED",
          reason: "Threat policy cancelled the output | blocked\nby policy",
        },
      ];
      const messages = [{}, {}, {}, {}, {}, {}];

      await writeSafeOutputSummaries(results, messages);

      const summaryContent = mockCore.summary.addRaw.mock.calls[0][0];
      expect(summaryContent).toContain("Status: **partial_success**");
      expect(summaryContent).toContain("Applied: **1** · Skipped: **2** · Warnings: **0** · Failed: **1** · Cancelled: **1** · Deferred: **1**");
      expect(summaryContent).toContain("| Skipped | Add Comment | 1 | Required labels missing |");
      expect(summaryContent).toContain("| Failed | Dispatch Workflow | 1 | ERR_PERMISSION |");
      expect(summaryContent).toContain("| Cancelled | Merge Pull Request | 1 | Threat policy cancelled the output \\| blocked<br>by policy |");
      expect(summaryContent).toContain("⚠️ Add Comment - Skipped (Message 2)");
      expect(summaryContent).toContain("❌ Dispatch Workflow - Failed (Message 4)");
      expect(summaryContent).toContain("⏸️ Upload Artifact - Deferred (Message 5)");
      expect(summaryContent).toContain("🚫 Merge Pull Request - Cancelled (Message 6)");
    });

    it("should skip results handled by standalone steps", async () => {
      const results = [
        {
          type: "create_issue",
          messageIndex: 0,
          success: true,
          result: { repo: "owner/repo", number: 123 },
        },
        {
          type: "noop",
          messageIndex: 1,
          success: false,
          skipped: true,
          delegated: true,
          reason: "Handled by standalone step",
        },
      ];

      const messages = [{ title: "Issue 1" }, { message: "Noop message" }];

      await writeSafeOutputSummaries(results, messages);

      const summaryContent = mockCore.summary.addRaw.mock.calls[0][0];
      expect(summaryContent).toContain("Create Issue");
      expect(summaryContent).not.toContain("Noop");
    });

    it("should handle empty results", async () => {
      await writeSafeOutputSummaries([], []);

      expect(mockCore.summary.addRaw).not.toHaveBeenCalled();
      expect(mockCore.summary.write).not.toHaveBeenCalled();
    });

    it("should handle write failures gracefully", async () => {
      mockCore.summary.write.mockRejectedValueOnce(new Error("Write failed"));

      const results = [
        {
          type: "create_issue",
          messageIndex: 0,
          success: true,
          result: { repo: "owner/repo", number: 123 },
        },
      ];

      const messages = [{ title: "Issue 1" }];

      await writeSafeOutputSummaries(results, messages);

      expect(mockCore.warning).toHaveBeenCalledWith("Failed to write safe output summaries: Write failed");
    });

    it("should log raw .jsonl content when safe outputs file exists", async () => {
      // Create a temporary .jsonl file
      const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "test-safe-outputs-"));
      const jsonlFile = path.join(tempDir, "outputs.jsonl");
      const jsonlContent = '{"type":"create_issue","title":"Test Issue"}\n{"type":"add_comment","body":"Test comment"}';
      fs.writeFileSync(jsonlFile, jsonlContent, "utf8");

      // Set environment variable
      const originalEnv = process.env.GH_AW_SAFE_OUTPUTS;
      process.env.GH_AW_SAFE_OUTPUTS = jsonlFile;

      try {
        const results = [
          {
            type: "create_issue",
            messageIndex: 0,
            success: true,
            result: { repo: "owner/repo", number: 123 },
          },
        ];

        const messages = [{ title: "Issue 1" }];

        await writeSafeOutputSummaries(results, messages);

        // Verify that displayFileContent was called (which uses core.startGroup and core.endGroup)
        expect(mockCore.startGroup).toHaveBeenCalled();
        expect(mockCore.endGroup).toHaveBeenCalled();

        // Verify that the group title includes the file name and size
        const startGroupCalls = mockCore.startGroup.mock.calls.map(call => call[0]);
        expect(startGroupCalls.some(call => call.includes("safe-outputs.jsonl"))).toBe(true);

        // Verify that content lines were logged
        const infoCalls = mockCore.info.mock.calls.map(call => call[0]);
        expect(infoCalls.length).toBeGreaterThan(0);
      } finally {
        // Cleanup
        process.env.GH_AW_SAFE_OUTPUTS = originalEnv;
        fs.rmSync(tempDir, { recursive: true, force: true });
      }
    });

    it("should handle missing safe outputs file gracefully", async () => {
      // Set environment variable to a non-existent file
      const originalEnv = process.env.GH_AW_SAFE_OUTPUTS;
      process.env.GH_AW_SAFE_OUTPUTS = "/non/existent/file.jsonl";

      try {
        const results = [
          {
            type: "create_issue",
            messageIndex: 0,
            success: true,
            result: { repo: "owner/repo", number: 123 },
          },
        ];

        const messages = [{ title: "Issue 1" }];

        await writeSafeOutputSummaries(results, messages);

        // Should not throw and should still write summary
        expect(mockCore.summary.write).toHaveBeenCalledTimes(1);
        expect(mockCore.info).toHaveBeenCalledWith("📝 Safe output summaries written to step summary");
      } finally {
        // Cleanup
        process.env.GH_AW_SAFE_OUTPUTS = originalEnv;
      }
    });

    it("should skip logging when safe outputs file is empty", async () => {
      // Create a temporary empty .jsonl file
      const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "test-safe-outputs-"));
      const jsonlFile = path.join(tempDir, "outputs.jsonl");
      fs.writeFileSync(jsonlFile, "", "utf8");

      // Set environment variable
      const originalEnv = process.env.GH_AW_SAFE_OUTPUTS;
      process.env.GH_AW_SAFE_OUTPUTS = jsonlFile;

      try {
        const results = [
          {
            type: "create_issue",
            messageIndex: 0,
            success: true,
            result: { repo: "owner/repo", number: 123 },
          },
        ];

        const messages = [{ title: "Issue 1" }];

        await writeSafeOutputSummaries(results, messages);

        // Should not log empty content or start a log group
        expect(mockCore.startGroup).not.toHaveBeenCalled();
        expect(mockCore.endGroup).not.toHaveBeenCalled();
      } finally {
        // Cleanup
        process.env.GH_AW_SAFE_OUTPUTS = originalEnv;
        fs.rmSync(tempDir, { recursive: true, force: true });
      }
    });

    it("should truncate large safe outputs file content", async () => {
      // Create a temporary .jsonl file with large content (> 5000 bytes)
      const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "test-safe-outputs-"));
      const jsonlFile = path.join(tempDir, "outputs.jsonl");

      // Create content larger than 5000 bytes
      const largeEntry = { type: "create_issue", title: "Test", body: "a".repeat(5000) };
      const jsonlContent = JSON.stringify(largeEntry) + "\n" + JSON.stringify(largeEntry);
      fs.writeFileSync(jsonlFile, jsonlContent, "utf8");

      // Set environment variable
      const originalEnv = process.env.GH_AW_SAFE_OUTPUTS;
      process.env.GH_AW_SAFE_OUTPUTS = jsonlFile;

      try {
        const results = [
          {
            type: "create_issue",
            messageIndex: 0,
            success: true,
            result: { repo: "owner/repo", number: 123 },
          },
        ];

        const messages = [{ title: "Issue 1" }];

        await writeSafeOutputSummaries(results, messages);

        // Verify that displayFileContent was called (which uses core.startGroup and core.endGroup)
        expect(mockCore.startGroup).toHaveBeenCalled();
        expect(mockCore.endGroup).toHaveBeenCalled();

        // Verify that the group title includes the file name
        const startGroupCalls = mockCore.startGroup.mock.calls.map(call => call[0]);
        expect(startGroupCalls.some(call => call.includes("safe-outputs.jsonl"))).toBe(true);

        // Verify that truncation message was logged (displayFileContent shows truncation info)
        const infoCalls = mockCore.info.mock.calls.map(call => call[0]);
        expect(infoCalls.some(call => call.includes("truncated") || call.includes("..."))).toBe(true);
      } finally {
        // Cleanup
        process.env.GH_AW_SAFE_OUTPUTS = originalEnv;
        fs.rmSync(tempDir, { recursive: true, force: true });
      }
    });
  });
});
