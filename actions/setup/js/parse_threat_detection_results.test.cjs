import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";

// Create mock functions
const mockExistsSync = vi.fn(() => false);
const mockReadFileSync = vi.fn(() => "");

// Mock fs module
vi.mock("fs", () => {
  return {
    existsSync: mockExistsSync,
    readFileSync: mockReadFileSync,
    default: {
      existsSync: mockExistsSync,
      readFileSync: mockReadFileSync,
    },
  };
});

// Mock file_helpers to avoid transitive fs issues
vi.mock("./file_helpers.cjs", () => {
  return {
    listFilesRecursively: vi.fn(() => []),
  };
});

// Mock the global core object
const mockCore = {
  info: vi.fn(),
  warning: vi.fn(),
  error: vi.fn(),
  setFailed: vi.fn(),
  setOutput: vi.fn(),
  exportVariable: vi.fn(),
};
global.core = mockCore;

const { parseDetectionLog, extractFromStreamJson, extractResultFromText, extractStructuredOutput, parseStructuredResultFile, reportAPIProxyGuardRejection } = require("./parse_threat_detection_results.cjs");

describe("extractResultFromText", () => {
  it("should extract a simple JSON object", () => {
    const text = 'THREAT_DETECTION_RESULT:{"prompt_injection":false,"reasons":[]}';
    const result = extractResultFromText(text);
    expect(result).toBe('THREAT_DETECTION_RESULT:{"prompt_injection":false,"reasons":[]}');
  });

  it("should stop at the matching closing brace and ignore trailing content", () => {
    const text = 'THREAT_DETECTION_RESULT:{"prompt_injection":false,"reasons":[]}\nSome trailing text';
    const result = extractResultFromText(text);
    expect(result).toBe('THREAT_DETECTION_RESULT:{"prompt_injection":false,"reasons":[]}');
    expect(result).not.toContain("trailing");
  });

  it("should handle nested objects correctly", () => {
    const text = 'THREAT_DETECTION_RESULT:{"a":{"b":{"c":1}}}';
    const result = extractResultFromText(text);
    expect(result).toBe('THREAT_DETECTION_RESULT:{"a":{"b":{"c":1}}}');
  });

  it("should not count braces inside JSON string values", () => {
    const text = 'THREAT_DETECTION_RESULT:{"reasons":["found {injection} here"]}';
    const result = extractResultFromText(text);
    expect(result).toBe('THREAT_DETECTION_RESULT:{"reasons":["found {injection} here"]}');
  });

  it("should handle escaped quotes inside strings", () => {
    const text = 'THREAT_DETECTION_RESULT:{"reasons":["he said \\"hello\\""]}';
    const result = extractResultFromText(text);
    expect(result).toBe('THREAT_DETECTION_RESULT:{"reasons":["he said \\"hello\\""]}');
  });

  it("should handle actual newlines inside string values", () => {
    const text = 'THREAT_DETECTION_RESULT:{"reasons":["line one\nline two"]}trailing';
    const result = extractResultFromText(text);
    expect(result).toBe('THREAT_DETECTION_RESULT:{"reasons":["line one\nline two"]}');
  });

  it("should return null when no opening brace found", () => {
    expect(extractResultFromText("THREAT_DETECTION_RESULT:null")).toBeNull();
    expect(extractResultFromText("THREAT_DETECTION_RESULT:[]")).toBeNull();
    expect(extractResultFromText("THREAT_DETECTION_RESULT:42")).toBeNull();
    expect(extractResultFromText("THREAT_DETECTION_RESULT:")).toBeNull();
  });

  it("should return null when closing brace is missing (truncated JSON)", () => {
    expect(extractResultFromText('THREAT_DETECTION_RESULT:{"key":')).toBeNull();
    expect(extractResultFromText('THREAT_DETECTION_RESULT:{"prompt_injection":true')).toBeNull();
  });
});

