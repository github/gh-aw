// @ts-check

/**
 * Read the compiler's allowlisted memory push outputs, not agent save requests.
 * Missing evidence must never be interpreted as a successful save.
 * @param {string | undefined} [value]
 * @returns {import("./types/unified_session").MemorySaveResultData[]}
 */
function readMemorySaveResults(value = process.env.GH_AW_MEMORY_SAVE_RESULTS) {
  if (!value) return [];
  let results;
  try {
    results = JSON.parse(value);
  } catch {
    return [];
  }
  if (!results || typeof results !== "object" || Array.isArray(results)) return [];
  return Object.entries(results)
    .sort(([left], [right]) => (left < right ? -1 : left > right ? 1 : 0))
    .flatMap(([memoryId, result]) => {
      if (!/^[A-Za-z0-9_-]+$/.test(memoryId) || !result || typeof result !== "object" || Array.isArray(result) || !["success", "failure", "cancelled", "skipped"].includes(result.jobResult)) return [];
      const outcome = ["success", "failure", "cancelled", "skipped"].includes(result.outcome) ? result.outcome : undefined;
      /** @type {import("./types/unified_session").MemorySaveResultData["status"]} */
      let status = "unknown";
      if (outcome === "failure" || outcome === "cancelled") status = outcome;
      else if (outcome === "skipped" || (outcome === undefined && ["skipped", "cancelled"].includes(result.jobResult))) status = outcome ?? result.jobResult;
      else if (outcome === "success" && ["saved", "unchanged", "skipped"].includes(result.status)) status = result.status;
      return [{ memoryId, jobResult: result.jobResult, ...(outcome ? { outcome } : {}), status }];
    });
}

module.exports = { readMemorySaveResults };
