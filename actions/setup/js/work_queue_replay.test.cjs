import { describe, expect, it, vi } from "vitest";
import { registerTests } from "./work_queue_replay_checks.cjs";
import { parseTransactionRecords } from "./work_queue_replay.cjs";
import { createRequire } from "node:module";
const require = createRequire(import.meta.url);
const { administrator, bind, evidence, finish, genesis, grant, operationCommit, reconciler, submission, workerActor } = require("./work_queue_test_helpers.cjs");
const { prepareCheckpoint, replayTransactions } = require("./work_queue_replay.cjs");
registerTests({ describe, it });

describe("shared JSONL work-queue framing", () => {
  it("preserves strict decoding across LF and CRLF records", () => {
    expect(parseTransactionRecords('{"id":1}\r\n{"id":2}\n')).toEqual([{ id: 1 }, { id: 2 }]);
  });

  it("enforces byte limits before shared JSON decoding, including multibyte records", () => {
    for (const text of ["x".repeat(8 * 1024 * 1024), "é".repeat(4 * 1024 * 1024)]) {
      const parse = vi.spyOn(JSON, "parse");
      try {
        expect(() => parseTransactionRecords(`"${text}"\n`)).toThrow(expect.objectContaining({ code: "resource_limit" }));
        expect(parse).not.toHaveBeenCalled();
      } finally {
        parse.mockRestore();
      }
    }
  });

  it.each([
    ['{"id":1,"id":2}\n', "duplicate_key"],
    ['{"id":1,"\\u0069d":2}\n', "duplicate_key"],
    ['{"id":1.0}\n', "noncanonical_number"],
    ['{"id":1e0}\n', "noncanonical_number"],
    ['{"id":9007199254740992}\n', "noncanonical_number"],
    ['{"id":"\\ud800"}\n', "invalid_unicode"],
    ['\ufeff{"id":1}\n', "codec_invalid"],
    ['\u00a0{"id":1}\n', "codec_invalid"],
    ['{"id":}\n', "codec_invalid"],
    ["{}\n\n", "ledger_invalid"],
    ["{}\n \r\n", "ledger_invalid"],
    ["{}", "ledger_invalid"],
  ])("retains strict rejection of %j", (input, code) => {
    expect(() => parseTransactionRecords(input)).toThrow(expect.objectContaining({ code }));
  });
});

describe("durable DeliveryEvidence replay", () => {
  function fixture(completed = true) {
    let log = [genesis()];
    log.push(submission(log, ["durable"]));
    const granted = grant(log);
    log.push(granted.commit);
    const dispatchId = granted.assignments[0].dispatch_id;
    log = bind(log, dispatchId);
    if (completed) log.push(finish(log, dispatchId, "h1", "completed"));
    const state = replayTransactions(log);
    const claim = state.claims.get(granted.operations[0].claim_id);
    const operation = {
      kind: "DeliveryEvidence",
      work_id: claim.work_id,
      claim_id: claim.claim_id,
      dispatch_id: dispatchId,
      claim_handle: "h1",
      completion_id: claim.terminal_commit_id || "not-completed",
      run_id: "200",
      run_attempt: 1,
      outputs: [{ index: 0, type: "create_issue", verifier: "builtin", receipt: { number: 123 } }],
      effects: [],
    };
    const actor = workerActor(state, dispatchId);
    return { log, operation, actor };
  }

  function publish(f, operation = f.operation, actor = f.actor, id = "delivery-evidence") {
    return [...f.log, operationCommit(f.log, id, "delivery_evidence", [operation], actor, 45)];
  }

  function result(log, operation) {
    const state = replayTransactions(log);
    const proof = evidence(state, state.dispatches.get(operation.dispatch_id), "delivery", 50, { source: "verified_receipts", run_id: "200", run_attempt: 1, receipt: "checked readback" });
    return operationCommit(log, "result", "result", [{ kind: "Result", work_id: operation.work_id, claim_id: operation.claim_id, completion_id: operation.completion_id, descriptor: { outputs: [] }, evidence: proof }], reconciler, 50);
  }

  it("retains authenticated original-worker receipts and allows identical republication", () => {
    const f = fixture();
    f.log = publish(f);
    expect(replayTransactions(f.log).deliveryEvidence.get(f.operation.claim_id)).toEqual(f.operation);
    expect(replayTransactions(publish(f, f.operation, f.actor, "same-evidence")).deliveryEvidence.size).toBe(1);
    expect(() => replayTransactions(publish(f, { ...f.operation, outputs: [] }, f.actor, "changed-evidence"))).toThrow(expect.objectContaining({ code: "delivery_conflict" }));
  });

  it("rejects evidence outside the completed, bound worker scope", () => {
    const f = fixture();
    expect(() => replayTransactions(publish(f, f.operation, reconciler))).toThrow(expect.objectContaining({ code: "actor_unauthorized" }));
    expect(() => replayTransactions(publish(fixture(false)))).toThrow(expect.objectContaining({ code: "claim_ineffective" }));
    for (const patch of [{ completion_id: "another-completion" }, { run_id: "201" }]) expect(() => replayTransactions(publish(f, { ...f.operation, ...patch }))).toThrow(expect.objectContaining({ code: "claim_scope_invalid" }));
    f.log = publish(f);
    f.log.push(result(f.log, f.operation));
    expect(replayTransactions(f.log).deliveryEvidence.size).toBe(0);
    expect(() => replayTransactions(publish(f, f.operation, f.actor, "late-evidence"))).toThrow(expect.objectContaining({ code: "delivery_conflict" }));
  });

  it.each([
    { outputs: Array.from({ length: 129 }, (_, index) => ({ index, type: "noop", verifier: "none" })) },
    { outputs: [{ index: 0, type: "custom", verifier: "rest_adapter", receipt: {}, adapter: JSON.stringify("x".repeat(8192)) }] },
    { outputs: [{ index: 0, type: "custom", verifier: "rest_adapter", receipt: {}, adapter: '{"x":1,"x":2}' }] },
    {
      outputs: [
        { index: 1, type: "noop", verifier: "none" },
        { index: 0, type: "noop", verifier: "none" },
      ],
    },
    { outputs: [{ index: 0, type: "create_issue", verifier: "builtin" }] },
    { outputs: [{ index: 0, type: "noop", verifier: "none", receipt: {} }] },
    { outputs: [{ index: 0, type: "custom", verifier: "builtin", receipt: { body: "x".repeat(32768) } }] },
  ])("rejects malformed or oversized evidence %#", patch => {
    const f = fixture();
    expect(() => replayTransactions(publish(f, { ...f.operation, ...patch }))).toThrow();
  });

  it("checkpoints pending evidence and removes terminal evidence without changing legacy snapshots", () => {
    const f = fixture();
    const legacy = prepareCheckpoint(f.log, "a".repeat(40), administrator, 60);
    expect(legacy[0].operations[0].state).not.toHaveProperty("delivery_evidence");
    expect(replayTransactions(legacy).deliveryEvidence.size).toBe(0);
    f.log = publish(f);
    const compacted = prepareCheckpoint(f.log, "a".repeat(40), administrator, 60);
    expect(replayTransactions(compacted).deliveryEvidence.get(f.operation.claim_id)).toEqual(f.operation);
    f.log.push(result(f.log, f.operation));
    const terminal = prepareCheckpoint(f.log, "a".repeat(40), administrator, 60);
    expect(terminal[0].operations[0].state).not.toHaveProperty("delivery_evidence");
    expect(replayTransactions(terminal).deliveryEvidence.size).toBe(0);
  });
});
