import { describe, expect, it } from "vitest";

const { sessionIDFromStream } = await import("./pi_session_export.cjs");

describe("Pi session export", () => {
  it("finds the real session header in mixed diagnostics and JSONL", () => {
    expect(sessionIDFromStream('diagnostic\n{"type":"session","id":"session-1"}\n{"type":"agent_settled"}')).toBe("session-1");
  });
  it("reports missing session identity", () => {
    expect(() => sessionIDFromStream('{"type":"agent_settled"}')).toThrow("session header");
  });
});
