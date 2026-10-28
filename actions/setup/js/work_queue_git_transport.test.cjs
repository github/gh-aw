import { afterEach, describe, expect, it } from "vitest";
import { execFileSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { createGitLedgerClient, gitEnvironment, queueLedgerClient, registerQueueLedgerClient } = require("./work_queue_git_transport.cjs");
const { readWorkQueueLog, publishWorkQueueRequest } = require("./work_queue_store.cjs");
const { readCheckedQueue, writeCheckedFiles } = require("./work_queue_checked_transport.cjs");
const { compactTransactions, newRequest, serializeTransactionLog } = require("./work_queue_replay.cjs");
const { queueFixture } = require("./work_queue_lifecycle.test_helpers.cjs");
const { context } = require("./work_queue_test_helpers.cjs");
const { canonical } = require("./work_queue_codec.cjs");

const cleanups = [];
afterEach(() => {
  for (const cleanup of cleanups.splice(0).reverse()) cleanup();
});

function fixture(options = {}) {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "queue-git-test-"));
  cleanups.push(() => fs.rmSync(directory, { recursive: true, force: true }));
  const env = gitEnvironment();
  function git(args, input) {
    return execFileSync("git", ["--git-dir", directory, ...args], { env, input, stdio: ["pipe", "pipe", "pipe"] })
      .toString("utf8")
      .trim();
  }
  git(["init", "--bare", "--quiet", "--initial-branch=main", "--template="]);
  const queue = queueFixture({ count: 2, batch: 1, ...options });
  let head;
  let serial = 0;
  function store(transactions, { mode = "100644", parents = head ? [head] : [] } = {}) {
    const blob = git(["hash-object", "-w", "--stdin"], transactions.map(commit => canonical(commit)).join("\n") + "\n");
    const tree = git(["mktree"], `${mode} blob ${blob}\twork-queue.jsonl\n`);
    head = git(["commit-tree", tree, ...parents.flatMap(parent => ["-p", parent]), "-F", "-"], `Queue fixture ${++serial}`);
    git(["update-ref", "refs/heads/work-queue", head]);
    return head;
  }
  store(queue.transactions);
  const calls = [];
  const networkCalls = [];
  const forbidden = () => {
    calls.push("API");
    throw new Error("Ledger verification must not call a hosted API");
  };
  const githubClient = {
    auth: async () => {
      networkCalls.push("Git");
      return { type: "token", token: "queue-test-token" };
    },
    graphql: forbidden,
    rest: new Proxy({}, { get: forbidden }),
  };
  const ledger = createGitLedgerClient({ githubClient, owner: "owner", repo: "repo", branch: "work-queue", remote: directory });
  registerQueueLedgerClient(githubClient, ledger);
  cleanups.push(() => ledger.close());
  return { queue, githubClient, ledger, calls, networkCalls, store, git, options: { githubClient, owner: "owner", repo: "repo" }, head: () => head };
}

