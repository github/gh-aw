// @ts-check
"use strict";

/**
 * Minimal OTLP trace and logs request encoders. Field numbers and wire types
 * follow opentelemetry-proto; no generated code or runtime dependencies.
 *
 * @typedef {"string" | "bool" | "int64" | "uint32" | "fixed32" | "fixed64" | "double" | "traceId" | "spanId" | "bytes" | "message"} WireType
 * @typedef {{ id: number, name: string, type: WireType, repeated?: boolean, fields?: Field[] }} Field
 */

/** @param {bigint} value @returns {Buffer} */
function varint(value) {
  const bytes = [];
  do {
    let byte = Number(value & 127n);
    value >>= 7n;
    if (value) byte |= 128;
    bytes.push(byte);
  } while (value);
  return Buffer.from(bytes);
}

/**
 * Preserve nanosecond timestamps and int64 attributes without Number rounding.
 * @param {unknown} value
 * @param {bigint} min
 * @param {bigint} max
 * @returns {bigint}
 */
function integer(value, min, max) {
  if ((typeof value !== "string" || !/^-?\d+$/.test(value)) && (typeof value !== "number" || !Number.isSafeInteger(value)) && typeof value !== "bigint") {
    throw new TypeError("OTLP protobuf integer must be an exact decimal integer");
  }
  const result = BigInt(value);
  if (result < min || result > max) throw new RangeError("OTLP protobuf integer is out of range");
  return result;
}

/**
 * @param {Field} field
 * @param {any} value
 * @returns {Buffer}
 */
function encodeField(field, value) {
  let wire = 2;
  /** @type {Buffer} */
  let bytes;
  switch (field.type) {
    case "message":
      bytes = encodeMessage(value, field.fields || []);
      break;
    case "string":
      if (typeof value !== "string") throw new TypeError("OTLP protobuf string must be a string");
      bytes = Buffer.from(value, "utf8");
      break;
    case "traceId":
    case "spanId": {
      const length = field.type === "traceId" ? 32 : 16;
      if (typeof value !== "string" || (value !== "" && (value.length !== length || !/^[0-9a-f]+$/i.test(value)))) {
        throw new TypeError("OTLP protobuf trace/span ID must be correctly sized hexadecimal");
      }
      bytes = Buffer.from(value, "hex");
      break;
    }
    case "bytes":
      if (typeof value !== "string") throw new TypeError("OTLP protobuf bytesValue must be base64");
      bytes = Buffer.from(value, "base64");
      break;
    case "bool":
      if (typeof value !== "boolean") throw new TypeError("OTLP protobuf boolValue must be a boolean");
      wire = 0;
      bytes = varint(value ? 1n : 0n);
      break;
    case "int64":
      wire = 0;
      bytes = varint(BigInt.asUintN(64, integer(value, -(1n << 63n), (1n << 63n) - 1n)));
      break;
    case "uint32":
      wire = 0;
      bytes = varint(integer(value, 0n, 0xffffffffn));
      break;
    case "fixed32":
      wire = 5;
      bytes = Buffer.alloc(4);
      bytes.writeUInt32LE(Number(integer(value, 0n, 0xffffffffn)));
      break;
    case "fixed64":
      wire = 1;
      bytes = Buffer.alloc(8);
      bytes.writeBigUInt64LE(integer(value, 0n, (1n << 64n) - 1n));
      break;
    case "double":
      if (value === "NaN") value = NaN;
      else if (value === "Infinity") value = Infinity;
      else if (value === "-Infinity") value = -Infinity;
      if (typeof value !== "number") throw new TypeError("OTLP protobuf doubleValue must be a number");
      wire = 1;
      bytes = Buffer.alloc(8);
      bytes.writeDoubleLE(value);
      break;
    default:
      throw new TypeError("Unsupported OTLP protobuf wire type");
  }
  const tag = varint(BigInt((field.id << 3) | wire));
  return wire === 2 ? Buffer.concat([tag, varint(BigInt(bytes.length)), bytes]) : Buffer.concat([tag, bytes]);
}

/**
 * @param {any} value
 * @param {Field[]} fields
 * @returns {Buffer}
 */
function encodeMessage(value, fields) {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new TypeError("OTLP protobuf message must be an object");
  /** @type {Buffer[]} */
  const chunks = [];
  const known = new Set(fields.map(field => field.name));
  for (const key of Object.keys(value)) {
    if (!known.has(key)) throw new TypeError(`OTLP protobuf message has unknown field: ${key}`);
  }
  for (const field of fields) {
    const item = value[field.name];
    if (item === undefined || item === null) continue;
    if (field.repeated && !Array.isArray(item)) throw new TypeError("OTLP protobuf repeated field must be an array");
    for (const entry of field.repeated ? item : [item]) chunks.push(encodeField(field, entry));
  }
  return Buffer.concat(chunks);
}

