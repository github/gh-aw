// @ts-check
/// <reference types="@actions/github-script" />

require("./shim.cjs");

const fs = require("fs");
const path = require("path");
const { execFileSync } = require("child_process");

const { getSetupTimeoutMs } = require("./child_process_timeouts.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");

const GIT_COMMAND_TIMEOUT_MS = getSetupTimeoutMs("gitBranch");
const GH_COMMAND_TIMEOUT_MS = getSetupTimeoutMs("outcomeGh");

function parseManifestEntries(entriesJSON = process.env.GH_AW_CHECKOUT_MANIFEST_ENTRIES || "[]") {
  let parsed;
  try {
    parsed = JSON.parse(entriesJSON);
  } catch (err) {
    throw new Error("Failed to parse GH_AW_CHECKOUT_MANIFEST_ENTRIES: " + getErrorMessage(err), { cause: err });
  }
  if (!Array.isArray(parsed)) {
    throw new Error("GH_AW_CHECKOUT_MANIFEST_ENTRIES must be a JSON array");
  }
  return parsed;
}

function readManifestEntriesFromEnv() {
  const count = Number.parseInt(process.env.GH_AW_CHECKOUT_MANIFEST_COUNT || "0", 10);
  if (!Number.isFinite(count) || count < 0) {
    throw new Error("GH_AW_CHECKOUT_MANIFEST_COUNT must be a non-negative integer");
  }

  const entries = [];
  for (let i = 0; i < count; i += 1) {
    entries.push({
      repository: process.env[`GH_AW_CHECKOUT_REPO_${i}`] || "",
      path: process.env[`GH_AW_CHECKOUT_PATH_${i}`] || "",
      token: process.env[`GH_AW_CHECKOUT_TOKEN_${i}`] || "",
    });
  }
  return entries;
}

function resolveDefaultBranch(repository, checkoutPath, options = {}) {
  const workspace = options.workspace || process.env.GITHUB_WORKSPACE || "";
  const runGit =
    options.runGit ||
    ((args, execOptions = {}) => {
      try {
        return execFileSync("git", args, { encoding: "utf8", ...execOptions });
      } catch (err) {
        throw new Error(`Failed to run git ${args.join(" ")}: ${getErrorMessage(err)}`, { cause: err });
      }
    });
  const runGH =
    options.runGH ||
    ((args, execOptions = {}) => {
      try {
        return execFileSync("gh", args, {
          encoding: "utf8",
          env: { ...process.env, ...(execOptions.env || {}) },
          ...execOptions,
        });
      } catch (err) {
        throw new Error(`Failed to run gh ${args.join(" ")}: ${getErrorMessage(err)}`, { cause: err });
      }
    });
  let defaultBranch = "";

  const repoPath = checkoutPath ? path.join(workspace, checkoutPath) : workspace;
  if (repoPath && fs.existsSync(path.join(repoPath, ".git"))) {
    try {
      const output = runGit(["-C", repoPath, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"], {
        stdio: ["ignore", "pipe", "pipe"],
        timeout: GIT_COMMAND_TIMEOUT_MS,
      });
      defaultBranch = output.trim().replace(/^origin\//, "");
      core.debug(`build_checkout_manifest: git resolved default branch for ${repository}: ${defaultBranch}`);
    } catch (error) {
      core.debug(`build_checkout_manifest: git default branch lookup failed for ${repository}: ${getErrorMessage(error)}`);
    }
  }

  if (defaultBranch === "") {
    try {
      const checkoutToken = options.checkoutToken || "";
      const ghExecOptions = {
        stdio: ["ignore", "pipe", "pipe"],
        timeout: GH_COMMAND_TIMEOUT_MS,
      };
      if (checkoutToken !== "") {
        ghExecOptions.env = { GH_TOKEN: checkoutToken };
      }
      defaultBranch = runGH(["api", `repos/${repository}`, "--jq", ".default_branch"], ghExecOptions).trim();
      core.debug(`build_checkout_manifest: gh api resolved default branch for ${repository}: ${defaultBranch}`);
    } catch (error) {
      core.debug(`build_checkout_manifest: gh api default branch lookup failed for ${repository}: ${getErrorMessage(error)}`);
    }
  }

  return defaultBranch;
}

function buildCheckoutManifest(entries, options = {}) {
  const runnerTemp = options.runnerTemp || process.env.RUNNER_TEMP;
  if (!runnerTemp) {
    throw new Error("RUNNER_TEMP is required to build checkout manifest");
  }

  const runGit = options.runGit;
  const runGH = options.runGH;

  // Write under safeoutputs/ because that subdirectory is the only part of
  // $RUNNER_TEMP/gh-aw that is bind-mounted into the containerized safe-outputs
  // MCP server, which is where the manifest is read by findRepoCheckout.
  const manifestDir = path.join(runnerTemp, "gh-aw", "safeoutputs");
  try {
    fs.mkdirSync(manifestDir, { recursive: true });
  } catch (err) {
    throw new Error(`Failed to create directory ${manifestDir}: ${getErrorMessage(err)}`, { cause: err });
  }
  const manifestPath = path.join(manifestDir, "checkout-manifest.json");
  let manifest = {};
  try {
    if (fs.existsSync(manifestPath)) {
      const existing = JSON.parse(fs.readFileSync(manifestPath, "utf8"));
      if (existing && typeof existing === "object" && !Array.isArray(existing)) {
        manifest = existing;
      }
    }
  } catch (error) {
    core.debug(`checkout-manifest: ignoring unreadable existing manifest: ${getErrorMessage(error)}`);
  }
  core.info(`checkout-manifest: building manifest for ${entries.length} checkout entries`);

  for (const entry of entries) {
    if (!entry || typeof entry !== "object") {
      core.debug("checkout-manifest: skipping non-object entry");
      continue;
    }
    const repository = String(entry.repository || "").trim();
    if (repository === "") {
      core.debug("checkout-manifest: skipping entry with empty repository");
      continue;
    }
    const checkoutPath = String(entry.path || "");
    const defaultBranch = resolveDefaultBranch(repository, checkoutPath, {
      workspace: options.workspace,
      runGit,
      runGH,
      checkoutToken: entry.token || "",
    });
    manifest[repository.toLowerCase()] = {
      repository,
      path: checkoutPath,
      default_branch: defaultBranch,
    };
    core.info(`checkout-manifest: ${repository} -> path=${checkoutPath} default_branch=${defaultBranch || "<unresolved>"}`);
  }

  try {
    fs.writeFileSync(manifestPath, JSON.stringify(manifest, null, 2) + "\n", "utf8");
  } catch (err) {
    throw new Error(`Failed to write file ${manifestPath}: ${getErrorMessage(err)}`, { cause: err });
  }
  core.info(`checkout-manifest written to ${manifestPath}`);
  return { manifestPath, manifest };
}

async function main(options = {}) {
  let entries;
  if (typeof options.entriesJSON === "string" && options.entriesJSON.trim() !== "") {
    entries = parseManifestEntries(options.entriesJSON);
  } else {
    entries = readManifestEntriesFromEnv();
  }
  return buildCheckoutManifest(entries, options);
}

module.exports = {
  buildCheckoutManifest,
  main,
  parseManifestEntries,
  readManifestEntriesFromEnv,
  resolveDefaultBranch,
};
