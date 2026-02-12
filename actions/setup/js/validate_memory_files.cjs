// @ts-check
/// <reference types="@actions/github-script" />

const fs = require("fs");
const path = require("path");

/**
 * Validate that all files in a memory directory have allowed file extensions
 * Allowed extensions: .json, .jsonl, .txt, .md, .csv
 *
 * @param {string} memoryDir - Path to the memory directory to validate
 * @param {string} memoryType - Type of memory ("cache" or "repo") for error messages
 * @returns {{valid: boolean, invalidFiles: string[]}} Validation result with list of invalid files
 */
function validateMemoryFiles(memoryDir, memoryType = "cache") {
  const allowedExtensions = [".json", ".jsonl", ".txt", ".md", ".csv"];
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
        if (!allowedExtensions.includes(ext)) {
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
    core.error(`Allowed extensions: ${allowedExtensions.join(", ")}`);
    return { valid: false, invalidFiles };
  }

  core.info(`All files in ${memoryType}-memory directory have valid extensions`);
  return { valid: true, invalidFiles: [] };
}

module.exports = {
  validateMemoryFiles,
};
