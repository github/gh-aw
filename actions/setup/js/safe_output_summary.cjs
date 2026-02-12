// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Safe Output Summary Generator
 *
 * This module provides functionality to generate step summaries for safe-output messages.
 * Each processed safe-output generates a summary enclosed in a <details> section.
 */

/**
 * Generate a step summary for a single safe-output message
 * @param {Object} options - Summary generation options
 * @param {string} options.type - The safe-output type (e.g., "create_issue", "create_project")
 * @param {number} options.messageIndex - The message index (1-based)
 * @param {boolean} options.success - Whether the message was processed successfully
 * @param {any} options.result - The result from the handler
 * @param {any} options.message - The original message
 * @param {string} [options.error] - Error message if processing failed
 * @returns {string} - Markdown content for the step summary
 */
function generateSafeOutputSummary(options) {
  const { type, messageIndex, success, result, message, error } = options;

  // Format the type for display (e.g., "create_issue" -> "Create Issue")
  const displayType = type
    .split("_")
    .map(word => word.charAt(0).toUpperCase() + word.slice(1))
    .join(" ");

  // Choose emoji and status based on success
  const emoji = success ? "✅" : "❌";
  const status = success ? "Success" : "Failed";

  // Start building the summary
  let summary = `<details>\n<summary>${emoji} ${displayType} - ${status} (Message ${messageIndex})</summary>\n\n`;

  // Add message details
  summary += `### ${displayType}\n\n`;

  if (success && result) {
    // Add result-specific information based on type
    if (result.url) {
      summary += `**URL:** ${result.url}\n\n`;
    }
    if (result.repo && result.number) {
      summary += `**Location:** ${result.repo}#${result.number}\n\n`;
    }
    if (result.projectUrl) {
      summary += `**Project URL:** ${result.projectUrl}\n\n`;
    }
    if (result.temporaryId) {
      summary += `**Temporary ID:** \`${result.temporaryId}\`\n\n`;
    }

    // Add original message details if available
    if (message) {
      if (message.title) {
        summary += `**Title:** ${message.title}\n\n`;
      }
      if (message.body && typeof message.body === "string") {
        // Truncate body if too long
        const maxBodyLength = 500;
        const bodyPreview = message.body.length > maxBodyLength ? message.body.substring(0, maxBodyLength) + "..." : message.body;
        summary += `**Body Preview:**\n\`\`\`\n${bodyPreview}\n\`\`\`\n\n`;
      }
      if (message.labels && Array.isArray(message.labels)) {
        summary += `**Labels:** ${message.labels.join(", ")}\n\n`;
      }
    }
  } else if (error) {
    // Show error information
    summary += `**Error:** ${error}\n\n`;

    // Add original message details for debugging
    if (message) {
      summary += `**Message Details:**\n\`\`\`json\n${JSON.stringify(message, null, 2).substring(0, 1000)}\n\`\`\`\n\n`;
    }
  }

  summary += `</details>\n\n`;

  return summary;
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
  const fs = require("fs");
  const safeOutputsFile = process.env.GH_AW_SAFE_OUTPUTS;
  if (safeOutputsFile && fs.existsSync(safeOutputsFile)) {
    try {
      const rawContent = fs.readFileSync(safeOutputsFile, "utf8");
      if (rawContent.trim()) {
        core.info("📄 Raw safe-output .jsonl content:");
        core.info(rawContent);
      }
    } catch (error) {
      core.debug(`Could not read raw safe-output file: ${error instanceof Error ? error.message : String(error)}`);
    }
  }

  let summaryContent = `## Safe Output Processing Summary\n\n`;
  summaryContent += `Processed ${results.length} safe-output message(s).\n\n`;

  // Generate summary for each result
  for (const result of results) {
    // Skip if this was handled by a standalone step
    if (result.skipped) {
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
    });
  }

  try {
    await core.summary.addRaw(summaryContent).write();
    core.info(`📝 Safe output summaries written to step summary`);
  } catch (error) {
    core.warning(`Failed to write safe output summaries: ${error instanceof Error ? error.message : String(error)}`);
  }
}

module.exports = {
  generateSafeOutputSummary,
  writeSafeOutputSummaries,
};
