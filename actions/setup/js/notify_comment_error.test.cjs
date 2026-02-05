import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import fs from "fs";
import path from "path";
const mockCore = {
    debug: vi.fn(),
    info: vi.fn(),
    notice: vi.fn(),
    warning: vi.fn(),
    error: vi.fn(),
    setFailed: vi.fn(),
    setOutput: vi.fn(),
    exportVariable: vi.fn(),
    setSecret: vi.fn(),
    setCancelled: vi.fn(),
    setError: vi.fn(),
    getInput: vi.fn(),
    getBooleanInput: vi.fn(),
    getMultilineInput: vi.fn(),
    getState: vi.fn(),
    saveState: vi.fn(),
    startGroup: vi.fn(),
    endGroup: vi.fn(),
    group: vi.fn(),
    addPath: vi.fn(),
    setCommandEcho: vi.fn(),
    isDebug: vi.fn().mockReturnValue(!1),
    getIDToken: vi.fn(),
    toPlatformPath: vi.fn(),
    toPosixPath: vi.fn(),
    toWin32Path: vi.fn(),
    summary: { addRaw: vi.fn().mockReturnThis(), write: vi.fn().mockResolvedValue() },
  },
  mockGithub = {
    request: vi.fn().mockResolvedValue({ data: { id: 123456, html_url: "https://github.com/owner/repo/issues/1#issuecomment-123456" } }),
    graphql: vi.fn().mockResolvedValue({ updateDiscussionComment: { comment: { id: "DC_kwDOABCDEF4ABCDEF", url: "https://github.com/owner/repo/discussions/1#discussioncomment-123456" } } }),
  },
  mockContext = { repo: { owner: "testowner", repo: "testrepo" } };
