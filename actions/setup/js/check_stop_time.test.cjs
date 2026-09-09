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
  mockGithub = { rest: { actions: { listRepoWorkflows: vi.fn(), disableWorkflow: vi.fn() } } },
  mockContext = { repo: { owner: "testowner", repo: "testrepo" } };
((global.core = mockCore),
  (global.github = mockGithub),
  (global.context = mockContext),
  describe("check_stop_time.cjs", () => {
    let checkStopTimeScript, originalEnv;
    (beforeEach(() => {
      (vi.clearAllMocks(), (originalEnv = { GH_AW_STOP_TIME: process.env.GH_AW_STOP_TIME, GH_AW_WORKFLOW_NAME: process.env.GH_AW_WORKFLOW_NAME }));
      const scriptPath = path.join(process.cwd(), "check_stop_time.cjs");
      checkStopTimeScript = fs.readFileSync(scriptPath, "utf8");
    }),
      afterEach(() => {
        (void 0 !== originalEnv.GH_AW_STOP_TIME ? (process.env.GH_AW_STOP_TIME = originalEnv.GH_AW_STOP_TIME) : delete process.env.GH_AW_STOP_TIME,
          void 0 !== originalEnv.GH_AW_WORKFLOW_NAME ? (process.env.GH_AW_WORKFLOW_NAME = originalEnv.GH_AW_WORKFLOW_NAME) : delete process.env.GH_AW_WORKFLOW_NAME);
      }),
      describe("when stop time is not configured", () => {
        (it("should fail if GH_AW_STOP_TIME is not set", async () => {
          (delete process.env.GH_AW_STOP_TIME,
            (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
            await eval(`(async () => { ${checkStopTimeScript}; await main(); })()`),
            expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("GH_AW_STOP_TIME not specified")),
            expect(mockCore.setOutput).not.toHaveBeenCalled());
        }),
          it("should fail if GH_AW_WORKFLOW_NAME is not set", async () => {
            ((process.env.GH_AW_STOP_TIME = "2025-12-31 23:59:59"),
              delete process.env.GH_AW_WORKFLOW_NAME,
              await eval(`(async () => { ${checkStopTimeScript}; await main(); })()`),
              expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("GH_AW_WORKFLOW_NAME not specified")),
              expect(mockCore.setOutput).not.toHaveBeenCalled());
          }));
      }),
      describe("when stop time format is invalid", () => {
        it("should fail with error for invalid format", async () => {
          ((process.env.GH_AW_STOP_TIME = "invalid-date"),
            (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
            await eval(`(async () => { ${checkStopTimeScript}; await main(); })()`),
            expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Invalid stop-time format")),
            expect(mockCore.setOutput).not.toHaveBeenCalled());
        });
      }),
      describe("when stop time is in the future", () => {
        it("should allow execution", async () => {
          const futureDate = new Date();
          futureDate.setFullYear(futureDate.getFullYear() + 1);
          const stopTime = futureDate.toISOString().replace("T", " ").substring(0, 19);
          ((process.env.GH_AW_STOP_TIME = stopTime),
            (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
            await eval(`(async () => { ${checkStopTimeScript}; await main(); })()`),
            expect(mockCore.setOutput).toHaveBeenCalledWith("stop_time_ok", "true"),
            expect(mockCore.setFailed).not.toHaveBeenCalled());
        });
      }),
      describe("when stop time has been reached", () => {
        it("should set stop_time_ok to false without attempting to disable workflow", async () => {
          const pastDate = new Date();
          pastDate.setFullYear(pastDate.getFullYear() - 1);
          const stopTime = pastDate.toISOString().replace("T", " ").substring(0, 19);
          ((process.env.GH_AW_STOP_TIME = stopTime),
            (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
            await eval(`(async () => { ${checkStopTimeScript}; await main(); })()`),
            expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Stop time reached")),
            expect(mockGithub.rest.actions.listRepoWorkflows).not.toHaveBeenCalled(),
            expect(mockGithub.rest.actions.disableWorkflow).not.toHaveBeenCalled(),
            expect(mockCore.setOutput).toHaveBeenCalledWith("stop_time_ok", "false"),
            expect(mockCore.setFailed).not.toHaveBeenCalled());
        });
      }),
      describe("when stop time is a relative delta (e.g. resolved from a GitHub Actions expression)", () => {
        it("should allow execution for a future relative delta such as +48h", async () => {
          ((process.env.GH_AW_STOP_TIME = "+48h"),
            (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
            await eval(`(async () => { ${checkStopTimeScript}; await main(); })()`),
            expect(mockCore.setOutput).toHaveBeenCalledWith("stop_time_ok", "true"),
            expect(mockCore.setFailed).not.toHaveBeenCalled());
        });
        it("should support combined units such as +1d12h", async () => {
          ((process.env.GH_AW_STOP_TIME = "+1d12h"),
            (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
            await eval(`(async () => { ${checkStopTimeScript}; await main(); })()`),
            expect(mockCore.setOutput).toHaveBeenCalledWith("stop_time_ok", "true"),
            expect(mockCore.setFailed).not.toHaveBeenCalled());
        });
        it("should fail with a descriptive error for an invalid relative delta", async () => {
          ((process.env.GH_AW_STOP_TIME = "+5x"),
            (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
            await eval(`(async () => { ${checkStopTimeScript}; await main(); })()`),
            expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Invalid stop-time format")),
            expect(mockCore.setOutput).not.toHaveBeenCalled());
        });
      }));
  }));
