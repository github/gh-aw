// @ts-check

const { getErrorMessage } = require("./error_helpers.cjs");
const { redactSensitiveConfig } = require("./safe_outputs_config_redact.cjs");
const { ERR_SYSTEM } = require("./error_codes.cjs");

const fs = require("fs");
const path = require("path");

/**
 * Collect the names of any ${GH_AW_INPUT_*} placeholders that are unresolved
 * (i.e. the corresponding environment variable is absent from process.env).
 * Returns an empty array when all placeholders are resolved.
 * @param {string} rawJson - The raw JSON string before placeholder substitution
 * @returns {string[]}
 */
function collectUnresolvedInputPlaceholders(rawJson) {
  const unresolved = [];
  const pattern = /\$\{(GH_AW_INPUT_[A-Z0-9_]+)\}/g;
  let match;
  while ((match = pattern.exec(rawJson)) !== null) {
    const envName = match[1];
    if (process.env[envName] === undefined && !unresolved.includes(envName)) {
      unresolved.push(envName);
    }
  }
  return unresolved;
}

function resolveEnvPlaceholders(value) {
  if (Array.isArray(value)) {
    return value.map(resolveEnvPlaceholders);
  }
  if (value && typeof value === "object") {
    return Object.fromEntries(Object.entries(value).map(([key, nestedValue]) => [key, resolveEnvPlaceholders(nestedValue)]));
  }
  if (typeof value !== "string") {
    return value;
  }
  return value.replace(/\$\{([A-Z_][A-Z0-9_]*)\}/g, (match, envName) => process.env[envName] ?? match);
}

/**
 * @typedef {Object} LoadConfigResult
 * @property {Record<string, any>} config - The processed configuration
 * @property {string} outputFile - Path to the output file
 */

/**
 * Load and process safe outputs configuration
 * @param {Object} server - The MCP server instance for logging
 * @returns {LoadConfigResult} An object containing the processed config and output file path
 */
function loadConfig(server) {
  // Read configuration from file
  const configPath = process.env.GH_AW_SAFE_OUTPUTS_CONFIG_PATH || `${process.env.RUNNER_TEMP}/gh-aw/safeoutputs/config.json`;
  let safeOutputsConfigRaw;

  server.debug(`Reading config from file: ${configPath}`);

  try {
    if (fs.existsSync(configPath)) {
      server.debug(`Config file exists at: ${configPath}`);
      const configFileContent = fs.readFileSync(configPath, "utf8");
      server.debug(`Config file content length: ${configFileContent.length} characters`);
      // Don't log raw content to avoid exposing sensitive configuration data
      server.debug(`Config file read successfully, attempting to parse JSON`);
      // Warn about any GH_AW_INPUT_* placeholders that cannot be resolved before substitution.
      // This should not happen when the workflow is compiled correctly (the compiler ensures these
      // env vars are in the MCP gateway step env and the docker -e allowlist), but if it does the
      // error will surface later as a cryptic "No remote refs available for merge-base calculation"
      // rather than pointing at the root cause.
      const unresolvedInputs = collectUnresolvedInputPlaceholders(configFileContent);
      if (unresolvedInputs.length > 0) {
        const varList = unresolvedInputs.join(", ");
        const unresolvedMsg = `ERR_CONFIG: Unresolved workflow input placeholder(s) in safe-outputs config: ${varList}. The values were not passed to the MCP container. Verify that the workflow was compiled with a version that forwards GH_AW_INPUT_* to the container env.`;
        server.debugError(unresolvedMsg);
        console.error(`[safe_outputs_config] ERR_CONFIG: Unresolved workflow input placeholder(s): ${varList}`);
        throw new Error(unresolvedMsg);
      }
      safeOutputsConfigRaw = resolveEnvPlaceholders(JSON.parse(configFileContent));
      server.debug(`Successfully parsed config from file with ${Object.keys(safeOutputsConfigRaw).length} configuration keys`);
    } else {
      server.debug(`Config file does not exist at: ${configPath}`);
      server.debug(`Using minimal default configuration`);
      safeOutputsConfigRaw = {};
    }
  } catch (error) {
    server.debug(`Error reading config file: ${getErrorMessage(error)}`);
    server.debug(`Falling back to empty configuration`);
    safeOutputsConfigRaw = {};
  }

  const safeOutputsConfig = Object.fromEntries(Object.entries(safeOutputsConfigRaw).map(([k, v]) => [k.replace(/-/g, "_"), v]));
  server.debug(`Final processed config: ${JSON.stringify(redactSensitiveConfig(safeOutputsConfig))}`);

  // Handle GH_AW_SAFE_OUTPUTS with default fallback
  // Default is /opt (read-only mount for agent container)
  const outputFile = process.env.GH_AW_SAFE_OUTPUTS || `${process.env.RUNNER_TEMP}/gh-aw/safeoutputs/outputs.jsonl`;
  if (!process.env.GH_AW_SAFE_OUTPUTS) {
    server.debug(`GH_AW_SAFE_OUTPUTS not set, using default: ${outputFile}`);
  }
  // Always ensure the directory exists, regardless of whether env var is set
  const outputDir = path.dirname(outputFile);
  if (!fs.existsSync(outputDir)) {
    server.debug(`Creating output directory: ${outputDir}`);
    try {
      fs.mkdirSync(outputDir, { recursive: true });
    } catch (err) {
      throw new Error(`${ERR_SYSTEM}: Failed to create directory ${outputDir}: ${getErrorMessage(err)}`, { cause: err });
    }
  }

  return {
    config: safeOutputsConfig,
    outputFile: outputFile,
  };
}

module.exports = { loadConfig, resolveEnvPlaceholders };