describe("extractFromStreamJson", () => {
  it("should extract result from type:result JSON envelope", () => {
    const line = '{"type":"result","subtype":"success","is_error":false,"result":"THREAT_DETECTION_RESULT:{\\"prompt_injection\\":false,\\"secret_leak\\":false,\\"malicious_patch\\":false,\\"reasons\\":[]}","stop_reason":"end_turn"}';
    const result = extractFromStreamJson(line);
    expect(result).toContain("THREAT_DETECTION_RESULT:");
  });

  it("should extract result when analysis text precedes the verdict line", () => {
    // The model may include explanatory text before THREAT_DETECTION_RESULT in the result field
    const line =
      '{"type":"result","subtype":"success","result":"**Analysis complete.**\\n\\nNo threats found.\\n\\nTHREAT_DETECTION_RESULT:{\\"prompt_injection\\":false,\\"secret_leak\\":false,\\"malicious_patch\\":false,\\"reasons\\":[]}","stop_reason":"end_turn"}';
    const result = extractFromStreamJson(line);
    // Ensure we extracted only the verdict line, not the preceding analysis text
    expect(result).toMatch(/^THREAT_DETECTION_RESULT:/);
    expect(result).not.toContain("**Analysis complete.**");
  });

  it("should allow parseDetectionLog to parse extracted verdict when analysis text precedes it", () => {
    const line =
      '{"type":"result","subtype":"success","result":"**Analysis complete.**\\n\\nNo threats found.\\n\\nTHREAT_DETECTION_RESULT:{\\"prompt_injection\\":false,\\"secret_leak\\":false,\\"malicious_patch\\":false,\\"reasons\\":[]}","stop_reason":"end_turn"}';
    const extracted = extractFromStreamJson(line);
    expect(extracted).not.toBeNull();
    const { verdict, error } = parseDetectionLog(extracted);
    expect(error).toBeUndefined();
    expect(verdict).toEqual({
      prompt_injection: false,
      secret_leak: false,
      malicious_patch: false,
      reasons: [],
    });
  });

  it("should return null for type:assistant JSON (not authoritative)", () => {
    const line = '{"type":"assistant","message":{"content":[{"type":"text","text":"THREAT_DETECTION_RESULT:{\\"prompt_injection\\":false}"}]}}';
    const result = extractFromStreamJson(line);
    expect(result).toBeNull();
  });

  it("should return null for non-JSON lines", () => {
    expect(extractFromStreamJson("just plain text")).toBeNull();
    expect(extractFromStreamJson("THREAT_DETECTION_RESULT:{...}")).toBeNull();
  });

  it("should return null for JSON without result field", () => {
    const line = '{"type":"result","subtype":"success"}';
    expect(extractFromStreamJson(line)).toBeNull();
  });

  it("should return null for type:result where result does not start with prefix", () => {
    const line = '{"type":"result","result":"some other output"}';
    expect(extractFromStreamJson(line)).toBeNull();
  });

  it("should return null for malformed JSON", () => {
    expect(extractFromStreamJson("{not valid json}")).toBeNull();
  });

  it("should handle reasons values with literal newlines introduced by outer JSON.parse", () => {
    // When the model output contains a reason string with an actual newline character,
    // stream-json encodes it as \n (JSON escape) in the result field.
    // After the outer JSON.parse, \n becomes an actual newline, splitting the verdict
    // JSON across multiple lines when we split obj.result by "\n".
    // The fix: rejoin lines from the prefix line onward and use brace-counting to
    // extract the complete JSON object.
    const resultWithLiteralNewline = 'THREAT_DETECTION_RESULT:{"prompt_injection":true,"secret_leak":false,"malicious_patch":false,"reasons":["Found injection in\nline 5"]}';
    // JSON.stringify encodes the actual newline as \n in the outer JSON, matching
    // how the stream-json format represents it on disk as a single log line.
    const logLine = JSON.stringify({ type: "result", subtype: "success", result: resultWithLiteralNewline });

    // Verify extractFromStreamJson returns the complete result (not truncated at the newline)
    const extracted = extractFromStreamJson(logLine);
    expect(extracted).not.toBeNull();
    expect(extracted).toMatch(/^THREAT_DETECTION_RESULT:/);
    expect(extracted).toContain("line 5");

    // Verify the full verdict parses correctly when the log line is passed to parseDetectionLog
    const { verdict, error } = parseDetectionLog(logLine);
    expect(error).toBeUndefined();
    expect(verdict).toBeDefined();
    expect(verdict.prompt_injection).toBe(true);
    expect(verdict.secret_leak).toBe(false);
    expect(verdict.malicious_patch).toBe(false);
    expect(verdict.reasons.length).toBeGreaterThan(0);
    // The newline in the reason should be preserved in the parsed output
    expect(verdict.reasons[0]).toContain("Found injection in");
    expect(verdict.reasons[0]).toContain("line 5");
  });

  it("should extract result from codex response.output_text.done events with log prefix", () => {
    const line =
      '2026-05-26T03:17:17.7671911Z TRACE codex_api::sse::responses: SSE event: {"type":"response.output_text.done","text":"THREAT_DETECTION_RESULT:{\\"prompt_injection\\":false,\\"secret_leak\\":false,\\"malicious_patch\\":false,\\"reasons\\":[]}"}';
    const result = extractFromStreamJson(line);
    expect(result).toBe('THREAT_DETECTION_RESULT:{"prompt_injection":false,"secret_leak":false,"malicious_patch":false,"reasons":[]}');
  });

  it("should extract result from codex item.completed events", () => {
    const line = '{"type":"item.completed","item":{"id":"item_3","type":"agent_message","text":"THREAT_DETECTION_RESULT:{\\"prompt_injection\\":false,\\"secret_leak\\":false,\\"malicious_patch\\":false,\\"reasons\\":[]}"}}';
    const result = extractFromStreamJson(line);
    expect(result).toBe('THREAT_DETECTION_RESULT:{"prompt_injection":false,"secret_leak":false,"malicious_patch":false,"reasons":[]}');
  });

  it("should extract structured output from response.output_text.done when no prefix present", () => {
    // Codex detection with response_schema: model outputs pure JSON, no THREAT_DETECTION_RESULT: prefix
    const line = '{"type":"response.output_text.done","text":"{\\"prompt_injection\\":false,\\"secret_leak\\":false,\\"malicious_patch\\":false,\\"reasons\\":[]}"}';
    const result = extractFromStreamJson(line);
    expect(result).toBe('THREAT_DETECTION_RESULT:{"prompt_injection":false,"secret_leak":false,"malicious_patch":false,"reasons":[]}');
  });

  it("should extract structured output from response.output_text.done with log prefix", () => {
    const line =
      '2026-05-26T03:17:17.7671911Z TRACE codex_api::sse::responses: SSE event: {"type":"response.output_text.done","text":"{\\"prompt_injection\\":true,\\"secret_leak\\":false,\\"malicious_patch\\":false,\\"reasons\\":[\\"prompt injection detected\\"]}"}';
    const result = extractFromStreamJson(line);
    expect(result).toBe('THREAT_DETECTION_RESULT:{"prompt_injection":true,"secret_leak":false,"malicious_patch":false,"reasons":["prompt injection detected"]}');
  });

  it("should still prefer prefixed format over structured output in response.output_text.done", () => {
    // If both THREAT_DETECTION_RESULT: prefix and valid JSON fields coexist, prefixed wins
    const line = '{"type":"response.output_text.done","text":"THREAT_DETECTION_RESULT:{\\"prompt_injection\\":false,\\"secret_leak\\":false,\\"malicious_patch\\":false,\\"reasons\\":[]}"}';
    const result = extractFromStreamJson(line);
    expect(result).toBe('THREAT_DETECTION_RESULT:{"prompt_injection":false,"secret_leak":false,"malicious_patch":false,"reasons":[]}');
  });

  it("should extract structured output from item.completed when no prefix present", () => {
    const line = '{"type":"item.completed","item":{"id":"item_1","type":"agent_message","text":"{\\"prompt_injection\\":false,\\"secret_leak\\":true,\\"malicious_patch\\":false,\\"reasons\\":[\\"secret found\\"]}"}}';
    const result = extractFromStreamJson(line);
    expect(result).toBe('THREAT_DETECTION_RESULT:{"prompt_injection":false,"secret_leak":true,"malicious_patch":false,"reasons":["secret found"]}');
  });

  it("should extract structured output from response.content_part.done when no prefix present", () => {
    const line = '{"type":"response.content_part.done","part":{"type":"output_text","text":"{\\"prompt_injection\\":false,\\"secret_leak\\":false,\\"malicious_patch\\":true,\\"reasons\\":[\\"malicious dependency\\"]}"}}';
    const result = extractFromStreamJson(line);
    expect(result).toBe('THREAT_DETECTION_RESULT:{"prompt_injection":false,"secret_leak":false,"malicious_patch":true,"reasons":["malicious dependency"]}');
  });

  it("should extract result from pi turn_end assistant content array", () => {
    const line = '{"type":"turn_end","message":{"role":"assistant","content":[{"type":"text","text":"THREAT_DETECTION_RESULT:{\\"prompt_injection\\":false,\\"secret_leak\\":false,\\"malicious_patch\\":false,\\"reasons\\":[]}"}]}}';
    const result = extractFromStreamJson(line);
    expect(result).toBe('THREAT_DETECTION_RESULT:{"prompt_injection":false,"secret_leak":false,"malicious_patch":false,"reasons":[]}');
  });
});

