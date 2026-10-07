// @ts-check

const fs = require("node:fs");
const path = require("node:path");
const { spawn } = require("node:child_process");
const readline = require("node:readline");
const { loadStructuredOutputSchema, validateStructuredOutput } = require("./structured_output.cjs");

const MAX_RESPONSE_BYTES = 256 * 1024;

/**
 * The pinned SDK's generateContent API supports a subset, not arbitrary JSON Schema.
 * In particular, oneOf is treated as anyOf, so its exclusive semantics cannot be guaranteed.
 * https://github.com/googleapis/js-genai/blob/v1.30.0/src/types.ts#L2178-L2193
 *
 * @param {import("ajv").AnySchemaObject} schema
 */
function validateNativeSchema(schema) {
  const supported = new Set([
    "$schema",
    "$id",
    "$defs",
    "$ref",
    "$anchor",
    "type",
    "format",
    "title",
    "description",
    "enum",
    "items",
    "prefixItems",
    "minItems",
    "maxItems",
    "minimum",
    "maximum",
    "anyOf",
    "properties",
    "additionalProperties",
    "required",
    "propertyOrdering",
  ]);
  /** @param {any} node @param {Set<any>} ancestors */
  function visit(node, ancestors) {
    if (!node || typeof node !== "object" || Array.isArray(node)) {
      throw new Error("Gemini native structured output requires object-valued subschemas; use prefixItems for tuple schemas");
    }
    if (ancestors.has(node)) {
      throw new Error("Gemini native structured output cannot fully enforce recursive JSON schemas; use an acyclic schema");
    }
    const nextAncestors = new Set(ancestors).add(node);
    for (const keyword of Object.keys(node)) {
      if (!supported.has(keyword)) {
        throw new Error(`Gemini native structured output does not support JSON Schema keyword "${keyword}"; use its supported generateContent schema subset`);
      }
    }
    if ("$ref" in node) {
      if (typeof node.$ref !== "string" || (node.$ref !== "#" && !node.$ref.startsWith("#/"))) {
        throw new Error("Gemini structured output supports local JSON Pointer references only; replace anchor or remote references");
      }
      if (Object.keys(node).some(keyword => !keyword.startsWith("$"))) {
        throw new Error("Gemini native structured output does not support validation keywords alongside $ref");
      }
      const reference =
        node.$ref === "#"
          ? []
          : node.$ref
              .slice(2)
              .split("/")
              .map(part => part.replace(/~1/g, "/").replace(/~0/g, "~"));
      const target = reference.reduce((value, part) => value?.[part], schema);
      visit(target, nextAncestors);
    }
    if (node.enum && (!Array.isArray(node.enum) || node.enum.some(value => typeof value !== "string" && typeof value !== "number"))) {
      throw new Error("Gemini native structured output supports enum values only for strings and numbers");
    }
    for (const group of ["properties", "$defs"]) {
      for (const child of Object.values(node[group] || {})) visit(child, nextAncestors);
    }
    if ("items" in node) visit(node.items, nextAncestors);
    if (typeof node.additionalProperties === "object") visit(node.additionalProperties, nextAncestors);
    for (const group of ["prefixItems", "anyOf"]) {
      for (const child of node[group] || []) visit(child, nextAncestors);
    }
  }
  visit(schema, new Set());
}

/**
 * Native CLI config, not a prompt-only schema instruction.
 * v0.62.0's primary chat sets isChatModel, and ModelConfigService matches
 * context keys before passing generateContentConfig through to GeminiChat.
 * https://github.com/google-gemini/gemini-cli/blob/v0.62.0/packages/core/src/services/modelConfigService.ts#L537-L566
 *
 * @param {Record<string, any>} settings
 * @param {import("ajv").AnySchemaObject} schema
 */
