// @ts-check
"use strict";

const { BUILT_IN_PATTERNS } = require("./redact_secrets.cjs");

const MAX_SUMMARY_ROWS = 32;
const MAX_SUMMARY_BYTES = 32 * 1024;
const MAX_UPDATE_SUMMARIES = 8;
const updateCounts = new WeakMap();

/** @typedef {{summary?: {addRaw(content: string): {write(): Promise<unknown>}}, warning?: (message: string) => void}} SummaryCore */

function code(value, limit = 48) {
  let text = String(value);
  for (const { pattern } of BUILT_IN_PATTERNS) text = text.replace(pattern, "[redacted]");
  text = text.replace(/[\u0000-\u001f\u007f-\u009f\u061c\u200b-\u200f\u2028-\u202e\u2060-\u2069\ufeff]/g, " ");
  const characters = [...text];
  if (characters.length > limit) text = characters.slice(0, limit).join("") + "...";
  const escaped = text.replace(/[&<>"'`|]/g, character => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;", "`": "&#96;", "|": "&#124;" })[character]);
  return `<code>${escaped}</code>`;
}

function focusedWorks(state, commit) {
  const selected = new Map();
  const select = id => {
    if (selected.size < MAX_SUMMARY_ROWS && state.works.has(id)) selected.set(id, state.works.get(id));
  };
  for (const operation of commit?.operations || []) {
    if (selected.size >= MAX_SUMMARY_ROWS) break;
    if (operation.work_id) select(operation.work_id);
    if (operation.dispatch_id) {
      const dispatch = state.dispatches.get(operation.dispatch_id);
      if (dispatch) for (const member of dispatch.claims) select(member.work_id);
    }
  }
  for (const work of state.works.values()) {
    if (selected.size >= MAX_SUMMARY_ROWS) break;
    if (work.state !== "cancelled" && (work.state !== "completed" || work.barrier !== "verified")) select(work.work_id);
  }
  for (const work of state.works.values()) {
    if (selected.size >= MAX_SUMMARY_ROWS) break;
    select(work.work_id);
  }
  return selected;
}

function dependencies(work) {
  if (!work.depends_on.length) return "none";
  const shown = work.depends_on.slice(0, 4).map(edge => (edge.kind === "work" ? code(edge.work_id, 16) : `${code(edge.kind)} gate`));
  if (work.depends_on.length > shown.length) shown.push(`(+${work.depends_on.length - shown.length} more)`);
  return shown.join(", ");
}

/**
 * Render only metadata from a validated projection, never Work payloads or receipts.
 * @param {{request: {kind: string}, id: string, operations: Array<Record<string, unknown>>} | undefined} [commit]
 */
