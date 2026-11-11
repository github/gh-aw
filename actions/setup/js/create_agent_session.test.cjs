import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import path from "path";

const mockCore = {
  debug: vi.fn(),
  info: vi.fn(),
  warning: vi.fn(),
  error: vi.fn(),
  setFailed: vi.fn(),
  setOutput: vi.fn(),
  summary: {
    addRaw: vi.fn().mockReturnThis(),
    write: vi.fn().mockResolvedValue(),
  },
};

const mockGithub = {
  request: vi.fn(),
};

const mockGetOctokit = vi.fn(token => ({
  _token: token,
  request: vi.fn(),
}));

const mockContext = {
  repo: { owner: "test-owner", repo: "test-repo" },
};

global.core = mockCore;
global.github = mockGithub;
global.getOctokit = mockGetOctokit;
global.context = mockContext;

describe("create_agent_session.cjs", () => {
  let createAgentSessionModule;

  beforeEach(() => {
    vi.clearAllMocks();

    // Restore mockGithub.request as a fresh mock after clearAllMocks
    mockGithub.request = vi.fn();
    // Restore mockGetOctokit after clearAllMocks
    mockGetOctokit.mockImplementation(token => ({
      _token: token,
      request: vi.fn(),
    }));
    global.getOctokit = mockGetOctokit;
    global.github = mockGithub;

    delete process.env.GH_AW_SAFE_OUTPUTS_STAGED;
    delete process.env.GH_AW_TARGET_REPO_SLUG;
    delete process.env.GH_AW_ALLOWED_REPOS;
    delete process.env.GH_AW_AGENT_SESSION_TOKEN;
    delete process.env.GITHUB_TOKEN;

    // Clear module cache to get a fresh module for each test
    const scriptPath = path.join(process.cwd(), "create_agent_session.cjs");
    delete require.cache[require.resolve(scriptPath)];
    createAgentSessionModule = require(scriptPath);
  });

  describe("handler factory", () => {
    it("should return a function when main() is called", async () => {
      const handler = await createAgentSessionModule.main({});
      expect(typeof handler).toBe("function");
    });

    it("should log configuration on initialization", async () => {
      await createAgentSessionModule.main({ base: "develop" });
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Configured base branch: develop"));
    });

    it("should log target repo on initialization", async () => {
      await createAgentSessionModule.main({ "target-repo": "owner/repo" });
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Default target repo: owner/repo"));
    });
  });

  describe("message handler - empty/invalid body", () => {
    it("should skip messages with empty body", async () => {
      const handler = await createAgentSessionModule.main({});
      const result = await handler({ type: "create_agent_session", body: "" });
      expect(result.success).toBe(false);
      expect(result.error).toContain("Empty task description");
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Agent task description is empty, skipping"));
    });

    it("should skip messages with whitespace-only body", async () => {
      const handler = await createAgentSessionModule.main({});
      const result = await handler({ type: "create_agent_session", body: "  \n\t  " });
      expect(result.success).toBe(false);
    });
  });

  describe("staged mode", () => {
    it("should generate staged preview and return skipped without calling the REST API", async () => {
      process.env.GH_AW_SAFE_OUTPUTS_STAGED = "true";
      const handler = await createAgentSessionModule.main({ base: "main" });
      const result = await handler({ type: "create_agent_session", body: "Implement feature X" });
      expect(result.success).toBe(true);
      expect(result.skipped).toBe(true);
      expect(mockGithub.request).not.toHaveBeenCalled();
      // Should have written a staged preview summary
      expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("🎭 Staged Mode: Create Agent Session Preview"));
      expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("Implement feature X"));
    });

    it("should include base branch and target repo in staged preview", async () => {
      process.env.GH_AW_SAFE_OUTPUTS_STAGED = "true";
      const handler = await createAgentSessionModule.main({ base: "develop" });
      await handler({ type: "create_agent_session", body: "Test task" });
      expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("develop"));
    });

    it("should support staged mode via config flag", async () => {
      const handler = await createAgentSessionModule.main({ staged: true, base: "main" });
      const result = await handler({ type: "create_agent_session", body: "Test task" });
      expect(result.success).toBe(true);
      expect(result.skipped).toBe(true);
      expect(mockGithub.request).not.toHaveBeenCalled();
    });
  });

  describe("successful session creation", () => {
    it("should create agent session via REST API and return task id and url", async () => {
      mockGithub.request.mockResolvedValueOnce({
        data: {
          id: "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
          html_url: "https://github.com/test-owner/test-repo/copilot/tasks/a1b2c3d4-e5f6-7890-abcd-ef1234567890",
          state: "queued",
        },
      });

      const handler = await createAgentSessionModule.main({ base: "main" });
      const result = await handler({ type: "create_agent_session", body: "Implement feature X" });

      expect(result.success).toBe(true);
      expect(result.id).toBe("a1b2c3d4-e5f6-7890-abcd-ef1234567890");
      expect(result.url).toBe("https://github.com/test-owner/test-repo/copilot/tasks/a1b2c3d4-e5f6-7890-abcd-ef1234567890");
    });

    it("should call REST API with correct path, owner, repo, prompt, and base_ref", async () => {
      mockGithub.request.mockResolvedValueOnce({
        data: {
          id: "task-uuid-001",
          html_url: "https://github.com/test-owner/test-repo/copilot/tasks/task-uuid-001",
          state: "queued",
        },
      });

      const handler = await createAgentSessionModule.main({ base: "develop" });
      await handler({ type: "create_agent_session", body: "Test task" });

      expect(mockGithub.request).toHaveBeenCalledWith(
        "POST /agents/repos/{owner}/{repo}/tasks",
        expect.objectContaining({
          owner: "test-owner",
          repo: "test-repo",
          prompt: "Test task",
          base_ref: "develop",
          headers: { "X-GitHub-Api-Version": "2026-03-10" },
        })
      );
    });

    it("should call REST API with cross-repo owner/repo derived from message", async () => {
      process.env.GH_AW_ALLOWED_REPOS = "other-owner/other-repo";
      mockGithub.request.mockResolvedValueOnce({
        data: {
          id: "cross-repo-task-uuid",
          html_url: "https://github.com/other-owner/other-repo/copilot/tasks/cross-repo-task-uuid",
          state: "queued",
        },
      });

      const handler = await createAgentSessionModule.main({ "target-repo": "other-owner/other-repo", base: "main" });
      await handler({ type: "create_agent_session", body: "Cross-repo task", repo: "other-owner/other-repo" });

      expect(mockGithub.request).toHaveBeenCalledWith("POST /agents/repos/{owner}/{repo}/tasks", expect.objectContaining({ owner: "other-owner", repo: "other-repo" }));
    });

    it("should use GH_AW_AGENT_SESSION_TOKEN to create a dedicated octokit client", async () => {
      process.env.GH_AW_AGENT_SESSION_TOKEN = "test-pat-token";
      const mockTokenClient = { request: vi.fn() };
      mockTokenClient.request.mockResolvedValueOnce({
        data: {
          id: "session-uuid-from-token",
          html_url: "https://github.com/test-owner/test-repo/copilot/tasks/session-uuid-from-token",
          state: "queued",
        },
      });
      mockGetOctokit.mockReturnValueOnce(mockTokenClient);

      const handler = await createAgentSessionModule.main({ base: "main" });
      await handler({ type: "create_agent_session", body: "Test task" });

      expect(mockGetOctokit).toHaveBeenCalledWith("test-pat-token");
      expect(mockTokenClient.request).toHaveBeenCalled();
      expect(mockGithub.request).not.toHaveBeenCalled();
    });

    it("should prefer per-handler github-token over GH_AW_AGENT_SESSION_TOKEN", async () => {
      process.env.GH_AW_AGENT_SESSION_TOKEN = "step-token";
      const mockHandlerClient = { request: vi.fn() };
      mockHandlerClient.request.mockResolvedValueOnce({
        data: {
          id: "handler-token-task",
          html_url: "https://github.com/test-owner/test-repo/copilot/tasks/handler-token-task",
          state: "queued",
        },
      });
      mockGetOctokit.mockReturnValueOnce(mockHandlerClient);

      const handler = await createAgentSessionModule.main({ base: "main", "github-token": "per-handler-token" });
      await handler({ type: "create_agent_session", body: "Test task" });

      expect(mockGetOctokit).toHaveBeenCalledWith("per-handler-token");
      expect(mockHandlerClient.request).toHaveBeenCalled();
    });
  });

  describe("error handling", () => {
    it.each([
      [401, false],
      [401, true],
      [403, false],
      [403, true],
      [500, false],
      [500, true],
      [undefined, false],
    ])("uses structured HTTP status %s (response-only: %s) despite authentication digits in the request ID", async (status, responseOnly) => {
      mockGithub.request.mockRejectedValueOnce(
        Object.assign(new Error(""), {
          ...(responseOnly ? {} : { status }),
          response: { status, headers: { "x-github-request-id": "C401:C403:C404:6553A7F0" } },
        })
      );

      const handler = await createAgentSessionModule.main({ base: "main" });
      const result = await handler({ type: "create_agent_session", body: "Test task" });

      expect(result.success).toBe(false);
      expect(result.error).toContain("request ID C401:C403:C404:");
      if (status === 401 || status === 403) {
        expect(mockCore.error).toHaveBeenCalledWith(expect.stringContaining("authentication/permission error"));
      } else {
        expect(mockCore.error).toHaveBeenCalledExactlyOnceWith(`Error creating agent session: ${result.error}`);
      }
    });

    it("should return failure when REST API fails with auth error", async () => {
      mockGithub.request.mockRejectedValueOnce(new Error("permission denied (403)"));

      const handler = await createAgentSessionModule.main({ base: "main" });
      const result = await handler({ type: "create_agent_session", body: "Test task" });

      expect(result.success).toBe(false);
      expect(mockCore.error).toHaveBeenCalledWith(expect.stringContaining("authentication/permission error"));
      expect(mockCore.error).toHaveBeenCalledWith(expect.stringContaining("GH_AW_AGENT_SESSION_TOKEN"));
    });

    it("should return failure when REST API fails with generic error", async () => {
      mockGithub.request.mockRejectedValueOnce(new Error("network failure"));

      const handler = await createAgentSessionModule.main({ base: "main" });
      const result = await handler({ type: "create_agent_session", body: "Test task" });

      expect(result.success).toBe(false);
      expect(mockCore.error).toHaveBeenCalled();
    });

    it("should reject repositories not in allowlist", async () => {
      process.env.GH_AW_ALLOWED_REPOS = "allowed-owner/allowed-repo";

      const handler = await createAgentSessionModule.main({ "target-repo": "other-owner/other-repo", base: "main" });
      const result = await handler({ type: "create_agent_session", body: "Test task", repo: "not-allowed/other-repo" });

      expect(result.success).toBe(false);
      expect(mockCore.error).toHaveBeenCalledWith(expect.stringContaining("E004:"));
    });
  });

  describe("per-handler accessors", () => {
    it("getSessionNumber() returns first successful session id", async () => {
      mockGithub.request.mockResolvedValue({
        data: {
          id: "uuid-task-42",
          html_url: "https://github.com/test-owner/test-repo/copilot/tasks/uuid-task-42",
          state: "queued",
        },
      });

      const handler = await createAgentSessionModule.main({ base: "main" });
      await handler({ type: "create_agent_session", body: "Task 1" });

      expect(handler.getSessionNumber()).toBe("uuid-task-42");
    });

    it("getSessionUrl() returns first successful session URL", async () => {
      const expectedUrl = "https://github.com/test-owner/test-repo/copilot/tasks/uuid-task-42";
      mockGithub.request.mockResolvedValue({
        data: {
          id: "uuid-task-42",
          html_url: expectedUrl,
          state: "queued",
        },
      });

      const handler = await createAgentSessionModule.main({ base: "main" });
      await handler({ type: "create_agent_session", body: "Task 1" });

      expect(handler.getSessionUrl()).toBe(expectedUrl);
    });

    it("getSessionNumber() returns empty string when no sessions created", async () => {
      const handler = await createAgentSessionModule.main({ base: "main" });
      expect(handler.getSessionNumber()).toBe("");
    });

    it("getSessionUrl() returns empty string when no sessions created", async () => {
      const handler = await createAgentSessionModule.main({ base: "main" });
      expect(handler.getSessionUrl()).toBe("");
    });
  });

  describe("writeSummary()", () => {
    it("should write summary with successful sessions", async () => {
      mockGithub.request.mockResolvedValue({
        data: {
          id: "uuid-task-42",
          html_url: "https://github.com/test-owner/test-repo/copilot/tasks/uuid-task-42",
          state: "queued",
        },
      });

      const handler = await createAgentSessionModule.main({ base: "main" });
      await handler({ type: "create_agent_session", body: "Task 1" });
      await handler.writeSummary();

      expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("Agent Sessions"));
      expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("uuid-task-42"));
    });

    it("should not write summary when no results", async () => {
      const handler = await createAgentSessionModule.main({ base: "main" });
      await handler.writeSummary();

      expect(mockCore.summary.addRaw).not.toHaveBeenCalled();
    });

    it("should write summary with failed sessions", async () => {
      mockGithub.request.mockRejectedValueOnce(new Error("some error"));

      const handler = await createAgentSessionModule.main({ base: "main" });
      await handler({ type: "create_agent_session", body: "Task 1" });
      await handler.writeSummary();

      expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("❌ Failed"));
    });
  });

  describe("concurrency safety - independent main() invocations", () => {
    it("should not cross-contaminate results between two concurrently-processed handler instances", async () => {
      mockGithub.request.mockResolvedValueOnce({
        data: {
          id: "session-A",
          html_url: "https://github.com/test-owner/test-repo/copilot/tasks/session-A",
          state: "queued",
        },
      });

      const mockTokenClient = { request: vi.fn() };
      mockTokenClient.request.mockResolvedValueOnce({
        data: {
          id: "session-B",
          html_url: "https://github.com/test-owner/test-repo/copilot/tasks/session-B",
          state: "queued",
        },
      });
      mockGetOctokit.mockReturnValueOnce(mockTokenClient);

      // Two independent handler instances created from separate main() invocations.
      const handlerA = await createAgentSessionModule.main({ base: "main" });
      const handlerB = await createAgentSessionModule.main({ base: "main", "github-token": "handler-b-token" });

      // Interleave processing to simulate concurrent/parallel dispatch.
      const resultA = await handlerA({ type: "create_agent_session", body: "Task A" });
      const resultB = await handlerB({ type: "create_agent_session", body: "Task B" });

      expect(resultA.id).toBe("session-A");
      expect(resultB.id).toBe("session-B");

      // Each handler's accessors must only reflect its own results.
      expect(handlerA.getSessionNumber()).toBe("session-A");
      expect(handlerB.getSessionNumber()).toBe("session-B");
      expect(handlerA.getSessionUrl()).toContain("session-A");
      expect(handlerB.getSessionUrl()).toContain("session-B");
    });
  });

  describe("cross-repository allowlist validation", () => {
    it("should allow target repository in allowlist", async () => {
      process.env.GH_AW_ALLOWED_REPOS = "allowed-owner/allowed-repo";
      mockGithub.request.mockResolvedValueOnce({
        data: {
          id: "cross-repo-uuid-123",
          html_url: "https://github.com/allowed-owner/allowed-repo/copilot/tasks/cross-repo-uuid-123",
          state: "queued",
        },
      });

      const handler = await createAgentSessionModule.main({ "target-repo": "allowed-owner/allowed-repo", base: "main" });
      const result = await handler({ type: "create_agent_session", body: "Test task", repo: "allowed-owner/allowed-repo" });

      expect(result.success).toBe(true);
      expect(result.id).toBe("cross-repo-uuid-123");
    });

    it("should allow default repository without allowlist", async () => {
      delete process.env.GH_AW_TARGET_REPO_SLUG;
      delete process.env.GH_AW_ALLOWED_REPOS;
      mockGithub.request.mockResolvedValueOnce({
        data: {
          id: "default-repo-uuid-123",
          html_url: "https://github.com/test-owner/test-repo/copilot/tasks/default-repo-uuid-123",
          state: "queued",
        },
      });

      const handler = await createAgentSessionModule.main({ base: "main" });
      const result = await handler({ type: "create_agent_session", body: "Test task" });

      expect(result.success).toBe(true);
      expect(result.id).toBe("default-repo-uuid-123");
    });
  });
});
