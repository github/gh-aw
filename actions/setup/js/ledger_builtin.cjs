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
  claims: ["claim", "vote"],
});
const MAX_REPLAY_CELL_BYTES = 65536;
const CLAIM_LIMITS = { subject: 512, claim: 4096, reason: 4096, citations: 32, citationBytes: 2048 };
const CLAIM_STATE_COLUMNS = { claim_id: "text", upvotes: "integer", downvotes: "integer", net_votes: "integer", last_vote_at: "text", last_positive_vote_at: "text" };
const CLAIM_STATE_VIEW = `CREATE VIEW claim_state AS
  SELECT c.id AS claim_id,
    (SELECT count(*) FROM claim_votes v WHERE v.claim_id = c.id AND v.vote = 'up') AS upvotes,
    (SELECT count(*) FROM claim_votes v WHERE v.claim_id = c.id AND v.vote = 'down') AS downvotes,
    (SELECT count(*) FROM claim_votes v WHERE v.claim_id = c.id AND v.vote = 'up') -
    (SELECT count(*) FROM claim_votes v WHERE v.claim_id = c.id AND v.vote = 'down') AS net_votes,
    (SELECT max(v.created_at) FROM claim_votes v WHERE v.claim_id = c.id) AS last_vote_at,
    (SELECT max(v.created_at) FROM claim_votes v WHERE v.claim_id = c.id AND v.vote = 'up') AS last_positive_vote_at
  FROM claims c`;

function validateCitation(citation) {
  if (!citation || typeof citation !== "object" || Array.isArray(citation)) return false;
  switch (citation.type) {
    case "repository":
      return validateRepositoryCitation(citation);
    default:
      return false;
  }
}

function validateRepositoryCitation(citation) {
  if (Object.keys(citation).some(key => !["type", "path", "start_line", "end_line"].includes(key))) return false;
  if (typeof citation.path !== "string" || !citation.path.trim() || /[\\\u0000-\u001f\u007f]/.test(citation.path) || /^[A-Za-z][A-Za-z0-9+.-]*:/.test(citation.path) || /%(?:2e|2f|5c)/i.test(citation.path)) return false;
  if (citation.path.split("/").some(part => !part || part === "." || part === "..")) return false;
  if (citation.start_line !== undefined && (!Number.isSafeInteger(citation.start_line) || citation.start_line < 1)) return false;
  if (citation.end_line !== undefined && (!Number.isSafeInteger(citation.end_line) || citation.end_line < 1 || (citation.start_line !== undefined && citation.end_line < citation.start_line))) return false;
  return true;
}

function checkSchema(value, schema) {
  if (!schema) return;
  const error = validateValueAgainstSchema(value, schema);
  if (error) throw new TypeError(`Ledger value does not match schema: ${error.path || "(root)"} ${error.message || "is invalid"}`);
}

