// @ts-check
import { describe, expect, it, vi } from "vitest";
import { normalizeAssignment, normalizeClaimScope, normalizeRuntimeMessage, assertClaimAuthorized, withClaimExecution, claimArtifactPath, scopedArtifactFilename } from "./work_queue_claim_scope.cjs";
import sharedChecks from "./work_queue_claim_scope_checks.cjs";

const assignment = count => ({
  version: 3,
  dispatch_id: "d",
  request_id: "r",
  commit_id: "q",
  policy_epoch: "p",
  pool: "default",
  worker_profile: "default",
  claims: Array.from({ length: count }, (_, index) => ({ handle: `h${index + 1}`, claim_id: `c${index + 1}`, work_id: `w${index + 1}`, work: { plan: `task${index + 1}` }, result_refs: [] })),
});

describe("immutable Claim scope", () => {
  it("preserves unscoped message identity unless normalizing a historical alias", () => {
    vi.stubEnv("GH_AW_WORK_QUEUE_ENABLED", "false");
    try {
      for (const type of ["report_missing_tool", "report_missing_data", "report_incomplete", "create_issue", "custom_output"]) {
        const message = Object.freeze({ type, body: "unchanged" });
        expect(normalizeRuntimeMessage(message)).toBe(message);
      }
      for (const type of ["missing_tool", "missing_data"]) {
        const message = Object.freeze({ type, body: "unchanged" });
        const normalized = normalizeRuntimeMessage(message);
        expect(normalized).not.toBe(message);
        expect(normalized).toEqual({ type: `report_${type}`, body: "unchanged" });
        expect(message.type).toBe(type);
      }
    } finally {
      vi.unstubAllEnvs();
    }
  });

  it("matches the canonical shared identity fixtures and preserves opaque domain values", sharedChecks.checkSharedIdentityFixtures);

  it("defaults every output type only for an original singleton", () => {
    for (const type of ["create_issue", "custom_job", "custom_action", "upload_asset", "noop", "report_incomplete", "report_missing_tool", "report_missing_data", "work_queue_submit", "work_queue_dispatch_next", "work_queue_claim_finish"]) {
      expect(normalizeClaimScope({ type }, assignment(1))).toEqual({ type, claim_handle: "h1" });
      expect(() => normalizeClaimScope({ type }, assignment(2))).toThrow(/original multi-Claim/);
    }
  });

  it("rejects explicit empty/null/foreign/malformed/conflicting scope without overwriting it", () => {
    for (const claim_handle of [null, "", false, {}, [], 1, "foreign"]) {
      expect(() => normalizeClaimScope({ type: "noop", claim_handle }, assignment(1))).toThrow();
    }
    expect(() => normalizeClaimScope({ claim_handle: "h1", claim_id: "c2" }, assignment(2))).toThrow(/conflicts/);
    expect(() => normalizeClaimScope({ claim_handle: "h1", work_id: "w2" }, assignment(2))).toThrow(/conflicts/);
    for (const field of ["authorized", "run_id", "assignment", "work_queue_assignment", "work_queue_claim"]) {
      expect(() => normalizeClaimScope({ claim_handle: "h1", [field]: null }, assignment(1))).toThrow(/trusted authority/);
    }
  });

  it("accepts 256-byte canonical identities without narrowing them to 128 characters", () => {
    for (const id of ["x".repeat(256), "\u00e9".repeat(128), "\ud83d\ude80".repeat(64)]) {
      const value = assignment(1);
      Object.assign(value, { dispatch_id: id, request_id: id, commit_id: id, policy_epoch: id, pool: id, worker_profile: id });
      Object.assign(value.claims[0], { handle: id, claim_id: id, work_id: id, result_refs: [{ work_id: id, result_commit_id: id, descriptor: {} }] });
      const normalized = normalizeAssignment(value);
      expect(normalizeClaimScope({ type: "noop", claim_handle: id }, normalized).claim_handle).toBe(id);
      expect(normalizeClaimScope({ type: "noop" }, normalized).claim_handle).toBe(id);
      expect(() => normalizeClaimScope({ claim_handle: `${id}x` }, normalized)).toThrow(/invalid/);
      expect(() => normalizeAssignment({ ...value, request_id: `${id}x` })).toThrow(/invalid/);
    }
  });

  it("freezes the original array and never defaults based on open members", () => {
    const original = normalizeAssignment(assignment(3));
    expect(Object.isFrozen(original.claims)).toBe(true);
    expect(Object.isFrozen(original.claims[0].work)).toBe(true);
    expect(() => normalizeClaimScope({ type: "noop" }, original)).toThrow(/multi-Claim/);
    expect(normalizeClaimScope({ type: "noop", claim_handle: "h3" }, original).claim_handle).toBe("h3");
  });

  it("rejects scalar, legacy, oversized and duplicate assignments", () => {
    expect(() => normalizeAssignment(assignment(0))).toThrow("ERR_WORK_QUEUE_CLAIM_SCOPE: work_queue_claim_scope: work_queue_assignment requires an immutable array of 1 to 16 Claims; scalar assignments are unsupported");
    for (const value of [{ work_id: "w", claim_id: "c" }, { ...assignment(1), version: 2 }, assignment(17), assignment(0), { ...assignment(1), run_id: "1" }]) {
      expect(() => normalizeAssignment(value)).toThrow();
    }
    const duplicate = assignment(2);
    duplicate.claims[1].handle = "h1";
    expect(() => normalizeAssignment(duplicate)).toThrow(/duplicate/);
  });

  it("requires a fresh same-Claim proof, including previews and missing messages", async () => {
    const authorize = async request => ({ authorized: request.claim_handle === "h1", claim_handle: request.claim_handle });
    await withClaimExecution({ assignment: assignment(2), claim_handle: "h1", authorize }, async () => {
      expect(await assertClaimAuthorized({ type: "noop", claim_handle: "h1" })).toEqual({ type: "noop", claim_handle: "h1" });
      await expect(assertClaimAuthorized({ type: "report_missing_tool", claim_handle: "h2" })).rejects.toThrow(/escape/);
      await expect(assertClaimAuthorized({ type: "noop", claim_handle: "h1" }, { authorize: async () => ({ authorized: true, claim_handle: "h2" }) })).rejects.toThrow(/same-Claim/);
    });
    await withClaimExecution({ assignment: assignment(2), claim_handle: "h2", authorize }, async () => {
      await expect(assertClaimAuthorized({ type: "report_incomplete", claim_handle: "h2" })).rejects.toThrow(/same-Claim/);
    });
  });

  it.each(["environment", "execution"])("logs a staged preview before rejecting resource effects via %s", async source => {
    vi.stubEnv("GH_AW_SAFE_OUTPUTS_STAGED", source === "environment" ? "true" : "false");
    vi.stubGlobal("core", undefined);
    const log = vi.spyOn(console, "log").mockImplementation(() => {});
    const authorize = vi.fn();
    try {
      await withClaimExecution({ assignment: assignment(1), claim_handle: "h1", staged: source === "execution", authorize }, async () => {
        await expect(assertClaimAuthorized({ type: "create_issue" }, { effect: true })).rejects.toMatchObject({
          message: "ERR_WORK_QUEUE_CLAIM_SCOPE: work_queue_claim_scope: read-only Claim preview cannot perform resource effects",
          code: "claim_scope_invalid",
        });
      });
      expect(log).toHaveBeenCalledExactlyOnceWith("🎭 Staged Mode Preview — blocking resource effects for read-only Claim preview");
      expect(authorize).not.toHaveBeenCalled();
    } finally {
      log.mockRestore();
      vi.unstubAllEnvs();
    }
  });

  it("keeps non-staged resource authorization available without a preview notice", async () => {
    vi.stubEnv("GH_AW_SAFE_OUTPUTS_STAGED", "false");
    const log = vi.spyOn(console, "log").mockImplementation(() => {});
    const authorize = vi.fn().mockResolvedValue({ authorized: true, claim_handle: "h1" });
    try {
      await withClaimExecution({ assignment: assignment(1), claim_handle: "h1", authorize }, async () => {
        expect(await assertClaimAuthorized({ type: "create_issue" }, { effect: true, resource: {} })).toEqual({ type: "create_issue", claim_handle: "h1" });
      });
      expect(authorize).toHaveBeenCalledOnce();
      expect(log).not.toHaveBeenCalled();
    } finally {
      log.mockRestore();
      vi.unstubAllEnvs();
      vi.unstubAllGlobals();
    }
  });

  it("partitions prepared artifacts without path traversal or cross-Claim collisions", async () => {
    const base = "/workspace/artifacts";
    const first = await withClaimExecution({ assignment: assignment(2), claim_handle: "h1" }, () => scopedArtifactFilename(`${base}/aw-patch.patch`));
    const second = await withClaimExecution({ assignment: assignment(2), claim_handle: "h2" }, () => scopedArtifactFilename(`${base}/aw-patch.patch`));
    expect(first).not.toBe(second);
    expect(first).toBe(`${claimArtifactPath(base, "h1", assignment(2))}/aw-patch.patch`);
    const opaque = assignment(1);
    opaque.claims[0].handle = "../../escape";
    expect(claimArtifactPath(base, "../../escape", opaque)).toMatch(/^\/workspace\/artifacts\/claims\/[a-f0-9]{64}$/);
  });
});
