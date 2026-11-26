import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { execFileSync } from "node:child_process";
import { writeUnifiedSession } from "./unified_session.cjs";

const req = require;
const fixtures = req("./fixtures/routing_analytics_runs.cjs");
const { readJsonlFile } = req("./jsonl_helpers.cjs");
const { applyRun, emptyState, trimRuns } = req("./routing_history.cjs");
const { fetchStreamWindow, ineligibility, listRuns, main, readConfig, runCost, sessionEvidence } = req("./push_routing_history.cjs");

const BRANCH = "memory/adaptive-routing/triage";
const GENERATION = `${"a".repeat(64)}:${"b".repeat(64)}`;
const PAIR = "github-copilot/model-a@high";

let root;
beforeEach(() => {
  root = fs.mkdtempSync(path.join(os.tmpdir(), "routing-history-writer-"));
  global.core = { info: vi.fn(), warning: vi.fn(), debug: vi.fn(), error: vi.fn(), setFailed: vi.fn(), summary: { addRaw: vi.fn().mockReturnThis(), write: vi.fn() } };
});
afterEach(() => {
  vi.useRealTimers();
  delete global.core;
  fs.rmSync(root, { recursive: true, force: true });
});

const git = (cwd, ...args) => execFileSync("git", args, { cwd, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] }).trim();

function bareRemote() {
  const dir = path.join(root, "remote.git");
  execFileSync("git", ["init", "--quiet", "--bare", dir]);
  git(dir, "config", "user.name", "remote");
  git(dir, "config", "user.email", "remote@example.com");
  return dir;
}

const remoteHead = remote =>
  execFileSync("git", ["ls-remote", remote, `refs/heads/${BRANCH}`], { encoding: "utf8" })
    .trim()
    .split(/\s+/)[0] || "";
const remoteFile = (remote, file) => git(remote, "show", `${BRANCH}:${file}`);
const remoteState = remote => JSON.parse(remoteFile(remote, "state.json"));
const remoteRuns = remote =>
  remoteFile(remote, "runs.jsonl")
    .split("\n")
    .map(line => JSON.parse(line));

/**
 * GraphQL's createCommitOnBranch, reduced to what the save loop relies on: the commit lands only
 * while the branch is still at the expected head.
 */
function signedPush(remote, { lose = 0, fail = false } = {}) {
  const pushes = [];
  const push = async ({ branch, baseRef, cwd }) => {
    const head = remoteHead(remote);
    if (fail || head !== baseRef) throw new Error(`ERR_API: expected head ${baseRef} but ${branch} is at ${head || "nothing"}`);
    execFileSync("git", ["push", "--quiet", `--force-with-lease=refs/heads/${branch}:${baseRef}`, remote, `HEAD:refs/heads/${branch}`], { cwd, stdio: "pipe" });
    pushes.push(git(cwd, "rev-parse", "HEAD"));
    // The commit landed, but the response never arrived.
    if (pushes.length <= lose) throw new Error("ERR_API: connection reset");
    return pushes.at(-1);
  };
  return Object.assign(push, { pushes });
}

const lsRemote = async (command, args, options) => ({ stdout: execFileSync(command, args, { cwd: options.cwd, encoding: "utf8" }), stderr: "", exitCode: 0 });

function matchesCreatedFilter(createdAt, created) {
  if (!created) return true;
  const ts = Date.parse(createdAt);
  if (!Number.isFinite(ts)) return false;
  if (created.startsWith("<=")) return ts <= Date.parse(created.slice(2));
  if (created.startsWith("<")) return ts < Date.parse(created.slice(1));
  if (created.startsWith(">=")) return ts >= Date.parse(created.slice(2));
  if (created.startsWith(">")) return ts > Date.parse(created.slice(1));
  const rangeIdx = created.indexOf("..");
  if (rangeIdx >= 0) {
    const lo = Date.parse(created.slice(0, rangeIdx));
    const hi = Date.parse(created.slice(rangeIdx + 2));
    return ts >= lo && ts <= hi;
  }
  return ts === Date.parse(created);
}

const runCreatedAt = number => new Date(Date.parse("2026-11-01T00:00:00Z") + number * 60_000).toISOString().replace(/\.\d{3}Z$/, "Z");

function paginationRun(number, overrides = {}) {
  return { id: 9000 + number, run_number: number, created_at: runCreatedAt(number), status: "completed", conclusion: "success", head_branch: "main", event: "schedule", run_attempt: 1, ...overrides };
}

function reconcilePagination(githubClient, floor, runNumber) {
  return listRuns({
    githubClient,
    owner: "octo",
    repo: "repo",
    workflowFile: "triage.lock.yml",
    defaultBranch: "main",
    runId: String(9000 + runNumber),
    runNumber,
    state: { ...emptyState("triage"), generation: { hash: GENERATION, since_run: 1 }, last_run: floor, reconciled_through: floor, unresolved_runs: [] },
    runs: [],
    generation: GENERATION,
  });
}

function fakeGitHub(remote, { runs = [], jobs = {}, attempts = {}, contents = {}, runsById = {}, listWorkflowRuns } = {}) {
  const calls = { jobs: [], jobParams: [], attempts: [], attemptParams: [], lists: [], contents: [], runs: [] };
  const client = {
    calls,
    rest: {
      repos: {
        get: async () => ({ data: { default_branch: "main" } }),
        getContent: async ({ path: filePath, ref }) => {
          calls.contents.push({ path: filePath, ref });
          const text = contents[`${ref}:${filePath}`] ?? contents[ref];
          if (text === undefined) throw Object.assign(new Error("Not Found"), { status: 404 });
          return { data: { content: Buffer.from(text, "utf8").toString("base64"), encoding: "base64" } };
        },
      },
      git: {
        createCommit: async ({ message, tree, parents }) => ({ data: { sha: git(remote, "commit-tree", tree, "-m", message, ...parents.flatMap(parent => ["-p", parent])), verification: { verified: true } } }),
        createRef: async ({ ref, sha }) => {
          try {
            git(remote, "update-ref", ref, sha, "0".repeat(40));
          } catch {
            throw Object.assign(new Error("Reference already exists"), { status: 422 });
          }
          return { data: { ref } };
        },
      },
      actions: {
        listWorkflowRuns: async params => {
          calls.lists.push(params);
          if (listWorkflowRuns) return listWorkflowRuns(params);
          const { branch, event, created, page = 1, per_page = 100 } = params;
          const filtered = runs.filter(run => (!branch || run.head_branch === branch) && (!event || run.event === event) && matchesCreatedFilter(run.created_at, created));
          const start = (page - 1) * per_page;
          // GitHub Actions caps paginated listWorkflowRuns results at 1,000 runs even when total_count is larger.
          const capped = filtered.slice(0, 1000);
          return { data: { total_count: filtered.length, workflow_runs: capped.slice(start, start + per_page) } };
        },
        getWorkflowRun: async ({ run_id }) => {
          calls.runs.push(run_id);
          const found = runsById[run_id] ?? runs.find(run => String(run.id) === String(run_id));
          if (!found) throw Object.assign(new Error("Not Found"), { status: 404 });
          return { data: found };
        },
        getWorkflowRunAttempt: async params => {
          calls.attempts.push(params.run_id);
          calls.attemptParams.push(params);
          return { data: attempts[params.run_id] };
        },
        listJobsForWorkflowRunAttempt: async ({ run_id }) => ({ data: { jobs: jobs[run_id] || [] } }),
      },
    },
    paginate: async (method, params, mapPage) => {
      calls.jobs.push(params.run_id);
      calls.jobParams.push(params);
      const data = (await method(params)).data.jobs;
      return mapPage ? mapPage({ data, headers: {} }) : data;
    },
  };
  return client;
}

/** A routed run's session: AW-1's verdict and AWF's selection, request and usage records. */
function sessionFile(name, outcome, { model = "model-a", aic = 2 } = {}) {
  const firewall = (type, data) => ({ type, data, provenance: { component: "firewall", phase: "agent" } });
  const events = [
    firewall("firewall.model_routing", { stage: "selection", selectedProvider: "copilot", wireModel: model, selectedEffort: "high" }),
    firewall("firewall.model_routing", { stage: "request", requestId: "r1", routed: "as_selected", requestedModel: model }),
    firewall("firewall.token_usage", { requestId: "r1", provider: "copilot", aic, usage: { inputTokens: 100, cacheReadInputTokens: 900, cacheCreationInputTokens: 0, outputTokens: 50 } }),
  ];
  if (outcome) events.unshift({ type: "routing.observation", data: outcome === "skip" ? { outcome, reason: "infrastructure" } : { outcome }, provenance: { component: "collector", phase: "conclusion" } });
  const file = path.join(root, `${name}.jsonl`);
  fs.writeFileSync(file, events.map(event => `${JSON.stringify(event)}\n`).join(""));
  return file;
}

/** One run's push_routing_history job. */
function save(remote, number, outcome, options = {}) {
  const {
    github = fakeGitHub(remote, { runs: Array.from({ length: number }, (_, i) => paginationRun(number - i)) }),
    push = signedPush(remote),
    attempt = "1",
    ref = "refs/heads/main",
    sleepFn = async () => {},
    session,
    generation = GENERATION,
  } = options;
  const workspaceDir = fs.mkdtempSync(path.join(root, `run-${number}-`));
  return main({
    github,
    context: { repo: { owner: "octo", repo: "demo" }, runId: 9000 + number, runNumber: number, payload: { repository: { default_branch: "main" } } },
    env: {
      GH_AW_ROUTING_WORKFLOW: "triage",
      GH_AW_ROUTING_GENERATION: generation,
      GH_AW_ROUTING_MODE: "observe",
      GH_AW_ROUTING_ENGINE: "copilot",
      GH_AW_ROUTING_GOAL: "cost",
      GH_AW_ROUTING_LEVEL: "",
      GH_AW_ROUTING_SESSION_FILE: session ?? sessionFile(`session-${number}`, outcome),
      GITHUB_RUN_ATTEMPT: attempt,
      GITHUB_WORKFLOW_REF: `octo/demo/.github/workflows/triage.lock.yml@${ref}`,
      GH_TOKEN: "test-token",
      GITHUB_SERVER_URL: "https://github.com",
    },
    workspaceDir,
    remoteUrl: remote,
    pushSignedCommitsFn: push,
    execGetExecOutput: lsRemote,
    sleepFn,
  });
}

function seedHistory(remote, state, runs) {
  const dir = fs.mkdtempSync(path.join(root, "seed-"));
  execFileSync("git", ["init", "--quiet", dir]);
  git(dir, "config", "user.name", "seed");
  git(dir, "config", "user.email", "seed@example.com");
  fs.writeFileSync(path.join(dir, "state.json"), `${JSON.stringify(state)}\n`);
  fs.writeFileSync(path.join(dir, "runs.jsonl"), runs.map(line => `${JSON.stringify(line)}\n`).join(""));
  git(dir, "add", "--", "state.json", "runs.jsonl");
  git(dir, "commit", "-m", "Seed history");
  git(dir, "push", "--quiet", remote, `HEAD:refs/heads/${BRANCH}`);
}

describe("push_routing_history evidence", () => {
  /**
   * Write the run's unified session the way the conclusion job does, from AW-1's fixtures.
   * `includeCache` adds the flag AWF's meter uses to every token usage record.
   */
  function fixtureSession(id, { includeCache } = {}) {
    const dir = path.join(root, id);
    fs.cpSync(fixtures.runDir(id), dir, { recursive: true, filter: file => path.basename(file) !== "aw_session.jsonl" });
    if (includeCache !== undefined) {
      const log = path.join(dir, "sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl");
      const records = [...readJsonlFile(log, { strict: true })].map(record => ({ ...record, input_tokens_include_cache: includeCache }));
      fs.writeFileSync(log, records.map(record => `${JSON.stringify(record)}\n`).join(""));
    }
    if (!fs.existsSync(path.join(dir, "agent_output.json"))) fs.writeFileSync(path.join(dir, "agent_output.json"), JSON.stringify({ items: [{ type: "create_issue" }], errors: [] }));
    const outputPath = path.join(dir, "usage/aw_session.jsonl");
    const jobResults = JSON.stringify({ activation: { result: "success" }, agent: { result: "success" } });
    writeUnifiedSession({ rootDir: dir, outputPath, warn: () => {}, dailyAIC: {}, admission: {}, jobDurations: {}, jobResults, routingObservation: true });
    return [...readJsonlFile(outputPath, { strict: true })];
  }

  it("derives run 39508154330's pair and cost from AWF's agent records, leaving its sub-agent out of the model's share", () => {
    const events = fixtureSession("39508154330");
    expect(sessionEvidence(events, fixtures.awInfo("39508154330").sub_agent_models)).toEqual({
      observation: { outcome: "success" },
      pair: "github-copilot/gpt-5.6-luna@max",
      cost: { credits: 3.180764, model_credits: 0.652539, tokens: [12, 56302, 19159, 506, null] },
    });
  });

  it("derives run 39147795806's pair and cost, counting only new input where its records' input includes the cache", () => {
    // Its later requests log input that includes the cache read, such as 23,799 with 23,194 cached.
    expect(sessionEvidence(fixtureSession("39147795806"))).toEqual({
      observation: { outcome: "success" },
      pair: "github-copilot/gpt-5.6-luna@max",
      cost: { credits: 0.713574, model_credits: 0.713574, tokens: [24093, 70857, 0, 750, null] },
    });
    expect(sessionEvidence(fixtureSession("39147795806", { includeCache: false })).cost.tokens).toEqual([94950, 70857, 0, 750, null]);
  });

  it("counts input without cache as AWF's meter does, using record.provider for Anthropic and OpenAI and the heuristic for Copilot", () => {
    const firewall = (type, data) => ({ type, data, provenance: { component: "firewall", phase: "agent" } });
    const tokensFor = (provider, ...usages) =>
      sessionEvidence([
        firewall("firewall.model_routing", { stage: "selection", selectedProvider: provider || "copilot", wireModel: "model-a", selectedEffort: "high" }),
        ...usages.flatMap((usage, index) => [
          firewall("firewall.model_routing", { stage: "request", requestId: `r${index}`, routed: "as_selected", requestedModel: "model-a" }),
          firewall("firewall.token_usage", { requestId: `r${index}`, ...(provider !== undefined ? { provider } : {}), aic: 1, usage }),
        ]),
      ]).cost.tokens;
    const uncached = { inputTokens: 23197, cacheReadInputTokens: 0, cacheCreationInputTokens: 0, outputTokens: 400 };
    // Input that covers the cache can include it; input smaller than the cache cannot.
    const covering = { inputTokens: 23799, cacheReadInputTokens: 23194, cacheCreationInputTokens: 0, outputTokens: 45 };
    const smaller = { inputTokens: 382, cacheReadInputTokens: 19712, cacheCreationInputTokens: 0, outputTokens: 69 };
    const coldAnthropic = { inputTokens: 20000, cacheReadInputTokens: 0, cacheCreationInputTokens: 15000, outputTokens: 300 };

    // Copilot (and other/missing providers) keep the input >= cached heuristic unless the flag is set.
    expect(tokensFor("copilot", uncached, covering, smaller)).toEqual([24184, 42906, 0, 514, null]);
    expect(tokensFor("copilot", { ...covering, inputTokensIncludeCache: false })).toEqual([23799, 23194, 0, 45, null]);
    expect(tokensFor("copilot", { ...smaller, inputTokensIncludeCache: true })).toEqual([0, 19712, 0, 69, null]);
    expect(tokensFor("gemini", covering, smaller)).toEqual([987, 42906, 0, 114, null]);
    expect(tokensFor(undefined, covering, smaller)).toEqual([987, 42906, 0, 114, null]);

    // Anthropic input tokens are additive even when input >= cacheWrite, unless AWF explicitly sets the flag.
    expect(tokensFor("anthropic", coldAnthropic)).toEqual([20000, 0, 15000, 300, null]);
    expect(tokensFor("anthropic", { ...coldAnthropic, inputTokensIncludeCache: true })).toEqual([5000, 0, 15000, 300, null]);

    // OpenAI input tokens include cached tokens (clamped at 0 when smaller), unless AWF explicitly sets the flag.
    expect(tokensFor("openai", covering)).toEqual([605, 23194, 0, 45, null]);
    expect(tokensFor("openai", smaller)).toEqual([0, 19712, 0, 69, null]);
    expect(tokensFor("openai", { ...covering, inputTokensIncludeCache: false })).toEqual([23799, 23194, 0, 45, null]);
  });

  it("has no verdict for a run AWF did not route, and no pair to count a verdict for without a selection", () => {
    expect(sessionEvidence([]).observation).toBeNull();
    const verdictOnly = [{ type: "routing.observation", data: { outcome: "success" }, provenance: { component: "collector", phase: "conclusion" } }];
    expect(sessionEvidence(verdictOnly)).toMatchObject({ observation: { outcome: "skip", reason: "unknown_evidence" }, pair: null });
  });

  it("names a provider it does not rename exactly as AWF wrote it, even one that matches an object property", () => {
    const selection = provider => [{ type: "firewall.model_routing", data: { stage: "selection", selectedProvider: provider, wireModel: "model-a", selectedEffort: "high" }, provenance: { component: "firewall", phase: "agent" } }];
    expect(sessionEvidence(selection("copilot")).pair).toBe("github-copilot/model-a@high");
    expect(sessionEvidence(selection("constructor")).pair).toBe("constructor/model-a@high");
  });

  it("takes the verdict only from the conclusion job's collector", () => {
    const routed = [
      { type: "firewall.model_routing", data: { stage: "selection", selectedProvider: "copilot", wireModel: "model-a", selectedEffort: "high" }, provenance: { component: "firewall", phase: "agent" } },
      { type: "routing.observation", data: { outcome: "failure" }, provenance: { component: "collector", phase: "conclusion" } },
    ];
    expect(sessionEvidence(routed).observation).toEqual({ outcome: "failure" });
    // A verdict from any other source cannot replace it or stand in for a missing one.
    const forged = { type: "routing.observation", data: { outcome: "success" }, provenance: { component: "engine", phase: "agent" } };
    expect(sessionEvidence([...routed, forged]).observation).toEqual({ outcome: "failure" });
    expect(sessionEvidence([routed[0], forged]).observation).toBeNull();
  });

  it("treats a request without a price or a count as an unknown cost, not a free one", () => {
    const firewall = (type, data) => ({ type, data, provenance: { component: "firewall", phase: "agent" } });
    const events = [
      firewall("firewall.model_routing", { stage: "selection", selectedProvider: "anthropic", wireModel: "model-b", selectedEffort: null }),
      firewall("firewall.model_routing", { stage: "request", requestId: "r1", routed: "as_selected", requestedModel: "model-b" }),
      firewall("firewall.token_usage", { requestId: "r1", provider: "anthropic", usage: { inputTokens: 1, outputTokens: 2 } }),
    ];
    expect(sessionEvidence(events)).toEqual({ observation: null, pair: "anthropic/model-b@none", cost: { credits: null, model_credits: null, tokens: null } });
  });

  it("emits null model_credits when an unpriced sub-agent makes total credits null, while preserving main tokens", () => {
    const routing = [
      { stage: "selection", selectedProvider: "copilot", wireModel: "model-a", selectedEffort: "high" },
      { stage: "request", request_id: "r-main", routed: "as_selected", requested_model: "model-a" },
      { stage: "request", request_id: "r-sub", routed: "deviated", requested_model: "sub-model" },
    ];
    const usage = [
      { requestId: "r-main", provider: "copilot", aic: 30, usage: { inputTokens: 1000, cacheReadInputTokens: 400, cacheCreationInputTokens: 0, outputTokens: 200 } },
      { requestId: "r-sub", provider: "copilot", usage: { inputTokens: 500, cacheReadInputTokens: 0, cacheCreationInputTokens: 0, outputTokens: 100 } },
    ];
    expect(runCost(usage, routing, ["sub-model"])).toEqual({
      credits: null,
      model_credits: null,
      tokens: [600, 400, 0, 200, null],
    });
  });
});

