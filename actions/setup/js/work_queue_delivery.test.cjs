// @ts-check
import { describe, expect, it } from "vitest";
import checks from "./work_queue_claim_scope_checks.cjs";
import { normalizeAssignment } from "./work_queue_claim_scope.cjs";
import { digest } from "./work_queue_codec.cjs";
import { createClaimDeliveryVerifier, validateDeliveryContract, verifyClaimDelivery, verifyBuiltinDeliveryOutput } from "./work_queue_delivery.cjs";

checks.registerTests({ describe, it });

describe("historical diagnostic delivery contracts", () => {
  it("uses standardized codes for invalid verifier configuration", () => {
    const original = assignment([{ type: "create_issue", min: 1, max: 1 }]);
    expect(() => createClaimDeliveryVerifier({ assignment: original })).toThrow(/^ERR_WORK_QUEUE_DELIVERY_READBACK_REQUIRED:/);
  });

  const assignment = outputs =>
    normalizeAssignment({
      version: 3,
      dispatch_id: "dispatch",
      request_id: "request",
      commit_id: "commit",
      policy_epoch: "policy",
      pool: "default",
      worker_profile: "default",
      claims: [{ handle: "h1", claim_id: "c1", work_id: "w1", work: { effect_contract: { version: 1, outputs } }, result_refs: [] }],
    });
  const deliver = (original, messages, options = {}) =>
    verifyClaimDelivery({
      assignment: original,
      claim_handle: "h1",
      authorize: async request => ({ authorized: true, claim_handle: request.claim_handle }),
      messages,
      results: messages.map((_, messageIndex) => ({ messageIndex, success: true, result: { number: 42 } })),
      ...options,
    });

  it("requires independent delivery readback with a stable error code", () => {
    expect(() => createClaimDeliveryVerifier({ assignment: assignment([]) })).toThrow("ERR_WORK_QUEUE_DELIVERY_READBACK_REQUIRED: Claim delivery verifier requires trusted independent readback");
  });

  for (const legacy of ["missing_tool", "missing_data"]) {
    const canonical = `report_${legacy}`;

    it(`matches ${legacy} contracts and messages canonically without rewriting immutable Work`, async () => {
      for (const contractType of [legacy, canonical]) {
        const original = assignment([
          { type: "create_issue", min: 1, max: 1 },
          { type: contractType, min: 1, max: 1 },
        ]);
        const before = JSON.stringify(original);
        const beforeDigest = digest(original);
        expect(validateDeliveryContract(original.claims[0].work.effect_contract)).toBe(original.claims[0].work.effect_contract);
        for (const messageType of [legacy, canonical]) {
          const messages = [Object.freeze({ type: "create_issue", repo: "owner/repo" }), Object.freeze({ type: messageType })];
          const delivered = await deliver(original, messages, {
            verifyOutput: verifyBuiltinDeliveryOutput,
            github: {
              rest: {
                repos: { get: async () => ({ data: { id: 7, full_name: "owner/repo" } }) },
                issues: { get: async () => ({ data: { id: 142, number: 42, html_url: "https://github.com/owner/repo/issues/42" } }) },
              },
            },
          });
          expect(delivered.verification).toBe("verified");
          expect(delivered.disposition).toBe("complete");
          expect(delivered.descriptor.outputs[1]).toEqual({ type: canonical, effect: "none" });
          expect(messages[1].type).toBe(messageType);
          expect(JSON.stringify(original)).toBe(before);
          expect(digest(original)).toBe(beforeDigest);
        }
      }
    });

    it(`shares cardinality across ${legacy} and ${canonical} messages`, async () => {
      const messages = [{ type: legacy }, { type: canonical }];
      for (const type of [legacy, canonical]) {
        const exact = assignment([{ type, min: 2, max: 2 }]);
        expect((await deliver(exact, messages)).verification).toBe("verified");
        expect((await deliver(exact, [messages[0]])).reason).toMatch(/cardinality/);
        expect((await deliver(assignment([{ type, min: 0, max: 1 }]), messages)).reason).toMatch(/cardinality/);
      }
    });

    it(`rejects ambiguous duplicate ${legacy} contract definitions`, () => {
      for (const types of [
        [legacy, canonical],
        [canonical, legacy],
        [legacy, legacy],
        [canonical, canonical],
      ]) {
        expect(() => validateDeliveryContract({ version: 1, outputs: types.map(type => ({ type, min: 0, max: 1 })) })).toThrow(/Invalid bounded effect_contract/);
      }
    });

    it(`does not skip declared verification intent on ${legacy} contracts`, async () => {
      const original = assignment([{ type: legacy, min: 1, max: 1, verification: { verifier_id: "diagnostic", expected: {} } }]);
      for (const type of [legacy, canonical]) {
        expect((await deliver(original, [{ type }])).reason).toMatch(/verification intent requires a supported trusted verifier binding/);
      }
    });

    it(`still requires declared ${legacy} outputs and exact successful receipts`, async () => {
      const original = assignment([{ type: legacy, min: 1, max: 1 }]);
      expect((await deliver(original, [{ type: "report_incomplete" }])).reason).toMatch(/Undeclared output/);
      for (const outcome of [{ success: false }, { success: true, delegated: true }, { success: true, skipped: true }]) {
        expect((await deliver(original, [{ type: canonical }], { results: [{ messageIndex: 0, ...outcome }] })).reason).toMatch(/Missing exact nondelegated delivery receipt/);
      }
    });
  }
});

it("registers dispatch identity and manifest checks as independent siblings before execution", async () => {
  let registering = true;
  /** @type {Map<string, () => unknown>} */
  const callbacks = new Map();
  checks.registerTests({
    describe(_name, register) {
      expect(registering, "suite registration cannot occur inside a running check").toBe(true);
      register();
    },
    it(name, run) {
      expect(registering, "test registration cannot occur inside a running check").toBe(true);
      expect(callbacks.has(name), "each check must be registered exactly once").toBe(false);
      callbacks.set(name, run);
    },
  });
  registering = false;
  for (const name of ["does not reuse local handles, factory contexts or receipts across immutable dispatch identities", "pins manifest writers to the full original Claim and persists immutable attribution"]) {
    const run = callbacks.get(name);
    expect(run, "both checks must exist before any test callback runs").toBeTypeOf("function");
    if (!run) throw new Error(`Missing sibling check: ${name}`);
    await run();
  }
});
