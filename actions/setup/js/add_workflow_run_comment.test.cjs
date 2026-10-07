// @ts-check
import { describe, it, expect, beforeEach, vi } from "vitest";
const { ERR_NOT_FOUND, ERR_VALIDATION } = require("./error_codes.cjs");

// Mock the global objects that GitHub Actions provides
const mockCore = {
  debug: vi.fn(),
  info: vi.fn(),
  warning: vi.fn(),
  error: vi.fn(),
  setFailed: vi.fn(),
  setOutput: vi.fn(),
};

const mockGithub = {
  request: vi.fn(),
  graphql: vi.fn(),
};

const mockContext = {
  eventName: "issues",
  runId: 12345,
  repo: {
    owner: "testowner",
    repo: "testrepo",
  },
  payload: {
    issue: {
      number: 123,
    },
    repository: {
      html_url: "https://github.com/testowner/testrepo",
    },
  },
};

// Set up global mocks before importing the module
global.core = mockCore;
global.github = mockGithub;
global.context = mockContext;

describe("add_workflow_run_comment", () => {
  let importCounter = 0;

  beforeEach(() => {
    // Reset all mocks before each test
    vi.clearAllMocks();
    vi.resetModules();

    // Reset environment variables
    delete process.env.GH_AW_WORKFLOW_NAME;
    delete process.env.GITHUB_WORKFLOW;
    delete process.env.GH_AW_TRACKER_ID;
    delete process.env.GH_AW_LOCK_FOR_AGENT;
    delete process.env.GH_AW_WORKFLOW_EMOJI;
    delete process.env.GITHUB_SERVER_URL;
    delete process.env.GH_AW_SAFE_OUTPUT_MESSAGES;

    // Reset context to default
    global.context = {
      eventName: "issues",
      runId: 12345,
      repo: {
        owner: "testowner",
        repo: "testrepo",
      },
      payload: {
        issue: {
          number: 123,
        },
        repository: {
          html_url: "https://github.com/testowner/testrepo",
        },
      },
    };

    // Reset default mock implementations
    mockGithub.request.mockResolvedValue({
      data: {
        id: 67890,
        html_url: "https://github.com/testowner/testrepo/issues/123#issuecomment-67890",
      },
    });

    mockGithub.graphql.mockResolvedValue({
      repository: {
        discussion: {
          id: "D_kwDOTest123",
        },
      },
      addDiscussionComment: {
        comment: {
          id: "DC_kwDOTest456",
          url: "https://github.com/testowner/testrepo/discussions/10#discussioncomment-456",
        },
      },
    });
  });

  async function importAddWorkflowRunComment() {
    importCounter += 1;
    return import("./add_workflow_run_comment.cjs?test=" + importCounter);
  }

  it.each([0, 1, 2])("keeps trusted activation reporting on the native triggering issue with %i queue Claims", async count => {
    const assignment = {
      version: 3,
      dispatch_id: "d1",
      request_id: "r1",
      commit_id: "commit1",
      policy_epoch: "e1",
      pool: "default",
      worker_profile: "worker",
      claims: Array.from({ length: count }, (_, index) => ({ handle: `h${index + 1}`, claim_id: `c${index + 1}`, work_id: `w${index + 1}`, work: { issue_number: 999 }, result_refs: [] })),
    };
    vi.stubEnv("GH_AW_WORK_QUEUE_ENABLED", count ? "true" : "false");
    vi.stubEnv("GH_AW_WORK_QUEUE_ROLE", "worker");
    global.context.payload.inputs = { work_queue_assignment: count ? JSON.stringify(assignment) : "stray malformed assignment" };
    try {
      const { main } = await importAddWorkflowRunComment();
      await main();
      expect(mockGithub.request).toHaveBeenCalledOnce();
      expect(mockGithub.request).toHaveBeenCalledWith(
        "POST /repos/{owner}/{repo}/issues/{issue_number}/comments",
        expect.objectContaining({ owner: "testowner", repo: "testrepo", issue_number: 123, body: expect.stringContaining("/actions/runs/12345") })
      );
      expect(mockCore.setOutput).toHaveBeenCalledWith("comment-id", "67890");
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    } finally {
      vi.unstubAllEnvs();
    }
  });

  describe("discussion endpoint validation", () => {
    it("rejects malformed and non-positive discussion endpoint numbers", async () => {
      const { parseDiscussionEndpoint } = await importAddWorkflowRunComment();

      for (const endpoint of ["discussion:12junk", "discussion:0", "discussion:12:extra"]) {
        expect(() => parseDiscussionEndpoint(endpoint, "discussion")).toThrow("Invalid discussion endpoint");
      }
      expect(() => parseDiscussionEndpoint("discussion_comment:5junk:2", "discussion_comment")).toThrow("Invalid discussion endpoint");
    });
  });

  // Helper function to run the script
  async function runScript() {
    const { main } = await importAddWorkflowRunComment();
    await main();
  }

  async function runCreateOrReuseStatusComment(rawContext) {
    const { createOrReuseStatusComment } = await importAddWorkflowRunComment();
    return createOrReuseStatusComment(rawContext);
  }

  // Helper function to run addCommentWithWorkflowLink
  async function runAddCommentWithWorkflowLink(endpoint, runUrl, eventName) {
    const { addCommentWithWorkflowLink } = await importAddWorkflowRunComment();
    await addCommentWithWorkflowLink(endpoint, runUrl, eventName);
  }

  // Typed route endpoint used across addCommentWithWorkflowLink tests
  const issuesCommentEndpoint123 = { route: "POST /repos/{owner}/{repo}/issues/{issue_number}/comments", params: { owner: "testowner", repo: "testrepo", issue_number: 123 } };
  const issuesCommentEndpoint101 = { route: "POST /repos/{owner}/{repo}/issues/{issue_number}/comments", params: { owner: "testowner", repo: "testrepo", issue_number: 101 } };

  describe("main() - issues event", () => {
    it("should create comment on an issue", async () => {
      global.context = {
        eventName: "issues",
        runId: 12345,
        repo: { owner: "testowner", repo: "testrepo" },
        payload: {
          issue: { number: 456 },
          repository: { html_url: "https://github.com/testowner/testrepo" },
        },
      };

      await runScript();

      expect(mockGithub.request).toHaveBeenCalledWith(
        "POST /repos/{owner}/{repo}/issues/{issue_number}/comments",
        expect.objectContaining({
          body: expect.stringContaining("https://github.com/testowner/testrepo/actions/runs/12345"),
        })
      );
      expect(mockCore.setOutput).toHaveBeenCalledWith("comment-id", "67890");
      expect(mockCore.setOutput).toHaveBeenCalledWith("comment-url", expect.stringContaining("issuecomment-67890"));
      expect(mockCore.setOutput).toHaveBeenCalledWith("comment-repo", "testowner/testrepo");
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should fail when issue number is missing", async () => {
      global.context = {
        eventName: "issues",
        runId: 12345,
        repo: { owner: "testowner", repo: "testrepo" },
        payload: {},
      };

      await runScript();

      expect(mockCore.setFailed).toHaveBeenCalledWith(`${ERR_NOT_FOUND}: Issue number not found in event payload`);
      expect(mockGithub.request).not.toHaveBeenCalled();
    });
  });

  describe("main() - issue_comment event", () => {
    it("should create comment on the issue", async () => {
      global.context = {
        eventName: "issue_comment",
        runId: 12345,
        repo: { owner: "testowner", repo: "testrepo" },
        payload: {
          issue: { number: 789 },
          repository: { html_url: "https://github.com/testowner/testrepo" },
        },
      };

      await runScript();

      expect(mockGithub.request).toHaveBeenCalledWith(
        "POST /repos/{owner}/{repo}/issues/{issue_number}/comments",
        expect.objectContaining({
          body: expect.any(String),
        })
      );
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });
  });

  describe("main() - repository_dispatch event", () => {
    it("should use workflow repo for run URL and client payload repo for comments", async () => {
      global.context = {
        eventName: "repository_dispatch",
        runId: 12345,
        repo: { owner: "sideowner", repo: "siderepo" },
        payload: {
          action: "issue_comment",
          client_payload: {
            issue: { number: 789 },
            repository: { owner: { login: "targetowner" }, name: "targetrepo" },
          },
        },
      };

      await runScript();

      expect(mockGithub.request).toHaveBeenCalledWith(
        "POST /repos/{owner}/{repo}/issues/{issue_number}/comments",
        expect.objectContaining({
          body: expect.stringContaining("https://github.com/sideowner/siderepo/actions/runs/12345"),
        })
      );
      expect(mockCore.setOutput).toHaveBeenCalledWith("comment-repo", "targetowner/targetrepo");
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("reuses an existing status comment from client_payload aw_context", async () => {
      mockGithub.request.mockResolvedValueOnce({
        data: {
          id: 67890,
          html_url: "https://github.com/statusowner/statusrepo/issues/789#issuecomment-67890",
        },
      });
      global.context = {
        eventName: "repository_dispatch",
        runId: 12345,
        repo: { owner: "workflowowner", repo: "workflowrepo" },
        payload: {
          action: "issue_comment",
          client_payload: {
            aw_context: JSON.stringify({
              repo: "targetowner/targetrepo",
              event_type: "issue_comment",
              item_type: "issue",
              item_number: 789,
              status_comment_id: 67890,
              status_comment_url: "https://github.com/targetowner/targetrepo/issues/789#issuecomment-67890",
              status_comment_repo: "statusowner/statusrepo",
            }),
          },
        },
      };

      await runScript();

      expect(mockGithub.request).toHaveBeenCalledWith(
        "PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}",
        expect.objectContaining({
          owner: "statusowner",
          repo: "statusrepo",
          comment_id: 67890,
          body: expect.stringContaining("https://github.com/workflowowner/workflowrepo/actions/runs/12345"),
        })
      );
      expect(mockCore.setOutput).toHaveBeenCalledWith("comment-id", "67890");
      expect(mockCore.setOutput).toHaveBeenCalledWith("comment-url", "https://github.com/statusowner/statusrepo/issues/789#issuecomment-67890");
      expect(mockCore.setOutput).toHaveBeenCalledWith("comment-repo", "statusowner/statusrepo");
      expect(mockCore.info).toHaveBeenCalledWith("Reusing existing status comment outputs");
    });
  });

  describe("main() - workflow_dispatch aw_context reuse", () => {
    it("reuses an existing status comment from aw_context", async () => {
      mockGithub.request.mockResolvedValueOnce({
        data: {
          id: 67890,
          html_url: "https://github.com/targetowner/targetrepo/issues/789#issuecomment-67890",
        },
      });
      global.context = {
        eventName: "workflow_dispatch",
        runId: 12345,
        repo: { owner: "workflowowner", repo: "workflowrepo" },
        payload: {
          inputs: {
            aw_context: JSON.stringify({
              repo: "targetowner/targetrepo",
              event_type: "issue_comment",
              item_type: "issue",
              item_number: 789,
              status_comment_id: 67890,
              status_comment_url: "https://github.com/targetowner/targetrepo/issues/789#issuecomment-67890",
            }),
          },
        },
      };

      await runScript();

      expect(mockGithub.request).toHaveBeenCalledWith(
        "PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}",
        expect.objectContaining({
          owner: "targetowner",
          repo: "targetrepo",
          comment_id: 67890,
          body: expect.stringContaining("https://github.com/workflowowner/workflowrepo/actions/runs/12345"),
        })
      );
      expect(mockCore.setOutput).toHaveBeenCalledWith("comment-id", "67890");
      expect(mockCore.setOutput).toHaveBeenCalledWith("comment-url", "https://github.com/targetowner/targetrepo/issues/789#issuecomment-67890");
      expect(mockCore.setOutput).toHaveBeenCalledWith("comment-repo", "targetowner/targetrepo");
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("reuses an existing status comment from camelCase awContext", async () => {
      mockGithub.request.mockResolvedValueOnce({
        data: {
          id: 67890,
          html_url: "https://github.com/statusowner/statusrepo/issues/789#issuecomment-67890",
        },
      });
      global.context = {
        eventName: "workflow_dispatch",
        runId: 12345,
        repo: { owner: "workflowowner", repo: "workflowrepo" },
        payload: {
          inputs: {
            awContext: JSON.stringify({
              repo: "targetowner/targetrepo",
              event_type: "issue_comment",
              item_type: "issue",
              item_number: 789,
              statusCommentId: 67890,
              statusCommentUrl: "https://github.com/targetowner/targetrepo/issues/789#issuecomment-67890",
              statusCommentRepo: "statusowner/statusrepo",
            }),
          },
        },
      };

      await runScript();

      expect(mockGithub.request).toHaveBeenCalledWith(
        "PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}",
        expect.objectContaining({
          owner: "statusowner",
          repo: "statusrepo",
          comment_id: 67890,
          body: expect.stringContaining("https://github.com/workflowowner/workflowrepo/actions/runs/12345"),
        })
      );
      expect(mockCore.setOutput).toHaveBeenCalledWith("comment-id", "67890");
      expect(mockCore.setOutput).toHaveBeenCalledWith("comment-url", "https://github.com/statusowner/statusrepo/issues/789#issuecomment-67890");
      expect(mockCore.setOutput).toHaveBeenCalledWith("comment-repo", "statusowner/statusrepo");
      expect(mockCore.info).toHaveBeenCalledWith("Reusing existing status comment outputs");
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("uses dispatched workflow metadata from aw_context when provided", async () => {
      global.context = {
        eventName: "workflow_dispatch",
        runId: 12345,
        repo: { owner: "workflowowner", repo: "workflowrepo" },
        payload: {
          inputs: {
            aw_context: JSON.stringify({
              repo: "targetowner/targetrepo",
              event_type: "issue_comment",
              item_type: "issue",
              item_number: 789,
              status_comment_id: 67890,
              status_comment_url: "https://github.com/targetowner/targetrepo/issues/789#issuecomment-67890",
              dispatched_workflow_name: "archie",
              dispatched_run_url: "https://github.com/github/gh-aw/actions/runs/444",
            }),
          },
        },
      };

      await runScript();

      expect(mockGithub.request).toHaveBeenCalledWith(
        "PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}",
        expect.objectContaining({
          comment_id: 67890,
          body: expect.stringContaining("[archie](https://github.com/github/gh-aw/actions/runs/444)"),
        })
      );
    });

    it("updates reusable centralized slash-command discussion comments via GraphQL", async () => {
      mockGithub.graphql.mockResolvedValue({
        updateDiscussionComment: {
          comment: {
            id: "DC_kwDOReusable123",
            url: "https://github.com/targetowner/targetrepo/discussions/789#discussioncomment-67890",
          },
        },
      });
      global.context = {
        eventName: "workflow_dispatch",
        runId: 12345,
        repo: { owner: "workflowowner", repo: "workflowrepo" },
        payload: {
          inputs: {
            aw_context: JSON.stringify({
              repo: "targetowner/targetrepo",
              event_type: "discussion_comment",
              item_type: "discussion",
              item_number: 789,
              command_name: "plan",
              status_comment_id: "DC_kwDOReusable123",
              status_comment_url: "https://github.com/targetowner/targetrepo/discussions/789#discussioncomment-67890",
              status_comment_repo: "statusowner/statusrepo",
            }),
          },
        },
      };

      await runScript();

      expect(mockGithub.graphql).toHaveBeenCalledWith(
        expect.stringContaining("updateDiscussionComment"),
        expect.objectContaining({
          commentId: "DC_kwDOReusable123",
          body: expect.stringContaining("https://github.com/workflowowner/workflowrepo/actions/runs/12345"),
        })
      );
      expect(mockGithub.request).not.toHaveBeenCalled();
      expect(mockCore.setOutput).toHaveBeenCalledWith("comment-id", "DC_kwDOReusable123");
      expect(mockCore.setOutput).toHaveBeenCalledWith("comment-url", "https://github.com/targetowner/targetrepo/discussions/789#discussioncomment-67890");
      expect(mockCore.setOutput).toHaveBeenCalledWith("comment-repo", "statusowner/statusrepo");
      expect(mockCore.info).toHaveBeenCalledWith("Updated reusable status comment with current workflow run metadata");
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("updates reusable discussion comments via GraphQL", async () => {
      mockGithub.graphql.mockResolvedValue({
        updateDiscussionComment: {
          comment: {
            id: "DC_kwDOReusable123",
            url: "https://github.com/targetowner/targetrepo/discussions/789#discussioncomment-67890",
          },
        },
      });
      global.context = {
        eventName: "workflow_dispatch",
        runId: 12345,
        repo: { owner: "workflowowner", repo: "workflowrepo" },
        payload: {
          inputs: {
            aw_context: JSON.stringify({
              repo: "targetowner/targetrepo",
              event_type: "discussion_comment",
              item_type: "discussion",
              item_number: 789,
              status_comment_id: "DC_kwDOReusable123",
            }),
          },
        },
      };

      await runScript();

      expect(mockGithub.graphql).toHaveBeenCalledWith(
        expect.stringContaining("updateDiscussionComment"),
        expect.objectContaining({
          commentId: "DC_kwDOReusable123",
          body: expect.stringContaining("https://github.com/workflowowner/workflowrepo/actions/runs/12345"),
        })
      );
      expect(mockGithub.request).not.toHaveBeenCalled();
      expect(mockCore.setOutput).toHaveBeenCalledWith("comment-id", "DC_kwDOReusable123");
      expect(mockCore.setOutput).toHaveBeenCalledWith("comment-url", "https://github.com/targetowner/targetrepo/discussions/789#discussioncomment-67890");
    });

    it("warns and falls back to stale URL when reusable issue-comment update fails", async () => {
      mockGithub.request.mockRejectedValueOnce(new Error("network timeout"));
      global.context = {
        eventName: "workflow_dispatch",
        runId: 12345,
        repo: { owner: "workflowowner", repo: "workflowrepo" },
        payload: {
          inputs: {
            aw_context: JSON.stringify({
              repo: "targetowner/targetrepo",
              event_type: "issue_comment",
              item_type: "issue",
              item_number: 789,
              status_comment_id: 67890,
              status_comment_url: "https://github.com/targetowner/targetrepo/issues/789#issuecomment-67890",
            }),
          },
        },
      };

      await runScript();

      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Failed to update reusable status comment body"));
      expect(mockCore.setOutput).toHaveBeenCalledWith("comment-url", "https://github.com/targetowner/targetrepo/issues/789#issuecomment-67890");
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("warns when reusable comment ID is not a positive integer", async () => {
      global.context = {
        eventName: "workflow_dispatch",
        runId: 12345,
        repo: { owner: "workflowowner", repo: "workflowrepo" },
        payload: {
          inputs: {
            aw_context: JSON.stringify({
              repo: "targetowner/targetrepo",
              event_type: "issue_comment",
              item_type: "issue",
              item_number: 789,
              status_comment_id: "67890abc",
              status_comment_url: "https://github.com/targetowner/targetrepo/issues/789#issuecomment-67890",
            }),
          },
        },
      };

      await runScript();

      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining(`${ERR_VALIDATION}: Reusable status comment ID must be a positive integer (received "67890abc")`));
      expect(mockCore.setOutput).toHaveBeenCalledWith("comment-url", "https://github.com/targetowner/targetrepo/issues/789#issuecomment-67890");
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });
  });

  describe("createOrReuseStatusComment()", () => {
    it("should skip comment when activationComments is false", async () => {
      process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({ activationComments: false });

      const result = await runCreateOrReuseStatusComment(global.context);

      expect(result).toBeNull();
      expect(mockGithub.request).not.toHaveBeenCalled();
      expect(mockCore.info).toHaveBeenCalledWith("activation-comments is disabled: skipping activation comment creation");
    });

    it("creates a pull request comment for pull_request_comment events", async () => {
      const result = await runCreateOrReuseStatusComment({
        eventName: "workflow_dispatch",
        runId: 12345,
        repo: { owner: "workflowowner", repo: "workflowrepo" },
        payload: {
          inputs: {
            aw_context: JSON.stringify({
              repo: "targetowner/targetrepo",
              event_type: "pull_request_comment",
              item_type: "pull_request",
              item_number: 101,
            }),
          },
        },
      });

      expect(result?.id).toBe("67890");
      expect(mockGithub.request).toHaveBeenCalledWith(
        "POST /repos/{owner}/{repo}/issues/{issue_number}/comments",
        expect.objectContaining({
          body: expect.stringContaining("has started processing this pull request comment"),
        })
      );
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("uses GITHUB_REPOSITORY for run URL when context is spread (repo getter lost)", async () => {
      // When route_slash_command.cjs does {...context, nonFatalStatusCommentErrors: true},
      // the context.repo prototype getter is not included in the spread.
      // The run URL must fall back to GITHUB_REPOSITORY env var.
      process.env.GITHUB_REPOSITORY = "central-owner/central-repo";
      process.env.GITHUB_SERVER_URL = "https://github.com";

      // Simulate a spread context: repo is absent (lost getter), but payload has the event repo
      const result = await runCreateOrReuseStatusComment({
        eventName: "issue_comment",
        runId: 27971614768,
        // no `repo` property - simulates lost prototype getter via {...context}
        payload: {
          issue: { number: 40795 },
          repository: { owner: { login: "github" }, name: "gh-aw" },
        },
        nonFatalStatusCommentErrors: true,
      });

      expect(result?.id).toBe("67890");
      expect(mockGithub.request).toHaveBeenCalledWith(
        expect.stringContaining("POST"),
        expect.objectContaining({
          body: expect.stringContaining("https://github.com/central-owner/central-repo/actions/runs/27971614768"),
        })
      );
    });
  });

  describe("main() - pull_request event", () => {
    it("should create comment on a pull request", async () => {
      global.context = {
        eventName: "pull_request",
        runId: 12345,
        repo: { owner: "testowner", repo: "testrepo" },
        payload: {
          pull_request: { number: 101 },
          repository: { html_url: "https://github.com/testowner/testrepo" },
        },
      };

      await runScript();

      expect(mockGithub.request).toHaveBeenCalledWith(
        "POST /repos/{owner}/{repo}/issues/{issue_number}/comments",
        expect.objectContaining({
          body: expect.any(String),
        })
      );
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should fail when PR number is missing", async () => {
      global.context = {
        eventName: "pull_request",
        runId: 12345,
        repo: { owner: "testowner", repo: "testrepo" },
        payload: {},
      };

      await runScript();

      expect(mockCore.setFailed).toHaveBeenCalledWith(`${ERR_NOT_FOUND}: Pull request number not found in event payload`);
      expect(mockGithub.request).not.toHaveBeenCalled();
    });
  });

  describe("main() - pull_request_review_comment event", () => {
    it("should create comment on the pull request", async () => {
      global.context = {
        eventName: "pull_request_review_comment",
        runId: 12345,
        repo: { owner: "testowner", repo: "testrepo" },
        payload: {
          pull_request: { number: 202 },
          repository: { html_url: "https://github.com/testowner/testrepo" },
        },
      };

      await runScript();

      expect(mockGithub.request).toHaveBeenCalledWith(
        "POST /repos/{owner}/{repo}/issues/{issue_number}/comments",
        expect.objectContaining({
          body: expect.any(String),
        })
      );
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should fail when PR number is missing in pull_request_review_comment event", async () => {
      global.context = {
        eventName: "pull_request_review_comment",
        runId: 12345,
        repo: { owner: "testowner", repo: "testrepo" },
        payload: {},
      };

      await runScript();

      expect(mockCore.setFailed).toHaveBeenCalledWith(`${ERR_NOT_FOUND}: Pull request number not found in event payload`);
      expect(mockGithub.request).not.toHaveBeenCalled();
    });
  });

  describe("main() - pull_request_review event", () => {
    it("should create comment on the pull request", async () => {
      global.context = {
        eventName: "pull_request_review",
        runId: 12345,
        repo: { owner: "testowner", repo: "testrepo" },
        payload: {
          pull_request: { number: 303 },
          repository: { html_url: "https://github.com/testowner/testrepo" },
        },
      };

      await runScript();

      expect(mockGithub.request).toHaveBeenCalledWith(
        "POST /repos/{owner}/{repo}/issues/{issue_number}/comments",
        expect.objectContaining({
          body: expect.any(String),
        })
      );
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should fail when PR number is missing in pull_request_review event", async () => {
      global.context = {
        eventName: "pull_request_review",
        runId: 12345,
        repo: { owner: "testowner", repo: "testrepo" },
        payload: {},
      };

      await runScript();

      expect(mockCore.setFailed).toHaveBeenCalledWith(`${ERR_NOT_FOUND}: Pull request number not found in event payload`);
      expect(mockGithub.request).not.toHaveBeenCalled();
    });
  });

  describe("main() - discussion event", () => {
    it("should create GraphQL comment on a discussion", async () => {
      global.context = {
        eventName: "discussion",
        runId: 12345,
        repo: { owner: "testowner", repo: "testrepo" },
        payload: {
          discussion: { number: 10 },
          repository: { html_url: "https://github.com/testowner/testrepo" },
        },
      };

      await runScript();

      expect(mockGithub.graphql).toHaveBeenCalled();
      expect(mockCore.setOutput).toHaveBeenCalledWith("comment-id", "DC_kwDOTest456");
      expect(mockCore.setOutput).toHaveBeenCalledWith("comment-url", expect.stringContaining("discussioncomment-456"));
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should fail when discussion number is missing", async () => {
      global.context = {
        eventName: "discussion",
        runId: 12345,
        repo: { owner: "testowner", repo: "testrepo" },
        payload: {},
      };

      await runScript();

      expect(mockCore.setFailed).toHaveBeenCalledWith(`${ERR_NOT_FOUND}: Discussion number not found in event payload`);
    });
  });

  describe("main() - discussion_comment event", () => {
    it("should create threaded comment on a discussion with replyToId", async () => {
      global.context = {
        eventName: "discussion_comment",
        runId: 12345,
        repo: { owner: "testowner", repo: "testrepo" },
        payload: {
          discussion: { number: 15 },
          comment: { id: 999, node_id: "DC_kwDOOriginal" },
          repository: { html_url: "https://github.com/testowner/testrepo" },
        },
      };

      await runScript();

      expect(mockGithub.graphql).toHaveBeenCalled();
      const graphqlCalls = mockGithub.graphql.mock.calls;
      // Find the mutation call (second call)
      const mutationCall = graphqlCalls.find(call => call[0].includes("addDiscussionComment"));
      expect(mutationCall).toBeDefined();
      expect(mutationCall[1]).toMatchObject({
        replyToId: "DC_kwDOOriginal",
      });
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should fail when discussion or comment fields are missing", async () => {
      global.context = {
        eventName: "discussion_comment",
        runId: 12345,
        repo: { owner: "testowner", repo: "testrepo" },
        payload: {
          discussion: { number: 15 },
          // Missing comment field
        },
      };

      await runScript();

      expect(mockCore.setFailed).toHaveBeenCalledWith(`${ERR_NOT_FOUND}: Discussion or comment information not found in event payload`);
    });
  });

  describe("main() - unsupported event types", () => {
    it("should fail for unsupported event type", async () => {
      global.context = {
        eventName: "unsupported_event",
        runId: 12345,
        repo: { owner: "testowner", repo: "testrepo" },
        payload: {},
      };

      await runScript();

      expect(mockCore.setFailed).toHaveBeenCalledWith(`${ERR_VALIDATION}: Unsupported event type: unsupported_event`);
      expect(mockGithub.request).not.toHaveBeenCalled();
    });
  });

  describe("main() - API errors", () => {
    it("should warn but not fail on API error", async () => {
      mockGithub.request.mockRejectedValueOnce(new Error("API Error"));

      global.context = {
        eventName: "issues",
        runId: 12345,
        repo: { owner: "testowner", repo: "testrepo" },
        payload: {
          issue: { number: 456 },
          repository: { html_url: "https://github.com/testowner/testrepo" },
        },
      };

      await runScript();

      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Failed to create comment with workflow link"));
      // Should NOT use core.error or core.setFailed for non-critical errors
      expect(mockCore.error).not.toHaveBeenCalled();
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });
  });

  describe("addCommentWithWorkflowLink() - workflow-id marker", () => {
    it("should include workflow-id marker when GITHUB_WORKFLOW is set", async () => {
      process.env.GITHUB_WORKFLOW = "test-workflow.yml";

      await runAddCommentWithWorkflowLink(issuesCommentEndpoint123, "https://github.com/testowner/testrepo/actions/runs/12345", "issues");

      expect(mockGithub.request).toHaveBeenCalledWith(
        expect.stringContaining("POST"),
        expect.objectContaining({
          body: expect.stringContaining("<!-- gh-aw-workflow-id: test-workflow.yml -->"),
        })
      );
    });

    it("should include tracker-id marker when GH_AW_TRACKER_ID is set", async () => {
      process.env.GH_AW_TRACKER_ID = "tracker-123";

      await runAddCommentWithWorkflowLink(issuesCommentEndpoint123, "https://github.com/testowner/testrepo/actions/runs/12345", "issues");

      expect(mockGithub.request).toHaveBeenCalledWith(
        expect.stringContaining("POST"),
        expect.objectContaining({
          body: expect.stringContaining("<!-- gh-aw-tracker-id: tracker-123 -->"),
        })
      );
    });

    it("should always include reaction comment type marker", async () => {
      await runAddCommentWithWorkflowLink(issuesCommentEndpoint123, "https://github.com/testowner/testrepo/actions/runs/12345", "issues");

      expect(mockGithub.request).toHaveBeenCalledWith(
        expect.stringContaining("POST"),
        expect.objectContaining({
          body: expect.stringContaining("<!-- gh-aw-comment-type: reaction -->"),
        })
      );
    });
  });

  describe("addCommentWithWorkflowLink() - lock notice", () => {
    it("should add lock notice for issues event when GH_AW_LOCK_FOR_AGENT=true", async () => {
      process.env.GH_AW_LOCK_FOR_AGENT = "true";

      await runAddCommentWithWorkflowLink(issuesCommentEndpoint123, "https://github.com/testowner/testrepo/actions/runs/12345", "issues");

      expect(mockGithub.request).toHaveBeenCalledWith(
        expect.stringContaining("POST"),
        expect.objectContaining({
          body: expect.stringContaining("🔒 This issue has been locked while the workflow is running to prevent concurrent modifications."),
        })
      );
    });

    it("should not add lock notice for pull_request events", async () => {
      process.env.GH_AW_LOCK_FOR_AGENT = "true";

      await runAddCommentWithWorkflowLink(issuesCommentEndpoint101, "https://github.com/testowner/testrepo/actions/runs/12345", "pull_request");

      expect(mockGithub.request).toHaveBeenCalledWith(
        expect.stringContaining("POST"),
        expect.objectContaining({
          body: expect.not.stringContaining("🔒 This issue has been locked"),
        })
      );
    });
  });

  describe("addCommentWithWorkflowLink() - outputs", () => {
    it("should set all required outputs (comment-id, comment-url, comment-repo)", async () => {
      await runAddCommentWithWorkflowLink(issuesCommentEndpoint123, "https://github.com/testowner/testrepo/actions/runs/12345", "issues");

      expect(mockCore.setOutput).toHaveBeenCalledWith("comment-id", "67890");
      expect(mockCore.setOutput).toHaveBeenCalledWith("comment-url", expect.stringContaining("issuecomment-67890"));
      expect(mockCore.setOutput).toHaveBeenCalledWith("comment-repo", "testowner/testrepo");
    });
  });

  describe("main() - activation comments disabled", () => {
    it("should skip comment when activationComments is false", async () => {
      process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({ activationComments: false });
      global.context = {
        eventName: "issues",
        runId: 12345,
        repo: { owner: "testowner", repo: "testrepo" },
        payload: {
          issue: { number: 456 },
          repository: { html_url: "https://github.com/testowner/testrepo" },
        },
      };

      await runScript();

      expect(mockGithub.request).not.toHaveBeenCalled();
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });
  });

  describe("main() - issue_comment missing number", () => {
    it("should fail when issue number is missing in issue_comment event", async () => {
      global.context = {
        eventName: "issue_comment",
        runId: 12345,
        repo: { owner: "testowner", repo: "testrepo" },
        payload: {},
      };

      await runScript();

      expect(mockCore.setFailed).toHaveBeenCalledWith(`${ERR_NOT_FOUND}: Issue number not found in event payload`);
      expect(mockGithub.request).not.toHaveBeenCalled();
    });
  });

  describe("addCommentWithWorkflowLink() - custom workflow name", () => {
    it("should use GH_AW_WORKFLOW_NAME in the comment body", async () => {
      process.env.GH_AW_WORKFLOW_NAME = "My Custom Workflow";

      await runAddCommentWithWorkflowLink(issuesCommentEndpoint123, "https://github.com/testowner/testrepo/actions/runs/12345", "issues");

      expect(mockGithub.request).toHaveBeenCalledWith(
        expect.stringContaining("POST"),
        expect.objectContaining({
          body: expect.stringContaining("My Custom Workflow"),
        })
      );
    });

    it("should fall back to GITHUB_WORKFLOW when GH_AW_WORKFLOW_NAME is not set", async () => {
      process.env.GITHUB_WORKFLOW = "Agentic Commands";

      await runAddCommentWithWorkflowLink(issuesCommentEndpoint123, "https://github.com/testowner/testrepo/actions/runs/12345", "issues");

      expect(mockGithub.request).toHaveBeenCalledWith(
        expect.stringContaining("POST"),
        expect.objectContaining({
          body: expect.stringContaining("Agentic Commands"),
        })
      );
    });
  });

  describe("buildCommentBody()", () => {
    it("should include the run URL in the comment body", async () => {
      const { buildCommentBody } = await importAddWorkflowRunComment();
      const body = buildCommentBody("issues", "https://github.com/testowner/testrepo/actions/runs/99");
      expect(body).toContain("https://github.com/testowner/testrepo/actions/runs/99");
    });

    it("should use workflow emoji from environment in the started status line", async () => {
      process.env.GH_AW_WORKFLOW_EMOJI = "🤖";
      const { buildCommentBody } = await importAddWorkflowRunComment();
      const body = buildCommentBody("issues", "https://example.com/run/1");
      expect(body).toContain("🤖 [");
      expect(body).not.toContain("🚀 [");
    });

    it("should always include reaction comment type marker", async () => {
      const { buildCommentBody } = await importAddWorkflowRunComment();
      const body = buildCommentBody("issues", "https://example.com/run/1");
      expect(body).toContain("<!-- gh-aw-comment-type: reaction -->");
    });

    it("should include workflow-id marker when GITHUB_WORKFLOW is set", async () => {
      process.env.GITHUB_WORKFLOW = "my-workflow.yml";
      const { buildCommentBody } = await importAddWorkflowRunComment();
      const body = buildCommentBody("issues", "https://example.com/run/1");
      expect(body).toContain("<!-- gh-aw-workflow-id: my-workflow.yml -->");
    });

    it("should use GITHUB_WORKFLOW as workflow name when GH_AW_WORKFLOW_NAME is not set", async () => {
      process.env.GITHUB_WORKFLOW = "Agentic Commands";
      const { buildCommentBody } = await importAddWorkflowRunComment();
      const body = buildCommentBody("issue_comment", "https://example.com/run/1");
      expect(body).toContain("[Agentic Commands]");
    });

    it("should prefer explicit workflow name override over environment defaults", async () => {
      process.env.GH_AW_WORKFLOW_NAME = "Agentic Commands";
      const { buildCommentBody } = await importAddWorkflowRunComment();
      const body = buildCommentBody("issue_comment", "https://example.com/run/1", "archie");
      expect(body).toContain("[archie]");
      expect(body).not.toContain("[Agentic Commands]");
    });

    it("should include tracker-id marker when GH_AW_TRACKER_ID is set", async () => {
      process.env.GH_AW_TRACKER_ID = "my-tracker";
      const { buildCommentBody } = await importAddWorkflowRunComment();
      const body = buildCommentBody("issues", "https://example.com/run/1");
      expect(body).toContain("<!-- gh-aw-tracker-id: my-tracker -->");
    });

    it("should add lock notice for issues event when GH_AW_LOCK_FOR_AGENT=true", async () => {
      process.env.GH_AW_LOCK_FOR_AGENT = "true";
      const { buildCommentBody } = await importAddWorkflowRunComment();
      const body = buildCommentBody("issues", "https://example.com/run/1");
      expect(body).toContain("🔒 This issue has been locked");
    });

    it("should not add lock notice for pull_request events", async () => {
      process.env.GH_AW_LOCK_FOR_AGENT = "true";
      const { buildCommentBody } = await importAddWorkflowRunComment();
      const body = buildCommentBody("pull_request", "https://example.com/run/1");
      expect(body).not.toContain("🔒 This issue has been locked");
    });

    it("should use unknown event type description for unrecognized events", async () => {
      const { buildCommentBody } = await importAddWorkflowRunComment();
      // Should not throw for unknown event types
      const body = buildCommentBody("some_unknown_event", "https://example.com/run/1");
      expect(body).toBeTruthy();
      expect(body).toContain("<!-- gh-aw-comment-type: reaction -->");
    });
  });

  describe("postDiscussionComment()", () => {
    it("should post a top-level discussion comment when no replyToNodeId", async () => {
      const { postDiscussionComment } = await importAddWorkflowRunComment();
      await postDiscussionComment(10, "Test body");

      expect(mockGithub.graphql).toHaveBeenCalled();
      const mutationCall = mockGithub.graphql.mock.calls.find(call => call[0].includes("addDiscussionComment"));
      expect(mutationCall).toBeDefined();
      expect(mutationCall[1]).toMatchObject({ body: "Test body" });
      expect(mutationCall[1]).not.toHaveProperty("replyToId");
    });

    it("should post a threaded comment when replyToNodeId is provided", async () => {
      const { postDiscussionComment } = await importAddWorkflowRunComment();
      await postDiscussionComment(10, "Reply body", "DC_kwParent123");

      const mutationCall = mockGithub.graphql.mock.calls.find(call => call[0].includes("replyToId"));
      expect(mutationCall).toBeDefined();
      expect(mutationCall[1]).toMatchObject({ body: "Reply body", replyToId: "DC_kwParent123" });
    });

    it("should set comment outputs after posting", async () => {
      const { postDiscussionComment } = await importAddWorkflowRunComment();
      await postDiscussionComment(10, "Test body");

      expect(mockCore.setOutput).toHaveBeenCalledWith("comment-id", "DC_kwDOTest456");
      expect(mockCore.setOutput).toHaveBeenCalledWith("comment-url", expect.stringContaining("discussioncomment-456"));
    });
  });
});
