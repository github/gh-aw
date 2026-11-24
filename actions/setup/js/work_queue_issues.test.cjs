// @ts-check
import { describe, expect, it } from "vitest";
import { projectIssues } from "./work_queue_issues.cjs";
import { checkpointCommit, replayTransactions } from "./work_queue_replay.cjs";
import { bind, dispatchNext, fakeQueueGitHub, finish, launch, ledgerText, reconciler, submit, work } from "./work_queue_test_helpers.cjs";

const context = { repo: { owner: "owner", repo: "repo" }, runId: 100, runAttempt: 1 };
const policy = {
  version: 4,
  pending_limit: 30,
  issues: { label: "work", title_prefix: "[work queue]" },
  effect_scope: "owner/repo",
  workers: ["worker"],
  pools: { default: { concurrency: 16, per_account_limit: 0, max_attempts: 3 } },
};

/** In-memory Issues REST API on top of the fake queue client. */
function fakeIssues(githubClient) {
  const issues = new Map();
  let nextId = 1000;
  let nextComment = 5000;
  const view = issue => ({ id: issue.id, number: issue.number, title: issue.title, body: issue.body, state: issue.state, labels: issue.labels.map(name => ({ name })), comments: issue.comments.length });
  const find = number => {
    const issue = issues.get(number);
    if (!issue) throw Object.assign(new Error("Not Found"), { status: 404 });
    return issue;
  };
  githubClient.rest.repos.get = async () => ({ data: { id: 77, full_name: "owner/repo", default_branch: "main", size: 1 } });
  githubClient.rest.issues = {
    create: async ({ title, body, labels }) => {
      const issue = { id: nextId++, number: issues.size + 1, title, body, labels, state: "open", comments: [] };
      issues.set(issue.number, issue);
      return { data: view(issue) };
    },
    get: async ({ issue_number }) => ({ data: view(find(issue_number)) }),
    update: async ({ issue_number, state }) => {
      find(issue_number).state = state;
      return { data: {} };
    },
    createComment: async ({ issue_number, body }) => {
      const comment = { id: nextComment++, body };
      find(issue_number).comments.push(comment);
      return { data: comment };
    },
    listComments: async ({ issue_number, per_page, page }) => ({ data: find(issue_number).comments.slice((page - 1) * per_page, page * per_page) }),
    listForRepo: async ({ labels, state, per_page, page }) => ({
      data: [...issues.values()]
        .filter(issue => issue.state === state && issue.labels.includes(labels))
        .slice((page - 1) * per_page, page * per_page)
        .map(view),
    }),
  };
  return issues;
}

function setup(works = [work("a")]) {
  const queue = fakeQueueGitHub(ledgerText(submit([], works)));
  const issues = fakeIssues(queue.githubClient);
  const log = () =>
    queue
      .contents()
      .trim()
      .split("\n")
      .map(line => JSON.parse(line));
  const state = () => replayTransactions(log());
  const project = (overrides = {}) => projectIssues({ githubClient: queue.githubClient, context, policy, ...overrides });
  return { queue, issues, log, state, project };
}

