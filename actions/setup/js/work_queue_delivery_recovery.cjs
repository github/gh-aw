// @ts-check
"use strict";

const { normalizeAssignment, claimIdentity, withClaimExecution, closeClaimEffectChannel } = require("./work_queue_claim_scope.cjs");
const { loadQueue } = require("./work_queue_binding.cjs");
const { wrapClaimEffectClient } = require("./work_queue_effect_client.cjs");
const { createClaimDeliveryVerifier, inspectClaimDelivery, readDeliveryControlInventory } = require("./work_queue_delivery.cjs");
const { createDurableDeclaredVerifier } = require("./work_queue_claim_adapters.cjs");
const { verifyEvidenceEntry } = require("./work_queue_delivery_evidence.cjs");
const { authorizeReconcilerClaimReadback } = require("./finish_work_queue_claim.cjs");

function createDurableDeliveryVerifier(options) {
  const assignment = normalizeAssignment(options.assignment);
  return async (member, verification = {}) => {
    const latest = await loadQueue(options);
    const evidence = latest.projection.deliveryEvidence.get(member.claim_id);
    if (!evidence || evidence.claim_handle !== member.handle || evidence.work_id !== member.work_id || evidence.dispatch_id !== assignment.dispatch_id) return { verified: false, diagnostics: { event: "delivery.durable.missing" } };
    const effects = evidence.effects.map(effect => ({ ...effect, ...claimIdentity(member.handle, assignment) }));
    const messages = evidence.outputs.map(entry => ({ type: entry.type, claim_handle: member.handle, ...(entry.verifier === "queue_control" ? { intent_id: entry.receipt.intent_id, request_id: entry.receipt.request_id } : {}) }));
    const results = evidence.outputs.map((entry, messageIndex) => ({ messageIndex, success: true, claim_handle: member.handle, result: { entry } }));
    const authorize = request => authorizeReconcilerClaimReadback({ ...options, ...request, assignment, context: options.context });
    const context = { ...options.context, runId: evidence.run_id, runAttempt: 1 };
    const rawGithub = options.githubClient || options.github;
    const verify = createClaimDeliveryVerifier({
      assignment,
      recheck: (_member, checked) =>
        withClaimExecution({ assignment, claim_handle: member.handle, authorize, effects }, () =>
          inspectClaimDelivery({
            assignment,
            claim_handle: member.handle,
            durable: true,
            effects,
            messages,
            results,
            authorize,
            context,
            github: wrapClaimEffectClient(rawGithub, { claim_handle: member.handle, authorize, authorizeGithub: rawGithub, context, signal: checked.signal }),
            signal: checked.signal,
            effectChannel: closeClaimEffectChannel(),
            requireControlInventory: true,
            readControlInventory: async () => {
              if (evidence.outputs.some(entry => entry.verifier === "queue_control") && evidence.controls_digest === undefined) throw new Error("work_queue_control_inventory_digest_missing");
              const inventory = await readDeliveryControlInventory({ ...options, assignment, claim_handle: member.handle });
              if (evidence.controls_digest !== undefined && inventory.controls_digest !== evidence.controls_digest) throw new Error("work_queue_control_inventory_changed");
              return inventory;
            },
            verifyDeclaredOutput: createDurableDeclaredVerifier(evidence.outputs, effects),
            verifyOutput: verifyEvidenceEntry,
          })
        ),
    });
    return verify(member, verification);
  };
}

module.exports = { createDurableDeliveryVerifier };
