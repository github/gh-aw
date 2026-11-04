// @ts-check
"use strict";

const MAX_OTLP_RESPONSE_BYTES = 64 * 1024;
const MAX_INT64 = (1n << 63n) - 1n;

/**
 * Read only a bounded response. Collector text is private, untrusted data and
 * must never be included in an exception or diagnostic.
 * @param {ReadableStream<Uint8Array> | null | undefined} body
 * @returns {Promise<Buffer>}
 */
async function readOTLPResponse(body) {
  if (!body) return Buffer.alloc(0);
  const reader = body.getReader();
  const chunks = [];
  let length = 0;
  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) return Buffer.concat(chunks);
      length += value.length;
      if (length > MAX_OTLP_RESPONSE_BYTES) throw new Error("OTLP response exceeds size limit");
      chunks.push(Buffer.from(value));
    }
  } finally {
    await reader.cancel();
    reader.releaseLock();
  }
}

/**
 * Visit bounded protobuf fields without interpreting or exposing unknown data.
 * @param {Buffer} bytes
 * @param {(id: number, wire: number, value: bigint | Buffer) => void} visit
 */
function visitFields(bytes, visit) {
  let offset = 0;
  function readVarint() {
    let value = 0n;
    for (let i = 0; i < 10 && offset < bytes.length; i++) {
      const byte = bytes[offset++];
      if (i === 9 && byte > 1) break;
      value |= BigInt(byte & 127) << BigInt(i * 7);
      if (!(byte & 128)) return value;
    }
    throw new Error("Invalid protobuf varint");
  }
  while (offset < bytes.length) {
    const tag = readVarint();
    if (tag < 8n || tag > 0xffffffffn) throw new Error("Invalid protobuf tag");
    const id = Number(tag >> 3n);
    const wire = Number(tag & 7n);
    if (wire === 0) {
      visit(id, wire, readVarint());
    } else {
      const length = wire === 2 ? Number(readVarint()) : wire === 1 ? 8 : wire === 5 ? 4 : -1;
      if (!Number.isSafeInteger(length) || length < 0 || length > bytes.length - offset) throw new Error("Invalid protobuf field");
      visit(id, wire, bytes.subarray(offset, offset + length));
      offset += length;
    }
  }
}

/**
 * Trace and logs partial-success messages share field numbers: rejected count
 * (int64, field 1) and error message (string, field 2).
 * @param {Buffer} bytes
 * @returns {{ rejected: bigint, warning: boolean }}
 */
function decodePartialSuccess(bytes) {
  let rejected = 0n;
  let warning = false;
  let seen = false;
  visitFields(bytes, (id, wire, value) => {
    if (id === 1) {
      if (seen || wire !== 2 || !Buffer.isBuffer(value)) throw new Error("Invalid partial success");
      seen = true;
      const fields = new Set();
      visitFields(value, (field, type, item) => {
        if (field !== 1 && field !== 2) return;
        if (fields.has(field)) throw new Error("Duplicate partial success field");
        fields.add(field);
        if (field === 1) {
          if (type !== 0 || typeof item !== "bigint" || item > MAX_INT64) throw new Error("Invalid rejection count");
          rejected = item;
        } else {
          if (type !== 2 || !Buffer.isBuffer(item)) throw new Error("Invalid collector message");
          warning = item.length > 0;
        }
      });
    }
  });
  return { rejected, warning };
}

/**
 * @param {"traces" | "logs"} signal
 * @returns {{ reason: string, error: boolean }}
 */
function unavailableOTLPResponse(signal) {
  return { reason: `OTLP ${signal} response diagnostics unavailable; acceptance unknown. Check collector response format and limits; request will not be retried.`, error: true };
}

/**
 * @param {Buffer} bytes
 * @param {"traces" | "logs"} signal
 * @returns {{ rejected: bigint, warning: boolean }}
 */
function parseJSONResponse(bytes, signal) {
  const response = JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(bytes));
  if (!response || typeof response !== "object" || Array.isArray(response)) throw new Error("Invalid response");
  if ("partialSuccess" in response && "partial_success" in response) throw new Error("Duplicate partial success");
  const partial = "partialSuccess" in response ? response.partialSuccess : response.partial_success;
  if (partial === undefined || partial === null) return { rejected: 0n, warning: false };
  if (typeof partial !== "object" || Array.isArray(partial)) throw new Error("Invalid partial success");
  const countKey = signal === "logs" ? "rejectedLogRecords" : "rejectedSpans";
  const snakeKey = signal === "logs" ? "rejected_log_records" : "rejected_spans";
  if ((countKey in partial && snakeKey in partial) || ("errorMessage" in partial && "error_message" in partial)) throw new Error("Duplicate partial success field");
  const count = (countKey in partial ? partial[countKey] : partial[snakeKey]) ?? 0;
  if ((typeof count !== "number" || !Number.isSafeInteger(count)) && (typeof count !== "string" || !/^\d{1,19}$/.test(count))) throw new Error("Invalid rejection count");
  const rejected = BigInt(count);
  if (rejected < 0n || rejected > MAX_INT64) throw new Error("Invalid rejection count");
  const message = ("errorMessage" in partial ? partial.errorMessage : partial.error_message) ?? "";
  if (typeof message !== "string") throw new Error("Invalid collector message");
  return { rejected, warning: message.length > 0 };
}

/**
 * Only locally constructed text leaves this parser. Never echo collector
 * messages, even after URL redaction: they can contain arbitrary credentials.
 * @param {Buffer} bytes
 * @param {string} contentType
 * @param {string} protocol
 * @param {"traces" | "logs"} signal
 * @returns {{ reason: string, error: boolean }}
 */
function inspectOTLPResponse(bytes, contentType, protocol, signal) {
  const unknown = unavailableOTLPResponse(signal);
  try {
    if (bytes.length > MAX_OTLP_RESPONSE_BYTES) return unknown;
    if (!bytes.length) return { reason: "", error: false };
    const type = contentType.split(";")[0].trim().toLowerCase() || (protocol === "http/protobuf" ? "application/x-protobuf" : "application/json");
    const result = type === "application/json" ? parseJSONResponse(bytes, signal) : type === "application/x-protobuf" ? decodePartialSuccess(bytes) : null;
    if (!result) return unknown;
    if (result.rejected > 0n) {
      const count = signal === "logs" ? "rejected_log_records" : "rejected_spans";
      return { reason: `OTLP ${signal} partial rejection: ${count}=${result.rejected}. Check collector limits and signal schema; request will not be retried.`, error: true };
    }
    return { reason: result.warning ? `OTLP ${signal} collector warning (message withheld). Check collector diagnostics; request will not be retried.` : "", error: false };
  } catch {
    return unknown;
  }
}

module.exports = { MAX_OTLP_RESPONSE_BYTES, readOTLPResponse, inspectOTLPResponse, unavailableOTLPResponse };
