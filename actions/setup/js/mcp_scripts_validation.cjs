// @ts-check

/**
 * MCP Scripts Validation Helpers
 *
 * This module provides validation utilities for mcp-scripts MCP server.
 */

/**
 * Maximum allowed byte length for any single string-typed input parameter (SM-IS-01).
 * 10 KB = 10 * 1024 bytes.
 */
const MAX_STRING_INPUT_BYTES = 10 * 1024;

/**
 * Validate required fields in tool arguments
 * @param {Object} args - The arguments object to validate
 * @param {Object} inputSchema - The input schema containing required fields
 * @returns {string[]} Array of missing field names (empty if all required fields are present)
 */
function validateRequiredFields(args, inputSchema) {
  const requiredFields = inputSchema && Array.isArray(inputSchema.required) ? inputSchema.required : [];

  if (!requiredFields.length) {
    return [];
  }

  const missing = requiredFields.filter(f => {
    const value = args[f];
    return value === undefined || value === null || (typeof value === "string" && value.trim() === "");
  });

  return missing;
}

/**
 * Validate that no string-typed input parameter exceeds the maximum allowed byte length (SM-IS-01).
 * Implementations MUST enforce a maximum input string length of at least 10KB for each
 * string-typed input parameter. Inputs exceeding the configured maximum MUST be rejected with a
 * validation error before the tool script is invoked. Implementations MUST NOT silently truncate
 * oversized inputs.
 * When a field declares an explicit schema maxLength, that explicit character limit is enforced here;
 * otherwise the default SM-IS-01 10KB byte limit applies.
 *
 * Scope: validates only top-level (direct) properties of the schema where `type === "string"`.
 * Nested object/array schemas are not recursively validated, consistent with the SM-IS-01
 * requirement that applies to "input parameters" (top-level tool arguments).
 *
 * @param {Object} args - The arguments object to validate
 * @param {Object} inputSchema - The input schema describing property types
 * @param {number} [maxBytes] - Maximum allowed bytes per string (defaults to MAX_STRING_INPUT_BYTES)
 * @returns {{ field: string, actualLength: number, limit: number, unit: "bytes" | "characters" }[]} Array of violations (empty if all within limit)
 */
function validateStringInputLengths(args, inputSchema, maxBytes) {
  const limit = typeof maxBytes === "number" ? maxBytes : MAX_STRING_INPUT_BYTES;
  const properties = inputSchema && inputSchema.properties ? inputSchema.properties : {};
  const violations = [];

  for (const [field, schema] of Object.entries(properties)) {
    if (schema && schema.type === "string") {
      const value = args[field];
      if (typeof value === "string") {
        if (typeof schema.maxLength === "number") {
          const characterLength = Array.from(value).length;
          if (characterLength > schema.maxLength) {
            violations.push({ field, actualLength: characterLength, limit: schema.maxLength, unit: "characters" });
          }
          continue;
        }

        const byteLength = Buffer.byteLength(value, "utf8");
        if (byteLength > limit) {
          violations.push({ field, actualLength: byteLength, limit, unit: "bytes" });
        }
      }
    }
  }

  return violations;
}

/**
 * Build actionable E006 validation message for string length violations.
 *
 * @param {string} toolName - Tool name being validated
 * @param {{ field: string, actualLength: number, limit: number, unit: "bytes" | "characters" }[]} violations - Violations returned by validateStringInputLengths
 * @returns {string} E006 message
 */
function buildStringLengthValidationError(toolName, violations) {
  const details = violations.map(v => `'${v.field}' exceeds maximum length of ${v.limit} ${v.unit} (got ${v.actualLength} ${v.unit})`).join(", ");
  return `E006: Input string parameter(s) exceed maximum length for tool '${toolName}': ${details}`;
}

/**
 * Validate that string-typed arguments meet the schema's minLength constraints.
 * Trims values before comparing (matching downstream validator behavior).
 * Only checks top-level properties with `type === "string"` and an explicit `minLength`.
 * Absent or non-string values are skipped.
 *
 * @param {Object} args - The arguments object to validate
 * @param {Object} inputSchema - The input schema describing property types and constraints
 * @returns {{ field: string, minLength: number, actualLength: number }[]} Array of violations (empty if all OK)
 */
