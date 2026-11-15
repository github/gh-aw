"use strict";

const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { execFileSync } = require("node:child_process");

function createAssetGitFixture(root, ledger = "") {
  const state = { bad: false, report: null, refs: new Map() };
  const directory = fs.mkdtempSync(path.join(root, "git-"));
  const env = {
    ...Object.fromEntries(Object.entries(process.env).filter(([key]) => !key.startsWith("GIT_"))),
    GIT_CONFIG_NOSYSTEM: "1",
    GIT_CONFIG_GLOBAL: os.devNull,
    GIT_AUTHOR_NAME: "Asset fixture",
    GIT_AUTHOR_EMAIL: "asset-fixture@example.invalid",
    GIT_COMMITTER_NAME: "Asset fixture",
    GIT_COMMITTER_EMAIL: "asset-fixture@example.invalid",
  };
  const git = (args, input) => execFileSync("git", ["-c", `core.hooksPath=${os.devNull}`, "--git-dir", directory, ...args], { env, input, stdio: ["pipe", "pipe", "pipe"], timeout: 10000 });
  const text = (args, input) => git(args, input).toString("utf8").trim();
  git(["init", "--bare", "--quiet", "--template="]);
  const readTree = tree =>
    tree
      ? text(["ls-tree", "-z", tree])
          .split("\0")
          .filter(Boolean)
          .map(line => {
            const [header, name] = line.split("\t");
            const [mode, type, sha] = header.split(" ");
            return { mode, type, sha, path: name };
          })
      : [];
  const writeTree = (base, segments, entry) => {
    const entries = new Map(readTree(base).map(value => [value.path, value]));
    const [name, ...tail] = segments;
    if (tail.length) {
      const previous = entries.get(name);
      if (previous && previous.type !== "tree") throw new Error("fixture refuses to replace a durable file with a directory");
      entries.set(name, { path: name, mode: "040000", type: "tree", sha: writeTree(previous?.sha, tail, entry) });
    } else entries.set(name, { ...entry, path: name });
    return text(["mktree", "-z"], [...entries.values()].map(value => `${value.mode} ${value.type} ${value.sha}\t${value.path}\0`).join(""));
  };
  const logBlob = text(["hash-object", "-w", "--stdin"], ledger);
  const durableBlob = text(["hash-object", "-w", "--stdin"], "durable projection state");
  let queueTree = writeTree(undefined, ["work-queue.jsonl"], { mode: "100644", type: "blob", sha: logBlob });
  queueTree = writeTree(queueTree, ["projection", "state.json"], { mode: "100644", type: "blob", sha: durableBlob });
  const queueCommit = text(["commit-tree", queueTree, "-F", "-"], "Initialize queue fixture");
  git(["update-ref", "refs/heads/work-queue", queueCommit]);
  state.refs.set("heads/work-queue", queueCommit);
  const api = {
    getRef: async input => {
      if (!state.refs.has(input.ref)) throw Object.assign(new Error("missing"), { status: 404 });
      return { data: { object: { sha: state.bad ? "changed" : state.refs.get(input.ref) } } };
    },
    createBlob: async input => {
      const content = input.encoding === "base64" ? Buffer.from(input.content, "base64") : Buffer.from(input.content, "utf8");
      return { data: { sha: text(["hash-object", "-w", "--stdin"], content) } };
    },
    createTree: async input => {
      let id = input.base_tree;
      for (const entry of input.tree) id = writeTree(id, entry.path.split("/"), entry);
      return { data: { sha: id } };
    },
    createCommit: async input => ({ data: { sha: text(["commit-tree", input.tree, ...input.parents.flatMap(parent => ["-p", parent]), "-F", "-"], input.message) } }),
    createRef: async input => {
      git(["update-ref", input.ref, input.sha, "0".repeat(40)]);
      state.refs.set(input.ref.slice(5), input.sha);
      return { data: { object: { sha: input.sha } } };
    },
    updateRef: async input => {
      if (input.force !== false) throw new Error("fixture forbids forced updates");
      const parents = text(["cat-file", "-p", input.sha])
        .split("\n")
        .filter(line => line.startsWith("parent "))
        .map(line => line.slice(7));
      if (parents.length !== 1 || state.refs.get(input.ref) !== parents[0]) throw Object.assign(new Error("Update is not a fast forward"), { status: 422 });
      git(["update-ref", `refs/${input.ref}`, input.sha, parents[0]]);
      state.refs.set(input.ref, input.sha);
      return { data: { object: { sha: input.sha } } };
    },
    getCommit: async input => {
      if (state.bad && input.commit_sha === "changed") return { data: { sha: "changed" } };
      const headers = text(["cat-file", "-p", input.commit_sha]).split("\n\n", 1)[0].split("\n");
      const tree = headers.find(line => line.startsWith("tree "));
      if (!tree) throw new Error("fixture commit requires an immutable tree");
      return { data: { sha: input.commit_sha, tree: { sha: tree.slice(5) }, parents: headers.filter(line => line.startsWith("parent ")).map(line => ({ sha: line.slice(7) })) } };
    },
    getTree: async input => ({ data: { sha: input.tree_sha, truncated: false, tree: readTree(input.tree_sha) } }),
    getBlob: async input => ({ data: { sha: input.file_sha, encoding: "base64", content: git(["cat-file", "blob", input.file_sha]).toString("base64") } }),
  };
  return { api, state, git, logBlob, durableBlob };
}

module.exports = { createAssetGitFixture };
