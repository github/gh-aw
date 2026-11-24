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
const { applyRun, emptyState, trimRuns } = require("./routing_history.cjs");

/** @typedef {import("./routing_history.cjs").HistoryState} HistoryState */

const BRANCH_PREFIX = "memory/adaptive-routing/";
const STATE_FILE = "state.json";
const RUNS_FILE = "runs.jsonl";
const DEFAULT_SESSION_FILE = "/tmp/gh-aw/usage/aw_session.jsonl";
const EMPTY_TREE = "4b825dc642cb6eb9a060e54bf8d69288fbee4904";
const CLASSIFIER_PURPOSE = "routing_classification";
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
  const verdict = events.filter(event => isRecord(event) && event.type === "routing.observation" && isRecord(event.data)).at(-1);
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
  return `${PROVIDER_NAMES[provider] ?? provider}/${model}@${effort}`;
}

/** @param {unknown} value @returns {number | null} */
const count = value => (typeof value === "number" && Number.isFinite(value) && value >= 0 ? value : null);

/**
 * One main request's counts as contract 4 defines them: input excluding cache, cache read, cache
 * write and output. AWF's records keep each provider's own input count, which includes cache for
 * some responses and not others. The record's flag says which when AWF provides it. Otherwise an
 * input count smaller than the cache cannot include it, and any other count is taken to include it,
 * which undercounts only a request whose new input exceeds its cached input.
 * @param {Record<string, any>} used the record's usage
 * @returns {number[] | null}
 */
function requestTokens(used) {
  const input = count(used.inputTokens);
  const cacheRead = count(used.cacheReadInputTokens);
  const cacheWrite = count(used.cacheCreationInputTokens);
  const output = count(used.outputTokens);
  if (input === null || cacheRead === null || cacheWrite === null || output === null) return null;
  const cached = cacheRead + cacheWrite;
  const includesCache = typeof used.inputTokensIncludeCache === "boolean" ? used.inputTokensIncludeCache : input >= cached;
  // As AWF's credit meter reads an input count that includes cache.
  return [includesCache ? Math.max(0, input - cached) : input, cacheRead, cacheWrite, output];
}

/**
 * The run's cost from AWF's records (contract 4): `credits` over every agent request except the
 * router's classifier call, and `model_credits` and token counts over the main conversation's
 * requests. Each is null when a request it covers lacks a price or a count.
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
    const counts = requestTokens(used);
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
  return {
    credits: requests > 0 ? round(credits) : null,
    model_credits: main > 0 ? round(modelCredits) : null,
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

/**
 * Completed runs on the newest page that the history may need to charge because their own save
 * never landed (contract 5): first attempts from the default branch, judged by their first attempt,
 * above both fences and not yet written. Only those that did not succeed need a jobs call, to tell
 * an agent that ran from one that never started.
 * @param {{githubClient: any, owner: string, repo: string, workflowFile: string, defaultBranch: string, runId: string, state: HistoryState, runs: any[], generation: string}} options
 */
async function listRuns({ githubClient, owner, repo, workflowFile, defaultBranch, runId, state, runs, generation }) {
  // A run that starts a generation fences off every earlier run.
  const sinceRun = state.generation?.hash === generation ? state.generation.since_run : Infinity;
  const written = new Set(runs.map(line => line.run));
  const { data } = await githubClient.rest.actions.listWorkflowRuns({ owner, repo, workflow_id: workflowFile, branch: defaultBranch, per_page: 100 });
  const listing = [];
  for (const run of data.workflow_runs || []) {
    const id = String(run.id);
    if (id === runId || run.status !== "completed" || written.has(id) || run.head_branch !== defaultBranch) continue;
    // Their definitions come from the pull request, not the default branch.
    if (/^pull_request(?!_target)/.test(run.event || "")) continue;
    if (run.run_number <= state.reset_after_run || run.run_number <= sinceRun) continue;
    let conclusion = run.conclusion;
    // A successful re-run can hide a failed first save.
    if ((run.run_attempt || 1) > 1)
      ({
        data: { conclusion },
      } = await githubClient.rest.actions.getWorkflowRunAttempt({ owner, repo, run_id: run.id, attempt_number: 1 }));
    /** @type {{id: string, number: number, conclusion: string, agent_ran?: boolean}} */
    const listed = { id, number: run.run_number, conclusion: conclusion || "unknown" };
    if (!SAVED_CONCLUSIONS.has(listed.conclusion)) {
      const jobs = await githubClient.paginate(githubClient.rest.actions.listJobsForWorkflowRunAttempt, { owner, repo, run_id: run.id, attempt_number: 1, per_page: 100 });
      listed.agent_ran = jobs.some((/** @type {any} */ job) => job?.name === "agent" && job.conclusion !== "skipped" && Boolean(job.started_at));
    }
    listing.push(listed);
  }
  return listing;
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
  const workflowFile = path.posix.basename(workflowRef.slice(0, workflowRef.lastIndexOf("@")));
  /** @type {any[]} */
  let listing = [];
  try {
    listing = await listRuns({ githubClient, owner, repo, workflowFile, defaultBranch, runId, state: initial.state, runs: initial.runs, generation: config.generation });
  } catch (error) {
    // Runs this save cannot charge stay on the list for the next save.
    core.warning(`Could not list earlier runs to charge unsaved ones: ${getErrorMessage(error)}`);
  }

  const run = { id: runId, number: Number(ctx.runNumber), attempt };
  /** @type {any[]} */
  let recorded = [];
  /** @param {{state: HistoryState, runs: any[]}} history */
  const record = history => {
    const result = applyRun(history.state, history.runs, { workflow: config.workflow, run, at: timestamp(), generation: config.generation, mode: config.mode, scope: config.scope, ...evidence, receipt: null, prepare: null, listing });
    recorded = result.lines;
    if (result.lines.length > 0) commitHistory(workspaceDir, result.state, trimRuns([...history.runs, ...result.lines]), runId);
    return result.lines.length > 0;
  };
  if (!record(initial)) {
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

module.exports = { checkoutHistoryBranch, ineligibility, listRuns, main, pairName, readConfig, readHistory, runCost, sessionEvidence };
