// @ts-check
/// <reference types="@actions/github-script" />

const { assertPendingWritesIntegrity, readAuthorizedManifest, inputDigest, parsePendingJSON } = require("./pending_writes.cjs");
const { readInput } = require("./pending_writes_files.cjs");
const { readDecisionInventory } = require("./write_gate_inventory.cjs");
const { evaluateItemDecision } = require("./write_gate_decisions.cjs");
const { createManifestLogger, MANIFEST_FILE_PATH } = require("./safe_output_manifest.cjs");
const { currentHeldReplayAuthorization, recordHeldReplayCompletion } = require("./write_gate_replay_approval.cjs");
const { createGateReviewIssue, resolveGateReviewRepo } = require("./write_gate_review.cjs");
const { getDefaultTargetRepo, parseAllowedRepos } = require("./repo_helpers.cjs");
const { ERR_VALIDATION } = require("./error_codes.cjs");
const NATIVE_REPLAY = Object.freeze({
  push_repo_memory: { module: "push_repo_memory.cjs", load: () => require("./push_repo_memory.cjs") },
  push_ledger_changes: { module: "push_ledger_changes.cjs", load: () => require("./push_ledger_changes.cjs") },
  push_evals_state: { module: "push_evals_state.cjs", load: () => require("./push_evals_state.cjs") },
});

function publicationState() {
  const manifest = assertPendingWritesIntegrity();
  if (!manifest) return null;
  const inventory = readDecisionInventory(process.env.GH_AW_WRITE_GATE_DECISIONS || "", process.env.GH_AW_WRITE_GATE_DECISIONS_SHA256 || "", process.env.GH_AW_PENDING_WRITES_SHA256 || "");
  return { manifest, inventory };
}

function publicationSelection(item, state) {
  const authorization = state.inventory.items.find(entry => entry.output_id === item.output_id);
  if (!authorization || authorization.item_sha256 !== item.item_sha256 || authorization.input_sha256 !== item.input_sha256) throw new Error(`${ERR_VALIDATION}: Publication decision does not match its original input`);
  const replay = currentHeldReplayAuthorization();
  const replayUnsupported = ["hold", "review"].includes(authorization.gate_decision) && (!Object.hasOwn(NATIVE_REPLAY, item.target.job) || item.publication.invocation?.module !== NATIVE_REPLAY[item.target.job].module);
  const decision = evaluateItemDecision({
    gate: state.inventory.decision === "allow" ? "partial" : state.inventory.decision,
    entry: replay && !replay.held_ids.includes(item.output_id) ? "drop" : authorization.gate_decision,
    outcome: item.complete !== true ? "drop" : replayUnsupported ? "stage" : "apply",
    detection: !state.manifest.detection.enabled || state.manifest.detection.conclusion === "success" ? "pass" : "strict-failed",
    manifestMatches: true,
    staged: item.settings.staged === true || process.env.GH_AW_SAFE_OUTPUTS_STAGED === "true" || process.env.GH_AW_REPLAY_FORCE_STAGED === "true",
    dependenciesApplied: true,
    reviewRepoUsable: resolveGateReviewRepo(process.env.GH_AW_WRITE_GATE_REVIEW_REPO, getDefaultTargetRepo(), parseAllowedRepos(process.env.GH_AW_WRITE_GATE_ALLOWED_REPOS)) !== null,
    replay: replay ? "approved" : "run",
    approverRecorded: !!replay,
  });
  return { ...decision, reason: replayUnsupported && decision.decision === "stage" ? "gate_replay_unsupported" : decision.decision === authorization.gate_decision ? authorization.decision_reason : decision.reason };
}

function publicationMetadata(item, selection) {
  const replay = currentHeldReplayAuthorization();
  return {
    output_id: item.output_id,
    item_sha256: item.item_sha256,
    gate_decision: selection.decision,
    decision_reason: selection.reason,
    ...(replay ? { approval_environment: replay.approval_environment, approvers: [...replay.approvers], replay_source_run_id: replay.source_run_id } : {}),
  };
}

