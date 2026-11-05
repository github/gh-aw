// @ts-check
"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { createHash } = require("node:crypto");
const { iterateJsonl } = require("./jsonl_helpers.cjs");

const SOURCE = "@earendil-works/pi-durable";
const VERSION = "1.4.0";
const FILE = /^(?:main|(?:doc|task)-(?:0|[1-9]\d*))\.jsonl$/;

/** Refuse secrets rather than rewriting Chord deltas and corrupting replay. */
function assertNoSecrets(value, env = process.env) {
  const text = JSON.stringify(value);
  for (const [name, secret] of Object.entries(env)) {
    if (/(?:TOKEN|KEY|SECRET|PASSWORD|CREDENTIAL)/i.test(name) && secret && secret.length >= 6 && text.includes(JSON.stringify(secret).slice(1, -1))) {
      throw new Error("Pi durable checkpoint contains a credential; refusing persistence");
    }
  }
  if (/\b(?:gh[pousr]_|ghs_|github_pat_)[A-Za-z0-9._-]{12,}|\bsk-(?:proj-|ant-api03-)?[A-Za-z0-9_-]{20,}|Bearer\s+[A-Za-z0-9._~+/-]{8,}/i.test(text)) {
    throw new Error("Pi durable checkpoint contains a credential; refusing persistence");
  }
}

function digest(text) {
  return createHash("sha256").update(text).digest("hex");
}

function regularFile(file) {
  if (!fs.lstatSync(file).isFile()) throw new Error("Pi durable checkpoint requires regular files, not symlinks");
}

/** The trusted Actions host restores/publishes this complete artifact envelope. */
function createCheckpoint(checkpoint, directory, scope, storage) {
  if (storage !== "artifact") throw new Error("Pi durable supports only artifact storage");
  let state = { requestId: "", submissionId: null, pending: false, calls: 0, denials: 0, usage: {}, trace: {} };
  if (fs.existsSync(checkpoint)) {
    regularFile(checkpoint);
    const saved = JSON.parse(fs.readFileSync(checkpoint, "utf8"));
    if (saved.source !== SOURCE || saved.packageVersion !== VERSION || saved.contractVersion !== 1 || saved.scope !== scope || saved.storage !== storage) {
      throw new Error("Incompatible Pi durable checkpoint source, version, workflow, or storage");
    }
    if (!saved.files || !Object.hasOwn(saved.files, "main.jsonl") || !saved.hashes || JSON.stringify(Object.keys(saved.files).sort()) !== JSON.stringify(Object.keys(saved.hashes).sort())) {
      throw new Error("Incomplete Pi durable JSONL checkpoint");
    }
    assertNoSecrets(saved);
    for (const [name, text] of Object.entries(saved.files)) {
      if (!FILE.test(name) || typeof text !== "string" || saved.hashes[name] !== digest(text) || (text && !text.endsWith("\n"))) throw new Error("Invalid Pi durable JSONL checkpoint file");
      for (const _record of iterateJsonl(text, {
        onError(error) {
          throw error;
        },
      })) {
        // Consume every record so malformed checkpoint journals fail closed.
      }
    }
    state = saved.state;
    if (!state || typeof state.pending !== "boolean" || typeof state.requestId !== "string" || !Number.isSafeInteger(state.calls) || state.calls < 0 || !Number.isSafeInteger(state.denials) || state.denials < 0) {
      throw new Error("Invalid Pi durable checkpoint state");
    }
    if (state.failure != null && !["policy", "unanswered"].includes(state.failure)) throw new Error("Invalid Pi durable checkpoint failure state");
    fs.mkdirSync(directory, { recursive: true, mode: 0o700 });
    for (const name of fs.readdirSync(directory)) {
      if (!FILE.test(name)) throw new Error("Unexpected file in Pi durable working directory");
      regularFile(path.join(directory, name));
      fs.unlinkSync(path.join(directory, name));
    }
    for (const [name, text] of Object.entries(saved.files)) fs.writeFileSync(path.join(directory, name), text, { mode: 0o600 });
  } else {
    fs.mkdirSync(directory, { recursive: true, mode: 0o700 });
    if (fs.readdirSync(directory).length) throw new Error("Pi durable working directory has no matching checkpoint");
  }

  function save() {
    const files = {};
    const hashes = {};
    for (const name of fs.readdirSync(directory).sort()) {
      if (!FILE.test(name)) throw new Error("Unexpected file in Pi durable JSONL storage");
      regularFile(path.join(directory, name));
      files[name] = fs.readFileSync(path.join(directory, name), "utf8");
      hashes[name] = digest(files[name]);
    }
    if (!Object.hasOwn(files, "main.jsonl")) return;
    const envelope = { source: SOURCE, packageVersion: VERSION, contractVersion: 1, scope, storage, state, hashes, files };
    assertNoSecrets(envelope);
    fs.mkdirSync(path.dirname(checkpoint), { recursive: true, mode: 0o700 });
    const staging = checkpoint + ".next";
    if (fs.existsSync(staging)) regularFile(staging);
    const fd = fs.openSync(staging, "w", 0o600);
    try {
      fs.writeFileSync(fd, JSON.stringify(envelope) + "\n");
      fs.fsyncSync(fd);
    } finally {
      fs.closeSync(fd);
    }
    fs.renameSync(staging, checkpoint);
  }

  /** Persist before acknowledging each official storage commit. */
  function wrap(storageBackend) {
    return new Proxy(storageBackend, {
      get(target, property) {
        if (property === "commit") {
          return async (writes, context) => {
            assertNoSecrets(writes);
            const seq = await target.commit(writes, context);
            save();
            return seq;
          };
        }
        const value = Reflect.get(target, property);
        return typeof value === "function" ? value.bind(target) : value;
      },
    });
  }
  return { state, save, wrap };
}

module.exports = { createCheckpoint, assertNoSecrets, SOURCE, VERSION };
