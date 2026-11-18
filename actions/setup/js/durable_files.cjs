// @ts-check
"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { createHash } = require("node:crypto");
const { isDeepStrictEqual } = require("node:util");
const { iterateJsonlEvents } = require("./jsonl_helpers.cjs");
const { maskedValueMatches, overlappingMatchGroups } = require("./mask_value_matcher.cjs");

/** Refuse credentials rather than redacting and corrupting restorable session state. */
function assertNoSecrets(value, env = process.env) {
  const text = JSON.stringify(value);
  for (const [name, secret] of Object.entries(env)) {
    if (name !== "GH_AW_SECRET_NAMES" && /(?:TOKEN|KEY|SECRET|PASSWORD|CREDENTIAL)/i.test(name) && secret && secret.length >= 6 && text.includes(JSON.stringify(secret).slice(1, -1))) {
      throw new Error("Durable session contains a credential; refusing persistence");
    }
  }
  if (/\b(?:gh[pousr]_|ghs_|github_pat_)[A-Za-z0-9._-]{12,}|\bsk-(?:proj-|ant-api03-)?[A-Za-z0-9_-]{20,}|Bearer\s+[A-Za-z0-9._~+/-]{8,}/i.test(text)) {
    throw new Error("Durable session contains a credential; refusing persistence");
  }
}

function configuration(root) {
  const files = JSON.parse(process.env.GH_AW_DURABLE_FILES || "[]");
  const bundle = path.join(root, "durable/bundle");
  if (!Array.isArray(files) || !files.length || !process.env.GH_AW_DURABLE_ENGINE) throw new Error("Durable execution requires engine-declared files");
  for (const [index, file] of files.entries()) {
    if (
      typeof file !== "string" ||
      !file.startsWith(root + path.sep) ||
      path.normalize(file) !== file ||
      file === path.dirname(bundle) ||
      file === bundle ||
      file.startsWith(bundle + path.sep) ||
      files.slice(0, index).some(other => file === other || file.startsWith(other + path.sep) || other.startsWith(file + path.sep))
    ) {
      throw new Error("Unsafe or overlapping durable file declaration");
    }
  }
  return { files, bundle, engine: process.env.GH_AW_DURABLE_ENGINE };
}

function noSymlinks(file, root) {
  for (let current = file; current.startsWith(root); current = path.dirname(current)) {
    if (fs.lstatSync(current, { throwIfNoEntry: false })?.isSymbolicLink()) throw new Error("Durable files must not contain symlinks");
    if (current === root) break;
  }
}

function walk(directory, callback, relative = "") {
  for (const name of fs.readdirSync(directory)) {
    const file = path.join(directory, name);
    const key = relative ? `${relative}/${name}` : name;
    const stat = fs.lstatSync(file);
    if (stat.isDirectory()) walk(file, callback, key);
    else if (stat.isFile()) callback(file, key);
    else throw new Error("Durable state must contain only regular files and directories");
  }
}

function hash(bytes) {
  return createHash("sha256").update(bytes).digest("hex");
}

/** Snapshot exactly the engine's declared session state before host redaction. */
function stageFiles({ core, root = "/tmp/gh-aw/agent" }) {
  const { files, bundle, engine } = configuration(root);
  noSymlinks(bundle, root);
  if (fs.existsSync(bundle)) fs.rmSync(bundle, { recursive: true });
  fs.mkdirSync(bundle, { recursive: true, mode: 0o700 });
  const entries = [];
  function collect(file, relative) {
    const bytes = fs.readFileSync(file);
    assertNoSecrets(bytes.toString("utf8"));
    assertNoSecrets(bytes.toString("latin1"));
    const target = path.join(bundle, hash(relative) + ".json");
    // Keep raw bytes under a host-redaction-scanned suffix; the manifest records their original paths.
    fs.writeFileSync(target, bytes, { mode: 0o600 });
    entries.push({ path: relative, hash: hash(bytes) });
  }
  for (const [index, file] of files.entries()) {
    noSymlinks(file, root);
    if (!fs.existsSync(file)) continue;
    const stat = fs.lstatSync(file);
    if (stat.isDirectory()) walk(file, (source, relative) => collect(source, `${index}/${relative}`));
    else if (stat.isFile()) collect(file, String(index));
    else throw new Error("Durable state must contain only regular files and directories");
  }
  if (!entries.length) {
    core.info("The engine did not produce durable session files");
    return;
  }
  fs.writeFileSync(path.join(bundle, "manifest.json"), JSON.stringify({ version: 2, engine, files, entries }) + "\n", { mode: 0o600 });
  core.setOutput("present", "true");
}

