// @ts-check
/// <reference types="@actions/github-script" />

require("./shim.cjs");

const fs = require("fs");
const path = require("path");
const { getErrorMessage } = require("./error_helpers.cjs");

const supportedFields = new Set(["repository", "ref", "path", "github-token", "token", "fetch-depth", "sparse-checkout", "submodules", "lfs", "wiki"]);

function parseDynamicCheckouts(value = process.env.GH_AW_DYNAMIC_CHECKOUTS || "") {
  if (!value.trim()) {
    return [];
  }
  let parsed;
  try {
    parsed = JSON.parse(value);
  } catch (error) {
    throw new Error(`checkout expression must resolve to a JSON object or array: ${getErrorMessage(error)}`, { cause: error });
  }
  const entries = Array.isArray(parsed) ? parsed : [parsed];
  if (entries.some(entry => !entry || typeof entry !== "object" || Array.isArray(entry))) {
    throw new Error("checkout expression must resolve to a checkout object or an array of checkout objects");
  }
  return entries;
}

function parseAllowedRepos(value = process.env.GH_AW_DYNAMIC_CHECKOUT_ALLOWED_REPOS || "") {
  let parsed;
  try {
    parsed = JSON.parse(value);
  } catch (error) {
    throw new Error(`dynamic checkout allowed-repos must resolve to a JSON array: ${getErrorMessage(error)}`, { cause: error });
  }
  if (!Array.isArray(parsed) || parsed.length === 0 || parsed.some(repository => typeof repository !== "string" || !/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(repository))) {
    throw new Error("dynamic checkout allowed-repos must resolve to a non-empty array of owner/repo names");
  }
  return new Set(parsed.map(repository => repository.toLowerCase()));
}