describe("extractStructuredOutput", () => {
  it("should wrap a valid threat detection JSON with the result prefix", () => {
    const text = '{"prompt_injection":false,"secret_leak":false,"malicious_patch":false,"reasons":[]}';
    const result = extractStructuredOutput(text);
    expect(result).toBe('THREAT_DETECTION_RESULT:{"prompt_injection":false,"secret_leak":false,"malicious_patch":false,"reasons":[]}');
  });

  it("should handle threat detected with reasons", () => {
    const text = '{"prompt_injection":true,"secret_leak":false,"malicious_patch":false,"reasons":["injected command"]}';
    const result = extractStructuredOutput(text);
    expect(result).toBe('THREAT_DETECTION_RESULT:{"prompt_injection":true,"secret_leak":false,"malicious_patch":false,"reasons":["injected command"]}');
  });

  it("should return null for text that does not start with {", () => {
    expect(extractStructuredOutput("plain text")).toBeNull();
    expect(extractStructuredOutput("THREAT_DETECTION_RESULT:{...}")).toBeNull();
    expect(extractStructuredOutput('["array"]')).toBeNull();
  });

  it("should return null for JSON missing required fields", () => {
    expect(extractStructuredOutput('{"prompt_injection":false}')).toBeNull();
    expect(extractStructuredOutput('{"secret_leak":false,"malicious_patch":false}')).toBeNull();
    expect(extractStructuredOutput("{}")).toBeNull();
  });

  it("should return null for non-object JSON values", () => {
    expect(extractStructuredOutput("null")).toBeNull();
    expect(extractStructuredOutput("true")).toBeNull();
    expect(extractStructuredOutput('"string"')).toBeNull();
  });

  it("should return null for invalid JSON", () => {
    expect(extractStructuredOutput("{not valid json}")).toBeNull();
  });

  it("should allow additional fields beyond required ones", () => {
    // The schema uses additionalProperties:false server-side, but the parser is lenient
    const text = '{"prompt_injection":false,"secret_leak":false,"malicious_patch":false,"reasons":[],"extra_field":"ignored"}';
    const result = extractStructuredOutput(text);
    expect(result).not.toBeNull();
    expect(result).toContain("THREAT_DETECTION_RESULT:");
  });

  it("should round-trip through parseDetectionLog", () => {
    // A full structured-output log line as emitted by Codex with response_schema
    const logLine = '2026-05-26T12:00:00Z TRACE codex_api::sse: {"type":"response.output_text.done","text":"{\\"prompt_injection\\":false,\\"secret_leak\\":false,\\"malicious_patch\\":false,\\"reasons\\":[]}"}';
    const { verdict, error } = parseDetectionLog(logLine);
    expect(error).toBeUndefined();
    expect(verdict).toEqual({
      prompt_injection: false,
      secret_leak: false,
      malicious_patch: false,
      reasons: [],
    });
  });

  it("should round-trip a threat detection through parseDetectionLog", () => {
    const logLine = '{"type":"response.output_text.done","text":"{\\"prompt_injection\\":true,\\"secret_leak\\":false,\\"malicious_patch\\":false,\\"reasons\\":[\\"injected payload\\"]}"}';
    const { verdict, error } = parseDetectionLog(logLine);
    expect(error).toBeUndefined();
    expect(verdict).toEqual({
      prompt_injection: true,
      secret_leak: false,
      malicious_patch: false,
      reasons: ["injected payload"],
    });
  });
});

