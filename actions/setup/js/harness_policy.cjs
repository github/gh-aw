// @ts-check

"use strict";

/**
 * Classified lifecycle observations; never includes prompts, output, or credentials.
 * @typedef {Readonly<{
 *   type: "attempt.completed",
 *   attempt: number,
 *   strategy: "restart" | "resume",
 *   exitCode: number,
 *   outcome: "success" | "failure" | "cancelled",
 *   failureClasses: readonly string[],
 * }>} HarnessAttemptCompletedEvent
 * @typedef {Readonly<{
 *   type: "turn.completed",
 *   ongoing: boolean,
 *   remainingMs: number,
 *   tools: readonly Readonly<{ toolName: string, success?: boolean }>[],
 * }>} HarnessTurnCompletedEvent
 * @typedef {Readonly<{
 *   type: "retry.ready",
 *   failureClasses: readonly string[],
 *   remainingMs: number,
 * }>} HarnessRetryReadyEvent
 * @typedef {HarnessAttemptCompletedEvent | HarnessTurnCompletedEvent | HarnessRetryReadyEvent} HarnessPolicyEvent
 * @typedef {Readonly<{ type: "steer" | "retry_guidance", message: string }>} HarnessPolicyAction
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
    if (!["steer", "retry_guidance"].includes(type) || typeof handler !== "function") {
      throw new TypeError("Harness policy adapter contains an invalid action handler");
    }
  }
  /** @type {HarnessPolicyAction["type"][]} */
  const capabilities = [];
  if (actions.steer) capabilities.push("steer");
  if (actions.retry_guidance) capabilities.push("retry_guidance");
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
      const snapshot =
        event.type !== "turn.completed"
          ? Object.freeze({ ...event, failureClasses: Object.freeze([...event.failureClasses]) })
          : Object.freeze({ ...event, tools: Object.freeze(event.tools.map(tool => Object.freeze({ toolName: tool.toolName, success: tool.success }))) });
      /** @type {HarnessPolicyAction[]} */
      const pendingActions = [];
      for (const policy of registeredPolicies) {
        const proposed = await policy.onEvent(snapshot, context);
        if (proposed === undefined) continue;
        if (!Array.isArray(proposed)) {
          throw new TypeError(`Harness policy ${policy.id} must return an action array or undefined`);
        }
        for (const action of proposed) {
          if (!["steer", "retry_guidance"].includes(action?.type) || typeof action.message !== "string" || !action.message.trim()) {
            throw new TypeError(`Harness policy ${policy.id} returned an invalid action`);
          }
          if (!actions[action.type]) {
            throw new Error(`Harness policy ${policy.id} requested unsupported capability: ${action.type}`);
          }
          pendingActions.push(Object.freeze({ type: action.type, message: action.message }));
        }
      }

      // Validate the entire event's proposals before causing native side effects.
      for (const action of pendingActions) {
        await actions[action.type]?.(action);
      }
    },
  });
}

module.exports = { createHarnessPolicyDispatcher };
