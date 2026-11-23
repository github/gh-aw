// @ts-check
/// <reference types="@actions/github-script" />

const fs = require("fs");
const os = require("os");
const path = require("path");
const { createHash } = require("crypto");
const { isUtf8 } = require("buffer");

const { getErrorMessage } = require("./error_helpers.cjs");
const { getGitAuthEnv } = require("./git_auth_helpers.cjs");
const { execGitSync, execGitSyncResult } = require("./git_helpers.cjs");
const { getStagedPatchDiffSizeBytes } = require("./git_patch_utils.cjs");
const { formatJSONFiles, runCustomMemoryValidation } = require("./memory_custom_validation.cjs");
const { compileFileGlobPatterns, isMemoryFileEligible } = require("./memory_file_eligibility.cjs");
const { parseAllowedRepos, validateRepo } = require("./repo_helpers.cjs");
const { pushSignedCommits } = require("./push_signed_commits.cjs");
const { loadTemporaryIdMapFromFile, replaceTemporaryIdReferencesInPatch } = require("./temporary_id.cjs");

// Matches .jsonl in any letter case, like the persistence filters do.
const JSONL_MERGE_ATTRIBUTE = "*.[jJ][sS][oO][nN][lL] merge=union";
const JSONL_PATH_PATTERN = /\.jsonl$/i;

function setOutput(name, value) {
  if (typeof core.setOutput === "function") core.setOutput(name, value);
}

function redactFailureSummary(message) {
  let safeMessage = String(message);
  for (const secret of [process.env.GH_TOKEN, process.env.GITHUB_TOKEN, process.env.GITHUB_APP_TOKEN]) {
    if (secret) {
      safeMessage = safeMessage.split(secret).join("[REDACTED]");
    }
  }
  return safeMessage.replace(/(authorization:\s*(?:bearer|basic)\s+)\S+/gi, "$1[REDACTED]");
}

function getCustomValidationFailureDetail({ stdout, stderr }) {
  return redactFailureSummary([stdout, stderr].filter(Boolean).join("\n"));
}

async function setPushRepoMemoryFailure(message) {
  const safeMessage = redactFailureSummary(message);
  core.info(`Repo-memory push failed: ${safeMessage}`);
  return core.setFailed(safeMessage);
}

/**
 * Exclude agent-supplied files that can mutate trusted ledger state.
 *
 * @param {string} relativePath
 * @param {Set<string>} trustedSegments
 * @returns {boolean}
 */
function isUntrustedLedgerArtifact(relativePath, trustedSegments) {
  const normalizedPath = relativePath.replace(/\\/g, "/");
  if (normalizedPath.startsWith("ledger/coverage/") || normalizedPath.startsWith("ledger/compaction/")) return true;
  const match = /^ledger\/shards\/([0-9a-f-]{36})\.jsonl$/.exec(normalizedPath);
  return Boolean(match && trustedSegments.has(match[1]));
}

/**
 * @param {string} relativePath
 * @returns {boolean}
 */
function isDailyAICLedgerFile(relativePath) {
  return /^daily-aic-ledger\/(?:\d{4}-\d{2}-\d{2}\.jsonl|coverage-[0-9a-f]{64}\.json)$/.test(relativePath.replace(/\\/g, "/"));
}

/**
 * Configure a checkout-local merge policy that keeps both sides of conflicting
 * JSONL regions. The info attributes file is not committed to the memory branch.
 *
 * @param {string} workspaceDir
 */
function configureRepoMemoryMergePolicy(workspaceDir) {
  const gitAttributesPath = execGitSync(["rev-parse", "--git-path", "info/attributes"], {
    cwd: workspaceDir,
    stdio: "pipe",
  }).trim();
  const absoluteAttributesPath = path.isAbsolute(gitAttributesPath) ? gitAttributesPath : path.join(workspaceDir, gitAttributesPath);

  try {
    const existingAttributes = fs.existsSync(absoluteAttributesPath) ? fs.readFileSync(absoluteAttributesPath, "utf8") : "";
    const attributeLines = existingAttributes.split(/\r?\n/);

    if (attributeLines.includes(JSONL_MERGE_ATTRIBUTE)) {
      return;
    }

    fs.mkdirSync(path.dirname(absoluteAttributesPath), { recursive: true });
    const separator = existingAttributes.length > 0 && !existingAttributes.endsWith("\n") ? "\n" : "";
    fs.appendFileSync(absoluteAttributesPath, `${separator}${JSONL_MERGE_ATTRIBUTE}\n`, "utf8");
  } catch (error) {
    throw new Error(`Failed to configure repo-memory merge attributes: ${getErrorMessage(error)}`, { cause: error });
  }
}

/**
 * Deterministic validation errors come from static commit shape or policy
 * checks in pushSignedCommits. Retrying them with the same local commits cannot
 * make them pass, unlike transient API failures or stale remote heads.
 *
 * @param {string} errorMessage
 * @returns {boolean}
 */
function isDeterministicPushValidationError(errorMessage) {
  return /^ERR_VALIDATION\b/.test(errorMessage);
}

/**
 * Rebase this run's repo-memory commit(s) onto a refreshed remote branch head
 * without creating a merge commit. JSONL conflicts keep both rows via the
 * checkout-local merge=union policy; non-JSONL conflicts keep this run's files.
 *
 * @param {{workspaceDir: string, branchName: string, repoUrl: string, previousBaseRef: string, remoteHead: string, gitAuthEnv?: Record<string, string>}} opts
 */
function reconcileRepoMemoryRetry({ workspaceDir, branchName, repoUrl, previousBaseRef, remoteHead, gitAuthEnv }) {
  try {
    configureRepoMemoryMergePolicy(workspaceDir);
  } catch (mergePolicyError) {
    core.warning(`Failed to configure JSONL union-merge policy; concurrent JSONL rows may be lost on conflict: ${getErrorMessage(mergePolicyError)}`);
  }

  const gitEnv = { ...process.env, ...(gitAuthEnv || {}) };
  execGitSync(["fetch", repoUrl, `refs/heads/${branchName}`], { cwd: workspaceDir, env: gitEnv, stdio: "pipe", suppressLogs: true });

  const rebaseArgs = previousBaseRef ? ["rebase", "-X", "theirs", "--onto", remoteHead, previousBaseRef] : ["rebase", "-X", "theirs", "--onto", remoteHead, "--root"];
  try {
    execGitSync(rebaseArgs, { cwd: workspaceDir, env: gitEnv, stdio: "inherit", suppressLogs: true });
  } catch (rebaseError) {
    try {
      execGitSync(["rebase", "--abort"], { cwd: workspaceDir, env: gitEnv, stdio: "pipe", suppressLogs: true });
    } catch {
      // Ignore cleanup failures; surface the original rebase error below.
    }
    throw rebaseError;
  }
}

const COMMIT_ID_PATTERN = /^(?:[0-9a-f]{40}|[0-9a-f]{64})$/;
// The clone step reports this when the branch did not exist, so the agent started from an empty tree.
const EMPTY_START = "empty";

/**
 * Compute the Git blob ID of file content without spawning git.
 *
 * @param {Buffer} content
 * @param {string} objectId - Any object ID from the same repository; its length selects SHA-1 or SHA-256.
 * @returns {string}
 */
function gitBlobId(content, objectId) {
  return createHash(objectId.length === 64 ? "sha256" : "sha1")
    .update(`blob ${content.length}\0`)
    .update(content)
    .digest("hex");
}

/**
 * Map each regular file in a commit to its blob ID.
 *
 * @param {string} commit
 * @param {string} workspaceDir
 * @returns {Map<string, string>}
 */
function listCommitBlobs(commit, workspaceDir) {
  /** @type {Map<string, string>} */
  const blobs = new Map();
  const listing = execGitSync(["ls-tree", "-r", "-z", commit], { cwd: workspaceDir, stdio: "pipe" });
  for (const entry of listing.split("\0")) {
    const match = /^100(?:644|755) blob ([0-9a-f]+)\t([\s\S]+)$/.exec(entry);
    if (match) blobs.set(match[2], match[1]);
  }
  return blobs;
}

/**
 * @param {string} blobId
 * @param {string} workspaceDir
 * @returns {Buffer}
 */
