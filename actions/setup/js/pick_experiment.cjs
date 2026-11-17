// @ts-check
/// <reference types="@actions/github-script" />

const { iterateJsonlEvents } = require("./jsonl_helpers.cjs");
/**
 * pick_experiment
 *
 * Selects A/B experiment variants for the current workflow run.
 *
 * Environment variables (set by the compiled workflow step):
 *   GH_AW_EXPERIMENT_SPEC       - JSON object mapping experiment name → variant config.
 *                                  Each value is either a legacy bare array of strings
 *                                  or a new object with a 'variants' field and optional
 *                                  metadata: weight, start_date, end_date, description, metric.
 *                                  e.g. '{"feature1":["A","B"],"style":{"variants":["concise","detailed"],"weight":[70,30]}}'
 *   GH_AW_EXPERIMENT_STATE_FILE - Absolute path to the experiment state file to read/write
 *                                  e.g. /tmp/gh-aw/experiments/state.jsonl
 *   GH_AW_EXPERIMENT_STATE_DIR  - Directory that holds the state file (created if missing)
 *                                  e.g. /tmp/gh-aw/experiments
 *
 * Algorithm:
 *   When weight is provided the variant is chosen by weighted-random selection.
 *   Otherwise the variant with the lowest invocation count is selected next (ties are
 *   broken by random selection, ensuring no variant is systematically favoured on the
 *   first run or whenever counts are equal).
 *   When start_date or end_date is provided and today falls outside that window the
 *   control variant (first variant) is used and no counter is incremented.
 */

const fs = require("fs");
const path = require("path");
const crypto = require("crypto");
const { getErrorMessage } = require("./error_helpers.cjs");

/** Maximum number of per-run ledger records retained in state.runs for summaries. */
const MAX_RUN_HISTORY = 512;
const STATE_SOURCE_FORMAT = Symbol("experimentStateSourceFormat");

/**
 * @typedef {Object} ExperimentRunRecord
 * @property {string} run_id       - GitHub Actions run ID (GITHUB_RUN_ID)
 * @property {string} timestamp    - ISO-8601 UTC timestamp of the run
 * @property {Record<string, string>} assignments - Maps experiment name → selected variant
 * @property {Record<string, Record<string, number>>} [baseline_counts]
 *   Optional cumulative counts that existed before the recorded run history began.
 * @property {string} [harness_version]
 * @property {Record<string, {current_stage: number}>} [continual_state]
 */

/**
 * @typedef {Object} ExperimentState
 * @property {Record<string, Record<string, number>>} counts
 *   Maps experiment name → variant → cumulative invocation count.
 * @property {ExperimentRunRecord[]} [runs]
 *   Per-run ledger history appended on each invocation.
 * @property {Record<string, {current_stage: number}>} [continual]
 *   Dynamic continual experiment state persisted independently of workflow configuration.
 */

/**
 * @typedef {Object} GuardrailMetric
 * @property {string} name      - Metric name (e.g. "success_rate")
 * @property {string} threshold - Comparison expression (e.g. ">=0.95")
 */

/**
 * @typedef {Object} ExperimentConfig
 * @property {string[]} variants                    - Array of variant values (length >= 2)
 * @property {number[]} [weight]                    - Optional per-variant weights (same length as variants)
 * @property {string} [start_date]                  - ISO-8601 date; inactive before this date
 * @property {string} [end_date]                    - ISO-8601 date; inactive after this date
 * @property {string} [description]
 * @property {string} [hypothesis]                  - Null and alternative hypothesis text
 * @property {string} [metric]                      - Primary metric name
 * @property {string[]} [secondary_metrics]         - Additional metrics to track
 * @property {GuardrailMetric[]} [guardrail_metrics] - Thresholds that must not degrade
 * @property {number} [min_samples]                 - Minimum runs per variant for reliable analysis
 * @property {number} [issue]
 * @property {string} [analysis_type]               - Statistical test: t_test | mann_whitney | proportion_test | bayesian_ab
 * @property {string[]} [tags]                      - Free-form labels for dashboard filtering
 * @property {{discussion?: number, issue?: number}} [notify] - Where to post significance alerts
 * @property {{seed: string, ramp: number[]}} [continual]
 */

