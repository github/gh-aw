// Host reader for the structured verdict produced by threat-detect v0.5.3.
const fs = require("node:fs");
const path = require("node:path");
const { escapeWorkflowCommandValue } = require("./actions_secret_masking.cjs");

const KINDS = ["prompt_injection", "secret_leak", "malicious_patch"];
const STATUS_REASONS = new Map([
  ["invalid_report_exhausted", "parse_error"],
  ["output_write_error", "parse_error"],
  ["engine_error", "agent_failure"],
  ["engine_timeout", "agent_failure"],
  ["cancelled", "agent_failure"],
  ["config_error", "agent_failure"],
]);
const RESULT_LIMIT = 1024 * 1024;
const LOG_LIMIT = 8 * RESULT_LIMIT;

function readBounded(file, limit, tail = false) {
  const fd = fs.openSync(file, fs.constants.O_RDONLY | fs.constants.O_NONBLOCK);
  try {
    const stat = fs.fstatSync(fd);
    if (!stat.isFile()) throw new Error("Not a regular file");
    const start = tail ? Math.max(0, stat.size - limit) : 0;
    const buffer = Buffer.alloc(Math.min(limit, stat.size));
    let length = 0;
    while (length < buffer.length) {
      const count = fs.readSync(fd, buffer, length, buffer.length - length, start + length);
      if (count === 0) break;
      length += count;
    }
    const text = buffer.toString("utf8", 0, length);
    // A truncated first line might have lost its untrusted engine prefix.
    return start > 0 ? text.slice(text.indexOf("\n") < 0 ? text.length : text.indexOf("\n") + 1) : text;
  } finally {
    fs.closeSync(fd);
  }
}

function isBoundedText(value, limit) {
  return typeof value === "string" && value.trim() !== "" && [...value].length <= limit;
}

