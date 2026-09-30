// @ts-check
"use strict";

const crypto = require("node:crypto");
const cp = require("node:child_process");
const path = require("node:path");

const WORKER = path.join(__dirname, "ledger_replay_worker.cjs");
const IDENTIFIER = /^[A-Za-z][A-Za-z0-9_]{0,63}$/;
const RESERVED = new Set([
  "records",
  "records_history",
  "parents",
  "parents_history",
  "shards",
  "diagnostics",
  "replay_metadata",
  "records_by_id",
  "records_by_type_time",
  "records_by_shard_offset",
  "parents_by_parent",
  "diagnostics_by_code",
]);
const TYPES = new Set(["text", "integer", "real", "boolean", "json"]);

function applyLedgerCompactions(records) {
  const dropped = new Set();
  for (const record of records) {
    if (record.type === "ledger_compact") {
      const operation = record.payload;
      if (operation && operation.operation === "drop" && typeof operation.id === "string") dropped.add(operation.id);
    }
  }
  return records.filter(record => record.type !== "ledger_compact" && !dropped.has(record.id) && !dropped.has(record.payload?.id));
}

function validateReplayOutput(output) {
  if (
    !output ||
    typeof output !== "object" ||
    Array.isArray(output) ||
    Object.keys(output).some(key => !["version", "tables"].includes(key)) ||
    (output.version !== undefined && output.version !== 1) ||
    !output.tables ||
    typeof output.tables !== "object" ||
    Array.isArray(output.tables)
  ) {
    throw new TypeError("Invalid replay output");
  }
  const tables = Object.entries(output.tables);
  if (tables.length > 16) throw new RangeError("Too many replay tables");
  if (new Set(tables.map(([name]) => name.toLowerCase())).size !== tables.length) throw new TypeError("Duplicate replay table name");
  let totalRows = 0;
  for (const [name, table] of tables) {
    if (
      !IDENTIFIER.test(name) ||
      name.toLowerCase().startsWith("sqlite_") ||
      RESERVED.has(name.toLowerCase()) ||
      !table ||
      typeof table !== "object" ||
      Array.isArray(table) ||
      Object.keys(table).some(key => !["columns", "primaryKey", "rows"].includes(key)) ||
      !table.columns ||
      typeof table.columns !== "object" ||
      Array.isArray(table.columns) ||
      !Array.isArray(table.rows) ||
      !Array.isArray(table.primaryKey)
    )
      throw new TypeError("Invalid replay table");
    const columns = Object.entries(table.columns);
    const names = columns.map(([column]) => column.toLowerCase());
    if (
      !columns.length ||
      columns.length > 32 ||
      new Set(names).size !== names.length ||
      columns.some(([column, type]) => !IDENTIFIER.test(column) || column.toLowerCase().startsWith("sqlite_") || !TYPES.has(type)) ||
      !table.primaryKey.length ||
      table.primaryKey.length > columns.length ||
      new Set(table.primaryKey).size !== table.primaryKey.length ||
      table.primaryKey.some(column => !Object.hasOwn(table.columns, column))
    )
      throw new TypeError("Invalid replay columns or primary key");
    totalRows += table.rows.length;
    if (totalRows > 10000) throw new RangeError("Too many replay rows");
    const keys = new Set();
    for (const row of table.rows) {
      if (!row || typeof row !== "object" || Array.isArray(row) || Object.keys(row).some(column => !Object.hasOwn(table.columns, column))) throw new TypeError("Invalid replay row");
      for (const [column, type] of columns) {
        const value = row[column];
        if (
          value === undefined ||
          (value !== null &&
            ((type === "text" && typeof value !== "string") ||
              (type === "integer" && !Number.isSafeInteger(value)) ||
              (type === "real" && (typeof value !== "number" || !Number.isFinite(value))) ||
              (type === "boolean" && typeof value !== "boolean") ||
              (type === "json" && (typeof value !== "object" || value === null))))
        )
          throw new TypeError("Invalid replay cell");
        if (type === "json" && value !== null) {
          const inspect = (item, depth) => {
            if (depth > 32 || item === undefined || typeof item === "bigint" || typeof item === "function" || typeof item === "symbol" || (typeof item === "number" && !Number.isFinite(item))) throw new TypeError("Invalid replay JSON cell");
            if (item && typeof item === "object") for (const child of Object.values(item)) inspect(child, depth + 1);
          };
          inspect(value, 0);
        }
        if (value !== null && Buffer.byteLength(type === "json" ? JSON.stringify(value) : String(value)) > 65536) {
          throw new RangeError("Replay cell exceeds size limit");
        }
      }
      const keyValues = table.primaryKey.map(column => row[column]);
      if (keyValues.some(value => value === null)) throw new TypeError("Null replay primary key");
      const key = JSON.stringify(keyValues);
      if (keys.has(key)) throw new TypeError("Duplicate replay primary key");
      keys.add(key);
    }
  }
  return tables;
}

