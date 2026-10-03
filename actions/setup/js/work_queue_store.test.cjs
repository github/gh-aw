// @ts-check
import { describe, expect, it, vi } from "vitest";
import { applyAndPublishWorkQueueTransactions, WORK_QUEUE_BRANCH, WORK_QUEUE_LOG_PATH, readWorkQueueLog } from "./work_queue_store.cjs";
import { claimOldestAvailableWork, createWorkTransaction, parseTransactionLog, replayTransactions, serializeTransactionLog } from "./work_queue_replay.cjs";
import { CURRENT_VERSION } from "./work_queue_codemods.cjs";

const work = id => ({ version: CURRENT_VERSION, kind: "Work", work: id, claim: null, attempt: null });
const claim = (workId, id) => ({ version: CURRENT_VERSION, kind: "Claim", work: workId, claim: id, attempt: null });
const complete = (workId, claimId, attempt) => ({ version: CURRENT_VERSION, kind: "Completion", work: workId, claim: claimId, attempt });

function createFakeGitHub() {
  const blobs = new Map();
  const trees = new Map();
  const commits = new Map();
  const state = { sha: null, branch: WORK_QUEUE_BRANCH, logPath: WORK_QUEUE_LOG_PATH, transactions: [], rawContents: null, conflictOnce: false, updateCalls: 0 };
  let nextId = 0;

  const makeId = prefix => `${prefix}-${++nextId}`;
  const branchMissing = () => Object.assign(new Error("Not Found"), { status: 404 });
  const staleRef = () => Object.assign(new Error("Update is not a fast forward"), { status: 422 });

  const githubClient = {
    rest: {
      git: {
        getRef: async ({ ref }) => {
          if (!state.sha || ref !== `heads/${state.branch}`) throw branchMissing();
          return { data: { object: { sha: state.sha } } };
        },
        getCommit: async ({ commit_sha: sha }) => ({ data: { tree: { sha: `tree-${sha}` } } }),
        getTree: async () => ({
          data: {
            tree: state.transactions.length ? [{ path: state.logPath, type: "blob", sha: "queue-log" }] : [],
          },
        }),
        getBlob: async () => ({
          data: {
            encoding: "base64",
            content: Buffer.from(state.rawContents ?? serializeTransactionLog(state.transactions), "utf8").toString("base64"),
          },
        }),
        createBlob: async ({ content }) => {
          const sha = makeId("blob");
          blobs.set(sha, content);
          return { data: { sha } };
        },
        createTree: async ({ tree }) => {
          expect(tree[0].path).toBe(state.logPath);
          const sha = makeId("tree");
          trees.set(sha, tree[0].sha);
          return { data: { sha } };
        },
        createCommit: async ({ tree, parents }) => {
          const sha = makeId("commit");
          commits.set(sha, { tree, parents });
          return { data: { sha } };
        },
        createRef: async ({ ref, sha }) => {
          expect(ref).toBe(`refs/heads/${WORK_QUEUE_BRANCH}`);
          if (state.sha) throw Object.assign(new Error("Reference already exists"), { status: 422 });
          state.sha = sha;
          state.transactions = parseTransactionLog(blobs.get(trees.get(commits.get(sha).tree)));
          state.rawContents = null;
          return { data: {} };
        },
        updateRef: async ({ ref, sha, force }) => {
          expect(ref).toBe(`heads/${state.branch}`);
          state.updateCalls++;
          expect(force).toBe(false);
          if (state.conflictOnce) {
            state.conflictOnce = false;
            state.sha = "competing-commit";
            state.transactions = [...state.transactions, work("remote-work")];
            state.rawContents = null;
            throw staleRef();
          }
          if (!commits.get(sha).parents.includes(state.sha)) throw staleRef();
          state.sha = sha;
          state.transactions = parseTransactionLog(blobs.get(trees.get(commits.get(sha).tree)));
          state.rawContents = null;
          return { data: {} };
        },
      },
    },
  };

  return { githubClient, state, blobs };
}

