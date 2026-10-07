// @ts-check

const { stripVTControlCharacters } = require("node:util");
const { createSessionEvent, normalizeAgentSession } = require("./agent_session.cjs");
const { collectAddMaskedValues, redactMaskedValues, isAddMaskCommandLine } = require("./add_mask_redaction.cjs");
const { createEngineLogParser, generateCopilotCliStyleSummary, buildStepSummaryDetailsSection } = require("./log_parser_shared.cjs");

const main = createEngineLogParser({ parserName: "Kiro", parseFunction: parseKiroLog, supportsDirectories: false });

/**
 * Supported Kiro headless stdout uses a CLI version banner, "> " assistant
 * paragraphs, native tool announcements, and timed completion markers.
 * Completion timing alone does not establish success. Overlapping calls have
 * anonymous completions because stdout does not identify their result owners.
 * @param {string} content
 * @returns {{markdown: string, logEntries: import("./types/agent_session").SessionEvent[], mcpFailures: string[], maxTurnsHit: boolean}}
 */
function parseKiroLog(content) {
  const lines = redactMaskedValues(stripVTControlCharacters(content), collectAddMaskedValues(content)).split(/\r\n|\r|\n/);
  const banner = lines.findIndex(line => /^kiro-cli \d+\.\d+\.\d+(?:\s|$)/.test(line));
  const isAssistantLine = line => /^> [^\r\n]*\S/.test(line) && !/^> (?:User|Human|System|Prompt):/i.test(line);
  /** @type {import("./types/agent_session").SessionEvent[]} */
  const entries = [];
  if (banner !== -1 && lines.slice(banner + 1).some(isAssistantLine)) {
    const emit = (line, type, data) => {
      const event = createSessionEvent({ line: line + 1 }, type, data);
      entries.push(event);
      return event;
    };
    emit(banner, "session.init", { sourceEngine: "kiro", agentVersion: lines[banner].slice("kiro-cli ".length).trim() });
    let assistant;
    let command;
    let output = [];
    let pending = [];
    let anonymousCompletions = 0;
    let mode = "idle";
    const flushAssistant = () => {
      if (assistant) assistant.data.content = assistant.data.content.replace(/(?:\n[ \t]*)+$/, "");
      assistant = undefined;
    };
    const finishTool = (line, duration) => {
      const text = output.join("\n").trim();
      const exits = [...text.matchAll(/^EXIT:(\d+)$/gm)].map(match => Number(match[1]));
      const owner = pending.length === 1 ? pending[0] : undefined;
      const reportsExit = /\becho\s+"EXIT:\$(?:\{PIPESTATUS\[0\]\}|\?)"/.test(owner?.data.input?.command ?? "");
      const exitCode = reportsExit && exits.length === 1 && exits[0] <= 255 ? exits[0] : undefined;
      const durationMs = Number(duration) * 1000;
      const errors = text
        .split("\n")
        .filter(line => /^(?:go: download .+\btls:|make: \*\*\* .+ Error \d+|Error: )/.test(line))
        .map(line => line.match(/\btls: .+/)?.[0] ?? line);
      emit(line, "tool.execution_complete", {
        ...(owner ? { toolName: owner.data.toolName } : {}),
        ...(Number.isFinite(durationMs) ? { durationMs } : {}),
        ...(text ? { output: text } : {}),
        ...(exitCode !== undefined ? { exitCode, success: exitCode === 0 } : {}),
        ...(exitCode !== undefined && exitCode !== 0 ? { error: { code: exitCode, ...(errors.length ? { message: errors.join("\n") } : {}) } } : {}),
      });
      if (pending.length === 1 || ++anonymousCompletions === pending.length) {
        pending = [];
        anonymousCompletions = 0;
      }
      output = [];
      mode = "idle";
    };
    const consumeOutput = (line, text) => {
      const completed = text.match(/^\s*- Completed in (\d+(?:\.\d+)?)s\s*$/);
      if (completed && pending.length) finishTool(line, completed[1]);
      else if (text || output.length) output.push(text);
    };
    for (let index = banner + 1; index < lines.length; index++) {
      const line = lines[index];
      if (isAddMaskCommandLine(line)) continue;
      if (/^\s*▸ Credits:|^\[entrypoint\]|^\[kiro-harness\]|^\[INFO\] (?:Stopping containers|Executing agent command)|^Process exiting with code:/.test(line)) {
        flushAssistant();
        mode = "idle";
        continue;
      }
      if (/^\[(?:INFO|WARN|SUCCESS|health-check|info)\]|^\s*(?:Container|Network) \S|^[⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏] Loading/.test(line)) continue;
      if (mode !== "command" && /^(?:> )?(?:User|Human|System|Prompt):/i.test(line)) {
        flushAssistant();
        mode = "idle";
        continue;
      }
      if (mode !== "command" && isAssistantLine(line)) {
        flushAssistant();
        assistant = emit(index, "assistant.message", { content: line.slice(2) });
        mode = "assistant";
        continue;
      }
      if (mode !== "command" && /^(?:I will run the following command: |Searching for symbols matching: |Querying available agents for task delegation)/.test(line)) {
        flushAssistant();
        command = { line: index, text: line };
        mode = "command";
      } else if (mode === "command") command.text += "\n" + line;
      if (mode === "command") {
        const announcement = command.text.match(/^([\s\S]+) \(using tool: (shell|code|subagent)\)(.*)$/);
        if (!announcement) continue;
        const [, description, toolName, tail] = announcement;
        let input;
        if (toolName === "shell" && description.startsWith("I will run the following command: ")) input = { command: description.slice("I will run the following command: ".length) };
        else if (toolName === "code" && description.startsWith("Searching for symbols matching: ")) input = { query: description.slice("Searching for symbols matching: ".length) };
        else if (toolName !== "subagent" || description !== "Querying available agents for task delegation") {
          mode = "idle";
          continue;
        }
        pending.push(emit(command.line, "tool.execution_start", { toolName, ...(input ? { input } : {}) }));
        command = undefined;
        mode = "output";
        output = [];
        if (tail.trim()) consumeOutput(index, tail);
        continue;
      }
      if (mode === "output" || (mode === "idle" && pending.length)) {
        if (/^Purpose: /.test(line) && mode === "output") pending.at(-1).data.description = line.slice("Purpose: ".length);
        else consumeOutput(index, line);
      } else if (mode === "assistant" && assistant) assistant.data.content += "\n" + line;
    }
    flushAssistant();
  }
  const logEntries = normalizeAgentSession(entries, { sourceEngine: "kiro" });
  return {
    markdown: logEntries.length ? generateCopilotCliStyleSummary(logEntries) : buildStepSummaryDetailsSection("Kiro", "Supported Kiro headless conversation signatures not found. Raw content is omitted."),
    logEntries,
    mcpFailures: [],
    maxTurnsHit: false,
  };
}

module.exports = { main, parseKiroLog };
