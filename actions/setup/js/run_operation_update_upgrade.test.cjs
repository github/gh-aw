// @ts-check
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";

/** Environment variables managed by tests */
const TEST_ENV_VARS = ["GH_AW_OPERATION", "GH_AW_CMD_PREFIX", "GH_AW_UPGRADE_OPTIONS", "GH_TOKEN", "GITHUB_TOKEN"];

describe("run_operation_update_upgrade", () => {
  let mockCore;
  let mockGithub;
  let mockContext;
  let mockExec;
  let originalGlobals;
  let originalEnv;

  beforeEach(() => {
    originalEnv = { ...process.env };

    // Save original globals
    originalGlobals = {
      core: global.core,
      github: global.github,
      context: global.context,
      exec: global.exec,
    };

    // Setup mock core module
    mockCore = {
      info: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
      notice: vi.fn(),
      setSecret: vi.fn(),
      summary: {
        addHeading: vi.fn().mockReturnThis(),
        addRaw: vi.fn().mockReturnThis(),
        write: vi.fn().mockResolvedValue(undefined),
      },
    };

    // Setup mock github
    mockGithub = {};

    // Setup mock context
    mockContext = {
      repo: {
        owner: "testowner",
        repo: "testrepo",
      },
    };

    // Setup mock exec module
    mockExec = {
      exec: vi.fn().mockResolvedValue(0),
      getExecOutput: vi.fn(),
    };

    // Set globals for the module
    global.core = mockCore;
    global.github = mockGithub;
    global.context = mockContext;
    global.exec = mockExec;
  });

  afterEach(() => {
    // Restore environment variables
    for (const key of TEST_ENV_VARS) {
      if (originalEnv[key] !== undefined) {
        process.env[key] = originalEnv[key];
      } else {
        delete process.env[key];
      }
    }

    // Restore original globals
    global.core = originalGlobals.core;
    global.github = originalGlobals.github;
    global.context = originalGlobals.context;
    global.exec = originalGlobals.exec;

    vi.clearAllMocks();
  });

  describe("formatTimestamp", () => {
    it("formats a date as YYYY-MM-DD-HH-MM-SS", async () => {
      const { formatTimestamp } = await import("./run_operation_update_upgrade.cjs");
      const date = new Date("2026-03-03T03:17:06.000Z");
      expect(formatTimestamp(date)).toBe("2026-03-03-03-17-06");
    });

    it("pads single-digit values with zeros", async () => {
      const { formatTimestamp } = await import("./run_operation_update_upgrade.cjs");
      const date = new Date("2026-01-05T09:05:03.000Z");
      expect(formatTimestamp(date)).toBe("2026-01-05-09-05-03");
    });
  });

  describe("main - skips non-update/upgrade operations", () => {
    it("skips when operation is not set", async () => {
      delete process.env.GH_AW_OPERATION;
      const { main } = await import("./run_operation_update_upgrade.cjs");
      await main();
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Skipping"));
      expect(mockExec.exec).not.toHaveBeenCalled();
    });

    it("skips when operation is unknown", async () => {
      process.env.GH_AW_OPERATION = "unknown-operation";
      const { main } = await import("./run_operation_update_upgrade.cjs");
      await main();
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Skipping"));
      expect(mockExec.exec).not.toHaveBeenCalled();
    });
  });

  describe("main - disable/enable operations", () => {
    it("runs gh aw disable and finishes without PR", async () => {
      process.env.GH_AW_OPERATION = "disable";
      process.env.GH_AW_CMD_PREFIX = "gh aw";

      const { main } = await import("./run_operation_update_upgrade.cjs");
      await main();

      expect(mockExec.exec).toHaveBeenCalledWith("gh", ["aw", "disable"]);
      expect(mockExec.exec).toHaveBeenCalledTimes(1);
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("disabled"));
      expect(mockExec.getExecOutput).not.toHaveBeenCalled();
    });

    it("runs gh aw enable and finishes without PR", async () => {
      process.env.GH_AW_OPERATION = "enable";
      process.env.GH_AW_CMD_PREFIX = "gh aw";

      const { main } = await import("./run_operation_update_upgrade.cjs");
      await main();

      expect(mockExec.exec).toHaveBeenCalledWith("gh", ["aw", "enable"]);
      expect(mockExec.exec).toHaveBeenCalledTimes(1);
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("enabled"));
      expect(mockExec.getExecOutput).not.toHaveBeenCalled();
    });

    it("runs ./gh-aw disable in dev mode", async () => {
      process.env.GH_AW_OPERATION = "disable";
      process.env.GH_AW_CMD_PREFIX = "./gh-aw";

      const { main } = await import("./run_operation_update_upgrade.cjs");
      await main();

      expect(mockExec.exec).toHaveBeenCalledWith("./gh-aw", ["disable"]);
      expect(mockExec.exec).toHaveBeenCalledTimes(1);
    });

    it("propagates error when disable command fails", async () => {
      process.env.GH_AW_OPERATION = "disable";
      process.env.GH_AW_CMD_PREFIX = "gh aw";

      mockExec.exec = vi.fn().mockRejectedValue(new Error("Command failed"));

      const { main } = await import("./run_operation_update_upgrade.cjs");
      await expect(main()).rejects.toThrow("Command failed");
    });

    it("throws when disable exits with non-zero code", async () => {
      process.env.GH_AW_OPERATION = "disable";
      process.env.GH_AW_CMD_PREFIX = "gh aw";

      mockExec.exec = vi.fn().mockResolvedValue(1);

      const { main } = await import("./run_operation_update_upgrade.cjs");
      await expect(main()).rejects.toThrow("exit code 1");
    });
  });

  describe("main - no changes after command", () => {
    it("finishes without creating PR when no known files changed", async () => {
      process.env.GH_AW_OPERATION = "update";
      process.env.GH_AW_CMD_PREFIX = "gh aw";
      process.env.GH_TOKEN = "test-token";

      // git diff --cached --name-only shows no staged changes
      mockExec.getExecOutput = vi.fn().mockResolvedValue({ stdout: "", stderr: "", exitCode: 0 });

      const { main } = await import("./run_operation_update_upgrade.cjs");
      await main();

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("No changes detected"));
      expect(mockExec.exec).toHaveBeenCalledWith("gh", ["aw", "update"]);
      // git add was called for known files
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["add", "-A", "--", ".github/aw/actions-lock.json"]);
    });

    it("does not stage workflow yml files for update operation", async () => {
      process.env.GH_AW_OPERATION = "update";
      process.env.GH_AW_CMD_PREFIX = "gh aw";
      process.env.GH_TOKEN = "test-token";

      // git diff --cached shows nothing staged (workflow files were not in allowlist)
      mockExec.getExecOutput = vi.fn().mockResolvedValue({ stdout: "", stderr: "", exitCode: 0 });

      const { main } = await import("./run_operation_update_upgrade.cjs");
      await main();

      // Workflow yml files must never be staged - they are not in the update allowlist
      expect(mockExec.exec).not.toHaveBeenCalledWith("git", ["add", "-A", "--", ".github/workflows/agentics-maintenance.yml"]);
      expect(mockExec.exec).not.toHaveBeenCalledWith("git", ["add", "-A", "--", ".github/workflows/my-workflow.md"]);
    });

    it("does not stage workflow md files for upgrade operation", async () => {
      process.env.GH_AW_OPERATION = "upgrade";
      process.env.GH_AW_CMD_PREFIX = "gh aw";
      process.env.GH_TOKEN = "test-token";

      // git diff --cached shows nothing staged
      mockExec.getExecOutput = vi.fn().mockResolvedValue({ stdout: "", stderr: "", exitCode: 0 });

      const { main } = await import("./run_operation_update_upgrade.cjs");
      await main();

      // Workflow files must never be staged
      expect(mockExec.exec).not.toHaveBeenCalledWith("git", ["add", "-A", "--", ".github/workflows/my-workflow.md"]);
      expect(mockExec.exec).not.toHaveBeenCalledWith("git", ["add", "-A", "--", ".github/workflows/agentics-maintenance.yml"]);
    });
  });

  describe("main - creates PR when files changed", () => {
    it("creates PR for update operation with changes", async () => {
      process.env.GH_AW_OPERATION = "update";
      process.env.GH_AW_CMD_PREFIX = "gh aw";
      process.env.GH_TOKEN = "test-token";

      const getExecOutputMock = vi.fn();
      // git diff --cached --name-only
      getExecOutputMock.mockResolvedValueOnce({
        stdout: ".github/aw/actions-lock.json\n",
        stderr: "",
        exitCode: 0,
      });
      // gh pr create
      getExecOutputMock.mockResolvedValueOnce({
        stdout: "https://github.com/testowner/testrepo/pull/1\n",
        stderr: "",
        exitCode: 0,
      });
      mockExec.getExecOutput = getExecOutputMock;

      const { main } = await import("./run_operation_update_upgrade.cjs");
      await main();

      // Verify gh aw update was run
      expect(mockExec.exec).toHaveBeenCalledWith("gh", ["aw", "update"]);
      // Verify only known update files were staged
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["add", "-A", "--", ".github/aw/actions-lock.json"]);
      // Verify branch was created
      expect(mockExec.exec).toHaveBeenCalledWith("git", expect.arrayContaining(["checkout", "-b", expect.stringContaining("aw/update-")]));
      // Verify commit was made
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["commit", "-m", "chore: update agentic workflows"]);
      // Verify PR title
      expect(getExecOutputMock).toHaveBeenCalledWith("gh", expect.arrayContaining(["pr", "create", "--title", "[aw] Updates available", "--label", "agentic-workflows"]), expect.anything());
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Created PR"));
    });

    it("stages only known update files, never workflow files", async () => {
      process.env.GH_AW_OPERATION = "update";
      process.env.GH_AW_CMD_PREFIX = "gh aw";
      process.env.GH_TOKEN = "test-token";

      const getExecOutputMock = vi.fn();
      // git diff --cached --name-only (only known file staged)
      getExecOutputMock.mockResolvedValueOnce({
        stdout: ".github/aw/actions-lock.json\n",
        stderr: "",
        exitCode: 0,
      });
      // gh pr create
      getExecOutputMock.mockResolvedValueOnce({
        stdout: "https://github.com/testowner/testrepo/pull/5\n",
        stderr: "",
        exitCode: 0,
      });
      mockExec.getExecOutput = getExecOutputMock;

      const { main } = await import("./run_operation_update_upgrade.cjs");
      await main();

      // Workflow files must NEVER be staged for update
      expect(mockExec.exec).not.toHaveBeenCalledWith("git", ["add", "-A", "--", ".github/workflows/my-workflow.md"]);
      expect(mockExec.exec).not.toHaveBeenCalledWith("git", ["add", "-A", "--", ".github/workflows/agentics-maintenance.yml"]);
      // Only known update file should be staged
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["add", "-A", "--", ".github/aw/actions-lock.json"]);
    });

    it("creates PR for upgrade operation with correct title", async () => {
      process.env.GH_AW_OPERATION = "upgrade";
      process.env.GH_AW_CMD_PREFIX = "gh aw";
      process.env.GH_TOKEN = "test-token";

      const getExecOutputMock = vi.fn();
      // git diff --cached --name-only
      getExecOutputMock.mockResolvedValueOnce({
        stdout: ".github/skills/agentic-workflows/SKILL.md\n",
        stderr: "",
        exitCode: 0,
      });
      // gh pr create
      getExecOutputMock.mockResolvedValueOnce({
        stdout: "https://github.com/testowner/testrepo/pull/2\n",
        stderr: "",
        exitCode: 0,
      });
      mockExec.getExecOutput = getExecOutputMock;

      const { main } = await import("./run_operation_update_upgrade.cjs");
      await main();

      // Verify gh aw upgrade was run
      expect(mockExec.exec).toHaveBeenCalledWith("gh", ["aw", "upgrade"]);
      // Verify known upgrade files were staged (including skills and agent files)
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["add", "-A", "--", ".github/aw/actions-lock.json"]);
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["add", "-A", "--", ".github/aw/instructions.md"]);
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["add", "-A", "--", ".github/skills/agentic-workflows/SKILL.md"]);
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["add", "-A", "--", ".github/skills/agentic-workflow-designer/SKILL.md"]);
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["add", "-A", "--", ".github/agents/agentic-workflows.md"]);
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["add", "-A", "--", ".github/agents/interactive-agent-designer.agent.md"]);
      // Verify correct commit message
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["commit", "-m", "chore: upgrade agentic workflows"]);
      // Verify PR title is "[aw] Upgrade available"
      expect(getExecOutputMock).toHaveBeenCalledWith("gh", expect.arrayContaining(["pr", "create", "--title", "[aw] Upgrade available", "--label", "agentic-workflows"]), expect.anything());
      // Verify workflow yml was NOT staged
      expect(mockExec.exec).not.toHaveBeenCalledWith("git", ["add", "-A", "--", ".github/workflows/agentics-maintenance.yml"]);
    });

    it("stages old agent files for upgrade operation (for deletion tracking)", async () => {
      process.env.GH_AW_OPERATION = "upgrade";
      process.env.GH_AW_CMD_PREFIX = "gh aw";
      process.env.GH_TOKEN = "test-token";

      const getExecOutputMock = vi.fn();
      // git diff --cached shows an old agent file was deleted
      getExecOutputMock.mockResolvedValueOnce({
        stdout: ".github/agents/create-agentic-workflow.agent.md\n",
        stderr: "",
        exitCode: 0,
      });
      // gh pr create
      getExecOutputMock.mockResolvedValueOnce({
        stdout: "https://github.com/testowner/testrepo/pull/6\n",
        stderr: "",
        exitCode: 0,
      });
      mockExec.getExecOutput = getExecOutputMock;

      const { main } = await import("./run_operation_update_upgrade.cjs");
      await main();

      // Old agent file deletion should be staged
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["add", "-A", "--", ".github/agents/create-agentic-workflow.agent.md"]);
    });

    it("uses ./gh-aw as binary in dev mode", async () => {
      process.env.GH_AW_OPERATION = "update";
      process.env.GH_AW_CMD_PREFIX = "./gh-aw";
      process.env.GH_TOKEN = "test-token";

      const getExecOutputMock = vi.fn();
      getExecOutputMock.mockResolvedValueOnce({ stdout: ".github/aw/actions-lock.json\n", stderr: "", exitCode: 0 }).mockResolvedValueOnce({ stdout: "https://github.com/testowner/testrepo/pull/3\n", stderr: "", exitCode: 0 });
      mockExec.getExecOutput = getExecOutputMock;

      const { main } = await import("./run_operation_update_upgrade.cjs");
      await main();

      // Verify binary is ./gh-aw (no prefix args)
      expect(mockExec.exec).toHaveBeenCalledWith("./gh-aw", ["update"]);
    });
  });

  describe("main - handles errors", () => {
    it("propagates error when command fails", async () => {
      process.env.GH_AW_OPERATION = "update";
      process.env.GH_AW_CMD_PREFIX = "gh aw";
      process.env.GH_TOKEN = "test-token";

      mockExec.exec = vi.fn().mockRejectedValue(new Error("Command failed"));

      const { main } = await import("./run_operation_update_upgrade.cjs");
      await expect(main()).rejects.toThrow("Command failed");
    });

    it("throws when update exits with non-zero code", async () => {
      process.env.GH_AW_OPERATION = "update";
      process.env.GH_AW_CMD_PREFIX = "gh aw";
      process.env.GH_TOKEN = "test-token";

      mockExec.exec = vi.fn().mockResolvedValue(1);

      const { main } = await import("./run_operation_update_upgrade.cjs");
      await expect(main()).rejects.toThrow("exit code 1");
    });

    it("warns and continues when staging a known file fails", async () => {
      process.env.GH_AW_OPERATION = "update";
      process.env.GH_AW_CMD_PREFIX = "gh aw";
      process.env.GH_TOKEN = "test-token";

      const getExecOutputMock = vi.fn();
      // git diff --cached --name-only - nothing was staged (git add failed)
      getExecOutputMock.mockResolvedValueOnce({ stdout: "", stderr: "", exitCode: 0 });
      mockExec.getExecOutput = getExecOutputMock;

      // git add fails for the known update file
      mockExec.exec = vi.fn().mockImplementation(async (cmd, args) => {
        if (cmd === "git" && args[0] === "add" && args[3] === ".github/aw/actions-lock.json") {
          throw new Error("git add failed");
        }
        return 0;
      });

      const { main } = await import("./run_operation_update_upgrade.cjs");
      await main();

      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Failed to stage"));
      // Nothing was staged, so no PR was created
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("No changes detected"));
    });
  });

  describe("mainNotifyIssue", () => {
    beforeEach(() => {
      process.env.GH_AW_CMD_PREFIX = "gh aw";
      process.env.GH_TOKEN = "test-token";

      mockGithub = {
        rest: {
          search: {
            issuesAndPullRequests: vi.fn().mockResolvedValue({ data: { items: [] } }),
          },
          issues: {
            update: vi.fn().mockResolvedValue({}),
            create: vi.fn().mockResolvedValue({
              data: { html_url: "https://github.com/testowner/testrepo/issues/42", number: 42 },
            }),
          },
        },
      };

      global.github = mockGithub;
    });

    it("runs upgrade and creates issue when files are changed", async () => {
      mockExec.exec = vi.fn().mockResolvedValue(0);

      // git diff returns a changed file for the first known upgrade file, empty for the rest
      mockExec.getExecOutput = vi.fn().mockImplementation(async (cmd, args) => {
        if (cmd === "git" && args[0] === "diff" && args[2] === "--" && args[3] === ".github/aw/actions-lock.json") {
          return { stdout: ".github/aw/actions-lock.json\n", stderr: "", exitCode: 0 };
        }
        return { stdout: "", stderr: "", exitCode: 0 };
      });

      const { mainNotifyIssue } = await import("./run_operation_update_upgrade.cjs");
      await mainNotifyIssue();

      expect(mockExec.exec).toHaveBeenCalledWith("gh", ["aw", "upgrade"]);
      expect(mockGithub.rest.search.issuesAndPullRequests).toHaveBeenCalledWith(expect.objectContaining({ q: expect.stringContaining("agentic-auto-upgrade") }));
      expect(mockGithub.rest.issues.create).toHaveBeenCalledWith(
        expect.objectContaining({
          owner: "testowner",
          repo: "testrepo",
          title: "[aw] Upgrade available",
          body: expect.stringContaining("<!-- gh-aw-workflow-id: agentic-auto-upgrade -->"),
          labels: ["agentic-workflows"],
        })
      );
      expect(mockCore.notice).toHaveBeenCalledWith(expect.stringContaining("https://github.com/testowner/testrepo/issues/42"));
    });

    it("passes configured upgrade options", async () => {
      process.env.GH_AW_UPGRADE_OPTIONS = '["--pre-releases"]';
      mockExec.exec = vi.fn().mockResolvedValue(0);
      mockExec.getExecOutput = vi.fn().mockResolvedValue({ stdout: "", stderr: "", exitCode: 0 });

      const { mainNotifyIssue } = await import("./run_operation_update_upgrade.cjs");
      await mainNotifyIssue();

      expect(mockExec.exec).toHaveBeenCalledWith("gh", ["aw", "upgrade", "--pre-releases"]);
    });

    it("closes existing issues before creating a new one", async () => {
      mockExec.exec = vi.fn().mockResolvedValue(0);
      mockExec.getExecOutput = vi.fn().mockImplementation(async (cmd, args) => {
        if (cmd === "git" && args[3] === ".github/aw/actions-lock.json") {
          return { stdout: ".github/aw/actions-lock.json\n", stderr: "", exitCode: 0 };
        }
        return { stdout: "", stderr: "", exitCode: 0 };
      });

      // Simulate two existing open issues with the marker
      mockGithub.rest.search.issuesAndPullRequests = vi.fn().mockResolvedValue({
        data: {
          items: [
            { number: 10, title: "old upgrade 1", body: "<!-- gh-aw-workflow-id: agentic-auto-upgrade -->", pull_request: undefined },
            { number: 11, title: "old upgrade 2", body: "<!-- gh-aw-workflow-id: agentic-auto-upgrade -->", pull_request: undefined },
          ],
        },
      });

      const { mainNotifyIssue } = await import("./run_operation_update_upgrade.cjs");
      await mainNotifyIssue();

      expect(mockGithub.rest.issues.update).toHaveBeenCalledWith(expect.objectContaining({ issue_number: 10, state: "closed" }));
      expect(mockGithub.rest.issues.update).toHaveBeenCalledWith(expect.objectContaining({ issue_number: 11, state: "closed" }));
      expect(mockGithub.rest.issues.create).toHaveBeenCalledTimes(1);
    });

    it("does not create issue when no files are changed", async () => {
      mockExec.exec = vi.fn().mockResolvedValue(0);
      // All git diff calls return empty output (no changes)
      mockExec.getExecOutput = vi.fn().mockResolvedValue({ stdout: "", stderr: "", exitCode: 0 });

      const { mainNotifyIssue } = await import("./run_operation_update_upgrade.cjs");
      await mainNotifyIssue();

      expect(mockGithub.rest.issues.create).not.toHaveBeenCalled();
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("already up to date"));
    });

    it("throws when upgrade command exits with non-zero code", async () => {
      mockExec.exec = vi.fn().mockResolvedValue(1);

      const { mainNotifyIssue } = await import("./run_operation_update_upgrade.cjs");
      await expect(mainNotifyIssue()).rejects.toThrow("exit code 1");
    });

    it("filters out pull_request items from search results", async () => {
      mockExec.exec = vi.fn().mockResolvedValue(0);
      mockExec.getExecOutput = vi.fn().mockImplementation(async (cmd, args) => {
        if (cmd === "git" && args[3] === ".github/aw/actions-lock.json") {
          return { stdout: ".github/aw/actions-lock.json\n", stderr: "", exitCode: 0 };
        }
        return { stdout: "", stderr: "", exitCode: 0 };
      });

      // Mix of PR and issue results — PR should be filtered out
      mockGithub.rest.search.issuesAndPullRequests = vi.fn().mockResolvedValue({
        data: {
          items: [
            { number: 5, title: "a PR", body: "<!-- gh-aw-workflow-id: agentic-auto-upgrade -->", pull_request: { url: "..." } },
            { number: 6, title: "an issue", body: "<!-- gh-aw-workflow-id: agentic-auto-upgrade -->", pull_request: undefined },
          ],
        },
      });

      const { mainNotifyIssue } = await import("./run_operation_update_upgrade.cjs");
      await mainNotifyIssue();

      // Only the issue (not the PR) should be closed
      expect(mockGithub.rest.issues.update).toHaveBeenCalledTimes(1);
      expect(mockGithub.rest.issues.update).toHaveBeenCalledWith(expect.objectContaining({ issue_number: 6 }));
    });

    it("retries issue creation without labels when label is missing", async () => {
      mockExec.exec = vi.fn().mockResolvedValue(0);
      mockExec.getExecOutput = vi.fn().mockImplementation(async (cmd, args) => {
        if (cmd === "git" && args[3] === ".github/aw/actions-lock.json") {
          return { stdout: ".github/aw/actions-lock.json\n", stderr: "", exitCode: 0 };
        }
        return { stdout: "", stderr: "", exitCode: 0 };
      });

      const labelError = new Error("Validation Failed");
      labelError.status = 422;
      mockGithub.rest.issues.create = vi
        .fn()
        .mockRejectedValueOnce(labelError)
        .mockResolvedValueOnce({ data: { html_url: "https://github.com/testowner/testrepo/issues/43", number: 43 } });

      const { mainNotifyIssue } = await import("./run_operation_update_upgrade.cjs");
      await mainNotifyIssue();

      expect(mockGithub.rest.issues.create).toHaveBeenCalledTimes(2);
      expect(mockGithub.rest.issues.create).toHaveBeenNthCalledWith(1, expect.objectContaining({ labels: ["agentic-workflows"] }));
      expect(mockGithub.rest.issues.create).toHaveBeenNthCalledWith(2, expect.not.objectContaining({ labels: expect.anything() }));
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("retrying without labels"));
    });
  });
});
