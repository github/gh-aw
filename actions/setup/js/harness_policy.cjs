// @ts-check

"use strict";

/**
 * Classified, completed invocations only; never includes prompts, output, or credentials.
 * @typedef {Readonly<{
 *   type: "attempt.completed",
 *   attempt: number,
 *   strategy: "restart" | "resume",
 *   exitCode: number,
 *   outcome: "success" | "failure" | "cancelled",
 *   failureClasses: readonly string[],
 * }>} HarnessPolicyEvent
 * @typedef {Readonly<{ type: "steer", message: string }>} HarnessPolicyAction
 * @typedef {Readonly<{ engine: string, capabilities: readonly HarnessPolicyAction["type"][] }>} HarnessPolicyContext
 * @typedef {Readonly<{
 *   id: string,
 *   onEvent: (event: HarnessPolicyEvent, context: HarnessPolicyContext) => readonly HarnessPolicyAction[] | void | Promise<readonly HarnessPolicyAction[] | void>,
 * }>} HarnessPolicy
 * @typedef {{
 *   engine: string,
 *   actions?: Partial<Record<HarnessPolicyAction["type"], (action: HarnessPolicyAction) => void | Promise<void>>>,
 * }} HarnessPolicyAdapter
 * @typedef {Readonly<{ dispatch: (event: HarnessPolicyEvent) => Promise<void> }>} HarnessPolicyDispatcher
 */

/**
 * Trusted, in-process policies propose actions; adapters own native engine calls.
 * No policies or capabilities are enabled implicitly.
 * @param {{ adapter: HarnessPolicyAdapter, policies?: readonly HarnessPolicy[] }} options
 * @returns {HarnessPolicyDispatcher}
 */
function createHarnessPolicyDispatcher({ adapter, policies = [] }) {
  if (typeof adapter.engine !== "string" || !adapter.engine.trim()) {
    throw new TypeError("Harness policy adapter requires an engine name");
  }
  const actions = Object.freeze({ ...adapter.actions });
  for (const [type, handler] of Object.entries(actions)) {
    if (type !== "steer" || typeof handler !== "function") {
      throw new TypeError("Harness policy adapter contains an invalid action handler");
    }
  }
  /** @type {HarnessPolicyAction["type"][]} */
  const capabilities = [];
  if (actions.steer) capabilities.push("steer");
  const context = Object.freeze({ engine: adapter.engine, capabilities: Object.freeze(capabilities) });
  const ids = new Set();
  const registeredPolicies = policies.map(({ id, onEvent }) => {
    if (typeof id !== "string" || !/^[a-z][a-z0-9_-]{0,63}$/.test(id) || ids.has(id) || typeof onEvent !== "function") {
      throw new TypeError("Harness policies require unique identifiers and an onEvent function");
    }
    ids.add(id);
    return Object.freeze({ id, onEvent });
  });

  return Object.freeze({
    async dispatch(event) {
      const snapshot = Object.freeze({ ...event, failureClasses: Object.freeze([...event.failureClasses]) });
      /** @type {HarnessPolicyAction[]} */
      const pendingActions = [];
      for (const policy of registeredPolicies) {
        const proposed = await policy.onEvent(snapshot, context);
        if (proposed === undefined) continue;
        if (!Array.isArray(proposed)) {
          throw new TypeError(`Harness policy ${policy.id} must return an action array or undefined`);
        }
        for (const action of proposed) {
          if (action?.type !== "steer" || typeof action.message !== "string" || !action.message.trim()) {
            throw new TypeError(`Harness policy ${policy.id} returned an invalid action`);
          }
          if (!actions.steer) {
            throw new Error(`Harness policy ${policy.id} requested unsupported capability: steer`);
          }
          pendingActions.push(Object.freeze({ type: "steer", message: action.message }));
        }
      }

      // Validate the entire event's proposals before causing native side effects.
      for (const action of pendingActions) {
        await actions.steer?.(action);
      }
    },
  });
}

module.exports = { createHarnessPolicyDispatcher };
