// @ts-check
"use strict";

const { loadWorkQueueSnapshot } = require("./work_queue_mcp_server.cjs");
const { replayTransactions } = require("./work_queue_replay.cjs");
const { readWorkQueueLog } = require("./work_queue_store.cjs");

/**
 * @param {Record<string, string>} states
 * @param {string} state
 */
function countState(states, state) {
  return Object.values(states).filter(value => value === state).length;
}

/**
 * @param {ReturnType<typeof loadWorkQueueSnapshot>} snapshot
 * @param {ReturnType<typeof replayTransactions>} current
 */
function renderSummary(snapshot, current) {
  const before = snapshot.projection;
  const baseline = new Set(before.transactions.map(transaction => JSON.stringify(transaction)));
  const added = current.transactions.filter(transaction => !baseline.has(JSON.stringify(transaction)));
  const workCount = Object.keys(current.work).length;
  const lines = [
    "### Work queue activity",
    "",
    `${workCount} work item${workCount === 1 ? "" : "s"} in the queue; ${added.length} new transaction${added.length === 1 ? "" : "s"} observed since activation.`,
    "",
    "<details>",
    "<summary>Show work queue activity</summary>",
    "",
    "This compares the activation snapshot with the queue read during conclusion. The queue is shared: changes may include activity from other workflow runs.",
    "",
    "| Work state | At activation | At conclusion |",
    "| --- | ---: | ---: |",
  ];
  for (const state of ["available", "claimed", "completed", "cancelled"]) {
    lines.push(`| ${state} | ${countState(before.work, state)} | ${countState(current.work, state)} |`);
  }
  lines.push("", "| Claim state | At activation | At conclusion |", "| --- | ---: | ---: |");
  for (const state of ["effective", "superseded", "cancelled"]) {
    lines.push(`| ${state} | ${countState(before.claim, state)} | ${countState(current.claim, state)} |`);
  }
  lines.push("", "| Activity since activation | Transactions |", "| --- | ---: |");
  for (const [kind, label] of [
    ["Work", "Work added"],
    ["Claim", "Claims created"],
    ["ClaimCancellation", "Claims cancelled"],
    ["Completion", "Work completed"],
    ["WorkCancellation", "Work cancelled"],
  ]) {
    lines.push(`| ${label} | ${added.filter(transaction => transaction.kind === kind).length} |`);
  }
  lines.push("", `Unique transactions: ${before.transactions.length} at activation; ${current.transactions.length} at conclusion.`);
  if (snapshot.worker) {
    const work = Object.hasOwn(current.work, snapshot.worker.work_id) ? current.work[snapshot.worker.work_id] : "absent";
    const claim = Object.hasOwn(current.claim, snapshot.worker.claim_id) ? current.claim[snapshot.worker.claim_id] : "absent";
    lines.push("", `Assigned worker: work **${work}**; claim **${claim}**.`);
  } else {
    lines.push("", "No worker claim was assigned to this run.");
  }
  lines.push("", "</details>", "");
  return lines.join("\n");
}

async function main(options = {}) {
  const coreApi = options.core || core;
  let summary;
  try {
    const snapshot = loadWorkQueueSnapshot(options.snapshotPath);
    const githubClient = options.githubClient || github;
    const repositoryContext = options.context || context;
    const readLog = options.readWorkQueueLog || options.readCoordinatorLog || readWorkQueueLog;
    const latest = await readLog({
      githubClient,
      owner: repositoryContext.repo.owner,
      repo: repositoryContext.repo.repo,
      publishUpgrades: false,
      core: coreApi,
    });
    summary = renderSummary(snapshot, replayTransactions(latest.transactions));
  } catch (error) {
    coreApi.warning("Work queue activity summary is unavailable; the activation snapshot or current queue could not be read.");
    await coreApi.summary.addRaw("### Work queue activity\n\n<details>\n<summary>Show work queue activity</summary>\n\nActivity is unavailable because the activation snapshot or current queue could not be read.\n\n</details>\n").write();
    throw new Error("Failed to summarize work queue activity", { cause: error });
  }
  await coreApi.summary.addRaw(summary).write();
}

module.exports = { main, renderSummary };
