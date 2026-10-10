// @ts-check
/// <reference types="@actions/github-script" />

/**
 * push_experiment_state
 *
 * Commits state files to a git
 * branch using the GitHub GraphQL `createCommitOnBranch` mutation so commits
 * are cryptographically signed (verified) by GitHub. Falls back to a plain
 * `git push` via pushSignedCommits when the GraphQL path is unavailable.
 *
 * Generic environment variables:
 *   GH_AW_STATE_DIR             - Directory containing files to commit
 *   GH_AW_STATE_BRANCH          - Target git branch
 *   GH_AW_STATE_FILES           - Comma-separated filenames to copy from GH_AW_STATE_DIR
 *   GH_AW_STATE_LABEL           - Human-readable label used in logs/messages
 *
 * Backward-compatible experiment aliases:
 *   GH_AW_EXPERIMENT_STATE_DIR  - Directory containing state.jsonl/state.json and assignments.json
 *   GH_AW_EXPERIMENT_BRANCH     - Target git branch for experiment state
 *   GH_TOKEN / GITHUB_TOKEN     - GitHub token for API access and git operations
 *   GITHUB_RUN_ID               - Run ID used in commit messages
 *   GITHUB_SERVER_URL           - GitHub server URL (defaults to https://github.com)
 *   GITHUB_REPOSITORY           - "owner/repo" of the current repository
 */

const fs = require("fs");
const path = require("path");

const { getErrorMessage } = require("./error_helpers.cjs");
const { getGitAuthEnv } = require("./git_auth_helpers.cjs");
const { execGitSync, withGitRetry } = require("./git_helpers.cjs");
const { pushSignedCommits } = require("./push_signed_commits.cjs");

