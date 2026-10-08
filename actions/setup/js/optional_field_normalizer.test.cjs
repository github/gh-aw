import { describe, expect, it } from "vitest";
import safeOutputTools from "./safe_outputs_tools.json";
import { isBlankOptionalField, normalizeBlankOptionalFields } from "./optional_field_normalizer.cjs";
import { normalizeSafeOutputToolArguments, stripInternalSafeOutputSchemaMetadata } from "./safe_outputs_mcp_arguments.cjs";
import { validateField } from "./safe_output_type_validator.cjs";

describe("blank optional fields", () => {
  const constrainedFields = [
    { type: "number" },
    { type: "integer" },
    { type: "boolean" },
    { type: "array" },
    { type: "object" },
    { type: "object", required: ["name"] },
    { type: ["number", "string"] },
    { type: "string", pattern: "^aw_" },
    { type: "string", enum: ["open", "closed"] },
    { type: "string", format: "date" },
    { type: "string", minLength: 1 },
    { optionalPositiveInteger: true },
    { positiveInteger: true },
    { issueOrPRNumber: true },
    { issueNumberOrTemporaryId: true },
  ];

  it.each(constrainedFields)("omits blanks for %j, but never required or nonblank values", field => {
    for (const value of ["", " \t\n"]) {
      expect(isBlankOptionalField(value, field)).toBe(true);
      expect(normalizeBlankOptionalFields({ value }, { value: field })).toEqual({});
      expect(validateField(value, "value", field, "test", 1)).toEqual({ isValid: true });
      expect(isBlankOptionalField(value, { ...field, required: true })).toBe(false);
      expect(normalizeBlankOptionalFields({ value }, { value: field }, ["value"])).toEqual({ value });
    }
    for (const value of [0, false, null, [], {}, "0", "invalid", "auto"]) {
      expect(isBlankOptionalField(value, field)).toBe(false);
    }
  });

  it("preserves free-text clears and unknown fields without mutating the arguments", () => {
    const args = { body: "", description: " \t", unknown: "" };
    expect(normalizeBlankOptionalFields(args, { body: { type: "string" }, description: { type: "string", maxLength: 256 } })).toEqual(args);
    expect(normalizeBlankOptionalFields({ value: "" }, { value: { type: "number" } })).toEqual({});
  });

  it("preserves blank arbitrary-JSON values when the schema opts out", () => {
    const properties = { value: { type: ["object", "array", "string", "number", "boolean", "null"], "x-preserve-blank": true } };
    const args = { operation: "append", value: "" };
    expect(normalizeBlankOptionalFields(args, properties)).toEqual(args);
    expect(normalizeSafeOutputToolArguments("ledger_append", args, undefined, { properties })).toEqual(args);

    const ledgerSchema = safeOutputTools.find(tool => tool.name === "ledger_append").inputSchema;
    expect(normalizeSafeOutputToolArguments("ledger_append", args, undefined, ledgerSchema)).toEqual(args);
    expect(stripInternalSafeOutputSchemaMetadata(ledgerSchema).properties.value["x-preserve-blank"]).toBe(true);
  });

  it("treats blank stack roots as absent branch references, but leaves required roots unchanged", () => {
    const fields = { stack_root: { type: "string" } };
    expect(normalizeBlankOptionalFields({ stack_root: " \t\n" }, fields)).toEqual({});
    expect(normalizeBlankOptionalFields({ stack_root: "" }, fields, ["stack_root"])).toEqual({ stack_root: "" });
    expect(normalizeBlankOptionalFields({ stack_root: "main" }, fields)).toEqual({ stack_root: "main" });
  });

  it("normalizes wrapped, synonym, and typed MCP payloads after canonicalizing field names", () => {
    const schema = { properties: { stack_position: { type: ["number", "string"], "x-synonyms": ["stackPosition"] } } };
    for (const args of [{ stackPosition: "" }, { create_pull_request: { stackPosition: "" } }, { type: "create_pull_request", stackPosition: "" }]) {
      const normalized = normalizeSafeOutputToolArguments("create_pull_request", args, undefined, schema);
      expect(normalized.stack_position).toBeUndefined();
      expect(normalized.stackPosition).toBeUndefined();
      expect(JSON.stringify(args)).toContain("stackPosition");
    }
  });

  it.each(["", " \t\n"])("preserves required-field errors (%j)", value => {
    expect(validateField(value, "body", { type: "string", required: true }, "add_comment", 1)).toEqual({
      isValid: false,
      error: "Line 1: add_comment requires a 'body' field (string)",
    });
  });
});
