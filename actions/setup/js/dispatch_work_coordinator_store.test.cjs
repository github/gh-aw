// @ts-check
import { describe, expect, it, vi } from "vitest";
import { applyAndPublishCoordinatorTransactions, COORDINATOR_BRANCH, COORDINATOR_LOG_PATH, readCoordinatorLog } from "./dispatch_work_coordinator_store.cjs";
import { parseTransactionLog, serializeTransactionLog } from "./dispatch_work_coordinator_replay.cjs";

const work = id => ({ kind: "Work", work: id, claim: null, attempt: null });
const claim = (workId, id) => ({ kind: "Claim", work: workId, claim: id, attempt: null });

function createFakeGitHub() {
  const blobs = new Map();
  const trees = new Map();
  const commits = new Map();
  const state = { sha: null, transactions: [], conflictOnce: false, updateCalls: 0 };
  let nextId = 0;

  const makeId = prefix => `${prefix}-${++nextId}`;
  const branchMissing = () => Object.assign(new Error("Not Found"), { status: 404 });
  const staleRef = () => Object.assign(new Error("Update is not a fast forward"), { status: 422 });

  const githubClient = {
    rest: {
      git: {
        getRef: async () => {
          if (!state.sha) throw branchMissing();
          return { data: { object: { sha: state.sha } } };
        },
        getCommit: async ({ commit_sha: sha }) => ({ data: { tree: { sha: `tree-${sha}` } } }),
        getTree: async () => ({
          data: {
            tree: state.transactions.length ? [{ path: COORDINATOR_LOG_PATH, type: "blob", sha: "coordinator-log" }] : [],
          },
        }),
        getBlob: async () => ({
          data: {
            encoding: "base64",
            content: Buffer.from(serializeTransactionLog(state.transactions), "utf8").toString("base64"),
          },
        }),
        createBlob: async ({ content }) => {
          const sha = makeId("blob");
          blobs.set(sha, content);
          return { data: { sha } };
        },
        createTree: async ({ tree }) => {
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
          expect(ref).toBe(`refs/heads/${COORDINATOR_BRANCH}`);
          if (state.sha) throw Object.assign(new Error("Reference already exists"), { status: 422 });
          state.sha = sha;
          state.transactions = parseTransactionLog(blobs.get(trees.get(commits.get(sha).tree)));
          return { data: {} };
        },
        updateRef: async ({ sha, force }) => {
          state.updateCalls++;
          expect(force).toBe(false);
          if (state.conflictOnce) {
            state.conflictOnce = false;
            state.sha = "competing-commit";
            state.transactions = [...state.transactions, work("remote-work")];
            throw staleRef();
          }
          if (!commits.get(sha).parents.includes(state.sha)) throw staleRef();
          state.sha = sha;
          state.transactions = parseTransactionLog(blobs.get(trees.get(commits.get(sha).tree)));
          return { data: {} };
        },
      },
    },
  };

  return { githubClient, state, blobs };
}

describe("dispatch work coordinator Git store", () => {
  it("creates the dedicated branch and writes the canonical transaction log", async () => {
    const fake = createFakeGitHub();
    const result = await applyAndPublishCoordinatorTransactions({
      githubClient: fake.githubClient,
      owner: "owner",
      repo: "repo",
      intents: [work("w"), claim("w", "c")],
    });

    expect(result.persisted).toBe(true);
    expect(fake.state.transactions).toEqual(parseTransactionLog(serializeTransactionLog([work("w"), claim("w", "c")])));
    expect([...fake.blobs.values()][0]).toBe(serializeTransactionLog([work("w"), claim("w", "c")]));
    expect((await readCoordinatorLog({ githubClient: fake.githubClient, owner: "owner", repo: "repo" })).transactions).toEqual(fake.state.transactions);
  });

  it("replays against a competing branch update before publishing", async () => {
    const fake = createFakeGitHub();
    await applyAndPublishCoordinatorTransactions({
      githubClient: fake.githubClient,
      owner: "owner",
      repo: "repo",
      intents: [work("w")],
    });
    fake.state.conflictOnce = true;
    const delays = [];
    const core = { info: vi.fn() };

    const result = await applyAndPublishCoordinatorTransactions({
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

  it("does not publish when every intent is already present", async () => {
    const fake = createFakeGitHub();
    const first = await applyAndPublishCoordinatorTransactions({
      githubClient: fake.githubClient,
      owner: "owner",
      repo: "repo",
      intents: [work("w")],
    });

    const result = await applyAndPublishCoordinatorTransactions({
      githubClient: fake.githubClient,
      owner: "owner",
      repo: "repo",
      intents: [work("w")],
    });

    expect(result.persisted).toBe(false);
    expect(result.sha).toBe(first.sha);
    expect(fake.state.updateCalls).toBe(0);
  });
});
