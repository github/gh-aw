// @ts-check
/// <reference types="@actions/github-script" />

const childProcess = require("child_process");
const fs = require("fs");
const path = require("path");
const { ERR_SYSTEM } = require("./error_codes.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { isTransientError } = require("./error_recovery.cjs");
const { buildGitAuthEnv } = require("./git_auth_env.cjs");

/**
 * Build GIT_CONFIG_* environment variables that inject an Authorization header
 * for git network operations (fetch, push, clone) without writing credentials
 * to .git/config on disk.
 *
 * Use this whenever .git/config credentials may have been cleaned (e.g. after
 * clean_git_credentials.sh runs in the agent job) to ensure git can still
 * authenticate against the GitHub server.
 *
 * SECURITY: Credentials are passed via GIT_CONFIG_* environment variables and
 * never written to .git/config, so they are not visible to file-monitoring
 * attacks and are not inherited by sub-processes that don't receive the env.
 *
 * @param {string} [token] - GitHub token to use. Falls back to GITHUB_TOKEN env var.
 * @returns {Object} Environment variables to spread into child_process/exec options.
 *   Returns an empty object when no token is available.
 */
function getGitAuthEnv(token) {
  const authToken = token || process.env.GITHUB_TOKEN;
  if (!authToken) {
    core.debug("getGitAuthEnv: no token available, git network operations may fail if credentials were cleaned");
    return {};
  }
  return buildGitAuthEnv(authToken);
}

/**
 * Ensure the given directory is trusted for git in the current process context.
 * Injects safe.directory via GIT_CONFIG_COUNT/KEY/VALUE env vars so that all
 * subsequent git commands in this process inherit the trust without writing to
 * ~/.gitconfig (no persistent global git config side effects).
 *
 * This is specifically needed for the "bridge" path (the Process Safe Outputs step
 * running outside the Docker container) where HOME or the uid may differ from the
 * in-container user that originally configured safe.directory in ~/.gitconfig.
 *
 * GIT_CONFIG_COUNT/KEY/VALUE is supported since git 2.31.
 *
 * @param {string} gitCwd - The directory to trust (e.g. GITHUB_WORKSPACE or a repo checkout path)
 * @param {Object|Function} [logger] - Optional logger object
 *   with a debug method (for example the MCP server) or a direct debug function.
 * @returns {void}
 */
function ensureSafeDirectoryTrust(gitCwd, logger) {
  if (!gitCwd) return;

  // Check if gitCwd is already present in the injected env-var config to avoid
  // duplicate entries when the handler is called more than once in the same process.
  // Malformed pre-existing GIT_CONFIG_COUNT values (NaN, negative, fractional,
  // or unsafe integers) fall back to 0 to avoid corrupting the env-var config chain.
  const rawCount = process.env.GIT_CONFIG_COUNT || "0";
  const parsedCount = Number(rawCount);
  const existingCount = Number.isSafeInteger(parsedCount) && parsedCount >= 0 ? parsedCount : 0;
  for (let i = 0; i < existingCount; i++) {
    if (process.env[`GIT_CONFIG_KEY_${i}`] === "safe.directory" && process.env[`GIT_CONFIG_VALUE_${i}`] === gitCwd) {
      return;
    }
  }

  const idx = existingCount;
  process.env.GIT_CONFIG_COUNT = String(existingCount + 1);
  process.env[`GIT_CONFIG_KEY_${idx}`] = "safe.directory";
  process.env[`GIT_CONFIG_VALUE_${idx}`] = gitCwd;
  let debug;
  if (typeof logger === "function") {
    debug = logger;
  } else if (typeof logger?.debug === "function") {
    debug = logger.debug.bind(logger);
  } else if (typeof globalThis.core?.debug === "function") {
    debug = globalThis.core.debug.bind(globalThis.core);
  }
  debug?.(`Configured git safe.directory for bridge context: ${gitCwd}`);
}

/**
 * Execute Git without logging command output, preserving ordinary exit statuses.
 * Spawn failures discard partial output; callers must check status before use.
 *
 * @param {string[]} args
 * @param {Object} [options]
 * @returns {import("child_process").SpawnSyncReturns<string>}
 */
function execGitSyncResult(args, options = {}) {
  const result = childProcess.spawnSync("git", args, {
    encoding: "utf8",
    maxBuffer: 100 * 1024 * 1024,
    timeout: 60_000,
    killSignal: "SIGKILL",
    ...options,
    stdio: options.stdio === "inherit" ? "pipe" : options.stdio,
    env: {
      ...(options.env || process.env),
      GIT_TERMINAL_PROMPT: "0",
      GCM_INTERACTIVE: "Never",
      GIT_ASKPASS: "/bin/echo",
    },
  });
  if (result.error || result.signal || result.status === null) {
    return { ...result, status: null, stdout: "", stderr: "" };
  }
  return result;
}

/**
 * Safely execute git command using spawnSync with args array to prevent shell injection.
 *
 * Hardened against indefinite hangs: always runs git with non-interactive
 * credential settings (GIT_TERMINAL_PROMPT=0, GCM_INTERACTIVE=Never,
 * GIT_ASKPASS=/bin/echo) and a default 60s timeout (override via options.timeout).
 *
 * @param {string[]} args - Git command arguments
 * @param {Object} options - Spawn options; suppressLogs avoids expected-error annotations,
 *   while silent suppresses raw subprocess diagnostics entirely.
 * @returns {string} Command output
 * @throws {Error} If command fails
 */
function execGitSync(args, options = {}) {
  // Extract suppressLogs before spreading into spawnSync options.
  // suppressLogs is a custom control flag (not a valid spawnSync option) that
  // routes failure details to core.debug instead of core.error, preventing
  // spurious GitHub Actions error annotations for expected failures (e.g., when
  // a branch does not yet exist).
  const { suppressLogs = false, silent = false, ...spawnOptions } = options;
  // Offline generation must not expose refs, paths, object bytes or raw errors.
  const log = silent ? { debug() {}, error() {} } : core;

  // Log the git command being executed for debugging (but redact credentials)
  const gitCommand = `git ${args
    .map(arg => {
      // Redact credentials in URLs
      if (typeof arg === "string" && arg.includes("://") && arg.includes("@")) {
        return arg.replace(/(https?:\/\/)[^@]+@/, "$1***@");
      }
      return arg;
    })
    .join(" ")}`;

  log.debug(`Executing git command: ${gitCommand}`);

  // Hard guards against indefinite hangs:
  //  - GIT_TERMINAL_PROMPT=0 / GCM_INTERACTIVE=Never / GIT_ASKPASS make any
  //    credential rejection fail fast instead of opening an interactive prompt.
  //  - timeout (default 60s) ensures a stuck network/TLS handshake cannot
  //    wedge the calling event loop. Callers can override via options.timeout.
  const defaultTimeoutMs = 60_000;

  const result = execGitSyncResult(args, spawnOptions);

  if (result.error) {
    // Detect ENOBUFS (buffer overflow) and provide a more actionable message
    /** @type {NodeJS.ErrnoException} */
    const spawnError = result.error;
    if (spawnError.code === "ENOBUFS") {
      /** @type {NodeJS.ErrnoException} */
      const bufferError = new Error(`${ERR_SYSTEM}: Git command output exceeded buffer limit (ENOBUFS). The output from '${args[0]}' is too large for the configured maxBuffer. Consider reducing the diff size or increasing maxBuffer.`);
      bufferError.code = "ENOBUFS";
      log.error(`Git command buffer overflow: ${gitCommand}`);
      throw bufferError;
    }
    if (spawnError.code === "ETIMEDOUT") {
      /** @type {NodeJS.ErrnoException} */
      const timeoutError = new Error(`${ERR_SYSTEM}: Git command timed out after ${spawnOptions.timeout || defaultTimeoutMs}ms: ${gitCommand}`);
      timeoutError.code = "ETIMEDOUT";
      log.error(`Git command timed out: ${gitCommand}`);
      throw timeoutError;
    }
    // Spawn-level errors (e.g. ENOENT, EACCES) are always unexpected — log
    // via core.error regardless of suppressLogs.
    log.error(`Git command failed with error: ${result.error.message}`);
    throw result.error;
  }

  // spawnSync sets signal when the process was killed (including by the timeout).
  if (result.signal === "SIGKILL" || result.signal === "SIGTERM") {
    /** @type {NodeJS.ErrnoException} */
    const timeoutError = new Error(`${ERR_SYSTEM}: Git command killed (${result.signal}), likely due to timeout (${spawnOptions.timeout || defaultTimeoutMs}ms): ${gitCommand}`);
    timeoutError.code = "ETIMEDOUT";
    log.error(`Git command killed by signal ${result.signal}: ${gitCommand}`);
    throw timeoutError;
  }

  if (result.status !== 0) {
    const errorMsg = `${ERR_SYSTEM}: ${result.stderr || `Git command failed with status ${result.status}`}`;
    if (suppressLogs) {
      log.debug(`Git command failed (expected): ${gitCommand}`);
      log.debug(`Exit status: ${result.status}`);
      if (result.stderr) {
        log.debug(`Stderr: ${result.stderr}`);
      }
    } else {
      log.error(`Git command failed: ${gitCommand}`);
      log.error(`Exit status: ${result.status}`);
      if (result.stderr) {
        log.error(`Stderr: ${result.stderr}`);
      }
    }
    throw new Error(errorMsg);
  }

  if (result.stdout) {
    log.debug(`Git command output: ${result.stdout.substring(0, 200)}${result.stdout.length > 200 ? "..." : ""}`);
  } else {
    log.debug("Git command completed successfully with no output");
  }

  return result.stdout;
}

/**
 * Git transport failures that are transient but not covered by the generic
 * network patterns in isTransientError (git reports them with its own wording).
 * Matched case-insensitively against the error message.
 * @type {string[]}
 */
const TRANSIENT_GIT_PATTERNS = [
  "the remote end hung up",
  "early eof",
  "rpc failed",
  "http 5",
  "unable to access",
  "could not resolve host",
  "connection reset",
  "timed out",
  "ssl_error",
  "gnutls_handshake",
  "failed to connect",
  "server hung up",
];

/**
 * Determine whether a git failure is a transient transport error worth
 * retrying. Deterministic failures (authentication/permission errors, invalid
 * refs, local git errors, filesystem errors) return false so callers fail fast.
 *
 * @param {any} error - The error thrown by a git operation
 * @returns {boolean} True when the failure looks like a transient transport error
 */
function isTransientGitError(error) {
  if (isTransientError(error)) return true;
  const msg = getErrorMessage(error).toLowerCase();
  return TRANSIENT_GIT_PATTERNS.some(pattern => msg.includes(pattern));
}

/**
 * Run a git operation, retrying only transient transport failures with
 * exponential backoff. Non-transient failures are rethrown immediately.
 *
 * @template T
 * @param {() => T | Promise<T>} operation - The git operation to run
 * @param {{maxRetries?: number, baseDelayMs?: number, operationName?: string}} [options]
 * @returns {Promise<T>} The operation result
 * @throws {any} The last error when retries are exhausted, or the first non-transient error
 */
async function withGitRetry(operation, options = {}) {
  const { maxRetries = 3, baseDelayMs = 1000, operationName = "git operation" } = options;
  let lastError;
  for (let attempt = 0; attempt <= maxRetries; attempt++) {
    try {
      return await operation();
    } catch (err) {
      lastError = err;
      if (!isTransientGitError(err)) throw err;
      if (attempt < maxRetries) {
        const delay = baseDelayMs * Math.pow(2, attempt);
        core.warning(`${operationName} failed (attempt ${attempt + 1}/${maxRetries + 1}), retrying in ${delay}ms: ${getErrorMessage(err)}`);
        await new Promise(resolve => setTimeout(resolve, delay));
      }
    }
  }
  throw lastError;
}

/**
 * Check refs/remotes/origin/<branch> locally without fetching missing refs.
 * The checkout is agent-writable, so its remote configuration must never be
 * used for network operations in the safe-outputs artifact generator.
 *
 * @param {string} branch - Branch name (without origin/ prefix)
 * @param {Object} options
 * @param {string} options.cwd - Working directory for git commands
 * @param {boolean} [options.suppressLogs=false] - Whether to suppress execGitSync error logs
 * @returns {boolean}
 */
function hasLocalOriginRemoteTrackingRef(branch, options) {
  const ref = `refs/remotes/origin/${branch}`;
  try {
    execLocalGitSync(["show-ref", "--verify", "--quiet", ref], {
      cwd: options.cwd,
      suppressLogs: options.suppressLogs || false,
    });
    return true;
  } catch {
    return false;
  }
}

/**
 * Maximum number of commits in a `base..head` range before it is considered
 * implausible for a shallow checkout.  A shallow clone with `fetch-depth: 1`
 * will report the entire local history as the range when the base ref is not
 * reachable from the shallow grafts, producing a count in the tens-of-thousands
 * even for a single-commit branch.  This threshold guards against that case.
 *
 * Callers can override the threshold via the `maxCommits` option on the
 * functions that accept it.
 */
const SHALLOW_RANGE_MAX_COMMITS = 100;

/**
 * Detect whether a commit range is implausibly large for a shallow checkout.
 *
 * In a shallow clone (`fetch-depth: 1`) the base ref (e.g. `origin/main`) has
 * no traversable ancestry, so `git rev-list base..HEAD` cannot exclude anything
 * and returns essentially the entire repository history.  A count far above what
 * a typical PR branch would contain almost certainly means the base ref is not
 * reachable from the shallow grafts rather than representing real branch work.
 *
 * When the range size exceeds `options.maxCommits` (default
 * `SHALLOW_RANGE_MAX_COMMITS`) **and** the repository is shallow, this function
 * emits a `core.warning` with an actionable message and returns
 * `{ implausible: true, commitCount }`.  Otherwise it returns
 * `{ implausible: false, commitCount }`.
 *
 * All git failures are treated as non-fatal so callers are never blocked.
 * If `git rev-list` fails, returns `{ implausible: false, commitCount: 0 }`.
 * If `git rev-list` succeeds but `git rev-parse --is-shallow-repository` fails,
 * returns `{ implausible: false, commitCount }` with the measured commit count.
 *
 * @param {string} baseRef - The base ref (exclusive). Example: "origin/main".
 * @param {string} headRef - The head ref (inclusive). Example: "HEAD" or a branch name.
 * @param {Object} [options]
 * @param {string} [options.cwd] - Working directory for git commands.
 * @param {number} [options.maxCommits] - Override the implausibility threshold.
 * @returns {{ implausible: boolean, commitCount: number }}
 */
function checkImplausibleShallowRange(baseRef, headRef, options = {}) {
  const { maxCommits = SHALLOW_RANGE_MAX_COMMITS, cwd } = options;
  if (!baseRef || !headRef) return { implausible: false, commitCount: 0 };

  let commitCount = 0;
  try {
    const countOut = execLocalGitSync(["rev-list", "--count", `${baseRef}..${headRef}`], {
      cwd,
      suppressLogs: true,
    });
    commitCount = parseInt(countOut.trim(), 10) || 0;
  } catch {
    return { implausible: false, commitCount: 0 };
  }

  if (!Number.isFinite(commitCount) || commitCount <= maxCommits) {
    return { implausible: false, commitCount };
  }

  // Count is suspiciously large; only flag it as implausible when the repo is
  // actually shallow — a large honest branch in a full clone is fine.
  let isShallow = false;
  try {
    const shallowOut = execLocalGitSync(["rev-parse", "--is-shallow-repository"], {
      cwd,
      suppressLogs: true,
    });
    isShallow = shallowOut.trim() === "true";
  } catch {
    return { implausible: false, commitCount };
  }

  if (isShallow) {
    core.warning(
      `Shallow checkout produced an implausible commit range of ${commitCount} commits for ${baseRef}..${headRef}. ` +
        `This usually means ${baseRef} is not reachable from the shallow grafts. ` +
        `Increase fetch-depth in your workflow checkout step (e.g. fetch-depth: 0) to resolve this.`
    );
    return { implausible: true, commitCount };
  }

  return { implausible: false, commitCount };
}

/**
 * Check whether a commit range contains any merge commits.
 *
 * `git am` (the default patch transport) cannot apply merge commits — it only
 * handles linear patches produced by `git format-patch`. Callers can use this
 * helper to detect when a range requires the `bundle` transport instead, which
 * preserves merge commit topology by transferring git objects directly.
 *
 * Returns `false` (rather than throwing) when the underlying git command fails
 * — for example when one of the refs cannot be resolved. Callers should treat
 * "unknown" as "no merge commits detected" so that a detection failure never
 * blocks the normal patch path.
 *
 * Also returns `false` when the range is implausibly large for a shallow
 * checkout (see `checkImplausibleShallowRange`).  In that case a warning is
 * already emitted by the range check, so forcing bundle transport based on
 * phantom merge-commit detections in a huge, unreliable range is avoided.
 *
 * @param {string} baseRef - The base ref (exclusive). Example: "origin/feature".
 * @param {string} headRef - The head ref (inclusive). Example: "feature".
 * @param {Object} [options]
 * @param {string} [options.cwd] - Working directory for the git command.
 * @param {number} [options.maxCommits] - Override the implausibility threshold.
 * @returns {boolean} True if at least one merge commit exists in baseRef..headRef.
 */
function hasMergeCommitsInRange(baseRef, headRef, options = {}) {
  if (!baseRef || !headRef) return false;

  // Guard: if the range is implausibly large for a shallow checkout, treat as
  // no merge commits so a false-positive does not force bundle transport.
  const { implausible } = checkImplausibleShallowRange(baseRef, headRef, options);
  if (implausible) return false;

  try {
    const out = execLocalGitSync(["rev-list", "--merges", "--count", `${baseRef}..${headRef}`], {
      cwd: options.cwd,
      suppressLogs: true,
    });
    const count = parseInt(out.trim(), 10);
    return Number.isFinite(count) && count > 0;
  } catch {
    // Detection failure — treat as no merge commits to avoid blocking the
    // normal patch path. The caller's downstream patch generation will surface
    // any actionable error.
    return false;
  }
}

const BUNDLE_HISTORY_ERROR =
  "Bundle prerequisites are unavailable locally. Automatic history fetching is disabled by default. " +
  "Provision the required objects during authenticated checkout, or set allow-full-history-fetch: true under safe-outputs.create-pull-request or safe-outputs.push-to-pull-request-branch in workflow frontmatter to enable recovery for that handler. " +
  "Opting in can download the entire repository history; commit depths and blob filters are not byte limits.";

/**
 * Deny every Git transport, including file and external helpers, during local
 * inspection. GIT_ALLOW_PROTOCOL predates the supported Git minimum; unlike
 * GIT_NO_LAZY_FETCH alone, it also protects older Git versions from promisor
 * downloads. Neither caller options nor repository config may relax this guard.
 *
 * @param {Object} [options]
 * @returns {Object}
 */
function offlineGitOptions(options = {}) {
  return {
    ...options,
    env: { ...process.env, ...options.env, GIT_ALLOW_PROTOCOL: "", GIT_NO_LAZY_FETCH: "1", GIT_TERMINAL_PROMPT: "0" },
    silent: true,
  };
}

/**
 * Execute artifact-generation Git commands with every transport denied and
 * repository-configured programs disabled. Generation uses index-only
 * plumbing; other local callers stage/inspect raw files without conversion.
 * @param {string[]} args
 * @param {Object} [options]
 * @returns {string}
 */
function execLocalGitSync(args, options = {}) {
  // Place command-local overrides after caller global options so neither -c
  // nor GIT_CONFIG_* can re-enable hooks, fsmonitor, signing, or auto maintenance.
  const commandIndex = args.findIndex((arg, index) => !arg.startsWith("-") && (index === 0 || !["-c", "-C", "--git-dir", "--work-tree", "--namespace", "--config-env"].includes(args[index - 1])));
  const globalArgs = args.slice(0, commandIndex);
  const commandArgs = args.slice(commandIndex);
  const localOptions = offlineGitOptions(options);
  const filterArgs = [];
  if (["status", "add", "hash-object", "diff", "read-tree", "apply", "checkout", "checkout-index", "reset", "am"].includes(commandArgs[0])) {
    // There is no wildcard -c override for attribute filter drivers. Discover
    // their names without reading values or executing worktree conversions.
    const result = execGitSyncResult([...globalArgs, "config", "--null", "--name-only", "--get-regexp", "^filter\\..*\\.(clean|smudge|process|required)$"], { ...localOptions, input: undefined });
    if (result.status !== 0 && result.status !== 1) throw new Error("Unable to inspect Git filter configuration safely");
    const drivers = new Set(
      result.stdout
        .split("\0")
        .filter(Boolean)
        .map(key => key.replace(/\.(clean|smudge|process|required)$/, ""))
    );
    for (const driver of drivers) {
      // -c separates key/value at '='; refuse an unrepresentable override.
      if (driver.includes("=")) throw new Error("Local Git conversion cannot safely disable a filter driver containing '='");
      filterArgs.push("-c", `${driver}.clean=`, "-c", `${driver}.smudge=`, "-c", `${driver}.process=`, "-c", `${driver}.required=false`);
    }
  }
  if (["diff", "show", "log", "format-patch"].includes(commandArgs[0])) {
    const separator = commandArgs.indexOf("--");
    commandArgs.splice(separator < 0 ? commandArgs.length : separator, 0, "--no-ext-diff", "--no-textconv");
  }
  return execGitSync(
    [
      ...globalArgs,
      "--no-pager",
      "-c",
      "core.hooksPath=/dev/null",
      "-c",
      "core.fsmonitor=false",
      "-c",
      "commit.gpgSign=false",
      "-c",
      "tag.gpgSign=false",
      "-c",
      "log.showSignature=false",
      "-c",
      "diff.submodule=short",
      "-c",
      "status.submoduleSummary=false",
      "-c",
      "gc.auto=0",
      "-c",
      "maintenance.auto=false",
      ...filterArgs,
      ...commandArgs,
    ],
    localOptions
  );
}

/**
 * Stage literal worktree paths as raw blobs without running Git conversions.
 * Filter discovery cannot secure git add against concurrent config changes;
 * hash-object --no-filters and update-index --index-info never run drivers.
 *
 * @param {string[]} files - Repository-relative literal paths, including deletions.
 * @param {import("child_process").SpawnSyncOptions & { cwd: string }} options
 * @returns {void}
 */
function stageRawGitFiles(files, options) {
  const records = [];
  let zeroOid;
  for (const file of files) {
    const segments = file.split("/");
    if (!file || file.includes("\0") || path.isAbsolute(file) || segments.some(segment => !segment || segment === "." || segment === ".." || segment.toLowerCase() === ".git")) {
      throw new Error("Unable to stage an unsafe repository-relative path");
    }
    let fullPath = options.cwd;
    let missing = false;
    for (const segment of segments.slice(0, -1)) {
      fullPath = path.join(fullPath, segment);
      try {
        const parentStats = fs.lstatSync(fullPath);
        if (parentStats.isSymbolicLink()) throw new Error("Unable to stage a path through a symlink");
        if (!parentStats.isDirectory()) {
          missing = true;
          break;
        }
      } catch (error) {
        if (error.code !== "ENOENT") throw error;
        missing = true;
        break;
      }
    }
    fullPath = path.join(options.cwd, file);
    let stats;
    try {
      if (!missing) stats = fs.lstatSync(fullPath);
    } catch (error) {
      if (error.code !== "ENOENT") throw error;
    }
    if (!stats || stats.isDirectory()) {
      zeroOid ||= "0".repeat(execLocalGitSync(["hash-object", "--no-filters", "--stdin"], { ...options, input: Buffer.alloc(0) }).trim().length);
      // Remove old entries before additions for file/directory replacements.
      records.unshift(`0 ${zeroOid}\t${file}\0`);
      continue;
    }
    let bytes;
    let mode;
    if (stats.isSymbolicLink()) {
      bytes = fs.readlinkSync(fullPath, { encoding: "buffer" });
      mode = "120000";
    } else if (stats.isFile()) {
      const fd = fs.openSync(fullPath, fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW);
      try {
        const openedStats = fs.fstatSync(fd);
        if (!openedStats.isFile()) throw new Error("Unable to stage a non-regular file");
        bytes = fs.readFileSync(fd);
        mode = openedStats.mode & 0o111 ? "100755" : "100644";
      } finally {
        fs.closeSync(fd);
      }
    } else {
      throw new Error("Unable to stage a non-file path");
    }
    const oid = execLocalGitSync(["hash-object", "-w", "--no-filters", "--stdin"], { ...options, input: bytes }).trim();
    records.push(`${mode} ${oid}\t${file}\0`);
  }
  if (records.length > 0) {
    execLocalGitSync(["update-index", "-z", "--index-info"], { ...options, input: records.join("") });
  }
}

/**
 * Import a local bundle without opening any transport (including promisor
 * remotes). Unlike fetch, bundle unbundle works with every protocol denied.
 *
 * @param {{ getExecOutput: Function, exec: Function }} execApi
 * @param {string} bundleFilePath
 * @param {string} sourceRef
 * @param {string} targetRef
 * @param {Object} [options]
 * @returns {Promise<void>}
 */
async function importLocalBundle(execApi, bundleFilePath, sourceRef, targetRef, options = {}) {
  const opts = offlineGitOptions(options);
  const result = await execApi.getExecOutput("git", ["bundle", "unbundle", bundleFilePath], { ...opts, ignoreReturnCode: true });
  if (result.exitCode !== 0) {
    if (extractBundlePrerequisiteCommits(result.stderr).length > 0) throw new Error(BUNDLE_HISTORY_ERROR);
    throw new Error("Unable to import local bundle with network access disabled. Verify the bundle and provision its required objects during authenticated checkout.");
  }
  const heads = [];
  for (const line of result.stdout.trim().split("\n")) {
    const match = /^([0-9a-f]{40}) (HEAD|refs\/heads\/\S+)$/.exec(line);
    if (!match) continue;
    const [, sha, ref] = match;
    if (ref !== "HEAD") {
      const valid = await execApi.getExecOutput("git", ["check-ref-format", ref], { ...opts, ignoreReturnCode: true });
      if (valid.exitCode !== 0) continue;
    }
    heads.push({ sha, ref });
  }
  let selected = heads.filter(head => head.ref === sourceRef);
  if (selected.length === 0) {
    selected = heads.filter(head => head.ref.startsWith("refs/heads/"));
    if (selected.length === 0) selected = heads.filter(head => head.ref === "HEAD");
  }
  if (selected.length !== 1) throw new Error("Unable to resolve a unique source ref from the local bundle.");
  await execApi.exec("git", ["update-ref", targetRef, selected[0].sha], opts);
}

/**
 * Extract prerequisite commit SHAs declared in a git bundle file.
 *
 * Runs `git bundle verify <file>` (with `ignoreReturnCode`) and parses the
 * "The bundle requires this ref:" section as well as the
 * "Repository lacks these prerequisite commits:" error block. Both formats
 * list the prerequisite commit SHAs.
 *
 * @param {{ getExecOutput: Function }} execApi
 * @param {string} bundleFilePath
 * @param {Object} [options]
 * @returns {Promise<string[]>} Deduplicated lowercase 40-char SHAs, or [] on failure.
 */
async function getBundlePrerequisites(execApi, bundleFilePath, options = {}) {
  try {
    const { stdout, stderr } = await execApi.getExecOutput("git", ["bundle", "verify", bundleFilePath], { ...offlineGitOptions(options), ignoreReturnCode: true });
    const combined = `${stdout || ""}\n${stderr || ""}`;
    const prereqs = new Set();
    const lines = combined.split(/\r?\n/);
    let inRequires = false;
    for (const line of lines) {
      if (/the bundle (requires|records) (this|these)/i.test(line)) {
        inRequires = true;
        continue;
      }
      if (/the bundle contains/i.test(line)) {
        inRequires = false;
        continue;
      }
      if (inRequires) {
        const match = line.match(/\b([0-9a-f]{40})\b/i);
        if (match) {
          prereqs.add(match[1].toLowerCase());
          continue;
        }
        if (line.trim() === "") {
          inRequires = false;
        }
      }
    }
    // Also pick up "Repository lacks these prerequisite commits:" block.
    for (const sha of extractBundlePrerequisiteCommits(combined)) {
      prereqs.add(sha);
    }
    return [...prereqs];
  } catch (error) {
    core.debug("Unable to inspect local bundle prerequisites");
    return [];
  }
}

/**
 * Check which of the given commit SHAs are NOT present in the local object
 * store. Uses `git cat-file -e <sha>^{commit}`, which exits non-zero when the
 * object is missing.
 *
 * This is the correct gate for bundle application: `git fetch <bundle>` only
 * needs the prerequisite *objects* to exist locally — it does not require them
 * to be reachable from any particular branch. (A prerequisite commit can live
 * on the pull request branch and never be an ancestor of the base branch, so an
 * ancestry-based check would loop forever trying to deepen the base.)
 *
 * @param {{ getExecOutput: Function }} execApi
 * @param {string[]} shas
 * @param {Object} [options]
 * @returns {Promise<string[]>} SHAs whose commit object is not present locally.
 */
async function findMissingObjects(execApi, shas, options = {}) {
  const missing = [];
  for (const sha of shas) {
    const { exitCode } = await execApi.getExecOutput("git", ["cat-file", "-e", `${sha}^{commit}`], { ...offlineGitOptions(options), ignoreReturnCode: true });
    if (exitCode !== 0) {
      missing.push(sha);
    }
  }
  return missing;
}

/**
 * Check bundle prerequisites locally. No recovery downloads are allowed unless
 * trusted workflow configuration explicitly opts into unbounded history fetches.
 *
 * @param {{ getExecOutput: Function, exec: Function }} execApi - Exec API to run git commands.
 * @param {Object} [options] - Options passed through to exec calls.
 * @param {Object} [deepenOptions]
 * @param {string} [deepenOptions.bundleFilePath] - Path to the bundle file whose prerequisites must become reachable.
 * @param {boolean} [deepenOptions.allowFullHistoryFetch=false] - Explicit opt-in to potentially unbounded recovery downloads.
 * @param {string} [deepenOptions.remote="origin"] - Trusted handler-selected repository supplying prerequisites.
 * @param {(fetch: () => Promise<void>) => Promise<void>} [deepenOptions.withRecoveryCredentials] - Scope head-repository credentials to recovery only.
 * @returns {Promise<void>}
 */
async function ensureFullHistoryForBundle(execApi, options = {}, deepenOptions = {}) {
  const { bundleFilePath, allowFullHistoryFetch = false, remote = "origin", withRecoveryCredentials = fetch => fetch() } = deepenOptions || {};
  if (!bundleFilePath) throw new Error("A local bundle path is required to check prerequisites; no history was fetched.");

  const prereqs = await getBundlePrerequisites(execApi, bundleFilePath, options);
  if (prereqs.length === 0) {
    core.info("Bundle declares no prerequisites; no history fetch required");
    return;
  }

  let missing = await findMissingObjects(execApi, prereqs, options);
  if (missing.length === 0) {
    core.info("Bundle prerequisite commits already present locally; no fetch required");
    return;
  }

  if (allowFullHistoryFetch !== true) throw new Error(BUNDLE_HISTORY_ERROR);
  core.warning("Bundle history recovery explicitly enabled; this may download the entire repository history without a byte limit");
  const fetchOptions = { ...options, silent: true, ignoreReturnCode: true };
  try {
    await withRecoveryCredentials(async () => {
      const shallow = await execApi.getExecOutput("git", ["rev-parse", "--is-shallow-repository"], { ...offlineGitOptions(options), ignoreReturnCode: true });
      if (shallow.exitCode !== 0) throw new Error("Shallow status unavailable");
      if (shallow.stdout.trim() === "true") {
        const unshallow = await execApi.getExecOutput("git", ["fetch", "--unshallow", remote], fetchOptions);
        if (unshallow.exitCode !== 0) throw new Error("History fetch failed");
      }
      missing = await findMissingObjects(execApi, prereqs, options);
      if (missing.length > 0) {
        const fetch = await execApi.getExecOutput("git", ["fetch", remote, ...missing], fetchOptions);
        if (fetch.exitCode !== 0) throw new Error("Prerequisite fetch failed");
      }
    });
    missing = await findMissingObjects(execApi, prereqs, options);
  } catch {
    throw new Error("Explicit bundle history recovery failed. Provision the required objects during authenticated checkout with credentials for the recovery repository.");
  }
  if (missing.length > 0) throw new Error("Bundle prerequisites remain unavailable after explicit history recovery. Provision the required objects during authenticated checkout.");
}

/**
 * Return true when the local repository is shallow OR has sparse-checkout enabled.
 *
 * This is the gate for using `--filter=blob:none` on follow-up fetches (e.g. bundle
 * prerequisite recovery). In a full, non-sparse clone the repo already contains all
 * blobs for committed history; adding `--filter=blob:none` to a fetch would convert
 * it to a partial clone and cause subsequent operations to lazily re-fetch blobs.
 * In shallow or sparse checkouts we already accept partial object availability, so
 * filtering blobs is consistent and saves bandwidth.
 *
 * Both probes are best-effort — on any error we return `false` (do not filter),
 * which is the safe default that preserves the legacy unfiltered fetch behavior.
 *
 * @param {{ getExecOutput: Function }} execApi - Exec API to run git commands.
 * @param {Object} [options] - Options passed through to exec calls.
 * @returns {Promise<boolean>}
 */
async function isShallowOrSparseCheckout(execApi, options = {}) {
  const probeOptions = { ...options, ignoreReturnCode: true };
  try {
    const { stdout, exitCode } = await execApi.getExecOutput("git", ["rev-parse", "--is-shallow-repository"], probeOptions);
    if (exitCode === 0 && stdout.trim() === "true") {
      return true;
    }
  } catch {
    // Fall through to sparse check; if both probes fail, return false (no filter).
  }
  try {
    const { stdout, exitCode } = await execApi.getExecOutput("git", ["config", "--get", "core.sparseCheckout"], probeOptions);
    if (exitCode === 0 && stdout.trim().toLowerCase() === "true") {
      return true;
    }
  } catch {
    // Fall through.
  }
  return false;
}

/**
 * Extract prerequisite commit SHAs from git bundle fetch error output.
 *
 * When `git fetch <bundle>` fails because the local repository is missing the
 * bundle's base commits, git prints:
 *   error: Repository lacks these prerequisite commits:
 *   error: <sha1>
 *   error: <sha2>
 *   ...
 *
 * This function parses the raw stderr/error text and returns the deduplicated
 * list of missing commit SHAs so callers can fetch them from origin and retry.
 *
 * NOTE: The @actions/exec `exec()` function throws with a generic
 * "The process '...' failed with exit code 1" message that does NOT include
 * stderr. Callers must use `getExecOutput()` with `ignoreReturnCode: true`
 * and pass the returned `stderr` field to this function.
 *
 * @param {string} message - Raw stderr text from the failed bundle fetch.
 * @returns {string[]} Deduplicated lowercase 40-character commit SHAs, or [] if none found.
 */
function extractBundlePrerequisiteCommits(message) {
  if (!message || !/lacks these prerequisite commits/i.test(message)) {
    return [];
  }
  return [...new Set((message.match(/\b[0-9a-f]{40}\b/gi) || []).map(sha => sha.toLowerCase()))];
}

/**
 * Backfill the full object content (trees + blobs) of specific commits from
 * origin, WITHOUT an uncontrolled `--unshallow` or unbounded deepen.
 *
 * Use this to recover an operation (e.g. `git rebase --onto`) that failed in a
 * shallow + partial (`--filter=blob:none`) clone because git tried to lazily
 * fetch objects from the promisor remote and the fetch was rejected. Instead of
 * downloading a huge monorepo's entire history, we fetch *exactly* the commit
 * SHAs the operation needs (`git fetch --no-filter origin <sha>...`). Passing
 * `--no-filter` overrides the clone's configured `blob:none` partial-clone
 * filter so the server sends the missing blobs for the reachable range — and
 * nothing beyond what those anchor commits reach.
 *
 * Fetch-by-SHA is not a byte bound: the reachable history can be large. This
 * helper is for signed-push recovery, not default bundle prerequisite recovery.
 *
 * @param {{ getExecOutput: Function }} execApi - Exec API to run git commands.
 * @param {Array<string | undefined>} commitShas - Anchor commit SHAs whose object content must be present.
 * @param {Object} [options] - Exec options (cwd, env, ...). `ignoreReturnCode` is forced on.
 * @returns {Promise<boolean>} True when the targeted fetch succeeds.
 */
async function backfillCommitObjects(execApi, commitShas, options = {}) {
  const targets = [...new Set((commitShas || []).filter(sha => typeof sha === "string" && /^[0-9a-f]{40}$/i.test(sha)))];
  if (targets.length === 0) {
    return false;
  }
  const fetchOptions = { ...options, ignoreReturnCode: true };
  try {
    const { exitCode, stderr } = await execApi.getExecOutput("git", ["fetch", "--no-filter", "origin", ...targets], fetchOptions);
    if (exitCode !== 0) {
      core.warning(`backfillCommitObjects: targeted fetch exited with code ${exitCode}: ${String(stderr || "").trim()}`);
    }
    return exitCode === 0;
  } catch (error) {
    core.warning(`backfillCommitObjects: targeted fetch failed: ${getErrorMessage(error)}`);
    return false;
  }
}

/**
 * Rewrite the commit range `baseRef..HEAD` as a single regular commit carrying the same tree.
 *
 * Saves the current HEAD, soft-resets to `baseRef`, validates that at least one file is
 * staged, and recommits under `commitMessage`.  On any failure the original HEAD is restored
 * via `reset --hard` and the error is re-thrown so the caller can surface an actionable
 * message.
 *
 * @param {string} baseRef - The base ref to reset to (e.g. `"origin/main"` or a SHA).
 * @param {string} commitMessage - Commit message for the linearized commit.
 * @param {{ exec: Function, getExecOutput: Function }} execApi - Actions exec API (e.g. the `exec` global).
 * @param {Object} [opts]
 * @param {Object} [opts.gitOpts] - Extra options passed to every exec call (e.g. `{ cwd }`).
 *   When omitted, exec calls are made without additional options.
 * @param {string[]} [opts.commitFlags] - Extra flags prepended before `-m` in the `git commit`
 *   invocation (e.g. `["--allow-empty", "--no-verify"]`).
 * @param {string[]} [opts.excludedFiles] - Paths that should be removed from the staged rewrite
 *   before creating the linearized commit.
 * @param {string} [opts.rebaseOnto] - Optional ref to replay the synthesized commit onto after
 *   it has been linearized relative to `baseRef`. Use this when `baseRef` captures the agent's
 *   actual change base but the resulting single commit must sit on a newer branch tip.
 * @param {number} [opts.maxCommits] - Override the implausibility threshold (default
 *   `SHALLOW_RANGE_MAX_COMMITS`).  Set to `Infinity` to disable the shallow guard.
 * @returns {Promise<string>} The new HEAD SHA after the rewrite.
 * @throws {Error} If the soft reset, staged-changes validation, or recommit fails, or if a
 *   shallow checkout produces an implausible commit range.
 */
async function linearizeRangeAsCommit(baseRef, commitMessage, execApi, opts = {}) {
  const { gitOpts, commitFlags = [], excludedFiles = [], rebaseOnto, maxCommits = SHALLOW_RANGE_MAX_COMMITS } = opts;
  // Spread gitOpts into exec calls only when it is explicitly provided — passing
  // `undefined` as a third argument changes the arity seen by mocks in tests.
  const execArgs = gitOpts !== undefined ? [gitOpts] : [];

  // Guard against implausibly large commit ranges in shallow checkouts.
  // In a shallow clone with fetch-depth:1 the base ref has no traversable
  // ancestry, so git rev-list cannot exclude anything and returns the entire
  // local history.  Synthesizing a rewrite commit from tens-of-thousands of
  // commits is almost certainly wrong and could produce an oversized payload.
  {
    let rangeCommitCount = 0;
    try {
      const { stdout: countOut } = await execApi.getExecOutput("git", ["rev-list", "--count", `${baseRef}..HEAD`], ...execArgs);
      rangeCommitCount = parseInt(countOut.trim(), 10) || 0;
    } catch {
      rangeCommitCount = 0;
    }
    if (Number.isFinite(rangeCommitCount) && rangeCommitCount > maxCommits) {
      let isShallow = false;
      try {
        const shallowOpts = gitOpts !== undefined ? { ...gitOpts, ignoreReturnCode: true } : { ignoreReturnCode: true };
        const { stdout: shallowOut } = await execApi.getExecOutput("git", ["rev-parse", "--is-shallow-repository"], shallowOpts);
        isShallow = shallowOut.trim() === "true";
      } catch {
        // Non-fatal: if the shallow probe fails, do not block the rewrite.
      }
      if (isShallow) {
        throw new Error(
          `${ERR_SYSTEM}: Refusing to linearize an implausible commit range: ${rangeCommitCount} commits in ${baseRef}..HEAD in a shallow checkout. ` +
            `This likely means ${baseRef} is not reachable from the shallow grafts. ` +
            `Increase fetch-depth in your workflow checkout step (e.g. fetch-depth: 0) to resolve this.`
        );
      }
    }
  }

  const { stdout: originalHeadOut } = await execApi.getExecOutput("git", ["rev-parse", "HEAD"], ...execArgs);
  const originalHead = originalHeadOut.trim();
  if (!originalHead) {
    throw new Error(`${ERR_SYSTEM}: ` + "Could not resolve current HEAD before linearizing range");
  }

  // Track whether a `git rebase` call was started so the catch block can distinguish
  // "rebase in progress" (needs --abort) from "pre-rebase failure" (needs reset only).
  let rebaseStarted = false;
  try {
    await execApi.exec("git", ["reset", "--soft", baseRef], ...execArgs);
    if (Array.isArray(excludedFiles) && excludedFiles.length > 0) {
      const { stdout: excludedStagedOut } = await execApi.getExecOutput("git", ["diff", "--cached", "--name-only", "--", ...excludedFiles], ...execArgs);
      if (excludedStagedOut.trim()) {
        // Use `git reset HEAD -- <files>` rather than `git checkout HEAD -- <files>`.
        // For newly-added excluded files (not present in HEAD), `checkout` fails with
        // "pathspec did not match any file(s) known to git". `reset HEAD --` handles
        // both cases: removes new files from the index and restores modified files to
        // the HEAD version, without touching the working tree.
        await execApi.exec("git", ["reset", "HEAD", "--", ...excludedFiles], ...execArgs);
        // For excluded files that were modifications (not new additions), the working tree
        // still has the agent's version while the index was just restored to HEAD. This
        // creates an unstaged change that would cause `git rebase --onto` to fail.
        // Detect any such unstaged changes among the excluded files and restore them from
        // the index so the working tree stays in sync before the commit and rebase steps.
        const { stdout: modifiedExcludedOut } = await execApi.getExecOutput("git", ["diff", "--name-only", "--", ...excludedFiles], ...execArgs);
        const modifiedExcluded = modifiedExcludedOut.trim().split("\n").filter(Boolean);
        if (modifiedExcluded.length > 0) {
          await execApi.exec("git", ["checkout", "--", ...modifiedExcluded], ...execArgs);
        }
      }
    }
    const { stdout: stagedFilesOut } = await execApi.getExecOutput("git", ["diff", "--cached", "--name-only"], ...execArgs);
    if (!stagedFilesOut.trim()) {
      throw new Error(`${ERR_SYSTEM}: No staged changes found after soft reset to ${baseRef}. ` + `The commit range may contain only no-op or empty commits. ` + `Ensure your commits contain actual file changes before pushing.`);
    }
    await execApi.exec("git", ["commit", ...commitFlags, "-m", commitMessage], ...execArgs);
    if (typeof rebaseOnto === "string" && rebaseOnto.trim() && rebaseOnto.trim() !== baseRef.trim()) {
      rebaseStarted = true;
      await execApi.exec("git", ["rebase", "--onto", rebaseOnto.trim(), baseRef, "HEAD"], ...execArgs);
      rebaseStarted = false;
      // Guard: if the rebase silently dropped the commit (became empty relative to rebaseOnto),
      // the agent's changes are lost. Detect and fail loudly rather than pushing an empty diff.
      const { stdout: diffOut } = await execApi.getExecOutput("git", ["diff", "--name-only", rebaseOnto.trim(), "HEAD"], ...execArgs);
      if (!diffOut.trim()) {
        throw new Error(`${ERR_SYSTEM}: Rebase onto ${rebaseOnto} produced no changes; the synthesized commit was dropped as empty`);
      }
    }
    const { stdout: newHeadOut } = await execApi.getExecOutput("git", ["rev-parse", "HEAD"], ...execArgs);
    return newHeadOut.trim();
  } catch (rewriteError) {
    try {
      if (rebaseStarted) {
        // A rebase was in progress when the error occurred; abort it to restore the repo to its
        // pre-rebase state before the hard reset below finishes the rollback.
        try {
          await execApi.exec("git", ["rebase", "--abort"], ...execArgs);
        } catch (abortError) {
          // --abort failed while a rebase was genuinely in progress — repo may be in a dirty state.
          core.error(`linearizeRangeAsCommit: rebase --abort also failed: ${getErrorMessage(abortError)}`);
        }
      }
      await execApi.exec("git", ["reset", "--hard", originalHead], ...execArgs);
      core.warning(`linearizeRangeAsCommit: rewrite failed; restored original HEAD ${originalHead}`);
    } catch (restoreError) {
      core.warning(`linearizeRangeAsCommit: rollback also failed: ${getErrorMessage(restoreError)}`);
    }
    throw new Error(`${ERR_SYSTEM}: Failed to linearize ${baseRef}..HEAD as a single commit: ${getErrorMessage(rewriteError)}`, { cause: rewriteError });
  }
}

module.exports = {
  stageRawGitFiles,
  ensureSafeDirectoryTrust,
  isTransientGitError,
  withGitRetry,
  execGitSync,
  execGitSyncResult,
  execLocalGitSync,
  backfillCommitObjects,
  checkImplausibleShallowRange,
  ensureFullHistoryForBundle,
  importLocalBundle,
  BUNDLE_HISTORY_ERROR,
  offlineGitOptions,
  hasLocalOriginRemoteTrackingRef,
  extractBundlePrerequisiteCommits,
  getBundlePrerequisites,
  getGitAuthEnv,
  hasMergeCommitsInRange,
  isShallowOrSparseCheckout,
  linearizeRangeAsCommit,
  SHALLOW_RANGE_MAX_COMMITS,
};
