// @ts-check
const { iterateJsonlEvents } = require("./jsonl_helpers.cjs");

const fs = require("fs");
const path = require("path");
const { isSessionEvent } = require("./agent_session.cjs");
const { hasCopilotConversation, deduplicateCopilotFallback, normalizeCopilotSession } = require("./copilot_session.cjs");
const { collectAddMaskedValues, serializeSessionArtifact, writeSessionArtifact, removeFailedSessionArtifacts } = require("./session_artifact.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { normalizeUnifiedSessionEvent, normalizeModelInventoryData, evalResultRunId } = require("./unified_session_payload.cjs");
const { ERR_SYSTEM, ERR_VALIDATION } = require("./error_codes.cjs");
const { collectAgentExecution, parseAgentExitCode, validateAgentExitCode, isAgentExecutionEvent } = require("./agent_execution.cjs");
const { normalizeEngineLogEntries, normalizeEngineSessionEvents, parseEngineLog } = require("./engine_log_parser.cjs");
const { sessionTimestamp, orderSessionSources } = require("./unified_session_order.cjs");
const { normalizeOtelEvents } = require("./unified_session_otel.cjs");
const { getApiProxyEventName } = require("./steering_helpers.cjs");
const { estimateBaseline, projectModelConfig } = require("./routing_analytics.cjs");

const SESSION_FILE_FORMAT_VERSION = 1;

/** @param {any} data @returns {boolean} */
function isModelEndpointMismatchData(data) {
  return (
    data !== null &&
    typeof data === "object" &&
    !Array.isArray(data) &&
    data.category === "model_endpoint_mismatch" &&
    ["startup", "runtime"].includes(data.phase) &&
    ["configured_model", "resolved_model", "wire_api", "wire_api_source", "detail", "fix"].every(key => typeof data[key] === "string") &&
    Array.isArray(data.supported_endpoints) &&
    data.supported_endpoints.every(endpoint => typeof endpoint === "string") &&
    ["model", "endpoint"].every(key => !Object.hasOwn(data, key) || typeof data[key] === "string")
  );
}

const MODEL_RESOLUTION_STATUSES = ["resolved", "exact", "failed"];
const MODEL_RESOLUTION_CAUSES = ["model_unavailable", "catalog_unavailable", "unsupported_endpoint"];

/**
 * Validate one entry of agent/model-resolution.json, written by the shared
 * "resolve configured model" step (resolve_model_alias.cjs).
 * @param {any} data
 * @returns {boolean}
 */
function isModelResolutionData(data) {
  if (data === null || typeof data !== "object" || Array.isArray(data)) return false;
  if (!["engine", "role", "configured_model"].every(key => typeof data[key] === "string" && data[key] !== "")) return false;
  if (!["resolved_model", "provider"].every(key => data[key] === null || typeof data[key] === "string")) return false;
  if (!MODEL_RESOLUTION_STATUSES.includes(data.status)) return false;
  if (data.status === "failed") return MODEL_RESOLUTION_CAUSES.includes(data.cause) && data.resolved_model === null;
  return data.cause === null && typeof data.resolved_model === "string";
}

/** @typedef {import("./types/agent_session").SessionEvent} SessionEvent */
/** @typedef {{component: string, phase: string, path: string, events: SessionEvent[], indices?: number[], timestampUnit?: import("./unified_session_order.cjs").TimestampUnit}} SessionSource */

/** @param {unknown} value @returns {boolean} */
const isNativePath = value => typeof value === "string" && /^sandbox\/agent\/logs\/copilot-session-state\/[A-Za-z0-9_-]+\/events\.jsonl$/.test(value);

/**
 * Source-local positions disambiguate repeated native IDs and expanded records.
 * Equal timestamps and untimed observations retain deterministic source order.
 * @param {SessionSource[]} sources
 * @returns {import("./types/unified_session").UnifiedSession}
 */
function mergeSessionSources(sources) {
  return orderSessionSources(
    sources.map(source => ({
      ...source,
      events: source.events.map((event, index) => {
        const timestampMs = sessionTimestamp(event, source.timestampUnit);
        const normalized = normalizeUnifiedSessionEvent(event, source.phase);
        const native = event.provenance;
        const sourceIndex =
          source.component === "agent" &&
          isNativePath(source.path) &&
          native &&
          typeof native === "object" &&
          "path" in native &&
          native.path === source.path &&
          (!("phase" in native) || native.phase === source.phase) &&
          "index" in native &&
          typeof native.index === "number" &&
          Number.isSafeInteger(native.index) &&
          native.index >= 0
            ? native.index
            : (source.indices?.[index] ?? index);
        if (event.type === "detection.result" && source.path !== "usage/detection/detection_result.json") {
          delete normalized.data.reason;
        }
        return {
          ...normalized,
          provenance: {
            component: source.component,
            phase: source.phase,
            path: source.path,
            index: sourceIndex,
            ...(timestampMs !== undefined ? { timestampMs } : {}),
            ...(Object.hasOwn(event, "provenance") ? { native: structuredClone(event.provenance) } : {}),
          },
        };
      }),
    }))
  );
}

/** @param {"mcp" | "firewall"} component @param {any} record @returns {SessionEvent} */
function normalizeRuntimeEvent(component, record) {
  /** @type {`${string}.${string}`} */
  let type = `${component}.event`;
  const kind = component === "firewall" ? getApiProxyEventName(record).toLowerCase() : (record.event ?? record.type ?? record.event_name ?? record.eventName);
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
      "mcp.configuration": "mcp.configuration",
    };
    if (Object.hasOwn(kinds, kind)) type = kinds[kind];
  } else if (component === "firewall") {
    if (kind === "http_access" || record.decision !== undefined) type = "firewall.http_access";
    else if (kind === "token_usage") type = "firewall.token_usage";
    else if (kind === "model_routing") type = "firewall.model_routing";
    else if (kind === "upstream_error_response") type = "firewall.upstream_error";
    else if (kind === "steering" || kind.endsWith("_steering")) type = "firewall.steering";
  }
  return {
    type,
    data: structuredClone(type === "mcp.configuration" ? record.data : type === "firewall.steering" ? { ...record, event: kind } : record),
    ...(record.timestamp !== undefined ? { timestamp: record.timestamp } : {}),
    ...(record.ts !== undefined ? { ts: record.ts } : {}),
    ...(record.time !== undefined ? { time: record.time } : {}),
    ...(record.created_at !== undefined ? { created_at: record.created_at } : {}),
  };
}

