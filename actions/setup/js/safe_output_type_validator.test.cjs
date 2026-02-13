import { describe, it, expect, beforeEach, vi } from "vitest";

// Mock core global
const mockCore = {
  warning: vi.fn(),
  info: vi.fn(),
  error: vi.fn(),
};
global.core = mockCore;

// Sample validation config to set in environment
const SAMPLE_VALIDATION_CONFIG = {
  create_issue: {
    defaultMax: 1,
    fields: {
      title: { required: true, type: "string", sanitize: true, maxLength: 128 },
      body: { required: true, type: "string", sanitize: true, maxLength: 65000 },
      labels: { type: "array", itemType: "string", itemSanitize: true, itemMaxLength: 128 },
      parent: { issueOrPRNumber: true },
      temporary_id: { type: "string" },
    },
  },
  add_comment: {
    defaultMax: 1,
    fields: {
      body: { required: true, type: "string", sanitize: true, maxLength: 65000 },
      item_number: { issueOrPRNumber: true },
    },
  },
  create_pull_request: {
    defaultMax: 1,
    fields: {
      title: { required: true, type: "string", sanitize: true, maxLength: 128 },
      body: { required: true, type: "string", sanitize: true, maxLength: 65000 },
      branch: { required: true, type: "string", sanitize: true, maxLength: 256 },
      labels: { type: "array", itemType: "string", itemSanitize: true, itemMaxLength: 128 },
    },
  },
  update_issue: {
    defaultMax: 1,
    customValidation: "requiresOneOf:status,title,body",
    fields: {
      status: { type: "string", enum: ["open", "closed"] },
      title: { type: "string", sanitize: true, maxLength: 128 },
      body: { type: "string", sanitize: true, maxLength: 65000 },
      issue_number: { issueOrPRNumber: true },
    },
  },
  assign_to_agent: {
    defaultMax: 1,
    customValidation: "requiresOneOf:issue_number,pull_number",
    fields: {
      issue_number: { optionalPositiveInteger: true },
      pull_number: { optionalPositiveInteger: true },
      agent: { type: "string", sanitize: true, maxLength: 128 },
    },
  },
  create_pull_request_review_comment: {
    defaultMax: 1,
    customValidation: "startLineLessOrEqualLine",
    fields: {
      path: { required: true, type: "string" },
      line: { required: true, positiveInteger: true },
      body: { required: true, type: "string", sanitize: true, maxLength: 65000 },
      start_line: { optionalPositiveInteger: true },
      side: { type: "string", enum: ["LEFT", "RIGHT"] },
    },
  },
  submit_pull_request_review: {
    defaultMax: 1,
    fields: {
      body: { type: "string", sanitize: true, maxLength: 65000 },
      event: { type: "string", enum: ["APPROVE", "REQUEST_CHANGES", "COMMENT"] },
    },
  },
  link_sub_issue: {
    defaultMax: 5,
    customValidation: "parentAndSubDifferent",
    fields: {
      parent_issue_number: { required: true, issueNumberOrTemporaryId: true },
      sub_issue_number: { required: true, issueNumberOrTemporaryId: true },
    },
  },
  noop: {
    defaultMax: 1,
    fields: {
      message: { required: true, type: "string", sanitize: true, maxLength: 65000 },
    },
  },
  missing_tool: {
    defaultMax: 20,
    fields: {
      tool: { required: true, type: "string", sanitize: true, maxLength: 128 },
      reason: { required: true, type: "string", sanitize: true, maxLength: 256 },
      alternatives: { type: "string", sanitize: true, maxLength: 512 },
    },
  },
  create_code_scanning_alert: {
    defaultMax: 40,
    fields: {
      file: { required: true, type: "string", sanitize: true, maxLength: 512 },
      line: { required: true, positiveInteger: true },
      severity: { required: true, type: "string", enum: ["error", "warning", "info", "note"] },
      message: { required: true, type: "string", sanitize: true, maxLength: 2048 },
      column: { optionalPositiveInteger: true },
      ruleIdSuffix: {
        type: "string",
        pattern: "^[a-zA-Z0-9_-]+$",
        patternError: "must contain only alphanumeric characters, hyphens, and underscores",
        sanitize: true,
        maxLength: 128,
      },
    },
  },
};

