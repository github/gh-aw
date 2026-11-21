// @ts-check

const fs = require("fs");
const path = require("path");

/**
 * Path to the experiment assignments file written by pick_experiment.cjs.
 * Contains a JSON object mapping experiment name → selected variant for the
 * current workflow run.  Example: `{"caveman":"yes","style":"detailed"}`.
 *
 * Used as the default fallback when `GH_AW_EXPERIMENT_STATE_DIR` is not set.
 * @type {string}
 */
const EXPERIMENT_ASSIGNMENTS_PATH = "/tmp/gh-aw/experiments/assignments.json";

/**
 * Read the experiment assignments written by pick_experiment.cjs.
 * Returns `null` when the file is absent (no experiments declared) or cannot
 * be parsed.  Errors are silently swallowed — this is an enrichment helper
 * and must never break the workflow.
 *
 * The path is derived from `GH_AW_EXPERIMENT_STATE_DIR` so it stays in sync
 * with pick_experiment.cjs, which writes to `<GH_AW_EXPERIMENT_STATE_DIR>/assignments.json`.
 * Falls back to {@link EXPERIMENT_ASSIGNMENTS_PATH} when the env var is absent.
 *
 * @returns {Record<string, string> | null}
 */
function readExperimentAssignments() {
  const stateDir = process.env.GH_AW_EXPERIMENT_STATE_DIR || "";
  const filePath = stateDir ? path.join(stateDir, "assignments.json") : EXPERIMENT_ASSIGNMENTS_PATH;
  try {
    const raw = fs.readFileSync(filePath, "utf8");
    const parsed = JSON.parse(raw);
    if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
      return parsed;
    }
    return null;
  } catch {
    // Fall through to environment-variable fallback.
  }

  /** @type {Record<string, string>} */
  const fromEnv = {};
  for (const [key, value] of Object.entries(process.env)) {
    if (!key.startsWith("GH_AW_EXPERIMENTS_")) {
      continue;
    }
    const name = key.substring("GH_AW_EXPERIMENTS_".length).toLowerCase();
    if (!name || typeof value !== "string" || value.length === 0) {
      continue;
    }
    fromEnv[name] = value;
  }
  if (Object.keys(fromEnv).length > 0) {
    return fromEnv;
  }
  return null;
}

module.exports = { readExperimentAssignments, EXPERIMENT_ASSIGNMENTS_PATH };