function validateOperation(record, config) {
  const { operation } = record;
  if (!Object.hasOwn(OPERATIONS, config.type) || !OPERATIONS[config.type].includes(operation)) throw new TypeError("Unsupported ledger operation");
  if (config.type === "claims") {
    const fields = operation === "claim" ? ["subject", "claim", "reason", "citations"] : ["claim_id", "vote"];
    const expected = new Set(["operation", ...fields, ...(operation === "vote" ? ["reason"] : []), ...(record.id === undefined ? [] : ["id"])]);
    if (Object.keys(record).some(key => !expected.has(key)) || fields.some(key => !Object.hasOwn(record, key))) throw new TypeError("Invalid ledger operation fields");
    if (record.id !== undefined && (typeof record.id !== "string" || !record.id || Buffer.byteLength(record.id) > 128)) throw new TypeError("Invalid claim record ID");
    if ((operation === "claim" || Object.hasOwn(record, "reason")) && (typeof record.reason !== "string" || !record.reason.trim() || Buffer.byteLength(record.reason) > CLAIM_LIMITS.reason))
      throw new TypeError("Claim reason must be a nonempty bounded string");
    if (operation === "claim") {
      if (
        typeof record.subject !== "string" ||
        !record.subject.trim() ||
        Buffer.byteLength(record.subject) > CLAIM_LIMITS.subject ||
        typeof record.claim !== "string" ||
        !record.claim.trim() ||
        Buffer.byteLength(record.claim) > CLAIM_LIMITS.claim
      )
        throw new TypeError("Claim subject and assertion must be nonempty bounded strings");
      if (!Array.isArray(record.citations) || !record.citations.length || record.citations.length > CLAIM_LIMITS.citations || record.citations.some(citation => !validateCitation(citation)))
        throw new TypeError("Claim citations must contain at least one valid citation object");
      for (const citation of record.citations) if (Buffer.byteLength(canonicalJSON(citation)) > CLAIM_LIMITS.citationBytes) throw new RangeError("Claim citation exceeds size limit");
    } else {
      if (typeof record.claim_id !== "string" || !record.claim_id || Buffer.byteLength(record.claim_id) > 128 || !["up", "down"].includes(record.vote)) throw new TypeError("Vote requires a bounded claim ID and an up or down vote");
    }
    return;
  }
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
  const claims = new Map();
  const votes = new Map();
  function apply(record, envelope) {
    validateOperation(record, config);
    const { operation, value, key } = record;
    switch (config.type) {
      case "claims": {
        const id = envelope?.id ?? record.id;
        if (typeof id !== "string" || !id) throw new TypeError("Claims projection requires a canonical record ID");
        const entry = { record, recordId: envelope?.id ?? null, timestamp: envelope?.timestamp ?? null, sha: envelope?.sha ?? null };
        if (operation === "claim") {
          if (claims.has(id) || votes.has(id)) throw new TypeError("Duplicate claim record ID");
          claims.set(id, entry);
        } else {
          if (!claims.has(record.claim_id)) throw new TypeError("Vote references a missing claim");
          if (votes.has(id) || claims.has(id)) throw new TypeError("Duplicate vote record ID");
          votes.set(id, entry);
        }
        break;
      }
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
    if (config.type === "claims") {
      const claimRows = [...claims].sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0));
      const voteRows = [...votes].sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0));
      const tables = {
        claims: {
          columns: { id: "text", subject: "text", claim: "text", reason: "text", created_at: "text", record_sha: "text" },
          primaryKey: ["id"],
          rows: claimRows.map(([id, { record, timestamp, sha }]) => ({ id, subject: record.subject, claim: record.claim, reason: record.reason, created_at: timestamp, record_sha: sha })),
        },
        claim_citations: {
          columns: { claim_id: "text", ordinal: "integer", citation_type: "text", path: "text", start_line: "integer", end_line: "integer" },
          primaryKey: ["claim_id", "ordinal"],
          rows: claimRows.flatMap(([claim_id, { record }]) =>
            record.citations.map((citation, ordinal) => ({
              claim_id,
              ordinal,
              citation_type: citation.type,
              path: citation.path,
              start_line: citation.start_line ?? null,
              end_line: citation.end_line ?? null,
            }))
          ),
        },
        claim_votes: {
          columns: { record_id: "text", claim_id: "text", vote: "text", reason: "text", created_at: "text" },
          primaryKey: ["record_id"],
          rows: voteRows.map(([, { record, recordId, timestamp }]) => ({ record_id: recordId, claim_id: record.claim_id, vote: record.vote, reason: record.reason ?? null, created_at: timestamp })),
        },
      };
      return { version: 1, tables };
    }
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
  if (config.type === "claims") {
    if (records.some(record => Object.hasOwn(record.payload, "id"))) throw new TypeError("Claims canonical payload must not duplicate the envelope ID");
    for (const record of records) if (record.payload.operation === "claim") reducer.apply(record.payload, record);
    for (const record of records) if (record.payload.operation !== "claim") reducer.apply(record.payload, record);
  } else {
    for (const record of records) reducer.apply(record.payload);
  }
  return reducer.output();
}

module.exports = { CLAIM_STATE_COLUMNS, CLAIM_STATE_VIEW, OPERATIONS, createReducer, replayBuiltin, validateOperation };
