import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import fs from "fs";
import path from "path";
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
  mockGithub = { rest: { repos: { getCollaboratorPermissionLevel: vi.fn(), listCollaborators: vi.fn() }, users: { getByUsername: vi.fn() } } },
  mockContext = { actor: "test-user", repo: { owner: "test-owner", repo: "test-repo" }, eventName: "issues", payload: {} };
((global.core = mockCore),
  (global.github = mockGithub),
  (global.context = mockContext),
  describe("compute_text.cjs", () => {
    let computeTextScript, sanitizeIncomingTextFunction;
    (beforeEach(() => {
      (vi.clearAllMocks(), (mockContext.eventName = "issues"), (mockContext.payload = {}), delete process.env.GH_AW_ALLOWED_DOMAINS);
      const scriptPath = path.join(process.cwd(), "compute_text.cjs");
      computeTextScript = fs.readFileSync(scriptPath, "utf8");
      const scriptWithExport = computeTextScript.replace("module.exports = { main };", "global.testSanitizeIncomingText = sanitizeIncomingText; global.testMain = main;");
      (eval(scriptWithExport), (sanitizeIncomingTextFunction = global.testSanitizeIncomingText));
    }),
      describe("sanitizeIncomingText function", () => {
        (it("should handle null and undefined inputs", () => {
          (expect(sanitizeIncomingTextFunction(null)).toBe(""), expect(sanitizeIncomingTextFunction(void 0)).toBe(""), expect(sanitizeIncomingTextFunction("")).toBe(""));
        }),
          it("should neutralize @mentions by wrapping in backticks", () => {
            const result = sanitizeIncomingTextFunction("Hello @user and @org/team");
            (expect(result).toContain("`@user`"), expect(result).toContain("`@org/team`"));
          }),
          it("should neutralize bot trigger phrases", () => {
            const result = sanitizeIncomingTextFunction("This fixes #123 and closes #456");
            (expect(result).toContain("`fixes #123`"), expect(result).toContain("`closes #456`"));
          }),
          it("should remove control characters", () => {
            const result = sanitizeIncomingTextFunction("Hello\0\bworld");
            expect(result).toBe("Helloworld");
          }),
          it("should convert XML tags to parentheses format", () => {
            const result = sanitizeIncomingTextFunction('Test <tag>content</tag> & "quotes"');
            (expect(result).toContain("(tag)content(/tag)"), expect(result).toContain("&"), expect(result).toContain('"quotes"'));
          }),
          it("should handle self-closing XML tags without whitespace", () => {
            const result = sanitizeIncomingTextFunction('Self-closing: <br/> <img src="test.jpg"/> <meta charset="utf-8"/>');
            (expect(result).toContain("<br/>"), expect(result).toContain('(img src="test.jpg"/)'), expect(result).toContain('(meta charset="utf-8"/)'));
          }),
          it("should handle self-closing XML tags with whitespace", () => {
            const result = sanitizeIncomingTextFunction('With spaces: <br /> <img src="test.jpg" /> <meta charset="utf-8" />');
            (expect(result).toContain("<br />"), expect(result).toContain('(img src="test.jpg" /)'), expect(result).toContain('(meta charset="utf-8" /)'));
          }),
          it("should handle XML tags with various whitespace patterns", () => {
            const result = sanitizeIncomingTextFunction('Various: <div\tclass="test">content</div> <span\n  id="test">text</span>');
            (expect(result).toContain('(div\tclass="test")content(/div)'), expect(result).toContain('(span\n  id="test")text(/span)'));
          }),
          it("should redact non-https protocols", () => {
            const result = sanitizeIncomingTextFunction("Visit http://example.com or ftp://files.com");
            (expect(result).toContain("/redacted"), expect(result).not.toContain("http://example.com"));
          }),
          it("should allow github.com domains", () => {
            const result = sanitizeIncomingTextFunction("Visit https://github.com/user/repo");
            expect(result).toContain("https://github.com/user/repo");
          }),
          it("should redact unknown domains", () => {
            const result = sanitizeIncomingTextFunction("Visit https://evil.com/malware");
            (expect(result).toContain("/redacted"), expect(result).not.toContain("https://evil.com"));
          }),
          it("should truncate long content", () => {
            const longContent = "a".repeat(6e5),
              result = sanitizeIncomingTextFunction(longContent);
            (expect(result.length).toBeLessThan(6e5), expect(result).toContain("[Content truncated due to length]"));
          }),
          it("should truncate too many lines", () => {
            const manyLines = Array(7e4).fill("line").join("\n"),
              result = sanitizeIncomingTextFunction(manyLines);
            (expect(result.split("\n").length).toBeLessThan(7e4), expect(result).toContain("[Content truncated due to line count]"));
          }),
          it("should remove ANSI escape sequences", () => {
            const result = sanitizeIncomingTextFunction("Hello [31mred[0m world");
            (expect(result).toMatch(/Hello.*red.*world/), expect(result).not.toMatch(/\u001b\[/));
          }),
          it("should respect custom allowed domains", () => {
            process.env.GH_AW_ALLOWED_DOMAINS = "example.com,trusted.org";
            const result = sanitizeIncomingTextFunction("Visit https://example.com and https://trusted.org and https://evil.com");
            (expect(result).toContain("https://example.com"), expect(result).toContain("https://trusted.org"), expect(result).toContain("/redacted"));
          }));
      }),
      describe("main function", () => {
        let testMain;
        (beforeEach(() => {
          (mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValue({ data: { permission: "admin" } }),
            mockGithub.rest.repos.listCollaborators.mockResolvedValue({
              data: [
                { login: "team-member-1", type: "User", permissions: { push: !1, admin: !1, maintain: !0 } },
                { login: "team-member-2", type: "User", permissions: { push: !1, admin: !0, maintain: !1 } },
                { login: "dependabot", type: "Bot", permissions: { push: !0, admin: !1, maintain: !1 } },
              ],
            }),
            (testMain = global.testMain));
        }),
          it("should extract text from issue payload", async () => {
            ((mockContext.eventName = "issues"),
              (mockContext.payload = { issue: { title: "Test Issue", body: "Issue description" } }),
              await testMain(),
              expect(mockCore.setOutput).toHaveBeenCalledWith("text", "Test Issue\n\nIssue description"));
          }),
          it("should extract text from pull request payload", async () => {
            ((mockContext.eventName = "pull_request"),
              (mockContext.payload = { pull_request: { title: "Test PR", body: "PR description" } }),
              await testMain(),
              expect(mockCore.setOutput).toHaveBeenCalledWith("text", "Test PR\n\nPR description"));
          }),
          it("should extract text from issue comment payload", async () => {
            ((mockContext.eventName = "issue_comment"), (mockContext.payload = { comment: { body: "This is a comment" } }), await testMain(), expect(mockCore.setOutput).toHaveBeenCalledWith("text", "This is a comment"));
          }),
          it("should extract text from pull request target payload", async () => {
            ((mockContext.eventName = "pull_request_target"),
              (mockContext.payload = { pull_request: { title: "Test PR Target", body: "PR target description" } }),
              await testMain(),
              expect(mockCore.setOutput).toHaveBeenCalledWith("text", "Test PR Target\n\nPR target description"));
          }),
          it("should extract text from pull request review comment payload", async () => {
            ((mockContext.eventName = "pull_request_review_comment"), (mockContext.payload = { comment: { body: "Review comment" } }), await testMain(), expect(mockCore.setOutput).toHaveBeenCalledWith("text", "Review comment"));
          }),
          it("should extract text from pull request review payload", async () => {
            ((mockContext.eventName = "pull_request_review"), (mockContext.payload = { review: { body: "Review body" } }), await testMain(), expect(mockCore.setOutput).toHaveBeenCalledWith("text", "Review body"));
          }),
          it("should extract text from discussion payload", async () => {
            ((mockContext.eventName = "discussion"),
              (mockContext.payload = { discussion: { title: "Test Discussion", body: "Discussion description" } }),
              await testMain(),
              expect(mockCore.setOutput).toHaveBeenCalledWith("text", "Test Discussion\n\nDiscussion description"));
          }),
          it("should extract text from discussion comment payload", async () => {
            ((mockContext.eventName = "discussion_comment"), (mockContext.payload = { comment: { body: "Discussion comment text" } }), await testMain(), expect(mockCore.setOutput).toHaveBeenCalledWith("text", "Discussion comment text"));
          }),
          it("should handle unknown event types", async () => {
            ((mockContext.eventName = "unknown_event"), (mockContext.payload = {}), await testMain(), expect(mockCore.setOutput).toHaveBeenCalledWith("text", ""));
          }),
          it("should deny access for non-admin/maintain users", async () => {
            (mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValue({ data: { permission: "read" } }),
              (mockContext.eventName = "issues"),
              (mockContext.payload = { issue: { title: "Test Issue", body: "Issue description" } }),
              await testMain(),
              expect(mockCore.setOutput).toHaveBeenCalledWith("text", ""));
          }),
          it("should sanitize extracted text before output", async () => {
            ((mockContext.eventName = "issues"), (mockContext.payload = { issue: { title: "Test @user fixes #123", body: "Visit https://evil.com" } }), await testMain());
            const outputCall = mockCore.setOutput.mock.calls[0];
            (expect(outputCall[1]).toContain("`@user`"), expect(outputCall[1]).toContain("`fixes #123`"), expect(outputCall[1]).toContain("/redacted"));
          }),
          it("should handle missing title and body gracefully", async () => {
            ((mockContext.eventName = "issues"), (mockContext.payload = { issue: {} }), await testMain(), expect(mockCore.setOutput).toHaveBeenCalledWith("text", ""));
          }),
          it("should handle null values in payload", async () => {
            ((mockContext.eventName = "issue_comment"), (mockContext.payload = { comment: { body: null } }), await testMain(), expect(mockCore.setOutput).toHaveBeenCalledWith("text", ""));
          }),
          it("should neutralize all mentions including issue author", async () => {
            ((mockContext.eventName = "issues"), (mockContext.payload = { issue: { title: "Test Issue by @issueAuthor", body: "Body mentioning @issueAuthor and @other", user: { login: "issueAuthor" } } }), await testMain());
            const outputCall = mockCore.setOutput.mock.calls[0];
            (expect(outputCall[1]).toContain("`@issueAuthor`"), expect(outputCall[1]).toContain("`@other`"));
          }),
          it("should neutralize all mentions including PR author", async () => {
            ((mockContext.eventName = "pull_request"), (mockContext.payload = { pull_request: { title: "PR by @prAuthor", body: "Mentioning @prAuthor", user: { login: "prAuthor" } } }), await testMain());
            const outputCall = mockCore.setOutput.mock.calls[0];
            expect(outputCall[1]).toContain("`@prAuthor`");
          }),
          it("should neutralize all mentions including comment author", async () => {
            ((mockContext.eventName = "issue_comment"), (mockContext.payload = { comment: { body: "Comment by @commentAuthor mentioning @commentAuthor", user: { login: "commentAuthor" } } }), await testMain());
            const outputCall = mockCore.setOutput.mock.calls[0];
            expect(outputCall[1]).toContain("`@commentAuthor`");
          }),
          it("should neutralize all mentions including discussion author", async () => {
            ((mockContext.eventName = "discussion"), (mockContext.payload = { discussion: { title: "Discussion by @discussionAuthor", body: "Body with @discussionAuthor", user: { login: "discussionAuthor" } } }), await testMain());
            const outputCall = mockCore.setOutput.mock.calls[0];
            expect(outputCall[1]).toContain("`@discussionAuthor`");
          }),
          it("should neutralize all mentions including release author", async () => {
            ((mockContext.eventName = "release"), (mockContext.payload = { release: { name: "Release by @releaseAuthor", body: "Notes mentioning @releaseAuthor", author: { login: "releaseAuthor" } } }), await testMain());
            const outputCall = mockCore.setOutput.mock.calls[0];
            expect(outputCall[1]).toContain("`@releaseAuthor`");
          }),
          it("should neutralize all mentions regardless of case", async () => {
            ((mockContext.eventName = "issues"), (mockContext.payload = { issue: { title: "Test @AUTHOR", body: "Body with @author", user: { login: "Author" } } }), await testMain());
            const outputCall = mockCore.setOutput.mock.calls[0];
            (expect(outputCall[1]).toContain("`@AUTHOR`"), expect(outputCall[1]).toContain("`@author`"));
          }),
          it("should neutralize all mentions including comment and issue authors", async () => {
            ((mockContext.eventName = "issue_comment"),
              (mockContext.payload = { comment: { body: "Mentioning @commentAuthor and @issueAuthor and @other", user: { login: "commentAuthor" } }, issue: { user: { login: "issueAuthor" } } }),
              await testMain());
            const outputCall = mockCore.setOutput.mock.calls[0];
            (expect(outputCall[1]).toContain("`@commentAuthor`"), expect(outputCall[1]).toContain("`@issueAuthor`"), expect(outputCall[1]).toContain("`@other`"));
          }),
          it("should neutralize all mentions including review comment and PR authors", async () => {
            ((mockContext.eventName = "pull_request_review_comment"),
              (mockContext.payload = { comment: { body: "Mentioning @reviewCommentAuthor and @prAuthor and @other", user: { login: "reviewCommentAuthor" } }, pull_request: { user: { login: "prAuthor" } } }),
              await testMain());
            const outputCall = mockCore.setOutput.mock.calls[0];
            (expect(outputCall[1]).toContain("`@reviewCommentAuthor`"), expect(outputCall[1]).toContain("`@prAuthor`"), expect(outputCall[1]).toContain("`@other`"));
          }),
          it("should neutralize all mentions including review and PR authors", async () => {
            ((mockContext.eventName = "pull_request_review"),
              (mockContext.payload = { review: { body: "Mentioning @reviewAuthor and @prAuthor and @other", user: { login: "reviewAuthor" } }, pull_request: { user: { login: "prAuthor" } } }),
              await testMain());
            const outputCall = mockCore.setOutput.mock.calls[0];
            (expect(outputCall[1]).toContain("`@reviewAuthor`"), expect(outputCall[1]).toContain("`@prAuthor`"), expect(outputCall[1]).toContain("`@other`"));
          }),
          it("should neutralize all mentions including comment and discussion authors", async () => {
            ((mockContext.eventName = "discussion_comment"),
              (mockContext.payload = { comment: { body: "Mentioning @commentAuthor and @discussionAuthor and @other", user: { login: "commentAuthor" } }, discussion: { user: { login: "discussionAuthor" } } }),
              await testMain());
            const outputCall = mockCore.setOutput.mock.calls[0];
            (expect(outputCall[1]).toContain("`@commentAuthor`"), expect(outputCall[1]).toContain("`@discussionAuthor`"), expect(outputCall[1]).toContain("`@other`"));
          }),
          it("should neutralize all mentions including workflow_dispatch actor", async () => {
            ((mockContext.actor = "dispatchActor"),
              (mockContext.eventName = "workflow_dispatch"),
              (mockContext.payload = { inputs: { release_id: "12345" } }),
              (mockGithub.rest.repos.getRelease = vi.fn().mockResolvedValue({ data: { name: "v1.0.0", body: "Release by @dispatchActor and @releaseAuthor and @other", author: { login: "releaseAuthor" } } })),
              await testMain());
            const outputCall = mockCore.setOutput.mock.calls[0];
            (expect(outputCall[1]).toContain("`@dispatchActor`"), expect(outputCall[1]).toContain("`@releaseAuthor`"), expect(outputCall[1]).toContain("`@other`"));
          }),
          it("should handle workflow_dispatch without release inputs", async () => {
            ((mockContext.actor = "dispatchActor"), (mockContext.eventName = "workflow_dispatch"), (mockContext.payload = { inputs: {} }), await testMain(), expect(mockCore.setOutput).toHaveBeenCalledWith("text", ""));
          }),
          it("should neutralize all mentions in workflow_dispatch with release_url", async () => {
            ((mockContext.actor = "dispatchActor"),
              (mockContext.eventName = "workflow_dispatch"),
              (mockContext.payload = { inputs: { release_url: "https://github.com/test-owner/test-repo/releases/tag/v1.0.0" } }),
              (mockGithub.rest.repos.getReleaseByTag = vi.fn().mockResolvedValue({ data: { name: "v1.0.0", body: "Release notes mentioning @dispatchActor", author: { login: "releaseAuthor" } } })),
              await testMain());
            const outputCall = mockCore.setOutput.mock.calls[0];
            expect(outputCall[1]).toContain("`@dispatchActor`");
          }),
          it("should neutralize bot authors like any other mention", async () => {
            ((mockContext.eventName = "issues"), (mockContext.payload = { issue: { title: "Test @botUser", body: "Body mentioning @botUser", user: { login: "botUser", type: "Bot" } } }), await testMain());
            const outputCall = mockCore.setOutput.mock.calls[0];
            expect(outputCall[1]).toContain("`@botUser`");
          }),
          it("should neutralize all mentions including team members", async () => {
            ((mockContext.eventName = "issues"),
              (mockContext.payload = { issue: { title: "Test @team-member-1 and @team-member-2", body: "Body mentioning @team-member-1 and @team-member-2", user: { login: "issueAuthor" } } }),
              await testMain());
            const outputCall = mockCore.setOutput.mock.calls[0];
            (expect(outputCall[1]).toContain("`@team-member-1`"), expect(outputCall[1]).toContain("`@team-member-2`"));
          }),
          it("should neutralize bot team members like any other mention", async () => {
            ((mockContext.eventName = "issues"), (mockContext.payload = { issue: { title: "Test @dependabot", body: "Body mentioning @dependabot", user: { login: "issueAuthor" } } }), await testMain());
            const outputCall = mockCore.setOutput.mock.calls[0];
            expect(outputCall[1]).toContain("`@dependabot`");
          }),
          it("should not log allowed mentions (mentions not resolved in compute_text)", async () => {
            ((mockContext.eventName = "issues"), (mockContext.payload = { issue: { title: "Test @team-member-1 and @issueAuthor", body: "Body mentioning @team-member-2", user: { login: "issueAuthor" } } }), await testMain());
            const allowedMentionsLog = mockCore.info.mock.calls.map(call => call[0]).find(msg => msg.includes("Allowed mentions"));
            expect(allowedMentionsLog).toBeUndefined();
          }),
          it("should not log known authors from payload (not tracked in compute_text)", async () => {
            ((mockContext.eventName = "issues"),
              (mockContext.payload = {
                issue: {
                  title: "Test issue",
                  body: "Body with @issueAuthor and @assignee1",
                  user: { login: "issueAuthor" },
                  assignees: [
                    { login: "assignee1", type: "User" },
                    { login: "assignee2", type: "User" },
                  ],
                },
              }),
              await testMain());
            const knownAuthorsLog = mockCore.info.mock.calls.map(call => call[0]).find(msg => msg.includes("Known authors (from payload)"));
            expect(knownAuthorsLog).toBeUndefined();
          }),
          it("should log escaped mentions", async () => {
            ((mockContext.eventName = "issues"), (mockContext.payload = { issue: { title: "Test @unknown-user", body: "Body mentioning @team-member-1", user: { login: "issueAuthor" } } }), await testMain());
            const escapedMentionLogs = mockCore.info.mock.calls.map(call => call[0]).filter(msg => msg.includes("Escaped mention"));
            expect(escapedMentionLogs.length).toBeGreaterThanOrEqual(2);
            const allEscapedMentions = escapedMentionLogs.join(" ");
            (expect(allEscapedMentions).toContain("@unknown-user"), expect(allEscapedMentions).toContain("@team-member-1"));
          }),
          it("should not handle team member fetch (moved to output collector)", async () => {
            ((mockContext.eventName = "issues"),
              (mockContext.payload = { issue: { title: "Test @issueAuthor", body: "Body", user: { login: "issueAuthor" } } }),
              await testMain(),
              expect(mockGithub.rest.repos.listCollaborators).not.toHaveBeenCalled(),
              expect(mockCore.setOutput).toHaveBeenCalledWith("text", expect.any(String)));
          }));
      }));
  }));