/**
 * @param {string} content
 * @param {string} engine
 * @returns {SessionEvent[]}
 */
function parseEngineSession(content, engine) {
  const parsed = parseEngineLog(content, engine);
  const events = parsed.logEntries ?? [];
  const observations = events.filter(isAgentExecutionEvent).map(event => event.data);
  if (observations.length) return events;
  const execution = collectAgentExecution({ content, events, observations, ...(engine === "aider" ? { rawSourceEngine: "aider" } : {}) });
  return [...events.filter(event => event.type !== "agent.execution"), ...(execution ? [execution] : [])];
}

/**
 * Collect only known runtime directories; never follow artifact symlinks.
 * Replicated firewall files use logs > audit > legacy precedence, including empty files.
 * @param {{rootDir?: string, engine?: string, protectedSessionCapture?: boolean, warn?: (message: string) => void, dailyAIC?: NodeJS.ProcessEnv, admission?: NodeJS.ProcessEnv, jobResults?: string, safeJobNames?: string, safeJobResults?: string, jobDurations?: Record<string, number>}} [options]
 * @returns {{events: import("./types/unified_session").UnifiedSession, maskedValues: string[]}}
 */
function collectUnifiedSession({
  rootDir = "/tmp/gh-aw",
  engine,
  protectedSessionCapture = false,
  warn = message => console.warn(message),
  dailyAIC = process.env,
  admission = process.env,
  jobResults = process.env.GH_AW_JOB_RESULTS,
  safeJobNames = process.env.GH_AW_SAFE_JOB_NAMES,
  safeJobResults = process.env.GH_AW_SAFE_JOB_RESULTS,
  jobDurations = {},
} = {}) {
  let currentRunId = dailyAIC.GITHUB_RUN_ID;
  let currentRunAttempt = dailyAIC.GITHUB_RUN_ATTEMPT;
  /** @type {SessionSource[]} */
  const sources = [];
  const masks = new Set();
  const incompleteSources = new Set();
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
      throw new Error(`${ERR_SYSTEM}: Failed to read unified session source ${path.relative(rootDir, file)}: ${getErrorMessage(error)}`, { cause: error });
    }
  };
  const recordCache = new Map();
  const malformedJsonSources = new Set();
  /** @type {Map<string, number[]>} */
  const recordLines = new Map();
  const records = file => {
    if (recordCache.has(file)) return recordCache.get(file);
    const content = read(file);
    if (file.endsWith(".json")) {
      try {
        const value = JSON.parse(content);
        const values = Array.isArray(value) ? value : [value];
        recordCache.set(file, values);
        return values;
      } catch {
        report(file, "malformed_json", undefined);
        malformedJsonSources.add(file);
        recordCache.set(file, []);
        return [];
      }
    }
    const values = [];
    const lines = [];
    for (const event of iterateJsonlEvents(content)) {
      if (event.type === "json") {
        values.push(event.value);
        lines.push(event.lineNumber);
      } else if (event.reason !== "blank") {
        incompleteSources.add(file);
        report(file, "malformed_jsonl", event.lineNumber);
      }
    }
    recordCache.set(file, values);
    recordLines.set(file, lines);
    return values;
  };
  /**
   * @param {string} file
   * @param {string} component
   * @param {string} phase
   * @param {`${string}.${string}` | undefined} type
   * @param {import("./unified_session_order.cjs").TimestampUnit} [timestampUnit]
   */
  const add = (file, component, phase, type, timestampUnit = "milliseconds") => {
    if (!exists(file)) return 0;
    const events = [];
    const indices = [];
    // These result types imply run-scoped files; do not reuse them for unscoped sources.
    const isRunResult = type === "grader.result" || type === "eval.result";
    const input = component === "agent" ? normalizeEngineSessionEvents(records(file), engine ?? "custom") : records(file);
    const normalized = component === "agent" ? normalizeEngineLogEntries(input, engine ?? "custom") : [];
    const hasLegacyContent = normalized.some(event => ["assistant.message", "assistant.reasoning", "assistant.refusal", "user.message", "tool.execution_start", "tool.execution_complete"].includes(event.type));
    for (const [index, record] of input.entries()) {
      if (!record || typeof record !== "object" || Array.isArray(record)) {
        incompleteSources.add(file);
        report(file, "non_object_record", undefined);
        continue;
      }
      if (isRunResult) {
        const line = recordLines.get(file)?.[index];
        const runId = type === "grader.result" ? record.run?.id : evalResultRunId(record);
        if (!currentRunId || runId === undefined || runId === null || String(runId) === "") {
          report(file, "unattributed_run_result", line);
          continue;
        }
        if (String(runId) !== String(currentRunId)) continue;
        const attempt = type === "grader.result" ? record.run?.attempt : (record.runAttempt ?? record.run_attempt);
        if (currentRunAttempt && attempt !== undefined && String(attempt) !== String(currentRunAttempt)) continue;
        indices.push(line === undefined ? index : line - 1);
      }
      if (component === "otel") {
        events.push(
          ...normalizeOtelEvents(record, code => {
            incompleteSources.add(file);
            report(file, code, undefined);
          })
        );
      } else if (component === "agent") {
        if (isSessionEvent(record)) events.push(record);
        else if (!hasLegacyContent || normalizeEngineLogEntries([record], engine ?? "custom").length === 0) {
          incompleteSources.add(file);
          report(file, "non_canonical_agent_event", undefined);
        }
      } else if (type === "plugin.installation" && (["plugin", "repository", "ref", "path"].some(key => typeof record[key] !== "string") || record.status !== "installed")) {
        const line = recordLines.get(file)?.[index];
        incompleteSources.add(file);
        report(file, "invalid_plugin_installation", line);
      } else if (type) {
        events.push({ type, data: record, ...Object.fromEntries(["timestamp", "ts", "time", "created_at"].filter(key => record[key] !== undefined).map(key => [key, record[key]])) });
      } else if (component === "mcp" || component === "firewall") events.push(normalizeRuntimeEvent(component, record));
      else throw new Error(`${ERR_VALIDATION}: Missing event mapping for ${component}`);
    }
    if (component === "agent" && hasLegacyContent) {
      const canonicalKeys = new Set(events.map(event => JSON.stringify([event.type, event.id, event.timestamp, event.data.content, event.data.toolCallId, event.data.input, event.data.output])));
      const canonicalRecords = new Set(input.filter(isSessionEvent).map(record => JSON.stringify(record)));
      events.length = 0;
      for (const event of normalized) {
        const key = JSON.stringify([event.type, event.id, event.timestamp, event.data.content, event.data.toolCallId, event.data.input, event.data.output]);
        if (canonicalRecords.has(JSON.stringify(event)) || !canonicalKeys.has(key)) events.push(event);
      }
    }
    sources.push({ component, phase, path: path.relative(rootDir, file), events, ...(isRunResult ? { indices } : {}), timestampUnit });
    return events.filter(event => event.type !== "agent.execution").length;
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
      throw new Error(`${ERR_SYSTEM}: Failed to enumerate unified session sources ${path.relative(rootDir, directory)}: ${getErrorMessage(error)}`, { cause: error });
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
  const metadata = choose(["runtime/aw_info.json", "aw_info.json", "activation/aw_info.json", "usage/aw_info.json"]);
  if (metadata) {
    const phase = metadata === path.join(rootDir, "runtime/aw_info.json") ? "agent" : metadata === path.join(rootDir, "usage/aw_info.json") ? "conclusion" : "activation";
    add(metadata, "workflow", phase, "workflow.info");
    const info = sources.at(-1)?.events[0]?.data;
    const observedEngine = info?.engine_id;
    if (engine === undefined && typeof observedEngine === "string") engine = observedEngine;
    if (info?.run_id !== undefined && info.run_id !== null && String(info.run_id) !== "") {
      currentRunId = String(info.run_id);
      currentRunAttempt = info.run_attempt !== undefined && info.run_attempt !== null ? String(info.run_attempt) : undefined;
    }
  }
  const skillInventory = choose(["skill_inventory.json", "activation/skill_inventory.json"]);
  if (skillInventory) {
    const inventory = records(skillInventory)[0];
    if (inventory && Array.isArray(inventory.skills) && Array.isArray(inventory.conflicts)) {
      const skills = inventory.skills
        .filter(skill => skill && typeof skill.name === "string" && typeof skill.source === "string" && typeof skill.scope === "string" && typeof skill.digest === "string")
        .map(skill => ({
          name: skill.name,
          source: skill.source,
          scope: skill.scope,
          digest: skill.digest,
          ...(typeof skill.purpose === "string" ? { purpose: skill.purpose } : {}),
        }));
      const conflicts = inventory.conflicts.filter(
        conflict => conflict && typeof conflict.name === "string" && ["name_collision", "scope_collision"].includes(conflict.kind) && Array.isArray(conflict.sources) && conflict.sources.every(source => typeof source === "string")
      );
      sources.push({
        component: "skill",
        phase: "activation",
        path: path.relative(rootDir, skillInventory),
        events: [{ type: "skill.inventory", data: { skills } }, ...conflicts.map(conflict => ({ type: "skill.conflict", data: { name: conflict.name, kind: conflict.kind, sources: conflict.sources } }))],
      });
    } else report(skillInventory, "invalid_skill_inventory", undefined);
  }
  /** @type {Array<[string, `${string}.${string}`]>} */
  const prompts = [
    ["system.txt", "prompt.system"],
    ["user.txt", "prompt.user"],
  ];
  for (const [fileName, type] of prompts) {
    const file = path.join(rootDir, "aw-prompts", fileName);
    if (exists(file)) sources.push({ component: "prompt", phase: "activation", path: path.relative(rootDir, file), events: [{ type, data: { content: read(file) } }] });
  }
  const stdio = path.join(rootDir, "agent-stdio.log");
  // Masks can be registered in stdio even when native session events are preferred.
  const stdioContent = !protectedSessionCapture && exists(stdio) ? read(stdio) : "";
  const canonical = path.join(rootDir, "agent-session.jsonl");
  const native = protectedSessionCapture ? [] : walk(path.join(rootDir, "sandbox/agent/logs/copilot-session-state")).filter(file => path.basename(file) === "events.jsonl");
  let agentEvents = add(canonical, "agent", "agent", undefined);
  const canonicalSource = sources.find(source => source.component === "agent" && source.path === "agent-session.jsonl");
  if (protectedSessionCapture && (!canonicalSource?.events.length || incompleteSources.has(canonical))) {
    throw new Error(`${ERR_VALIDATION}: Protected session collection requires a complete verified canonical session`);
  }
  const sessionId = event => (["session.start", "session.init"].includes(event?.type) && typeof event.data?.sessionId === "string" && event.data.sessionId ? event.data.sessionId : undefined);
  /** @param {SessionEvent} event @returns {Record<string, unknown>} */
  const nativeProvenance = event => (event.provenance && typeof event.provenance === "object" && !Array.isArray(event.provenance) ? Object.fromEntries(Object.entries(event.provenance)) : {});
  const canonicalCopilot = !protectedSessionCapture && (engine === "copilot" || canonicalSource?.events.some(event => event.data.sourceEngine === "copilot" || isNativePath(nativeProvenance(event).path)));
  const representedIds = new Set(canonicalCopilot ? canonicalSource?.events.map(sessionId).filter(Boolean) : []);
  /** @type {Map<string, {file: string, records: any[], start?: number}>} */
  const snapshots = new Map();
  const nativePaths = new Map();
  if (!agentEvents || (canonicalCopilot && (representedIds.size || canonicalSource?.events.some(event => isNativePath(nativeProvenance(event).path))))) {
    for (const file of native) {
      const input = records(file);
      const start = input.find(event => event?.type === "session.start" && sessionId(event));
      const id = sessionId(start);
      if (!id) continue;
      nativePaths.set(path.relative(rootDir, file), id);
      const previous = snapshots.get(id);
      if (!previous || input.length > previous.records.length) {
        snapshots.set(id, { file, records: input, start: sessionTimestamp({ timestamp: start.data.startTime ?? start.timestamp }) });
      }
    }
  }
  const canonicalPaths = new Map();
  const canonicalStarts = new Map();
  if (canonicalCopilot) {
    let activeId;
    for (const event of canonicalSource?.events ?? []) {
      const id = sessionId(event);
      if (id) {
        activeId = id;
        const start = sessionTimestamp({ timestamp: event.data.startTime ?? event.timestamp });
        if (start !== undefined) canonicalStarts.set(id, start);
      }
      const nativePath = nativeProvenance(event).path;
      if (isNativePath(nativePath)) {
        const observedId = nativePaths.get(nativePath) ?? activeId;
        if (observedId) {
          canonicalPaths.set(observedId, nativePath);
          representedIds.add(observedId);
        }
      }
    }
  }
  /** @type {Map<SessionSource, {id: string, start?: number}>} */
  const copilotSources = new Map();
  if (canonicalSource && canonicalCopilot) {
    const groups = new Map();
    let activeId;
    for (const event of canonicalSource.events) {
      const id = sessionId(event);
      if (id) activeId = id;
      const nativePath = nativeProvenance(event).path;
      const observedId = nativePaths.get(nativePath) ?? activeId;
      const snapshot = snapshots.get(observedId);
      const sourcePath = isNativePath(nativePath) ? nativePath : (canonicalPaths.get(observedId) ?? (snapshot ? path.relative(rootDir, snapshot.file) : canonicalSource.path));
      const groupKey = JSON.stringify([sourcePath, observedId]);
      let group = groups.get(groupKey);
      if (!group) {
        group = { ...canonicalSource, path: sourcePath, events: [] };
        groups.set(groupKey, group);
        if (isNativePath(sourcePath) || typeof observedId === "string") copilotSources.set(group, { id: observedId ?? sourcePath, start: canonicalStarts.get(observedId) ?? snapshot?.start });
      }
      group.events.push(isNativePath(sourcePath) ? { ...event, provenance: { ...nativeProvenance(event), path: sourcePath, persistedPath: canonicalSource.path } } : event);
    }
    for (const group of groups.values()) {
      if (copilotSources.has(group)) group.events = require("./copilot_session.cjs").normalizeCopilotSession(group.events);
    }
    sources.splice(sources.indexOf(canonicalSource), 1, ...groups.values());
  }
  // A canonical final attempt must not hide distinct earlier native sessions.
  // Represented IDs remain authoritative, even when raw copies are fuller.
  for (const [id, snapshot] of snapshots) {
    if (representedIds.has(id)) continue;
    const events = normalizeCopilotSession(snapshot.records);
    const source = { component: "agent", phase: "agent", path: path.relative(rootDir, snapshot.file), events };
    sources.push(source);
    if (!hasCopilotConversation(events)) {
      if (!incompleteSources.has(snapshot.file)) report(snapshot.file, "native_session_unusable", undefined);
      continue;
    }
    copilotSources.set(source, { id, start: snapshot.start });
    agentEvents += events.filter(event => event.type !== "agent.execution").length;
  }
  if (copilotSources.size) {
    const ordered = [...copilotSources].sort(([, left], [, right]) => (left.start ?? Infinity) - (right.start ?? Infinity));
    const positions = sources.map((source, index) => (copilotSources.has(source) ? index : -1)).filter(index => index >= 0);
    for (const [index, [source]] of ordered.entries()) sources[positions[index]] = source;
  } else if (!agentEvents) {
    for (const file of native) {
      if ([...snapshots.values()].some(snapshot => snapshot.file === file)) continue;
      add(file, "agent", "agent", undefined);
      const source = sources.at(-1);
      if (source?.path === path.relative(rootDir, file)) {
        source.events = normalizeCopilotSession(source.events);
        if (hasCopilotConversation(source.events)) {
          agentEvents += source.events.length;
        } else {
          if (!incompleteSources.has(file)) report(file, "native_session_unusable", undefined);
        }
      }
    }
  }
  if (!protectedSessionCapture && !agentEvents) {
    const file = choose(["sandbox/agent/logs/pi-streaming.jsonl", "pi-streaming.jsonl", "agent-stdio.log"]);
    if (file) {
      const content = read(file);
      const parsed = parseEngineSession(content, engine ?? (file.endsWith("pi-streaming.jsonl") ? "pi" : "custom"));
      const events =
        engine === "copilot" || native.length
          ? deduplicateCopilotFallback(
              parsed,
              sources.filter(source => source.component === "agent").map(source => source.events)
            )
          : parsed;
      if (!parsed.length && content.trim()) report(file, "unrecognized_engine_log", undefined);
      sources.push({ component: "agent", phase: "agent", path: path.relative(rootDir, file), events });
    }
  }
  for (const [prefix, phase] of [
    ["", "agent"],
    ["threat-detection/", "detection"],
  ]) {
    for (const candidates of [[`${prefix}agent/awf-reflect.json`, `${prefix}sandbox/firewall/awf-reflect.json`], [`${prefix}sandbox/firewall/models.json`]]) {
      for (const candidate of candidates) {
        const file = path.join(rootDir, candidate);
        if (!exists(file)) continue;
        const input = records(file);
        if (!input.length && malformedJsonSources.has(file)) continue;
        const inventory = normalizeModelInventoryData(input[0]);
        if (inventory === undefined) {
          report(file, "invalid_model_inventory", undefined);
          continue;
        }
        sources.push({ component: "firewall", phase, path: path.relative(rootDir, file), events: [{ type: "model.inventory", data: { ...inventory } }] });
        break;
      }
    }
    const otel = choose([`${prefix}otel.jsonl`, `usage/${prefix}otel.jsonl`]);
    if (otel) add(otel, "otel", phase, undefined, "nanoseconds");
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
    [["agent/awf-routing-outcome.json"], "agent", "agent", "model_routing.outcome"],
    [["safeoutputs.jsonl"], "safe_output", "agent", "safe_output.request"],
    [["safe-output-items.jsonl"], "safe_output", "safe_outputs", "safe_output.result"],
    [["safe-output-errors.json"], "safe_output", "safe_outputs", "safe_output.error"],
    [["experiments/state.jsonl", "usage/experiment/state.jsonl"], "experiment", "activation", "experiment.state"],
    [["experiments/state.json", "usage/experiment/state.json"], "experiment", "activation", "experiment.state"],
    [["experiments/assignments.json", "usage/experiment/assignments.json"], "experiment", "activation", "experiment.assignment"],
    [["agent/graders/grader_manifest.json", "usage/graders/grader_manifest.json"], "grader", "agent", "grader.manifest"],
    [["agent/graders/grader_results.json", "usage/graders/grader_results.json"], "grader", "agent", "grader.result"],
    [["evals/evals.jsonl", "evals.jsonl", "usage/evals.jsonl"], "eval", "evals", "eval.result"],
    [["agent_usage.jsonl", "usage/agent_usage.jsonl"], "usage", "agent", "usage.report"],
    [["github_rate_limits.jsonl", "usage/github_rate_limits.jsonl"], "github_api", "workflow", "github_api.rate_limit"],
    [["plugin-installations.jsonl", "usage/plugin-installations.jsonl"], "plugin", "agent", "plugin.installation"],
    [["threat-detection/detection_usage.jsonl", "detection_usage.jsonl", "usage/detection_usage.jsonl"], "usage", "detection", "usage.report"],
    [["evals/evals_token_usage.jsonl", "evals_token_usage.jsonl", "usage/evals/token_usage.jsonl"], "usage", "evals", "usage.report"],
    [["agent_execution.json", "usage/agent/execution.json"], "execution", "agent", "execution.result"],
    [["threat-detection/execution.json", "usage/detection/execution.json"], "execution", "detection", "execution.result"],
    [["evals/evals/execution.json", "evals/execution.json", "usage/evals/execution.json"], "execution", "evals", "execution.result"],
    // The sanitized conclusion result includes job outcomes as well as structured or inline verdicts.
    [["usage/detection/detection_result.json", "threat-detection/detection_result.json"], "detection", "detection", "detection.result"],
  ];
  const mismatchFile = path.join(rootDir, "agent/model-endpoint-mismatch.json");
  if (exists(mismatchFile)) {
    try {
      const record = JSON.parse(read(mismatchFile));
      if (!isModelEndpointMismatchData(record)) {
        report(mismatchFile, "invalid_model_endpoint_mismatch", undefined);
      } else {
        const keys = ["category", "phase", "configured_model", "resolved_model", "wire_api", "wire_api_source", "supported_endpoints", "detail", "fix", "model", "endpoint"];
        const data = Object.fromEntries(keys.filter(key => Object.hasOwn(record, key)).map(key => [key, record[key]]));
        sources.push({ component: "model_endpoint", phase: record.phase, path: "agent/model-endpoint-mismatch.json", events: [{ type: "model_endpoint.mismatch", data }] });
      }
    } catch (error) {
      if (!(error instanceof SyntaxError)) throw error;
      report(mismatchFile, "malformed_json", undefined);
    }
  }
  const resolutionFile = path.join(rootDir, "agent/model-resolution.json");
  if (exists(resolutionFile)) {
    try {
      const records = JSON.parse(read(resolutionFile));
      if (!Array.isArray(records) || records.length === 0 || !records.every(isModelResolutionData)) {
        report(resolutionFile, "invalid_model_resolution", undefined);
      } else {
        const keys = ["engine", "role", "configured_model", "resolved_model", "provider", "status", "cause"];
        const events = records.map(record => /** @type {SessionEvent} */ ({ type: "model.resolution", data: Object.fromEntries(keys.map(key => [key, record[key]])) }));
        sources.push({ component: "model_resolution", phase: "agent", path: "agent/model-resolution.json", events });
      }
    } catch (error) {
      if (!(error instanceof SyntaxError)) throw error;
      report(resolutionFile, "malformed_json", undefined);
    }
  }
  for (const [candidates, component, phase, type] of observations) {
    const file = choose(candidates);
    if (type === "model_routing.outcome" && file) {
      /** @type {SessionEvent[]} */
      const events = records(file).flatMap(record => {
        if (!record || typeof record !== "object" || Array.isArray(record)) {
          incompleteSources.add(file);
          report(file, "non_object_record", undefined);
          return [];
        }

        return [/** @type {SessionEvent} */ { type, data: record, ...(record.timestamp !== undefined ? { timestamp: record.timestamp } : {}) }];
      });
      sources.push({ component, phase, path: path.relative(rootDir, file), events });
      continue;
    }
    if (file) add(file, component, phase, type);
  }
  const executionFile = path.join(rootDir, "agent-errors.jsonl");
  if (!protectedSessionCapture) add(executionFile, "agent", "agent", undefined);
  const agentSources = sources.filter(source => source.component === "agent");
  const finalCopilotSource = agentSources.filter(source => copilotSources.has(source)).at(-1);
  const executionAgentSources = finalCopilotSource ? agentSources.filter(source => !copilotSources.has(source) || source === finalCopilotSource) : agentSources;
  const diagnosticSources = [...executionAgentSources.filter(source => source.path === "agent-errors.jsonl"), ...executionAgentSources.filter(source => source.path !== "agent-errors.jsonl")];
  const executionObservations = diagnosticSources.flatMap(source => source.events.filter(isAgentExecutionEvent).map(event => event.data));
  const exitFile = path.join(rootDir, "agent_execution_exit_code.txt");
  const executionSource = sources.find(source => source.component === "execution" && source.phase === "agent");
  const executionEvidence = executionSource?.events[0]?.data;
  const observedExit = executionEvidence?.exitCode ?? executionEvidence?.exit_code;
  const nativeExit = diagnosticSources
    .map(source =>
      source.events
        .filter(isAgentExecutionEvent)
        .reverse()
        .find(event => event.data.exitCode != null || event.data.exit_code != null)
    )
    .find(Boolean)?.data;
  const exitCode = observedExit ?? nativeExit?.exitCode ?? nativeExit?.exit_code;
  const execution = collectAgentExecution({
    content: stdioContent,
    ...(engine === "aider" ? { rawSourceEngine: "aider" } : {}),
    events: executionAgentSources.flatMap(source => source.events),
    observations: executionObservations,
    ...(exists(exitFile) ? { exitCode: parseAgentExitCode(read(exitFile)) } : exitCode !== undefined ? { exitCode: validateAgentExitCode(exitCode) } : {}),
  });
  for (const source of agentSources) source.events = source.events.filter(event => event.type !== "agent.execution");
  if (execution) {
    const primary = executionObservations.length
      ? (agentSources.find(source => source.path === "agent-errors.jsonl")?.path ?? finalCopilotSource?.path ?? agentSources[0]?.path)
      : stdioContent
        ? "agent-stdio.log"
        : (finalCopilotSource?.path ?? agentSources[0]?.path);
    sources.push({ component: "execution", phase: "agent", path: primary ?? executionSource?.path ?? path.relative(rootDir, exitFile), events: [execution] });
  }
  const admissionStatus = admission.GH_AW_ADMISSION_RESULT;
  if (admissionStatus) {
    /** @type {Record<string, string | number>} */
    const data = { status: admissionStatus };
    let valid = ["admitted", "ignored", "skipped"].includes(admissionStatus);
    if (admissionStatus === "admitted") {
      for (const [field, name] of [
        ["maxAiCredits", "GH_AW_ADMISSION_MAX_AI_CREDITS"],
        ["threatDetectionMaxAiCredits", "GH_AW_ADMISSION_MAX_AI_CREDITS_THREAT_DETECTION"],
        ["evalsMaxAiCredits", "GH_AW_ADMISSION_MAX_AI_CREDITS_EVALS"],
      ]) {
        const raw = admission[name];
        if ((raw === undefined || raw === "") && field !== "maxAiCredits") continue;
        if (typeof raw !== "string" || !/^[1-9][0-9]{0,9}$/.test(raw) || Number(raw) > 2147483647) {
          valid = false;
        } else {
          data[field] = Number(raw);
        }
      }
    }
    if (valid) {
      sources.push({ component: "guardrail", phase: "admission", path: "usage/aw_session.jsonl", events: [{ type: "guardrail.admission", data }] });
    } else {
      report(path.join(rootDir, "usage/aw_session.jsonl"), "invalid_admission_metadata", undefined);
    }
  }
  /** @type {SessionEvent[]} */
  const routingEstimates = [];
  const infoSource = metadata ? sources.find(source => source.component === "workflow" && source.path === path.relative(rootDir, metadata)) : undefined;
  const runInfo = infoSource?.events[0]?.type === "workflow.info" ? infoSource.events[0].data : undefined;
  if (metadata && infoSource && runInfo && Object.hasOwn(runInfo, "model_config")) {
    // Older sessions without runner-written configuration keep routing analytics unknown.
    const admissionData = sources.find(source => source.component === "guardrail" && source.phase === "admission")?.events[0]?.data;
    const config = projectModelConfig(runInfo.model_config, admissionData);
    if (!config) {
      report(metadata, "invalid_model_config", undefined);
    } else {
      infoSource.events.push({ type: "model.config", data: config });
      const firewall = sources.filter(source => source.component === "firewall" && source.phase === "agent");
      const proxyRecords = type => firewall.flatMap(source => source.events.filter(event => event.type === type).map(event => event.data));
      const estimate = estimateBaseline({ config, subAgentModels: runInfo.sub_agent_models, tokenUsage: proxyRecords("firewall.token_usage"), routing: proxyRecords("firewall.model_routing") });
      if (estimate) routingEstimates.push({ type: "model.baseline_estimate", data: estimate });
    }
  }
  const status = dailyAIC.GH_AW_DAILY_AI_CREDITS_GUARDRAIL_STATUS;
  if (typeof status === "string" && ["disabled", "skipped", "under_budget", "exceeded", "structural_error", "transient_error"].includes(status) && !sources.some(source => source.events.some(event => event.type === "guardrail.daily_aic"))) {
    /** @type {Record<string, string | number | boolean>} */
    const data = { status };
    if (dailyAIC.GH_AW_DAILY_AI_CREDITS_EXCEEDED === "true" || dailyAIC.GH_AW_DAILY_AI_CREDITS_EXCEEDED === "false") {
      data.exceeded = dailyAIC.GH_AW_DAILY_AI_CREDITS_EXCEEDED === "true";
    }
    for (const [field, name] of [
      ["total", "GH_AW_DAILY_AI_CREDITS_TOTAL"],
      ["estimated", "GH_AW_DAILY_AI_CREDITS_ESTIMATED"],
      ["threshold", "GH_AW_DAILY_AI_CREDITS_THRESHOLD"],
    ]) {
      const raw = dailyAIC[name];
      if (raw !== undefined && raw !== "" && Number.isFinite(Number(raw)) && Number(raw) >= 0) data[field] = Number(raw);
    }
    sources.push({ component: "guardrail", phase: "activation", path: "usage/aw_session.jsonl", events: [{ type: "guardrail.daily_aic", data }] });
  }
  if (jobResults) {
    const file = path.join(rootDir, "usage/aw_session.jsonl");
    const safeJobs = new Set();
    let safeResults;
    if (safeJobResults) {
      try {
        safeResults = JSON.parse(safeJobResults);
        if (!safeResults || typeof safeResults !== "object" || Array.isArray(safeResults)) throw new SyntaxError();
      } catch (error) {
        if (!(error instanceof SyntaxError)) throw error;
        safeResults = {};
        report(file, "invalid_safe_job_results", undefined);
      }
    }
    if (safeJobNames) {
      try {
        const names = JSON.parse(safeJobNames);
        if (!Array.isArray(names) || names.some(name => typeof name !== "string" || !/^[A-Za-z_][A-Za-z0-9_-]*$/.test(name))) throw new SyntaxError();
        for (const name of names) safeJobs.add(name);
      } catch (error) {
        if (!(error instanceof SyntaxError)) throw error;
        report(file, "invalid_safe_job_names", undefined);
      }
    }
    let needs;
    try {
      needs = JSON.parse(jobResults);
    } catch (error) {
      if (!(error instanceof SyntaxError)) throw error;
      report(file, "invalid_job_results", undefined);
    }
    if (needs !== undefined) {
      if (!needs || typeof needs !== "object" || Array.isArray(needs)) {
        report(file, "invalid_job_results", undefined);
      } else {
        /** @type {SessionEvent[]} */
        const events = [];
        /** @type {SessionEvent[]} */
        const outcomes = [];
        for (const [jobId, job] of Object.entries(needs).sort(([left], [right]) => (left < right ? -1 : left > right ? 1 : 0))) {
          if (!/^[A-Za-z_][A-Za-z0-9_-]*$/.test(jobId) || !job || typeof job !== "object" || Array.isArray(job) || !["success", "failure", "cancelled", "skipped"].includes(job.result)) {
            report(file, "invalid_job_result", undefined);
            continue;
          }
          const durationMs = jobDurations[jobId];
          events.push({ type: "workflow.job_result", data: { jobId, result: job.result, ...(job.result !== "skipped" && Number.isSafeInteger(durationMs) && durationMs >= 0 ? { durationMs } : {}) } });
          if (safeJobs.has(jobId)) {
            const rawOutputs = safeResults === undefined ? job.outputs : safeResults[jobId]?.outputs;
            const outputs = rawOutputs && typeof rawOutputs === "object" && !Array.isArray(rawOutputs) ? rawOutputs : {};
            let status = job.result;
            let validStatus = true;
            if (job.result === "success" && outputs.status) {
              if (["success", "failure", "cancelled", "skipped"].includes(outputs.status)) status = outputs.status;
              else {
                validStatus = false;
                report(file, "invalid_safe_job_status", undefined);
              }
            }
            /** @type {{type: string, status: string, url?: string}} */
            const data = { type: jobId, status };
            if (validStatus && status === "success" && outputs.url) {
              try {
                if (typeof outputs.url !== "string" || /[\s\x00-\x1f\x7f]/.test(outputs.url)) throw new TypeError();
                const uri = new URL(outputs.url);
                if (uri.username || uri.password) throw new TypeError();
                data.url = outputs.url;
              } catch {
                report(file, "invalid_safe_job_url", undefined);
              }
            }
            outcomes.push({ type: "safe_output.result", data });
          }
        }
        if (events.length) sources.push({ component: "workflow", phase: "conclusion", path: "usage/aw_session.jsonl", events });
        if (outcomes.length) sources.push({ component: "safe_output", phase: "safe_outputs", path: "usage/aw_session.jsonl", events: outcomes });
      }
    }
  }
  const persistedWarnings = sources.flatMap(source => source.events.filter(event => event.type === "session.collection_warning"));
  const coverageWarnings = [...warnings];
  for (const event of persistedWarnings) {
    const data = event.data;
    const matching = warnings.filter(warning => warning.data.path === data.path && warning.data.code === data.code);
    if (!matching.some(warning => data.line === undefined || warning.data.line === data.line)) coverageWarnings.push(event);
  }
  /** @type {SessionEvent} */
  const summary = {
    type: "session.collection",
    data: {
      sources: sources.map(({ events, indices: _indices, ...source }) => ({ ...source, events: events.length })),
      warnings: coverageWarnings.length,
      untimedEvents: sources.reduce((total, source) => total + source.events.filter(event => sessionTimestamp(event, source.timestampUnit) === undefined).length, 0),
      absentComponents: ["agent", "mcp", "firewall", "safe_output", "experiment", "grader", "eval", "workflow"].filter(component => !sources.some(source => source.component === component)),
    },
  };
  /** @type {import("./types/agent_session").SessionFileFormatEvent} */
  const format = { type: "session.format", data: { version: SESSION_FILE_FORMAT_VERSION } };
  sources.push({ component: "collector", phase: "conclusion", path: "usage/aw_session.jsonl", events: [format, ...warnings, ...routingEstimates, summary] });
  const events = mergeSessionSources(sources);
  const formatIndex = events.findIndex(event => event.type === "session.format" && event.provenance.component === "collector" && event.provenance.index === 0);
  // File metadata leads the stream without inventing a timestamp for it.
  const [header] = events.splice(formatIndex, 1);
  return { events: [header, ...events], maskedValues: [...masks] };
}

