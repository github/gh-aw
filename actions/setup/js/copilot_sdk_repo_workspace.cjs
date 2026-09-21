// @ts-check

"use strict";

const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { runCopilotSDKRepoProcess } = require("./copilot_sdk_repo_process.cjs");
const { lstatGuard } = require("./symlink_guard.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { repositoryCommandError } = require("./copilot_sdk_repo_diagnostics.cjs");

const MAX_REPOSITORY_FILE_BYTES = 2 * 1024 * 1024;
const MAX_REPOSITORY_FILES = 500;
const REPOSITORY_OUTPUT_BYTES = 256 * 1024;
const GIT_TIMEOUT_MS = 30_000;
const GO_TIMEOUT_MS = 5 * 60_000;

/** @param {string} root @param {string} candidate */
function isWithin(root, candidate) {
  const relative = path.relative(root, candidate);
  return relative === "" || (!path.isAbsolute(relative) && relative !== ".." && !relative.startsWith(`..${path.sep}`));
}

/** @param {string} filename */
function validateRepositoryPath(filename) {
  if (typeof filename !== "string" || !filename || /[\x00-\x1f\x7f\\]/.test(filename) || path.isAbsolute(filename) || filename.split("/").some(part => !part || part === "." || part === ".." || part.toLowerCase() === ".git")) {
    throw new Error("Repository operation encountered an unsafe or Git-internal path");
  }
}

/** @param {string} root @param {string} filename @param {boolean} [allowMissing] */
function inspectRepositoryFile(root, filename, allowMissing = false) {
  validateRepositoryPath(filename);
  let current = root;
  let stat;
  for (const segment of filename.split("/")) {
    current = path.join(current, segment);
    try {
      stat = lstatGuard(current);
    } catch (error) {
      if (allowMissing && error && typeof error === "object" && "code" in error && error.code === "ENOENT") return null;
      throw error;
    }
    if (!stat || !isWithin(root, fs.realpathSync(current))) throw new Error("Repository operations cannot follow symlinks outside their checkout");
  }
  if (!stat?.isFile() || stat.nlink !== 1) throw new Error("Repository operations require regular, non-hardlinked files");
  if (stat.size > MAX_REPOSITORY_FILE_BYTES) throw new Error(`Repository file exceeds the ${MAX_REPOSITORY_FILE_BYTES}-byte operation limit`);
  return stat;
}

/** @param {string} root @param {string} filename @param {import("node:fs").Stats} inspected */
function readRepositoryFile(root, filename, inspected) {
  const fd = fs.openSync(path.join(root, filename), fs.constants.O_RDONLY | (fs.constants.O_NOFOLLOW || 0));
  try {
    const opened = fs.fstatSync(fd);
    if (!opened.isFile() || opened.nlink !== 1 || opened.dev !== inspected.dev || opened.ino !== inspected.ino) throw new Error("Repository file changed while it was being opened");
    const buffer = Buffer.alloc(MAX_REPOSITORY_FILE_BYTES + 1);
    let length = 0;
    while (length < buffer.length) {
      const count = fs.readSync(fd, buffer, length, buffer.length - length, null);
      if (count === 0) break;
      length += count;
    }
    if (length > MAX_REPOSITORY_FILE_BYTES) throw new Error(`Repository file exceeds the ${MAX_REPOSITORY_FILE_BYTES}-byte operation limit`);
    return buffer.subarray(0, length);
  } finally {
    fs.closeSync(fd);
  }
}

/** @param {string} output */
function parseRepositoryPaths(output) {
  if (output && !output.endsWith("\0")) throw new Error("Git returned an incomplete file list");
  const files = output ? output.slice(0, -1).split("\0") : [];
  if (files.length > MAX_REPOSITORY_FILES) throw new Error(`Repository operation exceeds the ${MAX_REPOSITORY_FILES}-file limit`);
  for (const filename of files) validateRepositoryPath(filename);
  return [...new Set(files)].sort();
}

/** @param {string} output @returns {Array<{filename: string, deleted: boolean}>} */
function parseRepositoryChanges(output) {
  if (output && !output.endsWith("\0")) throw new Error("Git returned an incomplete change list");
  const fields = output ? output.slice(0, -1).split("\0") : [];
  if (fields.length % 2 !== 0 || fields.length / 2 > MAX_REPOSITORY_FILES) throw new Error("Git returned an invalid or oversized change list");
  const changes = [];
  for (let index = 0; index < fields.length; index += 2) {
    if (!["A", "M", "D", "T"].includes(fields[index])) throw new Error("Repository changes contain an unsupported or conflicted Git status");
    validateRepositoryPath(fields[index + 1]);
    changes.push({ filename: fields[index + 1], deleted: fields[index] === "D" });
  }
  return changes;
}

/** @param {string} name @param {NodeJS.ProcessEnv} env @param {string} root */
function findRepositoryExecutable(name, env, root) {
  const executable = process.platform === "win32" ? `${name}.exe` : name;
  const directories = (env.PATH || env.Path || "").split(path.delimiter);
  if (name !== "git" && env.GOROOT) directories.unshift(path.join(env.GOROOT, "bin"));
  for (const directory of directories) {
    if (!directory || !path.isAbsolute(directory)) continue;
    const candidate = path.join(directory, executable);
    let resolved;
    try {
      resolved = fs.realpathSync(candidate);
    } catch (error) {
      if (error && typeof error === "object" && "code" in error && ["ENOENT", "ENOTDIR"].includes(String(error.code))) continue;
      throw error;
    }
    if (isWithin(root, resolved)) throw new Error(`Repository-owned ${name} executables are not permitted`);
    if (!fs.statSync(resolved).isFile()) throw new Error(`The configured ${name} executable is not a regular file`);
    fs.accessSync(resolved, fs.constants.X_OK);
    return resolved;
  }
  throw new Error(`The go-repository profile requires a prepared ${name} executable`);
}

/** @param {string | undefined} value @param {string} fallback @param {string} root */
function cacheDirectory(value, fallback, root) {
  const selected = value || fallback;
  if (!path.isAbsolute(selected)) throw new Error("Go cache directories must be absolute");
  if (!value) fs.mkdirSync(selected, { recursive: true });
  const resolved = fs.realpathSync(selected);
  if (!fs.statSync(resolved).isDirectory() || isWithin(root, resolved)) throw new Error("Go caches must be prepared outside the repository checkout");
  return resolved;
}

/**
 * Build one owned workspace context. Child commands inherit only fixed Go/Git
 * configuration, prepared cache paths and essential OS variables, never SDK
 * credentials, provider settings or GitHub Actions command-file variables.
 *
 * @param {import("./copilot_sdk_repo_policy.cjs").GoRepositoryProfile} profile
 * @param {{env?: NodeJS.ProcessEnv, runProcess?: typeof runCopilotSDKRepoProcess, diagnosticSecrets?: string[]}} [options]
 */
function createRepositoryWorkspace(profile, { env = process.env, runProcess = runCopilotSDKRepoProcess, diagnosticSecrets = [] } = {}) {
  if (!env.GITHUB_WORKSPACE || !path.isAbsolute(env.GITHUB_WORKSPACE)) throw new Error("go-repository requires an absolute GITHUB_WORKSPACE");
  const root = fs.realpathSync(env.GITHUB_WORKSPACE);
  const rootStat = lstatGuard(root);
  if (!rootStat?.isDirectory()) throw new Error("go-repository requires a repository checkout directory");
  const gitDirectory = path.join(root, ".git");
  const gitStat = lstatGuard(gitDirectory);
  if (!gitStat?.isDirectory() || !isWithin(root, fs.realpathSync(gitDirectory))) {
    throw new Error("go-repository requires a single checkout with an in-workspace .git directory");
  }
  const rootIdentity = { dev: rootStat.dev, ino: rootStat.ino };
  const gitIdentity = { dev: gitStat.dev, ino: gitStat.ino };
  const repository = env.GITHUB_REPOSITORY;
  if (!repository || !/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(repository)) throw new Error("go-repository requires GITHUB_REPOSITORY");
  for (const configured of [profile.policy["target-repo"], profile.policy.current_checkout_repo]) {
    if (configured && configured.toLowerCase() !== repository.toLowerCase()) throw new Error("go-repository cannot operate on a different target checkout");
  }
  if (profile.policy.patch_workspace_path && profile.policy.patch_workspace_path !== ".") throw new Error("go-repository supports only the root repository checkout");
  if (env.GH_AW_ENGINE_CWD && fs.realpathSync(env.GH_AW_ENGINE_CWD) !== root) throw new Error("go-repository cannot override the checkout working directory");
  inspectRepositoryFile(root, "go.mod");
  const executables = {
    git: findRepositoryExecutable("git", env, root),
    go: findRepositoryExecutable("go", env, root),
    gofmt: findRepositoryExecutable("gofmt", env, root),
  };
  const goRoot = fs.realpathSync(env.GOROOT || path.dirname(path.dirname(executables.go)));
  if (!path.isAbsolute(goRoot) || isWithin(root, goRoot) || !fs.statSync(goRoot).isDirectory()) throw new Error("GOROOT must be a prepared directory outside the checkout");
  const privateRoot = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-go-repository-"));
  const home = path.join(privateRoot, "home");
  const temporary = path.join(privateRoot, "tmp");
  const hooks = path.join(privateRoot, "hooks");
  const gitConfig = path.join(privateRoot, "gitconfig");
  try {
    fs.chmodSync(privateRoot, 0o700);
    for (const directory of [home, temporary, hooks]) fs.mkdirSync(directory);
    fs.writeFileSync(gitConfig, "", { flag: "wx", mode: 0o600 });
    /** @type {NodeJS.ProcessEnv} */
    const childEnv = {};
    for (const name of ["SystemRoot", "WINDIR", "PATHEXT", "CI", "GITHUB_ACTIONS"]) {
      if (env[name]) childEnv[name] = env[name];
    }
    const executableDirectories = [...new Set(Object.values(executables).map(executable => path.dirname(executable)))];
    const systemDirectories = [];
    for (const directory of (env.PATH || env.Path || "").split(path.delimiter)) {
      if (!directory || !path.isAbsolute(directory)) continue;
      let resolved;
      try {
        resolved = fs.realpathSync(directory);
      } catch (error) {
        if (error && typeof error === "object" && "code" in error && ["ENOENT", "ENOTDIR"].includes(String(error.code))) continue;
        throw error;
      }
      if (!isWithin(root, resolved) && fs.statSync(resolved).isDirectory()) systemDirectories.push(resolved);
    }
    Object.assign(childEnv, {
      PATH: [...new Set([...executableDirectories, ...systemDirectories])].join(path.delimiter),
      HOME: home,
      USERPROFILE: home,
      APPDATA: home,
      LOCALAPPDATA: home,
      XDG_CONFIG_HOME: home,
      TMPDIR: temporary,
      TEMP: temporary,
      TMP: temporary,
      GOROOT: goRoot,
      GOPATH: path.join(privateRoot, "gopath"),
      GOCACHE: cacheDirectory(env.GOCACHE, path.join(privateRoot, "go-build"), root),
      GOMODCACHE: cacheDirectory(env.GOMODCACHE, path.join(privateRoot, "go-mod"), root),
      GOTOOLCHAIN: "local",
      GOFLAGS: "-mod=readonly",
      GOPROXY: "off",
      GOSUMDB: "off",
      GOENV: "off",
      GOWORK: "off",
      GIT_CONFIG_NOSYSTEM: "1",
      GIT_CONFIG_SYSTEM: gitConfig,
      GIT_CONFIG_GLOBAL: gitConfig,
      GIT_TERMINAL_PROMPT: "0",
      GIT_ALLOW_PROTOCOL: "",
      GIT_NO_LAZY_FETCH: "1",
      GIT_NO_REPLACE_OBJECTS: "1",
      GIT_AUTHOR_NAME: "github-actions[bot]",
      GIT_AUTHOR_EMAIL: "github-actions[bot]@users.noreply.github.com",
      GIT_COMMITTER_NAME: "github-actions[bot]",
      GIT_COMMITTER_EMAIL: "github-actions[bot]@users.noreply.github.com",
      LANG: "C.UTF-8",
      LC_ALL: "C.UTF-8",
    });
    const gitArguments = ["--no-pager", "-c", `core.hooksPath=${hooks}`, "-c", "core.fsmonitor=false", "-c", "core.untrackedCache=false", "-c", "commit.gpgSign=false", "-c", "protocol.allow=never"];
    /**
     * @param {"git" | "go" | "gofmt"} executable
     * @param {string[]} args
     * @param {AbortSignal} signal
     * @param {{indexFile?: string, allowFailure?: boolean, directory?: string, maxOutputBytes?: number}} [settings]
     */
    async function run(executable, args, signal, { indexFile, allowFailure = false, directory = root, maxOutputBytes = REPOSITORY_OUTPUT_BYTES } = {}) {
      const currentRoot = lstatGuard(root);
      const currentGit = lstatGuard(gitDirectory);
      if (!currentRoot?.isDirectory() || !currentGit?.isDirectory() || currentRoot.ino !== rootIdentity.ino || currentRoot.dev !== rootIdentity.dev || currentGit.ino !== gitIdentity.ino || currentGit.dev !== gitIdentity.dev) {
        throw new Error("Repository checkout or Git directory was replaced during execution");
      }
      const cwd = fs.realpathSync(directory);
      if (!lstatGuard(directory)?.isDirectory() || (cwd !== root && !isWithin(privateRoot, cwd))) throw new Error("Repository operations require the checkout or an owned validation directory");
      if (cwd !== root && !(executable === "git" && args[0] === "init")) {
        const projectedGit = path.join(cwd, ".git");
        if (!lstatGuard(projectedGit)?.isDirectory() || !isWithin(cwd, fs.realpathSync(projectedGit))) throw new Error("Validation Git metadata was replaced");
      }
      if (indexFile && !isWithin(privateRoot, path.resolve(indexFile))) throw new Error("Repository index files must stay in owned temporary storage");
      if (!Number.isSafeInteger(maxOutputBytes) || maxOutputBytes <= 0 || maxOutputBytes > 32 * 1024 * 1024) throw new Error("Invalid bounded repository output size");
      if (executable === "go") inspectRepositoryFile(cwd, "go.mod");
      const scopedGitArguments = [...gitArguments, `--git-dir=${path.join(cwd, ".git")}`, `--work-tree=${cwd}`, "-c", `safe.directory=${cwd}`];
      const result = await runProcess({
        command: executables[executable],
        args: executable === "git" ? [...scopedGitArguments, ...args] : args,
        cwd,
        env: indexFile ? { ...childEnv, GIT_INDEX_FILE: indexFile } : { ...childEnv },
        signal,
        timeoutMs: executable === "git" ? GIT_TIMEOUT_MS : GO_TIMEOUT_MS,
        maxOutputBytes,
      });
      if (!allowFailure && result.exitCode !== 0) {
        throw repositoryCommandError(`${executable} ${args[0]}`, result, diagnosticSecrets);
      }
      return result;
    }
    /**
     * @template T
     * @param {string} prefix
     * @param {(directory: string) => Promise<T>} operation
     * @returns {Promise<T>}
     */
    async function withTemporaryDirectory(prefix, operation) {
      if (!/^[a-z]+-$/.test(prefix)) throw new Error("Invalid internal temporary directory prefix");
      const directory = fs.mkdtempSync(path.join(temporary, prefix));
      let failed = false;
      /** @type {unknown} */
      let failure;
      try {
        return await operation(directory);
      } catch (error) {
        failed = true;
        failure = error;
        throw error;
      } finally {
        try {
          await fs.promises.rm(directory, { recursive: true, maxRetries: 3, retryDelay: 100 });
        } catch (error) {
          const previous = failure && typeof failure === "object" && "cleanupErrors" in failure && Array.isArray(failure.cleanupErrors) ? failure.cleanupErrors : [];
          const message = `${failed ? `${getErrorMessage(failure)}; ` : ""}repository temporary cleanup failed: ${getErrorMessage(error)}`;
          throw Object.assign(new AggregateError(failed ? [failure, error] : [error], message), { cleanupErrors: [...previous, error] });
        }
      }
    }
    function cleanupSync() {
      fs.rmSync(privateRoot, { recursive: true, maxRetries: 3, retryDelay: 100 });
    }
    function cleanup() {
      return fs.promises.rm(privateRoot, { recursive: true, maxRetries: 3, retryDelay: 100 });
    }
    return { root, privateRoot, temporary, hooks, sourceCommit: env.GITHUB_SHA, childEnv, executables, diagnosticSecrets, run, withTemporaryDirectory, cleanup, cleanupSync };
  } catch (error) {
    try {
      fs.rmSync(privateRoot, { recursive: true });
    } catch (cleanupError) {
      throw new AggregateError([error, cleanupError], "Repository workspace initialization and cleanup failed");
    }
    throw error;
  }
}

module.exports = { MAX_REPOSITORY_FILE_BYTES, MAX_REPOSITORY_FILES, REPOSITORY_OUTPUT_BYTES, createRepositoryWorkspace, inspectRepositoryFile, readRepositoryFile, parseRepositoryPaths, parseRepositoryChanges, isWithin };
