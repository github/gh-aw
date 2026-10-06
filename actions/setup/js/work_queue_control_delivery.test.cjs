// @ts-check
import { describe, it, expect } from "vitest";
import { queueFixture, REF, REPOSITORY, WORKFLOW } from "./work_queue_lifecycle.test_helpers.cjs";
import { intentContext } from "./work_queue_dispatch.cjs";
import { normalizeSubmitParameters, requestForIntent } from "./work_queue_intents.cjs";
import { readDeliveryControlInventory, verifyClaimDelivery } from "./work_queue_delivery.cjs";
import { withClaimExecution } from "./work_queue_claim_scope.cjs";
const { verifyClaimQueueControl } = require("./work_queue_control_receipts.cjs");

async function setup(contract, withControl = true) {
  const fixture = queueFixture({ bound: true, count: 1, workDefaults: { payload: { effect_contract: contract } } });
  const member = fixture.assignment.claims[0];
  fixture.append("finish", { dispatch_id: fixture.assignment.dispatch_id, claim_handle: member.handle, outcome: "completed" }, { ...fixture.workerActor, dispatch_id: fixture.assignment.dispatch_id, claim_handle: member.handle });
  const options = {
    assignment: fixture.assignment,
    claim_handle: member.handle,
    context: fixture.workerContext,
    githubClient: fixture.githubClient,
    workflowRef: `${REPOSITORY}/${WORKFLOW}@${REF}`,
    readWorkQueueLog: fixture.readWorkQueueLog,
  };
  const message = { type: "work_queue_submit", intent_id: "child-request", claim_handle: member.handle, parameters: { nodes: [{ graph_id: "children", node_key: "child", payload: { effect_contract: { kind: "none" } } }] } };
  if (withControl) {
    const context = await intentContext(options, { claim_handle: member.handle });
    const parameters = normalizeSubmitParameters(message.parameters, fixture.policy, context.created_at);
    const request = requestForIntent(context, message.intent_id, "submit", parameters);
    fixture.append("submit", parameters, { ...fixture.workerActor, dispatch_id: fixture.assignment.dispatch_id, claim_handle: member.handle }, request.id);
  }
  return { fixture, options, message };
}

describe("complete output contracts include independent durable queue controls", () => {
  it("refreshes the real manager's independent delivery evidence rather than replaying effects or reusing a cached verdict", async () => {
    const fs = require("node:fs");
    const path = require("node:path");
    const { randomUUID } = require("node:crypto");
    const root = path.resolve(".queue-validation-cache", `claim-recheck-${randomUUID()}`);
    const manager = require("./safe_output_handler_manager.cjs");
    const delivery = require("./work_queue_delivery.cjs");
    const scope = require("./work_queue_claim_scope.cjs");
    const { fixture, options, message } = await setup({ kind: "none" }, false);
    const authorize = async request => ({ authorized: true, claim_handle: request.claim_handle });
    const oldGithub = global.github;
    const oldContext = global.context;
    Reflect.set(global, "github", fixture.githubClient);
    Reflect.set(global, "context", fixture.workerContext);
    let recheck;
    let reads = 0;
    const settings = {
      deliveryArtifactRoot: root,
      readControlInventory: async () => {
        reads++;
        return delivery.readDeliveryControlInventory(options);
      },
      registerVerification: (messages, results) => {
        recheck = () => manager.settleClaimDelivery({ assignment: fixture.assignment }, messages, results, { ...settings, registerVerification: undefined });
      },
    };
    try {
      await scope.withClaimExecution({ assignment: fixture.assignment, claim_handle: "h1", authorize }, async () => {
        expect((await manager.settleClaimDelivery({ assignment: fixture.assignment }, [], [], settings)).verification).toBe("verified");
        const context = await intentContext(options, { claim_handle: "h1" });
        const parameters = normalizeSubmitParameters(message.parameters, fixture.policy, context.created_at);
        const request = requestForIntent(context, message.intent_id, "submit", parameters);
        fixture.append("submit", parameters, { ...fixture.workerActor, dispatch_id: fixture.assignment.dispatch_id, claim_handle: "h1" }, request.id);
        expect((await recheck()).verification).toBe("unknown");
      });
      expect(reads).toBe(2);
    } finally {
      Reflect.set(global, "github", oldGithub);
      Reflect.set(global, "context", oldContext);
      fs.rmSync(root, { recursive: true, force: true });
    }
  });

  it("verifies exact accepted child submission and forwards checked control digest to the Result barrier", async () => {
    const { fixture, options, message } = await setup({ version: 1, outputs: [{ type: "work_queue_submit", min: 1, max: 1 }] });
    const authorize = async request => ({ authorized: true, claim_handle: request.claim_handle });
    const before = fixture.transactions;
    await withClaimExecution({ assignment: fixture.assignment, claim_handle: "h1", authorize }, async () => {
      const inventory = await readDeliveryControlInventory(options);
      const verification = {
        ...options,
        messages: [message],
        results: [{ messageIndex: 0, success: true, result: null }],
        requireControlInventory: true,
        readControlInventory: async () => inventory,
        authorize,
        verifyOutput: input => verifyClaimQueueControl({ ...options, message: input.message, inventory }),
      };
      const result = await verifyClaimDelivery(verification);
      expect(result.verification).toBe("verified");
      expect(result.controls_digest).toBe(inventory.controls_digest);
      expect(result.descriptor.outputs[0].resource.kind).toBe("queue_commit");
      const changed = { ...message, parameters: { nodes: [{ ...message.parameters.nodes[0], payload: { changed: true } }] } };
      expect((await verifyClaimDelivery({ ...verification, messages: [changed] })).verification).toBe("unknown");
    });
    expect(fixture.transactions).toEqual(before);
  });

  it("does not verify undeclared durable effects even if all local transport messages vanished", async () => {
    const { fixture, options } = await setup({ kind: "none" });
    const authorize = async request => ({ authorized: true, claim_handle: request.claim_handle });
    await withClaimExecution({ assignment: fixture.assignment, claim_handle: "h1", authorize }, async () => {
      const inventory = await readDeliveryControlInventory(options);
      const result = await verifyClaimDelivery({ ...options, messages: [], results: [], authorize, requireControlInventory: true, readControlInventory: async () => inventory });
      expect(result.verification).toBe("unknown");
      expect(result.reason).toMatch(/undeclared durable queue-control/);
      expect(result.descriptor).toBeNull();
    });
  });

  it("requires a privately checked zero-effect inventory and rejects copied or missing proofs", async () => {
    const { fixture, options } = await setup({ kind: "none" }, false);
    const authorize = async request => ({ authorized: true, claim_handle: request.claim_handle });
    await withClaimExecution({ assignment: fixture.assignment, claim_handle: "h1", authorize }, async () => {
      const inventory = await readDeliveryControlInventory(options);
      const verification = { ...options, messages: [], results: [], authorize, requireControlInventory: true };
      expect((await verifyClaimDelivery({ ...verification, readControlInventory: async () => inventory })).verification).toBe("verified");
      expect((await verifyClaimDelivery({ ...verification, readControlInventory: async () => structuredClone(inventory) })).reason).toMatch(/untrusted/);
      expect((await verifyClaimDelivery(verification)).reason).toMatch(/inventory is required/);
    });
  });
});
