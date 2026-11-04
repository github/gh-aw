// @ts-check
/// <reference types="@actions/github-script" />

const { formatDateInProjectTimeZone, resolveProjectTimeZone } = require("./project_timezone.cjs");

/**
 * Regex pattern to match expiration marker with checked checkbox and HTML comment (new format)
 * Format: > - [x] expires <!-- gh-aw-expires: ISO_DATE --> on HUMAN_DATE UTC
 * Allows flexible whitespace and supports blockquote prefix
 */
const EXPIRATION_PATTERN = /^>\s*-\s*\[x\]\s+expires\s*<!--\s*gh-aw-expires:\s*([^>]+)\s*-->/m;

/**
 * Regex pattern to match legacy expiration marker without HTML comment (old format)
 * Format: > - [x] expires  on HUMAN_DATE UTC
 * Allows flexible whitespace and supports blockquote prefix
 * Captures the human-readable date for parsing
 */
const LEGACY_EXPIRATION_PATTERN = /^>\s*-\s*\[x\]\s+expires\s+on\s+(.+?)\s+UTC\s*$/m;

/**
 * Format a Date object to human-readable string in UTC
 * @param {Date} date - Date to format
 * @returns {string} Human-readable date string (e.g., "Jan 25, 2026, 1:53 PM")
 */
function formatExpirationDate(date) {
  const projectDate = formatDateInProjectTimeZone(date);
  if (projectDate) {
    return projectDate;
  }

  return date.toLocaleString("en-US", {
    dateStyle: "medium",
    timeStyle: "short",
    timeZone: "UTC",
  });
}

/**
 * Create expiration marker line with checkbox, XML comment, and human-readable date
 * @param {Date} expirationDate - Date when the item expires
 * @returns {string} Formatted expiration line
 */
function createExpirationLine(expirationDate) {
  const expirationISO = expirationDate.toISOString();
  const humanReadableDate = formatExpirationDate(expirationDate);
  if (resolveProjectTimeZone()) {
    return `- [x] expires <!-- gh-aw-expires: ${expirationISO} --> on ${humanReadableDate}`;
  }
  return `- [x] expires <!-- gh-aw-expires: ${expirationISO} --> on ${humanReadableDate} UTC`;
}

/**
 * Extract expiration date from text body
 * Supports two formats:
 * 1. New format with HTML comment: > - [x] expires <!-- gh-aw-expires: ISO_DATE --> on HUMAN_DATE UTC
 * 2. Legacy format without HTML comment: > - [x] expires  on HUMAN_DATE UTC
 * @param {string} body - Text body containing expiration marker
 * @returns {Date|null} Expiration date or null if not found/invalid
 */
function extractExpirationDate(body) {
  // Try new format with HTML comment first (preferred)
  const match = body.match(EXPIRATION_PATTERN);

  if (match) {
    const expirationISO = match[1].trim();
    const expirationDate = new Date(expirationISO);

    // Validate the date
    if (!Number.isNaN(expirationDate.getTime())) {
      return expirationDate;
    }
  }

  // Fall back to legacy format without HTML comment
  const legacyMatch = body.match(LEGACY_EXPIRATION_PATTERN);

  if (legacyMatch) {
    const humanReadableDate = legacyMatch[1].trim();
    // Parse human-readable date format: "Jan 20, 2026, 9:20 AM"
    // Add "UTC" timezone explicitly if not present to ensure UTC parsing
    const dateString = humanReadableDate.includes("UTC") ? humanReadableDate : `${humanReadableDate} UTC`;
    const expirationDate = new Date(dateString);

    // Validate the date
    if (!Number.isNaN(expirationDate.getTime())) {
      return expirationDate;
    }
  }

  return null;
}

/**
 * Generate a quoted footer with optional expiration line
 * @param {Object} options - Footer generation options
 * @param {string} options.footerText - The main footer text (already formatted with ">")
 * @param {number} [options.expiresHours] - Hours until expiration (0 or undefined means no expiration)
 * @param {string} [options.entityType] - Type of entity for logging (e.g., "Issue", "Discussion", "Pull Request")
 * @param {string} [options.suffix] - Optional suffix to append after the footer (e.g., XML marker, type marker)
 * @returns {string} Complete footer with expiration in quoted section
 */
function generateFooterWithExpiration(options) {
  const { footerText, expiresHours, entityType, suffix } = options;
  let footer = footerText;

  // Add expiration line inside the quoted section if configured
  if (expiresHours && expiresHours > 0) {
    const expirationDate = new Date();
    expirationDate.setHours(expirationDate.getHours() + expiresHours);
    const expirationLine = createExpirationLine(expirationDate);
    footer = `${footer.trimEnd()}\n> ${expirationLine}`;

    if (entityType) {
      core.info(`${entityType} will expire on ${expirationDate.toISOString()} (${expiresHours} hours)`);
    }
  }

  // Add suffix if provided (e.g., XML marker, type marker)
  if (suffix) {
    footer = `${footer}${suffix}`;
  }

  return footer;
}

/**
 * Add expiration to an existing footer that may contain an XML marker
 * Inserts the expiration line before the XML marker to keep it in the quoted section
 * @param {string} footer - Existing footer text
 * @param {number} [expiresHours] - Hours until expiration (0 or undefined means no expiration)
 * @param {string} [entityType] - Type of entity for logging
 * @returns {string} Footer with expiration inserted before XML marker
 */
function addExpirationToFooter(footer, expiresHours, entityType) {
  if (!expiresHours || expiresHours <= 0) {
    return footer;
  }

  const expirationDate = new Date();
  expirationDate.setHours(expirationDate.getHours() + expiresHours);
  const expirationLine = createExpirationLine(expirationDate);

  // Look for XML marker at the end of footer
  const xmlMarkerMatch = footer.match(/(?:\r?\n){2}<!--.*?-->\s*$/s);
  if (xmlMarkerMatch) {
    // Insert expiration before XML marker
    const xmlMarker = xmlMarkerMatch[0];
    const footerWithoutXml = footer.substring(0, footer.length - xmlMarker.length);
    footer = `${footerWithoutXml.trimEnd()}\n> ${expirationLine}${xmlMarker}`;
  } else {
    // No XML marker, just append to footer
    footer = `${footer.trimEnd()}\n> ${expirationLine}`;
  }

  if (entityType) {
    core.info(`${entityType} will expire on ${expirationDate.toISOString()} (${expiresHours} hours)`);
  }

  return footer;
}

module.exports = {
  EXPIRATION_PATTERN,
  LEGACY_EXPIRATION_PATTERN,
  formatExpirationDate,
  createExpirationLine,
  extractExpirationDate,
  generateFooterWithExpiration,
  addExpirationToFooter,
};
