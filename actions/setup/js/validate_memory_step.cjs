// @ts-check

const { formatJSONFiles, runCustomMemoryValidation, writeValidationMarker, clearValidationMarker } = require("./memory_custom_validation.cjs");
const { filterIneligibleMemoryFiles } = require("./memory_file_eligibility.cjs");

/**
 * @param {{ error: (message: string) => void, info: (message: string) => void, warning: (message: string) => void, setFailed: (message: string) => void }} core
 * @param {{
 *   kind: "repo" | "cache" | "drive",
 *   formatJSON?: boolean,
 *   requireValidationScript?: boolean,
 *   requireJSONSchemas?: boolean,
 *   writeMarker?: boolean,
 * }} options
 */
function validateMemoryStep(core, options) {
  const memoryDir = process.env.MEMORY_DIR || "";
  const memoryId = process.env.MEMORY_ID || "default";
  const allowedExtensions = JSON.parse(process.env.ALLOWED_EXTENSIONS || "[]");
  let failed = false;

  if (options.writeMarker) {
    clearValidationMarker(options.kind, memoryId);
  }

  // Allowed-extensions (and file-glob, when present) are persistence filters, not hard
  // failures: ineligible files are logged and removed here so that custom validation,
  // artifact upload/save, and any downstream push all see the same effective file set.
  // This mirrors the filtering applied for repo-memory before this step runs.
  if (allowedExtensions.length > 0) {
    filterIneligibleMemoryFiles(memoryDir, allowedExtensions, process.env.FILE_GLOB_FILTER || "", core);
  }

  if (options.formatJSON) {
    for (const file of formatJSONFiles(memoryDir, 102400000)) {
      core.info(`Formatted JSON before custom validation: ${file}`);
    }
  }

  let jsonSchemas;
  const schemaConfigRequired = options.requireJSONSchemas || process.env.MEMORY_JSON_SCHEMAS_REQUIRED === "true";
  const scriptRequired = options.requireValidationScript || process.env.VALIDATION_SCRIPT_REQUIRED === "true";
  try {
    if (process.env.MEMORY_JSON_SCHEMAS_B64) {
      const encoded = process.env.MEMORY_JSON_SCHEMAS_B64;
      const decodedBytes = Buffer.from(encoded, "base64");
      if (decodedBytes.toString("base64") !== encoded) throw new TypeError("json-schemas configuration is not valid base64");
      const decoded = decodedBytes.toString("utf8");
      jsonSchemas = JSON.parse(decoded);
      if (!Array.isArray(jsonSchemas) || jsonSchemas.length === 0) {
        throw new TypeError("json-schemas must be a non-empty array");
      }
    } else if (schemaConfigRequired) {
      throw new TypeError("json-schemas configuration is missing");
    }
  } catch (error) {
    core.setFailed(`Memory validation configuration is invalid for '${memoryId}': ${error instanceof Error ? error.message : String(error)}`);
    failed = true;
  }

  if (scriptRequired && !process.env.VALIDATION_SCRIPT_B64) {
    core.setFailed(`Custom ${options.kind}-memory validation script is missing for '${memoryId}'.`);
    failed = true;
  }

  if (!failed && (scriptRequired || process.env.VALIDATION_SCRIPT_B64 || jsonSchemas)) {
    const result = runCustomMemoryValidation({
      scriptBase64: process.env.VALIDATION_SCRIPT_B64,
      jsonSchemas,
      requireJSONSchemas: schemaConfigRequired,
      memoryDir,
      memoryId,
      kind: options.kind,
      timeoutSeconds: Number(process.env.VALIDATION_TIMEOUT_SECONDS || "30"),
    });
    if (result.stdout) {
      core.info(`Custom ${options.kind}-memory validation stdout:\n${result.stdout}`);
    }
    if (result.stderr) {
      core.info(`${jsonSchemas ? "Memory" : "Custom"} ${options.kind}-memory validation stderr:\n${result.stderr}`);
    }
    if (!result.ok) {
      core.setFailed(`${jsonSchemas ? "Memory" : "Custom"} ${options.kind}-memory validation failed for '${memoryId}': ${result.timedOut ? "timed out" : `exited with code ${result.exitCode}`}.${result.stderr ? ` ${result.stderr}` : ""}`);
      failed = true;
    }
  }

  if (options.writeMarker && !failed) {
    writeValidationMarker(options.kind, memoryId);
  }

  return !failed;
}

module.exports = { validateMemoryStep };
