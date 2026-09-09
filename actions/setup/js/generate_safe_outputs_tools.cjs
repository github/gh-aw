// @ts-check
/// <reference types="@actions/github-script" />
"use strict";
require("./shim.cjs");
// @safe-outputs-exempt SEC-004 — schema generator; does not process user body content. The substring "body:" appears only in the comment referencing the "allow-body" config option.

/**
 * generate_safe_outputs_tools.cjs
 *
 * Generates the safe outputs tools.json at runtime by:
 * 1. Writing tools_meta.json and validation.json from env var payloads (if provided)
 * 2. Loading the full safe_outputs_tools.json from the actions folder
 * 3. Filtering tools based on config.json (which tools are enabled)
 * 4. Applying description suffixes and repo parameters from tools_meta.json
 * 5. Appending dynamic tools (dispatch_workflow, call_workflow, custom jobs) from tools_meta.json
 * 6. Writing the result to the output tools.json path
 *
 * Environment variables:
 *   GH_AW_TOOLS_META_JSON - JSON payload for tools_meta.json (written to disk before processing)
 *   GH_AW_VALIDATION_JSON - JSON payload for validation.json (written to disk if provided)
 *   GH_AW_SAFE_OUTPUTS_TOOLS_SOURCE_PATH - Path to the source safe_outputs_tools.json
 *     Default: ${RUNNER_TEMP}/gh-aw/actions/safe_outputs_tools.json
 *   GH_AW_SAFE_OUTPUTS_CONFIG_PATH - Path to config.json (used to determine enabled tools)
 *     Default: ${RUNNER_TEMP}/gh-aw/safeoutputs/config.json
 *   GH_AW_SAFE_OUTPUTS_TOOLS_META_PATH - Path to tools_meta.json (descriptions, repo params, dynamic tools)
 *     Default: ${RUNNER_TEMP}/gh-aw/safeoutputs/tools_meta.json
 *   GH_AW_SAFE_OUTPUTS_TOOLS_PATH - Output path for the generated tools.json
 *     Default: ${RUNNER_TEMP}/gh-aw/safeoutputs/tools.json
 */

const fs = require("fs");
const path = require("path");
const { ERR_CONFIG, ERR_PARSE, ERR_SYSTEM } = require("./error_codes.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const ADD_COMMENT_DEFAULT_DISCUSSIONS_NOTE =
  "NOTE: By default, this tool does not require discussions:write permission. Set 'discussions: true' in the workflow's safe-outputs.add-comment configuration to enable discussion comments and request this permission.";
const ADD_COMMENT_DISCUSSIONS_ENABLED_NOTE = "NOTE: Discussion comments are enabled for this workflow because discussions:write permission is available.";
const ADD_COMMENT_DISCUSSIONS_DISABLED_NOTE =
  "NOTE: Discussion comments are disabled for this workflow because discussions:write permission is not available. Set 'discussions: true' in the workflow's safe-outputs.add-comment configuration to enable discussion comments and request this permission.";
const ADD_COMMENT_REPLY_SUPPORT_SENTENCE = "Supports reply_to_id for discussion threading.";
const ADD_COMMENT_REPLY_SUPPORT_REGEX = /\s*Supports reply_to_id for discussion threading\./g;
const ISSUE_INTENT_REQUIRED_SUFFIX = "INTENT REQUIRED: rationale (string, max 280 chars) and confidence (exactly one of: LOW, MEDIUM, HIGH) are required for each call.";
const ISSUE_INTENT_OPTIONAL_SUFFIX =
  "INTENT ENCOURAGED: Include rationale (string, max 280 chars) and confidence (exactly one of: LOW, MEDIUM, HIGH) with each call. These fields are optional but strongly encouraged — they improve transparency and should normally be included. Use suggest: true alongside rationale and confidence to route for human review.";
const ADD_LABELS_STRICT_FIELD_DESC =
  'Labels to add. Each label must be an object with required fields: name (string), rationale (string, max 280 chars), and confidence (exactly one of: LOW, MEDIUM, HIGH). Plain string label names are not permitted. Example: [{"name": "bug", "rationale": "The report describes reproducible incorrect behavior.", "confidence": "HIGH"}]. Labels must exist in the repository.';
const ADD_LABELS_OPTIONAL_FIELD_DESC =
  'Labels to add. Prefer structured label objects: {"name": "bug", "rationale": "The report describes reproducible incorrect behavior.", "confidence": "HIGH", "suggest": true}. Plain strings are also accepted for compatibility. Include rationale (string, max 280 chars) and confidence (LOW, MEDIUM, or HIGH) to improve transparency; use suggest: true to route for human review. Labels must exist in the repository.';
const ASSIGN_TO_AGENT_EXAMPLE_USAGE_REGEX = /Example usage: assign_to_agent\([^)]+\)(?: or assign_to_agent\([^)]+\))?/g;
const ASSIGN_TO_AGENT_STRICT_EXAMPLE_USAGE =
  'Example usage: assign_to_agent(issue_number=123, agent="copilot", rationale="Delegate this coding task to the agent.", confidence="HIGH") or assign_to_agent(pull_number=456, agent="copilot", pull_request_repo="owner/repo", rationale="The agent should implement this PR fix.", confidence="HIGH")';
