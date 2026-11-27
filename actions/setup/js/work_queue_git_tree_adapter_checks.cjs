"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const crypto = require("node:crypto");
const { withClaimExecution } = require("./work_queue_claim_scope.cjs");
const { createClaimAdapterHandler, preparedAdapterPath, validateAdapter } = require("./work_queue_claim_adapters.cjs");
const { canonical } = require("./work_queue_codec.cjs");
const { prepareAdapterContext } = require("./work_queue_prepare_claim_adapter.cjs");

const BASE = "a".repeat(40);
const BASE_TREE = "b".repeat(40);
const hash = value => crypto.createHash("sha1").update(value).digest("hex");
const blobHash = value => hash(Buffer.concat([Buffer.from(`blob ${Buffer.byteLength(value)}\0`), Buffer.from(value)]));

function member(dispatch, handle = "h1") {
  return {
    handle,
    claim_id: `${dispatch}-claim-${handle}`,
    work_id: `${dispatch}-work-${handle}`,
    work: { effect_contract: { version: 1, outputs: [{ type: "code", min: 1, max: 1 }] } },
    result_refs: [],
  };
}

function assigned(dispatch = "dispatch") {
  return { dispatch_id: dispatch, claims: [member(dispatch)] };
}

function configured(pullRequest = true) {
  return {
    mode: "prepared",
    "effect-type": "git_tree",
    "target-repo": "owner/repo",
    "field-map": { files: "files", ...(pullRequest ? { title: "title", body: "body" } : {}) },
    "git-tree": { "base-revision": BASE, "branch-prefix": "automation/code", ...(pullRequest ? { "pull-request": true, "base-branch": "main" } : {}) },
  };
}

function native() {
  /** @type {Map<string, string | Buffer>} */
  const blobs = new Map([[blobHash("base"), "base"]]);
  /** @type {Map<string, Record<string, any>>} */
  const commits = new Map([[BASE, { sha: BASE, tree: { sha: BASE_TREE }, parents: [] }]]);
  const refs = new Map();
  const trees = new Map([[BASE_TREE, [{ path: "README.md", type: "blob", mode: "100644", sha: blobHash("base") }]]]);
  const pulls = new Map();
  const writes = [];
  /** @type {{mutateReadback: ((tree: any) => void) | null, afterBlob: (() => void) | null}} */
  const state = { mutateReadback: null, afterBlob: null };
  const rest = {
    repos: {
      get: async ({ owner, repo }) => {
        assert.equal(`${owner}/${repo}`, "owner/repo");
        return { data: { id: 7, full_name: "owner/repo" } };
      },
    },
    git: {
      getCommit: async ({ commit_sha }) => ({ data: structuredClone(commits.get(commit_sha)) }),
      getTree: async ({ tree_sha }) => {
        const data = { sha: tree_sha, truncated: false, tree: structuredClone(trees.get(tree_sha)) };
        if (state.mutateReadback && tree_sha !== BASE_TREE) state.mutateReadback(data);
        return { data };
      },
      getRef: async ({ ref }) => {
        if (!refs.has(ref)) throw Object.assign(new Error("missing"), { status: 404 });
        return { data: { object: { sha: refs.get(ref) } } };
      },
      getBlob: async ({ file_sha }) => {
        const text = blobs.get(file_sha);
        assert.ok(text !== undefined);
        return { data: { sha: file_sha, encoding: "base64", content: Buffer.from(text).toString("base64") } };
      },
      createBlob: async ({ content, encoding }) => {
        writes.push("blob");
        assert.equal(encoding, "base64");
        const text = Buffer.from(content, encoding);
        const sha = blobHash(text);
        blobs.set(sha, text);
        if (state.afterBlob) state.afterBlob();
        return { data: { sha } };
      },
      createTree: async ({ base_tree, tree }) => {
        writes.push("tree");
        assert.equal(base_tree, BASE_TREE);
        const base = trees.get(base_tree);
        assert.ok(base);
        const leaves = new Map(base.filter(entry => entry.type !== "tree").map(entry => [entry.path, entry]));
        for (const entry of tree) entry.sha === null ? leaves.delete(entry.path) : leaves.set(entry.path, entry);
        const entries = [...leaves.values()];
        const directories = new Set();
        for (const entry of entries) {
          const segments = entry.path.split("/");
          for (let count = 1; count < segments.length; count++) directories.add(segments.slice(0, count).join("/"));
        }
        for (const directory of directories) entries.push({ path: directory, mode: "040000", type: "tree", sha: hash(directory) });
        const sha = hash(canonical(entries));
        trees.set(sha, entries);
        return { data: { sha } };
      },
      createCommit: async ({ tree, parents, message }) => {
        writes.push("commit");
        assert.deepEqual(parents, [BASE]);
        const sha = hash(canonical({ tree, parents, message }));
        commits.set(sha, { sha, tree: { sha: tree }, parents: parents.map(parent => ({ sha: parent })), message });
        return { data: { sha } };
      },
      createRef: async ({ ref, sha }) => {
        writes.push("ref");
        assert.match(ref, /^refs\/heads\/automation\/code\/claims\/[a-f0-9]{64}$/);
        assert.ok(!refs.has(ref.slice(5)));
        refs.set(ref.slice(5), sha);
        return { data: { object: { sha } } };
      },
    },
    pulls: {
      create: async ({ head, base, title, body }) => {
        writes.push("pull");
        const number = pulls.size + 1;
        const data = { id: number + 100, number, title, body, head: { sha: refs.get(`heads/${head}`), ref: head, repo: { full_name: "owner/repo" } }, base: { ref: base, repo: { full_name: "owner/repo" } } };
        pulls.set(number, data);
        return { data: structuredClone(data) };
      },
      get: async ({ pull_number }) => ({ data: structuredClone(pulls.get(pull_number)) }),
    },
  };
  return { github: { rest }, state, writes, refs, trees, commits, blobs, pulls };
}

