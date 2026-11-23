// @ts-check

const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { execFileSync } = require("node:child_process");
const { isDeepStrictEqual } = require("node:util");
const { ERR_API, ERR_PARSE, ERR_SYSTEM, ERR_VALIDATION } = require("./error_codes.cjs");
const { readJsonlFileEvents } = require("./jsonl_helpers.cjs");

const MAX_LEDGER_BYTES = 16 * 1024 * 1024;
const MAX_NEW_BYTES = 768 * 1024;
const MAX_DISCOVERY_BATCHES = 5;

class LifecycleAPIError extends Error {}

/** @param {string[]} args @param {string | undefined} input */
function runLogs(args, input) {
  let raw;
  try {
    raw = execFileSync(path.resolve(__dirname, "../../../gh-aw"), ["logs", ...args], {
      input,
      encoding: "utf8",
      stdio: ["pipe", "pipe", "inherit"],
      timeout: 360_000,
      maxBuffer: 64 * 1024 * 1024,
    });
  } catch (error) {
    throw new LifecycleAPIError(`${ERR_API}: Lifecycle collection failed`, { cause: error });
  }
  try {
    return JSON.parse(raw);
  } catch (error) {
    throw new Error(`${ERR_PARSE}: Invalid lifecycle collection JSON`, { cause: error });
  }
}

/** @param {string} filename @param {string} content */
function writeAtomic(filename, content) {
  try {
    fs.mkdirSync(path.dirname(filename), { recursive: true });
    fs.writeFileSync(`${filename}.tmp`, content, { mode: 0o600 });
    fs.renameSync(`${filename}.tmp`, filename);
  } catch (error) {
    throw new Error(`${ERR_SYSTEM}: Failed to persist lifecycle state: ${filename}`, { cause: error });
  }
}

/** @param {string} filename */
function readLedger(filename) {
  /** @type {Map<string, {line: string, record: Record<string, any>}>} */
  const rows = new Map();
  if (!fs.existsSync(filename)) return rows;
  const stat = fs.lstatSync(filename);
  if (!stat.isFile() || stat.size > MAX_LEDGER_BYTES) throw new Error(`${ERR_VALIDATION}: Invalid retained lifecycle file`);
  for (const event of readJsonlFileEvents(filename, { fatalUTF8: true, jsonWhitespaceOnly: true })) {
    if (event.type !== "json") {
      if (event.reason === "blank") continue;
      throw new Error(`${ERR_PARSE}: Invalid retained lifecycle JSONL at line ${event.lineNumber}`, { cause: event.error });
    }
    const row = event.value;
    if (![7, 8].includes(row?.schema_version) || row.kind !== "output_lifecycle" || typeof row.lifecycle?.id !== "string" || rows.has(row.lifecycle.id)) {
      throw new Error(`${ERR_VALIDATION}: Invalid or duplicate retained lifecycle record`);
    }
    rows.set(row.lifecycle.id, { line: event.rawLine.trim(), record: row.lifecycle });
  }
  return rows;
}

/** @param {unknown} value */
function lifecycleTime(value) {
  if (typeof value !== "string") throw new Error(`${ERR_VALIDATION}: Invalid lifecycle collection timestamp`);
  const parsed = Date.parse(value);
  if (!Number.isFinite(parsed)) throw new Error(`${ERR_VALIDATION}: Invalid lifecycle collection timestamp`);
  return parsed;
}

/** @param {Map<string, {line: string, record: Record<string, any>}>} rows */
function serializeLedger(rows) {
  return [...rows.entries()]
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([, row]) => row.line + "\n")
    .join("");
}

/** @param {string} filename @param {string} repository */
function readState(filename, repository) {
  if (!fs.existsSync(filename)) return { schema_version: 1, repository, completed_until: null, window: null, unavailable_runs: [] };
  const stat = fs.lstatSync(filename);
  if (!stat.isFile() || stat.size > 256 * 1024) throw new Error(`${ERR_VALIDATION}: Invalid lifecycle collection checkpoint`);
  let state;
  try {
    state = JSON.parse(fs.readFileSync(filename, "utf8"));
  } catch (error) {
    throw new Error(`${ERR_PARSE}: Failed to read lifecycle checkpoint: ${filename}`, { cause: error });
  }
  const validDate = value => typeof value === "string" && Number.isFinite(Date.parse(value));
  if (
    state.schema_version !== 1 ||
    state.repository !== repository ||
    (state.completed_until !== null && !validDate(state.completed_until)) ||
    (state.window !== null &&
      (!state.window ||
        !validDate(state.window.start_date) ||
        !validDate(state.window.end_date) ||
        lifecycleTime(state.window.start_date) > lifecycleTime(state.window.end_date) ||
        (state.window.before_run_id !== null && (!Number.isSafeInteger(state.window.before_run_id) || state.window.before_run_id <= 0)) ||
        (state.window.before_date !== null && (!validDate(state.window.before_date) || lifecycleTime(state.window.before_date) > lifecycleTime(state.window.end_date))))) ||
    !Array.isArray(state.unavailable_runs) ||
    state.unavailable_runs.length > 20000 ||
    state.unavailable_runs.some(id => !Number.isSafeInteger(id) || id <= 0)
  )
    throw new Error(`${ERR_VALIDATION}: Invalid lifecycle collection checkpoint`);
  return state;
}

