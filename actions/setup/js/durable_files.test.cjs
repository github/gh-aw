// @ts-check
"use strict";

import { afterEach, describe, expect, it, vi } from "vitest";
const fs = require("node:fs");
const path = require("node:path");
const { createHash } = require("node:crypto");
const { stageFiles, restoreFiles, validateFiles } = require("./durable_files.cjs");
const { processFile } = require("./redact_secrets.cjs");
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

describe("engine-declared durable files", () => {
  it("round trips only declared session files and tells the harness to continue", () => {
    const options = fixture();
    const binary = Buffer.from(Array.from({ length: 256 }, (_, index) => index));
    fs.writeFileSync(path.join(options.sessions, "session.db"), binary);
    stageFiles(options);
    const manifest = JSON.parse(fs.readFileSync(path.join(options.bundle, "manifest.json"), "utf8"));
    expect(manifest.entries.map(entry => entry.path).sort()).toEqual(["0/session.db", "0/session.jsonl"]);
    validateFiles(options);
    fs.rmSync(options.sessions, { recursive: true });
    restoreFiles(options);
    expect(fs.readFileSync(path.join(options.sessions, "session.db"))).toEqual(binary);
    expect(fs.readFileSync(path.join(options.sessions, "session.jsonl"), "utf8")).toContain("Continue this task");
    expect(options.core.setOutput).toHaveBeenCalledWith("restored", "true");
  });

  it("supports a single native checkpoint file through the same artifact contract", () => {
    const options = fixture();
    const checkpoint = path.join(options.root, "durable/pi-durable-example.json");
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
    const record = JSON.parse(fs.readFileSync(file, "utf8"));
    record.text = "***";
    fs.writeFileSync(file, JSON.stringify(record));
    expect(() => validateFiles(options)).toThrow("damaged or redacted");
    options.core.setOutput.mockClear();
    expect(() => restoreFiles(options)).toThrow("damaged or redacted");
    expect(options.core.setOutput).not.toHaveBeenCalled();
  });

  it("exposes every byte of binary state to host redaction and refuses the redacted bundle", () => {
    const options = fixture();
    const secret = "caf\u00e9-runtime-credential";
    const binary = Buffer.concat([Buffer.from([0xff, 0]), Buffer.from(secret, "latin1"), Buffer.from([0, 0xfe])]);
    fs.writeFileSync(path.join(options.sessions, "session.db"), binary);
    stageFiles(options);
    const file = path.join(options.bundle, createHash("sha256").update("0/session.db").digest("hex") + ".json");
    const record = JSON.parse(fs.readFileSync(file, "utf8"));
    expect(record.text).not.toContain(secret);
    expect(record.byteText).toContain(secret);
    expect(Buffer.from(record.byteText, "latin1")).toEqual(binary);
    expect(processFile(file, [], [secret])).toBeGreaterThan(0);
    options.core.setOutput.mockClear();
    expect(() => validateFiles(options)).toThrow("damaged or redacted");
    fs.rmSync(options.sessions, { recursive: true });
    expect(() => restoreFiles(options)).toThrow("damaged or redacted");
    expect(fs.existsSync(options.sessions)).toBe(false);
    expect(options.core.setOutput).not.toHaveBeenCalled();
  });

  it.each(["session.jsonl", "session.db"])("requires the lossless projection for publication while restoring legacy %s", name => {
    const options = fixture();
    const original = name === "session.db" ? Buffer.from([0, 1, 255, 0, 2]) : fs.readFileSync(path.join(options.sessions, name));
    fs.writeFileSync(path.join(options.sessions, name), original);
    stageFiles(options);
    const file = path.join(options.bundle, createHash("sha256").update(`0/${name}`).digest("hex") + ".json");
    const record = JSON.parse(fs.readFileSync(file, "utf8"));
    delete record.byteText;
    fs.writeFileSync(file, JSON.stringify(record));
    options.core.setOutput.mockClear();
    expect(() => validateFiles(options)).toThrow("damaged or redacted");
    expect(options.core.setOutput).not.toHaveBeenCalled();
    fs.rmSync(options.sessions, { recursive: true });
    restoreFiles(options);
    expect(fs.readFileSync(path.join(options.sessions, name))).toEqual(original);
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
    expect(() => validateFiles(options)).toThrow("damaged or redacted");
    expect(options.core.setOutput).not.toHaveBeenCalled();
  });

  it("does not claim persistence when the engine did not create its session files", () => {
    const options = fixture();
    fs.rmSync(options.sessions, { recursive: true });
    stageFiles(options);
    expect(options.core.setOutput).not.toHaveBeenCalled();
  });
});