function verifyFrozenHeldLedgerPreparation() {
  if (!currentHeldReplayAuthorization()) throw new Error(`${ERR_VALIDATION}: Frozen ledger preparation requires reviewed replay`);
  const manifest = assertPendingWritesIntegrity();
  const publications = manifest?.items.filter(item => item.target.job === "push_ledger_changes" && item.publication?.id === "transactions");
  const generated = parsePendingJSON(process.env.GH_AW_PENDING_GENERATED_BINDINGS || "[]");
  const bindings = generated.filter(binding => binding.id === "generated:push_ledger_changes:transactions" && binding.kind === "file");
  if (publications?.length !== 1 || publications[0].complete !== true || bindings.length !== 1) throw new Error(`${ERR_VALIDATION}: Held ledger preparation requires its original frozen transaction input`);
  const artifact = parsePendingJSON(readInput(bindings[0].path).bytes.toString("utf8"));
  if (inputDigest(artifact) !== publications[0].publication.sha256) throw new Error(`${ERR_VALIDATION}: Held ledger transaction input changed`);
  core.info("Using verified original ledger transactions for reviewed replay");
}

/** Complete the decision inventory without inventing successful effects. */
function recordUnpublishedItemDecisions(results, filename = MANIFEST_FILE_PATH) {
  const recorded = new Set(
    readInput(filename)
      .bytes.toString("utf8")
      .split("\n")
      .filter(Boolean)
      .map(line => parsePendingJSON(line).output_id)
  );
  const log = createManifestLogger(filename);
  for (const result of results) {
    if (!result.output_id || !result.gate_decision || recorded.has(result.output_id)) continue;
    const failure = result.success === false && !result.skipped && !result.staged && !result.deferred && !result.cancelled;
    log({
      type: result.type,
      output_id: result.output_id,
      item_sha256: result.item_sha256,
      gate_decision: result.gate_decision,
      decision_reason: result.decision_reason,
      status: failure ? "failure" : "skipped",
      ...(failure ? {} : { skipped: true }),
      reason: failure ? "gate_handler_failed" : "gate_no_effect",
      ...(result.approval_environment ? { approval_environment: result.approval_environment, approvers: result.approvers, replay_source_run_id: result.replay_source_run_id } : {}),
    });
    recorded.add(result.output_id);
  }
}

/** The native later job owns an apply receipt; preparing its inputs is not success. */
/** @param {((item: any) => void)|undefined} [logger] */
async function recordPublicationDecisions(logger) {
  if (currentHeldReplayAuthorization()) return;
  const state = publicationState();
  if (!state) return;
  const log = logger || createManifestLogger();
  for (const item of state.manifest.items.filter(item => item.publication)) {
    const selection = publicationSelection(item, state);
    if (selection.reason === "gate_replay_unsupported") core.warning(`Held/review publication ${item.output_id} was staged because its original native replay invocation is unavailable`);
    const metadata = publicationMetadata(item, selection);
    if (selection.decision === "review") {
      const result = await createGateReviewIssue({
        message: { type: item.type, output_id: item.output_id },
        pendingItem: item,
        client: require("./pending_writes_client.cjs").guardPendingWriteClient(github),
        reviewRepo: process.env.GH_AW_WRITE_GATE_REVIEW_REPO,
        defaultRepo: getDefaultTargetRepo(),
        allowedRepos: parseAllowedRepos(process.env.GH_AW_WRITE_GATE_ALLOWED_REPOS),
        assertUnchanged: assertPendingWritesIntegrity,
      });
      if (result.success === true && !result.skipped && result.issue_url) {
        log({ type: "create_issue", status: "success", url: result.issue_url, number: result.issue_number, repo: result.repo, ...metadata, review_publication: true });
        continue;
      }
      if (result.gate_decision !== "stage") throw new Error(`${ERR_VALIDATION}: Publication review was not published`);
      metadata.gate_decision = "stage";
      metadata.decision_reason = result.reason;
    }
    const decision = metadata.gate_decision;
    log({
      type: item.type,
      status: "skipped",
      skipped: true,
      reason: decision === "hold" ? "gate_held" : decision === "stage" ? "gate_staged" : decision === "apply" ? "gate_delegated" : "gate_dropped",
      ...metadata,
    });
  }
}

