// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Check if command is the first word in the triggering text
 * This prevents accidental command triggers from words appearing later in content
 * Supports multiple command names - checks if any of them match
 */
async function main() {
  const commandsJSON = process.env.GH_AW_COMMANDS;

  const { getErrorMessage } = require("./error_helpers.cjs");

  if (!commandsJSON) {
    core.setFailed("Configuration error: GH_AW_COMMANDS not specified.");
    return;
  }

  // Parse commands from JSON array
  let commands = [];
  try {
    commands = JSON.parse(commandsJSON);
    if (!Array.isArray(commands)) {
      core.setFailed("Configuration error: GH_AW_COMMANDS must be an array.");
      return;
    }
  } catch (error) {
    core.setFailed(`Configuration error: Failed to parse GH_AW_COMMANDS: ${getErrorMessage(error)}`);
    return;
  }

  if (commands.length === 0) {
    core.setFailed("Configuration error: No commands specified.");
    return;
  }

  // Get the triggering text based on event type
  let text = "";
  const eventName = context.eventName;

  try {
    if (eventName === "issues") {
      text = context.payload.issue?.body || "";
    } else if (eventName === "pull_request") {
      text = context.payload.pull_request?.body || "";
    } else if (eventName === "issue_comment") {
      text = context.payload.comment?.body || "";
    } else if (eventName === "pull_request_review_comment") {
      text = context.payload.comment?.body || "";
    } else if (eventName === "discussion") {
      text = context.payload.discussion?.body || "";
    } else if (eventName === "discussion_comment") {
      text = context.payload.comment?.body || "";
    } else {
      // For non-comment events, pass the check
      core.info(`Event ${eventName} does not require command position check`);
      core.setOutput("command_position_ok", "true");
      core.setOutput("matched_command", "");
      return;
    }

    // Normalize whitespace and get the first word
    const trimmedText = text.trim();
    const firstWord = trimmedText.split(/\s+/)[0];

    core.info(`Checking command position. First word in text: ${firstWord}`);
    core.info(`Looking for commands: ${commands.map(c => `/${c}`).join(", ")}`);

    // Check if any of the commands match
    let matchedCommand = null;
    for (const command of commands) {
      const expectedCommand = `/${command}`;

      if (firstWord === expectedCommand) {
        matchedCommand = command;
        break;
      }
    }

    if (matchedCommand) {
      core.info(`✓ Command '/${matchedCommand}' matched at the start of the text`);
      core.setOutput("command_position_ok", "true");
      core.setOutput("matched_command", matchedCommand);
    } else {
      const expectedCommands = commands.map(c => `/${c}`).join(", ");
      core.warning(`⚠️ None of the commands [${expectedCommands}] matched the first word (found: '${firstWord}'). Workflow will be skipped.`);
      core.setOutput("command_position_ok", "false");
      core.setOutput("matched_command", "");
    }
  } catch (error) {
    core.setFailed(getErrorMessage(error));
  }
}

module.exports = { main };
