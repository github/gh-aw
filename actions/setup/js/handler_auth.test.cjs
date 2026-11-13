// @ts-check
/// <reference types="@actions/github-script" />

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";

import { createAuthenticatedGitHubClient, createAuthenticatedLoginResolver } from "./handler_auth.cjs";

describe("createAuthenticatedGitHubClient", () => {
  let mockCore;
  let mockGithub;
  let originalGlobals;

  beforeEach(() => {
    originalGlobals = {
      core: global.core,
      github: global.github,
      getOctokit: global.getOctokit,
    };

    mockCore = {
      info: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
    };

    // The global github object (step-level token)
    mockGithub = {
      _token: "step-level-token",
      rest: {},
      graphql: vi.fn(),
    };

    global.core = mockCore;
    global.github = mockGithub;

    // Mock the builtin getOctokit (available in actions/github-script@v9)
    global.getOctokit = vi.fn(token => ({
      _token: token,
      rest: {},
      graphql: vi.fn(),
    }));
  });

  afterEach(() => {
    global.core = originalGlobals.core;
    global.github = originalGlobals.github;
    global.getOctokit = originalGlobals.getOctokit;
    vi.clearAllMocks();
  });

  it("returns the global github when no github-token in config", async () => {
    const client = await createAuthenticatedGitHubClient({});
    expect(client).toBe(mockGithub);
    expect(mockCore.info).not.toHaveBeenCalled();
  });

  it("returns the global github when config is empty object", async () => {
    const client = await createAuthenticatedGitHubClient({ max: 10, target: "triggering" });
    expect(client).toBe(mockGithub);
  });

  it("creates a new Octokit when github-token is set in config", async () => {
    const client = await createAuthenticatedGitHubClient({ "github-token": "my-pat-token" });

    // Should NOT be the global github
    expect(client).not.toBe(mockGithub);
    // Should have the token from config
    expect(client._token).toBe("my-pat-token");
  });

  it("logs a message when using per-handler token", async () => {
    await createAuthenticatedGitHubClient({ "github-token": "my-pat-token" });
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("per-handler github-token"));
  });

  it("creates distinct Octokit instances for different tokens", async () => {
    const client1 = await createAuthenticatedGitHubClient({ "github-token": "token-1" });
    const client2 = await createAuthenticatedGitHubClient({ "github-token": "token-2" });

    expect(client1._token).toBe("token-1");
    expect(client2._token).toBe("token-2");
    expect(client1).not.toBe(client2);
  });

  it("does not mutate the global github object", async () => {
    const originalGithub = global.github;
    await createAuthenticatedGitHubClient({ "github-token": "my-pat-token" });

    // Global should be unchanged
    expect(global.github).toBe(originalGithub);
    expect(global.github._token).toBe("step-level-token");
  });
});

describe("createAuthenticatedLoginResolver", () => {
  it("defers the lookup and shares successful results across concurrent and later calls", async () => {
    const getAuthenticated = vi.fn().mockResolvedValue({ data: { login: "publisher" } });
    const logger = { info: vi.fn() };
    const resolveLogin = createAuthenticatedLoginResolver({ rest: { users: { getAuthenticated } } }, logger);
    expect(getAuthenticated).not.toHaveBeenCalled();

    const first = resolveLogin();
    expect(resolveLogin()).toBe(first);
    expect(await first).toBe("publisher");
    expect(await resolveLogin()).toBe("publisher");
    expect(getAuthenticated).toHaveBeenCalledOnce();
    expect(logger.info).not.toHaveBeenCalled();
  });

  it.each(["rejection", "synchronous throw"])("caches and logs the bot fallback after %s", async failure => {
    const getAuthenticated = vi.fn(() => {
      const error = new Error("Resource not accessible by integration");
      if (failure === "synchronous throw") throw error;
      return Promise.reject(error);
    });
    const logger = { info: vi.fn() };
    const resolveLogin = createAuthenticatedLoginResolver({ rest: { users: { getAuthenticated } } }, logger);

    expect(await Promise.all([resolveLogin(), resolveLogin()])).toEqual([null, null]);
    expect(await resolveLogin()).toBeNull();
    expect(getAuthenticated).toHaveBeenCalledOnce();
    expect(logger.info).toHaveBeenCalledExactlyOnceWith(expect.stringContaining("considering bot-authored comments only"));
  });

  it("caches a missing login as unavailable", async () => {
    const getAuthenticated = vi.fn().mockResolvedValue({ data: {} });
    const resolveLogin = createAuthenticatedLoginResolver({ rest: { users: { getAuthenticated } } }, { info: vi.fn() });

    expect(await resolveLogin()).toBeNull();
    expect(await resolveLogin()).toBeNull();
    expect(getAuthenticated).toHaveBeenCalledOnce();
  });

  it("keeps publishing identities isolated between clients", async () => {
    const logger = { info: vi.fn() };
    const first = createAuthenticatedLoginResolver({ rest: { users: { getAuthenticated: vi.fn().mockResolvedValue({ data: { login: "first" } }) } } }, logger);
    const second = createAuthenticatedLoginResolver({ rest: { users: { getAuthenticated: vi.fn().mockResolvedValue({ data: { login: "second" } }) } } }, logger);

    expect(await Promise.all([first(), second()])).toEqual(["first", "second"]);
  });
});
