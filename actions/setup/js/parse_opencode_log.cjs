// @ts-check

const { createEngineLogParser, parseLogEntries, generateCopilotCliStyleSummary, buildStepSummaryDetailsSection } = require("./log_parser_shared.cjs");
const { createSessionEvent, isSessionEvent, accumulateSessionUsage, reconcileSessionUsage, normalizeSessionUsage, isMetric, isTokenCount, sessionToolSuccess } = require("./agent_session.cjs");
const { getMessageRefusal } = require("./provider_refusal.cjs");

const main = createEngineLogParser({ parserName: "OpenCode", parseFunction: parseOpenCodeLog, supportsDirectories: false });
/** @param {any} entry @returns {boolean} */
function isOpenCodeEvent(entry) {
  if (!entry || typeof entry !== "object" || Array.isArray(entry) || typeof entry.sessionID !== "string") return false;
  if (entry.type === "error") return !!entry.error && typeof entry.error === "object" && !Array.isArray(entry.error);
  const part = entry.part;
  if (!part || typeof part !== "object" || Array.isArray(part)) return false;
  if (entry.type === "text" || entry.type === "reasoning") return typeof part.text === "string";
  if (entry.type === "tool_use") return typeof part.tool === "string" && !!part.state && typeof part.state === "object" && !Array.isArray(part.state);
  return entry.type === "step_start" || entry.type === "step_finish" || entry.type === "opencode.compaction";
}

/** @param {any} part @returns {boolean} */
function isV2Part(part) {
  return typeof part?.partID === "string";
}

/** @param {any} tokens @returns {any} */
function projectOpenCodeTokens(tokens) {
  if (!tokens || typeof tokens !== "object" || Array.isArray(tokens)) return undefined;
  const projected = {};
  for (const key of ["input", "output", "reasoning"]) {
    if (isTokenCount(tokens[key])) projected[key] = tokens[key];
  }
  // Native v2 output excludes reasoning; canonical output includes that subset.
  if (isTokenCount(tokens.output) && isTokenCount(tokens.reasoning)) {
    const output = tokens.output + tokens.reasoning;
    if (isTokenCount(output)) projected.output = output;
    else delete projected.output;
  }
  const cache = {};
  for (const key of ["read", "write"]) {
    if (isTokenCount(tokens.cache?.[key])) cache[key] = tokens.cache[key];
  }
  if (Object.keys(cache).length) projected.cache = cache;
  return Object.keys(projected).length ? projected : undefined;
}

/**
 * Recover settled accounting omitted by the v2 CLI's projected-part reconciliation.
 * Never return user messages, prompts, or provider state from the native export.
 * @param {any} session
 * @param {string} sessionID
 * @returns {any[]}
 */
function getOpenCodeSessionSteps(session, sessionID) {
  if (session?.info?.id !== sessionID || !Array.isArray(session.messages)) throw new Error("OpenCode session export must contain the requested session and messages");
  return session.messages.flatMap(message => {
    const compaction = message?.type === "compaction" && (message.status === "completed" || message.status === "failed");
    if (!compaction && (message?.type !== "assistant" || message.time?.completed === undefined)) return [];
    const time = compaction ? message.time?.created : message.time.completed;
    const timestamp = typeof time === "number" ? time : Date.parse(time);
    if (typeof message.id !== "string" || !Number.isSafeInteger(timestamp) || timestamp < 0) throw new Error("OpenCode session export contains an invalid settled message");
    const tokens = projectOpenCodeTokens(message.tokens);
    return [
      {
        type: compaction ? "opencode.compaction" : "step_finish",
        timestamp,
        sessionID,
        part: {
          id: `gh_aw_export_${message.id}`,
          sessionID,
          messageID: message.id,
          ...(compaction ? {} : { type: "step-finish", ...(typeof message.finish === "string" ? { reason: message.finish } : {}) }),
          ...(tokens !== undefined ? { tokens } : {}),
          ...(isMetric(message.cost) ? { cost: message.cost } : {}),
        },
      },
    ];
  });
}

