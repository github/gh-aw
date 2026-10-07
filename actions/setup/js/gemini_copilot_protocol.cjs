// @ts-check
"use strict";

const { getErrorMessage } = require("./error_helpers.cjs");

/**
 * @typedef {{text?: string, thought?: boolean, inlineData?: {mimeType: string, data: string},
 * functionCall?: {id?: string, name: string, args?: object},
 * functionResponse?: {id?: string, name: string, response: object}}} GeminiPart
 * @typedef {{role?: string, parts: GeminiPart[]}} GeminiContent
 * @typedef {{contents: GeminiContent[], systemInstruction?: GeminiContent,
 * tools?: {functionDeclarations?: {name: string, description?: string, parameters?: object, parametersJsonSchema?: object}[]}[],
 * toolConfig?: {functionCallingConfig?: {mode?: string, allowedFunctionNames?: string[]}},
 * generationConfig?: {temperature?: number, topP?: number, maxOutputTokens?: number,
 * stopSequences?: string[], responseMimeType?: string, responseSchema?: object, responseJsonSchema?: object,
 * topK?: number, thinkingConfig?: object, candidateCount?: number},
 * cachedContent?: string, safetySettings?: object[]}} GeminiRequest
 * @typedef {{role: string, content: string | object[] | null, tool_calls?: object[], tool_call_id?: string}} ChatMessage
 * @typedef {{choices: {message: {content?: string | null, reasoning_content?: string,
 * tool_calls?: {id: string, type: string, function: {name: string, arguments: string}}[]}, finish_reason?: string}[],
 * usage?: {prompt_tokens?: number, completion_tokens?: number, total_tokens?: number,
 * prompt_tokens_details?: {cached_tokens?: number}, completion_tokens_details?: {reasoning_tokens?: number}}}} ChatResponse
 */

class GeminiProtocolError extends Error {
  /** @param {string} message */
  constructor(message) {
    super(message);
    this.name = "GeminiProtocolError";
  }
}

/** @param {unknown} schema @returns {unknown} */
function normalizeSchema(schema) {
  if (Array.isArray(schema)) return schema.map(normalizeSchema);
  if (!schema || typeof schema !== "object") return schema;
  return Object.fromEntries(Object.entries(schema).map(([key, value]) => [key, key === "type" && typeof value === "string" ? value.toLowerCase() : normalizeSchema(value)]));
}

/**
 * @param {GeminiContent[]} contents
 * @returns {ChatMessage[]}
 */
function translateContents(contents) {
  if (!Array.isArray(contents)) throw new GeminiProtocolError("Gemini contents must be an array.");
  /** @type {ChatMessage[]} */
  const messages = [];
  /** @type {{id: string, name: string}[]} */
  const pending = [];
  for (const [messageIndex, content] of contents.entries()) {
    if (!content || !Array.isArray(content.parts)) throw new GeminiProtocolError("Gemini content parts must be an array.");
    if (content.role && content.role !== "model" && content.role !== "user") throw new GeminiProtocolError(`Unsupported Gemini role '${content.role}'.`);
    const role = content.role === "model" ? "assistant" : "user";
    /** @type {object[]} */
    const text = [];
    /** @type {object[]} */
    const calls = [];
    /** @type {ChatMessage[]} */
    const results = [];
    for (const [partIndex, part] of content.parts.entries()) {
      if (!part || typeof part !== "object") throw new GeminiProtocolError("Gemini content parts must be objects.");
      if (part.thought) continue;
      if (typeof part.text === "string") {
        text.push({ type: "text", text: part.text });
      } else if (part.inlineData?.mimeType?.startsWith("image/")) {
        text.push({ type: "image_url", image_url: { url: `data:${part.inlineData.mimeType};base64,${part.inlineData.data}` } });
      } else if (part.functionCall) {
        if (role !== "assistant") throw new GeminiProtocolError("Function calls must belong to a model message.");
        const call = part.functionCall;
        if (!call.name || (call.args && (typeof call.args !== "object" || Array.isArray(call.args)))) throw new GeminiProtocolError("Gemini function calls require a name and object arguments.");
        const id = call.id || `call_${messageIndex}_${partIndex}`;
        calls.push({ id, type: "function", function: { name: call.name, arguments: JSON.stringify(call.args || {}) } });
        pending.push({ id, name: call.name });
      } else if (part.functionResponse) {
        const result = part.functionResponse;
        const index = pending.findIndex(call => (result.id ? call.id === result.id : call.name === result.name));
        if (index === -1) throw new GeminiProtocolError(`No preceding function call for response '${result.name}'.`);
        const [call] = pending.splice(index, 1);
        results.push({ role: "tool", tool_call_id: call.id, content: JSON.stringify(result.response) });
      } else {
        throw new GeminiProtocolError("Unsupported Gemini content part; only text, images, and function calls are supported.");
      }
    }
    if (text.length || calls.length) messages.push({ role, content: text.length ? text : null, ...(calls.length ? { tool_calls: calls } : {}) });
    messages.push(...results);
  }
  return messages;
}

