// @ts-check
import { describe, expect, it, vi } from "vitest";
import { applyAndPublishCoordinatorTransactions, COORDINATOR_BRANCH, COORDINATOR_LOG_PATH, readCoordinatorLog } from "./dispatch_work_coordinator_store.cjs";
import { parseTransactionLog, serializeTransactionLog } from "./dispatch_work_coordinator_replay.cjs";

const work = id => ({ kind: "Work", sequence: 1, version: 3, work: { legacy_work_id: id }, work_id: id });
const claim = (workId, id) => ({ claim_id: id, kind: "Claim", run_id: `legacy:${id}`, version: 3, work_id: workId });

function createFakeGitHub() {
  const blobs = new Map();
  const trees = new Map();
  const commits = new Map();
  const state = { sha: null, legacySha: null, transactions: [], rawContents: null, conflictOnce: false, updateCalls: 0 };
  let nextId = 0;

  const makeId = prefix => `${prefix}-${++nextId}`;
  const branchMissing = () => Object.assign(new Error("Not Found"), { status: 404 });
  const staleRef = () => Object.assign(new Error("Update is not a fast forward"), { status: 422 });

  const githubClient = {
    rest: {
      repos: { get: async () => ({ data: {} }) },
      git: {
        getRef: async ({ ref }) => {
          if (ref === "heads/dispatch-coordinator" && state.legacySha) return { data: { object: { sha: state.legacySha } } };
          if (!state.sha) throw branchMissing();
          return { data: { object: { sha: state.sha } } };
        },
        getCommit: async ({ commit_sha: sha }) => ({ data: { tree: { sha: `tree-${sha}` } } }),
        getTree: async () => ({
          data: {
            tree: state.transactions.length ? [{ path: COORDINATOR_LOG_PATH, mode: "100644", type: "blob", sha: "coordinator-log" }] : [],
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
          state.rawContents = null;
          return { data: {} };
        },
        updateRef: async ({ sha, force }) => {
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

  return { githubClient, state, blobs, commits };
}

describe("dispatch work coordinator Git store", () => {
  it("upgrades version-2 facts only through trusted publication", async () => {
    const fake = createFakeGitHub();
    fake.state.sha = "version-two-head";
    fake.state.transactions = [work("w")];
    fake.state.rawContents = `${JSON.stringify({ ...work("w"), version: 2 })}\n`;
    const readonly = await readCoordinatorLog({ githubClient: fake.githubClient, owner: "owner", repo: "repo", publishUpgrades: false });
    expect(readonly.transactions).toEqual([work("w")]);
    expect(readonly.sha).toBe("version-two-head");
    expect(fake.state.updateCalls).toBe(0);
    const upgraded = await readCoordinatorLog({ githubClient: fake.githubClient, owner: "owner", repo: "repo" });
    expect(upgraded.transactions).toEqual([work("w")]);
    expect(fake.state.updateCalls).toBe(1);
    expect([...fake.blobs.values()]).toEqual([serializeTransactionLog([work("w")])]);
  });
  it("does not treat an unavailable repository as an empty queue", async () => {
    const fake = createFakeGitHub();
    fake.githubClient.rest.repos.get = async () => {
      throw Object.assign(new Error("Not Found"), { status: 404 });
    };
    await expect(readCoordinatorLog({ githubClient: fake.githubClient, owner: "owner", repo: "repo" })).rejects.toThrow("repository");
    expect(fake.blobs.size).toBe(0);
  });

  it("rejects truncated trees and non-regular logs before publication", async () => {
    for (const data of [{ truncated: true, tree: [] }, { tree: [{ path: COORDINATOR_LOG_PATH, mode: "120000", type: "blob", sha: "coordinator-log" }] }]) {
      const fake = createFakeGitHub();
      fake.state.sha = "head";
      fake.githubClient.rest.git.getTree = async () => ({ data });
      await expect(readCoordinatorLog({ githubClient: fake.githubClient, owner: "owner", repo: "repo" })).rejects.toThrow("Failed to read dispatch coordinator log");
      expect(fake.blobs.size).toBe(0);
    }
  });
  it("migrates the historical runtime branch without changing its ref", async () => {
    const fake = createFakeGitHub();
    fake.state.legacySha = "legacy-head";
    fake.state.transactions = [work("w")];
    fake.state.rawContents = '{"version":1,"kind":"Work","work":"w","claim":null,"attempt":null}\n';
    const readonly = await readCoordinatorLog({ githubClient: fake.githubClient, owner: "owner", repo: "repo", publishUpgrades: false });
    expect(readonly.sha).toBe("legacy-head");
    expect(fake.state.sha).toBeNull();
    const migrated = await readCoordinatorLog({ githubClient: fake.githubClient, owner: "owner", repo: "repo" });
    expect(fake.state.sha).toBe(migrated.sha);
    expect(fake.commits.get(migrated.sha).parents).toEqual(["legacy-head"]);
    expect(fake.state.legacySha).toBe("legacy-head");
  });
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

  it("upgrades and compacts legacy messages through the checked ref update on read", async () => {
    const fake = createFakeGitHub();
    fake.state.sha = "legacy-head";
    const legacy = { kind: "Work", work: "w", claim: null, attempt: null };
    fake.state.rawContents = `${JSON.stringify(legacy)}\n${JSON.stringify(legacy)}\n`;
    fake.state.transactions = [work("w")];

    const result = await readCoordinatorLog({ githubClient: fake.githubClient, owner: "owner", repo: "repo" });
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

    const result = await readCoordinatorLog({ githubClient: fake.githubClient, owner: "owner", repo: "repo", publishUpgrades: false });
    expect(result.transactions).toEqual([work("w")]);
    expect(result.sha).toBe("legacy-head");
    expect(fake.state.updateCalls).toBe(0);
    expect(fake.blobs.size).toBe(0);
  });

  it("retries a concurrent upgrade against the new head", async () => {
    const fake = createFakeGitHub();
    fake.state.sha = "legacy-head";
    fake.state.transactions = [work("w")];
    fake.state.rawContents = `${JSON.stringify({ kind: "Work", work: "w", claim: null, attempt: null })}\n`;
    fake.state.conflictOnce = true;

    const result = await readCoordinatorLog({ githubClient: fake.githubClient, owner: "owner", repo: "repo" });
    expect(result.transactions).toEqual(parseTransactionLog(serializeTransactionLog([work("w"), work("remote-work")])));
    expect(fake.state.updateCalls).toBe(1);
    expect(fake.state.sha).toBe("competing-commit");
  });

  it("never publishes an invalid or unsupported mixed-version log", async () => {
    const fake = createFakeGitHub();
    fake.state.sha = "invalid-head";
    fake.state.transactions = [work("w")];
    fake.state.rawContents = `${JSON.stringify({ kind: "Work", work: "w", claim: null, attempt: null })}\n${JSON.stringify({ ...claim("w", "c"), version: 99 })}\n`;
    await expect(readCoordinatorLog({ githubClient: fake.githubClient, owner: "owner", repo: "repo" })).rejects.toThrow("Failed to read dispatch coordinator log");
    expect(fake.state.updateCalls).toBe(0);
    expect(fake.blobs.size).toBe(0);
  });
});
