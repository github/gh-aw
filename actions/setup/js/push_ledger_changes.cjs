// @ts-check
"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { execGitSync } = require("./git_helpers.cjs");
const { getGitAuthEnv } = require("./git_auth_helpers.cjs");
const { pushRepoMemoryChangesWithRetry } = require("./push_repo_memory.cjs");
const { resolveSchemaPath } = require("./ledger_mcp_server.cjs");
const { normalizeLedgerAppends } = require("./ledger_transactions.cjs");

const MAX_TRANSACTION_BYTES = 12 * 1024 * 1024;
const EMPTY_TREE_SHA = "4b825dc642cb6eb9a060e54bf8d69288fbee4904";

/**
 * Read the versioned transaction artifact used by trusted ledger persistence.
 * @param {string} [file]
 */
function readTransactions(file = process.env.GH_AW_LEDGER_TRANSACTIONS) {
  if (!file) return { version: 1, ledgers: {} };
  let artifact;
  try {
    const stat = fs.lstatSync(path.resolve(file));
    if (!stat.isFile() || stat.isSymbolicLink() || stat.size > MAX_TRANSACTION_BYTES) throw new TypeError("Invalid ledger transaction artifact");
    artifact = JSON.parse(fs.readFileSync(path.resolve(file), "utf8"));
  } catch (error) {
    if (error instanceof TypeError && error.message === "Invalid ledger transaction artifact") throw error;
    throw new Error("Failed to read validated ledger transaction artifact", { cause: error });
  }
  if (!artifact || artifact.version !== 1 || !artifact.ledgers || typeof artifact.ledgers !== "object" || Array.isArray(artifact.ledgers)) {
    throw new TypeError("Invalid validated ledger transaction artifact");
  }
  return artifact;
}

/**
 * Read ledger append requests from the downloaded safe-output NDJSON file.
 * @param {string} [file]
 */
function readLedgerAppendRequests(file = process.env.GH_AW_LEDGER_TRANSACTIONS) {
  if (!file || !fs.existsSync(file)) return [];
  const stat = fs.lstatSync(file);
  if (!stat.isFile() || stat.isSymbolicLink() || stat.size > MAX_TRANSACTION_BYTES) throw new TypeError("Invalid ledger append artifact");
  const requests = [];
  let content;
  try {
    content = fs.readFileSync(file, "utf8");
  } catch (error) {
    throw new Error("Failed to read ledger append artifact", { cause: error });
  }
  for (const line of content.split(/\r?\n/)) {
    if (!line.trim()) continue;
    let entry;
    try {
      entry = JSON.parse(line);
    } catch (error) {
      throw new TypeError("Invalid safe-output ledger append entry", { cause: error });
    }
    if (entry?.type === "ledger_append") {
      requests.push({ ledger: entry.ledger, temp_id: entry.temp_id, record: entry.record });
    }
  }
  return requests;
}

function loadLedgerConfigs() {
  const encoded = process.env.GH_AW_LEDGER_CONFIG_B64;
  if (!encoded) throw new TypeError("Missing ledger configuration");
  let parsed;
  try {
    parsed = JSON.parse(Buffer.from(encoded, "base64").toString("utf8"));
  } catch (error) {
    throw new TypeError("Invalid ledger configuration", { cause: error });
  }
  if (!Array.isArray(parsed) || parsed.length === 0) throw new TypeError("Invalid ledger configuration");
  const configs = new Map();
  for (const config of parsed) {
    if (!config || typeof config.name !== "string" || !/^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$/.test(config.name) || configs.has(config.name)) {
      throw new TypeError("Invalid ledger configuration");
    }
    if (typeof config.schemaPath === "string" && config.schemaPath) {
      const schemaPath = resolveSchemaPath(config.schemaPath, process.env.GITHUB_WORKSPACE || process.cwd());
      if (!schemaPath) throw new TypeError("Invalid ledger schema path");
      let schemaStat;
      let schemaContents;
      try {
        schemaStat = fs.statSync(schemaPath);
        schemaContents = fs.readFileSync(schemaPath, "utf8");
      } catch (error) {
        throw new Error("Failed to read ledger schema", { cause: error });
      }
      if (schemaStat.size > 1024 * 1024) throw new TypeError("Ledger schema exceeds maximum size");
      try {
        config.schema = JSON.parse(schemaContents);
      } catch (error) {
        throw new TypeError("Ledger schema must be valid JSON", { cause: error });
      }
    }
    configs.set(config.name, config);
  }
  return configs;
}

