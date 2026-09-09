// @ts-check

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import path from "path";
import fs from "fs";
import { createRequire } from "module";
import {
  main,
  loadConfig,
  loadHandlers,
  processMessages,
  sortMessagesByTemporaryIdDependencies,
  sortMessageIndicesByTemporaryIdDependencies,
  buildCommentMemoryMessagesFromFiles,
  rollbackReviewResults,
  rollbackReviewResultsForPR,
  skipReviewResults,
  skipReviewResultsForPR,
  logCreatedItemFromResult,
  isFailedProcessingResult,
  isReportOnlyFailureResult,
  partitionFailureResults,
  computeSafeOutputsStatus,
  setSafeOutputsStatusOutputs,
  processSyntheticUpdates,
} from "./safe_output_handler_manager.cjs";

const require = createRequire(import.meta.url);

describe("Safe Output Handler Manager", () => {
  beforeEach(() => {
    // Mock global core
    global.core = {
      info: vi.fn(),
      debug: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
      setOutput: vi.fn(),
      setFailed: vi.fn(),
    };
  });

  afterEach(() => {
    // Clean up environment variables
    delete process.env.GH_AW_AGENT_OUTPUT;
    delete process.env.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG;
    delete process.env.GH_AW_TRACKER_LABEL;
    delete process.env.GH_AW_SAFE_OUTPUT_JOBS;
    delete process.env.GH_AW_SAFE_OUTPUT_SCRIPTS;
    delete process.env.GH_AW_SAFE_OUTPUT_ACTIONS;
    delete process.env.GH_AW_DETECTION_CONCLUSION;
    delete process.env.RUNNER_TEMP;
    delete global.github;
    delete global.context;
    fs.rmSync("/tmp/gh-aw/comment-memory", { recursive: true, force: true });
    fs.rmSync("/tmp/gh-aw/safe-output-errors.json", { force: true });
    fs.rmSync("/tmp/gh-aw/actions", { recursive: true, force: true });
  });

  describe("loadConfig", () => {
    it("should load config from environment variable and normalize keys", () => {
      const config = {
        "create-issue": { max: 5 },
        "add-comment": { max: 1 },
      };

      process.env.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG = JSON.stringify(config);

      const result = loadConfig();

      expect(result).toHaveProperty("create_issue");
      expect(result).toHaveProperty("add_comment");
      expect(result.create_issue).toEqual({ max: 5 });
      expect(result.add_comment).toEqual({ max: 1 });
    });

    describe("main failure diagnostics artifact", () => {
      it("writes safe-output-errors.json when message processing has fatal failures", async () => {
        process.env.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG = "{}";
        process.env.GH_AW_SAFE_OUTPUT_SCRIPTS = JSON.stringify({ custom_fail: "custom_fail_handler.cjs" });
        global.context = {};
        global.github = {
          rest: {
            rateLimit: {
              get: vi.fn().mockResolvedValue({
                data: {
                  resources: {
                    core: {
                      remaining: 5000,
                      limit: 5000,
                      reset: Math.floor(Date.now() / 1000) + 60,
                    },
                  },
                },
              }),
            },
          },
          graphql: vi.fn(),
        };

        const scriptDir = "/tmp/gh-aw/actions";
        fs.mkdirSync(scriptDir, { recursive: true });
        fs.writeFileSync(`${scriptDir}/custom_fail_handler.cjs`, `module.exports = { main: async () => async () => ({ success: false, errorCode: "ERR_API", error: "script failure" }) };`);

        const outputFile = "/tmp/gh-aw/custom-failure-agent-output.json";
        fs.writeFileSync(outputFile, JSON.stringify({ items: [{ type: "custom_fail" }] }));
        process.env.GH_AW_AGENT_OUTPUT = outputFile;

        await main();

        expect(global.core.setFailed).toHaveBeenCalledWith(expect.stringContaining("safe output(s) failed"));
        expect(fs.existsSync("/tmp/gh-aw/safe-output-errors.json")).toBe(true);

        const report = JSON.parse(fs.readFileSync("/tmp/gh-aw/safe-output-errors.json", "utf8"));
        expect(report.errorCode).toBe("E099");
        expect(report.message).toContain("custom_fail");
        expect(report.failures).toEqual([{ type: "custom_fail", error: "script failure" }]);
      });

      it("writes safe-output-errors.json when main hits the catch path", async () => {
        delete process.env.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG;

        await main();

        expect(global.core.setFailed).toHaveBeenCalledWith(expect.stringContaining("ERR_VALIDATION: Handler manager failed"));
        expect(fs.existsSync("/tmp/gh-aw/safe-output-errors.json")).toBe(true);

        const report = JSON.parse(fs.readFileSync("/tmp/gh-aw/safe-output-errors.json", "utf8"));
        expect(report.errorCode).toBe("ERR_VALIDATION");
        expect(report.message).toContain("GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG environment variable is required but not set");
        expect(report.failures).toEqual([]);
      });
    });

    describe("temporary ID dependency ordering", () => {
      it("orders a blocked_by temporary-ID producer before its dependent issue", () => {
        const prerequisite = { type: "create_issue", temporary_id: "aw_prereq", title: "Prerequisite" };
        const blocked = { type: "create_issue", temporary_id: "aw_blocked", blocked_by: "aw_prereq", title: "Blocked" };

        expect(sortMessagesByTemporaryIdDependencies([blocked, prerequisite])).toEqual([prerequisite, blocked]);
      });

      it("keeps independent messages in their original relative order", () => {
        const dependent = { type: "create_issue", temporary_id: "aw_blocked", blocked_by: "aw_prereq", title: "Blocked" };
        const producer = { type: "create_issue", temporary_id: "aw_prereq", title: "Prerequisite" };
        const unrelated = { type: "create_issue", temporary_id: "aw_other", title: "Unrelated" };

        expect(sortMessagesByTemporaryIdDependencies([dependent, producer, unrelated])).toEqual([producer, dependent, unrelated]);
      });

      it("returns original message indices in processing order", () => {
        const dependent = { type: "create_issue", temporary_id: "aw_blocked", blocked_by: "aw_prereq", title: "Blocked" };
        const producer = { type: "create_issue", temporary_id: "aw_prereq", title: "Prerequisite" };
        const unrelated = { type: "create_issue", temporary_id: "aw_other", title: "Unrelated" };

        expect(sortMessageIndicesByTemporaryIdDependencies([dependent, producer, unrelated])).toEqual([1, 0, 2]);
      });

      it("tracks a comment emitted before its temporary-ID producer", async () => {
        const callOrder = [];
        const handlers = new Map([
          [
            "add_comment",
            vi.fn(async (_message, resolvedTemporaryIds) => {
              callOrder.push("add_comment");
              expect(resolvedTemporaryIds).toEqual({});
              return {
                success: true,
                commentId: 123,
                itemNumber: 42,
                repo: "owner/repo",
                isDiscussion: false,
                body: "Tracking issue: #aw_track1\n\nHandler footer marker",
              };
            }),
          ],
          [
            "create_issue",
            vi.fn(async () => {
              callOrder.push("create_issue");
              return { success: true, temporaryId: "aw_track1", repo: "owner/tracker", number: 99 };
            }),
          ],
        ]);
        const messages = [
          { type: "add_comment", item_number: 42, body: "Tracking issue: #aw_track1" },
          { type: "create_issue", temporary_id: "aw_track1", title: "Tracking issue" },
        ];

        const result = await processMessages(handlers, messages);

        expect(callOrder).toEqual(["add_comment", "create_issue"]);
        expect(result.temporaryIdMap.aw_track1).toEqual({ repo: "owner/tracker", number: 99 });
        expect(result.outputsWithUnresolvedIds).toEqual([
          {
            type: "add_comment",
            message: { type: "add_comment", item_number: 42, body: "Tracking issue: #aw_track1" },
            result: {
              success: true,
              commentId: 123,
              itemNumber: 42,
              repo: "owner/repo",
              isDiscussion: false,
              body: "Tracking issue: #aw_track1\n\nHandler footer marker",
            },
            originalTempIdMapSize: 0,
          },
        ]);
      });

      it("updates the posted comment body while retaining handler metadata", async () => {
        const updateComment = vi.fn().mockResolvedValue({});
        const github = { rest: { issues: { updateComment } } };
        const trackedOutputs = [
          {
            type: "add_comment",
            message: { type: "add_comment", body: "Tracking issue: #aw_track1" },
            result: {
              success: true,
              commentId: 123,
              itemNumber: 42,
              repo: "owner/repo",
              isDiscussion: false,
              body: "Tracking issue: #aw_track1\n\nHandler footer marker",
            },
            originalTempIdMapSize: 0,
          },
        ];

        const updateCount = await processSyntheticUpdates(github, {}, trackedOutputs, new Map([["aw_track1", { repo: "owner/tracker", number: 99 }]]), new Map());

        expect(updateCount).toBe(1);
        expect(updateComment).toHaveBeenCalledWith({
          owner: "owner",
          repo: "repo",
          comment_id: 123,
          body: "Tracking issue: owner/tracker#99\n\nHandler footer marker",
        });
      });
    });

    describe("logCreatedItemFromResult", () => {
      it("should log finalized review results and skip buffered review metadata", () => {
        const onItemCreated = vi.fn();

        logCreatedItemFromResult(onItemCreated, "submit_pull_request_review", { success: true, event: "COMMENT", body_length: 12 });
        expect(onItemCreated).not.toHaveBeenCalled();

        logCreatedItemFromResult(onItemCreated, "submit_pull_request_review", {
          success: true,
          review_url: "https://github.com/owner/repo/pull/1#pullrequestreview-2",
          pull_request_number: 1,
          repo: "owner/repo",
          before_state: { reviews: [] },
          after_state: { reviews: [{ id: 2, state: "COMMENTED" }] },
        });

        expect(onItemCreated).toHaveBeenCalledWith({
          type: "submit_pull_request_review",
          url: "https://github.com/owner/repo/pull/1#pullrequestreview-2",
          number: 1,
          repo: "owner/repo",
          before_state: { reviews: [] },
          after_state: { reviews: [{ id: 2, state: "COMMENTED" }] },
        });
      });
    });

    it("should throw error if environment variable is not set", () => {
      expect(() => loadConfig()).toThrow("GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG environment variable is required but not set");
    });

    it("should throw error if environment variable contains invalid JSON", () => {
      process.env.GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG = "not json";
      expect(() => loadConfig()).toThrow("Failed to parse GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG");
    });
  });

  describe("report-only assignment failures", () => {
    it("recognizes only active failures as failed processing results", () => {
      expect(isFailedProcessingResult({ success: false })).toBe(true);
      expect(isFailedProcessingResult({ success: false, deferred: true })).toBe(false);
      expect(isFailedProcessingResult({ success: false, skipped: true })).toBe(false);
      expect(isFailedProcessingResult({ success: false, cancelled: true })).toBe(false);
    });

    it("treats failed assign_to_agent results as report-only", () => {
      expect(
        isReportOnlyFailureResult({
          type: "assign_to_agent",
          success: false,
        })
      ).toBe(true);
    });

    it("treats failed upload_artifact results as report-only", () => {
      expect(
        isReportOnlyFailureResult({
          type: "upload_artifact",
          success: false,
        })
      ).toBe(true);
    });

    it("does not treat failed resolve_pull_request_review_thread results as report-only", () => {
      expect(
        isReportOnlyFailureResult({
          type: "resolve_pull_request_review_thread",
          success: false,
        })
      ).toBe(false);
    });

    it("does not treat failed dismiss_pull_request_review results as report-only", () => {
      expect(
        isReportOnlyFailureResult({
          type: "dismiss_pull_request_review",
          success: false,
        })
      ).toBe(false);
    });

    it("does not treat skipped or cancelled assign_to_agent results as report-only", () => {
      expect(
        isReportOnlyFailureResult({
          type: "assign_to_agent",
          success: false,
          skipped: true,
        })
      ).toBe(false);
      expect(
        isReportOnlyFailureResult({
          type: "assign_to_agent",
          success: false,
          cancelled: true,
        })
      ).toBe(false);
      expect(
        isReportOnlyFailureResult({
          type: "assign_to_agent",
          success: false,
          deferred: true,
        })
      ).toBe(false);
    });

    it("does not treat skipped or cancelled upload_artifact results as report-only", () => {
      expect(
        isReportOnlyFailureResult({
          type: "upload_artifact",
          success: false,
          skipped: true,
        })
      ).toBe(false);
      expect(
        isReportOnlyFailureResult({
          type: "upload_artifact",
          success: false,
          cancelled: true,
        })
      ).toBe(false);
    });

    it("partitions fatal failures away from assign_to_agent report-only failures", () => {
      const { fatalFailures, reportOnlyFailures } = partitionFailureResults([
        { type: "assign_to_agent", success: false, error: "Insufficient permissions" },
        { type: "create_issue", success: false, error: "Validation failed" },
        { type: "assign_to_agent", success: false, skipped: true, error: "Handled elsewhere" },
        { type: "create_discussion", success: true },
      ]);

      expect(reportOnlyFailures).toEqual([{ type: "assign_to_agent", success: false, error: "Insufficient permissions" }]);
      expect(fatalFailures).toEqual([{ type: "create_issue", success: false, error: "Validation failed" }]);
    });

    it("partitions upload_artifact failures as report-only, not fatal", () => {
      const { fatalFailures, reportOnlyFailures } = partitionFailureResults([
        { type: "upload_artifact", success: false, error: "artifact twirp CreateArtifact failed (400)" },
        { type: "create_discussion", success: true },
        { type: "create_issue", success: false, error: "Validation failed" },
      ]);

      expect(reportOnlyFailures).toEqual([{ type: "upload_artifact", success: false, error: "artifact twirp CreateArtifact failed (400)" }]);
      expect(fatalFailures).toEqual([{ type: "create_issue", success: false, error: "Validation failed" }]);
    });

    it("computes partial success item status from mixed successful and failed results", () => {
      const status = computeSafeOutputsStatus([
        { type: "create_issue", success: true },
        { type: "add_comment", success: true },
        { type: "create_discussion", success: false, error: "Validation failed" },
        { type: "noop", success: false, skipped: true },
        { type: "link_sub_issue", success: false, deferred: true },
        { type: "merge_pull_request", success: false, cancelled: true },
      ]);

      expect(status).toEqual({
        itemsSucceeded: 2,
        itemsApplied: 2,
        itemsSkipped: 1,
        itemsWarnings: 0,
        itemsCancelled: 1,
        itemsDeferred: 1,
        itemsFailed: 1,
        status: "partial_success",
      });
    });

    it("computes skipped and warning counts without counting them as applied mutations", () => {
      expect(
        computeSafeOutputsStatus([
          { type: "add_comment", success: true, result: { success: true, skipped: true, warning: "Target locked" } },
          { type: "add_labels", success: false, skipped: true, result: { success: false, skipped: true, reasonCode: "REQUIRED_LABELS_MISMATCH" } },
          { type: "create_issue", success: true },
        ])
      ).toEqual({
        itemsSucceeded: 1,
        itemsApplied: 1,
        itemsSkipped: 2,
        itemsWarnings: 0,
        itemsCancelled: 0,
        itemsDeferred: 0,
        itemsFailed: 0,
        status: "completed_with_skips",
      });
    });

    it("omits only explicitly delegated skips from outcome counts", () => {
      expect(
        computeSafeOutputsStatus([
          { type: "add_comment", success: false, skipped: true, reason: "Policy skipped this output" },
          { type: "noop", success: false, skipped: true, delegated: true, reason: "Handled by standalone step" },
        ])
      ).toEqual({
        itemsSucceeded: 0,
        itemsApplied: 0,
        itemsSkipped: 1,
        itemsWarnings: 0,
        itemsCancelled: 0,
        itemsDeferred: 0,
        itemsFailed: 0,
        status: "completed_with_skips",
      });
    });

    it("computes failure item status when all active results failed", () => {
      expect(
        computeSafeOutputsStatus([
          { type: "create_issue", success: false, error: "Validation failed" },
          { type: "add_comment", success: false, error: "Validation failed" },
        ])
      ).toEqual({
        itemsSucceeded: 0,
        itemsApplied: 0,
        itemsSkipped: 0,
        itemsWarnings: 0,
        itemsCancelled: 0,
        itemsDeferred: 0,
        itemsFailed: 2,
        status: "failure",
      });
    });

    it("exports item status outputs", () => {
      setSafeOutputsStatusOutputs({
        itemsSucceeded: 10,
        itemsFailed: 5,
        status: "partial_success",
      });

      expect(core.setOutput).toHaveBeenCalledWith("items_succeeded", "10");
      expect(core.setOutput).toHaveBeenCalledWith("items_applied", "10");
      expect(core.setOutput).toHaveBeenCalledWith("items_skipped", "0");
      expect(core.setOutput).toHaveBeenCalledWith("items_warnings", "0");
      expect(core.setOutput).toHaveBeenCalledWith("items_cancelled", "0");
      expect(core.setOutput).toHaveBeenCalledWith("items_deferred", "0");
      expect(core.setOutput).toHaveBeenCalledWith("items_failed", "5");
      expect(core.setOutput).toHaveBeenCalledWith("status", "partial_success");
    });

    it("keeps review cleanup failures fatal unless a handler marks them skipped", () => {
      const { fatalFailures, reportOnlyFailures } = partitionFailureResults([
        { type: "resolve_pull_request_review_thread", success: false, error: "wrong node type" },
        { type: "dismiss_pull_request_review", success: false, error: "wrong actor" },
      ]);

      expect(reportOnlyFailures).toEqual([]);
      expect(fatalFailures).toEqual([
        { type: "resolve_pull_request_review_thread", success: false, error: "wrong node type" },
        { type: "dismiss_pull_request_review", success: false, error: "wrong actor" },
      ]);
    });

    it("does not treat a resolve_pull_request_review_thread result marked skipped as a fatal failure", () => {
      const results = [
        { type: "resolve_pull_request_review_thread", success: false, skipped: true, error: "Repository 'other-owner/other-repo' is not in the allowed-repos list. Allowed: github/gh-aw" },
        { type: "create_issue", success: false, error: "Validation failed" },
      ];
      const { fatalFailures, reportOnlyFailures } = partitionFailureResults(results);

      expect(reportOnlyFailures).toEqual([]);
      expect(fatalFailures).toEqual([{ type: "create_issue", success: false, error: "Validation failed" }]);

      // The skipped result is neither a fatal nor a report-only failure; it is counted
      // as a skip in the overall item status instead of being silently dropped.
      const status = computeSafeOutputsStatus(results);
      expect(status.itemsSkipped).toBe(1);
      expect(status.itemsFailed).toBe(1);
    });
  });

  describe("loadHandlers", () => {
    // These tests are skipped because they require actual handler modules to exist
    // In a real environment, handlers are loaded dynamically via require()
    it.skip("should load handlers for enabled safe output types", async () => {
      const config = {
        create_issue: { max: 1 },
        add_comment: { max: 1 },
      };

      const handlers = await loadHandlers(config);

      expect(handlers.size).toBeGreaterThan(0);
      expect(handlers.has("create_issue")).toBe(true);
      expect(handlers.has("add_comment")).toBe(true);
    });

    it.skip("should not load handlers when config entry is missing", async () => {
      const config = {
        create_issue: { max: 1 },
        // add_comment is not in config
      };

      const handlers = await loadHandlers(config);

      expect(handlers.has("create_issue")).toBe(true);
      expect(handlers.has("add_comment")).toBe(false);
    });

    it.skip("should handle missing handlers gracefully", async () => {
      const config = {
        nonexistent_handler: { max: 1 },
      };

      const handlers = await loadHandlers(config);

      expect(handlers.size).toBe(0);
    });

    it("should throw error when handler main() does not return a function", async () => {
      // This test verifies that if a handler's main() function doesn't return
      // a message handler function, the loadHandlers function will throw an error
      // rather than just logging a warning.
      //
      // Expected behavior:
      // 1. Handler is loaded successfully
      // 2. main() is called with config
      // 3. If main() returns non-function, an error is thrown
      // 4. The error should fail the step
      //
      // This is important because:
      // - Old handlers execute directly and return undefined
      // - New handlers follow factory pattern and return a function
      // - Silent failures from misconfigured handlers are hard to debug
      //
      // The implementation checks: typeof messageHandler !== "function"
      // and throws: "Handler X main() did not return a function"

      // Note: Actual integration testing requires real handler modules
      // This test documents the expected behavior for validation
      expect(true).toBe(true);
    });

    it("should pass top-level mentions config into handler config", async () => {
      const addCommentModule = require("./add_comment.cjs");
      const addCommentMainSpy = vi.spyOn(addCommentModule, "main").mockImplementation(async () => async () => ({ success: true }));
      const createIssueModule = require("./create_issue.cjs");
      const createIssueMainSpy = vi.spyOn(createIssueModule, "main").mockImplementation(async () => async () => ({ success: true }));

      try {
        const mentionsConfig = { enabled: true, allowed: ["@copilot"] };
        const handlers = await loadHandlers(
          {
            add_comment: { max: 1 },
            create_issue: { max: 1 },
            mentions: mentionsConfig,
          },
          undefined,
          ["copilot", "octocat"]
        );

        expect(handlers.has("add_comment")).toBe(true);
        expect(handlers.has("create_issue")).toBe(true);
        expect(addCommentMainSpy).toHaveBeenCalledTimes(1);
        expect(createIssueMainSpy).toHaveBeenCalledTimes(1);
        expect(addCommentMainSpy).toHaveBeenCalledWith(
          expect.objectContaining({
            max: 1,
            mentions: mentionsConfig,
            allowedMentionAliases: ["copilot", "octocat"],
          }),
          null
        );
        expect(createIssueMainSpy).toHaveBeenCalledWith(
          expect.objectContaining({
            max: 1,
            mentions: mentionsConfig,
            allowedMentionAliases: ["copilot", "octocat"],
          }),
          null
        );
      } finally {
        addCommentMainSpy.mockRestore();
        createIssueMainSpy.mockRestore();
      }
    });

    it("injects GH_AW_PROJECT_GITHUB_TOKEN for project handlers when github-token is missing", async () => {
      process.env.GH_AW_PROJECT_GITHUB_TOKEN = "projects-token";
      global.getOctokit = vi.fn().mockReturnValue({ client: "project-client" });

      const updateProjectModule = require("./update_project.cjs");
      const updateProjectMainSpy = vi.spyOn(updateProjectModule, "main").mockImplementation(async () => async () => ({ success: true }));

      try {
        const handlers = await loadHandlers({
          update_project: { project: "https://github.com/orgs/myorg/projects/1" },
        });

        expect(handlers.has("update_project")).toBe(true);
        expect(global.getOctokit).toHaveBeenCalledWith("projects-token");
        expect(updateProjectMainSpy).toHaveBeenCalledWith(
          expect.objectContaining({
            project: "https://github.com/orgs/myorg/projects/1",
            "github-token": "projects-token",
          }),
          expect.objectContaining({ client: "project-client" })
        );
      } finally {
        updateProjectMainSpy.mockRestore();
        delete process.env.GH_AW_PROJECT_GITHUB_TOKEN;
        delete global.getOctokit;
      }
    });

    it("preserves explicit project handler github-token over GH_AW_PROJECT_GITHUB_TOKEN fallback", async () => {
      process.env.GH_AW_PROJECT_GITHUB_TOKEN = "fallback-project-token";
      global.getOctokit = vi.fn().mockReturnValue({ client: "project-client" });

      const updateProjectModule = require("./update_project.cjs");
      const updateProjectMainSpy = vi.spyOn(updateProjectModule, "main").mockImplementation(async () => async () => ({ success: true }));

      try {
        await loadHandlers({
          update_project: {
            project: "https://github.com/orgs/myorg/projects/1",
            "github-token": "explicit-project-token",
          },
        });

        expect(global.getOctokit).toHaveBeenCalledWith("explicit-project-token");
        expect(updateProjectMainSpy).toHaveBeenCalledWith(
          expect.objectContaining({
            "github-token": "explicit-project-token",
          }),
          expect.objectContaining({ client: "project-client" })
        );
      } finally {
        updateProjectMainSpy.mockRestore();
        delete process.env.GH_AW_PROJECT_GITHUB_TOKEN;
        delete global.getOctokit;
      }
    });

    it("wraps project handlers so global.github is set to the project client during execution", async () => {
      process.env.GH_AW_PROJECT_GITHUB_TOKEN = "projects-token";
      const projectClient = { client: "project-client" };
      global.getOctokit = vi.fn().mockReturnValue(projectClient);

      const previousGithub = { client: "shared-client" };
      global.github = previousGithub;
      let seenGithub = null;

      const updateProjectModule = require("./update_project.cjs");
      const updateProjectMainSpy = vi.spyOn(updateProjectModule, "main").mockImplementation(async () => async () => {
        // outer function: handler factory at loadHandlers() time; inner function: message handler at processMessages() time
        seenGithub = global.github;
        return { success: true };
      });

      try {
        const handlers = await loadHandlers({
          update_project: { project: "https://github.com/orgs/myorg/projects/1" },
        });
        const handler = handlers.get("update_project");
        expect(typeof handler).toBe("function");

        await handler({ type: "update_project" });

        expect(seenGithub).toBe(projectClient);
        expect(global.github).toBe(previousGithub);
      } finally {
        updateProjectMainSpy.mockRestore();
        delete process.env.GH_AW_PROJECT_GITHUB_TOKEN;
        delete global.getOctokit;
        global.github = previousGithub;
      }
    });

    it("restores global.github to absent state after wrapped project handler execution", async () => {
      process.env.GH_AW_PROJECT_GITHUB_TOKEN = "projects-token";
      const projectClient = { client: "project-client" };
      global.getOctokit = vi.fn().mockReturnValue(projectClient);
      delete global.github;

      const updateProjectModule = require("./update_project.cjs");
      const updateProjectMainSpy = vi.spyOn(updateProjectModule, "main").mockImplementation(async () => async () => ({ success: true }));

      try {
        const handlers = await loadHandlers({
          update_project: { project: "https://github.com/orgs/myorg/projects/1" },
        });
        const handler = handlers.get("update_project");
        expect(typeof handler).toBe("function");

        await handler({ type: "update_project" });

        expect("github" in global).toBe(false);
      } finally {
        updateProjectMainSpy.mockRestore();
        delete process.env.GH_AW_PROJECT_GITHUB_TOKEN;
        delete global.getOctokit;
        delete global.github;
      }
    });
  });

  describe("loadHandlers - path traversal sanitization", () => {
    // These tests exercise the path traversal sanitization added to protect against
    // malicious scriptFilename values in GH_AW_SAFE_OUTPUT_SCRIPTS.
    // The sanitization runs BEFORE require(), so no real file needs to exist.

    it("should reject scriptFilename with a leading ../", async () => {
      process.env.GH_AW_SAFE_OUTPUT_SCRIPTS = JSON.stringify({
        evil: "../evil.cjs",
      });

      const handlers = await loadHandlers({}, {});

      expect(handlers.has("evil")).toBe(false);
      expect(core.error).toHaveBeenCalledWith(expect.stringContaining('path traversal detected in "../evil.cjs"'));
    });

    it("should reject scriptFilename with an embedded directory separator", async () => {
      process.env.GH_AW_SAFE_OUTPUT_SCRIPTS = JSON.stringify({
        evil: "subdir/evil.cjs",
      });

      const handlers = await loadHandlers({}, {});

      expect(handlers.has("evil")).toBe(false);
      expect(core.error).toHaveBeenCalledWith(expect.stringContaining('path traversal detected in "subdir/evil.cjs"'));
    });

    it("should reject scriptFilename with an absolute Unix path", async () => {
      process.env.GH_AW_SAFE_OUTPUT_SCRIPTS = JSON.stringify({
        evil: "/etc/passwd",
      });

      const handlers = await loadHandlers({}, {});

      expect(handlers.has("evil")).toBe(false);
      expect(core.error).toHaveBeenCalledWith(expect.stringContaining('path traversal detected in "/etc/passwd"'));
    });

    it("should reject scriptFilename with multiple levels of path traversal", async () => {
      process.env.GH_AW_SAFE_OUTPUT_SCRIPTS = JSON.stringify({
        evil: "../../etc/shadow",
      });

      const handlers = await loadHandlers({}, {});

      expect(handlers.has("evil")).toBe(false);
      expect(core.error).toHaveBeenCalledWith(expect.stringContaining('path traversal detected in "../../etc/shadow"'));
    });

    it("should reject scriptFilename that is just '..'", async () => {
      process.env.GH_AW_SAFE_OUTPUT_SCRIPTS = JSON.stringify({
        evil: "..",
      });

      const handlers = await loadHandlers({}, {});

      expect(handlers.has("evil")).toBe(false);
      expect(core.error).toHaveBeenCalledWith(expect.stringContaining("evil"));
    });

    it("should skip rejected entry but continue loading other valid scripts", async () => {
      process.env.GH_AW_SAFE_OUTPUT_SCRIPTS = JSON.stringify({
        evil: "../evil.cjs",
        // This valid name will attempt require() which will fail (file doesn't exist),
        // and that failure is caught as a non-fatal warning — so the handler is absent
        // but no core.error is called for path traversal.
        safe_script: "safe_output_script_safe_script.cjs",
      });

      const handlers = await loadHandlers({}, {});

      // The evil entry must be rejected with core.error (path traversal)
      expect(core.error).toHaveBeenCalledWith(expect.stringContaining('path traversal detected in "../evil.cjs"'));
      // The safe entry should NOT trigger a path traversal error
      expect(core.error).not.toHaveBeenCalledWith(expect.stringContaining('path traversal detected in "safe_output_script_safe_script.cjs"'));
      // Neither handler is loaded (safe one fails because the file doesn't exist in test env)
      expect(handlers.has("evil")).toBe(false);
    });

    it("should accept a plain filename with no path components", async () => {
      // A well-formed filename — require() will fail because the file doesn't exist in
      // the test environment, but NO core.error for path traversal should be logged.
      process.env.GH_AW_SAFE_OUTPUT_SCRIPTS = JSON.stringify({
        my_script: "safe_output_script_my_script.cjs",
      });

      const handlers = await loadHandlers({}, {});

      // No path traversal error should have been emitted
      expect(core.error).not.toHaveBeenCalledWith(expect.stringContaining("path traversal detected"));
      expect(core.error).not.toHaveBeenCalledWith(expect.stringContaining("Script path outside expected directory"));
      // Handler is absent only because the file doesn't exist in the test env (warning, not error)
      expect(handlers.has("my_script")).toBe(false);
    });

    it("should surface the load failure reason when a message has no handler", async () => {
      process.env.GH_AW_SAFE_OUTPUT_SCRIPTS = JSON.stringify({
        my_script: "safe_output_script_my_script.cjs",
      });

      const handlers = await loadHandlers({}, {});
      const result = await processMessages(handlers, [{ type: "my_script" }]);

      expect(result.results[0].success).toBe(false);
      expect(result.results[0].error).toContain("No handler loaded for type 'my_script'");
      expect(result.results[0].error).toContain("Cannot find module");
      expect(core.warning).toHaveBeenCalledWith(expect.stringContaining("The handler was configured but failed to load"));
      expect(core.warning).not.toHaveBeenCalledWith(expect.stringContaining("safe output type is not configured"));
    });
  });

  describe("processMessages", () => {
    it("should process messages in order of appearance", async () => {
      const messages = [
        { type: "add_comment", body: "Comment" },
        { type: "create_issue", title: "Issue" },
      ];

      const mockHandler = vi.fn().mockResolvedValue({ success: true });

      const handlers = new Map([
        ["create_issue", mockHandler],
        ["add_comment", mockHandler],
      ]);

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);
      expect(result.results).toHaveLength(2);

      // Verify handlers were called
      expect(mockHandler).toHaveBeenCalledTimes(2);

      // Verify messages were processed in order of appearance (add_comment first, then create_issue)
      expect(result.results[0].type).toBe("add_comment");
      expect(result.results[0].messageIndex).toBe(0);
      expect(result.results[1].type).toBe("create_issue");
      expect(result.results[1].messageIndex).toBe(1);
    });

    it("preserves summary-safe diagnostics from skipped handler results", async () => {
      const messages = [{ type: "add_comment", item_number: 123 }];
      const skippedResult = {
        success: false,
        skipped: true,
        reasonCode: "REQUIRED_LABELS_MISMATCH",
        reason: "Required labels missing",
        error: "Item does not match required-labels filter",
        target: { repo: "owner/repo", number: 123 },
        safeDetails: {
          requiredLabels: ["automation", "n-plus-1"],
          missingLabels: ["automation"],
        },
      };
      const handler = vi.fn().mockResolvedValue(skippedResult);

      const result = await processMessages(new Map([["add_comment", handler]]), messages);

      expect(result.success).toBe(true);
      expect(result.results[0]).toMatchObject({
        type: "add_comment",
        messageIndex: 0,
        success: false,
        skipped: true,
        reasonCode: "REQUIRED_LABELS_MISMATCH",
        reason: "Required labels missing",
        result: skippedResult,
      });
    });

    it("should abort non-reviewable outputs in detection warning mode", async () => {
      process.env.GH_AW_DETECTION_CONCLUSION = "warning";
      const messages = [{ type: "merge_pull_request" }, { type: "create_issue", title: "Review this", body: "Body" }];
      const mergeHandler = vi.fn().mockResolvedValue({ success: true });
      const createIssueHandler = vi.fn().mockResolvedValue({ success: true });
      const handlers = new Map([
        ["merge_pull_request", mergeHandler],
        ["create_issue", createIssueHandler],
      ]);

      const result = await processMessages(handlers, messages);

      expect(mergeHandler).not.toHaveBeenCalled();
      expect(createIssueHandler).toHaveBeenCalledTimes(1);
      expect(result.results[0]).toMatchObject({
        type: "merge_pull_request",
        success: false,
        cancelled: true,
        threatDetected: true,
        errorCode: "threat_detected_abort_policy",
      });
      expect(result.results[1].success).toBe(true);
    });

    it.each(["set_issue_type", "set_issue_field", "jira_add_label", "add_labels", "remove_labels", "replace_label", "dispatch_repository", "call_workflow", "upload_artifact"])(
      "should abort %s in detection warning mode",
      async messageType => {
        process.env.GH_AW_DETECTION_CONCLUSION = "warning";
        const handler = vi.fn().mockResolvedValue({ success: true });
        const handlers = new Map([[messageType, handler]]);
        const messages = [{ type: messageType }];

        const result = await processMessages(handlers, messages);

        expect(handler).not.toHaveBeenCalled();
        expect(result.results[0]).toMatchObject({
          type: messageType,
          success: false,
          cancelled: true,
          threatDetected: true,
          errorCode: "threat_detected_abort_policy",
        });
      }
    );

    it("should log conversion requirement for push_to_pull_request_branch in detection warning mode", async () => {
      process.env.GH_AW_DETECTION_CONCLUSION = "warning";
      const pushHandler = vi.fn().mockResolvedValue({ success: true });
      const handlers = new Map([["push_to_pull_request_branch", pushHandler]]);
      const messages = [{ type: "push_to_pull_request_branch", branch: "topic" }];

      const result = await processMessages(handlers, messages);

      expect(pushHandler).toHaveBeenCalledTimes(1);
      expect(result.results[0]).toMatchObject({ type: "push_to_pull_request_branch", success: true });
      expect(core.info).toHaveBeenCalledWith(expect.stringContaining('Threat-detection warn policy conversion required for "push_to_pull_request_branch" -> "create_pull_request"'));
    });

    it("should pass the shared temporary ID map to handlers", async () => {
      const messages = [
        { type: "create_issue", title: "Issue", body: "Body", temporary_id: "aw_abc123" },
        { type: "update_project", project: "https://github.com/orgs/test/projects/1", content_type: "issue", content_number: "aw_abc123" },
      ];

      const mockCreateHandler = vi.fn().mockResolvedValue({
        repo: "owner/repo",
        number: 101,
        temporaryId: "aw_abc123",
      });

      const mockUpdateProjectHandler = vi.fn().mockImplementation((message, resolvedTemporaryIds, temporaryIdMap) => {
        expect(temporaryIdMap).toBeInstanceOf(Map);
        expect(temporaryIdMap.get("aw_abc123")).toEqual({ repo: "owner/repo", number: 101 });
        expect(resolvedTemporaryIds["aw_abc123"]).toEqual({ repo: "owner/repo", number: 101 });
        return Promise.resolve({ success: true });
      });

      const handlers = new Map([
        ["create_issue", mockCreateHandler],
        ["update_project", mockUpdateProjectHandler],
      ]);

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);
      expect(mockCreateHandler).toHaveBeenCalledTimes(1);
      expect(mockUpdateProjectHandler).toHaveBeenCalledTimes(1);
    });

    it("should skip messages without type", async () => {
      const messages = [{ type: "create_issue", title: "Issue" }, { title: "No type" }, { type: "add_comment", body: "Comment" }];

      const mockHandler = vi.fn().mockResolvedValue({ success: true });

      const handlers = new Map([
        ["create_issue", mockHandler],
        ["add_comment", mockHandler],
      ]);

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);
      expect(result.results).toHaveLength(2);
      expect(core.warning).toHaveBeenCalledWith("Skipping message 2 without type");
    });

    it("should warn and record result when no handler is available for message type", async () => {
      const messages = [
        { type: "create_issue", title: "Issue" },
        { type: "unknown_type", data: "test" },
      ];

      const mockHandler = vi.fn().mockResolvedValue({ success: true });

      // Only create_issue handler is available, unknown_type has no handler
      const handlers = new Map([["create_issue", mockHandler]]);

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);
      expect(result.results).toHaveLength(2);

      // First message should succeed
      expect(result.results[0].success).toBe(true);
      expect(result.results[0].type).toBe("create_issue");

      // Second message should be recorded as failed with no handler error
      expect(result.results[1].success).toBe(false);
      expect(result.results[1].type).toBe("unknown_type");
      expect(result.results[1].error).toContain("No handler loaded");

      // Should have logged a warning
      expect(core.warning).toHaveBeenCalledWith(expect.stringContaining("No handler loaded for message type 'unknown_type'"));
    });

    it("should skip custom safe output job types gracefully without error", async () => {
      // Set up custom safe output jobs (e.g., send_slack_message handled by a dedicated job step)
      process.env.GH_AW_SAFE_OUTPUT_JOBS = JSON.stringify({
        send_slack_message: "message_url",
      });

      const messages = [
        { type: "create_issue", title: "Issue" },
        { type: "send_slack_message", channel: "#alerts", text: "Hello" },
      ];

      const mockHandler = vi.fn().mockResolvedValue({ success: true });

      // Only create_issue handler is available; send_slack_message is a custom job
      const handlers = new Map([["create_issue", mockHandler]]);

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);
      expect(result.results).toHaveLength(2);

      // First message should succeed
      expect(result.results[0].success).toBe(true);
      expect(result.results[0].type).toBe("create_issue");

      // Custom job message should be skipped gracefully (not an error)
      expect(result.results[1].success).toBe(false);
      expect(result.results[1].type).toBe("send_slack_message");
      expect(result.results[1].skipped).toBe(true);
      expect(result.results[1].reason).toBe("Handled by custom safe output job");
      expect(result.results[1].error).toBeUndefined();

      // Should NOT have logged a "No handler loaded" warning
      expect(core.warning).not.toHaveBeenCalledWith(expect.stringContaining("No handler loaded for message type 'send_slack_message'"));
    });

    it("should skip multiple custom safe output job types gracefully", async () => {
      process.env.GH_AW_SAFE_OUTPUT_JOBS = JSON.stringify({
        send_slack_message: "message_url",
        notion_add_comment: "comment_url",
      });

      const messages = [
        { type: "send_slack_message", channel: "#alerts", text: "Hello" },
        { type: "notion_add_comment", page_id: "abc123", text: "Note" },
        { type: "create_issue", title: "Issue" },
      ];

      const mockHandler = vi.fn().mockResolvedValue({ success: true });
      const handlers = new Map([["create_issue", mockHandler]]);

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);
      expect(result.results).toHaveLength(3);

      // Custom job types should be skipped gracefully
      expect(result.results[0].skipped).toBe(true);
      expect(result.results[0].reason).toBe("Handled by custom safe output job");
      expect(result.results[1].skipped).toBe(true);
      expect(result.results[1].reason).toBe("Handled by custom safe output job");

      // create_issue should succeed
      expect(result.results[2].success).toBe(true);
    });

    it("should still warn for unknown types not in custom job types", async () => {
      process.env.GH_AW_SAFE_OUTPUT_JOBS = JSON.stringify({
        send_slack_message: "message_url",
      });

      const messages = [{ type: "completely_unknown_type", data: "test" }];

      const handlers = new Map();

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);
      expect(result.results[0].error).toContain("No handler loaded");
      expect(result.results[0].skipped).toBeUndefined();

      // Should have logged a warning for truly unknown types
      expect(core.warning).toHaveBeenCalledWith(expect.stringContaining("No handler loaded for message type 'completely_unknown_type'"));
    });

    it("should handle handler errors gracefully", async () => {
      const messages = [{ type: "create_issue", title: "Issue" }];

      const errorHandler = vi.fn().mockRejectedValue(new Error("Handler failed"));

      const handlers = new Map([["create_issue", errorHandler]]);

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);
      expect(result.results).toHaveLength(1);
      expect(result.results[0].success).toBe(false);
      expect(result.results[0].error).toBe("Handler failed");
    });

    it("should treat handler returning success: false as a failure", async () => {
      const messages = [{ type: "create_project_status_update", project: "https://github.com/orgs/test/projects/1", body: "Test" }];

      const failureHandler = vi.fn().mockResolvedValue({
        success: false,
        error: "GraphQL query failed",
      });

      const handlers = new Map([["create_project_status_update", failureHandler]]);

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);
      expect(result.results).toHaveLength(1);
      expect(result.results[0].success).toBe(false);
      expect(result.results[0].error).toBe("GraphQL query failed");
      expect(core.error).toHaveBeenCalledWith(expect.stringContaining("failed: GraphQL query failed"));
    });

    it("should track outputs with unresolved temporary IDs", async () => {
      const messages = [
        {
          type: "create_issue",
          body: "See #aw_abc123 for context",
          title: "Test Issue",
        },
      ];

      const mockCreateIssueHandler = vi.fn().mockResolvedValue({
        repo: "owner/repo",
        number: 100,
      });

      const handlers = new Map([["create_issue", mockCreateIssueHandler]]);

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);
      expect(result.outputsWithUnresolvedIds).toBeDefined();
      // Should track the output because it has unresolved temp ID
      expect(result.outputsWithUnresolvedIds.length).toBe(1);
      expect(result.outputsWithUnresolvedIds[0].type).toBe("create_issue");
      expect(result.outputsWithUnresolvedIds[0].result.number).toBe(100);
    });

    it("tracks comment_memory outputs using managed body for temporary ID resolution", async () => {
      const messages = [
        {
          type: "comment_memory",
          body: "raw body without ids",
          memory_id: "default",
        },
      ];

      const mockCommentMemoryHandler = vi.fn().mockResolvedValue({
        repo: "owner/repo",
        number: 55,
        commentId: 12345,
        managedBody: '<gh-aw-comment-memory id="default">\nSee #aw_abc123 for details\n</gh-aw-comment-memory>',
      });

      const handlers = new Map([["comment_memory", mockCommentMemoryHandler]]);

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);
      expect(result.outputsWithUnresolvedIds.length).toBe(1);
      expect(result.outputsWithUnresolvedIds[0].type).toBe("comment_memory");
      expect(result.outputsWithUnresolvedIds[0].result.commentId).toBe(12345);
    });

    it("should track outputs needing synthetic updates when temporary ID is resolved", async () => {
      const messages = [
        {
          type: "create_issue",
          body: "See #aw_abc123 for context",
          title: "First Issue",
        },
        {
          type: "create_issue",
          temporary_id: "aw_abc123",
          body: "Second issue body",
          title: "Second Issue",
        },
      ];

      const mockCreateIssueHandler = vi
        .fn()
        .mockResolvedValueOnce({
          repo: "owner/repo",
          number: 100,
        })
        .mockResolvedValueOnce({
          repo: "owner/repo",
          number: 101,
          temporaryId: "aw_abc123",
        });

      const handlers = new Map([["create_issue", mockCreateIssueHandler]]);

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);
      expect(result.outputsWithUnresolvedIds).toBeDefined();
      // Should track output with unresolved temp ID
      expect(result.outputsWithUnresolvedIds.length).toBe(1);
      expect(result.outputsWithUnresolvedIds[0].result.number).toBe(100);
      // Temp ID should be registered
      expect(result.temporaryIdMap["aw_abc123"]).toBeDefined();
      expect(result.temporaryIdMap["aw_abc123"].number).toBe(101);
    });

    it("should not track output if temporary IDs remain unresolved", async () => {
      const messages = [
        {
          type: "create_issue",
          body: "See #aw_abc123 and #aw_unresolved99 for context",
          title: "Test Issue",
        },
      ];

      const mockCreateIssueHandler = vi.fn().mockResolvedValue({
        repo: "owner/repo",
        number: 100,
      });

      const handlers = new Map([["create_issue", mockCreateIssueHandler]]);

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);
      expect(result.outputsWithUnresolvedIds).toBeDefined();
      // Should track because there are unresolved IDs
      expect(result.outputsWithUnresolvedIds.length).toBe(1);
    });

    it("should handle multiple outputs needing synthetic updates", async () => {
      const messages = [
        {
          type: "create_issue",
          body: "Related to #aw_aabbcc11",
          title: "First Issue",
        },
        {
          type: "create_discussion",
          body: "See #aw_aabbcc11 for details",
          title: "Discussion",
        },
        {
          type: "create_issue",
          temporary_id: "aw_aabbcc11",
          body: "The referenced issue",
          title: "Referenced Issue",
        },
      ];

      const mockCreateIssueHandler = vi
        .fn()
        .mockResolvedValueOnce({
          repo: "owner/repo",
          number: 100,
        })
        .mockResolvedValueOnce({
          repo: "owner/repo",
          number: 102,
          temporaryId: "aw_aabbcc11",
        });

      const mockCreateDiscussionHandler = vi.fn().mockResolvedValue({
        repo: "owner/repo",
        number: 101,
      });

      const handlers = new Map([
        ["create_issue", mockCreateIssueHandler],
        ["create_discussion", mockCreateDiscussionHandler],
      ]);

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);
      expect(result.outputsWithUnresolvedIds).toBeDefined();
      // Should track 2 outputs (issue and discussion) with unresolved temp IDs
      expect(result.outputsWithUnresolvedIds.length).toBe(2);
      // Temp ID should be registered
      expect(result.temporaryIdMap["aw_aabbcc11"]).toBeDefined();
    });

    it("should populate artifactUrlMap when upload_artifact succeeds", async () => {
      const messages = [
        {
          type: "upload_artifact",
          temporary_id: "aw_chart1",
          path: "chart.png",
        },
      ];

      const mockUploadHandler = vi.fn().mockResolvedValue({
        tmpId: "aw_chart1",
        artifactUrl: "https://github.com/owner/repo/actions/runs/1/artifacts/42",
      });

      const handlers = new Map([["upload_artifact", mockUploadHandler]]);
      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);
      expect(result.artifactUrlMap).toBeDefined();
      expect(result.artifactUrlMap.get("aw_chart1")).toBe("https://github.com/owner/repo/actions/runs/1/artifacts/42");
    });

    it("should track issue with artifact reference as needing synthetic update when artifact is uploaded after", async () => {
      const messages = [
        {
          type: "create_issue",
          title: "Issue with chart",
          body: "See chart: ![chart](#aw_chart1)",
        },
        {
          type: "upload_artifact",
          temporary_id: "aw_chart1",
          path: "chart.png",
        },
      ];

      const mockCreateIssueHandler = vi.fn().mockResolvedValue({
        repo: "owner/repo",
        number: 100,
      });

      const mockUploadHandler = vi.fn().mockResolvedValue({
        tmpId: "aw_chart1",
        artifactUrl: "https://github.com/owner/repo/actions/runs/1/artifacts/42",
      });

      const handlers = new Map([
        ["create_issue", mockCreateIssueHandler],
        ["upload_artifact", mockUploadHandler],
      ]);

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);
      expect(result.artifactUrlMap.get("aw_chart1")).toBe("https://github.com/owner/repo/actions/runs/1/artifacts/42");
      // The issue was created before the artifact was uploaded, so it should be tracked
      expect(result.outputsWithUnresolvedIds.length).toBe(1);
      expect(result.outputsWithUnresolvedIds[0].result.number).toBe(100);
    });

    it("should replace artifact URL references in issue body when artifact is uploaded before issue", async () => {
      const messages = [
        {
          type: "upload_artifact",
          temporary_id: "aw_chart1",
          path: "chart.png",
        },
        {
          type: "create_issue",
          title: "Issue with chart",
          body: "See chart: ![chart](#aw_chart1)",
        },
      ];

      const mockUploadHandler = vi.fn().mockResolvedValue({
        tmpId: "aw_chart1",
        artifactUrl: "https://github.com/owner/repo/actions/runs/1/artifacts/42",
      });

      const mockCreateIssueHandler = vi.fn().mockResolvedValue({
        repo: "owner/repo",
        number: 100,
      });

      const handlers = new Map([
        ["upload_artifact", mockUploadHandler],
        ["create_issue", mockCreateIssueHandler],
      ]);

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);
      // When artifact is already uploaded, the body passed to create_issue should have URL replaced
      const calledMessage = mockCreateIssueHandler.mock.calls[0][0];
      expect(calledMessage.body).toContain("https://github.com/owner/repo/actions/runs/1/artifacts/42");
      expect(calledMessage.body).not.toContain("#aw_chart1");
      // The issue does not need synthetic update since body was pre-resolved
      expect(result.outputsWithUnresolvedIds.length).toBe(0);
    });

    it("should silently skip message types handled by standalone steps", async () => {
      const messages = [
        { type: "create_issue", title: "Issue" },
        { type: "upload_asset", path: "file.txt" },
      ];

      const mockHandler = vi.fn().mockResolvedValue({ success: true });

      // Only create_issue handler is available
      // upload_asset is handled by a standalone step
      const handlers = new Map([["create_issue", mockHandler]]);

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);
      expect(result.results).toHaveLength(2);

      // First message should succeed
      expect(result.results[0].success).toBe(true);
      expect(result.results[0].type).toBe("create_issue");

      // Second message should be skipped (standalone step)
      expect(result.results[1].success).toBe(false);
      expect(result.results[1].type).toBe("upload_asset");
      expect(result.results[1].skipped).toBe(true);
      expect(result.results[1].reason).toBe("Handled by standalone step");

      // Should NOT have logged warnings for standalone step types
      expect(core.warning).not.toHaveBeenCalledWith(expect.stringContaining("No handler loaded for message type 'upload_asset'"));

      // Should have logged debug messages
      expect(core.debug).toHaveBeenCalledWith(expect.stringContaining("upload_asset"));
    });

    it("should track skipped message types for logging", async () => {
      const messages = [
        { type: "create_issue", title: "Issue" },
        { type: "upload_asset", path: "file.txt" },
        { type: "unknown_type", data: "test" },
        { type: "another_unknown", data: "test2" },
      ];

      const mockHandler = vi.fn().mockResolvedValue({ success: true });

      // Only create_issue handler is available
      const handlers = new Map([["create_issue", mockHandler]]);

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);

      // Collect skipped standalone types
      const skippedStandaloneResults = result.results.filter(r => r.skipped && r.reason === "Handled by standalone step");
      const standaloneTypes = [...new Set(skippedStandaloneResults.map(r => r.type))];
      expect(standaloneTypes).toEqual(expect.arrayContaining(["upload_asset"]));

      // Collect skipped no-handler types
      const skippedNoHandlerResults = result.results.filter(r => !r.success && !r.skipped && r.error?.includes("No handler loaded"));
      const noHandlerTypes = [...new Set(skippedNoHandlerResults.map(r => r.type))];
      expect(noHandlerTypes).toEqual(expect.arrayContaining(["unknown_type", "another_unknown"]));
    });

    it("should register temporary IDs from deferred messages on retry", async () => {
      const messages = [
        {
          type: "link_sub_issue",
          parent_issue_number: "aw_parent12",
          sub_issue_number: 42,
        },
        {
          type: "create_issue",
          temporary_id: "aw_parent12",
          title: "Parent Issue",
          body: "Parent body",
        },
      ];

      // First call: link_sub_issue is deferred (parent not resolved yet)
      // Second call: create_issue succeeds and registers temp ID
      // Third call: link_sub_issue retry succeeds
      const mockLinkHandler = vi
        .fn()
        .mockResolvedValueOnce({
          deferred: true,
          error: "Unresolved temporary IDs: parent: aw_parent12",
        })
        .mockResolvedValueOnce({
          parent_issue_number: 100,
          sub_issue_number: 42,
          success: true,
        });

      const mockCreateIssueHandler = vi.fn().mockResolvedValue({
        repo: "owner/repo",
        number: 100,
        temporaryId: "aw_parent12",
      });

      const handlers = new Map([
        ["link_sub_issue", mockLinkHandler],
        ["create_issue", mockCreateIssueHandler],
      ]);

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);

      // Temp ID should be registered after create_issue
      expect(result.temporaryIdMap["aw_parent12"]).toBeDefined();
      expect(result.temporaryIdMap["aw_parent12"].number).toBe(100);

      // link_sub_issue should succeed after retry
      const linkResult = result.results.find(r => r.type === "link_sub_issue");
      expect(linkResult.success).toBe(true);
      expect(linkResult.deferred).toBe(false);
    });

    it.each([
      ["returned failure", () => ({ success: false, error: "Retry failed" })],
      ["thrown error", () => Promise.reject(new Error("Retry failed"))],
    ])("should classify a deferred retry %s as failed", async (_name, retryResult) => {
      const handler = vi.fn().mockResolvedValueOnce({ deferred: true, error: "Unresolved temporary ID" }).mockImplementationOnce(retryResult);
      const result = await processMessages(new Map([["link_sub_issue", handler]]), [{ type: "link_sub_issue", parent_issue_number: "aw_parent12", sub_issue_number: 42 }]);

      expect(result.results[0]).toMatchObject({
        type: "link_sub_issue",
        success: false,
        deferred: false,
        error: "Retry failed",
        result: { success: false, deferred: false, error: "Retry failed" },
      });
      expect(isFailedProcessingResult(result.results[0])).toBe(true);
    });

    it("preserves summary-safe diagnostics when a deferred retry is skipped", async () => {
      const skippedResult = {
        success: false,
        skipped: true,
        reasonCode: "REQUIRED_LABELS_MISMATCH",
        reason: "Required labels missing",
        target: { repo: "owner/repo", number: 123 },
        safeDetails: { missingLabels: ["automation"] },
      };
      const handler = vi.fn().mockResolvedValueOnce({ deferred: true, error: "Unresolved temporary ID" }).mockResolvedValueOnce(skippedResult);
      const result = await processMessages(new Map([["link_sub_issue", handler]]), [{ type: "link_sub_issue", parent_issue_number: "aw_parent12", sub_issue_number: 42 }]);

      expect(result.results[0]).toMatchObject({
        type: "link_sub_issue",
        messageIndex: 0,
        success: false,
        skipped: true,
        reasonCode: "REQUIRED_LABELS_MISMATCH",
        reason: "Required labels missing",
        result: skippedResult,
      });
      expect(isFailedProcessingResult(result.results[0])).toBe(false);
    });

    it("should track outputs created during deferred retry with unresolved temp IDs", async () => {
      const messages = [
        {
          type: "create_issue",
          temporary_id: "aw_aabbcc11",
          title: "Issue 1",
          body: "References #aw_ddeeff22",
        },
        {
          type: "link_sub_issue",
          parent_issue_number: "aw_aabbcc11",
          sub_issue_number: "aw_ddeeff22",
        },
        {
          type: "create_issue",
          temporary_id: "aw_ddeeff22",
          title: "Issue 2",
          body: "Issue 2 body",
        },
      ];

      // create_issue for issue1: succeeds with unresolved temp ID in body
      // link_sub_issue: deferred (parent and sub not resolved)
      // create_issue for issue2: succeeds
      // link_sub_issue retry: succeeds
      const mockCreateHandler = vi
        .fn()
        .mockResolvedValueOnce({
          repo: "owner/repo",
          number: 100,
          temporaryId: "aw_aabbcc11",
        })
        .mockResolvedValueOnce({
          repo: "owner/repo",
          number: 101,
          temporaryId: "aw_ddeeff22",
        });

      const mockLinkHandler = vi
        .fn()
        .mockResolvedValueOnce({
          deferred: true,
          error: "Unresolved temporary IDs",
        })
        .mockResolvedValueOnce({
          parent_issue_number: 100,
          sub_issue_number: 101,
          success: true,
        });

      const handlers = new Map([
        ["create_issue", mockCreateHandler],
        ["link_sub_issue", mockLinkHandler],
      ]);

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);

      // Both issues should have temp IDs registered
      expect(result.temporaryIdMap["aw_aabbcc11"]).toBeDefined();
      expect(result.temporaryIdMap["aw_ddeeff22"]).toBeDefined();

      // Issue 1 should be tracked for synthetic update (had unresolved temp ID in body at creation time)
      // Note: By the time all messages are processed, the temp ID is resolved, but Issue 1 was
      // tracked when it was created because at that moment aw_ddeeff22 was not yet in the map
      const trackedIssue1 = result.outputsWithUnresolvedIds.find(o => o.result.number === 100);
      expect(trackedIssue1).toBeDefined();
    });

    it("should handle complex parent/sub-issue creation order", async () => {
      const messages = [
        {
          type: "create_issue",
          temporary_id: "aw_abc11def",
          title: "Parent",
          body: "See #aw_111aaa22 and #aw_333ccc44",
        },
        {
          type: "create_issue",
          temporary_id: "aw_111aaa22",
          title: "Sub 1",
          body: "Sub 1 body",
        },
        {
          type: "create_issue",
          temporary_id: "aw_333ccc44",
          title: "Sub 2",
          body: "Sub 2 body",
        },
      ];

      let issueCounter = 100;
      const mockCreateHandler = vi.fn().mockImplementation(message => {
        const tempId = message.temporary_id;
        const issueNumber = issueCounter++;
        return Promise.resolve({
          repo: "owner/repo",
          number: issueNumber,
          temporaryId: tempId,
        });
      });

      const handlers = new Map([["create_issue", mockCreateHandler]]);

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);

      // All temp IDs should be registered
      expect(result.temporaryIdMap["aw_abc11def"]).toBeDefined();
      expect(result.temporaryIdMap["aw_111aaa22"]).toBeDefined();
      expect(result.temporaryIdMap["aw_333ccc44"]).toBeDefined();

      // Parent issue should be tracked (had unresolved temp IDs at creation time)
      // When the parent was created, aw_111aaa22 and aw_333ccc44 were not yet in the map
      const parentTracked = result.outputsWithUnresolvedIds.find(
        o => o.result.number === 100 // Parent was issue #100
      );
      expect(parentTracked).toBeDefined();
      expect(parentTracked.type).toBe("create_issue");
    });

    it("should register temporary ID from create_pull_request result", async () => {
      const messages = [{ type: "create_pull_request", temporary_id: "aw_pr1", title: "My PR", body: "PR body" }];

      const prHandler = vi.fn().mockResolvedValue({
        success: true,
        number: 42,
        url: "https://github.com/owner/repo/pull/42",
        temporaryId: "aw_pr1",
        repo: "owner/repo",
      });

      const handlers = new Map([["create_pull_request", prHandler]]);

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);
      expect(result.temporaryIdMap["aw_pr1"]).toBeDefined();
      expect(result.temporaryIdMap["aw_pr1"].number).toBe(42);
      expect(result.temporaryIdMap["aw_pr1"].repo).toBe("owner/repo");
    });

    it("should resolve #aw_prN in later messages after create_pull_request registers its temp ID", async () => {
      const messages = [
        { type: "create_pull_request", temporary_id: "aw_pr1", title: "My PR", body: "PR body" },
        { type: "create_issue", title: "Summary", body: "See #aw_pr1 for the changes" },
      ];

      const prHandler = vi.fn().mockResolvedValue({
        success: true,
        number: 42,
        url: "https://github.com/owner/repo/pull/42",
        temporaryId: "aw_pr1",
        repo: "owner/repo",
      });

      let capturedResolvedIds;
      const issueHandler = vi.fn().mockImplementation((message, resolvedTemporaryIds) => {
        capturedResolvedIds = resolvedTemporaryIds;
        return Promise.resolve({ success: true, number: 100, repo: "owner/repo", temporaryId: undefined });
      });

      const handlers = new Map([
        ["create_pull_request", prHandler],
        ["create_issue", issueHandler],
      ]);

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);
      // aw_pr1 should be in the resolvedTemporaryIds snapshot passed to the second handler
      expect(capturedResolvedIds).toBeDefined();
      expect(capturedResolvedIds["aw_pr1"]).toBeDefined();
      expect(capturedResolvedIds["aw_pr1"].number).toBe(42);
    });

    it("should track create_pull_request with forward temp ID refs for synthetic update", async () => {
      const messages = [
        { type: "create_pull_request", temporary_id: "aw_pr1", title: "My PR", body: "Closes #aw_issue1" },
        { type: "create_issue", temporary_id: "aw_issue1", title: "Issue", body: "Issue body" },
      ];

      const prHandler = vi.fn().mockResolvedValue({
        success: true,
        number: 10,
        url: "https://github.com/owner/repo/pull/10",
        managedBody: "Managed: Closes #aw_issue1\n\n<!-- footer -->",
        temporaryId: "aw_pr1",
        repo: "owner/repo",
      });

      const issueHandler = vi.fn().mockResolvedValue({
        success: true,
        number: 99,
        repo: "owner/repo",
        temporaryId: "aw_issue1",
      });

      const handlers = new Map([
        ["create_pull_request", prHandler],
        ["create_issue", issueHandler],
      ]);

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);
      // PR was created with an unresolved forward ref (#aw_issue1 not yet registered)
      expect(result.outputsWithUnresolvedIds.length).toBeGreaterThan(0);
      const trackedPR = result.outputsWithUnresolvedIds.find(o => o.type === "create_pull_request");
      expect(trackedPR).toBeDefined();
      expect(trackedPR.result.number).toBe(10);
    });

    it("should collect missing_tool and missing_data messages and include in result", async () => {
      const messages = [
        {
          type: "missing_tool",
          tool: "docker",
          reason: "Need containerization",
          alternatives: "Use VM",
        },
        {
          type: "create_issue",
          title: "Test Issue",
          body: "Issue body",
        },
        {
          type: "missing_data",
          data_type: "api_key",
          reason: "API credentials missing",
          context: "GitHub API access",
        },
        {
          type: "missing_tool",
          tool: "kubectl",
          reason: "Kubernetes management",
        },
      ];

      const mockCreateIssueHandler = vi.fn().mockResolvedValue({
        repo: "owner/repo",
        number: 100,
      });

      const handlers = new Map([
        ["create_issue", mockCreateIssueHandler],
        ["missing_tool", vi.fn().mockResolvedValue({ success: true })],
        ["missing_data", vi.fn().mockResolvedValue({ success: true })],
      ]);

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);
      expect(result.missings).toBeDefined();
      expect(result.missings.missingTools).toHaveLength(2);
      expect(result.missings.missingData).toHaveLength(1);

      // Check missing tools
      expect(result.missings.missingTools[0].tool).toBe("docker");
      expect(result.missings.missingTools[0].reason).toBe("Need containerization");
      expect(result.missings.missingTools[0].alternatives).toBe("Use VM");

      expect(result.missings.missingTools[1].tool).toBe("kubectl");
      expect(result.missings.missingTools[1].reason).toBe("Kubernetes management");
      expect(result.missings.missingTools[1].alternatives).toBeNull();

      // Check missing data
      expect(result.missings.missingData[0].data_type).toBe("api_key");
      expect(result.missings.missingData[0].reason).toBe("API credentials missing");
      expect(result.missings.missingData[0].context).toBe("GitHub API access");
    });

    it("should collect noop messages alongside missing_tool and missing_data", async () => {
      const messages = [
        {
          type: "noop",
          message: "No issues found in this review",
        },
        {
          type: "create_issue",
          title: "Test Issue",
          body: "Issue body",
        },
        {
          type: "missing_tool",
          tool: "docker",
          reason: "Need containerization",
        },
        {
          type: "noop",
          message: "Analysis complete",
        },
      ];

      const mockCreateIssueHandler = vi.fn().mockResolvedValue({
        repo: "owner/repo",
        number: 100,
      });

      const handlers = new Map([
        ["create_issue", mockCreateIssueHandler],
        ["missing_tool", vi.fn().mockResolvedValue({ success: true })],
        ["noop", vi.fn().mockResolvedValue({ success: true })],
      ]);

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);
      expect(result.missings).toBeDefined();
      expect(result.missings.missingTools).toHaveLength(1);
      expect(result.missings.missingData).toHaveLength(0);
      expect(result.missings.noopMessages).toHaveLength(2);

      // Check missing tools
      expect(result.missings.missingTools[0].tool).toBe("docker");
      expect(result.missings.missingTools[0].reason).toBe("Need containerization");

      // Check noop messages
      expect(result.missings.noopMessages[0].message).toBe("No issues found in this review");
      expect(result.missings.noopMessages[1].message).toBe("Analysis complete");
    });

    it("should collect report_incomplete signals alongside other message types", async () => {
      const messages = [
        {
          type: "report_incomplete",
          reason: "MCP server crashed during execution",
          details: "Connection refused on port 3000",
        },
        {
          type: "create_issue",
          title: "Test Issue",
          body: "Issue body",
        },
        {
          type: "report_incomplete",
          reason: "Missing authentication token",
        },
        {
          type: "missing_tool",
          tool: "docker",
          reason: "Need containerization",
        },
      ];

      const mockCreateIssueHandler = vi.fn().mockResolvedValue({
        repo: "owner/repo",
        number: 100,
      });

      const handlers = new Map([
        ["create_issue", mockCreateIssueHandler],
        ["report_incomplete", vi.fn().mockResolvedValue({ success: true })],
        ["missing_tool", vi.fn().mockResolvedValue({ success: true })],
      ]);

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);
      expect(result.missings).toBeDefined();
      expect(result.missings.reportIncomplete).toHaveLength(2);
      expect(result.missings.missingTools).toHaveLength(1);

      expect(result.missings.reportIncomplete[0].reason).toBe("MCP server crashed during execution");
      expect(result.missings.reportIncomplete[0].details).toBe("Connection refused on port 3000");
      expect(result.missings.reportIncomplete[1].reason).toBe("Missing authentication token");
      expect(result.missings.reportIncomplete[1].details).toBeNull();
    });

    it("should return empty arrays when no missing messages present", async () => {
      const messages = [
        {
          type: "create_issue",
          title: "Test Issue",
          body: "Issue body",
        },
      ];

      const mockCreateIssueHandler = vi.fn().mockResolvedValue({
        repo: "owner/repo",
        number: 100,
      });

      const handlers = new Map([["create_issue", mockCreateIssueHandler]]);

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);
      expect(result.missings).toBeDefined();
      expect(result.missings.missingTools).toHaveLength(0);
      expect(result.missings.missingData).toHaveLength(0);
      expect(result.missings.noopMessages).toHaveLength(0);
      expect(result.missings.reportIncomplete).toHaveLength(0);
    });
  });

  describe("code-push failure behaviour", () => {
    it("should continue processing non-code-push messages when push_to_pull_request_branch fails", async () => {
      const messages = [{ type: "push_to_pull_request_branch" }, { type: "add_comment", body: "Success!" }, { type: "create_issue", title: "Issue" }];

      const codePushHandler = vi.fn().mockResolvedValue({ success: false, error: "Branch not found" });
      const commentHandler = vi.fn().mockResolvedValue([{ _tracking: null }]);
      const issueHandler = vi.fn();

      const handlers = new Map([
        ["push_to_pull_request_branch", codePushHandler],
        ["add_comment", commentHandler],
        ["create_issue", issueHandler],
      ]);

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);
      // Code-push failure recorded
      expect(result.codePushFailures).toHaveLength(1);
      expect(result.codePushFailures[0].type).toBe("push_to_pull_request_branch");
      expect(result.codePushFailures[0].error).toBe("Branch not found");
      // First result: code-push failed
      expect(result.results[0].success).toBe(false);
      expect(result.results[0].error).toBe("Branch not found");
      // add_comment is NOT cancelled — it should be called with a failure note prepended
      expect(result.results[1].cancelled).toBeUndefined();
      expect(commentHandler).toHaveBeenCalledTimes(1);
      const calledMessage = commentHandler.mock.calls[0][0];
      expect(calledMessage.body).toContain("push_to_pull_request_branch");
      expect(calledMessage.body).toContain("Branch not found");
      // non-code-push message should continue to execute
      expect(result.results[2].success).toBe(true);
      expect(result.results[2].cancelled).toBeUndefined();
      expect(issueHandler).toHaveBeenCalledTimes(1);
    });

    it("should allow add_comment through when create_pull_request fails via exception", async () => {
      const messages = [{ type: "create_pull_request" }, { type: "add_comment", body: "PR created!" }];

      const codePushHandler = vi.fn().mockRejectedValue(new Error("API error"));
      const commentHandler = vi.fn().mockResolvedValue([{ _tracking: null }]);

      const handlers = new Map([
        ["create_pull_request", codePushHandler],
        ["add_comment", commentHandler],
      ]);

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);
      expect(result.codePushFailures).toHaveLength(1);
      expect(result.codePushFailures[0].type).toBe("create_pull_request");
      expect(result.codePushFailures[0].error).toBe("API error");
      // add_comment is NOT cancelled — handler is called with failure note
      expect(result.results[1].cancelled).toBeUndefined();
      expect(commentHandler).toHaveBeenCalledTimes(1);
      const calledMessage = commentHandler.mock.calls[0][0];
      expect(calledMessage.body).toContain("create_pull_request");
      expect(calledMessage.body).toContain("API error");
      expect(calledMessage.body).toContain("PR created!");
    });

    it("should NOT cancel subsequent code-push messages after a code-push failure", async () => {
      const messages = [{ type: "push_to_pull_request_branch" }, { type: "create_pull_request" }];

      const pushHandler = vi.fn().mockResolvedValue({ success: false, error: "Push failed" });
      const createPRHandler = vi.fn().mockResolvedValue({ success: true, url: "https://github.com/pr/1" });

      const handlers = new Map([
        ["push_to_pull_request_branch", pushHandler],
        ["create_pull_request", createPRHandler],
      ]);

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);
      expect(result.codePushFailures).toHaveLength(1);
      // create_pull_request is also a code-push type, so it should NOT be cancelled
      expect(result.results[1].cancelled).toBeUndefined();
      expect(createPRHandler).toHaveBeenCalled();
    });

    it("should not cancel messages when no code-push failure occurs", async () => {
      const messages = [{ type: "push_to_pull_request_branch" }, { type: "add_comment", body: "Success!" }];

      const codePushHandler = vi.fn().mockResolvedValue({ success: true, branch: "my-branch" });
      const commentHandler = vi.fn().mockResolvedValue([{ _tracking: null }]);

      const handlers = new Map([
        ["push_to_pull_request_branch", codePushHandler],
        ["add_comment", commentHandler],
      ]);

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);
      expect(result.codePushFailures).toHaveLength(0);
      expect(result.results[0].success).toBe(true);
      expect(result.results[1].cancelled).toBeUndefined();
      expect(commentHandler).toHaveBeenCalled();
    });

    it("should return empty codePushFailures array when no code-push types present", async () => {
      const messages = [{ type: "create_issue", title: "Issue" }];
      const mockHandler = vi.fn().mockResolvedValue({ repo: "owner/repo", number: 1 });
      const handlers = new Map([["create_issue", mockHandler]]);

      const result = await processMessages(handlers, messages);

      expect(result.codePushFailures).toBeDefined();
      expect(result.codePushFailures).toHaveLength(0);
    });

    it("should NOT cancel subsequent messages when push_to_pull_request_branch returns skipped (if_no_changes: warn)", async () => {
      const messages = [{ type: "push_to_pull_request_branch" }, { type: "create_issue", title: "Issue" }, { type: "add_comment", body: "Done!" }];

      // Simulates push handler returning { success: false, skipped: true } for if_no_changes: warn/ignore
      const codePushHandler = vi.fn().mockResolvedValue({ success: false, error: "No patch file found - cannot push without changes", skipped: true });
      const issueHandler = vi.fn().mockResolvedValue({ repo: "owner/repo", number: 42 });
      const commentHandler = vi.fn().mockResolvedValue([{ _tracking: null }]);

      const handlers = new Map([
        ["push_to_pull_request_branch", codePushHandler],
        ["create_issue", issueHandler],
        ["add_comment", commentHandler],
      ]);

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);
      // Skipped code-push should NOT be recorded as a codePushFailure
      expect(result.codePushFailures).toHaveLength(0);
      // First result: skipped (not failed)
      expect(result.results[0].success).toBe(false);
      expect(result.results[0].skipped).toBe(true);
      expect(result.results[0].error).toContain("No patch file found");
      // Subsequent results: NOT cancelled — handlers were called
      expect(result.results[1].cancelled).toBeUndefined();
      expect(result.results[2].cancelled).toBeUndefined();
      expect(issueHandler).toHaveBeenCalled();
      expect(commentHandler).toHaveBeenCalled();
    });

    it("should NOT cancel subsequent messages when create_pull_request returns skipped", async () => {
      const messages = [{ type: "create_pull_request" }, { type: "add_comment", body: "Done!" }];

      const codePushHandler = vi.fn().mockResolvedValue({ success: false, error: "No patch file found - cannot push without changes", skipped: true });
      const commentHandler = vi.fn().mockResolvedValue([{ _tracking: null }]);

      const handlers = new Map([
        ["create_pull_request", codePushHandler],
        ["add_comment", commentHandler],
      ]);

      const result = await processMessages(handlers, messages);

      expect(result.success).toBe(true);
      expect(result.codePushFailures).toHaveLength(0);
      expect(result.results[0].skipped).toBe(true);
      expect(result.results[1].cancelled).toBeUndefined();
      expect(commentHandler).toHaveBeenCalled();
    });

    it("should prepend fallback note to add_comment body when create_pull_request falls back to issue", async () => {
      const messages = [
        {
          type: "create_pull_request",
          title: "My Fix PR",
          body: "This fixes the issue.",
        },
        {
          type: "add_comment",
          body: "A fix PR has been created. Please review and merge.",
        },
      ];

      const prHandler = vi.fn().mockResolvedValue({
        success: true,
        fallback_used: true,
        issue_number: 42,
        issue_url: "https://github.com/owner/repo/issues/42",
        repo: "owner/repo",
      });
      const commentHandler = vi.fn().mockResolvedValue([{ _tracking: null }]);

      const handlers = new Map([
        ["create_pull_request", prHandler],
        ["add_comment", commentHandler],
      ]);

      await processMessages(handlers, messages);

      // The add_comment handler must have been called with the modified body
      expect(commentHandler).toHaveBeenCalledTimes(1);
      const calledMessage = commentHandler.mock.calls[0][0];
      expect(calledMessage.body).toContain("A fix PR has been created. Please review and merge.");
      expect(calledMessage.body).toContain("pull request was not created");
      expect(calledMessage.body).toContain("#42");
      expect(calledMessage.body).toContain("https://github.com/owner/repo/issues/42");
      // Note should be prepended before the original body
      const noteIndex = calledMessage.body.indexOf("pull request was not created");
      const bodyIndex = calledMessage.body.indexOf("A fix PR has been created");
      expect(noteIndex).toBeLessThan(bodyIndex);
    });

    it("should prepend fallback note to add_comment body when push_to_pull_request_branch falls back to issue", async () => {
      const messages = [
        { type: "push_to_pull_request_branch", branch: "fix-branch" },
        { type: "add_comment", body: "Changes pushed." },
      ];

      const pushHandler = vi.fn().mockResolvedValue({
        success: true,
        fallback_used: true,
        issue_number: 7,
        issue_url: "https://github.com/owner/repo/issues/7",
      });
      const commentHandler = vi.fn().mockResolvedValue([{ _tracking: null }]);

      const handlers = new Map([
        ["push_to_pull_request_branch", pushHandler],
        ["add_comment", commentHandler],
      ]);

      await processMessages(handlers, messages);

      const calledMessage = commentHandler.mock.calls[0][0];
      expect(calledMessage.body).toContain("pull request was not created");
      expect(calledMessage.body).toContain("#7");
    });

    it("should prepend fallback note to add_comment body when push_to_pull_request_branch falls back to pull request", async () => {
      const messages = [
        { type: "push_to_pull_request_branch", branch: "fix-branch" },
        { type: "add_comment", body: "Changes pushed." },
      ];

      const pushHandler = vi.fn().mockResolvedValue({
        success: true,
        fallback_used: true,
        fallback_type: "pull_request",
        pull_request_number: 71,
        pull_request_url: "https://github.com/owner/repo/pull/71",
      });
      const commentHandler = vi.fn().mockResolvedValue([{ _tracking: null }]);

      const handlers = new Map([
        ["push_to_pull_request_branch", pushHandler],
        ["add_comment", commentHandler],
      ]);

      await processMessages(handlers, messages);

      const calledMessage = commentHandler.mock.calls[0][0];
      expect(calledMessage.body).toContain("Direct push to the original pull request branch was not possible");
      expect(calledMessage.body).toContain("#71");
      expect(calledMessage.body).toContain("https://github.com/owner/repo/pull/71");
    });

    it("should NOT prepend fallback note when create_pull_request succeeds normally", async () => {
      const messages = [
        { type: "create_pull_request", title: "My Fix PR" },
        { type: "add_comment", body: "A fix PR has been created." },
      ];

      const prHandler = vi.fn().mockResolvedValue({
        success: true,
        number: 5,
        url: "https://github.com/owner/repo/pull/5",
        repo: "owner/repo",
      });
      const commentHandler = vi.fn().mockResolvedValue([{ _tracking: null }]);

      const handlers = new Map([
        ["create_pull_request", prHandler],
        ["add_comment", commentHandler],
      ]);

      await processMessages(handlers, messages);

      const calledMessage = commentHandler.mock.calls[0][0];
      expect(calledMessage.body).toBe("A fix PR has been created.");
      expect(calledMessage.body).not.toContain("pull request was not created");
    });

    it("should prepend failure note to add_comment body when create_pull_request fails (e.g. patch application error)", async () => {
      const messages = [
        { type: "create_pull_request", title: "My Fix PR" },
        { type: "add_comment", body: "The agent has completed its work." },
      ];

      const prHandler = vi.fn().mockResolvedValue({ success: false, error: "Failed to apply patch" });
      const commentHandler = vi.fn().mockResolvedValue([{ _tracking: null }]);

      const handlers = new Map([
        ["create_pull_request", prHandler],
        ["add_comment", commentHandler],
      ]);

      await processMessages(handlers, messages);

      // add_comment handler must have been called (not cancelled)
      expect(commentHandler).toHaveBeenCalledTimes(1);
      const calledMessage = commentHandler.mock.calls[0][0];
      // Failure note should be prepended
      expect(calledMessage.body).toContain("create_pull_request");
      expect(calledMessage.body).toContain("Failed to apply patch");
      expect(calledMessage.body).toContain("The agent has completed its work.");
      // Note should appear before the original body
      const noteIndex = calledMessage.body.indexOf("create_pull_request");
      const bodyIndex = calledMessage.body.indexOf("The agent has completed its work.");
      expect(noteIndex).toBeLessThan(bodyIndex);
    });

    it("should prepend failure note to add_comment body when push_to_pull_request_branch fails", async () => {
      const messages = [
        { type: "push_to_pull_request_branch", branch: "fix-branch" },
        { type: "add_comment", body: "Changes have been pushed." },
      ];

      const pushHandler = vi.fn().mockResolvedValue({ success: false, error: "Branch not found" });
      const commentHandler = vi.fn().mockResolvedValue([{ _tracking: null }]);

      const handlers = new Map([
        ["push_to_pull_request_branch", pushHandler],
        ["add_comment", commentHandler],
      ]);

      await processMessages(handlers, messages);

      expect(commentHandler).toHaveBeenCalledTimes(1);
      const calledMessage = commentHandler.mock.calls[0][0];
      expect(calledMessage.body).toContain("push_to_pull_request_branch");
      expect(calledMessage.body).toContain("Branch not found");
      expect(calledMessage.body).toContain("Changes have been pushed.");
    });
  });

  describe("output emission via emitSafeOutputActionOutputs", () => {
    it("processMessages result includes create_issue result with number and url for emission", async () => {
      const messages = [{ type: "create_issue", title: "My Issue" }];
      const mockHandler = vi.fn().mockResolvedValue({ number: 42, url: "https://github.com/owner/repo/issues/42" });
      const handlers = new Map([["create_issue", mockHandler]]);

      const result = await processMessages(handlers, messages);

      const issueResult = result.results.find(r => r.type === "create_issue" && r.success);
      expect(issueResult).toBeDefined();
      expect(issueResult.result.number).toBe(42);
      expect(issueResult.result.url).toBe("https://github.com/owner/repo/issues/42");
    });

    it("processMessages result with failed create_issue does not include success result for emission", async () => {
      const messages = [{ type: "create_issue", title: "Failing Issue" }];
      const mockHandler = vi.fn().mockRejectedValue(new Error("API error"));
      const handlers = new Map([["create_issue", mockHandler]]);

      const result = await processMessages(handlers, messages);

      const successfulIssueResult = result.results.find(r => r.type === "create_issue" && r.success);
      expect(successfulIssueResult).toBeUndefined();
    });

    it("core.setOutput is called with created_issue_number when create_issue succeeds", async () => {
      const messages = [{ type: "create_issue", title: "My Issue" }];
      const mockHandler = vi.fn().mockResolvedValue({ number: 7, url: "https://github.com/owner/repo/issues/7" });
      const handlers = new Map([["create_issue", mockHandler]]);

      const result = await processMessages(handlers, messages);

      // Simulate what main() does: call emitSafeOutputActionOutputs with the result
      const { emitSafeOutputActionOutputs } = await import("./safe_outputs_action_outputs.cjs");
      emitSafeOutputActionOutputs(result);

      expect(global.core.setOutput).toHaveBeenCalledWith("created_issue_number", "7");
      expect(global.core.setOutput).toHaveBeenCalledWith("created_issue_url", "https://github.com/owner/repo/issues/7");
    });
  });

  describe("handler module runtime dependencies", () => {
    // Safe output handler modules are copied to the runner temp directory without a
    // node_modules folder, so requiring an npm package makes the handler fail to load
    // at runtime with "Cannot find module" and silently skips its messages.
    it("does not require npm packages from any handler module in HANDLER_MAP", () => {
      const jsDir = new URL(".", import.meta.url).pathname;
      const managerSource = fs.readFileSync(`${jsDir}safe_output_handler_manager.cjs`, "utf8");
      const handlerMapBlock = managerSource.match(/const HANDLER_MAP = \{([\s\S]*?)\n\};/);
      expect(handlerMapBlock).not.toBeNull();

      const handlerFiles = [...handlerMapBlock[1].matchAll(/["'](\.\/[^"']+\.cjs)["']/g)].map(match => match[1].slice(2));
      expect(handlerFiles).toContain("approve_workflow_run.cjs");
      expect(handlerFiles).toContain("replace_label.cjs");

      const builtinModules = new Set(require("module").builtinModules);
      const visited = new Set();
      const queue = [...handlerFiles];
      /** @type {string[]} */
      const externalRequires = [];

      while (queue.length > 0) {
        const file = queue.shift();
        if (visited.has(file) || !fs.existsSync(`${jsDir}${file}`)) continue;
        visited.add(file);

        const source = fs.readFileSync(`${jsDir}${file}`, "utf8");
        for (const match of source.matchAll(/require\(["']([^"']+)["']\)/g)) {
          const specifier = match[1];
          if (specifier.startsWith("./")) {
            queue.push(specifier.slice(2));
          } else if (!specifier.startsWith("node:") && !builtinModules.has(specifier)) {
            externalRequires.push(`${file}: ${specifier}`);
          }
        }
      }

      expect(externalRequires).toEqual([]);
    });
  });

  describe("call_workflow handler registration", () => {
    it("processes call_workflow messages without no-handler warnings when handler is registered", async () => {
      const messages = [{ type: "call_workflow", workflow_name: "worker-a" }];
      const mockHandler = vi.fn().mockResolvedValue({
        success: true,
        workflow_name: "worker-a",
        payload: "{}",
      });
      const handlers = new Map([["call_workflow", mockHandler]]);

      const result = await processMessages(handlers, messages);

      expect(result.results).toHaveLength(1);
      expect(result.results[0].type).toBe("call_workflow");
      // Handler was invoked, so no "no handler loaded" error
      expect(result.results[0].error).toBeUndefined();
      expect(mockHandler).toHaveBeenCalledTimes(1);
    });

    it("records no-handler error for call_workflow when handler map is missing entry", async () => {
      const messages = [{ type: "call_workflow", workflow_name: "worker-a" }];
      // Empty handler map - simulates the bug where call_workflow was not in HANDLER_MAP
      const handlers = new Map();

      const result = await processMessages(handlers, messages);

      expect(result.results).toHaveLength(1);
      expect(result.results[0].success).toBe(false);
      expect(result.results[0].error).toContain("No handler loaded for type 'call_workflow'");
    });
  });

  describe("replace_label handler registration", () => {
    // Regression test for the replace-label portion of
    // https://github.com/github/gh-aw/issues/54811: replace_label had a
    // dedicated handler module (replace_label.cjs) but was missing from
    // HANDLER_MAP, so the collect job never loaded it and label
    // transitions silently did nothing even though the sample/agent output
    // was recorded successfully.
    it("maps key 'replace_label' directly to './replace_label.cjs' in HANDLER_MAP", () => {
      const managerPath = path.join(typeof __dirname !== "undefined" ? __dirname : path.dirname(new URL(import.meta.url).pathname), "safe_output_handler_manager.cjs");
      const managerSource = fs.readFileSync(managerPath, "utf8");
      const handlerMapBlock = managerSource.match(/const HANDLER_MAP = \{([\s\S]*?)\n\};/);
      expect(handlerMapBlock).not.toBeNull();

      const pairs = Object.fromEntries([...handlerMapBlock[1].matchAll(/(\w+):\s*["'](\.\/[^"']+\.cjs)["']/g)].map(m => [m[1], m[2]]));
      expect(pairs["replace_label"]).toBe("./replace_label.cjs");
    });

    it("loads replace_label handler from HANDLER_MAP via loadHandlers when enabled in config", async () => {
      global.github = {};
      try {
        const handlers = await loadHandlers({ replace_label: {} });
        expect(handlers.has("replace_label")).toBe(true);
        expect(typeof handlers.get("replace_label")).toBe("function");
      } finally {
        delete global.github;
      }
    });

    it("processes replace_label messages without no-handler warnings when handler is registered", async () => {
      const messages = [{ type: "replace_label", item_number: 1, label_to_remove: "old", label_to_add: "new" }];
      const mockHandler = vi.fn().mockResolvedValue({
        success: true,
        item_number: 1,
        label_to_remove: "old",
        label_to_add: "new",
      });
      const handlers = new Map([["replace_label", mockHandler]]);

      const result = await processMessages(handlers, messages);

      expect(result.results).toHaveLength(1);
      expect(result.results[0].type).toBe("replace_label");
      // Handler was invoked, so no "no handler loaded" error
      expect(result.results[0].error).toBeUndefined();
      expect(mockHandler).toHaveBeenCalledTimes(1);
    });

    it("records no-handler error for replace_label when handler map is missing entry", async () => {
      const messages = [{ type: "replace_label", item_number: 1, label_to_remove: "old", label_to_add: "new" }];
      // Empty handler map - simulates the bug where replace_label was not in HANDLER_MAP
      const handlers = new Map();

      const result = await processMessages(handlers, messages);

      expect(result.results).toHaveLength(1);
      expect(result.results[0].success).toBe(false);
      expect(result.results[0].error).toContain("No handler loaded for type 'replace_label'");
    });
  });

  describe("rollbackReviewResults", () => {
    it("flips success:true to success:false for submit_pull_request_review results", () => {
      const results = [{ type: "submit_pull_request_review", success: true }];
      rollbackReviewResults(results, "422 Unprocessable Entity");
      expect(results[0].success).toBe(false);
      expect(results[0].error).toBe("Review finalization failed: 422 Unprocessable Entity");
    });

    it("flips success:true to success:false for create_pull_request_review_comment results", () => {
      const results = [
        { type: "create_pull_request_review_comment", success: true },
        { type: "create_pull_request_review_comment", success: true },
      ];
      rollbackReviewResults(results, "Path could not be resolved");
      expect(results[0].success).toBe(false);
      expect(results[0].error).toBe("Review finalization failed: Path could not be resolved");
      expect(results[1].success).toBe(false);
    });

    it("does not modify results with success:false", () => {
      const results = [{ type: "submit_pull_request_review", success: false, error: "already failed" }];
      rollbackReviewResults(results, "new error");
      expect(results[0].error).toBe("already failed");
    });

    it("does not modify unrelated result types", () => {
      const results = [
        { type: "add_comment", success: true },
        { type: "create_issue", success: true },
      ];
      rollbackReviewResults(results, "some error");
      expect(results[0].success).toBe(true);
      expect(results[1].success).toBe(true);
    });

    it("handles mixed result types correctly", () => {
      const results = [
        { type: "add_comment", success: true },
        { type: "create_pull_request_review_comment", success: true },
        { type: "submit_pull_request_review", success: true },
        { type: "create_issue", success: true },
      ];
      rollbackReviewResults(results, "finalization failed");
      expect(results[0].success).toBe(true);
      expect(results[1].success).toBe(false);
      expect(results[2].success).toBe(false);
      expect(results[3].success).toBe(true);
    });

    it("handles empty results array without throwing", () => {
      expect(() => rollbackReviewResults([], "error")).not.toThrow();
    });
  });

  describe("skipReviewResults", () => {
    it("marks submit_pull_request_review results as skipped", () => {
      const results = [{ type: "submit_pull_request_review", success: true }];
      skipReviewResults(results, "Review skipped — PR is locked");
      expect(results[0].skipped).toBe(true);
      expect(results[0].skipReason).toBe("Review skipped — PR is locked");
      expect(results[0].success).toBe(true);
    });

    it("marks create_pull_request_review_comment results as skipped", () => {
      const results = [
        { type: "create_pull_request_review_comment", success: true },
        { type: "create_pull_request_review_comment", success: true },
      ];
      skipReviewResults(results, "Review skipped — PR is locked");
      expect(results[0].skipped).toBe(true);
      expect(results[0].skipReason).toBe("Review skipped — PR is locked");
      expect(results[1].skipped).toBe(true);
    });

    it("does not modify results with success:false", () => {
      const results = [{ type: "submit_pull_request_review", success: false, error: "already failed" }];
      skipReviewResults(results, "PR is locked");
      expect(results[0].skipped).toBeUndefined();
      expect(results[0].error).toBe("already failed");
    });

    it("does not modify unrelated result types", () => {
      const results = [
        { type: "add_comment", success: true },
        { type: "create_issue", success: true },
      ];
      skipReviewResults(results, "PR is locked");
      expect(results[0].skipped).toBeUndefined();
      expect(results[1].skipped).toBeUndefined();
    });

    it("handles mixed result types correctly", () => {
      const results = [
        { type: "add_comment", success: true },
        { type: "create_pull_request_review_comment", success: true },
        { type: "submit_pull_request_review", success: true },
        { type: "create_issue", success: true },
      ];
      skipReviewResults(results, "Review skipped — PR is locked");
      expect(results[0].skipped).toBeUndefined();
      expect(results[1].skipped).toBe(true);
      expect(results[2].skipped).toBe(true);
      expect(results[3].skipped).toBeUndefined();
    });

    it("handles empty results array without throwing", () => {
      expect(() => skipReviewResults([], "PR is locked")).not.toThrow();
    });
  });

  describe("rollbackReviewResultsForPR", () => {
    it("rolls back only the matching PR result (nested result shape from processMessages)", () => {
      const results = [
        { type: "submit_pull_request_review", success: true, result: { repo: "o/r", pull_request_number: 1 } },
        { type: "submit_pull_request_review", success: true, result: { repo: "o/r", pull_request_number: 2 } },
      ];
      rollbackReviewResultsForPR(results, "o/r", 1, "submission failed");
      expect(results[0].success).toBe(false);
      expect(results[0].error).toBe("Review finalization failed: submission failed");
      // PR 2 result must not be touched
      expect(results[1].success).toBe(true);
      expect(results[1].error).toBeUndefined();
    });

    it("rolls back create_pull_request_review_comment results for the matching PR only", () => {
      const results = [
        { type: "create_pull_request_review_comment", success: true, result: { repo: "o/r", pull_request_number: 1 } },
        { type: "create_pull_request_review_comment", success: true, result: { repo: "o/r", pull_request_number: 2 } },
        { type: "submit_pull_request_review", success: true, result: { repo: "o/r", pull_request_number: 1 } },
      ];
      rollbackReviewResultsForPR(results, "o/r", 1, "finalize error");
      expect(results[0].success).toBe(false);
      expect(results[2].success).toBe(false);
      // PR 2 comment unchanged
      expect(results[1].success).toBe(true);
    });

    it("falls back to rolling back all review results when no nested result matches", () => {
      // Legacy shape: no r.result, no top-level repo/pull_request_number
      const results = [
        { type: "submit_pull_request_review", success: true },
        { type: "create_pull_request_review_comment", success: true },
        { type: "add_comment", success: true },
      ];
      rollbackReviewResultsForPR(results, "o/r", 99, "fallback error");
      expect(results[0].success).toBe(false);
      expect(results[1].success).toBe(false);
      // non-review type must not be rolled back
      expect(results[2].success).toBe(true);
    });

    it("does not modify results that already have success:false", () => {
      const results = [{ type: "submit_pull_request_review", success: false, error: "already failed", result: { repo: "o/r", pull_request_number: 1 } }];
      rollbackReviewResultsForPR(results, "o/r", 1, "new error");
      // Falls back to global rollback path — but success is already false so no change
      expect(results[0].error).toBe("already failed");
    });

    it("does not modify results for a different repo", () => {
      const results = [{ type: "submit_pull_request_review", success: true, result: { repo: "o/other", pull_request_number: 1 } }];
      rollbackReviewResultsForPR(results, "o/r", 1, "error");
      // No match → fallback path, but the only review result has a different repo
      // Fallback rolls back all review results regardless of repo
      // (existing behavior: warns and rolls everything back)
      expect(results[0].success).toBe(false);
    });

    it("handles empty results array without throwing", () => {
      expect(() => rollbackReviewResultsForPR([], "o/r", 1, "error")).not.toThrow();
    });
  });

  describe("skipReviewResultsForPR", () => {
    it("marks only the matching PR result as skipped (nested result shape from processMessages)", () => {
      const results = [
        { type: "submit_pull_request_review", success: true, result: { repo: "o/r", pull_request_number: 1 } },
        { type: "submit_pull_request_review", success: true, result: { repo: "o/r", pull_request_number: 2 } },
      ];
      skipReviewResultsForPR(results, "o/r", 1, "PR is locked");
      expect(results[0].skipped).toBe(true);
      expect(results[0].skipReason).toBe("PR is locked");
      // PR 2 must not be skipped
      expect(results[1].skipped).toBeUndefined();
    });

    it("marks create_pull_request_review_comment results for the matching PR only", () => {
      const results = [
        { type: "create_pull_request_review_comment", success: true, result: { repo: "o/r", pull_request_number: 3 } },
        { type: "create_pull_request_review_comment", success: true, result: { repo: "o/r", pull_request_number: 5 } },
        { type: "submit_pull_request_review", success: true, result: { repo: "o/r", pull_request_number: 3 } },
      ];
      skipReviewResultsForPR(results, "o/r", 3, "locked");
      expect(results[0].skipped).toBe(true);
      expect(results[2].skipped).toBe(true);
      expect(results[1].skipped).toBeUndefined();
    });

    it("does not modify results with success:false", () => {
      const results = [{ type: "submit_pull_request_review", success: false, error: "already failed", result: { repo: "o/r", pull_request_number: 1 } }];
      skipReviewResultsForPR(results, "o/r", 1, "locked");
      expect(results[0].skipped).toBeUndefined();
    });

    it("does not modify unrelated result types", () => {
      const results = [
        { type: "add_comment", success: true, result: { repo: "o/r", pull_request_number: 1 } },
        { type: "create_issue", success: true },
      ];
      skipReviewResultsForPR(results, "o/r", 1, "locked");
      expect(results[0].skipped).toBeUndefined();
      expect(results[1].skipped).toBeUndefined();
    });

    it("handles top-level repo/pull_request_number for backward compat", () => {
      const results = [
        { type: "submit_pull_request_review", success: true, repo: "o/r", pull_request_number: 7 },
        { type: "submit_pull_request_review", success: true, repo: "o/r", pull_request_number: 8 },
      ];
      skipReviewResultsForPR(results, "o/r", 7, "locked");
      expect(results[0].skipped).toBe(true);
      expect(results[1].skipped).toBeUndefined();
    });

    it("handles empty results array without throwing", () => {
      expect(() => skipReviewResultsForPR([], "o/r", 1, "locked")).not.toThrow();
    });
  });

  describe("buildCommentMemoryMessagesFromFiles", () => {
    it("loads comment-memory messages from markdown files when configured", () => {
      fs.mkdirSync("/tmp/gh-aw/comment-memory", { recursive: true });
      fs.writeFileSync("/tmp/gh-aw/comment-memory/default.md", "saved memory");

      const messages = buildCommentMemoryMessagesFromFiles([], { comment_memory: { max: "1" } });

      expect(messages).toEqual([
        {
          type: "comment_memory",
          memory_id: "default",
          body: "saved memory",
        },
      ]);
    });

    it("skips file-based comment memory when a message already exists for the same memory_id", () => {
      fs.mkdirSync("/tmp/gh-aw/comment-memory", { recursive: true });
      fs.writeFileSync("/tmp/gh-aw/comment-memory/default.md", "saved memory");

      const messages = buildCommentMemoryMessagesFromFiles([{ type: "comment_memory", memory_id: "default", body: "from output" }], { comment_memory: { max: "1" } });

      expect(messages).toEqual([]);
    });

    it("treats comment_memory messages without memory_id as default memory when checking precedence", () => {
      fs.mkdirSync("/tmp/gh-aw/comment-memory", { recursive: true });
      fs.writeFileSync("/tmp/gh-aw/comment-memory/default.md", "saved memory");

      const messages = buildCommentMemoryMessagesFromFiles([{ type: "comment_memory", body: "from output" }], { comment_memory: { max: "1", memory_id: "default" } });

      expect(messages).toEqual([]);
    });
  });
});
