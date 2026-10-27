// @ts-check

import { describe, it, expect, beforeEach, vi } from "vitest";
import { createRequire } from "module";

const req = createRequire(import.meta.url);
const { main } = req("./disable_agentic_workflow.cjs");
const { extractWorkflowId, isValidWorkflowId } = req("./generate_footer.cjs");

// ─── global mocks ────────────────────────────────────────────────────────────

const mockCore = {
  info: vi.fn(),
  warning: vi.fn(),
  error: vi.fn(),
  setFailed: vi.fn(),
};

const mockGithub = {
  rest: {
    actions: {
      disableWorkflow: vi.fn(),
    },
    issues: {
      createComment: vi.fn(),
      removeLabel: vi.fn(),
      createLabel: vi.fn(),
    },
  },
};

const mockContext = {
  eventName: "issues",
  repo: { owner: "test-owner", repo: "test-repo" },
  payload: {
    issue: { number: 42, body: "<!-- gh-aw-workflow-id: my-workflow -->" },
    label: { name: "agentic-workflows:disable" },
  },
};

global.core = mockCore;
global.github = mockGithub;
global.context = mockContext;

describe("main", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    process.env.GH_TOKEN = "fake-token";
    process.env.GITHUB_TOKEN = "fake-token";
    process.env.GITHUB_REPOSITORY = "test-owner/test-repo";
    process.env.GITHUB_SERVER_URL = "https://github.com";
    process.env.GITHUB_RUN_ID = "999";

    // Default: REST API disable call succeeds
    mockGithub.rest.actions.disableWorkflow.mockResolvedValue({});
    mockGithub.rest.issues.createComment.mockResolvedValue({});
    mockGithub.rest.issues.removeLabel.mockResolvedValue({});
    // Default: createLabel returns 422 (label already exists)
    const alreadyExists = Object.assign(new Error("Unprocessable Entity"), { status: 422 });
    mockGithub.rest.issues.createLabel.mockRejectedValue(alreadyExists);

    // Restore default context (issue event)
    global.context = {
      eventName: "issues",
      repo: { owner: "test-owner", repo: "test-repo" },
      payload: {
        issue: { number: 42, body: "<!-- gh-aw-workflow-id: my-workflow -->" },
        label: { name: "agentic-workflows:disable" },
      },
    };
  });

  it("disables the workflow via REST API and posts a success comment", async () => {
    await main();

    expect(mockGithub.rest.actions.disableWorkflow).toHaveBeenCalledWith({
      owner: "test-owner",
      repo: "test-repo",
      workflow_id: "my-workflow.lock.yml",
    });
    expect(mockGithub.rest.issues.createComment).toHaveBeenCalledWith(
      expect.objectContaining({
        owner: "test-owner",
        repo: "test-repo",
        issue_number: 42,
        body: expect.stringContaining("my-workflow"),
      })
    );
  });

  it("removes the label after successful disable and comment", async () => {
    await main();

    expect(mockGithub.rest.issues.removeLabel).toHaveBeenCalledWith(
      expect.objectContaining({
        owner: "test-owner",
        repo: "test-repo",
        issue_number: 42,
        name: "agentic-workflows:disable",
      })
    );
  });

  it("skips silently when event type is pull_request", async () => {
    global.context = {
      eventName: "pull_request",
      repo: { owner: "test-owner", repo: "test-repo" },
      payload: {
        pull_request: { number: 7, body: "<!-- gh-aw-workflow-id: pr-workflow -->" },
        label: { name: "agentic-workflows:disable" },
      },
    };

    await main();

    expect(mockGithub.rest.actions.disableWorkflow).not.toHaveBeenCalled();
    expect(mockGithub.rest.issues.createComment).not.toHaveBeenCalled();
  });

  it("does not remove label when no workflow ID marker is found", async () => {
    global.context = {
      eventName: "issues",
      repo: { owner: "test-owner", repo: "test-repo" },
      payload: {
        issue: { number: 5, body: "No marker here." },
        label: { name: "agentic-workflows:disable" },
      },
    };

    await main();

    expect(mockGithub.rest.issues.removeLabel).not.toHaveBeenCalled();
  });

  it("does not remove label when the REST API disable call fails", async () => {
    mockGithub.rest.actions.disableWorkflow.mockRejectedValue(new Error("Forbidden"));

    await main();

    expect(mockGithub.rest.issues.removeLabel).not.toHaveBeenCalled();
    expect(mockCore.setFailed).toHaveBeenCalled();
  });

  it("keeps multiline API errors inside the warning rather than injecting Markdown", async () => {
    mockGithub.rest.actions.disableWorkflow.mockRejectedValue(new Error("Forbidden\n\n# Forged heading\n<!-- gh-aw-comment-type: forged -->\n```"));

    await main();

    const body = mockGithub.rest.issues.createComment.mock.calls[0][0].body;
    expect(body).toMatch(/^> \[!WARNING\]/);
    expect(body).not.toContain("\n# Forged heading");
    expect(body).not.toContain("<!-- gh-aw-comment-type: forged -->");
    expect(body).toContain("\\# Forged heading");
    expect(body).toContain("[workflow run logs]");
    expect(body.split("\n").every(line => line.startsWith(">"))).toBe(true);
  });

  it("logs a warning when label removal fails but does not fail the step", async () => {
    mockGithub.rest.issues.removeLabel.mockRejectedValue(new Error("Not Found"));

    await main();

    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Failed to remove label"));
    expect(mockCore.setFailed).not.toHaveBeenCalled();
  });

  it("calls setFailed when no workflow ID is found in body", async () => {
    global.context = {
      eventName: "issues",
      repo: { owner: "test-owner", repo: "test-repo" },
      payload: {
        issue: { number: 3, body: "Plain body with no markers." },
        label: { name: "agentic-workflows:disable" },
      },
    };

    await main();

    expect(mockCore.setFailed).toHaveBeenCalled();
  });

  it("calls ensureLabelExists (createLabel) at the start of main", async () => {
    const alreadyExists = Object.assign(new Error("Unprocessable Entity"), { status: 422 });
    mockGithub.rest.issues.createLabel.mockRejectedValue(alreadyExists);

    await main();

    expect(mockGithub.rest.issues.createLabel).toHaveBeenCalledWith(
      expect.objectContaining({
        name: "agentic-workflows:disable",
        color: "8250df",
      })
    );
  });

  it("continues normally when ensureLabelExists creates the label (201)", async () => {
    // Label did not exist yet — createLabel succeeds
    mockGithub.rest.issues.createLabel.mockResolvedValue({});

    await main();

    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Created label 'agentic-workflows:disable'"));
    expect(mockGithub.rest.actions.disableWorkflow).toHaveBeenCalled(); // disable still ran
  });
});