describe("Git-only work queue ledger", { timeout: 30000 }, () => {
  it("links Work creation and Claim completion to their original Git revisions, even after compaction", async () => {
    const f = fixture({ bound: true });
    const created = f.head();
    const member = f.queue.assignment.claims[0];
    f.queue.append("finish", { dispatch_id: f.queue.assignment.dispatch_id, claim_handle: member.handle, outcome: "completed" }, { ...f.queue.workerActor, dispatch_id: f.queue.assignment.dispatch_id, claim_handle: member.handle });
    const finished = f.store(f.queue.transactions);
    f.store(compactTransactions(f.queue.transactions, finished, f.queue.administrator, f.queue.at));
    const head = f.head();
    const links = await f.ledger.workQueueClaimLinks({ head, work_id: member.work_id, claim_id: member.claim_id });
    const workLine = f.queue.transactions.findIndex(transaction => transaction.operations.some(operation => operation.kind === "Work" && operation.work_id === member.work_id)) + 1;
    expect(links).toEqual({
      work: `https://github.com/owner/repo/blob/${created}/work-queue.jsonl#L${workLine}`,
      claim: `https://github.com/owner/repo/blob/${finished}/work-queue.jsonl#L${f.queue.transactions.length}`,
    });
    expect(await f.ledger.workQueueClaimLinks({ head: created, work_id: member.work_id, claim_id: member.claim_id })).toEqual({ work: links.work, claim: undefined });
    const networkCalls = f.networkCalls.length;
    expect(await f.ledger.workQueueClaimLinks({ head, work_id: member.work_id, claim_id: member.claim_id })).toBe(links);
    expect(f.networkCalls).toHaveLength(networkCalls);
    expect(f.calls).toEqual([]);
  });

  it("looks up unchanged history locally but fetches a changed remote head", async () => {
    const f = fixture();
    const current = await readWorkQueueLog(f.options);
    expect(f.networkCalls).toHaveLength(2);
    await f.ledger.rest.repos.get();
    const files = await f.ledger.readFiles(current.sha, ["work-queue.jsonl"]);
    expect(files.get("work-queue.jsonl")).toBe(serializeTransactionLog(f.queue.transactions));
    expect(f.networkCalls).toHaveLength(3);

    f.store(f.queue.transactions);
    const refreshed = await readWorkQueueLog(f.options);
    expect(refreshed.sha).toBe(f.head());
    expect(refreshed.sha).not.toBe(current.sha);
    expect(f.networkCalls).toHaveLength(5);
    expect(f.calls).toEqual([]);
  });

  it("verifies nested checkpoints and detached repeated reads without REST or GraphQL", async () => {
    const f = fixture();
    const first = compactTransactions(f.queue.transactions, f.head(), f.queue.administrator, f.queue.at);
    const parent = f.store(first);
    const second = compactTransactions(first, parent, f.queue.administrator, f.queue.at + 1);
    const head = f.store(second);
    for (let iteration = 0; iteration < 3; iteration++) {
      const current = await readWorkQueueLog(f.options);
      expect(current.sha).toBe(head);
      expect(current.state.works.size).toBe(2);
      expect(current.state.claims.size).toBe(2);
      expect(serializeTransactionLog(current.transactions)).toBe(serializeTransactionLog(second));
      current.state.works.clear();
    }
    expect(f.calls).toEqual([]);
  });

  it("publishes and recovers a stable request after compaction using Git CAS only", async () => {
    const f = fixture();
    const checkpoint = compactTransactions(f.queue.transactions, f.head(), f.queue.administrator, f.queue.at);
    f.store(checkpoint);
    const request = newRequest("pause-using-git", "control", f.queue.administrator, {
      operations: [{ kind: "Control", control: "admission_paused", value: true, reason: "operator" }],
    });
    const input = { ...f.options, request, context: context(f.queue.administrator), now: () => f.queue.at + 1 };
    const published = await publishWorkQueueRequest(input);
    expect(published.publishedNow).toBe(true);
    expect(f.git(["rev-parse", "refs/heads/work-queue"])).toBe(published.sha);
    const recovered = await publishWorkQueueRequest(input);
    expect(recovered.reused).toBe(true);
    expect(f.git(["rev-parse", "refs/heads/work-queue"])).toBe(published.sha);
    expect((await readWorkQueueLog(f.options)).state.requests.has(request.id)).toBe(true);
    expect(f.calls).toEqual([]);
  });

  it("reads and publishes projection journals at the checked Git head without ledger APIs", async () => {
    const f = fixture();
    const initial = await readCheckedQueue(f.options);
    const journal = `.gh-aw/issue-projection/${"a".repeat(64)}.json`;
    const next = await writeCheckedFiles({ ...f.options, branch: "work-queue", expectedHeadOid: initial.sha, files: [{ path: journal, content: '{"version":1}' }] });
    const current = await readCheckedQueue({ ...f.options, paths: [journal] });
    expect(current.sha).toBe(next);
    expect(current.journal.get(journal)).toEqual({ version: 1 });
    expect(current.transactions).toEqual(initial.transactions);
    await expect(writeCheckedFiles({ ...f.options, branch: "work-queue", expectedHeadOid: initial.sha, files: [{ path: journal, content: '{"version":2}' }] })).rejects.toMatchObject({ status: 409 });
    expect(f.calls).toEqual([]);
  });

  it.each(["wrong-parent", "merge-parent", "nonregular-log", "damaged-state"])("rejects %s without trusting a checkpoint or issuing an API call", async defect => {
    const f = fixture();
    const original = f.head();
    const checkpoint = compactTransactions(f.queue.transactions, original, f.queue.administrator, f.queue.at);
    if (defect === "wrong-parent") {
      const foreign = f.store(f.queue.transactions, { parents: [] });
      f.store(checkpoint, { parents: [foreign] });
    } else if (defect === "merge-parent") {
      const foreign = f.store(f.queue.transactions, { parents: [] });
      f.store(checkpoint, { parents: [original, foreign] });
    } else if (defect === "nonregular-log") {
      f.store(checkpoint, { mode: "100755" });
    } else {
      checkpoint[0].operations[0].state.stats.nodes++;
      f.store(checkpoint);
    }
    await expect(readWorkQueueLog(f.options)).rejects.toThrow(/checkpoint_invalid|ledger_invalid/);
    expect(f.calls).toEqual([]);
  });

  it("distinguishes an authenticated absent branch from an unreadable repository", async () => {
    const f = fixture();
    f.git(["update-ref", "refs/heads/main", f.head()]);
    f.git(["update-ref", "-d", "refs/heads/work-queue"]);
    expect((await readWorkQueueLog(f.options)).sha).toBeNull();
    f.githubClient.auth = async () => {
      throw new Error("credential revoked");
    };
    await expect(readWorkQueueLog(f.options)).rejects.toThrow(/repository_unavailable/);
    expect(f.calls).toEqual([]);
  });

  it("preserves BOM-prefixed Git tree entry names", async () => {
    const f = fixture();
    const blob = f.git(["hash-object", "-w", "--stdin"], serializeTransactionLog(f.queue.transactions));
    const queueTree = f.git(["mktree", "-z"], [`100644 blob ${blob}\t\uFEFFnotes.txt\0`, `100644 blob ${blob}\t\uFEFFwork-queue.jsonl\0`].join(""));
    const head = f.git(["commit-tree", queueTree, "-p", f.head(), "-F", "-"], "BOM queue filename");
    f.git(["update-ref", "refs/heads/work-queue", head]);
    await expect(readWorkQueueLog(f.options)).rejects.toThrow(/exactly one regular canonical log/);
    const result = await f.ledger.rest.git.getTree({ tree_sha: queueTree });
    expect(result.data.tree.map(entry => entry.path)).toEqual(["\uFEFFnotes.txt", "\uFEFFwork-queue.jsonl"]);
  });

  it("rechecks credentials before reusing immutable objects and confines proofs to their repository", async () => {
    const f = fixture();
    f.store(compactTransactions(f.queue.transactions, f.head(), f.queue.administrator, f.queue.at));
    await readWorkQueueLog(f.options);
    f.githubClient.auth = async () => {
      throw new Error("credential revoked");
    };
    await expect(readWorkQueueLog(f.options)).rejects.toThrow(/repository_unavailable/);
    await expect(readWorkQueueLog({ ...f.options, owner: "foreign" })).rejects.toThrow(/repository_scope_invalid/);
    await expect(writeCheckedFiles({ ...f.options, owner: "foreign", branch: "work-queue", expectedHeadOid: f.head(), files: [{ path: "work-queue.jsonl", content: "not a foreign ledger" }] })).rejects.toThrow(/repository_scope_invalid/);
    expect(f.calls).toEqual([]);
  });

  it("never makes the queue the default branch of a branchless repository", async () => {
    const f = fixture();
    f.git(["update-ref", "-d", "refs/heads/work-queue"]);
    expect((await readWorkQueueLog(f.options)).sha).toBeNull();
    const content = serializeTransactionLog(f.queue.transactions);
    const blob = await f.ledger.rest.git.createBlob({ content, encoding: "utf-8" });
    const tree = await f.ledger.rest.git.createTree({ tree: [{ path: "work-queue.jsonl", mode: "100644", type: "blob", sha: blob.data.sha }] });
    const commit = await f.ledger.rest.git.createCommit({ tree: tree.data.sha, parents: [], message: "First queue submission" });
    const publication = { ref: "refs/heads/work-queue", sha: commit.data.sha };
    await expect(f.ledger.rest.git.createRef(publication)).rejects.toThrow(/separate default branch/);
    expect(f.git(["ls-remote", f.git(["rev-parse", "--absolute-git-dir"]), "refs/heads/work-queue"])).toBe("");
    f.git(["update-ref", "refs/heads/main", f.head()]);
    await f.ledger.rest.git.createRef(publication);
    expect((await readWorkQueueLog(f.options)).sha).toBe(commit.data.sha);
    expect(f.git(["symbolic-ref", "HEAD"])).toBe("refs/heads/main");
    expect(f.calls).toEqual([]);
  });

  it("fails closed on unavailable ancestry rather than using API fallback", async () => {
    const f = fixture();
    const prior = f.head();
    f.store(compactTransactions(f.queue.transactions, prior, f.queue.administrator, f.queue.at));
    fs.unlinkSync(path.join(f.git(["rev-parse", "--absolute-git-dir"]), "objects", prior.slice(0, 2), prior.slice(2)));
    await expect(readWorkQueueLog(f.options)).rejects.toThrow(/git_transport_failed/);
    expect(f.calls).toEqual([]);
  });

  it("does not inherit checkout credentials, replacement refs or Git tracing", () => {
    const previous = process.env.GIT_TRACE;
    process.env.GIT_TRACE = "unsafe-trace-file";
    try {
      const env = gitEnvironment();
      expect(env.GIT_TRACE).toBeUndefined();
      expect(env.GIT_CONFIG_COUNT).toBeUndefined();
      expect(env.GIT_EXEC_PATH).toBe(process.env.GIT_EXEC_PATH);
      expect(env.GIT_NO_REPLACE_OBJECTS).toBe("1");
      expect(env.GIT_NO_LAZY_FETCH).toBe("1");
      expect(() => queueLedgerClient({}, "../foreign", "repo")).toThrow(/repository_scope_invalid/);
    } finally {
      if (previous === undefined) delete process.env.GIT_TRACE;
      else process.env.GIT_TRACE = previous;
    }
  });
});
