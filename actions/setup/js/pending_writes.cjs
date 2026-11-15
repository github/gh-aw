// @ts-check
"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { AsyncLocalStorage } = require("node:async_hooks");
const { validString, parseStrictJSON } = require("./work_queue_codec.cjs");
const { readInput, describeDirectory, sha256, MAX_INPUT_BYTES } = require("./pending_writes_files.cjs");

const SCHEMA = "gh-aw/pending-writes/v1";
const activeWrite = new AsyncLocalStorage();

/**
 * Canonical JSON: UTF-16 lexicographic keys, ECMAScript finite-number spelling,
 * array order retained, UTF-8, no whitespace or trailing newline.
 * @param {unknown} value
 * @param {number} [depth]
 * @returns {string}
 */
function canonicalPendingJSON(value, depth = 0) {
  if (depth > 64) throw new Error("Pending-write JSON exceeds the nesting limit");
  if (value === null) return "null";
  if (typeof value === "string") return JSON.stringify(validString(value));
  if (typeof value === "boolean" || (typeof value === "number" && Number.isFinite(value))) return JSON.stringify(value);
  if (Array.isArray(value)) return `[${value.map(child => canonicalPendingJSON(child, depth + 1)).join(",")}]`;
  if (!value || typeof value !== "object" || ![Object.prototype, null].includes(Object.getPrototypeOf(value))) throw new Error("Pending-write value must be JSON");
  return `{${Object.keys(value)
    .sort()
    .map(key => `${JSON.stringify(validString(key))}:${canonicalPendingJSON(value[key], depth + 1)}`)
    .join(",")}}`;
}

const inputDigest = value => sha256(canonicalPendingJSON(value));
function parsePendingJSON(text) {
  const parsed = JSON.parse(text);
  return parseStrictJSON(text, { maxBytes: MAX_INPUT_BYTES, finiteNumbersIn: parsed && typeof parsed === "object" ? Object.keys(parsed) : [] });
}
function itemDigest(item) {
  const { item_sha256, ...body } = item;
  return inputDigest(body);
}

function pendingWritesEnabled() {
  return process.env.GH_AW_PENDING_WRITES_REQUIRED === "true";
}

function resolvedGateConfig() {
  return {
    review_repo: process.env.GH_AW_WRITE_GATE_REVIEW_REPO || "",
    allowed_repos: process.env.GH_AW_WRITE_GATE_ALLOWED_REPOS || "",
    approval_environment: process.env.GH_AW_WRITE_GATE_APPROVAL_ENVIRONMENT || "",
  };
}

function pendingContext(value = globalThis.context) {
  if (!value?.repo || !value.eventName || !value.payload) throw integrityError("context_missing");
  return parsePendingJSON(JSON.stringify({ ...value, repo: value.repo, ...(value.issue ? { issue: value.issue } : {}) }));
}

function pendingEnvironment() {
  return Object.fromEntries(
    [
      "GH_AW_TARGET_REPO_SLUG",
      "GH_AW_WORKFLOW_ID",
      "GH_AW_WORKFLOW_NAME",
      "GH_AW_WORKFLOW_SOURCE",
      "GH_AW_WORKFLOW_SOURCE_URL",
      "GH_AW_SAFE_OUTPUTS_STAGED",
      "GH_AW_CUSTOM_BASE_BRANCH",
      "DEFAULT_BRANCH",
      "GITHUB_BASE_REF",
      "GH_AW_DETECTION_CONCLUSION",
      "GH_AW_DETECTION_REASON",
      "GH_AW_DETECTION_THREAT_KINDS",
    ].map(name => [name, process.env[name] || ""])
  );
}

function integrityError(reason = "inputs_changed") {
  return Object.assign(new Error(`Pending writes refused: ${reason}`), { code: "ERR_PENDING_WRITES", reason });
}

