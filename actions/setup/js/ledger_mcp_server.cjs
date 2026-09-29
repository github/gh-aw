// @ts-check
"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { createServer, registerTool, start } = require("./mcp_server_core.cjs");
const { Ledger } = require("./ledger_store.cjs");
const { LEDGER_TRANSACTION_LOG_PATH } = require("./constants.cjs");

function parsePositiveInteger(value) {
  if (value === undefined || !/^[1-9][0-9]*$/.test(value)) return undefined;
  const parsed = Number(value);
  return Number.isSafeInteger(parsed) ? parsed : undefined;
}

function parseKilobytes(value) {
  const parsed = parsePositiveInteger(value);
  return parsed === undefined ? undefined : parsed * 1024;
}

function result(value) {
  return { content: [{ type: "text", text: JSON.stringify(value === undefined ? null : value) }] };
}

function toolHandler(operation) {
  return args => {
    try {
      return result(operation(args));
    } catch (error) {
      // Never return filesystem paths, raw records, or runtime exception details.
      let message = "Ledger operation failed. Check ledger_status for diagnostics.";
      if (error instanceof RangeError && /message size/i.test(error.message)) {
        message = "Ledger append exceeds the configured record-size limit.";
      } else if (error instanceof RangeError && /patch size/i.test(error.message)) {
        message = "Ledger append exceeds the configured patch-size limit.";
      } else if (error instanceof RangeError && /file-count/i.test(error.message)) {
        message = "Ledger file-count limit reached; no record was appended.";
      } else if (error instanceof TypeError || error instanceof RangeError) {
        message = "Invalid ledger arguments or ledger limit exceeded.";
      }
      return {
        isError: true,
        content: [{ type: "text", text: message }],
      };
    }
  };
}

function resolveSchemaPath(schemaPath, workspace) {
  if (schemaPath === undefined || schemaPath === "") return undefined;
  if (typeof schemaPath !== "string" || !workspace || path.isAbsolute(schemaPath) || fs.lstatSync(workspace).isSymbolicLink()) throw new TypeError("Invalid ledger schema configuration");
  const segments = schemaPath.split("/");
  if (segments.some(segment => !segment || segment === "." || segment === ".." || segment.includes("\\"))) throw new TypeError("Invalid ledger schema configuration");
  const base = fs.realpathSync(workspace);
  let file = base;
  for (const segment of segments) {
    file = path.join(file, segment);
    if (fs.lstatSync(file).isSymbolicLink()) throw new TypeError("Invalid ledger schema configuration");
  }
  file = fs.realpathSync(file);
  if (!file.startsWith(base + path.sep) || !fs.statSync(file).isFile()) throw new TypeError("Invalid ledger schema configuration");
  return file;
}

function createLedgerServer({ memoryDir = process.env.GH_AW_MEMORY_DIR, schemaPath = process.env.GH_AW_LEDGER_SCHEMA, workspace = process.env.GITHUB_WORKSPACE, schemaRoot = process.env.GH_AW_LEDGER_SCHEMA_ROOT || workspace } = {}) {
  const ledger = new Ledger({
    memoryDir,
    schemaPath: resolveSchemaPath(schemaPath, schemaRoot),
    maxFiles: parsePositiveInteger(process.env.GH_AW_LEDGER_MAX_SHARDS),
    maxSegmentBytes: parseKilobytes(process.env.GH_AW_LEDGER_MAX_SEGMENT_KB),
    maxRecordBytes: parseKilobytes(process.env.GH_AW_LEDGER_MAX_RECORD_KB),
    maxPatchBytes: parseKilobytes(process.env.GH_AW_LEDGER_MAX_PATCH_KB),
    transactionLogPath: process.env.GH_AW_LEDGER_TRANSACTION_LOG || LEDGER_TRANSACTION_LOG_PATH,
  });
  const server = createServer({ name: "ledger", version: "1.0.0" });

  registerTool(server, {
    name: "ledger_append",
    description: "Append one immutable structured record to repository ledger memory.",
    inputSchema: {
      type: "object",
      properties: {
        type: { type: "string", minLength: 1, maxLength: 128 },
        payload: {},
      },
      required: ["type", "payload"],
      additionalProperties: false,
    },
    handler: toolHandler(({ type, payload }) => ledger.append(type, payload)),
  });

  registerTool(server, {
    name: "ledger_get",
    description: "Retrieve one immutable ledger record by ID or SHA-256 hash.",
    inputSchema: {
      type: "object",
      properties: { id: { type: "string", minLength: 1, maxLength: 128 }, sha: { type: "string", minLength: 1, maxLength: 128 } },
      minProperties: 1,
      maxProperties: 1,
      additionalProperties: false,
    },
    handler: toolHandler(({ id, sha }) => {
      if (Boolean(id) === Boolean(sha)) throw new TypeError("Specify exactly one record ID or SHA");
      return ledger.get(id || sha);
    }),
  });

  registerTool(server, {
    name: "ledger_query",
    description: "Query ledger records by type and structured payload filters (eq, in, exists, prefix, gt, gte, lt, lte), without exposing SQL or storage paths.",
    inputSchema: {
      type: "object",
      properties: {
        type: { type: "string", minLength: 1, maxLength: 128 },
        where: {
          type: "object",
          description: "Map payload paths such as payload.foo to one predicate: eq, in, exists, prefix, gt, gte, lt, or lte.",
          maxProperties: 8,
          additionalProperties: {
            type: "object",
            properties: { eq: {}, in: { type: "array" }, exists: { type: "boolean" }, prefix: { type: "string" }, gt: {}, gte: {}, lt: {}, lte: {} },
            minProperties: 1,
            maxProperties: 1,
            additionalProperties: false,
          },
        },
        limit: { type: "integer", minimum: 1, maximum: 500 },
        after: { type: "string", pattern: "^sha256:[a-f0-9]{64}$" },
      },
      additionalProperties: false,
    },
    handler: toolHandler(({ type, where, limit, after }) => ledger.query({ type, where, limit, after })),
  });

  registerTool(server, {
    name: "ledger_status",
    description: "Inspect ledger status and diagnostics for malformed or incomplete records.",
    inputSchema: { type: "object", properties: {}, additionalProperties: false },
    handler: toolHandler(() => ledger.status()),
  });

  return server;
}

if (require.main === module) {
  try {
    start(createLedgerServer());
  } catch {
    console.error("Ledger MCP server could not start. Check ledger memory configuration.");
    process.exitCode = 1;
  }
}

module.exports = { createLedgerServer, resolveSchemaPath };
