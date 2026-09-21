// @ts-check

const { globPatternToRegex } = require("./glob_pattern_helpers.cjs");

/** @param {string[] | string | undefined} value @returns {string[]} */
function parseAllowedBranchPatterns(value) {
  if (Array.isArray(value)) return value.map(item => String(item).trim()).filter(Boolean);
  if (typeof value === "string")
    return value
      .split(",")
      .map(item => item.trim())
      .filter(Boolean);
  return [];
}

/** @param {string} branch @param {string[]} allowedPatterns @returns {boolean} */
function isAllowedBranch(branch, allowedPatterns) {
  return allowedPatterns.some(pattern => branch === pattern || pattern === "*" || (pattern.includes("*") && globPatternToRegex(pattern, { pathMode: true, caseSensitive: true }).test(branch)));
}

module.exports = { parseAllowedBranchPatterns, isAllowedBranch };
