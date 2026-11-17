import { describe, it, expect, vi } from "vitest";
import fs from "node:fs";
import path from "node:path";

const { withExecutionTrace, MAX_EXECUTION_TRACE_LENGTH } = require("./safe_output_trace.cjs");
const { withTransportIntegrity, TransportIntegrityError } = require("./transport_integrity.cjs");
const terminal = { success: "x", failure: "y", error: "z" };

describe("safe-output execution traces", () => {
  it("preserves result identity, arguments, receiver and nested branch order", async () => {
    const result = { success: true };
    const receiver = { value: 42 };
    const handler = withExecutionTrace(terminal)(async function (message, ids, trace) {
      expect(this).toBe(receiver);
      expect(message).toEqual({ value: 42 });
      expect(ids).toEqual({ temp: 1 });
      trace("A");
      const nested = () => trace("b");
      nested();
      trace("C");
      return result;
    });
    expect(await handler.call(receiver, { value: 42 }, { temp: 1 })).toBe(result);
    expect(result.execution_trace).toBe("AbCx");
    expect(JSON.parse(JSON.stringify(result)).execution_trace).toBe("AbCx");
  });

  it.each([{ success: false }, { skipped: true }, { success: true, skipped: true }])("marks unsuccessful/skipped returns: %j", async result => {
    const handler = withExecutionTrace(terminal)(async trace => {
      trace("A");
      return result;
    });
    expect(await handler()).toBe(result);
    expect(result.execution_trace).toBe("Ay");
  });

  it("preserves thrown Error identity, type, message and cause", async () => {
    const cause = new Error("cause");
    const error = new TypeError("original", { cause });
    const handler = withExecutionTrace(terminal)(async trace => {
      trace("A");
      await Promise.resolve();
      trace("B");
      throw error;
    });
    await expect(handler()).rejects.toBe(error);
    expect(error.execution_trace).toBe("ABz");
    expect(error).toBeInstanceOf(TypeError);
    expect(error.message).toBe("original");
    expect(error.cause).toBe(cause);
  });

  it("traces synchronous failures and does not replace primitive throws", async () => {
    const error = new Error("sync");
    const handler = withExecutionTrace(terminal)(trace => {
      trace("A");
      throw error;
    });
    await expect(handler()).rejects.toBe(error);
    expect(error.execution_trace).toBe("Az");
    await expect(
      withExecutionTrace(terminal)(() => {
        throw "original";
      })()
    ).rejects.toBe("original");
  });

  it("keeps overlapping and sequential invocations private", async () => {
    let release;
    const blocked = new Promise(resolve => {
      release = resolve;
    });
    const handler = withExecutionTrace(terminal)(async (first, trace) => {
      if (first) {
        trace("A");
        await blocked;
        trace("B");
      } else {
        trace("C");
      }
      return { success: true };
    });
    const first = handler(true);
    expect((await handler(false)).execution_trace).toBe("Cx");
    release();
    expect((await first).execution_trace).toBe("ABx");
    expect((await handler(false)).execution_trace).toBe("Cx");
  });

  it("bounds paths and reserves the final outcome without recording invalid markers or inputs", async () => {
    const secret = "untrusted-input-do-not-record";
    const handler = withExecutionTrace(terminal)(async (input, trace) => {
      trace(input);
      trace("AB");
      trace("1");
      trace("\n");
      trace(undefined);
      for (let i = 0; i < MAX_EXECUTION_TRACE_LENGTH * 2; i++) trace("A");
      return { success: true };
    });
    const result = await handler(secret);
    expect(result.execution_trace).toBe("A".repeat(MAX_EXECUTION_TRACE_LENGTH - 1) + "x");
    expect(result.execution_trace).toHaveLength(1024);
    expect(result.execution_trace).not.toContain(secret);
  });

  it("does not turn immutable results or errors into tracing failures", async () => {
    const result = Object.freeze({ success: true });
    expect(await withExecutionTrace(terminal)(() => result)()).toBe(result);
    const error = Object.freeze(new Error("immutable"));
    await expect(
      withExecutionTrace(terminal)(() => {
        throw error;
      })()
    ).rejects.toBe(error);
    expect(await withExecutionTrace(terminal)(() => undefined)()).toBeUndefined();
  });

  it("wraps transport conversion and cleanup without introducing API effects", async () => {
    const previousCore = global.core;
    global.core = { error: vi.fn() };
    try {
      const handler = withExecutionTrace(
        terminal,
        withTransportIntegrity
      )(async (message, ids, transport, trace) => {
        expect(message).toEqual({ untouched: true });
        expect(ids).toEqual({});
        expect(typeof transport.assertUnchanged).toBe("function");
        trace("A");
        throw new TransportIntegrityError("artifact");
      });
      const result = await handler({ untouched: true }, {});
      expect(result).toMatchObject({ success: false, error_type: "ERR_TRANSPORT_INTEGRITY", execution_trace: "Ay" });
      expect(global.core.error).toHaveBeenCalledOnce();
    } finally {
      global.core = previousCore;
    }
  });

  it("retains route state when an outer wrapper fails after the handler returns", async () => {
    const error = new Error("cleanup failed");
    const wrap =
      handler =>
      async (...args) => {
        await handler(...args);
        throw error;
      };
    const handler = withExecutionTrace(
      terminal,
      wrap
    )(trace => {
      trace("A");
      return { success: true };
    });
    await expect(handler()).rejects.toBe(error);
    expect(error.execution_trace).toBe("Az");
  });

  it.each(["create_pull_request.cjs", "push_to_pull_request_branch.cjs"])("%s has unique, explicit, documented single-letter branch markers", filename => {
    const source = fs.readFileSync(path.join(__dirname, filename), "utf8");
    const calls = [...source.matchAll(/\btrace\(([^)]*)\)/g)].map(match => match[1]);
    expect(calls.length).toBeGreaterThan(40);
    for (const call of calls) expect(call).toMatch(/^"[A-Za-z]"$/);
    const markers = calls.map(call => JSON.parse(call));
    const outcomes = [...source.matchAll(/(?:success|failure|error): "([xyz])"/g)].map(match => match[1]);
    expect(outcomes).toEqual(["x", "y", "z"]);
    expect(new Set([...markers, ...outcomes]).size).toBe(markers.length + outcomes.length);
    expect(source).toContain("// Trace map:");
    expect(source).toContain('require("./safe_output_trace.cjs")');
  });
});
