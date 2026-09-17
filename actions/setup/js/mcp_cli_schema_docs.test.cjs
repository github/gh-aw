import { describe, expect, it } from "vitest";

import { renderSafeOutputsPromptDocs, renderToolRecommendedExample, renderToolSignature } from "./mcp_cli_schema_docs.cjs";

describe("mcp_cli_schema_docs.cjs", () => {
  it("renders optional intent fields when issue-intent metadata is omitted", () => {
    const tool = {
      name: "set_issue_type",
      inputSchema: {
        type: "object",
        properties: {
          issue_number: { type: "integer" },
          issue_type: { type: "string" },
          rationale: { type: "string", maxLength: 280 },
          confidence: { type: "string", enum: ["LOW", "MEDIUM", "HIGH"] },
          suggest: { type: "boolean" },
        },
        required: ["issue_number", "issue_type"],
      },
    };

    const signature = renderToolSignature("safeoutputs", tool);
    expect(signature).toContain("--issue_number <number>");
    expect(signature).toContain("--issue_type <type>");
    expect(signature).toContain("[--rationale <reason, max 280 characters>]");
    expect(signature).toContain("[--confidence <LOW|MEDIUM|HIGH>]");
    expect(signature).toContain("[--suggest <true|false>]");

    const recommended = renderToolRecommendedExample("safeoutputs", tool);
    expect(recommended).toContain('--rationale "The report describes reproducible incorrect behavior."');
    expect(recommended).toContain('--confidence "HIGH"');
  });

  it("renders required intent fields when issue-intent is strict", () => {
    const tool = {
      name: "set_issue_type",
      inputSchema: {
        type: "object",
        properties: {
          issue_number: { type: "integer" },
          issue_type: { type: "string" },
          rationale: { type: "string", maxLength: 280 },
          confidence: { type: "string", enum: ["LOW", "MEDIUM", "HIGH"] },
        },
        required: ["issue_number", "issue_type", "rationale", "confidence"],
      },
    };

    const signature = renderToolSignature("safeoutputs", tool);
    expect(signature).toContain("--rationale <reason, max 280 characters>");
    expect(signature).toContain("--confidence <LOW|MEDIUM|HIGH>");
    expect(signature).not.toContain("[--rationale");
    expect(signature).not.toContain("[--confidence");
  });

  it("omits intent fields when issue-intent is disabled", () => {
    const tool = {
      name: "set_issue_type",
      inputSchema: {
        type: "object",
        properties: {
          issue_number: { type: "integer" },
          issue_type: { type: "string" },
        },
        required: ["issue_number", "issue_type"],
      },
    };

    const signature = renderToolSignature("safeoutputs", tool);
    expect(signature).not.toContain("rationale");
    expect(signature).not.toContain("confidence");
    expect(signature).not.toContain("suggest");
  });

  it("renders strict add_labels as structured JSON only", () => {
    const tool = {
      name: "add_labels",
      inputSchema: {
        type: "object",
        properties: {
          item_number: { type: "integer" },
          labels: {
            type: "array",
            items: {
              type: "object",
              properties: {
                name: { type: "string" },
                rationale: { type: "string", maxLength: 280 },
                confidence: { type: "string", enum: ["LOW", "MEDIUM", "HIGH"] },
              },
              required: ["name", "rationale", "confidence"],
            },
          },
        },
        required: ["item_number", "labels"],
      },
    };

    const signature = renderToolSignature("safeoutputs", tool);
    expect(signature).toContain("safeoutputs add_labels '<json object>'");

    const docs = renderSafeOutputsPromptDocs("safeoutputs", [tool]);
    expect(docs).toContain('"name": "bug"');
    expect(docs).toContain('"rationale": "The report describes reproducible incorrect behavior."');
    expect(docs).toContain('"confidence": "HIGH"');
  });

  it("preserves mixed scalar type placeholders in signatures", () => {
    const tool = {
      name: "set_issue_type",
      inputSchema: {
        type: "object",
        properties: {
          item_number: { type: ["number", "string"] },
          issue_type: { type: "string" },
        },
        required: ["item_number", "issue_type"],
      },
    };

    const signature = renderToolSignature("safeoutputs", tool);
    expect(signature).toContain("--item_number <number|string>");
  });

  it("omits nested intent fields in add_labels recommended docs when label objects do not require them", () => {
    const tool = {
      name: "add_labels",
      inputSchema: {
        type: "object",
        properties: {
          item_number: { type: "integer" },
          labels: {
            type: "array",
            items: {
              oneOf: [
                { type: "string" },
                {
                  type: "object",
                  properties: {
                    name: { type: "string" },
                    rationale: { type: "string", maxLength: 280 },
                    confidence: { type: "string", enum: ["LOW", "MEDIUM", "HIGH"] },
                  },
                  required: ["name"],
                },
              ],
            },
          },
        },
        required: ["item_number", "labels"],
      },
    };

    const docs = renderSafeOutputsPromptDocs("safeoutputs", [tool]);
    expect(docs).toContain('"name": "bug"');
    expect(docs).not.toContain('"rationale":');
    expect(docs).not.toContain('"confidence":');
  });

  it("includes one valid anyOf branch requirement in recommended examples", () => {
    const tool = {
      name: "assign_milestone",
      inputSchema: {
        type: "object",
        properties: {
          issue_number: { type: "number" },
          milestone_number: { type: "number" },
          milestone_title: { type: "string" },
        },
        required: ["issue_number"],
        anyOf: [{ required: ["milestone_number"] }, { required: ["milestone_title"] }],
      },
    };

    const recommended = renderToolRecommendedExample("safeoutputs", tool);
    expect(recommended).toContain("--issue_number 123");
    expect(recommended).toContain("--milestone_number 1");
  });
});
