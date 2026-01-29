#!/usr/bin/env node

/**
 * Schema Documentation Generator
 *
 * Generates markdown documentation from the main workflow schema JSON file.
 * Creates a comprehensive YAML reference with inline comments showing descriptions
 * and optional hints.
 *
 * Usage:
 *   node scripts/generate-schema-docs.js
 */

import fs from "fs";
import path from "path";
import { fileURLToPath } from "url";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

// Paths
const SCHEMA_PATH = path.join(__dirname, "../pkg/parser/schemas/main_workflow_schema.json");
const OUTPUT_PATH = path.join(__dirname, "../docs/src/content/docs/reference/frontmatter-full.md");

// Read and parse the schema
const schema = JSON.parse(fs.readFileSync(SCHEMA_PATH, "utf-8"));

/**
 * Resolve a $ref reference in the schema
 * @param {string} ref - The $ref value (e.g., "#/$defs/engine_config")
 * @returns {object|null} - The resolved schema object or null if not found
 */
function resolveRef(ref) {
  if (!ref || typeof ref !== "string") {
    return null;
  }

  // Handle JSON pointer references (e.g., "#/$defs/engine_config" or "#/properties/permissions")
  if (!ref.startsWith("#/")) {
    console.warn(`Unsupported $ref format: ${ref}`);
    return null;
  }

  const path = ref.substring(2).split("/"); // Remove '#/' and split by '/'
  let current = schema;

  for (const segment of path) {
    if (current && typeof current === "object" && segment in current) {
      current = current[segment];
    } else {
      console.warn(`Could not resolve $ref: ${ref} (failed at segment: ${segment})`);
      return null;
    }
  }

  return current;
}

/**
 * Resolve a property that may contain a $ref
 * @param {object} prop - The property object that may contain a $ref
 * @returns {object} - The resolved property object with $ref merged
 */
function resolvePropertyRef(prop) {
  if (!prop || typeof prop !== "object") {
    return prop;
  }

  // If the property has a $ref, resolve it
  if (prop.$ref) {
    const resolved = resolveRef(prop.$ref);
    if (resolved) {
      // Merge the resolved schema with the original property
      // The original property's description takes precedence
      return {
        ...resolved,
        ...prop,
        $ref: undefined, // Remove the $ref after resolving
      };
    }
  }

  return prop;
}

/**
 * Format a description as YAML comment
 */
function formatComment(text, indent = 0) {
  if (!text) return "";
  const prefix = " ".repeat(indent) + "# ";
  // Split long lines into multiple comment lines
  const words = text.split(" ");
  const lines = [];
  let currentLine = "";

  for (const word of words) {
    if (currentLine.length + word.length + 1 > 80) {
      lines.push(prefix + currentLine);
      currentLine = word;
    } else {
      currentLine += (currentLine ? " " : "") + word;
    }
  }
  if (currentLine) {
    lines.push(prefix + currentLine);
  }

  return lines.join("\n");
}

/**
 * Generate YAML example value based on schema type
 */
function getExampleValue(prop, propName = "") {
  if (prop.enum && prop.enum.length > 0) {
    return prop.enum[0];
  }

  switch (prop.type) {
    case "string":
      if (propName === "github-token") return "${{ secrets.GITHUB_TOKEN }}";
      if (propName === "name") return "My Workflow";
      if (propName === "description") return "Description of the workflow";
      return "example-value";
    case "number":
    case "integer":
      if (propName === "timeout_minutes") return 10;
      return 1;
    case "boolean":
      return true;
    case "array":
      return [];
    case "object":
      return {};
    case "null":
      return null;
    default:
      return null;
  }
}

/**
 * Generate YAML for a property with variants (oneOf/anyOf)
 */
function generateVariants(prop, propName, indent = 0, required = []) {
  // Resolve $ref if present
  prop = resolvePropertyRef(prop);

  const indentStr = " ".repeat(indent);
  const lines = [];
  const isRequired = required.includes(propName);

  // Add main description
  if (prop.description) {
    lines.push(formatComment(prop.description, indent));
  }

  if (!isRequired) {
    lines.push(formatComment("(optional)", indent));
  }

  // Handle oneOf/anyOf
  const variants = prop.oneOf || prop.anyOf;
  const variantType = prop.oneOf ? "oneOf" : "anyOf";

  if (variants && variants.length > 1) {
    lines.push(formatComment(`This field supports multiple formats (${variantType}):`, indent));

    variants.forEach((variant, index) => {
      lines.push("");
      lines.push(formatComment(`Option ${index + 1}: ${variant.description || variant.type}`, indent));

      if (variant.type === "string") {
        const example = getExampleValue(variant, propName);
        lines.push(`${indentStr}${propName}: ${JSON.stringify(example)}`);
      } else if (variant.type === "object") {
        lines.push(`${indentStr}${propName}:`);
        if (variant.properties) {
          const subLines = generateProperties(variant.properties, variant.required || [], indent + 2);
          lines.push(subLines);
        } else {
          lines.push(`${indentStr}  {}`);
        }
      } else if (variant.type === "array") {
        lines.push(`${indentStr}${propName}: []`);
        if (variant.items) {
          lines.push(formatComment(`Array items: ${variant.items.description || variant.items.type}`, indent + 2));
        }
      } else if (variant.type === "boolean") {
        lines.push(`${indentStr}${propName}: true`);
      } else if (variant.type === "null") {
        lines.push(`${indentStr}${propName}: null`);
      } else if (variant.type === "number" || variant.type === "integer") {
        const example = getExampleValue(variant, propName);
        lines.push(`${indentStr}${propName}: ${example}`);
      }
    });
  } else if (variants && variants.length === 1) {
    // Single variant, just render it normally
    lines.push(...generateProperty(propName, variants[0], indent, isRequired));
  } else {
    // No variants, render the property directly
    lines.push(...generateProperty(propName, prop, indent, isRequired));
  }

  return lines.join("\n");
}

