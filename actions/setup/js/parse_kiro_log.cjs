// @ts-check

const { stripVTControlCharacters } = require("node:util");
const { createSessionEvent, normalizeAgentSession } = require("./agent_session.cjs");
const { collectAddMaskedValues, redactMaskedValues, isAddMaskCommandLine } = require("./add_mask_redaction.cjs");
const { createEngineLogParser, generateCopilotCliStyleSummary, buildStepSummaryDetailsSection } = require("./log_parser_shared.cjs");

const main = createEngineLogParser({ parserName: "Kiro", parseFunction: parseKiroLog, supportsDirectories: false });

/**
 * Supported Kiro headless stdout uses either "> " assistant paragraphs and
 * timed tools, or 2.27's "[tool]" records and buffered unprefixed answers.
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
  const commandLines = new Set();
  const harnessFailure = findKiroHarnessFailure(lines);
  const executionStart = lines.findIndex(line => line === "[kiro-harness] Kiro CLI execution started");
  const compactStart = executionStart !== -1 ? executionStart : banner;
  const isLegacyObservation = line => isAssistantLine(line) || /^(?:I will run the following command: |Searching for symbols matching: |Querying available agents for task delegation|\s*- Completed in \d)/.test(line);
  const firstSignature = compactStart !== -1 ? lines.slice(compactStart + 1).find(line => /^\[tool\] /.test(line) || isLegacyObservation(line)) : undefined;
  const compact = compactStart !== -1 && (firstSignature?.startsWith("[tool] ") || (executionStart !== -1 && firstSignature === undefined));
  if (compact) entries.push(...parseCompactKiroLog(lines, compactStart, banner, commandLines, harnessFailure));
  else if (banner !== -1 && lines.slice(banner + 1).some(isLegacyObservation)) {
    const emit = (line, type, data) => {
      const event = createSessionEvent({ line: line + 1 }, type, data);
      entries.push(event);
      return event;
    };
    emit(banner, "session.init", { sourceEngine: "kiro", agentVersion: lines[banner].slice("kiro-cli ".length).trim() });
    let assistant;
    let command;
    let output = [];
    let outputLine;
    let pending = [];
    let anonymousCompletions = 0;
    let mode = "idle";
    const flushAssistant = () => {
      if (assistant) assistant.data.content = assistant.data.content.replace(/(?:\n[ \t]*)+$/, "");
      assistant = undefined;
    };
    const finishTool = (line, duration) => {
      const text = output.join("\n").replace(/^\n+|\n+$/g, "");
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
      if (pending.length <= 1 || ++anonymousCompletions === pending.length) {
        pending = [];
        anonymousCompletions = 0;
      }
      output = [];
      outputLine = undefined;
      mode = "idle";
    };
    const flushOutput = () => {
      if (output.length) {
        emit(outputLine, "tool.output", {
          ...(pending.length === 1 ? { toolName: pending[0].data.toolName } : {}),
          output: output.join("\n").replace(/^\n+|\n+$/g, ""),
          partial: true,
        });
      }
      output = [];
      outputLine = undefined;
    };
    const consumeOutput = (line, text) => {
      const completed = text.match(/^\s*- Completed in (\d+(?:\.\d+)?)s\s*$/);
      if (completed) finishTool(line, completed[1]);
      else if (text || output.length) {
        if (!output.length) outputLine = line;
        output.push(text);
      }
    };
    for (let index = banner + 1; index < lines.length; index++) {
      const line = lines[index];
      if (isAddMaskCommandLine(line)) continue;
      const harnessError = /^\[kiro-harness\] Kiro CLI execution failed with /.test(line);
      if (mode !== "command" && (index === harnessFailure || (!harnessError && /^\s*▸ Credits:|^\[entrypoint\]|^\[kiro-harness\]|^\[INFO\] (?:Stopping containers|Executing agent command)|^Process exiting with code:/.test(line)))) {
        flushAssistant();
        flushOutput();
        mode = entries.length > 1 ? "stopped" : "idle";
        continue;
      }
      if (mode === "stopped") continue;
      if (mode !== "command" && (/^\[info\] \[bridge\]/.test(line) || (mode === "idle" && /^\[(?:INFO|WARN|SUCCESS|health-check|info)\]|^\s*(?:Container|Network) \S|^[⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏] Loading/.test(line)))) continue;
      if (mode !== "command" && /^(?:> )?(?:User|Human|System|Prompt):/i.test(line)) {
        flushAssistant();
        mode = "idle";
        continue;
      }
      if (mode !== "command" && isAssistantLine(line)) {
        flushAssistant();
        flushOutput();
        assistant = emit(index, "assistant.message", { content: line.slice(2) });
        mode = "assistant";
        continue;
      }
      const completed = mode !== "command" && line.match(/^\s*- Completed in (\d+(?:\.\d+)?)s\s*$/);
      if (completed) {
        flushAssistant();
        finishTool(index, completed[1]);
        continue;
      }
      if (mode !== "command" && /^(?:I will run the following command: |Searching for symbols matching: |Querying available agents for task delegation)/.test(line)) {
        flushAssistant();
        flushOutput();
        command = { line: index, text: line };
        mode = "command";
      } else if (mode === "command") {
        commandLines.add(index);
        command.text += "\n" + line;
      }
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
    flushOutput();
  }
  if (harnessFailure !== undefined && !commandLines.has(harnessFailure)) {
    entries.push(
      createSessionEvent({ line: harnessFailure + 1 }, "session.result", {
        sourceEngine: "kiro",
        sourceType: "kiro-harness",
        status: "failed",
        errors: [lines[harnessFailure].slice("[kiro-harness] ".length)],
      })
    );
  }
  entries.sort((left, right) => Number(left.line) - Number(right.line));
  const logEntries = normalizeAgentSession(entries, { sourceEngine: "kiro" });
  return {
    markdown: logEntries.length ? generateCopilotCliStyleSummary(logEntries) : buildStepSummaryDetailsSection("Kiro", "Supported Kiro headless conversation signatures not found. Raw content is omitted."),
    logEntries,
    mcpFailures: [],
    maxTurnsHit: false,
  };
}

/**
 * Mixed stdout cannot authenticate a diagnostic prefix. Require the final
 * adjacent harness error/cleanup pair and a matching nonzero runner exit.
 * @param {string[]} lines
 * @returns {number | undefined}
 */
