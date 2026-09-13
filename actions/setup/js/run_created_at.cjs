// @ts-check

// ISO-8601 date-time with a UTC designator or a numeric offset. Date.parse accepts
// looser formats (for example "2026" or "2026-09-12 20:58:00"), which must not be
// promoted to a valid run creation time.
const ISO_8601_DATE_TIME = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/;

/**
 * Normalize a workflow-run creation time into the strict UTC ISO-8601 form
 * (`YYYY-MM-DDTHH:MM:SSZ`) expected by the operational-value grader contract.
 *
 * GitHub returns `created_at` as an ISO timestamp, but callers may also receive
 * an empty string (unresolved workflow output) or an offset-bearing timestamp.
 * Values that are not ISO-8601 date-times are reported as an empty string so
 * callers can treat the timestamp as unavailable rather than propagating an
 * invalid value. Sub-second precision is truncated (not rounded) because the
 * grader contract records run creation times at second granularity.
 *
 * @param {unknown} value
 * @returns {string} normalized timestamp, or "" when the value is unusable
 */
function normalizeRunCreatedAt(value) {
  if (typeof value !== "string") return "";
  const trimmed = value.trim();
  if (!ISO_8601_DATE_TIME.test(trimmed)) return "";
  const parsed = Date.parse(trimmed);
  if (!Number.isFinite(parsed)) return "";
  return new Date(Math.floor(parsed / 1000) * 1000).toISOString().replace(/\.\d{3}Z$/, "Z");
}

module.exports = { normalizeRunCreatedAt };