function readBlob(blobId, workspaceDir) {
  const result = execGitSyncResult(["cat-file", "blob", blobId], { cwd: workspaceDir, encoding: "buffer" });
  if (result.status !== 0 || !Buffer.isBuffer(result.stdout)) {
    throw new Error(`Failed to read blob ${blobId}: ${String(result.stderr || "").trim()}`);
  }
  return result.stdout;
}

/**
 * @param {string} filePath
 * @returns {Buffer}
 */
function readMemoryFile(filePath) {
  try {
    return fs.readFileSync(filePath);
  } catch (error) {
    throw new Error(`Failed to read memory file ${filePath}: ${getErrorMessage(error)}`, { cause: error });
  }
}

/**
 * Convert content to the form Git stores at this path, so that checkout
 * conversions such as eol attributes are not mistaken for this run's edits.
 *
 * @param {string} treePath
 * @param {Buffer} content
 * @param {string} workspaceDir
 * @returns {{blobId: string, content: Buffer}}
 */
function canonicalMemoryContent(treePath, content, workspaceDir) {
  const result = execGitSyncResult(["hash-object", "-w", "--path", treePath, "--stdin"], { cwd: workspaceDir, input: content });
  const blobId = String(result.stdout || "").trim();
  if (result.status !== 0 || !COMMIT_ID_PATTERN.test(blobId)) {
    throw new Error(`Failed to normalize ${treePath}: ${String(result.stderr || "").trim()}`);
  }
  return { blobId, content: readBlob(blobId, workspaceDir) };
}

/**
 * The bytes Git would check out for stored content at this path, so that
 * staging the file applies its attributes exactly once.
 *
 * @param {string} treePath
 * @param {Buffer} content - Content as Git stores it.
 * @param {string} workspaceDir
 * @returns {Buffer}
 */
function checkoutMemoryContent(treePath, content, workspaceDir) {
  const stored = execGitSyncResult(["hash-object", "-w", "--no-filters", "--stdin"], { cwd: workspaceDir, input: content });
  const blobId = String(stored.stdout || "").trim();
  if (stored.status !== 0 || !COMMIT_ID_PATTERN.test(blobId)) {
    throw new Error(`Failed to store ${treePath}: ${String(stored.stderr || "").trim()}`);
  }
  const result = execGitSyncResult(["cat-file", "--filters", `--path=${treePath}`, blobId], { cwd: workspaceDir, encoding: "buffer" });
  if (result.status !== 0 || !Buffer.isBuffer(result.stdout)) {
    throw new Error(`Failed to convert ${treePath} for the working tree: ${String(result.stderr || "").trim()}`);
  }
  return result.stdout;
}

/**
 * Three-way merge of one memory file changed both by this run and by another
 * run. JSONL keeps the rows of both sides, like the merge=union policy of the
 * retry path; other files keep this run's lines where the two sides conflict.
 *
 * @param {{relativePath: string, latest: Buffer, base: Buffer, local: Buffer, workspaceDir: string}} opts
 * @returns {Buffer | null} The merged content, or null when it is not text Git can merge.
 */
function mergeRepoMemoryFile({ relativePath, latest, base, local, workspaceDir }) {
  if ([latest, base, local].some(content => content.includes(0) || !isUtf8(content))) return null;
  /** @type {string} */
  let tempDir;
  try {
    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "repo-memory-merge-"));
  } catch (error) {
    throw new Error(`Failed to create a temporary directory to merge ${relativePath}: ${getErrorMessage(error)}`, { cause: error });
  }
  try {
    const [latestPath, basePath, localPath] = ["latest", "base", "local"].map(name => path.join(tempDir, name));
    try {
      fs.writeFileSync(latestPath, latest);
      fs.writeFileSync(basePath, base);
      fs.writeFileSync(localPath, local);
    } catch (error) {
      throw new Error(`Failed to stage ${relativePath} for merging: ${getErrorMessage(error)}`, { cause: error });
    }
    const favor = JSONL_PATH_PATTERN.test(relativePath) ? "--union" : "--theirs";
    const result = execGitSyncResult(["merge-file", "-p", favor, latestPath, basePath, localPath], { cwd: workspaceDir, encoding: "buffer" });
    // Text input always merges with a favor option, so any failure must stop the save.
    if (result.status !== 0 || !Buffer.isBuffer(result.stdout)) {
      throw new Error(`Failed to merge ${relativePath}: ${String(result.stderr || "").trim() || `git merge-file exited with status ${result.status}`}`);
    }
    return result.stdout;
  } finally {
    try {
      fs.rmSync(tempDir, { recursive: true, force: true });
    } catch (error) {
      core.info(`Could not remove temporary merge directory ${tempDir}: ${getErrorMessage(error)}`);
    }
  }
}

/**
 * Plan how to write each artifact file so that the save carries only this
 * run's changes, measured from the commit the agent job cloned (or an empty
 * tree when the branch did not exist yet) and replayed onto the checked-out
 * branch head:
 *   - unchanged by this run: keep the head's version, or skip the file if
 *     another run deleted it;
 *   - changed only by this run: write this run's version;
 *   - changed on both sides: write a three-way merge (mergeRepoMemoryFile).
 * Returns steps: null when the starting commit is unknown or no longer in the
 * branch history; the caller then copies this run's files as before.
 *
 * @param {{files: Array<{relativePath: string, source: string}>, baseCommit: string | undefined, workspaceDir: string}} opts
 * @returns {{steps: Map<string, {action: "keep" | "skip"} | {action: "write", content: Buffer}> | null, reason: string}}
 */
function planRepoMemoryReplay({ files, baseCommit, workspaceDir }) {
  const commit = String(baseCommit || "")
    .trim()
    .toLowerCase();
  if (!commit) return { steps: null, reason: "the agent job did not record the commit it started from" };
  if (commit !== EMPTY_START) {
    if (!COMMIT_ID_PATTERN.test(commit)) return { steps: null, reason: "the recorded starting commit is not a commit ID" };
    const notInHistory = { steps: null, reason: `the starting commit ${commit} is no longer in the branch history` };
    const present = execGitSyncResult(["cat-file", "-e", `${commit}^{commit}`], { cwd: workspaceDir });
    if (present.status === null) throw new Error(`Failed to look up the starting commit ${commit}: ${getErrorMessage(present.error)}`);
    if (present.status !== 0) return notInHistory;
    // Only a definite answer may fall back to copying; anything else stops the save.
    const ancestry = execGitSyncResult(["merge-base", "--is-ancestor", commit, "HEAD"], { cwd: workspaceDir });
    if (ancestry.status === 1) return notInHistory;
    if (ancestry.status !== 0) throw new Error(`Failed to check that ${commit} is in the branch history: ${String(ancestry.stderr || "").trim() || getErrorMessage(ancestry.error)}`);
  }

  const head = execGitSync(["rev-parse", "HEAD"], { cwd: workspaceDir, stdio: "pipe" }).trim();
  /** @type {Map<string, string>} */
  const baseBlobs = commit === EMPTY_START ? new Map() : listCommitBlobs(commit, workspaceDir);
  const headBlobs = listCommitBlobs(head, workspaceDir);
  const hasAttributes = [...baseBlobs.keys(), ...headBlobs.keys()].some(treePath => path.posix.basename(treePath) === ".gitattributes");
  /** @param {string} treePath @param {Buffer} content */
  const blobIdOf = (treePath, content) => (hasAttributes ? canonicalMemoryContent(treePath, content, workspaceDir) : { blobId: gitBlobId(content, head), content });
  /** @type {Map<string, {action: "keep" | "skip"} | {action: "write", content: Buffer}>} */
  const steps = new Map();
  for (const file of files) {
    const treePath = file.relativePath.replace(/\\/g, "/");
    // Written files are staged again later, so they must stay in working-tree form.
    const raw = readMemoryFile(file.source);
    const { blobId: localBlob, content: local } = blobIdOf(treePath, raw);
    /** @param {Buffer} stored */
    const forCheckout = stored => (hasAttributes ? checkoutMemoryContent(treePath, stored, workspaceDir) : stored);
    const baseBlob = baseBlobs.get(treePath);
    const headBlob = headBlobs.get(treePath);

    // Unchanged by this run, or the head already has this run's version.
    if (localBlob === baseBlob || localBlob === headBlob) {
      if (!headBlob) {
        steps.set(file.relativePath, { action: "skip" });
        continue;
      }
      // Sparse checkouts may not materialize the head's version in the working tree.
      const destPath = path.join(workspaceDir, file.relativePath);
      const inPlace = fs.existsSync(destPath) && blobIdOf(treePath, readMemoryFile(destPath)).blobId === headBlob;
      steps.set(file.relativePath, inPlace ? { action: "keep" } : { action: "write", content: forCheckout(readBlob(headBlob, workspaceDir)) });
      continue;
    }
    if (!headBlob || headBlob === baseBlob) {
      steps.set(file.relativePath, { action: "write", content: raw });
      continue;
    }

    const merged = mergeRepoMemoryFile({
      relativePath: treePath,
      latest: readBlob(headBlob, workspaceDir),
      base: baseBlob ? readBlob(baseBlob, workspaceDir) : Buffer.alloc(0),
      local,
      workspaceDir,
    });
    if (merged) {
      core.info(`Merged ${treePath} with changes another run saved after this run started`);
      steps.set(file.relativePath, { action: "write", content: forCheckout(merged) });
    } else {
      core.info(`Cannot merge ${treePath} as text; keeping this run's version`);
      steps.set(file.relativePath, { action: "write", content: raw });
    }
  }
  return { steps, reason: "" };
}

