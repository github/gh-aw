// @ts-check
"use strict";

const fs = require("node:fs");

/** @typedef {{description: string, command: string, args: readonly string[], timeout: number}} TaskDefinition */
const MAX_TASK_OUTPUT_BYTES = 256 * 1024;
const MAX_MANIFEST_BYTES = 1024 * 1024;

/** @param {unknown} value @returns {value is Record<string, unknown>} */
function isRecord(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

/** @param {unknown} manifest @returns {Record<string, TaskDefinition>} */
function parseTaskManifest(manifest) {
  if (!isRecord(manifest) || manifest.version !== 1 || !isRecord(manifest.tasks) || Object.keys(manifest).some(key => !["version", "tasks"].includes(key))) {
    throw new Error("Locked tasks manifest must be a version-1 object with task definitions");
  }
  const entries = Object.entries(manifest.tasks);
  if (entries.length < 1 || entries.length > 64) throw new Error("Locked tasks manifest must contain between 1 and 64 tasks");
  /** @type {Record<string, TaskDefinition>} */
  const tasks = Object.create(null);
  for (const [name, task] of entries) {
    if (!/^[A-Za-z][A-Za-z0-9_.-]{0,63}$/.test(name) || ["__proto__", "constructor", "prototype"].includes(name)) throw new Error("Invalid task name");
    if (!isRecord(task) || Object.keys(task).some(key => !["description", "command", "args", "timeout"].includes(key))) throw new Error(`Invalid definition for task ${name}`);
    if (typeof task.description !== "string" || !task.description.trim() || Buffer.byteLength(task.description) > 1024) throw new Error(`Task ${name} needs a description`);
    if (typeof task.command !== "string" || !/^[A-Za-z0-9][A-Za-z0-9_.+-]{0,127}$/.test(task.command)) throw new Error(`Task ${name} requires an executable name without a path`);
    if (!Array.isArray(task.args) || task.args.length > 128 || task.args.some(arg => typeof arg !== "string" || arg.includes("\0") || arg.includes("${{") || Buffer.byteLength(arg) > 8192)) {
      throw new Error(`Task ${name} arguments must be bounded literal strings`);
    }
    if (task.description.includes("\0") || task.description.includes("${{")) throw new Error(`Task ${name} description must be literal`);
    if (typeof task.timeout !== "number" || !Number.isSafeInteger(task.timeout) || task.timeout < 1 || task.timeout > 600) throw new Error(`Task ${name} timeout must be between 1 and 600 seconds`);
    tasks[name] = Object.freeze({ description: task.description, command: task.command, args: Object.freeze([...task.args]), timeout: task.timeout });
  }
  return Object.freeze(tasks);
}

/** @param {string} filename */
function loadTaskManifest(filename) {
  const fd = fs.openSync(filename, fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW);
  try {
    const stat = fs.fstatSync(fd);
    if (!stat.isFile() || stat.size > MAX_MANIFEST_BYTES) throw new Error("Locked tasks manifest must be a regular file of at most 1 MiB");
    const buffer = Buffer.alloc(MAX_MANIFEST_BYTES + 1);
    let length = 0;
    while (length < buffer.length) {
      const count = fs.readSync(fd, buffer, length, buffer.length - length, null);
      if (!count) break;
      length += count;
    }
    if (length > MAX_MANIFEST_BYTES) throw new Error("Locked tasks manifest exceeds 1 MiB");
    return parseTaskManifest(JSON.parse(buffer.subarray(0, length).toString("utf8")));
  } finally {
    fs.closeSync(fd);
  }
}

module.exports = { isRecord, parseTaskManifest, loadTaskManifest, MAX_TASK_OUTPUT_BYTES };
