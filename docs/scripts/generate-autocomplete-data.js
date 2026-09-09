#!/usr/bin/env node
/**
 * Generates a compact autocomplete data file from the main workflow JSON Schema.
 *
 * Usage:
 *   node docs/scripts/generate-autocomplete-data.js
 *
 * Input:  pkg/parser/schemas/main_workflow_schema.json
 * Output: docs/public/editor/autocomplete-data.json
 */

import fs from "fs";
import path from "path";
import { fileURLToPath } from "url";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const SCHEMA_PATH = path.resolve(__dirname, "../../pkg/parser/schemas/main_workflow_schema.json");
const OUTPUT_PATH = path.resolve(__dirname, "../public/editor/autocomplete-data.json");

const MAX_DEPTH = 3;

// Sections where we limit child recursion to just 1 level (key + desc/type only)
const SHALLOW_SECTIONS = new Set(["safe-outputs", "on"]);

const schema = JSON.parse(fs.readFileSync(SCHEMA_PATH, "utf-8"));
const defs = schema.$defs || {};

// Priority ordering for root-level keys in suggestions
const ROOT_SORT_ORDER = [
  "name",
  "description",
  "intent",
  "on",
  "engine",
  "permissions",
  "tools",
  "safe-outputs",
  "mcp-servers",
  "env",
  "imports",
  "command",
  "cache",
  "labels",
  "metadata",
  "tracker-id",
  "source",
  "run-name",
  "runs-on",
  "timeout-minutes",
  "concurrency",
  "environment",
  "container",
  "services",
  "network",
  "sandbox",
  "plugins",
  "if",
  "steps",
  "post-steps",
  "features",
  "secrets",
  "secret-masking",
  "bots",
  "user-rate-limit",
  "strict",
  "safe-inputs",
  "runtimes",
  "jobs",
];

/**
 * Resolve $ref pointers to their definitions.
 */
function resolveRef(node) {
  if (!node || typeof node !== "object") return node;
  if (node.$ref) {
    const refPath = node.$ref.replace("#/$defs/", "");
    const resolved = defs[refPath];
    if (!resolved) return node;
    return node.description ? { ...resolved, description: node.description } : resolved;
  }
  return node;
}

/**
 * Extract a compact type string from a schema node.
 */
function getType(node) {
  if (!node) return "unknown";
  if (node.type) {
    if (Array.isArray(node.type)) return node.type.join("|");
    return node.type;
  }
  if (node.oneOf) {
    const types = node.oneOf.map(o => resolveRef(o).type).filter(Boolean);
    const unique = [...new Set(types.flat())];
    return unique.join("|") || "unknown";
  }
  if (node.enum) return "string";
  return "unknown";
}

/**
 * Collect enum values from a schema node, including through oneOf.
 */
function getEnum(node) {
  if (node.enum) return node.enum;
  if (node.oneOf) {
    for (const variant of node.oneOf) {
      const resolved = resolveRef(variant);
      if (resolved.enum) return resolved.enum;
    }
  }
  return null;
}

/**
 * Truncate description to a reasonable length.
 */
function truncDesc(desc) {
  if (!desc) return undefined;
  const firstSentence = desc.split(/\.\s/)[0];
  if (firstSentence.length <= 120) return firstSentence + (desc.length > firstSentence.length ? "." : "");
  return desc.substring(0, 117) + "...";
}

/**
 * Determine if this schema node represents an object with known children.
 */
function getObjectProperties(node) {
  const resolved = resolveRef(node);
  if (resolved.properties) return resolved.properties;

  if (resolved.oneOf) {
    for (const variant of resolved.oneOf) {
      const r = resolveRef(variant);
      if (r.type === "object" && r.properties) return r.properties;
    }
  }
  return null;
}

/**
 * Check if a schema node can be a simple leaf (string, number, boolean, etc.)
 */
function canBeLeaf(node) {
  const resolved = resolveRef(node);
  const type = resolved.type;
  if (type === "string" || type === "integer" || type === "number" || type === "boolean") return true;
  if (Array.isArray(type) && type.some(t => t === "string" || t === "integer" || t === "number" || t === "boolean")) return true;
  if (resolved.oneOf) {
    return resolved.oneOf.some(v => {
      const r = resolveRef(v);
      return r.type === "string" || r.type === "integer" || r.type === "number" || r.type === "boolean" || r.type === "null";
    });
  }
  return false;
}

/**
 * Check if a node can be an array.
 */
function canBeArray(node) {
  const resolved = resolveRef(node);
  if (resolved.type === "array") return true;
  if (resolved.oneOf) {
    return resolved.oneOf.some(v => resolveRef(v).type === "array");
  }
  return false;
}

/**
 * Build a compact autocomplete entry from a schema property.
 */
function buildEntry(propSchema, depth) {
  const resolved = resolveRef(propSchema);
  const entry = {};

  const type = getType(resolved);
  if (type !== "unknown") entry.type = type;

  const desc = truncDesc(resolved.description);
  if (desc) entry.desc = desc;

  const enumVals = getEnum(resolved);
  if (enumVals) {
    const filtered = enumVals.filter(v => v != null);
    if (filtered.length > 0) entry.enum = filtered;
  }

  if (type === "boolean" && !entry.enum) {
    entry.enum = [true, false];
  }

  if (resolved.deprecated === true) {
    entry.deprecated = true;
  }

  if (typeof resolved["x-deprecation-message"] === "string" && resolved["x-deprecation-message"]) {
    entry["x-deprecation-message"] = resolved["x-deprecation-message"];
  }

  if (depth < MAX_DEPTH) {
    const childProps = getObjectProperties(resolved);
    if (childProps) {
      const children = {};
      for (const [key, childSchema] of Object.entries(childProps)) {
        children[key] = buildEntry(childSchema, depth + 1);
      }
      if (Object.keys(children).length > 0) {
        entry.children = children;
      }
    }
  }

  if (!entry.children && canBeLeaf(resolved)) {
    entry.leaf = true;
  }

  if (canBeArray(resolved)) {
    entry.array = true;
  }

  return entry;
}

// Build root-level entries
const root = {};
for (const [key, propSchema] of Object.entries(schema.properties)) {
  // For shallow sections, limit recursion depth so children don't bloat the output
  const effectiveDepth = SHALLOW_SECTIONS.has(key) ? MAX_DEPTH - 1 : 0;
  root[key] = buildEntry(propSchema, effectiveDepth);
}

const output = {
  root,
  sortOrder: ROOT_SORT_ORDER,
};

fs.writeFileSync(OUTPUT_PATH, JSON.stringify(output, null, 2));
const stats = fs.statSync(OUTPUT_PATH);
console.log(`Generated ${OUTPUT_PATH}`);
console.log(`  Size: ${(stats.size / 1024).toFixed(1)} KB`);
console.log(`  Root keys: ${Object.keys(root).length}`);
