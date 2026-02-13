import { describe, it, expect, beforeEach, vi } from "vitest";

describe("validate_lockdown_requirements", () => {
  let mockCore;
  let validateLockdownRequirements;

  beforeEach(async () => {
    vi.resetModules();

    // Setup mock core
    mockCore = {
      info: vi.fn(),
      setFailed: vi.fn(),
    };

    // Reset process.env
    delete process.env.GITHUB_MCP_LOCKDOWN_EXPLICIT;
    delete process.env.GH_AW_GITHUB_TOKEN;

    // Import the module
    validateLockdownRequirements = (await import("./validate_lockdown_requirements.cjs")).default;
  });

  it("should skip validation when lockdown is not explicitly enabled", () => {
    // GITHUB_MCP_LOCKDOWN_EXPLICIT not set

    validateLockdownRequirements(mockCore);

    expect(mockCore.info).toHaveBeenCalledWith("Lockdown mode not explicitly enabled, skipping validation");
    expect(mockCore.setFailed).not.toHaveBeenCalled();
  });

  it("should pass validation when lockdown is enabled and GH_AW_GITHUB_TOKEN is configured", () => {
    process.env.GITHUB_MCP_LOCKDOWN_EXPLICIT = "true";
    process.env.GH_AW_GITHUB_TOKEN = "ghp_test_token";

    validateLockdownRequirements(mockCore);

    expect(mockCore.info).toHaveBeenCalledWith("Lockdown mode is explicitly enabled, validating requirements...");
    expect(mockCore.info).toHaveBeenCalledWith("✓ Lockdown mode requirements validated: GH_AW_GITHUB_TOKEN is configured");
    expect(mockCore.setFailed).not.toHaveBeenCalled();
  });

  it("should fail when lockdown is enabled but GH_AW_GITHUB_TOKEN is not configured", () => {
    process.env.GITHUB_MCP_LOCKDOWN_EXPLICIT = "true";
    // GH_AW_GITHUB_TOKEN not set

    expect(() => {
      validateLockdownRequirements(mockCore);
    }).toThrow("Lockdown mode is enabled");

    expect(mockCore.info).toHaveBeenCalledWith("Lockdown mode is explicitly enabled, validating requirements...");
    expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Lockdown mode is enabled (lockdown: true) but GH_AW_GITHUB_TOKEN is not configured"));
    expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("gh aw secrets set GH_AW_GITHUB_TOKEN"));
  });

  it("should include documentation link in error message", () => {
    process.env.GITHUB_MCP_LOCKDOWN_EXPLICIT = "true";
    // GH_AW_GITHUB_TOKEN not set

    expect(() => {
      validateLockdownRequirements(mockCore);
    }).toThrow();

    expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("https://github.com/github/gh-aw/blob/main/docs/src/content/docs/reference/auth.mdx#gh_aw_github_token"));
  });

  it("should handle empty string GH_AW_GITHUB_TOKEN as not configured", () => {
    process.env.GITHUB_MCP_LOCKDOWN_EXPLICIT = "true";
    process.env.GH_AW_GITHUB_TOKEN = "";

    expect(() => {
      validateLockdownRequirements(mockCore);
    }).toThrow("Lockdown mode is enabled");

    expect(mockCore.setFailed).toHaveBeenCalled();
  });

  it("should skip validation when GITHUB_MCP_LOCKDOWN_EXPLICIT is false", () => {
    process.env.GITHUB_MCP_LOCKDOWN_EXPLICIT = "false";
    // GH_AW_GITHUB_TOKEN not set

    validateLockdownRequirements(mockCore);

    expect(mockCore.info).toHaveBeenCalledWith("Lockdown mode not explicitly enabled, skipping validation");
    expect(mockCore.setFailed).not.toHaveBeenCalled();
  });
});
