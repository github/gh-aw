// @ts-check
/// <reference types="@actions/github-script" />

const { getErrorMessage } = require("./error_helpers.cjs");
const { repairJson, sanitizePrototypePollution } = require("./json_repair_helpers.cjs");
const { AGENT_OUTPUT_FILENAME, TMP_GH_AW_PATH } = require("./constants.cjs");
const { ERR_API, ERR_PARSE } = require("./error_codes.cjs");
const { isPayloadUserBot } = require("./resolve_mentions.cjs");
const { parseIntTemplatable } = require("./templatable.cjs");
const { parseAllowedRepos, validateTargetRepo } = require("./repo_helpers.cjs");

async function main() {
  try {
    const fs = require("fs");
    const { sanitizeContent } = require("./sanitize_content.cjs");
    const { validateItem, getMaxAllowedForType, getMinRequiredForType, hasValidationConfig, MAX_BODY_LENGTH: maxBodyLength, resetValidationConfigCache } = require("./safe_output_type_validator.cjs");
    const { resolveAllowedMentionsFromPayload } = require("./resolve_mentions_from_payload.cjs");

    // Load validation config from file and set it in environment for the validator to read
    const validationConfigPath = process.env.GH_AW_VALIDATION_CONFIG_PATH || `${process.env.RUNNER_TEMP}/gh-aw/safeoutputs/validation.json`;
    /** @type {any} */
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
    const maxMentions = parseIntTemplatable(mentionsConfig?.max, 50);

    // Resolve allowed mentions for the output collector
    // This determines which @mentions are allowed in the agent output
    const allowedMentions = await resolveAllowedMentionsFromPayload(context, github, core, mentionsConfig);

    // maxBotMentions is populated after safeOutputsConfig is read below
    /** @type {number | undefined} */
    let maxBotMentions;

    function validateFieldWithInputSchema(value, fieldName, inputSchema, lineNum, allowedAliasesSeen) {
      if (inputSchema.required && (value === undefined || value === null)) {
        return {
          isValid: false,
          error: `Line ${lineNum}: ${fieldName} is required`,
        };
      }
      if (value === undefined || value === null) {
        return {
          isValid: true,
          normalizedValue: inputSchema.default ?? undefined,
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
          normalizedValue = sanitizeContent(value, { allowedAliases: allowedMentions, maxMentions, maxBotMentions, allowedAliasesSeen });
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
          normalizedValue = sanitizeContent(value, { allowedAliases: allowedMentions, maxMentions, maxBotMentions, allowedAliasesSeen });
          break;
        default:
          if (typeof value === "string") {
            normalizedValue = sanitizeContent(value, { allowedAliases: allowedMentions, maxMentions, maxBotMentions, allowedAliasesSeen });
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
      const normalizedItem = { type: item.type };
      if (!jobConfig || typeof jobConfig !== "object" || !jobConfig.inputs) {
        return {
          isValid: true,
          errors: [],
          normalizedItem,
        };
      }
      const allowedAliasesSeen = new Set();
      for (const [fieldName, inputSchema] of Object.entries(jobConfig.inputs)) {
        const fieldValue = item[fieldName];
        const validation = validateFieldWithInputSchema(fieldValue, fieldName, inputSchema, lineNum, allowedAliasesSeen);
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
          const originalMsg = getErrorMessage(originalError);
          const repairMsg = getErrorMessage(repairError);
          throw new Error(`${ERR_PARSE}: JSON parsing failed. Original: ${originalMsg}. After attempted repair: ${repairMsg}`);
        }
      }
    }
    const outputFile = process.env.GH_AW_SAFE_OUTPUTS;
    // Read config from file instead of environment variable
    const configPath = process.env.GH_AW_SAFE_OUTPUTS_CONFIG_PATH || `${process.env.RUNNER_TEMP}/gh-aw/safeoutputs/config.json`;
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
      // Before treating a missing outputs file as a graceful no-op, check whether
      // the safeoutputs MCP gateway reported 0 registered tools during setup.
      // When that flag exists the agent could not emit any safe outputs because
      // every safeoutputs call failed with "unknown tool" — this is a gateway
      // infrastructure failure, not an intentional no-op, and must surface as an
      // error rather than a silent green run.
      const runnerTemp = process.env.RUNNER_TEMP || "/home/runner/work/_temp";
      const gatewayEmptyFlagPath = `${runnerTemp}/gh-aw/safeoutputs/gateway_empty.flag`;
      if (fs.existsSync(gatewayEmptyFlagPath)) {
        core.setFailed(
          `safeoutputs MCP gateway registered 0 tools during setup; the agent could not emit any safe outputs. ` +
            `This is a gateway infrastructure failure, not a normal no-op. ` +
            `Check the MCP gateway startup logs for ECONNRESET errors or delayed backend registration and re-run the workflow.`
        );
        return;
      }
      core.info(`Output file does not exist: ${outputFile} — no safe-output items were emitted; treating as empty collection (graceful no-op)`);
      const emptyOutput = { items: [], errors: [] };
      const emptyOutputJson = JSON.stringify(emptyOutput);
      // Write agent_output.json for consistent downstream handling so the safe_outputs job
      // always finds a valid (empty) collection file even when the agent emitted nothing.
      try {
        fs.mkdirSync(TMP_GH_AW_PATH, { recursive: true });
        const agentOutputFile = require("path").join(TMP_GH_AW_PATH, AGENT_OUTPUT_FILENAME);
        fs.writeFileSync(agentOutputFile, emptyOutputJson, "utf8");
        core.info(`Stored empty collection to: ${agentOutputFile}`);
        core.exportVariable("GH_AW_AGENT_OUTPUT", agentOutputFile);
      } catch (writeError) {
        core.error(`Failed to write empty agent output file: ${getErrorMessage(writeError)}`);
      }
      // Always set the step output even if the artifact write failed;
      // downstream steps reading GH_AW_AGENT_OUTPUT must handle the var being absent.
      core.setOutput("output", emptyOutputJson);
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
    /** @type {any} */
    let expectedOutputTypes = {};
    if (safeOutputsConfig) {
      try {
        // safeOutputsConfig is already a parsed object from the file
        // Normalize all config keys to use underscores instead of dashes
        core.info(`[INGESTION] Normalizing config keys (dash -> underscore)`);
        expectedOutputTypes = Object.fromEntries(Object.entries(safeOutputsConfig).map(([key, value]) => [key.replace(/-/g, "_"), value]));
        core.info(`[INGESTION] Expected output types after normalization: ${JSON.stringify(Object.keys(expectedOutputTypes))}`);
        core.info(`[INGESTION] Expected output types full config: ${JSON.stringify(expectedOutputTypes)}`);
        // Extract max-bot-mentions from config (defaults to undefined, using neutralizeBotTriggers default)
        const rawMaxBotMentions = parseIntTemplatable(expectedOutputTypes.max_bot_mentions, 0);
        if (rawMaxBotMentions > 0) {
          maxBotMentions = rawMaxBotMentions;
        }
        // Remove global config keys so they are not treated as valid output types
        delete expectedOutputTypes.max_bot_mentions;
      } catch (error) {
        const errorMsg = getErrorMessage(error);
        core.info(`Warning: Could not parse safe-outputs config: ${errorMsg}`);
      }
    }
    // Parse JSONL (JSON Lines) format: each line is a separate JSON object
    // CRITICAL: This expects one JSON object per line. If JSON is formatted with
    // indentation/pretty-printing, parsing will fail.
    const lines = outputContent.trim().split("\n");

    // Resolve allowed repos for cross-repo targeting validation in the pre-scan loop.
    // The triggering repository is always allowed; additional repos come from config.
    const defaultTargetRepo = `${context.repo.owner}/${context.repo.repo}`;
    const allowedRepos = parseAllowedRepos(safeOutputsConfig?.allowed_repos || safeOutputsConfig?.["allowed-repos"]);

    // Pre-scan: collect target issue authors from add_comment items with explicit item_number
    // so they are included in the first sanitization pass.
    // We do this before the main loop so the allowed mentions array can be extended.
    for (const line of lines) {
      const trimmedLine = line.trim();
      if (!trimmedLine) continue;
      try {
        const preview = JSON.parse(trimmedLine);
        const previewType = (preview?.type || "").replace(/-/g, "_");
        if (previewType === "add_comment" && preview.item_number != null && typeof preview.item_number === "number") {
          // Determine which repo to query (use explicit repo field or fall back to triggering repo)
          let targetOwner = context.repo.owner;
          let targetRepo = context.repo.repo;
          if (typeof preview.repo === "string") {
            const candidateRepo = preview.repo.trim();
            if (candidateRepo.includes("/")) {
              // Validate the user-supplied repo against allowedRepos before making API calls
              const repoValidation = validateTargetRepo(candidateRepo, defaultTargetRepo, allowedRepos);
              if (repoValidation.valid) {
                const parts = candidateRepo.split("/");
                targetOwner = parts[0];
                targetRepo = parts[1];
              } else {
                core.info(`[MENTIONS] Skipping cross-repo mention lookup for '${candidateRepo}': ${repoValidation.error}`);
              }
            }
          }
          try {
            const { data: issueData } = await github.rest.issues.get({
              owner: targetOwner,
              repo: targetRepo,
              issue_number: preview.item_number,
            });
            if (issueData.user?.login && !isPayloadUserBot(issueData.user)) {
              const issueAuthor = issueData.user.login;
              if (!allowedMentions.some(m => m.toLowerCase() === issueAuthor.toLowerCase())) {
                allowedMentions.push(issueAuthor);
                core.info(`[MENTIONS] Added target issue #${preview.item_number} author '${issueAuthor}' to allowed mentions`);
              }
            }
          } catch (fetchErr) {
            core.info(`[MENTIONS] Could not fetch issue #${preview.item_number} author for mention allowlist: ${getErrorMessage(fetchErr)}`);
          }
        }
      } catch {
        // Ignore parse errors - main loop will report them
      }
    }

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

        const typeConfig = expectedOutputTypes[itemType];
        const normalizeIssueClosingKeywords = typeConfig !== null && typeof typeConfig === "object" && typeConfig.normalize_closing_keywords === true;
        if (itemType === "dispatch_workflow") {
          const hasWorkflowName = typeof item.workflow_name === "string" && item.workflow_name.trim().length > 0;
          if (!hasWorkflowName && typeConfig !== null && typeof typeConfig === "object" && Array.isArray(typeConfig.workflows)) {
            const { workflows: configuredWorkflows } = typeConfig;
            if (configuredWorkflows.length === 1 && typeof configuredWorkflows[0] === "string" && configuredWorkflows[0].trim().length > 0) {
              item.workflow_name = configuredWorkflows[0].trim();
              core.info(`[INGESTION] Line ${i + 1}: Inferred dispatch_workflow workflow_name='${item.workflow_name}' from safe-outputs config`);
            }
          }
        }

        // Use the validation engine to validate the item
        if (hasValidationConfig(itemType)) {
          const validationResult = validateItem(item, itemType, i + 1, {
            allowedAliases: allowedMentions,
            maxMentions,
            maxBotMentions,
            normalizeIssueClosingKeywords,
            dataEnabled: typeConfig !== null && typeof typeConfig === "object" && typeConfig.data_enabled === true,
            dataSchema: typeConfig !== null && typeof typeConfig === "object" ? typeConfig.data_schema : undefined,
          });
          if (!validationResult.isValid) {
            if (validationResult.error) {
              errors.push(validationResult.error);
            }
            continue;
          }
          // Use the normalized item (with sanitized/validated fields) rather
          // than the raw input, so downstream consumers see the canonical form.
          core.info(`Line ${i + 1}: Valid ${itemType} item`);
          parsedItems.push(validationResult.normalizedItem);
        } else {
          // Fall back to validateItemWithSafeJobConfig for unknown types
          const jobOutputType = expectedOutputTypes[itemType];
          if (!jobOutputType) {
            errors.push(`Line ${i + 1}: Unknown output type '${itemType}'`);
            continue;
          }
          const safeJobConfig = jobOutputType;
          const validation = validateItemWithSafeJobConfig(item, safeJobConfig, i + 1);
          if (!validation.isValid) {
            errors.push(...validation.errors);
            continue;
          }
          core.info(`Line ${i + 1}: Valid ${itemType} item`);
          parsedItems.push(validation.normalizedItem);
        }
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

    // Check if any patch or bundle files exist for detection job conditional
    // Patches are named aw-{branch}.patch (format-patch transport, one per branch)
    // Bundles are named aw-{branch}.bundle (git bundle transport, preserves merge topology)
    const patchDir = "/tmp/gh-aw";
    let hasPatch = false;
    const patchFiles = [];
    try {
      if (fs.existsSync(patchDir)) {
        const dirEntries = fs.readdirSync(patchDir);
        for (const entry of dirEntries) {
          if (/^aw-.+\.(patch|bundle)$/.test(entry)) {
            patchFiles.push(entry);
            hasPatch = true;
          }
        }
      }
    } catch {
      // Directory cannot be read — ignored, assume no patch is present.
    }
    if (hasPatch) {
      core.info(`Found ${patchFiles.length} patch/bundle file(s): ${patchFiles.join(", ")}`);
    } else {
      core.info(`No patch or bundle files found in: ${patchDir}`);
    }

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
    core.setFailed(`${ERR_API}: Agent output ingestion failed: ${errorMsg}`);
    throw error;
  }
}

module.exports = { main };