describe("push_routing_history configuration", () => {
  it("records only first attempts whose definition comes from the default branch", () => {
    const ref = branch => `octo/demo/.github/workflows/triage.lock.yml@${branch}`;
    expect(ineligibility({ attempt: 1, workflowRef: ref("refs/heads/main"), defaultBranch: "main" })).toBe("");
    expect(ineligibility({ attempt: 2, workflowRef: ref("refs/heads/main"), defaultBranch: "main" })).toMatch(/re-run/);
    expect(ineligibility({ attempt: 1, workflowRef: ref("refs/pull/7/merge"), defaultBranch: "main" })).toMatch(/refs\/pull\/7\/merge/);
    expect(ineligibility({ attempt: 1, workflowRef: ref("refs/heads/feature"), defaultBranch: "main" })).toMatch(/not the default branch/);
  });

  it("builds the scope from the compiled engine, goal and level, sharing records unless the level is chosen at run time", () => {
    const env = { GH_AW_ROUTING_WORKFLOW: "triage", GH_AW_ROUTING_GENERATION: GENERATION, GH_AW_ROUTING_MODE: "observe", GH_AW_ROUTING_ENGINE: "claude", GH_AW_ROUTING_GOAL: "cost" };
    expect(readConfig(env)).toMatchObject({ scope: "claude/cost/shared", branch: "memory/adaptive-routing/triage" });
    expect(readConfig({ ...env, GH_AW_ROUTING_LEVEL: "robust" }).scope).toBe("claude/cost/robust");
    expect(readConfig({ ...env, GH_AW_ROUTING_LEVEL: "auto" }).scope).toBe("claude/cost/shared");
    expect(() => readConfig({ ...env, GH_AW_ROUTING_MODE: "live" })).toThrow(/observe/);
    expect(() => readConfig({ ...env, GH_AW_ROUTING_WORKFLOW: "../main" })).toThrow(/workflow ID/);
  });
});