((global.core = mockCore),
  (global.github = mockGithub),
  (global.context = mockContext),
  describe("notify_comment_error.cjs", () => {
    let notifyCommentScript, originalEnv;
    (beforeEach(() => {
      (vi.clearAllMocks(),
        (mockContext.eventName = void 0),
        (mockContext.payload = void 0),
        (originalEnv = {
          GH_AW_COMMENT_ID: process.env.GH_AW_COMMENT_ID,
          GH_AW_COMMENT_REPO: process.env.GH_AW_COMMENT_REPO,
          GH_AW_RUN_URL: process.env.GH_AW_RUN_URL,
          GH_AW_WORKFLOW_NAME: process.env.GH_AW_WORKFLOW_NAME,
          GH_AW_AGENT_CONCLUSION: process.env.GH_AW_AGENT_CONCLUSION,
          GH_AW_DETECTION_CONCLUSION: process.env.GH_AW_DETECTION_CONCLUSION,
          GH_AW_SAFE_OUTPUT_MESSAGES: process.env.GH_AW_SAFE_OUTPUT_MESSAGES,
          GH_AW_SAFE_OUTPUT_JOBS: process.env.GH_AW_SAFE_OUTPUT_JOBS,
          GH_AW_OUTPUT_CREATE_ISSUE_ISSUE_URL: process.env.GH_AW_OUTPUT_CREATE_ISSUE_ISSUE_URL,
          GH_AW_OUTPUT_ADD_COMMENT_COMMENT_URL: process.env.GH_AW_OUTPUT_ADD_COMMENT_COMMENT_URL,
          GH_AW_OUTPUT_CREATE_PULL_REQUEST_PULL_REQUEST_URL: process.env.GH_AW_OUTPUT_CREATE_PULL_REQUEST_PULL_REQUEST_URL,
        }));
      const scriptPath = path.join(process.cwd(), "notify_comment_error.cjs");
      notifyCommentScript = fs.readFileSync(scriptPath, "utf8");
    }),
      afterEach(() => {
        Object.keys(originalEnv).forEach(key => {
          void 0 !== originalEnv[key] ? (process.env[key] = originalEnv[key]) : delete process.env[key];
        });
      }),
      describe("when comment ID is not provided", () => {
        it("should skip comment update", async () => {
          (delete process.env.GH_AW_COMMENT_ID,
            (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
            (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
            (process.env.GH_AW_AGENT_CONCLUSION = "failure"),
            await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
            expect(mockCore.info).toHaveBeenCalledWith("No comment ID found and no noop messages to process, skipping comment update"),
            expect(mockGithub.request).not.toHaveBeenCalled(),
            expect(mockGithub.graphql).not.toHaveBeenCalled());
        });
      }),
      describe("when append-only comments are enabled", () => {
        it("should create a new issue comment even when GH_AW_COMMENT_ID is not set", async () => {
          (delete process.env.GH_AW_COMMENT_ID,
            (process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({ appendOnlyComments: !0 })),
            (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
            (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
            (process.env.GH_AW_AGENT_CONCLUSION = "success"),
            (mockContext.eventName = "issues"),
            (mockContext.payload = { issue: { number: 1 } }),
            await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
            expect(mockGithub.request).toHaveBeenCalledWith("POST /repos/{owner}/{repo}/issues/{issue_number}/comments", expect.objectContaining({ owner: "testowner", repo: "testrepo", issue_number: 1, body: expect.any(String) })),
            expect(mockCore.info).toHaveBeenCalledWith("Successfully created append-only comment"));
        });

        it("should create a new comment instead of updating an existing comment", async () => {
          ((process.env.GH_AW_COMMENT_ID = "123456"),
            (process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({ appendOnlyComments: !0 })),
            (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
            (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
            (process.env.GH_AW_AGENT_CONCLUSION = "success"),
            (mockContext.eventName = "issues"),
            (mockContext.payload = { issue: { number: 1 } }),
            await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
            expect(mockGithub.request).toHaveBeenCalledWith("POST /repos/{owner}/{repo}/issues/{issue_number}/comments", expect.objectContaining({ owner: "testowner", repo: "testrepo", issue_number: 1, body: expect.any(String) })));

          const endpoints = mockGithub.request.mock.calls.map(call => call[0]);
          expect(endpoints).not.toContain("PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}");
        });
      }),
      describe("when run URL is not provided", () => {
        it("should fail with error", async () => {
          ((process.env.GH_AW_COMMENT_ID = "123456"),
            delete process.env.GH_AW_RUN_URL,
            (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
            (process.env.GH_AW_AGENT_CONCLUSION = "failure"),
            await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
            expect(mockCore.setFailed).toHaveBeenCalledWith("Run URL is required"),
            expect(mockGithub.request).not.toHaveBeenCalled(),
            expect(mockGithub.graphql).not.toHaveBeenCalled());
        });
      }),
      describe("when updating an issue/PR comment", () => {
        (it("should update with success message when agent succeeds", async () => {
          ((process.env.GH_AW_COMMENT_ID = "123456"),
            (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
            (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
            (process.env.GH_AW_AGENT_CONCLUSION = "success"),
            await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
            expect(mockGithub.request).toHaveBeenCalledWith(
              "PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}",
              expect.objectContaining({ owner: "testowner", repo: "testrepo", comment_id: 123456, body: expect.stringContaining("completed successfully!") })
            ),
            expect(mockCore.info).toHaveBeenCalledWith("Successfully updated comment"));
        }),
          it("should update with failure message when agent fails", async () => {
            ((process.env.GH_AW_COMMENT_ID = "123456"),
              (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
              (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
              (process.env.GH_AW_AGENT_CONCLUSION = "failure"),
              await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
              expect(mockGithub.request).toHaveBeenCalledWith(
                "PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}",
                expect.objectContaining({ owner: "testowner", repo: "testrepo", comment_id: 123456, body: expect.stringContaining("failed. Please review the logs") })
              ),
              expect(mockCore.info).toHaveBeenCalledWith("Successfully updated comment"));
          }),
          it("should update with cancelled message when agent is cancelled", async () => {
            ((process.env.GH_AW_COMMENT_ID = "123456"),
              (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
              (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
              (process.env.GH_AW_AGENT_CONCLUSION = "cancelled"),
              await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
              expect(mockGithub.request).toHaveBeenCalledWith("PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}", expect.objectContaining({ body: expect.stringContaining("was cancelled. Please review the logs") })));
          }),
          it("should update with timeout message when agent times out", async () => {
            ((process.env.GH_AW_COMMENT_ID = "123456"),
              (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
              (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
              (process.env.GH_AW_AGENT_CONCLUSION = "timed_out"),
              await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
              expect(mockGithub.request).toHaveBeenCalledWith("PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}", expect.objectContaining({ body: expect.stringContaining("timed out. Please review the logs") })));
          }),
          it("should update with skipped message when agent is skipped", async () => {
            ((process.env.GH_AW_COMMENT_ID = "123456"),
              (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
              (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
              (process.env.GH_AW_AGENT_CONCLUSION = "skipped"),
              await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
              expect(mockGithub.request).toHaveBeenCalledWith("PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}", expect.objectContaining({ body: expect.stringContaining("was skipped. Please review the logs") })));
          }),
          it("should use custom comment repo when provided", async () => {
            ((process.env.GH_AW_COMMENT_ID = "123456"),
              (process.env.GH_AW_COMMENT_REPO = "customowner/customrepo"),
              (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
              (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
              (process.env.GH_AW_AGENT_CONCLUSION = "success"),
              await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
              expect(mockGithub.request).toHaveBeenCalledWith("PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}", expect.objectContaining({ owner: "customowner", repo: "customrepo" })));
          }),
          it("should prioritize detection failure message when detection job fails", async () => {
            ((process.env.GH_AW_COMMENT_ID = "123456"),
              (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
              (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
              (process.env.GH_AW_AGENT_CONCLUSION = "success"),
              (process.env.GH_AW_DETECTION_CONCLUSION = "failure"),
              await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
              expect(mockGithub.request).toHaveBeenCalledWith(
                "PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}",
                expect.objectContaining({ owner: "testowner", repo: "testrepo", comment_id: 123456, body: expect.stringContaining("Security scanning failed") })
              ),
              expect(mockCore.info).toHaveBeenCalledWith("Successfully updated comment"));
          }),
          it("should report detection failure even when agent fails", async () => {
            ((process.env.GH_AW_COMMENT_ID = "123456"),
              (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
              (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
              (process.env.GH_AW_AGENT_CONCLUSION = "failure"),
              (process.env.GH_AW_DETECTION_CONCLUSION = "failure"),
              await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
              expect(mockGithub.request).toHaveBeenCalledWith("PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}", expect.objectContaining({ body: expect.stringContaining("Security scanning failed") })));
          }),
          it("should show agent success when detection succeeds", async () => {
            ((process.env.GH_AW_COMMENT_ID = "123456"),
              (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
              (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
              (process.env.GH_AW_AGENT_CONCLUSION = "success"),
              (process.env.GH_AW_DETECTION_CONCLUSION = "success"),
              await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
              expect(mockGithub.request).toHaveBeenCalledWith("PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}", expect.objectContaining({ body: expect.stringContaining("completed successfully!") })));
          }));
      }),
      describe("when updating a discussion comment", () => {
        (it("should use GraphQL API for discussion comments on success", async () => {
          ((process.env.GH_AW_COMMENT_ID = "DC_kwDOABCDEF4ABCDEF"),
            (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
            (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
            (process.env.GH_AW_AGENT_CONCLUSION = "success"),
            await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
            expect(mockGithub.graphql).toHaveBeenCalledWith(expect.stringContaining("updateDiscussionComment"), expect.objectContaining({ commentId: "DC_kwDOABCDEF4ABCDEF", body: expect.stringContaining("completed successfully!") })),
            expect(mockCore.info).toHaveBeenCalledWith("Successfully updated discussion comment"));
        }),
          it("should use GraphQL API for discussion comments on failure", async () => {
            ((process.env.GH_AW_COMMENT_ID = "DC_kwDOABCDEF4ABCDEF"),
              (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
              (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
              (process.env.GH_AW_AGENT_CONCLUSION = "failure"),
              await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
              expect(mockGithub.graphql).toHaveBeenCalledWith(
                expect.stringContaining("updateDiscussionComment"),
                expect.objectContaining({ commentId: "DC_kwDOABCDEF4ABCDEF", body: expect.stringContaining("failed. Please review the logs") })
              ));
          }));
      }),
      describe("error handling", () => {
        it("should warn but not fail when comment update fails", async () => {
          ((process.env.GH_AW_COMMENT_ID = "123456"),
            (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
            (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
            (process.env.GH_AW_AGENT_CONCLUSION = "success"),
            mockGithub.request.mockRejectedValueOnce(new Error("API error")),
            await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
            expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Failed to update comment")),
            expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("API error")),
            expect(mockCore.setFailed).not.toHaveBeenCalled());
        });
      }),
      describe("generated assets", () => {
        (it("should include generated asset links when safe output jobs produce URLs", async () => {
          ((process.env.GH_AW_COMMENT_ID = "123456"),
            (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
            (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
            (process.env.GH_AW_AGENT_CONCLUSION = "success"),
            (process.env.GH_AW_SAFE_OUTPUT_JOBS = JSON.stringify({ create_issue: "issue_url", add_comment: "comment_url", create_pull_request: "pull_request_url" })),
            (process.env.GH_AW_OUTPUT_CREATE_ISSUE_ISSUE_URL = "https://github.com/owner/repo/issues/42"),
            (process.env.GH_AW_OUTPUT_ADD_COMMENT_COMMENT_URL = "https://github.com/owner/repo/issues/1#issuecomment-123"),
            (process.env.GH_AW_OUTPUT_CREATE_PULL_REQUEST_PULL_REQUEST_URL = "https://github.com/owner/repo/pull/5"),
            await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
            expect(mockGithub.request).toHaveBeenCalledWith("PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}", expect.objectContaining({ owner: "testowner", repo: "testrepo", comment_id: 123456 })));
          const callArgs = mockGithub.request.mock.calls[0][1];
          (expect(callArgs.body).toContain("https://github.com/owner/repo/issues/42"),
            expect(callArgs.body).toContain("https://github.com/owner/repo/issues/1#issuecomment-123"),
            expect(callArgs.body).toContain("https://github.com/owner/repo/pull/5"),
            expect(callArgs.body).not.toContain("### Generated Assets"),
            expect(callArgs.body).not.toContain("Created Issue"),
            expect(callArgs.body).not.toContain("Added Comment"),
            expect(callArgs.body).not.toContain("Created Pull Request"));
        }),
          it("should not include generated assets section when no URLs are present", async () => {
            ((process.env.GH_AW_COMMENT_ID = "123456"),
              (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
              (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
              (process.env.GH_AW_AGENT_CONCLUSION = "success"),
              (process.env.GH_AW_SAFE_OUTPUT_JOBS = JSON.stringify({ create_issue: "issue_url" })),
              await eval(`(async () => { ${notifyCommentScript}; await main(); })()`));
            const callArgs = mockGithub.request.mock.calls[0][1];
            expect(callArgs.body).toMatch(/completed successfully!$/);
          }),
          it("should handle empty safe output jobs gracefully", async () => {
            ((process.env.GH_AW_COMMENT_ID = "123456"),
              (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
              (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
              (process.env.GH_AW_AGENT_CONCLUSION = "success"),
              await eval(`(async () => { ${notifyCommentScript}; await main(); })()`));
            const callArgs = mockGithub.request.mock.calls[0][1];
            expect(callArgs.body).toMatch(/completed successfully!$/);
          }));
      }));
  }));