function findKiroHarnessFailure(lines) {
  let terminal = lines.length - 1;
  while (terminal >= 0 && (!lines[terminal].trim() || isAddMaskCommandLine(lines[terminal]))) terminal--;
  const runner = lines[terminal]?.match(/^Process exiting with code: ([1-9]\d*)$/);
  if (!runner || Number(runner[1]) > 255) return undefined;
  let cleanupIndex = terminal - 1;
  while (cleanupIndex >= 0 && !/^\[kiro-harness\] Cleaned up Kiro CLI installation;/.test(lines[cleanupIndex])) cleanupIndex--;
  if (cleanupIndex <= 0 || cleanupIndex >= terminal) return undefined;
  const cleanup = lines[cleanupIndex].match(/^\[kiro-harness\] Cleaned up Kiro CLI installation; total duration=\d+ms; exit code=([1-9]\d*)$/);
  if (!cleanup || Number(cleanup[1]) !== Number(runner[1])) return undefined;
  const failure = lines[cleanupIndex - 1].match(/^\[kiro-harness\] Kiro CLI execution failed with (?:exit code ([1-9]\d*)|signal (\S+))(?:;|$)/);
  if (!failure || (failure[1] !== undefined && Number(failure[1]) !== Number(runner[1]))) return undefined;
  if (failure[2] !== undefined && Number(runner[1]) !== 1) return undefined;
  const infrastructure = /^(?:\s*$|\[entrypoint\]|\[(?:INFO|WARN|SUCCESS|health-check|info)\]|\s*(?:Container|Network) \S)/;
  if (lines.slice(cleanupIndex + 1, terminal).some(line => !isAddMaskCommandLine(line) && !infrastructure.test(line))) return undefined;
  return cleanupIndex - 1;
}

