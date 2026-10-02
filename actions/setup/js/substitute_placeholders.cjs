// @ts-check
/// <reference types="@actions/github-script" />

// Ensures global.core is available when running outside github-script context
require("./shim.cjs");
const fs = require("fs");
const path = require("path");
const { getErrorMessage } = require("./error_helpers.cjs");
const { ERR_SYSTEM, ERR_VALIDATION } = require("./error_codes.cjs");

/**
 * Substitutes `__KEY__` placeholders in a file with values from the substitutions map.
 * Undefined/null values are treated as empty strings.
 *
 * @param {{ file: string, substitutions: Record<string, string | null | undefined> }} params
 * @returns {Promise<string>}
 */
const substitutePlaceholders = async ({ file, substitutions }) => {
  // Validate parameters
  if (!file) {
    throw new Error(`${ERR_VALIDATION}: ` + "file parameter is required");
  }
  if (!substitutions || typeof substitutions !== "object") {
    throw new Error(`${ERR_VALIDATION}: ` + "substitutions parameter must be an object");
  }

  core.info(`[substitutePlaceholders] ${file} (${Object.keys(substitutions).length} substitution(s))`);

  const splitPaths = path.basename(file) === "prompt.txt" ? ["system.txt", "user.txt"].map(name => path.join(path.dirname(file), name)) : [];
  const hasSplit = splitPaths.length > 0 && splitPaths.every(splitPath => fs.existsSync(splitPath));
  const files = hasSplit ? [file, ...splitPaths] : [file];

  // Read the files
  const contents = [];
  for (const target of files) {
    let content;
    try {
      content = fs.readFileSync(target, "utf8");
    } catch (error) {
      const errorMessage = getErrorMessage(error);
      throw new Error(`${ERR_SYSTEM}: Failed to read file ${target}: ${errorMessage}`);
    }

    // Perform substitutions
    for (const [key, value] of Object.entries(substitutions)) {
      const placeholder = `__${key}__`;
      // Convert undefined/null to empty string to avoid leaving "undefined" or "null" in the output
      const safeValue = value == null ? "" : value;
      content = content.split(placeholder).join(safeValue);
    }

    contents.push(content);
  }
  if (hasSplit && contents[0] !== contents[1] + contents[2]) {
    throw new Error(`${ERR_VALIDATION}: Split prompt files do not match the combined prompt after substitution`);
  }

  // Write back to the files
  for (const [index, target] of files.entries()) {
    try {
      fs.writeFileSync(target, contents[index], "utf8");
    } catch (error) {
      const errorMessage = getErrorMessage(error);
      throw new Error(`${ERR_SYSTEM}: Failed to write file ${target}: ${errorMessage}`);
    }
  }

  return `Successfully substituted ${Object.keys(substitutions).length} placeholder(s) in ${file}`;
};

module.exports = substitutePlaceholders;
