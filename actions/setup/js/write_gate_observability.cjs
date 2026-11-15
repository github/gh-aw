// @ts-check
"use strict";

const VERDICTS = new Set(["allow", "deny", "partial"]);
const { DECISIONS, decisionRank } = require("./write_gate_decisions.cjs");
const SIGNALS = new Set(["gate_denied", "gate_held", "gate_staged", "gate_dropped"]);
const CODE = /^[a-z0-9_.-]{1,64}$/;
const DIGEST = /^[a-f0-9]{64}$/;
const CHECKS = new Set(["verified", "failed", "unknown"]);

/**
 * @param {unknown} event
 * @returns {event is {type: "gate.decision", data: Record<string, unknown>, provenance: {component: "write_gate", phase: "conclusion", path: "usage/aw_session.jsonl"}}}
 */
function isCompilerGateDecision(event) {
  if (!event || typeof event !== "object" || !("type" in event) || event.type !== "gate.decision" || !("data" in event) || !event.data || typeof event.data !== "object" || !("provenance" in event)) return false;
  const provenance = event.provenance;
  return Boolean(
    provenance &&
    typeof provenance === "object" &&
    "component" in provenance &&
    provenance.component === "write_gate" &&
    "phase" in provenance &&
    provenance.phase === "conclusion" &&
    "path" in provenance &&
    provenance.path === "usage/aw_session.jsonl"
  );
}

/** @param {any} source @returns {Record<string, any>} */
function projectGateDecision(source) {
  if (!source || !VERDICTS.has(source.verdict) || typeof source.reason !== "string" || !CODE.test(source.reason) || !CHECKS.has(source.integrity) || !CHECKS.has(source.conclusionCheck)) return {};
  const data = { verdict: source.verdict, reason: source.reason, integrity: source.integrity, conclusionCheck: source.conclusionCheck };
  if (DIGEST.test(source.manifestDigest)) data.manifestDigest = source.manifestDigest;
  if (source.failureCategory === "write_gate_violation") data.failureCategory = source.failureCategory;
  if (source.counts && DECISIONS.every(key => Number.isSafeInteger(source.counts[key]) && source.counts[key] >= 0)) {
    data.counts = Object.fromEntries(DECISIONS.map(key => [key, source.counts[key]]));
  }
  if (Array.isArray(source.items)) {
    data.items = source.items.flatMap(item => {
      if (!item || typeof item.output_id !== "string" || !/^(so_[A-Za-z0-9_-]{12}|job_[a-f0-9]{16})$/.test(item.output_id) || !DECISIONS.includes(item.gate_decision)) return [];
      return [
        {
          output_id: item.output_id,
          gate_decision: item.gate_decision,
          ...(typeof item.decision_reason === "string" && CODE.test(item.decision_reason) ? { decision_reason: item.decision_reason } : {}),
          ...(DIGEST.test(item.item_sha256) ? { item_sha256: item.item_sha256 } : {}),
        },
      ];
    });
  }
  return data;
}

/**
 * Check recorded enforcement without changing the verdict or equating missing
 * evidence with an absence of writes.
 * @param {{env: NodeJS.ProcessEnv, results: any[], manifest?: any, actualDigest?: string, manifestValid?: boolean, resultsComplete?: boolean, decisions?: any, decisionsDigest?: string, decisionsValid?: boolean}} options
 * @returns {Record<string, any>|undefined}
 */