/**
 * The compact CLI prints tool statuses without IDs and buffers assistant prose
 * until after tool activity. Displayed commands may already be truncated.
 * @param {string[]} lines
 * @param {number} start
 * @param {number} banner
 * @param {Set<number>} commandLines
 * @param {number | undefined} harnessFailure
 * @returns {import("./types/agent_session").SessionEvent[]}
 */
function parseCompactKiroLog(lines, start, banner, commandLines, harnessFailure) {
  /** @type {import("./types/agent_session").SessionEvent[]} */
  const entries = [];
  const emit = (line, type, data) => {
    const event = createSessionEvent({ line: line + 1 }, type, data);
    entries.push(event);
    return event;
  };
  if (banner !== -1) emit(banner, "session.init", { sourceEngine: "kiro", agentVersion: lines[banner].slice("kiro-cli ".length).trim() });
  let assistant;
  let command;
  let pending = [];
  let anonymousCompletions = 0;
  let active = start === lines.findIndex(line => line === "[kiro-harness] Kiro CLI execution started");
  const flush = () => {
    if (assistant) assistant.data.content = assistant.data.content.replace(/(?:\n[ \t]*)+$/, "");
    if (command) {
      command.data.input.command = command.data.input.command.replace(/\n+$/, "");
      if (Buffer.byteLength(command.data.input.command, "utf8") === 200 && command.data.input.command.endsWith("...")) command.data.inputTruncated = true;
    }
    assistant = undefined;
    command = undefined;
  };
  for (let index = start + 1; index < lines.length; index++) {
    const line = lines[index];
    if (isAddMaskCommandLine(line)) continue;
    const harnessError = /^\[kiro-harness\] Kiro CLI execution failed with /.test(line);
    if (!command && (index === harnessFailure || (!harnessError && /^\[kiro-harness\]|^\[entrypoint\]|^\[INFO\] Stopping containers|^Process exiting with code:/.test(line)))) {
      flush();
      active = false;
      continue;
    }
    if (!command && /^(?:> )?(?:User|Human|System|Prompt):/i.test(line)) {
      flush();
      active = false;
      continue;
    }
    const status = line.match(/^\[tool\] status: (Completed|Failed)$/);
    if (status) {
      flush();
      emit(index, "tool.execution_complete", {
        ...(pending.length === 1 ? { toolName: pending[0].data.toolName } : {}),
        status: status[1],
        ...(status[1] === "Failed" ? { success: false } : {}),
      });
      if (pending.length <= 1 || ++anonymousCompletions === pending.length) {
        pending = [];
        anonymousCompletions = 0;
      }
      active = true;
      continue;
    }
    if (line.startsWith("[tool] status: ")) {
      flush();
      emit(index, "kiro.tool_status", { status: line.slice("[tool] status: ".length) });
      active = true;
      continue;
    }
    if (line.startsWith("[tool] ")) {
      flush();
      const announcement = line.slice("[tool] ".length);
      const shell = announcement.startsWith("Running: ");
      const code = announcement.startsWith("Searching symbols: ");
      const aws = announcement.startsWith("AWS: ");
      if (!shell && !code && !aws) {
        pending.push(emit(index, "kiro.tool_announcement", { content: announcement }));
        active = true;
        continue;
      }
      const data = shell
        ? { toolName: "shell", input: { command: announcement.slice("Running: ".length) } }
        : code
          ? { toolName: "code", input: { query: announcement.slice("Searching symbols: ".length) } }
          : { toolName: "aws", input: { command: announcement.slice("AWS: ".length) } };
      const event = emit(index, "tool.execution_start", data);
      pending.push(event);
      if (shell || aws) command = event;
      active = true;
      continue;
    }
    if (!command && /^\[(?:INFO|WARN|SUCCESS|health-check|info)\]|^\s*(?:Container|Network) \S/.test(line)) continue;
    if (command) {
      commandLines.add(index);
      command.data.input.command += "\n" + line;
    } else if (assistant) assistant.data.content += "\n" + line;
    else if (active && line) assistant = emit(index, "assistant.message", { content: line });
  }
  flush();
  return entries.some(event => event.type !== "session.init") ? entries : [];
}

module.exports = { main, parseKiroLog };
