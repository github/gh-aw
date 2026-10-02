// @ts-check

const fs = require("fs");
const path = require("path");
const { isSessionEvent } = require("./agent_session.cjs");
const { collectAddMaskedValues, writeSessionArtifact, removeFailedSessionArtifacts } = require("./session_artifact.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");

const SESSION_FILE_FORMAT_VERSION = 1;

/** @typedef {import("./types/agent_session").SessionEvent} SessionEvent */
/** @typedef {{component: string, phase: string, path: string, events: SessionEvent[], timestampUnit?: "seconds" | "milliseconds"}} SessionSource */

/**
 * Timestamp units come from the source schema, never from the value's magnitude.
 * Native timestamps stay unchanged; this value is only an ordering key.
 * @param {any} record
 * @param {"seconds" | "milliseconds"} [unit]
 * @returns {number | undefined}
 */
function sessionTimestamp(record, unit = "milliseconds") {
  const value = record.timestamp ?? record.ts ?? record.time ?? record.created_at ?? record.message?.timestamp;
  if (typeof value === "number") {
    const ms = unit === "seconds" ? value * 1000 : value;
    return Number.isFinite(ms) && Math.abs(ms) <= 8640000000000000 ? ms : undefined;
  }
  if (typeof value !== "string" || !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/i.test(value)) return undefined;
  const ms = Date.parse(value);
  return Number.isFinite(ms) ? ms : undefined;
}

/**
 * Source-local positions disambiguate repeated native IDs and expanded records.
 * Equal timestamps and untimed observations retain deterministic source order.
 * @param {SessionSource[]} sources
 * @returns {import("./types/agent_session").UnifiedSession}
 */
function mergeSessionSources(sources) {
  const events = sources.flatMap(source =>
    source.events.map((event, index) => {
      const timestampMs = sessionTimestamp(event, source.timestampUnit);
      return {
        ...structuredClone(event),
        provenance: {
          component: source.component,
          phase: source.phase,
          path: source.path,
          index,
          ...(timestampMs !== undefined ? { timestampMs } : {}),
          ...(Object.hasOwn(event, "provenance") ? { native: structuredClone(event.provenance) } : {}),
        },
      };
    })
  );
  events.sort((left, right) => {
    const leftTime = left.provenance.timestampMs ?? Infinity;
    const rightTime = right.provenance.timestampMs ?? Infinity;
    return leftTime === rightTime ? 0 : leftTime < rightTime ? -1 : 1;
  });
  return events;
}

/** @param {"mcp" | "firewall"} component @param {any} record @returns {SessionEvent} */
function normalizeRuntimeEvent(component, record) {
  /** @type {`${string}.${string}`} */
  let type = `${component}.event`;
  const kind = record.event ?? record.type ?? record.event_name ?? record.eventName;
  if (component === "mcp") {
    /** @type {Record<string, `${string}.${string}`>} */
    const kinds = {
      REQUEST: "mcp.rpc.request",
      rpc_request: "mcp.rpc.request",
      RESPONSE: "mcp.rpc.response",
      rpc_response: "mcp.rpc.response",
      DIFC_FILTERED: "mcp.difc.filtered",
      difc_filtered: "mcp.difc.filtered",
      GUARD_POLICY_BLOCKED: "mcp.guard.blocked",
      tool_call: "mcp.tool_call",
      rpc_call: "mcp.tool_call",
    };
    if (Object.hasOwn(kinds, kind)) type = kinds[kind];
  } else if (component === "firewall") {
    if (kind === "http_access" || record.decision !== undefined) type = "firewall.http_access";
    else if (kind === "token_usage") type = "firewall.token_usage";
    else if (["token_steering", "timeout_steering"].includes(kind)) type = "firewall.steering";
  }
  return {
    type,
    data: structuredClone(record),
    ...(record.timestamp !== undefined ? { timestamp: record.timestamp } : {}),
    ...(record.ts !== undefined ? { ts: record.ts } : {}),
    ...(record.time !== undefined ? { time: record.time } : {}),
  };
}

