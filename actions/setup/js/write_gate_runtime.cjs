// @ts-check
/// <reference types="@actions/github-script" />

const path = require("node:path");
const { AsyncLocalStorage } = require("node:async_hooks");
const { assertPendingWritesIntegrity, readAuthorizedManifest, findPendingItem, inputDigest, withPendingWrite, currentPendingWrite, parsePendingJSON } = require("./pending_writes.cjs");
const { readInput } = require("./pending_writes_files.cjs");
const { readDecisionInventory, buildWriteDecisionInventory, writeDecisionInventory } = require("./write_gate_inventory.cjs");
const { evaluateItemDecision, decisionRank } = require("./write_gate_decisions.cjs");
const { createGateReviewIssue, resolveGateReviewRepo } = require("./write_gate_review.cjs");
const { parseAllowedRepos, getDefaultTargetRepo } = require("./repo_helpers.cjs");
const { checkFileProtectionPostApply, classifyFileProtectionPaths } = require("./manifest_file_helpers.cjs");
const { resolveTransportPaths } = require("./resolve_transport_paths.cjs");
const { ERR_VALIDATION } = require("./error_codes.cjs");
const { normalizeSafeOutputName } = require("./safe_output_names.cjs");
const activeItemExecution = new AsyncLocalStorage();

function replayAuthorization() {
  return require("./write_gate_replay_approval.cjs").currentHeldReplayAuthorization();
}

/** @param {any} item */
function dependenciesApplied(item) {
  if (item.depends_on.length === 0 || activeItemExecution.getStore()?.output_id === item.output_id) return true;
  const replay = replayAuthorization();
  const confirmed = new Set((replay?.confirmed_dependencies || []).map(entry => entry.output_id));
  return item.depends_on.every(id => confirmed.has(id));
}

/** Final mutation guard shared by authenticated API clients and git publication. */
function assertWriteGateItemMutation(scope) {
  const manifest = scope?.publication ? readAuthorizedManifest() : assertPendingWritesIntegrity();
  if (!manifest) return;
  const item = manifest.items.find(candidate => candidate.output_id === scope?.output_id);
  if (!item || item.complete !== true || item.item_sha256 !== scope.item_sha256) throw new Error(`${ERR_VALIDATION}: Mutation has no matching complete pending item`);
  const filename = process.env.GH_AW_WRITE_GATE_DECISIONS || process.env.GH_AW_WRITE_DECISIONS;
  if (!filename) throw new Error(`${ERR_VALIDATION}: Mutation has no normalized write decision`);
  const inventory = readDecisionInventory(filename, process.env.GH_AW_WRITE_GATE_DECISIONS_SHA256 || "", process.env.GH_AW_PENDING_WRITES_SHA256 || "");
  const authorization = inventory.items.find(candidate => candidate.output_id === item.output_id && candidate.item_sha256 === item.item_sha256);
  const replay = replayAuthorization();
  if (replay && (replay.manifest_sha256 !== inventory.manifest_sha256 || replay.write_decisions_sha256 !== process.env.GH_AW_WRITE_GATE_DECISIONS_SHA256)) throw new Error(`${ERR_VALIDATION}: Replay authorization integrity mismatch`);
  const config = parsePendingJSON(process.env.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG || "{}");
  let outcome = "apply";
  if (item.code) {
    const protection = classifyFileProtectionPaths(item.code.evaluated_paths, config[item.type] || {});
    if (protection.action === "deny") outcome = "drop";
    if (protection.action === "fallback") outcome = "review";
  }
  const { getThreatWarningPolicy } = require("./safe_output_handler_manager.cjs");
  const warning = getThreatWarningPolicy(item.type, new Map()).policy;
  const selection = evaluateItemDecision({
    gate: inventory.decision === "allow" ? "partial" : inventory.decision,
    entry: replay && !replay.held_ids.includes(item.output_id) ? "drop" : authorization?.gate_decision,
    outcome,
    detection:
      manifest.detection?.enabled !== true
        ? "pass"
        : manifest.detection.conclusion === "success"
          ? "pass"
          : manifest.detection.mode === "warn" && manifest.detection.conclusion === "warning"
            ? ["reviewable", "convertible"].includes(warning)
              ? "warn-reviewable"
              : "warn-veto"
            : "strict-failed",
    manifestMatches: true,
    staged: item.settings?.staged === true || process.env.GH_AW_SAFE_OUTPUTS_STAGED === "true" || process.env.GH_AW_REPLAY_FORCE_STAGED === "true",
    reviewRepoUsable: resolveGateReviewRepo(process.env.GH_AW_WRITE_GATE_REVIEW_REPO, getDefaultTargetRepo(), parseAllowedRepos(process.env.GH_AW_WRITE_GATE_ALLOWED_REPOS)) !== null,
    dependenciesApplied: scope.publication
      ? item.depends_on.every(id => {
          const source = manifest.items.find(candidate => candidate.output_id === id);
          const selected = inventory.items.find(candidate => candidate.output_id === id);
          return source?.complete === true && source.settings?.staged !== true && selected?.gate_decision === "apply";
        })
      : dependenciesApplied(item),
    replay: replay ? "approved" : "run",
    approverRecorded: !!replay,
  });
  if (scope.review ? decisionRank(selection.decision) > decisionRank("review") : selection.decision !== "apply") throw new Error(`${ERR_VALIDATION}: Mutation refused by per-item decision ${selection.decision} (${selection.reason})`);
}

