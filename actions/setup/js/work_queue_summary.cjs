// @ts-check
"use strict";

const { loadWorkQueueSnapshot } = require("./work_queue_mcp_server.cjs");
const { readWorkQueueLog } = require("./work_queue_store.cjs");
const { renderWorkQueue } = require("./work_queue_summary_renderer.cjs");
const { OP_FIELDS } = require("./work_queue_replay.cjs");

function countState(states, state) {
  return [...states.values()].filter(value => value.state === state).length;
}

function stateTable(label, before, after) {
  const states = [...new Set([...before.values(), ...after.values()].map(value => value.state))].sort();
  return [`| ${label} state | At activation | At conclusion |`, "| --- | ---: | ---: |", ...states.map(state => `| ${state.replace(/[^a-z_]/g, "")} | ${countState(before, state)} | ${countState(after, state)} |`)];
}

/** Queue activity between the activation snapshot and the conclusion read. */
function renderSummary(snapshot, current) {
  const before = snapshot.projection;
  const added = current.transactions.filter(commit => !before.requests.has(commit.request.id));
  const operations = added.flatMap(commit => commit.operations);
  const open = [...current.dispatches.values()].filter(dispatch => dispatch.state !== "closed");
  const unbound = open.filter(dispatch => dispatch.state !== "bound");
  const lines = [
    "### Work queue activity",
    "",
    `${current.works.size} Work nodes; ${added.length} new commits since activation; ${open.length} open dispatches (${unbound.length} not yet bound to a run).`,
    "",
    "<details>",
    "<summary>Show queue decisions and Claim outcomes</summary>",
    "",
    "The activation view is immutable and may be stale. Conclusion reads the shared ledger; concurrent runs may account for some changes. Staged intents are not commits.",
    "",
    ...stateTable("Work", before.works, current.works),
    "",
    ...stateTable("Claim", before.claims, current.claims),
    "",
    "| Activity since activation | Operations |",
    "| --- | ---: |",
    ...Object.keys(OP_FIELDS)
      .filter(kind => kind !== "Checkpoint")
      .map(kind => `| ${kind} | ${operations.filter(operation => operation.kind === kind).length} |`),
  ];
  if (snapshot.worker) {
    const members = snapshot.worker.claims;
    const completed = members.filter(member => current.claims.get(member.claim_id)?.state === "completed").length;
    const cancelled = members.filter(member => current.claims.get(member.claim_id)?.state === "cancelled").length;
    const open = members.length - completed - cancelled;
    lines.push("", `Assigned dispatch: ${completed} completed, ${cancelled} cancelled, ${open} open Claims; dispatch **${current.dispatches.get(snapshot.worker.dispatch_id)?.state ?? "unknown"}**.`);
  } else if (snapshot.role === "observer") {
    lines.push("", "This run is a read-only queue observer.");
  } else if (snapshot.role === "worker") {
    lines.push("", "This worker's assignment was already settled at activation; it held no open Claim.");
  } else {
    lines.push("", "This run has no worker assignment.");
  }
  lines.push("", "</details>", "", "", "");
  return lines.join("\n");
}

async function main(options = {}) {
  const coreApi = options.core || core;
  try {
    const snapshot = loadWorkQueueSnapshot(options.snapshotPath);
    const repositoryContext = options.context || context;
    const latest = await (options.readWorkQueueLog || readWorkQueueLog)({ githubClient: options.githubClient || github, owner: repositoryContext.repo.owner, repo: repositoryContext.repo.repo, core: coreApi });
    await coreApi.summary.addRaw(renderSummary(snapshot, latest.state) + renderWorkQueue(latest.state)).write();
  } catch (error) {
    coreApi.warning("Work queue activity is unavailable; the snapshot or the ledger could not be read.");
    await coreApi.summary.addRaw("### Work queue activity\n\n<details>\n<summary>Show queue activity</summary>\n\nActivity is unavailable; no queue state is inferred from an unreadable ledger.\n\n</details>\n\n\n").write();
    throw new Error("Failed to summarize work queue activity", { cause: error });
  }
}

module.exports = { main, renderSummary };
