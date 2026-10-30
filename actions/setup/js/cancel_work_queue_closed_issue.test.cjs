import { afterEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { main, matchingWork, closedIssue, readIssuesConfig } = require("./cancel_work_queue_closed_issue.cjs");
const { registerQueueLedgerClient } = require("./work_queue_git_transport.cjs");
const { publishWorkQueueRequest, readWorkQueueLog } = require("./work_queue_store.cjs");
const { newState, replayTransactions, serializeProjection, serializeTransactionLog } = require("./work_queue_replay.cjs");
const { legacyCheckpoint } = require("./work_queue_checkpoint_regression_helpers.cjs");
const { reservationCounts } = require("./work_queue_scheduler.cjs");
const { fakeGitHub } = require("./work_queue_store_checks.cjs");
const { administrator, bind, evidence, finish, genesis, grant, operationCommit, reconciler, submission } = require("./work_queue_test_helpers.cjs");

const resource = { kind: "issue", host: "github.com", repository: "owner/repo", repository_id: "12", resource_id: "34", number: "7" };
const work = { work_id: "work_1", backing_issue: resource, state: "available", depends_on: [] };
const context = {
  repo: { owner: "owner", repo: "repo" },
  issue: { number: 7 },
  sha: "a".repeat(40),
  eventName: "issues",
  payload: { action: "closed", issue: { id: 34, number: 7, labels: [{ name: "work" }, { name: "work:queued" }] }, repository: { id: 12, full_name: "owner/repo" } },
};
const trusted = {
  role: "administrator",
  principal: "2",
  repository: "owner/repo",
  workflow: ".github/workflows/agentic-workqueue.yml",
  run_id: "99",
  run_attempt: 1,
  authenticated: true,
  roles: ["administrator"],
  event: "issues",
  native_run: { repository: { id: 12 } },
};

afterEach(() => {
  vi.unstubAllEnvs();
});

function fixture(initialWork = work) {
  const state = { ...newState(), works: new Map([[initialWork.work_id, initialWork]]) };
  const confirmed = { ...newState(), works: new Map([[initialWork.work_id, initialWork]]) };
  const githubClient = {
    rest: {
      issues: { get: vi.fn(async () => ({ data: { id: 34, number: 7, state: "closed" } })), listComments: vi.fn(async () => ({ data: [] })), createComment: vi.fn(async () => ({})) },
    },
  };
  const publishWorkQueueRequest = vi.fn(async args => {
    const decision = await args.generateOperations(state, args.request, args.actor, Date.now(), "commit", []);
    if (decision.operations.length) {
      confirmed.works.set(initialWork.work_id, { ...initialWork, state: "cancelled", cancellation_reason: "related_issue_closed", cancellation_commit_id: "commit" });
      confirmed.requests.set(args.request.id, { request: args.request, operations: decision.operations });
    }
    return { publishedNow: decision.operations.length > 0 };
  });
  const options = {
    context,
    githubClient,
    issues: true,
    authenticatePublisher: vi.fn(async () => trusted),
    readWorkQueueLog: vi
      .fn()
      .mockResolvedValueOnce({ sha: "abc", state })
      .mockImplementation(async () => ({ sha: "def", state: confirmed })),
    closedIssue: vi.fn(async () => true),
    publishWorkQueueRequest,
    core: { info: vi.fn() },
  };
  registerQueueLedgerClient(githubClient, githubClient);
  return { githubClient, options, state, confirmed, publishWorkQueueRequest };
}

function expiringWorkLog() {
  const log = [genesis()];
  log.push(submission(log, ["expiring"], { transform: node => ({ ...node, subject: resource, expires_at: 50 }) }));
  return log;
}

function publisherFixture(log, at = 100) {
  const fake = fakeGitHub(log);
  const { options, githubClient } = fixture();
  fake.githubClient.rest.issues = githubClient.rest.issues;
  options.githubClient = fake.githubClient;
  options.authenticatePublisher.mockResolvedValue({ ...trusted, principal: administrator.principal });
  options.readWorkQueueLog = vi.fn(readWorkQueueLog);
  options.publishWorkQueueRequest = vi.fn(args => publishWorkQueueRequest({ ...args, now: () => at, sleepFn: async () => {} }));
  return { ...fake, options, readState: () => replayTransactions(fake.log()) };
}

describe("closed backing Issue cancellation", () => {
  it("rejects unbound Issues before configuration and authentication reads", async () => {
    const event = fixture({ ...work, backing_issue: { ...resource, resource_id: "35" } });
    delete event.options.issues;
    event.githubClient.rest.repos = { getContent: vi.fn().mockRejectedValue(new Error("API rate limit exceeded for installation")) };
    expect(await main(event.options)).toEqual({ status: "unbound" });
    expect(event.githubClient.rest.repos.getContent).not.toHaveBeenCalled();
    expect(event.options.authenticatePublisher).not.toHaveBeenCalled();
    expect(event.githubClient.rest.issues.get).not.toHaveBeenCalled();
    expect(event.options.readWorkQueueLog).toHaveBeenCalledOnce();
    expect(event.publishWorkQueueRequest).not.toHaveBeenCalled();
    expect(event.githubClient.rest.issues.createComment).not.toHaveBeenCalled();
  });

  it.each(["work", "tasks", "team work"])("preserves custom tracking/status configuration for bound Issues: %s", async label => {
    const event = fixture();
    event.options.issues = { label };
    event.options.context = { ...context, payload: { ...context.payload, issue: { ...context.payload.issue, labels: [{ name: label }, { name: `${label.replace(/\s+/g, "-")}:needs-attention` }] } } };
    expect((await main(event.options)).status).toBe("cancelled");
    expect(event.options.authenticatePublisher).toHaveBeenCalledOnce();
    expect(event.options.closedIssue).toHaveBeenCalledTimes(2);
    expect(event.githubClient.rest.issues.get).not.toHaveBeenCalled();
  });

  it.each([[], undefined, null, [{ name: null }]].map(labels => ({ labels })))("does not reject a bound event with missing label hints: $labels", async ({ labels }) => {
    const event = fixture();
    event.options.context = { ...context, payload: { ...context.payload, issue: { ...context.payload.issue, labels } } };
    expect((await main(event.options)).status).toBe("cancelled");
    expect(event.options.closedIssue).toHaveBeenCalledTimes(2);
  });

  it("propagates API quota failures for candidate Issues without publishing or retrying", async () => {
    const event = fixture();
    delete event.options.issues;
    event.githubClient.rest.repos = { getContent: vi.fn().mockRejectedValue(Object.assign(new Error("API rate limit exceeded for installation"), { status: 403 })) };
    await expect(main(event.options)).rejects.toThrow("API rate limit exceeded");
    expect(event.githubClient.rest.repos.getContent).toHaveBeenCalledOnce();
    expect(event.options.authenticatePublisher).not.toHaveBeenCalled();
    expect(event.publishWorkQueueRequest).not.toHaveBeenCalled();
  });

  it("rejects ledger read failures before configuration, authentication or mutation", async () => {
    const event = fixture();
    event.options.readWorkQueueLog.mockReset().mockRejectedValue(new Error("ledger_invalid"));
    await expect(main(event.options)).rejects.toThrow("ledger_invalid");
    expect(event.options.authenticatePublisher).not.toHaveBeenCalled();
    expect(event.publishWorkQueueRequest).not.toHaveBeenCalled();
    expect(event.githubClient.rest.issues.createComment).not.toHaveBeenCalled();
  });

  it.each([false, true])("uses one checked GraphQL read for an unrelated closure and no REST reads (initialized=%s)", async initialized => {
    const event = fixture();
    delete event.options.readWorkQueueLog;
    delete event.options.issues;
    const text = JSON.stringify(require("./work_queue_test_helpers.cjs").genesis()) + "\n";
    event.githubClient.graphql = vi.fn(async () => ({
      repository: {
        id: "repo-node",
        nameWithOwner: "owner/repo",
        ref: initialized ? { target: { oid: "b".repeat(40), tree: { oid: "c".repeat(40), entries: [{ name: "work-queue.jsonl", type: "blob", mode: 33188, oid: "d".repeat(40) }] } } } : null,
        log: initialized ? { oid: "d".repeat(40), text, byteSize: Buffer.byteLength(text), isTruncated: false } : null,
        isEmpty: false,
        defaultBranchRef: { target: { oid: "a".repeat(40) } },
        legacy0: null,
        legacy1: null,
      },
    }));
    event.githubClient.rest.repos = { getContent: vi.fn().mockRejectedValue(new Error("API rate limit exceeded for installation")) };
    expect(await main(event.options)).toEqual({ status: initialized ? "unbound" : "queue_missing" });
    expect(event.githubClient.graphql).toHaveBeenCalledOnce();
    expect(event.githubClient.graphql.mock.calls[0][0]).toContain("query CheckedWorkQueue");
    expect(event.githubClient.rest.repos.getContent).not.toHaveBeenCalled();
    expect(event.githubClient.rest.issues.get).not.toHaveBeenCalled();
    expect(event.options.authenticatePublisher).not.toHaveBeenCalled();
    expect(event.publishWorkQueueRequest).not.toHaveBeenCalled();
  });

  it("ignores non-closed issue events and is disabled without Issue mirroring", async () => {
    const event = fixture();
    event.options.context = { ...context, payload: { ...context.payload, action: "labeled" } };
    expect(await main(event.options)).toEqual({ status: "ignored" });
    expect(event.options.authenticatePublisher).not.toHaveBeenCalled();

    const disabled = fixture();
    disabled.options.issues = false;
    expect(await main(disabled.options)).toEqual({ status: "disabled" });
    expect(disabled.options.authenticatePublisher).not.toHaveBeenCalled();

    const comment = fixture();
    comment.options.context = { ...context, eventName: "issue_comment", payload: { ...context.payload, action: "created" } };
    expect(await main(comment.options)).toEqual({ status: "ignored" });
    expect(comment.options.authenticatePublisher).not.toHaveBeenCalled();
  });

  it("uses repository Issue settings from the event revision", async () => {
    const githubClient = {
      rest: {
        repos: {
          getContent: vi.fn(async () => ({
            data: { type: "file", encoding: "base64", size: 49, content: Buffer.from('{"work_queue":{"issues":{"label":"tasks"}}}').toString("base64") },
          })),
        },
      },
    };
    expect(await readIssuesConfig(githubClient, context)).toMatchObject({ label: "tasks" });
    expect(githubClient.rest.repos.getContent).toHaveBeenCalledWith({ ...context.repo, path: ".github/workflows/aw.json", ref: context.sha });
  });
  it("binds exact immutable identity rather than an Issue number or label", () => {
    const state = { works: new Map([["w", { ...work, backing_issue: { ...resource, resource_id: "35" } }]]) };
    expect(matchingWork(state, resource)).toBeUndefined();
    expect(matchingWork({ works: new Map([["w", { ...work, backing_issue: undefined, issue_link: resource }]]) }, resource)).toBeUndefined();
  });

  it("selects an exact Issue subject but never a dependency-only Issue", async () => {
    const subject = fixture({ ...work, backing_issue: undefined, subject: resource });
    expect((await main(subject.options)).status).toBe("cancelled");
    expect(subject.publishWorkQueueRequest.mock.calls[0][0].request.parameters.operations[0].reason).toBe("related_issue_closed");
    const dependency = fixture({ ...work, backing_issue: undefined, subject: undefined, dependencies: [resource] });
    expect((await main(dependency.options)).status).toBe("unbound");
    expect(dependency.publishWorkQueueRequest).not.toHaveBeenCalled();
  });

  it("refuses to pick one of several Work items with the same Issue subject", async () => {
    const ambiguous = fixture({ ...work, backing_issue: undefined, subject: resource });
    ambiguous.state.works.set("work_2", { ...work, work_id: "work_2", backing_issue: undefined, subject: resource });
    await expect(main(ambiguous.options)).rejects.toThrow("multiple Work items");
    expect(ambiguous.publishWorkQueueRequest).not.toHaveBeenCalled();
  });

  it("checks closed state, tracking and status labels against fresh Issue identity", async () => {
    const githubClient = {
      graphql: vi.fn(async () => ({
        i0: {
          issue: {
            databaseId: 34,
            number: 7,
            state: "CLOSED",
            repository: { databaseId: 12, nameWithOwner: "owner/repo" },
            labels: { nodes: [{ name: "work" }, { name: "work:running" }], pageInfo: { hasNextPage: false } },
          },
        },
      })),
    };
    expect(await closedIssue(githubClient, context, resource, { label: "work" })).toBe(true);
    githubClient.graphql.mockResolvedValueOnce({ i0: { issue: { databaseId: 35, number: 7, state: "CLOSED", repository: { databaseId: 12, nameWithOwner: "owner/repo" }, labels: { nodes: [], pageInfo: { hasNextPage: false } } } } });
    await expect(closedIssue(githubClient, context, resource, { label: "work" })).rejects.toThrow();
  });

  it.each([
    { state: "OPEN", labels: ["work", "work:queued"] },
    { state: "CLOSED", labels: ["tasks", "tasks:queued"] },
    { state: "CLOSED", labels: ["work"] },
  ])("does not authorize cancellation from event label hints when the fresh Issue is $state with $labels", async ({ state, labels }) => {
    const event = fixture();
    delete event.options.closedIssue;
    event.githubClient.graphql = vi.fn(async () => ({
      i0: { issue: { databaseId: 34, number: 7, state, repository: { databaseId: 12, nameWithOwner: "owner/repo" }, labels: { nodes: labels.map(name => ({ name })), pageInfo: { hasNextPage: false } } } },
    }));
    expect(await main(event.options)).toEqual({ status: "stale" });
    expect(event.githubClient.rest.issues.get).not.toHaveBeenCalled();
    expect(event.publishWorkQueueRequest).not.toHaveBeenCalled();
    expect(event.githubClient.rest.issues.createComment).not.toHaveBeenCalled();
  });

  it.each([{ databaseId: 35 }, { number: 8 }, { repository: { databaseId: 13, nameWithOwner: "owner/repo" } }, { repository: { databaseId: 12, nameWithOwner: "other/repo" } }])(
    "rejects changed immutable Issue identity using the fresh preflight alone: %j",
    async changed => {
      const event = fixture();
      delete event.options.closedIssue;
      event.githubClient.graphql = vi.fn(async () => ({
        i0: {
          issue: { databaseId: 34, number: 7, state: "CLOSED", repository: { databaseId: 12, nameWithOwner: "owner/repo" }, labels: { nodes: [{ name: "work" }, { name: "work:queued" }], pageInfo: { hasNextPage: false } }, ...changed },
        },
      }));
      await expect(main(event.options)).rejects.toThrow("backing Issue was deleted, transferred, or changed identity");
      expect(event.githubClient.rest.issues.get).not.toHaveBeenCalled();
      expect(event.publishWorkQueueRequest).not.toHaveBeenCalled();
      expect(event.githubClient.rest.issues.createComment).not.toHaveBeenCalled();
    }
  );

  it("commits cancellation through the administrator and confirms before commenting", async () => {
    const { options, githubClient, publishWorkQueueRequest } = fixture();
    expect(await main(options)).toMatchObject({ status: "cancelled", publishedNow: true });
    expect(publishWorkQueueRequest).toHaveBeenCalledOnce();
    const args = publishWorkQueueRequest.mock.calls[0][0];
    expect(args.actor.role).toBe("administrator");
    expect(args.request.kind).toBe("cancel_work");
    expect(args.request.parameters.operations).toEqual([{ kind: "WorkCancellation", work_id: "work_1", reason: "related_issue_closed" }]);
    expect(githubClient.rest.issues.createComment).toHaveBeenCalledOnce();
    expect(githubClient.rest.issues.createComment.mock.calls[0][0].body).toContain("Cancellation was recorded in the work queue ledger. New state: **cancelled**. The work is now canceled.");
    expect(githubClient.rest.issues.createComment.mock.calls[0][0].body).toMatch(/\[View the work queue ledger\]\(https:\/\/github.com\/owner\/repo\/blob\/work-queue\/work-queue.jsonl\)\.\n\n<!-- gh-aw-issue-closure:[^\s]+ -->$/);
  });

  it("cancels enterprise Work bound to a closed backing Issue", async () => {
    vi.stubEnv("GITHUB_SERVER_URL", "https://contoso.example");
    const enterpriseResource = { ...resource, host: "contoso.example" };
    const event = fixture({ ...work, backing_issue: enterpriseResource });
    expect(await main(event.options)).toMatchObject({ status: "cancelled", publishedNow: true });
    expect(event.publishWorkQueueRequest.mock.calls[0][0].request.parameters.operations).toEqual([{ kind: "WorkCancellation", work_id: "work_1", reason: "related_issue_closed" }]);
  });

  it("does not publish for a stale or unbound Issue", async () => {
    const stale = fixture();
    stale.options.closedIssue.mockResolvedValue(false);
    expect((await main(stale.options)).status).toBe("stale");
    expect(stale.publishWorkQueueRequest).not.toHaveBeenCalled();
    const unbound = fixture({ ...work, backing_issue: { ...resource, resource_id: "35" } });
    expect((await main(unbound.options)).status).toBe("unbound");
    expect(unbound.githubClient.rest.issues.createComment).not.toHaveBeenCalled();
  });

  it("does not comment on unresolved races or uncertain writes without a terminal commit", async () => {
    const race = fixture();
    race.options.closedIssue.mockResolvedValueOnce(true).mockResolvedValueOnce(false);
    expect((await main(race.options)).status).toBe("pending");
    expect(race.githubClient.rest.issues.createComment).not.toHaveBeenCalled();
  });

  it("comments on an earlier verified completed or cancelled outcome without claiming causation", async () => {
    const completed = fixture({ ...work, state: "completed", completion_id: "commit" });
    expect((await main(completed.options)).status).toBe("completed");
    expect(completed.publishWorkQueueRequest).not.toHaveBeenCalled();
    expect(completed.githubClient.rest.issues.createComment.mock.calls[0][0].body).toContain("New state: **completed**");
    const old = { ...work, state: "cancelled", cancellation_reason: "other_request", cancellation_commit_id: "commit" };
    const recovered = fixture(old);
    expect((await main(recovered.options)).status).toBe("cancelled");
    expect(recovered.publishWorkQueueRequest).not.toHaveBeenCalled();
    expect(recovered.githubClient.rest.issues.createComment).toHaveBeenCalledOnce();
    expect(recovered.githubClient.rest.issues.createComment.mock.calls[0][0].body).toContain("this Issue closure did not change it");
    const duplicate = fixture(old);
    duplicate.githubClient.rest.issues.listComments.mockResolvedValueOnce({ data: [{ body: recovered.githubClient.rest.issues.createComment.mock.calls[0][0].body }] });
    expect((await main(duplicate.options)).status).toBe("cancelled");
    expect(duplicate.githubClient.rest.issues.createComment).not.toHaveBeenCalled();
  });

  it("never renders an untrusted Work ID in the confirmation", async () => {
    const unsafe = fixture({ ...work, work_id: "`@example`" });
    expect((await main(unsafe.options)).status).toBe("cancelled");
    expect(unsafe.githubClient.rest.issues.createComment.mock.calls[0][0].body).not.toContain("`@example`");
  });

  it("checks an uncertain comment response rather than blindly retrying it", async () => {
    const uncertain = fixture({ ...work, state: "completed", completion_id: "commit" });
    uncertain.githubClient.rest.issues.createComment.mockRejectedValueOnce(new Error("response lost"));
    uncertain.githubClient.rest.issues.listComments
      .mockResolvedValueOnce({ data: [] })
      .mockResolvedValueOnce({ data: [{ body: "<!-- gh-aw-issue-closure:" + require("./work_queue_codec.cjs").digest({ resource, work_id: "work_1" }) + " -->" }] });
    expect((await main(uncertain.options)).status).toBe("completed");
    expect(uncertain.githubClient.rest.issues.createComment).toHaveBeenCalledOnce();
    const unresolved = fixture({ ...work, state: "completed", completion_id: "commit" });
    unresolved.githubClient.rest.issues.createComment.mockRejectedValueOnce(new Error("response lost"));
    await expect(main(unresolved.options)).rejects.toThrow("response lost");
    expect(unresolved.githubClient.rest.issues.createComment).toHaveBeenCalledOnce();
  });

  it("requires exact authenticated repository and event identity", async () => {
    const invalid = fixture();
    invalid.options.authenticatePublisher.mockResolvedValue({ ...trusted, native_run: { repository: { id: 13 } } });
    await expect(main(invalid.options)).rejects.toThrow("repository");
    expect(invalid.publishWorkQueueRequest).not.toHaveBeenCalled();
  });

  it("requires the authenticated primary maintenance workflow", async () => {
    const invalid = fixture();
    invalid.options.authenticatePublisher = vi.fn(async () => ({ ...trusted, workflow: ".github/workflows/agentics-maintenance.yml" }));
    await expect(main(invalid.options)).rejects.toThrow("authenticated Agentic Workqueue workflow");
    expect(invalid.publishWorkQueueRequest).not.toHaveBeenCalled();
  });

  it("scopes request and confirmation identities to Work when an Issue is reused", async () => {
    const first = fixture({ ...work, work_id: "first_work" });
    const second = fixture({ ...work, work_id: "second_work" });
    await main(first.options);
    await main(second.options);
    const firstRequest = first.publishWorkQueueRequest.mock.calls[0][0].request;
    const secondRequest = second.publishWorkQueueRequest.mock.calls[0][0].request;
    const firstComment = first.githubClient.rest.issues.createComment.mock.calls[0][0].body;
    const secondComment = second.githubClient.rest.issues.createComment.mock.calls[0][0].body;
    expect(firstRequest.id).not.toBe(secondRequest.id);
    expect(firstComment.match(/gh-aw-issue-closure:[^ ]+/)[0]).not.toBe(secondComment.match(/gh-aw-issue-closure:[^ ]+/)[0]);
  });
});

describe("closed Issue expiration with the checked publisher", () => {
  it.each([50, 100])("does not publish a conflicting cancellation or confirm wall-clock-only expiration at %s", async at => {
    const event = publisherFixture(expiringWorkLog(), at);
    const before = event.log();
    expect(await main(event.options)).toEqual({ status: "pending" });
    expect(event.options.publishWorkQueueRequest).toHaveBeenCalledOnce();
    expect(event.state.updates).toBe(0);
    expect(event.log()).toEqual(before);
    expect([...event.readState().works.values()][0]).toMatchObject({ state: "available", barrier: "none" });
    expect(event.githubClient.rest.issues.createComment).not.toHaveBeenCalled();
  });

  it("never treats an as-of expiration view as committed terminal proof", async () => {
    const log = expiringWorkLog();
    const event = publisherFixture(log);
    event.options.readWorkQueueLog = vi.fn(async args => {
      const checked = await readWorkQueueLog(args);
      return { ...checked, state: replayTransactions(checked.transactions, 100) };
    });
    expect(await main(event.options)).toEqual({ status: "pending" });
    expect(event.options.publishWorkQueueRequest).not.toHaveBeenCalled();
    expect([...event.readState().works.values()][0].state).toBe("available");
    expect(event.githubClient.rest.issues.createComment).not.toHaveBeenCalled();
  });

  it("confirms committed derived expiration without inventing an explicit cancellation receipt", async () => {
    const log = expiringWorkLog();
    log.push(operationCommit(log, "clock", "control", [{ kind: "Control", control: "grants_paused", value: false, reason: "time_observed" }], administrator, 50));
    const event = publisherFixture(log);
    const expired = [...event.readState().works.values()][0];
    expect(expired).toMatchObject({ state: "cancelled", cancellation_reason: "work_expired" });
    expect(expired.cancellation_commit_id).toBeUndefined();
    expect(await main(event.options)).toEqual({ status: "cancelled", publishedNow: false });
    expect(event.options.publishWorkQueueRequest).not.toHaveBeenCalled();
    expect(event.state.updates).toBe(0);
    expect(event.githubClient.rest.issues.createComment.mock.calls[0][0].body).toContain("already cancelled in the work queue ledger; this Issue closure did not change it");
    expect([...event.readState().works.values()][0].cancellation_commit_id).toBeUndefined();
  });

  it("confirms expiration established by recovered legacy checkpoint receipts after a backdated admission", async () => {
    const history = [genesis()];
    history.push(operationCommit(history, "clock", "control", [{ kind: "Control", control: "grants_paused", value: false, reason: "time_observed" }], administrator, 4000));
    history.push(submission(history, ["legacy"], { at: 2000 }));
    const event = publisherFixture(history, 4000);
    const prior = "c".repeat(40);
    const git = event.githubClient.rest.git;
    const source = event.refs.get("work-queue");
    const getCommit = git.getCommit;
    git.getCommit = args => getCommit({ ...args, commit_sha: args.commit_sha === prior ? source : args.commit_sha });
    event.refs.set("work-queue", prior);
    const checkpoint = legacyCheckpoint(history, prior, administrator, 2000);
    const log = [...checkpoint, submission(checkpoint, ["expiring"], { at: 2000, id: "backdated-admission", transform: node => ({ ...node, subject: resource, expires_at: 3500 }) })];
    const blob = await git.createBlob({ content: serializeTransactionLog(log), encoding: "utf-8" });
    const tree = await git.createTree({ tree: [{ path: "work-queue.jsonl", mode: "100644", type: "blob", sha: blob.data.sha }] });
    const head = await git.createCommit({ tree: tree.data.sha, parents: [prior] });
    event.refs.set("work-queue", head.data.sha);
    const checked = await readWorkQueueLog({ githubClient: event.githubClient, ...context.repo });
    expect(checked.state.time_watermark).toBe(4000);
    expect(checked.state.transactions.every(commit => commit.at < 3500)).toBe(true);
    expect(checked.state.historicalTransactions.some(commit => commit.at === 4000)).toBe(true);
    const expired = matchingWork(checked.state, resource);
    expect(expired).toMatchObject({ state: "cancelled", cancellation_reason: "work_expired" });
    expect(expired.cancellation_commit_id).toBeUndefined();
    expect(await main(event.options)).toEqual({ status: "cancelled", publishedNow: false });
    expect(event.options.publishWorkQueueRequest).not.toHaveBeenCalled();
    expect(event.state.updates).toBe(0);
    expect(event.githubClient.rest.issues.createComment.mock.calls[0][0].body).toContain("already cancelled in the work queue ledger; this Issue closure did not change it");
    expect(event.log()).toEqual(log);
    expect(matchingWork(event.readState(), resource).cancellation_commit_id).toBeUndefined();
  });

  it("recognizes expiration committed while closure publication is refreshing its checked history", async () => {
    const event = publisherFixture(expiringWorkLog());
    const publish = event.options.publishWorkQueueRequest;
    event.options.publishWorkQueueRequest = vi.fn(async args => {
      const current = event.log();
      event.install([...current, operationCommit(current, "clock", "control", [{ kind: "Control", control: "grants_paused", value: false, reason: "time_observed" }], administrator, 50)]);
      return publish(args);
    });
    expect(await main(event.options)).toEqual({ status: "cancelled", publishedNow: false });
    expect(event.state.updates).toBe(0);
    expect(event.githubClient.rest.issues.createComment.mock.calls[0][0].body).toContain("this Issue closure did not change it");
    expect([...event.readState().works.values()][0].cancellation_commit_id).toBeUndefined();
  });

  it("retains explicit closure cancellation before the deadline and recovers the stable request", async () => {
    const event = publisherFixture(expiringWorkLog(), 49);
    event.state.ambiguousOnce = true;
    expect(await main(event.options)).toEqual({ status: "cancelled", publishedNow: false });
    expect(event.state.updates).toBe(1);
    const cancelled = [...event.readState().works.values()][0];
    expect(cancelled.cancellation_reason).toBe("related_issue_closed");
    expect(cancelled.cancellation_commit_id).toBeTruthy();
    expect(event.githubClient.rest.issues.createComment.mock.calls[0][0].body).toContain("Cancellation was recorded");
    expect(event.log()).toHaveLength(3);
  });

  it("still explicitly cancels a running Claim after the deadline without releasing native capacity or changing fairness charges", async () => {
    let log = expiringWorkLog();
    const decision = grant(log);
    log.push(decision.commit);
    log = bind(log, decision.assignments[0].dispatch_id);
    const event = publisherFixture(log);
    const before = event.readState();
    expect(await main(event.options)).toEqual({ status: "cancelled", publishedNow: true });
    const after = event.readState();
    const cancelled = [...after.works.values()][0];
    expect(cancelled).toMatchObject({ cancellation_reason: "related_issue_closed", cancellation_commit_id: after.tip });
    expect([...after.claims.values()][0]).toMatchObject({ state: "cancelled", terminal_commit_id: after.tip });
    expect(reservationCounts(after, "default", "").native).toBe(reservationCounts(before, "default", "").native);
    expect(reservationCounts(after, "default", "").native).toBe(1);
    expect(serializeProjection(after).clocks).toEqual(serializeProjection(before).clocks);
  });

  it.each(["pending", "failed"])("does not turn a retained %s delivery barrier into expiration", async barrier => {
    let log = expiringWorkLog();
    const decision = grant(log);
    log.push(decision.commit);
    const dispatchId = decision.assignments[0].dispatch_id;
    log = bind(log, dispatchId);
    log.push(finish(log, dispatchId, "h1", "completed", { at: 40, keep_open: true }));
    if (barrier === "failed") {
      const state = replayTransactions(log);
      const claim = [...state.claims.values()][0];
      const dispatch = state.dispatches.get(dispatchId);
      log.push(
        operationCommit(
          log,
          "delivery-failed",
          "delivery_failure",
          [
            {
              kind: "DeliveryFailure",
              work_id: claim.work_id,
              claim_id: claim.claim_id,
              completion_id: state.works.get(claim.work_id).completion_id,
              reason: "verification_exhausted",
              disposition: "none",
              evidence: evidence(state, dispatch, "terminal_run", 45, { run_id: dispatch.run.run_id, run_attempt: 1, status: "completed", conclusion: "failure", attempts: 5, effects: "none", receipt: "closed-issue-delivery" }),
            },
          ],
          reconciler,
          45
        )
      );
    }
    const event = publisherFixture(log);
    if (barrier === "pending") {
      await expect(main(event.options)).rejects.toMatchObject({ code: "work_terminal" });
      expect(event.state.updates).toBe(0);
      expect(event.githubClient.rest.issues.createComment).not.toHaveBeenCalled();
      expect([...event.readState().works.values()][0]).toMatchObject({ state: "available", barrier });
    } else {
      expect(await main(event.options)).toEqual({ status: "cancelled", publishedNow: true });
      expect([...event.readState().works.values()][0]).toMatchObject({ cancellation_reason: "related_issue_closed", barrier });
    }
    expect(reservationCounts(event.readState(), "default", "").native).toBe(1);
  });
});
