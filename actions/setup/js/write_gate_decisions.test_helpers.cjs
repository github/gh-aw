// @ts-check

const { createHash } = require("node:crypto");
const { DECISIONS, decisionRank, evaluateItemDecision } = require("./write_gate_decisions.cjs");

/** @typedef {import("./write_gate_decisions.cjs").ItemDecisionInput & {entry: string, outcome: string, family: string}} PropertyInput */

// The plan did not define its 120,960 axes. These independent, explicit axes
// cover 564,480 cases, including separate later-job/custom-job families.
const AXES = Object.freeze({
  gate: ["allow", "partial", "deny", "failed", "missing", "malformed"],
  entry: ["apply", "hold", "review", "stage", "drop", "missing", "invalid"],
  outcome: DECISIONS,
  detection: ["pass", "warn-reviewable", "warn-veto", "strict-failed"],
  manifestMatches: [false, true],
  replay: ["run", "unapproved", "approved"],
  staged: [false, true],
  family: ["code", "issues", "comments", "labels", "dispatch", "later", "custom"],
  approverRecorded: [false, true],
  reviewRepoUsable: [false, true],
  dependenciesApplied: [false, true],
});

/** @param {PropertyInput} input */
function originalGateDecision(input) {
  return input.gate === "allow" ? "apply" : input.gate === "partial" ? DECISIONS[decisionRank(input.entry)] : "drop";
}

/** @param {PropertyInput} input @param {string} decision @returns {string[]} */
function violatedProperties(input, decision) {
  const failed = [];
  const writes = decision === "apply" || decision === "review";
  const authorized = input.gate === "allow" || input.gate === "partial";
  const gate = originalGateDecision(input);
  const approvedHold = input.replay === "approved" && input.approverRecorded && gate === "hold";
  const native = Math.max(decisionRank(input.outcome), input.staged ? 3 : 0, input.detection === "warn-veto" ? 4 : 0);
  if (writes && !authorized) failed.push("WG-1");
  if (writes && input.detection === "strict-failed") failed.push("WG-2");
  if (writes && !input.manifestMatches) failed.push("WG-3");
  if (decision === "apply" && (!authorized || !input.manifestMatches || input.detection === "strict-failed" || !input.dependenciesApplied || native !== 0 || !(input.replay === "run" ? gate === "apply" : approvedHold))) failed.push("WG-4");
  if (decisionRank(decision) < native || decisionRank(decision) < (approvedHold ? 0 : decisionRank(gate))) failed.push("WG-5");
  if (writes && input.gate === "partial" && (input.entry === "missing" || input.entry === "invalid")) failed.push("WG-6");
  if (decision === "review" && !input.reviewRepoUsable) failed.push("WG-7");
  if (
    writes &&
    (!authorized ||
      !input.manifestMatches ||
      (input.gate === "partial" && (input.entry === "missing" || input.entry === "invalid")) ||
      (input.replay !== "run" && !approvedHold) ||
      (decision === "apply" && !(input.replay === "run" ? gate === "apply" : approvedHold)))
  )
    failed.push("WG-9");
  return failed;
}

/** @type {Record<string, {required: string[], mutate: (input: PropertyInput, decision: string) => string}>} */
const DEFECTS = {
  gate_failure_allow: { required: ["WG-1", "WG-4", "WG-5", "WG-9"], mutate: (input, decision) => (input.gate === "failed" ? evaluateItemDecision({ ...input, gate: "allow" }).decision : decision) },
  malformed_file_allow: { required: ["WG-1", "WG-4", "WG-5", "WG-9"], mutate: (input, decision) => (["missing", "malformed"].includes(input.gate) ? evaluateItemDecision({ ...input, gate: "allow" }).decision : decision) },
  later_job_ignores_verdict: { required: ["WG-1", "WG-4", "WG-5", "WG-6"], mutate: (input, decision) => (input.family === "later" ? evaluateItemDecision({ ...input, gate: "allow" }).decision : decision) },
  integrity_skipped: { required: ["WG-3", "WG-9"], mutate: input => evaluateItemDecision({ ...input, manifestMatches: true }).decision },
  missing_entry_apply: {
    required: ["WG-4", "WG-5", "WG-6", "WG-9"],
    mutate: (input, decision) => (input.gate === "partial" && ["missing", "invalid"].includes(input.entry) ? evaluateItemDecision({ ...input, entry: "apply" }).decision : decision),
  },
  staged_overridden: { required: ["WG-4", "WG-5"], mutate: input => evaluateItemDecision({ ...input, staged: false }).decision },
  most_permissive: { required: ["WG-4", "WG-5", "WG-6", "WG-9"], mutate: input => DECISIONS[Math.min(decisionRank(originalGateDecision(input)), decisionRank(input.outcome))] },
  unusable_review_repo_apply: { required: ["WG-4", "WG-5", "WG-8", "WG-9"], mutate: (input, decision) => (decision === "stage" && evaluateItemDecision({ ...input, reviewRepoUsable: true }).decision === "review" ? "apply" : decision) },
  custom_job_ignores_decisions: { required: ["WG-4", "WG-5", "WG-6"], mutate: (input, decision) => (input.family === "custom" ? evaluateItemDecision({ ...input, gate: "allow" }).decision : decision) },
  warn_veto_ignored: { required: ["WG-4", "WG-5"], mutate: (input, decision) => (input.detection === "warn-veto" ? evaluateItemDecision({ ...input, detection: "pass" }).decision : decision) },
  replay_bypass: { required: ["WG-1", "WG-3", "WG-4", "WG-6", "WG-9"], mutate: (input, decision) => (input.replay !== "run" ? "apply" : decision) },
  hold_without_approval: {
    required: ["WG-4", "WG-9"],
    mutate: (input, decision) => (input.replay === "unapproved" && originalGateDecision(input) === "hold" ? evaluateItemDecision({ ...input, replay: "approved", approverRecorded: true }).decision : decision),
  },
};

