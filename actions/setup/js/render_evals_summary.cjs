// @ts-check
/// <reference types="@actions/github-script" />

/**
 * render_evals_summary — BinEval results step-summary renderer.
 *
 * Reads the redacted evals.jsonl written by run_evals (parse phase) and
 * renders the results as a collapsible <details> section in the GitHub
 * Actions step summary. When run_evals wrote the run-level not-evaluated
 * marker (evals_status.json) instead, it explains that evals did not run or
 * were not completed, with the recorded reason, rather than an answer table.
 *
 * This step runs AFTER redact_secrets so that no credentials appear in the
 * published summary. It is a no-op when neither file exists.
 */

"use strict";

const { readJsonlFile } = require("./jsonl_helpers.cjs");
const fs = require("fs");
const { EVALS_OUTPUT_PATH } = require("./evals_constants.cjs");
const { EVALS_REASON_CODES, readEvalsNotEvaluatedStatus } = require("./evals_status.cjs");
const { buildStepSummaryDetailsSection } = require("./log_parser_step_summary_builder.cjs");

/**
 * Reads the run-level not-evaluated marker, if run_evals wrote one.
 * @returns {import("./evals_status.cjs").EvalsNotEvaluatedStatus | null}
 */
function readEvalsStatus() {
  return readEvalsNotEvaluatedStatus();
}

/**
 * Reads and parses evals.jsonl records.
 * Returns an empty array when the file is absent or unparseable, or when the
 * run is marked not evaluated (the marker never yields answer records).
 * @returns {Array<{id: string, question: string, answer: string, model: string, timestamp: string}>}
 */
function readEvalsResults() {
  if (!fs.existsSync(EVALS_OUTPUT_PATH) || readEvalsStatus()) {
    return [];
  }

  const results = [];
  try {
    for (const record of readJsonlFile(EVALS_OUTPUT_PATH)) {
      if (record && typeof record === "object") {
        results.push({
          id: String(record.id ?? ""),
          question: String(record.question ?? ""),
          answer: String(record.answer ?? "UNKNOWN")
            .trim()
            .toUpperCase(),
          model: String(record.model ?? ""),
          timestamp: String(record.timestamp ?? ""),
        });
      }
    }
  } catch {
    return [];
  }

  return results;
}

/**
 * Builds the markdown body for the evals <details> section.
 * When a not-evaluated status is supplied, the body explains why no answers
 * were recorded instead of rendering an answer table.
 * @param {Array<{id: string, question: string, answer: string, model: string, timestamp: string}>} results
 * @param {import("./evals_status.cjs").EvalsNotEvaluatedStatus | null} [status]
 * @returns {string}
 */
function buildEvalsBody(results, status = null) {
  if (status) {
    return buildNotEvaluatedBody(status);
  }
  if (results.length === 0) {
    return "";
  }

  const yesCount = results.filter(r => r.answer === "YES").length;
  const noCount = results.filter(r => r.answer === "NO").length;
  const unknownCount = results.filter(r => r.answer === "UNKNOWN").length;

  const lines = [];
  lines.push(`| ID | Question | Answer |`);
  lines.push(`| --- | --- | --- |`);
  for (const r of results) {
    const answerEmoji = r.answer === "YES" ? "✅ YES" : r.answer === "NO" ? "❌ NO" : "❓ UNKNOWN";
    lines.push(`| ${escapeMarkdownCell(r.id)} | ${escapeMarkdownCell(r.question)} | ${answerEmoji} |`);
  }
  lines.push("");
  lines.push(`**YES**: ${yesCount} | **NO**: ${noCount} | **UNKNOWN**: ${unknownCount}`);

  const model = results[0]?.model;
  if (model) {
    lines.push(`**model**: ${escapeMarkdownCell(model)}`);
  }

  return lines.join("\n");
}

/**
 * Builds the explanation for a run whose evals judge did not produce answers.
 * @param {import("./evals_status.cjs").EvalsNotEvaluatedStatus} status
 * @returns {string}
 */
function buildNotEvaluatedBody(status) {
  const didNotRun = status.reason_code === EVALS_REASON_CODES.JUDGE_NOT_STARTED;
  const lines = [];
  lines.push(didNotRun ? "**Evals did not run.** No answers were recorded for this run." : "**Evals were not completed.** No answers were recorded for this run.");
  lines.push("");
  const code = status.reason_code ? ` (${escapeMarkdownText(status.reason_code)})` : "";
  lines.push(`**Reason**${code}: ${escapeMarkdownText(status.reason || "No reason was recorded.")}`);
  const details = [];
  if (status.engine) details.push(`**engine**: ${escapeMarkdownText(status.engine)}`);
  if (status.model) details.push(`**model**: ${escapeMarkdownText(status.model)}`);
  if (details.length > 0) {
    lines.push("");
    lines.push(details.join(" | "));
  }
  return lines.join("\n");
}

/**
 * Escapes free text for inline Markdown inside an HTML <details> element.
 * @param {string} text
 * @returns {string}
 */
function escapeMarkdownText(text) {
  return escapeMarkdownCell(text).replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

/**
 * Escapes a string for use inside a Markdown table cell.
 * @param {string} text
 * @returns {string}
 */
function escapeMarkdownCell(text) {
  return text
    .replace(/[\r\n]/g, " ")
    .replace(/\|/g, "\\|")
    .replace(/`/g, "\\`");
}

/**
 * Main entry point: reads evals results and writes the step summary section.
 * @returns {Promise<void>}
 */
async function main() {
  const status = readEvalsStatus();
  if (status) {
    core.info(`Rendering evals not-evaluated summary (${status.reason_code || "no reason code"})`);
    const markdown = buildStepSummaryDetailsSection("BinEval Results", buildEvalsBody([], status));
    await core.summary.addRaw(markdown).write();
    core.info("BinEval not-evaluated section written to step summary");
    return;
  }

  const results = readEvalsResults();
  if (results.length === 0) {
    core.info("No evals results found; skipping step summary section");
    return;
  }

  core.info(`Rendering evals summary: ${results.length} result(s)`);

  const body = buildEvalsBody(results);
  const markdown = buildStepSummaryDetailsSection("BinEval Results", body);

  await core.summary.addRaw(markdown).write();
  core.info("BinEval results section written to step summary");
}

module.exports = { main, readEvalsResults, readEvalsStatus, buildEvalsBody };
