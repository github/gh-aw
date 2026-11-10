// @ts-check
// Test-only workflow memory helpers; the setup action excludes this test-suffixed file.
"use strict";

const fs = require("node:fs");
const path = require("node:path");
const crypto = require("node:crypto");

const dimensions = [
  ["tiny", "small", "medium", "large", "huge"],
  ["none", "shallow", "medium", "deep"],
  ["single", "few", "many", "batch"],
  ["micro", "small", "medium", "large", "xlarge"],
  ["clean", "ahead", "diverged"],
  ["single", "multi", "merge_msg"],
];
const configIds = dimensions.reduce((prefixes, values) => prefixes.flatMap(prefix => values.map(value => (prefix ? `${prefix}-${value}` : value))), [""]);
const outcomes = ["pass", "fail", "error", "rejected", "timeout"];
const datePattern = /^[0-9]{4}-[0-9]{2}-[0-9]{2}$/;
const hashPattern = /^[a-f0-9]{64}$/;

function object(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function exactKeys(value, keys) {
  return object(value) && Object.keys(value).length === keys.length && keys.every(key => Object.hasOwn(value, key));
}

function parseOptionalJSON(text) {
  try {
    return JSON.parse(text);
  } catch (error) {
    if (error instanceof SyntaxError) return null;
    throw error;
  }
}

function freshState() {
  return { schema_version: 1, last_run: "1970-01-01", tested_count: 0, total_cells: 3600, tested: {}, strategies: [], next_index: 0 };
}

function sanitizeState(state) {
  if (
    !exactKeys(state, ["schema_version", "last_run", "tested_count", "total_cells", "tested", "strategies", "next_index"]) ||
    state.schema_version !== 1 ||
    state.total_cells !== 3600 ||
    !Number.isInteger(state.next_index) ||
    state.next_index < 0 ||
    state.next_index > 3600 ||
    !Number.isSafeInteger(state.tested_count) ||
    state.tested_count < 0 ||
    typeof state.last_run !== "string" ||
    !datePattern.test(state.last_run) ||
    !Array.isArray(state.strategies) ||
    !object(state.tested) ||
    Object.keys(state.tested).length > 3600
  ) {
    throw new TypeError("Invalid required git simulator state");
  }
  const tested = {};
  for (const [id, result] of Object.entries(state.tested)) {
    if (
      !configIds.includes(id) ||
      !exactKeys(result, ["outcome", "timestamp", "issue_url"]) ||
      !outcomes.includes(result.outcome) ||
      typeof result.timestamp !== "string" ||
      !datePattern.test(result.timestamp) ||
      (result.issue_url !== null && (typeof result.issue_url !== "string" || !/^https:\/\/github\.com\/[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+\/issues\/[1-9][0-9]*$/.test(result.issue_url)))
    ) {
      throw new TypeError("Invalid required git simulator tested cell");
    }
    tested[id] = result;
  }
  return { ...state, tested, strategies: [] };
}

function validSignal(entry) {
  return (
    exactKeys(entry, ["config_id", "signal", "confidence", "evidence_paths", "evidence_hashes"]) &&
    configIds.includes(entry.config_id) &&
    outcomes.includes(entry.signal) &&
    entry.confidence === "corroborated" &&
    Array.isArray(entry.evidence_paths) &&
    entry.evidence_paths.length >= 1 &&
    entry.evidence_paths.length <= 3 &&
    Array.isArray(entry.evidence_hashes) &&
    entry.evidence_hashes.length === entry.evidence_paths.length &&
    new Set(entry.evidence_paths).size === entry.evidence_paths.length &&
    entry.evidence_paths.every(name => typeof name === "string" && /^(pkg\/workflow|actions\/setup\/js)\/[A-Za-z0-9_./-]+\.(go|cjs)$/.test(name) && !name.split("/").some(part => part === ".." || part === "." || part === "")) &&
    entry.evidence_hashes.every(hash => typeof hash === "string" && hashPattern.test(hash))
  );
}

function verifySignal(entry, workspace) {
  if (!validSignal(entry)) return false;
  return entry.evidence_paths.every((name, index) => {
    const file = path.join(workspace, name);
    if (!fs.existsSync(file)) return false;
    const resolved = fs.realpathSync(file);
    if (!resolved.startsWith(`${fs.realpathSync(workspace)}${path.sep}`) || !fs.statSync(resolved).isFile()) return false;
    const stat = fs.statSync(resolved);
    if (stat.size > 1024 * 1024) return false;
    return crypto.createHash("sha256").update(fs.readFileSync(resolved)).digest("hex") === entry.evidence_hashes[index];
  });
}

function acceptSignals(review, notes, workspace) {
  const candidates = [];
  if (object(review) && review.version === 1 && object(review.repository_files) && Array.isArray(review.signals) && review.signals.length <= 10) {
    for (const entry of review.signals) {
      if (verifySignal(entry, workspace) && entry.evidence_paths.every((name, index) => review.repository_files[name] === entry.evidence_hashes[index])) {
        candidates.push(entry);
      }
    }
  }
  const contested = new Set();
  for (const row of notes.slice(0, 20)) {
    if (typeof row.note !== "string" || Buffer.byteLength(row.note) > 1024) continue;
    const entry = parseOptionalJSON(row.note);
    if (!verifySignal(entry, workspace) || entry.config_id !== row.subject || !Array.isArray(row.citations) || row.citations.length !== entry.evidence_paths.length || !entry.evidence_paths.every(name => row.citations.includes(name)))
      continue;
    if (!Number.isSafeInteger(row.downvotes) || row.downvotes < 0) continue;
    if (row.downvotes > 0) contested.add(entry.config_id);
    else candidates.push(entry);
  }
  const byConfig = new Map();
  for (const entry of candidates) {
    const previous = byConfig.get(entry.config_id);
    if (previous && previous.signal !== entry.signal) contested.add(entry.config_id);
    byConfig.set(entry.config_id, { config_id: entry.config_id, signal: entry.signal, confidence: "corroborated" });
  }
  return [...byConfig.values()].filter(entry => !contested.has(entry.config_id)).sort((a, b) => configIds.indexOf(a.config_id) - configIds.indexOf(b.config_id));
}

function boundedOptionalFile(root, name, limit) {
  const file = path.join(root, name);
  const stat = fs.lstatSync(file, { throwIfNoEntry: false });
  if (!stat) return null;
  if (!stat.isFile() || stat.size > limit) return null;
  return fs.readFileSync(file, "utf8");
}

const producerFileLimit = 204800;
const historyLimit = 8 * producerFileLimit;

function assertPrivatePath(destination, memoryDir, workspace) {
  const resolved = path.resolve(destination);
  if ([workspace, memoryDir, "/tmp/gh-aw"].some(root => resolved === path.resolve(root) || resolved.startsWith(`${path.resolve(root)}${path.sep}`))) {
    throw new TypeError("Private simulator history must be outside agent mounts");
  }
}

function readProducerFile(file, required = false, limit = producerFileLimit) {
  const stat = fs.lstatSync(file, { throwIfNoEntry: false });
  if (!stat) {
    if (required) throw new TypeError("Required simulator producer file is missing");
    return null;
  }
  if (!stat.isFile() || stat.size > limit) throw new TypeError("Invalid or oversized simulator producer file");
  const bytes = fs.readFileSync(file);
  if (bytes.length > limit) throw new RangeError("Simulator producer file grew beyond its byte budget");
  return new TextDecoder("utf-8", { fatal: true }).decode(bytes);
}

function readRequiredProducerFile(file, limit = producerFileLimit) {
  const text = readProducerFile(file, true, limit);
  if (text === null) throw new TypeError("Required simulator producer file is missing");
  return text;
}

function normalizeRestoredState(original) {
  if (!object(original) || !object(original.tested) || !Array.isArray(original.strategies)) return original;
  const state = { ...original, tested: {} };
  const metadata = {};
  for (const key of ["halted", "note", "escalation_issue"]) {
    if (!Object.hasOwn(state, key)) continue;
    metadata[key] = state[key];
    delete state[key];
  }
  for (const [id, result] of Object.entries(original.tested)) {
    const configId = /^(0|[1-9][0-9]{0,3})$/.test(id) ? configIds[Number(id)] : id;
    if (!configIds.includes(configId) || Object.hasOwn(state.tested, configId)) throw new TypeError("Invalid required git simulator tested cell");
    state.tested[configId] = configId !== id && object(result) && result.issue_url === "filed" ? { ...result, issue_url: null } : result;
  }
  // Keep legacy narrative as producer history, never in the agent-visible state.
  if (Object.keys(metadata).length) state.strategies = [...original.strategies, { legacy_metadata: metadata }];
  return state;
}

function isolateMemory({ memoryDir, workspace, notes = [], outputPath, privateGitDir, privateHistoryPath }) {
  const statePath = path.join(memoryDir, "git-sim-state.json");
  let state = freshState();
  let originalStrategies = [];
  const stat = fs.lstatSync(statePath, { throwIfNoEntry: false });
  if (stat) {
    if (!stat.isFile() || stat.size > 204800) throw new TypeError("Invalid required git simulator state file");
    const original = normalizeRestoredState(JSON.parse(readRequiredProducerFile(statePath)));
    state = sanitizeState(original);
    originalStrategies = original.strategies;
  }
  const reviewText = boundedOptionalFile(memoryDir, "dreaming-review.json", 16384);
  const signals = acceptSignals(reviewText === null ? null : parseOptionalJSON(reviewText), notes, workspace);
  const preferred = new Set(signals.filter(entry => entry.signal === "fail" || entry.signal === "error").map(entry => entry.config_id));
  const regressionPriority = configIds.filter(id => state.tested[id] && ["fail", "error"].includes(state.tested[id].outcome));
  regressionPriority.sort((a, b) => Number(preferred.has(b)) - Number(preferred.has(a)) || configIds.indexOf(a) - configIds.indexOf(b));
  const stateText = `${JSON.stringify(state, null, 2)}\n`;
  if (Buffer.byteLength(stateText) > producerFileLimit) throw new RangeError("Isolated simulator state exceeds its 204800-byte file budget");
  if (!privateHistoryPath) throw new TypeError("Simulator preparation requires private producer history storage");
  assertPrivatePath(privateHistoryPath, memoryDir, workspace);
  const originalNotes = readProducerFile(path.join(memoryDir, "git-sim-strategies.md")) || "";
  const history = JSON.stringify({ version: 1, strategies: originalStrategies, strategy_notes: originalNotes });
  if (Buffer.byteLength(history) > historyLimit) throw new RangeError("Simulator producer history exceeds its private archive budget");
  fs.mkdirSync(path.dirname(privateHistoryPath), { recursive: true });
  fs.writeFileSync(privateHistoryPath, history, { flag: "wx", mode: 0o600 });
  const gitDir = path.join(memoryDir, ".git");
  if (fs.existsSync(gitDir)) {
    if (!privateGitDir || !fs.lstatSync(gitDir).isDirectory()) throw new TypeError("Simulator memory requires a private Git metadata directory");
    assertPrivatePath(privateGitDir, memoryDir, workspace);
    fs.mkdirSync(path.dirname(privateGitDir), { recursive: true });
    fs.cpSync(gitDir, privateGitDir, { recursive: true, errorOnExist: true, force: false });
    fs.rmSync(gitDir, { recursive: true });
    // The host-side memory publisher needs Git history, but the agent must not read old narrative blobs.
    fs.writeFileSync(gitDir, `gitdir: ${privateGitDir}\n`);
  }
  // No restored narrative, vote reason, citation text, or arbitrary state string reaches the model.
  for (const name of fs.readdirSync(memoryDir)) {
    if (name === ".git") continue;
    fs.rmSync(path.join(memoryDir, name), { recursive: true, force: true });
  }
  fs.writeFileSync(statePath, stateText);
  fs.writeFileSync(path.join(memoryDir, "git-sim-strategies.md"), "");
  fs.mkdirSync(path.dirname(outputPath), { recursive: true });
  fs.writeFileSync(outputPath, `${JSON.stringify({ version: 1, regression_priority: regressionPriority.slice(0, 4) })}\n`);
  return { state, signals, regressionPriority };
}

function restoreProducerHistory({ memoryDir, workspace, privateGitDir, privateHistoryPath }) {
  assertPrivatePath(privateHistoryPath, memoryDir, workspace);
  const marker = `${privateHistoryPath}.merged`;
  if (fs.existsSync(marker)) throw new TypeError("Simulator producer history was already restored");
  const history = JSON.parse(readRequiredProducerFile(privateHistoryPath, historyLimit));
  if (
    !exactKeys(history, ["version", "strategies", "strategy_notes"]) ||
    history.version !== 1 ||
    !Array.isArray(history.strategies) ||
    typeof history.strategy_notes !== "string" ||
    Buffer.byteLength(history.strategy_notes) > producerFileLimit
  ) {
    throw new TypeError("Invalid private simulator producer history");
  }
  const statePath = path.join(memoryDir, "git-sim-state.json");
  const current = JSON.parse(readRequiredProducerFile(statePath));
  sanitizeState(current);
  const additions = readRequiredProducerFile(path.join(memoryDir, "git-sim-strategies.md"));
  const mergedState = { ...current, strategies: [...history.strategies, ...current.strategies] };
  const stateText = `${JSON.stringify(mergedState, null, 2)}\n`;
  const notesText = history.strategy_notes + (history.strategy_notes && additions && !history.strategy_notes.endsWith("\n") ? "\n" : "") + additions;
  if (Buffer.byteLength(stateText) > producerFileLimit || Buffer.byteLength(notesText) > producerFileLimit) {
    throw new RangeError("Merged simulator producer evidence exceeds its 204800-byte file budget");
  }
  if (privateGitDir) {
    assertPrivatePath(privateGitDir, memoryDir, workspace);
    if (!fs.lstatSync(privateGitDir).isDirectory()) throw new TypeError("Private simulator Git metadata is missing");
    // The compiler's first persistence filter removes the Git pointer, so reconstruct it for host-side staging.
    fs.writeFileSync(path.join(memoryDir, ".git"), `gitdir: ${privateGitDir}\n`);
  }
  fs.writeFileSync(statePath, stateText);
  fs.writeFileSync(path.join(memoryDir, "git-sim-strategies.md"), notesText);
  fs.writeFileSync(marker, "merged\n", { flag: "wx", mode: 0o600 });
  return mergedState;
}

function validateProducerUpload({ memoryDir, core, stageFiles }) {
  const { execLocalGitSync, ensureSafeDirectoryTrust, stageRawGitFiles } = require("./git_helpers.cjs");
  const { getStagedPatchDiffSizeBytes } = require("./git_patch_utils.cjs");
  const { filterIneligibleMemoryFiles } = require("./memory_file_eligibility.cjs");
  const files = ["git-sim-state.json", "git-sim-strategies.md"];
  sanitizeState(JSON.parse(readRequiredProducerFile(path.join(memoryDir, files[0]))));
  readRequiredProducerFile(path.join(memoryDir, files[1]));
  ensureSafeDirectoryTrust(memoryDir, core);
  (stageFiles || stageRawGitFiles)(files, { cwd: memoryDir, stdio: "pipe" });
  const bytes = getStagedPatchDiffSizeBytes({
    execGitSyncFn: execLocalGitSync,
    cwd: memoryDir,
    pathspecs: files.map(name => `:(literal)${name}`),
  });
  if (bytes > 12288) throw new RangeError("Merged simulator producer patch exceeds the existing 10240-byte budget plus 20 percent overhead");
  const result = filterIneligibleMemoryFiles(memoryDir, [".json", ".md"], files.join(" "), core);
  if (result.kept.length !== 2 || files.some(name => !result.kept.includes(name))) {
    throw new TypeError("Simulator upload requires exactly the two eligible producer files");
  }
  return result;
}

module.exports = { acceptSignals, configIds, freshState, isolateMemory, restoreProducerHistory, validateProducerUpload, sanitizeState, validSignal };
