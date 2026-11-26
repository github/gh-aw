import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";

const { main } = require("./set_issue_status.cjs");
const { PLANNING_STATUS_HEADERS, resolvePlanningStatus } = require("./planning_status.cjs");
const { extractCreatedItemFromResult } = require("./safe_output_manifest.cjs");
const { buildSafeOutputExecutions } = require("./safe_output_execution.cjs");
const statuses = [
  { id: 1, name: "Backlog", category: "backlog" },
  { id: 2, name: "Todo", category: "planned" },
  { id: 3, name: "In progress", category: "started" },
  { id: 4, name: "In review", category: "started" },
  { id: 5, name: "Done", category: "completed" },
  { id: 6, name: "Not planned", category: "not_planned" },
  { id: 7, name: "Duplicate", category: "duplicate" },
];
let client;
let current;
let updated;
let available;
let lastPatchedStatus;

beforeEach(() => {
  current = { id: 100, number: 42, state: "open", state_reason: null, status: statuses[0], labels: [], title: "Test issue", html_url: "https://github.com/owner/repo/issues/42", repository_url: "https://api.github.com/repos/owner/repo" };
  available = statuses;
  updated = undefined;
  lastPatchedStatus = undefined;
  client = {
    rest: {
      issues: {
        get: vi.fn(async ({ issue_number }) => ({
          data: issue_number === 42 ? current : { id: 200, number: issue_number, state: "open", repository_url: current.repository_url, html_url: `https://github.com/owner/repo/issues/${issue_number}` },
        })),
        update: vi.fn(),
      },
    },
    request: vi.fn(async (route, parameters) => {
      if (route.startsWith("GET ")) return { data: available };
      const status = statuses.find(value => value.name === parameters.status);
      lastPatchedStatus = status;
      return { data: updated || { ...current, status, state: status.id > 4 ? "closed" : "open", state_reason: status.id > 4 ? status.category : null } };
    }),
    graphql: vi.fn(async () => ({
      repository: {
        issue: {
          number: 42,
          duplicateOf: (lastPatchedStatus || current.status)?.category === "duplicate" ? { fullDatabaseId: "200", number: 9, repository: { nameWithOwner: "owner/repo" } } : null,
        },
      },
    })),
  };
  vi.stubGlobal("github", client);
  vi.stubGlobal("context", { repo: { owner: "owner", repo: "repo" }, payload: { issue: { number: 42 } }, eventName: "issues" });
  vi.stubGlobal("core", Object.fromEntries(["info", "debug", "warning", "error"].map(key => [key, vi.fn()])));
  vi.stubEnv("GH_AW_SAFE_OUTPUTS_STAGED", "");
});

it("releases the max slot when a temporary target is deferred", async () => {
  const handler = await main({ max: 1, target: "*" });
  const deferred = await handler({ status: "Todo", issue_number: "aw_target" }, {});
  expect(deferred).toMatchObject({ success: false, deferred: true });
  const retried = await handler({ status: "Todo", issue_number: "aw_target" }, { aw_target: { repo: "owner/repo", number: 42 } });
  expect(retried.success).toBe(true);
  expect(patches()).toHaveLength(1);
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
});

const handle = async (message = {}, config = {}) => (await main(config))({ type: "set_issue_status", status: "Todo", ...message }, {});
const patches = () => client.request.mock.calls.filter(([route]) => route.startsWith("PATCH "));

