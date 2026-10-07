// @ts-check
import { afterEach, describe, expect, it, vi } from "vitest";
import fs from "fs";
import path from "path";
import os from "node:os";
import { createRequire } from "node:module";
import { authorizeWorkerClaim, finalizeWorkerResults, main, readFinishIntent, reconcileWorkerClaim, verifyWithinBudget } from "./finish_work_queue_claim.cjs";
import { nodeId } from "./work_queue_graph.cjs";
import { queueFixture, noWriteClaimVerifier, REF, REPOSITORY, WORKFLOW, DISPATCHER } from "./work_queue_lifecycle.test_helpers.cjs";

const directories = [];
function setup(count = 3, fixtureOptions = {}) {
  const fixture = queueFixture({ bound: true, count, ...fixtureOptions });
  const directory = fs.mkdtempSync(path.join(fs.realpathSync(os.tmpdir()), "gh-aw-queue-finish-"));
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
  it.each([
    undefined,
    null,
    false,
    "verified",
    { verified: false, effects: "none", receipt: 123 },
    { verified: false, effects: "none", receipt: {} },
    { verified: false, effects: "partial" },
    { verified: false, effects: "partial", receipt: null },
    { verified: false, effects: "partial", receipt: false },
    { verified: false, effects: "partial", receipt: 123 },
    { verified: false, effects: "partial", receipt: {} },
    { verified: false, effects: "partial", receipt: "" },
    { verified: true, contractVerified: true, receipt: "", descriptor: {}, effects: "none" },
  ])("keeps malformed verification or receipt unknown through terminal budget exhaustion: %j", async verification => {
    const { fixture, options } = setup(1);
    finish(options, "h1", "completed");
    await reconcileWorkerClaim(options);
    fixture.githubClient.rest.actions.getWorkflowRunAttempt = vi.fn().mockResolvedValue({ status: 200, data: { ...fixture.nativeRun(), status: "completed", conclusion: "failure" } });
    const verifier = vi.fn().mockResolvedValue(verification);
    expect((await finalizeWorkerResults({ ...options, verifyEffects: verifier })).claims.h1).toMatchObject({ state: "delivery_failed", effects: "unknown" });
    expect(verifier).toHaveBeenCalledTimes(fixture.policy.pools.default.reconciliation.max_attempts);
    const operations = fixture.transactions.flatMap(commit => commit.operations);
    expect(operations.some(operation => operation.kind === "Result")).toBe(false);
    expect(operations.find(operation => operation.kind === "DeliveryFailure")).toMatchObject({ disposition: "unknown" });
  });

  it.each(["cancelled", "result"])("checks the entire durable original binding before suppressing %s effects", async state => {
    const { fixture, options } = setup(1);
    finish(options, "h1", state === "cancelled" ? "cancelled" : "completed");
    await reconcileWorkerClaim(options);
    if (state === "result") await finalizeWorkerResults({ ...options, verifyEffects: noWriteClaimVerifier(options) });
    expect(await authorizeWorkerClaim({ ...options, claim_handle: "h1" })).toMatchObject({ authorized: false, suppressed: true, state });
    const before = fixture.transactions;
    fixture.githubClient.rest.repos.get = async () => ({ status: 200, data: { full_name: "OWNER/REPO", id: 7 } });
    await expect(authorizeWorkerClaim({ ...options, claim_handle: "h1" })).rejects.toThrow(/binding_not_durable/);
    expect(fixture.transactions).toEqual(before);
  });

  function publishChild(fixture, member, label) {
    const template = fixture.transactions[1].operations[0];
    return fixture.append(
      "submit",
      { nodes: [{ ...template, graph_id: label, node_key: "child", work_id: nodeId(label, "child") }] },
      { ...fixture.workerActor, dispatch_id: fixture.assignment.dispatch_id, claim_handle: member.handle },
      `control:${label}`
    );
  }

  it("preserves declared verification intent without allowing it to replace a protected verifier", async () => {
    const contract = { version: 1, outputs: [{ type: "create_issue", min: 1, max: 1, verification: { verifier_id: "issue-readback-v1", expected: { title: "Claim outcome" } } }] };
    const { fixture, options } = setup(1, { workDefaults: { payload: { effect_contract: contract } } });
    finish(options, "h1", "completed");
    await reconcileWorkerClaim(options);
    expect((await finalizeWorkerResults(options)).claims.h1).toMatchObject({ state: "pending", effects: "unknown", reason: "verification_unavailable" });
    const verifier = vi.fn(async (member, scope) => {
      expect(member.work.effect_contract).toEqual(contract);
      expect(scope.contract).toEqual(contract);
      return { verified: true, contractVerified: true, receipt: "trusted:issue-readback", descriptor: { issue_id: "301" }, effects: "partial" };
    });
    expect((await finalizeWorkerResults({ ...options, verifyEffects: verifier })).claims.h1).toMatchObject({ state: "pending", effects: "unknown" });
    expect(verifier).toHaveBeenCalledTimes(fixture.policy.pools.default.reconciliation.max_attempts);
    expect(fixture.transactions.flatMap(commit => commit.operations).filter(operation => operation.kind === "Result")).toHaveLength(0);
  });

  const unsupportedPayloads = [
    { plan: "missing contract" },
    { output_contract: { kind: "none" } },
    { effect_contract: { version: 2, outputs: [], no_writes: true } },
    { effect_contract: { version: 1, outputs: [] } },
    { effect_contract: { version: 1, outputs: [{ type: "create_issue", min: 1, max: 1 }], no_writes: true } },
  ];
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
    const result = await finalizeWorkerResults({ ...options, verifyEffects: noWriteClaimVerifier(options), now: fixture.at });
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
      verifyEffects: noWriteClaimVerifier(options),
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
    const protectedVerifier = noWriteClaimVerifier(options);
    const verifier = vi.fn(async (member, scope) => {
      const proof = await protectedVerifier(member, scope);
      changed = true;
      return proof;
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
    const protectedVerifier = noWriteClaimVerifier(options);
    const verifier = vi.fn(async (member, scope) => {
      const proof = await protectedVerifier(member, scope);
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
      return proof;
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
    const { isTrustedClaimDelivery } = createRequire(import.meta.url)("./work_queue_delivery.cjs");
    const { fixture, options } = setup(2);
    finish(options, "h1", "completed");
    finish(options, "h2", "cancelled");
    await reconcileWorkerClaim(options);
    const protectedVerifier = noWriteClaimVerifier(options);
    const verifier = vi.fn(async (member, scope) => {
      expect(member.handle).toBe("h1");
      expect(scope.run.run_id).toBe("42");
      const proof = await protectedVerifier(member, scope);
      expect(isTrustedClaimDelivery(proof, scope.assignment, member.handle)).toBe(true);
      return proof;
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

  it.each(["json_copy", "spread_copy", "forged_success", "receipt_mutation", "descriptor_mutation", "descriptor_cycle"])("rejects non-original or changed successful delivery proof: %s", async alteration => {
    const { fixture, options } = setup(1, { workerPrincipal: "22" });
    finish(options, "h1", "completed");
    await reconcileWorkerClaim(options);
    const protectedVerifier = noWriteClaimVerifier(options);
    const verifier = vi.fn(async (member, scope) => {
      const proof = await protectedVerifier(member, scope);
      if (alteration === "json_copy") return JSON.parse(JSON.stringify(proof));
      if (alteration === "spread_copy") return { ...proof };
      if (alteration === "forged_success") return { verified: true, contractVerified: true, receipt: "agent-artifact", descriptor: { outputs: [] }, effects: "none" };
      if (alteration === "receipt_mutation") proof.receipt = "changed-receipt";
      else if (alteration === "descriptor_mutation") proof.descriptor.outputs[0].type = "undeclared";
      else proof.descriptor.self = proof.descriptor;
      return proof;
    });
    const result = await finalizeWorkerResults({ ...options, now: fixture.at, verifyEffects: verifier });
    expect(result.claims.h1).toMatchObject({ state: "pending", effects: "unknown" });
    expect(verifier).toHaveBeenCalledTimes(fixture.policy.pools.default.reconciliation.max_attempts);
    expect(fixture.state.works.get(fixture.assignment.claims[0].work_id).barrier).toBe("pending");
    expect(fixture.transactions.flatMap(commit => commit.operations).some(operation => operation.kind === "Result")).toBe(false);
  });

  it("rejects another Claim's protected proof without preventing that sibling's Result", async () => {
    const { fixture, options } = setup(2, { workerPrincipal: "22" });
    finish(options, "h1", "completed");
    finish(options, "h2", "completed");
    await reconcileWorkerClaim(options);
    const protectedVerifier = noWriteClaimVerifier(options);
    const result = await finalizeWorkerResults({
      ...options,
      now: fixture.at,
      verifyEffects: (member, scope) => protectedVerifier(member.handle === "h1" ? scope.assignment.claims[1] : member, scope),
    });
    expect(result.claims.h1).toMatchObject({ state: "pending", effects: "unknown" });
    expect(result.claims.h2).toMatchObject({ state: "result", effects: "none" });
    expect(
      fixture.transactions
        .flatMap(commit => commit.operations)
        .filter(operation => operation.kind === "Result")
        .map(operation => operation.claim_id)
    ).toEqual([fixture.assignment.claims[1].claim_id]);
  });

  it.each(["receipt", "nested_descriptor", "descriptor_proxy"])("rejects mutable accessor-backed %s proof fields before snapshotting", async field => {
    const { fixture, options } = setup(1, { workerPrincipal: "22" });
    finish(options, "h1", "completed");
    await reconcileWorkerClaim(options);
    const protectedVerifier = noWriteClaimVerifier(options);
    const result = await finalizeWorkerResults({
      ...options,
      now: fixture.at,
      verifyEffects: async (member, scope) => {
        const proof = await protectedVerifier(member, scope);
        if (field === "descriptor_proxy") {
          let reads = 0;
          proof.descriptor.outputs[0] = new Proxy(proof.descriptor.outputs[0], {
            get: (target, key, receiver) => (key === "type" && ++reads === 2 ? "unverified-snapshot" : Reflect.get(target, key, receiver)),
          });
          return proof;
        }
        const target = field === "receipt" ? proof : proof.descriptor.outputs[0];
        const key = field === "receipt" ? "receipt" : "type";
        const original = target[key];
        let reads = 0;
        Object.defineProperty(target, key, {
          enumerable: true,
          get: () => (++reads === (field === "receipt" ? 4 : 2) ? "unverified-snapshot" : original),
        });
        return proof;
      },
    });
    expect(result.claims.h1).toMatchObject({ state: "pending", effects: "unknown" });
    expect(fixture.transactions.flatMap(commit => commit.operations).some(operation => operation.kind === "Result")).toBe(false);
  });

  it.each(["native_read", "cas"])("rechecks the original delivery brand after awaited %s work", async phase => {
    const { fixture, options } = setup(2, { workerPrincipal: "22" });
    finish(options, "h1", "completed");
    finish(options, "h2", "completed");
    await reconcileWorkerClaim(options);
    const protectedVerifier = noWriteClaimVerifier(options);
    let firstProof;
    const mutate = () => {
      if (firstProof) firstProof.descriptor.outputs[0].type = "late-mutation";
    };
    if (phase === "native_read") {
      const get = fixture.githubClient.rest.actions.getWorkflowRunAttempt;
      fixture.githubClient.rest.actions.getWorkflowRunAttempt = async args => {
        const result = await get(args);
        mutate();
        return result;
      };
    } else {
      const publish = options.publishWorkQueueRequest;
      options.publishWorkQueueRequest = async args => {
        if (args.request.kind === "result") mutate();
        return publish(args);
      };
    }
    const result = await finalizeWorkerResults({
      ...options,
      now: fixture.at,
      verifyEffects: async (member, scope) => {
        const proof = await protectedVerifier(member, scope);
        if (member.handle === "h1") firstProof = proof;
        return proof;
      },
    });
    expect(firstProof).toBeDefined();
    expect(result.claims.h1).toMatchObject({ state: "pending", effects: "unknown" });
    expect(result.claims.h2).toMatchObject({ state: "result", effects: "none" });
    expect(
      fixture.transactions
        .flatMap(commit => commit.operations)
        .filter(operation => operation.kind === "Result")
        .map(operation => operation.claim_id)
    ).toEqual([fixture.assignment.claims[1].claim_id]);
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
    await expect(authorizeWorkerClaim({ ...options, claim_handle: "h1", resource: { repository: effectScope } })).rejects.toThrow(/scope|binding/);
    await expect(authorizeWorkerClaim({ ...options, claim_handle: "h1", resource: { repository: effectScope, number: "42", comment_id: "123", run_id: "42", ref: REF, path: "file.txt" } })).rejects.toThrow(/scope|binding/);

    await expect(authorizeWorkerClaim({ ...options, claim_handle: "h1", resource: { repo: effectScope, item_number: 42 } })).rejects.toThrow(/scope/);
    await expect(authorizeWorkerClaim({ ...options, claim_handle: "h1", resource: { repository: effectScope, run_id: "456" } })).rejects.toThrow(/scope/);
    await expect(authorizeWorkerClaim({ ...options, claim_handle: "h1", resource: { repository: REPOSITORY } })).rejects.toThrow(/scope/);
    await expect(authorizeWorkerClaim({ ...options, claim_handle: "h1", resource: { repo: REPOSITORY, item_number: 42 } })).rejects.toThrow(/scope/);
    await expect(authorizeWorkerClaim({ ...options, claim_handle: "h1", resource: { repository: effectScope, repo: REPOSITORY, item_number: 42 } })).rejects.toThrow(/scope/);
    await expect(authorizeWorkerClaim({ ...options, claim_handle: "h1", resource: { item_number: 42 } })).rejects.toThrow(/scope/);
    await expect(authorizeWorkerClaim({ ...options, claim_handle: "h1", resource: null })).rejects.toThrow(/scope/);
    await expect(authorizeWorkerClaim({ ...options, claim_handle: "h1", message: { claim_handle: "h1", repo: REPOSITORY } })).rejects.toThrow(/scope/);
    await expect(authorizeWorkerClaim({ ...options, claim_handle: "h1", message: { claim_handle: "h1", repo: effectScope, repository: "foreign/unapproved" } })).rejects.toThrow(/scope/);
  });

  it("intersects actual lifecycle effect targets with the authoritative immutable Work subject without changing assignment payloads", async () => {
    const subject = { kind: "issue", host: "github.com", repository: REPOSITORY, repository_id: "7", resource_id: "501", number: "19" };
    const { fixture, options } = setup(1, { workerPrincipal: "22", workDefaults: { subject } });
    expect(fixture.assignment.claims[0].work).not.toHaveProperty("subject");
    finish(options, "h1", "completed");
    await reconcileWorkerClaim(options);
    expect(await authorizeWorkerClaim({ ...options, claim_handle: "h1", resource: subject })).toMatchObject({ authorized: true, state: "completed", run_id: "42" });
    expect(fixture.state.works.get(fixture.assignment.claims[0].work_id).subject).toEqual(subject);
    for (const resource of [
      { repository: REPOSITORY },
      { ...subject, repository: "foreign/repo" },
      { ...subject, repository_id: "8" },
      { ...subject, resource_id: "502" },
      { ...subject, number: "20" },
      { ...subject, kind: "pull_request" },
      { ...subject, run_id: "43" },
    ]) {
      await expect(authorizeWorkerClaim({ ...options, claim_handle: "h1", resource })).rejects.toThrow(/scope/);
    }
    expect(fixture.policy.producers).not.toHaveProperty("22");
  });

  it.each(["alias_only", "matching_alias"])("normalizes resolved SDK repository aliases without replacing identity constraints: %s", async mode => {
    const subject = { kind: "issue", host: "github.com", repository: REPOSITORY, repository_id: "7", resource_id: "501", number: "19" };
    const { repository, ...identities } = subject;
    const resource = { ...identities, repo: repository, ...(mode === "matching_alias" ? { repository } : {}) };
    const { options } = setup(1, { workerPrincipal: "22", workDefaults: { subject } });
    finish(options, "h1", "completed");
    await reconcileWorkerClaim(options);
    const authority = vi.spyOn(createRequire(__filename)("./work_queue_replay.cjs"), "validateClaimAuthority");
    try {
      expect(await authorizeWorkerClaim({ ...options, claim_handle: "h1", resource })).toMatchObject({ authorized: true });
      expect(authority.mock.calls.at(-1)[3]).toEqual({ requireCompletion: true, resource: subject });
      expect(resource).toHaveProperty("repo", repository);
      await expect(authorizeWorkerClaim({ ...options, claim_handle: "h1", resource: { ...resource, repo: "foreign/repo" } })).rejects.toThrow(/scope/);
      await expect(authorizeWorkerClaim({ ...options, claim_handle: "h1", resource: { ...resource, resource_id: "502" } })).rejects.toThrow(/scope/);
      await expect(authorizeWorkerClaim({ ...options, claim_handle: "h1", resource: { ...resource, resource_scope: "allow" } })).rejects.toThrow(/scope/);
    } finally {
      authority.mockRestore();
    }
  });

  it.each(["repository_only", "payload_aliases", "payload_resource_scope"])("denies existing-resource writes without a positive authoritative Work binding: %s", async source => {
    const target = { kind: "issue", host: "github.com", repository: REPOSITORY, repository_id: "7", resource_id: "501", number: "19" };
    const payload = { effect_contract: { version: 1, outputs: [{ type: "update_issue", min: 1, max: 1 }] } };
    if (source === "payload_aliases") Object.assign(payload, { issue_number: "19", resource_id: "501", repository: REPOSITORY });
    if (source === "payload_resource_scope") Object.assign(payload, { resource_scope: { version: 1, resources: [{ repository: REPOSITORY, number: "19" }] } });
    const { options } = setup(1, { workerPrincipal: "22", workDefaults: { payload } });
    finish(options, "h1", "completed");
    await reconcileWorkerClaim(options);
    expect(await authorizeWorkerClaim({ ...options, claim_handle: "h1" })).toMatchObject({ authorized: true });
    await expect(authorizeWorkerClaim({ ...options, claim_handle: "h1", resource: target })).rejects.toThrow(/scope|binding/);
  });

  it.each([undefined, { kind: "issue", host: "github.com", repository: REPOSITORY, repository_id: "7", resource_id: "501", number: "19" }])(
    "denies non-Issue resource effects without closed approved selectors even under the original run and repository: subject=%s",
    async subject => {
      const { options } = setup(1, { workerPrincipal: "22", ...(subject ? { workDefaults: { subject } } : {}) });
      finish(options, "h1", "completed");
      await reconcileWorkerClaim(options);
      for (const resource of [{ repository: REPOSITORY }, { repository: REPOSITORY, run_id: "42" }, { repository: REPOSITORY, ref: REF }, { repository: REPOSITORY, run_id: "42", path: "h2/report.json" }]) {
        await expect(authorizeWorkerClaim({ ...options, claim_handle: "h1", resource })).rejects.toThrow(/scope|binding/);
      }
    }
  );

  it("checks metadata aliases without turning them into verified immutable Work resource targets", async () => {
    const subject = { kind: "issue", host: "github.com", repository: REPOSITORY, repository_id: "7", resource_id: "501", number: "19" };
    const { options } = setup(1, {
      workerPrincipal: "22",
      workDefaults: { subject, payload: { effect_contract: { kind: "none" }, resource_scope: { version: 1, resources: [subject] } } },
    });
    finish(options, "h1", "completed");
    await reconcileWorkerClaim(options);
    const message = { type: "work_queue_effect_pass", claim_handle: "h1", repo: REPOSITORY };
    const authority = vi.spyOn(createRequire(__filename)("./work_queue_replay.cjs"), "validateClaimAuthority");
    try {
      expect(await authorizeWorkerClaim({ ...options, message })).toMatchObject({ authorized: true, claim_handle: "h1" });
      expect(authority.mock.calls.at(-1)[3]).toEqual({ requireCompletion: true });
      expect(await authorizeWorkerClaim({ ...options, message, resource: subject })).toMatchObject({ authorized: true });
      expect(authority.mock.calls.at(-1)[3]).toEqual({ requireCompletion: true, resource: subject });
      await expect(authorizeWorkerClaim({ ...options, message, resource: { repository: REPOSITORY } })).rejects.toThrow(/scope/);
      await expect(authorizeWorkerClaim({ ...options, message: { ...message, repository: "foreign/repo" }, resource: subject })).rejects.toThrow(/scope/);
      await expect(authorizeWorkerClaim({ ...options, message: { ...message, "target-repo": "foreign/repo" } })).rejects.toThrow(/scope/);
    } finally {
      authority.mockRestore();
    }
  });

  it("gates independently resolved live SDK mutations by frozen Work narrowing without foreign or sibling escalation", async () => {
    const require = createRequire(__filename);
    const { withClaimExecution } = require("./work_queue_claim_scope.cjs");
    const { wrapClaimEffectClient } = require("./work_queue_effect_client.cjs");
    const { fixture, options } = setup(3, {
      workerPrincipal: "22",
      configurePolicy: policy => policy.pools.default.allowed_repositories.push("foreign/design"),
      configureWork: (work, index) => {
        const repository = index === 2 ? "foreign/design" : REPOSITORY;
        work.payload = {
          effect_contract: { version: 1, outputs: [{ type: "update_issue", min: 0, max: 1 }] },
          resource_scope: {
            version: 1,
            resources: [{ kind: "issue", host: "github.com", repository, repository_id: index === 2 ? "20" : "7", resource_id: index === 2 ? "401" : String(index + 501), number: String(index === 2 ? 7 : index + 7) }],
          },
        };
        if (index === 0) work.subject = { kind: "issue", host: "github.com", repository, repository_id: "7", resource_id: "501", number: "7" };
      },
    });
    for (const member of fixture.assignment.claims) finish(options, member.handle, "completed");
    await reconcileWorkerClaim(options);
    fixture.githubClient.rest.repos.get = vi.fn(async ({ owner, repo }) => ({ status: 200, data: { full_name: `${owner}/${repo}`, id: owner === "foreign" ? 20 : 7 } }));
    const update = vi.fn(async ({ issue_number }) => ({ status: 200, data: { number: issue_number, id: issue_number === 7 ? 501 : 502 } }));
    fixture.githubClient.rest.issues = {
      get: vi.fn(async ({ owner, issue_number }) => ({ status: 200, data: { number: issue_number, id: owner === "foreign" ? 401 : issue_number === 7 ? 501 : 502 } })),
      update,
    };
    const authorize = request => authorizeWorkerClaim({ ...options, ...request });
    const execute = (handle, callback) =>
      withClaimExecution({ assignment: fixture.assignment, claim_handle: handle, authorize, effects: [] }, () => callback(wrapClaimEffectClient(fixture.githubClient, { claim_handle: handle, authorize, context: options.context })));
    await execute("h1", async client => {
      await expect(client.rest.issues.update({ owner: "owner", repo: "repo", issue_number: 8, title: "sibling target" })).rejects.toThrow(/scope/);
      await expect(client.rest.issues.update({ owner: "foreign", repo: "design", issue_number: 7, title: "foreign target" })).rejects.toThrow(/scope/);
      expect(update).not.toHaveBeenCalled();
      await client.rest.issues.update({ owner: "owner", repo: "repo", issue_number: 7, title: "own immutable subject" });
    });
    await execute("h2", async client => {
      await expect(client.rest.issues.update({ owner: "owner", repo: "repo", issue_number: 7, title: "borrow first Claim scope" })).rejects.toThrow(/scope/);
      await client.rest.issues.update({ owner: "owner", repo: "repo", issue_number: 8, title: "own narrowed target" });
    });
    await execute("h3", async client => {
      expect((await client.rest.issues.get({ owner: "foreign", repo: "design", issue_number: 7 })).data.id).toBe(401);
      await expect(client.rest.issues.update({ owner: "foreign", repo: "design", issue_number: 7, title: "foreign selector grants nothing" })).rejects.toThrow(/scope/);
      await expect(client.rest.issues.update({ owner: "owner", repo: "repo", issue_number: 7, title: "profile cannot widen Work" })).rejects.toThrow(/scope/);
    });
    expect(update.mock.calls.map(([request]) => request.issue_number)).toEqual([7, 8]);
    const forged = JSON.parse(JSON.stringify(fixture.assignment));
    forged.claims[0].work.resource_scope.resources[0].number = "8";
    await expect(authorizeWorkerClaim({ ...options, assignment: forged, claim_handle: "h1", resource: { repository: REPOSITORY, number: "8" } })).rejects.toThrow(/assignment_mismatch/);
    expect(fixture.policy.producers).not.toHaveProperty("22");
  });

  it("intersects shared-profile Work scopes through the actual common handler factory before any resource mutation", async () => {
    const require = createRequire(__filename);
    const { withClaimExecution } = require("./work_queue_claim_scope.cjs");
    const { withClaimEffectClients } = require("./work_queue_effect_client.cjs");
    const { loadHandlers } = require("./safe_output_handler_manager.cjs");
    const { createPrReviewBufferRegistry } = require("./pr_review_buffer.cjs");
    const { fixture, options } = setup(3, {
      workerPrincipal: "22",
      configurePolicy: policy => policy.pools.default.allowed_repositories.push("foreign/design"),
      configureWork: (work, index) => {
        const repository = index === 2 ? "foreign/design" : REPOSITORY;
        work.payload = {
          effect_contract: { version: 1, outputs: [{ type: "update_issue", min: 0, max: 1 }] },
          resource_scope: {
            version: 1,
            resources: [{ kind: "issue", host: "github.com", repository, repository_id: index === 2 ? "20" : "7", resource_id: index === 2 ? "401" : String(index + 501), number: String(index === 2 ? 7 : index + 7) }],
          },
        };
        if (index === 0) work.subject = { kind: "issue", host: "github.com", repository, repository_id: "7", resource_id: "501", number: "7" };
      },
    });
    for (const member of fixture.assignment.claims) finish(options, member.handle, "completed");
    await reconcileWorkerClaim(options);
    fixture.githubClient.rest.repos.get = vi.fn(async ({ owner, repo }) => ({ status: 200, data: { full_name: `${owner}/${repo}`, id: owner === "foreign" ? 20 : 7 } }));
    const issue = (owner, number, title = "original") => ({
      id: owner === "foreign" ? 401 : number === 7 ? 501 : 502,
      number,
      title,
      body: "",
      state: "open",
      labels: [],
      html_url: `https://github.com/${owner}/${owner === "foreign" ? "design" : "repo"}/issues/${number}`,
    });
    const update = vi.fn(async ({ owner, issue_number, title }) => ({ status: 200, data: issue(owner, issue_number, title) }));
    fixture.githubClient.rest.issues = {
      get: vi.fn(async ({ owner, issue_number }) => ({ status: 200, data: issue(owner, issue_number) })),
      update,
    };
    const authorize = request => authorizeWorkerClaim({ ...options, ...request });
    const previous = { github: global.github, context: global.context, core: global.core };
    Object.assign(global, { github: fixture.githubClient, context: options.context, core: { info: vi.fn(), warning: vi.fn(), error: vi.fn(), debug: vi.fn() } });
    const effectsByHandle = new Map();
    const execute = (handle, callback) => {
      const effects = [];
      effectsByHandle.set(handle, effects);
      return withClaimExecution({ assignment: fixture.assignment, claim_handle: handle, authorize, effects }, () =>
        withClaimEffectClients({ claim_handle: handle, authorize, authorizeGithub: fixture.githubClient, context: options.context }, async () => {
          const handlers = await loadHandlers({ update_issue: { target: "*" } }, createPrReviewBufferRegistry());
          const handler = handlers.get("update_issue");
          expect(typeof handler).toBe("function");
          return callback(handler);
        })
      );
    };
    try {
      await execute("h1", async handler => {
        const denied = await handler({ type: "update_issue", claim_handle: "h1", issue_number: 8, title: "borrow sibling" }, {});
        expect(denied).toMatchObject({ success: false, error: expect.stringMatching(/scope/) });
        expect(update).not.toHaveBeenCalled();
        expect(effectsByHandle.get("h1")).toEqual([]);
        await expect(handler({ type: "update_issue", claim_handle: "h1", issue_number: 7, title: "own first resource" }, {})).resolves.toMatchObject({ success: true, number: 7 });
      });
      await execute("h2", async handler => {
        const denied = await handler({ type: "update_issue", claim_handle: "h2", issue_number: 7, title: "borrow first scope" }, {});
        expect(denied).toMatchObject({ success: false, error: expect.stringMatching(/scope/) });
        expect(update).toHaveBeenCalledTimes(1);
        expect(effectsByHandle.get("h2")).toEqual([]);
        await expect(handler({ type: "update_issue", claim_handle: "h2", issue_number: 8, title: "own second resource" }, {})).resolves.toMatchObject({ success: true, number: 8 });
      });
      await execute("h3", async handler => {
        expect((await global.github.rest.issues.get({ owner: "foreign", repo: "design", issue_number: 7 })).data.id).toBe(401);
        await expect(handler({ type: "update_issue", claim_handle: "h3", repo: "foreign/design", issue_number: 7, title: "foreign dependency grants nothing" }, {})).rejects.toThrow(/scope/);
        const denied = await handler({ type: "update_issue", claim_handle: "h3", issue_number: 7, title: "profile cannot widen Work" }, {});
        expect(denied).toMatchObject({ success: false, error: expect.stringMatching(/scope/) });
        expect(update).toHaveBeenCalledTimes(2);
        expect(effectsByHandle.get("h3")).toEqual([]);
      });
      expect(update.mock.calls.map(([request]) => [request.owner, request.repo, request.issue_number])).toEqual([
        ["owner", "repo", 7],
        ["owner", "repo", 8],
      ]);
      expect(fixture.policy.producers).not.toHaveProperty("22");
      expect(effectsByHandle.get("h1")).toHaveLength(1);
      expect(effectsByHandle.get("h2")).toHaveLength(1);
      expect(new Set([...fixture.state.works.values()].map(work => work.worker_profile))).toEqual(new Set(["default"]));
    } finally {
      Object.assign(global, previous);
    }
  });

  it("resolves the actual Pull Request resource ID before authorizing an Issues API mutation against a frozen PR subject", async () => {
    const require = createRequire(__filename);
    const { withClaimExecution } = require("./work_queue_claim_scope.cjs");
    const { wrapClaimEffectClient } = require("./work_queue_effect_client.cjs");
    const subject = { kind: "pull_request", host: "github.com", repository: REPOSITORY, repository_id: "7", resource_id: "501", number: "7" };
    const { fixture, options } = setup(1, {
      workerPrincipal: "22",
      workDefaults: { subject, payload: { effect_contract: { version: 1, outputs: [{ type: "update_issue", min: 1, max: 1 }] } } },
    });
    finish(options, "h1", "completed");
    await reconcileWorkerClaim(options);
    const update = vi.fn(async () => ({ status: 200, data: { number: 7, id: 701 } }));
    fixture.githubClient.rest.issues = {
      get: vi.fn(async () => ({ status: 200, data: { number: 7, id: 701, pull_request: { url: "https://api.github.com/repos/owner/repo/pulls/7" } } })),
      update,
    };
    const getPull = vi.fn(async () => ({ status: 200, data: { number: 7, id: 501, base: { repo: { id: 7, full_name: REPOSITORY } } } }));
    fixture.githubClient.rest.pulls = { get: getPull };
    const authorize = request => authorizeWorkerClaim({ ...options, ...request });
    await withClaimExecution({ assignment: fixture.assignment, claim_handle: "h1", authorize, effects: [] }, async () => {
      const client = wrapClaimEffectClient(fixture.githubClient, { claim_handle: "h1", authorize, context: options.context });
      await client.rest.issues.update({ owner: "owner", repo: "repo", issue_number: 7, title: "native typed PR target" });
    });
    expect(getPull).toHaveBeenCalledWith(expect.objectContaining({ owner: "owner", repo: "repo", pull_number: 7 }));
    expect(update).toHaveBeenCalledTimes(1);
  });

  it("authenticates foreign-profile metadata without synthesizing a resource target", async () => {
    const { options } = setup(1, { configurePolicy: policy => (policy.pools.default.profiles.default.effect_scope = "foreign/approved") });
    const authority = vi.spyOn(createRequire(__filename)("./work_queue_replay.cjs"), "validateClaimAuthority");
    try {
      expect(await authorizeWorkerClaim({ ...options, message: { type: "work_queue_effect_pass" }, requireCompletion: false })).toMatchObject({ authorized: true, claim_handle: "h1" });
      expect(authority.mock.calls.at(-1)[3]).toEqual({ requireCompletion: false });
      finish(options, "h1", "completed");
      await reconcileWorkerClaim(options);
      expect(await authorizeWorkerClaim({ ...options, message: { type: "work_queue_result" } })).toMatchObject({ authorized: true, claim_handle: "h1" });
      expect(authority.mock.calls.at(-1)[3]).toEqual({ requireCompletion: true });
    } finally {
      authority.mockRestore();
    }
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
    const cancelledMember = fixture.assignment.claims[1];
    expect(cancellation.operations[0]).toMatchObject({
      work_id: cancelledMember.work_id,
      reason: "worker_cancelled",
      retry_not_before: cancellation.at + fixture.policy.pools.default.retry.backoff_ms,
    });
    expect(cancellation.operations[1]).toMatchObject({ work_id: cancelledMember.work_id, reason: "attempts_exhausted" });
    expect(fixture.transactions.flatMap(commit => commit.operations).filter(operation => ["Completion", "Result"].includes(operation.kind) && operation.work_id === cancelledMember.work_id)).toHaveLength(0);
    expect(fixture.transactions.flatMap(commit => commit.operations).filter(operation => operation.kind === "Release" && operation.dispatch_id === fixture.assignment.dispatch_id)).toHaveLength(0);
    expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id).released).toBe(false);
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

  it("uses the shared bounded fatal-UTF-8 transport reader before publishing any finish", async () => {
    const { fixture, options } = setup(1);
    const prefix = Buffer.from('{"version":3,"intent_id":"');
    const suffix = Buffer.from('","kind":"finish","claim_handle":"h1","parameters":{"outcome":"completed"}}\n');
    fs.writeFileSync(options.finishIntentPath, Buffer.concat([prefix, Buffer.from([0xff]), suffix]));
    const before = fixture.transactions;
    expect(() => readFinishIntent(options.finishIntentPath, fixture.assignment)).toThrow(/transport_invalid/);
    await expect(reconcileWorkerClaim(options)).rejects.toThrow(/transport_invalid/);
    expect(fixture.transactions).toEqual(before);
    finish(options, "h1", "completed", "valid-independent-line");
    await expect(reconcileWorkerClaim(options)).rejects.toThrow(/transport_invalid/);
    expect(fixture.transactions).toEqual(before);
  });

  it("retains physical line positions when reporting finish scope errors", () => {
    const { fixture, options } = setup(1);
    fs.writeFileSync(options.finishIntentPath, '\n\n{"version":3,"intent_id":"bad","kind":"finish","parameters":{"outcome":"completed"},"claim_handle":"foreign"}\n');
    expect(readFinishIntent(options.finishIntentPath, fixture.assignment).errors).toMatchObject([{ line: 3, claim_handle: null, code: "work_queue_finish_scope_invalid" }]);
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
    const verify = vi.fn(noWriteClaimVerifier(options));
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

  it.each([0, 1])("uses the durable Completion deadline, not a fresh invocation budget, when terminal recovery is %i ms late", async late => {
    const { fixture, options } = setup(1);
    finish(options, "h1", "completed");
    await reconcileWorkerClaim(options);
    const work = fixture.state.works.get(fixture.assignment.claims[0].work_id);
    const deadline = work.completion_at + fixture.policy.pools.default.reconciliation.deadline_ms;
    const now = deadline + late;
    fixture.githubClient.rest.actions.getWorkflowRunAttempt = vi.fn().mockResolvedValue({ status: 200, data: { ...fixture.nativeRun(), status: "completed", conclusion: "failure" } });
    const verifier = vi.fn(async () => ({ verified: false, effects: "none" }));
    const configured = { ...options, now, verifyEffects: verifier, publishWorkQueueRequest: async args => fixture.append(args.request.kind, args.request.parameters, args.actor, args.request.id, now) };
    const result = await finalizeWorkerResults(configured);
    expect(result.claims.h1).toMatchObject({ state: "delivery_failed", effects: "unknown" });
    expect(verifier).not.toHaveBeenCalled();
    const failure = fixture.transactions.flatMap(commit => commit.operations).find(operation => operation.kind === "DeliveryFailure");
    expect(failure).toMatchObject({ disposition: "unknown", evidence: { kind: "terminal_run", run_id: "42", run_attempt: 1, effects: "unknown" } });
    expect(failure.evidence).not.toHaveProperty("attempts");
    expect(failure.evidence).not.toHaveProperty("receipt");
    expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id).released).toBe(false);
    await finalizeWorkerResults(configured);
    expect(verifier).not.toHaveBeenCalled();
  });

  it("consumes the remaining Completion budget at its exact boundary during the first verification attempt", async () => {
    const { fixture, options } = setup(1);
    finish(options, "h1", "completed");
    await reconcileWorkerClaim(options);
    const completion = fixture.state.works.get(fixture.assignment.claims[0].work_id).completion_at;
    const deadline = completion + fixture.policy.pools.default.reconciliation.deadline_ms;
    let now = deadline - 1;
    const clock = vi.spyOn(Date, "now").mockImplementation(() => now);
    fixture.githubClient.rest.actions.getWorkflowRunAttempt = vi.fn().mockResolvedValue({ status: 200, data: { ...fixture.nativeRun(), status: "completed", conclusion: "failure" } });
    const verifier = vi.fn(async () => {
      now = deadline;
      return { verified: false, effects: "none" };
    });
    try {
      const result = await finalizeWorkerResults({
        ...options,
        now: undefined,
        verifyEffects: verifier,
        publishWorkQueueRequest: async args => fixture.append(args.request.kind, args.request.parameters, args.actor, args.request.id, now),
      });
      expect(result.claims.h1).toMatchObject({ state: "delivery_failed", effects: "unknown" });
      expect(verifier).toHaveBeenCalledTimes(1);
      expect(fixture.transactions.at(-1).at).toBe(deadline);
      expect(fixture.transactions.at(-1).operations[0].evidence.attempts).toBe(1);
    } finally {
      clock.mockRestore();
    }
  });

  it("does not infer termination or release from an expired Completion deadline or a missing native API response", async () => {
    const { fixture, options } = setup(1);
    finish(options, "h1", "completed");
    await reconcileWorkerClaim(options);
    const completion = fixture.state.works.get(fixture.assignment.claims[0].work_id).completion_at;
    const now = completion + fixture.policy.pools.default.reconciliation.deadline_ms;
    const verifier = vi.fn(async () => ({ verified: false, effects: "none" }));
    const before = fixture.transactions;
    const pending = await finalizeWorkerResults({ ...options, now, verifyEffects: verifier });
    expect(pending.claims.h1).toMatchObject({ state: "pending", effects: "unknown", reason: "terminal_evidence_required" });
    expect(verifier).not.toHaveBeenCalled();
    expect(fixture.transactions).toEqual(before);
    verifier.mockClear();
    fixture.githubClient.rest.actions.getWorkflowRunAttempt = vi.fn().mockRejectedValue(new Error("native API unavailable"));
    const unavailable = await finalizeWorkerResults({ ...options, context: fixture.dispatcherContext, workflowRef: `${REPOSITORY}/${DISPATCHER}@${REF}`, now, verifyEffects: verifier });
    expect(unavailable.claims.h1).toMatchObject({ state: "pending", effects: "unknown", reason: "verification_unresolved" });
    expect(verifier).not.toHaveBeenCalled();
    expect(fixture.transactions).toEqual(before);
    expect(fixture.state.dispatches.get(fixture.assignment.dispatch_id).released).toBe(false);
  });

  it("does not start a new verification attempt after the Completion deadline even when its callback could report success", async () => {
    const { fixture, options } = setup(1);
    finish(options, "h1", "completed");
    await reconcileWorkerClaim(options);
    const now = fixture.state.works.get(fixture.assignment.claims[0].work_id).completion_at + fixture.policy.pools.default.reconciliation.deadline_ms;
    fixture.githubClient.rest.actions.getWorkflowRunAttempt = vi.fn().mockResolvedValue({ status: 200, data: { ...fixture.nativeRun(), status: "completed", conclusion: "success" } });
    const verifier = vi.fn(async () => ({ verified: true, contractVerified: true, receipt: "trusted:late-readback", descriptor: { delivered: true }, effects: "none" }));
    const result = await finalizeWorkerResults({
      ...options,
      now,
      verifyEffects: verifier,
      publishWorkQueueRequest: async args => fixture.append(args.request.kind, args.request.parameters, args.actor, args.request.id, now),
    });
    expect(result.claims.h1).toMatchObject({ state: "delivery_failed", effects: "unknown" });
    expect(verifier).not.toHaveBeenCalled();
    expect(fixture.transactions.flatMap(commit => commit.operations).some(operation => operation.kind === "Result")).toBe(false);
    expect(fixture.transactions.at(-1).operations[0].evidence).not.toHaveProperty("attempts");
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

  it("authenticates terminal original native evidence before any independent recovery receipt verification", async () => {
    const { fixture, options } = setup(1);
    finish(options, "h1", "completed");
    await reconcileWorkerClaim(options);
    let terminal = false;
    const events = [];
    const get = fixture.githubClient.rest.actions.getWorkflowRun;
    fixture.githubClient.rest.actions.getWorkflowRun = async args => {
      const response = await get(args);
      return { ...response, data: { ...response.data, ...(args.run_id === "42" && terminal ? { status: "completed", conclusion: "failure" } : {}) } };
    };
    const getAttempt = fixture.githubClient.rest.actions.getWorkflowRunAttempt;
    fixture.githubClient.rest.actions.getWorkflowRunAttempt = async args => {
      const response = await getAttempt(args);
      events.push(`native:${response.data.status}:${args.attempt_number}`);
      return response;
    };
    const verify = vi.fn(async () => {
      expect(events[0]).toBe("native:completed:1");
      events.push("verify");
      return { verified: false, contractVerified: false, receipt: "trusted:terminal-no-effects", effects: "none" };
    });
    const recovery = { ...options, context: fixture.dispatcherContext, workflowRef: `${REPOSITORY}/${DISPATCHER}@${REF}`, verifyEffects: verify };
    const before = fixture.transactions;
    expect((await finalizeWorkerResults(recovery)).claims.h1).toMatchObject({ state: "pending", effects: "unknown", reason: "terminal_evidence_required" });
    expect(verify).not.toHaveBeenCalled();
    expect(fixture.transactions).toEqual(before);
    terminal = true;
    events.length = 0;
    expect((await finalizeWorkerResults(recovery)).claims.h1).toMatchObject({ state: "delivery_failed", effects: "none" });
    expect(verify).toHaveBeenCalledTimes(fixture.policy.pools.default.reconciliation.max_attempts);
    expect(events.indexOf("native:completed:1")).toBeLessThan(events.indexOf("verify"));
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

  it("requires and preserves positive partial-effects receipt evidence in terminal delivery failure", async () => {
    const { fixture, options } = setup(1);
    finish(options, "h1", "completed");
    await reconcileWorkerClaim(options);
    fixture.githubClient.rest.actions.getWorkflowRunAttempt = vi.fn().mockResolvedValue({ status: 200, data: { ...fixture.nativeRun(), status: "completed", conclusion: "failure" } });
    const verifier = vi.fn(async () => ({ verified: false, contractVerified: false, receipt: "trusted:partial-effects", effects: "partial" }));
    const result = await finalizeWorkerResults({ ...options, verifyEffects: verifier });
    expect(result.claims.h1).toMatchObject({ state: "delivery_failed", effects: "partial" });
    expect(verifier).toHaveBeenCalledTimes(fixture.policy.pools.default.reconciliation.max_attempts);
    expect(fixture.transactions.at(-1).operations[0]).toMatchObject({ kind: "DeliveryFailure", disposition: "partial", evidence: { effects: "partial", receipt: "trusted:partial-effects" } });
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