/** Invoke only compiler-owned native modules with the restored original env. */
/** @param {any} restored @param {((item: any) => void)|undefined} [log] */
async function executeHeldPublications(restored, log) {
  const replay = currentHeldReplayAuthorization();
  if (!replay) throw new Error(`${ERR_VALIDATION}: Held publication has no reviewed replay capability`);
  const state = publicationState();
  if (!state) throw new Error(`${ERR_VALIDATION}: Held publication manifest is unavailable`);
  const items = state.manifest.items.filter(item => item.publication && replay.held_ids.includes(item.output_id));
  if (items.length === 0) return;
  const record = log || createManifestLogger();
  for (const item of items) {
    const selection = publicationSelection(item, state);
    const metadata = publicationMetadata(item, selection);
    if (selection.decision !== "apply") {
      record({ type: item.type, status: "skipped", skipped: true, reason: selection.decision === "stage" ? "gate_staged" : "gate_dropped", ...metadata });
      continue;
    }
    const invocation = restored.publication_invocations.filter(entry => entry.job === item.target.job && entry.id === item.publication.id);
    const implementation = Object.hasOwn(NATIVE_REPLAY, item.target.job) ? NATIVE_REPLAY[item.target.job] : undefined;
    if (invocation.length !== 1 || !implementation || invocation[0].module !== implementation.module) throw new Error(`${ERR_VALIDATION}: Held publication requires a supported frozen native invocation`);
    const original = restored.bindings.find(binding => binding.id === "publication_invocations")?.value?.find(entry => entry.job === item.target.job && entry.id === item.publication.id);
    if (!original || inputDigest(Object.fromEntries(Object.entries(original).filter(([key]) => !["job", "id"].includes(key)))) !== item.publication.invocation?.sha256)
      throw new Error(`${ERR_VALIDATION}: Held publication invocation digest mismatch`);
    const previous = new Map();
    const previousCore = global.core;
    const outputs = {};
    let failure;
    try {
      for (const [name, value] of Object.entries(invocation[0].env)) {
        if (/(?:TOKEN|SECRET|PASSWORD|PRIVATE_KEY)/i.test(name) || typeof value !== "string") throw new Error(`${ERR_VALIDATION}: Held publication invocation contains credentials or non-string env`);
        previous.set(name, process.env[name]);
        process.env[name] = value;
      }
      global.core = {
        ...previousCore,
        setOutput: (name, value) => {
          outputs[name] = value;
          previousCore.setOutput(name, value);
        },
        setFailed: message => {
          failure = message;
          previousCore.setFailed(message);
        },
      };
      const result = await implementation.load().main();
      if (failure) throw new Error(`${ERR_VALIDATION}: Held native publication failed: ${failure}`);
      const applied =
        item.target.job === "push_repo_memory"
          ? outputs.save_status === "saved"
          : item.target.job === "push_evals_state"
            ? result?.success === true && !result.skipped
            : result?.ledgers &&
              Object.values(result.ledgers).some(ledger => ledger.persisted > 0) &&
              Object.values(result.ledgers).every(ledger => !ledger.staged && !ledger.rejected && ledger.persisted + ledger.already_present === ledger.requested);
      if (applied) recordHeldReplayCompletion(item, { success: true });
      record({ type: item.type, status: applied ? "success" : "skipped", ...(applied ? {} : { skipped: true, reason: "native_no_effect" }), ...metadata });
    } catch (cause) {
      record({ type: item.type, status: "failure", reason: "native_publication_failed", ...metadata });
      throw new Error(`${ERR_VALIDATION}: Held publication did not complete`, { cause });
    } finally {
      global.core = previousCore;
      for (const [name, value] of previous) {
        if (value === undefined) delete process.env[name];
        else process.env[name] = value;
      }
    }
  }
  readAuthorizedManifest();
}

module.exports = { recordPublicationDecisions, executeHeldPublications, recordUnpublishedItemDecisions, verifyFrozenHeldLedgerPreparation };
