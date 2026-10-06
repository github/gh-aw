// @ts-check
import { describe, expect, it, vi } from "vitest";
import { claimControlReceipts, controlReceiptForRequest, readClaimQueueControls, verifyClaimQueueControl } from "./work_queue_control_receipts.cjs";
import { intentContext, acceptedSubmissionParameters } from "./work_queue_dispatch.cjs";
import { requestForIntent } from "./work_queue_intents.cjs";
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

  it("includes undeclared durable writes even without transport receipts or messages", async () => {
    const { fixture, options, request } = await setup();
    expect(claimControlReceipts(fixture.state, fixture.assignment, "h1").map(receipt => receipt.request_id)).toEqual([request.id]);
    const read = await readClaimQueueControls({ ...options, claim_handle: "h1" });
    expect(read.controls_digest).toMatch(/^[a-f0-9]{64}$/);
    expect(read.controls.length).toBeGreaterThan(0);
    expect(claimControlReceipts(fixture.state, fixture.assignment, "h2")).toEqual([]);
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