describe("push_routing_history saves", () => {
  it("creates the branch on the first write and records the run", async () => {
    const remote = bareRemote();
    await save(remote, 1, "success");
    expect(global.core.setFailed).not.toHaveBeenCalled();
    expect(remoteRuns(remote)).toEqual([expect.objectContaining({ run: "9001", number: 1, attempt: 1, mode: "observe", role: "observe", pair: PAIR, outcome: "success", credits: 2, counted: true, note: null })]);
    const state = remoteState(remote);
    expect(state).toMatchObject({ v: 2, workflow: "triage", seq: 1, generation: { hash: GENERATION, since_run: 1 }, last_run: 1 });
    expect(state.scopes["copilot/cost/shared"].pairs[PAIR].history).toEqual([[1, expect.any(String), true, 2, 2, [100, 900, 0, 50, null]]]);
    // The branch starts from its own root commit, not from the repository's history.
    expect(git(remote, "rev-list", "--count", BRANCH)).toBe("2");
  });

  it("lands two saves that read the same state, re-applying the later one to the newer file", async () => {
    const remote = bareRemote();
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date("2026-11-24T09:00:00Z"));
    for (let number = 31; number <= 40; number++) await save(remote, number, number === 35 ? "failure" : "success");
    const before = remoteFile(remote, "runs.jsonl");
    vi.setSystemTime(new Date("2026-11-24T10:00:00Z"));
    const later = async () => vi.setSystemTime(new Date("2026-11-24T10:00:05Z"));
    await Promise.all([save(remote, 41, "success", { sleepFn: later }), save(remote, 42, "failure", { sleepFn: later })]);

    expect(global.core.setFailed).not.toHaveBeenCalled();
    const history = remoteState(remote).scopes["copilot/cost/shared"].pairs[PAIR].history;
    expect(history).toHaveLength(12);
    expect(history.filter(entry => entry[2])).toHaveLength(10);
    const runs = remoteRuns(remote);
    expect(runs.map(line => line.number)).toEqual([31, 32, 33, 34, 35, 36, 37, 38, 39, 40, 41, 42]);
    // Earlier lines never change, and each line's time is the attempt that committed it.
    expect(remoteFile(remote, "runs.jsonl").startsWith(before)).toBe(true);
    expect(runs.slice(-2).map(line => line.at)).toEqual(["2026-11-24T10:00:00Z", "2026-11-24T10:00:05Z"]);
  });

  it("does not count a run twice when its save landed but the acknowledgment was lost", async () => {
    const remote = bareRemote();
    await save(remote, 1, "success");
    const push = signedPush(remote, { lose: 1 });
    await save(remote, 2, "success", { push });
    expect(push.pushes).toHaveLength(1);
    expect(global.core.setFailed).not.toHaveBeenCalled();
    expect(remoteRuns(remote).map(line => line.number)).toEqual([1, 2]);
    expect(remoteState(remote).seq).toBe(2);
  });

  it("resolves a race to create the branch", async () => {
    const remote = bareRemote();
    await Promise.all([save(remote, 1, "success"), save(remote, 2, "failure")]);
    expect(global.core.setFailed).not.toHaveBeenCalled();
    expect(
      remoteRuns(remote)
        .map(line => line.number)
        .sort()
    ).toEqual([1, 2]);
    expect(remoteState(remote).seq).toBe(2);
  });

  it("fails the job with a summary when every attempt finds the branch moved", async () => {
    const remote = bareRemote();
    await save(remote, 1, "success");
    await save(remote, 2, "success", { push: signedPush(remote, { fail: true }) });
    expect(global.core.setFailed).toHaveBeenCalledWith(expect.stringMatching(/Failed to push changes after 11 attempts/));
    expect(remoteRuns(remote).map(line => line.number)).toEqual([1]);
  });

  it("records a failed run, a skipped verdict and a run whose agent never reached the model, counting only the failure", async () => {
    const remote = bareRemote();
    await save(remote, 1, "failure");
    await save(remote, 2, "skip");
    await save(remote, 3, null);
    expect(remoteRuns(remote).map(({ number, outcome, counted, note }) => ({ number, outcome, counted, note }))).toEqual([
      { number: 1, outcome: "failure", counted: true, note: null },
      { number: 2, outcome: "skip", counted: false, note: "infrastructure" },
      { number: 3, outcome: null, counted: false, note: "no_agent" },
    ]);
    expect(remoteState(remote)).toMatchObject({ seq: 1, last_run: 3 });
  });

  it("fails without a usage artifact, so a later save charges the run", async () => {
    const remote = bareRemote();
    await expect(save(remote, 1, "success", { session: path.join(root, "missing.jsonl") })).rejects.toThrow(/usage artifact/);
    expect(remoteHead(remote)).toBe("");
  });

  it("skips re-runs and definitions outside the default branch without writing", async () => {
    const remote = bareRemote();
    await save(remote, 1, "success", { attempt: "2" });
    await save(remote, 2, "success", { ref: "refs/pull/3/merge" });
    expect(remoteHead(remote)).toBe("");
  });

  it("charges completed runs whose save never landed, after the fences, looking up only what it must", async () => {
    const remote = bareRemote();
    await save(remote, 50, "success");
    const run = (number, conclusion, extra = {}) => ({ id: 9000 + number, run_number: number, status: "completed", conclusion, head_branch: "main", event: "schedule", run_attempt: 1, ...extra });
    const github = fakeGitHub(remote, {
      runs: [
        run(55, "success", { run_attempt: 2 }),
        run(54, "failure", { event: "pull_request" }),
        run(53, null, { status: "in_progress" }),
        run(52, "failure"),
        run(51, "cancelled"),
        run(50, "success"),
        run(49, "failure"),
        run(48, "skipped"),
      ],
      attempts: { 9055: { conclusion: "failure" } },
      jobs: {
        9055: [
          { name: "agent", conclusion: "failure", started_at: "2026-11-24T09:00:00Z" },
          { name: "push_routing_history", conclusion: "failure", started_at: "2026-11-24T09:10:00Z" },
        ],
        9052: [
          { name: "agent", conclusion: "skipped", started_at: null },
          { name: "push_routing_history", conclusion: "skipped", started_at: null },
        ],
        9051: [
          { name: "agent", conclusion: "cancelled", started_at: "2026-11-24T09:00:00Z" },
          { name: "push_routing_history", conclusion: "failure", started_at: "2026-11-24T09:10:00Z" },
        ],
      },
    });
    await save(remote, 56, "success", { github });

    expect(remoteRuns(remote).map(({ number, note, counted }) => ({ number, note, counted }))).toEqual([
      { number: 50, note: null, counted: true },
      { number: 51, note: "unsaved", counted: false },
      { number: 52, note: "no_agent", counted: false },
      { number: 55, note: "unsaved", counted: false },
      { number: 56, note: null, counted: true },
    ]);
    expect(remoteState(remote).unexplained).toBe(2);
    // A re-run is judged by its first attempt; runs before the generation, successes and pull requests are not looked up.
    expect(github.calls.attempts).toEqual([9055]);
    expect(github.calls.jobs.sort()).toEqual([9051, 9052, 9055]);
  });

  it.each(["empty", "verified", "reset"])("accounts for failed saves exactly once starting with %s history", async start => {
    const remote = bareRemote();
    if (start === "verified") await save(remote, 9, "success");
    if (start === "reset") seedHistory(remote, { ...emptyState("triage"), reset_after_run: 9 }, []);
    const agent = { name: "agent", conclusion: "success", started_at: runCreatedAt(10) };
    const failedSave = [agent, { name: "push_routing_history", conclusion: "failure" }];
    const runs = Array.from({ length: 12 }, (_, i) => paginationRun(12 - i));
    const priorRuns = start === "verified" ? remoteRuns(remote) : [];
    const failures = new Set([7, 8, 10, 11]);
    const github = fakeGitHub(remote, {
      runs: runs.map(run => (run.run_number === 11 ? { ...run, run_attempt: 2, status: "in_progress", conclusion: null } : { ...run, conclusion: failures.has(run.run_number) ? "failure" : "success" })),
      attempts: { 9011: { status: "completed", conclusion: "failure" } },
      // Neither the pre-opt-in run (#7) nor the dry run (#8) had a history job.
      jobs: { 9007: [agent], 9008: [agent], 9010: failedSave, 9011: failedSave },
    });
    await save(remote, 12, "success", { github });
    const saved = remoteRuns(remote).slice(priorRuns.length);
    expect(saved.map(({ number, attempt, note, counted, outcome }) => ({ number, attempt, note, counted, outcome }))).toEqual([
      { number: 10, attempt: 1, note: "unsaved", counted: false, outcome: null },
      { number: 11, attempt: 1, note: "unsaved", counted: false, outcome: null },
      { number: 12, attempt: 1, note: null, counted: true, outcome: "success" },
    ]);
    expect(remoteState(remote)).toMatchObject({ last_run: 12, reconciled_through: 12, unresolved_runs: [], unexplained: 2 });
    expect(github.calls.jobs).toEqual(start === "empty" ? [9007, 9008, 9010, 9011] : [9010, 9011]);

    await save(remote, 13, "success", { github });
    expect(remoteRuns(remote).filter(line => line.note === "unsaved")).toEqual(saved.slice(0, 2));
    expect(remoteState(remote)).toMatchObject({ reconciled_through: 13, unexplained: 2 });
    expect(github.calls.attempts).toEqual([9011]);
  });

  it("retries from the beginning when the first writer could not verify earlier runs", async () => {
    const remote = bareRemote();
    const github = fakeGitHub(remote, {
      listWorkflowRuns: async () => {
        throw new Error("503 service unavailable");
      },
    });
    await save(remote, 3, "success", { github });
    expect(remoteState(remote)).toMatchObject({ last_run: 3, reconciled_through: 0, unexplained: 0 });
    expect(remoteRuns(remote).map(line => line.number)).toEqual([3]);

    await save(remote, 4, "success", {
      github: fakeGitHub(remote, {
        runs: [paginationRun(3), paginationRun(2, { conclusion: "failure" }), paginationRun(1)],
        jobs: {
          9002: [
            { name: "agent", started_at: runCreatedAt(2), conclusion: "success" },
            { name: "push_routing_history", conclusion: "failure" },
          ],
        },
      }),
    });
    expect(remoteRuns(remote).map(({ number, note }) => [number, note])).toEqual([
      [3, null],
      [2, "unsaved"],
      [4, null],
    ]);
    expect(remoteState(remote)).toMatchObject({ last_run: 4, reconciled_through: 4, unexplained: 1 });
  });

  it.each([
    { first: 1, gap: 0 },
    { first: 1, gap: 999 },
    { first: 1, gap: 3000 },
    { first: 2001, gap: 3000 },
  ])("bootstraps a 10,000-run workflow (first surviving run: $first, interior deletion: $gap)", async ({ first, gap }) => {
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date(runCreatedAt(10101)));
    const afterGap = 4001 + gap;
    const apiRuns = Array.from({ length: 10001 - first }, (_, i) => paginationRun(10000 - i)).filter(run => run.run_number <= 4000 || run.run_number >= afterGap);
    apiRuns.at(-2).conclusion = "failure";
    apiRuns.find(run => run.run_number === afterGap).conclusion = "failure";
    const failedSave = [
      { name: "agent", started_at: runCreatedAt(2), conclusion: "success" },
      { name: "push_routing_history", conclusion: "failure" },
    ];
    const github = fakeGitHub(null, { runs: apiRuns, jobs: { [9001 + first]: failedSave, [9000 + afterGap]: failedSave, 19001: failedSave } });
    let state = emptyState("triage");
    let rows = [];
    for (let number = 10000; number <= 10100; number++) {
      if (number === 10001) {
        apiRuns.unshift(paginationRun(number, { conclusion: "failure" }));
        continue;
      }
      if (number > 10000) apiRuns.unshift(paginationRun(number));
      const id = String(9000 + number);
      const coverage = await listRuns({ githubClient: github, owner: "octo", repo: "demo", workflowFile: "triage.lock.yml", defaultBranch: "main", runId: id, runNumber: number, state, runs: rows, generation: GENERATION });
      const saved = applyRun(state, rows, { workflow: "triage", generation: GENERATION, scope: "copilot/cost/shared", mode: "observe", run: { id, number, attempt: 1 }, at: runCreatedAt(number), observation: null, pair: null, ...coverage });
      expect(saved.state.reconciled_through).toBeGreaterThan(state.reconciled_through ?? 0);
      rows = trimRuns([...rows, ...saved.lines]);
      state = saved.state;
      if (number === 10000) expect(state.reconciled_through).toBeLessThan(number);
      if (state.reconciled_through === number) break;
    }
    expect(state.reconciled_through).toBe(state.last_run);
    expect(state).toMatchObject({ unexplained: 3, unresolved_runs: [] });
    expect(rows.filter(row => row.note === "unsaved").map(row => row.number)).toEqual([first + 1, afterGap, 10001]);
    expect(github.calls.jobs).toEqual([9001 + first, 9000 + afterGap, 19001]);
    expect(github.calls.lists.every(call => call.page <= 10)).toBe(true);
  });

  it("recovers a failure outside the bootstrap cutoff instead of certifying its run number as deleted", async () => {
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date("2026-11-19T00:00:00Z"));
    const runs = Array.from({ length: 10000 }, (_, i) => paginationRun(10000 - i));
    const skewed = runs.splice(-2, 1)[0];
    runs.unshift({ ...skewed, conclusion: "failure", created_at: runCreatedAt(10001) });
    const github = fakeGitHub(null, {
      runs,
      jobs: {
        9002: [
          { name: "agent", started_at: runCreatedAt(2), conclusion: "success" },
          { name: "push_routing_history", conclusion: "failure" },
        ],
      },
    });
    const options = {
      githubClient: github,
      owner: "octo",
      repo: "demo",
      workflowFile: "triage.lock.yml",
      defaultBranch: "main",
      runId: "19000",
      runNumber: 10000,
      state: emptyState("triage"),
      runs: [],
      generation: GENERATION,
    };
    const list = github.rest.actions.listWorkflowRuns;
    github.rest.actions.listWorkflowRuns = params => (!params.created && !params.branch && !params.event ? Promise.resolve({ data: { total_count: 10000, workflow_runs: [] } }) : list(params));
    const incomplete = await listRuns(options);
    expect(incomplete.reconciliation).toMatchObject({ verified: true, ceiling: 1 });
    expect(github.calls.jobs).toEqual([]);

    github.rest.actions.listWorkflowRuns = list;
    const result = await listRuns(options);
    expect(result.reconciliation.verified).toBe(true);
    expect(result.listing).toContainEqual(expect.objectContaining({ id: "9002", number: 2, agent_ran: true }));
    expect(github.calls.jobs).toEqual([9002]);
  });

  it.each([10000, 15000])("continues past an early gap with %i run numbers without losing a later failed save", async size => {
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date(runCreatedAt(size + 2)));
    const remote = bareRemote();
    const runs = Array.from({ length: size }, (_, i) => paginationRun(size - i, { conclusion: size - i === 3 ? "failure" : "success" })).filter(run => run.run_number !== 2);
    const failed = [
      { name: "agent", started_at: runCreatedAt(1), conclusion: "success" },
      { name: "push_routing_history", conclusion: "failure" },
    ];
    const github = fakeGitHub(remote, { runs, jobs: { 9002: failed, 9003: failed } });
    const list = github.rest.actions.listWorkflowRuns;
    github.rest.actions.listWorkflowRuns = async params => {
      const result = await list(params);
      if (size === 10000) result.data.total_count = Math.min(2500, result.data.total_count);
      return result;
    };
    await save(remote, size, "success", { github });
    const first = remoteState(remote);
    expect(first.reconciled_through).toBeGreaterThan(3);
    expect(first.unexplained).toBe(1);
    expect(first.unresolved_runs).toEqual(size === 10000 ? [] : [2]);
    await save(remote, size + 1, "success", { github });
    expect(remoteState(remote).reconciled_through).toBeGreaterThan(first.reconciled_through);
    if (size === 15000) {
      expect(remoteState(remote).unresolved_runs).toEqual([2]);
      runs.unshift(paginationRun(2, { created_at: runCreatedAt(size + 2), conclusion: "failure" }));
      await save(remote, size + 2, "success", { github });
      expect(remoteState(remote)).toMatchObject({ unresolved_runs: [], unexplained: 2 });
    }
  });

  it("does not replenish the inspection budget when a concurrent save forces a replay", async () => {
    const remote = bareRemote();
    await save(remote, 1, "success");
    const runs = Array.from({ length: 70 }, (_, i) => paginationRun(70 - i, { conclusion: "failure" }));
    const github = fakeGitHub(remote, { runs });
    const push = signedPush(remote);
    let interrupted = false;
    await save(remote, 70, "success", {
      github,
      push: async args => {
        if (!interrupted) {
          interrupted = true;
          await save(remote, 71, "success", {
            github: fakeGitHub(remote, {
              listWorkflowRuns: async () => {
                throw new Error("listing unavailable");
              },
            }),
          });
        }
        return push(args);
      },
    });
    expect(github.calls.jobs).toHaveLength(64);
    expect(remoteRuns(remote).map(row => row.number)).toEqual([1, 71, 70]);
    expect(remoteState(remote).reconciled_through).toBe(1);
  });

  it("keeps a failed first-write generation lookup unresolved until the old definition is verified", async () => {
    const remote = bareRemote();
    const previous = `${"c".repeat(64)}:${"d".repeat(64)}`;
    const contents = {};
    const runs = [paginationRun(2), paginationRun(1, { head_sha: "old", conclusion: "failure" })];
    const github = fakeGitHub(remote, {
      runs,
      contents,
      jobs: {
        9001: [
          { name: "agent", conclusion: "success", started_at: runCreatedAt(1) },
          { name: "push_routing_history", conclusion: "failure" },
        ],
      },
    });
    await save(remote, 2, "success", { github });
    expect(remoteState(remote)).toMatchObject({ unexplained: 0, unresolved_runs: [1], unresolved_run_ids: { "1": "9001" } });
    expect(remoteRuns(remote).map(row => row.number)).toEqual([2]);
    contents.old = `GH_AW_ROUTING_GENERATION: "${previous}"`;
    await save(remote, 3, "success", { github });
    expect(remoteState(remote)).toMatchObject({ unexplained: 0, unresolved_runs: [] });
    expect(remoteRuns(remote).filter(row => row.number === 1)).toMatchObject([{ note: "older_definition", generation: previous }]);
  });

  it("checkpoints a failure-heavy bootstrap and budgets every job page before requesting the next", async () => {
    const runs = Array.from({ length: 901 }, (_, i) => paginationRun(901 - i, { conclusion: "failure" }));
    const github = fakeGitHub(null, { runs });
    const inspected = [];
    let pages = 0;
    github.paginate = async (_method, { run_id }, mapPage) => {
      inspected.push(run_id);
      for (let page = 0; page < 3; page++) {
        pages++;
        mapPage?.({ data: [], headers: page === 2 ? {} : { link: '<https://api.github.com/next>; rel="next"' } });
      }
      return [{ name: "agent", started_at: runCreatedAt(1), conclusion: "success" }];
    };
    const options = { githubClient: github, owner: "octo", repo: "demo", workflowFile: "triage.lock.yml", defaultBranch: "main", generation: GENERATION, runs: [] };
    const first = await listRuns({ ...options, runId: "9901", runNumber: 901, state: emptyState("triage") });
    expect(pages).toBe(64);
    expect(first.reconciliation).toMatchObject({ verified: true, ceiling: 21 });
    expect(first.listing).toEqual([]);
    inspected.length = 0;
    pages = 0;
    const next = await listRuns({ ...options, runId: "9902", runNumber: 902, state: { ...emptyState("triage"), generation: { hash: GENERATION, since_run: 901 }, last_run: 901, reconciled_through: first.reconciliation.ceiling } });
    expect(inspected[0]).toBe(9022);
    expect(pages).toBe(64);
    expect(next.reconciliation.ceiling).toBe(42);
  });

  it("does not charge failed or cancelled runs compiled without the history job, such as dry runs", async () => {
    const remote = bareRemote();
    await save(remote, 50, "success");
    const run = (number, conclusion = "failure") => ({ id: 9000 + number, run_number: number, status: "completed", conclusion, head_branch: "main", event: "schedule", run_attempt: 1 });
    const agent = { name: "agent", conclusion: "failure", started_at: "2026-11-24T09:00:00Z" };
    const github = fakeGitHub(remote, {
      runs: [run(52, "cancelled"), run(51, "failure")],
      // Run 51 ran while adaptive routing was off; run 52 came from a dry-run compilation, which omits push_routing_history.
      jobs: { 9051: [agent], 9052: [{ name: "agent", conclusion: "cancelled", started_at: "2026-11-24T09:00:00Z" }] },
    });
    await save(remote, 53, "success", { github });
    expect(remoteRuns(remote).map(line => line.number)).toEqual([50, 53]);
    expect(remoteState(remote).unexplained).toBe(0);
  });

  it("charges real cancelled runs whose agent started when push_routing_history ended cancelled or skipped", async () => {
    const remote = bareRemote();
    await save(remote, 50, "success");
    const run = (number, conclusion = "cancelled") => ({ id: 9000 + number, run_number: number, status: "completed", conclusion, head_branch: "main", event: "schedule", run_attempt: 1 });
    const agent = { name: "agent", conclusion: "cancelled", started_at: "2026-11-24T09:00:00Z" };
    const github = fakeGitHub(remote, {
      runs: [run(52), run(51)],
      jobs: {
        9051: [agent, { name: "push_routing_history", conclusion: "cancelled", started_at: "2026-11-24T09:10:00Z" }],
        9052: [agent, { name: "push_routing_history", conclusion: "skipped", started_at: null }],
      },
    });
    await save(remote, 53, "success", { github });
    expect(remoteRuns(remote).map(({ number, note, counted }) => ({ number, note, counted }))).toEqual([
      { number: 50, note: null, counted: true },
      { number: 51, note: "unsaved", counted: false },
      { number: 52, note: "unsaved", counted: false },
      { number: 53, note: null, counted: true },
    ]);
    expect(remoteState(remote).unexplained).toBe(2);
  });

  it("records zero-job startup failures as no_agent without charging unexplained, while ignoring pre-opt-in runs", async () => {
    const remote = bareRemote();
    await save(remote, 50, "success");
    const run = (number, conclusion = "failure") => ({ id: 9000 + number, run_number: number, status: "completed", conclusion, head_branch: "main", event: "schedule", run_attempt: 1 });
    const agent = { name: "agent", conclusion: "failure", started_at: "2026-11-24T09:00:00Z" };
    const runs = [run(53, "startup_failure"), run(52, "failure"), run(51, "failure")];
    const jobs = { 9051: [agent], 9052: [], 9053: [] };
    const github = fakeGitHub(remote, { runs, jobs });
    await save(remote, 54, "success", { github });

    expect(remoteRuns(remote).map(({ number, attempt, outcome, note, counted }) => ({ number, attempt, outcome, note, counted }))).toEqual([
      { number: 50, attempt: 1, outcome: "success", note: null, counted: true },
      { number: 52, attempt: 1, outcome: null, note: "no_agent", counted: false },
      { number: 53, attempt: 1, outcome: null, note: "no_agent", counted: false },
      { number: 54, attempt: 1, outcome: "success", note: null, counted: true },
    ]);
    expect(remoteState(remote).unexplained).toBe(0);

    // Once reconciled at #54, subsequent saves do not re-query any of #51..#53's jobs.
    const nextGitHub = fakeGitHub(remote, { runs, jobs });
    await save(remote, 55, "success", { github: nextGitHub });
    expect(nextGitHub.calls.jobs).toEqual([]);
  });

  it("charges a failed save from a pull_request_target run, and never a run whose history job succeeded", async () => {
    const remote = bareRemote();
    await save(remote, 50, "success");
    const run = (number, extra = {}) => ({ id: 9000 + number, run_number: number, status: "completed", conclusion: "failure", head_branch: "main", event: "schedule", run_attempt: 1, ...extra });
    const agent = { name: "agent", conclusion: "failure", started_at: "2026-11-24T09:00:00Z" };
    const writer = conclusion => ({ name: "push_routing_history", conclusion, started_at: "2026-11-24T09:10:00Z" });
    const github = fakeGitHub(remote, {
      runs: [
        // Listed under the pull request's branch, but run from the default branch's definition.
        run(53, { event: "pull_request_target", head_branch: "fix-typo" }),
        // A pull request into another branch, whose history job declined to record it.
        run(52, { event: "pull_request_target", head_branch: "release-notes" }),
        // Saved long ago, its line since trimmed from runs.jsonl.
        run(51),
      ],
      jobs: { 9053: [agent, writer("failure")], 9052: [agent, writer("success")], 9051: [agent, writer("success")] },
    });
    await save(remote, 54, "success", { github });
    expect(remoteRuns(remote).map(({ number, note }) => ({ number, note }))).toEqual([
      { number: 50, note: null },
      { number: 53, note: "unsaved" },
      { number: 54, note: null },
    ]);
    expect(remoteState(remote).unexplained).toBe(1);
    expect(remoteState(remote).reconciled_through).toBe(54);
    expect(remoteState(remote).unresolved_runs).toEqual([]);
  });

  it("neither re-charges nor re-queries an evicted pull_request_target run past the 500-line retention window", async () => {
    const remote = bareRemote();
    const state = {
      ...emptyState("triage"),
      generation: { hash: GENERATION, since_run: 1 },
      last_run: 555,
      reconciled_through: 555,
      unresolved_runs: [],
      unexplained: 1,
    };
    // Simulate runs.jsonl after #50 aged out of the 500-line window (retaining #56..#555).
    const runs = Array.from({ length: 500 }, (_, i) => ({
      run: String(9056 + i),
      number: 56 + i,
      attempt: 1,
      at: "2026-11-24T09:00:00Z",
      generation: GENERATION,
      scope: "copilot/cost/shared",
      mode: "observe",
      role: "observe",
      pair: PAIR,
      outcome: "success",
      credits: 2,
      counted: true,
      note: null,
    }));
    seedHistory(remote, state, runs);
    const agent = { name: "agent", conclusion: "failure", started_at: "2026-11-24T08:00:00Z" };
    const writer = conclusion => ({ name: "push_routing_history", conclusion, started_at: "2026-11-24T08:10:00Z" });
    const github = fakeGitHub(remote, {
      runs: [
        // Both older pull_request_target runs remain on the 100-run pull_request_target page.
        { id: 9050, run_number: 50, status: "completed", conclusion: "failure", head_branch: "pr-50", event: "pull_request_target", run_attempt: 1 },
        { id: 9049, run_number: 49, status: "completed", conclusion: "failure", head_branch: "pr-49", event: "pull_request_target", run_attempt: 1 },
        { id: 9555, run_number: 555, status: "completed", conclusion: "success", head_branch: "main", event: "schedule", run_attempt: 1 },
      ],
      jobs: { 9050: [agent, writer("failure")], 9049: [agent, writer("success")] },
    });

    await save(remote, 556, "success", { github });
    expect(github.calls.jobs).toEqual([]);
    expect(remoteState(remote).unexplained).toBe(1);
    expect(remoteState(remote).reconciled_through).toBe(556);
    expect(remoteRuns(remote).filter(line => line.number === 50)).toEqual([]);
    expect(remoteRuns(remote).at(-1)).toMatchObject({ number: 556, counted: true, note: null });
  });

  it("refreshes listRuns on a same-generation reapply conflict and charges a run that failed in between", async () => {
    const remote = bareRemote();
    await save(remote, 100, "success");
    const agent = { name: "agent", conclusion: "failure", started_at: "2026-11-24T09:00:00Z" };
    const writer = conclusion => ({ name: "push_routing_history", conclusion, started_at: "2026-11-24T09:10:00Z" });
    const run101 = { id: 9101, run_number: 101, status: "in_progress", conclusion: null, head_branch: "main", event: "schedule", run_attempt: 1 };
    const apiRuns = [run101, { id: 9100, run_number: 100, status: "completed", conclusion: "success", head_branch: "main", event: "schedule", run_attempt: 1 }];
    const github = fakeGitHub(remote, { runs: apiRuns, jobs: { 9101: [agent, writer("failure")] } });

    const basePush = signedPush(remote);
    let intercepted = false;
    const conflictingPush = async args => {
      if (!intercepted) {
        intercepted = true;
        // Another run (#103) saves first while #101 is still active, then #101 finishes and fails its save.
        await save(remote, 103, "success", {
          github: fakeGitHub(remote, {
            runs: [{ id: 9102, run_number: 102, status: "in_progress", conclusion: null, head_branch: "main", event: "schedule", run_attempt: 1 }, { ...run101 }],
          }),
        });
        run101.status = "completed";
        run101.conclusion = "failure";
      }
      return basePush(args);
    };

    await save(remote, 102, "success", { github, push: conflictingPush });
    expect(github.calls.lists.length).toBe(4);
    expect(remoteRuns(remote).map(({ number, note }) => ({ number, note }))).toEqual([
      { number: 100, note: null },
      { number: 103, note: null },
      { number: 101, note: "unsaved" },
      { number: 102, note: null },
    ]);
    expect(remoteState(remote)).toMatchObject({ unexplained: 1, last_run: 103, reconciled_through: 103, unresolved_runs: [] });
  });

  it("refreshes listRuns on a generation-change conflict and charges a failed save on the new generation", async () => {
    const remote = bareRemote();
    const NEW_GENERATION = `${"c".repeat(64)}:${"d".repeat(64)}`;
    await save(remote, 99, "success", { generation: GENERATION });
    const agent = { name: "agent", conclusion: "failure", started_at: "2026-11-24T09:00:00Z" };
    const writer = conclusion => ({ name: "push_routing_history", conclusion, started_at: "2026-11-24T09:10:00Z" });
    const apiRuns = [{ id: 9099, run_number: 99, status: "completed", conclusion: "success", head_branch: "main", event: "schedule", run_attempt: 1 }];
    const github = fakeGitHub(remote, { runs: apiRuns, jobs: { 9101: [agent, writer("failure")] } });

    const basePush = signedPush(remote);
    let intercepted = false;
    const conflictingPush = async args => {
      if (!intercepted) {
        intercepted = true;
        // Run #100 starts NEW_GENERATION first, and #101 fails its save before #102 retries.
        await save(remote, 100, "success", { generation: NEW_GENERATION });
        apiRuns.unshift({ id: 9101, run_number: 101, status: "completed", conclusion: "failure", head_branch: "main", event: "schedule", run_attempt: 1 });
        apiRuns.unshift({ id: 9100, run_number: 100, status: "completed", conclusion: "success", head_branch: "main", event: "schedule", run_attempt: 1 });
      }
      return basePush(args);
    };

    await save(remote, 102, "success", { generation: NEW_GENERATION, github, push: conflictingPush });
    expect(github.calls.lists.length).toBeGreaterThanOrEqual(4);
    expect(remoteRuns(remote).map(({ number, note }) => ({ number, note }))).toEqual([
      { number: 99, note: null },
      { number: 100, note: null },
      { number: 101, note: "unsaved" },
      { number: 102, note: null },
    ]);
    expect(remoteState(remote)).toMatchObject({
      generation: { hash: NEW_GENERATION, since_run: 100 },
      unexplained: 1,
      reconciled_through: 102,
      unresolved_runs: [],
    });
  });

  it("charges a failed save on the first run of a new generation rather than discarding it at since_run", async () => {
    const remote = bareRemote();
    const NEW_GENERATION = `${"c".repeat(64)}:${"d".repeat(64)}`;
    seedHistory(
      remote,
      {
        ...emptyState("triage"),
        generation: { hash: GENERATION, since_run: 1 },
        last_run: 99,
        reconciled_through: 99,
        unresolved_runs: [],
        unexplained: 2,
      },
      []
    );
    const agent = { name: "agent", conclusion: "failure", started_at: "2026-11-24T09:00:00Z" };
    const writer = conclusion => ({ name: "push_routing_history", conclusion, started_at: "2026-11-24T09:10:00Z" });
    const github = fakeGitHub(remote, {
      runs: [
        { id: 9100, run_number: 100, status: "completed", conclusion: "failure", head_branch: "main", event: "schedule", run_attempt: 1 },
        { id: 9099, run_number: 99, status: "completed", conclusion: "success", head_branch: "main", event: "schedule", run_attempt: 1 },
      ],
      jobs: { 9100: [agent, writer("failure")] },
    });

    await save(remote, 101, "success", { generation: NEW_GENERATION, github });
    expect(remoteRuns(remote).map(({ number, note }) => ({ number, note }))).toEqual([
      { number: 100, note: "unsaved" },
      { number: 101, note: null },
    ]);
    expect(remoteState(remote)).toMatchObject({
      generation: { hash: NEW_GENERATION, since_run: 101 },
      unexplained: 1,
      reconciled_through: 101,
      unresolved_runs: [],
    });
  });

  it("saves this run without advancing reconciled_through on a transient listRuns error or incomplete first page", async () => {
    const remote = bareRemote();
    await save(remote, 50, "success");

    // 1. Transient API failure: save #52 without advancing reconciled_through past 50.
    const failingGitHub = fakeGitHub(remote, {
      listWorkflowRuns: async () => {
        throw new Error("503 service unavailable");
      },
    });
    await save(remote, 52, "success", { github: failingGitHub });
    expect(global.core.warning).toHaveBeenCalledWith(expect.stringMatching(/503 service unavailable/));
    expect(remoteState(remote)).toMatchObject({ last_run: 52, reconciled_through: 50, unresolved_runs: [] });

    // 2. Truncated listing (total_count exceeds returned pages, leaving a gap #51..#60): save #160 without advancing reconciled_through.
    const fullPageRuns = Array.from({ length: 100 }, (_, i) => ({
      id: 9061 + i,
      run_number: 61 + i,
      status: "completed",
      conclusion: "success",
      head_branch: "main",
      event: "schedule",
      run_attempt: 1,
    }));
    const incompleteGitHub = fakeGitHub(remote, {
      listWorkflowRuns: async ({ branch, event, page = 1 }) => {
        if (event === "pull_request_target") return { data: { total_count: 0, workflow_runs: [] } };
        if (branch === "main" && page === 1) return { data: { total_count: 160, workflow_runs: fullPageRuns } };
        return { data: { total_count: 160, workflow_runs: [] } };
      },
    });
    await save(remote, 160, "success", { github: incompleteGitHub });
    expect(remoteState(remote)).toMatchObject({ last_run: 160, reconciled_through: 50, unresolved_runs: [] });
  });

  it("paginates across a multi-page burst to recover reconciliation coverage instead of stalling", async () => {
    const remote = bareRemote();
    await save(remote, 100, "success");
    const agent = { name: "agent", conclusion: "failure", started_at: "2026-11-24T09:00:00Z" };
    const writer = conclusion => ({ name: "push_routing_history", conclusion, started_at: "2026-11-24T09:10:00Z" });
    // Complete newest-first history across 2 pages (#201 down to #100), with #200 failed.
    const runs = Array.from({ length: 102 }, (_, i) => {
      const number = 201 - i;
      return {
        id: 9000 + number,
        run_number: number,
        status: "completed",
        conclusion: number === 200 ? "failure" : "success",
        head_branch: "main",
        event: "schedule",
        run_attempt: 1,
      };
    });
    const github = fakeGitHub(remote, { runs, jobs: { 9200: [agent, writer("failure")] } });

    await save(remote, 201, "success", { github });
    expect(remoteState(remote)).toMatchObject({ last_run: 201, reconciled_through: 201, unresolved_runs: [], unexplained: 1 });
    expect(remoteRuns(remote).map(({ number, note }) => ({ number, note }))).toEqual([
      { number: 100, note: null },
      { number: 200, note: "unsaved" },
      { number: 201, note: null },
    ]);
  });

  it("tracks in-progress runs in unresolved_runs until they complete and reconciles them once finished", async () => {
    const remote = bareRemote();
    await save(remote, 50, "success");
    const agent = { name: "agent", conclusion: "failure", started_at: "2026-11-24T09:00:00Z" };
    const writer = conclusion => ({ name: "push_routing_history", conclusion, started_at: "2026-11-24T09:10:00Z" });
    const run51 = { id: 9051, run_number: 51, status: "in_progress", conclusion: null, head_branch: "main", event: "schedule", run_attempt: 1 };

    await save(remote, 52, "success", { github: fakeGitHub(remote, { runs: [run51] }) });
    expect(remoteState(remote)).toMatchObject({ last_run: 52, reconciled_through: 52, unresolved_runs: [51], unexplained: 0 });

    // Once #51 completes with a failed save, #53 charges #51 and clears unresolved_runs.
    run51.status = "completed";
    run51.conclusion = "failure";
    await save(remote, 53, "success", { github: fakeGitHub(remote, { runs: [run51], jobs: { 9051: [agent, writer("failure")] } }) });
    expect(remoteRuns(remote).map(({ number, note }) => ({ number, note }))).toEqual([
      { number: 50, note: null },
      { number: 52, note: null },
      { number: 51, note: "unsaved" },
      { number: 53, note: null },
    ]);
    expect(remoteState(remote)).toMatchObject({ last_run: 53, reconciled_through: 53, unresolved_runs: [], unexplained: 1 });
  });

  it("defers runs above reconciled_through on an incomplete scan so they cannot be double-charged after eviction", async () => {
    const remote = bareRemote();
    await save(remote, 50, "success");
    const agent = { name: "agent", conclusion: "failure", started_at: "2026-11-24T09:00:00Z" };
    const writer = conclusion => ({ name: "push_routing_history", conclusion, started_at: "2026-11-24T09:10:00Z" });
    // Truncated default-branch listing (reports total_count 660, but page 2+ returns empty) leaves #51..#560 unverified.
    const page1 = Array.from({ length: 100 }, (_, i) => ({
      id: 9660 - i,
      run_number: 660 - i,
      status: "completed",
      conclusion: "success",
      head_branch: "main",
      event: "schedule",
      run_attempt: 1,
    }));
    const prtFailure = { id: 9051, run_number: 51, status: "completed", conclusion: "failure", head_branch: "pr-51", event: "pull_request_target", run_attempt: 1 };
    const github = fakeGitHub(remote, {
      jobs: { 9051: [agent, writer("failure")] },
      listWorkflowRuns: async ({ branch, event, page = 1 }) => {
        if (event === "pull_request_target") return { data: { total_count: 1, workflow_runs: page === 1 ? [prtFailure] : [] } };
        if (branch === "main" && page === 1) return { data: { total_count: 660, workflow_runs: page1 } };
        return { data: { total_count: 660, workflow_runs: [] } };
      },
    });

    await save(remote, 661, "success", { github });
    expect(github.calls.jobs).toEqual([]);
    expect(remoteState(remote)).toMatchObject({ last_run: 661, reconciled_through: 50, unexplained: 0 });
    expect(remoteRuns(remote).filter(line => line.number === 51)).toEqual([]);
  });

  it("catches up a backlog past GitHub's 1,000-run listing cap in one save and charges a late failed save", async () => {
    const remote = bareRemote();
    await save(remote, 1, "success");
    const agent = { name: "agent", conclusion: "failure", started_at: "2026-11-24T09:00:00Z" };
    const writer = conclusion => ({ name: "push_routing_history", conclusion, started_at: "2026-11-24T09:10:00Z" });
    // 1,005 runs (#1005 down to #1) with #1004 failed: #2 lies past page 10, so the save reads a created window for the oldest runs.
    const runs = Array.from({ length: 1005 }, (_, i) => {
      const number = 1005 - i;
      return {
        id: 19000 + number,
        run_number: number,
        created_at: runCreatedAt(number),
        status: "completed",
        conclusion: number === 1004 ? "failure" : "success",
        head_branch: "main",
        event: "schedule",
        run_attempt: 1,
      };
    });
    const github = fakeGitHub(remote, { runs, jobs: { 20004: [agent, writer("failure")] } });

    await save(remote, 1005, "success", { github });
    expect(github.calls.lists.every(call => (call.page || 1) <= 10)).toBe(true);
    expect(github.calls.lists.some(call => call.created === `<=${runCreatedAt(6)}`)).toBe(true);
    expect(remoteState(remote)).toMatchObject({ last_run: 1005, reconciled_through: 1005, unresolved_runs: [], unexplained: 1 });
    expect(remoteRuns(remote).filter(line => line.number === 1004)).toEqual([expect.objectContaining({ number: 1004, note: "unsaved" })]);
  });

  it("does not trust pull_request_target head_sha to suppress an unsaved failure straddling since_run", async () => {
    const remote = bareRemote();
    const NEW_GENERATION = `${"c".repeat(64)}:${"d".repeat(64)}`;
    seedHistory(
      remote,
      {
        ...emptyState("triage"),
        generation: { hash: NEW_GENERATION, since_run: 101 },
        last_run: 101,
        reconciled_through: 101,
        unresolved_runs: [100],
        unresolved_run_ids: { "100": "9100" },
        unexplained: 0,
      },
      []
    );
    const agent = { name: "agent", conclusion: "failure", started_at: "2026-11-24T09:00:00Z" };
    const writer = conclusion => ({ name: "push_routing_history", conclusion, started_at: "2026-11-24T09:10:00Z" });
    const github = fakeGitHub(remote, {
      runs: [
        { id: 9101, run_number: 101, status: "completed", conclusion: "success", head_branch: "main", event: "schedule", run_attempt: 1 },
        // PR head still has the old generation lockfile, while pull_request_target actually executed NEW_GENERATION from main.
        { id: 9100, run_number: 100, status: "completed", conclusion: "failure", head_branch: "fork-pr", event: "pull_request_target", run_attempt: 1, head_sha: "sha-pr-head" },
      ],
      jobs: { 9100: [agent, writer("failure")] },
      contents: { "sha-pr-head:.github/workflows/triage.lock.yml": `env:\n  GH_AW_ROUTING_GENERATION: "${GENERATION}"\n` },
    });

    await save(remote, 102, "success", { generation: NEW_GENERATION, github });
    expect(github.calls.contents).toEqual([]);
    expect(remoteRuns(remote).map(({ number, note }) => ({ number, note }))).toEqual([
      { number: 100, note: "unsaved" },
      { number: 102, note: null },
    ]);
    expect(remoteState(remote)).toMatchObject({ unexplained: 1, reconciled_through: 102, unresolved_runs: [] });
  });

  it("does not re-charge an evicted legacy failure when upgrading a full 500-line runs.jsonl", async () => {
    const remote = bareRemote();
    const writtenRuns = Array.from({ length: 500 }, (_, i) => ({
      run: String(9003 + i),
      number: 3 + i,
      attempt: 1,
      at: "2026-11-24T09:00:00Z",
      generation: GENERATION,
      scope: "copilot/cost/shared",
      mode: "observe",
      role: "observe",
      pair: PAIR,
      outcome: "success",
      credits: 2,
      counted: true,
      note: null,
    }));
    seedHistory(
      remote,
      {
        ...emptyState("triage"),
        generation: { hash: GENERATION, since_run: 1 },
        last_run: 502,
        unexplained: 1,
      },
      writtenRuns
    );
    const agent = { name: "agent", conclusion: "failure", started_at: "2026-11-24T09:00:00Z" };
    const writer = conclusion => ({ name: "push_routing_history", conclusion, started_at: "2026-11-24T09:10:00Z" });
    const apiRuns = Array.from({ length: 502 }, (_, i) => {
      const number = 502 - i;
      return {
        id: 9000 + number,
        run_number: number,
        status: "completed",
        conclusion: number === 2 ? "failure" : "success",
        head_branch: number % 2 === 0 ? "pr-branch" : "main",
        event: number % 2 === 0 ? "pull_request_target" : "schedule",
        run_attempt: 1,
      };
    });
    const github = fakeGitHub(remote, { runs: apiRuns, jobs: { 9002: [agent, writer("failure")] } });

    await save(remote, 503, "success", { github });
    expect(github.calls.jobs).toEqual([]);
    expect(remoteState(remote)).toMatchObject({ last_run: 503, reconciled_through: 503, unexplained: 1, unresolved_runs: [] });
    expect(remoteRuns(remote).filter(line => line.number === 2)).toEqual([]);
  });

  it("resolves a tracked unresolved run via direct run-ID lookup after it falls outside the listing window", async () => {
    const remote = bareRemote();
    seedHistory(
      remote,
      {
        ...emptyState("triage"),
        generation: { hash: GENERATION, since_run: 1 },
        last_run: 900,
        reconciled_through: 900,
        unresolved_runs: [2],
        unresolved_run_ids: { "2": "9002" },
        unexplained: 0,
      },
      []
    );
    const agent = { name: "agent", conclusion: "failure", started_at: "2026-11-24T09:00:00Z" };
    const writer = conclusion => ({ name: "push_routing_history", conclusion, started_at: "2026-11-24T09:10:00Z" });
    // Page 1 only has #900..#801 (no #2); direct getWorkflowRun resolves #2 using stored run ID 9002.
    const recentRuns = Array.from({ length: 100 }, (_, i) => ({
      id: 9900 - i,
      run_number: 900 - i,
      status: "completed",
      conclusion: "success",
      head_branch: "main",
      event: "schedule",
      run_attempt: 1,
    }));
    const run2 = { id: 9002, run_number: 2, status: "completed", conclusion: "failure", head_branch: "main", event: "schedule", run_attempt: 1 };
    const github = fakeGitHub(remote, { runs: recentRuns, runsById: { 9002: run2 }, jobs: { 9002: [agent, writer("failure")] } });

    await save(remote, 901, "success", { github });
    expect(github.calls.runs).toEqual([9002]);
    expect(github.calls.jobs).toEqual([9002]);
    expect(remoteRuns(remote).map(({ number, note }) => ({ number, note }))).toEqual([
      { number: 2, note: "unsaved" },
      { number: 901, note: null },
    ]);
    const finalState = remoteState(remote);
    expect(finalState).toMatchObject({ reconciled_through: 901, unresolved_runs: [], unexplained: 1 });
    expect(finalState.unresolved_run_ids).toBeUndefined();
  });

  it("tracks an older missing run below the unfiltered first page as unresolved so later runs keep reconciling", async () => {
    const remote = bareRemote();
    await save(remote, 100, "success");
    // #100 and #103..#205 exist (#101 is PR, #102 is omitted from all pages): #205 tracks #102 in unresolved_runs instead of skipping it.
    const runs = [
      { id: 9205, run_number: 205, status: "completed", conclusion: "success", head_branch: "main", event: "schedule", run_attempt: 1 },
      ...Array.from({ length: 102 }, (_, i) => {
        const number = 204 - i;
        return {
          id: 9000 + number,
          run_number: number,
          status: "completed",
          conclusion: "success",
          head_branch: number % 2 === 0 ? "main" : "feature",
          event: number % 2 === 0 ? "schedule" : "pull_request",
          run_attempt: 1,
        };
      }),
      { id: 9101, run_number: 101, status: "completed", conclusion: "success", head_branch: "feature", event: "pull_request", run_attempt: 1 },
      { id: 9100, run_number: 100, status: "completed", conclusion: "success", head_branch: "main", event: "schedule", run_attempt: 1 },
    ];
    await save(remote, 205, "success", { github: fakeGitHub(remote, { runs }) });
    expect(remoteState(remote)).toMatchObject({ last_run: 205, reconciled_through: 205, unresolved_runs: [102] });
  });

  it("rotates direct unresolved run-ID lookups across saves when more than ten tracked runs are missing", async () => {
    const remote = bareRemote();
    const unresolvedRuns = Array.from({ length: 11 }, (_, i) => i + 1);
    const unresolvedRunIds = Object.fromEntries(unresolvedRuns.map(n => [String(n), String(9000 + n)]));
    seedHistory(
      remote,
      {
        ...emptyState("triage"),
        generation: { hash: GENERATION, since_run: 1 },
        last_run: 900,
        reconciled_through: 900,
        unresolved_runs: unresolvedRuns,
        unresolved_run_ids: unresolvedRunIds,
        unexplained: 0,
      },
      []
    );
    const agent = { name: "agent", conclusion: "failure", started_at: "2026-11-24T09:00:00Z" };
    const writer = conclusion => ({ name: "push_routing_history", conclusion, started_at: "2026-11-24T09:10:00Z" });
    const runsById = Object.fromEntries(
      unresolvedRuns.map(n => [
        9000 + n,
        {
          id: 9000 + n,
          run_number: n,
          status: n === 11 ? "completed" : "in_progress",
          conclusion: n === 11 ? "failure" : null,
          head_branch: "main",
          event: "schedule",
          run_attempt: 1,
        },
      ])
    );
    const recentRuns = Array.from({ length: 100 }, (_, i) => ({
      id: 9900 - i,
      run_number: 900 - i,
      status: "completed",
      conclusion: "success",
      head_branch: "main",
      event: "schedule",
      run_attempt: 1,
    }));
    const github = fakeGitHub(remote, { runs: recentRuns, runsById, jobs: { 9011: [agent, writer("failure")] } });

    // Across two consecutive saves, rotation ensures the 11th run (#11) receives a direct lookup and is charged.
    await save(remote, 901, "success", { github });
    recentRuns.unshift({ id: 9901, run_number: 901, status: "completed", conclusion: "success", head_branch: "main", event: "schedule", run_attempt: 1 });
    await save(remote, 902, "success", { github });
    expect(github.calls.runs).toContain(9011);
    expect(remoteState(remote)).toMatchObject({ reconciled_through: 902, unexplained: 1 });
    expect(remoteState(remote).unresolved_runs).not.toContain(11);
  });

  it("resolves a retained older-generation failed save without charging it to the new generation", async () => {
    const remote = bareRemote();
    const NEW_GENERATION = `${"c".repeat(64)}:${"d".repeat(64)}`;
    seedHistory(
      remote,
      {
        ...emptyState("triage"),
        generation: { hash: NEW_GENERATION, since_run: 101 },
        last_run: 101,
        reconciled_through: 101,
        unresolved_runs: [99],
        unexplained: 0,
      },
      []
    );
    const agent = { name: "agent", conclusion: "failure", started_at: "2026-11-24T09:00:00Z" };
    const writer = conclusion => ({ name: "push_routing_history", conclusion, started_at: "2026-11-24T09:10:00Z" });
    const github = fakeGitHub(remote, {
      runs: [
        { id: 9101, run_number: 101, status: "completed", conclusion: "success", head_branch: "main", event: "schedule", run_attempt: 1, head_sha: "sha-gen2" },
        { id: 9099, run_number: 99, status: "completed", conclusion: "failure", head_branch: "main", event: "schedule", run_attempt: 1, head_sha: "sha-gen1" },
      ],
      jobs: { 9099: [agent, writer("failure")] },
      contents: {
        "sha-gen1:.github/workflows/triage.lock.yml": `env:\n  GH_AW_ROUTING_GENERATION: "${GENERATION}"\n`,
      },
    });

    await save(remote, 102, "success", { generation: NEW_GENERATION, github });
    expect(remoteRuns(remote).map(({ number, note, generation }) => ({ number, note, generation }))).toEqual([
      { number: 99, note: "older_definition", generation: GENERATION },
      { number: 102, note: null, generation: NEW_GENERATION },
    ]);
    expect(remoteState(remote)).toMatchObject({ unexplained: 0, reconciled_through: 102, unresolved_runs: [] });
  });

  it("bootstraps legacy state without reconciled_through from its fence so unverified overlapping failures are still reconciled", async () => {
    const remote = bareRemote();
    const writtenRuns = Array.from({ length: 98 }, (_, i) => ({
      run: String(9001 + i),
      number: 1 + i,
      attempt: 1,
      at: "2026-11-24T09:00:00Z",
      generation: GENERATION,
      scope: "copilot/cost/shared",
      mode: "observe",
      role: "observe",
      pair: PAIR,
      outcome: "success",
      credits: 2,
      counted: true,
      note: null,
    }));
    writtenRuns.push({
      run: "9100",
      number: 100,
      attempt: 1,
      at: "2026-11-24T09:05:00Z",
      generation: GENERATION,
      scope: "copilot/cost/shared",
      mode: "observe",
      role: "observe",
      pair: PAIR,
      outcome: "success",
      credits: 2,
      counted: true,
      note: null,
    });
    seedHistory(
      remote,
      {
        ...emptyState("triage"),
        generation: { hash: GENERATION, since_run: 1 },
        last_run: 100,
      },
      writtenRuns
    );
    const agent = { name: "agent", conclusion: "failure", started_at: "2026-11-24T09:00:00Z" };
    const writer = conclusion => ({ name: "push_routing_history", conclusion, started_at: "2026-11-24T09:10:00Z" });
    const apiRuns = Array.from({ length: 100 }, (_, i) => {
      const number = 100 - i;
      return {
        id: 9000 + number,
        run_number: number,
        status: "completed",
        conclusion: number === 99 ? "failure" : "success",
        head_branch: "main",
        event: "schedule",
        run_attempt: 1,
      };
    });
    const github = fakeGitHub(remote, { runs: apiRuns, jobs: { 9099: [agent, writer("failure")] } });

    await save(remote, 101, "success", { github });
    expect(github.calls.jobs).toEqual([9099]);
    expect(remoteState(remote)).toMatchObject({ last_run: 101, reconciled_through: 101, unresolved_runs: [], unexplained: 1 });
    expect(
      remoteRuns(remote)
        .slice(-2)
        .map(({ number, note }) => ({ number, note }))
    ).toEqual([
      { number: 99, note: "unsaved" },
      { number: 101, note: null },
    ]);
  });

  it("does not let a delayed older-generation writer consume a new generation's first failed save", async () => {
    const remote = bareRemote();
    const NEW_GENERATION = `${"c".repeat(64)}:${"d".repeat(64)}`;
    await save(remote, 98, "success", { generation: GENERATION });
    const agent = { name: "agent", conclusion: "failure", started_at: "2026-11-24T09:00:00Z" };
    const writer = conclusion => ({ name: "push_routing_history", conclusion, started_at: "2026-11-24T09:10:00Z" });
    const run100 = { id: 9100, run_number: 100, status: "completed", conclusion: "failure", head_branch: "main", event: "schedule", run_attempt: 1 };

    // #99 (old generation) finishes after #100 (new generation) failed: must ignore #100.
    const gh99 = fakeGitHub(remote, {
      runs: [run100, { id: 9098, run_number: 98, status: "completed", conclusion: "success", head_branch: "main", event: "schedule", run_attempt: 1 }],
      jobs: { 9100: [agent, writer("failure")] },
    });
    await save(remote, 99, "success", { generation: GENERATION, github: gh99 });
    expect(gh99.calls.jobs).toEqual([]);
    expect(remoteState(remote)).toMatchObject({ generation: { hash: GENERATION, since_run: 98 }, reconciled_through: 99, unexplained: 0 });

    // #101 (new generation) then transitions generation and charges #100 to NEW_GENERATION.
    const gh101 = fakeGitHub(remote, {
      runs: [run100, { id: 9099, run_number: 99, status: "completed", conclusion: "success", head_branch: "main", event: "schedule", run_attempt: 1 }],
      jobs: { 9100: [agent, writer("failure")] },
    });
    await save(remote, 101, "success", { generation: NEW_GENERATION, github: gh101 });
    expect(remoteRuns(remote).map(({ number, note }) => ({ number, note }))).toEqual([
      { number: 98, note: null },
      { number: 99, note: null },
      { number: 100, note: "unsaved" },
      { number: 101, note: null },
    ]);
    expect(remoteState(remote)).toMatchObject({ generation: { hash: NEW_GENERATION, since_run: 101 }, reconciled_through: 101, unexplained: 1 });
  });

  it("tracks uncorroborated gaps in unresolved_runs without stalling later failure accounting, and retains transiently absent unresolved runs", async () => {
    const remote = bareRemote();
    await save(remote, 100, "success");
    const agent = { name: "agent", conclusion: "failure", started_at: "2026-11-24T09:00:00Z" };
    const writer = conclusion => ({ name: "push_routing_history", conclusion, started_at: "2026-11-24T09:10:00Z" });
    const run100 = { id: 9100, run_number: 100, status: "completed", conclusion: "success", head_branch: "main", event: "schedule", run_attempt: 1 };
    const run102 = { id: 9102, run_number: 102, status: "completed", conclusion: "success", head_branch: "main", event: "schedule", run_attempt: 1 };

    // 1. Listing omits #101 between #100 and #102: record #101 as unresolved rather than marking it reconciled or stalling.
    await save(remote, 102, "success", { github: fakeGitHub(remote, { runs: [run102, run100] }) });
    expect(remoteState(remote)).toMatchObject({ last_run: 102, reconciled_through: 102, unresolved_runs: [101], unexplained: 0 });

    // 2. When #101 reappears as a failed save on #103, #101 is charged and cleared from unresolved_runs.
    const run101 = { id: 9101, run_number: 101, status: "completed", conclusion: "failure", head_branch: "main", event: "schedule", run_attempt: 1 };
    await save(remote, 103, "success", { github: fakeGitHub(remote, { runs: [run102, run101, run100], jobs: { 9101: [agent, writer("failure")] } }) });
    expect(remoteState(remote)).toMatchObject({ last_run: 103, reconciled_through: 103, unresolved_runs: [], unexplained: 1 });

    // 3. Track #104 as unresolved while #105 saves, then transiently omit #104 on #106: #104 must stay unresolved.
    const run104 = { id: 9104, run_number: 104, status: "in_progress", conclusion: null, head_branch: "main", event: "schedule", run_attempt: 1 };
    await save(remote, 105, "success", { github: fakeGitHub(remote, { runs: [run104] }) });
    expect(remoteState(remote)).toMatchObject({ reconciled_through: 105, unresolved_runs: [104] });

    const run105 = { id: 9105, run_number: 105, status: "completed", conclusion: "success", head_branch: "main", event: "schedule", run_attempt: 1 };
    const gh106 = fakeGitHub(remote, { runs: [run105] });
    // Simulate a transient API error on direct lookup while #104 is omitted from the listing: #104 must stay unresolved.
    gh106.rest.actions.getWorkflowRun = async () => {
      throw Object.assign(new Error("503 service unavailable"), { status: 503 });
    };
    await save(remote, 106, "success", { github: gh106 });
    expect(remoteState(remote)).toMatchObject({ reconciled_through: 106, unresolved_runs: [104], unexplained: 1 });

    // 4. Once #104 reappears completed as a failed save on #107, it is charged and removed from unresolved_runs.
    run104.status = "completed";
    run104.conclusion = "failure";
    const run106 = { id: 9106, run_number: 106, status: "completed", conclusion: "success", head_branch: "main", event: "schedule", run_attempt: 1 };
    await save(remote, 107, "success", { github: fakeGitHub(remote, { runs: [run106, run105, run104], jobs: { 9104: [agent, writer("failure")] } }) });
    expect(remoteState(remote)).toMatchObject({ reconciled_through: 107, unresolved_runs: [], unexplained: 2 });
  });

  it("reaches a floor on page 2 (and page 9) of a 5,000-run workflow without probing past GitHub's 1,000-result cap", async () => {
    const remote = bareRemote();
    const agent = { name: "agent", conclusion: "failure", started_at: "2026-11-24T09:00:00Z" };
    const writer = conclusion => ({ name: "push_routing_history", conclusion, started_at: "2026-11-24T09:10:00Z" });
    const runs = Array.from({ length: 5000 }, (_, i) => {
      const number = 5000 - i;
      return paginationRun(number, { conclusion: number === 4880 ? "failure" : "success" });
    });

    // 1. Floor is 150 runs behind (#4850, on page 2) with total_count = 5000 and pages > 10 returning [].
    seedHistory(
      remote,
      {
        ...emptyState("triage"),
        generation: { hash: GENERATION, since_run: 1 },
        last_run: 4850,
        reconciled_through: 4850,
        unresolved_runs: [],
        unexplained: 0,
      },
      []
    );
    const githubPage2 = fakeGitHub(remote, { runs, jobs: { 13880: [agent, writer("failure")] } });
    await save(remote, 5000, "success", { github: githubPage2 });

    expect(githubPage2.calls.lists.every(call => (call.page || 1) <= 10)).toBe(true);
    expect(remoteState(remote)).toMatchObject({ last_run: 5000, reconciled_through: 5000, unresolved_runs: [], unexplained: 1 });
    expect(remoteRuns(remote).filter(line => line.number === 4880)).toEqual([expect.objectContaining({ number: 4880, note: "unsaved" })]);

    // 2. Floor is 850 runs behind (#4150, on page 9) on the same 5,000-run workflow: pages 1 to 9 are read in order and the save catches up fully.
    const githubPage9 = fakeGitHub(remote, { runs });
    const res = await reconcilePagination(githubPage9, 4150, 5000);
    expect(githubPage9.calls.lists.every(call => (call.page || 1) <= 10)).toBe(true);
    expect(res.reconciliation.verified).toBe(true);
    expect(res.reconciliation).not.toHaveProperty("ceiling");
  });

  it("uses bounded created windows to reconcile a 2,002-run backlog across GitHub's 1,000-result cap, including same-second boundary ties", async () => {
    const remote = bareRemote();
    seedHistory(
      remote,
      {
        ...emptyState("triage"),
        generation: { hash: GENERATION, since_run: 1 },
        last_run: 1,
        reconciled_through: 1,
        unresolved_runs: [],
        unexplained: 0,
      },
      []
    );
    const agent = { name: "agent", conclusion: "failure", started_at: "2026-11-24T09:00:00Z" };
    const writer = conclusion => ({ name: "push_routing_history", conclusion, started_at: "2026-11-24T09:10:00Z" });
    // 2,002 runs (#2002 down to #1) with a same-second timestamp tie spanning the 1,000-run boundary (#1004..#1001),
    // and unsaved failures at #2 (past two 1,000-run caps), #1002 (at the same-second tie) and #2001 (recent).
    const tieTime = runCreatedAt(1002);
    const runs = Array.from({ length: 2002 }, (_, i) => {
      const number = 2002 - i;
      const createdAt = number >= 1001 && number <= 1004 ? tieTime : runCreatedAt(number);
      return paginationRun(number, { created_at: createdAt, conclusion: [2, 1002, 2001].includes(number) ? "failure" : "success" });
    });
    const jobs = {
      9002: [agent, writer("failure")],
      10002: [agent, writer("failure")],
      11001: [agent, writer("failure")],
    };
    const github = fakeGitHub(remote, { runs, jobs });

    // One save steps through two created windows, past the tie at the first window's page 10, and catches up fully.
    await save(remote, 2002, "success", { github });
    expect(github.calls.lists.every(call => (call.page || 1) <= 10)).toBe(true);
    expect(github.calls.lists.some(call => call.created === `<=${tieTime}`)).toBe(true);
    expect(remoteState(remote)).toMatchObject({ last_run: 2002, reconciled_through: 2002, unresolved_runs: [], unexplained: 3 });
    expect(
      remoteRuns(remote)
        .filter(line => line.note === "unsaved")
        .map(line => line.number)
    ).toEqual([2, 1002, 2001]);
  });

  it("advances a backlog deeper than one save's calls in verified chunks, never verifying past an unread same-second tie", async () => {
    // 5,000 runs (#5000 down to #1) with failed saves at #3, #1520 and #4999. Runs #1501 to #1650 share one
    // creation second and are listed lowest number first, so a walk that stops inside the tie has already seen
    // #1650 but not #1520.
    const tieTime = runCreatedAt(1575);
    const runs = Array.from({ length: 5000 }, (_, i) => {
      const number = 5000 - i;
      return paginationRun(number, {
        created_at: number >= 1501 && number <= 1650 ? tieTime : runCreatedAt(number),
        conclusion: [3, 1520, 4999].includes(number) ? "failure" : "success",
      });
    });
    const tie = runs.filter(run => run.created_at === tieTime).reverse();
    runs.splice(runs.indexOf(tie[tie.length - 1]), tie.length, ...tie);
    const github = fakeGitHub(null, { runs });
    const list = github.rest.actions.listWorkflowRuns;
    // Verified coverage never leaves a gap, so the unfiltered corroboration listing is never needed.
    github.rest.actions.listWorkflowRuns = async params => {
      if (!params.branch && !params.event) throw new Error("unfiltered corroboration listing was needed");
      return list(params);
    };

    const charged = [];
    const ceilings = [];
    let reconciledThrough = 1;
    for (let step = 0; step < 10; step++) {
      const { listing, reconciliation } = await reconcilePagination(github, reconciledThrough, 5001);
      expect(reconciliation).toMatchObject({ verified: true, unresolved: [] });
      charged.push(...listing.filter(run => run.conclusion === "failure").map(run => run.number));
      if (reconciliation.ceiling === undefined) break;
      ceilings.push(reconciliation.ceiling);
      expect(reconciliation.ceiling).toBeGreaterThan(reconciledThrough);
      reconciledThrough = reconciliation.ceiling;
    }

    expect(ceilings.length).toBeGreaterThan(0);
    expect(charged).toEqual([3, 1520, 4999]);
    expect(github.calls.lists.every(call => (call.page || 1) <= 10)).toBe(true);
  });

  it.each([
    { countCap: Infinity, firstCeiling: 3989 },
    { countCap: 2500, firstCeiling: 5000 },
  ])("recovers a run shifted into a cached page during the walk back up (count capped at $countCap)", async ({ countCap, firstCeiling }) => {
    const remote = bareRemote();
    seedHistory(remote, { ...emptyState("triage"), generation: { hash: GENERATION, since_run: 1 }, last_run: 3989, reconciled_through: 3989 }, []);
    const runs = Array.from({ length: 5000 }, (_, i) => paginationRun(5000 - i, { conclusion: 5000 - i === 4101 ? "failure" : "success" }));
    const jobs = {
      13101: [
        { name: "agent", conclusion: "failure", started_at: runCreatedAt(4101) },
        { name: "push_routing_history", conclusion: "failure" },
      ],
    };
    const github = fakeGitHub(remote, { runs, jobs });
    const list = github.rest.actions.listWorkflowRuns;
    let inserted = false;
    github.rest.actions.listWorkflowRuns = async params => {
      if (!inserted && params.branch === "main" && !params.created && params.page === 9) {
        // Page 10 was cached before this insert. Run 4101 is now on that page, not on page 9.
        runs.unshift(paginationRun(5001));
        inserted = true;
      }
      const response = await list(params);
      if (params.branch === "main" && !params.created) response.data.total_count = Math.min(countCap, response.data.total_count);
      return response;
    };

    await save(remote, 5000, "success", { github });
    expect(inserted).toBe(true);
    // A changed count defers the save's coverage. With a capped count the unfiltered scan recovers the missing run.
    expect(remoteState(remote).reconciled_through).toBe(firstCeiling);
    await save(remote, 5002, "success", { github });
    expect(remoteState(remote).reconciled_through).toBe(5002);
    expect(remoteRuns(remote).filter(line => line.number === 4101)).toEqual([expect.objectContaining({ note: "unsaved" })]);
  });

  it("fails closed when a backlog exceeds the bounded call budget, stalls on identical timestamps, or lacks created_at past page 10", async () => {
    const remote = bareRemote();

    // 1. Backlog of 20,000 runs exceeds the bounded created-window search budget.
    const deepRuns = Array.from({ length: 20000 }, (_, i) => paginationRun(20000 - i));
    const deepGitHub = fakeGitHub(remote, { runs: deepRuns });
    const deepResult = await reconcilePagination(deepGitHub, 1, 20000);
    expect(deepResult.reconciliation.verified).toBe(false);
    expect(deepGitHub.calls.lists.every(call => (call.page || 1) <= 10)).toBe(true);
    expect(deepGitHub.calls.lists.length).toBeLessThanOrEqual(25);

    // 2. More than 1,000 runs sharing one created_at timestamp cannot advance past that second without risking gaps.
    const sameSecondRuns = Array.from({ length: 1500 }, (_, i) => paginationRun(1500 - i, { created_at: "2026-11-24T09:00:00Z" }));
    const sameSecondGitHub = fakeGitHub(remote, { runs: sameSecondRuns });
    const sameSecondResult = await reconcilePagination(sameSecondGitHub, 1, 1500);
    expect(sameSecondResult.reconciliation.verified).toBe(false);
    expect(sameSecondGitHub.calls.lists.every(call => (call.page || 1) <= 10)).toBe(true);

    // 3. Runs past the 1,000-result cap without created_at fail closed instead of probing page 11.
    const noTimestamps = Array.from({ length: 1200 }, (_, i) => paginationRun(1200 - i, { created_at: undefined }));
    const noTimestampGitHub = fakeGitHub(remote, { runs: noTimestamps });
    const noTimestampResult = await reconcilePagination(noTimestampGitHub, 1, 1200);
    expect(noTimestampResult.reconciliation.verified).toBe(false);
    expect(noTimestampGitHub.calls.lists.every(call => (call.page || 1) <= 10)).toBe(true);

    // 3b. Missing created_at on even a single boundary row on page 10 must fail closed before
    // crossing into a created window.
    const oneMissingBoundaryTimestamp = Array.from({ length: 1200 }, (_, i) => {
      const number = 1200 - i;
      return paginationRun(number, { created_at: number === 201 ? undefined : runCreatedAt(number) });
    });
    const oneMissingWithCount = fakeGitHub(remote, { runs: oneMissingBoundaryTimestamp });
    const oneMissingWithCountResult = await reconcilePagination(oneMissingWithCount, 1, 1200);
    expect(oneMissingWithCountResult.reconciliation.verified).toBe(false);
    expect(oneMissingWithCount.calls.lists.some(call => Boolean(call.created))).toBe(false);

    // 4. A non-empty short/truncated page when total_count indicates more rows must fail closed.
    const fullRuns = Array.from({ length: 5000 }, (_, i) => paginationRun(5000 - i));
    const truncatedPageGitHub = fakeGitHub(remote, {
      listWorkflowRuns: async ({ event, page = 1, per_page = 100 }) => {
        if (event === "pull_request_target") return { data: { total_count: 0, workflow_runs: [] } };
        const start = (page - 1) * per_page;
        const slice = fullRuns.slice(0, 1000).slice(start, start + per_page);
        // Drop one row from page 2 while still reporting total_count = 5000.
        const batch = page === 2 ? slice.filter(run => run.run_number !== 4880) : slice;
        return { data: { total_count: fullRuns.length, workflow_runs: batch } };
      },
    });
    const truncatedResult = await reconcilePagination(truncatedPageGitHub, 4850, 5000);
    expect(truncatedResult.reconciliation.verified).toBe(false);

    // 5. A short page found on the walk back up fails closed. The floor (#3990) lies past page 10, so the
    // search reads pages 1 and 10 and a created window, and the walk then meets a 99-row page 6.
    const upwardTruncatedGitHub = fakeGitHub(remote, {
      listWorkflowRuns: async ({ event, created, page = 1, per_page = 100 }) => {
        if (event === "pull_request_target") return { data: { total_count: 0, workflow_runs: [] } };
        const filtered = fullRuns.filter(run => matchesCreatedFilter(run.created_at, created));
        const start = (page - 1) * per_page;
        const slice = filtered.slice(0, 1000).slice(start, start + per_page);
        return { data: { total_count: filtered.length, workflow_runs: !created && page === 6 ? slice.slice(0, 99) : slice } };
      },
    });
    const upwardTruncatedResult = await reconcilePagination(upwardTruncatedGitHub, 3989, 5000);
    expect(upwardTruncatedResult.reconciliation.verified).toBe(false);
    expect(upwardTruncatedGitHub.calls.lists.filter(call => call.branch === "main").map(({ page, created }) => [page, created ?? null])).toEqual([
      [1, null],
      [10, null],
      [1, `<=${runCreatedAt(4001)}`],
      [9, null],
      [8, null],
      [7, null],
      [6, null],
    ]);

    // 6. Without total_count a short page cannot show that the listing ended, so a 99-row page 1 with
    // more runs behind it fails closed instead of being read as the whole listing.
    const noCountGitHub = fakeGitHub(remote, {
      listWorkflowRuns: async ({ event, page = 1 }) => {
        if (event === "pull_request_target") return { data: { total_count: 0, workflow_runs: [] } };
        return { data: { workflow_runs: page === 1 ? fullRuns.slice(0, 99) : fullRuns.slice(99, 199) } };
      },
    });
    const noCountResult = await reconcilePagination(noCountGitHub, 4850, 5000);
    expect(noCountResult.reconciliation.verified).toBe(false);
  });

  it("keeps searching for a reachable floor inside a tie straddling page 10", async () => {
    const agent = { name: "agent", conclusion: "failure", started_at: "2026-11-24T09:00:00Z" };
    const writer = conclusion => ({ name: "push_routing_history", conclusion, started_at: "2026-11-24T09:10:00Z" });
    // 300 newer runs, then 800 runs (#1200..#401) created in one second, then 400 older runs. The floor (#950)
    // is inside the tie on page 6, so the search reads all ten pages and then nine pages of a created window.
    const tieTime = runCreatedAt(800);
    const tied = [
      ...Array.from({ length: 300 }, (_, i) => paginationRun(1500 - i)),
      ...Array.from({ length: 800 }, (_, i) => paginationRun(1200 - i, { created_at: tieTime, conclusion: 1200 - i === 960 ? "failure" : "success" })),
      ...Array.from({ length: 400 }, (_, i) => paginationRun(400 - i)),
    ];
    const tiedGitHub = fakeGitHub(null, { runs: tied, jobs: { 9960: [agent, writer("failure")] } });
    const tiedResult = await reconcilePagination(tiedGitHub, 950, 1501);
    expect(tiedResult.reconciliation).toMatchObject({ verified: true, unresolved: [] });
    expect(tiedResult.reconciliation).not.toHaveProperty("ceiling");
    expect(tiedResult.listing.filter(item => item.conclusion === "failure").map(item => item.number)).toEqual([960]);
    expect(tiedGitHub.calls.lists.filter(call => call.branch === "main")).toHaveLength(19);
  });

  it.each(["tied", "distinct"])("reads the page-2 target after overshooting it on page 10 with %s timestamps", async timestamps => {
    // Run numbers are 20 apart, so page 1 looks far from the target. Page 10 proves we overshot.
    const sparse = [...Array.from({ length: 900 }, (_, i) => paginationRun(40000 - 20 * i)), ...Array.from({ length: 1200 }, (_, i) => paginationRun(20000 - i, { created_at: runCreatedAt(timestamps === "tied" ? 19000 : 20000 - i) }))];
    const sparseGitHub = fakeGitHub(null, { runs: sparse });
    const sparseStream = await fetchStreamWindow((page, created) => sparseGitHub.rest.actions.listWorkflowRuns({ branch: "main", page, ...(created ? { created } : {}) }), 37001, 40000);
    expect(sparseStream).toMatchObject({ reachedFloor: true, maxVerified: 40000 });
    expect(sparseStream.items.map(item => item.run_number)).toContain(37600);
    expect(sparseGitHub.calls.lists.map(call => call.page)).toEqual([1, 10, 2]);
  });

  it("drains out-of-order same-second ties across page 10 and verifies upper ties", async () => {
    const remote = bareRemote();
    const agent = { name: "agent", conclusion: "failure", started_at: "2026-11-24T09:00:00Z" };
    const writer = conclusion => ({ name: "push_routing_history", conclusion, started_at: "2026-11-24T09:10:00Z" });

    // 1. 2,002 runs (#2002..#1) where 206 runs (#1100..#895) share one creation second T, ordered so
    // #898 lands on row 1,000 (last row of page 10) while target #899 (a failed save) is pushed beyond
    // the 1,000-run cap onto page 3 of the inclusive created<=T window.
    const tieTime = runCreatedAt(1000);
    const newerRuns = Array.from({ length: 902 }, (_, i) => paginationRun(2002 - i));
    const tiedNumbers = [
      ...Array.from({ length: 97 }, (_, i) => 1100 - i), // 97 rows (#1100..#1004)
      898, // row 1,000 (last row of page 10): minNum <= 899 <= maxNum, hiding #899 past page 10!
      ...Array.from({ length: 104 }, (_, i) => 1003 - i), // #1003..#900
      897,
      896,
      895,
      899, // row 206 of second T (lands on page 3 of created<=T)
    ];
    const tiedRuns = tiedNumbers.map(number => paginationRun(number, { created_at: tieTime, conclusion: number === 899 ? "failure" : "success" }));
    const olderRuns = Array.from({ length: 894 }, (_, i) => paginationRun(894 - i));
    const outOfOrderRuns = [...newerRuns, ...tiedRuns, ...olderRuns];
    const tieGitHub = fakeGitHub(remote, { runs: outOfOrderRuns, jobs: { 9899: [agent, writer("failure")] } });
    const tieResult = await reconcilePagination(tieGitHub, 898, 2002);
    expect(tieGitHub.calls.lists.every(call => (call.page || 1) <= 10)).toBe(true);
    expect(tieGitHub.calls.lists.some(call => call.created === `<=${tieTime}`)).toBe(true);
    expect(tieResult.reconciliation.verified).toBe(true);
    expect(tieResult.reconciliation).not.toHaveProperty("ceiling");
    expect(tieResult.listing.filter(item => item.conclusion !== "success")).toEqual([expect.objectContaining({ id: "9899", number: 899, conclusion: "failure", agent_ran: true })]);

    // 3. Removing created_at from a nonterminal page-10 floor boundary must fail closed rather than
    // accepting page 10 without ruling out a same-second tie past row 1,000.
    const missingPage10Timestamps = outOfOrderRuns.map((run, idx) => (idx >= 900 && idx < 1000 ? { ...run, created_at: undefined } : run));
    const missingP10GitHub = fakeGitHub(remote, { runs: missingPage10Timestamps, jobs: { 9899: [agent, writer("failure")] } });
    const missingP10Result = await reconcilePagination(missingP10GitHub, 898, 2002);
    expect(missingP10Result.reconciliation.verified).toBe(false);

    // 4. Upward walk must not stop early mid-tie when a high run_number (#600) appears on page 5
    // of a 500-run same-second tie (#600..#101) while pages 1..4 hold lower-numbered tied runs.
    const upperTieTime = runCreatedAt(500);
    const upperTieNumbers = [
      ...Array.from({ length: 400 }, (_, i) => 599 - i), // pages 1..4 (#599..#200)
      600, // page 5 starts with #600 (>= maxRunNumber)
      ...Array.from({ length: 99 }, (_, i) => 199 - i), // rest of page 5 (#199..#101)
    ];
    const upperTieRuns = [...upperTieNumbers.map(number => paginationRun(number, { created_at: upperTieTime, conclusion: number === 550 ? "failure" : "success" })), ...Array.from({ length: 100 }, (_, i) => paginationRun(100 - i))];
    const upperTieGitHub = fakeGitHub(remote, { runs: upperTieRuns, jobs: { 9550: [agent, writer("failure")] } });
    const upperTieResult = await reconcilePagination(upperTieGitHub, 1, 600);
    expect(upperTieResult.reconciliation.verified).toBe(true);
    expect(upperTieResult.reconciliation.unresolved).toEqual([]);
    expect(upperTieResult.listing.filter(item => item.conclusion !== "success")).toEqual([expect.objectContaining({ id: "9550", number: 550, conclusion: "failure", agent_ran: true })]);

    // 5. An 800-row same-second tie covering the floor (#4900..#4101) on a 5,000-run workflow is read
    // page by page past the tie instead of stalling.
    const largeFloorTieTime = runCreatedAt(4500);
    const largeFloorTieRuns = Array.from({ length: 5000 }, (_, i) => {
      const number = 5000 - i;
      const createdAt = number >= 4101 && number <= 4900 ? largeFloorTieTime : runCreatedAt(number);
      return paginationRun(number, { created_at: createdAt, conclusion: number === 4500 ? "failure" : "success" });
    });
    const largeFloorTieGitHub = fakeGitHub(remote, { runs: largeFloorTieRuns, jobs: { 13500: [agent, writer("failure")] } });
    const largeFloorTieResult = await reconcilePagination(largeFloorTieGitHub, 4100, 5000);
    expect(largeFloorTieGitHub.calls.lists.every(call => (call.page || 1) <= 10)).toBe(true);
    expect(largeFloorTieResult.reconciliation.verified).toBe(true);
    expect(largeFloorTieResult.reconciliation.unresolved).toEqual([]);
    expect(largeFloorTieResult.listing.filter(item => item.conclusion !== "success")).toEqual([expect.objectContaining({ id: "13500", number: 4500, conclusion: "failure", agent_ran: true })]);
  });

  it("treats old missing run numbers as deleted while retaining recent gaps until they age past the 5-minute window", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-11-25T10:10:00Z"));
    const remote = bareRemote();
    await save(remote, 100, "success");

    // #101 is missing with higher neighbour #102 created 10 minutes ago (> 5m): confirmed deleted, not persisted.
    // #103 is missing with higher neighbour #104 created 2 minutes ago (<= 5m): recent gap, retained in unresolved_runs.
    const runsAt104 = [paginationRun(104, { created_at: "2026-11-25T10:08:00Z" }), paginationRun(102, { created_at: "2026-11-25T10:00:00Z" }), paginationRun(100, { created_at: "2026-11-25T09:50:00Z" })];
    await save(remote, 104, "success", { github: fakeGitHub(remote, { runs: runsAt104 }) });
    expect(remoteState(remote)).toMatchObject({ last_run: 104, reconciled_through: 104, unresolved_runs: [103], unexplained: 0 });
    expect(remoteState(remote).unresolved_run_ids).toBeUndefined();

    // Advance past the 5-minute window (now #104 is 7 minutes old): #103 is still absent across both sides and expires.
    vi.setSystemTime(new Date("2026-11-25T10:15:00Z"));
    const runsAt105 = [paginationRun(105, { created_at: "2026-11-25T10:14:00Z" }), ...runsAt104];
    await save(remote, 105, "success", { github: fakeGitHub(remote, { runs: runsAt105 }) });
    expect(remoteState(remote)).toMatchObject({ last_run: 105, reconciled_through: 105, unresolved_runs: [], unexplained: 0 });
  });

  it("recovers an older ID-less unresolved run on unfiltered page 4 even when an unrelated PR run on page 1 triggers hasIntervalGap", async () => {
    const remote = bareRemote();
    seedHistory(
      remote,
      {
        ...emptyState("triage"),
        generation: { hash: GENERATION, since_run: 1 },
        last_run: 399,
        reconciled_through: 399,
        unresolved_runs: [50],
        unexplained: 0,
      },
      []
    );
    const agent = { name: "agent", conclusion: "failure", started_at: "2026-11-24T09:00:00Z" };
    const writer = conclusion => ({ name: "push_routing_history", conclusion, started_at: "2026-11-24T09:10:00Z" });
    // Build 400 runs (#400 down to #1): #400 is an off-branch pull_request run on page 1 (triggering hasIntervalGap),
    // #200..#399 are 200 default-branch runs so defaultStream page 1 stops at #300 (never seeing #50),
    // and #50 is a failed eligible run on unfiltered page 4 (index 350).
    const runs = Array.from({ length: 400 }, (_, i) => {
      const number = 400 - i;
      if (number === 400) return paginationRun(400, { head_branch: "feature-pr", event: "pull_request" });
      if (number === 50) return paginationRun(50, { conclusion: "failure" });
      const isOffBranch = number > 50 && number < 200;
      return paginationRun(number, { head_branch: isOffBranch ? "feature-pr" : "main", event: isOffBranch ? "pull_request" : "schedule" });
    });
    const github = fakeGitHub(remote, { runs, jobs: { 9050: [agent, writer("failure")] } });

    await save(remote, 401, "success", { github });
    expect(github.calls.lists.some(params => !params.branch && !params.event && params.page === 4)).toBe(true);
    expect(github.calls.jobs).toEqual([9050]);
    expect(remoteRuns(remote).map(({ number, note }) => ({ number, note }))).toEqual([
      { number: 50, note: "unsaved" },
      { number: 401, note: null },
    ]);
    expect(remoteState(remote)).toMatchObject({ last_run: 401, reconciled_through: 401, unresolved_runs: [], unexplained: 1 });
  });

  it("recovers a run shifted across a page boundary by a mid-scan deletion under a capped count even with clock skew", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-11-25T10:30:00Z"));
    const remote = bareRemote();
    seedHistory(
      remote,
      {
        ...emptyState("triage"),
        generation: { hash: GENERATION, since_run: 1 },
        last_run: 302,
        reconciled_through: 302,
        unresolved_runs: [202],
        unexplained: 0,
      },
      []
    );
    const agent = { name: "agent", conclusion: "failure", started_at: "2026-11-25T09:00:00Z" };
    const writer = conclusion => ({ name: "push_routing_history", conclusion, started_at: "2026-11-25T09:05:00Z" });
    // Build 302 runs (#302 down to #1) where #202 is an unsaved failed run at index 100 (first item of page 2),
    // with created_at 7 seconds after #203 (clock skew). With a capped total_count (like GitHub's 2,500 cap),
    // deleting #300 before unfiltered page 2 keeps total_count unchanged while shifting #202 onto page 1 so
    // page 2 starts at #201. The overlapping unfiltered boundary check must recover #202 instead of dropping it.
    const baseMs = Date.parse("2026-11-25T06:00:00Z");
    const liveRuns = Array.from({ length: 302 }, (_, i) => {
      const number = 302 - i;
      const createdMs = number === 202 ? baseMs + 203 * 30_000 + 7_000 : baseMs + number * 30_000;
      return paginationRun(number, {
        conclusion: number === 202 ? "failure" : "success",
        created_at: new Date(createdMs).toISOString().replace(/\.\d{3}Z$/, "Z"),
      });
    });
    let deleted300 = false;
    const github = fakeGitHub(remote, {
      jobs: { 9202: [agent, writer("failure")] },
      listWorkflowRuns: async ({ branch, event, page = 1, per_page = 100 }) => {
        if (!branch && !event && per_page === 100 && page === 2 && !deleted300) {
          deleted300 = true;
          const idx = liveRuns.findIndex(r => r.run_number === 300);
          if (idx >= 0) liveRuns.splice(idx, 1);
        }
        const filtered = liveRuns.filter(run => (!branch || run.head_branch === branch) && (!event || run.event === event));
        const start = (page - 1) * per_page;
        const total = event === "pull_request_target" ? 0 : 2500;
        return { data: { total_count: total, workflow_runs: filtered.slice(start, start + per_page) } };
      },
    });

    await save(remote, 303, "success", { github });
    expect(github.calls.lists.some(params => !params.branch && !params.event && params.per_page < 100)).toBe(true);
    expect(github.calls.jobs).toEqual([9202]);
    expect(remoteRuns(remote).map(({ number, note }) => ({ number, note }))).toEqual([
      { number: 202, note: "unsaved" },
      { number: 303, note: null },
    ]);
    expect(remoteState(remote)).toMatchObject({ last_run: 303, reconciled_through: 303, unresolved_runs: [], unexplained: 1 });
  });

  it("expires a confirmed deleted run at a deep page boundary with a bounded same-second burst, and fails closed when a 100-run tie fills the page edge", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-11-25T16:00:00Z"));
    const baseMs = Date.parse("2026-11-25T00:00:00Z");
    const burstTs = new Date(baseMs + 516 * 30_000).toISOString().replace(/\.\d{3}Z$/, "Z");

    // 1. 30 runs (#531..#502) share burstTs right above deleted #501 at the page 4/5 boundary:
    // the overlapping unfiltered page (per_page < 100) brackets the entire tie strictly inside its
    // oldest/newest seconds and confirms #501 is deleted.
    const remote1 = bareRemote();
    seedHistory(
      remote1,
      {
        ...emptyState("triage"),
        generation: { hash: GENERATION, since_run: 1 },
        last_run: 901,
        reconciled_through: 901,
        unresolved_runs: [501],
        unexplained: 0,
      },
      []
    );
    const runs30 = [];
    for (let number = 901; number >= 1; number--) {
      if (number === 501) continue;
      const createdAt = number >= 502 && number <= 531 ? burstTs : new Date(baseMs + number * 30_000).toISOString().replace(/\.\d{3}Z$/, "Z");
      runs30.push(paginationRun(number, { created_at: createdAt }));
    }
    const gh30 = fakeGitHub(remote1, { runs: runs30 });
    await save(remote1, 902, "success", { github: gh30 });
    expect(gh30.calls.lists.some(params => !params.branch && !params.event && params.per_page < 100)).toBe(true);
    expect(remoteState(remote1)).toMatchObject({ last_run: 902, reconciled_through: 902, unresolved_runs: [], unexplained: 0 });

    // 2. When 100 runs (#601..#502) share burstTs, no single <=100-row page can bracket both sides
    // of the tie, so the writer fails closed and retains #501 rather than risking a false deletion.
    const burstTs100 = new Date(baseMs + 551 * 30_000).toISOString().replace(/\.\d{3}Z$/, "Z");
    const runs100 = [];
    for (let number = 901; number >= 1; number--) {
      if (number === 501) continue;
      const createdAt = number >= 502 && number <= 601 ? burstTs100 : new Date(baseMs + number * 30_000).toISOString().replace(/\.\d{3}Z$/, "Z");
      runs100.push(paginationRun(number, { created_at: createdAt }));
    }
    const { reconciliation } = await listRuns({
      githubClient: fakeGitHub(null, { runs: runs100 }),
      owner: "octo",
      repo: "demo",
      workflowFile: "triage.lock.yml",
      defaultBranch: "main",
      runId: "9902",
      runNumber: 902,
      state: { ...emptyState("triage"), generation: { hash: GENERATION, since_run: 1 }, last_run: 901, reconciled_through: 901, unresolved_runs: [501], unexplained: 0 },
      runs: [],
      generation: GENERATION,
    });
    expect(reconciliation.unresolved).toEqual([501]);
  });

  it("does not drop a live failed run hidden by same-second tie reordering across a page edge", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-11-25T16:00:00Z"));
    const remote = bareRemote();
    seedHistory(
      remote,
      {
        ...emptyState("triage"),
        generation: { hash: GENERATION, since_run: 1 },
        last_run: 1,
        reconciled_through: 1,
        unresolved_runs: [],
        unexplained: 0,
      },
      []
    );
    const agent = { name: "agent", conclusion: "failure", started_at: "2026-11-25T09:00:00Z" };
    const writer = conclusion => ({ name: "push_routing_history", conclusion, started_at: "2026-11-25T09:05:00Z" });
    // Old runs #100, #101 (failed), #102 share tieTs at the page 1/2 edge of a 200-run listing.
    // Reordering the tie between page 1 and page 2 puts both #102 and #100 on page 1 while omitting #101
    // from both 100-row pages. Because #100 sits on page 1's oldest second, page 1 cannot expire #101,
    // and the overlapping unfiltered page recovers and charges #101.
    const baseMs = Date.parse("2026-11-25T00:00:00Z");
    const tieTs = new Date(baseMs + 101 * 30_000).toISOString().replace(/\.\d{3}Z$/, "Z");
    const canonical = Array.from({ length: 200 }, (_, i) => {
      const number = 200 - i;
      const createdAt = number >= 100 && number <= 102 ? tieTs : new Date(baseMs + number * 30_000).toISOString().replace(/\.\d{3}Z$/, "Z");
      return paginationRun(number, { conclusion: number === 101 ? "failure" : "success", created_at: createdAt });
    });
    const run100 = canonical.find(r => r.run_number === 100);
    const run101 = canonical.find(r => r.run_number === 101);
    const run102 = canonical.find(r => r.run_number === 102);
    const page1Order = canonical.map(r => (r.run_number === 101 ? run100 : r.run_number === 100 ? run101 : r));
    const page2Order = canonical.map(r => (r.run_number === 102 ? run101 : r.run_number === 101 ? run102 : r));
    const github = fakeGitHub(remote, {
      jobs: { 9101: [agent, writer("failure")] },
      listWorkflowRuns: async ({ branch, event, page = 1, per_page = 100 }) => {
        if (event === "pull_request_target") return { data: { total_count: 0, workflow_runs: [] } };
        const source = per_page === 100 && page === 1 ? page1Order : per_page === 100 && page === 2 ? page2Order : canonical;
        const filtered = source.filter(run => (!branch || run.head_branch === branch) && (!event || run.event === event));
        const start = (page - 1) * per_page;
        return { data: { total_count: filtered.length, workflow_runs: filtered.slice(start, start + per_page) } };
      },
    });

    await save(remote, 200, "success", { github });
    expect(github.calls.lists.some(params => !params.branch && !params.event && params.per_page < 100)).toBe(true);
    expect(github.calls.jobs).toEqual([9101]);
    expect(remoteRuns(remote).map(({ number, note }) => ({ number, note }))).toEqual([
      { number: 101, note: "unsaved" },
      { number: 200, note: null },
    ]);
    expect(remoteState(remote)).toMatchObject({ last_run: 200, reconciled_through: 200, unresolved_runs: [], unexplained: 1 });
  });

  it("expires a >200-run deleted gap directly above the floor during a partial created-window walk", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-11-25T16:00:00Z"));
    // 3,500-run history with #2..#202 (201 runs) deleted right above floor #1 and distinct timestamps.
    // The walk stops before reaching window 0's page 1 (partial walk), yet one atomic page inside the
    // created window strictly brackets #203 and #1, so coverage advances instead of stalling at #1.
    const runs = [];
    for (let number = 3500; number >= 1; number--) {
      if (number >= 2 && number <= 202) continue;
      runs.push(paginationRun(number));
    }
    const github = fakeGitHub(null, { runs });
    const { reconciliation } = await reconcilePagination(github, 1, 3501);
    expect(reconciliation.verified).toBe(true);
    expect(reconciliation.unresolved).toEqual([]);
    expect(reconciliation.ceiling).toBeGreaterThan(203);

    // Also verify when the >200-run deleted gap sits directly on a cross-window created cutoff:
    // #3500..#503 fills three 1,000-row windows so #503 is both the last row of window 2 and the
    // first row of window 3 (created <= #503), while #302..#502 are deleted directly above floor #301.
    const cutoffRuns = [];
    for (let number = 3500; number >= 1; number--) {
      if (number >= 302 && number <= 502) continue;
      cutoffRuns.push(paginationRun(number));
    }
    const cutoffGh = fakeGitHub(null, { runs: cutoffRuns });
    const cutoffRes = await reconcilePagination(cutoffGh, 301, 3501);
    expect(cutoffRes.reconciliation.verified).toBe(true);
    expect(cutoffRes.reconciliation.unresolved).toEqual([]);
    expect(cutoffRes.reconciliation.ceiling).toBeGreaterThan(503);
  });

  it("clears an interval gap when the independent unindexed scan discovers it as an ineligible PR run", async () => {
    const remote = bareRemote();
    seedHistory(
      remote,
      {
        ...emptyState("triage"),
        generation: { hash: GENERATION, since_run: 1 },
        last_run: 500,
        reconciled_through: 500,
        unresolved_runs: [160],
        unexplained: 0,
      },
      []
    );
    // 702 runs (#702..#1): off-branch PR run #502 starts at index 200 (first item of page 3).
    // Under a capped total_count, deleting #650 right before the interval scan's unfiltered page 3 shifts #502
    // onto page 2 (already fetched), hiding #502 from the interval scan. The independent scan for older ID-less
    // obligation #160 then sees #502 on page 2 and marks it ineligible; #502 must not remain in unresolved_runs.
    const liveRuns = Array.from({ length: 702 }, (_, i) => {
      const number = 702 - i;
      if (number === 502 || number === 160) return paginationRun(number, { head_branch: "feature-pr", event: "pull_request" });
      return paginationRun(number);
    });
    let unfilteredPage3Calls = 0;
    const github = fakeGitHub(remote, {
      listWorkflowRuns: async ({ branch, event, page = 1, per_page = 100 }) => {
        if (!branch && !event && per_page === 100 && page === 3) {
          unfilteredPage3Calls += 1;
          if (unfilteredPage3Calls === 1) {
            const idx = liveRuns.findIndex(r => r.run_number === 650);
            if (idx >= 0) liveRuns.splice(idx, 1);
          }
        }
        const filtered = liveRuns.filter(run => (!branch || run.head_branch === branch) && (!event || run.event === event));
        const start = (page - 1) * per_page;
        const total = event === "pull_request_target" ? 0 : !branch && !event ? 2500 : filtered.length;
        return { data: { total_count: total, workflow_runs: filtered.slice(start, start + per_page) } };
      },
    });

    await save(remote, 703, "success", { github });
    expect(unfilteredPage3Calls).toBe(2);
    expect(remoteState(remote)).toMatchObject({ last_run: 703, reconciled_through: 703, unresolved_runs: [], unexplained: 0 });
  });

  it("visits all 20 tracked unresolved run IDs across consecutive even-numbered saves without modulo resonance", async () => {
    const remote = bareRemote();
    const unresolvedRuns = Array.from({ length: 20 }, (_, i) => i + 1);
    const unresolvedRunIds = Object.fromEntries(unresolvedRuns.map(n => [String(n), String(9000 + n)]));
    seedHistory(
      remote,
      {
        ...emptyState("triage"),
        generation: { hash: GENERATION, since_run: 1 },
        last_run: 900,
        reconciled_through: 900,
        unresolved_runs: unresolvedRuns,
        unresolved_run_ids: unresolvedRunIds,
        unexplained: 0,
      },
      []
    );
    const runsById = Object.fromEntries(unresolvedRuns.map(n => [9000 + n, paginationRun(n, { status: "in_progress", conclusion: null })]));
    const recentRuns = Array.from({ length: 100 }, (_, i) => paginationRun(900 - i));
    const github = fakeGitHub(remote, { runs: recentRuns, runsById });

    // Even-numbered writers #902 and #904 (odd numbers are off-branch PRs) must cover all 20 tracked IDs across two saves.
    recentRuns.unshift(paginationRun(901, { head_branch: "pr-901", event: "pull_request" }));
    await save(remote, 902, "success", { github });
    expect(github.calls.runs).toEqual(Array.from({ length: 10 }, (_, i) => 9001 + i));
    expect(remoteState(remote).unresolved_lookup_cursor).toBe(10);

    recentRuns.unshift(paginationRun(903, { head_branch: "pr-903", event: "pull_request" }), paginationRun(902));
    await save(remote, 904, "success", { github });
    expect(github.calls.runs).toEqual(Array.from({ length: 20 }, (_, i) => 9001 + i));
    expect(remoteState(remote).unresolved_lookup_cursor).toBe(20);
  });

  it("advances reconciled_through in chunks across saves when the boundary-check budget is exhausted before a >200-run boundary gap", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-11-25T16:00:00Z"));
    const remote = bareRemote();
    await save(remote, 1, "success");

    // Build 6 pages of old listed runs (600 items) with 5 page-boundary deletion gaps:
    // four single-run gaps at boundaries 1..4 and one 201-run gap at boundary 5. Save 1 spends its
    // 4-check budget on the lowest 4 boundaries, clamps reconciled_through below the unchecked
    // 201-run gap, and Save 2 finishes advancing to head.
    const baseMs = Date.parse("2026-11-25T00:00:00Z");
    const runs = [];
    const ranges = [
      [805, 706],
      [504, 405],
      [403, 304],
      [302, 203],
      [201, 102],
      [100, 1],
    ];
    for (const [hi, lo] of ranges) {
      for (let number = hi; number >= lo; number--) {
        runs.push(paginationRun(number, { created_at: new Date(baseMs + number * 30_000).toISOString().replace(/\.\d{3}Z$/, "Z") }));
      }
    }
    const github = fakeGitHub(remote, { runs });

    await save(remote, 805, "success", { github });
    const mid = remoteState(remote);
    expect(mid.reconciled_through).toBe(504);
    expect(mid.unresolved_runs).toEqual([]);

    runs.unshift(paginationRun(806, { created_at: new Date(baseMs + 806 * 30_000).toISOString().replace(/\.\d{3}Z$/, "Z") }));
    await save(remote, 806, "success", { github });
    expect(remoteState(remote)).toMatchObject({ last_run: 806, reconciled_through: 806, unresolved_runs: [], unexplained: 0 });
  });

  it("preserves direct-lookup cursor rotation and verified interval coverage when an independent ID-less scan fails", async () => {
    const remote = bareRemote();
    const trackedIds = Array.from({ length: 20 }, (_, i) => i + 1);
    const unresolvedRunIds = Object.fromEntries(trackedIds.map(n => [String(n), String(9000 + n)]));
    seedHistory(
      remote,
      {
        ...emptyState("triage"),
        generation: { hash: GENERATION, since_run: 1 },
        last_run: 900,
        reconciled_through: 900,
        unresolved_runs: [...trackedIds, 50],
        unresolved_run_ids: unresolvedRunIds,
        unexplained: 0,
      },
      []
    );
    const runsById = Object.fromEntries(trackedIds.map(n => [9000 + n, paginationRun(n, { status: "in_progress", conclusion: null })]));
    const recentRuns = Array.from({ length: 100 }, (_, i) => paginationRun(900 - i));
    const github = fakeGitHub(remote, {
      runsById,
      listWorkflowRuns: async ({ branch, event, page = 1, per_page = 100 }) => {
        if (!branch && !event && page > 1) {
          throw Object.assign(new Error("503 service unavailable"), { status: 503 });
        }
        const filtered = recentRuns.filter(run => (!branch || run.head_branch === branch) && (!event || run.event === event));
        const start = (page - 1) * per_page;
        return { data: { total_count: event === "pull_request_target" ? 0 : 900, workflow_runs: filtered.slice(start, start + per_page) } };
      },
    });

    // Save #902: unfiltered page 2+ throws 503 during the independent scan for ID-less #50.
    // Direct lookups 1..10, cursor 10, and verified interval coverage through 902 must still persist.
    recentRuns.unshift(paginationRun(901, { head_branch: "pr-901", event: "pull_request" }));
    await save(remote, 902, "success", { github });
    expect(global.core.warning).toHaveBeenCalledWith(expect.stringMatching(/503 service unavailable/));
    expect(github.calls.runs).toEqual(Array.from({ length: 10 }, (_, i) => 9001 + i));
    expect(remoteState(remote)).toMatchObject({ reconciled_through: 902, unresolved_lookup_cursor: 10 });
    expect(remoteState(remote).unresolved_runs).toContain(50);

    // Save #904 advances direct lookups to 11..20 despite the same deep-page 503.
    recentRuns.unshift(paginationRun(903, { head_branch: "pr-903", event: "pull_request" }), paginationRun(902));
    await save(remote, 904, "success", { github });
    expect(github.calls.runs).toEqual(Array.from({ length: 20 }, (_, i) => 9001 + i));
    expect(remoteState(remote)).toMatchObject({ reconciled_through: 904, unresolved_lookup_cursor: 20 });
    expect(remoteState(remote).unresolved_runs).toContain(50);
  });

  it("falls back to the newest uncovered ID-less obligation when the oldest exceeds the search call budget", async () => {
    const agent = { name: "agent", conclusion: "failure", started_at: "2026-11-24T09:00:00Z" };
    const writer = conclusion => ({ name: "push_routing_history", conclusion, started_at: "2026-11-24T09:10:00Z" });
    // 10,000 runs with unresolved [50, 4500]: #50 lies beyond MAX_SEARCH_CALLS=16, while live failed
    // #4500 is reachable via the fallback scan against the newest remaining uncovered obligation.
    const runs = Array.from({ length: 10000 }, (_, i) => {
      const number = 10000 - i;
      return paginationRun(number, { conclusion: number === 4500 ? "failure" : "success" });
    });
    const github = fakeGitHub(null, { runs, jobs: { 13500: [agent, writer("failure")] } });
    const { listing, reconciliation } = await listRuns({
      githubClient: github,
      owner: "octo",
      repo: "demo",
      workflowFile: "triage.lock.yml",
      defaultBranch: "main",
      runId: "19001",
      runNumber: 10001,
      state: { ...emptyState("triage"), generation: { hash: GENERATION, since_run: 1 }, last_run: 10000, reconciled_through: 10000, unresolved_runs: [50, 4500], unexplained: 0 },
      runs: [],
      generation: GENERATION,
    });
    expect(listing).toEqual([expect.objectContaining({ id: "13500", number: 4500, conclusion: "failure", agent_ran: true })]);
    expect(reconciliation.resolved).toContain(4500);
    expect(reconciliation.unresolved).toEqual([50]);
  });

  it("retains a failing run and still advances the direct-lookup cursor when per-run job enrichment throws", async () => {
    const remote = bareRemote();
    const trackedIds = Array.from({ length: 20 }, (_, i) => i + 1);
    const unresolvedRunIds = Object.fromEntries(trackedIds.map(n => [String(n), String(9000 + n)]));
    seedHistory(
      remote,
      {
        ...emptyState("triage"),
        generation: { hash: GENERATION, since_run: 1 },
        last_run: 900,
        reconciled_through: 900,
        unresolved_runs: trackedIds,
        unresolved_run_ids: unresolvedRunIds,
        unexplained: 0,
      },
      []
    );
    const runsById = Object.fromEntries(trackedIds.map(n => [9000 + n, paginationRun(n, n === 3 ? { status: "completed", conclusion: "failure" } : { status: "in_progress", conclusion: null })]));
    const recentRuns = Array.from({ length: 100 }, (_, i) => paginationRun(900 - i));
    const github = fakeGitHub(remote, { runs: recentRuns, runsById });
    const origJobs = github.rest.actions.listJobsForWorkflowRunAttempt;
    github.rest.actions.listJobsForWorkflowRunAttempt = async params => {
      if (params.run_id === 9003) throw Object.assign(new Error("500 internal error"), { status: 500 });
      return origJobs(params);
    };

    // Save #902 checks IDs 1..10: #3's job lookup throws, but #3 stays unresolved with its ID and
    // cursor advances to 10 so Save #904 still visits IDs 11..20 instead of starving them.
    recentRuns.unshift(paginationRun(901, { head_branch: "pr-901", event: "pull_request" }));
    await save(remote, 902, "success", { github });
    expect(global.core.warning).toHaveBeenCalledWith(expect.stringMatching(/500 internal error/));
    expect(github.calls.runs).toEqual(Array.from({ length: 10 }, (_, i) => 9001 + i));
    expect(remoteState(remote)).toMatchObject({ reconciled_through: 902, unresolved_lookup_cursor: 10 });
    expect(remoteState(remote).unresolved_runs).toContain(3);
    expect(remoteState(remote).unresolved_run_ids["3"]).toBe("9003");

    recentRuns.unshift(paginationRun(903, { head_branch: "pr-903", event: "pull_request" }), paginationRun(902));
    await save(remote, 904, "success", { github });
    expect(github.calls.runs).toEqual(Array.from({ length: 20 }, (_, i) => 9001 + i));
    expect(remoteState(remote)).toMatchObject({ reconciled_through: 904, unresolved_lookup_cursor: 20 });
  });

  it("reconciles and charges a failed first attempt immediately while a re-run is still in progress", async () => {
    const remote = bareRemote();
    await save(remote, 50, "success");
    const agent = { name: "agent", conclusion: "failure", started_at: "2026-11-24T09:00:00Z" };
    const writer = conclusion => ({ name: "push_routing_history", conclusion, started_at: "2026-11-24T09:10:00Z" });
    const run51 = { id: 9051, run_number: 51, status: "in_progress", conclusion: null, head_branch: "main", event: "schedule", run_attempt: 2 };
    const github = fakeGitHub(remote, {
      runs: [run51, { id: 9050, run_number: 50, status: "completed", conclusion: "success", head_branch: "main", event: "schedule", run_attempt: 1 }],
      attempts: { 9051: { status: "completed", conclusion: "failure" } },
      jobs: { 9051: [agent, writer("failure")] },
    });

    await save(remote, 52, "success", { github });
    expect(github.calls.attempts).toEqual([9051]);
    expect(github.calls.attemptParams).toEqual([expect.objectContaining({ run_id: 9051, attempt_number: 1 })]);
    expect(github.calls.jobs).toEqual([9051]);
    expect(github.calls.jobParams).toEqual([expect.objectContaining({ run_id: 9051, attempt_number: 1 })]);
    expect(remoteRuns(remote).map(({ number, attempt, note }) => ({ number, attempt, note }))).toEqual([
      { number: 50, attempt: 1, note: null },
      { number: 51, attempt: 1, note: "unsaved" },
      { number: 52, attempt: 1, note: null },
    ]);
    expect(remoteState(remote)).toMatchObject({ last_run: 52, reconciled_through: 52, unresolved_runs: [], unexplained: 1 });

    // A later save while attempt 2 is still running (or once it succeeds) does not re-query or double-charge #51.
    const nextGitHub = fakeGitHub(remote, {
      runs: [
        { id: 9052, run_number: 52, status: "completed", conclusion: "success", head_branch: "main", event: "schedule", run_attempt: 1 },
        { ...run51, status: "completed", conclusion: "success" },
      ],
      attempts: { 9051: { status: "completed", conclusion: "failure" } },
      jobs: { 9051: [agent, writer("failure")] },
    });
    await save(remote, 53, "success", { github: nextGitHub });
    expect(nextGitHub.calls.attempts).toEqual([]);
    expect(nextGitHub.calls.jobs).toEqual([]);
    expect(remoteState(remote)).toMatchObject({ last_run: 53, reconciled_through: 53, unresolved_runs: [], unexplained: 1 });
  });

  it("handles active re-runs for tracked unresolved runs, saved first attempts, and incomplete first attempts", async () => {
    const remote = bareRemote();
    await save(remote, 50, "success");
    const agent = { name: "agent", conclusion: "failure", started_at: "2026-11-24T09:00:00Z" };
    const writer = conclusion => ({ name: "push_routing_history", conclusion, started_at: "2026-11-24T09:10:00Z" });

    // #51 starts as an in-progress attempt 1 and is tracked in unresolved_runs by #52.
    const run51 = { id: 9051, run_number: 51, status: "in_progress", conclusion: null, head_branch: "main", event: "schedule", run_attempt: 1 };
    await save(remote, 52, "success", { github: fakeGitHub(remote, { runs: [run51] }) });
    expect(remoteState(remote)).toMatchObject({ reconciled_through: 52, unresolved_runs: [51], unexplained: 0 });

    // Before #55 saves:
    // - #51 failed attempt 1 and is now re-running (attempt 2 in_progress): must reconcile and clear from unresolved_runs.
    // - #53 succeeded attempt 1 and is re-running (attempt 2 in_progress): must reconcile without querying jobs or marking unresolved.
    // - #54 reports run_attempt 2 in_progress, but attempt 1 itself is not yet completed: must stay in unresolved_runs without querying jobs.
    const github = fakeGitHub(remote, {
      runs: [
        { id: 9054, run_number: 54, status: "in_progress", conclusion: null, head_branch: "main", event: "schedule", run_attempt: 2 },
        { id: 9053, run_number: 53, status: "in_progress", conclusion: null, head_branch: "main", event: "schedule", run_attempt: 2 },
        { id: 9052, run_number: 52, status: "completed", conclusion: "success", head_branch: "main", event: "schedule", run_attempt: 1 },
        { id: 9051, run_number: 51, status: "in_progress", conclusion: null, head_branch: "main", event: "schedule", run_attempt: 2 },
      ],
      attempts: {
        9051: { status: "completed", conclusion: "failure" },
        9053: { status: "completed", conclusion: "success" },
        9054: { status: "in_progress", conclusion: null },
      },
      jobs: { 9051: [agent, writer("failure")] },
    });

    await save(remote, 55, "success", { github });
    expect(github.calls.attempts).toEqual([9051, 9053, 9054]);
    expect(github.calls.jobs).toEqual([9051]);
    expect(remoteRuns(remote).map(({ number, note }) => ({ number, note }))).toEqual([
      { number: 50, note: null },
      { number: 52, note: null },
      { number: 51, note: "unsaved" },
      { number: 55, note: null },
    ]);
    expect(remoteState(remote)).toMatchObject({
      last_run: 55,
      reconciled_through: 55,
      unresolved_runs: [54],
      unresolved_run_ids: { "54": "9054" },
      unexplained: 1,
    });
  });

  it("retains an active re-run as unresolved without discarding unrelated reconciliation when attempt-1 or job lookup fails transiently", async () => {
    const remote = bareRemote();
    await save(remote, 50, "success");
    const agent = { name: "agent", conclusion: "failure", started_at: "2026-11-24T09:00:00Z" };
    const writer = conclusion => ({ name: "push_routing_history", conclusion, started_at: "2026-11-24T09:10:00Z" });
    const run50 = { id: 9050, run_number: 50, status: "completed", conclusion: "success", head_branch: "main", event: "schedule", run_attempt: 1 };
    const run51 = { id: 9051, run_number: 51, status: "completed", conclusion: "failure", head_branch: "main", event: "schedule", run_attempt: 1 };
    const run52 = { id: 9052, run_number: 52, status: "in_progress", conclusion: null, head_branch: "main", event: "schedule", run_attempt: 2 };

    // 1. Attempt-1 lookup for active re-run #52 fails with 503: #51 must still be charged while #52 stays unresolved.
    const gh53 = fakeGitHub(remote, { runs: [run52, run51, run50], jobs: { 9051: [agent, writer("failure")] } });
    gh53.rest.actions.getWorkflowRunAttempt = async () => {
      throw Object.assign(new Error("503 service unavailable"), { status: 503 });
    };
    await save(remote, 53, "success", { github: gh53 });
    expect(remoteRuns(remote).map(({ number, note }) => ({ number, note }))).toEqual([
      { number: 50, note: null },
      { number: 51, note: "unsaved" },
      { number: 53, note: null },
    ]);
    expect(remoteState(remote)).toMatchObject({
      last_run: 53,
      reconciled_through: 53,
      unresolved_runs: [52],
      unresolved_run_ids: { "52": "9052" },
      unexplained: 1,
    });

    // 2. Attempt-1 lookup succeeds, but attempt-1 jobs lookup fails with 503: #52 remains unresolved for retry.
    const run53 = { id: 9053, run_number: 53, status: "completed", conclusion: "success", head_branch: "main", event: "schedule", run_attempt: 1 };
    const gh54 = fakeGitHub(remote, {
      runs: [run53, run52, run51, run50],
      attempts: { 9052: { status: "completed", conclusion: "failure" } },
    });
    gh54.paginate = async () => {
      throw Object.assign(new Error("503 service unavailable"), { status: 503 });
    };
    await save(remote, 54, "success", { github: gh54 });
    expect(remoteState(remote)).toMatchObject({
      last_run: 54,
      reconciled_through: 54,
      unresolved_runs: [52],
      unresolved_run_ids: { "52": "9052" },
      unexplained: 1,
    });

    // 3. Once both lookups succeed, #52 is charged as unsaved and cleared from unresolved_runs.
    const run54 = { id: 9054, run_number: 54, status: "completed", conclusion: "success", head_branch: "main", event: "schedule", run_attempt: 1 };
    const gh55 = fakeGitHub(remote, {
      runs: [run54, run53, run52, run51, run50],
      attempts: { 9052: { status: "completed", conclusion: "failure" } },
      jobs: { 9052: [agent, writer("failure")] },
    });
    await save(remote, 55, "success", { github: gh55 });
    expect(remoteRuns(remote).map(({ number, note }) => ({ number, note }))).toEqual([
      { number: 50, note: null },
      { number: 51, note: "unsaved" },
      { number: 53, note: null },
      { number: 54, note: null },
      { number: 52, note: "unsaved" },
      { number: 55, note: null },
    ]);
    const finalState = remoteState(remote);
    expect(finalState).toMatchObject({ last_run: 55, reconciled_through: 55, unresolved_runs: [], unexplained: 2 });
    expect(finalState.unresolved_run_ids).toBeUndefined();
  });
});