describe("safe_output_type_validator", () => {
  beforeEach(async () => {
    vi.clearAllMocks();
    // Reset the validation config cache before each test
    const { resetValidationConfigCache } = await import("./safe_output_type_validator.cjs");
    resetValidationConfigCache();
    // Set the validation config in environment
    process.env.GH_AW_VALIDATION_CONFIG = JSON.stringify(SAMPLE_VALIDATION_CONFIG);
  });

  describe("loadValidationConfig", () => {
    it("should load config from environment variable", async () => {
      const { loadValidationConfig } = await import("./safe_output_type_validator.cjs");

      const config = loadValidationConfig();

      expect(config).toBeDefined();
      expect(config.create_issue).toBeDefined();
      expect(config.create_issue.defaultMax).toBe(1);
    });

    it("should return empty config when env var is not set", async () => {
      delete process.env.GH_AW_VALIDATION_CONFIG;
      const { loadValidationConfig, resetValidationConfigCache } = await import("./safe_output_type_validator.cjs");
      resetValidationConfigCache();

      const config = loadValidationConfig();

      expect(config).toEqual({});
    });

    it("should return empty config on invalid JSON", async () => {
      process.env.GH_AW_VALIDATION_CONFIG = "invalid json";
      const { loadValidationConfig, resetValidationConfigCache } = await import("./safe_output_type_validator.cjs");
      resetValidationConfigCache();

      const config = loadValidationConfig();

      expect(config).toEqual({});
      expect(mockCore.error).toHaveBeenCalled();
    });
  });

  describe("validateItem", () => {
    it("should validate create_issue with all required fields", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "create_issue", title: "Test Issue", body: "Test body" }, "create_issue", 1);

      expect(result.isValid).toBe(true);
      expect(result.normalizedItem).toBeDefined();
    });

    it("should fail validation when required title is missing", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "create_issue", body: "Test body" }, "create_issue", 1);

      expect(result.isValid).toBe(false);
      expect(result.error).toContain("title");
    });

    it("should fail validation when required body is missing", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "create_issue", title: "Test title" }, "create_issue", 1);

      expect(result.isValid).toBe(false);
      expect(result.error).toContain("body");
    });

    it("should sanitize string fields", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "create_issue", title: "Test @mention Issue", body: "Test body" }, "create_issue", 1);

      expect(result.isValid).toBe(true);
      // The sanitizeContent function converts @mentions to backticked format
      expect(result.normalizedItem.title).toContain("`@mention`");
    });
  });

  describe("validatePositiveInteger", () => {
    it("should validate positive integer", async () => {
      const { validatePositiveInteger } = await import("./safe_output_type_validator.cjs");

      const result = validatePositiveInteger(42, "line", 1);

      expect(result.isValid).toBe(true);
      expect(result.normalizedValue).toBe(42);
    });

    it("should reject negative numbers", async () => {
      const { validatePositiveInteger } = await import("./safe_output_type_validator.cjs");

      const result = validatePositiveInteger(-1, "line", 1);

      expect(result.isValid).toBe(false);
    });

    it("should reject zero", async () => {
      const { validatePositiveInteger } = await import("./safe_output_type_validator.cjs");

      const result = validatePositiveInteger(0, "line", 1);

      expect(result.isValid).toBe(false);
    });

    it("should parse string numbers", async () => {
      const { validatePositiveInteger } = await import("./safe_output_type_validator.cjs");

      const result = validatePositiveInteger("42", "line", 1);

      expect(result.isValid).toBe(true);
      expect(result.normalizedValue).toBe(42);
    });
  });

  describe("validateOptionalPositiveInteger", () => {
    it("should accept undefined", async () => {
      const { validateOptionalPositiveInteger } = await import("./safe_output_type_validator.cjs");

      const result = validateOptionalPositiveInteger(undefined, "column", 1);

      expect(result.isValid).toBe(true);
    });

    it("should validate positive integer when provided", async () => {
      const { validateOptionalPositiveInteger } = await import("./safe_output_type_validator.cjs");

      const result = validateOptionalPositiveInteger(5, "column", 1);

      expect(result.isValid).toBe(true);
      expect(result.normalizedValue).toBe(5);
    });

    it("should reject zero when provided", async () => {
      const { validateOptionalPositiveInteger } = await import("./safe_output_type_validator.cjs");

      const result = validateOptionalPositiveInteger(0, "column", 1);

      expect(result.isValid).toBe(false);
    });
  });

  describe("validateIssueOrPRNumber", () => {
    it("should accept undefined", async () => {
      const { validateIssueOrPRNumber } = await import("./safe_output_type_validator.cjs");

      const result = validateIssueOrPRNumber(undefined, "item_number", 1);

      expect(result.isValid).toBe(true);
    });

    it("should accept number", async () => {
      const { validateIssueOrPRNumber } = await import("./safe_output_type_validator.cjs");

      const result = validateIssueOrPRNumber(123, "item_number", 1);

      expect(result.isValid).toBe(true);
    });

    it("should accept string", async () => {
      const { validateIssueOrPRNumber } = await import("./safe_output_type_validator.cjs");

      const result = validateIssueOrPRNumber("456", "item_number", 1);

      expect(result.isValid).toBe(true);
    });
  });

  describe("validateIssueNumberOrTemporaryId", () => {
    it("should accept positive integer", async () => {
      const { validateIssueNumberOrTemporaryId } = await import("./safe_output_type_validator.cjs");

      const result = validateIssueNumberOrTemporaryId(123, "issue_number", 1);

      expect(result.isValid).toBe(true);
      expect(result.normalizedValue).toBe(123);
      expect(result.isTemporary).toBe(false);
    });

    it("should accept temporary ID", async () => {
      const { validateIssueNumberOrTemporaryId } = await import("./safe_output_type_validator.cjs");

      const result = validateIssueNumberOrTemporaryId("aw_abc123", "issue_number", 1);

      expect(result.isValid).toBe(true);
      expect(result.isTemporary).toBe(true);
      expect(result.normalizedValue).toBe("aw_abc123");
    });

    it("should reject invalid values", async () => {
      const { validateIssueNumberOrTemporaryId } = await import("./safe_output_type_validator.cjs");

      const result = validateIssueNumberOrTemporaryId(-1, "issue_number", 1);

      expect(result.isValid).toBe(false);
    });
  });

  describe("getMaxAllowedForType", () => {
    it("should return defaultMax from config", async () => {
      const { getMaxAllowedForType } = await import("./safe_output_type_validator.cjs");

      const max = getMaxAllowedForType("create_issue");

      expect(max).toBe(1);
    });

    it("should return overridden max from config", async () => {
      const { getMaxAllowedForType } = await import("./safe_output_type_validator.cjs");

      const max = getMaxAllowedForType("create_issue", { create_issue: { max: 5 } });

      expect(max).toBe(5);
    });

    it("should return 1 for unknown type", async () => {
      const { getMaxAllowedForType } = await import("./safe_output_type_validator.cjs");

      const max = getMaxAllowedForType("unknown_type");

      expect(max).toBe(1);
    });
  });

  describe("hasValidationConfig", () => {
    it("should return true for known type", async () => {
      const { hasValidationConfig } = await import("./safe_output_type_validator.cjs");

      expect(hasValidationConfig("create_issue")).toBe(true);
    });

    it("should return false for unknown type", async () => {
      const { hasValidationConfig } = await import("./safe_output_type_validator.cjs");

      expect(hasValidationConfig("unknown_type")).toBe(false);
    });
  });

  describe("custom validation: requiresOneOf", () => {
    it("should pass when at least one field is present", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "update_issue", status: "open" }, "update_issue", 1);

      expect(result.isValid).toBe(true);
    });

    it("should fail when none of the required fields are present", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "update_issue" }, "update_issue", 1);

      expect(result.isValid).toBe(false);
      expect(result.error).toContain("requires at least one of");
    });

    it("should pass for assign_to_agent with issue_number", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "assign_to_agent", issue_number: 123 }, "assign_to_agent", 1);

      expect(result.isValid).toBe(true);
    });

    it("should pass for assign_to_agent with pull_number", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "assign_to_agent", pull_number: 456 }, "assign_to_agent", 1);

      expect(result.isValid).toBe(true);
    });

    it("should fail for assign_to_agent without issue_number or pull_number", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "assign_to_agent", agent: "copilot" }, "assign_to_agent", 1);

      expect(result.isValid).toBe(false);
      expect(result.error).toContain("requires at least one of");
      expect(result.error).toContain("issue_number");
      expect(result.error).toContain("pull_number");
    });
  });

  describe("custom validation: startLineLessOrEqualLine", () => {
    it("should pass when start_line <= line", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "create_pull_request_review_comment", path: "test.js", line: 10, start_line: 5, body: "Test comment" }, "create_pull_request_review_comment", 1);

      expect(result.isValid).toBe(true);
    });

    it("should fail when start_line > line", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "create_pull_request_review_comment", path: "test.js", line: 5, start_line: 10, body: "Test comment" }, "create_pull_request_review_comment", 1);

      expect(result.isValid).toBe(false);
      expect(result.error).toContain("start_line");
    });
  });

  describe("custom validation: parentAndSubDifferent", () => {
    it("should pass when parent and sub are different", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "link_sub_issue", parent_issue_number: 1, sub_issue_number: 2 }, "link_sub_issue", 1);

      expect(result.isValid).toBe(true);
    });

    it("should fail when parent and sub are the same", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "link_sub_issue", parent_issue_number: 1, sub_issue_number: 1 }, "link_sub_issue", 1);

      expect(result.isValid).toBe(false);
      expect(result.error).toContain("must be different");
    });
  });

  describe("enum validation", () => {
    it("should validate enum values (case-insensitive)", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "update_issue", status: "OPEN" }, "update_issue", 1);

      expect(result.isValid).toBe(true);
      expect(result.normalizedItem.status).toBe("open");
    });

    it("should reject invalid enum value", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "update_issue", status: "invalid" }, "update_issue", 1);

      expect(result.isValid).toBe(false);
      expect(result.error).toContain("must be 'open' or 'closed'");
    });
  });

  describe("pattern validation", () => {
    it("should validate pattern match", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem(
        {
          type: "create_code_scanning_alert",
          file: "test.js",
          line: 10,
          severity: "warning",
          message: "Test",
          ruleIdSuffix: "test-rule-123",
        },
        "create_code_scanning_alert",
        1
      );

      expect(result.isValid).toBe(true);
    });

    it("should reject pattern mismatch with custom error", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "create_code_scanning_alert", file: "test.js", line: 10, severity: "warning", message: "Test", ruleIdSuffix: "test rule!" }, "create_code_scanning_alert", 1);

      expect(result.isValid).toBe(false);
      expect(result.error).toContain("must contain only alphanumeric characters, hyphens, and underscores");
    });
  });

  describe("array validation", () => {
    it("should validate array of strings", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "create_issue", title: "Test", body: "Body", labels: ["bug", "enhancement"] }, "create_issue", 1);

      expect(result.isValid).toBe(true);
      expect(Array.isArray(result.normalizedItem.labels)).toBe(true);
    });

    it("should reject array with non-string items", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "create_issue", title: "Test", body: "Body", labels: ["bug", 123] }, "create_issue", 1);

      expect(result.isValid).toBe(false);
      expect(result.error).toContain("must contain only strings");
    });
  });
});
