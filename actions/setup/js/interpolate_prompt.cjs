// @ts-check
/// <reference types="@actions/github-script" />

// interpolate_prompt.cjs
// Interpolates GitHub Actions expressions and renders template conditionals in the prompt file.
// This combines variable interpolation and template filtering into a single step.

const fs = require("fs");
const { isTruthy } = require("./is_truthy.cjs");
const { processRuntimeImports } = require("./runtime_import.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");

/**
 * Interpolates variables in the prompt content
 * @param {string} content - The prompt content with ${GH_AW_EXPR_*} placeholders
 * @param {Record<string, string>} variables - Map of variable names to their values
 * @returns {string} - The interpolated content
 */
function interpolateVariables(content, variables) {
  core.info(`[interpolateVariables] Starting interpolation with ${Object.keys(variables).length} variables`);
  core.info(`[interpolateVariables] Content length: ${content.length} characters`);

  let result = content;
  let totalReplacements = 0;

  // Replace each ${VAR_NAME} with its corresponding value
  for (const [varName, value] of Object.entries(variables)) {
    const pattern = new RegExp(`\\$\\{${varName}\\}`, "g");
    const matches = (content.match(pattern) || []).length;

    if (matches > 0) {
      core.info(`[interpolateVariables] Replacing ${varName} (${matches} occurrence(s))`);
      core.info(`[interpolateVariables]   Value: ${value.substring(0, 100)}${value.length > 100 ? "..." : ""}`);
      result = result.replace(pattern, value);
      totalReplacements += matches;
    } else {
      core.info(`[interpolateVariables] Variable ${varName} not found in content (unused)`);
    }
  }

  core.info(`[interpolateVariables] Completed: ${totalReplacements} total replacement(s)`);
  core.info(`[interpolateVariables] Result length: ${result.length} characters`);
  return result;
}

/**
 * Renders a Markdown template by processing {{#if}} conditional blocks.
 * When a conditional block is removed (falsy condition) and the template tags
 * were on their own lines, the empty lines are cleaned up to avoid
 * leaving excessive blank lines in the output.
 * @param {string} markdown - The markdown content to process
 * @returns {string} - The processed markdown content
 */
