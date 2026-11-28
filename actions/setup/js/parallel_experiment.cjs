// @ts-check

const fs = require("fs");
const path = require("path");
const { iterateJsonlEvents } = require("./jsonl_helpers.cjs");
const { readExperimentAssignments, readParallelExperimentAssignment } = require("./experiment_helpers.cjs");
const { collectUnifiedSession, writeUnifiedSession } = require("./unified_session.cjs");
const { parseTokenUsageJsonl } = require("./parse_mcp_gateway_log.cjs");
const { collectArtifactSecretValues, redactManifestValue } = require("./safe_output_manifest.cjs");
const { collectAddMaskedValues, redactMaskedValues } = require("./add_mask_redaction.cjs");
const { redactArtifactContent } = require("./redact_secrets.cjs");
const { redactSensitiveConfig } = require("./safe_outputs_config_redact.cjs");

const EVIDENCE_PATHS = [
  "agent-stdio.log",
  "agent-session.jsonl",
  "agent_usage.json",
  "agent_usage.jsonl",
  "aw_info.json",
  "runtime/aw_info.json",
  "awf-config.json",
  "awf-resolved-config.json",
  "sandbox/firewall/audit/awf-resolved-config.json",
  "agent/model-resolution.json",
  "agent/harness_retry.json",
  "aw-prompts/user.txt",
  "aw-prompts/system.txt",
  "sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl",
  "sandbox/firewall/audit/api-proxy-logs/token-usage.jsonl",
  "sandbox/firewall-audit-logs/api-proxy-logs/token-usage.jsonl",
  "safeoutputs/config.json",
  "safeoutputs/validation.json",
  "safeoutputs/outputs.ndjson",
  "safeoutputs.jsonl",
  "safe-outputs.jsonl",
  "otel.jsonl",
  "otel-metrics.jsonl",
  "otlp-export-errors.jsonl",
  "otlp-export-errors.count",
];

/** Read only local regular evidence files, never symlinks (including ancestors). */
function readEvidence(root, relative) {
  const target = path.resolve(root, relative);
  if (!target.startsWith(path.resolve(root) + path.sep)) throw new Error("Invalid parallel experiment evidence path");
  let current = path.parse(target).root;
  try {
    for (const part of target.slice(current.length).split(path.sep)) {
      current = path.join(current, part);
      if (fs.lstatSync(current).isSymbolicLink()) return null;
    }
    return fs.lstatSync(target).isFile() ? fs.readFileSync(target, "utf8") : null;
  } catch (error) {
    if (error.code === "ENOENT") return null;
    throw error;
  }
}

function readJSON(root, relative) {
  const raw = readEvidence(root, relative);
  if (raw === null) return null;
  try {
    return JSON.parse(raw);
  } catch {
    return null;
  }
}

function ensureOutput(file) {
  const directory = path.resolve(path.dirname(file));
  let current = path.parse(directory).root;
  for (const part of directory.slice(current.length).split(path.sep)) {
    current = path.join(current, part);
    if (!fs.existsSync(current)) fs.mkdirSync(current);
    if (fs.lstatSync(current).isSymbolicLink()) throw new Error("Parallel experiment output must not follow symlinks");
  }
  if (fs.existsSync(file) && fs.lstatSync(file).isSymbolicLink()) throw new Error("Parallel experiment output must not follow symlinks");
}

/** Every newly generated artifact is independently redacted before publication. */
function writeJSON(file, value, masks = []) {
  const redacted = redactManifestValue(value, artifactSecretValues());
  const content = JSON.stringify(redacted, (_key, item) => (typeof item === "string" ? redactMaskedValues(item, masks) : item), 2) + "\n";
  writeContent(file, content);
}

function writeContent(file, content) {
  ensureOutput(file);
  const partial = `${file}.partial`;
  try {
    fs.writeFileSync(partial, content, { mode: 0o600, flag: "wx" });
    fs.renameSync(partial, file);
  } catch (error) {
    if (error.code !== "EEXIST") fs.rmSync(partial, { force: true });
    throw error;
  }
}

