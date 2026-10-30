// @ts-check

const { sessionTimestamp } = require("./unified_session_order.cjs");

/**
 * Split only observed process invocations, never infer attempts from session IDs.
 * The manifest and harness boundary or legacy spawn diagnostics must agree.
 * @param {string} content
 * @param {string} engine
 * @param {import("./types/unified_session").HarnessRetryAttemptData[]} attempts
 * @param {(content: string, engine: string) => import("./types/agent_session").SessionEvent[]} parse
 */
function parseHarnessAttempts(content, engine, attempts, parse) {
  if (!["claude", "codex"].includes(engine) || attempts.length < 2) return undefined;
  const boundaries = [...content.matchAll(/^\[gh-aw-harness-attempt\] ([^\n]*)$/gm)];
  /** @type {{index: number, attempt: number, attemptId?: string, startupAttemptId?: string}[]} */
  let markers = [];
  if (boundaries.length) {
    try {
      for (const boundary of boundaries) {
        const record = JSON.parse(boundary[1]);
        if (record.event === "start") markers.push({ index: boundary.index, attempt: record.attempt, attemptId: record.attempt_id, startupAttemptId: record.startup_attempt_id });
      }
    } catch {
      return undefined;
    }
    if (markers.some((marker, index) => marker.attemptId !== attempts[index]?.attemptId || marker.startupAttemptId !== attempts[index]?.startupAttemptId)) return undefined;
  } else {
    const pattern = engine === "claude" ? /^\[claude-harness\] (?:\S+ )?attempt (\d+): spawning:/gm : /^\[codex-harness\] (?:\S+ )?attempt (\d+): spawning:/gm;
    markers = [...content.matchAll(pattern)].map(marker => ({ index: marker.index, attempt: Number(marker[1]) }));
  }
  if (markers.length !== attempts.length || markers.some((marker, index) => marker.attempt !== attempts[index].attempt)) return undefined;
  const segments = markers.map((marker, index) => ({
    attempt: attempts[index],
    events: parse(content.slice(marker.index, markers[index + 1]?.index ?? content.length), engine).filter(event => event.type !== "agent.execution"),
  }));
  if (!segments.some(segment => segment.events.length)) return undefined;
  // Attribute the whole-stream parser's observations, not independently reset
  // accounting snapshots. Codex accumulates per-turn usage across invocations.
  const combined = parse(content, engine).filter(event => event.type !== "agent.execution");
  const flattened = segments.flatMap(segment => segment.events);
  /** @param {import("./types/agent_session").SessionEvent} event */
  const signature = event => {
    const data = { ...event.data };
    if (engine === "codex" && event.type === "session.result") {
      delete data.usage;
      delete data.numTurns;
    }
    return JSON.stringify({ ...event, data });
  };
  if (combined.length !== flattened.length || combined.some((event, index) => signature(event) !== signature(flattened[index]))) return undefined;
  let offset = 0;
  for (const segment of segments) {
    segment.events = combined.slice(offset, offset + segment.events.length);
    offset += segment.events.length;
  }
  return segments;
}

/**
 * Untimed observations remain unassigned. Overlapping startup/harness intervals
 * are deliberately not mixed; only a unique harness interval proves correlation.
 * @param {import("./types/unified_session").UnifiedSession} events
 * @returns {import("./types/unified_session").UnifiedSession}
 */
function correlateHarnessAttempts(events) {
  const attempts = events.flatMap(event => {
    const { attemptId, startupAttemptId, startedAt, finishedAt } = event.data;
    return event.type === "harness.attempt" && event.data.layer === "harness" && typeof attemptId === "string" && typeof startedAt === "number" && typeof finishedAt === "number"
      ? [{ phase: event.provenance.phase, attemptId, startupAttemptId, startedAt, finishedAt }]
      : [];
  });
  for (const event of events) {
    if (!["agent", "firewall", "mcp", "usage"].includes(event.provenance.component) || event.provenance.attemptId) continue;
    const timestamp = event.provenance.timestampMs ?? sessionTimestamp(event);
    if (timestamp === undefined) continue;
    const matches = attempts.filter(attempt => attempt.phase === event.provenance.phase && timestamp >= attempt.startedAt && timestamp < attempt.finishedAt);
    if (matches.length === 1) {
      event.provenance.attemptId = matches[0].attemptId;
      if (typeof matches[0].startupAttemptId === "string") event.provenance.startupAttemptId = matches[0].startupAttemptId;
    }
  }
  return events;
}

module.exports = { parseHarnessAttempts, correlateHarnessAttempts };
