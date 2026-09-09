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
 * Get the value schema for an object with dynamic keys.
 */
function getDynamicProperty(prop) {
  if (prop.additionalProperties && typeof prop.additionalProperties === "object") {
    return resolvePropertyRef(prop.additionalProperties);
  }

  const patternProperties = Object.values(prop.patternProperties || {});
  if (patternProperties.length === 1 && typeof patternProperties[0] === "object") {
    return resolvePropertyRef(patternProperties[0]);
  }

  return null;
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
      if (propName === "docs") return "https://docs.example.com/automation/repository-health";
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
 * Returns true if a schema variant is a constraint-only annotation (e.g. a
 * mutual-exclusion rule expressed with required/not) rather than a real input
 * shape. We check the unresolved variant so that $ref entries—which always
 * represent real input shapes—are never classified as constraint-only even when
 * their type/description lives inside the referenced definition.
 * @param {object} variant - Unresolved schema variant object
 */
function isConstraintOnlyVariant(variant) {
  return !variant.type && !variant.description && !variant.$ref;
}

/**
 * Returns a human-readable label for a resolved schema variant, used as the
 * "Format N: <label>" suffix.
 * Priority: variant description → variant type → "Configuration object" for
 * complex nested schemas (e.g. $ref definitions that contain their own oneOf
 * union) → null if nothing is available.
 * @param {object} resolvedVariant - Variant object after $ref resolution
 * @returns {string|null}
 */
function getVariantLabel(resolvedVariant) {
  if (resolvedVariant.description) return resolvedVariant.description;
  if (resolvedVariant.type) return resolvedVariant.type;
  if (resolvedVariant.oneOf || resolvedVariant.anyOf) return "Configuration object";
  return null;
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

  // Constraint-only variants (e.g. mutual-exclusion rules using required/not/anyOf
  // without a type, description, or $ref) are schema validation annotations, not
  // alternate input shapes. Check the unresolved variants so that $ref entries
  // are not mistakenly classified as constraints (their type/description lives
  // inside the referenced definition).
  // When every variant is constraint-only, render the property normally using its
  // top-level type/properties instead.
  const allConstraints = variants && variants.length > 0 && variants.every(isConstraintOnlyVariant);

  // Resolve $refs so that labels (Format N: <desc>) show the referenced description.
  const resolvedVariants = variants ? variants.map(resolvePropertyRef) : variants;

  if (resolvedVariants && resolvedVariants.length > 1 && !allConstraints) {
    lines.push(formatComment(`Accepted formats:`, indent));

    resolvedVariants.forEach((variant, index) => {
      // Build a human-readable label for this format entry.
      const variantLabel = getVariantLabel(variant);

      // Skip variants that have no label and no renderable content.
      if (!variantLabel) return;

      lines.push("");
      lines.push(formatComment(`Format ${index + 1}: ${variantLabel}`, indent));

      if (variant.type === "string") {
        const example = getExampleValue(variant, propName);
        lines.push(`${indentStr}${propName}: ${JSON.stringify(example)}`);
      } else if (variant.type === "object") {
        lines.push(`${indentStr}${propName}:`);
        if (variant.properties) {
          const subLines = generateProperties(variant.properties, variant.required || [], indent + 2);
          lines.push(subLines);
        } else if (variant["x-example-key"] && getDynamicProperty(variant)) {
          const exampleKey = variant["x-example-key"];
          const addlProp = getDynamicProperty(variant);
          if (addlProp.description) {
            lines.push(formatComment(addlProp.description, indent + 2));
          }
          lines.push(`${indentStr}  ${exampleKey}:`);
          if (addlProp.properties) {
            const subLines = generateProperties(addlProp.properties, addlProp.required || [], indent + 4);
            lines.push(subLines);
          } else {
            lines.push(`${indentStr}    {}`);
          }
        } else {
          lines.push(`${indentStr}  {}`);
        }
      } else if (variant.type === "array") {
        const example = variant.examples?.find(Array.isArray) || [];
        lines.push(`${indentStr}${propName}: ${JSON.stringify(example)}`);
        if (variant.items) {
          const items = resolvePropertyRef(variant.items);
          lines.push(formatComment(`Array items: ${items.description || items.type}`, indent + 2));
        }
      } else if (variant.type === "boolean") {
        const boolExample = getExampleValue(variant, propName);
        lines.push(`${indentStr}${propName}: ${boolExample}`);
      } else if (variant.type === "null") {
        lines.push(`${indentStr}${propName}: null`);
      } else if (variant.type === "number" || variant.type === "integer") {
        const example = getExampleValue(variant, propName);
        lines.push(`${indentStr}${propName}: ${example}`);
      }
    });
  } else if (resolvedVariants && resolvedVariants.length === 1 && !allConstraints) {
    // Single variant, just render it normally
    lines.push(...generateProperty(propName, resolvedVariants[0], indent, isRequired));
  } else {
    // No variants, or all variants are constraint-only (validation annotations):
    // render the property directly using its top-level type/properties.
    // Strip description from the prop since generateVariants already emitted
    // the description comment above. Pass isRequired=true to also suppress the
    // redundant "(optional)" comment that was already emitted.
    lines.push(...generateProperty(propName, { ...prop, description: undefined }, indent, true));
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
    } else if (prop["x-example-key"] && getDynamicProperty(prop)) {
      // Dynamic-key object (additionalProperties pattern): expand an annotated example entry
      // so users can see the per-key schema rather than a bare '{}'.
      const exampleKey = prop["x-example-key"];
      const addlProp = getDynamicProperty(prop);
      if (addlProp.description) {
        lines.push(formatComment(addlProp.description, indent + 2));
      }
      lines.push(`${indentStr}  ${exampleKey}:`);
      if (addlProp.properties) {
        const subLines = generateProperties(addlProp.properties, addlProp.required || [], indent + 4);
        lines.push(subLines);
      } else {
        lines.push(`${indentStr}    {}`);
      }
    } else {
      lines.push(`${indentStr}  {}`);
    }
  } else if (prop.type === "array") {
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

    // Skip internal-only properties (marked with "x-internal": true in the schema).
    // These are implementation/debugging details not intended for end users.
    // Required fields are still rendered so that generated YAML examples remain schema-valid.
    const isRequired = required.includes(propName);
    if (resolvedProp["x-internal"] === true && !isRequired) {
      return;
    }
    if (addedCount > 0) {
      lines.push("");
    }

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