function ensureDirectory(directory) {
  const resolved = path.resolve(directory);
  const stat = fs.lstatSync(path.dirname(resolved));
  if (!stat.isDirectory() || stat.isSymbolicLink()) throw new TypeError("Invalid ledger transaction directory");
  try {
    fs.mkdirSync(resolved);
  } catch (error) {
    if (!error || typeof error !== "object" || Reflect.get(error, "code") !== "EEXIST") throw error;
  }
  const createdStat = fs.lstatSync(resolved);
  if (!createdStat.isDirectory() || createdStat.isSymbolicLink()) throw new TypeError("Invalid ledger transaction directory");
}

function getServerHost() {
  try {
    return new URL(process.env.GITHUB_SERVER_URL || "https://github.com").host;
  } catch (error) {
    throw new TypeError("Invalid GitHub server URL", { cause: error });
  }
}

function readRemoteBranch(workspaceDir, branchName, repoUrl, ghToken) {
  execGitSync(["fetch", repoUrl, `refs/heads/${branchName}:${branchName}`], {
    cwd: workspaceDir,
    stdio: "pipe",
    suppressLogs: true,
    env: { ...process.env, ...getGitAuthEnv(ghToken) },
  });
  execGitSync(["checkout", branchName], { cwd: workspaceDir, stdio: "pipe" });
  return execGitSync(["rev-parse", "HEAD"], { cwd: workspaceDir, stdio: "pipe" }).trim();
}

