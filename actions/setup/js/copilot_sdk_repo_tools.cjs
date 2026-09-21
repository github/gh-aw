// @ts-check

"use strict";

const fs = require("node:fs");
const path = require("node:path");
const crypto = require("node:crypto");
const { buildExcludePathspecs } = require("./git_patch_utils.cjs");
const { checkRepositoryPublicationFiles, validateRepositoryBranch } = require("./copilot_sdk_repo_policy.cjs");
const { createRepositoryWorkspace, inspectRepositoryFile, readRepositoryFile, parseRepositoryPaths, parseRepositoryChanges, MAX_REPOSITORY_FILES } = require("./copilot_sdk_repo_workspace.cjs");
const { requireObjectID, stageRepositoryProjection, materializeRepositoryProjection, verifyRepositoryProjection } = require("./copilot_sdk_repo_projection.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { createRepositoryFailure, repositoryCommandError } = require("./copilot_sdk_repo_diagnostics.cjs");

const REPOSITORY_ACTIONS = Object.freeze(["status", "diff", "prepare_branch", "format", "readiness", "validate", "commit"]);
const MAX_SNAPSHOT_BYTES = 8 * 1024 * 1024;

/** @param {unknown} value @returns {{action: string, branch?: string}} */
function parseRepositoryAction(value) {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("go_repository requires an action object");
  if (Object.keys(value).some(key => !["action", "branch"].includes(key)) || !Object.hasOwn(value, "action") || !("action" in value) || typeof value.action !== "string" || !REPOSITORY_ACTIONS.includes(value.action)) {
    throw new Error("go_repository requires one fixed repository action and accepts no command, argv, cwd or environment");
  }
  if (value.action === "prepare_branch") {
    if (!("branch" in value) || typeof value.branch !== "string") throw new Error("prepare_branch requires a branch name");
    return { action: value.action, branch: value.branch };
  }
  if ("branch" in value) throw new Error("Only prepare_branch accepts a branch name");
  return { action: value.action };
}

/** @param {string[]} files */
function batches(files) {
  /** @type {string[][]} */
  const result = [];
  let current = [];
  let bytes = 0;
  for (const filename of files) {
    const size = Buffer.byteLength(filename) + 4;
    if (size > 12_000) throw new Error("Repository path exceeds the bounded argument size");
    if (bytes + size > 12_000) {
      result.push(current);
      current = [];
      bytes = 0;
    }
    current.push(filename);
    bytes += size;
  }
  if (current.length) result.push(current);
  return result;
}

/**
 * The driver owns this runtime and its cancellation scope. The only model input
 * is an action enum and, for prepare_branch, a validated literal branch name.
 *
 * @param {typeof import("@github/copilot-sdk").defineTool} defineTool
 * @param {import("./copilot_sdk_repo_policy.cjs").GoRepositoryProfile} profile
 * @param {Parameters<typeof createRepositoryWorkspace>[1]} [options]
 */
function createCopilotSDKRepositoryRuntime(defineTool, profile, options) {
  if (typeof defineTool !== "function") throw new Error("SDK defineTool is required for go_repository");
  const workspace = createRepositoryWorkspace(profile, options);
  const controller = new AbortController();
  let initialized = false;
  let closed = false;
  let expectedHead = "";
  let baseline = "";
  /** @type {string | null} */
  let expectedRef = null;
  /** @type {string | null} */
  let preparedBranch = null;
  /** @type {(import("./copilot_sdk_repo_projection.cjs").RepositoryProjection & {fingerprint: string}) | null} */
  let validated = null;
  /** @type {import("./copilot_sdk_repo_projection.cjs").RepositoryProjection | null} */
  let pendingIndexSync = null;
  /** @type {string | null} */
  let lastCommit = null;
  /** @type {Promise<string> | null} */
  let active = null;
  /** @type {Promise<void> | null} */
  let initialization = null;
  /** @type {Promise<void> | null} */
  let closing = null;

  /** @param {string[]} args @param {AbortSignal} signal @param {{indexFile?: string, allowFailure?: boolean}} [settings] */
  async function git(args, signal, settings) {
    return workspace.run("git", args, signal, settings);
  }

  /** @param {AbortSignal} signal */
  async function currentRef(signal) {
    const result = await git(["symbolic-ref", "--quiet", "HEAD"], signal, { allowFailure: true });
    if (result.exitCode === 1) return null;
    if (result.exitCode !== 0) throw repositoryCommandError("Cannot resolve checkout branch: git symbolic-ref", result, options?.diagnosticSecrets);
    return result.stdout.trim();
  }

  /** @param {AbortSignal} signal */
  async function checkCheckout(signal) {
    const head = requireObjectID((await git(["rev-parse", "--verify", "HEAD^{commit}"], signal)).stdout);
    if (head !== expectedHead || (await currentRef(signal)) !== expectedRef) throw new Error("Repository HEAD or branch changed outside go_repository");
  }

  /** @param {AbortSignal} signal @param {boolean} [exclude] */
  async function changedFiles(signal, exclude = false) {
    const pathspecs = exclude ? buildExcludePathspecs(profile.policy.excluded_files) : [];
    const tracked = parseRepositoryChanges((await git(["diff", "--no-ext-diff", "--no-textconv", "--name-status", "--no-renames", "-z", baseline, ...pathspecs], signal)).stdout);
    const untracked = parseRepositoryPaths((await git(["ls-files", "--others", "--exclude-standard", "-z", ...pathspecs], signal)).stdout);
    const changes = new Map(tracked.map(change => [change.filename, change]));
    for (const filename of untracked) changes.set(filename, { filename, deleted: false });
    const files = [...changes.values()].sort((left, right) => left.filename.localeCompare(right.filename, "en"));
    if (files.length > MAX_REPOSITORY_FILES) throw new Error(`Repository operation exceeds the ${MAX_REPOSITORY_FILES}-file limit`);
    return files;
  }

  /** @param {AbortSignal} signal */
  async function publicationFiles(signal) {
    const files = await changedFiles(signal, true);
    checkRepositoryPublicationFiles(
      files.map(change => change.filename),
      profile.policy
    );
    return files;
  }

  /**
   * Freeze only publication-eligible bytes for commit. The fingerprint also
   * covers excluded changes so validation cannot be reused after further edits.
   * @param {AbortSignal} signal
   * @param {string} [copyDirectory]
   */
  async function snapshot(signal, copyDirectory) {
    await checkCheckout(signal);
    const changes = await changedFiles(signal);
    const selectedChanges = await publicationFiles(signal);
    const files = changes.map(change => change.filename);
    const selected = selectedChanges.map(change => change.filename);
    if (selectedChanges.some(change => !changes.some(item => item.filename === change.filename && item.deleted === change.deleted))) throw new Error("Repository file list changed during inspection");
    const selectedSet = new Set(selected);
    const digest = crypto.createHash("sha256").update(JSON.stringify({ expectedHead, selected })).update("\n");
    let bytes = 0;
    /** @type {Array<{filename: string, copy?: string, mode?: string, blob?: string}>} */
    const entries = [];
    for (const { filename, deleted } of changes) {
      signal.throwIfAborted();
      if (deleted) {
        digest.update(JSON.stringify([filename, "deleted"])).update("\n");
        if (selectedSet.has(filename)) entries.push({ filename });
        continue;
      }
      const stat = inspectRepositoryFile(workspace.root, filename);
      if (!stat) throw new Error("Repository file disappeared during inspection");
      const mode = stat.mode & 0o111 ? "100755" : "100644";
      const content = readRepositoryFile(workspace.root, filename, stat);
      bytes += content.length;
      if (bytes > MAX_SNAPSHOT_BYTES) throw new Error(`Repository snapshot exceeds the ${MAX_SNAPSHOT_BYTES}-byte operation limit`);
      digest.update(JSON.stringify([filename, mode, content.length, crypto.createHash("sha256").update(content).digest("hex")])).update("\n");
      if (copyDirectory && selectedSet.has(filename)) {
        const copy = path.join(copyDirectory, String(entries.length));
        fs.writeFileSync(copy, content, { flag: "wx", mode: 0o600 });
        const hash = crypto.createHash(expectedHead.length === 64 ? "sha256" : "sha1");
        const blob = hash.update(`blob ${content.length}\0`).update(content).digest("hex");
        entries.push({ filename, copy, mode, blob });
      }
    }
    await checkCheckout(signal);
    return { fingerprint: digest.digest("hex"), files, selected, selectedChanges, entries };
  }

  async function initialize() {
    if (closed || initialized || initialization || controller.signal.aborted) throw new Error("Repository runtime cannot be initialized again");
    initialization = (async () => {
      const signal = AbortSignal.any([controller.signal, AbortSignal.timeout(60_000)]);
      const topLevel = (await git(["rev-parse", "--show-toplevel"], signal)).stdout.trim();
      if (fs.realpathSync(topLevel) !== workspace.root) throw new Error("go_repository checkout differs from GITHUB_WORKSPACE");
      expectedHead = requireObjectID((await git(["rev-parse", "--verify", "HEAD^{commit}"], signal)).stdout);
      if (!workspace.sourceCommit || requireObjectID(workspace.sourceCommit) !== expectedHead) throw new Error("go_repository requires the checkout to match GITHUB_SHA for the publication baseline");
      baseline = expectedHead;
      expectedRef = await currentRef(signal);
      const status = await git(["status", "--porcelain=v1", "--untracked-files=all"], signal);
      if (status.stdout.trim()) throw new Error("go_repository requires a clean checkout before the agent starts; existing changes are left untouched");
      initialized = true;
    })();
    await initialization;
  }

  /** @param {Array<{filename: string, deleted: boolean}>} changes @param {AbortSignal} signal */
  async function formatFiles(changes, signal) {
    const directory = workspace.root;
    const sourceFiles = changes.filter(change => !change.deleted && change.filename.endsWith(".go")).map(change => change.filename);
    for (const filename of sourceFiles) inspectRepositoryFile(directory, filename);
    let output = "";
    for (const group of batches(sourceFiles.map(filename => path.join(directory, filename)))) {
      output += (await workspace.run("gofmt", ["-w", "-l", ...group], signal)).stdout;
    }
    return { checkedGoFiles: sourceFiles, output };
  }

  /** @param {import("./copilot_sdk_repo_projection.cjs").RepositoryProjection} projection @param {AbortSignal} signal */
  async function synchronizeIndex(projection, signal) {
    try {
      for (const entry of projection.entries.filter(entry => !entry.blob)) await git(["update-index", "--force-remove", "--", entry.filename], signal);
      for (const entry of projection.entries.filter(entry => entry.blob)) {
        if (!entry.blob || !entry.mode) throw new Error("Committed file metadata is incomplete");
        await git(["update-index", "--add", "--cacheinfo", entry.mode, entry.blob, entry.filename], signal);
      }
    } catch (error) {
      throw new Error(`Local commit ${projection.commit} exists on ${preparedBranch}, but index synchronization failed. Retry go_repository commit to finish synchronization without creating another commit: ${getErrorMessage(error)}`, {
        cause: error,
      });
    }
    pendingIndexSync = null;
    return {
      branch: preparedBranch,
      commit: projection.commit,
      files: projection.entries.map(entry => entry.filename),
      publication: "Declare the pull request through the native safeoutputs MCP tool; no remote Git operation was performed.",
    };
  }

  /** @param {AbortSignal} signal */
  async function commit(signal) {
    if (!preparedBranch || expectedRef !== `refs/heads/${preparedBranch}`) throw new Error("Prepare an allowed review branch before committing");
    if (pendingIndexSync) return synchronizeIndex(pendingIndexSync, signal);
    const candidate = validated;
    if (!candidate) throw new Error("Run validate successfully before committing");
    const current = await snapshot(signal);
    if (current.fingerprint !== candidate.fingerprint) throw new Error("Repository changes were edited after validation; run validate again");
    const headTree = requireObjectID((await git(["rev-parse", "--verify", "HEAD^{tree}"], signal)).stdout);
    if (candidate.tree === headTree) {
      if (lastCommit === expectedHead) return { branch: preparedBranch, commit: lastCommit, alreadyCommitted: true };
      throw new Error("There are no new publication-eligible changes to commit");
    }
    await git(["update-ref", "--no-deref", `refs/heads/${preparedBranch}`, candidate.commit, candidate.parent], signal);
    expectedHead = candidate.commit;
    lastCommit = candidate.commit;
    pendingIndexSync = candidate;
    validated = null;
    await checkCheckout(signal);
    return synchronizeIndex(candidate, signal);
  }

  /** @param {AbortSignal} signal @param {boolean} full */
  async function validateProjection(signal, full) {
    const { result, candidate } = await workspace.withTemporaryDirectory("validation-", async staging => {
      const frozen = await snapshot(signal, staging);
      const projection = await stageRepositoryProjection(workspace, frozen, { baseline, parent: expectedHead, staging, policy: profile.policy, signal });
      const directory = path.join(staging, "checkout");
      fs.mkdirSync(directory);
      await materializeRepositoryProjection(workspace, projection, directory, signal);
      if (full) {
        const formatted = await workspace.run("gofmt", ["-l", "."], signal, { directory });
        if (formatted.stdout.trim()) throw new Error(`Projected Go files need formatting before validation:\n${formatted.stdout}`);
      }
      const test = await workspace.run("go", full ? ["test", "-count=1", "./..."] : ["test", "-run", "^$", "./..."], signal, { directory });
      await verifyRepositoryProjection(workspace, projection, directory, signal);
      if (!full) return { result: { test, readiness: "Projected repository tests compiled without selecting tests." }, candidate: null };
      const vet = await workspace.run("go", ["vet", "./..."], signal, { directory });
      await verifyRepositoryProjection(workspace, projection, directory, signal);
      const build = await workspace.withTemporaryDirectory("build-", output => workspace.run("go", ["build", "-o", output, "./..."], signal, { directory }));
      await verifyRepositoryProjection(workspace, projection, directory, signal);
      const after = await snapshot(signal);
      if (frozen.fingerprint !== after.fingerprint) throw new Error("Repository changed during validation; rerun validation on the final changes");
      return {
        result: { test, vet, build, validatedFiles: frozen.selected, patchBytes: projection.patchBytes, patchFiles: projection.patchFiles },
        candidate: { ...projection, fingerprint: frozen.fingerprint },
      };
    });
    signal.throwIfAborted();
    if (full) validated = candidate;
    return result;
  }

  /** @param {{action: string, branch?: string}} input @param {AbortSignal} signal */
  async function execute(input, signal) {
    await checkCheckout(signal);
    if (input.action === "status") return { ...(await git(["status", "--short", "--branch", "--untracked-files=all"], signal)), branch: preparedBranch };
    if (input.action === "diff") {
      return { ...(await git(["diff", "--no-ext-diff", "--no-textconv", "--no-renames", baseline, "--"], signal)), changedFiles: (await changedFiles(signal)).map(change => change.filename) };
    }
    if (input.action === "prepare_branch") {
      const branch = input.branch;
      if (!branch) throw new Error("prepare_branch requires a branch");
      validateRepositoryBranch(branch, profile);
      if (preparedBranch) {
        if (preparedBranch !== branch) throw new Error("go_repository already prepared a different review branch");
        return { branch: preparedBranch };
      }
      await git(["check-ref-format", "--branch", branch], signal);
      await git(["switch", "--no-track", "--create", branch], signal);
      preparedBranch = branch;
      expectedRef = `refs/heads/${branch}`;
      return { branch, baseCommit: expectedHead };
    }
    if (input.action === "readiness") return validateProjection(signal, false);
    if (input.action === "format") {
      validated = null;
      return formatFiles(await publicationFiles(signal), signal);
    }
    if (input.action === "validate") {
      return validateProjection(signal, true);
    }
    if (input.action === "commit") return commit(signal);
    throw new Error("Unsupported repository action");
  }

  /** @param {unknown} input @param {import("@github/copilot-sdk").ToolInvocation} invocation */
  async function handler(input, invocation) {
    if (!initialized || closed || controller.signal.aborted) throw new Error("Repository runtime is not active");
    if (active) throw new Error("Run go_repository operations sequentially, not in parallel");
    const parsed = parseRepositoryAction(input);
    const duration = ["readiness", "validate"].includes(parsed.action) ? 10 * 60_000 : 60_000;
    const signals = [controller.signal, AbortSignal.timeout(duration)];
    if (invocation.signal) signals.push(invocation.signal);
    const signal = AbortSignal.any(signals);
    if (parsed.action === "validate") validated = null;
    active = execute(parsed, signal).then(result => JSON.stringify({ action: parsed.action, ...result }));
    try {
      const result = await active;
      signal.throwIfAborted();
      return result;
    } catch (error) {
      if (parsed.action === "validate" || parsed.action === "readiness") validated = null;
      return createRepositoryFailure(error, options?.diagnosticSecrets);
    } finally {
      active = null;
    }
  }

  /** @type {import("@github/copilot-sdk").Tool<unknown>} */
  let tool;
  try {
    tool = defineTool("go_repository", {
      description:
        "Run one fixed Go/Git operation inside the existing AWF sandbox. status/diff inspect local changes; prepare_branch creates one new allowed branch without overwriting an existing branch; format formats changed, publication-eligible Go files; readiness compiles Go tests without selecting tests; validate checks formatting and runs Go tests, vet and build; commit records only the validated, publication-eligible changes locally. Use native view/grep/glob for inspection and native safeoutputs MCP tools for reporting/publication. Run operations sequentially. No shell, executable, argv, cwd or environment input is accepted.",
      parameters: {
        type: "object",
        additionalProperties: false,
        required: ["action"],
        properties: {
          action: { type: "string", enum: [...REPOSITORY_ACTIONS] },
          branch: { type: "string", minLength: 1, maxLength: 200, description: "Required only for prepare_branch. A new branch matching the workflow's allowed-branches." },
        },
      },
      defer: "never",
      handler,
    });
  } catch (error) {
    try {
      workspace.cleanupSync();
    } catch (cleanupError) {
      throw new AggregateError([error, cleanupError], "Repository tool registration and cleanup failed");
    }
    throw error;
  }

  function abort() {
    controller.abort(new Error("Repository runtime stopped"));
  }

  function close() {
    if (closing) return closing;
    closed = true;
    abort();
    closing = (async () => {
      const results = await Promise.allSettled([initialization, active].filter(Boolean));
      const failures = results.filter(result => result.status === "rejected" && result.reason?.cleanupErrors?.length).map(result => (result.status === "rejected" ? result.reason : null));
      try {
        await workspace.cleanup();
      } catch (error) {
        failures.push(error);
      }
      if (failures.length) throw new AggregateError(failures, "Repository runtime cleanup failed");
    })();
    return closing;
  }

  return { tool, initialize, abort, close };
}

module.exports = { REPOSITORY_ACTIONS, MAX_SNAPSHOT_BYTES, parseRepositoryAction, createCopilotSDKRepositoryRuntime };
