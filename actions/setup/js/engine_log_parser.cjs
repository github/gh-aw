// @ts-check

const fs = require("fs");
const path = require("path");
const vm = require("vm");
const { isSessionEvent, normalizeAgentSession } = require("./agent_session.cjs");
const { ERR_PARSE, ERR_SYSTEM, ERR_VALIDATION } = require("./error_codes.cjs");

/** @param {string} filename @returns {string} */
function readEngineParserDefinition(filename) {
  try {
    return fs.readFileSync(filename, "utf8");
  } catch (error) {
    throw new Error(`${ERR_SYSTEM}: Failed to read engine parser definition ${filename}`, { cause: error });
  }
}

/** @param {string} filename @returns {string} */
function resolveEngineParserPath(filename) {
  try {
    return fs.realpathSync(filename);
  } catch (error) {
    throw new Error(`${ERR_SYSTEM}: Failed to resolve engine parser path ${filename}`, { cause: error });
  }
}

/**
 * Use generated behavior parsers in Actions/CLI bundles, or the checkout's
 * catalogued definition when running the same session CLI directly from source.
 * Never load executable code from the downloaded artifact directory.
 * @param {string} engine
 * @returns {((content: string) => any) | undefined}
 */
function loadEngineLogParser(engine) {
  if (!/^[a-z][a-z0-9-]*$/.test(engine)) return undefined;
  const generated = path.join(__dirname, `${engine}_log_parser.cjs`);
  if (fs.existsSync(generated)) return require(generated).parseLog;

  const root = path.resolve(__dirname, "../../..");
  const catalogPath = path.join(root, ".github/aw/engines.json");
  if (!fs.existsSync(catalogPath)) return undefined;
  const catalogContent = readEngineParserDefinition(catalogPath);
  let catalog;
  try {
    catalog = JSON.parse(catalogContent);
  } catch (error) {
    throw new Error(`${ERR_PARSE}: Invalid engine parser catalog ${catalogPath}`, { cause: error });
  }
  if (!catalog || typeof catalog !== "object" || !Array.isArray(catalog.engines)) throw new Error(`${ERR_VALIDATION}: Invalid engine parser catalog structure ${catalogPath}`);
  const entry = catalog.engines?.find(item => item.id === engine);
  const prefix = "github/gh-aw/";
  if (typeof entry?.import !== "string" || !entry.import.startsWith(prefix)) return undefined;
  const relative = entry.import.slice(prefix.length).split("@")[0];
  const definition = path.resolve(root, relative);
  const shared = path.join(root, ".github/workflows/shared") + path.sep;
  if (!definition.startsWith(shared) || !definition.endsWith(".md") || !fs.existsSync(definition)) return undefined;
  const resolved = resolveEngineParserPath(definition);
  const resolvedShared = path.join(resolveEngineParserPath(root), ".github/workflows/shared") + path.sep;
  if (!resolved.startsWith(resolvedShared) || !resolved.endsWith(".md")) return undefined;

  const frontmatter = readEngineParserDefinition(resolved)
    .replace(/\r\n/g, "\n")
    .match(/^---\n([\s\S]*?)\n---(?:\n|$)/)?.[1];
  if (!frontmatter || frontmatter.match(/^  id: ([a-z][a-z0-9-]*)\s*$/m)?.[1] !== engine) return undefined;
  // The definition contract uses an indented literal JavaScript block. Extract
  // only that scalar, not the installation, harness, or other executable fields.
  const block = frontmatter.match(/^    log-parser: \|[-+]?\s*\n((?:[ \t]*\n| {6}[^\n]*(?:\n|$))*)/m)?.[1];
  if (!block?.trim()) return undefined;
  const source = block
    .split("\n")
    .map(line => (line.startsWith("      ") ? line.slice(6) : line))
    .join("\n");
  return vm.compileFunction(`${source}\nreturn parseLog;`, ["require"], { filename: definition })(require);
}

/**
 * Normalize legacy behavior-parser records without assigning an unobserved
 * success to a tool result. Canonical records retain their original semantics.
 * @param {any[]} entries
 * @param {string} engine
 * @returns {import("./types/agent_session").SessionEvent[]}
 */
function normalizeEngineLogEntries(entries, engine) {
  const events = [];
  const tools = new Map();
  for (const entry of entries) {
    const record =
      !isSessionEvent(entry) && Array.isArray(entry?.message?.content)
        ? {
            ...entry,
            message: {
              ...entry.message,
              content: entry.message.content.map(block => (block?.type === "tool_result" && !Object.hasOwn(block, "is_error") && !Object.hasOwn(block, "success") && block.error == null ? { ...block, is_error: undefined } : block)),
            },
          }
        : entry;
    const normalized = isSessionEvent(entry) ? [structuredClone(entry)] : normalizeAgentSession([record], { sourceEngine: engine });
    for (const event of normalized) {
      if (event.type === "tool.execution_start" && event.data.toolCallId !== undefined) tools.set(event.data.toolCallId, event.data.toolName);
      if (!isSessionEvent(entry) && event.type === "tool.execution_complete" && event.data.toolName === undefined) event.data.toolName = tools.get(event.data.toolCallId);
      events.push(event);
    }
  }
  return events;
}

/** @param {string} content @param {string} engine @returns {any | undefined} */
function parseBehaviorLog(content, engine) {
  const records = require("./log_parser_shared.cjs").parseLogEntries(content) ?? [];
  if (records.some(record => (isSessionEvent(record) && record.type !== "agent.execution") || (["assistant", "user"].includes(record?.type) && normalizeEngineLogEntries([record], engine).length > 0))) {
    return { logEntries: normalizeEngineLogEntries(records, engine), mcpFailures: [], maxTurnsHit: false };
  }
  const parse = loadEngineLogParser(engine);
  if (!parse) return undefined;
  const lines = content.trimEnd().split("\n");
  let terminal;
  if ((lines.at(-1) ?? "").trimStart().startsWith("{")) {
    try {
      const record = JSON.parse(lines.at(-1) ?? "");
      if (record.type === "result" && (Object.hasOwn(record, "num_turns") || Object.hasOwn(record, "usage"))) {
        terminal = record;
        lines.pop();
      }
    } catch {
      const message = `${ERR_PARSE}: Malformed terminal JSON for ${engine}; retaining the original engine output`;
      if (global.core?.warning) global.core.warning(message);
      else console.error(message);
    }
  }
  const parsed = parse(lines.join("\n"));
  const entries = Array.isArray(parsed?.logEntries) ? parsed.logEntries : [];
  const events = normalizeEngineLogEntries(entries, engine);
  const logEntries = terminal
    ? [
        ...events.filter(event => event.type !== "session.result" || Object.entries(event.data).some(([key, value]) => value !== undefined && key !== "numTurns" && (key !== "usage" || Object.keys(value ?? {}).length > 0))),
        ...normalizeEngineLogEntries([terminal], engine),
      ]
    : events;
  return { ...parsed, logEntries };
}

module.exports = { loadEngineLogParser, normalizeEngineLogEntries, parseBehaviorLog };
