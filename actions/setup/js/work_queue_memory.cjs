// @ts-check
"use strict";
const log = require("./work_queue_logging.cjs").createWorkQueueLogger("memory");

const fs = require("node:fs");
const path = require("node:path");
const { closed, integer, parseStrictJSON, validString } = require("./work_queue_codec.cjs");
const { normalizeAssignment, normalizeClaimScope } = require("./work_queue_claim_scope.cjs");

const TYPES = new Set(["object", "array", "string", "number", "integer", "boolean", "null"]);
const KEYWORDS = ["type", "description", "properties", "required", "additionalProperties", "items", "enum", "minimum", "maximum", "minLength", "maxLength", "minItems", "maxItems"];

function validateMemorySchema(schema, depth = 0) {
  if (depth > 16) throw new Error("Memory schema exceeds 16 nested schemas");
  closed(
    schema,
    ["type"],
    KEYWORDS.filter(key => key !== "type"),
    "declarative memory schema"
  );
  if (!TYPES.has(schema.type)) throw new Error("Memory schema requires one supported type");
  if (schema.description !== undefined && typeof schema.description !== "string") throw new Error("Memory schema description must be a string");
  if (schema.additionalProperties !== undefined && typeof schema.additionalProperties !== "boolean") throw new Error("Memory additionalProperties must be boolean");
  if (schema.properties !== undefined) {
    if (!schema.properties || typeof schema.properties !== "object" || Array.isArray(schema.properties)) throw new Error("Memory properties must be an object");
    for (const child of Object.values(schema.properties)) validateMemorySchema(child, depth + 1);
  }
  if (schema.items !== undefined) validateMemorySchema(schema.items, depth + 1);
  if (schema.required !== undefined && (!Array.isArray(schema.required) || schema.required.some(key => typeof key !== "string") || new Set(schema.required).size !== schema.required.length))
    throw new Error("Memory required must contain unique property names");
  for (const key of ["minLength", "maxLength", "minItems", "maxItems"]) {
    if (schema[key] !== undefined && (typeof schema[key] !== "number" || !Number.isInteger(schema[key]) || schema[key] < 0)) throw new Error("Memory length bounds must be nonnegative integers");
  }
  for (const key of ["minimum", "maximum"]) if (schema[key] !== undefined && (typeof schema[key] !== "number" || !Number.isFinite(schema[key]))) throw new Error("Memory numeric bounds must be finite numbers");
  if (schema.enum !== undefined && (!Array.isArray(schema.enum) || !schema.enum.length || schema.enum.some(value => value !== null && !["string", "number", "boolean"].includes(typeof value))))
    throw new Error("Memory enum requires scalar choices");
  if (schema.enum) {
    schema.enum.forEach(value => {
      if (typeof value === "string") validString(value);
      if (typeof value === "number" && !Number.isFinite(value)) throw new Error("Memory enum numbers must be finite");
    });
    if (new Set(schema.enum).size !== schema.enum.length) throw new Error("Memory enum choices must be unique");
  }
}

function assertJSONData(value, depth = 0) {
  if (depth > 32) throw new Error("Memory data exceeds 32 nested levels");
  if (value === null || typeof value === "boolean") return;
  if (typeof value === "string") {
    validString(value);
    return;
  }
  if (typeof value === "number") {
    if (!Number.isSafeInteger(value) || Object.is(value, -0)) throw new Error("Memory numbers must be safe integers; encode fractional or exact quantities as strings");
    return;
  }
  if (Array.isArray(value)) {
    if (value.length > 16384) throw new Error("Memory arrays exceed 16384 members");
    if (Object.getPrototypeOf(value) !== Array.prototype || Reflect.ownKeys(value).length !== value.length + 1) throw new Error("Memory arrays must contain JSON members only");
    for (let index = 0; index < value.length; index++) {
      if (!Object.hasOwn(value, index)) throw new Error("Memory data cannot contain sparse arrays");
      const descriptor = Object.getOwnPropertyDescriptor(value, String(index));
      if (!descriptor || !descriptor.enumerable || !Object.hasOwn(descriptor, "value")) throw new Error("Memory cannot contain accessors or hidden fields");
      assertJSONData(descriptor.value, depth + 1);
    }
    return;
  }
  if (typeof value !== "object" || (Object.getPrototypeOf(value) !== Object.prototype && Object.getPrototypeOf(value) !== null)) throw new Error("Memory must contain finite JSON data only");
  for (const key of Reflect.ownKeys(value)) {
    if (typeof key !== "string") throw new Error("Memory cannot contain symbol keys");
    validString(key);
    const descriptor = Object.getOwnPropertyDescriptor(value, key);
    if (!descriptor || !descriptor.enumerable || !Object.hasOwn(descriptor, "value")) throw new Error("Memory cannot contain accessors or hidden fields");
    assertJSONData(descriptor.value, depth + 1);
  }
}

