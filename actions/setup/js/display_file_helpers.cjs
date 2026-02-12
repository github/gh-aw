// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Display File Helper Functions
 *
 * This module provides helper functions for displaying file contents
 * in GitHub Actions logs with collapsible groups and proper formatting.
 */

const fs = require("fs");
const { safeInfo } = require("./sanitized_logging.cjs");

/**
 * Display a single file's content in a collapsible group
 * @param {string} filePath - Full path to the file
 * @param {string} fileName - Display name for the file
 * @param {number} maxBytes - Maximum bytes to display (default: 64KB)
 */
function displayFileContent(filePath, fileName, maxBytes = 64 * 1024) {
  try {
    const stats = fs.statSync(filePath);

    if (stats.isDirectory()) {
      core.info(`  ${fileName}/ (directory)`);
      return;
    }

    // Handle empty files
    if (stats.size === 0) {
      core.info(`  ${fileName} (empty file)`);
      return;
    }

    // Handle files too large to read
    if (stats.size >= 1024 * 1024) {
      core.info(`  ${fileName} (file too large to display, ${stats.size} bytes)`);
      return;
    }

    // Only show content for specific file types
    const displayExtensions = [".json", ".jsonl", ".log", ".txt", ".md", ".yml", ".yaml", ".toml"];
    const fileExtension = fileName.substring(fileName.lastIndexOf(".")).toLowerCase();
    const shouldDisplayContent = displayExtensions.includes(fileExtension);

    if (!shouldDisplayContent) {
      core.info(`  ${fileName} (content not displayed for ${fileExtension} files)`);
      return;
    }

    // Read and display file content
    const content = fs.readFileSync(filePath, "utf8");
    const contentToDisplay = content.length > maxBytes ? content.substring(0, maxBytes) : content;
    const wasTruncated = content.length > maxBytes;

    // Use collapsible group for file content with filename and size as title
    core.startGroup(`${fileName} (${stats.size} bytes)`);
    const lines = contentToDisplay.split("\n");
    for (const line of lines) {
      safeInfo(line);
    }
    if (wasTruncated) {
      core.info(`...`);
      core.info(`(truncated, showing first ${maxBytes} bytes of ${content.length} total)`);
    }
    core.endGroup();
  } catch (/** @type {unknown} */ error) {
    const errorMessage = error instanceof Error ? error.message : String(error);
    core.warning(`Could not display file ${fileName}: ${errorMessage}`);
  }
}

/**
 * Display all files in a directory with collapsible groups
 * @param {string} dirPath - Directory path to display
 * @param {number} maxBytes - Maximum bytes to display per file (default: 64KB)
 */
function displayDirectory(dirPath, maxBytes = 64 * 1024) {
  core.startGroup(`📁 Directory: ${dirPath}`);

  try {
    if (!fs.existsSync(dirPath)) {
      core.notice(`Directory does not exist: ${dirPath}`);
      core.endGroup();
      return;
    }

    const files = fs.readdirSync(dirPath);
    if (files.length === 0) {
      core.info("  (empty directory)");
      core.endGroup();
      return;
    }

    // Display each file
    for (const file of files) {
      const filePath = `${dirPath}/${file}`;
      displayFileContent(filePath, file, maxBytes);
    }
  } catch (/** @type {unknown} */ error) {
    const errorMessage = error instanceof Error ? error.message : String(error);
    core.error(`Error reading directory ${dirPath}: ${errorMessage}`);
  }

  core.endGroup();
}

/**
 * Display multiple directories with their contents
 * @param {string[]} directories - Array of directory paths to display
 * @param {number} maxBytes - Maximum bytes to display per file (default: 64KB)
 */
function displayDirectories(directories, maxBytes = 64 * 1024) {
  core.startGroup("=== Listing All Gateway-Related Files ===");

  for (const dir of directories) {
    displayDirectory(dir, maxBytes);
  }

  core.endGroup();
}

module.exports = {
  displayFileContent,
  displayDirectory,
  displayDirectories,
};
