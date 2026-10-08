// @ts-check
import { describe, expect, it, vi } from "vitest";
import { claimControlReceipts, controlReceiptForRequest, readClaimQueueControls, isTrustedClaimQueueControlInventory, isUncommittedClaimDispatchNext, verifyClaimQueueControl } from "./work_queue_control_receipts.cjs";
import { intentContext, acceptedSubmissionParameters } from "./work_queue_dispatch.cjs";
import { normalizeDispatchParameters, requestForIntent } from "./work_queue_intents.cjs";
import { queueFixture, REF, REPOSITORY, WORKFLOW } from "./work_queue_lifecycle.test_helpers.cjs";

async function setup(count = 2) {
  const fixture = queueFixture({ bound: true, count });
  for (const member of fixture.assignment.claims)
    fixture.append("finish", { dispatch_id: fixture.assignment.dispatch_id, claim_handle: member.handle, outcome: "completed" }, { ...fixture.workerActor, dispatch_id: fixture.assignment.dispatch_id, claim_handle: member.handle });
  const options = { assignment: fixture.assignment, context: fixture.workerContext, githubClient: fixture.githubClient, workflowRef: `${REPOSITORY}/${WORKFLOW}@${REF}`, readWorkQueueLog: fixture.readWorkQueueLog };
  const trusted = await intentContext(options, { claim_handle: "h1" });
  const message = { type: "work_queue_submit", intent_id: "accepted-control", claim_handle: "h1", parameters: { nodes: [{ graph_id: "controls", node_key: "child", payload: { plan: "immutable child" } }] } };
  const { normalizeSubmitParameters } = await import("./work_queue_intents.cjs");
  const parameters = normalizeSubmitParameters(message.parameters, fixture.policy, trusted.created_at);
  const request = requestForIntent(trusted, message.intent_id, "submit", parameters);
  fixture.append("submit", parameters, { ...fixture.workerActor, dispatch_id: fixture.assignment.dispatch_id, claim_handle: "h1" }, request.id);
  return { fixture, options, message, request, trusted };
}

