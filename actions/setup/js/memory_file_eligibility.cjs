// @ts-check
/// <reference types="@actions/github-script" />

const fs = require("fs");
const path = require("path");

const { globPatternToRegex } = require("./glob_pattern_helpers.cjs");

/**
 * Compile a space-separated FILE_GLOB_FILTER string into an array of RegExp patterns.
 * Patterns are matched against the file's path relative to the memory directory root.
 * Slashless patterns (e.g. "*.json") match files directly at that root (depth 0), matching
 * the documented FILE_GLOB_FILTER contract (e.g. "history.jsonl" at the memory directory
 * root matches "*.jsonl"). Patterns containing "/" match the full relative path unchanged.
 *
 * @param {string} fileGlobFilter - Space-separated glob patterns (may be empty)
 * @returns {{ patternStrs: string[], compiledPatterns: RegExp[] }}
 */
function compileFileGlobPatterns(fileGlobFilter) {
  if (!fileGlobFilter) {
    return { patternStrs: [], compiledPatterns: [] };
  }
  const patternStrs = fileGlobFilter.trim().split(/\s+/).filter(Boolean);
  const compiledPatterns = patternStrs.map(pattern => globPatternToRegex(pattern));
  return { patternStrs, compiledPatterns };
}

/**
 * Determine whether a relative file path is eligible for persistence, given a set
 * of allowed extensions and compiled glob patterns. Both allowed-extensions and
 * file-glob act as persistence filters: files that do not pass are ignored (never
 * uploaded, validated, counted, or pushed) rather than causing a hard failure.
 *
 * @param {string} relativeFilePath - File path relative to the memory directory root
 * @param {string[]} allowedExtensions - Allowed extensions (e.g. [".json"]); empty means allow all
 * @param {RegExp[]} compiledPatterns - Compiled glob patterns; empty means allow all
 * @returns {{ eligible: boolean, reason?: string }}
 */
function isMemoryFileEligible(relativeFilePath, allowedExtensions, compiledPatterns) {
  const normalizedRelPath = relativeFilePath.replace(/\\/g, "/");

  if (allowedExtensions.length > 0) {
    const ext = path.extname(relativeFilePath).toLowerCase();
    const allowed = allowedExtensions.map(e => e.trim().toLowerCase());
    if (!allowed.includes(ext)) {
      return { eligible: false, reason: `disallowed extension "${ext || "(none)"}"` };
    }
  }

  if (compiledPatterns.length > 0) {
    if (!compiledPatterns.some(pattern => pattern.test(normalizedRelPath))) {
      return { eligible: false, reason: "no pattern matched" };
    }
  }

  return { eligible: true };
}

/**
 * Recursively scan a memory directory and delete any file that is not eligible
 * for persistence per the allowed-extensions and file-glob filters. Deleted files
 * are logged (not treated as errors) so that downstream validation, artifact
 * upload, and push steps only ever see the same effective file set.
 *
 * @param {string} memoryDir - Path to the memory directory to filter in place
 * @param {string[]} allowedExtensions - Allowed extensions (e.g. [".json"]); empty means allow all
 * @param {string} fileGlobFilter - Space-separated glob patterns; empty means allow all
 * @param {{ info: (message: string) => void }} core - Actions core module
 * @returns {{ kept: string[], removed: Array<{ path: string, reason: string }> }}
 */
function filterIneligibleMemoryFiles(memoryDir, allowedExtensions, fileGlobFilter, core) {
  /** @type {string[]} */
  const kept = [];
  /** @type {Array<{ path: string, reason: string }>} */
  const removed = [];

  if (!fs.existsSync(memoryDir)) {
    return { kept, removed };
  }

  const { compiledPatterns } = compileFileGlobPatterns(fileGlobFilter);

  /**
   * @param {string} dirPath
   * @param {string} relativePath
   */
  const scanDirectory = (dirPath, relativePath = "") => {
    const entries = fs.readdirSync(dirPath, { withFileTypes: true });
    for (const entry of entries) {
      const fullPath = path.join(dirPath, entry.name);
      const relativeFilePath = relativePath ? path.join(relativePath, entry.name) : entry.name;

      if (entry.isDirectory()) {
        if (entry.name === ".git") continue;
        scanDirectory(fullPath, relativeFilePath);
        continue;
      }
      if (!entry.isFile()) continue;

      const normalizedRelPath = relativeFilePath.replace(/\\/g, "/");
      const result = isMemoryFileEligible(relativeFilePath, allowedExtensions, compiledPatterns);
      if (!result.eligible) {
        core.info(`  [ignore] ${normalizedRelPath} — ${result.reason}`);
        removed.push({ path: normalizedRelPath, reason: result.reason || "ineligible" });
        fs.rmSync(fullPath, { force: true });
        continue;
      }
      kept.push(normalizedRelPath);
    }
  };

  scanDirectory(memoryDir);

  if (removed.length > 0) {
    core.info(`Ignored ${removed.length} ineligible file(s) before validation/upload:`);
    removed.forEach(f => core.info(`  - ${f.path} (${f.reason})`));
  }

  return { kept, removed };
}

/**
 * Entry point used by the compiler's "Filter memory files" step (via
 * generateGitHubScriptWithRequire). Reads MEMORY_DIR, ALLOWED_EXTENSIONS
 * (JSON array), and FILE_GLOB_FILTER from the environment and filters the
 * memory directory in place, relying on the `core` global set up by
 * setup_globals.cjs.
 */
function main() {
  const memoryDir = process.env.MEMORY_DIR || "";
  const allowedExtensions = JSON.parse(process.env.ALLOWED_EXTENSIONS || "[]");
  const fileGlobFilter = process.env.FILE_GLOB_FILTER || "";
  filterIneligibleMemoryFiles(memoryDir, allowedExtensions, fileGlobFilter, core);
}

module.exports = { compileFileGlobPatterns, isMemoryFileEligible, filterIneligibleMemoryFiles, main };
