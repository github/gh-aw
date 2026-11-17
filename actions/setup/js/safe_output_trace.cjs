// @ts-check

const MAX_EXECUTION_TRACE_LENGTH = 1024;

/**
 * Bind the trace inside each invocation so nested helpers share only that
 * invocation's trace. Markers must be stable, unique four-letter source mnemonics per handler.
 * The outer wrapper also observes failures converted by transport integrity.
 * The callback receives trace first, followed by any wrapped handler arguments.
 * Trace metadata is read explicitly by outcome projection, not spread into payloads.
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
        if (typeof marker === "string" && /^[A-Za-z]{4}$/.test(marker) && path.length + (path ? 5 : 4) <= MAX_EXECUTION_TRACE_LENGTH - 5) path += `${path ? "_" : ""}${marker}`;
      };
      const attach = value => {
        if ((value !== null && typeof value === "object") || typeof value === "function") {
          try {
            Object.defineProperty(value, "execution_trace", { value: path, configurable: true, enumerable: false, writable: true });
          } catch {
            // Frozen errors/results must not turn observability into a new failure.
          }
        }
      };
      const finish = marker => {
        // Reserve a separator and complete terminal marker even when a route is bounded.
        if (typeof marker === "string" && /^[A-Za-z]{4}$/.test(marker)) path += `${path ? "_" : ""}${marker}`;
      };
      try {
        const result = await wrap((...handlerArgs) => handler.call(this, trace, ...handlerArgs)).apply(this, args);
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
