// @ts-check
"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { Ledger } = require("./ledger_store.cjs");
const { execGitSync, getGitAuthEnv } = require("./git_helpers.cjs");
const { pushRepoMemoryChangesWithRetry, configureRepoMemoryMergePolicy } = require("./push_repo_memory.cjs");
const { finalId } = require("./ledger_transactions.cjs");
const { validateValueAgainstSchema } = require("./mcp_scripts_validation.cjs");

const LEDGER_NAME = /^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$/;
const RESERVED_RECORD_KEYS = new Set(["hash", "parents", "sha", "timestamp", "transaction_id", "version"]);
const MAX_RECORD_FIELDS = 128;
const MAX_RECORD_DEPTH = 32;

/**
 * Consume only the versioned artifact emitted by safe-output validation.
 * @param {string|undefined} file
 */
function readTransactions(file = process.env.GH_AW_LEDGER_TRANSACTIONS) {
  if (!file) return { version: 1, ledgers: {} };
  let artifact;
  try {
    artifact = JSON.parse(fs.readFileSync(path.resolve(file), "utf8"));
  } catch (error) {
    throw new Error("Failed to read validated ledger transaction artifact", { cause: error });
  }
  if (!artifact || artifact.version !== 1 || !artifact.ledgers || typeof artifact.ledgers !== "object" || Array.isArray(artifact.ledgers)) {
    throw new TypeError("Invalid validated ledger transaction artifact");
  }
  return artifact;
}

function readLedgerConfig(value = process.env.GH_AW_LEDGER_CONFIG_BASE64) {
  if (!value) return [];
  let ledgers;
  try {
    ledgers = JSON.parse(Buffer.from(value, "base64").toString("utf8"));
  } catch (error) {
    throw new TypeError("Invalid trusted ledger configuration", { cause: error });
  }
  if (!Array.isArray(ledgers)) throw new TypeError("Invalid trusted ledger configuration");
  const names = new Set();
  for (const ledger of ledgers) {
    if (
      !ledger ||
      typeof ledger.name !== "string" ||
      !LEDGER_NAME.test(ledger.name) ||
      names.has(ledger.name) ||
      !Number.isSafeInteger(ledger.max_record_kb) ||
      ledger.max_record_kb < 1 ||
      ledger.max_record_kb > 32 ||
      !Number.isSafeInteger(ledger.max_segment_kb) ||
      ledger.max_segment_kb < ledger.max_record_kb ||
      ledger.max_segment_kb > 10240 ||
      !Number.isSafeInteger(ledger.max_patch_kb) ||
      ledger.max_patch_kb < 1 ||
      ledger.max_patch_kb > 10240
    ) {
      throw new TypeError("Invalid trusted ledger definition");
    }
    names.add(ledger.name);
  }
  return ledgers;
}

