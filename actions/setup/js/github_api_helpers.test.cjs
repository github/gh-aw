import { describe, it, expect, beforeEach, vi } from "vitest";

const mockCore = {
  info: vi.fn(),
};

global.core = mockCore;

describe("github_api_helpers.cjs", () => {
  let getFileContent;
  let mockGithub;

  beforeEach(async () => {
    vi.clearAllMocks();

    mockGithub = {
      rest: {
        repos: {
          getContent: vi.fn(),
        },
      },
    };

    // Dynamically import the module
    const module = await import("./github_api_helpers.cjs");
    getFileContent = module.getFileContent;
  });

  describe("getFileContent", () => {
    it("should fetch and decode base64 file content", async () => {
      const fileContent = "Hello, World!";
      mockGithub.rest.repos.getContent.mockResolvedValueOnce({
        data: {
          type: "file",
          encoding: "base64",
          content: Buffer.from(fileContent).toString("base64"),
        },
      });

      const result = await getFileContent(mockGithub, "owner", "repo", "file.txt", "main");

      expect(result).toBe(fileContent);
      expect(mockGithub.rest.repos.getContent).toHaveBeenCalledWith({
        owner: "owner",
        repo: "repo",
        path: "file.txt",
        ref: "main",
      });
    });

    it("should handle non-base64 content", async () => {
      const fileContent = "Plain text content";
      mockGithub.rest.repos.getContent.mockResolvedValueOnce({
        data: {
          type: "file",
          encoding: "utf-8",
          content: fileContent,
        },
      });

      const result = await getFileContent(mockGithub, "owner", "repo", "file.txt", "main");

      expect(result).toBe(fileContent);
    });

    it("should return null for directory paths", async () => {
      mockGithub.rest.repos.getContent.mockResolvedValueOnce({
        data: [
          { name: "file1.txt", type: "file" },
          { name: "file2.txt", type: "file" },
        ],
      });

      const result = await getFileContent(mockGithub, "owner", "repo", "directory", "main");

      expect(result).toBeNull();
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("is a directory"));
    });

    it("should return null for non-file types", async () => {
      mockGithub.rest.repos.getContent.mockResolvedValueOnce({
        data: {
          type: "symlink",
          encoding: "base64",
          content: "link-content",
        },
      });

      const result = await getFileContent(mockGithub, "owner", "repo", "symlink.txt", "main");

      expect(result).toBeNull();
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("is not a file"));
    });

    it("should handle API errors gracefully", async () => {
      mockGithub.rest.repos.getContent.mockRejectedValueOnce(new Error("API error"));

      const result = await getFileContent(mockGithub, "owner", "repo", "file.txt", "main");

      expect(result).toBeNull();
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Could not fetch content"));
    });

    it("should handle missing content field", async () => {
      mockGithub.rest.repos.getContent.mockResolvedValueOnce({
        data: {
          type: "file",
          encoding: "base64",
          // content field is missing
        },
      });

      const result = await getFileContent(mockGithub, "owner", "repo", "file.txt", "main");

      expect(result).toBeNull();
    });
  });
});