/** @param {string} content @returns {string[]} */
function getOpenCodeMCPFailures(content) {
  const failures = new Set();
  for (const line of content.split(/\r?\n/)) {
    if (!/^timestamp=\S+ level=(?:WARN|ERROR)\b/.test(line)) continue;
    const fields = parseOpenCodeLogfmt(line);
    if (!fields) continue;
    const v2 = fields.message === "mcp connect failed";
    let status = v2 ? (fields["status.status"] ?? fields.status) : fields.status;
    if (v2 && status !== "failed") {
      try {
        status = JSON.parse(status)?.status;
      } catch {
        continue;
      }
    }
    if (status !== "failed" || (!v2 && (fields.message !== "server unavailable" || !["remote", "local"].includes(fields.type)))) continue;
    const server = v2 ? fields.server : fields.key;
    if (server) failures.add(server);
  }
  return [...failures];
}

/** @param {any} raw @returns {any} */
function sourceMetadata(raw) {
  const source = { ...raw };
  const partID = isV2Part(raw.part) ? raw.part.partID : raw.part?.id;
  if (!Object.hasOwn(raw, "id") && typeof partID === "string") source.id = partID;
  if (!Object.hasOwn(raw, "parentId") && typeof raw.part?.messageID === "string") source.parentId = raw.part.messageID;
  return source;
}

/**
 * @param {string} line
 * @returns {Record<string, string> | undefined}
 */
function parseOpenCodeLogfmt(line) {
  /** @type {Record<string, string>} */
  const fields = Object.create(null);
  const pattern = /([^\s=]+)=("(?:\\.|[^"\\])*"|[^\s"]+)(?:\s+|$)/gy;
  while (pattern.lastIndex < line.length) {
    const match = pattern.exec(line);
    if (!match) return undefined;
    if (match[2].startsWith('"')) {
      try {
        fields[match[1]] = JSON.parse(match[2]);
      } catch {
        return undefined;
      }
    } else fields[match[1]] = match[2];
  }
  return fields;
}

/**
 * The CLI can report a retried stream error only on stderr, without a JSON error.
 * Retain the diagnostic, not an inferred terminal outcome or model usage.
 * @param {string} line
 * @returns {import("./types/agent_session").SessionEvent | undefined}
 */
function parseOpenCodeStreamError(line) {
  if (!/^timestamp=\S+ level=ERROR\b/.test(line)) return undefined;
  const fields = parseOpenCodeLogfmt(line);
  if (!fields) return undefined;
  if (fields.message !== "stream error" || !fields["session.id"] || !Object.hasOwn(fields, "error.error")) return undefined;
  return createSessionEvent({ ...fields, nativeLogfmt: line }, "session.error", { ...fields, sessionId: fields["session.id"], error: fields["error.error"] });
}

/** @param {string} content @returns {any[]} */
function openCodeRecords(content) {
  if (typeof content !== "string") return [];
  try {
    const parsed = JSON.parse(content);
    if (Array.isArray(parsed)) return parsed;
  } catch {
    // Intentionally fall through: mixed stdout/stderr is not one JSON document.
  }
  const lines = content.split(/\r?\n/);
  const fencedLines = new Set();
  for (let index = 0; index < lines.length; index++) {
    const opening = lines[index].match(/^\s*(`{3,}|~{3,})/);
    if (!opening) continue;
    const marker = opening[1];
    const end = lines.findIndex((line, candidate) => {
      const closing = line.match(/^\s*(`{3,}|~{3,})\s*$/);
      return candidate > index && closing?.[1][0] === marker[0] && closing[1].length >= marker.length;
    });
    if (end === -1) continue;
    for (let fencedIndex = index; fencedIndex <= end; fencedIndex++) fencedLines.add(fencedIndex);
    index = end;
  }
  return lines.flatMap((line, index) => {
    const error = fencedLines.has(index) ? undefined : parseOpenCodeStreamError(line);
    return error ? [error] : (parseLogEntries(line) ?? []);
  });
}