function renderMarkdownTemplate(markdown) {
  core.info(`[renderMarkdownTemplate] Starting template rendering`);
  core.info(`[renderMarkdownTemplate] Input length: ${markdown.length} characters`);

  // Count conditionals before processing
  const blockConditionals = (markdown.match(/(\n?)([ \t]*{{#if\s+([^}]*)}}[ \t]*\n)([\s\S]*?)([ \t]*{{\/if}}[ \t]*)(\n?)/g) || []).length;
  const inlineConditionals = (markdown.match(/{{#if\s+([^}]*)}}([\s\S]*?){{\/if}}/g) || []).length - blockConditionals;

  core.info(`[renderMarkdownTemplate] Found ${blockConditionals} block conditional(s) and ${inlineConditionals} inline conditional(s)`);

  let blockCount = 0;
  let keptBlocks = 0;
  let removedBlocks = 0;

  // First pass: Handle blocks where tags are on their own lines
  // Captures: (leading newline)(opening tag line)(condition)(body)(closing tag line)(trailing newline)
  let result = markdown.replace(/(\n?)([ \t]*{{#if\s+([^}]*)}}[ \t]*\n)([\s\S]*?)([ \t]*{{\/if}}[ \t]*)(\n?)/g, (match, leadNL, openLine, cond, body, closeLine, trailNL) => {
    blockCount++;
    const condTrimmed = cond.trim();
    const truthyResult = isTruthy(cond);
    const bodyPreview = body.substring(0, 60).replace(/\n/g, "\\n");

    core.info(`[renderMarkdownTemplate] Block ${blockCount}: condition="${condTrimmed}" -> ${truthyResult ? "KEEP" : "REMOVE"}`);
    core.info(`[renderMarkdownTemplate]   Body preview: "${bodyPreview}${body.length > 60 ? "..." : ""}"`);

    if (truthyResult) {
      // Keep body with leading newline if there was one before the opening tag
      keptBlocks++;
      core.info(`[renderMarkdownTemplate]   Action: Keeping body with leading newline=${!!leadNL}`);
      return leadNL + body;
    } else {
      // Remove entire block completely - the line containing the template is removed
      removedBlocks++;
      core.info(`[renderMarkdownTemplate]   Action: Removing entire block`);
      return "";
    }
  });

  core.info(`[renderMarkdownTemplate] First pass complete: ${keptBlocks} kept, ${removedBlocks} removed`);

  let inlineCount = 0;
  let keptInline = 0;
  let removedInline = 0;

  // Second pass: Handle inline conditionals (tags not on their own lines)
  result = result.replace(/{{#if\s+([^}]*)}}([\s\S]*?){{\/if}}/g, (_, cond, body) => {
    inlineCount++;
    const condTrimmed = cond.trim();
    const truthyResult = isTruthy(cond);
    const bodyPreview = body.substring(0, 40).replace(/\n/g, "\\n");

    core.info(`[renderMarkdownTemplate] Inline ${inlineCount}: condition="${condTrimmed}" -> ${truthyResult ? "KEEP" : "REMOVE"}`);
    core.info(`[renderMarkdownTemplate]   Body preview: "${bodyPreview}${body.length > 40 ? "..." : ""}"`);

    if (truthyResult) {
      keptInline++;
      return body;
    } else {
      removedInline++;
      return "";
    }
  });

  core.info(`[renderMarkdownTemplate] Second pass complete: ${keptInline} kept, ${removedInline} removed`);

  // Clean up excessive blank lines (more than one blank line = 2 newlines)
  const beforeCleanup = result.length;
  const excessiveLines = (result.match(/\n{3,}/g) || []).length;
  result = result.replace(/\n{3,}/g, "\n\n");

  if (excessiveLines > 0) {
    core.info(`[renderMarkdownTemplate] Cleaned up ${excessiveLines} excessive blank line sequence(s)`);
    core.info(`[renderMarkdownTemplate] Length change from cleanup: ${beforeCleanup} -> ${result.length} characters`);
  }
  core.info(`[renderMarkdownTemplate] Final output length: ${result.length} characters`);

  return result;
}

/**
 * Main function for prompt variable interpolation and template rendering
 */
async function main() {
  try {
    core.info("========================================");
    core.info("[main] Starting interpolate_prompt processing");
    core.info("========================================");

    const promptPath = process.env.GH_AW_PROMPT;
    if (!promptPath) {
      core.setFailed("GH_AW_PROMPT environment variable is not set");
      return;
    }
    core.info(`[main] Prompt path: ${promptPath}`);

    // Get the workspace directory for runtime imports
    const workspaceDir = process.env.GITHUB_WORKSPACE;
    if (!workspaceDir) {
      core.setFailed("GITHUB_WORKSPACE environment variable is not set");
      return;
    }
    core.info(`[main] Workspace directory: ${workspaceDir}`);

    // Read the prompt file
    core.info(`[main] Reading prompt file...`);
    let content = fs.readFileSync(promptPath, "utf8");
    const originalLength = content.length;
    core.info(`[main] Original content length: ${originalLength} characters`);
    core.info(`[main] First 200 characters: ${content.substring(0, 200).replace(/\n/g, "\\n")}`);

    // Step 1: Process runtime imports (files and URLs)
    core.info("\n========================================");
    core.info("[main] STEP 1: Runtime Imports");
    core.info("========================================");
    const hasRuntimeImports = /{{#runtime-import\??[ \t]+[^\}]+}}/.test(content);
    if (hasRuntimeImports) {
      const importMatches = content.match(/{{#runtime-import\??[ \t]+[^\}]+}}/g) || [];
      core.info(`Processing ${importMatches.length} runtime import macro(s) (files and URLs)`);
      importMatches.forEach((match, i) => {
        core.info(`  Import ${i + 1}: ${match.substring(0, 80)}${match.length > 80 ? "..." : ""}`);
      });

      const beforeImports = content.length;
      content = await processRuntimeImports(content, workspaceDir);
      const afterImports = content.length;

      core.info(`Runtime imports processed successfully`);
      core.info(`Content length change: ${beforeImports} -> ${afterImports} (${afterImports > beforeImports ? "+" : ""}${afterImports - beforeImports})`);
    } else {
      core.info("No runtime import macros found, skipping runtime import processing");
    }

    // Step 2: Interpolate variables
    core.info("\n========================================");
    core.info("[main] STEP 2: Variable Interpolation");
    core.info("========================================");
    /** @type {Record<string, string>} */
    const variables = {};
    for (const [key, value] of Object.entries(process.env)) {
      if (key.startsWith("GH_AW_EXPR_")) {
        variables[key] = value || "";
      }
    }

    const varCount = Object.keys(variables).length;
    if (varCount > 0) {
      core.info(`Found ${varCount} expression variable(s) to interpolate:`);
      for (const [key, value] of Object.entries(variables)) {
        const preview = value.substring(0, 60);
        core.info(`  ${key}: ${preview}${value.length > 60 ? "..." : ""}`);
      }

      const beforeInterpolation = content.length;
      content = interpolateVariables(content, variables);
      const afterInterpolation = content.length;

      core.info(`Successfully interpolated ${varCount} variable(s) in prompt`);
      core.info(`Content length change: ${beforeInterpolation} -> ${afterInterpolation} (${afterInterpolation > beforeInterpolation ? "+" : ""}${afterInterpolation - beforeInterpolation})`);
    } else {
      core.info("No expression variables found, skipping interpolation");
    }

    // Step 3: Render template conditionals
    core.info("\n========================================");
    core.info("[main] STEP 3: Template Rendering");
    core.info("========================================");
    const hasConditionals = /{{#if\s+[^}]+}}/.test(content);
    if (hasConditionals) {
      const conditionalMatches = content.match(/{{#if\s+[^}]+}}/g) || [];
      core.info(`Processing ${conditionalMatches.length} conditional template block(s)`);

      const beforeRendering = content.length;
      content = renderMarkdownTemplate(content);
      const afterRendering = content.length;

      core.info(`Template rendered successfully`);
      core.info(`Content length change: ${beforeRendering} -> ${afterRendering} (${afterRendering > beforeRendering ? "+" : ""}${afterRendering - beforeRendering})`);
    } else {
      core.info("No conditional blocks found in prompt, skipping template rendering");
    }

    // Write back to the same file
    core.info("\n========================================");
    core.info("[main] STEP 4: Writing Output");
    core.info("========================================");
    core.info(`Writing processed content back to: ${promptPath}`);
    core.info(`Final content length: ${content.length} characters`);
    core.info(`Total length change: ${originalLength} -> ${content.length} (${content.length > originalLength ? "+" : ""}${content.length - originalLength})`);

    fs.writeFileSync(promptPath, content, "utf8");

    core.info(`Last 200 characters: ${content.substring(Math.max(0, content.length - 200)).replace(/\n/g, "\\n")}`);
    core.info("========================================");
    core.info("[main] Processing complete - SUCCESS");
    core.info("========================================");
  } catch (error) {
    core.info("========================================");
    core.info("[main] Processing failed - ERROR");
    core.info("========================================");
    const err = error instanceof Error ? error : new Error(String(error));
    core.info(`[main] Error type: ${err.constructor.name}`);
    core.info(`[main] Error message: ${err.message}`);
    if (err.stack) {
      core.info(`[main] Stack trace:\n${err.stack}`);
    }
    core.setFailed(getErrorMessage(error));
  }
}

module.exports = { main };
