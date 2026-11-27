import { afterEach, describe, expect, it } from "vitest";
import { execFileSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { createGitLedgerClient, gitEnvironment, queueLedgerClient, registerQueueLedgerClient } = require("./work_queue_git_transport.cjs");
const { publishWorkQueueCheckpoint, publishWorkQueueRequest, readWorkQueueLog } = require("./work_queue_store.cjs");
const { planDispatch } = require("./work_queue_scheduler.cjs");
const h = require("./work_queue_test_helpers.cjs");

const cleanups = [];
afterEach(() => {
  for (const cleanup of cleanups.splice(0).reverse()) cleanup();
});

/** A local bare repository served through the production Git ledger client. */
function fixture() {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "queue-git-test-"));
  cleanups.push(() => fs.rmSync(directory, { recursive: true, force: true }));
  const env = gitEnvironment();
  function git(args, input) {
    return execFileSync("git", ["--git-dir", directory, ...args], { env, input, stdio: ["pipe", "pipe", "pipe"] })
      .toString("utf8")
      .trim();
  }
  git(["init", "--bare", "--quiet", "--initial-branch=main", "--template="]);
  let head;
  function store(contents, { parents = head ? [head] : [], branch = "work-queue" } = {}) {
    const blob = git(["hash-object", "-w", "--stdin"], contents);
    const tree = git(["mktree"], `100644 blob ${blob}\twork-queue.jsonl\n`);
    head = git(["commit-tree", tree, ...parents.flatMap(parent => ["-p", parent]), "-F", "-"], "Queue fixture");
    git(["update-ref", `refs/heads/${branch}`, head]);
    return head;
  }
  const log = h.submit([], [h.work("a"), h.work("b")]);
  store(h.ledgerText(log), { branch: "main" });
  store(h.ledgerText(log));
  const calls = [];
  const forbidden = () => {
    calls.push("API");
    throw new Error("the Git ledger transport must not call hosted APIs");
  };
  const githubClient = { auth: async () => ({ type: "token", token: "queue-test-token" }), graphql: forbidden, rest: new Proxy({}, { get: forbidden }) };
  const ledger = createGitLedgerClient({ githubClient, owner: "owner", repo: "repo", branch: "work-queue", remote: directory });
  registerQueueLedgerClient(githubClient, ledger);
  cleanups.push(() => ledger.close());
  return { log, githubClient, ledger, calls, store, git, options: { githubClient, owner: "owner", repo: "repo" }, head: () => git(["rev-parse", "refs/heads/work-queue"]) };
}

