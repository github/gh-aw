// @ts-check
"use strict";

const fs = require("fs");
const { closed, identity, canonical, parseStrictJSON } = require("./work_queue_codec.cjs");
const { normalizeClaimScope } = require("./work_queue_claim_scope.cjs");
const { MAX_INTENT_BYTES, MAX_INTENTS } = require("./work_queue_intents.cjs");

function readClaimControlMessages(assignment, filename = "/tmp/gh-aw/work-queue.intents.jsonl") {
  const messages = [];
  const errors = [];
  if (!fs.existsSync(filename)) return { messages, errors };
  const stat = fs.lstatSync(filename);
  if (!stat.isFile() || stat.size > MAX_INTENT_BYTES) throw new Error("Queue control delivery transport is not a bounded regular file");
  const bytes = fs.readFileSync(filename);
  const lines = [];
  for (let start = 0; start < bytes.length;) {
    const end = bytes.indexOf(10, start);
    const line = bytes.subarray(start, end < 0 ? bytes.length : end);
    if (line.length && !/^\s*$/.test(line.toString("ascii"))) lines.push(line);
    start = end < 0 ? bytes.length : end + 1;
  }
  if (lines.length > MAX_INTENTS) throw new Error("Queue control delivery intent count exceeded");
  const seen = new Map();
  for (const bytes of lines) {
    let intent;
    let normalized;
    try {
      intent = parseStrictJSON(new TextDecoder("utf-8", { fatal: true }).decode(bytes));
      closed(intent, ["version", "intent_id", "kind", "parameters"], ["claim_handle"], "queue control delivery intent");
      if (intent.version !== 3 || !["submit", "dispatch_next"].includes(intent.kind)) throw new Error("Queue control delivery requires a closed version-3 control intent");
      identity(intent.intent_id, "intent ID");
      normalized = normalizeClaimScope(
        {
          type: intent.kind === "submit" ? "work_queue_submit" : "work_queue_dispatch_next",
          intent_id: intent.intent_id,
          parameters: intent.parameters,
          ...(Object.hasOwn(intent, "claim_handle") ? { claim_handle: intent.claim_handle } : {}),
        },
        assignment
      );
      const previous = seen.get(intent.intent_id);
      if (previous) {
        if (canonical(previous) !== canonical(normalized)) {
          errors.push({ claim_handle: previous.claim_handle, type: previous.type, error: "Conflicting queue control intent ID" });
          throw new Error("Conflicting queue control intent ID");
        }
        continue;
      }
      seen.set(intent.intent_id, normalized);
      messages.push(normalized);
    } catch (error) {
      errors.push({ claim_handle: normalized?.claim_handle || intent?.claim_handle, type: normalized?.type || "work_queue_control", error: error.message });
    }
  }
  return { messages, errors };
}

module.exports = { readClaimControlMessages };