function pendingAuthorization(manifest, item) {
  const { readDecisionInventory } = require("./write_gate_inventory.cjs");
  const file = process.env.GH_AW_WRITE_GATE_DECISIONS;
  const sha = process.env.GH_AW_WRITE_GATE_DECISIONS_SHA256;
  if (!file || !sha) throw integrityError("decisions_missing");
  const inventory = readDecisionInventory(file, sha, process.env.GH_AW_PENDING_WRITES_SHA256 || "");
  if (inventory.items.length !== manifest.items.length) throw integrityError("decisions_invalid");
  const selection = inventory.items.find(selection => selection.output_id === item.output_id);
  if (!selection || selection.item_sha256 !== item.item_sha256 || selection.input_sha256 !== item.input_sha256) throw integrityError("decisions_invalid");
  return { inventory, selection };
}

function pendingHoldApproved(item) {
  const modulePath = path.join(__dirname, "write_gate_replay_approval.cjs");
  if (!fs.existsSync(modulePath)) return false;
  const capability = require(modulePath).currentHeldReplayAuthorization();
  return Boolean(
    capability &&
    capability.manifest_sha256 === process.env.GH_AW_PENDING_WRITES_SHA256 &&
    capability.write_decisions_sha256 === process.env.GH_AW_WRITE_GATE_DECISIONS_SHA256 &&
    Array.isArray(capability.held_ids) &&
    capability.held_ids.includes(item.output_id)
  );
}

function assertPendingEffectAllowed() {
  if (!pendingWritesEnabled()) return;
  const active = activeWrite.getStore();
  if (!active || active.planning) throw integrityError("item_missing");
  const manifest = readAuthorizedManifest();
  const item = manifest.items.find(item => item.output_id === active.output_id && item.item_sha256 === active.item_sha256);
  if (!item || item.complete !== true) throw integrityError("item_missing");
  const { selection } = pendingAuthorization(manifest, item);
  const mayApply = selection.gate_decision === "apply" || (selection.gate_decision === "hold" && pendingHoldApproved(item));
  if (item.settings.staged === true || process.env.GH_AW_REPLAY_FORCE_STAGED === "true" || (active.review ? !["apply", "review", "hold"].includes(selection.gate_decision) : !mayApply)) throw integrityError("item_not_authorized");
}

/**
 * Load only the compiler-bound digest. Gate steps may edit the manifest but
 * cannot bless it by rewriting a digest stored alongside the downloaded file.
 */
function readAuthorizedManifest(file = process.env.GH_AW_PENDING_WRITES, expected = process.env.GH_AW_PENDING_WRITES_SHA256) {
  if (!file || !/^[0-9a-f]{64}$/.test(expected || "")) throw integrityError("manifest_missing");
  let bytes;
  try {
    ({ bytes } = readInput(file));
  } catch (cause) {
    throw Object.assign(integrityError("manifest_missing"), { cause });
  }
  if (sha256(bytes) !== expected) throw integrityError();
  const manifest = parsePendingJSON(bytes.toString("utf8"));
  if (manifest.schema !== SCHEMA || typeof manifest.complete !== "boolean" || !Array.isArray(manifest.items) || !Array.isArray(manifest.inputs) || canonicalPendingJSON(manifest) !== bytes.toString("utf8")) {
    throw integrityError("manifest_incomplete");
  }
  const ids = new Set();
  for (const item of manifest.items) {
    if (typeof item.complete !== "boolean" || typeof item.output_id !== "string" || ids.has(item.output_id) || item.item_sha256 !== itemDigest(item)) throw integrityError("manifest_invalid");
    ids.add(item.output_id);
  }
  return manifest;
}

