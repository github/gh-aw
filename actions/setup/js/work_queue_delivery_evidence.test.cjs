import { describe, expect, it } from "vitest";
import { createRequire } from "node:module";
const require = createRequire(import.meta.url);
const { queueFixture } = require("./work_queue_lifecycle.test_helpers.cjs");
const { completedClaimFixture } = require("./work_queue_delivery_evidence.test_helpers.cjs");
const { collectDeliveryEvidence, publishDeliveryEvidence } = require("./work_queue_delivery_evidence.cjs");
const { claimIdentity } = require("./work_queue_claim_scope.cjs");
const { requestIdForIntent } = require("./work_queue_intents.cjs");

function options(fixture) {
  return { ...fixture, context: fixture.workerContext, claim_handle: "h1", workflowRef: `owner/repo/.github/workflows/worker.lock.yml@${"a".repeat(40)}` };
}

describe("durable delivery evidence", () => {
  it("publishes one authenticated immutable evidence operation for an empty no-write pass", async () => {
    const fixture = completedClaimFixture();
    const evidence = collectDeliveryEvidence({ ...options(fixture), messages: [], results: [], effects: [] });
    expect(evidence).toEqual({ outputs: [], effects: [] });
    expect((await publishDeliveryEvidence({ ...options(fixture), ...evidence })).published).toBe(true);
    const member = fixture.assignment.claims[0];
    expect(fixture.state.deliveryEvidence.get(member.claim_id)).toMatchObject({ kind: "DeliveryEvidence", run_id: "42", outputs: [], effects: [] });
    expect(fixture.state.works.get(member.work_id).barrier).toBe("pending");
    expect(await publishDeliveryEvidence({ ...options(fixture), outputs: [], effects: [] })).toEqual({ published: false, reason: "delivery_evidence_exists" });
  });

  it("does not publish before Completion", async () => {
    const fixture = queueFixture({ count: 1, bound: true });
    expect(await publishDeliveryEvidence({ ...options(fixture), outputs: [], effects: [] })).toEqual({ published: false, reason: "claim_ineffective" });
    expect(fixture.state.deliveryEvidence.size).toBe(0);
  });

  it("derives control request identities from authenticated original-worker authority", async () => {
    const fixture = completedClaimFixture();
    const controls = [{ type: "work_queue_dispatch_next", claim_handle: "h1", intent_id: "next", parameters: { pool: "default" } }];
    const evidence = collectDeliveryEvidence({ ...options(fixture), controls });
    await publishDeliveryEvidence({ ...options(fixture), ...evidence });
    const entry = fixture.state.deliveryEvidence.get(fixture.assignment.claims[0].claim_id).outputs[0];
    const trusted = { ...fixture.workerActor, dispatch_id: fixture.assignment.dispatch_id, claim_handle: "h1", authenticated: true, roles: ["worker"] };
    expect(entry.receipt.request_id).toBe(requestIdForIntent(trusted, "next"));
  });

  it("fails closed rather than losing unsuccessful optional outputs after restart", () => {
    const fixture = completedClaimFixture();
    for (const result of [undefined, { success: false }, { success: true, deferred: true }, { success: true, delegated: true }, { success: true, skipped: true }, { success: true, cancelled: true }]) {
      const evidence = collectDeliveryEvidence({ ...options(fixture), messages: [{ type: "create_issue" }], results: result ? [{ messageIndex: 0, ...result }] : [] });
      expect(evidence).toEqual({ unsupported: "delivery_evidence_receipt_missing:create_issue" });
    }
  });

  it("bounds the complete inventory and keeps no-write receipts free of agent content", () => {
    const fixture = completedClaimFixture();
    const evidence = collectDeliveryEvidence({ ...options(fixture), messages: [{ type: "noop", message: "not a receipt" }], results: [{ messageIndex: 0, success: true }] });
    expect(evidence).toEqual({ outputs: [{ index: 0, type: "noop", verifier: "none" }], effects: [] });
    const controls = Array.from({ length: 129 }, (_, index) => ({ type: "work_queue_dispatch_next", claim_handle: "h1", intent_id: `next-${index}` }));
    expect(collectDeliveryEvidence({ ...options(fixture), controls })).toEqual({ unsupported: "delivery_evidence_oversized" });
    const effects = [{ kind: "git_ref", repository: "owner/repo", outcome: "succeeded", id: "x".repeat(32768), ...claimIdentity("h1", fixture.assignment) }];
    expect(collectDeliveryEvidence({ ...options(fixture), effects })).toEqual({ unsupported: "delivery_evidence_oversized" });
    effects[0].claim_id = "foreign-claim";
    expect(collectDeliveryEvidence({ ...options(fixture), effects })).toEqual({ unsupported: "delivery_evidence_foreign_effect" });
  });
});
