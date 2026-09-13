// @ts-check

/**
 * Normalize a workflow-run creation time into the strict UTC ISO-8601 form
 * (`YYYY-MM-DDTHH:MM:SSZ`) expected by the operational-value grader contract.
 *
 * GitHub returns `created_at` as an ISO timestamp, but callers may also receive
 * an empty string (unresolved workflow output) or an offset-bearing timestamp.
 * Values that cannot be parsed are reported as an empty string so callers can
 * treat the timestamp as unavailable rather than propagating an invalid value.
 *
 * @param {unknown} value
 * @returns {string} normalized timestamp, or "" when the value is unusable
 */
function normalizeRunCreatedAt(value) {
  if (typeof value !== "string") return "";
  const trimmed = value.trim();
  if (trimmed === "") return "";
  const parsed = Date.parse(trimmed);
  if (!Number.isFinite(parsed)) return "";
  return new Date(parsed).toISOString().replace(/\.\d{3}Z$/, "Z");
}

module.exports = { normalizeRunCreatedAt };
