// @ts-check
/// <reference types="@actions/github-script" />

const { loadAgentOutput } = require("./load_agent_output.cjs");
const { safeInfo } = require("./sanitized_logging.cjs");

/**
 * Main function to handle noop safe output
 * No-op is a fallback output type that logs messages for transparency
 * without taking any GitHub API actions
 */
async function main() {
  // Check if we're in staged mode
  const isStaged = process.env.GH_AW_SAFE_OUTPUTS_STAGED === "true";

  const result = loadAgentOutput();
  if (!result.success) {
    return;
  }

  // Find all noop items
  const noopItems = result.items.filter(/** @param {any} item */ item => item.type === "noop");
  if (noopItems.length === 0) {
    core.info("No noop items found in agent output");
    return;
  }

  core.info(`Found ${noopItems.length} noop item(s)`);

  // If in staged mode, emit step summary instead of logging
  if (isStaged) {
    let summaryContent = "## 🎭 Staged Mode: No-Op Messages Preview\n\n";
    summaryContent += "The following messages would be logged if staged mode was disabled:\n\n";

    for (let i = 0; i < noopItems.length; i++) {
      const item = noopItems[i];
      summaryContent += `### Message ${i + 1}\n`;
      summaryContent += `${item.message}\n\n`;
      summaryContent += "---\n\n";
    }

    await core.summary.addRaw(summaryContent).write();
    core.info("📝 No-op message preview written to step summary");
    return;
  }

  // Process each noop item - just log the messages for transparency
  let summaryContent = "\n\n## No-Op Messages\n\n";
  summaryContent += "The following messages were logged for transparency:\n\n";

  for (let i = 0; i < noopItems.length; i++) {
    const item = noopItems[i];
    safeInfo(`No-op message ${i + 1}: ${item.message}`);
    summaryContent += `- ${item.message}\n`;
  }

  // Write summary for all noop messages
  await core.summary.addRaw(summaryContent).write();

  // Export the first noop message for use in add-comment default reporting
  if (noopItems.length > 0) {
    core.setOutput("noop_message", noopItems[0].message);
    core.exportVariable("GH_AW_NOOP_MESSAGE", noopItems[0].message);
  }

  core.info(`Successfully processed ${noopItems.length} noop message(s)`);
}

module.exports = { main };
