// @ts-check
import { describe, it, expect, beforeEach, vi } from "vitest";

// messages_core.cjs calls core.warning on parse failures - provide a stub
const mockCore = {
  info: vi.fn(),
  warning: vi.fn(),
  error: vi.fn(),
  setFailed: vi.fn(),
  setOutput: vi.fn(),
};
global.core = mockCore;

const {
  getRunStartedMessage,
  getRunSuccessMessage,
  getRunFailureMessage,
  getDetectionFailureBlocks,
  getDetectionWarningBlocks,
  getPullRequestCreatedMessage,
  getIssueCreatedMessage,
  getCommitPushedMessage,
} = require("./messages_run_status.cjs");
const { renderSizedReport } = require("./report_size.cjs");

const WORKFLOW = "My Workflow";
const RUN_URL = "https://github.com/owner/repo/actions/runs/99";

describe("messages_run_status", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    delete process.env.GH_AW_SAFE_OUTPUT_MESSAGES;
    delete process.env.GH_AW_DETECTION_REASON;
    delete process.env.GH_AW_DETECTION_THREAT_KINDS;
  });

  describe("getRunStartedMessage", () => {
    it("returns default template with all placeholders substituted", () => {
      const msg = getRunStartedMessage({ workflowName: WORKFLOW, runUrl: RUN_URL, eventType: "issue" });
      expect(msg).toBe(`🚀 [${WORKFLOW}](${RUN_URL}) has started processing this issue`);
    });

    it("supports different event types", () => {
      expect(getRunStartedMessage({ workflowName: WORKFLOW, runUrl: RUN_URL, eventType: "pull request" })).toContain("pull request");
      expect(getRunStartedMessage({ workflowName: WORKFLOW, runUrl: RUN_URL, eventType: "discussion" })).toContain("discussion");
    });

    it("uses workflow emoji when provided", () => {
      const msg = getRunStartedMessage({ workflowName: WORKFLOW, runUrl: RUN_URL, eventType: "issue", emoji: "🤖" });
      expect(msg).toBe(`🤖 [${WORKFLOW}](${RUN_URL}) has started processing this issue`);
    });

    it("uses custom template from config", () => {
      process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({ runStarted: "Custom: {workflow_name} started" });
      const msg = getRunStartedMessage({ workflowName: WORKFLOW, runUrl: RUN_URL, eventType: "issue" });
      expect(msg).toBe(`Custom: ${WORKFLOW} started`);
    });

    it("supports emoji placeholder in custom template", () => {
      process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({ runStarted: "{emoji} {workflow_name} started" });
      const msg = getRunStartedMessage({ workflowName: WORKFLOW, runUrl: RUN_URL, eventType: "issue", emoji: "🤖" });
      expect(msg).toBe(`🤖 ${WORKFLOW} started`);
    });

    it("substitutes camelCase keys as well as snake_case", () => {
      process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({ runStarted: "{workflowName} at {runUrl}" });
      const msg = getRunStartedMessage({ workflowName: WORKFLOW, runUrl: RUN_URL, eventType: "issue" });
      expect(msg).toBe(`${WORKFLOW} at ${RUN_URL}`);
    });
  });

  describe("getRunSuccessMessage", () => {
    it("returns default template with placeholders substituted", () => {
      const msg = getRunSuccessMessage({ workflowName: WORKFLOW, runUrl: RUN_URL });
      expect(msg).toBe(`✅ [${WORKFLOW}](${RUN_URL}) completed successfully!`);
    });

    it("uses custom template from config", () => {
      process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({ runSuccess: "Done: {workflow_name}" });
      const msg = getRunSuccessMessage({ workflowName: WORKFLOW, runUrl: RUN_URL });
      expect(msg).toBe(`Done: ${WORKFLOW}`);
    });

    it("ignores unrelated config keys and uses default", () => {
      process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({ runStarted: "overridden" });
      const msg = getRunSuccessMessage({ workflowName: WORKFLOW, runUrl: RUN_URL });
      expect(msg).toContain("completed successfully");
    });
  });

  describe("getRunFailureMessage", () => {
    it("returns default template with status substituted", () => {
      const msg = getRunFailureMessage({ workflowName: WORKFLOW, runUrl: RUN_URL, status: "failed" });
      expect(msg).toBe(`❌ [${WORKFLOW}](${RUN_URL}) failed. Please review the logs for details.`);
    });

    it("handles different status values", () => {
      expect(getRunFailureMessage({ workflowName: WORKFLOW, runUrl: RUN_URL, status: "was cancelled" })).toContain("was cancelled");
      expect(getRunFailureMessage({ workflowName: WORKFLOW, runUrl: RUN_URL, status: "timed out" })).toContain("timed out");
    });

    it("uses custom template from config", () => {
      process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({ runFailure: "FAILED: {workflow_name} - {status}" });
      const msg = getRunFailureMessage({ workflowName: WORKFLOW, runUrl: RUN_URL, status: "failed" });
      expect(msg).toBe(`FAILED: ${WORKFLOW} - failed`);
    });
  });

  describe("detection report blocks", () => {
    it.each([getDetectionFailureBlocks, getDetectionWarningBlocks])("renders category-only details in failure and warning report trees", buildBlocks => {
      process.env.GH_AW_PROMPTS_DIR = new URL("../md/", import.meta.url).pathname;
      const children = buildBlocks({ workflowName: WORKFLOW, runUrl: RUN_URL, reason: "threat_detected", threatKinds: "prompt_injection,malicious_patch,PRIVATE_TEXT" });
      const body = renderSizedReport({ type: "report", title: "Detection", children }).body;
      expect(body).toContain("Detected threat types: prompt injection, malicious patch.");
      expect(body).not.toContain("secret leak");
      expect(body).not.toContain("PRIVATE_TEXT");
      expect(JSON.stringify(children)).toContain('"agentTainted":false');
    });

    it("supplies safe category and description placeholders in configured failure templates", () => {
      process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({ detectionFailure: "{threat_kinds}: {reason_text}" });
      const blocks = getDetectionFailureBlocks({ workflowName: WORKFLOW, runUrl: RUN_URL, reason: "threat_detected", threatKinds: "secret_leak PRIVATE_TEXT" });
      const body = renderSizedReport({ type: "report", title: "Detection", children: blocks }).body;
      expect(body).toContain("secret\\_leak: Potential security threats were detected in the agent output. Detected threat types: secret leak.");
      expect(body).not.toContain("PRIVATE_TEXT");
    });
  });

  describe("getPullRequestCreatedMessage", () => {
    it("returns default template with item_number and item_url substituted", () => {
      const msg = getPullRequestCreatedMessage({ itemNumber: 42, itemUrl: "https://github.com/owner/repo/pull/42" });
      expect(msg).toBe("Pull request created: [#42](https://github.com/owner/repo/pull/42)");
    });

    it("uses custom template from config", () => {
      process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({ pullRequestCreated: "PR #{item_number} ready" });
      const msg = getPullRequestCreatedMessage({ itemNumber: 7, itemUrl: "https://github.com/owner/repo/pull/7" });
      expect(msg).toBe("PR #7 ready");
    });
  });

  describe("getIssueCreatedMessage", () => {
    it("returns default template with item_number and item_url substituted", () => {
      const msg = getIssueCreatedMessage({ itemNumber: 15, itemUrl: "https://github.com/owner/repo/issues/15" });
      expect(msg).toBe("Issue created: [#15](https://github.com/owner/repo/issues/15)");
    });

    it("uses custom template from config", () => {
      process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({ issueCreated: "New issue #{item_number}" });
      const msg = getIssueCreatedMessage({ itemNumber: 3, itemUrl: "https://github.com/owner/repo/issues/3" });
      expect(msg).toBe("New issue #3");
    });
  });

  describe("getCommitPushedMessage", () => {
    const SHA = "abc1234def5678901234567890123456789012ab";
    const SHORT = "abc1234";
    const COMMIT_URL = `https://github.com/owner/repo/commit/${SHA}`;

    it("returns default template with short_sha and commit_url substituted", () => {
      const msg = getCommitPushedMessage({ commitSha: SHA, shortSha: SHORT, commitUrl: COMMIT_URL });
      expect(msg).toBe(`Commit pushed: [\`${SHORT}\`](${COMMIT_URL})`);
    });

    it("uses custom template from config", () => {
      process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({ commitPushed: "Pushed {short_sha} to repo" });
      const msg = getCommitPushedMessage({ commitSha: SHA, shortSha: SHORT, commitUrl: COMMIT_URL });
      expect(msg).toBe(`Pushed ${SHORT} to repo`);
    });

    it("supports full SHA in custom template", () => {
      process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({ commitPushed: "{commit_sha}" });
      const msg = getCommitPushedMessage({ commitSha: SHA, shortSha: SHORT, commitUrl: COMMIT_URL });
      expect(msg).toBe(SHA);
    });
  });

  describe("fallback when config is missing keys", () => {
    it("uses default template when only unrelated config keys are set", () => {
      process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({ footer: "custom footer" });
      expect(getRunStartedMessage({ workflowName: WORKFLOW, runUrl: RUN_URL, eventType: "issue" })).toContain("has started processing");
      expect(getRunSuccessMessage({ workflowName: WORKFLOW, runUrl: RUN_URL })).toContain("completed successfully");
      expect(getRunFailureMessage({ workflowName: WORKFLOW, runUrl: RUN_URL, status: "failed" })).toContain("failed");
    });
  });
});
