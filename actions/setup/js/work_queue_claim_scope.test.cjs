// @ts-check
import { afterEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import {
  assertClaimAuthorized,
  assertClaimIdentity,
  claimArtifactPath,
  claimIdentity,
  createClaimEffectBatch,
  discardClaimEffectBatch,
  markClaimEffects,
  normalizeAssignment,
  normalizeClaimScope,
  normalizeRuntimeMessage,
  readClaimScopeContext,
  recordClaimEffect,
  scopedArtifactFilename,
  withClaimEffectBatch,
  withClaimExecution,
  withClaimOutputType,
} from "./work_queue_claim_scope.cjs";

const assignment = count => ({
  dispatch_id: "d",
  claims: Array.from({ length: count }, (_, index) => ({ handle: `h${index + 1}`, claim_id: `c${index + 1}`, work_id: `w${index + 1}`, work: { plan: `task${index + 1}` }, result_refs: [] })),
});
// A valid singleton of a different Dispatch reusing the same handle `h1`.
const otherDispatch = () => ({ dispatch_id: "d2", claims: [{ handle: "h1", claim_id: "c2", work_id: "w2", work: { plan: "task2" }, result_refs: [] }] });
const errorCode = callback => {
  try {
    callback();
  } catch (error) {
    return error.code;
  }
  return undefined;
};

const directories = [];
function writeSnapshot(snapshot) {
  const directory = fs.mkdtempSync(path.join(fs.realpathSync(os.tmpdir()), "gh-aw-claim-scope-"));
  directories.push(directory);
  const file = path.join(directory, "snapshot.json");
  fs.writeFileSync(file, JSON.stringify(snapshot));
  vi.stubEnv("GH_AW_WORK_QUEUE_ENABLED", "true");
  vi.stubEnv("GH_AW_WORK_QUEUE_SNAPSHOT", file);
  return file;
}

function snapshot(overrides = {}) {
  return { version: 4, sha: "a".repeat(40), transactionLog: "", origin: { role: "worker", repository: "owner/repo" }, role: "worker", worker: assignment(1), ...overrides };
}

afterEach(() => {
  vi.unstubAllEnvs();
  vi.unstubAllGlobals();
  for (const directory of directories.splice(0)) fs.rmSync(directory, { recursive: true, force: true });
});

describe("v4 snapshot assignment", () => {
  it("matches the shared identity fixture for every assignment identifier", () => {
    const fixture = JSON.parse(fs.readFileSync(path.resolve(import.meta.dirname, "../../../specs/work-queue/fixtures/identity-validation.json"), "utf8"));
    expect(fixture.version).toBe(4);
    const locations = { assignment: ["dispatch_id"], claim: ["handle", "claim_id", "work_id"], result: ["work_id", "result_commit_id"] };
    for (const [location, fields] of Object.entries(locations)) {
      for (const field of fields) {
        for (const entry of fixture.identity) {
          const id = entry.text.repeat(entry.repeat);
          const value = assignment(1);
          const reference = { work_id: "dependency", result_commit_id: "result", descriptor: [] };
          value.claims[0].result_refs = [reference];
          Object.assign(location === "assignment" ? value : location === "claim" ? value.claims[0] : reference, { [field]: id });
          if (entry.valid) expect(normalizeAssignment(value)).toEqual(value);
          else expect(() => normalizeAssignment(value), `${location}.${field}: ${entry.name}`).toThrow(/invalid/);
        }
      }
    }
  });

  it("keeps Work payloads and predecessor effects opaque and frozen", () => {
    const value = assignment(1);
    value.claims[0].work = { graph_id: "x".repeat(400), control: "\u0001" };
    value.claims[0].result_refs = [{ work_id: "p", result_commit_id: "q_1", descriptor: [{ kind: "create_issue", resource: { repository: "owner/repo", number: 1 } }] }];
    const normalized = normalizeAssignment(value);
    expect(normalized).toEqual(value);
    expect(Object.isFrozen(normalized.claims[0].result_refs[0].descriptor)).toBe(true);
  });

  it("carries a validated continuation beside each Claim's payload", () => {
    const value = assignment(1);
    value.claims[0].continuation = { route: "workqueue", message: "Audit page 2", remaining: 0 };
    const normalized = normalizeAssignment(value);
    expect(normalized).toEqual(value);
    expect(Object.isFrozen(normalized.claims[0].continuation)).toBe(true);
    for (const continuation of [
      { route: "dispatch", message: "x", remaining: 0 },
      { route: "workqueue", message: " ", remaining: 0 },
      { route: "issue", message: "x", remaining: 1 },
      { route: "workqueue", message: "x", remaining: 0, target: "main" },
    ]) {
      value.claims[0].continuation = continuation;
      expect(() => normalizeAssignment(value)).toThrow(/invalid assignment continuation/);
    }
  });

  it("rejects v3 fields, assignments of other than exactly one Claim, and object descriptors", () => {
    for (const value of [
      { ...assignment(1), version: 3 },
      { ...assignment(1), pool: "default" },
      { work_id: "w", claim_id: "c" },
    ]) {
      expect(() => normalizeAssignment(value)).toThrow();
    }
    for (const count of [0, 2, 17]) expect(errorCode(() => normalizeAssignment(assignment(count)))).toBe("claim_scope_invalid");
    // Cardinality is rejected before any member is chosen, even when the first member is valid.
    const invalidSecond = assignment(2);
    invalidSecond.claims[1] = { handle: "h1" };
    expect(() => normalizeAssignment(invalidSecond)).toThrow(/exactly one open Claim/);
    const objectDescriptor = assignment(1);
    objectDescriptor.claims[0].result_refs = [{ work_id: "p", result_commit_id: "q", descriptor: {} }];
    expect(() => normalizeAssignment(objectDescriptor)).toThrow(/predecessor result/);
    const duplicate = assignment(1);
    duplicate.claims[0].result_refs = ["p", "p"].map(work_id => ({ work_id, result_commit_id: "q", descriptor: [] }));
    expect(() => normalizeAssignment(duplicate)).toThrow(/duplicate predecessor/);
    const oversized = assignment(1);
    oversized.claims[0].work = { blob: "x".repeat(70 * 1024) };
    expect(() => normalizeAssignment(oversized)).toThrow(/ceiling/);
  });
});

describe("Claim scope of messages", () => {
  it("preserves unscoped message identity unless normalizing a historical alias", () => {
    vi.stubEnv("GH_AW_WORK_QUEUE_ENABLED", "false");
    const message = Object.freeze({ type: "create_issue", body: "unchanged" });
    expect(normalizeRuntimeMessage(message)).toBe(message);
    expect(normalizeRuntimeMessage({ type: "missing_tool" })).toEqual({ type: "report_missing_tool" });
  });

  it("selects the sole Claim on omission and rejects foreign or authority-replacing scope", () => {
    expect(normalizeClaimScope({ type: "noop" }, assignment(1))).toEqual({ type: "noop", claim_handle: "h1" });
    expect(normalizeClaimScope({ type: "noop" }, otherDispatch())).toEqual({ type: "noop", claim_handle: "h1" });
    expect(() => normalizeClaimScope({ type: "noop", claim_handle: "h1" }, assignment(2))).toThrow(/exactly one open Claim/);
    for (const claim_handle of [null, "", {}, 1, "foreign", "h2"]) expect(() => normalizeClaimScope({ type: "noop", claim_handle }, assignment(1))).toThrow();
    expect(() => normalizeClaimScope({ claim_handle: "h1", claim_id: "c2" }, assignment(1))).toThrow(/conflicts/);
    expect(() => normalizeClaimScope({ work_id: "w1" }, otherDispatch())).toThrow(/conflicts/);
    for (const field of ["authorized", "run_id", "run_attempt", "assignment", "work_queue_assignment"]) {
      expect(() => normalizeClaimScope({ claim_handle: "h1", [field]: null }, assignment(1))).toThrow(/trusted authority/);
    }
  });

  it("namespaces prepared artifacts and factories by full Claim identity, not the reused handle", async () => {
    const base = "/workspace/artifacts";
    const first = await withClaimExecution({ assignment: assignment(1), claim_handle: "h1" }, () => scopedArtifactFilename(`${base}/aw-patch.patch`));
    const second = await withClaimExecution({ assignment: otherDispatch(), claim_handle: "h1" }, () => scopedArtifactFilename(`${base}/aw-patch.patch`));
    expect(first).not.toBe(second);
    expect(first).toBe(`${claimArtifactPath(base, "h1", assignment(1))}/aw-patch.patch`);
    expect(second).toBe(`${claimArtifactPath(base, "h1", otherDispatch())}/aw-patch.patch`);
    const factory = withClaimExecution({ assignment: assignment(1), claim_handle: "h1" }, () => claimIdentity("h1"));
    expect(() => withClaimExecution({ assignment: otherDispatch(), claim_handle: "h1" }, () => assertClaimIdentity(factory))).toThrow(/cannot escape/);
    expect(() => withClaimExecution({ assignment: assignment(1), claim_handle: "h1" }, () => assertClaimIdentity(factory))).not.toThrow();
  });
});

describe("claim scope context", () => {
  it("reads a worker snapshot that matches this run's dispatch input", () => {
    writeSnapshot(snapshot());
    vi.stubGlobal("context", { payload: { inputs: { work_queue_assignment: '{"version":4,"dispatch_id":"d"}' } } });
    expect(readClaimScopeContext()?.assignment).toEqual(assignment(1));
    vi.stubGlobal("context", { payload: { inputs: { work_queue_assignment: '{"version":4,"dispatch_id":"other"}' } } });
    expect(() => readClaimScopeContext()).toThrow(/does not match/);
  });

  it("treats a settled worker snapshot as unassigned", () => {
    writeSnapshot(snapshot({ worker: null }));
    vi.stubGlobal("context", { payload: { inputs: { work_queue_assignment: '{"version":4,"dispatch_id":"d"}' } } });
    expect(readClaimScopeContext()).toMatchObject({ assignment: null });
    expect(() => normalizeRuntimeMessage({ type: "create_issue" })).toThrow(/unassigned/);
  });

  it("rejects v3 envelopes and nonworkers carrying an assignment", () => {
    writeSnapshot({ ...snapshot(), version: 3, captured_at: 1 });
    expect(() => readClaimScopeContext()).toThrow();
    writeSnapshot(snapshot({ role: "dispatcher" }));
    expect(() => readClaimScopeContext()).toThrow(/assignment/);
  });

  it("returns null for an explicit observer", () => {
    vi.stubEnv("GH_AW_WORK_QUEUE_ROLE", "observer");
    writeSnapshot(snapshot({ role: "observer", worker: null }));
    vi.stubGlobal("context", { payload: {} });
    expect(readClaimScopeContext()).toBeNull();
  });
});

describe("Claim authorization", () => {
  it("requires a fresh same-Claim proof", async () => {
    const authorize = async request => ({ authorized: request.assignment.dispatch_id === "d", claim_handle: request.claim_handle, state: request.assignment.dispatch_id === "d" ? "open" : "unbound" });
    await withClaimExecution({ assignment: assignment(1), claim_handle: "h1", authorize }, async () => {
      expect(await assertClaimAuthorized({ type: "noop", claim_handle: "h1" })).toEqual({ type: "noop", claim_handle: "h1" });
      await expect(assertClaimAuthorized({ type: "noop", claim_handle: "h2" })).rejects.toThrow(/foreign/);
      await expect(assertClaimAuthorized({ type: "noop", claim_handle: "h1" }, { authorize: async () => ({ authorized: true, claim_handle: "h2" }) })).rejects.toThrow(/same-Claim/);
    });
    expect(() => withClaimExecution({ assignment: assignment(1), claim_handle: "h2", authorize }, () => {})).toThrow(/foreign Claim/);
    await withClaimExecution({ assignment: otherDispatch(), claim_handle: "h1", authorize }, async () => {
      await expect(assertClaimAuthorized({ type: "noop", claim_handle: "h1" })).rejects.toMatchObject({ code: "claim_not_authorized", suppressed: false });
    });
  });

  it.each([
    ["cancelled", "claim_cancelled"],
    ["completed", "claim_already_settled"],
    ["failed", "claim_failed"],
  ])("reports a %s Claim as suppressed", async (state, code) => {
    const authorize = async request => ({ authorized: false, suppressed: true, state, claim_handle: request.claim_handle });
    await withClaimExecution({ assignment: assignment(1), claim_handle: "h1", authorize }, async () => {
      await expect(assertClaimAuthorized({ type: "create_issue" })).rejects.toMatchObject({ code, state, suppressed: true });
    });
  });

  it("rejects proofs that suppress an open Claim or authorize a suppressed one", async () => {
    for (const proof of [
      { authorized: false, suppressed: true, state: "open" },
      { authorized: true, suppressed: true, state: "completed" },
    ]) {
      await withClaimExecution({ assignment: assignment(1), claim_handle: "h1", authorize: async () => ({ ...proof, claim_handle: "h1" }) }, async () => {
        await expect(assertClaimAuthorized({ type: "create_issue" })).rejects.toThrow(/proof/);
      });
    }
  });

  it("blocks resource effects in staged mode and for no-write Work", async () => {
    vi.stubEnv("GH_AW_SAFE_OUTPUTS_STAGED", "true");
    const log = vi.spyOn(console, "log").mockImplementation(() => {});
    const authorize = vi.fn();
    try {
      await withClaimExecution({ assignment: assignment(1), claim_handle: "h1", authorize }, async () => {
        await expect(assertClaimAuthorized({ type: "create_issue" }, { effect: true, resource: {} })).rejects.toThrow(/read-only Claim preview/);
      });
    } finally {
      log.mockRestore();
    }
    vi.stubEnv("GH_AW_SAFE_OUTPUTS_STAGED", "false");
    const noWrites = assignment(1);
    noWrites.claims[0].work = { effect_contract: { kind: "none" } };
    await withClaimExecution({ assignment: noWrites, claim_handle: "h1", authorize }, async () => {
      await expect(assertClaimAuthorized({ type: "create_issue" }, { effect: true, resource: {} })).rejects.toThrow(/prohibits resource writes/);
    });
    expect(authorize).not.toHaveBeenCalled();
  });
});

describe("Claim effect inventory", () => {
  it("tags effects with the current output type, discards batches and marks unverified uploads", async () => {
    const effects = [];
    await withClaimExecution({ assignment: assignment(1), claim_handle: "h1", effects }, async () => {
      withClaimOutputType("upload_code_coverage", () => recordClaimEffect({ repository: "owner/repo", kind: "code_coverage", outcome: "succeeded" }));
      const batch = createClaimEffectBatch();
      withClaimEffectBatch(batch, () => recordClaimEffect({ repository: "owner/repo", kind: "git_blob", outcome: "succeeded" }));
      expect(discardClaimEffectBatch(batch)).toBe(1);
      expect(markClaimEffects("code_coverage", { status: "unverified" })).toBe(1);
    });
    expect(effects).toEqual([{ repository: "owner/repo", kind: "code_coverage", outcome: "succeeded", output_type: "upload_code_coverage", status: "unverified", dispatch_id: "d", claim_id: "c1", work_id: "w1", claim_handle: "h1" }]);
  });

  it("records nothing outside a Claim execution", () => {
    expect(recordClaimEffect({ kind: "issue" })).toBeNull();
    expect(markClaimEffects("issue", {})).toBe(0);
  });
});
