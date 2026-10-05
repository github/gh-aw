// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Check for a stale workflow lock file using frontmatter hash comparison.
 * This script verifies that the stored frontmatter hash in the lock file
 * matches the recomputed hash from the source .md file, detecting cases where
 * the workflow was edited without recompiling the lock file. It does not
 * provide tamper protection — use code review to guard against intentional
 * modifications.
 *
 * Supports both same-repo and cross-repo reusable workflow scenarios:
 * - Primary: GitHub API (uses GITHUB_WORKFLOW_REF to identify source repo)
 * - Fallback: local filesystem ($GITHUB_WORKSPACE) when API access is unavailable
 *   (e.g., cross-org reusable workflows where the caller token can't read the source repo)
 */

const fs = require("fs");
const path = require("path");
const { getErrorMessage } = require("./error_helpers.cjs");
const { extractHashFromLockFile, extractBodyHashFromLockFile, computeFrontmatterHash, computeBodyHash, createGitHubFileReader } = require("./frontmatter_hash_pure.cjs");
const { getFileContent } = require("./github_api_helpers.cjs");
const { ERR_CONFIG, SAFE_OUTPUT_E009, CONFIG_HASH_MISMATCH } = require("./error_codes.cjs");

// Matches GitHub workflow ref paths of the form "owner/repo/...[@ref]"
// and captures: [1] owner, [2] repo, [3] optional ref
const GITHUB_REPO_PATH_RE = /^([^/]+)\/([^/]+)\/.+?(?:@(.+))?$/;

async function main() {
  const workflowFile = process.env.GH_AW_WORKFLOW_FILE;

  if (!workflowFile) {
    core.setFailed(`${ERR_CONFIG}: Configuration error: GH_AW_WORKFLOW_FILE not available.`);
    return;
  }

  // Determine if full stale check mode is enabled (checks both frontmatter and body hashes).
  const fullCheckMode = process.env.GH_AW_STALE_CHECK_FULL === "true";

  // Construct file paths
  const workflowBasename = workflowFile.replace(".lock.yml", "");
  const workflowMdPath = `.github/workflows/${workflowBasename}.md`;
  const lockFilePath = `.github/workflows/${workflowFile}`;

  core.info(`Checking for stale lock file using ${fullCheckMode ? "frontmatter + body" : "frontmatter"} hash:`);
  core.info(`  Source: ${workflowMdPath}`);
  core.info(`  Lock file: ${lockFilePath}`);

  // Determine workflow source repository from the workflow ref for cross-repo support.
  //
  // For cross-repo reusable workflow invocations, both GITHUB_WORKFLOW_REF (env var) and
  // ${{ github.workflow_ref }} (injected as GH_AW_CONTEXT_WORKFLOW_REF) resolve to the
  // TOP-LEVEL CALLER's workflow, not the reusable workflow being executed. This causes the
  // script to look for lock files in the wrong repository when used alone.
  //
  // The reliable fix is the referenced_workflows API lookup below, which identifies the
  // callee's repo/ref from the caller's run object. GH_AW_CONTEXT_WORKFLOW_REF is only
  // used as a fallback when the API lookup is unavailable or finds no matching entry.
  //
  // Refs: https://github.com/github/gh-aw/issues/23935
  //       https://github.com/github/gh-aw/issues/24422
  const workflowEnvRef = process.env.GH_AW_CONTEXT_WORKFLOW_REF || process.env.GITHUB_WORKFLOW_REF || "";
  const currentRepo = process.env.GITHUB_REPOSITORY || `${context.repo.owner}/${context.repo.repo}`;

  // Parse owner, repo, and optional ref from GITHUB_WORKFLOW_REF as a single unit so that
  // repo and ref are always consistent with each other.  The @ref segment may be absent (e.g.
  // when the env var was set without a ref suffix), so treat it as optional.
  const workflowRefMatch = workflowEnvRef.match(GITHUB_REPO_PATH_RE);

  // Use the workflow source repo if parseable, otherwise fall back to context.repo
  let owner = workflowRefMatch ? workflowRefMatch[1] : context.repo.owner;
  let repo = workflowRefMatch ? workflowRefMatch[2] : context.repo.repo;
  let workflowRepo = `${owner}/${repo}`;

  // Determine ref in a way that keeps repo+ref consistent:
  //   - If a ref is present in GITHUB_WORKFLOW_REF, use it.
  //   - For same-repo runs without a parsed ref, fall back to context.sha (existing behavior).
  //   - For cross-repo runs without a parsed ref, omit ref so the API uses the default branch
  //     (avoids mixing source repo owner/name with a SHA that only exists in the triggering repo).
  let ref;
  if (workflowRefMatch && workflowRefMatch[3]) {
    ref = workflowRefMatch[3];
  } else if (workflowRepo === currentRepo) {
    ref = context.sha;
  } else {
    ref = undefined;
  }

  // Attempt referenced_workflows API lookup to detect cross-repo callee repo/ref.
  //
  // IMPORTANT: GITHUB_EVENT_NAME inside a reusable workflow reflects the ORIGINAL trigger
  // event (e.g., "push", "issues"), NOT "workflow_call". We therefore cannot rely on event
  // name to detect cross-repo scenarios.
  //
  // Similarly, GH_AW_CONTEXT_WORKFLOW_REF (${{ github.workflow_ref }}) resolves to the
  // CALLER's workflow ref, not the callee's. It is used as a fallback only when the API
  // lookup is unavailable or finds no matching entry.
  //
  // Resolution priority:
  //   1. referenced_workflows[].sha  — immutable commit SHA from the callee repo (most precise).
  //   2. referenced_workflows[].ref  — branch/tag ref from the callee (fallback when sha absent).
  //   3. GH_AW_CONTEXT_WORKFLOW_REF  — injected by the compiler; used when the API is unavailable
  //      or when no matching entry is found in referenced_workflows.
  //
  // When a reusable workflow is called from another repo, GITHUB_RUN_ID and GITHUB_REPOSITORY
  // are set to the caller's run ID and repo. The caller's run object includes a
  // referenced_workflows array listing the callee's exact path, sha, and ref.
  //
  // Short-circuit: if the env workflow ref already ends with the current workflow file,
  // the env vars already correctly identify the source (same-repo or non-reusable run).
  // Skip the API call to avoid unnecessary rate-limit usage and permission noise.
  //
  // GITHUB_RUN_ID is always set in GitHub Actions environments.
  // context.runId is a fallback for environments where env vars are absent.
  //
  // Refs: https://github.com/github/gh-aw/issues/24422
  const runId = parseInt(process.env.GITHUB_RUN_ID || String(context.runId), 10);
  const envRefWithoutAt = workflowEnvRef.replace(/@.*$/, "");
  const envRefMatchesWorkflow = envRefWithoutAt.endsWith(`/.github/workflows/${workflowFile}`);

  if (envRefMatchesWorkflow) {
    core.info("Env workflow ref already identifies this workflow, skipping referenced_workflows API lookup");
  } else if (Number.isFinite(runId)) {
    const [runOwner, runRepo] = currentRepo.split("/");
    try {
      core.info(`Checking for cross-repo callee via referenced_workflows API (run ${runId})`);
      const runResponse = await github.rest.actions.getWorkflowRun({
        owner: runOwner,
        repo: runRepo,
        run_id: runId,
      });

      const referencedWorkflows = runResponse.data.referenced_workflows || [];
      core.info(`Found ${referencedWorkflows.length} referenced workflow(s) in run`);

      // Find the entry whose path matches the current workflow file.
      // Path format: "org/repo/.github/workflows/file.lock.yml@ref"
      // Using replace to robustly strip the optional @ref suffix before matching.
      const matchingEntry = referencedWorkflows.find(wf => {
        const pathWithoutRef = wf.path.replace(/@.*$/, "");
        return pathWithoutRef.endsWith(`/.github/workflows/${workflowFile}`);
      });

      if (matchingEntry) {
        const pathMatch = matchingEntry.path.match(GITHUB_REPO_PATH_RE);
        if (pathMatch) {
          owner = pathMatch[1];
          repo = pathMatch[2];
          // Prefer sha (immutable) over ref (branch/tag can drift) over path-parsed ref.
          ref = matchingEntry.sha || matchingEntry.ref || pathMatch[3];
          workflowRepo = `${owner}/${repo}`;
          core.info(`Resolved callee repo from referenced_workflows: ${owner}/${repo} @ ${ref || "(default branch)"}`);
          core.info(`  Referenced workflow path: ${matchingEntry.path}`);
        }
      } else {
        core.info(`No matching entry in referenced_workflows for "${workflowFile}", falling back to GH_AW_CONTEXT_WORKFLOW_REF`);
      }
    } catch (error) {
      core.info(`Could not fetch referenced_workflows from API: ${getErrorMessage(error)}, falling back to GH_AW_CONTEXT_WORKFLOW_REF`);
    }
  } else {
    core.info("Run ID is unavailable or invalid, falling back to GH_AW_CONTEXT_WORKFLOW_REF");
  }

  const contextWorkflowRef = process.env.GH_AW_CONTEXT_WORKFLOW_REF;
  core.info(`GITHUB_WORKFLOW_REF: ${process.env.GITHUB_WORKFLOW_REF || "(not set)"}`);
  if (contextWorkflowRef) {
    core.info(`GH_AW_CONTEXT_WORKFLOW_REF: ${contextWorkflowRef} (available as env fallback)`);
  }
  core.info(`GITHUB_REPOSITORY: ${currentRepo}`);
  core.info(`Resolved source repo: ${owner}/${repo} @ ${ref || "(default branch)"}`);

  if (workflowRepo !== currentRepo) {
    core.info(`Cross-repo invocation detected: workflow source is "${workflowRepo}", current repo is "${currentRepo}"`);
  } else {
    core.info(`Same-repo invocation: checking out ${workflowRepo} @ ${ref}`);
  }

  // Fallback: compare frontmatter hashes using local filesystem files.
  // Used when the GitHub API is inaccessible (e.g., cross-org reusable workflow where
  // the caller's GITHUB_TOKEN cannot read the source repo).
  // The activation job's "Checkout .github and .agents folders" step always runs before
  // this check and places the workflow source files in $GITHUB_WORKSPACE, so the local
  // files are always available at this point.

  /**
   * Compares the stored body hash in the lock file content against the body hash
   * recomputed from the workflow source. Returns null if the lock file predates
   * body hash support.
   * @param {string} lockFileContent - Content of the .lock.yml file
   * @param {string} mdPath - Path to the .md file to compute the body hash from
   * @param {Object} [options] - Options forwarded to computeBodyHash
   * @param {Function} [options.fileReader] - Custom file reader (defaults to local filesystem)
   * @param {string} [label] - Optional label appended to log lines for context (e.g. "local filesystem fallback")
   * @returns {Promise<{match: boolean, storedHash: string, recomputedHash: string, hashKind: string} | null>}
   */
  async function compareBodyHashes(lockFileContent, mdPath, { fileReader } = {}, label) {
    const suffix = label ? ` (${label})` : "";
    const storedBodyHash = extractBodyHashFromLockFile(lockFileContent);
    if (!storedBodyHash) {
      core.info(`No body hash found in lock file; skipping body hash check${suffix} (lock file may predate body hash support)`);
      return null;
    }
    const recomputedBodyHash = await computeBodyHash(mdPath, { fileReader });
    const match = storedBodyHash === recomputedBodyHash;
    core.info(`Body hash comparison${suffix}:`);
    core.info(`  Lock file body hash:    ${storedBodyHash}`);
    core.info(`  Recomputed body hash:   ${recomputedBodyHash}`);
    core.info(`  Status: ${match ? "✅ Body hashes match" : "⚠️  Body hashes differ"}`);
    return { match, storedHash: storedBodyHash, recomputedHash: recomputedBodyHash, hashKind: "body" };
  }

  async function compareFrontmatterHashesFromLocalFiles() {
    const workspace = process.env.GITHUB_WORKSPACE;
    if (!workspace) {
      core.info("GITHUB_WORKSPACE not available for local filesystem fallback");
      return null;
    }

    // Resolve and validate both paths to prevent path traversal attacks.
    // GH_AW_WORKFLOW_FILE could theoretically contain "../" segments; reject any
    // resolved path that escapes the workspace/.github/workflows directory.
    const allowedDir = path.resolve(workspace, ".github", "workflows");
    const localLockFilePath = path.resolve(workspace, lockFilePath);
    const localMdFilePath = path.resolve(workspace, workflowMdPath);

    if (!localLockFilePath.startsWith(allowedDir + path.sep) && localLockFilePath !== allowedDir) {
      core.info(`Resolved lock file path escapes workspace: ${localLockFilePath}`);
      return null;
    }
    if (!localMdFilePath.startsWith(allowedDir + path.sep) && localMdFilePath !== allowedDir) {
      core.info(`Resolved source file path escapes workspace: ${localMdFilePath}`);
      return null;
    }

    core.info(`Attempting local filesystem fallback for hash comparison:`);
    core.info(`  Lock file: ${localLockFilePath}`);
    core.info(`  Source: ${localMdFilePath}`);

    if (!fs.existsSync(localLockFilePath)) {
      core.info(`Local lock file not found: ${localLockFilePath}`);
      return null;
    }

    if (!fs.existsSync(localMdFilePath)) {
      core.info(`Local source file not found: ${localMdFilePath}`);
      return null;
    }

    try {
      const localLockContent = fs.readFileSync(localLockFilePath, "utf8");
      const storedHash = extractHashFromLockFile(localLockContent);
      if (!storedHash) {
        core.info("No frontmatter hash found in local lock file");
        return null;
      }

      // computeFrontmatterHash uses the local filesystem reader by default
      const recomputedHash = await computeFrontmatterHash(localMdFilePath);

      const match = storedHash === recomputedHash;

      core.info(`Frontmatter hash comparison (local filesystem fallback):`);
      core.info(`  Lock file hash:    ${storedHash}`);
      core.info(`  Recomputed hash:   ${recomputedHash}`);
      core.info(`  Status: ${match ? "✅ Hashes match" : "⚠️  Hashes differ"}`);

      // When full check mode is enabled, also compare body hashes.
      if (match && fullCheckMode) {
        const bodyComparison = await compareBodyHashes(localLockContent, localMdFilePath, undefined, "local filesystem fallback");
        if (bodyComparison) return bodyComparison;
      }

      return { match, storedHash, recomputedHash, hashKind: "frontmatter" };
    } catch (error) {
      core.info(`Could not compute frontmatter hash from local files: ${getErrorMessage(error)}`);
      return null;
    }
  }

  // Primary: compare frontmatter hashes using the GitHub API.
  // Falls back to local filesystem if the API is inaccessible.
  // Returns { result, crossRepoAuthFailure } where:
  //   result             — { match, storedHash, recomputedHash, hashKind } | null
  //   crossRepoAuthFailure — { status, repo } when a cross-repo 401/403/404 was the root cause, else null
  async function compareFrontmatterHashes() {
    try {
      // Fetch lock file content to extract stored hash.
      // errorStatus is populated when the API call fails so we can detect cross-repo auth
      // failures: the caller's GITHUB_TOKEN is repo-scoped and cannot read from a private
      // callee repo, returning 404/401/403.
      const { content: lockFileContent, errorStatus } = await getFileContent(github, owner, repo, lockFilePath, ref);

      // When the callee is a private repo, the caller's GITHUB_TOKEN (which is scoped to the
      // caller repo) will receive a 404/401/403 from the callee's Contents API.  Record this so
      // that the final error message can give actionable remediation guidance instead of
      // directing the user to re-run `gh aw compile`.
      /** @type {any} */
      let crossRepoAuthFailure = null;
      if (!lockFileContent && workflowRepo !== currentRepo && (errorStatus === 401 || errorStatus === 403 || errorStatus === 404)) {
        crossRepoAuthFailure = { status: errorStatus, repo: workflowRepo };
        core.info(`Cross-repo API access failed (HTTP ${errorStatus}): GITHUB_TOKEN is scoped to the caller repo and cannot read from '${workflowRepo}'. Configure GH_AW_GITHUB_TOKEN with read access to '${workflowRepo}' to resolve this.`);
      }

      if (!lockFileContent) {
        core.info("Unable to fetch lock file content for hash comparison via API, trying local filesystem fallback");
        return { result: await compareFrontmatterHashesFromLocalFiles(), crossRepoAuthFailure };
      }

      const storedHash = extractHashFromLockFile(lockFileContent);
      if (!storedHash) {
        core.info("No frontmatter hash found in lock file");
        return { result: null, crossRepoAuthFailure: null };
      }

      // Compute hash using pure JavaScript implementation
      // Create a GitHub file reader for fetching workflow files via API
      const fileReader = createGitHubFileReader(github, owner, repo, ref);
      const recomputedHash = await computeFrontmatterHash(workflowMdPath, { fileReader, readerMode: "github-api" });

      const match = storedHash === recomputedHash;

      // Log hash comparison
      core.info(`Frontmatter hash comparison:`);
      core.info(`  Lock file hash:    ${storedHash}`);
      core.info(`  Recomputed hash:   ${recomputedHash}`);
      core.info(`  Status: ${match ? "✅ Hashes match" : "⚠️  Hashes differ"}`);

      // When full check mode is enabled, also compare body hashes.
      if (match && fullCheckMode) {
        const bodyComparison = await compareBodyHashes(lockFileContent, workflowMdPath, { fileReader });
        if (bodyComparison) return { result: bodyComparison, crossRepoAuthFailure: null };
      }

      return { result: { match, storedHash, recomputedHash, hashKind: "frontmatter" }, crossRepoAuthFailure: null };
    } catch (error) {
      const errorMessage = getErrorMessage(error);
      core.info(`Could not compute frontmatter hash via API: ${errorMessage}`);
      // Fall back to local filesystem when API is unavailable
      // (e.g., cross-org reusable workflow where caller token lacks source repo access)
      return { result: await compareFrontmatterHashesFromLocalFiles(), crossRepoAuthFailure: null };
    }
  }

  /**
   * Re-runs the hash computation with full step-by-step debug logging.
   * Called only when the first comparison reveals a mismatch or failure so that
   * the extra verbosity does not appear in healthy runs.
   * Uses the same file reader strategy (API first, local filesystem fallback).
   */
  async function recomputeHashWithDebugLogging() {
    core.info("═══ Debug hash recomputation (verbose mode) ═══");
    core.info("Re-running hash computation with full logging to aid debugging.");
    core.info(`  Source file: ${workflowMdPath}`);
    core.info(`  Source repo: ${owner}/${repo} @ ${ref || "(default branch)"}`);
    try {
      // Try API first (same strategy as compareFrontmatterHashes)
      let fileReader;
      try {
        const { content: testContent } = await getFileContent(github, owner, repo, workflowMdPath, ref);
        if (testContent) {
          fileReader = createGitHubFileReader(github, owner, repo, ref);
          core.info("  Using GitHub API file reader for debug pass");
        }
      } catch (_apiErr) {
        // ignore; fall through to local filesystem
      }

      if (!fileReader) {
        // Local filesystem fallback
        const workspace = process.env.GITHUB_WORKSPACE;
        if (workspace) {
          const localMdFilePath = path.resolve(workspace, workflowMdPath);
          const allowedDir = path.resolve(workspace, ".github", "workflows");
          if (localMdFilePath.startsWith(allowedDir + path.sep)) {
            core.info("  Using local filesystem file reader for debug pass");
            // defaultFileReader is the built-in fs-based reader; wrap it as async
            fileReader = async filePath => fs.readFileSync(filePath, "utf8");
            // Override workflowMdPath to the resolved local path for this pass
            await computeFrontmatterHash(localMdFilePath, { fileReader, verbose: true });
            core.info("═══ End of debug hash recomputation ═══");
            return;
          }
        }
        core.info("  Cannot determine file reader for debug pass (API and local filesystem both unavailable)");
        core.info("═══ End of debug hash recomputation ═══");
        return;
      }

      await computeFrontmatterHash(workflowMdPath, { fileReader, readerMode: "github-api", verbose: true });
    } catch (debugErr) {
      core.info(`  Debug recomputation encountered an error: ${getErrorMessage(debugErr)}`);
    }
    core.info("═══ End of debug hash recomputation ═══");
  }

  const { result: hashComparison, crossRepoAuthFailure } = await compareFrontmatterHashes();

  if (!hashComparison) {
    // Could not compute hash - run verbose pass for debugging then fail
    core.warning("Could not compare frontmatter hashes - assuming lock file is outdated");
    await recomputeHashWithDebugLogging();

    let warningMessage;
    let actionRequiredText;

    if (crossRepoAuthFailure) {
      // The root cause is an auth gap, not a stale lock file. Direct the user to fix the token,
      // not to re-run `gh aw compile` (which would not resolve the issue).
      warningMessage = `Lock file '${lockFilePath}' could not be verified: GITHUB_TOKEN cannot access the callee repo '${crossRepoAuthFailure.repo}' (HTTP ${crossRepoAuthFailure.status}). Configure GH_AW_GITHUB_TOKEN with read access to '${crossRepoAuthFailure.repo}'.`;
      actionRequiredText = `**Root cause:** \`GITHUB_TOKEN\` is scoped to the caller repo and cannot read from the private callee repo \`${crossRepoAuthFailure.repo}\`.\n\n**Action Required:** Configure \`GH_AW_GITHUB_TOKEN\` with \`contents: read\` access to \`${crossRepoAuthFailure.repo}\`.\n\n`;
    } else {
      warningMessage = `Lock file '${lockFilePath}' is outdated or unverifiable! Could not verify frontmatter hash for '${workflowMdPath}'. Run 'gh aw compile' to regenerate the lock file.`;
      actionRequiredText = "**Action Required:** Run `gh aw compile` to regenerate the lock file.\n\n";
    }

    let summary = core.summary
      .addRaw("### ⚠️ Workflow Lock File Warning\n\n")
      .addRaw("**WARNING**: Could not verify whether lock file is up to date. Frontmatter hash check failed.\n\n")
      .addRaw("**Files:**\n")
      .addRaw(`- Source: \`${workflowMdPath}\`\n`)
      .addRaw(`- Lock: \`${lockFilePath}\`\n\n`)
      .addRaw(actionRequiredText);

    await summary.write();

    core.setOutput("stale_lock_file_failed", "true");
    core.setFailed(`${ERR_CONFIG}: ${SAFE_OUTPUT_E009} ${CONFIG_HASH_MISMATCH}: ${warningMessage}`);
  } else if (hashComparison.match) {
    // Hashes match - lock file is up to date
    core.info("✅ Lock file is up to date (hashes match)");
  } else {
    // Hashes differ - run verbose pass for debugging then fail
    await recomputeHashWithDebugLogging();

    const source = `${owner}/${repo}/${workflowMdPath}@${ref || "(default branch)"}`;
    const hashKind = hashComparison.hashKind;
    const warningMessage = `Lock file '${lockFilePath}' is outdated! Source '${source}' has a ${hashKind} hash mismatch${hashKind === "frontmatter" ? " (workflow frontmatter or imported configuration/expressions)" : " (workflow body or imported bodies)"}. Run 'gh aw compile' to regenerate the lock file.`;

    let summary = core.summary
      .addRaw("### ⚠️ Workflow Lock File Warning\n\n")
      .addRaw(`**WARNING**: Lock file is outdated (${hashKind} hash mismatch).\n\n`)
      .addRaw("**Files:**\n")
      .addRaw(`- Source: \`${source}\`\n`)
      .addRaw(`  - Recomputed ${hashKind} hash: \`${hashComparison.recomputedHash.substring(0, 12)}...\`\n`)
      .addRaw(`- Lock: \`${lockFilePath}\`\n`)
      .addRaw(`  - Stored hash: \`${hashComparison.storedHash.substring(0, 12)}...\`\n\n`)
      .addRaw("**Action Required:** Run `gh aw compile` to regenerate the lock file.\n\n");

    await summary.write();

    // Signal the activation job so the conclusion job can surface this as a
    // specialised failure issue (separate from agent runtime failures).
    core.setOutput("stale_lock_file_failed", "true");

    // Fail the step to prevent workflow from running with outdated configuration
    core.setFailed(`${ERR_CONFIG}: ${SAFE_OUTPUT_E009} ${CONFIG_HASH_MISMATCH}: ${warningMessage}`);
  }
}

module.exports = { main };
