// @ts-check
/// <reference types="@actions/github-script" />

const fs = require("fs");
const path = require("path");

/**
 * @typedef {Object} ValidationResult
 * @property {boolean} valid - Whether all files passed validation
 * @property {string[]} invalidFiles - List of files with invalid extensions
 */

/**
 * Validate that all files in a memory directory have allowed file extensions
 * If allowedExtensions is empty or not provided, all file extensions are allowed
 *
 * @param {string} memoryDir - Path to the memory directory to validate
 * @param {string} [memoryType="cache"] - Type of memory ("cache" or "repo") for error messages
 * @param {string[]} [allowedExtensions] - Optional custom list of allowed extensions (empty array or undefined means allow all files)
 * @returns {ValidationResult} Validation result with list of invalid files
 */
function validateMemoryFiles(memoryDir, memoryType = "cache", allowedExtensions) {
  const allowAll = !allowedExtensions?.length;

  if (allowAll) {
    core.info(`All file extensions are allowed in ${memoryType}-memory directory`);
    return { valid: true, invalidFiles: [] };
  }

  if (!fs.existsSync(memoryDir)) {
    core.info(`Memory directory does not exist: ${memoryDir}`);
    return { valid: true, invalidFiles: [] };
  }

  const extensions = allowedExtensions.map(ext => ext.trim().toLowerCase());
  const invalidFiles = [];

  /**
   * Recursively scan directory for files
   * @param {string} dirPath - Directory to scan
   * @param {string} [relativePath=""] - Relative path from memory directory
   */
  const scanDirectory = (dirPath, relativePath = "") => {
    const entries = fs.readdirSync(dirPath, { withFileTypes: true });

    for (const entry of entries) {
      const fullPath = path.join(dirPath, entry.name);
      const relativeFilePath = relativePath ? path.join(relativePath, entry.name) : entry.name;

      if (entry.isDirectory()) {
        scanDirectory(fullPath, relativeFilePath);
      } else if (entry.isFile()) {
        const ext = path.extname(entry.name).toLowerCase();
        if (!extensions.includes(ext)) {
          invalidFiles.push(relativeFilePath);
        }
      }
    }
  };

  try {
    scanDirectory(memoryDir);
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    core.error(`Failed to scan ${memoryType}-memory directory: ${message}`);
    return { valid: false, invalidFiles: [] };
  }

  if (invalidFiles.length > 0) {
    core.error(`Found ${invalidFiles.length} file(s) with invalid extensions in ${memoryType}-memory:`);
    invalidFiles.forEach(file => {
      const ext = path.extname(file).toLowerCase() || "(no extension)";
      core.error(`  - ${file} (extension: ${ext})`);
    });
    core.error(`Allowed extensions: ${extensions.join(", ")}`);
    return { valid: false, invalidFiles };
  }

  core.info(`All files in ${memoryType}-memory directory have valid extensions`);
  return { valid: true, invalidFiles: [] };
}

module.exports = {
  validateMemoryFiles,
};
