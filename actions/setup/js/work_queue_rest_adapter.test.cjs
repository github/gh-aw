import { describe, expect, it } from "vitest";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const scope = require("./work_queue_claim_scope.cjs");
const rest = require("./work_queue_rest_adapter.cjs");

const adapter = {
  mode: "prepared",
  "effect-type": "github_rest",
  "target-repo": "owner/repo",
  "field-map": { name: "name", head_sha: "revision" },
  expected: { status: "completed", conclusion: "success" },
  request: { method: "POST", route: "/repos/{owner}/{repo}/check-runs", permission: "checks" },
};

function fixture(proof = request => ({ claim_handle: request.claim_handle, authorized: true, state: "open" })) {
  const assignment = {
    dispatch_id: "dispatch",
    claims: ["h1", "h2"].map(handle => ({ handle, claim_id: `claim-${handle}`, work_id: `work-${handle}`, work: {}, result_refs: [] })),
  };
  const calls = [];
  const github = {
    rest: { repos: { get: async () => ({ data: { id: 7, full_name: "owner/repo" } }) } },
    request: async (route, fields) => {
      calls.push({ route, fields });
      return { data: { id: 91 } };
    },
  };
  return { assignment, github, calls, authorize: async request => proof(request) };
}

describe("REST adapter effects", () => {
  it("performs one scoped write, records it, and never reads back", async () => {
    const f = fixture();
    const effects = [];
    const fields = { ...adapter.expected, name: "Exact check", head_sha: "a".repeat(40) };
    await scope.withClaimExecution({ assignment: f.assignment, claim_handle: "h1", authorize: f.authorize, effects }, async () => {
      const result = await rest.createRestEffectHandler(adapter, f.github)({ type: "custom", claim_handle: "h1", ...fields });
      expect(result).toEqual({ success: true, repo: "owner/repo", id: "91" });
    });
    expect(f.calls).toEqual([{ route: "POST /repos/owner/repo/check-runs", fields }]);
    expect(effects.length).toBeGreaterThan(0);
    expect(effects.every(effect => effect.claim_handle === "h1")).toBe(true);
  });

  it("rejects foreign repositories and refused Claims before writing", async () => {
    const f = fixture(request => ({ claim_handle: request.claim_handle, authorized: false, state: "cancelled", suppressed: true }));
    await scope.withClaimExecution({ assignment: f.assignment, claim_handle: "h1", authorize: f.authorize }, async () => {
      const handler = rest.createRestEffectHandler(adapter, f.github);
      await expect(handler({ type: "custom", claim_handle: "h1", repo: "other/repo", name: "x", revision: "a" })).rejects.toThrow(/conflicts/);
      await expect(handler({ type: "custom", claim_handle: "h1", name: "x", revision: "a" })).rejects.toThrow(/cancelled/);
    });
    expect(f.calls).toEqual([]);
  });

  it("accepts the legacy verifier key without requiring it", () => {
    expect(rest.validateRestAdapter({ ...adapter, verifier: { route: "/repos/{owner}/{repo}/check-runs/{receipt_id}" } })).toBeTruthy();
    expect(rest.validateRestAdapter(adapter)).toBe(adapter);
    expect(() => rest.validateRestAdapter({ ...adapter, request: { ...adapter.request, route: "/repos/{owner}/{repo}/x/{unbound}" } })).toThrow(/unbound selector/);
  });
});
