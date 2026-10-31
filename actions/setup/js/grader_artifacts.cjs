// @ts-check

const fs = require("fs");
const path = require("path");
const { computeGraders, buildGradersSummaryBody } = require("./trace_graders.cjs");
const { normalizeUnifiedSessionEvent } = require("./unified_session_payload.cjs");

/** @param {string} [prefix] @returns {any} */
function readGraderManifestEnvironment(prefix = "GH_AW_GRADER_MANIFEST") {
  const countText = process.env[`${prefix}_CHUNKS`];
  if (countText === undefined) return { graders: [] };
  const count = Number(countText);
  if (!Number.isSafeInteger(count) || count < 1 || count > 10000) throw new Error(`invalid ${prefix}_CHUNKS`);
  const chunks = [];
  for (let index = 0; index < count; index++) {
    const chunk = process.env[`${prefix}_${index}`];
    if (chunk === undefined) throw new Error(`missing ${prefix}_${index}`);
    chunks.push(chunk);
  }
  try {
    const manifest = JSON.parse(chunks.join(""));
    if (!manifest || !Array.isArray(manifest.graders)) throw new Error("expected a graders array");
    if (manifest.customGraders !== undefined && (!Array.isArray(manifest.customGraders) || !manifest.customGraders.every(id => typeof id === "string" && /^[a-z][a-z0-9-]{0,63}$/.test(id)))) {
      throw new Error("expected custom grader IDs");
    }
    return manifest;
  } catch (error) {
    throw new Error("cannot parse grader manifest configuration", { cause: error });
  }
}

/**
 * Persistence boundary, not a grader: computation receives only the supplied log.
 * @param {import("./types/unified_session").UnifiedSession} session
 * @param {string} usageDirectory
 * @param {any[]} graders
 * @param {string[]} [customGraderIds]
 * @returns {import("./types/unified_session").UnifiedSession}
 */
function writeGraderArtifacts(session, usageDirectory, graders, customGraderIds = []) {
  // Grade and return the same filtered ordering, so reference indices address
  // the published array even when historical grader events were interleaved.
  const evidenceSession = session.filter(event => event.type !== "failure.observation" && !event.type.startsWith("grader."));
  const computed = computeGraders(evidenceSession, graders);
  const manifest = { ...computed.manifest, ...(customGraderIds.length ? { customGraders: customGraderIds } : {}) };
  const output = computed.output;
  const directory = path.join(usageDirectory, "graders");
  try {
    fs.mkdirSync(directory, { recursive: true });
    fs.writeFileSync(path.join(directory, "grader_manifest.json"), JSON.stringify(manifest, null, 2), { mode: 0o600 });
    fs.writeFileSync(path.join(directory, "grader_results.json"), JSON.stringify(output, null, 2), { mode: 0o600 });
  } catch (error) {
    throw new Error(`cannot persist grader results in ${directory}`, { cause: error });
  }
  /** @type {Array<{type: "grader.manifest" | "grader.result", data: Record<string, any>, file: string}>} */
  const records = [
    { type: "grader.manifest", data: manifest, file: "grader_manifest.json" },
    { type: "grader.result", data: output, file: "grader_results.json" },
  ];
  const events = records.map(({ type, data, file }) => ({
    ...normalizeUnifiedSessionEvent({ type, data }, "conclusion"),
    provenance: { component: "grader", phase: "conclusion", path: `usage/graders/${file}`, index: 0 },
  }));
  for (const result of output.results) {
    const observations = result.diagnostics?.failureObservations;
    if (!Array.isArray(observations)) continue;
    for (const observation of observations) {
      events.push({
        ...normalizeUnifiedSessionEvent({ type: "failure.observation", data: observation }, "conclusion"),
        provenance: { component: "grader", phase: "conclusion", path: "usage/graders/grader_results.json", index: events.length - 2 },
      });
    }
  }
  const previous = session.filter(event => event.type.startsWith("grader.") || event.type === "failure.observation");
  const previousUntimed = previous.filter(previousEvent => previousEvent.timestamp === undefined).length;
  const newUntimed = events.filter(graderEvent => graderEvent.timestamp === undefined).length;
  const collected = evidenceSession.map(sessionEvent => {
    if (sessionEvent.type !== "session.collection") return sessionEvent;
    return {
      ...sessionEvent,
      data: {
        ...sessionEvent.data,
        sources: [
          ...(Array.isArray(sessionEvent.data.sources) ? sessionEvent.data.sources : []).filter(source => source.component !== "grader"),
          ...events.map(({ provenance: { component, phase, path } }) => ({ component, phase, path, events: 1 })),
        ],
        absentComponents: (Array.isArray(sessionEvent.data.absentComponents) ? sessionEvent.data.absentComponents : []).filter(component => component !== "grader"),
        untimedEvents: (typeof sessionEvent.data.untimedEvents === "number" ? sessionEvent.data.untimedEvents : 0) - previousUntimed + newUntimed,
      },
    };
  });
  return [...collected, ...events];
}

/** @param {import("./types/unified_session").UnifiedSession} session */
async function publishGraderSummary(session) {
  const results = session.find(event => event.type === "grader.result")?.data.results;
  if (!Array.isArray(results)) return;
  for (const result of results) {
    if (result.error) global.core.warning(`Grader ${result.id}: ${result.error}`);
  }
  global.core.summary.addDetails("Graders", buildGradersSummaryBody(results));
  await global.core.summary.write({ overwrite: false });
}

module.exports = { readGraderManifestEnvironment, writeGraderArtifacts, publishGraderSummary };
