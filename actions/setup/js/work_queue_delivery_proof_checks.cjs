"use strict";

const assert = require("node:assert/strict");
const { queueFixture, REF, REPOSITORY, WORKFLOW } = require("./work_queue_lifecycle.test_helpers.cjs");
const { authorizeWorkerClaim, finalizeWorkerResults } = require("./finish_work_queue_claim.cjs");
const { withClaimExecution, closeClaimEffectChannel, claimEffectChannelMatches, recordClaimEffect, assertClaimAuthorized } = require("./work_queue_claim_scope.cjs");
const { inspectClaimDelivery, createClaimDeliveryVerifier, verifyClaimDelivery, isTrustedClaimDelivery, readDeliveryControlInventory, verifyBuiltinDeliveryOutput, validateDeliveryContract } = require("./work_queue_delivery.cjs");
const { wrapClaimEffectClient } = require("./work_queue_effect_client.cjs");
const { canonicalResourceTarget, nativeDecimalIdentity, resolveParentResourceTarget } = require("./work_queue_effect_resource.cjs");
const fs = require("node:fs");
const path = require("node:path");
const { createClaimAdapterHandler, preparedAdapterPath, createDeclaredAdapterVerifier, wrapDeclaredBuiltinHandler } = require("./work_queue_claim_adapters.cjs");
const { temporaryDirectory } = require("./work_queue_effect_test_helpers.cjs");

/** @typedef {{messages: Record<string, unknown>[], results: {messageIndex: number, success: boolean, claim_handle: string, result: Record<string, unknown>}[], effects: Record<string, unknown>[], effectChannel: ReturnType<typeof closeClaimEffectChannel>}} DeliveryProofRecord */

function deliveryResourceScope(existingIssue = false) {
  return {
    version: 1,
    resources: [{ host: "github.com", repository: REPOSITORY, repository_id: "7", ...(existingIssue ? { kind: "issue", resource_id: "100", number: "42" } : {}) }],
  };
}

function fixture(options = {}) {
  const queue = queueFixture({ bound: true, count: 2, ...options });
  const assignment = queue.assignment;
  if (!assignment) throw new Error("Delivery proof fixture requires a granted immutable assignment");
  for (const member of assignment.claims) {
    queue.append("finish", { dispatch_id: assignment.dispatch_id, claim_handle: member.handle, outcome: "completed" }, { ...queue.workerActor, dispatch_id: assignment.dispatch_id, claim_handle: member.handle });
  }
  const runtime = { assignment: queue.assignment, context: queue.workerContext, githubClient: queue.githubClient, workflowRef: `${REPOSITORY}/${WORKFLOW}@${REF}`, readWorkQueueLog: queue.readWorkQueueLog };
  const authorize = request => authorizeWorkerClaim({ ...runtime, ...request, githubClient: queue.githubClient });
  const verification = member => ({ assignment: queue.assignment, contract: member.work.effect_contract, attempt: 1, run: queue.binding });
  const inspect = (member, options = {}) => {
    const effects = options.effects || [];
    return withClaimExecution({ assignment: queue.assignment, claim_handle: member.handle, effects, authorize }, async () => {
      if (options.record) options.record();
      const channel = closeClaimEffectChannel();
      return inspectClaimDelivery({
        ...runtime,
        github: queue.githubClient,
        claim_handle: member.handle,
        authorize,
        effects,
        effectChannel: channel,
        requireControlInventory: true,
        readControlInventory: () => readDeliveryControlInventory({ ...runtime, claim_handle: member.handle }),
        ...options,
      });
    });
  };
  return { queue, assignment, runtime, authorize, verification, inspect };
}

