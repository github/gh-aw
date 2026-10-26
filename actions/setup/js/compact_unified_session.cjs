// @ts-check
"use strict";

const fs = require("node:fs");
const { ERR_VALIDATION } = require("./error_codes.cjs");
const { normalizeHarnessRetryRecord } = require("./harness_retry_record.cjs");

const COMPACT_UNIFIED_SESSION_VERSION = 1;
const MAX_COMPACT_SESSION_BYTES = 16 * 1024;
const MAX_COMPACT_SESSION_EVENTS = 128;
const MAX_COMPACT_STRING_BYTES = 256;
const MAX_SOURCE_LINE_BYTES = 16 * 1024;
const MAX_SOURCE_SCAN_BYTES = 64 * 1024 * 1024;

/** @type {Record<string, readonly string[]>} */
const EVENT_FIELDS = {
  "session.format": ["version"],
  "workflow.info": ["engineId", "agentVersion", "cliVersion", "awfVersion", "mcpgVersion", "model", "requestedModel", "triggerType", "dryRun", "workflowId", "workflowCallId", "repository", "runId", "runAttempt", "ref", "sha"],
  "session.result": ["sourceEngine", "numTurns", "durationMs", "totalCostUsd", "status", "sourceType", "sessionId", "agentId"],
  "usage.report": ["stepIndex", "provider", "model", "purpose", "status", "aic", "totalAic", "premiumRequests", "durationMs", "sessionId", "agentId"],
  "guardrail.admission": ["status", "maxAiCredits", "threatDetectionMaxAiCredits", "evalsMaxAiCredits"],
  "guardrail.daily_aic": ["status", "exceeded", "total", "estimated", "threshold"],
  "workflow.job_result": ["jobId", "result", "durationMs"],
  "memory.push": ["memoryId", "jobResult", "outcome", "status", "branch", "commit"],
  "execution.result": ["outcome", "conclusion", "exitCode", "durationMs", "startedAt", "finishedAt"],
  "harness.retry": ["attempts", "exitCode"],
  "grader.result": ["id", "status", "value", "score", "unit", "passed", "direction", "threshold", "enabled", "min", "max", "version"],
};
EVENT_FIELDS["firewall.token_usage"] = EVENT_FIELDS["usage.report"];
const USAGE_FIELDS = ["totalTokens", "inputTokens", "outputTokens", "reasoningOutputTokens", "cacheReadInputTokens", "cacheCreationInputTokens", "inputTokensIncludeCache"];
const GRADER_RUN_FIELDS = ["id", "attempt", "graderCount", "passed", "failed", "errors"];

/**
 * @typedef {{version: number, session: string, includedEvents: number, omittedEvents: number, oversizedLines: number, scanTruncated: boolean, truncated: boolean}} CompactUnifiedSession
 */

