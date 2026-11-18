// @ts-check
"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { iterateJsonlEvents } = require("./jsonl_helpers.cjs");

const MAX_READ_BYTES = 64 * 1024;
const MAX_LINE_BYTES = 8192;

/**
 * Consume AWF audit JSONL, never raw proxy logs. An origin's HTTP 403,
 * arbitrary tool text, and unknown decisions are not firewall denial evidence.
 * @param {unknown} entry
 * @returns {"warning" | "denied" | undefined}
 */
function classifyFirewallSteeringRecord(entry) {
  if (!entry || typeof entry !== "object" || Array.isArray(entry)) return;
  if ("_schema" in entry && (typeof entry._schema !== "string" || !entry._schema.startsWith("audit/"))) return;
  if ("event" in entry && entry.event !== "http_access") return;
  if (!("status" in entry) || typeof entry.status !== "number" || !Number.isInteger(entry.status) || entry.status < 0 || entry.status > 599) return;
  if ([200, 206, 304].includes(entry.status) || !("decision" in entry) || typeof entry.decision !== "string") return;
  if (!("host" in entry) || typeof entry.host !== "string" || !entry.host || ["-", "-:-"].includes(entry.host)) return;
  if (/^(?:awmg-mcpg|awmg-cli-proxy)(?::\d+)?$/i.test(entry.host)) return;
  const decision = entry.decision.toUpperCase();
  if (/^(?:TCP_DENIED(?::|$)|DENIED$)/.test(decision)) return "denied";
  if (!/^NONE_NONE(?::|$)/.test(decision)) return;
  if ([403, 407].includes(entry.status)) return "denied";
  if (entry.status === 0) return "warning";
  return undefined;
}

/**
 * Tail only the compiler-managed, read-only firewall mount. Never inspect agent
 * output or follow log symlinks. Existing bytes are a baseline, not fresh evidence.
 * @param {{env?: NodeJS.ProcessEnv, log: (message: string) => void}} options
 * @returns {() => {warning: number, denied: number}}
 */
function createFirewallLogReader({ env = process.env, log }) {
  const base = env.GH_AW_TMP_DIR || (env.GH_AW_AGENT_OUTPUT ? path.dirname(env.GH_AW_AGENT_OUTPUT) : "/tmp/gh-aw");
  const firewall = path.join(base, "sandbox", "firewall");
  const files = ["audit", "logs"].flatMap(directory => ["log.jsonl", "audit.jsonl"].map(filename => path.join(firewall, directory, filename)));
  /** @type {Map<string, {identity: string, offset: number, partial: string, skipLine: boolean}>} */
  const states = new Map();
  const diagnostics = new Set();
  let disabled = false;
  /** @param {string} reason */
  const diagnose = reason => {
    if (diagnostics.has(reason)) return;
    diagnostics.add(reason);
    log(`firewall-steering: evidence ${reason}; raw log content omitted`);
  };
  /** @param {unknown} error */
  const fail = error => {
    if (error && typeof error === "object" && "code" in error && error.code === "ENOENT") return;
    disabled = true;
    diagnose("unavailable_or_unsafe");
  };
  /** @param {string} file @returns {number} */
  const open = file => {
    for (const directory of [base, path.join(base, "sandbox"), firewall, path.dirname(file)]) {
      const stat = fs.lstatSync(directory);
      if (!stat.isDirectory() || stat.isSymbolicLink()) throw new Error("Unsafe firewall log directory");
    }
    return fs.openSync(file, fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW | fs.constants.O_NONBLOCK);
  };
  for (const file of files) {
    let fd;
    try {
      fd = open(file);
      const stat = fs.fstatSync(fd);
      if (!stat.isFile()) throw new Error("Firewall log is not a regular file");
      const last = Buffer.alloc(1);
      if (stat.size) fs.readSync(fd, last, 0, 1, stat.size - 1);
      states.set(file, { identity: `${stat.dev}:${stat.ino}`, offset: stat.size, partial: "", skipLine: stat.size > 0 && last[0] !== 10 });
    } catch (error) {
      fail(error);
    } finally {
      if (fd !== undefined) fs.closeSync(fd);
    }
    if (disabled) break;
  }

  return () => {
    const counts = { warning: 0, denied: 0 };
    if (disabled) return counts;
    const identities = new Set();
    for (const file of files) {
      let fd;
      try {
        fd = open(file);
        const stat = fs.fstatSync(fd);
        if (!stat.isFile()) throw new Error("Firewall log is not a regular file");
        const identity = `${stat.dev}:${stat.ino}`;
        if (identities.has(identity)) continue;
        identities.add(identity);
        let state = states.get(file);
        if (!state || state.identity !== identity || stat.size < state.offset) {
          state = { identity, offset: 0, partial: "", skipLine: false };
          states.set(file, state);
        }
        if (stat.size - state.offset > MAX_READ_BYTES) {
          state.offset = stat.size - MAX_READ_BYTES;
          state.partial = "";
          state.skipLine = true;
          diagnose("tail_truncated");
        }
        const size = Math.min(MAX_READ_BYTES, stat.size - state.offset);
        if (!size) continue;
        const buffer = Buffer.alloc(size);
        const bytes = fs.readSync(fd, buffer, 0, size, state.offset);
        state.offset += bytes;
        const lines = (state.partial + buffer.toString("utf8", 0, bytes)).split("\n");
        state.partial = lines.pop() || "";
        /** @type {string[]} */
        const complete = [];
        for (const line of lines) {
          if (state.skipLine) {
            state.skipLine = false;
            continue;
          }
          if (Buffer.byteLength(line) > MAX_LINE_BYTES) {
            diagnose("oversized_record_ignored");
            continue;
          }
          complete.push(line);
        }
        for (const record of iterateJsonlEvents(complete)) {
          if (record.type !== "json") {
            if (record.reason !== "blank") diagnose("invalid_record_ignored");
            continue;
          }
          const severity = classifyFirewallSteeringRecord(record.value);
          if (severity) counts[severity]++;
        }
        if (Buffer.byteLength(state.partial) > MAX_LINE_BYTES) {
          state.partial = "";
          state.skipLine = true;
          diagnose("oversized_record_ignored");
        }
      } catch (error) {
        fail(error);
      } finally {
        if (fd !== undefined) fs.closeSync(fd);
      }
      if (disabled) return { warning: 0, denied: 0 };
    }
    return counts;
  };
}

module.exports = { createFirewallLogReader, classifyFirewallSteeringRecord };
