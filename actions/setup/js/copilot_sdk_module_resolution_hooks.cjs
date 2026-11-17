// @ts-check
const path = require("node:path");
const { pathToFileURL } = require("node:url");

// ESM imports do not consult NODE_PATH. Use Node's package resolver so SDK
// export conditions and subpaths still apply to the isolated installation.
const parentURL = pathToFileURL(path.join(process.env.RUNNER_TEMP || "", "gh-aw", "engine-cli", "copilot-sdk", "package.json")).href;

/** @param {string} specifier @param {unknown} error */
function shouldFallback(specifier, error) {
  const sdkPackage = specifier === "@github/copilot-sdk" || specifier.startsWith("@github/copilot-sdk/") || specifier === "undici" || specifier.startsWith("undici/");
  return sdkPackage && error !== null && typeof error === "object" && "code" in error && (error.code === "ERR_MODULE_NOT_FOUND" || error.code === "MODULE_NOT_FOUND");
}

/** @type {import("node:module").ResolveHookSync} */
exports.resolveSync = (specifier, context, nextResolve) => {
  try {
    return nextResolve(specifier, context);
  } catch (error) {
    if (!shouldFallback(specifier, error)) throw error;
    return nextResolve(specifier, { ...context, parentURL });
  }
};

/** @type {import("node:module").ResolveHook} */
exports.resolve = async (specifier, context, nextResolve) => {
  try {
    return await nextResolve(specifier, context);
  } catch (error) {
    if (!shouldFallback(specifier, error)) throw error;
    return nextResolve(specifier, { ...context, parentURL });
  }
};
