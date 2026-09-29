import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";

describe("ledger_mutation.cjs handler", () => {
  let mockCore;

  beforeEach(() => {
    mockCore = {
      info: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
    };
    global.core = mockCore;
  });

  afterEach(() => {
    delete global.core;
    vi.clearAllMocks();
  });

  const auditEntry = () => ({
    type: "ledger_mutation",
    operation: "append",
    timestamp: "2026-01-01T00:00:00.000Z",
    record: {
      id: "6f2bb1a6-3f4b-4a6b-9c4a-1b2c3d4e5f60",
      type: "observation",
      timestamp: "2026-01-01T00:00:00.000Z",
      parents: ["sha256:aaaa"],
      sha: "sha256:bbbb",
      payload_sha: "sha256:cccc",
    },
  });

  it("logs append metadata without performing side effects", async () => {
    const { main } = await import("./ledger_mutation.cjs");
    const handler = await main({});

    const result = await handler(auditEntry());

    expect(result.success).toBe(true);
    expect(result.operation).toBe("append");
    expect(result.record_id).toBe("6f2bb1a6-3f4b-4a6b-9c4a-1b2c3d4e5f60");
    expect(result.record_sha).toBe("sha256:bbbb");
    expect(result.payload_sha).toBe("sha256:cccc");
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Recorded ledger append"));
  });

  it("rejects entries without an operation", async () => {
    const { main } = await import("./ledger_mutation.cjs");
    const handler = await main({});

    const entry = auditEntry();
    delete entry.operation;
    const result = await handler(entry);

    expect(result.success).toBe(false);
    expect(result.error).toBe("Missing required field: operation");
  });

  it("enforces the configured max count", async () => {
    const { main } = await import("./ledger_mutation.cjs");
    const handler = await main({ max: 1 });

    expect((await handler(auditEntry())).success).toBe(true);
    const second = await handler(auditEntry());

    expect(second.success).toBe(false);
    expect(second.error).toBe("Max count of 1 reached");
  });

  it("truncates unexpectedly long field values", async () => {
    const { main } = await import("./ledger_mutation.cjs");
    const handler = await main({});

    const entry = auditEntry();
    entry.record.type = "x".repeat(500);
    const result = await handler(entry);

    expect(result.success).toBe(true);
    expect(result.record_type.length).toBe(129);
    expect(result.record_type.endsWith("…")).toBe(true);
  });

  it("neutralizes workflow commands and line breaks in untrusted audit fields", async () => {
    const { main } = await import("./ledger_mutation.cjs");
    const handler = await main({});
    const entry = auditEntry();
    entry.record.type = "note\n::error::spoofed\r::add-mask::hidden";
    const result = await handler(entry);
    expect(result.record_type).not.toMatch(/[\r\n]|::/);
    expect(mockCore.info.mock.calls.flat().join("\n")).not.toContain("::error::");
  });

  it("tolerates entries without record metadata", async () => {
    const { main } = await import("./ledger_mutation.cjs");
    const handler = await main({});

    const result = await handler({ type: "ledger_mutation", operation: "append" });

    expect(result.success).toBe(true);
    expect(result.record_id).toBe("");
  });
});
