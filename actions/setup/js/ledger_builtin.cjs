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
  notes: ["note", "vote"],
});
const MAX_REPLAY_CELL_BYTES = 65536;
const NOTE_LIMITS = { subject: 512, note: 4096, reason: 1024, citations: 32, citationBytes: 2048 };
const NOTE_STATE_COLUMNS = { note_id: "text", upvotes: "integer", downvotes: "integer", net_votes: "integer", last_vote_at: "text", last_positive_vote_at: "text" };
const NOTE_STATE_VIEW = `CREATE VIEW note_state AS
  WITH vote_state AS (
    SELECT note_id,
      sum(CASE WHEN vote = 'up' THEN 1 ELSE 0 END) AS upvotes,
      sum(CASE WHEN vote = 'down' THEN 1 ELSE 0 END) AS downvotes,
      max(created_at) AS last_vote_at,
      max(CASE WHEN vote = 'up' THEN created_at END) AS last_positive_vote_at
    FROM note_votes
    GROUP BY note_id
  )
  SELECT c.id AS note_id,
    coalesce(v.upvotes, 0) AS upvotes,
    coalesce(v.downvotes, 0) AS downvotes,
    coalesce(v.upvotes, 0) - coalesce(v.downvotes, 0) AS net_votes,
    v.last_vote_at,
    v.last_positive_vote_at
  FROM notes c
  LEFT JOIN vote_state v ON v.note_id = c.id`;

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
  if (config.type === "notes") {
    const fields = operation === "note" ? ["subject", "note", "reason", "citations"] : ["note_id", "vote"];
    const expected = new Set(["operation", ...fields, ...(operation === "vote" ? ["reason"] : []), ...(record.id === undefined ? [] : ["id"])]);
    if (Object.keys(record).some(key => !expected.has(key)) || fields.some(key => !Object.hasOwn(record, key))) throw new TypeError("Invalid ledger operation fields");
    if (record.id !== undefined && (typeof record.id !== "string" || !record.id || Buffer.byteLength(record.id) > 128)) throw new TypeError("Invalid note record ID");
    if ((operation === "note" || Object.hasOwn(record, "reason")) && (typeof record.reason !== "string" || !record.reason.trim() || [...record.reason].length > NOTE_LIMITS.reason))
      throw new TypeError("Note reason must be a nonempty bounded string");
    if (operation === "note") {
      if (
        typeof record.subject !== "string" ||
        !record.subject.trim() ||
        Buffer.byteLength(record.subject) > NOTE_LIMITS.subject ||
        typeof record.note !== "string" ||
        !record.note.trim() ||
        Buffer.byteLength(record.note) > NOTE_LIMITS.note
      )
        throw new TypeError("Note subject and assertion must be nonempty bounded strings");
      if (!Array.isArray(record.citations) || !record.citations.length || record.citations.length > NOTE_LIMITS.citations || record.citations.some(citation => !validateCitation(citation)))
        throw new TypeError("Note citations must contain at least one valid citation object");
      for (const citation of record.citations) if (Buffer.byteLength(canonicalJSON(citation)) > NOTE_LIMITS.citationBytes) throw new RangeError("Note citation exceeds size limit");
    } else {
      if (typeof record.note_id !== "string" || !record.note_id || Buffer.byteLength(record.note_id) > 128 || !["up", "down"].includes(record.vote)) throw new TypeError("Vote requires a bounded note ID and an up or down vote");
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
  const notes = new Map();
  const votes = new Map();
  function apply(record, envelope) {
    validateOperation(record, config);
    const { operation, value, key } = record;
    switch (config.type) {
      case "notes": {
        const id = envelope?.id ?? record.id;
        if (typeof id !== "string" || !id) throw new TypeError("Notes projection requires a canonical record ID");
        const entry = { record, recordId: envelope?.id ?? null, timestamp: envelope?.timestamp ?? null, sha: envelope?.sha ?? null };
        if (operation === "note") {
          if (notes.has(id) || votes.has(id)) throw new TypeError("Duplicate note record ID");
          notes.set(id, entry);
        } else {
          if (!notes.has(record.note_id)) throw new TypeError("Vote references a missing note");
          if (votes.has(id) || notes.has(id)) throw new TypeError("Duplicate vote record ID");
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
    if (config.type === "notes") {
      const noteRows = [...notes].sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0));
      const voteRows = [...votes].sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0));
      const tables = {
        notes: {
          columns: { id: "text", subject: "text", note: "text", reason: "text", created_at: "text", record_sha: "text" },
          primaryKey: ["id"],
          rows: noteRows.map(([id, { record, timestamp, sha }]) => ({ id, subject: record.subject, note: record.note, reason: record.reason, created_at: timestamp, record_sha: sha })),
        },
        note_citations: {
          columns: { note_id: "text", ordinal: "integer", citation_type: "text", path: "text", start_line: "integer", end_line: "integer" },
          primaryKey: ["note_id", "ordinal"],
          rows: noteRows.flatMap(([note_id, { record }]) =>
            record.citations.map((citation, ordinal) => ({
              note_id,
              ordinal,
              citation_type: citation.type,
              path: citation.path,
              start_line: citation.start_line ?? null,
              end_line: citation.end_line ?? null,
            }))
          ),
        },
        note_votes: {
          columns: { record_id: "text", note_id: "text", vote: "text", reason: "text", created_at: "text" },
          primaryKey: ["record_id"],
          rows: voteRows.map(([, { record, recordId, timestamp }]) => ({ record_id: recordId, note_id: record.note_id, vote: record.vote, reason: record.reason ?? null, created_at: timestamp })),
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
  if (config.type === "notes") {
    if (records.some(record => Object.hasOwn(record.payload, "id"))) throw new TypeError("Notes canonical payload must not duplicate the envelope ID");
    for (const record of records) if (record.payload.operation === "note") reducer.apply(record.payload, record);
    for (const record of records) if (record.payload.operation !== "note") reducer.apply(record.payload, record);
  } else {
    for (const record of records) reducer.apply(record.payload);
  }
  return reducer.output();
}

module.exports = { NOTE_STATE_COLUMNS, NOTE_STATE_VIEW, OPERATIONS, createReducer, replayBuiltin, validateOperation };