function validateTransactions(artifact, ledgerConfigs) {
  if (!artifact || artifact.version !== 1 || !artifact.ledgers || typeof artifact.ledgers !== "object" || Array.isArray(artifact.ledgers)) {
    throw new TypeError("Invalid validated ledger transaction artifact");
  }
  const configs = new Map(ledgerConfigs.map(config => [config.name, config]));
  if (Object.keys(artifact.ledgers).length && (typeof artifact.transaction_id !== "string" || !artifact.transaction_id)) {
    throw new TypeError("Invalid ledger transaction ID");
  }
  const seenIndices = new Set();
  const seenIds = new Set();
  const patchBytes = new Map();
  let total = 0;
  for (const [name, ledger] of Object.entries(artifact.ledgers)) {
    const config = configs.get(name);
    if (!config || !ledger || !Array.isArray(ledger.appends)) throw new TypeError("Invalid ledger transaction list");
    for (const append of ledger.appends) {
      total++;
      if (
        total > 100 ||
        !append ||
        append.ledger !== name ||
        append.transaction_id !== artifact.transaction_id ||
        !Number.isSafeInteger(append.index) ||
        append.index < 0 ||
        append.index >= 100 ||
        seenIndices.has(append.index) ||
        !append.record ||
        typeof append.record !== "object" ||
        Array.isArray(append.record) ||
        Object.keys(append).sort().join(",") !== "index,ledger,record,transaction_id"
      ) {
        throw new TypeError("Invalid validated ledger append");
      }
      seenIndices.add(append.index);
      const record = append.record;
      if (typeof record.id !== "string" || record.id !== finalId(artifact.transaction_id, append.index) || seenIds.has(record.id)) {
        throw new TypeError("Invalid deterministic ledger record ID");
      }
      seenIds.add(record.id);
      for (const key of Object.keys(record)) {
        if (RESERVED_RECORD_KEYS.has(key)) throw new TypeError(`Ledger record contains reserved field "${key}"`);
      }
      const payload = { ...record };
      delete payload.id;
      validateRecordShape(payload);
      if (config.schema) {
        const schemaError = validateValueAgainstSchema(payload, config.schema);
        if (schemaError) throw new TypeError("Ledger record does not match configured schema");
      }
      const recordBytes = Buffer.byteLength(JSON.stringify(record), "utf8");
      if (recordBytes > config.max_record_kb * 1024) throw new RangeError("Ledger record exceeds max-record-kb");
      const currentPatchBytes = (patchBytes.get(name) || 0) + recordBytes;
      if (currentPatchBytes > config.max_patch_kb * 1024) throw new RangeError("Ledger append batch exceeds max-patch-kb");
      patchBytes.set(name, currentPatchBytes);
    }
  }
  if (total !== seenIndices.size || [...seenIndices].some((index, i) => index !== i)) {
    throw new TypeError("Ledger transaction indices are incomplete");
  }
  return configs;
}

function validateRecordShape(record) {
  const stack = [{ value: record, depth: 0 }];
  while (stack.length) {
    const entry = stack.pop();
    if (!entry) continue;
    const { value, depth } = entry;
    if (depth > MAX_RECORD_DEPTH) throw new RangeError("Ledger record exceeds maximum nesting depth");
    if (!value || typeof value !== "object") continue;
    const keys = Object.keys(value);
    if (keys.length > MAX_RECORD_FIELDS) throw new RangeError("Ledger record exceeds maximum field count");
    for (const key of keys) if (value[key] && typeof value[key] === "object") stack.push({ value: value[key], depth: depth + 1 });
  }
}

