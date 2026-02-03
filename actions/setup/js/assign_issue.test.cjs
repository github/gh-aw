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
  mockExec = { exec: vi.fn() },
  mockGithub = { graphql: vi.fn() },
  mockContext = { repo: { owner: "testowner", repo: "testrepo" } };
((global.core = mockCore),
  (global.exec = mockExec),
  (global.github = mockGithub),
  (global.context = mockContext),
  describe("assign_issue.cjs", () => {
    let assignIssueScript;
    (beforeEach(() => {
      (vi.clearAllMocks(), delete process.env.GH_TOKEN, delete process.env.ASSIGNEE, delete process.env.ISSUE_NUMBER);
      const scriptPath = path.join(process.cwd(), "assign_issue.cjs");
      assignIssueScript = fs.readFileSync(scriptPath, "utf8");
    }),
      describe("Environment variable validation", () => {
        (it("should fail when GH_TOKEN is not set", async () => {
          ((process.env.ASSIGNEE = "test-user"),
            (process.env.ISSUE_NUMBER = "123"),
            delete process.env.GH_TOKEN,
            await eval(`(async () => { ${assignIssueScript}; await main(); })()`),
            expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("GH_TOKEN environment variable is required but not set")),
            expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("https://github.github.com/gh-aw/reference/safe-outputs/#assigning-issues-to-copilot")),
            expect(mockExec.exec).not.toHaveBeenCalled());
        }),
          it("should fail when GH_TOKEN is empty string", async () => {
            ((process.env.GH_TOKEN = "   "),
              (process.env.ASSIGNEE = "test-user"),
              (process.env.ISSUE_NUMBER = "123"),
              await eval(`(async () => { ${assignIssueScript}; await main(); })()`),
              expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("GH_TOKEN environment variable is required but not set")),
              expect(mockExec.exec).not.toHaveBeenCalled());
          }),
          it("should fail when ASSIGNEE is not set", async () => {
            ((process.env.GH_TOKEN = "ghp_test123"),
              (process.env.ISSUE_NUMBER = "123"),
              delete process.env.ASSIGNEE,
              await eval(`(async () => { ${assignIssueScript}; await main(); })()`),
              expect(mockCore.setFailed).toHaveBeenCalledWith("ASSIGNEE environment variable is required but not set"),
              expect(mockExec.exec).not.toHaveBeenCalled());
          }),
          it("should fail when ASSIGNEE is empty string", async () => {
            ((process.env.GH_TOKEN = "ghp_test123"),
              (process.env.ASSIGNEE = "   "),
              (process.env.ISSUE_NUMBER = "123"),
              await eval(`(async () => { ${assignIssueScript}; await main(); })()`),
              expect(mockCore.setFailed).toHaveBeenCalledWith("ASSIGNEE environment variable is required but not set"),
              expect(mockExec.exec).not.toHaveBeenCalled());
          }),
          it("should fail when ISSUE_NUMBER is not set", async () => {
            ((process.env.GH_TOKEN = "ghp_test123"),
              (process.env.ASSIGNEE = "test-user"),
              delete process.env.ISSUE_NUMBER,
              await eval(`(async () => { ${assignIssueScript}; await main(); })()`),
              expect(mockCore.setFailed).toHaveBeenCalledWith("ISSUE_NUMBER environment variable is required but not set"),
              expect(mockExec.exec).not.toHaveBeenCalled());
          }),
          it("should fail when ISSUE_NUMBER is empty string", async () => {
            ((process.env.GH_TOKEN = "ghp_test123"),
              (process.env.ASSIGNEE = "test-user"),
              (process.env.ISSUE_NUMBER = "   "),
              await eval(`(async () => { ${assignIssueScript}; await main(); })()`),
              expect(mockCore.setFailed).toHaveBeenCalledWith("ISSUE_NUMBER environment variable is required but not set"),
              expect(mockExec.exec).not.toHaveBeenCalled());
          }));
      }),
      describe("Successful assignment for regular users", () => {
        (it("should successfully assign issue to a regular user", async () => {
          ((process.env.GH_TOKEN = "ghp_test123"),
            (process.env.ASSIGNEE = "test-user"),
            (process.env.ISSUE_NUMBER = "456"),
            mockExec.exec.mockResolvedValue(0),
            await eval(`(async () => { ${assignIssueScript}; await main(); })()`),
            expect(mockCore.info).toHaveBeenCalledWith("Assigning issue #456 to test-user"),
            expect(mockExec.exec).toHaveBeenCalledWith("gh", ["issue", "edit", "456", "--add-assignee", "test-user"], expect.objectContaining({ env: expect.objectContaining({ GH_TOKEN: "ghp_test123" }) })),
            expect(mockCore.info).toHaveBeenCalledWith("✅ Successfully assigned issue #456 to test-user"),
            expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("Successfully assigned issue #456")),
            expect(mockCore.summary.write).toHaveBeenCalled(),
            expect(mockCore.setFailed).not.toHaveBeenCalled());
        }),
          it("should trim whitespace from environment variables", async () => {
            ((process.env.GH_TOKEN = "  ghp_test123  "),
              (process.env.ASSIGNEE = "  test-user  "),
              (process.env.ISSUE_NUMBER = "  123  "),
              mockExec.exec.mockResolvedValue(0),
              await eval(`(async () => { ${assignIssueScript}; await main(); })()`),
              expect(mockCore.info).toHaveBeenCalledWith("Assigning issue #123 to test-user"),
              expect(mockExec.exec).toHaveBeenCalledWith("gh", ["issue", "edit", "123", "--add-assignee", "test-user"], expect.any(Object)),
              expect(mockCore.setFailed).not.toHaveBeenCalled());
          }),
          it("should include summary in output", async () => {
            ((process.env.GH_TOKEN = "ghp_test123"),
              (process.env.ASSIGNEE = "test-user"),
              (process.env.ISSUE_NUMBER = "123"),
              mockExec.exec.mockResolvedValue(0),
              await eval(`(async () => { ${assignIssueScript}; await main(); })()`),
              expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("## Issue Assignment")),
              expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("Successfully assigned issue #123 to `test-user`")),
              expect(mockCore.summary.write).toHaveBeenCalled());
          }));
      }),
      describe("Error handling for regular users", () => {
        (it("should handle gh CLI execution errors", async () => {
          ((process.env.GH_TOKEN = "ghp_test123"), (process.env.ASSIGNEE = "test-user"), (process.env.ISSUE_NUMBER = "999"));
          const testError = new Error("User not found");
          (mockExec.exec.mockRejectedValue(testError),
            await eval(`(async () => { ${assignIssueScript}; await main(); })()`),
            expect(mockCore.error).toHaveBeenCalledWith("Failed to assign issue: User not found"),
            expect(mockCore.setFailed).toHaveBeenCalledWith("Failed to assign issue #999 to test-user: User not found"));
        }),
          it("should handle non-Error objects in catch block", async () => {
            ((process.env.GH_TOKEN = "ghp_test123"), (process.env.ASSIGNEE = "test-user"), (process.env.ISSUE_NUMBER = "999"));
            const stringError = "Command failed";
            (mockExec.exec.mockRejectedValue(stringError),
              await eval(`(async () => { ${assignIssueScript}; await main(); })()`),
              expect(mockCore.error).toHaveBeenCalledWith("Failed to assign issue: Command failed"),
              expect(mockCore.setFailed).toHaveBeenCalledWith("Failed to assign issue #999 to test-user: Command failed"));
          }),
          it("should handle top-level errors with catch handler", async () => {
            ((process.env.GH_TOKEN = "ghp_test123"), (process.env.ASSIGNEE = "test-user"), (process.env.ISSUE_NUMBER = "123"));
            const uncaughtError = new Error("Uncaught error");
            (mockExec.exec.mockRejectedValue(uncaughtError), await eval(`(async () => { ${assignIssueScript}; await main(); })()`), expect(mockCore.setFailed).toHaveBeenCalled());
          }));
      }),
      describe("Edge cases for regular users", () => {
        (it("should handle numeric issue number", async () => {
          ((process.env.GH_TOKEN = "ghp_test123"),
            (process.env.ASSIGNEE = "test-user"),
            (process.env.ISSUE_NUMBER = "123"),
            mockExec.exec.mockResolvedValue(0),
            await eval(`(async () => { ${assignIssueScript}; await main(); })()`),
            expect(mockExec.exec).toHaveBeenCalledWith("gh", ["issue", "edit", "123", "--add-assignee", "test-user"], expect.any(Object)));
        }),
          it("should pass through GH_TOKEN in exec environment", async () => {
            ((process.env.GH_TOKEN = "ghp_test123"),
              (process.env.ASSIGNEE = "test-user"),
              (process.env.ISSUE_NUMBER = "123"),
              (process.env.OTHER_VAR = "other_value"),
              mockExec.exec.mockResolvedValue(0),
              await eval(`(async () => { ${assignIssueScript}; await main(); })()`),
              expect(mockExec.exec).toHaveBeenCalledWith("gh", ["issue", "edit", "123", "--add-assignee", "test-user"], { env: expect.objectContaining({ GH_TOKEN: "ghp_test123", OTHER_VAR: "other_value" }) }));
          }),
          it("should handle special characters in assignee name", async () => {
            ((process.env.GH_TOKEN = "ghp_test123"),
              (process.env.ASSIGNEE = "user-with-dash"),
              (process.env.ISSUE_NUMBER = "123"),
              mockExec.exec.mockResolvedValue(0),
              await eval(`(async () => { ${assignIssueScript}; await main(); })()`),
              expect(mockExec.exec).toHaveBeenCalledWith("gh", ["issue", "edit", "123", "--add-assignee", "user-with-dash"], expect.any(Object)),
              expect(mockCore.setFailed).not.toHaveBeenCalled());
          }),
          it("should include documentation link in error message", async () => {
            (delete process.env.GH_TOKEN, (process.env.ASSIGNEE = "test-user"), (process.env.ISSUE_NUMBER = "123"), await eval(`(async () => { ${assignIssueScript}; await main(); })()`));
            const failedCall = mockCore.setFailed.mock.calls[0][0];
            expect(failedCall).toContain("https://github.github.com/gh-aw/reference/safe-outputs/#assigning-issues-to-copilot");
          }));
      }));
  }));
