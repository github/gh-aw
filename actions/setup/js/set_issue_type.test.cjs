import { describe, it, expect, beforeEach, vi } from "vitest";

const mockCore = {
  debug: vi.fn(),
  info: vi.fn(),
  warning: vi.fn(),
  error: vi.fn(),
  setFailed: vi.fn(),
  setOutput: vi.fn(),
  summary: {
    addRaw: vi.fn().mockReturnThis(),
    write: vi.fn().mockResolvedValue(),
  },
};

const mockContext = {
  repo: {
    owner: "test-owner",
    repo: "test-repo",
  },
  eventName: "issues",
  payload: {
    issue: {
      number: 123,
    },
  },
};

const mockGithub = {
  rest: {
    issues: {
      get: vi.fn(),
      update: vi.fn(),
    },
  },
  graphql: vi.fn(),
};

global.core = mockCore;
global.context = mockContext;
global.github = mockGithub;

describe("set_issue_type (Handler Factory Architecture)", () => {
  let handler;
  const createInvalidTypeError = (message = "Type must be one of: Bug, Feature") =>
    Object.assign(new Error("Validation failed"), {
      response: {
        status: 422,
        data: {
          errors: [{ message }],
        },
      },
    });

  beforeEach(async () => {
    vi.clearAllMocks();

    mockGithub.rest.issues.get.mockResolvedValue({ data: { labels: [], title: "Issue title", node_id: "I_kwDO_testissue" } });
    mockGithub.rest.issues.update.mockResolvedValue({ data: {} });
    mockGithub.graphql.mockImplementation(async query => {
      if (query.includes("repository(owner")) {
        return {
          repository: {
            issueTypes: {
              nodes: [
                { id: "IT_kwDO_bug", name: "Bug" },
                { id: "IT_kwDO_feature", name: "Feature" },
              ],
            },
          },
        };
      }
      if (query.includes("updateIssue")) {
        return { updateIssue: { issue: { id: "I_kwDO_testissue" } } };
      }
      return {};
    });

    const { main } = require("./set_issue_type.cjs");
    handler = await main({ max: 5, issue_intent: true, target: "*" });
  });

  it("should return a function from main()", async () => {
    const { main } = require("./set_issue_type.cjs");
    const result = await main({});
    expect(typeof result).toBe("function");
  });

  it("should set issue type successfully", async () => {
    const message = {
      type: "set_issue_type",
      issue_number: 42,
      issue_type: "Bug",
    };

    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.issue_number).toBe(42);
    expect(result.issue_type).toBe("Bug");
    expect(mockGithub.rest.issues.update).not.toHaveBeenCalled();
    expect(mockGithub.graphql).toHaveBeenCalledWith(
      expect.stringContaining("IssueTypeUpdateInput"),
      expect.objectContaining({
        issueId: "I_kwDO_testissue",
        issueType: {
          issueTypeId: "IT_kwDO_bug",
        },
      })
    );
  });

  it("should clear issue type when issue_type is empty string", async () => {
    const message = {
      type: "set_issue_type",
      issue_number: 42,
      issue_type: "",
    };

    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.issue_type).toBe("");
    expect(mockGithub.rest.issues.update).toHaveBeenCalledWith({
      owner: "test-owner",
      repo: "test-repo",
      issue_number: 42,
      type: "",
    });
  });

  it("should use context issue number when issue_number not provided", async () => {
    const { main } = require("./set_issue_type.cjs");
    handler = await main({ max: 5, issue_intent: true, target: "triggering" });
    const message = {
      type: "set_issue_type",
      issue_type: "Bug",
    };

    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.issue_number).toBe(123); // from context.payload.issue.number
    expect(mockGithub.rest.issues.update).not.toHaveBeenCalled();
    expect(mockGithub.graphql).toHaveBeenCalledWith(
      expect.stringContaining("IssueTypeUpdateInput"),
      expect.objectContaining({
        issueId: "I_kwDO_testissue",
        issueType: {
          issueTypeId: "IT_kwDO_bug",
        },
      })
    );
  });

  it("should validate against allowed types list", async () => {
    const { main } = require("./set_issue_type.cjs");
    const handlerWithAllowed = await main({
      max: 5,
      allowed: ["Bug", "Feature"],
      issue_intent: true,
    });

    const message = {
      type: "set_issue_type",
      issue_number: 42,
      issue_type: "Bug",
    };

    const result = await handlerWithAllowed(message, {});
    expect(result.success).toBe(true);
  });

  it("should reject type not in allowed list", async () => {
    const { main } = require("./set_issue_type.cjs");
    const handlerWithAllowed = await main({
      max: 5,
      allowed: ["Bug", "Feature"],
      issue_intent: true,
    });

    const message = {
      type: "set_issue_type",
      issue_number: 42,
      issue_type: "Task",
    };

    const result = await handlerWithAllowed(message, {});
    expect(result.success).toBe(false);
    expect(result.error).toContain("is not in the allowed list");
  });

  it("should allow clearing type even with allowed list configured", async () => {
    const { main } = require("./set_issue_type.cjs");
    const handlerWithAllowed = await main({
      max: 5,
      allowed: ["Bug", "Feature"],
      issue_intent: true,
    });

    const message = {
      type: "set_issue_type",
      issue_number: 42,
      issue_type: "",
    };

    const result = await handlerWithAllowed(message, {});
    expect(result.success).toBe(true);
  });

  it("should respect max count configuration", async () => {
    const { main } = require("./set_issue_type.cjs");
    const limitedHandler = await main({ max: 1 });

    const message1 = { type: "set_issue_type", issue_number: 1, issue_type: "Bug" };
    const message2 = { type: "set_issue_type", issue_number: 2, issue_type: "Feature" };

    const result1 = await limitedHandler(message1, {});
    expect(result1.success).toBe(true);

    const result2 = await limitedHandler(message2, {});
    expect(result2.success).toBe(false);
    expect(result2.error).toContain("Max count");
  });

  it("should handle API errors gracefully", async () => {
    mockGithub.graphql.mockRejectedValue(new Error("API error"));

    const message = {
      type: "set_issue_type",
      issue_number: 42,
      issue_type: "Bug",
    };

    const result = await handler(message, {});
    expect(result.success).toBe(false);
    expect(result.error).toContain("API error");
  });

  it("should map 422 invalid issue type errors to not-found shape", async () => {
    const invalidTypeError = createInvalidTypeError();
    mockGithub.graphql.mockRejectedValue(invalidTypeError);

    const result = await handler(
      {
        type: "set_issue_type",
        issue_number: 42,
        issue_type: "Bug",
      },
      {}
    );

    expect(result.success).toBe(false);
    expect(result.error).toBe('Issue type "Bug" not found. Available types: Bug, Feature');
  });

  it("should map 422 invalid issue type errors without available list to base not-found message", async () => {
    mockGithub.graphql.mockRejectedValue(createInvalidTypeError("Validation Failed"));

    const result = await handler(
      {
        type: "set_issue_type",
        issue_number: 42,
        issue_type: "Bug",
      },
      {}
    );

    expect(result.success).toBe(false);
    expect(result.error).toBe('Issue type "Bug" not found.');
  });

  it("should preserve no-issue-types-available error mapping", async () => {
    mockGithub.graphql.mockImplementation(async query => {
      if (query.includes("repository(owner")) {
        return { repository: { issueTypes: { nodes: [] } } };
      }
      return {};
    });

    const result = await handler(
      {
        type: "set_issue_type",
        issue_number: 42,
        issue_type: "NonExistentType",
      },
      {}
    );

    expect(result.success).toBe(false);
    expect(result.error).toBe("No issue types are available for this repository. Issue types must be configured in the repository or organization settings.");
  });

  it("should handle invalid issue numbers", async () => {
    const message = {
      type: "set_issue_type",
      issue_number: -1,
      issue_type: "Bug",
    };

    const result = await handler(message, {});
    expect(result.success).toBe(false);
    expect(result.error).toContain("Invalid item number");
  });

  it("should handle staged mode", async () => {
    process.env.GH_AW_SAFE_OUTPUTS_STAGED = "true";

    try {
      const { main } = require("./set_issue_type.cjs");
      const stagedHandler = await main({ max: 5, target: "*" });

      const message = {
        type: "set_issue_type",
        issue_number: 42,
        issue_type: "Bug",
      };

      const result = await stagedHandler(message, {});
      expect(result.success).toBe(true);
      expect(result.staged).toBe(true);
      expect(result.previewInfo.issue_number).toBe(42);
      expect(result.previewInfo.issue_type).toBe("Bug");
      // Should not call any API when staged
      expect(mockGithub.rest.issues.get).not.toHaveBeenCalled();
      expect(mockGithub.rest.issues.update).not.toHaveBeenCalled();
    } finally {
      delete process.env.GH_AW_SAFE_OUTPUTS_STAGED;
    }
  });

  it("should handle case-insensitive type matching", async () => {
    const { main } = require("./set_issue_type.cjs");
    const handlerWithAllowed = await main({
      max: 5,
      allowed: ["Bug", "Feature"],
      issue_intent: true,
    });

    const message = {
      type: "set_issue_type",
      issue_number: 42,
      issue_type: "bug", // lowercase
    };

    const result = await handlerWithAllowed(message, {});
    expect(result.success).toBe(true);
    expect(result.issue_type).toBe("Bug");
    expect(mockGithub.graphql).toHaveBeenCalledWith(
      expect.stringContaining("IssueTypeUpdateInput"),
      expect.objectContaining({
        issueType: {
          issueTypeId: "IT_kwDO_bug",
        },
      })
    );
  });

  it("should use GraphQL intent path with IssueTypeUpdateInput", async () => {
    const issueNodeId = "I_kwDO_testissue";
    const issueTypeNodeId = "IT_kwDO_bug";

    mockGithub.rest.issues.get.mockResolvedValueOnce({ data: { node_id: issueNodeId } });
    mockGithub.graphql.mockImplementation(async query => {
      if (query.includes("repository(owner")) {
        return {
          repository: {
            issueTypes: {
              nodes: [{ id: issueTypeNodeId, name: "Bug" }],
            },
          },
        };
      }
      if (query.includes("updateIssue")) {
        return { updateIssue: { issue: { id: issueNodeId } } };
      }
      return {};
    });

    const { main } = require("./set_issue_type.cjs");
    const featureHandler = await main({ max: 5, issue_intent: true });

    const result = await featureHandler(
      {
        type: "set_issue_type",
        issue_number: 42,
        issue_type: "Bug",
        rationale: "Author explicitly requests a bug fix",
        confidence: "high",
        suggest: true,
      },
      {}
    );

    expect(result.success).toBe(true);
    expect(mockGithub.rest.issues.update).not.toHaveBeenCalled();
    expect(mockGithub.graphql).toHaveBeenCalledWith(
      expect.stringContaining("IssueTypeUpdateInput"),
      expect.objectContaining({
        issueId: issueNodeId,
        issueType: {
          issueTypeId: issueTypeNodeId,
          rationale: "Author explicitly requests a bug fix",
          confidence: "HIGH",
          suggest: true,
        },
      })
    );
  });

  it("should truncate issue intent rationale to 280 characters", async () => {
    const issueNodeId = "I_kwDO_testissue";
    const issueTypeNodeId = "IT_kwDO_bug";
    const longRationale = "a".repeat(350);

    mockGithub.rest.issues.get.mockResolvedValueOnce({ data: { node_id: issueNodeId } });
    mockGithub.graphql.mockImplementation(async query => {
      if (query.includes("repository(owner")) {
        return {
          repository: {
            issueTypes: {
              nodes: [{ id: issueTypeNodeId, name: "Bug" }],
            },
          },
        };
      }
      if (query.includes("updateIssue")) {
        return { updateIssue: { issue: { id: issueNodeId } } };
      }
      return {};
    });

    const { main } = require("./set_issue_type.cjs");
    const featureHandler = await main({ max: 5, issue_intent: true });

    const result = await featureHandler(
      {
        type: "set_issue_type",
        issue_number: 42,
        issue_type: "Bug",
        rationale: longRationale,
      },
      {}
    );

    expect(result.success).toBe(true);
    const mutationCall = mockGithub.graphql.mock.calls.find(([query]) => typeof query === "string" && query.includes("IssueTypeUpdateInput"));
    expect(mutationCall).toBeDefined();
    expect(mutationCall[1].issueType.rationale).toBe("a".repeat(280));
  });

  it("should preserve issue_intents rationale of exactly 280 characters", () => {
    const { normalizeIssueIntentMetadata } = require("./issue_intents.cjs");

    const metadata = normalizeIssueIntentMetadata({ rationale: "a".repeat(280) });

    expect(metadata.rationale).toBe("a".repeat(280));
  });

  it("should truncate issue_intents rationale of 281 characters", () => {
    const { normalizeIssueIntentMetadata } = require("./issue_intents.cjs");

    const metadata = normalizeIssueIntentMetadata({ rationale: "a".repeat(281) });

    expect(metadata.rationale).toBe("a".repeat(280));
  });

  it("should omit empty issue_intents rationale after sanitization", () => {
    const { normalizeIssueIntentMetadata } = require("./issue_intents.cjs");

    const metadata = normalizeIssueIntentMetadata({ rationale: "   " });

    expect(metadata).not.toHaveProperty("rationale");
  });

  it("should use legacy REST issue type update when issue_intent is disabled", async () => {
    const { main } = require("./set_issue_type.cjs");
    const handlerWithoutIntent = await main({ max: 5, issue_intent: false, target: "*" });

    const result = await handlerWithoutIntent(
      {
        type: "set_issue_type",
        issue_number: 99,
        issue_type: "Bug",
        rationale: "should be ignored",
        confidence: "HIGH",
      },
      {}
    );

    expect(result.success).toBe(true);
    expect(mockGithub.rest.issues.update).toHaveBeenCalledWith({
      owner: "test-owner",
      repo: "test-repo",
      issue_number: 99,
      type: "Bug",
    });
    expect(mockGithub.graphql).not.toHaveBeenCalledWith(expect.stringContaining("updateIssue"), expect.anything());
  });

  it("should use GraphQL intent path by default when issue_intent is omitted", async () => {
    const { main } = require("./set_issue_type.cjs");
    const defaultHandler = await main({ max: 5 });

    const result = await defaultHandler(
      {
        type: "set_issue_type",
        issue_number: 42,
        issue_type: "Bug",
      },
      {}
    );

    expect(result.success).toBe(true);
    expect(mockGithub.rest.issues.update).not.toHaveBeenCalled();
    expect(mockGithub.graphql).toHaveBeenCalledWith(
      expect.stringContaining("IssueTypeUpdateInput"),
      expect.objectContaining({
        issueId: "I_kwDO_testissue",
        issueType: expect.objectContaining({ issueTypeId: "IT_kwDO_bug" }),
      })
    );
  });

  it("should forward intent metadata by default when issue_intent is omitted", async () => {
    const { main } = require("./set_issue_type.cjs");
    const defaultHandler = await main({ max: 5 });

    const result = await defaultHandler(
      {
        type: "set_issue_type",
        issue_number: 42,
        issue_type: "Bug",
        rationale: "Author explicitly requests a bug fix",
        confidence: "high",
      },
      {}
    );

    expect(result.success).toBe(true);
    expect(mockGithub.rest.issues.update).not.toHaveBeenCalled();
    expect(mockGithub.graphql).toHaveBeenCalledWith(
      expect.stringContaining("IssueTypeUpdateInput"),
      expect.objectContaining({
        issueId: "I_kwDO_testissue",
        issueType: {
          issueTypeId: "IT_kwDO_bug",
          rationale: "Author explicitly requests a bug fix",
          confidence: "HIGH",
        },
      })
    );
  });
});
