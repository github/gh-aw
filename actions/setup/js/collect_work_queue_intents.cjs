// @ts-check
"use strict";

const fs = require("fs");
const path = require("path");

const MAX_BYTES = 4 * 1024 * 1024;
const MAX_LINES = 256;
const FILENAMES = ["work-queue.intents.jsonl", "work-queue.finish.jsonl"];

function copyIntentFile(source, destination) {
  if (fs.constants.O_NOFOLLOW === undefined) throw new Error("work_queue_collection_no_follow_unavailable");
  let input;
  let output;
  try {
    if (!fs.lstatSync(source).isFile()) throw new Error("work_queue_collection_source_invalid");
    input = fs.openSync(source, fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW | fs.constants.O_NONBLOCK);
    const stat = fs.fstatSync(input);
    if (!stat.isFile() || stat.size > MAX_BYTES) throw new Error("work_queue_collection_limit");
    const buffer = Buffer.alloc(MAX_BYTES + 1);
    let length = 0;
    while (length < buffer.length) {
      const read = fs.readSync(input, buffer, length, buffer.length - length, null);
      if (read === 0) break;
      length += read;
    }
    if (
      length > MAX_BYTES ||
      buffer
        .subarray(0, length)
        .toString("utf8")
        .split("\n")
        .filter(line => line.trim()).length > MAX_LINES
    ) {
      throw new Error("work_queue_collection_limit");
    }
    // Copy bytes, not interpreted authority: trusted ingestion validates each intent.
    output = fs.openSync(destination, fs.constants.O_WRONLY | fs.constants.O_CREAT | fs.constants.O_NOFOLLOW | fs.constants.O_NONBLOCK, 0o600);
    const destinationStat = fs.fstatSync(output);
    if (!destinationStat.isFile() || destinationStat.nlink !== 1) throw new Error("work_queue_collection_destination_invalid");
    fs.ftruncateSync(output, 0);
    fs.writeFileSync(output, buffer.subarray(0, length));
    return length;
  } finally {
    if (input !== undefined) fs.closeSync(input);
    if (output !== undefined) fs.closeSync(output);
  }
}

function main(options = {}) {
  const core = options.core || global.core;
  let sourceDir = options.sourceDir;
  if (!sourceDir) {
    const runnerTemp = process.env.RUNNER_TEMP;
    if (!runnerTemp) throw new Error("work_queue_collection_runner_temp_missing");
    sourceDir = path.join(runnerTemp, "gh-aw", "safeoutputs", "work-queue");
  }
  const outputDir = options.outputDir || "/tmp/gh-aw";
  fs.mkdirSync(outputDir, { recursive: true });
  if (!fs.lstatSync(outputDir).isDirectory()) throw new Error("work_queue_collection_destination_invalid");
  const results = [];
  for (const filename of FILENAMES) {
    try {
      const source = path.join(sourceDir, filename);
      if (!fs.existsSync(source)) continue;
      if (!fs.lstatSync(sourceDir).isDirectory()) throw new Error("work_queue_collection_source_invalid");
      const bytes = copyIntentFile(source, path.join(outputDir, filename));
      results.push({ filename, bytes, status: "collected" });
    } catch {
      results.push({ filename, status: "rejected" });
      core.setFailed(`Work queue intent collection rejected ${filename}`);
    }
  }
  return results;
}

module.exports = { main, copyIntentFile, MAX_BYTES, MAX_LINES };
