// @ts-check
// @safe-outputs-exempt SEC-005: workflow callers invoke main() without target overrides, so writes use only the current GitHub Actions repository context.
"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { Ledger, canonicalJSON, configuredLedgerLimits } = require("./ledger_store.cjs");
const { execGitSync, getGitAuthEnv } = require("./git_helpers.cjs");
const { pushRepoMemoryChangesWithRetry, configureRepoMemoryMergePolicy } = require("./push_repo_memory.cjs");
const { finalId } = require("./ledger_transactions.cjs");
const { validateValueAgainstSchema } = require("./mcp_scripts_validation.cjs");
const { createReducer, validateOperation } = require("./ledger_builtin.cjs");

const MAX_TRANSACTION_BYTES = 12 * 1024 * 1024;
const LEDGER_NAME = /^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$/;
const RESERVED_RECORD_KEYS = new Set(["hash", "parents", "payload_sha", "sha", "timestamp", "transaction_id", "version"]);
const MAX_RECORD_FIELDS = 128;
const MAX_RECORD_DEPTH = 32;
const MAX_RECORD_KEY_BYTES = 256;

/**
 * Consume only the versioned artifact emitted by safe-output validation.
 * @param {string|undefined} file
 */
function readTransactions(file = process.env.GH_AW_LEDGER_TRANSACTIONS) {
  if (!file) return { version: 1, ledgers: {} };
  let artifact;
  let fd;
  try {
    fd = fs.openSync(path.resolve(file), fs.constants.O_RDONLY | (fs.constants.O_NOFOLLOW || 0));
    const stat = fs.fstatSync(fd);
    if (!stat.isFile() || stat.size > MAX_TRANSACTION_BYTES) throw new TypeError("Invalid ledger transaction artifact");
    artifact = JSON.parse(fs.readFileSync(fd, "utf8"));
  } catch (error) {
    if (error instanceof TypeError && error.message === "Invalid ledger transaction artifact") throw error;
    throw new Error("Failed to read validated ledger transaction artifact", { cause: error });
  } finally {
    if (fd !== undefined) fs.closeSync(fd);
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
    if (ledger && Object.hasOwn(ledger, "replay")) throw new TypeError("Custom ledger replay is no longer supported; use a built-in ledger type");
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
  if (Object.keys(artifact).some(key => !["ledgers", "transaction_id", "version"].includes(key))) {
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
    if (!config || !ledger || !Array.isArray(ledger.appends) || Object.keys(ledger).some(key => key !== "appends")) throw new TypeError("Invalid ledger transaction list");
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
      if (config.type) {
        validateOperation(payload, config);
      } else if (config.schema) {
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
  if (total !== seenIndices.size || Array.from({ length: total }, (_, index) => !seenIndices.has(index)).some(Boolean)) {
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
    for (const key of keys) {
      if (Buffer.byteLength(key, "utf8") > MAX_RECORD_KEY_BYTES) throw new RangeError("Ledger record field name is too long");
      if (value[key] && typeof value[key] === "object") stack.push({ value: value[key], depth: depth + 1 });
    }
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

async function persistLedgerAppends({ appends, config, githubClient, owner, repo, token, serverHost, workspaceDir, checkoutLedgerBranchFn = checkoutLedgerBranch, pushChangesFn = pushRepoMemoryChangesWithRetry }) {
  const originalHead = execGitSync(["rev-parse", "HEAD"], { cwd: workspaceDir, stdio: "pipe" }).trim();
  const branchName = `ledgers/${config.name}`;
  let ledger;
  try {
    const baseRef = await checkoutLedgerBranchFn({ githubClient, owner, repo, branchName, workspaceDir, token, serverHost });
    configureRepoMemoryMergePolicy(workspaceDir);
    const limits = configuredLedgerLimits(config, appends.length);
    ledger = new Ledger({
      memoryDir: workspaceDir,
      maxSegmentBytes: config.max_segment_kb * 1024,
      maxRecordBytes: limits.maxRecordBytes,
      maxPatchBytes: limits.maxPatchBytes,
    });

    // Validate the entire state transition before the first append mutates this branch.
    const current = ledger.reconstruct();
    if (current.diagnostics.length) throw new TypeError("Invalid canonical ledger history");
    if (config.type) {
      const reducer = createReducer(config);
      for (const record of current.records) reducer.apply(record.payload, record);
      const existing = new Set(current.records.map(record => (config.type === "notes" ? record.id : record.payload?.id)));
      for (const append of appends)
        if (!existing.has(append.record.id)) {
          const record = config.type === "notes" ? Object.fromEntries(Object.entries(append.record).filter(([key]) => key !== "id")) : append.record;
          reducer.apply(record, config.type === "notes" ? { id: append.record.id } : undefined);
        }
    }
    let persisted = 0;
    let alreadyPresent = 0;
    const persistedIds = [];
    for (const append of appends) {
      const payload = config.type === "notes" ? Object.fromEntries(Object.entries(append.record).filter(([key]) => key !== "id")) : append.record;
      const prior = current.records.find(record => (config.type === "notes" ? record.id === append.record.id : record.payload?.id === append.record.id));
      if (prior && canonicalJSON(prior.payload) !== canonicalJSON(payload)) throw new TypeError("Conflicting ledger record ID");
      const exists = Boolean(prior);
      if (exists) {
        alreadyPresent++;
        continue;
      }
      ledger.append("ledger_append", payload, config.type === "notes" ? append.record.id : undefined);
      persisted++;
      persistedIds.push(append.record.id);
    }
    ledger.close();
    ledger = null;
    if (!persisted) return { persisted: 0, already_present: alreadyPresent, reconciled: alreadyPresent, persisted_ids: [] };

    execGitSync(["add", "--", "ledger/shards"], { cwd: workspaceDir, stdio: "pipe" });
    execGitSync(["commit", "-m", `Append ${persisted} ledger record(s) from workflow run ${process.env.GITHUB_RUN_ID || "unknown"}`], {
      cwd: workspaceDir,
      stdio: "pipe",
    });
    const pushed = await pushChangesFn({
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
      validateBeforePush: config.type
        ? () => {
            const state = ledgerForValidation(workspaceDir, config);
            const reducer = createReducer(config);
            if (config.type === "notes") {
              for (const record of state) if (record.payload.operation === "note") reducer.apply(record.payload, record);
              for (const record of state) if (record.payload.operation !== "note") reducer.apply(record.payload, record);
            } else {
              for (const record of state) reducer.apply(record.payload);
            }
            reducer.output();
          }
        : undefined,
    });
    if (!pushed) throw new Error(`Failed to persist records to ${branchName}`);
    return { persisted, already_present: alreadyPresent, reconciled: alreadyPresent, persisted_ids: persistedIds };
  } finally {
    if (ledger) ledger.close();
    execGitSync(["checkout", "--detach", originalHead], { cwd: workspaceDir, stdio: "pipe", suppressLogs: true });
    execGitSync(["clean", "-fd"], { cwd: workspaceDir, stdio: "pipe", suppressLogs: true });
  }

  function ledgerForValidation(workspaceDir, config) {
    const limits = configuredLedgerLimits(config);
    const ledger = new Ledger({
      memoryDir: workspaceDir,
      maxSegmentBytes: config.max_segment_kb * 1024,
      maxRecordBytes: limits.maxRecordBytes,
      maxPatchBytes: limits.maxPatchBytes,
    });
    try {
      const state = ledger.reconstruct();
      if (state.diagnostics.length) throw new TypeError("Invalid canonical ledger history");
      return state.records;
    } finally {
      ledger.close();
    }
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
    let persisted;
    try {
      persisted = appends.length
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
    } catch (error) {
      if (config.type === "notes" && error instanceof TypeError && error.message === "Vote references a missing note") {
        result.ledgers[config.name] = { requested: appends.length, persisted: 0, rejected: 1, invalid_vote_requests: 1 };
        const output = options.outputFile === undefined ? process.env.GITHUB_OUTPUT : options.outputFile;
        if (output) {
          try {
            fs.appendFileSync(output, `ledger_result=${JSON.stringify(result)}\n`);
          } catch {
            globalThis.core?.warning?.("Failed to write invalid vote audit output");
          }
        }
      }
      throw error;
    }
    const addedNotes = config.type === "notes" ? appends.filter(append => persisted.persisted_ids?.includes(append.record.id) || (persisted.persisted === appends.length && !persisted.persisted_ids)) : [];
    result.ledgers[config.name] = {
      requested: appends.length,
      validated: appends.length,
      persisted: persisted.persisted,
      already_present: persisted.already_present,
      reconciled: persisted.reconciled,
      rejected: 0,
      branch: `ledgers/${config.name}`,
      ...(config.type
        ? {
            type: config.type,
            ...(config.type === "notes"
              ? {
                  notes_added: addedNotes.filter(append => append.record.operation === "note").length,
                  note_votes_added: addedNotes.filter(append => append.record.operation === "vote").length,
                  upvotes: addedNotes.filter(append => append.record.operation === "vote" && append.record.vote === "up").length,
                  downvotes: addedNotes.filter(append => append.record.operation === "vote" && append.record.vote === "down").length,
                  invalid_vote_requests: 0,
                }
              : {}),
            transactions: appends.map(append => ({
              id: append.record.id,
              transaction_id: append.transaction_id,
              operation: append.record.operation,
              validated: true,
            })),
          }
        : {}),
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