/**
 * Keep one core observation for a native part while retaining every revision.
 * @param {any} previous
 * @param {any} raw
 * @param {import("./types/agent_session").SessionEvent} event
 */
function updatePartSnapshot(previous, raw, event) {
  const nativeSnapshots = [...(previous.snapshots ?? [structuredClone(previous.raw)]), structuredClone(raw)];
  previous.snapshots = nativeSnapshots;
  Object.assign(previous.event, event, ...(previous.event.id !== undefined ? [{ id: previous.event.id }] : []), { nativeSnapshots });
}

/**
 * Prefer OpenCode's message identity when a part is replayed with a fresh part ID.
 * @param {string} sessionID
 * @param {string} type
 * @param {unknown} messageID
 * @param {unknown} [partID]
 * @returns {string | undefined}
 */
function openCodeMessageIdentity(sessionID, type, messageID, partID) {
  if (typeof messageID === "string") return JSON.stringify([sessionID, type, "message", messageID]);
  if (typeof partID === "string") return JSON.stringify([sessionID, type, "part", partID]);
  return undefined;
}

/**
 * OpenCode `run --format json` emits completed parts, not Claude-style messages.
 * A completed tool part supplies both the invocation and its result.
 * @param {string} content
 * @returns {{markdown: string, logEntries: import("./types/agent_session").SessionEvent[], mcpFailures: string[], maxTurnsHit: boolean}}
 */