const RATIONALE_REQUIRED_DESC = "Required rationale for this change (max 280 characters).";
const CONFIDENCE_REQUIRED_DESC = "Required confidence level for this change. Must be exactly one of: LOW, MEDIUM, HIGH.";
const ISSUE_INTENT_TOOL_NAMES = new Set(["set_issue_type", "set_issue_field", "add_labels", "close_issue", "assign_to_user", "assign_to_agent"]);
const ISSUE_INTENT_SCHEMA_FIELDS = ["rationale", "confidence", "suggest"];

/**
 * Determine whether issue-intent guidance is enabled for a tool.
 * Default is disabled; explicit issue_intent: true enables it.
 *
 * @param {string} toolName
 * @param {unknown} toolConfig
 * @returns {boolean}
 */
function isIssueIntentEnabledForTool(toolName, toolConfig) {
  if (!ISSUE_INTENT_TOOL_NAMES.has(toolName)) {
    return false;
  }
  return !!(toolConfig && typeof toolConfig === "object" && "issue_intent" in toolConfig && toolConfig.issue_intent === true);
}

/**
 * Determine whether issue-intent schema fields should be omitted for a tool.
 * Default is optional (present, not required); explicit issue_intent: false omits them.
 *
 * @param {string} toolName
 * @param {unknown} toolConfig
 * @returns {boolean}
 */
function isIssueIntentDisabledForTool(toolName, toolConfig) {
  if (!ISSUE_INTENT_TOOL_NAMES.has(toolName)) {
    return false;
  }
  return !!(toolConfig && typeof toolConfig === "object" && "issue_intent" in toolConfig && toolConfig.issue_intent === false);
}

/**
 * Remove issue-intent properties from a tool schema.
 * If this removes all required fields, the required array is omitted.
 * @param {{inputSchema?: {properties?: Record<string, unknown>, required?: string[]}}} tool
 */
function stripIssueIntentSchemaFields(tool) {
  if (!tool.inputSchema || !tool.inputSchema.properties) {
    return;
  }
  for (const field of ISSUE_INTENT_SCHEMA_FIELDS) {
    if (field in tool.inputSchema.properties) {
      delete tool.inputSchema.properties[field];
    }
  }
  if (Array.isArray(tool.inputSchema.required)) {
    tool.inputSchema.required = tool.inputSchema.required.filter(f => !ISSUE_INTENT_SCHEMA_FIELDS.includes(f));
    if (tool.inputSchema.required.length === 0) {
      delete tool.inputSchema.required;
    }
  }
}

/**
 * Determine whether issue-intent guidance is omitted (neither enabled nor disabled) for a tool.
 * This is the default state: intent fields are present and optional, but no guidance suffix is added yet.
 *
 * @param {string} toolName
 * @param {unknown} toolConfig
 * @returns {boolean}
 */
function isIssueIntentOmittedForTool(toolName, toolConfig) {
  if (!ISSUE_INTENT_TOOL_NAMES.has(toolName)) {
    return false;
  }
  return !isIssueIntentEnabledForTool(toolName, toolConfig) && !isIssueIntentDisabledForTool(toolName, toolConfig);
}

/**
 * Update the rationale and confidence property descriptions on a tool to say "Required"
 * instead of "Optional", and add them to inputSchema.required so JSON Schema validators enforce them.
 * Only affects the direct properties of inputSchema (not nested schemas).
 * @param {{inputSchema?: {properties?: Record<string, {description?: string}>, required?: string[]}}} tool
 */