/** @returns {Record<string, number>} */
function emptyCounts() {
  return { delivered: 0, accepted_human: 0, accepted_automation: 0, accepted_delivery_fallback: 0, rejected: 0, pending: 0, lifecycle: 0, not_applicable: 0, unknown: 0, survival_observed: 0, survival_reversed: 0, survival_unknown: 0 };
}

/** @param {Record<string, number>} counts @param {Record<string, any>} record */
function countRecord(counts, record) {
  counts.delivered++;
  const adoption = record.adoption;
  if (adoption.coverage !== "available") counts.unknown++;
  else if (adoption.state === "accepted") {
    if (adoption.acceptance_kind === "explicit_human" && adoption.human_verified) counts.accepted_human++;
    else if (adoption.acceptance_kind === "automation" && !adoption.human_verified) counts.accepted_automation++;
    else if (adoption.acceptance_kind === "fallback_delivery" && !adoption.human_verified) counts.accepted_delivery_fallback++;
    else throw new Error(`${ERR_VALIDATION}: Invalid retained adoption acceptance`);
  } else if (["rejected", "pending", "lifecycle", "not_applicable", "unknown"].includes(adoption.state)) counts[adoption.state]++;
  else throw new Error(`${ERR_VALIDATION}: Invalid retained adoption state`);
  if (record.survival.state === "observed") counts.survival_observed++;
  else if (record.survival.state === "reversed") counts.survival_reversed++;
  else counts.survival_unknown++;
}

/**
 * @param {{memoryDir: string, metricsDir: string, repository: string, now?: Date,
 *   collect?: typeof runLogs}} options
 */
