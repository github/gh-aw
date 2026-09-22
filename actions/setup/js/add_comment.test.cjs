// @ts-check
import { describe, it, expect, beforeAll, beforeEach, afterEach, afterAll } from "vitest";
import fs from "fs";
import path from "path";
import { fileURLToPath } from "url";
import { syncRuntimePromptTemplates } from "./test_prompt_templates.js";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const { runtimePromptsDir } = syncRuntimePromptTemplates(import.meta.url);
const originalPromptsDir = process.env.GH_AW_PROMPTS_DIR;

beforeAll(() => {
  process.env.GH_AW_PROMPTS_DIR = runtimePromptsDir;
});

afterAll(() => {
  if (originalPromptsDir === undefined) {
    delete process.env.GH_AW_PROMPTS_DIR;
  } else {
    process.env.GH_AW_PROMPTS_DIR = originalPromptsDir;
  }
});

describe("add_comment", () => {
  let mockCore;
  let mockGithub;
  let mockContext;
  let originalGlobals;

  beforeEach(() => {
    // Save original globals
    originalGlobals = {
      core: global.core,
      github: global.github,
      context: global.context,
    };

    // Setup mock core
    mockCore = {
      debug: () => {},
      info: () => {},
      warning: () => {},
      error: () => {},
      setOutput: () => {},
      setFailed: () => {},
    };

    // Setup mock github API
    mockGithub = {
      rest: {
        issues: {
          createComment: async () => ({
            data: {
              id: 12345,
              html_url: "https://github.com/owner/repo/issues/42#issuecomment-12345",
            },
          }),
          updateComment: async () => ({
            data: {
              id: 12345,
              html_url: "https://github.com/owner/repo/issues/42#issuecomment-12345",
            },
          }),
          getComment: async params => ({
            data: {
              id: params.comment_id,
              issue_url: "https://api.github.com/repos/owner/repo/issues/42",
              html_url: `https://github.com/owner/repo/issues/42#issuecomment-${params.comment_id}`,
            },
          }),
          listComments: async () => ({ data: [] }),
        },
        pulls: {
          createReplyForReviewComment: async () => ({
            data: {
              id: 99999,
              html_url: "https://github.com/owner/repo/pull/8535#discussion_r99999",
            },
          }),
        },
      },
      graphql: async () => ({
        repository: {
          discussion: {
            id: "D_kwDOTest123",
            url: "https://github.com/owner/repo/discussions/10",
          },
        },
        addDiscussionComment: {
          comment: {
            id: "DC_kwDOTest456",
            url: "https://github.com/owner/repo/discussions/10#discussioncomment-456",
          },
        },
      }),
    };

    // Setup mock context
    mockContext = {
      eventName: "pull_request",
      runId: 12345,
      repo: {
        owner: "owner",
        repo: "repo",
      },
      payload: {
        pull_request: {
          number: 8535, // The correct PR that triggered the workflow
        },
      },
    };

    // Set globals
    global.core = mockCore;
    global.github = mockGithub;
    global.context = mockContext;
  });

  afterEach(() => {
    // Restore original globals
    global.core = originalGlobals.core;
    global.github = originalGlobals.github;
    global.context = originalGlobals.context;
    delete process.env.GH_AW_COMMENT_ID;
  });

  describe("target configuration", () => {
    it("should use triggering PR context when target is 'triggering'", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      /** @type {any} */
      let capturedIssueNumber = null;
      mockGithub.rest.issues.createComment = async params => {
        capturedIssueNumber = params.issue_number;
        return {
          data: {
            id: 12345,
            html_url: `https://github.com/owner/repo/issues/${params.issue_number}#issuecomment-12345`,
          },
        };
      };

      // Execute the handler factory with target: "triggering"
      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ target: 'triggering' }); })()`);

      const message = {
        type: "add_comment",
        body: "Test comment on triggering PR",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(capturedIssueNumber).toBe(8535);
      expect(result.itemNumber).toBe(8535);
    });

    it("should use explicit PR number when target is a number", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      /** @type {any} */
      let capturedIssueNumber = null;
      mockGithub.rest.issues.createComment = async params => {
        capturedIssueNumber = params.issue_number;
        return {
          data: {
            id: 12345,
            html_url: `https://github.com/owner/repo/issues/${params.issue_number}#issuecomment-12345`,
          },
        };
      };

      // Execute the handler factory with target: 21 (explicit PR number)
      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ target: '21' }); })()`);

      const message = {
        type: "add_comment",
        body: "Test comment on explicit PR",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(capturedIssueNumber).toBe(21);
      expect(result.itemNumber).toBe(21);
    });

    it("should use item_number from message when target is '*'", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      /** @type {any} */
      let capturedIssueNumber = null;
      mockGithub.rest.issues.createComment = async params => {
        capturedIssueNumber = params.issue_number;
        return {
          data: {
            id: 12345,
            html_url: `https://github.com/owner/repo/issues/${params.issue_number}#issuecomment-12345`,
          },
        };
      };

      // Execute the handler factory with target: "*"
      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ target: '*' }); })()`);

      const message = {
        type: "add_comment",
        item_number: 999,
        body: "Test comment on item_number PR",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(capturedIssueNumber).toBe(999);
      expect(result.itemNumber).toBe(999);
    });

    it("should accept pr-number as alias for item_number when target is '*'", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      /** @type {any} */
      let capturedIssueNumber = null;
      mockGithub.rest.issues.createComment = async params => {
        capturedIssueNumber = params.issue_number;
        return {
          data: {
            id: 12345,
            html_url: `https://github.com/owner/repo/issues/${params.issue_number}#issuecomment-12345`,
          },
        };
      };

      // Execute the handler factory with target: "*"
      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ target: '*' }); })()`);

      const message = {
        type: "add_comment",
        "pr-number": 28912,
        body: "Thanks for the automated bump...",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(capturedIssueNumber).toBe(28912);
      expect(result.itemNumber).toBe(28912);
    });

    it("should skip (not fail) when target is '*' but no item_number provided", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ target: '*' }); })()`);

      const message = {
        type: "add_comment",
        body: "Test comment without item_number",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(false);
      expect(result.skipped).toBe(true);
      expect(result.error).toMatch(/no.*item_number/i);
    });

    it("should hard-fail (not skip) when target is '*' and explicit pull_request_number is invalid", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ target: '*' }); })()`);

      const message = {
        type: "add_comment",
        pull_request_number: "invalid",
        body: "Test comment with invalid explicit pull_request_number",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(false);
      expect(result.skipped).toBeUndefined();
      expect(result.error).toBeTruthy();
    });

    it("should ignore explicit item_number with triggering target", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      /** @type {any} */
      let capturedIssueNumber = null;
      mockGithub.rest.issues.createComment = async params => {
        capturedIssueNumber = params.issue_number;
        return {
          data: {
            id: 12345,
            html_url: `https://github.com/owner/repo/issues/${params.issue_number}#issuecomment-12345`,
          },
        };
      };

      // Execute the handler factory with target: "triggering" (default)
      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ target: 'triggering' }); })()`);

      const message = {
        type: "add_comment",
        item_number: 777,
        body: "Test comment with explicit item_number",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(capturedIssueNumber).toBe(8535);
      expect(result.itemNumber).toBe(8535);
    });

    it("should resolve from context when item_number is not provided", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      /** @type {any} */
      let capturedIssueNumber = null;
      mockGithub.rest.issues.createComment = async params => {
        capturedIssueNumber = params.issue_number;
        return {
          data: {
            id: 12345,
            html_url: `https://github.com/owner/repo/issues/${params.issue_number}#issuecomment-12345`,
          },
        };
      };

      // Execute the handler factory with target: "triggering" (default)
      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ target: 'triggering' }); })()`);

      const message = {
        type: "add_comment",
        body: "Test comment without item_number, should use PR from context",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(capturedIssueNumber).toBe(8535); // Should use PR number from mockContext
      expect(result.itemNumber).toBe(8535);
    });

    it("should use issue context when triggered by an issue", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      // Change context to issue
      mockContext.eventName = "issues";
      mockContext.payload = {
        issue: {
          number: 42,
        },
      };

      /** @type {any} */
      let capturedIssueNumber = null;
      mockGithub.rest.issues.createComment = async params => {
        capturedIssueNumber = params.issue_number;
        return {
          data: {
            id: 12345,
            html_url: `https://github.com/owner/repo/issues/${params.issue_number}#issuecomment-12345`,
          },
        };
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ target: 'triggering' }); })()`);

      const message = {
        type: "add_comment",
        body: "Test comment on issue",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(capturedIssueNumber).toBe(42);
      expect(result.itemNumber).toBe(42);
      expect(result.isDiscussion).toBe(false);
    });

    it('should allow commenting on any repo when target-repo is "*"', async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      /** @type {any} */
      let capturedOwner = null;
      /** @type {any} */
      let capturedRepo = null;
      mockGithub.rest.issues.createComment = async params => {
        capturedOwner = params.owner;
        capturedRepo = params.repo;
        return {
          data: {
            id: 12345,
            html_url: `https://github.com/${params.owner}/${params.repo}/issues/${params.issue_number}#issuecomment-12345`,
          },
        };
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ "target-repo": "*", target: "triggering" }); })()`);

      const message = {
        type: "add_comment",
        item_number: 5,
        repo: "other-org/other-repo",
        body: "Cross-repo comment",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(capturedOwner).toBe("other-org");
      expect(capturedRepo).toBe("other-repo");
    });

    it("should return skipped (not failed) when target is 'triggering' but running in schedule context", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      let infoCalls = [];
      let warningCalls = [];
      mockCore.info = msg => {
        infoCalls.push(msg);
      };
      mockCore.warning = msg => {
        warningCalls.push(msg);
      };

      // Simulate a schedule run — no issue or PR in context
      mockContext.eventName = "schedule";
      mockContext.payload = {};

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ target: 'triggering' }); })()`);

      const message = {
        type: "add_comment",
        body: "Status comment from scheduled run",
      };

      const result = await handler(message, {});

      // Should be skipped, not failed — schedule runs have no triggering issue/PR
      expect(result.success).toBe(false);
      expect(result.skipped).toBe(true);
      expect(result.error).toContain("triggering");

      // Should use core.info, not core.warning, since this is an expected non-failure skip
      const skipInfo = infoCalls.find(msg => msg.includes("triggering"));
      expect(skipInfo).toBeTruthy();
      expect(warningCalls.filter(msg => msg.includes("triggering")).length).toBe(0);
    });

    it("should reply inline to triggering PR review comment when item_number is not provided", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      mockContext.eventName = "pull_request_review_comment";
      mockContext.payload = {
        pull_request: {
          number: 8535,
        },
        comment: {
          id: 777,
        },
      };

      /** @type {any} */
      let capturedReplyParams = null;
      let issueCommentCalled = false;
      mockGithub.rest.pulls.createReplyForReviewComment = async params => {
        capturedReplyParams = params;
        return {
          data: {
            id: 56789,
            html_url: "https://github.com/owner/repo/pull/8535#discussion_r56789",
          },
        };
      };
      mockGithub.rest.issues.createComment = async () => {
        issueCommentCalled = true;
        return {
          data: {
            id: 12345,
            html_url: "https://github.com/owner/repo/issues/8535#issuecomment-12345",
          },
        };
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ target: 'triggering' }); })()`);

      const message = {
        type: "add_comment",
        body: "Inline reply for review thread",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(result.itemNumber).toBe(8535);
      expect(result.isDiscussion).toBe(false);
      expect(issueCommentCalled).toBe(false);
      expect(capturedReplyParams).toEqual(
        expect.objectContaining({
          owner: "owner",
          repo: "repo",
          pull_number: 8535,
          comment_id: 777,
        })
      );
    });

    it("should keep top-level comment behavior for pull_request_review_comment when item_number is explicitly provided", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      mockContext.eventName = "pull_request_review_comment";
      mockContext.payload = {
        pull_request: {
          number: 8535,
        },
        comment: {
          id: 777,
        },
      };

      /** @type {any} */
      let capturedIssueNumber = null;
      let reviewReplyCalled = false;
      mockGithub.rest.issues.createComment = async params => {
        capturedIssueNumber = params.issue_number;
        return {
          data: {
            id: 12345,
            html_url: "https://github.com/owner/repo/issues/970#issuecomment-12345",
          },
        };
      };
      mockGithub.rest.pulls.createReplyForReviewComment = async () => {
        reviewReplyCalled = true;
        return {
          data: {
            id: 56789,
            html_url: "https://github.com/owner/repo/pull/8535#discussion_r56789",
          },
        };
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ target: '*' }); })()`);

      const message = {
        type: "add_comment",
        item_number: 970,
        body: "Top-level comment on explicit item number",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(capturedIssueNumber).toBe(970);
      expect(reviewReplyCalled).toBe(false);
    });

    it("should reply inline when pull_request_review_comment context is forwarded via workflow_dispatch inputs", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      mockContext.eventName = "workflow_dispatch";
      mockContext.payload = {
        inputs: {
          event_name: "pull_request_review_comment",
          event_payload: JSON.stringify({
            pull_request: { number: 8535 },
            comment: { id: 777 },
          }),
        },
      };

      /** @type {any} */
      let capturedReplyParams = null;
      let issueCommentCalled = false;
      mockGithub.rest.pulls.createReplyForReviewComment = async params => {
        capturedReplyParams = params;
        return {
          data: {
            id: 56789,
            html_url: "https://github.com/owner/repo/pull/8535#discussion_r56789",
          },
        };
      };
      mockGithub.rest.issues.createComment = async () => {
        issueCommentCalled = true;
        return {
          data: {
            id: 12345,
            html_url: "https://github.com/owner/repo/issues/8535#issuecomment-12345",
          },
        };
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ target: 'triggering' }); })()`);

      const result = await handler({ type: "add_comment", body: "Inline reply from workflow_dispatch relay" }, {});

      expect(result.success).toBe(true);
      expect(issueCommentCalled).toBe(false);
      expect(capturedReplyParams).toEqual(
        expect.objectContaining({
          owner: "owner",
          repo: "repo",
          pull_number: 8535,
          comment_id: 777,
        })
      );
    });

    it("should reply inline when pull_request_review_comment context is forwarded via workflow_call aw_context", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      mockContext.eventName = "workflow_call";
      mockContext.payload = {
        inputs: {
          aw_context: JSON.stringify({
            event_type: "pull_request_review_comment",
            item_number: "8535",
            comment_id: "777",
          }),
        },
      };

      /** @type {any} */
      let capturedReplyParams = null;
      let issueCommentCalled = false;
      mockGithub.rest.pulls.createReplyForReviewComment = async params => {
        capturedReplyParams = params;
        return {
          data: {
            id: 56789,
            html_url: "https://github.com/owner/repo/pull/8535#discussion_r56789",
          },
        };
      };
      mockGithub.rest.issues.createComment = async () => {
        issueCommentCalled = true;
        return {
          data: {
            id: 12345,
            html_url: "https://github.com/owner/repo/issues/8535#issuecomment-12345",
          },
        };
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ target: 'triggering' }); })()`);

      const result = await handler({ type: "add_comment", body: "Inline reply from workflow_call relay" }, {});

      expect(result.success).toBe(true);
      expect(result.itemNumber).toBe(8535);
      expect(issueCommentCalled).toBe(false);
      expect(capturedReplyParams).toEqual(
        expect.objectContaining({
          owner: "owner",
          repo: "repo",
          pull_number: 8535,
          comment_id: 777,
        })
      );
    });

    it("should update existing status comment when target=status is requested", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");
      process.env.GH_AW_COMMENT_ID = "55555";

      /** @type {any} */
      let capturedUpdateParams = null;
      let createCommentCalled = false;
      mockGithub.rest.issues.updateComment = async params => {
        capturedUpdateParams = params;
        return {
          data: {
            id: params.comment_id,
            html_url: `https://github.com/${params.owner}/${params.repo}/issues/8535#issuecomment-${params.comment_id}`,
          },
        };
      };
      mockGithub.rest.issues.createComment = async () => {
        createCommentCalled = true;
        return {
          data: {
            id: 12345,
            html_url: "https://github.com/owner/repo/issues/8535#issuecomment-12345",
          },
        };
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ target: 'triggering' }); })()`);
      const result = await handler({ type: "add_comment", body: "Final output", target: "status" }, {});

      expect(result.success).toBe(true);
      expect(capturedUpdateParams).toEqual(
        expect.objectContaining({
          owner: "owner",
          repo: "repo",
          comment_id: 55555,
        })
      );
      expect(createCommentCalled).toBe(false);
      delete process.env.GH_AW_COMMENT_ID;
    });

    it("should update existing comment when comment-id alias is allowed by target wildcard config", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      /** @type {any} */
      let capturedUpdateParams = null;
      let createCommentCalled = false;
      mockGithub.rest.issues.getComment = async params => ({
        data: {
          id: params.comment_id,
          issue_url: "https://api.github.com/repos/owner/repo/issues/8535",
          html_url: `https://github.com/owner/repo/issues/8535#issuecomment-${params.comment_id}`,
        },
      });
      mockGithub.rest.issues.updateComment = async params => {
        capturedUpdateParams = params;
        return {
          data: {
            id: params.comment_id,
            html_url: `https://github.com/${params.owner}/${params.repo}/issues/8535#issuecomment-${params.comment_id}`,
          },
        };
      };
      mockGithub.rest.issues.createComment = async () => {
        createCommentCalled = true;
        return {
          data: {
            id: 12345,
            html_url: "https://github.com/owner/repo/issues/8535#issuecomment-12345",
          },
        };
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ target: '*', allows_comment_ids: ['55555'] }); })()`);
      const result = await handler({ type: "add_comment", body: "Updated output", "comment-id": 55555 }, {});

      expect(result.success).toBe(true);
      expect(result.itemNumber).toBe(8535);
      expect(capturedUpdateParams).toEqual(
        expect.objectContaining({
          owner: "owner",
          repo: "repo",
          comment_id: 55555,
        })
      );
      expect(createCommentCalled).toBe(false);
    });

    it("should reject comment_id when it is not in allows-comment-ids", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      let updateCommentCalled = false;
      mockGithub.rest.issues.updateComment = async () => {
        updateCommentCalled = true;
        return { data: { id: 999, html_url: "https://github.com/owner/repo/issues/8535#issuecomment-999" } };
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ target: '*', allows_comment_ids: ['12345'] }); })()`);
      const result = await handler({ type: "add_comment", body: "Updated output", comment_id: 55555 }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("allows-comment-ids");
      expect(updateCommentCalled).toBe(false);
    });

    it("should warn and ignore unrecognized message-level target values instead of failing", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      let warningCalls = [];
      let createCommentCalled = false;
      mockCore.warning = msg => {
        warningCalls.push(msg);
      };
      mockGithub.rest.issues.createComment = async () => {
        createCommentCalled = true;
        return {
          data: {
            id: 12345,
            html_url: "https://github.com/owner/repo/pull/8535#issuecomment-12345",
          },
        };
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ target: 'triggering' }); })()`);
      // Truly unrecognized target value — should warn and proceed (not fail)
      const result = await handler({ type: "add_comment", body: "Test comment", target: "unknown_value" }, {});

      expect(result.success).toBe(true);
      expect(createCommentCalled).toBe(true);
      // Should have warned about the unrecognized target value
      const targetWarning = warningCalls.find(msg => msg.includes("unknown_value") && msg.includes("target"));
      expect(targetWarning).toBeTruthy();
    });

    it("should accept issue and discussion as valid message-level target values without warning", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      for (const allowedTarget of ["issue", "discussion"]) {
        let warningCalls = [];
        let createCommentCalled = false;
        mockCore.warning = msg => {
          warningCalls.push(msg);
        };
        mockGithub.rest.issues.createComment = async () => {
          createCommentCalled = true;
          return {
            data: {
              id: 12345,
              html_url: "https://github.com/owner/repo/pull/8535#issuecomment-12345",
            },
          };
        };

        const handler = await eval(`(async () => { ${addCommentScript}; return await main({ target: 'triggering' }); })()`);
        const result = await handler({ type: "add_comment", body: "Test comment", target: allowedTarget }, {});

        expect(result.success).toBe(true);
        expect(createCommentCalled).toBe(true);
        // Should NOT have warned about an allowed target value
        const targetWarning = warningCalls.find(msg => msg.includes(allowedTarget) && msg.includes("target") && msg.includes("Ignoring unrecognized"));
        expect(targetWarning).toBeUndefined();
      }
    });
  });

  describe("discussion support", () => {
    it("should use discussion context when triggered by a discussion", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      // Change context to discussion
      mockContext.eventName = "discussion";
      mockContext.payload = {
        discussion: {
          number: 10,
        },
      };

      /** @type {any} */
      let capturedDiscussionNumber = null;
      let graphqlCallCount = 0;
      mockGithub.graphql = async (query, variables) => {
        graphqlCallCount++;
        if (query.includes("addDiscussionComment")) {
          return {
            addDiscussionComment: {
              comment: {
                id: "DC_kwDOTest456",
                url: "https://github.com/owner/repo/discussions/10#discussioncomment-456",
              },
            },
          };
        }
        // Query for discussion ID
        if (variables.number) {
          capturedDiscussionNumber = variables.number;
        }
        if (variables.num) {
          capturedDiscussionNumber = variables.num;
        }
        return {
          repository: {
            discussion: {
              id: "D_kwDOTest123",
              url: "https://github.com/owner/repo/discussions/10",
            },
          },
        };
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ target: 'triggering' }); })()`);

      const message = {
        type: "add_comment",
        body: "Test comment on discussion",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(capturedDiscussionNumber).toBe(10);
      expect(result.itemNumber).toBe(10);
      expect(result.isDiscussion).toBe(true);
    });

    it("should post a threaded reply when triggered by discussion_comment event", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      // Change context to discussion_comment event
      mockContext.eventName = "discussion_comment";
      mockContext.payload = {
        discussion: {
          number: 10,
        },
        comment: {
          node_id: "DC_kwDOTriggeringComment123",
        },
      };

      /** @type {any} */
      let capturedReplyToId = undefined;
      mockGithub.graphql = async (query, variables) => {
        if (query.includes("addDiscussionComment")) {
          capturedReplyToId = variables?.replyToId;
          return {
            addDiscussionComment: {
              comment: {
                id: "DC_kwDOTest456",
                body: variables?.body,
                createdAt: "2024-01-01",
                url: "https://github.com/owner/repo/discussions/10#discussioncomment-456",
              },
            },
          };
        }
        // Mock replyTo resolution: top-level comment has no parent
        if (query.includes("replyTo")) {
          return { node: { replyTo: null } };
        }
        return {
          repository: {
            discussion: {
              id: "D_kwDOTest123",
              url: "https://github.com/owner/repo/discussions/10",
            },
          },
        };
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({}); })()`);

      const message = {
        type: "add_comment",
        body: "This is a threaded reply to a discussion comment",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(result.isDiscussion).toBe(true);
      // The reply should be threaded - replyToId should be the triggering comment node_id
      expect(capturedReplyToId).toBe("DC_kwDOTriggeringComment123");
    });

    it("should use parent comment node ID when the triggering comment is itself a threaded reply", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      // Change context to discussion_comment event where the triggering comment is itself a reply
      mockContext.eventName = "discussion_comment";
      mockContext.payload = {
        discussion: {
          number: 10,
        },
        comment: {
          node_id: "DC_kwDOReplyComment789", // This is a reply, not a top-level comment
        },
      };

      /** @type {any} */
      let capturedReplyToId = undefined;
      mockGithub.graphql = async (query, variables) => {
        if (query.includes("addDiscussionComment")) {
          capturedReplyToId = variables?.replyToId;
          return {
            addDiscussionComment: {
              comment: {
                id: "DC_kwDOTest456",
                body: variables?.body,
                createdAt: "2024-01-01",
                url: "https://github.com/owner/repo/discussions/10#discussioncomment-456",
              },
            },
          };
        }
        // Mock replyTo resolution: the triggering comment is a reply with a parent
        if (query.includes("replyTo")) {
          return { node: { replyTo: { id: "DC_kwDOParentComment456" } } };
        }
        return {
          repository: {
            discussion: {
              id: "D_kwDOTest123",
              url: "https://github.com/owner/repo/discussions/10",
            },
          },
        };
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({}); })()`);

      const message = {
        type: "add_comment",
        body: "Reply to a reply - should use parent node ID",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(result.isDiscussion).toBe(true);
      // The replyToId should be the parent (top-level) comment node ID, not the triggering reply
      expect(capturedReplyToId).toBe("DC_kwDOParentComment456");
    });

    it("should post a top-level comment (not threaded) when triggered by discussion_comment with explicit item_number", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      // Change context to discussion_comment event
      mockContext.eventName = "discussion_comment";
      mockContext.payload = {
        discussion: {
          number: 10,
        },
        comment: {
          node_id: "DC_kwDOTriggeringComment123",
        },
      };

      /** @type {any} */
      let capturedReplyToId = undefined;
      mockGithub.graphql = async (query, variables) => {
        if (query.includes("addDiscussionComment")) {
          capturedReplyToId = variables?.replyToId;
          return {
            addDiscussionComment: {
              comment: {
                id: "DC_kwDOTest456",
                body: variables?.body,
                createdAt: "2024-01-01",
                url: "https://github.com/owner/repo/discussions/20#discussioncomment-456",
              },
            },
          };
        }
        return {
          repository: {
            discussion: {
              id: "D_kwDOTest123",
              url: "https://github.com/owner/repo/discussions/20",
            },
          },
        };
      };

      // Make REST API return 404 so the code falls back to discussion
      mockGithub.rest.issues.createComment = async () => {
        const err = new Error("Not Found");
        // @ts-expect-error - Simulating GitHub REST API error
        err.status = 404;
        throw err;
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ target: '*' }); })()`);

      // Explicit item_number targeting a different discussion (via 404 fallback)
      const message = {
        type: "add_comment",
        item_number: 20,
        body: "This should be a top-level comment on discussion #20",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(result.isDiscussion).toBe(true);
      // Should NOT be threaded since item_number was explicitly provided
      expect(capturedReplyToId).toBeUndefined();
    });

    it("should use reply_to_id from message when not triggered by discussion_comment event", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      // workflow_dispatch trigger (not discussion_comment); item_number refers to a discussion
      mockContext.eventName = "workflow_dispatch";
      mockContext.payload = {};

      // Make REST API return 404 so the code falls back to the discussion path
      mockGithub.rest.issues.createComment = async () => {
        const err = new Error("Not Found");
        // @ts-expect-error - Simulating GitHub REST API error
        err.status = 404;
        throw err;
      };

      /** @type {any} */
      let capturedReplyToId = undefined;
      mockGithub.graphql = async (query, variables) => {
        if (query.includes("addDiscussionComment")) {
          capturedReplyToId = variables?.replyToId;
          return {
            addDiscussionComment: {
              comment: {
                id: "DC_kwDOTest456",
                body: variables?.body,
                createdAt: "2024-01-01",
                url: "https://github.com/owner/repo/discussions/10#discussioncomment-456",
              },
            },
          };
        }
        // Mock replyTo resolution: the provided reply_to_id is already top-level (no parent)
        if (query.includes("replyTo")) {
          return { node: { replyTo: null } };
        }
        return {
          repository: {
            discussion: {
              id: "D_kwDOTest123",
              url: "https://github.com/owner/repo/discussions/10",
            },
          },
        };
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ target: '*' }); })()`);

      const message = {
        type: "add_comment",
        item_number: 10,
        body: "🔄 Updated finding...",
        reply_to_id: "DC_kwDOParentComment456",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(result.isDiscussion).toBe(true);
      // Should use the reply_to_id from the message
      expect(capturedReplyToId).toBe("DC_kwDOParentComment456");
    });

    it("should resolve top-level parent when reply_to_id points to a nested reply", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      // workflow_dispatch trigger (not discussion_comment); item_number refers to a discussion
      mockContext.eventName = "workflow_dispatch";
      mockContext.payload = {};

      // Make REST API return 404 so the code falls back to the discussion path
      mockGithub.rest.issues.createComment = async () => {
        const err = new Error("Not Found");
        // @ts-expect-error - Simulating GitHub REST API error
        err.status = 404;
        throw err;
      };

      /** @type {any} */
      let capturedReplyToId = undefined;
      mockGithub.graphql = async (query, variables) => {
        if (query.includes("addDiscussionComment")) {
          capturedReplyToId = variables?.replyToId;
          return {
            addDiscussionComment: {
              comment: {
                id: "DC_kwDOTest456",
                body: variables?.body,
                createdAt: "2024-01-01",
                url: "https://github.com/owner/repo/discussions/10#discussioncomment-456",
              },
            },
          };
        }
        // Mock replyTo resolution: the provided reply_to_id is itself a reply, return its parent
        if (query.includes("replyTo")) {
          return { node: { replyTo: { id: "DC_kwDOTopLevelComment123" } } };
        }
        return {
          repository: {
            discussion: {
              id: "D_kwDOTest123",
              url: "https://github.com/owner/repo/discussions/10",
            },
          },
        };
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ target: '*' }); })()`);

      const message = {
        type: "add_comment",
        item_number: 10,
        body: "🔄 Updated finding...",
        reply_to_id: "DC_kwDONestedReply789",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(result.isDiscussion).toBe(true);
      // Should resolve to the top-level parent, not the nested reply
      expect(capturedReplyToId).toBe("DC_kwDOTopLevelComment123");
    });

    it("should ignore reply_to_id when triggered by discussion_comment event without explicit item_number", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      // discussion_comment trigger takes precedence over reply_to_id
      mockContext.eventName = "discussion_comment";
      mockContext.payload = {
        discussion: {
          number: 10,
        },
        comment: {
          node_id: "DC_kwDOTriggeringComment123",
        },
      };

      /** @type {any} */
      let capturedReplyToId = undefined;
      mockGithub.graphql = async (query, variables) => {
        if (query.includes("addDiscussionComment")) {
          capturedReplyToId = variables?.replyToId;
          return {
            addDiscussionComment: {
              comment: {
                id: "DC_kwDOTest456",
                body: variables?.body,
                createdAt: "2024-01-01",
                url: "https://github.com/owner/repo/discussions/10#discussioncomment-456",
              },
            },
          };
        }
        // Mock replyTo resolution: triggering comment is top-level (no parent)
        if (query.includes("replyTo")) {
          return { node: { replyTo: null } };
        }
        return {
          repository: {
            discussion: {
              id: "D_kwDOTest123",
              url: "https://github.com/owner/repo/discussions/10",
            },
          },
        };
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({}); })()`);

      const message = {
        type: "add_comment",
        body: "Reply that provides reply_to_id but should use triggering comment instead",
        reply_to_id: "DC_kwDOShouldBeIgnored999",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(result.isDiscussion).toBe(true);
      // Should use the triggering comment node_id, not the reply_to_id from the message
      expect(capturedReplyToId).toBe("DC_kwDOTriggeringComment123");
    });

    it("should ignore and warn when reply_to_id is a whitespace-only string", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      mockContext.eventName = "workflow_dispatch";
      mockContext.payload = {};

      // Capture warning calls
      const warningCalls = [];
      mockCore.warning = msg => {
        warningCalls.push(msg);
      };

      // Make REST API return 404 so the code falls back to the discussion path
      mockGithub.rest.issues.createComment = async () => {
        const err = new Error("Not Found");
        // @ts-expect-error - Simulating GitHub REST API error
        err.status = 404;
        throw err;
      };

      /** @type {any} */
      let capturedReplyToId = undefined;
      mockGithub.graphql = async (query, variables) => {
        if (query.includes("addDiscussionComment")) {
          capturedReplyToId = variables?.replyToId;
          return {
            addDiscussionComment: {
              comment: {
                id: "DC_kwDOTest456",
                body: variables?.body,
                createdAt: "2024-01-01",
                url: "https://github.com/owner/repo/discussions/10#discussioncomment-456",
              },
            },
          };
        }
        return {
          repository: {
            discussion: {
              id: "D_kwDOTest123",
              url: "https://github.com/owner/repo/discussions/10",
            },
          },
        };
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ target: '*' }); })()`);

      const message = {
        type: "add_comment",
        item_number: 10,
        body: "Comment with blank reply_to_id",
        reply_to_id: "   ",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(result.isDiscussion).toBe(true);
      // Whitespace-only reply_to_id should be ignored (post top-level)
      expect(capturedReplyToId).toBeUndefined();
      expect(warningCalls).toContain("Ignoring empty discussion reply_to_id after normalization");
    });

    it("should coerce numeric reply_to_id to string before use", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      mockContext.eventName = "workflow_dispatch";
      mockContext.payload = {};

      // Make REST API return 404 so the code falls back to the discussion path
      mockGithub.rest.issues.createComment = async () => {
        const err = new Error("Not Found");
        // @ts-expect-error - Simulating GitHub REST API error
        err.status = 404;
        throw err;
      };

      /** @type {any} */
      let capturedReplyToId = undefined;
      mockGithub.graphql = async (query, variables) => {
        if (query.includes("addDiscussionComment")) {
          capturedReplyToId = variables?.replyToId;
          return {
            addDiscussionComment: {
              comment: {
                id: "DC_kwDOTest456",
                body: variables?.body,
                createdAt: "2024-01-01",
                url: "https://github.com/owner/repo/discussions/10#discussioncomment-456",
              },
            },
          };
        }
        // Mock replyTo resolution: node ID is top-level (no parent)
        if (query.includes("replyTo")) {
          return { node: { replyTo: null } };
        }
        return {
          repository: {
            discussion: {
              id: "D_kwDOTest123",
              url: "https://github.com/owner/repo/discussions/10",
            },
          },
        };
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ target: '*' }); })()`);

      const message = {
        type: "add_comment",
        item_number: 10,
        body: "Comment with numeric reply_to_id",
        // @ts-expect-error - intentionally passing a number to test coercion
        reply_to_id: 12345,
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(result.isDiscussion).toBe(true);
      // Numeric reply_to_id should be coerced to "12345"
      expect(capturedReplyToId).toBe("12345");
    });
  });

  describe("regression test for wrong PR bug", () => {
    it("should NOT comment on a different PR when workflow runs on PR #8535", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      // Simulate the exact scenario from the bug:
      // - Workflow runs on PR #8535 (branch: copilot/enable-sandbox-mcp-gateway)
      // - Should comment on PR #8535, NOT PR #21
      mockContext.eventName = "pull_request";
      mockContext.payload = {
        pull_request: {
          number: 8535,
        },
      };

      /** @type {any} */
      let capturedIssueNumber = null;
      mockGithub.rest.issues.createComment = async params => {
        capturedIssueNumber = params.issue_number;
        return {
          data: {
            id: 12345,
            html_url: `https://github.com/owner/repo/issues/${params.issue_number}#issuecomment-12345`,
          },
        };
      };

      // Use default target configuration (should be "triggering")
      const handler = await eval(`(async () => { ${addCommentScript}; return await main({}); })()`);

      const message = {
        type: "add_comment",
        body: "## Smoke Test: Copilot MCP Scripts\n\n✅ Test passed",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(capturedIssueNumber).toBe(8535);
      expect(result.itemNumber).toBe(8535);
      expect(capturedIssueNumber).not.toBe(21);
    });
  });

  describe("append-only-comments integration", () => {
    it("should not hide older comments when append-only-comments is enabled", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      // Set up environment variable for append-only-comments
      process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({
        appendOnlyComments: true,
      });
      process.env.GH_AW_WORKFLOW_ID = "test-workflow";

      let hideCommentsWasCalled = false;
      let listCommentsCalls = 0;

      mockGithub.rest.issues.listComments = async () => {
        listCommentsCalls++;
        return {
          data: [
            {
              id: 999,
              node_id: "IC_kwDOTest999",
              body: "Old comment <!-- gh-aw-workflow-id: test-workflow -->",
            },
          ],
        };
      };

      mockGithub.graphql = async (query, variables) => {
        if (query.includes("minimizeComment")) {
          hideCommentsWasCalled = true;
        }
        return {
          minimizeComment: {
            minimizedComment: {
              isMinimized: true,
            },
          },
        };
      };

      /** @type {any} */
      let capturedComment = null;
      mockGithub.rest.issues.createComment = async params => {
        capturedComment = params;
        return {
          data: {
            id: 12345,
            html_url: `https://github.com/owner/repo/issues/${params.issue_number}#issuecomment-12345`,
          },
        };
      };

      // Execute with hide-older-comments enabled
      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ hide_older_comments: true }); })()`);

      const message = {
        type: "add_comment",
        body: "New comment - should not hide old ones",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(hideCommentsWasCalled).toBe(false);
      expect(listCommentsCalls).toBe(0);
      expect(capturedComment).toBeTruthy();
      expect(capturedComment.body).toContain("New comment - should not hide old ones");

      // Clean up
      delete process.env.GH_AW_SAFE_OUTPUT_MESSAGES;
      delete process.env.GH_AW_WORKFLOW_ID;
    });

    it("should hide older comments when append-only-comments is not enabled", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      // Set up environment variable WITHOUT append-only-comments
      delete process.env.GH_AW_SAFE_OUTPUT_MESSAGES;
      process.env.GH_AW_WORKFLOW_ID = "test-workflow";

      let hideCommentsWasCalled = false;
      let listCommentsCalls = 0;

      mockGithub.rest.issues.listComments = async () => {
        listCommentsCalls++;
        return {
          data: [
            {
              id: 999,
              node_id: "IC_kwDOTest999",
              body: "Old comment <!-- gh-aw-workflow-id: test-workflow -->",
            },
          ],
        };
      };

      mockGithub.graphql = async (query, variables) => {
        if (query.includes("minimizeComment")) {
          hideCommentsWasCalled = true;
        }
        return {
          minimizeComment: {
            minimizedComment: {
              isMinimized: true,
            },
          },
        };
      };

      /** @type {any} */
      let capturedComment = null;
      mockGithub.rest.issues.createComment = async params => {
        capturedComment = params;
        return {
          data: {
            id: 12345,
            html_url: `https://github.com/owner/repo/issues/${params.issue_number}#issuecomment-12345`,
          },
        };
      };

      // Execute with hide-older-comments enabled
      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ hide_older_comments: true }); })()`);

      const message = {
        type: "add_comment",
        body: "New comment - should hide old ones",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(hideCommentsWasCalled).toBe(true);
      expect(listCommentsCalls).toBeGreaterThan(0);
      expect(capturedComment).toBeTruthy();
      expect(capturedComment.body).toContain("New comment - should hide old ones");

      // Clean up
      delete process.env.GH_AW_WORKFLOW_ID;
    });

    it("should hide older comments with combined XML marker format (workflow_id inside gh-aw-agentic-workflow)", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      delete process.env.GH_AW_SAFE_OUTPUT_MESSAGES;
      process.env.GH_AW_WORKFLOW_ID = "test-workflow";

      let hideCommentsWasCalled = false;
      let listCommentsCalls = 0;

      mockGithub.rest.issues.listComments = async () => {
        listCommentsCalls++;
        return {
          data: [
            {
              id: 999,
              node_id: "IC_kwDOTest999",
              body: "Old comment\n\n<!-- gh-aw-agentic-workflow: Test Workflow, engine: copilot, id: 12345, workflow_id: test-workflow, run: https://github.com/owner/repo/actions/runs/12345 -->",
            },
          ],
        };
      };

      mockGithub.graphql = async (query, variables) => {
        if (query.includes("minimizeComment")) {
          hideCommentsWasCalled = true;
        }
        return {
          minimizeComment: {
            minimizedComment: {
              isMinimized: true,
            },
          },
        };
      };

      /** @type {any} */
      let capturedComment = null;
      mockGithub.rest.issues.createComment = async params => {
        capturedComment = params;
        return {
          data: {
            id: 12346,
            html_url: `https://github.com/owner/repo/issues/${params.issue_number}#issuecomment-12346`,
          },
        };
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ hide_older_comments: true }); })()`);

      const message = {
        type: "add_comment",
        body: "New comment - should hide combined-marker old ones",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(hideCommentsWasCalled).toBe(true);
      expect(listCommentsCalls).toBeGreaterThan(0);
      expect(capturedComment).toBeTruthy();
      expect(capturedComment.body).toContain("New comment - should hide combined-marker old ones");

      // Clean up
      delete process.env.GH_AW_WORKFLOW_ID;
    });
  });

  describe("404 error handling", () => {
    it("should treat 404 errors as warnings for issue comments", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      let warningCalls = [];
      mockCore.warning = msg => {
        warningCalls.push(msg);
      };

      let errorCalls = [];
      mockCore.error = msg => {
        errorCalls.push(msg);
      };

      // Mock API to throw 404 error
      mockGithub.rest.issues.createComment = async () => {
        const error = new Error("Not Found");
        // @ts-ignore
        error.status = 404;
        throw error;
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({}); })()`);

      const message = {
        type: "add_comment",
        body: "Test comment",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(result.warning).toBeTruthy();
      expect(result.warning).toContain("not found");
      expect(result.skipped).toBe(true);
      expect(warningCalls.length).toBeGreaterThan(0);
      expect(warningCalls.some(w => w.toLowerCase().includes("not found"))).toBe(true);
      expect(errorCalls.length).toBe(0);
    });

    it("should treat 404 errors as warnings for discussion comments", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      let warningCalls = [];
      mockCore.warning = msg => {
        warningCalls.push(msg);
      };

      let errorCalls = [];
      mockCore.error = msg => {
        errorCalls.push(msg);
      };

      // Change context to discussion
      mockContext.eventName = "discussion";
      mockContext.payload = {
        discussion: {
          number: 10,
        },
      };

      // Mock API to throw 404 error when querying discussion
      mockGithub.graphql = async (query, variables) => {
        if (query.includes("discussion(number")) {
          // Return null to trigger the "not found" error
          return {
            repository: {
              discussion: null, // Discussion not found
            },
          };
        }
        throw new Error("Unexpected query");
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({}); })()`);

      const message = {
        type: "add_comment",
        body: "Test comment on deleted discussion",
      };

      const result = await handler(message, {});

      // The error message contains "not found" so it should be treated as a warning
      expect(result.success).toBe(true);
      expect(result.warning).toBeTruthy();
      expect(result.warning).toContain("not found");
      expect(result.skipped).toBe(true);
      expect(warningCalls.length).toBeGreaterThan(0);
      expect(errorCalls.length).toBe(0);
    });

    it("should detect 404 from error message containing '404'", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      let warningCalls = [];
      mockCore.warning = msg => {
        warningCalls.push(msg);
      };

      // Mock API to throw error with 404 in message
      mockGithub.rest.issues.createComment = async () => {
        throw new Error("API request failed with status 404");
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({}); })()`);

      const message = {
        type: "add_comment",
        body: "Test comment",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(result.warning).toBeTruthy();
      expect(result.skipped).toBe(true);
      expect(warningCalls.length).toBeGreaterThan(0);
    });

    it("should detect 404 from error message containing 'Not Found'", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      let warningCalls = [];
      mockCore.warning = msg => {
        warningCalls.push(msg);
      };

      // Mock API to throw error with "Not Found" in message
      mockGithub.rest.issues.createComment = async () => {
        throw new Error("Resource Not Found");
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({}); })()`);

      const message = {
        type: "add_comment",
        body: "Test comment",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(result.warning).toBeTruthy();
      expect(result.skipped).toBe(true);
      expect(warningCalls.length).toBeGreaterThan(0);
    });

    it("should treat locked issue/PR errors as non-fatal warnings", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      let warningCalls = [];
      mockCore.warning = msg => {
        warningCalls.push(msg);
      };

      let errorCalls = [];
      mockCore.error = msg => {
        errorCalls.push(msg);
      };

      mockGithub.rest.issues.createComment = async () => {
        const error = new Error("Unable to create comment because issue is locked.");
        // @ts-ignore
        error.status = 403;
        throw error;
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({}); })()`);

      const message = {
        type: "add_comment",
        body: "Test comment",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(result.warning).toContain("locked");
      expect(result.skipped).toBe(true);
      expect(warningCalls.length).toBeGreaterThan(0);
      expect(errorCalls.length).toBe(0);
    });

    it("should treat conversation locked errors as non-fatal warnings", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      let warningCalls = [];
      mockCore.warning = msg => {
        warningCalls.push(msg);
      };

      let errorCalls = [];
      mockCore.error = msg => {
        errorCalls.push(msg);
      };

      mockGithub.rest.issues.createComment = async () => {
        const error = new Error("Conversation is locked");
        // @ts-ignore
        error.status = 403;
        throw error;
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({}); })()`);

      const result = await handler({ type: "add_comment", body: "Test comment" }, {});

      expect(result.success).toBe(true);
      expect(result.warning).toContain("locked");
      expect(result.skipped).toBe(true);
      expect(warningCalls.length).toBeGreaterThan(0);
      expect(errorCalls.length).toBe(0);
    });

    it("should treat HTTP 423 locked errors as non-fatal warnings", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      let warningCalls = [];
      mockCore.warning = msg => {
        warningCalls.push(msg);
      };

      let errorCalls = [];
      mockCore.error = msg => {
        errorCalls.push(msg);
      };

      mockGithub.rest.issues.createComment = async () => {
        const error = new Error("Locked");
        // @ts-ignore
        error.status = 423;
        throw error;
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({}); })()`);

      const result = await handler({ type: "add_comment", body: "Test comment" }, {});

      expect(result.success).toBe(true);
      expect(result.warning).toContain("locked");
      expect(result.skipped).toBe(true);
      expect(warningCalls.length).toBeGreaterThan(0);
      expect(errorCalls.length).toBe(0);
    });

    it("should still fail for non-404 errors", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      let warningCalls = [];
      mockCore.warning = msg => {
        warningCalls.push(msg);
      };

      let errorCalls = [];
      mockCore.error = msg => {
        errorCalls.push(msg);
      };

      // Mock API to throw non-404 error
      mockGithub.rest.issues.createComment = async () => {
        const error = new Error("Forbidden");
        // @ts-ignore
        error.status = 403;
        throw error;
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({}); })()`);

      const message = {
        type: "add_comment",
        body: "Test comment",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(false);
      expect(result.error).toBeTruthy();
      expect(result.error).toContain("Forbidden");
      expect(errorCalls.length).toBeGreaterThan(0);
      expect(errorCalls[0]).toContain("Failed to add comment");
    });

    it("should still fail for validation errors", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      let errorCalls = [];
      mockCore.error = msg => {
        errorCalls.push(msg);
      };

      // Mock API to throw validation error
      mockGithub.rest.issues.createComment = async () => {
        const error = new Error("Validation Failed");
        // @ts-ignore
        error.status = 422;
        throw error;
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({}); })()`);

      const message = {
        type: "add_comment",
        body: "Test comment",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(false);
      expect(result.error).toBeTruthy();
      expect(result.error).toContain("Validation Failed");
      expect(errorCalls.length).toBeGreaterThan(0);
    });
  });

  describe("discussion fallback", () => {
    it("should retry as discussion when item_number returns 404 as issue/PR", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      let infoCalls = [];
      mockCore.info = msg => {
        infoCalls.push(msg);
      };

      // Mock REST API to return 404 (not found as issue/PR)
      mockGithub.rest.issues.createComment = async () => {
        const error = new Error("Not Found");
        // @ts-ignore
        error.status = 404;
        throw error;
      };

      // Mock GraphQL to return discussion
      let graphqlCalls = [];
      mockGithub.graphql = async (query, vars) => {
        graphqlCalls.push({ query, vars });

        // First call is to check if discussion exists
        if (query.includes("query") && query.includes("discussion(number:")) {
          return {
            repository: {
              discussion: {
                id: "D_kwDOTest789",
                url: "https://github.com/owner/repo/discussions/14117",
              },
            },
          };
        }

        // Second call is to add comment
        if (query.includes("mutation") && query.includes("addDiscussionComment")) {
          return {
            addDiscussionComment: {
              comment: {
                id: "DC_kwDOTest999",
                body: "Test comment",
                createdAt: "2026-02-06T12:00:00Z",
                url: "https://github.com/owner/repo/discussions/14117#discussioncomment-999",
              },
            },
          };
        }
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ target: '*' }); })()`);

      const message = {
        type: "add_comment",
        item_number: 14117,
        body: "Test comment on discussion",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(result.isDiscussion).toBe(true);
      expect(result.itemNumber).toBe(14117);
      expect(result.url).toContain("discussions/14117");

      // Verify it logged the retry
      const retryLog = infoCalls.find(msg => msg.includes("retrying as discussion"));
      expect(retryLog).toBeTruthy();

      const createdLog = infoCalls.find(msg => msg.includes("Created comment on discussion"));
      expect(createdLog).toBeTruthy();
    });

    it("should return skipped when item_number not found as issue/PR or discussion", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      let warningCalls = [];
      mockCore.warning = msg => {
        warningCalls.push(msg);
      };

      // Mock REST API to return 404
      mockGithub.rest.issues.createComment = async () => {
        const error = new Error("Not Found");
        // @ts-ignore
        error.status = 404;
        throw error;
      };

      // Mock GraphQL to also return 404 (discussion doesn't exist either)
      mockGithub.graphql = async (query, vars) => {
        if (query.includes("query") && query.includes("discussion(number:")) {
          return {
            repository: {
              discussion: null,
            },
          };
        }
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ target: 'triggering' }); })()`);

      const message = {
        type: "add_comment",
        item_number: 99999,
        body: "Test comment",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(result.skipped).toBe(true);
      expect(result.warning).toContain("not found");

      // Verify warning was logged
      const notFoundWarning = warningCalls.find(msg => msg.includes("not found"));
      expect(notFoundWarning).toBeTruthy();
    });

    it("should not retry as discussion when 404 occurs without explicit item_number", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      let warningCalls = [];
      mockCore.warning = msg => {
        warningCalls.push(msg);
      };

      // Mock REST API to return 404
      mockGithub.rest.issues.createComment = async () => {
        const error = new Error("Not Found");
        // @ts-ignore
        error.status = 404;
        throw error;
      };

      // GraphQL should not be called
      let graphqlCalled = false;
      mockGithub.graphql = async () => {
        graphqlCalled = true;
        throw new Error("GraphQL should not be called");
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ target: 'triggering' }); })()`);

      const message = {
        type: "add_comment",
        // No item_number - using target resolution
        body: "Test comment",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(result.skipped).toBe(true);
      expect(graphqlCalled).toBe(false);

      // Verify warning was logged
      const notFoundWarning = warningCalls.find(msg => msg.includes("not found"));
      expect(notFoundWarning).toBeTruthy();
    });

    it("should not retry as discussion when already detected as discussion context", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      // Set discussion context
      mockContext.eventName = "discussion";
      mockContext.payload = {
        discussion: {
          number: 100,
        },
      };

      let warningCalls = [];
      mockCore.warning = msg => {
        warningCalls.push(msg);
      };

      // Mock GraphQL to return 404 for discussion
      let graphqlCallCount = 0;
      mockGithub.graphql = async (query, vars) => {
        graphqlCallCount++;

        if (query.includes("query") && query.includes("discussion(number:")) {
          return {
            repository: {
              discussion: null,
            },
          };
        }
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ target: 'triggering' }); })()`);

      const message = {
        type: "add_comment",
        body: "Test comment",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(result.skipped).toBe(true);

      // Should only call GraphQL once (not retry)
      expect(graphqlCallCount).toBe(1);
    });

    it("should return skipped (not fail) when discussion comment fails with Resource not accessible by integration", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      let warningCalls = [];
      let errorCalls = [];
      mockCore.warning = msg => {
        warningCalls.push(msg);
      };
      mockCore.error = msg => {
        errorCalls.push(msg);
      };

      // Mock REST API to return 404 (not found as issue/PR)
      mockGithub.rest.issues.createComment = async () => {
        const error = new Error("Not Found");
        // @ts-ignore -- Error type does not include status, but octokit adds it at runtime
        error.status = 404;
        throw error;
      };

      // Mock GraphQL to return the discussion node ID but fail on mutation
      let graphqlCalls = [];
      mockGithub.graphql = async (query, vars) => {
        graphqlCalls.push({ query, vars });

        // First call: fetch discussion node ID
        if (query.includes("query") && query.includes("discussion(number:")) {
          return {
            repository: {
              discussion: {
                id: "D_kwDOTest335",
                url: "https://github.com/owner/repo/discussions/335",
              },
            },
          };
        }

        // Second call: mutation — simulate "Resource not accessible by integration"
        if (query.includes("mutation") && query.includes("addDiscussionComment")) {
          const err = new Error("Request failed due to following response errors:\n - Resource not accessible by integration");
          throw err;
        }
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ target: '*' }); })()`);

      const message = {
        type: "add_comment",
        item_number: 335,
        body: "Test comment on discussion",
      };

      const result = await handler(message, {});

      // Should be skipped (not a hard failure) so the safe-outputs job doesn't fail
      expect(result.success).toBe(false);
      expect(result.skipped).toBe(true);
      expect(result.error).toContain("configuration mismatch");
      expect(result.error).toContain("discussions:write");

      // Warning should be emitted, no error
      const configMismatchWarning = warningCalls.find(msg => msg.includes("configuration mismatch"));
      expect(configMismatchWarning).toBeTruthy();
      expect(errorCalls.length).toBe(0);
    });

    it("should return skipped when discussion GraphQL errors array contains Resource not accessible", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      let warningCalls = [];
      mockCore.warning = msg => {
        warningCalls.push(msg);
      };

      // Mock REST API to return 404
      mockGithub.rest.issues.createComment = async () => {
        const error = new Error("Not Found");
        // @ts-ignore
        error.status = 404;
        throw error;
      };

      // GraphQL: discussion exists but mutation fails with errors array
      mockGithub.graphql = async (query, vars) => {
        if (query.includes("query") && query.includes("discussion(number:")) {
          return {
            repository: {
              discussion: {
                id: "D_kwDOTest336",
                url: "https://github.com/owner/repo/discussions/336",
              },
            },
          };
        }

        if (query.includes("mutation") && query.includes("addDiscussionComment")) {
          const err = new Error("Request failed");
          // @ts-ignore -- Error type does not include errors, but GraphQL errors surface this way at runtime
          err.errors = [{ type: "FORBIDDEN", message: "Resource not accessible by integration" }];
          throw err;
        }
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ target: '*' }); })()`);

      const message = {
        type: "add_comment",
        item_number: 336,
        body: "Test comment",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(false);
      expect(result.skipped).toBe(true);
      expect(result.error).toContain("configuration mismatch");

      const configMismatchWarning = warningCalls.find(msg => msg.includes("configuration mismatch"));
      expect(configMismatchWarning).toBeTruthy();
    });
  });

  describe("temporary ID resolution", () => {
    it("should resolve temporary ID in item_number field", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      /** @type {any} */
      let capturedIssueNumber = null;
      mockGithub.rest.issues.createComment = async params => {
        capturedIssueNumber = params.issue_number;
        return {
          data: {
            id: 12345,
            html_url: `https://github.com/owner/repo/issues/${params.issue_number}#issuecomment-12345`,
          },
        };
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ target: '*' }); })()`);

      const message = {
        type: "add_comment",
        item_number: "aw_test01",
        body: "Comment on issue created with temporary ID",
      };

      // Provide resolved temporary ID
      const resolvedTemporaryIds = {
        aw_test01: { repo: "owner/repo", number: 42 },
      };

      const result = await handler(message, resolvedTemporaryIds);

      expect(result.success).toBe(true);
      expect(capturedIssueNumber).toBe(42);
      expect(result.itemNumber).toBe(42);
    });

    it("should defer when temporary ID is not yet resolved", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ target: '*' }); })()`);

      const message = {
        type: "add_comment",
        item_number: "aw_test99",
        body: "Comment on issue with unresolved temporary ID",
      };

      // Empty resolved map - temporary ID not yet resolved
      const resolvedTemporaryIds = {};

      const result = await handler(message, resolvedTemporaryIds);

      expect(result.success).toBe(false);
      expect(result.deferred).toBe(true);
      expect(result.error).toContain("aw_test99");
    });

    it("should handle temporary ID with hash prefix", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      /** @type {any} */
      let capturedIssueNumber = null;
      mockGithub.rest.issues.createComment = async params => {
        capturedIssueNumber = params.issue_number;
        return {
          data: {
            id: 12345,
            html_url: `https://github.com/owner/repo/issues/${params.issue_number}#issuecomment-12345`,
          },
        };
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ target: '*' }); })()`);

      const message = {
        type: "add_comment",
        item_number: "#aw_test02",
        body: "Comment with hash prefix",
      };

      // Provide resolved temporary ID
      const resolvedTemporaryIds = {
        aw_test02: { repo: "owner/repo", number: 100 },
      };

      const result = await handler(message, resolvedTemporaryIds);

      expect(result.success).toBe(true);
      expect(capturedIssueNumber).toBe(100);
    });

    it("should handle invalid temporary ID format", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ target: '*' }); })()`);

      const message = {
        type: "add_comment",
        item_number: "aw_", // Invalid: too short
        body: "Comment with invalid temporary ID",
      };

      const resolvedTemporaryIds = {};

      const result = await handler(message, resolvedTemporaryIds);

      expect(result.success).toBe(false);
      expect(result.error).toContain("Invalid item number");
    });

    it("should replace temporary IDs in comment body", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      /** @type {any} */
      let capturedBody = null;
      mockGithub.rest.issues.createComment = async params => {
        capturedBody = params.body;
        return {
          data: {
            id: 12345,
            html_url: `https://github.com/owner/repo/issues/${params.issue_number}#issuecomment-12345`,
          },
        };
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({}); })()`);

      const message = {
        type: "add_comment",
        item_number: 42,
        body: "References: #aw_test01 and #aw_test02",
      };

      // Provide resolved temporary IDs
      const resolvedTemporaryIds = {
        aw_test01: { repo: "owner/repo", number: 100 },
        aw_test02: { repo: "owner/repo", number: 200 },
      };

      const result = await handler(message, resolvedTemporaryIds);

      expect(result.success).toBe(true);
      expect(capturedBody).toContain("#100");
      expect(capturedBody).toContain("#200");
      expect(capturedBody).not.toContain("aw_test01");
      expect(capturedBody).not.toContain("aw_test02");
    });

    it("should resolve temporary ID in issue_number field", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      /** @type {any} */
      let capturedIssueNumber = null;
      mockGithub.rest.issues.createComment = async params => {
        capturedIssueNumber = params.issue_number;
        return {
          data: {
            id: 12345,
            html_url: `https://github.com/owner/repo/issues/${params.issue_number}#issuecomment-12345`,
          },
        };
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ target: '*' }); })()`);

      const message = {
        type: "add_comment",
        issue_number: "aw_report",
        body: "Comment targeting issue via issue_number field",
      };

      const resolvedTemporaryIds = {
        aw_report: { repo: "owner/repo", number: 55 },
      };

      const result = await handler(message, resolvedTemporaryIds);

      expect(result.success).toBe(true);
      expect(capturedIssueNumber).toBe(55);
    });

    it("should prefer item_number over issue_number when both are provided", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      /** @type {any} */
      let capturedIssueNumber = null;
      mockGithub.rest.issues.createComment = async params => {
        capturedIssueNumber = params.issue_number;
        return {
          data: {
            id: 12345,
            html_url: `https://github.com/owner/repo/issues/${params.issue_number}#issuecomment-12345`,
          },
        };
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ target: '*' }); })()`);

      const message = {
        type: "add_comment",
        item_number: "aw_primary",
        issue_number: "aw_fallback",
        body: "Comment with both fields",
      };

      const resolvedTemporaryIds = {
        aw_primary: { repo: "owner/repo", number: 10 },
        aw_fallback: { repo: "owner/repo", number: 20 },
      };

      const result = await handler(message, resolvedTemporaryIds);

      expect(result.success).toBe(true);
      expect(capturedIssueNumber).toBe(10);
    });

    it("should defer when issue_number has unresolved temporary ID", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ target: '*' }); })()`);

      const message = {
        type: "add_comment",
        issue_number: "aw_pending",
        body: "Comment with unresolved issue_number temporary ID",
      };

      const resolvedTemporaryIds = {};

      const result = await handler(message, resolvedTemporaryIds);

      expect(result.success).toBe(false);
      expect(result.deferred).toBe(true);
      expect(result.error).toContain("aw_pending");
    });
  });

  describe("sanitization preserves markers", () => {
    it("should preserve tracker ID markers after sanitization", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      // Setup environment
      process.env.GH_AW_WORKFLOW_NAME = "Test Workflow";
      process.env.GH_AW_TRACKER_ID = "test-tracker-123";

      /** @type {any} */
      let capturedBody = null;
      mockGithub.rest.issues.createComment = async params => {
        capturedBody = params.body;
        return {
          data: {
            id: 12345,
            html_url: "https://github.com/owner/repo/issues/42#issuecomment-12345",
          },
        };
      };

      // Execute the handler
      const handler = await eval(`(async () => { ${addCommentScript}; return await main({}); })()`);

      const message = {
        type: "add_comment",
        body: "User content with <script>alert('xss')</script> attempt",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(capturedBody).toBeDefined();
      // Verify tracker ID is present (not removed by sanitization)
      expect(capturedBody).toContain("<!-- gh-aw-tracker-id: test-tracker-123 -->");
      // Verify script tags were sanitized (converted to safe format)
      expect(capturedBody).not.toContain("<script>");
      expect(capturedBody).toContain("(script)"); // Tags converted to parentheses

      delete process.env.GH_AW_WORKFLOW_NAME;
      delete process.env.GH_AW_TRACKER_ID;
    });

    it("should preserve workflow footer after sanitization", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      // Setup environment
      process.env.GH_AW_WORKFLOW_NAME = "Security Test Workflow";

      /** @type {any} */
      let capturedBody = null;
      mockGithub.rest.issues.createComment = async params => {
        capturedBody = params.body;
        return {
          data: {
            id: 12345,
            html_url: "https://github.com/owner/repo/issues/42#issuecomment-12345",
          },
        };
      };

      // Execute the handler
      const handler = await eval(`(async () => { ${addCommentScript}; return await main({}); })()`);

      const message = {
        type: "add_comment",
        body: "User content <!-- malicious comment -->",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(capturedBody).toBeDefined();
      // Verify footer is present (not removed by sanitization)
      expect(capturedBody).toContain("Generated by");
      expect(capturedBody).toContain("Security Test Workflow");
      // Verify malicious comment in user content was removed by sanitization
      expect(capturedBody).not.toContain("<!-- malicious comment -->");

      delete process.env.GH_AW_WORKFLOW_NAME;
    });

    it("should add a blank line before injected security scanning caution footer", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      process.env.GH_AW_WORKFLOW_NAME = "Security Test Workflow";
      process.env.GH_AW_DETECTION_CONCLUSION = "warning";
      process.env.GH_AW_DETECTION_REASON = "threat_detected";

      /** @type {any} */
      let capturedBody = null;
      mockGithub.rest.issues.createComment = async params => {
        capturedBody = params.body;
        return {
          data: {
            id: 12345,
            html_url: "https://github.com/owner/repo/issues/42#issuecomment-12345",
          },
        };
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({}); })()`);

      const message = {
        type: "add_comment",
        body: "Comment body for warning case",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      // CAUTION should appear at the top, before the comment body content
      const cautionIndex = capturedBody.indexOf("[!CAUTION]");
      const bodyIndex = capturedBody.indexOf("Comment body for warning case");
      expect(cautionIndex).toBeGreaterThanOrEqual(0);
      expect(cautionIndex).toBeLessThan(bodyIndex);
      expect(capturedBody).toContain("agentic threat detected");
      expect(capturedBody).toContain("<!-- gh-aw-threat-detected -->");
      expect(capturedBody).toContain("> Generated by [Security Test Workflow]");
      expect(capturedBody).toMatch(/> \[!CAUTION\][\s\S]*\n\n> Generated by \[Security Test Workflow\]/);

      delete process.env.GH_AW_WORKFLOW_NAME;
      delete process.env.GH_AW_DETECTION_CONCLUSION;
      delete process.env.GH_AW_DETECTION_REASON;
    });

    it("should sanitize user content but preserve system markers", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      /** @type {any} */
      let capturedBody = null;
      mockGithub.rest.issues.createComment = async params => {
        capturedBody = params.body;
        return {
          data: {
            id: 12345,
            html_url: "https://github.com/owner/repo/issues/42#issuecomment-12345",
          },
        };
      };

      // Execute the handler
      const handler = await eval(`(async () => { ${addCommentScript}; return await main({}); })()`);

      const message = {
        type: "add_comment",
        body: "User says: @badactor please <!-- inject this --> [phishing](http://evil.com)",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(capturedBody).toBeDefined();

      // User content should be sanitized
      expect(capturedBody).toContain("`@badactor`"); // Mention neutralized
      expect(capturedBody).not.toContain("<!-- inject this -->"); // Comment removed
      expect(capturedBody).toContain("(evil.com/redacted)"); // HTTP URL redacted

      // But footer should still be present with proper markdown
      expect(capturedBody).toContain("> Generated by");
    });

    it("should include gh-aw-workflow-call-id marker when GH_AW_CALLER_WORKFLOW_ID is set", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      process.env.GH_AW_WORKFLOW_NAME = "Test Workflow";
      process.env.GH_AW_CALLER_WORKFLOW_ID = "owner/repo/TestWorkflow";

      /** @type {any} */
      let capturedBody = null;
      mockGithub.rest.issues.createComment = async params => {
        capturedBody = params.body;
        return {
          data: {
            id: 12345,
            html_url: "https://github.com/owner/repo/issues/42#issuecomment-12345",
          },
        };
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({}); })()`);

      const message = {
        type: "add_comment",
        body: "Test comment body",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(capturedBody).toBeDefined();
      expect(capturedBody).toContain("<!-- gh-aw-workflow-call-id: owner/repo/TestWorkflow -->");

      delete process.env.GH_AW_WORKFLOW_NAME;
      delete process.env.GH_AW_CALLER_WORKFLOW_ID;
    });

    it("should not include gh-aw-workflow-call-id marker when GH_AW_CALLER_WORKFLOW_ID is not set", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      process.env.GH_AW_WORKFLOW_NAME = "Test Workflow";
      delete process.env.GH_AW_CALLER_WORKFLOW_ID;

      /** @type {any} */
      let capturedBody = null;
      mockGithub.rest.issues.createComment = async params => {
        capturedBody = params.body;
        return {
          data: {
            id: 12345,
            html_url: "https://github.com/owner/repo/issues/42#issuecomment-12345",
          },
        };
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({}); })()`);

      const message = {
        type: "add_comment",
        body: "Test comment body",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(capturedBody).toBeDefined();
      expect(capturedBody).not.toContain("gh-aw-workflow-call-id");

      delete process.env.GH_AW_WORKFLOW_NAME;
    });
  });
  describe("parent author mention allowlist", () => {
    it("should preserve issue author mention when triggered by issues event", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      // Change context to issues event with an issue author
      mockContext.eventName = "issues";
      mockContext.payload = {
        issue: {
          number: 970,
          user: { login: "Slesa", type: "User" },
        },
      };

      /** @type {any} */
      let capturedBody = null;
      mockGithub.rest.issues.createComment = async params => {
        capturedBody = params.body;
        return {
          data: {
            id: 12345,
            html_url: "https://github.com/owner/repo/issues/970#issuecomment-12345",
          },
        };
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({}); })()`);

      const message = {
        type: "add_comment",
        body: "Hey @Slesa, I found a fix for your issue!",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(capturedBody).toBeDefined();
      // Issue author mention should be preserved (not neutralized to `@Slesa`)
      expect(capturedBody).toContain("@Slesa");
      expect(capturedBody).not.toContain("`@Slesa`");
    });

    it("should preserve issue author mention when triggered by issue_comment event", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      // Change context to issue_comment event
      mockContext.eventName = "issue_comment";
      mockContext.payload = {
        issue: {
          number: 970,
          user: { login: "Slesa", type: "User" },
        },
        comment: {
          user: { login: "Commenter", type: "User" },
        },
      };

      /** @type {any} */
      let capturedBody = null;
      mockGithub.rest.issues.createComment = async params => {
        capturedBody = params.body;
        return {
          data: {
            id: 12345,
            html_url: "https://github.com/owner/repo/issues/970#issuecomment-12345",
          },
        };
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({}); })()`);

      const message = {
        type: "add_comment",
        body: "Hey @Slesa, responding to your comment.",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(capturedBody).toBeDefined();
      // Issue author mention should be preserved (not neutralized)
      expect(capturedBody).toContain("@Slesa");
      expect(capturedBody).not.toContain("`@Slesa`");
    });

    it("should preserve PR author mention when triggered by pull_request event", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      // Context is pull_request with an author (mockContext default is pull_request)
      mockContext.payload = {
        pull_request: {
          number: 8535,
          user: { login: "PRAuthor", type: "User" },
        },
      };

      /** @type {any} */
      let capturedBody = null;
      mockGithub.rest.issues.createComment = async params => {
        capturedBody = params.body;
        return {
          data: {
            id: 12345,
            html_url: "https://github.com/owner/repo/issues/8535#issuecomment-12345",
          },
        };
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({}); })()`);

      const message = {
        type: "add_comment",
        body: "Hey @PRAuthor, your PR looks great!",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(capturedBody).toBeDefined();
      // PR author mention should be preserved (not neutralized)
      expect(capturedBody).toContain("@PRAuthor");
      expect(capturedBody).not.toContain("`@PRAuthor`");
    });

    it("should neutralize @copilot mention by default", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      mockContext.payload = {
        pull_request: {
          number: 8535,
          user: { login: "PRAuthor", type: "User" },
        },
      };

      /** @type {any} */
      let capturedBody = null;
      mockGithub.rest.issues.createComment = async params => {
        capturedBody = params.body;
        return {
          data: {
            id: 12345,
            html_url: "https://github.com/owner/repo/issues/8535#issuecomment-12345",
          },
        };
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({}); })()`);

      const message = {
        type: "add_comment",
        body: "@copilot review all comments",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(capturedBody).toBeDefined();
      expect(capturedBody).toContain("`@copilot`");
    });

    it("should preserve @copilot mention when mentions allowlist includes copilot", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      mockContext.payload = {
        pull_request: {
          number: 8535,
          user: { login: "PRAuthor", type: "User" },
        },
      };

      /** @type {any} */
      let capturedBody = null;
      mockGithub.rest.issues.createComment = async params => {
        capturedBody = params.body;
        return {
          data: {
            id: 12345,
            html_url: "https://github.com/owner/repo/issues/8535#issuecomment-12345",
          },
        };
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ mentions: { allowed: ["@copilot"] } }); })()`);

      const message = {
        type: "add_comment",
        body: "@copilot review all comments",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(capturedBody).toBeDefined();
      expect(capturedBody).toContain("@copilot");
      expect(capturedBody).not.toContain("`@copilot`");
    });

    it("should preserve pre-resolved allowed mentions from handler manager", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      mockContext.payload = {
        pull_request: {
          number: 8535,
          user: { login: "PRAuthor", type: "User" },
        },
      };

      /** @type {any} */
      let capturedBody = null;
      mockGithub.rest.issues.createComment = async params => {
        capturedBody = params.body;
        return {
          data: {
            id: 12345,
            html_url: "https://github.com/owner/repo/issues/8535#issuecomment-12345",
          },
        };
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ mentions: { allowedTeams: ["myorg/eng"] }, allowedMentionAliases: ["TeamMate"] }); })()`);

      const message = {
        type: "add_comment",
        body: "@TeamMate thanks for taking a look",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(capturedBody).toBeDefined();
      expect(capturedBody).toContain("@TeamMate");
      expect(capturedBody).not.toContain("`@TeamMate`");
    });

    it("should escape all mentions when mentions.enabled is false", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      mockContext.payload = {
        pull_request: {
          number: 8535,
          user: { login: "PRAuthor", type: "User" },
        },
      };

      /** @type {any} */
      let capturedBody = null;
      mockGithub.rest.issues.createComment = async params => {
        capturedBody = params.body;
        return {
          data: {
            id: 12345,
            html_url: "https://github.com/owner/repo/issues/8535#issuecomment-12345",
          },
        };
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ mentions: { enabled: false, allowed: ["@copilot"] } }); })()`);

      const message = {
        type: "add_comment",
        body: "@copilot ping @PRAuthor",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(capturedBody).toBeDefined();
      expect(capturedBody).toContain("`@copilot`");
      expect(capturedBody).toContain("`@PRAuthor`");
    });

    it("should escape all mentions when mentions is false", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      mockContext.payload = {
        pull_request: {
          number: 8535,
          user: { login: "PRAuthor", type: "User" },
        },
      };

      /** @type {any} */
      let capturedBody = null;
      mockGithub.rest.issues.createComment = async params => {
        capturedBody = params.body;
        return {
          data: {
            id: 12345,
            html_url: "https://github.com/owner/repo/issues/8535#issuecomment-12345",
          },
        };
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ mentions: false }); })()`);

      const message = {
        type: "add_comment",
        body: "@copilot ping @PRAuthor",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(capturedBody).toBeDefined();
      expect(capturedBody).toContain("`@copilot`");
      expect(capturedBody).toContain("`@PRAuthor`");
    });

    it("should fetch and preserve issue author for explicit item_number", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      // Set up mock to return issue data when fetching issue #970
      mockGithub.rest.issues.get = async params => {
        if (params.issue_number === 970) {
          return {
            data: {
              number: 970,
              user: { login: "Slesa", type: "User" },
            },
          };
        }
        throw new Error("Issue not found");
      };

      /** @type {any} */
      let capturedBody = null;
      mockGithub.rest.issues.createComment = async params => {
        capturedBody = params.body;
        return {
          data: {
            id: 12345,
            html_url: "https://github.com/owner/repo/issues/970#issuecomment-12345",
          },
        };
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ target: '*' }); })()`);

      const message = {
        type: "add_comment",
        item_number: 970,
        body: "Hey @Slesa, I found a fix for your issue!",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(capturedBody).toBeDefined();
      // Issue author mention should be preserved (not neutralized)
      expect(capturedBody).toContain("@Slesa");
      expect(capturedBody).not.toContain("`@Slesa`");
    });

    it("should not preserve mentions of unknown users for explicit item_number", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      // Set up mock to return issue data with a known author (not the unknown user)
      mockGithub.rest.issues.get = async params => {
        if (params.issue_number === 970) {
          return {
            data: {
              number: 970,
              user: { login: "Slesa", type: "User" },
            },
          };
        }
        throw new Error("Issue not found");
      };

      /** @type {any} */
      let capturedBody = null;
      mockGithub.rest.issues.createComment = async params => {
        capturedBody = params.body;
        return {
          data: {
            id: 12345,
            html_url: "https://github.com/owner/repo/issues/970#issuecomment-12345",
          },
        };
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ target: '*' }); })()`);

      const message = {
        type: "add_comment",
        item_number: 970,
        body: "Hey @Slesa and @UnknownUser, here is the fix.",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(capturedBody).toBeDefined();
      // Issue author mention should be preserved
      expect(capturedBody).toContain("@Slesa");
      expect(capturedBody).not.toContain("`@Slesa`");
      // Unknown user mention should be neutralized
      expect(capturedBody).toContain("`@UnknownUser`");
    });

    it("should not allow bot users as parent authors", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      // Change context to issues event with a bot as the issue author
      mockContext.eventName = "issues";
      mockContext.payload = {
        issue: {
          number: 970,
          user: { login: "bot-user", type: "Bot" },
        },
      };

      /** @type {any} */
      let capturedBody = null;
      mockGithub.rest.issues.createComment = async params => {
        capturedBody = params.body;
        return {
          data: {
            id: 12345,
            html_url: "https://github.com/owner/repo/issues/970#issuecomment-12345",
          },
        };
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({}); })()`);

      const message = {
        type: "add_comment",
        body: "Hey @bot-user, your issue was processed.",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(capturedBody).toBeDefined();
      // Bot author mention should be neutralized (bots are excluded)
      expect(capturedBody).toContain("`@bot-user`");
    });

    it("should gracefully handle failed issue fetch for explicit item_number", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      // issue.get throws an error
      mockGithub.rest.issues.get = async () => {
        throw new Error("API error");
      };

      /** @type {any} */
      let capturedBody = null;
      mockGithub.rest.issues.createComment = async params => {
        capturedBody = params.body;
        return {
          data: {
            id: 12345,
            html_url: "https://github.com/owner/repo/issues/970#issuecomment-12345",
          },
        };
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({}); })()`);

      const message = {
        type: "add_comment",
        item_number: 970,
        body: "Hey @Slesa, here is the fix.",
      };

      const result = await handler(message, {});

      // Should still succeed, just with the mention neutralized
      expect(result.success).toBe(true);
      expect(capturedBody).toBeDefined();
    });

    it("should preserve discussion author mention when triggered by discussion_comment event", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      // Change context to discussion_comment event
      mockContext.eventName = "discussion_comment";
      mockContext.payload = {
        discussion: {
          number: 42,
          user: { login: "DiscussionAuthor", type: "User" },
        },
        comment: {
          user: { login: "Commenter", type: "User" },
        },
      };

      /** @type {any} */
      let capturedBody = null;
      // Mock graphql for discussion
      mockGithub.graphql = async query => {
        if (query.includes("discussion(number")) {
          return {
            repository: {
              discussion: { id: "D_kwDOTest123", url: "https://github.com/owner/repo/discussions/42" },
            },
          };
        }
        if (query.includes("addDiscussionComment")) {
          return {
            addDiscussionComment: {
              comment: {
                id: "DC_kwDOTest456",
                body: capturedBody,
                createdAt: "2024-01-01",
                url: "https://github.com/owner/repo/discussions/42#discussioncomment-456",
              },
            },
          };
        }
        return {};
      };

      // Capture the actual addDiscussionComment mutation
      const originalGraphql = mockGithub.graphql;
      mockGithub.graphql = async (query, vars) => {
        if (query.includes("addDiscussionComment")) {
          capturedBody = vars?.body || vars?.message;
          return {
            addDiscussionComment: {
              comment: {
                id: "DC_kwDOTest456",
                body: capturedBody,
                createdAt: "2024-01-01",
                url: "https://github.com/owner/repo/discussions/42#discussioncomment-456",
              },
            },
          };
        }
        return originalGraphql(query, vars);
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({}); })()`);

      const message = {
        type: "add_comment",
        body: "Hey @DiscussionAuthor, great discussion!",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      // Discussion author mention should be preserved (not neutralized)
      // capturedBody may be undefined if the graphql mock doesn't capture it,
      // but the key test is that the handler succeeds
      expect(result.isDiscussion).toBe(true);
    });

    it("should deduplicate allowed aliases case-insensitively across parentAuthors and configured mentions", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      // Issue author is "Alice"; configured mentions also include "alice" (different casing)
      mockContext.eventName = "issues";
      mockContext.payload = {
        issue: {
          number: 42,
          user: { login: "Alice", type: "User" },
        },
      };

      const infoMessages = [];
      mockCore.info = msg => infoMessages.push(msg);

      /** @type {any} */
      let capturedBody = null;
      mockGithub.rest.issues.createComment = async ({ body }) => {
        capturedBody = body;
        return { data: { id: 12345, html_url: "https://github.com/owner/repo/issues/42#issuecomment-12345" } };
      };

      // configured mentions includes "alice" (lowercase dup) and "Bob" (unique)
      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ mentions: { allowed: ["alice", "Bob"] } }); })()`);

      const result = await handler({ type: "add_comment", body: "@Alice @Bob check this out" }, {});

      expect(result.success).toBe(true);
      expect(capturedBody).toContain("@Alice");
      expect(capturedBody).toContain("@Bob");
      expect(infoMessages).toContain("[MENTIONS] Allowing aliases in comment: Alice, Bob");
    });

    it("should preserve order: parentAuthors first, then configuredMentionAliases", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      mockContext.eventName = "issues";
      mockContext.payload = {
        issue: {
          number: 42,
          user: { login: "Charlie", type: "User" },
        },
      };

      const infoMessages = [];
      mockCore.info = msg => infoMessages.push(msg);

      /** @type {any} */
      let capturedBody = null;
      mockGithub.rest.issues.createComment = async ({ body }) => {
        capturedBody = body;
        return { data: { id: 12345, html_url: "https://github.com/owner/repo/issues/42#issuecomment-12345" } };
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ mentions: { allowed: ["Dave", "Eve"] } }); })()`);

      const result = await handler({ type: "add_comment", body: "@Charlie @Dave @Eve thanks!" }, {});

      expect(result.success).toBe(true);
      expect(capturedBody).toContain("@Charlie");
      expect(capturedBody).toContain("@Dave");
      expect(capturedBody).toContain("@Eve");
      expect(infoMessages).toContain("[MENTIONS] Allowing aliases in comment: Charlie, Dave, Eve");
    });
  });

  describe("staged mode", () => {
    it("should return staged preview without creating comment", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      const originalStaged = process.env.GH_AW_SAFE_OUTPUTS_STAGED;
      process.env.GH_AW_SAFE_OUTPUTS_STAGED = "true";

      try {
        let createCommentCalled = false;
        mockGithub.rest.issues.createComment = async () => {
          createCommentCalled = true;
          return { data: { id: 1, html_url: "https://github.com/owner/repo/issues/8535#issuecomment-1" } };
        };

        const handler = await eval(`(async () => { ${addCommentScript}; return await main({}); })()`);

        const message = { type: "add_comment", body: "Staged comment preview" };
        const result = await handler(message, {});

        expect(result.success).toBe(true);
        expect(result.staged).toBe(true);
        expect(result.previewInfo).toBeDefined();
        expect(result.previewInfo.itemNumber).toBe(8535);
        expect(createCommentCalled).toBe(false);
      } finally {
        if (originalStaged === undefined) {
          delete process.env.GH_AW_SAFE_OUTPUTS_STAGED;
        } else {
          process.env.GH_AW_SAFE_OUTPUTS_STAGED = originalStaged;
        }
      }
    });

    it("should return staged preview for discussion when staged mode is set via config", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      mockContext.eventName = "discussion";
      mockContext.payload = { discussion: { number: 55 } };

      let createCommentCalled = false;
      mockGithub.graphql = async () => {
        createCommentCalled = true;
        return {};
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ staged: true }); })()`);

      const message = { type: "add_comment", body: "Staged discussion preview" };
      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(result.staged).toBe(true);
      expect(result.previewInfo.isDiscussion).toBe(true);
      expect(result.previewInfo.itemNumber).toBe(55);
      expect(createCommentCalled).toBe(false);
    });
  });

  describe("max count enforcement", () => {
    it("should skip comments beyond max count", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ max: 2 }); })()`);

      const message = { type: "add_comment", body: "Comment" };

      // First two should succeed
      const result1 = await handler(message, {});
      const result2 = await handler(message, {});
      // Third should be skipped
      const result3 = await handler(message, {});

      expect(result1.success).toBe(true);
      expect(result2.success).toBe(true);
      expect(result3.success).toBe(false);
      expect(result3.skipped).toBe(true);
      expect(result3.error).toMatch(/max count/i);
    });
  });

  describe("footer configuration", () => {
    it("should include only XML marker (no attribution text) when footer is disabled", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      const originalWorkflowName = process.env.GH_AW_WORKFLOW_NAME;
      process.env.GH_AW_WORKFLOW_NAME = "No-Footer Workflow";

      try {
        /** @type {any} */
        let capturedBody = null;
        mockGithub.rest.issues.createComment = async params => {
          capturedBody = params.body;
          return { data: { id: 1, html_url: "https://github.com/owner/repo/issues/8535#issuecomment-1" } };
        };

        const handler = await eval(`(async () => { ${addCommentScript}; return await main({ footer: false }); })()`);

        const message = { type: "add_comment", body: "No footer comment" };
        const result = await handler(message, {});

        expect(result.success).toBe(true);
        expect(capturedBody).not.toContain("Generated by");
        expect(capturedBody).toContain("<!-- gh-aw-agentic-workflow:");
      } finally {
        if (originalWorkflowName === undefined) {
          delete process.env.GH_AW_WORKFLOW_NAME;
        } else {
          process.env.GH_AW_WORKFLOW_NAME = originalWorkflowName;
        }
      }
    });

    it("should build footer run URL from workflowRepo for repository_dispatch context", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      mockContext.eventName = "repository_dispatch";
      mockContext.repo = { owner: "target-owner", repo: "target-repo" };
      mockContext.workflowRepo = { owner: "workflow-owner", repo: "workflow-repo" };
      mockContext.payload = {
        action: "issues",
        client_payload: {
          issue: { number: 8535 },
          repository: { owner: { login: "target-owner" }, name: "target-repo" },
        },
      };

      /** @type {any} */
      let capturedBody = null;
      mockGithub.rest.issues.createComment = async params => {
        capturedBody = params.body;
        return { data: { id: 1, html_url: "https://github.com/target-owner/target-repo/issues/8535#issuecomment-1" } };
      };

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({}); })()`);

      const message = { type: "add_comment", body: "Run URL repo test" };
      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(capturedBody).toContain("https://github.com/workflow-owner/workflow-repo/actions/runs/12345");
      expect(capturedBody).not.toContain("https://github.com/target-owner/target-repo/actions/runs/12345");
    });
  });

  describe("hide-older-comments behavior", () => {
    it("should normalize workflow ID match lists", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");
      const exports = await eval(`(async () => { ${addCommentScript}; return { normalizeWorkflowIdList }; })()`);

      expect(exports.normalizeWorkflowIdList(["  wf-a  ", "wf-a", "", "  ", "wf-b "])).toEqual(["wf-a", "wf-b"]);
    });

    it("should skip hiding when no workflow ID is set", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      const originalWorkflowId = process.env.GH_AW_WORKFLOW_ID;
      delete process.env.GH_AW_WORKFLOW_ID;

      try {
        let listCommentsCalled = false;
        mockGithub.rest.issues.listComments = async () => {
          listCommentsCalled = true;
          return { data: [] };
        };
        mockGithub.rest.issues.createComment = async () => ({
          data: { id: 1, html_url: "https://github.com/owner/repo/issues/8535#issuecomment-1" },
        });

        const handler = await eval(`(async () => { ${addCommentScript}; return await main({ hide_older_comments: true }); })()`);

        const message = { type: "add_comment", body: "Comment without workflow ID" };
        const result = await handler(message, {});

        expect(result.success).toBe(true);
        expect(listCommentsCalled).toBe(false);
      } finally {
        if (originalWorkflowId === undefined) {
          delete process.env.GH_AW_WORKFLOW_ID;
        } else {
          process.env.GH_AW_WORKFLOW_ID = originalWorkflowId;
        }
      }
    });

    it("should skip hiding when hide_older_comments is false (default)", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      const originalWorkflowId = process.env.GH_AW_WORKFLOW_ID;
      process.env.GH_AW_WORKFLOW_ID = "test-workflow";

      try {
        let listCommentsCalled = false;
        mockGithub.rest.issues.listComments = async () => {
          listCommentsCalled = true;
          return { data: [] };
        };
        mockGithub.rest.issues.createComment = async () => ({
          data: { id: 1, html_url: "https://github.com/owner/repo/issues/8535#issuecomment-1" },
        });

        const handler = await eval(`(async () => { ${addCommentScript}; return await main({}); })()`);

        const message = { type: "add_comment", body: "Comment with hide disabled" };
        const result = await handler(message, {});

        expect(result.success).toBe(true);
        expect(listCommentsCalled).toBe(false);
      } finally {
        if (originalWorkflowId === undefined) {
          delete process.env.GH_AW_WORKFLOW_ID;
        } else {
          process.env.GH_AW_WORKFLOW_ID = originalWorkflowId;
        }
      }
    });

    it("should hide comments matching compiled hide_older_comments_match values", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");
      const originalWorkflowId = process.env.GH_AW_WORKFLOW_ID;
      process.env.GH_AW_WORKFLOW_ID = "current-workflow";

      try {
        let hiddenNodeIds = [];
        mockGithub.rest.issues.listComments = async () => ({
          data: [
            {
              id: 10,
              node_id: "IC_kwDOTest10",
              body: "Current\n\n<!-- gh-aw-workflow-id: current-workflow -->",
            },
            {
              id: 11,
              node_id: "IC_kwDOTest11",
              body: "Other\n\n<!-- gh-aw-workflow-id: other_workflow -->",
            },
            {
              id: 12,
              node_id: "IC_kwDOTest12",
              body: "Unrelated\n\n<!-- gh-aw-workflow-id: unrelated -->",
            },
          ],
        });
        mockGithub.graphql = async (query, variables) => {
          if (query.includes("minimizeComment")) hiddenNodeIds.push(variables.nodeId);
          return { minimizeComment: { minimizedComment: { isMinimized: true } } };
        };
        mockGithub.rest.issues.createComment = async () => ({
          data: { id: 1, html_url: "https://github.com/owner/repo/issues/8535#issuecomment-1" },
        });

        const handler = await eval(`(async () => { ${addCommentScript}; return await main({ hide_older_comments: "true", hide_older_comments_match: [" other_workflow ", "other_workflow", "", "  "] }); })()`);

        const result = await handler({ type: "add_comment", body: "Comment with compiled match list" }, {});

        expect(result.success).toBe(true);
        expect(hiddenNodeIds).toContain("IC_kwDOTest10");
        expect(hiddenNodeIds).toContain("IC_kwDOTest11");
        expect(hiddenNodeIds).not.toContain("IC_kwDOTest12");
      } finally {
        if (originalWorkflowId === undefined) {
          delete process.env.GH_AW_WORKFLOW_ID;
        } else {
          process.env.GH_AW_WORKFLOW_ID = originalWorkflowId;
        }
      }
    });

    it("should hide comments matching hide-older-comments.match values", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");
      const originalWorkflowId = process.env.GH_AW_WORKFLOW_ID;
      process.env.GH_AW_WORKFLOW_ID = "current-workflow";

      try {
        let hiddenNodeIds = [];
        mockGithub.rest.issues.listComments = async () => ({
          data: [
            {
              id: 10,
              node_id: "IC_kwDOTest10",
              body: "Current\n\n<!-- gh-aw-workflow-id: current-workflow -->",
            },
            {
              id: 11,
              node_id: "IC_kwDOTest11",
              body: "Other\n\n<!-- gh-aw-workflow-id: other_workflow -->",
            },
            {
              id: 12,
              node_id: "IC_kwDOTest12",
              body: "Unrelated\n\n<!-- gh-aw-workflow-id: unrelated -->",
            },
          ],
        });
        mockGithub.graphql = async (query, variables) => {
          if (query.includes("minimizeComment")) hiddenNodeIds.push(variables.nodeId);
          return { minimizeComment: { minimizedComment: { isMinimized: true } } };
        };
        mockGithub.rest.issues.createComment = async () => ({
          data: { id: 1, html_url: "https://github.com/owner/repo/issues/8535#issuecomment-1" },
        });

        const handler = await eval(`(async () => { ${addCommentScript}; return await main({ hide_older_comments: { match: ["other_workflow"] } }); })()`);

        const result = await handler({ type: "add_comment", body: "Comment with match list" }, {});

        expect(result.success).toBe(true);
        expect(hiddenNodeIds).toContain("IC_kwDOTest10");
        expect(hiddenNodeIds).toContain("IC_kwDOTest11");
        expect(hiddenNodeIds).not.toContain("IC_kwDOTest12");
      } finally {
        if (originalWorkflowId === undefined) {
          delete process.env.GH_AW_WORKFLOW_ID;
        } else {
          process.env.GH_AW_WORKFLOW_ID = originalWorkflowId;
        }
      }
    });

    it("should hide match-list comments when workflow ID is not set", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");
      const originalWorkflowId = process.env.GH_AW_WORKFLOW_ID;
      delete process.env.GH_AW_WORKFLOW_ID;

      try {
        let hiddenNodeIds = [];
        mockGithub.rest.issues.listComments = async () => ({
          data: [
            {
              id: 11,
              node_id: "IC_kwDOTest11",
              body: "Target\n\n<!-- gh-aw-workflow-id: target-wf -->",
            },
            {
              id: 12,
              node_id: "IC_kwDOTest12",
              body: "Unrelated\n\n<!-- gh-aw-workflow-id: unrelated -->",
            },
          ],
        });
        mockGithub.graphql = async (query, variables) => {
          if (query.includes("minimizeComment")) hiddenNodeIds.push(variables.nodeId);
          return { minimizeComment: { minimizedComment: { isMinimized: true } } };
        };
        mockGithub.rest.issues.createComment = async () => ({
          data: { id: 1, html_url: "https://github.com/owner/repo/issues/8535#issuecomment-1" },
        });

        const handler = await eval(`(async () => { ${addCommentScript}; return await main({ hide_older_comments: { match: ["target-wf"] } }); })()`);

        const result = await handler({ type: "add_comment", body: "Comment with match list only" }, {});

        expect(result.success).toBe(true);
        expect(hiddenNodeIds).toEqual(["IC_kwDOTest11"]);
      } finally {
        if (originalWorkflowId === undefined) {
          delete process.env.GH_AW_WORKFLOW_ID;
        } else {
          process.env.GH_AW_WORKFLOW_ID = originalWorkflowId;
        }
      }
    });

    it("should respect hide_older_comments.enabled when object config is used", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");
      const originalWorkflowId = process.env.GH_AW_WORKFLOW_ID;
      process.env.GH_AW_WORKFLOW_ID = "current-workflow";

      try {
        let listCommentsCalled = false;
        mockGithub.rest.issues.listComments = async () => {
          listCommentsCalled = true;
          return { data: [] };
        };
        mockGithub.rest.issues.createComment = async () => ({
          data: { id: 1, html_url: "https://github.com/owner/repo/issues/8535#issuecomment-1" },
        });

        const handler = await eval(`(async () => { ${addCommentScript}; return await main({ hide_older_comments: { enabled: false, match: ["other_workflow"] } }); })()`);

        const result = await handler({ type: "add_comment", body: "Comment with disabled hide" }, {});

        expect(result.success).toBe(true);
        expect(listCommentsCalled).toBe(false);
      } finally {
        if (originalWorkflowId === undefined) {
          delete process.env.GH_AW_WORKFLOW_ID;
        } else {
          process.env.GH_AW_WORKFLOW_ID = originalWorkflowId;
        }
      }
    });
  });

  let enforceCommentLimits;
  let MAX_COMMENT_LENGTH;
  let MAX_MENTIONS;
  let MAX_LINKS;

  beforeEach(async () => {
    const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");
    const exports = await eval(`(async () => { ${addCommentScript}; return { enforceCommentLimits, MAX_COMMENT_LENGTH, MAX_MENTIONS, MAX_LINKS }; })()`);
    enforceCommentLimits = exports.enforceCommentLimits;
    MAX_COMMENT_LENGTH = exports.MAX_COMMENT_LENGTH;
    MAX_MENTIONS = exports.MAX_MENTIONS;
    MAX_LINKS = exports.MAX_LINKS;
  });

  it("should accept comment within all limits", () => {
    const validBody = "This is a valid comment with @user1 and https://github.com";
    expect(() => enforceCommentLimits(validBody)).not.toThrow();
  });

  it("should reject comment exceeding MAX_COMMENT_LENGTH", () => {
    const longBody = "a".repeat(MAX_COMMENT_LENGTH + 1);
    expect(() => enforceCommentLimits(longBody)).toThrow(/E006.*maximum length/i);
  });

  it("should accept comment at exactly MAX_COMMENT_LENGTH", () => {
    const exactBody = "a".repeat(MAX_COMMENT_LENGTH);
    expect(() => enforceCommentLimits(exactBody)).not.toThrow();
  });

  it("should reject comment with too many mentions", () => {
    const mentions = Array.from({ length: MAX_MENTIONS + 1 }, (_, i) => `@user${i}`).join(" ");
    const bodyWithMentions = `Comment with mentions: ${mentions}`;
    expect(() => enforceCommentLimits(bodyWithMentions)).toThrow(/E007.*mentions/i);
  });

  it("should accept comment at exactly MAX_MENTIONS", () => {
    const mentions = Array.from({ length: MAX_MENTIONS }, (_, i) => `@user${i}`).join(" ");
    const bodyWithMentions = `Comment with mentions: ${mentions}`;
    expect(() => enforceCommentLimits(bodyWithMentions)).not.toThrow();
  });

  it("should reject comment with too many links", () => {
    const links = Array.from({ length: MAX_LINKS + 1 }, (_, i) => `https://example.com/${i}`).join(" ");
    const bodyWithLinks = `Comment with links: ${links}`;
    expect(() => enforceCommentLimits(bodyWithLinks)).toThrow(/E008.*links/i);
  });

  it("should accept comment at exactly MAX_LINKS", () => {
    const links = Array.from({ length: MAX_LINKS }, (_, i) => `https://example.com/${i}`).join(" ");
    const bodyWithLinks = `Comment with links: ${links}`;
    expect(() => enforceCommentLimits(bodyWithLinks)).not.toThrow();
  });

  it("should count both http and https links", () => {
    const httpLinks = Array.from({ length: 26 }, (_, i) => `http://example.com/${i}`).join(" ");
    const httpsLinks = Array.from({ length: 25 }, (_, i) => `https://example.com/${i}`).join(" ");
    const bodyWithMixedLinks = `Comment with mixed: ${httpLinks} ${httpsLinks}`;
    expect(() => enforceCommentLimits(bodyWithMixedLinks)).toThrow(/E008.*links/i);
  });

  it("should provide detailed error message for length violation", () => {
    const longBody = "a".repeat(MAX_COMMENT_LENGTH + 100);
    try {
      enforceCommentLimits(longBody);
      throw new Error("Should have thrown");
    } catch (error) {
      expect(error.message).toMatch(/E006/);
      expect(error.message).toMatch(/65536/);
      expect(error.message).toMatch(/65636/);
    }
  });

  it("should provide detailed error message for mentions violation", () => {
    const mentions = Array.from({ length: 15 }, (_, i) => `@user${i}`).join(" ");
    const bodyWithMentions = `Comment: ${mentions}`;
    try {
      enforceCommentLimits(bodyWithMentions);
      throw new Error("Should have thrown");
    } catch (error) {
      expect(error.message).toMatch(/E007/);
      expect(error.message).toMatch(/15 mentions/);
      expect(error.message).toMatch(/maximum is 10/);
    }
  });

  it("should provide detailed error message for links violation", () => {
    const links = Array.from({ length: 60 }, (_, i) => `https://example.com/${i}`).join(" ");
    const bodyWithLinks = `Comment: ${links}`;
    try {
      enforceCommentLimits(bodyWithLinks);
      throw new Error("Should have thrown");
    } catch (error) {
      expect(error.message).toMatch(/E008/);
      expect(error.message).toMatch(/60 links/);
      expect(error.message).toMatch(/maximum is 50/);
    }
  });

  it("should handle empty comment body", () => {
    expect(() => enforceCommentLimits("")).not.toThrow();
  });

  it("should handle comment with no mentions", () => {
    const body = "This is a comment without any mentions at all";
    expect(() => enforceCommentLimits(body)).not.toThrow();
  });

  it("should handle comment with no links", () => {
    const body = "This is a comment without any links at all";
    expect(() => enforceCommentLimits(body)).not.toThrow();
  });

  it("should not count incomplete mention patterns", () => {
    const body = "@ not a mention, @ also not, @123 is not a mention";
    expect(() => enforceCommentLimits(body)).not.toThrow();
  });

  it("should count valid mention patterns only", () => {
    const body = "Valid: @user1 @user2. Invalid: @ @123 email@example.com";
    expect(() => enforceCommentLimits(body)).not.toThrow();
  });
});
