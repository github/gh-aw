"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { createHash } = require("node:crypto");
const { main: writeSnapshot } = require("./write_work_queue_snapshot.cjs");
const { createWorkQueueFinishTool, createWorkQueueSubmitTool, loadWorkQueueSnapshot } = require("./work_queue_mcp_server.cjs");
const { authorizeWorkerClaim, finalizeWorkerResults, reconcileWorkerClaim } = require("./finish_work_queue_claim.cjs");
const { inheritWorkerSubmission, intentContext, processWorkQueueIntents } = require("./work_queue_dispatch.cjs");
const { authenticatePublisher } = require("./work_queue_native.cjs");
const { readDeliveryControlInventory, createClaimDeliveryVerifier, verifyClaimDelivery } = require("./work_queue_delivery.cjs");
const { closeClaimEffectChannel, withClaimExecution } = require("./work_queue_claim_scope.cjs");
const { verifyClaimQueueControl } = require("./work_queue_control_receipts.cjs");
const { normalizeSubmitParameters, readStagedIntents, requestForIntent } = require("./work_queue_intents.cjs");
const { publishWorkQueueRequest } = require("./work_queue_store.cjs");
const { canonical } = require("./work_queue_codec.cjs");
const { nodeId } = require("./work_queue_graph.cjs");
const { actorFromContext } = require("./work_queue_policy.cjs");
const { explainWork, replayTransactions, validateRequestContext, validateWorkerContinuation } = require("./work_queue_replay.cjs");
const { fakeGitHub } = require("./work_queue_store_checks.cjs");
const { queueFixture, REF, REPOSITORY, WORKFLOW } = require("./work_queue_lifecycle.test_helpers.cjs");

function sourceHashes() {
  const names = [
    "work_queue_worker_child_lifecycle_checks",
    "work_queue_worker_child_lifecycle.test",
    "write_work_queue_snapshot",
    "work_queue_mcp_server",
    "finish_work_queue_claim",
    "work_queue_dispatch",
    "work_queue_binding",
    "work_queue_native",
    "work_queue_control_receipts",
    "work_queue_delivery",
    "work_queue_claim_scope",
    "work_queue_intents",
    "work_queue_codec",
    "work_queue_policy",
    "work_queue_graph",
    "work_queue_resource_scope",
    "work_queue_replay",
    "work_queue_store",
    "work_queue_lifecycle.test_helpers",
    "work_queue_store_checks",
  ];
  return Object.fromEntries(
    names.map(name => [
      name,
      createHash("sha256")
        .update(fs.readFileSync(path.join(__dirname, `${name}.cjs`)))
        .digest("hex"),
    ])
  );
}