function configureNativeSchema(settings, schema) {
  if (settings.experimental?.adk?.agentSessionNoninteractiveEnabled || process.env.GEMINI_CLI_EXP_AGENT === "true") {
    throw new Error("Gemini structured output requires the standard headless chat runtime; disable the experimental ADK agent session");
  }
  if (settings.hooks?.BeforeModel?.length) {
    throw new Error("Gemini structured output cannot use BeforeModel hooks that may replace native generation settings");
  }
  const modelConfigs = settings.modelConfigs || {};
  const configuredOverrides = modelConfigs.customOverrides || [];
  if (!Array.isArray(configuredOverrides)) {
    throw new Error("Gemini modelConfigs.customOverrides must be an array");
  }
  const overrides = configuredOverrides.filter(config => {
    const match = config.match;
    const generation = config.modelConfig?.generateContentConfig;
    return !(
      match?.isChatModel === true &&
      match.overrideScope === "core" &&
      Object.keys(match).length === 2 &&
      generation?.responseMimeType === "application/json" &&
      Object.keys(generation).length === 2 &&
      JSON.stringify(generation.responseJsonSchema) === JSON.stringify(schema)
    );
  });
  const existingConfigs = [...Object.values(modelConfigs.aliases || {}), ...Object.values(modelConfigs.customAliases || {}), ...(modelConfigs.overrides || []), ...overrides];
  for (const config of existingConfigs) {
    const generation = config.modelConfig?.generateContentConfig;
    if (generation && ("responseSchema" in generation || "responseJsonSchema" in generation || "responseMimeType" in generation)) {
      throw new Error("Gemini structured output cannot combine existing modelConfigs response schema settings; remove the conflicting native output overrides");
    }
  }
  return {
    ...settings,
    modelConfigs: {
      ...modelConfigs,
      customOverrides: [
        ...overrides,
        {
          match: { isChatModel: true, overrideScope: "core" },
          modelConfig: {
            generateContentConfig: {
              responseMimeType: "application/json",
              responseJsonSchema: schema,
            },
          },
        },
      ],
    },
  };
}

/**
 * Preserve only the last assistant turn, not commentary preceding tool calls.
 * @returns {{accept: (line: string) => void, finish: () => {text: string, sessionId: string}}}
 */
function createResponseCollector() {
  let text = "";
  let sessionId = "";
  let complete = false;
  return {
    accept(line) {
      let event;
      try {
        event = JSON.parse(line);
      } catch {
        return;
      }
      if (!event || typeof event !== "object") return;
      if (event.type === "init") {
        text = "";
        complete = false;
        sessionId = typeof event.session_id === "string" ? event.session_id : "";
      } else if (event.type === "tool_use" || event.type === "tool_result") {
        text = "";
        complete = false;
      } else if (event.type === "message" && event.role === "assistant" && typeof event.content === "string") {
        complete = false;
        text = event.delta === false ? event.content : text + event.content;
        if (Buffer.byteLength(text, "utf8") > MAX_RESPONSE_BYTES) {
          throw new Error("Gemini structured output exceeds the 256 KiB response limit");
        }
      } else if (event.type === "result") {
        complete = event.status === "success";
      }
    },
    finish() {
      if (!complete) throw new Error("Gemini structured output requires a successful terminal result");
      if (!text.trim()) throw new Error("Gemini produced no primary final response for structured output");
      return { text: text.trim(), sessionId };
    },
  };
}

/**
 * @param {string} command
 * @param {string[]} args
 * @returns {Promise<{text: string, sessionId: string}>}
 */
function executeGemini(command, args) {
  return new Promise((resolve, reject) => {
    const collector = createResponseCollector();
    const child = spawn(command, args, { stdio: ["ignore", "pipe", "inherit"], env: process.env });
    child.stdout.pipe(process.stdout);
    const lines = readline.createInterface({ input: child.stdout });
    /** @type {Error | undefined} */
    let collectionError;
    lines.on("line", line => {
      try {
        collector.accept(line);
      } catch (error) {
        collectionError = error instanceof Error ? error : new Error(String(error));
        child.kill("SIGTERM");
      }
    });
    child.on("error", reject);
    child.on("close", (code, signal) => {
      lines.close();
      if (collectionError) return reject(collectionError);
      if (code !== 0) return reject(new Error(`Gemini CLI failed during structured output (exit ${code}, signal ${signal || "none"})`));
      try {
        resolve(collector.finish());
      } catch (error) {
        reject(error);
      }
    });
  });
}