/**
 * Normalize a raw spec entry (either a legacy bare array or the new object form) into
 * an ExperimentConfig object.
 *
 * @param {string[]|ExperimentConfig} raw
 * @returns {ExperimentConfig}
 */
function normalizeConfig(raw) {
  if (Array.isArray(raw)) {
    return { variants: raw };
  }
  return raw;
}

/**
 * @param {unknown} value
 * @returns {value is Record<string, any>}
 */
function isPlainObject(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function mergeBaselineCounts(targetCounts, baselineCounts) {
  if (!isPlainObject(baselineCounts)) {
    throw new Error("Invalid baseline counts");
  }
  for (const [name, variants] of Object.entries(baselineCounts)) {
    if (!isPlainObject(variants)) {
      throw new Error("Invalid baseline count variants");
    }
    if (!targetCounts[name]) {
      targetCounts[name] = {};
    }
    for (const [variant, count] of Object.entries(variants)) {
      if (!Number.isFinite(count)) {
        throw new Error("Invalid baseline count value");
      }
      targetCounts[name][variant] = (targetCounts[name][variant] || 0) + count;
    }
  }
}

function deriveCountsFromRuns(runs) {
  const counts = {};
  for (const run of runs) {
    if (isPlainObject(run.baseline_counts)) {
      mergeBaselineCounts(counts, run.baseline_counts);
    }
    for (const [name, variant] of Object.entries(run.assignments || {})) {
      if (!counts[name]) {
        counts[name] = {};
      }
      counts[name][variant] = (counts[name][variant] || 0) + 1;
    }
  }
  return counts;
}

function diffBaselineCounts(totalCounts, representedCounts) {
  const baselineCounts = {};
  for (const [name, variants] of Object.entries(totalCounts || {})) {
    for (const [variant, count] of Object.entries(variants || {})) {
      const represented = representedCounts[name]?.[variant] || 0;
      const delta = count - represented;
      if (delta > 0) {
        if (!baselineCounts[name]) {
          baselineCounts[name] = {};
        }
        baselineCounts[name][variant] = delta;
      }
    }
  }
  return baselineCounts;
}

function hasCounts(counts) {
  return Object.values(counts).some(variants => Object.keys(variants).length > 0);
}

/**
 * Merge persisted continual stages into target without allowing stage regression.
 *
 * @param {Record<string, {current_stage: number}>} target
 * @param {unknown} source
 * @throws {Error} When source contains invalid continual stage state.
 */
function mergeContinualState(target, source) {
  if (!isPlainObject(source)) {
    throw new Error("Invalid continual experiment state: expected an object");
  }
  for (const [name, value] of Object.entries(/** @type {Record<string, unknown>} */ source)) {
    if (!isPlainObject(value) || !Number.isInteger(value.current_stage) || value.current_stage < 0) {
      throw new Error(`Invalid continual experiment stage for "${name}": current_stage must be a non-negative integer`);
    }
    const currentStage = target[name]?.current_stage ?? 0;
    target[name] = { current_stage: Math.max(currentStage, value.current_stage) };
  }
}

/**
 * Load and parse the state file. Returns an empty state if the file does not exist
 * or cannot be parsed (e.g. first run or corrupted cache).
 *
 * @param {string} stateFile
 * @returns {ExperimentState}
 */
function loadState(stateFile) {
  try {
    const raw = fs.readFileSync(stateFile, "utf8");
    try {
      const parsed = JSON.parse(raw);
      if (parsed && typeof parsed.counts === "object") {
        if (!Array.isArray(parsed.runs)) {
          parsed.runs = [];
        }
        Object.defineProperty(parsed, STATE_SOURCE_FORMAT, { value: "json", configurable: true });
        return parsed;
      }
    } catch {
      // Fall through to JSONL state parsing.
    }

    /** @type {ExperimentState & {runs: ExperimentRunRecord[]}} */
    const state = { counts: {}, runs: [] };
    for (const event of iterateJsonlEvents(raw)) {
      if (event.type === "invalid") {
        if (event.reason === "blank") continue;
        throw event.error;
      }
      const entry = event.value;
      if (entry && typeof entry.counts === "object") {
        state.counts = entry.counts;
        state.runs = Array.isArray(entry.runs) ? entry.runs.slice(-MAX_RUN_HISTORY) : [];
        continue;
      }
      if (entry && typeof entry.run_id === "string" && typeof entry.timestamp === "string" && entry.assignments && typeof entry.assignments === "object" && !Array.isArray(entry.assignments)) {
        if (entry.baseline_counts !== undefined) {
          mergeBaselineCounts(state.counts, entry.baseline_counts);
        }
        if (entry.continual_state !== undefined) {
          if (!state.continual) state.continual = {};
          mergeContinualState(state.continual, entry.continual_state);
        }
        state.runs.push(entry);
        if (state.runs.length > MAX_RUN_HISTORY) {
          state.runs = state.runs.slice(-MAX_RUN_HISTORY);
        }
        for (const [name, variant] of Object.entries(entry.assignments)) {
          if (typeof variant !== "string") {
            throw new Error("Invalid assignment variant");
          }
          if (!state.counts[name]) {
            state.counts[name] = {};
          }
          state.counts[name][variant] = (state.counts[name][variant] || 0) + 1;
        }
        continue;
      }
      throw new Error("Invalid experiment state record");
    }
    Object.defineProperty(state, STATE_SOURCE_FORMAT, { value: "jsonl", configurable: true });
    return state;
  } catch (err) {
    // When state.jsonl is absent, fall back to state.json for cache-mode compatibility.
    if (stateFile.endsWith(".jsonl") && typeof err === "object" && err !== null && "code" in err && err.code === "ENOENT") {
      const legacyFile = stateFile.replace(/\.jsonl$/, ".json");
      return loadState(legacyFile);
    }
    // File unreadable or invalid – start fresh.
  }
  return { counts: {}, runs: [] };
}

/**
 * Persist the state file to disk.
 *
 * @param {string} stateFile
 * @param {ExperimentState} state
 */
function saveState(stateFile, state) {
  const dir = path.dirname(stateFile);
  try {
    fs.mkdirSync(dir, { recursive: true });
    if (stateFile.endsWith(".jsonl")) {
      const runs = Array.isArray(state.runs) ? state.runs.map(run => JSON.parse(JSON.stringify(run))) : [];
      if (runs.length === 0) {
        fs.writeFileSync(stateFile, "", "utf8");
        return;
      }
      if (state[STATE_SOURCE_FORMAT] === "json") {
        const baselineCounts = diffBaselineCounts(state.counts || {}, deriveCountsFromRuns(runs));
        if (hasCounts(baselineCounts)) {
          runs[0].baseline_counts = baselineCounts;
        }
        fs.writeFileSync(stateFile, `${runs.map(run => JSON.stringify(run)).join("\n")}\n`, "utf8");
        Object.defineProperty(state, STATE_SOURCE_FORMAT, { value: "jsonl", configurable: true });
        return;
      }
      // Write all bounded runs (state.runs is already limited to MAX_RUN_HISTORY) using
      // writeFileSync so the on-disk ledger never grows past MAX_RUN_HISTORY entries.
      // Any cumulative counts from records that were trimmed by MAX_RUN_HISTORY slicing
      // are preserved in runs[0].baseline_counts so no historical totals are lost.
      const derivedCounts = deriveCountsFromRuns(runs);
      const excess = diffBaselineCounts(state.counts || {}, derivedCounts);
      if (hasCounts(excess)) {
        const existingBaseline = isPlainObject(runs[0].baseline_counts) ? /** @type {Record<string, Record<string, number>>} */ runs[0].baseline_counts : {};
        /** @type {Record<string, Record<string, number>>} */
        const mergedBaseline = Object.assign({}, existingBaseline);
        for (const [name, variants] of Object.entries(excess)) {
          if (!mergedBaseline[name]) mergedBaseline[name] = {};
          for (const [variant, count] of Object.entries(variants)) {
            mergedBaseline[name][variant] = (mergedBaseline[name][variant] || 0) + count;
          }
        }
        runs[0] = Object.assign({}, runs[0], { baseline_counts: mergedBaseline });
      }
      fs.writeFileSync(stateFile, `${runs.map(run => JSON.stringify(run)).join("\n")}\n`, "utf8");
      return;
    }
    fs.writeFileSync(stateFile, JSON.stringify(state, null, 2) + "\n", "utf8");
  } catch (err) {
    throw new Error(`Failed to persist experiment state ${stateFile}: ${getErrorMessage(err)}`, { cause: err });
  }
}

/**
 * Return true when today (UTC) falls within the optional [start_date, end_date] window.
 * A missing date is treated as unbounded (open interval).
 *
 * @param {string|undefined} startDate - YYYY-MM-DD or undefined
 * @param {string|undefined} endDate   - YYYY-MM-DD or undefined
 * @param {string} [todayOverride]     - Override today's date for testing (YYYY-MM-DD)
 * @returns {boolean}
 */
function isWithinDateWindow(startDate, endDate, todayOverride) {
  const today = todayOverride || new Date().toISOString().slice(0, 10);
  if (startDate && today < startDate) {
    return false;
  }
  if (endDate && today > endDate) {
    return false;
  }
  return true;
}

/**
 * Pick the variant for one experiment using a balanced least-used selection.
 * The variant with the lowest cumulative count is chosen; when multiple variants
 * share the lowest count (including the initial empty-cache state where all counts
 * are zero), one is selected at random to avoid systematically favouring the first
 * declared variant.
 *
 * @param {string} name       - Experiment name
 * @param {string[]} variants - Array of variant values (length >= 2)
 * @param {ExperimentState} state
 * @returns {string} The selected variant
 */
function pickVariant(name, variants, state) {
  const counts = state.counts[name] || {};
  let minCount = Infinity;
  let tied = [];
  for (const variant of variants) {
    const c = counts[variant] || 0;
    if (c < minCount) {
      minCount = c;
      tied = [variant];
    } else if (c === minCount) {
      tied.push(variant);
    }
  }
  return tied[Math.floor(Math.random() * tied.length)];
}

/**
 * Pick the variant for one experiment using weighted random selection.
 * Each variant is chosen with probability proportional to its weight.
 * Zero-weight variants are never selected.
 *
 * @param {string[]} variants - Array of variant values (length >= 2)
 * @param {number[]} weight   - Per-variant weights (same length as variants, all >= 0)
 * @returns {string} The selected variant
 */
function pickVariantWeighted(variants, weight) {
  const total = weight.reduce((a, b) => a + b, 0);
  if (total <= 0) {
    // All weights are zero – fall back to first variant (control).
    return variants[0];
  }

  let rnd = Math.random() * total;
  for (let i = 0; i < variants.length; i++) {
    rnd -= weight[i];
    if (rnd <= 0) {
      return variants[i];
    }
  }
  // Floating-point rounding guard: return last non-zero-weight variant.
  for (let i = variants.length - 1; i >= 0; i--) {
    if (weight[i] > 0) return variants[i];
  }
  return variants[0];
}

/**
 * Deterministically select a weighted variant from an explicit pre-treatment assignment unit.
 *
 * @param {string[]} variants
 * @param {number[]} weight
 * @param {string} key
 * @returns {string}
 */
function pickVariantDeterministic(variants, weight, key) {
  const total = weight.reduce((a, b) => a + b, 0);
  if (total <= 0) return variants[0];
  const digest = crypto.createHash("sha256").update(key).digest();
  const bucket = digest.readBigUInt64BE(0);
  let point = Number(bucket % BigInt(total));
  for (let i = 0; i < variants.length; i++) {
    if (point < weight[i]) return variants[i];
    point -= weight[i];
  }
  return variants[0];
}

/**
 * @param {ExperimentConfig} cfg
 * @param {string[]} variants
 * @param {ExperimentState} state
 * @param {string} name
 * @returns {number[]}
 */
function continualWeights(cfg, variants, state = { counts: {} }, name = "") {
  const continual = cfg.continual;
  const ramp = continual?.ramp;
  if (!Array.isArray(ramp) || ramp.length === 0 || variants.length !== 2) {
    return cfg.weight && cfg.weight.length === variants.length ? cfg.weight : variants.map(() => 1);
  }
  const configuredMinimum = cfg.min_samples;
  let minimumObservations = 20;
  if (typeof configuredMinimum === "number" && Number.isInteger(configuredMinimum) && configuredMinimum > 0) {
    minimumObservations = configuredMinimum;
  }
  const candidateObservations = state.counts?.[name]?.[variants[1]] ?? 0;
  const observedStage = Math.min(Math.floor(candidateObservations / minimumObservations), ramp.length - 1);
  const storedStageValue = state.continual?.[name]?.current_stage;
  const storedStage = typeof storedStageValue === "number" && Number.isInteger(storedStageValue) && storedStageValue >= 0 ? Math.min(storedStageValue, ramp.length - 1) : 0;
  const stage = Math.max(storedStage, observedStage);
  if (!state.continual) state.continual = {};
  state.continual[name] = { current_stage: stage };
  const candidate = ramp[stage] ?? 0;
  return [100 - candidate, candidate];
}

function logContinualDecision(name, cfg, variants, state, weights, selected, core) {
  if (!cfg.continual) return;

  const candidateObservations = state.counts?.[name]?.[variants[1]] ?? 0;
  const allocation = variants.map((variant, index) => `${variant}=${weights[index]}`).join(", ");
  let rampStage = "no ramp configured";
  if (Array.isArray(cfg.continual.ramp) && cfg.continual.ramp.length > 0 && variants.length === 2) {
    const configuredMinimum = cfg.min_samples;
    const minimumObservations = typeof configuredMinimum === "number" && Number.isInteger(configuredMinimum) && configuredMinimum > 0 ? configuredMinimum : 20;
    const stage = state.continual?.[name]?.current_stage ?? 0;
    rampStage = `ramp stage ${stage + 1}/${cfg.continual.ramp.length} (${candidateObservations}/${minimumObservations} candidate observations)`;
  }
  core.info(`Continual experiment "${name}": assignment decision; ${rampStage}; weights ${allocation}; selected variant "${selected}"`);
}

/**
 * Increment the counter for the chosen variant.
 *
 * @param {string} name    - Experiment name
 * @param {string} variant - Chosen variant
 * @param {ExperimentState} state
 */
function recordVariant(name, variant, state) {
  if (!state.counts[name]) {
    state.counts[name] = {};
  }
  state.counts[name][variant] = (state.counts[name][variant] || 0) + 1;
}

/**
 * Append a Markdown step summary describing the experiment assignments.
 *
 * @param {Record<string, string>} assignments  - Maps experiment name → selected variant
 * @param {Record<string, ExperimentConfig>} configs - Normalized config per experiment
 * @param {ExperimentState} state               - Updated state (post-selection)
 * @param {any} core                            - @actions/core
 */
async function writeSummary(assignments, configs, state, core) {
  const names = Object.keys(assignments).sort();
  const lines = ["<details>", "<summary>Experiment Assignments</summary>", "", "| Experiment | Variant | Counts (current/total) |", "| --- | --- | --- |"];
  const detailLines = [];
  for (const name of names) {
    const selected = assignments[name];
    const counts = state.counts[name] || {};
    const thisCount = counts[selected] || 0;
    // Prefer counting actual run records for the total when the runs array is present;
    // fall back to summing incremented counts (which excludes date-window gated runs).
    const runsForExp = state.runs ? state.runs.filter(r => r.assignments && name in r.assignments) : null;
    const totalCount = runsForExp !== null && runsForExp.length > 0 ? runsForExp.length : Object.values(/** @type {number[]} */ counts).reduce((a, b) => a + b, 0);
    lines.push(`| \`${name}\` | **${selected}** | ${thisCount} / ${totalCount} |`);

    const cfg = configs[name] || {};
    const minSamplesValue = cfg.min_samples;
    const minSamples = typeof minSamplesValue === "number" && Number.isInteger(minSamplesValue) && minSamplesValue > 0 ? minSamplesValue : null;
    const analysisType = cfg.analysis_type || "n/a";
    const tags = Array.isArray(cfg.tags) ? cfg.tags.filter(t => typeof t === "string" && t.length > 0) : [];
    const notifyTargets = [];
    if (cfg.notify?.discussion) {
      notifyTargets.push(`discussion #${cfg.notify.discussion}`);
    }
    if (cfg.notify?.issue) {
      notifyTargets.push(`issue #${cfg.notify.issue}`);
    }

    detailLines.push("<details>");
    detailLines.push(`<summary>🔎 ${name} assignment metadata</summary>`);
    detailLines.push("");
    detailLines.push(`### ${name}`);
    detailLines.push("");
    detailLines.push("");
    detailLines.push("| Field | Value |");
    detailLines.push("| --- | --- |");
    detailLines.push(`| Experiment | \`${name}\` |`);
    detailLines.push(`| Assigned variant | \`${selected}\` |`);
    detailLines.push(`| Analysis type | \`${analysisType}\` |`);
    detailLines.push(`| Run count (this variant) | ${minSamples !== null ? `${thisCount} / ${minSamples} min_samples` : `${thisCount}`} |`);
    if (tags.length > 0) {
      detailLines.push(`| Tags | ${tags.map(tag => `\`${tag}\``).join(", ")} |`);
    }
    if (notifyTargets.length > 0) {
      detailLines.push(`| Notify | ${notifyTargets.join("; ")} |`);
    }
    detailLines.push("");
    detailLines.push("</details>");
    detailLines.push("");
  }
  lines.push("");

  if (detailLines.length > 0) {
    lines.push("### 📋 Assignment Details");
    lines.push("");
    lines.push(...detailLines);
  }

  // Progress bars and ready-for-analysis flags when min_samples is a positive integer.
  const progressNames = names.filter(name => {
    const ms = configs[name]?.min_samples;
    return ms != null && Number.isInteger(ms) && ms > 0;
  });
  if (progressNames.length > 0) {
    lines.push("### 📊 Sampling Progress");
    lines.push("");
    for (const name of progressNames) {
      const cfg = configs[name];
      const minSamples = cfg.min_samples ?? 0;
      const variants = cfg.variants || [];
      const counts = state.counts[name] || {};
      const allReady = variants.every(v => (counts[v] || 0) >= minSamples);
      if (allReady) {
        lines.push(`**${name}** ✅ Ready for analysis`);
      } else {
        lines.push(`**${name}** (target: ${minSamples} per variant)`);
      }
      for (const variant of variants) {
        const n = counts[variant] || 0;
        const pct = Math.min(100, Math.round((n / minSamples) * 100));
        const filled = Math.round(pct / 5); // 20-char bar
        const bar = "█".repeat(filled) + "░".repeat(20 - filled);
        lines.push(`  ${variant}: ${bar} ${n}/${minSamples} (${pct}%)`);
      }
      lines.push("");
    }
  }

  // Append optional description, hypothesis, guardrail metrics, and issue link.
  const repo = process.env.GITHUB_REPOSITORY || "";
  const metadataNames = names.filter(name => configs[name]?.description || configs[name]?.hypothesis || configs[name]?.guardrail_metrics?.length || configs[name]?.issue);
  if (metadataNames.length > 0) {
    lines.push("### Experiment Details");
    lines.push("");
    for (const name of metadataNames) {
      const cfg = configs[name];
      const description = cfg?.description;
      const hypothesis = cfg?.hypothesis;
      const guardrails = cfg?.guardrail_metrics;
      const issue = cfg?.issue;
      lines.push(`**${name}**`);
      if (description) {
        lines.push("");
        lines.push(`> ${description}`);
      }
      if (hypothesis) {
        lines.push("");
        lines.push(`**Hypothesis:** ${hypothesis}`);
      }
      if (guardrails && guardrails.length > 0) {
        lines.push("");
        lines.push("**Guardrail metrics:**");
        for (const g of guardrails) {
          lines.push(`- \`${g.name}\` ${g.threshold}`);
        }
      }
      if (issue) {
        lines.push("");
        if (repo) {
          lines.push(`Tracking issue: [#${issue}](https://github.com/${repo}/issues/${issue})`);
        } else {
          lines.push(`Tracking issue: #${issue}`);
        }
      }
      lines.push("");
    }
  }

  lines.push("_Variants are selected by balanced round-robin (or weighted) to ensure statistical relevance across runs. Ties are broken randomly so no variant is systematically favoured on the first run._");
  lines.push("");
  lines.push("</details>");
  await core.summary.addRaw(lines.join("\n")).write();
}

