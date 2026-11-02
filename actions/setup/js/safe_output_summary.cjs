// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Safe Output Summary Generator
 *
 * This module provides functionality to generate step summaries for safe-output messages.
 * Each processed safe-output generates a summary enclosed in a <details> section.
 */

// @safe-outputs-exempt SEC-005 — read-only step-summary renderer; displays repo/target_repo
// fields from already-validated, already-processed safe-output messages for reporting
// purposes only. Never performs a cross-repo write or derives an API call target from these fields.

const { displayFileContent } = require("./display_file_helpers.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { classifySafeOutputResult, computeSafeOutputsStatus, pickOutcomeField } = require("./safe_outputs_status.cjs");
const ERROR_CODES = require("./error_codes.cjs");
const { redactStepSummaryContent } = require("./redact_secrets.cjs");

/**
 * Error codes that may be rendered verbatim in a step summary.
 * Handler errors are built from caught exception messages, which can embed request
 * URLs, payloads, or credentials, so only the machine-readable code prefix is shown.
 * @type {Set<string>}
 */
const SUMMARY_SAFE_ERROR_CODES = new Set(Object.values(ERROR_CODES));

/** @type {string} Rendered when an error carries no allowlisted code prefix */
const UNCLASSIFIED_ERROR_CODE = "UNCLASSIFIED";

const OUTCOME_DISPLAY = {
  cancelled: { emoji: "🚫", status: "Cancelled" },
  deferred: { emoji: "⏸️", status: "Deferred" },
  skipped: { emoji: "⚠️", status: "Skipped" },
  warning: { emoji: "⚠️", status: "Warning" },
  success: { emoji: "✅", status: "Success" },
  failed: { emoji: "❌", status: "Failed" },
};

/**
 * Reduces an error message to an allowlisted error code so that raw exception text
 * (which may contain secrets) never reaches the step summary.
 * @param {any} error - The error message produced by a safe-output handler
 * @returns {string} An allowlisted error code
 */
function toSummarySafeErrorCode(error) {
  const text = typeof error === "string" ? error : String(error ?? "");
  const match = text.match(/^\s*([A-Z][A-Z0-9_]*)\s*:/);
  if (match && SUMMARY_SAFE_ERROR_CODES.has(match[1])) {
    return match[1];
  }
  return UNCLASSIFIED_ERROR_CODE;
}

/**
 * Result fields that may carry the URL of the entity a handler acted upon,
 * in priority order. Handlers use different field names depending on the
 * entity type, so every known name is checked to keep summaries regular.
 * @type {string[]}
 */
const RESULT_URL_FIELDS = [
  "url",
  "html_url",
  "issue_url",
  "pull_request_url",
  "discussion_url",
  "comment_url",
  "reply_url",
  "review_url",
  "item_url",
  "commit_url",
  "check_run_url",
  "projectUrl",
  "autofixUrl",
  "artifactUrl",
  "run_url",
  "workflow_run_url",
];

/**
 * Message fields that may carry a target URL when a handler result is sparse.
 * @type {string[]}
 */
const MESSAGE_URL_FIELDS = [...RESULT_URL_FIELDS, "project", "project_url"];

/**
 * Result fields that may carry the number of the entity a handler acted upon,
 * in priority order.
 * @type {string[]}
 */
const RESULT_NUMBER_FIELDS = ["number", "issue_number", "pull_request_number", "discussion_number", "pr_number", "issueNumber", "sub_issue_number", "parent_issue_number", "milestone_number"];

/**
 * Result fields that may carry the repository slug (`owner/repo`) of the entity.
 * @type {string[]}
 */
const RESULT_REPO_FIELDS = ["repo", "repoSlug", "repository"];

/**
 * Message fields that may carry a repository slug (`owner/repo`) when a handler
 * result is sparse.
 * @type {string[]}
 */
const MESSAGE_REPO_FIELDS = ["repo", "target_repo", "repoSlug", "repository"];

/**
 * Return the first non-empty value among the given fields of an object.
 * @param {any} obj - Object to inspect
 * @param {string[]} fields - Field names in priority order
 * @returns {any} The first defined, non-null, non-empty value, or undefined
 */
function pickFirstField(obj, fields) {
  if (!obj || typeof obj !== "object") {
    return undefined;
  }
  for (const field of fields) {
    const value = obj[field];
    if (value !== undefined && value !== null && value !== "") {
      return value;
    }
  }
  return undefined;
}

/**
 * Render a markdown link for an entity, falling back to plain text when no
 * usable http(s) URL is available.
 * @param {any} url - Entity URL
 * @param {string} [text] - Link text (defaults to the URL itself)
 * @returns {string|undefined} Markdown link, plain text, or undefined when nothing to show
 */
function formatLink(url, text) {
  const isHttpUrl = typeof url === "string" && /^https?:\/\//.test(url);
  if (isHttpUrl) {
    return `[${text || url}](${url})`;
  }
  return text || undefined;
}

/**
 * Build the display text for an entity reference (e.g. `owner/repo#123` or `#123`).
 * @param {any} repo - Repository slug
 * @param {any} number - Entity number
 * @returns {string|undefined} Display text, or undefined when no number is available
 */
function formatEntityRef(repo, number) {
  if (number === undefined || number === null || number === "") {
    return undefined;
  }
  return `${repo ? `${repo}` : ""}#${number}`;
}

/**
 * Normalize a list of labels, which may be plain strings or GitHub label objects,
 * into a comma-separated list of label names.
 * @param {any} labels - Labels from a result or message
 * @returns {string|undefined} Comma-separated label names, or undefined when empty
 */
function formatLabels(labels) {
  if (!Array.isArray(labels)) {
    return undefined;
  }
  const names = labels
    .map(label => {
      if (typeof label === "string") {
        return label;
      }
      if (label && typeof label === "object" && typeof label.name === "string") {
        return label.name;
      }
      return undefined;
    })
    .filter(name => typeof name === "string" && name.length > 0);
  return names.length > 0 ? names.join(", ") : undefined;
}

/**
 * Render a list of safe detail values as inline code.
 * @param {any} values
 * @returns {string|undefined}
 */
function formatCodeList(values) {
  if (!Array.isArray(values) || values.length === 0) return undefined;
  return values.map(value => `\`${String(value)}\``).join(", ");
}

/**
 * Format a summary-safe diagnostic field without coupling the renderer to a handler.
 * @param {string} key
 * @returns {string}
 */
function formatSafeDetailLabel(key) {
  const aliases = { requiredLabels: "Required", missingLabels: "Missing" };
  return aliases[key] || key.replace(/([a-z0-9])([A-Z])/g, "$1 $2").replace(/^./, character => character.toUpperCase());
}

/**
 * @param {any} value
 * @returns {string|undefined}
 */
function formatSafeDetailValue(value) {
  if (Array.isArray(value)) return formatCodeList(value);
  if (typeof value === "string" || typeof value === "number" || typeof value === "boolean") return `\`${String(value)}\``;
  return undefined;
}

/**
 * Build a canonical entity URL from a repository slug and number when a handler
 * did not report an explicit URL. GitHub redirects `/issues/<n>` to the pull
 * request when the number refers to a pull request, so this works for both.
 * @param {any} repo - Repository slug (`owner/repo`)
 * @param {any} number - Entity number
 * @returns {string|undefined} A canonical URL, or undefined when it cannot be built
 */
function buildEntityUrl(repo, number) {
  if (typeof repo !== "string" || !/^[^/\s]+\/[^/\s]+$/.test(repo)) {
    return undefined;
  }
  const numeric = typeof number === "number" ? number : parseInt(String(number ?? ""), 10);
  if (!Number.isInteger(numeric) || numeric <= 0) {
    return undefined;
  }
  const serverUrl = (process.env.GITHUB_SERVER_URL || "https://github.com").replace(/\/+$/, "");
  return `${serverUrl}/${repo}/issues/${numeric}`;
}

/**
 * Render the entity link line shown for a successfully processed safe output.
 * A single regular `**Target:**` line is emitted so that every safe-output type
 * surfaces a link to the entity it acted upon.
 * @param {any} result - The handler result
 * @param {any} message - The original message
 * @returns {string} Markdown for the target line (may be empty)
 */
function formatTargetLine(result, message) {
  const target = result?.target && typeof result.target === "object" ? result.target : undefined;
  const number = pickFirstField(target, RESULT_NUMBER_FIELDS) ?? pickFirstField(result, RESULT_NUMBER_FIELDS) ?? pickFirstField(message, RESULT_NUMBER_FIELDS);
  const explicitRepo = pickFirstField(target, RESULT_REPO_FIELDS) ?? pickFirstField(result, RESULT_REPO_FIELDS) ?? pickFirstField(message, MESSAGE_REPO_FIELDS);
  const fallbackRepo = explicitRepo ?? process.env.GITHUB_REPOSITORY;
  const explicitUrl = pickFirstField(target, RESULT_URL_FIELDS) || pickFirstField(result, RESULT_URL_FIELDS) || pickFirstField(message, MESSAGE_URL_FIELDS);
  const url = explicitUrl || buildEntityUrl(fallbackRepo, number);
  const link = formatLink(url, formatEntityRef(explicitUrl ? explicitRepo : fallbackRepo, number));
  return link ? `**Target:** ${link}\n\n` : "";
}

/**
 * Render summary-safe diagnostics supplied by a handler.
 * @param {any} result
 * @param {any} message
 * @param {string|undefined} error
 * @param {string} outcome
 * @returns {string}
 */
function formatOutcomeDiagnostics(result, message, error, outcome) {
  let diagnostics = formatTargetLine(result, message);
  const reasonCode = result?.reasonCode || result?.errorCode;
  const reason = result?.reason || (outcome === "warning" ? result?.warning : undefined);
  if (reasonCode) {
    diagnostics += `**Reason Code:** \`${reasonCode}\`\n\n`;
  }
  if (reason) {
    diagnostics += `**Reason:** ${reason}\n\n`;
  }
  const safeDetails = result?.safeDetails && typeof result.safeDetails === "object" ? result.safeDetails : undefined;
  for (const [key, value] of Object.entries(safeDetails || {})) {
    const formattedValue = formatSafeDetailValue(value);
    if (formattedValue) {
      diagnostics += `**${formatSafeDetailLabel(key)}:** ${formattedValue}\n\n`;
    }
  }
  if (!reason && !reasonCode && error) {
    diagnostics += `**Error:** \`${toSummarySafeErrorCode(error)}\` (see the job logs for details)\n\n`;
  }
  return diagnostics;
}

/**
 * @param {string} type
 * @returns {string}
 */
function formatDisplayType(type) {
  return type
    .split("_")
    .map(word => word.charAt(0).toUpperCase() + word.slice(1))
    .join(" ");
}

/**
 * Generate a step summary for a single safe-output message
 * @param {Object} options - Summary generation options
 * @param {string} options.type - The safe-output type (e.g., "create_issue", "create_project")
 * @param {number} options.messageIndex - The message index (1-based)
 * @param {boolean} options.success - Whether the message was processed successfully
 * @param {any} options.result - The result from the handler
 * @param {any} options.message - The original message
 * @param {string} [options.error] - Error message if processing failed
 * @param {boolean} [options.skipped] - Whether the message was skipped
 * @param {boolean} [options.cancelled] - Whether the message was cancelled
 * @param {boolean} [options.deferred] - Whether the message was deferred
 * @param {string} [options.warning] - Warning message if processing produced a warning
 * @returns {string} - Markdown content for the step summary
 */
function generateSafeOutputSummary(options) {
  const { type, messageIndex, success, result, message, error } = options;

  // Format the type for display (e.g., "create_issue" -> "Create Issue")
  const displayType = formatDisplayType(type);

  // Detect fallback outcomes for code-push types.
  // Prefer explicit fallback_type when available; infer only for backward compatibility.
  const isDuplicateDrop = success && result && result.dropped_duplicate === true;
  const isFallback = success && result && result.fallback_used === true;
  const inferredFallbackType = isFallback && (result.pull_request_url || result.pull_request_number != null) ? "pull_request" : "issue";
  const fallbackType = isFallback && result?.fallback_type ? result.fallback_type : inferredFallbackType;

  const outcome = classifySafeOutputResult(options);
  const outcomeDisplay = OUTCOME_DISPLAY[outcome] || OUTCOME_DISPLAY.failed;

  // Choose emoji and status based on normalized outcome and fallback
  const emoji = isDuplicateDrop ? "⚠️" : isFallback ? "⚠️" : outcomeDisplay.emoji;
  const status = isDuplicateDrop ? "Duplicate Dropped" : isFallback ? (fallbackType === "pull_request" ? "Fallback Pull Request Created" : "Fallback Issue Created") : outcomeDisplay.status;

  // Start building the summary
  let summary = `<details>\n<summary>${emoji} ${displayType} - ${status} (Message ${messageIndex})</summary>\n\n`;

  // Add message details
  const sectionTitle = isFallback ? `### ${displayType} — ${fallbackType === "pull_request" ? "Fallback Pull Request" : "Fallback Issue"}\n\n` : `### ${displayType}\n\n`;
  summary += sectionTitle;

  if (isDuplicateDrop) {
    summary += `> ℹ️ Duplicate issue title was dropped by title-based deduplication.\n\n`;
    if (result.title || message?.title) {
      summary += `**Title:** ${result.title || message?.title}\n\n`;
    }
    if (result.duplicate_of_title) {
      summary += `**Matched Existing Title:** ${result.duplicate_of_title}\n\n`;
    }
    if (result.duplicate_distance !== undefined) {
      summary += `**Levenshtein Distance:** ${result.duplicate_distance}\n\n`;
    }
    if (result.dedup_source) {
      summary += `**Dedup Source:** ${result.dedup_source}\n\n`;
    }
  } else if (isFallback) {
    // Explain why the fallback occurred and show the created fallback target
    if (fallbackType === "pull_request") {
      summary += `> ℹ️ Direct push to the original pull request branch was not possible (diverged/non-fast-forward). A fallback pull request was created instead.\n\n`;
      const link = formatLink(result.pull_request_url, formatEntityRef(result.repo, result.pull_request_number));
      if (link) {
        summary += `**Fallback Pull Request:** ${link}\n\n`;
      }
    } else {
      summary += `> ℹ️ Pull request creation was blocked due to protected file changes. A review issue was created instead.\n\n`;
      const link = formatLink(result.issue_url, formatEntityRef(result.repo, result.issue_number));
      if (link) {
        summary += `**Fallback Issue:** ${link}\n\n`;
      }
    }
    if (result.branch_name) {
      summary += `**Branch:** \`${result.branch_name}\`\n\n`;
    }

    // Add original message details if available
    if (message) {
      if (message.title) {
        summary += `**Title:** ${message.title}\n\n`;
      }
    }
  } else if (outcome === "success" && result) {
    // Add a regular link to the entity the handler acted upon
    summary += formatTargetLine(result, message);
    if (result.temporaryId) {
      summary += `**Temporary ID:** \`${result.temporaryId}\`\n\n`;
    }

    // Add original message details if available
    const title = result.title || message?.title;
    if (title) {
      summary += `**Title:** ${title}\n\n`;
    }
    const labels = formatLabels(result.labelsAdded || result.labels || message?.labels);
    if (labels) {
      summary += `**Labels:** ${labels}\n\n`;
    }
  } else if (outcome !== "success") {
    summary += formatOutcomeDiagnostics(result, message, error, outcome);
  }

  if (type === "merge_pull_request" && result?.decision_evidence) {
    const codes = (result.failure_reasons || []).map(reason => reason.code).filter(code => typeof code === "string" && /^[a-z_]{1,80}$/.test(code));
    if (codes.length > 0) {
      summary += `**Blocked Gates:** ${codes.map(code => `\`${code}\``).join(", ")}\n\n`;
    }
    summary += "Decision evidence is recorded in `merge-pull-request-decisions.jsonl` in the safe-outputs-items artifact.\n\n";
  }

  // Display secrecy and integrity security metadata fields if present in the message.
  // secrecy indicates the confidentiality level of the message content.
  // integrity indicates the trustworthiness level of the message source.
  // message.data is intentionally omitted to prevent secret leakage into step summaries.
  if (message) {
    if (message.secrecy !== undefined && message.secrecy !== null) {
      summary += `**Secrecy:** \`${message.secrecy}\`\n\n`;
    }

    if (message.integrity !== undefined && message.integrity !== null) {
      summary += `**Integrity:** \`${message.integrity}\`\n\n`;
    }
  }

  summary += `</details>\n\n`;

  return summary;
}

