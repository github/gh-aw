// @ts-check
/// <reference types="@actions/github-script" />

const categories = new Set(["backlog", "planned", "started", "completed", "not_planned", "duplicate"]);

/**
 * Filters apply only to issues:status_changed. Each invocation evaluates its
 * captured transition independently; this is not a deduplication/freshness check.
 * @param {string} eventName
 * @param {any} payload
 * @param {any} filters
 * @returns {boolean}
 */
function matchesIssueStatus(eventName, payload, filters) {
  if (eventName !== "issues" || payload?.action !== "status_changed") return true;
  if (!filters || typeof filters !== "object" || Array.isArray(filters)) return false;
  const keys = Object.keys(filters);
  if (keys.length === 0 || keys.some(key => !["status-ids", "status-categories"].includes(key))) return false;
  const ids = filters["status-ids"];
  const allowedCategories = filters["status-categories"];
  if (Object.hasOwn(filters, "status-ids") && (!Array.isArray(ids) || ids.length === 0 || ids.some(id => !Number.isSafeInteger(id) || id <= 0))) return false;
  if (Object.hasOwn(filters, "status-categories") && (!Array.isArray(allowedCategories) || allowedCategories.length === 0 || allowedCategories.some(category => !categories.has(category)))) return false;
  const status = payload.status;
  if (!status || !Number.isSafeInteger(status.id) || status.id <= 0 || typeof status.name !== "string" || typeof status.category !== "string") return false;
  return (!ids || ids.includes(status.id)) && (!allowedCategories || allowedCategories.includes(status.category));
}

async function main() {
  let filters;
  try {
    filters = JSON.parse(process.env.GH_AW_ISSUE_STATUS_FILTER || "");
  } catch {
    core.setOutput("status_match", "false");
    return;
  }
  core.setOutput("status_match", String(matchesIssueStatus(context.eventName, context.payload, filters)));
}

module.exports = { main, matchesIssueStatus };