/** @param {unknown} value @returns {value is Record<string, unknown>} */
function isRecord(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

/** @param {Record<string, unknown>} record @param {readonly string[]} fields */
function projectScalars(record, fields) {
  /** @type {Record<string, string | number | boolean>} */
  const projected = {};
  for (const field of fields) {
    if (!Object.hasOwn(record, field)) continue;
    const value = record[field];
    if (typeof value === "boolean" || (typeof value === "number" && Number.isFinite(value))) projected[field] = value;
    else if (typeof value === "string" && Buffer.byteLength(value) <= MAX_COMPACT_STRING_BYTES && Buffer.from(value).toString("utf8") === value) projected[field] = value;
  }
  return projected;
}

/** @param {unknown} event */
function projectEvent(event) {
  if (!isRecord(event) || typeof event.type !== "string" || !Object.hasOwn(EVENT_FIELDS, event.type) || !isRecord(event.data)) return undefined;
  /** @type {Record<string, unknown>} */
  const data = projectScalars(event.data, EVENT_FIELDS[event.type]);
  if (event.type === "harness.retry") {
    const retry = normalizeHarnessRetryRecord(event.data);
    if (!retry) return undefined;
    Object.assign(data, retry);
  }
  if (["session.result", "usage.report", "firewall.token_usage"].includes(event.type) && isRecord(event.data.usage)) data.usage = projectScalars(event.data.usage, USAGE_FIELDS);
  if (event.type === "grader.result" && isRecord(event.data.run)) data.run = projectScalars(event.data.run, GRADER_RUN_FIELDS);
  /** @type {Record<string, unknown>} */
  const projected = { type: event.type, data, ...projectScalars(event, ["timestamp"]) };
  if (isRecord(event.provenance)) projected.provenance = projectScalars(event.provenance, ["component", "phase", "path", "index"]);
  return projected;
}

/**
 * Deterministic, lossy metadata projection. It never mutates its source events.
 * @param {Iterable<unknown>} events
 * @returns {CompactUnifiedSession}
 */
function compactUnifiedSession(events) {
  let session = "";
  let bytes = 0;
  let includedEvents = 0;
  let omittedEvents = 0;
  let truncated = false;
  for (const event of events) {
    const projected = projectEvent(event);
    if (!projected) {
      omittedEvents++;
      continue;
    }
    const line = JSON.stringify(projected) + "\n";
    const length = Buffer.byteLength(line);
    if (includedEvents >= MAX_COMPACT_SESSION_EVENTS || bytes + length > MAX_COMPACT_SESSION_BYTES) {
      omittedEvents++;
      truncated = true;
      continue;
    }
    session += line;
    bytes += length;
    includedEvents++;
  }
  return { version: COMPACT_UNIFIED_SESSION_VERSION, session, includedEvents, omittedEvents, oversizedLines: 0, scanTruncated: false, truncated };
}

/**
 * Skip oversized records without retaining or decoding their unbounded content.
 * @param {number} fd
 * @param {number} size
 * @param {{oversizedLines: number}} stats
 */
function* readEvents(fd, size, stats) {
  const chunk = Buffer.alloc(64 * 1024);
  const line = Buffer.alloc(MAX_SOURCE_LINE_BYTES);
  let length = 0;
  let oversized = false;
  let read = 0;
  /** @returns {unknown} */
  function decode() {
    if (oversized) {
      stats.oversizedLines++;
      return undefined;
    }
    try {
      const text = new TextDecoder("utf-8", { fatal: true, ignoreBOM: true }).decode(line.subarray(0, length));
      return text.trim() ? JSON.parse(text) : undefined;
    } catch {
      // Native parser diagnostics can include private source content.
      throw new Error(`${ERR_VALIDATION}: Invalid UTF-8 or JSON in compact unified session source.`);
    }
  }
  while (read < Math.min(size, MAX_SOURCE_SCAN_BYTES)) {
    const count = fs.readSync(fd, chunk, 0, Math.min(chunk.length, size - read, MAX_SOURCE_SCAN_BYTES - read), null);
    if (!count) throw new Error(`${ERR_VALIDATION}: Compact unified session source ended before its snapshot size.`);
    read += count;
    let start = 0;
    while (start < count) {
      const newline = chunk.indexOf(10, start);
      const end = newline < 0 || newline >= count ? count : newline;
      const part = end - start;
      if (!oversized && length + part <= line.length) {
        chunk.copy(line, length, start, end);
        length += part;
      } else oversized = true;
      if (end < count) {
        if (length || oversized) yield decode();
        length = 0;
        oversized = false;
      }
      start = end + 1;
    }
  }
  // A scan-boundary fragment is not a complete source record.
  if (read === size && (length || oversized)) yield decode();
}

/** @param {string} filePath @returns {CompactUnifiedSession} */
function readCompactUnifiedSession(filePath) {
  const fd = fs.openSync(filePath, fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW | fs.constants.O_NONBLOCK);
  try {
    const stat = fs.fstatSync(fd);
    if (!stat.isFile() || !Number.isSafeInteger(stat.size) || stat.size === 0) throw new Error(`${ERR_VALIDATION}: Compact unified session requires a nonempty regular source file.`);
    const stats = { oversizedLines: 0 };
    const compact = compactUnifiedSession(readEvents(fd, stat.size, stats));
    const scanTruncated = stat.size > MAX_SOURCE_SCAN_BYTES;
    return { ...compact, ...stats, scanTruncated, truncated: compact.truncated || scanTruncated || stats.oversizedLines > 0 };
  } finally {
    fs.closeSync(fd);
  }
}

module.exports = { compactUnifiedSession, readCompactUnifiedSession, COMPACT_UNIFIED_SESSION_VERSION, MAX_COMPACT_SESSION_BYTES, MAX_COMPACT_SESSION_EVENTS, MAX_COMPACT_STRING_BYTES, MAX_SOURCE_LINE_BYTES, MAX_SOURCE_SCAN_BYTES };
