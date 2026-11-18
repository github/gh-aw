// @ts-check

import { describe, expect, it, vi } from "vitest";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { createHarnessPolicyDispatcher } = require("./harness_policy.cjs");

function completedEvent(overrides = {}) {
  return { type: "attempt.completed", attempt: 1, strategy: "restart", exitCode: 1, outcome: "failure", failureClasses: ["rate_limit_error"], ...overrides };
}

describe("harness policy dispatcher", () => {
  it("does not enable policies or actions implicitly", async () => {
    const steer = vi.fn();
    const dispatcher = createHarnessPolicyDispatcher({ adapter: { engine: "pi", actions: { steer } } });
    await dispatcher.dispatch(completedEvent());
    expect(steer).not.toHaveBeenCalled();
  });

  it.each(["pi", "copilot", "codex", "claude"])("runs the same policy with a %s adapter", async engine => {
    const steer = vi.fn();
    const onEvent = vi.fn((event, context) => {
      expect(context).toEqual({ engine, capabilities: ["steer"] });
      return event.outcome === "failure" ? [{ type: "steer", message: "Finish the current task." }] : [];
    });
    const dispatcher = createHarnessPolicyDispatcher({ adapter: { engine, actions: { steer } }, policies: [{ id: "example", onEvent }] });
    await dispatcher.dispatch(completedEvent());
    expect(steer).toHaveBeenCalledExactlyOnceWith({ type: "steer", message: "Finish the current task." });
  });

  it("evaluates and applies composed policies in registration order", async () => {
    const order = [];
    const dispatcher = createHarnessPolicyDispatcher({
      adapter: {
        engine: "pi",
        actions: {
          steer: async action => {
            await Promise.resolve();
            order.push(action.message);
          },
        },
      },
      policies: ["first", "second"].map(id => ({
        id,
        onEvent: async () => {
          await Promise.resolve();
          order.push(`evaluate:${id}`);
          return [{ type: "steer", message: id }];
        },
      })),
    });
    await dispatcher.dispatch(completedEvent());
    expect(order).toEqual(["evaluate:first", "evaluate:second", "first", "second"]);
  });

  it("lets a policy abstain when its required capability is unavailable", async () => {
    const onEvent = vi.fn((_event, context) => {
      expect(context.capabilities).toEqual([]);
    });
    await createHarnessPolicyDispatcher({ adapter: { engine: "pi" }, policies: [{ id: "observer", onEvent }] }).dispatch(completedEvent());
    expect(onEvent).toHaveBeenCalledOnce();
  });

  it("rejects unsupported actions rather than silently dropping them", async () => {
    const dispatcher = createHarnessPolicyDispatcher({
      adapter: { engine: "pi" },
      policies: [{ id: "example", onEvent: () => [{ type: "steer", message: "Finish." }] }],
    });
    await expect(dispatcher.dispatch(completedEvent())).rejects.toThrow("requested unsupported capability: steer");
  });

  it.each([null, {}, [{ type: "permission", message: "Grant access." }], [{ type: "steer", message: "" }], [{ type: "steer", message: "   " }], [{ type: "steer", message: 1 }]])(
    "rejects invalid proposals without applying earlier valid actions: %j",
    async proposed => {
      const steer = vi.fn();
      const dispatcher = createHarnessPolicyDispatcher({
        adapter: { engine: "pi", actions: { steer } },
        policies: [
          { id: "valid", onEvent: () => [{ type: "steer", message: "Finish." }] },
          { id: "invalid", onEvent: () => proposed },
        ],
      });
      await expect(dispatcher.dispatch(completedEvent())).rejects.toThrow(/must return an action array|returned an invalid action/);
      expect(steer).not.toHaveBeenCalled();
    }
  );

  it("shares immutable event and capability snapshots without freezing caller data", async () => {
    const event = completedEvent();
    const steer = vi.fn();
    const adapter = { engine: "pi", actions: { steer } };
    const policies = [
      {
        id: "observer",
        onEvent: (snapshot, context) => {
          expect(context).toEqual({ engine: "pi", capabilities: ["steer"] });
          expect(() => snapshot.failureClasses.push("changed")).toThrow(TypeError);
          expect(() => (snapshot.exitCode = 0)).toThrow(TypeError);
          expect(() => context.capabilities.push("permission")).toThrow(TypeError);
          expect(() => (context.engine = "changed")).toThrow(TypeError);
          return [{ type: "steer", message: "Finish." }];
        },
      },
    ];
    const dispatcher = createHarnessPolicyDispatcher({ adapter, policies });
    adapter.actions.steer = vi.fn();
    adapter.engine = "changed";
    policies[0].onEvent = vi.fn();
    await dispatcher.dispatch(event);
    expect(steer).toHaveBeenCalledExactlyOnceWith({ type: "steer", message: "Finish." });
    expect(adapter.actions.steer).not.toHaveBeenCalled();
    expect(policies[0].onEvent).not.toHaveBeenCalled();
    expect(event.failureClasses).toEqual(["rate_limit_error"]);
    expect(Object.isFrozen(event)).toBe(false);
    expect(Object.isFrozen(event.failureClasses)).toBe(false);
  });

  it.each(["policy", "adapter"])("propagates %s failures and stops dispatching", async source => {
    const error = new Error("native failure");
    const steer = vi.fn(() => {
      if (source === "adapter") throw error;
    });
    const dispatcher = createHarnessPolicyDispatcher({
      adapter: { engine: "pi", actions: { steer } },
      policies: [
        {
          id: "example",
          onEvent: () => {
            if (source === "policy") throw error;
            return [
              { type: "steer", message: "First." },
              { type: "steer", message: "Second." },
            ];
          },
        },
      ],
    });
    await expect(dispatcher.dispatch(completedEvent())).rejects.toBe(error);
    expect(steer).toHaveBeenCalledTimes(source === "adapter" ? 1 : 0);
  });

  it.each([
    { adapter: { engine: "" } },
    { adapter: { engine: "pi", actions: { steer: true } } },
    { adapter: { engine: "pi", actions: { permission: () => {} } } },
    {
      adapter: { engine: "pi" },
      policies: [
        { id: "same", onEvent: () => {} },
        { id: "same", onEvent: () => {} },
      ],
    },
    { adapter: { engine: "pi" }, policies: [{ id: "invalid id", onEvent: () => {} }] },
  ])("rejects invalid registration", options => {
    expect(() => createHarnessPolicyDispatcher(options)).toThrow(TypeError);
  });
});
