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
    it("should skip sub-issue that already has a parent", async () => {
      const message = { type: "link_sub_issue", parent_issue_number: 100, sub_issue_number: 50 };
      mockGithub.rest.issues.get
        .mockResolvedValueOnce({ data: { number: 100, title: "Parent Issue", node_id: "I_parent_100", labels: [] } })
        .mockResolvedValueOnce({ data: { number: 50, title: "Sub Issue", node_id: "I_sub_50", labels: [] } });
      mockGithub.graphql.mockResolvedValueOnce({ repository: { issue: { parent: { number: 99, title: "Existing Parent Issue" } } } });

      const result = await handler(message, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("Sub-issue is already a sub-issue of #99");
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining('Sub-issue #50 is already a sub-issue of #99 ("Existing Parent Issue"). Skipping.'));
      expect(mockGithub.graphql).toHaveBeenCalledTimes(1);
      expect(mockGithub.graphql).toHaveBeenCalledWith(expect.stringContaining("parent {"), expect.any(Object));
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
    it("should handle max count limit", async () => {
      // Create handler with max=1
      const limitedHandler = await require(path.join(process.cwd(), "link_sub_issue.cjs")).main({ max: 1 });

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
      const message = { type: "link_sub_issue", parent_issue_number: "aw_123456789abc", sub_issue_number: 50 };

      // Empty temp ID map - temporary ID is unresolved
      const result = await handler(message, {});

      expect(result.success).toBe(false);
      expect(result.deferred).toBe(true);
      expect(result.error).toContain("Unresolved temporary IDs");
      expect(result.error).toContain("parent: aw_123456789abc");
    });
    it("should defer when sub-issue is unresolved temporary ID", async () => {
      const message = { type: "link_sub_issue", parent_issue_number: 100, sub_issue_number: "aw_456789abcdef" };

      // Empty temp ID map - temporary ID is unresolved
      const result = await handler(message, {});

      expect(result.success).toBe(false);
      expect(result.deferred).toBe(true);
      expect(result.error).toContain("Unresolved temporary IDs");
      expect(result.error).toContain("sub: aw_456789abcdef");
    });
    it("should succeed when temporary IDs are resolved", async () => {
      const message = { type: "link_sub_issue", parent_issue_number: "aw_123456789abc", sub_issue_number: "aw_456789abcdef" };

      // Provide resolved temp IDs
      const resolvedIds = {
        aw_123456789abc: { repo: "testowner/testrepo", number: 100 },
        aw_456789abcdef: { repo: "testowner/testrepo", number: 50 },
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
      const message = { type: "link_sub_issue", parent_issue_number: "aw_123456789abc", sub_issue_number: "aw_456789abcdef" };

      const resolvedIds = {
        aw_123456789abc: { repo: "org-a/repo-a", number: 100 },
        aw_456789abcdef: { repo: "org-b/repo-b", number: 50 },
      };

      const result = await handler(message, resolvedIds);

      expect(result.success).toBe(false);
      expect(result.error).toContain("must be in the same repository");
      expect(mockGithub.rest.issues.get).not.toHaveBeenCalled();
      expect(mockGithub.graphql).not.toHaveBeenCalled();
    });
  }));
