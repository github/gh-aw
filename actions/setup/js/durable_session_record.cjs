// @ts-check
"use strict";

const fs = require("node:fs");
const path = require("node:path");

/** Metadata only: never persist restored transcript content or credentials here. */
function recordDurableSessionEvent(type, data, { env = process.env, root = path.join(env.GH_AW_TMP_DIR || "/tmp/gh-aw", "agent") } = {}) {
  if (!["durable.restore", "durable.session"].includes(type)) throw new Error("Invalid durable session event type");
  const record = {
    type,
    timestamp: new Date().toISOString(),
    data: {
      engine: env.GH_AW_DURABLE_ENGINE,
      storage: "artifact",
      runId: env.GITHUB_RUN_ID || "local",
      runAttempt: env.GITHUB_RUN_ATTEMPT || "1",
      status: data.status,
      ...(data.parentRunId ? { parentRunId: String(data.parentRunId) } : {}),
      ...(data.artifactId ? { artifactId: String(data.artifactId) } : {}),
      ...(data.sessionId ? { sessionId: data.sessionId } : {}),
    },
  };
  if (!isDurableSessionRecord(record)) throw new Error("Invalid durable session metadata");
  fs.mkdirSync(root, { recursive: true, mode: 0o700 });
  if (fs.lstatSync(root).isSymbolicLink()) throw new Error("Durable session log directory must not be a symlink");
  const file = path.join(root, "durable-session.jsonl");
  const descriptor = fs.openSync(file, fs.constants.O_WRONLY | fs.constants.O_CREAT | fs.constants.O_APPEND | fs.constants.O_NOFOLLOW, 0o600);
  try {
    fs.writeSync(descriptor, JSON.stringify(record) + "\n");
  } finally {
    fs.closeSync(descriptor);
  }
}

function isDurableSessionRecord(record) {
  const data = record?.data;
  return (
    ["durable.restore", "durable.session"].includes(record?.type) &&
    data &&
    typeof data.engine === "string" &&
    /^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$/.test(data.engine) &&
    data.storage === "artifact" &&
    typeof data.runId === "string" &&
    /^(?:[1-9]\d*|local)$/.test(data.runId) &&
    typeof data.runAttempt === "string" &&
    /^[1-9]\d*$/.test(data.runAttempt) &&
    (record.type === "durable.restore" ? ["restored", "fresh", "failed"] : ["resumed", "fresh"]).includes(data.status) &&
    ["parentRunId", "artifactId"].every(key => data[key] === undefined || (typeof data[key] === "string" && /^[1-9]\d*$/.test(data[key]))) &&
    (record.type !== "durable.session" ||
      (typeof data.sessionId === "string" &&
        (data.engine === "pi"
          ? /^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/i.test(data.sessionId) || (data.sessionId.length <= 512 && /^pi-durable-[A-Za-z0-9_-]+:[A-Za-z0-9][A-Za-z0-9_-]*$/.test(data.sessionId))
          : /^[A-Za-z0-9][A-Za-z0-9:_-]{0,127}$/.test(data.sessionId)))) &&
    (data.status !== "restored" || (data.parentRunId !== undefined && data.artifactId !== undefined))
  );
}

/**
 * Observability failures must not change the engine's continuation outcome.
 * @param {string} type
 * @param {any} data
 * @param {{ core?: { warning: (message: string) => void }, env?: NodeJS.ProcessEnv, root?: string }} [options]
 */
function tryRecordDurableSessionEvent(type, data, { core = global.core, ...options } = {}) {
  try {
    recordDurableSessionEvent(type, data, options);
    return true;
  } catch {
    const message = "Durable session metadata could not be recorded; continuation evidence is unavailable.";
    if (core?.warning) core.warning(message);
    else console.error(message);
    return false;
  }
}

module.exports = { recordDurableSessionEvent, tryRecordDurableSessionEvent, isDurableSessionRecord };