describe("work queue Git store", () => {
  it("preserves enqueue age and the selected Work when an older submission arrives during retry", async () => {
    const fake = createFakeGitHub();
    const queued = createWorkTransaction("selected", 100);
    await applyAndPublishWorkQueueTransactions({ githubClient: fake.githubClient, owner: "owner", repo: "repo", intents: [queued] });
    const selected = claimOldestAvailableWork(fake.state.transactions, "c");
    const later = createWorkTransaction("later", 200);
    fake.state.conflictOnce = true;
    const result = await applyAndPublishWorkQueueTransactions({
      githubClient: fake.githubClient,
      owner: "owner",
      repo: "repo",
      intents: [selected, later],
      sleepFn: async () => {},
    });
    expect(result.rejected).toEqual([]);
    expect(result.transactions).toContainEqual(queued);
    expect(result.transactions).toContainEqual(later);
    expect(result.transactions).toContainEqual(selected);
    expect(replayTransactions(result.transactions).available).toEqual(["remote-work", "later"]);
    expect(fake.state.updateCalls).toBe(2);
  });

  it("creates the dedicated branch and writes the canonical transaction log", async () => {
    const fake = createFakeGitHub();
    const result = await applyAndPublishWorkQueueTransactions({
      githubClient: fake.githubClient,
      owner: "owner",
      repo: "repo",
      intents: [work("w"), claim("w", "c")],
    });

    expect(result.persisted).toBe(true);
    expect(fake.state.transactions).toEqual(parseTransactionLog(serializeTransactionLog([work("w"), claim("w", "c")])));
    expect([...fake.blobs.values()][0]).toBe(serializeTransactionLog([work("w"), claim("w", "c")]));
    expect((await readWorkQueueLog({ githubClient: fake.githubClient, owner: "owner", repo: "repo" })).transactions).toEqual(fake.state.transactions);
  });

  it("replays against a competing branch update before publishing", async () => {
    const fake = createFakeGitHub();
    await applyAndPublishWorkQueueTransactions({
      githubClient: fake.githubClient,
      owner: "owner",
      repo: "repo",
      intents: [work("w")],
    });
    fake.state.conflictOnce = true;
    const delays = [];
    const core = { info: vi.fn() };

    const result = await applyAndPublishWorkQueueTransactions({
      githubClient: fake.githubClient,
      owner: "owner",
      repo: "repo",
      intents: [claim("w", "c")],
      sleepFn: async delay => delays.push(delay),
      core,
    });

    expect(result.persisted).toBe(true);
    expect(result.rejected).toEqual([]);
    expect(fake.state.transactions).toEqual(parseTransactionLog(serializeTransactionLog([work("w"), work("remote-work"), claim("w", "c")])));
    expect(fake.state.updateCalls).toBe(2);
    expect(core.info).toHaveBeenCalledWith(expect.stringContaining("queue ref conflict"));
    expect(core.info).toHaveBeenCalledWith(expect.stringContaining("queue published"));
    expect(core.info.mock.calls.flat().join("\n")).not.toContain("remote-work");
    expect(delays).toEqual([50]);
  });

  it("reports all-no-op intents without writing, re-aging Work, or leaking identifiers", async () => {
    const fake = createFakeGitHub();
    const transactions = [createWorkTransaction("submitted-work", 100), claim("submitted-work", "submitted-claim"), complete("submitted-work", "submitted-claim", "submitted-attempt"), work("historical-work")];
    const first = await applyAndPublishWorkQueueTransactions({
      githubClient: fake.githubClient,
      owner: "owner",
      repo: "repo",
      intents: transactions,
    });
    const core = { info: vi.fn() };
    const writeSpies = ["createBlob", "createTree", "createCommit", "createRef", "updateRef"].map(method => vi.spyOn(fake.githubClient.rest.git, method));

    const result = await applyAndPublishWorkQueueTransactions({
      githubClient: fake.githubClient,
      owner: "owner",
      repo: "repo",
      intents: [...transactions, createWorkTransaction("submitted-work", 200), createWorkTransaction("historical-work", 300)],
      core,
    });

    expect(result.persisted).toBe(false);
    expect(result.sha).toBe(first.sha);
    expect(result.transactions).toEqual(first.transactions);
    expect(result.rejected).toEqual([]);
    expect(result.idempotent).toBe(6);
    expect(fake.state.updateCalls).toBe(0);
    for (const spy of writeSpies) expect(spy).not.toHaveBeenCalled();
    expect(core.info).toHaveBeenCalledWith("Work queue: 0 new intents, 0 rejected intents, 6 idempotent intents");
    expect(core.info).toHaveBeenCalledWith("Work queue: queue unchanged; publication skipped");
    const output = core.info.mock.calls.flat().join("\n");
    for (const id of ["submitted-work", "submitted-claim", "submitted-attempt", "historical-work"]) expect(output).not.toContain(id);
  });

  it("reports mixed intent outcomes independently of duplicate physical source records", async () => {
    const fake = createFakeGitHub();
    fake.state.sha = "head";
    fake.state.transactions = [work("existing"), work("existing")];
    fake.state.rawContents = `${fake.state.transactions.map(transaction => JSON.stringify(transaction)).join("\n")}\n`;
    const submitted = createWorkTransaction("new", 100);
    const core = { info: vi.fn() };

    const result = await applyAndPublishWorkQueueTransactions({
      githubClient: fake.githubClient,
      owner: "owner",
      repo: "repo",
      intents: [createWorkTransaction("existing", 200), submitted, submitted, claim("missing", "c")],
      core,
    });

    expect(result.persisted).toBe(true);
    expect(result.transactions).toEqual(parseTransactionLog(serializeTransactionLog([work("existing"), submitted])));
    expect(result.rejected).toEqual([{ transaction: claim("missing", "c"), reason: "work does not exist" }]);
    expect(result.idempotent).toBe(2);
    expect(core.info).toHaveBeenCalledWith("Work queue: 1 new intents, 1 rejected intents, 2 idempotent intents");
    expect(core.info.mock.calls.flat().join("\n")).not.toContain("existing");
  });

  it("reports zero new intents when only compacting duplicate physical records", async () => {
    const fake = createFakeGitHub();
    fake.state.sha = "head";
    fake.state.transactions = [work("w"), work("w")];
    fake.state.rawContents = `${fake.state.transactions.map(transaction => JSON.stringify(transaction)).join("\n")}\n`;
    const core = { info: vi.fn() };

    const result = await applyAndPublishWorkQueueTransactions({ githubClient: fake.githubClient, owner: "owner", repo: "repo", intents: [work("w")], core });

    expect(result.persisted).toBe(true);
    expect(result.transactions).toEqual([work("w")]);
    expect(result.idempotent).toBe(1);
    expect(core.info).toHaveBeenCalledWith("Work queue: 0 new intents, 0 rejected intents, 1 idempotent intents");
  });

  it("recounts each attempt after a conflict without accumulating outcomes or re-aging Work", async () => {
    const fake = createFakeGitHub();
    await applyAndPublishWorkQueueTransactions({ githubClient: fake.githubClient, owner: "owner", repo: "repo", intents: [work("w")] });
    fake.state.conflictOnce = true;
    const core = { info: vi.fn() };

    const result = await applyAndPublishWorkQueueTransactions({
      githubClient: fake.githubClient,
      owner: "owner",
      repo: "repo",
      intents: [createWorkTransaction("remote-work", 100), claim("w", "c"), claim("missing", "unknown"), work("w")],
      sleepFn: async () => {},
      core,
    });

    expect(result.persisted).toBe(true);
    expect(result.transactions).toContainEqual(work("remote-work"));
    expect(result.transactions.filter(transaction => transaction.work === "remote-work")).toHaveLength(1);
    expect(result.rejected).toEqual([{ transaction: claim("missing", "unknown"), reason: "work does not exist" }]);
    expect(result.idempotent).toBe(2);
    expect(fake.state.updateCalls).toBe(2);
    expect(core.info.mock.calls.flat().filter(message => message.includes("new intents"))).toEqual([
      "Work queue: 2 new intents, 1 rejected intents, 1 idempotent intents",
      "Work queue: 1 new intents, 1 rejected intents, 2 idempotent intents",
    ]);
  });

  it("skips publication when a conflict makes every intent idempotent", async () => {
    const fake = createFakeGitHub();
    await applyAndPublishWorkQueueTransactions({ githubClient: fake.githubClient, owner: "owner", repo: "repo", intents: [work("w")] });
    fake.state.conflictOnce = true;
    const createBlob = vi.spyOn(fake.githubClient.rest.git, "createBlob");
    const core = { info: vi.fn() };

    const result = await applyAndPublishWorkQueueTransactions({
      githubClient: fake.githubClient,
      owner: "owner",
      repo: "repo",
      intents: [createWorkTransaction("remote-work", 100)],
      sleepFn: async () => {},
      core,
    });

    expect(result.persisted).toBe(false);
    expect(result.sha).toBe("competing-commit");
    expect(result.transactions).toContainEqual(work("remote-work"));
    expect(result.rejected).toEqual([]);
    expect(result.idempotent).toBe(1);
    expect(createBlob).toHaveBeenCalledOnce();
    expect(fake.state.updateCalls).toBe(1);
    expect(core.info.mock.calls.flat().filter(message => message.includes("new intents"))).toEqual([
      "Work queue: 1 new intents, 0 rejected intents, 0 idempotent intents",
      "Work queue: 0 new intents, 0 rejected intents, 1 idempotent intents",
    ]);
    expect(core.info).toHaveBeenCalledWith("Work queue: queue unchanged; publication skipped");
  });

  it("upgrades and compacts legacy messages through the checked ref update on read", async () => {
    const fake = createFakeGitHub();
    fake.state.sha = "legacy-head";
    const legacy = { kind: "Work", work: "w", claim: null, attempt: null };
    fake.state.rawContents = `${JSON.stringify(legacy)}\n${JSON.stringify(legacy)}\n`;
    fake.state.transactions = [work("w")];

    const result = await readWorkQueueLog({ githubClient: fake.githubClient, owner: "owner", repo: "repo" });
    expect(result.sha).not.toBe("legacy-head");
    expect(result.transactions).toEqual([work("w")]);
    expect(fake.state.updateCalls).toBe(1);
    expect([...fake.blobs.values()]).toEqual([serializeTransactionLog([work("w")])]);
  });

  it("allows read-only activation to upgrade in memory without publishing", async () => {
    const fake = createFakeGitHub();
    fake.state.sha = "legacy-head";
    fake.state.transactions = [work("w")];
    fake.state.rawContents = `${JSON.stringify({ kind: "Work", work: "w", claim: null, attempt: null })}\n`;

    const result = await readWorkQueueLog({ githubClient: fake.githubClient, owner: "owner", repo: "repo", publishUpgrades: false });
    expect(result.transactions).toEqual([work("w")]);
    expect(result.sha).toBe("legacy-head");
    expect(fake.state.updateCalls).toBe(0);
    expect(fake.blobs.size).toBe(0);
  });

  it("upgrades a v1 log through checked publication while preserving historical Work age", async () => {
    const fake = createFakeGitHub();
    fake.state.sha = "v1-head";
    fake.state.transactions = [work("old")];
    fake.state.rawContents = `${JSON.stringify({ ...work("old"), version: 1 })}\n`;

    const readOnly = await readWorkQueueLog({ githubClient: fake.githubClient, owner: "owner", repo: "repo", publishUpgrades: false });
    expect(readOnly.sha).toBe("v1-head");
    expect(readOnly.transactions).toEqual([work("old")]);
    expect(fake.blobs.size).toBe(0);

    const result = await applyAndPublishWorkQueueTransactions({
      githubClient: fake.githubClient,
      owner: "owner",
      repo: "repo",
      intents: [createWorkTransaction("old", 200), createWorkTransaction("new", 100)],
    });
    expect(result.persisted).toBe(true);
    expect(fake.state.updateCalls).toBe(1);
    expect(result.transactions).toEqual([createWorkTransaction("new", 100), work("old")]);
    expect(replayTransactions(result.transactions).available).toEqual(["old", "new"]);
    expect([...fake.blobs.values()]).toEqual([serializeTransactionLog(result.transactions)]);
    expect(await readWorkQueueLog({ githubClient: fake.githubClient, owner: "owner", repo: "repo" })).toMatchObject({ transactions: result.transactions });
    expect(fake.state.updateCalls).toBe(1);
  });

  it("retries a concurrent upgrade against the new head", async () => {
    const fake = createFakeGitHub();
    fake.state.sha = "legacy-head";
    fake.state.transactions = [work("w")];
    fake.state.rawContents = `${JSON.stringify({ kind: "Work", work: "w", claim: null, attempt: null })}\n`;
    fake.state.conflictOnce = true;

    const result = await readWorkQueueLog({ githubClient: fake.githubClient, owner: "owner", repo: "repo" });
    expect(result.transactions).toEqual(parseTransactionLog(serializeTransactionLog([work("w"), work("remote-work")])));
    expect(fake.state.updateCalls).toBe(1);
    expect(fake.state.sha).toBe("competing-commit");
  });

  it("never publishes an invalid or unsupported mixed-version log", async () => {
    for (const invalid of [
      { ...claim("w", "c"), version: 99 },
      { kind: "Work", work: "invalid", claim: null, attempt: null, enqueued: 1 },
      { version: 0, kind: "Work", work: "invalid", claim: null, attempt: null, enqueued: 1 },
      { version: 1, kind: "Work", work: "invalid", claim: null, attempt: null, enqueued: 1 },
    ]) {
      const fake = createFakeGitHub();
      fake.state.sha = "invalid-head";
      fake.state.transactions = [work("w")];
      fake.state.rawContents = `${JSON.stringify({ kind: "Work", work: "w", claim: null, attempt: null })}\n${JSON.stringify(invalid)}\n`;
      await expect(readWorkQueueLog({ githubClient: fake.githubClient, owner: "owner", repo: "repo" })).rejects.toThrow("Failed to read work queue log");
      expect(fake.state.updateCalls).toBe(0);
      expect(fake.blobs.size).toBe(0);
    }
  });

  it("retains live legacy storage for read-only snapshots and checked writes", async () => {
    const fake = createFakeGitHub();
    fake.state.branch = "dispatch-coordinator";
    fake.state.logPath = "dispatch-work-coordinator.jsonl";
    fake.state.sha = "legacy-head";
    fake.state.transactions = [work("w"), claim("w", "c")];
    const core = { info: vi.fn() };
    const before = await readWorkQueueLog({ githubClient: fake.githubClient, owner: "owner", repo: "repo", publishUpgrades: false, core });
    expect(before.transactions).toEqual(parseTransactionLog(serializeTransactionLog(fake.state.transactions)));
    expect(fake.blobs.size).toBe(0);
    expect(core.info).toHaveBeenCalledWith(expect.stringContaining("retaining legacy storage"));

    fake.state.conflictOnce = true;
    const result = await applyAndPublishWorkQueueTransactions({
      githubClient: fake.githubClient,
      owner: "owner",
      repo: "repo",
      intents: [{ version: CURRENT_VERSION, kind: "Completion", work: "w", claim: "c", attempt: "a" }],
      sleepFn: async () => {},
    });
    expect(result.persisted).toBe(true);
    expect(fake.state.transactions).toContainEqual(work("remote-work"));
    expect(fake.state.transactions).toContainEqual({ version: CURRENT_VERSION, kind: "Completion", work: "w", claim: "c", attempt: "a" });
    expect(fake.state.updateCalls).toBe(2);
    expect(fake.state.branch).toBe("dispatch-coordinator");
  });

  it("fails closed if both branch names exist instead of choosing a queue", async () => {
    const fake = createFakeGitHub();
    fake.githubClient.rest.git.getRef = async () => ({ data: { object: { sha: "ambiguous-head" } } });
    await expect(applyAndPublishWorkQueueTransactions({ githubClient: fake.githubClient, owner: "owner", repo: "repo", intents: [work("w")] })).rejects.toThrow("Both current and legacy");
    expect(fake.blobs.size).toBe(0);
  });

  it("fails closed if both transaction log names exist", async () => {
    const fake = createFakeGitHub();
    fake.state.sha = "head";
    fake.githubClient.rest.git.getTree = async () => ({
      data: { tree: [WORK_QUEUE_LOG_PATH, "dispatch-work-coordinator.jsonl"].map(path => ({ path, type: "blob", sha: "blob" })) },
    });
    await expect(readWorkQueueLog({ githubClient: fake.githubClient, owner: "owner", repo: "repo" })).rejects.toThrow("Failed to read work queue log");
    expect(fake.blobs.size).toBe(0);
  });

  it("does not interpret a failed legacy lookup as an absent queue", async () => {
    const fake = createFakeGitHub();
    fake.githubClient.rest.git.getRef = async ({ ref }) => {
      throw Object.assign(new Error("lookup failed"), { status: ref === "heads/work-queue" ? 404 : 403 });
    };
    await expect(readWorkQueueLog({ githubClient: fake.githubClient, owner: "owner", repo: "repo" })).rejects.toThrow("Failed to read work queue branch");
    expect(fake.blobs.size).toBe(0);
  });
});
