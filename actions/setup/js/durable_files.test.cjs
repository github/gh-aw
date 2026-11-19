// @ts-check
"use strict";

import { afterEach, describe, expect, it, vi } from "vitest";
const fs = require("node:fs");
const path = require("node:path");
const { createHash } = require("node:crypto");
const { stageFiles, restoreFiles, validateFiles, redactCopilotCheckpoint } = require("./durable_files.cjs");
const { openDurableSession } = require("./durable_native_session.cjs");
const { processFile, redactArtifactContent } = require("./redact_secrets.cjs");
const directories = [];

afterEach(() => {
  vi.unstubAllEnvs();
  for (const directory of directories.splice(0)) fs.rmSync(directory, { recursive: true, force: true });
});

function fixture() {
  const root = fs.mkdtempSync(path.join(process.cwd(), ".durable-files-test-"));
  directories.push(root);
  const sessions = path.join(root, "example/sessions");
  fs.mkdirSync(sessions, { recursive: true });
  fs.writeFileSync(path.join(sessions, "session.jsonl"), '{"role":"user","content":"Continue this task"}\n');
  fs.writeFileSync(path.join(root, "example/runtime-config.json"), '{"not":"session-state"}');
  vi.stubEnv("GH_AW_DURABLE_FILES", JSON.stringify([sessions]));
  vi.stubEnv("GH_AW_DURABLE_ENGINE", "session-agent");
  const bundle = path.join(root, "durable/bundle");
  return { root, sessions, bundle, core: { info: vi.fn(), setOutput: vi.fn() } };
}

describe("Copilot durable checkpoint redaction", () => {
  const id = "6f9f1648-1234-4000-8000-000000000001";
  const secret = "fixture-value-only-known-to-host-redaction";

  function checkpoint() {
    const options = fixture();
    const selector = path.join(options.sessions, "session.json");
    const state = path.join(options.sessions, "session-state");
    const events = path.join(state, id, "events.jsonl");
    fs.mkdirSync(path.dirname(events), { recursive: true });
    const header = { type: "session.start", id: "event-1", parentId: null, data: { sessionId: id } };
    const message = { type: "user.message", id: "event-2", parentId: "event-1", data: { content: `Before API_KEY=${secret} after` } };
    fs.writeFileSync(selector, JSON.stringify({ version: 1, engine: "copilot", id }));
    fs.writeFileSync(events, `${JSON.stringify(header)}\n${JSON.stringify(message)}\n`);
    vi.stubEnv("GH_AW_DURABLE_FILES", JSON.stringify([selector, state]));
    vi.stubEnv("GH_AW_DURABLE_ENGINE", "copilot");
    stageFiles(options);
    const blob = path.join(options.bundle, createHash("sha256").update(`1/${id}/events.jsonl`).digest("hex") + ".json");
    return { ...options, header, message, events, blob };
  }

  it("restores the same native session after masking a credential in an event", () => {
    const options = checkpoint();
    vi.stubEnv("SECRET_FIXTURE_TOKEN", secret);
    vi.stubEnv("GH_AW_SECRET_NAMES", "API_KEY");
    redactCopilotCheckpoint(text => redactArtifactContent(text, [secret], []).content, options.root, [secret]);
    expect(processFile(options.blob, [secret])).toBe(0);
    validateFiles(options);
    fs.rmSync(options.sessions, { recursive: true });
    restoreFiles(options);
    const home = path.join(options.root, "copilot-home");
    const session = openDurableSession("copilot", {
      GH_AW_DURABLE_ENGINE: "copilot",
      GH_AW_DURABLE_STORAGE: "artifact",
      GH_AW_DURABLE_RESUMED: "true",
      GH_AW_DURABLE_SESSION_DIR: options.sessions,
      COPILOT_HOME: home,
    });
    expect(session.copilotSession()).toBe(id);
    const restored = fs.readFileSync(path.join(home, "session-state", id, "events.jsonl"), "utf8");
    expect(restored).not.toContain(secret);
    expect(
      restored
        .trimEnd()
        .split("\n")
        .map(line => JSON.parse(line))
    ).toEqual([options.header, { ...options.message, data: { content: "Before API_KEY=***REDACTED*** after" } }]);
  });

  it("refuses to accept changes made before the trusted redaction step", () => {
    const options = checkpoint();
    fs.appendFileSync(options.blob, "\n");
    const redact = vi.fn();
    expect(() => redactCopilotCheckpoint(redact, options.root, [secret])).toThrow("damaged or redacted");
    expect(redact).not.toHaveBeenCalled();
  });

  it("rejects non-UTF-8 transcripts even when the redactor makes no changes", () => {
    const options = checkpoint();
    fs.writeFileSync(options.events, Buffer.from([0xff, 0xfe]));
    stageFiles(options);
    expect(() => redactCopilotCheckpoint(text => text, options.root)).toThrow("Cannot safely redact");
  });

  it("refuses redaction that would change the native session identity", () => {
    const options = checkpoint();
    expect(() => redactCopilotCheckpoint(text => redactArtifactContent(text, [id], []).content, options.root, [id])).toThrow("Cannot safely redact");
    expect(fs.readFileSync(options.blob)).toEqual(fs.readFileSync(options.events));
  });

  it("allows recognized credentials in nested tool results without freezing arbitrary ID keys", () => {
    const options = checkpoint();
    const accessKey = `AKIA${"A".repeat(16)}`;
    options.message.data = { result: { content: [{ json: { accessKeyId: accessKey, id: "tool-result-id" } }] } };
    fs.writeFileSync(options.events, `${JSON.stringify(options.header)}\n${JSON.stringify(options.message)}\n`);
    stageFiles(options);

    redactCopilotCheckpoint(text => redactArtifactContent(text, [], []).content, options.root, [accessKey]);

    validateFiles(options);
    const records = fs
      .readFileSync(options.blob, "utf8")
      .trimEnd()
      .split("\n")
      .map(line => JSON.parse(line));
    expect(records[1].data.result.content[0].json).toEqual({ accessKeyId: "***REDACTED***", id: "tool-result-id" });
  });

  it("rejects arbitrary transcript rewrites even when event structure is unchanged", () => {
    const options = checkpoint();
    expect(() => redactCopilotCheckpoint(text => text.replace("Before", "Persisted prompt injection"), options.root, [secret])).toThrow("Cannot safely redact");
    expect(fs.readFileSync(options.blob)).toEqual(fs.readFileSync(options.events));
  });
});

