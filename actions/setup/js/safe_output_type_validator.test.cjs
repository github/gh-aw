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
    dataEnabled: true,
    fields: {
      title: { required: true, type: "string", sanitize: true, maxLength: 128 },
      body: { required: true, type: "string", sanitize: true, maxLength: 65000, minLength: 20 },
      labels: { type: "array", itemType: "string", itemSanitize: true, itemMaxLength: 128 },
      parent: { issueOrPRNumber: true },
      temporary_id: { type: "string" },
    },
  },
  linear_create_issue: {
    defaultMax: 1,
    fields: {
      title: { required: true, type: "string", sanitize: true, maxLength: 128, rejectIfOversized: true },
      body: { required: true, type: "string", sanitize: true, maxLength: 65000, minLength: 20, rejectIfOversized: true },
    },
  },
  add_comment: {
    defaultMax: 1,
    dataEnabled: true,
    fields: {
      body: { required: true, type: "string", sanitize: true, maxLength: 65000 },
      item_number: { issueOrPRNumber: true },
      comment_id: { optionalPositiveInteger: true },
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
  add_labels: {
    defaultMax: 3,
    fields: {
      labels: { required: true, type: "array" },
      item_number: { issueNumberOrTemporaryId: true },
    },
  },
  remove_labels: {
    defaultMax: 3,
    fields: {
      labels: { required: true, type: "array" },
      item_number: { issueNumberOrTemporaryId: true },
    },
  },
  update_issue: {
    defaultMax: 1,
    customValidation: "requiresOneOf:status,title,body,labels,assignees,milestone",
    fields: {
      status: { type: "string", enum: ["open", "closed"] },
      title: { type: "string", sanitize: true, maxLength: 128 },
      body: { type: "string", sanitize: true, maxLength: 65000 },
      labels: { type: "array" },
      assignees: { type: "array", itemType: "string", itemSanitize: true, itemMaxLength: 39 },
      milestone: { optionalPositiveInteger: true },
      issue_number: { issueOrPRNumber: true },
    },
  },
  update_pull_request: {
    defaultMax: 1,
    customValidation: "requiresOneOf:title,body,update_branch",
    fields: {
      title: { type: "string", sanitize: true, maxLength: 256 },
      body: { type: "string", sanitize: true, maxLength: 65000 },
      update_branch: { type: "boolean" },
      pull_request_number: { issueOrPRNumber: true },
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
  assign_milestone: {
    defaultMax: 1,
    customValidation: "requiresOneOf:milestone_number,milestone_title",
    fields: {
      issue_number: { issueNumberOrTemporaryId: true },
      milestone_number: { optionalPositiveInteger: true },
      milestone_title: { type: "string", sanitize: true, maxLength: 128 },
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
      pull_request_number: { issueOrPRNumber: true },
      repo: { type: "string", maxLength: 256 },
    },
  },
  upload_asset: {
    defaultMax: 10,
    fields: {
      path: { required: true, type: "string" },
    },
  },
  close_issue: {
    defaultMax: 1,
    fields: {
      issue_number: { optionalPositiveInteger: true },
    },
  },
  dismiss_pull_request_review: {
    defaultMax: 10,
    fields: {
      review_id: { optionalPositiveInteger: true, allowAuto: true },
      justification: { required: true, type: "string", sanitize: true, minLength: 20, maxLength: 65000 },
    },
  },
  push_to_pull_request_branch: {
    defaultMax: 1,
    fields: {
      message: { required: true, type: "string", sanitize: true, maxLength: 65000 },
    },
  },
  set_issue_type: {
    defaultMax: 5,
    fields: {
      issue_number: { issueOrPRNumber: true },
      issue_type: { required: true, type: "string", sanitize: true, maxLength: 128 },
      rationale: { type: "string", sanitize: true, maxLength: 280, "x-strip-on-error": true },
      confidence: { type: "string", enum: ["LOW", "MEDIUM", "HIGH"], "x-strip-on-error": true },
      suggest: { type: "boolean" },
    },
  },
  set_issue_field: {
    defaultMax: 5,
    customValidation: "requiresOneOf:field_name,field_node_id",
    fields: {
      issue_number: { issueOrPRNumber: true },
      field_name: { type: "string", sanitize: true, maxLength: 128 },
      field_node_id: { type: "string", maxLength: 256 },
      value: { required: true, type: "string", sanitize: true, maxLength: 256 },
      rationale: { type: "string", sanitize: true, maxLength: 280, "x-strip-on-error": true },
      confidence: { type: "string", enum: ["LOW", "MEDIUM", "HIGH"], "x-strip-on-error": true },
      suggest: { type: "boolean" },
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
  create_discussion: {
    defaultMax: 1,
    fields: {
      title: { required: true, type: "string", sanitize: true, maxLength: 128 },
      body: { required: true, type: "string", sanitize: true, maxLength: 65000, minLength: 64 },
      category: { type: "string", sanitize: true, maxLength: 128 },
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
  update_release: {
    defaultMax: 1,
    fields: {
      tag: { type: "string", sanitize: true, maxLength: 256 },
      operation: { required: true, type: "string", enum: ["replace", "append", "prepend"] },
      body: { required: true, type: "string", sanitize: true, maxLength: 65000, minLength: 20 },
    },
  },
  dispatch_workflow: {
    defaultMax: 1,
    fields: {
      workflow_name: {
        required: true,
        type: "string",
        sanitize: true,
        minLength: 1,
        maxLength: 256,
        pattern: ".*\\S.*",
        patternError: "must not be empty",
      },
      inputs: { type: "object" },
    },
  },
  hide_comment: {
    defaultMax: 5,
    fields: {
      comment_id: {
        required: true,
        type: "string",
        maxLength: 256,
        typeHint: "GraphQL node ID string (e.g. 'IC_kwDOABCD123456'); numeric REST comment IDs are accepted but may not resolve for all comment types (e.g. PR review comments)",
      },
      reason: { type: "string" },
    },
  },
};

const ISSUE_CLOSING_KEYWORDS = ["fix", "fixes", "fixed", "close", "closes", "closed", "resolve", "resolves", "resolved"];
const ISSUE_CLOSING_KEYWORD_PATTERN = ISSUE_CLOSING_KEYWORDS.join("|");
const BACKTICKED_CLOSER_WHOLE_RE = new RegExp("`(?:" + ISSUE_CLOSING_KEYWORD_PATTERN + ")\\b[^`]*#\\d+`", "i");
const BACKTICKED_CLOSER_REFERENCE_RE = new RegExp("(?:" + ISSUE_CLOSING_KEYWORD_PATTERN + ")\\b\\s+`(?:[a-zA-Z0-9_.-]+\\/[a-zA-Z0-9_.-]+)?#\\d+`", "i");
const BACKTICKED_CLOSER_BOTH_RE = new RegExp("`(?:" + ISSUE_CLOSING_KEYWORD_PATTERN + ")`\\s+`(?:[a-zA-Z0-9_.-]+\\/[a-zA-Z0-9_.-]+)?#\\d+`", "i");

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

      const result = validateItem({ type: "create_issue", title: "Test Issue", body: "Detailed issue body text." }, "create_issue", 1);

      expect(result.isValid).toBe(true);
      expect(result.normalizedItem).toBeDefined();
    });

    it("should validate dispatch_workflow with non-empty workflow_name", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "dispatch_workflow", workflow_name: "haiku-printer", inputs: {} }, "dispatch_workflow", 1);

      expect(result.isValid).toBe(true);
    });

    it("should fail dispatch_workflow when workflow_name is whitespace-only", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "dispatch_workflow", workflow_name: "   ", inputs: {} }, "dispatch_workflow", 1);

      expect(result.isValid).toBe(false);
      expect(result.error).toContain("workflow_name");
    });

    it("should fail validation when required title is missing", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "create_issue", body: "Detailed issue body text." }, "create_issue", 1);

      expect(result.isValid).toBe(false);
      expect(result.error).toContain("title");
    });

    it("should fail validation when required body is missing", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "create_issue", title: "Test title" }, "create_issue", 1);

      expect(result.isValid).toBe(false);
      expect(result.error).toContain("body");
    });

    it("should validate add_labels with structured label entries", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem(
        {
          type: "add_labels",
          item_number: 123,
          labels: [{ name: "bug", rationale: "Known failure mode", confidence: "high", suggest: true }],
        },
        "add_labels",
        1
      );

      expect(result.isValid).toBe(true);
      expect(result.normalizedItem.labels).toEqual([{ name: "bug", rationale: "Known failure mode", confidence: "HIGH", suggest: true }]);
    });

    it("should truncate structured label rationale for all issue-intent label mutations", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      for (const [type, item] of [
        ["add_labels", { type: "add_labels", item_number: 123, labels: [{ name: "bug", rationale: "a".repeat(350) }] }],
        ["remove_labels", { type: "remove_labels", item_number: 123, labels: [{ name: "bug", rationale: "a".repeat(350) }] }],
        ["update_issue", { type: "update_issue", issue_number: 123, labels: [{ name: "bug", rationale: "a".repeat(350) }] }],
      ]) {
        const result = validateItem(item, type, 1);

        expect(result.isValid).toBe(true);
        expect(result.normalizedItem.labels[0].rationale).toBe("a".repeat(280));
      }
    });

    it("should fail add_labels when structured label entry is invalid", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem(
        {
          type: "add_labels",
          item_number: 123,
          labels: [{ rationale: "missing name" }],
        },
        "add_labels",
        1
      );

      expect(result.isValid).toBe(false);
      expect(result.error).toContain("name");
    });

    it("should strip invalid label confidence instead of rejecting the item", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      // Numeric string confidence in a label entry
      const result = validateItem(
        {
          type: "add_labels",
          item_number: 123,
          labels: [{ name: "bug", confidence: "0.95", rationale: "Known failure mode" }],
        },
        "add_labels",
        1
      );

      expect(result.isValid).toBe(true);
      expect(result.normalizedItem.labels[0].name).toBe("bug");
      expect(result.normalizedItem.labels[0].rationale).toBe("Known failure mode");
      expect(result.normalizedItem.labels[0].confidence).toBeUndefined();
    });

    it("should strip non-string label rationale instead of rejecting the item", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      // Numeric rationale in a label entry
      const result = validateItem(
        {
          type: "add_labels",
          item_number: 123,
          labels: [{ name: "bug", rationale: 99 }],
        },
        "add_labels",
        1
      );

      expect(result.isValid).toBe(true);
      expect(result.normalizedItem.labels[0].name).toBe("bug");
      expect(result.normalizedItem.labels[0].rationale).toBeUndefined();
    });

    it("should sanitize string fields", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "create_issue", title: "Test @mention Issue", body: "Detailed issue body text." }, "create_issue", 1);

      expect(result.isValid).toBe(true);
      // The sanitizeContent function converts @mentions to backticked format
      expect(result.normalizedItem.title).toContain("`@mention`");
    });

    it("should append structured data as fenced JSON to body fields", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem(
        {
          type: "add_comment",
          body: "Review complete.",
          data: {
            verdict: "APPROVE",
            marker: "<!-- [PIPELINE-VERDICT] APPROVE -->",
            criteria_passed: 5,
          },
        },
        "add_comment",
        1
      );

      expect(result.isValid).toBe(true);
      expect(result.normalizedItem.body).toContain("Review complete.");
      expect(result.normalizedItem.body).toContain("Structured data:");
      expect(result.normalizedItem.body).toContain("```json");
      expect(result.normalizedItem.body).toContain('"verdict": "APPROVE"');
      expect(result.normalizedItem.body).toContain('"marker": "<!-- [PIPELINE-VERDICT] APPROVE -->"');
      expect(result.normalizedItem.data).toEqual({
        verdict: "APPROVE",
        marker: "<!-- [PIPELINE-VERDICT] APPROVE -->",
        criteria_passed: 5,
      });
    });

    it("should reject data values that are not objects", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "add_comment", body: "Review complete.", data: ["APPROVE"] }, "add_comment", 1);

      expect(result.isValid).toBe(false);
      expect(result.error).toContain("'data' must be an object");
    });

    it("should reject data when not enabled", async () => {
      const { validateItem, resetValidationConfigCache } = await import("./safe_output_type_validator.cjs");
      const configWithoutData = JSON.parse(JSON.stringify(SAMPLE_VALIDATION_CONFIG));
      delete configWithoutData.add_comment.dataEnabled;
      process.env.GH_AW_VALIDATION_CONFIG = JSON.stringify(configWithoutData);
      resetValidationConfigCache();

      const result = validateItem({ type: "add_comment", body: "Review complete.", data: { verdict: "APPROVE" } }, "add_comment", 1);
      expect(result.isValid).toBe(false);
      expect(result.error).toContain("'data' is not enabled");
    });

    it("should enforce data schema when configured", async () => {
      const { validateItem, resetValidationConfigCache } = await import("./safe_output_type_validator.cjs");
      const configWithDataSchema = JSON.parse(JSON.stringify(SAMPLE_VALIDATION_CONFIG));
      configWithDataSchema.add_comment.dataSchema = {
        type: "object",
        properties: {
          verdict: { type: "string", enum: ["APPROVE", "REJECT"] },
          criteria_passed: { type: "number" },
        },
        required: ["verdict"],
        additionalProperties: false,
      };
      process.env.GH_AW_VALIDATION_CONFIG = JSON.stringify(configWithDataSchema);
      resetValidationConfigCache();

      const invalid = validateItem({ type: "add_comment", body: "Review complete.", data: { verdict: "APPROVE", criteria_passed: 5, extra: "x" } }, "add_comment", 1);
      expect(invalid.isValid).toBe(false);
      expect(invalid.error).toContain("'data'.extra");

      const valid = validateItem({ type: "add_comment", body: "Review complete.", data: { verdict: "APPROVE", criteria_passed: 5 } }, "add_comment", 1);
      expect(valid.isValid).toBe(true);
    });

    it("should enforce runtime data schema supplied as JSON string", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const invalid = validateItem({ type: "add_comment", body: "Review complete.", data: { verdict: "APPROVE", extra: "x" } }, "add_comment", 1, {
        dataEnabled: true,
        dataSchema: JSON.stringify({
          type: "object",
          properties: { verdict: { type: "string" } },
          required: ["verdict"],
          additionalProperties: false,
        }),
      });
      expect(invalid.isValid).toBe(false);
      expect(invalid.error).toContain("'data'.extra");
    });

    it("should normalize a backticked issue reference when enabled", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "create_pull_request", title: "Test", body: "Closes `#123`", branch: "fix/test" }, "create_pull_request", 1, { normalizeIssueClosingKeywords: true });

      expect(result.isValid).toBe(true);
      expect(result.normalizedItem.body).toContain("Closes #123");
      expect(result.normalizedItem.body).not.toContain("`#123`");
    });

    it("should normalize a whole backticked closing phrase when enabled", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "create_pull_request", title: "Test", body: "`Resolves GitHub/Repo#321`", branch: "fix/test" }, "create_pull_request", 1, { normalizeIssueClosingKeywords: true });

      expect(result.isValid).toBe(true);
      expect(result.normalizedItem.body).toContain("Resolves GitHub/Repo#321");
      expect(result.normalizedItem.body).not.toContain("`Resolves GitHub/Repo#321`");
    });

    it("should normalize separately backticked keyword and reference when enabled", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "create_pull_request", title: "Test", body: "`Fixed` `#99`", branch: "fix/test" }, "create_pull_request", 1, { normalizeIssueClosingKeywords: true });

      expect(result.isValid).toBe(true);
      expect(result.normalizedItem.body).toContain("Fixed #99");
      expect(result.normalizedItem.body).not.toContain("`Fixed` `#99`");
    });

    it("should not normalize backticked closing references when disabled", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "create_issue", title: "Test", body: "Detailed context. Closes `#123`" }, "create_issue", 1, { normalizeIssueClosingKeywords: false });

      expect(result.isValid).toBe(true);
      expect(result.normalizedItem.body).toContain("`#123`");
    });

    it("should not modify unrelated backticked text when normalization is enabled", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "add_comment", body: "Use `#123` for docs reference.\nThen Closes `#456`." }, "add_comment", 1, { normalizeIssueClosingKeywords: true });

      expect(result.isValid).toBe(true);
      expect(result.normalizedItem.body).toContain("Use `#123` for docs reference.");
      expect(result.normalizedItem.body).toContain("Then Closes #456.");
      expect(result.normalizedItem.body).toContain("`#123`");
      expect(result.normalizedItem.body).not.toContain("`#456`");
    });

    it("should leave malformed backticks unchanged", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "add_comment", body: "Closes` #123" }, "add_comment", 1, { normalizeIssueClosingKeywords: true });

      expect(result.isValid).toBe(true);
      expect(result.normalizedItem.body).toContain("Closes` #123");
    });

    it("should normalize all supported closing keywords in whole and split backtick forms", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      for (const keyword of ISSUE_CLOSING_KEYWORDS) {
        const mixedCase = keyword[0].toUpperCase() + keyword.slice(1);
        const wholeResult = validateItem({ type: "add_comment", body: `\`${mixedCase} #12\`` }, "add_comment", 1, { normalizeIssueClosingKeywords: true });
        expect(wholeResult.isValid).toBe(true);
        expect(wholeResult.normalizedItem.body).toBe(`${mixedCase} #12`);

        const splitResult = validateItem({ type: "add_comment", body: `\`${mixedCase}\` \`owner/repo#12\`` }, "add_comment", 1, { normalizeIssueClosingKeywords: true });
        expect(splitResult.isValid).toBe(true);
        expect(splitResult.normalizedItem.body).toBe(`${mixedCase} owner/repo#12`);
      }
    });

    it("should only normalize body fields of configured tool types", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const updateIssueResult = validateItem({ type: "update_issue", body: "Closes `#321`", issue_number: 7 }, "update_issue", 1, { normalizeIssueClosingKeywords: true });
      expect(updateIssueResult.isValid).toBe(true);
      expect(updateIssueResult.normalizedItem.body).toContain("`#321`");

      const prResult = validateItem({ type: "create_pull_request", title: "Closes `#654`", body: "Body text", branch: "feature/test" }, "create_pull_request", 1, { normalizeIssueClosingKeywords: true });
      expect(prResult.isValid).toBe(true);
      expect(prResult.normalizedItem.title).toContain("`#654`");
      expect(prResult.normalizedItem.body).toBe("Body text");
    });

    describe("fuzz: normalizeIssueClosingKeywords invariants", () => {
      async function normalizeBody(body, options = { normalizeIssueClosingKeywords: true }) {
        const { validateItem } = await import("./safe_output_type_validator.cjs");
        const result = validateItem({ type: "add_comment", body }, "add_comment", 1, options);
        expect(result.isValid).toBe(true);
        return result.normalizedItem.body;
      }

      it("is idempotent and preserves non-closing backticked references for fuzz seed cases", async () => {
        const keywords = [...ISSUE_CLOSING_KEYWORDS, "FIXED"];
        const references = ["#1", "#42", "Owner/Repo#987", "team.repo/service_name#9001"];
        const prefixes = ["", "Before: ", "  ", "- ", "Start\n", "prefix `code` "];
        const suffixes = ["", ".", " now", "\nTrailing line", " -- done"];
        const seedBodies = [];

        for (const keyword of keywords) {
          for (const reference of references) {
            for (const prefix of prefixes) {
              for (const suffix of suffixes) {
                seedBodies.push(`${prefix}\`${keyword} ${reference}\`${suffix}\nUse \`${reference}\` for docs`);
                seedBodies.push(`${prefix}\`${keyword}\` \`${reference}\`${suffix}\nUse \`${reference}\` for docs`);
                seedBodies.push(`${prefix}\`${keyword}\` ${reference}${suffix}\nUse \`${reference}\` for docs`);
                seedBodies.push(`${prefix}${keyword} \`${reference}\`${suffix}\nUse \`${reference}\` for docs`);
              }
            }
          }
        }

        for (const body of seedBodies) {
          const once = await normalizeBody(body);
          const twice = await normalizeBody(once);
          expect(twice).toBe(once);
          expect(once).toMatch(/Use `[^`]*#\d+` for docs/);
          expect(once).not.toMatch(BACKTICKED_CLOSER_WHOLE_RE);
          expect(once).not.toMatch(BACKTICKED_CLOSER_REFERENCE_RE);
          expect(once).not.toMatch(BACKTICKED_CLOSER_BOTH_RE);
        }
      });

      it("never throws and always returns a string for adversarial backtick-heavy inputs", async () => {
        const adversarial = [
          "`".repeat(200),
          "```Closes `#1`",
          "`Fixes`" + " `#".repeat(50) + "1" + "`".repeat(50),
          "\0Closes `#123`\0",
          "Resolves `owner/repo#1` and `close #2` and ````",
          "\n".repeat(20) + "`Fixed` `#99999`",
          "👾 `resolves #42` 👾",
        ];

        for (const body of adversarial) {
          await expect(normalizeBody(body)).resolves.toEqual(expect.any(String));
        }
      });
    });

    it("should validate submit_pull_request_review with pull_request_number and repo", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "submit_pull_request_review", event: "APPROVE", pull_request_number: "42", repo: "owner/repo" }, "submit_pull_request_review", 1);

      expect(result.isValid).toBe(true);
      expect(result.normalizedItem.pull_request_number).toBe("42"); // IssueOrPRNumber does not normalize strings to integers
      expect(result.normalizedItem.repo).toBe("owner/repo");
    });

    it("should validate submit_pull_request_review with only pull_request_number", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "submit_pull_request_review", event: "COMMENT", pull_request_number: 42 }, "submit_pull_request_review", 1);

      expect(result.isValid).toBe(true);
      expect(result.normalizedItem.pull_request_number).toBe(42);
      expect(result.normalizedItem.repo).toBeUndefined();
    });

    it("should validate submit_pull_request_review with only repo", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "submit_pull_request_review", event: "COMMENT", repo: "owner/repo" }, "submit_pull_request_review", 1);

      expect(result.isValid).toBe(true);
      expect(result.normalizedItem.pull_request_number).toBeUndefined();
      expect(result.normalizedItem.repo).toBe("owner/repo");
    });

    it("should validate submit_pull_request_review with neither pull_request_number nor repo", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "submit_pull_request_review", event: "COMMENT", body: "Looks good." }, "submit_pull_request_review", 1);

      expect(result.isValid).toBe(true);
    });

    it("should reject invalid submit_pull_request_review pull_request_number", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      // IssueOrPRNumber accepts any string or number - only reject invalid types
      const invalidValues = [{ foo: "bar" }, ["array"], true];
      for (const value of invalidValues) {
        const result = validateItem({ type: "submit_pull_request_review", event: "APPROVE", pull_request_number: value }, "submit_pull_request_review", 1);
        expect(result.isValid).toBe(false);
        expect(result.error).toContain("pull_request_number");
      }
    });

    it("should include typeHint in error when hide_comment comment_id is missing", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "hide_comment" }, "hide_comment", 1);

      expect(result.isValid).toBe(false);
      expect(result.error).toContain("GraphQL node ID");
    });

    it("should include typeHint in error when hide_comment comment_id is a numeric REST id", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "hide_comment", comment_id: 4748731349 }, "hide_comment", 1);

      expect(result.isValid).toBe(false);
      expect(result.error).toContain("GraphQL node ID");
    });

    it("should accept hide_comment with a GraphQL node ID comment_id", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "hide_comment", comment_id: "IC_kwDOABCD123456" }, "hide_comment", 1);

      expect(result.isValid).toBe(true);
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

  describe("dismiss_pull_request_review review_id", () => {
    it.each([
      [{ type: "dismiss_pull_request_review", justification: "This stale review no longer reflects the updated implementation." }, undefined],
      [{ type: "dismiss_pull_request_review", review_id: "auto", justification: "This stale review no longer reflects the updated implementation." }, "auto"],
      [{ type: "dismiss_pull_request_review", review_id: "123", justification: "This stale review no longer reflects the updated implementation." }, 123],
    ])("accepts automatic selection and positive review IDs", async (item, reviewId) => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem(item, "dismiss_pull_request_review", 1);

      expect(result.isValid).toBe(true);
      expect(result.normalizedItem?.review_id).toBe(reviewId);
    });

    it("rejects invalid review IDs", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "dismiss_pull_request_review", review_id: "invalid", justification: "This stale review no longer reflects the updated implementation." }, "dismiss_pull_request_review", 1);

      expect(result.isValid).toBe(false);
      expect(result.error).toContain("must be a valid positive integer");
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

    it("should accept temporary ID and normalize to # prefix form", async () => {
      const { validateIssueNumberOrTemporaryId } = await import("./safe_output_type_validator.cjs");

      const result = validateIssueNumberOrTemporaryId("aw_abc123", "issue_number", 1);

      expect(result.isValid).toBe(true);
      expect(result.isTemporary).toBe(true);
      expect(result.normalizedValue).toBe("#aw_abc123");
    });

    it("should accept temporary ID with leading '#' and normalize to # prefix form", async () => {
      const { validateIssueNumberOrTemporaryId } = await import("./safe_output_type_validator.cjs");

      const result = validateIssueNumberOrTemporaryId("#aw_abc123", "issue_number", 1);

      expect(result.isValid).toBe(true);
      expect(result.isTemporary).toBe(true);
      expect(result.normalizedValue).toBe("#aw_abc123");
    });

    it("should accept temporary ID with underscore in the suffix", async () => {
      const { validateIssueNumberOrTemporaryId } = await import("./safe_output_type_validator.cjs");

      const result = validateIssueNumberOrTemporaryId("aw_pr_fix", "item_number", 1);

      expect(result.isValid).toBe(true);
      expect(result.isTemporary).toBe(true);
      expect(result.normalizedValue).toBe("#aw_pr_fix");
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

    it("should return configured max for custom safe-job type", async () => {
      const { getMaxAllowedForType } = await import("./safe_output_type_validator.cjs");

      // Simulates a custom safe-job entry as written by safe_outputs_config_generation.go
      const max = getMaxAllowedForType("emit_finding", { emit_finding: { max: 25, description: "Emit a finding" } });

      expect(max).toBe(25);
    });

    it("should return 1 for custom safe-job type with no max in config", async () => {
      const { getMaxAllowedForType } = await import("./safe_output_type_validator.cjs");

      // Custom safe-job without max: field — should fall through to default of 1
      const max = getMaxAllowedForType("emit_finding", { emit_finding: { description: "Emit a finding" } });

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

    it("should pass when update_issue only includes labels", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "update_issue", labels: [{ name: "bug", confidence: "HIGH" }] }, "update_issue", 1);

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

    it("should fail for update_pull_request when update_branch is false and no title/body is provided", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "update_pull_request", update_branch: false }, "update_pull_request", 1);

      expect(result.isValid).toBe(false);
      expect(result.error).toContain("requires at least one of");
    });

    it("should validate assign_milestone with milestone_title only", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "assign_milestone", issue_number: 42, milestone_title: "v1.0" }, "assign_milestone", 1);

      expect(result.isValid).toBe(true);
      expect(result.normalizedItem).toBeDefined();
      expect(result.normalizedItem.milestone_title).toBe("v1.0");
    });

    it("should fail assign_milestone when both milestone_number and milestone_title are missing", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "assign_milestone", issue_number: 42 }, "assign_milestone", 1);

      expect(result.isValid).toBe(false);
      expect(result.error).toContain("requires at least one of");
      expect(result.error).toContain("milestone_number");
      expect(result.error).toContain("milestone_title");
    });

    it("should pass for update_pull_request when update_branch is true", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "update_pull_request", update_branch: true }, "update_pull_request", 1);

      expect(result.isValid).toBe(true);
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

    it("should validate issue intent confidence enums", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const typeResult = validateItem({ type: "set_issue_type", issue_type: "Bug", confidence: "high", suggest: true }, "set_issue_type", 1);
      expect(typeResult.isValid).toBe(true);
      expect(typeResult.normalizedItem.confidence).toBe("HIGH");

      const fieldResult = validateItem({ type: "set_issue_field", field_name: "Priority", value: "P1", confidence: "medium", rationale: "Customer escalation" }, "set_issue_field", 1);
      expect(fieldResult.isValid).toBe(true);
      expect(fieldResult.normalizedItem.confidence).toBe("MEDIUM");
    });

    it("should strip invalid confidence instead of rejecting the item (x-strip-on-error)", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      // Numeric string like "0.95" sent instead of enum value
      const typeResult = validateItem({ type: "set_issue_type", issue_type: "Bug", confidence: "0.95" }, "set_issue_type", 1);
      expect(typeResult.isValid).toBe(true);
      expect(typeResult.normalizedItem.issue_type).toBe("Bug");
      expect(typeResult.normalizedItem.confidence).toBeUndefined();

      // Raw number
      const fieldResult = validateItem({ type: "set_issue_field", field_name: "Priority", value: "P1", confidence: 0.9 }, "set_issue_field", 1);
      expect(fieldResult.isValid).toBe(true);
      expect(fieldResult.normalizedItem.value).toBe("P1");
      expect(fieldResult.normalizedItem.confidence).toBeUndefined();
    });

    it("should strip non-string rationale instead of rejecting the item (x-strip-on-error)", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      // Numeric rationale
      const typeResult = validateItem({ type: "set_issue_type", issue_type: "Bug", rationale: 42 }, "set_issue_type", 1);
      expect(typeResult.isValid).toBe(true);
      expect(typeResult.normalizedItem.issue_type).toBe("Bug");
      expect(typeResult.normalizedItem.rationale).toBeUndefined();

      // Object rationale
      const fieldResult = validateItem({ type: "set_issue_field", field_name: "Priority", value: "P1", rationale: { text: "bad" } }, "set_issue_field", 1);
      expect(fieldResult.isValid).toBe(true);
      expect(fieldResult.normalizedItem.value).toBe("P1");
      expect(fieldResult.normalizedItem.rationale).toBeUndefined();
    });

    it("should not strip required fields even when x-strip-on-error is set", async () => {
      const { validateItem, resetValidationConfigCache } = await import("./safe_output_type_validator.cjs");
      process.env.GH_AW_VALIDATION_CONFIG = JSON.stringify({
        ...SAMPLE_VALIDATION_CONFIG,
        misconfigured_type: {
          defaultMax: 1,
          fields: {
            name: { required: true, type: "string", "x-strip-on-error": true },
          },
        },
      });
      resetValidationConfigCache();

      const result = validateItem({ type: "misconfigured_type" }, "misconfigured_type", 1);

      expect(result.isValid).toBe(false);
      expect(result.error).toContain("requires a 'name' field");
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

  describe("minLength validation", () => {
    it("should reject create_issue body shorter than minLength", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "create_issue", title: "Test Issue", body: "Short" }, "create_issue", 1);

      expect(result.isValid).toBe(false);
      expect(result.error).toContain("too short");
      expect(result.error).toContain("20");
    });

    it("should accept create_issue body that meets minLength", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const body = "Detailed issue body with clear context.";
      const result = validateItem({ type: "create_issue", title: "Test Issue", body }, "create_issue", 1);

      expect(result.isValid).toBe(true);
    });

    it("should accept create_issue body at exact minLength", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const body = "Exactly twenty chars";
      expect(body.length).toBe(20);
      const result = validateItem({ type: "create_issue", title: "Test Issue", body }, "create_issue", 1);

      expect(result.isValid).toBe(true);
    });

    it("should reject create_issue body at minLength - 1", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const body = "Exactly nineteen ch";
      expect(body.length).toBe(19);
      const result = validateItem({ type: "create_issue", title: "Test Issue", body }, "create_issue", 1);

      expect(result.isValid).toBe(false);
      expect(result.error).toContain("too short");
    });

    it("should reject create_issue body that is only whitespace", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "create_issue", title: "Test Issue", body: "                         " }, "create_issue", 1);

      expect(result.isValid).toBe(false);
      expect(result.error).toContain("too short");
    });

    it("should reject body shorter than minLength", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "create_discussion", title: "Test Discussion", body: "PLACEHOLDER" }, "create_discussion", 1);

      expect(result.isValid).toBe(false);
      expect(result.error).toContain("too short");
      expect(result.error).toContain("64");
    });

    it("should reject single-word placeholder body", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "create_discussion", title: "Test Discussion", body: "TODO" }, "create_discussion", 1);

      expect(result.isValid).toBe(false);
      expect(result.error).toContain("too short");
    });

    it("should accept body that meets minLength", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const longEnoughBody = "No safe output job failures detected in the last 24 hours of analysis.";
      const result = validateItem({ type: "create_discussion", title: "Test Discussion", body: longEnoughBody }, "create_discussion", 1);

      expect(result.isValid).toBe(true);
    });

    it("should reject body that is only whitespace below minLength", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "create_discussion", title: "Test Discussion", body: "   short   " }, "create_discussion", 1);

      expect(result.isValid).toBe(false);
      expect(result.error).toContain("too short");
    });

    it("should reject update_release body shorter than minLength (e.g. 'test')", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "update_release", tag: "v1.0.0", operation: "prepend", body: "test" }, "update_release", 1);

      expect(result.isValid).toBe(false);
      expect(result.error).toContain("too short");
      expect(result.error).toContain("20");
    });

    it("should accept update_release body that meets minLength", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "update_release", tag: "v1.0.0", operation: "prepend", body: "Patch release with bug fixes and improvements." }, "update_release", 1);

      expect(result.isValid).toBe(true);
    });

    it("should reject update_release body that is only whitespace below minLength", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "update_release", tag: "v1.0.0", operation: "prepend", body: "   test   " }, "update_release", 1);

      expect(result.isValid).toBe(false);
      expect(result.error).toContain("too short");
    });
  });

  describe("rejectIfOversized validation", () => {
    it("should reject an oversized linear_create_issue title instead of truncating it", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const oversizedTitle = "x".repeat(129);
      const result = validateItem({ type: "linear_create_issue", title: oversizedTitle, body: "A sufficiently detailed body." }, "linear_create_issue", 1);

      expect(result.isValid).toBe(false);
      expect(result.error).toContain("exceeds maximum length");
    });

    it("should reject an oversized linear_create_issue body instead of truncating it", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const oversizedBody = "x".repeat(65001);
      const result = validateItem({ type: "linear_create_issue", title: "Valid title", body: oversizedBody }, "linear_create_issue", 1);

      expect(result.isValid).toBe(false);
      expect(result.error).toContain("exceeds maximum length");
    });

    it("should accept a linear_create_issue title/body within the configured limits", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "linear_create_issue", title: "Valid title", body: "A sufficiently detailed body." }, "linear_create_issue", 1);

      expect(result.isValid).toBe(true);
    });
  });

  describe("array validation", () => {
    it("should validate array of strings", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "create_issue", title: "Test", body: "Detailed issue body text.", labels: ["bug", "enhancement"] }, "create_issue", 1);

      expect(result.isValid).toBe(true);
      expect(Array.isArray(result.normalizedItem.labels)).toBe(true);
    });

    it("should normalize comma-separated labels string to array", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "create_issue", title: "Test", body: "Detailed issue body text.", labels: "reliability, telemetry" }, "create_issue", 1);

      expect(result.isValid).toBe(true);
      expect(result.normalizedItem.labels).toEqual(["reliability", "telemetry"]);
    });

    it("should normalize JSON-array labels string to array", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "create_issue", title: "Test", body: "Detailed issue body text.", labels: '["cookie"]' }, "create_issue", 1);

      expect(result.isValid).toBe(true);
      expect(result.normalizedItem.labels).toEqual(["cookie"]);
    });

    it("should normalize multi-label JSON-array string to array", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "create_issue", title: "Test", body: "Detailed issue body text.", labels: '["bug", "enhancement"]' }, "create_issue", 1);

      expect(result.isValid).toBe(true);
      expect(result.normalizedItem.labels).toEqual(["bug", "enhancement"]);
    });

    it("should filter non-string entries from JSON-array labels string", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "create_issue", title: "Test", body: "Detailed issue body text.", labels: '["bug", 123, "enhancement"]' }, "create_issue", 1);

      expect(result.isValid).toBe(true);
      expect(result.normalizedItem.labels).toEqual(["bug", "enhancement"]);
    });

    it("should fall back to comma-separated parsing for malformed JSON label string", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "create_issue", title: "Test", body: "Detailed issue body text.", labels: "[bug, enhancement" }, "create_issue", 1);

      expect(result.isValid).toBe(true);
      expect(result.normalizedItem.labels).toEqual(["[bug", "enhancement"]);
    });

    it("should reject array with non-string items", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "create_issue", title: "Test", body: "Detailed issue body text.", labels: ["bug", 123] }, "create_issue", 1);

      expect(result.isValid).toBe(false);
      expect(result.error).toContain("must contain only strings");
    });
  });

  describe("undeclared fields", () => {
    it("should preserve the normalized type and declared fields", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const item = {
        type: "create_pull_request",
        title: "Fix bug",
        body: "Fixes the thing",
        branch: "fix/bug",
      };

      const result = validateItem(item, "create_pull_request", 1);
      expect(result.isValid).toBe(true);
      expect(result.normalizedItem).toEqual(item);
    });

    it("should preserve declared add_comment.comment_id as a positive integer", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem({ type: "add_comment", body: "Test comment", comment_id: "123" }, "add_comment", 1);
      expect(result.isValid).toBe(true);
      expect(result.normalizedItem).toEqual({ type: "add_comment", body: "Test comment", comment_id: 123 });
    });

    it.each([
      { itemType: "update_pull_request", item: { type: "update_pull_request", title: "Updated title", base: "release", state: "closed" }, fieldName: "base" },
      { itemType: "update_pull_request", item: { type: "update_pull_request", title: "Updated title", base: "release", state: "closed" }, fieldName: "state" },
      { itemType: "upload_asset", item: { type: "upload_asset", path: "image.png", targetFileName: "../../.git/config" }, fieldName: "targetFileName" },
      { itemType: "create_issue", item: { type: "create_issue", title: "Test", body: "Detailed issue body text.", assignees: ["octocat"] }, fieldName: "assignees" },
      { itemType: "create_discussion", item: { type: "create_discussion", title: "Test", body: "This discussion body is intentionally long enough for validation.", labels: ["security"] }, fieldName: "labels" },
      { itemType: "close_issue", item: { type: "close_issue", issue_number: 1, state_reason: "not_planned" }, fieldName: "state_reason" },
      { itemType: "push_to_pull_request_branch", item: { type: "push_to_pull_request_branch", message: "Apply changes", diff_size: 0 }, fieldName: "diff_size" },
      { itemType: "create_pull_request", item: { type: "create_pull_request", title: "Fix bug", body: "Fixes the thing", branch: "fix/bug", base_commit: "abc123deadbeef" }, fieldName: "base_commit" },
    ])("should strip undeclared $itemType.$fieldName", async ({ itemType, item, fieldName }) => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const result = validateItem(item, itemType, 1);
      expect(result.isValid).toBe(true);
      expect(result.normalizedItem).not.toHaveProperty(fieldName);
    });

    it("should preserve enabled structured data", async () => {
      const { validateItem } = await import("./safe_output_type_validator.cjs");

      const item = {
        type: "create_issue",
        title: "Test",
        body: "Detailed issue body text.",
        data: { project: "test" },
      };

      const result = validateItem(item, "create_issue", 1, { dataEnabled: true });
      expect(result.isValid).toBe(true);
      expect(result.normalizedItem.data).toEqual({ project: "test" });
    });
  });
});
