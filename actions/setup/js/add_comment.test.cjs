// @ts-check
import { describe, it, expect, beforeEach, afterEach } from "vitest";
import fs from "fs";
import path from "path";
import { fileURLToPath } from "url";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

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
          listComments: async () => ({ data: [] }),
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
  });

  describe("target configuration", () => {
    it("should use triggering PR context when target is 'triggering'", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

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

    it("should fail when target is '*' but no item_number provided", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ target: '*' }); })()`);

      const message = {
        type: "add_comment",
        body: "Test comment without item_number",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(false);
      expect(result.error).toMatch(/no.*item_number/i);
    });

    it("should use explicit item_number even with triggering target", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

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
      expect(capturedIssueNumber).toBe(777);
      expect(result.itemNumber).toBe(777);
    });

    it("should resolve from context when item_number is not provided", async () => {
      const addCommentScript = fs.readFileSync(path.join(__dirname, "add_comment.cjs"), "utf8");

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
        body: "## Smoke Test: Copilot Safe Inputs\n\n✅ Test passed",
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
      expect(warningCalls[0]).toContain("not found");
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

      const handler = await eval(`(async () => { ${addCommentScript}; return await main({ target: 'triggering' }); })()`);

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

      const foundLog = infoCalls.find(msg => msg.includes("Found discussion"));
      expect(foundLog).toBeTruthy();
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
  });
});
