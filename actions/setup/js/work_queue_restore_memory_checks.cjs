"use strict";

const assert = require("node:assert/strict");
const { createHash } = require("node:crypto");
const { restoreESLintMemory } = require("./work_queue_restore_memory.cjs");

const BASE = "46b68a61c366a01d86dc319b8e689d09a48bad04";
const SNAPSHOT = "b".repeat(40);
const REF = `refs/heads/memory/eslint-refiner-runs/claims/${"c".repeat(64)}`;

function fixture() {
  const blobs = new Map();
  const blob = (name, content) => {
    const bytes = Buffer.from(content);
    const sha = createHash("sha1").update(`blob ${bytes.length}\0`).update(bytes).digest("hex");
    blobs.set(sha, { sha, encoding: "base64", size: bytes.length, content: bytes.toString("base64") });
    return { path: name, sha, type: "blob", mode: "100644", size: bytes.length };
  };
  const legacy = [blob("strategy.json", '{"strategy":"keep existing memory"}\n'), blob("history.jsonl", '{"run":1}\n{"run":2}\n')];
  const memory = blob("eslint-refiner.json", '{"work_id":"eslint-refiner:known","findings":["exact snapshot"]}\n');
  const calls = [];
  const state = { missingLegacy: false, truncated: false, repositoryId: 1036865607, refs: [{ ref: REF, object: { sha: SNAPSHOT, type: "commit" } }], tamper: false };
  const read = name => {
    calls.push(name);
    assert.ok(!/create|update|delete|dispatch|push/i.test(name));
  };
  const github = {
    rest: {
      repos: {
        get: async () => {
          read("repos.get");
          return { data: { id: state.repositoryId, full_name: "github/gh-aw" } };
        },
      },
      git: {
        getRef: async args => {
          read("git.getRef");
          assert.equal(args.ref, "heads/memory/eslint-refiner");
          if (state.missingLegacy) throw Object.assign(new Error("missing"), { status: 404 });
          return { data: { ref: `refs/${args.ref}`, object: { sha: BASE, type: "commit" } } };
        },
        getCommit: async args => {
          read("git.getCommit");
          assert.ok([BASE, SNAPSHOT].includes(args.commit_sha));
          return { data: { sha: args.commit_sha, tree: { sha: args.commit_sha === BASE ? "d".repeat(40) : "e".repeat(40) } } };
        },
        getTree: async args => {
          read("git.getTree");
          assert.equal(args.recursive, "1");
          return { data: { sha: args.tree_sha, truncated: state.truncated, tree: args.tree_sha === "d".repeat(40) ? legacy : [...legacy, memory] } };
        },
        getBlob: async args => {
          read("git.getBlob");
          const data = structuredClone(blobs.get(args.file_sha));
          if (state.tamper) data.content = Buffer.from("x".repeat(data.size)).toString("base64");
          return { data };
        },
      },
    },
    paginate: async (route, args) => {
      read("paginate:GET");
      assert.equal(route, "GET /repos/{owner}/{repo}/git/matching-refs/{ref}");
      assert.equal(args.ref, "heads/memory/eslint-refiner-runs/claims/");
      assert.equal(args.per_page, 100);
      return structuredClone(state.refs);
    },
  };
  return { github, state, calls, blobs, legacy };
}

function registerTests({ describe, it }) {
  describe("read-only restore of persistent Claim and legacy ESLint memory", () => {
    it("restores legacy JSON/JSONL and new snapshots from exact native refs, commits, trees and blob bytes", async () => {
      const f = fixture();
      const result = await restoreESLintMemory(f.github);
      assert.equal(result.legacy.commit, BASE);
      assert.deepEqual(
        result.legacy.files.map(file => file.path),
        ["strategy.json", "history.jsonl"]
      );
      assert.equal(result.snapshots.length, 1);
      assert.equal(result.snapshots[0].commit, SNAPSHOT);
      assert.equal(result.snapshots[0].ref, REF);
      assert.deepEqual({ ...result.snapshots[0].memory }, { work_id: "eslint-refiner:known", findings: ["exact snapshot"] });
      assert.equal("verified" in result, false);
      assert.equal("authority_resource" in result, false);
      assert.equal(f.calls.filter(name => name === "git.getBlob").length, 3);
    });

    it("retains the pinned original history when the legacy branch is absent and supports first-run snapshot absence", async () => {
      const f = fixture();
      f.state.missingLegacy = true;
      f.state.refs = [];
      const result = await restoreESLintMemory(f.github);
      assert.equal(result.legacy.commit, BASE);
      assert.equal(result.legacy.files.length, 2);
      assert.deepEqual(result.snapshots, []);
    });

    it("rejects foreign identities, incomplete trees, duplicate or malformed refs and changed blob bytes", async () => {
      for (const mutate of [
        f => {
          f.state.repositoryId = 7;
        },
        f => {
          f.state.truncated = true;
        },
        f => {
          f.state.refs.push(f.state.refs[0]);
        },
        f => {
          f.state.refs[0].ref = "refs/heads/foreign";
        },
        f => {
          f.state.refs[0].object.sha = "main";
        },
        f => {
          f.state.tamper = true;
        },
      ]) {
        const f = fixture();
        mutate(f);
        await assert.rejects(restoreESLintMemory(f.github));
      }
    });

    it("fails closed rather than silently discarding older history or oversized files", async () => {
      const f = fixture();
      f.state.refs = Array.from({ length: 129 }, () => f.state.refs[0]);
      await assert.rejects(restoreESLintMemory(f.github), /at most 128/);
      const oversized = fixture();
      oversized.legacy[0].size = 1048577;
      await assert.rejects(restoreESLintMemory(oversized.github), /byte limit/);
    });
  });
}

module.exports = { registerTests };
if (require.main === module) registerTests(require("node:test"));