async function prepare(root, assignment, handle, adapter, message, github) {
  const filename = preparedAdapterPath(root, handle, "code", assignment);
  fs.mkdirSync(path.dirname(filename), { recursive: true });
  fs.writeFileSync(filename, JSON.stringify({ version: 3, claim_handle: handle, type: "code", messages: [{ input: message, payload: Object.fromEntries(Object.values(adapter["field-map"]).map(field => [field, message[field]])) }] }));
  return createClaimAdapterHandler({ adapter, filename, github });
}

function registerTests({ describe, it }) {
  describe("immutable per-Claim prepared code delivery", () => {
    it("rejects an original 2-Claim assignment before authorization, preparation or native effects", async () => {
      const root = require("./work_queue_effect_test_helpers.cjs").temporaryDirectory("claim-code-two-member");
      const assignment = { dispatch_id: "dispatch", claims: [member("dispatch", "h1"), member("dispatch", "h2")] };
      const adapter = configured(false);
      const host = native();
      const authorizations = [];
      const authorize = async request => {
        authorizations.push(request);
        return { claim_handle: request.claim_handle, authorized: true };
      };
      const messages = assignment.claims.map(entry => ({ type: "code", claim_handle: entry.handle, files: [{ path: `${entry.handle}.txt`, content: entry.work_id }] }));
      try {
        for (const index of [0, 1]) {
          await assert.rejects(prepareAdapterContext({ assignment, adapter, index, type: "code", messages, authorize, artifactRoot: root, github: host.github }), /exactly one open Claim/);
        }
        assert.throws(() => withClaimExecution({ assignment, claim_handle: "h1", authorize }, () => assert.fail("two-member assignment must not execute")), /exactly one open Claim/);
        assert.throws(() => preparedAdapterPath(root, "h1", "code", assignment), /exactly one open Claim/);
        assert.deepEqual(authorizations, []);
        assert.deepEqual(host.writes, []);
        assert.equal(host.refs.size, 0);
        assert.equal(fs.existsSync(path.join(root, "claims")), false);
      } finally {
        fs.rmSync(root, { recursive: true, force: true });
      }
    });

    it("prepares and delivers two singleton dispatches independently under the reused handle h1", async () => {
      const root = require("./work_queue_effect_test_helpers.cjs").temporaryDirectory("claim-code-matrix");
      const assignments = [assigned("dispatch-a"), assigned("dispatch-b")];
      const adapter = configured(false);
      const host = native();
      const authorize = async request => {
        assert.equal(request.assignment.claims.length, 1);
        assert.ok(assignments.some(entry => entry.dispatch_id === request.assignment.dispatch_id));
        return { claim_handle: request.claim_handle, authorized: true };
      };
      const results = [];
      const handlers = [];
      const outputFiles = new Set();
      try {
        for (const assignment of assignments) {
          const [entry] = assignment.claims;
          const input = { type: "code", files: [{ path: `${assignment.dispatch_id}.txt`, content: entry.work_id }] };
          const foreign = { type: "code", claim_handle: "h2", files: [{ path: "foreign.txt", content: "foreign" }] };
          const prepared = await prepareAdapterContext({ assignment, adapter, index: 0, type: "code", messages: [foreign, input], authorize, artifactRoot: root });
          assert.deepEqual(prepared.messages, [{ ...input, claim_handle: "h1" }]);
          assert.equal(prepared.outputFile, preparedAdapterPath(root, "h1", "code", assignment));
          outputFiles.add(prepared.outputFile);
          fs.writeFileSync(prepared.outputFile, JSON.stringify({ version: 3, claim_handle: "h1", type: "code", messages: [{ input: prepared.messages[0], payload: { files: input.files } }] }));
          const effects = [];
          await withClaimExecution({ assignment, claim_handle: "h1", authorize, effects }, async () => {
            const handler = await createClaimAdapterHandler({ adapter, filename: prepared.outputFile, github: host.github });
            handlers.push(handler);
            await assert.rejects(handler({ ...input, claim_handle: "h2" }), /foreign/);
            const result = await handler(input);
            results.push(result);
            assert.equal(result.success, true);
            assert.ok(effects.length > 0);
          });
        }
        assert.equal(outputFiles.size, 2);
        assert.equal(host.refs.size, 2);
        assert.equal(new Set(results.map(result => result.branch)).size, 2);
        assert.equal(host.pulls.size, 0);
        const writes = host.writes.length;
        await withClaimExecution({ assignment: assignments[1], claim_handle: "h1", authorize }, async () => {
          await assert.rejects(handlers[0]({ type: "code", files: [{ path: "escape.txt", content: "escape" }] }), /original immutable Claim identity/);
        });
        assert.equal(host.writes.length, writes);
      } finally {
        fs.rmSync(root, { recursive: true, force: true });
      }
    });

    it("writes exact tree, parent, branch and PR fields without a shared writer checkout", async () => {
      const root = require("./work_queue_effect_test_helpers.cjs").temporaryDirectory("claim-code");
      const assignment = assigned();
      const adapter = configured();
      const host = native();
      const authorize = async request => ({ claim_handle: request.claim_handle, authorized: true, state: "open" });
      const effects = [];
      const message = {
        type: "code",
        claim_handle: "h1",
        files: [
          { path: "src/code.js", content: "new code\n", mode: "100755" },
          { path: "binary.bin", content: Buffer.from([0, 255, 170]).toString("base64"), encoding: "base64" },
          { path: "README.md", delete: true },
        ],
        title: "Claim code",
        body: "Exact delivered code",
      };
      try {
        await withClaimExecution({ assignment, claim_handle: "h1", authorize, effects }, async () => {
          const handler = await prepare(root, assignment, "h1", adapter, message, host.github);
          const result = await handler(message);
          assert.deepEqual(host.writes, ["blob", "blob", "tree", "commit", "ref", "pull"]);
          assert.equal(host.refs.get(`heads/${result.branch}`), result.commit);
          const tree = host.trees.get(host.commits.get(result.commit)?.tree.sha);
          assert.ok(tree);
          assert.deepEqual(
            tree
              .filter(entry => entry.type === "blob")
              .map(entry => entry.path)
              .sort(),
            ["binary.bin", "src/code.js"]
          );
          const pull = host.pulls.get(result.number);
          assert.equal(pull.title, message.title);
          assert.equal(pull.body, message.body);
          assert.ok(effects.length > 0);
          await assert.rejects(handler(message), /only one immutable delivery/);
        });
      } finally {
        fs.rmSync(root, { recursive: true, force: true });
      }
    });

    it("isolates failed and valid code effects of two singleton dispatches and persistent branch-only deliveries", async () => {
      const root = require("./work_queue_effect_test_helpers.cjs").temporaryDirectory("claim-code");
      const failed = assigned("dispatch-a");
      const valid = assigned("dispatch-b");
      const host = native();
      let cancelled = false;
      const authorize = async request => ({ claim_handle: request.claim_handle, authorized: request.assignment.dispatch_id !== failed.dispatch_id || !cancelled });
      const adapter = configured(false);
      try {
        for (const assignment of [failed, valid]) {
          const message = { type: "code", claim_handle: "h1", files: [{ path: "state.json", content: `{"dispatch":"${assignment.dispatch_id}"}` }] };
          const effects = [];
          await withClaimExecution({ assignment, claim_handle: "h1", authorize, effects }, async () => {
            const handler = await prepare(root, assignment, "h1", adapter, message, host.github);
            host.state.afterBlob = () => {
              if (assignment === failed) cancelled = true;
            };
            if (assignment === failed) {
              await assert.rejects(handler(message), /not open/);
              assert.deepEqual(host.writes, ["blob"]);
            } else {
              const result = await handler(message);
              assert.equal(result.success, true);
              assert.ok(effects.length > 0);
            }
          });
        }
        assert.equal(host.refs.size, 1);
        assert.equal(host.pulls.size, 0);
      } finally {
        fs.rmSync(root, { recursive: true, force: true });
      }
    });

    it("rejects malformed, conflicting and unauthorized file paths before any native mutation", async () => {
      const root = require("./work_queue_effect_test_helpers.cjs").temporaryDirectory("claim-code");
      const assignment = assigned();
      const host = native();
      const adapter = configured(false);
      const authorize = async request => ({ claim_handle: request.claim_handle, authorized: request.resource?.path !== "forbidden.txt" });
      try {
        await withClaimExecution({ assignment, claim_handle: "h1", authorize }, async () => {
          for (const files of [
            [{ path: ".git/config", content: "bad" }],
            [{ path: "README.md/child", content: "conflict" }],
            [{ path: "new", delete: true }],
            [{ path: "forbidden.txt", content: "bad" }],
            [{ path: "x", content: "x", executable: true }],
          ]) {
            const message = { type: "code", claim_handle: "h1", files };
            const handler = await prepare(root, assignment, "h1", adapter, message, host.github);
            await assert.rejects(handler(message));
          }
        });
        assert.deepEqual(host.writes, []);
        const invalid = configured(false);
        invalid["git-tree"]["base-revision"] = "main";
        assert.throws(() => validateAdapter(invalid), /immutable base revision/);
      } finally {
        fs.rmSync(root, { recursive: true, force: true });
      }
    });
  });
}

module.exports = { registerTests };
if (require.main === module) registerTests(require("node:test"));
