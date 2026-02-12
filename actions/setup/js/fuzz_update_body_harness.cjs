// @ts-check
// @ts-ignore - global.core is injected at runtime
/**
 * Fuzz test harness for updateBody (body insertion/replacement operations)
 * This file is used by Go fuzz tests to test the updateBody function with various inputs.
 */

// Mock core for logging
// @ts-ignore - global.core is injected at runtime
global.core = {
  info: () => {},
  debug: () => {},
  warning: () => {},
  error: () => {},
};

const { updateBody } = require("./update_pr_description_helpers.cjs");

/**
 * Test the updateBody function with given parameters
 * @param {string} currentBody - Current body content
 * @param {string} newContent - New content to add/replace
 * @param {string} operation - Operation type: "append", "prepend", "replace", or "replace-island"
 * @param {string} workflowName - Name of the workflow
 * @param {string} runUrl - URL of the workflow run
 * @param {string} workflowId - Workflow ID (stable identifier across runs)
 * @returns {{result: string, error: string | null}} Result object
 */
function testUpdateBody(currentBody, newContent, operation, workflowName, runUrl, workflowId) {
  try {
    const result = updateBody({
      currentBody,
      newContent,
      operation,
      workflowName,
      runUrl,
      workflowId,
    });
    return { result, error: null };
  } catch (err) {
    return {
      result: "",
      error: err instanceof Error ? err.message : String(err),
    };
  }
}

// Read input from stdin for fuzzing
if (require.main === module) {
  let input = "";

  process.stdin.on("data", chunk => {
    input += chunk;
  });

  process.stdin.on("end", () => {
    try {
      // Parse input as JSON: { currentBody, newContent, operation, workflowName, runUrl, workflowId }
      const { currentBody, newContent, operation, workflowName, runUrl, workflowId } = JSON.parse(input);
      const result = testUpdateBody(currentBody || "", newContent || "", operation || "append", workflowName || "Test Workflow", runUrl || "https://github.com/test/actions/runs/123", workflowId || "test-workflow");
      process.stdout.write(JSON.stringify(result));
      process.exit(0);
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : String(err);
      process.stdout.write(JSON.stringify({ result: "", error: errorMsg }));
      process.exit(1);
    }
  });
}

module.exports = { testUpdateBody };
