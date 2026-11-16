// @ts-check
"use strict";

const { spawnSync } = require("node:child_process");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { pathToFileURL } = require("node:url");
const { queueError } = require("./work_queue_codec.cjs");
const { buildGitAuthEnv } = require("./git_auth_env.cjs");
const { createQueueGitObjectReader } = require("./work_queue_git_objects.cjs");

const OID = /^(?:[a-f0-9]{40}|[a-f0-9]{64})$/;
/** @type {WeakMap<object, Map<string, ReturnType<typeof createGitLedgerClient>>>} */
const clients = new WeakMap();
const overrides = new WeakMap();
const gitClients = new WeakMap();
const directories = new Set();
const objectReaders = new Set();
process.once("exit", () => {
  for (const reader of objectReaders) reader.close();
  for (const directory of directories) fs.rmSync(directory, { recursive: true, force: true });
});

// Explicit dependency injection for in-memory stores; production always uses Git.
function registerQueueLedgerClient(client, ledgerClient) {
  overrides.set(client, ledgerClient);
}

function inheritQueueLedgerClient(source, target) {
  if (overrides.has(source)) {
    const ledger = overrides.get(source);
    registerQueueLedgerClient(target, ledger === source ? target : ledger);
  }
  let repositories = clients.get(source);
  if (!repositories) clients.set(source, (repositories = new Map()));
  clients.set(target, repositories);
}

function queueGitScope(client) {
  return gitClients.has(client) ? client : undefined;
}

function queueLedgerClient(githubClient, owner, repo, branch = "work-queue") {
  const override = overrides.get(githubClient);
  const scope = gitClients.get(override || githubClient);
  if (scope && (scope.owner.toLowerCase() !== owner.toLowerCase() || scope.repo.toLowerCase() !== repo.toLowerCase() || scope.branch !== branch))
    throw queueError("repository_scope_invalid", "queue Git objects belong to a different repository or branch");
  if (override) return override;
  if (!/^[A-Za-z0-9_.-]+$/.test(owner) || !/^[A-Za-z0-9_.-]+$/.test(repo) || [owner, repo].some(part => part === "." || part === "..")) throw queueError("repository_scope_invalid", "invalid queue repository");
  const server = new URL(process.env.GITHUB_SERVER_URL || "https://github.com");
  if (server.protocol !== "https:" || server.username || server.password || server.pathname !== "/" || server.search || server.hash)
    throw queueError("repository_scope_invalid", "queue Git transport requires the configured HTTPS GitHub origin");
  const apiOrigin = process.env.GITHUB_API_URL || (server.hostname === "github.com" ? "https://api.github.com" : `${server.origin}/api/v3`);
  const clientOrigin = githubClient.request?.endpoint?.DEFAULTS?.baseUrl;
  if (clientOrigin && new URL(clientOrigin).origin !== new URL(apiOrigin).origin) throw queueError("repository_scope_invalid", "queue credentials belong to a different GitHub API origin");
  let repositories = clients.get(githubClient);
  if (!repositories) clients.set(githubClient, (repositories = new Map()));
  const key = `${server.origin}/${owner}/${repo}:${branch}`;
  let client = repositories.get(key);
  if (!client) {
    client = createGitLedgerClient({ githubClient, owner, repo, branch, remote: `${server.origin}/${owner}/${repo}.git` });
    repositories.set(key, client);
  }
  return client;
}

function gitEnvironment(authEnv = {}) {
  const env = Object.fromEntries(Object.entries(process.env).filter(([key]) => !key.startsWith("GIT_") || key === "GIT_EXEC_PATH"));
  return {
    ...env,
    GIT_CONFIG_NOSYSTEM: "1",
    GIT_CONFIG_GLOBAL: os.devNull,
    GIT_TERMINAL_PROMPT: "0",
    GIT_ASKPASS: "",
    SSH_ASKPASS: "",
    GIT_NO_REPLACE_OBJECTS: "1",
    GIT_NO_LAZY_FETCH: "1",
    GIT_AUTHOR_NAME: "github-actions[bot]",
    GIT_AUTHOR_EMAIL: "41898282+github-actions[bot]@users.noreply.github.com",
    GIT_COMMITTER_NAME: "github-actions[bot]",
    GIT_COMMITTER_EMAIL: "41898282+github-actions[bot]@users.noreply.github.com",
    ...authEnv,
  };
}

