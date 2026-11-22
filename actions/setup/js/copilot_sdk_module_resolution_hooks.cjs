// @ts-check
const path = require("node:path");
const { pathToFileURL } = require("node:url");

// ESM imports do not consult NODE_PATH. Use Node's package resolver so SDK
// export conditions and subpaths still apply to the isolated installation.
const runnerTemp = process.env.RUNNER_TEMP;
if (!runnerTemp) throw new Error("RUNNER_TEMP must be set for Copilot SDK module resolution");
const parentURL = pathToFileURL(path.join(path.resolve(runnerTemp), "gh-aw", "engine-cli", "copilot-sdk", "package.json")).href;

/** @param {string} specifier @param {unknown} error */
function shouldFallback(specifier, error) {
  const sdkPackage = specifier === "@microsoft/copilot-sdk" || specifier.startsWith("@microsoft/copilot-sdk/") || specifier === "undici" || specifier.startsWith("undici/");
  return sdkPackage && error !== null && typeof error === "object" && "code" in error && (error.code === "ERR_MODULE_NOT_FOUND" || error.code === "MODULE_NOT_FOUND");
}

/**
 * @overload
 * @param {string} specifier
 * @param {import("node:module").ResolveHookContext} context
 * @param {Parameters<import("node:module").ResolveHookSync>[2]} nextResolve
 * @param {false} awaitResult
 * @returns {import("node:module").ResolveFnOutput}
 */
/**
 * @overload
 * @param {string} specifier
 * @param {import("node:module").ResolveHookContext} context
 * @param {Parameters<import("node:module").ResolveHook>[2]} nextResolve
 * @param {true} awaitResult
 * @returns {import("node:module").ResolveFnOutput | Promise<import("node:module").ResolveFnOutput>}
 */
/**
 * @param {string} specifier
 * @param {import("node:module").ResolveHookContext} context
 * @param {Parameters<import("node:module").ResolveHookSync>[2] | Parameters<import("node:module").ResolveHook>[2]} nextResolve
 * @param {boolean} awaitResult
 * @returns {import("node:module").ResolveFnOutput | Promise<import("node:module").ResolveFnOutput>}
 */
function resolveWithFallback(specifier, context, nextResolve, awaitResult) {
  const fallback = error => {
    if (!shouldFallback(specifier, error)) throw error;
    return nextResolve(specifier, { ...context, parentURL });
  };
  try {
    const result = nextResolve(specifier, context);
    return awaitResult ? Promise.resolve(result).catch(fallback) : result;
  } catch (error) {
    return fallback(error);
  }
}

/** @type {import("node:module").ResolveHookSync} */
exports.resolveSync = (specifier, context, nextResolve) => resolveWithFallback(specifier, context, nextResolve, false);

/** @type {import("node:module").ResolveHook} */
exports.resolve = async (specifier, context, nextResolve) => resolveWithFallback(specifier, context, nextResolve, true);
