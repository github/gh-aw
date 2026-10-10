// @ts-check
require("./shim.cjs");

/**
 * evaluate_outcomes.cjs
 *
 * Evaluates safe output outcomes for recent successful workflow runs.
 * Replaces the shell-based evaluation logic in the outcome-collector workflow.
 *
 * Responsibilities:
 * - Load previously evaluated run IDs from cache-memory
 * - Fetch recent successful runs via `gh run list`
 * - Download safe-outputs-items artifacts via `gh run download`
 * - Classify each item (accepted/rejected/pending/noop) using the GitHub API
 * - Extract time-to-resolution, PR quality signals, pending age
 * - Write per-item evaluations to outcome-evaluations.jsonl
 * - Compute and write fleet summary to outcome-summary.json
 * - Update the seen-runs cache
 *
 * Outputs:
 *   /tmp/gh-aw/outcome-evaluations.jsonl  — per-item JSONL
 *   /tmp/gh-aw/outcome-summary.json       — fleet summary
 *   /tmp/gh-aw/outcomes/run-*.json        — per-run data
 *
 * Errors in individual run/item evaluation are non-fatal and logged to stderr.
 */

const fs = require("fs");
const path = require("path");
const { evaluateAction } = require("./outcome_action_evaluators.cjs");
const { execFileSync } = require("child_process");
const { getSetupTimeoutMs } = require("./child_process_timeouts.cjs");

// ---------------------------------------------------------------------------
// Paths
// ---------------------------------------------------------------------------
const CACHE_DIR = "/tmp/gh-aw/cache-memory/outcome-collector";
const SEEN_FILE = path.join(CACHE_DIR, "seen-runs.json");
const OUTCOMES_DIR = "/tmp/gh-aw/outcomes";
const EVAL_JSONL = "/tmp/gh-aw/outcome-evaluations.jsonl";
const SUMMARY_PATH = "/tmp/gh-aw/outcome-summary.json";

// ---------------------------------------------------------------------------
// Noop types that are tracked but not counted as actionable
// ---------------------------------------------------------------------------
const NOOP_TYPES = new Set(["noop", "missing_tool", "missing_data", "report_incomplete"]);
const GH_COMMAND_TIMEOUT_MS = getSetupTimeoutMs("outcomeGh");

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/**
 * Run a `gh` CLI command, returning stdout as a string.
 * Returns null on failure.
 * @param {string[]} args
 * @returns {string | null}
 */
function gh(args) {
  try {
    return execFileSync("gh", args, { encoding: "utf8", stdio: ["pipe", "pipe", "pipe"], timeout: GH_COMMAND_TIMEOUT_MS }).trim();
  } catch (error) {
    console.warn(`Outcome GitHub command failed: ${String(error)}`);
    return null;
  }
}

/**
 * Run a paginated `gh api` call, preserving failures for outcome classification.
 * @param {string} endpoint
 * @returns {any | null}
 */
function ghAPI(endpoint) {
  let raw;
  try {
    raw = execFileSync("gh", ["api", endpoint, "--paginate", "--slurp"], { encoding: "utf8", stdio: ["pipe", "pipe", "pipe"], timeout: GH_COMMAND_TIMEOUT_MS });
  } catch (error) {
    const message = error && typeof error === "object" && "stderr" in error ? String(error.stderr) : String(error);
    const match = message.match(/HTTP (\d{3})/);
    throw Object.assign(new Error(`GitHub outcome API failed: ${endpoint}: ${message}`), { status: match ? Number(match[1]) : null });
  }
  const pages = JSON.parse(raw);
  if (!Array.isArray(pages) || pages.length === 0) throw new Error(`Invalid GitHub outcome response: ${endpoint}`);
  return Array.isArray(pages[0]) ? pages.flat() : pages[0];
}

/**
 * Read a JSON file, returning a default value on failure.
 * @param {string} filePath
 * @param {any} fallback
 * @returns {any}
 */
function readJSON(filePath, fallback) {
  try {
    return JSON.parse(fs.readFileSync(filePath, "utf8"));
  } catch {
    return fallback;
  }
}

/**
 * Read a JSONL file, returning an array of parsed objects.
 * @param {string} filePath
 * @returns {any[]}
 */