/**
 * @param {string} content
 * @param {string} engine
 * @returns {SessionEvent[]}
 */
function parseEngineSession(content, engine) {
  const parsers = {
    claude: ["parse_claude_log.cjs", "parseClaudeLog"],
    copilot: ["parse_copilot_log.cjs", "parseCopilotLog"],
    codex: ["parse_codex_log.cjs", "parseCodexLog"],
    gemini: ["parse_gemini_log.cjs", "parseGeminiLog"],
    pi: ["parse_pi_log.cjs", "parsePiLog"],
    custom: ["parse_custom_log.cjs", "parseCustomLog"],
  };
  const [moduleName, functionName] = Object.hasOwn(parsers, engine) ? parsers[engine] : parsers.custom;
  return require(`./${moduleName}`)[functionName](content).logEntries ?? [];
}

/**
 * Collect only known runtime directories; never follow artifact symlinks.
 * Replicated firewall files use logs > audit > legacy precedence, including empty files.
 * @param {{rootDir?: string, engine?: string, warn?: (message: string) => void}} [options]
 * @returns {{events: SessionEvent[], maskedValues: string[]}}
 */
function collectUnifiedSession({ rootDir = "/tmp/gh-aw", engine, warn = message => console.warn(message) } = {}) {
  /** @type {SessionSource[]} */
  const sources = [];
  const masks = new Set();
  /** @type {SessionEvent[]} */
  const warnings = [];
  const report = (file, code, line) => {
    const relative = path.relative(rootDir, file);
    warn(`Unified session: ${code} in ${relative}${line === undefined ? "" : `:${line}`}`);
    warnings.push({ type: "session.collection_warning", data: { path: relative, code, ...(line !== undefined ? { line } : {}) } });
  };
  const exists = file => {
    if (!fs.existsSync(file)) return false;
    let current = rootDir;
    for (const part of path.relative(rootDir, file).split(path.sep)) {
      current = path.join(current, part);
      if (fs.lstatSync(current).isSymbolicLink()) {
        report(current, "symlink_not_read", undefined);
        return false;
      }
    }
    return fs.lstatSync(file).isFile();
  };
  const read = file => {
    try {
      const content = fs.readFileSync(file, "utf8");
      for (const mask of collectAddMaskedValues(content)) masks.add(mask);
      return content;
    } catch (error) {
      throw new Error(`Failed to read unified session source ${path.relative(rootDir, file)}: ${getErrorMessage(error)}`, { cause: error });
    }
  };
  const records = file => {
    const content = read(file);
    if (file.endsWith(".json")) {
      try {
        const value = JSON.parse(content);
        return Array.isArray(value) ? value : [value];
      } catch {
        report(file, "malformed_json", undefined);
        return [];
      }
    }
    const values = [];
    for (const [index, raw] of content.split(/\r?\n/).entries()) {
      if (!raw.trim()) continue;
      try {
        values.push(JSON.parse(raw));
      } catch {
        report(file, "malformed_jsonl", index + 1);
      }
    }
    return values;
  };
  /**
   * @param {string} file
   * @param {string} component
   * @param {string} phase
   * @param {`${string}.${string}` | undefined} type
   * @param {"seconds" | "milliseconds"} [timestampUnit]
   */
  const add = (file, component, phase, type, timestampUnit = "milliseconds") => {
    if (!exists(file)) return;
    const events = [];
    for (const record of records(file)) {
      if (!record || typeof record !== "object" || Array.isArray(record)) {
        report(file, "non_object_record", undefined);
        continue;
      }
      if (component === "agent") {
        if (isSessionEvent(record)) events.push(record);
        else report(file, "non_canonical_agent_event", undefined);
      } else if (type) {
        events.push({ type, data: record, ...(record.timestamp !== undefined ? { timestamp: record.timestamp } : {}), ...(record.created_at !== undefined ? { created_at: record.created_at } : {}) });
      } else if (component === "mcp" || component === "firewall") events.push(normalizeRuntimeEvent(component, record));
      else throw new Error(`Missing event mapping for ${component}`);
    }
    sources.push({ component, phase, path: path.relative(rootDir, file), events, timestampUnit });
  };
  const walk = (directory, depth = 0) => {
    if (!fs.existsSync(directory)) return [];
    let current = rootDir;
    for (const part of path.relative(rootDir, directory).split(path.sep)) {
      current = path.join(current, part);
      if (fs.lstatSync(current).isSymbolicLink()) {
        report(current, "symlink_not_read", undefined);
        return [];
      }
    }
    if (depth > 8) {
      report(directory, "directory_depth_limit", undefined);
      return [];
    }
    const files = [];
    let entries;
    try {
      entries = fs.readdirSync(directory, { withFileTypes: true });
    } catch (error) {
      throw new Error(`Failed to enumerate unified session sources ${path.relative(rootDir, directory)}: ${getErrorMessage(error)}`, { cause: error });
    }
    for (const entry of entries.sort((a, b) => (a.name === b.name ? 0 : a.name < b.name ? -1 : 1))) {
      const file = path.join(directory, entry.name);
      if (entry.isSymbolicLink()) report(file, "symlink_not_read", undefined);
      else if (entry.isDirectory()) files.push(...walk(file, depth + 1));
      else if (entry.isFile() && entry.name.endsWith(".jsonl")) files.push(file);
    }
    return files;
  };
  const choose = candidates => candidates.map(file => path.join(rootDir, file)).find(exists);
  const metadata = choose(["aw_info.json", "usage/aw_info.json"]);
  if (metadata) {
    add(metadata, "workflow", "activation", "workflow.info");
    const observedEngine = sources.at(-1)?.events[0]?.data.engine_id;
    if (engine === undefined && typeof observedEngine === "string") engine = observedEngine;
  }
  const stdio = path.join(rootDir, "agent-stdio.log");
  // Masks can be registered in stdio even when native session events are preferred.
  if (exists(stdio)) read(stdio);
  const canonical = path.join(rootDir, "agent-session.jsonl");
  const native = walk(path.join(rootDir, "sandbox/agent/logs/copilot-session-state")).filter(file => path.basename(file) === "events.jsonl");
  if (exists(canonical)) add(canonical, "agent", "agent", undefined);
  else if (native.length) {
    for (const file of native) add(file, "agent", "agent", undefined);
  } else {
    const file = choose(["pi-streaming.jsonl", "agent-stdio.log"]);
    if (file) {
      const content = read(file);
      const events = parseEngineSession(content, engine ?? (file.endsWith("pi-streaming.jsonl") ? "pi" : "custom"));
      if (!events.length && content.trim()) report(file, "unrecognized_engine_log", undefined);
      sources.push({ component: "agent", phase: "agent", path: path.relative(rootDir, file), events });
    }
  }
  for (const [prefix, phase] of [
    ["", "agent"],
    ["threat-detection/", "detection"],
  ]) {
    for (const file of walk(path.join(rootDir, prefix, "mcp-logs"))) add(file, "mcp", phase, undefined);
    const seen = new Set();
    for (const layout of ["sandbox/firewall/logs", "sandbox/firewall/audit", "sandbox/firewall-audit-logs", "firewall-logs", "firewall-audit-logs"]) {
      const directory = path.join(rootDir, prefix, layout);
      for (const file of walk(directory)) {
        const relative = path.relative(directory, file);
        if (seen.has(relative)) continue;
        seen.add(relative);
        const unit = relative === "audit.jsonl" ? "seconds" : "milliseconds";
        add(file, "firewall", phase, undefined, unit);
      }
    }
  }
  /** @type {Array<[string[], string, string, `${string}.${string}`]>} */
  const observations = [
    [["safeoutputs.jsonl"], "safe_output", "agent", "safe_output.request"],
    [["safe-output-items.jsonl"], "safe_output", "safe_outputs", "safe_output.result"],
    [["safe-output-errors.json"], "safe_output", "safe_outputs", "safe_output.error"],
    [["usage/experiment/state.jsonl", "experiments/state.jsonl"], "experiment", "activation", "experiment.state"],
    [["usage/experiment/state.json", "experiments/state.json"], "experiment", "activation", "experiment.state"],
    [["usage/experiment/assignments.json", "experiments/assignments.json"], "experiment", "activation", "experiment.assignment"],
    [["usage/graders/grader_manifest.json", "agent/graders/grader_manifest.json"], "grader", "agent", "grader.manifest"],
    [["usage/graders/grader_results.json", "agent/graders/grader_results.json"], "grader", "agent", "grader.result"],
    [["usage/evals.jsonl", "evals/evals.jsonl"], "eval", "evals", "eval.result"],
    [["usage/agent_usage.jsonl", "agent_usage.jsonl"], "usage", "agent", "usage.report"],
    [["usage/detection_usage.jsonl", "threat-detection/detection_usage.jsonl"], "usage", "detection", "usage.report"],
    [["usage/evals/token_usage.jsonl", "evals/evals_token_usage.jsonl"], "usage", "evals", "usage.report"],
    [["usage/agent/execution.json", "agent_execution.json"], "execution", "agent", "execution.result"],
    [["usage/detection/execution.json", "threat-detection/execution.json"], "execution", "detection", "execution.result"],
    [["usage/evals/execution.json", "evals/evals/execution.json", "evals/execution.json"], "execution", "evals", "execution.result"],
    [["usage/detection/detection_result.json", "threat-detection/detection_result.json"], "detection", "detection", "detection.result"],
  ];
  for (const [candidates, component, phase, type] of observations) {
    const file = choose(candidates);
    if (file) add(file, component, phase, type);
  }
  /** @type {SessionEvent} */
  const summary = {
    type: "session.collection",
    data: {
      sources: sources.map(({ events, ...source }) => ({ ...source, events: events.length })),
      warnings: warnings.length,
      untimedEvents: sources.reduce((total, source) => total + source.events.filter(event => sessionTimestamp(event, source.timestampUnit) === undefined).length, 0),
      absentComponents: ["agent", "mcp", "firewall", "safe_output", "experiment", "grader", "eval"].filter(component => !sources.some(source => source.component === component)),
    },
  };
  /** @type {import("./types/agent_session").SessionFileFormatEvent} */
  const format = { type: "session.format", data: { version: SESSION_FILE_FORMAT_VERSION } };
  sources.push({ component: "collector", phase: "conclusion", path: "usage/aw_session.jsonl", events: [format, ...warnings, summary] });
  const events = mergeSessionSources(sources);
  const formatIndex = events.findIndex(event => event.type === "session.format" && event.provenance.component === "collector" && event.provenance.index === 0);
  // File metadata leads the stream without inventing a timestamp for it.
  const [header] = events.splice(formatIndex, 1);
  return { events: [header, ...events], maskedValues: [...masks] };
}

/**
 * @param {{rootDir?: string, engine?: string, outputPath?: string, warn?: (message: string) => void}} [options]
 * @returns {SessionEvent[]}
 */
function writeUnifiedSession(options = {}) {
  const outputPath = options.outputPath ?? path.join(options.rootDir ?? "/tmp/gh-aw", "usage/aw_session.jsonl");
  try {
    const { events, maskedValues } = collectUnifiedSession(options);
    writeSessionArtifact(outputPath, events, maskedValues);
    return events;
  } catch (error) {
    removeFailedSessionArtifacts([outputPath], error);
    throw error;
  }
}

if (require.main === module) {
  require("./shim.cjs");
  try {
    writeUnifiedSession();
  } catch (error) {
    console.error(`Failed to collect unified session: ${getErrorMessage(error)}`);
    process.exitCode = 1;
  }
}

module.exports = { SESSION_FILE_FORMAT_VERSION, sessionTimestamp, mergeSessionSources, normalizeRuntimeEvent, parseEngineSession, collectUnifiedSession, writeUnifiedSession };