function validateMemoryValue(value, schema, location = "memory") {
  const fail = reason => {
    throw new Error(`${location}: ${reason}`);
  };
  const object = value !== null && typeof value === "object" && !Array.isArray(value);
  const valid =
    schema.type === "object"
      ? object
      : schema.type === "array"
        ? Array.isArray(value)
        : schema.type === "integer"
          ? typeof value === "number" && Number.isInteger(value)
          : schema.type === "null"
            ? value === null
            : typeof value === schema.type;
  if (!valid) fail(`must have type ${schema.type}`);
  if (schema.enum !== undefined && !schema.enum.some(choice => choice === value)) fail("is not a configured enum choice");
  if (typeof value === "number") {
    if (schema.minimum !== undefined && value < schema.minimum) fail("is below minimum");
    if (schema.maximum !== undefined && value > schema.maximum) fail("exceeds maximum");
  }
  if (typeof value === "string") {
    const length = [...value].length;
    if (schema.minLength !== undefined && length < schema.minLength) fail("is shorter than minLength");
    if (schema.maxLength !== undefined && length > schema.maxLength) fail("exceeds maxLength");
  }
  if (Array.isArray(value)) {
    if (schema.minItems !== undefined && value.length < schema.minItems) fail("has fewer than minItems");
    if (schema.maxItems !== undefined && value.length > schema.maxItems) fail("exceeds maxItems");
    if (schema.items) value.forEach((child, index) => validateMemoryValue(child, schema.items, `${location}[${index}]`));
  }
  if (object) {
    for (const key of schema.required || []) if (!Object.hasOwn(value, key)) fail(`requires ${key}`);
    for (const [key, child] of Object.entries(value)) {
      if (schema.properties && Object.hasOwn(schema.properties, key)) validateMemoryValue(child, schema.properties[key], `${location}.${key}`);
      else if (schema.additionalProperties === false) fail("does not allow undeclared properties");
    }
  }
}

/**
 * @param {Record<string, unknown>} message
 * @param {{path: string, schema: Record<string, unknown>, max_bytes: number}} config
 * @param {{assignment?: unknown}} [options]
 */
function prepareMemorySnapshot(message, config, options = {}) {
  log.debug("memory.prepare.start");
  closed(config, ["path", "schema", "max_bytes"], [], "declarative memory configuration");
  integer(config.max_bytes, 1, 262144, "memory max_bytes");
  if (
    typeof config.path !== "string" ||
    Buffer.byteLength(config.path) > 256 ||
    path.posix.isAbsolute(config.path) ||
    path.posix.normalize(config.path) !== config.path ||
    config.path.startsWith("../") ||
    /[\\\u0000-\u001f\u007f]/.test(config.path) ||
    config.path.split("/").some(component => component.toLowerCase() === ".git") ||
    !config.path.endsWith(".json")
  )
    throw new Error("Memory requires a fixed relative JSON path");
  validateMemorySchema(config.schema);
  if (config.schema.type !== "object" || Buffer.byteLength(JSON.stringify(config.schema)) > 16384) throw new Error("Memory requires a bounded object schema");
  const assignmentFile = process.env.GH_AW_CLAIM_ASSIGNMENT;
  if (options.assignment === undefined && !assignmentFile) throw new Error("Memory preparation requires the compiler-produced original Claim assignment");
  let original = options.assignment;
  if (original === undefined) {
    if (!assignmentFile) throw new Error("Memory preparation requires the compiler-produced original Claim assignment");
    original = readMemoryAssignment(assignmentFile);
  }
  const assignment = normalizeAssignment(original);
  const normalized = normalizeClaimScope(message, assignment);
  const member = assignment.claims.find(claim => claim.handle === normalized.claim_handle);
  const memory = normalized.memory;
  assertJSONData(memory);
  validateMemoryValue(memory, config.schema);
  if (Object.hasOwn(memory, "work_id") && memory.work_id !== member.work_id) throw new Error("Memory work_id conflicts with its original Claim");
  const content = JSON.stringify(memory) + "\n";
  if (Buffer.byteLength(content, "utf8") > config.max_bytes) throw new Error("Memory snapshot exceeds max-bytes");
  log.debug("memory.prepare.complete", { bytes: Buffer.byteLength(content, "utf8") });
  return { files: [{ path: config.path, content }] };
}

function readMemoryAssignment(filename) {
  let content;
  try {
    const stat = fs.lstatSync(filename);
    if (!stat.isFile() || stat.nlink !== 1 || stat.size > 48 * 1024 + 1) throw new Error("Original assignment is not a bounded regular file");
    content = fs.readFileSync(filename);
  } catch (error) {
    throw new Error("Memory preparation requires a readable bounded regular original assignment file", { cause: error });
  }
  return parseStrictJSON(new TextDecoder("utf-8", { fatal: true }).decode(content), { maxBytes: 48 * 1024 + 1 });
}

module.exports = { prepareMemorySnapshot, validateMemorySchema, validateMemoryValue };
