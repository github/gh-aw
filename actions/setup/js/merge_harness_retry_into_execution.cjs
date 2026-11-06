"use strict";

const fs = require("node:fs");
const { normalizeHarnessRetryRecord } = require("./harness_retry_record.cjs");

function readRegularJson(file, description) {
  const stat = fs.lstatSync(file);
  if (!stat.isFile() || stat.isSymbolicLink()) {
    throw new Error(`${description} must be a regular file`);
  }
  const value = JSON.parse(fs.readFileSync(file, "utf8"));
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new Error(`${description} must contain a JSON object`);
  }
  return value;
}

function mergeHarnessRetryIntoExecution(retryPath, executionPath) {
  if (!fs.existsSync(retryPath)) return false;

  const retryRecord = normalizeHarnessRetryRecord(readRegularJson(retryPath, "Harness retry telemetry"));
  if (!retryRecord) throw new Error("Harness retry telemetry is invalid");

  const execution = readRegularJson(executionPath, "Detection execution evidence");
  if (execution.version !== 1 || execution.component !== "detection") {
    throw new Error("Detection execution evidence has an unsupported format");
  }

  const temporaryPath = `${executionPath}.${process.pid}.tmp`;
  try {
    fs.writeFileSync(temporaryPath, `${JSON.stringify({ ...execution, harness_retry: retryRecord })}\n`, { flag: "wx", mode: 0o600 });
    fs.renameSync(temporaryPath, executionPath);
  } finally {
    fs.rmSync(temporaryPath, { force: true });
  }
  return true;
}

if (require.main === module) {
  try {
    if (process.argv.length !== 4) throw new Error("Expected retry telemetry and execution evidence paths");
    mergeHarnessRetryIntoExecution(process.argv[2], process.argv[3]);
  } catch (error) {
    process.stderr.write(`${error instanceof Error ? error.message : String(error)}\n`);
    process.exitCode = 1;
  }
}

module.exports = { mergeHarnessRetryIntoExecution };