describe("parseDetectionLog", () => {
  describe("valid results", () => {
    it("should parse a clean verdict (no threats)", () => {
      const content = 'Some debug output\nTHREAT_DETECTION_RESULT:{"prompt_injection":false,"secret_leak":false,"malicious_patch":false,"reasons":[]}\nMore output';
      const { verdict, error } = parseDetectionLog(content);

      expect(error).toBeUndefined();
      expect(verdict).toEqual({
        prompt_injection: false,
        secret_leak: false,
        malicious_patch: false,
        reasons: [],
      });
    });

    it("should parse a verdict with threats detected", () => {
      const content = 'THREAT_DETECTION_RESULT:{"prompt_injection":true,"secret_leak":false,"malicious_patch":true,"reasons":["found backdoor","injected prompt"]}';
      const { verdict, error } = parseDetectionLog(content);

      expect(error).toBeUndefined();
      expect(verdict.prompt_injection).toBe(true);
      expect(verdict.secret_leak).toBe(false);
      expect(verdict.malicious_patch).toBe(true);
      expect(verdict.reasons).toEqual(["found backdoor", "injected prompt"]);
    });

    it("should handle leading/trailing whitespace on the result line", () => {
      const content = '  THREAT_DETECTION_RESULT:{"prompt_injection":false,"secret_leak":false,"malicious_patch":false,"reasons":[]}  ';
      const { verdict, error } = parseDetectionLog(content);

      expect(error).toBeUndefined();
      expect(verdict.prompt_injection).toBe(false);
    });

    it("should parse a verdict wrapped in Markdown bold emphasis (**)", () => {
      const content = '**THREAT_DETECTION_RESULT:{"prompt_injection":false,"secret_leak":false,"malicious_patch":false,"reasons":[]}**';
      const { verdict, error } = parseDetectionLog(content);

      expect(error).toBeUndefined();
      expect(verdict).toEqual({
        prompt_injection: false,
        secret_leak: false,
        malicious_patch: false,
        reasons: [],
      });
    });

    it("should parse a verdict wrapped in Markdown underscore emphasis (__)", () => {
      const content = '__THREAT_DETECTION_RESULT:{"prompt_injection":true,"secret_leak":false,"malicious_patch":false,"reasons":["found injection"]}__';
      const { verdict, error } = parseDetectionLog(content);

      expect(error).toBeUndefined();
      expect(verdict.prompt_injection).toBe(true);
      expect(verdict.reasons).toEqual(["found injection"]);
    });

    it("should handle leading whitespace before a Markdown-wrapped verdict", () => {
      const content = '  **THREAT_DETECTION_RESULT:{"prompt_injection":false,"secret_leak":false,"malicious_patch":false,"reasons":[]}**  ';
      const { verdict, error } = parseDetectionLog(content);

      expect(error).toBeUndefined();
      expect(verdict.prompt_injection).toBe(false);
    });

    it("should parse a Markdown-emphasis-wrapped verdict from a stream-json 'result' line", () => {
      const line = JSON.stringify({
        type: "result",
        subtype: "success",
        result: '**THREAT_DETECTION_RESULT:{"prompt_injection":false,"secret_leak":false,"malicious_patch":false,"reasons":[]}**',
        stop_reason: "end_turn",
      });
      const { verdict, error } = parseDetectionLog(line);

      expect(error).toBeUndefined();
      expect(verdict).toEqual({
        prompt_injection: false,
        secret_leak: false,
        malicious_patch: false,
        reasons: [],
      });
    });

    it("should not treat a result-format example embedded in prose as the authoritative verdict", () => {
      const content =
        'The output format should look like this: some text THREAT_DETECTION_RESULT:{"prompt_injection":false,...} is an example.\nActual verdict below.\nTHREAT_DETECTION_RESULT:{"prompt_injection":true,"secret_leak":false,"malicious_patch":false,"reasons":["real threat"]}';
      const { verdict, error } = parseDetectionLog(content);

      // The prose example does not start the line with the prefix (with or without
      // emphasis), so only the real verdict line should be matched.
      expect(error).toBeUndefined();
      expect(verdict.prompt_injection).toBe(true);
      expect(verdict.reasons).toEqual(["real threat"]);
    });

    it("should reject non-boolean values with a type error", () => {
      const content = 'THREAT_DETECTION_RESULT:{"prompt_injection":1,"secret_leak":0,"malicious_patch":"yes","reasons":"not-array"}';
      const { verdict, error } = parseDetectionLog(content);

      expect(verdict).toBeUndefined();
      expect(error).toContain('Invalid type for "prompt_injection"');
      expect(error).toContain("expected boolean");
    });

    it('should reject string "false" as non-boolean', () => {
      const content = 'THREAT_DETECTION_RESULT:{"prompt_injection":"false","secret_leak":false,"malicious_patch":false,"reasons":[]}';
      const { verdict, error } = parseDetectionLog(content);

      expect(verdict).toBeUndefined();
      expect(error).toContain('Invalid type for "prompt_injection"');
      expect(error).toContain("got string");
    });

    it('should reject string "true" as non-boolean', () => {
      const content = 'THREAT_DETECTION_RESULT:{"prompt_injection":"true","secret_leak":false,"malicious_patch":false,"reasons":[]}';
      const { verdict, error } = parseDetectionLog(content);

      expect(verdict).toBeUndefined();
      expect(error).toContain('Invalid type for "prompt_injection"');
    });

    it("should parse Gemini stream-json assistant chunks when verdict is split across messages", () => {
      const content = [
        // User prompt can contain the expected output format example and must be ignored.
        JSON.stringify({
          type: "message",
          role: "user",
          content: 'Output format example: THREAT_DETECTION_RESULT:{"prompt_injection":false}',
        }),
        JSON.stringify({ type: "message", role: "assistant", content: "THREAT_DETECTION_", delta: true }),
        JSON.stringify({
          type: "message",
          role: "assistant",
          content: 'RESULT:{"prompt_injection":false,"secret_leak":false,"malicious_patch":false,"reasons',
          delta: true,
        }),
        JSON.stringify({ type: "message", role: "assistant", content: '":[]}', delta: true }),
        JSON.stringify({ type: "result", status: "success", stats: { total_tokens: 123 } }),
      ].join("\n");

      const { verdict, error } = parseDetectionLog(content);

      expect(error).toBeUndefined();
      expect(verdict).toEqual({
        prompt_injection: false,
        secret_leak: false,
        malicious_patch: false,
        reasons: [],
      });
    });

    it("should parse pi turn_end verdict and ignore user prompt examples", () => {
      const content = [
        JSON.stringify({
          type: "message_end",
          message: {
            role: "user",
            content: [
              {
                type: "text",
                text: 'Output format example: THREAT_DETECTION_RESULT:{"prompt_injection":false}',
              },
            ],
          },
        }),
        JSON.stringify({
          type: "turn_end",
          message: {
            role: "assistant",
            content: [
              {
                type: "text",
                text: 'THREAT_DETECTION_RESULT:{"prompt_injection":false,"secret_leak":false,"malicious_patch":false,"reasons":[]}',
              },
            ],
          },
        }),
      ].join("\n");

      const { verdict, error } = parseDetectionLog(content);

      expect(error).toBeUndefined();
      expect(verdict).toEqual({
        prompt_injection: false,
        secret_leak: false,
        malicious_patch: false,
        reasons: [],
      });
    });
  });

  describe("no result line", () => {
    it("should return error when no THREAT_DETECTION_RESULT line exists", () => {
      const content = "Some debug output\nNo result here\nMore output";
      const { verdict, error } = parseDetectionLog(content);

      expect(verdict).toBeUndefined();
      expect(error).toContain("No THREAT_DETECTION_RESULT found");
    });

    it("should return error for empty content", () => {
      const { verdict, error } = parseDetectionLog("");

      expect(verdict).toBeUndefined();
      expect(error).toContain("No THREAT_DETECTION_RESULT found");
    });

    it("should return error when content has only whitespace", () => {
      const { verdict, error } = parseDetectionLog("   \n  \n  ");

      expect(verdict).toBeUndefined();
      expect(error).toContain("No THREAT_DETECTION_RESULT found");
    });
  });

  describe("multiple result lines", () => {
    it("should deduplicate identical THREAT_DETECTION_RESULT lines", () => {
      // --debug-file and tee both write to the same file, causing duplicates
      const content = [
        'THREAT_DETECTION_RESULT:{"prompt_injection":false,"secret_leak":false,"malicious_patch":false,"reasons":[]}',
        'THREAT_DETECTION_RESULT:{"prompt_injection":false,"secret_leak":false,"malicious_patch":false,"reasons":[]}',
        'THREAT_DETECTION_RESULT:{"prompt_injection":false,"secret_leak":false,"malicious_patch":false,"reasons":[]}',
      ].join("\n");
      const { verdict, error } = parseDetectionLog(content);

      expect(error).toBeUndefined();
      expect(verdict).toEqual({
        prompt_injection: false,
        secret_leak: false,
        malicious_patch: false,
        reasons: [],
      });
    });

    it("should error when conflicting THREAT_DETECTION_RESULT lines found", () => {
      const content = [
        'THREAT_DETECTION_RESULT:{"prompt_injection":false,"secret_leak":false,"malicious_patch":false,"reasons":[]}',
        'THREAT_DETECTION_RESULT:{"prompt_injection":true,"secret_leak":false,"malicious_patch":false,"reasons":["injection"]}',
      ].join("\n");
      const { verdict, error } = parseDetectionLog(content);

      expect(verdict).toBeUndefined();
      expect(error).toContain("Multiple conflicting THREAT_DETECTION_RESULT entries");
    });

    it("should include unique lines in error for debugging", () => {
      const content = [
        'THREAT_DETECTION_RESULT:{"prompt_injection":false,"secret_leak":false,"malicious_patch":false,"reasons":[]}',
        "some other output",
        'THREAT_DETECTION_RESULT:{"prompt_injection":true,"secret_leak":false,"malicious_patch":false,"reasons":[]}',
      ].join("\n");
      const { error } = parseDetectionLog(content);

      expect(error).toContain("[1]");
      expect(error).toContain("[2]");
    });
  });

  describe("invalid JSON", () => {
    it("should return error when JSON is malformed", () => {
      const content = "THREAT_DETECTION_RESULT:{not valid json}";
      const { verdict, error } = parseDetectionLog(content);

      expect(verdict).toBeUndefined();
      expect(error).toContain("Failed to parse JSON from THREAT_DETECTION_RESULT");
      expect(error).toContain("Raw value:");
    });

    it("should return error when JSON is empty", () => {
      const content = "THREAT_DETECTION_RESULT:";
      const { verdict, error } = parseDetectionLog(content);

      expect(verdict).toBeUndefined();
      expect(error).toContain("Failed to parse JSON");
    });

    it("should return error when JSON is truncated", () => {
      const content = 'THREAT_DETECTION_RESULT:{"prompt_injection":true,"secret_leak":';
      const { verdict, error } = parseDetectionLog(content);

      expect(verdict).toBeUndefined();
      expect(error).toContain("Failed to parse JSON");
    });

    it("should return error when JSON is null", () => {
      const content = "THREAT_DETECTION_RESULT:null";
      const { verdict, error } = parseDetectionLog(content);

      expect(verdict).toBeUndefined();
      expect(error).toContain("must be an object");
      expect(error).toContain("got null");
    });

    it("should return error when JSON is an array", () => {
      const content = "THREAT_DETECTION_RESULT:[]";
      const { verdict, error } = parseDetectionLog(content);

      expect(verdict).toBeUndefined();
      expect(error).toContain("must be an object");
      expect(error).toContain("got array");
    });

    it("should return error when JSON is a string", () => {
      const content = 'THREAT_DETECTION_RESULT:"clean"';
      const { verdict, error } = parseDetectionLog(content);

      expect(verdict).toBeUndefined();
      expect(error).toContain("must be an object");
      expect(error).toContain("got string");
    });

    it("should return error when JSON is a number", () => {
      const content = "THREAT_DETECTION_RESULT:42";
      const { verdict, error } = parseDetectionLog(content);

      expect(verdict).toBeUndefined();
      expect(error).toContain("must be an object");
      expect(error).toContain("got number");
    });
  });

  describe("stream-json format (--output-format stream-json)", () => {
    it("should extract result from type:result JSON envelope", () => {
      const content = [
        "2026-03-23T00:04:39.809Z [DEBUG] Fast mode unavailable",
        '{"type":"assistant","message":{"model":"claude-sonnet-4-6","content":[{"type":"text","text":"THREAT_DETECTION_RESULT:{\\"prompt_injection\\":false,\\"secret_leak\\":false,\\"malicious_patch\\":false,\\"reasons\\":[]}"}]}}',
        '{"type":"result","subtype":"success","is_error":false,"result":"THREAT_DETECTION_RESULT:{\\"prompt_injection\\":false,\\"secret_leak\\":false,\\"malicious_patch\\":false,\\"reasons\\":[]}","stop_reason":"end_turn"}',
        "2026-03-23T00:04:42.251Z [DEBUG] LSP server manager shut down successfully",
      ].join("\n");
      const { verdict, error } = parseDetectionLog(content);

      expect(error).toBeUndefined();
      expect(verdict).toEqual({
        prompt_injection: false,
        secret_leak: false,
        malicious_patch: false,
        reasons: [],
      });
    });

    it("should extract threats from stream-json format", () => {
      const content = [
        '{"type":"result","subtype":"success","result":"THREAT_DETECTION_RESULT:{\\"prompt_injection\\":true,\\"secret_leak\\":false,\\"malicious_patch\\":false,\\"reasons\\":[\\"Injected JSON payload in prompt.txt\\"]}","stop_reason":"end_turn"}',
      ].join("\n");
      const { verdict, error } = parseDetectionLog(content);

      expect(error).toBeUndefined();
      expect(verdict.prompt_injection).toBe(true);
      expect(verdict.reasons).toEqual(["Injected JSON payload in prompt.txt"]);
    });

    it("should not double-count from assistant and result envelopes", () => {
      // Both assistant and result contain the same THREAT_DETECTION_RESULT
      // The parser should only extract from type:result (authoritative)
      const content = [
        '{"type":"assistant","message":{"content":[{"type":"text","text":"THREAT_DETECTION_RESULT:{\\"prompt_injection\\":false,\\"secret_leak\\":false,\\"malicious_patch\\":false,\\"reasons\\":[]}"}]}}',
        '{"type":"result","result":"THREAT_DETECTION_RESULT:{\\"prompt_injection\\":false,\\"secret_leak\\":false,\\"malicious_patch\\":false,\\"reasons\\":[]}"}',
      ].join("\n");
      const { verdict, error } = parseDetectionLog(content);

      expect(error).toBeUndefined();
      expect(verdict).toBeDefined();
    });

    it("should parse codex response events with timestamp prefixes", () => {
      const content = [
        '2026-05-26T03:17:06.382419Z DEBUG codex_core::session::handlers: {"model":"gpt-5-mini","instructions":"Output format: THREAT_DETECTION_RESULT:{\\"prompt_injection\\":false}"}',
        '2026-05-26T03:17:17.7671911Z TRACE codex_api::sse::responses: SSE event: {"type":"response.output_text.done","text":"THREAT_DETECTION_RESULT:{\\"prompt_injection\\":false,\\"secret_leak\\":false,\\"malicious_patch\\":false,\\"reasons\\":[]}"}',
      ].join("\n");
      const { verdict, error } = parseDetectionLog(content);

      expect(error).toBeUndefined();
      expect(verdict).toEqual({
        prompt_injection: false,
        secret_leak: false,
        malicious_patch: false,
        reasons: [],
      });
    });
  });

  describe("non-stream format (--print / --output-format text)", () => {
    it("should extract from a realistic non-stream detection log", () => {
      const content = [
        "● Read workflow prompt and agent output files (shell)",
        "  │ cat /tmp/gh-aw/threat-detection/aw-prompts/prompt.txt",
        "  └ 195 lines...",
        "",
        "Looking at the content carefully, I notice a classic prompt injection pattern.",
        "",
        'THREAT_DETECTION_RESULT:{"prompt_injection":true,"secret_leak":false,"malicious_patch":false,"reasons":["Injected JSON payload in prompt.txt"]}',
        "",
        "Total usage est:        1 token",
      ].join("\n");
      const { verdict, error } = parseDetectionLog(content);

      expect(error).toBeUndefined();
      expect(verdict.prompt_injection).toBe(true);
      expect(verdict.secret_leak).toBe(false);
      expect(verdict.malicious_patch).toBe(false);
      expect(verdict.reasons).toEqual(["Injected JSON payload in prompt.txt"]);
    });
  });
});

