#!/usr/bin/env node
/**
 * Model Tables Documentation Generator
 *
 * Reads the built-in model alias map from JSON data files and generates an
 * intuitively readable reference page with markdown tables.
 *
 * Usage:
 *   node scripts/generate-model-tables.js
 *
 * Inputs:
 *   pkg/cli/data/model_aliases.json     – Built-in alias → pattern mappings
 *
 * Output:
 *   docs/src/content/docs/reference/model-tables.md
 */

import fs from "fs";
import path from "path";
import { fileURLToPath } from "url";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const ROOT = path.resolve(__dirname, "..");
const ALIASES_PATH = path.join(ROOT, "pkg/workflow/data/model_aliases.json");
const OUTPUT_PATH = path.join(ROOT, "docs/src/content/docs/reference/model-tables.md");

// ---------------------------------------------------------------------------
// Load data
// ---------------------------------------------------------------------------

/**
 * Read and parse a JSON file, with a clear error message on failure.
 * @param {string} filePath
 * @returns {any}
 */
function readJSON(filePath) {
  let raw;
  try {
    raw = fs.readFileSync(filePath, "utf-8");
  } catch (err) {
    console.error(`Error: could not read ${filePath}: ${err.message}`);
    process.exit(1);
  }
  try {
    return JSON.parse(raw);
  } catch (err) {
    console.error(`Error: invalid JSON in ${filePath}: ${err.message}`);
    process.exit(1);
  }
}

const aliasesData = readJSON(ALIASES_PATH);

const aliases = aliasesData.aliases;

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/**
 * Group aliases into "vendor" aliases (patterns using glob wildcards) and
 * "meta" aliases (patterns that reference other aliases by name, with no slash).
 */
function classifyAliases(aliasMap) {
  const vendor = [];
  const meta = [];
  for (const [alias, patterns] of Object.entries(aliasMap)) {
    const isMetaOnly = patterns.every(p => !p.includes("/"));
    if (isMetaOnly) {
      meta.push({ alias, resolves: patterns });
    } else {
      vendor.push({ alias, patterns });
    }
  }
  return { vendor, meta };
}

// ---------------------------------------------------------------------------
// Markdown generators
// ---------------------------------------------------------------------------

function generateAliasTable(vendorAliases) {
  const lines = [];
  lines.push("| Alias | Fallback patterns (tried in order) |");
  lines.push("|-------|-------------------------------------|");
  for (const { alias, patterns } of vendorAliases) {
    const formattedPatterns = patterns.map(p => `\`${p}\``).join(", ");
    lines.push(`| \`${alias}\` | ${formattedPatterns} |`);
  }
  return lines.join("\n");
}

function generateMetaAliasTable(metaAliases) {
  const lines = [];
  lines.push("| Meta-alias | Expands to |");
  lines.push("|------------|------------|");
  for (const { alias, resolves } of metaAliases) {
    const formattedResolves = resolves.map(r => `\`${r}\``).join(" → ");
    lines.push(`| \`${alias}\` | ${formattedResolves} |`);
  }
  return lines.join("\n");
}

// ---------------------------------------------------------------------------
// Build the full document
// ---------------------------------------------------------------------------

function generateMarkdown() {
  const { vendor, meta } = classifyAliases(aliases);

  const lines = [];

  // Frontmatter
  lines.push("---");
  lines.push("title: Model Aliases");
  lines.push("description: Reference tables for the built-in model alias map used by GitHub Agentic Workflows.");
  lines.push("sidebar:");
  lines.push("  order: 297");
  lines.push("---");
  lines.push("");

  lines.push("This page lists the built-in model aliases used by GitHub Agentic Workflows.");
  lines.push("");
  lines.push(
    "Copilot catalogs can differ by organization and lag Copilot seats; an exact ID missing from a complete live catalog is not offered to this workflow, so prefer `opus` or `sonnet` aliases. Run `gh aw audit <run-url> --models` for detailed model information from that run; see [workflow-specific catalog availability](/gh-aw/reference/engines/#model-aliases). If a model is available but lacks a pricing entry, define per-model pricing with `models.providers` in frontmatter or an imported shared workflow; see [custom model pricing](/gh-aw/reference/faq/#how-do-i-supply-pricing-for-a-custom-or-private-model). Pricing configuration does not make an unavailable model available."
  );
  lines.push("");

  // -------------------------------------------------------------------------
  // Model Aliases
  // -------------------------------------------------------------------------
  lines.push("## Model Aliases");
  lines.push("");
  lines.push(
    "Model aliases let you write `engine: copilot` with a human-friendly model name such as `sonnet` or `mini`, and gh-aw resolves it to the best available concrete model at compile time. Each alias holds an ordered list of patterns; the first pattern that matches an available model wins."
  );
  lines.push("");
  lines.push("For details on the alias syntax, fallback resolution algorithm, and how to define your own aliases in workflow frontmatter, see the [Model Alias Format Specification](/gh-aw/specs/model-alias-specification/).");
  lines.push("");

  lines.push("### Vendor Aliases");
  lines.push("");
  lines.push("Vendor aliases map a short name to one or more provider-scoped glob patterns. The Copilot gateway is always tried first.");
  lines.push("");
  lines.push(generateAliasTable(vendor));
  lines.push("");

  lines.push("### Meta-Aliases");
  lines.push("");
  lines.push("Meta-aliases reference other aliases by name. They are resolved recursively until a concrete pattern is reached.");
  lines.push("");
  lines.push(generateMetaAliasTable(meta));
  lines.push("");

  return lines.join("\n");
}

// ---------------------------------------------------------------------------
// Write output
// ---------------------------------------------------------------------------
console.log("Generating model tables documentation...");

const markdown = generateMarkdown();

const outputDir = path.dirname(OUTPUT_PATH);
if (!fs.existsSync(outputDir)) {
  fs.mkdirSync(outputDir, { recursive: true });
}

fs.writeFileSync(OUTPUT_PATH, markdown, "utf-8");
console.log(`✓ Generated: ${OUTPUT_PATH}`);