function normalizeCheckout(entry, workspace) {
  for (const field of Object.keys(entry)) {
    if (!supportedFields.has(field)) {
      throw new Error(`dynamic checkout field '${field}' is not supported`);
    }
  }

  let repository = String(entry.repository || "").trim();
  if (!/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(repository)) {
    throw new Error(`dynamic checkout repository must use owner/repo format, got '${repository}'`);
  }
  if (entry.lfs !== undefined && typeof entry.lfs !== "boolean") {
    throw new Error("dynamic checkout lfs must be a boolean");
  }
  if (entry.wiki !== undefined && typeof entry.wiki !== "boolean") {
    throw new Error("dynamic checkout wiki must be a boolean");
  }

  let checkoutPath = entry.path === undefined ? repository.split("/")[1] : String(entry.path).trim();
  checkoutPath = checkoutPath.replaceAll("\\", "/").replace(/^\.\//, "");
  if (!checkoutPath || path.isAbsolute(checkoutPath) || checkoutPath.split("/").includes("..")) {
    throw new Error(`dynamic checkout path must be a non-empty relative path under the workspace, got '${checkoutPath}'`);
  }

  const fetchDepth = entry["fetch-depth"] === undefined ? 1 : Number(entry["fetch-depth"]);
  if (!Number.isInteger(fetchDepth) || fetchDepth < 0) {
    throw new Error("dynamic checkout fetch-depth must be a non-negative integer");
  }
  if (entry.submodules !== undefined && !["true", "false", "recursive", true, false].includes(entry.submodules)) {
    throw new Error("dynamic checkout submodules must be true, false, or recursive");
  }

  if (entry.wiki === true) {
    repository += ".wiki";
  }
  const ref = String(entry.ref || "").trim();
  if (ref.startsWith("-")) {
    throw new Error("dynamic checkout ref must not start with '-'");
  }
  return {
    repository,
    ref,
    path: checkoutPath,
    token: String(entry["github-token"] || entry.token || ""),
    fetchDepth,
    sparseCheckout: String(entry["sparse-checkout"] || ""),
    submodules: entry.submodules,
    lfs: entry.lfs === true,
  };
}

function assertSafeSparsePatterns(patterns) {
  if (patterns.some(pattern => pattern.startsWith("-"))) {
    throw new Error("dynamic checkout sparse-checkout patterns must not start with '-'");
  }
}

async function defaultRunGit(args, options = {}) {
  const result = await exec.getExecOutput("git", args, { silent: true, ...options });
  if (result.exitCode !== 0) {
    throw new Error("git command failed while preparing a dynamic checkout");
  }
  return result.stdout.trim();
}

function credentialArgs(serverURL, token, maskSecret = value => core.setSecret(value)) {
  if (!token) {
    return [];
  }
  maskSecret(token);
  const encoded = Buffer.from(`x-access-token:${token}`).toString("base64");
  maskSecret(encoded);
  return ["-c", `http.${serverURL}/.extraheader=AUTHORIZATION: basic ${encoded}`];
}

async function checkoutRepository(checkout, options = {}) {
  const workspace = options.workspace || process.env.GITHUB_WORKSPACE || "";
  const serverURL = (options.serverURL || process.env.GITHUB_SERVER_URL || "https://github.com").replace(/\/+$/, "");
  const token = checkout.token || options.overrideToken || process.env.GH_TOKEN || "";
  const persistCredentials = options.persistCredentials === true;
  const runGit = options.runGit || defaultRunGit;

  let workspaceReal;
  let checkoutTarget;
  try {
    workspaceReal = fs.realpathSync(workspace);
    checkoutTarget = path.join(workspaceReal, checkout.path);
    if (fs.existsSync(checkoutTarget)) {
      throw new Error(`dynamic checkout path already exists: ${checkout.path}`);
    }

    let parent = workspaceReal;
    const parentSegments = path
      .dirname(checkout.path)
      .split(path.sep)
      .filter(segment => segment && segment !== ".");
    for (const segment of parentSegments) {
      const candidate = path.join(parent, segment);
      if (fs.existsSync(candidate)) {
        if (fs.lstatSync(candidate).isSymbolicLink()) {
          throw new Error(`dynamic checkout path traverses a symbolic link: ${checkout.path}`);
        }
      } else {
        fs.mkdirSync(candidate);
      }
      parent = fs.realpathSync(candidate);
      if (parent !== workspaceReal && !parent.startsWith(workspaceReal + path.sep)) {
        throw new Error(`dynamic checkout path escapes the workspace: ${checkout.path}`);
      }
    }
  } catch (error) {
    throw new Error(`failed to prepare dynamic checkout path '${checkout.path}': ${getErrorMessage(error)}`, { cause: error });
  }

  const authArgs = credentialArgs(serverURL, token, options.maskSecret);
  const worktreeOptions = { env: { ...process.env, GIT_LFS_SKIP_SMUDGE: "1" } };
  const cloneArgs = [...authArgs, "clone", "--no-tags"];
  if (checkout.fetchDepth > 0) {
    cloneArgs.push("--depth", String(checkout.fetchDepth));
  }
  cloneArgs.push(`${serverURL}/${checkout.repository}.git`, checkoutTarget);

  core.info(`Checking out ${checkout.repository} into ${checkout.path}`);
  await runGit(cloneArgs, worktreeOptions);
  if (checkout.ref) {
    const fetchArgs = [...authArgs, "-C", checkoutTarget, "fetch", "--no-tags"];
    if (checkout.fetchDepth > 0) {
      fetchArgs.push("--depth", String(checkout.fetchDepth));
    }
    fetchArgs.push("origin", "--", checkout.ref);
    await runGit(fetchArgs);
    await runGit(["-C", checkoutTarget, "checkout", "--force", "FETCH_HEAD"], worktreeOptions);
  }

  if (checkout.sparseCheckout.trim()) {
    const patterns = checkout.sparseCheckout
      .split(/\r?\n/)
      .map(pattern => pattern.trim())
      .filter(Boolean);
    assertSafeSparsePatterns(patterns);
    await runGit(["-C", checkoutTarget, "sparse-checkout", "set", "--no-cone", "--", ...patterns], worktreeOptions);
  }
  if (checkout.submodules === true || checkout.submodules === "true" || checkout.submodules === "recursive") {
    const args = [...authArgs, "-C", checkoutTarget, "submodule", "update", "--init"];
    if (checkout.submodules === "recursive") {
      args.push("--recursive");
    }
    await runGit(args, worktreeOptions);
  }
  if (checkout.lfs) {
    await runGit([...authArgs, "-C", checkoutTarget, "lfs", "pull"]);
  }

  const credentialKey = `http.${serverURL}/.extraheader`;
  if (persistCredentials && token) {
    const encoded = Buffer.from(`x-access-token:${token}`).toString("base64");
    await runGit(["-C", checkoutTarget, "config", credentialKey, `AUTHORIZATION: basic ${encoded}`]);
  } else {
    try {
      await runGit(["-C", checkoutTarget, "config", "--unset-all", credentialKey]);
    } catch (error) {
      core.debug(`No persisted credential needed removal: ${getErrorMessage(error)}`);
    }
  }

  let defaultBranch = "";
  try {
    defaultBranch = (await runGit(["-C", checkoutTarget, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"])).replace(/^origin\//, "");
  } catch (error) {
    core.debug(`Could not resolve dynamic checkout default branch: ${getErrorMessage(error)}`);
  }
  return {
    repository: checkout.repository,
    path: checkout.path,
    default_branch: defaultBranch,
  };
}

function writeManifest(entries, runnerTemp = process.env.RUNNER_TEMP || "") {
  if (!runnerTemp) {
    throw new Error("RUNNER_TEMP is required for dynamic checkouts");
  }
  const manifestDir = path.join(runnerTemp, "gh-aw", "safeoutputs");
  const manifestPath = path.join(manifestDir, "checkout-manifest.json");
  let manifest = {};
  try {
    fs.mkdirSync(manifestDir, { recursive: true });
    if (fs.existsSync(manifestPath)) {
      manifest = JSON.parse(fs.readFileSync(manifestPath, "utf8"));
    }
    for (const entry of entries) {
      manifest[entry.repository.toLowerCase()] = entry;
    }
    fs.writeFileSync(manifestPath, JSON.stringify(manifest, null, 2) + "\n", "utf8");
  } catch (error) {
    throw new Error(`failed to write dynamic checkout manifest: ${getErrorMessage(error)}`, { cause: error });
  }
  return manifestPath;
}

async function main(options = {}) {
  const workspace = options.workspace || process.env.GITHUB_WORKSPACE || "";
  if (!workspace) {
    throw new Error("GITHUB_WORKSPACE is required for dynamic checkouts");
  }
  const checkouts = parseDynamicCheckouts(options.value);
  const normalized = checkouts.map(entry => normalizeCheckout(entry, workspace));
  const allowedRepos = parseAllowedRepos(options.allowedRepos);
  for (const checkout of normalized) {
    if (!allowedRepos.has(checkout.repository.toLowerCase())) {
      throw new Error(`dynamic checkout repository '${checkout.repository}' is not in allowed-repos`);
    }
  }
  if (new Set(normalized.map(entry => entry.path.toLowerCase())).size !== normalized.length) {
    throw new Error("dynamic checkout paths must be unique");
  }

  const persistCredentials = options.persistCredentials ?? process.env.GH_AW_DYNAMIC_CHECKOUT_PERSIST_CREDENTIALS === "true";
  const overrideToken = options.overrideToken ?? process.env.GH_AW_DYNAMIC_CHECKOUT_TOKEN ?? "";
  const manifestEntries = [];
  for (const checkout of normalized) {
    manifestEntries.push(
      await checkoutRepository(checkout, {
        ...options,
        workspace,
        persistCredentials,
        overrideToken,
      })
    );
  }
  writeManifest(manifestEntries, options.runnerTemp);
  return manifestEntries;
}

module.exports = {
  checkoutRepository,
  main,
  normalizeCheckout,
  parseAllowedRepos,
  parseDynamicCheckouts,
  writeManifest,
};
