import { describe, it, expect, beforeEach, vi } from "vitest";

const mockCore = {
  debug: vi.fn(),
  info: vi.fn(),
  warning: vi.fn(),
  error: vi.fn(),
  setOutput: vi.fn(),
  summary: { addRaw: vi.fn().mockReturnThis(), write: vi.fn().mockResolvedValue() },
};

const mockGithub = {
  rest: {
    issues: {
      create: vi.fn(),
      createComment: vi.fn(),
    },
  },
  graphql: vi.fn(),
};

const mockContext = {
  runId: 12345,
  repo: { owner: "testowner", repo: "testrepo" },
  payload: {
    repository: { html_url: "https://github.com/testowner/testrepo" },
  },
};

global.core = mockCore;
global.github = mockGithub;
global.context = mockContext;

describe("create_issue.cjs (New Handler Factory Architecture)", () => {
  let handler;

  beforeEach(async () => {
    vi.clearAllMocks();

    // Load the module and create handler
    const { main } = require("./create_issue.cjs");
    handler = await main({
      max: 10,
      labels: ["automation"],
      title_prefix: "[AUTO] ",
    });
  });

  it("should return a function from main()", async () => {
    const { main } = require("./create_issue.cjs");
    const result = await main({});
    expect(typeof result).toBe("function");
  });

  it("should create an issue with title and body", async () => {
    const mockIssue = { number: 123, html_url: "https://github.com/testowner/testrepo/issues/123", node_id: "I_123" };
    mockGithub.rest.issues.create.mockResolvedValue({ data: mockIssue });

    const message = {
      type: "create_issue",
      title: "Test Issue",
      body: "This is a test issue",
    };

    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.number).toBe(123);
    expect(result.repo).toBe("testowner/testrepo");
    expect(result.temporaryId).toBeTruthy();
    expect(mockGithub.rest.issues.create).toHaveBeenCalledWith({
      owner: "testowner",
      repo: "testrepo",
      title: "[AUTO] Test Issue",
      body: expect.stringContaining("This is a test issue"),
      labels: ["automation"],
      assignees: [],
    });
  });

  it("should respect max count limit", async () => {
    const { main } = require("./create_issue.cjs");
    const limitedHandler = await main({ max: 1 });

    const mockIssue = { number: 123, html_url: "https://github.com/testowner/testrepo/issues/123", node_id: "I_123" };
    mockGithub.rest.issues.create.mockResolvedValue({ data: mockIssue });

    const message = { type: "create_issue", title: "Test", body: "Test" };

    // First call should succeed
    const result1 = await limitedHandler(message, {});
    expect(result1.success).toBe(true);

    // Second call should fail
    const result2 = await limitedHandler(message, {});
    expect(result2.success).toBe(false);
    expect(result2.error.toLowerCase()).toContain("max count");
  });

  it("should apply labels from config and message", async () => {
    const mockIssue = { number: 123, html_url: "https://github.com/testowner/testrepo/issues/123", node_id: "I_123" };
    mockGithub.rest.issues.create.mockResolvedValue({ data: mockIssue });

    const message = {
      type: "create_issue",
      title: "Test",
      body: "Test",
      labels: ["bug", "enhancement"],
    };

    await handler(message, {});

    const callArgs = mockGithub.rest.issues.create.mock.calls[0][0];
    expect(callArgs.labels).toEqual(expect.arrayContaining(["automation", "bug", "enhancement"]));
  });

  it("should generate and return temporary ID", async () => {
    const mockIssue = { number: 123, html_url: "https://github.com/testowner/testrepo/issues/123", node_id: "I_123" };
    mockGithub.rest.issues.create.mockResolvedValue({ data: mockIssue });

    const message = { type: "create_issue", title: "Test", body: "Test" };

    const result = await handler(message, {});

    expect(result.temporaryId).toBeTruthy();
    expect(result.temporaryId).toMatch(/^aw_[A-Za-z0-9]{4,8}$/i);
  });

  it("should use provided temporary ID if available", async () => {
    const mockIssue = { number: 123, html_url: "https://github.com/testowner/testrepo/issues/123", node_id: "I_123" };
    mockGithub.rest.issues.create.mockResolvedValue({ data: mockIssue });

    const message = {
      type: "create_issue",
      title: "Test",
      body: "Test",
      temporary_id: "aw_aabbcc",
    };

    const result = await handler(message, {});

    expect(result.temporaryId).toBe("aw_aabbcc");
  });

  it("should resolve parent temporary ID from resolvedTemporaryIds", async () => {
    const mockIssue = { number: 123, html_url: "https://github.com/testowner/testrepo/issues/123", node_id: "I_123" };
    mockGithub.rest.issues.create.mockResolvedValue({ data: mockIssue });

    const message = {
      type: "create_issue",
      title: "Child Issue",
      body: "This is a child issue",
      parent: "aw_aabbcc",
    };

    const resolvedIds = {
      aw_aabbcc: { repo: "testowner/testrepo", number: 100 },
    };

    await handler(message, resolvedIds);

    const callArgs = mockGithub.rest.issues.create.mock.calls[0][0];
    expect(callArgs.body).toContain("Related to #100");
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Resolved parent temporary ID 'aw_aabbcc' to testowner/testrepo#100"));
  });

  it("should handle disabled issues repository gracefully", async () => {
    mockGithub.rest.issues.create.mockRejectedValue(new Error("Issues has been disabled in this repository"));

    const message = { type: "create_issue", title: "Test", body: "Test" };

    const result = await handler(message, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("Issues disabled");
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Issues are disabled for this repository"));
  });

  it("should validate repository format", async () => {
    const message = {
      type: "create_issue",
      title: "Test",
      body: "Test",
      repo: "invalid-repo-format",
    };

    const result = await handler(message, {});

    expect(result.success).toBe(false);
    // The error could be about format or allowed repos depending on validation order
    expect(result.error).toMatch(/Invalid repository format|not in the allowed-repos list/);
  });
});
