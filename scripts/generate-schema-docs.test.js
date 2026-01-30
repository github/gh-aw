#!/usr/bin/env node

/**
 * Test for Schema Documentation Generator
 *
 * Validates that $ref resolution works correctly and that the generated
 * documentation includes properly resolved schema definitions.
 */

import fs from "fs";
import path from "path";
import { fileURLToPath } from "url";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

// Paths
const SCHEMA_PATH = path.join(__dirname, "../pkg/parser/schemas/main_workflow_schema.json");
const OUTPUT_PATH = path.join(__dirname, "../docs/src/content/docs/reference/frontmatter-full.md");

// Read the schema
const schema = JSON.parse(fs.readFileSync(SCHEMA_PATH, "utf-8"));

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

// Run the schema doc generator
console.log("Running schema documentation generator...");
import("./generate-schema-docs.js");

// Wait a bit for the file to be written
await new Promise(resolve => setTimeout(resolve, 500));

// Read the generated output
const output = fs.readFileSync(OUTPUT_PATH, "utf-8");

// Test suite
let allPassed = true;

console.log("\nRunning tests...\n");

// Test 1: Engine $ref resolution
allPassed &= assertContains(output, 'engine: "claude"', "Engine $ref resolved - should show string option");

allPassed &= assertContains(output, 'id: "claude"', "Engine $ref resolved - should show object variant with id field");

allPassed &= assertContains(output, "max-turns:", "Engine $ref resolved - should show max-turns field");

allPassed &= assertNotContains(output, "engine: null", "Engine $ref resolved - should NOT show null value");

// Test 2: Permissions $ref resolution
allPassed &= assertContains(output, 'permissions: "read-all"', "Permissions $ref resolved - should show string option");

allPassed &= assertContains(output, 'actions: "read"', "Permissions $ref resolved - should show object variant with actions field");

// Test 3: Defaults $ref resolution
allPassed &= assertContains(output, "defaults:", "Defaults $ref resolved - should be present in output");

// Test 4: Concurrency $ref resolution
allPassed &= assertContains(output, "concurrency:", "Concurrency $ref resolved - should be present in output");

// Test 5: MCP tool $ref resolution
allPassed &= assertContains(output, "mcp-servers:", "MCP servers section present");

// Test 6: Verify $defs/engine_config is properly resolved
const engineDefHasOneOf = schema.$defs.engine_config.oneOf !== undefined;
if (engineDefHasOneOf) {
  console.log("✓ PASS: Schema has $defs/engine_config with oneOf variants");
} else {
  console.error("❌ FAIL: Schema should have $defs/engine_config with oneOf variants");
  allPassed = false;
}

// Test 7: Verify yaml code block has wrap attribute
allPassed &= assertContains(output, "```yaml wrap", "YAML code block should have wrap attribute for line wrapping");

allPassed &= assertNotContains(output, "```yaml\n---\n# Workflow name", "YAML code block should NOT be plain ```yaml without wrap");

// Test 8: Verify that all $refs in schema can be resolved
const allRefs = ["#/$defs/engine_config", "#/$defs/stdio_mcp_tool", "#/$defs/http_mcp_tool", "#/properties/permissions", "#/properties/defaults", "#/properties/concurrency"];

for (const ref of allRefs) {
  const path = ref.substring(2).split("/");
  let current = schema;
  let resolved = true;

  for (const segment of path) {
    if (current && typeof current === "object" && segment in current) {
      current = current[segment];
    } else {
      resolved = false;
      break;
    }
  }

  if (resolved && current) {
    console.log(`✓ PASS: Schema $ref ${ref} can be resolved`);
  } else {
    console.error(`❌ FAIL: Schema $ref ${ref} cannot be resolved`);
    allPassed = false;
  }
}

// Test 9: Verify that deprecated fields are excluded from output and schema
allPassed &= assertNotContains(output, "timeout_minutes:", "Deprecated field timeout_minutes should NOT be in output");

allPassed &= assertContains(output, "timeout-minutes:", "Non-deprecated field timeout-minutes should be in output");

// Verify the schema does NOT have the deprecated field (it was removed completely)
if (schema.properties && !schema.properties.timeout_minutes) {
  console.log("✓ PASS: Schema does not have timeout_minutes field (removed completely)");
} else {
  console.error("❌ FAIL: Schema should NOT have timeout_minutes field");
  allPassed = false;
}

// Summary
console.log("\n" + "=".repeat(50));
if (allPassed) {
  console.log("✅ All tests passed!");
  process.exit(0);
} else {
  console.log("❌ Some tests failed!");
  process.exit(1);
}
