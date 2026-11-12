import { describe, expect, it } from "vitest";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const scope = require("./work_queue_claim_scope.cjs");
const rest = require("./work_queue_rest_adapter.cjs");
const { canonical, canonicalBytes } = require("./work_queue_codec.cjs");

const adapter = {
  mode: "prepared",
  "effect-type": "github_rest",
  "target-repo": "owner/repo",
  "field-map": { name: "name", head_sha: "revision" },
  expected: { status: "completed", conclusion: "success" },
  request: { method: "POST", route: "/repos/{owner}/{repo}/check-runs", permission: "checks" },
  verifier: { route: "/repos/{owner}/{repo}/check-runs/{receipt_id}", "resource-kind": "check_run", fields: { name: "name", head_sha: "head_sha", status: "status", conclusion: "conclusion" } },
};

function fixture() {
  const assignment = {
    version: 3,
    dispatch_id: "dispatch",
    request_id: "request",
    commit_id: "commit",
    policy_epoch: "epoch",
    pool: "default",
    worker_profile: "default",
    claims: ["h1", "h2"].map(handle => ({ handle, claim_id: `claim:${handle}`, work_id: `work:${handle}`, work: { effect_contract: { version: 1, outputs: [{ type: "custom", min: 1, max: 1 }] } }, result_refs: [] })),
  };
  const state = { observed: null, reads: 0, writes: 0 };
  const github = {
    rest: { repos: { get: async () => ({ data: { id: 7, full_name: "owner/repo" } }) } },
    request: async (route, fields) => {
      if (route.startsWith("POST ")) {
        expect(route).toBe("POST /repos/owner/repo/check-runs");
        state.writes++;
        state.observed = { id: 91, ...fields };
        return { data: structuredClone(state.observed) };
      }
      expect(route).toBe("GET /repos/owner/repo/check-runs/91");
      state.reads++;
      return { data: structuredClone(state.observed) };
    },
  };
  const authorize = async request => ({ claim_handle: request.claim_handle, authorized: true });
  return { assignment, claim: assignment.claims[0], github, state, authorize };
}

describe("durable REST adapter receipts", () => {
  it("restarts independent readback from a serialized receipt without trusting copied results or proofs", async () => {
    const f = fixture();
    const fields = { ...adapter.expected, name: "Exact check", head_sha: "a".repeat(40) };
    let receipt;
    let result;
    await scope.withClaimExecution({ assignment: f.assignment, claim_handle: "h1", authorize: f.authorize }, async () => {
      result = await rest.createRestEffectHandler(adapter, f.github)({ type: "custom", claim_handle: "h1", ...fields });
      receipt = rest.durableRestReceipt(result, f.claim);
      expect(receipt).toEqual({ id: "91", fields });
      expect(rest.durableRestReceipt(structuredClone(result), f.claim)).toBeNull();
      expect(rest.durableRestReceipt(result, f.assignment.claims[1])).toBeNull();
      receipt.fields.name = "cannot mutate the private receipt";
      receipt = JSON.parse(JSON.stringify(rest.durableRestReceipt(result, f.claim)));
    });
    const filename = require.resolve("./work_queue_rest_adapter.cjs");
    const cached = require.cache[filename];
    delete require.cache[filename];
    try {
      const restarted = require(filename);
      await scope.withClaimExecution({ assignment: f.assignment, claim_handle: "h1", authorize: f.authorize }, async () => {
        const options = {
          adapter,
          claim: f.claim,
          github: f.github,
          authorize: f.authorize,
          verification: { verifier_id: "custom", expected: fields },
          durable: true,
          result: { entry: { verifier: "rest_adapter", adapter: canonical(adapter), receipt } },
        };
        expect((await restarted.verifyRestAdapterDelivery({ ...options, durable: false, result })).verified).toBe(false);
        const proof = await restarted.verifyRestAdapterDelivery(options);
        expect(proof.verified).toBe(true);
        expect(f.state.reads).toBe(1);
        expect(proof.resource).toEqual({ kind: "check_run", repository: "owner/repo", id: "91" });
        await expect(scope.withClaimResourceVerification(structuredClone(proof), () => true)).rejects.toThrow(/private in-process/);
        expect((await restarted.verifyRestAdapterDelivery({ ...options, verification: { verifier_id: "custom", expected: { ...fields, name: "different" } } })).verified).toBe(false);
        const reads = f.state.reads;
        for (const invalid of [{}, { ...receipt, id: undefined }, { ...receipt, fields: {} }, { ...receipt, fields: { ...fields, name: undefined } }]) {
          expect((await restarted.verifyRestAdapterDelivery({ ...options, result: { entry: { ...options.result.entry, receipt: invalid } } })).verified).toBe(false);
        }
        expect((await restarted.verifyRestAdapterDelivery({ ...options, result: { entry: { ...options.result.entry, adapter: canonical({ ...adapter, "target-repo": "other/repo" }) } } })).verified).toBe(false);
        expect((await restarted.verifyRestAdapterDelivery({ ...options, result: { entry: { ...options.result.entry, verifier: "graphql_adapter" } } })).verified).toBe(false);
        expect(f.state.reads).toBe(reads);
        f.state.observed.conclusion = "failure";
        expect((await restarted.verifyRestAdapterDelivery(options)).verified).toBe(false);
      });
      expect(f.state.writes).toBe(1);
    } finally {
      require.cache[filename] = cached;
    }
  });

  it("bounds durable fields at 8192 bytes without limiting private readback", async () => {
    const f = fixture();
    await scope.withClaimExecution({ assignment: f.assignment, claim_handle: "h1", authorize: f.authorize }, async () => {
      const handler = rest.createRestEffectHandler(adapter, f.github);
      const fields = { ...adapter.expected, name: "", head_sha: "a".repeat(40) };
      fields.name = "x".repeat(8192 - canonicalBytes(fields));
      const result = await handler({ type: "custom", claim_handle: "h1", ...fields });
      expect(canonicalBytes(rest.durableRestReceipt(result, f.claim).fields)).toBe(8192);
      const oversized = await handler({ type: "custom", claim_handle: "h1", ...fields, name: `${fields.name}x` });
      expect(rest.durableRestReceipt(oversized, f.claim)).toBeNull();
      expect((await rest.verifyRestAdapterDelivery({ adapter, claim: f.claim, github: f.github, result: oversized })).verified).toBe(true);
    });
  });
});
