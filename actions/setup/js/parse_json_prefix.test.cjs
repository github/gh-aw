import { describe, expect, it } from "vitest";
import { createRequire } from "module";

const require = createRequire(import.meta.url);
const { parseJsonPrefix } = require("./parse_json_prefix.cjs");

describe("parseJsonPrefix", () => {
  it.each([
    ["object", '{"type":"item.started","item":{"id":"1"}}', { type: "item.started", item: { id: "1" } }],
    ["array", '[1,true,null,"text"]', [1, true, null, "text"]],
    ["string", '"escaped \\"text\\""', 'escaped "text"'],
    ["number", "-1.25e2", -125],
    ["boolean", "false", false],
    ["null", "null", null],
  ])("parses a complete JSON %s", (_name, input, expected) => {
    expect(parseJsonPrefix(input)).toEqual(expected);
  });

  it("decodes escaped object keys and string values", () => {
    expect(parseJsonPrefix('{"ty\\u0070e":"item.com\\u0070leted","value":"\\u2603"}')).toEqual({
      type: "item.completed",
      value: "☃",
    });
  });

  it("retains completed properties when a nested result is truncated", () => {
    const result = parseJsonPrefix('{"type":"item.completed","item":{"id":"large","type":"mcp_tool_call","result":"');
    expect(result).toEqual({
      type: "item.completed",
      item: { id: "large", type: "mcp_tool_call" },
    });
  });

  it("retains completed array elements before a truncated element", () => {
    expect(parseJsonPrefix('{"items":[1,{"complete":true,"text":"unfinished')).toEqual({
      items: [1, { complete: true }],
    });
  });

  it("keeps completed properties when the following key or value is malformed", () => {
    expect(parseJsonPrefix('{"type":"item.started","item":')).toEqual({ type: "item.started" });
    expect(parseJsonPrefix('{"type":"item.started",?')).toEqual({ type: "item.started" });
    expect(parseJsonPrefix('{"type":"item.started","broken"')).toEqual({ type: "item.started" });
  });

  it("does not expose partially parsed strings or invalid scalar values", () => {
    expect(parseJsonPrefix('{"type":"item.started","id":"unfinished')).toEqual({ type: "item.started" });
    expect(parseJsonPrefix('{"type":"item.started","value":truX')).toEqual({ type: "item.started" });
  });

  it("returns undefined for empty, whitespace-only, and invalid input", () => {
    expect(parseJsonPrefix("")).toBeUndefined();
    expect(parseJsonPrefix(" \t\r\n")).toBeUndefined();
    expect(parseJsonPrefix("not-json")).toBeUndefined();
    expect(parseJsonPrefix("{invalid")).toEqual({});
  });

  it("returns already parsed properties for mismatched delimiters", () => {
    expect(parseJsonPrefix('{"type":"item.started","item":[1,2}')).toEqual({
      type: "item.started",
      item: [1, 2],
    });
  });

  it("parses the first JSON value and ignores any following content", () => {
    expect(parseJsonPrefix('{"type":"item.started"} trailing payload')).toEqual({ type: "item.started" });
  });

  it("preserves __proto__ as data without modifying object prototypes", () => {
    const result = parseJsonPrefix('{"__proto__":{"polluted":true},"type":"item.started"}');
    expect(Object.hasOwn(result, "__proto__")).toBe(true);
    expect(result.__proto__).toEqual({ polluted: true });
    expect({}.polluted).toBeUndefined();
  });

  it("supports nested JSON up to the configured depth limit", () => {
    const input = `${"[".repeat(64)}0${"]".repeat(64)}`;
    expect(parseJsonPrefix(input)).toEqual(JSON.parse(input));
  });

  it("rejects excessive nesting without throwing", () => {
    expect(parseJsonPrefix(`${"[".repeat(65)}0${"]".repeat(65)}`)).toBeUndefined();
  });

  it("handles a large truncated payload with bounded parser input", () => {
    const prefix = '{"type":"item.completed","item":{"id":"large","type":"mcp_tool_call","result":"';
    const result = parseJsonPrefix(prefix + "x".repeat(64 * 1024));
    expect(result).toEqual({
      type: "item.completed",
      item: { id: "large", type: "mcp_tool_call" },
    });
  });
});
