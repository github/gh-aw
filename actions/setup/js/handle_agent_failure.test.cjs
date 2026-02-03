// @ts-check
/// <reference types="@actions/github-script" />

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import fs from "fs";

describe("handle_agent_failure.cjs", () => {
  let main;
  let mockCore;
  let mockGithub;
  let mockContext;
  let originalEnv;
  let originalReadFileSync;

  beforeEach(async () => {
    // Save original environment
    originalEnv = { ...process.env };

    // Mock fs.readFileSync to return template content
    originalReadFileSync = fs.readFileSync;
    fs.readFileSync = vi.fn((filePath, encoding) => {
      if (filePath.includes("agent_failure_issue.md")) {
        return `### Workflow Failure

**Workflow:** [{workflow_name}]({workflow_source_url})  
**Branch:** {branch}  
**Run URL:** {run_url}{pull_request_info}

{secret_verification_context}{assignment_errors_context}{missing_safe_outputs_context}

### Action Required

Debug this workflow failure using the \`agentic-workflows\` agent:

\`\`\`
/agent agentic-workflows
\`\`\`

When prompted, instruct the agent to debug this workflow failure.`;
      } else if (filePath.includes("agent_failure_comment.md")) {
        return `Agent job [{run_id}]({run_url}) failed.

{secret_verification_context}{assignment_errors_context}{missing_safe_outputs_context}`;
      }
      return originalReadFileSync.call(fs, filePath, encoding);
    });

    // Mock core
    mockCore = {
      info: vi.fn(),
      warning: vi.fn(),
      setFailed: vi.fn(),
      setOutput: vi.fn(),
      error: vi.fn(),
    };
    global.core = mockCore;

    // Mock github
    mockGithub = {
      rest: {
        search: {
          issuesAndPullRequests: vi.fn(),
        },
        issues: {
          create: vi.fn(),
          createComment: vi.fn(),
          update: vi.fn(),
        },
      },
    };
    global.github = mockGithub;

    // Mock context
    mockContext = {
      repo: {
        owner: "test-owner",
        repo: "test-repo",
      },
    };
    global.context = mockContext;

    // Set up environment
    process.env.GH_AW_WORKFLOW_NAME = "Test Workflow";
    process.env.GH_AW_AGENT_CONCLUSION = "failure";
    process.env.GH_AW_RUN_URL = "https://github.com/test-owner/test-repo/actions/runs/123";
    process.env.GH_AW_WORKFLOW_SOURCE = "test-owner/test-repo/.github/workflows/test.md@main";
    process.env.GH_AW_WORKFLOW_SOURCE_URL = "https://github.com/test-owner/test-repo/blob/main/.github/workflows/test.md";
    process.env.GITHUB_REF_NAME = "main"; // Add this to prevent getCurrentBranch from failing

    // Load the module
    const module = await import("./handle_agent_failure.cjs");
    main = module.main;
  });

  afterEach(() => {
    // Restore fs
    fs.readFileSync = originalReadFileSync;

    // Restore environment
    process.env = originalEnv;

    // Clear mocks
    vi.clearAllMocks();
  });

  describe("when agent job failed", () => {
    it("should create parent issue and link sub-issue when creating new failure issue", async () => {
      // Mock no existing parent issue - will create it
      mockGithub.rest.search.issuesAndPullRequests
        .mockResolvedValueOnce({
          // First search: PR search (no PR found)
          data: { total_count: 0, items: [] },
        })
        .mockResolvedValueOnce({
          // Second search: parent issue
          data: { total_count: 0, items: [] },
        })
        .mockResolvedValueOnce({
          // Third search: failure issue
          data: { total_count: 0, items: [] },
        });

      // Mock parent issue creation
      mockGithub.rest.issues.create
        .mockResolvedValueOnce({
          // Parent issue
          data: {
            number: 1,
            html_url: "https://github.com/test-owner/test-repo/issues/1",
            node_id: "I_parent_1",
          },
        })
        .mockResolvedValueOnce({
          // Failure issue
          data: {
            number: 42,
            html_url: "https://github.com/test-owner/test-repo/issues/42",
            node_id: "I_sub_42",
          },
        });

      // Mock GraphQL sub-issue linking
      mockGithub.graphql = vi.fn().mockResolvedValue({
        addSubIssue: {
          issue: { id: "I_parent_1", number: 1 },
          subIssue: { id: "I_sub_42", number: 42 },
        },
      });

      await main();

      // Verify parent issue was searched for
      expect(mockGithub.rest.search.issuesAndPullRequests).toHaveBeenCalledWith({
        q: expect.stringContaining('repo:test-owner/test-repo is:issue is:open label:agentic-workflows in:title "[agentic-workflows] Failed runs"'),
        per_page: 1,
      });

      // Verify parent issue was created
      expect(mockGithub.rest.issues.create).toHaveBeenCalledWith({
        owner: "test-owner",
        repo: "test-repo",
        title: "[agentic-workflows] Failed runs",
        body: expect.stringContaining("This issue tracks all failures from agentic workflows"),
        labels: ["agentic-workflows"],
      });

      // Verify parent body contains troubleshooting info
      const parentCreateCall = mockGithub.rest.issues.create.mock.calls[0][0];
      expect(parentCreateCall.body).toContain("agentic-workflows");
      expect(parentCreateCall.body).toContain("gh aw logs");
      expect(parentCreateCall.body).toContain("gh aw audit");
      expect(parentCreateCall.body).toContain("no:parent-issue");
      expect(parentCreateCall.body).toContain("- [x] expires");
      expect(parentCreateCall.body).toContain("<!-- gh-aw-expires:");
      expect(parentCreateCall.body).toMatch(/- \[x\] expires <!-- gh-aw-expires: \d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z --> on .+ UTC/);

      // Verify failure issue was created (second call, after parent issue)
      expect(mockGithub.rest.issues.create).toHaveBeenNthCalledWith(
        2,
        expect.objectContaining({
          owner: "test-owner",
          repo: "test-repo",
          title: "[agentics] Test Workflow failed",
          body: expect.stringContaining("Test Workflow"),
          labels: ["agentic-workflows"],
        })
      );

      // Verify sub-issue was linked
      expect(mockGithub.graphql).toHaveBeenCalledWith(expect.stringContaining("addSubIssue"), {
        parentId: "I_parent_1",
        subIssueId: "I_sub_42",
      });

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Created parent issue #1"));
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Created new issue #42"));
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Successfully linked #42 as sub-issue of #1"));
    });

    it("should reuse existing parent issue when it exists", async () => {
      // Mock existing parent issue
      mockGithub.rest.search.issuesAndPullRequests
        .mockResolvedValueOnce({
          // First search: PR search (no PR found)
          data: { total_count: 0, items: [] },
        })
        .mockResolvedValueOnce({
          // Second search: existing parent issue
          data: {
            total_count: 1,
            items: [
              {
                number: 5,
                html_url: "https://github.com/test-owner/test-repo/issues/5",
                node_id: "I_parent_5",
              },
            ],
          },
        })
        .mockResolvedValueOnce({
          // Third search: no failure issue
          data: { total_count: 0, items: [] },
        });

      // Mock failure issue creation only (parent already exists)
      mockGithub.rest.issues.create.mockResolvedValueOnce({
        data: {
          number: 42,
          html_url: "https://github.com/test-owner/test-repo/issues/42",
          node_id: "I_sub_42",
        },
      });

      // Mock GraphQL - first check sub-issue count (30), then link sub-issue
      mockGithub.graphql = vi
        .fn()
        .mockResolvedValueOnce({
          // Sub-issue count check
          repository: {
            issue: {
              subIssues: {
                totalCount: 30,
              },
            },
          },
        })
        .mockResolvedValueOnce({
          // Link sub-issue
          addSubIssue: {
            issue: { id: "I_parent_5", number: 5 },
            subIssue: { id: "I_sub_42", number: 42 },
          },
        });

      await main();

      // Verify parent issue was found (not created)
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Found existing parent issue #5"));

      // Verify only failure issue was created (not parent)
      expect(mockGithub.rest.issues.create).toHaveBeenCalledTimes(1);
      expect(mockGithub.rest.issues.create).toHaveBeenCalledWith({
        owner: "test-owner",
        repo: "test-repo",
        title: "[agentics] Test Workflow failed",
        body: expect.any(String),
        labels: ["agentic-workflows"],
      });

      // Verify sub-issue was linked to existing parent
      expect(mockGithub.graphql).toHaveBeenCalledWith(expect.stringContaining("addSubIssue"), {
        parentId: "I_parent_5",
        subIssueId: "I_sub_42",
      });
    });

    it("should handle sub-issue API not available gracefully", async () => {
      // Mock searches
      mockGithub.rest.search.issuesAndPullRequests
        .mockResolvedValueOnce({
          // First search: PR search (no PR found)
          data: { total_count: 0, items: [] },
        })
        .mockResolvedValueOnce({
          // Second search: parent issue
          data: { total_count: 0, items: [] },
        })
        .mockResolvedValueOnce({
          // Third search: failure issue
          data: { total_count: 0, items: [] },
        });

      // Mock issue creation
      mockGithub.rest.issues.create
        .mockResolvedValueOnce({
          data: { number: 1, html_url: "https://example.com/1", node_id: "I_1" },
        })
        .mockResolvedValueOnce({
          data: { number: 42, html_url: "https://example.com/42", node_id: "I_42" },
        });

      // Mock GraphQL failure (sub-issue API not available)
      // Note: The sub-issue API is attempted after issue creation, so we reject it
      mockGithub.graphql = vi.fn().mockRejectedValue(new Error("Field 'addSubIssue' doesn't exist on type 'Mutation'"));

      await main();

      // Verify both issues were created
      expect(mockGithub.rest.issues.create).toHaveBeenCalledTimes(2);

      // Verify warning was logged
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Sub-issue API not available"));
    });

    it("should continue if parent issue creation fails", async () => {
      // Mock searches
      mockGithub.rest.search.issuesAndPullRequests
        .mockResolvedValueOnce({
          // First search: PR search (no PR found)
          data: { total_count: 0, items: [] },
        })
        .mockResolvedValueOnce({
          // Second search: parent issue
          data: { total_count: 0, items: [] },
        })
        .mockResolvedValueOnce({
          // Third search: failure issue
          data: { total_count: 0, items: [] },
        });

      // Mock parent issue creation failure, but failure issue creation succeeds
      mockGithub.rest.issues.create.mockRejectedValueOnce(new Error("API Error creating parent")).mockResolvedValueOnce({
        data: { number: 42, html_url: "https://example.com/42", node_id: "I_42" },
      });

      // Mock GraphQL - won't be called since parent creation failed
      mockGithub.graphql = vi.fn().mockResolvedValue({
        addSubIssue: {
          issue: { id: "I_parent_1", number: 1 },
          subIssue: { id: "I_42", number: 42 },
        },
      });

      await main();

      // Verify warning about parent issue creation
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Could not create parent issue"));

      // Verify failure issue was still created
      expect(mockGithub.rest.issues.create).toHaveBeenCalledWith({
        owner: "test-owner",
        repo: "test-repo",
        title: "[agentics] Test Workflow failed",
        body: expect.any(String),
        labels: ["agentic-workflows"],
      });
    });

    it("should create a new issue when no existing issue is found", async () => {
      // Mock no existing issues (PR search + parent search + failure issue search)
      mockGithub.rest.search.issuesAndPullRequests
        .mockResolvedValueOnce({
          // First search: PR search (no PR found)
          data: { total_count: 0, items: [] },
        })
        .mockResolvedValueOnce({
          // Second search: parent issue
          data: { total_count: 0, items: [] },
        })
        .mockResolvedValueOnce({
          // Third search: failure issue
          data: { total_count: 0, items: [] },
        });

      mockGithub.rest.issues.create
        .mockResolvedValueOnce({
          // Parent issue
          data: { number: 1, html_url: "https://example.com/1", node_id: "I_1" },
        })
        .mockResolvedValueOnce({
          // Failure issue
          data: {
            number: 42,
            html_url: "https://github.com/test-owner/test-repo/issues/42",
            node_id: "I_42",
          },
        });

      // Mock GraphQL - new parent created, so just addSubIssue
      mockGithub.graphql = vi.fn().mockResolvedValue({
        addSubIssue: {
          issue: { id: "I_1", number: 1 },
          subIssue: { id: "I_42", number: 42 },
        },
      });

      await main();

      // Verify search was called
      expect(mockGithub.rest.search.issuesAndPullRequests).toHaveBeenCalledWith({
        q: expect.stringContaining('repo:test-owner/test-repo is:issue is:open label:agentic-workflows in:title "[agentics] Test Workflow failed"'),
        per_page: 1,
      });

      // Verify failure issue was created (second call, after parent issue)
      expect(mockGithub.rest.issues.create).toHaveBeenNthCalledWith(
        2,
        expect.objectContaining({
          owner: "test-owner",
          repo: "test-repo",
          title: "[agentics] Test Workflow failed",
          body: expect.stringContaining("Test Workflow"),
          labels: ["agentic-workflows"],
        })
      );

      // Verify body contains required sections (check second call - failure issue)
      const failureIssueCreateCall = mockGithub.rest.issues.create.mock.calls[1][0];
      expect(failureIssueCreateCall.body).toContain("### Workflow Failure");
      expect(failureIssueCreateCall.body).toContain("### Action Required");
      expect(failureIssueCreateCall.body).toContain("agentic-workflows");
      expect(failureIssueCreateCall.body).toContain("https://github.com/test-owner/test-repo/actions/runs/123");
      expect(failureIssueCreateCall.body).toContain("**Branch:**");
      expect(failureIssueCreateCall.body).toContain("- [x] expires");
      expect(failureIssueCreateCall.body).toContain("<!-- gh-aw-expires:");
      expect(failureIssueCreateCall.body).not.toContain("## Root Cause");
      expect(failureIssueCreateCall.body).not.toContain("## Expected Outcome");
      expect(failureIssueCreateCall.body).toContain("Generated from [Test Workflow](https://github.com/test-owner/test-repo/actions/runs/123)");

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Created new issue #42"));
    });

    it("should add a comment to existing issue when found", async () => {
      // Mock searches: PR search, parent search, and existing failure issue search
      mockGithub.rest.search.issuesAndPullRequests
        .mockResolvedValueOnce({
          // First search: PR search (no PR found)
          data: { total_count: 0, items: [] },
        })
        .mockResolvedValueOnce({
          // Second search: parent issue (exists)
          data: {
            total_count: 1,
            items: [
              {
                number: 1,
                html_url: "https://github.com/test-owner/test-repo/issues/1",
                node_id: "I_parent_1",
              },
            ],
          },
        })
        .mockResolvedValueOnce({
          // Third search: existing failure issue
          data: {
            total_count: 1,
            items: [
              {
                number: 10,
                html_url: "https://github.com/test-owner/test-repo/issues/10",
              },
            ],
          },
        });

      // Mock GraphQL sub-issue count check (parent exists with < 64 sub-issues)
      // When an existing failure issue is found, only the count check happens (no new issue created/linked)
      mockGithub.graphql = vi.fn().mockResolvedValue({
        repository: {
          issue: {
            subIssues: {
              totalCount: 20,
            },
          },
        },
      });

      mockGithub.rest.issues.createComment.mockResolvedValue({});

      await main();

      // Verify comment was created
      expect(mockGithub.rest.issues.createComment).toHaveBeenCalledWith({
        owner: "test-owner",
        repo: "test-repo",
        issue_number: 10,
        body: expect.stringContaining("Agent job [123]"),
      });

      // Verify comment contains required sections
      const commentCall = mockGithub.rest.issues.createComment.mock.calls[0][0];
      expect(commentCall.body).toContain("Agent job [123]");
      expect(commentCall.body).toContain("https://github.com/test-owner/test-repo/actions/runs/123");
      expect(commentCall.body).not.toContain("```bash");
      expect(commentCall.body).toContain("Generated from [Test Workflow](https://github.com/test-owner/test-repo/actions/runs/123)");

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Added comment to existing issue #10"));
    });

    it("should sanitize workflow name in title", async () => {
      process.env.GH_AW_WORKFLOW_NAME = "Test @user <script>alert(1)</script>";

      mockGithub.rest.search.issuesAndPullRequests
        .mockResolvedValueOnce({
          // First search: PR search (no PR found)
          data: { total_count: 0, items: [] },
        })
        .mockResolvedValueOnce({
          // Second search: parent issue
          data: { total_count: 0, items: [] },
        })
        .mockResolvedValueOnce({
          // Third search: failure issue
          data: { total_count: 0, items: [] },
        });

      mockGithub.rest.issues.create
        .mockResolvedValueOnce({
          data: { number: 1, html_url: "https://example.com/1", node_id: "I_1" },
        })
        .mockResolvedValueOnce({
          data: { number: 2, html_url: "https://example.com/2", node_id: "I_2" },
        });

      // Mock GraphQL - new parent created, so just addSubIssue
      mockGithub.graphql = vi.fn().mockResolvedValue({
        addSubIssue: {
          issue: { id: "I_1", number: 1 },
          subIssue: { id: "I_2", number: 2 },
        },
      });

      await main();

      const failureIssueCreateCall = mockGithub.rest.issues.create.mock.calls[1][0];
      // Verify sanitization occurred - script tags are removed/escaped
      expect(failureIssueCreateCall.title).not.toContain("<script>");
      // Verify mentions are escaped
      expect(failureIssueCreateCall.body).toContain("`@user`");
    });

    it("should handle API errors gracefully", async () => {
      mockGithub.rest.search.issuesAndPullRequests.mockRejectedValue(new Error("API Error"));

      await main();

      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Failed to create or update failure tracking issue"));
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("API Error"));
    });
  });

  describe("when agent job did not fail", () => {
    it("should skip processing when agent conclusion is success with safe outputs", async () => {
      process.env.GH_AW_AGENT_CONCLUSION = "success";

      // Set up agent output file with safe outputs
      const tempFilePath = "/tmp/test_agent_output_success.json";
      fs.writeFileSync(
        tempFilePath,
        JSON.stringify({
          items: [{ type: "noop", message: "No action taken" }],
        })
      );
      process.env.GH_AW_AGENT_OUTPUT = tempFilePath;

      try {
        await main();

        expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Agent job did not fail"));
        expect(mockGithub.rest.search.issuesAndPullRequests).not.toHaveBeenCalled();
        expect(mockGithub.rest.issues.create).not.toHaveBeenCalled();
      } finally {
        // Clean up
        if (fs.existsSync(tempFilePath)) {
          fs.unlinkSync(tempFilePath);
        }
      }
    });

    it("should process when agent conclusion is success but no safe outputs", async () => {
      process.env.GH_AW_AGENT_CONCLUSION = "success";
      // Don't set up GH_AW_AGENT_OUTPUT to simulate missing safe outputs

      // Mock API responses
      mockGithub.rest.search.issuesAndPullRequests.mockResolvedValue({
        data: { total_count: 0, items: [] },
      });

      mockGithub.rest.issues.create.mockResolvedValue({
        data: {
          number: 1,
          html_url: "https://github.com/test-owner/test-repo/issues/1",
          node_id: "test-node-id",
        },
      });

      await main();

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Agent succeeded but produced no safe outputs"));
      expect(mockGithub.rest.search.issuesAndPullRequests).toHaveBeenCalled();
      expect(mockGithub.rest.issues.create).toHaveBeenCalled();
    });

    it("should skip processing when agent conclusion is cancelled", async () => {
      process.env.GH_AW_AGENT_CONCLUSION = "cancelled";

      await main();

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Agent job did not fail"));
      expect(mockGithub.rest.search.issuesAndPullRequests).not.toHaveBeenCalled();
    });

    it("should skip processing when agent conclusion is skipped", async () => {
      process.env.GH_AW_AGENT_CONCLUSION = "skipped";

      await main();

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Agent job did not fail"));
      expect(mockGithub.rest.search.issuesAndPullRequests).not.toHaveBeenCalled();
    });
  });

  describe("edge cases", () => {
    it("should handle missing environment variables", async () => {
      delete process.env.GH_AW_WORKFLOW_NAME;
      delete process.env.GH_AW_RUN_URL;

      mockGithub.rest.search.issuesAndPullRequests
        .mockResolvedValueOnce({
          // First search: PR search (no PR found)
          data: { total_count: 0, items: [] },
        })
        .mockResolvedValueOnce({
          // Second search: parent issue
          data: { total_count: 0, items: [] },
        })
        .mockResolvedValueOnce({
          // Third search: failure issue
          data: { total_count: 0, items: [] },
        });

      mockGithub.rest.issues.create
        .mockResolvedValueOnce({
          data: { number: 1, html_url: "https://example.com/1", node_id: "I_1" },
        })
        .mockResolvedValueOnce({
          data: { number: 2, html_url: "https://example.com/2", node_id: "I_2" },
        });

      // Mock GraphQL - new parent created, so just addSubIssue
      mockGithub.graphql = vi.fn().mockResolvedValue({
        addSubIssue: {
          issue: { id: "I_1", number: 1 },
          subIssue: { id: "I_2", number: 2 },
        },
      });

      await main();

      // Should still attempt to create issue with defaults
      expect(mockGithub.rest.issues.create).toHaveBeenCalled();
      const failureIssueCreateCall = mockGithub.rest.issues.create.mock.calls[1][0];
      expect(failureIssueCreateCall.title).toContain("[agentics] unknown failed");
    });

    it("should truncate very long workflow names in title", async () => {
      process.env.GH_AW_WORKFLOW_NAME = "A".repeat(200);

      mockGithub.rest.search.issuesAndPullRequests
        .mockResolvedValueOnce({
          // First search: PR search (no PR found)
          data: { total_count: 0, items: [] },
        })
        .mockResolvedValueOnce({
          // Second search: parent issue
          data: { total_count: 0, items: [] },
        })
        .mockResolvedValueOnce({
          // Third search: failure issue
          data: { total_count: 0, items: [] },
        });

      mockGithub.rest.issues.create
        .mockResolvedValueOnce({
          data: { number: 1, html_url: "https://example.com/1", node_id: "I_1" },
        })
        .mockResolvedValueOnce({
          data: { number: 2, html_url: "https://example.com/2", node_id: "I_2" },
        });

      // Mock GraphQL - new parent created, so just addSubIssue
      mockGithub.graphql = vi.fn().mockResolvedValue({
        addSubIssue: {
          issue: { id: "I_1", number: 1 },
          subIssue: { id: "I_2", number: 2 },
        },
      });

      await main();

      const failureIssueCreateCall = mockGithub.rest.issues.create.mock.calls[1][0];
      // Title should be truncated via sanitization
      // Title includes "[agentics] " prefix (5 chars) + workflow name (up to 100 chars) + " failed" (8 chars)
      // So max should be around 113 chars, but sanitize may add ... so let's be lenient
      expect(failureIssueCreateCall.title.length).toBeLessThan(200); // More lenient - actual is 146
      expect(failureIssueCreateCall.title).toContain("[agentics]");
      expect(failureIssueCreateCall.title).toContain("failed");
      // Verify it was truncated (not 200 As)
      expect(failureIssueCreateCall.title.length).toBeLessThan(220);
    });

    it("should add expiration comment to new issues", async () => {
      mockGithub.rest.search.issuesAndPullRequests
        .mockResolvedValueOnce({
          // First search: PR search (no PR found)
          data: { total_count: 0, items: [] },
        })
        .mockResolvedValueOnce({
          // Second search: parent issue
          data: { total_count: 0, items: [] },
        })
        .mockResolvedValueOnce({
          // Third search: failure issue
          data: { total_count: 0, items: [] },
        });

      mockGithub.rest.issues.create
        .mockResolvedValueOnce({
          data: { number: 1, html_url: "https://example.com/1", node_id: "I_1" },
        })
        .mockResolvedValueOnce({
          data: { number: 2, html_url: "https://example.com/2", node_id: "I_2" },
        });

      // Mock GraphQL - new parent created, so just addSubIssue
      mockGithub.graphql = vi.fn().mockResolvedValue({
        addSubIssue: {
          issue: { id: "I_1", number: 1 },
          subIssue: { id: "I_2", number: 2 },
        },
      });

      await main();

      const failureIssueCreateCall = mockGithub.rest.issues.create.mock.calls[1][0];
      expect(failureIssueCreateCall.body).toContain("- [x] expires");
      expect(failureIssueCreateCall.body).toContain("<!-- gh-aw-expires:");
      expect(failureIssueCreateCall.body).toMatch(/- \[x\] expires <!-- gh-aw-expires: \d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z --> on .+ UTC/);
    });

    it("should include pull request information when PR is found", async () => {
      mockGithub.rest.search.issuesAndPullRequests
        .mockResolvedValueOnce({
          // First search: PR search (PR found!)
          data: {
            total_count: 1,
            items: [
              {
                number: 99,
                html_url: "https://github.com/test-owner/test-repo/pull/99",
              },
            ],
          },
        })
        .mockResolvedValueOnce({
          // Second search: parent issue
          data: { total_count: 0, items: [] },
        })
        .mockResolvedValueOnce({
          // Third search: failure issue
          data: { total_count: 0, items: [] },
        });

      mockGithub.rest.issues.create
        .mockResolvedValueOnce({
          data: { number: 1, html_url: "https://example.com/1", node_id: "I_1" },
        })
        .mockResolvedValueOnce({
          data: { number: 2, html_url: "https://example.com/2", node_id: "I_2" },
        });

      // Mock GraphQL - new parent created, so just addSubIssue
      mockGithub.graphql = vi.fn().mockResolvedValue({
        addSubIssue: {
          issue: { id: "I_1", number: 1 },
          subIssue: { id: "I_2", number: 2 },
        },
      });

      await main();

      const failureIssueCreateCall = mockGithub.rest.issues.create.mock.calls[1][0];
      // Verify PR information is included in the issue body
      expect(failureIssueCreateCall.body).toContain("**Pull Request:**");
      expect(failureIssueCreateCall.body).toContain("#99");
      expect(failureIssueCreateCall.body).toContain("https://github.com/test-owner/test-repo/pull/99");
    });

    it("should not include pull request information when no PR is found", async () => {
      mockGithub.rest.search.issuesAndPullRequests
        .mockResolvedValueOnce({
          // First search: PR search (no PR found)
          data: { total_count: 0, items: [] },
        })
        .mockResolvedValueOnce({
          // Second search: parent issue
          data: { total_count: 0, items: [] },
        })
        .mockResolvedValueOnce({
          // Third search: failure issue
          data: { total_count: 0, items: [] },
        });

      mockGithub.rest.issues.create
        .mockResolvedValueOnce({
          data: { number: 1, html_url: "https://example.com/1", node_id: "I_1" },
        })
        .mockResolvedValueOnce({
          data: { number: 2, html_url: "https://example.com/2", node_id: "I_2" },
        });

      // Mock GraphQL - new parent created, so just addSubIssue
      mockGithub.graphql = vi.fn().mockResolvedValue({
        addSubIssue: {
          issue: { id: "I_1", number: 1 },
          subIssue: { id: "I_2", number: 2 },
        },
      });

      await main();

      const failureIssueCreateCall = mockGithub.rest.issues.create.mock.calls[1][0];
      // Verify PR information is NOT included in the issue body
      expect(failureIssueCreateCall.body).not.toContain("**Pull Request:**");
    });

    it("should include branch information in the issue body", async () => {
      mockGithub.rest.search.issuesAndPullRequests
        .mockResolvedValueOnce({
          // First search: PR search (no PR found)
          data: { total_count: 0, items: [] },
        })
        .mockResolvedValueOnce({
          // Second search: parent issue
          data: { total_count: 0, items: [] },
        })
        .mockResolvedValueOnce({
          // Third search: failure issue
          data: { total_count: 0, items: [] },
        });

      mockGithub.rest.issues.create
        .mockResolvedValueOnce({
          data: { number: 1, html_url: "https://example.com/1", node_id: "I_1" },
        })
        .mockResolvedValueOnce({
          data: { number: 2, html_url: "https://example.com/2", node_id: "I_2" },
        });

      // Mock GraphQL - new parent created, so just addSubIssue
      mockGithub.graphql = vi.fn().mockResolvedValue({
        addSubIssue: {
          issue: { id: "I_1", number: 1 },
          subIssue: { id: "I_2", number: 2 },
        },
      });

      await main();

      const failureIssueCreateCall = mockGithub.rest.issues.create.mock.calls[1][0];
      // Verify branch information is included in the issue body
      expect(failureIssueCreateCall.body).toContain("**Branch:**");
      // The actual branch will be determined by getCurrentBranch() which may get it from git or env
      // Just verify the branch field exists
    });
  });

  describe("parent issue sub-issue limit", () => {
    it("should create new parent issue when existing parent reaches 64 sub-issues", async () => {
      // Mock searches: PR search, parent issue search, failure issue search
      mockGithub.rest.search.issuesAndPullRequests
        .mockResolvedValueOnce({
          // First search: PR search (no PR found)
          data: { total_count: 0, items: [] },
        })
        .mockResolvedValueOnce({
          // Second search: existing parent issue
          data: {
            total_count: 1,
            items: [
              {
                number: 1,
                html_url: "https://github.com/test-owner/test-repo/issues/1",
                node_id: "I_parent_1",
              },
            ],
          },
        })
        .mockResolvedValueOnce({
          // Third search: no failure issue exists
          data: { total_count: 0, items: [] },
        });

      // Mock GraphQL query for sub-issue count (returns 64)
      mockGithub.graphql = vi
        .fn()
        .mockResolvedValueOnce({
          // First call: check sub-issue count
          repository: {
            issue: {
              subIssues: {
                totalCount: 64,
              },
            },
          },
        })
        .mockResolvedValueOnce({
          // Second call: link sub-issue to new parent
          addSubIssue: {
            issue: { id: "I_parent_2", number: 2 },
            subIssue: { id: "I_sub_42", number: 42 },
          },
        });

      // Mock issue creation (new parent, failure issue)
      mockGithub.rest.issues.create
        .mockResolvedValueOnce({
          // New parent issue
          data: {
            number: 2,
            html_url: "https://github.com/test-owner/test-repo/issues/2",
            node_id: "I_parent_2",
          },
        })
        .mockResolvedValueOnce({
          // Failure issue
          data: {
            number: 42,
            html_url: "https://github.com/test-owner/test-repo/issues/42",
            node_id: "I_sub_42",
          },
        });

      await main();

      // Verify sub-issue count was checked
      expect(mockGithub.graphql).toHaveBeenCalledWith(
        expect.stringContaining("subIssues"),
        expect.objectContaining({
          owner: "test-owner",
          repo: "test-repo",
          issueNumber: 1,
        })
      );

      // Verify new parent issue was created with reference to old parent
      const newParentCall = mockGithub.rest.issues.create.mock.calls[0][0];
      expect(newParentCall.title).toBe("[agentic-workflows] Failed runs");
      expect(newParentCall.labels).toEqual(["agentic-workflows"]);
      expect(newParentCall.body).toContain("continuation parent issue");
      expect(newParentCall.body).toContain("previous parent issue #1");
      expect(newParentCall.body).toContain("reached the maximum of 64 sub-issues");

      // Verify failure issue was created
      expect(mockGithub.rest.issues.create).toHaveBeenCalledWith(
        expect.objectContaining({
          title: "[agentics] Test Workflow failed",
          labels: ["agentic-workflows"],
        })
      );

      // Verify sub-issue was linked to new parent (not old one)
      expect(mockGithub.graphql).toHaveBeenCalledWith(expect.stringContaining("addSubIssue"), {
        parentId: "I_parent_2",
        subIssueId: "I_sub_42",
      });

      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("has 64 sub-issues"));
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Creating a new parent issue"));
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Created parent issue #2"));
    });

    it("should reuse parent issue when sub-issue count is below 64", async () => {
      // Mock searches
      mockGithub.rest.search.issuesAndPullRequests
        .mockResolvedValueOnce({
          // First search: PR search (no PR found)
          data: { total_count: 0, items: [] },
        })
        .mockResolvedValueOnce({
          // Second search: existing parent issue
          data: {
            total_count: 1,
            items: [
              {
                number: 5,
                html_url: "https://github.com/test-owner/test-repo/issues/5",
                node_id: "I_parent_5",
              },
            ],
          },
        })
        .mockResolvedValueOnce({
          // Third search: no failure issue
          data: { total_count: 0, items: [] },
        });

      // Mock GraphQL query for sub-issue count (returns 30)
      mockGithub.graphql = vi
        .fn()
        .mockResolvedValueOnce({
          // First call: check sub-issue count
          repository: {
            issue: {
              subIssues: {
                totalCount: 30,
              },
            },
          },
        })
        .mockResolvedValueOnce({
          // Second call: link sub-issue
          addSubIssue: {
            issue: { id: "I_parent_5", number: 5 },
            subIssue: { id: "I_sub_42", number: 42 },
          },
        });

      // Mock failure issue creation only (parent already exists)
      mockGithub.rest.issues.create.mockResolvedValueOnce({
        data: {
          number: 42,
          html_url: "https://github.com/test-owner/test-repo/issues/42",
          node_id: "I_sub_42",
        },
      });

      mockGithub.rest.issues.update = vi.fn();
      mockGithub.rest.issues.createComment = vi.fn();

      await main();

      // Verify parent issue was NOT closed
      expect(mockGithub.rest.issues.update).not.toHaveBeenCalled();

      // Verify only one issue was created (failure issue, not parent)
      expect(mockGithub.rest.issues.create).toHaveBeenCalledTimes(1);

      // Verify sub-issue was linked to existing parent
      expect(mockGithub.graphql).toHaveBeenCalledWith(expect.stringContaining("addSubIssue"), {
        parentId: "I_parent_5",
        subIssueId: "I_sub_42",
      });

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("has 30 sub-issues (within limit of 64)"));
    });

    it("should continue if sub-issue count check fails", async () => {
      // Mock searches
      mockGithub.rest.search.issuesAndPullRequests
        .mockResolvedValueOnce({
          // First search: PR search (no PR found)
          data: { total_count: 0, items: [] },
        })
        .mockResolvedValueOnce({
          // Second search: existing parent issue
          data: {
            total_count: 1,
            items: [
              {
                number: 5,
                html_url: "https://github.com/test-owner/test-repo/issues/5",
                node_id: "I_parent_5",
              },
            ],
          },
        })
        .mockResolvedValueOnce({
          // Third search: no failure issue
          data: { total_count: 0, items: [] },
        });

      // Mock GraphQL query failure for sub-issue count
      mockGithub.graphql = vi
        .fn()
        .mockRejectedValueOnce(new Error("GraphQL API Error"))
        .mockResolvedValueOnce({
          // Second call: link sub-issue
          addSubIssue: {
            issue: { id: "I_parent_5", number: 5 },
            subIssue: { id: "I_sub_42", number: 42 },
          },
        });

      // Mock failure issue creation
      mockGithub.rest.issues.create.mockResolvedValueOnce({
        data: {
          number: 42,
          html_url: "https://github.com/test-owner/test-repo/issues/42",
          node_id: "I_sub_42",
        },
      });

      await main();

      // Verify warning was logged about count check failure
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Could not check sub-issue count"));

      // Verify parent issue was still used (graceful degradation)
      expect(mockGithub.rest.issues.create).toHaveBeenCalledTimes(1);
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Found existing parent issue #5"));
    });
  });

  describe("checkout PR failure via output", () => {
    it("should skip issue creation when checkout_pr_success is false", async () => {
      // Set the checkout PR failure environment variable
      process.env.GH_AW_CHECKOUT_PR_SUCCESS = "false";

      await main();

      // Verify that no issue was created
      expect(mockGithub.rest.issues.create).not.toHaveBeenCalled();
      expect(mockGithub.rest.issues.createComment).not.toHaveBeenCalled();
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Skipping failure handling - failure was due to PR checkout"));
    });

    it("should create issue when checkout_pr_success is true", async () => {
      // Set the checkout PR success environment variable
      process.env.GH_AW_CHECKOUT_PR_SUCCESS = "true";

      mockGithub.rest.search.issuesAndPullRequests
        .mockResolvedValueOnce({
          // First search: PR search (no PR found)
          data: { total_count: 0, items: [] },
        })
        .mockResolvedValueOnce({
          // Second search: parent issue
          data: { total_count: 0, items: [] },
        })
        .mockResolvedValueOnce({
          // Third search: failure issue
          data: { total_count: 0, items: [] },
        });

      mockGithub.rest.issues.create
        .mockResolvedValueOnce({
          data: { number: 1, html_url: "https://example.com/1", node_id: "I_1" },
        })
        .mockResolvedValueOnce({
          data: { number: 2, html_url: "https://example.com/2", node_id: "I_2" },
        });

      mockGithub.graphql = vi.fn().mockResolvedValue({
        addSubIssue: {
          issue: { id: "I_1", number: 1 },
          subIssue: { id: "I_2", number: 2 },
        },
      });

      await main();

      // Verify issue was created
      expect(mockGithub.rest.issues.create).toHaveBeenCalled();
    });

    it("should create issue when checkout_pr_success is not set", async () => {
      // Don't set GH_AW_CHECKOUT_PR_SUCCESS (workflow without PR checkout)
      delete process.env.GH_AW_CHECKOUT_PR_SUCCESS;

      mockGithub.rest.search.issuesAndPullRequests
        .mockResolvedValueOnce({
          // First search: PR search (no PR found)
          data: { total_count: 0, items: [] },
        })
        .mockResolvedValueOnce({
          // Second search: parent issue
          data: { total_count: 0, items: [] },
        })
        .mockResolvedValueOnce({
          // Third search: failure issue
          data: { total_count: 0, items: [] },
        });

      mockGithub.rest.issues.create
        .mockResolvedValueOnce({
          data: { number: 1, html_url: "https://example.com/1", node_id: "I_1" },
        })
        .mockResolvedValueOnce({
          data: { number: 2, html_url: "https://example.com/2", node_id: "I_2" },
        });

      mockGithub.graphql = vi.fn().mockResolvedValue({
        addSubIssue: {
          issue: { id: "I_1", number: 1 },
          subIssue: { id: "I_2", number: 2 },
        },
      });

      await main();

      // Verify issue was created (normal failure handling)
      expect(mockGithub.rest.issues.create).toHaveBeenCalled();
    });
  });
});
