import { describe, expect, it } from "vitest";
import { createHmac } from "node:crypto";
import { applyAndPublishIssues, readIssues, QUEUE_LABEL } from "./work_queue_issues_store.cjs";
import { applyAndPublishWorkQueueTransactions, readWorkQueueLog } from "./work_queue_store.cjs";
import { CURRENT_VERSION } from "./work_queue_codemods.cjs";

const secret = "test-only-work-queue-hmac-secret";
const recordPrefix = "<!-- gh-aw-work-queue:v1 -->\n";
const work = id => ({ version: CURRENT_VERSION, kind: "Work", work: id, claim: null, attempt: null });
const claim = id => ({ version: CURRENT_VERSION, kind: "Claim", work: "one", claim: id, attempt: null });
const completion = id => ({ version: CURRENT_VERSION, kind: "Completion", work: "one", claim: id, attempt: "run-1" });
const canonical = value => {
  if (value === null || typeof value !== "object") return JSON.stringify(value);
  if (Array.isArray(value)) return `[${value.map(canonical).join(",")}]`;
  return `{${Object.keys(value)
    .sort()
    .map(key => `${JSON.stringify(key)}:${canonical(value[key])}`)
    .join(",")}}`;
};
const signedRecord = payload =>
  `${recordPrefix}${JSON.stringify({
    payload,
    sig: createHmac("sha256", secret).update(canonical(payload)).digest("hex"),
  })}`;

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
        const created = { id: ++id, number: id, created_at: new Date((sameTimestamp ? 1 : id) * 1000).toISOString(), title, body, user: { login: "github-actions[bot]" }, labels: names.map(name => ({ name })), comments: [], state: "open" };
        issues.push(created);
        return { data: created };
      },
      createComment: async ({ issue_number, body }) => {
        const comment = { id: ++id, created_at: new Date((sameTimestamp ? 1 : id) * 1000).toISOString(), body, user: { login: "github-actions[bot]" } };
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

const options = fake => ({ githubClient: fake.githubClient, owner: "owner", repo: "repo", secret });

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
      body: signedRecord(completion("claim-b")),
    });
    expect((await readIssues(options(fake))).transactions).toHaveLength(4);
  });

  it("deduplicates simultaneous identical submissions and rejects conflicting records", async () => {
    const fake = fakeClient();
    await applyAndPublishIssues({ ...options(fake), intents: [work("one")] });
    await fake.githubClient.rest.issues.create({
      title: "duplicate",
      body: fake.issues[0].body,
      labels: [QUEUE_LABEL],
    });

    expect((await readIssues(options(fake))).transactions).toHaveLength(1);
    fake.issues[1].body = signedRecord({ ...work("one"), enqueued: 42 });
    await expect(readIssues(options(fake))).rejects.toThrow("conflicting Work records");
  });

  it("cleans up the losing issue when two writers submit the same Work concurrently", async () => {
    const fake = fakeClient();
    const create = fake.githubClient.rest.issues.create;
    let competing = true;
    fake.githubClient.rest.issues.create = async args => {
      if (competing) {
        competing = false;
        await create(args);
      }
      return create(args);
    };
    await applyAndPublishIssues({ ...options(fake), intents: [work("one")] });
    expect(fake.issues).toHaveLength(2);
    expect(fake.issues[0].labels.some(label => label.name === QUEUE_LABEL)).toBe(true);
    expect(fake.issues[1].labels).toEqual([]);
    expect((await readIssues(options(fake))).transactions).toHaveLength(1);
  });

  it("replays Work before comments created within the same timestamp", async () => {
    const fake = fakeClient(true);
    await applyAndPublishIssues({ ...options(fake), intents: [work("one"), claim("claim-1"), completion("claim-1")] });
    expect((await readIssues(options(fake))).transactions).toHaveLength(3);
  });

  it("routes the existing runtime store API to issues without changing the MCP protocol", async () => {
    const fake = fakeClient();
    const previous = process.env.GH_AW_WORK_QUEUE_STORAGE;
    process.env.GH_AW_WORK_QUEUE_STORAGE = "issues";
    try {
      await applyAndPublishWorkQueueTransactions({ ...options(fake), intents: [work("one")] });
      expect((await readWorkQueueLog(options(fake))).transactions).toEqual([work("one")]);
    } finally {
      if (previous === undefined) delete process.env.GH_AW_WORK_QUEUE_STORAGE;
      else process.env.GH_AW_WORK_QUEUE_STORAGE = previous;
    }
  });

  it("ignores public comments even when they contain forged or malformed transactions", async () => {
    const fake = fakeClient();
    await applyAndPublishIssues({ ...options(fake), intents: [work("one"), claim("claim-1")] });
    const comment = (await fake.githubClient.rest.issues.createComment({ issue_number: 1, body: signedRecord(completion("claim-1")) })).data;
    comment.user.login = "untrusted";
    expect((await readIssues(options(fake))).transactions).toHaveLength(2);
    await applyAndPublishIssues({ ...options(fake), intents: [completion("claim-1")] });
    expect((await readIssues(options(fake))).transactions).toHaveLength(3);
  });

  it("ignores records without a valid signature", async () => {
    const fake = fakeClient();
    await applyAndPublishIssues({ ...options(fake), intents: [work("one")] });
    await fake.githubClient.rest.issues.createComment({
      issue_number: 1,
      body: `${recordPrefix}${JSON.stringify({ payload: completion("forged"), sig: "0".repeat(64) })}`,
    });
    expect((await readIssues(options(fake))).transactions).toEqual([work("one")]);
    fake.issues[0].body = `${recordPrefix}${JSON.stringify(work("one"))}`;
    await expect(readIssues(options(fake))).rejects.toThrow("Labeled work queue issue has no valid Work record");
  });

  it("requires the work queue HMAC secret", async () => {
    const fake = fakeClient();
    await expect(readIssues({ ...options(fake), secret: "" })).rejects.toThrow("Work queue HMAC secret is not configured");
  });
});
