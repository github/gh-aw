"use strict";

const fs = require("node:fs");
const path = require("node:path");
const os = require("node:os");

function resetRuntime() {
  for (const filename of Object.keys(require.cache)) if (filename.startsWith(__dirname + path.sep)) delete require.cache[filename];
}

function completedClaimFixture(options = {}) {
  const { queueFixture } = require("./work_queue_lifecycle.test_helpers.cjs");
  const fixture = queueFixture({ count: 1, ...options, bound: true });
  if (!fixture.assignment) throw new Error("restart fixture requires a bound worker assignment");
  for (const member of fixture.assignment.claims)
    fixture.append("finish", { dispatch_id: fixture.assignment.dispatch_id, claim_handle: member.handle, outcome: "completed" }, { ...fixture.workerActor, dispatch_id: fixture.assignment.dispatch_id, claim_handle: member.handle });
  return fixture;
}

function createHarness(options = {}) {
  resetRuntime();
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "wq-delivery-evidence-"));
  const oldEnv = { ...process.env };
  const oldGlobals = Object.fromEntries(["core", "context", "github", "getOctokit", "exec"].map(key => [key, Object.getOwnPropertyDescriptor(global, key)]));
  const oldExitListeners = new Set(process.listeners("exit"));
  for (const key of Object.keys(process.env)) if (key.startsWith("GH_AW_")) delete process.env[key];
  const type = options.type ?? "create_issue";
  const contract = options.contract ?? { version: 1, outputs: [{ type, min: 1, max: 1 }] };
  const fixture = completedClaimFixture({
    count: options.count ?? 1,
    workDefaults: { payload: { effect_contract: contract, resource_scope: { version: 1, resources: [{ host: "github.com", repository: "owner/repo", repository_id: "7" }] } } },
  });
  let transactions = fixture.transactions;
  const clock = { now: fixture.at + 1 };
  /** @type {{unavailable: boolean, terminal: boolean, tampered: boolean, writes: number, native: Record<string, unknown> | null}} */
  const state = { unavailable: true, terminal: false, tampered: false, writes: 0, native: null };
  /** @type {{info: string[], warning: string[], error: string[], failed: string[], output: unknown[][]}} */
  const logs = { info: [], warning: [], error: [], failed: [], output: [] };
  const core = {
    info: value => logs.info.push(value),
    warning: value => logs.warning.push(value),
    error: value => logs.error.push(value),
    debug() {},
    notice() {},
    startGroup() {},
    endGroup() {},
    setFailed: value => logs.failed.push(value),
    setOutput: (...value) => logs.output.push(value),
    summary: {
      addRaw() {
        return this;
      },
      async write() {},
    },
  };
  const snapshotPath = path.join(root, "snapshot.json");
  const agentOutputPath = path.join(root, "agent-output.json");
  Object.assign(process.env, {
    RUNNER_TEMP: root,
    GITHUB_WORKSPACE: root,
    GITHUB_REPOSITORY: "owner/repo",
    GITHUB_REPOSITORY_ID: "7",
    GITHUB_RUN_ID: "42",
    GITHUB_RUN_ATTEMPT: "1",
    GITHUB_JOB: "safe_outputs",
    GITHUB_WORKFLOW_REF: `owner/repo/.github/workflows/worker.lock.yml@${"a".repeat(40)}`,
    GH_AW_JOB_CHECK_RUN_ID: "789",
    GH_AW_WORKFLOW_ID: "evidence-test",
    GH_AW_WORK_QUEUE_ENABLED: "true",
    GH_AW_WORK_QUEUE_ROLE: "worker",
    GH_AW_WORK_QUEUE_SNAPSHOT: snapshotPath,
    GH_AW_AGENT_OUTPUT: agentOutputPath,
    GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG: JSON.stringify(options.config ?? { [type]: { max: 1, footer: false } }),
  });
  const github = fixture.githubClient;
  github.rest.rateLimit = { get: async () => ({ data: { resources: { core: { remaining: 5000, limit: 5000 } } } }) };
  github.rest.actions.getWorkflowRun = async ({ run_id }) => ({
    status: 200,
    data: { ...fixture.nativeRun(String(run_id) === "42" ? fixture.workerContext : fixture.dispatcherContext), ...(String(run_id) === "42" && state.terminal ? { status: "completed", conclusion: "success" } : {}) },
  });
  github.rest.issues = {
    create: async parameters => {
      state.writes++;
      state.native = { ...parameters, id: 190, number: 19, html_url: "https://github.com/owner/repo/issues/19", labels: [], assignees: [], state: "open" };
      return { data: state.native };
    },
    get: async ({ issue_number }) => {
      if (state.unavailable) throw Object.assign(new Error("injected readback unavailable"), { status: 503 });
      if (issue_number !== 19) throw Object.assign(new Error("not found"), { status: 404 });
      return { data: { ...state.native, ...(state.tampered ? { title: "changed after publication" } : {}) } };
    },
  };
  Object.assign(global, { core, context: fixture.workerContext, github, getOctokit: () => github });
  const constants = require("./constants.cjs");
  for (const [key, value] of Object.entries(constants)) if (typeof value === "string" && value.startsWith("/tmp/gh-aw/")) constants[key] = path.join(root, value.slice("/tmp/gh-aw/".length));
  const redact = require("./redact_secrets.cjs");
  redact.MCP_GATEWAY_CONFIG_PATHS.splice(0, redact.MCP_GATEWAY_CONFIG_PATHS.length, path.join(root, "missing-gateway.json"));
  const readWorkQueueLog = async () => ({ sha: "e".repeat(40), transactions, state: require("./work_queue_replay.cjs").replayTransactions(transactions) });
  const publishWorkQueueRequest = async args => {
    const queue = require("./work_queue_replay.cjs");
    const { actorFromContext, validateTrustedContext } = require("./work_queue_policy.cjs");
    const actor = args.actor || actorFromContext(args.context);
    validateTrustedContext(args.context, actor);
    const projection = queue.replayTransactions(transactions);
    const prior = projection.requests.get(args.request.id);
    if (prior) return { state: projection, transactions, commit: prior, persisted: false, recovered: true, idempotent: true };
    const id = `evidence-test-${transactions.length}`;
    const at = clock.now++;
    const decision = (args.generateOperations || queue.generateRequestOperations)(projection, args.request, actor, at, id, []);
    if (!decision.operations.length) return { ...decision, state: projection, transactions, persisted: false };
    const commit = { version: 3, id, previous: projection.tip, request: args.request, actor, policy_epoch: projection.policy_epoch, at, operations: decision.operations };
    const result = queue.appendCommit(transactions, commit);
    transactions = result.transactions;
    return { ...result, ...decision, persisted: true, publishedNow: true };
  };
  const hookStore = () => Object.assign(require("./work_queue_store.cjs"), { readWorkQueueLog, publishWorkQueueRequest });
  hookStore();
  const runtime = () => ({
    assignment: fixture.assignment,
    context: fixture.workerContext,
    githubClient: github,
    core,
    workflowRef: `owner/repo/.github/workflows/worker.lock.yml@${"a".repeat(40)}`,
    readWorkQueueLog,
    publishWorkQueueRequest,
    sleepFn: async () => {},
  });
  const snapshot = () =>
    fs.writeFileSync(
      snapshotPath,
      JSON.stringify({
        version: 3,
        sha: "e".repeat(40),
        transactionLog: require("./work_queue_replay.cjs").serializeTransactionLog(transactions),
        captured_at: clock.now,
        origin: fixture.workerActor,
        worker: fixture.assignment,
        role: "worker",
      })
    );
  snapshot();
  const messages = options.messages ?? [{ type, title: "Required report", body: "Report body" }];
  fs.writeFileSync(agentOutputPath, JSON.stringify({ items: messages }));
  const harness = {
    root,
    fixture,
    github,
    core,
    logs,
    state,
    clock,
    runtime,
    readWorkQueueLog,
    publishWorkQueueRequest,
    snapshot,
    get transactions() {
      return transactions;
    },
    get projection() {
      return require("./work_queue_replay.cjs").replayTransactions(transactions);
    },
    operations(kind) {
      return transactions.flatMap(commit => commit.operations).filter(operation => operation.kind === kind);
    },
    settlement(member) {
      const projection = this.projection;
      return {
        barrier: projection.works.get(member.work_id).barrier,
        evidenceRetained: projection.deliveryEvidence.has(member.claim_id),
        results: this.operations("Result").length,
        failures: this.operations("DeliveryFailure").length,
        writes: state.writes,
      };
    },
    async workerPass(extra = {}) {
      const { authorizeWorkerClaim, finalizeWorkerResults } = require("./finish_work_queue_claim.cjs");
      const { readDeliveryControlInventory } = require("./work_queue_delivery.cjs");
      const claimOptions = {
        queueClient: github,
        controlIntentPath: path.join(root, "control-intents.jsonl"),
        deliveryArtifactRoot: path.join(root, "delivery"),
        authorize: request => authorizeWorkerClaim({ ...runtime(), ...request }),
        readControlInventory: () => readDeliveryControlInventory({ ...runtime(), claim_handle: "h1" }),
        finalizeResults: input => finalizeWorkerResults({ ...input, ...runtime(), now: clock.now }),
        ...extra,
      };
      return require("./safe_output_handler_manager.cjs").main(claimOptions);
    },
    async maintenance(extra = {}) {
      if (extra.now !== undefined) clock.now = extra.now;
      const { assignment: _assignment, ...configured } = runtime();
      return require("./reconcile_work_queue_run.cjs").maintenance({ ...configured, context: fixture.dispatcherContext, workflowRef: `owner/repo/.github/workflows/dispatcher.lock.yml@${"a".repeat(40)}`, now: clock.now, ...extra });
    },
    restartForMaintenance({ tampered = false } = {}) {
      Object.assign(state, { unavailable: false, terminal: true, tampered });
      resetRuntime();
      hookStore();
    },
    dispose() {
      for (const key of Object.keys(process.env)) if (!Object.hasOwn(oldEnv, key)) delete process.env[key];
      Object.assign(process.env, oldEnv);
      for (const [key, descriptor] of Object.entries(oldGlobals)) {
        if (descriptor) Object.defineProperty(global, key, descriptor);
        else delete global[key];
      }
      for (const listener of process.listeners("exit"))
        if (!oldExitListeners.has(listener)) {
          process.removeListener("exit", listener);
          listener(0);
        }
      fs.rmSync(root, { recursive: true, force: true });
      resetRuntime();
    },
  };
  options.setupNative?.(harness);
  return harness;
}

module.exports = { createHarness, resetRuntime, completedClaimFixture };
