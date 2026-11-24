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
const { ineligibility, main, readConfig, runCost, sessionEvidence } = req("./push_routing_history.cjs");

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

function fakeGitHub(remote, { runs = [], jobs = {}, attempts = {}, contents = {}, runsById = {}, listWorkflowRuns } = {}) {
  const calls = { jobs: [], attempts: [], lists: [], contents: [], runs: [] };
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
          const { branch, event, page = 1, per_page = 100 } = params;
          const filtered = runs.filter(run => (!branch || run.head_branch === branch) && (!event || run.event === event));
          const start = (page - 1) * per_page;
          return { data: { total_count: filtered.length, workflow_runs: filtered.slice(start, start + per_page) } };
        },
        getWorkflowRun: async ({ run_id }) => {
          calls.runs.push(run_id);
          const found = runsById[run_id] ?? runs.find(run => String(run.id) === String(run_id));
          if (!found) throw Object.assign(new Error("Not Found"), { status: 404 });
          return { data: found };
        },
        getWorkflowRunAttempt: async ({ run_id }) => {
          calls.attempts.push(run_id);
          return { data: attempts[run_id] };
        },
        listJobsForWorkflowRunAttempt: async ({ run_id }) => ({ data: { jobs: jobs[run_id] || [] } }),
      },
    },
    paginate: async (method, params) => {
      calls.jobs.push(params.run_id);
      return (await method(params)).data.jobs;
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
  const { github = fakeGitHub(remote), push = signedPush(remote), attempt = "1", ref = "refs/heads/main", sleepFn = async () => {}, session, generation = GENERATION } = options;
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

  it("does not look up runs from before the workflow opted in", async () => {
    const remote = bareRemote();
    const github = fakeGitHub(remote, { runs: [{ id: 9004, run_number: 4, status: "completed", conclusion: "failure", head_branch: "main", event: "schedule", run_attempt: 1 }] });
    await save(remote, 5, "success", { github });
    expect(github.calls.jobs).toEqual([]);
    expect(remoteRuns(remote).map(line => line.number)).toEqual([5]);
  });

  it("does not charge failed runs compiled without the history job, or with it disabled for a dry run", async () => {
    const remote = bareRemote();
    await save(remote, 50, "success");
    const run = number => ({ id: 9000 + number, run_number: number, status: "completed", conclusion: "failure", head_branch: "main", event: "schedule", run_attempt: 1 });
    const agent = { name: "agent", conclusion: "failure", started_at: "2026-11-24T09:00:00Z" };
    const github = fakeGitHub(remote, {
      runs: [run(52), run(51)],
      // Run 51 ran while the repository default was off; run 52 came from a dry-run compilation.
      jobs: { 9051: [agent], 9052: [agent, { name: "push_routing_history", conclusion: "skipped", started_at: null }] },
    });
    await save(remote, 53, "success", { github });
    expect(remoteRuns(remote).map(line => line.number)).toEqual([50, 53]);
    expect(remoteState(remote).unexplained).toBe(0);
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

  it("advances across a >800-run backlog in verified chunks without stalling and charges a late failed save", async () => {
    const remote = bareRemote();
    await save(remote, 1, "success");
    const agent = { name: "agent", conclusion: "failure", started_at: "2026-11-24T09:00:00Z" };
    const writer = conclusion => ({ name: "push_routing_history", conclusion, started_at: "2026-11-24T09:10:00Z" });
    // 1,005 runs (#1005 down to #1) with #1004 failed: exceeds MAX_LIST_PAGES=8 in one pass, so chunked backfill recovers in two saves.
    const runs = Array.from({ length: 1005 }, (_, i) => {
      const number = 1005 - i;
      return {
        id: 19000 + number,
        run_number: number,
        status: "completed",
        conclusion: number === 1004 ? "failure" : "success",
        head_branch: "main",
        event: "schedule",
        run_attempt: 1,
      };
    });
    const github = fakeGitHub(remote, { runs, jobs: { 20004: [agent, writer("failure")] } });

    await save(remote, 1005, "success", { github });
    const mid = remoteState(remote);
    expect(mid.reconciled_through).toBeGreaterThan(1);
    expect(mid.reconciled_through).toBeLessThan(1004);
    expect(mid.unexplained).toBe(0);

    runs.unshift({ id: 20006, run_number: 1006, status: "completed", conclusion: "success", head_branch: "main", event: "schedule", run_attempt: 1 });
    await save(remote, 1006, "success", { github });
    expect(remoteState(remote)).toMatchObject({ last_run: 1006, reconciled_through: 1006, unresolved_runs: [], unexplained: 1 });
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

  it("does not advance coverage when an older gap below the unfiltered first page remains missing", async () => {
    const remote = bareRemote();
    await save(remote, 100, "success");
    // #100 and #103..#205 exist (#101 is PR, #102 is omitted from all pages): #205 must not skip #102.
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
    expect(remoteState(remote)).toMatchObject({ last_run: 205, reconciled_through: 100, unresolved_runs: [] });
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

  it("holds reconciled_through across an uncorroborated gap and retains transiently absent unresolved runs", async () => {
    const remote = bareRemote();
    await save(remote, 100, "success");
    const agent = { name: "agent", conclusion: "failure", started_at: "2026-11-24T09:00:00Z" };
    const writer = conclusion => ({ name: "push_routing_history", conclusion, started_at: "2026-11-24T09:10:00Z" });
    const run100 = { id: 9100, run_number: 100, status: "completed", conclusion: "success", head_branch: "main", event: "schedule", run_attempt: 1 };
    const run102 = { id: 9102, run_number: 102, status: "completed", conclusion: "success", head_branch: "main", event: "schedule", run_attempt: 1 };

    // 1. Listing omits #101 between #100 and #102: do not advance reconciled_through past 100.
    await save(remote, 102, "success", { github: fakeGitHub(remote, { runs: [run102, run100] }) });
    expect(remoteState(remote)).toMatchObject({ last_run: 102, reconciled_through: 100, unresolved_runs: [], unexplained: 0 });

    // 2. When #101 reappears as a failed save on #103 (with #104 still active? No, #103 sees #101), #101 is charged.
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
