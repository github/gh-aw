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

/** @param {string} value @param {string} label @returns {number} */
function parseTimestamp(value, label) {
  if (typeof value !== "string" || !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{3})?Z$/.test(value)) {
    throw new Error(`${label} must be a UTC ISO-8601 timestamp`);
  }
  const timestamp = Date.parse(value);
  if (!Number.isFinite(timestamp)) {
    throw new Error(`${label} must be a valid timestamp`);
  }
  return timestamp;
}

/** @param {NodeJS.ProcessEnv} env @param {{createdAt?: string}} [metadata] */
function buildRunSubject(env, metadata = {}) {
  const runId = String(env.GITHUB_RUN_ID || "");
  if (!/^\d+$/.test(runId) || runId === "0") {
    throw new Error("GITHUB_RUN_ID must identify the workflow run");
  }
  if (metadata.createdAt !== undefined && metadata.createdAt !== null) {
    // Reject malformed acquisition results instead of forwarding an invalid run subject.
    parseTimestamp(metadata.createdAt, "run.createdAt");
  }
  return {
    id: runId,
    attempt: Number(env.GITHUB_RUN_ATTEMPT) || 1,
    repository: String(env.GITHUB_REPOSITORY || ""),
    workflow: String(env.GITHUB_WORKFLOW || ""),
    ref: String(env.GITHUB_REF || ""),
    sha: String(env.GITHUB_SHA || ""),
    eventName: String(env.GITHUB_EVENT_NAME || ""),
    createdAt: metadata.createdAt || null,
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

function parseOperationalValueBaselineDefinition(rawDefinition) {
  let definition;
  try {
    definition = JSON.parse(rawDefinition || "{}");
  } catch (err) {
    throw new Error(`operational-value evaluator returned an invalid definition: ${getErrorMessage(err)}`, { cause: err });
  }
  if (!isRecord(definition) || definition.schemaVersion !== 4 || definition.grader !== "operational-value" || !isRecord(definition.baseline)) {
    throw new Error("operational-value evaluator definition must use schemaVersion 4 and grader 'operational-value'");
  }
  if (definition.baseline.mode === "attainment-only") {
    if (definition.baseline.value !== null) throw new Error("attainment-only operational-value evaluators must have a null baseline value");
    return null;
  }
  if (definition.baseline.mode !== "baseline-comparable") {
    throw new Error("operational-value evaluator baseline mode must be 'baseline-comparable' or 'attainment-only'");
  }
  const baselineValue = definition.baseline.value;
  if (typeof baselineValue !== "number" || !Number.isFinite(baselineValue) || baselineValue < 0 || baselineValue > 1) {
    throw new Error("baseline-comparable operational-value evaluators require a baseline value in [0,1]");
  }
  return baselineValue;
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
 * @param {{evidenceAt?: string, env?: NodeJS.ProcessEnv, event?: object|null, case?: object|null, runMetadata?: {createdAt?: string}, bashPath?: string}} [options]
 */
function executeOperationalValueEvaluator(evaluatorContent, meta, options = {}) {
  const env = options.env || process.env;
  const syntaxCheckTimeoutMs = getSetupTimeoutMs("operationalValueSyntaxCheck", env);
  const definitionTimeoutMs = getSetupTimeoutMs("operationalValueDefinition", env);
  const gradeRunTimeoutMs = getSetupTimeoutMs("operationalValueGradeRun", env);
  const evidenceAt = options.evidenceAt || new Date().toISOString();
  const evidenceAtMs = parseTimestamp(evidenceAt, "evidenceAt");
  const run = buildRunSubject(env, options.runMetadata);
  const request = {
    schemaVersion: 1,
    run,
    evidenceAt,
    case: options.case || null,
    event: options.event === undefined ? readEventPayload(env) : options.event,
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

    const definitionExecution = executeEvaluatorSubprocess(bashPath, [evaluatorPath, "--definition"], {
      timeout: definitionTimeoutMs,
      maxBuffer: OPERATIONAL_VALUE_EVALUATOR_MAX_OUTPUT,
      env: safeFunctionEnv(env),
      operation: "operational-value evaluator --definition",
    });
    const baselineValue = parseOperationalValueBaselineDefinition(definitionExecution.stdout);

    const execution = executeEvaluatorSubprocess(bashPath, [evaluatorPath, "--grade-run"], {
      input: JSON.stringify(request),
      timeout: gradeRunTimeoutMs,
      maxBuffer: OPERATIONAL_VALUE_EVALUATOR_MAX_OUTPUT,
      env: safeFunctionEnv(env),
      operation: "operational-value evaluator",
    });

    let output;
    try {
      output = JSON.parse(execution.stdout || "{}");
    } catch (err) {
      throw new Error(`operational-value evaluator returned invalid JSON: ${getErrorMessage(err)}`, { cause: err });
    }
    if (!isRecord(output)) throw new Error("operational-value evaluator output must be an object");
    if (output.value !== null && (typeof output.value !== "number" || !Number.isFinite(output.value) || output.value < 0 || output.value > 1)) {
      throw new Error("operational-value evaluator result.value must be null or a finite number in [0,1]");
    }
    if (!isRecord(output.case)) throw new Error("operational-value evaluator output.case must be an object");
    if (typeof output.opportunityKey !== "string" || output.opportunityKey.trim() === "") {
      throw new Error("operational-value evaluator opportunityKey must be a non-empty string");
    }
    const evidenceCutoffMs = parseTimestamp(output.evidenceCutoff, "evidenceCutoff");
    const maturesAtMs = parseTimestamp(output.maturesAt, "maturesAt");
    if (evidenceCutoffMs > evidenceAtMs) throw new Error("operational-value evaluator evidenceCutoff cannot follow evidenceAt");
    if (evidenceCutoffMs > maturesAtMs) throw new Error("operational-value evaluator evidenceCutoff cannot follow maturesAt");
    if (!Array.isArray(output.provenance) || (output.value !== null && output.provenance.length === 0)) {
      throw new Error("operational-value evaluator must return provenance for a numeric value");
    }
    for (const provenance of output.provenance) {
      if (!isRecord(provenance) || !["repository", "kind", "ref"].every(key => typeof provenance[key] === "string" && provenance[key].length > 0)) {
        throw new Error("operational-value evaluator provenance entries require repository, kind, and ref");
      }
    }
    return {
      value: output.value,
      ...(typeof output.message === "string" ? { message: output.message } : {}),
      ...(isRecord(output.diagnostics) ? { diagnostics: output.diagnostics } : {}),
      observation: {
        subject: {
          type: "workflow-run",
          runId: run.id,
          attempt: run.attempt,
          repository: run.repository,
          workflow: run.workflow,
          ref: run.ref,
          sha: run.sha,
          eventName: run.eventName,
          createdAt: run.createdAt,
        },
        opportunityKey: output.opportunityKey,
        evidenceAt,
        evidenceCutoff: output.evidenceCutoff,
        maturesAt: output.maturesAt,
        mature: evidenceAtMs >= maturesAtMs,
        case: output.case,
        provenance: output.provenance,
      },
      baselineValue,
      deltaFromBaseline: typeof output.value === "number" && baselineValue !== null ? output.value - baselineValue : null,
    };
  } finally {
    fs.rmSync(tempDir, { recursive: true, force: true });
  }
}

module.exports = {
  executeOperationalValueEvaluator,
  buildRunSubject,
  readEventPayload,
  parseTimestamp,
  parseOperationalValueBaselineDefinition,
  safeFunctionEnv,
  OPERATIONAL_VALUE_EVALUATOR_TEMP_ROOT,
  OPERATIONAL_VALUE_EVALUATOR_TIMEOUT_MS,
  OPERATIONAL_VALUE_EVALUATOR_MAX_OUTPUT,
  OPERATIONAL_VALUE_EVENT_MAX_SIZE,
};