function parseOpenCodeLog(content) {
  const records = openCodeRecords(content).filter(entry => isSessionEvent(entry) || isOpenCodeEvent(entry));
  /** @type {import("./types/agent_session").SessionEvent[]} */
  const logEntries = [];
  const sessions = new Set();
  const parts = new Set();
  const toolStarts = new Map();
  const toolCompletions = new Map();
  const stepReports = new Map();
  const compactionReports = new Map();
  const finishRefusals = new Map();
  const messageParts = new Map();
  /** @type {Record<string, any>} */
  const usage = {};
  const errors = [];
  let totalCostUsd;
  let costOverflowed = false;
  let resultSource;

  for (const raw of records) {
    if (isSessionEvent(raw)) {
      const refusal = raw.type === "assistant.message" ? getMessageRefusal(raw.data) : undefined;
      logEntries.push(refusal ? createSessionEvent(raw, "assistant.refusal", refusal) : structuredClone(raw));
      if (raw.type === "opencode.max_turns" || raw.type === "opencode.mcp_failure") {
        errors.push({ type: raw.type, ...structuredClone(raw.data) });
        resultSource = raw;
      }
      if (raw.type === "session.result") resultSource = undefined;
      continue;
    }
    const emit = (type, data) => logEntries.push(createSessionEvent(sourceMetadata(raw), type, data));
    if (!sessions.has(raw.sessionID)) {
      sessions.add(raw.sessionID);
      emit("session.init", { sourceEngine: "opencode", sessionId: raw.sessionID });
    }
    const part = raw.part;
    const partID = isV2Part(part) ? part.partID : part?.id;
    // Equal snapshots are repeated evidence; changed parts retain their payloads.
    if (typeof partID === "string") {
      const identity = JSON.stringify([raw.sessionID, raw.type, partID, raw]);
      if (parts.has(identity)) continue;
      parts.add(identity);
    }
    if (raw.type === "text" || raw.type === "reasoning") {
      if (typeof part.text === "string") {
        const identity = openCodeMessageIdentity(raw.sessionID, raw.type, part.messageID, partID);
        const previous = identity !== undefined ? messageParts.get(identity) : undefined;
        const finishRefusalIdentity = raw.type === "text" && typeof part.messageID === "string" ? openCodeMessageIdentity(raw.sessionID, "text", part.messageID) : undefined;
        const finishRefusal = finishRefusalIdentity !== undefined ? finishRefusals.get(finishRefusalIdentity) : undefined;
        const existing = previous ?? finishRefusal;
        const refusal = raw.type === "text" ? (getMessageRefusal({ ...part, content: part.text }) ?? (existing?.event.type === "assistant.refusal" ? { reason: existing.event.data.reason } : undefined)) : undefined;
        const data = {
          content: part.text,
          ...(Object.hasOwn(part, "partial") ? { partial: part.partial } : Object.hasOwn(raw, "partial") ? { partial: raw.partial } : {}),
          ...(refusal ?? {}),
        };
        const type = refusal ? "assistant.refusal" : raw.type === "text" ? "assistant.message" : "assistant.reasoning";
        if (existing) {
          updatePartSnapshot(existing, raw, createSessionEvent(sourceMetadata(raw), type, { ...existing.event.data, ...raw.data, ...data }));
        } else {
          emit(type, data);
        }
        if (identity !== undefined) messageParts.set(identity, { event: existing?.event ?? logEntries.at(-1), raw });
      }
    } else if (raw.type === "tool_use") {
      const state = part.state;
      if (!state || typeof state !== "object") continue;
      const callID = part.callID ?? (isV2Part(part) ? part.id : undefined);
      const data = { toolCallId: callID, toolName: part.tool, input: state.input };
      const callIdentity = typeof callID === "string" ? JSON.stringify([raw.sessionID, callID]) : typeof partID === "string" ? JSON.stringify([raw.sessionID, "part", partID]) : undefined;
      if (callIdentity === undefined || !toolStarts.has(callIdentity)) {
        emit("tool.execution_start", data);
        if (callIdentity !== undefined) toolStarts.set(callIdentity, logEntries.at(-1));
      } else if (toolStarts.get(callIdentity).data.input === undefined && Object.hasOwn(state, "input")) {
        toolStarts.get(callIdentity).data.input = structuredClone(state.input);
      }
      if (state.status === "completed" || state.status === "error") {
        const identity = callIdentity;
        const previous = identity !== undefined ? toolCompletions.get(identity) : undefined;
        const start = state.time?.start;
        const end = state.time?.end;
        const exitCode = isV2Part(part) ? state.metadata?.metadata?.exit : state.metadata?.exit;
        const outputTruncated = isV2Part(part) ? state.metadata?.metadata?.truncated : state.metadata?.truncated;
        const completion = {
          toolCallId: callID,
          toolName: part.tool,
          status: state.status,
          ...(Object.hasOwn(state, "output") ? { output: state.output } : {}),
          ...(typeof outputTruncated === "boolean" ? { outputTruncated } : {}),
          ...(Object.hasOwn(state, "result") ? { result: state.result } : {}),
          ...(state.metadata !== undefined ? { metadata: state.metadata } : {}),
          ...(Number.isSafeInteger(exitCode) ? { exitCode } : {}),
          ...(state.error !== undefined ? { error: state.error } : {}),
          ...(isMetric(start) && isMetric(end) && end >= start ? { durationMs: end - start } : {}),
        };
        const success = previous?.event.data.success === false ? false : (sessionToolSuccess({ ...raw.data, ...state, ...completion }) ?? state.status === "completed");
        const previousData = { ...previous?.event.data };
        if (previous && typeof outputTruncated !== "boolean") delete previousData.outputTruncated;
        if (previous && state.error === undefined) delete previousData.error;
        if (previous && state.metadata === undefined) {
          delete previousData.metadata;
          delete previousData.exitCode;
        } else if (previous && !Number.isSafeInteger(exitCode)) delete previousData.exitCode;
        const data = { ...previousData, ...raw.data, ...completion, status: success ? "completed" : "error", success };
        if (previous) updatePartSnapshot(previous, raw, createSessionEvent(sourceMetadata(raw), "tool.execution_complete", data));
        else {
          emit("tool.execution_complete", data);
          if (identity !== undefined) toolCompletions.set(identity, { event: logEntries.at(-1), raw });
        }
      }
    } else if (raw.type === "step_start") {
      emit("opencode.step_start", { sessionId: raw.sessionID, partId: partID });
    } else if (raw.type === "step_finish" || raw.type === "opencode.compaction") {
      const compaction = raw.type === "opencode.compaction";
      const reports = compaction ? compactionReports : stepReports;
      const tokens = part.tokens;
      const identity = openCodeMessageIdentity(raw.sessionID, raw.type, part.messageID, partID) ?? Symbol();
      const report = reports.get(identity) ?? {};
      if (tokens && typeof tokens === "object" && !Array.isArray(tokens)) {
        const snapshot = normalizeSessionUsage({
          total_tokens: tokens.total,
          input_tokens: tokens.input,
          output_tokens: tokens.output,
          reasoning_output_tokens: tokens.reasoning,
          cache_read_input_tokens: tokens.cache?.read,
          cache_creation_input_tokens: tokens.cache?.write,
        });
        report.usage = reconcileSessionUsage(report.usage, snapshot);
      }
      if (isMetric(part.cost)) report.cost = part.cost;
      reports.set(identity, report);
      if (compaction) {
        emit("opencode.compaction", { sessionId: raw.sessionID, partId: partID });
        resultSource = raw;
        continue;
      }
      if (part.reason === "content-filter" || part.reason === "refusal") {
        const reason = part.reason === "content-filter" ? "content_filter" : "refusal";
        const messageIdentity = openCodeMessageIdentity(raw.sessionID, "text", part.messageID);
        const messages = messageIdentity !== undefined && messageParts.has(messageIdentity) ? [messageParts.get(messageIdentity)] : [];
        const refusalIdentity = messageIdentity ?? (typeof partID === "string" ? JSON.stringify([raw.sessionID, "finish", partID]) : undefined);
        if (!messages.length) {
          const previous = refusalIdentity !== undefined ? finishRefusals.get(refusalIdentity) : undefined;
          if (previous) updatePartSnapshot(previous, raw, createSessionEvent(sourceMetadata(raw), "assistant.refusal", { reason }));
          else {
            emit("assistant.refusal", { reason });
            if (refusalIdentity !== undefined) finishRefusals.set(refusalIdentity, { event: logEntries.at(-1), raw });
          }
        }
        for (const previous of messages) {
          previous.event.type = "assistant.refusal";
          previous.event.data.reason = reason;
        }
      }
      emit("opencode.step_finish", { sessionId: raw.sessionID, partId: partID, reason: part.reason });
      resultSource = raw;
    } else if (raw.type === "error") {
      errors.push(structuredClone(raw.error));
      emit("session.error", { error: raw.error });
      resultSource = raw;
    }
  }
  if (resultSource) {
    for (const report of [...stepReports.values(), ...compactionReports.values()]) {
      accumulateSessionUsage(usage, report.usage);
      if (!costOverflowed && isMetric(report.cost)) {
        const sum = (totalCostUsd ?? 0) + report.cost;
        if (isMetric(sum)) totalCostUsd = sum;
        else {
          costOverflowed = true;
          totalCostUsd = undefined;
        }
      }
    }
    if (Object.keys(usage).length) usage.input_tokens_include_cache = false;
    logEntries.push(
      createSessionEvent(sourceMetadata(resultSource), "session.result", {
        ...(stepReports.size > 0 ? { numTurns: stepReports.size } : {}),
        ...(Object.keys(usage).length ? { usage } : {}),
        ...(totalCostUsd !== undefined ? { totalCostUsd } : {}),
        ...(errors.length ? { errors, status: "error" } : {}),
      })
    );
  }
  const mcpFailures = new Set(getOpenCodeMCPFailures(content));
  for (const event of logEntries) {
    if (event.type === "opencode.mcp_failure" && typeof event.data.serverName === "string") mcpFailures.add(event.data.serverName);
  }
  return {
    markdown: logEntries.length ? generateCopilotCliStyleSummary(logEntries) : buildStepSummaryDetailsSection("OpenCode", "Log format not recognized as OpenCode JSONL. Raw content is omitted."),
    logEntries,
    mcpFailures: [...mcpFailures],
    maxTurnsHit: logEntries.some(event => event.type === "opencode.max_turns"),
  };
}

module.exports = { main, parseOpenCodeLog, isOpenCodeEvent, getOpenCodeMCPFailures, getOpenCodeSessionSteps };
