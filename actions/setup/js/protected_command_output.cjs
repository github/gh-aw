// @ts-check

const fs = require("fs");
const path = require("path");
const crypto = require("crypto");
const { ERR_VALIDATION } = require("./error_codes.cjs");
const { collectAddMaskedValues } = require("./add_mask_redaction.cjs");
const { MAX_CAPTURE_BYTES } = require("./constants.cjs");

/** @param {string} message @returns {never} */
function invalidCapture(message) {
  throw new Error(`${ERR_VALIDATION}: Invalid AWF protected command output: ${message}`);
}

/** @param {string} file @param {number} maxBytes @param {number} [expectedBytes] @param {boolean} [readContent] @returns {Buffer} */
function readPrivateFile(file, maxBytes, expectedBytes, readContent = true) {
  const fd = fs.openSync(file, fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW | fs.constants.O_NONBLOCK);
  try {
    const stat = fs.fstatSync(fd);
    if (!stat.isFile() || stat.nlink !== 1 || (stat.mode & 0o077) !== 0 || stat.uid !== process.getuid?.() || stat.size > maxBytes || (expectedBytes !== undefined && stat.size !== expectedBytes)) {
      invalidCapture("expected a bounded, private regular file");
    }
    if (!readContent) return Buffer.alloc(0);
    const content = fs.readFileSync(fd);
    if (content.length !== stat.size) invalidCapture("capture changed while reading");
    return content;
  } catch (error) {
    throw new Error(`${ERR_VALIDATION}: Invalid AWF protected command output file`, { cause: error });
  } finally {
    fs.closeSync(fd);
  }
}

/**
 * Consume only the final stderr attempt. Stdout is an untrusted presentation
 * stream, never a continuation of the recorder's stderr channel.
 * @param {string} directory
 * @returns {{content: string, exitCode: number, maskedValues: string[]}}
 */
function readProtectedCommandOutput(directory) {
  const stat = fs.lstatSync(directory);
  if (!stat.isDirectory() || stat.isSymbolicLink() || (stat.mode & 0o077) !== 0 || stat.uid !== process.getuid?.()) {
    invalidCapture("expected a private host-only directory");
  }
  let metadata;
  try {
    metadata = JSON.parse(readPrivateFile(path.join(directory, "metadata.json"), 1024 * 1024).toString("utf8"));
  } catch (error) {
    if (error instanceof SyntaxError) invalidCapture("malformed metadata");
    throw error;
  }
  if (
    metadata.schemaVersion !== 1 ||
    metadata.status !== "complete" ||
    metadata.teardown !== "complete" ||
    metadata.truncated !== false ||
    metadata.captureFailure !== undefined ||
    metadata.command?.outcome !== "exited" ||
    !Number.isInteger(metadata.command.exitCode) ||
    metadata.command.exitCode < 0 ||
    metadata.command.exitCode > 255 ||
    !Number.isSafeInteger(metadata.maxBytes) ||
    metadata.maxBytes < 1 ||
    metadata.maxBytes > MAX_CAPTURE_BYTES ||
    !Array.isArray(metadata.attempts) ||
    metadata.attempts.length < 1 ||
    metadata.attempts.length > 2
  ) {
    invalidCapture("capture is incomplete, unsupported or truncated");
  }
  let finalAttempt;
  let finalIndex = 0;
  let capturedBytes = 0;
  for (const [index, attempt] of metadata.attempts.entries()) {
    for (const streamName of ["stdout", "stderr"]) {
      const stream = attempt[streamName];
      const expectedFile = `attempt-${String(index + 1).padStart(3, "0")}.${streamName}.bin`;
      if (stream?.file !== expectedFile || !Number.isSafeInteger(stream.capturedBytes) || stream.capturedBytes < 0 || stream.observedBytes !== stream.capturedBytes) {
        invalidCapture("invalid stream metadata");
      }
      capturedBytes += stream.capturedBytes;
      if (attempt.outcome !== "not_started") readPrivateFile(path.join(directory, expectedFile), MAX_CAPTURE_BYTES, stream.capturedBytes, false);
      else if (stream.capturedBytes !== 0) invalidCapture("unstarted attempt contains output");
    }
    if (attempt.outcome === "not_started") continue;
    if (attempt.outcome !== "exited" || attempt.logsDrained !== true) invalidCapture("attempt did not finish draining");
    finalAttempt = attempt;
    finalIndex = index + 1;
  }
  if (capturedBytes > metadata.maxBytes || capturedBytes !== metadata.capturedBytes || capturedBytes !== metadata.observedBytes) invalidCapture("invalid aggregate byte counts");
  if (!finalAttempt || finalAttempt.exitCode !== metadata.command.exitCode) invalidCapture("no consistent final command attempt");
  const stderr = finalAttempt.stderr;
  const file = `attempt-${String(finalIndex).padStart(3, "0")}.stderr.bin`;
  if (stderr?.file !== file || !Number.isSafeInteger(stderr.capturedBytes) || stderr.capturedBytes < 0 || stderr.capturedBytes > MAX_CAPTURE_BYTES || stderr.observedBytes !== stderr.capturedBytes) {
    invalidCapture("invalid stderr metadata");
  }
  const content = readPrivateFile(path.join(directory, file), MAX_CAPTURE_BYTES);
  if (content.length !== stderr.capturedBytes) invalidCapture("stderr size does not match metadata");
  const maskedValues = new Set();
  // Mask requests affect privacy, but never provide session events or outcomes.
  for (const attempt of metadata.attempts) {
    if (attempt.outcome === "not_started") continue;
    for (const stream of [attempt.stdout, attempt.stderr]) {
      const text = readPrivateFile(path.join(directory, stream.file), MAX_CAPTURE_BYTES, stream.capturedBytes).toString("utf8");
      for (const value of collectAddMaskedValues(text)) maskedValues.add(value);
    }
  }
  return { content: content.toString("utf8"), exitCode: metadata.command.exitCode, maskedValues: [...maskedValues] };
}

