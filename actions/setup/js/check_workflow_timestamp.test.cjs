import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import fs from "fs";
import path from "path";
import os from "os";
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
};
((global.core = mockCore),
  describe("check_workflow_timestamp.cjs", () => {
    let checkWorkflowTimestampScript, originalEnv, tmpDir, workflowsDir;
    (beforeEach(() => {
      (vi.clearAllMocks(),
        (originalEnv = { GITHUB_WORKSPACE: process.env.GITHUB_WORKSPACE, GH_AW_WORKFLOW_FILE: process.env.GH_AW_WORKFLOW_FILE }),
        (tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "workflow-test-"))),
        (workflowsDir = path.join(tmpDir, ".github", "workflows")),
        fs.mkdirSync(workflowsDir, { recursive: !0 }),
        (process.env.GITHUB_WORKSPACE = tmpDir));
      const scriptPath = path.join(process.cwd(), "check_workflow_timestamp.cjs");
      checkWorkflowTimestampScript = fs.readFileSync(scriptPath, "utf8");
    }),
      afterEach(() => {
        (void 0 !== originalEnv.GITHUB_WORKSPACE ? (process.env.GITHUB_WORKSPACE = originalEnv.GITHUB_WORKSPACE) : delete process.env.GITHUB_WORKSPACE,
          void 0 !== originalEnv.GH_AW_WORKFLOW_FILE ? (process.env.GH_AW_WORKFLOW_FILE = originalEnv.GH_AW_WORKFLOW_FILE) : delete process.env.GH_AW_WORKFLOW_FILE,
          tmpDir && fs.existsSync(tmpDir) && fs.rmSync(tmpDir, { recursive: !0, force: !0 }));
      }),
      describe("when environment variables are missing", () => {
        (it("should fail if GITHUB_WORKSPACE is not set", async () => {
          (delete process.env.GITHUB_WORKSPACE,
            (process.env.GH_AW_WORKFLOW_FILE = "test.lock.yml"),
            await eval(`(async () => { ${checkWorkflowTimestampScript}; await main(); })()`),
            expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("GITHUB_WORKSPACE not available")));
        }),
          it("should fail if GH_AW_WORKFLOW_FILE is not set", async () => {
            ((process.env.GITHUB_WORKSPACE = tmpDir),
              delete process.env.GH_AW_WORKFLOW_FILE,
              await eval(`(async () => { ${checkWorkflowTimestampScript}; await main(); })()`),
              expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("GH_AW_WORKFLOW_FILE not available")));
          }));
      }),
      describe("when files do not exist", () => {
        (it("should skip check when source file does not exist", async () => {
          ((process.env.GITHUB_WORKSPACE = tmpDir), (process.env.GH_AW_WORKFLOW_FILE = "test.lock.yml"));
          const lockFile = path.join(workflowsDir, "test.lock.yml");
          (fs.writeFileSync(lockFile, "# Lock file content"),
            await eval(`(async () => { ${checkWorkflowTimestampScript}; await main(); })()`),
            expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Source file does not exist")),
            expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Skipping timestamp check")),
            expect(mockCore.setFailed).not.toHaveBeenCalled(),
            expect(mockCore.error).not.toHaveBeenCalled());
        }),
          it("should skip check when lock file does not exist", async () => {
            ((process.env.GITHUB_WORKSPACE = tmpDir), (process.env.GH_AW_WORKFLOW_FILE = "test.lock.yml"));
            const workflowFile = path.join(workflowsDir, "test.md");
            (fs.writeFileSync(workflowFile, "# Workflow content"),
              await eval(`(async () => { ${checkWorkflowTimestampScript}; await main(); })()`),
              expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Lock file does not exist")),
              expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Skipping timestamp check")),
              expect(mockCore.setFailed).not.toHaveBeenCalled(),
              expect(mockCore.error).not.toHaveBeenCalled());
          }),
          it("should skip check when both files do not exist", async () => {
            ((process.env.GITHUB_WORKSPACE = tmpDir),
              (process.env.GH_AW_WORKFLOW_FILE = "test.lock.yml"),
              await eval(`(async () => { ${checkWorkflowTimestampScript}; await main(); })()`),
              expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Skipping timestamp check")),
              expect(mockCore.setFailed).not.toHaveBeenCalled(),
              expect(mockCore.error).not.toHaveBeenCalled());
          }));
      }),
      describe("when lock file is up to date", () => {
        (it("should pass when lock file is newer than source file", async () => {
          ((process.env.GITHUB_WORKSPACE = tmpDir), (process.env.GH_AW_WORKFLOW_FILE = "test.lock.yml"));
          const workflowFile = path.join(workflowsDir, "test.md"),
            lockFile = path.join(workflowsDir, "test.lock.yml");
          (fs.writeFileSync(workflowFile, "# Workflow content"),
            await new Promise(resolve => setTimeout(resolve, 10)),
            fs.writeFileSync(lockFile, "# Lock file content"),
            await eval(`(async () => { ${checkWorkflowTimestampScript}; await main(); })()`),
            expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Lock file is up to date")),
            expect(mockCore.error).not.toHaveBeenCalled(),
            expect(mockCore.setFailed).not.toHaveBeenCalled(),
            expect(mockCore.summary.addRaw).not.toHaveBeenCalled());
        }),
          it("should pass when lock file has same timestamp as source file", async () => {
            ((process.env.GITHUB_WORKSPACE = tmpDir), (process.env.GH_AW_WORKFLOW_FILE = "test.lock.yml"));
            const workflowFile = path.join(workflowsDir, "test.md"),
              lockFile = path.join(workflowsDir, "test.lock.yml"),
              now = new Date();
            (fs.writeFileSync(workflowFile, "# Workflow content"),
              fs.writeFileSync(lockFile, "# Lock file content"),
              fs.utimesSync(workflowFile, now, now),
              fs.utimesSync(lockFile, now, now),
              await eval(`(async () => { ${checkWorkflowTimestampScript}; await main(); })()`),
              expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Lock file is up to date")),
              expect(mockCore.error).not.toHaveBeenCalled(),
              expect(mockCore.setFailed).not.toHaveBeenCalled(),
              expect(mockCore.summary.addRaw).not.toHaveBeenCalled());
          }));
      }),
      describe("when lock file is outdated", () => {
        (it("should warn when source file is newer than lock file", async () => {
          ((process.env.GITHUB_WORKSPACE = tmpDir), (process.env.GH_AW_WORKFLOW_FILE = "test.lock.yml"));
          const workflowFile = path.join(workflowsDir, "test.md"),
            lockFile = path.join(workflowsDir, "test.lock.yml");
          (fs.writeFileSync(lockFile, "# Lock file content"),
            await new Promise(resolve => setTimeout(resolve, 10)),
            fs.writeFileSync(workflowFile, "# Workflow content"),
            await eval(`(async () => { ${checkWorkflowTimestampScript}; await main(); })()`),
            expect(mockCore.error).toHaveBeenCalledWith(expect.stringContaining("WARNING: Lock file")),
            expect(mockCore.error).toHaveBeenCalledWith(expect.stringContaining("is outdated")),
            expect(mockCore.error).toHaveBeenCalledWith(expect.stringContaining("gh aw compile")),
            expect(mockCore.summary.addRaw).toHaveBeenCalled(),
            expect(mockCore.summary.write).toHaveBeenCalled(),
            expect(mockCore.setFailed).not.toHaveBeenCalled());
        }),
          it("should include file paths in warning message", async () => {
            ((process.env.GITHUB_WORKSPACE = tmpDir), (process.env.GH_AW_WORKFLOW_FILE = "my-workflow.lock.yml"));
            const workflowFile = path.join(workflowsDir, "my-workflow.md"),
              lockFile = path.join(workflowsDir, "my-workflow.lock.yml");
            (fs.writeFileSync(lockFile, "# Lock file content"),
              await new Promise(resolve => setTimeout(resolve, 10)),
              fs.writeFileSync(workflowFile, "# Workflow content"),
              await eval(`(async () => { ${checkWorkflowTimestampScript}; await main(); })()`),
              expect(mockCore.error).toHaveBeenCalledWith(expect.stringMatching(/WARNING.*my-workflow\.lock\.yml.*outdated/)),
              expect(mockCore.error).toHaveBeenCalledWith(expect.stringMatching(/my-workflow\.md/)));
          }),
          it("should add step summary with warning", async () => {
            ((process.env.GITHUB_WORKSPACE = tmpDir), (process.env.GH_AW_WORKFLOW_FILE = "test.lock.yml"));
            const workflowFile = path.join(workflowsDir, "test.md"),
              lockFile = path.join(workflowsDir, "test.lock.yml");
            (fs.writeFileSync(lockFile, "# Lock file content"),
              await new Promise(resolve => setTimeout(resolve, 10)),
              fs.writeFileSync(workflowFile, "# Workflow content"),
              await eval(`(async () => { ${checkWorkflowTimestampScript}; await main(); })()`),
              expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("Workflow Lock File Warning")),
              expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("WARNING")),
              expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("gh aw compile")),
              expect(mockCore.summary.write).toHaveBeenCalled());
          }),
          it("should include git SHA in summary when GITHUB_SHA is available", async () => {
            ((process.env.GITHUB_WORKSPACE = tmpDir), (process.env.GH_AW_WORKFLOW_FILE = "test.lock.yml"), (process.env.GITHUB_SHA = "abc123def456"));
            const workflowFile = path.join(workflowsDir, "test.md"),
              lockFile = path.join(workflowsDir, "test.lock.yml");
            (fs.writeFileSync(lockFile, "# Lock file content"),
              await new Promise(resolve => setTimeout(resolve, 10)),
              fs.writeFileSync(workflowFile, "# Workflow content"),
              await eval(`(async () => { ${checkWorkflowTimestampScript}; await main(); })()`),
              expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("Git Commit")),
              expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("abc123def456")),
              expect(mockCore.summary.write).toHaveBeenCalled());
          }),
          it("should include file timestamps in summary", async () => {
            ((process.env.GITHUB_WORKSPACE = tmpDir), (process.env.GH_AW_WORKFLOW_FILE = "test.lock.yml"));
            const workflowFile = path.join(workflowsDir, "test.md"),
              lockFile = path.join(workflowsDir, "test.lock.yml");
            (fs.writeFileSync(lockFile, "# Lock file content"),
              await new Promise(resolve => setTimeout(resolve, 10)),
              fs.writeFileSync(workflowFile, "# Workflow content"),
              await eval(`(async () => { ${checkWorkflowTimestampScript}; await main(); })()`),
              expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("modified:")),
              expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringMatching(/\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}/)),
              expect(mockCore.summary.write).toHaveBeenCalled());
          }));
      }),
      describe("with different workflow names", () => {
        (it("should handle workflow names with hyphens", async () => {
          ((process.env.GITHUB_WORKSPACE = tmpDir), (process.env.GH_AW_WORKFLOW_FILE = "my-test-workflow.lock.yml"));
          const workflowFile = path.join(workflowsDir, "my-test-workflow.md"),
            lockFile = path.join(workflowsDir, "my-test-workflow.lock.yml");
          (fs.writeFileSync(workflowFile, "# Workflow content"),
            fs.writeFileSync(lockFile, "# Lock file content"),
            await eval(`(async () => { ${checkWorkflowTimestampScript}; await main(); })()`),
            expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("my-test-workflow.md")),
            expect(mockCore.setFailed).not.toHaveBeenCalled());
        }),
          it("should handle workflow names with underscores", async () => {
            ((process.env.GITHUB_WORKSPACE = tmpDir), (process.env.GH_AW_WORKFLOW_FILE = "my_test_workflow.lock.yml"));
            const workflowFile = path.join(workflowsDir, "my_test_workflow.md"),
              lockFile = path.join(workflowsDir, "my_test_workflow.lock.yml");
            (fs.writeFileSync(workflowFile, "# Workflow content"),
              fs.writeFileSync(lockFile, "# Lock file content"),
              await eval(`(async () => { ${checkWorkflowTimestampScript}; await main(); })()`),
              expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("my_test_workflow.md")),
              expect(mockCore.setFailed).not.toHaveBeenCalled());
          }));
      }));
  }));