describe("routing history under concurrent writers", () => {
  it("counts each of 100 concurrent runs exactly once when every writer re-applies to the newest files", () => {
    // Each writer reads the branch, applies its run and commits only if the branch has not moved; otherwise it re-reads and re-applies.
    let random = 7;
    const next = () => (random = (random * 1103515245 + 12345) % 2147483648) / 2147483648;
    let branch = { state: emptyState("triage"), runs: [] };
    const writers = Array.from({ length: 100 }, (_, index) => ({ number: index + 1, read: null }));
    const pending = new Set(writers);
    for (const writer of writers) writer.read = branch;
    while (pending.size > 0) {
      const ready = [...pending];
      const writer = ready[Math.floor(next() * ready.length)];
      const run = {
        workflow: "triage",
        run: { id: String(writer.number), number: writer.number, attempt: 1 },
        at: new Date(Date.UTC(2026, 10, 24, 10, 0, writer.number)).toISOString().replace(/\.\d{3}Z$/, "Z"),
        generation: "g1",
        mode: "observe",
        scope: "copilot/cost/shared",
        observation: { outcome: "success" },
        pair: PAIR,
        cost: { credits: 1, model_credits: 1, tokens: null },
      };
      const result = applyRun(writer.read.state, writer.read.runs, run);
      if (writer.read === branch) {
        branch = { state: result.state, runs: trimRuns([...branch.runs, ...result.lines]) };
        pending.delete(writer);
      } else {
        writer.read = branch;
      }
      // Other writers keep reading whatever head they last saw.
      if (next() < 0.3) for (const other of pending) other.read = branch;
    }
    expect(branch.runs.map(line => line.run).sort((a, b) => a - b)).toEqual(writers.map(writer => String(writer.number)));
    expect(branch.runs.every(line => line.counted)).toBe(true);
    expect(branch.state.seq).toBe(100);
    // The pair keeps its newest 50 outcomes.
    expect(branch.state.scopes["copilot/cost/shared"].pairs[PAIR].history.map(entry => entry[0])).toEqual(Array.from({ length: 50 }, (_, index) => index + 51));
  });
});