describe("parseStructuredResultFile", () => {
  it("should return null when file does not exist", () => {
    // Uses real fs - the path /tmp/gh-aw/threat-detection/detection_result.json
    // does not exist on the test runner, so existsSync returns false reliably.
    const result = parseStructuredResultFile("/tmp/gh-aw/threat-detection/detection_result.json");
    expect(result).toBeNull();
  });

  // Note: The following tests are skipped because mocking fs for CJS modules
  // is difficult in vitest (vi.mock("fs") does not intercept require("fs") for
  // built-in modules in this CJS+vitest setup, same as safe_output_validator.test.cjs).
  // These tests document the expected behavior of parseStructuredResultFile for
  // each scenario.
  describe.skip("with structured result file present (CJS fs mock limitation)", () => {
    it("should return verdict for valid clean JSON", () => {
      mockExistsSync.mockReturnValue(true);
      mockReadFileSync.mockReturnValue('{"prompt_injection":false,"secret_leak":false,"malicious_patch":false,"reasons":[]}');
      const result = parseStructuredResultFile("/tmp/gh-aw/threat-detection/detection_result.json");
      expect(result).not.toBeNull();
      expect(result.error).toBeUndefined();
      expect(result.verdict).toEqual({
        prompt_injection: false,
        secret_leak: false,
        malicious_patch: false,
        reasons: [],
      });
    });

    it("should return verdict with reasons populated", () => {
      mockExistsSync.mockReturnValue(true);
      mockReadFileSync.mockReturnValue('{"prompt_injection":true,"secret_leak":false,"malicious_patch":false,"reasons":["Injection detected in prompt"]}');
      const result = parseStructuredResultFile("/tmp/gh-aw/threat-detection/detection_result.json");
      expect(result).not.toBeNull();
      expect(result.error).toBeUndefined();
      expect(result.verdict.prompt_injection).toBe(true);
      expect(result.verdict.reasons).toEqual(["Injection detected in prompt"]);
    });

    it("should return error object for invalid JSON", () => {
      mockExistsSync.mockReturnValue(true);
      mockReadFileSync.mockReturnValue("{not valid json}");
      const result = parseStructuredResultFile("/tmp/gh-aw/threat-detection/detection_result.json");
      expect(result).not.toBeNull();
      expect(result.error).toBeDefined();
      expect(result.verdict).toBeUndefined();
    });

    it("should return error object for empty file", () => {
      mockExistsSync.mockReturnValue(true);
      mockReadFileSync.mockReturnValue("   ");
      const result = parseStructuredResultFile("/tmp/gh-aw/threat-detection/detection_result.json");
      expect(result).not.toBeNull();
      expect(result.error).toContain("empty");
    });

    it("should return error when required boolean fields are missing", () => {
      mockExistsSync.mockReturnValue(true);
      mockReadFileSync.mockReturnValue('{"prompt_injection":false,"secret_leak":false}');
      const result = parseStructuredResultFile("/tmp/gh-aw/threat-detection/detection_result.json");
      expect(result).not.toBeNull();
      expect(result.error).toBeDefined();
      expect(result.error).toContain("malicious_patch");
    });

    it("should return error when a boolean field has wrong type", () => {
      mockExistsSync.mockReturnValue(true);
      mockReadFileSync.mockReturnValue('{"prompt_injection":"false","secret_leak":false,"malicious_patch":false,"reasons":[]}');
      const result = parseStructuredResultFile("/tmp/gh-aw/threat-detection/detection_result.json");
      expect(result).not.toBeNull();
      expect(result.error).toBeDefined();
      expect(result.error).toContain("prompt_injection");
    });

    it("should return error when JSON root is an array", () => {
      mockExistsSync.mockReturnValue(true);
      mockReadFileSync.mockReturnValue('[{"prompt_injection":false}]');
      const result = parseStructuredResultFile("/tmp/gh-aw/threat-detection/detection_result.json");
      expect(result).not.toBeNull();
      expect(result.error).toBeDefined();
      expect(result.error).toContain("array");
    });

    it("should return error when readFileSync throws", () => {
      mockExistsSync.mockReturnValue(true);
      mockReadFileSync.mockImplementation(() => {
        throw new Error("EACCES: permission denied");
      });
      const result = parseStructuredResultFile("/tmp/gh-aw/threat-detection/detection_result.json");
      expect(result).not.toBeNull();
      expect(result.error).toBeDefined();
      expect(result.error).toContain("Failed to read");
    });

    it("should treat missing reasons field as empty array", () => {
      mockExistsSync.mockReturnValue(true);
      mockReadFileSync.mockReturnValue('{"prompt_injection":false,"secret_leak":false,"malicious_patch":false}');
      const result = parseStructuredResultFile("/tmp/gh-aw/threat-detection/detection_result.json");
      expect(result).not.toBeNull();
      expect(result.error).toBeUndefined();
      expect(result.verdict.reasons).toEqual([]);
    });
  });
});

