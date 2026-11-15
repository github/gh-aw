// @ts-check

const DECISIONS = Object.freeze(["apply", "hold", "review", "stage", "drop"]);

/** @param {unknown} value @returns {number} */
function decisionRank(value) {
  const rank = typeof value === "string" ? DECISIONS.indexOf(value) : -1;
  return rank < 0 ? DECISIONS.length - 1 : rank;
}

/** @param {...unknown} values @returns {string} */
function restrictiveDecision(...values) {
  return DECISIONS[values.reduce((rank, value) => Math.max(rank, decisionRank(value)), 0)];
}

/**
 * Missing entries remain absent: the evaluator treats them as drop, never apply.
 * Invalid records with a known ID poison that ID, including valid duplicates.
 *
 * @param {unknown} entries
 * @param {Set<string>} knownIds
 * @param {(message: string) => void} warn
 * @returns {Map<string, {decision: string, reason: string}>}
 */
function parseItemDecisions(entries, knownIds, warn) {
  if (!Array.isArray(entries)) throw new Error("Write gate partial verdict requires an items array");
  const decisions = new Map();
  for (const entry of entries) {
    if (!entry || typeof entry !== "object" || Array.isArray(entry) || typeof entry.output_id !== "string") {
      throw new Error("Write gate item entry requires an output_id");
    }
    if (!knownIds.has(entry.output_id)) {
      warn(`Ignoring write gate decision for unknown output_id: ${entry.output_id}`);
      continue;
    }
    const valid = DECISIONS.includes(entry.decision) && typeof entry.reason === "string" && /^[a-z0-9_.-]{1,64}$/.test(entry.reason);
    if (!valid) warn(`Invalid write gate item decision; dropping output_id: ${entry.output_id}`);
    const current = valid ? { decision: entry.decision, reason: entry.reason } : { decision: "drop", reason: "gate_item_invalid" };
    const prior = decisions.get(entry.output_id);
    if (!prior || decisionRank(current.decision) > decisionRank(prior.decision)) decisions.set(entry.output_id, current);
  }
  return decisions;
}

/**
 * Pure narrowing model. Gate/manifest/approval booleans must come from trusted
 * runtime verification, never from the authored decision document.
 *
 * @typedef {object} ItemDecisionInput
 * @property {string} gate allow|partial, or a failed/unavailable gate state
 * @property {unknown} entry
 * @property {unknown} outcome Existing gh-aw restriction
 * @property {string} detection pass|warn-reviewable|warn-veto|strict-failed
 * @property {boolean} manifestMatches
 * @property {boolean} staged
 * @property {boolean} reviewRepoUsable
 * @property {boolean} dependenciesApplied
 * @property {string} replay run|unapproved|approved
 * @property {boolean} approverRecorded
 *
 * @param {ItemDecisionInput} input
 * @returns {{decision: string, reason: string}}
 */
function evaluateItemDecision(input) {
  if (input.gate !== "allow" && input.gate !== "partial") return { decision: "drop", reason: "gate_denied" };
  if (!input.manifestMatches) return { decision: "drop", reason: "gate_integrity_mismatch" };
  if (input.detection === "strict-failed") return { decision: "drop", reason: "gate_detection_failed" };
  if (!input.dependenciesApplied) return { decision: "drop", reason: "gate_dependency_unapplied" };
  const gateDecision = input.gate === "allow" ? "apply" : DECISIONS[decisionRank(input.entry)];
  let decision = gateDecision;
  if (input.replay !== "run") {
    if (input.replay !== "approved" || !input.approverRecorded || gateDecision !== "hold") return { decision: "drop", reason: "gate_replay_unapproved" };
    decision = "apply";
  }
  const outcome = restrictiveDecision(input.outcome, input.staged ? "stage" : "apply", input.detection === "warn-veto" ? "drop" : "apply");
  decision = restrictiveDecision(decision, outcome);
  let reason = decision === gateDecision ? `gate_${decision}` : "gate_existing_restriction";
  if (input.replay === "approved" && decision === "apply") reason = "gate_approved_replay";
  if (decision === "review" && !input.reviewRepoUsable) {
    decision = "stage";
    reason = "gate_review_repo_unusable";
  }
  return { decision, reason };
}

module.exports = { DECISIONS, decisionRank, restrictiveDecision, parseItemDecisions, evaluateItemDecision };
