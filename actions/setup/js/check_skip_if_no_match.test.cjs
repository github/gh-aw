// @ts-check
import { describe, it, expect, beforeEach } from "vitest";
const { main } = require("./check_skip_if_no_match.cjs");

describe("check_skip_if_no_match", () => {
  let mockCore;
  let mockGithub;
  let mockContext;

  beforeEach(() => {
    mockCore = {
      info: () => {},
      warning: () => {},
      setFailed: () => {},
      setOutput: () => {},
      messages: [],
      infos: [],
      warnings: [],
      errors: [],
      outputs: {},
    };

    mockCore.info = msg => {
      mockCore.infos.push(msg);
      mockCore.messages.push({ level: "info", message: msg });
    };
    mockCore.warning = msg => {
      mockCore.warnings.push(msg);
      mockCore.messages.push({ level: "warning", message: msg });
    };
    mockCore.setFailed = msg => {
      mockCore.errors.push(msg);
      mockCore.messages.push({ level: "error", message: msg });
    };
    mockCore.setOutput = (key, value) => {
      mockCore.outputs[key] = value;
    };

    mockGithub = {
      rest: {
        search: {
          issuesAndPullRequests: async () => ({}),
        },
      },
    };

    mockContext = {
      repo: {
        owner: "test-owner",
        repo: "test-repo",
      },
    };

    global.core = mockCore;
    global.github = mockGithub;
    global.context = mockContext;
  });

  it("should fail when GH_AW_SKIP_QUERY is not specified", async () => {
    process.env.GH_AW_SKIP_QUERY = "";
    process.env.GH_AW_WORKFLOW_NAME = "test-workflow";

    await main();

    expect(mockCore.errors).toContain("Configuration error: GH_AW_SKIP_QUERY not specified.");
  });

  it("should fail when GH_AW_WORKFLOW_NAME is not specified", async () => {
    process.env.GH_AW_SKIP_QUERY = "is:open is:issue";
    process.env.GH_AW_WORKFLOW_NAME = "";

    await main();

    expect(mockCore.errors).toContain("Configuration error: GH_AW_WORKFLOW_NAME not specified.");
  });

  it("should fail when GH_AW_SKIP_MIN_MATCHES is not a valid positive integer", async () => {
    process.env.GH_AW_SKIP_QUERY = "is:open is:issue";
    process.env.GH_AW_WORKFLOW_NAME = "test-workflow";
    process.env.GH_AW_SKIP_MIN_MATCHES = "invalid";

    await main();

    expect(mockCore.errors).toContain('Configuration error: GH_AW_SKIP_MIN_MATCHES must be a positive integer, got "invalid".');
  });

  it("should fail when GH_AW_SKIP_MIN_MATCHES is zero", async () => {
    process.env.GH_AW_SKIP_QUERY = "is:open is:issue";
    process.env.GH_AW_WORKFLOW_NAME = "test-workflow";
    process.env.GH_AW_SKIP_MIN_MATCHES = "0";

    await main();

    expect(mockCore.errors).toContain('Configuration error: GH_AW_SKIP_MIN_MATCHES must be a positive integer, got "0".');
  });

  it("should use default min matches of 1 when not specified", async () => {
    process.env.GH_AW_SKIP_QUERY = "is:open is:issue";
    process.env.GH_AW_WORKFLOW_NAME = "test-workflow";
    delete process.env.GH_AW_SKIP_MIN_MATCHES;

    mockGithub.rest.search.issuesAndPullRequests = async () => ({
      data: { total_count: 5 },
    });

    await main();

    expect(mockCore.infos).toContain("Minimum matches threshold: 1");
    expect(mockCore.outputs["skip_no_match_check_ok"]).toBe("true");
  });

  it("should set skip_no_match_check_ok to false when no matches found", async () => {
    process.env.GH_AW_SKIP_QUERY = "is:open is:issue";
    process.env.GH_AW_WORKFLOW_NAME = "test-workflow";
    process.env.GH_AW_SKIP_MIN_MATCHES = "1";

    mockGithub.rest.search.issuesAndPullRequests = async () => ({
      data: { total_count: 0 },
    });

    await main();

    expect(mockCore.warnings).toContain("🔍 Skip condition matched (0 items found, minimum required: 1). Workflow execution will be prevented by activation job.");
    expect(mockCore.outputs["skip_no_match_check_ok"]).toBe("false");
  });

  it("should set skip_no_match_check_ok to false when matches below minimum threshold", async () => {
    process.env.GH_AW_SKIP_QUERY = "is:open is:issue label:bug";
    process.env.GH_AW_WORKFLOW_NAME = "test-workflow";
    process.env.GH_AW_SKIP_MIN_MATCHES = "5";

    mockGithub.rest.search.issuesAndPullRequests = async () => ({
      data: { total_count: 3 },
    });

    await main();

    expect(mockCore.warnings).toContain("🔍 Skip condition matched (3 items found, minimum required: 5). Workflow execution will be prevented by activation job.");
    expect(mockCore.outputs["skip_no_match_check_ok"]).toBe("false");
  });

  it("should set skip_no_match_check_ok to true when matches equal minimum threshold", async () => {
    process.env.GH_AW_SKIP_QUERY = "is:open is:issue";
    process.env.GH_AW_WORKFLOW_NAME = "test-workflow";
    process.env.GH_AW_SKIP_MIN_MATCHES = "3";

    mockGithub.rest.search.issuesAndPullRequests = async () => ({
      data: { total_count: 3 },
    });

    await main();

    expect(mockCore.infos).toContain("✓ Found 3 matches (meets or exceeds minimum of 3), workflow can proceed");
    expect(mockCore.outputs["skip_no_match_check_ok"]).toBe("true");
  });

  it("should set skip_no_match_check_ok to true when matches exceed minimum threshold", async () => {
    process.env.GH_AW_SKIP_QUERY = "is:open is:issue";
    process.env.GH_AW_WORKFLOW_NAME = "test-workflow";
    process.env.GH_AW_SKIP_MIN_MATCHES = "2";

    mockGithub.rest.search.issuesAndPullRequests = async () => ({
      data: { total_count: 10 },
    });

    await main();

    expect(mockCore.infos).toContain("✓ Found 10 matches (meets or exceeds minimum of 2), workflow can proceed");
    expect(mockCore.outputs["skip_no_match_check_ok"]).toBe("true");
  });

  it("should scope the query to the repository", async () => {
    process.env.GH_AW_SKIP_QUERY = "is:open is:issue";
    process.env.GH_AW_WORKFLOW_NAME = "test-workflow";

    let capturedQuery;
    mockGithub.rest.search.issuesAndPullRequests = async params => {
      capturedQuery = params.q;
      return { data: { total_count: 1 } };
    };

    await main();

    expect(capturedQuery).toBe("is:open is:issue repo:test-owner/test-repo");
  });

  it("should fail when search API throws an error", async () => {
    process.env.GH_AW_SKIP_QUERY = "is:open is:issue";
    process.env.GH_AW_WORKFLOW_NAME = "test-workflow";

    const error = new Error("API rate limit exceeded");
    mockGithub.rest.search.issuesAndPullRequests = async () => {
      throw error;
    };

    await main();

    expect(mockCore.errors).toContain("Failed to execute search query: API rate limit exceeded");
  });

  it("should log info messages during execution", async () => {
    process.env.GH_AW_SKIP_QUERY = "is:open is:issue label:enhancement";
    process.env.GH_AW_WORKFLOW_NAME = "test-workflow";
    process.env.GH_AW_SKIP_MIN_MATCHES = "5";

    mockGithub.rest.search.issuesAndPullRequests = async () => ({
      data: { total_count: 8 },
    });

    await main();

    expect(mockCore.infos).toContain("Checking skip-if-no-match query: is:open is:issue label:enhancement");
    expect(mockCore.infos).toContain("Minimum matches threshold: 5");
    expect(mockCore.infos).toContain("Scoped query: is:open is:issue label:enhancement repo:test-owner/test-repo");
    expect(mockCore.infos).toContain("Search found 8 matching items");
  });
});