function buildGateDecision({ env, results, manifest, actualDigest, manifestValid, resultsComplete = false, decisions, decisionsDigest, decisionsValid }) {
  if (env.GH_AW_WRITE_GATE === undefined) return undefined;
  const valid = VERDICTS.has(env.GH_AW_WRITE_GATE) && CODE.test(env.GH_AW_WRITE_GATE_REASON ?? "");
  const verdict = valid ? env.GH_AW_WRITE_GATE : "deny";
  const reason = valid ? env.GH_AW_WRITE_GATE_REASON : "gate_error";
  const expected = env.GH_AW_PENDING_WRITES_SHA256;
  let integrity = "unknown";
  if (expected && actualDigest) {
    integrity = DIGEST.test(expected) && expected === actualDigest ? (manifest?.complete === true ? "verified" : "unknown") : "failed";
    if (manifestValid === false) integrity = "failed";
  }
  let failed = integrity === "failed";
  let complete = integrity === "verified" && (resultsComplete || (Array.isArray(manifest?.items) && manifest.items.every(item => item.target?.job)));
  const counts = Object.fromEntries(DECISIONS.map(key => [key, 0]));
  const items = [];
  const pending = new Map((Array.isArray(manifest?.items) ? manifest.items : []).map(item => [item.output_id, item]));
  const authorized = new Map();
  const observed = new Set();
  let decisionsVerified = false;
  if (decisionsValid === false) failed = true;
  if (decisions || env.GH_AW_WRITE_GATE_DECISIONS_SHA256) {
    const expectedDecisions = env.GH_AW_WRITE_GATE_DECISIONS_SHA256;
    if (!decisions || !decisionsDigest || !expectedDecisions) complete = false;
    else if (!DIGEST.test(expectedDecisions) || decisionsDigest !== expectedDecisions || decisions.manifest_sha256 !== expected || decisions.schema !== "gh-aw/write-decisions/v1" || !Array.isArray(decisions.items)) failed = true;
    else {
      decisionsVerified = true;
      for (const decision of decisions.items) {
        const item = pending.get(decision.output_id);
        if (!item || item.item_sha256 !== decision.item_sha256 || !DECISIONS.includes(decision.gate_decision) || authorized.has(decision.output_id)) {
          failed = true;
          continue;
        }
        authorized.set(decision.output_id, decision);
        counts[decision.gate_decision]++;
        items.push(decision);
      }
      if (authorized.size !== pending.size) complete = false;
    }
  }
  for (const result of results) {
    if (!result || typeof result !== "object") {
      complete = false;
      continue;
    }
    if (typeof result.output_id === "string") observed.add(result.output_id);
    if (!decisions && DECISIONS.includes(result.gate_decision)) {
      counts[result.gate_decision]++;
      items.push(result);
    }
    const blocked = SIGNALS.has(result.gate_signal) || SIGNALS.has(result.decision_reason);
    const applied = ["success", "warning"].includes(result.status ?? "success") && !result.staged;
    if (!applied) {
      if (!["skipped", "staged"].includes(result.status) && !blocked) complete = false;
      continue;
    }
    if (manifest && manifest.complete !== true) failed = true;
    const item = pending.get(result.output_id);
    if (verdict === "deny" || ["hold", "stage", "drop"].includes(result.gate_decision)) failed = true;
    if (!item || !DIGEST.test(result.item_sha256)) {
      complete = false;
      if ((result.output_id && !item && integrity === "verified") || (result.item_sha256 && !DIGEST.test(result.item_sha256))) failed = true;
    } else if (item.item_sha256 !== result.item_sha256) failed = true;
    if (result.gate_decision !== undefined && !DECISIONS.includes(result.gate_decision)) failed = true;
    if (result.gate_decision === "review") {
      const reviewUrl = require("./safe_output_outcomes.cjs").canonicalSafeOutputUrl(result.url);
      const reviewUri = reviewUrl ? new URL(reviewUrl) : undefined;
      if (result.review_publication !== true) complete = false;
      else if (result.type !== "create_issue") failed = true;
      else if (!reviewUri || reviewUri.hash || !/^\/[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+\/issues\/[1-9][0-9]*\/?$/.test(reviewUri.pathname)) complete = false;
    }
    const decision = authorized.get(result.output_id);
    if (decisionsVerified) {
      if (!decision) failed = true;
      else if (result.gate_decision === undefined) complete = false;
      else if (decisionRank(result.gate_decision) < decisionRank(decision.gate_decision)) failed = true;
    } else if (verdict === "partial") complete = false;
  }
  let needs;
  let writeJobs;
  let publicationResults;
  const verifiedJobs = new Set();
  const memoryResults = require("./memory_save_results.cjs").readMemorySaveResults(env.GH_AW_MEMORY_SAVE_RESULTS);
  try {
    needs = JSON.parse(env.GH_AW_JOB_RESULTS ?? "null");
    writeJobs = JSON.parse(env.GH_AW_WRITE_GATE_WRITE_JOB_NAMES ?? "null");
    publicationResults = JSON.parse(env.GH_AW_WRITE_GATE_PUBLICATION_RESULTS ?? "null");
  } catch {
    complete = false;
  }
  if (!needs || !Array.isArray(writeJobs)) complete = false;
  else {
    for (const job of writeJobs) {
      const result = needs[job]?.result;
      const jobItems = [...pending.values()].filter(item => item.target?.job === job);
      if (!["success", "failure", "cancelled", "skipped"].includes(result)) complete = false;
      if (job === "push_repo_memory" && decisionsVerified) {
        for (const item of jobItems) {
          if (memoryResults.some(memory => memory.memoryId === item.publication?.id && memory.status === "saved" && memory.commit) && decisionRank("apply") < decisionRank(authorized.get(item.output_id)?.gate_decision)) failed = true;
        }
      }
      if (result === "success") {
        if (verdict === "deny") failed = true;
        if (verdict === "partial" && decisionsVerified) {
          if (!jobItems.length || jobItems.some(item => !["apply", "review"].includes(authorized.get(item.output_id)?.gate_decision))) failed = true;
        }
        const proof = publicationResults?.[job];
        let suppliedItems;
        if (proof?.pending_writes_verified === "true") {
          try {
            suppliedItems = JSON.parse(proof.pending_writes_item_sha256);
          } catch {
            complete = false;
          }
        }
        let inputsVerified = Boolean(suppliedItems && typeof suppliedItems === "object" && !Array.isArray(suppliedItems) && jobItems.length);
        if (inputsVerified) {
          for (const [id, digest] of Object.entries(suppliedItems)) {
            const item = jobItems.find(item => item.output_id === id);
            if (!item || item.item_sha256 !== digest) failed = true;
          }
          if (jobItems.some(item => !Object.hasOwn(suppliedItems, item.output_id))) inputsVerified = false;
        }
        // Existing memory push outputs attest to saved/unchanged effects. Input
        // receipts alone do not attest to arbitrary custom or action effects.
        const effectsVerified =
          job === "push_repo_memory" &&
          jobItems.every(item =>
            memoryResults.some(
              memory =>
                memory.memoryId === item.publication?.id &&
                memory.jobResult === "success" &&
                memory.outcome === "success" &&
                (memory.status === "unchanged" || (memory.status === "saved" && memory.commit && (!item.target.branch || item.target.branch === memory.branch)))
            )
          );
        if (inputsVerified && effectsVerified) verifiedJobs.add(job);
        else complete = false;
      }
      if (result === "failure" || result === "cancelled") complete = false;
    }
  }
  for (const item of pending.values()) {
    if (item.target?.job) {
      if (needs?.[item.target.job]?.result !== "skipped" && !verifiedJobs.has(item.target.job)) complete = false;
    } else if (!observed.has(item.output_id)) complete = false;
  }
  return projectGateDecision({
    verdict,
    reason,
    integrity,
    conclusionCheck: failed ? "failed" : complete ? "verified" : "unknown",
    ...(DIGEST.test(expected ?? "") ? { manifestDigest: expected } : {}),
    ...(failed ? { failureCategory: "write_gate_violation" } : {}),
    counts,
    items,
  });
}

/**
 * Policy suppression is observable without a handler execution. Emit only
 * pinned non-application decisions, never successful effects or resource URLs.
 * @param {{gate?: Record<string, any>, manifest?: any, decisions?: any, decisionsValid?: boolean, results: any[], resultsReadable?: boolean}} options
 * @returns {Array<Record<string, any>>}
 */
function buildGateBlockedResults({ gate, manifest, decisions, decisionsValid, results, resultsReadable = true }) {
  if (gate?.integrity !== "verified" || decisionsValid !== true || !resultsReadable) return [];
  const observed = new Set(results.map(result => result?.output_id));
  const pending = new Map(manifest.items.map(item => [item.output_id, item]));
  return decisions.items.flatMap(decision => {
    const item = pending.get(decision.output_id);
    if (!item || typeof item.type !== "string" || !item.type || observed.has(decision.output_id) || item.item_sha256 !== decision.item_sha256) return [];
    const signal = gate.verdict === "deny" ? "gate_denied" : { hold: "gate_held", stage: "gate_staged", drop: "gate_dropped" }[decision.gate_decision];
    if (!signal) return [];
    return [
      {
        type: item.type,
        output_id: item.output_id,
        item_sha256: item.item_sha256,
        status: "skipped",
        gate_decision: decision.gate_decision,
        decision_reason: decision.decision_reason,
        gate_signal: signal,
      },
    ];
  });
}

/**
 * Compiler-owned conclusion check. It runs before best-effort usage generation
 * so a proven violation cannot be hidden by that step's continue-on-error.
 * @param {{core?: typeof import("@actions/core"), rootDir?: string, env?: NodeJS.ProcessEnv}} [options]
 */
function checkConclusion({ core = global.core, rootDir = "/tmp/gh-aw", env = process.env } = {}) {
  try {
    const { collectUnifiedSession } = require("./unified_session.cjs");
    const gate = collectUnifiedSession({ rootDir, writeGate: env, jobResults: env.GH_AW_JOB_RESULTS, warn: message => core.warning(message) }).events.find(isCompilerGateDecision)?.data;
    if (!gate) throw new Error("Gate conclusion evidence is unavailable");
    core.setOutput("integrity", gate.integrity);
    core.setOutput("conclusion_check", gate.conclusionCheck);
    if (gate.failureCategory === "write_gate_violation") {
      const fs = require("node:fs");
      const path = require("node:path");
      fs.writeFileSync(path.join(rootDir, "failure_categories.json"), JSON.stringify(["write_gate_violation"]));
      core.setFailed("ERR_WRITE_GATE: Conclusion detected an unauthorized write or publication digest mismatch.");
      return;
    }
  } catch (error) {
    core.setFailed(`ERR_WRITE_GATE: Unable to check write gate enforcement: ${require("./error_helpers.cjs").getErrorMessage(error)}`);
    return;
  }
}

module.exports = { buildGateDecision, buildGateBlockedResults, projectGateDecision, isCompilerGateDecision, checkConclusion, DECISIONS, SIGNALS, CODE, DIGEST };
