// @ts-check

const fs = require("fs");
const { isSessionEvent } = require("./agent_session.cjs");
const { collectUnifiedSession, parseEngineSession } = require("./unified_session.cjs");
const { serializeSessionArtifact } = require("./session_artifact.cjs");
const { generateCopilotCliStyleSummary } = require("./log_parser_shared.cjs");
const { validateSessionFileHeader } = require("./unified_session_render.cjs");
const { normalizeEngineLogEntries, parseBehaviorLog } = require("./engine_log_parser.cjs");

class UnrecognizedSessionError extends Error {
  constructor() {
    super("No recognizable agent session found in the agent artifact");
    this.name = "UnrecognizedSessionError";
  }
}

/** @param {string} inputPath @returns {string} */
function readSessionInput(inputPath) {
  try {
    return fs.readFileSync(inputPath, "utf8");
  } catch (error) {
    throw new Error(`Failed to read session input ${inputPath}`, { cause: error });
  }
}

/**
 * CLI adapter for the same parsers and renderers used by workflow summaries.
 * Diagnostics go to stderr; stdout contains only the requested session text.
 * @param {string[]} args
 * @returns {string}
 */
function sessionCLI(args) {
  const [mode, inputPath, engine, customParser] = args;
  if (!inputPath) throw new Error("A session input path is required");
  if (mode === "reconstruct") {
    const { events, maskedValues } = collectUnifiedSession({
      rootDir: inputPath,
      ...(engine ? { engine } : {}),
      warn: message => console.error(message),
    });
    if (!events.some(event => event.provenance?.component === "agent" || event.type === "agent.execution")) throw new UnrecognizedSessionError();
    return serializeSessionArtifact(events, maskedValues);
  }
  if (mode === "markdown") {
    const events = readSessionInput(inputPath)
      .split(/\r?\n/)
      .filter(line => line.trim())
      .map((line, index) => {
        let event;
        try {
          event = JSON.parse(line);
        } catch (error) {
          throw new Error(`Invalid session JSONL at record ${index + 1}`, { cause: error });
        }
        if (!isSessionEvent(event)) throw new Error(`Invalid session event at record ${index + 1}`);
        return event;
      });
    validateSessionFileHeader(events);
    return generateCopilotCliStyleSummary(events) + "\n";
  }
  if (mode === "agent-markdown") {
    if (!engine) throw new Error("An engine is required to parse an agent log");
    if (customParser) {
      if (customParser !== "behavior_log_parser.cjs") throw new Error("Invalid custom log parser");
      const content = readSessionInput(inputPath);
      const parsed = parseBehaviorLog(content, engine) ?? require("./" + customParser).parseLog(content);
      if (typeof parsed === "string") return parsed + "\n";
      if (Array.isArray(parsed?.mcpFailures) && parsed.mcpFailures.length > 0) {
        throw new Error(`MCP server(s) failed to launch: ${parsed.mcpFailures.join(", ")}`);
      }
      if (parsed?.maxTurnsHit) throw new Error("Agent execution stopped: max-turns limit reached");
      if (Array.isArray(parsed?.logEntries) && parsed.logEntries.length > 0) {
        return generateCopilotCliStyleSummary(normalizeEngineLogEntries(parsed.logEntries, engine)) + "\n";
      }
      if (typeof parsed?.markdown === "string") return parsed.markdown + "\n";
      return generateCopilotCliStyleSummary(parsed?.logEntries ?? []) + "\n";
    }
    return generateCopilotCliStyleSummary(parseEngineSession(readSessionInput(inputPath), engine)) + "\n";
  }
  throw new Error(`Unsupported session CLI mode: ${mode}`);
}

if (require.main === module) {
  try {
    process.stdout.write(sessionCLI(process.argv.slice(2)));
  } catch (error) {
    console.error(error instanceof Error ? error.message : String(error));
    process.exitCode = error instanceof UnrecognizedSessionError ? 2 : 1;
  }
}

module.exports = { sessionCLI, UnrecognizedSessionError };