describe("independent Claim-scoped queue control readback", () => {
  it("returns exact compact completed-Claim receipts from checked ledger and actual native attempt", async () => {
    const { fixture, options, message, request } = await setup();
    const inventory = await readClaimQueueControls({ ...options, claim_handle: "h1" });
    expect(isTrustedClaimQueueControlInventory(inventory, fixture.assignment, "h1")).toBe(true);
    expect(inventory.controls).toHaveLength(1);
    expect(inventory.controls[0]).toMatchObject({
      type: message.type,
      claim_handle: "h1",
      claim_id: fixture.assignment.claims[0].claim_id,
      work_id: fixture.assignment.claims[0].work_id,
      request_id: request.id,
      completion_id: fixture.state.works.get(fixture.assignment.claims[0].work_id).completion_id,
      writes: { works: 1, claims: 0, dispatches: 0 },
    });
    expect(Object.isFrozen(inventory.controls[0])).toBe(true);
    const proof = await verifyClaimQueueControl({ ...options, claim_handle: "h1", message, inventory });
    expect(proof).toMatchObject({ verified: true, claim_handle: "h1", resource: { kind: "queue_commit", repository: REPOSITORY, id: inventory.controls[0].commit_id }, evidence: inventory.controls[0] });
    expect(controlReceiptForRequest(fixture.state, request.id)).toEqual(inventory.controls[0]);
  });

  it("never trusts serialized inventory, foreign sibling scope, changed parameters or an unaccepted request", async () => {
    const { options, message } = await setup();
    const inventory = await readClaimQueueControls({ ...options, claim_handle: "h1" });
    await expect(verifyClaimQueueControl({ ...options, message, inventory: JSON.parse(JSON.stringify(inventory)) })).rejects.toThrow(/untrusted/);
    await expect(verifyClaimQueueControl({ ...options, message: { ...message, claim_handle: "h2" }, inventory })).rejects.toThrow(/untrusted/);
    const changed = { ...message, parameters: { nodes: [{ ...message.parameters.nodes[0], payload: { plan: "replacement" } }] } };
    expect(await verifyClaimQueueControl({ ...options, message: changed, inventory })).toMatchObject({ verified: false, effects: "unknown", reason: "queue_control_request_mismatch" });
    expect(await verifyClaimQueueControl({ ...options, message: { ...message, intent_id: "never-accepted" }, inventory })).toMatchObject({ verified: false, effects: "unknown", reason: "queue_control_not_committed" });
    expect((await readClaimQueueControls({ ...options, claim_handle: "h2" })).controls).toEqual([]);
  });

  it("brands even zero-control inventories against the complete immutable assignment and original Claim", async () => {
    const { fixture, options } = await setup();
    const inventory = await readClaimQueueControls({ ...options, claim_handle: "h2" });
    expect(inventory.controls).toEqual([]);
    expect(isTrustedClaimQueueControlInventory(inventory, fixture.assignment, "h2")).toBe(true);
    expect(isTrustedClaimQueueControlInventory(inventory, JSON.parse(JSON.stringify(fixture.assignment)), "h2")).toBe(true);
    for (const fabricated of [null, undefined, false, "verified", {}, [], { ...inventory }, JSON.parse(JSON.stringify(inventory)), Object.create(inventory), new Proxy(inventory, {})]) {
      expect(isTrustedClaimQueueControlInventory(fabricated, fixture.assignment, "h2")).toBe(false);
    }
    for (const handle of [undefined, null, "", "h1", "foreign"]) expect(isTrustedClaimQueueControlInventory(inventory, fixture.assignment, handle)).toBe(false);
    for (const field of ["dispatch_id", "request_id", "commit_id", "policy_epoch", "pool", "worker_profile"]) {
      expect(isTrustedClaimQueueControlInventory(inventory, { ...fixture.assignment, [field]: "different" }, "h2")).toBe(false);
    }
    const altered = JSON.parse(JSON.stringify(fixture.assignment));
    altered.claims[1].work.plan = "forged payload";
    expect(isTrustedClaimQueueControlInventory(inventory, altered, "h2")).toBe(false);
    for (const assignment of [null, undefined, {}, { ...fixture.assignment, claims: [] }]) expect(isTrustedClaimQueueControlInventory(inventory, assignment, "h2")).toBe(false);
    expect(Object.isFrozen(inventory)).toBe(true);
    expect(Object.isFrozen(inventory.controls)).toBe(true);
  });

  it("includes undeclared durable writes even without transport receipts or messages", async () => {
    const { fixture, options, request } = await setup();
    expect(claimControlReceipts(fixture.state, fixture.assignment, "h1").map(receipt => receipt.request_id)).toEqual([request.id]);
    const read = await readClaimQueueControls({ ...options, claim_handle: "h1" });
    expect(read.controls_digest).toMatch(/^[a-f0-9]{64}$/);
    expect(read.controls.length).toBeGreaterThan(0);
    expect(claimControlReceipts(fixture.state, fixture.assignment, "h2")).toEqual([]);
  });

  it("classifies only independently checked absent dispatch requests as non-output without minting delivery evidence", async () => {
    const { fixture, options, message: submit, trusted } = await setup();
    const inventory = await readClaimQueueControls({ ...options, claim_handle: "h1" });
    const message = { type: "work_queue_dispatch_next", intent_id: "unaccepted-dispatch", claim_handle: "h1", parameters: { pool: "default", max_claims: 1, max_dispatches: 1 } };
    const argumentsFor = { assignment: fixture.assignment, claim_handle: "h1", message, inventory };
    const before = fixture.transactions;
    expect(isUncommittedClaimDispatchNext(argumentsFor)).toBe(true);
    expect(await verifyClaimQueueControl({ ...options, message, inventory })).toMatchObject({ verified: false, effects: "unknown", reason: "queue_control_not_committed" });
    expect(isUncommittedClaimDispatchNext({ ...argumentsFor, message: { ...message, intent_id: submit.intent_id } })).toBe(false);
    expect(isUncommittedClaimDispatchNext({ ...argumentsFor, message: { ...submit, intent_id: "unaccepted-submit" } })).toBe(false);
    expect(fixture.transactions).toEqual(before);
    const parameters = normalizeDispatchParameters(message.parameters, fixture.policy, 4096);
    const request = requestForIntent(trusted, message.intent_id, "dispatch_next", parameters);
    const accepted = fixture.append("dispatch_next", parameters, { ...fixture.workerActor, dispatch_id: fixture.assignment.dispatch_id, claim_handle: "h1" }, request.id);
    expect(accepted.persisted).toBe(true);
    const refreshed = await readClaimQueueControls({ ...options, claim_handle: "h1" });
    expect(refreshed.controls_digest).not.toBe(inventory.controls_digest);
    expect(isUncommittedClaimDispatchNext({ ...argumentsFor, inventory: refreshed })).toBe(false);
    expect(await verifyClaimQueueControl({ ...options, message, inventory: refreshed })).toMatchObject({ verified: true, evidence: { type: "work_queue_dispatch_next", request_id: request.id } });
  });

  it("keeps a real zero-operation fair-prefix evaluation uncommitted and non-output", async () => {
    const fixture = queueFixture({ bound: true, count: 1 });
    fixture.append("finish", { dispatch_id: fixture.assignment.dispatch_id, claim_handle: "h1", outcome: "completed" }, { ...fixture.workerActor, dispatch_id: fixture.assignment.dispatch_id, claim_handle: "h1" });
    const options = { assignment: fixture.assignment, context: fixture.workerContext, githubClient: fixture.githubClient, workflowRef: `${REPOSITORY}/${WORKFLOW}@${REF}`, readWorkQueueLog: fixture.readWorkQueueLog };
    const trusted = await intentContext(options, { claim_handle: "h1" });
    const message = { type: "work_queue_dispatch_next", intent_id: "zero-prefix", claim_handle: "h1", parameters: { pool: "default", max_claims: 1, max_dispatches: 1 } };
    const parameters = normalizeDispatchParameters(message.parameters, fixture.policy, 4096);
    const request = requestForIntent(trusted, message.intent_id, "dispatch_next", parameters);
    const before = fixture.transactions;
    const evaluated = fixture.append("dispatch_next", parameters, { ...fixture.workerActor, dispatch_id: fixture.assignment.dispatch_id, claim_handle: "h1" }, request.id);
    expect(evaluated.operations).toEqual([]);
    expect(evaluated.persisted).toBe(false);
    expect(fixture.transactions).toEqual(before);
    expect(fixture.state.requests.has(request.id)).toBe(false);
    const inventory = await readClaimQueueControls({ ...options, claim_handle: "h1" });
    expect(inventory.controls).toEqual([]);
    expect(isUncommittedClaimDispatchNext({ assignment: fixture.assignment, claim_handle: "h1", message, inventory })).toBe(true);
    expect(await verifyClaimQueueControl({ ...options, message, inventory })).toMatchObject({ verified: false, effects: "unknown", reason: "queue_control_not_committed" });
  });

  it("rejects forged, foreign and malformed non-output classifications", async () => {
    const { fixture, options } = await setup();
    const inventory = await readClaimQueueControls({ ...options, claim_handle: "h1" });
    const message = { type: "work_queue_dispatch_next", intent_id: "not-an-output", claim_handle: "h1", parameters: { pool: "default", max_claims: 1, max_dispatches: 1 } };
    const argumentsFor = { assignment: fixture.assignment, claim_handle: "h1", message, inventory };
    for (const fabricated of [undefined, {}, { ...inventory }, JSON.parse(JSON.stringify(inventory)), new Proxy(inventory, {})]) {
      expect(() => isUncommittedClaimDispatchNext({ ...argumentsFor, inventory: fabricated })).toThrow(/untrusted/);
    }
    expect(() => isUncommittedClaimDispatchNext({ ...argumentsFor, claim_handle: "h2" })).toThrow(/scope_invalid/);
    expect(() => isUncommittedClaimDispatchNext({ ...argumentsFor, claim_handle: "h2", message: { ...message, claim_handle: "h2" } })).toThrow(/untrusted/);
    const altered = JSON.parse(JSON.stringify(fixture.assignment));
    altered.claims[0].work.plan = "forged immutable payload";
    expect(() => isUncommittedClaimDispatchNext({ ...argumentsFor, assignment: altered })).toThrow(/untrusted/);
    for (const parameters of [
      { ...message.parameters, work_id: "agent-selected-work" },
      { ...message.parameters, max_claims: 0 },
      { ...message.parameters, max_dispatches: 0 },
    ]) {
      expect(() => isUncommittedClaimDispatchNext({ ...argumentsFor, message: { ...message, parameters } })).toThrow();
    }
    expect(() => isUncommittedClaimDispatchNext({ ...argumentsFor, message: { ...message, type: "unsupported_control" } })).toThrow(/scope_invalid/);
    expect(() => isUncommittedClaimDispatchNext({ ...argumentsFor, message: { ...message, job_success: true } })).toThrow();
  });

  it("reads immutable accepted effects after exact native Release without new authority or writes", async () => {
    const { fixture, options, message } = await setup(1);
    const evidence = {
      kind: "terminal_run",
      source: "github_api",
      repository: REPOSITORY,
      workflow: WORKFLOW,
      ref: REF,
      principal: fixture.binding.principal,
      checked_at: fixture.at,
      run_id: "42",
      run_attempt: 1,
      status: "completed",
      conclusion: "success",
    };
    fixture.append("release", { operations: [{ kind: "Release", dispatch_id: fixture.assignment.dispatch_id, evidence }] }, { role: "reconciler", principal: "11", repository: REPOSITORY });
    const before = fixture.transactions;
    expect(await verifyClaimQueueControl({ ...options, message })).toMatchObject({ verified: true });
    expect(fixture.transactions).toEqual(before);
    await expect(intentContext(options, { claim_handle: "h1" })).rejects.toThrow(/claim_ineffective/);
  });

  it("fails closed when actual native principal or original-attempt identity differs", async () => {
    const { fixture, options } = await setup();
    fixture.githubClient.rest.actions.getWorkflowRunAttempt = vi.fn().mockResolvedValue({ status: 200, data: { ...fixture.nativeRun(), triggering_actor: { id: 99 } } });
    await expect(readClaimQueueControls({ ...options, claim_handle: "h1" })).rejects.toThrow(/principal_mismatch/);
  });

  it("verifies accepted normalized parameters without replaying submission or effects", async () => {
    const { fixture, trusted, message, request } = await setup();
    const prior = fixture.state.requests.get(request.id);
    expect(acceptedSubmissionParameters(fixture.state, trusted, message.parameters, prior)).toEqual(prior.request.parameters);
  });
});
