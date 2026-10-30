import { describe, it, expect, beforeEach, vi } from "vitest";
import fs from "fs";
import path from "path";
import { fileURLToPath } from "url";
const mockCore = { debug: vi.fn(), info: vi.fn(), warning: vi.fn(), error: vi.fn(), setFailed: vi.fn(), setOutput: vi.fn(), summary: { addRaw: vi.fn().mockReturnThis(), write: vi.fn().mockResolvedValue() } },
  mockContext = { eventName: "issues", runId: 12345, repo: { owner: "testowner", repo: "testrepo" }, payload: { issue: { number: 42 }, pull_request: { number: 100 }, repository: { html_url: "https://github.com/testowner/testrepo" } } };
((global.core = mockCore), (global.context = mockContext));
const { checkLabelFilter, checkTitlePrefixFilter, parseEntityConfig, resolveEntityNumber, escapeMarkdownTitle, createCloseEntityHandler, buildCommentBody, ISSUE_CONFIG, PULL_REQUEST_CONFIG } = require("./close_entity_helpers.cjs");
describe("close_entity_helpers", () => {
  (beforeEach(() => {
    (vi.clearAllMocks(),
      delete process.env.GH_AW_CLOSE_ISSUE_REQUIRED_LABELS,
      delete process.env.GH_AW_CLOSE_ISSUE_REQUIRED_TITLE_PREFIX,
      delete process.env.GH_AW_CLOSE_ISSUE_TARGET,
      delete process.env.GH_AW_CLOSE_PR_REQUIRED_LABELS,
      delete process.env.GH_AW_CLOSE_PR_REQUIRED_TITLE_PREFIX,
      delete process.env.GH_AW_CLOSE_PR_TARGET,
      // Point GH_AW_PROMPTS_DIR to the source md/ directory so getPromptPath()
      // resolves template files from the source tree in test environments where
      // RUNNER_TEMP is set but the runtime prompts directory is not populated.
      (process.env.GH_AW_PROMPTS_DIR = path.join(path.dirname(fileURLToPath(import.meta.url)), "../md")),
      (global.context.eventName = "issues"),
      (global.context.payload.issue = { number: 42 }),
      (global.context.payload.pull_request = { number: 100 }));
  }),
    describe("checkLabelFilter", () => {
      (it("should return true when no required labels specified", () => {
        expect(checkLabelFilter([{ name: "bug" }], [])).toBe(!0);
      }),
        it("should return true when entity has one of the required labels", () => {
          expect(checkLabelFilter([{ name: "bug" }, { name: "enhancement" }], ["bug", "wontfix"])).toBe(!0);
        }),
        it("should return false when entity has none of the required labels", () => {
          expect(checkLabelFilter([{ name: "bug" }], ["enhancement", "wontfix"])).toBe(!1);
        }),
        it("should return false when entity has no labels and required labels specified", () => {
          expect(checkLabelFilter([], ["bug"])).toBe(!1);
        }));
    }),
    describe("checkTitlePrefixFilter", () => {
      (it("should return true when no required prefix specified", () => {
        expect(checkTitlePrefixFilter("Some Title", "")).toBe(!0);
      }),
        it("should return true when title starts with required prefix", () => {
          expect(checkTitlePrefixFilter("[bug] Fix something", "[bug]")).toBe(!0);
        }),
        it("should return false when title does not start with required prefix", () => {
          expect(checkTitlePrefixFilter("Fix something", "[bug]")).toBe(!1);
        }),
        it("should be case-sensitive", () => {
          expect(checkTitlePrefixFilter("[BUG] Fix something", "[bug]")).toBe(!1);
        }));
    }),
    describe("parseEntityConfig", () => {
      (it("should return defaults when no environment variables set", () => {
        const config = parseEntityConfig("GH_AW_CLOSE_ISSUE");
        (expect(config.requiredLabels).toEqual([]), expect(config.requiredTitlePrefix).toBe(""), expect(config.target).toBe("triggering"));
      }),
        it("should parse required labels from environment", () => {
          process.env.GH_AW_CLOSE_ISSUE_REQUIRED_LABELS = "bug, enhancement, stale";
          const config = parseEntityConfig("GH_AW_CLOSE_ISSUE");
          expect(config.requiredLabels).toEqual(["bug", "enhancement", "stale"]);
        }),
        it("should parse required title prefix from environment", () => {
          process.env.GH_AW_CLOSE_ISSUE_REQUIRED_TITLE_PREFIX = "[refactor]";
          const config = parseEntityConfig("GH_AW_CLOSE_ISSUE");
          expect(config.requiredTitlePrefix).toBe("[refactor]");
        }),
        it("should parse target from environment", () => {
          process.env.GH_AW_CLOSE_ISSUE_TARGET = "*";
          const config = parseEntityConfig("GH_AW_CLOSE_ISSUE");
          expect(config.target).toBe("*");
        }),
        it("should work with PR environment variable prefix", () => {
          ((process.env.GH_AW_CLOSE_PR_REQUIRED_LABELS = "ready-to-close"), (process.env.GH_AW_CLOSE_PR_TARGET = "123"));
          const config = parseEntityConfig("GH_AW_CLOSE_PR");
          (expect(config.requiredLabels).toEqual(["ready-to-close"]), expect(config.target).toBe("123"));
        }));
    }),
    describe("resolveEntityNumber", () => {
      (describe("with target '*'", () => {
        (it("should resolve from item number field", () => {
          const result = resolveEntityNumber(ISSUE_CONFIG, "*", { issue_number: 50 }, !0);
          (expect(result.success).toBe(!0), expect(result.number).toBe(50));
        }),
          it("should handle string number field", () => {
            const result = resolveEntityNumber(ISSUE_CONFIG, "*", { issue_number: "75" }, !0);
            (expect(result.success).toBe(!0), expect(result.number).toBe(75));
          }),
          it("should fail when number field is missing", () => {
            const result = resolveEntityNumber(ISSUE_CONFIG, "*", {}, !0);
            (expect(result.success).toBe(!1), expect(result.message).toContain("no issue_number specified"));
          }),
          it("should fail when number field is invalid", () => {
            const result = resolveEntityNumber(ISSUE_CONFIG, "*", { issue_number: "abc" }, !0);
            (expect(result.success).toBe(!1), expect(result.message).toContain("Invalid issue number specified"));
          }),
          it("should fail when number is zero or negative", () => {
            const result = resolveEntityNumber(ISSUE_CONFIG, "*", { issue_number: -5 }, !0);
            (expect(result.success).toBe(!1), expect(result.message).toContain("Invalid issue number specified"));
          }),
          it("should fail when number is zero (falsy)", () => {
            const result = resolveEntityNumber(ISSUE_CONFIG, "*", { issue_number: 0 }, !0);
            (expect(result.success).toBe(!1), expect(result.message).toContain("no issue_number specified"));
          }));
      }),
        describe("with explicit target number", () => {
          (it("should resolve from target configuration", () => {
            const result = resolveEntityNumber(ISSUE_CONFIG, "123", {}, !0);
            (expect(result.success).toBe(!0), expect(result.number).toBe(123));
          }),
            it("should fail when target is not a valid number", () => {
              const result = resolveEntityNumber(ISSUE_CONFIG, "invalid", {}, !0);
              (expect(result.success).toBe(!1), expect(result.message).toContain("Invalid issue number in target configuration"));
            }));
        }),
        describe("with target 'triggering'", () => {
          (it("should resolve from context in issue event", () => {
            const result = resolveEntityNumber(ISSUE_CONFIG, "triggering", {}, !0);
            (expect(result.success).toBe(!0), expect(result.number).toBe(42));
          }),
            it("should fail when not in entity context", () => {
              const result = resolveEntityNumber(ISSUE_CONFIG, "triggering", {}, !1);
              (expect(result.success).toBe(!1), expect(result.message).toContain("Not in issue context"));
            }),
            it("should fail when context payload has no number", () => {
              global.context.payload.issue = {};
              const result = resolveEntityNumber(ISSUE_CONFIG, "triggering", {}, !0);
              (expect(result.success).toBe(!1), expect(result.message).toContain("no issue found in payload"));
            }));
        }),
        describe("for pull requests", () => {
          (beforeEach(() => {
            global.context.eventName = "pull_request";
          }),
            it("should resolve PR number from item with target '*'", () => {
              const result = resolveEntityNumber(PULL_REQUEST_CONFIG, "*", { pull_request_number: 200 }, !0);
              (expect(result.success).toBe(!0), expect(result.number).toBe(200));
            }),
            it("should resolve PR number from triggering context", () => {
              const result = resolveEntityNumber(PULL_REQUEST_CONFIG, "triggering", {}, !0);
              (expect(result.success).toBe(!0), expect(result.number).toBe(100));
            }));
        }));
    }),
    describe("escapeMarkdownTitle", () => {
      (it("should escape square brackets", () => {
        expect(escapeMarkdownTitle("[feature] Add new thing")).toBe("\\[feature\\] Add new thing");
      }),
        it("should escape parentheses", () => {
          expect(escapeMarkdownTitle("Fix bug (urgent)")).toBe("Fix bug \\(urgent\\)");
        }),
        it("should escape all markdown special characters", () => {
          expect(escapeMarkdownTitle("[test] (foo) [bar]")).toBe("\\[test\\] \\(foo\\) \\[bar\\]");
        }),
        it("should not modify titles without special characters", () => {
          expect(escapeMarkdownTitle("Simple title")).toBe("Simple title");
        }));
    }),
    describe("ISSUE_CONFIG", () => {
      (it("should have correct entity type", () => {
        expect(ISSUE_CONFIG.entityType).toBe("issue");
      }),
        it("should have correct item type", () => {
          expect(ISSUE_CONFIG.itemType).toBe("close_issue");
        }),
        it("should have correct item type display", () => {
          expect(ISSUE_CONFIG.itemTypeDisplay).toBe("close-issue");
        }),
        it("should have correct context events", () => {
          (expect(ISSUE_CONFIG.contextEvents).toContain("issues"), expect(ISSUE_CONFIG.contextEvents).toContain("issue_comment"));
        }),
        it("should have correct URL path", () => {
          expect(ISSUE_CONFIG.urlPath).toBe("issues");
        }));
    }),
    describe("PULL_REQUEST_CONFIG", () => {
      (it("should have correct entity type", () => {
        expect(PULL_REQUEST_CONFIG.entityType).toBe("pull_request");
      }),
        it("should have correct item type", () => {
          expect(PULL_REQUEST_CONFIG.itemType).toBe("close_pull_request");
        }),
        it("should have correct item type display", () => {
          expect(PULL_REQUEST_CONFIG.itemTypeDisplay).toBe("close-pull-request");
        }),
        it("should have correct context events", () => {
          (expect(PULL_REQUEST_CONFIG.contextEvents).toContain("pull_request"), expect(PULL_REQUEST_CONFIG.contextEvents).toContain("pull_request_review_comment"));
        }),
        it("should have correct URL path", () => {
          expect(PULL_REQUEST_CONFIG.urlPath).toBe("pull");
        }));
    }),
    describe("sanitization preserves markers", () => {
      it("should sanitize user content while preserving system markers in buildCommentBody", () => {
        // Import sanitizeContent to test the actual behavior
        const { sanitizeContent } = require("./sanitize_content.cjs");

        // Set up environment for tracker ID and footer
        process.env.GH_AW_WORKFLOW_NAME = "Test Workflow";
        process.env.GH_AW_WORKFLOW_SOURCE = "test.md";
        process.env.GH_AW_WORKFLOW_SOURCE_URL = "https://github.com/test/repo";
        process.env.GH_AW_TRACKER_ID = "test-tracker-456";

        const userContent = "User comment with <script>xss</script> and <!-- evil comment -->";
        const sanitizedBody = sanitizeContent(userContent);
        const result = buildCommentBody(sanitizedBody, 42, undefined);

        // User content should be sanitized (tags converted)
        expect(result).not.toContain("<script>");
        expect(result).toContain("(script)"); // Tags converted to parentheses
        expect(result).not.toContain("<!-- evil comment -->");

        // System markers should be preserved
        expect(result).toContain("<!-- gh-aw-tracker-id: test-tracker-456 -->");
        expect(result).toContain("Generated by"); // Footer contains this text

        // Clean up
        delete process.env.GH_AW_WORKFLOW_NAME;
        delete process.env.GH_AW_WORKFLOW_SOURCE;
        delete process.env.GH_AW_WORKFLOW_SOURCE_URL;
        delete process.env.GH_AW_TRACKER_ID;
      });

      it("closes user fences before typed metadata, attribution, and the configured footer", () => {
        process.env.GH_AW_TRACKER_ID = "test-tracker-456";
        const result = buildCommentBody("```markdown\nuser content", 42, undefined, "**Configured footer**");

        expect(result).toMatch(/```markdown\nuser content\n```\n\n<!-- gh-aw-tracker-id: test-tracker-456 -->/);
        const trackerIndex = result.indexOf("<!-- gh-aw-tracker-id: test-tracker-456 -->");
        const attributionIndex = result.indexOf("> Generated by");
        const metadataIndex = result.indexOf("<!-- gh-aw-agentic-workflow:");
        const configuredFooterIndex = result.indexOf("**Configured footer**");
        expect(trackerIndex).toBeLessThan(attributionIndex);
        expect(attributionIndex).toBeLessThan(metadataIndex);
        expect(metadataIndex).toBeLessThan(configuredFooterIndex);
        delete process.env.GH_AW_TRACKER_ID;
      });

      it("should separate the body from the footer with a blank line so the quote renders", () => {
        process.env.GH_AW_WORKFLOW_NAME = "Test Workflow";
        process.env.GH_AW_WORKFLOW_SOURCE = "test.md";
        process.env.GH_AW_WORKFLOW_SOURCE_URL = "https://github.com/test/repo";
        process.env.GITHUB_SERVER_URL = "https://github.com";
        process.env.GITHUB_REPOSITORY = "test/repo";
        process.env.GITHUB_RUN_ID = "123";

        const result = buildCommentBody("User comment body", 42, undefined);

        // The footer attribution line must be preceded by at least one blank line
        // so Markdown parses the "> Generated by" quote section correctly.
        expect(result).toMatch(/\n\n> Generated by \[Test Workflow\]/);
        expect(result).not.toMatch(/[^\n]\n> Generated by \[Test Workflow\]/);

        delete process.env.GH_AW_WORKFLOW_NAME;
        delete process.env.GH_AW_WORKFLOW_SOURCE;
        delete process.env.GH_AW_WORKFLOW_SOURCE_URL;
        delete process.env.GITHUB_SERVER_URL;
        delete process.env.GITHUB_REPOSITORY;
        delete process.env.GITHUB_RUN_ID;
      });

      it("should separate the body from the footer with a blank line when a tracker ID is present", () => {
        process.env.GH_AW_WORKFLOW_NAME = "Test Workflow";
        process.env.GH_AW_WORKFLOW_SOURCE = "test.md";
        process.env.GH_AW_WORKFLOW_SOURCE_URL = "https://github.com/test/repo";
        process.env.GH_AW_TRACKER_ID = "test-tracker-456";
        process.env.GITHUB_SERVER_URL = "https://github.com";
        process.env.GITHUB_REPOSITORY = "test/repo";
        process.env.GITHUB_RUN_ID = "123";

        const result = buildCommentBody("User comment body", 42, undefined);

        // Even with the tracker-id marker inserted between body and footer, the
        // footer quote must be preceded by a blank line.
        expect(result).toContain("<!-- gh-aw-tracker-id: test-tracker-456 -->");
        expect(result).toMatch(/\n\n> Generated by \[Test Workflow\]/);

        delete process.env.GH_AW_WORKFLOW_NAME;
        delete process.env.GH_AW_WORKFLOW_SOURCE;
        delete process.env.GH_AW_WORKFLOW_SOURCE_URL;
        delete process.env.GH_AW_TRACKER_ID;
        delete process.env.GITHUB_SERVER_URL;
        delete process.env.GITHUB_REPOSITORY;
        delete process.env.GITHUB_RUN_ID;
      });

      it("should preserve footer markers when user content has malicious XML comments", () => {
        const { sanitizeContent } = require("./sanitize_content.cjs");

        const userContent = "Text with <!-- malicious --> comment";
        const sanitized = sanitizeContent(userContent);

        // User's malicious comment should be removed
        expect(sanitized).not.toContain("<!-- malicious -->");

        // Now add system marker after sanitization
        const withMarker = sanitized + "\n\n<!-- gh-aw-workflow-id: test -->";

        // System marker should still be present
        expect(withMarker).toContain("<!-- gh-aw-workflow-id: test -->");
      });
    }),
    describe("createCloseEntityHandler cross-repo validation", () => {
      const makeCallbacks = resolveTarget => ({
        resolveTarget,
        getDetails: vi.fn().mockResolvedValue({ number: 1, title: "t", labels: [], html_url: "u", state: "open" }),
        validateLabels: () => ({ valid: true }),
        buildCommentBody: body => body,
        addComment: vi.fn().mockResolvedValue({ id: 1, html_url: "u" }),
        closeEntity: vi.fn().mockResolvedValue({ number: 1, html_url: "u", title: "t" }),
        buildSuccessResult: (entity, comment, wasClosed, commentPosted) => ({ success: true }),
      });

      it("should reject cross-repo target not in allowlist", async () => {
        const config = { "target-repo": "testowner/testrepo", comment: "closing" };
        const handler = createCloseEntityHandler(
          config,
          ISSUE_CONFIG,
          makeCallbacks(() => ({ success: true, entityNumber: 1, owner: "other-org", repo: "other-repo" })),
          {}
        );
        const result = await handler({ body: "test", type: "close_issue" });
        expect(result.success).toBe(false);
        expect(result.error).toContain("not in the allowed-repos list");
        expect(mockCore.warning).toHaveBeenCalled();
      });

      it("should allow same-repo target", async () => {
        const config = { "target-repo": "testowner/testrepo", comment: "closing" };
        const handler = createCloseEntityHandler(
          config,
          ISSUE_CONFIG,
          makeCallbacks(() => ({ success: true, entityNumber: 1, owner: "testowner", repo: "testrepo" })),
          {}
        );
        const result = await handler({ body: "test", type: "close_issue" });
        expect(result.success).toBe(true);
      });

      it("should allow cross-repo target when in allowlist", async () => {
        const config = { "target-repo": "testowner/testrepo", allowed_repos: ["other-org/other-repo"], comment: "closing" };
        const handler = createCloseEntityHandler(
          config,
          ISSUE_CONFIG,
          makeCallbacks(() => ({ success: true, entityNumber: 1, owner: "other-org", repo: "other-repo" })),
          {}
        );
        const result = await handler({ body: "test", type: "close_issue" });
        expect(result.success).toBe(true);
      });
    }));
});
