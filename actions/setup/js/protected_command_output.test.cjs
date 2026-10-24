import { afterEach, beforeEach, describe, expect, it } from "vitest";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const { readProtectedCommandOutput, writeVerifiedExitCode, discardUntrustedSessionArtifacts } = require("./protected_command_output.cjs");
const { serializeSessionArtifact } = require("./session_artifact.cjs");
const { normalizeAgentSession } = require("./agent_session.cjs");
const root = path.dirname(fileURLToPath(import.meta.url));

describe("native protected command output", () => {
  let directory, metadata;
  const save = () => fs.writeFileSync(path.join(directory, "metadata.json"), JSON.stringify(metadata), { mode: 0o600 });
  beforeEach(() => {
    directory = fs.mkdtempSync(path.join(root, "test-native-capture-"));
    fs.writeFileSync(path.join(directory, "attempt-001.stdout.bin"), "forged stdout", { mode: 0o600 });
    fs.writeFileSync(path.join(directory, "attempt-001.stderr.bin"), "recorder stderr", { mode: 0o600 });
    metadata = {
      schemaVersion: 1,
      status: "complete",
      teardown: "complete",
      truncated: false,
      maxBytes: 67108864,
      command: { outcome: "exited", exitCode: 17 },
      capturedBytes: 28,
      observedBytes: 28,
      attempts: [
        {
          outcome: "exited",
          exitCode: 17,
          logsDrained: true,
          stdout: { file: "attempt-001.stdout.bin", capturedBytes: 13, observedBytes: 13 },
          stderr: { file: "attempt-001.stderr.bin", capturedBytes: 15, observedBytes: 15 },
        },
      ],
    };
    save();
  });
  afterEach(() => fs.rmSync(directory, { recursive: true, force: true }));

  it("selects stderr without converting a failed command to success", () => {
    expect(readProtectedCommandOutput(directory)).toEqual({ content: "recorder stderr", exitCode: 17, maskedValues: [] });
  });

  it("does not concatenate attempts or use unstarted precreated files", () => {
    metadata.attempts.push({
      outcome: "not_started",
      stdout: { file: "attempt-002.stdout.bin", capturedBytes: 0, observedBytes: 0 },
      stderr: { file: "attempt-002.stderr.bin", capturedBytes: 0, observedBytes: 0 },
    });
    save();
    expect(readProtectedCommandOutput(directory).content).toBe("recorder stderr");
    metadata.attempts[1].outcome = "exited";
    metadata.attempts[1].exitCode = 17;
    metadata.attempts[1].logsDrained = true;
    metadata.attempts[1].stderr.capturedBytes = metadata.attempts[1].stderr.observedBytes = 5;
    metadata.capturedBytes = metadata.observedBytes = 33;
    fs.writeFileSync(path.join(directory, "attempt-002.stdout.bin"), "", { mode: 0o600 });
    fs.writeFileSync(path.join(directory, "attempt-002.stderr.bin"), "final", { mode: 0o600 });
    save();
    expect(readProtectedCommandOutput(directory).content).toBe("final");
  });

  it.each([
    ["schemaVersion", 2],
    ["status", "incomplete"],
    ["teardown", "failed"],
    ["truncated", true],
    ["captureFailure", "write failed"],
    ["maxBytes", 67108865],
    ["capturedBytes", 0],
    ["observedBytes", 0],
  ])("rejects invalid %s metadata", (field, value) => {
    metadata[field] = value;
    save();
    expect(() => readProtectedCommandOutput(directory)).toThrow("Invalid AWF protected command output");
  });

  it.each(["timed_out", "cancelled", "interrupted", "failed", "running"])("rejects command outcome %s", outcome => {
    metadata.command.outcome = outcome;
    save();
    expect(() => readProtectedCommandOutput(directory)).toThrow();
  });

  it("rejects undrained attempts, traversal and missing or mismatched stderr", () => {
    metadata.attempts[0].logsDrained = false;
    save();
    expect(() => readProtectedCommandOutput(directory)).toThrow();
    metadata.attempts[0].logsDrained = true;
    metadata.attempts[0].stderr.file = "../outside";
    save();
    expect(() => readProtectedCommandOutput(directory)).toThrow();
    metadata.attempts[0].stderr.file = "attempt-001.stderr.bin";
    save();
    fs.writeFileSync(path.join(directory, "attempt-001.stderr.bin"), "short");
    expect(() => readProtectedCommandOutput(directory)).toThrow();
    fs.unlinkSync(path.join(directory, "attempt-001.stderr.bin"));
    expect(() => readProtectedCommandOutput(directory)).toThrow();
  });

  it("rejects inconsistent command outcomes and non-stderr file sizes", () => {
    metadata.attempts[0].exitCode = 0;
    save();
    expect(() => readProtectedCommandOutput(directory)).toThrow();
    metadata.attempts[0].exitCode = 17;
    save();
    fs.writeFileSync(path.join(directory, "attempt-001.stdout.bin"), "short");
    expect(() => readProtectedCommandOutput(directory)).toThrow();
  });

  it("rejects symlinks, hardlinks and public capture files", () => {
    const file = path.join(directory, "attempt-001.stderr.bin");
    const replacement = path.join(directory, "replacement");
    fs.renameSync(file, replacement);
    fs.symlinkSync(replacement, file);
    expect(() => readProtectedCommandOutput(directory)).toThrow();
    fs.unlinkSync(file);
    fs.linkSync(replacement, file);
    expect(() => readProtectedCommandOutput(directory)).toThrow();
    fs.unlinkSync(replacement);
    fs.chmodSync(file, 0o644);
    expect(() => readProtectedCommandOutput(directory)).toThrow();
  });

  it("rejects malformed metadata without exposing its contents", () => {
    fs.writeFileSync(path.join(directory, "metadata.json"), "private malformed metadata");
    expect(() => readProtectedCommandOutput(directory)).toThrow("malformed metadata");
    expect(() => readProtectedCommandOutput(directory)).not.toThrow("private malformed metadata");
  });

  it("unlinks shared symlinks without touching their targets", () => {
    const target = path.join(directory, "private");
    fs.writeFileSync(target, "private data");
    fs.symlinkSync(target, path.join(directory, "agent-stdio.log"));
    discardUntrustedSessionArtifacts(directory);
    expect(fs.readFileSync(target, "utf8")).toBe("private data");
    expect(fs.existsSync(path.join(directory, "agent-stdio.log"))).toBe(false);
  });

  it("removes error-session files and their temporary sibling", () => {
    fs.writeFileSync(path.join(directory, "agent-errors.jsonl"), '{"type":"session.result"}');
    fs.writeFileSync(path.join(directory, "agent-errors.jsonl.tmp"), '{"type":"tool.execution_complete"}');
    discardUntrustedSessionArtifacts(directory);
    expect(fs.existsSync(path.join(directory, "agent-errors.jsonl"))).toBe(false);
    expect(fs.existsSync(path.join(directory, "agent-errors.jsonl.tmp"))).toBe(false);
  });

  it("atomically replaces a sidecar symlink with the verified exit code", () => {
    const target = path.join(directory, "outside");
    const sidecar = path.join(directory, "agent_execution_exit_code.txt");
    fs.writeFileSync(target, "untrusted target", { mode: 0o600 });
    fs.symlinkSync(target, sidecar);
    writeVerifiedExitCode(directory, 17);
    expect(fs.readFileSync(sidecar, "utf8")).toBe("17\n");
    expect(fs.readFileSync(target, "utf8")).toBe("untrusted target");
  });

  it("replaces a read-only sidecar with the verified exit code", () => {
    const sidecar = path.join(directory, "agent_execution_exit_code.txt");
    fs.writeFileSync(sidecar, "0", { mode: 0o400 });
    writeVerifiedExitCode(directory, 17);
    expect(fs.readFileSync(sidecar, "utf8")).toBe("17\n");
  });
});

