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
      if (dispatch) for (const id of dispatch.claims) select(state.claims.get(id)?.work_id);
    }
  }
  for (const work of state.works.values()) {
    if (selected.size >= MAX_SUMMARY_ROWS) break;
    if (work.state === "available" || work.state === "claimed") select(work.work_id);
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
  let active = 0;
  let unbound = 0;
  let open = 0;
  for (const dispatch of state.dispatches.values()) {
    if (dispatch.state === "closed") continue;
    active++;
    if (dispatch.state !== "bound") unbound++;
  }
  for (const claim of state.claims.values()) if (claim.state === "open") open++;
  const lines = [
    "<details>",
    commit?.request.kind === "submit" ? "<summary>Work queue admission</summary>" : "<summary>Work queue state</summary>",
    "",
    `${state.works.size} Work nodes; ${open} open Claims; ${active} active Dispatches (${unbound} not yet bound to a run).`,
    "",
    "This is committed ledger state, not a scheduling grant. Available Work may still be blocked by dependencies or pool capacity.",
    ...(commit ? ["", `Checked update: ${code(commit.request.kind)} at ${code(commit.id, 24)}.`] : []),
    "",
    "### Work nodes and Claim ownership",
    "",
    "Updated nodes are shown first, followed by other unsettled Work. Identifiers are shortened for display; table order is not scheduler order. Concurrent writers can advance the ledger after this view.",
    "",
    "| Work / node | Profile | Priority | Accounting key | Work state | Attempts | Predecessors |",
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
    if (!row(`| ${code(work.work_id, 16)} / ${code(work.node_key)} | ${code(work.worker_profile)} | ${work.priority} | ${code(key)} | ${code(work.state)} | ${work.attempts} | ${dependencies(work)} |`)) break;
    shownWorks++;
  }
  if (!shownWorks) lines.push("| No Work nodes shown | | | | | | |");
  lines.push("", `Showing ${shownWorks} of ${state.works.size} Work nodes.`, "", "| Dispatch / handle | Claim | Work | Claim state | Dispatch state | Run |", "| --- | --- | --- | --- | --- | --- |");
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
    const dispatch = state.dispatches.get(claim.dispatch_id);
    const run = dispatch.run ? `${code(dispatch.run.run_id)} attempt ${dispatch.run.run_attempt}` : "none";
    if (!row(`| ${code(claim.dispatch_id, 16)} / ${code(claim.handle)} | ${code(claim.claim_id, 16)} | ${code(claim.work_id, 16)} | ${code(claim.state)} | ${code(dispatch.state)} | ${run} |`)) break;
    shownClaims++;
  }
  if (!shownClaims) lines.push("| No Claims shown | | | | | |");
  lines.push("", `Showing ${shownClaims} of ${state.claims.size} Claims.`, "", "A Completion records the worker's effects; cancelled Claims cannot authorize effects.", "", "</details>", "", "", "");
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
