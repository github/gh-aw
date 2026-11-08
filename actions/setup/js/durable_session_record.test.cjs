import { afterEach, beforeEach, expect, it } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { recordDurableSessionEvent } from "./durable_session_record.cjs";

let directory;
let options;
beforeEach(() => {
  directory = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-durable-record-"));
  options = { root: path.join(directory, "agent"), env: { GH_AW_DURABLE_ENGINE: "copilot", GITHUB_RUN_ID: "200", GITHUB_RUN_ATTEMPT: "1" } };
});
afterEach(() => fs.rmSync(directory, { recursive: true, force: true }));

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