function registerTests({ describe, it }) {
  describe("protected complete Claim delivery proof facade", () => {
    it("matches the canonical embedded delivery contract fixtures without deriving verifier authority", () => {
      const shared = require("../../../specs/work-queue/fixtures/effect-contract.json");
      for (const entry of shared.cases) {
        if (entry.valid) assert.doesNotThrow(() => validateDeliveryContract(entry.contract), entry.name);
        else assert.throws(() => validateDeliveryContract(entry.contract), entry.name);
      }
      for (const expected of [undefined, null, [], { value: 1.5 }, { value: Number.MAX_SAFE_INTEGER + 1 }, { value: "\ud800" }, { value: new Date() }]) {
        assert.throws(() => validateDeliveryContract({ version: 1, outputs: [{ type: "custom", min: 1, max: 1, verification: { verifier_id: "v1", expected } }] }));
      }
    });

    it("never treats structurally valid verification intent as a trusted readback implementation", async () => {
      const contract = { version: 1, outputs: [{ type: "create_issue", min: 1, max: 1, verification: { verifier_id: "issue.v1", expected: { title: "different title" } } }] };
      const f = fixture({ count: 1, workDefaults: { payload: { effect_contract: contract } } });
      const member = f.assignment.claims[0];
      let reads = 0;
      const delivery = await f.inspect(member, {
        messages: [{ type: "create_issue", claim_handle: member.handle, repo: REPOSITORY, title: "ordinary title" }],
        results: [{ messageIndex: 0, success: true, claim_handle: member.handle, result: { number: 42 } }],
        verifyOutput: async () => {
          reads++;
          return { verified: true, claim_handle: member.handle, resource: { kind: "issue", repository: REPOSITORY, id: "100", number: 42 }, evidence: { source: "independent_read" } };
        },
      });
      assert.equal(delivery.verification, "unknown");
      assert.match(delivery.reason, /supported trusted verifier binding/);
      assert.equal(reads, 0);
    });

    it("binds declared adapter expectations before effects and independently settles a valid sibling Result", async () => {
      const expected = { name: "approved", head_sha: REF };
      const contract = { version: 1, outputs: [{ type: "custom", min: 1, max: 1, verification: { verifier_id: "approved-check.v1", expected } }] };
      const f = fixture({
        workDefaults: { payload: { effect_contract: contract, resource_scope: deliveryResourceScope() } },
        configurePolicy: policy => {
          policy.pools.default.reconciliation.max_attempts = 1;
        },
      });
      const adapter = {
        mode: "prepared",
        "effect-type": "github_rest",
        "target-repo": REPOSITORY,
        "verifier-id": "approved-check.v1",
        "field-map": { name: "check_name" },
        expected: { head_sha: REF },
        request: { method: "POST", route: "/repos/{owner}/{repo}/check-runs", permission: "checks" },
        verifier: { route: "/repos/{owner}/{repo}/check-runs/{receipt_id}", "resource-kind": "check_run", fields: { name: "name", head_sha: "head_sha" } },
      };
      const root = temporaryDirectory("declared-delivery");
      const records = new Map();
      let writes = 0;
      let observed;
      const github = {
        ...f.queue.githubClient,
        request: async (route, fields) => {
          if (route.startsWith("POST ")) {
            writes++;
            assert.deepEqual(fields, expected);
            observed = { id: 91, ...fields };
          } else assert.equal(route, "GET /repos/owner/repo/check-runs/91");
          return { data: structuredClone(observed) };
        },
      };
      const verifyDeclaredOutput = createDeclaredAdapterVerifier({ custom: adapter });
      const authorize = request => authorizeWorkerClaim({ ...f.runtime, ...request, context: f.runtime.context, githubClient: f.runtime.githubClient });
      const recheck = async member => {
        const record = records.get(member.handle);
        return withClaimExecution({ assignment: f.assignment, claim_handle: member.handle, authorize, effects: record.effects }, () =>
          inspectClaimDelivery({
            ...f.runtime,
            ...record,
            github,
            claim_handle: member.handle,
            authorize,
            verifyDeclaredOutput,
            requireControlInventory: true,
            readControlInventory: () => readDeliveryControlInventory({ ...f.runtime, claim_handle: member.handle }),
          })
        );
      };
      try {
        for (const member of f.assignment.claims) {
          const effects = [];
          const message = { type: "custom", claim_handle: member.handle, check_name: member.handle === "h1" ? "wrong" : "approved" };
          await withClaimExecution({ assignment: f.assignment, claim_handle: member.handle, authorize, effects }, async () => {
            const filename = preparedAdapterPath(root, member.handle, "custom");
            fs.mkdirSync(path.dirname(filename), { recursive: true });
            fs.writeFileSync(filename, JSON.stringify({ version: 3, claim_handle: member.handle, type: "custom", messages: [{ input: message, payload: { check_name: message.check_name } }] }));
            const handler = await createClaimAdapterHandler({ adapter, filename, github });
            let result;
            if (member.handle === "h1") {
              await assert.rejects(handler(message), /immutable declared verification expectation/);
              assert.equal(writes, 0);
            } else result = await handler(message);
            records.set(member.handle, { messages: [message], results: [{ messageIndex: 0, success: !!result, claim_handle: member.handle, result }], effects, effectChannel: closeClaimEffectChannel() });
          });
        }
        const delivery = await recheck(f.assignment.claims[1]);
        assert.equal(delivery.verification, "verified");
        assert.equal(delivery.descriptor.outputs[0].evidence.verifier_id, "approved-check.v1");
        assert.equal(writes, 1);
        const result = await finalizeWorkerResults({
          ...f.runtime,
          publishWorkQueueRequest: f.queue.publishWorkQueueRequest,
          now: f.queue.at,
          verifyEffects: createClaimDeliveryVerifier({ assignment: f.assignment, recheck }),
        });
        assert.equal(result.claims.h1.state, "pending");
        assert.equal(result.claims.h2.state, "result");
        assert.equal(result.claims.h2.effects, "partial");
        assert.equal(f.queue.state.works.get(f.assignment.claims[1].work_id).barrier, "verified");
        assert.equal(writes, 1);
      } finally {
        fs.rmSync(root, { recursive: true, force: true });
      }
    });

    for (const transport of ["prepared", "native"])
      for (const test of [
        { type: "create_issue", fields: { title: "approved", body: "delivered", labels: ["approved"], assignees: ["bot"], milestone: 3 }, mismatch: "title", methods: ["create"], tamper: "labels", writes: 1 },
        { type: "update_issue", fields: { item_number: 42, title: "approved", body: "delivered" }, mismatch: "title", methods: ["update"], tamper: "title", writes: 1 },
        { type: "close_issue", fields: { item_number: 42, state_reason: "completed" }, mismatch: "state_reason", methods: ["update"], tamper: "state", writes: 1 },
        { type: "add_comment", fields: { item_number: 42, body: "delivered" }, mismatch: "body", methods: ["createComment"], tamper: "body", writes: 1 },
        { type: "add_labels", fields: { item_number: 42, labels: ["approved"] }, mismatch: "labels", methods: ["addLabels"], tamper: "labels", writes: 1 },
        { type: "remove_labels", fields: { item_number: 42, labels: ["removed"] }, mismatch: "labels", methods: ["removeLabel"], tamper: "labels", writes: 1 },
        { type: "replace_label", fields: { item_number: 42, label_to_add: "added", label_to_remove: "removed" }, mismatch: "label_to_add", methods: ["removeLabel", "addLabels"], tamper: "labels", writes: 2 },
      ]) {
        it(`verifies declared ${transport} ${test.type} through native readback while isolating sibling expectations`, async () => {
          const expected = test.fields;
          const outputType = transport === "prepared" ? "custom" : test.type;
          const contract = { version: 1, outputs: [{ type: outputType, min: 1, max: 1, verification: { verifier_id: transport === "prepared" ? "IssueUpdated.v1" : test.type, expected } }] };
          const f = fixture({
            workDefaults: { payload: { effect_contract: contract, resource_scope: deliveryResourceScope(test.type !== "create_issue") } },
            configurePolicy: policy => {
              policy.pools.default.reconciliation.max_attempts = 1;
            },
          });
          const adapter = {
            mode: "prepared",
            "effect-type": test.type,
            "target-repo": REPOSITORY,
            "verifier-id": "IssueUpdated.v1",
            "field-map": Object.fromEntries(Object.keys(expected).map(field => [field, field])),
          };
          const root = temporaryDirectory("declared-builtin-delivery");
          const authorize = request => authorizeWorkerClaim({ ...f.runtime, ...request, context: f.runtime.context, githubClient: f.runtime.githubClient });
          let writes = 0;
          /** @type {{id: number, number: number, title: string, body: string, state: string, labels: string[], assignees: {login: string}[], milestone: {number: number} | null, html_url: string, issue_url?: string}} */
          let observed = {
            id: test.type === "add_comment" ? 200 : 100,
            number: 42,
            title: "old",
            body: "old",
            state: "open",
            labels: ["removed"],
            assignees: [],
            milestone: null,
            html_url: `https://github.com/${REPOSITORY}/issues/42`,
            ...(test.type === "add_comment" ? { issue_url: `https://api.github.com/repos/${REPOSITORY}/issues/42` } : {}),
          };
          const parent = { id: 100, number: 42, title: "parent", body: "parent", html_url: `https://github.com/${REPOSITORY}/issues/42` };
          const mutate = async (method, parameters) => {
            assert.equal(parameters.owner, "owner");
            assert.equal(parameters.repo, "repo");
            if (method !== "create") assert.equal(parameters.issue_number, 42);
            writes++;
            if (method === "addLabels") {
              observed.labels = [...new Set([...observed.labels, ...parameters.labels])];
            } else if (method === "removeLabel") {
              observed.labels = observed.labels.filter(label => label !== parameters.name);
            } else {
              observed = { ...observed, ...Object.fromEntries(Object.entries(parameters).filter(([field]) => !["owner", "repo", "issue_number"].includes(field))) };
              if (parameters.assignees !== undefined) observed.assignees = parameters.assignees.map(login => ({ login }));
              if (parameters.milestone !== undefined) observed.milestone = parameters.milestone === null ? null : { number: parameters.milestone };
            }
            return { data: structuredClone(["addLabels", "removeLabel"].includes(method) ? observed.labels : observed) };
          };
          const github = {
            ...f.queue.githubClient,
            rest: {
              ...f.queue.githubClient.rest,
              issues: {
                get: async () => ({ data: structuredClone(test.type === "add_comment" ? parent : observed) }),
                getComment: async () => ({ data: structuredClone(observed) }),
                ...Object.fromEntries(test.methods.map(method => [method, parameters => mutate(method, parameters)])),
              },
            },
          };
          const verifyDeclaredOutput = createDeclaredAdapterVerifier({ custom: adapter });
          const recheck = async (member, record) =>
            withClaimExecution({ assignment: f.assignment, claim_handle: member.handle, authorize, effects: record.effects }, () =>
              inspectClaimDelivery({
                ...f.runtime,
                ...record,
                github,
                claim_handle: member.handle,
                authorize,
                verifyDeclaredOutput: transport === "prepared" ? verifyDeclaredOutput : createDeclaredAdapterVerifier({}, { [test.type]: {} }, record.effects),
                requireControlInventory: true,
                readControlInventory: () => readDeliveryControlInventory({ ...f.runtime, claim_handle: member.handle }),
              })
            );
          try {
            for (const member of f.assignment.claims) {
              const effects = [];
              const payload = { ...expected, ...(member.handle === "h1" ? { [test.mismatch]: test.mismatch === "labels" ? ["wrong"] : "wrong" } : {}) };
              const message = { type: outputType, claim_handle: member.handle, ...(transport === "native" ? { repo: REPOSITORY } : {}), ...payload };
              /** @type {{record: DeliveryProofRecord | null}} */
              const captured = { record: null };
              await withClaimExecution({ assignment: f.assignment, claim_handle: member.handle, authorize, effects }, async () => {
                const filename = preparedAdapterPath(root, member.handle, outputType);
                if (transport === "prepared") {
                  fs.mkdirSync(path.dirname(filename), { recursive: true });
                  fs.writeFileSync(filename, JSON.stringify({ version: 3, claim_handle: member.handle, type: outputType, messages: [{ input: message, payload }] }));
                }
                const client = wrapClaimEffectClient(github, { claim_handle: member.handle, authorize, context: f.queue.workerContext });
                const execute = async projected => {
                  let response;
                  for (const method of test.methods) {
                    const fields =
                      method === "removeLabel"
                        ? { name: projected.label_to_remove || projected.labels[0] }
                        : method === "addLabels"
                          ? { labels: projected.label_to_add ? [projected.label_to_add] : projected.labels }
                          : {
                              ...Object.fromEntries(
                                Object.keys(expected)
                                  .filter(field => field !== "item_number")
                                  .map(field => [field, projected[field]])
                              ),
                              ...(test.type === "close_issue" ? { state: "closed" } : {}),
                            };
                    response = await client.rest.issues[method]({ owner: "owner", repo: "repo", ...(projected.item_number === undefined ? {} : { issue_number: projected.item_number }), ...fields });
                  }
                  return { number: 42, repo: REPOSITORY, ...(test.type === "add_comment" ? { comment_id: response.data.id } : {}) };
                };
                const handler = transport === "prepared" ? await createClaimAdapterHandler({ adapter, filename, github: client, loadEffectHandler: async () => execute }) : wrapDeclaredBuiltinHandler(test.type, execute);
                if (member.handle === "h1") {
                  await assert.rejects(handler(message), /immutable declared verification expectation/);
                  assert.equal(writes, 0);
                  return;
                }
                const result = await handler(message);
                captured.record = { messages: [message], results: [{ messageIndex: 0, success: true, claim_handle: member.handle, result }], effects, effectChannel: closeClaimEffectChannel() };
              });
              const record = captured.record;
              if (!record) continue;
              assert.equal((await recheck(member, record)).verification, "verified");
              assert.equal(writes, test.writes);
              const delivered = structuredClone(observed);
              if (test.type === "create_issue") {
                observed = { ...observed, labels: [...observed.labels, "unexpected"] };
                assert.equal((await recheck(member, record)).verification, "unknown");
                observed = { ...delivered, assignees: [...delivered.assignees, { login: "unexpected" }] };
                assert.equal((await recheck(member, record)).verification, "unknown");
                observed = delivered;
              }
              observed = { ...observed, [test.tamper]: test.tamper === "labels" ? (test.type === "remove_labels" ? ["removed"] : []) : "tampered" };
              assert.equal((await recheck(member, record)).verification, "unknown");
              observed = delivered;
              record.results[0].result.number = 43;
              assert.equal((await recheck(member, record)).verification, "unknown");
              record.results[0].result.number = 42;
              const copied = { ...record, results: [{ ...record.results[0], result: structuredClone(record.results[0].result) }] };
              assert.equal((await recheck(member, copied)).verification, "unknown");
              assert.equal((await recheck(member, record)).verification, "verified");
              assert.equal(writes, test.writes);
              if (transport === "native") {
                const finalized = await finalizeWorkerResults({
                  ...f.runtime,
                  publishWorkQueueRequest: f.queue.publishWorkQueueRequest,
                  now: f.queue.at,
                  verifyEffects: createClaimDeliveryVerifier({ assignment: f.assignment, recheck: selected => (selected.handle === member.handle ? recheck(selected, record) : null) }),
                });
                assert.equal(finalized.claims.h1.state, "pending");
                assert.equal(finalized.claims.h2.state, "result");
                assert.equal(f.queue.state.works.get(member.work_id).barrier, "verified");
                assert.equal(writes, test.writes);
              }
            }
          } finally {
            fs.rmSync(root, { recursive: true, force: true });
          }
        });
      }

    it("rejects unsupported builtin fields and conflicting explicit target aliases", () => {
      const { builtinTargetNumber } = require("./work_queue_declared_verification.cjs");
      assert.equal(builtinTargetNumber({ item_number: 42, issue_number: "42" }), "42");
      for (const target of [{ item_number: 42, issue_number: 43 }, { item_number: null }, { issue_number: "042" }, { item_number: 1.5 }]) {
        assert.throws(() => builtinTargetNumber(target), /canonical|conflicting/);
      }
      for (const field of ["state", "item_number", "claim_handle"]) {
        assert.throws(
          () =>
            createDeclaredAdapterVerifier({
              custom: { mode: "prepared", "effect-type": "create_issue", "target-repo": REPOSITORY, "field-map": { [field]: "content" } },
            }),
          /Unknown trusted Claim adapter effect field/
        );
      }
    });

    it("gates the actual manager's ordinary native handler before writes and privately verifies its completed sibling", async () => {
      const expected = { issue_number: 42, state_reason: "completed" };
      const contract = { version: 1, outputs: [{ type: "close_issue", min: 1, max: 1, verification: { verifier_id: "close_issue", expected } }] };
      const f = fixture({ workDefaults: { payload: { effect_contract: contract, resource_scope: deliveryResourceScope(true) } } });
      const oldGithub = global.github;
      const oldCore = global.core;
      const oldContext = global.context;
      const config = { close_issue: { max: 5, "target-repo": REPOSITORY, issue_intent: false, allow_body: false, target: "*" } };
      let writes = 0;
      let observed = { id: 100, number: 42, title: "parent", body: "", state: "open", state_reason: null, labels: [], assignees: [], html_url: `https://github.com/${REPOSITORY}/issues/42` };
      const github = {
        ...f.queue.githubClient,
        rest: {
          ...f.queue.githubClient.rest,
          issues: {
            get: async () => ({ data: structuredClone(observed) }),
            update: async parameters => {
              writes++;
              observed = { ...observed, state: parameters.state, state_reason: parameters.state_reason };
              return { data: structuredClone(observed) };
            },
          },
        },
      };
      try {
        global.core = { ...oldCore, info() {}, debug() {}, warning() {}, error() {} };
        global.context = {
          ...oldContext,
          ...f.queue.workerContext,
          runId: Number(f.queue.workerContext.runId),
          payload: {
            ...f.queue.workerContext.payload,
            repository: { ...f.queue.workerContext.payload.repository, name: "repo", owner: { login: "owner" }, full_name: REPOSITORY },
          },
          get issue() {
            return { owner: "owner", repo: "repo", number: 42 };
          },
        };
        const manager = require("./safe_output_handler_manager.cjs");
        for (const member of f.assignment.claims) {
          const effects = [];
          await withClaimExecution({ assignment: f.assignment, claim_handle: member.handle, authorize: f.authorize, effects }, async () => {
            global.github = wrapClaimEffectClient(github, { claim_handle: member.handle, authorize: f.authorize, context: f.queue.workerContext });
            const handlers = await manager.loadHandlers(config, null);
            const handler = handlers.get("close_issue");
            if (typeof handler !== "function") throw new Error("Actual manager did not load the native close handler");
            const message = { type: "close_issue", claim_handle: member.handle, ...expected, ...(member.handle === "h1" ? { state_reason: "not_planned" } : {}) };
            if (member.handle === "h1") {
              await assert.rejects(handler(message), /immutable declared verification expectation/);
              assert.equal(writes, 0);
              return;
            }
            const result = await handler(message);
            assert.equal(result.success, true, JSON.stringify(result));
            const delivery = await inspectClaimDelivery({
              ...f.runtime,
              github,
              claim_handle: member.handle,
              authorize: f.authorize,
              messages: [message],
              results: [{ messageIndex: 0, success: true, claim_handle: member.handle, result }],
              effects,
              effectChannel: closeClaimEffectChannel(),
              verifyDeclaredOutput: createDeclaredAdapterVerifier({}, config, effects),
              requireControlInventory: true,
              readControlInventory: () => readDeliveryControlInventory({ ...f.runtime, claim_handle: member.handle }),
            });
            assert.equal(delivery.verification, "verified");
            assert.equal(delivery.descriptor.outputs[0].evidence.verifier_id, "close_issue");
            assert.equal(writes, 1);
          });
        }
      } finally {
        global.github = oldGithub;
        global.core = oldCore;
        global.context = oldContext;
      }
    });

    it("rejects native verifier aliases, partial/extra expectations and unbound configuration before handlers", async () => {
      for (const verification of [
        { verifier_id: "agent-alias", expected: { title: "approved", body: "delivered" } },
        { verifier_id: "create_issue", expected: { title: "approved" } },
        { verifier_id: "create_issue", expected: { title: "approved", body: "delivered", repo: REPOSITORY } },
      ]) {
        const f = fixture({ count: 1, workDefaults: { payload: { effect_contract: { version: 1, outputs: [{ type: "create_issue", min: 1, max: 1, verification }] } } } });
        let invoked = 0;
        await withClaimExecution({ assignment: f.assignment, claim_handle: "h1", authorize: f.authorize, effects: [] }, async () => {
          const handler = wrapDeclaredBuiltinHandler("create_issue", async () => {
            invoked++;
            return { number: 42 };
          });
          await assert.rejects(handler({ type: "create_issue", claim_handle: "h1", title: "approved", body: "delivered" }), /expectation|missing or unknown fields/);
          assert.equal(invoked, 0);
        });
      }
      const { isDeclaredAdapterVerifier } = require("./work_queue_claim_adapters.cjs");
      const intent = { verifier_id: "create_issue", expected: { title: "approved" } };
      /** @type {{create_issue?: object}} */
      const config = { create_issue: {} };
      const verifier = createDeclaredAdapterVerifier({}, config);
      delete config.create_issue;
      assert.equal(isDeclaredAdapterVerifier(verifier, "create_issue", intent), true);
      assert.equal(isDeclaredAdapterVerifier(createDeclaredAdapterVerifier({}, config), "create_issue", intent), false);
      assert.equal(
        isDeclaredAdapterVerifier(options => verifier(options), "create_issue", intent),
        false
      );
      assert.throws(() => createDeclaredAdapterVerifier({ custom: { mode: "prepared", "effect-type": "create_issue", "target-repo": REPOSITORY, "verifier-id": "create_issue" } }, { create_issue: {} }), /must be unique/);
    });

    it("does not let an empty registry, no-write type or arbitrary callback bypass declared verification", async () => {
      const contract = { version: 1, outputs: [{ type: "noop", min: 1, max: 1, verification: { verifier_id: "noop", expected: {} } }] };
      const f = fixture({ count: 1, workDefaults: { payload: { effect_contract: contract } } });
      const member = f.assignment.claims[0];
      for (const verifyDeclaredOutput of [createDeclaredAdapterVerifier(), async () => ({ verified: true, claim_handle: "h1", resource: {}, evidence: {} })]) {
        const delivery = await f.inspect(member, {
          messages: [{ type: "noop", claim_handle: member.handle }],
          results: [{ messageIndex: 0, success: true, claim_handle: member.handle, result: {} }],
          verifyDeclaredOutput,
        });
        assert.equal(delivery.verification, "unknown");
        assert.match(delivery.reason, /supported trusted verifier binding/);
      }
    });

    it("closes expected schemas over all fixed and mapped native fields without allowing verifier aliases or duplicate bindings", async () => {
      const { matchesDeclaredAdapterExpected } = require("./work_queue_declared_verification.cjs");
      const { isDeclaredAdapterVerifier } = require("./work_queue_claim_adapters.cjs");
      const adapter = {
        mode: "prepared",
        "effect-type": "github_rest",
        "target-repo": REPOSITORY,
        "verifier-id": "CheckCreated.v1",
        "field-map": { name: "check_name" },
        expected: { head_sha: REF },
        request: { method: "POST", route: "/repos/{owner}/{repo}/check-runs", permission: "checks" },
        verifier: { route: "/repos/{owner}/{repo}/check-runs/{receipt_id}", "resource-kind": "check_run", fields: { name: "name", head_sha: "head_sha" } },
      };
      const expected = { name: "approved", head_sha: REF };
      const intent = { verifier_id: "CheckCreated.v1", expected };
      assert.equal(matchesDeclaredAdapterExpected(adapter, expected, intent), true);
      assert.equal(matchesDeclaredAdapterExpected(adapter, { ...expected, name: "wrong" }, intent), false);
      for (const malformed of [{ name: "approved" }, { ...expected, repo: "foreign/repo" }, { ...expected, request: "POST https://foreign.example" }]) {
        assert.throws(() => matchesDeclaredAdapterExpected(adapter, expected, { ...intent, expected: malformed }), /missing or unknown fields/);
      }
      const verifier = createDeclaredAdapterVerifier({ custom: adapter });
      adapter["verifier-id"] = "agent-replaced";
      assert.equal(isDeclaredAdapterVerifier(verifier, "custom", intent), true);
      assert.equal(isDeclaredAdapterVerifier(verifier, "other", intent), false);
      assert.equal(isDeclaredAdapterVerifier(verifier, "custom", { ...intent, verifier_id: "agent-replaced" }), false);
      assert.equal(
        isDeclaredAdapterVerifier(input => verifier(input), "custom", intent),
        false
      );
      assert.deepEqual(await verifier({ message: { type: "custom" }, verification: { ...intent, verifier_id: "agent-replaced" } }), { verified: false });
      assert.throws(() => createDeclaredAdapterVerifier({ custom: adapter, other: adapter }), /must be unique/);
    });

    it("converts only lossless native identities while retaining closed canonical resource authority", () => {
      assert.deepEqual(canonicalResourceTarget({ kind: "issue", repository: REPOSITORY, number: 42, id: "100", url: "https://github.com/owner/repo/issues/42" }), {
        kind: "issue",
        repository: REPOSITORY,
        number: "42",
        resource_id: "100",
      });
      assert.equal(nativeDecimalIdentity("1".repeat(256)), "1".repeat(256));
      for (const identity of [0, -1, 1.5, Number.MAX_SAFE_INTEGER + 1, null, undefined, "0", "01", "+1", "1".repeat(257)]) {
        assert.throws(() => nativeDecimalIdentity(identity), /lossless positive native identity/);
      }
      assert.throws(() => canonicalResourceTarget({ repository: REPOSITORY, kind: "issue", id: "100", resource_id: "101" }), /conflicting native identities/);
      assert.throws(() => canonicalResourceTarget({ repository: REPOSITORY, arbitrary_scope: "trusted" }), /Unsupported Claim effect target field/);
    });

    it("resolves issue-backed Pull Request identity without borrowing its Issue ID or a foreign native parent", async () => {
      const resource = { repository: REPOSITORY, kind: "issue", number: 7 };
      const issue = { id: 701, number: 7, pull_request: { url: "https://api.github.com/repos/owner/repo/pulls/7" } };
      let pull = { id: 501, number: 7, base: { repo: { id: 7, full_name: REPOSITORY } } };
      const github = {
        rest: {
          pulls: {
            get: async parameters => {
              assert.deepEqual(parameters, { owner: "owner", repo: "repo", pull_number: 7 });
              return { data: pull };
            },
          },
          repos: { get: async () => ({ data: { id: 7, full_name: REPOSITORY } }) },
        },
      };
      assert.deepEqual(await resolveParentResourceTarget(github, resource, issue), { repository: REPOSITORY, kind: "pull_request", number: "7", resource_id: "501", repository_id: "7", host: "github.com" });
      await assert.rejects(resolveParentResourceTarget({ rest: {} }, resource, issue), /cannot be independently resolved/);
      await assert.rejects(resolveParentResourceTarget(github, resource, { ...issue, pull_request: { url: "https://api.github.com/repos/foreign/repo/pulls/7" } }), /cannot be independently resolved/);
      pull = { ...pull, number: 8 };
      await assert.rejects(resolveParentResourceTarget(github, resource, issue), /parent identity mismatch/);
      pull = { ...pull, number: 7, base: { repo: { id: 7, full_name: "foreign/repo" } } };
      await assert.rejects(resolveParentResourceTarget(github, resource, issue), /repository identity mismatch/);
      await assert.rejects(resolveParentResourceTarget(github, { ...resource, number: "9007199254740993" }, issue), /resource number is invalid/);
    });

    it("emits exact lifecycle proofs with distinct full-Claim receipt identities for identical no-effect descriptors", async () => {
      const f = fixture();
      const verifier = createClaimDeliveryVerifier({ assignment: f.queue.assignment, recheck: f.inspect });
      const [first, second] = await Promise.all(f.assignment.claims.map(member => verifier(member, f.verification(member))));
      for (const proof of [first, second]) {
        assert.equal(proof.verified, true);
        assert.equal(proof.contractVerified, true);
        assert.equal(proof.effects, "none");
        assert.equal(proof.controls_digest, require("./work_queue_codec.cjs").digest([]));
        assert.match(proof.receipt, /^[a-f0-9]{64}$/);
        assert.deepEqual(proof.descriptor, { version: 1, outputs: [] });
      }
      assert.notEqual(first.receipt, second.receipt);
    });

    it("binds delivery to the actual launch principal without equating it with logical dispatcher origin", async () => {
      const f = fixture({ workerPrincipal: "22" });
      const member = f.assignment.claims[0];
      const dispatch = f.queue.state.dispatches.get(f.assignment.dispatch_id);
      assert.equal(dispatch.sender.principal, "11");
      assert.equal(dispatch.run.principal, "22");
      const verifier = createClaimDeliveryVerifier({ assignment: f.assignment, recheck: f.inspect });
      assert.equal((await verifier(member, f.verification(member))).verified, true);
      const senderRun = { ...f.verification(member), run: { ...f.queue.binding, principal: "11" } };
      assert.deepEqual(await verifier(member, senderRun), { verified: false, effects: "unknown" });
    });

    it("does not replace an explicitly invalid authority target with rich receipt metadata", async () => {
      const f = fixture({ count: 1, workDefaults: { payload: { effect_contract: { version: 1, outputs: [{ type: "create_issue", min: 1, max: 1 }] } } } });
      const member = f.assignment.claims[0];
      await assert.rejects(
        f.inspect(member, {
          messages: [{ type: "create_issue", claim_handle: member.handle, repo: REPOSITORY }],
          results: [{ messageIndex: 0, success: true, claim_handle: member.handle, result: {} }],
          verifyOutput: async () => ({
            verified: true,
            claim_handle: member.handle,
            resource: { kind: "issue", repository: REPOSITORY, number: 42, id: "100" },
            authority_resource: null,
            evidence: { source: "test_host" },
          }),
        }),
        /independently resolved resource/
      );
    });

    it("rejects serialized or fabricated successful receipts and unprotected write-channel assertions", async () => {
      const f = fixture();
      const member = f.assignment.claims[0];
      const delivery = await f.inspect(member);
      assert.deepEqual(await verifyClaimDelivery(member, f.verification(member)), { verified: false, effects: "unknown" });
      for (const untrusted of [JSON.parse(JSON.stringify(delivery)), { verification: "verified", jobSuccess: true, contractVerified: true, descriptor: delivery.descriptor }]) {
        const verifier = createClaimDeliveryVerifier({ assignment: f.queue.assignment, recheck: async () => untrusted });
        assert.deepEqual(await verifier(member, f.verification(member)), { verified: false, effects: "unknown" });
      }
      for (const effectChannel of [undefined, {}]) {
        const verifier = createClaimDeliveryVerifier({ assignment: f.queue.assignment, recheck: candidate => f.inspect(candidate, { effectChannel }) });
        assert.deepEqual(await verifier(member, f.verification(member)), { verified: false, effects: "unknown" });
      }
    });

    it("attests only unchanged successful in-memory lifecycle proof objects for their original assignment and Claim", async () => {
      const f = fixture();
      const member = f.assignment.claims[0];
      const verifier = createClaimDeliveryVerifier({ assignment: f.assignment, recheck: f.inspect });
      const proof = await verifier(member, f.verification(member));
      assert.equal(isTrustedClaimDelivery(proof, f.assignment, "h1"), true);
      assert.equal(isTrustedClaimDelivery(proof, JSON.parse(JSON.stringify(f.assignment)), "h1"), true);
      for (const copied of [JSON.parse(JSON.stringify(proof)), { ...proof }, { verification: "verified", ...proof }, null]) {
        assert.equal(isTrustedClaimDelivery(copied, f.assignment, "h1"), false);
      }
      assert.equal(isTrustedClaimDelivery(proof, f.assignment, "h2"), false);
      assert.equal(isTrustedClaimDelivery(proof, { ...f.assignment, dispatch_id: "foreign" }, "h1"), false);
      assert.equal(isTrustedClaimDelivery(proof, null, "h1"), false);
      const raw = await f.inspect(member);
      assert.equal(isTrustedClaimDelivery(raw, f.assignment, "h1"), false);
      const receipt = proof.receipt;
      proof.receipt = "agent-replaced-receipt";
      assert.equal(isTrustedClaimDelivery(proof, f.assignment, "h1"), false);
      proof.receipt = receipt;
      proof.descriptor.outputs.push({ type: "invented" });
      assert.equal(isTrustedClaimDelivery(proof, f.assignment, "h1"), false);
      proof.descriptor.outputs.pop();
      assert.equal(isTrustedClaimDelivery(proof, f.assignment, "h1"), true);
      proof.descriptor.cycle = proof;
      assert.equal(isTrustedClaimDelivery(proof, f.assignment, "h1"), false);
      Object.defineProperty(proof, "descriptor", {
        get: () => {
          throw new Error("tampered accessor");
        },
      });
      assert.equal(isTrustedClaimDelivery(proof, f.assignment, "h1"), false);
      const unknown = await verifyClaimDelivery(member, f.verification(member));
      assert.equal(isTrustedClaimDelivery(unknown, f.assignment, "h1"), false);
    });

    it("requires the complete immutable member, contract, assignment and exact independently proved bound run", async () => {
      const f = fixture();
      const member = f.assignment.claims[0];
      const verifier = createClaimDeliveryVerifier({ assignment: f.queue.assignment, recheck: f.inspect });
      const good = f.verification(member);
      for (const verification of [
        { ...good, attempt: 0 },
        { ...good, contract: { kind: "unproved" } },
        { ...good, assignment: { ...good.assignment, dispatch_id: "foreign" } },
        { ...good, run: { ...good.run, run_id: "99" } },
        { ...good, run: { ...good.run, run_attempt: 2 } },
        { ...good, run: { ...good.run, principal: "99" } },
        { ...good, run: { ...good.run, ref: "b".repeat(40) } },
      ])
        assert.deepEqual(await verifier(member, verification), { verified: false, effects: "unknown" });
      assert.deepEqual(await verifier({ ...member, work_id: "foreign" }, good), { verified: false, effects: "unknown" });
    });

    it("never turns missing contracts, delegated, skipped, unresolved or failed outcomes into verified no effects", async () => {
      for (const outcome of [{ delegated: true }, { skipped: true }, { deferred: true }, { success: false }, {}]) {
        const f = fixture({ workDefaults: { payload: { effect_contract: { version: 1, outputs: [{ type: "create_issue", min: 1, max: 1 }] } } } });
        const member = f.assignment.claims[0];
        const verifier = createClaimDeliveryVerifier({
          assignment: f.queue.assignment,
          recheck: candidate => f.inspect(candidate, { messages: [{ type: "create_issue", claim_handle: candidate.handle }], results: [{ messageIndex: 0, ...outcome }] }),
        });
        assert.deepEqual(await verifier(member, f.verification(member)), { verified: false, effects: "unknown" });
      }
      const absent = fixture({ workDefaults: { payload: { plan: "no declared contract" } } });
      const member = absent.assignment.claims[0];
      const verifier = createClaimDeliveryVerifier({ assignment: absent.queue.assignment, recheck: absent.inspect });
      assert.deepEqual(await verifier(member, absent.verification(member)), { verified: false, effects: "unknown" });
      const optional = fixture({ workDefaults: { payload: { effect_contract: { version: 1, outputs: [{ type: "create_issue", min: 0, max: 1 }] } } } });
      const optionalMember = optional.assignment.claims[0];
      const optionalVerifier = createClaimDeliveryVerifier({ assignment: optional.queue.assignment, recheck: optional.inspect });
      assert.deepEqual(await optionalVerifier(optionalMember, optional.verification(optionalMember)), { verified: false, effects: "unknown" });
    });

    it("proves actual declared resource identity and destination without executing or replaying effects", async () => {
      const f = fixture({ workDefaults: { payload: { effect_contract: { version: 1, outputs: [{ type: "create_issue", min: 1, max: 1 }] }, resource_scope: deliveryResourceScope() } } });
      const member = f.assignment.claims[0];
      let reads = 0;
      f.queue.githubClient.rest.issues = {
        get: async parameters => {
          reads++;
          assert.deepEqual(parameters, { owner: "owner", repo: "repo", issue_number: 42 });
          return { data: { id: 100, number: 42, title: "delivered", html_url: "https://github.com/owner/repo/issues/42" } };
        },
        create: async () => {
          throw new Error("Receipt verification must not execute effects");
        },
      };
      const verifier = createClaimDeliveryVerifier({
        assignment: f.queue.assignment,
        recheck: candidate => {
          const effects = [];
          return f.inspect(candidate, {
            effects,
            record: () => recordClaimEffect({ kind: "issue", id: "100", number: 42, repository: REPOSITORY, outcome: "succeeded", expected: { title: "delivered" } }),
            messages: [{ type: "create_issue", claim_handle: candidate.handle, repo: REPOSITORY, title: "delivered" }],
            results: [{ messageIndex: 0, success: true, claim_handle: candidate.handle, result: { number: 42, repo: REPOSITORY } }],
            verifyOutput: input => verifyBuiltinDeliveryOutput({ ...input, effects }),
          });
        },
      });
      const proof = await verifier(member, f.verification(member));
      assert.equal(proof.verified, true);
      assert.equal(proof.effects, "partial");
      assert.deepEqual(proof.descriptor.outputs[0].resource, { kind: "issue", repository: REPOSITORY, number: 42, id: "100", url: "https://github.com/owner/repo/issues/42" });
      assert.equal(reads, 1);
    });

    it("does not read a foreign handler-receipt repository before independently scoped delivery authorization", async () => {
      const f = fixture({ count: 1, workDefaults: { payload: { effect_contract: { version: 1, outputs: [{ type: "create_issue", min: 1, max: 1 }] } } } });
      const member = f.assignment.claims[0];
      let reads = 0;
      f.queue.githubClient.rest.issues = {
        get: async () => {
          reads++;
          throw new Error("A foreign result cannot borrow the worker's read credential");
        },
      };
      await assert.rejects(
        f.inspect(member, {
          messages: [{ type: "create_issue", claim_handle: member.handle, repo: REPOSITORY }],
          results: [{ messageIndex: 0, success: true, claim_handle: member.handle, result: { repo: "foreign/repo", number: 42 } }],
          verifyOutput: verifyBuiltinDeliveryOutput,
        }),
        /work_queue_effect_scope_denied/
      );
      assert.equal(reads, 0);
    });

    it("resolves full native subject identities before effects and verifies canonical resources independently of rich receipt metadata", async () => {
      const subject = { kind: "issue", host: "github.com", repository: REPOSITORY, repository_id: "7", resource_id: "100", number: "42" };
      const f = fixture({
        count: 1,
        workDefaults: { subject, payload: { resource_scope: { version: 1, resources: [subject] }, effect_contract: { version: 1, outputs: [{ type: "update_issue", min: 1, max: 1 }] } } },
      });
      const member = f.assignment.claims[0];
      let writes = 0;
      let nativeId = 100;
      f.queue.githubClient.rest.issues = {
        get: async ({ issue_number }) => ({ data: { id: nativeId, number: issue_number, title: "delivered", html_url: "https://github.com/owner/repo/issues/42" } }),
        update: async () => {
          writes++;
          return { data: { id: nativeId, number: 42 } };
        },
      };
      const effects = [];
      let result;
      const message = { type: "update_issue", claim_handle: member.handle, repo: REPOSITORY, item_number: 42, title: "delivered" };
      await withClaimExecution({ assignment: f.assignment, claim_handle: member.handle, effects, authorize: f.authorize }, async () => {
        const client = wrapClaimEffectClient(f.queue.githubClient, { claim_handle: member.handle, authorize: f.authorize, context: f.queue.workerContext });
        nativeId = 101;
        await assert.rejects(client.rest.issues.update({ owner: "owner", repo: "repo", issue_number: 42, title: "delivered" }), /claim_scope_invalid/);
        assert.equal(writes, 0);
        nativeId = 100;
        result = await client.rest.issues.update({ owner: "owner", repo: "repo", issue_number: 42, title: "delivered" });
      });
      assert.equal(writes, 1);
      const verifier = createClaimDeliveryVerifier({
        assignment: f.assignment,
        recheck: candidate =>
          f.inspect(candidate, {
            effects,
            messages: [message],
            results: [{ messageIndex: 0, success: true, claim_handle: member.handle, result: { number: result.data.number, repo: REPOSITORY } }],
            verifyOutput: input => verifyBuiltinDeliveryOutput({ ...input, effects }),
          }),
      });
      const proof = await verifier(member, f.verification(member));
      assert.equal(proof.verified, true);
      assert.equal(proof.descriptor.outputs[0].resource.id, "100");
      assert.equal(proof.descriptor.outputs[0].resource.number, 42);
      assert.equal(writes, 1);
      nativeId = 101;
      await assert.rejects(verifier(member, f.verification(member)), /claim_scope_invalid/);
      assert.equal(writes, 1);
    });

    it("verifies issue-backed Pull Request effects against the frozen PR identity while retaining Issue API receipt metadata", async () => {
      const subject = { kind: "pull_request", host: "github.com", repository: REPOSITORY, repository_id: "7", resource_id: "501", number: "7" };
      const f = fixture({ count: 1, workDefaults: { subject, payload: { resource_scope: { version: 1, resources: [subject] }, effect_contract: { version: 1, outputs: [{ type: "update_issue", min: 1, max: 1 }] } } } });
      const member = f.assignment.claims[0];
      let nativeId = 501;
      let writes = 0;
      const issue = { id: 701, number: 7, title: "delivered", html_url: "https://github.com/owner/repo/pull/7", pull_request: { url: "https://api.github.com/repos/owner/repo/pulls/7" } };
      f.queue.githubClient.rest.issues = {
        get: async () => ({ data: issue }),
        update: async () => {
          writes++;
          return { data: issue };
        },
      };
      f.queue.githubClient.rest.pulls = { get: async () => ({ data: { id: nativeId, number: 7, base: { repo: { id: 7, full_name: REPOSITORY } } } }) };
      const effects = [];
      const message = { type: "update_issue", claim_handle: member.handle, repo: REPOSITORY, item_number: 7, title: "delivered" };
      await withClaimExecution({ assignment: f.assignment, claim_handle: member.handle, effects, authorize: f.authorize }, async () => {
        const client = wrapClaimEffectClient(f.queue.githubClient, { claim_handle: member.handle, authorize: f.authorize, context: f.queue.workerContext });
        nativeId = 502;
        await assert.rejects(client.rest.issues.update({ owner: "owner", repo: "repo", issue_number: 7, title: "delivered" }), /claim_scope_invalid/);
        assert.equal(writes, 0);
        nativeId = 501;
        await client.rest.issues.update({ owner: "owner", repo: "repo", issue_number: 7, title: "delivered" });
      });
      const verifier = createClaimDeliveryVerifier({
        assignment: f.assignment,
        recheck: candidate =>
          f.inspect(candidate, {
            effects,
            messages: [message],
            results: [{ messageIndex: 0, success: true, claim_handle: member.handle, result: { number: 7, repo: REPOSITORY } }],
            verifyOutput: input => verifyBuiltinDeliveryOutput({ ...input, effects }),
          }),
      });
      const proof = await verifier(member, f.verification(member));
      assert.equal(proof.verified, true);
      assert.equal(proof.descriptor.outputs[0].resource.id, "701");
      assert.equal(proof.descriptor.outputs[0].resource.kind, "issue");
      assert.equal(writes, 1);
      nativeId = 502;
      await assert.rejects(verifier(member, f.verification(member)), /claim_scope_invalid/);
      assert.equal(writes, 1);
    });

    it("closes the write channel before proof and detects later mutations of effect accounting", async () => {
      const f = fixture({ workDefaults: { payload: { effect_contract: { version: 1, outputs: [{ type: "create_issue", min: 0, max: 1 }] } } } });
      const member = f.assignment.claims[0];
      const effects = [];
      let writes = 0;
      await withClaimExecution({ assignment: f.queue.assignment, claim_handle: member.handle, effects, authorize: f.authorize }, async () => {
        const channel = closeClaimEffectChannel();
        assert.equal(claimEffectChannelMatches(channel, effects, f.queue.assignment, member.handle), true);
        assert.equal(claimEffectChannelMatches(channel, effects, f.queue.assignment, "h2"), false);
        assert.throws(() => recordClaimEffect({ outcome: "succeeded" }), /closed/);
        await assert.rejects(assertClaimAuthorized({ type: "create_issue", claim_handle: member.handle }, { effect: true }), /closed/);
        const client = wrapClaimEffectClient(
          {
            rest: {
              issues: {
                create: async () => {
                  writes++;
                },
              },
            },
          },
          { claim_handle: member.handle, authorize: f.authorize }
        );
        await assert.rejects(client.rest.issues.create({ owner: "owner", repo: "repo" }), /closed/);
        effects.push({ outcome: "succeeded" });
        assert.equal(claimEffectChannelMatches(channel, effects, f.queue.assignment, member.handle), false);
      });
      assert.equal(writes, 0);
    });

    it("lets a valid sibling settle when one Claim readback or normalizer fails", async () => {
      const f = fixture({
        configurePolicy: policy => {
          policy.pools.default.reconciliation.max_attempts = 1;
        },
      });
      const before = f.queue.transactions.length;
      const result = await finalizeWorkerResults({
        ...f.runtime,
        publishWorkQueueRequest: f.queue.publishWorkQueueRequest,
        now: f.queue.at,
        verifyEffects: createClaimDeliveryVerifier({
          assignment: f.queue.assignment,
          recheck: member => {
            if (member.handle === "h1") throw new Error("h1 scope/readback error");
            return f.inspect(member);
          },
        }),
      });
      assert.equal(result.claims.h1.state, "pending");
      assert.equal(result.claims.h2.state, "result");
      assert.equal(result.claims.h2.effects, "none");
      assert.equal(f.queue.transactions.length, before + 1);
    });

    it("ignores aborted late manager readback without replacing a sibling's global clients", async () => {
      const f = fixture();
      const manager = require("./safe_output_handler_manager.cjs");
      const root = temporaryDirectory("delivery-abort");
      const controller = new AbortController();
      const originalGithub = global.github;
      const originalFactory = global.getOctokit;
      /** @type {{release?: () => void, entered?: () => void}} */
      const callbacks = {};
      /** @type {Promise<void>} */
      const wait = new Promise(resolve => {
        callbacks.release = resolve;
      });
      /** @type {Promise<void>} */
      const started = new Promise(resolve => {
        callbacks.entered = resolve;
      });
      assert.ok(callbacks.release);
      assert.ok(callbacks.entered);
      const { release, entered } = callbacks;
      const verifier = createClaimDeliveryVerifier({
        assignment: f.assignment,
        recheck: (member, verification) => {
          const effects = [];
          return withClaimExecution({ assignment: f.assignment, claim_handle: member.handle, effects, authorize: f.authorize }, () => {
            const localGithub = wrapClaimEffectClient(f.queue.githubClient, { claim_handle: member.handle, authorize: f.authorize });
            return manager.settleClaimDelivery({ assignment: f.assignment }, [], [], {
              github: localGithub,
              context: f.queue.workerContext,
              authorize: f.authorize,
              effects,
              signal: verification.signal,
              deliveryArtifactRoot: root,
              readControlInventory: async () => {
                if (member.handle === "h1") {
                  entered();
                  await wait;
                }
                return readDeliveryControlInventory({ ...f.runtime, claim_handle: member.handle, signal: verification.signal });
              },
            });
          });
        },
      });
      const first = verifier(f.assignment.claims[0], { ...f.verification(f.assignment.claims[0]), signal: controller.signal });
      const firstRejected = assert.rejects(first, /abort/i);
      try {
        await started;
        controller.abort();
        assert.equal(global.github, originalGithub);
        assert.equal(global.getOctokit, originalFactory);
        const second = await verifier(f.assignment.claims[1], f.verification(f.assignment.claims[1]));
        assert.equal(second.verified, true);
        assert.equal(global.github, originalGithub);
        assert.equal(global.getOctokit, originalFactory);
        release();
        await firstRejected;
        assert.equal(global.github, originalGithub);
        assert.equal(global.getOctokit, originalFactory);
      } finally {
        release();
        await firstRejected;
        fs.rmSync(root, { recursive: true, force: true });
      }
    });

    it("forwards cancellation to REST, generic GET, GraphQL and paginated read-only API requests", async () => {
      const f = fixture();
      const controller = new AbortController();
      const calls = [];
      const checked = async (kind, parameters) => {
        assert.equal(parameters.request.signal, controller.signal);
        calls.push(kind);
        return {};
      };
      const source = {
        rest: { issues: { get: parameters => checked("rest", parameters) } },
        request: (_route, parameters) => checked("request", parameters),
        graphql: (_query, parameters) => checked("graphql", parameters),
        paginate: (_route, parameters) => checked("paginate", parameters),
      };
      await withClaimExecution({ assignment: f.assignment, claim_handle: "h1", effects: [], authorize: f.authorize }, async () => {
        closeClaimEffectChannel();
        const client = wrapClaimEffectClient(source, { claim_handle: "h1", signal: controller.signal, authorize: f.authorize });
        const parameters = { owner: "owner", repo: "repo", request: { timeout: 1000 } };
        await client.rest.issues.get(parameters);
        await client.request("GET /repos/owner/repo/issues", parameters);
        await client.graphql("query { viewer { id } }", parameters);
        await client.paginate("GET /repos/owner/repo/issues", parameters);
        assert.deepEqual(parameters.request, { timeout: 1000 });
        controller.abort();
        assert.throws(() => client.rest.issues.get(parameters), /abort/i);
      });
      assert.deepEqual(calls, ["rest", "request", "graphql", "paginate"]);
    });
  });
}

module.exports = { registerTests };
if (require.main === module) registerTests(require("node:test"));
