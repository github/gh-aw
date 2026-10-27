// @ts-check

const fs = require("fs");
const path = require("path");
const { execFileSync, spawn } = require("child_process");
const { createInterface } = require("readline");
const { isRepoAllowed } = require("./repo_helpers.cjs");
const { getSetupTimeoutMs } = require("./child_process_timeouts.cjs");

// Only controlled messages may be propagated to workflow failure issues.
class CheckoutPreparationError extends Error {}

/**
 * @param {string | undefined} value
 */
function validateSingleRepository(value) {
  if (!value || !/^[a-zA-Z0-9][a-zA-Z0-9-]*\/[a-zA-Z0-9_.-]+$/.test(value.trim()) || [".", ".."].includes(value.trim().split("/")[1])) {
    throw new CheckoutPreparationError("checkout.repository (or repo) must resolve to exactly one owner/repo slug unless checkout.multi is true");
  }
}

/**
 * @param {string} value
 * @returns {string[]}
 */
function parseRepositories(value) {
  const repos = value
    .split(/\r?\n/)
    .map(repo => repo.trim())
    .filter(Boolean);
  for (const repo of repos) {
    if (!/^[a-zA-Z0-9][a-zA-Z0-9-]*\/[a-zA-Z0-9_.-]+$/.test(repo) || [".", ".."].includes(repo.split("/")[1])) {
      throw new CheckoutPreparationError("Runtime checkout repositories must be newline-separated owner/repo slugs; wildcards are only supported in checkout.allowed-repos");
    }
  }
  return [...new Set(repos.map(repo => repo.toLowerCase()))];
}

/**
 * @param {string} value
 * @returns {Set<string>}
 */
function parseAllowedRepos(value) {
  const patterns = value
    .split(/[\r\n,]+/)
    .map(pattern => pattern.trim().toLowerCase())
    .filter(Boolean);
  if (patterns.length === 0) {
    throw new CheckoutPreparationError("Runtime checkout requires a non-empty allowed-repos list");
  }
  for (const pattern of patterns) {
    if (!/^[a-zA-Z0-9*-]+\/[a-zA-Z0-9_.*-]+$/.test(pattern) || /^[*/]+$/.test(pattern)) {
      throw new CheckoutPreparationError("checkout.allowed-repos must contain scoped patterns such as owner/repo, owner/*, or */repo; unrestricted wildcards are not allowed");
    }
  }
  return new Set(patterns);
}

/**
 * Check every existing ancestor before writing or allowing actions/checkout to clean a directory.
 * @param {string} workspace
 * @param {string} relative
 * @param {boolean} [mustBeAbsent]
 * @returns {string}
 */
function workspacePath(workspace, relative, mustBeAbsent = false) {
  if (!relative || path.isAbsolute(relative) || relative.includes("\\") || relative.includes("${{") || relative.split("/").some(part => part === ".." || part === ".git") || /[\x00-\x1f\x7f]/.test(relative)) {
    throw new CheckoutPreparationError("Runtime checkout paths must stay within GITHUB_WORKSPACE");
  }
  let root;
  try {
    root = fs.realpathSync(workspace);
  } catch (error) {
    throw new CheckoutPreparationError("Unable to resolve the runtime checkout workspace", { cause: error });
  }
  const target = path.resolve(root, relative);
  if (target === root || !target.startsWith(root + path.sep)) {
    throw new CheckoutPreparationError("Runtime checkout paths must stay within GITHUB_WORKSPACE");
  }
  let current = root;
  for (const part of path.relative(root, target).split(path.sep)) {
    current = path.join(current, part);
    let stat;
    try {
      stat = fs.lstatSync(current);
    } catch (error) {
      if (error instanceof Error && "code" in error && error.code === "ENOENT") continue;
      throw error;
    }
    if (!stat.isDirectory() || stat.isSymbolicLink()) {
      throw new CheckoutPreparationError("Runtime checkout paths must not contain symlinks or non-directory ancestors");
    }
    if (mustBeAbsent && current === target) {
      throw new CheckoutPreparationError("Runtime checkout destination already exists; use a different path prefix");
    }
  }
  return target;
}

