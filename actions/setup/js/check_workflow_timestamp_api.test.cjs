import { describe, it, expect, beforeEach, vi } from "vitest";

const mockCore = {
  debug: vi.fn(),
  info: vi.fn(),
  notice: vi.fn(),
  warning: vi.fn(),
  error: vi.fn(),
  setFailed: vi.fn(),
  setOutput: vi.fn(),
  exportVariable: vi.fn(),
  summary: {
    addRaw: vi.fn().mockReturnThis(),
    write: vi.fn().mockResolvedValue(),
  },
};

const mockGithub = {
  rest: {
    repos: {
      listCommits: vi.fn(),
      getContent: vi.fn(),
    },
  },
};

const mockContext = {
  repo: {
    owner: "test-owner",
    repo: "test-repo",
  },
  sha: "abc123",
};

const mockExec = {
  exec: vi.fn(),
};

global.core = mockCore;
global.github = mockGithub;
global.context = mockContext;
global.exec = mockExec;

describe("check_workflow_timestamp_api.cjs", () => {
  let main;

  beforeEach(async () => {
    vi.clearAllMocks();
    delete process.env.GH_AW_WORKFLOW_FILE;

    // Dynamically import the module to get fresh instance
    const module = await import("./check_workflow_timestamp_api.cjs");
    main = module.main;
  });

  describe("when environment variables are missing", () => {
    it("should fail if GH_AW_WORKFLOW_FILE is not set", async () => {
      await main();
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("GH_AW_WORKFLOW_FILE not available"));
    });
  });

  describe("when files do not exist in git", () => {
    beforeEach(() => {
      process.env.GH_AW_WORKFLOW_FILE = "test.lock.yml";
    });

    it("should skip check when source file does not exist", async () => {
      mockGithub.rest.repos.listCommits
        .mockResolvedValueOnce({ data: [] }) // Source file - no commits
        .mockResolvedValueOnce({
          data: [
            {
              sha: "lock123",
              commit: {
                committer: { date: "2024-01-01T12:00:00Z" },
                message: "Lock commit",
              },
            },
          ],
        }); // Lock file

      await main();

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Source file does not exist"));
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Skipping timestamp check"));
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should skip check when lock file does not exist", async () => {
      mockGithub.rest.repos.listCommits
        .mockResolvedValueOnce({
          data: [
            {
              sha: "src123",
              commit: {
                committer: { date: "2024-01-01T12:00:00Z" },
                message: "Source commit",
              },
            },
          ],
        }) // Source file
        .mockResolvedValueOnce({ data: [] }); // Lock file - no commits

      await main();

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Lock file does not exist"));
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Skipping timestamp check"));
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should skip check when both files do not exist", async () => {
      mockGithub.rest.repos.listCommits
        .mockResolvedValueOnce({ data: [] }) // Source file
        .mockResolvedValueOnce({ data: [] }); // Lock file

      await main();

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Skipping timestamp check"));
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });
  });

  describe("when lock file is up to date", () => {
    beforeEach(() => {
      process.env.GH_AW_WORKFLOW_FILE = "test.lock.yml";
    });

    it("should pass when lock file is newer than source file", async () => {
      mockGithub.rest.repos.listCommits
        .mockResolvedValueOnce({
          data: [
            {
              sha: "src123",
              commit: {
                committer: { date: "2024-01-01T12:00:00Z" },
                message: "Source commit",
              },
            },
          ],
        }) // Source file - older
        .mockResolvedValueOnce({
          data: [
            {
              sha: "lock123",
              commit: {
                committer: { date: "2024-01-01T13:00:00Z" },
                message: "Lock commit",
              },
            },
          ],
        }); // Lock file - newer

      await main();

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Lock file is up to date"));
      expect(mockCore.setFailed).not.toHaveBeenCalled();
      expect(mockCore.summary.addRaw).not.toHaveBeenCalled();
    });

    it("should pass when both files have same commit SHA", async () => {
      mockGithub.rest.repos.listCommits
        .mockResolvedValueOnce({
          data: [
            {
              sha: "same123",
              commit: {
                committer: { date: "2024-01-01T12:00:00Z" },
                message: "Same commit",
              },
            },
          ],
        }) // Source file
        .mockResolvedValueOnce({
          data: [
            {
              sha: "same123",
              commit: {
                committer: { date: "2024-01-01T12:00:00Z" },
                message: "Same commit",
              },
            },
          ],
        }); // Lock file

      await main();

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Lock file is up to date (same commit)"));
      expect(mockCore.setFailed).not.toHaveBeenCalled();
      expect(mockCore.summary.addRaw).not.toHaveBeenCalled();
    });
  });

  describe("when lock file is outdated", () => {
    beforeEach(() => {
      process.env.GH_AW_WORKFLOW_FILE = "test.lock.yml";
    });

    it("should fail when source file is newer than lock file", async () => {
      mockGithub.rest.repos.listCommits
        .mockResolvedValueOnce({
          data: [
            {
              sha: "src123",
              commit: {
                committer: { date: "2024-01-01T13:00:00Z" },
                message: "Source commit",
              },
            },
          ],
        }) // Source file - newer
        .mockResolvedValueOnce({
          data: [
            {
              sha: "lock123",
              commit: {
                committer: { date: "2024-01-01T12:00:00Z" },
                message: "Lock commit",
              },
            },
          ],
        }); // Lock file - older

      await main();

      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Lock file"));
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("is outdated"));
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("gh aw compile"));
      expect(mockCore.summary.addRaw).toHaveBeenCalled();
      expect(mockCore.summary.write).toHaveBeenCalled();
    });

    it("should include file paths in failure message", async () => {
      process.env.GH_AW_WORKFLOW_FILE = "my-workflow.lock.yml";

      mockGithub.rest.repos.listCommits
        .mockResolvedValueOnce({
          data: [
            {
              sha: "src123",
              commit: {
                committer: { date: "2024-01-01T13:00:00Z" },
                message: "Source commit",
              },
            },
          ],
        }) // Source file - newer
        .mockResolvedValueOnce({
          data: [
            {
              sha: "lock123",
              commit: {
                committer: { date: "2024-01-01T12:00:00Z" },
                message: "Lock commit",
              },
            },
          ],
        }); // Lock file - older

      await main();

      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringMatching(/my-workflow\.lock\.yml.*outdated/));
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringMatching(/my-workflow\.md/));
    });

    it("should add step summary with warning details", async () => {
      mockGithub.rest.repos.listCommits
        .mockResolvedValueOnce({
          data: [
            {
              sha: "src123abc",
              commit: {
                committer: { date: "2024-01-01T13:00:00Z" },
                message: "Source commit",
              },
            },
          ],
        }) // Source file - newer
        .mockResolvedValueOnce({
          data: [
            {
              sha: "lock456def",
              commit: {
                committer: { date: "2024-01-01T12:00:00Z" },
                message: "Lock commit",
              },
            },
          ],
        }); // Lock file - older

      await main();

      expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("Workflow Lock File Warning"));
      expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("WARNING"));
      expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("gh aw compile"));
      expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("src123a")); // Short SHA
      expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("lock456")); // Short SHA
      expect(mockCore.summary.write).toHaveBeenCalled();
    });

    it("should include timestamps in summary", async () => {
      mockGithub.rest.repos.listCommits
        .mockResolvedValueOnce({
          data: [
            {
              sha: "src123",
              commit: {
                committer: { date: "2024-01-01T13:00:00Z" },
                message: "Source commit",
              },
            },
          ],
        }) // Source file - newer
        .mockResolvedValueOnce({
          data: [
            {
              sha: "lock123",
              commit: {
                committer: { date: "2024-01-01T12:00:00Z" },
                message: "Lock commit",
              },
            },
          ],
        }); // Lock file - older

      await main();

      expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("2024-01-01T13:00:00")); // Source timestamp
      expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("2024-01-01T12:00:00")); // Lock timestamp
      expect(mockCore.summary.write).toHaveBeenCalled();
    });
  });

  describe("error handling", () => {
    beforeEach(() => {
      process.env.GH_AW_WORKFLOW_FILE = "test.lock.yml";
    });

    it("should handle API errors gracefully", async () => {
      mockGithub.rest.repos.listCommits.mockRejectedValue(new Error("API error"));

      await main();

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Could not fetch commit"));
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Skipping timestamp check"));
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should handle missing committer date", async () => {
      mockGithub.rest.repos.listCommits.mockResolvedValueOnce({
        data: [
          {
            sha: "src123",
            commit: {
              committer: {}, // Missing date
              message: "Source commit",
            },
          },
        ],
      });

      await main();

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Skipping timestamp check"));
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });
  });

  describe("frontmatter hash comparison", () => {
    beforeEach(() => {
      process.env.GH_AW_WORKFLOW_FILE = "test.lock.yml";
    });

    it("should log frontmatter hash comparison when both files exist", async () => {
      const validHash = "cdb5fdf551a14f93f6a8bb32b4f8ee5a6e93a8075052ecd915180be7fbc168ca";
      const lockFileContent = `# frontmatter-hash: ${validHash}
name: Test Workflow
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "test"`;

      const mdFileContent = `---
engine: copilot
---
# Test Workflow`;

      mockGithub.rest.repos.listCommits
        .mockResolvedValueOnce({
          data: [
            {
              sha: "src123",
              commit: {
                committer: { date: "2024-01-01T12:00:00Z" },
                message: "Source commit",
              },
            },
          ],
        })
        .mockResolvedValueOnce({
          data: [
            {
              sha: "lock123",
              commit: {
                committer: { date: "2024-01-01T13:00:00Z" },
                message: "Lock commit",
              },
            },
          ],
        });

      mockGithub.rest.repos.getContent.mockResolvedValueOnce({
        data: {
          type: "file",
          encoding: "base64",
          content: Buffer.from(lockFileContent).toString("base64"),
        },
      });

      // Mock the gh aw hash-frontmatter command to return a matching hash
      mockExec.exec.mockImplementation((command, args, options) => {
        if (command === "gh" && args[0] === "aw" && args[1] === "hash-frontmatter") {
          // Call the stdout listener with a hash
          options.listeners.stdout(Buffer.from(validHash + "\n"));
          return Promise.resolve(0);
        }
        return Promise.resolve(0);
      });

      await main();

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Frontmatter hash comparison"));
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Lock file hash:"));
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Recomputed hash:"));
      expect(mockExec.exec).toHaveBeenCalledWith("gh", ["aw", "hash-frontmatter", ".github/workflows/test.md"], expect.any(Object));
    });

    it("should handle missing frontmatter hash in lock file", async () => {
      const lockFileContent = `name: Test Workflow
on: push
jobs:
  test:
    runs-on: ubuntu-latest`;

      mockGithub.rest.repos.listCommits
        .mockResolvedValueOnce({
          data: [
            {
              sha: "src123",
              commit: {
                committer: { date: "2024-01-01T12:00:00Z" },
                message: "Source commit",
              },
            },
          ],
        })
        .mockResolvedValueOnce({
          data: [
            {
              sha: "lock123",
              commit: {
                committer: { date: "2024-01-01T13:00:00Z" },
                message: "Lock commit",
              },
            },
          ],
        });

      mockGithub.rest.repos.getContent.mockResolvedValueOnce({
        data: {
          type: "file",
          encoding: "base64",
          content: Buffer.from(lockFileContent).toString("base64"),
        },
      });

      await main();

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("No frontmatter hash found"));
    });

    it("should handle errors during hash computation gracefully", async () => {
      const validHash = "cdb5fdf551a14f93f6a8bb32b4f8ee5a6e93a8075052ecd915180be7fbc168ca";
      const lockFileContent = `# frontmatter-hash: ${validHash}
name: Test Workflow
on: push
jobs:
  test:
    runs-on: ubuntu-latest`;

      mockGithub.rest.repos.listCommits
        .mockResolvedValueOnce({
          data: [
            {
              sha: "src123",
              commit: {
                committer: { date: "2024-01-01T12:00:00Z" },
                message: "Source commit",
              },
            },
          ],
        })
        .mockResolvedValueOnce({
          data: [
            {
              sha: "lock123",
              commit: {
                committer: { date: "2024-01-01T13:00:00Z" },
                message: "Lock commit",
              },
            },
          ],
        });

      mockGithub.rest.repos.getContent.mockResolvedValueOnce({
        data: {
          type: "file",
          encoding: "base64",
          content: Buffer.from(lockFileContent).toString("base64"),
        },
      });

      // Mock the gh aw hash-frontmatter command to fail
      mockExec.exec.mockImplementation((command, args, options) => {
        if (command === "gh" && args[0] === "aw" && args[1] === "hash-frontmatter") {
          options.listeners.stderr(Buffer.from("Command failed"));
          return Promise.resolve(1); // Exit code 1 indicates failure
        }
        return Promise.resolve(0);
      });

      await main();

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Could not compute frontmatter hash"));
      expect(mockCore.setFailed).not.toHaveBeenCalled(); // Should not fail the workflow
    });
  });
});