async function checkoutOrCreateLedgerBranch({ workspaceDir, branchName, repoUrl, targetOwner, targetRepoName, githubClient, ghToken }) {
  try {
    return readRemoteBranch(workspaceDir, branchName, repoUrl, ghToken);
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    if (!/couldn't find remote ref|remote branch .* not found/i.test(message)) throw error;
  }

  const seed = await githubClient.rest.git.createCommit({
    owner: targetOwner,
    repo: targetRepoName,
    message: `Initialize ${branchName}`,
    tree: EMPTY_TREE_SHA,
    parents: [],
  });
  try {
    await githubClient.rest.git.createRef({ owner: targetOwner, repo: targetRepoName, ref: `refs/heads/${branchName}`, sha: seed.data.sha });
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    if (!/422|Reference already exists/i.test(message)) throw error;
  }
  try {
    return readRemoteBranch(workspaceDir, branchName, repoUrl, ghToken);
  } catch (error) {
    throw new Error(`Failed to check out ledger branch ${branchName}`, { cause: error });
  }
}

async function persistLedgerAppends({
  name,
  ledger,
  config,
  transactionId,
  workspaceDir = process.env.GITHUB_WORKSPACE || process.cwd(),
  targetRepo = process.env.GITHUB_REPOSITORY || `${context.repo.owner}/${context.repo.repo}`,
  githubClient = github,
  ghToken = process.env.GH_TOKEN || process.env.GITHUB_TOKEN,
  serverHost = getServerHost(),
  execGitSyncFn = execGitSync,
  pushChangesFn = pushRepoMemoryChangesWithRetry,
  checkoutBranchFn = checkoutOrCreateLedgerBranch,
}) {
  if (!ghToken) throw new TypeError("Missing token for ledger persistence");
  if (!/^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$/.test(name) || !/^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$/.test(transactionId)) {
    throw new TypeError("Invalid ledger transaction identity");
  }
  const branchName = config.branchName || `ledgers/${name}`;
  if (branchName !== `ledgers/${name}`) throw new TypeError("Invalid ledger branch configuration");
  const [targetOwner, targetRepoName] = targetRepo.split("/");
  if (!targetOwner || !targetRepoName || targetRepo.split("/").length !== 2) throw new TypeError("Invalid target repository for ledger persistence");
  const repoUrl = `https://${serverHost}/${targetRepo}.git`;
  const originalBranch = execGitSyncFn(["branch", "--show-current"], { cwd: workspaceDir, stdio: "pipe" }).trim() || execGitSyncFn(["rev-parse", "HEAD"], { cwd: workspaceDir, stdio: "pipe" }).trim();
  let baseRef = "";
  try {
    baseRef = await checkoutBranchFn({ workspaceDir, branchName, repoUrl, targetOwner, targetRepoName, githubClient, ghToken });
    const relativeFile = `ledger/transactions/${transactionId}.jsonl`;
    const segments = relativeFile.split("/");
    let current = workspaceDir;
    for (const segment of segments.slice(0, -1)) {
      current = path.join(current, segment);
      ensureDirectory(current);
    }
    const filePath = path.join(workspaceDir, relativeFile);
    const content = ledger.appends.map(append => `${JSON.stringify({ transaction_id: append.transaction_id, record: append.record })}\n`).join("");
    const bytes = Buffer.byteLength(content, "utf8");
    if (bytes > (config.max_segment_kb || 100) * 1024) throw new RangeError("Ledger transaction exceeds configured segment-size limit");
    if (fs.existsSync(filePath)) {
      let existingContent;
      try {
        const stat = fs.lstatSync(filePath);
        if (!stat.isFile() || stat.isSymbolicLink()) throw new TypeError("Ledger transaction ID collision");
        existingContent = fs.readFileSync(filePath, "utf8");
      } catch (error) {
        if (error instanceof TypeError && error.message === "Ledger transaction ID collision") throw error;
        throw new Error("Failed to read existing ledger transaction", { cause: error });
      }
      if (existingContent !== content) throw new TypeError("Ledger transaction ID collision");
      return { persisted: 0, already_present: ledger.appends.length, branch: branchName };
    }
    try {
      fs.writeFileSync(filePath, content, { flag: "wx", mode: 0o600 });
    } catch (error) {
      throw new Error("Failed to write ledger transaction", { cause: error });
    }
    execGitSyncFn(["config", "user.name", "github-actions[bot]"], { cwd: workspaceDir, stdio: "pipe" });
    execGitSyncFn(["config", "user.email", "41898282+github-actions[bot]@users.noreply.github.com"], { cwd: workspaceDir, stdio: "pipe" });
    execGitSyncFn(["add", "--", relativeFile], { cwd: workspaceDir, stdio: "pipe" });
    execGitSyncFn(["commit", "-m", `Append ${ledger.appends.length} records to ${name} ledger`], { cwd: workspaceDir, stdio: "pipe" });
    await pushChangesFn({
      githubClient,
      targetOwner,
      targetRepoName,
      targetRepo,
      branchName,
      baseRef,
      workspaceDir,
      ghToken,
      serverHost,
    });
    return { persisted: ledger.appends.length, already_present: 0, branch: branchName };
  } finally {
    execGitSyncFn(["checkout", originalBranch], { cwd: workspaceDir, stdio: "pipe" });
  }
}

async function main() {
  const requests = readLedgerAppendRequests();
  const configs = loadLedgerConfigs();
  const normalized = normalizeLedgerAppends(requests, {
    transactionId: process.env.GH_AW_LEDGER_TRANSACTION_ID || `${context.runId}-${context.runAttempt}`,
    ledgerConfigs: configs,
  });
  const grouped = new Map();
  for (const append of normalized.appends) {
    if (!grouped.has(append.ledger)) grouped.set(append.ledger, []);
    grouped.get(append.ledger).push(append);
  }
  const result = { version: 1, ledgers: {} };
  for (const [name, appends] of grouped) {
    const ledger = { appends };
    const persisted = await persistLedgerAppends({
      name,
      ledger,
      config: configs.get(name),
      transactionId: normalized.transaction_id,
    });
    result.ledgers[name] = {
      requested: appends.length,
      validated: appends.length,
      persisted: persisted.persisted,
      already_present: persisted.already_present,
      reconciled: 0,
      rejected: 0,
      branch: persisted.branch,
    };
  }
  if (process.env.GITHUB_OUTPUT) {
    try {
      fs.appendFileSync(process.env.GITHUB_OUTPUT, `ledger_result=${JSON.stringify(result)}\n`);
    } catch (error) {
      throw new Error("Failed to write ledger persistence result", { cause: error });
    }
  }
  return result;
}

if (require.main === module) {
  main().catch(error => {
    core.setFailed(error instanceof TypeError || error instanceof RangeError ? error.message : "Ledger persistence failed");
  });
}

module.exports = { loadLedgerConfigs, main, persistLedgerAppends, readLedgerAppendRequests, readTransactions };
