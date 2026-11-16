// @ts-check

const fs = require("fs");
const os = require("os");
const path = require("path");
const { spawnSync } = require("child_process");

const EVALUATOR_TIMEOUT_MS = 120000;
const EVALUATOR_MAX_OUTPUT = 1024 * 1024;

/** @param {any} metrics @returns {{id: string, value: number|null}[]} */
function validateOperationalValueMetrics(metrics) {
  if (!Array.isArray(metrics) || metrics.length === 0) throw new Error("operational-value evaluator must return a nonempty metric array");
  const ids = new Set();
  for (const metric of metrics) {
    if (
      !metric ||
      typeof metric !== "object" ||
      Object.keys(metric).sort().join(",") !== "id,value" ||
      typeof metric.id !== "string" ||
      !metric.id.trim() ||
      ids.has(metric.id) ||
      (metric.value !== null && (typeof metric.value !== "number" || !Number.isFinite(metric.value)))
    ) {
      throw new Error("operational-value metrics must contain unique nonempty ids and finite numeric or null values");
    }
    ids.add(metric.id);
  }
  return metrics;
}

/**
 * Repository-owned shell compatibility boundary, not a sandbox. MicroVM
 * isolation is future work; never call this module from conclusion.
 * @param {string} source
 * @param {import("./types/unified_session").UnifiedSession} session
 * @param {Record<string, any>} config
 * @param {{timeout?: number, temporaryRoot?: string}} [options]
 * @returns {{id: string, value: number|null}[]}
 */
function executeOperationalValueEvaluator(source, session, config, options = {}) {
  const directory = fs.mkdtempSync(path.join(options.temporaryRoot ?? os.tmpdir(), "gh-aw-operational-value-"));
  try {
    const sessionPath = path.join(directory, "aw_session.jsonl");
    const scriptPath = path.join(directory, "evaluator.sh");
    fs.writeFileSync(sessionPath, session.map(event => JSON.stringify(event)).join("\n") + (session.length ? "\n" : ""), { mode: 0o400 });
    fs.writeFileSync(scriptPath, source, { mode: 0o400 });
    const timeout = options.timeout ?? EVALUATOR_TIMEOUT_MS;
    const execution = spawnSync(process.execPath, [path.join(__dirname, "operational_value_worker.cjs"), scriptPath, sessionPath, String(timeout)], {
      cwd: directory,
      input: "",
      encoding: "utf8",
      env: {
        PATH: process.env.PATH ?? "/usr/bin:/bin",
        LC_ALL: "C",
        GH_AW_SESSION_FILE: sessionPath,
        GH_AW_GRADER_CONFIG: JSON.stringify(config),
      },
      timeout: timeout + 2000,
      maxBuffer: EVALUATOR_MAX_OUTPUT,
      killSignal: "SIGKILL",
    });
    if (execution.error) throw new Error("operational-value evaluator failed or exceeded its time/output limit", { cause: execution.error });
    if (execution.signal || execution.status !== 0) {
      if (/exceeded its (?:time|output) limit/.test(execution.stderr)) throw new Error("operational-value evaluator exceeded its time/output limit");
      throw new Error(`operational-value evaluator failed (${execution.signal ?? execution.status})`);
    }
    let metrics;
    try {
      metrics = JSON.parse(execution.stdout);
    } catch (error) {
      throw new Error("operational-value evaluator returned invalid JSON", { cause: error });
    }
    return validateOperationalValueMetrics(metrics);
  } finally {
    fs.rmSync(directory, { recursive: true, force: true });
  }
}

module.exports = { executeOperationalValueEvaluator, validateOperationalValueMetrics };
