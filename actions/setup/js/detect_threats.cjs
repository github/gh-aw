// Sandboxed producer; conclude_threat_detection.cjs is the host-side consumer.
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { randomUUID } = require("node:crypto");
const { parseArgs } = require("node:util");
const { prepareAssessment } = require("./threat_assessment.cjs");
const { runHarness } = require("./threat_harness.cjs");
const { readSink, publishableResult } = require("./threat_report.cjs");

function diagnostic(text) {
  const line = String(text)
    .replace(/[\p{Cc}\u2028\u2029]/gu, " ")
    .replaceAll("::", ":\\:")
    .replaceAll("##[", "##\\[");
  process.stderr.write(`[threat-detect] ${line}\n`);
}

function intOption(value, fallback) {
  if (value === undefined) return fallback;
  if (!/^\+?\d+$/.test(value) || !Number.isSafeInteger(Number(value))) throw new Error("Expected a nonnegative integer budget");
  return Number(value);
}

function durationMs(value) {
  if (value === "0") return 0;
  const scale = { ns: 1e-6, us: 1e-3, µs: 1e-3, μs: 1e-3, ms: 1, s: 1000, m: 60000, h: 3600000 };
  const part = /(\d+(?:\.\d*)?|\.\d+)(ns|us|µs|μs|ms|s|m|h)/gy;
  let ms = 0;
  let length = 0;
  for (const m of value.matchAll(part)) {
    length += m[0].length;
    ms += Number(m[1]) * scale[m[2]];
  }
  if (!length || length !== value.length || !Number.isSafeInteger(Math.ceil(ms))) throw new Error("Invalid engine timeout");
  return Math.ceil(ms);
}

function envTurns() {
  for (const raw of [process.env.THREAT_DETECTION_MAX_TURNS, process.env.GH_AW_MAX_TURNS]) {
    if (/^\+?\d+$/.test(raw?.trim() ?? "") && Number.isSafeInteger(Number(raw))) return Number(raw);
  }
  return 50;
}

function readLaunch(file) {
  const launch = JSON.parse(fs.readFileSync(file, "utf8"));
  if (!launch || typeof launch.command !== "string" || !launch.command.trim()) throw new Error("The launch descriptor requires an executable command");
  if (!["copilot", "claude", "codex"].includes(launch.convention)) throw new Error("Expected a copilot, claude, or codex launch convention");
  if (launch.args !== undefined && (!Array.isArray(launch.args) || !launch.args.every(arg => typeof arg === "string"))) throw new Error("Launch args must be an array of strings");
  if (launch.mode !== undefined && !["direct", "harness"].includes(launch.mode)) throw new Error("Expected direct or harness launch mode");
  return launch;
}

function parseOptions(args) {
  const names = ["launch-config", "workflow-name", "workflow-description", "custom-prompt", "custom-prompt-file", "prompt-template", "retries", "max-turns", "engine-timeout", "output", "full-output"];
  const { values, positionals } = parseArgs({ args, allowPositionals: true, options: Object.fromEntries(names.map(name => [name, { type: "string" }])) });
  if (positionals.length !== 1 || !values["launch-config"]) throw new Error("Expected --launch-config <trusted JSON file> and one artifacts directory");
  let timeoutMs = 300000;
  if (process.env.THREAT_DETECTION_ENGINE_TIMEOUT) {
    try {
      timeoutMs = durationMs(process.env.THREAT_DETECTION_ENGINE_TIMEOUT.trim());
    } catch {
      diagnostic("Invalid THREAT_DETECTION_ENGINE_TIMEOUT; retaining the five-minute default");
    }
  }
  const output = values.output ?? "";
  const { dir, name, ext } = path.parse(output);
  const rawRetries = process.env.THREAT_DETECTION_RETRIES ?? "";
  return {
    artifactsDir: positionals[0],
    launch: readLaunch(values["launch-config"]),
    workflowName: values["workflow-name"],
    workflowDescription: values["workflow-description"],
    customPrompt: values["custom-prompt"],
    customPromptFile: values["custom-prompt-file"],
    promptTemplate: values["prompt-template"],
    retries: intOption(values.retries ?? (/^[+-]?\d+$/.test(rawRetries) ? rawRetries : undefined), 0),
    timeoutMs: values["engine-timeout"] === undefined ? timeoutMs : durationMs(values["engine-timeout"]),
    maxTurns: intOption(values["max-turns"], envTurns()),
    output,
    fullOutput: values["full-output"] ?? (output ? path.join(dir, `${name}_full${ext}`) : ""),
  };
}

