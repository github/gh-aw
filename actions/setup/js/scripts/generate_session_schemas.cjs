// @ts-check

const fs = require("node:fs");
const path = require("node:path");
const { createGenerator } = require("ts-json-schema-generator");
const prettier = require("prettier");
const { SESSION_FILE_FORMAT_VERSION } = require("../unified_session.cjs");

const SCHEMA_DIRECTORY = path.resolve(__dirname, "../../../../docs/public/schemas");
const TYPE_FILE = path.resolve(__dirname, "../types/unified_session.d.ts");

/** @typedef {import("ts-json-schema-generator").Schema} Schema */
/** @typedef {import("json-schema").JSONSchema7Definition} SchemaDefinition */

/** @param {SchemaDefinition} schema @param {Record<string, SchemaDefinition>} definitions @returns {Schema} */
function resolveDefinition(schema, definitions) {
  if (typeof schema === "boolean") throw new Error("Session declarations must resolve to an object schema");
  if (!schema.$ref) return schema;
  const name = decodeURIComponent(schema.$ref.slice("#/definitions/".length)).replace(/~1/g, "/").replace(/~0/g, "~");
  if (!definitions[name]) throw new Error(`Missing session schema definition: ${name}`);
  return resolveDefinition(definitions[name], definitions);
}

/** @param {Schema} schema @param {Record<string, SchemaDefinition>} definitions */
function reachableDefinitions(schema, definitions) {
  const selected = {};
  const visit = value => {
    if (!value || typeof value !== "object") return;
    if (typeof value.$ref === "string") {
      const name = decodeURIComponent(value.$ref.slice("#/definitions/".length)).replace(/~1/g, "/").replace(/~0/g, "~");
      if (!Object.hasOwn(selected, name)) {
        if (!definitions[name]) throw new Error(`Missing session schema definition: ${name}`);
        selected[name] = definitions[name];
        visit(definitions[name]);
      }
    }
    for (const child of Object.values(value)) visit(child);
  };
  visit(schema);
  return Object.fromEntries(Object.entries(selected).sort(([left], [right]) => (left < right ? -1 : left > right ? 1 : 0)));
}

/** @returns {Promise<Record<string, string>>} */
async function generateSessionSchemas() {
  const generated = createGenerator({
    path: TYPE_FILE,
    type: "SessionSchemaDefinitions",
    expose: "all",
    additionalProperties: false,
    jsDoc: "extended",
    skipTypeCheck: false,
  }).createSchema("SessionSchemaDefinitions");
  const definitions = generated.definitions ?? {};
  const root = resolveDefinition(generated, definitions);
  if (!root.properties) throw new Error("Session schema declarations are missing their properties");
  const output = {};
  for (const kind of ["agent", "unified"]) {
    const map = resolveDefinition(root.properties[kind], definitions);
    if (!map.properties) throw new Error(`Missing ${kind} session payload map`);
    const branches = Object.entries(map.properties)
      .sort(([left], [right]) => (left < right ? -1 : left > right ? 1 : 0))
      .map(([type, data]) => ({
        if: { properties: { type: { const: type } }, required: ["type"] },
        then: { properties: { data } },
      }));
    /** @type {Schema} */
    const event = {
      type: "object",
      required: ["type", "data", ...(kind === "unified" ? ["provenance"] : [])],
      properties: {
        type: { type: "string", pattern: "\\." },
        data: { type: "object" },
        ...(kind === "unified" ? { provenance: root.properties.provenance } : {}),
      },
      // Conditional dispatch prevents an invalid known payload matching the
      // permissive native-extension branch of the TypeScript union.
      allOf: [root.properties.metadata, ...branches],
    };
    const filename = `${kind === "agent" ? "agent" : "unified"}-session.schema.json`;
    /** @type {Schema} */
    const schema = {
      $schema: "http://json-schema.org/draft-07/schema#",
      $id: `https://github.github.io/gh-aw/schemas/${filename}`,
      title: kind === "agent" ? "Canonical agent session" : "Unified agent session file",
      description: "Generated from actions/setup/js/types/{agent_session,unified_session}.d.ts. Run make session-schemas; do not edit by hand.",
      type: "array",
      items: event,
      ...(kind === "unified"
        ? {
            minItems: 1,
            allOf: [
              {
                items: [
                  {
                    properties: {
                      type: { const: "session.format" },
                      data: { properties: { version: { const: SESSION_FILE_FORMAT_VERSION } }, required: ["version"] },
                      provenance: {
                        properties: {
                          component: { const: "collector" },
                          phase: { const: "conclusion" },
                          path: { const: "usage/aw_session.jsonl" },
                          index: { const: 0 },
                        },
                        not: { required: ["timestampMs"] },
                      },
                    },
                    not: { required: ["timestamp"] },
                  },
                ],
              },
            ],
          }
        : {}),
    };
    const complete = { ...schema, definitions: reachableDefinitions(schema, definitions) };
    output[filename] = await prettier.format(JSON.stringify(complete), { parser: "json", printWidth: 240, trailingComma: "es5" });
  }
  return output;
}

/** @param {string[]} args */
async function main(args) {
  if (args.length && (args.length !== 1 || args[0] !== "--check")) throw new Error("Usage: generate_session_schemas.cjs [--check]");
  const schemas = await generateSessionSchemas();
  for (const [filename, content] of Object.entries(schemas)) {
    const file = path.join(SCHEMA_DIRECTORY, filename);
    if (args[0] === "--check") {
      if (!fs.existsSync(file) || fs.readFileSync(file, "utf8") !== content) throw new Error(`Session schema is stale: ${filename}. Run make session-schemas.`);
    } else {
      fs.mkdirSync(SCHEMA_DIRECTORY, { recursive: true });
      fs.writeFileSync(file, content);
    }
  }
}

if (require.main === module) {
  main(process.argv.slice(2)).catch(error => {
    console.error(error instanceof Error ? error.message : String(error));
    process.exitCode = 1;
  });
}

module.exports = { generateSessionSchemas, main, SCHEMA_DIRECTORY };
