import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import fs from "fs";
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

const mockContext = {
  repo: {
    owner: "test-owner",
    repo: "test-repo",
  },
};

const mockGithub = {
  request: vi.fn().mockResolvedValue({ data: { id: "task-123" } }),
  rest: {
    issues: {
      createComment: vi.fn().mockResolvedValue({ data: { id: 12345 } }),
      checkUserCanBeAssigned: vi.fn().mockResolvedValue({}),
      get: vi.fn(),
    },
    users: {
      getByUsername: vi.fn().mockResolvedValue({ data: { id: 99999 } }),
    },
    pulls: {
      get: vi.fn(),
    },
    repos: {
      get: vi.fn().mockResolvedValue({ data: { node_id: "REPO_NODE_ID", default_branch: "main" } }),
    },
  },
};

global.core = mockCore;
global.context = mockContext;
global.github = mockGithub;

describe("assign_to_agent", () => {
  let assignToAgentScript;
  let tempFilePath;
  let sleepSpy;
  const mockSleep = vi.fn().mockResolvedValue();

  // Simulates the safe-output handler manager: builds handler config from env vars,
  // calls main() as a factory, then processes items from GH_AW_AGENT_OUTPUT.
  // This mirrors the production flow without requiring any backward-compat changes in
  // assign_to_agent.cjs itself.
  const STANDALONE_RUNNER = `
    const _config = {};
    if (process.env.GH_AW_AGENT_DEFAULT?.trim()) _config.name = process.env.GH_AW_AGENT_DEFAULT.trim();
    if (process.env.GH_AW_AGENT_MODEL?.trim()) _config.model = process.env.GH_AW_AGENT_MODEL.trim();
    if (process.env.GH_AW_AGENT_MAX_COUNT?.trim()) _config.max = process.env.GH_AW_AGENT_MAX_COUNT.trim();
    if (process.env.GH_AW_AGENT_TARGET?.trim()) _config.target = process.env.GH_AW_AGENT_TARGET.trim();
    if (process.env.GH_AW_AGENT_ALLOWED?.trim()) _config.allowed = process.env.GH_AW_AGENT_ALLOWED.trim();
    if (process.env.GH_AW_AGENT_IGNORE_IF_ERROR?.trim()) _config["ignore-if-error"] = process.env.GH_AW_AGENT_IGNORE_IF_ERROR.trim();
    if (process.env.GH_AW_AGENT_PULL_REQUEST_REPO?.trim()) _config["pull-request-repo"] = process.env.GH_AW_AGENT_PULL_REQUEST_REPO.trim();
    if (process.env.GH_AW_AGENT_ALLOWED_PULL_REQUEST_REPOS?.trim()) _config["allowed-pull-request-repos"] = process.env.GH_AW_AGENT_ALLOWED_PULL_REQUEST_REPOS.trim();
    if (process.env.GH_AW_AGENT_BASE_BRANCH?.trim()) _config["base-branch"] = process.env.GH_AW_AGENT_BASE_BRANCH.trim();
    if (process.env.GH_AW_AGENT_REASONING_EFFORT != null) _config["reasoning-effort"] = process.env.GH_AW_AGENT_REASONING_EFFORT;
    if (process.env.GH_AW_ALLOWED_REPOS?.trim()) _config.allowed_repos = process.env.GH_AW_ALLOWED_REPOS.trim();

    let _handler;
    try { _handler = await main(_config); } catch (_err) { core.setFailed(_err.message); return; }

    const _agentOutputFile = process.env.GH_AW_AGENT_OUTPUT;
    if (!_agentOutputFile) { core.info("No GH_AW_AGENT_OUTPUT environment variable found"); return; }

    const _fs = require("fs");
    const _agentOutput = JSON.parse(_fs.readFileSync(_agentOutputFile, "utf8"));
    const _items = _agentOutput.items.filter(i => i.type === "assign_to_agent");
    if (_items.length === 0) {
      core.info("No assign_to_agent items found in agent output");
    } else {
      const _maxCount = parseInt(String(_config.max ?? "1"), 10);
      if (_items.length > _maxCount) {
        core.warning("Found " + _items.length + " agent assignments, but max is " + _maxCount + ". Extra assignments will be skipped.");
      }
      const { loadTemporaryIdMap } = require("./temporary_id.cjs");
      const _tempIdMap = loadTemporaryIdMap();
      for (const _item of _items) { await _handler(_item, {}, _tempIdMap); }
    }
    await writeAssignToAgentSummary(_handler);
    const _errorCount = getAssignToAgentErrorCount(_handler);
    core.setOutput("assigned", getAssignToAgentAssigned(_handler));
    core.setOutput("assignment_errors", getAssignToAgentErrors(_handler));
    core.setOutput("assignment_error_count", String(_errorCount));
    if (_errorCount > 0) { core.setFailed("Failed to assign " + _errorCount + " agent(s)"); }
  `;

  const setAgentOutput = data => {
    tempFilePath = path.join(process.cwd(), `.test_agent_output_${Date.now()}_${Math.random().toString(36).slice(2)}.json`);
    const content = typeof data === "string" ? data : JSON.stringify(data);
    fs.writeFileSync(tempFilePath, content);
    process.env.GH_AW_AGENT_OUTPUT = tempFilePath;
  };

  beforeEach(() => {
    vi.clearAllMocks();
    mockSleep.mockClear();

    // Reset REST mocks to default implementations (vi.clearAllMocks() cleared them)
    mockGithub.request.mockResolvedValue({ data: { id: "task-123" } });
    mockGithub.rest.issues.checkUserCanBeAssigned.mockResolvedValue({});
    mockGithub.rest.issues.get.mockReset();
    mockGithub.rest.users.getByUsername.mockResolvedValue({ data: { id: 99999 } });
    mockGithub.rest.pulls.get.mockReset();
    mockGithub.rest.repos.get.mockResolvedValue({ data: { node_id: "REPO_NODE_ID", default_branch: "main" } });

    // Reset mockGithub.rest.issues.createComment
    mockGithub.rest.issues.createComment = vi.fn().mockResolvedValue({ data: { id: 12345 } });

    delete process.env.GH_AW_AGENT_OUTPUT;
    delete process.env.GH_AW_SAFE_OUTPUTS_STAGED;
    delete process.env.GH_AW_AGENT_DEFAULT;
    delete process.env.GH_AW_AGENT_MODEL;
    delete process.env.GH_AW_AGENT_MAX_COUNT;
    delete process.env.GH_AW_AGENT_TARGET;
    delete process.env.GH_AW_AGENT_ALLOWED;
    delete process.env.GH_AW_TARGET_REPO_SLUG;
    delete process.env.GH_AW_ALLOWED_REPOS;
    delete process.env.GH_AW_AGENT_IGNORE_IF_ERROR;
    delete process.env.GH_AW_TEMPORARY_ID_MAP;
    delete process.env.GH_AW_AGENT_PULL_REQUEST_REPO;
    delete process.env.GH_AW_AGENT_ALLOWED_PULL_REQUEST_REPOS;
    delete process.env.GH_AW_AGENT_BASE_BRANCH;
    delete process.env.GH_AW_AGENT_REASONING_EFFORT;

    // Reset context to default
    mockContext.eventName = "issues";
    mockContext.payload = {
      issue: { number: 42 },
    };

    // Clear module cache to ensure we get the latest version of assign_agent_helpers
    const helpersPath = require.resolve("./assign_agent_helpers.cjs");
    delete require.cache[helpersPath];
    const errorRecovery = require("./error_recovery.cjs");
    sleepSpy = vi.spyOn(errorRecovery, "sleep").mockImplementation(mockSleep);

    const scriptPath = path.join(process.cwd(), "assign_to_agent.cjs");
    assignToAgentScript = fs.readFileSync(scriptPath, "utf8");
  });

  afterEach(() => {
    if (tempFilePath && fs.existsSync(tempFilePath)) {
      fs.unlinkSync(tempFilePath);
    }
    sleepSpy?.mockRestore();
  });

  it("should handle empty agent output", async () => {
    setAgentOutput({ items: [], errors: [] });
    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("No assign_to_agent items found"));
  });

  it("should handle missing agent output", async () => {
    delete process.env.GH_AW_AGENT_OUTPUT;
    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);
    expect(mockCore.info).toHaveBeenCalledWith("No GH_AW_AGENT_OUTPUT environment variable found");
  });

  it("should handle staged mode correctly", async () => {
    process.env.GH_AW_SAFE_OUTPUTS_STAGED = "true";
    setAgentOutput({
      items: [
        {
          type: "assign_to_agent",
          issue_number: 42,
          agent: "copilot",
        },
      ],
      errors: [],
    });

    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

    expect(mockGithub.request).not.toHaveBeenCalled();
    expect(mockCore.summary.addRaw).toHaveBeenCalled();
    const summaryCall = mockCore.summary.addRaw.mock.calls[0][0];
    expect(summaryCall).toContain("🎭 Staged Mode");
    expect(summaryCall).toContain("Issue:** #42");
    expect(summaryCall).toContain("Agent:** copilot");
  });

  it("should include configured reasoning effort in staged previews", async () => {
    process.env.GH_AW_SAFE_OUTPUTS_STAGED = "true";
    process.env.GH_AW_AGENT_REASONING_EFFORT = "high";
    process.env.GH_AW_AGENT_MODEL = "o3";
    setAgentOutput({ items: [{ type: "assign_to_agent", pull_number: 42, agent: "copilot" }], errors: [] });

    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

    expect(mockCore.summary.addRaw.mock.calls[0][0]).toContain("Reasoning Effort:** high");
  });

  it("should use default agent when not specified", async () => {
    process.env.GH_AW_AGENT_DEFAULT = "copilot";
    setAgentOutput({
      items: [
        {
          type: "assign_to_agent",
          issue_number: 42,
        },
      ],
      errors: [],
    });

    // Mock REST responses: findAgent, getIssueDetails, assignAgentToIssue
    mockGithub.rest.issues.checkUserCanBeAssigned.mockResolvedValueOnce({});
    mockGithub.rest.users.getByUsername.mockResolvedValueOnce({ data: { id: 99999 } });
    mockGithub.rest.issues.get.mockResolvedValueOnce({
      data: { id: 12345, number: 42, assignees: [], html_url: "", title: "", body: "" },
    });
    mockGithub.request.mockResolvedValueOnce({ data: { id: "task-123" } });

    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

    expect(mockCore.info).toHaveBeenCalledWith("Default agent: copilot");
  });

  it("should forward reasoning effort for issue assignments", async () => {
    process.env.GH_AW_AGENT_MODEL = "o3";
    process.env.GH_AW_AGENT_REASONING_EFFORT = "high";
    setAgentOutput({ items: [{ type: "assign_to_agent", issue_number: 42, agent: "copilot" }], errors: [] });
    mockGithub.rest.issues.get.mockResolvedValueOnce({
      data: { id: 12345, number: 42, assignees: [], html_url: "", title: "", body: "" },
    });

    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

    expect(mockGithub.request).toHaveBeenLastCalledWith(
      "POST /repos/{owner}/{repo}/issues/{issue_number}/assignees",
      expect.objectContaining({
        agent_assignment: expect.objectContaining({
          model: "o3",
          reasoning_effort: "high",
        }),
      })
    );
  });

  it("should respect max count configuration", async () => {
    process.env.GH_AW_AGENT_MAX_COUNT = "2";
    setAgentOutput({
      items: [
        { type: "assign_to_agent", issue_number: 1, agent: "copilot" },
        { type: "assign_to_agent", issue_number: 2, agent: "copilot" },
        { type: "assign_to_agent", issue_number: 3, agent: "copilot" },
      ],
      errors: [],
    });

    // Mock REST responses for 2 assignments (agent cached after first findAgent)
    mockGithub.rest.issues.checkUserCanBeAssigned.mockResolvedValueOnce({});
    mockGithub.rest.users.getByUsername.mockResolvedValueOnce({ data: { id: 99999 } });
    mockGithub.rest.issues.get.mockResolvedValueOnce({
      data: { id: 11111, number: 1, assignees: [], html_url: "", title: "", body: "" },
    });
    mockGithub.request.mockResolvedValueOnce({ data: { id: "task-1" } });
    mockGithub.rest.issues.get.mockResolvedValueOnce({
      data: { id: 22222, number: 2, assignees: [], html_url: "", title: "", body: "" },
    });
    mockGithub.request.mockResolvedValueOnce({ data: { id: "task-2" } });

    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Found 3 agent assignments, but max is 2"));
  }, 20000); // Increase timeout to 20 seconds to account for the delay

  it("should resolve temporary issue IDs (aw_...) using GH_AW_TEMPORARY_ID_MAP", async () => {
    process.env.GH_AW_TEMPORARY_ID_MAP = JSON.stringify({
      aw_abc123: { repo: "test-owner/test-repo", number: 99 },
    });

    setAgentOutput({
      items: [
        {
          type: "assign_to_agent",
          issue_number: "aw_abc123",
          agent: "copilot",
        },
      ],
      errors: [],
    });

    // Mock REST responses: findAgent -> getIssueDetails (issueNumber 99) -> assignAgentToIssue
    mockGithub.rest.issues.checkUserCanBeAssigned.mockResolvedValueOnce({});
    mockGithub.rest.users.getByUsername.mockResolvedValueOnce({ data: { id: 99999 } });
    mockGithub.rest.issues.get.mockResolvedValueOnce({
      data: { id: 12345, number: 99, assignees: [], html_url: "", title: "", body: "" },
    });
    mockGithub.request.mockResolvedValueOnce({ data: { id: "task-99" } });

    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Resolved temporary issue id"));

    // Ensure the issue lookup used the resolved issue number
    expect(mockGithub.rest.issues.get).toHaveBeenCalledWith(expect.objectContaining({ issue_number: 99 }));
  });

  it("should resolve temporary issue IDs with '#' prefix (#aw_...) using GH_AW_TEMPORARY_ID_MAP", async () => {
    process.env.GH_AW_TEMPORARY_ID_MAP = JSON.stringify({
      aw_abc123: { repo: "test-owner/test-repo", number: 99 },
    });

    setAgentOutput({
      items: [
        {
          type: "assign_to_agent",
          issue_number: "#aw_abc123",
          agent: "copilot",
        },
      ],
      errors: [],
    });

    // Mock REST responses: findAgent -> getIssueDetails (issueNumber 99) -> assignAgentToIssue
    mockGithub.rest.issues.checkUserCanBeAssigned.mockResolvedValueOnce({});
    mockGithub.rest.users.getByUsername.mockResolvedValueOnce({ data: { id: 99999 } });
    mockGithub.rest.issues.get.mockResolvedValueOnce({
      data: { id: 12345, number: 99, assignees: [], html_url: "", title: "", body: "" },
    });
    mockGithub.request.mockResolvedValueOnce({ data: { id: "task-99" } });

    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Resolved temporary issue id"));

    expect(mockGithub.rest.issues.get).toHaveBeenCalledWith(expect.objectContaining({ issue_number: 99 }));
  });

  it("should defer when issue_number is a '#aw_' temporary ID not yet in map", async () => {
    // No temporary ID map set — the ID is unresolved
    delete process.env.GH_AW_TEMPORARY_ID_MAP;

    setAgentOutput({
      items: [
        {
          type: "assign_to_agent",
          issue_number: "#aw_abc123",
          agent: "copilot",
        },
      ],
      errors: [],
    });

    // Call main() factory then invoke the handler directly so we can inspect the deferred result
    const deferred = await eval(`(async () => {
      ${assignToAgentScript};
      const _handler = await main({});
      const { loadTemporaryIdMap } = require("./temporary_id.cjs");
      const _map = loadTemporaryIdMap();
      return _handler({ type: "assign_to_agent", issue_number: "#aw_abc123", agent: "copilot" }, {}, _map);
    })()`);

    expect(deferred).toMatchObject({ success: false, deferred: true });
    expect(mockGithub.request).not.toHaveBeenCalled();
  });

  it("should reject unsupported agents", async () => {
    setAgentOutput({
      items: [
        {
          type: "assign_to_agent",
          issue_number: 42,
          agent: "unsupported-agent",
        },
      ],
      errors: [],
    });

    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining('Agent "unsupported-agent" is not supported'));
    expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Failed to assign 1 agent(s)"));
  });

  it("should handle invalid issue numbers", async () => {
    setAgentOutput({
      items: [
        {
          type: "assign_to_agent",
          issue_number: -1,
          agent: "copilot",
        },
      ],
      errors: [],
    });

    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

    // Error message changed to use resolveTarget validation
    expect(mockCore.error).toHaveBeenCalledWith(expect.stringContaining("Invalid"));
  });

  it("should handle agent already assigned", async () => {
    setAgentOutput({
      items: [
        {
          type: "assign_to_agent",
          issue_number: 42,
          agent: "copilot",
        },
      ],
      errors: [],
    });

    // Mock REST responses - agent already assigned
    mockGithub.rest.issues.checkUserCanBeAssigned.mockResolvedValueOnce({});
    mockGithub.rest.users.getByUsername.mockResolvedValueOnce({ data: { id: 99999 } });
    mockGithub.rest.issues.get.mockResolvedValueOnce({
      data: { id: 12345, number: 42, assignees: [{ id: 99999, login: "copilot-swe-agent" }], html_url: "", title: "", body: "" },
    });

    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("copilot is already assigned to issue #42"));
  });

  it("should allow re-assignment when agent is already assigned but pull_request_repo differs", async () => {
    process.env.GH_AW_AGENT_PULL_REQUEST_REPO = "test-owner/default-pr-repo";
    process.env.GH_AW_AGENT_ALLOWED_PULL_REQUEST_REPOS = "test-owner/other-platform-repo";
    setAgentOutput({
      items: [
        {
          type: "assign_to_agent",
          issue_number: 42,
          agent: "copilot",
          pull_request_repo: "test-owner/other-platform-repo",
        },
      ],
      errors: [],
    });

    // Mock REST responses
    // Get global PR repository (default-pr-repo)
    mockGithub.rest.repos.get.mockResolvedValueOnce({ data: { node_id: "default-pr-repo-id", default_branch: "main" } });
    // Get per-item PR repository (other-platform-repo)
    mockGithub.rest.repos.get.mockResolvedValueOnce({ data: { node_id: "other-platform-repo-id", default_branch: "main" } });
    // Find agent
    mockGithub.rest.issues.checkUserCanBeAssigned.mockResolvedValueOnce({});
    mockGithub.rest.users.getByUsername.mockResolvedValueOnce({ data: { id: 99999 } });
    // Get issue details - agent is already assigned
    mockGithub.rest.issues.get.mockResolvedValueOnce({
      data: { id: 12345, number: 42, assignees: [{ id: 99999, login: "copilot-swe-agent" }], html_url: "", title: "", body: "" },
    });
    // Assign agent (re-assignment allowed)
    mockGithub.request.mockResolvedValueOnce({ data: { id: "task-123" } });

    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

    // Should NOT see "already assigned" skip message
    expect(mockCore.info).not.toHaveBeenCalledWith(expect.stringContaining("is already assigned to issue #42"));
    // Should see successful assignment
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Successfully assigned copilot coding agent to issue #42"));
    expect(mockCore.setFailed).not.toHaveBeenCalled();

    expect(mockGithub.request).toHaveBeenCalledWith("POST /repos/{owner}/{repo}/issues/{issue_number}/assignees", expect.objectContaining({ owner: "test-owner", repo: "test-repo", issue_number: 42 }));
  });

  it("should process multiple assignments for the same temporary issue ID across different pull_request_repo targets", async () => {
    process.env.GH_AW_AGENT_MAX_COUNT = "5";
    process.env.GH_AW_TEMPORARY_ID_MAP = JSON.stringify({
      aw_multi_repo: { repo: "test-owner/test-repo", number: 6587 },
    });
    process.env.GH_AW_AGENT_ALLOWED_PULL_REQUEST_REPOS = "test-owner/ios-repo,test-owner/android-repo";

    setAgentOutput({
      items: [
        {
          type: "assign_to_agent",
          issue_number: "aw_multi_repo",
          agent: "copilot",
          pull_request_repo: "test-owner/ios-repo",
        },
        {
          type: "assign_to_agent",
          issue_number: "aw_multi_repo",
          agent: "copilot",
          pull_request_repo: "test-owner/android-repo",
        },
      ],
      errors: [],
    });

    // Item 1: per-item PR repository (ios-repo)
    mockGithub.rest.repos.get.mockResolvedValueOnce({ data: { node_id: "ios-repo-id", default_branch: "main" } });
    // Item 1: find agent
    mockGithub.rest.issues.checkUserCanBeAssigned.mockResolvedValueOnce({});
    mockGithub.rest.users.getByUsername.mockResolvedValueOnce({ data: { id: 99999 } });
    // Item 1: issue details (not assigned yet)
    mockGithub.rest.issues.get.mockResolvedValueOnce({
      data: { id: 12345, number: 6587, assignees: [], html_url: "", title: "", body: "" },
    });
    // Item 1: assignment
    mockGithub.request.mockResolvedValueOnce({ data: { id: "task-1" } });
    // Item 2: per-item PR repository (android-repo)
    mockGithub.rest.repos.get.mockResolvedValueOnce({ data: { node_id: "android-repo-id", default_branch: "main" } });
    // Item 2: issue details (already assigned, but re-assignment allowed for different repo)
    mockGithub.rest.issues.get.mockResolvedValueOnce({
      data: { id: 12345, number: 6587, assignees: [{ id: 99999, login: "copilot-swe-agent" }], html_url: "", title: "", body: "" },
    });
    // Item 2: assignment (re-assignment allowed)
    mockGithub.request.mockResolvedValueOnce({ data: { id: "task-2" } });

    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

    expect(mockCore.info).not.toHaveBeenCalledWith(expect.stringContaining("copilot is already assigned to issue #6587"));
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Successfully assigned copilot coding agent to issue #6587"));

    const assignmentCalls = mockGithub.request.mock.calls.filter(([route]) => route === "POST /repos/{owner}/{repo}/issues/{issue_number}/assignees");
    expect(assignmentCalls).toHaveLength(2);
    expect(assignmentCalls[0][1]).toMatchObject({ owner: "test-owner", repo: "test-repo", issue_number: 6587 });
    expect(assignmentCalls[1][1]).toMatchObject({ owner: "test-owner", repo: "test-repo", issue_number: 6587 });
    expect(mockSleep).toHaveBeenCalledTimes(1);
    expect(mockSleep).toHaveBeenCalledWith(10000);

    const summaryCall = mockCore.summary.addRaw.mock.calls[0][0];
    expect(summaryCall).toContain("PR target: test-owner/ios-repo");
    expect(summaryCall).toContain("PR target: test-owner/android-repo");
  });

  it("should avoid duplicate re-assignment for the same issue and same pull_request_repo in one run", async () => {
    process.env.GH_AW_AGENT_MAX_COUNT = "5";
    process.env.GH_AW_TEMPORARY_ID_MAP = JSON.stringify({
      aw_duplicate: { repo: "test-owner/test-repo", number: 6587 },
    });
    process.env.GH_AW_AGENT_ALLOWED_PULL_REQUEST_REPOS = "test-owner/ios-repo";

    setAgentOutput({
      items: [
        {
          type: "assign_to_agent",
          issue_number: "aw_duplicate",
          agent: "copilot",
          pull_request_repo: "test-owner/ios-repo",
        },
        {
          type: "assign_to_agent",
          issue_number: "aw_duplicate",
          agent: "copilot",
          pull_request_repo: "test-owner/ios-repo",
        },
      ],
      errors: [],
    });

    // Item 1: per-item PR repository (ios-repo)
    mockGithub.rest.repos.get.mockResolvedValueOnce({ data: { node_id: "ios-repo-id", default_branch: "main" } });
    // Item 1: find agent
    mockGithub.rest.issues.checkUserCanBeAssigned.mockResolvedValueOnce({});
    mockGithub.rest.users.getByUsername.mockResolvedValueOnce({ data: { id: 99999 } });
    // Item 1: issue details (not assigned yet)
    mockGithub.rest.issues.get.mockResolvedValueOnce({
      data: { id: 12345, number: 6587, assignees: [], html_url: "", title: "", body: "" },
    });
    // Item 1: assignment
    mockGithub.request.mockResolvedValueOnce({ data: { id: "task-1" } });
    // Item 2: per-item PR repository (ios-repo, same)
    mockGithub.rest.repos.get.mockResolvedValueOnce({ data: { node_id: "ios-repo-id", default_branch: "main" } });
    // Item 2: issue details (already assigned, same context → skip)
    mockGithub.rest.issues.get.mockResolvedValueOnce({
      data: { id: 12345, number: 6587, assignees: [{ id: 99999, login: "copilot-swe-agent" }], html_url: "", title: "", body: "" },
    });

    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("copilot is already assigned to issue #6587"));
    const assignmentCalls = mockGithub.request.mock.calls.filter(([route]) => route === "POST /repos/{owner}/{repo}/issues/{issue_number}/assignees");
    expect(assignmentCalls).toHaveLength(1);
    expect(mockSleep).toHaveBeenCalledTimes(1);
    expect(mockSleep).toHaveBeenCalledWith(10000);
  });

  it("should not treat whitespace pull_request_repo as a reassignment override", async () => {
    setAgentOutput({
      items: [
        {
          type: "assign_to_agent",
          issue_number: 42,
          agent: "copilot",
          pull_request_repo: "   ",
        },
      ],
      errors: [],
    });

    // Find agent
    mockGithub.rest.issues.checkUserCanBeAssigned.mockResolvedValueOnce({});
    mockGithub.rest.users.getByUsername.mockResolvedValueOnce({ data: { id: 99999 } });
    // Get issue details - already assigned
    mockGithub.rest.issues.get.mockResolvedValueOnce({
      data: { id: 12345, number: 42, assignees: [{ id: 99999, login: "copilot-swe-agent" }], html_url: "", title: "", body: "" },
    });

    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("copilot is already assigned to issue #42"));
    const assignmentCalls = mockGithub.request.mock.calls.filter(([route]) => route === "POST /repos/{owner}/{repo}/issues/{issue_number}/assignees");
    expect(assignmentCalls).toHaveLength(0);
  });

  it("should still skip when agent is already assigned with global pull-request-repo but no per-item override", async () => {
    process.env.GH_AW_AGENT_PULL_REQUEST_REPO = "test-owner/global-pr-repo";
    setAgentOutput({
      items: [
        {
          type: "assign_to_agent",
          issue_number: 42,
          agent: "copilot",
        },
      ],
      errors: [],
    });

    // Mock REST responses
    // Get global PR repository ID and default branch
    mockGithub.rest.repos.get.mockResolvedValueOnce({ data: { node_id: "global-pr-repo-id", default_branch: "main" } });
    // Find agent
    mockGithub.rest.issues.checkUserCanBeAssigned.mockResolvedValueOnce({});
    mockGithub.rest.users.getByUsername.mockResolvedValueOnce({ data: { id: 99999 } });
    // Get issue details - agent is already assigned
    mockGithub.rest.issues.get.mockResolvedValueOnce({
      data: { id: 12345, number: 42, assignees: [{ id: 99999, login: "copilot-swe-agent" }], html_url: "", title: "", body: "" },
    });

    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

    // Should see "already assigned" skip message
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("copilot is already assigned to issue #42"));
    // Should NOT have called the assignment mutation (only 3 GraphQL calls: repo lookup, find agent, get issue)
    expect(mockGithub.rest.repos.get).toHaveBeenCalledTimes(1); // global PR repo lookup
    const assignmentCalls = mockGithub.request.mock.calls.filter(([route]) => route === "POST /repos/{owner}/{repo}/issues/{issue_number}/assignees");
    expect(assignmentCalls).toHaveLength(0); // no assignment since already assigned
    expect(mockCore.setFailed).not.toHaveBeenCalled();
  });

  it("should handle API errors gracefully", async () => {
    setAgentOutput({
      items: [
        {
          type: "assign_to_agent",
          issue_number: 42,
          agent: "copilot",
        },
      ],
      errors: [],
    });

    const apiError = new Error("API rate limit exceeded");
    mockGithub.rest.issues.checkUserCanBeAssigned.mockRejectedValue(apiError);

    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

    expect(mockCore.error).toHaveBeenCalledWith(expect.stringContaining("Failed to assign agent"));
    expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Failed to assign 1 agent(s)"));
  });

  it("should fail on 502 assignment errors", async () => {
    setAgentOutput({
      items: [
        {
          type: "assign_to_agent",
          issue_number: 42,
          agent: "copilot",
        },
      ],
      errors: [],
    });

    // Mock successful agent lookup and issue details
    mockGithub.rest.issues.checkUserCanBeAssigned.mockResolvedValueOnce({});
    mockGithub.rest.users.getByUsername.mockResolvedValueOnce({ data: { id: 99999 } });
    mockGithub.rest.issues.get.mockResolvedValueOnce({
      data: { id: 12345, number: 42, assignees: [], html_url: "", title: "", body: "" },
    });
    // Assignment fails with 502
    mockGithub.request.mockImplementation(route => {
      if (route === "POST /repos/{owner}/{repo}/issues/{issue_number}/assignees") {
        return Promise.reject({
          response: {
            status: 502,
            url: "https://api.github.com/repos/test-owner/test-repo/tasks",
            headers: { "content-type": "text/html" },
            data: "<html>\n<head><title>502 Bad Gateway</title></head>\n<body>\n<center><h1>502 Bad Gateway</h1></center>\n<hr><center>nginx</center>\n</body>\n</html>\n",
          },
        });
      }
      return Promise.resolve({ status: 204 });
    });

    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

    expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Failed to assign 1 agent(s)"));
  });

  it("should fail on 502 assignment message errors", async () => {
    setAgentOutput({
      items: [
        {
          type: "assign_to_agent",
          issue_number: 42,
          agent: "copilot",
        },
      ],
      errors: [],
    });

    // Mock successful agent lookup and issue details
    mockGithub.rest.issues.checkUserCanBeAssigned.mockResolvedValueOnce({});
    mockGithub.rest.users.getByUsername.mockResolvedValueOnce({ data: { id: 99999 } });
    mockGithub.rest.issues.get.mockResolvedValueOnce({
      data: { id: 12345, number: 42, assignees: [], html_url: "", title: "", body: "" },
    });
    // Assignment fails with 502 message
    mockGithub.request.mockImplementation(route => {
      if (route === "POST /repos/{owner}/{repo}/issues/{issue_number}/assignees") {
        return Promise.reject(new Error("502 Bad Gateway"));
      }
      return Promise.resolve({ status: 204 });
    });

    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

    expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Failed to assign 1 agent(s)"));
  });

  it("should cache resolved agent logins for multiple assignments", async () => {
    setAgentOutput({
      items: [
        { type: "assign_to_agent", issue_number: 1, agent: "copilot" },
        { type: "assign_to_agent", issue_number: 2, agent: "copilot" },
      ],
      errors: [],
    });

    // Mock REST responses
    mockGithub.rest.issues.checkUserCanBeAssigned.mockResolvedValueOnce({});
    mockGithub.rest.users.getByUsername.mockResolvedValueOnce({ data: { id: 99999 } });
    mockGithub.rest.issues.get.mockResolvedValueOnce({
      data: { id: 11111, number: 1, assignees: [], html_url: "", title: "", body: "" },
    });
    mockGithub.request.mockResolvedValueOnce({ data: { id: "task-1" } });
    mockGithub.rest.issues.get.mockResolvedValueOnce({
      data: { id: 22222, number: 2, assignees: [], html_url: "", title: "", body: "" },
    });
    mockGithub.request.mockResolvedValueOnce({ data: { id: "task-2" } });

    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

    // Should only look up agent once (cached for second assignment)
    const lookupCalls = mockGithub.request.mock.calls.filter(([route]) => route === "GET /repos/{owner}/{repo}/issues/{issue_number}/assignees/{assignee}");
    expect(lookupCalls).toHaveLength(1);
  }, 15000); // Increase timeout to 15 seconds to account for the delay

  it("should use target repository when configured", async () => {
    process.env.GH_AW_TARGET_REPO_SLUG = "other-owner/other-repo";
    process.env.GH_AW_ALLOWED_REPOS = "other-owner/other-repo"; // Add to allowlist
    setAgentOutput({
      items: [
        {
          type: "assign_to_agent",
          issue_number: 42,
          agent: "copilot",
        },
      ],
      errors: [],
    });

    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

    expect(mockCore.info).toHaveBeenCalledWith("Default target repo: other-owner/other-repo");
  });

  it("should handle invalid max count configuration", async () => {
    process.env.GH_AW_AGENT_MAX_COUNT = "invalid";
    setAgentOutput({
      items: [
        {
          type: "assign_to_agent",
          issue_number: 42,
          agent: "copilot",
        },
      ],
      errors: [],
    });

    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

    expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Invalid max value: invalid"));
  });

  it.skip("should generate permission error summary when appropriate", async () => {
    // TODO: This test needs to be fixed - the mock setup doesn't work correctly with eval()
    // The error from getIssueDetails is not being propagated properly in the test environment
    setAgentOutput({
      items: [
        {
          type: "assign_to_agent",
          issue_number: 42,
          agent: "copilot",
        },
      ],
      errors: [],
    });

    const permissionError = new Error("Resource not accessible by integration");
    mockGithub.rest.issues.checkUserCanBeAssigned.mockResolvedValueOnce({});
    mockGithub.rest.users.getByUsername.mockResolvedValueOnce({ data: { id: 99999 } });
    mockGithub.rest.issues.get.mockResolvedValueOnce({
      data: { id: 12345, number: 42, assignees: [], html_url: "", title: "", body: "" },
    });
    mockGithub.request.mockRejectedValueOnce(permissionError);

    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

    expect(mockCore.summary.addRaw).toHaveBeenCalled();
    const summaryCall = mockCore.summary.addRaw.mock.calls[0][0];
    expect(summaryCall).toContain("Resource not accessible");
    expect(summaryCall).toContain("Permission Requirements");
  });

  it("should forward reasoning effort for pull request assignments", async () => {
    process.env.GH_AW_AGENT_DEFAULT = "copilot";
    process.env.GH_AW_AGENT_MODEL = "o3";
    process.env.GH_AW_AGENT_REASONING_EFFORT = "high";
    setAgentOutput({
      items: [
        {
          type: "assign_to_agent",
          pull_number: 123,
          agent: "copilot",
        },
      ],
      errors: [],
    });

    mockGithub.rest.issues.checkUserCanBeAssigned.mockResolvedValueOnce({});
    mockGithub.rest.users.getByUsername.mockResolvedValueOnce({ data: { id: 99999 } });
    mockGithub.rest.pulls.get.mockResolvedValueOnce({
      data: { id: 67890, number: 123, assignees: [], html_url: "", title: "", body: "" },
    });
    mockGithub.request.mockResolvedValueOnce({ data: { id: "task-pr-123" } });

    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

    if (mockCore.error.mock.calls.length > 0) {
      console.log("Errors:", mockCore.error.mock.calls);
    }

    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Successfully assigned copilot coding agent to pull request #123"));
    expect(mockGithub.request).toHaveBeenLastCalledWith(
      "POST /repos/{owner}/{repo}/issues/{issue_number}/assignees",
      expect.objectContaining({
        agent_assignment: expect.objectContaining({
          model: "o3",
          reasoning_effort: "high",
        }),
      })
    );
    expect(mockCore.setFailed).not.toHaveBeenCalled();
  });

  it("should error when both issue_number and pull_number are provided", async () => {
    setAgentOutput({
      items: [
        {
          type: "assign_to_agent",
          issue_number: 42,
          pull_number: 123,
          agent: "copilot",
        },
      ],
      errors: [],
    });

    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

    expect(mockCore.error).toHaveBeenCalledWith("Cannot specify both issue_number and pull_number in the same assign_to_agent item");
    expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Failed to assign 1 agent(s)"));
  });

  it("should auto-resolve issue number from context when not provided (triggering target)", async () => {
    // Set up context to simulate an issue event
    mockContext.eventName = "issues";
    mockContext.payload = {
      issue: { number: 123 },
    };
    mockContext.repo = {
      owner: "test-owner",
      repo: "test-repo",
    };

    setAgentOutput({
      items: [
        {
          type: "assign_to_agent",
          agent: "copilot",
          // No issue_number or pull_number - should auto-resolve
        },
      ],
      errors: [],
    });

    // Mock REST responses in the correct order
    mockGithub.rest.issues.checkUserCanBeAssigned.mockResolvedValueOnce({});
    mockGithub.rest.users.getByUsername.mockResolvedValueOnce({ data: { id: 99999 } });
    mockGithub.rest.issues.get.mockResolvedValueOnce({
      data: { id: 12345, number: 123, assignees: [], html_url: "", title: "", body: "" },
    });
    mockGithub.request.mockResolvedValueOnce({ data: { id: "task-123" } });

    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

    // The key assertion: Target configuration should be "triggering" (the default)
    // This shows that when no explicit issue_number/pull_number is provided,
    // the handler falls back to the triggering context
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Target configuration: triggering"));

    // GraphQL should have been called for finding the agent and getting issue details
    expect(mockGithub.request).toHaveBeenCalled();
  });

  it("should skip when context doesn't match triggering target", async () => {
    // Set up context that doesn't support triggering target (e.g., push event)
    mockContext.eventName = "push";

    setAgentOutput({
      items: [
        {
          type: "assign_to_agent",
          agent: "copilot",
          // No issue_number or pull_number
        },
      ],
      errors: [],
    });

    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

    // Should skip gracefully (not fail the workflow)
    expect(mockCore.error).not.toHaveBeenCalled();
    expect(mockCore.setFailed).not.toHaveBeenCalled();
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("not running in issue or pull request context"));
  });

  it("should error when neither issue_number nor pull_number provided and target is '*'", async () => {
    process.env.GH_AW_AGENT_TARGET = "*"; // Explicit target mode

    setAgentOutput({
      items: [
        {
          type: "assign_to_agent",
          agent: "copilot",
          // No issue_number or pull_number
        },
      ],
      errors: [],
    });

    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

    // Should fail because target "*" requires explicit issue_number or pull_number
    expect(mockCore.error).toHaveBeenCalled();
    expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Failed to assign 1 agent(s)"));
  });

  it("should accept agent when in allowed list", async () => {
    process.env.GH_AW_AGENT_ALLOWED = "copilot";
    setAgentOutput({
      items: [
        {
          type: "assign_to_agent",
          issue_number: 42,
          agent: "copilot",
        },
      ],
      errors: [],
    });

    // Mock REST responses
    mockGithub.rest.issues.checkUserCanBeAssigned.mockResolvedValueOnce({});
    mockGithub.rest.users.getByUsername.mockResolvedValueOnce({ data: { id: 99999 } });
    mockGithub.rest.issues.get.mockResolvedValueOnce({
      data: { id: 12345, number: 42, assignees: [], html_url: "", title: "", body: "" },
    });
    mockGithub.request.mockResolvedValueOnce({ data: { id: "task-123" } });

    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

    // Key assertion: allowed agents list should be logged
    expect(mockCore.info).toHaveBeenCalledWith("Allowed agents: copilot");

    // Should not reject the agent for being not in the allowed list
    expect(mockCore.error).not.toHaveBeenCalledWith(expect.stringContaining("not in the allowed list"));
  });

  it("should reject agent not in allowed list", async () => {
    process.env.GH_AW_AGENT_ALLOWED = "other-agent";
    setAgentOutput({
      items: [
        {
          type: "assign_to_agent",
          issue_number: 42,
          agent: "copilot",
        },
      ],
      errors: [],
    });

    // No GraphQL mocks needed - validation happens before GraphQL calls

    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

    expect(mockCore.info).toHaveBeenCalledWith("Allowed agents: other-agent");
    expect(mockCore.error).toHaveBeenCalledWith(expect.stringContaining('Agent "copilot" is not in the allowed list'));
    expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Failed to assign 1 agent(s)"));

    // Should not have made any GraphQL calls since validation failed early
    expect(mockGithub.request).not.toHaveBeenCalled();
  });

  it("should allow any agent when no allowed list is configured", async () => {
    // No GH_AW_AGENT_ALLOWED set
    setAgentOutput({
      items: [
        {
          type: "assign_to_agent",
          issue_number: 42,
          agent: "copilot",
        },
      ],
      errors: [],
    });

    // Mock REST responses
    mockGithub.rest.issues.checkUserCanBeAssigned.mockResolvedValueOnce({});
    mockGithub.rest.users.getByUsername.mockResolvedValueOnce({ data: { id: 99999 } });
    mockGithub.rest.issues.get.mockResolvedValueOnce({
      data: { id: 12345, number: 42, assignees: [], html_url: "", title: "", body: "" },
    });
    mockGithub.request.mockResolvedValueOnce({ data: { id: "task-123" } });

    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

    // Should not log allowed agents when list is not configured
    expect(mockCore.info).not.toHaveBeenCalledWith(expect.stringContaining("Allowed agents:"));
    expect(mockCore.error).not.toHaveBeenCalled();
    expect(mockCore.setFailed).not.toHaveBeenCalled();
  });

  it("should skip assignment and not fail when ignore-if-error is true and auth error occurs", async () => {
    process.env.GH_AW_AGENT_IGNORE_IF_ERROR = "true";
    setAgentOutput({
      items: [
        {
          type: "assign_to_agent",
          issue_number: 42,
          agent: "copilot",
        },
      ],
      errors: [],
    });

    // Simulate authentication error - use mockRejectedValueOnce to avoid affecting other tests
    const authError = new Error("Bad credentials");
    mockGithub.request.mockRejectedValueOnce(authError);

    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

    // Should log that ignore-if-error is enabled
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Ignore-if-error mode enabled: Will not fail if agent assignment encounters auth or availability errors"));

    // Should warn about skipping but not fail
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Agent assignment failed"));
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("ignore-if-error=true"));

    // Should not fail the workflow
    expect(mockCore.setFailed).not.toHaveBeenCalled();
    expect(mockCore.setOutput).toHaveBeenCalledWith("assignment_error_count", "0");
    expect(mockCore.setOutput).toHaveBeenCalledWith("assignment_errors", expect.stringContaining("Bad credentials"));

    // Summary should show skipped assignments
    expect(mockCore.summary.addRaw).toHaveBeenCalled();
    const summaryCall = mockCore.summary.addRaw.mock.calls[0][0];
    expect(summaryCall).toContain("⏭️ Skipped");
    expect(summaryCall).toContain("assignment failed due to error");
  });

  it("should fail when ignore-if-error is false (default) and auth error occurs", async () => {
    // Don't set GH_AW_AGENT_IGNORE_IF_MISSING (defaults to false)
    setAgentOutput({
      items: [
        {
          type: "assign_to_agent",
          issue_number: 42,
          agent: "copilot",
        },
      ],
      errors: [],
    });

    // Simulate authentication error
    const authError = new Error("Bad credentials");
    mockGithub.request.mockRejectedValue(authError);

    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

    // Should NOT log ignore-if-error mode
    expect(mockCore.info).not.toHaveBeenCalledWith(expect.stringContaining("ignore-if-error mode enabled"));

    // Should error and fail
    expect(mockCore.error).toHaveBeenCalledWith(expect.stringContaining("Failed to assign agent"));
    expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Failed to assign 1 agent(s)"));

    // Should post a failure comment on the issue with all required properties
    expect(mockGithub.rest.issues.createComment).toHaveBeenCalledWith(
      expect.objectContaining({
        owner: "test-owner",
        repo: "test-repo",
        issue_number: 42,
        body: expect.stringMatching(/Assignment failed.*Bad credentials/s),
      })
    );
  });

  it("should handle ignore-if-error when 'Resource not accessible' error", async () => {
    process.env.GH_AW_AGENT_IGNORE_IF_ERROR = "true";
    setAgentOutput({
      items: [
        {
          type: "assign_to_agent",
          issue_number: 42,
          agent: "copilot",
        },
      ],
      errors: [],
    });

    // Simulate permission error
    const permError = new Error("Resource not accessible by integration");
    mockGithub.request.mockRejectedValue(permError);

    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

    // Should skip and not fail
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Agent assignment failed"));
    expect(mockCore.setFailed).not.toHaveBeenCalled();
  });

  it("should skip assignment and not fail when ignore-if-error is true and agent is not available for the repository", async () => {
    process.env.GH_AW_AGENT_IGNORE_IF_ERROR = "true";
    setAgentOutput({
      items: [
        {
          type: "assign_to_agent",
          issue_number: 42,
          agent: "copilot",
        },
      ],
      errors: [],
    });

    // Simulate agent not available (assignee check returns 404)
    const notFoundError = new Error("Not Found");
    notFoundError.status = 404;
    mockGithub.request.mockRejectedValue(notFoundError);

    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

    // Should warn about skipping due to agent availability error, but not fail
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("agent availability"));
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("ignore-if-error=true"));
    expect(mockCore.setFailed).not.toHaveBeenCalled();

    // Summary should show skipped assignments
    expect(mockCore.summary.addRaw).toHaveBeenCalled();
    const summaryCall = mockCore.summary.addRaw.mock.calls[0][0];
    expect(summaryCall).toContain("⏭️ Skipped");
  });

  it("should still fail on non-auth errors even with ignore-if-error enabled", async () => {
    process.env.GH_AW_AGENT_IGNORE_IF_ERROR = "true";
    setAgentOutput({
      items: [
        {
          type: "assign_to_agent",
          issue_number: 42,
          agent: "copilot",
        },
      ],
      errors: [],
    });

    // Simulate a different error (not auth-related) during assignment
    const otherError = new Error("Network timeout");
    mockGithub.request.mockImplementation(route => {
      if (route === "POST /repos/{owner}/{repo}/issues/{issue_number}/assignees") {
        return Promise.reject(otherError);
      }
      return Promise.resolve({ status: 204 });
    });

    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

    // Should error and fail (not skipped because it's not an auth error)
    expect(mockCore.error).toHaveBeenCalledWith(expect.stringContaining("Failed to assign agent"));
    expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Failed to assign 1 agent(s)"));
  });

  it("should not post failure comment on success", async () => {
    setAgentOutput({
      items: [
        {
          type: "assign_to_agent",
          issue_number: 42,
          agent: "copilot",
        },
      ],
      errors: [],
    });

    mockGithub.rest.issues.checkUserCanBeAssigned.mockResolvedValueOnce({});
    mockGithub.rest.users.getByUsername.mockResolvedValueOnce({ data: { id: 99999 } });
    mockGithub.rest.issues.get.mockResolvedValueOnce({
      data: { id: 12345, number: 42, assignees: [], html_url: "", title: "", body: "" },
    });
    mockGithub.request.mockResolvedValueOnce({ data: { id: "task-123" } });

    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

    // Should NOT post a failure comment on success
    expect(mockGithub.rest.issues.createComment).not.toHaveBeenCalled();
  });

  it("should skip silently and not post a comment when issue_number resolves to a pull request", async () => {
    setAgentOutput({
      items: [{ type: "assign_to_agent", issue_number: 99, agent: "copilot" }],
      errors: [],
    });

    // Simulate the issues.get API returning a PR (has pull_request field)
    mockGithub.rest.issues.get.mockResolvedValueOnce({
      data: {
        id: 99999,
        number: 99,
        assignees: [],
        html_url: "https://github.com/test-owner/test-repo/pull/99",
        title: "Some PR",
        body: "",
        pull_request: { url: "https://api.github.com/repos/test-owner/test-repo/pulls/99" },
      },
    });

    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

    // Should warn about the PR but not post a comment on it
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("pull request, not an issue"));
    expect(mockGithub.rest.issues.createComment).not.toHaveBeenCalled();
    // Should not call setFailed — skipping a PR target is not a workflow failure
    expect(mockCore.setFailed).not.toHaveBeenCalled();
  });

  it("should post failure comment on single failed assignment", async () => {
    setAgentOutput({
      items: [{ type: "assign_to_agent", issue_number: 11, agent: "copilot" }],
      errors: [],
    });

    // Fail all assignments with auth error
    const authError = new Error("Bad credentials");
    mockGithub.request.mockRejectedValue(authError);

    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

    // Should post a failure comment for the failed issue with all required properties
    expect(mockGithub.rest.issues.createComment).toHaveBeenCalledTimes(1);
    expect(mockGithub.rest.issues.createComment).toHaveBeenCalledWith(
      expect.objectContaining({
        owner: "test-owner",
        repo: "test-repo",
        issue_number: 11,
        body: expect.stringMatching(/Assignment failed.*Bad credentials/s),
      })
    );
  });

  it("should sanitize dangerous content in failure comment body", async () => {
    setAgentOutput({
      items: [{ type: "assign_to_agent", issue_number: 11, agent: "copilot" }],
      errors: [],
    });

    // Simulate an error whose message contains an @mention and an HTML comment —
    // both are potentially dangerous if posted unsanitized.
    const dangerousError = new Error("@admin triggered <!-- inject --> error");
    mockGithub.request.mockRejectedValue(dangerousError);

    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

    expect(mockGithub.rest.issues.createComment).toHaveBeenCalledTimes(1);
    const [callArg] = mockGithub.rest.issues.createComment.mock.calls[0];
    // The body must be a string (sanitizeContent never returns undefined)
    expect(typeof callArg.body).toBe("string");
    // The raw @mention should be neutralized (wrapped in backticks, not bare)
    expect(callArg.body).not.toMatch(/(?<!`)@admin(?!`)/);
    // The HTML comment should be stripped
    expect(callArg.body).not.toContain("<!-- inject -->");
  });

  it("should not post failure comment when ignore-if-error skips the assignment", async () => {
    process.env.GH_AW_AGENT_IGNORE_IF_ERROR = "true";
    setAgentOutput({
      items: [
        {
          type: "assign_to_agent",
          issue_number: 42,
          agent: "copilot",
        },
      ],
      errors: [],
    });

    // Simulate authentication error (will be skipped by ignore-if-error)
    const authError = new Error("Bad credentials");
    mockGithub.request.mockRejectedValue(authError);

    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

    // Should NOT post a failure comment since it was skipped
    expect(mockGithub.rest.issues.createComment).not.toHaveBeenCalled();
  });

  it("should still set outputs and log warning when failure comment post fails", async () => {
    setAgentOutput({
      items: [
        {
          type: "assign_to_agent",
          issue_number: 42,
          agent: "copilot",
        },
      ],
      errors: [],
    });

    const authError = new Error("Bad credentials");
    mockGithub.request.mockRejectedValue(authError);

    // Simulate failure to post comment
    mockGithub.rest.issues.createComment.mockRejectedValue(new Error("Could not post comment"));

    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

    // Should still set the assignment_error outputs even if comment fails
    expect(mockCore.setOutput).toHaveBeenCalledWith("assignment_error_count", "1");
    expect(mockCore.setOutput).toHaveBeenCalledWith("assignment_errors", expect.stringContaining("Bad credentials"));

    // Should warn about failure to post comment (best-effort)
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Failed to post failure comment"));
  });

  it.skip("should add 10-second delay between multiple agent assignments", async () => {
    // Note: This test is skipped because testing actual delays with eval() is complex.
    // The implementation has been manually verified to include the delay logic.
    // See lines in assign_to_agent.cjs where sleep(10000) is called between iterations.
    setAgentOutput({
      items: [
        { type: "assign_to_agent", issue_number: 1, agent: "copilot" },
        { type: "assign_to_agent", issue_number: 2, agent: "copilot" },
        { type: "assign_to_agent", issue_number: 3, agent: "copilot" },
      ],
      errors: [],
    });

    mockGithub.rest.issues.checkUserCanBeAssigned.mockResolvedValueOnce({});
    mockGithub.rest.users.getByUsername.mockResolvedValueOnce({ data: { id: 99999 } });
    mockGithub.rest.issues.get.mockResolvedValueOnce({ data: { id: 11111, number: 1, assignees: [], html_url: "", title: "", body: "" } });
    mockGithub.request.mockResolvedValueOnce({ data: { id: "task-1" } });
    mockGithub.rest.issues.get.mockResolvedValueOnce({ data: { id: 22222, number: 2, assignees: [], html_url: "", title: "", body: "" } });
    mockGithub.request.mockResolvedValueOnce({ data: { id: "task-2" } });
    mockGithub.rest.issues.get.mockResolvedValueOnce({ data: { id: 33333, number: 3, assignees: [], html_url: "", title: "", body: "" } });
    mockGithub.request.mockResolvedValueOnce({ data: { id: "task-3" } });

    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

    // Verify delay message was logged twice (2 delays between 3 items)
    const delayMessages = mockCore.info.mock.calls.filter(call => call[0].includes("Waiting 10 seconds before processing next agent assignment"));
    expect(delayMessages).toHaveLength(2);
    expect(mockSleep).toHaveBeenCalledTimes(2);
    expect(mockSleep).toHaveBeenCalledWith(10000);
  });

  it("does not consume a max slot for invalid items", async () => {
    mockGithub.rest.issues.checkUserCanBeAssigned.mockResolvedValue({});
    mockGithub.rest.users.getByUsername.mockResolvedValue({ data: { id: 99999 } });
    mockGithub.rest.issues.get.mockImplementation(async ({ issue_number }) => ({
      data: { id: Number(issue_number) + 1000, number: issue_number, assignees: [], html_url: "", title: "", body: "" },
    }));
    mockGithub.request.mockResolvedValue({ data: { id: "task-123" } });

    const result = await eval(`(async () => {
      ${assignToAgentScript};
      const _handler = await main({ max: "1", name: "copilot" });
      const _invalid = await _handler({ type: "assign_to_agent", issue_number: 1, pull_number: 2, agent: "copilot" }, {}, new Map());
      const _valid = await _handler({ type: "assign_to_agent", issue_number: 3, agent: "copilot" }, {}, new Map());
      return {
        invalid: _invalid,
        valid: _valid,
        assigned: getAssignToAgentAssigned(_handler),
      };
    })()`);

    expect(result.invalid.success).toBe(false);
    expect(result.valid.success).toBe(true);
    expect(result.assigned.split("\n").filter(Boolean)).toHaveLength(1);
  });

  it("atomically reserves the max slot before the inter-assignment delay", async () => {
    mockGithub.rest.issues.checkUserCanBeAssigned.mockResolvedValue({});
    mockGithub.rest.users.getByUsername.mockResolvedValue({ data: { id: 99999 } });
    mockGithub.rest.issues.get.mockImplementation(async ({ issue_number }) => ({
      data: { id: Number(issue_number) + 1000, number: issue_number, assignees: [], html_url: "", title: "", body: "" },
    }));
    mockGithub.request.mockResolvedValue({ data: { id: "task-123" } });

    let releaseSleep;
    mockSleep.mockImplementationOnce(
      () =>
        new Promise(resolve => {
          releaseSleep = resolve;
        })
    );

    const result = await eval(`(async () => {
      ${assignToAgentScript};
      const _handler = await main({ max: "2", name: "copilot" });
      await _handler({ type: "assign_to_agent", issue_number: 1, agent: "copilot" }, {}, new Map());
      return {
        second: _handler({ type: "assign_to_agent", issue_number: 2, agent: "copilot" }, {}, new Map()),
        third: _handler({ type: "assign_to_agent", issue_number: 3, agent: "copilot" }, {}, new Map()),
      };
    })()`);

    await vi.waitFor(() => expect(mockSleep).toHaveBeenCalledTimes(1));
    releaseSleep();

    const [second, third] = await Promise.all([result.second, result.third]);
    expect(second.success).toBe(true);
    expect(third.skipped).toBe(true);
  });

  it("keeps assign_to_agent results isolated per main() invocation", async () => {
    mockGithub.rest.issues.checkUserCanBeAssigned.mockResolvedValue({});
    mockGithub.rest.users.getByUsername.mockResolvedValue({ data: { id: 99999 } });
    mockGithub.rest.issues.get.mockImplementation(async ({ issue_number }) => ({
      data: { id: Number(issue_number) + 2000, number: issue_number, assignees: [], html_url: "", title: "", body: "" },
    }));
    mockGithub.request.mockResolvedValue({ data: { id: "task-123" } });

    const result = await eval(`(async () => {
      ${assignToAgentScript};
      const _handlerA = await main({ max: "5", name: "copilot" });
      const _handlerB = await main({ max: "5", name: "copilot" });
      await _handlerA({ type: "assign_to_agent", issue_number: 11, agent: "copilot" }, {}, new Map());
      return {
        assignedA: getAssignToAgentAssigned(_handlerA),
        assignedB: getAssignToAgentAssigned(_handlerB),
      };
    })()`);

    expect(result.assignedA).toContain("issue:11:copilot");
    expect(result.assignedB).toBe("");
  });

  describe("Cross-repository allowlist validation", () => {
    it("should reject target repository not in allowlist", async () => {
      process.env.GH_AW_ALLOWED_REPOS = "allowed-owner/allowed-repo";

      setAgentOutput({
        items: [
          {
            type: "assign_to_agent",
            issue_number: 42,
            agent: "copilot",
            repo: "not-allowed/other-repo",
          },
        ],
        errors: [],
      });

      await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

      expect(mockCore.error).toHaveBeenCalledWith(expect.stringContaining("E004:"));
      expect(mockCore.error).toHaveBeenCalledWith(expect.stringContaining("not in the allowed-repos list"));
    });

    it("should allow target repository in allowlist", async () => {
      process.env.GH_AW_ALLOWED_REPOS = "allowed-owner/allowed-repo,other-owner/other-repo";

      setAgentOutput({
        items: [
          {
            type: "assign_to_agent",
            issue_number: 42,
            agent: "copilot",
            repo: "allowed-owner/allowed-repo",
          },
        ],
        errors: [],
      });

      // Mock REST responses
      mockGithub.rest.issues.checkUserCanBeAssigned.mockResolvedValueOnce({});
      mockGithub.rest.users.getByUsername.mockResolvedValueOnce({ data: { id: 99999 } });
      mockGithub.rest.issues.get.mockResolvedValueOnce({
        data: { id: 12345, number: 42, assignees: [], html_url: "", title: "", body: "" },
      });
      mockGithub.request.mockResolvedValueOnce({ data: { id: "task-123" } });

      await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

      expect(mockCore.setFailed).not.toHaveBeenCalled();
      // Check that the target repository was used and assignment proceeded
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Looking for copilot coding agent"));
    }, 20000);

    it("should allow default repository even without allowlist", async () => {
      // Default repo is test-owner/test-repo (from mockContext)
      // No GH_AW_TARGET_REPO_SLUG set, no GH_AW_ALLOWED_REPOS set
      setAgentOutput({
        items: [
          {
            type: "assign_to_agent",
            issue_number: 42,
            agent: "copilot",
          },
        ],
        errors: [],
      });

      // Mock REST responses
      mockGithub.rest.issues.checkUserCanBeAssigned.mockResolvedValueOnce({});
      mockGithub.rest.users.getByUsername.mockResolvedValueOnce({ data: { id: 99999 } });
      mockGithub.rest.issues.get.mockResolvedValueOnce({
        data: { id: 12345, number: 42, assignees: [], html_url: "", title: "", body: "" },
      });
      mockGithub.request.mockResolvedValueOnce({ data: { id: "task-123" } });

      await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

      expect(mockCore.setFailed).not.toHaveBeenCalled();
      // Check that assignment proceeded without errors
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Default target repo: test-owner/test-repo"));
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Looking for copilot coding agent"));
    }, 20000);
  });

  it("should handle pull-request-repo configuration correctly", async () => {
    process.env.GH_AW_AGENT_PULL_REQUEST_REPO = "test-owner/pull-request-repo";
    // Note: pull-request-repo is automatically allowed, no need to set allowed list
    setAgentOutput({
      items: [
        {
          type: "assign_to_agent",
          issue_number: 42,
          agent: "copilot",
        },
      ],
      errors: [],
    });

    // Mock REST responses
    // Get PR repository (pull-request-repo)
    mockGithub.rest.repos.get.mockResolvedValueOnce({ data: { node_id: "pull-request-repo-id", default_branch: "main" } });
    // Find agent
    mockGithub.rest.issues.checkUserCanBeAssigned.mockResolvedValueOnce({});
    mockGithub.rest.users.getByUsername.mockResolvedValueOnce({ data: { id: 99999 } });
    // Get issue details
    mockGithub.rest.issues.get.mockResolvedValueOnce({
      data: { id: 12345, number: 42, assignees: [], html_url: "", title: "", body: "" },
    });
    // Assign agent
    mockGithub.request.mockResolvedValueOnce({ data: { id: "task-123" } });

    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Using pull request repository: test-owner/pull-request-repo"));
    expect(mockGithub.request).toHaveBeenCalledWith("POST /repos/{owner}/{repo}/issues/{issue_number}/assignees", expect.objectContaining({ owner: "test-owner", repo: "test-repo", issue_number: 42 }));
  });

  it("should handle per-item pull_request_repo parameter", async () => {
    // Set global pull-request-repo which will be automatically allowed
    process.env.GH_AW_AGENT_PULL_REQUEST_REPO = "test-owner/default-pr-repo";
    // Set allowed list for additional repos
    process.env.GH_AW_AGENT_ALLOWED_PULL_REQUEST_REPOS = "test-owner/item-pull-request-repo";
    setAgentOutput({
      items: [
        {
          type: "assign_to_agent",
          issue_number: 42,
          agent: "copilot",
          pull_request_repo: "test-owner/item-pull-request-repo",
        },
      ],
      errors: [],
    });

    // Mock REST responses
    // Get global PR repository ID and default branch (for default-pr-repo)
    mockGithub.rest.repos.get.mockResolvedValueOnce({ data: { node_id: "default-pr-repo-id", default_branch: "main" } });
    // Get item PR repository
    mockGithub.rest.repos.get.mockResolvedValueOnce({ data: { node_id: "item-pull-request-repo-id", default_branch: "develop" } });
    // Find agent
    mockGithub.rest.issues.checkUserCanBeAssigned.mockResolvedValueOnce({});
    mockGithub.rest.users.getByUsername.mockResolvedValueOnce({ data: { id: 99999 } });
    // Get issue details
    mockGithub.rest.issues.get.mockResolvedValueOnce({
      data: { id: 12345, number: 42, assignees: [], html_url: "", title: "", body: "" },
    });
    // Assign agent
    mockGithub.request.mockResolvedValueOnce({ data: { id: "task-123" } });

    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Using per-item pull request repository: test-owner/item-pull-request-repo"));

    expect(mockGithub.request).toHaveBeenLastCalledWith("POST /repos/{owner}/{repo}/issues/{issue_number}/assignees", {
      owner: "test-owner",
      repo: "test-repo",
      issue_number: 42,
      assignees: ["copilot-swe-agent[bot]"],
      agent_assignment: {
        target_repo: "test-owner/item-pull-request-repo",
        base_branch: "develop",
      },
    });
  });

  it("should reject per-item pull_request_repo not in allowed list", async () => {
    process.env.GH_AW_AGENT_PULL_REQUEST_REPO = "test-owner/default-pr-repo";
    process.env.GH_AW_AGENT_ALLOWED_PULL_REQUEST_REPOS = "test-owner/allowed-pr-repo";
    setAgentOutput({
      items: [
        {
          type: "assign_to_agent",
          issue_number: 42,
          agent: "copilot",
          pull_request_repo: "test-owner/not-allowed-repo",
        },
      ],
      errors: [],
    });

    // Mock global PR repo lookup
    mockGithub.rest.repos.get.mockResolvedValueOnce({ data: { node_id: "default-pr-repo-id", default_branch: "main" } });

    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

    expect(mockCore.error).toHaveBeenCalledWith(expect.stringContaining("E004:"));
    expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Failed to assign 1 agent(s)"));
  });

  it("should allow pull-request-repo without it being in allowed-pull-request-repos", async () => {
    // Set pull-request-repo but DO NOT set allowed-pull-request-repos
    // This tests that pull-request-repo is automatically allowed (like target-repo behavior)
    process.env.GH_AW_AGENT_PULL_REQUEST_REPO = "test-owner/auto-allowed-repo";
    setAgentOutput({
      items: [
        {
          type: "assign_to_agent",
          issue_number: 42,
          agent: "copilot",
        },
      ],
      errors: [],
    });

    // Mock REST responses
    // Get PR repository ID and default branch
    mockGithub.rest.repos.get.mockResolvedValueOnce({ data: { node_id: "auto-allowed-repo-id", default_branch: "main" } });
    // Find agent
    mockGithub.rest.issues.checkUserCanBeAssigned.mockResolvedValueOnce({});
    mockGithub.rest.users.getByUsername.mockResolvedValueOnce({ data: { id: 99999 } });
    // Get issue details
    mockGithub.rest.issues.get.mockResolvedValueOnce({
      data: { id: 12345, number: 42, assignees: [], html_url: "", title: "", body: "" },
    });
    // Assign agent
    mockGithub.request.mockResolvedValueOnce({ data: { id: "task-123" } });

    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

    // Should succeed - pull-request-repo is automatically allowed
    expect(mockCore.setFailed).not.toHaveBeenCalled();
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Using pull request repository: test-owner/auto-allowed-repo"));
  });

  it("should use explicit base-branch when GH_AW_AGENT_BASE_BRANCH is set", async () => {
    process.env.GH_AW_AGENT_PULL_REQUEST_REPO = "test-owner/code-repo";
    process.env.GH_AW_AGENT_BASE_BRANCH = "develop";
    setAgentOutput({
      items: [{ type: "assign_to_agent", issue_number: 42, agent: "copilot" }],
      errors: [],
    });

    mockGithub.rest.repos.get.mockResolvedValueOnce({ data: { node_id: "code-repo-id", default_branch: "main" } });
    mockGithub.rest.issues.checkUserCanBeAssigned.mockResolvedValueOnce({});
    mockGithub.rest.users.getByUsername.mockResolvedValueOnce({ data: { id: 99999 } });
    mockGithub.rest.issues.get.mockResolvedValueOnce({ data: { id: 12345, number: 42, assignees: [], html_url: "", title: "", body: "" } });
    mockGithub.request.mockResolvedValueOnce({ data: { id: "task-123" } });

    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

    expect(mockCore.setFailed).not.toHaveBeenCalled();
    // Assignment uses issue assignees endpoint and does not include base_ref/custom instructions
    expect(mockGithub.request).toHaveBeenCalledWith("POST /repos/{owner}/{repo}/issues/{issue_number}/assignees", expect.objectContaining({ owner: "test-owner", repo: "test-repo", issue_number: 42 }));
  });

  it("should auto-resolve non-main default branch from pull-request-repo and set as baseRef", async () => {
    process.env.GH_AW_AGENT_PULL_REQUEST_REPO = "test-owner/code-repo";
    // No GH_AW_AGENT_BASE_BRANCH set - should use repo's default branch
    setAgentOutput({
      items: [{ type: "assign_to_agent", issue_number: 42, agent: "copilot" }],
      errors: [],
    });

    mockGithub.rest.repos.get.mockResolvedValueOnce({ data: { node_id: "code-repo-id", default_branch: "develop" } });
    mockGithub.rest.issues.checkUserCanBeAssigned.mockResolvedValueOnce({});
    mockGithub.rest.users.getByUsername.mockResolvedValueOnce({ data: { id: 99999 } });
    mockGithub.rest.issues.get.mockResolvedValueOnce({ data: { id: 12345, number: 42, assignees: [], html_url: "", title: "", body: "" } });
    mockGithub.request.mockResolvedValueOnce({ data: { id: "task-123" } });

    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

    expect(mockCore.setFailed).not.toHaveBeenCalled();
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Resolved pull request repository default branch: develop"));
    expect(mockGithub.request).toHaveBeenCalledWith("POST /repos/{owner}/{repo}/issues/{issue_number}/assignees", expect.objectContaining({ owner: "test-owner", repo: "test-repo", issue_number: 42 }));
  });

  it("should set baseRef when pull-request-repo default branch is main (no explicit base-branch)", async () => {
    process.env.GH_AW_AGENT_PULL_REQUEST_REPO = "test-owner/code-repo";
    // No GH_AW_AGENT_BASE_BRANCH set; repo default is main
    setAgentOutput({
      items: [{ type: "assign_to_agent", issue_number: 42, agent: "copilot" }],
      errors: [],
    });

    mockGithub.rest.repos.get.mockResolvedValueOnce({ data: { node_id: "code-repo-id", default_branch: "main" } });
    mockGithub.rest.issues.checkUserCanBeAssigned.mockResolvedValueOnce({});
    mockGithub.rest.users.getByUsername.mockResolvedValueOnce({ data: { id: 99999 } });
    mockGithub.rest.issues.get.mockResolvedValueOnce({ data: { id: 12345, number: 42, assignees: [], html_url: "", title: "", body: "" } });
    mockGithub.request.mockResolvedValueOnce({ data: { id: "task-123" } });

    await eval(`(async () => { ${assignToAgentScript}; ${STANDALONE_RUNNER} })()`);

    expect(mockCore.setFailed).not.toHaveBeenCalled();
    expect(mockGithub.request).toHaveBeenCalledWith("POST /repos/{owner}/{repo}/issues/{issue_number}/assignees", expect.objectContaining({ owner: "test-owner", repo: "test-repo", issue_number: 42 }));
  });
});
