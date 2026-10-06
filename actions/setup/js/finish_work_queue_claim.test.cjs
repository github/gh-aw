// @ts-check
import { afterEach, describe, expect, it, vi } from "vitest";
import fs from "fs";
import path from "path";
import { randomUUID } from "crypto";
import { authorizeWorkerClaim, finalizeWorkerResults, main, readFinishIntent, reconcileWorkerClaim, verifyWithinBudget } from "./finish_work_queue_claim.cjs";
import { verifyClaimDelivery } from "./work_queue_delivery.cjs";
import { digest } from "./work_queue_codec.cjs";
import { claimControlReceipts } from "./work_queue_control_receipts.cjs";
import { nodeId } from "./work_queue_graph.cjs";
import { queueFixture, REF, REPOSITORY, WORKFLOW } from "./work_queue_lifecycle.test_helpers.cjs";

const directories = [];
function setup(count = 3, fixtureOptions = {}) {
  const fixture = queueFixture({ bound: true, count, ...fixtureOptions });
  const directory = path.join(process.cwd(), `.queue-finish-test-${randomUUID()}`);
  fs.mkdirSync(directory);
  directories.push(directory);
  const options = {
    assignment: fixture.assignment,
    githubClient: fixture.githubClient,
    context: fixture.workerContext,
    workflowRef: `${REPOSITORY}/${WORKFLOW}@${REF}`,
    finishIntentPath: path.join(directory, "finish.jsonl"),
    readWorkQueueLog: fixture.readWorkQueueLog,
    publishWorkQueueRequest: fixture.publishWorkQueueRequest,
    now: fixture.at,
    sleepFn: async () => {},
  };
  return { fixture, options };
}
function finish(options, handle, outcome, id = `${handle}:${outcome}`) {
  fs.appendFileSync(options.finishIntentPath, `${JSON.stringify({ version: 3, kind: "finish", intent_id: id, parameters: { outcome }, ...(handle === undefined ? {} : { claim_handle: handle }) })}\n`);
}
afterEach(() => {
  for (const directory of directories.splice(0)) fs.rmSync(directory, { recursive: true, force: true });
});