/**
 * @param {{rootDir?: string, engine?: string, outputPath?: string, warn?: (message: string) => void, admission?: NodeJS.ProcessEnv, jobDurations?: Record<string, number>, graders?: any[], customGraderIds?: string[]}} [options]
 * @returns {import("./types/unified_session").UnifiedSession}
 */
function writeUnifiedSession(options = {}) {
  const outputPath = options.outputPath ?? path.join(options.rootDir ?? "/tmp/gh-aw", "usage/aw_session.jsonl");
  try {
    const collected = collectUnifiedSession(options);
    const maskedValues = collected.maskedValues;
    let events = collected.events;
    if (options.graders !== undefined) {
      // Grade the exact redacted representation that is published, without
      // rereading the session or any of its source files.
      const session = [];
      for (const entry of iterateJsonlEvents(serializeSessionArtifact(events, maskedValues))) {
        if (entry.type === "json") session.push(entry.value);
        else if (entry.reason !== "blank") throw new Error(`${ERR_VALIDATION}: Invalid serialized unified session`);
      }
      events = require("./grader_artifacts.cjs").writeGraderArtifacts(session, path.dirname(outputPath), options.graders, options.customGraderIds);
    }
    writeSessionArtifact(outputPath, events, maskedValues);
    return events;
  } catch (error) {
    const graderFiles = options.graders === undefined ? [] : ["grader_manifest.json", "grader_results.json"].map(file => path.join(path.dirname(outputPath), "graders", file));
    removeFailedSessionArtifacts([outputPath, ...graderFiles], error);
    throw error;
  }
}

