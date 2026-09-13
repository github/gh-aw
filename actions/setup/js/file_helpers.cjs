// @ts-check
/// <reference types="@actions/github-script" />

/**
 * File Helper Functions
 *
 * This module provides helper functions for file operations,
 * particularly for listing directories and checking file existence
 * with helpful error messages.
 */

const fs = require("fs");
const path = require("path");
const { getErrorMessage } = require("./error_helpers.cjs");
const { ERR_SYSTEM } = require("./error_codes.cjs");

/**
 * Return the candidate paths that exist as regular files, preserving the caller's
 * preference order. Use this when the same file may be published in several
 * artifact layouts (for example the compact usage artifact and the agent artifact).
 *
 * @param {string[]} candidatePaths - Candidate file paths, in preference order
 * @returns {string[]} The subset of candidates that exist
 */
function findExistingFiles(candidatePaths) {
  return candidatePaths.filter(candidate => {
    try {
      return fs.statSync(candidate).isFile();
    } catch {
      return false;
    }
  });
}

/**
 * Return the first candidate path that exists as a regular file, or undefined when
 * none of them do.
 *
 * @param {string[]} candidatePaths - Candidate file paths, in preference order
 * @returns {string | undefined} The first existing candidate
 */
function findFirstExistingFile(candidatePaths) {
  return findExistingFiles(candidatePaths)[0];
}

/**
 * List all files recursively in a directory
 * @param {string} dirPath - The directory path to list
 * @param {string} [relativeTo] - Optional base path to show relative paths
 * @returns {string[]} Array of file paths
 */
function listFilesRecursively(dirPath, relativeTo) {
  const files = [];
  try {
    if (!fs.existsSync(dirPath)) {
      return files;
    }
    const entries = fs.readdirSync(dirPath, { withFileTypes: true });
    for (const entry of entries) {
      const fullPath = path.join(dirPath, entry.name);
      if (entry.isDirectory()) {
        files.push(...listFilesRecursively(fullPath, relativeTo));
      } else {
        const displayPath = relativeTo ? path.relative(relativeTo, fullPath) : fullPath;
        files.push(displayPath);
      }
    }
  } catch (error) {
    core.warning("Failed to list files in " + dirPath + ": " + getErrorMessage(error));
  }
  return files;
}

/**
 * Check if file exists and provide helpful error message with directory listing
 * @param {string} filePath - The file path to check
 * @param {string} artifactDir - The artifact directory to list if file not found
 * @param {string} fileDescription - Description of the file (e.g., "Prompt file", "Agent output file")
 * @param {boolean} required - Whether the file is required
 * @param {boolean} [continueOnError=false] - Whether missing required files should warn instead of failing
 * @returns {boolean} True if file exists (or not required), false otherwise
 */
function checkFileExists(filePath, artifactDir, fileDescription, required, continueOnError = false) {
  if (fs.existsSync(filePath)) {
    try {
      const stats = fs.statSync(filePath);
      const fileInfo = filePath + " (" + stats.size + " bytes)";
      core.info(fileDescription + " found: " + fileInfo);
      return true;
    } catch (error) {
      core.warning("Failed to stat " + fileDescription.toLowerCase() + ": " + getErrorMessage(error));
      return false;
    }
  } else {
    if (required) {
      if (!continueOnError) {
        core.error("❌ " + fileDescription + " not found at: " + filePath);
      }
      // List all files in artifact directory for debugging
      core.info("📁 Listing all files in artifact directory: " + artifactDir);
      const files = listFilesRecursively(artifactDir, artifactDir);
      if (files.length === 0) {
        core.warning("  No files found in " + artifactDir);
      } else {
        core.info("  Found " + files.length + " file(s):");
        files.forEach(file => core.info("    - " + file));
      }
      if (continueOnError) {
        core.warning(`${ERR_SYSTEM}: ❌ ${fileDescription} not found at: ${filePath}. Continuing because GH_AW_DETECTION_CONTINUE_ON_ERROR=true`);
      } else {
        core.setFailed(`${ERR_SYSTEM}: ❌ ${fileDescription} not found at: ${filePath}`);
      }
      return false;
    } else {
      core.info("No " + fileDescription.toLowerCase() + " found at: " + filePath);
      return true;
    }
  }
}

module.exports = { listFilesRecursively, checkFileExists, findExistingFiles, findFirstExistingFile };
