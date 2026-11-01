import { describe, it, expect } from "vitest";
const { parseDocument } = require("./work_queue_yaml.cjs");

describe("work_queue_yaml", () => {
  it("exports the expected functions", () => {
    expect(typeof parseDocument).toBe("function");
  });

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
    const doc = parseDocument("a: [1, 2\nb: : :\n");
    expect(doc.errors.length).toBeGreaterThan(0);
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