/** @param {{rootDir?: string, engine?: string, outputPath?: string, jobDurations?: Record<string, number>, graders?: any[], customGraderIds?: string[]}} [options] @returns {Promise<void>} */
async function main(options = {}) {
  const jobDurations = options.jobDurations ?? (await require("./workflow_job_durations.cjs").collectJobDurations({ warn: message => global.core.warning(message) }));
  const { readGraderManifestEnvironment, publishGraderSummary } = require("./grader_artifacts.cjs");
  const manifest = options.graders === undefined ? readGraderManifestEnvironment() : { graders: options.graders };
  const events = writeUnifiedSession({ ...options, jobDurations, graders: manifest.graders, customGraderIds: options.customGraderIds ?? manifest.customGraders });
  const { publishUnifiedSessionSummary } = require("./unified_session_render.cjs");
  await publishUnifiedSessionSummary(options.outputPath ?? path.join(options.rootDir ?? "/tmp/gh-aw", "usage/aw_session.jsonl"));
  await publishGraderSummary(events);
}

if (require.main === module) {
  require("./shim.cjs");
  main().catch(error => {
    console.error(`Failed to collect unified session: ${getErrorMessage(error)}`);
    process.exitCode = 1;
  });
}

module.exports = { SESSION_FILE_FORMAT_VERSION, sessionTimestamp, mergeSessionSources, normalizeRuntimeEvent, parseEngineSession, collectUnifiedSession, isModelEndpointMismatchData, isModelResolutionData, writeUnifiedSession, main };
