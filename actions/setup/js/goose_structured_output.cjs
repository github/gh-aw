// @ts-check

"use strict";

const { isDeepStrictEqual } = require("node:util");
const http = require("node:http");
const https = require("node:https");

/**
 * Enforce the correction budget before another inference reaches the provider.
 * Goose's native validator supplies the failures; this proxy does not constrain
 * or rewrite outputs. stdout observation alone races the next model request.
 *
 * @param {string} upstreamHost
 * @returns {Promise<{host: string, hasExhaustedCorrection: () => boolean, close: () => Promise<void>}>}
 */
async function startGooseStructuredOutputProxy(upstreamHost) {
  const failures = new Set();
  let exhausted = false;
  const server = http.createServer(async (request, response) => {
    try {
      if (!request.url?.startsWith("/")) throw new Error("Invalid Goose inference request path");
      const chunks = [];
      let bytes = 0;
      for await (const chunk of request) {
        bytes += chunk.length;
        if (bytes > 16 * 1024 * 1024) throw new Error("Goose inference request exceeds 16 MiB");
        chunks.push(chunk);
      }
      const body = Buffer.concat(chunks);
      if (request.method === "POST" && body.length) {
        let payload;
        try {
          payload = JSON.parse(body.toString("utf8"));
        } catch {
          throw new Error("Goose inference request must contain valid JSON");
        }
        const nativeCalls = new Set();
        for (const message of Array.isArray(payload.messages) ? payload.messages : []) {
          for (const call of Array.isArray(message.tool_calls) ? message.tool_calls : []) {
            if (call.function?.name === "recipe__final_output" && typeof call.id === "string") nativeCalls.add(call.id);
          }
          const content = typeof message.content === "string" ? message.content : "";
          if (message.role === "tool" && nativeCalls.has(message.tool_call_id) && content.includes("Validation failed:")) {
            failures.add(message.tool_call_id);
          }
        }
        if (failures.size > 1) {
          exhausted = true;
          response.writeHead(400, { "Content-Type": "application/json" });
          response.end(JSON.stringify({ error: { message: "Goose native structured output failed after one correction attempt", type: "invalid_request_error" } }));
          return;
        }
      }
      const target = new URL(upstreamHost.replace(/\/+$/, "") + request.url);
      const transport = target.protocol === "https:" ? https : http;
      const headers = { ...request.headers, host: target.host };
      const upstream = transport.request(target, { method: request.method, headers }, result => {
        response.writeHead(result.statusCode || 502, result.headers);
        result.on("error", () => response.destroy());
        result.pipe(response);
      });
      upstream.on("error", () => {
        if (!response.headersSent) response.writeHead(502);
        response.end();
      });
      response.on("close", () => upstream.destroy());
      upstream.end(body);
    } catch {
      if (!response.headersSent) response.writeHead(400);
      response.end();
    }
  });
  await new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => resolve(undefined));
  });
  const address = server.address();
  if (!address || typeof address === "string") throw new Error("Goose inference proxy did not bind a loopback port");
  return {
    host: `http://127.0.0.1:${address.port}`,
    hasExhaustedCorrection: () => exhausted,
    close: () =>
      new Promise(resolve => {
        server.close(() => resolve());
        server.closeAllConnections();
      }),
  };
}

/**
 * Goose renders recipe files as Jinja templates before parsing JSON. Escape
 * braces inside JSON strings so workflow prompts and schema descriptions remain
 * data, while JSON object delimiters retain their normal meaning.
 *
 * @param {Record<string, any>} schema
 * @param {string} prompt
 * @returns {string}
 */