describe("independent trusted Claim finalization", () => {
  function publishChild(fixture, member, label) {
    const template = fixture.transactions[1].operations[0];
    return fixture.append(
      "submit",
      { nodes: [{ ...template, graph_id: label, node_key: "child", work_id: nodeId(label, "child") }] },
      { ...fixture.workerActor, dispatch_id: fixture.assignment.dispatch_id, claim_handle: member.handle },
      `control:${label}`
    );
  }

  const unsupportedPayloads = [{ plan: "missing contract" }, { output_contract: { kind: "none" } }, { effect_contract: { version: 2, outputs: [], no_writes: true } }, { effect_contract: { version: 1, outputs: [] } }];
  it.each(unsupportedPayloads)("keeps unsupported immutable contracts unknown despite a positive callback: %j", async payload => {
    const { fixture, options } = setup(1, { workDefaults: { payload } });
    finish(options, "h1", "completed");
    await reconcileWorkerClaim(options);
    const verifier = vi.fn().mockResolvedValue({ verified: true, contractVerified: true, receipt: "overoptimistic-callback", descriptor: { outputs: [] }, effects: "none" });
    const result = await finalizeWorkerResults({ ...options, verifyEffects: verifier });
    expect(result.claims.h1).toMatchObject({ state: "pending", effects: "unknown", reason: "effect_contract_invalid" });
    expect(verifier).toHaveBeenCalledTimes(fixture.policy.pools.default.reconciliation.max_attempts);
    expect(verifier.mock.calls[0][1].contract).toEqual(payload.effect_contract);
    expect(fixture.state.works.get(fixture.assignment.claims[0].work_id).barrier).toBe("pending");
    expect(fixture.transactions.flatMap(commit => commit.operations).some(operation => ["Result", "DeliveryFailure"].includes(operation.kind))).toBe(false);
  });

  it.each(unsupportedPayloads)("requires terminal evidence and real verification exhaustion for unsupported-contract failure: %j", async payload => {
    const { fixture, options } = setup(1, { workDefaults: { payload } });
    finish(options, "h1", "completed");
    await reconcileWorkerClaim(options);
    const original = { ...fixture.nativeRun(fixture.workerContext), status: "completed", conclusion: "failure" };
    fixture.githubClient.rest.actions.getWorkflowRunAttempt = vi.fn().mockResolvedValue({ status: 200, data: original });
    const verifier = vi.fn().mockResolvedValue({ verified: true, contractVerified: true, receipt: "overoptimistic-callback", descriptor: { outputs: [] }, effects: "none" });
    const result = await finalizeWorkerResults({ ...options, verifyEffects: verifier });
    expect(result.claims.h1).toMatchObject({ state: "delivery_failed", effects: "unknown" });
    expect(verifier).toHaveBeenCalledTimes(fixture.policy.pools.default.reconciliation.max_attempts);
    const operations = fixture.transactions.flatMap(commit => commit.operations);
    expect(operations.some(operation => operation.kind === "Result")).toBe(false);
    expect(operations.find(operation => operation.kind === "DeliveryFailure")).toMatchObject({ disposition: "unknown", evidence: { attempts: fixture.policy.pools.default.reconciliation.max_attempts, run_attempt: 1 } });
  });

  it("cannot settle no-write proof over unverified durable queue controls or suppress a valid sibling", async () => {
    const { fixture, options } = setup(2);
    finish(options, "h1", "completed");
    finish(options, "h2", "completed");
    await reconcileWorkerClaim(options);
    publishChild(fixture, fixture.assignment.claims[0], "undeclared-control");
    const result = await finalizeWorkerResults({ ...options, verifyEffects: async () => ({ verified: true, contractVerified: true, receipt: "legacy-no-write", descriptor: { outputs: [] }, effects: "none" }), now: fixture.at });
    expect(result.claims.h1.state).toBe("pending");
    expect(result.claims.h2.state).toBe("result");
    expect(fixture.state.works.get(fixture.assignment.claims[0].work_id).barrier).toBe("pending");
    expect(fixture.state.works.get(fixture.assignment.claims[1].work_id).barrier).toBe("verified");
  });

  it("fences complete queue-control inventory on every Result CAS prefix", async () => {
    const { fixture, options } = setup(1);
    finish(options, "h1", "completed");
    await reconcileWorkerClaim(options);
    const member = fixture.assignment.claims[0];
    const publish = options.publishWorkQueueRequest;
    let inserted = false;
    options.publishWorkQueueRequest = async args => {
      if (args.request.kind === "result" && !inserted) {
        inserted = true;
        publishChild(fixture, member, "concurrent-control");
      }
      return publish(args);
    };
    const result = await finalizeWorkerResults({
      ...options,
      verifyEffects: async () => ({
        verified: true,
        contractVerified: true,
        receipt: "no-write-inventory",
        descriptor: { outputs: [] },
        effects: "none",
        controls_digest: digest(claimControlReceipts(fixture.state, fixture.assignment, member.handle)),
      }),
      now: fixture.at,
    });
    expect(inserted).toBe(true);
    expect(result.claims.h1).toMatchObject({ state: "pending", effects: "unknown" });
    expect(fixture.state.works.get(member.work_id).barrier).toBe("pending");
    expect(fixture.transactions.flatMap(commit => commit.operations).some(operation => operation.kind === "Result")).toBe(false);
  });

  it("rechecks native evidence after verification before publishing any Result", async () => {
    const { fixture, options } = setup(1);
    finish(options, "h1", "completed");
    await reconcileWorkerClaim(options);
    let changed = false;
    const get = fixture.githubClient.rest.actions.getWorkflowRun;
    fixture.githubClient.rest.actions.getWorkflowRun = async parameters => {
      const response = await get(parameters);
      return changed ? { ...response, data: { ...response.data, actor: { id: "22" } } } : response;
    };
    const verifier = vi.fn(async () => {
      changed = true;
      return { verified: true, contractVerified: true, receipt: "trusted-receipt", descriptor: { ok: true }, effects: "none" };
    });
    const result = await finalizeWorkerResults({ ...options, verifyEffects: verifier, now: fixture.at });
    expect(result.claims.h1).toMatchObject({ state: "pending", effects: "unknown", reason: "verification_unresolved" });
    expect(verifier).toHaveBeenCalledTimes(1);
    expect(fixture.state.works.get(fixture.assignment.claims[0].work_id).barrier).toBe("pending");
    expect(fixture.transactions.flatMap(commit => commit.operations).some(operation => operation.kind === "Result")).toBe(false);
  });

  it("recovers a concurrently settled Claim without replaying it or suppressing a valid sibling Result", async () => {
    const { fixture, options } = setup(2);
    finish(options, "h1", "completed");
    finish(options, "h2", "completed");
    await reconcileWorkerClaim(options);
    const verifier = vi.fn(async member => {
      if (member.handle === "h1") {
        const completion = fixture.state.works.get(member.work_id).completion_id;
        fixture.append(
          "result",
          {
            operations: [
              {
                kind: "Result",
                work_id: member.work_id,
                claim_id: member.claim_id,
                completion_id: completion,
                descriptor: { owner: "other-trusted-verifier" },
                evidence: {
                  kind: "delivery",
                  source: "verified_receipts",
                  repository: REPOSITORY,
                  workflow: WORKFLOW,
                  ref: REF,
                  principal: "11",
                  checked_at: fixture.at,
                  run_id: "42",
                  run_attempt: 1,
                  receipt: "other-verified-receipt",
                  effects: "none",
                },
              },
            ],
          },
          { role: "reconciler", principal: "11", repository: REPOSITORY }
        );
      }
      return { verified: true, contractVerified: true, receipt: `verified-${member.handle}`, descriptor: { owner: member.handle }, effects: "none" };
    });
    const result = await finalizeWorkerResults({ ...options, verifyEffects: verifier, now: fixture.at });
    expect(result.claims.h1).toMatchObject({ state: "result", effects: "none" });
    expect(result.claims.h2).toMatchObject({ state: "result", effects: "none" });
    expect(verifier).toHaveBeenCalledTimes(2);
    const facts = fixture.transactions.flatMap(commit => commit.operations).filter(operation => operation.kind === "Result");
    expect(facts).toHaveLength(2);
    expect(facts.find(operation => operation.claim_id === fixture.assignment.claims[0].claim_id).descriptor).toEqual({ owner: "other-trusted-verifier" });
  });

  it("integrates independently verified in-memory delivery through the trusted callback without reopening settled siblings", async () => {
    const { fixture, options } = setup(2);
    finish(options, "h1", "completed");
    finish(options, "h2", "cancelled");
    await reconcileWorkerClaim(options);
    const delivery = await verifyClaimDelivery({ ...options, claim_handle: "h1", authorize: request => authorizeWorkerClaim({ ...options, ...request }) });
    expect(delivery.verification).toBe("verified");
    const verifier = vi.fn(async (member, scope) => {
      expect(member.handle).toBe("h1");
      expect(scope.run.run_id).toBe("42");
      expect(delivery.claim_id).toBe(member.claim_id);
      expect(delivery.dispatch_id).toBe(scope.assignment.dispatch_id);
      return { verified: true, contractVerified: true, receipt: digest(delivery.descriptor), descriptor: delivery.descriptor, effects: delivery.disposition === "none" ? "none" : "partial" };
    });
    const get = fixture.githubClient.rest.actions.getWorkflowRun;
    fixture.githubClient.rest.actions.getWorkflowRun = async request => ({ status: 200, data: { ...(await get(request)).data, run_attempt: 2 } });
    await expect(finalizeWorkerResults({ ...options, verifyEffects: verifier })).rejects.toThrow(/rerun|attempt/);
    await expect(finalizeWorkerResults({ ...options, context: { ...options.context, runAttempt: 2 }, verifyEffects: verifier })).rejects.toThrow(/rerun/);
    expect(verifier).not.toHaveBeenCalled();
    fixture.githubClient.rest.actions.getWorkflowRun = get;
    const result = await finalizeWorkerResults({ ...options, verifyEffects: verifier, now: fixture.at });
    expect(result.claims.h1).toMatchObject({ state: "result", effects: "none" });
    expect(result.claims.h2).toMatchObject({ state: "cancelled" });
    const before = fixture.transactions.length;
    await finalizeWorkerResults({ ...options, verifyEffects: verifier, now: fixture.at });
    expect(verifier).toHaveBeenCalledTimes(1);
    expect(fixture.transactions).toHaveLength(before);
    expect(fixture.state.works.get(fixture.assignment.claims[0].work_id).barrier).toBe("verified");
  });

  it("bounds a hung read-only delivery verifier and aborts its scoped API context", async () => {
    let signal;
    const proof = await verifyWithinBudget(
      async (_member, context) => {
        signal = context.signal;
        return new Promise(() => {});
      },
      { handle: "h1" },
      { attempt: 1 },
      5
    );
    expect(proof).toBeNull();
    expect(signal.aborted).toBe(true);
  });

  it("does not recover native binding or publish terminal mutations during staged preview", async () => {
    const { options } = setup(1);
    const fixture = queueFixture({ started: true, count: 1 });
    const preview = {
      ...options,
      assignment: fixture.assignment,
      githubClient: fixture.githubClient,
      context: fixture.workerContext,
      readWorkQueueLog: fixture.readWorkQueueLog,
      publishWorkQueueRequest: vi.fn(fixture.publishWorkQueueRequest),
      config: { staged: true },
    };
    finish(preview, "h1", "completed");
    const before = fixture.transactions.length;
    expect((await reconcileWorkerClaim(preview)).claims.h1).toMatchObject({ state: "staged_preview", authorized: false });
    expect((await finalizeWorkerResults(preview)).claims.h1).toMatchObject({ state: "staged_preview", effects: "unknown" });
    expect(preview.publishWorkQueueRequest).not.toHaveBeenCalled();
    expect(fixture.transactions).toHaveLength(before);
    expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id).run).toBeUndefined();
  });

  it("retains canonical open Claim handles without wrapup cancellation when global preview is enabled", async () => {
    const { fixture, options } = setup(2);
    const before = fixture.transactions.length;
    vi.stubEnv("GH_AW_SAFE_OUTPUTS_STAGED", "true");
    try {
      const preview = await reconcileWorkerClaim(options);
      expect(preview.status).toBe("staged_preview");
      expect(preview.claims.h1).toMatchObject({ claim_handle: "h1", state: "staged_preview", authorized: false });
      expect(preview.claims.h2).toMatchObject({ claim_handle: "h2", state: "staged_preview", authorized: false });
      expect(await authorizeWorkerClaim({ ...options, claim_handle: "h1", requireCompletion: false })).toMatchObject({ authorized: true, state: "open" });
      expect(fixture.transactions).toHaveLength(before);
      expect([...fixture.state.claims.values()].every(claim => claim.state === "open")).toBe(true);
    } finally {
      vi.unstubAllEnvs();
    }
  });

  it("publishes independent Completion/cancellation commits and gates only completed siblings", async () => {
    const { fixture, options } = setup();
    finish(options, "h1", "completed");
    finish(options, "h2", "cancelled");
    finish(options, "h3", "completed");
    const result = await reconcileWorkerClaim(options);
    expect(result.status).toBe("completed_with_cancellations");
    expect(result).not.toHaveProperty("authorized");
    expect(result.claims.h1).toMatchObject({ state: "completed", authorized: true });
    expect(result.claims.h2).toMatchObject({ state: "cancelled", authorized: false });
    expect(result.claims.h3).toMatchObject({ state: "completed", authorized: true });
    expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id).released).toBe(false);
    expect(fixture.transactions.filter(commit => commit.request.kind === "finish")).toHaveLength(3);
    expect(fixture.transactions.filter(commit => commit.request.kind === "finish").every(commit => commit.operations.filter(operation => operation.kind === "Completion").length <= 1)).toBe(true);
    expect(await authorizeWorkerClaim({ ...options, claim_handle: "h1" })).toMatchObject({ authorized: true, claim_handle: "h1" });
    expect(await authorizeWorkerClaim({ ...options, claim_handle: "h2" })).toMatchObject({ authorized: false, suppressed: true, state: "cancelled" });
    await expect(authorizeWorkerClaim({ ...options, claim_handle: "h1", message: { claim_handle: "h1", repository: "foreign/repo" } })).rejects.toThrow(/scope/);
  });

  it("authorizes targetless ownership checks against the immutable effect scope and still gates resolved resources", async () => {
    const effectScope = "foreign/approved";
    const { options } = setup(1, { configurePolicy: policy => (policy.pools.default.profiles.default.effect_scope = effectScope) });
    expect(await authorizeWorkerClaim({ ...options, claim_handle: "h1", requireCompletion: false })).toMatchObject({ authorized: true, state: "open", effect_scope: effectScope });
    finish(options, "h1", "completed");
    await reconcileWorkerClaim(options);
    expect(await authorizeWorkerClaim({ ...options, message: { type: "work_queue_effect_pass", claim_handle: "h1" } })).toMatchObject({ authorized: true, state: "completed" });
    expect(await authorizeWorkerClaim({ ...options, message: { type: "work_queue_result", claim_handle: "h1" } })).toMatchObject({ authorized: true, state: "completed" });
    expect(await authorizeWorkerClaim({ ...options, claim_handle: "h1", resource: { repository: effectScope } })).toMatchObject({ authorized: true, state: "completed" });
    await expect(authorizeWorkerClaim({ ...options, claim_handle: "h1", resource: { repository: REPOSITORY } })).rejects.toThrow(/scope/);
    await expect(authorizeWorkerClaim({ ...options, claim_handle: "h1", message: { claim_handle: "h1", repo: REPOSITORY } })).rejects.toThrow(/scope/);
    await expect(authorizeWorkerClaim({ ...options, claim_handle: "h1", message: { claim_handle: "h1", repo: effectScope, repository: "foreign/unapproved" } })).rejects.toThrow(/scope/);
  });

  it("automatically scopes only an actually omitted selector on the immutable original singleton", async () => {
    const single = setup(1);
    expect(await authorizeWorkerClaim({ ...single.options, requireCompletion: false })).toMatchObject({ authorized: true, claim_handle: "h1" });
    finish(single.options, "h1", "completed");
    await reconcileWorkerClaim(single.options);
    expect(await authorizeWorkerClaim(single.options)).toMatchObject({ authorized: true, claim_handle: "h1" });
    for (const selector of [null, "", "foreign", undefined]) await expect(authorizeWorkerClaim({ ...single.options, claim_handle: selector })).rejects.toThrow();
    await expect(authorizeWorkerClaim({ ...single.options, claim_handle: "h1", message: null })).rejects.toThrow(/scope/);
    const batch = setup(2);
    finish(batch.options, "h1", "completed");
    finish(batch.options, "h2", "cancelled");
    await reconcileWorkerClaim(batch.options);
    await expect(authorizeWorkerClaim(batch.options)).rejects.toThrow(/scope/);
  });

  it("cancels only missing finishes while preserving an already durable Completion", async () => {
    const { fixture, options } = setup();
    finish(options, "h1", "completed");
    await reconcileWorkerClaim(options);
    const before = fixture.transactions.length;
    const again = await reconcileWorkerClaim(options);
    expect(again.claims.h1.authorized).toBe(true);
    expect(again.claims.h2.state).toBe("cancelled");
    expect(again.claims.h3.state).toBe("cancelled");
    expect(fixture.transactions).toHaveLength(before);
    expect(fixture.state.works.get(fixture.assignment.claims[0].work_id).state).toBe("completed");
  });

  it("atomically cancels exhausted scoped Work without poisoning an independently completed sibling", async () => {
    const { options } = setup(2);
    const fixture = queueFixture({
      bound: true,
      count: 2,
      configurePolicy: policy => {
        policy.pools.default.retry.max_attempts = 1;
      },
    });
    const configured = { ...options, assignment: fixture.assignment, githubClient: fixture.githubClient, context: fixture.workerContext, readWorkQueueLog: fixture.readWorkQueueLog, publishWorkQueueRequest: fixture.publishWorkQueueRequest };
    finish(configured, "h1", "completed");
    finish(configured, "h2", "cancelled");
    const result = await reconcileWorkerClaim(configured);
    expect(result.claims.h1).toMatchObject({ state: "completed", authorized: true });
    expect(result.claims.h2).toMatchObject({ state: "cancelled", authorized: false });
    const cancellation = fixture.transactions.find(commit => commit.request.parameters.claim_handle === "h2");
    expect(cancellation.operations.map(operation => operation.kind)).toEqual(["ClaimCancellation", "WorkCancellation"]);
    expect(fixture.state.works.get(fixture.assignment.claims[1].work_id).state).toBe("cancelled");
    expect(fixture.state.works.get(fixture.assignment.claims[0].work_id).barrier).toBe("pending");
    expect(await authorizeWorkerClaim({ ...configured, claim_handle: "h2" })).toMatchObject({ authorized: false, suppressed: true, state: "cancelled" });
  });

  it("normalizes omissions only for an original singleton and isolates conflicting or foreign scopes", async () => {
    const single = setup(1);
    finish(single.options, undefined, "completed", "single-finish");
    expect((await reconcileWorkerClaim(single.options)).claims.h1.authorized).toBe(true);
    const batch = setup();
    finish(batch.options, "h1", "completed");
    finish(batch.options, "h1", "cancelled");
    finish(batch.options, "h2", "completed");
    finish(batch.options, undefined, "completed", "ambiguous");
    finish(batch.options, "foreign", "completed");
    const parsed = readFinishIntent(batch.options.finishIntentPath, batch.fixture.assignment);
    expect(parsed.errors).toHaveLength(3);
    const result = await reconcileWorkerClaim(batch.options);
    expect(result.claims.h1).toMatchObject({ state: "blocked", authorized: false });
    expect(result.claims.h2).toMatchObject({ state: "completed", authorized: true });
    expect(result.claims.h3).toMatchObject({ state: "cancelled", authorized: false });
  });

  it("requires actual authenticated native binding and rejects reruns or substituted payloads", async () => {
    const { fixture, options } = setup();
    finish(options, "h1", "completed");
    const get = fixture.githubClient.rest.actions.getWorkflowRun;
    fixture.githubClient.rest.actions.getWorkflowRun = async args => ({ ...(await get(args)), data: { ...(await get(args)).data, run_attempt: 2 } });
    await expect(reconcileWorkerClaim(options)).rejects.toThrow(/attempt|rerun/);
    expect(fixture.state.claims.get(fixture.assignment.claims[0].claim_id).state).toBe("open");
    fixture.githubClient.rest.actions.getWorkflowRun = get;
    const forged = { ...fixture.assignment, claims: fixture.assignment.claims.map((member, index) => (index ? member : { ...member, work: { plan: "replaced" } })) };
    await expect(reconcileWorkerClaim({ ...options, assignment: forged })).rejects.toThrow(/assignment_mismatch/);
  });

  it("rejects complete payload, effect contract and Result-reference substitutions at authorization and finalization", async () => {
    const { fixture, options } = setup(1);
    finish(options, "h1", "completed");
    await reconcileWorkerClaim(options);
    const member = fixture.assignment.claims[0];
    const verifier = vi.fn();
    const before = fixture.transactions.length;
    for (const substitution of [
      { work: { ...member.work, plan: "replacement payload" } },
      { work: { ...member.work, effect_contract: { version: 1, outputs: [], no_writes: true } } },
      { result_refs: [{ work_id: "forged_parent", result_commit_id: "forged_result", descriptor: { replaced: true } }] },
    ]) {
      const assignment = { ...fixture.assignment, claims: [{ ...member, ...substitution }] };
      await expect(authorizeWorkerClaim({ ...options, assignment })).rejects.toThrow(/assignment_mismatch/);
      await expect(finalizeWorkerResults({ ...options, assignment, verifyEffects: verifier })).rejects.toThrow(/assignment_mismatch/);
    }
    expect(verifier).not.toHaveBeenCalled();
    expect(fixture.transactions).toHaveLength(before);
    expect(fixture.state.works.get(member.work_id).barrier).toBe("pending");
  });

  it("publishes Result only after verified declared contracts and never replays a settled member", async () => {
    const { fixture, options } = setup();
    finish(options, "h1", "completed");
    finish(options, "h3", "completed");
    await reconcileWorkerClaim(options);
    const verify = vi.fn(async member => ({ verified: true, contractVerified: true, receipt: `trusted:${member.handle}`, descriptor: { report: member.work.plan }, effects: "none" }));
    const result = await finalizeWorkerResults({ ...options, now: fixture.at, verifyEffects: verify });
    expect(result.claims.h1.state).toBe("result");
    expect(result.claims.h2.state).toBe("cancelled");
    expect(result.claims.h3.state).toBe("result");
    expect(verify).toHaveBeenCalledTimes(2);
    const recovered = await finalizeWorkerResults({ ...options, now: fixture.at, verifyEffects: verify });
    expect(verify).toHaveBeenCalledTimes(2);
    expect(recovered.claims.h1).toMatchObject({ state: "result", effects: "none" });
    expect(await authorizeWorkerClaim({ ...options, claim_handle: "h1" })).toMatchObject({ authorized: false, suppressed: true, state: "result" });
    await expect(authorizeWorkerClaim({ ...options, claim_handle: "h1", resource: { repository: "foreign/repo" } })).rejects.toThrow(/scope/);
  });

  it("keeps missing receipts unknown and needs both terminal evidence and exhausted verification for failure", async () => {
    const { fixture, options } = setup(1);
    finish(options, undefined, "completed", "finish");
    await reconcileWorkerClaim(options);
    const verify = vi.fn(async () => ({ verified: false, contractVerified: false, effects: "none" }));
    const pending = await finalizeWorkerResults({ ...options, now: fixture.at, verifyEffects: verify });
    expect(pending.claims.h1).toMatchObject({ state: "pending", effects: "unknown", reason: "terminal_evidence_required" });
    expect(fixture.state.works.get(fixture.assignment.claims[0].work_id).barrier).toBe("pending");
    const get = fixture.githubClient.rest.actions.getWorkflowRun;
    fixture.githubClient.rest.actions.getWorkflowRun = async args => ({ ...(await get(args)), data: { ...(await get(args)).data, status: "completed", conclusion: "failure" } });
    const result = await finalizeWorkerResults({ ...options, now: fixture.at, verifyEffects: verify });
    expect(result.claims.h1).toMatchObject({ state: "delivery_failed", effects: "unknown" });
    expect(fixture.state.works.get(fixture.assignment.claims[0].work_id).disposition).toBe("unknown");
    expect(verify).toHaveBeenCalledTimes(10);
  });

  it("does not promote a no-effect receipt verified before native termination into definitive no effects", async () => {
    const { fixture, options } = setup(1);
    finish(options, "h1", "completed");
    await reconcileWorkerClaim(options);
    const get = fixture.githubClient.rest.actions.getWorkflowRun;
    let terminal = false;
    fixture.githubClient.rest.actions.getWorkflowRun = async args => {
      const response = await get(args);
      return { ...response, data: { ...response.data, ...(terminal ? { status: "completed", conclusion: "failure" } : {}) } };
    };
    const verify = vi.fn(async () => {
      terminal = true;
      return { verified: false, contractVerified: false, receipt: "trusted:preterminal-snapshot", effects: "none" };
    });
    const result = await finalizeWorkerResults({ ...options, now: fixture.at, verifyEffects: verify });
    expect(result.claims.h1).toMatchObject({ state: "delivery_failed", effects: "unknown" });
    expect(fixture.state.works.get(fixture.assignment.claims[0].work_id).disposition).toBe("unknown");
  });

  it("retains verified no-effect failure evidence only when the exact native run was terminal before verification", async () => {
    const { fixture, options } = setup(1);
    finish(options, "h1", "completed");
    await reconcileWorkerClaim(options);
    const get = fixture.githubClient.rest.actions.getWorkflowRun;
    fixture.githubClient.rest.actions.getWorkflowRun = async args => {
      const response = await get(args);
      return { ...response, data: { ...response.data, status: "completed", conclusion: "failure" } };
    };
    const verify = vi.fn(async () => ({ verified: false, contractVerified: false, receipt: "trusted:terminal-no-effects", effects: "none" }));
    const result = await finalizeWorkerResults({ ...options, now: fixture.at, verifyEffects: verify });
    expect(result.claims.h1).toMatchObject({ state: "delivery_failed", effects: "none" });
    expect(fixture.state.works.get(fixture.assignment.claims[0].work_id).disposition).toBe("none");
  });

  it("exposes per-Claim authorization JSON, not a run-wide boolean", async () => {
    const { options } = setup(1);
    finish(options, undefined, "completed", "finish");
    const core = { setOutput: vi.fn(), info: vi.fn(), summary: { addRaw: vi.fn(() => ({ write: async () => {} })) } };
    await main({ ...options, core });
    expect(core.setOutput.mock.calls.map(([name]) => name)).toEqual(["claim_authorizations"]);
    expect(JSON.parse(core.setOutput.mock.calls[0][1]).claims.h1.authorized).toBe(true);
  });

  it("stages wrap-up and verification previews without terminal publication or effect verification", async () => {
    const { fixture, options } = setup(1);
    finish(options, undefined, "completed", "finish");
    const before = fixture.transactions.length;
    expect((await reconcileWorkerClaim({ ...options, staged: true })).claims.h1).toMatchObject({ state: "staged_preview", authorized: false });
    const verify = vi.fn();
    expect((await finalizeWorkerResults({ ...options, staged: true, verifyEffects: verify })).claims.h1.state).toBe("staged_preview");
    expect(verify).not.toHaveBeenCalled();
    expect(fixture.transactions).toHaveLength(before);
  });
});
