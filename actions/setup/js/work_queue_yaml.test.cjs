import { describe, it, expect } from "vitest";
const { parseDocument } = require("./work_queue_yaml.cjs");

describe("work_queue_yaml", () => {
  it("parses a simple mapping", () => {
    const doc = parseDocument("a: 1\nb: hello\n");
    expect(doc.errors).toHaveLength(0);
    expect(doc.toJS()).toEqual({ a: 1, b: "hello" });
  });

  it("parses sequences", () => {
    expect(parseDocument("- one\n- two\n- 3\n").toJS()).toEqual(["one", "two", 3]);
  });

  it("parses nested structures", () => {
    expect(parseDocument("outer:\n  inner:\n    - x\n    - y\n").toJS()).toEqual({ outer: { inner: ["x", "y"] } });
  });

  it("parses scalar types", () => {
    expect(parseDocument("t: true\nn: null\nf: 1.5\ns: '1'\n").toJS()).toEqual({ t: true, n: null, f: 1.5, s: "1" });
  });

  it("handles block scalars", () => {
    expect(parseDocument("text: |\n  line1\n  line2\n").toJS()).toEqual({ text: "line1\nline2\n" });
  });

  it("returns an empty document for empty input", () => {
    expect(parseDocument("").toJS()).toBeNull();
  });

  it("reports errors for invalid YAML", () => {
    const doc = parseDocument("a: [1, 2\n");
    expect(doc.errors.some(error => error.code === "BAD_INDENT")).toBe(true);
  });

  it("rejects duplicate keys when uniqueKeys is enabled", () => {
    const doc = parseDocument("key: first\nkey: second\n", { uniqueKeys: true });
    expect(doc.errors.some(error => error.code === "DUPLICATE_KEY")).toBe(true);
  });

  it("merges anchored mappings when merge is enabled", () => {
    const doc = parseDocument("base: &b\n  x: 1\nover:\n  <<: *b\n  y: 2\n", { merge: true });
    expect(doc.toJS().over).toEqual({ x: 1, y: 2 });
  });

  it("uses YAML 1.2 scalar rules when requested", () => {
    const doc = parseDocument("affirmative: yes\nnumber: 012\n", { version: "1.2" });
    expect(doc.toJS()).toEqual({ affirmative: "yes", number: 12 });
  });

  it("supports mapAsMap during JavaScript conversion", () => {
    const doc = parseDocument("a: 1\nb: 2\n");
    expect(doc.toJS({ mapAsMap: true })).toEqual(
      new Map([
        ["a", 1],
        ["b", 2],
      ])
    );
  });

  it("enforces maxAliasCount during JavaScript conversion", () => {
    const doc = parseDocument("base: &x [1]\nreferences: [*x, *x]\n");
    expect(() => doc.toJS({ maxAliasCount: 1 })).toThrow(/Excessive alias count/);
  });

  it("exposes raw contents for AST-level consumers", () => {
    const doc = parseDocument("a: 1\nb: 2\n");
    expect(Array.isArray(doc.contents.items)).toBe(true);
    expect(doc.contents.items).toHaveLength(2);
  });

  it("exposes warnings separately from errors", () => {
    const doc = parseDocument("a: 1\n");
    expect(doc.warnings).toEqual([]);
  });

  it("reports an error for multiple documents", () => {
    const doc = parseDocument("a: 1\n---\nb: 2\n");
    expect(doc.errors.some(e => e.code === "MULTIPLE_DOCS")).toBe(true);
  });

  it("resolves anchors and aliases", () => {
    expect(parseDocument("a: &x 5\nb: *x\n").toJS()).toEqual({ a: 5, b: 5 });
  });

  it("exposes parsed content via get", () => {
    expect(parseDocument("key: value\n").get("key")).toBe("value");
  });
});
