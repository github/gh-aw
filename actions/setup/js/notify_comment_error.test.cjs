import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import fs from "fs";
import path from "path";
import { syncRuntimePromptTemplates } from "./test_prompt_templates.js";

const { runtimePromptsDir } = syncRuntimePromptTemplates(import.meta.url);
const { ERR_VALIDATION } = require("./error_codes.cjs");
const mockCore = {
    debug: vi.fn(),
    info: vi.fn(),
    notice: vi.fn(),
    warning: vi.fn(),
    error: vi.fn(),
    setFailed: vi.fn(),
    setOutput: vi.fn(),
    exportVariable: vi.fn(),
    setSecret: vi.fn(),
    setCancelled: vi.fn(),
    setError: vi.fn(),
    getInput: vi.fn(),
    getBooleanInput: vi.fn(),
    getMultilineInput: vi.fn(),
    getState: vi.fn(),
    saveState: vi.fn(),
    startGroup: vi.fn(),
    endGroup: vi.fn(),
    group: vi.fn(),
    addPath: vi.fn(),
    setCommandEcho: vi.fn(),
    isDebug: vi.fn().mockReturnValue(!1),
    getIDToken: vi.fn(),
    toPlatformPath: vi.fn(),
    toPosixPath: vi.fn(),
    toWin32Path: vi.fn(),
    summary: { addRaw: vi.fn().mockReturnThis(), write: vi.fn().mockResolvedValue() },
  },
  mockGithub = {
    request: vi.fn().mockResolvedValue({ data: { id: 123456, html_url: "https://github.com/owner/repo/issues/1#issuecomment-123456" } }),
    graphql: vi.fn().mockResolvedValue({ updateDiscussionComment: { comment: { id: "DC_kwDOABCDEF4ABCDEF", url: "https://github.com/owner/repo/discussions/1#discussioncomment-123456" } } }),
  },
  mockContext = { repo: { owner: "testowner", repo: "testrepo" } };
