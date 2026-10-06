// @ts-check

/** @typedef {import("./types/agent_session").AssistantRefusalData} AssistantRefusalData */

const isOpenAIRefusal = message => typeof message?.refusal === "string";
const isResponsesRefusal = message => Array.isArray(message?.content) && message.content.some(part => part?.type === "refusal" && typeof part.refusal === "string");
const isAnthropicRefusal = message => message?.stop_reason === "refusal";

/**
 * Map Chat Completions usage to canonical token fields.
 * @param {any} usage
 * @returns {Record<string, number>|undefined}
 */
function normalizeOpenAIChatUsage(usage) {
  if (!usage || typeof usage !== "object" || Array.isArray(usage)) return undefined;
  /** @type {Record<string, number>} */
  const normalized = {};
  for (const [nativeKey, key] of [
    ["prompt_tokens", "input_tokens"],
    ["completion_tokens", "output_tokens"],
  ]) {
    if (Number.isSafeInteger(usage[nativeKey]) && usage[nativeKey] >= 0) normalized[key] = usage[nativeKey];
  }
  const cached = usage.prompt_tokens_details?.cached_tokens;
  if (Number.isSafeInteger(cached) && cached >= 0) normalized.cache_read_input_tokens = cached;
  return Object.keys(normalized).length ? normalized : undefined;
}

/**
 * Classify only structured provider signals, never natural-language disclaimers.
 * @param {any} message
 * @param {string} [finishReason]
 * @returns {AssistantRefusalData | undefined}
 */
function getMessageRefusal(message, finishReason) {
  if (!message || (message.role !== undefined && message.role !== "assistant")) return undefined;
  const refusalPart = Array.isArray(message.content) ? message.content.find(part => part?.type === "refusal" && typeof part.refusal === "string") : undefined;
  const reason = finishReason === "content_filter" || message.finish_reason === "content_filter" ? "content_filter" : isAnthropicRefusal(message) || isOpenAIRefusal(message) || isResponsesRefusal(message) ? "refusal" : undefined;
  if (!reason) return undefined;
  /** @type {AssistantRefusalData} */
  const data = { reason };
  if (typeof message.refusal === "string") data.content = message.refusal;
  else if (refusalPart) data.content = refusalPart.refusal;
  else if (typeof message.content === "string") data.content = message.content;
  else if (Array.isArray(message.content)) {
    const texts = message.content.filter(block => block?.type === "text" && typeof block.text === "string").map(block => block.text);
    if (texts.length) data.content = texts.join("");
  }
  if (message.stop_reason === "refusal") {
    const details = message.stop_details;
    if (typeof details?.category === "string" || details?.category === null) data.policyCategory = details.category;
    if (typeof details?.explanation === "string" || details?.explanation === null) data.explanation = details.explanation;
  }
  return data;
}

/**
 * Supported OpenAI response envelopes and Anthropic Messages API responses.
 * Request messages and arbitrary nested tool/user content are not inspected.
 * @param {any} record
 * @returns {AssistantRefusalData[]}
 */
function getProviderRefusals(record) {
  if (!record || typeof record !== "object" || Array.isArray(record)) return [];
  if (Array.isArray(record.choices)) {
    return record.choices.flatMap(choice => {
      const data = getMessageRefusal(choice?.message ?? choice?.delta ?? {}, choice?.finish_reason);
      return data ? [{ ...data, ...(record.object === "chat.completion.chunk" && choice?.finish_reason == null ? { partial: true } : {}) }] : [];
    });
  }
  if (record.type === "message" && record.role === "assistant") {
    const data = getMessageRefusal(record);
    return data ? [data] : [];
  }
  if (record.type === "response.refusal.delta" && typeof record.delta === "string") return [{ reason: "refusal", content: record.delta, partial: true }];
  if (record.type === "response.refusal.done" && typeof record.refusal === "string") return [{ reason: "refusal", content: record.refusal }];
  if (["response.content_part.added", "response.content_part.done"].includes(record.type) && record.part?.type === "refusal" && typeof record.part.refusal === "string") {
    return [{ reason: "refusal", content: record.part.refusal, ...(record.type === "response.content_part.added" ? { partial: true } : {}) }];
  }
  if (["response.output_item.added", "response.output_item.done"].includes(record.type)) {
    return getProviderRefusals({ object: "response", output: [record.item] }).map(data => ({ ...data, ...(record.type === "response.output_item.added" ? { partial: true } : {}) }));
  }
  const response = ["response.completed", "response.incomplete", "response.failed"].includes(record.type) ? record.response : record;
  if (response?.object !== "response") return [];
  /** @type {AssistantRefusalData[]} */
  const refusals = [];
  for (const item of Array.isArray(response.output) ? response.output : []) {
    if (item?.type !== "message" || item.role !== "assistant" || !Array.isArray(item.content)) continue;
    for (const block of item.content) {
      if (block?.type === "refusal" && typeof block.refusal === "string") refusals.push({ reason: "refusal", content: block.refusal });
    }
  }
  if (response.incomplete_details?.reason === "content_filter") {
    if (!refusals.length) {
      const texts = (Array.isArray(response.output) ? response.output : [])
        .filter(item => item?.type === "message" && item.role === "assistant")
        .flatMap(item => (Array.isArray(item.content) ? item.content : []).filter(block => block?.type === "output_text" && typeof block.text === "string").map(block => block.text));
      refusals.push({ reason: "content_filter", ...(texts.length ? { content: texts.join("") } : {}) });
    } else for (const data of refusals) data.reason = "content_filter";
  }
  return refusals;
}

module.exports = { getMessageRefusal, getProviderRefusals, normalizeOpenAIChatUsage };
