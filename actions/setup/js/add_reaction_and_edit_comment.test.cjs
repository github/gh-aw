import { describe, it, expect, beforeEach, vi } from "vitest";
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
  mockGithub = { request: vi.fn(), graphql: vi.fn(), rest: { issues: { createComment: vi.fn() } } },
  mockContext = { eventName: "issues", runId: 12345, repo: { owner: "testowner", repo: "testrepo" }, payload: { issue: { number: 123 }, repository: { html_url: "https://github.com/testowner/testrepo" } } };
((global.core = mockCore),
  (global.github = mockGithub),
  (global.context = mockContext),
  describe("add_reaction_and_edit_comment.cjs", () => {
    let reactionScript;
    (beforeEach(() => {
      (vi.clearAllMocks(),
        delete process.env.GH_AW_REACTION,
        delete process.env.GH_AW_COMMAND,
        delete process.env.GH_AW_WORKFLOW_NAME,
        (global.context.eventName = "issues"),
        (global.context.payload = { issue: { number: 123 }, repository: { html_url: "https://github.com/testowner/testrepo" } }));
      const scriptPath = path.join(process.cwd(), "add_reaction_and_edit_comment.cjs");
      reactionScript = fs.readFileSync(scriptPath, "utf8");
    }),
      describe("Issue reactions", () => {
        (it("should add reaction to issue successfully", async () => {
          ((process.env.GH_AW_REACTION = "eyes"),
            (global.context.eventName = "issues"),
            (global.context.payload.issue = { number: 123 }),
            mockGithub.request.mockResolvedValue({ data: { id: 456 } }),
            await eval(`(async () => { ${reactionScript}; await main(); })()`),
            expect(mockGithub.request).toHaveBeenCalledWith("POST /repos/testowner/testrepo/issues/123/reactions", expect.objectContaining({ content: "eyes" })),
            expect(mockCore.setOutput).toHaveBeenCalledWith("reaction-id", "456"));
        }),
          it("should reject invalid reaction type", async () => {
            ((process.env.GH_AW_REACTION = "invalid"),
              (global.context.eventName = "issues"),
              (global.context.payload.issue = { number: 123 }),
              await eval(`(async () => { ${reactionScript}; await main(); })()`),
              expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Invalid reaction type: invalid")),
              expect(mockGithub.request).not.toHaveBeenCalled());
          }));
      }),
      describe("Pull request reactions", () => {
        it("should add reaction to pull request and create comment", async () => {
          ((process.env.GH_AW_REACTION = "heart"),
            (process.env.GH_AW_WORKFLOW_NAME = "Test Workflow"),
            (global.context.eventName = "pull_request"),
            (global.context.payload = { pull_request: { number: 456 }, repository: { html_url: "https://github.com/testowner/testrepo" } }),
            mockGithub.request.mockResolvedValueOnce({ data: { id: 789 } }).mockResolvedValueOnce({ data: { id: 999, html_url: "https://github.com/testowner/testrepo/pull/456#issuecomment-999" } }),
            await eval(`(async () => { ${reactionScript}; await main(); })()`),
            expect(mockGithub.request).toHaveBeenCalledWith("POST /repos/testowner/testrepo/issues/456/reactions", expect.objectContaining({ content: "heart" })),
            expect(mockGithub.request).toHaveBeenCalledWith("POST /repos/testowner/testrepo/issues/456/comments", expect.objectContaining({ body: expect.stringContaining("has started processing this pull request") })),
            expect(mockCore.setOutput).toHaveBeenCalledWith("reaction-id", "789"),
            expect(mockCore.setOutput).toHaveBeenCalledWith("comment-id", "999"),
            expect(mockCore.setOutput).toHaveBeenCalledWith("comment-url", "https://github.com/testowner/testrepo/pull/456#issuecomment-999"));
        });
      }),
      describe("Discussion reactions", () => {
        (it("should add reaction to discussion using GraphQL", async () => {
          ((process.env.GH_AW_REACTION = "rocket"),
            (global.context.eventName = "discussion"),
            (global.context.payload = { discussion: { number: 10 }, repository: { html_url: "https://github.com/testowner/testrepo" } }),
            mockGithub.graphql
              .mockResolvedValueOnce({ repository: { discussion: { id: "D_kwDOABcD1M4AaBbC", url: "https://github.com/testowner/testrepo/discussions/10" } } })
              .mockResolvedValueOnce({ addReaction: { reaction: { id: "MDg6UmVhY3Rpb24xMjM0NTY3ODk=", content: "ROCKET" } } }),
            await eval(`(async () => { ${reactionScript}; await main(); })()`),
            expect(mockGithub.graphql).toHaveBeenCalledWith(expect.stringContaining("query"), expect.objectContaining({ owner: "testowner", repo: "testrepo", num: 10 })),
            expect(mockGithub.graphql).toHaveBeenCalledWith(expect.stringContaining("mutation"), expect.objectContaining({ subjectId: "D_kwDOABcD1M4AaBbC", content: "ROCKET" })),
            expect(mockCore.setOutput).toHaveBeenCalledWith("reaction-id", "MDg6UmVhY3Rpb24xMjM0NTY3ODk="));
        }),
          it("should map reaction types correctly for GraphQL", async () => {
            const reactionTests = [
              { input: "+1", expected: "THUMBS_UP" },
              { input: "-1", expected: "THUMBS_DOWN" },
              { input: "laugh", expected: "LAUGH" },
              { input: "confused", expected: "CONFUSED" },
              { input: "heart", expected: "HEART" },
              { input: "hooray", expected: "HOORAY" },
              { input: "rocket", expected: "ROCKET" },
              { input: "eyes", expected: "EYES" },
            ];
            for (const test of reactionTests)
              (vi.clearAllMocks(),
                (process.env.GH_AW_REACTION = test.input),
                (global.context.eventName = "discussion"),
                (global.context.payload = { discussion: { number: 10 }, repository: { html_url: "https://github.com/testowner/testrepo" } }),
                mockGithub.graphql
                  .mockResolvedValueOnce({ repository: { discussion: { id: "D_kwDOABcD1M4AaBbC", url: "https://github.com/testowner/testrepo/discussions/10" } } })
                  .mockResolvedValueOnce({ addReaction: { reaction: { id: "MDg6UmVhY3Rpb24xMjM0NTY3ODk=", content: test.expected } } }),
                await eval(`(async () => { ${reactionScript}; await main(); })()`),
                expect(mockGithub.graphql).toHaveBeenCalledWith(expect.stringContaining("mutation"), expect.objectContaining({ content: test.expected })));
          }));
      }),
      describe("Discussion comment reactions", () => {
        (it("should add reaction to discussion comment using GraphQL", async () => {
          ((process.env.GH_AW_REACTION = "heart"),
            (global.context.eventName = "discussion_comment"),
            (global.context.payload = {
              discussion: { number: 10 },
              comment: { id: 123, node_id: "DC_kwDOABcD1M4AaBbC", html_url: "https://github.com/testowner/testrepo/discussions/10#discussioncomment-123" },
              repository: { html_url: "https://github.com/testowner/testrepo" },
            }),
            mockGithub.graphql.mockResolvedValueOnce({ addReaction: { reaction: { id: "MDg6UmVhY3Rpb24xMjM0NTY3ODk=", content: "HEART" } } }),
            await eval(`(async () => { ${reactionScript}; await main(); })()`),
            expect(mockGithub.graphql).toHaveBeenCalledWith(expect.stringContaining("mutation"), expect.objectContaining({ subjectId: "DC_kwDOABcD1M4AaBbC", content: "HEART" })),
            expect(mockCore.setOutput).toHaveBeenCalledWith("reaction-id", "MDg6UmVhY3Rpb24xMjM0NTY3ODk="));
        }),
          it("should fail when discussion comment node_id is missing", async () => {
            ((process.env.GH_AW_REACTION = "eyes"),
              (global.context.eventName = "discussion_comment"),
              (global.context.payload = { discussion: { number: 10 }, comment: { id: 123 }, repository: { html_url: "https://github.com/testowner/testrepo" } }),
              await eval(`(async () => { ${reactionScript}; await main(); })()`),
              expect(mockCore.setFailed).toHaveBeenCalledWith("Discussion comment node ID not found in event payload"),
              expect(mockGithub.graphql).not.toHaveBeenCalled());
          }));
      }),
      describe("Comment creation (always creates new comments)", () => {
        (it("should create comment for issue event", async () => {
          ((process.env.GH_AW_REACTION = "eyes"),
            (process.env.GH_AW_WORKFLOW_NAME = "Test Workflow"),
            (global.context.eventName = "issues"),
            (global.context.payload = { issue: { number: 123 }, repository: { html_url: "https://github.com/testowner/testrepo" } }),
            mockGithub.request.mockResolvedValueOnce({ data: { id: 456 } }).mockResolvedValueOnce({ data: { id: 789, html_url: "https://github.com/testowner/testrepo/issues/123#issuecomment-789" } }),
            await eval(`(async () => { ${reactionScript}; await main(); })()`),
            expect(mockGithub.request).toHaveBeenCalledWith("POST /repos/testowner/testrepo/issues/123/reactions", expect.objectContaining({ content: "eyes" })),
            expect(mockGithub.request).toHaveBeenCalledWith("POST /repos/testowner/testrepo/issues/123/comments", expect.objectContaining({ body: expect.stringContaining("has started processing this issue") })),
            expect(mockCore.setOutput).toHaveBeenCalledWith("reaction-id", "456"),
            expect(mockCore.setOutput).toHaveBeenCalledWith("comment-id", "789"),
            expect(mockCore.setOutput).toHaveBeenCalledWith("comment-url", "https://github.com/testowner/testrepo/issues/123#issuecomment-789"));
        }),
          it("should create new comment for issue_comment event (not edit)", async () => {
            ((process.env.GH_AW_REACTION = "eyes"),
              (process.env.GH_AW_WORKFLOW_NAME = "Test Workflow"),
              (global.context.eventName = "issue_comment"),
              (global.context.payload = { issue: { number: 123 }, comment: { id: 456 }, repository: { html_url: "https://github.com/testowner/testrepo" } }),
              mockGithub.request.mockResolvedValueOnce({ data: { id: 111 } }).mockResolvedValueOnce({ data: { id: 789, html_url: "https://github.com/testowner/testrepo/issues/123#issuecomment-789" } }),
              await eval(`(async () => { ${reactionScript}; await main(); })()`),
              expect(mockGithub.request).toHaveBeenCalledWith("POST /repos/testowner/testrepo/issues/123/comments", expect.objectContaining({ body: expect.stringContaining("has started processing this issue comment") })),
              expect(mockGithub.request).not.toHaveBeenCalledWith("POST /repos/testowner/testrepo/issues/comments/456", expect.anything()),
              expect(mockCore.setOutput).toHaveBeenCalledWith("comment-id", "789"),
              expect(mockCore.setOutput).toHaveBeenCalledWith("comment-url", "https://github.com/testowner/testrepo/issues/123#issuecomment-789"),
              expect(mockCore.setOutput).toHaveBeenCalledWith("comment-repo", "testowner/testrepo"));
          }),
          it("should create new comment for pull_request_review_comment event (not edit)", async () => {
            ((process.env.GH_AW_REACTION = "rocket"),
              (process.env.GH_AW_WORKFLOW_NAME = "PR Review Bot"),
              (global.context.eventName = "pull_request_review_comment"),
              (global.context.payload = { pull_request: { number: 456 }, comment: { id: 789 }, repository: { html_url: "https://github.com/testowner/testrepo" } }),
              mockGithub.request.mockResolvedValueOnce({ data: { id: 222 } }).mockResolvedValueOnce({ data: { id: 999, html_url: "https://github.com/testowner/testrepo/pull/456#discussion_r999" } }),
              await eval(`(async () => { ${reactionScript}; await main(); })()`),
              expect(mockGithub.request).toHaveBeenCalledWith("POST /repos/testowner/testrepo/issues/456/comments", expect.objectContaining({ body: expect.stringContaining("has started processing this pull request review comment") })),
              expect(mockGithub.request).not.toHaveBeenCalledWith("POST /repos/testowner/testrepo/pulls/comments/789", expect.anything()),
              expect(mockCore.setOutput).toHaveBeenCalledWith("comment-id", "999"),
              expect(mockCore.setOutput).toHaveBeenCalledWith("comment-url", "https://github.com/testowner/testrepo/pull/456#discussion_r999"),
              expect(mockCore.setOutput).toHaveBeenCalledWith("comment-repo", "testowner/testrepo"));
          }),
          it("should create comment on discussion", async () => {
            ((process.env.GH_AW_REACTION = "eyes"),
              (process.env.GH_AW_WORKFLOW_NAME = "Test Workflow"),
              (global.context.eventName = "discussion"),
              (global.context.payload = { discussion: { number: 10 }, repository: { html_url: "https://github.com/testowner/testrepo" } }),
              mockGithub.graphql
                .mockResolvedValueOnce({ repository: { discussion: { id: "D_kwDOABcD1M4AaBbC", url: "https://github.com/testowner/testrepo/discussions/10" } } })
                .mockResolvedValueOnce({ addReaction: { reaction: { id: "MDg6UmVhY3Rpb24xMjM0NTY3ODk=", content: "EYES" } } })
                .mockResolvedValueOnce({ repository: { discussion: { id: "D_kwDOABcD1M4AaBbC" } } })
                .mockResolvedValueOnce({ addDiscussionComment: { comment: { id: "DC_kwDOABcD1M4AaBbE", url: "https://github.com/testowner/testrepo/discussions/10#discussioncomment-999" } } }),
              await eval(`(async () => { ${reactionScript}; await main(); })()`),
              expect(mockGithub.graphql).toHaveBeenCalledTimes(4),
              expect(mockGithub.graphql).toHaveBeenCalledWith(expect.stringContaining("addDiscussionComment"), expect.objectContaining({ dId: "D_kwDOABcD1M4AaBbC", body: expect.stringContaining("has started processing this discussion") })),
              expect(mockCore.setOutput).toHaveBeenCalledWith("reaction-id", "MDg6UmVhY3Rpb24xMjM0NTY3ODk="),
              expect(mockCore.setOutput).toHaveBeenCalledWith("comment-id", "DC_kwDOABcD1M4AaBbE"),
              expect(mockCore.setOutput).toHaveBeenCalledWith("comment-url", "https://github.com/testowner/testrepo/discussions/10#discussioncomment-999"));
          }),
          it("should create new comment for discussion_comment events", async () => {
            ((process.env.GH_AW_REACTION = "eyes"),
              (process.env.GH_AW_WORKFLOW_NAME = "Discussion Bot"),
              (global.context.eventName = "discussion_comment"),
              (global.context.payload = { discussion: { number: 10 }, comment: { id: 123, node_id: "DC_kwDOABcD1M4AaBbC" }, repository: { html_url: "https://github.com/testowner/testrepo" } }),
              mockGithub.graphql
                .mockResolvedValueOnce({ addReaction: { reaction: { id: "MDg6UmVhY3Rpb24xMjM0NTY3ODk=", content: "EYES" } } })
                .mockResolvedValueOnce({ repository: { discussion: { id: "D_kwDOABcD1M4AaBbC" } } })
                .mockResolvedValueOnce({ addDiscussionComment: { comment: { id: "DC_kwDOABcD1M4AaBbE", url: "https://github.com/testowner/testrepo/discussions/10#discussioncomment-789" } } }),
              await eval(`(async () => { ${reactionScript}; await main(); })()`),
              expect(mockGithub.graphql).toHaveBeenCalledWith(
                expect.stringContaining("addDiscussionComment"),
                expect.objectContaining({ dId: "D_kwDOABcD1M4AaBbC", body: expect.stringContaining("has started processing this discussion comment"), replyToId: "DC_kwDOABcD1M4AaBbC" })
              ),
              expect(mockCore.setOutput).toHaveBeenCalledWith("comment-id", "DC_kwDOABcD1M4AaBbE"),
              expect(mockCore.setOutput).toHaveBeenCalledWith("comment-url", "https://github.com/testowner/testrepo/discussions/10#discussioncomment-789"),
              expect(mockCore.setOutput).toHaveBeenCalledWith("comment-repo", "testowner/testrepo"));
          }));
      }),
      describe("Error handling", () => {
        (it("should handle missing discussion number", async () => {
          ((process.env.GH_AW_REACTION = "eyes"),
            (global.context.eventName = "discussion"),
            (global.context.payload = { repository: { html_url: "https://github.com/testowner/testrepo" } }),
            await eval(`(async () => { ${reactionScript}; await main(); })()`),
            expect(mockCore.setFailed).toHaveBeenCalledWith("Discussion number not found in event payload"));
        }),
          it("should handle missing discussion or comment info for discussion_comment", async () => {
            ((process.env.GH_AW_REACTION = "eyes"),
              (global.context.eventName = "discussion_comment"),
              (global.context.payload = { discussion: { number: 10 }, repository: { html_url: "https://github.com/testowner/testrepo" } }),
              await eval(`(async () => { ${reactionScript}; await main(); })()`),
              expect(mockCore.setFailed).toHaveBeenCalledWith("Discussion or comment information not found in event payload"));
          }),
          it("should handle unsupported event types", async () => {
            ((process.env.GH_AW_REACTION = "eyes"),
              (global.context.eventName = "push"),
              (global.context.payload = { repository: { html_url: "https://github.com/testowner/testrepo" } }),
              await eval(`(async () => { ${reactionScript}; await main(); })()`),
              expect(mockCore.setFailed).toHaveBeenCalledWith("Unsupported event type: push"));
          }));
      }));
  }));