describe("Git work queue ledger transport", { timeout: 60000 }, () => {
  it("reads and publishes through Git CAS only, recovering a committed request", async () => {
    const f = fixture();
    expect((await readWorkQueueLog(f.options)).state.stats.available).toBe(2);
    const options = {
      ...f.options,
      actor: h.dispatcher,
      request: { kind: "dispatch_next", parameters: h.dispatchParameters() },
      operationsFor: (state, request) => planDispatch(state, request.parameters, request.id).operations,
    };
    const published = await publishWorkQueueRequest(options);
    expect(published).toMatchObject({ committed: true, recovered: false });
    expect(f.head()).toBe(published.sha);
    expect(await publishWorkQueueRequest(options)).toMatchObject({ committed: true, recovered: true });
    expect(f.head()).toBe(published.sha);
    expect(f.calls).toEqual([]);
  });

  it("re-plans after another writer moves the remote head", async () => {
    const f = fixture();
    await readWorkQueueLog(f.options);
    const competing = h.dispatchNext(f.log, { concurrency: 1 }, { ...h.dispatcher, run_id: "900" });
    f.store(h.ledgerText(competing.log));
    const plans = [];
    const result = await publishWorkQueueRequest({
      ...f.options,
      actor: h.dispatcher,
      request: { kind: "dispatch_next", parameters: h.dispatchParameters({ concurrency: 2 }) },
      operationsFor: (state, request) => {
        plans.push(state.stats.claimed);
        return planDispatch(state, request.parameters, request.id).operations;
      },
    });
    expect(plans).toEqual([1]);
    expect(result.state.stats.claimed).toBe(2);
  });

  it("checkpoints as a child of the previous head", async () => {
    const f = fixture();
    const before = f.head();
    expect(await publishWorkQueueCheckpoint({ ...f.options, actor: h.reconciler, minimumLines: 0 })).toMatchObject({ checkpointed: false });
    const extended = h.submit(f.log, [h.work("c")], { ...h.producer, run_id: "11" });
    f.store(h.ledgerText(extended));
    const result = await publishWorkQueueCheckpoint({ ...f.options, actor: h.reconciler, minimumLines: 1 });
    expect(result.checkpointed).toBe(true);
    expect(f.git(["rev-parse", `${f.head()}^`])).not.toBe(before);
    expect(f.git(["show", `${f.head()}:work-queue.jsonl`]).split("\n")).toHaveLength(1);
    expect((await readWorkQueueLog(f.options)).state.stats.work).toBe(3);
  });

  it("distinguishes an absent branch from an unreadable repository", async () => {
    const f = fixture();
    f.git(["update-ref", "-d", "refs/heads/work-queue"]);
    expect((await readWorkQueueLog(f.options)).sha).toBeNull();
    f.githubClient.auth = async () => {
      throw new Error("credential revoked");
    };
    await expect(readWorkQueueLog(f.options)).rejects.toThrow(/repository_unavailable/);
    expect(f.calls).toEqual([]);
  });

  it("requires exactly one regular log entry", async () => {
    const f = fixture();
    const blob = f.git(["hash-object", "-w", "--stdin"], h.ledgerText(f.log));
    const tree = f.git(["mktree", "-z"], [`100644 blob ${blob}\t\uFEFFnotes.txt\0`, `100644 blob ${blob}\t\uFEFFwork-queue.jsonl\0`].join(""));
    const head = f.git(["commit-tree", tree, "-p", f.head(), "-F", "-"], "BOM queue filename");
    f.git(["update-ref", "refs/heads/work-queue", head]);
    await expect(readWorkQueueLog(f.options)).rejects.toThrow(/exactly one regular log/);
    expect((await f.ledger.rest.git.getTree({ tree_sha: tree })).data.tree.map(entry => entry.path)).toEqual(["\uFEFFnotes.txt", "\uFEFFwork-queue.jsonl"]);
  });

  it("never makes the queue the default branch of a branchless repository", async () => {
    const f = fixture();
    f.git(["update-ref", "-d", "refs/heads/work-queue"]);
    f.git(["update-ref", "-d", "refs/heads/main"]);
    const blob = await f.ledger.rest.git.createBlob({ content: h.ledgerText(f.log), encoding: "utf-8" });
    const tree = await f.ledger.rest.git.createTree({ tree: [{ path: "work-queue.jsonl", mode: "100644", type: "blob", sha: blob.data.sha }] });
    const commit = await f.ledger.rest.git.createCommit({ tree: tree.data.sha, parents: [], message: "First queue submission" });
    await expect(f.ledger.rest.git.createRef({ ref: "refs/heads/work-queue", sha: commit.data.sha })).rejects.toThrow(/separate default branch/);
    expect(f.calls).toEqual([]);
  });

  it("does not inherit checkout credentials, replacement refs or Git tracing", () => {
    const previous = process.env.GIT_TRACE;
    process.env.GIT_TRACE = "unsafe-trace-file";
    try {
      const env = gitEnvironment();
      expect(env.GIT_TRACE).toBeUndefined();
      expect(env.GIT_NO_REPLACE_OBJECTS).toBe("1");
      expect(env.GIT_NO_LAZY_FETCH).toBe("1");
      expect(() => queueLedgerClient({}, "../foreign", "repo")).toThrow(/repository_scope_invalid/);
    } finally {
      if (previous === undefined) delete process.env.GIT_TRACE;
      else process.env.GIT_TRACE = previous;
    }
  });
});
