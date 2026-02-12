import { describe, it, expect, beforeEach, vi, afterEach } from "vitest";
import * as fs from "fs";
import * as path from "path";
import * as os from "os";

// Mock the global objects that GitHub Actions provides
const mockCore = {
  info: vi.fn(),
  debug: vi.fn(),
  warning: vi.fn(),
  summary: {
    addRaw: vi.fn().mockReturnThis(),
    write: vi.fn().mockResolvedValue(undefined),
  },
};

// Set up global mocks before importing the module
globalThis.core = mockCore;

const { generateSafeOutputSummary, writeSafeOutputSummaries } = await import("./safe_output_summary.cjs");

describe("safe_output_summary", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  describe("generateSafeOutputSummary", () => {
    it("should generate summary for successful create_issue", () => {
      const options = {
        type: "create_issue",
        messageIndex: 1,
        success: true,
        result: {
          repo: "owner/repo",
          number: 123,
          url: "https://github.com/owner/repo/issues/123",
          temporaryId: "issue-1",
        },
        message: {
          title: "Test Issue",
          body: "This is a test issue body",
          labels: ["bug", "enhancement"],
        },
      };

      const summary = generateSafeOutputSummary(options);

      expect(summary).toContain("<details>");
      expect(summary).toContain("</details>");
      expect(summary).toContain("✅");
      expect(summary).toContain("Create Issue");
      expect(summary).toContain("Message 1");
      expect(summary).toContain("owner/repo#123");
      expect(summary).toContain("https://github.com/owner/repo/issues/123");
      expect(summary).toContain("issue-1");
      expect(summary).toContain("Test Issue");
      expect(summary).toContain("bug, enhancement");
    });

    it("should generate summary for failed message with error", () => {
      const options = {
        type: "create_project",
        messageIndex: 2,
        success: false,
        result: null,
        message: {
          title: "Test Project",
        },
        error: "Failed to create project: permission denied",
      };

      const summary = generateSafeOutputSummary(options);

      expect(summary).toContain("❌");
      expect(summary).toContain("Failed");
      expect(summary).toContain("Create Project");
      expect(summary).toContain("Message 2");
      expect(summary).toContain("permission denied");
    });

    it("should truncate long body content", () => {
      const longBody = "a".repeat(1000);

      const options = {
        type: "create_discussion",
        messageIndex: 3,
        success: true,
        result: {
          repo: "owner/repo",
          number: 456,
        },
        message: {
          title: "Test Discussion",
          body: longBody,
        },
      };

      const summary = generateSafeOutputSummary(options);

      expect(summary).toContain("Body Preview");
      expect(summary).toContain("...");
      expect(summary.length).toBeLessThan(longBody.length + 1000);
    });

    it("should handle project-specific results", () => {
      const options = {
        type: "create_project",
        messageIndex: 4,
        success: true,
        result: {
          projectUrl: "https://github.com/orgs/owner/projects/123",
        },
        message: {
          title: "Test Project",
        },
      };

      const summary = generateSafeOutputSummary(options);

      expect(summary).toContain("Project URL");
      expect(summary).toContain("https://github.com/orgs/owner/projects/123");
    });
  });

  describe("writeSafeOutputSummaries", () => {
    it("should write summaries for multiple results", async () => {
      const results = [
        {
          type: "create_issue",
          messageIndex: 0,
          success: true,
          result: {
            repo: "owner/repo",
            number: 123,
            url: "https://github.com/owner/repo/issues/123",
          },
        },
        {
          type: "create_project",
          messageIndex: 1,
          success: true,
          result: {
            projectUrl: "https://github.com/orgs/owner/projects/456",
          },
        },
      ];

      const messages = [{ title: "Issue 1", body: "Body 1" }, { title: "Project 1" }];

      await writeSafeOutputSummaries(results, messages);

      expect(mockCore.summary.addRaw).toHaveBeenCalledTimes(1);
      expect(mockCore.summary.write).toHaveBeenCalledTimes(1);
      expect(mockCore.info).toHaveBeenCalledWith("📝 Safe output summaries written to step summary");

      const summaryContent = mockCore.summary.addRaw.mock.calls[0][0];
      expect(summaryContent).toContain("Safe Output Processing Summary");
      expect(summaryContent).toContain("Processed 2 safe-output message(s)");
      expect(summaryContent).toContain("Create Issue");
      expect(summaryContent).toContain("Create Project");
    });

    it("should skip results handled by standalone steps", async () => {
      const results = [
        {
          type: "create_issue",
          messageIndex: 0,
          success: true,
          result: { repo: "owner/repo", number: 123 },
        },
        {
          type: "noop",
          messageIndex: 1,
          success: false,
          skipped: true,
          reason: "Handled by standalone step",
        },
      ];

      const messages = [{ title: "Issue 1" }, { message: "Noop message" }];

      await writeSafeOutputSummaries(results, messages);

      const summaryContent = mockCore.summary.addRaw.mock.calls[0][0];
      expect(summaryContent).toContain("Create Issue");
      expect(summaryContent).not.toContain("Noop");
    });

    it("should handle empty results", async () => {
      await writeSafeOutputSummaries([], []);

      expect(mockCore.summary.addRaw).not.toHaveBeenCalled();
      expect(mockCore.summary.write).not.toHaveBeenCalled();
    });

    it("should handle write failures gracefully", async () => {
      mockCore.summary.write.mockRejectedValueOnce(new Error("Write failed"));

      const results = [
        {
          type: "create_issue",
          messageIndex: 0,
          success: true,
          result: { repo: "owner/repo", number: 123 },
        },
      ];

      const messages = [{ title: "Issue 1" }];

      await writeSafeOutputSummaries(results, messages);

      expect(mockCore.warning).toHaveBeenCalledWith("Failed to write safe output summaries: Write failed");
    });

    it("should log raw .jsonl content when safe outputs file exists", async () => {
      // Create a temporary .jsonl file
      const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "test-safe-outputs-"));
      const jsonlFile = path.join(tempDir, "outputs.jsonl");
      const jsonlContent = '{"type":"create_issue","title":"Test Issue"}\n{"type":"add_comment","body":"Test comment"}';
      fs.writeFileSync(jsonlFile, jsonlContent, "utf8");

      // Set environment variable
      const originalEnv = process.env.GH_AW_SAFE_OUTPUTS;
      process.env.GH_AW_SAFE_OUTPUTS = jsonlFile;

      try {
        const results = [
          {
            type: "create_issue",
            messageIndex: 0,
            success: true,
            result: { repo: "owner/repo", number: 123 },
          },
        ];

        const messages = [{ title: "Issue 1" }];

        await writeSafeOutputSummaries(results, messages);

        // Verify that core.info was called with the raw content
        expect(mockCore.info).toHaveBeenCalledWith("📄 Raw safe-output .jsonl content:");
        expect(mockCore.info).toHaveBeenCalledWith(jsonlContent);
      } finally {
        // Cleanup
        process.env.GH_AW_SAFE_OUTPUTS = originalEnv;
        fs.rmSync(tempDir, { recursive: true, force: true });
      }
    });

    it("should handle missing safe outputs file gracefully", async () => {
      // Set environment variable to a non-existent file
      const originalEnv = process.env.GH_AW_SAFE_OUTPUTS;
      process.env.GH_AW_SAFE_OUTPUTS = "/non/existent/file.jsonl";

      try {
        const results = [
          {
            type: "create_issue",
            messageIndex: 0,
            success: true,
            result: { repo: "owner/repo", number: 123 },
          },
        ];

        const messages = [{ title: "Issue 1" }];

        await writeSafeOutputSummaries(results, messages);

        // Should not throw and should still write summary
        expect(mockCore.summary.write).toHaveBeenCalledTimes(1);
        expect(mockCore.info).toHaveBeenCalledWith("📝 Safe output summaries written to step summary");
      } finally {
        // Cleanup
        process.env.GH_AW_SAFE_OUTPUTS = originalEnv;
      }
    });

    it("should skip logging when safe outputs file is empty", async () => {
      // Create a temporary empty .jsonl file
      const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "test-safe-outputs-"));
      const jsonlFile = path.join(tempDir, "outputs.jsonl");
      fs.writeFileSync(jsonlFile, "", "utf8");

      // Set environment variable
      const originalEnv = process.env.GH_AW_SAFE_OUTPUTS;
      process.env.GH_AW_SAFE_OUTPUTS = jsonlFile;

      try {
        const results = [
          {
            type: "create_issue",
            messageIndex: 0,
            success: true,
            result: { repo: "owner/repo", number: 123 },
          },
        ];

        const messages = [{ title: "Issue 1" }];

        await writeSafeOutputSummaries(results, messages);

        // Should not log empty content
        const infoCalls = mockCore.info.mock.calls.map(call => call[0]);
        expect(infoCalls).not.toContain("📄 Raw safe-output .jsonl content:");
      } finally {
        // Cleanup
        process.env.GH_AW_SAFE_OUTPUTS = originalEnv;
        fs.rmSync(tempDir, { recursive: true, force: true });
      }
    });
  });
});
