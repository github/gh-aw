// @ts-check
"use strict";

const { actorFromContext, defaultPolicy, validateTrustedContext } = require("./work_queue_policy.cjs");
const { appendCommit, generateRequestOperations, newRequest, newState, replayTransactions } = require("./work_queue_replay.cjs");
const { assignmentOnly } = require("./work_queue_scheduler.cjs");
const { nodeId } = require("./work_queue_graph.cjs");

const REF = "a".repeat(40);
const REPOSITORY = "owner/repo";
const WORKFLOW = ".github/workflows/worker.lock.yml";
const DISPATCHER = ".github/workflows/dispatcher.lock.yml";

function queueFixture(options = {}) {
  let transactions = [];
  let sequence = 0;
  let at = 1000;
  const principal = "11";
  const workerPrincipal = options.workerPrincipal || principal;
  const policy = defaultPolicy({ repository: REPOSITORY, principal, workflow: WORKFLOW, ref: REF });
  policy.pools.default.profiles.default.principal = workerPrincipal;
  policy.pools.default.profiles.default.max_claims = options.batch ?? 16;
  policy.pools.default.retry.backoff_ms = 10;
  if (options.configurePolicy) options.configurePolicy(policy);
  const administrator = { role: "administrator", principal, repository: REPOSITORY };
  const dispatcher = { role: "dispatcher", principal, repository: REPOSITORY, workflow: DISPATCHER, run_id: "15", run_attempt: options.dispatcherRunAttempt || 1 };
  const workerActor = { role: "worker", principal: workerPrincipal, repository: REPOSITORY, workflow: WORKFLOW, run_id: "42", run_attempt: 1 };
  function append(kind, parameters, actor = administrator, requestId = `test-request-${sequence + 1}`, now = ++at) {
    const state = transactions.length ? replayTransactions(transactions) : newState();
    const id = `test-commit-${++sequence}`;
    const request = newRequest(requestId, kind, actor, parameters);
    const decision = generateRequestOperations(state, request, actor, now, id);
    if (!decision.operations.length) return { transactions, state, ...decision, persisted: false, publishedNow: false };
    const commit = { version: 3, id, previous: state.tip || null, request, actor, policy_epoch: kind === "policy" ? parameters.operations[0].epoch : state.policy_epoch, at: now, operations: decision.operations };
    const result = transactions.length ? appendCommit(transactions, commit) : { transactions: [commit], state: replayTransactions([commit]), commit, idempotent: false };
    transactions = result.transactions;
    return { ...result, ...decision, persisted: !result.idempotent, recovered: result.idempotent, publishedNow: !result.idempotent, reused: result.idempotent };
  }
  append("policy", { operations: [{ kind: "Policy", epoch: "e1", policy }] });
  append(
    "submit",
    {
      nodes: Array.from({ length: options.count ?? 3 }, (_, index) => ({
        kind: "Work",
        work_id: nodeId("g1", `n${index + 1}`),
        graph_id: "g1",
        node_key: `n${index + 1}`,
        pool: "default",
        priority: 3,
        fairness_key: "",
        worker_profile: "default",
        batch_trust_domain: "default",
        payload: { plan: `stored task ${index + 1}`, effect_contract: { kind: "none" } },
        depends_on: [],
        enqueued: 1000,
        ...(options.workDefaults || {}),
      })),
    },
    { role: "producer", principal, repository: REPOSITORY }
  );
  if (options.granted !== false) append("dispatch_next", { pool: "default", max_claims: options.count ?? 3, max_dispatches: 16, max_bytes: policy.limits.assignment_bytes }, dispatcher);
  const assignment = options.granted === false ? null : assignmentOnly([...replayTransactions(transactions).dispatches.values()][0]);
  const binding = { run_id: "42", run_attempt: 1, repository: REPOSITORY, workflow: WORKFLOW, ref: REF, principal: workerPrincipal, event: "workflow_dispatch" };
  if (options.started || options.bound) append("dispatch", { operations: [{ kind: "Dispatch", dispatch_id: assignment.dispatch_id, state: "started", sender: dispatcher }] }, dispatcher);
  if (options.bound)
    append(
      "dispatch",
      {
        operations: [
          {
            kind: "Dispatch",
            dispatch_id: assignment.dispatch_id,
            state: "bound",
            run: binding,
            evidence: { kind: "reconciliation", source: "github_api", repository: REPOSITORY, workflow: WORKFLOW, ref: REF, principal: workerPrincipal, checked_at: at, run_id: "42", run_attempt: 1 },
          },
        ],
      },
      dispatcher
    );
  const workerContext = {
    repo: { owner: "owner", repo: "repo" },
    runId: "42",
    runAttempt: 1,
    actorId: workerPrincipal,
    sha: REF,
    eventName: "workflow_dispatch",
    payload: { repository: { id: 7 }, ...(assignment ? { inputs: { work_queue_assignment: JSON.stringify(assignment) } } : {}) },
  };
  const dispatcherContext = { ...workerContext, runId: "15", runAttempt: dispatcher.run_attempt, actorId: principal, eventName: "schedule", payload: { repository: { id: 7 } } };
  const nativeRun = (context = workerContext) => ({
    id: context.runId,
    run_attempt: context.runAttempt,
    repository: { full_name: REPOSITORY, id: 7 },
    path: context.runId === "42" ? WORKFLOW : DISPATCHER,
    event: context.eventName,
    head_sha: REF,
    actor: { id: context.actorId },
    triggering_actor: { id: context.actorId },
    display_title: assignment ? `gh-aw work-queue ${assignment.dispatch_id}` : "dispatcher",
    status: "in_progress",
    conclusion: null,
    created_at: "2026-10-05T00:00:00Z",
  });
  const githubClient = { rest: { actions: { getWorkflowRun: async ({ run_id }) => ({ status: 200, data: nativeRun(run_id === "42" ? workerContext : dispatcherContext) }) } } };
  githubClient.rest.actions.getWorkflowRunAttempt = async parameters => {
    const response = await githubClient.rest.actions.getWorkflowRun(parameters);
    return { ...response, data: { ...response.data, run_attempt: parameters.attempt_number } };
  };
  async function readWorkQueueLog() {
    return { sha: `head-${sequence}`, transactions, state: replayTransactions(transactions) };
  }
  async function publishWorkQueueRequest(args) {
    const actor = args.actor || actorFromContext(args.context);
    validateTrustedContext(args.context, actor);
    const state = replayTransactions(transactions);
    const prior = state.requests.get(args.request.id);
    if (prior)
      return {
        state,
        transactions,
        commit: prior,
        persisted: false,
        recovered: true,
        idempotent: true,
        publishedNow: false,
        reused: true,
        assignments: [...state.dispatches.values()].filter(dispatch => dispatch.request_id === args.request.id),
      };
    const refresh = args.refreshObservations ? await args.refreshObservations(structuredClone(state), structuredClone(args.request), structuredClone(actor)) : [];
    const observations = Array.isArray(refresh) ? refresh : refresh.operations;
    const id = `test-commit-${++sequence}`;
    const now = ++at;
    const decision = await (args.generateOperations || generateRequestOperations)(state, args.request, actor, now, id, observations);
    if (!decision.operations.length) return { state, transactions, ...decision, persisted: false, publishedNow: false };
    const commit = { version: 3, id, previous: state.tip || null, request: args.request, actor, policy_epoch: state.policy_epoch, at: now, operations: decision.operations };
    const result = appendCommit(transactions, commit);
    transactions = result.transactions;
    return {
      ...decision,
      ...result,
      persisted: !result.idempotent,
      recovered: result.idempotent,
      publishedNow: !result.idempotent,
      reused: result.idempotent,
      ...(args.request.kind === "dispatch_next" ? { assignments: [...result.state.dispatches.values()].filter(dispatch => dispatch.request_id === args.request.id) } : {}),
    };
  }
  return {
    assignment,
    binding,
    policy,
    administrator,
    dispatcher,
    workerActor,
    workerContext,
    dispatcherContext,
    githubClient,
    nativeRun,
    append,
    readWorkQueueLog,
    publishWorkQueueRequest,
    get transactions() {
      return transactions;
    },
    get state() {
      return replayTransactions(transactions);
    },
    get at() {
      return at;
    },
  };
}

module.exports = { REF, REPOSITORY, WORKFLOW, DISPATCHER, queueFixture };
