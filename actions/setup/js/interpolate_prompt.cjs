// @ts-check
/// <reference types="@actions/github-script" />

// @safe-outputs-exempt SEC-004 — no issue body is read or reflected; the only "body" occurrence is
// a literal log string ("body") used to describe a template branch, not user-controlled content.

// interpolate_prompt.cjs
// Interpolates GitHub Actions expressions and renders template conditionals in the prompt file.
// This combines variable interpolation and template filtering into a single step.

const fs = require("fs");
const path = require("path");
const { randomUUID } = require("crypto");
const { processRuntimeImports, generatePlaceholderName, generateHashedExpressionEnvVarName } = require("./runtime_import.cjs");
const { writeInlineSubAgents } = require("./extract_inline_sub_agents.cjs");
const { writeInlineSkills } = require("./extract_inline_skills.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { ERR_API, ERR_CONFIG, ERR_VALIDATION } = require("./error_codes.cjs");
const { renderMarkdownTemplate } = require("./render_template.cjs");
const { createLiteralValues, readLiteralState, restoreInlineMetadata, writeLiteralState, literalFileKey } = require("./prompt_literal_values.cjs");

/**
 * @typedef {Object} ImportTreeNode
 * @property {string} macro - The original {{#runtime-import ...}} macro text
 * @property {string} src - The resolved local file path
 * @property {"author"} trust - Author-capable local source, not a guarantee about interpolated values
 * @property {boolean} optional - Whether the import was optional ({{#runtime-import?}})
 * @property {number|null} startLine - Start line for partial imports, or null
 * @property {number|null} endLine - End line for partial imports, or null
 * @property {string} rawContent - File content before nested import expansion (or raw cached content)
 * @property {boolean} [cached] - True when content was served from import cache
 * @property {ImportTreeNode[]} children - Nested import nodes
 */

/**
 * Interpolates variables in the prompt content
 * @param {string} content - The prompt content with ${GH_AW_EXPR_*} placeholders
 * @param {Record<string, string>} variables - Map of variable names to their values
 * @returns {string} - The interpolated content
 */
function interpolateVariables(content, variables) {
  core.info(`[interpolateVariables] Starting interpolation with ${Object.keys(variables).length} variables`);
  core.info(`[interpolateVariables] Content length: ${content.length} characters`);

  const replacements = new Map();
  let totalReplacements = 0;

  // Replace each ${VAR_NAME} with its corresponding value
  for (const [varName, value] of Object.entries(variables)) {
    const escapedVarName = varName.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    const pattern = new RegExp(`\\$\\{${escapedVarName}\\}`, "g");
    const matches = (content.match(pattern) || []).length;

    if (matches > 0) {
      core.debug(`[interpolateVariables] Replacing variable (${matches} occurrence(s))`);
      replacements.set(`\${${varName}}`, value);
      totalReplacements += matches;
    } else {
      core.debug("[interpolateVariables] Unused variable");
    }
  }

  // Replace only author-owned placeholders, never placeholders inside inserted values.
  const pattern = new RegExp([...replacements.keys()].map(key => key.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")).join("|"), "g");
  const result = replacements.size > 0 ? content.replace(pattern, match => replacements.get(match) ?? match) : content;
  core.info(`[interpolateVariables] Completed: ${totalReplacements} total replacement(s)`);
  core.info(`[interpolateVariables] Result length: ${result.length} characters`);
  return result;
}

/**
 * Main function for prompt variable interpolation and template rendering
 * @param {{deferredBindings?: Record<string, string>, authorExpressions?: Record<string, string>}} [options] - Compiler-owned expression bindings
 */
async function main({ deferredBindings = {}, authorExpressions = {} } = {}) {
  try {
    core.info("========================================");
    core.info("[main] Starting interpolate_prompt processing");
    core.info("========================================");

    const promptPath = process.env.GH_AW_PROMPT;
    if (!promptPath) {
      core.setFailed(`${ERR_CONFIG}: GH_AW_PROMPT environment variable is not set`);
      return;
    }

    // Get the workspace directory for runtime imports
    const workspaceDir = process.env.GITHUB_WORKSPACE;
    if (!workspaceDir) {
      core.setFailed(`${ERR_CONFIG}: GITHUB_WORKSPACE environment variable is not set`);
      return;
    }

    // Read the prompt file
    core.info(`[main] Reading prompt file...`);
    let content = fs.readFileSync(promptPath, "utf8");
    const originalLength = content.length;
    // Keep evaluated context opaque until all author-owned directives have run.
    // The same protection covers values evaluated while expanding runtime imports.
    const { state: literalState, protect: protectValue, restore: restoreValues } = createLiteralValues(readLiteralState(promptPath) ?? undefined);
    const protectExpression = (/** @type {string} */ value, /** @type {string|undefined} */ expression) => {
      if (expression && (deferredBindings[generatePlaceholderName(expression)] === value || deferredBindings[generateHashedExpressionEnvVarName(expression)] === value)) {
        return value;
      }
      return protectValue(value);
    };
    const recordFile = (/** @type {string} */ opaque, /** @type {string} */ target, metadata = false) => {
      const rendered = metadata ? restoreInlineMetadata(opaque, restoreValues) : restoreValues(opaque);
      literalState.files[literalFileKey(promptPath, target)] = { opaque, rendered, metadata };
      return rendered;
    };
    const restoreArtifact = (/** @type {string} */ opaque, /** @type {string|undefined} */ target) => (target ? recordFile(opaque, target, true) : restoreInlineMetadata(opaque, restoreValues));
    core.info(`[main] Original content length: ${originalLength} characters`);

    // Write the raw template to prompt-template.txt BEFORE any processing.
    // This allows downstream consumers (e.g. threat detection) to diff the
    // template against the fully-rendered prompt to identify interpolation boundaries.
    const promptDir = path.dirname(promptPath);
    const templatePath = path.join(promptDir, "prompt-template.txt");
    core.info("[main] Writing raw template artifact");
    fs.writeFileSync(templatePath, content, "utf8");

    const systemPath = path.join(promptDir, "system.txt");
    const userPath = path.join(promptDir, "user.txt");
    let splitMarker;
    if (fs.existsSync(systemPath) && fs.existsSync(userPath)) {
      const system = fs.readFileSync(systemPath, "utf8");
      const user = fs.readFileSync(userPath, "utf8");
      if (content !== system + user) {
        throw new Error(`${ERR_VALIDATION}: Split prompt files do not match the combined prompt`);
      }
      // Keep the marker on its own line so line-anchored processing (inline
      // agent/skill headings, template blocks) sees the same line starts.
      const leadingNewline = system === "" || system.endsWith("\n") ? "" : "\n";
      splitMarker = `${leadingNewline}\x00GH_AW_PROMPT_SPLIT_${randomUUID()}\x00\n`;
      content = system + splitMarker + user;
    }

    // Step 1: Process local file runtime imports without network access
    core.info("\n========================================");
    core.info("[main] STEP 1: Runtime Imports");
    core.info("========================================");
    const hasRuntimeImports = /\{\{#(?:runtime-import|import)\??(?:[ \t]+|[ \t]*:[ \t]*)/.test(content);

    // Build an import provenance tree so downstream consumers (e.g. threat detection)
    // can identify which parts of the rendered prompt originated from which source files.
    const importTree = {
      version: 1,
      trust: "author",
      template: splitMarker ? content.replace(splitMarker, "") : content,
      children: /** @type {ImportTreeNode[]} */ [],
    };

    if (hasRuntimeImports) {
      const importMatches = content.match(/{{#runtime-import\??[ \t]+[^\}]+}}/g) || [];
      core.info(`Processing ${importMatches.length} runtime import macro(s) (local files only)`);

      const beforeImports = content.length;
      content = await processRuntimeImports(content, workspaceDir, new Set(), new Map(), [], importTree.children, new Map(), new Set(), protectExpression, authorExpressions);
      const afterImports = content.length;

      core.info(`Runtime imports processed successfully`);
      core.info(`Content length change: ${beforeImports} -> ${afterImports} (${afterImports > beforeImports ? "+" : ""}${afterImports - beforeImports})`);
    } else {
      core.info("No runtime import macros found, skipping runtime import processing");
    }

    // Write the import tree JSON to the artifact directory so threat detection can
    // analyse provenance without re-parsing the rendered prompt.
    const importTreePath = path.join(promptDir, "prompt-import-tree.json");
    core.info("[main] Writing import tree artifact");
    const restoreImportTree = (/** @type {ImportTreeNode[]} */ nodes) => {
      for (const node of nodes) {
        node.rawContent = restoreValues(node.rawContent);
        restoreImportTree(node.children);
      }
    };
    restoreImportTree(importTree.children);
    fs.writeFileSync(importTreePath, JSON.stringify(importTree, null, 2), "utf8");

    // Step 1.5: Extract and write inline sub-agents
    // ## agent: name / ## end: name blocks are written to .github/agents/<name>.md.
    // This happens after runtime imports so that any {{#runtime-import}} macros
    // inside an agent block have already been resolved.
    core.info("\n========================================");
    core.info("[main] STEP 1.5: Inline Sub-Agent Extraction");
    core.info("========================================");
    const hasAgentMarkers = /^##[ \t]+agent:[ \t]+`[a-z]/m.test(content);
    if (hasAgentMarkers) {
      const beforeExtraction = content.length;
      // Write agents to /tmp/gh-aw/<engine-dir>/ so the files are included in the
      // activation artifact and available to the downstream agent job.
      const agentsBaseDir = "/tmp/gh-aw";
      const engineId = process.env.GH_AW_ENGINE_ID || "";
      content = writeInlineSubAgents(content, workspaceDir, agentsBaseDir, engineId, restoreArtifact);
      const afterExtraction = content.length;
      core.info(`Inline sub-agents extracted and written`);
      core.info(`Content length change: ${beforeExtraction} -> ${afterExtraction} (${afterExtraction > beforeExtraction ? "+" : ""}${afterExtraction - beforeExtraction})`);
    } else {
      core.info("No inline sub-agent markers found, skipping");
    }

    // Step 1.6: Extract and write inline skills
    // ## skill: `name` blocks are written to engine-specific skills directories.
    // This runs after runtime imports so macros inside skill blocks are resolved.
    core.info("\n========================================");
    core.info("[main] STEP 1.6: Inline Skill Extraction");
    core.info("========================================");
    const hasSkillMarkers = /^##[ \t]+skill:[ \t]+`[a-z]/m.test(content);
    if (hasSkillMarkers) {
      const beforeExtraction = content.length;
      const skillsBaseDir = "/tmp/gh-aw";
      const engineId = process.env.GH_AW_ENGINE_ID || "";
      content = writeInlineSkills(content, workspaceDir, skillsBaseDir, engineId, restoreArtifact);
      const afterExtraction = content.length;
      core.info(`Inline skills extracted and written`);
      core.info(`Content length change: ${beforeExtraction} -> ${afterExtraction} (${afterExtraction > beforeExtraction ? "+" : ""}${afterExtraction - beforeExtraction})`);
    } else {
      core.info("No inline skill markers found, skipping");
    }

    // Step 2: Interpolate variables
    core.info("\n========================================");
    core.info("[main] STEP 2: Variable Interpolation");
    core.info("========================================");
    /** @type {Record<string, string>} */
    const variables = {};
    for (const [key, value] of Object.entries(process.env)) {
      if (key.startsWith("GH_AW_EXPR_")) {
        variables[key] = deferredBindings[key] === value ? value : protectValue(value || "");
      }
    }

    const varCount = Object.keys(variables).length;
    if (varCount > 0) {
      core.info(`Found ${varCount} expression variable(s) to interpolate`);

      const beforeInterpolation = content.length;
      content = interpolateVariables(content, variables);
      const afterInterpolation = content.length;

      core.info(`Successfully interpolated ${varCount} variable(s) in prompt`);
      core.info(`Content length change: ${beforeInterpolation} -> ${afterInterpolation} (${afterInterpolation > beforeInterpolation ? "+" : ""}${afterInterpolation - beforeInterpolation})`);
    } else {
      core.info("No expression variables found, skipping interpolation");
    }

    // Step 2.5: Protect experiment values BEFORE template rendering.
    // Resolve placeholders and dot-notation references to opaque tokens. Actual
    // values are used only after parsing conditions, or when restoring final content.
    core.info("\n========================================");
    core.info("[main] STEP 2.5: Experiment Placeholder Substitution");
    core.info("========================================");
    let experimentSubCount = 0;
    for (const [key, value] of Object.entries(process.env)) {
      if (key.startsWith("GH_AW_EXPERIMENTS_")) {
        const protectedValue = protectValue(value || "");
        const placeholder = `__${key}__`;
        if (content.includes(placeholder)) {
          content = content.split(placeholder).join(protectedValue);
          experimentSubCount++;
          core.debug("[main] Substituted experiment placeholder");
        }
        // Comparisons in author-owned if/elseif tags also use experiment values.
        const experimentName = key.substring("GH_AW_EXPERIMENTS_".length).toLowerCase();
        const exprForm = `experiments.${experimentName}`;
        const conditionPattern = /(\{\{#?(?:if|else[-_]?if)\s+)([^}]*)(\}\})/gi;
        const operandPattern = new RegExp(`"[^"]*"|'[^']*'|\\b${exprForm.replace(".", "\\.")}\\b`, "gi");
        let substitutedCondition = false;
        content = content.replace(conditionPattern, (_, prefix, condition, suffix) => {
          const substituted = condition.replace(operandPattern, operand => {
            if (operand.startsWith('"') || operand.startsWith("'")) return operand;
            substitutedCondition = true;
            return protectedValue;
          });
          return prefix + substituted + suffix;
        });
        if (substitutedCondition) {
          core.debug("[main] Substituted experiment condition operand");
        }
      }
    }
    if (experimentSubCount > 0) {
      core.info(`Substituted ${experimentSubCount} experiment placeholder(s)`);
    } else {
      core.info("No experiment placeholders found in prompt");
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
      content = renderMarkdownTemplate(content, restoreValues);
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
    core.info("Writing processed prompt");
    core.info(`Final content length: ${content.length} characters`);
    core.info(`Total length change: ${originalLength} -> ${content.length} (${content.length > originalLength ? "+" : ""}${content.length - originalLength})`);

    if (splitMarker) {
      const parts = content.split(splitMarker);
      if (parts.length !== 2) {
        throw new Error(`${ERR_VALIDATION}: Prompt split boundary was lost during processing`);
      }
      fs.writeFileSync(systemPath, recordFile(parts[0], systemPath), "utf8");
      fs.writeFileSync(userPath, recordFile(parts[1], userPath), "utf8");
      content = parts.join("");
    }
    content = recordFile(content, promptPath);
    fs.writeFileSync(promptPath, content, "utf8");
    // Preserve the opaque author-owned view across later activation/deferred passes.
    writeLiteralState(promptPath, literalState);

    core.info("========================================");
    core.info("[main] Processing complete - SUCCESS");
    core.info("========================================");
  } catch (error) {
    core.info("========================================");
    core.info("[main] Processing failed - ERROR");
    core.info("========================================");
    core.setFailed(`${ERR_API}: ${getErrorMessage(error)}`);
  }
}

module.exports = { main, renderMarkdownTemplate, interpolateVariables };