function artifactSecretValues() {
  return [
    ...collectArtifactSecretValues(),
    ...Object.entries(process.env)
      .filter(([name, value]) => name.startsWith("GH_AW_DYNAMIC_SECRET_VALUE_") && value)
      .map(([, value]) => value || ""),
  ];
}

function removeOutput(file) {
  ensureOutput(file);
  fs.rmSync(file, { force: true });
}

/** Bundle only known local files; apply the same artifact and config redactors. */
function collectEvidence(root, output, masks) {
  const evidence = {};
  const secretValues = artifactSecretValues();
  for (const source of EVIDENCE_PATHS) {
    const destination = path.join("evidence", source + (source.endsWith(".ndjson") ? ".jsonl" : source.endsWith(".count") ? ".txt" : ""));
    let raw = readEvidence(root, source);
    if (raw === null) {
      removeOutput(path.join(output, destination));
      evidence[source] = null;
      continue;
    }
    if (/(?:config|validation|aw_info)\.json$/.test(source)) {
      try {
        raw = JSON.stringify(redactSensitiveConfig(JSON.parse(raw)), null, 2) + "\n";
      } catch {
        removeOutput(path.join(output, destination));
        evidence[source] = null;
        continue;
      }
    }
    writeContent(path.join(output, destination), redactArtifactContent(raw, secretValues, masks).content);
    evidence[source] = destination;
  }
  return evidence;
}

function measurement(value) {
  return typeof value === "number" && Number.isFinite(value) && value >= 0 ? value : null;
}

/** Reuse proxy accounting (including deduplication and unknown-counter semantics). */
function collectUsage(root) {
  const missingSources = [];
  for (const source of ["sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl", "sandbox/firewall/audit/api-proxy-logs/token-usage.jsonl", "sandbox/firewall-audit-logs/api-proxy-logs/token-usage.jsonl"]) {
    const raw = readEvidence(root, source);
    if (raw === null) continue;
    const summary = parseTokenUsageJsonl(raw);
    if (!summary) {
      missingSources.push(source);
      continue;
    }
    const costAvailable =
      summary.aiCreditsAvailable && (summary.aiCreditsSource === "awf_reported" || ["input_tokens", "output_tokens", "cache_read_tokens", "cache_write_tokens"].every(key => measurement(summary.tokenUsage[key]) !== null));
    return {
      source,
      aic: costAvailable ? measurement(summary.totalAIC) : null,
      input_tokens: measurement(summary?.tokenUsage.input_tokens),
      output_tokens: measurement(summary?.tokenUsage.output_tokens),
      cache_read_tokens: measurement(summary?.tokenUsage.cache_read_tokens),
      cache_write_tokens: measurement(summary?.tokenUsage.cache_write_tokens),
      aic_source: costAvailable ? summary.aiCreditsSource : null,
      warnings: [...missingSources.map(source => `No usage evidence in ${source}`), ...(summary?.aiCreditsWarnings ?? [])],
    };
  }
  const usage = readJSON(root, "agent_usage.json");
  return {
    source: usage ? "agent_usage.json" : null,
    aic: measurement(usage?.ai_credits),
    input_tokens: measurement(usage?.input_tokens),
    output_tokens: measurement(usage?.output_tokens),
    cache_read_tokens: measurement(usage?.cache_read_tokens),
    cache_write_tokens: measurement(usage?.cache_write_tokens),
    aic_source: usage && measurement(usage.ai_credits) !== null ? "agent_usage" : null,
    warnings: missingSources.map(source => `No usage evidence in ${source}`),
  };
}

