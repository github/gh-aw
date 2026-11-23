// @ts-check

const path = require("path");
const tools = require("./safe_outputs_tools.json");
const { redactArtifactMaskedValues } = require("./add_mask_redaction.cjs");
const { ERR_VALIDATION } = require("./error_codes.cjs");
const { iterateJsonlEvents } = require("./jsonl_helpers.cjs");

const schemas = new Map(tools.map(tool => [tool.name, tool.inputSchema]));
const controlKeys = new Set([
  ...schemas.keys(),
  ...Array.from(schemas.keys(), name => name.replace(/_/g, "-")),
  ...tools.flatMap(tool => Object.keys(tool.inputSchema.properties || {})),
  "max",
  "min",
  "side",
  "allowed_events",
  "allowed-events",
  "defaultMax",
  "fields",
  "customValidation",
  "dataEnabled",
  "dataSchema",
  "collapseData",
  "required",
  "type",
  "typeHint",
  "sanitize",
  "maxLength",
  "minLength",
  "positiveInteger",
  "optionalPositiveInteger",
  "allowAuto",
  "allowNull",
  "allowEmpty",
  "issueOrPRNumber",
  "issueNumberOrTemporaryId",
  "enum",
  "itemType",
  "itemSanitize",
  "itemMaxLength",
  "pattern",
  "patternError",
  "temporaryId",
  "rejectIfOversized",
  "x-strip-on-error",
]);
const publicEnums = new Set(tools.flatMap(tool => Object.values(tool.inputSchema.properties || {}).flatMap(property => property.enum || [])));

/**
 * Config and validation files are consumed after the redaction step too.
 * Preserve their public vocabulary, but never rewrite a constraint or target.
 * @param {any} value
 * @param {(text: string) => string} redactText
 * @param {string} [field]
 */
function assertSafeControl(value, redactText, field = "") {
  if (typeof value === "string") {
    if (["type", "itemType"].includes(field) && ["string", "number", "integer", "boolean", "array", "object", "null"].includes(value)) return;
    if (["enum", "allowed_events", "allowed-events"].includes(field) && publicEnums.has(value)) return;
    if (redactText(value) !== value) throw new Error(`${ERR_VALIDATION}: Secret redaction would change safe-output control data`);
  } else if (typeof value === "number") {
    if (redactText(String(value)) !== String(value)) throw new Error(`${ERR_VALIDATION}: Secret redaction would change a safe-output constraint`);
  } else if (Array.isArray(value)) {
    value.forEach(item => assertSafeControl(item, redactText, field));
  } else if (value && typeof value === "object") {
    for (const [key, nested] of Object.entries(value)) {
      if (!controlKeys.has(key) && redactText(key) !== key) throw new Error(`${ERR_VALIDATION}: Secret redaction would change a custom safe-output control field`);
      assertSafeControl(nested, redactText, key);
    }
  }
}

/**
 * Public schema vocabulary is not secret payload data. Only schemas shipped
 * with the action can authorize preserving a key or enum, never agent files.
 * @param {any} value
 * @param {any} schema
 * @param {(text: string) => string} redactText
 * @returns {any}
 */
function redactBySchema(value, schema, redactText) {
  if (typeof value === "string") return schema?.enum?.includes(value) ? value : redactText(value);
  if (Array.isArray(value)) return value.map(item => redactBySchema(item, schema?.items, redactText));
  if (value && typeof value === "object") {
    return Object.fromEntries(
      Object.entries(value).map(([key, nested]) => {
        const property = schema?.properties && Object.hasOwn(schema.properties, key) ? schema.properties[key] : undefined;
        return [property ? key : redactText(key), redactBySchema(nested, property, redactText)];
      })
    );
  }
  return value;
}

/**
 * @param {any} item
 * @param {(text: string) => string} redactText
 * @returns {any}
 */