/**
 * Generate YAML for a single property
 */
function generateProperty(propName, prop, indent = 0, isRequired = false) {
  // Resolve $ref if present
  prop = resolvePropertyRef(prop);

  const indentStr = " ".repeat(indent);
  const lines = [];

  // Add description as comment
  if (prop.description) {
    lines.push(formatComment(prop.description, indent));
  }

  if (!isRequired) {
    lines.push(formatComment("(optional)", indent));
  }

  // Generate the YAML
  if (prop.type === "object") {
    lines.push(`${indentStr}${propName}:`);
    if (prop.properties) {
      const subLines = generateProperties(prop.properties, prop.required || [], indent + 2);
      lines.push(subLines);
    } else {
      lines.push(`${indentStr}  {}`);
    }
  } else if (prop.type === "array") {
    const example = getExampleValue(prop, propName);
    lines.push(`${indentStr}${propName}: []`);
    if (prop.items) {
      if (prop.items.type === "string") {
        lines.push(formatComment(`Array of ${prop.items.description || "strings"}`, indent + 2));
      } else if (prop.items.type === "object") {
        lines.push(formatComment("Array items:", indent + 2));
        if (prop.items.properties) {
          const subLines = generateProperties(prop.items.properties, prop.items.required || [], indent + 4);
          lines.push(subLines);
        }
      }
    }
  } else {
    const example = getExampleValue(prop, propName);
    const value = typeof example === "string" ? JSON.stringify(example) : example;
    lines.push(`${indentStr}${propName}: ${value}`);
  }

  return lines;
}

/**
 * Generate YAML for all properties
 */
function generateProperties(properties, required = [], indent = 0) {
  const lines = [];
  let addedCount = 0;

  Object.entries(properties).forEach(([propName, prop]) => {
    // Resolve $ref before checking for deprecated flag
    const resolvedProp = resolvePropertyRef(prop);

    // Skip deprecated properties
    if (resolvedProp.deprecated === true) {
      return;
    }

    if (addedCount > 0) {
      lines.push("");
    }

    const isRequired = required.includes(propName);

    // Check if property has variants
    if (resolvedProp.oneOf || resolvedProp.anyOf) {
      lines.push(generateVariants(resolvedProp, propName, indent, required));
    } else {
      lines.push(...generateProperty(propName, resolvedProp, indent, isRequired));
    }

    addedCount++;
  });

  return lines.join("\n");
}

/**
 * Generate the full markdown documentation
 */
function generateMarkdown() {
  const lines = [];

  // Frontmatter
  lines.push("---");
  lines.push("title: Frontmatter Reference");
  lines.push("description: Complete JSON Schema-based reference for all GitHub Agentic Workflows frontmatter configuration options with YAML examples.");
  lines.push("sidebar:");
  lines.push("  order: 201");
  lines.push("---");
  lines.push("");

  // Introduction
  lines.push(
    "This document provides a comprehensive reference for all available frontmatter configuration options in GitHub Agentic Workflows. The examples below are generated from the JSON Schema and include inline comments describing each field."
  );
  lines.push("");
  lines.push("> [!NOTE]");
  lines.push("> This documentation is automatically generated from the JSON Schema. For a more user-friendly guide, see [Frontmatter](/gh-aw/reference/frontmatter/).");
  lines.push("");

  // Schema description
  if (schema.description) {
    lines.push(`## Schema Description`);
    lines.push("");
    lines.push(schema.description);
    lines.push("");
  }

  // Full YAML example
  lines.push("## Complete Frontmatter Reference");
  lines.push("");
  lines.push("```yaml wrap");
  lines.push("---");

  const yamlContent = generateProperties(schema.properties, schema.required || [], 0);
  lines.push(yamlContent);

  lines.push("---");
  lines.push("```");
  lines.push("");

  // Footer
  lines.push("## Additional Information");
  lines.push("");
  lines.push("- Fields marked with `(optional)` are not required");
  lines.push("- Fields with multiple options show all possible formats");
  lines.push("- See the [Frontmatter guide](/gh-aw/reference/frontmatter/) for detailed explanations and examples");
  lines.push("- See individual reference pages for specific topics like [Triggers](/gh-aw/reference/triggers/), [Tools](/gh-aw/reference/tools/), and [Safe Outputs](/gh-aw/reference/safe-outputs/)");
  lines.push("");

  return lines.join("\n");
}

// Main execution
console.log("Generating schema documentation...");
const markdown = generateMarkdown();

// Ensure output directory exists
const outputDir = path.dirname(OUTPUT_PATH);
if (!fs.existsSync(outputDir)) {
  fs.mkdirSync(outputDir, { recursive: true });
}

// Write the output
fs.writeFileSync(OUTPUT_PATH, markdown, "utf-8");
console.log(`✓ Generated documentation: ${OUTPUT_PATH}`);
