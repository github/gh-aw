import { describe, it, expect, beforeEach, vi } from "vitest";

const mockCore = { info: vi.fn(), debug: vi.fn(), warning: vi.fn() };
global.core = mockCore;

const { parseSearchQuery, getCapableBackends, orderBackendsByPressure, countSearchMatches } = await import("./skip_if_query.cjs");

const REPO = "repo:octo/demo";

describe("skip_if_query.cjs - parseSearchQuery", () => {
  it("parses simple repo-scoped list queries", () => {
    const result = parseSearchQuery(`is:issue is:open label:bug label:"needs triage" author:octocat sort:created-desc ${REPO}`);
    expect(result).toEqual({
      ok: true,
      query: { type: "issue", state: "open", labels: ["bug", "needs triage"], repo: { owner: "octo", repo: "demo" }, author: "octocat", assignee: null, mentions: null },
    });
  });

  it("treats is:merged and is:pull-request as pull request filters", () => {
    const result = parseSearchQuery(`is:pull-request is:merged ${REPO}`);
    expect(result.ok && result.query).toMatchObject({ type: "pr", state: "merged" });
  });

  it.each([
    [`is:issue in:title "[daily-report]" ${REPO}`, "unsupported qualifier"],
    [`is:issue "[daily-report]" ${REPO}`, "free-text"],
    [`is:issue -label:bug ${REPO}`, "negated"],
    [`is:issue label:bug,urgent ${REPO}`, "OR label list"],
    [`is:issue is:pr ${REPO}`, "conflicting type"],
    [`is:issue is:merged ${REPO}`, "conflicting type"],
    [`is:issue ${REPO} repo:octo/other`, "multiple repo"],
    [`is:issue assignee:@me ${REPO}`, "unsupported assignee"],
    ["org:octo is:issue label:bug", "unsupported qualifier"],
    ["is:issue label:bug", "not scoped"],
    [`is:issue label:"unterminated ${REPO}`, "unbalanced quotes"],
  ])("rejects %s", (query, reason) => {
    const result = parseSearchQuery(query);
    expect(result.ok).toBe(false);
    expect(!result.ok && result.reason).toContain(reason);
  });
});

describe("skip_if_query.cjs - backend selection", () => {
  it("excludes GraphQL when label or user filters cannot be expressed exactly", () => {
    expect(getCapableBackends(parseSearchQuery(`is:issue label:bug ${REPO}`))).toEqual(["graphql", "rest", "search"]);
    expect(getCapableBackends(parseSearchQuery(`is:issue label:a label:b ${REPO}`))).toEqual(["rest", "search"]);
    expect(getCapableBackends(parseSearchQuery(`is:pr author:octocat ${REPO}`))).toEqual(["rest", "search"]);
    expect(getCapableBackends(parseSearchQuery(`is:issue in:title x ${REPO}`))).toEqual(["search"]);
  });

  it("demotes backends whose quota is under pressure", () => {
    const resources = {
      graphql: { limit: 5000, remaining: 100 },
      core: { limit: 5000, remaining: 4000 },
      search: { limit: 30, remaining: 30 },
    };
    expect(orderBackendsByPressure(["graphql", "rest", "search"], resources)).toEqual(["rest", "search", "graphql"]);
    expect(orderBackendsByPressure(["graphql", "rest", "search"], { ...resources, core: { limit: 5000, remaining: 0 } })).toEqual(["search", "graphql", "rest"]);
    expect(orderBackendsByPressure(["graphql", "rest", "search"], null)).toEqual(["graphql", "rest", "search"]);
  });
});