function registerTests({ describe, it }) {
  describe("integrated original-worker child capability", () => {
    it("case192 publishes worker22 children before Result, rejects fresh controls after Result, and recovers accepted requests with only producer11 registered", async () => {
      const hashes = sourceHashes();
      console.log(`case192_source_sha256=${JSON.stringify(hashes)}`);
      const fixture = queueFixture({
        started: true,
        count: 1,
        workerPrincipal: "22",
        workDefaults: { priority: 1, fairness_key: "tenant", payload: { effect_contract: { version: 1, outputs: [{ type: "work_queue_submit", min: 1, max: 2 }] } } },
        configurePolicy: policy => {
          policy.accounting_weights.tenant = 7;
          policy.producers["11"].priorities = [1];
          policy.producers["11"].fairness_keys = ["tenant"];
        },
      });
      const fake = fakeGitHub(fixture.transactions);
      let nativePrincipal = "22";
      let dispatchPosts = 0;
      const githubClient = {
        rest: {
          repos: fixture.githubClient.rest.repos,
          git: fake.githubClient.rest.git,
          actions: {
            ...fixture.githubClient.rest.actions,
            getWorkflowRun: async () => ({ status: 200, data: { ...fixture.nativeRun(), actor: { id: nativePrincipal }, triggering_actor: { id: nativePrincipal } } }),
            createWorkflowDispatch: async () => {
              dispatchPosts++;
              throw new Error("Child admission must not launch a native workflow");
            },
          },
        },
      };
      const directory = fs.mkdtempSync(path.join(fs.realpathSync(os.tmpdir()), "gh-aw-queue-case192-"));
      /** @type {string | undefined} */
      let originOutput;
      const options = {
        githubClient,
        context: fixture.workerContext,
        workflowRef: `${REPOSITORY}/${WORKFLOW}@${REF}`,
        role: "worker",
        requireAssignment: true,
        policyProposal: undefined,
        snapshotPath: path.join(directory, "snapshot.json"),
        finishIntentPath: path.join(directory, "finish.jsonl"),
        intentPath: path.join(directory, "intents.jsonl"),
        core: {
          info: () => {},
          setOutput: (name, value) => {
            assert.equal(name, "work_queue_origin");
            originOutput = value;
          },
        },
        sleepFn: async () => {},
      };
      try {
        assert.deepEqual(Object.keys(fixture.policy.producers), ["11"]);
        const snapshot = await writeSnapshot(options);
        assert.equal(snapshot.origin.principal, "22");
        assert.equal(snapshot.origin.role, "worker");
        assert.equal(snapshot.origin.run_attempt, 1);
        assert.deepEqual(snapshot.worker, fixture.assignment);
        assert.ok(typeof originOutput === "string");
        assert.equal(JSON.parse(originOutput).principal, "22");
        assert.equal(fake.log().at(-1).operations[0].run.principal, "22");
        assert.equal(fake.log().at(-1).actor.principal, "22");
        const assignment = snapshot.worker;
        assert.ok(assignment);
        const member = assignment.claims[0];
        const loaded = loadWorkQueueSnapshot(options.snapshotPath);
        const runtime = { ...options, assignment, intentOrigin: originOutput };
        const parameters = {
          nodes: [{ graph_id: "g1", node_key: "case192-child", payload: { task: "inherited child", effect_contract: { kind: "none" } }, depends_on: [{ kind: "work", work_id: member.work_id }] }],
        };
        const staged = JSON.parse(createWorkQueueSubmitTool(loaded, { ...options, createIntentId: () => "case192-child" }).handler(parameters).content[0].text);
        assert.equal(staged.status, "staged");
        assert.equal(staged.claim_handle, member.handle);
        assert.equal(fake.log().length, fixture.transactions.length + 1);
        const beforeOpen = canonical(fake.log());
        assert.equal((await processWorkQueueIntents(runtime)).receipts[0].status, "blocked");
        assert.equal(canonical(fake.log()), beforeOpen);
        createWorkQueueFinishTool({ ...options, snapshot: loaded, createIntentId: () => "case192-finish" }).handler({ outcome: "completed" });
        const closure = await reconcileWorkerClaim(runtime);
        assert.equal(closure.claims[member.handle].state, "completed");
        assert.equal(closure.claims[member.handle].authorized, true);
        assert.equal(fake.log().at(-1).operations[0].kind, "Completion");
        assert.equal(fake.log().at(-1).actor.principal, "22");
        const completedPrefix = canonical(fake.log());
        nativePrincipal = "11";
        assert.equal((await processWorkQueueIntents(runtime)).receipts[0].status, "blocked");
        assert.equal(canonical(fake.log()), completedPrefix);
        nativePrincipal = "22";
        const trusted = await intentContext(runtime, { intent_id: staged.intent_id, claim_handle: member.handle });
        const completedState = replayTransactions(fake.log());
        const normalized = normalizeSubmitParameters(inheritWorkerSubmission(completedState, trusted, parameters), completedState.policy, trusted.created_at, completedState);
        const expectedRequest = requestForIntent(trusted, staged.intent_id, "submit", normalized);
        validateRequestContext(completedState, expectedRequest, actorFromContext(trusted));
        validateWorkerContinuation(completedState, trusted);
        const publication = await processWorkQueueIntents(runtime);
        assert.equal(publication.receipts[0].status, "durable", JSON.stringify(publication));
        const childCommit = fake.log().at(-1);
        assert.equal(canonical(childCommit.actor), canonical({ ...snapshot.origin, claim_handle: member.handle }));
        assert.equal(childCommit.request.kind, "submit");
        assert.equal(childCommit.operations.length, 1);
        assert.equal(canonical(childCommit.operations[0].payload), canonical(parameters.nodes[0].payload));
        assert.equal(childCommit.operations[0].pool, "default");
        assert.equal(childCommit.operations[0].priority, 1);
        assert.equal(childCommit.operations[0].fairness_key, "tenant");
        const childId = nodeId("g1", "case192-child");
        assert.equal(childCommit.operations[0].work_id, childId);
        let state = replayTransactions(fake.log());
        assert.deepEqual(Object.keys(state.policy.producers), ["11"]);
        assert.equal(state.works.get(childId).state, "available");
        assert.equal(explainWork(state, childId, Date.now()).readiness.ready, false);
        const unscoped = await authenticatePublisher({ ...options, role: "producer" });
        const root = { ...childCommit.operations[0], graph_id: "unscoped-worker22", node_key: "root", work_id: nodeId("unscoped-worker22", "root"), depends_on: [] };
        const rootRequest = requestForIntent(unscoped, "case192-root-escalation", "submit", { nodes: [root] });
        const beforeRoot = canonical(fake.log());
        await assert.rejects(publishWorkQueueRequest({ githubClient, owner: "owner", repo: "repo", context: unscoped, request: rootRequest, sleepFn: async () => {} }), /entitlement|producer/);
        assert.equal(canonical(fake.log()), beforeRoot);
        const message = { type: "work_queue_submit", intent_id: staged.intent_id, claim_handle: member.handle, parameters: readStagedIntents(options.intentPath)[0].parameters };
        const authorize = request => authorizeWorkerClaim({ ...runtime, ...request });
        const protectedVerifier = createClaimDeliveryVerifier({
          assignment,
          recheck: async () => {
            const inventory = await readDeliveryControlInventory({ ...runtime, claim_handle: member.handle });
            assert.equal(inventory.controls.length, 1);
            assert.equal(inventory.controls[0].commit_id, childCommit.id);
            assert.equal(inventory.controls[0].work_id, member.work_id);
            assert.equal(inventory.controls[0].claim_id, member.claim_id);
            const effects = [];
            return withClaimExecution({ assignment, claim_handle: member.handle, authorize, effects }, async () => {
              const effectChannel = closeClaimEffectChannel();
              const delivery = await verifyClaimDelivery({
                ...runtime,
                claim_handle: member.handle,
                messages: [message],
                results: [{ messageIndex: 0, success: true, claim_handle: member.handle }],
                authorize,
                effects,
                effectChannel,
                requireControlInventory: true,
                readControlInventory: async () => inventory,
                verifyOutput: input => verifyClaimQueueControl({ ...runtime, message: input.message, inventory }),
              });
              assert.equal(delivery.verification, "verified", JSON.stringify(delivery));
              return delivery;
            });
          },
        });
        let verifierError;
        const verifyEffects = async (...args) => {
          try {
            return await protectedVerifier(...args);
          } catch (error) {
            verifierError = error;
            throw error;
          }
        };
        const finalized = await finalizeWorkerResults({ ...runtime, verifyEffects });
        assert.ifError(verifierError);
        assert.equal(finalized.claims[member.handle].state, "result", JSON.stringify(finalized));
        state = replayTransactions(fake.log());
        assert.equal(state.works.get(member.work_id).barrier, "verified");
        assert.equal(explainWork(state, childId, Date.now()).readiness.ready, true);
        assert.equal(state.dispatches.get(assignment.dispatch_id).released, false);
        assert.deepEqual(Object.keys(state.policy.producers), ["11"]);
        const beforeRecovery = canonical(fake.log());
        const recovered = await processWorkQueueIntents(runtime);
        assert.equal(recovered.receipts[0].status, "durable");
        assert.equal(recovered.receipts[0].request_id, publication.receipts[0].request_id);
        assert.equal(canonical(fake.log()), beforeRecovery);
        assert.equal(fake.log().filter(commit => commit.request.id === childCommit.request.id).length, 1);
        assert.equal((await authorizeWorkerClaim({ ...runtime, claim_handle: member.handle })).authorized, false);
        const followupParameters = { nodes: [{ node_key: "verified-parent-followup", payload: { task: "fresh continuation after Result", effect_contract: { kind: "none" } }, depends_on: [{ kind: "work", work_id: member.work_id }] }] };
        const followup = JSON.parse(createWorkQueueSubmitTool(loaded, { ...options, createIntentId: () => "case192-verified-continuation" }).handler(followupParameters).content[0].text);
        assert.equal(followup.status, "staged");
        nativePrincipal = "11";
        assert.equal((await processWorkQueueIntents(runtime)).receipts[1].status, "blocked");
        assert.equal(canonical(fake.log()), beforeRecovery);
        nativePrincipal = "22";
        const rejected = await processWorkQueueIntents(runtime);
        assert.equal(rejected.receipts[0].status, "durable", JSON.stringify(rejected));
        assert.equal(rejected.receipts[0].request_id, publication.receipts[0].request_id);
        assert.equal(rejected.receipts[1].status, "blocked", JSON.stringify(rejected));
        assert.equal(canonical(fake.log()), beforeRecovery);
        assert.equal(replayTransactions(fake.log()).works.has(nodeId("g1", "verified-parent-followup")), false);
        assert.equal((await authorizeWorkerClaim({ ...runtime, claim_handle: member.handle })).authorized, false);
        assert.equal(
          fake
            .log()
            .flatMap(commit => commit.operations)
            .filter(operation => operation.kind === "Result").length,
          1
        );
        const retry = await processWorkQueueIntents(runtime);
        assert.equal(retry.receipts[0].request_id, publication.receipts[0].request_id);
        assert.equal(retry.receipts[1].status, "blocked");
        assert.equal(canonical(fake.log()), beforeRecovery);
        assert.equal(dispatchPosts, 0);
        assert.deepEqual(sourceHashes(), hashes, "Integrated route sources changed during execution");
      } finally {
        fs.rmSync(directory, { recursive: true, force: true });
        assert.deepEqual(sourceHashes(), hashes, "Integrated route sources changed during execution");
      }
    });
  });
}

if (require.main === module) registerTests(require("node:test"));
module.exports = { registerTests };