describe("native planning status resolution", () => {
  it.each(["Todo", "todo", " TODO ", 2])("resolves %j to canonical repository identity", async selector => {
    expect(await resolvePlanningStatus(client, "owner", "repo", selector)).toEqual(statuses[1]);
    expect(client.request).toHaveBeenCalledWith("GET /repos/{owner}/{repo}/planning-statuses", { owner: "owner", repo: "repo", headers: PLANNING_STATUS_HEADERS });
  });

  it.each([undefined, null, "", " ", 0, -1, 1.1, Number.MAX_SAFE_INTEGER + 1, {}, [], true])("rejects invalid selector %j before discovery", async selector => {
    await expect(resolvePlanningStatus(client, "owner", "repo", selector)).rejects.toThrow("non-empty");
    expect(client.request).not.toHaveBeenCalled();
  });

  it.each([["todo"], [2], ["Backlog", 2]].map(allowed => [allowed]))("accepts allowed aliases %j", async allowed => {
    expect(await resolvePlanningStatus(client, "owner", "repo", 2, allowed)).toEqual(statuses[1]);
  });

  it.each([["Backlog"], [3], ["2"]].map(allowed => [allowed]))("denies mismatched allowed selector %j", async allowed => {
    await expect(resolvePlanningStatus(client, "owner", "repo", "Todo", allowed)).rejects.toThrow("allowed list");
  });

  it.each([["*"], [null], [0], "Todo"].map(allowed => [allowed]))("fails closed for malformed or nonmatching policy %j", async allowed => {
    await expect(resolvePlanningStatus(client, "owner", "repo", "Todo", allowed)).rejects.toThrow();
  });

  it("rejects unknown status and ambiguous names instead of choosing arbitrarily", async () => {
    await expect(resolvePlanningStatus(client, "owner", "repo", "New")).rejects.toThrow("Unknown");
    available = [...statuses, { id: 8, name: "todo", category: "planned" }];
    await expect(resolvePlanningStatus(client, "owner", "repo", "Todo")).rejects.toThrow("Ambiguous");
    await expect(resolvePlanningStatus(client, "owner", "repo", 2)).rejects.toThrow("Ambiguous");
  });

  it.each([{}, [], [{ id: "2", name: "Todo", category: "planned" }], [{ id: 2, name: "Todo", category: "future" }]])("rejects malformed or unavailable discovery %j", async response => {
    available = response;
    await expect(resolvePlanningStatus(client, "owner", "repo", "Todo")).rejects.toThrow();
  });

  it.each([403, 404, 422, 500])("propagates unavailable API status %i without fallback", async status => {
    client.request.mockRejectedValue(Object.assign(new Error("API failed"), { status }));
    expect((await handle()).success).toBe(false);
    expect(client.rest.issues.update).not.toHaveBeenCalled();
    expect(patches()).toHaveLength(0);
  });

  it.each(statuses.slice(4))("forbids closed initial status $name", async status => {
    await expect(resolvePlanningStatus(client, "owner", "repo", status.id, [], true)).rejects.toThrow("open category");
  });
});