async function checkoutLedgerBranch({ githubClient, owner, repo, branchName, workspaceDir, token, serverHost }) {
  const repository = `${owner}/${repo}`;
  const repoUrl = `https://${serverHost}/${repository}.git`;
  const authEnv = getGitAuthEnv(token);
  try {
    execGitSync(["fetch", repoUrl, `${branchName}:${branchName}`], { cwd: workspaceDir, env: authEnv, stdio: "pipe", suppressLogs: true });
    execGitSync(["checkout", branchName], { cwd: workspaceDir, stdio: "pipe" });
    return execGitSync(["rev-parse", "HEAD"], { cwd: workspaceDir, stdio: "pipe" }).trim();
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    if (!/couldn't find remote ref|remote branch .* not found/i.test(message)) throw error;
  }

  const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904";
  const { data: seedCommit } = await githubClient.rest.git.createCommit({
    owner,
    repo,
    message: `Initialize ${branchName}`,
    tree: emptyTree,
    parents: [],
  });
  let baseRef = seedCommit.sha;
  try {
    await githubClient.rest.git.createRef({ owner, repo, ref: `refs/heads/${branchName}`, sha: seedCommit.sha });
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    if (!/422|Reference already exists/i.test(message)) throw error;
    baseRef = "";
  }
  execGitSync(["fetch", repoUrl, `${branchName}:${branchName}`], { cwd: workspaceDir, env: authEnv, stdio: "pipe", suppressLogs: true });
  execGitSync(["checkout", branchName], { cwd: workspaceDir, stdio: "pipe" });
  return baseRef || execGitSync(["rev-parse", "HEAD"], { cwd: workspaceDir, stdio: "pipe" }).trim();
}

async function persistLedgerAppends({ appends, config, githubClient, owner, repo, token, serverHost, workspaceDir }) {
  const originalHead = execGitSync(["rev-parse", "HEAD"], { cwd: workspaceDir, stdio: "pipe" }).trim();
  const branchName = `ledgers/${config.name}`;
  let ledger;
  try {
    const baseRef = await checkoutLedgerBranch({ githubClient, owner, repo, branchName, workspaceDir, token, serverHost });
    configureRepoMemoryMergePolicy(workspaceDir);
    ledger = new Ledger({
      memoryDir: workspaceDir,
      maxSegmentBytes: config.max_segment_kb * 1024,
      maxRecordBytes: Math.min(32 * 1024, config.max_record_kb * 1024 + 1024),
      maxPatchBytes: config.max_patch_kb * 1024,
    });

    let persisted = 0;
    let alreadyPresent = 0;
    for (const append of appends) {
      const exists = ledger.reconstruct().records.some(record => record.payload?.id === append.record.id);
      if (exists) {
        alreadyPresent++;
        continue;
      }
      ledger.append("ledger_append", append.record);
      persisted++;
    }
    ledger.close();
    ledger = null;
    if (!persisted) return { persisted: 0, already_present: alreadyPresent, reconciled: alreadyPresent };

    execGitSync(["add", "--", "ledger/shards"], { cwd: workspaceDir, stdio: "pipe" });
    execGitSync(["commit", "-m", `Append ${persisted} ledger record(s) from workflow run ${process.env.GITHUB_RUN_ID || "unknown"}`], {
      cwd: workspaceDir,
      stdio: "pipe",
    });
    const pushed = await pushRepoMemoryChangesWithRetry({
      githubClient,
      targetOwner: owner,
      targetRepoName: repo,
      targetRepo: `${owner}/${repo}`,
      branchName,
      baseRef,
      workspaceDir,
      ghToken: token,
      serverHost,
      originUrlForPush: repoUrl(serverHost, owner, repo),
    });
    if (!pushed) throw new Error(`Failed to persist records to ${branchName}`);
    return { persisted, already_present: alreadyPresent, reconciled: alreadyPresent };
  } finally {
    if (ledger) ledger.close();
    execGitSync(["checkout", "--detach", originalHead], { cwd: workspaceDir, stdio: "pipe", suppressLogs: true });
    execGitSync(["clean", "-fd"], { cwd: workspaceDir, stdio: "pipe", suppressLogs: true });
  }
}

function repoUrl(host, owner, repo) {
  return `https://${host}/${owner}/${repo}.git`;
}

async function main(options = {}) {
  const artifact = options.artifact || readTransactions(options.file);
  const ledgerConfigs = options.ledgerConfigs || readLedgerConfig(options.config);
  validateTransactions(artifact, ledgerConfigs);
  const result = { version: 1, ledgers: {} };
  for (const config of ledgerConfigs) {
    const appends = artifact.ledgers[config.name]?.appends || [];
    const persisted = appends.length
      ? await (options.persistLedger || persistLedgerAppends)({
          appends,
          config,
          githubClient: options.githubClient || github,
          owner: options.owner || context.repo.owner,
          repo: options.repo || context.repo.repo,
          token: options.token || process.env.GH_TOKEN,
          serverHost: options.serverHost || new URL(process.env.GITHUB_SERVER_URL || "https://github.com").host,
          workspaceDir: options.workspaceDir || process.env.GITHUB_WORKSPACE || process.cwd(),
        })
      : { persisted: 0, already_present: 0, reconciled: 0 };
    result.ledgers[config.name] = {
      requested: appends.length,
      validated: appends.length,
      persisted: persisted.persisted,
      already_present: persisted.already_present,
      reconciled: persisted.reconciled,
      rejected: 0,
      branch: `ledgers/${config.name}`,
    };
  }
  if (options.outputFile === undefined ? process.env.GITHUB_OUTPUT : options.outputFile) {
    const output = options.outputFile === undefined ? process.env.GITHUB_OUTPUT : options.outputFile;
    try {
      fs.appendFileSync(output, `ledger_result=${JSON.stringify(result)}\n`);
    } catch (error) {
      throw new Error("Failed to write ledger persistence result", { cause: error });
    }
  }
  return result;
}

if (require.main === module) {
  main().catch(error => {
    core.setFailed(error instanceof Error ? error.message : "Ledger persistence failed");
  });
}

module.exports = { main, persistLedgerAppends, readLedgerConfig, readTransactions, validateTransactions };
