import { describe, expect, it } from "vitest";
import { createRequire } from "node:module";
const require = createRequire(import.meta.url);
const { createHarness } = require("./work_queue_delivery_evidence.test_helpers.cjs");

describe("worker durable delivery publication", () => {
  it("publishes evidence before a transient issue readback failure without replaying the write", async () => {
    const h = createHarness();
    try {
      await h.workerPass();
      const member = h.fixture.assignment.claims[0];
      const evidence = h.projection.deliveryEvidence.get(member.claim_id);
      expect(evidence, JSON.stringify(h.logs)).toMatchObject({ kind: "DeliveryEvidence", outputs: [{ type: "create_issue", verifier: "builtin" }] });
      expect(evidence.outputs[0].receipt.number).toBe(19);
      expect(h.projection.works.get(member.work_id).barrier).toBe("pending");
      expect(h.operations("DeliveryEvidence")).toHaveLength(1);
      expect(h.state.writes).toBe(1);
      h.restartForMaintenance();
      const maintenance = await h.maintenance();
      expect(maintenance.results[0].delivery.claims.h1.state, JSON.stringify(h.logs)).toBe("result");
      expect(h.settlement(member)).toEqual({ barrier: "verified", evidenceRetained: false, results: 1, failures: 0, writes: 1 });
    } finally {
      h.dispose();
    }
  });

  it("publishes an empty inventory for a zero-message no-write contract", async () => {
    const h = createHarness({ contract: { kind: "none" }, messages: [] });
    try {
      await h.workerPass({ finalizeResults: async () => ({ claims: { h1: { state: "pending" } } }) });
      expect(h.projection.deliveryEvidence.get(h.fixture.assignment.claims[0].claim_id)).toMatchObject({ outputs: [], effects: [] });
      expect(h.state.writes).toBe(0);
      h.restartForMaintenance();
      await h.maintenance();
      expect(h.projection.works.get(h.fixture.assignment.claims[0].work_id).barrier).toBe("verified");
      expect(h.state.writes).toBe(0);
    } finally {
      h.dispose();
    }
  });

  it("keeps undeclared builtin fields unsupported without publishing partial evidence", async () => {
    const h = createHarness({ messages: [{ type: "create_issue", title: "Required report", body: "Report body", unsupported: true }] });
    try {
      await h.workerPass();
      expect(h.projection.deliveryEvidence.size).toBe(0);
      expect(h.projection.works.get(h.fixture.assignment.claims[0].work_id).barrier).toBe("pending");
      expect(h.state.writes).toBe(1);
    } finally {
      h.dispose();
    }
  });

  it.each([false, true])("attempts persisted evidence before expiry even after the delivery deadline (tampered=%s)", async tampered => {
    const h = createHarness();
    try {
      await h.workerPass();
      const member = h.fixture.assignment.claims[0];
      const deadline = h.projection.works.get(member.work_id).completion_at + h.projection.policy.pools.default.reconciliation.deadline_ms;
      h.restartForMaintenance({ tampered });
      await h.maintenance({ now: deadline + 1 });
      expect(h.settlement(member)).toEqual({ barrier: tampered ? "failed" : "verified", evidenceRetained: false, results: tampered ? 0 : 1, failures: tampered ? 1 : 0, writes: 1 });
      if (tampered) expect(h.operations("DeliveryFailure")[0].evidence.attempts).toBeGreaterThan(0);
    } finally {
      h.dispose();
    }
  });

  it("keeps receipts and failures isolated between two completed Claims", async () => {
    const h = createHarness({ count: 2, messages: [{ type: "create_issue", claim_handle: "h1", title: "Required report", body: "Report body" }] });
    try {
      await h.workerPass();
      h.restartForMaintenance();
      await h.maintenance();
      expect(h.projection.works.get(h.fixture.assignment.claims[0].work_id).barrier).toBe("verified");
      expect(h.projection.works.get(h.fixture.assignment.claims[1].work_id).barrier).toBe("failed");
      expect(h.operations("Result")).toHaveLength(1);
      expect(h.state.writes).toBe(1);
    } finally {
      h.dispose();
    }
  });

  it("rejects an authenticated evidence receipt whose native issue number was changed", async () => {
    const h = createHarness();
    try {
      const evidenceModule = require("./work_queue_delivery_evidence.cjs");
      const collect = evidenceModule.collectDeliveryEvidence;
      evidenceModule.collectDeliveryEvidence = options => {
        const evidence = collect(options);
        if (evidence.outputs?.[0]?.receipt) evidence.outputs[0].receipt.number = 20;
        return evidence;
      };
      await h.workerPass();
      const member = h.fixture.assignment.claims[0];
      expect(h.projection.deliveryEvidence.get(member.claim_id).outputs[0].receipt.number).toBe(20);
      h.restartForMaintenance();
      await h.maintenance();
      expect(h.settlement(member)).toEqual({ barrier: "failed", evidenceRetained: false, results: 0, failures: 1, writes: 1 });
      expect(h.operations("DeliveryFailure")[0].evidence.attempts).toBeGreaterThan(0);
    } finally {
      h.dispose();
    }
  });

  it("does not promote copied artifact-shaped evidence without a checked DeliveryEvidence operation", async () => {
    const h = createHarness();
    try {
      const fs = require("node:fs");
      const path = require("node:path");
      fs.mkdirSync(path.join(h.root, "delivery"), { recursive: true });
      fs.writeFileSync(path.join(h.root, "delivery", "h1.json"), JSON.stringify({ verified: true, claim_handle: "h1", descriptor: { outputs: [{ type: "create_issue", repository: "owner/repo", number: 19 }] } }));
      h.restartForMaintenance();
      await h.maintenance();
      expect(h.projection.works.get(h.fixture.assignment.claims[0].work_id).barrier).toBe("failed");
      expect(h.operations("Result")).toHaveLength(0);
      expect(h.state.writes).toBe(0);
    } finally {
      h.dispose();
    }
  });
});
