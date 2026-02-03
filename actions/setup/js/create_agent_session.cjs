// @ts-check
/// <reference types="@actions/github-script" />

const { getErrorMessage } = require("./error_helpers.cjs");

const fs = require("fs");
const path = require("path");

async function main() {
  // Initialize outputs to empty strings to ensure they're always set
  core.setOutput("session_number", "");
  core.setOutput("session_url", "");

  const isStaged = process.env.GITHUB_AW_SAFE_OUTPUTS_STAGED === "true";
  const agentOutputFile = process.env.GH_AW_AGENT_OUTPUT;
  if (!agentOutputFile) {
    core.info("No GH_AW_AGENT_OUTPUT environment variable found");
    return;
  }

  // Read agent output from file
  let outputContent;
  try {
    outputContent = fs.readFileSync(agentOutputFile, "utf8");
  } catch (error) {
    core.setFailed(`Error reading agent output file: ${getErrorMessage(error)}`);
    return;
  }

  if (outputContent.trim() === "") {
    core.info("Agent output content is empty");
    return;
  }
  core.info(`Agent output content length: ${outputContent.length}`);

  let validatedOutput;
  try {
    validatedOutput = JSON.parse(outputContent);
  } catch (error) {
    core.setFailed(`Error parsing agent output JSON: ${getErrorMessage(error)}`);
    return;
  }

  if (!validatedOutput.items || !Array.isArray(validatedOutput.items)) {
    core.info("No valid items found in agent output");
    return;
  }

  const createAgentSessionItems = validatedOutput.items.filter(item => item.type === "create_agent_session");
  if (createAgentSessionItems.length === 0) {
    core.info("No create-agent-session items found in agent output");
    return;
  }

  core.info(`Found ${createAgentSessionItems.length} create-agent-session item(s)`);

  if (isStaged) {
    let summaryContent = "## 🎭 Staged Mode: Create Agent Sessions Preview\n\n";
    summaryContent += "The following agent sessions would be created if staged mode was disabled:\n\n";

    for (const [index, item] of createAgentSessionItems.entries()) {
      summaryContent += `### Task ${index + 1}\n\n`;
      summaryContent += `**Description:**\n${item.body || "No description provided"}\n\n`;

      const baseBranch = process.env.GITHUB_AW_AGENT_SESSION_BASE || "main";
      summaryContent += `**Base Branch:** ${baseBranch}\n\n`;

      const targetRepo = process.env.GITHUB_AW_TARGET_REPO || process.env.GITHUB_REPOSITORY || "unknown";
      summaryContent += `**Target Repository:** ${targetRepo}\n\n`;

      summaryContent += "---\n\n";
    }

    core.info(summaryContent);
    core.summary.addRaw(summaryContent);
    await core.summary.write();
    return;
  }

  // Get base branch from environment or use current branch
  const baseBranch = process.env.GITHUB_AW_AGENT_SESSION_BASE || process.env.GITHUB_REF_NAME || "main";
  const targetRepo = process.env.GITHUB_AW_TARGET_REPO;

  // Process all agent session items
  const createdTasks = [];
  let summaryContent = "## ✅ Agent Sessions Created\n\n";

  for (const [index, taskItem] of createAgentSessionItems.entries()) {
    const taskDescription = taskItem.body;

    if (!taskDescription || taskDescription.trim() === "") {
      core.warning(`Task ${index + 1}: Agent task description is empty, skipping`);
      continue;
    }

    try {
      // Write task description to a temporary file
      const tmpDir = "/tmp/gh-aw";
      if (!fs.existsSync(tmpDir)) {
        fs.mkdirSync(tmpDir, { recursive: true });
      }

      const taskFile = path.join(tmpDir, `agent-task-description-${index + 1}.md`);
      fs.writeFileSync(taskFile, taskDescription, "utf8");
      core.info(`Task ${index + 1}: Task description written to ${taskFile}`);

      // Build gh agent-task create command
      const ghArgs = ["agent-task", "create", "--from-file", taskFile, "--base", baseBranch];

      if (targetRepo) {
        ghArgs.push("--repo", targetRepo);
      }

      core.info(`Task ${index + 1}: Creating agent session with command: gh ${ghArgs.join(" ")}`);

      // Execute gh agent-task create command
      let taskOutput;
      try {
        taskOutput = await exec.getExecOutput("gh", ghArgs, {
          silent: false,
          ignoreReturnCode: false,
          env: {
            ...process.env,
            GH_TOKEN: process.env.GITHUB_TOKEN || "",
          },
        });
      } catch (execError) {
        const errorMessage = execError instanceof Error ? execError.message : String(execError);

        // Check for authentication/permission errors
        if (errorMessage.includes("authentication") || errorMessage.includes("permission") || errorMessage.includes("forbidden") || errorMessage.includes("401") || errorMessage.includes("403")) {
          core.error(`Task ${index + 1}: Failed to create agent session due to authentication/permission error.`);
          core.error(`The default GITHUB_TOKEN does not have permission to create agent sessions.`);
          core.error(`You must configure a Personal Access Token (PAT) as COPILOT_GITHUB_TOKEN or GH_AW_GITHUB_TOKEN.`);
          core.error(`See documentation: https://github.github.com/gh-aw/reference/safe-outputs/#agent-task-creation-create-agent-session`);
        } else {
          core.error(`Task ${index + 1}: Failed to create agent session: ${errorMessage}`);
        }
        continue;
      }

      // Parse the output to extract task number and URL
      // Expected output format from gh agent-task create is typically:
      // https://github.com/owner/repo/issues/123
      const output = taskOutput.stdout.trim();
      core.info(`Task ${index + 1}: Agent task created: ${output}`);

      // Extract task number from URL
      const urlMatch = output.match(/github\.com\/[^/]+\/[^/]+\/issues\/(\d+)/);
      if (urlMatch) {
        const taskNumber = urlMatch[1];
        createdTasks.push({ number: taskNumber, url: output });

        summaryContent += `### Task ${index + 1}\n\n`;
        summaryContent += `**Task:** [#${taskNumber}](${output})\n\n`;
        summaryContent += `**Base Branch:** ${baseBranch}\n\n`;

        core.info(`✅ Successfully created agent session #${taskNumber}`);
      } else {
        core.warning(`Task ${index + 1}: Could not parse task number from output: ${output}`);
        createdTasks.push({ number: "", url: output });
      }
    } catch (error) {
      core.error(`Task ${index + 1}: Error creating agent session: ${getErrorMessage(error)}`);
    }
  }

  // Set outputs for the first created task (for backward compatibility)
  if (createdTasks.length > 0) {
    core.setOutput("session_number", createdTasks[0].number);
    core.setOutput("session_url", createdTasks[0].url);
  } else {
    core.setFailed("No agent sessions were created");
    return;
  }

  // Write summary
  core.info(summaryContent);
  core.summary.addRaw(summaryContent);
  await core.summary.write();
}

module.exports = { main };