function readJSONL(filePath) {
  try {
    return fs
      .readFileSync(filePath, "utf8")
      .split("\n")
      .filter(l => l.trim())
      .map(l => {
        try {
          return JSON.parse(l);
        } catch {
          return null;
        }
      })
      .filter(Boolean);
  } catch {
    return [];
  }
}

/**
 * Atomically write JSON to a file using a tmp+rename swap.
 * @param {string} filePath
 * @param {any} data
 */
function writeJSONAtomic(filePath, data) {
  const tmp = filePath + ".tmp";
  try {
    fs.writeFileSync(tmp, JSON.stringify(data, null, 2) + "\n");
  } catch (err) {
    throw new Error(`Failed to write file ${tmp}: ${String(err)}`, { cause: err });
  }
  try {
    fs.renameSync(tmp, filePath);
  } catch (err) {
    throw new Error(`Failed to rename file ${tmp} to ${filePath}: ${String(err)}`, { cause: err });
  }
}

/**
 * Parse an ISO-8601 timestamp to epoch seconds. Returns null on failure.
 * @param {string} ts
 * @returns {number | null}
 */
function isoToEpoch(ts) {
  if (!ts) return null;
  const ms = Date.parse(ts);
  return Number.isFinite(ms) ? Math.floor(ms / 1000) : null;
}

/**
 * Compute seconds between two ISO timestamps. Returns null if either is invalid.
 * @param {string} from
 * @param {string} to
 * @returns {number | null}
 */
function secondsBetween(from, to) {
  const a = isoToEpoch(from);
  const b = isoToEpoch(to);
  if (a === null || b === null) return null;
  return b - a;
}

/**
 * Normalize legacy result/detail pairs into the shared outcome model.
 * @param {string} result
 * @param {string} detail
 * @returns {{ outcome_status: string, evidence_strength: string, signal: string }}
 */
function normalizeOutcome(result, detail) {
  const normalizedDetail = String(detail || "")
    .toLowerCase()
    .trim();

  const signals = {
    completed: "strong",
    closed_not_planned: "strong",
    lifecycle: "medium",
    lifecycle_close: "medium",
    acted_on: "medium",
    closed_without_merge: "strong",
    reopened: "strong",
    closed_by_merge: "strong",
    state_retained: "medium",
    state_reverted: "strong",
    state_replaced: "strong",
    missing_reference: "none",
    unsupported_evaluator: "none",
    workflow_success: "strong",
    workflow_failed: "strong",
    workflow_no_effect: "medium",
    workflow_pending: "medium",
    missing_run_id: "none",
  };
  if (normalizedDetail in signals) {
    return { outcome_status: result, evidence_strength: signals[normalizedDetail], signal: normalizedDetail };
  }
  if (normalizedDetail === "no action-specific evaluator") {
    return { outcome_status: "unknown", evidence_strength: "none", signal: "unsupported_evaluator" };
  }
  if (result === "error") {
    return { outcome_status: "error", evidence_strength: "weak", signal: "evaluation_error" };
  }

  if (result === "noop") {
    return { outcome_status: "skipped", evidence_strength: "none", signal: "noop" };
  }
  if (normalizedDetail === "object still exists") {
    return { outcome_status: "unknown", evidence_strength: "weak", signal: "target_exists_only" };
  }
  if (normalizedDetail === "review approved") {
    return { outcome_status: "accepted", evidence_strength: "strong", signal: "review_approved" };
  }
  if (normalizedDetail === "review submitted") {
    return { outcome_status: "accepted", evidence_strength: "medium", signal: "review_submitted" };
  }
  if (normalizedDetail === "review request removed") {
    return { outcome_status: "rejected", evidence_strength: "strong", signal: "review_request_removed" };
  }
  if (normalizedDetail === "review dismissed") {
    return { outcome_status: "rejected", evidence_strength: "strong", signal: "review_dismissed" };
  }
  if (normalizedDetail === "changes requested addressed and merged") {
    return { outcome_status: "accepted", evidence_strength: "medium", signal: "changes_requested_addressed" };
  }
  if (normalizedDetail === "closed without merge after review") {
    return { outcome_status: "rejected", evidence_strength: "medium", signal: "closed_without_merge_after_review" };
  }
  if (normalizedDetail === "latest review awaiting outcome") {
    return { outcome_status: "pending", evidence_strength: "medium", signal: "latest_review_pending" };
  }
  if (normalizedDetail === "awaiting review") {
    return { outcome_status: "pending", evidence_strength: "medium", signal: "awaiting_review" };
  }
  if (normalizedDetail === "update retained and merged") {
    return { outcome_status: "accepted", evidence_strength: "strong", signal: "state_retained_and_merged" };
  }
  if (normalizedDetail === "update retained") {
    return { outcome_status: "accepted", evidence_strength: "medium", signal: "state_retained" };
  }
  if (normalizedDetail === "update reverted") {
    return { outcome_status: "rejected", evidence_strength: "strong", signal: "state_reverted" };
  }
  if (normalizedDetail === "update replaced") {
    return { outcome_status: "rejected", evidence_strength: "strong", signal: "state_replaced" };
  }
  if (normalizedDetail === "missing execution state") {
    return { outcome_status: "unknown", evidence_strength: "none", signal: "missing_execution_state" };
  }
  if (normalizedDetail === "no persisted state delta") {
    return { outcome_status: "unknown", evidence_strength: "none", signal: "no_state_delta" };
  }
  if (result === "accepted" && normalizedDetail.startsWith("merged")) {
    return { outcome_status: "accepted", evidence_strength: "strong", signal: "merged" };
  }
  if (result === "accepted" && normalizedDetail === "closed") {
    return { outcome_status: "accepted", evidence_strength: "strong", signal: "closed" };
  }
  if (result === "rejected" && normalizedDetail === "closed") {
    return { outcome_status: "rejected", evidence_strength: "strong", signal: "closed" };
  }
  if (result === "rejected" && normalizedDetail === "merged") {
    return { outcome_status: "rejected", evidence_strength: "strong", signal: "closed_by_merge" };
  }
  if (result === "rejected" && normalizedDetail === "not_closed") {
    return { outcome_status: "rejected", evidence_strength: "strong", signal: "not_closed" };
  }
  if (result === "pending" && normalizedDetail === "open") {
    return { outcome_status: "pending", evidence_strength: "medium", signal: "open" };
  }
  switch (result) {
    case "accepted":
      return { outcome_status: "accepted", evidence_strength: "medium", signal: "acted_on" };
    case "rejected":
      return { outcome_status: "rejected", evidence_strength: "medium", signal: "rejected" };
    case "ignored":
      return { outcome_status: "ignored", evidence_strength: "medium", signal: "ignored" };
    case "pending":
      return { outcome_status: "pending", evidence_strength: "medium", signal: "pending" };
    default:
      return { outcome_status: "unknown", evidence_strength: "weak", signal: "unknown" };
  }
}

