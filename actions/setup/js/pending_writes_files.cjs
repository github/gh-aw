// @ts-check
"use strict";

const fs = require("node:fs");
const path = require("node:path");
const crypto = require("node:crypto");

const MAX_INPUT_BYTES = 80 * 1024 * 1024;
const MAX_FILES = 10000;
const sha256 = bytes => crypto.createHash("sha256").update(bytes).digest("hex");

/**
 * Read through one descriptor: symlinks, special files and unbounded inputs are
 * not safe gate inputs. Never log rejected bytes or agent-controlled paths.
 * @param {string} file
 */
function readInput(file) {
  if (typeof file !== "string" || !path.isAbsolute(file)) throw new Error("Pending-write input requires an absolute path");
  const fd = fs.openSync(file, fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW | fs.constants.O_NONBLOCK);
  try {
    const stat = fs.fstatSync(fd);
    if (!stat.isFile() || stat.size > MAX_INPUT_BYTES) throw new Error("Pending-write input is not a bounded regular file");
    const bytes = fs.readFileSync(fd);
    const after = fs.fstatSync(fd);
    if (after.size !== stat.size || after.mtimeMs !== stat.mtimeMs || bytes.length !== stat.size) throw new Error("Pending-write input changed while reading");
    return { bytes, length: bytes.length, sha256: sha256(bytes), mode: stat.mode & 0o111 ? "100755" : "100644" };
  } finally {
    fs.closeSync(fd);
  }
}

/** @param {string} root */
function describeDirectory(root) {
  if (!fs.lstatSync(root).isDirectory()) throw new Error("Pending-write directory must be a real directory");
  const files = [];
  function walk(directory, relative = "") {
    for (const entry of fs.readdirSync(directory, { withFileTypes: true }).sort((a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : 0))) {
      if (entry.name === ".git") continue;
      const relativePath = relative ? `${relative}/${entry.name}` : entry.name;
      const file = path.join(directory, entry.name);
      if (entry.isDirectory()) walk(file, relativePath);
      else {
        if (!entry.isFile()) throw new Error("Pending-write directory contains a symlink or special file");
        if (files.length >= MAX_FILES) throw new Error("Pending-write file inventory exceeds the limit");
        const { length, sha256: digest, mode } = readInput(file);
        files.push({ path: relativePath, length, sha256: digest, mode });
      }
    }
  }
  walk(root);
  return files;
}

module.exports = { MAX_INPUT_BYTES, MAX_FILES, sha256, readInput, describeDirectory };
