// @ts-check
"use strict";

const { loadWorkQueueSnapshot } = require("./work_queue_mcp_server.cjs");
const { newState, replayTransactions } = require("./work_queue_replay.cjs");
const { readWorkQueueLog } = require("./work_queue_store.cjs");

function countState(states, state) {
  return [...states.values()].filter(value => value.state === state).length;
}

function stateTable(label, before, after) {
  const states = [...new Set([...before.values(), ...after.values()].map(value => value.state))].sort();
  return [`| ${label} state | At activation | At conclusion |`, "| --- | ---: | ---: |", ...states.map(state => `| ${state.replace(/[^a-z_]/g, "")} | ${countState(before, state)} | ${countState(after, state)} |`)];
}

function renderSummary(snapshot, current) {
  const before = snapshot.projection;
  const baseline = new Set([...before.requests.values()].map(commit => commit.id));
  const added = [...current.requests.values()].filter(commit => !baseline.has(commit.id));
  const operations = added.flatMap(commit => commit.operations);
  const outstanding = [...current.dispatches.values()].filter(dispatch => !dispatch.released);
  const uncertain = outstanding.filter(dispatch => ["started", "uncertain", "unresolved"].includes(dispatch.state) && !dispatch.run);
  const lines = [
    "### Work queue activity",
    "",
    `${current.works.size} Work nodes; ${added.length} new checked commits since activation; ${outstanding.length} outstanding native reservations (${uncertain.length} unbound or unresolved).`,
    "",
    "<details>",
    "<summary>Show queue decisions and independent Claim outcomes</summary>",
    "",
    "The activation view is immutable and may be stale. Conclusion reads the shared ledger; concurrent runs may account for some changes. Staged intents are not durable grants or completions.",
    "",
    ...stateTable("Work", before.works, current.works),
    "",
    ...stateTable("Claim", before.claims, current.claims),
    "",
    "| Activity since activation | Operations |",
    "| --- | ---: |",
  ];
  for (const kind of ["Work", "Claim", "Completion", "ClaimCancellation", "Result", "DeliveryFailure", "Observation", "Dispatch", "Release", "WorkCancellation", "Policy", "Control"])
    lines.push(`| ${kind} | ${operations.filter(operation => operation.kind === kind).length} |`);
  if (snapshot.worker) {
    const members = snapshot.worker.claims;
    const completed = members.filter(member => current.claims.get(member.claim_id)?.state === "completed").length;
    const cancelled = members.filter(member => current.claims.get(member.claim_id)?.state === "cancelled").length;
    const unsettled = members.length - completed - cancelled;
    const outcome = unsettled ? "pending" : completed && cancelled ? "completed_with_cancellations" : completed ? "completed" : "cancelled";
    const dispatch = current.dispatches.get(snapshot.worker.dispatch_id);
    lines.push("", `Assigned group: **${outcome}**; ${completed} completed, ${cancelled} cancelled, ${unsettled} unsettled Claims. Native reservation: **${dispatch?.released ? "released" : "retained"}**.`);
    lines.push("Completion does not imply verified delivery. Results and delivery failures settle per Claim; completed sibling effects are not replayed by launch recovery.");
  } else if (snapshot.role === "observer") {
    lines.push("", "This run is a read-only queue observer. Queue mutations and worker Claim effects are unavailable; ordinary configured outputs retain their normal authorization.");
  } else {
    lines.push("", "This run has no worker assignment. Queue-control intents confer no worker effect authority.");
  }
  lines.push("", "</details>", "");
  return lines.join("\n");
}

async function main(options = {}) {
  const coreApi = options.core || core;
  try {
    const snapshot = loadWorkQueueSnapshot(options.snapshotPath);
    const githubClient = options.githubClient || github;
    const repositoryContext = options.context || context;
    const latest = await (options.readWorkQueueLog || readWorkQueueLog)({ githubClient, owner: repositoryContext.repo.owner, repo: repositoryContext.repo.repo, core: coreApi });
    const current = snapshot.role === "observer" && snapshot.sha === null && latest.sha === null && latest.transactions.length === 0 ? newState() : replayTransactions(latest.transactions);
    await coreApi.summary.addRaw(renderSummary(snapshot, current)).write();
  } catch (error) {
    coreApi.warning("Work queue activity is unavailable; snapshot or authoritative ledger validation failed.");
    await coreApi.summary.addRaw("### Work queue activity\n\n<details>\n<summary>Show queue activity</summary>\n\nActivity is unavailable; no successful queue state is inferred from an unreadable ledger.\n\n</details>\n").write();
    throw new Error("Failed to summarize work queue activity", { cause: error });
  }
}

module.exports = { main, renderSummary };
