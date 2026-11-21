// @ts-check
"use strict";

const { sanitizeContent } = require("./sanitize_content.cjs");

const ISSUE_MARKER = /<!-- gh-aw-work-queue-issue:([^\s>]+) -->/;

/** Single-line, sanitized, backtick-free inline text. @param {unknown} value */
function inline(value) {
  return sanitizeContent(
    String(value)
      .replace(/[`\r\n]+/g, " ")
      .trim()
  );
}

/** Marker identifying the Work an Issue tracks (Issue body). @param {string} workId */
function issueMarker(workId) {
  return `<!-- gh-aw-work-queue-issue:${workId} -->`;
}

/** Work id recorded in an Issue body, or undefined. @param {unknown} body */
function markedWorkId(body) {
  return typeof body === "string" ? body.match(ISSUE_MARKER)?.[1] : undefined;
}

/** Idempotence marker of a projection comment. @param {string} workId @param {string} state */
function commentMarker(workId, state) {
  return `<!-- gh-aw-work-queue:${workId}:${state} -->`;
}

/** @param {any} subject */
function subjectLabel(subject) {
  if (!subject) return undefined;
  return `${subject.repository}#${subject.number}`;
}

/** @param {any} work @param {{title_prefix: string}} config */
function issueTitle(work, config) {
  return `${config.title_prefix} ${inline(subjectLabel(work.subject) ?? work.node_key)}`.slice(0, 256);
}

/** @param {any} work @param {string} repository */
function issueBody(work, repository) {
  const lines = ["This Issue tracks work queue Work.", "", `- Pool: \`${inline(work.pool)}\``, `- Graph: \`${inline(work.graph_id)}\``, `- Node: \`${inline(work.node_key)}\``, `- Work: \`${work.work_id}\``];
  if (work.subject) lines.push(`- Subject: ${work.subject.repository}#${work.subject.number}`);
  lines.push("", `[View the work queue ledger](https://github.com/${repository}/blob/work-queue/work-queue.jsonl)`, "", issueMarker(work.work_id));
  return lines.join("\n");
}

/** @param {any} work @param {any} claim @param {any} dispatch @param {string} repository */
function claimComment(work, claim, dispatch, repository) {
  const run = `https://github.com/${repository}/actions/runs/${dispatch.run.run_id}/attempts/${dispatch.run.run_attempt}`;
  return [`Claimed by dispatch \`${dispatch.dispatch_id}\` (attempt ${work.attempts + 1}); worker [run ${dispatch.run.run_id}](${run}).`, "", commentMarker(work.work_id, `claim-${claim.claim_id}`)].join("\n");
}

/** @param {any} work */
function terminalComment(work) {
  const outcome = work.state === "completed" ? "Work completed." : `Work cancelled (\`${work.cancellation_reason}\`).`;
  return [outcome, "", commentMarker(work.work_id, work.state)].join("\n");
}

/** @param {string} workId */
function orphanComment(workId) {
  return ["This Issue tracks Work that is no longer in the work queue ledger; closing it.", "", commentMarker(workId, "orphaned")].join("\n");
}

module.exports = { claimComment, commentMarker, issueBody, issueMarker, issueTitle, markedWorkId, orphanComment, terminalComment };
