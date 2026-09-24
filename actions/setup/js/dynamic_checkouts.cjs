// @ts-check
/// <reference types="@actions/github-script" />

require("./shim.cjs");

const fs = require("fs");
const path = require("path");
const { getErrorMessage } = require("./error_helpers.cjs");

const supportedFields = new Set(["repository", "ref", "path", "github-token", "token", "fetch-depth", "sparse-checkout", "submodules", "lfs", "current", "wiki"]);

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
  if (entry.current !== undefined && typeof entry.current !== "boolean") {
    throw new Error("dynamic checkout current must be a boolean");
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
  return {
    repository,
    ref: String(entry.ref || "").trim(),
    path: checkoutPath,
    target: path.resolve(workspace, checkoutPath),
    token: String(entry["github-token"] || entry.token || ""),
    fetchDepth,
    sparseCheckout: String(entry["sparse-checkout"] || ""),
    submodules: entry.submodules,
    lfs: entry.lfs === true,
    current: entry.current === true,
  };
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
  const token = options.overrideToken || checkout.token || process.env.GH_TOKEN || "";
  const persistCredentials = options.persistCredentials === true;
  const runGit = options.runGit || defaultRunGit;

  let workspaceReal;
  try {
    workspaceReal = fs.realpathSync(workspace);
    if (fs.existsSync(checkout.target)) {
      throw new Error(`dynamic checkout path already exists: ${checkout.path}`);
    }
    fs.mkdirSync(path.dirname(checkout.target), { recursive: true });
    const parentReal = fs.realpathSync(path.dirname(checkout.target));
    if (parentReal !== workspaceReal && !parentReal.startsWith(workspaceReal + path.sep)) {
      throw new Error(`dynamic checkout path escapes the workspace through a symbolic link: ${checkout.path}`);
    }
  } catch (error) {
    throw new Error(`failed to prepare dynamic checkout path '${checkout.path}': ${getErrorMessage(error)}`, { cause: error });
  }

  const authArgs = credentialArgs(serverURL, token, options.maskSecret);
  const cloneArgs = [...authArgs, "clone", "--no-tags"];
  if (checkout.fetchDepth > 0) {
    cloneArgs.push("--depth", String(checkout.fetchDepth));
  }
  cloneArgs.push(`${serverURL}/${checkout.repository}.git`, checkout.target);

  core.info(`Checking out ${checkout.repository} into ${checkout.path}`);
  await runGit(cloneArgs);
  if (checkout.ref) {
    const fetchArgs = [...authArgs, "-C", checkout.target, "fetch", "--no-tags"];
    if (checkout.fetchDepth > 0) {
      fetchArgs.push("--depth", String(checkout.fetchDepth));
    }
    fetchArgs.push("origin", checkout.ref);
    await runGit(fetchArgs);
    await runGit(["-C", checkout.target, "checkout", "--force", "FETCH_HEAD"]);
  }

  if (checkout.sparseCheckout.trim()) {
    const patterns = checkout.sparseCheckout
      .split(/\r?\n/)
      .map(pattern => pattern.trim())
      .filter(Boolean);
    await runGit(["-C", checkout.target, "sparse-checkout", "set", "--no-cone", ...patterns]);
  }
  if (checkout.submodules === true || checkout.submodules === "true" || checkout.submodules === "recursive") {
    const args = [...authArgs, "-C", checkout.target, "submodule", "update", "--init"];
    if (checkout.submodules === "recursive") {
      args.push("--recursive");
    }
    await runGit(args);
  }
  if (checkout.lfs) {
    await runGit([...authArgs, "-C", checkout.target, "lfs", "pull"]);
  }

  const credentialKey = `http.${serverURL}/.extraheader`;
  if (persistCredentials && token) {
    const encoded = Buffer.from(`x-access-token:${token}`).toString("base64");
    await runGit(["-C", checkout.target, "config", credentialKey, `AUTHORIZATION: basic ${encoded}`]);
  } else {
    try {
      await runGit(["-C", checkout.target, "config", "--unset-all", credentialKey]);
    } catch (error) {
      core.debug(`No persisted credential needed removal: ${getErrorMessage(error)}`);
    }
  }

  let defaultBranch = "";
  try {
    defaultBranch = (await runGit(["-C", checkout.target, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"])).replace(/^origin\//, "");
  } catch (error) {
    core.debug(`Could not resolve dynamic checkout default branch: ${getErrorMessage(error)}`);
  }
  return {
    repository: checkout.repository,
    path: checkout.path,
    default_branch: defaultBranch,
    current: checkout.current,
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
  if (normalized.filter(entry => entry.current).length > 1) {
    throw new Error("only one dynamic checkout may set current: true");
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
  parseDynamicCheckouts,
  writeManifest,
};
