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
const { ineligibility, main, readConfig, sessionEvidence } = req("./push_routing_history.cjs");

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

function fakeGitHub(remote, { runs = [], jobs = {}, attempts = {} } = {}) {
  const calls = { jobs: [], attempts: [] };
  const client = {
    calls,
    rest: {
      repos: { get: async () => ({ data: { default_branch: "main" } }) },
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
        listWorkflowRuns: async () => ({ data: { workflow_runs: runs } }),
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
  const { github = fakeGitHub(remote), push = signedPush(remote), attempt = "1", ref = "refs/heads/main", sleepFn = async () => {}, session } = options;
  const workspaceDir = fs.mkdtempSync(path.join(root, `run-${number}-`));
  return main({
    github,
    context: { repo: { owner: "octo", repo: "demo" }, runId: 9000 + number, runNumber: number, payload: { repository: { default_branch: "main" } } },
    env: {
      GH_AW_ROUTING_WORKFLOW: "triage",
      GH_AW_ROUTING_GENERATION: GENERATION,
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

  it("counts input without cache as AWF's meter does, inferring whether input includes cache when the record does not say", () => {
    const firewall = (type, data) => ({ type, data, provenance: { component: "firewall", phase: "agent" } });
    const tokens = (...usages) =>
      sessionEvidence([
        firewall("firewall.model_routing", { stage: "selection", selectedProvider: "copilot", wireModel: "model-a", selectedEffort: "high" }),
        ...usages.flatMap((usage, index) => [
          firewall("firewall.model_routing", { stage: "request", requestId: `r${index}`, routed: "as_selected", requestedModel: "model-a" }),
          firewall("firewall.token_usage", { requestId: `r${index}`, provider: "copilot", aic: 1, usage }),
        ]),
      ]).cost.tokens;
    const uncached = { inputTokens: 23197, cacheReadInputTokens: 0, cacheCreationInputTokens: 0, outputTokens: 400 };
    // Input that covers the cache can include it; input smaller than the cache cannot.
    const covering = { inputTokens: 23799, cacheReadInputTokens: 23194, cacheCreationInputTokens: 0, outputTokens: 45 };
    const smaller = { inputTokens: 382, cacheReadInputTokens: 19712, cacheCreationInputTokens: 0, outputTokens: 69 };
    expect(tokens(uncached, covering, smaller)).toEqual([24184, 42906, 0, 514, null]);
    // AWF's flag wins over the inference.
    expect(tokens({ ...covering, inputTokensIncludeCache: false })).toEqual([23799, 23194, 0, 45, null]);
    expect(tokens({ ...smaller, inputTokensIncludeCache: true })).toEqual([0, 19712, 0, 69, null]);
  });

  it("has no verdict for a run AWF did not route, and no pair to count a verdict for without a selection", () => {
    expect(sessionEvidence([]).observation).toBeNull();
    const verdictOnly = [{ type: "routing.observation", data: { outcome: "success" } }];
    expect(sessionEvidence(verdictOnly)).toMatchObject({ observation: { outcome: "skip", reason: "unknown_evidence" }, pair: null });
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
        9055: [{ name: "agent", conclusion: "failure", started_at: "2026-11-24T09:00:00Z" }],
        9052: [{ name: "agent", conclusion: "skipped", started_at: null }],
        9051: [{ name: "agent", conclusion: "cancelled", started_at: "2026-11-24T09:00:00Z" }],
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