function isPlainObject(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function stableJSONStringify(value) {
  if (Array.isArray(value)) {
    return `[${value.map(stableJSONStringify).join(",")}]`;
  }
  if (isPlainObject(value)) {
    return `{${Object.keys(value)
      .sort()
      .map(key => `${JSON.stringify(key)}:${stableJSONStringify(value[key])}`)
      .join(",")}}`;
  }
  return JSON.stringify(value);
}

/** Maximum number of run-ledger records to retain per state.jsonl file. Keeps the file well under the load limit. */
const MAX_LEDGER_RECORDS = 512;

function sortRunsByTimestamp(runs) {
  return runs.slice().sort((a, b) => {
    const ta = isPlainObject(a) && typeof a.timestamp === "string" ? a.timestamp : "";
    const tb = isPlainObject(b) && typeof b.timestamp === "string" ? b.timestamp : "";
    if (ta < tb) return -1;
    if (ta > tb) return 1;
    const ra = isPlainObject(a) && typeof a.run_id === "string" ? a.run_id : "";
    const rb = isPlainObject(b) && typeof b.run_id === "string" ? b.run_id : "";
    return ra < rb ? -1 : ra > rb ? 1 : 0;
  });
}

function mergeExperimentRuns(remoteRuns, localRuns) {
  const merged = [];
  const seen = new Set();
  for (const run of [...remoteRuns, ...localRuns]) {
    const key =
      isPlainObject(run) && typeof run.run_id === "string" && typeof run.timestamp === "string" && isPlainObject(run.assignments)
        ? `${run.run_id}\u0000${run.timestamp}\u0000${stableJSONStringify(run.assignments)}`
        : stableJSONStringify(run);
    if (!seen.has(key)) {
      seen.add(key);
      merged.push(run);
    }
  }
  return sortRunsByTimestamp(merged);
}

function mergeExperimentStateValue(baseValue, remoteValue, localValue) {
  if (Number.isFinite(baseValue) && Number.isFinite(remoteValue) && Number.isFinite(localValue)) {
    return remoteValue + localValue - baseValue;
  }
  if (stableJSONStringify(baseValue) === stableJSONStringify(remoteValue)) {
    return localValue;
  }
  if (stableJSONStringify(baseValue) === stableJSONStringify(localValue)) {
    return remoteValue;
  }
  if (Array.isArray(remoteValue) && Array.isArray(localValue)) {
    return mergeExperimentRuns(remoteValue, localValue);
  }
  if (isPlainObject(remoteValue) && isPlainObject(localValue)) {
    const result = {};
    for (const key of new Set([...Object.keys(baseValue || {}), ...Object.keys(remoteValue), ...Object.keys(localValue)])) {
      result[key] = mergeExperimentStateValue(baseValue?.[key], remoteValue[key], localValue[key]);
    }
    return result;
  }
  if (stableJSONStringify(remoteValue) === stableJSONStringify(localValue)) {
    return localValue;
  }
  return localValue;
}

function mergeExperimentStateJSON(baseState, remoteState, localState) {
  if (!isPlainObject(baseState) || !isPlainObject(remoteState) || !isPlainObject(localState)) {
    throw new Error("Experiment state merge requires JSON objects");
  }
  const merged = mergeExperimentStateValue(baseState, remoteState, localState);
  if (!isPlainObject(merged) || !isPlainObject(merged.counts)) {
    throw new Error("Merged experiment state is invalid");
  }
  if (merged.runs !== undefined && !Array.isArray(merged.runs)) {
    throw new Error("Merged experiment state runs must be an array when present");
  }
  /** @type {Record<string, {current_stage: number}>} */
  const continual = {};
  for (const state of [baseState, remoteState, localState]) {
    if (state.continual === undefined) continue;
    if (!isPlainObject(state.continual)) {
      throw new Error("Continual experiment state must be a plain object");
    }
    for (const [name, value] of Object.entries(state.continual)) {
      if (!isPlainObject(value) || !Number.isInteger(value.current_stage) || value.current_stage < 0) {
        throw new Error("Merged continual experiment stage must be a non-negative integer");
      }
      continual[name] = {
        current_stage: Math.max(continual[name]?.current_stage ?? 0, value.current_stage),
      };
    }
  }
  if (Object.keys(continual).length > 0) {
    merged.continual = continual;
  }
  return merged;
}

function mergeExperimentStateJSONL(remoteContent, localContent) {
  const merged = [];
  const seen = new Set();
  for (const content of [remoteContent, localContent]) {
    for (const line of content.split(/\r?\n/)) {
      const trimmed = line.trim();
      if (!trimmed) {
        continue;
      }
      let entry;
      try {
        entry = JSON.parse(trimmed);
      } catch {
        // Skip lines that cannot be parsed as JSON. This makes the merge resilient
        // to partial writes or corruption in either side of the conflict.
        core.warning(`mergeExperimentStateJSONL: skipping unparseable line during conflict merge`);
        continue;
      }
      const key = stableJSONStringify(entry);
      if (!seen.has(key)) {
        seen.add(key);
        merged.push(entry);
      }
    }
  }

  // Sort entries chronologically so the ledger is always in timestamp order.
  merged.sort((a, b) => {
    const ta = isPlainObject(a) && typeof a.timestamp === "string" ? a.timestamp : "";
    const tb = isPlainObject(b) && typeof b.timestamp === "string" ? b.timestamp : "";
    if (ta < tb) return -1;
    if (ta > tb) return 1;
    const ra = isPlainObject(a) && typeof a.run_id === "string" ? a.run_id : "";
    const rb = isPlainObject(b) && typeof b.run_id === "string" ? b.run_id : "";
    return ra < rb ? -1 : ra > rb ? 1 : 0;
  });

  // Compact the ledger to avoid exceeding the loader file-size limit.
  // Counts from pruned records are folded into the first remaining entry's baseline_counts
  // so cumulative totals are preserved across compaction boundaries.
  if (merged.length > MAX_LEDGER_RECORDS) {
    const pruned = merged.splice(0, merged.length - MAX_LEDGER_RECORDS);
    if (merged.length > 0) {
      /** @type {Record<string, Record<string, number>>} */
      const baseline = {};
      for (const entry of pruned) {
        if (!isPlainObject(entry)) continue;
        if (isPlainObject(entry.baseline_counts)) {
          for (const [name, variants] of Object.entries(entry.baseline_counts)) {
            if (!baseline[name]) baseline[name] = {};
            for (const [variant, count] of Object.entries(/** @type {Record<string,unknown>} */ variants)) {
              baseline[name][variant] = (baseline[name][variant] || 0) + (typeof count === "number" ? count : 0);
            }
          }
        }
        if (isPlainObject(entry.assignments)) {
          for (const [name, variant] of Object.entries(entry.assignments)) {
            if (typeof variant !== "string") continue;
            if (!baseline[name]) baseline[name] = {};
            baseline[name][variant] = (baseline[name][variant] || 0) + 1;
          }
        }
      }
      if (Object.keys(baseline).length > 0) {
        const first = merged[0];
        const existing = isPlainObject(first) && isPlainObject(first.baseline_counts) ? first.baseline_counts : {};
        /** @type {Record<string, Record<string, number>>} */
        const mergedBaseline = Object.assign({}, /** @type {Record<string, Record<string, number>>} */ existing);
        for (const [name, variants] of Object.entries(baseline)) {
          if (!mergedBaseline[name]) mergedBaseline[name] = {};
          for (const [variant, count] of Object.entries(variants)) {
            mergedBaseline[name][variant] = (mergedBaseline[name][variant] || 0) + count;
          }
        }
        merged[0] = Object.assign({}, first, { baseline_counts: mergedBaseline });
      }
    }
  }

  return merged.length > 0 ? `${merged.map(entry => JSON.stringify(entry)).join("\n")}\n` : "";
}

function mergeAppendOnlyJSONL(remoteContent, localContent) {
  const merged = [];
  const seen = new Set();
  for (const content of [remoteContent, localContent]) {
    for (const line of content.split(/\r?\n/)) {
      const trimmed = line.trim();
      if (!trimmed) {
        continue;
      }
      let outputLine;
      let key;
      try {
        const entry = JSON.parse(trimmed);
        outputLine = JSON.stringify(entry);
        key = `json:${stableJSONStringify(entry)}`;
      } catch {
        core.warning(`mergeAppendOnlyJSONL: preserving unparseable line during merge`);
        outputLine = trimmed;
        key = `raw:${trimmed}`;
      }
      if (!seen.has(key)) {
        seen.add(key);
        merged.push(outputLine);
      }
    }
  }
  return merged.length > 0 ? `${merged.join("\n")}\n` : "";
}

function readGitStageFile(workspaceDir, stage, filePath) {
  return execGitSync(["show", `:${stage}:${filePath}`], {
    cwd: workspaceDir,
    stdio: "pipe",
    suppressLogs: true,
  });
}

function resolveExperimentStateRebaseConflict({ cwd }) {
  const conflictedFiles = execGitSync(["diff", "--name-only", "--diff-filter=U"], {
    cwd,
    stdio: "pipe",
    suppressLogs: true,
  })
    .trim()
    .split("\n")
    .map(file => file.trim())
    .filter(Boolean);

  const appendFiles = new Set(
    (process.env.GH_AW_STATE_FILES || "")
      .split(",")
      .map(name => name.trim())
      .filter(name => Boolean(name) && name.endsWith(".jsonl") && name !== "state.jsonl")
  );
  const hasMergeableConflict = conflictedFiles.some(file => file === "state.json" || file === "state.jsonl" || appendFiles.has(file));
  if (conflictedFiles.length === 0 || !hasMergeableConflict) {
    return false;
  }

  const allowedConflicts = new Set(["state.json", "state.jsonl", "assignments.json", ...appendFiles]);
  for (const file of conflictedFiles) {
    if (!allowedConflicts.has(file)) {
      return false;
    }
  }

  if (conflictedFiles.includes("state.json")) {
    try {
      const baseState = JSON.parse(readGitStageFile(cwd, 1, "state.json"));
      const remoteState = JSON.parse(readGitStageFile(cwd, 2, "state.json"));
      const localState = JSON.parse(readGitStageFile(cwd, 3, "state.json"));
      const mergedState = mergeExperimentStateJSON(baseState, remoteState, localState);
      fs.writeFileSync(path.join(cwd, "state.json"), JSON.stringify(mergedState, null, 2) + "\n", "utf8");
    } catch (err) {
      throw new Error(`Failed to resolve state.json rebase conflict: ${getErrorMessage(err)}`, { cause: err });
    }
  }

  if (conflictedFiles.includes("state.jsonl")) {
    try {
      const remoteState = readGitStageFile(cwd, 2, "state.jsonl");
      const localState = readGitStageFile(cwd, 3, "state.jsonl");
      const mergedState = mergeExperimentStateJSONL(remoteState, localState);
      fs.writeFileSync(path.join(cwd, "state.jsonl"), mergedState, "utf8");
    } catch (err) {
      throw new Error(`Failed to resolve state.jsonl rebase conflict: ${getErrorMessage(err)}`, { cause: err });
    }
  }

  for (const file of conflictedFiles.filter(name => appendFiles.has(name))) {
    try {
      const remoteState = readGitStageFile(cwd, 2, file);
      const localState = readGitStageFile(cwd, 3, file);
      fs.writeFileSync(path.join(cwd, file), mergeAppendOnlyJSONL(remoteState, localState), "utf8");
    } catch (err) {
      throw new Error(`Failed to resolve ${file} rebase conflict: ${getErrorMessage(err)}`, { cause: err });
    }
  }

  if (conflictedFiles.includes("assignments.json")) {
    try {
      const localAssignments = readGitStageFile(cwd, 3, "assignments.json");
      fs.writeFileSync(path.join(cwd, "assignments.json"), localAssignments, "utf8");
    } catch (err) {
      throw new Error(`Failed to resolve assignments.json rebase conflict: ${getErrorMessage(err)}`, { cause: err });
    }
  }

  execGitSync(["add", "--", ...conflictedFiles], { stdio: "inherit", cwd });
  return true;
}

/**
 * Checkout or create an orphan git branch for experiment state.
 * Returns the remote HEAD SHA (empty string for a new branch).
 *
 * Only the network `git fetch` is retried (via withGitRetry), and only for
 * transient transport failures. Everything after it — the local checkout, the
 * missing-ref classification, and the non-idempotent orphan-branch path
 * (`checkout --orphan`, `read-tree --empty`, working-tree cleanup) — runs
 * exactly once, so a partial failure can never be replayed against a
 * half-mutated workspace.
 *
 * @param {string} branchName - Target branch name (e.g. "experiments/myworkflow")
 * @param {string} repoUrl    - Authenticated HTTPS URL of the target repo
 * @param {string} workspaceDir - Local git workspace directory
 * @param {{maxRetries?: number, baseDelayMs?: number, fetchFn?: () => void}} [options]
 * @returns {Promise<string>} baseRef (empty string when branch is brand new)
 */
async function checkoutOrCreateBranch(branchName, repoUrl, workspaceDir, options = {}) {
  const { maxRetries, baseDelayMs, fetchFn = () => execGitSync(["fetch", repoUrl, `${branchName}:${branchName}`], { stdio: "pipe", cwd: workspaceDir, suppressLogs: true }) } = options;

  try {
    await withGitRetry(fetchFn, {
      maxRetries,
      baseDelayMs,
      operationName: `Fetch of branch "${branchName}"`,
    });
  } catch (fetchErr) {
    const msg = getErrorMessage(fetchErr);
    const isMissing = /couldn't find remote ref/i.test(msg) || /remote branch .* not found/i.test(msg);
    if (!isMissing) throw fetchErr;

    // Branch does not exist yet – create an orphan branch. This path performs
    // non-idempotent local mutations and is deliberately never retried.
    core.info(`Branch ${branchName} does not exist, creating orphan branch...`);
    execGitSync(["checkout", "--orphan", branchName], { stdio: "inherit", cwd: workspaceDir });
    execGitSync(["read-tree", "--empty"], { stdio: "pipe", cwd: workspaceDir });
    // Remove any pre-existing working-tree files (from sparse checkout).
    let entries;
    try {
      entries = fs.readdirSync(workspaceDir);
    } catch (err) {
      throw new Error(`Failed to read workspace directory ${workspaceDir}: ${getErrorMessage(err)}`, { cause: err });
    }
    for (const entry of entries) {
      if (entry !== ".git") {
        try {
          fs.rmSync(path.join(workspaceDir, entry), { recursive: true, force: true });
        } catch (err) {
          throw new Error(`Failed to remove workspace entry ${entry}: ${getErrorMessage(err)}`, { cause: err });
        }
      }
    }
    return "";
  }

  execGitSync(["checkout", branchName], { stdio: "inherit", cwd: workspaceDir });
  const baseRef = execGitSync(["rev-parse", "HEAD"], { cwd: workspaceDir }).trim();
  core.info(`Checked out existing branch ${branchName}, baseRef=${baseRef}`);
  return baseRef;
}

/**
 * Main entry point called by the actions/github-script step.
 */
async function main() {
  const stateDir = process.env.GH_AW_STATE_DIR || process.env.GH_AW_EXPERIMENT_STATE_DIR || "/tmp/gh-aw/experiments";
  const branchName = process.env.GH_AW_STATE_BRANCH || process.env.GH_AW_EXPERIMENT_BRANCH || "";
  const stateLabel = process.env.GH_AW_STATE_LABEL || "experiment state";
  const filesEnv = process.env.GH_AW_STATE_FILES || "state.jsonl,state.json,assignments.json";
  const candidateFiles = filesEnv
    .split(",")
    .map(name => name.trim())
    .filter(Boolean);
  const appendFiles = new Set(candidateFiles.filter(name => name.endsWith(".jsonl") && name !== "state.jsonl"));
  const ghToken = process.env.GH_TOKEN || process.env.GITHUB_TOKEN || "";
  const githubRunId = process.env.GITHUB_RUN_ID || "unknown";
  const githubServerUrl = (process.env.GITHUB_SERVER_URL || "https://github.com").replace(/\/$/, "");
  const serverHost = githubServerUrl.replace(/^https?:\/\//, "");

  if (!branchName) {
    core.setFailed("GH_AW_STATE_BRANCH (or GH_AW_EXPERIMENT_BRANCH) is not set");
    return;
  }
  if (!ghToken) {
    core.setFailed("GH_TOKEN or GITHUB_TOKEN is not set");
    return;
  }

  const targetRepo = `${context.repo.owner}/${context.repo.repo}`;
  const allowedRepos = new Set(
    (process.env.GH_AW_ALLOWED_TARGET_REPOS || targetRepo)
      .split(",")
      .map(repo => repo.trim())
      .filter(Boolean)
  );
  if (!allowedRepos.has(targetRepo)) {
    core.setFailed(`Target repository "${targetRepo}" is not in GH_AW_ALLOWED_TARGET_REPOS. ` + `Current allowlist: ${Array.from(allowedRepos).join(", ")}`);
    return;
  }
  const [owner, repo] = targetRepo.split("/");

  core.info(`Pushing ${stateLabel} to branch "${branchName}" in ${targetRepo}`);

  // Collect the JSON files that exist in the state directory.
  /** @type {string[]} */
  const filesToPush = [];
  /** @type {string[]} */
  const inspectErrors = [];
  for (const name of candidateFiles) {
    const full = path.join(stateDir, name);
    if (!fs.existsSync(full)) {
      continue;
    }
    let fileInfo;
    try {
      fileInfo = fs.statSync(full);
    } catch (err) {
      inspectErrors.push(`${full}: ${getErrorMessage(err)}`);
      continue;
    }
    if (fileInfo.isFile()) {
      filesToPush.push(name);
    }
  }

  if (inspectErrors.length > 0) {
    core.setFailed(`Failed to inspect ${stateLabel} files:\n${inspectErrors.join("\n")}`);
    return;
  }

  if (filesToPush.length === 0) {
    core.info(`No ${stateLabel} files found – nothing to push`);
    return;
  }

  core.info(`Files to push: ${filesToPush.join(", ")}`);

  const workspaceDir = process.env.GITHUB_WORKSPACE || process.cwd();
  const repoUrl = `https://x-access-token:${ghToken}@${serverHost}/${targetRepo}.git`;

  // Checkout the target branch (or create it as an orphan on first run).
  // Retries with exponential backoff since the initial `git fetch` can hit
  // transient network failures (e.g. HTTP 502s or timeouts against the git
  // remote) that are unrelated to genuine push conflicts.
  let baseRef;
  try {
    baseRef = await checkoutOrCreateBranch(branchName, repoUrl, workspaceDir);
  } catch (err) {
    core.setFailed(`Failed to checkout branch "${branchName}" after retries: ${getErrorMessage(err)}`);
    return;
  }

  // Copy state files into the workspace root.
  for (const name of filesToPush) {
    const src = path.join(stateDir, name);
    const dest = path.join(workspaceDir, name);
    try {
      if (appendFiles.has(name) && fs.existsSync(dest)) {
        const existingContent = fs.readFileSync(dest, "utf8");
        const newContent = fs.readFileSync(src, "utf8");
        fs.writeFileSync(dest, mergeAppendOnlyJSONL(existingContent, newContent), "utf8");
      } else {
        fs.copyFileSync(src, dest);
      }
      core.info(`Copied ${name}`);
    } catch (err) {
      core.setFailed(`Failed to copy ${name}: ${getErrorMessage(err)}`);
      return;
    }
  }

  // Stage all changes.
  try {
    execGitSync(["add", "--sparse", "."], { stdio: "inherit", cwd: workspaceDir });
  } catch (err) {
    core.setFailed(`Failed to stage changes: ${getErrorMessage(err)}`);
    return;
  }

  // Check whether there are any staged changes to commit.
  const status = execGitSync(["status", "--porcelain"], { cwd: workspaceDir }).trim();
  if (!status) {
    core.info(`No changes to ${stateLabel} – skipping push`);
    return;
  }

  // Commit.
  try {
    execGitSync(["commit", "-m", `Update ${stateLabel} from workflow run ${githubRunId}`], { stdio: "inherit", cwd: workspaceDir });
  } catch (err) {
    core.setFailed(`Failed to commit ${stateLabel}: ${getErrorMessage(err)}`);
    return;
  }

  // Point origin at the target repo so pushSignedCommits can resolve the remote branch HEAD.
  execGitSync(["remote", "set-url", "origin", `https://${serverHost}/${targetRepo}.git`], { stdio: "pipe", cwd: workspaceDir });

  // Push using GraphQL createCommitOnBranch (signed commits) with a plain-git fallback.
  const MAX_RETRIES = 3;
  const BASE_DELAY_MS = 1000;
  let currentBaseRef = baseRef;

  for (let attempt = 0; attempt <= MAX_RETRIES; attempt++) {
    core.info(`Pushing to ${branchName} (attempt ${attempt + 1}/${MAX_RETRIES + 1})...`);
    try {
      await pushSignedCommits({
        githubClient: github,
        owner,
        repo,
        branch: branchName,
        baseRef: currentBaseRef,
        cwd: workspaceDir,
        gitAuthEnv: getGitAuthEnv(ghToken),
        resolveRebaseConflict: resolveExperimentStateRebaseConflict,
      });
      core.info(`Successfully pushed ${stateLabel} to ${branchName}`);
      return;
    } catch (err) {
      const errMsg = getErrorMessage(err);
      if (attempt < MAX_RETRIES) {
        const delay = BASE_DELAY_MS * Math.pow(2, attempt);
        core.warning(`Push failed (attempt ${attempt + 1}/${MAX_RETRIES + 1}), retrying in ${delay}ms: ${errMsg}`);
        await new Promise(resolve => setTimeout(resolve, delay));

        // Refresh baseRef and fetch the updated remote history so that
        // pushSignedCommits can resolve the new baseRef in git rev-list.
        try {
          const gitEnv = { ...process.env, ...getGitAuthEnv(ghToken) };
          const { stdout: lsOut } = await exec.getExecOutput("git", ["ls-remote", "origin", `refs/heads/${branchName}`], { cwd: workspaceDir, env: gitEnv });
          const remoteHead = lsOut.trim().split(/\s+/)[0] || "";
          if (remoteHead && remoteHead !== currentBaseRef) {
            // Fetch the updated branch history into the local repo so pushSignedCommits
            // can resolve currentBaseRef in `git rev-list baseRef..HEAD`.
            try {
              execGitSync(["fetch", "origin", `refs/heads/${branchName}`], { stdio: "pipe", cwd: workspaceDir, env: gitEnv, suppressLogs: true });
              currentBaseRef = remoteHead;
              core.info(`Refreshed baseRef for retry: ${currentBaseRef}`);
            } catch (fetchErr) {
              core.info(`Fetch of branch "${branchName}" on retry failed (non-fatal): ${getErrorMessage(fetchErr)}`);
            }
          }
        } catch (remoteErr) {
          core.warning(`Could not refresh baseRef for branch "${branchName}" on retry; keeping existing baseRef: ${getErrorMessage(remoteErr)}`);
        }
      } else {
        core.setFailed(`Failed to push ${stateLabel} after ${MAX_RETRIES + 1} attempts: ${errMsg}`);
        return;
      }
    }
  }
}

module.exports = {
  main,
  checkoutOrCreateBranch,
  mergeExperimentStateJSON,
  mergeExperimentStateJSONL,
  mergeAppendOnlyJSONL,
  mergeExperimentRuns,
  resolveExperimentStateRebaseConflict,
};
