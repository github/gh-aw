import { describe, it, expect, beforeEach, vi } from "vitest";
import fs from "fs";
import path from "path";

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
  isDebug: vi.fn().mockReturnValue(false),
  getIDToken: vi.fn(),
  toPlatformPath: vi.fn(),
  toPosixPath: vi.fn(),
  toWin32Path: vi.fn(),
  summary: { addRaw: vi.fn().mockReturnThis(), write: vi.fn().mockResolvedValue() },
};

const mockGithub = { rest: { repos: { getCollaboratorPermissionLevel: vi.fn() } } };

const mockContext = { actor: "testuser", repo: { owner: "testowner", repo: "testrepo" } };

global.core = mockCore;
global.github = mockGithub;
global.context = mockContext;

describe("check_team_member.cjs", () => {
  let checkTeamMemberScript;

  beforeEach(() => {
    vi.clearAllMocks();
    global.context.actor = "testuser";
    global.context.repo = { owner: "testowner", repo: "testrepo" };
    const scriptPath = path.join(process.cwd(), "check_team_member.cjs");
    checkTeamMemberScript = fs.readFileSync(scriptPath, "utf8");
  });

  it("should set is_team_member to true for admin permission", async () => {
    mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValue({ data: { permission: "admin" } });
    await eval(`(async () => { ${checkTeamMemberScript}; await main(); })()`);
    expect(mockGithub.rest.repos.getCollaboratorPermissionLevel).toHaveBeenCalledWith({ owner: "testowner", repo: "testrepo", username: "testuser" });
    expect(mockCore.info).toHaveBeenCalledWith("Checking if user 'testuser' is admin or maintainer of testowner/testrepo");
    expect(mockCore.info).toHaveBeenCalledWith("Repository permission level: admin");
    expect(mockCore.info).toHaveBeenCalledWith("User has admin access to repository");
    expect(mockCore.setOutput).toHaveBeenCalledWith("is_team_member", "true");
  });

  it("should set is_team_member to true for maintain permission", async () => {
    mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValue({ data: { permission: "maintain" } });
    await eval(`(async () => { ${checkTeamMemberScript}; await main(); })()`);
    expect(mockGithub.rest.repos.getCollaboratorPermissionLevel).toHaveBeenCalledWith({ owner: "testowner", repo: "testrepo", username: "testuser" });
    expect(mockCore.info).toHaveBeenCalledWith("Checking if user 'testuser' is admin or maintainer of testowner/testrepo");
    expect(mockCore.info).toHaveBeenCalledWith("Repository permission level: maintain");
    expect(mockCore.info).toHaveBeenCalledWith("User has maintain access to repository");
    expect(mockCore.setOutput).toHaveBeenCalledWith("is_team_member", "true");
  });

  it("should set is_team_member to false for write permission", async () => {
    mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValue({ data: { permission: "write" } });
    await eval(`(async () => { ${checkTeamMemberScript}; await main(); })()`);
    expect(mockGithub.rest.repos.getCollaboratorPermissionLevel).toHaveBeenCalledWith({ owner: "testowner", repo: "testrepo", username: "testuser" });
    expect(mockCore.info).toHaveBeenCalledWith("Checking if user 'testuser' is admin or maintainer of testowner/testrepo");
    expect(mockCore.info).toHaveBeenCalledWith("Repository permission level: write");
    expect(mockCore.setOutput).toHaveBeenCalledWith("is_team_member", "false");
  });

  it("should set is_team_member to false for read permission", async () => {
    mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValue({ data: { permission: "read" } });
    await eval(`(async () => { ${checkTeamMemberScript}; await main(); })()`);
    expect(mockGithub.rest.repos.getCollaboratorPermissionLevel).toHaveBeenCalledWith({ owner: "testowner", repo: "testrepo", username: "testuser" });
    expect(mockCore.info).toHaveBeenCalledWith("Checking if user 'testuser' is admin or maintainer of testowner/testrepo");
    expect(mockCore.info).toHaveBeenCalledWith("Repository permission level: read");
    expect(mockCore.setOutput).toHaveBeenCalledWith("is_team_member", "false");
  });

  it("should set is_team_member to false for none permission", async () => {
    mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValue({ data: { permission: "none" } });
    await eval(`(async () => { ${checkTeamMemberScript}; await main(); })()`);
    expect(mockGithub.rest.repos.getCollaboratorPermissionLevel).toHaveBeenCalledWith({ owner: "testowner", repo: "testrepo", username: "testuser" });
    expect(mockCore.info).toHaveBeenCalledWith("Checking if user 'testuser' is admin or maintainer of testowner/testrepo");
    expect(mockCore.info).toHaveBeenCalledWith("Repository permission level: none");
    expect(mockCore.setOutput).toHaveBeenCalledWith("is_team_member", "false");
  });

  it("should handle API errors and set is_team_member to false", async () => {
    const apiError = new Error("API Error: Not Found");
    mockGithub.rest.repos.getCollaboratorPermissionLevel.mockRejectedValue(apiError);
    await eval(`(async () => { ${checkTeamMemberScript}; await main(); })()`);
    expect(mockGithub.rest.repos.getCollaboratorPermissionLevel).toHaveBeenCalledWith({ owner: "testowner", repo: "testrepo", username: "testuser" });
    expect(mockCore.info).toHaveBeenCalledWith("Checking if user 'testuser' is admin or maintainer of testowner/testrepo");
    expect(mockCore.warning).toHaveBeenCalledWith("Repository permission check failed: API Error: Not Found");
    expect(mockCore.setOutput).toHaveBeenCalledWith("is_team_member", "false");
  });

  it("should handle different actor names correctly", async () => {
    global.context.actor = "different-user";
    mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValue({ data: { permission: "admin" } });
    await eval(`(async () => { ${checkTeamMemberScript}; await main(); })()`);
    expect(mockGithub.rest.repos.getCollaboratorPermissionLevel).toHaveBeenCalledWith({ owner: "testowner", repo: "testrepo", username: "different-user" });
    expect(mockCore.info).toHaveBeenCalledWith("Checking if user 'different-user' is admin or maintainer of testowner/testrepo");
    expect(mockCore.setOutput).toHaveBeenCalledWith("is_team_member", "true");
  });

  it("should handle different repository contexts correctly", async () => {
    global.context.repo = { owner: "different-owner", repo: "different-repo" };
    mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValue({ data: { permission: "maintain" } });
    await eval(`(async () => { ${checkTeamMemberScript}; await main(); })()`);
    expect(mockGithub.rest.repos.getCollaboratorPermissionLevel).toHaveBeenCalledWith({ owner: "different-owner", repo: "different-repo", username: "testuser" });
    expect(mockCore.info).toHaveBeenCalledWith("Checking if user 'testuser' is admin or maintainer of different-owner/different-repo");
    expect(mockCore.setOutput).toHaveBeenCalledWith("is_team_member", "true");
  });

  it("should handle authentication errors gracefully", async () => {
    const authError = new Error("Bad credentials");
    authError.status = 401;
    mockGithub.rest.repos.getCollaboratorPermissionLevel.mockRejectedValue(authError);
    await eval(`(async () => { ${checkTeamMemberScript}; await main(); })()`);
    expect(mockCore.warning).toHaveBeenCalledWith("Repository permission check failed: Bad credentials");
    expect(mockCore.setOutput).toHaveBeenCalledWith("is_team_member", "false");
  });

  it("should handle rate limiting errors gracefully", async () => {
    const rateLimitError = new Error("API rate limit exceeded");
    rateLimitError.status = 403;
    mockGithub.rest.repos.getCollaboratorPermissionLevel.mockRejectedValue(rateLimitError);
    await eval(`(async () => { ${checkTeamMemberScript}; await main(); })()`);
    expect(mockCore.warning).toHaveBeenCalledWith("Repository permission check failed: API rate limit exceeded");
    expect(mockCore.setOutput).toHaveBeenCalledWith("is_team_member", "false");
  });
});