/**
 * @param {string[]} args
 * @param {string} sessionId
 * @param {string} reason
 */
function correctionArguments(args, sessionId, reason) {
  if (!/^[a-zA-Z0-9_-]+$/.test(sessionId)) {
    throw new Error("Gemini structured output correction requires a valid native session ID");
  }
  const promptIndex = args.indexOf("--prompt");
  if (promptIndex < 0 || promptIndex + 1 >= args.length) {
    throw new Error("Gemini structured output requires the primary --prompt argument");
  }
  const corrected = [...args];
  corrected.splice(promptIndex, 2);
  corrected.push(
    "--resume",
    sessionId,
    "--prompt",
    `The final response did not validate (${reason}). Correct your primary final response to satisfy the configured native JSON schema. Return only the corrected JSON value; do not repeat completed tool actions.`
  );
  return corrected;
}

/**
 * @param {string} command
 * @param {string[]} args
 * @param {import("ajv").AnySchemaObject} schema
 * @param {(command: string, args: string[]) => Promise<{text: string, sessionId: string}>} [execute]
 */
async function generateStructuredResponse(command, args, schema, execute = executeGemini) {
  let response = await execute(command, args);
  for (let attempt = 0; attempt < 2; attempt++) {
    try {
      const value = JSON.parse(response.text);
      validateStructuredOutput(value, schema);
      return response.text;
    } catch (error) {
      if (!(error instanceof SyntaxError) && !(error instanceof Error && error.message.startsWith("Structured output schema violation:"))) {
        throw new Error("Gemini structured output schema could not be validated");
      }
      const reason =
        error instanceof SyntaxError
          ? "the response is not raw JSON"
          : error instanceof Error && error.message.startsWith("Structured output schema violation:")
            ? error.message
            : "the response could not be validated against the JSON schema";
      if (attempt === 1) throw new Error(`Gemini structured output failed after one correction attempt: ${reason}`);
      response = await execute(command, correctionArguments(args, response.sessionId, reason));
    }
  }
  throw new Error("Gemini structured output did not produce a response");
}

/**
 * @param {string[]} argv
 */
async function main(argv) {
  const [schemaPath, outputPath, separator, command, ...args] = argv;
  if (!schemaPath || !outputPath || separator !== "--" || !command) {
    throw new Error("Usage: gemini_structured_output.cjs SCHEMA OUTPUT -- gemini ARGS");
  }
  fs.rmSync(outputPath, { force: true });
  const schema = loadStructuredOutputSchema(schemaPath);
  validateNativeSchema(schema);
  try {
    validateStructuredOutput(null, schema);
  } catch (error) {
    if (!(error instanceof Error) || !error.message.startsWith("Structured output schema violation:")) {
      throw new Error("Gemini structured output schema is invalid");
    }
  }
  const settingsPath = path.join(process.env.GITHUB_WORKSPACE || process.cwd(), ".gemini", "settings.json");
  const settings = JSON.parse(fs.readFileSync(settingsPath, "utf8"));
  const nativeSettings = `${JSON.stringify(configureNativeSchema(settings, schema))}\n`;
  const response = await generateStructuredResponse(command, args, schema, (command, args) => {
    fs.writeFileSync(settingsPath, nativeSettings);
    return executeGemini(command, args);
  });
  fs.writeFileSync(outputPath, `${response}\n`, { mode: 0o600 });
}

module.exports = { validateNativeSchema, configureNativeSchema, createResponseCollector, correctionArguments, generateStructuredResponse, main };

if (require.main === module) {
  main(process.argv.slice(2)).catch(error => {
    console.error(error.message);
    process.exitCode = 1;
  });
}