function checkDestinations({ output, fullOutput }) {
  if (!output || !fullOutput) return;
  const canonical = file => {
    try {
      return fs.realpathSync(file);
    } catch (err) {
      if (err.code !== "ENOENT") throw err;
      // Resolve existing parent components even when the leaf does not yet exist.
      return path.join(canonical(path.dirname(path.resolve(file))), path.basename(file));
    }
  };
  const a = fs.statSync(output, { throwIfNoEntry: false });
  const b = fs.statSync(fullOutput, { throwIfNoEntry: false });
  if (canonical(output) === canonical(fullOutput) || (a && b && a.dev === b.dev && a.ino === b.ino)) throw new Error("Publishable and full results must have distinct paths");
}

function writeResult(file, result) {
  const temp = path.join(path.dirname(file), `.threat-result-${randomUUID()}`);
  try {
    fs.writeFileSync(temp, JSON.stringify(result, null, 2), { flag: "wx", mode: 0o600 });
    fs.renameSync(temp, file);
  } finally {
    fs.rmSync(temp, { force: true });
  }
}

function publish(opts, result, warnings) {
  const full = { ...result, warnings }; // The sink is never authoritative for warnings.
  if (opts.fullOutput) {
    try {
      writeResult(opts.fullOutput, full);
    } catch {
      diagnostic("Could not write the private full result; the verdict is unaffected");
    }
  }
  try {
    const redacted = publishableResult(full);
    if (opts.output) writeResult(opts.output, redacted);
    else process.stdout.write(`${JSON.stringify(redacted, null, 2)}\n`);
  } catch {
    return { reason: "output_write_error", code: 2 };
  }
  return { reason: "result_recorded", code: result.prompt_injection || result.secret_leak || result.malicious_patch ? 1 : 0 };
}

async function detect(opts, signal) {
  checkDestinations(opts);
  const { prompt, eligibility, warnings } = prepareAssessment(opts);
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "threat-detect-"));
  let correction = "";
  try {
    for (let i = 0; i <= opts.retries; i++) {
      if (signal.aborted) return { reason: "cancelled", code: 2 };
      // A fresh path for every attempt: no stale or partially written verdict can carry forward.
      const sink = path.join(dir, `attempt-${i}.json`);
      const run = await runHarness({ launch: opts.launch, sink, prompt: prompt + correction, eligibility, maxTurns: opts.maxTurns, timeoutMs: opts.timeoutMs, signal });
      let result;
      try {
        // Always validate in this process, even if the deadline or an engine error just fired.
        result = readSink(sink, eligibility);
      } catch (err) {
        correction =
          err.code === "ERR_REPORT_INELIGIBLE"
            ? `\n\nYour previous verdict was rejected as structurally ineligible: ${err.message}\nRe-analyze the artifacts and run threat_detection_result again, keeping any category you can still support.`
            : "\n\nYour previous response did not record a verdict. Run threat_detection_result with all three boolean flags. For findings, write reasons to $THREAT_DETECTION_REASONS_FILE and use --reasons-file.";
      }
      if (result) return publish(opts, result, warnings);
      if (signal.aborted) return { reason: "cancelled", code: 2 };
      if (run.timedOut) return { reason: "engine_timeout", code: 2 };
      if (run.error) {
        diagnostic(run.error.message);
        return { reason: "engine_error", code: 2 };
      }
    }
    return { reason: "invalid_report_exhausted", code: 2 };
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
}

async function main(args) {
  const controller = new AbortController();
  const cancel = () => controller.abort();
  process.on("SIGTERM", cancel);
  process.on("SIGINT", cancel);
  let status;
  try {
    status = await detect(parseOptions(args), controller.signal);
  } catch (err) {
    diagnostic(err.message);
    status = { reason: "config_error", code: 2 };
  } finally {
    process.off("SIGTERM", cancel);
    process.off("SIGINT", cancel);
  }
  process.stderr.write(`THREAT_DETECTION_STATUS: reason=${status.reason} exit=${status.code}\n`);
  return status.code;
}

if (require.main === module)
  main(process.argv.slice(2)).then(code => {
    process.exitCode = code;
  });
