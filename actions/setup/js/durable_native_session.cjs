// @ts-check
"use strict";

const fs = require("node:fs");
const path = require("node:path");
const os = require("node:os");
const { randomUUID } = require("node:crypto");
const SESSION_ID = /^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/i;

function readCopilotCaptureBaseline(directory) {
  const file = path.join(directory, "capture-offset.json");
  if (!fs.existsSync(file)) return undefined;
  const baseline = JSON.parse(fs.readFileSync(file, "utf8"));
  if (!SESSION_ID.test(baseline.id || "") || !Number.isSafeInteger(baseline.bytes) || baseline.bytes < 0) throw new Error("Invalid durable Copilot capture baseline");
  return baseline;
}

function openDurableSession(engine, env = process.env) {
  if (env.GH_AW_DURABLE_STORAGE !== "artifact") return undefined;
  const configuredDirectory = env.GH_AW_DURABLE_SESSION_DIR;
  if (env.GH_AW_DURABLE_ENGINE !== engine || !configuredDirectory || !path.isAbsolute(configuredDirectory)) throw new Error("Missing engine-owned durable session configuration");
  const directory = configuredDirectory;
  const file = path.join(directory, "session.json");
  fs.mkdirSync(directory, { recursive: true, mode: 0o700 });
  if (fs.lstatSync(file, { throwIfNoEntry: false })?.isSymbolicLink()) throw new Error("Durable session selector must not be a symlink");
  let id = "";
  if (fs.existsSync(file)) {
    const state = JSON.parse(fs.readFileSync(file, "utf8"));
    if (state.version !== 1 || state.engine !== engine || !SESSION_ID.test(state.id || "")) throw new Error("Invalid durable session selector");
    id = state.id;
  } else if (env.GH_AW_DURABLE_RESUMED === "true") {
    throw new Error("Restored durable state has no exact session ID; refusing to start an unrelated conversation");
  }
  function save(sessionId) {
    if (typeof sessionId !== "string" || !SESSION_ID.test(sessionId)) throw new Error("Durable continuation requires an exact session UUID");
    if (id && id !== sessionId) throw new Error("Durable continuation cannot switch to an unrelated session");
    const temporary = path.join(directory, `session-${randomUUID()}.tmp`);
    fs.writeFileSync(temporary, JSON.stringify({ version: 1, engine, id: sessionId }) + "\n", { mode: 0o600, flag: "wx" });
    fs.renameSync(temporary, file);
    id = sessionId;
  }
  function copilotSession(sessionId = id || randomUUID()) {
    if (!SESSION_ID.test(sessionId)) throw new Error("Invalid durable Copilot session UUID");
    const baseline = readCopilotCaptureBaseline(directory);
    const target = path.join(env.COPILOT_HOME || path.join(env.HOME || os.homedir(), ".copilot"), "session-state", sessionId);
    if (!baseline) {
      if (fs.existsSync(target)) throw new Error("Refusing to overwrite an unrelated native Copilot session");
      if (id) {
        const source = path.join(directory, "session-state", sessionId);
        if (!fs.existsSync(path.join(source, "events.jsonl"))) throw new Error("Durable Copilot session transcript is missing");
        fs.mkdirSync(path.dirname(target), { recursive: true, mode: 0o700 });
        fs.cpSync(source, target, { recursive: true, dereference: false });
      }
      const events = path.join(target, "events.jsonl");
      fs.writeFileSync(path.join(directory, "capture-offset.json"), JSON.stringify({ id: sessionId, bytes: fs.existsSync(events) ? fs.statSync(events).size : 0 }), { mode: 0o600, flag: "wx" });
    } else if (baseline.id !== sessionId) {
      throw new Error("Durable Copilot continuation cannot select a different session");
    }
    return sessionId;
  }
  return {
    get id() {
      return id;
    },
    save,
    copilotSession,
  };
}

function observeDurableSession(session, engine, line) {
  if (!session) return;
  let event;
  try {
    event = JSON.parse(line);
  } catch {
    return; // Harness diagnostics are interleaved with structured CLI events.
  }
  if (!event || typeof event !== "object") return;
  const id = engine === "codex" && event.type === "thread.started" ? event.thread_id : engine === "claude" && event.type === "system" && event.subtype === "init" ? event.session_id : undefined;
  if (id !== undefined) session.save(id);
}

module.exports = { openDurableSession, observeDurableSession, readCopilotCaptureBaseline };
