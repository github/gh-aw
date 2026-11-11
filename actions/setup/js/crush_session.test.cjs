import { describe, expect, it } from "vitest";
import { loadEngineLogParser, parseBehaviorLog } from "./engine_log_parser.cjs";
import { mergeSessionSources, parseEngineSession } from "./unified_session.cjs";
import { validateSession } from "./scripts/validate_session.cjs";

describe("Crush 0.98.1 quiet-mode sessions", () => {
  // Sanitized startup excerpt from https://github.com/github/gh-aw/actions/runs/42930955037.
  const nativePreamble = [
    "[crush-harness] starting Crush CLI",
    "INFO  Running in non-interactive mode",
    'WARN  Failed to parse skill file path=/fixture/SKILL.md error="no YAML frontmatter found"',
    "INFO  Overriding large model for non-interactive run provider=awf-proxy model=auto",
    "WARN  Using large model as small model for unknown provider provider=awf-proxy model=auto",
    "INFO  Created session for non-interactive run session_id=05482e8a-0c34-4d78-8c21-26e6d147607a",
    "INFO  ModelProvider called provider=awf-proxy model=auto",
  ].join("\n");

  it("preserves observed startup session/model metadata separately from assistant text", () => {
    const raw = `${nativePreamble}\nObserved answer.\n[crush-harness] Crush CLI finished after 19232ms (exit: 0; signal: none)\n`;
    const canonical = parseEngineSession(raw, "crush");
    const unified = mergeSessionSources([{ component: "agent", phase: "agent", path: "agent-session.jsonl", events: canonical }]);
    for (const events of [canonical, unified]) {
      expect(events.find(event => event.type === "session.init").data).toEqual({ sourceEngine: "crush", provider: "awf-proxy", model: "auto", sessionId: "05482e8a-0c34-4d78-8c21-26e6d147607a" });
      expect(events.find(event => event.type === "crush.diagnostic").data.content).toBe(nativePreamble.split("\n").slice(1).join("\n"));
      expect(events.filter(event => event.type === "assistant.message").map(event => event.data.content)).toEqual(["Observed answer.\n"]);
      expect(events.some(event => event.type.startsWith("tool.") || event.type === "session.result")).toBe(false);
    }
    expect(validateSession(JSON.stringify(canonical), "agent", "json")).toBe(canonical.length);
  });

  it("does not promote unframed or assistant-quoted startup metadata", () => {
    const unframed = nativePreamble.split("\n").slice(1).join("\n") + "\n";
    const quoted = "[crush-harness] starting Crush CLI\nObserved answer:\n" + nativePreamble + "\n";
    for (const raw of [unframed, quoted]) {
      const events = parseEngineSession(raw, "crush");
      expect(events).toEqual([
        {
          type: "assistant.message",
          data: {
            content: raw
              .split("\n")
              .filter(line => !line.startsWith("[crush-harness]"))
              .join("\n"),
          },
        },
      ]);
    }
  });

  it("preserves level-prefixed answer text after the recognized startup prefix", () => {
    const answer = "INFO  Explanation of the result.\nWARN  This is answer prose.\n";
    const events = parseEngineSession(`${nativePreamble}\n${answer}`, "crush");
    expect(events.find(event => event.type === "assistant.message").data.content).toBe(answer);
    expect(events.find(event => event.type === "crush.diagnostic").data.content).not.toContain("Explanation");
  });

  it("preserves observed output without interpreting prose as tool execution", () => {
    const text = "Running bash is only a plan.\n\nTool edit may be useful.\nExecuting ls would list files.\n";
    const events = parseEngineSession(`[crush-harness] resolved executable\n${text}`, "crush");
    expect(events).toEqual([{ type: "assistant.message", data: { content: text } }]);
    expect(events.some(event => event.type.startsWith("tool."))).toBe(false);
    expect(validateSession(JSON.stringify(events), "agent", "json")).toBe(1);
  });

  it("does not manufacture initialization, counters, usage, or results for empty output", () => {
    expect(parseEngineSession("[crush-harness] resolved executable\n[INFO] infrastructure\n", "crush")).toEqual([]);
    const parsed = parseBehaviorLog("Observed final answer.\n", "crush");
    expect(parsed.logEntries).toEqual([{ type: "assistant.message", data: { content: "Observed final answer.\n" } }]);
    expect(parsed.markdown).toContain("telemetry unavailable");
    expect(validateSession(JSON.stringify(parsed.logEntries), "agent", "json")).toBe(1);
  });

  it("preserves canonical observed fields including false and zero", () => {
    const events = [
      { type: "session.init", data: { model: "gpt-6.1-sol", sessionId: "observed-session", sourceEngine: "crush" } },
      { type: "tool.execution_start", data: { toolCallId: "observed-call", toolName: "bash", input: { command: "pwd" } } },
      { type: "tool.execution_complete", data: { toolCallId: "observed-call", output: "", success: false } },
      { type: "session.result", data: { numTurns: 0, usage: { output_tokens: 0 }, success: false } },
    ];
    expect(parseEngineSession(events.map(JSON.stringify).join("\n"), "crush")).toEqual(events);
  });

  it.each(["compact array", "pretty array", "JSONL"])("preserves canonical %s with or without a historical result", format => {
    const events = [
      { type: "session.init", data: { model: "gpt-6.1-sol", sessionId: "observed-session", sourceEngine: "crush" } },
      { type: "tool.execution_start", data: { toolCallId: "observed-call", toolName: "bash", input: { command: "pwd" } } },
      { type: "tool.execution_complete", data: { toolCallId: "observed-call", output: "", success: false } },
      { type: "session.result", data: { numTurns: 0, usage: { output_tokens: 0 }, success: false } },
    ];
    const text = format === "JSONL" ? events.map(JSON.stringify).join("\n") : JSON.stringify(events, null, format === "pretty array" ? 2 : undefined);
    expect(parseEngineSession(`[crush-harness] saved session\n${text}\n`, "crush")).toEqual(events);
    const terminal = { type: "result", num_turns: 0, usage: { output_tokens: 0 } };
    const parsed = parseEngineSession(`${text}\n${JSON.stringify(terminal)}\n`, "crush");
    expect(parsed.slice(0, events.length)).toEqual(events);
    expect(parsed).toHaveLength(events.length + 1);
    expect(parsed.at(-1)).toMatchObject({ type: "session.result", data: { numTurns: 0, usage: { output_tokens: 0 } } });
    expect(parsed.some(event => event.type === "assistant.message")).toBe(false);
  });

  it("preserves an empty canonical array without turning it into assistant prose", () => {
    expect(parseEngineSession("[]\n", "crush")).toEqual([]);
  });

  it("retains explicit historical terminal observations rather than inferred counters", () => {
    const events = parseEngineSession('Observed answer.\n{"type":"result","num_turns":0,"usage":{"output_tokens":0}}\n', "crush");
    expect(events.find(event => event.type === "assistant.message").data.content).toBe("Observed answer.");
    expect(events.find(event => event.type === "session.result").data).toMatchObject({ numTurns: 0, usage: { output_tokens: 0 } });
    expect(events.some(event => event.type === "session.init")).toBe(false);
  });

  it("does not promote canonical-looking JSON embedded in prose", () => {
    const text = 'Example output:\n{"type":"tool.execution_complete","data":{"success":true}}\nnot an observed call\n';
    expect(parseEngineSession(text, "crush")).toEqual([{ type: "assistant.message", data: { content: text } }]);
  });

  it("does not promote a mixed canonical and noncanonical array into session events", () => {
    const text = JSON.stringify([{ type: "tool.execution_complete", data: { success: true } }, { example: true }], null, 2);
    expect(parseEngineSession(text, "crush")).toEqual([{ type: "assistant.message", data: { content: text } }]);
  });

  it.each([
    "MCP server 'fixture' failed in the earlier incident, but is healthy now.\n",
    "MCP server 'fixture' failed\nMaximum turns reached\n",
    "MCP connection error is only hypothetical.\n",
    "Failed to connect to MCP is an example error message.\n",
    "The turn limit may be reached; an MCP connection error is only hypothetical.",
  ])("does not infer failure metadata from unframed answer text: %s", text => {
    const parse = loadEngineLogParser("crush");
    const parsed = parse(text);
    expect(parsed.mcpFailures).toEqual([]);
    expect(parsed.maxTurnsHit).toBe(false);
    expect(parsed.logEntries).toEqual([{ type: "assistant.message", data: { content: text } }]);
    expect(parse("").mcpFailures).toEqual([]);
  });
});
