// @ts-check
import { describe, expect, it, vi } from "vitest";
import { applyTransactions, compactTransactions, parseTransactionLog, replayTransactions, serializeTransactionLog, validateTransaction } from "./dispatch_work_coordinator_replay.cjs";

const work = id => ({ kind: "Work", sequence: 1, version: 2, work: { legacy_work_id: id }, work_id: id });
const claim = (workId, id) => ({ claim_id: id, kind: "Claim", run_id: `legacy:${id}`, version: 2, work_id: workId });
const cancelClaim = (workId, id) => ({ claim_id: id, kind: "ClaimCancellation", version: 2, work_id: workId });
const complete = (workId, claimId, attempt) => ({ attempt_id: attempt, claim_id: claimId, kind: "Completion", version: 2, work_id: workId });
const cancelWork = id => ({ kind: "WorkCancellation", version: 2, work_id: id });

function permutations(items) {
  if (items.length < 2) return [items];
  return items.flatMap((item, index) => permutations([...items.slice(0, index), ...items.slice(index + 1)]).map(permutation => [item, ...permutation]));
}

describe("dispatch work coordinator replay", () => {
  it("logs debug summaries without transaction identifiers", () => {
    const previousDebug = process.env.DEBUG;
    const errorSpy = vi.spyOn(console, "error").mockImplementation(() => {});
    process.env.DEBUG = "dispatch_work_coordinator";
    try {
      replayTransactions([work("submitted-work-id")]);

      expect(errorSpy).toHaveBeenCalledOnce();
      expect(errorSpy.mock.calls.flat().join(" ")).not.toContain("submitted-work-id");
    } finally {
      if (previousDebug === undefined) delete process.env.DEBUG;
      else process.env.DEBUG = previousDebug;
      errorSpy.mockRestore();
    }
  });

  it("projects work and claim states from the transaction facts", () => {
    expect(replayTransactions([work("w"), claim("w", "c")])).toEqual({
      work: { w: "claimed" },
      winner: { w: "c" },
      claim: { c: "effective" },
      transactions: [claim("w", "c"), work("w")],
    });
  });

  it("produces the same projection for every ordering of a valid fact set", () => {
    const transactions = [work("w"), claim("w", "a"), claim("w", "b"), cancelClaim("w", "a"), work("other")];
    const expected = replayTransactions(transactions);
    for (const permutation of permutations(transactions)) {
      expect(replayTransactions(permutation)).toEqual(expected);
      expect(JSON.stringify(replayTransactions(permutation))).toBe(JSON.stringify(expected));
    }
    expect(expected.winner.w).toBe("b");
    expect(expected.work.other).toBe("available");
  });

  it("supports prototype-named identifiers without treating inherited members as state", () => {
    const result = applyTransactions([], [claim("constructor", "toString"), cancelWork("valueOf"), work("__proto__"), claim("__proto__", "constructor")]);

    expect(result.rejected.map(item => item.reason)).toEqual(["work does not exist", "work does not exist"]);
    const projection = replayTransactions(result.transactions);
    expect(projection.work["__proto__"]).toBe("claimed");
    expect(projection.winner["__proto__"]).toBe("constructor");
    expect(projection.claim.constructor).toBe("effective");
  });

  it("uses lexicographic claim identity for deterministic arbitration", () => {
    const projection = replayTransactions([work("w"), claim("w", "claim-2"), claim("w", "claim-10")]);
    expect(projection.winner.w).toBe("claim-10");
    expect(projection.claim).toEqual({ "claim-10": "effective", "claim-2": "superseded" });
  });

  it("promotes the next active claim after cancellation", () => {
    const projection = replayTransactions([work("w"), claim("w", "a"), claim("w", "b"), cancelClaim("w", "a")]);
    expect(projection.winner.w).toBe("b");
    expect(projection.claim).toEqual({ a: "cancelled", b: "effective" });
  });

  it("fixes the winner when a valid completion exists", () => {
    const transactions = [work("w"), claim("w", "a"), claim("w", "z"), complete("w", "a", "run-1")];
    const projections = permutations(transactions).map(replayTransactions);
    expect(projections.every(projection => projection.work.w === "completed" && projection.winner.w === "a")).toBe(true);
  });

  it("cancels work and all its claims", () => {
    const projection = replayTransactions([work("w"), claim("w", "a"), cancelWork("w")]);
    expect(projection.work.w).toBe("cancelled");
    expect(projection.winner.w).toBeNull();
    expect(projection.claim.a).toBe("cancelled");
  });

  it("treats identical physical records as idempotent and compacts to a stable order", () => {
    const transactions = [claim("w", "c"), work("w"), claim("w", "c"), work("w")];
    const compacted = compactTransactions(transactions);
    expect(compacted).toEqual(compactTransactions([...transactions].reverse()));
    expect(compacted).toHaveLength(2);
    expect(replayTransactions(compacted)).toEqual(replayTransactions(transactions));

    const applied = applyTransactions(compacted, [claim("w", "c")]);
    expect(applied.rejected).toEqual([]);
    expect(applied.transactions).toHaveLength(2);
    expect(replayTransactions(applyTransactions(transactions, [claim("w", "later")]).transactions)).toEqual(replayTransactions(applyTransactions(compacted, [claim("w", "later")]).transactions));
  });

  it("returns immutable normalized facts instead of retaining caller-owned objects", () => {
    const submittedWork = work("w");
    const applied = applyTransactions([], [submittedWork]);
    submittedWork.work.legacy_work_id = "tampered";

    expect(replayTransactions(applied.transactions).work).toEqual({ w: "available" });
    expect(Object.isFrozen(applied.transactions[0])).toBe(true);
  });

  it("parses and serializes a canonical JSONL transaction log", () => {
    const log = serializeTransactionLog([claim("w", "c"), work("w"), claim("w", "c")]);
    expect(log).toBe(`${JSON.stringify(claim("w", "c"))}\n${JSON.stringify(work("w"))}\n`);
    expect(parseTransactionLog(log)).toEqual([claim("w", "c"), work("w")]);
    expect(parseTransactionLog("")).toEqual([]);
  });

  it("upgrades unversioned and version-zero records to the current protocol", () => {
    const oldWork = { kind: "Work", work: "w", claim: null, attempt: null };
    const oldClaim = { version: 0, kind: "Claim", work: "w", claim: "c", attempt: null };
    const upgraded = parseTransactionLog(`${JSON.stringify(oldWork)}\n${JSON.stringify(oldClaim)}\n`);
    expect(upgraded).toEqual([work("w"), claim("w", "c")]);
    expect(serializeTransactionLog(upgraded)).toBe(serializeTransactionLog([work("w"), claim("w", "c")]));
    expect(() => validateTransaction(oldWork)).toThrow("unsupported");
  });

  it("rejects unknown or malformed message versions and never accepts partial upgrades", () => {
    for (const version of [-1, 1.5, "1", 3, null]) {
      expect(() => parseTransactionLog(`${JSON.stringify(work("w"))}\n${JSON.stringify({ ...claim("w", "c"), version })}\n`)).toThrow("unsupported dispatch coordinator transaction version");
    }
    expect(() => parseTransactionLog(`${JSON.stringify({ kind: "Work", work: "w", claim: null, attempt: null, extra: true })}\n`)).toThrow("exactly version");
  });

  it("rejects malformed JSONL records and blank lines", () => {
    expect(() => parseTransactionLog("{")).toThrow("malformed JSON");
    expect(() => parseTransactionLog(`${JSON.stringify(work("w"))}\n\n`)).toThrow("blank lines");
    expect(() => parseTransactionLog(`${JSON.stringify(work("w"))}\n${JSON.stringify(claim("missing", "c"))}\n`)).toThrow("missing work");
  });

  it("rejects malformed transaction shapes and identifiers", () => {
    for (const transaction of [null, [], { ...work("w"), unexpected: true }, { ...work(""), kind: "Claim" }, { ...claim("w", "c"), attempt: "run" }, { ...complete("w", "c", "run"), attempt: null }, { ...work("w"), kind: "Unknown" }]) {
      expect(() => validateTransaction(transaction)).toThrow(TypeError);
    }
  });

  it("rejects payload numbers that cannot cross runtimes safely", () => {
    for (const payload of [{ nested: [9007199254740992] }, { nested: { number: -9007199254740992 } }, { number: Infinity }]) {
      expect(() => validateTransaction({ ...work("w"), work: payload })).toThrow("JavaScript-safe range");
    }
    expect(() => validateTransaction({ ...work("w"), work: { min: -9007199254740991, max: 9007199254740991, fraction: 1.5 } })).not.toThrow();
  });

  it("rejects missing references and references to a different work item", () => {
    expect(() => replayTransactions([cancelClaim("w", "missing")])).toThrow("missing work");
    expect(() => replayTransactions([work("w"), cancelClaim("w", "missing")])).toThrow("missing claim");
    expect(() => replayTransactions([work("w"), work("other"), claim("w", "c"), complete("other", "c", "run")])).toThrow("different work");
  });

  it("rejects a completion for a superseded or cancelled claim", () => {
    expect(() => replayTransactions([work("w"), claim("w", "a"), claim("w", "b"), complete("w", "b", "run")])).toThrow("winning claim");
    expect(() => replayTransactions([work("w"), claim("w", "a"), cancelClaim("w", "a"), complete("w", "a", "run")])).toThrow("winning claim");
  });

  it("rejects conflicting claim identities, repeated attempts, and multiple terminal facts", () => {
    expect(() => replayTransactions([work("w"), work("other"), claim("w", "c"), claim("other", "c")])).toThrow("conflicting transactions");
    expect(() => replayTransactions([work("w"), claim("w", "a"), complete("w", "a", "run"), complete("w", "a", "run-2")])).toThrow("multiple terminal");
    expect(() => replayTransactions([work("w"), claim("w", "a"), complete("w", "a", "run"), cancelWork("w")])).toThrow("multiple terminal");
    expect(() => replayTransactions([work("w"), claim("w", "a"), complete("w", "a", "run"), work("other"), claim("other", "b"), complete("other", "b", "run")])).toThrow("multiple completions");
  });
});

