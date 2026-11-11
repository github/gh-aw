import { describe, expect, it } from "vitest";
import { createRequire } from "node:module";
import fs from "node:fs";
import path from "node:path";
import crypto from "node:crypto";

const require = createRequire(import.meta.url);
const { createHarness } = require("./work_queue_delivery_evidence.test_helpers.cjs");
const REF = "a".repeat(40);
const BLOB = "b".repeat(40);
const TREE = "c".repeat(40);
const COMMIT = "d".repeat(40);

function pendingEvidence(h, verifier) {
  const member = h.fixture.assignment.claims[0];
  const evidence = h.operations("DeliveryEvidence");
  expect(evidence).toHaveLength(1);
  expect(evidence[0].outputs[0].verifier).toBe(verifier);
  expect(h.projection.deliveryEvidence.get(member.claim_id)).toEqual(evidence[0]);
  expect(h.projection.works.get(member.work_id).barrier).toBe("pending");
  expect(h.operations("Result")).toHaveLength(0);
  expect(h.operations("DeliveryFailure")).toHaveLength(0);
  return evidence[0];
}

function assertSettlement(h, maintenance, tampered, writes) {
  const member = h.fixture.assignment.claims[0];
  const settled = maintenance.results.find(result => result.dispatch_id === h.fixture.assignment.dispatch_id);
  expect(settled.delivery.claims.h1.state).toBe(tampered ? "delivery_failed" : "result");
  expect(h.settlement(member)).toEqual({ barrier: tampered ? "failed" : "verified", evidenceRetained: false, results: tampered ? 0 : 1, failures: tampered ? 1 : 0, writes });
  expect(h.operations("DeliveryEvidence")).toHaveLength(1);
  if (tampered) {
    const failures = h.operations("DeliveryFailure");
    expect(failures[0].reason).toBe("verification_exhausted");
    expect(failures[0].evidence.attempts).toBeGreaterThan(0);
    return failures[0];
  }
  return h.operations("Result")[0];
}

function setupAsset(h) {
  const { claimArtifactPath } = require("./work_queue_claim_scope.cjs");
  const assets = path.join(h.root, "assets");
  const directory = claimArtifactPath(assets, "h1", h.fixture.assignment);
  fs.mkdirSync(directory, { recursive: true });
  fs.writeFileSync(path.join(directory, crypto.createHash("sha256").update("asset.png").digest("hex") + ".png"), "durable asset bytes");
  process.env.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG = JSON.stringify({ upload_asset: { max: 1, "target-repo": "owner/repo", "assets-dir": assets } });
  h.state.native = { blob: null, tree: null, commit: null, ref: null };
  h.state.reads = { ref: 0, commit: 0, tree: 0, blob: 0 };
  const originalGetRef = h.github.rest.git.getRef;
  Object.assign(h.github.rest.git, {
    getRef: async input => {
      if (!input.ref.startsWith("heads/assets/")) return originalGetRef(input);
      h.state.reads.ref++;
      if (!h.state.native.ref || h.state.tampered) throw Object.assign(new Error("asset ref missing"), { status: 404 });
      // Creation's initial missing-ref probe must succeed before readback fails.
      if (h.state.unavailable) throw Object.assign(new Error("asset readback unavailable"), { status: 503 });
      return { data: { ref: `refs/${input.ref}`, object: { type: "commit", sha: h.state.native.ref } } };
    },
    createBlob: async input => {
      h.state.writes++;
      h.state.native.blob = input.content;
      return { data: { sha: BLOB } };
    },
    createTree: async input => {
      h.state.writes++;
      h.state.native.tree = input.tree;
      return { data: { sha: TREE } };
    },
    createCommit: async input => {
      h.state.writes++;
      h.state.native.commit = { sha: COMMIT, tree: { sha: input.tree }, parents: input.parents.map(sha => ({ sha })) };
      return { data: { sha: COMMIT } };
    },
    createRef: async input => {
      h.state.writes++;
      h.state.native.ref = input.sha;
      return { data: { ref: input.ref, object: { type: "commit", sha: input.sha } } };
    },
    getCommit: async ({ commit_sha }) => {
      h.state.reads.commit++;
      expect(commit_sha).toBe(COMMIT);
      return { data: structuredClone(h.state.native.commit) };
    },
    getTree: async ({ tree_sha }) => {
      h.state.reads.tree++;
      expect(tree_sha).toBe(TREE);
      return { data: { sha: TREE, truncated: false, tree: structuredClone(h.state.native.tree) } };
    },
    getBlob: async ({ file_sha }) => {
      h.state.reads.blob++;
      expect(file_sha).toBe(BLOB);
      return { data: { sha: BLOB, encoding: "base64", content: h.state.native.blob } };
    },
  });
}

