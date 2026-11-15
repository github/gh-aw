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
const { execLocalGitSync, importLocalBundle, getGitAuthEnv } = require("./git_helpers.cjs");
const { getBundlePreApplyFiles } = require("./push_to_pull_request_branch.cjs");
const { isObjectPack } = require("./object_pack_transport.cjs");
const OID = /^(?:[0-9a-f]{40}|[0-9a-f]{64})$/;

/**
 * Bundle manifests use Git and the publisher's own changed-file helper. Work in
 * an isolated object store; never deepen or mutate the user's checkout.
 */
async function inspectBundle(bundlePath, message, target, config, token) {
  const server = new URL(process.env.GITHUB_SERVER_URL || "https://github.com");
  if (server.protocol !== "https:" || server.username || server.password || server.pathname !== "/" || server.search || server.hash) throw new Error("Pending bundle requires the trusted GitHub server");
  const [owner, repo] = target.repository.split("/");
  let base = target.head_commit;
  if (!base) {
    const { data: branch } = await github.rest.repos.getBranch({ owner, repo, branch: target.base });
    base = branch?.commit?.sha;
  }
  if (!OID.test(base || "")) throw new Error("Pending bundle requires an independently resolved base commit");
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-pending-bundle-"));
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
    git(["init", "--bare", "--quiet"]);
    git(["-c", "core.hooksPath=/dev/null", "fetch", "--no-tags", "--depth=1", `${server.origin}/${target.repository}.git`, base]);
    const source = `refs/heads/${message.branch}`;
    const candidate = "refs/bundles/pending-write";
    await importLocalBundle(execApi, bundlePath, source, candidate, options);
    const rawFiles = await getBundlePreApplyFiles(execApi, options, base, candidate);
    const diff = git(["diff", "--binary", "--find-renames", `${base}..${candidate}`, ...buildExcludePathspecs(getExcludedFiles(config))]);
    const described = describePatchFiles(diff);
    // Only compiler/publisher exclusions may narrow the bundle's net file set.
    if (described.evaluated_paths.some(file => !rawFiles.includes(file))) throw new Error("Pending bundle file inventory disagrees with publisher");
    return { base, ...described };
  } finally {
    fs.rmSync(directory, { recursive: true, force: true });
  }
}

async function describeCodeWrite(message, target, config, salt, token) {
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
    if (!objectPack) {
      const inspected = await inspectBundle(transport.bundlePath, message, target, config, token);
      base = inspected.base;
      described = inspected;
    }
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
    complete: !objectPack && OID.test(base || "") && described.files.length <= (config.max_patch_files || 1000),
    ...(bundle ? { bundle_sha256: bundle.sha256 } : {}),
  };
  return { code, bindings };
}

module.exports = { describeCodeWrite, inspectBundle };
