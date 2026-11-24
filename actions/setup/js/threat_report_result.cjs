// @ts-check
const fs = require("node:fs");
const path = require("node:path");
const { randomUUID } = require("node:crypto");

const CATEGORIES = Object.freeze(["prompt_injection", "secret_leak", "malicious_patch"]);
const MAX_BYTES = 1 << 20;
const MAX_REASONS = 20;
const MAX_REASON_LENGTH = 2000;
const CHANNELS = {
  prompt_injection: "an untrusted prompt region or comment-memory input",
  secret_leak: "a nonempty safe-output file, patch/bundle, or comment memory",
  malicious_patch: "a patch or bundle (rejected safe-output validation is not a code change)",
};

class IneligibleReportError extends Error {
  constructor(message) {
    super(message);
    this.code = "ERR_REPORT_INELIGIBLE";
  }
}

function isBoundedText(text, limit) {
  return typeof text === "string" && text.length <= 2 * limit && /\P{White_Space}/u.test(text) && [...text].length <= limit;
}

function validateSinkWarnings(warnings) {
  if (!Array.isArray(warnings) || warnings.length > 20) throw new Error("Expected an array of at most 20 warnings");
  for (const warning of warnings) {
    if (!warning || typeof warning !== "object" || Array.isArray(warning) || Object.keys(warning).length !== 3) throw new Error("Expected a warning with field, code, and message");
    for (const [key, limit] of Object.entries({ field: 64, code: 64, message: 2000 })) {
      if (!Object.hasOwn(warning, key) || !isBoundedText(warning[key], limit)) throw new Error(`Warning ${key} must be nonblank and at most ${limit} characters`);
    }
  }
}

// Validate sink warnings for wire compatibility, but discard them: only the
// host's artifact inspection is authoritative for the warnings it publishes.
function validateReport(value, eligibility) {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("Expected a report object");
  if (Object.keys(value).some(k => ![...CATEGORIES, "reasons", "warnings"].includes(k))) throw new Error("Unexpected field in report");
  for (const key of CATEGORIES) {
    if (!Object.hasOwn(value, key) || typeof value[key] !== "boolean") throw new Error(`Expected a boolean for ${key}`);
  }
  if (!Object.hasOwn(value, "reasons") || !Array.isArray(value.reasons) || value.reasons.length > MAX_REASONS) throw new Error(`Expected an array of at most ${MAX_REASONS} reasons`);
  for (const reason of value.reasons) {
    if (!isBoundedText(reason, MAX_REASON_LENGTH)) {
      throw new Error(`Each reason must be nonblank and at most ${MAX_REASON_LENGTH} characters`);
    }
  }
  if (Object.hasOwn(value, "warnings")) validateSinkWarnings(value.warnings);
  const problems = [];
  for (const key of CATEGORIES) {
    if (!eligibility || !Object.hasOwn(eligibility, key) || typeof eligibility[key] !== "boolean") throw new Error(`Missing boolean eligibility for ${key}`);
    if (value[key] && !eligibility[key]) problems.push(`--${key.replaceAll("_", "-")}=true is not eligible: requires ${CHANNELS[key]}`);
  }
  if (problems.length) throw new IneligibleReportError(problems.join("; "));
  const { prompt_injection, secret_leak, malicious_patch } = value;
  return { prompt_injection, secret_leak, malicious_patch, reasons: [...value.reasons], warnings: [] };
}

// Only pass the validated full result, with warnings sourced from the detector.
function publishableResult({ prompt_injection, secret_leak, malicious_patch, warnings }) {
  return { prompt_injection, secret_leak, malicious_patch, reasons: [], warnings };
}

function readBoundedJSON(file) {
  // O_NONBLOCK avoids hanging on a FIFO before fstat can reject it.
  const fd = fs.openSync(file, fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW | fs.constants.O_NONBLOCK);
  try {
    const stat = fs.fstatSync(fd);
    if (!stat.isFile()) throw new Error("Expected a regular JSON file");
    if (stat.size > MAX_BYTES) throw new Error("JSON file exceeds 1 MiB");
    // An extra byte detects growth after fstat without an unbounded read.
    const buf = Buffer.alloc(stat.size + 1);
    let used = 0;
    while (used < buf.length) {
      const n = fs.readSync(fd, buf, used, buf.length - used, null);
      if (!n) break;
      used += n;
    }
    if (used !== stat.size) throw new Error("JSON file changed during read");
    try {
      return JSON.parse(new TextDecoder("utf8", { fatal: true, ignoreBOM: true }).decode(buf.subarray(0, used)));
    } catch {
      throw new Error("Expected one UTF-8 JSON value");
    }
  } finally {
    fs.closeSync(fd);
  }
}

function readSink(file, eligibility) {
  return validateReport(readBoundedJSON(file), eligibility);
}

// The caller supplies an initially absent path in a private, trusted directory.
// Publish a complete private file atomically, without clobbering a racing writer.
function recordSink(file, full, eligibility) {
  const tmp = path.join(path.dirname(file), `.threat-result-${randomUUID()}`);
  const fd = fs.openSync(tmp, "wx", 0o600);
  try {
    try {
      fs.writeFileSync(fd, JSON.stringify(full));
    } finally {
      fs.closeSync(fd);
    }
    try {
      fs.linkSync(tmp, file);
      return true;
    } catch (error) {
      if (error.code !== "EEXIST") throw error;
      readSink(file, eligibility);
      return false;
    }
  } finally {
    fs.unlinkSync(tmp);
  }
}

module.exports = { CATEGORIES, IneligibleReportError, validateReport, publishableResult, readBoundedJSON, readSink, recordSink };