/** @param {NodeJS.ProcessEnv} env */
function configuration(env) {
  const assignment = readParallelExperimentAssignment(env);
  const config = JSON.parse(env.GH_AW_PARALLEL_EXPERIMENT || "null");
  if (!assignment || !config || typeof config.id !== "string" || !/^[a-zA-Z0-9_-]+$/.test(config.id)) throw new Error("Invalid parallel experiment configuration");
  return {
    id: config.id,
    ...assignment,
    engine: typeof config.engine === "string" ? config.engine : null,
    model: typeof config.model === "string" ? config.model : null,
    harness: typeof config.harness === "string" ? config.harness : null,
    max_ai_credits: measurement(config.max_ai_credits),
    timeout_minutes: measurement(config.timeout_minutes),
    admitted: typeof config.admitted === "boolean" ? config.admitted : true,
    skip_reason: typeof config.skip_reason === "string" ? config.skip_reason : null,
  };
}

/** Admission is compiler evidence, not proof that another matrix job executed. */
function admissionLedger(env) {
  if (!env.GH_AW_PARALLEL_ADMISSION) return null;
  const entries = JSON.parse(env.GH_AW_PARALLEL_ADMISSION);
  if (!Array.isArray(entries) || entries.length > 16) throw new Error("Invalid parallel experiment admission ledger");
  const rows = entries.map(entry => {
    const config = configuration({ ...env, GH_AW_PARALLEL_EXPERIMENT: JSON.stringify(entry) });
    return { ...config, state: config.admitted ? "admitted" : "skipped", executed: null, completed: null, failed: null };
  });
  return { configured: rows.length, admitted: rows.filter(row => row.admitted).length, skipped: rows.filter(row => !row.admitted).length, entries: rows };
}

/**
 * prepare runs after activation restoration; start/end bracket the execution
 * shell, not the workflow run or artifact collection. finalize never runs graders.
 * @param {string} mode
 * @param {{env?: NodeJS.ProcessEnv, rootDir?: string, outputDir?: string, now?: () => Date}} [options]
 */
