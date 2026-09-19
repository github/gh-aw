// @ts-check
/// <reference types="@actions/github-script" />

/**
 * @fileoverview Shared helper for pushing local commits either through
 * GitHub's signed-commit GraphQL API or, when explicitly configured, direct
 * `git push`.
 */

const { ERR_API, ERR_SYSTEM, ERR_VALIDATION } = require("./error_codes.cjs");
const { loadTemporaryIdMapFromResolved, replaceTemporaryIdReferencesInPatch, TEMPORARY_ID_CANDIDATE_REFERENCE_PATTERN } = require("./temporary_id.cjs");
const { checkFileProtectionPostApply } = require("./manifest_file_helpers.cjs");
const { backfillCommitObjects } = require("./git_helpers.cjs");
const { overridePersistedExtraheader, restorePersistedExtraheader } = require("./git_auth_helpers.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const ERROR_CODE_PREFIX_RE = /^(?:ERR_[A-Z_]+|E\d{3}):\s*/;
/**
 * Strip the single leading `ERR_*: ` or `E001: ` sentinel from a message.
 * Each sentinel error in this file carries exactly one such prefix, so one
 * removal is sufficient; the wrapper can then add its own prefix without
 * emitting a duplicate code.
 */
function stripLeadingErrorCode(msg) {
  return msg.replace(ERROR_CODE_PREFIX_RE, "");
}
const OID_PATTERN = /^[0-9a-f]{40}$/i;

/**
 * Detect whether git output indicates a partial/shallow-clone object availability
 * failure (as opposed to a genuine merge conflict).
 *
 * On a shallow + partial (`--filter=blob:none`) checkout of a large monorepo, a
 * `git rebase --onto` can fail because git lazily fetches required objects (merge
 * bases, trees, blobs of intermediate commits) from the promisor remote and the
 * fetch is rejected. The hallmark messages are emitted by git's lazy-fetch path:
 *   - `could not fetch <sha> from promisor remote`
 *   - `remote error: upload-pack: not our ref <sha>`
 *   - `unable to read <sha>` / `object <sha> missing` for objects git expected locally
 *
 * These failures are recoverable by backfilling the exact commit objects the
 * operation needs from origin (`git fetch --no-filter origin <sha>...`), unlike
 * merge conflicts which require manual resolution. We deliberately fetch only
 * the anchor commits — never an uncontrolled `--unshallow` or unbounded deepen,
 * which would download a huge monorepo's entire history.
 *
 * @param {string} output - Combined stdout/stderr from the failed git command.
 * @returns {boolean} True when the failure looks like a partial-clone object fetch failure.
 */
function isPartialCloneObjectFailure(output) {
  if (!output) return false;
  return (
    /could not fetch .* from promisor remote/i.test(output) ||
    /upload-pack: not our ref/i.test(output) ||
    /remote error: upload-pack/i.test(output) ||
    /(unable to read|object).*(missing|not found)/i.test(output) ||
    /fatal: bad object/i.test(output)
  );
}

/** Sentinel error class used to signal that the commit range contains a shape
 *  that the GitHub GraphQL `createCommitOnBranch` mutation cannot represent
 *  (merge commit, symlink mode 120000, or submodule mode 160000).  The catch
 *  block uses this to avoid silently falling back to an unsigned `git push`
 *  for these permanent, structural refusals.  Executable bit (mode 100755) is
 *  not included here because it only triggers a warning and continues with the
 *  GraphQL path (the bit is silently dropped by the mutation).
 */
class PushSignedCommitsUnsupportedShape extends Error {
  /** @param {string} message */
  constructor(message) {
    super(message);
    this.name = "PushSignedCommitsUnsupportedShape";
  }
}

/** Sentinel error class for synthesized-payload policy violations. */
class PushSignedCommitsPolicyViolation extends Error {
  /** @param {string} message */
  constructor(message) {
    super(message);
    this.name = "PushSignedCommitsPolicyViolation";
  }
}

/**
 * Sentinel error class marking a terminal failure of the unsigned git push fallback that is
 * attempted after a genuine (non-recoverable) rebase conflict. This must propagate as-is
 * without triggering the generic "GraphQL failed, retry with git push" fallback further up —
 * that push was already attempted (and rejected), so retrying it again would just repeat the
 * same failure (e.g. against a branch-protection rule requiring signed commits).
 */
class PushSignedCommitsUnsignedFallbackFailed extends Error {
  /** @param {string} message */
  constructor(message, options) {
    super(message, options);
    this.name = "PushSignedCommitsUnsignedFallbackFailed";
  }
}

/**
 * Unescape a C-quoted path returned by `git diff-tree --raw`.
 *
 * git wraps paths that contain special characters (spaces, non-ASCII bytes,
 * control characters, etc.) in double-quotes and encodes each "unusual" byte
 * as a C-style escape sequence.  This function strips the surrounding quotes
 * and decodes the escape sequences back to the original byte sequence, then
 * interprets the result as UTF-8.
 *
 * Supported escape sequences: `\\`, `\"`, `\a`, `\b`, `\f`, `\n`, `\r`,
 * `\t`, `\v`, and octal `\NNN` (1–3 octal digits).
 *
 * @param {string} s - Raw path token from git output (may or may not be quoted)
 * @returns {string} Unescaped path
 */
function unquoteCPath(s) {
  if (!s.startsWith('"')) return s;
  // Strip surrounding double-quotes
  const inner = s.slice(1, s.endsWith('"') ? s.length - 1 : s.length);
  const bytes = [];
  let i = 0;
  while (i < inner.length) {
    if (inner[i] === "\\") {
      i++;
      if (i < inner.length && inner[i] >= "0" && inner[i] <= "7") {
        // Octal sequence – collect up to 3 octal digits
        let oct = "";
        while (i < inner.length && inner[i] >= "0" && inner[i] <= "7" && oct.length < 3) {
          oct += inner[i++];
        }
        bytes.push(parseInt(oct, 8));
      } else {
        const esc = inner[i++];
        switch (esc) {
          case "\\":
            bytes.push(0x5c);
            break;
          case '"':
            bytes.push(0x22);
            break;
          case "a":
            bytes.push(0x07);
            break;
          case "b":
            bytes.push(0x08);
            break;
          case "f":
            bytes.push(0x0c);
            break;
          case "n":
            bytes.push(0x0a);
            break;
          case "r":
            bytes.push(0x0d);
            break;
          case "t":
            bytes.push(0x09);
            break;
          case "v":
            bytes.push(0x0b);
            break;
          default:
            // Unknown escape: preserve backslash and the character as-is
            bytes.push(0x5c, esc.charCodeAt(0));
        }
      }
    } else {
      bytes.push(inner.charCodeAt(i++));
    }
  }
  return Buffer.from(bytes).toString("utf8");
}

/**
 * Read a blob object as a base64-encoded string using `git cat-file blob <blobHash>`.
 * The raw bytes emitted by git are collected via the `exec.exec` stdout
 * listener so that binary files are not corrupted by any UTF-8 decoding
 * layer (unlike `exec.getExecOutput` which always passes stdout through a
 * `StringDecoder('utf8')`).
 *
 * @param {string} blobHash - Object hash of the blob (from `git diff-tree --raw` dstHash field)
 * @param {string} cwd - Working directory of the local git checkout
 * @returns {Promise<string>} Base64-encoded file contents
 */
async function readBlobAsBase64(blobHash, cwd) {
  /** @type {Buffer[]} */
  const chunks = [];
  await exec.exec("git", ["cat-file", "blob", blobHash], {
    cwd,
    silent: true,
    listeners: {
      stdout: (/** @type {Buffer} */ data) => {
        chunks.push(data);
      },
    },
  });
  return Buffer.concat(chunks).toString("base64");
}

/**
 * Replace temporary ID references in base64-encoded UTF-8 text content.
 * Returns original content unchanged for:
 * - binary / non-UTF8 blobs
 * - UTF-8 text with no temporary ID matches
 * Returns rewritten base64 content when UTF-8 text contains resolvable temporary IDs.
 *
 * @param {string} base64Content
 * @param {Map<string, {repo: string, number: number}>} temporaryIdMap
 * @param {string} currentRepo
 * @param {string} filePath
 * @returns {string}
 */
function maybeReplaceTemporaryIdsInBase64Content(base64Content, temporaryIdMap, currentRepo, filePath) {
  if (!(temporaryIdMap instanceof Map) || temporaryIdMap.size === 0) {
    return base64Content;
  }

  const rawBytes = Buffer.from(base64Content, "base64");
  const utf8Text = rawBytes.toString("utf8");

  // Treat only clean UTF-8 round-trippable content as text.
  if (!Buffer.from(utf8Text, "utf8").equals(rawBytes)) {
    return base64Content;
  }

  if (!TEMPORARY_ID_CANDIDATE_REFERENCE_PATTERN.test(utf8Text)) {
    return base64Content;
  }

  const replaced = replaceTemporaryIdReferencesInPatch(utf8Text, temporaryIdMap, currentRepo);
  if (replaced === utf8Text) {
    return base64Content;
  }

  core.info(`pushSignedCommits: resolved temporary ID references in file content: ${filePath}`);
  return Buffer.from(replaced, "utf8").toString("base64");
}

/**
 * Probe the remote for a branch HEAD OID using ls-remote.
 * Uses an explicit push remote URL and token when provided (fork-backed pushes).
 *
 * @param {string} branch
 * @param {string} cwd
 * @param {any} [gitAuthEnv]
 * @param {string} [pushRemoteUrl]
 * @param {string} [pushToken]
 * @returns {Promise<string | undefined>} The OID if the branch exists on the remote, otherwise undefined
 */
async function lsRemoteHeadOid(branch, cwd, gitAuthEnv, pushRemoteUrl, pushToken) {
  const remote = pushRemoteUrl || "origin";
  const doLsRemote = () => exec.getExecOutput("git", ["ls-remote", remote, `refs/heads/${branch}`], { cwd, env: { ...process.env, ...(gitAuthEnv || {}) } });
  let result;
  if (pushRemoteUrl && pushToken) {
    const githubServerUrl = (process.env.GITHUB_SERVER_URL || "https://github.com").replace(/\/+$/, "");
    let previousExtraheaders = [];
    let overrideApplied = false;
    try {
      previousExtraheaders = await overridePersistedExtraheader(githubServerUrl, pushToken, cwd);
      overrideApplied = true;
      result = await doLsRemote();
    } finally {
      if (overrideApplied) {
        await restorePersistedExtraheader(githubServerUrl, previousExtraheaders, cwd);
      }
    }
  } else {
    result = await doLsRemote();
  }
  const resolved = result.stdout.trim().split(/\s+/)[0];
  return OID_PATTERN.test(resolved) ? resolved : undefined;
}

/**
 * Push the local branch to origin using git directly and return the local HEAD
 * SHA after the push succeeds.
 *
 * @param {object} opts
 * @param {string} opts.branch
 * @param {string} opts.cwd
 * @param {any} [opts.gitAuthEnv]
 * @param {string} [opts.pushRemoteUrl]
 * @param {string} [opts.pushToken]
 * @returns {Promise<string>}
 */
async function pushBranchAndResolveHead({ branch, cwd, gitAuthEnv, pushRemoteUrl, pushToken }) {
  const pushArgs = pushRemoteUrl ? ["push", pushRemoteUrl, branch] : ["push", "origin", branch];
  const pushOnce = async () => {
    await exec.exec("git", pushArgs, {
      cwd,
      env: { ...process.env, ...(gitAuthEnv || {}) },
    });
    return resolveLocalHeadSha(cwd);
  };
  if (!pushRemoteUrl || !pushToken) {
    return pushOnce();
  }

  const githubServerUrl = (process.env.GITHUB_SERVER_URL || "https://github.com").replace(/\/+$/, "");
  let previousExtraheaders = [];
  let overrideApplied = false;
  try {
    previousExtraheaders = await overridePersistedExtraheader(githubServerUrl, pushToken, cwd);
    overrideApplied = true;
    return await pushOnce();
  } finally {
    if (overrideApplied) {
      await restorePersistedExtraheader(githubServerUrl, previousExtraheaders, cwd);
    }
  }
}

/**
 * Enforce limits and file-protection policy on the synthesized GraphQL payload.
 *
 * @param {Array<{path: string, contents: string}>} additions
 * @param {Array<{path: string}>} deletions
 * @param {Record<string, any> | undefined} validationConfig
 */
function validateSynthesizedFileChanges(additions, deletions, validationConfig) {
  if (!validationConfig) {
    return;
  }

  const uniquePaths = Array.from(new Set([...deletions.map(entry => entry.path), ...additions.map(entry => entry.path)].map(path => String(path || "").trim()).filter(Boolean)));

  const maxFilesRaw = Number.parseInt(String(validationConfig.max_patch_files ?? ""), 10);
  if (Number.isFinite(maxFilesRaw) && maxFilesRaw > 0 && uniquePaths.length > maxFilesRaw) {
    throw new PushSignedCommitsPolicyViolation(`${ERR_VALIDATION}: E003: Signed-commit payload exceeds max-patch-files (${maxFilesRaw}). ` + `Synthesized payload touches ${uniquePaths.length} file(s): ${uniquePaths.join(", ")}`);
  }

  const maxSizeKbRaw = Number.parseInt(String(validationConfig.max_patch_size ?? ""), 10);
  if (Number.isFinite(maxSizeKbRaw) && maxSizeKbRaw > 0) {
    const additionsBytes = additions.reduce((total, entry) => total + Buffer.from(entry.contents, "base64").length, 0);
    const additionsKb = Math.ceil(additionsBytes / 1024);
    if (additionsKb > maxSizeKbRaw) {
      throw new PushSignedCommitsPolicyViolation(`${ERR_VALIDATION}: E003: Signed-commit payload exceeds max-patch-size (${maxSizeKbRaw} KB). ` + `Synthesized payload additions total ${additionsKb} KB`);
    }
  }

  const protection = checkFileProtectionPostApply(uniquePaths, {
    ...validationConfig,
    protected_files_policy: validationConfig.protected_files_policy ?? "request_review",
  });
  // Only "deny" is fatal here: "fallback" and "request_review" are non-fatal outcomes that the
  // caller (create_pull_request.cjs) already decided to tolerate and handles after the push.
  if (protection.action === "deny") {
    throw new PushSignedCommitsPolicyViolation(`${ERR_VALIDATION}: Signed-commit payload violates file-protection policy (${protection.action}): ${protection.files.join(", ")}`);
  }
}

/**
 * Resolve the local HEAD SHA.
 *
 * @param {string} cwd
 * @returns {Promise<string>}
 */
async function resolveLocalHeadSha(cwd) {
  const { stdout } = await exec.getExecOutput("git", ["rev-parse", "HEAD"], { cwd });
  return stdout.trim();
}

/**
 * Pushes local commits to a remote branch using the GitHub GraphQL
 * `createCommitOnBranch` mutation so commits are cryptographically signed.
 * Falls back to `git push` if the GraphQL approach fails (e.g. on GHES).
 *
 * @param {object} opts
 * @param {any} opts.githubClient - Authenticated Octokit client with `.graphql()` and `.rest.git.createRef()`
 * @param {string} opts.owner - Repository owner
 * @param {string} opts.repo - Repository name
 * @param {string} opts.branch - Target branch name
 * @param {string} opts.baseRef - Git ref of the remote head before commits were applied (used for rev-list)
 * @param {string} opts.cwd - Working directory of the local git checkout
 * @param {any} [opts.gitAuthEnv] - Environment variables for git push fallback auth
 * @param {string} [opts.pushRemoteUrl] - Optional explicit git remote URL used for direct git push fallback
 * @param {string} [opts.pushToken] - Optional token used when pushing to pushRemoteUrl
 * @param {boolean} [opts.signedCommits=true] - When false, skip GraphQL signed commits and use git push directly
 * @param {boolean} [opts.allowGitPushFallback=true] - When false, refuse any fallback path that would use direct git push
 * @param {Record<string, any>} [opts.resolvedTemporaryIds] - Resolved temporary IDs map
 * @param {string} [opts.currentRepo] - Repository slug used for same-repo temporary ID resolution
 * @param {Record<string, any>} [opts.validationConfig] - Optional safe-output policy config applied to synthesized GraphQL fileChanges
 * @param {(context: {cwd: string, branch: string, output: string}) => (boolean|Promise<boolean>)} [opts.resolveRebaseConflict]
 * @returns {Promise<string | undefined>} SHA of the commit that landed on the target branch
 */
async function pushSignedCommits({
  githubClient,
  owner,
  repo,
  branch,
  baseRef,
  cwd,
  gitAuthEnv,
  pushRemoteUrl,
  pushToken,
  signedCommits = true,
  allowGitPushFallback = true,
  resolvedTemporaryIds,
  currentRepo,
  validationConfig,
  resolveRebaseConflict,
}) {
  const effectiveCurrentRepo = currentRepo || `${owner}/${repo}`;
  const temporaryIdMap = loadTemporaryIdMapFromResolved(resolvedTemporaryIds, {
    defaultRepo: effectiveCurrentRepo,
    validatePositiveIntegers: true,
    onInvalidNumber: (normalizedKey, rawValue) => {
      core.warning(`pushSignedCommits: ignoring invalid resolved temporary ID number for '${normalizedKey}': ${String(rawValue)}`);
    },
  });

  // The default parameter value converts undefined to true; this check tests only the explicit false value.
  if (signedCommits === false) {
    core.info(`pushSignedCommits: signed-commits disabled (using direct git push) for branch ${branch}`);
    const headSha = await pushBranchAndResolveHead({ branch, cwd, gitAuthEnv, pushRemoteUrl, pushToken });
    core.info(`pushSignedCommits: git push and HEAD resolution completed, HEAD=${headSha}`);
    return headSha;
  }

  // Orphan branch first push: baseRef is "" when push_experiment_state creates a brand-new
  // branch for the first time (checkoutOrCreateBranch returns "" for new branches).
  // The GraphQL createCommitOnBranch path cannot handle root commits (no parent to resolve),
  // so skip it entirely and fall directly through to git push.
  if (!baseRef) {
    if (allowGitPushFallback === false) {
      throw new Error(`${ERR_VALIDATION}: pushSignedCommits: cannot push branch '${branch}' without a baseRef when git push fallback is disabled. ` + `Seed the branch with a signed commit first, then retry.`);
    }
    core.info(`pushSignedCommits: empty baseRef detected (orphan branch first push), using git push directly for branch ${branch}`);
    try {
      const headSha = await pushBranchAndResolveHead({ branch, cwd, gitAuthEnv, pushRemoteUrl, pushToken });
      core.info(`pushSignedCommits: git push completed for orphan branch, HEAD=${headSha}`);
      return headSha;
    } catch (pushErr) {
      const pushErrMsg = getErrorMessage(pushErr);
      throw new Error(
        `${ERR_SYSTEM}: pushSignedCommits: failed to push orphan branch '${branch}' (first commit). ` +
          `If the repository requires signed commits, the branch must be seeded manually with a signed commit before this workflow can push to it. ` +
          `Run the following commands locally (requires a GPG key configured with Git):\n\n` +
          `  git switch --orphan ${branch}\n` +
          `  git commit --allow-empty -S -m "Initialize ${branch}"\n` +
          `  git push origin ${branch}\n\n` +
          `Original error: ${pushErrMsg}`,
        { cause: pushErr }
      );
    }
  }

  /** @type {string | undefined} */
  let baseRefOid;
  try {
    const { stdout: baseRefOut } = await exec.getExecOutput("git", ["rev-parse", `${baseRef}^{commit}`], { cwd });
    const trimmedBaseRefOid = baseRefOut.trim();
    if (OID_PATTERN.test(trimmedBaseRefOid)) {
      baseRefOid = trimmedBaseRefOid;
    } else if (trimmedBaseRefOid) {
      core.warning(
        `pushSignedCommits: git rev-parse returned an unexpected baseRef OID value for '${baseRef}'; ` +
          `boundary-commit filter is disabled for this run. Check that '${baseRef}' resolves to a valid commit in this checkout. ` +
          `Observed value: ${JSON.stringify(trimmedBaseRefOid)}`
      );
    }
  } catch (baseRefResolveError) {
    core.warning(`pushSignedCommits: could not resolve baseRef '${baseRef}' to OID; boundary-commit filter is disabled for this run and parent OID resolution may fall back to per-commit rev-parse: ${getErrorMessage(baseRefResolveError)}`);
  }
  // Collect the commits introduced (oldest-first) using topological order to ensure
  // correct sequencing even when commit dates are out of sync (e.g. after rebase --committer-date-is-author-date).
  // Using --parents emits each line as "<sha> <parent1> [<parent2> ...]", which lets us detect merge commits
  // (more than one parent) in a single subprocess call without iterating each SHA individually.
  const revListBase = baseRefOid ?? baseRef;
  const { stdout: revListOut } = await exec.getExecOutput("git", ["rev-list", "--parents", "--topo-order", "--reverse", `${revListBase}..HEAD`], { cwd });
  const revListEntriesRaw = revListOut
    .trim()
    .split("\n")
    .filter(Boolean)
    .map(line => {
      const fields = line.split(" ");
      return { line, fields, sha: fields[0] };
    });
  let revListEntries = baseRefOid !== undefined ? revListEntriesRaw.filter(entry => entry.sha !== baseRefOid) : revListEntriesRaw;
  const droppedBoundaryCount = revListEntriesRaw.length - revListEntries.length;
  if (baseRefOid !== undefined && droppedBoundaryCount > 0) {
    core.info(`pushSignedCommits: dropped ${droppedBoundaryCount} baseRef boundary commit(s) from replay set`);
  }
  let shas = revListEntries.map(entry => entry.sha);

  // When a genuine (non-recoverable) rebase conflict is hit below, the original un-rebased
  // commit range is pushed unsigned instead of being replayed through GraphQL. Rather than
  // returning immediately, that path sets this flag and falls through into the same
  // preflight validation (merge commit / unsupported file mode / file-protection policy
  // checks) that every other unsigned-push fallback goes through, so genuinely unsupported
  // or policy-blocked content still refuses the fallback instead of being pushed as-is.
  let unsignedPushFallbackReason;

  if (shas.length === 0) {
    core.info("pushSignedCommits: no new commits to push via GraphQL");
    return undefined;
  }

  let remoteHeadOid;
  try {
    remoteHeadOid = await lsRemoteHeadOid(branch, cwd, gitAuthEnv, pushRemoteUrl, pushToken);
  } catch {
    // Non-fatal. The existing branch-creation logic handles missing remote refs.
  }
  const firstReplayParentOid = revListEntries[0] && revListEntries[0].fields.length > 1 ? revListEntries[0].fields[1] : undefined;
  const firstGraphqlParentOid = remoteHeadOid || baseRefOid;
  let graphqlParentIsAncestorOfHead = true;
  if (firstGraphqlParentOid) {
    try {
      const ancestryCheck = await exec.getExecOutput("git", ["merge-base", "--is-ancestor", firstGraphqlParentOid, "HEAD"], { cwd, ignoreReturnCode: true });
      graphqlParentIsAncestorOfHead = ancestryCheck.exitCode === 0;
    } catch {
      // Ancestry probe failed — ignored, keep the default (true) and avoid rewrite.
    }
  }
  if (firstReplayParentOid && firstGraphqlParentOid && firstReplayParentOid !== firstGraphqlParentOid && !graphqlParentIsAncestorOfHead) {
    core.warning(`pushSignedCommits: replay parent ${firstReplayParentOid} does not match GraphQL parent ${firstGraphqlParentOid}; ` + `rebasing commit range before signed replay to avoid stale-base file synthesis`);
    // Attempt the rebase, capturing combined output so we can distinguish a
    // recoverable partial/shallow-clone object fetch failure from a genuine
    // merge conflict. On a shallow + partial checkout of a large monorepo the
    // rebase can fail because git lazily fetches intermediate-commit objects
    // from the promisor remote and the fetch is rejected. Pass gitAuthEnv so
    // those on-demand promisor fetches can authenticate in private repos even
    // after checkout credentials have been scrubbed from .git/config.
    const runRebase = async () => {
      const result = await exec.getExecOutput("git", ["rebase", "--onto", firstGraphqlParentOid, firstReplayParentOid, "HEAD"], { cwd, env: { ...process.env, ...(gitAuthEnv || {}) }, ignoreReturnCode: true });
      return result;
    };
    let rebaseResult = await runRebase();
    if (rebaseResult.exitCode !== 0) {
      const combinedOutput = `${rebaseResult.stdout || ""}\n${rebaseResult.stderr || ""}`;
      let rebaseResolved = false;
      if (!isPartialCloneObjectFailure(combinedOutput) && typeof resolveRebaseConflict === "function") {
        try {
          rebaseResolved = await resolveRebaseConflict({ cwd, branch, output: combinedOutput });
        } catch (resolveError) {
          core.warning(`pushSignedCommits: custom rebase conflict resolver failed: ${getErrorMessage(resolveError)}`);
        }
      }

      if (rebaseResolved) {
        rebaseResult = await exec.getExecOutput("git", ["rebase", "--continue"], {
          cwd,
          env: { ...process.env, ...(gitAuthEnv || {}), GIT_EDITOR: "true" },
          ignoreReturnCode: true,
        });
        if (rebaseResult.exitCode !== 0) {
          try {
            await exec.exec("git", ["rebase", "--abort"], { cwd });
          } catch {
            // Ignore cleanup failures.
          }
          const continueOutput = `${rebaseResult.stdout || ""}\n${rebaseResult.stderr || ""}`;
          throw new Error(`${ERR_SYSTEM}: pushSignedCommits: resolved a rebase conflict for branch '${branch}' but could not continue the rebase. ` + `Root cause: ${continueOutput.trim()}`);
        }
      } else if (isPartialCloneObjectFailure(combinedOutput)) {
        // Always abort the in-progress rebase before attempting recovery.
        try {
          await exec.exec("git", ["rebase", "--abort"], { cwd });
        } catch {
          // Ignore cleanup failures.
        }
        // Recovery: if the failure was caused by missing objects in a shallow/
        // partial clone, backfill the exact commit objects this rebase needs and
        // retry once.
        // Backfill the full object content (trees + blobs) of EXACTLY the
        // commits this rebase touches: the new GraphQL parent, the old replay
        // parent, and the current branch tip. Fetching these anchor commits
        // with `--no-filter` materializes the missing blobs for the reachable
        // range — without an uncontrolled `--unshallow` or unbounded deepen,
        // which would download a large monorepo's entire history.
        let headOid;
        try {
          const { stdout: headOut } = await exec.getExecOutput("git", ["rev-parse", "HEAD"], { cwd });
          headOid = headOut.trim();
        } catch {
          // HEAD cannot be resolved — ignored, fall back to the known range anchors.
        }
        const fetchTargets = [firstGraphqlParentOid, firstReplayParentOid, headOid].filter(sha => typeof sha === "string" && sha.length > 0);
        core.warning(`pushSignedCommits: rebase failed due to missing objects in a shallow/partial clone; ` + `backfilling object content for ${fetchTargets.length} anchor commit(s) directly from origin by SHA and retrying once`);
        let recovered = false;
        try {
          recovered = await backfillCommitObjects(exec, fetchTargets, { cwd, env: { ...process.env, ...(gitAuthEnv || {}) } });
        } catch (recoveryError) {
          core.warning(`pushSignedCommits: targeted object backfill failed: ${getErrorMessage(recoveryError)}`);
        }

        if (recovered) {
          rebaseResult = await runRebase();
          if (rebaseResult.exitCode !== 0) {
            const retryOutput = `${rebaseResult.stdout || ""}\n${rebaseResult.stderr || ""}`;
            try {
              await exec.exec("git", ["rebase", "--abort"], { cwd });
            } catch (abortError) {
              throw new Error(
                `${ERR_SYSTEM}: pushSignedCommits: failed to rebase commit range onto current GraphQL parent (${firstGraphqlParentOid}) even after backfilling the required commit objects, ` +
                  `and 'git rebase --abort' also failed to restore branch '${branch}' to its original state. ` +
                  `Root cause: ${retryOutput.trim()}. Abort failure: ${getErrorMessage(abortError)}`,
                { cause: abortError }
              );
            }
            if (isPartialCloneObjectFailure(retryOutput)) {
              // Still a missing-object failure even after the targeted backfill — the shallow/
              // partial clone genuinely cannot supply what this rebase needs, so there is no
              // valid replay range to fall back to unsigned; this remains fatal.
              throw new Error(
                `${ERR_SYSTEM}: pushSignedCommits: failed to rebase commit range onto current GraphQL parent (${firstGraphqlParentOid}) even after backfilling the required commit objects. ` +
                  `Resolve conflicts by rebasing/cherry-picking locally and retry. Root cause: ${retryOutput.trim()}`
              );
            }
            // The backfill succeeded (the objects needed to attempt the rebase are now present),
            // but retrying the rebase still failed — this is a genuine content conflict that was
            // only revealed once the missing objects were available, not a partial-clone object
            // issue. Route it through the same unsigned-push fallback as any other genuine
            // conflict below, instead of unconditionally throwing.
            const diagnosticMessage =
              `${ERR_SYSTEM}: pushSignedCommits: failed to rebase commit range onto current GraphQL parent (${firstGraphqlParentOid}) even after backfilling the required commit objects. ` + `Root cause: ${retryOutput.trim()}`;
            if (allowGitPushFallback === false) {
              throw new Error(`${ERR_SYSTEM}: ${diagnosticMessage} Resolve conflicts by rebasing/cherry-picking locally and retry.`);
            }
            unsignedPushFallbackReason = diagnosticMessage;
          }
        } else {
          throw new Error(
            `${ERR_SYSTEM}: pushSignedCommits: failed to rebase commit range onto current GraphQL parent (${firstGraphqlParentOid}); ` +
              `the required commit objects could not be backfilled from a shallow/partial clone. ` +
              `Resolve conflicts by rebasing/cherry-picking locally and retry. Root cause: ${combinedOutput.trim()}`
          );
        }
      } else {
        try {
          await exec.exec("git", ["rebase", "--abort"], { cwd });
        } catch (abortError) {
          // If the abort itself fails, HEAD can be left in a detached, partially-rebased or
          // still-conflicted state. Continuing from there (falling through to the unsigned push
          // fallback, or even the strict throw below) would risk operating on / pushing that
          // broken worktree state, so this must be fatal rather than silently ignored.
          throw new Error(
            `${ERR_SYSTEM}: pushSignedCommits: failed to rebase commit range onto current GraphQL parent (${firstGraphqlParentOid}), and 'git rebase --abort' also failed to restore branch '${branch}' to its original state. ` +
              `Root cause: ${combinedOutput.trim()}. Abort failure: ${getErrorMessage(abortError)}`,
            { cause: abortError }
          );
        }
        const diagnosticMessage = `${ERR_SYSTEM}: pushSignedCommits: failed to rebase commit range onto current GraphQL parent (${firstGraphqlParentOid}). ` + `Root cause: ${combinedOutput.trim()}`;
        if (allowGitPushFallback === false) {
          throw new Error(`${ERR_SYSTEM}: ${diagnosticMessage} Resolve conflicts by rebasing/cherry-picking locally and retry.`);
        }
        // Genuine merge conflict (not a shallow/partial-clone object-fetch issue, and no custom
        // resolver handled it): rebasing the commit range onto the current base cannot be done
        // automatically, and replaying the stale-base commits through GraphQL would silently
        // synthesize file content against the wrong parent. Rather than failing the whole
        // operation (which previously forced callers to fall back to opening an issue instead
        // of a pull request), fall through to push the ORIGINAL un-rebased commits directly via
        // unsigned `git push` once they pass the same preflight validation (merge commit /
        // unsupported file mode / file-protection policy) as any other unsigned push fallback.
        // GitHub will still create the pull request; it will simply report the branch as having
        // conflicts that need to be resolved, the same as any normal PR.
        // `rebase --abort` (above) already restored HEAD and the branch to their original,
        // un-rebased state, so revListEntries/shas (computed before the rebase attempt) still
        // describe exactly what will be pushed — no need to recompute them.
        unsignedPushFallbackReason = diagnosticMessage;
      }
    }
    if (!unsignedPushFallbackReason) {
      const { stdout: rebasedRevListOut } = await exec.getExecOutput("git", ["rev-list", "--parents", "--topo-order", "--reverse", `${firstGraphqlParentOid}..HEAD`], { cwd });
      revListEntries = rebasedRevListOut
        .trim()
        .split("\n")
        .filter(Boolean)
        .map(line => {
          const fields = line.split(" ");
          return { line, fields, sha: fields[0] };
        });
      shas = revListEntries.map(entry => entry.sha);
      if (shas.length === 0) {
        core.info("pushSignedCommits: no new commits to replay after rebase");
        return undefined;
      }
    }
  }

  core.info(`pushSignedCommits: replaying ${shas.length} commit(s) via GraphQL createCommitOnBranch (branch: ${branch}, repo: ${owner}/${repo})`);

  try {
    // Pre-flight check: detect merge commits. Each --parents output line is "<sha> <parent1> [<parent2> ...]".
    // A line with 3+ space-separated fields means the commit has 2+ parents (i.e. a merge commit).
    // The GitHub GraphQL createCommitOnBranch mutation does not support multiple parents, so refuse
    // the unsigned push fallback if any merge commit is found.
    for (const { fields } of revListEntries) {
      if (fields.length > 2) {
        const sha = fields[0];
        core.warning(`pushSignedCommits: merge commit ${sha} detected, refusing unsigned push fallback`);
        throw new PushSignedCommitsUnsupportedShape(`${ERR_VALIDATION}: merge commit detected`);
      }
    }

    // Pre-scan ALL commits: collect file changes and check for unsupported file modes
    // BEFORE starting any GraphQL mutations. If a symlink is found mid-loop after some
    // commits have already been signed, the remote branch diverges and the git push
    // fallback would be rejected as non-fast-forward.
    //
    // The GitHub GraphQL createCommitOnBranch mutation only supports regular file mode 100644:
    //   - Symlinks (120000) would be silently converted to regular files containing the link target path
    //   - Executable bits (100755) are silently dropped
    //   - Submodules/gitlinks (160000) are not supported; the mutation does not accept commit-object entries
    /** @type {Map<string, Array<{path: string, contents: string}>>} */
    const additionsMap = new Map();
    /** @type {Map<string, Array<{path: string}>>} */
    const deletionsMap = new Map();

    for (const sha of shas) {
      /** @type {Array<{path: string, contents: string}>} */
      const additions = [];
      /** @type {Array<{path: string}>} */
      const deletions = [];

      // Use git diff-tree --raw to obtain file mode information per changed file.
      // Format: :<srcMode> <dstMode> <srcHash> <dstHash> <status>[score]\t<path>[<\t><newPath>]
      // Fields: [0]=srcMode, [1]=dstMode, [2]=srcHash, [3]=dstHash, [4]=status
      const { stdout: rawDiffOut } = await exec.getExecOutput("git", ["diff-tree", "-r", "--raw", sha], { cwd });

      for (const line of rawDiffOut.trim().split("\n").filter(Boolean)) {
        // Raw format lines start with ':'; skip the commit SHA header line and any other non-raw lines
        if (!line.startsWith(":")) continue;

        const tabIdx = line.indexOf("\t");
        if (tabIdx === -1) continue;

        const modeFields = line.slice(1, tabIdx).split(" "); // strip leading ':'
        if (modeFields.length < 5) {
          core.warning(`pushSignedCommits: unexpected diff-tree output format, skipping line: ${line}`);
          continue;
        }
        const srcMode = modeFields[0]; // source file mode (e.g. 100644, 100755, 120000, 160000)
        const dstMode = modeFields[1]; // destination file mode (e.g. 100644, 100755, 120000, 160000)
        const dstHash = modeFields[3]; // destination blob hash (object ID of the file in this commit)
        const status = modeFields[4]; // A=Added, M=Modified, D=Deleted, R=Renamed, C=Copied

        const paths = line.slice(tabIdx + 1).split("\t");
        const filePath = unquoteCPath(paths[0]);

        if (status === "D") {
          // mode 160000 = gitlink (submodule); GitHub GraphQL createCommitOnBranch does not support submodules
          if (srcMode === "160000") {
            core.warning(`pushSignedCommits: submodule change detected in ${filePath}, refusing unsigned push fallback`);
            throw new PushSignedCommitsUnsupportedShape(`${ERR_VALIDATION}: submodule change detected`);
          }
          deletions.push({ path: filePath });
        } else if (status && status.startsWith("R")) {
          // Rename: source path is deleted, destination path is added
          const renamedPath = unquoteCPath(paths[1]);
          if (!renamedPath) {
            core.warning(`pushSignedCommits: rename entry missing destination path, skipping: ${line}`);
            continue;
          }
          deletions.push({ path: filePath });
          if (srcMode === "160000" || dstMode === "160000") {
            core.warning(`pushSignedCommits: submodule change detected in ${filePath} -> ${renamedPath}, refusing unsigned push fallback`);
            throw new PushSignedCommitsUnsupportedShape(`${ERR_VALIDATION}: submodule change detected`);
          }
          if (dstMode === "120000") {
            core.warning(`pushSignedCommits: symlink ${renamedPath} cannot be pushed as a signed commit, refusing unsigned push fallback`);
            throw new PushSignedCommitsUnsupportedShape(`${ERR_VALIDATION}: symlink file mode requires git push fallback`);
          }
          if (dstMode === "100755") {
            core.warning(`pushSignedCommits: executable bit on ${renamedPath} will be lost in signed commit (GitHub GraphQL does not support mode 100755)`);
          }
          const blobContents = await readBlobAsBase64(dstHash, cwd);
          additions.push({ path: renamedPath, contents: maybeReplaceTemporaryIdsInBase64Content(blobContents, temporaryIdMap, effectiveCurrentRepo, renamedPath) });
        } else if (status && status.startsWith("C")) {
          // Copy: source path is kept (no deletion), only the destination path is added
          const copiedPath = unquoteCPath(paths[1]);
          if (!copiedPath) {
            core.warning(`pushSignedCommits: copy entry missing destination path, skipping: ${line}`);
            continue;
          }
          if (dstMode === "160000") {
            core.warning(`pushSignedCommits: submodule change detected in ${copiedPath}, refusing unsigned push fallback`);
            throw new PushSignedCommitsUnsupportedShape(`${ERR_VALIDATION}: submodule change detected`);
          }
          if (dstMode === "120000") {
            core.warning(`pushSignedCommits: symlink ${copiedPath} cannot be pushed as a signed commit, refusing unsigned push fallback`);
            throw new PushSignedCommitsUnsupportedShape(`${ERR_VALIDATION}: symlink file mode requires git push fallback`);
          }
          if (dstMode === "100755") {
            core.warning(`pushSignedCommits: executable bit on ${copiedPath} will be lost in signed commit (GitHub GraphQL does not support mode 100755)`);
          }
          const blobContents = await readBlobAsBase64(dstHash, cwd);
          additions.push({ path: copiedPath, contents: maybeReplaceTemporaryIdsInBase64Content(blobContents, temporaryIdMap, effectiveCurrentRepo, copiedPath) });
        } else {
          // Added or Modified
          if (dstMode === "160000") {
            core.warning(`pushSignedCommits: submodule change detected in ${filePath}, refusing unsigned push fallback`);
            throw new PushSignedCommitsUnsupportedShape(`${ERR_VALIDATION}: submodule change detected`);
          }
          if (dstMode === "120000") {
            core.warning(`pushSignedCommits: symlink ${filePath} cannot be pushed as a signed commit, refusing unsigned push fallback`);
            throw new PushSignedCommitsUnsupportedShape(`${ERR_VALIDATION}: symlink file mode requires git push fallback`);
          }
          if (dstMode === "100755") {
            core.warning(`pushSignedCommits: executable bit on ${filePath} will be lost in signed commit (GitHub GraphQL does not support mode 100755)`);
          }
          const blobContents = await readBlobAsBase64(dstHash, cwd);
          additions.push({ path: filePath, contents: maybeReplaceTemporaryIdsInBase64Content(blobContents, temporaryIdMap, effectiveCurrentRepo, filePath) });
        }
      }

      additionsMap.set(sha, additions);
      deletionsMap.set(sha, deletions);
    }

    // Enforce file-protection/size policy for every commit up front — before choosing between
    // an unsigned push fallback and the GraphQL replay below — so a validationConfig-blocked
    // file always refuses the unsigned push fallback, not just the GraphQL path.
    for (const sha of shas) {
      validateSynthesizedFileChanges(additionsMap.get(sha) || [], deletionsMap.get(sha) || [], validationConfig);
    }

    if (unsignedPushFallbackReason) {
      // All commits in the original, un-rebased range passed the same merge-commit,
      // unsupported-file-mode, and file-protection-policy checks as the GraphQL replay path
      // above; push them directly via unsigned `git push` instead of replaying through GraphQL
      // (which would synthesize file content against the wrong parent).
      core.warning(`${unsignedPushFallbackReason} Falling back to an unsigned git push of the un-rebased commit(s) so the pull request can still be created (it will show as having merge conflicts with the base branch).`);
      try {
        const fallbackSha = await pushBranchAndResolveHead({ branch, cwd, gitAuthEnv, pushRemoteUrl, pushToken });
        core.info(`pushSignedCommits: unsigned git push fallback (unresolved rebase conflict) completed, using pushed SHA ${fallbackSha}`);
        return fallbackSha;
      } catch (pushError) {
        // The unsigned push itself can be rejected (e.g. by branch protection rules requiring
        // signed commits). Surface a clear, combined error instead of letting the raw git-push
        // failure — or a misleading "success" — propagate; this preserves the original
        // throw-on-conflict behavior for repos where an unsigned push is not a viable fallback.
        throw new PushSignedCommitsUnsignedFallbackFailed(`${ERR_SYSTEM}: ${unsignedPushFallbackReason} Unsigned git push fallback was also rejected: ${getErrorMessage(pushError)}`, { cause: pushError });
      }
    }

    // All commits passed the mode checks. Replay via GraphQL.
    /** @type {string | undefined} */
    let lastOid;
    for (let i = 0; i < shas.length; i++) {
      const sha = shas[i];
      core.info(`pushSignedCommits: processing commit ${i + 1}/${shas.length} sha=${sha}`);

      // Determine the expected HEAD OID for this commit.
      // After the first signed commit, reuse the OID returned by the previous GraphQL
      // mutation instead of re-querying ls-remote (works even if the branch is new).
      let expectedHeadOid;
      if (lastOid) {
        expectedHeadOid = lastOid;
        core.info(`pushSignedCommits: using chained OID from previous mutation: ${expectedHeadOid}`);
      } else {
        // First commit: check whether the branch already exists on the remote.
        const oidResult = await lsRemoteHeadOid(branch, cwd, gitAuthEnv, pushRemoteUrl, pushToken);
        expectedHeadOid = oidResult || "";
        if (!expectedHeadOid) {
          // Branch does not exist on the remote yet.
          // createCommitOnBranch requires the branch to already exist – it does NOT auto-create branches.
          // Resolve the parent OID, create the branch on the remote via the REST API,
          // then proceed with the signed-commit mutation as normal.
          core.info(`pushSignedCommits: branch ${branch} not yet on the remote, resolving parent OID for first commit`);
          if (baseRefOid !== undefined) {
            expectedHeadOid = baseRefOid;
            core.info(`pushSignedCommits: using baseRef OID for initial branch creation: ${expectedHeadOid}`);
          } else {
            const { stdout: parentOut } = await exec.getExecOutput("git", ["rev-parse", `${sha}^`], { cwd });
            expectedHeadOid = parentOut.trim();
          }
          if (!expectedHeadOid) {
            throw new Error(`${ERR_API}: Could not resolve OID for new branch ${branch}`);
          }
          core.info(`pushSignedCommits: creating remote branch ${branch} at parent OID ${expectedHeadOid}`);
          try {
            await githubClient.rest.git.createRef({
              owner,
              repo,
              ref: `refs/heads/${branch}`,
              sha: expectedHeadOid,
            });
            core.info(`pushSignedCommits: remote branch ${branch} created successfully`);
          } catch (createRefError) {
            /** @type {any} */
            const err = createRefError;
            const status = err && typeof err === "object" ? err.status : undefined;
            const message = err && typeof err === "object" ? String(err.message || "") : "";
            // If the branch was created concurrently between our ls-remote check and this call,
            // GitHub returns 422 "Reference refs/heads/<branch> already exists". Treat that as success and continue.
            if (status === 422 && /reference.*already exists/i.test(message)) {
              core.info(`pushSignedCommits: remote branch ${branch} was created concurrently (422 Reference already exists); continuing with signed commits`);
              const refreshedOid = await lsRemoteHeadOid(branch, cwd, gitAuthEnv, pushRemoteUrl, pushToken);
              if (!refreshedOid) {
                throw new Error(`${ERR_API}: Could not resolve remote branch OID for ${branch} after concurrent creation`);
              }
              expectedHeadOid = refreshedOid;
            } else {
              throw createRefError;
            }
          }
        } else {
          core.info(`pushSignedCommits: using remote HEAD OID from ls-remote: ${expectedHeadOid}`);
        }
      }

      // Full commit message (subject + body)
      const { stdout: msgOut } = await exec.getExecOutput("git", ["log", "-1", "--format=%B", sha], { cwd });
      const message = msgOut.trim();
      const headline = message.split("\n")[0];
      const body = message.split("\n").slice(1).join("\n").trim();
      core.info(`pushSignedCommits: commit message headline: "${headline}"`);

      const additions = additionsMap.get(sha) || [];
      const deletions = deletionsMap.get(sha) || [];
      core.info(`pushSignedCommits: file changes: ${additions.length} addition(s), ${deletions.length} deletion(s)`);

      /** @type {any} */
      const input = {
        branch: { repositoryNameWithOwner: `${owner}/${repo}`, branchName: branch },
        message: { headline, ...(body ? { body } : {}) },
        fileChanges: { additions, deletions },
        expectedHeadOid,
      };

      core.info(`pushSignedCommits: calling createCommitOnBranch mutation (expectedHeadOid=${expectedHeadOid})`);
      const result = await githubClient.graphql(
        `mutation($input: CreateCommitOnBranchInput!) {
          createCommitOnBranch(input: $input) { commit { oid } }
        }`,
        { input }
      );
      const newOid = result && result.createCommitOnBranch && result.createCommitOnBranch.commit ? result.createCommitOnBranch.commit.oid : undefined;
      if (typeof newOid !== "string" || newOid.length === 0) {
        throw new Error(`${ERR_API}: GraphQL createCommitOnBranch did not return a valid commit OID`);
      }
      lastOid = newOid;
      core.info(`pushSignedCommits: signed commit created: ${lastOid}`);
    }
    core.info(`pushSignedCommits: all ${shas.length} commit(s) pushed as signed commits`);
    return lastOid ?? shas[shas.length - 1];
  } catch (err) {
    if (err instanceof PushSignedCommitsUnsignedFallbackFailed) {
      // The unsigned push fallback for a genuine rebase conflict was already attempted (and
      // rejected) above; re-throw as-is instead of retrying the same push again below.
      throw err;
    }
    if (err instanceof PushSignedCommitsUnsupportedShape) {
      throw new Error(
        `${ERR_VALIDATION}: pushSignedCommits: refusing unsigned push for branch '${branch}': ${stripLeadingErrorCode(getErrorMessage(err))}. ` +
          `GitHub's createCommitOnBranch GraphQL mutation cannot represent merge commits, symlinks (mode 120000), ` +
          `submodule entries (mode 160000), or executable bits (mode 100755). ` +
          `Rewrite the commits to use only regular files (mode 100644) with no merge commits, ` +
          `or set signed-commits: false if the repository does not require signed commits.`,
        { cause: err }
      );
    }
    if (err instanceof PushSignedCommitsPolicyViolation) {
      throw new Error(`${ERR_VALIDATION}: pushSignedCommits: refusing unsigned push for branch '${branch}': ${stripLeadingErrorCode(getErrorMessage(err))}`, { cause: err });
    }
    if (allowGitPushFallback === false) {
      throw new Error(`${ERR_API}: pushSignedCommits: signed commit push failed for branch '${branch}' and git push fallback is disabled: ${getErrorMessage(err)}`, { cause: err });
    }
    core.warning(`pushSignedCommits: GraphQL signed push failed, falling back to git push: ${getErrorMessage(err)}`);
    const fallbackSha = await pushBranchAndResolveHead({ branch, cwd, gitAuthEnv, pushRemoteUrl, pushToken });
    core.info(`pushSignedCommits: git push fallback completed, using pushed SHA ${fallbackSha}`);
    return fallbackSha;
  }
}

module.exports = { pushSignedCommits, unquoteCPath };
