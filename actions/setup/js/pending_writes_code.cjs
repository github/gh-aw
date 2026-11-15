// @ts-check
"use strict";

const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { readInput } = require("./pending_writes_files.cjs");
const { resolveTransportPaths } = require("./resolve_transport_paths.cjs");
const { describePatchFiles } = require("./manifest_file_helpers.cjs");
const { extractPatchBaseCommit } = require("./commit_sha_helpers.cjs");
const { getExcludedFiles } = require("./workflow_file_exclusions.cjs");
const { buildExcludePathspecs } = require("./git_patch_utils.cjs");
const { execGitSync, execLocalGitSync, importLocalBundle, getGitAuthEnv, linearizeRangeAsCommit } = require("./git_helpers.cjs");
const { sha256 } = require("./pending_writes_files.cjs");
const { getBundlePreApplyFiles } = require("./push_to_pull_request_branch.cjs");
const { isObjectPack } = require("./object_pack_transport.cjs");
const OID = /^(?:[0-9a-f]{40}|[0-9a-f]{64})$/;
const { pendingRead } = require("./pending_writes_read.cjs");

/**
 * Bundle manifests use Git and the publisher's own changed-file helper. Work in
 * an isolated object store; never deepen or mutate the user's checkout.
 */
async function inspectTransport(transport, message, target, config, token, localRepository = undefined, githubClient = globalThis.github) {
  const server = new URL(process.env.GITHUB_SERVER_URL || "https://github.com");
  if (server.protocol !== "https:" || server.username || server.password || server.pathname !== "/" || server.search || server.hash) throw new Error("Pending bundle requires the trusted GitHub server");
  const [owner, repo] = target.repository.split("/");
  let base = target.head_commit;
  if (!base) {
    const { data: branch } = await pendingRead(() => githubClient.rest.repos.getBranch({ owner, repo, branch: target.base }));
    base = branch?.commit?.sha;
  }
  if (!OID.test(base || "")) throw new Error("Pending bundle requires an independently resolved base commit");
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-pending-code-"));
  const cleanEnv = Object.fromEntries(Object.entries(process.env).filter(([key]) => !key.startsWith("GIT_")));
  const env = { ...cleanEnv, ...getGitAuthEnv(token), GIT_CONFIG_NOSYSTEM: "1", GIT_CONFIG_GLOBAL: "/dev/null", GIT_NO_REPLACE_OBJECTS: "1", GIT_NO_LAZY_FETCH: "1" };
  const options = { cwd: directory, env, timeout: 30000, maxBuffer: 80 * 1024 * 1024 };
  const git = args => execLocalGitSync(args, options);
  const execApi = {
    exec: async (command, args) => {
      if (command !== "git") throw new Error("Pending bundle only supports Git");
      git(args);
      return 0;
    },
    getExecOutput: async (command, args) => {
      if (command !== "git") throw new Error("Pending bundle only supports Git");
      return { stdout: git(args), stderr: "", exitCode: 0 };
    },
  };
  try {
    git(["init", "--quiet"]);
    git(["config", "core.hooksPath", "/dev/null"]);
    git(["config", "commit.gpgsign", "false"]);
    git(["config", "user.name", "github-actions[bot]"]);
    git(["config", "user.email", "github-actions[bot]@users.noreply.github.com"]);
    if (localRepository) {
      execGitSync(["-c", "core.hooksPath=/dev/null", "fetch", "--no-tags", localRepository, base], { ...options, env: { ...env, GIT_ALLOW_PROTOCOL: "file" } });
    } else {
      await pendingRead(async () =>
        execGitSync(["-c", "core.hooksPath=/dev/null", "fetch", "--no-tags", "--depth=1", `${server.origin}/${target.repository}.git`, base], { ...options, env: { ...env, GIT_ALLOW_PROTOCOL: "https" }, silent: true })
      );
    }
    git(["checkout", "--quiet", "--detach", base]);
    let candidate = "HEAD";
    if (transport.bundlePath) {
      const source = `refs/heads/${message.branch}`;
      candidate = "refs/bundles/pending-write";
      await importLocalBundle(execApi, transport.bundlePath, source, candidate, options);
      git(["checkout", "--quiet", "--detach", candidate]);
      if (message.type === "create_pull_request" && (config.signed_commits !== false || getExcludedFiles(config).length)) {
        candidate = await linearizeRangeAsCommit(base, "Prepare pending code change", execApi, { gitOpts: options, excludedFiles: getExcludedFiles(config) });
      }
      git(["checkout", "--quiet", "--detach", candidate]);
    } else {
      // The publisher applies this same mail transport with git am --3way.
      git(["am", "--3way", transport.patchPath]);
    }
    const rawFiles = await getBundlePreApplyFiles(execApi, options, base, candidate);
    const diff = git(["diff", "--binary", "--find-renames", `${base}..HEAD`, ...buildExcludePathspecs(getExcludedFiles(config))]);
    const described = describePatchFiles(diff);
    // Only compiler/publisher exclusions may narrow the bundle's net file set.
    if (described.evaluated_paths.some(file => !rawFiles.includes(file))) throw new Error("Pending bundle file inventory disagrees with publisher");
    const files = described.files.map(file => {
      if (file.status === "deleted") {
        return { ...file, content_sha256: null, length: null };
      }
      const bytes = Buffer.from(execLocalGitSync(file.submodule ? ["rev-parse", `HEAD:${file.path}`] : ["show", `HEAD:${file.path}`], { ...options, encoding: "latin1" }), "latin1");
      return { ...file, content_sha256: sha256(bytes), length: bytes.length };
    });
    const treeSha256 = sha256(Buffer.from(execLocalGitSync(["ls-tree", "-r", "-z", "HEAD"], { ...options, encoding: "latin1" }), "latin1"));
    return { base, tree_sha256: treeSha256, ...described, files };
  } finally {
    fs.rmSync(directory, { recursive: true, force: true });
  }
}