function validateStringMinLengths(args, inputSchema) {
  const properties = inputSchema && inputSchema.properties ? inputSchema.properties : {};
  const violations = [];

  for (const [field, schema] of Object.entries(properties)) {
    if (schema && schema.type === "string" && typeof schema.minLength === "number") {
      const value = args[field];
      if (typeof value === "string") {
        const trimmedLength = value.trim().length;
        if (trimmedLength < schema.minLength) {
          violations.push({ field, minLength: schema.minLength, actualLength: trimmedLength });
        }
      }
    }
  }

  return violations;
}

function isPlainObject(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function schemaTypeMatches(value, expectedType) {
  switch (expectedType) {
    case "object":
      return isPlainObject(value);
    case "array":
      return Array.isArray(value);
    case "string":
      return typeof value === "string";
    case "number":
      return typeof value === "number" && Number.isFinite(value);
    case "integer":
      return typeof value === "number" && Number.isInteger(value);
    case "boolean":
      return typeof value === "boolean";
    case "null":
      return value === null;
    default:
      return true;
  }
}

function formatPath(basePath, key) {
  if (!basePath) {
    return key;
  }
  if (typeof key === "number") {
    return `${basePath}[${key}]`;
  }
  return `${basePath}.${key}`;
}

function validateSchemaNode(value, schema, path, options = {}) {
  if (!schema || typeof schema !== "object") {
    return null;
  }

  if (Array.isArray(schema.oneOf) && schema.oneOf.length > 0) {
    let successCount = 0;
    /** @type {Array<{path: string, message: string, expected?: string, received?: string}>} */
    const errors = [];
    for (const subSchema of schema.oneOf) {
      const error = validateSchemaNode(value, subSchema, path, options);
      if (!error) {
        successCount += 1;
        continue;
      }
      errors.push(error);
    }
    if (successCount !== 1) {
      if (errors.length > 0) {
        errors.sort((a, b) => (b.path || "").length - (a.path || "").length);
        return errors[0];
      }
      return { path, message: "must match exactly one schema option" };
    }
    return null;
  }

  if (Array.isArray(schema.anyOf) && schema.anyOf.length > 0) {
    /** @type {Array<{path: string, message: string, expected?: string, received?: string}>} */
    const errors = [];
    for (const subSchema of schema.anyOf) {
      const error = validateSchemaNode(value, subSchema, path, options);
      if (!error) {
        return null;
      }
      errors.push(error);
    }
    if (errors.length > 0) {
      errors.sort((a, b) => (b.path || "").length - (a.path || "").length);
      return errors[0];
    }
    return { path, message: "must match at least one schema option" };
  }

  if (schema.type) {
    const allowedTypes = Array.isArray(schema.type) ? schema.type : [schema.type];
    const typeMatched = allowedTypes.some(type => schemaTypeMatches(value, type));
    if (!typeMatched) {
      return {
        path,
        message: `must be ${allowedTypes.length === 1 ? `a ${allowedTypes[0]}` : `one of: ${allowedTypes.join(", ")}`}`,
        expected: allowedTypes.join(" | "),
        received: JSON.stringify(value),
      };
    }
  }

  if (Array.isArray(schema.enum) && schema.enum.length > 0) {
    const enumMatched = schema.enum.some(candidate => Object.is(candidate, value));
    if (!enumMatched) {
      return {
        path,
        message: `must be one of: ${schema.enum.join(", ")}`,
        expected: JSON.stringify(schema.enum),
        received: JSON.stringify(value),
      };
    }
  }

  if (typeof value === "string" && typeof schema.maxLength === "number") {
    const characterLength = Array.from(value).length;
    if (characterLength > schema.maxLength) {
      return {
        path,
        message: `must be at most ${schema.maxLength} characters`,
        expected: `maxLength ${schema.maxLength}`,
        received: `${characterLength} characters`,
      };
    }
  }

  if (isPlainObject(value)) {
    if (!options.skipRequiredAtRoot || path !== "") {
      const required = Array.isArray(schema.required) ? schema.required : [];
      for (const field of required) {
        if (!(field in value)) {
          return {
            path: formatPath(path, field),
            message: "is required",
          };
        }
      }
    }

    const properties = isPlainObject(schema.properties) ? schema.properties : {};
    if (schema.additionalProperties === false) {
      for (const key of Object.keys(value)) {
        if (!(key in properties)) {
          return {
            path: formatPath(path, key),
            message: "is not allowed by the schema",
          };
        }
      }
    }

    for (const [key, propertySchema] of Object.entries(properties)) {
      if (!(key in value)) {
        continue;
      }
      const nestedError = validateSchemaNode(value[key], propertySchema, formatPath(path, key), options);
      if (nestedError) {
        return nestedError;
      }
    }
  }

  if (Array.isArray(value) && schema.items) {
    for (let i = 0; i < value.length; i++) {
      const nestedError = validateSchemaNode(value[i], schema.items, formatPath(path, i), options);
      if (nestedError) {
        return nestedError;
      }
    }
  }

  return null;
}

function validateArgumentsAgainstSchema(args, inputSchema) {
  return validateSchemaNode(args, inputSchema, "", { skipRequiredAtRoot: true });
}

function validateValueAgainstSchema(value, schema) {
  return validateSchemaNode(value, schema, "", { skipRequiredAtRoot: false });
}

/**
 * Build an example value for a single property from its schema.
 * @param {string} field
 * @param {any} propertySchema
 * @returns {any}
 */
function exampleValueForProperty(field, propertySchema) {
  const schema = isPlainObject(propertySchema) ? propertySchema : {};
  if (Array.isArray(schema.enum) && schema.enum.length > 0) {
    return schema.enum[schema.enum.length - 1];
  }
  const type = Array.isArray(schema.type) ? schema.type[0] : schema.type;
  switch (type) {
    case "boolean":
      return false;
    case "number":
    case "integer":
      return 1;
    case "array":
      return [];
    case "object":
      return {};
    default:
      return EXAMPLE_LABEL_FIELD_VALUES[field] || field;
  }
}

const EXAMPLE_LABEL_FIELD_VALUES = {
  name: "bug",
  rationale: "Why this label applies",
  confidence: "HIGH",
};

const DEFAULT_LABEL_REQUIRED_FIELDS = ["name", "rationale", "confidence"];

/**
 * Build a schema-compliant example label object from the effective item schema.
 * Falls back to the default intent fields when no schema is available.
 * @param {string[]} required
 * @param {any} itemSchema
 * @returns {string}
 */
function buildLabelExample(required, itemSchema) {
  const schema = isPlainObject(itemSchema) ? itemSchema : {};
  const properties = isPlainObject(schema.properties) ? schema.properties : {};
  /** @type {Record<string, any>} */
  const example = {};
  for (const field of required) {
    example[field] = exampleValueForProperty(field, properties[field]);
  }
  return JSON.stringify(example);
}

function formatSchemaValidationError(toolName, args, error, inputSchema) {
  if (toolName === "add_labels" && typeof error?.path === "string" && /^labels\[\d+\]$/.test(error.path) && Array.isArray(args?.labels)) {
    const index = Number(error.path.match(/^labels\[(\d+)\]$/)?.[1] || -1);
    const receivedLabel = index >= 0 ? args.labels[index] : undefined;
    if (typeof receivedLabel === "string") {
      const itemSchema = inputSchema?.properties?.labels?.items;
      const objectSchema = Array.isArray(itemSchema?.oneOf) ? itemSchema.oneOf.find(/** @param {any} option */ option => option?.type === "object") : itemSchema;
      const schemaRequired = objectSchema?.required;
      const requiredFields = Array.isArray(schemaRequired) && schemaRequired.length > 0 ? schemaRequired : DEFAULT_LABEL_REQUIRED_FIELDS;
      return [
        "Invalid arguments for add_labels:",
        `  ${error.path} must be an object (string shorthand is not supported).`,
        `  Expected: ${buildLabelExample(requiredFields, objectSchema)}`,
        `  Required fields: ${requiredFields.join(", ")}`,
        `  Received: ${JSON.stringify(receivedLabel)}`,
      ].join("\n");
    }
  }

  const path = error?.path || "(root)";
  const details = [`Invalid arguments for ${toolName}:`, `  ${path} ${error?.message || "is invalid"}.`];
  if (error?.expected) {
    details.push(`  Expected: ${error.expected}`);
  }
  if (error?.received) {
    details.push(`  Received: ${error.received}`);
  }
  return details.join("\n");
}

module.exports = {
  validateRequiredFields,
  validateStringInputLengths,
  buildStringLengthValidationError,
  validateStringMinLengths,
  validateArgumentsAgainstSchema,
  validateValueAgainstSchema,
  formatSchemaValidationError,
  MAX_STRING_INPUT_BYTES,
};