((global.core = mockCore),
  (global.github = mockGithub),
  (global.context = mockContext),
  describe("notify_comment_error.cjs", () => {
    let notifyCommentScript, originalEnv;
    (beforeEach(() => {
      (vi.clearAllMocks(),
        (mockContext.eventName = void 0),
        (mockContext.payload = void 0),
        (originalEnv = {
          GITHUB_RUN_ID: process.env.GITHUB_RUN_ID,
          GH_AW_COMMENT_ID: process.env.GH_AW_COMMENT_ID,
          GH_AW_COMMENT_REPO: process.env.GH_AW_COMMENT_REPO,
          GH_AW_PROMPTS_DIR: process.env.GH_AW_PROMPTS_DIR,
          GH_AW_RUN_URL: process.env.GH_AW_RUN_URL,
          GH_AW_WORKFLOW_NAME: process.env.GH_AW_WORKFLOW_NAME,
          GH_AW_WORKFLOW_ID: process.env.GH_AW_WORKFLOW_ID,
          GH_AW_AIC: process.env.GH_AW_AIC,
          GH_AW_THREAT_DETECTION_AIC: process.env.GH_AW_THREAT_DETECTION_AIC,
          GH_AW_EVALS_AIC: process.env.GH_AW_EVALS_AIC,
          GH_AW_AMBIENT_CONTEXT: process.env.GH_AW_AMBIENT_CONTEXT,
          GH_AW_AGENT_CONCLUSION: process.env.GH_AW_AGENT_CONCLUSION,
          GH_AW_DETECTION_CONCLUSION: process.env.GH_AW_DETECTION_CONCLUSION,
          GH_AW_DETECTION_ACCEPTED: process.env.GH_AW_DETECTION_ACCEPTED,
          GH_AW_AUTHORIZATION_PROTECTED_REPORTS: process.env.GH_AW_AUTHORIZATION_PROTECTED_REPORTS,
          GH_AW_SAFE_OUTPUTS_STAGED: process.env.GH_AW_SAFE_OUTPUTS_STAGED,
          GH_AW_COMPLETION_STAGED_NOOP: process.env.GH_AW_COMPLETION_STAGED_NOOP,
          GH_AW_COMPLETION_STAGED_REPORT_INCOMPLETE: process.env.GH_AW_COMPLETION_STAGED_REPORT_INCOMPLETE,
          GH_AW_COMPLETION_STAGED_REPORT_MISSING_DATA: process.env.GH_AW_COMPLETION_STAGED_REPORT_MISSING_DATA,
          GH_AW_ASSIGNMENT_ERROR_COUNT: process.env.GH_AW_ASSIGNMENT_ERROR_COUNT,
          GH_AW_SAFE_OUTPUT_MESSAGES: process.env.GH_AW_SAFE_OUTPUT_MESSAGES,
          GH_AW_SAFE_OUTPUT_JOBS: process.env.GH_AW_SAFE_OUTPUT_JOBS,
          GH_AW_SAFE_OUTPUTS_RESULT: process.env.GH_AW_SAFE_OUTPUTS_RESULT,
          GH_AW_AGENT_OUTPUT: process.env.GH_AW_AGENT_OUTPUT,
          GH_AW_NOOP_COMMENT_BODY: process.env.GH_AW_NOOP_COMMENT_BODY,
          GH_AW_ACTIVATION_COMMENT_LINKS: process.env.GH_AW_ACTIVATION_COMMENT_LINKS,
          GH_AW_NOOP_REPORT_AS_ISSUE: process.env.GH_AW_NOOP_REPORT_AS_ISSUE,
          GH_AW_OUTPUT_CREATE_ISSUE_ISSUE_URL: process.env.GH_AW_OUTPUT_CREATE_ISSUE_ISSUE_URL,
          GH_AW_OUTPUT_ADD_COMMENT_COMMENT_URL: process.env.GH_AW_OUTPUT_ADD_COMMENT_COMMENT_URL,
          GH_AW_OUTPUT_CREATE_PULL_REQUEST_PULL_REQUEST_URL: process.env.GH_AW_OUTPUT_CREATE_PULL_REQUEST_PULL_REQUEST_URL,
        }),
        (process.env.GH_AW_PROMPTS_DIR = runtimePromptsDir));
      const scriptPath = path.join(process.cwd(), "notify_comment_error.cjs");
      notifyCommentScript = fs.readFileSync(scriptPath, "utf8");
    }),
      afterEach(() => {
        Object.keys(originalEnv).forEach(key => {
          void 0 !== originalEnv[key] ? (process.env[key] = originalEnv[key]) : delete process.env[key];
        });
      }),
      describe("when comment ID is not provided", () => {
        it("should write a regular linked conclusion summary for noop messages", async () => {
          const tempDir = fs.mkdtempSync(path.join(process.cwd(), "gh-aw-conclusion-summary-"));
          const outputPath = path.join(tempDir, "agent-output.json");
          fs.writeFileSync(outputPath, JSON.stringify({ items: [{ type: "noop", message: "No changes were needed." }] }));
          try {
            delete process.env.GH_AW_COMMENT_ID;
            process.env.GH_AW_AGENT_OUTPUT = outputPath;
            process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123";
            process.env.GH_AW_WORKFLOW_NAME = "test-workflow";
            process.env.GH_AW_AGENT_CONCLUSION = "success";

            await eval(`(async () => { ${notifyCommentScript}; await main(); })()`);

            const summary = mockCore.summary.addRaw.mock.calls[0][0];
            expect(summary).toContain("<summary>✅ Conclusion Summary (1 no-op message)</summary>");
            expect(summary).toContain("**Target:** [Workflow run](https://github.com/owner/repo/actions/runs/123)");
          } finally {
            fs.rmSync(tempDir, { recursive: true, force: true });
          }
        });

        it("should skip comment update", async () => {
          (delete process.env.GH_AW_COMMENT_ID,
            (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
            (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
            (process.env.GH_AW_AGENT_CONCLUSION = "failure"),
            await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
            expect(mockCore.info).toHaveBeenCalledWith("No comment ID found and no noop messages to process, skipping comment update"),
            expect(mockGithub.request).not.toHaveBeenCalled(),
            expect(mockGithub.graphql).not.toHaveBeenCalled());
        });
      }),
      describe("categorized no-op completion comments", () => {
        it.each([
          { conclusion: "success", incomplete: false, category: "reported_no_action" },
          { conclusion: "failure", incomplete: false, category: "incomplete" },
          { conclusion: "cancelled", incomplete: false, category: "unconfirmed" },
          { conclusion: "success", incomplete: true, category: "incomplete" },
          { conclusion: "success", incomplete: false, category: "reported_no_action", safeOutputsResult: "failure" },
          { conclusion: "success", incomplete: false, category: "reported_action", safeOutputsResult: "failure", hasAction: true },
        ])("publishes the no-op step body with one footer and without agent prose: %j", async ({ conclusion, incomplete, category, safeOutputsResult, hasAction }) => {
          const outputPath = path.join(process.cwd(), "noop-completion-output.json");
          const privateMessage = "private repository contents, credentials, prompt and tool payload";
          const items = [{ type: "noop", message: privateMessage }];
          if (incomplete) items.push({ type: "report_incomplete", reason: "private incomplete reason" });
          if (hasAction) items.push({ type: "add_comment", body: "private action body" });
          fs.writeFileSync(outputPath, JSON.stringify({ items }));
          try {
            process.env.GH_AW_AGENT_OUTPUT = outputPath;
            process.env.GH_AW_COMMENT_ID = "123456";
            process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123";
            process.env.GH_AW_WORKFLOW_NAME = "test-workflow";
            process.env.GH_AW_AGENT_CONCLUSION = conclusion;
            process.env.GH_AW_SAFE_OUTPUTS_RESULT = safeOutputsResult || "success";
            process.env.GH_AW_WORKFLOW_ID = "test-workflow";
            process.env.GH_AW_AIC = "5";
            process.env.GH_AW_THREAT_DETECTION_AIC = "1";
            process.env.GH_AW_EVALS_AIC = "1";
            process.env.GH_AW_AMBIENT_CONTEXT = "1700";
            process.env.GH_AW_NOOP_REPORT_AS_ISSUE = "false";
            mockContext.payload = { pull_request: { number: 77463 } };
            delete process.env.GH_AW_SAFE_OUTPUT_MESSAGES;
            delete process.env.GH_AW_DETECTION_CONCLUSION;
            const { main } = require("./handle_noop_message.cjs");
            await main();
            process.env.GH_AW_NOOP_COMMENT_BODY = mockCore.setOutput.mock.calls.find(call => call[0] === "noop_comment_body")[1];

            await eval(`(async () => { ${notifyCommentScript}; await main(); })()`);

            const body = mockGithub.request.mock.calls.find(call => call[0] === "PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}")[1].body;
            expect(body).toContain(process.env.GH_AW_NOOP_COMMENT_BODY);
            expect(body).toContain(`**Outcome:** \`${category}\``);
            expect(body).not.toContain(privateMessage);
            expect(body).not.toContain("private incomplete reason");
            expect(body.match(/> Generated (?:by|from) /g)).toHaveLength(1);
            expect(body).toContain("> Generated by [test-workflow]");
            expect(body).toContain("for #77463");
            expect(body).toContain("5 AIC");
            expect(body).toContain("⌖ \\~1 AIC");
            expect(body).toContain("◇ \\~1 AIC");
            expect(body).toContain("⊞ 1.7K");
            expect(body).toContain("[history](https://github.com/search?");
            expect(body).toContain("<!-- gh-aw-agentic-workflow: test-workflow");
            if (safeOutputsResult === "failure") expect(body).toContain("failed to deliver outputs");
          } finally {
            fs.rmSync(outputPath, { force: true });
          }
        });

        it("does not fall back to publishing raw noop messages when no categorized body is available", async () => {
          const outputPath = path.join(process.cwd(), "noop-completion-output.json");
          fs.writeFileSync(outputPath, JSON.stringify({ items: [{ type: "noop", message: "private agent prose" }] }));
          try {
            process.env.GH_AW_AGENT_OUTPUT = outputPath;
            process.env.GH_AW_COMMENT_ID = "123456";
            process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123";
            process.env.GH_AW_AGENT_CONCLUSION = "success";
            delete process.env.GH_AW_NOOP_COMMENT_BODY;
            delete process.env.GH_AW_SAFE_OUTPUT_MESSAGES;
            await eval(`(async () => { ${notifyCommentScript}; await main(); })()`);
            expect(mockGithub.request.mock.calls[0][1].body).not.toContain("private agent prose");
            expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("omitting the no-op category"));
          } finally {
            fs.rmSync(outputPath, { force: true });
          }
        });
      }),
      describe("agent explanations in status comments", () => {
        it.each([
          { verdict: "success", accepted: "true", published: true },
          { verdict: "success", accepted: "false", published: false },
          { verdict: "warning", accepted: "false", published: false },
          { verdict: "failure", accepted: "false", published: false },
          { verdict: "skipped", accepted: "true", published: false },
          { verdict: "", accepted: "false", published: false },
          { detectionDisabled: true, published: false },
        ])("requires a clean, accepted verdict: %j", async ({ verdict, accepted, published, detectionDisabled }) => {
          const tempDir = fs.mkdtempSync(path.join(process.cwd(), "gh-aw-status-reports-"));
          const outputPath = path.join(tempDir, "agent-output.json");
          const items = [
            { type: "noop", message: "Nothing to change." },
            { type: "report_incomplete", reason: "Review did not finish.", details: "Unrequested details" },
            { type: "report_missing_tool", reason: "The build tool was unavailable." },
            { type: "report_missing_data", reason: "No input was provided." },
            { type: "request_continuation", message: "Review the remaining files." },
            { type: "add_comment", body: "Unrelated safe output" },
          ];
          fs.writeFileSync(outputPath, JSON.stringify({ items }));
          try {
            process.env.GH_AW_AGENT_OUTPUT = outputPath;
            process.env.GH_AW_COMMENT_ID = "123456";
            process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123";
            process.env.GH_AW_AGENT_CONCLUSION = detectionDisabled ? "success" : "failure";
            delete process.env.GH_AW_SAFE_OUTPUTS_RESULT;
            delete process.env.GH_AW_NOOP_COMMENT_BODY;
            delete process.env.GH_AW_SAFE_OUTPUT_MESSAGES;
            if (verdict === undefined) delete process.env.GH_AW_DETECTION_CONCLUSION;
            else process.env.GH_AW_DETECTION_CONCLUSION = verdict;
            if (accepted === undefined) delete process.env.GH_AW_DETECTION_ACCEPTED;
            else process.env.GH_AW_DETECTION_ACCEPTED = accepted;

            await eval(`(async () => { ${notifyCommentScript}; await main(); })()`);

            const body = mockGithub.request.mock.calls.find(call => call[0] === "PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}")[1].body;
            for (const item of items.slice(0, -1)) {
              expect(body.includes(item.message || item.reason)).toBe(published);
            }
            expect(body.includes("## Agent messages")).toBe(published);
            expect(body.includes("**Continuation requested**")).toBe(published);
            expect(body).not.toContain("<summary>No-op message</summary>");
            if (detectionDisabled) expect(body).toContain("completed successfully");
            expect(body).not.toContain("Unrequested details");
            expect(body).not.toContain("Unrelated safe output");
          } finally {
            fs.rmSync(tempDir, { recursive: true, force: true });
          }
        });
      }),
      describe("rendering accepted agent explanations", () => {
        it("keeps agent content tainted under framework-authored section labels", () => {
          const children = eval(`(() => {
            ${notifyCommentScript}
            const children = [];
            appendAgentReports(children, [{ type: "noop", message: "No changes needed." }]);
            return children;
          })()`);

          expect(children).toMatchObject([
            { type: "heading", level: 2, agentTainted: false, children: [{ type: "text", value: "Agent messages", agentTainted: false }] },
            { type: "paragraph", agentTainted: false, children: [{ type: "strong", agentTainted: false }] },
            { type: "paragraph", agentTainted: true, children: [{ type: "text", value: "No changes needed.", agentTainted: true }] },
          ]);
        });

        async function renderAcceptedExplanations(items, noopCommentBody) {
          const fixtureDir = fs.mkdtempSync(path.join(process.cwd(), "gh-aw-explanation-rendering-"));
          try {
            const outputPath = path.join(fixtureDir, "agent-output.json");
            fs.writeFileSync(outputPath, JSON.stringify({ items }));
            process.env.GH_AW_AGENT_OUTPUT = outputPath;
            process.env.GH_AW_COMMENT_ID = "123456";
            process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123";
            process.env.GH_AW_AGENT_CONCLUSION = "success";
            process.env.GH_AW_DETECTION_CONCLUSION = "success";
            process.env.GH_AW_DETECTION_ACCEPTED = "true";
            delete process.env.GH_AW_SAFE_OUTPUTS_RESULT;
            delete process.env.GH_AW_NOOP_COMMENT_BODY;
            delete process.env.GH_AW_SAFE_OUTPUT_MESSAGES;
            if (noopCommentBody) process.env.GH_AW_NOOP_COMMENT_BODY = noopCommentBody;

            await eval(`(async () => { ${notifyCommentScript}; await main(); })()`);

            const call = mockGithub.request.mock.calls.find(call => call[0] === "PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}");
            expect(call).toBeDefined();
            return call[1].body;
          } finally {
            fs.rmSync(fixtureDir, { recursive: true, force: true });
          }
        }

        it("sanitizes accepted no-op Markdown and preserves allowed links", async () => {
          vi.stubEnv("GH_AW_ALLOWED_DOMAINS", "api.example.com");
          try {
            const body = await renderAcceptedExplanations([
              {
                type: "noop",
                message: "# No changes\n\nNotify @some-org/some-team <b>if needed</b>. [Health](https://api.example.com/health) [Blocked](https://blocked.example.net/health)",
              },
            ]);
            expect(body).toContain("## Agent messages\n\n**No-op message**\n\n### No changes\n");
            expect(body).toContain("Notify `@some-org/some-team` **if needed**.");
            expect(body.replace("`@some-org/some-team`", "")).not.toContain("@some-org/some-team");
            expect(body).not.toMatch(/<\/?b>/);
            expect(body).toContain("[Health](https://api.example.com/health)");
            expect(body).not.toContain("https://blocked.example.net/health");
          } finally {
            vi.unstubAllEnvs();
          }
        });

        it("shows multiline agent messages before links, categorized outcomes, and attribution", async () => {
          process.env.GH_AW_ACTIVATION_COMMENT_LINKS = "Pull request created: [#900](https://github.com/owner/repo/pull/900)";
          const body = await renderAcceptedExplanations(
            [
              { type: "noop", message: "No findings.\n\nReviewed all changed contracts." },
              { type: "report_missing_data", reason: "No input was provided." },
            ],
            "### test-workflow\n\n**Outcome:** `reported_action`\n\nStructured outcome explanation."
          );

          expect(body).toContain("## Agent messages\n\n**No-op message**\n\nNo findings.\n\nReviewed all changed contracts.\n\n**Missing data**\n\nNo input was provided.");
          expect(body).not.toContain("<details>");
          expect(body.indexOf("completed successfully")).toBeLessThan(body.indexOf("## Agent messages"));
          expect(body.indexOf("No input was provided.")).toBeLessThan(body.indexOf("Pull request created:"));
          expect(body.indexOf("Pull request created:")).toBeLessThan(body.indexOf("**Outcome:**"));
          expect(body.indexOf("**Outcome:**")).toBeLessThan(body.indexOf("> Generated by"));
          expect(body.match(/<!-- gh-aw-agentic-workflow:/g)).toHaveLength(1);
        });

        it("bounds many accepted explanations while preserving the status and footer", async () => {
          const items = Array.from({ length: 3000 }, (_, index) => ({ type: "noop", message: `Message ${index}` }));
          const body = await renderAcceptedExplanations(items);

          expect(body.length).toBeLessThanOrEqual(65536);
          expect(body.match(/\*\*No-op message\*\*/g)).toHaveLength(50);
          expect(body).toContain("Message 49");
          expect(body).not.toContain("Message 50\n");
          expect(body).toContain("Additional agent messages omitted.");
          expect(body).toContain("completed successfully");
          expect(body).toContain("> Generated by");
          expect(body).toContain("<!-- gh-aw-agentic-workflow:");
        });

        it("fits large explanations without omitting an exact-limit collection", async () => {
          const items = Array.from({ length: 50 }, () => ({ type: "report_missing_data", reason: "Large explanation ".repeat(1000) }));
          const body = await renderAcceptedExplanations(items);

          expect(body.length).toBeLessThanOrEqual(65536);
          expect(body).toContain("Content redacted to fit size limit.");
          expect(body).not.toContain("Additional agent messages omitted.");
          expect(body).toContain("completed successfully");
          expect(body).toContain("<!-- gh-aw-agentic-workflow:");
        });

        it("does not count invalid or unsupported explanations toward the limit", async () => {
          const items = [
            ...Array.from({ length: 50 }, (_, index) => ({ type: "noop", message: `Message ${index}` })),
            { type: "noop", message: " " },
            { type: "noop", message: "API_SECRET=synthetic-opaque-value" },
            { type: "add_comment", body: "Unrelated output" },
          ];
          const body = await renderAcceptedExplanations(items);

          expect(body).toContain("Message 49");
          expect(body).not.toContain("Additional agent messages omitted.");
          expect(body).not.toContain("synthetic-opaque-value");
        });

        it("preserves agent-authored alerts without nesting them in another alert", async () => {
          const body = await renderAcceptedExplanations([{ type: "report_incomplete", reason: "> [!WARNING]\n> Review did not finish." }]);

          expect(body).toContain("## Agent messages\n\n**Incomplete work**\n\n> [!WARNING]\n> Review did not finish.");
          expect(body).not.toContain("> > [!");
        });

        it("omits the info section when there are no publishable explanations", async () => {
          const body = await renderAcceptedExplanations([
            { type: "noop", message: " " },
            { type: "add_comment", body: "Unrelated output" },
          ]);

          expect(body).not.toContain("## Agent messages");
          expect(body).not.toContain("**No-op message**");
          expect(body).toContain("completed successfully");
        });

        it("masks recognized credentials in the published explanation", async () => {
          const pat = `ghp_${"A".repeat(36)}`;
          const awsKey = `AKIA${"B".repeat(16)}`;
          const body = await renderAcceptedExplanations([{ type: "noop", message: `Credential diagnostics:\n\n\`\`\`text\n${pat}\n${awsKey}\n\`\`\`` }]);

          expect(body).toContain("**No-op message**");
          expect(body).toContain("Credential diagnostics:");
          expect(body).toContain("***REDACTED***\n***REDACTED***");
          expect(body.includes(pat)).toBe(false);
          expect(body.includes(awsKey)).toBe(false);
        });

        it.each([
          ["bearer credential", "Authorization: Bearer synthetic-opaque-bearer"],
          ["PEM private key", "-----BEGIN PRIVATE KEY-----\nU3ludGhldGljIGZpeHR1cmU=\n-----END PRIVATE KEY-----"],
          ["sensitive assignment", "API_SECRET=synthetic-opaque-value"],
        ])("withholds an explanation containing a %s and preserves the status", async (_case, diagnostic) => {
          const body = await renderAcceptedExplanations([
            { type: "noop", message: `Credential diagnostics:\n\n\`\`\`text\n${diagnostic}\n\`\`\`` },
            { type: "report_incomplete", reason: "Review did not finish." },
          ]);

          expect(body).toContain("completed successfully");
          expect(body).toContain("Review did not finish.");
          expect(body.includes("No-op message")).toBe(false);
          expect(body.includes("Credential diagnostics:")).toBe(false);
        });

        it.each([
          ["parser depth limit", "> ".repeat(20) + "Omitted explanation"],
          ["empty details summary", "<details>\n<summary></summary>\n\nOmitted explanation\n\n</details>"],
        ])("preserves the completion comment when an explanation fails the %s check", async (_case, message) => {
          const body = await renderAcceptedExplanations([
            { type: "noop", message },
            { type: "report_incomplete", reason: "Review did not finish." },
          ]);
          expect(body).toContain("completed successfully");
          expect(body).toContain("Review did not finish");
          expect(body).toContain("<!-- gh-aw-agentic-workflow:");
          expect(body).not.toContain("No-op message");
          expect(body).not.toContain("Omitted explanation");
          expect(mockCore.warning).toHaveBeenCalledWith("Omitting an invalid agent explanation from the completion comment");
          expect(JSON.stringify(mockCore.warning.mock.calls)).not.toContain("Omitted explanation");
        });
      }),
      describe("completion comment publication restrictions", () => {
        it.each([
          { staged: true, protectedReports: "", toolStaged: false, publishExempt: false },
          { staged: false, protectedReports: "noop,report_incomplete", toolStaged: false, publishExempt: true },
          { staged: false, protectedReports: "", toolStaged: true, publishExempt: true },
        ])("preserves the status without restricted explanations: %j", async ({ staged, protectedReports, toolStaged, publishExempt }) => {
          const tempDir = fs.mkdtempSync(path.join(process.cwd(), "gh-aw-status-policy-"));
          const outputPath = path.join(tempDir, "agent-output.json");
          fs.writeFileSync(
            outputPath,
            JSON.stringify({
              items: [
                { type: "noop", message: "Private no-op explanation." },
                { type: "report_incomplete", reason: "Private incomplete explanation.", authorized: true },
                { type: "report_missing_data", reason: "Exempt explanation." },
              ],
            })
          );
          try {
            process.env.GH_AW_AGENT_OUTPUT = outputPath;
            process.env.GH_AW_COMMENT_ID = "123456";
            process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123";
            process.env.GH_AW_AGENT_CONCLUSION = "success";
            process.env.GH_AW_DETECTION_CONCLUSION = "success";
            process.env.GH_AW_DETECTION_ACCEPTED = "true";
            process.env.GH_AW_SAFE_OUTPUTS_STAGED = String(staged);
            process.env.GH_AW_AUTHORIZATION_PROTECTED_REPORTS = protectedReports;
            if (toolStaged) {
              process.env.GH_AW_COMPLETION_STAGED_NOOP = "true";
              process.env.GH_AW_COMPLETION_STAGED_REPORT_INCOMPLETE = "";
              process.env.GH_AW_COMPLETION_STAGED_REPORT_MISSING_DATA = "false";
            }
            process.env.GH_AW_NOOP_COMMENT_BODY = "Private categorized no-op body.";
            delete process.env.GH_AW_SAFE_OUTPUTS_RESULT;
            delete process.env.GH_AW_SAFE_OUTPUT_MESSAGES;

            await eval(`(async () => { ${notifyCommentScript}; await main(); })()`);

            const body = mockGithub.request.mock.calls.find(call => call[0] === "PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}")[1].body;
            expect(body).toContain("completed successfully");
            expect(body).not.toContain("Private");
            expect(body.includes("Exempt explanation.")).toBe(publishExempt);
          } finally {
            fs.rmSync(tempDir, { recursive: true, force: true });
          }
        });
      }),
      describe("when append-only comments are enabled", () => {
        it("should create a new issue comment even when GH_AW_COMMENT_ID is not set", async () => {
          (delete process.env.GH_AW_COMMENT_ID,
            (process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({ appendOnlyComments: !0 })),
            (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
            (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
            (process.env.GH_AW_AGENT_CONCLUSION = "success"),
            (mockContext.eventName = "issues"),
            (mockContext.payload = { issue: { number: 1 } }),
            await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
            expect(mockGithub.request).toHaveBeenCalledWith("POST /repos/{owner}/{repo}/issues/{issue_number}/comments", expect.objectContaining({ owner: "testowner", repo: "testrepo", issue_number: 1, body: expect.any(String) })),
            expect(mockCore.info).toHaveBeenCalledWith("Successfully created append-only comment"));
        });

        it("should create a new comment instead of updating an existing comment", async () => {
          ((process.env.GH_AW_COMMENT_ID = "123456"),
            (process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({ appendOnlyComments: !0 })),
            (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
            (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
            (process.env.GH_AW_AGENT_CONCLUSION = "success"),
            (mockContext.eventName = "issues"),
            (mockContext.payload = { issue: { number: 1 } }),
            await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
            expect(mockGithub.request).toHaveBeenCalledWith("POST /repos/{owner}/{repo}/issues/{issue_number}/comments", expect.objectContaining({ owner: "testowner", repo: "testrepo", issue_number: 1, body: expect.any(String) })));

          const endpoints = mockGithub.request.mock.calls.map(call => call[0]);
          expect(endpoints).not.toContain("PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}");
        });
      }),
      describe("when run URL is not provided", () => {
        it("should fail with error", async () => {
          ((process.env.GH_AW_COMMENT_ID = "123456"),
            delete process.env.GH_AW_RUN_URL,
            (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
            (process.env.GH_AW_AGENT_CONCLUSION = "failure"),
            await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
            expect(mockCore.setFailed).toHaveBeenCalledWith(`${ERR_VALIDATION}: Run URL is required`),
            expect(mockGithub.request).not.toHaveBeenCalled(),
            expect(mockGithub.graphql).not.toHaveBeenCalled());
        });
      }),
      describe("when updating an issue/PR comment", () => {
        it.each([undefined, "40497447793"])("closes imported activation-link fences before the attribution footer with run ID %s", async runId => {
          if (runId === undefined) delete process.env.GITHUB_RUN_ID;
          else process.env.GITHUB_RUN_ID = runId;
          process.env.GH_AW_COMMENT_ID = "123456";
          process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123";
          process.env.GH_AW_WORKFLOW_NAME = "test-workflow";
          process.env.GH_AW_AGENT_CONCLUSION = "success";
          process.env.GH_AW_ACTIVATION_COMMENT_LINKS = "```markdown\nkept link text";
          await eval(`(async () => { ${notifyCommentScript}; await main(); })()`);

          const patchCall = mockGithub.request.mock.calls.find(call => call[0] === "PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}");
          expect(patchCall[1].body).toMatch(/```markdown\nkept link text\n```\n\n> Generated by/);
          const runIdField = runId === undefined ? "" : `, id: ${runId}`;
          expect(patchCall[1].body).toContain(`<!-- gh-aw-agentic-workflow: test-workflow${runIdField}, run: https://github.com/owner/repo/actions/runs/123 -->`);
        });

        (it("should preserve pull request links appended to the activation comment by safe outputs", async () => {
          ((process.env.GH_AW_COMMENT_ID = "123456"),
            (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
            (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
            (process.env.GH_AW_AGENT_CONCLUSION = "success"),
            (process.env.GH_AW_ACTIVATION_COMMENT_LINKS = "Pull request created: [#900](https://github.com/owner/repo/pull/900)"),
            await eval(`(async () => { ${notifyCommentScript}; await main(); })()`));
          const patchCall = mockGithub.request.mock.calls.find(call => call[0] === "PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}");
          expect(patchCall).toBeDefined();
          expect(patchCall[1].body).toContain("completed successfully\\!");
          expect(patchCall[1].body).toContain("Pull request created: [#900](https://github.com/owner/repo/pull/900)");
        }),
          it("should update with success message when agent succeeds", async () => {
            ((process.env.GH_AW_COMMENT_ID = "123456"),
              (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
              (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
              (process.env.GH_AW_AGENT_CONCLUSION = "success"),
              await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
              expect(mockGithub.request).toHaveBeenCalledWith(
                "PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}",
                expect.objectContaining({ owner: "testowner", repo: "testrepo", comment_id: 123456, body: expect.stringContaining("completed successfully\\!") })
              ),
              expect(mockCore.info).toHaveBeenCalledWith("Successfully updated comment"));
          }),
          it("should update with assignment failure message when agent succeeds but assign-to-agent fails", async () => {
            ((process.env.GH_AW_COMMENT_ID = "123456"),
              (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
              (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
              (process.env.GH_AW_AGENT_CONCLUSION = "success"),
              (process.env.GH_AW_ASSIGNMENT_ERROR_COUNT = "3"),
              await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
              expect(mockGithub.request).toHaveBeenCalledWith(
                "PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}",
                expect.objectContaining({
                  owner: "testowner",
                  repo: "testrepo",
                  comment_id: 123456,
                  body: expect.stringContaining("failed to assign the coding agent"),
                })
              ),
              expect(mockCore.info).toHaveBeenCalledWith("Successfully updated comment"),
              expect(mockGithub.request).not.toHaveBeenCalledWith("PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}", expect.objectContaining({ body: expect.stringContaining("completed successfully\\!") })));
          }),
          it("should update with failure message when agent fails", async () => {
            ((process.env.GH_AW_COMMENT_ID = "123456"),
              (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
              (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
              (process.env.GH_AW_AGENT_CONCLUSION = "failure"),
              await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
              expect(mockGithub.request).toHaveBeenCalledWith(
                "PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}",
                expect.objectContaining({ owner: "testowner", repo: "testrepo", comment_id: 123456, body: expect.stringContaining("failed. Please review the logs") })
              ),
              expect(mockCore.info).toHaveBeenCalledWith("Successfully updated comment"));
          }),
          it("should update with cancelled message when agent is cancelled", async () => {
            ((process.env.GH_AW_COMMENT_ID = "123456"),
              (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
              (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
              (process.env.GH_AW_AGENT_CONCLUSION = "cancelled"),
              await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
              expect(mockGithub.request).toHaveBeenCalledWith("PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}", expect.objectContaining({ body: expect.stringContaining("was cancelled. Please review the logs") })));
          }),
          it("should update with timeout message when agent times out", async () => {
            ((process.env.GH_AW_COMMENT_ID = "123456"),
              (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
              (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
              (process.env.GH_AW_AGENT_CONCLUSION = "timed_out"),
              await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
              expect(mockGithub.request).toHaveBeenCalledWith("PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}", expect.objectContaining({ body: expect.stringContaining("timed out. Please review the logs") })));
          }),
          it("should update with skipped message when agent is skipped", async () => {
            ((process.env.GH_AW_COMMENT_ID = "123456"),
              (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
              (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
              (process.env.GH_AW_AGENT_CONCLUSION = "skipped"),
              await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
              expect(mockGithub.request).toHaveBeenCalledWith("PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}", expect.objectContaining({ body: expect.stringContaining("was skipped. Please review the logs") })));
          }),
          it("should use custom comment repo when provided", async () => {
            ((process.env.GH_AW_COMMENT_ID = "123456"),
              (process.env.GH_AW_COMMENT_REPO = "customowner/customrepo"),
              (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
              (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
              (process.env.GH_AW_AGENT_CONCLUSION = "success"),
              await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
              expect(mockGithub.request).toHaveBeenCalledWith("PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}", expect.objectContaining({ owner: "customowner", repo: "customrepo" })));
          }),
          it("should prioritize detection failure message when detection job fails", async () => {
            ((process.env.GH_AW_COMMENT_ID = "123456"),
              (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
              (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
              (process.env.GH_AW_AGENT_CONCLUSION = "success"),
              (process.env.GH_AW_DETECTION_CONCLUSION = "failure"),
              await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
              expect(mockGithub.request).toHaveBeenCalledWith(
                "PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}",
                expect.objectContaining({ owner: "testowner", repo: "testrepo", comment_id: 123456, body: expect.stringContaining("Security scanning failed") })
              ),
              expect(mockCore.info).toHaveBeenCalledWith("Successfully updated comment"));
          }),
          it("should report detection failure even when agent fails", async () => {
            ((process.env.GH_AW_COMMENT_ID = "123456"),
              (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
              (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
              (process.env.GH_AW_AGENT_CONCLUSION = "failure"),
              (process.env.GH_AW_DETECTION_CONCLUSION = "failure"),
              await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
              expect(mockGithub.request).toHaveBeenCalledWith("PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}", expect.objectContaining({ body: expect.stringContaining("Security scanning failed") })));
          }),
          it("should show agent success when detection succeeds", async () => {
            ((process.env.GH_AW_COMMENT_ID = "123456"),
              (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
              (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
              (process.env.GH_AW_AGENT_CONCLUSION = "success"),
              (process.env.GH_AW_DETECTION_CONCLUSION = "success"),
              await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
              expect(mockGithub.request).toHaveBeenCalledWith("PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}", expect.objectContaining({ body: expect.stringContaining("completed successfully\\!") })));
          }),
          it("should show failure message with detection warning when agent fails", async () => {
            ((process.env.GH_AW_COMMENT_ID = "123456"),
              (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
              (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
              (process.env.GH_AW_AGENT_CONCLUSION = "failure"),
              (process.env.GH_AW_DETECTION_CONCLUSION = "warning"),
              await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
              expect(mockGithub.request).toHaveBeenCalledWith("PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}", expect.objectContaining({ body: expect.stringContaining("failed. Please review the logs") })));
          }),
          it("should show cancelled message with detection warning when agent is cancelled", async () => {
            ((process.env.GH_AW_COMMENT_ID = "123456"),
              (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
              (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
              (process.env.GH_AW_AGENT_CONCLUSION = "cancelled"),
              (process.env.GH_AW_DETECTION_CONCLUSION = "warning"),
              await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
              expect(mockGithub.request).toHaveBeenCalledWith("PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}", expect.objectContaining({ body: expect.stringContaining("was cancelled. Please review the logs") })));
          }),
          it("should show timed out message with detection warning when agent times out", async () => {
            ((process.env.GH_AW_COMMENT_ID = "123456"),
              (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
              (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
              (process.env.GH_AW_AGENT_CONCLUSION = "timed_out"),
              (process.env.GH_AW_DETECTION_CONCLUSION = "warning"),
              await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
              expect(mockGithub.request).toHaveBeenCalledWith("PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}", expect.objectContaining({ body: expect.stringContaining("timed out. Please review the logs") })));
          }),
          it("should show assignment failure message with detection warning when agent succeeds but assign-to-agent fails", async () => {
            ((process.env.GH_AW_COMMENT_ID = "123456"),
              (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
              (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
              (process.env.GH_AW_AGENT_CONCLUSION = "success"),
              (process.env.GH_AW_ASSIGNMENT_ERROR_COUNT = "2"),
              (process.env.GH_AW_DETECTION_CONCLUSION = "warning"),
              await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
              expect(mockGithub.request).toHaveBeenCalledWith("PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}", expect.objectContaining({ body: expect.stringContaining("failed to assign the coding agent") })));
          }));
      }),
      describe("when updating a discussion comment", () => {
        (it("should use GraphQL API for discussion comments on success", async () => {
          ((process.env.GH_AW_COMMENT_ID = "DC_kwDOABCDEF4ABCDEF"),
            (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
            (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
            (process.env.GH_AW_AGENT_CONCLUSION = "success"),
            await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
            expect(mockGithub.graphql).toHaveBeenCalledWith(expect.stringContaining("updateDiscussionComment"), expect.objectContaining({ commentId: "DC_kwDOABCDEF4ABCDEF", body: expect.stringContaining("completed successfully\\!") })),
            expect(mockCore.info).toHaveBeenCalledWith("Successfully updated discussion comment"));
        }),
          it("should use GraphQL API for discussion comments on failure", async () => {
            ((process.env.GH_AW_COMMENT_ID = "DC_kwDOABCDEF4ABCDEF"),
              (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
              (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
              (process.env.GH_AW_AGENT_CONCLUSION = "failure"),
              await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
              expect(mockGithub.graphql).toHaveBeenCalledWith(
                expect.stringContaining("updateDiscussionComment"),
                expect.objectContaining({ commentId: "DC_kwDOABCDEF4ABCDEF", body: expect.stringContaining("failed. Please review the logs") })
              ));
          }));
      }),
      describe("error handling", () => {
        it("should warn but not fail when comment update fails", async () => {
          ((process.env.GH_AW_COMMENT_ID = "123456"),
            (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
            (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
            (process.env.GH_AW_AGENT_CONCLUSION = "success"),
            mockGithub.request.mockRejectedValueOnce(new Error("API error")),
            await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
            expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Failed to update comment")),
            expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("API error")),
            expect(mockCore.setFailed).not.toHaveBeenCalled());
        });
      }),
      describe("generated assets", () => {
        (it("should include generated asset links when safe output jobs produce URLs", async () => {
          ((process.env.GH_AW_COMMENT_ID = "123456"),
            (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
            (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
            (process.env.GH_AW_AGENT_CONCLUSION = "success"),
            (process.env.GH_AW_SAFE_OUTPUT_JOBS = JSON.stringify({ create_issue: "issue_url", add_comment: "comment_url", create_pull_request: "pull_request_url" })),
            (process.env.GH_AW_OUTPUT_CREATE_ISSUE_ISSUE_URL = "https://github.com/owner/repo/issues/42"),
            (process.env.GH_AW_OUTPUT_ADD_COMMENT_COMMENT_URL = "https://github.com/owner/repo/issues/1#issuecomment-123"),
            (process.env.GH_AW_OUTPUT_CREATE_PULL_REQUEST_PULL_REQUEST_URL = "https://github.com/owner/repo/pull/5"),
            await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
            expect(mockGithub.request).toHaveBeenCalledWith("PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}", expect.objectContaining({ owner: "testowner", repo: "testrepo", comment_id: 123456 })));
          const callArgs = mockGithub.request.mock.calls[0][1];
          (expect(callArgs.body).toContain("https://github.com/owner/repo/issues/42"),
            expect(callArgs.body).toContain("https://github.com/owner/repo/issues/1#issuecomment-123"),
            expect(callArgs.body).toContain("https://github.com/owner/repo/pull/5"),
            expect(callArgs.body).not.toContain("### Generated Assets"),
            expect(callArgs.body).not.toContain("Created Issue"),
            expect(callArgs.body).not.toContain("Added Comment"),
            expect(callArgs.body).not.toContain("Created Pull Request"));
        }),
          it("should not include generated assets section when no URLs are present", async () => {
            ((process.env.GH_AW_COMMENT_ID = "123456"),
              (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
              (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
              (process.env.GH_AW_AGENT_CONCLUSION = "success"),
              (process.env.GH_AW_SAFE_OUTPUT_JOBS = JSON.stringify({ create_issue: "issue_url" })),
              await eval(`(async () => { ${notifyCommentScript}; await main(); })()`));
            const callArgs = mockGithub.request.mock.calls[0][1];
            expect(callArgs.body).toContain("completed successfully\\!");
          }),
          it("should handle empty safe output jobs gracefully", async () => {
            ((process.env.GH_AW_COMMENT_ID = "123456"),
              (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
              (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
              (process.env.GH_AW_AGENT_CONCLUSION = "success"),
              await eval(`(async () => { ${notifyCommentScript}; await main(); })()`));
            const callArgs = mockGithub.request.mock.calls[0][1];
            expect(callArgs.body).toContain("completed successfully\\!");
          }));
      }),
      describe("when safe_outputs job fails", () => {
        (it("should show failure message when agent succeeds but safe_outputs fails", async () => {
          ((process.env.GH_AW_COMMENT_ID = "123456"),
            (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
            (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
            (process.env.GH_AW_AGENT_CONCLUSION = "success"),
            (process.env.GH_AW_SAFE_OUTPUTS_RESULT = "failure"),
            await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
            expect(mockGithub.request).toHaveBeenCalledWith("PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}", expect.objectContaining({ body: expect.stringContaining("failed to deliver outputs. Please review the logs") })),
            expect(mockGithub.request).not.toHaveBeenCalledWith("PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}", expect.objectContaining({ body: expect.stringContaining("completed successfully\\!") })));
        }),
          it("should show failure message when agent succeeds, safe_outputs fails, and detection warns", async () => {
            ((process.env.GH_AW_COMMENT_ID = "123456"),
              (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
              (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
              (process.env.GH_AW_AGENT_CONCLUSION = "success"),
              (process.env.GH_AW_SAFE_OUTPUTS_RESULT = "failure"),
              (process.env.GH_AW_DETECTION_CONCLUSION = "warning"),
              await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
              expect(mockGithub.request).toHaveBeenCalledWith("PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}", expect.objectContaining({ body: expect.stringContaining("failed to deliver outputs. Please review the logs") })));
          }),
          it("should show success message when agent succeeds and safe_outputs succeeds", async () => {
            ((process.env.GH_AW_COMMENT_ID = "123456"),
              (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
              (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
              (process.env.GH_AW_AGENT_CONCLUSION = "success"),
              (process.env.GH_AW_SAFE_OUTPUTS_RESULT = "success"),
              await eval(`(async () => { ${notifyCommentScript}; await main(); })()`),
              expect(mockGithub.request).toHaveBeenCalledWith("PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}", expect.objectContaining({ body: expect.stringContaining("completed successfully\\!") })));
          }));
      }),
      describe("footer in status comment", () => {
        (it("should include the generated footer in the updated comment body", async () => {
          ((process.env.GH_AW_COMMENT_ID = "123456"),
            (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
            (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
            (process.env.GH_AW_AGENT_CONCLUSION = "success"),
            await eval(`(async () => { ${notifyCommentScript}; await main(); })()`));
          const callArgs = mockGithub.request.mock.calls[0][1];
          expect(callArgs.body).toMatch(/Generated by \[test-workflow\]/);
          expect(callArgs.body).toMatch(/gh-aw-agentic-workflow/);
        }),
          it("should include the generated footer even when agent fails", async () => {
            ((process.env.GH_AW_COMMENT_ID = "123456"),
              (process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123"),
              (process.env.GH_AW_WORKFLOW_NAME = "test-workflow"),
              (process.env.GH_AW_AGENT_CONCLUSION = "failure"),
              await eval(`(async () => { ${notifyCommentScript}; await main(); })()`));
            const callArgs = mockGithub.request.mock.calls[0][1];
            expect(callArgs.body).toMatch(/Generated by \[test-workflow\]/);
            expect(callArgs.body).toMatch(/gh-aw-agentic-workflow/);
          }));
      }));
  }));
