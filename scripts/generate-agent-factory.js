#!/usr/bin/env node

/**
 * Agent Factory Page Generator
 *
 * Generates a markdown documentation page with GitHub Actions status badges
 * for all workflows in the repository (only from .lock.yml files).
 * Displays workflows in a table with columns for name, agent, status, and workflow link.
 *
 * Usage:
 *   node scripts/generate-agent-factory.js
 */

import fs from "fs";
import path from "path";
import { fileURLToPath } from "url";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

// Paths
const WORKFLOWS_DIR = path.join(__dirname, "../.github/workflows");
const OUTPUT_PATH = path.join(__dirname, "../docs/src/content/docs/agent-factory-status.mdx");

// Repository owner and name
const REPO_OWNER = "github";
const REPO_NAME = "gh-aw";

/**
 * Extract workflow name and filename from a lock file
 * Uses simple regex parsing instead of YAML parser
 */
function extractWorkflowInfo(filePath) {
  try {
    const content = fs.readFileSync(filePath, "utf-8");

    // Match the name field in YAML format
    // Handles both quoted and unquoted values:
    // name: "My Workflow"
    // name: 'My Workflow'
    // name: My Workflow
    const nameMatch = content.match(/^name:\s*["']?([^"'\n]+?)["']?\s*$/m);

    if (!nameMatch) {
      return null;
    }

    const workflowName = nameMatch[1].trim();
    const filename = path.basename(filePath);

    return {
      name: workflowName,
      filename: filename,
      badgeUrl: `https://github.com/${REPO_OWNER}/${REPO_NAME}/actions/workflows/${filename}/badge.svg`,
      workflowUrl: `https://github.com/${REPO_OWNER}/${REPO_NAME}/actions/workflows/${filename}`,
    };
  } catch (error) {
    console.error(`Error parsing ${filePath}:`, error.message);
    return null;
  }
}

/**
 * Extract engine type from a markdown workflow file
 * Returns 'copilot', 'claude', 'codex', 'custom', or 'copilot' (default)
 */