async function publishControls(h, tampered) {
  const { authenticatePublisher } = require("./work_queue_native.cjs");
  const { normalizeSubmitParameters, normalizeDispatchParameters, requestForIntent } = require("./work_queue_intents.cjs");
  const context = await authenticatePublisher({ ...h.runtime(), role: "worker", dispatch_id: h.fixture.assignment.dispatch_id, claim_handle: "h1" });
  const submit = {
    version: 3,
    intent_id: "submit-child",
    kind: "submit",
    claim_handle: "h1",
    parameters: { nodes: [{ graph_id: "restart-children", node_key: "child", payload: { effect_contract: { kind: "none" } } }] },
  };
  const dispatch = { version: 3, intent_id: "dispatch-child", kind: "dispatch_next", claim_handle: "h1", parameters: { pool: "default", max_claims: 1, max_dispatches: 1 } };
  const submitted = await h.publishWorkQueueRequest({
    context,
    request: requestForIntent(context, submit.intent_id, "submit", normalizeSubmitParameters(submit.parameters, h.fixture.policy, context.created_at)),
  });
  expect(submitted.persisted).toBe(true);
  expect(submitted.commit.operations.filter(operation => operation.kind === "Work")).toHaveLength(1);
  const granted = await h.publishWorkQueueRequest({
    context,
    request: requestForIntent(context, dispatch.intent_id, "dispatch_next", normalizeDispatchParameters(dispatch.parameters, h.fixture.policy, 1)),
  });
  expect(granted.persisted).toBe(true);
  const claims = granted.commit.operations.filter(operation => operation.kind === "Claim");
  expect(claims).toHaveLength(1);
  expect(claims[0].work_id).toBe(submitted.commit.operations.find(operation => operation.kind === "Work").work_id);
  // Alter only the staged intent. The manager still publishes a valid authenticated
  // operation, but its derived request ID cannot match the actual accepted dispatch.
  const stagedDispatch = tampered ? { ...dispatch, intent_id: "dispatch-never-committed" } : dispatch;
  fs.writeFileSync(path.join(h.root, "control-intents.jsonl"), [submit, stagedDispatch].map(intent => JSON.stringify(intent)).join("\n") + "\n");
  h.snapshot();
  return { submitted: submitted.commit, granted: granted.commit };
}

