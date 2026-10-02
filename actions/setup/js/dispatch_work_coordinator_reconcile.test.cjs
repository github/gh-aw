import { describe, expect, it, vi } from "vitest";
import { deriveWorkId } from "./dispatch_work_coordinator.cjs";
import { reconcileDispatchWorkCoordinator } from "./dispatch_work_coordinator_reconcile.cjs";

const work = { task: "test" };
const assignment = { work_id: deriveWorkId(work), claim_id: "claim-1", work };
const env = {
  GH_AW_DISPATCH_WORK_COORDINATOR_CONTEXT: JSON.stringify(assignment),
  GITHUB_REPOSITORY: "owner/repo",
  GITHUB_WORKFLOW_REF: "owner/repo/.github/workflows/worker.yml@refs/heads/main",
  GITHUB_RUN_ID: "123",
  GH_AW_DISPATCH_WORK_COORDINATOR_SCHEMA: JSON.stringify({ type: "object" }),
};

function createCoordinator(state = "effective") {
  return {
    status: vi.fn().mockResolvedValue({
      works: [
        {
          work_id: assignment.work_id,
          state: state === "effective" ? "claimed" : "available",
          effective_claim_id: state === "effective" ? assignment.claim_id : null,
          claims: [{ claim_id: assignment.claim_id, run_id: env.GITHUB_RUN_ID, workflow_id: env.GITHUB_WORKFLOW_REF, state }],
        },
      ],
    }),
    finishClaim: vi.fn().mockResolvedValue({ state: "completed" }),
    cancelClaim: vi.fn().mockResolvedValue({ state: "available" }),
  };
}

describe("Dispatch Work Coordinator safe-output reconciliation", () => {
  it("persists Completion before authorizing ordinary safe outputs", async () => {
    const coordinator = createCoordinator();
    const result = await reconcileDispatchWorkCoordinator({
      messages: [{ type: "dispatch_claim_finish", outcome: { summary: "done" } }],
      env,
      coordinatorFactory: () => coordinator,
    });

    expect(coordinator.status).toHaveBeenCalledOnce();
    expect(coordinator.finishClaim).toHaveBeenCalledWith(assignment.claim_id, { summary: "done" });
    expect(coordinator.cancelClaim).not.toHaveBeenCalled();
    expect(result).toEqual({ forceStaged: false, finishAuthorized: true });
  });

  it("cancels an effective Claim and forces staged mode when finish is absent", async () => {
    const coordinator = createCoordinator();
    const result = await reconcileDispatchWorkCoordinator({ messages: [], env, coordinatorFactory: () => coordinator });

    expect(coordinator.cancelClaim).toHaveBeenCalledWith(assignment.claim_id);
    expect(coordinator.finishClaim).not.toHaveBeenCalled();
    expect(result.forceStaged).toBe(true);
  });

  it("stages every safe output without mutating a superseded Claim", async () => {
    const coordinator = createCoordinator("superseded");
    const result = await reconcileDispatchWorkCoordinator({
      messages: [{ type: "dispatch_claim_finish" }],
      env,
      coordinatorFactory: () => coordinator,
    });

    expect(coordinator.cancelClaim).not.toHaveBeenCalled();
    expect(coordinator.finishClaim).not.toHaveBeenCalled();
    expect(result).toEqual({ forceStaged: true, finishAuthorized: false, reason: "Claim is no longer effective" });
  });

  it("rejects forged authority fields, outcomes, and duplicate finish intents", async () => {
    const coordinator = createCoordinator();
    await expect(
      reconcileDispatchWorkCoordinator({
        messages: [{ type: "dispatch_claim_finish", claim_id: "forged" }],
        env,
        coordinatorFactory: () => coordinator,
      })
    ).rejects.toThrow("cannot contain Claim authority fields");
    await expect(
      reconcileDispatchWorkCoordinator({
        messages: [{ type: "dispatch_claim_finish", outcome: { nested: { work_id: "forged" } } }],
        env,
        coordinatorFactory: () => coordinator,
      })
    ).rejects.toThrow("Claim authority fields");
    await expect(
      reconcileDispatchWorkCoordinator({
        messages: [{ type: "dispatch_claim_finish" }, { type: "dispatch_claim_finish" }],
        env,
        coordinatorFactory: () => coordinator,
      })
    ).rejects.toThrow("Only one dispatch_claim_finish intent is allowed");
  });

  it("rejects finish when trusted assignment is absent or provenance mismatches", async () => {
    await expect(
      reconcileDispatchWorkCoordinator({
        messages: [{ type: "dispatch_claim_finish" }],
        env: { ...env, GH_AW_DISPATCH_WORK_COORDINATOR_CONTEXT: "null" },
      })
    ).rejects.toThrow("requires a trusted assigned Claim");
    const coordinator = createCoordinator();
    await expect(
      reconcileDispatchWorkCoordinator({
        messages: [{ type: "dispatch_claim_finish" }],
        env: { ...env, GITHUB_RUN_ID: "999" },
        coordinatorFactory: () => coordinator,
      })
    ).rejects.toThrow("provenance could not be verified");
  });
});