function executeReplay(script, records, config = {}) {
  if (typeof script !== "string" || !script.trim() || Buffer.byteLength(script) > 65536) throw new TypeError("Invalid replay script");
  const input = JSON.stringify({ script, records, config });
  if (Buffer.byteLength(input) > 16 * 1024 * 1024) throw new RangeError("Replay input exceeds size limit");
  const proc = cp.spawnSync(process.execPath, ["--permission", "--max-old-space-size=256", `--allow-fs-read=${WORKER}`, WORKER], {
    input,
    encoding: "utf8",
    timeout: 6000,
    maxBuffer: 4 * 1024 * 1024 + 1024,
    env: {},
    cwd: "/tmp",
  });
  if (proc.error || proc.status !== 0) throw new Error(proc.error && Reflect.get(proc.error, "code") === "ETIMEDOUT" ? "Replay timed out" : "Replay worker failed");
  let output;
  try {
    output = JSON.parse(proc.stdout);
  } catch {
    throw new TypeError("Malformed replay output");
  }
  validateReplayOutput(output);
  return output;
}

function materializeReplay(db, ledgerName, script, records, output) {
  const tables = validateReplayOutput(output);
  db.exec("BEGIN");
  try {
    db.exec(
      "CREATE TABLE replay_metadata (ledger_name TEXT NOT NULL, projection_version INTEGER NOT NULL, record_count INTEGER NOT NULL, script_sha256 TEXT NOT NULL, output_version INTEGER NOT NULL, table_name TEXT NOT NULL, columns_json TEXT NOT NULL)"
    );
    const insertMetadata = db.prepare("INSERT INTO replay_metadata VALUES (?, ?, ?, ?, ?, ?, ?)");
    const hash = crypto.createHash("sha256").update(script).digest("hex");
    if (!tables.length) insertMetadata.run(ledgerName, 1, records.length, hash, output.version ?? 1, "", "{}");
    for (const [name, table] of tables) {
      const columns = Object.entries(table.columns);
      const quote = value => `"${value}"`;
      const sqlType = { text: "TEXT", integer: "INTEGER", real: "REAL", boolean: "INTEGER", json: "TEXT" };
      db.exec(
        `CREATE TABLE ${quote(name)} (${columns.map(([column, type]) => `${quote(column)} ${sqlType[type]}${table.primaryKey.includes(column) ? " NOT NULL" : ""}`).join(", ")}, PRIMARY KEY (${table.primaryKey.map(quote).join(", ")}))`
      );
      const insert = db.prepare(`INSERT INTO ${quote(name)} (${columns.map(([column]) => quote(column)).join(", ")}) VALUES (${columns.map(() => "?").join(", ")})`);
      for (const row of table.rows) {
        insert.run(...columns.map(([column, type]) => (type === "json" ? JSON.stringify(row[column]) : type === "boolean" ? (row[column] === null ? null : Number(row[column])) : row[column])));
      }
      insertMetadata.run(ledgerName, 1, records.length, hash, output.version ?? 1, name, JSON.stringify(table.columns));
    }
    db.exec("COMMIT");
  } catch (error) {
    db.exec("ROLLBACK");
    throw error;
  }
}

module.exports = { applyLedgerCompactions, executeReplay, materializeReplay, validateReplayOutput };
