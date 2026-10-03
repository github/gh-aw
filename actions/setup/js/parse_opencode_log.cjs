// @ts-check

const { createEngineLogParser, parseLogEntries, generateCopilotCliStyleSummary, buildStepSummaryDetailsSection } = require("./log_parser_shared.cjs");
const { createSessionEvent, isSessionEvent, accumulateSessionUsage, isMetric, isTokenCount } = require("./agent_session.cjs");

const main = createEngineLogParser({ parserName: "OpenCode", parseFunction: parseOpenCodeLog, supportsDirectories: false });
/** @param {any} entry @returns {boolean} */
function isOpenCodeEvent(entry) {
  if (!entry || typeof entry !== "object" || Array.isArray(entry) || typeof entry.sessionID !== "string") return false;
  if (entry.type === "error") return !!entry.error && typeof entry.error === "object" && !Array.isArray(entry.error);
  const part = entry.part;
  if (!part || typeof part !== "object" || Array.isArray(part)) return false;
  if (entry.type === "text" || entry.type === "reasoning") return typeof part.text === "string";
  if (entry.type === "tool_use") return typeof part.tool === "string" && !!part.state && typeof part.state === "object" && !Array.isArray(part.state);
  return entry.type === "step_start" || entry.type === "step_finish";
}

/** @param {string} content @returns {string[]} */
function getOpenCodeMCPFailures(content) {
  const failures = new Set();
  for (const line of content.split(/\r?\n/)) {
    if (!/^timestamp=\S+ level=(?:WARN|ERROR)\b/.test(line) || !/\bmessage="server unavailable"(?:\s|$)/.test(line) || !/\btype=(?:remote|local)\b/.test(line) || !/\bstatus=failed\b/.test(line)) continue;
    const match = line.match(/\bkey=("(?:\\.|[^"\\])*"|[^\s]+)(?:\s|$)/);
    if (!match) continue;
    if (match[1].startsWith('"')) {
      try {
        failures.add(JSON.parse(match[1]));
      } catch {
        /* malformed logfmt field */
      }
    } else failures.add(match[1]);
  }
  return [...failures];
}

/** @param {any} raw @returns {any} */
function sourceMetadata(raw) {
  const source = { ...raw };
  if (!Object.hasOwn(raw, "id") && typeof raw.part?.id === "string") source.id = raw.part.id;
  if (!Object.hasOwn(raw, "parentId") && typeof raw.part?.messageID === "string") source.parentId = raw.part.messageID;
  return source;
}

/**
 * OpenCode `run --format json` emits completed parts, not Claude-style messages.
 * A completed tool part supplies both the invocation and its result.
 * @param {string} content
 * @returns {{markdown: string, logEntries: import("./types/agent_session").SessionEvent[], mcpFailures: string[], maxTurnsHit: boolean}}
 */
function parseOpenCodeLog(content) {
  const records = (parseLogEntries(content) ?? []).filter(entry => isSessionEvent(entry) || isOpenCodeEvent(entry));
  /** @type {import("./types/agent_session").SessionEvent[]} */
  const logEntries = [];
  const sessions = new Set();
  const parts = new Set();
  const toolStarts = new Set();
  /** @type {Record<string, any>} */
  const usage = {};
  const errors = [];
  let turns = 0;
  let totalCostUsd;
  let reasoningOverflowed = false;
  let totalTokensOverflowed = false;
  let resultSource;

  for (const raw of records) {
    if (isSessionEvent(raw)) {
      logEntries.push(structuredClone(raw));
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
    // Repeated snapshots are the same observation; IDs are scoped to a session.
    if (typeof part?.id === "string") {
      const identity = JSON.stringify([raw.sessionID, raw.type, part.id, part.state?.status]);
      if (parts.has(identity)) continue;
      parts.add(identity);
    }
    if (raw.type === "text" || raw.type === "reasoning") {
      if (typeof part.text === "string") {
        emit(raw.type === "text" ? "assistant.message" : "assistant.reasoning", { content: part.text });
      }
    } else if (raw.type === "tool_use") {
      const state = part.state;
      if (!state || typeof state !== "object") continue;
      const data = { toolCallId: part.callID, toolName: part.tool, input: state.input };
      const callIdentity = typeof part.callID === "string" ? JSON.stringify([raw.sessionID, part.callID]) : undefined;
      if (callIdentity === undefined || !toolStarts.has(callIdentity)) {
        emit("tool.execution_start", data);
        if (callIdentity !== undefined) toolStarts.add(callIdentity);
      }
      if (state.status === "completed" || state.status === "error") {
        const start = state.time?.start;
        const end = state.time?.end;
        emit("tool.execution_complete", {
          toolCallId: part.callID,
          toolName: part.tool,
          status: state.status,
          success: state.status === "completed",
          output: state.output,
          ...(state.error !== undefined ? { error: state.error } : {}),
          ...(isMetric(start) && isMetric(end) && end >= start ? { durationMs: end - start } : {}),
        });
      }
    } else if (raw.type === "step_start") {
      emit("opencode.step_start", { sessionId: raw.sessionID, partId: part.id });
    } else if (raw.type === "step_finish") {
      turns++;
      const tokens = part.tokens;
      if (tokens && typeof tokens === "object") {
        accumulateSessionUsage(usage, {
          input_tokens: tokens.input,
          output_tokens: tokens.output,
          cache_read_input_tokens: tokens.cache?.read,
          cache_creation_input_tokens: tokens.cache?.write,
        });
        if (!reasoningOverflowed && isTokenCount(tokens.reasoning)) {
          const sum = (usage.reasoning_output_tokens ?? 0) + tokens.reasoning;
          if (isTokenCount(sum)) usage.reasoning_output_tokens = sum;
          else {
            reasoningOverflowed = true;
            delete usage.reasoning_output_tokens;
          }
        }
        if (!totalTokensOverflowed && isTokenCount(tokens.total)) {
          const sum = (usage.total_tokens ?? 0) + tokens.total;
          if (isTokenCount(sum)) usage.total_tokens = sum;
          else {
            totalTokensOverflowed = true;
            delete usage.total_tokens;
          }
        }
        usage.input_tokens_include_cache = false;
      }
      if (isMetric(part.cost)) {
        const sum = (totalCostUsd ?? 0) + part.cost;
        if (isMetric(sum)) totalCostUsd = sum;
      }
      emit("opencode.step_finish", { sessionId: raw.sessionID, partId: part.id, reason: part.reason });
      resultSource = raw;
    } else if (raw.type === "error") {
      errors.push(structuredClone(raw.error));
      emit("session.error", { error: raw.error });
      resultSource = raw;
    }
  }
  if (resultSource) {
    logEntries.push(
      createSessionEvent(sourceMetadata(resultSource), "session.result", {
        ...(turns > 0 ? { numTurns: turns } : {}),
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

module.exports = { main, parseOpenCodeLog, isOpenCodeEvent, getOpenCodeMCPFailures };