function readBundle(root, { checkSecrets = true } = {}) {
  const config = configuration(root);
  noSymlinks(config.bundle, root);
  const manifestFile = path.join(config.bundle, "manifest.json");
  if (!fs.lstatSync(manifestFile).isFile()) throw new Error("Durable artifact manifest must be a regular file");
  const manifest = JSON.parse(fs.readFileSync(manifestFile, "utf8"));
  if (manifest.version !== 2 || manifest.engine !== config.engine || !isDeepStrictEqual(manifest.files, config.files) || !Array.isArray(manifest.entries) || !manifest.entries.length) {
    throw new Error("Durable artifact is incompatible with the engine's declared files");
  }
  const entries = new Map();
  for (const entry of manifest.entries) {
    const match = /^(0|[1-9]\d*)(?:\/(.+))?$/.exec(entry.path);
    if (!match || Number(match[1]) >= config.files.length || entry.path.split("/").some(part => part === "." || part === ".." || part === "") || entry.path.includes("\\") || entries.has(entry.path)) {
      throw new Error("Invalid durable artifact file path");
    }
    entries.set(entry.path, entry);
  }
  const blobs = new Map([...entries].map(([relative, entry]) => [hash(relative) + ".json", entry]));
  let count = 0;
  walk(config.bundle, (file, relative) => {
    if (relative === "manifest.json") return;
    const entry = blobs.get(relative);
    const bytes = fs.readFileSync(file);
    if (checkSecrets) {
      assertNoSecrets(bytes.toString("utf8"));
      assertNoSecrets(bytes.toString("latin1"));
    }
    if (!entry || hash(bytes) !== entry.hash) {
      throw new Error("Durable artifact was damaged or redacted; refusing continuation or publication");
    }
    entry.bytes = bytes;
    count++;
  });
  if (count !== entries.size) throw new Error("Durable artifact is missing declared session files");
  return { ...config, entries, manifest };
}

/** Validate every file before copying any untrusted artifact content. */
function restoreFiles({ core, root = "/tmp/gh-aw/agent" }) {
  const { files, entries } = readBundle(root);
  const targets = [...entries.entries()].map(([relative, entry]) => {
    const [index, ...suffix] = relative.split("/");
    const target = path.join(files[Number(index)], ...suffix);
    noSymlinks(target, root);
    if (fs.existsSync(target) && !fs.lstatSync(target).isFile()) throw new Error("Durable restore destination must be a regular file");
    return { bytes: entry.bytes, target };
  });
  for (const { bytes, target } of targets) {
    fs.mkdirSync(path.dirname(target), { recursive: true, mode: 0o700 });
    fs.writeFileSync(target, bytes, { mode: 0o600 });
    fs.chmodSync(target, 0o600);
  }
  core.setOutput("restored", "true");
}

function validateFiles({ core, root = "/tmp/gh-aw/agent" }) {
  readBundle(root);
  core.setOutput("present", "true");
}

const COPILOT_EVENT_METADATA_FIELDS = new Set(["sessionId", "toolCallId", "toolName", "turnId", "parentToolCallId", "parentToolUseId", "mcpServerName", "role", "model"]);

/** Preserve event envelopes, known top-level metadata, and payload structure. */
function eventPayloadShape(value, topLevel = false) {
  if (typeof value === "string") return null;
  if (Array.isArray(value)) return value.map(child => eventPayloadShape(child));
  if (value && typeof value === "object") {
    return Object.fromEntries(Object.entries(value).map(([key, child]) => [key, topLevel && COPILOT_EVENT_METADATA_FIELDS.has(key) ? child : eventPayloadShape(child)]));
  }
  // Distinguish a string replaced with a non-string JSON null.
  return [typeof value, value];
}

function copilotEventStructure(text) {
  const structure = [];
  for (const event of iterateJsonlEvents(text)) {
    if (event.type !== "json") {
      if (event.reason === "blank") continue;
      throw new Error("Cannot safely redact durable Copilot session events");
    }
    const { data, ...envelope } = event.value ?? {};
    if (typeof envelope.type !== "string" || !data || typeof data !== "object" || Array.isArray(data)) {
      throw new Error("Cannot safely redact durable Copilot session events");
    }
    structure.push({ envelope, data: eventPayloadShape(data, true) });
  }
  return structure;
}