/**
 * Atomically replace the shared exit-code sidecar with AWF's verified value.
 * Renaming over the destination replaces a symlink itself rather than following it.
 * @param {string} rootDir
 * @param {number} exitCode
 */
function writeVerifiedExitCode(rootDir, exitCode) {
  if (!Number.isInteger(exitCode) || exitCode < 0 || exitCode > 255) invalidCapture("invalid verified exit code");
  const destination = path.join(rootDir, "agent_execution_exit_code.txt");
  const temporary = `${destination}.${crypto.randomBytes(16).toString("hex")}.tmp`;
  let descriptor;
  try {
    descriptor = fs.openSync(temporary, fs.constants.O_WRONLY | fs.constants.O_CREAT | fs.constants.O_EXCL | (fs.constants.O_NOFOLLOW ?? 0), 0o600);
    fs.writeFileSync(descriptor, `${exitCode}\n`, "utf8");
    fs.fsyncSync(descriptor);
    fs.closeSync(descriptor);
    descriptor = undefined;
    fs.renameSync(temporary, destination);
  } catch (error) {
    let cause = error;
    if (descriptor !== undefined) {
      try {
        fs.closeSync(descriptor);
      } catch (closeError) {
        cause = new AggregateError([cause, closeError], "Failed to close the temporary exit-code file");
      }
    }
    try {
      fs.unlinkSync(temporary);
    } catch (cleanupError) {
      cause = new AggregateError([cause, cleanupError], "Failed to remove the temporary exit-code file");
    }
    throw new Error(`${ERR_VALIDATION}: Failed to replace the shared session exit code with AWF's verified value`, { cause });
  }
}

/** @param {string} rootDir */
function discardUntrustedSessionArtifacts(rootDir) {
  const errors = [];
  for (const name of ["agent-stdio.log", "agent-session.jsonl", "agent-session.jsonl.tmp", "agent-errors.jsonl", "agent-errors.jsonl.tmp"]) {
    try {
      fs.rmSync(path.join(rootDir, name), { force: true });
    } catch (error) {
      errors.push(error);
    }
  }
  if (errors.length) throw new AggregateError(errors, `${ERR_VALIDATION}: Failed to discard sandbox-written session artifacts`);
}

module.exports = { readProtectedCommandOutput, writeVerifiedExitCode, discardUntrustedSessionArtifacts };