/**
 * Push repo-memory changes with optimistic retries after stale-head failures.
 *
 * @param {object} opts
 * @param {any} opts.githubClient
 * @param {string} opts.targetOwner
 * @param {string} opts.targetRepoName
 * @param {string} opts.targetRepo
 * @param {string} opts.branchName
 * @param {string} opts.baseRef
 * @param {string} opts.workspaceDir
 * @param {string} opts.ghToken
 * @param {string} opts.serverHost
 * @param {typeof pushSignedCommits} [opts.pushSignedCommitsFn]
 * @param {typeof exec.getExecOutput} [opts.execGetExecOutput]
 * @param {(delay: number) => Promise<void>} [opts.sleepFn]
 * @param {string} [opts.originUrlForPush]
 * @param {string} [opts.repoUrlForRetry]
 * @param {() => void} [opts.validateBeforePush]
 * @param {(context: {remoteHead: string}) => (boolean | Promise<boolean>)} [opts.reapply] For gh-aw's own state writers, not agent memory:
 *   when the branch head moved, check out the newest head and call this to rebuild and commit the change from the newest files,
 *   instead of rebasing with this run's copy winning. It returns false when there is nothing left to commit.
 * @returns {Promise<string | undefined | false>} Remote commit, unchanged replay, or failure.
 */
async function pushRepoMemoryChangesWithRetry({
  githubClient,
  targetOwner,
  targetRepoName,
  targetRepo,
  branchName,
  baseRef,
  workspaceDir,
  ghToken,
  serverHost,
  pushSignedCommitsFn = pushSignedCommits,
  execGetExecOutput = exec.getExecOutput,
  sleepFn = delay => new Promise(resolve => setTimeout(resolve, delay)),
  originUrlForPush,
  repoUrlForRetry,
  validateBeforePush,
  reapply,
}) {
  const retryUrl = repoUrlForRetry || `https://${serverHost}/${targetRepo}.git`;

  // Point origin at the memory target repo so pushSignedCommits can resolve
  // the remote branch HEAD (ls-remote origin) and the git-push fallback
  // pushes to the correct repository.
  execGitSync(["remote", "set-url", "origin", originUrlForPush || `https://${serverHost}/${targetRepo}.git`], { cwd: workspaceDir, stdio: "pipe" });

  // Pushes to a memory branch are not serialised by a job-level concurrency group
  // (GitHub Actions would cancel all but one pending job under fan-out), so the retry
  // loop is what makes concurrent writers converge. Full-jitter exponential backoff
  // spreads out the retries of many runs that finish at the same time.
  const MAX_RETRIES = 10;
  const BASE_DELAY_MS = 1000;
  const MAX_DELAY_MS = 20000;
  let currentBaseRef = baseRef;

  for (let attempt = 0; attempt <= MAX_RETRIES; attempt++) {
    core.info(`Pushing changes to ${branchName} (attempt ${attempt + 1}/${MAX_RETRIES + 1})...`);
    // A typed ledger must revalidate the merged history after a concurrent
    // writer moves the branch; validation errors must not enter the retry loop.
    if (validateBeforePush) {
      try {
        validateBeforePush();
      } catch (error) {
        await setPushRepoMemoryFailure(`ERR_VALIDATION: Repo-memory candidate validation failed before push: ${getErrorMessage(error)}`);
        return false;
      }
    }
    try {
      const commit = await pushSignedCommitsFn({
        githubClient,
        owner: targetOwner,
        repo: targetRepoName,
        branch: branchName,
        baseRef: currentBaseRef,
        cwd: workspaceDir,
        gitAuthEnv: getGitAuthEnv(ghToken),
        allowGitPushFallback: !validateBeforePush && !reapply,
        // Commit only on the head this save was built on; a moved branch is
        // rebased below with the JSONL union policy, or re-applied, and retried.
        requireBaseRefMatch: true,
      });
      core.info(commit ? `Successfully pushed changes to ${branchName} branch` : `No changes left to push to ${branchName} branch`);
      return commit;
    } catch (error) {
      const errMsg = getErrorMessage(error);
      if (isDeterministicPushValidationError(errMsg)) {
        await setPushRepoMemoryFailure(`Failed to push changes: ${errMsg}`);
        return false;
      }
      if (attempt < MAX_RETRIES) {
        const ceiling = Math.min(MAX_DELAY_MS, BASE_DELAY_MS * Math.pow(2, attempt));
        const delay = Math.floor(Math.random() * ceiling) + 1;
        core.warning(`Push failed (attempt ${attempt + 1}/${MAX_RETRIES + 1}), retrying in ${delay}ms: ${errMsg}`);
        await sleepFn(delay);

        // Refresh currentBaseRef and replay our commit(s) onto the latest remote
        // head before retrying. This keeps the local history linear for
        // pushSignedCommits while still preserving concurrent JSONL rows via the
        // checkout-local merge=union policy.
        try {
          const { stdout: lsOut } = await execGetExecOutput("git", ["ls-remote", retryUrl, `refs/heads/${branchName}`], {
            cwd: workspaceDir,
            env: getGitAuthEnv(ghToken),
            silent: true,
          });
          const remoteHead = lsOut.trim().split(/\s+/)[0] || "";
          if (remoteHead && remoteHead !== currentBaseRef) {
            const previousBaseRef = currentBaseRef;
            currentBaseRef = remoteHead;
            core.info(`Refreshed baseRef for retry: ${currentBaseRef}`);
            try {
              if (reapply) {
                // A clean textual merge of a derived state file can still be wrong, so rebuild
                // the change from the newest files whether or not the files conflict.
                execGitSync(["fetch", retryUrl, `refs/heads/${branchName}`], { cwd: workspaceDir, env: { ...process.env, ...getGitAuthEnv(ghToken) }, stdio: "pipe", suppressLogs: true });
                execGitSync(["reset", "--hard", remoteHead], { cwd: workspaceDir, stdio: "pipe" });
                if (!(await reapply({ remoteHead }))) {
                  core.info(`No changes left to push to ${branchName} branch`);
                  return undefined;
                }
              } else {
                reconcileRepoMemoryRetry({
                  workspaceDir,
                  branchName,
                  repoUrl: retryUrl,
                  previousBaseRef,
                  remoteHead,
                  gitAuthEnv: getGitAuthEnv(ghToken),
                });
              }
            } catch (reconcileError) {
              await setPushRepoMemoryFailure(`Failed to reconcile repo-memory changes onto refreshed head before retry: ${getErrorMessage(reconcileError)}`);
              return false;
            }
          }
        } catch (lsRemoteError) {
          // ls-remote failed; proceed with existing currentBaseRef
          core.info(`ls-remote on retry failed, keeping existing baseRef: ${getErrorMessage(lsRemoteError)}`);
        }
      } else {
        // Surface a helpful message when the repository's signed-commits
        // ruleset rejects the git-push fallback path.
        if (/GH013|must have verified signatures|Commits must have verified signatures/i.test(errMsg)) {
          await setPushRepoMemoryFailure(
            `repo-memory: push to branch ${branchName} was rejected because the repository requires verified (signed) commits. ` +
              `Commits pushed via the GitHub GraphQL API are signed automatically, but the signed-commit path could not be used for this push. ` +
              `If your memory files contain symlinks, executable files, or submodule references, remove them and use regular plain-text files (.json, .jsonl, .txt, .md, .csv). ` +
              `Original error: ${errMsg}`
          );
        } else {
          await setPushRepoMemoryFailure(`Failed to push changes after ${MAX_RETRIES + 1} attempts: ${errMsg}`);
        }
        return false;
      }
    }
  }
  return false;
}