function safeRedactionDelta(original, redacted, allowedValues) {
  if (typeof original === "string" && typeof redacted === "string") {
    if (original === redacted) return true;
    let sourceOffset = 0;
    let redactedOffset = 0;
    let changed = false;
    for (const match of overlappingMatchGroups(maskedValueMatches(original, allowedValues, undefined, true))) {
      const unchanged = original.slice(sourceOffset, match.start);
      if (!redacted.startsWith(unchanged, redactedOffset)) return false;
      redactedOffset += unchanged.length;
      const marker = redacted.startsWith("***REDACTED***", redactedOffset) ? "***REDACTED***" : redacted.startsWith("***", redactedOffset) ? "***" : "";
      if (!marker) return false;
      redactedOffset += marker.length;
      sourceOffset = match.end;
      changed = true;
    }
    return changed && redacted.slice(redactedOffset) === original.slice(sourceOffset);
  }
  if (Array.isArray(original) && Array.isArray(redacted)) {
    return original.length === redacted.length && original.every((value, index) => safeRedactionDelta(value, redacted[index], allowedValues));
  }
  if (original && redacted && typeof original === "object" && typeof redacted === "object") {
    const originalKeys = Object.keys(original);
    const redactedKeys = Object.keys(redacted);
    return isDeepStrictEqual(originalKeys, redactedKeys) && originalKeys.every(key => safeRedactionDelta(original[key], redacted[key], allowedValues));
  }
  return isDeepStrictEqual(original, redacted);
}

function copilotEventRedactionDeltas(originalText, redactedText, allowedValues) {
  const originalRecords = iterateJsonlEvents(originalText);
  const redactedRecords = iterateJsonlEvents(redactedText);
  while (true) {
    const original = originalRecords.next();
    const redacted = redactedRecords.next();
    if (original.done || redacted.done) return original.done === redacted.done;
    if (original.value.lineNumber !== redacted.value.lineNumber) return false;
    const originalLine = original.value.rawLine;
    const redactedLine = redacted.value.rawLine;
    const originalEnding = originalLine.endsWith("\r\n") ? "\r\n" : originalLine.endsWith("\n") ? "\n" : "";
    const redactedEnding = redactedLine.endsWith("\r\n") ? "\r\n" : redactedLine.endsWith("\n") ? "\n" : "";
    if (originalEnding !== redactedEnding) return false;
    if (original.value.type !== "json" || redacted.value.type !== "json") {
      if (original.value.type !== "invalid" || redacted.value.type !== "invalid" || original.value.reason !== "blank" || redacted.value.reason !== "blank" || originalLine !== redactedLine) {
        return false;
      }
      continue;
    }
    if (!safeRedactionDelta(original.value.value, redacted.value.value, allowedValues)) return false;
  }
}

/** Rehash only the trusted host redactor's own changes to valid Copilot transcripts. */
function redactCopilotCheckpoint(redact, root = "/tmp/gh-aw/agent", allowedValues = []) {
  const { bundle, engine, files } = configuration(root);
  if (engine !== "copilot") return;
  noSymlinks(bundle, root);
  const manifestPath = path.join(bundle, "manifest.json");
  if (!fs.existsSync(manifestPath)) return;
  const { entries, manifest } = readBundle(root, { checkSecrets: false });
  let changed = false;
  const updates = [];
  for (const [relative, entry] of entries) {
    const match = /^(\d+)\/[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}\/events\.jsonl$/i.exec(relative);
    if (!match || path.basename(files[Number(match[1])]) !== "session-state") continue;
    const text = entry.bytes.toString("utf8");
    const sanitized = redact(text);
    if (text === sanitized) continue;
    if (!entry.bytes.equals(Buffer.from(text)) || !isDeepStrictEqual(copilotEventStructure(text), copilotEventStructure(sanitized)) || !copilotEventRedactionDeltas(text, sanitized, [...allowedValues])) {
      throw new Error("Cannot safely redact durable Copilot session events");
    }
    assertNoSecrets(sanitized);
    const file = path.join(bundle, hash(relative) + ".json");
    const bytes = Buffer.from(sanitized);
    updates.push({ entry, file, bytes });
  }
  for (const { entry, file, bytes } of updates) {
    fs.writeFileSync(file, bytes);
    entry.hash = hash(bytes);
    changed = true;
  }
  if (changed) {
    const metadata = [...entries.values()].map(({ path, hash }) => ({ path, hash }));
    fs.writeFileSync(manifestPath, JSON.stringify({ ...manifest, entries: metadata }) + "\n");
  }
}

function invalidateCopilotCheckpoint(root = "/tmp/gh-aw/agent") {
  const bundle = path.join(root, "durable/bundle");
  noSymlinks(bundle, root);
  fs.rmSync(bundle, { recursive: true, force: true });
}

module.exports = { stageFiles, restoreFiles, validateFiles, redactCopilotCheckpoint, invalidateCopilotCheckpoint };
