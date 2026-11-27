// @ts-check
// Entrypoint for the harness's threat_detection_result PATH shim.
const { CATEGORIES, IneligibleReportError, publishableResult, readBoundedJSON, readSink, recordSink, validateReport } = require("./threat_report_result.cjs");

function parseBoolean(value) {
  if (["1", "t", "T", "true", "True", "TRUE"].includes(value)) return true;
  if (["0", "f", "F", "false", "False", "FALSE"].includes(value)) return false;
  return undefined;
}

function eligibilityFromEnv(env) {
  return Object.fromEntries(CATEGORIES.map(key => [key, parseBoolean(env[`THREAT_DETECTION_ELIGIBLE_${key.toUpperCase()}`]) ?? true]));
}

function parseFlags(argv) {
  /** @type {{reasons: string[], resultFile?: string, reasonsFile?: string}} */
  const flags = { reasons: [], resultFile: undefined, reasonsFile: undefined };
  for (let i = 0; i < argv.length; i++) {
    const match = /^--?([a-z-]+)(?:=([\s\S]*))?$/.exec(argv[i]);
    if (!match) throw new Error("Expected report flags, not positional arguments");
    const [, flag, inline] = match;
    const key = flag.replaceAll("-", "_");
    if (CATEGORIES.includes(key)) {
      let value = parseBoolean(inline);
      if (inline === undefined) {
        value = parseBoolean(argv[i + 1]);
        if (value !== undefined) i++;
        else value = true; // Go FlagSet's bare boolean flag.
      }
      if (value === undefined) throw new Error(`--${flag} must be true or false`);
      flags[key] = value;
    } else {
      if (!["reason", "reasons-file", "result-file"].includes(flag)) throw new Error("Unknown report flag");
      const value = inline ?? argv[++i];
      if (value === undefined) throw new Error(`Missing value for --${flag}`);
      if (flag === "reason") flags.reasons.push(value);
      else if (flag === "reasons-file") flags.reasonsFile = value;
      else flags.resultFile = value;
    }
  }
  for (const key of CATEGORIES) {
    if (!Object.hasOwn(flags, key)) throw new Error(`Missing required flag --${key.replaceAll("_", "-")}`);
  }
  return flags;
}

function failure(code, reason) {
  const message = `THREAT_DETECTION_RESULT_ERROR: ${reason}.${code === 2 ? " Re-run threat_detection_result with corrected values." : ""}`;
  console.log(message);
  console.error(message);
  return code;
}

function runReport(argv, env) {
  let sink;
  let report;
  const eligibility = eligibilityFromEnv(env);
  try {
    const { resultFile, reasonsFile, ...fields } = parseFlags(argv);
    sink = env.THREAT_DETECTION_BOUND_RESULT_FILE || (resultFile ?? env.THREAT_DETECTION_RESULT_FILE);
    if (!sink) return failure(3, "No result sink configured");
    if (reasonsFile) {
      let entries;
      try {
        entries = readBoundedJSON(reasonsFile);
      } catch {
        throw new Error("--reasons-file must name a readable regular file containing one JSON array, at most 1 MiB");
      }
      if (!Array.isArray(entries)) throw new Error("--reasons-file must contain an array of strings");
      fields.reasons = fields.reasons.concat(entries);
    }
    if (!fields.reasons.length && CATEGORIES.some(key => fields[key])) throw new Error("At least one reason is required when any threat is true; supply --reasons-file or --reason");
    report = validateReport(fields, eligibility);
  } catch (error) {
    // Validation messages are fixed text; never echo a path, reason, or JSON parse error.
    return failure(2, error.message);
  }
  try {
    const recorded = recordSink(sink, report, eligibility);
    console.log(`THREAT_DETECTION_RESULT_RECORDED: ${recorded ? "" : "result already recorded; "}analysis complete; stop now and produce no further output.`);
    return 0;
  } catch {
    return failure(3, "Failed to record result in the configured sink");
  }
}

if (require.main === module) process.exitCode = runReport(process.argv.slice(2), process.env);
module.exports = { IneligibleReportError, runReport, validateReport, readSink, publishableResult };
