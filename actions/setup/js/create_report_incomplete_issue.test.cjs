// @ts-check
import { describe, it, expect, beforeEach, vi } from "vitest";
import { createRequire } from "module";
import fs from "fs";
import os from "os";
import path from "path";

const require = createRequire(import.meta.url);
const promptsDir = path.join(os.tmpdir(), "gh-aw-test-prompts");
fs.mkdirSync(promptsDir, { recursive: true });
fs.writeFileSync(path.join(promptsDir, "missing_tool_issue.md"), "# Incomplete\n\n{incomplete_signals_list}\n");
process.env.GH_AW_PROMPTS_DIR = promptsDir;

const mockCore = {
  info: vi.fn(),
  warning: vi.fn(),
  error: vi.fn(),
  setOutput: vi.fn(),
  setFailed: vi.fn(),
};

const mockGithub = {
  rest: {
    search: {
      issuesAndPullRequests: vi.fn(),
    },
    issues: {
      create: vi.fn(),
      createComment: vi.fn(),
    },
  },
};

const mockContext = {
  repo: { owner: "test-owner", repo: "test-repo" },
};

globalThis.core = mockCore;
globalThis.github = mockGithub;
globalThis.context = mockContext;

const { main } = require("./create_report_incomplete_issue.cjs");

describe("create_report_incomplete_issue.cjs", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGithub.rest.search.issuesAndPullRequests.mockResolvedValue({
      data: { total_count: 0, items: [] },
    });
    mockGithub.rest.issues.create.mockResolvedValue({
      data: { number: 50, html_url: "https://github.com/test-owner/test-repo/issues/50" },
    });
  });

  it("gracefully handles missing incomplete_signals", async () => {
    const handler = await main({});
    const result = await handler({ workflow_name: "Test Workflow", run_url: "https://github.com/test-owner/test-repo/actions/runs/123" });

    expect(result.success).toBe(true);
    expect(mockGithub.rest.issues.create).toHaveBeenCalledWith(
      expect.objectContaining({
        body: expect.stringContaining("**Task completion:** unconfirmed"),
      })
    );
  });

  it("gracefully handles empty incomplete_signals", async () => {
    const handler = await main({});
    const result = await handler({ workflow_name: "Test Workflow", run_url: "https://github.com/test-owner/test-repo/actions/runs/123", incomplete_signals: [] });

    expect(result.success).toBe(true);
    expect(mockGithub.rest.issues.create).toHaveBeenCalledWith(
      expect.objectContaining({
        body: expect.stringContaining("**Task completion:** unconfirmed"),
      })
    );
  });

  it.each([false, true])("excludes agent-supplied reason, details, and timestamp from tracking reports (existing=%s)", async existing => {
    mockGithub.rest.search.issuesAndPullRequests.mockResolvedValue({
      data: { total_count: existing ? 1 : 0, items: existing ? [{ number: 50, html_url: "https://github.com/test-owner/test-repo/issues/50" }] : [] },
    });
    const handler = await main({});
    const result = await handler({
      workflow_name: "Test Workflow",
      run_url: "https://github.com/test-owner/test-repo/actions/runs/123",
      incomplete_signals: [{ reason: "private-reason", details: "private-details", timestamp: "private-timestamp" }],
    });
    expect(result.success).toBe(true);
    const body = (existing ? mockGithub.rest.issues.createComment : mockGithub.rest.issues.create).mock.calls[0][0].body;
    expect(body).toContain("Task completion");
    expect(body).not.toContain("private-");
  });
});
