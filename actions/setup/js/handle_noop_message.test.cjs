// @ts-check

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import fs from "fs";
import path from "path";
import os from "os";

describe("handle_noop_message", () => {
  let mockCore;
  let mockGithub;
  let mockContext;
  let originalEnv;
  let tempDir;
  let originalReadFileSync;

  beforeEach(async () => {
    // Save original environment
    originalEnv = { ...process.env };

    // Create temp directory for test files
    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "handle-noop-test-"));

    // Mock fs.readFileSync to return template content
    originalReadFileSync = fs.readFileSync;
    fs.readFileSync = vi.fn((filePath, encoding) => {
      if (filePath.includes("noop_runs_issue.md")) {
        return `This issue tracks all no-op runs from agentic workflows in this repository. Each workflow run that completes with a no-op message (indicating no action was needed) posts a comment here.

<details>
<summary><b>📘 What is a No-Op?</b></summary>

A no-op (no operation) occurs when an agentic workflow runs successfully but determines that no action is required. For example:
- A security scanner that finds no issues
- An update checker that finds nothing to update
- A monitoring workflow that finds everything is healthy

These are successful outcomes, not failures, and help provide transparency into workflow behavior.

</details>

<details>
<summary><b>🎯 How This Helps</b></summary>

This issue helps you:
- Track workflows that ran but determined no action was needed
- Distinguish between failures and intentional no-ops
- Monitor workflow health by seeing when workflows decide not to act

</details>

<details>
<summary><b>📚 Resources</b></summary>

- [GitHub Agentic Workflows Documentation](https://github.com/github/gh-aw)

</details>

---

> This issue is automatically managed by GitHub Agentic Workflows. Do not close this issue manually.
> 
> **No action to take** - Do not assign to an agent.

<!-- gh-aw-noop-runs -->`;
      }
      if (filePath.includes("noop_comment.md")) {
        return `### {workflow_name}

{message}

> Generated from [{workflow_name}]({run_url})`;
      }
      return originalReadFileSync.call(fs, filePath, encoding);
    });

    // Mock core
    mockCore = {
      info: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
    };

    // Mock GitHub API
    mockGithub = {
      rest: {
        search: {
          issuesAndPullRequests: vi.fn(),
        },
        issues: {
          create: vi.fn(),
          createComment: vi.fn(),
        },
      },
    };

    // Mock context
    mockContext = {
      repo: {
        owner: "test-owner",
        repo: "test-repo",
      },
    };

    // Setup globals
    global.core = mockCore;
    global.github = mockGithub;
    global.context = mockContext;
  });

  afterEach(() => {
    // Restore environment
    process.env = originalEnv;

    // Restore fs.readFileSync
    if (originalReadFileSync) {
      fs.readFileSync = originalReadFileSync;
    }

    // Clean up temp directory
    if (tempDir && fs.existsSync(tempDir)) {
      fs.rmSync(tempDir, { recursive: true, force: true });
    }

    vi.clearAllMocks();
  });

  it("should skip if no noop message is present", async () => {
    process.env.GH_AW_WORKFLOW_NAME = "Test Workflow";
    process.env.GH_AW_RUN_URL = "https://github.com/test-owner/test-repo/actions/runs/123";
    process.env.GH_AW_NOOP_MESSAGE = "";
    process.env.GH_AW_AGENT_CONCLUSION = "success";

    const { main } = await import("./handle_noop_message.cjs?t=" + Date.now());
    await main();

    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("No no-op message found, skipping"));
    expect(mockGithub.rest.search.issuesAndPullRequests).not.toHaveBeenCalled();
  });

  it("should skip if report-as-issue is set to false", async () => {
    process.env.GH_AW_WORKFLOW_NAME = "Test Workflow";
    process.env.GH_AW_RUN_URL = "https://github.com/test-owner/test-repo/actions/runs/123";
    process.env.GH_AW_NOOP_MESSAGE = "Some message";
    process.env.GH_AW_AGENT_CONCLUSION = "success";
    process.env.GH_AW_NOOP_REPORT_AS_ISSUE = "false";

    // Create agent output file with only noop outputs
    const outputFile = path.join(tempDir, "agent_output.json");
    fs.writeFileSync(
      outputFile,
      JSON.stringify({
        items: [{ type: "noop", message: "Some message" }],
      })
    );
    process.env.GH_AW_AGENT_OUTPUT = outputFile;

    const { main } = await import("./handle_noop_message.cjs?t=" + Date.now());
    await main();

    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("report-as-issue is disabled"));
    expect(mockGithub.rest.search.issuesAndPullRequests).not.toHaveBeenCalled();
  });

  it("should proceed if report-as-issue is set to true", async () => {
    process.env.GH_AW_WORKFLOW_NAME = "Test Workflow";
    process.env.GH_AW_RUN_URL = "https://github.com/test-owner/test-repo/actions/runs/123";
    process.env.GH_AW_NOOP_MESSAGE = "Some message";
    process.env.GH_AW_AGENT_CONCLUSION = "success";
    process.env.GH_AW_NOOP_REPORT_AS_ISSUE = "true";

    // Create agent output file with only noop outputs
    const outputFile = path.join(tempDir, "agent_output.json");
    fs.writeFileSync(
      outputFile,
      JSON.stringify({
        items: [{ type: "noop", message: "Some message" }],
      })
    );
    process.env.GH_AW_AGENT_OUTPUT = outputFile;

    // Mock search to return existing issue
    mockGithub.rest.search.issuesAndPullRequests.mockResolvedValue({
      data: {
        total_count: 1,
        items: [
          {
            number: 42,
            node_id: "MDU6SXNzdWU0Mg==",
            html_url: "https://github.com/test-owner/test-repo/issues/42",
          },
        ],
      },
    });

    // Mock comment creation
    mockGithub.rest.issues.createComment.mockResolvedValue({
      data: {
        id: 1,
        html_url: "https://github.com/test-owner/test-repo/issues/42#issuecomment-1",
      },
    });

    const { main } = await import("./handle_noop_message.cjs?t=" + Date.now());
    await main();

    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Report as issue: true"));
    expect(mockGithub.rest.search.issuesAndPullRequests).toHaveBeenCalled();
    expect(mockGithub.rest.issues.createComment).toHaveBeenCalled();
  });

  it("should default to true if report-as-issue is not set", async () => {
    process.env.GH_AW_WORKFLOW_NAME = "Test Workflow";
    process.env.GH_AW_RUN_URL = "https://github.com/test-owner/test-repo/actions/runs/123";
    process.env.GH_AW_NOOP_MESSAGE = "Some message";
    process.env.GH_AW_AGENT_CONCLUSION = "success";
    // Don't set GH_AW_NOOP_REPORT_AS_ISSUE at all

    // Create agent output file with only noop outputs
    const outputFile = path.join(tempDir, "agent_output.json");
    fs.writeFileSync(
      outputFile,
      JSON.stringify({
        items: [{ type: "noop", message: "Some message" }],
      })
    );
    process.env.GH_AW_AGENT_OUTPUT = outputFile;

    // Mock search to return existing issue
    mockGithub.rest.search.issuesAndPullRequests.mockResolvedValue({
      data: {
        total_count: 1,
        items: [
          {
            number: 42,
            node_id: "MDU6SXNzdWU0Mg==",
            html_url: "https://github.com/test-owner/test-repo/issues/42",
          },
        ],
      },
    });

    // Mock comment creation
    mockGithub.rest.issues.createComment.mockResolvedValue({
      data: {
        id: 1,
        html_url: "https://github.com/test-owner/test-repo/issues/42#issuecomment-1",
      },
    });

    const { main } = await import("./handle_noop_message.cjs?t=" + Date.now());
    await main();

    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Report as issue: true"));
    expect(mockGithub.rest.search.issuesAndPullRequests).toHaveBeenCalled();
    expect(mockGithub.rest.issues.createComment).toHaveBeenCalled();
  });

  it("should skip if agent did not succeed", async () => {
    process.env.GH_AW_WORKFLOW_NAME = "Test Workflow";
    process.env.GH_AW_RUN_URL = "https://github.com/test-owner/test-repo/actions/runs/123";
    process.env.GH_AW_NOOP_MESSAGE = "Some message";
    process.env.GH_AW_AGENT_CONCLUSION = "failure";

    const { main } = await import("./handle_noop_message.cjs?t=" + Date.now());
    await main();

    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Agent did not succeed"));
    expect(mockGithub.rest.search.issuesAndPullRequests).not.toHaveBeenCalled();
  });

  it("should skip if there are non-noop outputs", async () => {
    process.env.GH_AW_WORKFLOW_NAME = "Test Workflow";
    process.env.GH_AW_RUN_URL = "https://github.com/test-owner/test-repo/actions/runs/123";
    process.env.GH_AW_NOOP_MESSAGE = "Some message";
    process.env.GH_AW_AGENT_CONCLUSION = "success";

    // Create agent output file with noop + other outputs
    const outputFile = path.join(tempDir, "agent_output.json");
    fs.writeFileSync(
      outputFile,
      JSON.stringify({
        items: [
          { type: "noop", message: "No action needed" },
          { type: "create_issue", title: "Some issue" },
        ],
      })
    );
    process.env.GH_AW_AGENT_OUTPUT = outputFile;

    const { main } = await import("./handle_noop_message.cjs?t=" + Date.now());
    await main();

    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Found 1 non-noop output(s)"));
    expect(mockGithub.rest.search.issuesAndPullRequests).not.toHaveBeenCalled();
  });

  it("should create no-op runs issue if it doesn't exist", async () => {
    process.env.GH_AW_WORKFLOW_NAME = "Test Workflow";
    process.env.GH_AW_RUN_URL = "https://github.com/test-owner/test-repo/actions/runs/123456";
    process.env.GH_AW_NOOP_MESSAGE = "No updates needed";
    process.env.GH_AW_AGENT_CONCLUSION = "success";

    // Create agent output file with only noop outputs
    const outputFile = path.join(tempDir, "agent_output.json");
    fs.writeFileSync(
      outputFile,
      JSON.stringify({
        items: [{ type: "noop", message: "No updates needed" }],
      })
    );
    process.env.GH_AW_AGENT_OUTPUT = outputFile;

    // Mock search to return no results
    mockGithub.rest.search.issuesAndPullRequests.mockResolvedValue({
      data: {
        total_count: 0,
        items: [],
      },
    });

    // Mock issue creation
    mockGithub.rest.issues.create.mockResolvedValue({
      data: {
        number: 42,
        node_id: "MDU6SXNzdWU0Mg==",
        html_url: "https://github.com/test-owner/test-repo/issues/42",
      },
    });

    // Mock comment creation
    mockGithub.rest.issues.createComment.mockResolvedValue({
      data: {
        id: 1,
        html_url: "https://github.com/test-owner/test-repo/issues/42#issuecomment-1",
      },
    });

    const { main } = await import("./handle_noop_message.cjs?t=" + Date.now());
    await main();

    // Verify search was performed
    expect(mockGithub.rest.search.issuesAndPullRequests).toHaveBeenCalledWith({
      q: expect.stringContaining("[agentics] No-Op Runs"),
      per_page: 1,
    });

    // Verify issue was created with correct title
    const createCall = mockGithub.rest.issues.create.mock.calls[0][0];
    expect(createCall.title).toBe("[agentics] No-Op Runs");
    expect(createCall.labels).toContain("agentic-workflows");
    expect(createCall.body).toContain("tracks all no-op runs");

    // Verify comment was posted
    const commentCall = mockGithub.rest.issues.createComment.mock.calls[0][0];
    expect(commentCall.issue_number).toBe(42);
    expect(commentCall.body).toContain("Test Workflow");
    expect(commentCall.body).toContain("No updates needed");
    // The new format doesn't have a separate Run ID line, but the URL is still in the footer
    expect(commentCall.body).toContain("https://github.com/test-owner/test-repo/actions/runs/123456");
  });

  it("should use existing no-op runs issue if it exists", async () => {
    process.env.GH_AW_WORKFLOW_NAME = "Another Workflow";
    process.env.GH_AW_RUN_URL = "https://github.com/test-owner/test-repo/actions/runs/789";
    process.env.GH_AW_NOOP_MESSAGE = "Everything is up to date";
    process.env.GH_AW_AGENT_CONCLUSION = "success";

    // Create agent output file with only noop outputs
    const outputFile = path.join(tempDir, "agent_output.json");
    fs.writeFileSync(
      outputFile,
      JSON.stringify({
        items: [{ type: "noop", message: "Everything is up to date" }],
      })
    );
    process.env.GH_AW_AGENT_OUTPUT = outputFile;

    // Mock search to return existing issue
    mockGithub.rest.search.issuesAndPullRequests.mockResolvedValue({
      data: {
        total_count: 1,
        items: [
          {
            number: 99,
            node_id: "MDU6SXNzdWU5OQ==",
            html_url: "https://github.com/test-owner/test-repo/issues/99",
          },
        ],
      },
    });

    // Mock comment creation
    mockGithub.rest.issues.createComment.mockResolvedValue({
      data: {
        id: 2,
        html_url: "https://github.com/test-owner/test-repo/issues/99#issuecomment-2",
      },
    });

    const { main } = await import("./handle_noop_message.cjs?t=" + Date.now());
    await main();

    // Verify issue was not created
    expect(mockGithub.rest.issues.create).not.toHaveBeenCalled();

    // Verify comment was posted to existing issue
    const commentCall = mockGithub.rest.issues.createComment.mock.calls[0][0];
    expect(commentCall.issue_number).toBe(99);
    expect(commentCall.body).toContain("Another Workflow");
    expect(commentCall.body).toContain("Everything is up to date");
  });

  it("should handle comment creation failure gracefully", async () => {
    process.env.GH_AW_WORKFLOW_NAME = "Test Workflow";
    process.env.GH_AW_RUN_URL = "https://github.com/test-owner/test-repo/actions/runs/456";
    process.env.GH_AW_NOOP_MESSAGE = "No action required";
    process.env.GH_AW_AGENT_CONCLUSION = "success";

    // Create agent output file with only noop outputs
    const outputFile = path.join(tempDir, "agent_output.json");
    fs.writeFileSync(
      outputFile,
      JSON.stringify({
        items: [{ type: "noop", message: "No action required" }],
      })
    );
    process.env.GH_AW_AGENT_OUTPUT = outputFile;

    // Mock existing issue
    mockGithub.rest.search.issuesAndPullRequests.mockResolvedValue({
      data: {
        total_count: 1,
        items: [{ number: 10, node_id: "MDU6SXNzdWUxMA==", html_url: "https://github.com/test-owner/test-repo/issues/10" }],
      },
    });

    // Mock comment creation failure
    mockGithub.rest.issues.createComment.mockRejectedValue(new Error("API rate limit exceeded"));

    const { main } = await import("./handle_noop_message.cjs?t=" + Date.now());
    await main();

    // Verify warning was logged but workflow didn't fail
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Failed to post comment"));
  });

  it("should handle issue creation failure gracefully", async () => {
    process.env.GH_AW_WORKFLOW_NAME = "Test Workflow";
    process.env.GH_AW_RUN_URL = "https://github.com/test-owner/test-repo/actions/runs/789";
    process.env.GH_AW_NOOP_MESSAGE = "All checks passed";
    process.env.GH_AW_AGENT_CONCLUSION = "success";

    // Create agent output file with only noop outputs
    const outputFile = path.join(tempDir, "agent_output.json");
    fs.writeFileSync(
      outputFile,
      JSON.stringify({
        items: [{ type: "noop", message: "All checks passed" }],
      })
    );
    process.env.GH_AW_AGENT_OUTPUT = outputFile;

    // Mock no existing issue
    mockGithub.rest.search.issuesAndPullRequests.mockResolvedValue({
      data: { total_count: 0, items: [] },
    });

    // Mock issue creation failure
    mockGithub.rest.issues.create.mockRejectedValue(new Error("Insufficient permissions"));

    const { main } = await import("./handle_noop_message.cjs?t=" + Date.now());
    await main();

    // Verify warning was logged but workflow didn't fail
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Could not create no-op runs issue"));
  });

  it("should extract run ID from URL correctly", async () => {
    process.env.GH_AW_WORKFLOW_NAME = "Test";
    process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/987654321";
    process.env.GH_AW_NOOP_MESSAGE = "Done";
    process.env.GH_AW_AGENT_CONCLUSION = "success";

    // Create agent output file with only noop outputs
    const outputFile = path.join(tempDir, "agent_output.json");
    fs.writeFileSync(
      outputFile,
      JSON.stringify({
        items: [{ type: "noop", message: "Done" }],
      })
    );
    process.env.GH_AW_AGENT_OUTPUT = outputFile;

    mockGithub.rest.search.issuesAndPullRequests.mockResolvedValue({
      data: { total_count: 1, items: [{ number: 1, node_id: "ID", html_url: "url" }] },
    });

    mockGithub.rest.issues.createComment.mockResolvedValue({ data: {} });

    const { main } = await import("./handle_noop_message.cjs?t=" + Date.now());
    await main();

    const commentCall = mockGithub.rest.issues.createComment.mock.calls[0][0];
    expect(commentCall.body).toContain("987654321");
  });

  it("should sanitize workflow name in comment", async () => {
    process.env.GH_AW_WORKFLOW_NAME = "Test <script>alert('xss')</script> Workflow";
    process.env.GH_AW_RUN_URL = "https://github.com/test/test/actions/runs/123";
    process.env.GH_AW_NOOP_MESSAGE = "Clean";
    process.env.GH_AW_AGENT_CONCLUSION = "success";

    // Create agent output file with only noop outputs
    const outputFile = path.join(tempDir, "agent_output.json");
    fs.writeFileSync(
      outputFile,
      JSON.stringify({
        items: [{ type: "noop", message: "Clean" }],
      })
    );
    process.env.GH_AW_AGENT_OUTPUT = outputFile;

    mockGithub.rest.search.issuesAndPullRequests.mockResolvedValue({
      data: { total_count: 1, items: [{ number: 1, node_id: "ID", html_url: "url" }] },
    });

    mockGithub.rest.issues.createComment.mockResolvedValue({ data: {} });

    const { main } = await import("./handle_noop_message.cjs?t=" + Date.now());
    await main();

    const commentCall = mockGithub.rest.issues.createComment.mock.calls[0][0];
    // Verify XSS attempt was sanitized (specific behavior depends on sanitizeContent implementation)
    expect(commentCall.body).not.toContain("<script>");
  });
});
