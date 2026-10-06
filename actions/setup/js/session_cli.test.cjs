import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { spawnSync } from "node:child_process";
import { sessionCLI, UnrecognizedSessionError } from "./session_cli.cjs";

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
      { type: "tool.execution_start", data: { toolName: "bash", arguments: { command: "visible shell command" } } },
    ]);
    const file = write("usage/aw_session.jsonl", sessionCLI(["reconstruct", root]));
    const markdown = sessionCLI(["markdown", file]);
    expect(markdown).toContain("### Unified session");
    expect(markdown).toContain("Visible assistant response");
    expect(markdown).toContain("tool.execution_start");
    expect(markdown).not.toContain("private user prompt");
    expect(markdown).toContain("$ visible shell command [pending]");
  });

  it("uses the engine parser for audit Markdown", () => {
    const file = write("agent-stdio.log", '{"type":"item.completed","item":{"type":"agent_message","text":"Codex response"}}\n');
    expect(sessionCLI(["agent-markdown", file, "codex"])).toContain("Codex response");
  });

  it("rejects unrecognized agent logs rather than returning a metadata-only session", () => {
    write("agent-stdio.log", "unrecognized log text\n");
    vi.spyOn(console, "error").mockImplementation(() => {});
    expect(() => sessionCLI(["reconstruct", root, "claude"])).toThrow("No recognizable agent session");
    expect(() => sessionCLI(["reconstruct", root, "claude"])).toThrow(UnrecognizedSessionError);
  });

  it("uses a distinct exit status for unsupported sessions without disguising genuine parser failures", () => {
    write("agent-stdio.log", "unrecognized log text\n");
    const executable = path.join(import.meta.dirname, "session_cli.cjs");
    const unsupported = spawnSync(process.execPath, [executable, "reconstruct", root, "claude"], { encoding: "utf8" });
    expect(unsupported.error).toBeUndefined();
    expect(unsupported.status).toBe(2);
    expect(unsupported.stdout).toBe("");
    expect(unsupported.stderr).toContain("No recognizable agent session");
    const invalid = spawnSync(process.execPath, [executable, "markdown", write("invalid.jsonl", "not JSON")], { encoding: "utf8" });
    expect(invalid.error).toBeUndefined();
    expect(invalid.status).toBe(1);
    expect(invalid.stderr).toContain("Invalid session JSONL");
  });

  it("rejects invalid session files and unsupported versions", () => {
    expect(() => sessionCLI(["markdown", write("invalid.jsonl", "not JSON")])).toThrow();
    expect(() => sessionCLI(["markdown", write("invalid-event.jsonl", [{ type: "assistant.message", data: [] }])])).toThrow("Invalid session event");
    expect(() => sessionCLI(["markdown", write("missing-header.jsonl", [{ type: "assistant.message", data: {} }])])).toThrow("missing its leading");
    expect(() => sessionCLI(["markdown", write("future.jsonl", [{ type: "session.format", data: { version: 2 }, provenance: { component: "collector" } }])])).toThrow("Unsupported unified session file-format version");
  });

  it("reconstructs an error-only historical startup failure", () => {
    write("agent-stdio.log", "Access denied by policy settings\n[copilot-harness] done: exitCode=1\n");
    const events = sessionCLI(["reconstruct", root, "copilot"]).trim().split("\n").map(JSON.parse);
    expect(events.filter(event => event.type === "agent.execution")).toMatchObject([{ data: { categories: ["inference_access_error"], errorCodes: [], errorTypes: [], exitCode: 1 } }]);
  });

  it("rejects duplicate or malformed execution entries when reading unified files", () => {
    const header = { type: "session.format", data: { version: 1 }, provenance: { component: "collector" } };
    const execution = { type: "agent.execution", data: { categories: [], errorCodes: [], errorTypes: [], exitCode: 0 } };
    expect(() => sessionCLI(["markdown", write("duplicate.jsonl", [header, execution, execution])])).toThrow("multiple agent.execution");
    expect(() => sessionCLI(["markdown", write("malformed.jsonl", [header, { ...execution, data: { ...execution.data, exitCode: "0" } }])])).toThrow("Invalid agent.execution exitCode");
  });

  it("reports missing input, engine, and invalid modes", () => {
    expect(() => sessionCLI([])).toThrow("input path is required");
    expect(() => sessionCLI(["agent-markdown", root])).toThrow("engine is required");
    expect(() => sessionCLI(["invalid", root])).toThrow("Unsupported session CLI mode");
  });
});
