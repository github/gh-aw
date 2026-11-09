// @ts-check

/**
 * A wrapper's zero status cannot establish success against an attributed failure.
 * Preserve an observed native failure code; a timeout alone has no known code.
 * @param {import("./types/agent_session").AgentExecutionEvent | undefined} execution
 * @param {import("./types/agent_session").SessionEvent[]} events
 * @returns {import("./types/agent_session").AgentExecutionEvent | undefined}
 */
function reconcileDeepSeekExecution(execution, events) {
  const errors = events.filter(event => event.type === "session.error");
  if (!execution || execution.data.exitCode !== 0 || !errors.length) return execution;
  const reconciled = structuredClone(execution);
  const nativeExit = errors.map(event => event.data.exitCode).find(value => typeof value === "number" && Number.isSafeInteger(value) && value > 0 && value <= 255);
  if (typeof nativeExit === "number") reconciled.data.exitCode = nativeExit;
  else delete reconciled.data.exitCode;
  return reconciled;
}

module.exports = { reconcileDeepSeekExecution };