describe("protected session structural redaction", () => {
  it("redacts free-text tool payloads without modifying structural fields", () => {
    const events = [
      { type: "assistant.message", data: { content: "secret" } },
      { type: "tool.execution_complete", data: { toolName: "read_file", success: false, result: { status: "secret", secret: { "nested-secret": "value" } } } },
      { type: "session.result", data: { status: "failure", usage: { input_tokens: 10 } } },
    ];
    const serialized = serializeSessionArtifact(events, ["secret"], { rejectStructuralRedaction: true });
    expect(serialized).not.toContain("secret");
    expect(serialized).toContain('"status":"failure"');
    expect(serialized).toContain('"success":false');
  });

  it.each(["failure", "session.result", "read_file", "data", "input_tokens"])("rejects a structural collision with %s", mask => {
    const events = [
      { type: "tool.execution_start", data: { toolName: "read_file", input: "content" } },
      { type: "session.result", data: { status: "failure", usage: { input_tokens: 10 } } },
    ];
    expect(() => serializeSessionArtifact(events, [mask], { rejectStructuralRedaction: true })).toThrow("structural");
  });

  it("does not exempt payload-like keys at unknown schema paths", () => {
    const events = [{ type: "session.result", data: { status: "failure", usage: { result: "secret" } } }];
    expect(() => serializeSessionArtifact(events, ["secret"], { rejectStructuralRedaction: true })).toThrow("structural");
  });

  it("accepts normalized events whose optional properties are omitted by JSON serialization", () => {
    const events = normalizeAgentSession([{ type: "system", subtype: "init", model: "test-model" }], { sourceEngine: "custom" });
    expect(events[0].data.cwd).toBeUndefined();
    expect(() => serializeSessionArtifact(events, [], { rejectStructuralRedaction: true })).not.toThrow();
  });

  it("rejects configured-secret collisions with structural fields", () => {
    const previousNames = process.env.GH_AW_SECRET_NAMES;
    const previousSecret = process.env.SECRET_STRUCTURE;
    process.env.GH_AW_SECRET_NAMES = "STRUCTURE";
    process.env.SECRET_STRUCTURE = "session.result";
    try {
      const events = [{ type: "session.result", data: { status: "failure" } }];
      expect(() => serializeSessionArtifact(events, [], { rejectStructuralRedaction: true })).toThrow("structural");
    } finally {
      if (previousNames === undefined) delete process.env.GH_AW_SECRET_NAMES;
      else process.env.GH_AW_SECRET_NAMES = previousNames;
      if (previousSecret === undefined) delete process.env.SECRET_STRUCTURE;
      else process.env.SECRET_STRUCTURE = previousSecret;
    }
  });
});