async function describeCodeWrite(message, target, config, salt, token, localRepository = undefined, githubClient = globalThis.github) {
  const transport = resolveTransportPaths(message, target.repository);
  if (!transport.patchPath) throw new Error("Pending-write patch is missing");
  const patch = readInput(transport.patchPath);
  let described = describePatchFiles(patch.bytes.toString("utf8"));
  let base = extractPatchBaseCommit(patch.bytes.toString("utf8")) || target.head_commit || null;
  const bindings = [{ kind: "file", path: transport.patchPath, suffix: "patch" }];
  let bundle;
  let objectPack = false;
  if (transport.bundlePath) {
    bundle = readInput(transport.bundlePath);
    bindings.push({ kind: "file", path: transport.bundlePath, suffix: "bundle" });
    objectPack = isObjectPack(bundle.bytes);
  }
  let treeSha256;
  if (!objectPack) {
    const inspected = await inspectTransport(transport, message, target, config, token, localRepository, githubClient);
    base = inspected.base;
    treeSha256 = inspected.tree_sha256;
    described = inspected;
  }
  const code = {
    transport: objectPack ? "object-pack" : bundle ? "bundle" : "patch",
    base_commit: base,
    sha256: patch.sha256,
    length: patch.length,
    patch_rendering: { path: path.basename(transport.patchPath), length: patch.length, sha256: patch.sha256 },
    files: described.files,
    evaluated_paths: described.evaluated_paths,
    additions: described.files.reduce((total, file) => total + file.additions, 0),
    deletions: described.files.reduce((total, file) => total + file.deletions, 0),
    branch_salt: salt || null,
    tree_sha256: treeSha256 || null,
    complete: !objectPack && OID.test(base || "") && described.files.length <= (config.max_patch_files || 1000),
    ...(bundle ? { bundle_sha256: bundle.sha256 } : {}),
  };
  return { code, bindings };
}

module.exports = { describeCodeWrite, inspectTransport };