/** Recompute downloaded bytes, including missing/added files in inventories. */
function assertManifestInputs(manifest, bindings) {
  if (!Array.isArray(bindings) || bindings.length !== manifest.inputs.length) throw integrityError();
  const described = bindings.map(binding => {
    if (binding.kind === "directory") return { id: binding.id, kind: binding.kind, files: describeDirectory(binding.path) };
    if (binding.kind === "value") return { id: binding.id, kind: binding.kind, sha256: inputDigest(binding.value) };
    if (binding.kind !== "file") throw integrityError("manifest_invalid");
    const { length, sha256: digest, mode } = readInput(binding.path);
    return { id: binding.id, kind: binding.kind, length, sha256: digest, mode };
  });
  if (inputDigest(described) !== inputDigest(manifest.inputs)) throw integrityError();
}

/**
 * Check all inputs immediately before handler creation and again at each
 * handler invocation; raw content never becomes a verdict reason.
 */
function assertPendingWritesIntegrity() {
  if (!pendingWritesEnabled()) return null;
  const manifest = readAuthorizedManifest();
  const bindings = parseStrictJSON(process.env.GH_AW_PENDING_WRITES_BINDINGS || "[]");
  const { withoutCredentials } = require("./pending_writes_plan.cjs");
  const handlerConfig = parsePendingJSON(process.env.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG || "{}");
  const configBinding = bindings.find(binding => binding.id === "handler_config");
  if (!configBinding) throw integrityError("manifest_invalid");
  configBinding.value = withoutCredentials(handlerConfig);
  const gateBinding = bindings.find(binding => binding.id === "gate_config");
  if (!gateBinding) throw integrityError("manifest_invalid");
  gateBinding.value = resolvedGateConfig();
  const contextBinding = bindings.find(binding => binding.id === "context");
  if (!contextBinding) throw integrityError("manifest_invalid");
  contextBinding.value = pendingContext();
  const environmentBinding = bindings.find(binding => binding.id === "environment");
  if (!environmentBinding) throw integrityError("manifest_invalid");
  environmentBinding.value = pendingEnvironment();
  try {
    assertManifestInputs(manifest, bindings);
  } catch (cause) {
    throw Object.assign(integrityError(), { cause });
  }
  return manifest;
}

/** @param {any} manifest @param {any} message @param {number} [index] */
function findPendingItem(manifest, message, index = undefined) {
  const digest = inputDigest(message);
  const id = message.output_id || (index === undefined ? undefined : `so_${inputDigest([index, message]).slice(0, 12)}`);
  const matches = manifest.items.filter(item => item.input_sha256 === digest && (id === undefined || item.output_id === id));
  if (matches.length !== 1) throw integrityError("item_missing");
  return matches[0];
}

/** @param {any} message @param {() => any} operation @param {number} [index] */
async function withPendingWrite(message, operation, index = undefined) {
  if (!pendingWritesEnabled()) return operation();
  const manifest = assertPendingWritesIntegrity();
  const item = findPendingItem(manifest, message, index);
  if (item.complete !== true) throw integrityError("item_incomplete");
  return activeWrite.run(item, operation);
}

function withPendingPlanning(operation) {
  return activeWrite.run({ planning: true }, operation);
}

async function withPendingReview(item, repository, operation) {
  if (!pendingWritesEnabled()) throw integrityError("review_scope_missing");
  const manifest = assertPendingWritesIntegrity();
  if (repository !== process.env.GH_AW_WRITE_GATE_REVIEW_REPO || !manifest.items.some(candidate => candidate.output_id === item.output_id && candidate.item_sha256 === item.item_sha256)) throw integrityError("review_scope_invalid");
  return activeWrite.run({ type: "create_issue", review: true, target: { repository }, output_id: item.output_id, item_sha256: item.item_sha256 }, operation);
}

function pendingBranchSalt() {
  return activeWrite.getStore()?.code?.branch_salt;
}

function currentPendingWrite() {
  return activeWrite.getStore();
}

/** Validate the exact handler-resolved destination, not the agent's proposal. */
function assertPendingTarget(target) {
  if (!pendingWritesEnabled()) return;
  const item = activeWrite.getStore();
  if (item?.planning) return;
  if (!item) throw integrityError("item_missing");
  for (const [key, value] of Object.entries(target)) {
    if (key === "repository" && value === item.target.head_repository) continue;
    if (value !== undefined && item.target[key] !== value) throw integrityError("target_changed");
  }
}

