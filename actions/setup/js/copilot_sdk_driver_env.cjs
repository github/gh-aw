// @ts-check

"use strict";

const fs = require("fs");
const path = require("path");

/**
 * @param {NodeJS.ProcessEnv} baseEnv
 * @param {(message: string) => void} [logger]
 * @returns {NodeJS.ProcessEnv}
 */
function buildCopilotSDKDriverEnv(baseEnv, logger = message => process.stderr.write(`[copilot-harness] ${message}\n`)) {
  const sdkDir = path.join(baseEnv.GITHUB_WORKSPACE || process.cwd(), ".gh-aw", "copilot-sdk");
  const resolver = path.join(sdkDir, "resolve.cjs");
  if (!fs.existsSync(resolver)) return baseEnv;
  if (baseEnv.GH_AW_TRUSTED_SDK_RESOLVER !== "1") {
    logger("Skipping workspace Copilot SDK resolver preload because GH_AW_TRUSTED_SDK_RESOLVER is not enabled");
    return baseEnv;
  }
  const nodeModules = path.join(sdkDir, "node_modules");
  return {
    ...baseEnv,
    NODE_OPTIONS: `${baseEnv.NODE_OPTIONS ? `${baseEnv.NODE_OPTIONS} ` : ""}--import=${JSON.stringify(resolver)}`,
    NODE_PATH: `${nodeModules}${baseEnv.NODE_PATH ? `${path.delimiter}${baseEnv.NODE_PATH}` : ""}`,
    PATH: `${path.join(nodeModules, ".bin")}${path.delimiter}${baseEnv.PATH || ""}`,
  };
}

module.exports = { buildCopilotSDKDriverEnv };