/** @type {Field[]} */
const anyValue = [
  { id: 1, name: "stringValue", type: "string" },
  { id: 2, name: "boolValue", type: "bool" },
  { id: 3, name: "intValue", type: "int64" },
  { id: 4, name: "doubleValue", type: "double" },
  { id: 7, name: "bytesValue", type: "bytes" },
];
/** @type {Field[]} */
const keyValue = [
  { id: 1, name: "key", type: "string" },
  { id: 2, name: "value", type: "message", fields: anyValue },
];
anyValue.push(
  { id: 5, name: "arrayValue", type: "message", fields: [{ id: 1, name: "values", type: "message", repeated: true, fields: anyValue }] },
  { id: 6, name: "kvlistValue", type: "message", fields: [{ id: 1, name: "values", type: "message", repeated: true, fields: keyValue }] }
);
/** @type {Field} */
const attributes = { id: 1, name: "attributes", type: "message", repeated: true, fields: keyValue };
/** @type {Field} */
const resource = {
  id: 1,
  name: "resource",
  type: "message",
  fields: [attributes, { id: 2, name: "droppedAttributesCount", type: "uint32" }],
};
/** @type {Field} */
const scope = {
  id: 1,
  name: "scope",
  type: "message",
  fields: [
    { id: 1, name: "name", type: "string" },
    { id: 2, name: "version", type: "string" },
    { ...attributes, id: 3 },
    { id: 4, name: "droppedAttributesCount", type: "uint32" },
  ],
};
/** @type {Field[]} */
const span = [
  { id: 1, name: "traceId", type: "traceId" },
  { id: 2, name: "spanId", type: "spanId" },
  { id: 3, name: "traceState", type: "string" },
  { id: 4, name: "parentSpanId", type: "spanId" },
  { id: 5, name: "name", type: "string" },
  { id: 6, name: "kind", type: "uint32" },
  { id: 7, name: "startTimeUnixNano", type: "fixed64" },
  { id: 8, name: "endTimeUnixNano", type: "fixed64" },
  { ...attributes, id: 9 },
  { id: 10, name: "droppedAttributesCount", type: "uint32" },
  {
    id: 11,
    name: "events",
    type: "message",
    repeated: true,
    fields: [
      { id: 1, name: "timeUnixNano", type: "fixed64" },
      { id: 2, name: "name", type: "string" },
      { ...attributes, id: 3 },
      { id: 4, name: "droppedAttributesCount", type: "uint32" },
    ],
  },
  { id: 12, name: "droppedEventsCount", type: "uint32" },
  {
    id: 13,
    name: "links",
    type: "message",
    repeated: true,
    fields: [
      { id: 1, name: "traceId", type: "traceId" },
      { id: 2, name: "spanId", type: "spanId" },
      { id: 3, name: "traceState", type: "string" },
      { ...attributes, id: 4 },
      { id: 5, name: "droppedAttributesCount", type: "uint32" },
      { id: 6, name: "flags", type: "fixed32" },
    ],
  },
  { id: 14, name: "droppedLinksCount", type: "uint32" },
  {
    id: 15,
    name: "status",
    type: "message",
    fields: [
      { id: 2, name: "message", type: "string" },
      { id: 3, name: "code", type: "uint32" },
    ],
  },
  { id: 16, name: "flags", type: "fixed32" },
];
/** @type {Field[]} */
const request = [
  {
    id: 1,
    name: "resourceSpans",
    type: "message",
    repeated: true,
    fields: [
      resource,
      {
        id: 2,
        name: "scopeSpans",
        type: "message",
        repeated: true,
        fields: [scope, { id: 2, name: "spans", type: "message", repeated: true, fields: span }, { id: 3, name: "schemaUrl", type: "string" }],
      },
      { id: 3, name: "schemaUrl", type: "string" },
    ],
  },
];

/** @type {Field[]} */
const logRecord = [
  { id: 1, name: "timeUnixNano", type: "fixed64" },
  { id: 2, name: "severityNumber", type: "uint32" },
  { id: 3, name: "severityText", type: "string" },
  { id: 5, name: "body", type: "message", fields: anyValue },
  { ...attributes, id: 6 },
  { id: 7, name: "droppedAttributesCount", type: "uint32" },
  { id: 8, name: "flags", type: "fixed32" },
  { id: 9, name: "traceId", type: "traceId" },
  { id: 10, name: "spanId", type: "spanId" },
  { id: 11, name: "observedTimeUnixNano", type: "fixed64" },
  { id: 12, name: "eventName", type: "string" },
];
/** @type {Field[]} */
const logsRequest = [
  {
    id: 1,
    name: "resourceLogs",
    type: "message",
    repeated: true,
    fields: [
      resource,
      {
        id: 2,
        name: "scopeLogs",
        type: "message",
        repeated: true,
        fields: [scope, { id: 2, name: "logRecords", type: "message", repeated: true, fields: logRecord }, { id: 3, name: "schemaUrl", type: "string" }],
      },
      { id: 3, name: "schemaUrl", type: "string" },
    ],
  },
];

/**
 * Encode the same OTLP JSON trace object used by the local JSONL mirror.
 * OTLP JSON IDs are hex, bytesValue is base64, and 64-bit integers are decimal.
 * @param {any} payload
 * @returns {Buffer}
 */
function encodeOTLPTraceRequest(payload) {
  return encodeMessage(payload, request);
}

/**
 * Encode an ExportLogsServiceRequest using the same AnyValue wire types as traces.
 * @param {any} payload
 * @returns {Buffer}
 */
function encodeOTLPLogsRequest(payload) {
  return encodeMessage(payload, logsRequest);
}

module.exports = { encodeOTLPTraceRequest, encodeOTLPLogsRequest };