/** Compare the list evaluated by the authoritative post-apply check. */
function assertPendingCodeFiles(actualFiles) {
  if (!pendingWritesEnabled()) return;
  const item = activeWrite.getStore();
  if (!item?.code?.complete) throw integrityError("code_plan_incomplete");
  const expected = [...new Set(item.code.evaluated_paths)].sort();
  if (inputDigest([...new Set(actualFiles)].sort()) !== inputDigest(expected)) throw integrityError("files_changed");
}

function assertPendingGitDestination(repository, branch) {
  if (!pendingWritesEnabled()) return;
  const item = activeWrite.getStore();
  if (!item || item.planning || item.complete !== true) throw integrityError("item_missing");
  assertPendingEffectAllowed();
  const expected = item.target.head_repository || item.target.repository;
  if (repository.toLowerCase() !== expected.toLowerCase() || (item.target.branch && branch !== item.target.branch) || (item.target.branches && !item.target.branches.includes(branch))) throw integrityError("target_changed");
  if (item.publication) item.recheck();
}

/** Signed-push API and git-push transports share this last publication check. */
function assertPendingCodePublication({ owner, repo, branch, baseRef, cwd }) {
  if (!pendingWritesEnabled()) return;
  assertPendingEffectAllowed();
  const item = activeWrite.getStore();
  const { execLocalGitSync } = require("./git_helpers.cjs");
  if (item?.publication) {
    item.recheck();
    if (`${owner}/${repo}` !== item.target.repository || (item.target.branch && branch !== item.target.branch) || (item.target.branches && !item.target.branches.includes(branch))) throw integrityError("target_changed");
    return execLocalGitSync(["rev-parse", "--verify", "HEAD^{commit}"], { cwd }).trim();
  }
  if (!item?.code?.complete) throw integrityError("code_plan_incomplete");
  if (`${owner}/${repo}` !== (item.target.head_repository || item.target.repository) || branch !== item.target.branch) throw integrityError("target_changed");
  const options = { cwd, timeout: 10000, maxBuffer: MAX_INPUT_BYTES };
  const head = execLocalGitSync(["rev-parse", "--verify", "HEAD^{commit}"], options).trim();
  const base = execLocalGitSync(["rev-parse", "--verify", `${baseRef}^{commit}`], options).trim();
  if (item.code.base_commit && !base.startsWith(item.code.base_commit)) throw integrityError("base_changed");
  const files = execLocalGitSync(["diff", "--name-only", "--no-renames", "-z", `${base}..${head}`], options)
    .split("\0")
    .filter(Boolean);
  assertPendingCodeFiles(files);
  if (!item.code.tree_sha256 || sha256(Buffer.from(execLocalGitSync(["ls-tree", "-r", "-z", head], { ...options, encoding: "latin1" }), "latin1")) !== item.code.tree_sha256) throw integrityError("content_changed");
  return head;
}

/**
 * Downstream jobs compare their exact supplied artifact, record batch or
 * directory against the entry bound to the safe_outputs job digest.
 */
function assertPendingPublication(job, id, supplied) {
  if (!pendingWritesEnabled()) return;
  const manifest = readAuthorizedManifest();
  const matches = manifest.items.filter(item => item.target.job === job && item.publication?.id === id);
  if (matches.length !== 1 || matches[0].complete !== true || matches[0].publication.sha256 !== inputDigest(supplied)) throw integrityError();
  const { inventory, selection } = pendingAuthorization(manifest, matches[0]);
  const mayApply = selection.gate_decision === "apply" || (selection.gate_decision === "hold" && pendingHoldApproved(matches[0]));
  if (!mayApply || process.env.GH_AW_REPLAY_FORCE_STAGED === "true" || matches[0].depends_on.some(id => inventory.items.find(item => item.output_id === id)?.gate_decision !== "apply")) throw integrityError("item_not_authorized");
  if (matches[0].settings?.staged === true || (manifest.detection.enabled && ["warning", "failure"].includes(manifest.detection.conclusion))) throw integrityError("native_policy_veto");
}

