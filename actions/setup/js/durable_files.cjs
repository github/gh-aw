// @ts-check
"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { createHash } = require("node:crypto");
const { isDeepStrictEqual } = require("node:util");

/** Refuse credentials rather than redacting and corrupting restorable session state. */
function assertNoSecrets(value, env = process.env) {
  const text = JSON.stringify(value);
  for (const [name, secret] of Object.entries(env)) {
    if (/(?:TOKEN|KEY|SECRET|PASSWORD|CREDENTIAL)/i.test(name) && secret && secret.length >= 6 && text.includes(JSON.stringify(secret).slice(1, -1))) {
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

function readBundle(root) {
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
    assertNoSecrets(bytes.toString("utf8"));
    assertNoSecrets(bytes.toString("latin1"));
    if (!entry || hash(bytes) !== entry.hash) {
      throw new Error("Durable artifact was damaged or redacted; refusing continuation or publication");
    }
    entry.bytes = bytes;
    count++;
  });
  if (count !== entries.size) throw new Error("Durable artifact is missing declared session files");
  return { ...config, entries };
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

module.exports = { stageFiles, restoreFiles, validateFiles };
