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
  mockGithub = { rest: { issues: { get: vi.fn() } }, graphql: vi.fn() },
  mockContext = { eventName: "workflow_dispatch", runId: 12345, repo: { owner: "testowner", repo: "testrepo" }, payload: { repository: { html_url: "https://github.com/testowner/testrepo" } } };
((global.core = mockCore),
  (global.github = mockGithub),
  (global.context = mockContext),
  describe("link_sub_issue.cjs", () => {
    let tempDir, handler;
    beforeEach(async () => {
      vi.clearAllMocks();
      tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "link-sub-issue-test-"));
      // Load the handler module
      const { main } = require(path.join(process.cwd(), "link_sub_issue.cjs"));
      // Create handler with default config
      handler = await main({
        max: 5,
        target: "*",
        parent_required_labels: [],
        parent_title_prefix: "",
        sub_required_labels: [],
        sub_title_prefix: "",
      });
    });
    afterEach(() => {
      if (tempDir && fs.existsSync(tempDir)) {
        fs.rmSync(tempDir, { recursive: true });
      }
    });
    it("should succeed (skip) when sub-issue already has a different parent", async () => {
      // Previously this returned failure; it now returns success/skipped to be idempotent —
      // if a sub-issue is already tracked in any group, we warn and move on rather than
      // failing the safe-output job.
      const message = { type: "link_sub_issue", parent_issue_number: 100, sub_issue_number: 50 };
      mockGithub.rest.issues.get
        .mockResolvedValueOnce({ data: { number: 100, title: "Parent Issue", node_id: "I_parent_100", labels: [] } })
        .mockResolvedValueOnce({ data: { number: 50, title: "Sub Issue", node_id: "I_sub_50", labels: [] } });
      mockGithub.graphql.mockResolvedValueOnce({ repository: { issue: { parent: { number: 99, title: "Existing Parent Issue" } } } });

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(result.skipped).toBe(true);
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining('Sub-issue #50 is already a sub-issue of #99 ("Existing Parent Issue"). Skipping.'));
      expect(mockGithub.graphql).toHaveBeenCalledTimes(1);
      expect(mockGithub.graphql).toHaveBeenCalledWith(expect.stringContaining("parent {"), expect.any(Object));
    });
    it("should succeed when sub-issue is already linked to the requested parent", async () => {
      const message = { type: "link_sub_issue", parent_issue_number: 99, sub_issue_number: 50 };
      mockGithub.rest.issues.get
        .mockResolvedValueOnce({ data: { number: 99, title: "Parent Issue", node_id: "I_parent_99", labels: [] } })
        .mockResolvedValueOnce({ data: { number: 50, title: "Sub Issue", node_id: "I_sub_50", labels: [] } });
      mockGithub.graphql.mockResolvedValueOnce({ repository: { issue: { parent: { number: 99, title: "Parent Issue" } } } });

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(result.skipped).toBe(true);
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining('Sub-issue #50 is already a sub-issue of #99 ("Parent Issue"). Skipping.'));
      expect(mockGithub.graphql).toHaveBeenCalledTimes(1);
      expect(mockGithub.graphql).toHaveBeenCalledWith(expect.stringContaining("parent {"), expect.any(Object));
      expect(mockGithub.graphql).not.toHaveBeenCalledWith(expect.stringContaining("addSubIssue"), expect.any(Object));
    });
    it("should proceed with linking when sub-issue has no parent", async () => {
      const message = { type: "link_sub_issue", parent_issue_number: 100, sub_issue_number: 50 };
      mockGithub.rest.issues.get
        .mockResolvedValueOnce({ data: { number: 100, title: "Parent Issue", node_id: "I_parent_100", labels: [] } })
        .mockResolvedValueOnce({ data: { number: 50, title: "Sub Issue", node_id: "I_sub_50", labels: [] } });
      mockGithub.graphql.mockResolvedValueOnce({ repository: { issue: { parent: null } } }).mockResolvedValueOnce({ addSubIssue: { issue: { id: "I_parent_100", number: 100 }, subIssue: { id: "I_sub_50", number: 50 } } });

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(mockCore.warning).not.toHaveBeenCalledWith(expect.stringContaining("already a sub-issue"));
      expect(mockGithub.graphql).toHaveBeenCalledTimes(2);
      expect(mockGithub.graphql).toHaveBeenLastCalledWith(expect.stringContaining("addSubIssue"), expect.any(Object));
      expect(mockCore.info).toHaveBeenCalledWith("Successfully linked issue #50 as sub-issue of #100");
    });
    it("should continue with linking if parent check query fails", async () => {
      const message = { type: "link_sub_issue", parent_issue_number: 100, sub_issue_number: 50 };
      mockGithub.rest.issues.get
        .mockResolvedValueOnce({ data: { number: 100, title: "Parent Issue", node_id: "I_parent_100", labels: [] } })
        .mockResolvedValueOnce({ data: { number: 50, title: "Sub Issue", node_id: "I_sub_50", labels: [] } });
      mockGithub.graphql.mockRejectedValueOnce(new Error("Field 'parent' doesn't exist on type 'Issue'")).mockResolvedValueOnce({ addSubIssue: { issue: { id: "I_parent_100", number: 100 }, subIssue: { id: "I_sub_50", number: 50 } } });

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Could not check if sub-issue #50 has a parent"));
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Proceeding with link attempt"));
      expect(mockGithub.graphql).toHaveBeenCalledTimes(2);
      expect(mockGithub.graphql).toHaveBeenLastCalledWith(expect.stringContaining("addSubIssue"), expect.any(Object));
      expect(mockCore.info).toHaveBeenCalledWith("Successfully linked issue #50 as sub-issue of #100");
    });
    it("should succeed (skip) when mutation fails because sub-issue is already a sub-issue", async () => {
      const message = { type: "link_sub_issue", parent_issue_number: 100, sub_issue_number: 50 };
      mockGithub.rest.issues.get
        .mockResolvedValueOnce({ data: { number: 100, title: "Parent Issue", node_id: "I_parent_100", labels: [] } })
        .mockResolvedValueOnce({ data: { number: 50, title: "Sub Issue", node_id: "I_sub_50", labels: [] } });
      // Pre-flight check says no parent (e.g. parent field not yet populated), but mutation fails
      mockGithub.graphql.mockResolvedValueOnce({ repository: { issue: { parent: null } } }).mockRejectedValueOnce(new Error("Issue is already a sub-issue of the specified issue."));

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(result.skipped).toBe(true);
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Sub-issue #50 is already linked as a sub-issue"));
      expect(mockGithub.graphql).toHaveBeenCalledTimes(2);
    });
    it("should handle max count limit", async () => {
      // Create handler with max=1
      const limitedHandler = await require(path.join(process.cwd(), "link_sub_issue.cjs")).main({ max: 1, target: "*" });

      const message1 = { type: "link_sub_issue", parent_issue_number: 100, sub_issue_number: 50 };
      const message2 = { type: "link_sub_issue", parent_issue_number: 100, sub_issue_number: 51 };

      mockGithub.rest.issues.get.mockResolvedValue({ data: { number: 100, title: "Parent Issue", node_id: "I_parent_100", labels: [] } }).mockResolvedValue({ data: { number: 50, title: "Sub Issue 50", node_id: "I_sub_50", labels: [] } });
      mockGithub.graphql.mockResolvedValueOnce({ repository: { issue: { parent: null } } }).mockResolvedValueOnce({ addSubIssue: { issue: { id: "I_parent_100", number: 100 }, subIssue: { id: "I_sub_50", number: 50 } } });

      const result1 = await limitedHandler(message1, {});
      const result2 = await limitedHandler(message2, {});

      expect(result1.success).toBe(true);
      expect(result2.success).toBe(false);
      expect(result2.error).toContain("Max count of 1 reached");
    });
    it("should defer when parent is unresolved temporary ID", async () => {
      const message = { type: "link_sub_issue", parent_issue_number: "aw_12345678", sub_issue_number: 50 };

      // Empty temp ID map - temporary ID is unresolved
      const result = await handler(message, {});

      expect(result.success).toBe(false);
      expect(result.deferred).toBe(true);
      expect(result.error).toContain("Unresolved temporary IDs");
      expect(result.error).toContain("parent: aw_12345678");
    });
    it("should defer when sub-issue is unresolved temporary ID", async () => {
      const message = { type: "link_sub_issue", parent_issue_number: 100, sub_issue_number: "aw_456789ab" };

      // Empty temp ID map - temporary ID is unresolved
      const result = await handler(message, {});

      expect(result.success).toBe(false);
      expect(result.deferred).toBe(true);
      expect(result.error).toContain("Unresolved temporary IDs");
      expect(result.error).toContain("sub: aw_456789ab");
    });
    it("should succeed when temporary IDs are resolved", async () => {
      const message = { type: "link_sub_issue", parent_issue_number: "aw_12345678", sub_issue_number: "aw_456789ab" };

      // Provide resolved temp IDs
      const resolvedIds = {
        aw_12345678: { repo: "testowner/testrepo", number: 100 },
        aw_456789ab: { repo: "testowner/testrepo", number: 50 },
      };

      mockGithub.rest.issues.get
        .mockResolvedValueOnce({ data: { number: 100, title: "Parent Issue", node_id: "I_parent_100", labels: [] } })
        .mockResolvedValueOnce({ data: { number: 50, title: "Sub Issue", node_id: "I_sub_50", labels: [] } });
      mockGithub.graphql.mockResolvedValueOnce({ repository: { issue: { parent: null } } }).mockResolvedValueOnce({ addSubIssue: { issue: { id: "I_parent_100", number: 100 }, subIssue: { id: "I_sub_50", number: 50 } } });

      const result = await handler(message, resolvedIds);

      expect(result.success).toBe(true);
      expect(result.deferred).toBeUndefined();
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Resolved parent temporary ID"));
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Resolved sub-issue temporary ID"));
    });

    it("should fail when parent and sub temporary IDs resolve to different repos", async () => {
      const message = { type: "link_sub_issue", parent_issue_number: "aw_12345678", sub_issue_number: "aw_456789ab" };

      const resolvedIds = {
        aw_12345678: { repo: "org-a/repo-a", number: 100 },
        aw_456789ab: { repo: "org-b/repo-b", number: 50 },
      };

      const result = await handler(message, resolvedIds);

      expect(result.success).toBe(false);
      expect(result.error).toContain("must be in the same repository");
      expect(mockGithub.rest.issues.get).not.toHaveBeenCalled();
      expect(mockGithub.graphql).not.toHaveBeenCalled();
    });

    it("should reject a cross-repository sub-issue for a fixed parent target", async () => {
      const { main } = require(path.join(process.cwd(), "link_sub_issue.cjs"));
      const fixedTargetHandler = await main({ max: 5, target: "100" });
      const resolvedIds = {
        aw_456789ab: { repo: "other-org/other-repo", number: 50 },
      };

      const result = await fixedTargetHandler({ type: "link_sub_issue", parent_issue_number: 999, sub_issue_number: "aw_456789ab" }, resolvedIds);

      expect(result.success).toBe(false);
      expect(result.error).toContain("must be in the same repository");
      expect(mockGithub.rest.issues.get).not.toHaveBeenCalled();
      expect(mockGithub.graphql).not.toHaveBeenCalled();
    });

    it("should reject when a fixed parent target equals the sub-issue, even though the raw model parent differs", async () => {
      const { main } = require(path.join(process.cwd(), "link_sub_issue.cjs"));
      // The configured target (#50) is the same as the requested sub-issue (#50), so applying
      // the target would create a self-link even though the model-provided parent (999) differs.
      const fixedTargetHandler = await main({ max: 5, target: "50" });

      const result = await fixedTargetHandler({ type: "link_sub_issue", parent_issue_number: 999, sub_issue_number: 50 }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("must be different");
      expect(mockGithub.rest.issues.get).not.toHaveBeenCalled();
      expect(mockGithub.graphql).not.toHaveBeenCalled();
    });

    it("should use target-repo config as default for issue resolution", async () => {
      const { main } = require(path.join(process.cwd(), "link_sub_issue.cjs"));
      const handlerWithTarget = await main({
        max: 5,
        "target-repo": "external-org/external-repo",
        target: "*",
      });

      mockGithub.rest.issues.get
        .mockResolvedValueOnce({ data: { number: 100, title: "Parent Issue", node_id: "I_parent_100", labels: [] } })
        .mockResolvedValueOnce({ data: { number: 50, title: "Sub Issue", node_id: "I_sub_50", labels: [] } });
      mockGithub.graphql.mockResolvedValueOnce({ repository: { issue: { parent: null } } }).mockResolvedValueOnce({ addSubIssue: { issue: { id: "I_parent_100", number: 100 }, subIssue: { id: "I_sub_50", number: 50 } } });

      const result = await handlerWithTarget({ type: "link_sub_issue", parent_issue_number: 100, sub_issue_number: 50 }, {});

      expect(result.success).toBe(true);
      expect(mockGithub.rest.issues.get).toHaveBeenCalledWith(expect.objectContaining({ owner: "external-org", repo: "external-repo", issue_number: 100 }));
    });

    it("should use context.repo as default when no target-repo configured", async () => {
      mockGithub.rest.issues.get
        .mockResolvedValueOnce({ data: { number: 100, title: "Parent Issue", node_id: "I_parent_100", labels: [] } })
        .mockResolvedValueOnce({ data: { number: 50, title: "Sub Issue", node_id: "I_sub_50", labels: [] } });
      mockGithub.graphql.mockResolvedValueOnce({ repository: { issue: { parent: null } } }).mockResolvedValueOnce({ addSubIssue: { issue: { id: "I_parent_100", number: 100 }, subIssue: { id: "I_sub_50", number: 50 } } });

      const result = await handler({ type: "link_sub_issue", parent_issue_number: 100, sub_issue_number: 50 }, {});

      expect(result.success).toBe(true);
      expect(mockGithub.rest.issues.get).toHaveBeenCalledWith(expect.objectContaining({ owner: "testowner", repo: "testrepo", issue_number: 100 }));
    });

    it("should reject item.repo not in allowed_repos", async () => {
      const { main } = require(path.join(process.cwd(), "link_sub_issue.cjs"));
      const handlerWithAllowed = await main({
        max: 5,
        "target-repo": "default-org/default-repo",
        allowed_repos: ["default-org/default-repo"],
      });

      const result = await handlerWithAllowed({ type: "link_sub_issue", parent_issue_number: 100, sub_issue_number: 50, repo: "evil-org/evil-repo" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toMatch(/not allowed|evil-org\/evil-repo/);
    });
  }));
