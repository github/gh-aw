import { describe, it, expect, beforeEach, vi } from "vitest";

// Mock the global objects that GitHub Actions provides
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
    write: vi.fn().mockResolvedValue(),
  },
};

// Set up global mocks expected by the scripts we import
global.core = mockCore;

describe("update_issue.cjs - generator payload", () => {
  beforeEach(async () => {
    vi.clearAllMocks();
    vi.resetModules();
  });

  it("defaults body updates to append (preserve original issue)", async () => {
    const updateIssueModule = await import("./update_issue.cjs");

    const { success, data } = updateIssueModule.buildIssueUpdateData(
      {
        // Representative of a generator updating the triggering issue.
        body: "## Run status\n\n**Status:** Ready for review\n",
      },
      {}
    );

    expect(success).toBe(true);

    // The handler should keep the raw body + operation so it can append + add footer.
    expect(data._operation).toBe("append");
    expect(data._rawBody).toContain("## Run status");

    // The actual API body is computed later (after fetching current issue body).
    expect(data.body).toBeUndefined();
  });

  it("maps safe-outputs 'status' to GitHub API 'state'", async () => {
    const updateIssueModule = await import("./update_issue.cjs");

    const { success, data } = updateIssueModule.buildIssueUpdateData(
      {
        status: "closed",
      },
      {}
    );

    expect(success).toBe(true);
    expect(data.state).toBe("closed");
  });

  it("respects explicit operation when provided", async () => {
    const updateIssueModule = await import("./update_issue.cjs");

    const { success, data } = updateIssueModule.buildIssueUpdateData(
      {
        operation: "replace-island",
        body: "Short status island for this run.",
      },
      {}
    );

    expect(success).toBe(true);
    expect(data._operation).toBe("replace-island");
    expect(data._rawBody).toBe("Short status island for this run.");
  });
});