/**
 * Apply the final safe-output temporary ID map to memory files before persistence.
 *
 * @param {Array<{relativePath: string}>} files
 * @param {string} memoryDir
 * @param {Map<string, {repo: string, number: number}>} temporaryIdMap
 * @param {string} currentRepo
 * @param {number} maxFileSize
 * @returns {string[]} relative paths whose contents were updated
 */
function applyTemporaryIdSubstitutions(files, memoryDir, temporaryIdMap, currentRepo, maxFileSize) {
  if (temporaryIdMap.size === 0) {
    return [];
  }

  const updatedFiles = [];
  for (const file of files) {
    const filePath = path.join(memoryDir, file.relativePath);
    let buffer;
    try {
      buffer = fs.readFileSync(filePath);
    } catch (error) {
      throw new Error(`Failed to read memory file ${file.relativePath}: ${getErrorMessage(error)}`, { cause: error });
    }
    // NUL is valid UTF-8 but indicates binary content for repo-memory files.
    if (!isUtf8(buffer) || buffer.includes(0)) {
      continue;
    }
    const content = buffer.toString("utf8");
    const updatedContent = replaceTemporaryIdReferencesInPatch(content, temporaryIdMap, currentRepo);
    if (updatedContent !== content) {
      const updatedSize = Buffer.byteLength(updatedContent, "utf8");
      if (updatedSize > maxFileSize) {
        throw new Error(`Rewritten memory file ${file.relativePath} exceeds size limit (${updatedSize} bytes > ${maxFileSize} bytes)`);
      }
      try {
        fs.writeFileSync(filePath, updatedContent, "utf8");
      } catch (error) {
        throw new Error(`Failed to write memory file ${file.relativePath}: ${getErrorMessage(error)}`, { cause: error });
      }
      core.info(`Rewrote repo-memory file after temporary ID substitution: ${file.relativePath}`);
      updatedFiles.push(file.relativePath);
    }
  }
  return updatedFiles;
}

/**
 * Push repo-memory changes to git branch
 * Environment variables:
 *   ARTIFACT_DIR: Path to the downloaded artifact directory containing memory files
 *   MEMORY_ID: Memory identifier (used for subdirectory path)
 *   TARGET_REPO: Target repository (owner/name)
 *   BRANCH_NAME: Branch name to push to
 *   MAX_FILE_SIZE: Maximum file size in bytes
 *   MAX_FILE_COUNT: Maximum number of files per commit
 *   MAX_PATCH_SIZE: Maximum total patch size in bytes (default: 10240 = 10KB)
 *   ALLOWED_EXTENSIONS: JSON array of allowed file extensions (e.g., '[".json",".txt"]')
 *   FILE_GLOB_FILTER: Optional space-separated list of file patterns (e.g., "*.md metrics/** data/**")
 *                     Supports * (matches any chars except /) and ** (matches any chars including /)
 *
 *                     IMPORTANT: Patterns are matched against the RELATIVE FILE PATH from the artifact directory,
 *                     NOT against the branch path. Do NOT include the branch name in the patterns.
 *
 *                     Example:
 *                       BRANCH_NAME: memory/code-metrics
 *                       Artifact file: /tmp/gh-aw/repo-memory/default/history.jsonl
 *                       Relative path tested: "history.jsonl"
 *                       CORRECT pattern: "*.jsonl"
 *                       INCORRECT pattern: "memory/code-metrics/*.jsonl"  (includes branch name)
 *
 *                     The branch name is used for git operations (checkout, push) but not for pattern matching.
 *   GH_TOKEN: GitHub token for authentication
 *   GITHUB_RUN_ID: Workflow run ID for commit messages
 *   REPO_MEMORY_BASE_COMMIT: Commit the agent job cloned, or "empty" when the branch did not exist yet. Files are
 *                     compared with it so that only this run's changes are replayed onto the latest branch;
 *                     an unset value falls back to copying files.
 */

