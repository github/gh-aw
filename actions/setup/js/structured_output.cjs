// @ts-check

const fs = require("fs");
const path = require("path");
const { createRequire } = require("module");
const { getErrorMessage } = require("./error_helpers.cjs");
const runtimeRequire = createRequire(path.join(__dirname, "structured-output-validator", "package.json"));
/** @type {typeof import("ajv").default} */
const Ajv = runtimeRequire("ajv").default;
/** @type {typeof import("ajv/dist/2020").default} */
const Ajv2020 = runtimeRequire("ajv/dist/2020").default;
/** @type {typeof import("ajv-formats").default} */
const addFormats = runtimeRequire("ajv-formats").default;

const MAX_STRUCTURED_OUTPUT_BYTES = 256 * 1024;
/** @type {WeakMap<import("ajv").AnySchemaObject, import("ajv").ValidateFunction>} */
const validators = new WeakMap();

/**
 * @param {string} schemaPath
 * @returns {import("ajv").AnySchemaObject}
 */
function loadStructuredOutputSchema(schemaPath) {
  let content;
  try {
    content = fs.readFileSync(schemaPath, "utf8");
  } catch (error) {
    throw new Error(`Unable to read structured output schema at ${schemaPath}`, { cause: error });
  }
  return parseStructuredOutputJSON(content, "Structured output schema");
}

/**
 * @param {string} content
 * @param {string} label
 */
function parseStructuredOutputJSON(content, label) {
  try {
    return JSON.parse(content);
  } catch {
    throw new Error(`${label} must contain valid JSON`);
  }
}

/**
 * @param {unknown} value
 * @param {import("ajv").AnySchemaObject} schema
 */
function validateStructuredOutput(value, schema) {
  validateFiniteJSONNumbers(value);
  let validate = validators.get(schema);
  if (!validate) {
    if (
      schema.$schema !== undefined &&
      (typeof schema.$schema !== "string" || !["http://json-schema.org/draft-07/schema", "https://json-schema.org/draft-07/schema", "https://json-schema.org/draft/2020-12/schema"].includes(schema.$schema.replace(/#$/, "")))
    ) {
      throw new Error("Structured output schema must use draft-07 or draft 2020-12");
    }
    const draft = schema.$schema?.replace(/#$/, "");
    const modern = draft === "https://json-schema.org/draft/2020-12/schema";
    const Validator = modern ? Ajv2020 : Ajv;
    const ajv = new Validator({ allErrors: true, strict: false, strictNumbers: true, validateFormats: true });
    addFormats(ajv);
    const normalized = schema.$schema ? { ...schema, $schema: modern ? "https://json-schema.org/draft/2020-12/schema" : "http://json-schema.org/draft-07/schema#" } : schema;
    validate = ajv.compile(normalized);
    validators.set(schema, validate);
  }
  if (!validate(value)) {
    // Only schema locations and constraint names: never echo model-produced values.
    const details = (validate.errors || []).map(error => `${error.schemaPath}: ${error.keyword}`).join("; ");
    throw new Error(`Structured output schema violation: ${details}`);
  }
}

/** @param {unknown} value */
function validateFiniteJSONNumbers(value) {
  if (typeof value === "number" && !Number.isFinite(value)) {
    throw new Error("Structured output cannot contain non-finite JSON numbers");
  }
  if (value && typeof value === "object") {
    for (const child of Object.values(value)) {
      validateFiniteJSONNumbers(child);
    }
  }
}

/**
 * @param {Pick<typeof import("@actions/core"), "setOutput">} core
 * @param {NodeJS.ProcessEnv} [env]
 */
function publishStructuredOutput(core, env = process.env) {
  const filename = env.GH_AW_STRUCTURED_OUTPUT_FILE;
  const encodedSchema = env.GH_AW_STRUCTURED_OUTPUT_SCHEMA;
  if (!filename || !encodedSchema) {
    throw new Error("Structured output requires the compiled schema and response file");
  }
  const fd = fs.openSync(filename, fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW);
  let content;
  try {
    const stat = fs.fstatSync(fd);
    if (!stat.isFile() || stat.size > 1024 * 1024) {
      throw new Error("Structured output must be a regular JSON file no larger than 1 MiB");
    }
    content = fs.readFileSync(fd, "utf8");
  } catch (error) {
    throw new Error("Unable to read structured output response: " + getErrorMessage(error), { cause: error });
  } finally {
    fs.closeSync(fd);
  }
  const value = parseStructuredOutputJSON(content, "Structured output response");
  validateStructuredOutput(value, parseStructuredOutputJSON(encodedSchema, "Compiled structured output schema"));
  const encoded = JSON.stringify(value);
  if (Buffer.byteLength(encoded, "utf8") > MAX_STRUCTURED_OUTPUT_BYTES || Buffer.byteLength(encoded, "utf16le") > MAX_STRUCTURED_OUTPUT_BYTES) {
    throw new Error("Structured output exceeds the 256 KiB job-output limit; reduce the response size");
  }
  core.setOutput("structured", encoded);
}

module.exports = {
  loadStructuredOutputSchema,
  validateStructuredOutput,
  publishStructuredOutput,
  MAX_STRUCTURED_OUTPUT_BYTES,
};
