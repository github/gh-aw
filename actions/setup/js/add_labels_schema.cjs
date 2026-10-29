"use strict";

const ADD_LABELS_OBJECT_ONLY_FIELD_DESC = "Labels to add. Each label must be an object matching the configured item schema; plain string label names are not permitted. Labels must exist in the repository.";

const OBJECT_SCHEMA_KEYS = new Set(["type", "properties", "required", "additionalProperties", "title", "description", "$comment"]);
const PROPERTY_SCHEMA_KEYS = new Set(["type", "enum", "const", "title", "description", "$comment", "default", "examples"]);

/**
 * @param {unknown} value
 * @returns {value is Record<string, unknown>}
 */
function isRecord(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

/**
 * @param {unknown} value
 * @param {unknown} type
 * @returns {boolean}
 */
function matchesType(value, type) {
  if (type === undefined) return true;
  const types = Array.isArray(type) ? type : [type];
  return types.some(candidate => {
    switch (candidate) {
      case "array":
        return Array.isArray(value);
      case "object":
        return isRecord(value);
      case "integer":
        return typeof value === "number" && Number.isInteger(value);
      case "number":
        return typeof value === "number" && Number.isFinite(value);
      case "null":
        return value === null;
      case "boolean":
        return typeof value === "boolean";
      case "string":
        return typeof value === "string";
      default:
        return false;
    }
  });
}

/**
 * @param {string} fieldName
 * @param {unknown} schema
 * @returns {{value: unknown} | null}
 */
function buildAddLabelsExampleValue(fieldName, schema) {
  if (!isRecord(schema) || Object.keys(schema).some(key => !PROPERTY_SCHEMA_KEYS.has(key))) {
    return null;
  }

  const type = schema.type;
  let value;
  if (Object.prototype.hasOwnProperty.call(schema, "const")) {
    value = schema.const;
    if (Array.isArray(schema.enum) && !schema.enum.some(candidate => Object.is(candidate, value))) {
      return null;
    }
  } else if (Array.isArray(schema.enum)) {
    value = schema.enum.find(candidate => matchesType(candidate, type));
    if (value === undefined) return null;
  } else {
    switch (type) {
      case "boolean":
        value = true;
        break;
      case "integer":
      case "number":
        value = 0;
        break;
      case "string":
        value = fieldName === "name" ? "bug" : fieldName === "rationale" ? "Why this label applies" : "value";
        break;
      default:
        return null;
    }
  }

  return matchesType(value, type) ? { value } : null;
}

/**
 * @param {unknown} itemSchema
 * @returns {Record<string, unknown> | null}
 */
function getAddLabelsObjectSchema(itemSchema) {
  if (!isRecord(itemSchema)) return null;
  if (itemSchema.type === "object") return itemSchema;
  if (Array.isArray(itemSchema.oneOf)) {
    return itemSchema.oneOf.find(schema => isRecord(schema) && schema.type === "object") ?? null;
  }
  return null;
}

/**
 * Build the schema-derived required fields and an example only when all required values
 * can be inferred without violating declared schema constraints.
 * @param {unknown} itemSchema
 * @returns {{required: string[], fields: string[], example: string | null} | null}
 */
function buildAddLabelsObjectShape(itemSchema) {
  const objectSchema = getAddLabelsObjectSchema(itemSchema);
  if (!objectSchema) return null;

  const schemaKeys = Object.keys(objectSchema);
  const safeObjectSchema = schemaKeys.every(key => OBJECT_SCHEMA_KEYS.has(key));
  const properties = isRecord(objectSchema.properties) ? objectSchema.properties : {};
  const required = Array.isArray(objectSchema.required) ? objectSchema.required.filter(field => typeof field === "string") : [];
  /** @type {string | null} */
  let example = null;

  if (safeObjectSchema && required.length > 0) {
    const exampleValue = Object.create(null);
    let canBuildExample = true;
    for (const field of required) {
      if (!Object.prototype.hasOwnProperty.call(properties, field)) {
        canBuildExample = false;
        break;
      }
      const fieldValue = buildAddLabelsExampleValue(field, properties[field]);
      if (!fieldValue) {
        canBuildExample = false;
        break;
      }
      exampleValue[field] = fieldValue.value;
    }
    if (canBuildExample) example = JSON.stringify(exampleValue);
  }

  return {
    required,
    fields: Object.keys(properties),
    example,
  };
}

/**
 * Build accurate object-only guidance for the generated add_labels labels field.
 * @param {unknown} itemSchema
 * @returns {string}
 */
function buildAddLabelsObjectOnlyFieldDesc(itemSchema) {
  const shape = buildAddLabelsObjectShape(itemSchema);
  if (!shape) return ADD_LABELS_OBJECT_ONLY_FIELD_DESC;

  const guidance = [];
  if (shape.required.length > 0) {
    guidance.push(`Required fields: ${shape.required.join(", ")}.`);
  } else {
    guidance.push("No required fields are declared by the configured item schema.");
    if (shape.fields.length > 0) {
      guidance.push(`Available fields: ${shape.fields.join(", ")}.`);
    }
  }
  if (shape.example) {
    guidance.push(`Example: [${shape.example}].`);
  }

  return `Labels to add. Each label must be an object matching the configured item schema; plain string label names are not permitted. ${guidance.join(" ")} Labels must exist in the repository.`;
}

module.exports = {
  buildAddLabelsObjectOnlyFieldDesc,
  buildAddLabelsObjectShape,
};