function readConfig() {
  const index = process.env.GH_AW_DYNAMIC_CHECKOUT_INDEX || "";
  if (!/^\d+$/.test(index)) throw new CheckoutPreparationError("Runtime checkout index must be a non-negative integer");
  const workspace = process.env.GITHUB_WORKSPACE;
  const runnerTemp = process.env.RUNNER_TEMP;
  if (!workspace || !runnerTemp) throw new CheckoutPreparationError("GITHUB_WORKSPACE and RUNNER_TEMP are required for runtime checkout");
  const prefix = process.env.GH_AW_DYNAMIC_PREFIX || "";
  workspacePath(workspace, prefix);
  if (prefix.split("/").includes(".gh-aw")) throw new CheckoutPreparationError("Runtime checkout prefix must not use the reserved .gh-aw directory");
  return { index, workspace, runnerTemp, prefix };
}

/**
 * @typedef {{ repository: string, path: string, default_branch: string }} DynamicCheckout
 * @typedef {{ core: { setSecret: (value: string) => void, setOutput: (name: string, value: string) => void }, github: import('@octokit/core').Octokit, getOctokit: (token: string) => import('@octokit/core').Octokit }} CheckoutRuntime
 * @param {CheckoutRuntime} runtime
 */
async function prepare(runtime) {
  const token = process.env.GH_AW_DYNAMIC_TOKEN || "";
  if (token) runtime.core.setSecret(token);
  try {
    return await prepareCheckouts(runtime, token);
  } catch (error) {
    runtime.core.setOutput("checkout_error", error instanceof CheckoutPreparationError ? error.message : "Runtime checkout preparation failed; inspect the failed step for details");
    throw error;
  }
}

/**
 * @param {CheckoutRuntime} runtime
 * @param {string} token
 */
