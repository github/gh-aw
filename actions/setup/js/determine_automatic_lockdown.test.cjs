import { describe, it, expect, beforeEach, vi } from "vitest";

describe("determine_automatic_lockdown", () => {
  let mockContext;
  let mockGithub;
  let mockCore;
  let determineAutomaticLockdown;

  beforeEach(async () => {
    vi.resetModules();

    // Setup mock context
    mockContext = {
      repo: {
        owner: "test-owner",
        repo: "test-repo",
      },
    };

    // Setup mock GitHub API
    mockGithub = {
      rest: {
        repos: {
          get: vi.fn(),
        },
      },
    };

    // Setup mock core
    mockCore = {
      info: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
      setOutput: vi.fn(),
    };

    // Reset process.env
    delete process.env.GH_AW_GITHUB_TOKEN;

    // Import the module
    determineAutomaticLockdown = (await import("./determine_automatic_lockdown.cjs")).default;
  });

  it("should set lockdown to true for public repository with GH_AW_GITHUB_TOKEN", async () => {
    process.env.GH_AW_GITHUB_TOKEN = "ghp_test_token";

    mockGithub.rest.repos.get.mockResolvedValue({
      data: {
        private: false,
        visibility: "public",
      },
    });

    await determineAutomaticLockdown(mockGithub, mockContext, mockCore);

    expect(mockGithub.rest.repos.get).toHaveBeenCalledWith({
      owner: "test-owner",
      repo: "test-repo",
    });
    expect(mockCore.info).toHaveBeenCalledWith("GH_AW_GITHUB_TOKEN configured: true");
    expect(mockCore.setOutput).toHaveBeenCalledWith("lockdown", "true");
    expect(mockCore.setOutput).toHaveBeenCalledWith("visibility", "public");
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("GitHub MCP lockdown mode enabled"));
  });

  it("should set lockdown to false for public repository without GH_AW_GITHUB_TOKEN", async () => {
    // GH_AW_GITHUB_TOKEN not set

    mockGithub.rest.repos.get.mockResolvedValue({
      data: {
        private: false,
        visibility: "public",
      },
    });

    await determineAutomaticLockdown(mockGithub, mockContext, mockCore);

    expect(mockGithub.rest.repos.get).toHaveBeenCalledWith({
      owner: "test-owner",
      repo: "test-repo",
    });
    expect(mockCore.info).toHaveBeenCalledWith("GH_AW_GITHUB_TOKEN configured: false");
    expect(mockCore.setOutput).toHaveBeenCalledWith("lockdown", "false");
    expect(mockCore.setOutput).toHaveBeenCalledWith("visibility", "public");
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Automatic lockdown mode disabled for public repository (GH_AW_GITHUB_TOKEN not configured)"));
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("To enable lockdown mode for enhanced security"));
  });

  it("should set lockdown to false for private repository", async () => {
    mockGithub.rest.repos.get.mockResolvedValue({
      data: {
        private: true,
        visibility: "private",
      },
    });

    await determineAutomaticLockdown(mockGithub, mockContext, mockCore);

    expect(mockGithub.rest.repos.get).toHaveBeenCalledWith({
      owner: "test-owner",
      repo: "test-repo",
    });
    expect(mockCore.setOutput).toHaveBeenCalledWith("lockdown", "false");
    expect(mockCore.setOutput).toHaveBeenCalledWith("visibility", "private");
    expect(mockCore.warning).not.toHaveBeenCalled();
  });

  it("should set lockdown to false for internal repository", async () => {
    mockGithub.rest.repos.get.mockResolvedValue({
      data: {
        private: true,
        visibility: "internal",
      },
    });

    await determineAutomaticLockdown(mockGithub, mockContext, mockCore);

    expect(mockCore.setOutput).toHaveBeenCalledWith("lockdown", "false");
    expect(mockCore.setOutput).toHaveBeenCalledWith("visibility", "internal");
  });

  it("should handle API failure and default to lockdown mode", async () => {
    const error = new Error("API request failed");
    mockGithub.rest.repos.get.mockRejectedValue(error);

    await determineAutomaticLockdown(mockGithub, mockContext, mockCore);

    expect(mockCore.error).toHaveBeenCalledWith("Failed to determine automatic lockdown mode: API request failed");
    expect(mockCore.setOutput).toHaveBeenCalledWith("lockdown", "true");
    expect(mockCore.setOutput).toHaveBeenCalledWith("visibility", "unknown");
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Failed to determine repository visibility"));
  });

  it("should infer visibility from private field when visibility field is missing", async () => {
    // Public repo without GH_AW_GITHUB_TOKEN
    mockGithub.rest.repos.get.mockResolvedValue({
      data: {
        private: false,
        // visibility field not present
      },
    });

    await determineAutomaticLockdown(mockGithub, mockContext, mockCore);

    expect(mockCore.setOutput).toHaveBeenCalledWith("lockdown", "false");
    expect(mockCore.setOutput).toHaveBeenCalledWith("visibility", "public");
  });

  it("should log appropriate info messages for public repo with token", async () => {
    process.env.GH_AW_GITHUB_TOKEN = "ghp_test_token";

    mockGithub.rest.repos.get.mockResolvedValue({
      data: {
        private: false,
        visibility: "public",
      },
    });

    await determineAutomaticLockdown(mockGithub, mockContext, mockCore);

    expect(mockCore.info).toHaveBeenCalledWith("Determining automatic lockdown mode for GitHub MCP server");
    expect(mockCore.info).toHaveBeenCalledWith("Checking repository: test-owner/test-repo");
    expect(mockCore.info).toHaveBeenCalledWith("Repository visibility: public");
    expect(mockCore.info).toHaveBeenCalledWith("Repository is private: false");
    expect(mockCore.info).toHaveBeenCalledWith("GH_AW_GITHUB_TOKEN configured: true");
    expect(mockCore.info).toHaveBeenCalledWith("Automatic lockdown mode determined: true");
    expect(mockCore.info).toHaveBeenCalledWith("Automatic lockdown mode enabled for public repository with GH_AW_GITHUB_TOKEN");
  });
});
