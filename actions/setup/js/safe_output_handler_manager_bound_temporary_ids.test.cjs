import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const protocol = require("./safe_output_authorization.cjs");
const createIssueModule = require("./create_issue.cjs");
const updateProjectModule = require("./update_project.cjs");
const { loadHandlers, processMessages } = require("./safe_output_handler_manager.cjs");
const root = path.resolve(`.manager-bound-temporary-ids-${process.pid}`);
const requirement = { endpoint: "https://authority.example/authorize", audience: "safe-outputs", source: "local" };

beforeEach(() => {
  vi.stubGlobal("core", { info: vi.fn(), debug: vi.fn(), warning: vi.fn(), error: vi.fn(), setOutput: vi.fn(), setFailed: vi.fn() });
  vi.stubGlobal("github", { rest: {} });
  vi.stubGlobal("context", { repo: { owner: "org", repo: "repo" }, payload: {} });
  for (const [key, value] of Object.entries({
    RUNNER_TEMP: root,
    GITHUB_REPOSITORY: "org/repo",
    GH_AW_WORKFLOW_ID: "test-workflow",
    GITHUB_JOB: "safe_outputs",
    GH_AW_JOB_CHECK_RUN_ID: "789",
    GITHUB_REPOSITORY_ID: "123",
    GITHUB_WORKFLOW_REF: "org/repo/.github/workflows/test.yml@refs/heads/main",
    GITHUB_WORKFLOW_SHA: "a".repeat(40),
    GH_AW_AUTHORIZATION_WORKFLOW_HASH: "b".repeat(64),
    GITHUB_RUN_ID: "456",
    GITHUB_RUN_ATTEMPT: "1",
    GH_AW_SAFE_OUTPUTS_STAGED: "false",
  }))
    vi.stubEnv(key, value);
  vi.spyOn(protocol, "recordAuthorizationOutcome").mockReturnValue({ evidenceRecorded: true });
  vi.spyOn(protocol, "assertAuthorizationFresh").mockImplementation(({ authorization }) => authorization);
  fs.mkdirSync(root, { recursive: true });
});

afterEach(() => {
  fs.rmSync(root, { recursive: true, force: true });
  vi.restoreAllMocks();
  vi.unstubAllEnvs();
  vi.unstubAllGlobals();
});

/** Allows every invocation and runs `duringWait` while the authority decision is pending. */
function allowWith(duringWait) {
  return vi.spyOn(protocol, "authorizePreparedInvocation").mockImplementation(async ({ invocation }) => {
    duringWait();
    return { state: "allowed", invocation_id: invocation.invocation_id, expires_at: new Date(Date.now() + 30000).toISOString() };
  });
}

describe("protected executors read temporary IDs from the bound snapshot", () => {
  it("does not let a live-map consumer observe an entry mutated during the authority wait, and still publishes new IDs", async () => {
    const entry = { projectUrl: "https://github.com/orgs/org/projects/1" };
    vi.spyOn(createIssueModule, "main").mockResolvedValue(async () => ({ success: true, temporaryId: "aw_proj", temporaryIdEntry: entry }));
    const observed = [];
    vi.spyOn(updateProjectModule, "main").mockResolvedValue(async (message, resolvedTemporaryIds, temporaryIdMap) => {
      observed.push({ map: temporaryIdMap instanceof Map, live: temporaryIdMap.get("aw_proj")?.projectUrl, bound: resolvedTemporaryIds.aw_proj.projectUrl });
      temporaryIdMap.set("aw_draft", { draftItemId: "item-1" });
      observed.push({ own: temporaryIdMap.get("aw_draft")?.draftItemId });
      return { success: true };
    });
    allowWith(() => {
      entry.projectUrl = "https://github.com/orgs/evil/projects/9";
    });
    const handlers = await loadHandlers({ create_issue: {}, update_project: { authorization: requirement } });
    const { results, temporaryIdMap } = await processMessages(handlers, [
      { type: "create_issue", title: "Provider" },
      { type: "update_project", project: "aw_proj", content_type: "draft_issue", draft_title: "Draft" },
    ]);
    expect(results.find(record => record.type === "update_project")).toMatchObject({ success: true, authorization: { state: "allowed" } });
    expect(observed).toEqual([{ map: true, live: "https://github.com/orgs/org/projects/1", bound: "https://github.com/orgs/org/projects/1" }, { own: "item-1" }]);
    expect(temporaryIdMap.aw_draft).toEqual({ draftItemId: "item-1" });
  });

  it("refreshes a reused add_comment handler cache from each invocation's bound snapshot", async () => {
    const entry = { repo: "org/repo", number: 42 };
    vi.spyOn(createIssueModule, "main").mockResolvedValue(async () => ({ success: true, temporaryId: "aw_target", temporaryIdEntry: entry }));
    const posted = [];
    const client = {
      rest: {
        issues: {
          get: vi.fn(async ({ issue_number }) => ({ data: { number: issue_number, state: "open", html_url: `https://github.com/org/repo/issues/${issue_number}` } })),
          listComments: vi.fn(async () => ({ data: [] })),
          createComment: vi.fn(async ({ issue_number, body }) => {
            posted.push(issue_number);
            return { data: { id: posted.length, html_url: `https://github.com/org/repo/issues/${issue_number}#c${posted.length}`, body } };
          }),
        },
      },
      graphql: vi.fn(async () => ({})),
    };
    vi.stubGlobal("github", client);
    let waits = 0;
    const authorize = allowWith(() => {
      if (++waits === 1) entry.number = 99;
    });
    const handlers = await loadHandlers({ create_issue: {}, add_comment: { authorization: requirement, target: "*", max: 5 } });
    const { results } = await processMessages(handlers, [
      { type: "create_issue", title: "Parent" },
      { type: "add_comment", item_number: '"AW_target"', body: "first" },
      { type: "add_comment", item_number: '"AW_target"', body: "second" },
    ]);
    const bound = authorize.mock.calls.map(([{ invocation }]) => invocation);
    expect(bound).toHaveLength(2);
    expect(results.filter(record => record.type === "add_comment")).toMatchObject([{ success: true }, { success: true }]);
    expect(posted).toEqual([42, 99]);
  });
});