function extractEngineFromMarkdown(mdFilePath) {
  try {
    if (!fs.existsSync(mdFilePath)) {
      return "copilot"; // Default engine
    }

    const content = fs.readFileSync(mdFilePath, "utf-8");

    // Look for engine field in frontmatter
    // Handles both simple string format and object format:
    // engine: copilot
    // engine: "claude"
    // engine:
    //   id: codex
    const engineStringMatch = content.match(/^engine:\s*["']?(\w+)["']?\s*$/m);
    if (engineStringMatch) {
      return engineStringMatch[1].toLowerCase();
    }

    // Check for object format with 'id' field
    const engineObjectMatch = content.match(/^engine:\s*\n\s+id:\s*["']?(\w+)["']?\s*$/m);
    if (engineObjectMatch) {
      return engineObjectMatch[1].toLowerCase();
    }

    return "copilot"; // Default engine
  } catch (error) {
    console.error(`Error extracting engine from ${mdFilePath}:`, error.message);
    return "copilot"; // Default engine
  }
}

/**
 * Extract schedule from a markdown workflow file
 * Returns the cron schedule or null if not scheduled
 */
function extractScheduleFromMarkdown(mdFilePath) {
  try {
    if (!fs.existsSync(mdFilePath)) {
      return null;
    }

    const content = fs.readFileSync(mdFilePath, "utf-8");

    // Look for schedule field with cron in frontmatter
    // schedule:
    //   - cron: "0 0 * * *"
    const cronMatch = content.match(/^\s+[-\s]*cron:\s*["']([^"'\n]+)["']/m);
    if (cronMatch) {
      return cronMatch[1];
    }

    return null;
  } catch (error) {
    console.error(`Error extracting schedule from ${mdFilePath}:`, error.message);
    return null;
  }
}

/**
 * Extract command name from a markdown workflow file
 * Returns the command name if it exists (e.g., "brave", "plan", "summarize")
 */
function extractCommandFromMarkdown(mdFilePath) {
  try {
    if (!fs.existsSync(mdFilePath)) {
      return null;
    }

    const content = fs.readFileSync(mdFilePath, "utf-8");

    // Look for command.name in frontmatter (both command and slash_command)
    // on:
    //   command:
    //     name: brave
    // or
    // on:
    //   slash_command:
    //     name: brave
    const commandMatch = content.match(/^  (?:slash_)?command:\s*\n\s+name:\s*["']?(\w+)["']?/m);
    if (commandMatch) {
      return commandMatch[1];
    }

    return null;
  } catch (error) {
    console.error(`Error extracting command from ${mdFilePath}:`, error.message);
    return null;
  }
}

/**
 * Generate the markdown documentation
 */
function generateMarkdown(workflows) {
  const lines = [];

  // Frontmatter
  lines.push("---");
  lines.push("title: Agent Factory");
  lines.push("description: Experimental agentic workflows used by the team to learn and build.");
  lines.push("sidebar:");
  lines.push("  order: 1000");
  lines.push("---");
  lines.push("");

  // Introduction
  lines.push("These are experimental agentic workflows used by the GitHub Next team to learn, build, and use agentic workflows. [Browse source files](https://github.com/github/gh-aw/tree/main/.github/workflows).");
  lines.push("");

  // Sort workflows alphabetically by name
  workflows.sort((a, b) => a.name.localeCompare(b.name));

  // Generate table header with pretty formatting
  lines.push("| Workflow | Agent | Status | Schedule | Command |");
  lines.push("|:---------|:-----:|:------:|:--------:|:-------:|");

  // Generate table rows
  for (const workflow of workflows) {
    const agent = workflow.engine || "copilot";
    const statusBadge = `[![${workflow.name}](${workflow.badgeUrl})](${workflow.workflowUrl})`;

    // Consolidate name and source link
    const workflowNameWithLink = workflow.mdFilename ? `[${workflow.name}](https://github.com/${REPO_OWNER}/${REPO_NAME}/blob/main/.github/workflows/${workflow.mdFilename})` : workflow.name;

    // Format schedule - show cron or "-"
    const schedule = workflow.schedule ? `\`${workflow.schedule}\`` : "-";

    // Format command - show /command or "-"
    const command = workflow.command ? `\`/${workflow.command}\`` : "-";

    lines.push(`| ${workflowNameWithLink} | ${agent} | ${statusBadge} | ${schedule} | ${command} |`);
  }

  lines.push("");
  lines.push(":::note");
  lines.push("Badges update automatically. Click badges for run details or workflow names for source files.");
  lines.push(":::");
  lines.push("");

  return lines.join("\n");
}

// Main execution
console.log("Generating agent factory documentation...");

// Read all .lock.yml files
const lockFiles = fs
  .readdirSync(WORKFLOWS_DIR)
  .filter(file => file.endsWith(".lock.yml"))
  .map(file => path.join(WORKFLOWS_DIR, file));

console.log(`Found ${lockFiles.length} lock files`);

// Extract workflow information and match with markdown files
const workflows = lockFiles
  .map(lockFilePath => {
    const workflowInfo = extractWorkflowInfo(lockFilePath);
    if (!workflowInfo) {
      return null;
    }

    // Try to find corresponding .md file
    // Convert "workflow-name.lock.yml" to "workflow-name.md"
    const mdFilename = workflowInfo.filename.replace(".lock.yml", ".md");
    const mdFilePath = path.join(WORKFLOWS_DIR, mdFilename);

    // Extract all workflow metadata from markdown file
    const engine = extractEngineFromMarkdown(mdFilePath);
    const schedule = extractScheduleFromMarkdown(mdFilePath);
    const command = extractCommandFromMarkdown(mdFilePath);

    return {
      ...workflowInfo,
      engine: engine,
      schedule: schedule,
      command: command,
      mdFilename: fs.existsSync(mdFilePath) ? mdFilename : null,
    };
  })
  .filter(info => info !== null);

console.log(`Extracted ${workflows.length} workflows with valid names`);

// Generate the markdown
const markdown = generateMarkdown(workflows);

// Ensure output directory exists
const outputDir = path.dirname(OUTPUT_PATH);
if (!fs.existsSync(outputDir)) {
  fs.mkdirSync(outputDir, { recursive: true });
}

// Write the output
fs.writeFileSync(OUTPUT_PATH, markdown, "utf-8");
console.log(`✓ Generated agent factory documentation: ${OUTPUT_PATH}`);
console.log(`✓ Total workflows: ${workflows.length}`);
