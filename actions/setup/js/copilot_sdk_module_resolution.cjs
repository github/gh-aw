// @ts-check
const { register, registerHooks } = require("node:module");
const { pathToFileURL } = require("node:url");

if (typeof registerHooks === "function") {
  registerHooks({ resolve: require("./copilot_sdk_module_resolution_hooks.cjs").resolveSync });
} else {
  register(pathToFileURL(require.resolve("./copilot_sdk_module_resolution_hooks.cjs")));
}
