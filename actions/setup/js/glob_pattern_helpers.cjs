// @ts-check

/**
 * Internal helper to escape special regex characters in a pattern
 * @param {string} pattern - Pattern to escape
 * @returns {string} - Pattern with special characters escaped
 * @private
 */
function escapeRegexChars(pattern) {
  // Escape backslashes first, then dots, then other special chars
  return pattern
    .replace(/\\/g, "\\\\") // Escape backslashes
    .replace(/\./g, "\\.") // Escape dots
    .replace(/[+?^${}()|[\]]/g, "\\$&"); // Escape other special regex chars (except * which is handled separately)
}

/**
 * Convert a glob pattern to a RegExp
 * @param {string} pattern - Glob pattern (e.g., "*.json", "metrics/**", "data/**\/*.csv")
 * @param {Object} [options] - Options for pattern conversion
 * @param {boolean} [options.pathMode=true] - If true, * matches non-slash chars; if false, * matches any char
 * @param {boolean} [options.caseSensitive=true] - Whether matching should be case-sensitive
 * @param {boolean} [options.matchSubfolderRoot=false] - If true and the pattern contains no "/",
 *   the pattern is required to be prefixed by exactly one path segment (i.e. matches only at the
 *   root of a single memory subfolder: "subfolder/file.json").  Files at the artifact root (depth 0)
 *   and files deeper than one level (depth 2+) are not matched.
 * @returns {RegExp} - Regular expression that matches the pattern
 *
 * Supports:
 * - * matches any characters except / (in path mode) or any characters (in simple mode)
 * - ** matches any characters including / (only in path mode)
 * - . is escaped to match literal dots
 * - \ is escaped properly
 *
 * @example
 * const regex = globPatternToRegex("*.json");
 * regex.test("file.json"); // true
 * regex.test("file.txt"); // false
 *
 * @example
 * const regex = globPatternToRegex("metrics/**");
 * regex.test("metrics/data.json"); // true
 * regex.test("metrics/daily/data.json"); // true
 *
 * @example
 * const regex = globPatternToRegex("*.json", { matchSubfolderRoot: true });
 * regex.test("discussion-task-miner/processed-discussions.json"); // true  (depth 1)
 * regex.test("file.json"); // false  (depth 0 – not matched)
 * regex.test("discussion-task-miner/archive/old.json"); // false  (depth 2 – not matched)
 */
function globPatternToRegex(pattern, options) {
  const { pathMode = true, caseSensitive = true, matchSubfolderRoot = false } = options || {};

  let regexPattern = escapeRegexChars(pattern);

  if (pathMode) {
    // Path mode: handle ** and * differently
    regexPattern = regexPattern.replace(/\*\*|\*/g, wildcard => (wildcard === "**" ? ".*" : "[^/]*"));
  } else {
    // Simple mode: * matches any character
    regexPattern = regexPattern.replace(/\*/g, ".*");
  }

  // matchSubfolderRoot: slashless patterns must match exactly at the root of one subfolder
  // (depth 1 only – neither depth 0 nor depth 2+).
  // Patterns that already contain a "/" are matched against the full relative path unchanged.
  if (matchSubfolderRoot && !pattern.includes("/")) {
    regexPattern = `[^/]+/${regexPattern}`;
  }

  // eslint-disable-next-line gh-aw-custom/require-escaped-regexp-interpolation -- regexPattern is intentionally built as a regex: * and ** are replaced with regex patterns after escaping all other metacharacters
  return new RegExp(`^${regexPattern}$`, caseSensitive ? "" : "i");
}

/**
 * Parse a space-separated list of glob patterns into RegExp objects
 * @param {string} fileGlobFilter - Space-separated glob patterns (e.g., "*.json *.jsonl *.csv *.md")
 * @returns {RegExp[]} - Array of regular expressions
 *
 * @example
 * const patterns = parseGlobPatterns("*.json *.jsonl");
 * patterns[0].test("file.json"); // true
 * patterns[1].test("file.jsonl"); // true
 */
function parseGlobPatterns(fileGlobFilter) {
  return fileGlobFilter
    .trim()
    .split(/\s+/)
    .filter(Boolean)
    .map(pattern => globPatternToRegex(pattern));
}

/**
 * Check if a file path matches any of the provided glob patterns
 * @param {string} filePath - File path to test (e.g., "data/file.json")
 * @param {string} fileGlobFilter - Space-separated glob patterns
 * @returns {boolean} - True if the file matches at least one pattern
 *
 * @example
 * matchesGlobPattern("file.json", "*.json *.jsonl"); // true
 * matchesGlobPattern("file.txt", "*.json *.jsonl"); // false
 */
function matchesGlobPattern(filePath, fileGlobFilter) {
  const patterns = parseGlobPatterns(fileGlobFilter);
  return patterns.some(pattern => pattern.test(filePath));
}

/**
 * Convert a simple glob pattern to a RegExp (for non-path matching)
 * @param {string} pattern - Glob pattern (e.g., "copilot", "*[bot]")
 * @param {boolean} caseSensitive - Whether matching should be case-sensitive (default: false)
 * @returns {RegExp} - Regular expression that matches the pattern
 *
 * Supports:
 * - * matches any characters (not limited to non-slash like path mode)
 * - Escapes special regex characters except *
 * - Case-insensitive by default
 *
 * @example
 * const regex = simpleGlobToRegex("*[bot]");
 * regex.test("dependabot[bot]"); // true
 * regex.test("github-actions[bot]"); // true
 *
 * @example
 * const regex = simpleGlobToRegex("copilot");
 * regex.test("copilot"); // true
 * regex.test("Copilot"); // true (case-insensitive)
 */
function simpleGlobToRegex(pattern, caseSensitive = false) {
  return globPatternToRegex(pattern, { pathMode: false, caseSensitive });
}

/**
 * Check if a string matches a simple glob pattern
 * @param {string} str - String to test (e.g., "copilot", "dependabot[bot]")
 * @param {string} pattern - Glob pattern (e.g., "copilot", "*[bot]")
 * @param {boolean} caseSensitive - Whether matching should be case-sensitive (default: false)
 * @returns {boolean} - True if the string matches the pattern
 *
 * @example
 * matchesSimpleGlob("dependabot[bot]", "*[bot]"); // true
 * matchesSimpleGlob("copilot", "copilot"); // true
 * matchesSimpleGlob("Copilot", "copilot"); // true (case-insensitive by default)
 * matchesSimpleGlob("alice", "*[bot]"); // false
 */
function matchesSimpleGlob(str, pattern, caseSensitive = false) {
  if (!str || !pattern) {
    return false;
  }

  // Exact match check (case-insensitive by default)
  if (!caseSensitive && str.toLowerCase() === pattern.toLowerCase()) {
    return true;
  } else if (caseSensitive && str === pattern) {
    return true;
  }

  const regex = simpleGlobToRegex(pattern, caseSensitive);
  return regex.test(str);
}

module.exports = {
  globPatternToRegex,
  parseGlobPatterns,
  matchesGlobPattern,
  simpleGlobToRegex,
  matchesSimpleGlob,
};