describe("dispatch work coordinator transaction application", () => {
  it("applies valid intents in order and rejects state changes after terminal work", () => {
    const result = applyTransactions([], [work("w"), claim("w", "c"), complete("w", "c", "run"), claim("w", "late"), cancelWork("w")]);
    expect(replayTransactions(result.transactions).work.w).toBe("completed");
    expect(result.rejected).toEqual([
      { transaction: claim("w", "late"), reason: "work is terminal" },
      { transaction: cancelWork("w"), reason: "work is terminal" },
    ]);
  });

  it("reports invalid intents without adding them to the durable candidate", () => {
    const result = applyTransactions([work("w"), claim("w", "winner")], [claim("missing", "c"), claim("w", "winner"), complete("w", "unknown", "run"), complete("w", "winner", "run")]);
    expect(result.rejected).toEqual([
      { transaction: claim("missing", "c"), reason: "work does not exist" },
      { transaction: complete("w", "unknown", "run"), reason: "claim does not exist" },
    ]);
    expect(replayTransactions(result.transactions).work.w).toBe("completed");
  });

  it("rejects mutations with mismatched claim ownership", () => {
    const result = applyTransactions([work("w"), work("other"), claim("w", "c")], [cancelClaim("other", "c"), complete("other", "c", "run")]);
    expect(result.rejected.map(item => item.reason)).toEqual(["claim belongs to different work", "claim belongs to different work"]);
    expect(replayTransactions(result.transactions).claim.c).toBe("effective");
  });
});
