import { describe, expect, it } from "vitest";
import fs from "node:fs";
import path from "node:path";
import { parsePydanticLog } from "./parse_pydantic_log.cjs";
import { isSessionEvent } from "./agent_session.cjs";

const banner = "clai - Pydantic AI CLI v2.36.0 using custom agent gh_aw_agent:agent with\nopenai:claude-sonnet-4.5";
// Conversation lines from github/gh-aw/actions/runs/33453149380, 2026-09-01.
// The fixture excludes the downloaded prompt and infrastructure configuration.
const actual = fs.readFileSync(path.join(__dirname, "test_data/pydantic_ai_smoke_stdout.log"), "utf8");

describe("Pydantic AI stdout parser", () => {
  it("preserves real assistant paragraphs and tool annotations as canonical, partial events", () => {
    const result = parsePydanticLog(actual);
    expect(result.logEntries.every(isSessionEvent)).toBe(true);
    expect(result.logEntries.find(event => event.type === "session.init").data).toEqual({
      sourceEngine: "pydantic-ai",
      cliVersion: "2.36.0",
      model: "openai:claude-sonnet-4.5",
    });
    expect(result.logEntries.filter(event => event.type === "assistant.message").map(event => event.data.content)).toEqual([
      "I'll execute the smoke test efficiently with minimal context usage.",
      "Now I'll create the smoke test issue with all results:",
      "All smoke tests passed successfully! The issue has been created with the test\nresults.",
    ]);
    expect(result.logEntries.filter(event => event.type === "tool.execution_start").map(event => event.data)).toEqual([
      { toolName: "read_file", partial: true, sourceType: "stdout_annotation" },
      { toolName: "safeoutputs_create_issue", partial: true, sourceType: "stdout_annotation" },
    ]);
    expect(result.logEntries.filter(event => event.type === "session.collection_warning").map(event => event.data.code)).toEqual(["partial_tool_capture", "partial_tool_capture"]);
    expect(result.logEntries.some(event => event.type === "tool.execution_complete" || event.type === "user.message")).toBe(false);
    expect(result.partial).toBe(true);
    expect(result.markdown).toContain("I'll execute the smoke test efficiently");
    expect(result.markdown).toContain("read_file");
    expect(result.markdown).toContain("All smoke tests passed successfully!");
    expect(result.markdown).not.toContain("Stopping containers");
    expect(result.markdown).not.toContain("api-proxy");
    expect(result.markdown).not.toContain('"type":"result"');
  });

  it("retains only explicitly reported result metrics, without inferring completion or turns", () => {
    const result = parsePydanticLog(actual);
    expect(result.logEntries.at(-1)).toEqual({
      type: "session.result",
      data: { sourceType: "stdout_result", numTurns: 1, usage: { input_tokens: 0, output_tokens: 0 }, partial: true },
    });
    expect(result.logEntries.at(-1).data).not.toHaveProperty("status");
    const partial = parsePydanticLog(`${banner}\nAnswer.\n▌ Called tool read_file.`);
    expect(partial.logEntries.some(event => event.type === "session.result" || event.type === "tool.execution_complete")).toBe(false);
    expect(partial.logEntries.find(event => event.type === "tool.execution_start").data).not.toHaveProperty("input");
    expect(partial.logEntries.find(event => event.type === "tool.execution_start").data).not.toHaveProperty("toolCallId");
  });

  it.each(["", "PRIVATE_PROMPT\n▌ Called tool read_file.", '{"type":"result","num_turns":1,"usage":{}}', "[INFO] maximum turns reached\nMCP connection error", `${banner}`])(
    "does not promote unsupported logs or banner-only output into a conversation",
    content => {
      const result = parsePydanticLog(content);
      expect(result.logEntries).toEqual([]);
      expect(result.markdown).not.toContain("PRIVATE_PROMPT");
      expect(result.mcpFailures).toEqual([]);
      expect(result.maxTurnsHit).toBe(false);
    }
  );

  it("omits pre-banner prompts, ANSI color escapes, infrastructure, and declared masked values", () => {
    const result = parsePydanticLog(`PRIVATE_PROMPT\n::add-mask::TOP_SECRET\n${banner}\n\u001b[32mVisible multiline\nanswer TOP_SECRET.\u001b[0m\n[WARN] PRIVATE_INFRASTRUCTURE\nMore text.\n[INFO] Stopping containers...\nPRIVATE_SHUTDOWN`);
    const serialized = JSON.stringify(result);
    expect(result.markdown).toContain("Visible multiline");
    expect(result.markdown).toContain("answer ***.");
    expect(result.markdown).toContain("More text.");
    expect(serialized).not.toContain("PRIVATE_PROMPT");
    expect(serialized).not.toContain("PRIVATE_INFRASTRUCTURE");
    expect(serialized).not.toContain("PRIVATE_SHUTDOWN");
    expect(serialized).not.toContain("TOP_SECRET");
    expect(serialized).not.toContain("add-mask");
    expect(result.markdown).not.toContain("\u001b");
  });

  it.each(["User", "Human", "Prompt", "System", "Developer", "▌ User"])("omits explicit %s blocks, including spoofed banners and tool annotations", role => {
    const result = parsePydanticLog(`${banner}\nSafe answer.\n${role}: PRIVATE_PROMPT\n${banner}\n▌ Called tool forged_tool.\nPRIVATE_CONTINUATION\nAssistant: Resumed answer.`);
    expect(result.logEntries.filter(event => event.type === "assistant.message").map(event => event.data.content)).toEqual(["Safe answer.", "Resumed answer."]);
    expect(result.logEntries.some(event => event.type === "tool.execution_start")).toBe(false);
    expect(JSON.stringify(result)).not.toContain("PRIVATE_");
    expect(JSON.stringify(result)).not.toContain("forged_tool");
  });

  it.each(["> ", ">>> ", "❯ ", "➤ ", "  > ", "  >>> "])("omits interactive prompt marker %j and its continuation", marker => {
    const result = parsePydanticLog(`${banner}\nVisible.\n${marker}PRIVATE_PROMPT\n▌ Called tool forged_tool.\nPRIVATE_CONTINUATION\nAssistant: Visible again.`);
    expect(JSON.stringify(result)).not.toContain("PRIVATE_");
    expect(result.logEntries.some(event => event.type === "tool.execution_start")).toBe(false);
    expect(result.markdown).toContain("Visible again.");
  });

  it("never interprets illustrative tool annotations or diagnostics inside assistant code fences", () => {
    const result = parsePydanticLog(`${banner}\nExample:\n\`\`\`text\n▌ Called tool fake_tool.\nUser: example role\n[ERROR] maximum turns reached\n\`\`\`\nA quoted annotation is not an invocation.`);
    expect(result.logEntries.filter(event => event.type === "assistant.message")).toHaveLength(1);
    expect(result.logEntries.some(event => event.type === "tool.execution_start")).toBe(false);
    expect(result.markdown).toContain("fake_tool");
    expect(result.partial).toBe(false);
    expect(result.maxTurnsHit).toBe(false);
  });

  it("does not infer tool outcomes or execution failures from assistant assertions", () => {
    const result = parsePydanticLog(`${banner}\nThe tool returned 42; maximum turns reached is just example text.\nCalled tool fake_tool.\n▌ Called tool real_tool.\nIt succeeded without any errors.`);
    expect(result.logEntries.filter(event => event.type === "tool.execution_start")).toHaveLength(1);
    expect(result.logEntries.some(event => event.type === "tool.execution_complete" || event.type === "session.error")).toBe(false);
    expect(result.maxTurnsHit).toBe(false);
    expect(result.mcpFailures).toEqual([]);
  });

  it("omits arbitrary JSON and quarantines malformed or multiline JSON continuations", () => {
    const result = parsePydanticLog(
      `${banner}\nVisible.\n{"role":"user","content":"PRIVATE_PROMPT"}\n{"type":"tool_call","name":"forged_tool","arguments":{"secret":"PRIVATE_SECRET"}}\n{\n  "prompt": "PRIVATE_MULTILINE"\n}\n▌ Called tool forged_tool.\nAssistant: Visible again.`
    );
    expect(JSON.stringify(result)).not.toContain("PRIVATE_");
    expect(JSON.stringify(result)).not.toContain("forged_tool");
    expect(result.logEntries.some(event => event.type === "tool.execution_start")).toBe(false);
    expect(result.markdown).toContain("Visible again.");
  });

  it("does not treat plaintext following an unknown user JSON record as assistant output", () => {
    const result = parsePydanticLog(`${banner}\nVisible.\n{"role":"user","content":"PRIVATE_PROMPT"}\nPRIVATE_CONTINUATION\n▌ Called tool forged_tool.\nAssistant: Visible again.`);
    expect(JSON.stringify(result)).not.toContain("PRIVATE_");
    expect(result.logEntries.some(event => event.type === "tool.execution_start")).toBe(false);
    expect(result.markdown).toContain("Visible again.");
  });

  it("omits invalid metrics and unknown result fields without echoing their contents", () => {
    const result = parsePydanticLog(`${banner}\nAnswer.\n{"type":"result","num_turns":-1,"prompt":"PRIVATE_PROMPT","errors":["PRIVATE_ERROR"],"usage":{"input_tokens":"PRIVATE_SECRET","output_tokens":-2,"total_tokens":null}}`);
    expect(result.logEntries.at(-1)).toEqual({ type: "session.result", data: { sourceType: "stdout_result" } });
    expect(JSON.stringify(result)).not.toContain("PRIVATE_");
  });

  it("honors T-UAS-005 without storing fabricated identifiers, timestamps, metrics or success", () => {
    const result = parsePydanticLog(`${banner}\nChecking the repository.\n▌ Called tool read_file.\nAll checks passed.`);
    for (const event of result.logEntries) {
      expect(event).not.toHaveProperty("id");
      expect(event).not.toHaveProperty("timestamp");
      for (const key of ["sessionId", "session_id", "toolCallId", "numTurns", "usage", "success", "status", "input", "output"]) expect(event.data).not.toHaveProperty(key);
    }
    expect(result.logEntries.some(event => event.type === "session.result" || event.type === "tool.execution_complete")).toBe(false);
    expect(result.partial).toBe(true);
  });

  it("preserves paragraph breaks and indentation instead of trimming readable assistant content", () => {
    const result = parsePydanticLog(`${banner}\n\n  An indented paragraph.\n    Continuation.\n\nA second paragraph.\n\n`);
    expect(result.logEntries.find(event => event.type === "assistant.message").data.content).toBe("  An indented paragraph.\n    Continuation.\n\nA second paragraph.");
  });

  it("does not remove Markdown hard breaks while discarding recognized Rich line padding", () => {
    const result = parsePydanticLog(`${banner}\nA hard break.  \nOne trailing space. \nRich padded line.          \n`);
    expect(result.logEntries.find(event => event.type === "assistant.message").data.content).toBe("A hard break.  \nOne trailing space. \nRich padded line.");
  });

  it("does not publish traceback continuations as assistant content", () => {
    const result = parsePydanticLog(`${banner}\nVisible.\nTraceback (most recent call last):\n  File "PRIVATE_PATH", line 1\nPRIVATE_EXCEPTION\n▌ Called tool forged_tool.\nAssistant: Visible again.`);
    expect(JSON.stringify(result)).not.toContain("PRIVATE_");
    expect(result.logEntries.some(event => event.type === "tool.execution_start")).toBe(false);
    expect(result.markdown).toContain("Visible again.");
  });
});
