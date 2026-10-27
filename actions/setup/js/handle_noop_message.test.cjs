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

    // getPromptPath() throws unless GH_AW_PROMPTS_DIR or RUNNER_TEMP is set.
    // On CI RUNNER_TEMP is ambient, but it is not set in local dev, so set the
    // prompts dir explicitly to make the suite environment-independent. The
    // actual path is irrelevant because fs.readFileSync is mocked below.
    process.env.GH_AW_PROMPTS_DIR = tempDir;

    // Mock fs.readFileSync to return template content
    originalReadFileSync = fs.readFileSync;
    fs.readFileSync = vi.fn((filePath, encoding) => {
      if (filePath.includes("noop_runs_issue.md")) {
        return `This issue tracks all no-op runs from agentic workflows in this repository. Each workflow run that completes with a no-op message (indicating no action was needed) posts a comment here.

<details>
<summary>📘 What is a No-Op?</summary>

A no-op (no operation) occurs when an agentic workflow runs successfully but determines that no action is required. For example:
- A security scanner that finds no issues
- An update checker that finds nothing to update
- A monitoring workflow that finds everything is healthy

These are successful outcomes, not failures, and help provide transparency into workflow behavior.

</details>

<details>
<summary>🎯 How This Helps</summary>

This issue helps you:
- Track workflows that ran but determined no action was needed
- Distinguish between failures and intentional no-ops
- Monitor workflow health by seeing when workflows decide not to act

</details>

<details>
<summary>📚 Resources</summary>

- [GitHub Agentic Workflows Documentation](https://github.com/github/gh-aw)

</details>

<details>
<summary>How to disable no-op reporting for a workflow</summary>

To stop a workflow from posting here, set \`report-as-issue: false\` in its frontmatter:

\`\`\`yaml
safe-outputs:
  noop:
    report-as-issue: false
\`\`\`

</details>

---

> This issue is automatically managed by GitHub Agentic Workflows. Do not close this issue manually.
> 
> **No action to take** - Do not assign to an agent.

<!-- gh-aw-noop-runs -->`;
      }
      if (filePath.includes("noop_comment.md")) {
        return originalReadFileSync.call(fs, path.resolve(import.meta.dirname, "../md/noop_comment.md"), encoding);
      }
      return originalReadFileSync.call(fs, filePath, encoding);
    });

    // Mock core
    const mockSummary = {
      addRaw: vi.fn(),
      write: vi.fn().mockResolvedValue(undefined),
    };
    mockSummary.addRaw.mockReturnValue(mockSummary);
    mockCore = {
      info: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
      setOutput: vi.fn(),
      summary: mockSummary,
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
    // Restore environment by mutating process.env in place
    for (const key of Object.keys(process.env)) {
      if (!(key in originalEnv)) {
        delete process.env[key];
      }
    }
    Object.assign(process.env, originalEnv);

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

  it("should skip if no noop items in agent output", async () => {
    process.env.GH_AW_WORKFLOW_NAME = "Test Workflow";
    process.env.GH_AW_RUN_URL = "https://github.com/test-owner/test-repo/actions/runs/123";
    process.env.GH_AW_AGENT_CONCLUSION = "success";

    // Create agent output file with no noop items
    const outputFile = path.join(tempDir, "agent_output.json");
    fs.writeFileSync(
      outputFile,
      JSON.stringify({
        items: [{ type: "create_issue", title: "Some issue" }],
      })
    );
    process.env.GH_AW_AGENT_OUTPUT = outputFile;

    const { main } = await import("./handle_noop_message.cjs?t=" + Date.now());
    await main();

    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("No noop items found in agent output"));
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

    const summary = mockCore.summary.addRaw.mock.calls[0][0];
    expect(summary).toContain("<summary>✅ Conclusion Summary (1 no-op message)</summary>");
    expect(summary).toContain("**Target:** [Workflow run](https://github.com/test-owner/test-repo/actions/runs/123)");
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("report-as-issue is disabled"));
    expect(mockGithub.rest.search.issuesAndPullRequests).not.toHaveBeenCalled();
  });

  it.each(["No action needed", "Blocked by unavailable approval channel", "Missing PR diff data", "safeoutputs command denied", "private repository content, prompt, credentials and tool payload"])(
    "does not infer failure from or publish noop prose: %s",
    async message => {
      process.env.GH_AW_AGENT_CONCLUSION = "success";
      const outputFile = path.join(tempDir, "agent_output.json");
      fs.writeFileSync(outputFile, JSON.stringify({ items: [{ type: "noop", message }] }));
      process.env.GH_AW_AGENT_OUTPUT = outputFile;
      mockGithub.rest.search.issuesAndPullRequests.mockResolvedValue({
        data: { total_count: 1, items: [{ number: 42, node_id: "ID", html_url: "url" }] },
      });
      mockGithub.rest.issues.createComment.mockResolvedValue({ data: {} });

      const { main } = await import("./handle_noop_message.cjs?t=" + Date.now());
      await main();

      const body = mockGithub.rest.issues.createComment.mock.calls[0][0].body;
      expect(body).toContain("**Outcome:** `reported_no_action`");
      expect(body).toContain("does not verify");
      expect(body).not.toContain(message);
      expect(JSON.stringify(mockCore.info.mock.calls)).not.toContain(message);
    }
  );

  it.each(["cancelled", "skipped", "", "private unknown status"])("keeps completion unconfirmed for status %s", async conclusion => {
    process.env.GH_AW_AGENT_CONCLUSION = conclusion;
    const outputFile = path.join(tempDir, "agent_output.json");
    fs.writeFileSync(outputFile, JSON.stringify({ items: [{ type: "noop", message: "private noop text" }] }));
    process.env.GH_AW_AGENT_OUTPUT = outputFile;

    const { main, classifyNoopOutcome } = await import("./handle_noop_message.cjs?t=" + Date.now());
    await main();

    expect(classifyNoopOutcome(conclusion).category).toBe("unconfirmed");
    const body = mockCore.setOutput.mock.calls.find(call => call[0] === "noop_comment_body")[1];
    expect(body).toContain("**Outcome:** `unconfirmed`");
    expect(body).not.toContain("private noop text");
    expect(mockGithub.rest.issues.createComment).not.toHaveBeenCalled();
  });

  it("leaves explicit report_incomplete to failure reporting rather than the noop tracker", async () => {
    process.env.GH_AW_AGENT_CONCLUSION = "success";
    const outputFile = path.join(tempDir, "agent_output.json");
    fs.writeFileSync(
      outputFile,
      JSON.stringify({
        items: [
          { type: "noop", message: "No action needed" },
          { type: "report_incomplete", reason: "private reason" },
        ],
      })
    );
    process.env.GH_AW_AGENT_OUTPUT = outputFile;

    const { main } = await import("./handle_noop_message.cjs?t=" + Date.now());
    await main();

    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Found 1 non-noop output(s)"));
    expect(mockGithub.rest.issues.createComment).not.toHaveBeenCalled();
    const body = mockCore.setOutput.mock.calls.find(call => call[0] === "noop_comment_body")[1];
    expect(body).toContain("**Outcome:** `incomplete`");
    expect(body).not.toContain("private reason");
  });

  it("should expose the rendered comment body as an output", async () => {
    process.env.GH_AW_WORKFLOW_NAME = "Test Workflow";
    process.env.GH_AW_RUN_URL = "https://github.com/test-owner/test-repo/actions/runs/123";
    process.env.GH_AW_AGENT_CONCLUSION = "success";
    process.env.GH_AW_NOOP_REPORT_AS_ISSUE = "false";

    const outputFile = path.join(tempDir, "agent_output.json");
    fs.writeFileSync(
      outputFile,
      JSON.stringify({
        items: [{ type: "noop", message: "Nothing to do" }],
      })
    );
    process.env.GH_AW_AGENT_OUTPUT = outputFile;

    const { main } = await import("./handle_noop_message.cjs?t=" + Date.now());
    await main();

    const commentBodyCall = mockCore.setOutput.mock.calls.find(call => call[0] === "noop_comment_body");
    expect(commentBodyCall).toBeDefined();
    expect(commentBodyCall[1]).toContain("### Test Workflow");
    expect(commentBodyCall[1]).toContain("reported_no_action");
    expect(commentBodyCall[1]).not.toContain("Nothing to do");
    expect(mockCore.setOutput).toHaveBeenCalledWith("noop_message", "Nothing to do");
  });

  it.each([
    { taskOutput: true, completed: true, genuineReport: false, category: "reported_no_action" },
    { taskOutput: false, completed: true, genuineReport: false, category: "incomplete" },
    { taskOutput: true, completed: false, genuineReport: false, category: "incomplete" },
    { taskOutput: true, completed: true, genuineReport: true, category: "incomplete" },
  ])("matches failure policy for registration diagnostics: %j", async ({ taskOutput, completed, genuineReport, category }) => {
    process.env.GH_AW_AGENT_CONCLUSION = "success";
    process.env.GH_AW_NOOP_REPORT_AS_ISSUE = "false";
    const outputFile = path.join(tempDir, "agent_output.json");
    const items = [
      { type: "noop", message: "private noop prose" },
      { type: "report_incomplete", reason: "task_complete tool calls are not registering; analysis completed successfully" },
    ];
    if (taskOutput) items.push({ type: "add_comment", body: "private task output" });
    if (genuineReport) items.push({ type: "report_incomplete", reason: "private genuine failure" });
    fs.writeFileSync(outputFile, JSON.stringify({ items }));
    if (completed) fs.writeFileSync(path.join(tempDir, "agent-stdio.log"), '{"terminal_reason":"completed"}\n');
    process.env.GH_AW_AGENT_OUTPUT = outputFile;

    const { main } = await import("./handle_noop_message.cjs?t=" + Date.now());
    await main();

    const body = mockCore.setOutput.mock.calls.find(call => call[0] === "noop_comment_body")[1];
    expect(body).toContain(`**Outcome:** \`${category}\``);
    expect(body).not.toContain("private");
    expect(body).not.toContain("not registering");
    expect(mockCore.setOutput).toHaveBeenCalledWith("noop_message", "private noop prose");
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

  it("should skip if agent conclusion is cancelled (not success or failure)", async () => {
    process.env.GH_AW_WORKFLOW_NAME = "Test Workflow";
    process.env.GH_AW_RUN_URL = "https://github.com/test-owner/test-repo/actions/runs/123";
    process.env.GH_AW_AGENT_CONCLUSION = "cancelled";

    // Create agent output file with noop items so we reach the conclusion check
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

    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Agent did not succeed"));
    expect(mockGithub.rest.search.issuesAndPullRequests).not.toHaveBeenCalled();
  });

  it("should skip if agent failed with no agent output file", async () => {
    process.env.GH_AW_WORKFLOW_NAME = "Test Workflow";
    process.env.GH_AW_RUN_URL = "https://github.com/test-owner/test-repo/actions/runs/123";
    process.env.GH_AW_NOOP_MESSAGE = "Some message";
    process.env.GH_AW_AGENT_CONCLUSION = "failure";

    const { main } = await import("./handle_noop_message.cjs?t=" + Date.now());
    await main();

    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Could not load agent output, skipping"));
    expect(mockGithub.rest.search.issuesAndPullRequests).not.toHaveBeenCalled();
  });

  it("should report incomplete when agent failed with only noop outputs", async () => {
    process.env.GH_AW_WORKFLOW_NAME = "Test Workflow";
    process.env.GH_AW_RUN_URL = "https://github.com/test-owner/test-repo/actions/runs/123";
    process.env.GH_AW_NOOP_MESSAGE = "No issues found";
    process.env.GH_AW_AGENT_CONCLUSION = "failure";

    // Create agent output file with only noop outputs
    const outputFile = path.join(tempDir, "agent_output.json");
    fs.writeFileSync(
      outputFile,
      JSON.stringify({
        items: [{ type: "noop", message: "No issues found" }],
      })
    );
    process.env.GH_AW_AGENT_OUTPUT = outputFile;

    mockGithub.rest.search.issuesAndPullRequests.mockResolvedValue({
      data: { total_count: 1, items: [{ number: 1, node_id: "ID", html_url: "url" }] },
    });

    mockGithub.rest.issues.createComment.mockResolvedValue({ data: {} });

    const { main } = await import("./handle_noop_message.cjs?t=" + Date.now());
    await main();

    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("reporting incomplete outcome"));
    expect(mockGithub.rest.issues.createComment).toHaveBeenCalled();
    const body = mockGithub.rest.issues.createComment.mock.calls[0][0].body;
    expect(body).toContain("**Outcome:** `incomplete`");
    expect(body).toContain("Task completion is unconfirmed");
    expect(body).not.toContain("No issues found");
    expect(body).not.toContain("reported_no_action");
  });

  it("should skip if agent failed with non-noop outputs", async () => {
    process.env.GH_AW_WORKFLOW_NAME = "Test Workflow";
    process.env.GH_AW_RUN_URL = "https://github.com/test-owner/test-repo/actions/runs/123";
    process.env.GH_AW_NOOP_MESSAGE = "Some message";
    process.env.GH_AW_AGENT_CONCLUSION = "failure";

    // Create agent output file with noop + non-noop outputs
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
      q: expect.stringContaining("[aw] No-Op Runs"),
      per_page: 1,
    });

    // Verify issue was created with correct title
    const createCall = mockGithub.rest.issues.create.mock.calls[0][0];
    expect(createCall.title).toBe("[aw] No-Op Runs");
    expect(createCall.labels).toContain("agentic-workflows");
    expect(createCall.body).toContain("tracks all no-op runs");

    // Verify comment was posted
    const commentCall = mockGithub.rest.issues.createComment.mock.calls[0][0];
    expect(commentCall.issue_number).toBe(42);
    expect(commentCall.body).toContain("Test Workflow");
    expect(commentCall.body).toContain("reported_no_action");
    expect(commentCall.body).not.toContain("No updates needed");
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
    expect(commentCall.body).toContain("reported_no_action");
    expect(commentCall.body).not.toContain("Everything is up to date");
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

  it("should not create issue when search throws (prevents duplicate issues)", async () => {
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

    // Mock search failure (e.g. transient API error)
    mockGithub.rest.search.issuesAndPullRequests.mockRejectedValue(new Error("API rate limit exceeded"));

    const { main } = await import("./handle_noop_message.cjs?t=" + Date.now());
    await main();

    // Search error should be caught and logged as a warning (via ensureAgentRunsIssue throw → main catch)
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Could not create no-op runs issue"));
    // Issue must NOT be created to prevent duplicates
    expect(mockGithub.rest.issues.create).not.toHaveBeenCalled();
    // Comment must NOT be posted
    expect(mockGithub.rest.issues.createComment).not.toHaveBeenCalled();
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

  it.each([
    ["0.025", "~1"],
    ["0.000000000001", "~1"],
    ["1", "~1"],
    ["1.000000000001", "~2"],
    ["12.4", "~13"],
    ["1000.1", "~1001"],
    ["1000000.1", "~1000001"],
  ])("should include estimated %s AIC in the no-op footer", async (value, formatted) => {
    process.env.GH_AW_WORKFLOW_NAME = "AIC Test Workflow";
    process.env.GH_AW_RUN_URL = "https://github.com/test/test/actions/runs/123";
    process.env.GH_AW_AGENT_CONCLUSION = "success";
    process.env.GH_AW_AIC = value;

    const outputFile = path.join(tempDir, "agent_output.json");
    fs.writeFileSync(outputFile, JSON.stringify({ items: [{ type: "noop", message: "No action needed" }] }));
    process.env.GH_AW_AGENT_OUTPUT = outputFile;

    mockGithub.rest.search.issuesAndPullRequests.mockResolvedValue({
      data: { total_count: 1, items: [{ number: 1, node_id: "ID", html_url: "url" }] },
    });
    mockGithub.rest.issues.createComment.mockResolvedValue({ data: {} });

    const { main } = await import("./handle_noop_message.cjs?t=" + Date.now());
    await main();

    const commentCall = mockGithub.rest.issues.createComment.mock.calls[0][0];
    expect(commentCall.body).toContain(` · ${formatted.replace("~", "\\~")} AIC`);
  });

  it("should include detection AIC in the footer total when GH_AW_THREAT_DETECTION_AIC is set", async () => {
    process.env.GH_AW_WORKFLOW_NAME = "Detection AIC Workflow";
    process.env.GH_AW_RUN_URL = "https://github.com/test/test/actions/runs/123";
    process.env.GH_AW_AGENT_CONCLUSION = "success";
    process.env.GH_AW_AIC = "0.100";
    process.env.GH_AW_THREAT_DETECTION_AIC = "0.025";
    process.env.GH_AW_ENGINE_MODEL = "claude-sonnet-4.6";

    const outputFile = path.join(tempDir, "agent_output.json");
    fs.writeFileSync(outputFile, JSON.stringify({ items: [{ type: "noop", message: "No action needed" }] }));
    process.env.GH_AW_AGENT_OUTPUT = outputFile;

    mockGithub.rest.search.issuesAndPullRequests.mockResolvedValue({
      data: { total_count: 1, items: [{ number: 1, node_id: "ID", html_url: "url" }] },
    });
    mockGithub.rest.issues.createComment.mockResolvedValue({ data: {} });

    const { main } = await import("./handle_noop_message.cjs?t=" + Date.now());
    await main();

    const commentCall = mockGithub.rest.issues.createComment.mock.calls[0][0];
    expect(commentCall.body).toContain("sonnet46 · \\~1 AIC · ⌖ \\~1 AIC");
  });

  it("should include evals AIC in the footer breakdown when GH_AW_EVALS_AIC is set", async () => {
    process.env.GH_AW_WORKFLOW_NAME = "Evals AIC Workflow";
    process.env.GH_AW_RUN_URL = "https://github.com/test/test/actions/runs/123";
    process.env.GH_AW_AGENT_CONCLUSION = "success";
    process.env.GH_AW_AIC = "0.100";
    process.env.GH_AW_EVALS_AIC = "0.025";
    process.env.GH_AW_ENGINE_MODEL = "claude-sonnet-4.6";

    const outputFile = path.join(tempDir, "agent_output.json");
    fs.writeFileSync(outputFile, JSON.stringify({ items: [{ type: "noop", message: "No action needed" }] }));
    process.env.GH_AW_AGENT_OUTPUT = outputFile;

    mockGithub.rest.search.issuesAndPullRequests.mockResolvedValue({
      data: { total_count: 1, items: [{ number: 1, node_id: "ID", html_url: "url" }] },
    });
    mockGithub.rest.issues.createComment.mockResolvedValue({ data: {} });

    const { main } = await import("./handle_noop_message.cjs?t=" + Date.now());
    await main();

    const commentCall = mockGithub.rest.issues.createComment.mock.calls[0][0];
    expect(commentCall.body).toContain("sonnet46 · \\~1 AIC · ◇ \\~1 AIC");
  });

  it("should place evals AIC after detection AIC in the footer breakdown", async () => {
    process.env.GH_AW_WORKFLOW_NAME = "Ordered AIC Workflow";
    process.env.GH_AW_RUN_URL = "https://github.com/test/test/actions/runs/123";
    process.env.GH_AW_AGENT_CONCLUSION = "success";
    process.env.GH_AW_AIC = "0.100";
    process.env.GH_AW_THREAT_DETECTION_AIC = "0.025";
    process.env.GH_AW_EVALS_AIC = "0.010";
    process.env.GH_AW_ENGINE_MODEL = "claude-sonnet-4.6";

    const outputFile = path.join(tempDir, "agent_output.json");
    fs.writeFileSync(outputFile, JSON.stringify({ items: [{ type: "noop", message: "No action needed" }] }));
    process.env.GH_AW_AGENT_OUTPUT = outputFile;

    mockGithub.rest.search.issuesAndPullRequests.mockResolvedValue({
      data: { total_count: 1, items: [{ number: 1, node_id: "ID", html_url: "url" }] },
    });
    mockGithub.rest.issues.createComment.mockResolvedValue({ data: {} });

    const { main } = await import("./handle_noop_message.cjs?t=" + Date.now());
    await main();

    const commentCall = mockGithub.rest.issues.createComment.mock.calls[0][0];
    expect(commentCall.body).toContain("sonnet46 · \\~1 AIC · ⌖ \\~1 AIC · ◇ \\~1 AIC");
  });

  it("should not include AIC suffix in comment footer when GH_AW_AIC is not set", async () => {
    process.env.GH_AW_WORKFLOW_NAME = "No Credits Workflow";
    process.env.GH_AW_RUN_URL = "https://github.com/test/test/actions/runs/123";
    process.env.GH_AW_AGENT_CONCLUSION = "success";
    delete process.env.GH_AW_AIC;
    delete process.env.GH_AW_THREAT_DETECTION_AIC;

    const outputFile = path.join(tempDir, "agent_output.json");
    fs.writeFileSync(outputFile, JSON.stringify({ items: [{ type: "noop", message: "No action needed" }] }));
    process.env.GH_AW_AGENT_OUTPUT = outputFile;

    mockGithub.rest.search.issuesAndPullRequests.mockResolvedValue({
      data: { total_count: 1, items: [{ number: 1, node_id: "ID", html_url: "url" }] },
    });
    mockGithub.rest.issues.createComment.mockResolvedValue({ data: {} });

    const { main } = await import("./handle_noop_message.cjs?t=" + Date.now());
    await main();

    const commentCall = mockGithub.rest.issues.createComment.mock.calls[0][0];
    expect(commentCall.body).not.toContain(" AIC");
  });

  it("should include ambient context suffix in comment footer when GH_AW_AMBIENT_CONTEXT is set", async () => {
    process.env.GH_AW_WORKFLOW_NAME = "Ambient Context Workflow";
    process.env.GH_AW_RUN_URL = "https://github.com/test/test/actions/runs/123";
    process.env.GH_AW_AGENT_CONCLUSION = "success";
    process.env.GH_AW_AMBIENT_CONTEXT = "1200";

    const outputFile = path.join(tempDir, "agent_output.json");
    fs.writeFileSync(outputFile, JSON.stringify({ items: [{ type: "noop", message: "No action needed" }] }));
    process.env.GH_AW_AGENT_OUTPUT = outputFile;

    mockGithub.rest.search.issuesAndPullRequests.mockResolvedValue({
      data: { total_count: 1, items: [{ number: 1, node_id: "ID", html_url: "url" }] },
    });
    mockGithub.rest.issues.createComment.mockResolvedValue({ data: {} });

    const { main } = await import("./handle_noop_message.cjs?t=" + Date.now());
    await main();

    const commentCall = mockGithub.rest.issues.createComment.mock.calls[0][0];
    expect(commentCall.body).toContain("⊞");
    expect(commentCall.body).toContain("1.2K");
  });

  it("should include history link in comment footer when GH_AW_WORKFLOW_ID is set", async () => {
    process.env.GH_AW_WORKFLOW_NAME = "History Link Workflow";
    process.env.GH_AW_RUN_URL = "https://github.com/test/test/actions/runs/123";
    process.env.GH_AW_AGENT_CONCLUSION = "success";
    process.env.GH_AW_WORKFLOW_ID = "history-link-workflow";

    const outputFile = path.join(tempDir, "agent_output.json");
    fs.writeFileSync(outputFile, JSON.stringify({ items: [{ type: "noop", message: "No action needed" }] }));
    process.env.GH_AW_AGENT_OUTPUT = outputFile;

    mockGithub.rest.search.issuesAndPullRequests.mockResolvedValue({
      data: { total_count: 1, items: [{ number: 1, node_id: "ID", html_url: "url" }] },
    });
    mockGithub.rest.issues.createComment.mockResolvedValue({ data: {} });

    const { main } = await import("./handle_noop_message.cjs?t=" + Date.now());
    await main();

    const commentCall = mockGithub.rest.issues.createComment.mock.calls[0][0];
    expect(commentCall.body).toContain("◷");
    expect(commentCall.body).toContain("history-link-workflow");
  });

  it("should not include history link when GH_AW_WORKFLOW_ID is not set", async () => {
    process.env.GH_AW_WORKFLOW_NAME = "No History Workflow";
    process.env.GH_AW_RUN_URL = "https://github.com/test/test/actions/runs/123";
    process.env.GH_AW_AGENT_CONCLUSION = "success";
    delete process.env.GH_AW_WORKFLOW_ID;

    const outputFile = path.join(tempDir, "agent_output.json");
    fs.writeFileSync(outputFile, JSON.stringify({ items: [{ type: "noop", message: "No action needed" }] }));
    process.env.GH_AW_AGENT_OUTPUT = outputFile;

    mockGithub.rest.search.issuesAndPullRequests.mockResolvedValue({
      data: { total_count: 1, items: [{ number: 1, node_id: "ID", html_url: "url" }] },
    });
    mockGithub.rest.issues.createComment.mockResolvedValue({ data: {} });

    const { main } = await import("./handle_noop_message.cjs?t=" + Date.now());
    await main();

    const commentCall = mockGithub.rest.issues.createComment.mock.calls[0][0];
    expect(commentCall.body).not.toContain("◷");
  });
});
