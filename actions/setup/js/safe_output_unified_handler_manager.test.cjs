// @ts-check

import { describe, it, expect, beforeEach, vi } from "vitest";
import { loadConfig, setupProjectGitHubClient } from "./safe_output_unified_handler_manager.cjs";

// Mock @actions/github
vi.mock("@actions/github", () => ({
  getOctokit: vi.fn(() => ({
    graphql: vi.fn(),
    request: vi.fn(),
    rest: {},
  })),
}));

describe("Unified Safe Output Handler Manager", () => {
  beforeEach(() => {
    // Mock global core
    global.core = {
      info: vi.fn(),
      debug: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
      setOutput: vi.fn(),
      setFailed: vi.fn(),
    };

    // Mock global context
    global.context = {
      repo: {
        owner: "testowner",
        repo: "testrepo",
      },
      payload: {},
    };

    // Clean up environment variables
    delete process.env.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG;
    delete process.env.GH_AW_SAFE_OUTPUTS_PROJECT_HANDLER_CONFIG;
    delete process.env.GH_AW_PROJECT_GITHUB_TOKEN;
  });

  describe("loadConfig", () => {
    it("should load regular handler config", () => {
      process.env.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG = JSON.stringify({
        create_issue: { max: 5 },
        add_comment: {},
      });

      const config = loadConfig();

      expect(config).toHaveProperty("regular");
      expect(config).toHaveProperty("project");
      expect(config.regular).toHaveProperty("create_issue");
      expect(config.regular.create_issue).toEqual({ max: 5 });
      expect(config.regular).toHaveProperty("add_comment");
    });

    it("should load project handler config", () => {
      process.env.GH_AW_SAFE_OUTPUTS_PROJECT_HANDLER_CONFIG = JSON.stringify({
        create_project: { max: 1 },
        update_project: { max: 100 },
      });

      const config = loadConfig();

      expect(config).toHaveProperty("project");
      expect(config.project).toHaveProperty("create_project");
      expect(config.project.create_project).toEqual({ max: 1 });
      expect(config.project).toHaveProperty("update_project");
    });

    it("should load both regular and project configs", () => {
      process.env.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG = JSON.stringify({
        create_issue: { max: 5 },
      });
      process.env.GH_AW_SAFE_OUTPUTS_PROJECT_HANDLER_CONFIG = JSON.stringify({
        create_project: { max: 1 },
      });

      const config = loadConfig();

      expect(config.regular).toHaveProperty("create_issue");
      expect(config.project).toHaveProperty("create_project");
    });

    it("should throw error if no config is provided", () => {
      expect(() => loadConfig()).toThrow(/At least one of .* is required/);
    });

    it("should normalize hyphenated keys to underscores", () => {
      process.env.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG = JSON.stringify({
        "create-issue": { max: 5 },
      });

      const config = loadConfig();

      expect(config.regular).toHaveProperty("create_issue");
      expect(config.regular).not.toHaveProperty("create-issue");
    });
  });

  describe("setupProjectGitHubClient", () => {
    it("should throw error if GH_AW_PROJECT_GITHUB_TOKEN is not set", () => {
      expect(() => setupProjectGitHubClient()).toThrow(/GH_AW_PROJECT_GITHUB_TOKEN environment variable is required/);
    });

    it("should create Octokit instance when token is provided", () => {
      process.env.GH_AW_PROJECT_GITHUB_TOKEN = "test-project-token";

      const octokit = setupProjectGitHubClient();

      expect(octokit).toBeDefined();
      expect(octokit).toHaveProperty("graphql");
      expect(octokit).toHaveProperty("request");
    });
  });
});
