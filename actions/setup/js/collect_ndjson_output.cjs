// @ts-check
/// <reference types="@actions/github-script" />

const { getErrorMessage } = require("./error_helpers.cjs");
const { repairJson, sanitizePrototypePollution } = require("./json_repair_helpers.cjs");
const { AGENT_OUTPUT_FILENAME, TMP_GH_AW_PATH } = require("./constants.cjs");

async function main() {
  try {
    const fs = require("fs");
    const { sanitizeContent } = require("./sanitize_content.cjs");
    const { validateItem, getMaxAllowedForType, getMinRequiredForType, hasValidationConfig, MAX_BODY_LENGTH: maxBodyLength, resetValidationConfigCache } = require("./safe_output_type_validator.cjs");
    const { resolveAllowedMentionsFromPayload } = require("./resolve_mentions_from_payload.cjs");

    // Load validation config from file and set it in environment for the validator to read
    const validationConfigPath = process.env.GH_AW_VALIDATION_CONFIG_PATH || "/opt/gh-aw/safeoutputs/validation.json";
    let validationConfig = null;
    try {
      if (fs.existsSync(validationConfigPath)) {
        const validationConfigContent = fs.readFileSync(validationConfigPath, "utf8");
        process.env.GH_AW_VALIDATION_CONFIG = validationConfigContent;
        validationConfig = JSON.parse(validationConfigContent);
        resetValidationConfigCache(); // Reset cache so it reloads from new env var
        core.info(`Loaded validation config from ${validationConfigPath}`);
      }
    } catch (error) {
      core.warning(`Failed to read validation config from ${validationConfigPath}: ${getErrorMessage(error)}`);
    }

    // Extract mentions configuration from validation config
    const mentionsConfig = validationConfig?.mentions || null;

    // Resolve allowed mentions for the output collector
    // This determines which @mentions are allowed in the agent output
    const allowedMentions = await resolveAllowedMentionsFromPayload(context, github, core, mentionsConfig);

    function validateFieldWithInputSchema(value, fieldName, inputSchema, lineNum) {
      if (inputSchema.required && (value === undefined || value === null)) {
        return {
          isValid: false,
          error: `Line ${lineNum}: ${fieldName} is required`,
        };
      }
      if (value === undefined || value === null) {
        return {
          isValid: true,
          normalizedValue: inputSchema.default || undefined,
        };
      }
      const inputType = inputSchema.type || "string";
      let normalizedValue = value;
      switch (inputType) {
        case "string":
          if (typeof value !== "string") {
            return {
              isValid: false,
              error: `Line ${lineNum}: ${fieldName} must be a string`,
            };
          }
          normalizedValue = sanitizeContent(value, { allowedAliases: allowedMentions });
          break;
        case "boolean":
          if (typeof value !== "boolean") {
            return {
              isValid: false,
              error: `Line ${lineNum}: ${fieldName} must be a boolean`,
            };
          }
          break;
        case "number":
          if (typeof value !== "number") {
            return {
              isValid: false,
              error: `Line ${lineNum}: ${fieldName} must be a number`,
            };
          }
          break;
        case "choice":
          if (typeof value !== "string") {
            return {
              isValid: false,
              error: `Line ${lineNum}: ${fieldName} must be a string for choice type`,
            };
          }
          if (inputSchema.options && !inputSchema.options.includes(value)) {
            return {
              isValid: false,
              error: `Line ${lineNum}: ${fieldName} must be one of: ${inputSchema.options.join(", ")}`,
            };
          }
          normalizedValue = sanitizeContent(value, { allowedAliases: allowedMentions });
          break;
        default:
          if (typeof value === "string") {
            normalizedValue = sanitizeContent(value, { allowedAliases: allowedMentions });
          }
          break;
      }
      return {
        isValid: true,
        normalizedValue,
      };
    }
    function validateItemWithSafeJobConfig(item, jobConfig, lineNum) {
      const errors = [];
      const normalizedItem = { ...item };
      if (!jobConfig.inputs) {
        return {
          isValid: true,
          errors: [],
          normalizedItem: item,
        };
      }
      for (const [fieldName, inputSchema] of Object.entries(jobConfig.inputs)) {
        const fieldValue = item[fieldName];
        const validation = validateFieldWithInputSchema(fieldValue, fieldName, inputSchema, lineNum);
        if (!validation.isValid && validation.error) {
          errors.push(validation.error);
        } else if (validation.normalizedValue !== undefined) {
          normalizedItem[fieldName] = validation.normalizedValue;
        }
      }
      return {
        isValid: errors.length === 0,
        errors,
        normalizedItem,
      };
    }
    function parseJsonWithRepair(jsonStr) {
      try {
        const parsed = JSON.parse(jsonStr);
        // Sanitize the parsed object to prevent prototype pollution
        return sanitizePrototypePollution(parsed);
      } catch (originalError) {
        try {
          const repairedJson = repairJson(jsonStr);
          const parsed = JSON.parse(repairedJson);
          // Sanitize the parsed object to prevent prototype pollution
          return sanitizePrototypePollution(parsed);
        } catch (repairError) {
          core.info(`invalid input json: ${jsonStr}`);
          const originalMsg = originalError instanceof Error ? originalError.message : String(originalError);
          const repairMsg = repairError instanceof Error ? repairError.message : String(repairError);
          throw new Error(`JSON parsing failed. Original: ${originalMsg}. After attempted repair: ${repairMsg}`);
        }
      }
    }
    const outputFile = process.env.GH_AW_SAFE_OUTPUTS;
    // Read config from file instead of environment variable
    const configPath = process.env.GH_AW_SAFE_OUTPUTS_CONFIG_PATH || "/opt/gh-aw/safeoutputs/config.json";
    let safeOutputsConfig;
    core.info(`[INGESTION] Reading config from: ${configPath}`);
    try {
      if (fs.existsSync(configPath)) {
        const configFileContent = fs.readFileSync(configPath, "utf8");
        core.info(`[INGESTION] Raw config content: ${configFileContent}`);
        safeOutputsConfig = JSON.parse(configFileContent);
        core.info(`[INGESTION] Parsed config keys: ${JSON.stringify(Object.keys(safeOutputsConfig))}`);
      } else {
        core.info(`[INGESTION] Config file does not exist at: ${configPath}`);
      }
    } catch (error) {
      core.warning(`Failed to read config file from ${configPath}: ${getErrorMessage(error)}`);
    }

    core.info(`[INGESTION] Output file path: ${outputFile}`);
    if (!outputFile) {
      core.info("GH_AW_SAFE_OUTPUTS not set, no output to collect");
      core.setOutput("output", "");
      core.setOutput("output_types", "");
      core.setOutput("has_patch", "false");
      return;
    }
    if (!fs.existsSync(outputFile)) {
      core.info(`Output file does not exist: ${outputFile}`);
      core.setOutput("output", "");
      core.setOutput("output_types", "");
      core.setOutput("has_patch", "false");
      return;
    }
    const outputContent = fs.readFileSync(outputFile, "utf8");
    if (outputContent.trim() === "") {
      core.info("Output file is empty");
    }
    core.info(`Raw output content length: ${outputContent.length}`);
    core.info(`[INGESTION] First 500 chars of output: ${outputContent.substring(0, 500)}`);
    let expectedOutputTypes = {};
    if (safeOutputsConfig) {
      try {
        // safeOutputsConfig is already a parsed object from the file
        // Normalize all config keys to use underscores instead of dashes
        core.info(`[INGESTION] Normalizing config keys (dash -> underscore)`);
        expectedOutputTypes = Object.fromEntries(Object.entries(safeOutputsConfig).map(([key, value]) => [key.replace(/-/g, "_"), value]));
        core.info(`[INGESTION] Expected output types after normalization: ${JSON.stringify(Object.keys(expectedOutputTypes))}`);
        core.info(`[INGESTION] Expected output types full config: ${JSON.stringify(expectedOutputTypes)}`);
      } catch (error) {
        const errorMsg = getErrorMessage(error);
        core.info(`Warning: Could not parse safe-outputs config: ${errorMsg}`);
      }
    }
    // Parse JSONL (JSON Lines) format: each line is a separate JSON object
    // CRITICAL: This expects one JSON object per line. If JSON is formatted with
    // indentation/pretty-printing, parsing will fail.
    const lines = outputContent.trim().split("\n");
    const parsedItems = [];
    const errors = [];
    for (let i = 0; i < lines.length; i++) {
      const line = lines[i].trim();
      if (line === "") continue;
      core.info(`[INGESTION] Processing line ${i + 1}: ${line.substring(0, 200)}...`);
      try {
        const item = parseJsonWithRepair(line);
        if (item === undefined) {
          errors.push(`Line ${i + 1}: Invalid JSON - JSON parsing failed`);
          continue;
        }
        if (!item.type) {
          errors.push(`Line ${i + 1}: Missing required 'type' field`);
          continue;
        }
        // Normalize type to use underscores (convert any dashes to underscores for resilience)
        const originalType = item.type;
        const itemType = item.type.replace(/-/g, "_");
        core.info(`[INGESTION] Line ${i + 1}: Original type='${originalType}', Normalized type='${itemType}'`);
        // Update item.type to normalized value
        item.type = itemType;
        if (!expectedOutputTypes[itemType]) {
          core.warning(`[INGESTION] Line ${i + 1}: Type '${itemType}' not found in expected types: ${JSON.stringify(Object.keys(expectedOutputTypes))}`);
          errors.push(`Line ${i + 1}: Unexpected output type '${itemType}'. Expected one of: ${Object.keys(expectedOutputTypes).join(", ")}`);
          continue;
        }
        const typeCount = parsedItems.filter(existing => existing.type === itemType).length;
        const maxAllowed = getMaxAllowedForType(itemType, expectedOutputTypes);
        if (typeCount >= maxAllowed) {
          errors.push(`Line ${i + 1}: Too many items of type '${itemType}'. Maximum allowed: ${maxAllowed}.`);
          continue;
        }
        core.info(`Line ${i + 1}: type '${itemType}'`);

        // Use the validation engine to validate the item
        if (hasValidationConfig(itemType)) {
          const validationResult = validateItem(item, itemType, i + 1, { allowedAliases: allowedMentions });
          if (!validationResult.isValid) {
            if (validationResult.error) {
              errors.push(validationResult.error);
            }
            continue;
          }
          // Update item with normalized values
          Object.assign(item, validationResult.normalizedItem);
        } else {
          // Fall back to validateItemWithSafeJobConfig for unknown types
          const jobOutputType = expectedOutputTypes[itemType];
          if (!jobOutputType) {
            errors.push(`Line ${i + 1}: Unknown output type '${itemType}'`);
            continue;
          }
          const safeJobConfig = jobOutputType;
          if (safeJobConfig && safeJobConfig.inputs) {
            const validation = validateItemWithSafeJobConfig(item, safeJobConfig, i + 1);
            if (!validation.isValid) {
              errors.push(...validation.errors);
              continue;
            }
            Object.assign(item, validation.normalizedItem);
          }
        }

        core.info(`Line ${i + 1}: Valid ${itemType} item`);
        parsedItems.push(item);
      } catch (error) {
        const errorMsg = getErrorMessage(error);
        errors.push(`Line ${i + 1}: Invalid JSON - ${errorMsg}`);
      }
    }
    if (errors.length > 0) {
      core.warning("Validation errors found:");
      errors.forEach(error => core.warning(`  - ${error}`));
    }
    for (const itemType of Object.keys(expectedOutputTypes)) {
      const minRequired = getMinRequiredForType(itemType, expectedOutputTypes);
      if (minRequired > 0) {
        const actualCount = parsedItems.filter(item => item.type === itemType).length;
        if (actualCount < minRequired) {
          errors.push(`Too few items of type '${itemType}'. Minimum required: ${minRequired}, found: ${actualCount}.`);
        }
      }
    }
    core.info(`Successfully parsed ${parsedItems.length} valid output items`);
    const validatedOutput = {
      items: parsedItems,
      errors: errors,
    };
    const path = require("path");
    const agentOutputFile = path.join(TMP_GH_AW_PATH, AGENT_OUTPUT_FILENAME);
    const validatedOutputJson = JSON.stringify(validatedOutput);
    try {
      fs.mkdirSync(TMP_GH_AW_PATH, { recursive: true });
      fs.writeFileSync(agentOutputFile, validatedOutputJson, "utf8");
      core.info(`Stored validated output to: ${agentOutputFile}`);
      core.exportVariable("GH_AW_AGENT_OUTPUT", agentOutputFile);
    } catch (error) {
      const errorMsg = getErrorMessage(error);
      core.error(`Failed to write agent output file: ${errorMsg}`);
    }
    core.setOutput("output", JSON.stringify(validatedOutput));
    core.setOutput("raw_output", outputContent);
    const outputTypes = Array.from(new Set(parsedItems.map(item => item.type)));
    core.info(`output_types: ${outputTypes.join(", ")}`);
    core.setOutput("output_types", outputTypes.join(","));

    // Check if patch file exists for detection job conditional
    const patchPath = "/tmp/gh-aw/aw.patch";
    const hasPatch = fs.existsSync(patchPath);
    core.info(`Patch file ${hasPatch ? "exists" : "does not exist"} at: ${patchPath}`);

    // Check if allow-empty is enabled for create_pull_request (reuse already loaded config)
    let allowEmptyPR = false;
    if (safeOutputsConfig) {
      // Check if create-pull-request has allow-empty enabled
      if (safeOutputsConfig["create-pull-request"]?.["allow-empty"] === true || safeOutputsConfig["create_pull_request"]?.["allow_empty"] === true) {
        allowEmptyPR = true;
        core.info(`allow-empty is enabled for create-pull-request`);
      }
    }

    // If allow-empty is enabled for create_pull_request and there's no patch, that's OK
    // Set has_patch to true so the create_pull_request job will run
    if (allowEmptyPR && !hasPatch && outputTypes.includes("create_pull_request")) {
      core.info(`allow-empty is enabled and no patch exists - will create empty PR`);
      core.setOutput("has_patch", "true");
    } else {
      core.setOutput("has_patch", hasPatch ? "true" : "false");
    }
  } catch (error) {
    const errorMsg = getErrorMessage(error);
    core.error(`Failed to ingest agent output: ${errorMsg}`);
    if (error instanceof Error && error.stack) {
      core.error(`Stack trace: ${error.stack}`);
    }
    // Set outputs to empty/false even on error to ensure they are always defined
    core.setOutput("output", "");
    core.setOutput("output_types", "");
    core.setOutput("has_patch", "false");
    core.setFailed(`Agent output ingestion failed: ${errorMsg}`);
    throw error;
  }
}

module.exports = { main };
