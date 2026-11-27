// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Setup Threat Detection
 *
 * This module sets up the threat detection analysis by:
 * 1. Checking for existence of artifact files (prompt, agent output, patch)
 * 2. Reading the threat detection prompt template from file
 * 3. Creating a threat detection prompt from the template
 * 4. Writing the prompt to a file for the AI engine to process
 * 5. Adding the rendered prompt to the workflow summary
 */

const fs = require("fs");
const path = require("path");
const { checkFileExists } = require("./file_helpers.cjs");
const { AGENT_OUTPUT_FILENAME } = require("./constants.cjs");
const { ERR_VALIDATION, ERR_SYSTEM } = require("./error_codes.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { getPromptPath } = require("./messages_core.cjs");

/**
 * Marker written in place of the framework-generated `<system>` block that is removed
 * from the analyzed workflow prompt before threat detection reads it.
 */
const SYSTEM_BLOCK_REMOVED_MARKER = "[gh-aw framework system prompt block removed before analysis]";

/**
 * Removes the leading framework-generated `<system>...</system>` block from an agent prompt.
 *
 * The block is trusted only because of its position: gh-aw always emits it as the very first
 * element of the generated prompt file. Any `<system>` markup appearing later in the file is
 * left untouched so that attacker-supplied lookalike blocks remain visible to the analysis.
 *
 * @param {string} content Prompt file content
 * @returns {string|null} The content without the leading system block, or null if there is none
 */
function stripFrameworkSystemBlock(content) {
  const openMatch = /^\s*<system(?:\s[^>]*)?>/i.exec(content);
  if (!openMatch) {
    return null;
  }
  const afterOpen = openMatch[0].length;
  const closeMatch = /<\/\s*system\s*>/i.exec(content.slice(afterOpen));
  if (!closeMatch) {
    return null;
  }
  const endIndex = afterOpen + closeMatch.index + closeMatch[0].length;
  return `${SYSTEM_BLOCK_REMOVED_MARKER}\n${content.slice(endIndex).replace(/^\s*\n/, "")}`;
}

/**
 * Main entry point for setting up threat detection
 * @returns {Promise<void>}
 */
async function main() {
  core.setOutput("ready", "false");
  const continueOnError = (process.env.GH_AW_DETECTION_CONTINUE_ON_ERROR || "true").toLowerCase() !== "false";

  // Read the threat detection template from file
  const templatePath = getPromptPath("threat_detection.md");
  if (!fs.existsSync(templatePath)) {
    core.setFailed(`${ERR_VALIDATION}: Threat detection template not found at: ${templatePath}`);
    return;
  }
  let templateContent;
  try {
    templateContent = fs.readFileSync(templatePath, "utf-8");
  } catch (err) {
    throw new Error(`${ERR_SYSTEM}: Failed to read file ${templatePath}: ${getErrorMessage(err)}`, { cause: err });
  }
  // Check if prompt file exists (soft check; detection can continue with fallback context)
  // The agent artifact is downloaded to /tmp/gh-aw/threat-detection/
  // GitHub Actions preserves the directory structure from the uploaded artifact
  // (stripping the common /tmp/gh-aw/ prefix from the uploaded paths)
  // So /tmp/gh-aw/aw-prompts/prompt.txt becomes /tmp/gh-aw/threat-detection/aw-prompts/prompt.txt
  const threatDetectionDir = "/tmp/gh-aw/threat-detection";
  const promptPath = path.join(threatDetectionDir, "aw-prompts/prompt.txt");
  let promptFileInfo;
  if (!fs.existsSync(promptPath)) {
    promptFileInfo = `${promptPath} (unavailable)`;
    core.warning(`${ERR_VALIDATION}: Missing workflow prompt context at ${promptPath}. ` + "Ensure the agent artifact includes /tmp/gh-aw/aw-prompts/prompt.txt. " + "Threat detection will continue with fallback workflow context.");
  } else {
    let promptStats;
    try {
      promptStats = fs.statSync(promptPath);
    } catch (err) {
      core.warning(`${ERR_VALIDATION}: Failed to inspect workflow prompt context at ${promptPath}: ${getErrorMessage(err)}. ` + "Threat detection will continue with fallback workflow context.");
      promptFileInfo = `${promptPath} (unavailable)`;
      promptStats = null;
    }
    if (!promptStats) {
      // Already recorded fallback warning above.
    } else if (promptStats.size === 0) {
      promptFileInfo = `${promptPath} (unavailable)`;
      core.warning(`${ERR_VALIDATION}: Workflow prompt context is empty at ${promptPath}. ` + "Threat detection will continue with fallback workflow context.");
    } else {
      // Remove gh-aw's own leading <system> block so the detection agent never sees the
      // framework scaffolding (immutable security policy, safe-output instructions) as if it
      // were content produced by the analyzed workflow.
      let promptSize = promptStats.size;
      try {
        const rawPrompt = fs.readFileSync(promptPath, "utf-8");
        const strippedPrompt = stripFrameworkSystemBlock(rawPrompt);
        if (strippedPrompt !== null) {
          fs.writeFileSync(promptPath, strippedPrompt);
          promptSize = Buffer.byteLength(strippedPrompt);
          core.info(`Removed framework system prompt block from ${promptPath}`);
        }
      } catch (err) {
        core.warning(`${ERR_VALIDATION}: Failed to remove framework system prompt block from ${promptPath}: ${getErrorMessage(err)}. Continuing with the original prompt context.`);
      }
      core.info(`Prompt file found: ${promptPath} (${promptSize} bytes)`);
      promptFileInfo = `${promptPath} (${promptSize} bytes)`;
    }
  }

  // Check if agent output file exists
  // The agent-output artifact is also downloaded to /tmp/gh-aw/threat-detection/
  // The artifact contains /tmp/gh-aw/agent_output.json which becomes /tmp/gh-aw/threat-detection/agent_output.json
  const agentOutputPath = path.join(threatDetectionDir, AGENT_OUTPUT_FILENAME);
  if (!checkFileExists(agentOutputPath, threatDetectionDir, "Agent output file", true, continueOnError)) {
    return;
  }

  // Check if patch/bundle file(s) exist
  // Patches are named aw-{branch}.patch (format-patch / git am transport)
  // Bundles are named aw-{branch}.bundle (git bundle transport, preserves merge topology)
  // The agent artifact is downloaded to /tmp/gh-aw/threat-detection/
  const hasPatch = process.env.HAS_PATCH === "true";
  const patchFiles = [];
  try {
    const dirEntries = fs.readdirSync(threatDetectionDir);
    for (const entry of dirEntries) {
      if (/^aw-.+\.(patch|bundle)$/.test(entry)) {
        patchFiles.push(path.join(threatDetectionDir, entry));
      }
    }
  } catch {
    // Directory may not exist or be readable — ignored, no patch files.
  }

  if (patchFiles.length === 0 && hasPatch) {
    if (continueOnError) {
      core.warning(`${ERR_VALIDATION}: Patch/bundle file(s) expected but not found in: ${threatDetectionDir}. Continuing because GH_AW_DETECTION_CONTINUE_ON_ERROR=true`);
    } else {
      core.setFailed(`${ERR_VALIDATION}: Patch/bundle file(s) expected but not found in: ${threatDetectionDir}`);
    }
    return;
  }

  // Get file info for template replacement
  let agentOutputStats;
  try {
    agentOutputStats = fs.statSync(agentOutputPath);
  } catch (err) {
    core.setFailed(`${ERR_VALIDATION}: Failed to inspect agent output file ${agentOutputPath}: ${getErrorMessage(err)}`);
    return;
  }
  const agentOutputFileInfo = agentOutputPath + " (" + agentOutputStats.size + " bytes)";
  const commentMemoryDir = path.join(threatDetectionDir, "comment-memory");
  let commentMemoryFileInfo = "No comment-memory files found";
  if (fs.existsSync(commentMemoryDir)) {
    try {
      const commentMemoryFiles = fs
        .readdirSync(commentMemoryDir)
        .filter(file => file.endsWith(".md"))
        .map(file => {
          const fullPath = path.join(commentMemoryDir, file);
          const stats = fs.statSync(fullPath);
          return `${fullPath} (${stats.size} bytes)`;
        });
      if (commentMemoryFiles.length > 0) {
        commentMemoryFileInfo = commentMemoryFiles.join("\n");
      }
    } catch (err) {
      core.warning(`${ERR_VALIDATION}: Failed to inspect comment-memory files in ${commentMemoryDir}: ${getErrorMessage(err)}. Continuing without comment-memory file details.`);
    }
  }

  // Build patch/bundle file info for template replacement
  let patchFileInfo = "No patch or bundle file found";
  if (patchFiles.length > 0) {
    patchFileInfo = patchFiles
      .map(p => {
        if (!fs.existsSync(p)) {
          return `${p} (0 bytes, ${p.endsWith(".bundle") ? "git-bundle" : "git-patch"})`;
        }
        let size = 0;
        try {
          size = fs.statSync(p).size;
        } catch (err) {
          core.warning(`${ERR_VALIDATION}: Failed to inspect patch artifact ${p}: ${getErrorMessage(err)}. Reporting size as 0 bytes.`);
        }
        const type = p.endsWith(".bundle") ? "git-bundle" : "git-patch";
        return `${p} (${size} bytes, ${type})`;
      })
      .join("\n");
  }

  // Create threat detection prompt with embedded template
  let promptContent = templateContent
    .replace(/{WORKFLOW_NAME}/g, process.env.WORKFLOW_NAME || "Unnamed Workflow")
    .replace(/{WORKFLOW_DESCRIPTION}/g, process.env.WORKFLOW_DESCRIPTION || "No description provided")
    .replace(/{WORKFLOW_PROMPT_FILE}/g, promptFileInfo)
    .replace(/{AGENT_OUTPUT_FILE}/g, agentOutputFileInfo)
    .replace(/{COMMENT_MEMORY_FILES}/g, commentMemoryFileInfo)
    .replace(/{AGENT_PATCH_FILE}/g, patchFileInfo);

  // Append custom prompt instructions if provided
  const customPrompt = process.env.CUSTOM_PROMPT;
  if (customPrompt) {
    promptContent += "\n\n## Additional Instructions\n\n" + customPrompt;
  }

  // Write prompt file
  try {
    fs.mkdirSync("/tmp/gh-aw/aw-prompts", { recursive: true });
    fs.writeFileSync("/tmp/gh-aw/aw-prompts/prompt.txt", promptContent);
  } catch (err) {
    throw new Error(`${ERR_SYSTEM}: Failed to prepare threat detection prompt file: ${getErrorMessage(err)}`, { cause: err });
  }
  core.exportVariable("GH_AW_PROMPT", "/tmp/gh-aw/aw-prompts/prompt.txt");

  // Note: creation of /tmp/gh-aw/threat-detection and detection.log is handled by a separate shell step

  // Write rendered prompt to step summary using HTML details/summary.
  // The bundled detector renders its own prompt; don't display the unused inline prompt.
  const skipPromptSummary = (process.env.GH_AW_DETECTION_SKIP_PROMPT_SUMMARY || "").toLowerCase() === "true";
  if (skipPromptSummary) {
    core.info("Skipping inline prompt summary: the bundled detector renders its own prompt");
  } else {
    await core.summary.addRaw("<details>\n<summary>Threat Detection Prompt</summary>\n\n" + "``````markdown\n" + promptContent + "\n" + "``````\n\n</details>\n").write();
  }

  core.setOutput("ready", "true");
  core.info("Threat detection setup completed");
}

module.exports = { main, stripFrameworkSystemBlock };