/** Compiler-owned normalization after successful gate validation, before handlers. */
function publishWriteDecisionInventory() {
  const manifest = assertPendingWritesIntegrity();
  if (!manifest) throw new Error(`${ERR_VALIDATION}: Write gate normalization requires a bound pending manifest`);
  const manifestFile = process.env.GH_AW_PENDING_WRITES;
  if (!manifestFile) throw new Error(`${ERR_VALIDATION}: Missing verified pending manifest path`);
  const rawFile = process.env.GH_AW_WRITE_DECISIONS || path.join(path.dirname(manifestFile), "write-decisions.json");
  const validatedGate = process.env.GH_AW_VALIDATED_WRITE_GATE;
  let verdict;
  if (validatedGate === "deny") {
    const reason = process.env.GH_AW_VALIDATED_WRITE_GATE_REASON;
    verdict = { decision: "deny", reason: typeof reason === "string" && /^[a-z0-9_.-]{1,64}$/.test(reason) ? reason : "gate_error" };
  } else if (validatedGate === "allow" || validatedGate === "partial") {
    const input = readInput(rawFile);
    if (input.sha256 !== process.env.GH_AW_VALIDATED_WRITE_DECISIONS_SHA256) throw new Error(`${ERR_VALIDATION}: Authored write decisions changed after verdict validation`);
    const authoredVerdict = parsePendingJSON(input.bytes.toString("utf8"));
    verdict = authoredVerdict.decision === validatedGate ? authoredVerdict : { decision: "deny", reason: "gate_unavailable" };
    if (verdict !== authoredVerdict) core.warning("Write gate did not complete with a matching validated verdict; all items are dropped");
  } else {
    core.warning("Write gate validation is unavailable; all items are dropped");
    verdict = { decision: "deny", reason: "gate_unavailable" };
  }
  const inventory = buildWriteDecisionInventory(manifest, process.env.GH_AW_PENDING_WRITES_SHA256 || "", verdict, message => core.warning(message));
  const filename = path.join(path.dirname(manifestFile), "write-decisions.normalized.json");
  const sha256 = writeDecisionInventory(filename, inventory);
  core.setOutput("sha256", sha256);
  core.setOutput("decision", inventory.decision);
  core.setOutput("reason", inventory.decision === verdict.decision ? verdict.reason : "gate_item_incomplete");
  if (inventory.decision === "deny") {
    const { createManifestLogger } = require("./safe_output_manifest.cjs");
    const log = createManifestLogger(path.join(path.dirname(manifestFile), "safe-output-items.jsonl"));
    inventory.items.forEach((item, messageIndex) => log({ type: manifest.items[messageIndex].type, status: "skipped", skipped: true, reason: verdict.decision === "deny" ? "gate_denied" : "gate_dropped", ...item, messageIndex }));
    core.setOutput("items_sha256", readInput(path.join(path.dirname(manifestFile), "safe-output-items.jsonl")).sha256);
  }
  const reviewUsable = resolveGateReviewRepo(process.env.GH_AW_WRITE_GATE_REVIEW_REPO, getDefaultTargetRepo(), parseAllowedRepos(process.env.GH_AW_WRITE_GATE_ALLOWED_REPOS)) !== null;
  core.setOutput("review_repo_usable", String(reviewUsable));
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
    staged: item.settings?.staged === true || process.env.GH_AW_REPLAY_FORCE_STAGED === "true",
    reviewRepoUsable: resolveGateReviewRepo(process.env.GH_AW_WRITE_GATE_REVIEW_REPO, getDefaultTargetRepo(), parseAllowedRepos(process.env.GH_AW_WRITE_GATE_ALLOWED_REPOS)) !== null,
    dependenciesApplied: dependenciesApplied(item),
    replay: replayAuthorization() ? "approved" : "run",
    approverRecorded: !!replayAuthorization(),
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
  const replay = replayAuthorization();
  if (replay && (replay.manifest_sha256 !== inventory.manifest_sha256 || replay.write_decisions_sha256 !== process.env.GH_AW_WRITE_GATE_DECISIONS_SHA256))
    throw new Error(`${ERR_VALIDATION}: Replay policy does not match the pending write inventory`);

  /** @param {number} index */
  function metadata(index) {
    const item = boundItems[index];
    const selection = effective.get(index) || { decision: "drop", reason: "gate_not_evaluated" };
    return {
      output_id: item.output_id,
      item_sha256: item.item_sha256,
      messageIndex: index,
      gate_decision: selection.decision,
      decision_reason: selection.reason,
      ...(replay && replay.held_ids.includes(item.output_id) ? { approval_environment: replay.approval_environment, approvers: [...replay.approvers], replay_source_run_id: replay.source_run_id } : {}),
    };
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
      entry: replay && !replay.held_ids.includes(item.output_id) ? "drop" : authorized?.gate_decision,
      outcome,
      detection,
      manifestMatches: true,
      staged: item.settings?.staged === true || process.env.GH_AW_SAFE_OUTPUTS_STAGED === "true" || process.env.GH_AW_REPLAY_FORCE_STAGED === "true",
      reviewRepoUsable,
      dependenciesApplied: true,
      replay: replay ? "approved" : "run",
      approverRecorded: !!replay,
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
    const result = await createGateReviewIssue({
      message: { ...messages[index], output_id: item.output_id },
      reviewRepo,
      defaultRepo,
      allowedRepos,
      client: require("./pending_writes_client.cjs").guardPendingWriteClient(github),
      pendingItem: item,
      assertUnchanged: () => {
        assertPendingWritesIntegrity();
      },
      patchContent,
    });
    if (result.gate_decision === "stage") effective.set(index, { decision: "stage", reason: result.reason });
    return result;
  }

  return {
    metadata,
    select,
    review,
    /** @param {number} index @param {() => any} operation */
    run: (index, operation) => activeItemExecution.run({ output_id: boundItems[index].output_id }, operation),
    target: index => boundItems[index].target,
    confirmedPrerequisites: new Set(boundItems.flatMap((item, index) => (replay?.confirmed_dependencies.some(entry => entry.output_id === item.output_id) ? [index] : []))),
    temporaryIds: replay?.confirmed_dependencies || [],
    /** @param {number} index @param {string} reason */
    drop: (index, reason) => effective.set(index, { decision: "drop", reason }),
    planningMessages: messages.map((message, index) => {
      const { "depends-on": legacyAlias, depends_on: alias, ...planning } = message;
      return { ...planning, output_id: boundItems[index].output_id, ...(boundItems[index].depends_on.length ? { depends_on: boundItems[index].depends_on } : {}) };
    }),
  };
}

module.exports = { publishWriteDecisionInventory, createWriteGateRuntime, getHelperWriteDecision, withHelperWrite, assertWriteGateItemMutation };
