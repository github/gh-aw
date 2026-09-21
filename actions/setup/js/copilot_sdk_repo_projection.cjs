// @ts-check

"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { buildExcludePathspecs } = require("./git_patch_utils.cjs");
const { embedBaseCommit } = require("./generate_git_patch.cjs");
const { parseRepositoryChanges, parseRepositoryPaths, MAX_REPOSITORY_FILES } = require("./copilot_sdk_repo_workspace.cjs");
const { repositoryMutationError } = require("./copilot_sdk_repo_diagnostics.cjs");

/** @typedef {ReturnType<typeof import("./copilot_sdk_repo_workspace.cjs").createRepositoryWorkspace>} RepositoryWorkspace */
/** @typedef {{filename: string, copy?: string, mode?: string, blob?: string}} FrozenEntry */
/** @typedef {{tree: string, commit: string, parent: string, entries: FrozenEntry[], patchBytes: number, patchFiles: number}} RepositoryProjection */

const COMMIT_MESSAGE = "Apply reviewed repository changes\n\nCo-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>";

/** @param {string} value */
function requireObjectID(value) {
  const oid = value.trim();
  if (!/^(?:[a-f0-9]{40}|[a-f0-9]{64})$/.test(oid)) throw new Error("Git returned an invalid object ID");
  return oid;
}

/**
 * Create an unreferenced candidate with an isolated index. Its full patch is
 * measured with the same transport framing and unique-file gate as publication.
 *
 * @param {RepositoryWorkspace} workspace
 * @param {{entries: FrozenEntry[]}} frozen
 * @param {{baseline: string, parent: string, staging: string, policy: import("./copilot_sdk_repo_policy.cjs").RepositoryPolicy, signal: AbortSignal}} options
 * @returns {Promise<RepositoryProjection>}
 */
async function stageRepositoryProjection(workspace, frozen, { baseline, parent, staging, policy, signal }) {
  const indexFile = path.join(staging, "index");
  const git = (args, settings = {}) => workspace.run("git", args, signal, { indexFile, ...settings });
  await git(["read-tree", baseline]);
  for (const entry of frozen.entries.filter(entry => !entry.copy)) {
    await git(["update-index", "--force-remove", "--", entry.filename]);
  }
  for (const entry of frozen.entries) {
    if (!entry.copy) continue;
    const blob = requireObjectID((await git(["hash-object", "--no-filters", "-w", "--", entry.copy])).stdout);
    if (blob !== entry.blob) throw new Error("Frozen repository bytes changed before staging");
    let mode = entry.mode;
    if (process.platform === "win32") {
      const previous = (await git(["--literal-pathspecs", "ls-tree", baseline, "--", entry.filename])).stdout;
      if (previous.startsWith("100755 ")) mode = "100755";
    }
    if (!mode) throw new Error("Frozen repository file is missing its mode");
    await git(["update-index", "--add", "--cacheinfo", mode, blob, entry.filename]);
  }
  const tree = requireObjectID((await git(["write-tree"])).stdout);
  // The public index follows the previous commit, not the immutable review
  // baseline. Include reversions that disappeared from the frozen net changes.
  const changes = parseRepositoryChanges((await git(["diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--name-status", "-z", parent, tree, "--"])).stdout);
  /** @type {FrozenEntry[]} */
  const entries = [];
  for (const { filename, deleted } of changes) {
    if (deleted) {
      entries.push({ filename });
      continue;
    }
    const metadata = (await git(["--literal-pathspecs", "ls-tree", "--format=%(objectmode) %(objectname)", tree, "--", filename])).stdout.trim();
    const match = /^(100644|100755) ([a-f0-9]{40}|[a-f0-9]{64})$/.exec(metadata);
    if (!match) throw new Error("Projected repository file metadata is invalid");
    entries.push({ filename, mode: match[1], blob: match[2] });
  }
  const commit = requireObjectID((await git(["commit-tree", tree, "-p", parent, "-m", COMMIT_MESSAGE])).stdout);
  const maxSizeKb = policy.max_patch_size ?? 4096;
  const maxOutputBytes = Math.min((maxSizeKb + 1) * 1024, 32 * 1024 * 1024);
  const rawPatch = (await git(["format-patch", "--stdout", `${baseline}..${commit}`, ...buildExcludePathspecs(policy.excluded_files)], { maxOutputBytes })).stdout;
  const patch = embedBaseCommit(rawPatch, baseline);
  // Loaded only for this opt-in profile; invoking these pure exports performs no
  // API calls or publication and avoids a second interpretation of file limits.
  const { enforcePullRequestLimits, countUniquePatchFiles } = require("./create_pull_request.cjs");
  enforcePullRequestLimits(patch, policy.max_patch_files);
  const patchBytes = Buffer.byteLength(patch, "utf8");
  if (Math.ceil(patchBytes / 1024) > maxSizeKb) throw new Error(`Repository patch exceeds create-pull-request max-patch-size (${maxSizeKb} KB)`);
  return { tree, commit, parent, entries, patchBytes, patchFiles: countUniquePatchFiles(patch) };
}