function renderWorkQueue(state, commit = undefined) {
  const works = focusedWorks(state, commit);
  let reservations = 0;
  let unbound = 0;
  let open = 0;
  let pending = 0;
  for (const dispatch of state.dispatches.values()) {
    if (dispatch.released) continue;
    reservations++;
    if (!dispatch.run) unbound++;
  }
  for (const claim of state.claims.values()) if (claim.state === "open") open++;
  for (const work of state.works.values()) if (work.barrier === "pending") pending++;
  const lines = [
    commit?.request.kind === "submit" ? "### Work queue admission" : "### Work queue state",
    "",
    `${state.works.size} Work nodes; ${open} open Claims; ${pending} pending delivery barriers; ${reservations} native reservations (${unbound} unbound).`,
    "",
    state.policy ? "This is committed ledger state, not a scheduling grant. Available Work may still be blocked by dependencies, backoff, scope, or capacity." : "The queue has no installed Policy; this view is uninitialized.",
    ...(commit ? ["", `Checked update: ${code(commit.request.kind)} at ${code(commit.id, 24)}.`] : []),
    "",
    "<details>",
    "<summary>Show Work nodes and Claim ownership</summary>",
    "",
    "Updated nodes are shown first, followed by other unsettled Work. Identifiers are shortened for display; table order is not scheduler order. Concurrent writers can advance the ledger after this view.",
    "",
    "| Work / node | Profile | Priority | Accounting key | Work state | Delivery barrier | Predecessors |",
    "| --- | --- | ---: | --- | --- | --- | --- |",
  ];
  let bytes = Buffer.byteLength(lines.join("\n"), "utf8");
  function row(line) {
    const length = Buffer.byteLength(line + "\n", "utf8");
    if (bytes + length > MAX_SUMMARY_BYTES - 2048) return false;
    lines.push(line);
    bytes += length;
    return true;
  }
  let shownWorks = 0;
  for (const work of works.values()) {
    const key = work.fairness_key === "" ? "(default)" : work.fairness_key;
    if (!row(`| ${code(work.work_id, 16)} / ${code(work.node_key)} | ${code(work.worker_profile)} | ${work.effective_priority ?? work.priority} | ${code(key)} | ${code(work.state)} | ${code(work.barrier)} | ${dependencies(work)} |`)) break;
    shownWorks++;
  }
  if (!shownWorks) lines.push("| No Work nodes shown | | | | | | |");
  lines.push("", `Showing ${shownWorks} of ${state.works.size} Work nodes.`, "", "| Dispatch / handle | Claim | Work | Claim state | Delivery barrier | Native reservation |", "| --- | --- | --- | --- | --- | --- |");
  let shownClaims = 0;
  const orderedClaims = function* () {
    for (const current of [true, false]) {
      for (const focused of [true, false]) {
        for (const claim of state.claims.values()) {
          if ((state.works.get(claim.work_id).claim_id === claim.claim_id) === current && works.has(claim.work_id) === focused) yield claim;
        }
      }
    }
  };
  for (const claim of orderedClaims()) {
    if (shownClaims >= MAX_SUMMARY_ROWS) break;
    const work = state.works.get(claim.work_id);
    const dispatch = state.dispatches.get(claim.dispatch_id);
    const barrier = work.claim_id === claim.claim_id ? code(work.barrier) : "not current owner";
    if (
      !row(
        `| ${code(claim.dispatch_id, 16)} / ${code(claim.handle)} | ${code(claim.claim_id, 16)} | ${code(claim.work_id, 16)} | ${code(claim.state)} | ${barrier} | ${code(dispatch.state)} (${dispatch.released ? "released" : "retained"}) |`
      )
    )
      break;
    shownClaims++;
  }
  if (!shownClaims) lines.push("| No Claims shown | | | | | |");
  lines.push("", `Showing ${shownClaims} of ${state.claims.size} Claims.`, "", "Completion is not verified delivery. Only Result opens Work dependency barriers; cancelled Claims cannot authorize effects.", "", "</details>", "");
  return lines.join("\n");
}

/**
 * Summary failures must not change a successful CAS outcome or launch fencing.
 * @param {SummaryCore | undefined} coreApi
 */
async function writeWorkQueueUpdateSummary(coreApi, state, commit) {
  if (!coreApi?.summary) return;
  const count = updateCounts.get(coreApi.summary) || 0;
  if (count > MAX_UPDATE_SUMMARIES) return;
  updateCounts.set(coreApi.summary, count + 1);
  const markdown = count === MAX_UPDATE_SUMMARIES ? "### Work queue updates\n\nFurther intermediate queue views are omitted to bound this step summary. The conclusion summary reads the latest ledger.\n" : renderWorkQueue(state, commit);
  try {
    await coreApi.summary.addRaw(markdown).write();
  } catch (error) {
    const warning = coreApi.warning || console.warn;
    warning("Queue update committed, but its step summary could not be written. Publication and Claim accounting are unchanged.");
  }
}

module.exports = { MAX_SUMMARY_BYTES, MAX_SUMMARY_ROWS, MAX_UPDATE_SUMMARIES, renderWorkQueue, writeWorkQueueUpdateSummary };
