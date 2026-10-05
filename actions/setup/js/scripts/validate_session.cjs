// @ts-check

const fs = require("node:fs");
const path = require("node:path");
const { Ajv } = require("ajv");
const { validateSessionFileHeader } = require("../unified_session_render.cjs");

const SCHEMA_DIRECTORY = path.resolve(__dirname, "../../../../docs/public/schemas");

/** @param {"agent" | "unified"} kind */
function createSessionValidator(kind) {
  const schema = JSON.parse(fs.readFileSync(path.join(SCHEMA_DIRECTORY, `${kind}-session.schema.json`), "utf8"));
  const ajv = new Ajv({ allErrors: true, strict: false, strictNumbers: true });
  ajv.addSchema(schema);
  return { array: ajv.getSchema(schema.$id), event: ajv.compile({ $ref: `${schema.$id}#/items` }), ajv };
}

/** @param {import("ajv").ErrorObject[] | null | undefined} errors */
function schemaDiagnostic(errors) {
  // Instance paths can contain private, source-supplied object keys.
  return [...new Set(errors?.map(error => `${error.schemaPath}: ${error.message}`) ?? ["Schema validation failed without diagnostics"])].join("; ");
}

/** @param {string} content @param {"agent" | "unified"} kind @param {"json" | "jsonl"} format */
function validateSession(content, kind = "unified", format = "jsonl") {
  const validators = createSessionValidator(kind);
  let events = [];
  if (format === "json") {
    let value;
    try {
      value = JSON.parse(content);
    } catch {
      throw new Error("Invalid session JSON");
    }
    if (!Array.isArray(value)) throw new Error("Session JSON must be an event array, not a wrapper");
    events = value;
  } else {
    if (kind === "unified" && !content.endsWith("\n")) throw new Error("Unified session JSONL must end with a newline");
    const lines = content.split("\n");
    if (lines.at(-1) === "") lines.pop();
    for (const [index, line] of lines.entries()) {
      if (!line.trim()) {
        if (kind === "unified") throw new Error(`Blank unified session JSONL line ${index + 1}`);
        continue;
      }
      let event;
      try {
        event = JSON.parse(line);
      } catch {
        throw new Error(`Invalid session JSONL at line ${index + 1}`);
      }
      if (kind === "unified" && /[ \t\r]/.test(line.replace(/"(?:[^"\\]|\\.)*"/g, ""))) throw new Error(`Unified session JSONL must use compact JSON at line ${index + 1}`);
      if (!validators.event(event)) throw new Error(`Invalid session event at line ${index + 1}: ${schemaDiagnostic(validators.event.errors)}`);
      events.push(event);
    }
  }
  if (!validators.array) throw new Error("Session array schema is unavailable");
  if (!validators.array(events)) throw new Error(`Invalid ${kind} session: ${schemaDiagnostic(validators.array.errors)}`);
  if (kind === "unified") {
    validateSessionFileHeader(events);
    let previousTime = -Infinity;
    let untimed = false;
    for (const [index, event] of events.entries()) {
      const sourcePath = event.provenance.path;
      if (!sourcePath || /^(?:[/\\]|[a-z]:)/i.test(sourcePath) || sourcePath.split(/[/\\]/).includes("..")) throw new Error(`Invalid relative provenance path at record ${index + 1}`);
      if (index === 0) continue;
      const time = event.provenance.timestampMs;
      if (time === undefined) untimed = true;
      else {
        if (untimed || time < previousTime) throw new Error(`Unified session is not timestamp ordered at record ${index + 1}`);
        previousTime = time;
      }
    }
  }
  return events.length;
}

/** @param {string[]} args */
function main(args) {
  const [kind, filename, format = filename?.endsWith(".json") ? "json" : "jsonl"] = args;
  if (args.length < 2 || args.length > 3 || !["agent", "unified"].includes(kind) || !["json", "jsonl"].includes(format)) {
    throw new Error("Usage: validate_session.cjs <agent|unified> <file> [json|jsonl]");
  }
  if (kind !== "agent" && kind !== "unified") throw new Error("Invalid session kind");
  if (format !== "json" && format !== "jsonl") throw new Error("Invalid session format");
  const count = validateSession(fs.readFileSync(filename, "utf8"), kind, format);
  process.stdout.write(`Valid ${kind} session: ${count} record(s)\n`);
}

if (require.main === module) {
  try {
    main(process.argv.slice(2));
  } catch (error) {
    // Report structural diagnostics only, never source payloads or parser errors.
    console.error(error instanceof Error ? error.message : String(error));
    process.exitCode = 1;
  }
}

module.exports = { validateSession, createSessionValidator, main };
