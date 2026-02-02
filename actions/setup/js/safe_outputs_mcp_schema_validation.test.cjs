// @ts-check
import { describe, it, expect } from "vitest";
import fs from "fs";
import path from "path";

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
          const property = schema.properties[field];

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
          // Check that type is defined
          if (!property.type) {
            inconsistentTypes.push({
              tool: tool.name,
              field: fieldName,
              issue: "missing type definition",
            });
          }

          // Check for array types
          if (property.type === "array") {
            if (!property.items) {
              inconsistentTypes.push({
                tool: tool.name,
                field: fieldName,
                issue: "array type missing items definition",
              });
            }
          }

          // Check for union types (multiple types)
          if (Array.isArray(property.type)) {
            // Union types are valid but should be intentional
            // Just verify they're not empty
            if (property.type.length === 0) {
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
