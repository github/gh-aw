import { afterEach, beforeEach, expect, it, vi } from "vitest";
import fs from "node:fs";
import path from "node:path";
import { recordDurableSessionEvent, tryRecordDurableSessionEvent } from "./durable_session_record.cjs";

let directory;
let options;
beforeEach(() => {
  directory = fs.mkdtempSync(path.resolve(".gh-aw-durable-record-"));
  options = { root: path.join(directory, "agent"), env: { GH_AW_DURABLE_ENGINE: "copilot", GITHUB_RUN_ID: "200", GITHUB_RUN_ATTEMPT: "1" } };
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  fs.rmSync(directory, { recursive: true, force: true });
});

it("appends only validated metadata, not transcript or credentials", () => {
  recordDurableSessionEvent("durable.restore", { status: "restored", parentRunId: "100", artifactId: "99", transcript: "private history" }, options);
  recordDurableSessionEvent("durable.session", { status: "resumed", parentRunId: "100", sessionId: "native-session", token: "private token" }, options);
  const file = path.join(options.root, "durable-session.jsonl");
  const content = fs.readFileSync(file, "utf8");
  const records = content.trim().split("\n").map(JSON.parse);
  expect(records).toHaveLength(2);
  expect(records[1].data).toEqual({ engine: "copilot", storage: "artifact", runId: "200", runAttempt: "1", status: "resumed", parentRunId: "100", sessionId: "native-session" });
  expect(content).not.toContain("private");
  expect(fs.statSync(file).mode & 0o777).toBe(0o600);
});

it("rejects invalid metadata without creating a log", () => {
  expect(() => recordDurableSessionEvent("durable.restore", { status: "restored" }, options)).toThrow("Invalid durable session metadata");
  expect(() => recordDurableSessionEvent("durable.session", { status: "resumed", sessionId: "../../secret" }, options)).toThrow("Invalid durable session metadata");
  expect(fs.existsSync(options.root)).toBe(false);
});

it.each([120, 200, 499])("preserves a full Pi session ID with a %i-character workflow name", length => {
  options.env.GH_AW_DURABLE_ENGINE = "pi";
  const sessionId = `pi-durable-${"a".repeat(length)}:1`;
  recordDurableSessionEvent("durable.session", { status: "fresh", sessionId }, options);
  expect(JSON.parse(fs.readFileSync(path.join(options.root, "durable-session.jsonl"), "utf8")).data.sessionId).toBe(sessionId);
});

it.each(["pi-durable-a:b:1", "pi-durable-a:../secret", "pi-durable-a:1.2", `pi-durable-${"a".repeat(500)}:1`, "native-id"])("rejects malformed or oversized Pi identity %s", sessionId => {
  options.env.GH_AW_DURABLE_ENGINE = "pi";
  expect(() => recordDurableSessionEvent("durable.session", { status: "fresh", sessionId }, options)).toThrow("Invalid durable session metadata");
  expect(fs.existsSync(options.root)).toBe(false);
});

it("retains the non-Pi ID bound", () => {
  expect(() => recordDurableSessionEvent("durable.session", { status: "fresh", sessionId: "a".repeat(129) }, options)).toThrow("Invalid durable session metadata");
});

it("warns safely when metadata cannot be validated or written", () => {
  const core = { warning: vi.fn() };
  expect(tryRecordDurableSessionEvent("durable.restore", { status: "restored" }, { ...options, core })).toBe(false);
  fs.mkdirSync(options.root);
  fs.mkdirSync(path.join(options.root, "durable-session.jsonl"));
  expect(tryRecordDurableSessionEvent("durable.session", { status: "fresh", sessionId: "native-session" }, { ...options, core })).toBe(false);
  expect(core.warning).toHaveBeenCalledTimes(2);
  expect(core.warning.mock.calls).toEqual([["Durable session metadata could not be recorded; continuation evidence is unavailable."], ["Durable session metadata could not be recorded; continuation evidence is unavailable."]]);
});

it("records without Actions core and reports standalone recording failures", () => {
  vi.stubGlobal("core", undefined);
  const warning = vi.spyOn(console, "error").mockImplementation(() => {});
  expect(tryRecordDurableSessionEvent("durable.session", { status: "fresh", sessionId: "native-session" }, options)).toBe(true);
  expect(warning).not.toHaveBeenCalled();
  expect(tryRecordDurableSessionEvent("durable.restore", { status: "restored" }, options)).toBe(false);
  expect(warning).toHaveBeenCalledExactlyOnceWith("Durable session metadata could not be recorded; continuation evidence is unavailable.");
});

it("refuses symlink log directories and files without changing their targets", () => {
  const target = path.join(directory, "target");
  fs.mkdirSync(target);
  fs.symlinkSync(target, options.root);
  expect(() => recordDurableSessionEvent("durable.restore", { status: "fresh" }, options)).toThrow("must not be a symlink");
  fs.unlinkSync(options.root);
  fs.mkdirSync(options.root);
  const targetFile = path.join(target, "unchanged");
  fs.writeFileSync(targetFile, "unchanged");
  fs.symlinkSync(targetFile, path.join(options.root, "durable-session.jsonl"));
  expect(() => recordDurableSessionEvent("durable.restore", { status: "fresh" }, options)).toThrow();
  expect(fs.readFileSync(targetFile, "utf8")).toBe("unchanged");
});
