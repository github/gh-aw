import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import fs from "fs";
import path from "path";
import crypto from "crypto";

const mockCore = {
  debug: vi.fn(),
  info: vi.fn(),
  notice: vi.fn(),
  warning: vi.fn(),
  error: vi.fn(),
  setFailed: vi.fn(),
  setOutput: vi.fn(),
  summary: {
    addRaw: vi.fn().mockReturnThis(),
    write: vi.fn().mockResolvedValue(void 0),
  },
};

describe("upload_assets.cjs", () => {
  let uploadAssetsScript;
  let mockExec;
  let tempBase;
  let cwdArtifacts;

  const getAssetsDir = () => path.join(tempBase, "safeoutputs", "assets");

  const setAgentOutput = data => {
    const tempFilePath = path.join(tempBase, "agent_output.json");
    const content = typeof data === "string" ? data : JSON.stringify(data);
    fs.writeFileSync(tempFilePath, content);
    process.env.GH_AW_AGENT_OUTPUT = tempFilePath;
  };

  const executeScript = async () => {
    global.core = mockCore;
    global.exec = mockExec;
    await eval(`(async () => { ${uploadAssetsScript}; await main(); })()`);
  };

  /** Creates an asset file in assetDir and returns its path, sha, and size. */
  const makeAsset = (assetDir, fileName, content = "fake content") => {
    const assetPath = path.join(assetDir, fileName);
    fs.writeFileSync(assetPath, content);
    const fileContent = fs.readFileSync(assetPath);
    const sha = crypto.createHash("sha256").update(fileContent).digest("hex");
    return { assetPath, sha, size: fileContent.length };
  };

  const trackCwdArtifact = fileName => {
    const artifactPath = path.join(process.cwd(), fileName);
    cwdArtifacts.add(artifactPath);
    return artifactPath;
  };

  const isGitCommand = (command, args, subcommand) => command === "git" && Array.isArray(args) && args[0] === subcommand;

  /** Configures mockExec so that rev-parse throws (branch missing) and all else succeeds. */
  const mockBranchMissing = onExec => {
    mockExec.exec.mockImplementation(async (command, args) => {
      onExec?.(command, args);
      if (isGitCommand(command, args, "rev-parse")) throw new Error("Branch does not exist");
      return 0;
    });
  };

  beforeEach(() => {
    vi.clearAllMocks();
    delete process.env.GH_AW_ASSETS_BRANCH;
    delete process.env.GH_AW_AGENT_OUTPUT;
    delete process.env.GH_AW_ASSETS_DIR;
    delete process.env.GH_AW_SAFE_OUTPUTS_STAGED;
    delete process.env.GITHUB_SERVER_URL;
    delete process.env.GITHUB_REPOSITORY;

    tempBase = fs.mkdtempSync(path.join("/tmp", "test-gh-aw-"));
    cwdArtifacts = new Set();
    process.env.GH_AW_ASSETS_DIR = getAssetsDir();

    uploadAssetsScript = fs.readFileSync(path.join(__dirname, "upload_assets.cjs"), "utf8");
    mockExec = {
      exec: vi.fn().mockResolvedValue(0),
      getExecOutput: vi.fn().mockResolvedValue({ exitCode: 0, stdout: "", stderr: "" }),
    };
  });

  afterEach(() => {
    if (tempBase && fs.existsSync(tempBase)) {
      fs.rmSync(tempBase, { recursive: true, force: true });
    }
    for (const artifactPath of cwdArtifacts) {
      if (fs.existsSync(artifactPath)) fs.unlinkSync(artifactPath);
    }
    cwdArtifacts = undefined;
    tempBase = undefined;
  });

  describe("environment validation", () => {
    it("should fail when GH_AW_ASSETS_BRANCH is not set", async () => {
      setAgentOutput({ items: [] });
      await executeScript();
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("GH_AW_ASSETS_BRANCH"));
    });
  });

  describe("no upload items", () => {
    it("should output upload_count=0 and branch_name when no upload-asset items found", async () => {
      process.env.GH_AW_ASSETS_BRANCH = "assets/test-workflow";
      setAgentOutput({ items: [] });
      await executeScript();
      expect(mockCore.setFailed).not.toHaveBeenCalled();
      expect(mockCore.setOutput).toHaveBeenCalledWith("upload_count", "0");
      expect(mockCore.setOutput).toHaveBeenCalledWith("branch_name", "assets/test-workflow");
    });

    it("should output upload_count=0 when all items are non-upload_asset types", async () => {
      process.env.GH_AW_ASSETS_BRANCH = "assets/test-workflow";
      setAgentOutput({ items: [{ type: "some_other_type", data: "irrelevant" }] });
      await executeScript();
      expect(mockCore.setFailed).not.toHaveBeenCalled();
      expect(mockCore.setOutput).toHaveBeenCalledWith("upload_count", "0");
    });
  });

  describe("normalizeBranchName", () => {
    it("should normalize branch names with special characters", async () => {
      process.env.GH_AW_ASSETS_BRANCH = "assets/My Branch!@#$%";
      process.env.GH_AW_SAFE_OUTPUTS_STAGED = "false";
      setAgentOutput({ items: [] });
      await executeScript();
      const branchNameCall = mockCore.setOutput.mock.calls.find(call => call[0] === "branch_name");
      expect(branchNameCall).toBeDefined();
      expect(branchNameCall[1]).toBe("assets/My-Branch");
    });
  });

  describe("branch prefix validation", () => {
    it("should allow creating orphaned branch with 'assets/' prefix when branch doesn't exist", async () => {
      process.env.GH_AW_ASSETS_BRANCH = "assets/test-workflow";
      process.env.GH_AW_SAFE_OUTPUTS_STAGED = "false";
      const assetDir = getAssetsDir();
      fs.mkdirSync(assetDir, { recursive: true });
      const { assetPath, sha, size } = makeAsset(assetDir, "test.png", "fake png data");
      setAgentOutput({
        items: [{ type: "upload_asset", fileName: "test.png", sha, size, targetFileName: "test.png", url: "https://example.com/test.png" }],
      });

      let orphanBranchCreated = false;
      trackCwdArtifact("test.png");
      mockBranchMissing((command, args) => {
        if (isGitCommand(command, args, "checkout") && args[1] === "--orphan") orphanBranchCreated = true;
      });

      await executeScript();
      expect(orphanBranchCreated).toBe(true);
      expect(mockCore.setFailed).not.toHaveBeenCalled();
      expect(mockCore.summary.addRaw).toHaveBeenCalledWith(
        `### Assets\n\nSuccessfully uploaded **1** asset to branch \`assets/test-workflow\`.\n\n<details>\n<summary>View 1 asset</summary>\n\n- [\`test.png\`](https://github.com/owner/repo/blob/assets/test-workflow/test.png?raw=true) → \`test.png\` (${size} bytes)\n\n</details>\n`
      );
      expect(mockCore.summary.write).toHaveBeenCalledOnce();
    });

    it("should fail when trying to create orphaned branch without 'assets/' prefix", async () => {
      process.env.GH_AW_ASSETS_BRANCH = "custom/branch-name";
      process.env.GH_AW_SAFE_OUTPUTS_STAGED = "false";
      const assetDir = getAssetsDir();
      fs.mkdirSync(assetDir, { recursive: true });
      const { assetPath, sha, size } = makeAsset(assetDir, "test.png", "fake png data");
      setAgentOutput({
        items: [{ type: "upload_asset", fileName: "test.png", sha, size, targetFileName: "test.png", url: "https://example.com/test.png" }],
      });

      let orphanBranchCreated = false;
      mockBranchMissing((command, args) => {
        if (isGitCommand(command, args, "checkout") && args[1] === "--orphan") orphanBranchCreated = true;
      });

      await executeScript();
      expect(orphanBranchCreated).toBe(false);
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("does not start with the required 'assets/' prefix"));
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("custom/branch-name"));
    });

    it("should allow using existing branch regardless of prefix", async () => {
      process.env.GH_AW_ASSETS_BRANCH = "custom/existing-branch";
      process.env.GH_AW_SAFE_OUTPUTS_STAGED = "false";
      const assetDir = getAssetsDir();
      fs.mkdirSync(assetDir, { recursive: true });
      const { assetPath, sha, size } = makeAsset(assetDir, "test.png", "fake png data");
      setAgentOutput({
        items: [{ type: "upload_asset", fileName: "test.png", sha, size, targetFileName: "test.png", url: "https://example.com/test.png" }],
      });

      let orphanBranchCreated = false;
      let existingBranchCheckedOut = false;
      trackCwdArtifact("test.png");
      mockExec.exec.mockImplementation(async (command, args) => {
        if (isGitCommand(command, args, "checkout") && args[1] === "--orphan") orphanBranchCreated = true;
        if (isGitCommand(command, args, "checkout") && args[1] === "-B") existingBranchCheckedOut = true;
        return 0; // rev-parse succeeds (branch exists on origin)
      });

      await executeScript();
      expect(orphanBranchCreated).toBe(false);
      expect(existingBranchCheckedOut).toBe(true);
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });
  });

  describe("SHA verification", () => {
    it("should fail when asset SHA does not match declared SHA", async () => {
      process.env.GH_AW_ASSETS_BRANCH = "assets/test-workflow";
      process.env.GH_AW_SAFE_OUTPUTS_STAGED = "false";
      const assetDir = getAssetsDir();
      fs.mkdirSync(assetDir, { recursive: true });
      const assetPath = path.join(assetDir, "test.png");
      fs.writeFileSync(assetPath, "actual content");
      setAgentOutput({
        items: [{ type: "upload_asset", fileName: "test.png", sha: "deadbeefdeadbeef", size: 14, targetFileName: "test.png", url: "https://example.com/test.png" }],
      });
      mockBranchMissing();

      await executeScript();
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("SHA mismatch"));
    });
  });

  describe("invalid asset entry", () => {
    it("should fail when asset entry is missing required fields", async () => {
      process.env.GH_AW_ASSETS_BRANCH = "assets/test-workflow";
      process.env.GH_AW_SAFE_OUTPUTS_STAGED = "false";
      const assetDir = getAssetsDir();
      fs.mkdirSync(assetDir, { recursive: true });
      // Missing sha and targetFileName
      setAgentOutput({ items: [{ type: "upload_asset", fileName: "test.png" }] });
      mockBranchMissing();

      await executeScript();
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("missing required fields"));
    });

    it("should derive trusted metadata from a validated asset path", async () => {
      process.env.GH_AW_ASSETS_BRANCH = "assets/test-workflow";
      process.env.GH_AW_SAFE_OUTPUTS_STAGED = "false";
      const assetDir = getAssetsDir();
      fs.mkdirSync(assetDir, { recursive: true });
      const declaredPath = "/workspace/test.png";
      const stagedFileName = `${crypto.createHash("sha256").update(declaredPath).digest("hex")}.png`;
      const { sha } = makeAsset(assetDir, stagedFileName, "actual content");
      trackCwdArtifact(`${sha}.png`);
      setAgentOutput({ items: [{ type: "upload_asset", path: declaredPath }] });
      mockBranchMissing();

      await executeScript();
      expect(mockCore.setFailed).not.toHaveBeenCalled();
      expect(mockCore.setOutput).toHaveBeenCalledWith("upload_count", "1");
    });

    it("should derive GHES raw URLs from trusted metadata", async () => {
      process.env.GH_AW_ASSETS_BRANCH = "assets/test-workflow";
      process.env.GH_AW_SAFE_OUTPUTS_STAGED = "false";
      process.env.GITHUB_SERVER_URL = "https://ghe.example.com";
      process.env.GITHUB_REPOSITORY = "octo/repo";
      const assetDir = getAssetsDir();
      fs.mkdirSync(assetDir, { recursive: true });
      const declaredPath = "/workspace/test.png";
      const stagedFileName = `${crypto.createHash("sha256").update(declaredPath).digest("hex")}.png`;
      const { sha } = makeAsset(assetDir, stagedFileName, "actual content");
      trackCwdArtifact(`${sha}.png`);
      setAgentOutput({ items: [{ type: "upload_asset", path: declaredPath }] });
      mockBranchMissing();

      await executeScript();

      expect(mockCore.setFailed).not.toHaveBeenCalled();
      expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining(`https://ghe.example.com/octo/repo/raw/assets/test-workflow/${sha}.png`));
    });

    it("should reject target filenames outside the checkout root", async () => {
      process.env.GH_AW_ASSETS_BRANCH = "assets/test-workflow";
      process.env.GH_AW_SAFE_OUTPUTS_STAGED = "false";
      const assetDir = getAssetsDir();
      fs.mkdirSync(assetDir, { recursive: true });
      const { sha, size } = makeAsset(assetDir, "test.png", "actual content");
      setAgentOutput({
        items: [{ type: "upload_asset", fileName: "test.png", sha, size, targetFileName: "../../.git/config" }],
      });
      mockBranchMissing();

      await executeScript();
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Invalid asset target filename"));
    });
  });

  describe("missing asset handling", () => {
    it("should skip missing assets and upload present ones", async () => {
      process.env.GH_AW_ASSETS_BRANCH = "assets/test-workflow";
      process.env.GH_AW_SAFE_OUTPUTS_STAGED = "false";
      const assetDir = getAssetsDir();
      fs.mkdirSync(assetDir, { recursive: true });
      const { assetPath: presentPath, sha: presentSha, size: presentSize } = makeAsset(assetDir, "present.png", "present content");
      const missingSha = crypto.createHash("sha256").update("missing content").digest("hex");
      setAgentOutput({
        items: [
          { type: "upload_asset", fileName: "present.png", sha: presentSha, size: presentSize, targetFileName: "present-uploaded.png", url: "https://example.com/present.png" },
          { type: "upload_asset", fileName: "missing.png", sha: missingSha, size: 7, targetFileName: "missing-uploaded.png", url: "https://example.com/missing.png" },
        ],
      });
      trackCwdArtifact("present-uploaded.png");
      mockBranchMissing();

      await executeScript();
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("missing.png"));
      expect(mockCore.setFailed).not.toHaveBeenCalled();
      const uploadCountCall = mockCore.setOutput.mock.calls.find(call => call[0] === "upload_count");
      expect(uploadCountCall).toBeDefined();
      if (uploadCountCall) expect(uploadCountCall[1]).toBe("1");
      const summary = mockCore.summary.addRaw.mock.calls.map(call => String(call[0])).join("\n");
      expect(summary).toContain("present-uploaded.png");
      expect(summary).not.toContain("missing-uploaded.png");
    });

    it("should fail when all declared assets are missing", async () => {
      process.env.GH_AW_ASSETS_BRANCH = "assets/test-workflow";
      process.env.GH_AW_SAFE_OUTPUTS_STAGED = "false";
      const assetDir = getAssetsDir();
      fs.mkdirSync(assetDir, { recursive: true });
      const missingSha = crypto.createHash("sha256").update("missing content").digest("hex");
      const missingItems = [{ type: "upload_asset", fileName: "missing.png", sha: missingSha, size: 7, targetFileName: "missing-uploaded.png", url: "https://example.com/missing.png" }];
      setAgentOutput({
        items: missingItems,
      });
      mockBranchMissing();

      await executeScript();
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining(`All ${missingItems.length} declared assets were missing`));
    });
  });

  describe("staged mode", () => {
    it("should not push to origin when GH_AW_SAFE_OUTPUTS_STAGED=true", async () => {
      process.env.GH_AW_ASSETS_BRANCH = "assets/test-workflow";
      process.env.GH_AW_SAFE_OUTPUTS_STAGED = "true";
      const assetDir = getAssetsDir();
      fs.mkdirSync(assetDir, { recursive: true });
      const { assetPath, sha, size } = makeAsset(assetDir, "test.png", "fake png data");
      setAgentOutput({
        items: [{ type: "upload_asset", fileName: "test.png", sha, size, targetFileName: "staged-test.png", url: "https://example.com/test.png" }],
      });

      let pushCalled = false;
      trackCwdArtifact("staged-test.png");
      mockBranchMissing((command, args) => {
        if (isGitCommand(command, args, "push")) pushCalled = true;
      });

      await executeScript();
      expect(pushCalled).toBe(false);
      expect(mockCore.setFailed).not.toHaveBeenCalled();
      expect(mockCore.summary.addRaw).toHaveBeenCalledWith(
        expect.stringContaining("### Assets\n\nStaged mode: **1** asset ready for publication to branch `assets/test-workflow` (not pushed).\n\n<details>\n<summary>View 1 asset</summary>\n\n")
      );
      expect(mockCore.summary.addRaw.mock.calls[0][0]).toContain("\n\n</details>\n");
    });
  });

  describe("concurrent push recovery", () => {
    const prepareAsset = () => {
      process.env.GH_AW_ASSETS_BRANCH = "assets/test-workflow";
      process.env.GH_AW_SAFE_OUTPUTS_STAGED = "false";
      const assetDir = getAssetsDir();
      fs.mkdirSync(assetDir, { recursive: true });
      const { sha, size } = makeAsset(assetDir, "test.png", "fake png data");
      setAgentOutput({
        items: [{ type: "upload_asset", fileName: "test.png", sha, size, targetFileName: "test.png", url: "https://example.com/test.png" }],
      });
      trackCwdArtifact("test.png");
    };

    it("should fetch, rebase, and retry after a concurrent push", async () => {
      prepareAsset();
      mockExec.getExecOutput.mockResolvedValueOnce({ exitCode: 1, stdout: "! [rejected] assets/test-workflow -> assets/test-workflow (fetch first)", stderr: "" }).mockResolvedValueOnce({ exitCode: 0, stdout: "", stderr: "" });

      await executeScript();

      expect(mockCore.setFailed).not.toHaveBeenCalled();
      expect(mockExec.getExecOutput).toHaveBeenCalledTimes(2);
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["fetch", "--no-tags", "origin", "+refs/heads/assets/test-workflow:refs/remotes/origin/assets/test-workflow"]);
      expect(mockExec.exec).toHaveBeenCalledWith("git", ["rebase", "refs/remotes/origin/assets/test-workflow"]);
    });

    it("should stop after three failed push attempts", async () => {
      prepareAsset();
      mockExec.getExecOutput.mockResolvedValue({ exitCode: 1, stdout: "! [rejected] assets/test-workflow -> assets/test-workflow (non-fast-forward)", stderr: "" });

      await executeScript();

      expect(mockExec.getExecOutput).toHaveBeenCalledTimes(3);
      expect(mockExec.exec.mock.calls.filter(call => isGitCommand(call[0], call[1], "fetch"))).toHaveLength(2);
      expect(mockExec.exec.mock.calls.filter(call => isGitCommand(call[0], call[1], "rebase"))).toHaveLength(2);
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("non-fast-forward"));
    });

    it("should not retry a non-concurrent push failure", async () => {
      prepareAsset();
      mockExec.getExecOutput.mockResolvedValue({ exitCode: 1, stdout: "", stderr: "remote: permission denied" });

      await executeScript();

      expect(mockExec.getExecOutput).toHaveBeenCalledTimes(1);
      expect(mockExec.exec.mock.calls.some(call => isGitCommand(call[0], call[1], "fetch"))).toBe(false);
      expect(mockExec.exec.mock.calls.some(call => isGitCommand(call[0], call[1], "rebase"))).toBe(false);
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("permission denied"));
    });

    it("should abort the rebase and rethrow when rebase fails with conflicts", async () => {
      prepareAsset();
      mockExec.getExecOutput.mockResolvedValueOnce({ exitCode: 1, stdout: "! [rejected] assets/test-workflow -> assets/test-workflow (non-fast-forward)", stderr: "" });
      mockExec.exec.mockImplementation(async (command, args) => {
        if (isGitCommand(command, args, "rebase") && args[1] !== "--abort") {
          throw new Error("CONFLICT (content): Merge conflict in test.png");
        }
      });

      await executeScript();

      expect(mockExec.exec.mock.calls.some(call => isGitCommand(call[0], call[1], "rebase") && call[1].includes("--abort"))).toBe(true);
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("CONFLICT"));
    });
  });

  describe("git commit message security", () => {
    it("should not wrap commit message in extra quotes to prevent command injection", async () => {
      process.env.GH_AW_ASSETS_BRANCH = "assets/test-workflow";
      process.env.GH_AW_SAFE_OUTPUTS_STAGED = "false";
      const assetDir = getAssetsDir();
      fs.mkdirSync(assetDir, { recursive: true });
      const { assetPath, sha, size } = makeAsset(assetDir, "test.png", "fake png data");
      setAgentOutput({
        items: [{ type: "upload_asset", fileName: "test.png", sha, size, targetFileName: "test.png", url: "https://example.com/test.png" }],
      });

      let gitCheckoutCalled = false;
      trackCwdArtifact("test.png");
      mockBranchMissing((command, args) => {
        if (isGitCommand(command, args, "checkout")) gitCheckoutCalled = true;
      });

      await executeScript();
      expect(gitCheckoutCalled).toBe(true);
      const gitCommitCall = mockExec.exec.mock.calls.find(call => Array.isArray(call[1]) && call[0] === "git" && call[1].includes("commit"));
      expect(gitCommitCall).toBeDefined();
      if (gitCommitCall) {
        const commitArgs = gitCommitCall[1];
        const messageArgIndex = commitArgs.indexOf("-m");
        const commitMessage = commitArgs[messageArgIndex + 1];
        expect(commitMessage).toBeDefined();
        expect(typeof commitMessage).toBe("string");
        expect(commitMessage).not.toMatch(/^"/);
        expect(commitMessage).not.toMatch(/"$/);
        expect(commitMessage).toContain("[skip-ci]");
        expect(commitMessage).toContain("asset(s)");
      }
    });
  });

  describe("assets dir resolution", () => {
    it("should read assets from the GH_AW_ASSETS_DIR directory", async () => {
      process.env.GH_AW_ASSETS_BRANCH = "assets/test-workflow";
      process.env.GH_AW_SAFE_OUTPUTS_STAGED = "false";
      const customAssetsDir = fs.mkdtempSync(path.join("/tmp", "test-gh-aw-assets-"));
      process.env.GH_AW_ASSETS_DIR = customAssetsDir;
      const { sha, size } = makeAsset(customAssetsDir, "chart.png", "chart content");
      const targetFile = "chart-uploaded.png";
      trackCwdArtifact(targetFile);
      setAgentOutput({
        items: [{ type: "upload_asset", fileName: "chart.png", sha, size, targetFileName: targetFile, url: "https://example.com/chart.png" }],
      });
      mockBranchMissing();

      try {
        await executeScript();
        expect(mockCore.setFailed).not.toHaveBeenCalled();
        const uploadCountCall = mockCore.setOutput.mock.calls.find(call => call[0] === "upload_count");
        expect(uploadCountCall).toBeDefined();
        if (uploadCountCall) expect(uploadCountCall[1]).toBe("1");
      } finally {
        if (fs.existsSync(customAssetsDir)) fs.rmSync(customAssetsDir, { recursive: true, force: true });
      }
    });
  });
});
