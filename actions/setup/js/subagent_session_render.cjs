// @ts-check

const { isMetric, isTokenCount, sessionOutputText } = require("./agent_session.cjs");
const { collectArtifactSecretValues, redactManifestValue } = require("./safe_output_manifest.cjs");
const { ERR_VALIDATION } = require("./error_codes.cjs");

/** @param {any} value @returns {string} */
const text = value => sessionOutputText(value).replace(/[\u0000-\u001f\u007f]/g, " ");

/** @param {any} value @returns {boolean} */
const object = value => value !== null && typeof value === "object" && !Array.isArray(value);

/**
 * Accounting snapshots replace earlier observations; lifecycle metadata is
 * correlated by instance ID, never by potentially repeated display names.
 * @param {Array<any>} events
 * @returns {string[]}
 */
function renderSubagentSummary(events) {
  const redacted = redactManifestValue(events, collectArtifactSecretValues());
  if (!Array.isArray(redacted)) throw new Error(`${ERR_VALIDATION}: Expected subagent session events`);
  const agents = new Map();
  let metrics;
  let sessionId;
  for (const event of redacted) {
    const data = event.data ?? {};
    if (["session.start", "session.init"].includes(event.type) && !event.agentId) {
      if ((data.sessionId !== undefined && data.sessionId !== sessionId) || (event.type === "session.init" && data.sessionId === undefined && data.sourceEngine === "copilot")) {
        agents.clear();
        metrics = undefined;
        sessionId = data.sessionId;
      }
    }
    if (["session.result", "session.shutdown"].includes(event.type) && object(data.agentMetrics)) metrics = data.agentMetrics;
    if (!event.type?.startsWith("subagent.")) continue;
    const id = event.agentId ?? data.toolCallId;
    if (id === undefined) continue;
    const previous = agents.get(id) ?? {};
    const fields = ["agentName", "agentDisplayName", "parentId", "model", "reasoningEffort", "executionMode"];
    for (const field of fields) if (data[field] !== undefined) previous[field] = data[field];
    agents.set(id, previous);
  }
  if (object(metrics)) {
    for (const [id, metric] of Object.entries(metrics)) {
      if (id === "main" || !object(metric)) continue;
      agents.set(id, { agentName: metric.agentName, agentDisplayName: metric.agentDisplayName, ...agents.get(id) });
    }
  }
  if (!agents.size) return [];
  const lines = ["Subagents (per-agent snapshots, not additional session usage):"];
  const totalCredits = object(metrics) && Object.values(metrics).every(metric => isMetric(metric?.totalNanoAiu)) ? Object.values(metrics).reduce((sum, metric) => sum + metric.totalNanoAiu, 0) / 1e9 : undefined;
  if (object(metrics?.main)) lines.push(...agentUsageLines("main", metrics.main, totalCredits));
  for (const [id, agent] of agents) {
    const metadata = ["agentName", "parentId", "model", "reasoningEffort", "executionMode"].filter(field => agent[field] !== undefined).map(field => `${field}=${text(agent[field])}`);
    lines.push(`  ${text(agent.agentDisplayName ?? agent.agentName ?? id)} (agentId=${text(id)}) ${metadata.join(" ")}`.trimEnd());
    const metric = metrics?.[id];
    lines.push(...(object(metric) ? agentUsageLines(id, metric, totalCredits) : ["    Usage: unavailable"]));
  }
  return lines;
}

/** @param {string} id @param {any} metric @param {number | undefined} totalCredits @returns {string[]} */
function agentUsageLines(id, metric, totalCredits) {
  const lines = [];
  if (isMetric(metric.totalNanoAiu)) {
    const credits = metric.totalNanoAiu / 1e9;
    const share = totalCredits !== undefined && isMetric(totalCredits) && totalCredits > 0 ? ` (${((credits / totalCredits) * 100).toFixed(1)}% of agent credits)` : "";
    lines.push(`    ${text(id)} credits: ${credits.toFixed(3)}${share}`);
  }
  if (object(metric.modelMetrics)) {
    for (const [model, usage] of Object.entries(metric.modelMetrics)) {
      if (!object(usage)) continue;
      const fields = [];
      if (isTokenCount(usage.requests?.count)) fields.push(`requests=${usage.requests.count}`);
      for (const key of ["inputTokens", "outputTokens", "cacheReadTokens", "cacheWriteTokens"]) {
        if (isTokenCount(usage.usage?.[key])) fields.push(`${key}=${usage.usage[key]}`);
      }
      if (fields.length) lines.push(`    ${text(model)}: ${fields.join(" ")}`);
    }
  }
  return lines.length ? lines : ["    Usage: unavailable"];
}

module.exports = { renderSubagentSummary };