async function main() {
  setOutput("save_status", "skipped");
  const artifactDir = process.env.ARTIFACT_DIR;
  const memoryId = process.env.MEMORY_ID;
  const targetRepo = process.env.TARGET_REPO;
  const branchName = process.env.BRANCH_NAME;
  const maxFileSize = Number(process.env.MAX_FILE_SIZE || "10240");
  const maxFileCount = Number(process.env.MAX_FILE_COUNT || "100");
  const maxPatchSize = Number(process.env.MAX_PATCH_SIZE || "10240");
  const fileGlobFilter = process.env.FILE_GLOB_FILTER || "";
  const hasTrustedDailyAICLedger = process.env.GH_AW_DAILY_AIC_REPO_MEMORY_WRITER === "true";
  const formatJSON = process.env.FORMAT_JSON === "true";
  const validationScriptBase64 = process.env.VALIDATION_SCRIPT_B64 || "";
  const validationScriptRequired = process.env.VALIDATION_SCRIPT_REQUIRED === "true";
  const jsonSchemasRequired = process.env.MEMORY_JSON_SCHEMAS_REQUIRED === "true";
  let jsonSchemas;
  const validationTimeoutSeconds = Number(process.env.VALIDATION_TIMEOUT_SECONDS || "60");
  /** @type {{normalized: string[], saving: {changedFiles: number, patchBytes: number, pushed: boolean}}} */
  const ledgerActivity = {
    normalized: [],
    saving: { changedFiles: 0, patchBytes: 0, pushed: false },
  };
  const writeLedgerSummary = async () => {
    if (!formatJSON && ledgerActivity.normalized.length === 0) return;
    const lines = ["<details>", "<summary>Repo-memory ledger activity</summary>", "", "### Normalizing"];
    lines.push(ledgerActivity.normalized.length ? `- Formatted ${ledgerActivity.normalized.length} JSON file(s).` : "- No JSON normalization requested.");
    lines.push("", "### Saving");
    lines.push(`- ${ledgerActivity.saving.changedFiles} managed file(s) changed; ${ledgerActivity.saving.patchBytes} staged patch byte(s).`, `- ${ledgerActivity.saving.pushed ? "Changes pushed." : "No changes pushed."}`, "", "</details>");
    if (core.summary && typeof core.summary.addRaw === "function" && typeof core.summary.write === "function") {
      await core.summary.addRaw(lines.join("\n")).write();
    }
  };
  if (
    !Number.isFinite(maxFileSize) ||
    !Number.isSafeInteger(maxFileSize) ||
    maxFileSize <= 0 ||
    !Number.isFinite(maxFileCount) ||
    !Number.isSafeInteger(maxFileCount) ||
    maxFileCount <= 0 ||
    !Number.isFinite(maxPatchSize) ||
    !Number.isSafeInteger(maxPatchSize) ||
    maxPatchSize <= 0 ||
    !Number.isFinite(validationTimeoutSeconds) ||
    !Number.isSafeInteger(validationTimeoutSeconds) ||
    validationTimeoutSeconds <= 0
  ) {
    await setPushRepoMemoryFailure("Memory size, count, patch size, and validation timeout limits must be positive integers");
    return;
  }

  // Parse allowed extensions with error handling
  let allowedExtensions = [".json", ".jsonl", ".txt", ".md", ".csv"];
  if (process.env.ALLOWED_EXTENSIONS) {
    try {
      allowedExtensions = JSON.parse(process.env.ALLOWED_EXTENSIONS);
    } catch (/** @type {any} */ error) {
      await setPushRepoMemoryFailure(`Failed to parse ALLOWED_EXTENSIONS environment variable: ${getErrorMessage(error)}. Expected JSON array format.`);
      return;
    }
  }

  try {
    if (process.env.MEMORY_JSON_SCHEMAS_B64) {
      const encoded = process.env.MEMORY_JSON_SCHEMAS_B64;
      const decoded = Buffer.from(encoded, "base64");
      if (decoded.toString("base64") !== encoded) throw new TypeError("MEMORY_JSON_SCHEMAS_B64 is not valid base64");
      jsonSchemas = JSON.parse(decoded.toString("utf8"));
      if (!Array.isArray(jsonSchemas) || jsonSchemas.length === 0) throw new TypeError("json-schemas must be a non-empty array");
    } else if (jsonSchemasRequired) {
      throw new TypeError("MEMORY_JSON_SCHEMAS_B64 is missing");
    }
    if (validationScriptRequired && !validationScriptBase64) throw new TypeError("VALIDATION_SCRIPT_B64 is missing");
  } catch (error) {
    await setPushRepoMemoryFailure(`Memory validation configuration is invalid: ${getErrorMessage(error)}`);
    return;
  }

  const ghToken = process.env.GH_TOKEN;
  const githubRunId = process.env.GITHUB_RUN_ID || "unknown";
  const githubServerUrl = process.env.GITHUB_SERVER_URL || "https://github.com";
  const serverHost = githubServerUrl.replace(/^https?:\/\//, "");
  const temporaryIdMap = loadTemporaryIdMapFromFile(process.env.GH_AW_TEMPORARY_ID_MAP_FILE ?? "");

  // Log environment variable configuration for debugging
  core.info("Environment configuration:");
  core.info(`  MEMORY_ID: ${memoryId}`);
  core.info(`  MAX_FILE_SIZE: ${maxFileSize}`);
  core.info(`  MAX_FILE_COUNT: ${maxFileCount}`);
  core.info(`  MAX_PATCH_SIZE: ${maxPatchSize}`);
  core.info(`  ALLOWED_EXTENSIONS: ${JSON.stringify(allowedExtensions)}`);
  core.info(`  FILE_GLOB_FILTER: ${fileGlobFilter ? `"${fileGlobFilter}"` : "(empty - all files accepted)"}`);
  core.info(`  FILE_GLOB_FILTER length: ${fileGlobFilter.length}`);
  core.info(`  FORMAT_JSON: ${formatJSON}`);
  core.info(`  TEMPORARY_ID_MAPPINGS: ${temporaryIdMap.size}`);

  /** @param {unknown} value */
  function isPlainObject(value) {
    return typeof value === "object" && value !== null && !Array.isArray(value);
  }

  /** @param {string} absPath */
  function tryParseJSONFile(absPath) {
    let raw;
    try {
      raw = fs.readFileSync(absPath, "utf8");
    } catch (err) {
      throw new Error(`Failed to read file ${absPath}: ${getErrorMessage(err)}`, { cause: err });
    }
    if (!raw.trim()) {
      throw new Error(`Empty JSON file: ${absPath}`);
    }
    try {
      return JSON.parse(raw);
    } catch (e) {
      throw new Error(`Invalid JSON in ${absPath}: ${getErrorMessage(e)}`, { cause: e });
    }
  }

  // Validate required environment variables
  if (!artifactDir || !memoryId || !targetRepo || !branchName || !ghToken) {
    await setPushRepoMemoryFailure("Missing required environment variables: ARTIFACT_DIR, MEMORY_ID, TARGET_REPO, BRANCH_NAME, GH_TOKEN");
    return;
  }

  // Validate branch name against the naming constraints enforced by the compiler:
  //
  //  Non-wiki memory (default): "memory/{id}" or "{custom-prefix}/{id}"
  //    - The branch prefix is validated at compile time: 4-32 alphanumeric/hyphen/underscore chars
  //    - generateDefaultBranchName() always produces "{prefix}/{id}" with a "/" separator
  //  Wiki memory: bare branch name, typically "master" or "main"
  //    - Wikis use the repository's default branch and never create an orphan
  //    - The target repo is already appended with ".wiki" by the compiler
  //
  // At runtime we enforce:
  //   1. Namespaced branches (contain "/") to prevent pushing to top-level branches like "main"
  //   2. Known wiki branch names ("master", "main", "gh-pages") are only valid when
  //      TARGET_REPO ends with ".wiki" – the compiler always appends ".wiki" for wiki memory.
  const isNamespaced = /^[a-zA-Z0-9_-]+\/.+/.test(branchName);
  const isKnownWikiBranch = branchName === "master" || branchName === "main" || branchName === "gh-pages";
  const isWikiRepo = targetRepo.endsWith(".wiki");
  if (!isNamespaced && !isKnownWikiBranch) {
    await setPushRepoMemoryFailure(`ERR_VALIDATION: Invalid branch name "${branchName}": branch name must be namespaced (e.g. "memory/default") or a known wiki branch ("master", "main", "gh-pages")`);
    return;
  }
  if (isKnownWikiBranch && !isWikiRepo) {
    await setPushRepoMemoryFailure(`ERR_VALIDATION: Branch name "${branchName}" is only valid for wiki repositories (TARGET_REPO must end with ".wiki", got "${targetRepo}")`);
    return;
  }

  // Validate target repository against allowlist
  const allowedReposEnv = process.env.REPO_MEMORY_ALLOWED_REPOS?.trim();
  const allowedRepos = parseAllowedRepos(allowedReposEnv);
  const defaultRepo = `${context.repo.owner}/${context.repo.repo}`;

  const repoValidation = validateRepo(targetRepo, defaultRepo, allowedRepos);
  if (!repoValidation.valid) {
    await setPushRepoMemoryFailure(`E004: ${repoValidation.error}`);
    return;
  }

  // Validate FILE_GLOB_FILTER patterns: absolute paths (starting with "/") are not supported.
  if (fileGlobFilter) {
    const allPatternStrs = fileGlobFilter.trim().split(/\s+/).filter(Boolean);
    for (const pat of allPatternStrs) {
      if (pat.startsWith("/")) {
        await setPushRepoMemoryFailure(`FILE_GLOB_FILTER contains an unsupported pattern: "${pat}". Patterns must not start with "/" (absolute paths are not allowed).`);
        return;
      }
    }
  }

  // Source directory with memory files (artifact location)
  // The artifactDir IS the memory directory (no nested structure needed)
  const sourceMemoryPath = artifactDir;

  // Check if artifact memory directory exists
  if (!fs.existsSync(sourceMemoryPath)) {
    if (jsonSchemasRequired) {
      await setPushRepoMemoryFailure(`ERR_VALIDATION: Repo-memory '${memoryId}' is missing the required schema file(s) in its artifact.`);
      return;
    }
    core.info(`Memory directory not found in artifact: ${sourceMemoryPath}`);
    return;
  }

  // We're already in the checked out repository (from checkout step)
  const workspaceDir = process.env.GITHUB_WORKSPACE || process.cwd();
  core.info(`Working in repository: ${workspaceDir}`);

  // Split targetRepo into owner and repo name here so they are available for
  // the GitHub REST API seeding calls below (before the checkout block).
  const [targetOwner, targetRepoName] = targetRepo.split("/");

  // Checkout or create the memory branch
  // Note: we do NOT disable sparse checkout here. Disabling sparse checkout on a
  // large repository forces git to materialize all tracked files into the working
  // tree, which can exhaust pipe buffers (ENOBUFS) when thousands of files are
  // involved. The memory branch only holds a handful of small files, so sparse
  // checkout does not need to be altered for either case below.
  core.info(`Checking out branch: ${branchName}...`);

  // baseRef: the remote branch HEAD SHA before we make our local commit.
  // Used by pushSignedCommits to compute git rev-list baseRef..HEAD (i.e. only
  // our new commit) and as the OCC token for the GraphQL createCommitOnBranch
  // mutation.  Empty string when the branch is brand new (orphan).
  let baseRef = "";
  // Whether the branch existed before this job, so other runs may have saved to it.
  let branchExisted = false;

  try {
    const repoUrl = `https://x-access-token:${ghToken}@${serverHost}/${targetRepo}.git`;

    // Try to fetch the branch
    try {
      execGitSync(["fetch", repoUrl, `${branchName}:${branchName}`], { stdio: "pipe", suppressLogs: true });
      execGitSync(["checkout", branchName], { stdio: "inherit" });
      core.info(`Checked out existing branch: ${branchName}`);
      branchExisted = true;
      // Capture the remote HEAD SHA so pushSignedCommits can compute which
      // local commits are new (rev-list range: baseRef..HEAD).
      baseRef = execGitSync(["rev-parse", "HEAD"]).trim();
      core.info(`Captured baseRef for signed commit push: ${baseRef}`);
    } catch (fetchError) {
      // Determine whether the fetch failed because the branch does not exist
      // (expected for new memory branches) or because of a network / auth
      // problem (unexpected – must surface as a real error and must NOT fall
      // through to orphan-branch creation).
      const fetchErrMsg = getErrorMessage(fetchError);
      const isMissingBranch = /couldn't find remote ref/i.test(fetchErrMsg) || /remote branch .* not found/i.test(fetchErrMsg);
      if (!isMissingBranch) {
        // Re-throw so the outer catch reports the real cause.
        throw fetchError;
      }
      // A recorded starting commit means the branch existed when this run's
      // agent cloned it, so it was deleted since; do not recreate it.
      if (COMMIT_ID_PATTERN.test(String(process.env.REPO_MEMORY_BASE_COMMIT || "").trim())) {
        await setPushRepoMemoryFailure(`Branch ${branchName} was deleted after this run started from it; not recreating it from this run's files.`);
        return;
      }

      // Branch doesn't exist – attempt to seed it via the GitHub REST API so
      // the seed commit is server-signed, satisfying "Require signed commits"
      // branch protection rules.  Commits created via the REST API with
      // GITHUB_TOKEN are automatically signed by GitHub.
      const EMPTY_TREE_SHA = "4b825dc642cb6eb9a060e54bf8d69288fbee4904";
      try {
        core.info(`Branch ${branchName} does not exist, seeding via GitHub REST API...`);
        const { data: seedCommit } = await github.rest.git.createCommit({
          owner: targetOwner,
          repo: targetRepoName,
          message: `Initialize ${branchName}`,
          tree: EMPTY_TREE_SHA,
          parents: [],
        });
        let useApiSeedSha = true;
        try {
          await github.rest.git.createRef({
            owner: targetOwner,
            repo: targetRepoName,
            ref: `refs/heads/${branchName}`,
            sha: seedCommit.sha,
          });
        } catch (createRefError) {
          // GitHub returns HTTP 422 with "Reference already exists" when the
          // branch was created concurrently between our fetch-check and this
          // createRef call.  Check for either the status code or the message
          // text since different Octokit versions surface errors differently.
          // Treat as success and use the existing branch instead.
          const createRefErrMsg = getErrorMessage(createRefError);
          if (!/422|Reference already exists/i.test(createRefErrMsg)) {
            throw createRefError;
          }
          core.info(`Branch ${branchName} was created concurrently (422 Reference already exists); using existing branch.`);
          useApiSeedSha = false;
        }
        // Fetch the newly seeded (or concurrently created) branch and check it out.
        execGitSync(["fetch", repoUrl, `${branchName}:${branchName}`], { stdio: "pipe", suppressLogs: true });
        execGitSync(["checkout", branchName], { stdio: "inherit" });
        // Set baseRef to the fetched head so pushSignedCommits can use the
        // GraphQL signed-commit path instead of the unsigned git push fallback.
        // Another run may already have saved on top of the seed (or created the
        // branch itself, the 422 case), in which case this save must replay
        // onto that run's files rather than overwrite them.
        baseRef = execGitSync(["rev-parse", "HEAD"]).trim();
        branchExisted = !useApiSeedSha || baseRef !== seedCommit.sha;
        core.info(`Seeded and checked out new branch ${branchName} via GitHub API (baseRef: ${baseRef})`);
      } catch (seedError) {
        // Fallback: API seeding failed (e.g. insufficient token permissions).
        // Fall back to the original orphan-branch + git push path and emit a
        // warning so the operator knows signed commits may not be produced.
        core.warning(`Failed to seed branch ${branchName} via GitHub API, falling back to orphan branch: ${getErrorMessage(seedError)}`);
        // baseRef stays "" — pushSignedCommits will use git push for this
        // orphan-branch first push (unsigned, may be rejected by strict rulesets).
        core.info(`Branch ${branchName} does not exist, creating orphan branch...`);
        execGitSync(["checkout", "--orphan", branchName], { stdio: "inherit" });
        // Reset the index to an empty tree. This is O(1) regardless of how many
        // files the source branch contained, avoiding the ENOBUFS error that
        // "git rm -rf ." (with stdio:pipe) causes on large repos (10K+ files).
        execGitSync(["read-tree", "--empty"], { stdio: "pipe" });
        // Clean the working directory using Node.js so we never pipe large git
        // output back through spawnSync buffers.
        core.info("Cleaning working directory for orphan branch...");
        for (const entry of fs.readdirSync(workspaceDir)) {
          if (entry !== ".git") {
            fs.rmSync(path.join(workspaceDir, entry), { recursive: true, force: true });
          }
        }
        core.info(`Created orphan branch: ${branchName}`);
      }
    }
  } catch (error) {
    await setPushRepoMemoryFailure(`Failed to checkout branch: ${getErrorMessage(error)}`);
    return;
  }

  // Create destination directory in repo
  // Files are copied to the root of the checked-out branch (workspaceDir)
  // The branch name (e.g., "memory/campaigns") identifies the branch,
  // but files go at the branch root, not in a nested subdirectory
  const destMemoryPath = workspaceDir;
  core.info(`Destination directory: ${destMemoryPath}`);
  const existingLedgerShardDir = path.join(destMemoryPath, "ledger", "shards");
  const existingLedgerSegments = fs.existsSync(existingLedgerShardDir)
    ? new Set(
        fs
          .readdirSync(existingLedgerShardDir)
          .filter(name => name.endsWith(".jsonl"))
          .map(name => name.slice(0, -6))
      )
    : new Set();

  // Recursively scan and collect files from artifact directory
  let filesToCopy = [];
  /** @type {Array<{path: string, reason: string}>} */
  let filteredOutFiles = [];

  // Compile glob patterns once, outside the scan loop
  const { patternStrs, compiledPatterns } = compileFileGlobPatterns(fileGlobFilter);
  const isEligibleFile = relativePath => isMemoryFileEligible(relativePath, allowedExtensions, compiledPatterns).eligible;
  if (compiledPatterns.length > 0) {
    core.info(`File glob filter enabled with ${patternStrs.length} pattern(s):`);
    patternStrs.forEach((pat, idx) => {
      core.info(`  [${idx + 1}] "${pat}" -> regex: ${compiledPatterns[idx].source}`);
    });
  } else {
    core.info("No file glob filter - all files will be accepted");
  }

  /**
   * Recursively scan directory and collect files
   * @param {string} dirPath - Directory to scan
   * @param {string} relativePath - Relative path from sourceMemoryPath (for nested files)
   */
  function scanDirectory(dirPath, relativePath = "") {
    let entries;
    try {
      entries = fs.readdirSync(dirPath, { withFileTypes: true });
    } catch (err) {
      throw new Error(`Failed to read directory ${dirPath}: ${getErrorMessage(err)}`, { cause: err });
    }

    for (const entry of entries) {
      const fullPath = path.join(dirPath, entry.name);
      const relativeFilePath = relativePath ? path.join(relativePath, entry.name) : entry.name;

      if (entry.isDirectory()) {
        core.info(`  [dir]  ${relativeFilePath}/`);
        // Recursively scan subdirectory
        scanDirectory(fullPath, relativeFilePath);
      } else if (entry.isFile()) {
        let stats;
        try {
          stats = fs.statSync(fullPath);
        } catch (err) {
          throw new Error(`Failed to inspect file ${fullPath}: ${getErrorMessage(err)}`, { cause: err });
        }
        const normalizedRelPath = relativeFilePath.replace(/\\/g, "/");

        if (isUntrustedLedgerArtifact(normalizedRelPath, existingLedgerSegments)) {
          core.info(`  [skip] ${normalizedRelPath} (untrusted ledger artifact)`);
          filteredOutFiles.push({ path: normalizedRelPath, reason: "untrusted ledger artifact" });
          continue;
        }

        // Allowed extensions and file-glob are persistence filters: files that do not
        // pass are logged and ignored (never uploaded, validated, counted toward
        // max-file-count/size/patch-size, or pushed) rather than causing a hard failure.
        // Only the compiler's sanitized writer pipeline can exempt daily AIC files.
        if (!hasTrustedDailyAICLedger || !isDailyAICLedgerFile(normalizedRelPath)) {
          const eligibility = isMemoryFileEligible(relativeFilePath, allowedExtensions, compiledPatterns);
          if (!eligibility.eligible) {
            core.info(`  [skip] ${normalizedRelPath} (${stats.size} bytes) — ${eligibility.reason}`);
            filteredOutFiles.push({ path: normalizedRelPath, reason: eligibility.reason || "ineligible" });
            continue;
          }
        }

        // Validate file size
        if (stats.size > maxFileSize) {
          core.error(`File exceeds size limit: ${relativeFilePath} (${stats.size} bytes > ${maxFileSize} bytes)`);
          throw new Error("File size validation failed");
        }

        core.info(`  [keep] ${normalizedRelPath} (${stats.size} bytes)`);
        filesToCopy.push({
          relativePath: relativeFilePath,
          source: fullPath,
          size: stats.size,
        });
      }
    }
  }

  try {
    core.info(`Scanning artifact directory: ${sourceMemoryPath}`);
    scanDirectory(sourceMemoryPath);
    core.info(`Scan complete: ${filesToCopy.length} file(s) accepted, ${filteredOutFiles.length} file(s) filtered out`);
    if (filteredOutFiles.length > 0) {
      core.warning(`Filtered-out files (${filteredOutFiles.length}):\n${filteredOutFiles.map(f => `  - ${f.path} (${f.reason})`).join("\n")}`);
    }
    if (filesToCopy.length > 0 && filesToCopy.length <= 10) {
      core.info("Accepted files:");
      filesToCopy.forEach(f => core.info(`  - ${f.relativePath} (${f.size} bytes)`));
    } else if (filesToCopy.length > 10) {
      core.info(`Accepted files (first 10 of ${filesToCopy.length}):`);
      filesToCopy.slice(0, 10).forEach(f => core.info(`  - ${f.relativePath} (${f.size} bytes)`));
      core.info(`  ... and ${filesToCopy.length - 10} more`);
    }
  } catch (error) {
    await setPushRepoMemoryFailure(`Failed to scan artifact directory: ${getErrorMessage(error)}`);
    return;
  }

  if (filesToCopy.length === 0 && !formatJSON) {
    core.info("No eligible files to copy from artifact (all files were filtered out or none present)");
    return;
  }

  // Replay only this run's changes so that rows other runs saved after this
  // run's agent started are not overwritten by its older copy of a file.
  /** @type {ReturnType<typeof planRepoMemoryReplay>} */
  let replayPlan = { steps: null, reason: "" };
  if (branchExisted) {
    try {
      replayPlan = planRepoMemoryReplay({ files: filesToCopy, baseCommit: process.env.REPO_MEMORY_BASE_COMMIT, workspaceDir });
    } catch (error) {
      await setPushRepoMemoryFailure(`Failed to compare memory files with the commit this run started from: ${getErrorMessage(error)}`);
      return;
    }
    if (!replayPlan.steps) {
      core.warning(`Copying this run's memory files over ${branchName} because ${replayPlan.reason}. Rows that other runs saved to the same files after this run started may be lost.`);
    }
  }

  core.info(`Copying ${filesToCopy.length} validated file(s)...`);

  // Copy files to destination (preserving directory structure)
  /** @type {typeof filesToCopy} */
  const writtenFiles = [];
  for (const file of filesToCopy) {
    const destFilePath = path.join(destMemoryPath, file.relativePath);
    const destDir = path.dirname(destFilePath);

    try {
      // Path traversal protection
      const resolvedRoot = path.resolve(destMemoryPath) + path.sep;
      const resolvedDest = path.resolve(destFilePath);
      if (!resolvedDest.startsWith(resolvedRoot)) {
        await setPushRepoMemoryFailure(`Refusing to write outside repo-memory directory: ${file.relativePath}`);
        return;
      }

      const step = replayPlan.steps?.get(file.relativePath);
      if (step?.action === "skip") {
        core.info(`Skipped: ${file.relativePath} (unchanged by this run and deleted by another run)`);
        continue;
      }

      // Ensure destination directory exists
      fs.mkdirSync(destDir, { recursive: true });

      if (!step) {
        fs.copyFileSync(file.source, destFilePath);
        core.info(`Copied: ${file.relativePath} (${file.size} bytes)`);
      } else if (step.action === "write") {
        // A merge with another run's rows can outgrow a file that passed the scan.
        if (step.content.length > maxFileSize) {
          await setPushRepoMemoryFailure(`File exceeds size limit after merging with other runs' changes: ${file.relativePath} (${step.content.length} bytes > ${maxFileSize} bytes)`);
          return;
        }
        fs.writeFileSync(destFilePath, step.content);
        core.info(`Wrote: ${file.relativePath} (${step.content.length} bytes)`);
      }
      writtenFiles.push(file);
    } catch (error) {
      await setPushRepoMemoryFailure(`Failed to copy file ${file.relativePath}: ${getErrorMessage(error)}`);
      return;
    }
  }
  filesToCopy = writtenFiles;

  try {
    const currentRepo = targetRepo.endsWith(".wiki") ? targetRepo.slice(0, -".wiki".length) : targetRepo;
    const substitutedFiles = applyTemporaryIdSubstitutions(filesToCopy, destMemoryPath, temporaryIdMap, currentRepo, maxFileSize);
    if (substitutedFiles.length > 0) {
      core.info(`Applied temporary ID substitutions to ${substitutedFiles.length} memory file(s):`);
      substitutedFiles.forEach(file => core.info(`  - ${file}`));
    }
  } catch (error) {
    await setPushRepoMemoryFailure(`Failed to apply temporary ID substitutions to repo-memory files: ${getErrorMessage(error)}`);
    return;
  }

  // Format JSON files if requested
  if (formatJSON) {
    core.info("FORMAT_JSON is enabled: formatting .json files as human-readable...");

    try {
      const formattedFiles = formatJSONFiles(destMemoryPath, maxFileSize, isEligibleFile);
      ledgerActivity.normalized.push(...formattedFiles);
      for (const formattedFile of formattedFiles) {
        core.info(`Formatted JSON: ${formattedFile}`);
      }
    } catch (error) {
      await setPushRepoMemoryFailure(`Failed to format JSON files: ${getErrorMessage(error)}`);
      return;
    }
  }

  if (validationScriptBase64 || jsonSchemas) {
    core.info("Running repo-memory validation before commit...");
    const customValidation = runCustomMemoryValidation({
      scriptBase64: validationScriptBase64,
      jsonSchemas,
      requireJSONSchemas: jsonSchemasRequired,
      memoryDir: destMemoryPath,
      memoryId,
      kind: "repo",
      phase: "push",
      sourceJobResult: process.env.GH_AW_MEMORY_SOURCE_JOB_RESULT,
      timeoutSeconds: validationTimeoutSeconds,
      isEligibleFile,
    });
    if (!customValidation.ok) {
      const reason = customValidation.timedOut ? `timed out after ${validationTimeoutSeconds} second(s)` : `exited with code ${customValidation.exitCode}`;
      const failureDetail = getCustomValidationFailureDetail(customValidation);
      const errorMessage = `Repo-memory validation failed for '${memoryId}': ${reason}${failureDetail ? `:\n${failureDetail}` : "."}`;
      if (customValidation.stdout) {
        core.info(`Custom repo-memory validation stdout:\n${redactFailureSummary(customValidation.stdout)}`);
      }
      if (customValidation.stderr) {
        core.error(`Custom repo-memory validation stderr:\n${redactFailureSummary(customValidation.stderr)}`);
      }
      setOutput("validation_failed", "true");
      setOutput("validation_error", errorMessage);
      await setPushRepoMemoryFailure(errorMessage);
      return;
    }
    if (customValidation.stdout) {
      core.info(`Custom repo-memory validation stdout:\n${redactFailureSummary(customValidation.stdout)}`);
    }
    if (customValidation.stderr) {
      core.info(`Custom repo-memory validation stderr:\n${redactFailureSummary(customValidation.stderr)}`);
    }
    core.info("Repo-memory validation passed.");
  }

  // Build literal pathspecs from the relative paths of files to copy.
  // The :(literal) magic prefix tells Git to treat each entry as a plain string,
  // preventing glob expansion or pathspec-magic interpretation (e.g. :(top),
  // wildcards) even when a filename happens to contain those characters.
  const managedPaths = [...filesToCopy.map(file => file.relativePath), ...ledgerActivity.normalized];
  const literalPathspecs = Array.from(new Set(managedPaths.map(relativePath => `:(literal)${relativePath}`))).sort();

  if (literalPathspecs.length === 0) {
    core.info("No eligible memory files changed after normalization");
    await writeLedgerSummary();
    return;
  }

  // Check if we have any changes to commit, scoped to managed memory files only.
  let changedFileCount = 0;
  try {
    const statusArgs = ["status", "--porcelain"];
    if (literalPathspecs.length > 0) {
      statusArgs.push("--", ...literalPathspecs);
    }
    const status = execGitSync(statusArgs, { cwd: workspaceDir });
    const changedEntries = status
      .split("\n")
      .map(line => line.trim())
      .filter(Boolean);
    changedFileCount = changedEntries.length;
  } catch (error) {
    await setPushRepoMemoryFailure(`Failed to check git status: ${getErrorMessage(error)}`);
    return;
  }

  if (changedFileCount === 0) {
    core.info("No changes detected after copying files");
    setOutput("save_status", "unchanged");
    await writeLedgerSummary();
    return;
  }

  if (changedFileCount > maxFileCount) {
    await setPushRepoMemoryFailure(`Too many changed files in working directory (${changedFileCount} > ${maxFileCount})`);
    return;
  }

  core.info(`Changed files detected after copying: ${changedFileCount}`);
  core.info("Changes detected, committing and pushing...");

  // Stage all changes.
  // --sparse: "Allow updating index entries outside of the sparse-checkout cone.
  // Normally, git add refuses to update index entries whose paths do not fit
  // within the sparse-checkout cone, since those files might be removed from the
  // working tree without warning." (git-add(1))
  // This is required because "git checkout --orphan" can re-activate
  // sparse-checkout, causing a plain "git add ." to silently skip or reject
  // files on the first run for a new memory branch.
  try {
    const addArgs = ["add", "--sparse"];
    if (literalPathspecs.length > 0) {
      addArgs.push("--", ...literalPathspecs);
    } else {
      addArgs.push(".");
    }
    execGitSync(addArgs, { stdio: "inherit", cwd: workspaceDir });
  } catch (error) {
    await setPushRepoMemoryFailure(`Failed to stage changes: ${getErrorMessage(error)}`);
    return;
  }

  // Validate total patch size before committing.
  // The patch diff size is the net bytes added (additions minus deletions, clamped to zero).
  // Using the net value prevents files that are rewritten with similar-sized content
  // (e.g. a regenerated JSON object) from being counted as "entire source code size"
  // even though only a small portion of the data actually changed.
  try {
    const patchSizeBytes = getStagedPatchDiffSizeBytes({ execGitSyncFn: execGitSync, cwd: workspaceDir });
    ledgerActivity.saving.changedFiles = changedFileCount;
    ledgerActivity.saving.patchBytes = patchSizeBytes;
    const patchSizeKb = Math.ceil(patchSizeBytes / 1024);
    const maxPatchSizeKb = Math.floor(maxPatchSize / 1024);
    // Allow 20% overhead to account for git diff format (headers, context lines, etc.)
    const effectiveMaxPatchSize = Math.floor(maxPatchSize * 1.2);
    const effectiveMaxPatchSizeKb = Math.floor(effectiveMaxPatchSize / 1024);
    const patchSizeMessage = `Patch diff size: ${patchSizeKb} KB (${patchSizeBytes} bytes) (configured limit: ${maxPatchSizeKb} KB (${maxPatchSize} bytes), effective with 20% overhead: ${effectiveMaxPatchSizeKb} KB (${effectiveMaxPatchSize} bytes))`;
    if (patchSizeBytes > effectiveMaxPatchSize) {
      // Warn at warning level so the size is visible even without verbose mode
      core.warning(patchSizeMessage);
      // Add per-file diff stats to diagnose what's causing the large patch
      // (e.g. a full rewrite of an accumulated history file shows old + new content in the diff)
      try {
        const diffStat = execGitSync(["diff", "--cached", "--stat"], { stdio: "pipe", cwd: workspaceDir });
        core.warning(`Patch content breakdown (git diff --stat):\n${diffStat}`);
      } catch (statError) {
        core.warning(`Could not retrieve diff stat: ${getErrorMessage(statError)}`);
      }
      setOutput("patch_size_exceeded", "true");
      await setPushRepoMemoryFailure(
        `Patch diff size (${patchSizeKb} KB, ${patchSizeBytes} bytes) exceeds maximum allowed size (${effectiveMaxPatchSizeKb} KB, ${effectiveMaxPatchSize} bytes, configured limit: ${maxPatchSizeKb} KB with 20% overhead allowance). Reduce the number or size of changes, or increase max-patch-size.`
      );
      return;
    } else if (patchSizeBytes > maxPatchSize) {
      // Within the 20% overhead window — still log as a warning so it's visible
      core.warning(patchSizeMessage);
    } else {
      core.info(patchSizeMessage);
    }
  } catch (error) {
    await setPushRepoMemoryFailure(`Failed to compute patch diff size: ${getErrorMessage(error)}`);
    return;
  }

  // Commit changes
  try {
    execGitSync(["commit", "-m", `Update repo memory from workflow run ${githubRunId}`], { stdio: "inherit" });
  } catch (error) {
    await setPushRepoMemoryFailure(`Failed to commit changes: ${getErrorMessage(error)}`);
    return;
  }

  // Push using the GraphQL createCommitOnBranch mutation so commits are
  // server-signed (verified) by GitHub.  This satisfies "Require signed
  // commits" branch-protection rules that reject plain git push.
  //
  // pushSignedCommits falls back to a plain `git push` when the mutation
  // cannot be used (merge commits, symlinks, submodule entries).  Under a
  // strict signed-commits ruleset that fallback will also be rejected —
  // that is expected behaviour: remove the unsupported file types and
  // re-run.
  const pushedCommit = await pushRepoMemoryChangesWithRetry({
    githubClient: github,
    targetOwner,
    targetRepoName,
    targetRepo,
    branchName,
    baseRef,
    workspaceDir,
    ghToken,
    serverHost,
    validateBeforePush:
      validationScriptBase64 || jsonSchemas
        ? () => {
            const validation = runCustomMemoryValidation({
              scriptBase64: validationScriptBase64,
              jsonSchemas,
              requireJSONSchemas: jsonSchemasRequired,
              memoryDir: destMemoryPath,
              memoryId,
              kind: "repo",
              phase: "push",
              sourceJobResult: process.env.GH_AW_MEMORY_SOURCE_JOB_RESULT,
              timeoutSeconds: validationTimeoutSeconds,
              isEligibleFile,
            });
            if (!validation.ok) {
              throw new Error(
                `${validation.timedOut ? `timed out after ${validationTimeoutSeconds} second(s)` : `exited with code ${validation.exitCode}`}${getCustomValidationFailureDetail(validation) ? `: ${getCustomValidationFailureDetail(validation)}` : ""}`
              );
            }
          }
        : undefined,
  });
  ledgerActivity.saving.pushed = Boolean(pushedCommit);
  if (pushedCommit) {
    setOutput("save_branch", branchName);
    setOutput("save_commit", pushedCommit);
    setOutput("save_status", "saved");
  } else if (pushedCommit !== false) {
    setOutput("save_status", "unchanged");
  }
  await writeLedgerSummary();
}

module.exports = {
  applyTemporaryIdSubstitutions,
  configureRepoMemoryMergePolicy,
  getCustomValidationFailureDetail,
  isDeterministicPushValidationError,
  isUntrustedLedgerArtifact,
  main,
  mergeRepoMemoryFile,
  planRepoMemoryReplay,
  pushRepoMemoryChangesWithRetry,
  reconcileRepoMemoryRetry,
  redactFailureSummary,
  setPushRepoMemoryFailure,
};
