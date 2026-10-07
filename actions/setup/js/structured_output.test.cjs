import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";
import { loadStructuredOutputSchema, validateStructuredOutput, publishStructuredOutput, MAX_STRUCTURED_OUTPUT_BYTES } from "./structured_output.cjs";

const schema = {
  type: "object",
  properties: { decision: { type: "string", enum: ["APPROVE", "ESCALATE"] }, confidence: { type: "number", minimum: 0, maximum: 1 } },
  required: ["decision", "confidence"],
  additionalProperties: false,
};

describe("structured output validation", () => {
  it("validates required fields, enums, ranges, and extra properties without mutation", () => {
    const valid = { decision: "APPROVE", confidence: 0.9 };
    validateStructuredOutput(valid, schema);
    expect(valid).toEqual({ decision: "APPROVE", confidence: 0.9 });
    for (const value of [{}, { decision: "INVALID", confidence: 0.5 }, { decision: "APPROVE", confidence: 2 }, { decision: "APPROVE", confidence: 0.5, extra: true }, { decision: "APPROVE", confidence: "0.5" }]) {
      expect(() => validateStructuredOutput(value, schema)).toThrow("schema violation");
    }
  });

  it("supports draft 2020-12 and local references", () => {
    const modern = {
      $schema: "https://json-schema.org/draft/2020-12/schema",
      type: "object",
      properties: { values: { type: "array", prefixItems: [{ $ref: "#/$defs/value" }], items: false } },
      $defs: { value: { type: "string", minLength: 2 } },
    };
    validateStructuredOutput({ values: ["ok"] }, modern);
    expect(() => validateStructuredOutput({ values: ["x"] }, modern)).toThrow("minLength");
    expect(() => validateStructuredOutput({ values: ["ok", "extra"] }, modern)).toThrow("schema violation");
  });

  it("accepts canonical draft URI aliases without mutating schemas", () => {
    for (const uri of ["http://json-schema.org/draft-07/schema", "https://json-schema.org/draft-07/schema#", "https://json-schema.org/draft/2020-12/schema#"]) {
      const declared = { type: "object", $schema: uri };
      validateStructuredOutput({}, declared);
      expect(declared.$schema).toBe(uri);
    }
  });

  it("rejects unsupported drafts rather than interpreting them as draft-07", () => {
    expect(() => validateStructuredOutput({}, { type: "object", $schema: "https://json-schema.org/draft/2019-09/schema" })).toThrow("must use draft-07 or draft 2020-12");
  });

  it("enforces standard formats and does not expose model text in errors", () => {
    expect(() => validateStructuredOutput({ email: "secret-value" }, { type: "object", properties: { email: { type: "string", format: "email" } } })).toThrow("format");
    expect(() => validateStructuredOutput({ decision: "secret-value", confidence: 1 }, schema)).not.toThrow("secret-value");
    const check = () => validateStructuredOutput({ "secret-property-name": "secret-value" }, { type: "object", additionalProperties: { type: "integer" } });
    expect(check).toThrow("type");
    expect(check).not.toThrow("secret-property-name");
    expect(check).not.toThrow("secret-value");
  });

  it("rejects overflowing numbers instead of silently serializing them as null", () => {
    const value = JSON.parse('{"value":1e999}');
    for (const declared of [{ type: "object", properties: { value: { type: "number" } } }, { type: "object" }]) {
      expect(() => validateStructuredOutput(value, declared)).toThrow("non-finite JSON numbers");
    }
  });
});

describe("structured output publication", () => {
  let directory;
  let filename;
  beforeEach(() => {
    directory = fs.mkdtempSync(path.join(os.tmpdir(), "structured-output-test-"));
    filename = path.join(directory, "output.json");
  });
  afterEach(() => fs.rmSync(directory, { recursive: true, force: true }));

  function publish(value, declaredSchema = schema) {
    fs.writeFileSync(filename, JSON.stringify(value));
    const core = { setOutput: vi.fn() };
    publishStructuredOutput(core, { GH_AW_STRUCTURED_OUTPUT_FILE: filename, GH_AW_STRUCTURED_OUTPUT_SCHEMA: JSON.stringify(declaredSchema) });
    return core;
  }

  it("publishes only validated compact JSON", () => {
    const core = publish({ decision: "ESCALATE", confidence: 1 });
    expect(core.setOutput).toHaveBeenCalledWith("structured", '{"decision":"ESCALATE","confidence":1}');
    expect(loadStructuredOutputSchema(filename)).toEqual({ decision: "ESCALATE", confidence: 1 });
  });

  it("fails closed for missing, malformed, or invalid responses", () => {
    const core = { setOutput: vi.fn() };
    const env = { GH_AW_STRUCTURED_OUTPUT_FILE: filename, GH_AW_STRUCTURED_OUTPUT_SCHEMA: JSON.stringify(schema) };
    expect(() => publishStructuredOutput(core, env)).toThrow();
    fs.writeFileSync(filename, "```json\n{}\n```");
    expect(() => publishStructuredOutput(core, env)).toThrow();
    fs.writeFileSync(filename, "{}");
    expect(() => publishStructuredOutput(core, env)).toThrow("schema violation");
    expect(core.setOutput).not.toHaveBeenCalled();
  });

  it("rejects oversized output accounting for UTF-16 job limits", () => {
    const large = { text: "a".repeat(MAX_STRUCTURED_OUTPUT_BYTES / 2) };
    expect(() => publish(large, { type: "object" })).toThrow("256 KiB");
  });

  it("accepts output exactly at the UTF-16 publication limit", () => {
    const value = { text: "x".repeat(MAX_STRUCTURED_OUTPUT_BYTES / 2 - JSON.stringify({ text: "" }).length) };
    const core = publish(value, { type: "object" });
    expect(core.setOutput).toHaveBeenCalledWith("structured", JSON.stringify(value));
  });

  it("validates the compiler-provided schema, not an agent-editable schema file", () => {
    fs.writeFileSync(path.join(directory, "schema.json"), '{"type":"object"}');
    expect(() => publish({ arbitrary: true })).toThrow("schema violation");
  });

  it("does not publish symlinked files or oversized raw JSON", () => {
    const actual = path.join(directory, "actual.json");
    fs.writeFileSync(actual, '{"decision":"APPROVE","confidence":1}');
    fs.symlinkSync(actual, filename);
    const core = { setOutput: vi.fn() };
    const env = { GH_AW_STRUCTURED_OUTPUT_FILE: filename, GH_AW_STRUCTURED_OUTPUT_SCHEMA: JSON.stringify(schema) };
    expect(() => publishStructuredOutput(core, env)).toThrow();
    fs.unlinkSync(filename);
    fs.writeFileSync(filename, " ".repeat(1024 * 1024 + 1));
    expect(() => publishStructuredOutput(core, env)).toThrow("1 MiB");
    expect(core.setOutput).not.toHaveBeenCalled();
  });
});
