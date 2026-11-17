import { afterEach, describe, it, expect, vi } from "vitest";

const { withExecutionTrace, MAX_EXECUTION_TRACE_LENGTH } = require("./safe_output_trace.cjs");
const { withTransportIntegrity, TransportIntegrityError } = require("./transport_integrity.cjs");
const { processMessages } = require("./safe_output_handler_manager.cjs");
const { buildSafeOutputExecutions } = require("./safe_output_execution.cjs");
const terminal = { success: "done", failure: "fail", error: "thrw" };

afterEach(() => {
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
});

describe("safe-output execution traces", () => {
  it("preserves result identity, arguments, receiver and nested branch order", async () => {
    const result = { success: true };
    const receiver = { value: 42 };
    const handler = withExecutionTrace(terminal)(async function (trace, message, ids) {
      expect(this).toBe(receiver);
      expect(message).toEqual({ value: 42 });
      expect(ids).toEqual({ temp: 1 });
      trace("init");
      const nested = () => trace("bund");
      nested();
      trace("push");
      return result;
    });
    expect(await handler.call(receiver, { value: 42 }, { temp: 1 })).toBe(result);
    expect(result.execution_trace).toBe("init_bund_push_done");
    expect({ ...result }).toEqual({ success: true });
    expect(JSON.parse(JSON.stringify(result))).toEqual({ success: true });
    expect(buildSafeOutputExecutions([{ type: "create_pull_request", success: true, result }])[0].execution_trace).toBe("init_bund_push_done");
  });

  it.each([false, true])("keeps tracing in a fixed position when the temporary-ID map is supplied: %s", async includeMap => {
    const message = { type: "create_pull_request" };
    const ids = { temp: 1 };
    const map = new Map(Object.entries(ids));
    const handler = withExecutionTrace(terminal)(async (trace, receivedMessage, receivedIds, receivedMap) => {
      expect(receivedMessage).toBe(message);
      expect(receivedIds).toBe(ids);
      expect(receivedMap).toBe(includeMap ? map : undefined);
      trace("init");
      return { success: true };
    });
    const result = await handler(message, ids, ...(includeMap ? [map] : []));
    expect(result.execution_trace).toBe("init_done");
  });

  it.each(["success", "failed", "skipped", "thrown"])("projects non-enumerable traces through processMessages for %s handlers", async scenario => {
    vi.stubGlobal("core", { info: vi.fn(), debug: vi.fn(), warning: vi.fn(), error: vi.fn() });
    vi.stubGlobal("context", { repo: { owner: "org", repo: "repo" }, payload: {} });
    const handler = withExecutionTrace(terminal)(async (trace, message, ids, temporaryIdMap) => {
      expect(message.type).toBe("create_pull_request");
      expect(ids).toEqual({});
      expect(temporaryIdMap).toBeInstanceOf(Map);
      trace("init");
      if (scenario === "thrown") throw new Error("failed");
      return { success: scenario !== "failed", ...(scenario === "skipped" ? { skipped: true } : {}) };
    });
    const processing = await processMessages(new Map([["create_pull_request", handler]]), [{ type: "create_pull_request" }]);
    expect(buildSafeOutputExecutions(processing.results)[0]).toMatchObject({
      status: scenario === "thrown" ? "failed" : scenario,
      execution_trace: `init_${scenario === "success" ? "done" : scenario === "thrown" ? "thrw" : "fail"}`,
    });
  });

  it("preserves the terminal trace when a deferred retry returns an unsuccessful result", async () => {
    vi.stubGlobal("core", { info: vi.fn(), debug: vi.fn(), warning: vi.fn(), error: vi.fn() });
    vi.stubGlobal("context", { repo: { owner: "org", repo: "repo" }, payload: {} });
    let invocation = 0;
    const handler = vi.fn(
      withExecutionTrace(terminal)(async trace => {
        trace("init");
        if (++invocation === 1) {
          trace("wait");
          return { deferred: true };
        }
        trace("push");
        return { success: false, error: "retry failed" };
      })
    );
    const processing = await processMessages(new Map([["create_pull_request", handler]]), [{ type: "create_pull_request" }]);
    expect(handler).toHaveBeenCalledTimes(2);
    expect(processing.results[0]).toMatchObject({ success: false, deferred: false, execution_trace: "init_push_fail" });
    expect(buildSafeOutputExecutions(processing.results)[0]).toMatchObject({ status: "failed", execution_trace: "init_push_fail" });
  });

  it.each([{ success: false }, { skipped: true }, { success: true, skipped: true }])("marks unsuccessful/skipped returns: %j", async result => {
    const handler = withExecutionTrace(terminal)(async trace => {
      trace("init");
      return result;
    });
    expect(await handler()).toBe(result);
    expect(result.execution_trace).toBe("init_fail");
  });

  it("preserves thrown Error identity, type, message and cause", async () => {
    const cause = new Error("cause");
    const error = new TypeError("original", { cause });
    const handler = withExecutionTrace(terminal)(async trace => {
      trace("init");
      await Promise.resolve();
      trace("push");
      throw error;
    });
    await expect(handler()).rejects.toBe(error);
    expect(error.execution_trace).toBe("init_push_thrw");
    expect(error).toBeInstanceOf(TypeError);
    expect(error.message).toBe("original");
    expect(error.cause).toBe(cause);
    expect({ ...error }).not.toHaveProperty("execution_trace");
    expect(JSON.parse(JSON.stringify(error))).not.toHaveProperty("execution_trace");
  });

  it("traces synchronous failures and does not replace primitive throws", async () => {
    const error = new Error("sync");
    const handler = withExecutionTrace(terminal)(trace => {
      trace("init");
      throw error;
    });
    await expect(handler()).rejects.toBe(error);
    expect(error.execution_trace).toBe("init_thrw");
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
    const handler = withExecutionTrace(terminal)(async (trace, first) => {
      if (first) {
        trace("init");
        await blocked;
        trace("push");
      } else {
        trace("stag");
      }
      return { success: true };
    });
    const first = handler(true);
    expect((await handler(false)).execution_trace).toBe("stag_done");
    release();
    expect((await first).execution_trace).toBe("init_push_done");
    expect((await handler(false)).execution_trace).toBe("stag_done");
  });

  it("bounds paths and reserves the final outcome without recording invalid markers or inputs", async () => {
    const secret = "untrusted-input-do-not-record";
    const handler = withExecutionTrace(terminal)(async (trace, input) => {
      trace(input);
      trace("A");
      trace("ABCDE");
      trace("ab_c");
      trace("ab12");
      trace("init\n");
      trace("\n");
      trace(undefined);
      for (let i = 0; i < MAX_EXECUTION_TRACE_LENGTH * 2; i++) trace("push");
      return { success: true };
    });
    const result = await handler(secret);
    expect(result.execution_trace).toBe("push_".repeat(204) + "done");
    expect(result.execution_trace).toHaveLength(1024);
    expect(result.execution_trace).not.toContain(secret);
  });

  it.each([{ success: false }, new Error("failed")])("retains complete markers and the failure terminal at the length limit", async result => {
    const handler = withExecutionTrace(terminal)(trace => {
      for (let i = 0; i < MAX_EXECUTION_TRACE_LENGTH; i++) trace("push");
      if (result instanceof Error) throw result;
      return result;
    });
    if (result instanceof Error) {
      await expect(handler()).rejects.toBe(result);
    } else {
      expect(await handler()).toBe(result);
    }
    expect(result.execution_trace).toBe("push_".repeat(204) + (result instanceof Error ? "thrw" : "fail"));
  });

  it("emits the terminal without a leading underscore when no branch is recorded", async () => {
    const result = await withExecutionTrace(terminal)(() => ({ success: true }))();
    expect(result.execution_trace).toBe("done");
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
      )(async (trace, message, ids, transport) => {
        expect(message).toEqual({ untouched: true });
        expect(ids).toEqual({});
        expect(typeof transport.assertUnchanged).toBe("function");
        trace("init");
        throw new TransportIntegrityError("artifact");
      });
      const result = await handler({ untouched: true }, {});
      expect(result).toMatchObject({ success: false, error_type: "ERR_TRANSPORT_INTEGRITY", execution_trace: "init_fail" });
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
      trace("init");
      return { success: true };
    });
    await expect(handler()).rejects.toBe(error);
    expect(error.execution_trace).toBe("init_thrw");
  });

  it.each(["create_pull_request", "push_to_pull_request_branch"])("%s records a real handler invocation without Git or API effects", async type => {
    vi.stubGlobal("core", { info: vi.fn(), debug: vi.fn(), warning: vi.fn(), error: vi.fn() });
    vi.stubGlobal("github", { rest: {} });
    vi.stubGlobal("context", { repo: { owner: "org", repo: "repo" }, payload: {} });
    vi.stubGlobal("exec", { exec: vi.fn(), getExecOutput: vi.fn() });
    vi.stubEnv("GIT_CONFIG_COUNT", "0");
    vi.stubEnv("GIT_CONFIG_KEY_0", undefined);
    vi.stubEnv("GIT_CONFIG_VALUE_0", undefined);
    vi.stubEnv("GH_AW_SAFE_OUTPUTS_STAGED", "false");
    vi.stubEnv("GH_AW_WORKFLOW_ID", "trace-test");
    const { main } = require(`./${type}.cjs`);
    const handler = await main({ target_repo: "org/repo" });
    const result = await handler({ type, repo: "not-allowed/repo", temporary_id: "aw_trace" }, {}, new Map());
    expect(result).toMatchObject({ success: false, execution_trace: "init_repo_fail" });
    expect(result.error).toContain("allowed-repos");
    expect(global.exec.exec).not.toHaveBeenCalled();
    expect(global.exec.getExecOutput).not.toHaveBeenCalled();
  });
});