describe("skip_if_query.cjs - countSearchMatches", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    global.github = {
      graphql: vi.fn(),
      rest: {
        rateLimit: { get: vi.fn().mockResolvedValue({ data: { resources: {} } }) },
        issues: { listForRepo: vi.fn() },
        pulls: { list: vi.fn() },
        search: { issuesAndPullRequests: vi.fn() },
      },
    };
  });

  it("counts simple queries through GraphQL without using search", async () => {
    global.github.graphql.mockResolvedValue({ repository: { issues: { totalCount: 3 } } });

    const result = await countSearchMatches(`is:issue is:open label:bug ${REPO}`, 1);

    expect(result).toEqual({ totalCount: 3, exact: true, backend: "graphql" });
    const [query, variables] = global.github.graphql.mock.calls[0];
    expect(query).toContain("issues(");
    expect(query).not.toContain("pullRequests(");
    expect(query).not.toContain("$prStates");
    expect(variables).toMatchObject({ owner: "octo", repo: "demo", labels: ["bug"], issueStates: ["OPEN"] });
    // A null assignee would mean "unassigned" in GraphQL, so unset filters must be omitted.
    expect(variables.filterBy).toEqual({});
    expect(global.github.rest.search.issuesAndPullRequests).not.toHaveBeenCalled();
  });

  it("sums issues and pull requests for untyped queries and maps closed PRs to closed+merged", async () => {
    global.github.graphql.mockResolvedValue({ repository: { issues: { totalCount: 2 }, pullRequests: { totalCount: 5 } } });

    const result = await countSearchMatches(`is:closed ${REPO}`, 1);

    expect(result.totalCount).toBe(7);
    expect(global.github.graphql.mock.calls[0][1]).toMatchObject({ issueStates: ["CLOSED"], prStates: ["CLOSED", "MERGED"] });
  });

  it("falls back to REST when GraphQL fails and stops paging at the threshold", async () => {
    global.github.graphql.mockRejectedValue(new Error("boom"));
    const page = Array.from({ length: 100 }, (_, i) => (i % 2 ? { pull_request: {} } : {}));
    global.github.rest.issues.listForRepo.mockResolvedValue({ data: page });

    const result = await countSearchMatches(`is:issue is:open label:bug ${REPO}`, 60);

    expect(result).toEqual({ totalCount: 100, exact: false, backend: "rest" });
    expect(global.github.rest.issues.listForRepo).toHaveBeenCalledTimes(2);
    expect(global.github.rest.issues.listForRepo).toHaveBeenCalledWith({ owner: "octo", repo: "demo", state: "open", labels: "bug", per_page: 100, page: 1 });
    expect(global.github.rest.search.issuesAndPullRequests).not.toHaveBeenCalled();
  });

  it("uses REST with AND label semantics when GraphQL cannot express the query", async () => {
    global.github.rest.issues.listForRepo.mockResolvedValue({ data: [{}] });

    const result = await countSearchMatches(`label:a label:b assignee:octocat ${REPO}`, 2);

    expect(result).toEqual({ totalCount: 1, exact: true, backend: "rest" });
    expect(global.github.rest.issues.listForRepo).toHaveBeenCalledWith({ owner: "octo", repo: "demo", state: "all", labels: "a,b", assignee: "octocat", per_page: 2, page: 1 });
    expect(global.github.graphql).not.toHaveBeenCalled();
  });

  it("counts merged pull requests through the pulls endpoint", async () => {
    global.github.rest.rateLimit.get.mockResolvedValue({ data: { resources: { graphql: { limit: 5000, remaining: 0 } } } });
    global.github.rest.pulls.list.mockResolvedValue({ data: [{ merged_at: "2026-01-01" }, { merged_at: null }] });

    const result = await countSearchMatches(`is:pr is:merged ${REPO}`, 5);

    expect(result).toEqual({ totalCount: 1, exact: true, backend: "rest" });
    expect(global.github.rest.pulls.list).toHaveBeenCalledWith({ owner: "octo", repo: "demo", state: "closed", per_page: 100, page: 1 });
  });

  it("uses search directly for queries the parser cannot translate", async () => {
    global.github.rest.search.issuesAndPullRequests.mockResolvedValue({ data: { total_count: 4 } });
    const query = `is:issue in:title "[daily-report]" ${REPO}`;

    const result = await countSearchMatches(query, 1);

    expect(result).toEqual({ totalCount: 4, exact: true, backend: "search" });
    expect(global.github.rest.search.issuesAndPullRequests).toHaveBeenCalledWith({ q: query, per_page: 1 });
    expect(global.github.rest.rateLimit.get).not.toHaveBeenCalled();
  });

  it("rethrows the last backend error when every backend fails", async () => {
    global.github.graphql.mockRejectedValue(new Error("graphql down"));
    global.github.rest.issues.listForRepo.mockRejectedValue(new Error("rest down"));
    global.github.rest.search.issuesAndPullRequests.mockRejectedValue(new Error("search down"));

    await expect(countSearchMatches(`is:issue ${REPO}`, 1)).rejects.toThrow("search down");
  });
});