describe("engine-declared durable files", () => {
  it("round trips only declared session files and tells the harness to continue", () => {
    const options = fixture();
    const binary = Buffer.from(Array.from({ length: 256 }, (_, index) => index));
    fs.writeFileSync(path.join(options.sessions, "session.db"), binary);
    stageFiles(options);
    const manifest = JSON.parse(fs.readFileSync(path.join(options.bundle, "manifest.json"), "utf8"));
    expect(manifest.version).toBe(2);
    expect(manifest.entries.map(entry => entry.path).sort()).toEqual(["0/session.db", "0/session.jsonl"]);
    for (const entry of manifest.entries) {
      expect(fs.readFileSync(path.join(options.bundle, createHash("sha256").update(entry.path).digest("hex") + ".json"))).toEqual(fs.readFileSync(path.join(options.sessions, path.basename(entry.path))));
    }
    validateFiles(options);
    fs.rmSync(options.sessions, { recursive: true });
    restoreFiles(options);
    expect(fs.readFileSync(path.join(options.sessions, "session.db"))).toEqual(binary);
    expect(fs.readFileSync(path.join(options.sessions, "session.jsonl"), "utf8")).toContain("Continue this task");
    expect(options.core.setOutput).toHaveBeenCalledWith("restored", "true");
  });

  it("supports a single native checkpoint file through the same artifact contract", () => {
    const options = fixture();
    const checkpoint = path.join(options.root, "durable/checkpoint-example.json");
    fs.mkdirSync(path.dirname(checkpoint));
    fs.writeFileSync(checkpoint, '{"native":"checkpoint"}');
    vi.stubEnv("GH_AW_DURABLE_FILES", JSON.stringify([checkpoint]));
    stageFiles(options);
    fs.unlinkSync(checkpoint);
    restoreFiles(options);
    expect(fs.readFileSync(checkpoint, "utf8")).toBe('{"native":"checkpoint"}');
  });

  it("preserves file and directory names that would collide with JSON wrapper suffixes", () => {
    const options = fixture();
    fs.writeFileSync(path.join(options.sessions, "state"), "saved state");
    fs.mkdirSync(path.join(options.sessions, "state.json"));
    fs.writeFileSync(path.join(options.sessions, "state.json/index"), "saved index");
    stageFiles(options);
    fs.rmSync(options.sessions, { recursive: true });
    restoreFiles(options);
    expect(fs.readFileSync(path.join(options.sessions, "state"), "utf8")).toBe("saved state");
    expect(fs.readFileSync(path.join(options.sessions, "state.json/index"), "utf8")).toBe("saved index");
  });

  it("rejects state changed by host secret redaction without restoring any files", () => {
    const options = fixture();
    stageFiles(options);
    const file = path.join(options.bundle, createHash("sha256").update("0/session.jsonl").digest("hex") + ".json");
    expect(processFile(file, ["Continue this task"])).toBeGreaterThan(0);
    expect(() => validateFiles(options)).toThrow("damaged or redacted");
    options.core.setOutput.mockClear();
    expect(() => restoreFiles(options)).toThrow("damaged or redacted");
    expect(options.core.setOutput).not.toHaveBeenCalled();
  });

  it.each(["modified", "missing", "extra"])("rejects %s raw files before restoring any state", change => {
    const options = fixture();
    stageFiles(options);
    const file = path.join(options.bundle, createHash("sha256").update("0/session.jsonl").digest("hex") + ".json");
    if (change === "modified") fs.appendFileSync(file, "\nchanged");
    else if (change === "missing") fs.unlinkSync(file);
    else fs.writeFileSync(path.join(options.bundle, "unexpected.json"), "extra state");
    fs.rmSync(options.sessions, { recursive: true });
    options.core.setOutput.mockClear();
    expect(() => validateFiles(options)).toThrow();
    expect(() => restoreFiles(options)).toThrow();
    expect(fs.existsSync(options.sessions)).toBe(false);
    expect(options.core.setOutput).not.toHaveBeenCalled();
  });

  it("keeps binary state unencoded and refuses the bundle after lossless host redaction", () => {
    const options = fixture();
    const secret = "caf\u00e9-runtime-credential";
    const binary = Buffer.concat([Buffer.from([0xff, 0]), Buffer.from(secret, "latin1"), Buffer.from([0, 0xfe])]);
    fs.writeFileSync(path.join(options.sessions, "session.db"), binary);
    stageFiles(options);
    const file = path.join(options.bundle, createHash("sha256").update("0/session.db").digest("hex") + ".json");
    expect(fs.readFileSync(file)).toEqual(binary);
    expect(processFile(file, [], [secret])).toBeGreaterThan(0);
    expect(fs.existsSync(file)).toBe(false);
    options.core.setOutput.mockClear();
    expect(() => validateFiles(options)).toThrow("missing declared session files");
    fs.rmSync(options.sessions, { recursive: true });
    expect(() => restoreFiles(options)).toThrow("missing declared session files");
    expect(fs.existsSync(options.sessions)).toBe(false);
    expect(options.core.setOutput).not.toHaveBeenCalled();
  });

  it("rejects legacy encoded bundles instead of interpreting them as raw session files", () => {
    const options = fixture();
    stageFiles(options);
    const file = path.join(options.bundle, "manifest.json");
    const manifest = JSON.parse(fs.readFileSync(file, "utf8"));
    manifest.version = 1;
    fs.writeFileSync(file, JSON.stringify(manifest));
    options.core.setOutput.mockClear();
    expect(() => validateFiles(options)).toThrow("incompatible");
    expect(options.core.setOutput).not.toHaveBeenCalled();
    fs.rmSync(options.sessions, { recursive: true });
    expect(() => restoreFiles(options)).toThrow("incompatible");
    expect(fs.existsSync(options.sessions)).toBe(false);
  });

  it("leaves unredacted binary bytes unchanged during host redaction", () => {
    const options = fixture();
    const binary = Buffer.from(Array.from({ length: 256 }, (_, index) => index));
    fs.writeFileSync(path.join(options.sessions, "session.db"), binary);
    stageFiles(options);
    const file = path.join(options.bundle, createHash("sha256").update("0/session.db").digest("hex") + ".json");
    expect(processFile(file, [])).toBe(0);
    expect(fs.readFileSync(file)).toEqual(binary);
    validateFiles(options);
  });

  it("rejects a checkpoint made for a different engine or different file declarations", () => {
    const options = fixture();
    stageFiles(options);
    vi.stubEnv("GH_AW_DURABLE_ENGINE", "other-agent");
    expect(() => restoreFiles(options)).toThrow("incompatible");
  });

  it("rejects path traversal even when a downloaded manifest declares it", () => {
    const options = fixture();
    stageFiles(options);
    const manifestFile = path.join(options.bundle, "manifest.json");
    const manifest = JSON.parse(fs.readFileSync(manifestFile, "utf8"));
    manifest.entries[0].path = "0/../../outside";
    fs.writeFileSync(manifestFile, JSON.stringify(manifest));
    expect(() => restoreFiles(options)).toThrow("Invalid durable artifact file path");
  });

  it("refuses symlinks in declared directories and dangling restore destinations", () => {
    const options = fixture();
    fs.symlinkSync(path.join(options.root, "example/runtime-config.json"), path.join(options.sessions, "linked"));
    expect(() => stageFiles(options)).toThrow("regular files");
    fs.unlinkSync(path.join(options.sessions, "linked"));
    stageFiles(options);
    fs.unlinkSync(path.join(options.sessions, "session.jsonl"));
    fs.symlinkSync(path.join(options.root, "outside"), path.join(options.sessions, "session.jsonl"));
    expect(() => restoreFiles(options)).toThrow("symlinks");
    expect(fs.existsSync(path.join(options.root, "outside"))).toBe(false);
  });

  it("refuses credentials rather than uploading native agent authentication", () => {
    const options = fixture();
    vi.stubEnv("EXAMPLE_API_KEY", "never-persist-this-credential");
    fs.writeFileSync(path.join(options.sessions, "session.jsonl"), '{"key":"never-persist-this-credential"}');
    expect(() => stageFiles(options)).toThrow("credential");
    expect(options.core.setOutput).not.toHaveBeenCalled();
  });

  it.each(["utf8", "latin1"])("refuses known %s credentials in binary state", encoding => {
    const options = fixture();
    const secret = "caf\u00e9-private-credential";
    vi.stubEnv("EXAMPLE_API_KEY", secret);
    fs.writeFileSync(path.join(options.sessions, "session.db"), Buffer.concat([Buffer.from([0xff, 0]), Buffer.from(secret, encoding), Buffer.from([0xfe])]));
    expect(() => stageFiles(options)).toThrow("credential");
    expect(options.core.setOutput).not.toHaveBeenCalled();
  });

  it("blocks publication when host redaction detects ASCII tokens in binary state", () => {
    const options = fixture();
    fs.writeFileSync(path.join(options.sessions, "session.db"), Buffer.concat([Buffer.from([0xff, 0]), Buffer.from("ghp_" + "a".repeat(36)), Buffer.from([0xfe])]));
    stageFiles(options);
    const file = path.join(options.bundle, createHash("sha256").update("0/session.db").digest("hex") + ".json");
    expect(processFile(file, [])).toBeGreaterThan(0);
    options.core.setOutput.mockClear();
    expect(() => validateFiles(options)).toThrow("missing declared session files");
    expect(options.core.setOutput).not.toHaveBeenCalled();
  });

  it("does not claim persistence when the engine did not create its session files", () => {
    const options = fixture();
    fs.rmSync(options.sessions, { recursive: true });
    stageFiles(options);
    expect(options.core.setOutput).not.toHaveBeenCalled();
  });
});