/**
 * Materialize the exact candidate without the checkout's ignored files, excluded
 * edits, hooks or filter configuration. No clone, fetch or push is involved.
 *
 * @param {RepositoryWorkspace} workspace
 * @param {RepositoryProjection} projection
 * @param {string} directory
 * @param {AbortSignal} signal
 */
async function materializeRepositoryProjection(workspace, projection, directory, signal) {
  const modes = (await workspace.run("git", ["ls-tree", "-r", "--format=%(objectmode)", projection.tree], signal)).stdout.trim().split("\n").filter(Boolean);
  if (modes.some(mode => mode !== "100644" && mode !== "100755")) throw new Error("go-repository validation requires regular files; symlinks and submodules are unsupported");
  const git = args => workspace.run("git", args, signal, { directory });
  await git(["init", `--template=${workspace.hooks}`]);
  fs.mkdirSync(path.join(directory, ".git", "objects", "info"), { recursive: true });
  fs.mkdirSync(path.join(directory, ".git", "info"), { recursive: true });
  fs.writeFileSync(path.join(directory, ".git", "objects", "info", "alternates"), `${path.join(workspace.root, ".git", "objects").replaceAll("\\", "/")}\n`, { flag: "wx" });
  fs.writeFileSync(path.join(directory, ".git", "info", "attributes"), "* -filter -text -ident -working-tree-encoding\n", { flag: "wx" });
  await git(["symbolic-ref", "HEAD", "refs/heads/validation"]);
  await git(["update-ref", "refs/heads/validation", projection.commit]);
  await git(["read-tree", projection.tree]);
  await git(["checkout-index", "--all", "--force"]);
  await verifyRepositoryProjection(workspace, projection, directory, signal);
}

/** @param {RepositoryWorkspace} workspace @param {RepositoryProjection} projection @param {string} directory @param {AbortSignal} signal */
async function verifyRepositoryProjection(workspace, projection, directory, signal) {
  const changed = await workspace.run("git", ["diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--name-only", "-z", projection.tree, "--"], signal, { directory });
  const untracked = await workspace.run("git", ["ls-files", "--others", "-z"], signal, { directory });
  if (changed.stdout || untracked.stdout) {
    let trackedPaths;
    let untrackedPaths;
    try {
      trackedPaths = parseRepositoryPaths(changed.stdout);
      untrackedPaths = parseRepositoryPaths(untracked.stdout);
      if (trackedPaths.length + untrackedPaths.length > MAX_REPOSITORY_FILES) throw new Error("Excessive mutation path list");
    } catch {
      throw new Error("Go validation changed or added files in the projected checkout; path details withheld (unsafe, incomplete, or excessive file list)");
    }
    throw repositoryMutationError(trackedPaths, untrackedPaths, workspace.diagnosticSecrets);
  }
}

module.exports = { requireObjectID, stageRepositoryProjection, materializeRepositoryProjection, verifyRepositoryProjection };