function collectLifecycle({ memoryDir, metricsDir, repository, now = new Date(), collect = runLogs }) {
  if (!Number.isFinite(now.getTime())) throw new Error(`${ERR_VALIDATION}: Invalid lifecycle collection clock`);
  if (!/^(?:[a-zA-Z0-9.-]+\/)?[a-zA-Z0-9_.-]+\/[a-zA-Z0-9_.-]+$/.test(repository)) {
    throw new Error(`${ERR_VALIDATION}: Lifecycle collection requires [HOST/]OWNER/REPO`);
  }
  const stateFile = path.join(memoryDir, "collection.json");
  const ledgerFile = path.join(memoryDir, "lifecycle.jsonl");
  const state = readState(stateFile, repository);
  if (fs.existsSync(stateFile) && !fs.existsSync(ledgerFile)) {
    throw new Error(`${ERR_VALIDATION}: Lifecycle checkpoint has no retained ledger; restore lifecycle.jsonl before collecting`);
  }
  const initial = readLedger(ledgerFile);
  let temporary;
  try {
    temporary = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-lifecycle-"));
  } catch (error) {
    throw new Error(`${ERR_SYSTEM}: Failed to prepare lifecycle collection directory`, { cause: error });
  }
  const ledger = path.join(temporary, "lifecycle.jsonl");
  const errors = [];
  let discoveryDeferred = false;
  let addedBytes = 0;
  const common = ["--repo", repository, "--output", temporary, "--json"];
  const bounded = [...common, "--max-github-api-rate-limit", "-500"];
  try {
    writeAtomic(ledger, serializeLedger(initial));
    if (state.completed_until && lifecycleTime(state.completed_until) > now.getTime()) throw new Error(`${ERR_VALIDATION}: Lifecycle checkpoint is in the future`);
    if (!state.window) {
      const start = state.completed_until ? new Date(lifecycleTime(state.completed_until) - 60 * 60_000) : new Date(now.getTime() - 24 * 60 * 60_000);
      state.window = { start_date: start.toISOString(), end_date: now.toISOString(), before_run_id: null, before_date: null };
    }
    if (lifecycleTime(state.window.end_date) > now.getTime()) throw new Error(`${ERR_VALIDATION}: Lifecycle checkpoint is in the future`);
    const unavailable = new Set(state.unavailable_runs);

    /** @param {Record<string, any>} logs */
    function retainBatch(logs) {
      if (!Array.isArray(logs.runs) || (!Array.isArray(logs.delivery) && (logs.runs.length > 0 || logs.delivery !== undefined)) || (Array.isArray(logs.delivery) && logs.delivery.length !== logs.runs.length)) {
        throw new Error(`${ERR_VALIDATION}: Missing delivery collection evidence`);
      }
      const candidate = readLedger(ledger);
      const kept = new Map(initial);
      let complete = true;
      for (const [id, row] of candidate) {
        if (!initial.has(id)) {
          const bytes = Buffer.byteLength(row.line + "\n");
          if (bytes > MAX_NEW_BYTES) throw new Error(`${ERR_VALIDATION}: One delivery exceeds the per-collection persistence budget`);
          if (addedBytes + bytes > MAX_NEW_BYTES) {
            complete = false;
            continue;
          }
          addedBytes += bytes;
        }
        kept.set(id, row);
      }
      initial.clear();
      for (const [id, row] of kept) initial.set(id, row);
      writeAtomic(ledger, serializeLedger(initial));
      const reportedRuns = new Set(logs.runs.map(run => run.run_id));
      for (const report of logs.delivery || []) {
        if (!Number.isSafeInteger(report.run_id) || report.run_id <= 0 || !reportedRuns.delete(report.run_id)) throw new Error(`${ERR_VALIDATION}: Invalid delivery run identity`);
        if (report.coverage === "complete" && complete) unavailable.delete(report.run_id);
        else unavailable.add(report.run_id);
      }
      if (unavailable.size > 20000) throw new Error(`${ERR_VALIDATION}: Lifecycle retry backlog exceeds 20000 runs`);
      return complete;
    }

    if (unavailable.size > 0) {
      const retry = [...unavailable].slice(0, 20);
      try {
        const logs = collect([...common, "--stdin", "--delivery", "--artifacts", "delivery", "--timeout", "1"], retry.join("\n") + "\n");
        if (!retainBatch(logs)) discoveryDeferred = true;
      } catch (error) {
        if (!(error instanceof LifecycleAPIError)) throw error;
        errors.push(error.message);
      }
      // Rotate unresolved runs so an expired artifact cannot starve the backlog.
      for (const id of retry) if (unavailable.delete(id)) unavailable.add(id);
    }

    for (let batch = 0; batch < MAX_DISCOVERY_BATCHES && !discoveryDeferred; batch++) {
      const window = state.window;
      if (!window) break;
      const args = [...bounded, "--delivery", "--artifacts", "delivery", "--count", "200", "--timeout", "4", "--start-date", window.start_date, "--end-date", window.before_date || window.end_date];
      if (window.before_run_id) args.push("--before-run-id", String(window.before_run_id));
      let logs;
      try {
        logs = collect(args, undefined);
      } catch (error) {
        if (!(error instanceof LifecycleAPIError)) throw error;
        errors.push(error.message);
        break;
      }
      if (!retainBatch(logs)) {
        discoveryDeferred = true;
        break;
      }
      if (!logs.continuation) {
        state.completed_until = window.end_date;
        state.window = null;
        break;
      }
      const continuation = logs.continuation;
      if (!Number.isSafeInteger(continuation.before_run_id) || continuation.before_run_id < 0 || lifecycleTime(continuation.end_date) > lifecycleTime(window.before_date || window.end_date)) {
        throw new Error(`${ERR_VALIDATION}: Invalid lifecycle discovery continuation`);
      }
      if ((continuation.before_run_id || null) === window.before_run_id && continuation.end_date === (window.before_date || window.end_date)) {
        errors.push("Lifecycle discovery made no progress; the saved window will be retried.");
        break;
      }
      state.window = { ...window, before_run_id: continuation.before_run_id || null, before_date: continuation.end_date };
    }

    const refreshed = collect([...bounded, "--retained", "--adoption", "--count", "1000", "--timeout", "2"], undefined).retained_adoption;
    if (
      !refreshed ||
      !Array.isArray(refreshed.records) ||
      !Number.isSafeInteger(refreshed.refreshed) ||
      !Number.isSafeInteger(refreshed.deferred) ||
      refreshed.refreshed < 0 ||
      refreshed.deferred < 0 ||
      refreshed.refreshed + refreshed.deferred > refreshed.records.length ||
      !["complete", "partial"].includes(refreshed.coverage)
    ) {
      throw new Error(`${ERR_VALIDATION}: Invalid retained adoption refresh`);
    }
    const finalRows = readLedger(ledger);
    const reported = new Set();
    for (const record of refreshed.records) {
      if (reported.has(record.id) || !isDeepStrictEqual(finalRows.get(record.id)?.record, record)) {
        throw new Error(`${ERR_VALIDATION}: Lifecycle report does not match the retained ledger`);
      }
      reported.add(record.id);
    }
    if (reported.size !== finalRows.size) throw new Error(`${ERR_VALIDATION}: Lifecycle report omits retained deliveries`);
    const content = serializeLedger(finalRows);
    state.unavailable_runs = [...unavailable];
    state.observed_at = now.toISOString();
    const partial = state.window !== null || discoveryDeferred || unavailable.size > 0 || errors.length > 0 || refreshed.coverage !== "complete";
    const summary = {
      schema_version: 1,
      repository,
      observed_at: now.toISOString(),
      scope: "retained_deliveries",
      coverage: partial ? "partial" : "complete",
      discovery_complete: state.window === null,
      discovery_completed_until: state.completed_until,
      unavailable_runs: unavailable.size,
      refreshed: refreshed.refreshed,
      deferred: refreshed.deferred,
      errors,
      totals: emptyCounts(),
      /** @type {Record<string, Record<string, number>>} */
      workflows: Object.create(null),
    };
    for (const record of refreshed.records) {
      if (typeof record.workflow !== "string" || record.workflow.length === 0) throw new Error(`${ERR_VALIDATION}: Retained delivery has no source workflow`);
      countRecord(summary.totals, record);
      const counts = (summary.workflows[record.workflow] ??= emptyCounts());
      countRecord(counts, record);
    }
    const summaryJSON = JSON.stringify(summary) + "\n";
    if (Buffer.byteLength(summaryJSON) > 512 * 1024) throw new Error(`${ERR_VALIDATION}: Lifecycle summary exceeds the metrics file budget`);
    const stateJSON = JSON.stringify(state) + "\n";
    if (Buffer.byteLength(stateJSON) > 256 * 1024 || Buffer.byteLength(stateJSON) + Buffer.byteLength(content) > MAX_LEDGER_BYTES) {
      throw new Error(`${ERR_VALIDATION}: Lifecycle checkpoint exceeds its persistence budget`);
    }
    const growth = (filename, next) => {
      try {
        return Math.max(0, Buffer.byteLength(next) - (fs.existsSync(filename) ? fs.statSync(filename).size : 0));
      } catch (error) {
        throw new Error(`${ERR_SYSTEM}: Failed to check lifecycle persistence budget: ${filename}`, { cause: error });
      }
    };
    if (growth(ledgerFile, content) + growth(stateFile, stateJSON) > 900 * 1024) {
      throw new Error(`${ERR_VALIDATION}: Lifecycle persistence exceeds its bounded patch budget`);
    }
    writeAtomic(ledgerFile, content);
    writeAtomic(stateFile, stateJSON);
    writeAtomic(path.join(metricsDir, "lifecycle.json"), summaryJSON);
    for (const error of errors) console.error(`${ERR_API}: ${error}`);
    return summary;
  } finally {
    try {
      fs.rmSync(temporary, { recursive: true, force: true });
    } catch (error) {
      throw new Error(`${ERR_SYSTEM}: Failed to remove lifecycle collection directory: ${temporary}`, { cause: error });
    }
  }
}

if (require.main === module) {
  const repository = process.env.EXPR_GITHUB_REPOSITORY || "";
  const server = process.env.GITHUB_SERVER_URL || "https://github.com";
  let host;
  try {
    host = new URL(server).host;
  } catch (error) {
    throw new Error(`${ERR_VALIDATION}: Invalid lifecycle GitHub server URL`, { cause: error });
  }
  collectLifecycle({
    repository: host === "github.com" ? repository : `${host}/${repository}`,
    memoryDir: "/tmp/gh-aw/repo-memory/lifecycle",
    metricsDir: "/tmp/gh-aw/repo-memory/default/metrics",
  });
}

module.exports = { collectLifecycle, LifecycleAPIError };