function createGooseStructuredOutputRecipe(schema, prompt) {
  const modern = schema.$schema?.replace(/#$/, "") === "https://json-schema.org/draft/2020-12/schema";
  if (modern) {
    // Goose uses jsonschema::validator_for defaults, which treat modern formats
    // as annotations. AJV's shared contract enforces them in both drafts.
    const checkFormats = node => {
      if (!node || typeof node !== "object" || Array.isArray(node)) return;
      if (typeof node.format === "string") {
        throw new Error("Goose 1.53.0 cannot natively enforce draft-2020-12 format constraints. Use draft-07 or remove format constraints.");
      }
      for (const keyword of ["$defs", "definitions", "properties", "patternProperties", "dependentSchemas", "dependencies"]) {
        if (node[keyword] && typeof node[keyword] === "object") Object.values(node[keyword]).forEach(checkFormats);
      }
      for (const keyword of ["items", "additionalItems", "additionalProperties", "contains", "propertyNames", "not", "if", "then", "else", "unevaluatedProperties", "unevaluatedItems"]) {
        checkFormats(node[keyword]);
      }
      for (const keyword of ["allOf", "anyOf", "oneOf", "prefixItems"]) {
        if (Array.isArray(node[keyword])) node[keyword].forEach(checkFormats);
      }
    };
    checkFormats(schema);
  }
  // Goose defaults an absent $schema to its latest draft, while gh-aw uses 07.
  const nativeSchema = { ...schema, $schema: modern ? "https://json-schema.org/draft/2020-12/schema" : "http://json-schema.org/draft-07/schema#" };
  return JSON.stringify({
    version: "1.0.0",
    title: "GitHub Agentic Workflows structured output",
    description: "Native schema-validated final output",
    prompt,
    response: { json_schema: nativeSchema },
  }).replace(/"(?:[^"\\]|\\.)*"/g, literal => literal.replace(/\{/g, "\\u007b").replace(/\}/g, "\\u007d"));
}

/**
 * Collect only Goose's native recipe final-output result. A JSON-looking text
 * response alone is insufficient: the native tool must have succeeded, Goose
 * must emit the matching final assistant message, and the run must complete.
 *
 * @returns {{onStdoutLine: (line: string) => void, hasExhaustedCorrection: () => boolean, result: () => any}}
 */
function createGooseStructuredOutputCollector() {
  const requests = new Map();
  const failedValidationCalls = new Set();
  let accepted;
  let finalText;
  let complete = false;
  let failed = false;

  return {
    onStdoutLine(line) {
      let event;
      try {
        event = JSON.parse(line);
      } catch {
        return;
      }
      if (event?.type === "error") failed = true;
      if (event?.type === "complete") complete = true;
      if (event?.type !== "message" || !Array.isArray(event.message?.content)) return;
      for (const content of event.message.content) {
        if (event.message.role === "assistant" && content?.type === "toolRequest" && typeof content.id === "string" && content.toolCall?.status === "success" && content.toolCall.value?.name === "recipe__final_output") {
          requests.set(content.id, content.toolCall.value.arguments);
        }
        if (content?.type === "toolResponse" && requests.has(content.id) && content.toolResult?.status === "error") {
          const error = content.toolResult.error;
          const message = typeof error === "string" ? error : error?.message;
          if (typeof message === "string" && message.includes("Validation failed:")) {
            failedValidationCalls.add(content.id);
          }
        }
        if (
          content?.type === "toolResponse" &&
          requests.has(content.id) &&
          content.toolResult?.status === "success" &&
          content.toolResult.value?.isError !== true &&
          content.toolResult.value?.content?.some(item => item?.type === "text" && item.text === "Final output successfully collected.")
        ) {
          accepted = requests.get(content.id);
          finalText = undefined;
        }
        if (event.message.role === "assistant" && content?.type === "text") {
          finalText = content.text;
        }
      }
    },
    hasExhaustedCorrection() {
      return failedValidationCalls.size > 1;
    },
    result() {
      if (failedValidationCalls.size > 1) {
        throw new Error("Goose native structured output failed after one correction attempt");
      }
      if (failed || !complete || accepted === undefined || typeof finalText !== "string") {
        throw new Error("Goose did not complete a native schema-validated final output");
      }
      let value;
      try {
        value = JSON.parse(finalText);
      } catch {
        throw new Error("Goose native final output was not valid JSON");
      }
      if (!isDeepStrictEqual(value, accepted)) {
        throw new Error("Goose final response does not match its validated native output");
      }
      return value;
    },
  };
}

module.exports = { createGooseStructuredOutputRecipe, createGooseStructuredOutputCollector, startGooseStructuredOutputProxy };