describe("projectIssues", () => {
  it("creates one Issue for unlinked available Work and links it, without duplicating on rerun", async () => {
    const { issues, state, project } = setup();
    expect(await project()).toEqual({ created: 1, commented: 0, closed: 0, warnings: [] });
    const [issue] = issues.values();
    expect(issue.title).toBe("[work queue] a");
    expect(issue.labels).toEqual(["work"]);
    const linked = state().works.get(work("a").work_id).backing_issue;
    expect(linked).toMatchObject({ kind: "issue", repository: "owner/repo", repository_id: "77", resource_id: String(issue.id), number: "1" });
    expect(await project()).toEqual({ created: 0, commented: 0, closed: 0, warnings: [] });
    expect(issues.size).toBe(1);
  });

  it("reuses a marked Issue whose link was not committed", async () => {
    const { issues, queue, log, state, project } = setup();
    await project();
    queue.install(ledgerText(log().slice(0, 1)));
    expect((await project()).created).toBe(0);
    expect(issues.size).toBe(1);
    expect(state().works.get(work("a").work_id).backing_issue.number).toBe("1");
  });

  it("comments on a bound Claim and closes the Issue when Work completes", async () => {
    const { issues, queue, log, state, project } = setup();
    await project();
    const { log: claimed, decision } = dispatchNext(log());
    const dispatchId = decision.dispatches[0];
    queue.install(ledgerText(bind(launch(claimed, dispatchId), dispatchId)));
    expect(await project()).toMatchObject({ created: 0, commented: 1, closed: 0 });
    expect(state().claims.get(state().works.get(work("a").work_id).claim_id).issue_comment).toBeDefined();
    expect(await project()).toMatchObject({ commented: 0 });

    queue.install(ledgerText(finish(log(), dispatchId)));
    expect(await project()).toMatchObject({ commented: 1, closed: 1, warnings: [] });
    const issue = issues.get(1);
    expect(issue.state).toBe("closed");
    expect(issue.comments.at(-1).body).toContain(`<!-- gh-aw-work-queue:${work("a").work_id}:completed -->`);
    expect(await project()).toEqual({ created: 0, commented: 0, closed: 0, warnings: [] });
    expect(issue.comments).toHaveLength(2);
  });

  it("still closes a completed Work's Issue as completed when a checkpoint lands first", async () => {
    const { issues, queue, log, state, project } = setup();
    await project();
    const { log: claimed, decision } = dispatchNext(log());
    const dispatchId = decision.dispatches[0];
    // The reconciler checkpoints before it projects: the completed Work awaits its terminal comment.
    queue.install(ledgerText([checkpointCommit(replayTransactions(finish(bind(launch(claimed, dispatchId), dispatchId), dispatchId)), reconciler, 1)]));
    expect(state().works.get(work("a").work_id)?.state).toBe("completed");
    expect(await project()).toMatchObject({ commented: 1, closed: 1, warnings: [] });
    expect(issues.get(1).comments.at(-1).body).toContain(`<!-- gh-aw-work-queue:${work("a").work_id}:completed -->`);
    // Once that comment is recorded, the next checkpoint folds the Work; its closed Issue is not orphaned.
    queue.install(ledgerText([checkpointCommit(state(), reconciler, 2)]));
    expect(state().works.size).toBe(0);
    expect(await project()).toEqual({ created: 0, commented: 0, closed: 0, warnings: [] });
  });

  it("closes an orphaned labelled Issue whose Work is absent from the ledger", async () => {
    const { issues, queue, project } = setup();
    await queue.githubClient.rest.issues.create({ title: "old", body: "<!-- gh-aw-work-queue-issue:w_gone -->", labels: ["work"] });
    expect(await project()).toMatchObject({ created: 1, closed: 1, warnings: [] });
    expect(issues.get(1).state).toBe("closed");
    expect(issues.get(1).comments[0].body).toContain("<!-- gh-aw-work-queue:w_gone:orphaned -->");
    expect(issues.get(2).state).toBe("open");
  });

  it("only warns on failures and is a no-op when Issues are disabled", async () => {
    const { issues, queue, project } = setup();
    queue.githubClient.rest.issues.create = async () => {
      throw new Error("boom");
    };
    const warnings = [];
    const result = await project({ core: { warning: text => warnings.push(text) } });
    expect(result).toMatchObject({ created: 0 });
    expect(result.warnings).toHaveLength(1);
    expect(warnings[0]).toContain("boom");
    expect(await project({ policy: { ...policy, issues: false } })).toEqual({ created: 0, commented: 0, closed: 0, warnings: [] });
    expect(await project({ policy: null })).toEqual({ created: 0, commented: 0, closed: 0, warnings: [] });
    queue.githubClient.rest.git.getRef = async () => {
      throw new Error("ledger down");
    };
    expect((await project()).warnings[0]).toContain("ledger down");
    expect(issues.size).toBe(0);
  });
});