describe("durable adapter and queue-control delivery across process restart", () => {
  it.each([false, true])("rechecks an uploaded asset after restart without replaying writes (missing ref: %s)", async tampered => {
    const h = createHarness({ type: "upload_asset", messages: [{ type: "upload_asset", path: "asset.png" }], setupNative: setupAsset });
    try {
      await h.workerPass();
      const evidence = pendingEvidence(h, "asset");
      expect(evidence.outputs[0].receipt).toMatchObject({ commit: COMMIT, tree: TREE, blob: BLOB });
      expect(evidence.effects).toHaveLength(4);
      expect(evidence.effects.every(effect => effect.outcome === "succeeded")).toBe(true);
      expect(h.state.writes).toBe(4);
      expect(h.state.reads.ref).toBeGreaterThan(1);
      const reads = { ...h.state.reads };
      // Maintenance must use the ledger and native API, not the worker's staged bytes.
      fs.rmSync(path.join(h.root, "assets"), { recursive: true, force: true });
      h.restartForMaintenance({ tampered });
      const operation = assertSettlement(h, await h.maintenance(), tampered, 4);
      expect(h.state.reads.ref).toBeGreaterThan(reads.ref);
      if (!tampered) {
        expect(operation.descriptor.outputs).toHaveLength(1);
        expect(operation.descriptor.outputs[0]).toMatchObject({ type: "upload_asset", resource: { kind: "asset", repository: "owner/repo", id: COMMIT, url: evidence.outputs[0].receipt.url } });
        for (const kind of ["commit", "tree", "blob"]) expect(h.state.reads[kind]).toBeGreaterThan(reads[kind]);
      }
    } finally {
      h.dispose();
    }
  });

  it.each([false, true])("rechecks a prepared REST effect after restart without its artifact or another POST (changed field: %s)", async tampered => {
    const adapterDirectory = "/tmp/gh-aw/claim-adapters";
    fs.mkdirSync(adapterDirectory, { recursive: true });
    const adapterRoot = fs.mkdtempSync(path.join(adapterDirectory, "delivery_evidence_"));
    const type = path.basename(adapterRoot);
    const message = { type, claim_handle: "h1", check_name: "durable-check", revision: REF };
    const expected = { name: message.check_name, head_sha: REF, status: "completed", conclusion: "success" };
    const adapter = {
      mode: "prepared",
      "effect-type": "github_rest",
      "target-repo": "owner/repo",
      "field-map": { name: "check_name", head_sha: "revision" },
      expected: { status: "completed", conclusion: "success" },
      request: { method: "POST", route: "/repos/{owner}/{repo}/check-runs", permission: "checks" },
      verifier: { route: "/repos/{owner}/{repo}/check-runs/{receipt_id}", "resource-kind": "check_run", fields: { name: "name", head_sha: "head_sha", status: "status", conclusion: "conclusion" } },
    };
    let h;
    try {
      h = createHarness({
        type,
        messages: [message],
        contract: { version: 1, outputs: [{ type, min: 1, max: 1, verification: { verifier_id: type, expected } }] },
        config: { [type]: { max: 1 }, claim_adapters: { [type]: adapter } },
        setupNative(harness) {
          const { preparedAdapterPath } = require("./work_queue_claim_adapters.cjs");
          const filename = preparedAdapterPath(path.join(adapterRoot, "0"), "h1", type, harness.fixture.assignment);
          fs.mkdirSync(path.dirname(filename), { recursive: true });
          fs.writeFileSync(filename, JSON.stringify({ version: 3, claim_handle: "h1", type, messages: [{ input: message, payload: { check_name: message.check_name, revision: REF } }] }));
          harness.state.reads = 0;
          harness.github.request = async (route, fields) => {
            if (route === "POST /repos/owner/repo/check-runs") {
              expect(fields).toEqual(expected);
              harness.state.writes++;
              harness.state.native = { id: 91, ...fields };
              return { data: structuredClone(harness.state.native) };
            }
            expect(route).toBe("GET /repos/owner/repo/check-runs/91");
            harness.state.reads++;
            if (harness.state.unavailable) throw Object.assign(new Error("REST readback unavailable"), { status: 503 });
            return { data: { ...harness.state.native, ...(harness.state.tampered ? { conclusion: "failure" } : {}) } };
          };
        },
      });
      await h.workerPass();
      const evidence = pendingEvidence(h, "rest_adapter");
      expect(evidence.outputs[0]).toMatchObject({ type, receipt: { id: "91", fields: expected } });
      expect(JSON.parse(evidence.outputs[0].adapter)).toEqual(adapter);
      expect(evidence.effects).toMatchObject([{ kind: "check_run", repository: "owner/repo", id: "91", outcome: "succeeded" }]);
      expect(h.state.writes).toBe(1);
      expect(h.state.reads).toBeGreaterThan(0);
      const reads = h.state.reads;
      fs.rmSync(adapterRoot, { recursive: true, force: true });
      h.restartForMaintenance({ tampered });
      const operation = assertSettlement(h, await h.maintenance(), tampered, 1);
      expect(h.state.reads).toBeGreaterThan(reads);
      if (!tampered) {
        expect(operation.descriptor.outputs).toHaveLength(1);
        expect(operation.descriptor.outputs[0]).toMatchObject({ type, resource: { kind: "check_run", repository: "owner/repo", id: "91" } });
      }
    } finally {
      h?.dispose();
      fs.rmSync(adapterRoot, { recursive: true, force: true });
    }
  });

  it.each([false, true])("rechecks a real child dispatch against fresh checked inventory without repeating it (wrong request ID: %s)", async tampered => {
    const h = createHarness({
      type: "work_queue_dispatch_next",
      messages: [],
      config: {},
      contract: {
        version: 1,
        outputs: [
          { type: "work_queue_submit", min: 1, max: 1 },
          { type: "work_queue_dispatch_next", min: 1, max: 1 },
        ],
      },
    });
    try {
      const { submitted, granted } = await publishControls(h, tampered);
      const { readDeliveryControlInventory } = require("./work_queue_delivery.cjs");
      const inventory = await readDeliveryControlInventory({ ...h.runtime(), claim_handle: "h1" });
      expect(inventory.controls).toHaveLength(2);
      expect(inventory.controls.find(control => control.type === "work_queue_dispatch_next")).toMatchObject({ request_id: granted.request.id, writes: { claims: 1, dispatches: 1 } });
      let failedInventoryReads = 0;
      await h.workerPass({
        readControlInventory: async () => {
          failedInventoryReads++;
          throw Object.assign(new Error("queue-control readback unavailable"), { status: 503 });
        },
      });
      expect(failedInventoryReads).toBeGreaterThan(0);
      const evidence = pendingEvidence(h, "queue_control");
      expect(evidence.outputs).toHaveLength(2);
      expect(evidence.effects).toEqual([]);
      expect(evidence.controls_digest).toBe(inventory.controls_digest);
      expect(evidence.outputs[0]).toMatchObject({ type: "work_queue_submit", receipt: { request_id: submitted.request.id } });
      const dispatchReceipt = evidence.outputs.find(output => output.type === "work_queue_dispatch_next");
      if (tampered) expect(h.projection.requests.has(dispatchReceipt.receipt.request_id)).toBe(false);
      else expect(dispatchReceipt.receipt.request_id).toBe(granted.request.id);
      const before = {
        works: [...h.projection.works.keys()],
        claims: [...h.projection.claims.keys()],
        controls: h.transactions.filter(commit => commit.actor.role === "worker" && ["submit", "dispatch_next"].includes(commit.request.kind)),
      };
      fs.unlinkSync(path.join(h.root, "control-intents.jsonl"));
      h.restartForMaintenance({ tampered });
      // The old checked inventory's private provenance is gone along with all receipts.
      expect(require("./work_queue_control_receipts.cjs").isTrustedClaimQueueControlInventory(inventory, h.fixture.assignment, "h1")).toBe(false);
      // The bound original run sorts before the newly reserved child assignment.
      // Restrict this maintenance batch to it; no child launch is part of readback.
      const operation = assertSettlement(h, await h.maintenance({ maxReconciliations: 1 }), tampered, 0);
      expect([...h.projection.works.keys()]).toEqual(before.works);
      expect([...h.projection.claims.keys()]).toEqual(before.claims);
      expect(h.transactions.filter(commit => commit.actor.role === "worker" && ["submit", "dispatch_next"].includes(commit.request.kind))).toEqual(before.controls);
      if (!tampered) {
        expect(operation.descriptor.outputs).toHaveLength(2);
        expect(operation.descriptor.outputs).toEqual(
          expect.arrayContaining([
            expect.objectContaining({ type: "work_queue_submit", resource: { kind: "queue_commit", repository: "owner/repo", id: submitted.id } }),
            expect.objectContaining({ type: "work_queue_dispatch_next", resource: { kind: "queue_commit", repository: "owner/repo", id: granted.id } }),
          ])
        );
      }
    } finally {
      h.dispose();
    }
  });
});