async function prepareCheckouts({ github, getOctokit }, token) {
  const config = readConfig();
  const repos = parseRepositories(process.env.GH_AW_DYNAMIC_REPOSITORIES || "");
  const allowed = parseAllowedRepos(process.env.GH_AW_DYNAMIC_ALLOWED_REPOS || "");
  const allowPrivateRepos = process.env.GH_AW_DYNAMIC_ALLOW_PRIVATE_REPOS ?? "false";
  if (!["false", "true"].includes(allowPrivateRepos)) {
    throw new CheckoutPreparationError("checkout.allow-private-repos must be a literal boolean; use true or false");
  }
  const maxReposValue = process.env.GH_AW_DYNAMIC_MAX_REPOS ?? "10";
  const maxRepos = Number(maxReposValue);
  if (!/^[1-9]\d*$/.test(maxReposValue) || !Number.isSafeInteger(maxRepos)) {
    throw new CheckoutPreparationError("checkout.max-repos must be a positive integer no greater than 9007199254740991");
  }
  if (repos.length > maxRepos) {
    throw new CheckoutPreparationError(`Runtime checkout selected ${repos.length} repositories, exceeding checkout.max-repos (${maxRepos}); reduce the repository list or raise max-repos`);
  }
  if ((process.env.GH_AW_DYNAMIC_LFS || "false") !== "false") {
    throw new CheckoutPreparationError("Runtime repository-list checkout does not support lfs: true because LFS endpoints can bypass allowed-repos; disable checkout.lfs");
  }
  if ((process.env.GH_AW_DYNAMIC_SUBMODULES || "false") !== "false") {
    throw new CheckoutPreparationError("Runtime repository-list checkout does not support enabled submodules because submodule URLs can bypass allowed-repos; set checkout.submodules to false");
  }
  for (const repository of repos) {
    if (!isRepoAllowed(repository, allowed)) throw new CheckoutPreparationError(`Runtime checkout repository ${repository} is not permitted by allowed-repos`);
  }
  if (!token) throw new CheckoutPreparationError("Runtime checkout requires a GitHub token");
  const client = getOctokit(token);
  const host = parseRepositories(process.env.GITHUB_REPOSITORY || "");
  if (host.length !== 1) throw new CheckoutPreparationError("GITHUB_REPOSITORY must identify the workflow repository");
  const [owner, repo] = host[0].split("/");
  let hostData;
  try {
    hostData = (await github.request("GET /repos/{owner}/{repo}", { owner, repo })).data;
  } catch {
    throw new CheckoutPreparationError("Unable to verify workflow repository visibility; runtime checkout refused");
  }
  if (typeof hostData.private !== "boolean" || !["public", "private", "internal"].includes(hostData.visibility)) {
    throw new CheckoutPreparationError("Unable to verify workflow repository visibility; runtime checkout refused");
  }
  const isPublic = hostData.visibility === "public" || hostData.private === false;
  /** @type {DynamicCheckout[]} */
  const entries = [];
  for (const repository of repos) {
    const [targetOwner, targetRepo] = repository.split("/");
    let data;
    try {
      data = (await client.request("GET /repos/{owner}/{repo}", { owner: targetOwner, repo: targetRepo })).data;
    } catch {
      throw new CheckoutPreparationError(`Unable to verify runtime checkout repository ${repository}; checkout refused`);
    }
    const canonical = parseRepositories(String(data.full_name || ""));
    if (
      canonical.length !== 1 ||
      !isRepoAllowed(canonical[0], allowed) ||
      typeof data.private !== "boolean" ||
      !["public", "private", "internal"].includes(data.visibility) ||
      typeof data.default_branch !== "string" ||
      !data.default_branch
    ) {
      throw new CheckoutPreparationError(`Unable to verify allowlist and metadata for runtime checkout repository ${repository}`);
    }
    if (isPublic && (data.private || data.visibility !== "public")) {
      throw new CheckoutPreparationError(`Public workflows cannot check out non-public repository ${repository}`);
    }
    if (allowPrivateRepos !== "true" && (data.private || data.visibility !== "public")) {
      throw new CheckoutPreparationError(`Runtime checkout of non-public repository ${repository} requires checkout.allow-private-repos: true`);
    }
    if (entries.some(entry => entry.repository === canonical[0])) continue;
    const checkoutPath = path.posix.join(config.prefix, canonical[0]);
    workspacePath(config.workspace, checkoutPath, true);
    entries.push({ repository: canonical[0], path: checkoutPath, default_branch: data.default_branch });
  }
  const action = process.env.GH_AW_DYNAMIC_CHECKOUT_ACTION || "";
  if (!/^actions\/checkout@[a-f0-9]{40}$/.test(action)) throw new CheckoutPreparationError("Runtime checkout requires a SHA-pinned actions/checkout");
  const actionDir = workspacePath(config.workspace, `.gh-aw/checkouts/${config.index}`, true);
  const steps = entries.map(entry => ({
    uses: action,
    with: {
      repository: entry.repository,
      path: entry.path,
      ref: "${{ env.GH_AW_DYNAMIC_REF }}",
      token: "${{ env.GH_AW_DYNAMIC_TOKEN }}",
      "fetch-depth": "${{ env.GH_AW_DYNAMIC_FETCH_DEPTH }}",
      "sparse-checkout": "${{ env.GH_AW_DYNAMIC_SPARSE }}",
      filter: process.env.GH_AW_DYNAMIC_SPARSE ? "blob:limit=1073741824" : "",
      // Do not initialize repository-controlled .gitmodules; later fetches also disable recursion.
      submodules: false,
      lfs: false,
      "persist-credentials": "${{ env.GH_AW_DYNAMIC_PERSIST }}",
    },
  }));
  // A composite action must contain at least one step, even for an empty scheduled batch.
  try {
    fs.mkdirSync(actionDir, { recursive: true });
    fs.writeFileSync(
      path.join(actionDir, "action.yml"),
      JSON.stringify({
        name: "Runtime repository checkout",
        description: "Checkout prevalidated runtime repositories",
        runs: { using: "composite", steps: steps.length ? steps : [{ shell: "bash", run: "true" }] },
      })
    );
  } catch (error) {
    throw new CheckoutPreparationError("Unable to write the validated runtime checkout action", { cause: error });
  }
  const planDir = path.join(config.runnerTemp, "gh-aw", "dynamic-checkouts");
  try {
    fs.mkdirSync(planDir, { recursive: true });
    fs.writeFileSync(path.join(planDir, `${config.index}.json`), JSON.stringify(entries));
  } catch (error) {
    throw new CheckoutPreparationError("Unable to write the runtime checkout plan", { cause: error });
  }
  return entries;
}

