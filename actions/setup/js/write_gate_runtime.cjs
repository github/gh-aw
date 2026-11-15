// @ts-check
/// <reference types="@actions/github-script" />

const path = require("node:path");
const { assertPendingWritesIntegrity, findPendingItem, inputDigest, withPendingWrite, currentPendingWrite, parsePendingJSON, withPendingReview } = require("./pending_writes.cjs");
const { readInput } = require("./pending_writes_files.cjs");
const { readDecisionInventory, buildWriteDecisionInventory, writeDecisionInventory } = require("./write_gate_inventory.cjs");
const { evaluateItemDecision } = require("./write_gate_decisions.cjs");
const { createGateReviewIssue, resolveGateReviewRepo } = require("./write_gate_review.cjs");
const { parseAllowedRepos, getDefaultTargetRepo } = require("./repo_helpers.cjs");
const { checkFileProtectionPostApply } = require("./manifest_file_helpers.cjs");
const { resolveTransportPaths } = require("./resolve_transport_paths.cjs");
const { ERR_VALIDATION } = require("./error_codes.cjs");
const { normalizeSafeOutputName } = require("./safe_output_names.cjs");

/** Compiler-owned normalization after successful gate validation, before handlers. */
function publishWriteDecisionInventory() {
  const manifest = assertPendingWritesIntegrity();
  if (!manifest) throw new Error(`${ERR_VALIDATION}: Write gate normalization requires a bound pending manifest`);
  const rawFile = process.env.GH_AW_WRITE_DECISIONS;
  if (!rawFile) throw new Error(`${ERR_VALIDATION}: Missing authored write gate verdict`);
  const verdict = parsePendingJSON(readInput(rawFile).bytes.toString("utf8"));
  const inventory = buildWriteDecisionInventory(manifest, process.env.GH_AW_PENDING_WRITES_SHA256 || "", verdict, message => core.warning(message));
  const filename = path.join(path.dirname(rawFile), "write-decisions.normalized.json");
  const sha256 = writeDecisionInventory(filename, inventory);
  core.setOutput("sha256", sha256);
  core.exportVariable("GH_AW_WRITE_GATE_DECISIONS", filename);
  core.exportVariable("GH_AW_WRITE_GATE_DECISIONS_SHA256", sha256);
  return { filename, sha256, inventory };
}

/**
 * Custom helpers outside the manager must identify their exact pending item.
 * Dependency evidence is not inferred from a gate's permission to attempt it.
 * @param {string|undefined} outputId
 */
function getHelperWriteDecision(outputId) {
  const manifest = assertPendingWritesIntegrity();
  if (!manifest) return null;
  const active = currentPendingWrite();
  const id = outputId || active?.output_id;
  if (!id) throw new Error(`${ERR_VALIDATION}: Gated custom createIssue requires output_id`);
  const item = manifest.items.find(candidate => candidate.output_id === id);
  if (!item || (active && active.output_id !== id)) throw new Error(`${ERR_VALIDATION}: Custom helper pending item mismatch`);
  const filename = process.env.GH_AW_WRITE_GATE_DECISIONS || process.env.GH_AW_WRITE_DECISIONS;
  if (!filename) throw new Error(`${ERR_VALIDATION}: Missing normalized write decisions`);
  const inventory = readDecisionInventory(filename, process.env.GH_AW_WRITE_GATE_DECISIONS_SHA256 || "", process.env.GH_AW_PENDING_WRITES_SHA256 || "");
  const authorization = inventory.items.find(candidate => candidate.output_id === id);
  if (!authorization || authorization.item_sha256 !== item.item_sha256 || authorization.input_sha256 !== item.input_sha256) throw new Error(`${ERR_VALIDATION}: Custom helper write decision integrity mismatch`);
  const selection = evaluateItemDecision({
    gate: inventory.decision === "allow" ? "partial" : inventory.decision,
    entry: authorization.gate_decision,
    outcome: item.complete === true ? "apply" : "drop",
    detection: manifest.detection?.enabled !== true ? "pass" : manifest.detection.conclusion === "success" ? "pass" : "strict-failed",
    manifestMatches: true,
    staged: item.settings?.staged === true,
    reviewRepoUsable: resolveGateReviewRepo(process.env.GH_AW_WRITE_GATE_REVIEW_REPO, getDefaultTargetRepo(), parseAllowedRepos(process.env.GH_AW_WRITE_GATE_ALLOWED_REPOS)) !== null,
    dependenciesApplied: item.depends_on.length === 0,
    replay: "run",
    approverRecorded: false,
  });
  return { item, selection };
}

