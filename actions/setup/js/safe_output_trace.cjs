// @ts-check

const MAX_EXECUTION_TRACE_LENGTH = 1024;

/**
 * Bind the trace inside each invocation so nested helpers share only that
 * invocation's trace. Markers must be stable, unique source literals per handler.
 * The outer wrapper also observes failures converted by transport integrity.
 *
 * @param {{success: string, failure: string, error: string}} terminal
 * @param {(handler: (...args: any[]) => any) => (...args: any[]) => any} [wrap]
 * @returns {(handler: (...args: any[]) => any) => (...args: any[]) => Promise<any>}
 */
function withExecutionTrace(terminal, wrap = handler => handler) {
  return handler =>
    /** @this {any} */
    async function (...args) {
      let path = "";
      const trace = marker => {
        if (typeof marker === "string" && /^[A-Za-z]$/.test(marker) && path.length < MAX_EXECUTION_TRACE_LENGTH - 1) path += marker;
      };
      const attach = value => {
        if ((value !== null && typeof value === "object") || typeof value === "function") {
          try {
            Object.defineProperty(value, "execution_trace", { value: path, configurable: true, enumerable: true, writable: true });
          } catch {
            // Frozen errors/results must not turn observability into a new failure.
          }
        }
      };
      const finish = marker => {
        // Reserve one byte for the terminal outcome even when a route is bounded.
        if (typeof marker === "string" && /^[A-Za-z]$/.test(marker)) path += marker;
      };
      try {
        const result = await wrap((...handlerArgs) => handler.call(this, ...handlerArgs, trace)).apply(this, args);
        finish(result?.success === false || result?.skipped ? terminal.failure : terminal.success);
        attach(result);
        return result;
      } catch (error) {
        finish(terminal.error);
        attach(error);
        throw error;
      }
    };
}

module.exports = { withExecutionTrace, MAX_EXECUTION_TRACE_LENGTH };