/**
 * WG-8 checks adjacent lattice decisions and removal of review repository access.
 * Reviewed held-item replay is the explicitly authorized exception to gate rank;
 * replay still cannot loosen an existing gh-aw restriction.
 *
 * @param {PropertyInput} input
 * @param {(input: PropertyInput) => string} evaluate
 */
function violatesMonotonicity(input, evaluate) {
  const rank = decisionRank(evaluate(input));
  if (input.reviewRepoUsable && decisionRank(evaluate({ ...input, reviewRepoUsable: false })) < rank) return true;
  const outcome = decisionRank(input.outcome);
  if (outcome < 4 && decisionRank(evaluate({ ...input, outcome: DECISIONS[outcome + 1] })) < rank) return true;
  const entry = decisionRank(input.entry);
  return input.replay === "run" && input.gate === "partial" && entry < 4 && decisionRank(evaluate({ ...input, entry: DECISIONS[entry + 1] })) < rank;
}

/** @param {(input: PropertyInput) => void} visit */
function enumerateInputs(visit) {
  const axes = Object.entries(AXES);
  /** @type {PropertyInput} */
  const input = {
    gate: "missing",
    entry: "missing",
    outcome: "drop",
    detection: "strict-failed",
    manifestMatches: false,
    replay: "run",
    staged: false,
    family: "code",
    approverRecorded: false,
    reviewRepoUsable: false,
    dependenciesApplied: false,
  };
  function walk(index) {
    if (index === axes.length) return visit(input);
    const [key, values] = axes[index];
    for (const value of values) {
      input[key] = value;
      walk(index + 1);
    }
  }
  walk(0);
}

/** @param {boolean} [mutations] */
function runProperties(mutations = true) {
  const failures = {};
  const defectFailures = Object.fromEntries(Object.keys(DEFECTS).map(name => [name, new Set()]));
  const ranks = [];
  const histogram = Object.fromEntries(DECISIONS.map(decision => [decision, 0]));
  let count = 0;
  /** @param {PropertyInput} input */
  const evaluate = input => evaluateItemDecision(input).decision;
  enumerateInputs(input => {
    const decision = evaluate(input);
    ranks.push(decisionRank(decision));
    histogram[decision]++;
    count++;
    const violated = violatedProperties(input, decision);
    if (violatesMonotonicity(input, evaluate)) violated.push("WG-8");
    for (const property of violated) failures[property] = (failures[property] || 0) + 1;
    if (!mutations) return;
    for (const [name, defect] of Object.entries(DEFECTS)) {
      const mutate = candidate => defect.mutate(candidate, evaluate(candidate));
      for (const property of violatedProperties(input, defect.mutate(input, decision))) defectFailures[name].add(property);
      if (violatesMonotonicity(input, mutate)) defectFailures[name].add("WG-8");
    }
  });
  return {
    count,
    sha256: createHash("sha256").update(Buffer.from(ranks)).digest("hex"),
    histogram,
    failures,
    defects: Object.fromEntries(Object.entries(defectFailures).map(([name, properties]) => [name, [...properties].sort()])),
  };
}

if (require.main === module) process.stdout.write(JSON.stringify(runProperties()));
module.exports = { AXES, DEFECTS, enumerateInputs, violatedProperties, violatesMonotonicity, runProperties };
