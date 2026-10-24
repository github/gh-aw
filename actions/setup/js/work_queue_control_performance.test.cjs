// @ts-check
import { afterEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import path from "node:path";
import { performance } from "node:perf_hooks";
import { createRequire } from "node:module";
import { ESLINT_WORKERS, buildESLintFactoryPlan } from "./eslint_factory_portfolio.cjs";
import { compiledFactoryPolicyEnvironments, installFactoryNativeAPI } from "./eslint_factory_native.test_helpers.cjs";
import { canonical } from "./work_queue_codec.cjs";
import { stagePlanIntents } from "./work_queue_intents.cjs";
import { prepareCheckpoint, replayTransactions } from "./work_queue_replay.cjs";
import { fakeGitHub } from "./work_queue_store_checks.cjs";
import { queueFixture, DISPATCHER, REF, REPOSITORY } from "./work_queue_lifecycle.test_helpers.cjs";
import { main as processControls } from "./work_queue_control_adapter.cjs";
import { readWorkQueuePolicyConfig } from "./work_queue_policy_config.cjs";

const require = createRequire(import.meta.url);
const { setupGlobals } = require("./setup_globals.cjs");
const { queueAuthenticationScope } = require("./work_queue_auth_cache.cjs");
const { registerQueueLedgerClient } = require("./work_queue_git_transport.cjs");
const GLOBAL_NAMES = ["core", "github", "context", "exec", "io", "getOctokit", "runtimeFeatures", "hasRuntimeFeature", "getRuntimeFeatureValue"];
const savedGlobals = new Map(GLOBAL_NAMES.map(name => [name, Object.getOwnPropertyDescriptor(global, name)]));
const directories = [];
const compiled = fs.readFileSync(new URL("../../../.github/workflows/eslint-factory-dispatcher.lock.yml", import.meta.url), "utf8");
const configMatch = compiled.match(/GH_AW_WORK_QUEUE_CONTROL_CONFIG: ("[^\n]+")/);
if (!configMatch) throw new Error("Compiled ESLint factory control configuration is missing");
const config = { ...JSON.parse(JSON.parse(configMatch[1])), "github-token": "fixture-token" };
const [policyEnvironment] = compiledFactoryPolicyEnvironments(compiled);
if (!policyEnvironment) throw new Error("Compiled ESLint factory Policy is missing");
const compiledPolicy = readWorkQueuePolicyConfig(policyEnvironment).replaceAll("${{ github.repository }}", REPOSITORY).replaceAll("${{ github.sha }}", REF);

afterEach(() => {
  vi.unstubAllEnvs();
  vi.unstubAllGlobals();
  for (const name of GLOBAL_NAMES) {
    const descriptor = savedGlobals.get(name);
    if (descriptor) Object.defineProperty(global, name, descriptor);
    else delete global[name];
  }
  for (const directory of directories.splice(0)) fs.rmSync(directory, { recursive: true, force: true });
});

async function fixture({ depth = 32, profiles = 3, works = 15, retained = 100, auth = "constructed", deploymentsChanged = false, transport = "graphql", publisherToken = false } = {}) {
  const root = fs.mkdtempSync(path.join(process.cwd(), ".gh-aw-control-performance-"));
  directories.push(root);
  const workers = ESLINT_WORKERS.slice(0, profiles);
  const plan = buildESLintFactoryPlan({ date: "2026-10-08", repository: REPOSITORY, repositoryId: "7" });
  plan.nodes = plan.nodes.filter(node => workers.includes(node.worker_profile));
  plan.dispatch.max_claims = profiles;
  plan.dispatch.max_dispatches = profiles;
  const policy = JSON.parse(compiledPolicy);
  policy.pools.default.profiles = Object.fromEntries(workers.map(worker => [worker, policy.pools.default.profiles[worker]]));
  policy.pools.default.default_profile = workers[0];
  const queue = queueFixture({
    count: works,
    granted: false,
    workerPrincipal: "12",
    configurePolicy: value => Object.assign(value, policy),
    configureWork: (work, index) => {
      const node = plan.nodes[index % profiles];
      work.worker_profile = node.worker_profile;
      work.payload = { ...node.payload, historical_context: "Original ESLint factory task context. ".repeat(350) };
    },
  });
  expect(queue.state.deployments.get("default").size).toBe(profiles);
  for (let index = 0; index < retained; index++) {
    queue.append("control", { operations: [{ kind: "Control", control: "admission_paused", value: index % 2 === 0, reason: "performance_fixture" }] }, queue.administrator);
  }
  const fake = fakeGitHub(queue.transactions, { immutableIds: true });
  const native = installFactoryNativeAPI(fake, queue);
  const git = fake.githubClient.rest.git;
  const original = fake.refs.get("work-queue");
  const prior = "c".repeat(40);
  const getCommit = git.getCommit;
  git.getCommit = parameters => getCommit({ ...parameters, commit_sha: parameters.commit_sha === prior ? original : parameters.commit_sha });
  const checkpoint = canonical(prepareCheckpoint(queue.transactions, prior, queue.administrator, queue.at)[0]) + "\n";
  expect(Buffer.byteLength(checkpoint)).toBeGreaterThanOrEqual(200 * 1024);
  const blob = await git.createBlob({ content: checkpoint, encoding: "utf-8" });
  const tree = await git.createTree({ tree: [{ path: "work-queue.jsonl", mode: "100644", type: "blob", sha: blob.data.sha }] });
  const history = new Set([prior]);
  fake.refs.set("work-queue", prior);
  for (let index = 0; index < depth; index++) {
    const commit = await git.createCommit({ tree: tree.data.sha, parents: [fake.refs.get("work-queue")] });
    fake.refs.set("work-queue", commit.data.sha);
    history.add(commit.data.sha);
  }
  if (deploymentsChanged) fake.state.defaultRevision = "b".repeat(40);

  // Model one atomic GraphQL publication, not four REST requests hidden inside
  // the measurement. Both transports still execute the real checked publisher.
  const rawGit = { ...git };
  fake.githubClient.graphql = async (query, variables) => {
    expect(query).toMatch(/^mutation CheckedWorkQueuePublication/);
    const input = variables.input;
    expect(input.branch).toEqual({ repositoryNameWithOwner: REPOSITORY, branchName: "work-queue" });
    expect(input.expectedHeadOid).toBe(fake.refs.get("work-queue"));
    expect(input.fileChanges.additions).toHaveLength(1);
    const file = input.fileChanges.additions[0];
    expect(file.path).toBe("work-queue.jsonl");
    const blob = await rawGit.createBlob({ content: Buffer.from(file.contents, "base64").toString("utf8"), encoding: "utf-8" });
    const tree = await rawGit.createTree({ tree: [{ path: file.path, mode: "100644", type: "blob", sha: blob.data.sha }] });
    const commit = await rawGit.createCommit({ tree: tree.data.sha, parents: [input.expectedHeadOid] });
    await rawGit.updateRef({ ref: "heads/work-queue", sha: commit.data.sha, force: false });
    return { createCommitOnBranch: { commit: { oid: commit.data.sha } } };
  };
  const calls = [];
  let sleepMs = 0;
  for (const [namespace, client] of Object.entries(fake.githubClient.rest)) {
    for (const [method, implementation] of Object.entries(client)) {
      client[method] = parameters => {
        calls.push({ method: `${namespace}.${method}`, write: /^(?:create|update|delete)/.test(method), history: namespace === "git" && method === "getCommit" && history.has(parameters.commit_sha) });
        return implementation(parameters);
      };
    }
  }
  const graphql = fake.githubClient.graphql;
  fake.githubClient.graphql = (query, variables) => {
    calls.push({ method: "graphql.publication", write: true, history: false });
    return graphql(query, variables);
  };
  const failures = [];
  const core = { setOutput: () => {}, info: () => {}, setFailed: message => failures.push(message) };
  const createClient = token => {
    const client = {
      rest: fake.githubClient.rest,
      graphql: fake.githubClient.graphql,
      auth: async () => ({ type: "token", token }),
      hook: { before: () => {} },
    };
    registerQueueLedgerClient(client, client);
    return client;
  };
  const injected = createClient("fixture-token");
  setupGlobals(core, injected, queue.dispatcherContext, {}, {}, createClient, "github_actions");
  const getOctokit = global.getOctokit;
  global.getOctokit = (...args) => {
    const client = getOctokit(...args);
    registerQueueLedgerClient(client, client);
    return client;
  };
  expect(await queueAuthenticationScope(injected)).toBeUndefined();
  const client = auth === "injected" ? injected : global.getOctokit("fixture-token", auth === "dynamic" ? { authStrategy: () => {} } : {});
  expect(Boolean(await queueAuthenticationScope(client))).toBe(auth === "constructed");
  const intentPath = path.join(root, "intents.jsonl");
  stagePlanIntents(intentPath, plan);
  vi.stubEnv("GH_AW_WORK_QUEUE_CHECKED_TRANSPORT", transport);
  vi.stubGlobal("fetch", () => {
    throw new Error("Performance tests must not access the network");
  });
  const options = {
    github: client,
    context: queue.dispatcherContext,
    workflowRef: `${REPOSITORY}/${DISPATCHER}@${REF}`,
    intentOrigin: queue.dispatcher,
    role: "dispatcher",
    policyProposal: policy,
    core,
    env: publisherToken ? { GH_AW_WORK_QUEUE_TOKEN: "publisher-token", GH_AW_GITHUB_TOKEN_SOURCE: "github_actions" } : {},
    intentPath,
    config: { ...config, work_queue_workflows: workers, aw_context_workflows: workers },
    sleepFn: async milliseconds => {
      sleepMs += milliseconds;
    },
  };
  const run = async phase => {
    calls.length = 0;
    sleepMs = 0;
    const start = performance.now();
    const result = await processControls(options);
    const cpuMs = performance.now() - start;
    expect(result.success, JSON.stringify({ result, failures })).toBe(true);
    expect(result.receipts.map(receipt => receipt.status)).toEqual(["durable", "durable"]);
    expect(failures).toEqual([]);
    const writes = calls.filter(call => call.write).length;
    const metrics = {
      benchmark: "trusted-work-queue-controls",
      phase,
      success: result.success,
      auth,
      publisher_token: publisherToken,
      transport,
      profiles,
      works,
      retained_requests: retained,
      checkpoint_bytes: Buffer.byteLength(checkpoint),
      ancestry_commits: depth,
      api_calls: calls.length,
      reads: calls.length - writes,
      writes,
      history_reads: calls.filter(call => call.history).length,
      // Each ancestry commit is loaded and its parent proof checked once.
      within_single_traversal_budget: calls.filter(call => call.history).length <= 2 * depth + 1,
      // Serial cost model only; this is not a measured GitHub wall-clock time.
      simulated_network_ms: (calls.length - writes) * 250 + writes * 500 + sleepMs,
      cpu_ms: Math.round(cpuMs),
      methods: Object.fromEntries([...new Set(calls.map(call => call.method))].sort().map(method => [method, calls.filter(call => call.method === method).length])),
    };
    console.info(JSON.stringify(metrics));
    return metrics;
  };
  return { fake, native, run, works, profiles, depth, workers };
}

function assertBoundClaims(fixture) {
  const state = replayTransactions(fixture.fake.log());
  expect(state.works.size).toBe(fixture.works + fixture.profiles);
  expect(state.claims.size).toBe(fixture.profiles);
  expect([...state.works.values()].filter(work => work.state === "claimed")).toHaveLength(fixture.profiles);
  expect(state.dispatches.size).toBe(fixture.profiles);
  expect(fixture.native.posts).toHaveLength(fixture.profiles);
  expect(fixture.native.tagCreations).toHaveLength(1);
  expect([...state.dispatches.values()].every(dispatch => dispatch.state === "bound")).toBe(true);
  expect(new Set([...state.dispatches.values()].map(dispatch => dispatch.run.run_id))).toEqual(new Set(fixture.native.runs.keys()));
}

describe("trusted work queue control step performance", () => {
  it.each([
    { profiles: 1, depth: 8 },
    { profiles: 3, depth: 8 },
    { profiles: 3, depth: 32 },
    { profiles: 3, depth: 64 },
    { profiles: 3, depth: 8, works: 27, retained: 100 },
  ])(
    "bounds cold history verification and full-batch idempotent recovery: %j",
    async parameters => {
      const scenario = await fixture(parameters);
      const cold = await scenario.run("cold");
      assertBoundClaims(scenario);
      expect(cold.history_reads).toBe(2 * scenario.depth + 1);
      expect(cold.api_calls).toBeLessThanOrEqual(4 * scenario.depth + 40 * scenario.profiles + 48);
      const head = scenario.fake.refs.get("work-queue");
      const warm = await scenario.run("idempotent");
      assertBoundClaims(scenario);
      expect(scenario.fake.refs.get("work-queue")).toBe(head);
      expect(warm.history_reads).toBe(0);
      expect(warm.writes).toBe(0);
      expect(warm.api_calls).toBeLessThanOrEqual(20 + 8 * scenario.profiles);
      expect(warm.simulated_network_ms).toBeLessThanOrEqual((20 + 8 * scenario.profiles) * 250);
    },
    30000
  );

  it.each(["graphql", "rest"])(
    "refreshes three worker deployments without multiplying history traversals (%s)",
    async transport => {
      const scenario = await fixture({ deploymentsChanged: true, transport });
      const metrics = await scenario.run("deployment-refresh");
      assertBoundClaims(scenario);
      const deployments = scenario.fake
        .log()
        .flatMap(commit => commit.operations)
        .filter(operation => operation.kind === "Deployment");
      expect(deployments).toHaveLength(3);
      expect(deployments.every(deployment => deployment.profile.ref === "b".repeat(40))).toBe(true);
      expect([...scenario.native.runs.values()].every(run => run.head_sha === "b".repeat(40))).toBe(true);
      expect(metrics.history_reads).toBe(2 * scenario.depth + 1);
      expect(metrics.api_calls).toBeLessThanOrEqual(4 * scenario.depth + 60 * scenario.profiles + 64);
    },
    30000
  );

  it.each(["graphql", "rest"])(
    "constructs a cache-eligible publisher client for compiled injected-client controls (%s)",
    async transport => {
      const scenario = await fixture({ auth: "injected", publisherToken: true, deploymentsChanged: true, transport });
      const metrics = await scenario.run("compiled-publisher-token");
      assertBoundClaims(scenario);
      expect(metrics.history_reads).toBe(2 * scenario.depth + 1);
      expect(metrics.api_calls).toBeLessThanOrEqual(4 * scenario.depth + 60 * scenario.profiles + 64);
      const head = scenario.fake.refs.get("work-queue");
      const retry = await scenario.run("compiled-publisher-retry");
      assertBoundClaims(scenario);
      expect(scenario.fake.refs.get("work-queue")).toBe(head);
      expect(retry.writes).toBe(0);
      expect(retry.history_reads).toBeLessThanOrEqual(2 * (scenario.depth + 1) + 8 * scenario.profiles);
    },
    30000
  );

  it.each(["injected", "dynamic"])(
    "detects uncached-history amplification for the %s client at the real compiler adapter boundary",
    async auth => {
      const scenario = await fixture({ auth, deploymentsChanged: true, depth: 8 });
      const metrics = await scenario.run("uncached-auth-negative-control");
      assertBoundClaims(scenario);
      // Negative controls must fail the same one-traversal budget as the cached
      // cases when no explicit publisher token is supplied.
      expect(metrics.within_single_traversal_budget).toBe(false);
      expect(metrics.history_reads).toBeGreaterThan(5 * (scenario.depth + 1));
      expect(metrics.simulated_network_ms).toBeGreaterThan(120000);
    },
    // Uncached negative controls deliberately replay thousands of API reads.
    120000
  );
});
