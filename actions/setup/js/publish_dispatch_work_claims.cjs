// @ts-check
"use strict";

const fs = require("fs");
const path = require("path");
const { CURRENT_VERSION } = require("./work_queue_codemods.cjs");
const { applyTransactions, replayTransactions } = require("./work_queue_replay.cjs");
const { applyAndPublishCoordinatorTransactions, readCoordinatorLog } = require("./work_queue_store.cjs");
const { selectNext, validateSelection } = require("./dispatch_work_coordinator_selection.cjs");

const CLAIM_INTENT_PATH = "/tmp/gh-aw/work-queue.claims.jsonl";
// Kept outside downloaded agent artifacts: only this trusted job writes it.
const PUBLISHED_CLAIMS_PATH = path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw", "trusted-dispatch-coordinator", "published.json");

function readClaimIntents(filePath = CLAIM_INTENT_PATH) {
  if (!fs.existsSync(filePath)) return [];
  let contents;
  try {
    contents = fs.readFileSync(filePath, "utf8");
  } catch (error) {
    throw new Error("Failed to read pending dispatch claim intents", { cause: error });
  }
  const lines = contents
    .trim()
    .split("\n")
    .filter(line => line.trim());
  if (lines.length > 100) throw new RangeError("at most 100 pending claims may be published");
  const identities = new Set();
  return lines.map((line, index) => {
    let intent;
    try {
      intent = JSON.parse(line);
    } catch (error) {
      throw new TypeError(`claim intent line ${index + 1} is malformed`, { cause: error });
    }
    if (
      !intent ||
      typeof intent !== "object" ||
      Array.isArray(intent) ||
      Object.keys(intent).sort().join(",") !== "claim_id,selection,work_id" ||
      typeof intent.work_id !== "string" ||
      !intent.work_id ||
      typeof intent.claim_id !== "string" ||
      !intent.claim_id
    ) {
      throw new TypeError(`claim intent line ${index + 1} has an invalid shape`);
    }
    validateSelection(intent.selection);
    if (identities.has(intent.claim_id)) throw new TypeError("claim intents contain duplicate identities");
    identities.add(intent.claim_id);
    return intent;
  });
}

async function publishDispatcherClaims(options = {}) {
  const publishedPath = options.publishedClaimsPath || PUBLISHED_CLAIMS_PATH;
  writePublishedClaims(publishedPath, []);
  const intents = readClaimIntents(options.claimIntentPath || CLAIM_INTENT_PATH);
  if (!intents.length) return [];
  const repositoryContext = options.context;
  const runID = String(repositoryContext?.runId ?? process.env.GITHUB_RUN_ID ?? "").trim();
  if (!runID || !repositoryContext?.repo) throw new Error("trusted workflow run context is required to publish claims");
  const { owner, repo } = repositoryContext.repo;
  const publish = options.applyAndPublish || applyAndPublishCoordinatorTransactions;
  const published = await publish({
    githubClient: options.githubClient,
    owner,
    repo,
    intents: [],
    core: options.core,
    deriveIntents: transactions => {
      let current = transactions;
      const claims = [];
      for (const intent of intents) {
        const claim = { version: CURRENT_VERSION, kind: "Claim", work_id: intent.work_id, claim_id: intent.claim_id, run_id: runID };
        const existing = current.find(transaction => transaction.kind === "Claim" && transaction.claim_id === claim.claim_id);
        if (existing) {
          if (existing.work_id !== claim.work_id || existing.run_id !== runID || replayTransactions(current).work[claim.work_id] !== "claimed" || replayTransactions(current).winner[claim.work_id] !== claim.claim_id) {
            throw new Error("pending claim conflicts with an existing claim");
          }
        } else {
          const next = selectNext(current, intent.selection);
          if (!next || next.work_id !== intent.work_id) throw new Error("pending claim selection is stale; no dispatcher effects are authorized");
          const applied = applyTransactions(current, [claim]);
          if (applied.rejected.length) throw new Error("pending claim was rejected");
          current = applied.transactions;
        }
        claims.push(claim);
      }
      return claims;
    },
  });
  if (published.rejected.length) throw new Error("dispatcher claim publication was rejected");
  const readLog = options.readCoordinatorLog || readCoordinatorLog;
  const latest = await readLog({ githubClient: options.githubClient, owner, repo, core: options.core });
  const projection = replayTransactions(latest.transactions);
  const assignments = intents.map(intent => {
    const claim = latest.transactions.find(transaction => transaction.kind === "Claim" && transaction.claim_id === intent.claim_id);
    const work = latest.transactions.find(transaction => transaction.kind === "Work" && transaction.work_id === intent.work_id);
    if (!claim || !work || claim.work_id !== intent.work_id || claim.run_id !== runID || projection.work[intent.work_id] !== "claimed" || projection.winner[intent.work_id] !== intent.claim_id) {
      throw new Error("dispatcher claim publication could not be verified");
    }
    return { work_id: intent.work_id, claim_id: intent.claim_id, work: work.work };
  });
  writePublishedClaims(publishedPath, assignments);
  options.core?.info(`Dispatch coordinator: verified ${assignments.length} dispatcher claims`);
  return assignments;
}

function readPublishedAssignment(claimID, filePath = PUBLISHED_CLAIMS_PATH) {
  let assignments;
  try {
    assignments = JSON.parse(fs.readFileSync(filePath, "utf8"));
  } catch (error) {
    throw new Error("Failed to read verified dispatch claim assignments", { cause: error });
  }
  if (!Array.isArray(assignments)) throw new TypeError("published dispatcher claims have an invalid shape");
  const assignment = assignments.find(assignment => assignment.claim_id === claimID);
  if (!assignment) throw new Error("dispatch claim was not published by this workflow");
  return assignment;
}

function writePublishedClaims(filePath, assignments) {
  try {
    fs.mkdirSync(path.dirname(filePath), { recursive: true });
    fs.writeFileSync(filePath, JSON.stringify(assignments), { mode: 0o600 });
  } catch (error) {
    throw new Error("Failed to record verified dispatch claim assignments", { cause: error });
  }
}

module.exports = { CLAIM_INTENT_PATH, PUBLISHED_CLAIMS_PATH, publishDispatcherClaims, readClaimIntents, readPublishedAssignment };
