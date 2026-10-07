import { describe, it, expect } from "vitest";
const { toChatRequest, fromChatResponse } = require("./gemini_copilot_protocol.cjs");

describe("Gemini Copilot protocol", () => {
  it("maps system instructions, images, tools, and generation options", () => {
    const request = toChatRequest(
      {
        systemInstruction: { parts: [{ text: "System" }] },
        contents: [{ role: "user", parts: [{ text: "Hello" }, { inlineData: { mimeType: "image/png", data: "YWJj" } }] }],
        tools: [{ functionDeclarations: [{ name: "read_file", parameters: { type: "OBJECT", properties: { path: { type: "STRING" } } } }] }],
        generationConfig: { temperature: 0.5, topP: 0.8, maxOutputTokens: 100 },
      },
      "gemini-3.8-flash"
    );
    expect(request).toEqual({
      model: "gemini-3.8-flash",
      stream: false,
      messages: [
        { role: "system", content: "System" },
        {
          role: "user",
          content: [
            { type: "text", text: "Hello" },
            { type: "image_url", image_url: { url: "data:image/png;base64,YWJj" } },
          ],
        },
      ],
      tools: [{ type: "function", function: { name: "read_file", description: "", parameters: { type: "object", properties: { path: { type: "string" } } } } }],
      temperature: 0.5,
      top_p: 0.8,
      max_tokens: 100,
    });
  });

  it("preserves parallel tool-call ids and out-of-order results", () => {
    const request = toChatRequest(
      {
        contents: [
          { role: "model", parts: [{ functionCall: { id: "a", name: "read_file", args: { path: "a" } } }, { functionCall: { id: "b", name: "read_file", args: { path: "b" } } }] },
          { role: "user", parts: [{ functionResponse: { id: "b", name: "read_file", response: { output: "B" } } }, { functionResponse: { id: "a", name: "read_file", response: { output: "A" } } }] },
        ],
      },
      "gemini-3.8-flash"
    );
    expect(request.messages[1]).toEqual({ role: "tool", tool_call_id: "b", content: '{"output":"B"}' });
    expect(request.messages[2]).toEqual({ role: "tool", tool_call_id: "a", content: '{"output":"A"}' });
  });

  it("pairs calls without ids by name and rejects unmatched responses", () => {
    const contents = [
      { role: "model", parts: [{ functionCall: { name: "test", args: {} } }] },
      { role: "user", parts: [{ functionResponse: { name: "test", response: { ok: true } } }] },
    ];
    expect(toChatRequest({ contents }, "gemini-3.8-flash").messages[1].tool_call_id).toBe("call_0_0");
    expect(() => toChatRequest({ contents: contents.slice(1) }, "gemini-3.8-flash")).toThrow("No preceding function call");
  });

  it("translates response text, reasoning, tool calls, and cache-aware usage", () => {
    const result = fromChatResponse(
      {
        choices: [{ message: { content: "Done", reasoning_content: "Thinking", tool_calls: [{ id: "a", type: "function", function: { name: "test", arguments: '{"path":"a"}' } }] }, finish_reason: "tool_calls" }],
        usage: { prompt_tokens: 100, completion_tokens: 20, total_tokens: 120, prompt_tokens_details: { cached_tokens: 50 }, completion_tokens_details: { reasoning_tokens: 5 } },
      },
      "gemini-3.8-flash"
    );
    expect(result.candidates[0]).toEqual({ index: 0, content: { role: "model", parts: [{ text: "Thinking", thought: true }, { text: "Done" }, { functionCall: { id: "a", name: "test", args: { path: "a" } } }] }, finishReason: "STOP" });
    expect(result.usageMetadata).toEqual({ promptTokenCount: 100, candidatesTokenCount: 15, totalTokenCount: 120, cachedContentTokenCount: 50, thoughtsTokenCount: 5 });
  });

  it("fails explicitly for hosted tools and unsupported content rather than dropping them", () => {
    expect(() => toChatRequest({ contents: [], tools: [{ urlContext: {} }] }, "gemini")).toThrow("hosted tools");
    expect(() => toChatRequest({ contents: [{ parts: [{ fileData: {} }] }] }, "gemini")).toThrow("Unsupported Gemini content");
    expect(() => toChatRequest({ contents: [], cachedContent: "cached" }, "gemini")).toThrow("cachedContent");
    expect(() => toChatRequest({ contents: [], generationConfig: { candidateCount: 2 } }, "gemini")).toThrow("one Gemini response candidate");
    expect(() => toChatRequest({ contents: [], generationConfig: { responseMimeType: "audio/wav" } }, "gemini")).toThrow("response MIME type");
    expect(() => toChatRequest({ contents: [], generationConfig: { seed: 12 } }, "gemini")).toThrow("generation options");
    expect(() => fromChatResponse({ choices: [] }, "gemini")).toThrow("no Chat Completions message");
    expect(() => fromChatResponse({ choices: [{ message: {}, finish_reason: "stop" }] }, "gemini")).toThrow("empty completion");
  });

  it("preserves truncation and rejects malformed upstream function arguments", () => {
    expect(fromChatResponse({ choices: [{ message: { content: "Partial" }, finish_reason: "length" }] }, "gemini").candidates[0].finishReason).toBe("MAX_TOKENS");
    expect(() => fromChatResponse({ choices: [{ message: { tool_calls: [{ id: "a", type: "function", function: { name: "test", arguments: "invalid" } }] }, finish_reason: "tool_calls" }] }, "gemini")).toThrow();
  });
});
