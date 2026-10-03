import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { sessionCLI } from "./session_cli.cjs";

describe("Session CLI adapter", () => {
  let root;
  beforeEach(() => {
    root = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-session-cli-"));
  });
  afterEach(() => {
    vi.restoreAllMocks();
    fs.rmSync(root, { recursive: true, force: true });
  });

  function write(name, events) {
    const file = path.join(root, name);
    fs.mkdirSync(path.dirname(file), { recursive: true });
    fs.writeFileSync(file, typeof events === "string" ? events : events.map(event => JSON.stringify(event)).join("\n") + "\n");
    return file;
  }

  it("reconstructs compact unified JSONL with source provenance and no invented timestamps", () => {
    write("agent-session.jsonl", [{ type: "assistant.message", id: "native-id", data: { content: "  exact text\n" } }]);
    const jsonl = sessionCLI(["reconstruct", root]);
    const lines = jsonl.trimEnd().split("\n");
    const events = lines.map(JSON.parse);
    expect(events[0]).toMatchObject({ type: "session.format", data: { version: 1 } });
    expect(events.find(event => event.type === "assistant.message")).toEqual({
      type: "assistant.message",
      id: "native-id",
      data: { content: "  exact text\n" },
      provenance: { component: "agent", phase: "agent", path: "agent-session.jsonl", index: 0 },
    });
    expect(lines.every(line => JSON.stringify(JSON.parse(line)) === line)).toBe(true);
    expect(jsonl.endsWith("\n")).toBe(true);
  });

  it("renders unified Markdown using the privacy-preserving shared summary", () => {
    write("agent-session.jsonl", [
      { type: "user.message", data: { content: "private user prompt" } },
      { type: "assistant.message", data: { content: "Visible assistant response" } },
      { type: "tool.execution_start", data: { toolName: "bash", arguments: { command: "private shell command" } } },
    ]);
    const file = write("usage/aw_session.jsonl", sessionCLI(["reconstruct", root]));
    const markdown = sessionCLI(["markdown", file]);
    expect(markdown).toContain("### Unified session");
    expect(markdown).toContain("Visible assistant response");
    expect(markdown).toContain("tool.execution_start");
    expect(markdown).not.toContain("private user prompt");
    expect(markdown).not.toContain("private shell command");
  });

  it("uses the engine parser for audit Markdown", () => {
    const file = write("agent-stdio.log", '{"type":"item.completed","item":{"type":"agent_message","text":"Codex response"}}\n');
    expect(sessionCLI(["agent-markdown", file, "codex"])).toContain("Codex response");
  });

  it("rejects unrecognized agent logs rather than returning a metadata-only session", () => {
    write("agent-stdio.log", "unrecognized log text\n");
    vi.spyOn(console, "error").mockImplementation(() => {});
    expect(() => sessionCLI(["reconstruct", root, "claude"])).toThrow("No recognizable agent session");
  });

  it("rejects invalid session files and unsupported versions", () => {
    expect(() => sessionCLI(["markdown", write("invalid.jsonl", "not JSON")])).toThrow();
    expect(() => sessionCLI(["markdown", write("invalid-event.jsonl", [{ type: "assistant.message", data: [] }])])).toThrow("Invalid session event");
    expect(() => sessionCLI(["markdown", write("missing-header.jsonl", [{ type: "assistant.message", data: {} }])])).toThrow("missing its leading");
    expect(() => sessionCLI(["markdown", write("future.jsonl", [{ type: "session.format", data: { version: 2 }, provenance: { component: "collector" } }])])).toThrow("Unsupported unified session file-format version");
  });

  it("reports missing input, engine, and invalid modes", () => {
    expect(() => sessionCLI([])).toThrow("input path is required");
    expect(() => sessionCLI(["agent-markdown", root])).toThrow("engine is required");
    expect(() => sessionCLI(["invalid", root])).toThrow("Unsupported session CLI mode");
  });
});
