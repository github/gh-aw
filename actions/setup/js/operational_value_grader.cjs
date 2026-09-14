// @ts-check

const cp = require("child_process");
const fs = require("fs");
const path = require("path");
const { getSetupTimeoutMs } = require("./child_process_timeouts.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");

const OPERATIONAL_VALUE_EVALUATOR_TIMEOUT_MS = 120000;
const OPERATIONAL_VALUE_EVALUATOR_MAX_OUTPUT = 1024 * 1024;
const OPERATIONAL_VALUE_EVENT_MAX_SIZE = 1024 * 1024;
const OPERATIONAL_VALUE_EVALUATOR_TEMP_ROOT = "/tmp/gh-aw/agent";

/** @param {unknown} value @returns {value is Record<string, any>} */
function isRecord(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

/** @param {NodeJS.ProcessEnv} env */
function buildRunSubject(env) {
  const runId = String(env.GITHUB_RUN_ID || "");
  if (!/^\d+$/.test(runId) || runId === "0") {
    throw new Error("GITHUB_RUN_ID must identify the workflow run");
  }
  return {
    id: runId,
    attempt: Number(env.GITHUB_RUN_ATTEMPT) || 1,
    repository: String(env.GITHUB_REPOSITORY || ""),
    workflow: String(env.GITHUB_WORKFLOW || ""),
    ref: String(env.GITHUB_REF || ""),
    sha: String(env.GITHUB_SHA || ""),
    eventName: String(env.GITHUB_EVENT_NAME || ""),
  };
}

/** @param {NodeJS.ProcessEnv} env */
function readEventPayload(env) {
  const eventPath = env.GITHUB_EVENT_PATH;
  if (!eventPath) return null;
  try {
    const stat = fs.statSync(eventPath);
    if (!stat.isFile() || stat.size > OPERATIONAL_VALUE_EVENT_MAX_SIZE) return null;
    const event = JSON.parse(fs.readFileSync(eventPath, "utf8"));
    return isRecord(event) ? event : null;
  } catch {
    return null;
  }
}

/** @param {NodeJS.ProcessEnv} env */
function safeFunctionEnv(env) {
  /** @type {NodeJS.ProcessEnv} */
  const result = {};
  for (const key of ["PATH", "HOME", "TMPDIR", "TEMP", "TMP", "SystemRoot", "ComSpec", "GH_TOKEN", "GH_HOST", "GITHUB_API_URL", "GITHUB_GRAPHQL_URL", "GITHUB_SERVER_URL"]) {
    if (env[key]) result[key] = env[key];
  }
  return result;
}

/**
 * Execute a subprocess and throw resilient action-specific errors for timeout,
 * signal termination, and non-zero exits.
 * @param {string} bashPath
 * @param {string[]} args
 * @param {{input?: string, timeout: number, maxBuffer?: number, env: NodeJS.ProcessEnv, operation: string}} options
 */
function executeEvaluatorSubprocess(bashPath, args, options) {
  const execution = cp.spawnSync(bashPath, args, {
    input: options.input,
    encoding: "utf8",
    timeout: options.timeout,
    maxBuffer: options.maxBuffer,
    env: options.env,
  });
  if (execution.error) {
    if (typeof execution.error === "object" && execution.error !== null && "code" in execution.error && execution.error.code === "ETIMEDOUT") {
      throw new Error(`${options.operation} timed out after ${String(options.timeout)}ms`);
    }
    throw execution.error;
  }
  if (execution.signal) {
    throw new Error(`${options.operation} was terminated by signal ${execution.signal}`);
  }
  if (execution.status !== 0) {
    throw new Error(execution.stderr?.trim() || `${options.operation} exited with status ${String(execution.status)}`);
  }
  return execution;
}

/**
 * Execute and validate one trusted, frozen operational-value evaluator.
 * @param {string} evaluatorContent
 * @param {{digest?: string, config?: object}} meta
 * @param {{env?: NodeJS.ProcessEnv, event?: object|null, outputs?: any[], bashPath?: string}} [options]
 * @returns {{id: string, value: number|null}[]}
 */
function executeOperationalValueEvaluator(evaluatorContent, meta, options = {}) {
  const env = options.env || process.env;
  const syntaxCheckTimeoutMs = getSetupTimeoutMs("operationalValueSyntaxCheck", env);
  const evaluatorTimeoutMs = getSetupTimeoutMs("operationalValueEvaluator", env);
  const run = buildRunSubject(env);
  const event = options.event === undefined ? readEventPayload(env) : options.event;
  const request = {
    schemaVersion: 1,
    run,
    event: isRecord(event) ? event : {},
    outputs: Array.isArray(options.outputs) ? options.outputs : [],
    config: meta.config || {},
  };

  fs.mkdirSync(OPERATIONAL_VALUE_EVALUATOR_TEMP_ROOT, { recursive: true, mode: 0o700 });
  const tempDir = fs.mkdtempSync(path.join(OPERATIONAL_VALUE_EVALUATOR_TEMP_ROOT, "operational-value-grader-"));
  const evaluatorPath = path.join(tempDir, "operational-value.sh");
  const bashPath = options.bashPath || "/bin/bash";
  try {
    fs.writeFileSync(evaluatorPath, evaluatorContent, { encoding: "utf8", mode: 0o700 });
    try {
      executeEvaluatorSubprocess(bashPath, ["-n", evaluatorPath], {
        timeout: syntaxCheckTimeoutMs,
        env: safeFunctionEnv(env),
        operation: "operational-value evaluator syntax check",
      });
    } catch (err) {
      throw new Error(`operational-value evaluator has invalid Bash syntax: ${getErrorMessage(err)}`, { cause: err });
    }

    const execution = executeEvaluatorSubprocess(bashPath, [evaluatorPath], {
      input: JSON.stringify(request),
      timeout: evaluatorTimeoutMs,
      maxBuffer: OPERATIONAL_VALUE_EVALUATOR_MAX_OUTPUT,
      env: safeFunctionEnv(env),
      operation: "operational-value evaluator",
    });

    let output;
    try {
      output = JSON.parse(execution.stdout || "null");
    } catch (err) {
      throw new Error(`operational-value evaluator returned invalid JSON: ${getErrorMessage(err)}`, { cause: err });
    }
    if (!Array.isArray(output) || output.length === 0) {
      throw new Error("operational-value evaluator output must be a non-empty array");
    }
    const ids = new Set();
    for (const metric of output) {
      if (!isRecord(metric) || Object.keys(metric).sort().join(",") !== "id,value") {
        throw new Error("operational-value evaluator metrics must contain exactly id and value");
      }
      if (typeof metric.id !== "string" || metric.id.trim() === "" || ids.has(metric.id)) {
        throw new Error("operational-value evaluator metric ids must be non-empty and unique");
      }
      if (metric.value !== null && (typeof metric.value !== "number" || !Number.isFinite(metric.value))) {
        throw new Error("operational-value evaluator metric values must be null or finite numbers");
      }
      ids.add(metric.id);
    }
    return output;
  } finally {
    fs.rmSync(tempDir, { recursive: true, force: true });
  }
}

module.exports = {
  executeOperationalValueEvaluator,
  buildRunSubject,
  readEventPayload,
  safeFunctionEnv,
  OPERATIONAL_VALUE_EVALUATOR_TEMP_ROOT,
  OPERATIONAL_VALUE_EVALUATOR_TIMEOUT_MS,
  OPERATIONAL_VALUE_EVALUATOR_MAX_OUTPUT,
  OPERATIONAL_VALUE_EVENT_MAX_SIZE,
};
