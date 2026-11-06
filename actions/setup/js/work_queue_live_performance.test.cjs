// @ts-check
import { describe, expect, it } from "vitest";
import { execFileSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { performance } from "node:perf_hooks";
import { createRequire } from "node:module";
import { digest } from "./work_queue_codec.cjs";
import { replayTransactionLog, serializeProjection } from "./work_queue_replay.cjs";
import { readWorkQueueLog } from "./work_queue_store.cjs";

const require = createRequire(import.meta.url);
const { registerStaticQueueAuthentication } = require("./work_queue_auth_cache.cjs");
const { createGitLedgerClient, gitEnvironment, registerQueueLedgerClient } = require("./work_queue_git_transport.cjs");
const head = process.env.GH_AW_WORK_QUEUE_PERF_LIVE_HEAD;

function projectionDigest(state) {
  return digest(JSON.parse(JSON.stringify(serializeProjection(state))));
}

function gitObject(type, sha) {
  expect(sha).toMatch(/^(?:[a-f0-9]{40}|[a-f0-9]{64})$/);
  return execFileSync("git", ["cat-file", type, sha], { timeout: 5000, maxBuffer: 80 * 1024 * 1024, env: { ...process.env, GIT_NO_LAZY_FETCH: "1", GIT_NO_REPLACE_OBJECTS: "1" } });
}

function recordedClient(repository, registered) {
  const calls = [];
  const token = "offline-performance-token";
  const record = (name, implementation) => async parameters => {
    calls.push(name);
    return implementation(parameters);
  };
  const client = {
    auth: async () => ({ type: "token", token }),
    rest: {
      repos: {
        get: record("repos.get", () => ({ data: { full_name: repository, default_branch: "main", size: 1 } })),
      },
      git: {
        getRef: record("git.getRef", ({ ref }) => {
          expect(ref).toBe("heads/work-queue");
          return { data: { ref: `refs/${ref}`, object: { type: "commit", sha: head } } };
        }),
        getCommit: record("git.getCommit", ({ commit_sha }) => {
          const headers = gitObject("commit", commit_sha).toString("utf8").split("\n\n")[0].split("\n");
          const tree = headers.find(line => line.startsWith("tree "));
          if (!tree) throw new Error("Captured Git commit has no tree");
          return { data: { sha: commit_sha, tree: { sha: tree.slice(5) }, parents: headers.filter(line => line.startsWith("parent ")).map(line => ({ sha: line.slice(7) })) } };
        }),
        getTree: record("git.getTree", ({ tree_sha }) => {
          expect(tree_sha).toMatch(/^(?:[a-f0-9]{40}|[a-f0-9]{64})$/);
          const contents = execFileSync("git", ["ls-tree", "--full-tree", "-z", tree_sha], { timeout: 5000, maxBuffer: 1024 * 1024, env: { ...process.env, GIT_NO_LAZY_FETCH: "1", GIT_NO_REPLACE_OBJECTS: "1" } }).toString("utf8");
          const tree = contents
            .split("\0")
            .filter(Boolean)
            .map(line => {
              const separator = line.indexOf("\t");
              const [mode, type, sha] = line.slice(0, separator).split(" ");
              return { mode, type, sha, path: line.slice(separator + 1) };
            });
          return { data: { sha: tree_sha, truncated: false, tree } };
        }),
        getBlob: record("git.getBlob", ({ file_sha }) => {
          const content = gitObject("blob", file_sha);
          return { data: { sha: file_sha, encoding: "base64", content: content.toString("base64"), size: content.length } };
        }),
      },
    },
  };
  if (registered) registerStaticQueueAuthentication(client, token);
  registerQueueLedgerClient(client, client);
  return { client, calls };
}

describe.runIf(Boolean(head))("read-only captured live work queue performance", () => {
  it("validates captured queue ancestry through the production Git transport in less than ten seconds", async () => {
    const filename = process.env.GH_AW_WORK_QUEUE_PERF_LEDGER;
    if (!filename || !/^(?:[a-f0-9]{40}|[a-f0-9]{64})$/.test(head ?? "")) throw new Error("A captured ledger and immutable Git head are required");
    const expected = replayTransactionLog(fs.readFileSync(filename, "utf8"));
    const [owner, repo] = expected.repository.split("/");
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), "queue-live-performance-"));
    const env = gitEnvironment();
    let ledger;
    try {
      const source = execFileSync("git", ["rev-parse", "--show-toplevel"], { env, encoding: "utf8" }).trim();
      execFileSync("git", ["init", "--bare", "--quiet", "--template=", directory], { env });
      execFileSync("git", ["--git-dir", directory, "fetch", "--quiet", "--no-tags", "--no-recurse-submodules", source, `${head}:refs/heads/work-queue`], { env, timeout: 120000, stdio: "pipe" });
      const githubClient = { auth: async () => ({ type: "token", token: "offline-performance-token" }) };
      ledger = createGitLedgerClient({ githubClient, owner, repo, branch: "work-queue", remote: directory });
      registerQueueLedgerClient(githubClient, ledger);
      const start = performance.now();
      const cold = await readWorkQueueLog({ githubClient, owner, repo });
      const coldMs = performance.now() - start;
      expect(cold.sha).toBe(head);
      expect(projectionDigest(cold.state)).toBe(projectionDigest(expected));
      console.info(
        JSON.stringify({ benchmark: "captured-production-git-queue", head, works: cold.state.works.size, claims: cold.state.claims.size, requests: cold.state.requests.size, cold_ms: Math.round(coldMs), budget_ms: 10000, mutations: 0 })
      );
      expect(coldMs).toBeLessThan(10000);
    } finally {
      ledger?.close();
      fs.rmSync(directory, { recursive: true, force: true });
    }
  }, 120000);

  it.each([false, true])(
    "verifies actual Git checkpoint history and measures repeated reads (static cache registered: %s)",
    async registered => {
      const filename = process.env.GH_AW_WORK_QUEUE_PERF_LEDGER;
      if (!filename) throw new Error("GH_AW_WORK_QUEUE_PERF_LEDGER must accompany GH_AW_WORK_QUEUE_PERF_LIVE_HEAD");
      const contents = fs.readFileSync(filename, "utf8");
      const expected = replayTransactionLog(contents);
      const repository = expected.repository;
      expect(repository).toMatch(/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/);
      const [owner, repo] = repository.split("/");
      const { client, calls } = recordedClient(repository, registered);
      const options = { githubClient: client, owner, repo };
      const cold = await readWorkQueueLog(options);
      expect(cold.sha).toBe(head);
      expect(projectionDigest(cold.state)).toBe(projectionDigest(expected));
      const coldCalls = calls.length;
      expect(coldCalls).toBeGreaterThan(2);
      let warmCalls = 0;
      for (let iteration = 0; iteration < 3; iteration++) {
        calls.length = 0;
        const warm = await readWorkQueueLog(options);
        expect(projectionDigest(warm.state)).toBe(projectionDigest(expected));
        if (registered) expect(calls).toEqual(["repos.get", "git.getRef"]);
        else expect(calls.length).toBe(coldCalls);
        warmCalls = calls.length;
        warm.state.works.clear();
        warm.transactions.length = 0;
      }
      console.info(
        JSON.stringify({
          benchmark: "captured-live-work-queue",
          head,
          registered_static_cache: registered,
          bytes: Buffer.byteLength(contents),
          works: expected.works.size,
          claims: expected.claims.size,
          dispatches: expected.dispatches.size,
          requests: expected.requests.size,
          cold_api_calls: coldCalls,
          warm_api_calls: warmCalls,
          simulated_warm_network_ms: warmCalls * 250,
          mutations: 0,
        })
      );
    },
    120000
  );
});