function makeIntentFieldDescriptionsRequired(tool) {
  const schema = tool.inputSchema;
  const properties = schema?.properties;
  if (!properties) {
    return;
  }
  const toRequire = [];
  if (properties.rationale && typeof properties.rationale === "object") {
    properties.rationale = { ...properties.rationale, description: RATIONALE_REQUIRED_DESC };
    toRequire.push("rationale");
  }
  if (properties.confidence && typeof properties.confidence === "object") {
    properties.confidence = { ...properties.confidence, description: CONFIDENCE_REQUIRED_DESC };
    toRequire.push("confidence");
  }
  if (toRequire.length > 0 && schema) {
    const required = new Set(schema.required ?? []);
    for (const field of toRequire) {
      required.add(field);
    }
    schema.required = [...required];
  }
}

/**
 * Update add_comment description to match runtime-safe-output permissions.
 * @param {string} description
 * @param {unknown} addCommentConfig
 * @returns {string}
 */
function updateAddCommentDescription(description, addCommentConfig) {
  const discussionCommentsEnabled = typeof addCommentConfig === "object" && addCommentConfig !== null && "discussions" in addCommentConfig && addCommentConfig.discussions === true;

  let updated = description || "";
  const note = discussionCommentsEnabled ? ADD_COMMENT_DISCUSSIONS_ENABLED_NOTE : ADD_COMMENT_DISCUSSIONS_DISABLED_NOTE;
  if (updated.includes(ADD_COMMENT_DEFAULT_DISCUSSIONS_NOTE)) {
    updated = updated.replace(ADD_COMMENT_DEFAULT_DISCUSSIONS_NOTE, note);
  } else if (!updated.includes(ADD_COMMENT_DISCUSSIONS_ENABLED_NOTE) && !updated.includes(ADD_COMMENT_DISCUSSIONS_DISABLED_NOTE)) {
    updated = `${updated} ${note}`.trim();
  }

  if (discussionCommentsEnabled) {
    if (!updated.includes(ADD_COMMENT_REPLY_SUPPORT_SENTENCE)) {
      updated = `${updated} ${ADD_COMMENT_REPLY_SUPPORT_SENTENCE}`.trim();
    }
  } else {
    updated = updated
      .replace(ADD_COMMENT_REPLY_SUPPORT_REGEX, "")
      .replace(/\s{2,}/g, " ")
      .trim();
  }

  return updated;
}

/**
 * Encode assign_milestone handler requirement: either milestone_number or milestone_title is required.
 * @param {{name: string, inputSchema?: {properties?: Record<string, unknown>, anyOf?: Array<{required: string[]}>}}} tool
 */
function applyAssignMilestoneAlternativeRequirements(tool) {
  if (tool.name !== "assign_milestone") {
    return;
  }
  const schema = tool.inputSchema;
  const properties = schema?.properties;
  if (!schema || !properties || typeof properties !== "object") {
    return;
  }
  if (!("milestone_number" in properties) || !("milestone_title" in properties)) {
    return;
  }
  schema.anyOf = [{ required: ["milestone_number"] }, { required: ["milestone_title"] }];
}

/**
 * Resolve ${ENV_VAR} placeholders inside a JSON string from process.env.
 * Replacement values are escaped as JSON string content so quotes, backslashes, and
 * newlines in the resolved value do not corrupt the surrounding JSON document.
 * Unresolved placeholders are left unchanged.
 * @param {string} value
 * @returns {string}
 */
function resolveEnvStringPlaceholders(value) {
  return value.replace(/\$\{([A-Z_][A-Z0-9_]*)\}/g, (match, envName) => {
    const envValue = process.env[envName];
    if (envValue === undefined) {
      return match;
    }
    // JSON.stringify wraps the value in quotes; strip them to get escaped string content.
    return JSON.stringify(envValue).slice(1, -1);
  });
}