/**
 * Main entry point called by the actions/github-script step.
 */
async function main() {
  const specRaw = process.env.GH_AW_EXPERIMENT_SPEC || "{}";
  const stateFile = process.env.GH_AW_EXPERIMENT_STATE_FILE || require("./runtime_paths.cjs").runtimePath("experiments/state.jsonl");
  const stateDir = process.env.GH_AW_EXPERIMENT_STATE_DIR || require("./runtime_paths.cjs").runtimePath("experiments");

  /** @type {Record<string, string[]|ExperimentConfig>} */
  let rawSpec;
  try {
    rawSpec = JSON.parse(specRaw);
  } catch (e) {
    core.setFailed(`Failed to parse GH_AW_EXPERIMENT_SPEC: ${getErrorMessage(e)}`);
    return;
  }

  const experimentNames = Object.keys(rawSpec).sort();
  if (experimentNames.length === 0) {
    core.info("No experiments defined – nothing to do.");
    return;
  }

  // Normalize all spec entries to ExperimentConfig objects.
  /** @type {Record<string, ExperimentConfig>} */
  const configs = {};
  for (const name of experimentNames) {
    configs[name] = normalizeConfig(rawSpec[name]);
  }

  // Ensure the state directory exists so that the cache-save step can find it.
  try {
    fs.mkdirSync(stateDir, { recursive: true });
  } catch (err) {
    throw new Error(`Failed to create directory ${stateDir}: ${getErrorMessage(err)}`, { cause: err });
  }

  const state = loadState(stateFile);

  /** @type {Record<string, string>} */
  const assignments = {};
  for (const name of experimentNames) {
    const cfg = configs[name];
    const variants = cfg.variants;
    if (!Array.isArray(variants) || variants.length < 2) {
      core.warning(`Experiment "${name}" has fewer than 2 variants – skipping.`);
      continue;
    }

    // Date-window check: use control variant (first variant) when outside the window.
    if (!isWithinDateWindow(cfg.start_date, cfg.end_date)) {
      const control = variants[0];
      assignments[name] = control;
      core.setOutput(name, control);
      core.info(`Experiment "${name}": outside date window – using control variant "${control}"`);
      continue;
    }

    let selected;
    const weights = continualWeights(cfg, variants, state, name);
    const assignmentUnit = [name, process.env.GITHUB_REPOSITORY || "", process.env.GITHUB_WORKFLOW_REF || process.env.GITHUB_WORKFLOW || "", process.env.GITHUB_RUN_ID || ""].join(":");
    if (cfg.continual?.seed) {
      selected = pickVariantDeterministic(variants, weights, `${cfg.continual.seed}:${assignmentUnit}`);
    } else if (cfg.weight && cfg.weight.length === variants.length) {
      selected = pickVariantWeighted(variants, cfg.weight);
    } else {
      selected = pickVariant(name, variants, state);
    }
    logContinualDecision(name, cfg, variants, state, weights, selected, core);
    recordVariant(name, selected, state);
    assignments[name] = selected;
    // Expose the selected variant as a step output (individual per experiment).
    // Downstream jobs access this via needs.activation.outputs.<name>.
    core.setOutput(name, selected);
    core.info(`Experiment "${name}": selected variant "${selected}" (output: ${name}=${selected})`);
  }

  // Expose the full assignments map as a serialized JSON step output.
  // Downstream jobs access this via needs.activation.outputs.experiments.
  const experimentsJSON = JSON.stringify(assignments);
  core.setOutput("experiments", experimentsJSON);
  core.info(`Experiment assignments (JSON): ${experimentsJSON}`);

  if (Object.keys(assignments).length > 0) {
    // Append a per-run record to state.runs so each assignment is traceable.
    const runId = process.env.GITHUB_RUN_ID || "";
    const timestamp = new Date().toISOString();
    if (!state.runs) {
      state.runs = [];
    }
    state.runs.push({
      run_id: runId,
      timestamp,
      harness_version: process.env.GH_AW_HARNESS_VERSION || "",
      assignments: { ...assignments },
      continual_state: state.continual ? structuredClone(state.continual) : undefined,
    });
    // Prune in-memory run history so summaries stay small even when state.jsonl is append-only.
    if (state.runs.length > MAX_RUN_HISTORY) {
      state.runs = state.runs.slice(-MAX_RUN_HISTORY);
    }
  }

  // Persist updated counts and run history.
  saveState(stateFile, state);
  core.info(`Experiment state written to ${stateFile}`);

  // Persist current-run assignments to a separate file so downstream jobs and
  // OTLP telemetry can read which variant was selected without recomputing it.
  // Only written when at least one experiment was successfully assigned.
  if (Object.keys(assignments).length > 0) {
    const assignmentsFile = path.join(stateDir, "assignments.json");
    try {
      fs.writeFileSync(assignmentsFile, JSON.stringify(assignments, null, 2) + "\n", "utf8");
    } catch (err) {
      throw new Error(`Failed to write file ${assignmentsFile}: ${getErrorMessage(err)}`, { cause: err });
    }
    core.info(`Experiment assignments written to ${assignmentsFile}`);

    // Emit OTEL resource attributes so every span in this run carries the
    // experiment assignments for filtering in Honeycomb/Grafana.
    const otelAttrs = Object.entries(assignments)
      .map(([name, variant]) => `experiment.${name}=${variant}`)
      .join(",");
    const existingAttrs = process.env.OTEL_RESOURCE_ATTRIBUTES || "";
    core.exportVariable("OTEL_RESOURCE_ATTRIBUTES", existingAttrs ? `${existingAttrs},${otelAttrs}` : otelAttrs);
    core.info(`OTEL resource attributes set: ${otelAttrs}`);
  }

  // Write step summary.
  await writeSummary(assignments, configs, state, core);
}

module.exports = {
  main,
  pickVariant,
  pickVariantWeighted,
  pickVariantDeterministic,
  continualWeights,
  loadState,
  saveState,
  recordVariant,
  isWithinDateWindow,
  normalizeConfig,
};