/**
 * @param {any} item
 * @param {string} defaultRepo
 * @param {((endpoint: string) => any) | {ghAPI?: (endpoint: string) => any, nowMs?: number}} [apiOrOptions]
 */
function evaluateItem(item, defaultRepo, apiOrOptions) {
  const api = typeof apiOrOptions === "function" ? apiOrOptions : apiOrOptions?.ghAPI || ghAPI;
  const now = typeof apiOrOptions === "object" && typeof apiOrOptions.nowMs === "number" ? apiOrOptions.nowMs : Date.now();
  return evaluateAction(item, defaultRepo, api, now, normalizeOutcome);
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

function main() {
  const repo = process.env.GITHUB_REPOSITORY || "";
  if (!repo) {
    console.error("GITHUB_REPOSITORY is not set");
    process.exit(1);
  }

  // Ensure directories exist
  try {
    fs.mkdirSync(CACHE_DIR, { recursive: true });
    fs.mkdirSync(OUTCOMES_DIR, { recursive: true });
  } catch (err) {
    throw new Error(`Failed to create evaluation directories: ${String(err)}`, { cause: err });
  }

  // Load seen-runs cache
  const seenIds = new Set(readJSON(SEEN_FILE, []));

  // Fetch recent successful runs
  const runsRaw = gh(["run", "list", "--repo", repo, "--limit", "200", "--json", "databaseId,conclusion,workflowName,event", "--jq", '[.[] | select(.conclusion == "success")] | .[0:150]']);

  if (!runsRaw || runsRaw === "[]" || runsRaw === "null") {
    core.info("No recent successful runs found");
    writeJSONAtomic(SUMMARY_PATH, { runs_checked: 0, total_outcomes: 0 });
    process.exit(0);
  }

  /** @type {Array<{databaseId: number, workflowName: string, event: string}>} */
  let runs;
  try {
    runs = JSON.parse(runsRaw);
  } catch {
    console.error("Failed to parse run list");
    writeJSONAtomic(SUMMARY_PATH, { runs_checked: 0, total_outcomes: 0 });
    process.exit(0);
  }

  // Counters
  let checked = 0;
  let accepted = 0;
  let rejected = 0;
  let ignored = 0;
  let pending = 0;
  let unknown = 0;
  let errors = 0;
  let lifecycle = 0;
  let total = 0;
  let noop = 0;
  let zeroTouchCount = 0;
  let acceptedStrong = 0;
  let acceptedMedium = 0;
  let acceptedWeak = 0;
  let fallbackExistsOnlyCount = 0;
  /** @type {number[]} */
  const resolutionTimes = [];

  // Clear the evaluations file
  try {
    fs.writeFileSync(EVAL_JSONL, "");
  } catch (err) {
    throw new Error(`Failed to write file ${EVAL_JSONL}: ${String(err)}`, { cause: err });
  }

  /** @type {number[]} */
  const evaluatedIds = [];

  for (const run of runs) {
    const runId = run.databaseId;
    const workflow = run.workflowName || "";
    const event = run.event || "";

    // Skip previously evaluated
    if (seenIds.has(runId)) continue;

    // Download artifact
    const itemDir = path.join(OUTCOMES_DIR, `run-${runId}`);
    const dlResult = gh(["run", "download", String(runId), "--repo", repo, "--name", "safe-outputs-items", "--dir", itemDir]);
    if (dlResult === null) continue;

    const manifestPath = path.join(itemDir, "safe-output-items.jsonl");
    if (!fs.existsSync(manifestPath)) continue;

    const manifest = readJSONL(manifestPath);
    if (manifest.length === 0) continue;

    // Separate actionable items from noops
    const actionable = manifest.filter(m => m.type && !NOOP_TYPES.has(m.type));
    const noops = manifest.filter(m => m.type && NOOP_TYPES.has(m.type));
    const runNoops = noops.length;
    const runItems = actionable.length;

    if (runItems === 0 && runNoops === 0) continue;

    noop += runNoops;

    core.info(`Run ${runId} (${workflow}): ${runItems} item(s), ${runNoops} noop(s) [trigger: ${event}]`);
    checked++;
    total += runItems;

    // Write noop entries
    for (const n of noops) {
      const normalized = normalizeOutcome("noop", n.type || "");
      try {
        fs.appendFileSync(
          EVAL_JSONL,
          JSON.stringify({
            type: n.type,
            url: "",
            repo,
            result: "noop",
            outcome_status: normalized.outcome_status,
            evidence_strength: normalized.evidence_strength,
            signal: normalized.signal,
            detail: n.type,
            workflow,
            run_id: runId,
            timestamp: "",
            event,
          }) + "\n"
        );
      } catch (err) {
        throw new Error(`Failed to append to file ${EVAL_JSONL}: ${String(err)}`, { cause: err });
      }
    }

    if (runItems === 0) {
      // Only noops — still mark as evaluated
      writeJSONAtomic(path.join(OUTCOMES_DIR, `run-${runId}.json`), {
        workflow,
        run_id: runId,
        items: 0,
        noops: runNoops,
        event,
      });
      evaluatedIds.push(runId);
      continue;
    }

    // Evaluate each actionable item
    for (const item of actionable) {
      const evalResult = evaluateItem(item, repo);
      const normalized = evalResult;

      switch (normalized.outcome_status) {
        case "accepted":
          accepted++;
          switch (normalized.evidence_strength) {
            case "strong":
              acceptedStrong++;
              break;
            case "medium":
              acceptedMedium++;
              break;
            case "weak":
              acceptedWeak++;
              break;
          }
          if (evalResult.zero_touch === true) {
            zeroTouchCount++;
          }
          break;
        case "rejected":
          rejected++;
          break;
        case "ignored":
          ignored++;
          break;
        case "pending":
          pending++;
          break;
        case "unknown":
          unknown++;
          break;
        case "error":
          errors++;
          break;
        case "lifecycle":
        case "lifecycle_close":
          lifecycle++;
          break;
      }
      if (normalized.signal === "target_exists_only") {
        fallbackExistsOnlyCount++;
      }
      if (typeof evalResult.resolution_sec === "number" && evalResult.resolution_sec > 0) {
        resolutionTimes.push(evalResult.resolution_sec);
      }

      try {
        fs.appendFileSync(
          EVAL_JSONL,
          JSON.stringify({
            type: item.type || "",
            url: item.url || "",
            repo: item.repo || repo,
            result: evalResult.result,
            outcome_status: normalized.outcome_status,
            evidence_strength: normalized.evidence_strength,
            signal: normalized.signal,
            detail: evalResult.detail,
            workflow,
            run_id: runId,
            timestamp: item.timestamp || "",
            event,
            resolution_sec: evalResult.resolution_sec,
            pending_age_sec: evalResult.pending_age_sec,
            review_comments: evalResult.review_comments,
            changed_files: evalResult.changed_files,
            additions: evalResult.additions,
            deletions: evalResult.deletions,
            reactions_total: evalResult.reactions_total,
            reactions_positive: evalResult.reactions_positive,
            reactions_negative: evalResult.reactions_negative,
            comments: evalResult.comments,
            human_comments: evalResult.human_comments,
            human_reviews: evalResult.human_reviews,
            human_edits: evalResult.human_edits,
            zero_touch: evalResult.zero_touch || false,
          }) + "\n"
        );
      } catch (err) {
        throw new Error(`Failed to append to file ${EVAL_JSONL}: ${String(err)}`, { cause: err });
      }
    }

    // Save per-run data
    writeJSONAtomic(path.join(OUTCOMES_DIR, `run-${runId}.json`), {
      workflow,
      run_id: runId,
      items: runItems,
      noops: runNoops,
      event,
    });

    evaluatedIds.push(runId);
  }

  // Compute fleet summary
  const resolved = accepted + rejected;
  const acceptanceRate = resolved > 0 ? accepted / resolved : 0;
  const wasteRate = total > 0 ? rejected / total : 0;
  const noopRate = total + noop > 0 ? noop / (total + noop) : 0;

  // Economics: zero-touch rate and median time-to-outcome
  const zeroTouchRate = accepted > 0 ? zeroTouchCount / accepted : 0;
  resolutionTimes.sort((a, b) => a - b);
  /** @type {any} */
  let medianResolutionSec = null;
  if (resolutionTimes.length > 0) {
    const mid = Math.floor(resolutionTimes.length / 2);
    medianResolutionSec = resolutionTimes.length % 2 !== 0 ? resolutionTimes[mid] : Math.round((resolutionTimes[mid - 1] + resolutionTimes[mid]) / 2);
  }

  writeJSONAtomic(SUMMARY_PATH, {
    runs_checked: checked,
    total_outcomes: total,
    accepted,
    rejected,
    ignored,
    pending,
    unknown,
    errors,
    lifecycle,
    noop,
    accepted_strong: acceptedStrong,
    accepted_medium: acceptedMedium,
    accepted_weak: acceptedWeak,
    fallback_exists_only_count: fallbackExistsOnlyCount,
    acceptance_rate: Math.round(acceptanceRate * 10000) / 10000,
    waste_rate: Math.round(wasteRate * 10000) / 10000,
    noop_rate: Math.round(noopRate * 10000) / 10000,
    zero_touch: zeroTouchCount,
    zero_touch_rate: Math.round(zeroTouchRate * 10000) / 10000,
    median_resolution_sec: medianResolutionSec,
    date: new Date().toISOString().slice(0, 10),
  });

  // Update seen-runs cache: merge old + new, keep last 500
  const merged = [...new Set([...seenIds, ...evaluatedIds])].sort((a, b) => a - b).slice(-500);
  writeJSONAtomic(SEEN_FILE, merged);

  core.info(`✓ Checked ${checked} runs, ${total} outcomes`);
  core.info(`  Accepted: ${accepted}, Rejected: ${rejected}, Ignored: ${ignored}, Pending: ${pending}, Noop: ${noop}`);
  core.info(`  Acceptance rate: ${acceptanceRate.toFixed(4)}`);
  core.info(JSON.stringify(readJSON(SUMMARY_PATH, {}), null, 2));
}

if (require.main === module) {
  main();
}

module.exports = { main, evaluateItem, normalizeOutcome, readJSONL, secondsBetween, isoToEpoch, ghAPI };
