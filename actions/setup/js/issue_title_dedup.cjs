// @ts-check

const { levenshteinDistance } = require("./levenshtein_distance.cjs");
const MAX_DEDUPLICATE_BY_TITLE_DISTANCE = 100;

/**
 * @param {string[] | undefined} labels
 * @returns {string[]}
 */
function normalizeDeduplicationLabels(labels) {
  return (labels ?? []).map(label => label.trim()).filter(Boolean);
}

/**
 * @param {string[]} labels
 * @param {string[]} requiredLabels
 * @returns {boolean}
 */
function hasDeduplicationLabels(labels, requiredLabels) {
  const names = new Set(labels.map(label => label.toLowerCase()));
  return requiredLabels.every(label => names.has(label.toLowerCase()));
}

/**
 * @param {string} normalizedTitle
 * @param {TitleCandidate[]} candidates
 * @param {{ enabled: boolean, maxDistance: number }} titleFilter
 * @returns {{ title: string, distance: number } | null}
 */
function findDuplicateIssue(normalizedTitle, candidates, titleFilter) {
  if (titleFilter.enabled) {
    return findDuplicateByTitle(normalizedTitle, candidates, titleFilter.maxDistance);
  }
  return candidates.length > 0 ? { title: candidates[0].title, distance: 0 } : null;
}

/**
 * Parse create-issue deduplication config.
 * - true  => enabled with exact-match distance 0
 * - false => disabled
 * - N     => enabled with Levenshtein max distance N
 *
 * @param {unknown} value
 * @returns {{ enabled: boolean, maxDistance: number }}
 */
function parseDeduplicateByTitle(value) {
  if (value === undefined || value === null || value === false) {
    return { enabled: false, maxDistance: 0 };
  }
  if (value === true) {
    return { enabled: true, maxDistance: 0 };
  }
  if (value === "false") {
    return { enabled: false, maxDistance: 0 };
  }
  if (value === "true") {
    return { enabled: true, maxDistance: 0 };
  }
  const numeric = typeof value === "string" && /^\d+$/.test(value) ? Number.parseInt(value, 10) : value;
  if (typeof numeric === "number" && Number.isFinite(numeric) && Number.isInteger(numeric) && numeric >= 0 && numeric <= MAX_DEDUPLICATE_BY_TITLE_DISTANCE) {
    return { enabled: true, maxDistance: numeric };
  }
  throw new Error(`deduplicate-by-title must be a boolean, a boolean-like string, or a non-negative integer (0-${MAX_DEDUPLICATE_BY_TITLE_DISTANCE})`);
}

/**
 * Normalize a title for deduplication comparisons.
 * @param {string} title
 * @returns {string}
 */
function normalizeTitleForDedup(title) {
  return String(title ?? "")
    .toLowerCase()
    .replace(/\s+/g, " ")
    .trim();
}

/**
 * @typedef {{ title: string, normalizedTitle?: string }} TitleCandidate
 */

/**
 * Find a duplicate candidate by Levenshtein distance threshold.
 *
 * @param {string} normalizedTitle
 * @param {TitleCandidate[]} candidates
 * @param {number} maxDistance
 * @returns {{ title: string, distance: number } | null}
 */
function findDuplicateByTitle(normalizedTitle, candidates, maxDistance) {
  /** @type {{ title: string, distance: number } | null} */
  let bestMatch = null;

  for (const candidate of candidates) {
    const candidateTitle = normalizeTitleForDedup(candidate.normalizedTitle || candidate.title);
    const distance = levenshteinDistance(normalizedTitle, candidateTitle);
    if (distance <= maxDistance && (!bestMatch || distance < bestMatch.distance)) {
      bestMatch = { title: candidate.title, distance };
      if (distance === 0) {
        return bestMatch;
      }
    }
  }

  return bestMatch;
}

module.exports = {
  normalizeDeduplicationLabels,
  hasDeduplicationLabels,
  findDuplicateIssue,
  parseDeduplicateByTitle,
  normalizeTitleForDedup,
  findDuplicateByTitle,
};
