// @ts-check
"use strict";

const { canonicalJSON } = require("./ledger_store.cjs");
const { validateValueAgainstSchema } = require("./mcp_scripts_validation.cjs");

const OPERATIONS = Object.freeze({
  log: ["append"],
  set: ["add", "remove"],
  map: ["put", "delete"],
  table: ["insert", "update", "upsert", "delete"],
  counter: ["increment", "decrement"],
});
const MAX_REPLAY_CELL_BYTES = 65536;

function checkSchema(value, schema) {
  if (!schema) return;
  const error = validateValueAgainstSchema(value, schema);
  if (error) throw new TypeError(`Ledger value does not match schema: ${error.path || "(root)"} ${error.message || "is invalid"}`);
}

function validateOperation(record, config) {
  const { operation } = record;
  if (!Object.hasOwn(OPERATIONS, config.type) || !OPERATIONS[config.type].includes(operation)) throw new TypeError("Unsupported ledger operation");
  const fields = {
    append: ["value"],
    add: ["value"],
    remove: ["value"],
    put: ["key", "value"],
    delete: ["key"],
    insert: ["value"],
    update: ["key", "patch"],
    upsert: ["value"],
    increment: ["name", "amount"],
    decrement: ["name", "amount"],
  }[operation];
  const expected = new Set(["operation", ...fields, ...(record.id === undefined ? [] : ["id"])]);
  if (Object.keys(record).some(key => !expected.has(key)) || fields.some(key => !Object.hasOwn(record, key))) throw new TypeError("Invalid ledger operation fields");
  if (config.type === "set" || config.type === "log" || operation === "put") {
    canonicalJSON(record.value);
    checkSchema(record.value, config.schema);
  }
  if (config.type === "map" || (config.type === "table" && (operation === "delete" || operation === "update"))) {
    if (typeof record.key !== "string") throw new TypeError("Ledger key must be a string");
  }
  if (config.type === "table") {
    if (operation === "insert" || operation === "upsert") {
      if (!record.value || typeof record.value !== "object" || Array.isArray(record.value) || typeof record.value[config.key] !== "string") throw new TypeError("Table row must contain a string primary key");
      canonicalJSON(record.value);
      checkSchema(record.value, config.schema);
    }
    if (operation === "update") {
      if (!record.patch || typeof record.patch !== "object" || Array.isArray(record.patch) || Object.hasOwn(record.patch, config.key)) throw new TypeError("Table patch must be an object without the primary key");
      canonicalJSON(record.patch);
    }
  }
  if (config.type === "counter") {
    if (typeof record.name !== "string" || !record.name || typeof record.amount !== "number" || !Number.isSafeInteger(record.amount) || record.amount < 0) throw new TypeError("Counter requires a nonnegative safe integer amount and a name");
  }
}

function createReducer(config) {
  if (!Object.hasOwn(OPERATIONS, config.type)) throw new TypeError("Unknown built-in ledger type");
  const sequence = [];
  const state = new Map();
  function apply(record) {
    validateOperation(record, config);
    const { operation, value, key } = record;
    switch (config.type) {
      case "log":
        sequence.push(value);
        break;
      case "set": {
        const identity = canonicalJSON(value);
        if (operation === "add") state.set(identity, value);
        else state.delete(identity);
        break;
      }
      case "map":
        if (operation === "put") state.set(key, value);
        else state.delete(key);
        break;
      case "table": {
        const primary = operation === "insert" || operation === "upsert" ? value[config.key] : key;
        if (operation === "insert" && state.has(primary)) throw new TypeError("Table key already exists");
        if (operation === "update" && !state.has(primary)) throw new TypeError("Table key does not exist");
        if (operation === "delete") state.delete(primary);
        else {
          const row = operation === "update" ? { ...state.get(primary), ...record.patch } : value;
          checkSchema(row, config.schema);
          if (Buffer.byteLength(canonicalJSON(row)) > MAX_REPLAY_CELL_BYTES) throw new RangeError("Replay cell exceeds size limit");
          state.set(primary, row);
        }
        break;
      }
      case "counter": {
        const old = state.get(record.name) || 0;
        const next = old + (operation === "increment" ? record.amount : -record.amount);
        if (!Number.isSafeInteger(next)) throw new RangeError("Counter exceeds safe integer range");
        state.set(record.name, next);
        break;
      }
    }
  }
  function output() {
    const rows =
      config.type === "log"
        ? sequence.map((value, index) => ({ position: index, value }))
        : [...state.entries()]
            .sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))
            .map(([key, value]) => {
              if (config.type === "set") return { identity: key, value };
              if (config.type === "counter") return { name: key, value };
              return { key, value };
            });
    const columns = {
      log: { position: "integer", value: "json" },
      set: { identity: "text", value: "json" },
      map: { key: "text", value: "json" },
      table: { key: "text", value: "json" },
      counter: { name: "text", value: "integer" },
    }[config.type];
    // Replay JSON cells require objects; scalar JSON values are represented in
    // their canonical JSON text, avoiding SQLite affinity-dependent conversion.
    if (config.type !== "counter") {
      columns.value = "text";
      for (const row of rows) row.value = canonicalJSON(row.value);
    }
    return { version: 1, tables: { state: { columns, primaryKey: [Object.keys(columns)[0]], rows } } };
  }
  return { apply, output };
}

function replayBuiltin(config, records) {
  const reducer = createReducer(config);
  for (const record of records) reducer.apply(record.payload);
  return reducer.output();
}

module.exports = { OPERATIONS, createReducer, replayBuiltin, validateOperation };