async function main() {
  const toolsSourcePath = process.env.GH_AW_SAFE_OUTPUTS_TOOLS_SOURCE_PATH || `${process.env.RUNNER_TEMP}/gh-aw/actions/safe_outputs_tools.json`;
  const configPath = process.env.GH_AW_SAFE_OUTPUTS_CONFIG_PATH || `${process.env.RUNNER_TEMP}/gh-aw/safeoutputs/config.json`;
  const toolsMetaPath = process.env.GH_AW_SAFE_OUTPUTS_TOOLS_META_PATH || path.join(path.dirname(configPath), "tools_meta.json");
  const outputPath = process.env.GH_AW_SAFE_OUTPUTS_TOOLS_PATH || `${process.env.RUNNER_TEMP}/gh-aw/safeoutputs/tools.json`;

  // Write JSON payloads from env vars if provided (replaces heredoc-based file writing)
  if (process.env.GH_AW_TOOLS_META_JSON) {
    try {
      fs.writeFileSync(toolsMetaPath, resolveEnvStringPlaceholders(process.env.GH_AW_TOOLS_META_JSON));
    } catch (err) {
      throw new Error(`${ERR_SYSTEM}: Failed to write file ${toolsMetaPath}: ${getErrorMessage(err)}`, { cause: err });
    }
  }
  if (process.env.GH_AW_VALIDATION_JSON) {
    const validationPath = path.join(path.dirname(configPath), "validation.json");
    try {
      fs.writeFileSync(validationPath, process.env.GH_AW_VALIDATION_JSON);
    } catch (err) {
      throw new Error(`${ERR_SYSTEM}: Failed to write file ${validationPath}: ${getErrorMessage(err)}`, { cause: err });
    }
  }

  // Load all source tools from the actions folder
  if (!fs.existsSync(toolsSourcePath)) {
    const msg = `${ERR_CONFIG}: Source tools file not found at: ${toolsSourcePath}`;
    console.error(msg);
    throw new Error(msg);
  }
  /** @type {Array<{name: string, description: string, inputSchema?: {properties?: Record<string, unknown>}}>} */
  let allTools;
  /** @type {string} */
  let allToolsRaw;
  try {
    allToolsRaw = fs.readFileSync(toolsSourcePath, "utf8");
  } catch (err) {
    throw new Error(`${ERR_SYSTEM}: Failed to read tools source file ${toolsSourcePath}: ${getErrorMessage(err)}`, { cause: err });
  }
  try {
    allTools = JSON.parse(allToolsRaw);
  } catch (err) {
    throw new Error(`${ERR_PARSE}: ` + "Failed to parse tools source file " + toolsSourcePath + ": " + getErrorMessage(err), { cause: err });
  }

  // Load config to determine which tools are enabled
  if (!fs.existsSync(configPath)) {
    const msg = `${ERR_CONFIG}: Config file not found at: ${configPath}`;
    console.error(msg);
    throw new Error(msg);
  }
  /** @type {Record<string, unknown>} */
  let config;
  /** @type {string} */
  let configRaw;
  try {
    configRaw = fs.readFileSync(configPath, "utf8");
  } catch (err) {
    throw new Error(`${ERR_SYSTEM}: Failed to read config file ${configPath}: ${getErrorMessage(err)}`, { cause: err });
  }
  try {
    config = JSON.parse(configRaw);
  } catch (err) {
    throw new Error(`${ERR_PARSE}: ` + "Failed to parse config file " + configPath + ": " + getErrorMessage(err), { cause: err });
  }

  // Load tools meta (description suffixes, repo params, dynamic tools)
  /** @type {{description_suffixes?: Record<string, string>, repo_params?: Record<string, {type: string, description: string}>, dynamic_tools?: Array<unknown>, required_field_removals?: Record<string, string[]>, required_field_additions?: Record<string, string[]>, property_injections?: Record<string, Record<string, unknown>>}} */
  let toolsMeta = { description_suffixes: {}, repo_params: {}, dynamic_tools: [] };
  if (fs.existsSync(toolsMetaPath)) {
    /** @type {string} */
    let toolsMetaRaw;
    try {
      toolsMetaRaw = fs.readFileSync(toolsMetaPath, "utf8");
    } catch (err) {
      throw new Error(`${ERR_SYSTEM}: Failed to read tools meta file ${toolsMetaPath}: ${getErrorMessage(err)}`, { cause: err });
    }
    try {
      toolsMeta = JSON.parse(toolsMetaRaw);
    } catch (err) {
      throw new Error(`${ERR_PARSE}: ` + "Failed to parse tools meta file " + toolsMetaPath + ": " + getErrorMessage(err), { cause: err });
    }
  }

  // Build set of source tool names (predefined/static tools only)
  const normalizeToolName = name => String(name).replace(/-/g, "_").toLowerCase();
  const sourceToolNames = new Set(allTools.map(t => normalizeToolName(t.name)));

  // Determine enabled tools: config keys that match source tool names
  // This filters out non-tool config entries like dispatch_workflow, call_workflow,
  // mentions, max_bot_mentions, etc.
  const enabledToolNames = new Set(
    Object.keys(config)
      .map(normalizeToolName)
      .filter(name => sourceToolNames.has(name))
  );
  // Filter predefined tools to those enabled in config and apply enhancements
  const filteredTools = allTools
    .filter(tool => enabledToolNames.has(normalizeToolName(tool.name)))
    .map(tool => {
      // Deep copy to avoid modifying the original. `tool` here is parsed straight from the
      // JSON tools-source file (see toolsSourcePath above), so it can never carry a function-valued
      // `handler` field — unlike safe_outputs_tools_loader.cjs, which clones live tool objects that
      // do carry handlers and must strip them before cloning.
      let enhancedTool;
      try {
        enhancedTool = structuredClone(tool);
      } catch (err) {
        throw new Error(`${ERR_CONFIG}: ` + "Failed to deep-copy tool " + tool.name + ": " + getErrorMessage(err), { cause: err });
      }

      // Apply description suffix if available (e.g., " CONSTRAINTS: Maximum 5 issues.")
      const descSuffix = toolsMeta.description_suffixes?.[tool.name];
      if (descSuffix) {
        enhancedTool.description = (enhancedTool.description || "") + descSuffix;
      }
      if (isIssueIntentEnabledForTool(tool.name, config[tool.name])) {
        enhancedTool.description = `${enhancedTool.description || ""} ${ISSUE_INTENT_REQUIRED_SUFFIX}`.trim();
        // Update top-level rationale/confidence descriptions to say "required" instead of "optional",
        // and add them to inputSchema.required so JSON Schema validators enforce them.
        makeIntentFieldDescriptionsRequired(enhancedTool);
        // For add_labels strict mode, replace the labels items schema with an object-only
        // variant that requires name, rationale, and confidence, and update the labels description.
        if (tool.name === "add_labels") {
          const labelsSchema = enhancedTool.inputSchema?.properties?.labels;
          if (labelsSchema) {
            labelsSchema.description = ADD_LABELS_STRICT_FIELD_DESC;
            if (labelsSchema.items && Array.isArray(labelsSchema.items.oneOf)) {
              const objectSchema = labelsSchema.items.oneOf.find(/** @param {{type: string}} s */ s => s.type === "object");
              if (objectSchema) {
                const sourceProperties = objectSchema.properties ?? {};
                const strictProperties = { ...sourceProperties };
                if (strictProperties.rationale) {
                  strictProperties.rationale = { ...strictProperties.rationale, description: "Required rationale for the label (max 280 characters)." };
                }
                if (strictProperties.confidence) {
                  strictProperties.confidence = { ...strictProperties.confidence, description: "Required confidence level for the label. Must be exactly one of: LOW, MEDIUM, HIGH." };
                }
                labelsSchema.items = {
                  ...objectSchema,
                  required: ["name", "rationale", "confidence"],
                  properties: strictProperties,
                };
              }
            }
          }
        }
        // Update inline example calls to include required fields for assign_to_agent
        if (tool.name === "assign_to_agent") {
          enhancedTool.description = enhancedTool.description.replace(ASSIGN_TO_AGENT_EXAMPLE_USAGE_REGEX, ASSIGN_TO_AGENT_STRICT_EXAMPLE_USAGE);
        }
      } else if (isIssueIntentDisabledForTool(tool.name, config[tool.name])) {
        stripIssueIntentSchemaFields(enhancedTool);
      } else if (isIssueIntentOmittedForTool(tool.name, config[tool.name])) {
        enhancedTool.description = `${enhancedTool.description || ""} ${ISSUE_INTENT_OPTIONAL_SUFFIX}`.trim();
        // For add_labels omitted mode, update the labels description to prefer structured objects,
        // but only when the source schema actually supports plain strings via items.oneOf.
        if (tool.name === "add_labels") {
          const labelsSchema = enhancedTool.inputSchema?.properties?.labels;
          if (labelsSchema && Array.isArray(labelsSchema.items?.oneOf)) {
            labelsSchema.description = ADD_LABELS_OPTIONAL_FIELD_DESC;
          }
        }
      }

      if (tool.name === "add_comment") {
        enhancedTool.description = updateAddCommentDescription(enhancedTool.description, config.add_comment);
      }

      if (tool.name === "linear_update_issue") {
        const linearUpdateConfig = config.linear_update_issue;
        const properties = enhancedTool.inputSchema?.properties;
        if (properties && linearUpdateConfig && typeof linearUpdateConfig === "object") {
          if (!("allow_title" in linearUpdateConfig) || linearUpdateConfig.allow_title !== true) {
            delete properties.title;
          }
          if (!("allow_body" in linearUpdateConfig) || linearUpdateConfig.allow_body !== true) {
            delete properties.body;
          }
          enhancedTool.inputSchema.anyOf = Object.keys(properties).map(field => ({ required: [field] }));
        }
      }

      // Add repo parameter to inputSchema if configured
      const repoParam = toolsMeta.repo_params?.[tool.name];
      if (repoParam) {
        if (!enhancedTool.inputSchema) {
          enhancedTool.inputSchema = { type: "object", properties: {} };
        }
        if (!enhancedTool.inputSchema.properties) {
          enhancedTool.inputSchema.properties = {};
        }
        enhancedTool.inputSchema.properties.repo = repoParam;
      }

      // Remove fields from inputSchema.required when configured (e.g. allow-body: false)
      const requiredRemovals = toolsMeta.required_field_removals?.[tool.name];
      if (requiredRemovals && Array.isArray(enhancedTool.inputSchema?.required)) {
        enhancedTool.inputSchema.required = enhancedTool.inputSchema.required.filter(/** @param {string} f */ f => !requiredRemovals.includes(f));
        if (enhancedTool.inputSchema.required.length === 0) {
          delete enhancedTool.inputSchema.required;
        }
      }

      // Add fields to inputSchema.required when configured (e.g. require-temporary-id: true)
      const requiredAdditions = toolsMeta.required_field_additions?.[tool.name];
      if (requiredAdditions && requiredAdditions.length > 0) {
        const existingRequired = Array.isArray(enhancedTool.inputSchema?.required) ? enhancedTool.inputSchema.required : [];
        enhancedTool.inputSchema.required = Array.from(new Set([...existingRequired, ...requiredAdditions]));
      }

      // Inject or override properties in inputSchema based on workflow configuration.
      // Used for dynamic fields like state_reason whose schema depends on config.
      const propertyInjections = toolsMeta.property_injections?.[tool.name];
      if (propertyInjections && typeof propertyInjections === "object") {
        if (!enhancedTool.inputSchema) {
          enhancedTool.inputSchema = { type: "object", properties: {} };
        }
        if (!enhancedTool.inputSchema.properties) {
          enhancedTool.inputSchema.properties = {};
        }
        for (const [propName, propSchema] of Object.entries(propertyInjections)) {
          enhancedTool.inputSchema.properties[propName] = propSchema;
        }
      }

      applyAssignMilestoneAlternativeRequirements(enhancedTool);

      return enhancedTool;
    });

  // Append dynamic tools (custom jobs, dispatch_workflow, call_workflow)
  const dynamicTools = Array.isArray(toolsMeta.dynamic_tools) ? toolsMeta.dynamic_tools : [];
  const allFilteredTools = [...filteredTools, ...dynamicTools];

  // Write the result to the output path
  try {
    fs.writeFileSync(outputPath, JSON.stringify(allFilteredTools, null, 2));
  } catch (err) {
    throw new Error(`${ERR_SYSTEM}: Failed to write file ${outputPath}: ${getErrorMessage(err)}`, { cause: err });
  }

  const debugEnabled = process.env.DEBUG === "*" || (process.env.DEBUG || "").includes("safe_outputs");
  if (debugEnabled) {
    const infoMsg = `Generated tools.json with ${allFilteredTools.length} tools (${filteredTools.length} static + ${dynamicTools.length} dynamic)`;
    core.info(infoMsg);
  }
}

module.exports = { main };

// Run when executed directly (e.g. node generate_safe_outputs_tools.cjs)
if (require.main === module) {
  main().catch(err => {
    process.exit(1);
  });
}