describe("main", () => {
  let mod;

  beforeEach(async () => {
    vi.clearAllMocks();
    mockExistsSync.mockReturnValue(false);
    mockReadFileSync.mockReturnValue("");
    // Reset environment variables
    delete process.env.RUN_DETECTION;
    delete process.env.GH_AW_DETECTION_CONTINUE_ON_ERROR;
    delete process.env.DETECTION_AGENTIC_EXECUTION_OUTCOME;
    // Re-import to get fresh module with mocks
    mod = await import("./parse_threat_detection_results.cjs");
  });

  describe("when detection was not needed (RUN_DETECTION != 'true')", () => {
    it("should set conclusion=skipped when RUN_DETECTION is undefined", async () => {
      delete process.env.RUN_DETECTION;

      await mod.main();

      expect(mockCore.setOutput).toHaveBeenCalledWith("conclusion", "skipped");
      expect(mockCore.setOutput).toHaveBeenCalledWith("success", "true");
      expect(mockCore.exportVariable).toHaveBeenCalledWith("GH_AW_DETECTION_CONCLUSION", "skipped");
      expect(mockCore.exportVariable).toHaveBeenCalledWith("GH_AW_DETECTION_REASON", "");
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should set conclusion=skipped when RUN_DETECTION is 'false'", async () => {
      process.env.RUN_DETECTION = "false";

      await mod.main();

      expect(mockCore.setOutput).toHaveBeenCalledWith("conclusion", "skipped");
      expect(mockCore.setOutput).toHaveBeenCalledWith("success", "true");
      expect(mockCore.exportVariable).toHaveBeenCalledWith("GH_AW_DETECTION_CONCLUSION", "skipped");
      expect(mockCore.exportVariable).toHaveBeenCalledWith("GH_AW_DETECTION_REASON", "");
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should set conclusion=skipped when RUN_DETECTION is empty string", async () => {
      process.env.RUN_DETECTION = "";

      await mod.main();

      expect(mockCore.setOutput).toHaveBeenCalledWith("conclusion", "skipped");
      expect(mockCore.setOutput).toHaveBeenCalledWith("success", "true");
      expect(mockCore.exportVariable).toHaveBeenCalledWith("GH_AW_DETECTION_CONCLUSION", "skipped");
      expect(mockCore.exportVariable).toHaveBeenCalledWith("GH_AW_DETECTION_REASON", "");
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });
  });

  describe("when detection is needed (RUN_DETECTION === 'true')", () => {
    beforeEach(() => {
      process.env.RUN_DETECTION = "true";
    });

    it("should warn when detection log file does not exist (default continue-on-error)", async () => {
      mockExistsSync.mockReturnValue(false);

      await mod.main();

      expect(mockCore.setOutput).toHaveBeenCalledWith("conclusion", "warning");
      expect(mockCore.setOutput).toHaveBeenCalledWith("success", "false");
      expect(mockCore.setOutput).toHaveBeenCalledWith("reason", "agent_failure");
      expect(mockCore.exportVariable).toHaveBeenCalledWith("GH_AW_DETECTION_CONCLUSION", "warning");
      expect(mockCore.exportVariable).toHaveBeenCalledWith("GH_AW_DETECTION_REASON", "agent_failure");
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should fail when detection log file does not exist (continue-on-error false)", async () => {
      process.env.GH_AW_DETECTION_CONTINUE_ON_ERROR = "false";
      mockExistsSync.mockReturnValue(false);

      await mod.main();

      expect(mockCore.setOutput).toHaveBeenCalledWith("conclusion", "failure");
      expect(mockCore.setOutput).toHaveBeenCalledWith("success", "false");
      expect(mockCore.setOutput).toHaveBeenCalledWith("reason", "agent_failure");
      expect(mockCore.exportVariable).toHaveBeenCalledWith("GH_AW_DETECTION_CONCLUSION", "failure");
      expect(mockCore.exportVariable).toHaveBeenCalledWith("GH_AW_DETECTION_REASON", "agent_failure");
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Detection log file not found"));
    });

    it("should fail when detection execution failed in warn mode", async () => {
      process.env.DETECTION_AGENTIC_EXECUTION_OUTCOME = "failure";
      mockExistsSync.mockReturnValue(false);

      await mod.main();

      expect(mockCore.setOutput).toHaveBeenCalledWith("conclusion", "failure");
      expect(mockCore.setOutput).toHaveBeenCalledWith("success", "false");
      expect(mockCore.setOutput).toHaveBeenCalledWith("reason", "agent_failure");
      expect(mockCore.exportVariable).toHaveBeenCalledWith("GH_AW_DETECTION_CONCLUSION", "failure");
      expect(mockCore.exportVariable).toHaveBeenCalledWith("GH_AW_DETECTION_REASON", "agent_failure");
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Detection log file not found"));
    });

    it("should fail on parse errors when detection execution failed in warn mode", async () => {
      process.env.DETECTION_AGENTIC_EXECUTION_OUTCOME = "failure";
      mockCore.info.mockImplementationOnce(() => {
        throw new Error("unexpected parse failure");
      });

      await mod.main();

      expect(mockCore.setOutput).toHaveBeenCalledWith("conclusion", "failure");
      expect(mockCore.setOutput).toHaveBeenCalledWith("success", "false");
      expect(mockCore.setOutput).toHaveBeenCalledWith("reason", "parse_error");
      expect(mockCore.exportVariable).toHaveBeenCalledWith("GH_AW_DETECTION_CONCLUSION", "failure");
      expect(mockCore.exportVariable).toHaveBeenCalledWith("GH_AW_DETECTION_REASON", "parse_error");
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Unexpected error in threat detection parse"));
    });

    // Note: The following tests are skipped because mocking fs for CJS modules
    // is difficult in vitest (same issue as safe_output_validator.test.cjs).
    // The core parsing logic is thoroughly tested via parseDetectionLog above.
    // These tests document the expected behavior of main() for each scenario.
    describe.skip("with detection log file present (CJS fs mock limitation)", () => {
      it("should fail when detection log has no result line", async () => {
        mockExistsSync.mockReturnValue(true);
        mockReadFileSync.mockReturnValue("just some debug output\nno result here\n");

        await mod.main();

        expect(mockCore.setOutput).toHaveBeenCalledWith("conclusion", "failure");
        expect(mockCore.setOutput).toHaveBeenCalledWith("success", "false");
        expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("No THREAT_DETECTION_RESULT found"));
      });

      it("should fail when detection log has multiple result lines", async () => {
        mockExistsSync.mockReturnValue(true);
        mockReadFileSync.mockReturnValue(
          ['THREAT_DETECTION_RESULT:{"prompt_injection":false,"secret_leak":false,"malicious_patch":false,"reasons":[]}', 'THREAT_DETECTION_RESULT:{"prompt_injection":true,"secret_leak":false,"malicious_patch":false,"reasons":[]}'].join(
            "\n"
          )
        );

        await mod.main();

        expect(mockCore.setOutput).toHaveBeenCalledWith("conclusion", "failure");
        expect(mockCore.setOutput).toHaveBeenCalledWith("success", "false");
        expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Multiple conflicting THREAT_DETECTION_RESULT entries"));
      });

      it("should fail when result JSON is invalid", async () => {
        mockExistsSync.mockReturnValue(true);
        mockReadFileSync.mockReturnValue("THREAT_DETECTION_RESULT:{bad json}");

        await mod.main();

        expect(mockCore.setOutput).toHaveBeenCalledWith("conclusion", "failure");
        expect(mockCore.setOutput).toHaveBeenCalledWith("success", "false");
        expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Failed to parse JSON"));
      });

      it("should set conclusion=success with clean verdict", async () => {
        mockExistsSync.mockReturnValue(true);
        mockReadFileSync.mockReturnValue('debug output\nTHREAT_DETECTION_RESULT:{"prompt_injection":false,"secret_leak":false,"malicious_patch":false,"reasons":[]}\n');

        await mod.main();

        expect(mockCore.setOutput).toHaveBeenCalledWith("conclusion", "success");
        expect(mockCore.setOutput).toHaveBeenCalledWith("success", "true");
        expect(mockCore.setFailed).not.toHaveBeenCalled();
      });

      it("should set conclusion=failure when threats are detected", async () => {
        mockExistsSync.mockReturnValue(true);
        mockReadFileSync.mockReturnValue('THREAT_DETECTION_RESULT:{"prompt_injection":true,"secret_leak":false,"malicious_patch":false,"reasons":["found injection"]}');

        await mod.main();

        expect(mockCore.setOutput).toHaveBeenCalledWith("conclusion", "failure");
        expect(mockCore.setOutput).toHaveBeenCalledWith("success", "false");
        expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Security threats detected: prompt injection"));
      });

      it("should fail when readFileSync throws", async () => {
        mockExistsSync.mockReturnValue(true);
        mockReadFileSync.mockImplementation(() => {
          throw new Error("EACCES: permission denied");
        });

        await mod.main();

        expect(mockCore.setOutput).toHaveBeenCalledWith("conclusion", "failure");
        expect(mockCore.setOutput).toHaveBeenCalledWith("success", "false");
        expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Failed to read detection log"));
      });
    });
  });
});

