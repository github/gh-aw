import { describe, it, expect } from "vitest";
import fs from "fs";
import { fileURLToPath } from "url";

describe("schema-validation.cjs", () => {
  function loadUpdateIssueToolSchema() {
    const toolsPath = fileURLToPath(new URL("./safe_outputs_tools.json", import.meta.url));
    try {
      const tools = JSON.parse(fs.readFileSync(toolsPath, "utf8"));
      if (!Array.isArray(tools)) {
        throw new Error("Expected tools schema to be a JSON array");
      }
      const updateIssueTool = tools.find(tool => tool.name === "update_issue");
      if (!updateIssueTool) {
        const availableNames = tools.map(tool => tool?.name).filter(Boolean);
        throw new Error(`Expected a tool definition named 'update_issue' in safe_outputs_tools.json. Found tools: ${availableNames.join(", ")}`);
      }
      return updateIssueTool.inputSchema;
    } catch (error) {
      throw new Error(`Failed to load or parse safe outputs tool schema at ${toolsPath}. Expected a JSON array containing an 'update_issue' tool definition. Cause: ${error.message}`);
    }
  }

  describe("validateRequiredFields", () => {
    it("should return empty array when no required fields", async () => {
      const { validateRequiredFields } = await import("./schema-validation.cjs");

      const args = { foo: "bar" };
      const schema = { type: "object", properties: { foo: { type: "string" } } };

      const missing = validateRequiredFields(args, schema);

      expect(missing).toEqual([]);
    });

    it("should return empty array when all required fields are present", async () => {
      const { validateRequiredFields } = await import("./schema-validation.cjs");

      const args = { name: "test", age: 25 };
      const schema = {
        type: "object",
        properties: { name: { type: "string" }, age: { type: "number" } },
        required: ["name", "age"],
      };

      const missing = validateRequiredFields(args, schema);

      expect(missing).toEqual([]);
    });

    it("should return missing field names when fields are undefined", async () => {
      const { validateRequiredFields } = await import("./schema-validation.cjs");

      const args = { name: "test" };
      const schema = {
        type: "object",
        properties: { name: { type: "string" }, age: { type: "number" } },
        required: ["name", "age"],
      };

      const missing = validateRequiredFields(args, schema);

      expect(missing).toEqual(["age"]);
    });

    it("should return missing field names when fields are null", async () => {
      const { validateRequiredFields } = await import("./schema-validation.cjs");

      const args = { name: "test", age: null };
      const schema = {
        type: "object",
        properties: { name: { type: "string" }, age: { type: "number" } },
        required: ["name", "age"],
      };

      const missing = validateRequiredFields(args, schema);

      expect(missing).toEqual(["age"]);
    });

    it("should return missing field names when string fields are empty", async () => {
      const { validateRequiredFields } = await import("./schema-validation.cjs");

      const args = { name: "", age: 25 };
      const schema = {
        type: "object",
        properties: { name: { type: "string" }, age: { type: "number" } },
        required: ["name", "age"],
      };

      const missing = validateRequiredFields(args, schema);

      expect(missing).toEqual(["name"]);
    });

    it("should return missing field names when string fields are whitespace only", async () => {
      const { validateRequiredFields } = await import("./schema-validation.cjs");

      const args = { name: "   ", age: 25 };
      const schema = {
        type: "object",
        properties: { name: { type: "string" }, age: { type: "number" } },
        required: ["name", "age"],
      };

      const missing = validateRequiredFields(args, schema);

      expect(missing).toEqual(["name"]);
    });

    it("should return multiple missing field names", async () => {
      const { validateRequiredFields } = await import("./schema-validation.cjs");

      const args = {};
      const schema = {
        type: "object",
        properties: { name: { type: "string" }, age: { type: "number" }, email: { type: "string" } },
        required: ["name", "age", "email"],
      };

      const missing = validateRequiredFields(args, schema);

      expect(missing).toEqual(["name", "age", "email"]);
    });

    it("should handle schema without required array", async () => {
      const { validateRequiredFields } = await import("./schema-validation.cjs");

      const args = { name: "test" };
      const schema = {
        type: "object",
        properties: { name: { type: "string" } },
      };

      const missing = validateRequiredFields(args, schema);

      expect(missing).toEqual([]);
    });

    it("should handle null schema", async () => {
      const { validateRequiredFields } = await import("./schema-validation.cjs");

      const args = { name: "test" };
      const missing = validateRequiredFields(args, null);

      expect(missing).toEqual([]);
    });

    it("should handle undefined schema", async () => {
      const { validateRequiredFields } = await import("./schema-validation.cjs");

      const args = { name: "test" };
      const missing = validateRequiredFields(args, undefined);

      expect(missing).toEqual([]);
    });
  });

  describe("validateStringInputLengths", () => {
    it("should return empty array when no string properties exceed limit", async () => {
      const { validateStringInputLengths } = await import("./schema-validation.cjs");

      const args = { name: "hello", count: 42 };
      const schema = {
        type: "object",
        properties: {
          name: { type: "string" },
          count: { type: "number" },
        },
      };

      const violations = validateStringInputLengths(args, schema);

      expect(violations).toEqual([]);
    });

    it("should return violation when string property exceeds 10 KB limit", async () => {
      const { validateStringInputLengths, MAX_STRING_INPUT_BYTES } = await import("./schema-validation.cjs");

      const oversizedValue = "a".repeat(MAX_STRING_INPUT_BYTES + 1);
      const args = { message: oversizedValue };
      const schema = {
        type: "object",
        properties: { message: { type: "string" } },
      };

      const violations = validateStringInputLengths(args, schema);

      expect(violations).toHaveLength(1);
      expect(violations[0].field).toBe("message");
      expect(violations[0].actualLength).toBe(MAX_STRING_INPUT_BYTES + 1);
      expect(violations[0].unit).toBe("bytes");
    });

    it("should not flag a string at exactly the 10 KB limit", async () => {
      const { validateStringInputLengths, MAX_STRING_INPUT_BYTES } = await import("./schema-validation.cjs");

      const exactValue = "a".repeat(MAX_STRING_INPUT_BYTES);
      const args = { message: exactValue };
      const schema = {
        type: "object",
        properties: { message: { type: "string" } },
      };

      const violations = validateStringInputLengths(args, schema);

      expect(violations).toEqual([]);
    });

    it("should return multiple violations when multiple strings exceed the limit", async () => {
      const { validateStringInputLengths, MAX_STRING_INPUT_BYTES } = await import("./schema-validation.cjs");

      const oversizedValue = "x".repeat(MAX_STRING_INPUT_BYTES + 100);
      const args = { a: oversizedValue, b: oversizedValue };
      const schema = {
        type: "object",
        properties: {
          a: { type: "string" },
          b: { type: "string" },
        },
      };

      const violations = validateStringInputLengths(args, schema);

      expect(violations).toHaveLength(2);
      expect(violations.map(v => v.field).sort()).toEqual(["a", "b"]);
    });

    it("should not flag non-string typed properties even if they contain string values", async () => {
      const { validateStringInputLengths, MAX_STRING_INPUT_BYTES } = await import("./schema-validation.cjs");

      const oversizedValue = "z".repeat(MAX_STRING_INPUT_BYTES + 1);
      const args = { count: oversizedValue };
      const schema = {
        type: "object",
        properties: { count: { type: "number" } },
      };

      const violations = validateStringInputLengths(args, schema);

      expect(violations).toEqual([]);
    });

    it("should handle null or undefined schema gracefully", async () => {
      const { validateStringInputLengths } = await import("./schema-validation.cjs");

      const args = { message: "hello" };

      expect(validateStringInputLengths(args, null)).toEqual([]);
      expect(validateStringInputLengths(args, undefined)).toEqual([]);
    });

    it("should respect a custom maxBytes override", async () => {
      const { validateStringInputLengths } = await import("./schema-validation.cjs");

      const args = { label: "abcde" }; // 5 ASCII bytes
      const schema = {
        type: "object",
        properties: { label: { type: "string" } },
      };

      // Limit of 4 bytes — should flag the 5-byte value
      const violations = validateStringInputLengths(args, schema, 4);
      expect(violations).toHaveLength(1);
      expect(violations[0].field).toBe("label");

      // Limit of 5 bytes — exactly at limit, should not flag
      const noViolations = validateStringInputLengths(args, schema, 5);
      expect(noViolations).toEqual([]);
    });

    it("should measure byte length correctly for multi-byte UTF-8 characters", async () => {
      const { validateStringInputLengths } = await import("./schema-validation.cjs");

      // Each emoji is 4 bytes in UTF-8
      const emojiString = "🚀".repeat(10); // 40 bytes
      const args = { emoji: emojiString };
      const schema = {
        type: "object",
        properties: { emoji: { type: "string" } },
      };

      // Limit of 39 bytes — should flag (10 emojis = 40 bytes)
      const violations = validateStringInputLengths(args, schema, 39);
      expect(violations).toHaveLength(1);

      // Limit of 40 bytes — at limit, should not flag
      const noViolations = validateStringInputLengths(args, schema, 40);
      expect(noViolations).toEqual([]);
    });

    it("should not flag missing (undefined) string values", async () => {
      const { validateStringInputLengths } = await import("./schema-validation.cjs");

      const args = {};
      const schema = {
        type: "object",
        properties: { message: { type: "string" } },
      };

      const violations = validateStringInputLengths(args, schema);

      expect(violations).toEqual([]);
    });

    it("should enforce explicit maxLength for string fields", async () => {
      const { validateStringInputLengths, MAX_STRING_INPUT_BYTES } = await import("./schema-validation.cjs");

      // Exceeds explicit maxLength
      const valueExceedingExplicitMax = "a".repeat(MAX_STRING_INPUT_BYTES + 1);
      const args = { body: valueExceedingExplicitMax };
      const schema = {
        type: "object",
        properties: { body: { type: "string", maxLength: MAX_STRING_INPUT_BYTES } },
      };

      const violations = validateStringInputLengths(args, schema);
      expect(violations).toHaveLength(1);
      expect(violations[0].field).toBe("body");
    });

    it("should still check string fields without maxLength when other fields have explicit maxLength", async () => {
      const { validateStringInputLengths, MAX_STRING_INPUT_BYTES } = await import("./schema-validation.cjs");

      const oversizedValue = "a".repeat(MAX_STRING_INPUT_BYTES + 1);
      const args = { body: oversizedValue, title: oversizedValue };
      const schema = {
        type: "object",
        properties: {
          // body has explicit maxLength lower than value
          body: { type: "string", maxLength: MAX_STRING_INPUT_BYTES },
          // title has no maxLength — checked against default 10KB
          title: { type: "string" },
        },
      };

      const violations = validateStringInputLengths(args, schema);
      expect(violations).toHaveLength(2);
      expect(violations.map(v => v.field).sort()).toEqual(["body", "title"]);
    });

    it("should enforce explicit maxLength using character count", async () => {
      const { validateStringInputLengths } = await import("./schema-validation.cjs");

      const args = { body: "🚀".repeat(3) };
      const schema = {
        type: "object",
        properties: { body: { type: "string", maxLength: 3 } },
      };

      const violations = validateStringInputLengths(args, schema);
      expect(violations).toEqual([]);
    });

    it("enforces explicit limits on nested label strings and object names without imposing default nested limits", async () => {
      const { validateStringInputLengths, MAX_STRING_INPUT_BYTES } = await import("./schema-validation.cjs");
      const schema = {
        properties: {
          labels: {
            type: "array",
            items: {
              oneOf: [
                { type: "string", maxLength: 50 },
                { type: "object", properties: { name: { type: "string", maxLength: 50 }, notes: { type: "string" } } },
              ],
            },
          },
        },
      };
      expect(validateStringInputLengths({ labels: ["a".repeat(51), { name: "\u{1f680}".repeat(54), notes: "a".repeat(MAX_STRING_INPUT_BYTES + 1) }] }, schema)).toEqual([
        { field: "labels[0]", actualLength: 51, limit: 50, unit: "characters" },
        { field: "labels[1].name", actualLength: 54, limit: 50, unit: "characters" },
      ]);
    });

    it.each(["oneOf", "anyOf"])("checks only the enum-selected %s object branch", async keyword => {
      const { validateStringInputLengths } = await import("./schema-validation.cjs");
      const schema = {
        properties: {
          labels: {
            type: "array",
            items: {
              [keyword]: [
                { type: "object", properties: { kind: { enum: ["short"] }, name: { type: "string", maxLength: 5 } } },
                { type: "object", properties: { kind: { enum: ["long"] }, name: { type: "string", maxLength: 50 } } },
              ],
            },
          },
        },
      };

      expect(validateStringInputLengths({ labels: [{ kind: "long", name: "abcdefghij" }] }, schema)).toEqual([]);
      expect(validateStringInputLengths({ labels: [{ kind: "short", name: "abcdefghij" }] }, schema)).toEqual([{ field: "labels[0].name", actualLength: 10, limit: 5, unit: "characters" }]);
      expect(validateStringInputLengths({ labels: [{ kind: "long", name: "a".repeat(51) }] }, schema)).toEqual([{ field: "labels[0].name", actualLength: 51, limit: 50, unit: "characters" }]);
    });

    it("accepts anyOf when one applicable branch satisfies its length constraints", async () => {
      const { validateStringInputLengths } = await import("./schema-validation.cjs");
      const schema = {
        properties: {
          name: {
            anyOf: [
              { type: "string", maxLength: 5 },
              { type: "string", maxLength: 50 },
            ],
          },
        },
      };

      expect(validateStringInputLengths({ name: "abcdefghij" }, schema)).toEqual([]);
      expect(validateStringInputLengths({ name: "a".repeat(51) }, schema)).toEqual([
        { field: "name", actualLength: 51, limit: 5, unit: "characters" },
        { field: "name", actualLength: 51, limit: 50, unit: "characters" },
      ]);
      schema.properties.name.anyOf.push({ type: "string" });
      expect(validateStringInputLengths({ name: "a".repeat(51) }, schema)).toEqual([]);
    });

    it.each(["oneOf", "anyOf"])("ignores %s branches with missing required fields, including nested alternatives", async keyword => {
      const { validateStringInputLengths } = await import("./schema-validation.cjs");
      const schema = {
        properties: {
          label: {
            [keyword]: [
              { required: ["short"], properties: { name: { type: "string", maxLength: 5 } } },
              {
                required: ["long"],
                properties: {
                  name: {
                    anyOf: [
                      { type: "string", maxLength: 10 },
                      { type: "string", maxLength: 50 },
                    ],
                  },
                },
              },
            ],
          },
        },
      };

      expect(validateStringInputLengths({ label: { long: true, name: "a".repeat(20) } }, schema)).toEqual([]);
      expect(validateStringInputLengths({ label: { long: true, name: "a".repeat(51) } }, schema)).toEqual([
        { field: "label.name", actualLength: 51, limit: 10, unit: "characters" },
        { field: "label.name", actualLength: 51, limit: 50, unit: "characters" },
      ]);
    });

    it("requires a complete anyOf branch to satisfy all its length constraints", async () => {
      const { validateStringInputLengths } = await import("./schema-validation.cjs");
      const schema = {
        properties: {
          label: {
            anyOf: [{ properties: { name: { type: "string", maxLength: 5 }, notes: { type: "string", maxLength: 50 } } }, { properties: { name: { type: "string", maxLength: 50 }, notes: { type: "string", maxLength: 5 } } }],
          },
        },
      };

      expect(validateStringInputLengths({ label: { name: "abcdefghij", notes: "abcdefghij" } }, schema)).toEqual([
        { field: "label.name", actualLength: 10, limit: 5, unit: "characters" },
        { field: "label.notes", actualLength: 10, limit: 5, unit: "characters" },
      ]);
    });

    it("does not suppress parent or sibling limits when an alternative passes", async () => {
      const { validateStringInputLengths } = await import("./schema-validation.cjs");
      const schema = {
        properties: {
          name: { type: "string", maxLength: 5, anyOf: [{ type: "string", maxLength: 50 }] },
          message: { type: "string" },
        },
      };

      expect(validateStringInputLengths({ name: "abcdefghij", message: "abcdef" }, schema, 5)).toEqual([
        { field: "name", actualLength: 10, limit: 5, unit: "characters" },
        { field: "message", actualLength: 6, limit: 5, unit: "bytes" },
      ]);
    });

    it.each(["string", "object", "nested", "nested-anyOf"])("uses explicit lengths to resolve %s oneOf branches through the MCP core", async shape => {
      const { validateArgumentsAgainstSchema } = await import("./schema-validation.cjs");
      const { registerTool, handleRequest } = await import("./mcp_server_core.cjs");
      const stringAlternatives = {
        oneOf: [
          { type: "string", maxLength: 5 },
          { type: "string", maxLength: 50 },
        ],
      };
      const itemSchema =
        shape === "object"
          ? {
              oneOf: [
                { type: "object", properties: { name: { type: "string", maxLength: 5 } } },
                { type: "object", properties: { name: { type: "string", maxLength: 50 } } },
              ],
            }
          : shape === "nested"
            ? { anyOf: [stringAlternatives, { type: "boolean" }] }
            : shape === "nested-anyOf"
              ? { oneOf: [{ anyOf: stringAlternatives.oneOf }, { type: "string", maxLength: 5 }] }
              : stringAlternatives;
      const inputSchema = { type: "object", properties: { labels: { type: "array", items: itemSchema } } };
      const server = { tools: {}, debug() {} };
      let handlerCalls = 0;
      registerTool(server, {
        name: "length_options",
        inputSchema,
        handler() {
          handlerCalls += 1;
          return { content: [{ type: "text", text: "ok" }] };
        },
      });

      const call = async length => {
        const name = "a".repeat(length);
        const args = { labels: [shape === "object" ? { name } : name] };
        const response = await handleRequest(server, {
          jsonrpc: "2.0",
          id: length,
          method: "tools/call",
          params: { name: "length_options", arguments: args },
        });
        return { args, response };
      };

      const accepted = await call(10);
      expect(validateArgumentsAgainstSchema(accepted.args, inputSchema)).toBeNull();
      expect(accepted.response.result).toEqual({ content: [{ type: "text", text: "ok" }], isError: false });

      const ambiguous = await call(3);
      expect(validateArgumentsAgainstSchema(ambiguous.args, inputSchema)).toMatchObject({ path: "labels[0]", message: "must match exactly one schema option" });
      expect(ambiguous.response.error.code).toBe(-32602);

      const oversized = await call(51);
      expect(oversized.response.error.code).toBe(-32602);
      expect(oversized.response.error.message).toContain("E006:");
      expect(oversized.response.error.message).toContain(`'${shape === "object" ? "labels[0].name" : "labels[0]"}' exceeds maximum length`);
      expect(handlerCalls).toBe(1);
    });

    it("should allow update_issue body above 10KB when schema declares maxLength", async () => {
      const { validateStringInputLengths, MAX_STRING_INPUT_BYTES } = await import("./schema-validation.cjs");
      const updateIssueSchema = loadUpdateIssueToolSchema();

      expect(updateIssueSchema.properties.body.maxLength).toBe(65536);

      const args = { body: "a".repeat(MAX_STRING_INPUT_BYTES + 1) };
      const violations = validateStringInputLengths(args, updateIssueSchema);
      expect(violations).toEqual([]);
    });

    it("should enforce update_issue body schema maxLength boundary", async () => {
      const { validateStringInputLengths } = await import("./schema-validation.cjs");
      const updateIssueSchema = loadUpdateIssueToolSchema();

      const args = { body: "a".repeat(65537) };
      const violations = validateStringInputLengths(args, updateIssueSchema);
      expect(violations).toHaveLength(1);
      expect(violations[0]).toMatchObject({ field: "body", limit: 65536, unit: "characters" });
    });

    it("should honor maxLength above 10KB for custom safe-job inputs", async () => {
      const { validateStringInputLengths, buildStringLengthValidationError, MAX_STRING_INPUT_BYTES } = await import("./schema-validation.cjs");
      const customJobTool = {
        name: "large_payload_job",
        inputSchema: {
          type: "object",
          properties: {
            payload: { type: "string", maxLength: 40000 },
            legacy_payload: { type: "string" },
          },
        },
      };

      const acceptedPayload = "a".repeat(12 * 1024);
      expect(acceptedPayload.length).toBeGreaterThan(MAX_STRING_INPUT_BYTES);
      expect(validateStringInputLengths({ payload: acceptedPayload }, customJobTool.inputSchema)).toEqual([]);

      const legacyViolations = validateStringInputLengths({ legacy_payload: acceptedPayload }, customJobTool.inputSchema);
      expect(legacyViolations).toMatchObject([{ field: "legacy_payload", actualLength: acceptedPayload.length, limit: MAX_STRING_INPUT_BYTES, unit: "bytes" }]);

      const oversizedPayload = "a".repeat(40001);
      const violations = validateStringInputLengths({ payload: oversizedPayload }, customJobTool.inputSchema);
      expect(violations).toMatchObject([{ field: "payload", actualLength: 40001, limit: 40000, unit: "characters" }]);
      expect(buildStringLengthValidationError(customJobTool.name, violations)).toContain("'payload' exceeds maximum length of 40000 characters (got 40001 characters)");
    });
  });

  describe("validateStringMinLengths", () => {
    it("should return empty array when all string fields meet minLength", async () => {
      const { validateStringMinLengths } = await import("./schema-validation.cjs");

      const args = { body: "This body is long enough to satisfy the constraint" };
      const schema = {
        type: "object",
        properties: { body: { type: "string", minLength: 20 } },
      };

      expect(validateStringMinLengths(args, schema)).toEqual([]);
    });

    it("should return violation when field is shorter than minLength", async () => {
      const { validateStringMinLengths } = await import("./schema-validation.cjs");

      const args = { body: "." };
      const schema = {
        type: "object",
        properties: { body: { type: "string", minLength: 20 } },
      };

      const violations = validateStringMinLengths(args, schema);
      expect(violations).toHaveLength(1);
      expect(violations[0].field).toBe("body");
      expect(violations[0].minLength).toBe(20);
      expect(violations[0].actualLength).toBe(1);
    });

    it("should trim whitespace before comparing against minLength", async () => {
      const { validateStringMinLengths } = await import("./schema-validation.cjs");

      // 25 spaces — trims to 0 chars, below minLength 20
      const args = { body: " ".repeat(25) };
      const schema = {
        type: "object",
        properties: { body: { type: "string", minLength: 20 } },
      };

      const violations = validateStringMinLengths(args, schema);
      expect(violations).toHaveLength(1);
      expect(violations[0].actualLength).toBe(0);
    });

    it("should accept a value at exactly minLength", async () => {
      const { validateStringMinLengths } = await import("./schema-validation.cjs");

      const args = { body: "12345678901234567890" }; // exactly 20 chars
      const schema = {
        type: "object",
        properties: { body: { type: "string", minLength: 20 } },
      };

      expect(validateStringMinLengths(args, schema)).toEqual([]);
    });

    it("should skip absent fields", async () => {
      const { validateStringMinLengths } = await import("./schema-validation.cjs");

      const args = {};
      const schema = {
        type: "object",
        properties: { body: { type: "string", minLength: 20 } },
      };

      expect(validateStringMinLengths(args, schema)).toEqual([]);
    });

    it("should skip fields with no minLength in schema", async () => {
      const { validateStringMinLengths } = await import("./schema-validation.cjs");

      const args = { title: "short" };
      const schema = {
        type: "object",
        properties: { title: { type: "string" } },
      };

      expect(validateStringMinLengths(args, schema)).toEqual([]);
    });

    it("should skip non-string typed fields", async () => {
      const { validateStringMinLengths } = await import("./schema-validation.cjs");

      const args = { count: 3 };
      const schema = {
        type: "object",
        properties: { count: { type: "number", minLength: 5 } },
      };

      expect(validateStringMinLengths(args, schema)).toEqual([]);
    });

    it("should return violations for multiple short fields", async () => {
      const { validateStringMinLengths } = await import("./schema-validation.cjs");

      const args = { title: "Hi", body: "." };
      const schema = {
        type: "object",
        properties: {
          title: { type: "string", minLength: 5 },
          body: { type: "string", minLength: 20 },
        },
      };

      const violations = validateStringMinLengths(args, schema);
      expect(violations).toHaveLength(2);
      const fields = violations.map(v => v.field);
      expect(fields).toContain("title");
      expect(fields).toContain("body");
    });

    it("should handle null/undefined inputSchema gracefully", async () => {
      const { validateStringMinLengths } = await import("./schema-validation.cjs");

      const args = { body: "short" };

      expect(validateStringMinLengths(args, null)).toEqual([]);
      expect(validateStringMinLengths(args, undefined)).toEqual([]);
    });

    it("should accept minLength of zero (always passes)", async () => {
      const { validateStringMinLengths } = await import("./schema-validation.cjs");

      const args = { body: "" };
      const schema = {
        type: "object",
        properties: { body: { type: "string", minLength: 0 } },
      };

      expect(validateStringMinLengths(args, schema)).toEqual([]);
    });
  });

  describe("validateArgumentsAgainstSchema", () => {
    it("enforces required fields in root anyOf branches", async () => {
      const { validateArgumentsAgainstSchema } = await import("./schema-validation.cjs");
      const schema = {
        type: "object",
        properties: { issue_number: { type: "number" }, status: { type: "string" }, labels: { type: "array" } },
        anyOf: [{ required: ["status"] }, { required: ["labels"] }],
      };

      expect(validateArgumentsAgainstSchema({ issue_number: 42 }, schema)).toMatchObject({ path: "status", message: "is required" });
      expect(validateArgumentsAgainstSchema({ issue_number: 42, status: "open" }, schema)).toBeNull();
    });

    it("validates nested array item objects and required fields", async () => {
      const { validateArgumentsAgainstSchema } = await import("./schema-validation.cjs");
      const schema = {
        type: "object",
        properties: {
          labels: {
            type: "array",
            items: {
              type: "object",
              properties: {
                name: { type: "string" },
                rationale: { type: "string" },
                confidence: { type: "string", enum: ["LOW", "MEDIUM", "HIGH"] },
              },
              required: ["name", "rationale", "confidence"],
              additionalProperties: false,
            },
          },
        },
        required: ["labels"],
      };

      expect(validateArgumentsAgainstSchema({ labels: ["bug"] }, schema)).toMatchObject({
        path: "labels[0]",
      });
    });

    it("validates oneOf/anyOf, enum, and additionalProperties recursively", async () => {
      const { validateArgumentsAgainstSchema } = await import("./schema-validation.cjs");
      const schema = {
        type: "object",
        properties: {
          labels: {
            type: "array",
            items: {
              oneOf: [
                { type: "string" },
                {
                  type: "object",
                  properties: {
                    name: { type: "string" },
                    confidence: { type: "string", enum: ["LOW", "MEDIUM", "HIGH"] },
                  },
                  required: ["name", "confidence"],
                  additionalProperties: false,
                },
              ],
            },
          },
        },
      };

      expect(validateArgumentsAgainstSchema({ labels: [{ name: "bug", confidence: "SURE" }] }, schema)).toMatchObject({
        path: "labels[0].confidence",
      });
      expect(validateArgumentsAgainstSchema({ labels: [{ name: "bug", confidence: "HIGH", extra: true }] }, schema)).toMatchObject({
        path: "labels[0].extra",
      });
      expect(validateArgumentsAgainstSchema({ labels: [{ name: "bug", confidence: "HIGH" }] }, schema)).toBeNull();
    });

    it("rejects inherited property names as undeclared schema properties", async () => {
      const { validateValueAgainstSchema } = await import("./schema-validation.cjs");
      const schema = { type: "object", properties: { subject: { type: "string" } }, additionalProperties: false };

      for (const key of ["__proto__", "constructor", "toString", "valueOf", "hasOwnProperty"]) {
        const value = JSON.parse(`{"${key}":"unvalidated"}`);
        expect(validateValueAgainstSchema(value, schema)).toMatchObject({ path: key, message: "is not allowed by the schema" });
      }
    });

    it("requires required properties to be own properties", async () => {
      const { validateValueAgainstSchema } = await import("./schema-validation.cjs");
      const value = Object.create({ subject: "inherited" });

      expect(validateValueAgainstSchema(value, { type: "object", required: ["subject"] })).toMatchObject({
        path: "subject",
        message: "is required",
      });
    });
  });

  describe("formatSchemaValidationError", () => {
    it("formats strict add_labels object-shape guidance", async () => {
      const { formatSchemaValidationError } = await import("./schema-validation.cjs");
      const message = formatSchemaValidationError("add_labels", { labels: ["bug"] }, { path: "labels[0]", message: "must be a object" });
      expect(message).toContain("Invalid arguments for add_labels:");
      expect(message).toContain("labels[0] must be an object (string shorthand is not supported).");
      expect(message).toContain("Required fields: name, rationale, confidence");
      expect(message).toContain('Received: \"bug\"');
    });

    it("derives add_labels object-shape guidance from a workflow-configured item schema", async () => {
      const { formatSchemaValidationError } = await import("./schema-validation.cjs");
      const inputSchema = {
        type: "object",
        properties: {
          labels: {
            type: "array",
            items: {
              type: "object",
              required: ["name", "confidence"],
              properties: {
                name: { type: "string" },
                confidence: { type: "string", enum: ["HIGH", "MEDIUM", "LOW"] },
              },
            },
          },
        },
      };
      const message = formatSchemaValidationError("add_labels", { labels: ["bug"] }, { path: "labels[0]", message: "must be a object" }, inputSchema);
      expect(message).toContain("Invalid arguments for add_labels:");
      expect(message).toContain("labels[0] must be an object (string shorthand is not supported).");
      expect(message).toContain("Required fields: name, confidence");
      expect(message).not.toContain("rationale");
      expect(message).toContain('Expected: {"name":"bug","confidence":"HIGH"}');
      expect(message).toContain('Received: \"bug\"');
    });

    it("uses a boolean example for the required suggest field", async () => {
      const { formatSchemaValidationError } = await import("./schema-validation.cjs");
      const inputSchema = {
        type: "object",
        properties: {
          labels: {
            type: "array",
            items: {
              type: "object",
              required: ["name", "suggest"],
              properties: {
                name: { type: "string" },
                suggest: { type: "boolean" },
              },
            },
          },
        },
      };
      const message = formatSchemaValidationError("add_labels", { labels: ["bug"] }, { path: "labels[0]", message: "must be a object" }, inputSchema);
      expect(message).toContain('Expected: {"name":"bug","suggest":true}');
      expect(message).toContain("Required fields: name, suggest");
    });

    it("does not report optional fields as required or suggest an invalid example", async () => {
      const { formatSchemaValidationError } = await import("./schema-validation.cjs");
      const inputSchema = {
        type: "object",
        properties: {
          labels: {
            type: "array",
            items: {
              type: "object",
              properties: {
                slug: { type: "string", pattern: "^[A-Z]{3}$" },
              },
            },
          },
        },
      };
      const message = formatSchemaValidationError("add_labels", { labels: ["bug"] }, { path: "labels[0]", message: "must be a object" }, inputSchema);
      expect(message).toContain("No required fields are declared by the configured item schema.");
      expect(message).toContain("Available fields: slug");
      expect(message).not.toContain("Required fields:");
      expect(message).not.toContain("Expected:");
    });

    it("omits an example when a constrained required value cannot be inferred", async () => {
      const { formatSchemaValidationError } = await import("./schema-validation.cjs");
      const inputSchema = {
        type: "object",
        properties: {
          labels: {
            type: "array",
            items: {
              type: "object",
              required: ["slug"],
              properties: {
                slug: { type: "string", pattern: "^[A-Z]{3}$" },
              },
            },
          },
        },
      };
      const message = formatSchemaValidationError("add_labels", { labels: ["bug"] }, { path: "labels[0]", message: "must be a object" }, inputSchema);
      expect(message).toContain("Required fields: slug");
      expect(message).not.toContain("Expected:");
    });
  });
});
