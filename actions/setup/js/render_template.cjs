// @ts-check
/// <reference types="@actions/github-script" />

// render_template.cjs
// Single-function Markdown → Markdown postprocessor for GitHub Actions.
// Processes only {{#if <expr>}} ... {{/if}} blocks after ${{ }} evaluation.

require("./shim.cjs");

const { getErrorMessage } = require("./error_helpers.cjs");
const fs = require("fs");
const { randomUUID } = require("crypto");
const { ERR_API, ERR_CONFIG } = require("./error_codes.cjs");
const { isTruthy } = require("./is_truthy.cjs");
const { selectBranch } = require("./template_branch.cjs");
const { applyToNonCodeRegions } = require("./sanitize_content_core.cjs");

// Closing tag: {{#endif}} (primary) or {{/if}} (alternate)
const BLOCK_CONDITIONAL_REGEX = /(\n?)([ \t]*{{#if\s+([^}]*)}}[ \t]*\n)([\s\S]*?)([ \t]*(?:{{#endif}}|{{\/if}})[ \t]*)(\n?)/;
const INLINE_CONDITIONAL_REGEX = /{{#if\s+([^}]*)}}([\s\S]*?)(?:{{#endif}}|{{\/if}})/;

// Max characters shown in body preview log lines
const BLOCK_BODY_PREVIEW_LENGTH = 60;
const INLINE_BODY_PREVIEW_LENGTH = 40;

/**
 * Renders a Markdown template by processing {{#if}} conditional blocks.
 * When a conditional block is removed (falsy condition) and the template tags
 * were on their own lines, the empty lines are cleaned up to avoid
 * leaving excessive blank lines in the output.
 * @param {string} markdown - The markdown content to process
 * @param {(condition: string) => string} [resolveCondition] - Resolve context values only after directive parsing
 * @returns {string} - The processed markdown content
 */
function renderMarkdownTemplate(markdown, resolveCondition = condition => condition) {
  core.info(`[renderMarkdownTemplate] Starting template rendering`);
  core.info(`[renderMarkdownTemplate] Input length: ${markdown.length} characters`);

  // Use the sanitizer's code-region rules so inline and fenced examples stay literal.
  const _codeBlocks = [];
  const _FENCE_PH = `\x00CODE_${randomUUID()}\x00`;
  const codePattern = new RegExp(`${_FENCE_PH}(\\d+)${_FENCE_PH}`, "g");
  const _stripped = applyToNonCodeRegions(
    markdown,
    text => text,
    code => {
      const trailingNewline = code.endsWith("\n") ? "\n" : "";
      _codeBlocks.push(trailingNewline ? code.slice(0, -1) : code);
      return `${_FENCE_PH}${_codeBlocks.length - 1}${_FENCE_PH}${trailingNewline}`;
    }
  );
  if (_codeBlocks.length > 0) {
    core.info(`[renderMarkdownTemplate] Preserved ${_codeBlocks.length} code region(s) from template processing`);
  }

  // Count conditionals before processing
  const blockConditionals = (_stripped.match(new RegExp(BLOCK_CONDITIONAL_REGEX.source, "g")) || []).length;
  const inlineConditionals = (_stripped.match(new RegExp(INLINE_CONDITIONAL_REGEX.source, "g")) || []).length - blockConditionals;

  core.info(`[renderMarkdownTemplate] Found ${blockConditionals} block conditional(s) and ${inlineConditionals} inline conditional(s)`);

  let blockCount = 0;
  let keptBlocks = 0;
  let removedBlocks = 0;

  // First pass: Handle blocks where tags are on their own lines
  // Captures: (leading newline)(opening tag line)(condition)(body)(closing tag line)(trailing newline)
  let result = _stripped.replace(new RegExp(BLOCK_CONDITIONAL_REGEX.source, "g"), (match, leadNL, openLine, cond, body) => {
    blockCount++;
    const condTrimmed = cond.trim();
    const bodyPreview = body.substring(0, BLOCK_BODY_PREVIEW_LENGTH).replace(/\n/g, "\\n");

    core.info(`[renderMarkdownTemplate] Block ${blockCount}: condition="${condTrimmed}" -> evaluating branches`);
    core.info(`[renderMarkdownTemplate]   Body preview: "${bodyPreview}${body.length > BLOCK_BODY_PREVIEW_LENGTH ? "..." : ""}"`);

    const selectedContent = selectBranch(cond, body, resolveCondition);

    if (selectedContent !== null) {
      keptBlocks++;
      core.info(`[renderMarkdownTemplate]   Action: Keeping selected branch with leading newline=${!!leadNL}`);
      return leadNL + selectedContent;
    } else {
      removedBlocks++;
      core.info(`[renderMarkdownTemplate]   Action: Removing entire block`);
      // Keep the leading newline so the line before the block stays separated
      // from the line after it (the closing tag's trailing newline is already
      // consumed by the match). Dropping it merges unrelated lines together,
      // e.g. "Before\n{{#if false}}...{{/if}}\nAfter" -> "BeforeAfter".
      return leadNL;
    }
  });

  core.info(`[renderMarkdownTemplate] First pass complete: ${keptBlocks} kept, ${removedBlocks} removed`);

  let inlineCount = 0;
  let keptInline = 0;
  let removedInline = 0;

  // Second pass: Handle inline conditionals (tags not on their own lines)
  result = result.replace(new RegExp(INLINE_CONDITIONAL_REGEX.source, "g"), (_, cond, body) => {
    inlineCount++;
    const condTrimmed = cond.trim();
    const bodyPreview = body.substring(0, INLINE_BODY_PREVIEW_LENGTH).replace(/\n/g, "\\n");
    const selectedContent = selectBranch(cond, body, resolveCondition);

    core.info(`[renderMarkdownTemplate] Inline ${inlineCount}: condition="${condTrimmed}" -> ${selectedContent !== null ? "KEEP" : "REMOVE"}`);
    core.info(`[renderMarkdownTemplate]   Body preview: "${bodyPreview}${body.length > INLINE_BODY_PREVIEW_LENGTH ? "..." : ""}"`);

    if (selectedContent !== null) {
      keptInline++;
      return selectedContent;
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

  // Count which placeholders survived to detect code blocks removed in false conditional branches
  const _survivedIndices = new Set([...result.matchAll(codePattern)].map(m => +m[1]));
  const _removedFenceMarkerCount = _codeBlocks.reduce((sum, block, i) => (_survivedIndices.has(i) ? sum : sum + (block.match(/`{3,}/g) || []).length), 0);

  // Restore fenced code blocks
  if (_codeBlocks.length > 0) {
    result = result.replace(codePattern, (_, i) => _codeBlocks[+i]);
  }

  // Runtime assertion: number of fence markers must be the same before and after processing,
  // accounting for any fenced code blocks intentionally removed inside false conditional branches.
  const _inputFenceCount = (markdown.match(/`{3,}/g) || []).length;
  const _outputFenceCount = (result.match(/`{3,}/g) || []).length;
  if (_inputFenceCount - _removedFenceMarkerCount !== _outputFenceCount) {
    core.warning(`[renderMarkdownTemplate] Fence count mismatch: input had ${_inputFenceCount} fence marker(s), output has ${_outputFenceCount}`);
  }

  core.info(`[renderMarkdownTemplate] Final output length: ${result.length} characters`);

  return result;
}

/**
 * Main function for template rendering in GitHub Actions
 */
function main() {
  try {
    core.info("[render_template] Starting template rendering");

    const promptPath = process.env.GH_AW_PROMPT;
    if (!promptPath) {
      core.setFailed(`${ERR_CONFIG}: GH_AW_PROMPT environment variable is not set`);
      process.exit(1);
    }

    core.info(`[render_template] Prompt path: ${promptPath}`);

    const markdown = fs.readFileSync(promptPath, "utf8");
    core.info(`[render_template] Read ${markdown.length} characters`);

    const hasConditionals = /{{#if\s+[^}]+}}/.test(markdown);
    if (!hasConditionals) {
      core.info("No conditional blocks found in prompt, skipping template rendering");
      process.exit(0);
    }

    const conditionalMatches = markdown.match(/{{#if\s+[^}]+}}/g) || [];
    core.info(`[render_template] Processing ${conditionalMatches.length} conditional template block(s)`);

    const rendered = renderMarkdownTemplate(markdown);

    core.info(`[render_template] Writing back to ${promptPath} (${rendered.length} characters)`);
    fs.writeFileSync(promptPath, rendered, "utf8");

    core.info("[render_template] Processing complete");
  } catch (error) {
    const err = error instanceof Error ? error : new Error(String(error));
    if (err.stack) {
      core.info(`[render_template] Stack trace:\n${err.stack}`);
    }
    core.setFailed(`${ERR_API}: ${getErrorMessage(error)}`);
  }
}

module.exports = { renderMarkdownTemplate, main };