/**
 * @param {GeminiRequest} body
 * @param {string} model
 * @returns {Record<string, unknown>}
 */
function toChatRequest(body, model) {
  if (!body || typeof body !== "object" || Array.isArray(body)) throw new GeminiProtocolError("Gemini request must be an object.");
  if (body.cachedContent || body.safetySettings?.length) throw new GeminiProtocolError("Copilot does not support Gemini cachedContent or custom safetySettings.");
  const messages = translateContents(body.contents);
  if (body.systemInstruction) {
    const parts = body.systemInstruction.parts;
    if (!Array.isArray(parts) || parts.some(part => typeof part.text !== "string")) throw new GeminiProtocolError("System instructions must contain text only.");
    messages.unshift({ role: "system", content: parts.map(part => part.text).join("\n") });
  }
  /** @type {Record<string, unknown>} */
  const request = { model, messages, stream: false };
  if (body.tools && !Array.isArray(body.tools)) throw new GeminiProtocolError("Gemini tools must be an array.");
  const tools = (body.tools || []).flatMap(tool => {
    if (!tool || !Array.isArray(tool.functionDeclarations) || Object.keys(tool).some(key => key !== "functionDeclarations")) {
      throw new GeminiProtocolError("Copilot supports function declarations, not Gemini hosted tools such as urlContext or googleSearch.");
    }
    return tool.functionDeclarations.map(declaration => {
      if (!declaration || typeof declaration.name !== "string" || !declaration.name) throw new GeminiProtocolError("Gemini function declarations require a name.");
      return {
        type: "function",
        function: {
          name: declaration.name,
          description: declaration.description || "",
          parameters: normalizeSchema(declaration.parametersJsonSchema || declaration.parameters || { type: "object", properties: {} }),
        },
      };
    });
  });
  if (tools.length) request.tools = tools;
  const calling = body.toolConfig?.functionCallingConfig;
  if (calling?.allowedFunctionNames?.length) {
    throw new GeminiProtocolError("Copilot does not support Gemini allowedFunctionNames; restrict the declared tools instead.");
  }
  if (calling?.mode) {
    const modes = { AUTO: "auto", ANY: "required", NONE: "none" };
    const choice = modes[calling.mode];
    if (!choice) throw new GeminiProtocolError(`Unsupported function calling mode '${calling.mode}'.`);
    request.tool_choice = choice;
  }
  const config = body.generationConfig || {};
  const supportedConfig = new Set(["temperature", "topP", "maxOutputTokens", "stopSequences", "responseMimeType", "responseSchema", "responseJsonSchema", "topK", "thinkingConfig", "candidateCount"]);
  if (Object.keys(config).some(key => !supportedConfig.has(key))) throw new GeminiProtocolError("Unsupported Gemini generation options for Copilot inference.");
  if (config.candidateCount !== undefined && config.candidateCount !== 1) throw new GeminiProtocolError("Copilot routing supports one Gemini response candidate.");
  if (config.responseMimeType && !["text/plain", "application/json"].includes(config.responseMimeType)) throw new GeminiProtocolError(`Unsupported Gemini response MIME type '${config.responseMimeType}'.`);
  for (const [source, target] of [
    ["temperature", "temperature"],
    ["topP", "top_p"],
    ["maxOutputTokens", "max_tokens"],
    ["stopSequences", "stop"],
  ]) {
    if (config[source] !== undefined) request[target] = config[source];
  }
  if (config.responseMimeType === "application/json") {
    const schema = config.responseJsonSchema || config.responseSchema;
    request.response_format = schema ? { type: "json_schema", json_schema: { name: "gemini_response", schema: normalizeSchema(schema) } } : { type: "json_object" };
  }
  return request;
}