function isRecord(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function readResult(file) {
  let text;
  try {
    text = readBounded(file, RESULT_LIMIT + 1);
  } catch {
    return { problem: "unreadable" };
  }
  if (Buffer.byteLength(text) > RESULT_LIMIT) return { problem: "malformed" };
  let result;
  try {
    result = JSON.parse(text);
  } catch {
    return { problem: "malformed" };
  }
  if (
    !isRecord(result) ||
    Object.keys(result).some(key => ![...KINDS, "reasons", "warnings"].includes(key)) ||
    !KINDS.every(key => typeof result[key] === "boolean") ||
    !Array.isArray(result.reasons) ||
    result.reasons.length > 20 ||
    !result.reasons.every(reason => isBoundedText(reason, 2000))
  ) {
    return { problem: "malformed" };
  }
  if (
    Object.hasOwn(result, "warnings") &&
    (!Array.isArray(result.warnings) ||
      result.warnings.length > 20 ||
      !result.warnings.every(warning => isRecord(warning) && Object.keys(warning).length === 3 && isBoundedText(warning.field, 64) && isBoundedText(warning.code, 64) && isBoundedText(warning.message, 2000)))
  ) {
    return { problem: "malformed" };
  }
  return { result };
}

function readFailureStatus(file) {
  let status;
  try {
    for (const line of readBounded(file, LOG_LIMIT, true).split("\n")) {
      // The pinned producer starts its terminal status at the beginning of a
      // line; forwarded subprocess output always begins with "[engine] ".
      const match = /^[ \t]*THREAT_DETECTION_STATUS:(.*)$/.exec(line.trimEnd());
      if (match) {
        status = undefined;
        for (const token of match[1].split(/\s+/)) {
          if (token.startsWith("reason=")) status = token.slice("reason=".length);
        }
      }
    }
  } catch {
    status = undefined;
  }
  const reason = STATUS_REASONS.get(status);
  return { reason: reason ?? "agent_failure", status: reason ? status : undefined };
}

function safeLine(text) {
  return text
    .replaceAll("::", ":\\:")
    .replaceAll("##[", "##\\[")
    .replace(/[\p{Cc}\u2028\u2029]/gu, char => `\\u${char.charCodeAt(0).toString(16).padStart(4, "0")}`);
}

function annotate(level, message) {
  process.stdout.write(`::${level}::${escapeWorkflowCommandValue(safeLine(message))}\n`);
}

function append(file, values) {
  if (!file) return;
  try {
    fs.appendFileSync(
      file,
      Object.entries(values)
        .map(([key, value]) => `${key}=${value}\n`)
        .join("")
    );
  } catch (cause) {
    throw new Error("Could not write an Actions command file", { cause });
  }
}

function pathsAlias(left, right) {
  if (path.resolve(left) === path.resolve(right)) return true;
  try {
    const a = fs.statSync(left);
    const b = fs.statSync(right);
    return a.dev === b.dev && a.ino === b.ino;
  } catch {
    return false;
  }
}

function reportVerdict(result, reasons) {
  info("Threat detection verdict (from structured result file):");
  for (const kind of KINDS) info(`   ${kind}: ${result[kind]}`);
  for (const [index, reason] of reasons.entries()) {
    const lines = reason.split(/\r\n|\r|\n/);
    for (const [lineIndex, line] of lines.slice(0, 40).entries()) {
      info(`${lineIndex === 0 ? `   [${index + 1}] ` : "       | "}${line}`);
    }
    if (lines.length > 40) info("       | … (reason truncated)");
  }
  for (const warning of result.warnings ?? []) {
    info(`   Detector warning (does not affect the verdict): field=${warning.field} code=${warning.code} message=${warning.message}`);
  }
}

function info(message) {
  process.stdout.write(`${safeLine(message)}\n`);
}

function conclude(resultFile, env) {
  const run = env.RUN_DETECTION;
  const warn = (env.GH_AW_DETECTION_CONTINUE_ON_ERROR ?? "").toLowerCase() !== "false";
  const finish = (conclusion, reason = "", kinds = [], exportEnv = true) => {
    append(env.GITHUB_OUTPUT, { threat_kinds: kinds.join(","), conclusion, success: !reason, reason });
    if (exportEnv) append(env.GITHUB_ENV, { GH_AW_DETECTION_CONCLUSION: conclusion, GH_AW_DETECTION_REASON: reason });
    return conclusion === "failure" ? 1 : 0;
  };
  const fail = (reason, message, kinds = [], exportEnv = true) => {
    // A confirmed threat must not enter the tooling-failure warn policy, even
    // when the workflow's step-level continue-on-error absorbs this exit code.
    const allowWarning = warn && reason !== "threat_detected";
    annotate(allowWarning ? "warning" : "error", message);
    return finish(allowWarning ? "warning" : "failure", reason, kinds, exportEnv);
  };
  if (run !== "true") {
    info("Detection skipped — no threats to evaluate.");
    return finish("skipped", "", [], !(run === "false" && env.THREAT_DETECT_INSTALL_OUTCOME === "skipped"));
  }

  // Keep the host wrapper's install outcome precedence and outputs: AWF
  // failure can cause the detector install step to be skipped.
  for (const [tool, outcome] of [
    ["AWF", env.AWF_INSTALL_OUTCOME],
    ["threat-detect", env.THREAT_DETECT_INSTALL_OUTCOME],
  ]) {
    if (!outcome || outcome === "success") continue;
    if (tool === "AWF" && env.GH_AW_GITHUB_API_QUOTA_BLOCKED) {
      const quota = env.GH_AW_GITHUB_API_QUOTA_BLOCKED.replace(/[^A-Za-z0-9_=;:.+ -]/g, "").slice(0, 512);
      const message = `Threat detection is deferred: AWF attestation verification could not complete within the wait budget (${quota}).`;
      if (env.GITHUB_STEP_SUMMARY) {
        try {
          fs.appendFileSync(env.GITHUB_STEP_SUMMARY, `### GitHub API quota exhausted\n\n${message}\nThis is not a security pass. Safe-output handling follows the configured detection policy; completed agent artifacts are retained.\n\n`);
        } catch {
          info("Could not write the quota step summary.");
        }
      }
      return fail("github_api_quota_exhausted", `GitHub API quota exhausted. ${message}`, [], false);
    }
    return fail("agent_failure", `ERR_SYSTEM: ${tool} installation did not complete verification (install outcome: ${outcome})`, [], false);
  }

  const commandFiles = [env.GITHUB_OUTPUT, env.GITHUB_ENV].filter(Boolean);
  if (commandFiles.some(file => pathsAlias(resultFile, file)) || (commandFiles.length === 2 && pathsAlias(...commandFiles))) {
    console.error("ERR_SYSTEM: Threat detection result and Actions command files must be separate.");
    return 1;
  }
  const { result, problem } = readResult(resultFile);
  if (!result) {
    const log = env.DETECTION_LOG_FILE || path.join(path.dirname(resultFile), "detection.log");
    const { reason, status } = problem === "malformed" ? { reason: "parse_error" } : readFailureStatus(log);
    const code = reason === "parse_error" ? "ERR_PARSE" : "ERR_SYSTEM";
    // Never echo raw parser exceptions or the captured engine log.
    return fail(reason, `${code}: Structured detection result is ${problem} (${status ?? reason}).`);
  }

  const { dir, name, ext } = path.parse(resultFile);
  const full = readResult(path.join(dir, `${name}_full${ext}`)).result;
  const compatible = full && KINDS.every(kind => result[kind] === full[kind]);
  if (full && !compatible) info("Full result reports a different verdict; ignoring it.");
  reportVerdict(result, compatible && full.reasons.length ? full.reasons : result.reasons);

  const kinds = KINDS.filter(kind => result[kind]);
  if (kinds.length) {
    return fail("threat_detected", `ERR_VALIDATION: Security threats detected: ${kinds.map(kind => kind.replaceAll("_", " ")).join(", ")}. See the verdict log for details.`, kinds);
  }
  info("No security threats detected. Safe outputs may proceed.");
  return finish("success");
}

try {
  process.exitCode = conclude(process.argv[2] || "/tmp/gh-aw/threat-detection/detection_result.json", process.env);
} catch {
  console.error("ERR_SYSTEM: Could not write threat detection conclusion.");
  process.exitCode = 1;
}