describe("reportAPIProxyGuardRejection", () => {
  // Uses real files on disk: vi.mock("fs") does not intercept require("fs") in this
  // CJS+vitest setup (see the skipped block above), so the proxy log path is passed explicitly.
  const realFs = require("node:fs");
  const realOs = require("node:os");
  const realPath = require("node:path");
  let tmpDir;

  beforeEach(() => {
    mockCore.warning.mockClear();
    tmpDir = realFs.mkdtempSync(realPath.join(realOs.tmpdir(), "detection-proxy-guard-"));
  });

  afterEach(() => {
    realFs.rmSync(tmpDir, { recursive: true, force: true });
  });

  /**
   * @param {object[]} entries
   * @returns {string} path to the written proxy event log
   */
  function writeProxyEventLog(entries) {
    const logPath = realPath.join(tmpDir, "event-logs.jsonl");
    realFs.writeFileSync(logPath, entries.map(entry => JSON.stringify(entry)).join("\n") + "\n", "utf8");
    return logPath;
  }

  it("warns with the guard name and counters when the proxy rejected requests", () => {
    const logPath = writeProxyEventLog([{ type: "max_cache_misses_exceeded", consecutive_cache_misses: 5, max_cache_misses: 5 }]);
    expect(reportAPIProxyGuardRejection(logPath)).toBe(true);
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("max_cache_misses_exceeded (consecutive_cache_misses=5, max_cache_misses=5)"));
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("not an authentication failure"));
  });

  it("stays silent when no guard rejection is recorded", () => {
    const logPath = writeProxyEventLog([{ type: "response", status: 200 }]);
    expect(reportAPIProxyGuardRejection(logPath)).toBe(false);
    expect(mockCore.warning).not.toHaveBeenCalled();
  });

  it("stays silent when the proxy logs are unavailable", () => {
    expect(reportAPIProxyGuardRejection(realPath.join(tmpDir, "missing.jsonl"))).toBe(false);
    expect(mockCore.warning).not.toHaveBeenCalled();
  });
});