function redactMessage(item, redactText) {
  if (!item || typeof item !== "object" || Array.isArray(item) || typeof item.type !== "string") {
    throw new Error(`${ERR_VALIDATION}: Invalid safe-output message during secret redaction`);
  }
  const schema = schemas.get(item.type);
  if (!schema && redactText(item.type) !== item.type) {
    throw new Error(`${ERR_VALIDATION}: Secret redaction would change a custom safe-output type`);
  }
  const properties = { ...schema?.properties, type: { enum: [item.type] } };
  for (const property of Object.values(schema?.properties || {})) {
    for (const alias of property["x-synonyms"] || []) properties[alias] = property;
  }
  // Unknown top-level fields may be custom job inputs or Claim routing data.
  // Do not silently rename them and then execute a different message.
  for (const key of Object.keys(item)) {
    if (!Object.hasOwn(properties, key) && redactText(key) !== key) {
      throw new Error(`${ERR_VALIDATION}: Secret redaction would change a custom safe-output field`);
    }
  }
  return redactBySchema(item, { properties }, redactText);
}

/**
 * Return null for ordinary artifacts; recognized executable outputs must be
 * valid JSON/JSONL or throw so processFile removes them before publication.
 * @param {string} filePath
 * @param {string} content
 * @param {string[]} maskedValues
 * @param {(text: string) => string} redactText
 * @returns {string | null}
 */
function redactSafeOutputContent(filePath, content, maskedValues, redactText) {
  const name = path.basename(filePath);
  const control =
    filePath === process.env.GH_AW_SAFE_OUTPUTS_CONFIG_PATH || filePath === process.env.GH_AW_VALIDATION_CONFIG_PATH || (path.basename(path.dirname(filePath)) === "safeoutputs" && ["config.json", "validation.json"].includes(name));
  const envelope = name === "agent_output.json";
  if (!control && !envelope && name !== "safeoutputs.jsonl" && filePath !== process.env.GH_AW_SAFE_OUTPUTS) return null;
  const parse = record => {
    try {
      return JSON.parse(record);
    } catch (error) {
      throw new Error(`${ERR_VALIDATION}: Invalid JSON safe-output record during secret redaction`, { cause: error });
    }
  };
  if (control) {
    const value = parse(content);
    if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error(`${ERR_VALIDATION}: Invalid safe-output control object`);
    const redacted = redactArtifactMaskedValues(content, maskedValues, parsed => {
      assertSafeControl(parsed, redactText);
      return parsed;
    });
    if (redacted !== content) throw new Error(`${ERR_VALIDATION}: Secret redaction would change a safe-output constraint`);
    return content;
  }
  const transform = value => {
    if (!envelope) return redactMessage(value, redactText);
    if (!value || typeof value !== "object" || !Array.isArray(value.items)) {
      throw new Error(`${ERR_VALIDATION}: Invalid agent-output envelope during secret redaction`);
    }
    const properties = Object.fromEntries(["errors", "collectorEmptyOutputCause", "collectorFailureCause", "collectorDriverExitCode", "collectorRetryCount", "collectorEngineErrorType", "collectorInfrastructureCause"].map(key => [key, {}]));
    return Object.fromEntries(
      Object.entries(value).map(([key, nested]) =>
        key === "items" ? [key, nested.map(item => redactMessage(item, redactText))] : [Object.hasOwn(properties, key) ? key : redactText(key), redactBySchema(nested, properties[key], redactText)]
      )
    );
  };
  if (envelope) {
    parse(content);
    return redactArtifactMaskedValues(content, maskedValues, transform);
  }
  const redactedLines = [];
  for (const event of iterateJsonlEvents(content)) {
    if (event.type === "invalid") {
      if (event.reason !== "blank") {
        throw new Error(`${ERR_VALIDATION}: Invalid JSON safe-output record during secret redaction`, { cause: event.error });
      }
      redactedLines.push(event.rawLine);
      continue;
    }
    const newline = event.rawLine.endsWith("\n") ? "\n" : "";
    const line = newline ? event.rawLine.slice(0, -1) : event.rawLine;
    redactedLines.push(redactArtifactMaskedValues(line, maskedValues, transform), newline);
  }
  return redactedLines.join("");
}

module.exports = { redactSafeOutputContent };
