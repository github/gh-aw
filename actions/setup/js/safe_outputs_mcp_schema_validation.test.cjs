// @ts-check
import { describe, it, expect, vi } from "vitest";
import fs from "fs";
import path from "path";
import Ajv from "ajv";
import mcpCore from "./mcp_server_core.cjs";
import { normalizeSafeOutputToolArguments } from "./safe_outputs_mcp_arguments.cjs";

/**
 * Tests for MCP tool schema validation
 *
 * This test suite validates that:
 * 1. All required parameters have complete descriptions with examples
 * 2. Error messages follow the enhanced format with parameter names and examples
 * 3. Schema structure is consistent across all tools
 *
 * Related to github/gh-aw#7950
 */

describe("Safe Outputs MCP Schema Validation", () => {
  let tools;

  // Load tools schema once for all tests
  const toolsPath = path.join(process.cwd(), "safe_outputs_tools.json");
  const toolsContent = fs.readFileSync(toolsPath, "utf8");
  tools = JSON.parse(toolsContent);

  describe("Label name limits", () => {
    const cases = [
      { tool: "add_labels", args: name => ({ labels: [name] }), field: "labels[0]" },
      { tool: "add_labels", args: name => ({ labels: [{ name }] }), field: "labels[0].name" },
      { tool: "remove_labels", args: name => ({ labels: [name] }), field: "labels[0]" },
      { tool: "remove_labels", args: name => ({ labels: [{ name }] }), field: "labels[0].name" },
      { tool: "replace_label", args: name => ({ label_to_add: name, label_to_remove: "bug" }), field: "label_to_add" },
      { tool: "replace_label", args: name => ({ label_to_remove: name, label_to_add: "bug" }), field: "label_to_remove" },
      { tool: "update_issue", args: name => ({ labels: [name] }), field: "labels[0]" },
      { tool: "update_issue", args: name => ({ labels: [{ name }] }), field: "labels[0].name" },
      { tool: "update_discussion", args: name => ({ labels: [name] }), field: "labels[0]" },
      { tool: "create_pull_request", args: name => ({ title: "Fix label validation", body: "Reject oversized labels.", labels: [name] }), field: "labels[0]" },
      { tool: "create_issue", args: name => ({ title: "Fix label validation", body: "Reject oversized labels.", labels: [name] }), field: "labels[0]" },
      { tool: "create_issue", args: name => ({ title: "Fix label validation", body: "Reject oversized labels.", labels: `${name}, bug` }), field: "labels[0]" },
      { tool: "create_issue", args: name => ({ title: "Fix label validation", body: "Reject oversized labels.", labels: JSON.stringify([name, "bug"]) }), field: "labels[0]" },
    ];

    describe.each(["HTTP", "stdio"])("%s", transport => {
      for (const testCase of cases) {
        it.each([
          ["a".repeat(50), true],
          ["a".repeat(51), false],
          ["a".repeat(54), false],
          ["\u{1f680}".repeat(50), true],
          ["\u{1f680}".repeat(51), false],
        ])(`${testCase.tool} ${testCase.field} validates %s`, async (name, valid) => {
          const server = mcpCore.createServer({ name: "safeoutputs", version: "1.0.0" }, { normalizeArguments: (toolName, args, tool) => normalizeSafeOutputToolArguments(toolName, args, undefined, tool.inputSchema) });
          const handler = vi.fn(() => ({ content: [{ type: "text", text: "Recorded" }] }));
          mcpCore.registerTool(server, { ...tools.find(tool => tool.name === testCase.tool), handler });
          const request = { jsonrpc: "2.0", id: 1, method: "tools/call", params: { name: testCase.tool, arguments: testCase.args(name) } };
          let response;
          if (transport === "HTTP") {
            response = await mcpCore.handleRequest(server, request);
          } else {
            server.writeMessage = message => {
              response = message;
            };
            await mcpCore.handleMessage(server, request);
          }
          if (valid) {
            expect(response.error).toBeUndefined();
            expect(handler).toHaveBeenCalledOnce();
          } else {
            expect(response.error.code).toBe(-32602);
            expect(response.error.message).toContain(testCase.field);
            expect(response.error.message).toContain("maximum length of 50 characters");
            expect(handler).not.toHaveBeenCalled();
          }
        });
      }
    });
  });

  describe("Draft security advisories", () => {
    const schema = tools.find(tool => tool.name === "create_security_advisory").inputSchema;
    const validate = new Ajv({ strict: false }).compile(schema);
    const advisory = { summary: "Unsafe input handling", description: "Impact and remediation details.", vulnerabilities: [{ package: { ecosystem: "npm", name: "example" } }] };

    it("accepts draft content and optional advisory metadata", () => {
      expect(validate({ ...advisory, severity: "high", cwe_ids: ["CWE-79"] })).toBe(true);
    });

    describe("Security advisory comments", () => {
      const schema = tools.find(tool => tool.name === "add_security_advisory_comment").inputSchema;
      const validate = new Ajv({ strict: false }).compile(schema);
      const comment = { ghsa_id: "GHSA-2345-6789-cfgh", body: "The proposed fix is ready for review." };

      it("accepts a GHSA identifier and comment body", () => {
        expect(validate(comment)).toBe(true);
      });

      it.each([{ repo: "other/public" }, { confidential: true }, { state: "published" }, { "github-token": "agent-token" }, { ghsa_id: "../issues/123" }, { body: "" }])("rejects unsupported controls and invalid comments: %j", fields => {
        expect(validate({ ...comment, ...fields })).toBe(false);
      });
    });

    it.each([{ state: "published" }, { start_private_fork: true }, { repo: "other/public" }, { "github-token": "agent-token" }, { vulnerabilities: [] }, { cwe_ids: ["invalid"] }])(
      "rejects unsupported controls and invalid advisory metadata: %j",
      fields => {
        expect(validate({ ...advisory, ...fields })).toBe(false);
      }
    );
  });

  describe("Issue artifact content sources", () => {
    const schema = tools.find(tool => tool.name === "create_issue").inputSchema;
    const validate = new Ajv({ strict: false }).compile(schema);
    const request = artifact => ({ title: "Parser caching", body: "Implementation details", artifacts: [artifact] });

    it.each([
      { name: "Notes", body: "Details" },
      { type: "plan", name: "Plan", path: "/tmp/gh-aw/agent/plan.md" },
    ])("accepts a single content source: %j", artifact => {
      expect(validate(request(artifact))).toBe(true);
    });

    it.each([{ name: "Notes" }, { name: "Notes", body: "Details", path: "/tmp/plan.md" }, { name: "Notes", path: "" }, { name: "Notes", path: "plan.md" }, { name: "Notes", path: 42 }, { name: "Notes", body: "" }])(
      "rejects an invalid content source: %j",
      artifact => {
        expect(validate(request(artifact))).toBe(false);
      }
    );
  });

  describe("Discussion temporary IDs", () => {
    const schema = tools.find(tool => tool.name === "create_discussion").inputSchema;
    const validate = new Ajv({ strict: false }).compile(schema);

    it.each(["aw_discuss", "#aw_Discuss", "aw_disc_123"])("accepts %s in the strict discussion schema", temporaryId => {
      expect(validate({ title: "Discussion", body: "Report", temporary_id: temporaryId })).toBe(true);
    });

    it.each(["aw_ab", "aw_has-hyphen", "aw_1234567890123", "", 42])("rejects malformed discussion ID %s", temporaryId => {
      expect(validate({ title: "Discussion", body: "Report", temporary_id: temporaryId })).toBe(false);
    });

    it("uses the shared ID pattern and camel-case alias without relaxing the schema", () => {
      const issueSchema = tools.find(tool => tool.name === "create_issue").inputSchema;
      expect(schema.properties.temporary_id.pattern).toBe(issueSchema.properties.temporary_id.pattern);
      expect(schema.properties.temporary_id["x-synonyms"]).toEqual(["temporaryId"]);
      expect(schema.additionalProperties).toBe(false);
      expect(validate({ title: "Discussion", body: "Report" })).toBe(true);
    });
  });

  function resolveSchemaProperty(property) {
    if (!property || !property.$ref || typeof property.$ref !== "string") {
      return property;
    }

    if (!property.$ref.startsWith("#/")) {
      return property;
    }

    const refPath = property.$ref.slice(2).split("/");
    let resolved = tools;
    for (const segment of refPath) {
      if (resolved && Object.prototype.hasOwnProperty.call(resolved, segment)) {
        resolved = resolved[segment];
        continue;
      }
      return property;
    }

    return resolved && typeof resolved === "object" ? resolved : property;
  }

  describe("Schema Completeness", () => {
    it("should load tools schema successfully", () => {
      expect(tools).toBeDefined();
      expect(Array.isArray(tools)).toBe(true);
      expect(tools.length).toBeGreaterThan(0);
    });

    it("should have all required properties for each tool", () => {
      tools.forEach(tool => {
        expect(tool.name).toBeDefined();
        expect(typeof tool.name).toBe("string");
        expect(tool.name.length).toBeGreaterThan(0);

        expect(tool.description).toBeDefined();
        expect(typeof tool.description).toBe("string");
        expect(tool.description.length).toBeGreaterThan(0);

        expect(tool.inputSchema).toBeDefined();
        expect(typeof tool.inputSchema).toBe("object");
      });
    });

    it("should have proper inputSchema structure for each tool", () => {
      tools.forEach(tool => {
        const schema = tool.inputSchema;

        expect(schema.type).toBe("object");
        expect(schema.properties).toBeDefined();
        expect(typeof schema.properties).toBe("object");

        // Check that required fields array is valid if present
        if (schema.required) {
          expect(Array.isArray(schema.required)).toBe(true);
          schema.required.forEach(field => {
            expect(typeof field).toBe("string");
          });
        }
      });
    });
  });

  describe("Required Parameter Descriptions", () => {
    it("should have non-empty descriptions for all required parameters", () => {
      const missingDescriptions = [];

      tools.forEach(tool => {
        const schema = tool.inputSchema;
        const requiredFields = schema.required || [];

        requiredFields.forEach(field => {
          const property = schema.properties[field];

          if (!property) {
            missingDescriptions.push({
              tool: tool.name,
              field,
              issue: "required field not in properties",
            });
            return;
          }

          if (!property.description || property.description.trim() === "") {
            missingDescriptions.push({
              tool: tool.name,
              field,
              issue: "missing or empty description",
            });
          }
        });
      });

      if (missingDescriptions.length > 0) {
        const errorMessage = missingDescriptions.map(item => `  - Tool '${item.tool}', field '${item.field}': ${item.issue}`).join("\n");
        throw new Error(`Required parameters missing descriptions:\n${errorMessage}`);
      }
    });

    it("should have detailed descriptions (at least 50 characters) for required parameters", () => {
      const shortDescriptions = [];
      const MIN_DESCRIPTION_LENGTH = 50;

      tools.forEach(tool => {
        const schema = tool.inputSchema;
        const requiredFields = schema.required || [];

        requiredFields.forEach(field => {
          const property = schema.properties[field];

          if (property && property.description) {
            const desc = property.description.trim();
            if (desc.length < MIN_DESCRIPTION_LENGTH) {
              shortDescriptions.push({
                tool: tool.name,
                field,
                length: desc.length,
                description: desc.substring(0, 100),
              });
            }
          }
        });
      });

      if (shortDescriptions.length > 0) {
        const errorMessage = shortDescriptions.map(item => `  - Tool '${item.tool}', field '${item.field}': ${item.length} chars - "${item.description}..."`).join("\n");
        throw new Error(`Required parameters have too-short descriptions (< ${MIN_DESCRIPTION_LENGTH} chars):\n${errorMessage}`);
      }
    });

    it("should include examples or format guidance in required parameter descriptions", () => {
      const missingExamples = [];

      // Keywords that indicate examples or format guidance
      const exampleIndicators = [
        "e.g.",
        "example",
        "for example",
        "such as",
        "format:",
        "github.com/",
        "aw_",
        /\d+/, // Contains numbers (often used in examples)
        /'[^']+'/, // Contains quoted strings (often examples)
      ];

      // Self-explanatory field names that don't need explicit examples
      const selfExplanatoryFields = ["title", "body", "message", "line", "name", "description", "comment", "content"];

      tools.forEach(tool => {
        const schema = tool.inputSchema;
        const requiredFields = schema.required || [];

        requiredFields.forEach(field => {
          const property = schema.properties[field];

          if (property && property.description) {
            const desc = property.description.toLowerCase();

            // Check if any example indicator is present
            const hasExample = exampleIndicators.some(indicator => {
              if (typeof indicator === "string") {
                return desc.includes(indicator);
              } else if (indicator instanceof RegExp) {
                return indicator.test(property.description);
              }
              return false;
            });

            // Special case: enum fields have examples built-in
            const hasEnum = property.enum && property.enum.length > 0;

            // Special case: self-explanatory field names don't need explicit examples
            const isSelfExplanatory = selfExplanatoryFields.includes(field.toLowerCase());

            if (!hasExample && !hasEnum && !isSelfExplanatory) {
              missingExamples.push({
                tool: tool.name,
                field,
                description: property.description.substring(0, 100),
              });
            }
          }
        });
      });

      if (missingExamples.length > 0) {
        const errorMessage = missingExamples.map(item => `  - Tool '${item.tool}', field '${item.field}': "${item.description}..."`).join("\n");
        throw new Error(`Required parameters missing examples or format guidance:\n${errorMessage}`);
      }
    });
  });

  describe("Optional Parameter Descriptions", () => {
    it("should have descriptions for all optional parameters", () => {
      const missingDescriptions = [];

      tools.forEach(tool => {
        const schema = tool.inputSchema;
        const requiredFields = schema.required || [];
        const allFields = Object.keys(schema.properties);

        // Optional fields are those not in required array
        const optionalFields = allFields.filter(field => !requiredFields.includes(field));

        optionalFields.forEach(field => {
          const property = resolveSchemaProperty(schema.properties[field]);

          if (!property.description || property.description.trim() === "") {
            missingDescriptions.push({
              tool: tool.name,
              field,
            });
          }
        });
      });

      if (missingDescriptions.length > 0) {
        const errorMessage = missingDescriptions.map(item => `  - Tool '${item.tool}', field '${item.field}'`).join("\n");
        throw new Error(`Optional parameters missing descriptions:\n${errorMessage}`);
      }
    });
  });

  describe("Tool Description Quality", () => {
    it("should have detailed tool descriptions (at least 100 characters)", () => {
      const shortDescriptions = [];
      const MIN_TOOL_DESCRIPTION_LENGTH = 100;

      tools.forEach(tool => {
        if (tool.description.length < MIN_TOOL_DESCRIPTION_LENGTH) {
          shortDescriptions.push({
            name: tool.name,
            length: tool.description.length,
            description: tool.description.substring(0, 150),
          });
        }
      });

      if (shortDescriptions.length > 0) {
        const errorMessage = shortDescriptions.map(item => `  - Tool '${item.name}': ${item.length} chars - "${item.description}..."`).join("\n");
        throw new Error(`Tools have too-short descriptions (< ${MIN_TOOL_DESCRIPTION_LENGTH} chars):\n${errorMessage}`);
      }
    });

    it("should have clear use cases in tool descriptions", () => {
      const missingUseCases = [];

      // Keywords that indicate use cases or when to use the tool
      const useCaseIndicators = ["use this", "use for", "when you", "for ", "to ", "when ", "if you"];

      tools.forEach(tool => {
        const desc = tool.description.toLowerCase();
        const hasUseCase = useCaseIndicators.some(indicator => desc.includes(indicator));

        if (!hasUseCase) {
          missingUseCases.push({
            name: tool.name,
            description: tool.description.substring(0, 100),
          });
        }
      });

      if (missingUseCases.length > 0) {
        const errorMessage = missingUseCases.map(item => `  - Tool '${item.name}': "${item.description}..."`).join("\n");
        throw new Error(`Tools missing clear use cases in descriptions:\n${errorMessage}`);
      }
    });
  });

  describe("Schema Consistency", () => {
    it("should use consistent property type definitions", () => {
      const inconsistentTypes = [];

      tools.forEach(tool => {
        const properties = tool.inputSchema.properties;

        Object.entries(properties).forEach(([fieldName, property]) => {
          const resolvedProperty = resolveSchemaProperty(property);

          // Check that type is defined
          if (!resolvedProperty.type) {
            inconsistentTypes.push({
              tool: tool.name,
              field: fieldName,
              issue: "missing type definition",
            });
          }

          // Check for array types
          if (resolvedProperty.type === "array") {
            if (!resolvedProperty.items) {
              inconsistentTypes.push({
                tool: tool.name,
                field: fieldName,
                issue: "array type missing items definition",
              });
            }
          }

          // Check for union types (multiple types)
          if (Array.isArray(resolvedProperty.type)) {
            // Union types are valid but should be intentional
            // Just verify they're not empty
            if (resolvedProperty.type.length === 0) {
              inconsistentTypes.push({
                tool: tool.name,
                field: fieldName,
                issue: "empty type array",
              });
            }
          }
        });
      });

      if (inconsistentTypes.length > 0) {
        const errorMessage = inconsistentTypes.map(item => `  - Tool '${item.tool}', field '${item.field}': ${item.issue}`).join("\n");
        throw new Error(`Schema type definitions are inconsistent:\n${errorMessage}`);
      }
    });

    it("should use snake_case for tool names", () => {
      const invalidNames = [];

      tools.forEach(tool => {
        // Check if name contains hyphens or is not lowercase
        if (tool.name.includes("-") || tool.name !== tool.name.toLowerCase()) {
          invalidNames.push(tool.name);
        }
      });

      if (invalidNames.length > 0) {
        throw new Error(`Tool names should use snake_case: ${invalidNames.join(", ")}`);
      }
    });

    it("should set additionalProperties to false for strict validation", () => {
      const missingAdditionalProperties = [];

      tools.forEach(tool => {
        if (tool.inputSchema.additionalProperties !== false) {
          missingAdditionalProperties.push(tool.name);
        }
      });

      if (missingAdditionalProperties.length > 0) {
        throw new Error(`Tools missing 'additionalProperties: false' for strict validation:\n  - ${missingAdditionalProperties.join("\n  - ")}`);
      }
    });

    it("should not use oneOf, allOf, or anyOf at the top level of inputSchema (Anthropic API incompatible)", () => {
      // The Anthropic API rejects input_schema objects that use oneOf, allOf, or anyOf
      // at the top level. These composition keywords must not appear directly on the
      // inputSchema object — only inside nested property definitions.
      const violations = [];

      tools.forEach(tool => {
        const schema = tool.inputSchema;
        for (const keyword of ["oneOf", "allOf", "anyOf"]) {
          if (Object.prototype.hasOwnProperty.call(schema, keyword)) {
            violations.push({ tool: tool.name, keyword });
          }
        }
      });

      if (violations.length > 0) {
        const errorMessage = violations.map(v => `  - Tool '${v.tool}': uses top-level '${v.keyword}'`).join("\n");
        throw new Error(
          `Tools use top-level JSON Schema composition keywords incompatible with the Anthropic API:\n${errorMessage}\n` + `Fix: remove the top-level composition keyword and rely on the tool description to convey constraints.`
        );
      }
    });
  });

  describe("Enum Value Validation", () => {
    it("should have clear enum values with uppercase constants where appropriate", () => {
      const enumIssues = [];

      tools.forEach(tool => {
        const properties = tool.inputSchema.properties;

        Object.entries(properties).forEach(([fieldName, property]) => {
          if (property.enum) {
            // Check that enum array is not empty
            if (!Array.isArray(property.enum) || property.enum.length === 0) {
              enumIssues.push({
                tool: tool.name,
                field: fieldName,
                issue: "enum array is empty or invalid",
              });
            }

            // Check for common enum value patterns
            // Status/reason fields should typically use UPPERCASE
            if ((fieldName.includes("status") || fieldName.includes("reason") || fieldName.includes("side")) && property.enum.length > 0) {
              const hasNonUppercase = property.enum.some(value => typeof value === "string" && value !== value.toUpperCase());

              if (hasNonUppercase && fieldName !== "status") {
                // 'status' field in update_issue can be lowercase ('open', 'closed')
                // But reason fields like in close_discussion should be uppercase
                if (fieldName.includes("reason")) {
                  enumIssues.push({
                    tool: tool.name,
                    field: fieldName,
                    issue: "reason enum values should be UPPERCASE constants",
                    values: property.enum,
                  });
                }
              }
            }
          }
        });
      });

      if (enumIssues.length > 0) {
        const errorMessage = enumIssues.map(item => `  - Tool '${item.tool}', field '${item.field}': ${item.issue}${item.values ? ` (values: ${JSON.stringify(item.values)})` : ""}`).join("\n");
        throw new Error(`Enum value issues found:\n${errorMessage}`);
      }
    });
  });
});
