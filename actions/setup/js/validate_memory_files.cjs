// @ts-check
/// <reference types="@actions/github-script" />

const fs = require("fs");
const path = require("path");

/**
 * Validate that all files in a memory directory have allowed file extensions
 * Default allowed extensions: .json, .jsonl, .txt, .md, .csv
 *
 * @param {string} memoryDir - Path to the memory directory to validate
 * @param {string} memoryType - Type of memory ("cache" or "repo") for error messages
 * @param {string[]} [allowedExtensions] - Optional custom list of allowed extensions (defaults to [".json", ".jsonl", ".txt", ".md", ".csv"])
 * @returns {{valid: boolean, invalidFiles: string[]}} Validation result with list of invalid files
 */
function validateMemoryFiles(memoryDir, memoryType = "cache", allowedExtensions) {
  // Use default extensions if not provided or if empty array
  const defaultExtensions = [".json", ".jsonl", ".txt", ".md", ".csv"];
  const rawExtensions = allowedExtensions && allowedExtensions.length > 0 ? allowedExtensions : defaultExtensions;
  // Normalize extensions to lowercase and trim whitespace
  const extensions = rawExtensions.map(ext => ext.trim().toLowerCase());
  const invalidFiles = [];

  // Check if directory exists
  if (!fs.existsSync(memoryDir)) {
    core.info(`Memory directory does not exist: ${memoryDir}`);
    return { valid: true, invalidFiles: [] };
  }

  /**
   * Recursively scan directory for files
   * @param {string} dirPath - Directory to scan
   * @param {string} relativePath - Relative path from memory directory
   */
  function scanDirectory(dirPath, relativePath = "") {
    const entries = fs.readdirSync(dirPath, { withFileTypes: true });

    for (const entry of entries) {
      const fullPath = path.join(dirPath, entry.name);
      const relativeFilePath = relativePath ? path.join(relativePath, entry.name) : entry.name;

      if (entry.isDirectory()) {
        // Recursively scan subdirectory
        scanDirectory(fullPath, relativeFilePath);
      } else if (entry.isFile()) {
        // Check file extension
        const ext = path.extname(entry.name).toLowerCase();
        if (!extensions.includes(ext)) {
          invalidFiles.push(relativeFilePath);
        }
      }
    }
  }

  try {
    scanDirectory(memoryDir);
  } catch (error) {
    core.error(`Failed to scan ${memoryType}-memory directory: ${error instanceof Error ? error.message : String(error)}`);
    return { valid: false, invalidFiles: [] };
  }

  if (invalidFiles.length > 0) {
    core.error(`Found ${invalidFiles.length} file(s) with invalid extensions in ${memoryType}-memory:`);
    invalidFiles.forEach(file => {
      const ext = path.extname(file).toLowerCase();
      core.error(`  - ${file} (extension: ${ext || "(no extension)"})`);
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