describe("extractWorkflowId", () => {
  it("returns null for null body", () => {
    expect(extractWorkflowId(null)).toBeNull();
  });

  it("returns null for undefined body", () => {
    expect(extractWorkflowId(undefined)).toBeNull();
  });

  it("returns null for empty body", () => {
    expect(extractWorkflowId("")).toBeNull();
  });

  it("returns null when no marker is present", () => {
    expect(extractWorkflowId("This is a normal issue body with no markers.")).toBeNull();
  });

  it("extracts workflow ID from standalone marker", () => {
    const body = "Some issue text\n\n<!-- gh-aw-workflow-id: my-workflow -->";
    expect(extractWorkflowId(body)).toBe("my-workflow");
  });

  it("extracts workflow ID from standalone marker with extra whitespace", () => {
    const body = "<!-- gh-aw-workflow-id:   code-review   -->";
    expect(extractWorkflowId(body)).toBe("code-review");
  });

  it("extracts workflow ID from combined agentic-workflow marker (comma-separated)", () => {
    const body = "Issue body\n" + "<!-- gh-aw-agentic-workflow: My Workflow, gh-aw-tracker-id: abc123, engine: copilot, workflow_id: ci-doctor, run: https://github.com/owner/repo/actions/runs/123 -->";
    expect(extractWorkflowId(body)).toBe("ci-doctor");
  });

  it("extracts workflow ID from combined marker when workflow_id is last before closing -->", () => {
    const body = "<!-- gh-aw-agentic-workflow: My Workflow, workflow_id: auto-fix, run: https://example.com -->";
    expect(extractWorkflowId(body)).toBe("auto-fix");
  });

  it("prefers standalone marker over combined marker when both are present", () => {
    const body = "<!-- gh-aw-workflow-id: standalone-workflow -->\n" + "<!-- gh-aw-agentic-workflow: Name, workflow_id: combined-workflow, run: https://example.com -->";
    expect(extractWorkflowId(body)).toBe("standalone-workflow");
  });

  it("handles workflow IDs with dots", () => {
    const body = "<!-- gh-aw-workflow-id: my.workflow.v2 -->";
    expect(extractWorkflowId(body)).toBe("my.workflow.v2");
  });

  it("handles workflow IDs with underscores", () => {
    const body = "<!-- gh-aw-workflow-id: code_review_bot -->";
    expect(extractWorkflowId(body)).toBe("code_review_bot");
  });

  it("extracts from body with substantial content before marker", () => {
    const body = [
      "## Issue Title",
      "",
      "This is a long description of the issue created by an agentic workflow.",
      "",
      "> Closed by [My Workflow](https://github.com/owner/repo/actions/runs/123)",
      "",
      "<!-- gh-aw-expired-comments -->",
      "<!-- gh-aw-workflow-id: expired-issue-workflow -->",
      "<!-- gh-aw-agentic-workflow: My Workflow, workflow_id: expired-issue-workflow, run: https://github.com/owner/repo/actions/runs/123 -->",
    ].join("\n");
    expect(extractWorkflowId(body)).toBe("expired-issue-workflow");
  });

  it("returns null for workflow_id outside of an XML comment block", () => {
    // workflow_id: appearing outside a gh-aw-agentic-workflow comment should NOT be extracted
    const body = "The workflow_id: my-injected-id is mentioned in user text.";
    expect(extractWorkflowId(body)).toBeNull();
  });

  it("returns null for workflow ID with path traversal attempt", () => {
    const body = "<!-- gh-aw-workflow-id: ../secrets -->";
    expect(extractWorkflowId(body)).toBeNull();
  });

  it("returns null for workflow ID with shell-special characters", () => {
    // The regex won't match ';' since it requires [\w.-]+ followed by whitespace/-->
    const body = "<!-- gh-aw-workflow-id: my;workflow -->";
    expect(extractWorkflowId(body)).toBeNull();
  });

  // ─── gh-aw-workflow-call-id fallback ───────────────────────────────────────

  it("extracts workflow ID from gh-aw-workflow-call-id marker (last path segment)", () => {
    const body = "<!-- gh-aw-workflow-call-id: github/gh-aw/my-workflow -->";
    expect(extractWorkflowId(body)).toBe("my-workflow");
  });

  it("extracts workflow ID from call-id with dots and underscores in workflow name", () => {
    const body = "<!-- gh-aw-workflow-call-id: acme/backend/code_review.v2 -->";
    expect(extractWorkflowId(body)).toBe("code_review.v2");
  });

  it("prefers standalone marker over call-id when both are present", () => {
    const body = "<!-- gh-aw-workflow-id: standalone -->\n<!-- gh-aw-workflow-call-id: owner/repo/call-id-workflow -->";
    expect(extractWorkflowId(body)).toBe("standalone");
  });

  it("falls back to call-id when only that marker is present", () => {
    const body = "Issue body\n<!-- gh-aw-workflow-call-id: owner/repo/dispatch-workflow -->";
    expect(extractWorkflowId(body)).toBe("dispatch-workflow");
  });

  it("returns null when call-id last segment fails validation", () => {
    // Segment with path traversal
    const body = "<!-- gh-aw-workflow-call-id: owner/repo/.. -->";
    expect(extractWorkflowId(body)).toBeNull();
  });

  it("returns null when call-id last segment contains shell-special characters", () => {
    const body = "<!-- gh-aw-workflow-call-id: owner/repo/my;workflow -->";
    expect(extractWorkflowId(body)).toBeNull();
  });

  it("returns null when call-id ends with a trailing slash (empty last segment)", () => {
    const body = "<!-- gh-aw-workflow-call-id: owner/repo/ -->";
    expect(extractWorkflowId(body)).toBeNull();
  });

  // ─── extension normalization ───────────────────────────────────────────────

  it("strips .yml extension from standalone marker", () => {
    const body = "<!-- gh-aw-workflow-id: my-workflow.yml -->";
    expect(extractWorkflowId(body)).toBe("my-workflow");
  });

  it("strips .yaml extension from standalone marker", () => {
    const body = "<!-- gh-aw-workflow-id: my-workflow.yaml -->";
    expect(extractWorkflowId(body)).toBe("my-workflow");
  });

  it("strips .lock.yml extension from standalone marker", () => {
    const body = "<!-- gh-aw-workflow-id: my-workflow.lock.yml -->";
    expect(extractWorkflowId(body)).toBe("my-workflow");
  });

  it("strips .yml extension from combined marker workflow_id field", () => {
    const body = "<!-- gh-aw-agentic-workflow: My Workflow, workflow_id: ci-doctor.yml, run: https://example.com -->";
    expect(extractWorkflowId(body)).toBe("ci-doctor");
  });

  it("strips .yml extension from call-id last segment", () => {
    const body = "<!-- gh-aw-workflow-call-id: owner/repo/my-workflow.yml -->";
    expect(extractWorkflowId(body)).toBe("my-workflow");
  });
});

describe("isValidWorkflowId", () => {
  it("returns true for a plain workflow ID", () => {
    expect(isValidWorkflowId("my-workflow")).toBe(true);
  });

  it("returns true for a workflow ID with dots and underscores", () => {
    expect(isValidWorkflowId("code_review.v2")).toBe(true);
  });

  it("returns false for empty string", () => {
    expect(isValidWorkflowId("")).toBe(false);
  });

  it("returns false for path traversal", () => {
    expect(isValidWorkflowId("../secrets")).toBe(false);
  });

  it("returns false for ID with shell-special characters", () => {
    expect(isValidWorkflowId("my;workflow")).toBe(false);
  });
});