/**
 * @param {string} dir
 * @param {string} runnerTemp
 */
function cleanCheckoutCredentials(dir, runnerTemp) {
  try {
    const gitDir = path.join(dir, ".git");
    if (!fs.lstatSync(gitDir).isDirectory() || fs.lstatSync(gitDir).isSymbolicLink()) {
      throw new Error("Runtime checkout must have a regular .git directory");
    }
    const configs = [path.join(gitDir, "config")];
    const modules = path.join(gitDir, "modules");
    const pending = fs.readdirSync(gitDir).includes("modules") ? [modules] : [];
    while (pending.length) {
      const current = pending.pop();
      if (!current) throw new Error("Missing runtime checkout submodule directory");
      if (fs.lstatSync(current).isSymbolicLink()) throw new Error("Runtime checkout submodule configs must not use symlinks");
      for (const entry of fs.readdirSync(current, { withFileTypes: true })) {
        const file = path.join(current, entry.name);
        if (entry.isSymbolicLink()) throw new Error("Runtime checkout submodule configs must not use symlinks");
        if (entry.isDirectory()) pending.push(file);
        else if (entry.name === "config") configs.push(file);
      }
    }
    for (const config of configs) {
      const stat = fs.lstatSync(config);
      if (!stat.isFile() || stat.isSymbolicLink()) throw new Error("Runtime checkout must have a regular git config");
      for (const script of ["clean_git_credentials.sh", "verify_git_credentials.sh"]) {
        execFileSync("bash", [path.join(runnerTemp, "gh-aw", "actions", script), config], {
          env: process.env,
          stdio: ["ignore", "pipe", "pipe"],
          timeout: getSetupTimeoutMs("gitBranch"),
        });
      }
    }
  } catch {
    throw new Error("Unable to clean and verify runtime checkout credentials; refusing agent execution");
  }
}

/**
 * @param {string} source
 * @param {string} line
 */
function matchesSourceRef(source, line) {
  const ref = line.split("\t")[1];
  if (!ref) return false;
  const wildcard = source.indexOf("*");
  if (wildcard === -1) return ref === source;
  return ref.length >= source.length - 1 && ref.startsWith(source.slice(0, wildcard)) && ref.endsWith(source.slice(wildcard + 1));
}

/**
 * @param {string[]} args
 * @param {NodeJS.ProcessEnv} env
 * @param {string} source
 * @returns {Promise<boolean>}
 */
function discoverRemoteRef(args, env, source) {
  return new Promise((resolve, reject) => {
    const child = spawn("git", args, { env, stdio: ["ignore", "pipe", "ignore"], timeout: getSetupTimeoutMs("importGit") });
    let matched = false;
    createInterface({ input: child.stdout }).on("line", line => {
      if (matchesSourceRef(source, line)) matched = true;
    });
    child.on("error", reject);
    child.on("close", status => {
      if (status === 0) resolve(matched);
      else if (status === 2) resolve(false);
      else reject(new Error("Unable to discover runtime checkout refs"));
    });
  });
}

/**
 * @param {{ cleanCredentials?: boolean, runGit?: (args: string[], options: import('child_process').ExecFileSyncOptionsWithStringEncoding) => string }} [options]
 */