/** @param {Record<string, any>} item @param {() => any} operation */
async function withHelperWrite(item, operation) {
  assertPendingWritesIntegrity();
  if (currentPendingWrite()?.output_id === item.output_id) return operation();
  const bindings = parsePendingJSON(process.env.GH_AW_PENDING_WRITES_BINDINGS || "[]");
  const source = bindings.find(binding => binding.id === "agent_output" && binding.kind === "file");
  if (!source) throw new Error(`${ERR_VALIDATION}: Missing custom helper source binding`);
  const output = parsePendingJSON(readInput(source.path).bytes.toString("utf8"));
  if (!Array.isArray(output.items)) throw new Error(`${ERR_VALIDATION}: Invalid custom helper source items`);
  const messages = output.items.map(message => ({ ...message, type: normalizeSafeOutputName(message.type) }));
  const index = messages.findIndex((message, index) => inputDigest(message) === item.input_sha256 && (message.output_id || `so_${inputDigest([index, message]).slice(0, 12)}`) === item.output_id);
  if (index < 0) throw new Error(`${ERR_VALIDATION}: Custom helper source item mismatch`);
  return withPendingWrite(messages[index], operation, index);
}

/**
 * This snapshot is created before any handler. It is never read from handler
 * result metadata, and every per-message selection rechecks the bound inputs.
 *
 * @param {Array<Record<string, any>>} messages
 */
