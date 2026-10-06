// @ts-check
"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { randomUUID } = require("node:crypto");
const { queueFixture, REF, REPOSITORY, WORKFLOW, DISPATCHER } = require("./work_queue_lifecycle.test_helpers.cjs");
const { immutableRef, nativeId, dispatchResponse, validateNativeRun } = require("./work_queue_native.cjs");
const { createWorkQueueDispatchTool, createWorkQueueFinishTool, createWorkQueueSubmitTool, loadWorkQueueSnapshot, readWorkQueueState } = require("./work_queue_mcp_server.cjs");
const { readStagedIntents, requestForIntent } = require("./work_queue_intents.cjs");
const { main: activate } = require("./write_work_queue_snapshot.cjs");
const { authorizeWorkerClaim, reconcileWorkerClaim, finalizeWorkerResults } = require("./finish_work_queue_claim.cjs");
const { dispatchQueueIntent, launchAssignment, processWorkQueueIntents } = require("./work_queue_dispatch.cjs");
const { cancelBeforeLaunch, reconcileDispatch } = require("./work_queue_reconciler.cjs");
const { resolveExternalEdges, isFreshObservation } = require("./work_queue_dependency_resolver.cjs");
const { renderSummary } = require("./work_queue_summary.cjs");

async function run() {
  const directory = path.join(process.cwd(), `.queue-lifecycle-checks-${randomUUID()}`);
  fs.mkdirSync(directory);
  let checked = 0;
  const check = condition => {
    assert(condition);
    checked++;
  };
  const optionsFor = (fixture, worker = true) => ({
    githubClient: fixture.githubClient,
    context: worker ? fixture.workerContext : fixture.dispatcherContext,
    workflowRef: `${REPOSITORY}/${worker ? WORKFLOW : DISPATCHER}@${REF}`,
    assignment: fixture.assignment,
    readWorkQueueLog: fixture.readWorkQueueLog,
    publishWorkQueueRequest: fixture.publishWorkQueueRequest,
    now: fixture.at,
    sleepFn: async () => {},
  });
  const stageFinish = (filename, handle, outcome) => fs.appendFileSync(filename, `${JSON.stringify({ version: 3, kind: "finish", intent_id: `finish:${handle}:${outcome}`, claim_handle: handle, parameters: { outcome } })}\n`);
  const prepareLaunch = (fixture, response) => {
    let posts = 0;
    fixture.githubClient.rest.actions.createWorkflowDispatch = async parameters => {
      posts++;
      const assignment = JSON.parse(parameters.inputs.work_queue_assignment);
      check(fixture.state.dispatches.get(assignment.dispatch_id).state === "started");
      check(parameters.headers["X-GitHub-Api-Version"] === "2026-03-10" && parameters.request.retries === 0);
      check(parameters.ref === REF);
      if (response instanceof Error || response?.response) throw response;
      return response || { status: 200, data: { workflow_run_id: "42", run_url: "https://api.github.com/repos/owner/repo/actions/runs/42", html_url: "https://github.com/owner/repo/actions/runs/42" } };
    };
    fixture.githubClient.rest.actions.listWorkflowRuns = async () => ({ status: 200, data: { workflow_runs: [] } });
    return () => posts;
  };
  try {
    check(nativeId("9007199254740993") === "9007199254740993");
    check(immutableRef("a".repeat(64)) === "a".repeat(64));
    assert.throws(() => nativeId(9007199254740993));
    assert.throws(() => dispatchResponse({ status: 204, data: {} }, { repository: REPOSITORY }));

    const readFixture = queueFixture({ granted: false });
    const snapshot = { projection: readFixture.state, worker: null, sha: "activation", captured_at: readFixture.at };
    const before = readWorkQueueState(snapshot, { limit: 1 });
    const sorted = readWorkQueueState(snapshot, { sort: [{ key: "id", direction: "desc" }] });
    check(before.works.length === 1 && before.total === 3 && before.next_offset === 1);
    check(before.prediction.work_id === sorted.prediction.work_id && before.prediction.authoritative === false);
    assert.throws(() => readWorkQueueState(snapshot, { limit: 129 }));
    const intentPath = path.join(directory, "controls.jsonl");
    const dispatchTool = createWorkQueueDispatchTool(snapshot, { intentPath, createIntentId: () => "dispatch-intent" });
    check(JSON.parse(dispatchTool.handler({ pool: "default", max_claims: 2, max_dispatches: 1 }).content[0].text).status === "staged");
    assert.throws(() => dispatchTool.handler({ pool: "default", max_claims: 1, max_dispatches: 1, work_id: "preferred" }));
    const submitTool = createWorkQueueSubmitTool(snapshot, { intentPath, createIntentId: () => "submit-intent" });
    submitTool.handler({ nodes: [{ graph_id: "g2", node_key: "n", payload: { plan: "new stored plan" } }] });
    check(readStagedIntents(intentPath).length === 2 && readFixture.state.claims.size === 0);
    const stagedControlsCount = readFixture.transactions.length;
    const stagedControls = await processWorkQueueIntents({ ...optionsFor(readFixture, false), assignment: null, intentPath, config: { staged: true } });
    check(stagedControls.receipts.every(receipt => receipt.status === "staged_preview") && readFixture.transactions.length === stagedControlsCount);
    prepareLaunch(readFixture, { status: 204, data: {} });
    const controls = await processWorkQueueIntents({ ...optionsFor(readFixture, false), assignment: null, intentPath, maxDispatches: 1, config: { work_queue_workflows: ["worker"], aw_context_workflows: ["worker"], max: 1 } });
    check(controls.receipts.every(receipt => receipt.status === "durable"));
    check(readFixture.state.works.size === 4 && readFixture.state.claims.size === 2);
    const proposalRace = queueFixture({ count: 1, granted: false });
    proposalRace.append("cancel_work", { operations: [...proposalRace.state.works.keys()].map(work_id => ({ kind: "WorkCancellation", work_id, reason: "draining" })) });
    const proposalRacePath = path.join(directory, "proposal-race.jsonl");
    fs.writeFileSync(proposalRacePath, `${JSON.stringify({ version: 3, intent_id: "proposal-race", kind: "submit", parameters: { nodes: [{ graph_id: "g2", node_key: "n", payload: { plan: "new stored plan" } }] } })}\n`);
    const raced = await processWorkQueueIntents({
      ...optionsFor(proposalRace, false),
      assignment: null,
      policyProposal: proposalRace.policy,
      intentPath: proposalRacePath,
      publishWorkQueueRequest: async args => {
        const changed = JSON.parse(JSON.stringify(proposalRace.policy));
        changed.class_weights[0]++;
        proposalRace.append("policy", { operations: [{ kind: "Policy", epoch: "e2", policy: changed }] });
        return proposalRace.publishWorkQueueRequest(args);
      },
    });
    check(raced.receipts[0].status === "blocked" && proposalRace.state.works.size === 1);

    const activationFixture = queueFixture({ started: true });
    const activationOptions = { ...optionsFor(activationFixture), snapshotPath: path.join(directory, "activation.json"), core: { info: () => {} }, requireAssignment: true };
    const activated = await activate(activationOptions);
    check(activated.worker.claims.length === 3 && activated.origin.principal === "11");
    check(activationFixture.state.dispatches.get(activated.worker.dispatch_id).run.run_id === "42");
    check(loadWorkQueueSnapshot(activationOptions.snapshotPath).worker.claims[0].work.plan === "stored task 1");
    check((fs.statSync(activationOptions.snapshotPath).mode & 0o777) === 0o444);
    assert.throws(() => validateNativeRun({ ...activationFixture.nativeRun(), run_attempt: 2 }, { repository: REPOSITORY, workflow: WORKFLOW, ref: REF, principal_id: "11" }));

    const fixture = queueFixture({ bound: true });
    const finishPath = path.join(directory, "finish.jsonl");
    const finishTool = createWorkQueueFinishTool({ snapshot: { worker: fixture.assignment }, finishIntentPath: finishPath, createIntentId: () => "finish-h1" });
    assert.throws(() => finishTool.handler({}));
    assert.throws(() => finishTool.handler({ claim_handle: null }));
    finishTool.handler({ claim_handle: "h1", outcome: "completed" });
    stageFinish(finishPath, "h2", "cancelled");
    stageFinish(finishPath, "h3", "completed");
    const workerOptions = { ...optionsFor(fixture), finishIntentPath: finishPath };
    const stagedCount = fixture.transactions.length;
    check((await reconcileWorkerClaim({ ...workerOptions, staged: true })).claims.h1.state === "staged_preview");
    check(
      (
        await finalizeWorkerResults({
          ...workerOptions,
          staged: true,
          verifyEffects: async () => {
            throw new Error("preview must not verify");
          },
        })
      ).claims.h1.state === "staged_preview"
    );
    check(fixture.transactions.length === stagedCount && [...fixture.state.claims.values()].every(claim => claim.state === "open"));
    const finished = await reconcileWorkerClaim(workerOptions);
    check(finished.status === "completed_with_cancellations" && !Object.hasOwn(finished, "authorized"));
    check(finished.claims.h1.authorized && !finished.claims.h2.authorized && finished.claims.h3.authorized);
    check(!fixture.state.dispatches.get(fixture.assignment.dispatch_id).released);
    check((await authorizeWorkerClaim({ ...workerOptions, claim_handle: "h2" })).suppressed);
    const count = fixture.transactions.length;
    await reconcileWorkerClaim(workerOptions);
    check(fixture.transactions.length === count);
    let verifications = 0;
    const verifyEffects = async member => {
      verifications++;
      return { verified: true, contractVerified: true, receipt: `trusted:${member.handle}`, descriptor: { report: member.work.plan }, effects: "none" };
    };
    const result = await finalizeWorkerResults({ ...workerOptions, now: fixture.at, verifyEffects });
    check(result.claims.h1.state === "result" && result.claims.h3.state === "result" && result.claims.h2.state === "cancelled");
    const settled = await finalizeWorkerResults({ ...workerOptions, now: fixture.at, verifyEffects });
    check(verifications === 2);
    check(settled.claims.h1.effects === "none" && settled.claims.h3.effects === "none");
    check((await authorizeWorkerClaim({ ...workerOptions, claim_handle: "h1" })).authorized === false);
    await assert.rejects(authorizeWorkerClaim({ ...workerOptions, claim_handle: "h1", resource: { repository: "foreign/repo" } }), /scope/);
    check(renderSummary({ projection: activationFixture.state, worker: fixture.assignment }, fixture.state).includes("completed_with_cancellations"));
    const fanout = queueFixture({
      count: 2,
      bound: true,
      workerPrincipal: "22",
      workDefaults: { priority: 1, fairness_key: "tenant" },
      configurePolicy: policy => {
        policy.accounting_weights.tenant = 1;
        policy.producers["11"].fairness_keys.push("tenant");
      },
    });
    const fanoutFinishPath = path.join(directory, "fanout-finish.jsonl");
    stageFinish(fanoutFinishPath, "h1", "completed");
    stageFinish(fanoutFinishPath, "h2", "cancelled");
    const fanoutOptions = { ...optionsFor(fanout), finishIntentPath: fanoutFinishPath };
    await reconcileWorkerClaim(fanoutOptions);
    await finalizeWorkerResults({ ...fanoutOptions, now: fanout.at, verifyEffects: async () => ({ verified: true, contractVerified: true, receipt: "trusted:fanout", descriptor: { outputs: [] }, effects: "none" }) });
    const fanoutPath = path.join(directory, "fanout-controls.jsonl");
    const child = { graph_id: "g1", node_key: "child", payload: { plan: "scoped follow-up", effect_contract: { kind: "none" } }, depends_on: [{ kind: "work", work_id: fanout.assignment.claims[0].work_id }] };
    fs.writeFileSync(
      fanoutPath,
      [
        { version: 3, intent_id: "child-h1", kind: "submit", claim_handle: "h1", parameters: { nodes: [child] } },
        { version: 3, intent_id: "child-cancelled-h2", kind: "submit", claim_handle: "h2", parameters: { nodes: [{ ...child, node_key: "cancelled-child" }] } },
        { version: 3, intent_id: "child-cross-scope", kind: "submit", claim_handle: "h1", parameters: { nodes: [{ ...child, node_key: "cross-scope-child", priority: 5 }] } },
      ]
        .map(value => JSON.stringify(value))
        .join("\n") + "\n"
    );
    const publicationErrors = [];
    const children = await processWorkQueueIntents({
      ...fanoutOptions,
      now: fanout.at,
      intentPath: fanoutPath,
      publishWorkQueueRequest: async args => {
        try {
          return await fanout.publishWorkQueueRequest(args);
        } catch (error) {
          publicationErrors.push(error.message);
          throw error;
        }
      },
    });
    assert(children.receipts[0].status === "durable" && children.receipts[1].status === "blocked" && children.receipts[2].status === "blocked", JSON.stringify({ children, publicationErrors }));
    checked++;
    const admittedChild = [...fanout.state.works.values()].find(work => work.node_key === "child");
    check(admittedChild.priority === 1 && admittedChild.fairness_key === "tenant" && admittedChild.pool === "default");
    check(fanout.state.works.size === 3 && fanout.state.works.get(fanout.assignment.claims[0].work_id).barrier === "verified");

    const unknown = queueFixture({ count: 1, bound: true });
    const unknownPath = path.join(directory, "unknown-finish.jsonl");
    stageFinish(unknownPath, "h1", "completed");
    const unknownOptions = { ...optionsFor(unknown), finishIntentPath: unknownPath };
    await reconcileWorkerClaim(unknownOptions);
    const unknownVerification = async () => ({ verified: false, effects: "none" });
    const pending = await finalizeWorkerResults({ ...unknownOptions, now: unknown.at, verifyEffects: unknownVerification });
    check(pending.claims.h1.state === "pending" && pending.claims.h1.effects === "unknown");
    const getUnknown = unknown.githubClient.rest.actions.getWorkflowRun;
    unknown.githubClient.rest.actions.getWorkflowRun = async args => ({ status: 200, data: { ...(await getUnknown(args)).data, status: "completed", conclusion: "failure" } });
    const failed = await finalizeWorkerResults({ ...unknownOptions, now: unknown.at, verifyEffects: unknownVerification });
    check(failed.claims.h1.state === "delivery_failed" && failed.claims.h1.effects === "unknown");

    const launch = queueFixture();
    const posts = prepareLaunch(launch);
    const launchOptions = optionsFor(launch, false);
    check((await launchAssignment(launchOptions, launch.assignment)).state === "bound");
    await launchAssignment(launchOptions, launch.assignment);
    check(posts() === 1 && launch.state.dispatches.get(launch.assignment.dispatch_id).run.run_id === "42");
    const concurrent = queueFixture();
    const concurrentPosts = prepareLaunch(concurrent);
    await Promise.all([launchAssignment(optionsFor(concurrent, false), concurrent.assignment), launchAssignment(optionsFor(concurrent, false), concurrent.assignment)]);
    check(concurrentPosts() === 1 && concurrent.state.dispatches.get(concurrent.assignment.dispatch_id).run.run_id === "42");
    for (const destination of [{ repository: "foreign/repo" }, { workflow: ".github/workflows/other.yml" }, { ref: "b".repeat(40) }]) {
      const invalidDestination = queueFixture();
      const destinationPosts = prepareLaunch(invalidDestination);
      await assert.rejects(launchAssignment({ ...optionsFor(invalidDestination, false), destination }, invalidDestination.assignment));
      check(destinationPosts() === 0 && invalidDestination.state.dispatches.get(invalidDestination.assignment.dispatch_id).state === "reserved");
    }
    for (const response of [new Error("timeout"), { status: 204, data: {} }, { status: 200, data: { workflow_run_id: "42" } }]) {
      const uncertain = queueFixture();
      const uncertainPosts = prepareLaunch(uncertain, response);
      const uncertainOptions = optionsFor(uncertain, false);
      check((await launchAssignment(uncertainOptions, uncertain.assignment)).state === "launch_unresolved");
      await launchAssignment(uncertainOptions, uncertain.assignment);
      check(uncertainPosts() === 1 && !uncertain.state.dispatches.get(uncertain.assignment.dispatch_id).released);
      check([...uncertain.state.claims.values()].every(claim => claim.state === "open"));
    }
    const recovered = queueFixture();
    const recoveredPosts = prepareLaunch(recovered);
    const recoveredOptions = optionsFor(recovered, false);
    const publisher = recoveredOptions.publishWorkQueueRequest;
    recoveredOptions.publishWorkQueueRequest = async args => {
      const result = await publisher(args);
      return args.request.parameters.operations?.[0]?.state === "started" ? { ...result, persisted: false, recovered: true, idempotent: true, publishedNow: false, reused: true } : result;
    };
    await launchAssignment(recoveredOptions, recovered.assignment);
    check(recoveredPosts() === 0 && !recovered.state.dispatches.get(recovered.assignment.dispatch_id).released);

    const rejected = queueFixture();
    const rejectedPosts = prepareLaunch(rejected, { status: 422, response: { status: 422, headers: { "x-github-request-id": "positive-provider-evidence" } } });
    check((await launchAssignment(optionsFor(rejected, false), rejected.assignment)).released);
    check([...rejected.state.claims.values()].every(claim => claim.state === "cancelled"));
    await launchAssignment(optionsFor(rejected, false), rejected.assignment);
    check(rejectedPosts() === 1);
    const reserved = queueFixture();
    check((await cancelBeforeLaunch(optionsFor(reserved, false))).released);
    reserved.append("cancel_work", { operations: reserved.assignment.claims.map(member => ({ kind: "WorkCancellation", work_id: member.work_id, reason: "retired" })) });
    const nextPolicy = JSON.parse(JSON.stringify(reserved.policy));
    nextPolicy.pools.default.profiles.default.ref = "b".repeat(40);
    reserved.append("policy", { operations: [{ kind: "Policy", epoch: "e2", policy: nextPolicy }] });
    check((await launchAssignment(optionsFor(reserved, false), reserved.assignment)).state === "released");
    const started = queueFixture({ started: true });
    await assert.rejects(cancelBeforeLaunch(optionsFor(started, false)));

    const duplicate = queueFixture({ started: true });
    duplicate.githubClient.rest.actions.listWorkflowRuns = async () => ({ status: 200, data: { workflow_runs: [duplicate.nativeRun(), { ...duplicate.nativeRun(), id: "43" }] } });
    check((await reconcileDispatch(optionsFor(duplicate, false))).state === "run_binding_conflict");
    check(!duplicate.state.dispatches.get(duplicate.assignment.dispatch_id).run);
    const terminal = queueFixture({ bound: true });
    terminal.append("finish", { dispatch_id: terminal.assignment.dispatch_id, claim_handle: "h1", outcome: "completed" }, { ...terminal.workerActor, dispatch_id: terminal.assignment.dispatch_id, claim_handle: "h1" });
    const getTerminal = terminal.githubClient.rest.actions.getWorkflowRun;
    terminal.githubClient.rest.actions.getWorkflowRun = async args => ({ status: 200, data: { ...(await getTerminal(args)).data, ...(args.run_id === "42" ? { status: "completed", conclusion: "cancelled" } : {}) } });
    check((await reconcileDispatch(optionsFor(terminal, false))).released);
    check(terminal.state.claims.get(terminal.assignment.claims[0].claim_id).state === "completed");
    check(terminal.state.works.get(terminal.assignment.claims[0].work_id).barrier === "pending");
    const rerun = queueFixture({ bound: true });
    const rerunGet = rerun.githubClient.rest.actions.getWorkflowRun;
    rerun.githubClient.rest.actions.getWorkflowRun = async args => (args.run_id === "42" ? { status: 200, data: { ...rerun.nativeRun(), run_attempt: 2 } } : rerunGet(args));
    let exactAttempts = 0;
    rerun.githubClient.rest.actions.getWorkflowRunAttempt = async parameters => {
      check(parameters.attempt_number === 1 && parameters.headers["X-GitHub-Api-Version"] === "2026-03-10");
      exactAttempts++;
      return { status: 200, data: { ...rerun.nativeRun(), status: "completed", conclusion: "failure" } };
    };
    check((await reconcileDispatch(optionsFor(rerun, false))).released && exactAttempts === 1);

    const budgetFixture = queueFixture({ granted: false });
    prepareLaunch(budgetFixture, { status: 204, data: {} });
    const budgetOptions = { ...optionsFor(budgetFixture, false), assignment: null, config: { work_queue_workflows: ["worker"], aw_context_workflows: ["worker"], max: 1 } };
    const requestMessage = { type: "work_queue_dispatch_next", intent_id: "batch1", pool: "default", max_claims: 2, max_dispatches: 1 };
    const batch = await dispatchQueueIntent({ ...budgetOptions, remainingDispatches: 1, message: requestMessage });
    check(batch.dispatches === 1 && budgetFixture.state.claims.size === 2);
    await assert.rejects(dispatchQueueIntent({ ...budgetOptions, remainingDispatches: 1, message: { ...requestMessage, intent_id: "batch2" } }), /budget/);
    const racing = queueFixture({ granted: false });
    const racingPosts = prepareLaunch(racing);
    const competingPublisher = async args => {
      racing.append("dispatch_next", { pool: "default", max_claims: 1, max_dispatches: 1, max_bytes: racing.policy.limits.assignment_bytes }, racing.dispatcher, "competing-grant");
      return racing.publishWorkQueueRequest(args);
    };
    await assert.rejects(
      dispatchQueueIntent({ ...optionsFor(racing, false), assignment: null, config: budgetOptions.config, remainingDispatches: 1, publishWorkQueueRequest: competingPublisher, message: { ...requestMessage, intent_id: "racing-grant" } }),
      /budget/
    );
    check(racingPosts() === 0 && racing.state.dispatches.size === 1);
    const trustedOrigin = { authenticated: true, roles: ["dispatcher"], ...budgetFixture.dispatcher };
    const stableParameters = { pool: "default", max_claims: 1, max_dispatches: 1, max_bytes: 1000 };
    const stable = requestForIntent(trustedOrigin, "logical-intent", "dispatch_next", stableParameters);
    check(stable.id === requestForIntent(trustedOrigin, "logical-intent", "dispatch_next", stableParameters).id);
    check(stable.id !== requestForIntent({ ...trustedOrigin, run_id: "16" }, "logical-intent", "dispatch_next", stableParameters).id);

    const resource = { kind: "issue", host: "github.com", repository: "foreign/design", repository_id: "20", resource_id: "30", number: "7" };
    const scope = { host: "github.com", repository: "foreign/design", access_generation: "initial" };
    let issueReads = 0;
    const resourceClient = {
      rest: {
        repos: { get: async () => ({ data: { id: 20, full_name: "foreign/design" } }) },
        issues: {
          get: async () => {
            issueReads++;
            return { data: { id: 30, number: 7, state: "closed", state_reason: "completed" } };
          },
        },
      },
    };
    const dependencies = await resolveExternalEdges(
      [
        { kind: "issue", resource, condition: "completed" },
        { kind: "issue", resource, condition: "closed" },
      ],
      { scopes: [scope], getClient: async () => resourceClient, now: 1000 }
    );
    check(issueReads === 1 && dependencies.operations.length === 2 && dependencies.operations[0].state === "ready");
    await assert.rejects(resolveExternalEdges([{ kind: "issue", resource, condition: "completed" }], { scopes: [], getClient: async () => resourceClient, now: 1000 }));
    const observation = { resource: { ...resource, condition: "completed" }, state: "ready", observed_at: 900, access_generation: "initial" };
    check(isFreshObservation(observation, observation.resource, scope, 1000, 100));
    check(!isFreshObservation(observation, observation.resource, { ...scope, access_generation: "revoked" }, 1000, 100));

    const foreign = queueFixture({
      count: 1,
      granted: false,
      configurePolicy: policy => {
        policy.pools.default.allowed_repositories.push("foreign/design");
      },
    });
    const foreignControls = path.join(directory, "foreign-controls.jsonl");
    fs.writeFileSync(
      foreignControls,
      `${JSON.stringify({ version: 3, intent_id: "foreign-submit", kind: "submit", parameters: { nodes: [{ graph_id: "g2", node_key: "external", payload: { plan: "authorized foreign gate" }, depends_on: [{ kind: "issue", resource: { kind: "issue", host: "github.com", repository: "foreign/design", number: "7" }, condition: "completed" }] }] } })}\n`
    );
    const foreignOptions = {
      ...optionsFor(foreign, false),
      assignment: null,
      intentPath: foreignControls,
      maxDispatches: 1,
      config: { work_queue_workflows: ["worker"], aw_context_workflows: ["worker"], max: 1 },
      dependencyResolver: { scopes: [scope], getClient: async () => resourceClient },
    };
    const foreignSubmit = await processWorkQueueIntents(foreignOptions);
    check(foreignSubmit.receipts[0].status === "durable");
    const admittedForeign = [...foreign.state.works.values()].find(work => work.graph_id === "g2");
    check(admittedForeign.depends_on[0].resource.repository_id === "20" && admittedForeign.depends_on[0].resource.resource_id === "30");
    check(foreign.state.observations.size === 0 && foreign.state.claims.size === 0);
    prepareLaunch(foreign, { status: 204, data: {} });
    const foreignBatch = await dispatchQueueIntent({
      ...foreignOptions,
      now: foreign.at,
      remainingDispatches: 1,
      message: { type: "work_queue_dispatch_next", intent_id: "foreign-dispatch", pool: "default", max_claims: 2, max_dispatches: 1 },
    });
    check(foreignBatch.dispatches === 1 && foreign.state.claims.size === 2 && foreign.state.observations.size === 1);
    const foreignClaim = [...foreign.state.claims.values()].find(claim => claim.work_id === admittedForeign.work_id);
    check(foreignClaim.observations.length === 1);
    check(foreign.state.policy.pools.default.profiles.default.effect_scope === REPOSITORY);

    console.log(`Work queue lifecycle checks passed (${checked} assertions, plus rejection/ambiguity checks).`);
  } finally {
    fs.rmSync(directory, { recursive: true, force: true });
  }
}

if (require.main === module)
  run().catch(error => {
    console.error(error);
    process.exitCode = 1;
  });
module.exports = { run };
