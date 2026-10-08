// @ts-check
import { afterEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { prepareMemorySnapshot, validateMemorySchema } from "./work_queue_memory.cjs";
import Ajv from "ajv";
import { queueFixture } from "./work_queue_lifecycle.test_helpers.cjs";

const schema = {
  type: "object",
  additionalProperties: false,
  required: ["work_id", "strategy", "findings", "metrics"],
  properties: {
    work_id: { type: "string" },
    strategy: { type: "string", minLength: 1, maxLength: 4 },
    findings: { type: "array", minItems: 1, maxItems: 2, items: { type: "string", enum: ["edge", "wording"] } },
    metrics: { type: "object", additionalProperties: false, required: ["count"], properties: { count: { type: "integer", minimum: 0, maximum: 3 } } },
  },
};
const config = { path: "memory/refiner.json", max_bytes: 262144, schema };
const directories = [];
afterEach(() => {
  vi.unstubAllEnvs();
  for (const directory of directories.splice(0)) fs.rmSync(directory, { recursive: true, force: true });
});
function memory(assignment, index = 0) {
  return { work_id: assignment.claims[index].work_id, strategy: "scan", findings: ["edge"], metrics: { count: 1 } };
}
function prepare(value, assignment, extra = {}, configuration = config) {
  return prepareMemorySnapshot({ memory: value, ...extra }, configuration, { assignment });
}

describe("declarative Claim memory", () => {
  it("matches JSON Schema validation for supported types and nested constraints", () => {
    const { assignment } = queueFixture({ count: 1 });
    const examples = [
      { type: "null", value: null },
      { type: "boolean", value: true },
      { type: "number", value: 2 },
      { type: "integer", value: 0 },
      { type: "string", value: "text" },
      { type: "array", value: [] },
      { type: "object", value: {} },
    ];
    const ajv = new Ajv({ strict: false });
    for (const { type, value } of examples) {
      const candidate = { type: "object", properties: { value: { type } }, additionalProperties: false };
      const validate = ajv.compile(candidate);
      for (const input of [{ value }, { value: "invalid" }, { foreign: true }]) {
        if (validate(input)) expect(prepare(input, assignment, {}, { ...config, schema: candidate }).files).toHaveLength(1);
        else expect(() => prepare(input, assignment, {}, { ...config, schema: candidate })).toThrow();
      }
    }
    const validate = ajv.compile(schema);
    const valid = memory(assignment);
    expect(validate(valid)).toBe(true);
    const invalid = { ...valid, metrics: { count: 4 } };
    expect(validate(invalid)).toBe(false);
    expect(() => prepare(invalid, assignment)).toThrow();
  });
  it("prepares an originally singleton Claim without repository effects", () => {
    const { assignment } = queueFixture({ count: 1 });
    const write = vi.spyOn(fs, "writeFileSync");
    try {
      const value = memory(assignment);
      expect(prepare(value, assignment)).toEqual({ files: [{ path: config.path, content: JSON.stringify(value) + "\n" }] });
      expect(write).not.toHaveBeenCalled();
      expect(Object.isFrozen(value)).toBe(false);
    } finally {
      write.mockRestore();
    }
  });

  it("prepares each original batch member independently and never infers a surviving singleton", () => {
    const { assignment } = queueFixture({ count: 2 });
    for (let index = 0; index < 2; index++) expect(prepare(memory(assignment, index), assignment, { claim_handle: assignment.claims[index].handle }).files[0].content).toContain(assignment.claims[index].work_id);
    expect(() => prepare(memory(assignment), assignment)).toThrow(/multi-Claim/);
    expect(() => prepare(memory(assignment), assignment, { claim_handle: "foreign" })).toThrow(/foreign/);
    expect(() => prepare(memory(assignment, 1), assignment, { claim_handle: "h1" })).toThrow(/work_id conflicts/);
    expect(() => prepare(memory(assignment), assignment, { claim_handle: "h1", claim_id: "foreign" })).toThrow(/conflicts/);
    expect(() => prepare(memory(assignment), assignment, { claim_handle: "h1", authorized: true })).toThrow(/authority/);
  });

  it("uses the bounded compiler-produced assignment file and fails closed without it", () => {
    const { assignment } = queueFixture({ count: 1 });
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-memory-"));
    directories.push(directory);
    const filename = path.join(directory, "assignment.json");
    fs.writeFileSync(filename, JSON.stringify(assignment) + "\n");
    vi.stubEnv("GH_AW_CLAIM_ASSIGNMENT", filename);
    expect(prepareMemorySnapshot({ memory: memory(assignment) }, config).files).toHaveLength(1);
    const link = path.join(directory, "link.json");
    fs.symlinkSync(filename, link);
    vi.stubEnv("GH_AW_CLAIM_ASSIGNMENT", link);
    expect(() => prepareMemorySnapshot({ memory: memory(assignment) }, config)).toThrow(/regular/);
    fs.writeFileSync(filename, " ".repeat(48 * 1024 + 2));
    vi.stubEnv("GH_AW_CLAIM_ASSIGNMENT", filename);
    expect(() => prepareMemorySnapshot({ memory: memory(assignment) }, config)).toThrow(/bounded/);
    vi.stubEnv("GH_AW_CLAIM_ASSIGNMENT", "");
    expect(() => prepareMemorySnapshot({ memory: memory(assignment) }, config)).toThrow(/compiler-produced/);
  });

  it.each([
    [
      "required",
      value => {
        delete value.strategy;
      },
    ],
    [
      "closed root",
      value => {
        value.extra = true;
      },
    ],
    [
      "nested required",
      value => {
        value.metrics = {};
      },
    ],
    [
      "nested closed",
      value => {
        value.metrics.other = 1;
      },
    ],
    [
      "type",
      value => {
        value.metrics.count = "1";
      },
    ],
    [
      "minimum",
      value => {
        value.metrics.count = -1;
      },
    ],
    [
      "maximum",
      value => {
        value.metrics.count = 4;
      },
    ],
    [
      "minLength",
      value => {
        value.strategy = "";
      },
    ],
    [
      "maxLength",
      value => {
        value.strategy = "longer";
      },
    ],
    [
      "minItems",
      value => {
        value.findings = [];
      },
    ],
    [
      "maxItems",
      value => {
        value.findings = ["edge", "edge", "edge"];
      },
    ],
    [
      "enum",
      value => {
        value.findings = ["different"];
      },
    ],
  ])("enforces %s in the declared nested schema", (_, mutate) => {
    const { assignment } = queueFixture({ count: 1 });
    const value = memory(assignment);
    mutate(value);
    expect(() => prepare(value, assignment)).toThrow();
  });

  it("counts Unicode codepoints and exact UTF-8 bytes including the final newline", () => {
    const { assignment } = queueFixture({ count: 1 });
    const value = memory(assignment);
    value.strategy = "😀😀😀😀";
    const bytes = Buffer.byteLength(JSON.stringify(value) + "\n");
    expect(prepare(value, assignment, {}, { ...config, max_bytes: bytes }).files).toHaveLength(1);
    expect(() => prepare(value, assignment, {}, { ...config, max_bytes: bytes - 1 })).toThrow(/max-bytes/);
    value.strategy += "😀";
    expect(() => prepare(value, assignment)).toThrow(/maxLength/);
  });

  it.each([undefined, NaN, Infinity, -0, 1.5, Number.MAX_SAFE_INTEGER + 1, 1n, Symbol("invalid"), () => {}, new Date(), new Map(), new Array(2), "\ud800"])("rejects values JSON serialization would omit or alter: %s", invalid => {
    const { assignment } = queueFixture({ count: 1 });
    expect(() => prepare({ invalid }, assignment, {}, { ...config, schema: { type: "object" } })).toThrow();
  });

  it("rejects getters, hidden fields, symbol keys, cycles, excessive nesting and array sizes", () => {
    const { assignment } = queueFixture({ count: 1 });
    const getter = vi.fn(() => "hidden");
    const accessor = Object.defineProperty({}, "value", { enumerable: true, get: getter });
    const cycle = {};
    cycle.self = cycle;
    let nested = {};
    for (let index = 0; index < 33; index++) nested = { nested };
    for (const value of [accessor, Object.defineProperty({}, "hidden", { value: true }), { [Symbol("key")]: true }, cycle, nested, { values: Array(16385).fill(null) }]) {
      expect(() => prepare(value, assignment, {}, { ...config, schema: { type: "object" } })).toThrow();
    }
    expect(getter).not.toHaveBeenCalled();
  });

  it.each(["../memory.json", "/memory.json", "a/../memory.json", "a//memory.json", ".git/data.json", "a/.GIT/data.json", "data\\memory.json", "data\t.json", "data.txt"])("rejects unsafe fixed paths: %s", filename => {
    const { assignment } = queueFixture({ count: 1 });
    expect(() => prepare(memory(assignment), assignment, {}, { ...config, path: filename })).toThrow(/path/);
  });

  it.each([
    { type: [] },
    { type: {} },
    { type: "unknown" },
    { type: "object", $ref: "https://example.com/schema" },
    { type: "string", pattern: ".*" },
    { type: "object", additionalProperties: {} },
    { type: "object", required: ["a", "a"] },
    { type: "array", items: [{ type: "string" }] },
    { type: "string", enum: [{}] },
    { type: "string", enum: [undefined] },
    { type: "string", enum: ["a", "a"] },
    { type: "string", minLength: -1 },
    { type: "number", minimum: Infinity },
    { type: "object", properties: { a: {} } },
  ])("rejects unsupported or malformed schemas: %s", candidate => {
    expect(() => validateMemorySchema(candidate)).toThrow();
  });

  it("bounds schema bytes and nesting and checks optional Work identity without inventing it", () => {
    const { assignment } = queueFixture({ count: 1 });
    expect(prepare({ value: true }, assignment, {}, { ...config, schema: { type: "object", properties: { value: { type: "boolean" } } } }).files).toHaveLength(1);
    expect(() => prepare({}, assignment, {}, { ...config, schema: { type: "object", description: "x".repeat(16384) } })).toThrow(/bounded/);
    let nested = { type: "string" };
    for (let index = 0; index < 17; index++) nested = { type: "array", items: nested };
    expect(() => validateMemorySchema(nested)).toThrow(/nested/);
  });
});
