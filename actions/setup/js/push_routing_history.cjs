// @ts-check
// @safe-outputs-exempt SEC-005: the job writes only its own workflow's history branch in the current repository, taken from the GitHub Actions context.
"use strict";

/**
 * The push_routing_history job: records one finished run in its workflow's adaptive-routing history,
 * state.json and runs.jsonl on memory/adaptive-routing/<workflow> (routing_history.cjs).
 *
 * The job runs after the conclusion job has published the run's unified session. It reads the
 * routing verdict, the pair AWF selected and what the run cost from that session, charges earlier
 * runs whose own save never landed, and saves through repo-memory's save loop. When another run
 * moved the branch first, the loop re-applies this run to the newest files, so concurrent runs all
 * land and none counts twice.
 */

const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { ERR_API, ERR_CONFIG, ERR_SYSTEM, ERR_VALIDATION } = require("./error_codes.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { getGitAuthEnv } = require("./git_auth_helpers.cjs");
const { execGitSync } = require("./git_helpers.cjs");
const { readJsonlFile } = require("./jsonl_helpers.cjs");
const { pushRepoMemoryChangesWithRetry } = require("./push_repo_memory.cjs");
const { attributeRequest, routedRequests } = require("./routing_analytics.cjs");
const { BOUNDS, applyRun, emptyState, legacyReconciliationFloor, trimRuns } = require("./routing_history.cjs");

/** @typedef {import("./routing_history.cjs").HistoryState} HistoryState */

const BRANCH_PREFIX = "memory/adaptive-routing/";
const STATE_FILE = "state.json";
const RUNS_FILE = "runs.jsonl";
const DEFAULT_SESSION_FILE = "/tmp/gh-aw/usage/aw_session.jsonl";
const EMPTY_TREE = "4b825dc642cb6eb9a060e54bf8d69288fbee4904";
const CLASSIFIER_PURPOSE = "routing_classification";
// The job that runs this script, as the jobs API names it.
const WRITER_JOB = "push_routing_history";
const LIST_RUNS_PER_PAGE = 100;
// GitHub Actions lists only the newest 1,000 runs that match a query, so page 10 is the last readable page.
const MAX_LIST_PAGE = 10;
// Per-stream limits, separate from bootstrap timestamp probes and exhaustive gap corroboration.
// New windows open only within the first MAX_SEARCH_CALLS, leaving calls for the return walk.
const MAX_SEARCH_CALLS = 16;
const MAX_LIST_CALLS = 24;
const MAX_BOOTSTRAP_CORROBORATION_CALLS = 128;
const MAX_NEW_INSPECTION_CALLS = 64;
const MAX_DIRECT_UNRESOLVED_LOOKUPS = 10;
const MAX_BOUNDARY_CHECKS = 4;
// Gaps whose higher listed neighbour is within 5 minutes of the scan can still reflect GitHub
// Actions run-list lag; older gaps bracketed by an atomic unfiltered page are treated as deleted.
const RECENT_GAP_MS = 5 * 60 * 1000;
const LEVELS = new Set(["economy", "balanced", "robust"]);
const MODES = new Set(["observe"]);
const SAVED_CONCLUSIONS = new Set(["success", "skipped", "neutral"]);
const OUTCOMES = new Set(["success", "failure", "skip"]);
const SKIP_REASONS = new Set(["cancelled", "infrastructure", "rate_limited", "pair_mismatch", "unknown_evidence"]);
// AWF's provider keys, written as contract 1 names them.
/** @type {Record<string, string>} */
const PROVIDER_NAMES = { copilot: "github-copilot" };
const WORKFLOW_ID = /^[A-Za-z0-9][A-Za-z0-9_-]*(?:\.[A-Za-z0-9_-]+)*$/;
const SCOPE_PART = /^[a-z0-9][a-z0-9-]*$/;
const BOT_IDENTITY = ["-c", "user.name=github-actions[bot]", "-c", "user.email=41898282+github-actions[bot]@users.noreply.github.com"];

/** @param {unknown} value @returns {value is Record<string, any>} */
const isRecord = value => value !== null && typeof value === "object" && !Array.isArray(value);

/** Seconds precision, as the history's examples show. */
const timestamp = () => new Date().toISOString().replace(/\.\d{3}Z$/, "Z");

/**
 * The job's compiled configuration.
 * @param {Record<string, string | undefined>} env
 */
function readConfig(env) {
  const workflow = env.GH_AW_ROUTING_WORKFLOW || "";
  if (!WORKFLOW_ID.test(workflow) || workflow.length > 100 || workflow.endsWith(".lock")) throw new Error(`${ERR_CONFIG}: GH_AW_ROUTING_WORKFLOW is not a workflow ID: ${JSON.stringify(workflow)}`);
  const generation = env.GH_AW_ROUTING_GENERATION || "";
  if (!/^[0-9a-f]{64}:[0-9a-f]{64}$/.test(generation)) throw new Error(`${ERR_CONFIG}: GH_AW_ROUTING_GENERATION is not a workflow definition hash`);
  const mode = env.GH_AW_ROUTING_MODE || "";
  if (!MODES.has(mode)) throw new Error(`${ERR_CONFIG}: GH_AW_ROUTING_MODE must be observe, not ${JSON.stringify(mode)}`);
  const engine = env.GH_AW_ROUTING_ENGINE || "";
  const goal = env.GH_AW_ROUTING_GOAL || "";
  if (!SCOPE_PART.test(engine) || !SCOPE_PART.test(goal)) throw new Error(`${ERR_CONFIG}: GH_AW_ROUTING_ENGINE and GH_AW_ROUTING_GOAL must name the routing scope`);
  // A level chosen at run time arrives resolved; anything else shares records across levels.
  const requestedLevel = env.GH_AW_ROUTING_LEVEL || "";
  const level = LEVELS.has(requestedLevel) ? requestedLevel : "shared";
  let subAgentModels;
  if (env.GH_AW_INFO_SUB_AGENT_MODELS) {
    try {
      subAgentModels = JSON.parse(env.GH_AW_INFO_SUB_AGENT_MODELS);
    } catch (error) {
      throw new Error(`${ERR_CONFIG}: GH_AW_INFO_SUB_AGENT_MODELS is not JSON: ${getErrorMessage(error)}`, { cause: error });
    }
    if (!Array.isArray(subAgentModels)) throw new Error(`${ERR_CONFIG}: GH_AW_INFO_SUB_AGENT_MODELS must be an array`);
  }
  return { workflow, generation, mode, scope: `${engine}/${goal}/${level}`, branch: BRANCH_PREFIX + workflow, sessionFile: env.GH_AW_ROUTING_SESSION_FILE || DEFAULT_SESSION_FILE, subAgentModels };
}

/**
 * Why this run must not update the history, or "" when it may: only first attempts whose workflow
 * definition comes from the default branch are recorded.
 * @param {{attempt: number, workflowRef: string, defaultBranch: string}} run
 */
function ineligibility({ attempt, workflowRef, defaultBranch }) {
  if (attempt !== 1) return `attempt ${attempt} is a re-run; only first attempts are recorded`;
  const ref = workflowRef.slice(workflowRef.lastIndexOf("@") + 1);
  if (!defaultBranch || ref !== `refs/heads/${defaultBranch}`) return `the workflow definition comes from ${ref || "an unknown ref"}, not the default branch`;
  return "";
}

/**
 * The run's unified session. A missing file means the usage artifact never arrived, which fails the
 * job, so the next save can charge the run as unsaved.
 * @param {string} file
 */
function readSession(file) {
  if (!fs.existsSync(file)) throw new Error(`${ERR_SYSTEM}: The run's unified session is missing (${file}); the usage artifact was not published`);
  return [...readJsonlFile(file, { strict: true })];
}

/**
 * What the run's session says about it: the routing verdict (contract 4), the pair AWF selected
 * (contract 1) and the run's cost from AWF's records. Only agent-phase AWF records count; threat
 * detection's are not the agent's.
 * @param {unknown[]} events
 * @param {unknown} [subAgentModels] the compiled sub-agent model declarations, as aw_info records them
 */
function sessionEvidence(events, subAgentModels) {
  /**
   * @param {string} type
   * @returns {Record<string, any>[]}
   */
  const agentRecords = type => events.flatMap(event => (isRecord(event) && event.type === type && isRecord(event.data) && event.provenance?.component === "firewall" && event.provenance?.phase === "agent" ? [event.data] : []));
  // T-UAS-094: only the conclusion job's collector emits the verdict; any other source is dropped.
  const verdict = events.filter(event => isRecord(event) && event.type === "routing.observation" && isRecord(event.data) && event.provenance?.component === "collector" && event.provenance?.phase === "conclusion").at(-1);
  // routing_analytics attributes AWF's records by their raw field names.
  /** @type {Record<string, any>[]} */
  const routing = agentRecords("firewall.model_routing").map(record => ({ ...record, request_id: record.requestId, requested_model: record.requestedModel }));
  const usage = agentRecords("firewall.token_usage");
  const selection = routing.filter(record => record.stage === "selection").at(-1);
  let observation = isRecord(verdict) ? validObservation(verdict.data) : null;
  const pair = selection ? pairName(selection) : null;
  // A verdict on a pair the records do not name cannot count for any pair.
  if (observation && !pair) observation = { outcome: "skip", reason: "unknown_evidence" };
  return { observation, pair, cost: runCost(usage, routing, subAgentModels) };
}

/** @param {unknown} data */
function validObservation(data) {
  if (!isRecord(data) || !OUTCOMES.has(data.outcome)) return { outcome: "skip", reason: "unknown_evidence" };
  if (data.outcome !== "skip") return { outcome: data.outcome };
  return { outcome: "skip", reason: SKIP_REASONS.has(data.reason) ? data.reason : "unknown_evidence" };
}

/**
 * `<provider>/<wire model>@<effort>`, as AWF names the pair it selected.
 * @param {Record<string, any>} selection
 */
function pairName(selection) {
  const provider = typeof selection.selectedProvider === "string" && selection.selectedProvider ? selection.selectedProvider : selection.provider;
  const model = selection.wireModel;
  if (typeof provider !== "string" || !provider || typeof model !== "string" || !model) return null;
  const effort = typeof selection.selectedEffort === "string" && selection.selectedEffort ? selection.selectedEffort : "none";
  // Own keys only: an inherited name such as `constructor` is an ordinary provider key.
  const name = Object.hasOwn(PROVIDER_NAMES, provider) ? PROVIDER_NAMES[provider] : provider;
  return `${name}/${model}@${effort}`;
}

/** @param {unknown} value @returns {number | null} */
const count = value => (typeof value === "number" && Number.isFinite(value) && value >= 0 ? value : null);

/**
 * One main request's counts as contract 4 defines them: input excluding cache, cache read, cache
 * write and output. AWF's records keep each provider's own input count, which includes cache for
 * some responses and not others. The record's flag says which when AWF provides it. Otherwise
 * Anthropic counts exclude cache, OpenAI counts include cache, and for Copilot (or any other
 * provider) an input count smaller than the cache cannot include it while any other count is taken
 * to include it, which undercounts only a request whose new input exceeds its cached input.
 * @param {Record<string, any>} used the record's usage
 * @param {unknown} [provider] the record's provider
 * @returns {number[] | null}
 */
function requestTokens(used, provider) {
  const input = count(used.inputTokens);
  const cacheRead = count(used.cacheReadInputTokens);
  const cacheWrite = count(used.cacheCreationInputTokens);
  const output = count(used.outputTokens);
  if (input === null || cacheRead === null || cacheWrite === null || output === null) return null;
  const cached = cacheRead + cacheWrite;
  const includesCache = typeof used.inputTokensIncludeCache === "boolean" ? used.inputTokensIncludeCache : provider === "anthropic" ? false : provider === "openai" ? true : input >= cached;
  // As AWF's credit meter reads an input count that includes cache.
  return [includesCache ? Math.max(0, input - cached) : input, cacheRead, cacheWrite, output];
}

/**
 * The run's cost from AWF's records (contract 4): `credits` over every agent request except the
 * router's classifier call, and `model_credits` and token counts over the main conversation's
 * requests. `credits` is null when any covered request lacks a price, `model_credits` is null
 * whenever `credits` is null or there are no main requests, and `tokens` is null when a main
 * request lacks a count.
 * @param {Record<string, any>[]} usage firewall.token_usage data
 * @param {Record<string, any>[]} routing firewall.model_routing data with raw request fields
 * @param {unknown} subAgentModels
 */
function runCost(usage, routing, subAgentModels) {
  const routes = routedRequests(routing);
  const classifier = new Set([...routes].flatMap(([id, route]) => (route?.purpose === CLASSIFIER_PURPOSE ? [id] : [])));
  const seen = new Set();
  /** @type {number | null} */
  let credits = 0;
  /** @type {number | null} */
  let modelCredits = 0;
  /** @type {number[] | null} */
  let tokens = [0, 0, 0, 0];
  /** @type {number | null} */
  let reasoning = 0;
  let requests = 0;
  let main = 0;
  for (const record of usage) {
    const id = typeof record.requestId === "string" && record.requestId ? record.requestId : null;
    if (record.purpose === CLASSIFIER_PURPOSE || (id && classifier.has(id))) continue;
    // Count each proxy request once, even when it appears in replicated logs.
    if (id && seen.has(id)) continue;
    if (id) seen.add(id);
    requests++;
    const aic = count(record.aic);
    credits = credits === null || aic === null ? null : credits + aic;
    if (!id || attributeRequest(routes.get(id), record.provider, subAgentModels) !== "main") continue;
    main++;
    modelCredits = modelCredits === null || aic === null ? null : modelCredits + aic;
    const used = isRecord(record.usage) ? record.usage : {};
    const counts = requestTokens(used, record.provider);
    if (tokens === null || counts === null) {
      tokens = null;
      continue;
    }
    const totals = tokens;
    tokens = counts.map((value, index) => totals[index] + value);
    const reasoned = count(used.reasoningOutputTokens);
    reasoning = reasoning === null || reasoned === null ? null : reasoning + reasoned;
  }
  const round = (/** @type {number | null} */ value) => (value === null ? null : Number(value.toFixed(6)));
  const totalCredits = requests > 0 ? round(credits) : null;
  return {
    credits: totalCredits,
    model_credits: totalCredits !== null && main > 0 ? round(modelCredits) : null,
    tokens: main > 0 && tokens !== null ? [...tokens, reasoning] : null,
  };
}

/**
 * A missing branch is an empty history (contract 5).
 * @param {string} workspaceDir
 * @param {string} workflow
 * @returns {{state: HistoryState, runs: any[]}}
 */
function readHistory(workspaceDir, workflow) {
  const statePath = path.join(workspaceDir, STATE_FILE);
  const runsPath = path.join(workspaceDir, RUNS_FILE);
  /** @type {HistoryState} */
  let state = emptyState(workflow);
  if (fs.existsSync(statePath)) {
    try {
      state = JSON.parse(fs.readFileSync(statePath, "utf8"));
    } catch (error) {
      throw new Error(`${ERR_VALIDATION}: ${STATE_FILE} is not readable JSON: ${getErrorMessage(error)}`, { cause: error });
    }
  }
  if (!isRecord(state) || state.v !== 2 || state.workflow !== workflow || !isRecord(state.scopes)) {
    throw new Error(`${ERR_VALIDATION}: ${STATE_FILE} is not a version 2 routing history for ${workflow}`);
  }
  const runs = fs.existsSync(runsPath) ? [...readJsonlFile(runsPath, { strict: true })] : [];
  return { state, runs };
}

/**
 * @param {string} workspaceDir
 * @param {HistoryState} state
 * @param {any[]} runs
 * @param {string} runId
 */
function commitHistory(workspaceDir, state, runs, runId) {
  try {
    fs.writeFileSync(path.join(workspaceDir, STATE_FILE), `${JSON.stringify(state)}\n`);
    fs.writeFileSync(path.join(workspaceDir, RUNS_FILE), runs.map(line => `${JSON.stringify(line)}\n`).join(""));
  } catch (error) {
    throw new Error(`${ERR_SYSTEM}: Could not write the routing history: ${getErrorMessage(error)}`, { cause: error });
  }
  execGitSync(["add", "--", STATE_FILE, RUNS_FILE], { cwd: workspaceDir, stdio: "pipe" });
  execGitSync([...BOT_IDENTITY, "commit", "-m", `Record workflow run ${runId} in the routing history`], { cwd: workspaceDir, stdio: "pipe" });
}

/**
 * Check out the history branch in a fresh repository, creating it on the first write with a root
 * commit made through the API, which GitHub signs. If another run creates it first, use theirs.
 * @param {{githubClient: any, owner: string, repo: string, branch: string, workspaceDir: string, token: string, serverHost: string, remoteUrl?: string}} options
 * @returns {Promise<string>} the head the save builds on
 */
async function checkoutHistoryBranch({ githubClient, owner, repo, branch, workspaceDir, token, serverHost, remoteUrl }) {
  const env = { ...process.env, ...getGitAuthEnv(token) };
  execGitSync(["init", "--quiet"], { cwd: workspaceDir, stdio: "pipe" });
  execGitSync(["remote", "add", "origin", remoteUrl || `https://${serverHost}/${owner}/${repo}.git`], { cwd: workspaceDir, stdio: "pipe" });
  for (let attempt = 0; attempt < 2; attempt++) {
    try {
      execGitSync(["fetch", "--quiet", "--no-tags", "--depth=1", "origin", `+refs/heads/${branch}:refs/remotes/origin/history`], { cwd: workspaceDir, env, stdio: "pipe", suppressLogs: true });
      execGitSync(["checkout", "--quiet", "-B", branch, "refs/remotes/origin/history"], { cwd: workspaceDir, stdio: "pipe" });
      return execGitSync(["rev-parse", "HEAD"], { cwd: workspaceDir, stdio: "pipe" }).trim();
    } catch (error) {
      if (attempt > 0 || !/couldn't find remote ref|remote branch .* not found/i.test(getErrorMessage(error))) throw error;
    }
    const { data: root } = await githubClient.rest.git.createCommit({ owner, repo, message: `Initialize ${branch}`, tree: EMPTY_TREE, parents: [] });
    core.info(`Created the history's root commit ${root.sha} (signature verified: ${root.verification?.verified === true})`);
    try {
      await githubClient.rest.git.createRef({ owner, repo, ref: `refs/heads/${branch}`, sha: root.sha });
    } catch (error) {
      if (!/422|Reference already exists/i.test(getErrorMessage(error))) throw error;
      core.info(`Another run created ${branch} first; recording on top of it`);
    }
  }
  throw new Error(`${ERR_API}: Could not check out ${branch}`);
}

/** A run's creation time in whole seconds, rounded up so `created: "<=..."` includes it, or null. */
function createdSecond(run) {
  const ms = typeof run?.created_at === "string" ? Date.parse(run.created_at) : NaN;
  return Number.isFinite(ms) ? Math.ceil(ms / 1000) : null;
}

/**
 * What the floor search needs to know about one listed page.
 * @param {any[]} runs
 * @param {boolean} last whether the page ends the listing
 */
function pageSummary(runs, last) {
  let minNum = Infinity;
  let maxNum = -Infinity;
  let oldest = Infinity;
  let newest = -Infinity;
  let timed = true;
  for (const run of runs) {
    const second = createdSecond(run);
    if (second === null) {
      timed = false;
    } else {
      oldest = Math.min(oldest, second);
      newest = Math.max(newest, second);
    }
    if (Number.isInteger(run?.run_number)) {
      minNum = Math.min(minNum, run.run_number);
      maxNum = Math.max(maxNum, run.run_number);
    }
  }
  // Runs created in the same second are listed in any order, so the page's oldest second can
  // continue on the next page. Every later run is older than the runs created after it.
  let settled = Infinity;
  for (const run of runs) {
    if ((createdSecond(run) ?? -Infinity) > oldest && Number.isInteger(run?.run_number)) settled = Math.min(settled, run.run_number);
  }
  return { runs, last, full: runs.length === LIST_RUNS_PER_PAGE, timed, minNum, maxNum, oldest, newest, settled };
}

/** @param {any[]} runs */
function uniqueRuns(runs) {
  const seen = new Set();
  return runs.filter(run => {
    if (run?.id == null) return true;
    if (seen.has(run.id)) return false;
    seen.add(run.id);
    return true;
  });
}

/**
 * Read one newest-first run listing from the page holding `targetRunNumber` back up toward
 * `maxRunNumber`, within a bounded number of calls.
 *
 * GitHub Actions lists only the newest 1,000 runs that match a query (pages 1 to 10). When the
 * target is older, the search continues in a window of the runs created no later than the oldest
 * second on the previous window's page 10, and the walk back up re-reads each window in turn. A
 * page holds the floor when `total_count` shows it ends the listing, or when it is full, every run
 * on it has a creation time, and the runs created after its oldest second already reach the target.
 * Pages are read in order when the target can be among a window's readable pages, which also
 * covers the walk back up; otherwise the window's last readable page is read first, and the pages
 * in between are read in order when it is already past the target. A listing without
 * a consistent `total_count` and the expected rows on each page, or a search that runs out of
 * calls, fails closed.
 * @param {(page: number, created?: string) => Promise<any>} fetchPage
 * @param {number} targetRunNumber lowest run_number we must reach (inclusive)
 * @param {number} maxRunNumber highest run_number we need to verify (inclusive)
 * @param {number | null} [until] last creation second of a bootstrap chunk
 * @param {boolean} [exhaustive] require coverage of the entire unfiltered listing to corroborate a bootstrap gap
 * @returns {Promise<{items: any[], reachedFloor: boolean, maxVerified: number, reachedStart: boolean, reachedTop: boolean, budgetLimited?: boolean, contiguousPages: Array<{page: number, created: string | undefined, batch: any[], minNum: number, maxNum: number, reachedStart: boolean, timed: boolean, oldest: number, newest: number, settled: number}>}>}
 */
async function fetchStreamWindow(fetchPage, targetRunNumber, maxRunNumber, until = null, exhaustive = false) {
  /** @typedef {ReturnType<typeof pageSummary>} Page */
  /** @typedef {{created: string | undefined, total: number | null, pages: Map<number, Page>, bottom: number}} RunWindow */
  /** @type {RunWindow[]} */
  const windows = [];
  let calls = 0;
  const callLimit = exhaustive ? MAX_BOOTSTRAP_CORROBORATION_CALLS : MAX_LIST_CALLS;

  /** @param {RunWindow} win @param {number} page @returns {Promise<Page | null>} */
  const load = async (win, page) => {
    const cached = win.pages.get(page);
    if (cached) return cached;
    if (calls >= callLimit) return null;
    calls += 1;
    const data = (await fetchPage(page, win.created))?.data;
    const runs = data?.workflow_runs;
    const total = data?.total_count;
    // Changing counts can move a run onto a page we already cached. Retry on a later save.
    if (!Array.isArray(runs) || !Number.isSafeInteger(total) || total < 0 || (win.total !== null && total !== win.total)) return null;
    const remaining = total - (page - 1) * LIST_RUNS_PER_PAGE;
    if (runs.length !== Math.min(LIST_RUNS_PER_PAGE, remaining)) return null;
    win.total = total;
    const entry = pageSummary(runs, runs.length === remaining);
    win.pages.set(page, entry);
    return entry;
  };
  /** @param {Page} entry @param {RunWindow} win */
  const holdsFloor = (entry, win) => (exhaustive ? entry.last && win.total !== MAX_LIST_PAGE * LIST_RUNS_PER_PAGE : entry.last || (entry.full && entry.timed && entry.settled <= targetRunNumber));
  const fail = (budgetLimited = false) => ({
    items: uniqueRuns(windows.flatMap(win => [...win.pages.values()].flatMap(entry => entry.runs))),
    budgetLimited,
    reachedFloor: false,
    maxVerified: targetRunNumber - 1,
    reachedStart: false,
    reachedTop: false,
    contiguousPages: [],
  });

  /** @type {number | null} */
  let cutoff = until;
  let floorPage = 1;
  for (;;) {
    if (calls >= (exhaustive ? callLimit : MAX_SEARCH_CALLS)) return fail(true);
    /** @type {RunWindow} */
    const win = { created: cutoff === null ? undefined : `<=${new Date(cutoff * 1000).toISOString().replace(/\.\d{3}Z$/, "Z")}`, total: null, pages: new Map(), bottom: 0 };
    windows.push(win);
    const first = await load(win, 1);
    // A listing without total_count cannot show where it ends, and a later window repeats at least
    // the runs from the second it was cut at.
    if (!first || win.total === null || (windows.length > 1 && first.runs.length === 0)) return fail();
    if (exhaustive && windows.length === 1 && win.total > callLimit * LIST_RUNS_PER_PAGE) return fail(true);
    const lastPage = Math.min(MAX_LIST_PAGE, Math.ceil(win.total / LIST_RUNS_PER_PAGE));
    // Run numbers are unique, so a target this far below page 1 is likely past the readable pages.
    let jump = first.minNum - targetRunNumber > (lastPage - 1) * LIST_RUNS_PER_PAGE;
    let page = 1;
    let entry = first;
    while (!holdsFloor(entry, win) && entry.full && page < lastPage) {
      page = jump ? lastPage : page + 1;
      if (!win.pages.has(page) && calls >= callLimit) return fail(true);
      const next = await load(win, page);
      if (!next) return fail();
      entry = next;
      if (jump && entry.maxNum < targetRunNumber) {
        // Look for a nearer floor, unless we already have one and cannot afford all the skipped pages.
        if (holdsFloor(entry, win) && calls + lastPage - 2 > callLimit) break;
        jump = false;
        page = 1;
        entry = first;
      }
    }
    if (holdsFloor(entry, win)) {
      floorPage = page;
      break;
    }
    if (!entry.full || !entry.timed || page !== MAX_LIST_PAGE || (cutoff !== null && entry.oldest >= cutoff)) return fail();
    win.bottom = page;
    cutoff = entry.oldest;
  }

  /** @type {Array<{page: number, created: string | undefined, entry: Page}>} */
  const walked = [];
  /** @param {typeof walked} entries */
  const toContiguousPages = entries =>
    [...entries]
      .reverse()
      .filter(({ entry }) => entry.runs.length > 0)
      .map(({ page: pageNum, created, entry }) => ({
        page: pageNum,
        created,
        batch: entry.runs,
        minNum: entry.minNum,
        maxNum: entry.maxNum,
        reachedStart: entry.last,
        timed: entry.timed,
        oldest: entry.oldest,
        newest: entry.newest,
        settled: entry.settled,
      }));
  let w = windows.length - 1;
  let page = floorPage;
  for (;;) {
    const win = windows[w];
    if (!win.pages.has(page) && calls >= callLimit) break;
    const entry = await load(win, page);
    // Every page above the floor is full unless the listing lost runs between calls.
    if (!entry || (walked.length > 0 && !entry.full)) return fail();
    walked.push({ page, created: win.created, entry });
    if (page > 1) {
      page -= 1;
    } else if (w > 0) {
      w -= 1;
      page = windows[w].bottom;
    } else {
      const items = uniqueRuns(walked.flatMap(e => e.entry.runs));
      if (exhaustive) {
        if (items.some(run => run?.id == null || !Number.isSafeInteger(run.run_number))) return fail();
        const total = windows[0].total;
        if (total === null || items.length < total || (items.length > total && total < MAX_LIST_PAGE * LIST_RUNS_PER_PAGE)) return fail();
        if (windows.length > 1) {
          // A capped overall total cannot establish completeness. Count the disjoint time
          // slices between our window boundaries; each must fit below a single query's cap.
          const seconds = items.map(createdSecond);
          if (seconds.some(sec => sec === null)) return fail();
          for (let i = 0; i < windows.length; i++) {
            const lower = windows[i + 1]?.created?.slice(2);
            const upper = windows[i].created?.slice(2);
            const low = lower ? Date.parse(lower) / 1000 : -Infinity;
            const high = upper ? Date.parse(upper) / 1000 : Infinity;
            const count = seconds.filter(sec => sec !== null && sec > low && sec <= high).length;
            if (count >= MAX_LIST_PAGE * LIST_RUNS_PER_PAGE) return fail();
            if (!lower) {
              if (count !== windows[i].total) return fail();
              break;
            }
            if (calls >= callLimit) return fail(true);
            calls++;
            const created = upper ? `${new Date((low + 1) * 1000).toISOString().replace(/\.\d{3}Z$/, "Z")}..${upper}` : `>${lower}`;
            const { data } = await fetchPage(1, created);
            if (data?.total_count !== count || !Array.isArray(data.workflow_runs) || data.workflow_runs.length !== Math.min(count, LIST_RUNS_PER_PAGE)) return fail();
          }
        }
      }
      return {
        items,
        reachedFloor: true,
        maxVerified: maxRunNumber,
        reachedStart: walked[0]?.entry.last === true,
        reachedTop: true,
        contiguousPages: toContiguousPages(walked),
      };
    }
  }

  if (exhaustive) return fail(true);
  // The unread page above can still hold runs created in the newest second read, so only runs
  // created before it are verified.
  const newest = walked[walked.length - 1].entry.newest;
  const items = uniqueRuns(walked.flatMap(e => e.entry.runs));
  let maxVerified = -Infinity;
  for (const run of items) {
    if ((createdSecond(run) ?? Infinity) < newest && Number.isInteger(run.run_number)) maxVerified = Math.max(maxVerified, run.run_number);
  }
  if (maxVerified < targetRunNumber) return fail();
  return {
    items,
    reachedFloor: true,
    maxVerified: Math.min(maxRunNumber, maxVerified),
    reachedStart: walked[0]?.entry.last === true,
    reachedTop: false,
    contiguousPages: toContiguousPages(walked),
  };
}

/**
 * Deduplicate and sort unfiltered runs ascending by `run_number` for gap and boundary checks.
 * @param {any[]} items
 * @param {boolean} reachedStart
 */
function indexRuns(items, reachedStart) {
  /** @type {Map<number, any>} */
  const byNumber = new Map();
  for (const item of items) {
    if (Number.isInteger(item?.run_number) && !byNumber.has(item.run_number)) {
      byNumber.set(item.run_number, item);
    }
  }
  const sorted = [...byNumber.values()].sort((a, b) => a.run_number - b.run_number);
  return {
    reachedStart,
    sorted,
    minNum: sorted.length > 0 ? sorted[0].run_number : Infinity,
    maxNum: sorted.length > 0 ? sorted[sorted.length - 1].run_number : -Infinity,
  };
}

/**
 * Inspect whether `indexed` brackets an absent `[startNum, endNum]` range with a higher listed
 * neighbour strictly older than `RECENT_GAP_MS` (5 minutes), returning the neighbours' creation
 * seconds when confirmed or `null` otherwise.
 * @param {ReturnType<typeof indexRuns>} indexed
 * @param {number} startNum
 * @param {number} endNum
 * @param {number} nowMs
 * @returns {{ higherSec: number, lowerSec: number | null } | null}
 */
function oldGapNeighbours(indexed, startNum, endNum, nowMs) {
  if (!indexed || indexed.sorted.length === 0) return null;
  if (!indexed.reachedStart && !(indexed.minNum < startNum)) return null;
  const higherIdx = indexed.sorted.findIndex(item => item.run_number > endNum);
  if (higherIdx === -1) return null;
  const lower = higherIdx > 0 ? indexed.sorted[higherIdx - 1] : null;
  if (lower && lower.run_number >= startNum) return null;
  const higher = indexed.sorted[higherIdx];
  const neighbourCreatedMs = Date.parse(higher.created_at);
  if (!Number.isFinite(neighbourCreatedMs) || nowMs - neighbourCreatedMs <= RECENT_GAP_MS) return null;
  const higherSec = createdSecond(higher);
  if (higherSec === null) return null;
  return { higherSec, lowerSec: lower ? createdSecond(lower) : null };
}

/**
 * Whether a verified unfiltered scan already covered both sides of `num` without an open
 * same-second tie at its edges.
 * @param {(ReturnType<typeof indexRuns> & { reachedFloor: boolean, reachedTop: boolean, maxVerified: number, bottomTimed: boolean, settledFloor: number }) | null} scan
 * @param {number} num
 */
function scanCoversNumber(scan, num) {
  if (!scan || !scan.reachedFloor || scan.sorted.length === 0) return false;
  const lowerCovered = scan.reachedStart || (scan.bottomTimed && scan.settledFloor < num);
  const upperCovered = scan.reachedTop ? scan.maxNum > num : scan.maxVerified > num;
  return lowerCovered && upperCovered;
}

/**
 * An unseen gap `[startNum, endNum]` is confirmed deleted only when a single atomic page brackets
 * both sides, its higher listed neighbour is older than `RECENT_GAP_MS`, and neither neighbour sits
 * on an open same-second page edge where tied runs could spill onto an adjacent page or across a
 * `created` window cutoff.
 * @param {number} startNum
 * @param {number} endNum
 * @param {Array<ReturnType<typeof indexRuns> & { page: number, created: string | undefined, timed: boolean, oldest: number, newest: number }>} atomicPages
 * @param {number} nowMs
 */
function isConfirmedDeletedGap(startNum, endNum, atomicPages, nowMs) {
  for (const page of atomicPages) {
    const gap = oldGapNeighbours(page, startNum, endNum, nowMs);
    if (!gap) continue;
    const bottomSafe = page.reachedStart || (page.timed && gap.higherSec > page.oldest && gap.lowerSec !== null && gap.lowerSec > page.oldest);
    if (!bottomSafe) continue;
    const topSafe = (!page.created && page.page === 1) || (page.timed && gap.higherSec < page.newest && (gap.lowerSec === null || gap.lowerSec < page.newest));
    if (topSafe) return true;
  }
  return false;
}

/**
 * Choose an unfiltered `(perPage, page)` window (with `perPage < LIST_RUNS_PER_PAGE`) that places
 * 0-based `boundaryIndex` near the center of one atomic page, giving ~33-49 rows of overlap on
 * both sides without any timestamp filtering.
 * @param {number} boundaryIndex
 */
function overlappingPageParams(boundaryIndex) {
  let bestPerPage = 67;
  let bestPage = Math.floor(boundaryIndex / bestPerPage) + 1;
  let bestMargin = -1;
  for (let perPage = 50; perPage < LIST_RUNS_PER_PAGE; perPage++) {
    const page = Math.floor(boundaryIndex / perPage) + 1;
    const start = (page - 1) * perPage;
    const end = page * perPage - 1;
    const margin = Math.min(boundaryIndex - start, end - boundaryIndex);
    if (margin > bestMargin) {
      bestMargin = margin;
      bestPerPage = perPage;
      bestPage = page;
    }
  }
  return { perPage: bestPerPage, page: bestPage };
}

/**
 * Find the page boundary straddled by an older-than-window gap `[startNum, endNum]` across
 * verified unfiltered scans (either a non-overlapping numeric gap or a same-second/edge tie,
 * within one window or across a `created` window cutoff), returning the single-page query params
 * needed to observe both sides atomically.
 * @param {number} startNum
 * @param {number} endNum
 * @param {Array<ReturnType<typeof indexRuns> & { reachedFloor: boolean, contiguousPages: Array<{ page: number, created: string | undefined, batch: any[], minNum: number, maxNum: number, reachedStart: boolean, timed: boolean, oldest: number, newest: number }> }>} scans
 * @param {number} nowMs
 * @returns {{ created: string | undefined, perPage: number, page: number } | null}
 */
function findOldBoundaryGap(startNum, endNum, scans, nowMs) {
  for (const scan of scans) {
    if (!scan?.reachedFloor) continue;
    const gap = oldGapNeighbours(scan, startNum, endNum, nowMs);
    if (!gap) continue;
    for (let i = 0; i < scan.contiguousPages.length - 1; i++) {
      const newer = scan.contiguousPages[i];
      const older = scan.contiguousPages[i + 1];
      const numericGap = older.maxNum < startNum && endNum < newer.minNum;
      const bottomEdge = newer.timed && !newer.reachedStart && (gap.higherSec === newer.oldest || gap.lowerSec === newer.oldest);
      const topEdge = older.timed && (gap.higherSec === older.newest || gap.lowerSec === older.newest);
      if (!numericGap && !bottomEdge && !topEdge) continue;
      if (newer.created === older.created) {
        return { created: newer.created, ...overlappingPageParams(newer.page * LIST_RUNS_PER_PAGE) };
      }
      // Cross-window boundary: anchor one page-1 created query just above newer.oldest so both
      // sides of the cutoff appear strictly inside one atomic page.
      let anchorSec = Infinity;
      for (const run of newer.batch) {
        const sec = createdSecond(run);
        if (sec !== null && sec > newer.oldest && sec < anchorSec) anchorSec = sec;
      }
      if (Number.isFinite(anchorSec)) {
        return { created: `<=${new Date(anchorSec * 1000).toISOString().replace(/\.\d{3}Z$/, "Z")}`, perPage: LIST_RUNS_PER_PAGE, page: 1 };
      }
    }
    const lastPage = scan.contiguousPages.at(-1);
    if (scan.reachedStart && lastPage && !lastPage.reachedStart && endNum < lastPage.minNum) {
      return { created: lastPage.created, ...overlappingPageParams(lastPage.page * LIST_RUNS_PER_PAGE) };
    }
  }
  return null;
}

/**
 * Read the compiled `GH_AW_ROUTING_GENERATION` hash from the workflow lock file at `ref` so
 * failed saves straddling a generation edit on the default branch can be attributed to their true
 * definition. Never called for `pull_request_target` runs whose REST `head_sha` is the PR head.
 * An unavailable definition leaves the run unresolved for the next writer.
 * @param {{githubClient: any, owner: string, repo: string, workflowPath: string, ref: string}} options
 */
async function readRunGeneration({ githubClient, owner, repo, workflowPath, ref }) {
  if (!workflowPath || !ref) throw new Error("Missing workflow path or ref for generation lookup");
  const { data } = await githubClient.rest.repos.getContent({ owner, repo, path: workflowPath, ref });
  if (typeof data?.content !== "string") throw new Error("Workflow content unavailable for generation lookup");
  const text = Buffer.from(data.content, data.encoding === "base64" ? "base64" : "utf8").toString("utf8");
  const match = /\bGH_AW_ROUTING_GENERATION:\s*["']?([0-9a-f]{64}:[0-9a-f]{64})["']?/.exec(text);
  if (!match) throw new Error("Workflow does not contain a compiled routing generation");
  return match[1];
}

/**
 * Whether a workflow run's definition comes from the default branch (matching AW-3's eligibility
 * rule): pull_request* except pull_request_target are ineligible; pull_request_target is eligible
 * regardless of head_branch; every other event is eligible only on defaultBranch.
 * @param {any} run
 * @param {string} defaultBranch
 */
function isEligibleRun(run, defaultBranch) {
  if (/^pull_request(?!_target)/.test(run.event || "")) return false;
  return run.event === "pull_request_target" || run.head_branch === defaultBranch;
}

/**
 * Find a creation-time window ending within one listing's worth of run numbers above the floor.
 * This is only a search hint: the ordinary reconciliation scan still verifies every obligation.
 * @param {(created: string) => Promise<any>} probe newest unfiltered run at or before a timestamp
 * @param {number} floor
 * @param {number} nowMs
 */
async function findBootstrapChunk(probe, floor, nowMs) {
  const span = MAX_LIST_PAGE * LIST_RUNS_PER_PAGE;
  let low = 0;
  let high = Math.ceil(nowMs / 1000);
  /** @type {{second: number, number: number} | null} */
  let firstAbove = null;
  // Bisect whole seconds (at most 32 probes through 2106), not pages past the API's result cap.
  while (low <= high) {
    const second = Math.floor((low + high) / 2);
    const { data } = await probe(`<=${new Date(second * 1000).toISOString().replace(/\.\d{3}Z$/, "Z")}`);
    const items = data?.workflow_runs;
    if (!Array.isArray(items) || !Number.isSafeInteger(data.total_count) || data.total_count < 0 || items.length !== Math.min(data.total_count, 1)) return null;
    const number = items[0]?.run_number;
    if (items.length && (!Number.isSafeInteger(number) || number < 1)) return null;
    if (!items.length || number <= floor) low = second + 1;
    else if (number > floor + span && data.total_count >= span) {
      firstAbove = { second, number };
      high = second - 1;
    } else return { second, number };
  }
  return firstAbove;
}

/**
 * Completed runs on the newest pages that the history may need to charge because their own save
 * never landed (contract 5): first attempts strictly earlier than `runNumber` whose definition
 * comes from the default branch, judged by their first attempt, not yet reconciled
 * (`run_number > floor` or in `unresolved_runs`) and not yet written. Runs that
 * `pull_request_target` started are listed under the pull request's branch, so they come from a
 * second page. Streams use bounded chunked pagination to reach back to the previous floor without
 * stalling across large gaps, ambiguous gaps are corroborated against an unfiltered run listing,
 * and missing tracked `unresolved_runs` with persisted run IDs fall back to direct `getWorkflowRun`
 * lookups so long-running obligations can always be settled.
 * @param {{githubClient: any, owner: string, repo: string, workflowFile: string, workflowPath?: string, defaultBranch: string, runId: string, runNumber: number, state: HistoryState, runs: any[], generation: string, inspectionBudget?: {remaining: number}}} options
 * @returns {Promise<{listing: any[], reconciliation: import("./routing_history.cjs").ReconciliationCoverage}>}
 */
async function listRuns({ githubClient, owner, repo, workflowFile, workflowPath = "", defaultBranch, runId, runNumber, state, runs, generation, inspectionBudget = { remaining: MAX_NEW_INSPECTION_CALLS } }) {
  // At or immediately after the reset fence there are no earlier obligations to inspect.
  if (runNumber <= state.reset_after_run + 1 || (state.generation && state.generation.hash !== generation && runNumber <= state.generation.since_run)) {
    return { listing: [], reconciliation: { verified: true, unresolved: [], resolved: [] } };
  }

  const nowMs = Date.now();
  const legacyFloor = legacyReconciliationFloor(state, runs);
  const baseReconciled = typeof state.reconciled_through === "number" ? state.reconciled_through : legacyFloor;
  const floor = Math.max(baseReconciled, state.reset_after_run || 0);
  // Leave tracked unresolved runs at or above this writer's number for a later writer to handle.
  const prevUnresolved = new Set((Array.isArray(state.unresolved_runs) ? state.unresolved_runs : []).filter(n => Number.isInteger(n) && n > state.reset_after_run && n < runNumber));
  const storedUnresolvedIds = state.unresolved_run_ids && typeof state.unresolved_run_ids === "object" ? state.unresolved_run_ids : {};
  const written = new Set(runs.map(line => line.run));
  const writtenNumbers = new Set(runs.map(line => line.number));
  writtenNumbers.add(runNumber);

  let upperNeeded = Math.max(floor, runNumber - 1);
  /** @type {number | null} */
  let until = null;
  /** @param {string} created */
  const probe = created => githubClient.rest.actions.listWorkflowRuns({ owner, repo, workflow_id: workflowFile, created, page: 1, per_page: 1 });
  // A long-lived workflow may already have thousands of runs when learning is first enabled.
  // Start near the unverified floor rather than spending every save's budget searching back to it.
  if (floor < (state.generation?.since_run ?? runNumber) && upperNeeded - floor > MAX_LIST_PAGE * LIST_RUNS_PER_PAGE) {
    const chunk = await findBootstrapChunk(probe, floor, nowMs);
    if (chunk) {
      upperNeeded = Math.min(upperNeeded, chunk.number);
      until = chunk.second;
    }
  }
  /** @param {Record<string, string>} filter */
  const listPages = filter => (/** @type {number} */ page, /** @type {string | undefined} */ created) =>
    githubClient.rest.actions.listWorkflowRuns({ owner, repo, workflow_id: workflowFile, ...filter, per_page: LIST_RUNS_PER_PAGE, page, ...(created ? { created } : {}) });
  // In a bootstrap window, list all event types once so a non-default-branch run is
  // positive evidence of ineligibility, not a gap requiring exhaustive corroboration.
  const streams =
    until !== null
      ? [await fetchStreamWindow(listPages({}), floor + 1, upperNeeded, until)]
      : await Promise.all([fetchStreamWindow(listPages({ branch: defaultBranch }), floor + 1, upperNeeded), fetchStreamWindow(listPages({ event: "pull_request_target" }), floor + 1, upperNeeded)]);

  let verified = streams.every(stream => stream.reachedFloor);
  let verifiedUpper = verified ? streams.reduce((upper, stream) => Math.min(upper, stream.maxVerified), upperNeeded) : floor;
  if (verified && runNumber > floor + 1 && verifiedUpper <= floor) verified = false;

  /** @type {Map<number, any>} */
  const eligibleByNumber = new Map();
  /** @type {Set<number>} */
  const ineligibleNumbers = new Set();
  /** @param {any[]} items */
  const ingestRuns = items => {
    for (const run of items) {
      const num = run.run_number;
      if (!Number.isInteger(num) || num <= state.reset_after_run || num >= runNumber) continue;
      if (!isEligibleRun(run, defaultBranch)) {
        ineligibleNumbers.add(num);
        continue;
      }
      if (!eligibleByNumber.has(num)) eligibleByNumber.set(num, run);
    }
  };
  for (const stream of streams) ingestRuns(stream.items);

  const missingUnresolved = [...prevUnresolved].filter(num => !writtenNumbers.has(num) && !eligibleByNumber.has(num) && !ineligibleNumbers.has(num));
  const expectedIntervalCount = verified ? Math.max(0, verifiedUpper - floor) : 0;
  /** @type {Set<number>} */
  const knownInInterval = new Set();
  if (verified && expectedIntervalCount > 0) {
    for (const num of writtenNumbers) if (num > floor && num <= verifiedUpper) knownInInterval.add(num);
    for (const num of eligibleByNumber.keys()) if (num > floor && num <= verifiedUpper) knownInInterval.add(num);
    for (const num of ineligibleNumbers) if (num > floor && num <= verifiedUpper) knownInInterval.add(num);
  }
  const hasIntervalGap = verified && knownInInterval.size < expectedIntervalCount;

  /** @type {number[]} */
  const unresolvedGaps = [];
  /** @type {Array<ReturnType<typeof indexRuns> & { reachedFloor: boolean, reachedTop: boolean, maxVerified: number, bottomTimed: boolean, settledFloor: number, contiguousPages: Array<{ page: number, created: string | undefined, batch: any[], minNum: number, maxNum: number, reachedStart: boolean, timed: boolean, oldest: number, newest: number, settled: number }> }>} */
  const unfilteredScans = [];
  /** @type {Array<ReturnType<typeof indexRuns> & { page: number, created: string | undefined, timed: boolean, oldest: number, newest: number }>} */
  const atomicPages = [];
  /** @type {Set<string>} */
  const checkedBoundaries = new Set();
  let boundaryChecks = 0;

  /** @param {Awaited<ReturnType<typeof fetchStreamWindow>>} stream */
  const recordUnfilteredStream = stream => {
    const oldestPage = stream.contiguousPages.at(-1);
    const scan = {
      ...indexRuns(stream.items, stream.reachedStart),
      reachedFloor: stream.reachedFloor,
      reachedTop: stream.reachedTop,
      maxVerified: stream.maxVerified,
      bottomTimed: oldestPage?.timed === true,
      settledFloor: oldestPage ? oldestPage.settled : Infinity,
      contiguousPages: stream.contiguousPages,
    };
    unfilteredScans.push(scan);
    for (const page of stream.contiguousPages) {
      atomicPages.push({
        ...indexRuns(page.batch, page.reachedStart),
        page: page.page,
        created: page.created,
        timed: page.timed,
        oldest: page.oldest,
        newest: page.newest,
      });
    }
    ingestRuns(stream.items);
    return scan;
  };

  /**
   * When an older-than-window gap straddles a page boundary across separate paginated requests,
   * issue a bounded overlapping request within the same window using a staggered `per_page` so
   * both sides are observed atomically (recovering any shift-hidden run or confirming true deletion).
   * @param {number} startNum
   * @param {number} endNum
   */
  const verifyBoundaryGap = async (startNum, endNum) => {
    if (isConfirmedDeletedGap(startNum, endNum, atomicPages, nowMs)) return;
    if (boundaryChecks >= MAX_BOUNDARY_CHECKS) return;
    const boundary = findOldBoundaryGap(startNum, endNum, unfilteredScans, nowMs);
    if (!boundary) return;
    const { created, perPage, page } = boundary;
    const boundaryKey = `${created || ""}:${perPage}:${page}`;
    if (checkedBoundaries.has(boundaryKey)) return;
    checkedBoundaries.add(boundaryKey);
    boundaryChecks += 1;
    let response;
    try {
      response = await githubClient.rest.actions.listWorkflowRuns({
        owner,
        repo,
        workflow_id: workflowFile,
        ...(created ? { created } : {}),
        per_page: perPage,
        page,
      });
    } catch (error) {
      // Leave unconfirmed boundary gaps unresolved on transient API errors.
      core.warning(`Could not verify boundary gap ${startNum}-${endNum}: ${getErrorMessage(error)}`);
      return;
    }
    const batch = response?.data?.workflow_runs;
    const totalCount = response?.data?.total_count;
    if (!Array.isArray(batch) || !Number.isSafeInteger(totalCount) || totalCount < 0) return;
    const remaining = totalCount - (page - 1) * perPage;
    if (remaining <= 0 || batch.length !== Math.min(perPage, remaining)) return;
    const summary = pageSummary(batch, batch.length === remaining);
    atomicPages.push({
      ...indexRuns(batch, summary.last),
      page,
      created,
      timed: summary.timed,
      oldest: summary.oldest,
      newest: summary.newest,
    });
    ingestRuns(batch);
  };

  /** @type {(ReturnType<typeof indexRuns> & { reachedFloor: boolean, reachedTop: boolean, maxVerified: number, bottomTimed: boolean, settledFloor: number }) | null} */
  let intervalUnfilteredScan = null;
  /** @type {Awaited<ReturnType<typeof fetchStreamWindow>> | undefined} */
  let bootstrapProof;
  let needsIntervalGapFinalization = false;
  let mayConfirmIntervalDeletions = true;
  if (hasIntervalGap) {
    const sortedKnown = [...knownInInterval].sort((a, b) => a - b);
    let earliestGap = floor + 1;
    for (const num of sortedKnown) {
      if (num > earliestGap) break;
      earliestGap = num + 1;
    }
    // A run's timestamp may be newer than both its numbered neighbours. A bootstrap
    // cutoff can find positive evidence, but cannot prove a missing run was deleted.
    const unfilteredStream = await fetchStreamWindow(listPages({}), until === null ? earliestGap : 0, verifiedUpper, null, until !== null);
    if (until !== null) bootstrapProof = unfilteredStream;
    intervalUnfilteredScan = recordUnfilteredStream(unfilteredStream);
    if (!unfilteredStream.reachedFloor) {
      if (until === null) verified = false;
      else if (unfilteredStream.budgetLimited) {
        // Retain missing numbers explicitly. The remainder of the chunk has positive evidence.
        needsIntervalGapFinalization = true;
        mayConfirmIntervalDeletions = false;
      } else {
        while (earliestGap <= verifiedUpper && (writtenNumbers.has(earliestGap) || eligibleByNumber.has(earliestGap) || ineligibleNumbers.has(earliestGap))) earliestGap++;
        verifiedUpper = earliestGap - 1;
        verified = verifiedUpper > floor;
      }
    } else {
      verifiedUpper = Math.min(verifiedUpper, unfilteredStream.maxVerified);
      if (runNumber > floor + 1 && verifiedUpper <= floor) {
        verified = false;
      } else {
        needsIntervalGapFinalization = true;
      }
    }
  }

  /** @type {Set<number>} */
  const resolved = new Set();
  /** @type {number | undefined} */
  let nextLookupCursor;
  // Resolve tracked unresolved runs that fell past the listing windows via bounded direct run-ID lookups,
  // advancing a persisted run-number cursor across saves so every tracked ID gets checked without resonance.
  const stillMissingUnresolved = missingUnresolved.filter(num => !eligibleByNumber.has(num) && !ineligibleNumbers.has(num));
  const lookupCandidates = stillMissingUnresolved.filter(num => Boolean(storedUnresolvedIds[String(num)])).sort((a, b) => a - b);
  if (lookupCandidates.length > 0 && typeof githubClient?.rest?.actions?.getWorkflowRun === "function") {
    const prevCursor = typeof state.unresolved_lookup_cursor === "number" && Number.isInteger(state.unresolved_lookup_cursor) && state.unresolved_lookup_cursor > 0 ? state.unresolved_lookup_cursor : 0;
    const firstAfterCursor = lookupCandidates.findIndex(num => num > prevCursor);
    const startIndex = firstAfterCursor === -1 ? 0 : firstAfterCursor;
    const countToCheck = Math.min(MAX_DIRECT_UNRESOLVED_LOOKUPS, lookupCandidates.length);
    for (let i = 0; i < countToCheck; i++) {
      const num = lookupCandidates[(startIndex + i) % lookupCandidates.length];
      nextLookupCursor = num;
      const storedId = storedUnresolvedIds[String(num)];
      try {
        const { data: run } = await githubClient.rest.actions.getWorkflowRun({ owner, repo, run_id: Number(storedId) || storedId });
        if (run && Number.isInteger(run.run_number)) {
          if (!isEligibleRun(run, defaultBranch)) {
            ineligibleNumbers.add(num);
          } else {
            eligibleByNumber.set(num, run);
          }
        }
      } catch (error) {
        // A confirmed 404 proves the tracked run was deleted; any other error leaves it unresolved.
        if (/** @type {any} */ error?.status === 404) resolved.add(num);
      }
    }
  }

  // Give ID-less unresolved runs independent unfiltered scan coverage whenever they were not already
  // covered by the interval-gap unfiltered scan. Isolate failures locally so an API error on a deep
  // page does not discard completed direct-ID lookups, cursor rotation, or verified interval coverage.
  const unindexedMissing = stillMissingUnresolved.filter(num => !eligibleByNumber.has(num) && !ineligibleNumbers.has(num) && !resolved.has(num) && !storedUnresolvedIds[String(num)]);
  const generationStart = state.generation?.since_run ?? runNumber;
  if (!bootstrapProof && unindexedMissing.some(num => num < generationStart)) {
    try {
      bootstrapProof = await fetchStreamWindow(listPages({}), 0, upperNeeded, null, true);
      recordUnfilteredStream(bootstrapProof);
    } catch (error) {
      core.warning(`Could not corroborate a retained bootstrap gap: ${getErrorMessage(error)}`);
    }
  }
  /** @param {number} num */
  const mayExpireGap = num => num >= generationStart || bootstrapProof?.reachedFloor === true;
  const uncoveredUnindexed = unindexedMissing.filter(num => !scanCoversNumber(intervalUnfilteredScan, num));
  if (uncoveredUnindexed.length > 0) {
    try {
      const minTarget = Math.min(...uncoveredUnindexed);
      const unfilteredStream = await fetchStreamWindow(listPages({}), minTarget, upperNeeded);
      recordUnfilteredStream(unfilteredStream);
      // If the oldest uncovered obligation exceeds the search budget, retry once against the newest
      // remaining uncovered obligation so an unreachable ancient entry cannot starve newer ones.
      if (!unfilteredStream.reachedFloor && uncoveredUnindexed.length > 1) {
        const remaining = uncoveredUnindexed.filter(num => num !== minTarget && !eligibleByNumber.has(num) && !ineligibleNumbers.has(num) && !resolved.has(num));
        if (remaining.length > 0) {
          const fallbackStream = await fetchStreamWindow(listPages({}), Math.max(...remaining), upperNeeded);
          recordUnfilteredStream(fallbackStream);
        }
      }
    } catch (error) {
      core.warning(`Could not scan unfiltered runs for ID-less unresolved obligations: ${getErrorMessage(error)}`);
    }
  }

  /** @returns {Array<[number, number]>} */
  const intervalGaps = () => {
    /** @type {Set<number>} */
    const covered = new Set();
    for (const num of writtenNumbers) if (num > floor && num <= verifiedUpper) covered.add(num);
    for (const num of eligibleByNumber.keys()) if (num > floor && num <= verifiedUpper) covered.add(num);
    for (const num of ineligibleNumbers) if (num > floor && num <= verifiedUpper) covered.add(num);
    /** @type {Array<[number, number]>} */
    const gaps = [];
    let prev = floor;
    for (const next of [...[...covered].sort((a, b) => a - b), verifiedUpper + 1]) {
      if (prev + 1 <= next - 1) gaps.push([prev + 1, next - 1]);
      prev = next;
    }
    return gaps;
  };

  // Run bounded boundary checks after both unfiltered scans have been ingested so recovered runs
  // (eligible or ineligible) are reflected before finalizing interval gaps and inspecting eligible runs.
  if (needsIntervalGapFinalization && mayConfirmIntervalDeletions) {
    for (const [gapStart, gapEnd] of intervalGaps()) await verifyBoundaryGap(gapStart, gapEnd);
  }
  for (const num of unindexedMissing) {
    if (eligibleByNumber.has(num) || ineligibleNumbers.has(num) || resolved.has(num) || !mayExpireGap(num)) continue;
    await verifyBoundaryGap(num, num);
  }
  if (needsIntervalGapFinalization) {
    for (const [gapStart, gapEnd] of intervalGaps()) {
      if (mayConfirmIntervalDeletions && isConfirmedDeletedGap(gapStart, gapEnd, atomicPages, nowMs)) continue;
      if (unresolvedGaps.length + (gapEnd - gapStart + 1) > BOUNDS.unresolved_runs) {
        if (gapStart - 1 > floor) {
          // Clamp verifiedUpper below the overflowing gap so already-verified earlier coverage still advances.
          verifiedUpper = gapStart - 1;
        } else {
          verified = false;
          unresolvedGaps.length = 0;
        }
        break;
      }
      for (let num = gapStart; num <= gapEnd; num++) unresolvedGaps.push(num);
    }
  }

  const effectiveSinceRun = state.generation?.hash === generation ? state.generation.since_run : runNumber;
  const fullCatchUp = verified && (runNumber <= floor + 1 || verifiedUpper >= runNumber - 1);
  let reconciliationCeiling = verified ? (fullCatchUp ? runNumber : verifiedUpper) : floor;
  const listing = [];
  /** @type {Set<number>} */
  const unresolved = new Set(verified ? unresolvedGaps : []);
  /** @type {Record<string, string>} */
  const unresolvedIds = {};
  const budgetExhausted = new Error("New routing-history inspection budget exhausted");

  for (const num of [...eligibleByNumber.keys()].sort((a, b) => a - b)) {
    const isTrackedUnresolved = prevUnresolved.has(num);
    // Defer runs above reconciliationCeiling so no charge lands outside persisted verified coverage.
    if (!isTrackedUnresolved && (!verified || num <= floor || num > reconciliationCeiling)) continue;
    const run = eligibleByNumber.get(num);
    const id = String(run.id);
    if (id === runId || written.has(id) || writtenNumbers.has(num)) {
      if (isTrackedUnresolved) resolved.add(num);
      continue;
    }
    // Previously tracked obligations have their own bounded state and must not be
    // starved by new work. Checkpoint new work before the first unfinished inspection.
    const takeInspection = () => {
      if (isTrackedUnresolved) return;
      if (!inspectionBudget.remaining) throw budgetExhausted;
      inspectionBudget.remaining--;
    };
    try {
      let { status, conclusion } = run;
      // The listing describes the latest attempt. Only the first attempt can owe a save.
      if ((run.run_attempt || 1) > 1) {
        takeInspection();
        const { data: firstAttempt } = await githubClient.rest.actions.getWorkflowRunAttempt({ owner, repo, run_id: run.id, attempt_number: 1 });
        conclusion = firstAttempt.conclusion;
        status = firstAttempt.status ?? (conclusion ? "completed" : null);
      }
      if (status !== "completed") {
        unresolved.add(num);
        unresolvedIds[String(num)] = id;
        continue;
      }
      /** @type {{id: string, number: number, conclusion: string, agent_ran?: boolean, generation?: string}} */
      const listed = { id, number: num, conclusion: conclusion || "unknown" };
      let include = true;
      if (!SAVED_CONCLUSIONS.has(listed.conclusion)) {
        takeInspection();
        const jobs = await githubClient.paginate(githubClient.rest.actions.listJobsForWorkflowRunAttempt, { owner, repo, run_id: run.id, attempt_number: 1, per_page: 100 }, (/** @type {any} */ response) => {
          if (/<[^>]+>;\s*rel="next"/.test(response.headers.link || "")) takeInspection();
          return response.data;
        });
        listed.agent_ran = jobs.some((/** @type {any} */ job) => job?.name === "agent" && job.conclusion !== "skipped" && Boolean(job.started_at));
        const writer = jobs.find((/** @type {any} */ job) => job?.name === WRITER_JOB);
        // A run that failed before any job was created (jobs.length === 0) is recorded as no_agent.
        // When jobs exist, nothing is owed by a run compiled while adaptive routing was off or for a
        // dry run (both omit the history job), or by a run whose history job succeeded: it saved the
        // run, or declined it, as for a pull request into another branch.
        if (jobs.length > 0 && (!writer || writer.conclusion === "success")) include = false;
        // Only trust head_sha for non-pull_request_target runs on the default branch; pull_request_target's
        // REST head_sha is the PR head rather than the base commit that supplied the workflow definition.
        if (include && listed.agent_ran && num <= effectiveSinceRun && run.event !== "pull_request_target" && run.head_branch === defaultBranch && typeof run.head_sha === "string" && run.head_sha) {
          takeInspection();
          listed.generation = await readRunGeneration({ githubClient, owner, repo, workflowPath, ref: run.head_sha });
        }
      }
      if (include) listing.push(listed);
      if (isTrackedUnresolved) resolved.add(num);
    } catch (error) {
      if (error === budgetExhausted) {
        reconciliationCeiling = num - 1;
        break;
      }
      // Keep this run unresolved with its ID so a per-run API failure does not discard other
      // resolved runs, verified interval coverage, or direct-lookup cursor progress.
      core.warning(`Could not inspect earlier run ${num} (${id}): ${getErrorMessage(error)}`);
      unresolved.add(num);
      unresolvedIds[String(num)] = id;
    }
  }

  for (const num of prevUnresolved) {
    if (eligibleByNumber.has(num) || resolved.has(num)) continue;
    const storedId = storedUnresolvedIds[String(num)];
    if (writtenNumbers.has(num) || ineligibleNumbers.has(num)) {
      resolved.add(num);
    } else if (!storedId && mayExpireGap(num) && isConfirmedDeletedGap(num, num, atomicPages, nowMs)) {
      // Expire confirmed old gap-only entries once a single atomic unfiltered page covering both sides shows them absent.
      resolved.add(num);
    } else {
      // Retain tracked entries that have a run ID (pending direct 404/completion) or unconfirmed/recent gaps.
      unresolved.add(num);
      if (storedId) unresolvedIds[String(num)] = storedId;
    }
  }

  return {
    listing,
    reconciliation: {
      verified,
      ...(verified && reconciliationCeiling < runNumber ? { ceiling: reconciliationCeiling } : {}),
      unresolved: [...unresolved].sort((a, b) => a - b),
      ...(Object.keys(unresolvedIds).length > 0 ? { unresolved_ids: unresolvedIds } : {}),
      ...(nextLookupCursor !== undefined ? { unresolved_lookup_cursor: nextLookupCursor } : {}),
      resolved: [...resolved].sort((a, b) => a - b),
    },
  };
}

/** @param {string} serverUrl */
function hostOf(serverUrl) {
  try {
    return new URL(serverUrl).host;
  } catch (error) {
    throw new Error(`${ERR_CONFIG}: GITHUB_SERVER_URL is not a URL: ${getErrorMessage(error)}`, { cause: error });
  }
}

/** @param {string} parent */
function makeWorkspace(parent) {
  try {
    return fs.mkdtempSync(path.join(parent, "gh-aw-routing-history-"));
  } catch (error) {
    throw new Error(`${ERR_SYSTEM}: Could not create a directory for the history branch: ${getErrorMessage(error)}`, { cause: error });
  }
}

/**
 * @param {{github?: any, context?: any, env?: Record<string, string | undefined>, workspaceDir?: string, remoteUrl?: string, pushSignedCommitsFn?: any, execGetExecOutput?: any, sleepFn?: (delay: number) => Promise<void>}} [options]
 *   Tests replace the API client, the remote and the signed push.
 */
async function main(options = {}) {
  const githubClient = options.github || github;
  const ctx = options.context || context;
  const env = options.env || process.env;
  const config = readConfig(env);
  const { owner, repo } = ctx.repo;
  const runId = String(ctx.runId);
  const attempt = Number(env.GITHUB_RUN_ATTEMPT || "1");
  const workflowRef = env.GITHUB_WORKFLOW_REF || "";
  const defaultBranch = ctx.payload?.repository?.default_branch || (await githubClient.rest.repos.get({ owner, repo })).data.default_branch;
  const skipped = ineligibility({ attempt, workflowRef, defaultBranch });
  if (skipped) {
    core.info(`Not recording run ${runId} in the routing history: ${skipped}`);
    return;
  }

  const evidence = sessionEvidence(readSession(config.sessionFile), config.subAgentModels);
  const token = env.GH_TOKEN || "";
  const serverHost = hostOf(env.GITHUB_SERVER_URL || "https://github.com");
  const workspaceDir = options.workspaceDir || makeWorkspace(env.RUNNER_TEMP || os.tmpdir());
  const baseRef = await checkoutHistoryBranch({ githubClient, owner, repo, branch: config.branch, workspaceDir, token, serverHost, remoteUrl: options.remoteUrl });
  const initial = readHistory(workspaceDir, config.workflow);
  const workflowSpec = workflowRef.slice(0, workflowRef.lastIndexOf("@"));
  const workflowFile = path.posix.basename(workflowSpec);
  const repoPrefix = `${owner}/${repo}/`;
  const workflowPath = workflowSpec.startsWith(repoPrefix) ? workflowSpec.slice(repoPrefix.length) : "";
  const run = { id: runId, number: Number(ctx.runNumber), attempt };
  const inspectionBudget = { remaining: MAX_NEW_INSPECTION_CALLS };
  /** @type {any[]} */
  let recorded = [];
  /** @param {{state: HistoryState, runs: any[]}} history */
  const record = async history => {
    if (history.runs.some(entry => entry.run === runId)) {
      recorded = [];
      return false;
    }
    /** @type {any[]} */
    let listing = [];
    /** @type {import("./routing_history.cjs").ReconciliationCoverage} */
    let reconciliation = { verified: false };
    try {
      ({ listing, reconciliation } = await listRuns({
        githubClient,
        owner,
        repo,
        workflowFile,
        workflowPath,
        defaultBranch,
        runId,
        runNumber: run.number,
        state: history.state,
        runs: history.runs,
        generation: config.generation,
        inspectionBudget,
      }));
    } catch (error) {
      // Runs this save cannot charge stay unreconciled for the next save.
      core.warning(`Could not list earlier runs to charge unsaved ones: ${getErrorMessage(error)}`);
    }
    const result = applyRun(history.state, history.runs, {
      workflow: config.workflow,
      run,
      at: timestamp(),
      generation: config.generation,
      mode: config.mode,
      scope: config.scope,
      ...evidence,
      receipt: null,
      prepare: null,
      listing,
      reconciliation,
    });
    recorded = result.lines;
    if (result.lines.length > 0) commitHistory(workspaceDir, result.state, trimRuns([...history.runs, ...result.lines]), runId);
    return result.lines.length > 0;
  };
  if (!(await record(initial))) {
    core.info(`Run ${runId} is already in the routing history`);
    return;
  }
  const pushed = await pushRepoMemoryChangesWithRetry({
    githubClient,
    targetOwner: owner,
    targetRepoName: repo,
    targetRepo: `${owner}/${repo}`,
    branchName: config.branch,
    baseRef,
    workspaceDir,
    ghToken: token,
    serverHost,
    reapply: () => record(readHistory(workspaceDir, config.workflow)),
    ...(options.remoteUrl ? { originUrlForPush: options.remoteUrl, repoUrlForRetry: options.remoteUrl } : {}),
    ...(options.pushSignedCommitsFn ? { pushSignedCommitsFn: options.pushSignedCommitsFn } : {}),
    ...(options.execGetExecOutput ? { execGetExecOutput: options.execGetExecOutput } : {}),
    ...(options.sleepFn ? { sleepFn: options.sleepFn } : {}),
  });
  if (pushed === false) return;
  if (pushed === undefined) {
    core.info(`Run ${runId} was already in the routing history when this save re-read it`);
    return;
  }
  const own = recorded.find(line => line.run === runId);
  const counted = own?.counted ? "counted" : `not counted${own?.note ? ` (${own.note})` : ""}`;
  core.info(`Recorded run ${runId} in ${config.branch}: ${own?.pair ?? "no pair"}, ${own?.outcome ?? "no outcome"}, ${counted}`);
  await core.summary.addRaw(`### Routing history\n\nRecorded run ${runId} in \`${config.branch}\` (${config.mode}): ${own?.pair ? `\`${own.pair}\`` : "no pair"}, ${own?.outcome ?? "no outcome"}, ${counted}.\n`).write();
}

module.exports = { checkoutHistoryBranch, fetchStreamWindow, ineligibility, listRuns, main, pairName, readConfig, readHistory, runCost, sessionEvidence };
