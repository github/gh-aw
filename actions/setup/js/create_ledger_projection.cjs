// @ts-check
"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { Ledger, configuredLedgerLimits } = require("./ledger_store.cjs");
const { execGitSync, getGitAuthEnv } = require("./git_helpers.cjs");
const { readLedgerConfig } = require("./push_ledger_changes.cjs");
const { validateValueAgainstSchema } = require("./mcp_scripts_validation.cjs");
const { materializeReplay } = require("./ledger_replay.cjs");
const { CLAIM_STATE_COLUMNS, CLAIM_STATE_VIEW, replayBuiltin } = require("./ledger_builtin.cjs");

const PROJECTION_ROOT = "/tmp/gh-aw/ledgers";
const MAX_PROJECTION_BYTES = 100 * 1024 * 1024;
const SHARD_PATH = /^ledger\/shards\/[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\.jsonl$/;
const COVERAGE_PATH = /^ledger\/coverage\/[0-9a-f]{64}\.jsonl$/;

function formatReplayPrompt(lines) {
  const retained = [];
  let bytes = 0;
  for (const line of lines) {
    const size = Buffer.byteLength(`${line}\n`);
    if (bytes + size > 60 * 1024) {
      retained.push("Additional replay tables or ledgers are not listed here. Query each ledger's replay_metadata for table names and columns; if absent, only generic records are available.");
      break;
    }
    retained.push(line);
    bytes += size;
  }
  return `${retained.join("\n")}\n`;
}

function formatReplayTable(name, table) {
  const columns = Object.entries(table.columns).map(([column, type]) => `${column}: ${type}`);
  return `- ${name}(${columns.join(", ")})`;
}

function hasStatus(error, status) {
  return error && typeof error === "object" && Reflect.get(error, "status") === status;
}

async function fetchLedgerBranch({ githubClient, owner, repo, branchName, refName, workspaceDir, serverHost, token }) {
  try {
    await githubClient.rest.git.getRef({ owner, repo, ref: `heads/${branchName}` });
  } catch (error) {
    if (hasStatus(error, 404)) return false;
    throw new Error(`Failed to resolve ledger branch ${branchName}`, { cause: error });
  }

  const remote = `https://${serverHost}/${owner}/${repo}.git`;
  execGitSync(["fetch", "--no-tags", remote, `refs/heads/${branchName}:${refName}`], {
    cwd: workspaceDir,
    env: getGitAuthEnv(token),
    stdio: "pipe",
  });
  return true;
}

function materializeLedger({ refName, workspaceDir, sourceDir, config }) {
  const entries = execGitSync(["ls-tree", "-r", "-z", "--full-tree", refName, "--", "ledger/shards", "ledger/coverage"], {
    cwd: workspaceDir,
    stdio: "pipe",
  });
  fs.mkdirSync(sourceDir, { recursive: true });
  let totalBytes = 0;
  let fileCount = 0;
  for (const entry of entries.split("\0").filter(Boolean)) {
    const [metadata, relativePath] = entry.split("\t");
    const [mode, type, objectId] = metadata.split(" ");
    if (!SHARD_PATH.test(relativePath) && !COVERAGE_PATH.test(relativePath)) continue;
    if (type !== "blob" || (mode !== "100644" && mode !== "100755") || !/^[0-9a-f]{40,64}$/.test(objectId)) {
      throw new TypeError("Ledger branch contains an invalid canonical file");
    }
    const bytes = execGitSync(["cat-file", "blob", objectId], { cwd: workspaceDir, stdio: "pipe" });
    const size = Buffer.byteLength(bytes);
    if (size > config.max_segment_kb * 1024 || totalBytes + size > MAX_PROJECTION_BYTES || ++fileCount > 1024) {
      throw new RangeError("Ledger branch exceeds projection limits");
    }
    totalBytes += size;
    const destination = path.join(sourceDir, relativePath);
    fs.mkdirSync(path.dirname(destination), { recursive: true });
    fs.writeFileSync(destination, bytes, { mode: 0o600 });
  }
}

/** @param {{sourceDir: string, databasePath: string, config: any}} options */
function createProjection({ sourceDir, databasePath, config }) {
  if (Object.hasOwn(config, "replay")) throw new TypeError("Custom ledger replay is no longer supported; use a built-in ledger type");
  const limits = configuredLedgerLimits(config);
  const ledger = new Ledger({
    memoryDir: sourceDir,
    maxRecordBytes: limits.maxRecordBytes,
    maxSegmentBytes: config.max_segment_kb * 1024,
    maxPatchBytes: limits.maxPatchBytes,
  });
  try {
    const state = ledger.reconstruct();
    if (state.diagnostics.length) throw new TypeError("Ledger branch contains invalid canonical records");
    if (config.schema && !config.type) {
      for (const record of state.records) {
        const payload = { ...record.payload };
        delete payload.id;
        if (validateValueAgainstSchema(payload, config.schema)) {
          throw new TypeError("Ledger branch contains a record that violates its configured schema");
        }
      }
    }
    const database = ledger.project(state);
    if (!database) throw new Error("Node.js SQLite support is required to create the ledger projection");
    /** @type {Record<string, { columns: Record<string, string> }> | null} */
    let tables = null;
    if (config.type) {
      const output = replayBuiltin(config, state.records);
      const maxRows = config.type === "claims" ? Object.values(output.tables).reduce((count, table) => count + table.rows.length, 0) : state.records.length;
      materializeReplay(database, config.name, `builtin:${config.type}`, state.records, output, maxRows);
      tables = output.tables;
      if (config.type === "claims") {
        database.exec(CLAIM_STATE_VIEW);
        database
          .prepare("INSERT INTO replay_metadata SELECT ledger_name, projection_version, record_count, script_sha256, output_version, ?, ? FROM replay_metadata WHERE table_name = 'claims'")
          .run("claim_state", JSON.stringify(CLAIM_STATE_COLUMNS));
        tables = { ...output.tables, claim_state: { columns: CLAIM_STATE_COLUMNS } };
      }
    }
    fs.mkdirSync(path.dirname(databasePath), { recursive: true });
    fs.rmSync(databasePath, { force: true });
    database.exec(`VACUUM INTO '${databasePath.replaceAll("'", "''")}'`);
    fs.chmodSync(databasePath, 0o444);
    return tables;
  } finally {
    ledger.close();
  }
}

async function main(options = {}) {
  const config = options.ledgerConfigs || readLedgerConfig(options.config);
  const owner = options.owner || context.repo.owner;
  const repo = options.repo || context.repo.repo;
  const serverHost = options.serverHost || new URL(process.env.GITHUB_SERVER_URL || "https://github.com").host;
  const token = options.token || process.env.GH_TOKEN;
  const workspaceDir = options.workspaceDir || process.env.GITHUB_WORKSPACE || process.cwd();
  const sourceRoot = path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw", "ledger-source");
  fs.rmSync(sourceRoot, { recursive: true, force: true });
  fs.mkdirSync(PROJECTION_ROOT, { recursive: true });
  const replayGuidance = [];

  try {
    for (const ledger of config) {
      const branchName = `ledgers/${ledger.name}`;
      const refName = `refs/gh-aw/ledgers/${ledger.name}`;
      const sourceDir = path.join(sourceRoot, ledger.name);
      const projectionDir = path.join(PROJECTION_ROOT, ledger.name);
      const databasePath = path.join(projectionDir, "ledger.db");
      fs.mkdirSync(projectionDir, { recursive: true });
      const exists = await fetchLedgerBranch({
        githubClient: options.githubClient || github,
        owner,
        repo,
        branchName,
        refName,
        workspaceDir,
        serverHost,
        token,
      });
      if (exists) materializeLedger({ refName, workspaceDir, sourceDir, config: ledger });
      else fs.mkdirSync(sourceDir, { recursive: true });
      const tables = createProjection({ sourceDir, databasePath, config: ledger });
      if (ledger.type) {
        replayGuidance.push(`Ledger ${ledger.name} (${databasePath}):`);
        if (tables && Object.keys(tables).length) {
          replayGuidance.push(...Object.entries(tables).map(([name, table]) => formatReplayTable(name, table)));
          replayGuidance.push("Use these derived, read-only tables for current state; use generic records for immutable event history. Persist new events only through the configured ledger safe-output tools. Do not update replay tables.");
        } else if (tables) {
          replayGuidance.push("Replay produced no tables; use generic records for immutable event history.");
        }
      }
      fs.chmodSync(projectionDir, 0o555);
      fs.rmSync(sourceDir, { recursive: true, force: true });
    }
    if (replayGuidance.length) fs.writeFileSync(path.join(PROJECTION_ROOT, "replay-prompt.txt"), formatReplayPrompt(replayGuidance), { mode: 0o444 });
    fs.chmodSync(PROJECTION_ROOT, 0o555);
  } finally {
    fs.rmSync(sourceRoot, { recursive: true, force: true });
  }
  return config.map(ledger => path.join(PROJECTION_ROOT, ledger.name, "ledger.db"));
}

if (require.main === module) {
  main().catch(error => {
    core.setFailed(error instanceof Error ? error.message : "Failed to create ledger projection");
  });
}

module.exports = { createProjection, fetchLedgerBranch, formatReplayPrompt, formatReplayTable, main, materializeLedger };