async function finish(options = {}) {
  const config = readConfig();
  /** @type {DynamicCheckout[]} */
  let entries;
  try {
    entries = JSON.parse(fs.readFileSync(path.join(config.runnerTemp, "gh-aw", "dynamic-checkouts", `${config.index}.json`), "utf8"));
  } catch (error) {
    throw new Error("Unable to read the validated runtime checkout plan", { cause: error });
  }
  if (!Array.isArray(entries)) throw new Error("Runtime checkout plan must contain an array");
  const count = Number(process.env.GH_AW_DYNAMIC_FETCH_COUNT || "0");
  if (!Number.isFinite(count) || !Number.isSafeInteger(count) || count < 0) throw new Error("Runtime checkout fetch count must be a non-negative integer");
  const refs = Array.from({ length: count }, (_, index) => {
    const ref = process.env[`GH_AW_DYNAMIC_FETCH_${index}`];
    if (!ref || !ref.trim()) throw new Error("Runtime checkout fetch refs must not be empty");
    return ref;
  });
  const token = process.env.GH_AW_DYNAMIC_TOKEN || "";
  let server;
  try {
    server = new URL(process.env.GITHUB_SERVER_URL || "https://github.com");
  } catch (error) {
    throw new Error("Runtime checkout requires a valid GitHub server URL", { cause: error });
  }
  if (server.protocol !== "https:" || server.username || server.password) throw new Error("Runtime checkout requires an HTTPS GitHub server");
  for (const entry of entries) {
    const dir = workspacePath(config.workspace, entry.path);
    if (options.cleanCredentials) cleanCheckoutCredentials(dir, config.runnerTemp);
    const run = async (/** @type {string[]} */ args, authenticated = false, allowedExitCode = 0) => {
      const env = { ...process.env, GIT_TERMINAL_PROMPT: "0" };
      if (authenticated) {
        Object.assign(env, {
          GIT_CONFIG_COUNT: "2",
          GIT_CONFIG_KEY_0: `http.${server.origin}/.extraheader`,
          GIT_CONFIG_VALUE_0: "",
          GIT_CONFIG_KEY_1: `http.${server.origin}/.extraheader`,
          GIT_CONFIG_VALUE_1: `AUTHORIZATION: basic ${Buffer.from(`x-access-token:${token}`).toString("base64")}`,
        });
      }
      try {
        const command = ["-C", dir, ...args];
        if (args[0] === "ls-remote" && !options.runGit) return await discoverRemoteRef(command, env, args[args.length - 1]);
        /** @type {import('child_process').ExecFileSyncOptionsWithStringEncoding} */
        const execOptions = { encoding: "utf8", env, stdio: ["ignore", "pipe", "pipe"], timeout: getSetupTimeoutMs(authenticated ? "importGit" : "gitBranch") };
        const output = options.runGit ? options.runGit(command, execOptions) : execFileSync("git", command, execOptions);
        if (args[0] === "ls-remote") return output.split("\n").some(line => matchesSourceRef(args[args.length - 1], line));
        return true;
      } catch (error) {
        if (allowedExitCode && error instanceof Error && "status" in error && error.status === allowedExitCode) return null;
        throw new Error(`Unable to finalize runtime checkout of ${entry.repository}`);
      }
    };
    if (process.env.GH_AW_DYNAMIC_SPARSE) {
      await run(["config", "--local", "--unset-all", "remote.origin.promisor"], false, 5);
      await run(["config", "--local", "--unset-all", "remote.origin.partialclonefilter"], false, 5);
    }
    if (refs.length) {
      const depth = Number(process.env.GH_AW_DYNAMIC_FETCH_DEPTH || "1");
      if (!Number.isSafeInteger(depth) || depth < 0) throw new Error("Runtime checkout fetch-depth must be a non-negative integer");
      const refspecs = refs.map(ref => {
        if (ref === "*") return "+refs/heads/*:refs/remotes/origin/*";
        if (ref === "refs/pulls/open/*") return "+refs/pull/*/head:refs/remotes/origin/pull/*/head";
        return `+refs/heads/${ref}:refs/remotes/origin/${ref}`;
      });
      for (const refspec of refspecs) {
        const source = refspec.slice(1).split(":")[0];
        if (!(await run(["ls-remote", "--exit-code", "origin", source], true, 2))) continue;
        await run(["fetch", "--no-recurse-submodules", "origin", ...(depth ? [`--depth=${depth}`] : []), refspec], true);
      }
    }
  }
  const actionDir = workspacePath(config.workspace, `.gh-aw/checkouts/${config.index}`);
  try {
    fs.unlinkSync(path.join(actionDir, "action.yml"));
    fs.rmdirSync(actionDir);
  } catch (error) {
    throw new Error("Unable to remove the temporary runtime checkout action", { cause: error });
  }
  return entries;
}

module.exports = { prepare, finish, parseRepositories, parseAllowedRepos, workspacePath, validateSingleRepository };
