// @ts-check
import { describe, it, expect } from "vitest";

const { buildWriteGateRecord } = require("./record_write_gate.cjs");
const identity = {
  GH_AW_WRITE_GATE_REPOSITORY: "owner/repo",
  GH_AW_WRITE_GATE_RUN_ID: "123",
  GH_AW_WRITE_GATE_RUN_ATTEMPT: "1",
  GH_AW_WRITE_GATE_HEAD_SHA: "a".repeat(40),
};

describe("compiler-owned write gate record", () => {
  it("records successful gate decisions without claiming item enforcement", () => {
    for (const verdict of ["allow", "deny", "partial"]) {
      const record = buildWriteGateRecord({
        ...identity,
        GH_AW_WRITE_GATE: verdict,
        GH_AW_WRITE_GATE_REASON: "policy",
        GH_AW_WRITE_GATE_JOB_RESULT: "success",
      });
      expect(record).toEqual({
        version: 1,
        repository: "owner/repo",
        run_id: "123",
        run_attempt: "1",
        head_sha: "a".repeat(40),
        verdict,
        reason: "policy",
        safe_outputs_result: "success",
      });
    }
  });

  it("narrows failed, skipped, cancelled, timed-out, or unknown gates to deny", () => {
    for (const result of ["failure", "cancelled", "timed_out", "skipped", undefined]) {
      const record = buildWriteGateRecord({ ...identity, GH_AW_WRITE_GATE: "allow", GH_AW_WRITE_GATE_JOB_RESULT: result });
      expect(record.verdict).toBe("deny");
      expect(record.reason).toBe("gate_unavailable");
    }
    for (const verdict of ["", "unknown", undefined, "ALLOW"]) {
      expect(buildWriteGateRecord({ ...identity, GH_AW_WRITE_GATE: verdict, GH_AW_WRITE_GATE_JOB_RESULT: "success" }).verdict).toBe("deny");
    }
  });

  it("rejects records without immutable run identity", () => {
    for (const key of Object.keys(identity)) {
      expect(() => buildWriteGateRecord({ ...identity, [key]: "" })).toThrow(/commit identity/);
    }
  });
});