/** A Git-object implementation of the store's existing object/ref interface. */
function createGitLedgerClient({ githubClient, owner, repo, branch, remote }) {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-queue-git-"));
  directories.add(directory);
  const baseEnv = gitEnvironment();
  const gitArgs = ["-c", "credential.helper=", "-c", `core.hooksPath=${os.devNull}`, "-c", "http.followRedirects=false", "-c", "fetch.fsckObjects=true", "--git-dir", directory];
  const objectReader = createQueueGitObjectReader(gitArgs, baseEnv);
  objectReaders.add(objectReader);
  /** @type {Map<string, Buffer>} */
  const objects = new Map();
  let cachedBytes = 0;
  const commitParents = new Map();
  let fetched = false;
  let verifiedHead = "";
  let advertised = new Map();
  let defaultBranch = "";
  let filterSupported = false;

  /** @param {string[]} args @param {string | Buffer | undefined} [input] */
  function git(args, input = undefined, env = baseEnv, probeFilter = false) {
    const result = spawnSync("git", [...gitArgs, ...args], {
      env,
      input,
      maxBuffer: 81 * 1024 * 1024,
      timeout: 120000,
      stdio: ["pipe", "pipe", "pipe"],
    });
    if (result.error || result.status !== 0) {
      // Never forward child-process arguments/environment/stderr to logs.
      const failure = queueError("git_transport_failed", `queue Git ${args[0]} failed (${typeof result.status === "number" ? result.status : "transport"}); check queue credentials and repository access`);
      throw Object.assign(failure, { gitStatus: result.status });
    }
    const diagnostics = result.stderr.toString("utf8");
    if (probeFilter) {
      // Capture only the advertisement's protocol packets in memory. Never
      // trace HTTP authentication or forward diagnostics to callers or logs.
      filterSupported = /\bpacket:\s+\S+< fetch=(?:[^\r\n]* )?filter(?: |\r?$)/m.test(diagnostics) || /\bpacket:\s+\S+< [a-f0-9]{40,64} [^\r\n]*\\0[^\r\n]* filter(?: |\r?$)/m.test(diagnostics);
    }
    if (args.includes("--filter=blob:none") && /filtering not recognized by server, ignoring/.test(diagnostics))
      throw queueError("git_transport_failed", "queue Git server must support blob filtering; unfiltered asset downloads are not permitted");
    return result.stdout;
  }
  git(["init", "--bare", "--quiet", "--template="]);
  git(["check-ref-format", `refs/heads/${branch}`]);

  async function network(args, probeFilter = false) {
    if (typeof githubClient.auth !== "function") throw queueError("repository_unavailable", "queue Git transport requires the selected queue client's credentials");
    const authentication = await githubClient.auth({ type: "token" });
    if (authentication?.type !== "token" || typeof authentication.token !== "string" || !authentication.token) throw queueError("repository_unavailable", "queue Git transport requires a repository token");
    // Git rejects absolute filesystem paths as filtered promisor remote names.
    const commandRemote = path.isAbsolute(remote) ? pathToFileURL(remote).href : remote;
    return git(
      args.map(arg => (arg === remote ? commandRemote : arg)),
      undefined,
      gitEnvironment({ ...buildGitAuthEnv(authentication.token), LC_ALL: "C", ...(probeFilter ? { GIT_TRACE_PACKET: "1" } : {}) }),
      probeFilter
    );
  }

  async function advertise() {
    filterSupported = false;
    const output = await network(["ls-remote", "--symref", remote, "HEAD", "refs/heads/*"], true);
    advertised = new Map();
    defaultBranch = "";
    for (const line of output.toString("utf8").trim().split("\n")) {
      const [value, ref] = line.split("\t");
      if (ref === "HEAD" && value.startsWith("ref: refs/heads/")) defaultBranch = value.slice("ref: refs/heads/".length);
      else if (ref && OID.test(value)) advertised.set(ref, value);
    }
    fetched = false;
  }

  async function fetchObjects() {
    if (fetched) return;
    if (fs.existsSync(path.join(directory, "shallow"))) throw queueError("checkpoint_invalid", "queue history must not be shallow");
    if (!filterSupported) throw queueError("git_transport_failed", "queue Git server must support blob filtering; unfiltered asset downloads are not permitted");
    // Recheck access through advertisement, then reuse the validated local history.
    if (verifiedHead && advertised.get(`refs/heads/${branch}`) === verifiedHead) {
      fetched = true;
      return;
    }
    await network(["fetch", "--quiet", "--no-tags", "--no-recurse-submodules", "--filter=blob:none", remote, `+refs/heads/${branch}:refs/heads/queue`]);
    if (fs.existsSync(path.join(directory, "shallow"))) throw queueError("checkpoint_invalid", "queue history must not be shallow");
    git(["fsck", "--connectivity-only", "--no-dangling", "--no-reflogs", "refs/heads/queue"]);
    git(["rev-list", "--quiet", "--objects", "--filter=blob:none", "--missing=error", "refs/heads/queue"]);
    verifiedHead = git(["rev-parse", "--verify", "refs/heads/queue^{commit}"]).toString("utf8").trim();
    fetched = true;
  }

  async function object(oid, type) {
    if (!OID.test(oid)) throw queueError("ledger_invalid", "invalid queue Git object identity");
    const key = `${type}:${oid}`;
    const cached = objects.get(key);
    if (cached) return cached;
    await fetchObjects();
    if (type === "blob") {
      try {
        git(["cat-file", "-e", `${oid}^{blob}`]);
      } catch {
        await network(["fetch", "--quiet", "--no-tags", "--no-recurse-submodules", "--filter=blob:none", remote, oid]);
      }
    }
    const data = await objectReader.object(oid, type);
    // Keep the immutable ancestry hot without retaining an unbounded ledger.
    if (data.length <= 4 * 1024 * 1024) {
      while (objects.size && (cachedBytes + data.length > 16 * 1024 * 1024 || objects.size >= 512)) {
        const oldest = objects.keys().next().value;
        if (oldest === undefined) break;
        cachedBytes -= objects.get(oldest)?.length || 0;
        objects.delete(oldest);
      }
      objects.set(key, data);
      cachedBytes += data.length;
    }
    return data;
  }

  async function getCommit({ commit_sha }) {
    const contents = (await object(commit_sha, "commit")).toString("utf8").split("\n\n", 1)[0];
    const tree = contents
      .split("\n")
      .find(line => line.startsWith("tree "))
      ?.slice(5);
    const parents = contents
      .split("\n")
      .filter(line => line.startsWith("parent "))
      .map(line => ({ sha: line.slice(7) }));
    return { data: { sha: commit_sha, tree: { sha: tree }, parents } };
  }

  async function getTree({ tree_sha }) {
    const bytes = await object(tree_sha, "tree");
    const entries = [];
    const oidBytes = tree_sha.length / 2;
    for (let offset = 0; offset < bytes.length;) {
      const space = bytes.indexOf(32, offset);
      const nul = bytes.indexOf(0, space + 1);
      if (space < offset || nul < space || nul + 1 + oidBytes > bytes.length || entries.length >= 16384) throw queueError("ledger_invalid", "queue Git tree is malformed or oversized");
      const mode = bytes.subarray(offset, space).toString("ascii").padStart(6, "0");
      const name = new TextDecoder("utf-8", { fatal: true, ignoreBOM: true }).decode(bytes.subarray(space + 1, nul));
      const sha = bytes.subarray(nul + 1, nul + 1 + oidBytes).toString("hex");
      entries.push({ mode, type: mode === "040000" ? "tree" : mode === "160000" ? "commit" : "blob", sha, path: name });
      offset = nul + 1 + oidBytes;
    }
    return { data: { truncated: false, tree: entries } };
  }

  async function createTree({ base_tree, tree }) {
    const entries = new Map(base_tree ? (await getTree({ tree_sha: base_tree })).data.tree.map(entry => [entry.path, entry]) : []);
    for (const entry of tree) {
      if (typeof entry.path !== "string" || entry.path.includes("/") || /[\0\t\n]/.test(entry.path) || !OID.test(entry.sha) || entry.mode !== "100644" || entry.type !== "blob")
        throw queueError("publication_invalid", "queue tree writes require canonical root blobs");
      entries.set(entry.path, entry);
    }
    const input = [...entries.values()].map(entry => `${entry.mode} ${entry.type} ${entry.sha}\t${entry.path}\0`).join("");
    return { data: { sha: git(["mktree", "-z"], input).toString("utf8").trim() } };
  }

  async function push(ref, sha, expected) {
    if (!ref.startsWith("refs/heads/") || git(["check-ref-format", ref]).length || (sha && !OID.test(sha)) || (expected && !OID.test(expected))) throw queueError("publication_invalid", "invalid checked queue ref update");
    if (ref === `refs/heads/${branch}` && !expected) {
      const head = (await network(["ls-remote", "--symref", remote, "HEAD"])).toString("utf8");
      if (!head.split("\n").some(line => OID.test(line.split("\t")[0])) || head.includes(`ref: ${ref}\tHEAD`)) throw queueError("repository_unavailable", "queue publication requires a readable, separate default branch");
    }
    try {
      await network(["push", "--quiet", "--porcelain", `--force-with-lease=${ref}:${expected}`, remote, `${sha}:${ref}`]);
    } catch (error) {
      // Classify a proven race only. An unknown/lost push response remains
      // ambiguous so the caller recovers the original stable request.
      await advertise();
      if ((advertised.get(ref) || "") !== expected) throw Object.assign(queueError("publication_conflict", "reference update failed: queue head changed"), { status: 409 });
      throw error;
    }
    advertised.set(ref, sha);
    return { data: {} };
  }

  const client = {
    rest: {
      repos: {
        get: async () => {
          await advertise();
          return { data: { full_name: `${owner}/${repo}`, size: advertised.size ? 1 : 0, default_branch: defaultBranch } };
        },
      },
      git: {
        getRef: async ({ ref }) => {
          const sha = advertised.get(`refs/${ref}`);
          if (!sha) throw Object.assign(queueError("queue_missing", "queue Git ref is absent"), { status: 404 });
          return { data: { object: { sha } } };
        },
        getCommit,
        getTree,
        getBlob: async ({ file_sha }) => {
          const data = await object(file_sha, "blob");
          return { data: { sha: file_sha, encoding: "base64", content: data.toString("base64"), size: data.length } };
        },
        createBlob: async ({ content, encoding }) => {
          if (encoding !== "utf-8") throw queueError("publication_invalid", "queue blob writes require UTF-8");
          return { data: { sha: git(["hash-object", "-w", "--stdin"], content).toString("utf8").trim() } };
        },
        createTree,
        createCommit: async ({ tree, parents, message }) => {
          if (!OID.test(tree) || parents.length > 1 || parents.some(parent => !OID.test(parent))) throw queueError("publication_invalid", "queue commits require one immutable parent or a new root");
          const sha = git(["commit-tree", tree, ...parents.flatMap(parent => ["-p", parent]), "-F", "-"], message)
            .toString("utf8")
            .trim();
          commitParents.set(sha, parents);
          return { data: { sha } };
        },
        updateRef: async ({ ref, sha, force }) => {
          const parents = commitParents.get(sha);
          if (force !== false || parents?.length !== 1) throw queueError("publication_invalid", "queue ref updates require a newly checked single-parent commit");
          return push(`refs/${ref}`, sha, parents[0]);
        },
        createRef: async ({ ref, sha }) => push(ref, sha, ""),
      },
    },
    async readFiles(head, paths) {
      await fetchObjects();
      const result = new Map();
      for (const file of paths) {
        const entry = git(["ls-tree", "-z", head, "--", file]).toString("utf8");
        if (!entry) continue;
        const [mode, type, oid] = entry.slice(0, entry.indexOf("\t")).split(" ");
        if (mode !== "100644" || type !== "blob") throw queueError("projection_journal_conflict", "projection journal must be a regular blob");
        result.set(file, new TextDecoder("utf-8", { fatal: true }).decode(await object(oid, "blob")));
      }
      return result;
    },
    async writeFiles(head, files) {
      await fetchObjects();
      const index = path.join(directory, "publication-index");
      const env = { ...baseEnv, GIT_INDEX_FILE: index };
      git(["read-tree", head], undefined, env);
      for (const file of files) {
        const oid = git(["hash-object", "-w", "--stdin"], file.content).toString("utf8").trim();
        git(["update-index", "--add", "--cacheinfo", `100644,${oid},${file.path}`], undefined, env);
      }
      const tree = git(["write-tree"], undefined, env).toString("utf8").trim();
      const commit = await client.rest.git.createCommit({ tree, parents: [head], message: "Publish checked work queue projection" });
      await client.rest.git.updateRef({ ref: `heads/${branch}`, sha: commit.data.sha, force: false });
      return commit.data.sha;
    },
    close() {
      objectReader.close();
      objectReaders.delete(objectReader);
      fs.rmSync(directory, { recursive: true, force: true });
      directories.delete(directory);
    },
  };
  registerQueueLedgerClient(client, client);
  gitClients.set(client, { owner, repo, branch });
  return client;
}

module.exports = { createGitLedgerClient, queueLedgerClient, registerQueueLedgerClient, inheritQueueLedgerClient, queueGitScope, gitEnvironment };
