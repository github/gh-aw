import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { getOctokit } from "@actions/github";
import { executionEnv } from "./test_execution_provenance.js";

const broker = require("./token_broker.cjs");
const recovery = require("./error_recovery.cjs");
const moduleNames = ["token_broker_github", "create_agent_session", "assign_to_agent", "assign_copilot_to_created_issues", "create_issue", "create_pull_request"];

describe("broker-authenticated agent operations", () => {
  let tokenSpy;
  let fetchMock;
  let coreMock;
  let legacy;
  let clock;
  let scripts;

  beforeEach(() => {
    vi.stubEnv("GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG", JSON.stringify({ _token_broker: { "copilot-task": true } }));
    vi.stubEnv("GH_AW_TOKEN_BROKER_URL", "https://broker.example/token");
    vi.stubEnv("GH_AW_AGENT_SESSION_TOKEN", "legacy-session");
    vi.stubEnv("GH_AW_ASSIGN_TO_AGENT_TOKEN", "legacy-assignment");
    vi.stubEnv("GH_AW_AGENT_TOKEN", "legacy-agent");
    vi.stubEnv("GITHUB_TOKEN", "legacy-step");
    vi.stubEnv("GH_AW_ISSUES_TO_ASSIGN_COPILOT", "owner/repo:1,owner/repo:2");
    vi.stubEnv("GH_AW_SAFE_OUTPUTS_STAGED", "");
    vi.stubEnv("GH_AW_TARGET_REPO_SLUG", "");
    vi.stubEnv("GH_AW_ALLOWED_REPOS", "");
    clock = 0;
    tokenSpy = vi.spyOn(broker, "getBrokerToken").mockImplementation(async () => `broker-secret-${clock}`);
    vi.spyOn(recovery, "sleep").mockImplementation(async () => {
      clock += 3600000;
    });
    coreMock = {
      info: vi.fn(),
      debug: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
      setOutput: vi.fn(),
      setFailed: vi.fn(),
      summary: { addRaw: vi.fn().mockReturnThis(), write: vi.fn().mockResolvedValue() },
    };
    legacy = { request: vi.fn(), rest: { issues: { createComment: vi.fn().mockResolvedValue({}) } } };
    vi.stubGlobal("core", coreMock);
    vi.stubGlobal("github", legacy);
    vi.stubGlobal("context", { repo: { owner: "owner", repo: "repo" }, eventName: "issues", payload: { issue: { number: 1 } } });
    fetchMock = vi.fn(async (url, options) => {
      const pathname = new URL(url).pathname;
      if (pathname.endsWith("/rate_limit")) return response({});
      if (options.method === "GET" && pathname.includes("/assignees/")) return new Response(null, { status: 204 });
      if (options.method === "POST" && pathname.endsWith("/tasks")) return response({ id: "task-id", html_url: "https://github.com/owner/repo/tasks/task-id" });
      if (options.method === "POST" && pathname.endsWith("/assignees")) return response({});
      if (options.method === "GET" && /\/issues\/\d+$/.test(pathname)) return response({ id: 100, number: Number(pathname.split("/").at(-1)), assignees: [] });
      throw new Error(`Unexpected request: ${options.method} ${pathname}`);
    });
    vi.stubGlobal(
      "getOctokit",
      vi.fn((token, options) => getOctokit(token, { ...options, request: { fetch: fetchMock } }))
    );
    // CommonJS requires are not intercepted by vi.mock; spy before loading clients.
    for (const name of moduleNames) delete require.cache[require.resolve(`./${name}.cjs`)];
    scripts = Object.fromEntries(moduleNames.map(name => [name, require(`./${name}.cjs`)]));
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllEnvs();
    vi.unstubAllGlobals();
    for (const name of moduleNames) delete require.cache[require.resolve(`./${name}.cjs`)];
  });

  function response(data) {
    return new Response(JSON.stringify(data), { status: 200, headers: { "content-type": "application/json" } });
  }

  function expectBrokerOnly() {
    expect(legacy.request).not.toHaveBeenCalled();
    expect(tokenSpy).toHaveBeenCalledTimes(fetchMock.mock.calls.length);
    expect(tokenSpy.mock.calls.every(args => args[0] === "copilot-task")).toBe(true);
    for (const [, options] of fetchMock.mock.calls) expect(options.headers.authorization).toMatch(/^token broker-secret-/);
    for (const [token, options] of global.getOctokit.mock.calls) {
      expect(token).toBe("");
      expect(options.authStrategy).toBeTypeOf("function");
    }
  }

  function expectNoCredentials(value) {
    const diagnostics = JSON.stringify([
      value,
      ...Object.values(coreMock)
        .filter(fn => vi.isMockFunction(fn))
        .map(fn => fn.mock.calls),
      coreMock.summary.addRaw.mock.calls,
    ]);
    expect(diagnostics).not.toMatch(/broker-secret|legacy-session|legacy-assignment|legacy-agent|legacy-step|legacy-config|Authorization|sensitive-header/);
  }

  function configureIssueCreation() {
    for (const [key, value] of Object.entries(executionEnv)) vi.stubEnv(key, value);
    process.env.GH_AW_ASSIGN_COPILOT = "true";
    process.env.GITHUB_REPOSITORY = "owner/repo";
    process.env.GITHUB_BASE_REF = "main";
    process.env.GH_AW_WORKFLOW_ID = "broker-test";
    process.env.GH_AW_WORKFLOW_NAME = "Broker Test";
    let issueNumber = 0;
    legacy.rest.issues.create = vi.fn(async () => ({ data: { number: ++issueNumber, html_url: `https://github.com/owner/repo/issues/${issueNumber}`, title: "Test issue" } }));
    legacy.rest.issues.addLabels = vi.fn().mockResolvedValue({});
    legacy.rest.issues.get = vi.fn().mockResolvedValue({ data: { number: 1, assignees: [] } });
    legacy.rest.pulls = {
      create: vi.fn().mockRejectedValue(Object.assign(new Error("Permission denied"), { status: 403 })),
      requestReviewers: vi.fn().mockResolvedValue({}),
    };
    legacy.rest.repos = { get: vi.fn().mockResolvedValue({ data: { default_branch: "main" } }) };
    legacy.rest.search = { issuesAndPullRequests: vi.fn().mockResolvedValue({ data: { total_count: 0, items: [] } }) };
    legacy.rest.rateLimit = { get: vi.fn().mockResolvedValue({ data: { resources: { search: { remaining: 30, limit: 30 } } } }) };
    coreMock.startGroup = vi.fn();
    coreMock.endGroup = vi.fn();
    global.context.runId = "12345";
    vi.stubGlobal("exec", {
      exec: vi.fn().mockResolvedValue(0),
      getExecOutput: vi.fn(async (program, args) => ({ exitCode: 0, stdout: program === "git" && args[0] === "rev-list" ? "" : "main", stderr: "" })),
    });
    const brokerFactory = global.getOctokit.getMockImplementation();
    global.getOctokit.mockImplementation((token, options) => (token ? legacy : brokerFactory(token, options)));
  }

  it("authenticates actual Octokit REST and GraphQL calls with renewed tokens without changing the environment", async () => {
    const envBefore = { ...process.env };
    const normalFetch = fetchMock.getMockImplementation();
    fetchMock.mockImplementation(async (url, options) => {
      if (new URL(url).pathname === "/graphql") return response({ data: { viewer: { login: "broker-user" } } });
      return normalFetch(url, options);
    });
    const client = scripts.token_broker_github.createTokenBrokerGitHubClient();
    await client.rest.issues.get({ owner: "owner", repo: "repo", issue_number: 1 });
    clock += 3600000;
    expect(await client.graphql("query { viewer { login } }")).toEqual({ viewer: { login: "broker-user" } });
    expectBrokerOnly();
    expect(fetchMock.mock.calls.map(([, options]) => options.headers.authorization)).toEqual(["token broker-secret-0", "token broker-secret-3600000"]);
    expect(process.env).toEqual(envBefore);
    expectNoCredentials();
  });

  it.each([
    ["CONFIGURATION", "Token broker configuration is invalid."],
    ["OIDC", "Token broker identity acquisition failed."],
    ["UNAVAILABLE", "Token broker is unavailable."],
    ["UNAUTHORIZED", "Token broker authorization failed."],
    ["UNSUPPORTED", "Token broker capability is unsupported."],
    ["MALFORMED_RESPONSE", "Token broker response is invalid."],
  ])("preserves safe broker error kind %s without retaining causes or request data", async (kind, message) => {
    const code = `ERR_TOKEN_BROKER_${kind}`;
    tokenSpy.mockRejectedValue(Object.assign(new Error(message), { code, cause: new Error("sensitive-header"), request: { headers: { authorization: "broker-secret" } } }));
    const client = scripts.token_broker_github.createTokenBrokerGitHubClient();
    const error = await client.request("GET /rate_limit").catch(value => value);
    expect(error.message).toBe(message);
    expect(error.code).toBe(code);
    expect(error.cause).toBeUndefined();
    expect(error.request).toBeUndefined();
    expect(fetchMock).not.toHaveBeenCalled();
    expectNoCredentials(error);
  });

  it("uses request-time broker auth before per-handler and compiled tokens for session creation", async () => {
    const handler = await scripts.create_agent_session.main({ base: "main", "github-token": "legacy-config" });
    expect(tokenSpy).not.toHaveBeenCalled();
    expect((await handler({ body: "first task" })).success).toBe(true);
    clock += 3600000;
    expect((await handler({ body: "second task" })).success).toBe(true);
    expectBrokerOnly();
    expect(fetchMock.mock.calls.map(([, options]) => options.headers.authorization)).toEqual(["token broker-secret-0", "token broker-secret-3600000"]);
  });

  it("refreshes broker authentication on every assignment API call across a long batch", async () => {
    const handler = await scripts.assign_to_agent.main({ target: "*", max: 2, "github-token": "legacy-config" });
    expect(tokenSpy).not.toHaveBeenCalled();
    expect(await handler({ issue_number: 1 })).toEqual({ success: true });
    expect(await handler({ issue_number: 2 })).toEqual({ success: true });
    expectBrokerOnly();
    expect(fetchMock.mock.calls.filter(([, options]) => options.method === "POST").map(([, options]) => options.headers.authorization)).toEqual(["token broker-secret-0", "token broker-secret-3600000"]);
    expect(handler.getAssignToAgentErrorCount()).toBe(0);
  });

  it("uses broker auth and correct issue context for create-issue copilot assignments across a long batch", async () => {
    await scripts.assign_copilot_to_created_issues.main();
    expectBrokerOnly();
    const assignments = fetchMock.mock.calls.filter(([, options]) => options.method === "POST");
    expect(assignments.map(([url]) => new URL(url).pathname)).toEqual(["/repos/owner/repo/issues/1/assignees", "/repos/owner/repo/issues/2/assignees"]);
    expect(assignments.map(([, options]) => options.headers.authorization)).toEqual(["token broker-secret-0", "token broker-secret-3600000"]);
    expect(coreMock.setFailed).not.toHaveBeenCalled();
  });

  it.each(["create_issue", "create_pull_request"])("uses refreshed broker tokens only for Copilot assignment in %s, leaving issue creation on its legacy token", async operation => {
    configureIssueCreation();
    const handler = await scripts[operation].main({ assignees: ["copilot"], "github-token": "legacy-config", max: 2, allow_empty: true });
    expect(tokenSpy).not.toHaveBeenCalled();
    await handler({ title: "First task", body: "Task description" }, {});
    clock += 3600000;
    await handler({ title: "Second task", body: "Task description" }, {});
    expect(legacy.rest.issues.create).toHaveBeenCalledTimes(2);
    expect(legacy.request.mock.calls.every(([route]) => route.endsWith("/comments"))).toBe(true);
    expect(coreMock.setFailed).not.toHaveBeenCalled();
    expect(tokenSpy).toHaveBeenCalledTimes(fetchMock.mock.calls.length);
    expect(tokenSpy).toHaveBeenCalledTimes(6);
    expect(fetchMock.mock.calls.filter(([, options]) => options.method === "POST").map(([, options]) => options.headers.authorization)).toEqual(["token broker-secret-0", "token broker-secret-3600000"]);
    expect(global.getOctokit).toHaveBeenCalledWith("legacy-config", expect.any(Object));
    expectNoCredentials();
  });

  it.each(["create_issue", "create_pull_request"])("fails closed on broker errors in %s without retrying assignment with its handler token", async operation => {
    configureIssueCreation();
    tokenSpy.mockRejectedValue(Object.assign(new Error("Token broker capability is unsupported."), { code: "ERR_TOKEN_BROKER_UNSUPPORTED", cause: new Error("sensitive-header") }));
    const handler = await scripts[operation].main({ assignees: ["copilot"], "github-token": "legacy-config", allow_empty: true, "ignore-if-error": true });
    const result = await handler({ title: "Task", body: "Task description" }, {});
    expect(legacy.rest.issues.create).toHaveBeenCalledTimes(1);
    expect(result.success).toBe(false);
    expect(result.error).toContain("Token broker capability is unsupported.");
    expect(fetchMock).not.toHaveBeenCalled();
    expect(legacy.request.mock.calls.every(([route]) => route.endsWith("/comments"))).toBe(true);
    expectNoCredentials();
  });

  it.each(["create_issue", "create_pull_request"])("fails closed when the shared assignment helper swallows a broker-authenticated API error in %s", async operation => {
    configureIssueCreation();
    const normalFetch = fetchMock.getMockImplementation();
    fetchMock.mockImplementation(async (url, options) => {
      if (options.method === "POST") return new Response(JSON.stringify({ message: "Authorization broker-secret-0 sensitive-header" }), { status: 403, headers: { "content-type": "application/json" } });
      return normalFetch(url, options);
    });
    const handler = await scripts[operation].main({ assignees: ["copilot"], "github-token": "legacy-config", allow_empty: true, "ignore-if-error": true });
    const result = await handler({ title: "Task", body: "Task description" }, {});
    expect(legacy.rest.issues.create).toHaveBeenCalledTimes(1);
    expect(result.success).toBe(false);
    expect(result.error).toContain("Broker-authenticated copilot assignment failed");
    expect(legacy.request.mock.calls.every(([route]) => route.endsWith("/comments"))).toBe(true);
    expectNoCredentials();
  });

  it.each([
    ["create_issue", "agent unavailable"],
    ["create_issue", "issue details unavailable"],
    ["create_pull_request", "agent unavailable"],
    ["create_pull_request", "issue details unavailable"],
  ])("returns an operation failure from %s when assignment helpers return null: %s", async (operation, failure) => {
    configureIssueCreation();
    const normalFetch = fetchMock.getMockImplementation();
    fetchMock.mockImplementation(async (url, options) => {
      const pathname = new URL(url).pathname;
      if (failure === "agent unavailable") {
        if (pathname.includes("/assignees/")) return new Response(JSON.stringify({ message: "Not Found" }), { status: 404, headers: { "content-type": "application/json" } });
        if (pathname.endsWith("/assignees")) return response([]);
      } else if (/\/issues\/\d+$/.test(pathname)) {
        return response({});
      }
      return normalFetch(url, options);
    });
    const handler = await scripts[operation].main({ assignees: ["copilot"], "github-token": "legacy-config", allow_empty: true, "ignore-if-error": true });
    const result = await handler({ title: "Task", body: "Task description" }, {});
    expect(result.success).toBe(false);
    expect(result.error).toContain(failure);
    expect(legacy.rest.issues.create).toHaveBeenCalledTimes(1);
    expect(fetchMock.mock.calls.some(([, options]) => options.method === "POST")).toBe(false);
    expectNoCredentials(result);
  });

  it.each(["create_issue", "create_pull_request"])("returns an operation failure when broker renewal fails during the mutation in %s", async operation => {
    configureIssueCreation();
    tokenSpy
      .mockReset()
      .mockResolvedValueOnce("broker-secret-0")
      .mockResolvedValueOnce("broker-secret-0")
      .mockRejectedValue(Object.assign(new Error("Token broker authorization failed."), { code: "ERR_TOKEN_BROKER_UNAUTHORIZED" }));
    const handler = await scripts[operation].main({ assignees: ["copilot"], "github-token": "legacy-config", allow_empty: true, "ignore-if-error": true });
    const result = await handler({ title: "Task", body: "Task description" }, {});
    expect(result.success).toBe(false);
    expect(legacy.rest.issues.create).toHaveBeenCalledTimes(1);
    expect(fetchMock.mock.calls.some(([, options]) => options.method === "POST")).toBe(false);
    expectNoCredentials(result);
  });

  it.each(
    ["create_agent_session", "assign_to_agent", "assign_copilot_to_created_issues", "create_issue", "create_pull_request"].flatMap(operation => [
      [operation, "empty"],
      [operation, "missing"],
    ])
  )("fails closed with an opted-in %s and %s broker URL without using legacy assignment tokens", async (operation, urlState) => {
    if (urlState === "missing") delete process.env.GH_AW_TOKEN_BROKER_URL;
    else process.env.GH_AW_TOKEN_BROKER_URL = "";
    tokenSpy.mockRejectedValue(new Error("Authorization broker-secret sensitive-header"));
    let result;
    if (operation === "assign_copilot_to_created_issues") {
      await scripts[operation].main();
      expect(coreMock.setFailed).toHaveBeenCalled();
    } else if (operation === "create_issue" || operation === "create_pull_request") {
      configureIssueCreation();
      const handler = await scripts[operation].main({ assignees: ["copilot"], "github-token": "legacy-config", allow_empty: true, "ignore-if-error": true });
      result = await handler({ title: "Task", body: "Task description" }, {});
      expect(result.success).toBe(false);
    } else {
      const handler = await scripts[operation].main({ base: "main", target: "*", "github-token": "legacy-config", "ignore-if-error": true });
      result = await handler({ body: "task", issue_number: 1 });
      expect(result.success).toBe(false);
      expect(result.skipped).not.toBe(true);
      if (operation === "assign_to_agent") expect(handler.getAssignToAgentErrorCount()).toBe(1);
      await (handler.writeSummary?.() || handler.writeAssignToAgentSummary?.());
    }
    expect(fetchMock).not.toHaveBeenCalled();
    expect(legacy.request).not.toHaveBeenCalled();
    expectNoCredentials(result);
  });

  it.each(["create_agent_session", "assign_to_agent", "assign_copilot_to_created_issues"])("sanitizes GitHub API errors before helper logs and outputs for %s", async operation => {
    fetchMock.mockImplementation(async () => {
      throw new Error("Authorization broker-secret-0 sensitive-header");
    });
    let result;
    if (operation === "assign_copilot_to_created_issues") {
      await scripts[operation].main();
      expect(coreMock.setFailed).toHaveBeenCalled();
    } else {
      const handler = await scripts[operation].main({ base: "main", target: "*", "github-token": "legacy-config", "ignore-if-error": true });
      result = await handler({ body: "task", issue_number: 1 });
      expect(result.success).toBe(false);
      expect(result.skipped).not.toBe(true);
      await (handler.writeSummary?.() || handler.writeAssignToAgentSummary?.());
    }
    expectNoCredentials(result);
    expect(legacy.request).not.toHaveBeenCalled();
  });

  it.each(["assign_to_agent", "assign_copilot_to_created_issues"])("does not ignore broker refresh failure during the mutation in %s", async operation => {
    const successfulCalls = operation === "assign_to_agent" ? 3 : 2;
    tokenSpy.mockReset();
    for (let i = 0; i < successfulCalls; i++) tokenSpy.mockResolvedValueOnce("broker-secret-0");
    tokenSpy.mockRejectedValue(new Error("Authorization broker-secret-0 sensitive-header"));
    if (operation === "assign_to_agent") {
      const handler = await scripts[operation].main({ target: "*", "ignore-if-error": true });
      expect((await handler({ issue_number: 1 })).success).toBe(false);
      expect(handler.getAssignToAgentErrorCount()).toBe(1);
    } else {
      await scripts[operation].main();
      expect(coreMock.setFailed).toHaveBeenCalled();
    }
    expect(fetchMock.mock.calls.some(([, options]) => options.method === "POST")).toBe(false);
    expectNoCredentials();
    expect(legacy.request).not.toHaveBeenCalled();
  });

  it.each(["assign_to_agent", "assign_copilot_to_created_issues"])("sanitizes mutation response diagnostics before assignment helper logging in %s", async operation => {
    const normalFetch = fetchMock.getMockImplementation();
    fetchMock.mockImplementation(async (url, options) => {
      if (options.method === "POST") {
        return new Response(JSON.stringify({ message: "Authorization broker-secret-0 sensitive-header" }), { status: 403, headers: { "content-type": "application/json" } });
      }
      return normalFetch(url, options);
    });
    // Failure comments are unrelated operations that retain the legacy client,
    // but their diagnostics must also stay safe while broker mode is enabled.
    legacy.rest.issues.createComment.mockRejectedValue(new Error("Authorization legacy-step sensitive-header"));
    if (operation === "assign_to_agent") {
      const handler = await scripts[operation].main({ target: "*", "ignore-if-error": true });
      const result = await handler({ issue_number: 1 });
      expect(result.success).toBe(false);
      expect(handler.getAssignToAgentErrorCount()).toBe(1);
      await handler.writeAssignToAgentSummary();
      expectNoCredentials(result);
    } else {
      await scripts[operation].main();
      expect(coreMock.setFailed).toHaveBeenCalled();
      expectNoCredentials();
    }
    expect(legacy.request).not.toHaveBeenCalled();
  });

  it("preserves the disabled created-issue helper calls and uses no broker client", async () => {
    delete process.env.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG;
    process.env.GH_AW_ISSUES_TO_ASSIGN_COPILOT = "owner/repo:1";
    const helpers = require("./assign_agent_helpers.cjs");
    const findAgent = vi.spyOn(helpers, "findAgent").mockResolvedValue("agent-id");
    const getIssueDetails = vi.spyOn(helpers, "getIssueDetails").mockResolvedValue({ issueId: "issue-id", currentAssignees: [] });
    const assignAgentToIssue = vi.spyOn(helpers, "assignAgentToIssue").mockResolvedValue(true);
    delete require.cache[require.resolve("./assign_copilot_to_created_issues.cjs")];
    await require("./assign_copilot_to_created_issues.cjs").main();
    expect(findAgent).toHaveBeenCalledWith("owner", "repo", "copilot", 1);
    expect(getIssueDetails).toHaveBeenCalledWith("owner", "repo", 1);
    expect(assignAgentToIssue).toHaveBeenCalledWith("issue-id", "agent-id", [], "copilot", null);
    expect(global.getOctokit).not.toHaveBeenCalled();
    expect(tokenSpy).not.toHaveBeenCalled();
    expect(coreMock.setFailed).not.toHaveBeenCalled();
  });

  it("keeps per-handler token precedence and the step-level created-issue path when broker is absent", async () => {
    delete process.env.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG;
    delete process.env.GH_AW_TOKEN_BROKER_URL;
    global.getOctokit.mockReturnValue(legacy);
    legacy.request.mockResolvedValue({ data: { id: "legacy-id" } });
    const handler = await scripts.create_agent_session.main({ base: "main", "github-token": "legacy-config" });
    expect((await handler({ body: "task" })).success).toBe(true);
    await scripts.assign_to_agent.main({ "github-token": "legacy-assignment-config" });
    delete process.env.GH_AW_ISSUES_TO_ASSIGN_COPILOT;
    await scripts.assign_copilot_to_created_issues.main();
    expect(global.getOctokit.mock.calls).toEqual([["legacy-config"], ["legacy-assignment-config"]]);
    expect(tokenSpy).not.toHaveBeenCalled();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it.each(
    ["create_agent_session", "assign_to_agent", "assign_copilot_to_created_issues", "create_issue", "create_pull_request"].flatMap(operation =>
      [
        ["missing", undefined],
        ["absent marker", "{}"],
        ["false marker", '{"_token_broker":false}'],
        ["non-boolean marker", '{"_token_broker":{"copilot-task":"true"}}'],
        ["invalid JSON", "{"],
      ].map(([label, config]) => [operation, label, config])
    )
  )("keeps %s on legacy auth with inherited broker URL and %s configuration", async (operation, _label, config) => {
    if (config === undefined) delete process.env.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG;
    else process.env.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG = config;
    configureIssueCreation();
    process.env.GH_AW_ISSUES_TO_ASSIGN_COPILOT = "owner/repo:1";
    global.getOctokit.mockReturnValue(legacy);
    legacy.request.mockResolvedValue({ data: { id: "legacy-id" } });
    if (operation === "assign_copilot_to_created_issues") {
      await scripts[operation].main();
    } else {
      const handler = await scripts[operation].main({ base: "main", target: "*", assignees: ["copilot"], "github-token": "legacy-config", allow_empty: true });
      const result = await handler({ body: "Task description", title: "Task", issue_number: 1 }, {});
      expect(result.success).toBe(true);
    }
    expect(tokenSpy).not.toHaveBeenCalled();
    expect(fetchMock).not.toHaveBeenCalled();
    expect(coreMock.setFailed).not.toHaveBeenCalled();
    expectNoCredentials();
  });
});