async function withPendingPublication(job, id, readSupplied, operation) {
  if (!pendingWritesEnabled()) return operation();
  const manifest = readAuthorizedManifest();
  const item = manifest.items.find(item => item.target.job === job && item.publication?.id === id);
  if (!item) throw integrityError("item_missing");
  const recheck = () => assertPendingPublication(job, id, readSupplied());
  recheck();
  return activeWrite.run({ ...item, recheck }, async () => {
    const previousGithub = globalThis.github;
    const previousFactory = globalThis.getOctokit;
    require("./pending_writes_client.cjs").installPendingWriteClientGuards();
    try {
      return await operation();
    } finally {
      globalThis.github = previousGithub;
      globalThis.getOctokit = previousFactory;
    }
  });
}

async function withPendingDirectoryPublication(job, id, bindingId, directory, operation) {
  if (!pendingWritesEnabled()) return operation();
  const { captureDirectory } = require("./pending_writes_files.cjs");
  const captured = captureDirectory(directory, bindingId);
  assertPendingPublication(job, id, captured.supplied);
  return withPendingPublication(
    job,
    id,
    () => ({ id: bindingId, kind: "directory", files: describeDirectory(directory) }),
    () => {
      activeWrite.getStore().captured_files = captured.captured;
      return operation();
    }
  );
}

function capturedPublicationFile(file) {
  if (!pendingWritesEnabled()) return null;
  const input = activeWrite.getStore()?.captured_files?.get(path.resolve(file));
  if (!input) throw integrityError("inputs_changed");
  return { bytes: Buffer.from(input.bytes), mode: input.mode };
}

/** Compiler-owned check after authored gate steps. */
async function verify(expected, options) {
  try {
    const { buildPendingWrites } = require("./pending_writes_plan.cjs");
    const current = await buildPendingWrites(options);
    const bytes = readInput(options.manifestPath).bytes;
    if (current.sha256 !== expected || sha256(bytes) !== expected) return { allow: false, reason: "inputs_changed" };
    return { allow: true, reason: current.manifest.complete ? "allowed" : "manifest_incomplete", manifest: current.manifest };
  } catch {
    return { allow: false, reason: "inputs_changed" };
  }
}

/** @param {Record<string, any>} options */
async function prepare(options) {
  const { buildPendingWrites } = require("./pending_writes_plan.cjs");
  const result = await buildPendingWrites(options);
  fs.mkdirSync(path.dirname(options.manifestPath), { recursive: true });
  fs.writeFileSync(options.manifestPath, result.bytes, { flag: "wx", mode: 0o600 });
  return result;
}

module.exports = {
  SCHEMA,
  canonicalPendingJSON,
  parsePendingJSON,
  inputDigest,
  itemDigest,
  pendingWritesEnabled,
  resolvedGateConfig,
  gateConfigFromEnv: resolvedGateConfig,
  pendingContext,
  pendingEnvironment,
  readAuthorizedManifest,
  assertManifestInputs,
  assertPendingWritesIntegrity,
  findPendingItem,
  withPendingWrite,
  withPendingPlanning,
  withPendingReview,
  pendingBranchSalt,
  currentPendingWrite,
  assertPendingTarget,
  assertPendingCodeFiles,
  assertPendingGitDestination,
  assertPendingCodePublication,
  assertPendingPublication,
  withPendingPublication,
  withPendingDirectoryPublication,
  capturedPublicationFile,
  integrityError,
  pendingAuthorization,
  assertPendingEffectAllowed,
  verify,
  prepare,
};
