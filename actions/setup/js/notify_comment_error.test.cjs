import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import fs from "fs";
import path from "path";
import { syncRuntimePromptTemplates } from "./test_prompt_templates.js";

const { runtimePromptsDir } = syncRuntimePromptTemplates(import.meta.url);
const { ERR_VALIDATION } = require("./error_codes.cjs");
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
          GITHUB_RUN_ID: process.env.GITHUB_RUN_ID,
          GH_AW_COMMENT_ID: process.env.GH_AW_COMMENT_ID,
          GH_AW_COMMENT_REPO: process.env.GH_AW_COMMENT_REPO,
          GH_AW_PROMPTS_DIR: process.env.GH_AW_PROMPTS_DIR,
          GH_AW_RUN_URL: process.env.GH_AW_RUN_URL,
          GH_AW_WORKFLOW_NAME: process.env.GH_AW_WORKFLOW_NAME,
          GH_AW_AGENT_CONCLUSION: process.env.GH_AW_AGENT_CONCLUSION,
          GH_AW_DETECTION_CONCLUSION: process.env.GH_AW_DETECTION_CONCLUSION,
          GH_AW_ASSIGNMENT_ERROR_COUNT: process.env.GH_AW_ASSIGNMENT_ERROR_COUNT,
          GH_AW_SAFE_OUTPUT_MESSAGES: process.env.GH_AW_SAFE_OUTPUT_MESSAGES,
          GH_AW_SAFE_OUTPUT_JOBS: process.env.GH_AW_SAFE_OUTPUT_JOBS,
          GH_AW_SAFE_OUTPUTS_RESULT: process.env.GH_AW_SAFE_OUTPUTS_RESULT,
          GH_AW_AGENT_OUTPUT: process.env.GH_AW_AGENT_OUTPUT,
          GH_AW_NOOP_COMMENT_BODY: process.env.GH_AW_NOOP_COMMENT_BODY,
          GH_AW_ACTIVATION_COMMENT_LINKS: process.env.GH_AW_ACTIVATION_COMMENT_LINKS,
          GH_AW_NOOP_REPORT_AS_ISSUE: process.env.GH_AW_NOOP_REPORT_AS_ISSUE,
          GH_AW_OUTPUT_CREATE_ISSUE_ISSUE_URL: process.env.GH_AW_OUTPUT_CREATE_ISSUE_ISSUE_URL,
          GH_AW_OUTPUT_ADD_COMMENT_COMMENT_URL: process.env.GH_AW_OUTPUT_ADD_COMMENT_COMMENT_URL,
          GH_AW_OUTPUT_CREATE_PULL_REQUEST_PULL_REQUEST_URL: process.env.GH_AW_OUTPUT_CREATE_PULL_REQUEST_PULL_REQUEST_URL,
        }),
        (process.env.GH_AW_PROMPTS_DIR = runtimePromptsDir));
      const scriptPath = path.join(process.cwd(), "notify_comment_error.cjs");
      notifyCommentScript = fs.readFileSync(scriptPath, "utf8");
    }),
      afterEach(() => {
        Object.keys(originalEnv).forEach(key => {
          void 0 !== originalEnv[key] ? (process.env[key] = originalEnv[key]) : delete process.env[key];
        });
      }),
      describe("when comment ID is not provided", () => {
        it("should write a regular linked conclusion summary for noop messages", async () => {
          const tempDir = fs.mkdtempSync(path.join(process.cwd(), "gh-aw-conclusion-summary-"));
          const outputPath = path.join(tempDir, "agent-output.json");
          fs.writeFileSync(outputPath, JSON.stringify({ items: [{ type: "noop", message: "No changes were needed." }] }));
          try {
            delete process.env.GH_AW_COMMENT_ID;
            process.env.GH_AW_AGENT_OUTPUT = outputPath;
            process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123";
            process.env.GH_AW_WORKFLOW_NAME = "test-workflow";
            process.env.GH_AW_AGENT_CONCLUSION = "success";

            await eval(`(async () => { ${notifyCommentScript}; await main(); })()`);

            const summary = mockCore.summary.addRaw.mock.calls[0][0];
            expect(summary).toContain("<summary>✅ Conclusion Summary (1 no-op message)</summary>");
            expect(summary).toContain("**Target:** [Workflow run](https://github.com/owner/repo/actions/runs/123)");
          } finally {
            fs.rmSync(tempDir, { recursive: true, force: true });
          }
        });

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
      describe("categorized no-op completion comments", () => {
        it.each([
          { conclusion: "success", incomplete: false, category: "reported_no_action" },
          { conclusion: "failure", incomplete: false, category: "incomplete" },
          { conclusion: "cancelled", incomplete: false, category: "unconfirmed" },
          { conclusion: "success", incomplete: true, category: "incomplete" },
        ])("publishes the no-op step body without agent prose: %j", async ({ conclusion, incomplete, category }) => {
          const outputPath = path.join(process.cwd(), "noop-completion-output.json");
          const privateMessage = "private repository contents, credentials, prompt and tool payload";
          const items = [{ type: "noop", message: privateMessage }];
          if (incomplete) items.push({ type: "report_incomplete", reason: "private incomplete reason" });
          fs.writeFileSync(outputPath, JSON.stringify({ items }));
          try {
            process.env.GH_AW_AGENT_OUTPUT = outputPath;
            process.env.GH_AW_COMMENT_ID = "123456";
            process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123";
            process.env.GH_AW_WORKFLOW_NAME = "test-workflow";
            process.env.GH_AW_AGENT_CONCLUSION = conclusion;
            process.env.GH_AW_NOOP_REPORT_AS_ISSUE = "false";
            delete process.env.GH_AW_SAFE_OUTPUT_MESSAGES;
            delete process.env.GH_AW_DETECTION_CONCLUSION;
            const { main } = require("./handle_noop_message.cjs");
            await main();
            process.env.GH_AW_NOOP_COMMENT_BODY = mockCore.setOutput.mock.calls.find(call => call[0] === "noop_comment_body")[1];

            await eval(`(async () => { ${notifyCommentScript}; await main(); })()`);

            const body = mockGithub.request.mock.calls.find(call => call[0] === "PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}")[1].body;
            expect(body).toContain(process.env.GH_AW_NOOP_COMMENT_BODY);
            expect(body).toContain(`**Outcome:** \`${category}\``);
            expect(body).not.toContain(privateMessage);
            expect(body).not.toContain("private incomplete reason");
          } finally {
            fs.rmSync(outputPath, { force: true });
          }
        });

        it("does not fall back to publishing raw noop messages when no categorized body is available", async () => {
          const outputPath = path.join(process.cwd(), "noop-completion-output.json");
          fs.writeFileSync(outputPath, JSON.stringify({ items: [{ type: "noop", message: "private agent prose" }] }));
          try {
            process.env.GH_AW_AGENT_OUTPUT = outputPath;
            process.env.GH_AW_COMMENT_ID = "123456";
            process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123";
            process.env.GH_AW_AGENT_CONCLUSION = "success";
            delete process.env.GH_AW_NOOP_COMMENT_BODY;
            delete process.env.GH_AW_SAFE_OUTPUT_MESSAGES;
            await eval(`(async () => { ${notifyCommentScript}; await main(); })()`);
            expect(mockGithub.request.mock.calls[0][1].body).not.toContain("private agent prose");
            expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("omitting agent messages"));
          } finally {
            fs.rmSync(outputPath, { force: true });
          }
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
            expect(mockCore.setFailed).toHaveBeenCalledWith(`${ERR_VALIDATION}: Run URL is required`),
            expect(mockGithub.request).not.toHaveBeenCalled(),
            expect(mockGithub.graphql).not.toHaveBeenCalled());
        });
      }),
      describe("when updating an issue/PR comment", () => {
        it.each([undefined, "40497447793"])("closes imported activation-link fences before the attribution footer with run ID %s", async runId => {
          if (runId === undefined) delete process.env.GITHUB_RUN_ID;
          else process.env.GITHUB_RUN_ID = runId;
          process.env.GH_AW_COMMENT_ID = "123456";
          process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123";
          process.env.GH_AW_WORKFLOW_NAME = "test-workflow";
          process.env.GH_AW_AGENT_CONCLUSION = "success";
          process.env.GH_AW_ACTIVATION_COMMENT_LINKS = "```markdown\nkept link text";
          await eval(`(async () => { ${notifyCommentScript}; await main(); })()`);

          const patchCall = mockGithub.request.mock.calls.find(call => call[0] === "PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}");
          expect(patchCall[1].body).toMatch(/```markdown\nkept link text\n```\n\n> Generated by/);
          const runIdField = runId === undefined ? "" : `, id: ${runId}`;
          expect(patchCall[1].body).toContain(`<!-- gh-aw-agentic-workflow: test-workflow${runIdField}, run: https://github.com/owner/repo/actions/runs/123 -->`);
        });

        (it("should preserve pull request links appended to the activation comment by safe outputs", async () => {
          ((process.env.GH_AW_COMMENT_ID = "123456"),
            (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
            (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
            (process.env.GH_AW_AGENT_CONCLUSION = "success"),
            (process.env.GH_AW_ACTIVATION_COMMENT_LINKS = "Pull request created: [#900](https://github.com/owner/repo/pull/900)"),
            await eval(`(async () => { ${notifyCommentScript}; await main(); })()`));
          const patchCall = mockGithub.request.mock.calls.find(call => call[0] === "PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}");
          expect(patchCall).toBeDefined();
          expect(patchCall[1].body).toContain("completed successfully\\!");
          expect(patchCall[1].body).toContain("Pull request created: [#900](https://github.com/owner/repo/pull/900)");
        }),
          it("should update with success message when agent succeeds", async () => {
            ((process.env.GH_AW_COMMENT_ID = "123456"),
              (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
              (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
              (process.env.GH_AW_AGENT_CONCLUSION = "success"),
              await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
              expect(mockGithub.request).toHaveBeenCalledWith(
                "PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}",
                expect.objectContaining({ owner: "testowner", repo: "testrepo", comment_id: 123456, body: expect.stringContaining("completed successfully\\!") })
              ),
              expect(mockCore.info).toHaveBeenCalledWith("Successfully updated comment"));
          }),
          it("should update with assignment failure message when agent succeeds but assign-to-agent fails", async () => {
            ((process.env.GH_AW_COMMENT_ID = "123456"),
              (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
              (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
              (process.env.GH_AW_AGENT_CONCLUSION = "success"),
              (process.env.GH_AW_ASSIGNMENT_ERROR_COUNT = "3"),
              await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
              expect(mockGithub.request).toHaveBeenCalledWith(
                "PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}",
                expect.objectContaining({
                  owner: "testowner",
                  repo: "testrepo",
                  comment_id: 123456,
                  body: expect.stringContaining("failed to assign the coding agent"),
                })
              ),
              expect(mockCore.info).toHaveBeenCalledWith("Successfully updated comment"),
              expect(mockGithub.request).not.toHaveBeenCalledWith("PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}", expect.objectContaining({ body: expect.stringContaining("completed successfully\\!") })));
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
              expect(mockGithub.request).toHaveBeenCalledWith("PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}", expect.objectContaining({ body: expect.stringContaining("completed successfully\\!") })));
          }),
          it("should show failure message with detection warning when agent fails", async () => {
            ((process.env.GH_AW_COMMENT_ID = "123456"),
              (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
              (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
              (process.env.GH_AW_AGENT_CONCLUSION = "failure"),
              (process.env.GH_AW_DETECTION_CONCLUSION = "warning"),
              await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
              expect(mockGithub.request).toHaveBeenCalledWith("PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}", expect.objectContaining({ body: expect.stringContaining("failed. Please review the logs") })));
          }),
          it("should show cancelled message with detection warning when agent is cancelled", async () => {
            ((process.env.GH_AW_COMMENT_ID = "123456"),
              (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
              (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
              (process.env.GH_AW_AGENT_CONCLUSION = "cancelled"),
              (process.env.GH_AW_DETECTION_CONCLUSION = "warning"),
              await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
              expect(mockGithub.request).toHaveBeenCalledWith("PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}", expect.objectContaining({ body: expect.stringContaining("was cancelled. Please review the logs") })));
          }),
          it("should show timed out message with detection warning when agent times out", async () => {
            ((process.env.GH_AW_COMMENT_ID = "123456"),
              (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
              (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
              (process.env.GH_AW_AGENT_CONCLUSION = "timed_out"),
              (process.env.GH_AW_DETECTION_CONCLUSION = "warning"),
              await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
              expect(mockGithub.request).toHaveBeenCalledWith("PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}", expect.objectContaining({ body: expect.stringContaining("timed out. Please review the logs") })));
          }),
          it("should show assignment failure message with detection warning when agent succeeds but assign-to-agent fails", async () => {
            ((process.env.GH_AW_COMMENT_ID = "123456"),
              (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
              (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
              (process.env.GH_AW_AGENT_CONCLUSION = "success"),
              (process.env.GH_AW_ASSIGNMENT_ERROR_COUNT = "2"),
              (process.env.GH_AW_DETECTION_CONCLUSION = "warning"),
              await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
              expect(mockGithub.request).toHaveBeenCalledWith("PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}", expect.objectContaining({ body: expect.stringContaining("failed to assign the coding agent") })));
          }));
      }),
      describe("when updating a discussion comment", () => {
        (it("should use GraphQL API for discussion comments on success", async () => {
          ((process.env.GH_AW_COMMENT_ID = "DC_kwDOABCDEF4ABCDEF"),
            (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
            (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
            (process.env.GH_AW_AGENT_CONCLUSION = "success"),
            await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
            expect(mockGithub.graphql).toHaveBeenCalledWith(expect.stringContaining("updateDiscussionComment"), expect.objectContaining({ commentId: "DC_kwDOABCDEF4ABCDEF", body: expect.stringContaining("completed successfully\\!") })),
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
            expect(callArgs.body).toContain("completed successfully\\!");
          }),
          it("should handle empty safe output jobs gracefully", async () => {
            ((process.env.GH_AW_COMMENT_ID = "123456"),
              (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
              (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
              (process.env.GH_AW_AGENT_CONCLUSION = "success"),
              await eval(`(async () => { ${notifyCommentScript}; await main(); })()`));
            const callArgs = mockGithub.request.mock.calls[0][1];
            expect(callArgs.body).toContain("completed successfully\\!");
          }));
      }),
      describe("when safe_outputs job fails", () => {
        (it("should show failure message when agent succeeds but safe_outputs fails", async () => {
          ((process.env.GH_AW_COMMENT_ID = "123456"),
            (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
            (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
            (process.env.GH_AW_AGENT_CONCLUSION = "success"),
            (process.env.GH_AW_SAFE_OUTPUTS_RESULT = "failure"),
            await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
            expect(mockGithub.request).toHaveBeenCalledWith("PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}", expect.objectContaining({ body: expect.stringContaining("failed to deliver outputs. Please review the logs") })),
            expect(mockGithub.request).not.toHaveBeenCalledWith("PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}", expect.objectContaining({ body: expect.stringContaining("completed successfully\\!") })));
        }),
          it("should show failure message when agent succeeds, safe_outputs fails, and detection warns", async () => {
            ((process.env.GH_AW_COMMENT_ID = "123456"),
              (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
              (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
              (process.env.GH_AW_AGENT_CONCLUSION = "success"),
              (process.env.GH_AW_SAFE_OUTPUTS_RESULT = "failure"),
              (process.env.GH_AW_DETECTION_CONCLUSION = "warning"),
              await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
              expect(mockGithub.request).toHaveBeenCalledWith("PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}", expect.objectContaining({ body: expect.stringContaining("failed to deliver outputs. Please review the logs") })));
          }),
          it("should show success message when agent succeeds and safe_outputs succeeds", async () => {
            ((process.env.GH_AW_COMMENT_ID = "123456"),
              (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
              (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
              (process.env.GH_AW_AGENT_CONCLUSION = "success"),
              (process.env.GH_AW_SAFE_OUTPUTS_RESULT = "success"),
              await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
              expect(mockGithub.request).toHaveBeenCalledWith("PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}", expect.objectContaining({ body: expect.stringContaining("completed successfully\\!") })));
          }));
      }),
      describe("footer in status comment", () => {
        (it("should include the generated footer in the updated comment body", async () => {
          ((process.env.GH_AW_COMMENT_ID = "123456"),
            (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
            (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
            (process.env.GH_AW_AGENT_CONCLUSION = "success"),
            await eval(`(async () => { ${notifyCommentScript}; await main(); })()`));
          const callArgs = mockGithub.request.mock.calls[0][1];
          expect(callArgs.body).toMatch(/Generated by \[test-workflow\]/);
          expect(callArgs.body).toMatch(/gh-aw-agentic-workflow/);
        }),
          it("should include the generated footer even when agent fails", async () => {
            ((process.env.GH_AW_COMMENT_ID = "123456"),
              (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
              (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
              (process.env.GH_AW_AGENT_CONCLUSION = "failure"),
              await eval(`(async () => { ${notifyCommentScript}; await main(); })()`));
            const callArgs = mockGithub.request.mock.calls[0][1];
            expect(callArgs.body).toMatch(/Generated by \[test-workflow\]/);
            expect(callArgs.body).toMatch(/gh-aw-agentic-workflow/);
          }));
      }));
  }));
