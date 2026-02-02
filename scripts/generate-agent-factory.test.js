#!/usr/bin/env node

/**
 * Test for Agent Factory Page Generator
 *
 * Validates that the agent factory page generator correctly:
 * - Extracts workflow information from lock files
 * - Extracts engine types from markdown files
 * - Generates a properly formatted table
 * - Links to workflow markdown files
 */

import fs from "fs";
import path from "path";
import { fileURLToPath } from "url";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

// Paths
const OUTPUT_PATH = path.join(__dirname, "../docs/src/content/docs/agent-factory-status.mdx");

/**
 * Test helper to check if output contains expected content
 */
function assertContains(content, expected, testName) {
  if (!content.includes(expected)) {
    console.error(`❌ FAIL: ${testName}`);
    console.error(`   Expected to find: "${expected}"`);
    return false;
  }
  console.log(`✓ PASS: ${testName}`);
  return true;
}

/**
 * Test helper to check if output does NOT contain unexpected content
 */
function assertNotContains(content, unexpected, testName) {
  if (content.includes(unexpected)) {
    console.error(`❌ FAIL: ${testName}`);
    console.error(`   Expected NOT to find: "${unexpected}"`);
    return false;
  }
  console.log(`✓ PASS: ${testName}`);
  return true;
}

/**
 * Test helper to count occurrences of a pattern
 */
function countOccurrences(content, pattern) {
  const matches = content.match(new RegExp(pattern, "g"));
  return matches ? matches.length : 0;
}

// Run the agent factory page generator
console.log("Running agent factory page generator...");
import("./generate-agent-factory.js");

// Wait a bit for the file to be written
await new Promise(resolve => setTimeout(resolve, 500));

// Read the generated output
const output = fs.readFileSync(OUTPUT_PATH, "utf-8");

// Test suite
let allPassed = true;

console.log("\nRunning tests...\n");

// Test 1: Table format with new columns
allPassed &= assertContains(output, "| Workflow | Agent | Status | Schedule | Command |", "Table header is present with correct columns");

allPassed &= assertContains(output, "|:---------|:-----:|:------:|:--------:|:-------:|", "Table separator is present with center alignment");

// Test 2: Engine detection (copilot)
allPassed &= assertContains(output, "| copilot |", "Copilot engine detected in at least one workflow");

// Test 3: Engine detection (claude)
allPassed &= assertContains(output, "| claude |", "Claude engine detected in at least one workflow");

// Test 4: Engine detection (codex)
allPassed &= assertContains(output, "| codex |", "Codex engine detected in at least one workflow");

// Test 5: Workflow links are present
allPassed &= assertContains(output, ".github/workflows/", "Workflow links to .github/workflows directory");

allPassed &= assertContains(output, ".md)", "Workflow links point to .md files");

// Test 6: Status badges are present
allPassed &= assertContains(output, "badge.svg", "Status badges are present");

allPassed &= assertContains(output, "https://github.com/github/gh-aw/actions/workflows/", "Status badges link to workflow runs");

// Test 7: No "unknown" engine values
allPassed &= assertNotContains(output, "| unknown |", "No workflows with unknown engine (should default to copilot)");

// Test 8: Frontmatter is correct
allPassed &= assertContains(output, "title: Agent Factory", "Frontmatter title is present");

allPassed &= assertContains(output, "description: Experimental agentic workflows used by the team to learn and build.", "Frontmatter description is present");

// Test 9: Introduction text is present (streamlined)
allPassed &= assertContains(
  output,
  "These are experimental agentic workflows used by the GitHub Next team to learn, build, and use agentic workflows. [Browse source files](https://github.com/github/gh-aw/tree/main/.github/workflows).",
  "Introduction text is present (streamlined)"
);

// Test 10: Note section is present (streamlined)
allPassed &= assertContains(output, ":::note", "Note section is present");

allPassed &= assertContains(output, "Badges update automatically. Click badges for run details or workflow names for source files.", "Note text is streamlined");

// Test 11: Schedule column has cron expressions
allPassed &= assertContains(output, "| `0", "Schedule column contains cron expressions with backticks");

// Test 12: Schedule column has no-schedule indicator
allPassed &= assertContains(output, "| - |", "Schedule column has '-' for non-scheduled workflows");

// Test 13: Command column exists
allPassed &= assertContains(output, "| Command |", "Command column header is present");

// Test 14: Command column has command values
allPassed &= assertContains(output, "`/", "Command column contains command values with backticks and /");

// Test 15: Firewall column should NOT exist
allPassed &= assertNotContains(output, "| Firewall |", "Firewall column header is removed");

// Test 16: Edit column should NOT exist
allPassed &= assertNotContains(output, "| Edit |", "Edit column header is removed");

// Test 17: Bash * column should NOT exist
allPassed &= assertNotContains(output, "| Bash * |", "Bash * column header is removed");

// Test 18: Verify table rows match workflow count
const tableRowCount = countOccurrences(output, "\\| \\[!\\[");
console.log(`Found ${tableRowCount} table rows with workflows`);
if (tableRowCount >= 50) {
  // We expect at least 50 workflows
  console.log("✓ PASS: Table contains workflow rows");
} else {
  console.error(`❌ FAIL: Table should contain at least 50 workflow rows, found ${tableRowCount}`);
  allPassed = false;
}

// Test 19: Verify no CardGrid remnants
allPassed &= assertNotContains(output, "<CardGrid>", "No CardGrid component (should be table now)");

allPassed &= assertNotContains(output, "<Card>", "No Card component (should be table now)");

// Test 20: Verify source link is present (moved to intro text, streamlined)
allPassed &= assertContains(output, "[Browse source files](https://github.com/github/gh-aw/tree/main/.github/workflows)", "Source files link is present (streamlined)");

// Test 21: Verify no separate "Workflow Link" column
allPassed &= assertNotContains(output, "| Workflow Link |", "No separate 'Workflow Link' column (consolidated into first column)");

// Summary
console.log("\n" + "=".repeat(50));
if (allPassed) {
  console.log("✅ All tests passed!");
  process.exit(0);
} else {
  console.log("❌ Some tests failed!");
  process.exit(1);
}