/**
 * Generate a compact grouped overview table for processed safe-output results.
 * @param {Array<Object>} results
 * @returns {string}
 */
function generateOutcomeOverview(results) {
  const groups = new Map();
  for (const result of results) {
    const outcome = classifySafeOutputResult(result);
    if (outcome === "delegated") continue;
    const handlerResult = result?.result && typeof result.result === "object" ? result.result : {};
    const reason = handlerResult.reason || result.reason || handlerResult.reasonCode || result.reasonCode || handlerResult.errorCode || result.errorCode || (outcome === "failed" ? toSummarySafeErrorCode(result.error) : "");
    const key = `${outcome}\0${result.type}\0${reason}`;
    const current = groups.get(key) || {
      outcome: OUTCOME_DISPLAY[outcome]?.status || outcome,
      type: formatDisplayType(result.type || "unknown"),
      count: 0,
      reason,
    };
    current.count += 1;
    groups.set(key, current);
  }
  if (groups.size === 0) return "";
  let table = "| Outcome | Type | Count | Reason |\n|---|---|---:|---|\n";
  for (const group of groups.values()) {
    const reason = String(group.reason || "")
      .replace(/\\/g, "\\\\")
      .replace(/\|/g, "\\|")
      .replace(/\r?\n/g, "<br>");
    table += `| ${group.outcome} | ${group.type} | ${group.count} | ${reason} |\n`;
  }
  return `${table}\n`;
}