function main(mode, { env = process.env, rootDir = env.GH_AW_TMP_DIR || "/tmp/gh-aw", outputDir = env.GH_AW_PARALLEL_EXPERIMENT_DIR || path.join(rootDir, "experiments"), now = () => new Date() } = {}) {
  const config = configuration(env);
  const manifestPath = path.join(outputDir, "parallel.json");
  const previous = readJSON(outputDir, "parallel.json");
  const identity = { run_id: env.GITHUB_RUN_ID ?? null, run_attempt: env.GITHUB_RUN_ATTEMPT ?? null, job: env.GITHUB_JOB ?? null, matrix_id: config.id };
  if (previous && JSON.stringify(previous.identity) !== JSON.stringify(identity)) throw new Error("Parallel experiment identity mismatch");
  const observedAt = now().toISOString();
  /** @type {Record<string, any>} */
  const manifest = previous ?? {
    version: 1,
    identity,
    configured: config,
    resolved: { engine: null, model: null, harness: null },
    configured_at: observedAt,
    admitted: config.admitted ? null : false,
    state: "configured",
    executed: false,
    failed: false,
    completed: false,
    skipped: !config.admitted,
    execution_started_at: null,
    execution_ended_at: null,
    missing_evidence: [],
  };
  manifest.admission = admissionLedger(env);
  if (mode === "prepare") {
    manifest.admitted = config.admitted && (!env.GH_AW_ADMISSION_RESULT || env.GH_AW_ADMISSION_RESULT === "admitted");
    manifest.state = manifest.admitted ? "admitted" : "skipped";
    manifest.skipped = !manifest.admitted;
    manifest.assignments = { ...(readExperimentAssignments(env) ?? {}), [config.experiment]: config.variant };
    writeJSON(path.join(rootDir, "experiments", "assignments.json"), manifest.assignments);
    if (env.GH_AW_EXPERIMENT_STATE_DIR && path.resolve(env.GH_AW_EXPERIMENT_STATE_DIR) !== path.resolve(rootDir, "experiments")) {
      writeJSON(path.join(env.GH_AW_EXPERIMENT_STATE_DIR, "assignments.json"), manifest.assignments);
    }
    const info = readJSON(rootDir, "aw_info.json");
    if (info && typeof info === "object" && !Array.isArray(info)) {
      writeJSON(path.join(rootDir, "aw_info.json"), {
        ...info,
        engine_id: config.engine ?? info.engine_id,
        model: config.model ?? info.model,
        engine_harness: config.harness ?? info.engine_harness,
        parallel_experiment: { id: config.id, experiment: config.experiment, variant: config.variant },
      });
    }
    if (env.GITHUB_ENV) {
      ensureOutput(env.GITHUB_ENV);
      const descriptor = fs.openSync(env.GITHUB_ENV, fs.constants.O_WRONLY | fs.constants.O_APPEND | fs.constants.O_CREAT | fs.constants.O_NOFOLLOW, 0o600);
      try {
        fs.appendFileSync(descriptor, `GH_AW_EXPERIMENTS_${config.experiment.toUpperCase()}=${config.variant}\n`);
      } finally {
        fs.closeSync(descriptor);
      }
    }
  } else if (mode === "start") {
    if (!manifest.admitted) throw new Error("Parallel experiment execution requires admission");
    manifest.execution_started_at ??= observedAt;
    manifest.executed = true;
    manifest.state = "executed";
  } else if (mode === "end") {
    if (manifest.execution_started_at) manifest.execution_ended_at ??= observedAt;
  } else if (mode === "finalize") {
    const steps = JSON.parse(env.GH_AW_PARALLEL_STEPS || "{}");
    const outcomes = Object.fromEntries(Object.entries(steps).map(([id, step]) => [id, { outcome: step?.outcome ?? null, conclusion: step?.conclusion ?? null }]));
    manifest.outcomes = outcomes;
    manifest.failed = Object.values(outcomes).some(step => step.outcome === "failure" || step.outcome === "cancelled");
    const executionId = `${config.id.replace(/-/g, "_")}_agentic_execution`;
    const executedSteps = Object.entries(outcomes).filter(([id]) => id === executionId || (!config.id.startsWith("entry-") && /execute.*agent|agent.*execute|agentic_execution$/.test(id)));
    manifest.executed ||= executedSteps.some(([, step]) => ["success", "failure", "cancelled"].includes(step.outcome));
    manifest.completed = manifest.executed && !manifest.failed && executedSteps.some(([, step]) => step.outcome === "success");
    manifest.skipped = !manifest.executed && (manifest.admitted === false || executedSteps.some(([, step]) => step.outcome === "skipped"));
    manifest.state = manifest.failed ? "failed" : manifest.completed ? "completed" : manifest.skipped ? "skipped" : manifest.executed ? "executed" : manifest.state;
    manifest.finalized_at = observedAt;
    manifest.usage = collectUsage(rootDir);
    manifest.missing_evidence = [];
    if (!manifest.admission) manifest.missing_evidence.push("admission_ledger");
    if (!manifest.execution_started_at) manifest.missing_evidence.push("execution_start");
    if (!manifest.execution_ended_at) manifest.missing_evidence.push("execution_end");
    for (const key of ["aic", "input_tokens", "output_tokens", "cache_read_tokens", "cache_write_tokens"]) {
      if (manifest.usage[key] === null) manifest.missing_evidence.push(key);
    }
    const masks = [];
    for (const relative of [...EVIDENCE_PATHS, "agent_output.json"]) {
      const raw = readEvidence(rootDir, relative);
      if (raw) masks.push(...collectAddMaskedValues(raw));
    }
    try {
      masks.push(...collectUnifiedSession({ rootDir, engine: config.engine ?? undefined, admission: {}, warn: () => {} }).maskedValues);
      ensureOutput(path.join(outputDir, "aw_session.jsonl"));
      const events = writeUnifiedSession({
        rootDir,
        outputPath: path.join(outputDir, "aw_session.jsonl"),
        engine: config.engine ?? undefined,
        executionIdentity: { runId: env.GITHUB_RUN_ID, runAttempt: Number(env.GITHUB_RUN_ATTEMPT) },
        admission: {},
        warn: () => {},
      });
      const sessionText = readEvidence(outputDir, "aw_session.jsonl");
      if (sessionText === null) throw new Error("Unified session publication is unavailable");
      writeContent(path.join(outputDir, "aw_session.jsonl"), redactArtifactContent(sessionText, artifactSecretValues(), masks).content);
      manifest.session = "aw_session.jsonl";
      const resolution = events.filter(event => event.type === "model.resolution" && (event.data.role === "main" || event.data.role === "agent")).at(-1);
      const info = readJSON(rootDir, "runtime/aw_info.json");
      const observedModel = readJSON(rootDir, "agent_usage.json")?.primary_model;
      manifest.resolved = {
        engine: resolution?.data.engine ?? info?.engine_id ?? null,
        model: resolution ? (resolution.data.resolved_model ?? null) : typeof observedModel === "string" && observedModel ? observedModel : null,
        harness: info?.engine_harness ?? null,
      };
      if (!events.some(event => event.provenance?.component === "agent")) manifest.missing_evidence.push("agent_session");
      if (events.some(event => event.type === "session.collection_warning")) manifest.missing_evidence.push("session_collection_warnings");
    } catch {
      removeOutput(path.join(outputDir, "aw_session.jsonl"));
      manifest.session = null;
      manifest.missing_evidence.push("unified_session");
    }
    if (manifest.resolved.model === null) manifest.missing_evidence.push("resolved_model");
    let staged = readJSON(rootDir, "agent_output.json");
    if (staged === null) {
      for (const source of ["safeoutputs/outputs.ndjson", "safeoutputs.jsonl", "safe-outputs.jsonl"]) {
        const raw = readEvidence(rootDir, source);
        if (raw === null) continue;
        const items = [];
        let invalid = 0;
        for (const event of iterateJsonlEvents(raw)) {
          if (event.type === "json") items.push(event.value);
          else if (event.reason !== "blank") invalid++;
        }
        staged = { source, items, validated: false, malformed_records: invalid };
        if (invalid) manifest.missing_evidence.push("partial_staged_safe_outputs");
        break;
      }
    }
    manifest.staged_safe_outputs = staged === null ? null : "staged-safe-outputs.json";
    if (staged !== null) writeJSON(path.join(outputDir, "staged-safe-outputs.json"), staged, masks);
    else {
      removeOutput(path.join(outputDir, "staged-safe-outputs.json"));
      manifest.missing_evidence.push("staged_safe_outputs");
    }
    manifest.evidence = collectEvidence(rootDir, outputDir, masks);
    manifest.configuration_evidence = {
      configured: manifest.evidence["awf-config.json"],
      resolved: manifest.evidence["awf-resolved-config.json"] ?? manifest.evidence["sandbox/firewall/audit/awf-resolved-config.json"],
      tools: manifest.evidence["safeoutputs/config.json"],
      validation: manifest.evidence["safeoutputs/validation.json"],
    };
    if (!manifest.configuration_evidence.resolved) manifest.missing_evidence.push("resolved_configuration");
    writeJSON(manifestPath, manifest, masks);
    return manifest;
  } else {
    throw new Error("Expected parallel experiment mode prepare, start, end, or finalize");
  }
  writeJSON(manifestPath, manifest);
  return manifest;
}

if (require.main === module) {
  try {
    main(process.argv[2]);
  } catch {
    console.error("Parallel experiment collection failed");
    process.exitCode = 1;
  }
}

module.exports = { main, collectUsage };
