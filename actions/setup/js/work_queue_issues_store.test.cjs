import { describe, expect, it } from "vitest";
import { applyAndPublishIssues, readIssues, QUEUE_LABEL } from "./work_queue_issues_store.cjs";
import { CURRENT_VERSION } from "./work_queue_codemods.cjs";

const work = id => ({ version: CURRENT_VERSION, kind: "Work", work: id, claim: null, attempt: null });
const claim = id => ({ version: CURRENT_VERSION, kind: "Claim", work: "one", claim: id, attempt: null });
const completion = id => ({ version: CURRENT_VERSION, kind: "Completion", work: "one", claim: id, attempt: "run-1" });

function fakeClient(sameTimestamp = false) {
  const issues = [];
  const labels = new Set();
  let id = 0;
  const issue = number => issues.find(value => value.number === number);
  const rest = {
    issues: {
      listForRepo: async ({ labels: filter, state, page, per_page }) => ({
        data: issues.filter(value => value.labels.some(label => label.name === filter) && (state === "all" || value.state === state)).slice((page - 1) * per_page, page * per_page),
      }),
      listComments: async ({ issue_number, page, per_page }) => ({ data: issue(issue_number).comments.slice((page - 1) * per_page, page * per_page) }),
      create: async ({ title, body, labels: names }) => {
        const created = { id: ++id, number: id, created_at: new Date((sameTimestamp ? 1 : id) * 1000).toISOString(), title, body, labels: names.map(name => ({ name })), comments: [], state: "open" };
        issues.push(created);
        return { data: created };
      },
      createComment: async ({ issue_number, body }) => {
        const comment = { id: ++id, created_at: new Date((sameTimestamp ? 1 : id) * 1000).toISOString(), body };
        issue(issue_number).comments.push(comment);
        return { data: comment };
      },
      get: async ({ issue_number }) => ({ data: issue(issue_number) }),
      getLabel: async ({ name }) => {
        if (!labels.has(name)) throw Object.assign(new Error("missing"), { status: 404 });
        return { data: { name } };
      },
      createLabel: async ({ name }) => {
        labels.add(name);
        return { data: { name } };
      },
      addLabels: async ({ issue_number, labels: names }) => {
        issue(issue_number).labels.push(...names.map(name => ({ name })));
      },
      removeLabel: async ({ issue_number, name }) => {
        issue(issue_number).labels = issue(issue_number).labels.filter(label => label.name !== name);
      },
    },
  };
  return { githubClient: { rest }, issues };
}

const options = fake => ({ githubClient: fake.githubClient, owner: "owner", repo: "repo" });

describe("issue-backed work queue", () => {
  it("stores Work in an issue, transactions in comments, and synchronizes state labels", async () => {
    const fake = fakeClient();
    const first = await applyAndPublishIssues({ ...options(fake), intents: [work("one"), claim("claim-1")] });
    expect(first.persisted).toBe(true);
    expect(fake.issues).toHaveLength(1);
    expect(fake.issues[0].labels.map(label => label.name)).toEqual([QUEUE_LABEL, "aw:work-queue:claimed"]);
    expect(fake.issues[0].comments).toHaveLength(1);
    await applyAndPublishIssues({ ...options(fake), intents: [completion("claim-1")] });
    expect(fake.issues[0].labels.map(label => label.name)).toEqual([QUEUE_LABEL, "aw:work-queue:completed"]);
    expect((await readIssues(options(fake))).transactions).toHaveLength(3);
    expect((await applyAndPublishIssues({ ...options(fake), intents: [work("one")] })).persisted).toBe(false);
  });

  it("rejects stale competing completion comments without granting them authority", async () => {
    const fake = fakeClient();
    await applyAndPublishIssues({ ...options(fake), intents: [work("one"), claim("claim-a"), claim("claim-b"), completion("claim-a")] });
    await fake.githubClient.rest.issues.createComment({
      issue_number: 1,
      body: `<!-- gh-aw-work-queue:v1 -->\n${JSON.stringify(completion("claim-b"))}`,
    });
    expect((await readIssues(options(fake))).transactions).toHaveLength(4);
  });

  it("fails closed when two issues share one Work identity", async () => {
    const fake = fakeClient();
    await applyAndPublishIssues({ ...options(fake), intents: [work("one")] });
    await fake.githubClient.rest.issues.create({
      title: "duplicate",
      body: fake.issues[0].body,
      labels: [QUEUE_LABEL],
    });
    await expect(readIssues(options(fake))).rejects.toThrow("Multiple work queue issues");
  });

  it("replays Work before comments created within the same timestamp", async () => {
    const fake = fakeClient(true);
    await applyAndPublishIssues({ ...options(fake), intents: [work("one"), claim("claim-1"), completion("claim-1")] });
    expect((await readIssues(options(fake))).transactions).toHaveLength(3);
  });
});