/**
 * Write safe-output summaries to the GitHub Actions step summary
 * @param {Array<Object>} results - Array of processing results
 * @param {Array<Object>} messages - Array of original messages
 * @returns {Promise<void>}
 */
async function writeSafeOutputSummaries(results, messages) {
  if (!results || results.length === 0) {
    return;
  }

  // Log the raw .jsonl content from the safe outputs file
  const safeOutputsFile = process.env.GH_AW_SAFE_OUTPUTS;
  if (safeOutputsFile) {
    const fs = require("fs");
    if (fs.existsSync(safeOutputsFile)) {
      try {
        const content = fs.readFileSync(safeOutputsFile, "utf8");
        if (content.trim()) {
          // Use displayFileContent helper to show file with truncation and collapsible group
          // Pass a filename with .jsonl extension so it's recognized as displayable
          displayFileContent(safeOutputsFile, "safe-outputs.jsonl", 5000);
        }
      } catch (error) {
        core.debug(`Could not read raw safe-output file: ${getErrorMessage(error)}`);
      }
    }
  }

  const status = computeSafeOutputsStatus(results);
  const statusEmoji = status.itemsFailed > 0 ? "❌" : status.itemsSkipped > 0 || status.itemsWarnings > 0 || status.itemsCancelled > 0 || status.itemsDeferred > 0 ? "⚠️" : "✅";

  // Lead with a collapsible section so this block matches the look of the other
  // run-summary sections (e.g. threat detection).
  let summaryContent = `<details>\n<summary>${statusEmoji} Safe Output Processing Summary (${status.itemsApplied} applied, ${status.itemsSkipped} skipped, ${status.itemsFailed} failed)</summary>\n\n`;
  summaryContent += `Processed ${results.length} safe-output message(s).\n\n`;
  summaryContent += `Status: **${status.status}**\n\n`;
  summaryContent += `Applied: **${status.itemsApplied}** · Skipped: **${status.itemsSkipped}** · Warnings: **${status.itemsWarnings}** · Failed: **${status.itemsFailed}** · Cancelled: **${status.itemsCancelled}** · Deferred: **${status.itemsDeferred}**\n\n`;
  summaryContent += generateOutcomeOverview(results);
  if (status.itemsFailed > 0 || status.itemsSkipped > 0 || status.itemsWarnings > 0) {
    const playbookUrl = "https://github.com/github/gh-aw/blob/main/.github/aw/debug-playbook.md#completion-and-safe-output-errors";
    core.info(`Debug playbook: ${playbookUrl}`);
    summaryContent += `[Debug playbook](${playbookUrl})\n\n`;
  }

  // Generate summary for each result
  for (const result of results) {
    // Skip only if this was explicitly delegated to a standalone step or custom safe output job.
    // `result.reason` is set (e.g. "Handled by standalone step") only when processMessages
    // decides that a different step is responsible for the message; it is NOT set when a
    // handler itself returns { success: false, skipped: true } for a handler-side condition
    // (e.g. "no issue fields available"). Handler-returned skips still appear in the summary
    // so their diagnostic signal is preserved without the job failing.
    if (classifySafeOutputResult(result) === "delegated") {
      continue;
    }

    // Get the original message
    const message = messages[result.messageIndex];

    summaryContent += generateSafeOutputSummary({
      type: result.type,
      messageIndex: result.messageIndex + 1, // Convert to 1-based
      success: result.success,
      result: result.result,
      message: message,
      error: result.error,
      skipped: result.skipped,
      cancelled: result.cancelled,
      deferred: result.deferred,
      warning: result.warning,
    });
  }

  summaryContent += `</details>\n\n`;

  try {
    await core.summary.addRaw(redactStepSummaryContent(summaryContent)).write();
    core.info(`📝 Safe output summaries written to step summary`);
  } catch (error) {
    core.warning(`Failed to write safe output summaries: ${getErrorMessage(error)}`);
  }
}

module.exports = {
  generateOutcomeOverview,
  generateSafeOutputSummary,
  writeSafeOutputSummaries,
};