/** @param {unknown} value @returns {value is ChatResponse} */
function isChatResponse(value) {
  if (!value || typeof value !== "object" || !("choices" in value) || !Array.isArray(value.choices)) return false;
  return value.choices.every(choice => {
    if (!choice || typeof choice !== "object" || !choice.message || typeof choice.message !== "object") return false;
    const message = choice.message;
    if (message.content != null && typeof message.content !== "string") return false;
    if (message.reasoning_content !== undefined && typeof message.reasoning_content !== "string") return false;
    return (
      message.tool_calls === undefined ||
      (Array.isArray(message.tool_calls) &&
        message.tool_calls.every(call => call && typeof call.id === "string" && typeof call.type === "string" && call.function && typeof call.function.name === "string" && typeof call.function.arguments === "string"))
    );
  });
}

/** @param {unknown} body @param {string} model */
function fromChatResponse(body, model) {
  if (!isChatResponse(body)) throw new GeminiProtocolError("Copilot returned an invalid Chat Completions response.");
  const choice = body?.choices?.[0];
  if (!choice?.message) throw new GeminiProtocolError("Copilot returned no Chat Completions message.");
  const message = choice.message;
  /** @type {GeminiPart[]} */
  const parts = [];
  if (message.reasoning_content) parts.push({ text: message.reasoning_content, thought: true });
  if (message.content) parts.push({ text: message.content });
  for (const call of message.tool_calls || []) {
    if (call.type !== "function" || !call.function?.name || !call.id) throw new GeminiProtocolError("Copilot returned an invalid function call.");
    let args;
    try {
      args = JSON.parse(call.function.arguments);
    } catch (error) {
      throw new GeminiProtocolError(`Invalid Copilot function arguments: ${getErrorMessage(error)}`);
    }
    if (!args || typeof args !== "object" || Array.isArray(args)) throw new GeminiProtocolError("Copilot function arguments must be an object.");
    parts.push({ functionCall: { id: call.id, name: call.function.name, args } });
  }
  if (!parts.length) throw new GeminiProtocolError("Copilot returned an empty completion.");
  const reasons = { stop: "STOP", tool_calls: "STOP", length: "MAX_TOKENS", content_filter: "SAFETY" };
  const finishReason = reasons[choice.finish_reason || ""];
  if (!finishReason) throw new GeminiProtocolError(`Unsupported Copilot finish reason '${choice.finish_reason}'.`);
  const usage = body.usage;
  const thoughts = usage?.completion_tokens_details?.reasoning_tokens || 0;
  return {
    candidates: [{ index: 0, content: { role: "model", parts }, finishReason }],
    modelVersion: model,
    ...(usage
      ? {
          usageMetadata: {
            promptTokenCount: usage.prompt_tokens,
            candidatesTokenCount: usage.completion_tokens === undefined ? undefined : Math.max(0, usage.completion_tokens - thoughts),
            totalTokenCount: usage.total_tokens,
            cachedContentTokenCount: usage.prompt_tokens_details?.cached_tokens || 0,
            thoughtsTokenCount: thoughts,
          },
        }
      : {}),
  };
}

module.exports = { GeminiProtocolError, toChatRequest, fromChatResponse };