describe("set_issue_status", () => {
  it("uses canonical names on the documented REST route with the preview API version", async () => {
    const result = await handle({ status: 2 });
    expect(result).toMatchObject({
      success: true,
      issue_number: 42,
      repo: "owner/repo",
      url: current.html_url,
      before_state: { state: "open", planning_status: statuses[0] },
      after_state: { state: "open", planning_status: statuses[1] },
      metadata: { planning_status: statuses[1] },
    });
    expect(patches()).toEqual([["PATCH /repos/{owner}/{repo}/issues/{issue_number}", { owner: "owner", repo: "repo", issue_number: 42, status: "Todo", headers: PLANNING_STATUS_HEADERS, request: { redirect: "error" } }]]);
    expect(client.rest.issues.get).toHaveBeenCalledWith({ owner: "owner", repo: "repo", issue_number: 42, headers: PLANNING_STATUS_HEADERS, request: { redirect: "error" } });
    expect(client.rest.issues.update).not.toHaveBeenCalled();
  });

  it("records verified manifest snapshots and a successful outcome only after application", async () => {
    const result = await handle();
    expect(extractCreatedItemFromResult("set_issue_status", result)).toMatchObject({ status: "success", url: current.html_url, number: 42, before_state: result.before_state, after_state: result.after_state });
    expect(buildSafeOutputExecutions([{ type: "set_issue_status", result }])[0]).toMatchObject({ type: "set_issue_status", status: "success", url: current.html_url });
  });

  it.each(["Done", "Not planned", "Duplicate"])("requires explicit close authorization for %s", async status => {
    const result = await handle({ status, duplicate_of: status === "Duplicate" ? 9 : undefined });
    expect(result.error).toContain("allow-close");
    expect(patches()).toHaveLength(0);
  });

  it.each(["Done", "Not planned"])("closes with explicit permission for %s", async status => {
    expect((await handle({ status }, { allow_close: true })).success).toBe(true);
    expect(patches()[0][1].status).toBe(status);
  });

  it("requires reopening authorization and re-fetches current state instead of the webhook", async () => {
    current = { ...current, state: "closed", state_reason: "completed", status: statuses[4] };
    expect((await handle()).error).toContain("allow-reopen");
    expect((await handle({}, { allow_reopen: true })).success).toBe(true);
  });

  it.each([{ allow_close: true }, { allow_reopen: true }, {}, { allow_close: "true", allow_reopen: true }])("requires both authorizations to change closed reason %j", async config => {
    current = { ...current, state: "closed", state_reason: "completed", status: statuses[4] };
    expect((await handle({ status: "Not planned" }, config)).success).toBe(false);
    expect(patches()).toHaveLength(0);
  });

  it("changes closed reason only with both flags", async () => {
    current = { ...current, state: "closed", state_reason: "completed", status: statuses[4] };
    expect((await handle({ status: "Not planned" }, { allow_close: true, allow_reopen: true })).success).toBe(true);
  });

  it("resolves duplicate issue number to database ID and records the canonical number", async () => {
    const result = await handle({ status: "Duplicate", duplicate_of: 9 }, { allow_close: true });
    expect(result).toMatchObject({ success: true, metadata: { duplicate_of: 9 }, before_state: { duplicate_issue_id: null }, after_state: { duplicate_issue_id: 200 } });
    expect(patches()[0][1]).toMatchObject({ status: "Duplicate", duplicate_issue_id: 200 });
    expect(patches()[0][1]).not.toHaveProperty("duplicate_of");
    expect(client.graphql).toHaveBeenCalledTimes(2);
    expect(client.graphql.mock.calls[0][0]).toContain("duplicateOf");
    expect(client.graphql.mock.calls[0][0]).toContain("fullDatabaseId");
  });

  it("only no-ops Duplicate when the canonical target is proven identical", async () => {
    current = { ...current, status: statuses[6], state: "closed", state_reason: "duplicate" };
    expect(await handle({ status: "Duplicate", duplicate_of: 9 })).toMatchObject({ success: true, skipped: true });
    expect(client.graphql).toHaveBeenCalledOnce();
    expect(patches()).toHaveLength(0);
  });

  it("requires both lifecycle flags when retargeting a closed duplicate", async () => {
    current = { ...current, status: statuses[6], state: "closed", state_reason: "duplicate" };
    client.graphql.mockResolvedValue({ repository: { issue: { number: 42, duplicateOf: { fullDatabaseId: "201", number: 10, repository: { nameWithOwner: "owner/repo" } } } } });
    expect((await handle({ status: "Duplicate", duplicate_of: 9 })).error).toContain("both allow-close");
    expect(patches()).toHaveLength(0);
  });

  it("retargets only with both flags and verified before/after duplicate identities", async () => {
    current = { ...current, status: statuses[6], state: "closed", state_reason: "duplicate" };
    client.graphql.mockResolvedValueOnce({ repository: { issue: { number: 42, duplicateOf: { fullDatabaseId: "201", number: 10, repository: { nameWithOwner: "owner/repo" } } } } });
    const result = await handle({ status: "Duplicate", duplicate_of: 9 }, { allow_close: true, allow_reopen: true });
    expect(result).toMatchObject({ success: true, before_state: { duplicate_issue_id: 201 }, after_state: { duplicate_issue_id: 200 } });
    expect(patches()).toHaveLength(1);
  });

  it.each([null, {}, { repository: { issue: { number: 42 } } }, { repository: { issue: { number: 42, duplicateOf: null } } }])("fails closed when current Duplicate relation cannot be proven (%j)", async response => {
    current = { ...current, status: statuses[6], state: "closed", state_reason: "duplicate" };
    client.graphql.mockResolvedValue(response);
    expect((await handle({ status: "Duplicate", duplicate_of: 9 }, { allow_close: true, allow_reopen: true })).success).toBe(false);
    expect(patches()).toHaveLength(0);
  });

  it("fails closed before writing if the documented duplicate field is unsupported", async () => {
    client.graphql.mockRejectedValue(new Error("Field 'duplicateOf' doesn't exist"));
    expect((await handle({ status: "Duplicate", duplicate_of: 9 }, { allow_close: true })).success).toBe(false);
    expect(patches()).toHaveLength(0);
  });

  it("does not claim application when the returned duplicate relationship differs", async () => {
    client.graphql
      .mockResolvedValueOnce({ repository: { issue: { number: 42, duplicateOf: null } } })
      .mockResolvedValueOnce({ repository: { issue: { number: 42, duplicateOf: { fullDatabaseId: "201", number: 10, repository: { nameWithOwner: "owner/repo" } } } } });
    const result = await handle({ status: "Duplicate", duplicate_of: 9 }, { allow_close: true });
    expect(result).toMatchObject({ success: false, issue_number: 42, url: current.html_url });
    expect(result.error).toContain("PATCH was sent to owner/repo#42");
    expect(result.error).toContain("may have been applied");
    expect(result.error).toContain("canonical duplicate");
    expect(extractCreatedItemFromResult("set_issue_status", result)).toBeNull();
    expect(patches()).toHaveLength(1);
  });

  it.each([undefined, null, 0, -1, 42, 1.1, "9"])("rejects invalid duplicate reference %j", async duplicate_of => {
    expect((await handle({ status: "Duplicate", duplicate_of }, { allow_close: true })).error).toContain("duplicate_of");
    expect(patches()).toHaveLength(0);
  });

  it("fails closed when the current issue response omits status", async () => {
    delete current.status;
    const result = await handle();
    expect(result).toMatchObject({ success: false });
    expect(result.error).toContain("response omitted status");
    expect(patches()).toHaveLength(0);
  });

  it("rejects a duplicate reference for other statuses", async () => {
    expect((await handle({ duplicate_of: 9 })).error).toContain("only supported");
  });

  it("rejects inaccessible and pull-request duplicate targets", async () => {
    client.rest.issues.get.mockImplementation(async ({ issue_number }) => {
      if (issue_number === 42) return { data: current };
      return { data: { id: 200, pull_request: {} } };
    });
    expect((await handle({ status: "Duplicate", duplicate_of: 9 }, { allow_close: true })).success).toBe(false);
    expect(patches()).toHaveLength(0);
  });

  it.each([{ number: 10 }, { repository_url: "https://api.github.com/repos/foreign/repo" }, { html_url: "https://github.com/foreign/repo/issues/9" }, { repository_url: undefined }, { html_url: undefined }])(
    "rejects transferred or unverified duplicate targets before PATCH (%j)",
    async changes => {
      client.rest.issues.get.mockImplementation(async ({ issue_number }) => ({
        data: issue_number === 42 ? current : { id: 200, number: 9, repository_url: current.repository_url, html_url: "https://github.com/owner/repo/issues/9", ...changes },
      }));
      expect((await handle({ status: "Duplicate", duplicate_of: 9 }, { allow_close: true })).error).toContain("authorized target repository");
      expect(patches()).toHaveLength(0);
      expect(client.graphql).not.toHaveBeenCalled();
    }
  );

  it.each([{ number: 99 }, { repository_url: "https://api.github.com/repos/foreign/repo" }, { html_url: "https://github.com/foreign/repo/issues/42" }])("rejects a redirected source issue before mutation (%j)", async changes => {
    Object.assign(current, changes);
    expect((await handle()).error).toContain("authorized repository");
    expect(patches()).toHaveLength(0);
  });

  it.each([null, statuses[1], statuses[4], statuses[6]])("handles current status %j without legacy state fallback", async status => {
    current.status = status;
    if (status?.category === "completed" || status?.category === "duplicate") {
      current.state = "closed";
      current.state_reason = status.category;
    }
    const result = await handle({ status: status?.id ?? 2, ...(status?.category === "duplicate" ? { duplicate_of: 9 } : {}) });
    expect(result.success).toBe(true);
    if (status) {
      expect(result.skipped).toBe(true);
      expect(patches()).toHaveLength(0);
      expect(extractCreatedItemFromResult("set_issue_status", result)).toBeNull();
      expect(buildSafeOutputExecutions([{ type: "set_issue_status", result }])[0].status).toBe("skipped");
    } else {
      expect(patches()).toHaveLength(1);
    }
  });

  it.each([{ state: "mystery" }, { pull_request: {} }, { status: { id: 8, name: "Future", category: "future" } }])("rejects unsupported current issue %j", async changes => {
    Object.assign(current, changes);
    expect((await handle()).success).toBe(false);
    expect(patches()).toHaveLength(0);
  });

  it.each([{ status: null }, { status: statuses[0] }, { status: statuses[1], state: "closed" }])("does not claim ignored or inconsistent writes %j", async changes => {
    updated = { ...current, ...changes };
    const result = await handle();
    expect(result).toMatchObject({ success: false, issue_number: 42, url: current.html_url });
    expect(result.error).toContain("did not apply");
    expect(extractCreatedItemFromResult("set_issue_status", result)).toBeNull();
    expect(buildSafeOutputExecutions([{ type: "set_issue_status", result }])[0]).toMatchObject({ status: "failed" });
    expect(buildSafeOutputExecutions([{ type: "set_issue_status", result }])[0]).not.toHaveProperty("url");
  });

  it("previews validated status without mutation or applied audit effect", async () => {
    const result = await handle({}, { staged: true });
    expect(result).toMatchObject({ success: true, staged: true, previewInfo: { planning_status: statuses[1] } });
    expect(patches()).toHaveLength(0);
    expect(extractCreatedItemFromResult("set_issue_status", result)).toBeNull();
    expect(buildSafeOutputExecutions([{ type: "set_issue_status", result }])[0].status).toBe("staged");
  });

  it.each([
    ["invalid status", "unknown", statuses[0], "Unknown"],
    ["forbidden close transition", "Done", statuses[0], "allow-close"],
  ])("validates protected previews read-only for %s", async (_name, requestedStatus, initialStatus, expectedError) => {
    current = { ...current, status: initialStatus };
    const handler = await main({ allowed: statuses.map(status => status.name) });
    const result = await handler.validatePreview({ type: "set_issue_status", status: requestedStatus }, {}, new Map());
    expect(result.success).toBe(false);
    expect(result.error).toContain(expectedError);
    expect(patches()).toHaveLength(0);
    expect(client.rest.issues.update).not.toHaveBeenCalled();
  });

  it("fails closed on unavailable status preview support in a read-only preview", async () => {
    client.request.mockRejectedValue(Object.assign(new Error("Not Found"), { status: 404 }));
    const handler = await main({});
    const result = await handler.validatePreview({ type: "set_issue_status", status: "Todo" }, {}, new Map());
    expect(result.success).toBe(false);
    expect(result.error).toContain("unavailable");
    expect(patches()).toHaveLength(0);
  });

  it("enforces configured max", async () => {
    const handler = await main({ max: 1 });
    expect((await handler({ status: "Todo" }, {})).success).toBe(true);
    expect((await handler({ status: "In progress" }, {})).error).toContain("Max count");
    expect(patches()).toHaveLength(1);
  });

  it("defaults to triggering target and refuses target escape", async () => {
    expect((await handle({ issue_number: 99 })).success).toBe(true);
    expect(patches()[0][1].issue_number).toBe(42);
  });

  it("supports configured numeric and temporary targets", async () => {
    expect((await handle({}, { target: 42 })).success).toBe(true);
    const handler = await main({ target: "*" });
    expect((await handler({ status: "Todo", issue_number: "aw_new" }, { aw_new: { repo: "owner/repo", number: 42 } })).success).toBe(true);
  });

  it("rejects foreign temporary IDs", async () => {
    const handler = await main({ target: "*" });
    expect((await handler({ status: "Todo", issue_number: "aw_new" }, { aw_new: { repo: "other/repo", number: 42 } })).success).toBe(false);
    expect(patches()).toHaveLength(0);
  });

  it("skips triggering output outside issue events", async () => {
    global.context.payload = {};
    global.context.eventName = "workflow_dispatch";
    expect(await handle()).toMatchObject({ success: false, skipped: true });
  });

  it("enforces repository, labels and title prefix restrictions before status writes", async () => {
    expect((await handle({ repo: "other/repo" })).success).toBe(false);
    expect((await handle({}, { required_labels: ["approved"] })).success).toBe(false);
    expect((await handle({}, { required_title_prefix: "[Approved]" })).success).toBe(false);
    expect(patches()).toHaveLength(0);
  });

  it("uses per-handler authenticated clients", async () => {
    vi.stubGlobal("getOctokit", vi.fn().mockReturnValue(client));
    expect((await handle({}, { "github-token": "test-token" })).success).toBe(true);
    expect(global.getOctokit).toHaveBeenCalledWith("test-token", expect.any(Object));
  });

  it.each([{ suggest: true }, { suggest: false }, { rationale: "test" }, { confidence: "HIGH" }])("rejects unsupported issue intent %j", async metadata => {
    expect((await handle(metadata)).success).toBe(false);
    expect(patches()).toHaveLength(0);
  });

  it("does not retry or fall back on update permission failure", async () => {
    client.request.mockImplementation(async route => {
      if (route.startsWith("GET ")) return { data: statuses };
      throw Object.assign(new Error("Forbidden"), { status: 403 });
    });
    expect(await handle()).toMatchObject({ success: false, error: "Forbidden" });
    expect(patches()).toHaveLength(1);
    expect(client.rest.issues.update).not.toHaveBeenCalled();
  });
});