function createWriteGateRuntime(messages) {
  const manifest = assertPendingWritesIntegrity();
  if (!manifest) return null;
  const filename = process.env.GH_AW_WRITE_GATE_DECISIONS || process.env.GH_AW_WRITE_DECISIONS;
  if (!filename) throw new Error(`${ERR_VALIDATION}: Missing normalized write decisions`);
  const inventory = readDecisionInventory(filename, process.env.GH_AW_WRITE_GATE_DECISIONS_SHA256 || "", process.env.GH_AW_PENDING_WRITES_SHA256 || "");
  const selections = new Map(inventory.items.map(item => [item.output_id, item]));
  if (inventory.items.length !== manifest.items.length) throw new Error(`${ERR_VALIDATION}: Write decisions do not cover the pending manifest`);
  for (const item of manifest.items) {
    const selection = selections.get(item.output_id);
    if (!selection || selection.item_sha256 !== item.item_sha256 || selection.input_sha256 !== item.input_sha256) {
      throw new Error(`${ERR_VALIDATION}: Write decision item integrity mismatch`);
    }
  }
  const boundItems = messages.map((message, index) => {
    const id = message.output_id || `so_${inputDigest([index, message]).slice(0, 12)}`;
    const item = manifest.items.find(candidate => candidate.output_id === id && candidate.input_sha256 === inputDigest(message));
    if (!item) throw new Error(`${ERR_VALIDATION}: Missing pending write item`);
    return item.complete === true ? findPendingItem(manifest, message, index) : item;
  });
  const handlerConfig = parsePendingJSON(process.env.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG || "{}");
  const allowedRepos = parseAllowedRepos(process.env.GH_AW_WRITE_GATE_ALLOWED_REPOS);
  const reviewRepo = process.env.GH_AW_WRITE_GATE_REVIEW_REPO;
  const defaultRepo = getDefaultTargetRepo();
  const reviewRepoUsable = resolveGateReviewRepo(reviewRepo, defaultRepo, allowedRepos) !== null;
  const effective = new Map();

  /** @param {number} index */
  function metadata(index) {
    const item = boundItems[index];
    const selection = effective.get(index) || { decision: "drop", reason: "gate_not_evaluated" };
    return { output_id: item.output_id, item_sha256: item.item_sha256, messageIndex: index, gate_decision: selection.decision, decision_reason: selection.reason };
  }

  /**
   * @param {number} index
   * @param {"reviewable"|"convertible"|"abort"|"none"} warningPolicy
   */
  async function select(index, warningPolicy) {
    assertPendingWritesIntegrity();
    const item = boundItems[index];
    const authorized = selections.get(item.output_id);
    let outcome = item.complete === true ? "apply" : "drop";
    if (item.complete === true && item.code) {
      const protection = await withPendingWrite(messages[index], () => checkFileProtectionPostApply(item.code.evaluated_paths, handlerConfig[item.type] || {}), index);
      if (protection.action === "deny") outcome = "drop";
      if (protection.action === "fallback") outcome = "review";
    }
    const detection =
      manifest.detection?.enabled !== true
        ? "pass"
        : manifest.detection.conclusion === "success"
          ? "pass"
          : manifest.detection.conclusion === "warning" && manifest.detection.mode === "warn"
            ? warningPolicy === "reviewable"
              ? "warn-reviewable"
              : warningPolicy === "convertible"
                ? "warn-reviewable"
                : "warn-veto"
            : "strict-failed";
    if (manifest.detection?.conclusion === "warning" && warningPolicy === "convertible") outcome = "review";
    const selection = evaluateItemDecision({
      gate: inventory.decision === "allow" ? "partial" : inventory.decision,
      entry: authorized?.gate_decision,
      outcome,
      detection,
      manifestMatches: true,
      staged: item.settings?.staged === true || process.env.GH_AW_SAFE_OUTPUTS_STAGED === "true",
      reviewRepoUsable,
      dependenciesApplied: true,
      replay: "run",
      approverRecorded: false,
    });
    if (selection.decision === authorized?.gate_decision) selection.reason = authorized.decision_reason;
    effective.set(index, selection);
    return selection;
  }

  /** @param {number} index */
  async function review(index) {
    const item = boundItems[index];
    let patchContent = "";
    if (item.code) {
      const transport = resolveTransportPaths(messages[index], item.target.repository);
      if (!transport.patchPath) throw new Error(`${ERR_VALIDATION}: Gate review patch missing`);
      const patch = readInput(transport.patchPath);
      if (patch.sha256 !== item.code.patch_rendering.sha256) throw new Error(`${ERR_VALIDATION}: Gate review patch integrity mismatch`);
      patchContent = patch.bytes.toString("utf8");
    }
    const result = await withPendingReview(item, reviewRepo, () =>
      createGateReviewIssue({
        message: { ...messages[index], output_id: item.output_id },
        reviewRepo,
        defaultRepo,
        allowedRepos,
        client: require("./pending_writes_client.cjs").guardPendingWriteClient(github),
        assertUnchanged: () => {
          assertPendingWritesIntegrity();
        },
        patchContent,
      })
    );
    if (result.gate_decision === "stage") effective.set(index, { decision: "stage", reason: result.reason });
    return result;
  }

  return {
    metadata,
    select,
    review,
    /** @param {number} index @param {string} reason */
    drop: (index, reason) => effective.set(index, { decision: "drop", reason }),
    planningMessages: messages.map((message, index) => {
      const { "depends-on": legacyAlias, depends_on: alias, ...planning } = message;
      return { ...planning, output_id: boundItems[index].output_id, ...(boundItems[index].depends_on.length ? { depends_on: boundItems[index].depends_on } : {}) };
    }),
  };
}

module.exports = { publishWriteDecisionInventory, createWriteGateRuntime, getHelperWriteDecision, withHelperWrite };
