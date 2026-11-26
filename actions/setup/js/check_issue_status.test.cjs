import { describe, it, expect, vi, afterEach } from "vitest";

const { matchesIssueStatus, main } = await import("./check_issue_status.cjs");
const { buildAwContext } = await import("./aw_context.cjs");

const started = { id: 4, name: "In progress", category: "started" };
const review = { id: 8, name: "In review", category: "started" };
const done = { id: 5, name: "Done", category: "completed" };
const payload = { action: "status_changed", status: review, issue: { number: 42, status: done }, changes: { status: { from: started } } };

afterEach(() => {
  delete global.context;
  delete global.core;
  delete process.env.GH_AW_ISSUE_STATUS_FILTER;
});

describe("captured issue status filters", () => {
  it.each([
    [{ "status-ids": [4, 8] }, true],
    [{ "status-categories": ["planned", "started"] }, true],
    [{ "status-ids": [8], "status-categories": ["started"] }, true],
    [{ "status-ids": [5], "status-categories": ["started"] }, false],
    [{ "status-ids": [8], "status-categories": ["completed"] }, false],
    [{ "status-ids": [5] }, false],
  ])("matches event.status with OR within and AND across dimensions: %j", (filters, expected) => {
    expect(matchesIssueStatus("issues", payload, filters)).toBe(expected);
  });

  it.each([
    undefined,
    null,
    {},
    [],
    "started",
    { "status-ids": [] },
    { "status-ids": null },
    { "status-ids": ["8"] },
    { "status-ids": [0] },
    { "status-ids": [1.5] },
    { "status-ids": [Number.MAX_SAFE_INTEGER + 1] },
    { "status-categories": [] },
    { "status-categories": ["STARTED"] },
    { "status-categories": ["unknown"] },
    { "status-ids": [8], unexpected: true },
  ])("fails closed for malformed filter %j", filters => {
    expect(matchesIssueStatus("issues", payload, filters)).toBe(false);
  });

  it.each([undefined, null, {}, [], { id: "8", name: "Review", category: "started" }, { id: 8, category: "started" }, { id: 8, name: "Review" }])(
    "fails closed for malformed/missing captured status %j even with matching snapshot",
    status => {
      expect(matchesIssueStatus("issues", { ...payload, status, issue: { status: review } }, { "status-ids": [8] })).toBe(false);
    }
  );

  it.each(["opened", "assigned", "closed", "reopened"])("leaves mixed issue action %s unaffected", action => {
    expect(matchesIssueStatus("issues", { action }, { "status-ids": [8] })).toBe(true);
  });

  it("leaves other event families unaffected", () => {
    expect(matchesIssueStatus("workflow_dispatch", {}, { "status-ids": [8] })).toBe(true);
  });

  it("does not match names, including after a rename", () => {
    expect(matchesIssueStatus("issues", { ...payload, status: { ...review, name: "Renamed" } }, { "status-ids": [8] })).toBe(true);
  });

  it("evaluates repeated and stale transitions independently without deduplication or freshness claims", () => {
    for (let attempt = 0; attempt < 2; attempt++) {
      expect(matchesIssueStatus("issues", payload, { "status-ids": [8] })).toBe(true);
    }
    expect(matchesIssueStatus("issues", { ...payload, status: started }, { "status-ids": [4] })).toBe(true);
  });

  it("writes a blocking output for malformed JSON configuration", async () => {
    global.core = { setOutput: vi.fn() };
    global.context = { eventName: "issues", payload };
    process.env.GH_AW_ISSUE_STATUS_FILTER = "{";
    await main();
    expect(global.core.setOutput).toHaveBeenCalledWith("status_match", "false");
  });

  it("writes a matching output from the captured event", async () => {
    global.core = { setOutput: vi.fn() };
    global.context = { eventName: "issues", payload };
    process.env.GH_AW_ISSUE_STATUS_FILTER = JSON.stringify({ "status-ids": [8] });
    await main();
    expect(global.core.setOutput).toHaveBeenCalledWith("status_match", "true");
  });
});

describe("issue status agent context", () => {
  it("preserves event/current distinction, previous status, and id/name/category", () => {
    global.context = { repo: { owner: "owner", repo: "repo" }, eventName: "issues", payload };
    expect(buildAwContext().issue_status).toEqual({ event: review, previous: started, snapshot: done });
  });

  it("preserves a null prior status on first assignment", () => {
    global.context = {
      repo: { owner: "owner", repo: "repo" },
      eventName: "issues",
      payload: { ...payload, changes: { status: { from: null } } },
    };
    expect(buildAwContext().issue_status.previous).toBeNull();
  });

  it("retains issue snapshot on other actions without inventing a transition", () => {
    global.context = { repo: { owner: "owner", repo: "repo" }, eventName: "issues", payload: { action: "opened", issue: { number: 42, status: started } } };
    expect(buildAwContext().issue_status).toEqual({ snapshot: started });
  });

  it("does not invent a status when read/search responses omit it", () => {
    global.context = { repo: { owner: "owner", repo: "repo" }, eventName: "issues", payload: { issue: { number: 42 } } };
    expect(buildAwContext().issue_status).toEqual({ snapshot: null });
  });

  it("preserves captured status context across workflow dispatch without upgrading it to latest state", () => {
    const issue_status = { event: review, previous: started, snapshot: done };
    global.context = {
      repo: { owner: "owner", repo: "repo" },
      eventName: "workflow_dispatch",
      payload: { inputs: { aw_context: JSON.stringify({ issue_status }) } },
    };
    expect(buildAwContext().issue_status).toEqual(issue_status);
  });
});
