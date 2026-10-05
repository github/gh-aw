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
  summary: {
    addRaw: vi.fn().mockReturnThis(),
    write: vi.fn().mockResolvedValue(),
  },
};

const mockGithub = {
  rest: {
    repos: {
      getContent: vi.fn(),
    },
    actions: {
      getWorkflowRun: vi.fn(),
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
    delete process.env.GITHUB_WORKFLOW_REF;
    delete process.env.GH_AW_CONTEXT_WORKFLOW_REF;
    delete process.env.GITHUB_REPOSITORY;
    delete process.env.GITHUB_WORKSPACE;
    delete process.env.GITHUB_EVENT_NAME;
    delete process.env.GITHUB_RUN_ID;
    delete process.env.GH_AW_STALE_CHECK_FULL;

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

  describe("when lock file is up to date", () => {
    beforeEach(() => {
      process.env.GH_AW_WORKFLOW_FILE = "test.lock.yml";
    });

    it("should pass when hashes match", async () => {
      // Hash for frontmatter "engine: copilot"
      const validHash = "c2a79263dc72f28c76177afda9bf0935481b26da094407a50155a6e0244084e3";
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

      mockGithub.rest.repos.getContent
        .mockResolvedValueOnce({
          data: {
            type: "file",
            encoding: "base64",
            content: Buffer.from(lockFileContent).toString("base64"),
          },
        })
        .mockResolvedValueOnce({
          data: {
            type: "file",
            encoding: "base64",
            content: Buffer.from(mdFileContent).toString("base64"),
          },
        });

      await main();

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("✅ Lock file is up to date (hashes match)"));
      expect(mockCore.setFailed).not.toHaveBeenCalled();
      expect(mockCore.summary.addRaw).not.toHaveBeenCalled();
    });

    it("should log same-repo invocation when GITHUB_WORKFLOW_REF matches GITHUB_REPOSITORY", async () => {
      process.env.GITHUB_WORKFLOW_REF = "test-owner/test-repo/.github/workflows/test.lock.yml@refs/heads/main";
      process.env.GITHUB_REPOSITORY = "test-owner/test-repo";

      mockGithub.rest.repos.getContent.mockResolvedValue({ data: null });

      await main();

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Same-repo invocation"));
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("GITHUB_WORKFLOW_REF:"));
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Resolved source repo:"));
    });
  });

  describe("when lock file is outdated (hashes differ)", () => {
    beforeEach(() => {
      process.env.GH_AW_WORKFLOW_FILE = "test.lock.yml";
    });

    it("should fail when hashes differ", async () => {
      const storedHash = "c2a79263dc72f28c76177afda9bf0935481b26da094407a50155a6e0244084e3";
      const lockFileContent = `# frontmatter-hash: ${storedHash}
name: Test Workflow
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "test"`;

      // Different frontmatter - will produce different hash
      const mdFileContent = `---
engine: claude
model: claude-sonnet-4
---
# Test Workflow`;

      mockGithub.rest.repos.getContent
        .mockResolvedValueOnce({
          data: {
            type: "file",
            encoding: "base64",
            content: Buffer.from(lockFileContent).toString("base64"),
          },
        })
        .mockResolvedValueOnce({
          data: {
            type: "file",
            encoding: "base64",
            content: Buffer.from(mdFileContent).toString("base64"),
          },
        });

      await main();

      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Lock file"));
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("E009"));
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("CONFIG_HASH_MISMATCH"));
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("is outdated"));
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Source 'test-owner/test-repo/.github/workflows/test.md@abc123' has a frontmatter hash mismatch"));
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("gh aw compile"));
      expect(mockCore.summary.addRaw).toHaveBeenCalled();
      expect(mockCore.summary.write).toHaveBeenCalled();
    });

    it("should fail when lock file is newer than source but hashes differ", async () => {
      // Security: a tampered lock file committed after the source must still fail
      const storedHash = "c2a79263dc72f28c76177afda9bf0935481b26da094407a50155a6e0244084e3";
      const lockFileContent = `# frontmatter-hash: ${storedHash}
name: Test Workflow
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "test"`;

      // Different frontmatter - tampered source
      const mdFileContent = `---
engine: claude
model: claude-sonnet-4
---
# Test Workflow`;

      mockGithub.rest.repos.getContent
        .mockResolvedValueOnce({
          data: {
            type: "file",
            encoding: "base64",
            content: Buffer.from(lockFileContent).toString("base64"),
          },
        })
        .mockResolvedValueOnce({
          data: {
            type: "file",
            encoding: "base64",
            content: Buffer.from(mdFileContent).toString("base64"),
          },
        });

      await main();

      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("is outdated"));
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("frontmatter hash mismatch"));
      expect(mockCore.summary.addRaw).toHaveBeenCalled();
      expect(mockCore.summary.write).toHaveBeenCalled();
    });

    it("should fail when hash check cannot be performed (no hash in lock file)", async () => {
      const lockFileContent = `name: Test Workflow
on: push
jobs:
  test:
    runs-on: ubuntu-latest`;

      mockGithub.rest.repos.getContent.mockResolvedValueOnce({
        data: {
          type: "file",
          encoding: "base64",
          content: Buffer.from(lockFileContent).toString("base64"),
        },
      });

      await main();

      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Could not compare frontmatter hashes"));
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("E009"));
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("CONFIG_HASH_MISMATCH"));
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("is outdated"));
      expect(mockCore.summary.addRaw).toHaveBeenCalled();
      expect(mockCore.summary.write).toHaveBeenCalled();
    });

    it("should fail when lock file content cannot be fetched", async () => {
      mockGithub.rest.repos.getContent.mockResolvedValueOnce({ data: null });

      await main();

      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Could not compare frontmatter hashes"));
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("is outdated"));
      expect(mockCore.summary.addRaw).toHaveBeenCalled();
      expect(mockCore.summary.write).toHaveBeenCalled();
    });

    it("should include file paths in failure message when hashes differ", async () => {
      process.env.GH_AW_WORKFLOW_FILE = "my-workflow.lock.yml";

      const storedHash = "c2a79263dc72f28c76177afda9bf0935481b26da094407a50155a6e0244084e3";
      const lockFileContent = `# frontmatter-hash: ${storedHash}
name: Test Workflow
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "test"`;

      // Different frontmatter
      const mdFileContent = `---
engine: claude
---
# Test Workflow`;

      mockGithub.rest.repos.getContent
        .mockResolvedValueOnce({
          data: {
            type: "file",
            encoding: "base64",
            content: Buffer.from(lockFileContent).toString("base64"),
          },
        })
        .mockResolvedValueOnce({
          data: {
            type: "file",
            encoding: "base64",
            content: Buffer.from(mdFileContent).toString("base64"),
          },
        });

      await main();

      const failMessage = mockCore.setFailed.mock.calls[0][0];
      expect(failMessage).toMatch(/my-workflow\.lock\.yml/);
      expect(failMessage).toMatch(/my-workflow\.md/);
      expect(failMessage).toMatch(/outdated/);
    });

    it("should add step summary with warning details when hashes differ", async () => {
      const storedHash = "c2a79263dc72f28c76177afda9bf0935481b26da094407a50155a6e0244084e3";
      const lockFileContent = `# frontmatter-hash: ${storedHash}
name: Test Workflow
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "test"`;

      // Different frontmatter - will produce different hash
      const mdFileContent = `---
engine: claude
---
# Test Workflow`;

      mockGithub.rest.repos.getContent
        .mockResolvedValueOnce({
          data: {
            type: "file",
            encoding: "base64",
            content: Buffer.from(lockFileContent).toString("base64"),
          },
        })
        .mockResolvedValueOnce({
          data: {
            type: "file",
            encoding: "base64",
            content: Buffer.from(mdFileContent).toString("base64"),
          },
        });

      await main();

      expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("Workflow Lock File Warning"));
      expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("WARNING"));
      expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("gh aw compile"));
      expect(mockCore.summary.write).toHaveBeenCalled();
    });
  });

  describe("error handling", () => {
    beforeEach(() => {
      process.env.GH_AW_WORKFLOW_FILE = "test.lock.yml";
    });

    it("should handle API errors gracefully by failing", async () => {
      mockGithub.rest.repos.getContent.mockRejectedValue(new Error("API error"));

      await main();

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Unable to fetch lock file content"));
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Could not compare frontmatter hashes"));
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("is outdated"));
    });
  });

  describe("hash comparison details", () => {
    beforeEach(() => {
      process.env.GH_AW_WORKFLOW_FILE = "test.lock.yml";
    });

    it("should log hash values during comparison", async () => {
      const storedHash = "c2a79263dc72f28c76177afda9bf0935481b26da094407a50155a6e0244084e3";
      const lockFileContent = `# frontmatter-hash: ${storedHash}
name: Test Workflow
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "test"`;

      // Same frontmatter - hashes will match
      const mdFileContent = `---
engine: copilot
---
# Test Workflow`;

      mockGithub.rest.repos.getContent
        .mockResolvedValueOnce({
          data: {
            type: "file",
            encoding: "base64",
            content: Buffer.from(lockFileContent).toString("base64"),
          },
        })
        .mockResolvedValueOnce({
          data: {
            type: "file",
            encoding: "base64",
            content: Buffer.from(mdFileContent).toString("base64"),
          },
        });

      await main();

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Frontmatter hash comparison"));
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Lock file hash"));
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Recomputed hash"));
    });

    it("should include hash values in summary on mismatch", async () => {
      const storedHash = "c2a79263dc72f28c76177afda9bf0935481b26da094407a50155a6e0244084e3";
      const lockFileContent = `# frontmatter-hash: ${storedHash}
name: Test Workflow
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "test"`;

      // Different frontmatter
      const mdFileContent = `---
engine: claude
---
# Test Workflow`;

      mockGithub.rest.repos.getContent
        .mockResolvedValueOnce({
          data: {
            type: "file",
            encoding: "base64",
            content: Buffer.from(lockFileContent).toString("base64"),
          },
        })
        .mockResolvedValueOnce({
          data: {
            type: "file",
            encoding: "base64",
            content: Buffer.from(mdFileContent).toString("base64"),
          },
        });

      await main();

      expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("frontmatter hash mismatch"));
      expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("Stored hash"));
      expect(mockCore.summary.write).toHaveBeenCalled();
    });
  });

  describe("lock file newer than source file (security fix)", () => {
    beforeEach(() => {
      process.env.GH_AW_WORKFLOW_FILE = "test.lock.yml";
    });

    it("should fail when lock file is newer but hashes differ", async () => {
      // Security fix: tampered lock file with newer timestamp must be rejected
      const storedHash = "c2a79263dc72f28c76177afda9bf0935481b26da094407a50155a6e0244084e3";
      const lockFileContent = `# frontmatter-hash: ${storedHash}
name: Test Workflow
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "test"`;

      // Different frontmatter - will produce different hash
      const mdFileContent = `---
engine: claude
model: claude-sonnet-4
---
# Test Workflow`;

      mockGithub.rest.repos.getContent
        .mockResolvedValueOnce({
          data: {
            type: "file",
            encoding: "base64",
            content: Buffer.from(lockFileContent).toString("base64"),
          },
        })
        .mockResolvedValueOnce({
          data: {
            type: "file",
            encoding: "base64",
            content: Buffer.from(mdFileContent).toString("base64"),
          },
        });

      await main();

      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("is outdated"));
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("frontmatter hash mismatch"));
      expect(mockCore.summary.addRaw).toHaveBeenCalled();
      expect(mockCore.summary.write).toHaveBeenCalled();
    });
  });

  describe("cross-repo invocation via org rulesets", () => {
    beforeEach(() => {
      process.env.GH_AW_WORKFLOW_FILE = "test.lock.yml";
      // Simulate cross-repo: workflow defined in platform-repo, running in target-repo
      process.env.GITHUB_WORKFLOW_REF = "source-owner/source-repo/.github/workflows/test.lock.yml@refs/heads/main";
      process.env.GITHUB_REPOSITORY = "target-owner/target-repo";
    });

    it("should fetch files from the workflow source repo, not context.repo", async () => {
      const validHash = "c2a79263dc72f28c76177afda9bf0935481b26da094407a50155a6e0244084e3";
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

      mockGithub.rest.repos.getContent
        .mockResolvedValueOnce({
          data: {
            type: "file",
            encoding: "base64",
            content: Buffer.from(lockFileContent).toString("base64"),
          },
        })
        .mockResolvedValueOnce({
          data: {
            type: "file",
            encoding: "base64",
            content: Buffer.from(mdFileContent).toString("base64"),
          },
        });

      await main();

      // Verify the API was called with the workflow source repo (source-owner/source-repo),
      // not context.repo (test-owner/test-repo)
      expect(mockGithub.rest.repos.getContent).toHaveBeenCalledWith(expect.objectContaining({ owner: "source-owner", repo: "source-repo" }));
      expect(mockGithub.rest.repos.getContent).not.toHaveBeenCalledWith(expect.objectContaining({ owner: "test-owner", repo: "test-repo" }));
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Cross-repo invocation detected"));
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("✅ Lock file is up to date (hashes match)"));
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should log GITHUB_WORKFLOW_REF, GITHUB_REPOSITORY, and resolved source repo", async () => {
      mockGithub.rest.repos.getContent.mockResolvedValue({ data: null });

      await main();

      expect(mockCore.info).toHaveBeenCalledWith("GITHUB_WORKFLOW_REF: source-owner/source-repo/.github/workflows/test.lock.yml@refs/heads/main");
      expect(mockCore.info).toHaveBeenCalledWith("GITHUB_REPOSITORY: target-owner/target-repo");
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Resolved source repo: source-owner/source-repo @ refs/heads/main"));
    });

    it("should use the workflow ref from GITHUB_WORKFLOW_REF, not context.sha", async () => {
      const validHash = "c2a79263dc72f28c76177afda9bf0935481b26da094407a50155a6e0244084e3";
      const lockFileContent = `# frontmatter-hash: ${validHash}
name: Test Workflow
on: push
jobs:
  test:
    runs-on: ubuntu-latest`;

      const mdFileContent = `---
engine: copilot
---
# Test Workflow`;

      mockGithub.rest.repos.getContent
        .mockResolvedValueOnce({
          data: {
            type: "file",
            encoding: "base64",
            content: Buffer.from(lockFileContent).toString("base64"),
          },
        })
        .mockResolvedValueOnce({
          data: {
            type: "file",
            encoding: "base64",
            content: Buffer.from(mdFileContent).toString("base64"),
          },
        });

      await main();

      // Verify the API was called with the ref from GITHUB_WORKFLOW_REF (refs/heads/main),
      // not context.sha (abc123)
      expect(mockGithub.rest.repos.getContent).toHaveBeenCalledWith(expect.objectContaining({ ref: "refs/heads/main" }));
      expect(mockGithub.rest.repos.getContent).not.toHaveBeenCalledWith(expect.objectContaining({ ref: "abc123" }));
    });

    it("should fall back to context.repo when GITHUB_WORKFLOW_REF is not set", async () => {
      delete process.env.GITHUB_WORKFLOW_REF;

      mockGithub.rest.repos.getContent.mockResolvedValue({ data: null });

      await main();

      // Falls back to context.repo for owner/repo; ref is undefined because workflowRepo
      // (test-owner/test-repo) differs from currentRepo (target-owner/target-repo) — cross-repo
      expect(mockGithub.rest.repos.getContent).toHaveBeenCalledWith(expect.objectContaining({ owner: "test-owner", repo: "test-repo" }));
      expect(mockGithub.rest.repos.getContent).not.toHaveBeenCalledWith(expect.objectContaining({ ref: "abc123" }));
    });

    it("should fall back to context.repo when GITHUB_WORKFLOW_REF is malformed", async () => {
      process.env.GITHUB_WORKFLOW_REF = "not-a-valid-workflow-ref";

      mockGithub.rest.repos.getContent.mockResolvedValue({ data: null });

      await main();

      // Falls back to context.repo for owner/repo; ref is undefined (cross-repo, no parsed ref)
      expect(mockGithub.rest.repos.getContent).toHaveBeenCalledWith(expect.objectContaining({ owner: "test-owner", repo: "test-repo" }));
      expect(mockGithub.rest.repos.getContent).not.toHaveBeenCalledWith(expect.objectContaining({ ref: "abc123" }));
    });

    it("should use the default branch for cross-repo when GITHUB_WORKFLOW_REF has no @ref segment", async () => {
      // GITHUB_WORKFLOW_REF with owner/repo but missing the @ref suffix
      process.env.GITHUB_WORKFLOW_REF = "source-owner/source-repo/.github/workflows/test.lock.yml";

      mockGithub.rest.repos.getContent.mockResolvedValue({ data: null });

      await main();

      // Should resolve to the source repo parsed from GITHUB_WORKFLOW_REF
      expect(mockGithub.rest.repos.getContent).toHaveBeenCalledWith(expect.objectContaining({ owner: "source-owner", repo: "source-repo" }));
      // Should NOT use context.sha — ref must be undefined so GitHub API uses the default branch
      expect(mockGithub.rest.repos.getContent).not.toHaveBeenCalledWith(expect.objectContaining({ ref: "abc123" }));
      // Log should indicate default branch is being used
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("(default branch)"));
    });
  });

  describe("local filesystem fallback for cross-org reusable workflows", () => {
    let tmpDir;
    let workflowsDir;
    // Pre-computed hash for frontmatter "engine: copilot" (used across multiple tests)
    const copilotFrontmatterHash = "c2a79263dc72f28c76177afda9bf0935481b26da094407a50155a6e0244084e3";

    beforeEach(async () => {
      process.env.GH_AW_WORKFLOW_FILE = "test.lock.yml";
      // Simulate cross-org: workflow defined in source-org/source-repo, running in target-org/target-repo
      process.env.GITHUB_WORKFLOW_REF = "source-org/source-repo/.github/workflows/test.lock.yml@v1";
      process.env.GITHUB_REPOSITORY = "target-org/target-repo";

      // Create temp directory structure mimicking $GITHUB_WORKSPACE after checkout
      tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-test-"));
      workflowsDir = path.join(tmpDir, ".github", "workflows");
      fs.mkdirSync(workflowsDir, { recursive: true });

      const module = await import("./check_workflow_timestamp_api.cjs");
      main = module.main;
    });

    afterEach(() => {
      delete process.env.GITHUB_WORKSPACE;
      fs.rmSync(tmpDir, { recursive: true, force: true });
    });

    it("should pass when API fails but local files have matching hashes", async () => {
      // Simulate cross-org API permission error
      mockGithub.rest.repos.getContent.mockRejectedValue(new Error("Resource not accessible by integration"));

      // Write local files — hash matches "engine: copilot" frontmatter
      fs.writeFileSync(path.join(workflowsDir, "test.lock.yml"), `# frontmatter-hash: ${copilotFrontmatterHash}\nname: Test\n`);
      fs.writeFileSync(path.join(workflowsDir, "test.md"), "---\nengine: copilot\n---\n# Test");

      process.env.GITHUB_WORKSPACE = tmpDir;

      await main();

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("local filesystem fallback"));
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("✅ Lock file is up to date (hashes match)"));
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should fail when API fails and local files have mismatched hashes", async () => {
      // Simulate cross-org API permission error
      mockGithub.rest.repos.getContent.mockRejectedValue(new Error("Resource not accessible by integration"));

      // Lock file stores copilot hash but .md file now has claude frontmatter
      fs.writeFileSync(path.join(workflowsDir, "test.lock.yml"), `# frontmatter-hash: ${copilotFrontmatterHash}\nname: Test\n`);
      fs.writeFileSync(path.join(workflowsDir, "test.md"), "---\nengine: claude\n---\n# Test");

      process.env.GITHUB_WORKSPACE = tmpDir;

      await main();

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("local filesystem fallback"));
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("outdated"));
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Source 'source-org/source-repo/.github/workflows/test.md@v1' has a frontmatter hash mismatch"));
      expect(mockCore.summary.addRaw).toHaveBeenCalled();
      expect(mockCore.summary.write).toHaveBeenCalled();
    });

    it("should fail when both API and local filesystem are unavailable", async () => {
      // Simulate cross-org API permission error
      mockGithub.rest.repos.getContent.mockRejectedValue(new Error("Resource not accessible by integration"));
      // Do not set GITHUB_WORKSPACE — local filesystem fallback also unavailable

      await main();

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Unable to fetch lock file content for hash comparison via API"));
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("GITHUB_WORKSPACE not available"));
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Could not compare frontmatter hashes"));
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("is outdated"));
    });

    it("should fail when API fails and local lock file is missing", async () => {
      mockGithub.rest.repos.getContent.mockRejectedValue(new Error("Resource not accessible by integration"));
      // Workspace exists but lock file not present
      process.env.GITHUB_WORKSPACE = tmpDir;

      await main();

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Local lock file not found"));
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Could not compare frontmatter hashes"));
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("is outdated"));
    });

    it("should use API if available even in cross-repo scenario (API preferred over local files)", async () => {
      const lockFileContent = `# frontmatter-hash: ${copilotFrontmatterHash}\nname: Test\n`;
      const mdFileContent = "---\nengine: copilot\n---\n# Test";

      // API succeeds
      mockGithub.rest.repos.getContent
        .mockResolvedValueOnce({
          data: {
            type: "file",
            encoding: "base64",
            content: Buffer.from(lockFileContent).toString("base64"),
          },
        })
        .mockResolvedValueOnce({
          data: {
            type: "file",
            encoding: "base64",
            content: Buffer.from(mdFileContent).toString("base64"),
          },
        });

      // Local files also available (but should not be used since API succeeds)
      fs.writeFileSync(path.join(workflowsDir, "test.lock.yml"), "# frontmatter-hash: different-hash\nname: Test\n");
      fs.writeFileSync(path.join(workflowsDir, "test.md"), "---\nengine: claude\n---\n# Different");
      process.env.GITHUB_WORKSPACE = tmpDir;

      await main();

      // API result takes precedence (hashes match via API)
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("✅ Lock file is up to date (hashes match)"));
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should fall back to local files when API lock file fetch succeeds but md file fetch throws", async () => {
      // First API call (lock file) succeeds, second (md file) throws — triggers the catch-block fallback
      const lockFileContent = `# frontmatter-hash: ${copilotFrontmatterHash}\nname: Test\n`;
      mockGithub.rest.repos.getContent
        .mockResolvedValueOnce({
          data: {
            type: "file",
            encoding: "base64",
            content: Buffer.from(lockFileContent).toString("base64"),
          },
        })
        .mockRejectedValueOnce(new Error("Resource not accessible by integration"));

      // Local files have matching hashes
      fs.writeFileSync(path.join(workflowsDir, "test.lock.yml"), `# frontmatter-hash: ${copilotFrontmatterHash}\nname: Test\n`);
      fs.writeFileSync(path.join(workflowsDir, "test.md"), "---\nengine: copilot\n---\n# Test");
      process.env.GITHUB_WORKSPACE = tmpDir;

      await main();

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Could not compute frontmatter hash via API"));
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("local filesystem fallback"));
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("✅ Lock file is up to date (hashes match)"));
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should reject path traversal in GH_AW_WORKFLOW_FILE via local filesystem fallback", async () => {
      // Craft a malicious workflow file name that tries to escape the workspace
      process.env.GH_AW_WORKFLOW_FILE = "../../etc/passwd.lock.yml";
      mockGithub.rest.repos.getContent.mockRejectedValue(new Error("Resource not accessible by integration"));
      process.env.GITHUB_WORKSPACE = tmpDir;

      await main();

      // The path traversal is rejected before any file read
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("escapes workspace"));
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Could not compare frontmatter hashes"));
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("is outdated"));
    });
  });

  describe("cross-repo private callee auth failure guidance", () => {
    // Regression test for the private-callee lockdown failure described in the issue:
    // When a caller workflow_call dispatches against a private callee repo, the caller's
    // GITHUB_TOKEN is repo-scoped and cannot read from the callee's Contents API,
    // resulting in a 404/401/403.  The system should surface actionable guidance pointing
    // to GH_AW_GITHUB_TOKEN instead of the generic "run gh aw compile" message.

    beforeEach(() => {
      process.env.GH_AW_WORKFLOW_FILE = "worker-fix.lock.yml";
      process.env.GITHUB_REPOSITORY = "gominimal/min-ctl";
      process.env.GITHUB_WORKFLOW_REF = "gominimal/min-aw/.github/workflows/worker-fix.lock.yml@v0.6.3";
    });

    it("should emit GH_AW_GITHUB_TOKEN guidance when cross-repo fetch returns HTTP 404", async () => {
      const error = new Error("Not Found");
      error.status = 404;
      mockGithub.rest.repos.getContent.mockRejectedValue(error);

      await main();

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Cross-repo API access failed (HTTP 404)"));
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("GH_AW_GITHUB_TOKEN"));
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("gominimal/min-aw"));
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Configure GH_AW_GITHUB_TOKEN with read access to 'gominimal/min-aw'"));
      // Must NOT direct the user to run `gh aw compile` — that doesn't fix an auth gap
      expect(mockCore.setFailed).not.toHaveBeenCalledWith(expect.stringContaining("gh aw compile"));
    });

    it("should emit GH_AW_GITHUB_TOKEN guidance when cross-repo fetch returns HTTP 401", async () => {
      const error = new Error("Unauthorized");
      error.status = 401;
      mockGithub.rest.repos.getContent.mockRejectedValue(error);

      await main();

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Cross-repo API access failed (HTTP 401)"));
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("GH_AW_GITHUB_TOKEN"));
      expect(mockCore.setFailed).not.toHaveBeenCalledWith(expect.stringContaining("gh aw compile"));
    });

    it("should emit GH_AW_GITHUB_TOKEN guidance when cross-repo fetch returns HTTP 403", async () => {
      const error = new Error("Forbidden");
      error.status = 403;
      mockGithub.rest.repos.getContent.mockRejectedValue(error);

      await main();

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Cross-repo API access failed (HTTP 403)"));
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("GH_AW_GITHUB_TOKEN"));
      expect(mockCore.setFailed).not.toHaveBeenCalledWith(expect.stringContaining("gh aw compile"));
    });

    it("should include the callee repo name in the cross-repo auth guidance summary", async () => {
      const error = new Error("Not Found");
      error.status = 404;
      mockGithub.rest.repos.getContent.mockRejectedValue(error);

      await main();

      // Summary must explain the root cause (auth gap, not stale lock) and direct to GH_AW_GITHUB_TOKEN
      expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("GITHUB_TOKEN"));
      expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("GH_AW_GITHUB_TOKEN"));
      expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("gominimal/min-aw"));
      // Must NOT show the generic "gh aw compile" action
      const summaryArgs = mockCore.summary.addRaw.mock.calls.flat();
      expect(summaryArgs.some(a => a.includes("gh aw compile"))).toBe(false);
      expect(mockCore.summary.write).toHaveBeenCalled();
    });

    it("should still set stale_lock_file_failed output on cross-repo auth failure", async () => {
      const error = new Error("Not Found");
      error.status = 404;
      mockGithub.rest.repos.getContent.mockRejectedValue(error);

      await main();

      expect(mockCore.setOutput).toHaveBeenCalledWith("stale_lock_file_failed", "true");
    });

    it("should NOT emit cross-repo auth guidance for a generic API error without HTTP status", async () => {
      // Generic network/permission error without a numeric status (existing test behaviour)
      mockGithub.rest.repos.getContent.mockRejectedValue(new Error("Resource not accessible by integration"));

      await main();

      expect(mockCore.info).not.toHaveBeenCalledWith(expect.stringContaining("Cross-repo API access failed"));
      // Falls back to the generic "outdated or unverifiable" message
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("is outdated"));
    });

    it("should NOT emit cross-repo auth guidance for same-repo HTTP 404", async () => {
      // Same-repo scenario: source == current repo, so no cross-repo auth guidance expected
      process.env.GITHUB_REPOSITORY = "gominimal/min-aw";
      const error = new Error("Not Found");
      error.status = 404;
      mockGithub.rest.repos.getContent.mockRejectedValue(error);

      await main();

      expect(mockCore.info).not.toHaveBeenCalledWith(expect.stringContaining("Cross-repo API access failed"));
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("is outdated"));
    });

    it("should succeed via local filesystem fallback even when cross-repo API returns 404", async () => {
      const copilotHash = "c2a79263dc72f28c76177afda9bf0935481b26da094407a50155a6e0244084e3";
      const error = new Error("Not Found");
      error.status = 404;
      mockGithub.rest.repos.getContent.mockRejectedValue(error);

      // Provide local files so the filesystem fallback can verify the hash
      const tmpDir = require("fs").mkdtempSync(require("path").join(require("os").tmpdir(), "gh-aw-auth-test-"));
      const workflowsDir = require("path").join(tmpDir, ".github", "workflows");
      require("fs").mkdirSync(workflowsDir, { recursive: true });
      require("fs").writeFileSync(require("path").join(workflowsDir, "worker-fix.lock.yml"), `# frontmatter-hash: ${copilotHash}\nname: Test\n`);
      require("fs").writeFileSync(require("path").join(workflowsDir, "worker-fix.md"), "---\nengine: copilot\n---\n# Test");
      process.env.GITHUB_WORKSPACE = tmpDir;

      try {
        await main();

        // Local fallback succeeds — no failure despite the API 404
        expect(mockCore.setFailed).not.toHaveBeenCalled();
        expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("✅ Lock file is up to date"));
      } finally {
        require("fs").rmSync(tmpDir, { recursive: true, force: true });
        delete process.env.GITHUB_WORKSPACE;
      }
    });
  });

  describe("manual GH_AW_CONTEXT_WORKFLOW_REF fallback override", () => {
    // Regression test for https://github.com/github/gh-aw/issues/23935
    // In reusable workflow contexts, both GITHUB_WORKFLOW_REF and
    // ${{ github.workflow_ref }} resolve to the caller's workflow.
    // The referenced_workflows API lookup is the primary fix for identifying the callee
    // workflow. These tests cover the fallback path used when that API lookup is bypassed
    // by the short-circuit (the env ref already ends with the current workflow file, meaning
    // GH_AW_CONTEXT_WORKFLOW_REF was manually set to the callee's ref as a targeted override).

    beforeEach(() => {
      process.env.GH_AW_WORKFLOW_FILE = "test.lock.yml";
      // Simulate a caller workflow context where GITHUB_WORKFLOW_REF points at the caller.
      process.env.GITHUB_WORKFLOW_REF = "caller-owner/caller-repo/.github/workflows/caller.yml@refs/heads/main";
      process.env.GITHUB_REPOSITORY = "caller-owner/caller-repo";
      // Manually inject GH_AW_CONTEXT_WORKFLOW_REF to exercise the fallback/override path.
      // This value intentionally points to the callee repo (platform-repo) so the env ref
      // ends with "/.github/workflows/test.lock.yml", triggering the short-circuit and
      // bypassing the API lookup.
      process.env.GH_AW_CONTEXT_WORKFLOW_REF = "platform-owner/platform-repo/.github/workflows/test.lock.yml@refs/heads/main";
    });

    it("should use GH_AW_CONTEXT_WORKFLOW_REF override to identify source repo when env ref matches workflow file", async () => {
      const validHash = "c2a79263dc72f28c76177afda9bf0935481b26da094407a50155a6e0244084e3";
      const lockFileContent = `# frontmatter-hash: ${validHash}\nname: Test\n`;
      const mdFileContent = "---\nengine: copilot\n---\n# Test";

      mockGithub.rest.repos.getContent
        .mockResolvedValueOnce({
          data: { type: "file", encoding: "base64", content: Buffer.from(lockFileContent).toString("base64") },
        })
        .mockResolvedValueOnce({
          data: { type: "file", encoding: "base64", content: Buffer.from(mdFileContent).toString("base64") },
        });

      await main();

      // Must use the platform repo (from GH_AW_CONTEXT_WORKFLOW_REF override), not the caller repo
      expect(mockGithub.rest.repos.getContent).toHaveBeenCalledWith(expect.objectContaining({ owner: "platform-owner", repo: "platform-repo" }));
      expect(mockGithub.rest.repos.getContent).not.toHaveBeenCalledWith(expect.objectContaining({ owner: "caller-owner", repo: "caller-repo" }));
      expect(mockCore.setFailed).not.toHaveBeenCalled();
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("✅ Lock file is up to date"));
    });

    it("should log GH_AW_CONTEXT_WORKFLOW_REF when it is set", async () => {
      mockGithub.rest.repos.getContent.mockResolvedValue({ data: null });

      await main();

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("GH_AW_CONTEXT_WORKFLOW_REF: platform-owner/platform-repo/.github/workflows/test.lock.yml@refs/heads/main"));
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("available as env fallback"));
    });

    it("should detect cross-repo invocation using GH_AW_CONTEXT_WORKFLOW_REF source vs GITHUB_REPOSITORY", async () => {
      mockGithub.rest.repos.getContent.mockResolvedValue({ data: null });

      await main();

      // platform-repo != caller-repo so it should be detected as cross-repo
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Cross-repo invocation detected"));
    });

    it("should fall back to GITHUB_WORKFLOW_REF when GH_AW_CONTEXT_WORKFLOW_REF is not set", async () => {
      delete process.env.GH_AW_CONTEXT_WORKFLOW_REF;
      // Without GH_AW_CONTEXT_WORKFLOW_REF, falls back to GITHUB_WORKFLOW_REF (the broken behavior)
      // This test documents the fallback; GITHUB_WORKFLOW_REF points to the caller
      mockGithub.rest.repos.getContent.mockResolvedValue({ data: null });

      await main();

      // Falls back to caller repo from GITHUB_WORKFLOW_REF
      expect(mockGithub.rest.repos.getContent).toHaveBeenCalledWith(expect.objectContaining({ owner: "caller-owner", repo: "caller-repo" }));
    });
  });

  describe("same-repo invocation via workflow_call (GH_AW_CONTEXT_WORKFLOW_REF same-repo)", () => {
    // When the reusable workflow is defined in the same repo that triggers it,
    // GH_AW_CONTEXT_WORKFLOW_REF still points to the same repo as GITHUB_REPOSITORY.
    // Ensures that the same-repo code path is not broken when GH_AW_CONTEXT_WORKFLOW_REF is injected.

    beforeEach(() => {
      process.env.GH_AW_WORKFLOW_FILE = "test.lock.yml";
      // Same-repo: both the workflow file and the repository are in my-org/my-repo
      process.env.GITHUB_REPOSITORY = "my-org/my-repo";
      process.env.GH_AW_CONTEXT_WORKFLOW_REF = "my-org/my-repo/.github/workflows/test.lock.yml@refs/heads/main";
      // GITHUB_WORKFLOW_REF also matches (normal same-repo case)
      process.env.GITHUB_WORKFLOW_REF = "my-org/my-repo/.github/workflows/test.lock.yml@refs/heads/main";
    });

    it("should detect same-repo invocation when GH_AW_CONTEXT_WORKFLOW_REF points to GITHUB_REPOSITORY", async () => {
      mockGithub.rest.repos.getContent.mockResolvedValue({ data: null });

      await main();

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Same-repo invocation"));
      expect(mockCore.info).not.toHaveBeenCalledWith(expect.stringContaining("Cross-repo invocation detected"));
    });

    it("should pass hashes when same-repo and GH_AW_CONTEXT_WORKFLOW_REF is set", async () => {
      const validHash = "c2a79263dc72f28c76177afda9bf0935481b26da094407a50155a6e0244084e3";
      const lockFileContent = `# frontmatter-hash: ${validHash}\nname: Test\n`;
      const mdFileContent = "---\nengine: copilot\n---\n# Test";

      mockGithub.rest.repos.getContent
        .mockResolvedValueOnce({
          data: { type: "file", encoding: "base64", content: Buffer.from(lockFileContent).toString("base64") },
        })
        .mockResolvedValueOnce({
          data: { type: "file", encoding: "base64", content: Buffer.from(mdFileContent).toString("base64") },
        });

      await main();

      // Must use the same repo (from GH_AW_CONTEXT_WORKFLOW_REF)
      expect(mockGithub.rest.repos.getContent).toHaveBeenCalledWith(expect.objectContaining({ owner: "my-org", repo: "my-repo" }));
      expect(mockCore.setFailed).not.toHaveBeenCalled();
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("✅ Lock file is up to date"));
    });

    it("should use ref from GH_AW_CONTEXT_WORKFLOW_REF for same-repo API calls", async () => {
      mockGithub.rest.repos.getContent.mockResolvedValue({ data: null });

      await main();

      // Should use the ref from GH_AW_CONTEXT_WORKFLOW_REF (refs/heads/main), not context.sha
      // because the ref is parseable from the env var
      expect(mockGithub.rest.repos.getContent).toHaveBeenCalledWith(expect.objectContaining({ ref: "refs/heads/main" }));
      expect(mockGithub.rest.repos.getContent).not.toHaveBeenCalledWith(expect.objectContaining({ ref: "abc123" }));
    });

    it("should skip referenced_workflows API when env ref already matches the workflow file, even with a valid GITHUB_RUN_ID", async () => {
      // Short-circuit: if the env ref ends with the current workflowFile, the API call is
      // skipped to avoid unnecessary rate-limit usage in normal (non-reusable) runs.
      process.env.GITHUB_RUN_ID = "99999";
      mockGithub.rest.repos.getContent.mockResolvedValue({ data: null });

      await main();

      // API must NOT be called — env ref already identifies this workflow
      expect(mockGithub.rest.actions.getWorkflowRun).not.toHaveBeenCalled();
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("skipping referenced_workflows API lookup"));
    });
  });

  describe("cross-repo reusable workflow via referenced_workflows API (issue #24422)", () => {
    // Fix for https://github.com/github/gh-aw/issues/24422 and cross-repo bug
    // When a reusable workflow is triggered, GITHUB_EVENT_NAME reflects the ORIGINAL trigger
    // event (e.g., "push", "issues"), NOT "workflow_call". We therefore cannot rely on event
    // name to detect cross-repo scenarios.
    //
    // Additionally, github.workflow_ref (injected as GH_AW_CONTEXT_WORKFLOW_REF) resolves to
    // the CALLER's workflow ref, not the callee's. The referenced_workflows API lookup from
    // the caller's run object is the reliable way to identify the callee's repo and ref.
    //
    // In the workflow_call context, GITHUB_RUN_ID and GITHUB_REPOSITORY are set to
    // the caller's run and repo. The caller's run object includes referenced_workflows
    // listing the callee's exact path, sha, and ref.

    beforeEach(() => {
      process.env.GH_AW_WORKFLOW_FILE = "callee-workflow.lock.yml";
      process.env.GITHUB_EVENT_NAME = "workflow_call";
      process.env.GITHUB_RUN_ID = "12345";
      // GITHUB_REPOSITORY is the caller's repo in a workflow_call context
      process.env.GITHUB_REPOSITORY = "caller-owner/caller-repo";
      // GH_AW_CONTEXT_WORKFLOW_REF (from ${{ github.workflow_ref }}) resolves to the caller
      process.env.GH_AW_CONTEXT_WORKFLOW_REF = "caller-owner/caller-repo/.github/workflows/caller.yml@refs/heads/main";
      process.env.GITHUB_WORKFLOW_REF = "caller-owner/caller-repo/.github/workflows/caller.yml@refs/heads/main";
    });

    it("should use referenced_workflows to resolve callee owner/repo/ref", async () => {
      const validHash = "c2a79263dc72f28c76177afda9bf0935481b26da094407a50155a6e0244084e3";
      const lockFileContent = `# frontmatter-hash: ${validHash}\nname: Test\n`;
      const mdFileContent = "---\nengine: copilot\n---\n# Test";

      mockGithub.rest.actions.getWorkflowRun.mockResolvedValueOnce({
        data: {
          referenced_workflows: [
            {
              path: "callee-owner/callee-repo/.github/workflows/callee-workflow.lock.yml@refs/heads/main",
              sha: "deadbeef",
              ref: "refs/heads/main",
            },
          ],
        },
      });

      mockGithub.rest.repos.getContent
        .mockResolvedValueOnce({
          data: { type: "file", encoding: "base64", content: Buffer.from(lockFileContent).toString("base64") },
        })
        .mockResolvedValueOnce({
          data: { type: "file", encoding: "base64", content: Buffer.from(mdFileContent).toString("base64") },
        });

      await main();

      // Must use the callee repo (from referenced_workflows), not the caller repo
      // sha (deadbeef) is preferred over ref (refs/heads/main) for deterministic lookups
      expect(mockGithub.rest.actions.getWorkflowRun).toHaveBeenCalledWith(expect.objectContaining({ owner: "caller-owner", repo: "caller-repo", run_id: 12345 }));
      expect(mockGithub.rest.repos.getContent).toHaveBeenCalledWith(expect.objectContaining({ owner: "callee-owner", repo: "callee-repo", ref: "deadbeef" }));
      expect(mockGithub.rest.repos.getContent).not.toHaveBeenCalledWith(expect.objectContaining({ owner: "caller-owner", repo: "caller-repo" }));
      expect(mockCore.setFailed).not.toHaveBeenCalled();
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("✅ Lock file is up to date"));
    });

    it("should log resolved callee repo info from referenced_workflows", async () => {
      mockGithub.rest.actions.getWorkflowRun.mockResolvedValueOnce({
        data: {
          referenced_workflows: [
            {
              path: "callee-owner/callee-repo/.github/workflows/callee-workflow.lock.yml@refs/heads/main",
              sha: "deadbeef",
              ref: "refs/heads/main",
            },
          ],
        },
      });
      mockGithub.rest.repos.getContent.mockResolvedValue({ data: null });

      await main();

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Checking for cross-repo callee via referenced_workflows API"));
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Resolved callee repo from referenced_workflows: callee-owner/callee-repo"));
    });

    it("should fall back to GH_AW_CONTEXT_WORKFLOW_REF when no matching entry in referenced_workflows", async () => {
      // referenced_workflows exists but doesn't contain the current workflow file
      mockGithub.rest.actions.getWorkflowRun.mockResolvedValueOnce({
        data: {
          referenced_workflows: [
            {
              path: "callee-owner/callee-repo/.github/workflows/other-workflow.lock.yml@refs/heads/main",
              sha: "deadbeef",
              ref: "refs/heads/main",
            },
          ],
        },
      });
      mockGithub.rest.repos.getContent.mockResolvedValue({ data: null });

      await main();

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining('No matching entry in referenced_workflows for "callee-workflow.lock.yml"'));
      // Falls back to GH_AW_CONTEXT_WORKFLOW_REF (caller repo in this test)
      expect(mockGithub.rest.repos.getContent).toHaveBeenCalledWith(expect.objectContaining({ owner: "caller-owner", repo: "caller-repo" }));
    });

    it("should fall back to GH_AW_CONTEXT_WORKFLOW_REF when referenced_workflows is empty", async () => {
      mockGithub.rest.actions.getWorkflowRun.mockResolvedValueOnce({
        data: { referenced_workflows: [] },
      });
      mockGithub.rest.repos.getContent.mockResolvedValue({ data: null });

      await main();

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Found 0 referenced workflow(s)"));
      // Falls back to caller repo from GH_AW_CONTEXT_WORKFLOW_REF
      expect(mockGithub.rest.repos.getContent).toHaveBeenCalledWith(expect.objectContaining({ owner: "caller-owner", repo: "caller-repo" }));
    });

    it("should fall back to GH_AW_CONTEXT_WORKFLOW_REF when API call fails", async () => {
      mockGithub.rest.actions.getWorkflowRun.mockRejectedValueOnce(new Error("Resource not accessible by integration"));
      mockGithub.rest.repos.getContent.mockResolvedValue({ data: null });

      await main();

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Could not fetch referenced_workflows from API"));
      // Falls back to caller repo from GH_AW_CONTEXT_WORKFLOW_REF
      expect(mockGithub.rest.repos.getContent).toHaveBeenCalledWith(expect.objectContaining({ owner: "caller-owner", repo: "caller-repo" }));
    });

    it("should call referenced_workflows API even for non-workflow_call events", async () => {
      // In reusable workflows, GITHUB_EVENT_NAME reflects the original trigger event (e.g.,
      // "push"), not "workflow_call". We must try referenced_workflows regardless of event name.
      process.env.GITHUB_EVENT_NAME = "push";
      mockGithub.rest.actions.getWorkflowRun.mockResolvedValueOnce({
        data: {
          referenced_workflows: [
            {
              path: "callee-owner/callee-repo/.github/workflows/callee-workflow.lock.yml@refs/heads/main",
              sha: "deadbeef",
              ref: "refs/heads/main",
            },
          ],
        },
      });
      mockGithub.rest.repos.getContent.mockResolvedValue({ data: null });

      await main();

      // API must be called even for "push" events
      expect(mockGithub.rest.actions.getWorkflowRun).toHaveBeenCalled();
      // Resolves to callee repo even though GITHUB_EVENT_NAME is "push"
      expect(mockGithub.rest.repos.getContent).toHaveBeenCalledWith(expect.objectContaining({ owner: "callee-owner", repo: "callee-repo" }));
    });

    it("should prefer sha over ref from referenced_workflows entry", async () => {
      const validHash = "c2a79263dc72f28c76177afda9bf0935481b26da094407a50155a6e0244084e3";
      const lockFileContent = `# frontmatter-hash: ${validHash}\nname: Test\n`;
      const mdFileContent = "---\nengine: copilot\n---\n# Test";

      mockGithub.rest.actions.getWorkflowRun.mockResolvedValueOnce({
        data: {
          referenced_workflows: [
            {
              path: "callee-owner/callee-repo/.github/workflows/callee-workflow.lock.yml@refs/tags/v1.0.0",
              sha: "deadbeef",
              ref: "refs/tags/v1.0.0",
            },
          ],
        },
      });

      mockGithub.rest.repos.getContent
        .mockResolvedValueOnce({
          data: { type: "file", encoding: "base64", content: Buffer.from(lockFileContent).toString("base64") },
        })
        .mockResolvedValueOnce({
          data: { type: "file", encoding: "base64", content: Buffer.from(mdFileContent).toString("base64") },
        });

      await main();

      // sha (deadbeef) is preferred over ref (refs/tags/v1.0.0) to prevent drift
      expect(mockGithub.rest.repos.getContent).toHaveBeenCalledWith(expect.objectContaining({ owner: "callee-owner", repo: "callee-repo", ref: "deadbeef" }));
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should fall back to ref when sha is absent in referenced_workflows entry", async () => {
      mockGithub.rest.actions.getWorkflowRun.mockResolvedValueOnce({
        data: {
          referenced_workflows: [
            {
              path: "callee-owner/callee-repo/.github/workflows/callee-workflow.lock.yml@refs/tags/v1.0.0",
              // sha is absent — should fall back to ref
              ref: "refs/tags/v1.0.0",
            },
          ],
        },
      });
      mockGithub.rest.repos.getContent.mockResolvedValue({ data: null });

      await main();

      expect(mockGithub.rest.repos.getContent).toHaveBeenCalledWith(expect.objectContaining({ owner: "callee-owner", repo: "callee-repo", ref: "refs/tags/v1.0.0" }));
    });

    it("should fall back to GH_AW_CONTEXT_WORKFLOW_REF when GITHUB_RUN_ID is invalid", async () => {
      process.env.GITHUB_RUN_ID = "not-a-number";
      mockGithub.rest.repos.getContent.mockResolvedValue({ data: null });

      await main();

      // API must not be called with a NaN run_id
      expect(mockGithub.rest.actions.getWorkflowRun).not.toHaveBeenCalled();
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Run ID is unavailable or invalid"));
      // Falls back to caller repo from GH_AW_CONTEXT_WORKFLOW_REF
      expect(mockGithub.rest.repos.getContent).toHaveBeenCalledWith(expect.objectContaining({ owner: "caller-owner", repo: "caller-repo" }));
    });
  });

  describe("stale_lock_file_failed output", () => {
    beforeEach(() => {
      process.env.GH_AW_WORKFLOW_FILE = "test.lock.yml";
    });

    it("should set stale_lock_file_failed output when hashes differ", async () => {
      const storedHash = "c2a79263dc72f28c76177afda9bf0935481b26da094407a50155a6e0244084e3";
      const lockFileContent = `# frontmatter-hash: ${storedHash}
name: Test Workflow
on: push
jobs:
  test:
    runs-on: ubuntu-latest`;

      // Different frontmatter — produces a different hash
      const mdFileContent = `---
engine: claude
model: claude-sonnet-4
---
# Test Workflow`;

      mockGithub.rest.repos.getContent
        .mockResolvedValueOnce({
          data: { type: "file", encoding: "base64", content: Buffer.from(lockFileContent).toString("base64") },
        })
        .mockResolvedValueOnce({
          data: { type: "file", encoding: "base64", content: Buffer.from(mdFileContent).toString("base64") },
        })
        // Third call is from the debug recomputation pass (re-fetches the .md file)
        .mockResolvedValueOnce({
          data: { type: "file", encoding: "base64", content: Buffer.from(mdFileContent).toString("base64") },
        });

      await main();

      expect(mockCore.setOutput).toHaveBeenCalledWith("stale_lock_file_failed", "true");
      expect(mockCore.setFailed).toHaveBeenCalled();
    });

    it("should set stale_lock_file_failed output when hash cannot be verified", async () => {
      // No hash in lock file — compareFrontmatterHashes returns null
      const lockFileContent = `name: Test Workflow
on: push
jobs:
  test:
    runs-on: ubuntu-latest`;

      mockGithub.rest.repos.getContent.mockResolvedValueOnce({
        data: { type: "file", encoding: "base64", content: Buffer.from(lockFileContent).toString("base64") },
      });

      await main();

      expect(mockCore.setOutput).toHaveBeenCalledWith("stale_lock_file_failed", "true");
      expect(mockCore.setFailed).toHaveBeenCalled();
    });

    it("should NOT set stale_lock_file_failed output when hashes match", async () => {
      // Hash for frontmatter "engine: copilot"
      const validHash = "c2a79263dc72f28c76177afda9bf0935481b26da094407a50155a6e0244084e3";
      const lockFileContent = `# frontmatter-hash: ${validHash}
name: Test Workflow
on: push
jobs:
  test:
    runs-on: ubuntu-latest`;

      const mdFileContent = `---
engine: copilot
---
# Test Workflow`;

      mockGithub.rest.repos.getContent
        .mockResolvedValueOnce({
          data: { type: "file", encoding: "base64", content: Buffer.from(lockFileContent).toString("base64") },
        })
        .mockResolvedValueOnce({
          data: { type: "file", encoding: "base64", content: Buffer.from(mdFileContent).toString("base64") },
        });

      await main();

      expect(mockCore.setOutput).not.toHaveBeenCalledWith("stale_lock_file_failed", "true");
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });
  });

  describe("debug recomputation logging on failure", () => {
    beforeEach(() => {
      process.env.GH_AW_WORKFLOW_FILE = "test.lock.yml";
    });

    it("should emit debug recomputation log section when hashes differ", async () => {
      const storedHash = "c2a79263dc72f28c76177afda9bf0935481b26da094407a50155a6e0244084e3";
      const lockFileContent = `# frontmatter-hash: ${storedHash}
name: Test Workflow
on: push`;

      const mdFileContent = `---
engine: claude
---
# Test Workflow`;

      mockGithub.rest.repos.getContent
        .mockResolvedValueOnce({
          data: { type: "file", encoding: "base64", content: Buffer.from(lockFileContent).toString("base64") },
        })
        .mockResolvedValueOnce({
          data: { type: "file", encoding: "base64", content: Buffer.from(mdFileContent).toString("base64") },
        })
        // Third call is from the debug recomputation pass
        .mockResolvedValueOnce({
          data: { type: "file", encoding: "base64", content: Buffer.from(mdFileContent).toString("base64") },
        });

      await main();

      // The debug section header must appear in the info log
      const infoMessages = mockCore.info.mock.calls.map(c => c[0]);
      expect(infoMessages.some(m => m.includes("Debug hash recomputation"))).toBe(true);
      // Verbose hash-debug lines must appear inside the debug pass
      expect(infoMessages.some(m => m.includes("[hash-debug]"))).toBe(true);
    });

    it("should emit debug recomputation log section when hash cannot be verified", async () => {
      // No hash in lock file — triggers the null branch
      const lockFileContent = `name: Test Workflow
on: push`;

      mockGithub.rest.repos.getContent.mockResolvedValueOnce({
        data: { type: "file", encoding: "base64", content: Buffer.from(lockFileContent).toString("base64") },
      });

      await main();

      const infoMessages = mockCore.info.mock.calls.map(c => c[0]);
      expect(infoMessages.some(m => m.includes("Debug hash recomputation"))).toBe(true);
    });

    it("should NOT emit debug log section when hashes match", async () => {
      const validHash = "c2a79263dc72f28c76177afda9bf0935481b26da094407a50155a6e0244084e3";
      const lockFileContent = `# frontmatter-hash: ${validHash}
name: Test Workflow
on: push`;

      const mdFileContent = `---
engine: copilot
---
# Test Workflow`;

      mockGithub.rest.repos.getContent
        .mockResolvedValueOnce({
          data: { type: "file", encoding: "base64", content: Buffer.from(lockFileContent).toString("base64") },
        })
        .mockResolvedValueOnce({
          data: { type: "file", encoding: "base64", content: Buffer.from(mdFileContent).toString("base64") },
        });

      await main();

      const infoMessages = mockCore.info.mock.calls.map(c => c[0]);
      expect(infoMessages.some(m => m.includes("Debug hash recomputation"))).toBe(false);
      expect(infoMessages.some(m => m.includes("[hash-debug]"))).toBe(false);
    });
  });

  describe("GH_AW_STALE_CHECK_FULL mode (full stale check)", () => {
    // Pre-computed hash values for test content:
    // MD content: "---\nengine: copilot\n---\n# Test Workflow"
    // frontmatter hash for "engine: copilot": c2a79263dc72f28c76177afda9bf0935481b26da094407a50155a6e0244084e3
    // body hash for "# Test Workflow": SHA-256 of normalized body text (single opaque string)
    const copilotFrontmatterHash = "c2a79263dc72f28c76177afda9bf0935481b26da094407a50155a6e0244084e3";
    const testWorkflowBodyHash = "e03b2c4b02859c4998bba6f73a568dcc2d10a948de44959c8e0f89e3038adea9";
    const differentBodyHash = "794cb937d6acca63bce48910fce81475e29ba05b3928a457f16a9ec73c40a6eb";
    const mdFileContent = "---\nengine: copilot\n---\n# Test Workflow";
    const differentBodyMdFileContent = "---\nengine: copilot\n---\n# Different Body";

    beforeEach(() => {
      process.env.GH_AW_WORKFLOW_FILE = "test.lock.yml";
      process.env.GH_AW_STALE_CHECK_FULL = "true";
    });

    it("should pass when both frontmatter and body hashes match", async () => {
      const lockFileContent = `# gh-aw-metadata: {"schema_version":"v4","frontmatter_hash":"${copilotFrontmatterHash}","body_hash":"${testWorkflowBodyHash}"}
name: Test Workflow
on: push`;

      mockGithub.rest.repos.getContent
        // lock file fetch
        .mockResolvedValueOnce({
          data: { type: "file", encoding: "base64", content: Buffer.from(lockFileContent).toString("base64") },
        })
        // md file fetch for frontmatter hash
        .mockResolvedValueOnce({
          data: { type: "file", encoding: "base64", content: Buffer.from(mdFileContent).toString("base64") },
        })
        // md file fetch for body hash
        .mockResolvedValueOnce({
          data: { type: "file", encoding: "base64", content: Buffer.from(mdFileContent).toString("base64") },
        });

      await main();

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("✅ Lock file is up to date (hashes match)"));
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should log that it is checking both frontmatter and body hashes", async () => {
      const lockFileContent = `# gh-aw-metadata: {"schema_version":"v4","frontmatter_hash":"${copilotFrontmatterHash}","body_hash":"${testWorkflowBodyHash}"}
name: Test Workflow
on: push`;

      mockGithub.rest.repos.getContent
        .mockResolvedValueOnce({
          data: { type: "file", encoding: "base64", content: Buffer.from(lockFileContent).toString("base64") },
        })
        .mockResolvedValueOnce({
          data: { type: "file", encoding: "base64", content: Buffer.from(mdFileContent).toString("base64") },
        })
        .mockResolvedValueOnce({
          data: { type: "file", encoding: "base64", content: Buffer.from(mdFileContent).toString("base64") },
        });

      await main();

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("frontmatter + body"));
    });

    it("should fail when frontmatter matches but body hash differs", async () => {
      // Lock stores body hash for "# Test Workflow", but md body is "# Different Body"
      const lockFileContent = `# gh-aw-metadata: {"schema_version":"v4","frontmatter_hash":"${copilotFrontmatterHash}","body_hash":"${testWorkflowBodyHash}"}
name: Test Workflow
on: push`;

      mockGithub.rest.repos.getContent
        // lock file fetch
        .mockResolvedValueOnce({
          data: { type: "file", encoding: "base64", content: Buffer.from(lockFileContent).toString("base64") },
        })
        // md file fetch for frontmatter hash
        .mockResolvedValueOnce({
          data: { type: "file", encoding: "base64", content: Buffer.from(differentBodyMdFileContent).toString("base64") },
        })
        // md file fetch for body hash
        .mockResolvedValueOnce({
          data: { type: "file", encoding: "base64", content: Buffer.from(differentBodyMdFileContent).toString("base64") },
        });

      await main();

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("⚠️  Body hashes differ"));
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("is outdated"));
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Source 'test-owner/test-repo/.github/workflows/test.md@abc123' has a body hash mismatch"));
      expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("body hash mismatch"));
      expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining(`Recomputed body hash: \`${differentBodyHash.substring(0, 12)}...`));
      expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining(`Stored hash: \`${testWorkflowBodyHash.substring(0, 12)}...`));
      expect(mockCore.setOutput).toHaveBeenCalledWith("stale_lock_file_failed", "true");
    });

    it("should skip body hash check when lock file has no body hash (backward compat)", async () => {
      // Old lock file format (pre-v4) without body_hash — should still pass when frontmatter matches
      const lockFileContent = `# frontmatter-hash: ${copilotFrontmatterHash}
name: Test Workflow
on: push`;

      mockGithub.rest.repos.getContent
        .mockResolvedValueOnce({
          data: { type: "file", encoding: "base64", content: Buffer.from(lockFileContent).toString("base64") },
        })
        .mockResolvedValueOnce({
          data: { type: "file", encoding: "base64", content: Buffer.from(mdFileContent).toString("base64") },
        });

      await main();

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("skipping body hash check"));
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("✅ Lock file is up to date (hashes match)"));
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should not perform body hash check when GH_AW_STALE_CHECK_FULL is not set", async () => {
      delete process.env.GH_AW_STALE_CHECK_FULL;

      // Lock file has body_hash but stale-check full mode is off — body hash should be ignored
      const lockFileContent = `# gh-aw-metadata: {"schema_version":"v4","frontmatter_hash":"${copilotFrontmatterHash}","body_hash":"${differentBodyHash}"}
name: Test Workflow
on: push`;

      mockGithub.rest.repos.getContent
        .mockResolvedValueOnce({
          data: { type: "file", encoding: "base64", content: Buffer.from(lockFileContent).toString("base64") },
        })
        .mockResolvedValueOnce({
          data: { type: "file", encoding: "base64", content: Buffer.from(mdFileContent).toString("base64") },
        });

      await main();

      // Body hashes would differ if checked, but full mode is off — should pass
      expect(mockCore.setFailed).not.toHaveBeenCalled();
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("✅ Lock file is up to date (hashes match)"));
    });

    it("should check body hash via local filesystem fallback when API is unavailable", async () => {
      const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-full-test-"));
      const workflowsDir = path.join(tmpDir, ".github", "workflows");
      fs.mkdirSync(workflowsDir, { recursive: true });

      process.env.GITHUB_WORKFLOW_REF = "source-org/source-repo/.github/workflows/test.lock.yml@v1";
      process.env.GITHUB_REPOSITORY = "target-org/target-repo";

      const lockFileContent = `# gh-aw-metadata: {"schema_version":"v4","frontmatter_hash":"${copilotFrontmatterHash}","body_hash":"${testWorkflowBodyHash}"}
name: Test Workflow
on: push`;

      fs.writeFileSync(path.join(workflowsDir, "test.lock.yml"), lockFileContent);
      fs.writeFileSync(path.join(workflowsDir, "test.md"), mdFileContent);
      process.env.GITHUB_WORKSPACE = tmpDir;

      // Simulate cross-org API permission error
      mockGithub.rest.repos.getContent.mockRejectedValue(new Error("Resource not accessible by integration"));

      try {
        await main();

        expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("local filesystem fallback"));
        expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("✅ Lock file is up to date (hashes match)"));
        expect(mockCore.setFailed).not.toHaveBeenCalled();
      } finally {
        fs.rmSync(tmpDir, { recursive: true, force: true });
        delete process.env.GITHUB_WORKSPACE;
      }
    });

    it("should fail body hash check via local filesystem fallback when body has changed", async () => {
      const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-full-test-"));
      const workflowsDir = path.join(tmpDir, ".github", "workflows");
      fs.mkdirSync(workflowsDir, { recursive: true });

      process.env.GITHUB_WORKFLOW_REF = "source-org/source-repo/.github/workflows/test.lock.yml@v1";
      process.env.GITHUB_REPOSITORY = "target-org/target-repo";

      // Lock file has body hash for "# Test Workflow", but local .md has "# Different Body"
      const lockFileContent = `# gh-aw-metadata: {"schema_version":"v4","frontmatter_hash":"${copilotFrontmatterHash}","body_hash":"${testWorkflowBodyHash}"}
name: Test Workflow
on: push`;

      fs.writeFileSync(path.join(workflowsDir, "test.lock.yml"), lockFileContent);
      fs.writeFileSync(path.join(workflowsDir, "test.md"), differentBodyMdFileContent);
      process.env.GITHUB_WORKSPACE = tmpDir;

      mockGithub.rest.repos.getContent.mockRejectedValue(new Error("Resource not accessible by integration"));

      try {
        await main();

        expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("⚠️  Body hashes differ"));
        expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("is outdated"));
        expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("body hash mismatch"));
        expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining(`Recomputed body hash: \`${differentBodyHash.substring(0, 12)}...`));
      } finally {
        fs.rmSync(tmpDir, { recursive: true, force: true });
        delete process.env.GITHUB_WORKSPACE;
      }
    });
  });
});
